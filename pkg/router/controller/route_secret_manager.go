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
	"golang.org/x/time/rate"
	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
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

// postAdmissionRecheckDelay gives RBAC and Secret changes time to propagate
// before a newly admitted route using a restricted informer is checked again.
const postAdmissionRecheckDelay = 10 * time.Second

func init() {
	secretUpdateRecheckDelay.Store(int64(3 * time.Second))
}

type routeSecretRetry struct {
	key        types.NamespacedName
	uid        types.UID
	secretName string
}

type routeSecretValidation struct {
	routeSecretRetry
	validated                 bool
	everValidated             bool
	postAdmissionCheckPending bool
	loadedSecretVersion       string
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

	// routeValidation is read and written only while holding the matching
	// route lock. It records whether the current route and secret reference
	// have passed full validation, independently of lagging Route status.
	routeValidation    sync.Map // map[types.NamespacedName]routeSecretValidation
	retryQueue         workqueue.RateLimitingInterface
	postAdmissionQueue workqueue.DelayingInterface
}

// lockRoute acquires the per-route lock for key, creating it on first use,
// and returns a function to release it.
func (p *RouteSecretManager) lockRoute(key types.NamespacedName) func() {
	value, _ := p.routeLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Start runs rate-limited workers for failed initial validation and restricted
// route follow-up checks. Both workers stop with the router.
func (p *RouteSecretManager) Start(stopCh <-chan struct{}) {
	p.retryQueue = workqueue.NewNamedRateLimitingQueue(
		workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(5*time.Second, time.Minute),
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(5, 10)},
		),
		"external-certificate-initial-validation",
	)
	p.postAdmissionQueue = workqueue.NewNamedDelayingQueue("external-certificate-post-admission-check")
	postAdmissionContext, cancelPostAdmissionChecks := context.WithCancel(context.Background())
	go func() {
		<-stopCh
		p.retryQueue.ShutDown()
		cancelPostAdmissionChecks()
		p.postAdmissionQueue.ShutDown()
	}()
	go p.runValidationRetries()
	go p.runPostAdmissionChecks(postAdmissionContext)
}

// runPostAdmissionChecks limits the follow-up API work to five routes per
// second, including when many routes are admitted during router startup.
func (p *RouteSecretManager) runPostAdmissionChecks(ctx context.Context) {
	limiter := rate.NewLimiter(5, 1)
	for {
		item, shutdown := p.postAdmissionQueue.Get()
		if shutdown {
			return
		}
		if err := limiter.Wait(ctx); err == nil {
			p.recheckAdmittedRoute(item.(routeSecretRetry))
		}
		p.postAdmissionQueue.Done(item)
	}
}

// recheckAdmittedRoute performs a fresh SAR and Secret check after initial
// admission. It also recovers Secret changes missed while a restricted
// informer was starting.
func (p *RouteSecretManager) recheckAdmittedRoute(retry routeSecretRetry) {
	unlock := p.lockRoute(retry.key)
	defer unlock()

	stored, ok := p.routeValidation.Load(retry.key)
	if !ok {
		return
	}
	state := stored.(routeSecretValidation)
	if state.routeSecretRetry != retry || !state.postAdmissionCheckPending {
		return
	}
	state.postAdmissionCheckPending = false
	p.routeValidation.Store(retry.key, state)

	route, err := p.routelister.Routes(retry.key.Namespace).Get(retry.key.Name)
	if err != nil || route.UID != retry.uid || !hasExternalCertificate(route) || route.Spec.TLS.ExternalCertificate.Name != retry.secretName {
		return
	}
	if secretName, registered := p.secretManager.LookupRouteSecret(retry.key.Namespace, retry.key.Name); !registered || secretName != retry.secretName {
		return
	}
	if _, deleted := p.deletedSecrets.Load(retry.key); deleted {
		// Reassert a deletion rejection in case its status write lost to an
		// earlier admission write in the writer lease.
		p.markRouteValidationFailed(route)
		p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonValidationFailed, fmt.Sprintf("secret %q was deleted", retry.secretName))
		p.plugin.HandleRoute(watch.Deleted, route)
		if err := p.plugin.Commit(); err != nil {
			log.Error(err, "failed to commit route rejection after Secret deletion", "namespace", retry.key.Namespace, "route", retry.key.Name)
		}
		return
	}

	route = route.DeepCopy()
	routeapihelpers.InvalidateAsyncSARCache(retry.key.Namespace, retry.secretName)
	if err := p.validate(route); err != nil {
		if err := p.plugin.Commit(); err != nil {
			log.Error(err, "failed to commit route rejection after post-admission check", "namespace", retry.key.Namespace, "route", retry.key.Name)
		}
		return
	}

	// A Secret update immediately after Route creation can precede the
	// restricted informer's first list and therefore deliver no update event.
	// Fetch the authoritative version instead of relying on its cache.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	secret, err := p.secretsGetter.Secrets(retry.key.Namespace).Get(ctx, retry.secretName, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "failed to read Secret during post-admission check", "namespace", retry.key.Namespace, "secret", retry.secretName)
		return
	}
	if secret.Type != kapi.SecretTypeTLS {
		routeapihelpers.InvalidateAsyncSARCache(retry.key.Namespace, retry.secretName)
		if err := p.validate(route); err != nil {
			if err := p.plugin.Commit(); err != nil {
				log.Error(err, "failed to commit route rejection after Secret type changed", "namespace", retry.key.Namespace, "route", retry.key.Name)
			}
		}
		return
	}
	if state.validated && secret.ResourceVersion == state.loadedSecretVersion {
		return
	}

	populateRouteTLSFromSecretObject(route, secret)
	if err := p.plugin.HandleRoute(watch.Modified, route); err != nil {
		log.Error(err, "failed to propagate route after post-admission Secret refresh", "namespace", retry.key.Namespace, "route", retry.key.Name)
		return
	}
	p.markRouteValidated(route)
	p.recorder.RecordRouteUpdate(route, ExtCrtStatusReasonSecretUpdated, fmt.Sprintf("revalidated secret %q after route admission", retry.secretName))
	if err := p.plugin.Commit(); err != nil {
		log.Error(err, "failed to commit route after post-admission Secret refresh", "namespace", retry.key.Namespace, "route", retry.key.Name)
	}
}

