package endpointsubset_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/router/pkg/router/controller/endpointsubset"
	kapi "k8s.io/api/core/v1"
)

func TestSortPorts(t *testing.T) {
	var testcases = []struct {
		description string
		input       []kapi.EndpointPort
		expected    []kapi.EndpointPort
	}{{
		description: "empty slice",
	}, {
		description: "port numbers sort lowest -> highest",
		input: []kapi.EndpointPort{{
			Port: 2,
		}, {
			Port: 0,
		}, {
			Port: 1,
		}, {
			Port: 0,
		}},
		expected: []kapi.EndpointPort{{
			Port: 0,
		}, {
			Port: 0,
		}, {
			Port: 1,
		}, {
			Port: 2,
		}},
	}, {
		description: "equal port numbers sort by Name",
		input: []kapi.EndpointPort{{
			Port: 3,
			Name: "b",
		}, {
			Port: 3,
			Name: "a",
		}, {
			Port: 2,
			Name: "a",
		}},
		expected: []kapi.EndpointPort{{
			Port: 2,
			Name: "a",
		}, {
			Port: 3,
			Name: "a",
		}, {
			Port: 3,
			Name: "b",
		}},
	}, {
		description: "Equal port numbers and equal names sort by protocol",
		input: []kapi.EndpointPort{{
			Port:     3,
			Name:     "a",
			Protocol: "UDP",
		}, {
			Port:     3,
			Name:     "a",
			Protocol: "TCP",
		}},
		expected: []kapi.EndpointPort{{
			Port:     3,
			Name:     "a",
			Protocol: "TCP",
		}, {
			Port:     3,
			Name:     "a",
			Protocol: "UDP",
		}},
	}}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			if len(tc.input) > 0 {
				if diff := cmp.Diff(tc.input, tc.expected); len(diff) == 0 {
					t.Errorf("expecting input to differ from expected (-want +got):\n%s", diff)
				}
			}

			endpointsubset.SortPorts(tc.input, endpointsubset.DefaultEndpointPortOrderByFuncs())

			if diff := cmp.Diff(tc.input, tc.expected); len(diff) != 0 {
				t.Errorf("mismatched sort order (-want +got):\n%s", diff)
			}
		})
	}
}
