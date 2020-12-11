package controller

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			name: "valid-host-trailing-dot",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.test.",
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
			name: "invalid-host-64-chars-label-can-be-overridden",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						routev1.AllowNonDNSCompliantHostAnnotation: "true",
					},
				},
				Spec: routev1.RouteSpec{
					Host: "name-namespace-1234567890-1234567890-1234567890-1234567890-12345.example.test",
				},
			},
			expectedErrors: false,
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
		{
			name: "invalid-name-279-chars-cannot-be-overridden",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						routev1.AllowNonDNSCompliantHostAnnotation: "true",
					},
				},
				Spec: routev1.RouteSpec{
					Host: "name-namespace.a1234567890.b1234567890.c1234567890.d1234567890.e1234567890.f1234567890.g1234567890.h1234567890.i1234567890.j1234567890.k1234567890.l1234567890.m1234567890.n1234567890.o1234567890.p1234567890.q1234567890.r1234567890.s1234567890.t1234567890.u1234567890.example.test",
				},
			},
			expectedErrors: true,
		},
		{
			name: "invalid-name-one-label",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "org",
				},
			},
			expectedErrors: true,
		},
		{
			name: "invalid-name-one-label-can-be-overridden",
			route: &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						routev1.AllowNonDNSCompliantHostAnnotation: "true",
					},
				},
				Spec: routev1.RouteSpec{
					Host: "org",
				},
			},
			expectedErrors: false,
		},
		{
			name: "invalid-name-asterisk",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "*",
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