func (p *RouteSecretManager) runValidationRetries() {
	for {
		item, shutdown := p.retryQueue.Get()
		if shutdown {
			return
		}
		retry := item.(routeSecretRetry)
		p.retryInitialValidation(retry)
		p.retryQueue.Done(item)
	}
}

func (p *RouteSecretManager) retryInitialValidation(retry routeSecretRetry) {
	unlock := p.lockRoute(retry.key)
	defer unlock()

	stored, ok := p.routeValidation.Load(retry.key)
	if !ok || stored.(routeSecretValidation).routeSecretRetry != retry || stored.(routeSecretValidation).everValidated {
		p.retryQueue.Forget(retry)
		return
	}

	route, err := p.routelister.Routes(retry.key.Namespace).Get(retry.key.Name)
	if err != nil || route.UID != retry.uid || !hasExternalCertificate(route) || route.Spec.TLS.ExternalCertificate.Name != retry.secretName {
		p.retryQueue.Forget(retry)
		return
	}
	if secretName, registered := p.secretManager.LookupRouteSecret(retry.key.Namespace, retry.key.Name); !registered || secretName != retry.secretName {
		p.retryQueue.Forget(retry)
		return
	}
	if _, deleted := p.deletedSecrets.Load(retry.key); deleted {
		p.retryQueue.Forget(retry)
		return
	}

	route = route.DeepCopy()
	routeapihelpers.InvalidateAsyncSARCache(route.Namespace, retry.secretName)
	if err := p.validate(route); err != nil {
		return
	}
	if err := p.populateRouteTLSFromSecret(route); err != nil {
		return
	}
	if err := p.plugin.HandleRoute(watch.Modified, route); err != nil {
		log.Error(err, "failed to propagate route after initial external certificate retry", "namespace", route.Namespace, "route", route.Name)
		p.retryQueue.AddRateLimited(retry)
		return
	}
	p.markRouteValidated(route)
	msg := fmt.Sprintf("SAR check and secret load completed for secret %q", retry.secretName)
	p.recorder.RecordRouteUpdate(route, ExtCrtStatusReasonSARCompleted, msg)
	if err := p.plugin.Commit(); err != nil {
		log.Error(err, "failed to commit route after initial external certificate retry", "namespace", route.Namespace, "route", route.Name)
	}
	p.retryQueue.Forget(retry)
}

func (p *RouteSecretManager) markRouteValidationFailed(route *routev1.Route) {
	key := routeKey(route.Namespace, route.Name)
	stored, ok := p.routeValidation.Load(key)
	if !ok {
		return
	}
	state := stored.(routeSecretValidation)
	if state.uid != route.UID || !hasExternalCertificate(route) || state.secretName != route.Spec.TLS.ExternalCertificate.Name {
		return
	}
	state.validated = false
	p.routeValidation.Store(key, state)
	if !state.everValidated && p.retryQueue != nil {
		p.retryQueue.AddRateLimited(state.routeSecretRetry)
	}
}

