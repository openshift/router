package controller

import (
	"fmt"
	"net/netip"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kapi "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/openshift/router/pkg/router/controller/endpointsubset"
)

type fakeTestPlugin struct {
	endpoints []*kapi.Endpoints
}

func (p *fakeTestPlugin) HandleRoute(watch.EventType, *routev1.Route) error { return nil }
func (p *fakeTestPlugin) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	p.endpoints = append(p.endpoints, endpoints)
	return nil
}
func (p *fakeTestPlugin) HandleNamespaces(sets.String) error           { return nil }
func (p *fakeTestPlugin) HandleNode(watch.EventType, *kapi.Node) error { return nil }
func (p *fakeTestPlugin) Commit() error                                { return nil }

type fakeTestRecorder struct {
	rejections []string
}

func (r *fakeTestRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	r.rejections = append(r.rejections, fmt.Sprintf("%s: %s", reason, message))
}
func (r *fakeTestRecorder) RecordRouteUpdate(route *routev1.Route, reason, message string) {}
func (r *fakeTestRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
}
func (r *fakeTestRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {}

func Test_checkRestrictedIP(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		expectError bool
	}{
		{
			name:        "valid public IPv4",
			ip:          "1.2.3.4",
			expectError: false,
		},
		{
			name:        "valid private IPv4",
			ip:          "10.0.0.1",
			expectError: false,
		},
		{
			name:        "loopback IPv4",
			ip:          "127.0.0.1",
			expectError: true,
		},
		{
			name:        "loopback IPv4 inside 127.0.0.0/8",
			ip:          "127.127.127.222",
			expectError: true,
		},
		{
			name:        "loopback IPv6",
			ip:          "::1",
			expectError: true,
		},
		{
			name:        "link-local IPv4 metadata",
			ip:          "169.254.169.254",
			expectError: true,
		},
		{
			name:        "link-local IPv4 other",
			ip:          "169.254.1.1",
			expectError: true,
		},
		{
			name:        "Azure metadata IP",
			ip:          "168.63.129.16",
			expectError: true,
		},
		{
			name:        "valid IPv6",
			ip:          "2001:db8::1",
			expectError: false,
		},
		{
			name:        "link-local IPv6",
			ip:          "fe80::1",
			expectError: true,
		},
		{
			name:        "unspecified IPv4",
			ip:          "0.0.0.0",
			expectError: true,
		},
		{
			name:        "unspecified IPv6",
			ip:          "::",
			expectError: true,
		},
		{
			name:        "AWS IPv6 IMDS",
			ip:          "fd00:ec2::254",
			expectError: true,
		},
		{
			name:        "link-local multicast IPv6",
			ip:          "ff02::1",
			expectError: true,
		},
		{
			name:        "link-local multicast IPv4",
			ip:          "224.0.0.1",
			expectError: true,
		},
		{
			name:        "non-link-local multicast IPv4",
			ip:          "239.255.255.250",
			expectError: true,
		},
		{
			name:        "IPv4-mapped IPv6 loopback",
			ip:          "::ffff:127.0.0.1",
			expectError: true,
		},
		{
			name:        "IPv4-mapped IPv6 metadata link-local",
			ip:          "::ffff:169.254.169.254",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 metadata (canonical)",
			ip:          "::a9fe:a9fe",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 metadata (dotted)",
			ip:          "::169.254.169.254",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 loopback",
			ip:          "::127.0.0.1",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 loopback (hex)",
			ip:          "::7f00:1",
			expectError: true,
		},
		{
			name:        "loopback IPv4 from within 127/8",
			ip:          "127.172.127.172",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 loopback from within 127/8 (dotted)",
			ip:          "::127.172.127.172",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 loopback from within 127/8 (hex)",
			ip:          "::7fac:7fac",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := netip.ParseAddr(tc.ip)
			require.NoErrorf(t, err, "failed to parse IP address %s", tc.ip)
			err = checkRestrictedIP(addr)
			if tc.expectError {
				require.Error(t, err)
			}
			if !tc.expectError {
				require.NoError(t, err)
			}
		})
	}
}

