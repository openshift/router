package controller

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/library-go/pkg/route/secretmanager"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/routeapihelpers"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	ExtCrtStatusReasonValidationFailed = "ExternalCertificateValidationFailed"
	ExtCrtStatusReasonSecretRecreated  = "ExternalCertificateSecretRecreated"
	ExtCrtStatusReasonSecretUpdated    = "ExternalCertificateSecretUpdated"
	ExtCrtStatusReasonSecretDeleted    = "ExternalCertificateSecretDeleted"
	ExtCrtStatusReasonGetFailed        = "ExternalCertificateGetFailed"
	ExtCrtStatusReasonSARCompleted     = "ExternalCertificateSARCompleted"

	// certResourceVersionAnnotation is an in-memory-only annotation set
	// by populateRouteTLSFromSecret to carry the secret's
	// ResourceVersion through the plugin chain into
	// templateRouter.AddRoute, where it is used as a staleness guard.
	certResourceVersionAnnotation = "router.openshift.io/cert-resource-version"
)

// secretUpdateRecheckDelay is how long the UpdateFunc secret handler waits
// before re-checking SAR permissions after a secret update, to catch RBAC
// revocations that haven't propagated to the API server's authorizer yet.
// Overridable in tests (via atomic Store/Load, since a prior test's spawned
// goroutine may still be sleeping on this value when a later test runs) to
// avoid real sleeps.
var secretUpdateRecheckDelay atomic.Int64

func init() {
	secretUpdateRecheckDelay.Store(int64(3 * time.Second))
}

// RouteSecretManager implements the router.Plugin interface to register
// or unregister route with secretManger if externalCertificate is used.
// It also reads the referenced secret to update in-memory tls.Certificate and tls.Key
type RouteSecretManager struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin
	// recorder is an interface for indicating route status.
	recorder RouteStatusRecorder

	secretManager secretmanager.SecretManager
	routerName    string
	secretsGetter corev1client.SecretsGetter
	routelister   routelisters.RouteLister
	sarClient     authorizationclient.SubjectAccessReviewInterface
	// deletedSecrets tracks routes for which the associated secret was deleted after initial creation of the secret monitor.
	// This helps to differentiate between a new secret creation and a recreation of a previously deleted secret.
	// Populated inside DeleteFunc, and consumed or cleaned inside AddFunc and unregister().
	// It is thread safe and "namespace/routeName" is used as its key.
	deletedSecrets sync.Map

	// routeLocks serializes cert validation and refresh (validate +
	// populateRouteTLSFromSecret + propagation to the plugin chain) per
	// route, keyed by "namespace/routeName". This work can be triggered
	// concurrently from two different goroutines for the same route: the
	// route-watch-driven HandleRoute path (re-validation on any Modified
	// event) and the secret-watch-driven UpdateFunc path (reacting to a
	// secret change). GetSecret always reads the current, monotonically
	// advancing informer cache rather than a captured-earlier snapshot, so
	// serializing the full read-then-propagate sequence guarantees whichever
	// side runs second observes state at least as fresh as the first --
	// closing the race where a slower call that started earlier finishes
	// after a faster one and silently overwrites its fresh cert with stale
	// data.
	//
	// Deliberately never cleaned up: safely removing a keyed mutex entry
	// requires knowing no one else is about to look it up, which a simple
	// sync.Map can't guarantee -- deleting while another goroutine is
	// mid-LoadOrStore for the same key would hand out two different mutex
	// objects for the same route, silently defeating the serialization this
	// exists for. A live router process seeing enough distinct route names
	// over its lifetime to make this map's size a real concern is not a
	// realistic scenario, so unbounded (but tiny, one *sync.Mutex per name)
	// growth is the safer tradeoff over a subtly-reintroduced race.
	routeLocks sync.Map // map[types.NamespacedName]*sync.Mutex
}

