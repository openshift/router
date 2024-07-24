package controller

import (
	"context"
	"fmt"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/library-go/pkg/route/secretmanager"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/routeapihelpers"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

// RouteSecretManager implements the router.Plugin interface to register
// or unregister route with secretManger if externalCertificate is used.
// It also reads the referenced secret to update in-memory tls.Certificate and tls.Key
type RouteSecretManager struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin
	// recorder is an interface for indicating route status.
	recorder RouteStatusRecorder

	secretManager secretmanager.SecretManager
	secretsGetter corev1client.SecretsGetter
	sarClient     authorizationclient.SubjectAccessReviewInterface
	// deletedSecrets tracks routes for which the associated secret was deleted after intial creation of the secret monitor.
	// This helps to differentiate between a new secret creation and a recreation of a previously deleted secret.
	// Populated inside DeleteFunc, and consumed or cleaned inside AddFunc and unregister().
	// It is thread safe and "namespace/routeName" is used as it's key.
	deletedSecrets sync.Map
}

// NewRouteSecretManager creates a new instance of RouteSecretManager.
// It wraps the provided plugin and adds secret management capabilities.
func NewRouteSecretManager(plugin router.Plugin, recorder RouteStatusRecorder, secretManager secretmanager.SecretManager, secretsGetter corev1client.SecretsGetter, sarClient authorizationclient.SubjectAccessReviewInterface) *RouteSecretManager {
	return &RouteSecretManager{
		plugin:         plugin,
		recorder:       recorder,
		secretManager:  secretManager,
		secretsGetter:  secretsGetter,
		sarClient:      sarClient,
		deletedSecrets: sync.Map{},
	}
}

func (p *RouteSecretManager) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return p.plugin.HandleNode(eventType, node)
}

func (p *RouteSecretManager) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	return p.plugin.HandleEndpoints(eventType, endpoints)
}

func (p *RouteSecretManager) HandleNamespaces(namespaces sets.String) error {
	return p.plugin.HandleNamespaces(namespaces)
}

func (p *RouteSecretManager) Commit() error {
	return p.plugin.Commit()
}

// HandleRoute manages the registration, unregistration, and validation of routes with external certificates.
//
// For Added events, it validates the route's external certificate configuration and registers it with the secret manager.
//
// For Modified events, it checks if the route's external certificate configuration has changed and takes appropriate actions:
//  1. Both the old and new routes have an external certificate:
//     - If the external certificate has changed, it unregisters the old one and registers the new one.
//     - If the external certificate has not changed, it revalidates and updates the in-memory TLS certificate and key.
//  2. The new route has an external certificate, but the old one did not:
//     - It registers the new route with the secret manager.
//  3. The old route had an external certificate, but the new one does not:
//     - It unregisters the old route from the secret manager.
//  4. Neither the old nor the new route has an external certificate:
//     - No action is taken.
//
// For Deleted events, it unregisters the route if it's registered.
// Additionally, it delegates the handling of the event to the next plugin in the chain after performing the necessary actions.
func (p *RouteSecretManager) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	log.V(10).Info("HandleRoute: RouteSecretManager", "eventType", eventType)

	switch eventType {
	case watch.Added:
		// register with secret monitor
		if hasExternalCertificate(route) {
			if err := p.validateAndRegister(route); err != nil {
				return err
			}
		}

	// TODO always creating a new `secretHandler` ensures that there are no stale route specs
	// in the next plugin chain, especially when the referenced secret is updated or deleted.
	// This prevents sending outdated routes to subsequent plugins, preserving expected functionality.
	// TODO : we might need to add RouteLister()
	case watch.Modified:
		// Determine if the route's external certificate configuration has changed
		newHasExt := hasExternalCertificate(route)
		oldSecret, oldHadExt := p.secretManager.LookupRouteSecret(route.Namespace, route.Name)

		switch {
		case newHasExt && oldHadExt:
			// Both new and old routes have externalCertificate
			log.V(6).Info("Both new and old routes have externalCertificate", "namespace", route.Namespace, "route", route.Name)
			if oldSecret != route.Spec.TLS.ExternalCertificate.Name {
				// ExternalCertificate is updated
				log.V(4).Info("ExternalCertificate is updated", "namespace", route.Namespace, "route", route.Name)
				// Unregister the old and register the new external certificate
				if err := p.unregister(route); err != nil {
					return err
				}
				if err := p.validateAndRegister(route); err != nil {
					return err
				}
			} else {
				// ExternalCertificate is not updated
				// Revalidate and update the in-memory TLS certificate and key
				// It is the responsibility of this plugin to ensure everything is synced properly.

				// One Scenario: The user deletes the secret, causing the route's status to be updated to reject.
				// This triggers the entire plugin chain again. Without re-validating the external certificate
				// and re-syncing the secret here, the route could become active again and start serving
				// the default certificate, even though its spec has an external certificate.
				// Therefore, it is essential to re-sync the secret to ensure the plugin chain correctly handles the route.

				log.V(4).Info("ExternalCertificate is not updated", "namespace", route.Namespace, "route", route.Name)
				// re-validate
				if err := p.validate(route); err != nil {
					return err
				}
				// read referenced secret and update TLS certificate and key
				if err := p.populateRouteTLSFromSecret(route); err != nil {
					return err
				}
			}

		case newHasExt && !oldHadExt:
			// New route has externalCertificate, old route did not
			log.V(4).Info("New route has externalCertificate, old route did not", "namespace", route.Namespace, "route", route.Name)
			// register with secret monitor
			if err := p.validateAndRegister(route); err != nil {
				return err
			}

		case !newHasExt && oldHadExt:
			// Old route had externalCertificate, new route does not
			log.V(4).Info("Old route had externalCertificate, new route does not", "namespace", route.Namespace, "route", route.Name)
			// unregister with secret monitor
			if err := p.unregister(route); err != nil {
				return err
			}

		case !newHasExt && !oldHadExt:
			// Neither new nor old route have externalCertificate
			// Do nothing
		}

	case watch.Deleted:
		// unregister associated secret monitor, if registered
		if _, exists := p.secretManager.LookupRouteSecret(route.Namespace, route.Name); exists {
			if err := p.unregister(route); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("invalid eventType %v", eventType)
	}

	// call next plugin
	return p.plugin.HandleRoute(eventType, route)
}

