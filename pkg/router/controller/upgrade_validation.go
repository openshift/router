package controller

import (
	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/routeapihelpers"
)

// UpgradeValidation implements the router.Plugin interface to provide
// upgrade route validation.
type UpgradeValidation struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin

	// recorder is an interface for indicating route status.
	recorder RouteStatusRecorder

	// forceAddCondition indicates the plugin should forcibly add the condition.
	forceAddCondition bool

	// forceRemoveCondition indicates the plugin should forcibly remove the condition.
	forceRemoveCondition bool
}

// NewUpgradeValidation creates a plugin wrapper that validates for upgrades
// and adds an UnservableInFutureVersions status if needed. It does not stop
// the plugin chain if the route is unservable in future versions.
// Recorder is an interface for indicating routes status update.
func NewUpgradeValidation(plugin router.Plugin, recorder RouteStatusRecorder, forceAddCondition, forceRemoveCondition bool) *UpgradeValidation {
	return &UpgradeValidation{
		plugin:               plugin,
		recorder:             recorder,
		forceAddCondition:    forceAddCondition,
		forceRemoveCondition: forceRemoveCondition,
	}
}

// HandleNode processes watch events on the node resource
func (p *UpgradeValidation) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return p.plugin.HandleNode(eventType, node)
}

// HandleEndpoints processes watch events on the Endpoints resource.
func (p *UpgradeValidation) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	return p.plugin.HandleEndpoints(eventType, endpoints)
}

// HandleRoute processes watch events on the Route resource.
// It checks if the route is upgradeable to a future version of OpenShift
// and sets UnservableInFutureVersions condition if needed.
func (p *UpgradeValidation) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	log.V(10).Info("HandleRoute: UpgradeValidation")
	routeName := routeNameKey(route)

	// Force add and force removal logic for debugging and testing.
	if p.forceAddCondition {
		log.Info("force adding UnservableInFutureVersions condition in upgrade validation", "conditionType", p.forceRemoveCondition, "route", routeName)
		p.recorder.RecordRouteUnservableInFutureVersions(route, "ForceUpgradeValidationCondition", "forced upgrade validation condition")
		return p.plugin.HandleRoute(eventType, route)
	} else if p.forceRemoveCondition {
		log.Info("force removing UnservableInFutureVersions condition in upgrade validation", "conditionType", p.forceRemoveCondition, "route", routeName)
		p.recorder.RecordRouteUnservableInFutureVersionsClear(route)
		return p.plugin.HandleRoute(eventType, route)
	}

	if err := routeapihelpers.UpgradeRouteValidation(route).ToAggregate(); err != nil {
		log.Error(err, "route failed upgrade validation", "route", routeName)
		p.recorder.RecordRouteUnservableInFutureVersions(route, "UpgradeRouteValidationFailed", err.Error())
	} else {
		p.recorder.RecordRouteUnservableInFutureVersionsClear(route)
	}

	return p.plugin.HandleRoute(eventType, route)
}

// HandleNamespaces processes watch events on namespaces.
func (p *UpgradeValidation) HandleNamespaces(namespaces sets.String) error {
	return p.plugin.HandleNamespaces(namespaces)
}

func (p *UpgradeValidation) Commit() error {
	return p.plugin.Commit()
}