// lockRoute acquires the per-route lock for key, creating it on first use,
// and returns a function to release it.
func (p *RouteSecretManager) lockRoute(key types.NamespacedName) func() {
	value, _ := p.routeLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// NewRouteSecretManager creates a new instance of RouteSecretManager.
// It wraps the provided plugin and adds secret management capabilities.
func NewRouteSecretManager(
	plugin router.Plugin,
	recorder RouteStatusRecorder,
	secretManager secretmanager.SecretManager,
	routerName string,
	secretsGetter corev1client.SecretsGetter,
	routelister routelisters.RouteLister,
	sarClient authorizationclient.SubjectAccessReviewInterface,
) *RouteSecretManager {
	return &RouteSecretManager{
		plugin:         plugin,
		recorder:       recorder,
		secretManager:  secretManager,
		routerName:     routerName,
		secretsGetter:  secretsGetter,
		routelister:    routelister,
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

	// DeepCopy the route before any mutation. The route pointer may come from
	// the informer cache (via the lister), which is shared across goroutines.
	// populateRouteTLSFromSecret writes Certificate and Key in-place, which
	// would race with informer goroutines that read the same object (e.g.,
	// secret handler UpdateFunc calling route.DeepCopy()).
	if hasExternalCertificate(route) {
		route = route.DeepCopy()
	}

	// registered tracks whether validateAndRegister was called, meaning this
	// is a first-time or new-cert registration that should emit SARCompleted.
	registered := false

	switch eventType {
	case watch.Added:
		// register with secret monitor
		if hasExternalCertificate(route) {
			log.V(4).Info("Validating and registering external certificate", "namespace", route.Namespace, "secret", route.Spec.TLS.ExternalCertificate.Name, "route", route.Name)
			unlock, err := p.validateAndRegister(route)
			if err != nil {
				return err
			}
			defer unlock()
			registered = true
		}

	case watch.Modified:
		// Determine if the route's external certificate configuration has changed
		newHasExt := hasExternalCertificate(route)
		oldSecret, oldHadExt := p.secretManager.LookupRouteSecret(route.Namespace, route.Name)

		switch {
		case newHasExt && oldHadExt:
			// Both new and old routes have externalCertificate
			if oldSecret != route.Spec.TLS.ExternalCertificate.Name {
				// ExternalCertificate is updated
				log.V(4).Info("Validating and registering updated external certificate", "namespace", route.Namespace, "oldSecret", oldSecret, "newSecret", route.Spec.TLS.ExternalCertificate.Name, "route", route.Name)
				// Unregister the old and register the new external certificate
				if err := p.unregister(route); err != nil {
					return err
				}
				unlock, err := p.validateAndRegister(route)
				if err != nil {
					return err
				}
				defer unlock()
				registered = true
			} else {
				// ExternalCertificate is not updated
				// Re-validate and update the in-memory TLS certificate and key (even if ExternalCertificate remains unchanged)
				// It is the responsibility of this plugin to ensure everything is synced properly, because there might
				// have been updates to the secret data or other events that require re-evaluation.
				//
				// 1. Secret update and re-create: Even if the externalCertificate name remains
				//    the same, the router needs to be aware of the events when the content of
				//    the secret is updated or the secret is recreated, to re-validate and
				//    fetch the new TLS data. Note: These events won't trigger the apiserver
				//    route admission, hence we need to rely on the router controller for this validation.
				//
				// 2. Consider a case where a user deletes the secret, causing the route's
				//    status to be updated to "reject". This status update triggers the
				//    entire plugin chain again.  Without re-validating the external certificate
				//    and re-syncing the secret here, the route could incorrectly transition
				//    back to an "active" state and start serving the default certificate
				//    even though its spec still references an external certificate.
				//
				// Therefore, it is essential to re-sync the secret to ensure the plugin chain correctly handles the route.

				log.V(4).Info("Re-validating existing external certificate", "namespace", route.Namespace, "secret", oldSecret, "route", route.Name)
				// re-validate (synchronous, throttled by semaphore). Runs
				// outside the per-route lock below: it only checks SAR/secret
				// existence and doesn't write cert content, so there's
				// nothing here for a concurrent UpdateFunc refresh to race
				// against -- serializing it too would just add this call's
				// SAR-check latency to the critical section for no benefit.
				if err := p.validate(route); err != nil {
					return err
				}
				// Don't let the SAR result from this re-validation persist
				// in the cache. If RBAC was just revoked, the API server
				// may not have propagated the change yet, producing a stale
				// "allowed" result. Invalidating ensures the next evaluation
				// (triggered by the rejection→re-admission status cycle)
				// does a fresh SAR check.
				routeapihelpers.InvalidateAsyncSARCache(route.Namespace, route.Spec.TLS.ExternalCertificate.Name)

				// Serialize with any concurrent secret-triggered refresh (see
				// UpdateFunc) from here through the propagation call below,
				// so the two can never race to write different cert content
				// to the plugin chain (see the routeLocks field comment).
				unlock := p.lockRoute(routeKey(route.Namespace, route.Name))
				defer unlock()

				// read referenced secret and update TLS certificate and key
				if err := p.populateRouteTLSFromSecret(route); err != nil {
					return err
				}

			}

		case newHasExt && !oldHadExt:
			// New route has externalCertificate, old route did not
			log.V(4).Info("Validating and registering new external certificate", "namespace", route.Namespace, "secret", route.Spec.TLS.ExternalCertificate.Name, "route", route.Name)
			// register with secret monitor
			unlock, err := p.validateAndRegister(route)
			if err != nil {
				return err
			}
			defer unlock()
			registered = true

		case !newHasExt && oldHadExt:
			// Old route had externalCertificate, new route does not
			log.V(4).Info("Unregistering removed external certificate", "namespace", route.Namespace, "secret", oldSecret, "route", route.Name)
			// unregister with secret monitor
			if err := p.unregister(route); err != nil {
				return err
			}
		}

	case watch.Deleted:
		// unregister associated secret monitor, if registered
		if secretName, exists := p.secretManager.LookupRouteSecret(route.Namespace, route.Name); exists {
			log.V(4).Info("Unregistering external certificate", "namespace", route.Namespace, "secret", secretName, "route", route.Name)
			if err := p.unregister(route); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("invalid eventType %v", eventType)
	}

	// call next plugin
	err := p.plugin.HandleRoute(eventType, route)

	// Only emit SARCompleted when validateAndRegister was called in this
	// pass — i.e., on first-time registration or cert change. Skip it on
	// re-validation (Modified with same cert), which would create a
	// re-enqueue feedback loop and can re-admit routes that were rejected
	// by the secret handlers.
	//
	// registered alone is not enough: validateAndRegister runs concurrently
	// with the secret informer's own DeleteFunc on a different goroutine, so
	// a watch.Added registration that was already in flight when the secret
	// got deleted can finish afterward and write this SARCompleted status,
	// silently overwriting DeleteFunc's rejection. Checking deletedSecrets
	// here closes that race regardless of which goroutine finishes last.
	if err == nil && registered {
		key := routeKey(route.Namespace, route.Name)
		if _, secretDeleted := p.deletedSecrets.Load(key); !secretDeleted {
			msg := fmt.Sprintf("SAR check and secret load completed for secret %q", route.Spec.TLS.ExternalCertificate.Name)
			p.recorder.RecordRouteUpdate(route, ExtCrtStatusReasonSARCompleted, msg)
		}
	}

	return err
}

// validateAndRegister registers the route with the secret manager, validates
// its externalCertificate configuration, and loads the TLS cert data.
//
// Registration happens BEFORE validation so the route receives informer
// events (Add/Update/Delete) even if the initial SAR check fails due to
// RBAC propagation delays. Without this ordering, a transient SAR failure
// permanently orphans the route from the secret informer — it never
// receives UpdateFunc events and can never pick up secret changes.
//
// If validation fails after registration, the route stays registered (so
// future informer events can trigger re-evaluation) but is not admitted.
//
// The per-route lock is acquired after registration and held until the
// caller releases it, so cert-population and propagation happen as one
// atomic unit with respect to any concurrent secret-triggered refresh.
func (p *RouteSecretManager) validateAndRegister(route *routev1.Route) (unlock func(), err error) {
	// Register route with secretManager first, so it receives informer
	// events regardless of whether the SAR check below passes.
	handler := p.generateSecretHandler(route.Namespace, route.Name)
	if err := p.secretManager.RegisterRoute(context.TODO(), route.Namespace, route.Name, route.Spec.TLS.ExternalCertificate.Name, handler); err != nil {
		return nil, fmt.Errorf("failed to register router: %w", err)
	}

	// validate (synchronous, throttled by semaphore)
	if err := p.validate(route); err != nil {
		return nil, err
	}

	unlock = p.lockRoute(routeKey(route.Namespace, route.Name))

	// read referenced secret and update TLS certificate and key
	if err := p.populateRouteTLSFromSecret(route); err != nil {
		unlock()
		return nil, err
	}

	return unlock, nil
}

// generateSecretHandler creates ResourceEventHandlerFuncs to handle Add, Update, and Delete events on secrets.
//
// To ensure that the handlers always operate on the most up-to-date route object,
// it fetches the latest route from the informer and then updates the route's status
// with specific reasons related to the secret event. This status update is crucial
// because it serves as a signal to trigger the entire route plugin chain to re-evaluate
// the route from the beginning.
//
// Triggering a re-evaluation ensures that both:
//   - Changes made by `RouteModifierFn` registered with the `RouterControllerFactory`
//     are propagated correctly to all plugins.
//   - All plugins get a chance to react to the changes and make necessary
//     in-memory modifications to the route object, ensuring consistent behavior.
//
// - AddFunc:
//   - Invoked when a new secret is added.
//   - Handles secret recreation: If a secret is recreated (created after being
//     deleted), it fetches the latest route object associated with it using the
//     RouteLister and updates the route's status to trigger a full
//     re-evaluation of the route by the entire plugin chain.
//
// - UpdateFunc:
//   - Invoked when an existing secret is updated.
//   - Fetches the latest associated route object and
//     updates the route's status to trigger a re-evaluation by the entire plugin chain.
//
// - DeleteFunc:
//   - Invoked when the secret is deleted.
//   - Marks the secret as deleted for the associated route in the `deletedSecrets` map.
//   - Fetches the latest associated route object and records a route rejection event.
//   - Triggers the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
//   - NOTE: It doesn't unregister the route.
func (p *RouteSecretManager) generateSecretHandler(namespace, routeName string) cache.ResourceEventHandlerFuncs {
	// secret handler
	return cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			log.V(4).Info("Secret added for route", "namespace", namespace, "secret", secret.Name, "route", routeName)
			routeapihelpers.InvalidateAsyncSARCache(namespace, secret.Name)

			// Secret re-creation scenario
			// Check if the route key exists in the deletedSecrets map, indicating that the secret was previously deleted for this route.
			// If it exists, it means the secret is being recreated. Remove the key from the map and proceed with handling the route.
			// Otherwise, no-op (new secret creation scenario and no race condition with that flow)
			// This helps to differentiate between a new secret creation and a re-creation of a previously deleted secret.
			key := routeKey(namespace, routeName)
			if _, deleted := p.deletedSecrets.LoadAndDelete(key); deleted {
				log.V(4).Info("Secret recreated for route", "namespace", namespace, "secret", secret.Name, "route", routeName)

				// Ensure fetching the updated route and DeepCopy to avoid
				// reading/writing the shared informer cache object.
				route, err := p.routelister.Routes(namespace).Get(routeName)
				if err != nil {
					log.Error(err, "failed to get route", "namespace", namespace, "route", routeName)
					return
				}
				route = route.DeepCopy()

				// The route should *remain* rejected until it's re-evaluated
				// by all the plugins (including this plugin). Once passes, the route will become active again.
				msg := fmt.Sprintf("secret %q recreated for route %q", secret.Name, key)
				p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonSecretRecreated, msg)
			}
		},

		UpdateFunc: func(old interface{}, new interface{}) {
			secretOld := old.(*kapi.Secret)
			secretNew := new.(*kapi.Secret)
			key := routeKey(namespace, routeName)
			log.V(4).Info("Secret updated for route", "namespace", namespace, "secret", secretNew.Name, "oldSecretVersion", secretOld.ResourceVersion, "newSecretVersion", secretNew.ResourceVersion, "route", routeName)
			routeapihelpers.InvalidateAsyncSARCache(namespace, secretNew.Name)

			// Ensure fetching the updated route and DeepCopy to avoid
			// reading/writing the shared informer cache object.
			route, err := p.routelister.Routes(namespace).Get(routeName)
			if err != nil {
				log.Error(err, "failed to get route", "namespace", namespace, "route", routeName)
				return
			}
			route = route.DeepCopy()

			msg := fmt.Sprintf("secret %q updated for route %q (oldSecretVersion=%v, newSecretVersion=%v)", secretNew.Name, key, secretOld.ResourceVersion, secretNew.ResourceVersion)
			// Keep the route admitted so it remains reachable while the
			// new cert is picked up below.
			p.recorder.RecordRouteUpdate(route, ExtCrtStatusReasonSecretUpdated, msg)

			// Read the new secret and push it through the plugin chain
			// immediately. We skip the synchronous validate() (SAR +
			// secret-existence check) here because:
			//  1. The secret was just updated — it exists.
			//  2. RBAC was verified when the route was first admitted.
			//  3. A delayed re-check goroutine (below) catches any RBAC
			//     revocation that happened concurrently.
			// Removing validate() from this synchronous path is critical
			// for performance: SharedSecretManager.notify() dispatches to
			// all route handlers for the secret sequentially on one
			// goroutine. With N routes sharing a secret, each validate()
			// makes 4 blocking API calls, creating N×4 sequential
			// round-trips that can exceed the test's poll timeout under
			// API-server load (e.g. HyperShift CI).
			func() {
				unlock := p.lockRoute(key)
				defer unlock()
				if err := p.populateRouteTLSFromSecret(route); err != nil {
					return
				}
				if err := p.plugin.HandleRoute(watch.Modified, route); err != nil {
					log.Error(err, "failed to propagate route after secret update", "namespace", namespace, "route", routeName)
				}
			}()

			// Trigger a rate-limited HAProxy reload directly instead of
			// depending on the indirect round trip (status write → API
			// server → route informer → RouterController.HandleRoute →
			// Commit), which adds 10-30s under HyperShift conditions.
			p.plugin.Commit()

			// Schedule a delayed re-check to catch RBAC revocations that
			// may not have propagated yet. The SAR cache was already
			// invalidated above (line 423), so the re-check does a fresh
			// SAR. validate() rejects and deactivates the route only if
			// the check now fails; a passing check is a no-op.
			go func() {
				time.Sleep(time.Duration(secretUpdateRecheckDelay.Load()))
				routeapihelpers.InvalidateAsyncSARCache(namespace, secretNew.Name)
				route, err := p.routelister.Routes(namespace).Get(routeName)
				if err != nil {
					return
				}
				route = route.DeepCopy()
				_ = p.validate(route)
			}()
		},

		DeleteFunc: func(obj interface{}) {
			secret, ok := obj.(*kapi.Secret)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					log.Error(nil, "Couldn't get object from tombstone", "type", fmt.Sprintf("%T", obj))
					return
				}
				secret, ok = tombstone.Obj.(*kapi.Secret)
				if !ok {
					log.Error(nil, "Tombstone contained object that is not a secret", "type", fmt.Sprintf("%T", tombstone.Obj))
					return
				}
			}
			key := routeKey(namespace, routeName)
			msg := fmt.Sprintf("external certificate validation failed: secret %q deleted for route %q", secret.Name, key)
			log.V(4).Info(msg)
			routeapihelpers.InvalidateAsyncSARCache(namespace, secret.Name)

			// keep the secret monitor active and mark the secret as deleted for this route.
			p.deletedSecrets.Store(key, true)

			// Ensure fetching the updated route and DeepCopy to avoid
			// reading/writing the shared informer cache object.
			route, err := p.routelister.Routes(namespace).Get(routeName)
			if err != nil {
				log.Error(err, "failed to get route", "namespace", namespace, "route", routeName)
				return
			}
			route = route.DeepCopy()

			// Reject this route
			p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonValidationFailed, msg)
		},
	}
}