func Test_validateEndpointAddress(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		expectError bool
	}{
		{
			name:        "valid private network IPv4 address",
			address:     "10.0.0.1",
			expectError: false,
		},
		{
			name:        "valid private network IPv4 address",
			address:     "23.206.60.92",
			expectError: false,
		},
		{
			name:        "valid IPv6",
			address:     "2001:db8::1",
			expectError: false,
		},
		{
			name:        "restricted loopback IP",
			address:     "127.0.0.1",
			expectError: true,
		},
		{
			name:        "restricted loopback IP from within the range",
			address:     "127.0.127.1",
			expectError: true,
		}, {
			name:        "restricted link-local IP",
			address:     "169.254.169.254",
			expectError: true,
		},
		{
			name:        "restricted Azure metadata IP",
			address:     "168.63.129.16",
			expectError: true,
		},
		{
			name:        "unspecified IPv4",
			address:     "0.0.0.0",
			expectError: true,
		},
		{
			name:        "non-IP address rejected",
			address:     "evil.example.com",
			expectError: true,
		},
		{
			name:        "empty string rejected",
			address:     "",
			expectError: true,
		},
		{
			name:        "IPv4-mapped IPv6 loopback",
			address:     "::ffff:127.0.0.1",
			expectError: true,
		},
		{
			name:        "IPv4-mapped IPv6 metadata link-local",
			address:     "::ffff:169.254.169.254",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 metadata (canonical)",
			address:     "::a9fe:a9fe",
			expectError: true,
		},
		{
			name:        "IPv4-compatible IPv6 loopback",
			address:     "::127.0.0.1",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEndpointAddress(tc.address)
			if tc.expectError {
				require.Error(t, err)
			}
			if !tc.expectError {
				require.NoError(t, err)
			}
		})
	}
}

func TestExtendedValidator_HandleEndpoints(t *testing.T) {
	tests := []struct {
		name              string
		endpoints         *kapi.Endpoints
		expectedEndpoints *kapi.Endpoints
	}{
		{
			name: "valid IP passes through",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "1.2.3.4"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "1.2.3.4"}},
					},
				},
			},
		},
		{
			name: "restricted link-local IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "169.254.169.254"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{},
					},
				},
			},
		},
		{
			name: "restricted loopback IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "127.0.0.1"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{},
					},
				},
			},
		},
		{
			name: "restricted IP in NotReadyAddresses filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{{IP: "169.254.169.254"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{},
					},
				},
			},
		},
		{
			name: "valid IP in NotReadyAddresses passes through",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{{IP: "10.0.0.5"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{{IP: "10.0.0.5"}},
					},
				},
			},
		},
		{
			name: "mixed valid and restricted keeps only valid",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
					},
					{
						Addresses: []kapi.EndpointAddress{{IP: "127.0.0.1"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
					},
					{
						Addresses: []kapi.EndpointAddress{},
					},
				},
			},
		},
		{
			name: "Azure metadata IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "168.63.129.16"}},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{},
					},
				},
			},
		},
		{
			name: "mixed valid and restricted in same subset",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{
							{IP: "10.0.0.1"},
							{IP: "127.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{
							{IP: "10.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			},
		},
		{
			name: "non-IP hostname filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{
						{IP: "10.0.0.1"},
						{IP: "metadata.google.internal"},
					},
				}},
			},
			expectedEndpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := &fakeTestPlugin{}
			recorder := &fakeTestRecorder{}
			validator := NewExtendedValidator(inner, recorder, true)

			err := validator.HandleEndpoints(watch.Added, tc.endpoints)
			require.NoError(t, err)
			require.Len(t, inner.endpoints, 1)

			assert.Equal(t, tc.expectedEndpoints, inner.endpoints[0], "expected endpoints should match inner endpoints")
		})
	}
}

func TestExtendedValidator_EndpointValidationWithRouteValidationDisabled(t *testing.T) {
	inner := &fakeTestPlugin{}
	recorder := &fakeTestRecorder{}
	validator := NewExtendedValidator(inner, recorder, false)

	endpoints := &kapi.Endpoints{
		Subsets: []kapi.EndpointSubset{{
			Addresses: []kapi.EndpointAddress{
				{IP: "10.0.0.1"},
				{IP: "127.0.0.1"},
			},
		}},
	}

	err := validator.HandleEndpoints(watch.Added, endpoints)
	require.NoError(t, err)
	require.Len(t, inner.endpoints, 1)

	expected := &kapi.Endpoints{
		Subsets: []kapi.EndpointSubset{{
			Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
		}},
	}
	assert.Equal(t, expected, inner.endpoints[0], "loopback should be filtered even with route validation disabled")
}