// validateAndRegister validates the route's externalCertificate configuration and registers it with the secret manager.
// It also updates the in-memory TLS certificate and key after reading from secret informer's cache.
func (p *RouteSecretManager) validateAndRegister(route *routev1.Route) error {
	// validate
	if err := p.validate(route); err != nil {
		return err
	}
	// register route with secretManager
	handler := p.generateSecretHandler(route)
	if err := p.secretManager.RegisterRoute(context.TODO(), route.Namespace, route.Name, route.Spec.TLS.ExternalCertificate.Name, handler); err != nil {
		log.Error(err, "failed to register route")
		return err
	}
	// read referenced secret and update TLS certificate and key
	if err := p.populateRouteTLSFromSecret(route); err != nil {
		return err
	}

	return nil
}

// generateSecretHandler creates ResourceEventHandlerFuncs to handle Add, Update, and Delete events on secrets.
//
// AddFunc: Invoked when a new secret is added. It logs the addition of the secret. This function also handles the
// re-creation scenario where a previously deleted secret is added again. In such cases, it revalidates the route's
// external certificate configuration, updates the route's TLS certificate and key, and calls the next plugin's HandleRoute method with a watch.Modified event, and then commits the changes by calling the next plugin's Commit() method.
//
// UpdateFunc: Invoked when an existing secret is updated. It performs validation of the route's external certificate configuration.
// If the validation succeeds, it updates the route's TLS certificate and key with the new secret data and calls the next plugin's HandleRoute method with a watch.Modified event, and then commits the changes by calling the next plugin's Commit() method.
//
// DeleteFunc: Invoked when the secret is deleted. It logs the deletion, marks the secret as deleted for this route in the deletedSecrets map,
// records the route rejection, and triggers the deletion of the route by calling the HandleRoute method with a watch.Deleted event. NOTE: It doesn't unregister the route.
func (p *RouteSecretManager) generateSecretHandler(route *routev1.Route) cache.ResourceEventHandlerFuncs {
	// secret handler
	return cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			log.V(4).Info("secret added for route", "namespace", route.Namespace, "secret", secret.Name, "route", route.Name)

			// Secret re-creation scenario
			// Check if the route key exists in the deletedSecrets map, indicating that the secret was previously deleted for this route.
			// If it exists, it means the secret is being recreated. Remove the key from the map and proceed with re-registration.
			// Otherwise, no-op (new secret creation scenario and no race condition with that flow)
			// This helps to differentiate between a new secret creation and a re-creation of a previously deleted secret.
			key := generateKey(route)
			if _, deleted := p.deletedSecrets.LoadAndDelete(key); deleted {
				log.V(4).Info("secret recreated for route", "namespace", route.Namespace, "secret", secret.Name, "route", route.Name)

				// re-validate
				// since secret re-creation will not trigger the apiserver route admission,
				// we need to rely on the router controller for this validation.
				if err := p.validate(route); err != nil {
					return
				}
				// read the re-created secret and update TLS certificate and key
				if err := p.populateRouteTLSFromSecret(route); err != nil {
					return
				}
				// call the next plugin with watch.Modified
				p.plugin.HandleRoute(watch.Modified, route)
				// commit the changes
				p.plugin.Commit()
			}
		},

		UpdateFunc: func(old interface{}, new interface{}) {
			secretOld := old.(*kapi.Secret)
			secretNew := new.(*kapi.Secret)
			log.V(4).Info("secret updated for route", "namespace", route.Namespace, "secret", secretNew.Name, "old-version", secretOld.ResourceVersion, "new-version", secretNew.ResourceVersion, "route", route.Name)

			// re-validate
			if err := p.validate(route); err != nil {
				return
			}
			// read referenced secret (updated data) and update TLS certificate and key
			if err := p.populateRouteTLSFromSecret(route); err != nil {
				return
			}
			// call the next plugin with watch.Modified
			p.plugin.HandleRoute(watch.Modified, route)
			// commit the changes
			p.plugin.Commit()
		},

		DeleteFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			key := generateKey(route)
			msg := fmt.Sprintf("secret %s deleted for route %s", secret.Name, key)
			log.V(4).Info(msg)

			// keep the secret monitor active and mark the secret as deleted for this route.
			p.deletedSecrets.Store(key, true)

			p.recorder.RecordRouteRejection(route, "ExternalCertificateSecretDeleted", msg)
			p.plugin.HandleRoute(watch.Deleted, route)
		},
	}
}

