package controller

import (
	"fmt"
	"net"

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

	// extendedRouteValidation enables extended route validation checks.
	extendedRouteValidation bool
}

// NewExtendedValidator creates a plugin wrapper that ensures only routes and
// endpoints that pass validation are relayed to the next plugin in the chain.
// Endpoint address validation is always enabled. Route validation is
// controlled by extendedRouteValidation.
func NewExtendedValidator(plugin router.Plugin, recorder RouteStatusRecorder, extendedRouteValidation bool) *ExtendedValidator {
	return &ExtendedValidator{
		plugin:                  plugin,
		recorder:                recorder,
		extendedRouteValidation: extendedRouteValidation,
	}
}

// HandleNode processes watch events on the node resource
func (p *ExtendedValidator) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return p.plugin.HandleNode(eventType, node)
}

// HandleEndpoints processes watch events on the Endpoints resource.
// Addresses that fail IP validation are removed individually rather
// than rejecting the entire endpoint set.
func (p *ExtendedValidator) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	nsName := fmt.Sprintf("%s/%s", endpoints.Namespace, endpoints.Name)

	// Build filtered subsets without mutating the original, which
	// may be an informer cache object on the legacy Endpoints path.
	ep := endpoints.DeepCopy()
	for i, subset := range ep.Subsets {
		ep.Subsets[i].Addresses = filterValidAddresses(subset.Addresses, nsName)
		ep.Subsets[i].NotReadyAddresses = filterValidAddresses(subset.NotReadyAddresses, nsName)
	}
	return p.plugin.HandleEndpoints(eventType, ep)
}

func filterValidAddresses(addrs []kapi.EndpointAddress, nsName string) []kapi.EndpointAddress {
	valid := make([]kapi.EndpointAddress, 0, len(addrs))
	for _, addr := range addrs {
		if err := validateEndpointAddress(addr.IP); err != nil {
			log.Error(err, "Skipping endpoint address with restricted IP", "endpoints", nsName, "address", addr.IP)
			continue
		}
		valid = append(valid, addr)
	}
	return valid
}

func validateEndpointAddress(address string) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("address %q is not a valid IP", address)
	}
	return checkRestrictedIP(ip)
}

var (
	azureMetadata = net.ParseIP("168.63.129.16")
	awsIPv6IMDS   = net.ParseIP("fd00:ec2::254")
)

func checkRestrictedIP(ip net.IP) error {
	if ip.IsUnspecified() {
		return fmt.Errorf("IP address %s is a restricted unspecified IP", ip.String())
	}
	if ip.IsLoopback() {
		return fmt.Errorf("IP address %s is a restricted loopback IP", ip.String())
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("IP address %s is a restricted link-local IP", ip.String())
	}
	if ip.IsMulticast() {
		return fmt.Errorf("IP address %s is a restricted multicast IP", ip.String())
	}
	if ip.Equal(azureMetadata) {
		return fmt.Errorf("IP address %s is a restricted cloud metadata IP", ip.String())
	}
	if ip.Equal(awsIPv6IMDS) {
		return fmt.Errorf("IP address %s is a restricted cloud metadata IP", ip.String())
	}
	return nil
}

// HandleRoute processes watch events on the Route resource.
func (p *ExtendedValidator) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	log.V(10).Info("HandleRoute: ExtendedValidator")

	if p.extendedRouteValidation {
		routeName := routeNameKey(route)
		if err := routeapihelpers.ExtendedValidateRoute(route).ToAggregate(); err != nil {
			log.Error(err, "skipping route due to invalid configuration", "route", routeName)

			p.recorder.RecordRouteRejection(route, "ExtendedValidationFailed", err.Error())
			p.plugin.HandleRoute(watch.Deleted, route)
			return fmt.Errorf("invalid route configuration")
		}
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
