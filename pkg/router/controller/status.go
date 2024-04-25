package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	client "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/writerlease"
)

const (
	workKeySeparator                      = "_"
	unservableInFutureVersionsAction      = string(routev1.RouteUnservableInFutureVersions)
	unservableInFutureVersionsClearAction = string(routev1.RouteUnservableInFutureVersions) + "-Clear"
)

// RouteStatusRecorder is an object capable of recording why a route status condition changed.
type RouteStatusRecorder interface {
	RecordRouteRejection(route *routev1.Route, reason, message string)
	RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string)
	RecordRouteUnservableInFutureVersionsClear(route *routev1.Route)
}

// LogRejections writes route status change messages to the log.
var LogRejections = logRecorder{}

type logRecorder struct{}

func (logRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	log.V(3).Info("rejected route", "name", route.Name, "namespace", route.Namespace, "reason", reason, "message", message)
}

func (logRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
	log.V(3).Info("route unservable in future versions", "name", route.Name, "namespace", route.Namespace, "reason", reason, "message", message)
}

func (logRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {
	log.V(3).Info("route clear unservable in future versions", "name", route.Name, "namespace", route.Namespace)
}

// StatusAdmitter ensures routes added to the plugin have status set.
type StatusAdmitter struct {
	plugin router.Plugin
	client client.RoutesGetter
	lister routelisters.RouteLister

	routerName              string
	routerCanonicalHostname string

	lease   writerlease.Lease
	tracker ContentionTracker
}

// NewStatusAdmitter creates a plugin wrapper that ensures every accepted
// route has a status field set that matches this router. The admitter manages
// an LRU of recently seen conflicting updates to handle when two router processes
// with differing configurations are writing updates at the same time.
func NewStatusAdmitter(plugin router.Plugin, client client.RoutesGetter, lister routelisters.RouteLister, name, hostName string, lease writerlease.Lease, tracker ContentionTracker) *StatusAdmitter {
	return &StatusAdmitter{
		plugin: plugin,
		client: client,
		lister: lister,

		routerName:              name,
		routerCanonicalHostname: hostName,

		tracker: tracker,
		lease:   lease,
	}
}

// Return a time truncated to the second to ensure that in-memory and
// serialized timestamps can be safely compared.
func getRfc3339Timestamp() metav1.Time {
	return metav1.Now().Rfc3339Copy()
}

// nowFn allows the package to be tested
var nowFn = getRfc3339Timestamp

// HandleRoute attempts to admit the provided route on watch add / modifications.
func (a *StatusAdmitter) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	log.V(10).Info("HandleRoute: StatusAdmitter")
	switch eventType {
	case watch.Added, watch.Modified:
		performIngressConditionUpdate("admit", a.lease, a.tracker, a.client, a.lister, route, a.routerName, a.routerCanonicalHostname, routev1.RouteIngressCondition{
			Type:   routev1.RouteAdmitted,
			Status: corev1.ConditionTrue,
		})
	}
	return a.plugin.HandleRoute(eventType, route)
}

func (a *StatusAdmitter) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return a.plugin.HandleNode(eventType, node)
}

func (a *StatusAdmitter) HandleEndpoints(eventType watch.EventType, route *kapi.Endpoints) error {
	return a.plugin.HandleEndpoints(eventType, route)
}

func (a *StatusAdmitter) HandleNamespaces(namespaces sets.String) error {
	return a.plugin.HandleNamespaces(namespaces)
}

func (a *StatusAdmitter) Commit() error {
	return a.plugin.Commit()
}

