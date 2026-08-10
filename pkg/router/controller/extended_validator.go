package controller

import (
	"fmt"
	"net/netip"
	"slices"

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
	if eventType == watch.Deleted {
		return p.plugin.HandleEndpoints(eventType, endpoints)
	}

	// endpoints may be an object shared with (and owned by) an informer's
	// cache, so it must be deep-copied before it is mutated below.
	endpoints = endpoints.DeepCopy()
	for i, subset := range endpoints.Subsets {
		endpoints.Subsets[i].Addresses = filterValidAddresses(subset.Addresses)
		endpoints.Subsets[i].NotReadyAddresses = filterValidAddresses(subset.NotReadyAddresses)
	}
	return p.plugin.HandleEndpoints(eventType, endpoints)
}

func filterValidAddresses(addrs []kapi.EndpointAddress) []kapi.EndpointAddress {
	return slices.DeleteFunc(addrs, func(addr kapi.EndpointAddress) bool {
		err := validateEndpointAddress(addr.IP)
		if err != nil {
			log.Error(err, "Skipping endpoint address with restricted or invalid IP", "address", addr.IP)
		}
		return err != nil
	})
}

func validateEndpointAddress(address string) error {
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return fmt.Errorf("address %q is not a valid IP address", address)
	}
	// EndpointSlice addresses of type IPv4 or IPv6 are validated by the Kubernetes
	// API, which requires canonical IP form and rejects hostnames, non-canonical
	// encodings (such as ::127.0.0.1), and IPv4-mapped IPv6 literals.
	return checkRestrictedIP(addr)
}

var (
	azureMetadata = netip.MustParseAddr("168.63.129.16")
	awsIPv6IMDS   = netip.MustParseAddr("fd00:ec2::254")
)

func ipv4CompatibleTo4(addr netip.Addr) (netip.Addr, bool) {
	if !addr.Is6() || addr.Is4In6() {
		return netip.Addr{}, false
	}
	b := addr.As16()
	for i := 0; i < 12; i++ {
		if b[i] != 0 {
			return netip.Addr{}, false
		}
	}
	return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
}

func normalizeEmbeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	if addr.Is4In6() {
		return addr.Unmap(), true
	}
	if v4, ok := ipv4CompatibleTo4(addr); ok {
		return v4, true
	}
	return addr, false
}

func restrictedProperties(addr netip.Addr) error {
	if addr.IsUnspecified() {
		return fmt.Errorf("IP address %s is a restricted unspecified IP", addr)
	}
	if addr.IsLoopback() {
		return fmt.Errorf("IP address %s is a restricted loopback IP", addr)
	}
	if addr.IsLinkLocalUnicast() {
		return fmt.Errorf("IP address %s is a restricted link-local IP", addr)
	}
	if addr.IsMulticast() {
		return fmt.Errorf("IP address %s is a restricted multicast IP", addr)
	}
	if addr == azureMetadata || addr == awsIPv6IMDS {
		return fmt.Errorf("IP address %s is a restricted cloud metadata IP", addr)
	}
	return nil
}

// checkRestrictedIP rejects addresses that must not be used as router backends.
//
// Validation runs in two phases against a single rule set (restrictedProperties):
//
//  1. Check the address as parsed. This catches native forms directly — plain IPv4
//     (10.0.0.1), native IPv6 loopback (::1), link-local (fe80::1), and cloud-specific
//     IPv6 metadata (fd00:ec2::254). Most addresses, including essentially all plain
//     IPv4 endpoints, are fully validated here and return immediately.
//
//  2. If the address embeds an IPv4 address inside an IPv6 encoding, extract it and
//     check again. IPv4-mapped literals (::ffff:x.x.x.x) are unmapped; IPv4-compatible
//     literals (::x.x.x.x / ::hh:hh:hh:hh, the deprecated ::/96 form) have their low
//     32 bits decoded. The second pass catches restricted destinations that do not look
//     restricted in IPv6 form, such as ::a9fe:a9fe (169.254.169.254 metadata) or
//     ::127.0.0.1 (loopback).
//
// The native check must run before extraction: ::1 also matches the ::/96 layout but
// is IPv6 loopback, not embedded 0.0.0.1; checking after extraction would misclassify
// it and allow it through.
//
// Typical cost: one pass for plain IPv4 and legitimate IPv6; two passes only for
// IPv4-mapped or IPv4-compatible encodings.
func checkRestrictedIP(addr netip.Addr) error {
	if err := restrictedProperties(addr); err != nil {
		return err
	}
	if normalized, ok := normalizeEmbeddedIPv4(addr); ok {
		return restrictedProperties(normalized)
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