func (p *RouteSecretManager) markRouteValidated(route *routev1.Route) {
	key := routeKey(route.Namespace, route.Name)
	stored, ok := p.routeValidation.Load(key)
	if !ok {
		return
	}
	state := stored.(routeSecretValidation)
	if state.uid != route.UID || !hasExternalCertificate(route) || state.secretName != route.Spec.TLS.ExternalCertificate.Name {
		return
	}
	firstValidation := !state.everValidated
	state.validated = true
	state.everValidated = true
	state.loadedSecretVersion = route.Annotations[certResourceVersionAnnotation]
	// Namespace-wide informers are shared by all routes in a namespace.
	// Avoid one fresh SAR and Secret read per route in large unrestricted
	// namespaces; the missed startup events were in per-Secret informers.
	needsPostAdmissionCheck := firstValidation && p.postAdmissionQueue != nil
	if needsPostAdmissionCheck {
		if shared, ok := p.secretManager.(*SharedSecretManager); ok {
			needsPostAdmissionCheck = shared.RouteUsesRestrictedInformer(route.Namespace, route.Name)
		}
	}
	if needsPostAdmissionCheck {
		state.postAdmissionCheckPending = true
	}
	p.routeValidation.Store(key, state)
	if needsPostAdmissionCheck {
		p.postAdmissionQueue.AddAfter(state.routeSecretRetry, postAdmissionRecheckDelay)
	}
	if p.retryQueue != nil {
		p.retryQueue.Forget(state.routeSecretRetry)
	}
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
	unlock := p.lockRoute(routeKey(route.Namespace, route.Name))
	defer unlock()

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
			if err := p.validateAndRegister(route); err != nil {
				return err
			}
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
				if err := p.validateAndRegister(route); err != nil {
					return err
				}
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
				// Re-validate under the route lock so a Secret update or
				// deletion cannot change the validated state midway through
				// propagation.
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

				// read referenced secret and update TLS certificate and key
				if err := p.populateRouteTLSFromSecret(route); err != nil {
					return err
				}

			}

		case newHasExt && !oldHadExt:
			// New route has externalCertificate, old route did not
			log.V(4).Info("Validating and registering new external certificate", "namespace", route.Namespace, "secret", route.Spec.TLS.ExternalCertificate.Name, "route", route.Name)
			// register with secret monitor
			if err := p.validateAndRegister(route); err != nil {
				return err
			}
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

	// A deletion that won the route lock before this event must remain
	// authoritative even if a stale cache read above found the old Secret.
	if hasExternalCertificate(route) {
		if _, secretDeleted := p.deletedSecrets.Load(routeKey(route.Namespace, route.Name)); secretDeleted {
			p.plugin.HandleRoute(watch.Deleted, route)
			return fmt.Errorf("secret %q was deleted during route validation", route.Spec.TLS.ExternalCertificate.Name)
		}
	}

	// call next plugin
	err := p.plugin.HandleRoute(eventType, route)
	if err == nil && eventType != watch.Deleted && hasExternalCertificate(route) {
		if _, secretDeleted := p.deletedSecrets.Load(routeKey(route.Namespace, route.Name)); !secretDeleted {
			p.markRouteValidated(route)
		}
	}

	// Only emit SARCompleted when validateAndRegister was called in this
	// pass — i.e., on first-time registration or cert change. Skip it on
	// re-validation (Modified with same cert), which would create a
	// re-enqueue feedback loop and can re-admit routes that were rejected
	// by the secret handlers.
	//
	// The route lock also serializes this status write with Secret deletion.
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
// The caller holds the per-route lock through validation and propagation.
func (p *RouteSecretManager) validateAndRegister(route *routev1.Route) error {
	// Register route with secretManager first, so it receives informer
	// events regardless of whether the SAR check below passes.
	handler := p.generateSecretHandler(route)
	if err := p.secretManager.RegisterRoute(context.TODO(), route.Namespace, route.Name, route.Spec.TLS.ExternalCertificate.Name, handler); err != nil {
		return fmt.Errorf("failed to register router: %w", err)
	}
	key := routeKey(route.Namespace, route.Name)
	state := routeSecretValidation{routeSecretRetry: routeSecretRetry{
		key: key, uid: route.UID, secretName: route.Spec.TLS.ExternalCertificate.Name,
	}}
	if stored, ok := p.routeValidation.Load(key); ok {
		previous := stored.(routeSecretValidation)
		if previous.routeSecretRetry == state.routeSecretRetry {
			state.everValidated = previous.everValidated
			state.postAdmissionCheckPending = previous.postAdmissionCheckPending
			state.loadedSecretVersion = previous.loadedSecretVersion
		}
	}
	p.routeValidation.Store(key, state)

	// validate (synchronous, throttled by semaphore)
	if err := p.validate(route); err != nil {
		return err
	}

	// read referenced secret and update TLS certificate and key
	if err := p.populateRouteTLSFromSecret(route); err != nil {
		return err
	}

	return nil
}

