package controller

import (
	"fmt"

	kapi "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/routeapihelpers"
)

// ExtendedValidator implements the router.Plugin interface to provide
// extended config validation for template based, backend-agnostic routers.
type ExtendedValidator struct {
	// plugin is the next plugin in the chain.
	plugin router.Plugin

	// recorder is an interface for indicating route status.
	recorder RouteStatusRecorder
}

// NewExtendedValidator creates a plugin wrapper that ensures only routes that
// pass extended validation are relayed to the next plugin in the chain.
// Recorder is an interface for indicating route status updates.
func NewExtendedValidator(plugin router.Plugin, recorder RouteStatusRecorder) *ExtendedValidator {
	return &ExtendedValidator{
		plugin:   plugin,
		recorder: recorder,
	}
}

// HandleNode processes watch events on the node resource
func (p *ExtendedValidator) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return p.plugin.HandleNode(eventType, node)
}

// HandleEndpoints processes watch events on the Endpoints resource.
func (p *ExtendedValidator) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	return p.plugin.HandleEndpoints(eventType, endpoints)
}

// HandleRoute processes watch events on the Route resource.
func (p *ExtendedValidator) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	// Check if previously seen route and its Spec is unchanged.
	routeName := routeNameKey(route)
	if err := routeapihelpers.ExtendedValidateRoute(route).ToAggregate(); err != nil {
		log.Error(err, "skipping route due to invalid configuration", "route", routeName)

		p.recorder.RecordRouteRejection(route, "ExtendedValidationFailed", err.Error())
		p.plugin.HandleRoute(watch.Deleted, route)
		return fmt.Errorf("invalid route configuration")
	}

	return p.plugin.HandleRoute(eventType, route)
}

// HandleNamespaces limits the scope of valid routes to only those that match
// the provided namespace list.
func (p *ExtendedValidator) HandleNamespaces(namespaces sets.String) error {
	return p.plugin.HandleNamespaces(namespaces)
}

func (p *ExtendedValidator) Commit() error {
	return p.plugin.Commit()
}
