package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/client-go/route/clientset/versioned/fake"
)

// TestValidateHostName checks that a route's host name matches DNS requirements.
func TestValidateHostName(t *testing.T) {
	tests := []struct {
		name           string
		route          *routev1.Route
		expectedErrors bool
	}{
		{
			name: "valid-host-name",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.test",
				},
			},
			expectedErrors: false,
		},
		{
			name: "invalid-host-name",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "name-namespace-1234567890-1234567890-1234567890-1234567890-1234567890-1234567890-1234567890.example.test",
				},
			},
			expectedErrors: true,
		},
		{
			name: "valid-host-63-chars-label",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "name-namespace-1234567890-1234567890-1234567890-1234567890-1234.example.test",
				},
			},
			expectedErrors: false,
		},
		{
			name: "invalid-host-64-chars-label",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "name-namespace-1234567890-1234567890-1234567890-1234567890-12345.example.test",
				},
			},
			expectedErrors: true,
		},
		{
			name: "valid-name-253-chars",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "name-namespace.a1234567890.b1234567890.c1234567890.d1234567890.e1234567890.f1234567890.g1234567890.h1234567890.i1234567890.j1234567890.k1234567890.l1234567890.m1234567890.n1234567890.o1234567890.p1234567890.q1234567890.r1234567890.s12345678.example.test",
				},
			},
			expectedErrors: false,
		},
		{
			name: "invalid-name-279-chars",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "name-namespace.a1234567890.b1234567890.c1234567890.d1234567890.e1234567890.f1234567890.g1234567890.h1234567890.i1234567890.j1234567890.k1234567890.l1234567890.m1234567890.n1234567890.o1234567890.p1234567890.q1234567890.r1234567890.s1234567890.t1234567890.u1234567890.example.test",
				},
			},
			expectedErrors: true,
		},
	}

	for _, tc := range tests {
		errs := ValidateHostName(tc.route)

		if tc.expectedErrors {
			if len(errs) < 1 {
				t.Errorf("Test case %s expected errors, got none.", tc.name)
			}
		} else {
			if len(errs) > 0 {
				t.Errorf("Test case %s expected no errors, got %d. %v", tc.name, len(errs), errs)
			}
		}
	}
}

// TestUniqueHostResyncRetriesContendedStatus verifies that a status update
// skipped due to contention is retried when the unchanged route is resynced
// (OCPBUGS-130687).
func TestUniqueHostResyncRetriesContendedStatus(t *testing.T) {
	// A stale rejection from this router forces an update of an existing
	// ingress, which is the path subject to the contention check.
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "default", UID: types.UID("uid1"), ResourceVersion: "1"},
		Spec:       routev1.RouteSpec{Host: "route1.test.local"},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{
				Host:       "route1.test.local",
				RouterName: "test",
				Conditions: []routev1.RouteIngressCondition{{
					Type:   routev1.RouteAdmitted,
					Status: corev1.ConditionFalse,
				}},
			}},
		},
	}
	c := fake.NewSimpleClientset(route)
	tracker := &fakeTracker{results: map[contentionKey]bool{"uid1": true}}
	lister := &routeLister{items: []*routev1.Route{route}}
	admitter := NewStatusAdmitter(&fakePlugin{}, c.RouteV1(), lister, "test", "", noopLease{}, tracker)
	recorder := routeStatusRecorder{rejections: make(map[string]string)}
	plugin := NewUniqueHost(admitter, false, recorder)

	if err := plugin.HandleRoute(watch.Added, route); err != nil {
		t.Fatal(err)
	}
	if n := len(c.Actions()); n != 0 {
		t.Fatalf("expected the contended status update to be skipped, got %d actions", n)
	}

	// Contention clears; the informer resync delivers the same object again.
	tracker.results = nil
	if err := plugin.HandleRoute(watch.Modified, route); err != nil {
		t.Fatal(err)
	}
	actions := c.Actions()
	if len(actions) != 1 || actions[0].GetVerb() != "update" || actions[0].GetSubresource() != "status" {
		t.Fatalf("expected the resync to update the route status, got %v", actions)
	}
}
