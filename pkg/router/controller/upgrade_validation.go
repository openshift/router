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
}

// NewUpgradeValidation creates a plugin wrapper that validates for upgrades
// and adds a unservableInFutureVersions status if needed. It does not stop
// the plugin chain if the route is unservable in future versions.
// Recorder is an interface for indicating routes status update.
func NewUpgradeValidation(plugin router.Plugin, recorder RouteStatusRecorder) *UpgradeValidation {
	return &UpgradeValidation{
		plugin:   plugin,
		recorder: recorder,
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
func (p *UpgradeValidation) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	// Check if route is upgradeable to a future version of OpenShift
	// and set UnservableInFutureVersions condition if needed.
	routeName := routeNameKey(route)
	if err := routeapihelpers.UpgradeRouteValidation(route).ToAggregate(); err != nil {
		log.Error(err, "route failed upgrade validation", "route", routeName)
		p.recorder.RecordRouteUnservableInFutureVersions(route, "UpgradeRouteValidationFailed", err.Error())
	} else {
		p.recorder.RecordRouteUnservableInFutureVersionsClear(route)
	}

	return p.plugin.HandleRoute(eventType, route)
}

// HandleNamespaces limits the scope of valid routes to only those that match
// the provided namespace list.
func (p *UpgradeValidation) HandleNamespaces(namespaces sets.String) error {
	return p.plugin.HandleNamespaces(namespaces)
}

func (p *UpgradeValidation) Commit() error {
	return p.plugin.Commit()
}