// generateSecretHandler creates handlers for one route UID and Secret reference.
// Each handler fetches the current route and skips stale callbacks. Secret
// updates refresh previously validated routes immediately, but require full
// validation before admitting a route that is currently rejected.
func (p *RouteSecretManager) generateSecretHandler(registeredRoute *routev1.Route) cache.ResourceEventHandlerFuncs {
	namespace, routeName, uid := registeredRoute.Namespace, registeredRoute.Name, registeredRoute.UID
	secretName := registeredRoute.Spec.TLS.ExternalCertificate.Name
	key := routeKey(namespace, routeName)
	currentRoute := func() *routev1.Route {
		route, err := p.routelister.Routes(namespace).Get(routeName)
		if err != nil {
			log.Error(err, "failed to get route", "namespace", namespace, "route", routeName)
			return nil
		}
		if route.UID != uid || !hasExternalCertificate(route) || route.Spec.TLS.ExternalCertificate.Name != secretName {
			return nil
		}
		return route.DeepCopy()
	}
	// secret handler
	return cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			secret := obj.(*kapi.Secret)
			if secret.Name != secretName {
				return
			}
			unlock := p.lockRoute(key)
			defer unlock()
			route := currentRoute()
			if route == nil {
				return
			}
			log.V(4).Info("Secret added for route", "namespace", namespace, "secret", secret.Name, "route", routeName)
			routeapihelpers.InvalidateAsyncSARCache(namespace, secret.Name)

			// Secret re-creation scenario
			// Check if the route key exists in the deletedSecrets map, indicating that the secret was previously deleted for this route.
			// If it exists, it means the secret is being recreated. Remove the key from the map and proceed with handling the route.
			// Otherwise, no-op (new secret creation scenario and no race condition with that flow)
			// This helps to differentiate between a new secret creation and a re-creation of a previously deleted secret.
			if _, deleted := p.deletedSecrets.LoadAndDelete(key); deleted {
				log.V(4).Info("Secret recreated for route", "namespace", namespace, "secret", secret.Name, "route", routeName)

				// The route should *remain* rejected until it's re-evaluated
				// by all the plugins (including this plugin). Once passes, the route will become active again.
				msg := fmt.Sprintf("secret %q recreated for route %q", secret.Name, key)
				p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonSecretRecreated, msg)
				if stored, ok := p.routeValidation.Load(key); ok {
					state := stored.(routeSecretValidation)
					if !state.everValidated && p.retryQueue != nil {
						p.retryQueue.AddRateLimited(state.routeSecretRetry)
					}
				}
			}
		},

		UpdateFunc: func(old interface{}, new interface{}) {
			secretOld := old.(*kapi.Secret)
			secretNew := new.(*kapi.Secret)
			if secretNew.Name != secretName || (secretOld.UID == secretNew.UID && secretOld.ResourceVersion == secretNew.ResourceVersion) {
				return
			}
			unlock := p.lockRoute(key)
			defer unlock()
			route := currentRoute()
			if route == nil {
				return
			}
			if _, deleted := p.deletedSecrets.Load(key); deleted {
				return
			}
			stored, ok := p.routeValidation.Load(key)
			if !ok || stored.(routeSecretValidation).uid != uid || stored.(routeSecretValidation).secretName != secretName {
				return
			}
			state := stored.(routeSecretValidation)
			log.V(4).Info("Secret updated for route", "namespace", namespace, "secret", secretNew.Name, "oldSecretVersion", secretOld.ResourceVersion, "newSecretVersion", secretNew.ResourceVersion, "route", routeName)
			routeapihelpers.InvalidateAsyncSARCache(namespace, secretNew.Name)

			msg := fmt.Sprintf("secret %q updated for route %q (oldSecretVersion=%v, newSecretVersion=%v)", secretNew.Name, key, secretOld.ResourceVersion, secretNew.ResourceVersion)
			// A route that has never passed validation, or was subsequently
			// rejected, must pass a fresh SAR and Secret check before any
			// status update can admit it. Already validated routes retain
			// the immediate cert refresh and delayed RBAC recheck.
			if !state.validated {
				if err := p.validate(route); err != nil {
					return
				}
			}
			if err := p.populateRouteTLSFromSecret(route); err != nil {
				return
			}
			if err := p.plugin.HandleRoute(watch.Modified, route); err != nil {
				log.Error(err, "failed to propagate route after secret update", "namespace", namespace, "route", routeName)
				return
			}
			p.markRouteValidated(route)
			p.recorder.RecordRouteUpdate(route, ExtCrtStatusReasonSecretUpdated, msg)

			// Trigger a rate-limited HAProxy reload directly instead of
			// depending on the indirect round trip (status write → API
			// server → route informer → RouterController.HandleRoute →
			// Commit), which adds 10-30s under HyperShift conditions.
			if err := p.plugin.Commit(); err != nil {
				log.Error(err, "failed to commit route after secret update", "namespace", namespace, "route", routeName)
			}

			// Schedule a delayed fresh SAR check to catch RBAC revocations
			// that may not have propagated yet. A passing check is a no-op.
			go func() {
				time.Sleep(time.Duration(secretUpdateRecheckDelay.Load()))
				unlock := p.lockRoute(key)
				defer unlock()
				if _, deleted := p.deletedSecrets.Load(key); deleted {
					return
				}
				route := currentRoute()
				if route == nil {
					return
				}
				routeapihelpers.InvalidateAsyncSARCache(namespace, secretNew.Name)
				if err := p.validate(route); err != nil {
					if err := p.plugin.Commit(); err != nil {
						log.Error(err, "failed to commit route rejection after delayed SAR check", "namespace", namespace, "route", routeName)
					}
				}
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
			if secret.Name != secretName {
				return
			}
			msg := fmt.Sprintf("external certificate validation failed: secret %q deleted for route %q", secret.Name, key)
			log.V(4).Info(msg)

			// Serialize the mark-deleted + reject sequence against a
			// concurrent registration's SARCompleted write in HandleRoute's
			// tail, which holds this same per-route lock. Without it, the two
			// steps here are not atomic relative to that tail's
			// deletedSecrets.Load() guard and RecordRouteUpdate(SARCompleted)
			// write: an in-flight registration that loaded deletedSecrets
			// before this handler stored it can still finish afterward and
			// overwrite the rejection below with a stale SARCompleted status,
			// leaving the route admitted with its backing secret already gone.
			// Holding the lock across both makes the rejection authoritative
			// regardless of which goroutine runs second (OCPBUGS-77056).
			unlock := p.lockRoute(key)
			defer unlock()
			route := currentRoute()
			if route == nil {
				return
			}
			routeapihelpers.InvalidateAsyncSARCache(namespace, secret.Name)

			// keep the secret monitor active and mark the secret as deleted for this route.
			p.deletedSecrets.Store(key, true)
			if stored, ok := p.routeValidation.Load(key); ok {
				state := stored.(routeSecretValidation)
				if state.uid == uid && state.secretName == secretName {
					state.validated = false
					p.routeValidation.Store(key, state)
				}
			}

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
		p.markRouteValidationFailed(route)
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
		p.markRouteValidationFailed(route)
		p.recorder.RecordRouteRejection(route, ExtCrtStatusReasonGetFailed, err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return err
	}
	populateRouteTLSFromSecretObject(route, secret)
	return nil
}

// populateRouteTLSFromSecretObject copies a fetched Secret into the in-memory
// Route and stamps its version for the template router's staleness guard.
func populateRouteTLSFromSecretObject(route *routev1.Route, secret *kapi.Secret) {
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
}

// unregister removes the route's registration with the secret manager and ensures
// that any references to the deletedSecrets are cleaned up.
func (p *RouteSecretManager) unregister(route *routev1.Route) error {
	key := routeKey(route.Namespace, route.Name)
	if stored, ok := p.routeValidation.Load(key); ok && p.retryQueue != nil {
		p.retryQueue.Forget(stored.(routeSecretValidation).routeSecretRetry)
	}
	// unregister associated secret monitor
	if err := p.secretManager.UnregisterRoute(route.Namespace, route.Name); err != nil {
		log.Error(err, "failed to unregister route")
		return err
	}
	p.routeValidation.Delete(key)
	// clean the route if present inside deletedSecrets
	// this is required for the scenario when the associated secret is deleted, before unregistering with secretManager
	p.deletedSecrets.Delete(key)
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