// RecordRouteRejection attempts to update the route status with a reason for a route being rejected.
func (a *StatusAdmitter) RecordRouteRejection(route *routev1.Route, reason, message string) {
	performIngressConditionUpdate("reject", a.lease, a.tracker, a.client, a.lister, route, a.routerName, a.routerCanonicalHostname, routev1.RouteIngressCondition{
		Type:    routev1.RouteAdmitted,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

// RecordRouteUnservableInFutureVersions attempts to update the route status with a
// reason for a route being unservable in future versions.
func (a *StatusAdmitter) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
	performIngressConditionUpdate(unservableInFutureVersionsAction, a.lease, a.tracker, a.client, a.lister, route, a.routerName, a.routerCanonicalHostname, routev1.RouteIngressCondition{
		Type:    routev1.RouteUnservableInFutureVersions,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

// RecordRouteUnservableInFutureVersionsClear clears the UnservableInFutureVersions status back to an unset state.
func (a *StatusAdmitter) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {
	performIngressConditionRemoval(unservableInFutureVersionsClearAction, a.lease, a.tracker, a.client, a.lister, route, a.routerName, routev1.RouteUnservableInFutureVersions)
}

// performIngressConditionUpdate updates the route to the appropriate status for the provided condition.
func performIngressConditionUpdate(action string, lease writerlease.Lease, tracker ContentionTracker, oc client.RoutesGetter, lister routelisters.RouteLister, route *routev1.Route, routerName, hostName string, condition routev1.RouteIngressCondition) {
	// Key the lease's work off of the route UID and the condition type, as different conditions will require separate updates.
	workKey := createWorkKey(route.UID, condition.Type)
	routeNamespace, routeName := route.Namespace, route.Name
	oldRouteUID := route.UID

	lease.Try(workKey, func() (writerlease.WorkResult, bool) {
		route, err := lister.Routes(routeNamespace).Get(routeName)
		if err != nil {
			return writerlease.None, false
		}
		if route.UID != oldRouteUID {
			log.V(4).Info("skipped update due to route UID changing (likely delete and recreate)", "action", action, "namespace", route.Namespace, "name", route.Name)
			return writerlease.None, false
		}

		route = route.DeepCopy()
		changed, created, now, latest, original := recordIngressCondition(route, routerName, hostName, condition)
		if !changed {
			log.V(4).Info("no changes to route needed", "action", action, "namespace", route.Namespace, "name", route.Name)
			// if the most recent change was to our ingress status, consider the current lease extended
			if findMostRecentIngress(route) == routerName {
				lease.Extend(workKey)
			}
			return writerlease.None, false
		}

		// If the tracker determines that another process is attempting to update the ingress to an inconsistent
		// value, skip updating altogether and rely on the next resync to resolve conflicts. This prevents routers
		// with different configurations from endlessly updating the route status.
		// TRICKY: The tracker keys off of the route UID, not the workKey.
		if !created && tracker.IsChangeContended(contentionKey(route.UID), now, original) {
			log.V(4).Info("skipped update due to another process altering the route with a different ingress status value", "action", action, "workKey", workKey, "original", original)
			return writerlease.Release, false
		}

		return handleRouteStatusUpdate(context.TODO(), action, oc, route, latest, tracker)
	})
}

// performIngressConditionRemoval removes the provided condition type from the route.
func performIngressConditionRemoval(action string, lease writerlease.Lease, tracker ContentionTracker, oc client.RoutesGetter, lister routelisters.RouteLister, route *routev1.Route, routerName string, condType routev1.RouteIngressConditionType) {
	// Key the lease's work off of the route UID and the condition type, as different conditions will require separate updates.
	workKey := createWorkKey(route.UID, condType)
	routeNamespace, routeName := route.Namespace, route.Name
	oldRouteUID := route.UID

	lease.Try(workKey, func() (writerlease.WorkResult, bool) {
		route, err := lister.Routes(routeNamespace).Get(routeName)
		if err != nil {
			return writerlease.None, false
		}
		if route.UID != oldRouteUID {
			log.V(4).Info("skipped update due to route UID changing (likely delete and recreate)", "action", action, "namespace", route.Namespace, "name", route.Name)
			return writerlease.None, false
		}

		route = route.DeepCopy()
		changed, now, latest, original := removeIngressCondition(route, routerName, condType)
		if !changed {
			log.V(4).Info("no changes to route needed", "action", action, "namespace", route.Namespace, "name", route.Name)
			// Extending the lease ensures a delay in future work ONLY for followers. Unlike in
			// performIngressConditionUpdate, it's not logical to invoke findMostRecentIngress here and expect the last
			// update to be from our router. This is because performIngressConditionRemoval *removes* a condition
			// without providing a LastTransitionTime on a condition for us to track previous actions.
			lease.Extend(workKey)
			return writerlease.None, false
		}

		// If the tracker determines that another process is attempting to update the ingress to an inconsistent
		// value, skip updating altogether and rely on the next resync to resolve conflicts. This prevents routers
		// with different configurations from endlessly updating the route status.
		// TRICKY: The tracker keys off of the route UID, not the workKey.
		if tracker.IsChangeContended(contentionKey(route.UID), now, original) {
			log.V(4).Info("skipped update due to another process altering the route with a different ingress status value", "action", action, "workKey", workKey, "original", original)
			return writerlease.Release, false
		}

		return handleRouteStatusUpdate(context.TODO(), action, oc, route, latest, tracker)
	})
}

// handleRouteStatusUpdate manages the update of route status in conjunction with a writerlease and a tracker. It
// attempts to update the route status and, depending on the outcome, clears the tracker if necessary. It returns the
// writerlease's WorkResult and a boolean flag indicating whether the writerlease should retry.
func handleRouteStatusUpdate(ctx context.Context, action string, oc client.RoutesGetter, route *routev1.Route, latest *routev1.RouteIngress, tracker ContentionTracker) (workResult writerlease.WorkResult, retry bool) {
	switch _, err := oc.Routes(route.Namespace).UpdateStatus(ctx, route, metav1.UpdateOptions{}); {
	case err == nil:
		log.V(4).Info("updated route status", "action", action, "namespace", route.Namespace, "name", route.Name)
		tracker.Clear(contentionKey(route.UID), latest)
		return writerlease.Extend, false
	case errors.IsNotFound(err):
		// route was deleted
		log.V(4).Info("route was deleted before we could update status", "action", action, "namespace", route.Namespace, "name", route.Name)
		return writerlease.Release, false
	case errors.IsConflict(err):
		// just follow the normal process, and retry when we receive the update notification due to
		// the other entity updating the route.
		log.V(4).Info("updating route status failed due to write conflict", "action", action, "namespace", route.Namespace, "name", route.Name)
		return writerlease.Release, true
	default:
		utilruntime.HandleError(fmt.Errorf("Unable to write router status for %s/%s: %v", route.Namespace, route.Name, err))
		return writerlease.Release, true
	}
}

// createWorkKey creates a unique key for the writer lease logic given a route UID and a condition type.
func createWorkKey(uid types.UID, conditionType routev1.RouteIngressConditionType) writerlease.WorkKey {
	return writerlease.WorkKey(string(uid) + workKeySeparator + string(conditionType))
}

// recordIngressCondition updates the matching ingress on the route (or adds a new one) with the specified
// condition. It returns whether the ingress record was updated or created, the time assigned to the
// condition, a pointer to the latest ingress record, and a pointer to a shallow copy of the original ingress
// record.
func recordIngressCondition(route *routev1.Route, name, hostName string, condition routev1.RouteIngressCondition) (changed, created bool, at time.Time, latest, original *routev1.RouteIngress) {
	for i := range route.Status.Ingress {
		existing := &route.Status.Ingress[i]
		if existing.RouterName != name {
			continue
		}

		// check whether the ingress is out of date without modifying it
		changed := existing.Host != route.Spec.Host ||
			existing.WildcardPolicy != route.Spec.WildcardPolicy ||
			existing.RouterCanonicalHostname != hostName

		existingCondition := findCondition(existing, condition.Type)
		if existingCondition != nil {
			condition.LastTransitionTime = existingCondition.LastTransitionTime
			if *existingCondition != condition {
				changed = true
			}
		} else {
			// If the condition we want doesn't exist, then consider it changed.
			changed = true
		}
		if !changed {
			return false, false, time.Time{}, existing, existing
		}

		// preserve a shallow copy of the original ingress
		original := *existing

		// generate the correct ingress
		existing.Host = route.Spec.Host
		existing.WildcardPolicy = route.Spec.WildcardPolicy
		existing.RouterCanonicalHostname = hostName

		// Add or update the condition
		if existingCondition == nil {
			existing.Conditions = append(existing.Conditions, condition)
			existingCondition = &existing.Conditions[len(existing.Conditions)-1]
		} else {
			*existingCondition = condition
		}
		now := nowFn()
		existingCondition.LastTransitionTime = &now

		return true, false, now.Time, existing, &original
	}

	// add a new ingress
	route.Status.Ingress = append(route.Status.Ingress, routev1.RouteIngress{
		RouterName:              name,
		Host:                    route.Spec.Host,
		WildcardPolicy:          route.Spec.WildcardPolicy,
		RouterCanonicalHostname: hostName,
		Conditions: []routev1.RouteIngressCondition{
			condition,
		},
	})
	ingress := &route.Status.Ingress[len(route.Status.Ingress)-1]
	now := nowFn()
	ingress.Conditions[0].LastTransitionTime = &now

	return true, true, now.Time, ingress, nil
}

// removeIngressCondition removes a matching status condition if it is found.
// It returns whether the route was changed, the time it removed the condition, and
// a pointer to the latest and original ingress records.
func removeIngressCondition(route *routev1.Route, name string, condType routev1.RouteIngressConditionType) (changed bool, at time.Time, latest, original *routev1.RouteIngress) {
	for i := range route.Status.Ingress {
		existing := &route.Status.Ingress[i]
		if existing.RouterName != name {
			continue
		}

		// preserve a deep copy of the original ingress
		original := existing.DeepCopy()

		at := time.Time{}

		// Remove the condition if we can find it.
		changed = false
		var updatedConditions []routev1.RouteIngressCondition
		for _, condition := range existing.Conditions {
			if condition.Type != condType {
				updatedConditions = append(updatedConditions, condition)
			} else {
				changed = true
			}
		}

		// Update the conditions slice if any condition was removed.
		if changed {
			at = nowFn().Time
			existing.Conditions = updatedConditions
		}

		return changed, at, existing, original
	}

	return false, time.Time{}, nil, nil
}

// findMostRecentIngress returns the name of the ingress status with the most recent condition transition time,
// or an empty string if no such ingress exists.
func findMostRecentIngress(route *routev1.Route) string {
	var newest string
	var recent time.Time
	for i := range route.Status.Ingress {
		for j := range route.Status.Ingress[i].Conditions {
			condition := &route.Status.Ingress[i].Conditions[j]
			if condition.LastTransitionTime != nil && condition.LastTransitionTime.Time.After(recent) {
				recent = condition.LastTransitionTime.Time
				newest = route.Status.Ingress[i].RouterName
			}
		}
	}
	return newest
}

// findCondition locates the first condition that corresponds to the requested type.
func findCondition(ingress *routev1.RouteIngress, t routev1.RouteIngressConditionType) (_ *routev1.RouteIngressCondition) {
	for i, existing := range ingress.Conditions {
		if existing.Type != t {
			continue
		}
		return &ingress.Conditions[i]
	}
	return nil
}