func TestExtendedValidator_HandleEndpoints_DoesNotMutateInput(t *testing.T) {
	inner := &fakeTestPlugin{}
	recorder := &fakeTestRecorder{}
	validator := NewExtendedValidator(inner, recorder, true)

	addresses := []kapi.EndpointAddress{
		{IP: "10.0.0.1"},
		{IP: "127.0.0.1"},
		{IP: "10.0.0.2"},
	}
	notReadyAddresses := []kapi.EndpointAddress{
		{IP: "169.254.169.254"},
	}
	endpoints := &kapi.Endpoints{
		Subsets: []kapi.EndpointSubset{
			{
				Addresses:         addresses,
				NotReadyAddresses: notReadyAddresses,
			},
		},
	}

	originalAddresses := append([]kapi.EndpointAddress{}, addresses...)
	originalNotReadyAddresses := append([]kapi.EndpointAddress{}, notReadyAddresses...)

	err := validator.HandleEndpoints(watch.Added, endpoints)
	require.NoError(t, err)
	require.Len(t, inner.endpoints, 1)

	// The caller's Endpoints object, and the backing arrays of its address
	// slices, must be left untouched: HandleEndpoints may be called with an
	// object shared with (and owned by) an informer's cache.
	assert.Equal(t, originalAddresses, endpoints.Subsets[0].Addresses, "input Addresses must not be mutated")
	assert.Equal(t, originalNotReadyAddresses, endpoints.Subsets[0].NotReadyAddresses, "input NotReadyAddresses must not be mutated")
	assert.Len(t, endpoints.Subsets[0].Addresses, 3, "input Addresses length must be unchanged")

	// The plugin further down the chain must still observe the filtered result.
	assert.Equal(t, []kapi.EndpointAddress{{IP: "10.0.0.1"}, {IP: "10.0.0.2"}}, inner.endpoints[0].Subsets[0].Addresses)
	assert.Equal(t, []kapi.EndpointAddress{}, inner.endpoints[0].Subsets[0].NotReadyAddresses)
}

func TestExtendedValidator_HandleEndpoints_DeletedSkipsFiltering(t *testing.T) {
	inner := &fakeTestPlugin{}
	recorder := &fakeTestRecorder{}
	validator := NewExtendedValidator(inner, recorder, true)

	endpoints := &kapi.Endpoints{
		Subsets: []kapi.EndpointSubset{{
			Addresses: []kapi.EndpointAddress{{IP: "127.0.0.1"}},
		}},
	}

	err := validator.HandleEndpoints(watch.Deleted, endpoints)
	require.NoError(t, err)
	require.Len(t, inner.endpoints, 1)
	assert.Same(t, endpoints, inner.endpoints[0])
	assert.Equal(t, []kapi.EndpointAddress{{IP: "127.0.0.1"}}, inner.endpoints[0].Subsets[0].Addresses)
}

func TestExtendedValidator_HandleEndpointSliceConversion(t *testing.T) {
	serviceLabels := map[string]string{
		discoveryv1.LabelServiceName: "service-a",
	}
	endpointsFromSlices := func(namespace, name string, slices []discoveryv1.EndpointSlice) *kapi.Endpoints {
		return &kapi.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Subsets: endpointsubset.ConvertEndpointSlice(
				slices,
				endpointsubset.DefaultEndpointAddressOrderByFuncs(),
				endpointsubset.DefaultEndpointPortOrderByFuncs(),
			),
		}
	}

	tests := []struct {
		name              string
		slices            []discoveryv1.EndpointSlice
		expectedEndpoints *kapi.Endpoints
	}{
		{
			name: "FQDN EndpointSlice produces no backend addresses",
			slices: []discoveryv1.EndpointSlice{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice-fqdn",
					Namespace: "namespace-a",
					Labels:    serviceLabels,
				},
				AddressType: discoveryv1.AddressTypeFQDN,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"metadata.google.internal"},
				}},
				Ports: []discoveryv1.EndpointPort{{
					Port: int32Ptr(8080),
				}},
			}},
			expectedEndpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace-a",
					Name:      "service-a",
				},
			},
		},
		{
			name: "IPv4 EndpointSlice with hostname keeps only valid IP after validation",
			slices: []discoveryv1.EndpointSlice{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice-ipv4",
					Namespace: "namespace-a",
					Labels:    serviceLabels,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"10.0.0.1", "metadata.google.internal"},
				}},
				Ports: []discoveryv1.EndpointPort{{
					Port: int32Ptr(8080),
				}},
			}},
			expectedEndpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace-a",
					Name:      "service-a",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
					Ports:     []kapi.EndpointPort{{Port: 8080}},
				}},
			},
		},
		{
			name: "IPv6 EndpointSlice with canonical metadata address filtered",
			slices: []discoveryv1.EndpointSlice{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice-ipv6-metadata",
					Namespace: "namespace-a",
					Labels:    serviceLabels,
				},
				AddressType: discoveryv1.AddressTypeIPv6,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{"::a9fe:a9fe"},
				}},
				Ports: []discoveryv1.EndpointPort{{
					Port: int32Ptr(8080),
				}},
			}},
			expectedEndpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "namespace-a",
					Name:      "service-a",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{},
					Ports:     []kapi.EndpointPort{{Port: 8080}},
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := &fakeTestPlugin{}
			recorder := &fakeTestRecorder{}
			validator := NewExtendedValidator(inner, recorder, true)

			endpoints := endpointsFromSlices("namespace-a", "service-a", tc.slices)
			err := validator.HandleEndpoints(watch.Added, endpoints)
			require.NoError(t, err)
			require.Len(t, inner.endpoints, 1)

			assert.Equal(t, tc.expectedEndpoints, inner.endpoints[0])
		})
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
