package controller

import (
	"context"
	"fmt"
	"net"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kapi "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
)

type recordingPlugin struct {
	endpointEvents []endpointEvent
}

type endpointEvent struct {
	eventType watch.EventType
	endpoints *kapi.Endpoints
}

func (p *recordingPlugin) HandleRoute(watch.EventType, *routev1.Route) error { return nil }
func (p *recordingPlugin) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	p.endpointEvents = append(p.endpointEvents, endpointEvent{eventType: eventType, endpoints: endpoints})
	return nil
}
func (p *recordingPlugin) HandleNamespaces(sets.String) error           { return nil }
func (p *recordingPlugin) HandleNode(watch.EventType, *kapi.Node) error { return nil }
func (p *recordingPlugin) Commit() error                                { return nil }

type mockResolver struct {
	results map[string][]net.IP
	errors  map[string]error
}

func (r *mockResolver) ResolveEndpointAddress(_ context.Context, hostname string) ([]net.IP, error) {
	if err, ok := r.errors[hostname]; ok {
		return nil, err
	}
	if ips, ok := r.results[hostname]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no such host: %s", hostname)
}

func TestHandleEndpointSlice_FQDNResolution(t *testing.T) {
	ipv4Type := discoveryv1.AddressTypeIPv4
	ipv6Type := discoveryv1.AddressTypeIPv6
	fqdnType := discoveryv1.AddressTypeFQDN

	tests := []struct {
		name          string
		resolver      EndpointResolver
		items         []discoveryv1.EndpointSlice
		expectedAddrs []string
	}{
		{
			name:     "IPv4 slices pass through unchanged",
			resolver: &mockResolver{},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: ipv4Type,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"10.0.0.1"},
				}},
			}},
			expectedAddrs: []string{"10.0.0.1"},
		},
		{
			name:     "IPv6 slices pass through unchanged",
			resolver: &mockResolver{},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: ipv6Type,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"2001:db8::1"},
				}},
			}},
			expectedAddrs: []string{"2001:db8::1"},
		},
		{
			name: "FQDN resolved to single IP",
			resolver: &mockResolver{
				results: map[string][]net.IP{
					"service.example.com": {net.ParseIP("93.184.216.34")},
				},
			},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: fqdnType,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"service.example.com"},
				}},
			}},
			expectedAddrs: []string{"93.184.216.34"},
		},
		{
			name: "FQDN resolved to multiple IPs with IPv6 first",
			resolver: &mockResolver{
				results: map[string][]net.IP{
					"multi.example.com": {net.ParseIP("2001:db8::1"), net.ParseIP("10.0.0.1")},
				},
			},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: fqdnType,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"multi.example.com"},
				}},
			}},
			expectedAddrs: []string{"2001:db8::1", "10.0.0.1"},
		},
		{
			name: "FQDN resolution failure skips address",
			resolver: &mockResolver{
				errors: map[string]error{
					"fail.example.com": fmt.Errorf("DNS resolution failed"),
				},
			},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: fqdnType,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"fail.example.com"},
				}},
			}},
			expectedAddrs: nil,
		},
		{
			name: "mixed IPv4 and FQDN slices",
			resolver: &mockResolver{
				results: map[string][]net.IP{
					"service.example.com": {net.ParseIP("93.184.216.34")},
				},
			},
			items: []discoveryv1.EndpointSlice{
				{
					ObjectMeta:  metav1.ObjectMeta{Name: "slice-ipv4", Namespace: "ns"},
					AddressType: ipv4Type,
					Endpoints: []discoveryv1.Endpoint{{
						Addresses: []string{"10.0.0.1"},
					}},
				},
				{
					ObjectMeta:  metav1.ObjectMeta{Name: "slice-fqdn", Namespace: "ns"},
					AddressType: fqdnType,
					Endpoints: []discoveryv1.Endpoint{{
						Addresses: []string{"service.example.com"},
					}},
				},
			},
			expectedAddrs: []string{"10.0.0.1", "93.184.216.34"},
		},
		{
			name: "all FQDN resolution failures results in Modified with empty addresses",
			resolver: &mockResolver{
				errors: map[string]error{
					"a.example.com": fmt.Errorf("DNS error"),
					"b.example.com": fmt.Errorf("DNS error"),
				},
			},
			items: []discoveryv1.EndpointSlice{
				{
					ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
					AddressType: fqdnType,
					Endpoints: []discoveryv1.Endpoint{{
						Addresses: []string{"a.example.com"},
					}},
				},
				{
					ObjectMeta:  metav1.ObjectMeta{Name: "slice-2", Namespace: "ns"},
					AddressType: fqdnType,
					Endpoints: []discoveryv1.Endpoint{{
						Addresses: []string{"b.example.com"},
					}},
				},
			},
			expectedAddrs: nil,
		},
		{
			name: "FQDN resolving to restricted IP is resolved but will be filtered by ExtendedValidator",
			resolver: &mockResolver{
				results: map[string][]net.IP{
					"evil.example.com": {net.ParseIP("127.0.0.1")},
				},
			},
			items: []discoveryv1.EndpointSlice{{
				ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
				AddressType: fqdnType,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"evil.example.com"},
				}},
			}},
			expectedAddrs: []string{"127.0.0.1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &recordingPlugin{}
			rc := &RouterController{
				Plugin:                 plugin,
				Resolver:               tc.resolver,
				firstSyncDone:          true,
				FilteredNamespaceNames: make(sets.String),
				NamespaceRoutes:        make(map[string]map[string]*routev1.Route),
				NamespaceEndpoints:     make(map[string]map[string]*kapi.Endpoints),
			}

			objMeta := metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "ns",
			}

			rc.HandleEndpointSlice(watch.Added, objMeta, tc.items)

			require.Len(t, plugin.endpointEvents, 1, "expected only 1 endpoint event")

			event := plugin.endpointEvents[0]
			assert.Equal(t, watch.Modified, event.eventType, "unexpected event type")

			var gotAddrs []string
			for _, subset := range event.endpoints.Subsets {
				for _, addr := range subset.Addresses {
					gotAddrs = append(gotAddrs, addr.IP)
				}
			}

			assert.Equal(t, tc.expectedAddrs, gotAddrs, "resolved addresses should match")
		})
	}
}

