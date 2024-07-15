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
	// deletedSecrets tracks routes for which the associated secret was deleted (populated inside DeleteFunc)
	// after intial creation of the secret monitor. This helps to differentiate between a new secret creation
	// and a recreation of a previously deleted secret.
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
// For Added events, it validates the route's external certificate configuration and registers it with the secret manager.
// For Modified events, it first unregisters the route if it's already registered and then revalidates and registers it again.
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

	// For Modified events always unregister and reregister the route even if the TLS configuration did not change.
	// Since the `HandleRoute()` method does not carry the old route spec,
	// and there's no definite way to compare old and new TLS configurations,
	// assume that the TLS configuration is always updated, necessitating re-registration.
	// Additionally, always creating a new `secretHandler` ensures that there are no stale route specs
	// in the next plugin chain, especially when the referenced secret is updated or deleted.
	// This prevents sending outdated routes to subsequent plugins, preserving expected functionality.
	// TODO: Refer https://github.com/openshift/router/pull/565#discussion_r1596441128 for possible ways to improve the logic.
	case watch.Modified:
		// unregister with secret monitor
		if err := p.unregister(route); err != nil {
			return err
		}

		// register with secret monitor
		if hasExternalCertificate(route) {
			if err := p.validateAndRegister(route); err != nil {
				return err
			}
		}

	case watch.Deleted:
		// unregister with secret monitor
		if err := p.unregister(route); err != nil {
			return err
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
	fldPath := field.NewPath("spec").Child("tls").Child("externalCertificate")
	// validate
	if err := routeapihelpers.ValidateTLSExternalCertificate(route, fldPath, p.sarClient, p.secretsGetter).ToAggregate(); err != nil {
		log.Error(err, "skipping route due to invalid externalCertificate configuration", "namespace", route.Namespace, "route", route.Name)
		p.recorder.RecordRouteRejection(route, "ExternalCertificateValidationFailed", err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return err
	}

	// register route with secretManager
	handler := p.generateSecretHandler(route)
	if err := p.secretManager.RegisterRoute(context.TODO(), route.Namespace, route.Name, route.Spec.TLS.ExternalCertificate.Name, handler); err != nil {
		log.Error(err, "failed to register route")
		return err
	}
	// read referenced secret
	secret, err := p.secretManager.GetSecret(context.TODO(), route.Namespace, route.Name)
	if err != nil {
		log.Error(err, "failed to get referenced secret")
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

// generateSecretHandler creates ResourceEventHandlerFuncs to handle Add, Update, and Delete events on secrets.
// AddFunc: Invoked when a new secret is added. It logs the addition of the secret.
// UpdateFunc: Invoked when an existing secret is updated. It performs validation of the route's external certificate configuration.
// If the validation fails, it records the route rejection, and triggers the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
// If the validation succeeds, it updates the route's TLS certificate and key with the new secret data and calls the next plugin's HandleRoute method with a watch.Modified event, and then the next plugin's Commit() method.
// DeleteFunc: Invoked when the secret is deleted. It unregisters the associated route, records the route rejection, and triggers the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
func (p *RouteSecretManager) generateSecretHandler(route *routev1.Route) cache.ResourceEventHandlerFuncs {
	// secret handler
	return cache.ResourceEventHandlerFuncs{

		// AddFunc is intentionally left empty (only logs the event) because this handler is generated only after ensuring the existence of the secret.
		// By leaving this empty, we prevent unnecessary triggering for the addition of the secret again. Additionally, GetSecret() method is called
		// immediately after registering with the secretManager, to read the secret from the cache.
		AddFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			log.V(4).Info("secret added for route", "namespace", route.Namespace, "secret", secret.Name, "route", route.Name)

			// Secret re-creation scenario

			// Check if the route key exists in the deletedSecrets map i.e check if the secret was deleted previously for this route.
			// If it exists, it means the secret is being recreated. Remove the key from the map and proceed with re-registration. Else no-op (new secret creation).
			// this helps to differentiate between a new secret creation and a recreation of a previously deleted secret.
			key := generateKey(route.Namespace, route.Name)
			if _, deleted := p.deletedSecrets.LoadAndDelete(key); deleted {
				log.V(4).Info("secret recreated for route", "namespace", route.Namespace, "secret", secret.Name, "route", route.Name)

				// re-validate
				// since secret re-creation will not trigger apiserver route admission,
				// we need to rely on router controller for this validation.
				fldPath := field.NewPath("spec").Child("tls").Child("externalCertificate")
				if err := routeapihelpers.ValidateTLSExternalCertificate(route, fldPath, p.sarClient, p.secretsGetter).ToAggregate(); err != nil {
					log.Error(err, "skipping route due to invalid externalCertificate configuration", "namespace", route.Namespace, "route", route.Name)
					p.recorder.RecordRouteRejection(route, "ExternalCertificateValidationFailed", err.Error())
					// route should be already deleted in this case
					// p.plugin.HandleRoute(watch.Deleted, route)
					return
				}

				// read the re-created secret
				reCreatedSecret, err := p.secretManager.GetSecret(context.TODO(), route.Namespace, route.Name)
				if err != nil {
					log.Error(err, "failed to get referenced secret")
					p.recorder.RecordRouteRejection(route, "ExternalCertificateGetFailed", err.Error())
					// route should be already deleted in this case
					// p.plugin.HandleRoute(watch.Deleted, route)
					return
				}

				// update tls.Certificate and tls.Key
				route.Spec.TLS.Certificate = string(reCreatedSecret.Data["tls.crt"])
				route.Spec.TLS.Key = string(reCreatedSecret.Data["tls.key"])

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
			fldPath := field.NewPath("spec").Child("tls").Child("externalCertificate")
			if err := routeapihelpers.ValidateTLSExternalCertificate(route, fldPath, p.sarClient, p.secretsGetter).ToAggregate(); err != nil {
				log.Error(err, "skipping route due to invalid externalCertificate configuration", "namespace", route.Namespace, "route", route.Name)
				p.recorder.RecordRouteRejection(route, "ExternalCertificateValidationFailed", err.Error())
				p.plugin.HandleRoute(watch.Deleted, route)
				return
			}

			// read referenced secret (updated data)
			secret, err := p.secretManager.GetSecret(context.TODO(), route.Namespace, route.Name)
			if err != nil {
				log.Error(err, "failed to get referenced secret")
				p.recorder.RecordRouteRejection(route, "ExternalCertificateGetFailed", err.Error())
				p.plugin.HandleRoute(watch.Deleted, route)
				return
			}

			// update tls.Certificate and tls.Key
			route.Spec.TLS.Certificate = string(secret.Data["tls.crt"])
			route.Spec.TLS.Key = string(secret.Data["tls.key"])

			// call the next plugin with watch.Modified
			p.plugin.HandleRoute(watch.Modified, route)
			// commit the changes
			p.plugin.Commit()
		},

		DeleteFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			key := generateKey(route.Namespace, route.Name)
			msg := fmt.Sprintf("secret %s deleted for route %s", secret.Name, key)
			log.V(4).Info(msg)

			// keep the secret monitor active and mark the secret as deleted for this route.
			p.deletedSecrets.Store(key, true)

			p.recorder.RecordRouteRejection(route, "ExternalCertificateSecretDeleted", msg)
			p.plugin.HandleRoute(watch.Deleted, route)
		},
	}
}

func (p *RouteSecretManager) unregister(route *routev1.Route) error {
	// unregister associated secret monitor, if registered
	if p.secretManager.IsRouteRegistered(route.Namespace, route.Name) {
		if err := p.secretManager.UnregisterRoute(route.Namespace, route.Name); err != nil {
			log.Error(err, "failed to unregister route")
			return err
		}
		// clean the route if present inside deletedSecrets
		// this is required for the scenario when the associated secret is deleted, before unregistering with secretManager
		p.deletedSecrets.Delete(generateKey(route.Namespace, route.Name))
	}
	return nil
}

func hasExternalCertificate(route *routev1.Route) bool {
	tls := route.Spec.TLS
	return tls != nil && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0
}

// generateKey creates a unique identifier for a route
func generateKey(namespace, route string) string {
	return fmt.Sprintf("%s/%s", namespace, route)
}
