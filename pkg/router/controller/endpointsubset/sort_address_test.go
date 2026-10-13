package endpointsubset_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/router/pkg/router/controller/endpointsubset"
	kapi "k8s.io/api/core/v1"
)

func TestSortAddresses(t *testing.T) {
	var testcases = []struct {
		description string
		input       []kapi.EndpointAddress
		expected    []kapi.EndpointAddress
	}{{
		description: "empty slice",
	}, {
		description: "IP address sort lowest -> highest",
		input: []kapi.EndpointAddress{{
			IP: "172.16.0.1",
		}, {
			IP: "192.168.0.1",
		}, {
			IP: "10.0.0.1",
		}},
		expected: []kapi.EndpointAddress{{
			IP: "10.0.0.1",
		}, {
			IP: "172.16.0.1",
		}, {
			IP: "192.168.0.1",
		}},
	}, {
		description: "equal IP address order by hostname",
		input: []kapi.EndpointAddress{{
			IP:       "172.16.0.1",
			Hostname: "b",
		}, {
			IP:       "172.16.0.1",
			Hostname: "a",
		}, {
			IP:       "10.0.0.1",
			Hostname: "a",
		}},
		expected: []kapi.EndpointAddress{{
			IP:       "10.0.0.1",
			Hostname: "a",
		}, {
			IP:       "172.16.0.1",
			Hostname: "a",
		}, {
			IP:       "172.16.0.1",
			Hostname: "b",
		}},
	}}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			if len(tc.input) > 0 {
				if diff := cmp.Diff(tc.input, tc.expected); len(diff) == 0 {
					t.Errorf("expecting input to differ from expected (-want +got):\n%s", diff)
				}
			}

			endpointsubset.SortAddresses(tc.input, endpointsubset.DefaultEndpointAddressOrderByFuncs())

			if diff := cmp.Diff(tc.input, tc.expected); len(diff) != 0 {
				t.Errorf("mismatched sort order (-want +got):\n%s", diff)
			}
		})
	}
}