// TestFQDNToRestrictedIP_DefenseInDepth verifies that when a FQDN
// resolves to a restricted IP, the address is resolved by the
// RouterController but then filtered by the ExtendedValidator before
// reaching the inner plugin.
func TestFQDNToRestrictedIP_DefenseInDepth(t *testing.T) {
	inner := &recordingPlugin{}
	recorder := &fakeTestRecorder{}
	validator := NewExtendedValidator(inner, recorder, true)

	resolver := &mockResolver{
		results: map[string][]net.IP{
			"evil.example.com": {net.ParseIP("127.0.0.1")},
			"good.example.com": {net.ParseIP("93.184.216.34")},
			"mixed.example.com": {
				net.ParseIP("10.0.0.1"),
				net.ParseIP("169.254.169.254"),
			},
		},
	}

	rc := &RouterController{
		Plugin:                 validator,
		Resolver:               resolver,
		firstSyncDone:          true,
		FilteredNamespaceNames: make(sets.String),
		NamespaceRoutes:        make(map[string]map[string]*routev1.Route),
		NamespaceEndpoints:     make(map[string]map[string]*kapi.Endpoints),
	}

	objMeta := metav1.ObjectMeta{Name: "svc", Namespace: "ns"}
	items := []discoveryv1.EndpointSlice{{
		ObjectMeta:  metav1.ObjectMeta{Name: "slice-1", Namespace: "ns"},
		AddressType: discoveryv1.AddressTypeFQDN,
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"evil.example.com"}},
			{Addresses: []string{"good.example.com"}},
			{Addresses: []string{"mixed.example.com"}},
		},
	}}

	rc.HandleEndpointSlice(watch.Added, objMeta, items)

	require.Len(t, inner.endpointEvents, 1, "expected only 1 endpoint event")

	var gotAddrs []string
	for _, subset := range inner.endpointEvents[0].endpoints.Subsets {
		for _, addr := range subset.Addresses {
			gotAddrs = append(gotAddrs, addr.IP)
		}
	}

	// evil.example.com (127.0.0.1) blocked by loopback check
	// good.example.com (93.184.216.34) passes
	// mixed.example.com: 10.0.0.1 passes, 169.254.169.254 blocked by link-local check
	// Order follows FQDN resolution order (no re-sort after resolution)
	expectedAddrs := []string{"93.184.216.34", "10.0.0.1"}
	assert.Equal(t, expectedAddrs, gotAddrs)
}