// validate checks the route's external certificate configuration.
// If the validation fails, it records the route rejection and triggers
// the deletion of the route by calling the HandleRoute method with a watch.Deleted event.
//
// This function is synchronous: it blocks until the SAR check completes.
// Concurrency is throttled by the semaphore in ValidateTLSExternalCertificate.
//
// NOTE: TLS data validation and sanitization are handled by the next plugin `ExtendedValidator`,
// by reading the "tls.crt" and "tls.key" added by populateRouteTLSFromSecret.
func (p *RouteSecretManager) validate(route *routev1.Route) error {
	fldPath := field.NewPath("spec").Child("tls").Child("externalCertificate")

	if err := routeapihelpers.ValidateTLSExternalCertificate(route, fldPath, p.sarClient, p.secretsGetter).ToAggregate(); err != nil {
		log.Error(err, "skipping route due to invalid externalCertificate configuration", "namespace", route.Namespace, "route", route.Name)
		p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonValidationFailed, err.Error())
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
	// read referenced secret from the informer cache.
	// GetSecret attempts to read from the cache and falls back to a direct API
	// call if the cache is not synced or the secret is not found.
	secret, err := p.secretManager.GetSecret(context.TODO(), route.Namespace, route.Name)
	if err != nil {
		log.Error(err, "failed to get referenced secret")
		p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonGetFailed, err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return err
	}

	// Update the tls.Certificate and tls.Key fields of the route with the data from the referenced secret.
	// Since externalCertificate does not contain the CACertificate, tls.CACertificate will not be updated.
	// NOTE that this update is only performed in-memory and will not reflect in the actual route resource stored in etcd, because
	// the router does not make kube-client calls to directly update route resources.
	route.Spec.TLS.Certificate = string(secret.Data["tls.crt"])
	route.Spec.TLS.Key = string(secret.Data["tls.key"])

	// Stamp the secret's ResourceVersion onto the route as an in-memory
	// annotation so that templateRouter.AddRoute can use it as a
	// staleness guard (see CertResourceVersion on ServiceAliasConfig).
	if route.Annotations == nil {
		route.Annotations = make(map[string]string)
	}
	route.Annotations[certResourceVersionAnnotation] = secret.ResourceVersion

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
	p.deletedSecrets.Delete(routeKey(route.Namespace, route.Name))
	return nil
}

// hasExternalCertificate checks whether the given route has an externalCertificate specified.
func hasExternalCertificate(route *routev1.Route) bool {
	tls := route.Spec.TLS
	return tls != nil && tls.ExternalCertificate != nil && len(tls.ExternalCertificate.Name) > 0
}

func routeKey(namespace, routeName string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: routeName}
}