// validate checks the route's external certificate configuration.
// If the validation fails, it records the route rejection and triggers
// the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
func (p *RouteSecretManager) validate(route *routev1.Route) error {
	fldPath := field.NewPath("spec").Child("tls").Child("externalCertificate")
	if err := routeapihelpers.ValidateTLSExternalCertificate(route, fldPath, p.sarClient, p.secretsGetter).ToAggregate(); err != nil {
		log.Error(err, "skipping route due to invalid externalCertificate configuration", "namespace", route.Namespace, "route", route.Name)
		p.recorder.RecordRouteRejection(route, "ExternalCertificateValidationFailed", err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return err
	}
	return nil
}

// populateRouteTLSFromSecret updates the TLS configuration of the route using data from the referenced secret.
// If fetching the secret fails, it records the route rejection and triggers
// the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
// Note: This function performs an in-place update of the route. The caller should be aware that the route's TLS configuration will be modified directly.
func (p *RouteSecretManager) populateRouteTLSFromSecret(route *routev1.Route) error {
	// read referenced secret
	secret, err := p.secretManager.GetSecret(context.TODO(), route.Namespace, route.Name)
	if err != nil {
		log.Error(err, "failed to get referenced secret")
		p.recorder.RecordRouteRejection(route, "ExternalCertificateGetFailed", err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return err
	}

	// Update the tls.Certificate and tls.Key fields of the route with the data from the referenced secret.
	// Since externalCertificate does not contain the CACertificate, tls.CACertificate will not be updated.
	// NOTE that this update is only performed in-memory and will not reflect in the actual route resource stored in etcd, because
	// the router does not make kube-client calls to directly update route resources.
	route.Spec.TLS.Certificate = string(secret.Data["tls.crt"])
	route.Spec.TLS.Key = string(secret.Data["tls.key"])

	return nil
}

// unregister removes the route's registration with the secret manager and ensures
// that any references to the deletedSecrets are cleaned up.
func (p *RouteSecretManager) unregister(route *routev1.Route) error {
	// unregister associated secret monitor
	if err := p.secretManager.UnregisterRoute(route.Namespace, route.Name); err != nil {
		log.Error(err, "failed to unregister route")
		return err
	}
	// clean the route if present inside deletedSecrets
	// this is required for the scenario when the associated secret is deleted, before unregistering with secretManager
	p.deletedSecrets.Delete(generateKey(route))
	return nil
}

// hasExternalCertificate checks whether the given route has an externalCertificate specified.
func hasExternalCertificate(route *routev1.Route) bool {
	tls := route.Spec.TLS
	return tls != nil && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0
}

// generateKey creates a unique identifier for a route
func generateKey(route *routev1.Route) string {
	return fmt.Sprintf("%s/%s", route.Namespace, route.Name)
}
