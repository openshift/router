package endpointsubset_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/openshift/router/pkg/router/controller/endpointsubset"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// int32Ptr returns a pointer to an int32
func int32Ptr(i int32) *int32 {
	return &i
}

// boolPtr returns a pointer to a bool
func boolPtr(v bool) *bool {
	return &v
}

func TestConvertEndpointSlice(t *testing.T) {
	tests := []struct {
		name       string
		want       []v1.EndpointSubset
		conditions discoveryv1.EndpointConditions
	}{{
		name: "no Ready condition set, expect zero NotReadyAddresses",
		conditions: discoveryv1.EndpointConditions{
			Ready: nil,
		},
		want: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{
				IP: "192.168.0.1",
			}},
			NotReadyAddresses: nil,
			Ports: []v1.EndpointPort{{
				Port: 8080,
			}},
		}},
	}, {
		name: "Ready condition set to true, expect zero NotReadyAddresses",
		conditions: discoveryv1.EndpointConditions{
			Ready: boolPtr(true),
		},
		want: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{
				IP: "192.168.0.1",
			}},
			NotReadyAddresses: nil,
			Ports: []v1.EndpointPort{{
				Port: 8080,
			}},
		}},
	}, {
		name: "Ready condition set to false, expect zero ReadyAddresses and non-zero NotReadyAddresses",
		conditions: discoveryv1.EndpointConditions{
			Ready: boolPtr(false),
		},
		want: []v1.EndpointSubset{{
			Addresses: nil,
			NotReadyAddresses: []v1.EndpointAddress{{
				IP: "192.168.0.1",
			}},
			Ports: []v1.EndpointPort{{
				Port: 8080,
			}},
		}},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := []discoveryv1.EndpointSlice{{
				TypeMeta: metav1.TypeMeta{
					Kind:       "EndpointSlice",
					APIVersion: "discovery.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "slice-1",
					Namespace: "namespace-a",
					Labels: map[string]string{
						discoveryv1.LabelServiceName: "service-a",
					},
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints: []discoveryv1.Endpoint{{
					Addresses: []string{
						"192.168.0.1",
					},
					Conditions: tc.conditions,
				}},
				Ports: []discoveryv1.EndpointPort{{
					Port: int32Ptr(8080),
				}},
			}}

			got := endpointsubset.ConvertEndpointSlice(items, endpointsubset.DefaultEndpointAddressOrderByFuncs(), endpointsubset.DefaultEndpointPortOrderByFuncs())
			if diff := cmp.Diff(got, tc.want); len(diff) != 0 {
				t.Errorf("ConvertEndpointSlice() failed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConvertEndpointSlice_addressTypes(t *testing.T) {
	serviceLabels := map[string]string{
		discoveryv1.LabelServiceName: "service-a",
	}
	sliceMeta := metav1.TypeMeta{
		Kind:       "EndpointSlice",
		APIVersion: "discovery.k8s.io/v1",
	}

	tests := []struct {
		name  string
		items []discoveryv1.EndpointSlice
		want  []v1.EndpointSubset
	}{{
		name: "FQDN AddressType is skipped",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
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
	}, {
		name: "unknown AddressType is skipped",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-unknown",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressType("Unknown"),
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.2"},
			}},
		}},
	}, {
		name: "empty AddressType is skipped",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-empty-type",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.3"},
			}},
		}},
	}, {
		name: "IPv6 AddressType is converted",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-ipv6",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressTypeIPv6,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"2001:db8::1"},
			}},
			Ports: []discoveryv1.EndpointPort{{
				Port: int32Ptr(443),
			}},
		}},
		want: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{IP: "2001:db8::1"}},
			Ports:     []v1.EndpointPort{{Port: 443}},
		}},
	}, {
		name: "IPv4 slice with hostname in addresses passes through conversion",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-ipv4-hostname",
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
		want: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{
				{IP: "metadata.google.internal"},
				{IP: "10.0.0.1"},
			},
			Ports: []v1.EndpointPort{{Port: 8080}},
		}},
	}, {
		name: "mixed supported and unsupported slices",
		items: []discoveryv1.EndpointSlice{{
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-fqdn",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressTypeFQDN,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"metadata.google.internal"},
			}},
		}, {
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-unknown",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressType("Unknown"),
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.2"},
			}},
		}, {
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-empty-type",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.3"},
			}},
		}, {
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-ipv4",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"10.0.0.1"},
			}},
			Ports: []discoveryv1.EndpointPort{{
				Port: int32Ptr(80),
			}},
		}, {
			TypeMeta: sliceMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "slice-ipv6",
				Namespace: "namespace-a",
				Labels:    serviceLabels,
			},
			AddressType: discoveryv1.AddressTypeIPv6,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses: []string{"2001:db8::1"},
			}},
			Ports: []discoveryv1.EndpointPort{{
				Port: int32Ptr(443),
			}},
		}},
		want: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{IP: "10.0.0.1"}},
			Ports:     []v1.EndpointPort{{Port: 80}},
		}, {
			Addresses: []v1.EndpointAddress{{IP: "2001:db8::1"}},
			Ports:     []v1.EndpointPort{{Port: 443}},
		}},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := endpointsubset.ConvertEndpointSlice(
				tc.items,
				endpointsubset.DefaultEndpointAddressOrderByFuncs(),
				endpointsubset.DefaultEndpointPortOrderByFuncs(),
			)
			if diff := cmp.Diff(tc.want, got); len(diff) != 0 {
				t.Errorf("ConvertEndpointSlice() failed (-want +got):\n%s", diff)
			}
		})
	}
}
