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
