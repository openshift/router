package templaterouter

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	routev1 "github.com/openshift/api/route/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
)

// TestCreateServiceUnit tests creating a service unit and finding it in router state
func TestCreateServiceUnit(t *testing.T) {
	router := NewFakeTemplateRouter()
	suKey := ServiceUnitKey("ns/test")
	router.CreateServiceUnit(suKey)

	if _, ok := router.FindServiceUnit(suKey); !ok {
		t.Errorf("Unable to find serivce unit %s after creation", suKey)
	}
}

// TestDeleteServiceUnit tests that deleted service units no longer exist in state
func TestDeleteServiceUnit(t *testing.T) {
	router := NewFakeTemplateRouter()
	suKey := ServiceUnitKey("ns/test")
	router.CreateServiceUnit(suKey)
	router.AddRoute(&routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "edge",
		},
		Spec: routev1.RouteSpec{
			Host: "edge-ns.foo.com",
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "test",
			},
		},
	})

	if _, ok := router.FindServiceUnit(suKey); !ok {
		t.Errorf("Unable to find serivce unit %s after creation", suKey)
	}

	router.DeleteServiceUnit(suKey)

	if _, ok := router.FindServiceUnit(suKey); ok {
		t.Errorf("Service unit %s was found in state after delete", suKey)
	}

	var expectedStateChanged = true
	if expectedStateChanged != router.stateChanged {
		t.Errorf("Expected router state change=%v, got=%v", expectedStateChanged, router.stateChanged)
	}
}

// TestAddEndpoints test adding endpoints to service units
func TestAddEndpoints(t *testing.T) {
	router := NewFakeTemplateRouter()
	suKey := ServiceUnitKey("nsl/test")
	router.CreateServiceUnit(suKey)

	if _, ok := router.FindServiceUnit(suKey); !ok {
		t.Errorf("Unable to find serivce unit %s after creation", suKey)
	}

	// Adding endpoints without an associated route will not
	// result in a state change so create the associated route.
	router.AddRoute(&routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "nsl",
			Name:      "edge",
		},
		Spec: routev1.RouteSpec{
			Host: "edge-nsl.foo.com",
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "test",
			},
		},
	})

	endpoint := Endpoint{
		ID:     "ep1",
		IP:     "ip",
		Port:   "port",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep1ipport"))),
	}

	router.AddEndpoints(suKey, []Endpoint{endpoint})

	if !router.stateChanged {
		t.Errorf("Expected router stateChanged to be true")
	}

	su, ok := router.FindServiceUnit(suKey)

	if !ok {
		t.Errorf("Unable to find created service unit %s", suKey)
	} else {
		if len(su.EndpointTable) != 1 {
			t.Errorf("Expected endpoint table to contain 1 entry")
		} else {
			actualEp := su.EndpointTable[0]
			if endpoint.IP != actualEp.IP || endpoint.Port != actualEp.Port || endpoint.IdHash != actualEp.IdHash {
				t.Errorf("Expected endpoint %v did not match actual endpoint %v", endpoint, actualEp)
			}
		}
	}
}

// Test that AddEndpoints returns true and false correctly for changed endpoints.
func TestAddEndpointDuplicates(t *testing.T) {
	router := NewFakeTemplateRouter()
	suKey := ServiceUnitKey("ns/test")
	router.CreateServiceUnit(suKey)
	if _, ok := router.FindServiceUnit(suKey); !ok {
		t.Fatalf("Unable to find service unit %s after creation", suKey)
	}

	// Adding endpoints without an associated route will not
	// result in a state change so create the associated route.
	router.AddRoute(&routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "edge",
		},
		Spec: routev1.RouteSpec{
			Host: "edge-ns.foo.com",
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "test",
			},
		},
	})

	endpoint := Endpoint{
		ID:   "ep1",
		IP:   "1.1.1.1",
		Port: "80",
	}
	endpoint2 := Endpoint{
		ID:   "ep2",
		IP:   "2.2.2.2",
		Port: "8080",
	}
	endpoint3 := Endpoint{
		ID:   "ep3",
		IP:   "3.3.3.3",
		Port: "8888",
	}

	testCases := []struct {
		name      string
		endpoints []Endpoint
		expected  bool
	}{
		{
			name:      "initial add",
			endpoints: []Endpoint{endpoint, endpoint2},
			expected:  true,
		},
		{
			name:      "add same endpoints",
			endpoints: []Endpoint{endpoint, endpoint2},
			expected:  false,
		},
		{
			name:      "add changed endpoints",
			endpoints: []Endpoint{endpoint3, endpoint2},
			expected:  true,
		},
	}

	for _, v := range testCases {
		router.stateChanged = false
		router.AddEndpoints(suKey, v.endpoints)
		if router.stateChanged != v.expected {
			t.Errorf("%s expected to set router stateChanged to %v but got %v", v.name, v.expected, router.stateChanged)
		}
		su, ok := router.FindServiceUnit(suKey)
		if !ok {
			t.Errorf("%s was unable to find created service unit %s", v.name, suKey)
			continue
		}
		if len(su.EndpointTable) != len(v.endpoints) {
			t.Errorf("%s expected endpoint table to contain %d entries but found %v", v.name, len(v.endpoints), su.EndpointTable)
			continue
		}
		for i, ep := range su.EndpointTable {
			expected := v.endpoints[i]
			if expected.IP != ep.IP || expected.Port != ep.Port {
				t.Errorf("%s expected endpoint %v did not match actual endpoint %v", v.name, endpoint, ep)
			}
		}
	}
}

// TestDeleteEndpoints tests removing endpoints from service units
func TestDeleteEndpoints(t *testing.T) {
	router := NewFakeTemplateRouter()
	suKey := ServiceUnitKey("ns/test")
	router.CreateServiceUnit(suKey)

	if _, ok := router.FindServiceUnit(suKey); !ok {
		t.Errorf("Unable to find serivce unit %s after creation", suKey)
	}

	// Deleting endpoints without an associated route won't result
	// in a state change so create an associated route for the
	// service unit.
	router.AddRoute(&routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      "edge",
		},
		Spec: routev1.RouteSpec{
			Host: "edge-ns.foo.com",
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: "test",
			},
		},
	})

	router.AddEndpoints(suKey, []Endpoint{
		{
			ID:   "ep1",
			IP:   "ip",
			Port: "port",
		},
	})

	su, ok := router.FindServiceUnit(suKey)

	if !ok {
		t.Errorf("Unable to find created service unit %s", suKey)
	} else {
		if len(su.EndpointTable) != 1 {
			t.Errorf("Expected endpoint table to contain 1 entry")
		} else {
			router.stateChanged = false
			router.DeleteEndpoints(suKey)
			if !router.stateChanged {
				t.Errorf("Expected router stateChanged to be true")
			}

			su, ok := router.FindServiceUnit(suKey)

			if !ok {
				t.Errorf("Unable to find created service unit %s", suKey)
			} else {
				if len(su.EndpointTable) > 0 {
					t.Errorf("Expected endpoint table to be empty")
				}
			}
		}
	}
}

// TestRouteKey tests that route keys are created as expected
func TestRouteKey(t *testing.T) {
	router := NewFakeTemplateRouter()
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
	}

	key := routeKey(route)

	if key != "foo:bar" {
		t.Errorf("Expected key 'foo:bar' but got: %s", key)
	}

	testCases := []struct {
		Namespace string
		Name      string
	}{
		{
			Namespace: "foo-bar",
			Name:      "baz",
		},
		{
			Namespace: "foo",
			Name:      "bar-baz",
		},
		{
			Namespace: "usain-bolt",
			Name:      "dash-dash",
		},
		{
			Namespace: "usain",
			Name:      "bolt-dash-dash",
		},
		{
			Namespace: "",
			Name:      "ab-testing",
		},
		{
			Namespace: "ab-testing",
			Name:      "",
		},
		{
			Namespace: "ab",
			Name:      "testing",
		},
	}

	startCount := len(router.state)
	for _, tc := range testCases {
		route := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: tc.Namespace,
				Name:      tc.Name,
			},
			Spec: routev1.RouteSpec{
				Host: "host",
				Path: "path",
				TLS: &routev1.TLSConfig{
					Termination:              routev1.TLSTerminationEdge,
					Certificate:              "abc",
					Key:                      "def",
					CACertificate:            "ghi",
					DestinationCACertificate: "jkl",
				},
			},
		}

		router.AddRoute(route)
		routeKey := routeKey(route)
		_, ok := router.state[routeKey]
		if !ok {
			t.Errorf("Unable to find created service alias config for route %s", routeKey)
		}
	}

	// ensure all the generated routes were added.
	numRoutesAdded := len(router.state) - startCount
	expectedCount := len(testCases)
	if numRoutesAdded != expectedCount {
		t.Errorf("Expected %v routes to be added but only %v were actually added", expectedCount, numRoutesAdded)
	}
}

// TestCreateServiceAliasConfig validates creation of a ServiceAliasConfig from a route and the router state
func TestCreateServiceAliasConfig(t *testing.T) {
	router := NewFakeTemplateRouter()

	namespace := "foo"
	serviceName := "TestService"
	serviceWeight := int32(0)

	var headerNameXFrame string = "X-Frame-Options"
	var headerNameXSS string = "X-XSS-Protection"

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "bar",
		},
		Spec: routev1.RouteSpec{
			Host: "host",
			Path: "path",
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromInt(8080),
			},
			To: routev1.RouteTargetReference{
				Name:   serviceName,
				Weight: &serviceWeight,
			},
			TLS: &routev1.TLSConfig{
				Termination:              routev1.TLSTerminationEdge,
				Certificate:              "abc",
				Key:                      "def",
				CACertificate:            "ghi",
				DestinationCACertificate: "jkl",
			},
			HTTPHeaders: &routev1.RouteHTTPHeaders{

				Actions: routev1.RouteHTTPHeaderActions{
					Response: []routev1.RouteHTTPHeader{
						{
							Name: headerNameXFrame,
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set: &routev1.RouteSetHTTPHeader{
									Value: "DENY",
								},
							},
						},
						{
							Name: headerNameXSS,
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set: &routev1.RouteSetHTTPHeader{
									Value: "1;mode=block",
								},
							},
						},
						{

							Name: headerNameXFrame,
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Delete,
							},
						},
						{

							Name: headerNameXSS,
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Delete,
							},
						},
						{},
					},
					Request: []routev1.RouteHTTPHeader{
						{
							Name: "Accept",
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set: &routev1.RouteSetHTTPHeader{
									Value: "text/plain,text/html",
								},
							},
						},
						{
							Name: "x-client",
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set: &routev1.RouteSetHTTPHeader{
									Value: `"abc"\ 'def'`,
								},
							},
						},
						{

							Name: "Accept-Encoding",
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Delete,
							},
						},
						// blank object.
						{},
						// no value provided.
						{
							Name: "Accept",
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set:  &routev1.RouteSetHTTPHeader{},
							},
						},
						// invalid value provided.
						{
							Name: "Accept",
							Action: routev1.RouteHTTPHeaderActionUnion{
								Type: routev1.Set,
								Set: &routev1.RouteSetHTTPHeader{
									Value: "text/}plain,text/html{",
								},
							},
						},
					},
				},
			},
		},
	}

	config := *router.createServiceAliasConfig(route, "foo")

	suName := endpointsKeyFromParts(namespace, serviceName)
	expectedSUs := map[ServiceUnitKey]int32{
		suName: serviceWeight,
	}
	httpResponseHeadersList := []HTTPHeader{{Name: "X-Frame-Options", Value: "'DENY'", Action: "Set"}, {Name: "X-XSS-Protection", Value: "'1;mode=block'", Action: "Set"},
		{Name: "X-Frame-Options", Action: "Delete"}, {Name: "X-XSS-Protection", Action: "Delete"}}
	httpRequestHeadersList := []HTTPHeader{{Name: "Accept", Value: "'text/plain,text/html'", Action: "Set"}, {Name: "x-client", Value: `'"abc"\ '\''def'\'''`, Action: "Set"}, {Name: "Accept-Encoding", Action: "Delete"}, {Name: "Accept", Value: "'text/}plain,text/html{'", Action: "Set"}}

	// Basic sanity, validate more fields as necessary
	if config.Host != route.Spec.Host || config.Path != route.Spec.Path || !compareTLS(route, config, t) ||
		config.PreferPort != route.Spec.Port.TargetPort.String() || !reflect.DeepEqual(expectedSUs, config.ServiceUnits) ||
		config.ActiveServiceUnits != 0 ||
		!cmp.Equal(config.HTTPResponseHeaders, httpResponseHeadersList) ||
		!cmp.Equal(config.HTTPRequestHeaders, httpRequestHeadersList) {
		t.Errorf("Route %v did not match service alias config %v", route, config)
	}

}

// TestAddRoute validates that adding a route creates a service alias config and associated service units
func TestAddRoute(t *testing.T) {
	router := NewFakeTemplateRouter()

	namespace := "foo"
	serviceName := "TestService"

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "bar",
		},
		Spec: routev1.RouteSpec{
			Host: "host",
			Path: "path",
			To: routev1.RouteTargetReference{
				Name: serviceName,
			},
		},
	}

	router.AddRoute(route)
	if !router.stateChanged {
		t.Fatalf("router state not marked as changed")
	}

	suName := endpointsKeyFromParts(namespace, serviceName)
	expectedSUs := map[ServiceUnitKey]ServiceUnit{
		suName: {
			Name:          string(suName),
			Hostname:      "TestService.foo.svc",
			EndpointTable: []Endpoint{},

			ServiceAliasAssociations: map[ServiceAliasConfigKey]bool{"foo:bar": true},
		},
	}

	if !reflect.DeepEqual(expectedSUs, router.serviceUnits) {
		t.Fatalf("Unexpected service units:\nwant: %#v\n got: %#v", expectedSUs, router.serviceUnits)
	}

	routeKey := routeKey(route)

	if config, ok := router.state[routeKey]; !ok {
		t.Errorf("Unable to find created service alias config for route %s", routeKey)
	} else if config.Host != route.Spec.Host {
		// This test is not validating createServiceAliasConfig, so superficial validation should be good enough.
		t.Errorf("Route %v did not match service alias config %v", route, config)
	}
}

func TestUpdateRoute(t *testing.T) {
	router := NewFakeTemplateRouter()

	// Add a route that can be targeted for an update
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
		Spec: routev1.RouteSpec{
			Host: "host",
			Path: "/foo",
		},
	}
	router.AddRoute(route)

	testCases := []struct {
		name    string
		path    string
		updated bool
	}{
		{
			name:    "Same route does not update state",
			path:    "/foo",
			updated: false,
		},
		{
			name:    "Different route updates state",
			path:    "/bar",
			updated: true,
		},
	}

	for _, tc := range testCases {
		router.stateChanged = false
		route.Spec.Path = tc.path
		router.AddRoute(route)
		if router.stateChanged != tc.updated {
			t.Errorf("%s: expected stateChanged = %v, but got %v", tc.name, tc.updated, router.stateChanged)
		}
	}
}

// compareTLS is a utility to help compare cert contents between an route and a config
func compareTLS(route *routev1.Route, saCfg ServiceAliasConfig, t *testing.T) bool {
	return findCert(route.Spec.TLS.DestinationCACertificate, saCfg.Certificates, false, t) &&
		findCert(route.Spec.TLS.CACertificate, saCfg.Certificates, false, t) &&
		findCert(route.Spec.TLS.Key, saCfg.Certificates, true, t) &&
		findCert(route.Spec.TLS.Certificate, saCfg.Certificates, false, t)
}

// findCert is a utility to help find the cert in a config's set of certificates
func findCert(cert string, certs map[string]Certificate, isPrivateKey bool, t *testing.T) bool {
	found := false

	for _, c := range certs {
		if isPrivateKey {
			if c.PrivateKey == cert {
				found = true
				break
			}
		} else {
			if c.Contents == cert {
				found = true
				break
			}
		}
	}

	if !found {
		t.Errorf("unable to find cert %s in %v", cert, certs)
	}

	return found
}

// TestRemoveRoute tests removing a ServiceAliasConfig from a ServiceUnit
func TestRemoveRoute(t *testing.T) {
	router := NewFakeTemplateRouter()
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
		Spec: routev1.RouteSpec{
			Host: "host",
		},
	}
	route2 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar2",
		},
		Spec: routev1.RouteSpec{
			Host: "host",
		},
	}
	suKey := endpointsKeyFromParts("bar", "test")

	router.CreateServiceUnit(suKey)
	router.AddRoute(route)
	router.AddRoute(route2)

	_, ok := router.FindServiceUnit(suKey)
	if !ok {
		t.Fatalf("Unable to find created service unit %s", suKey)
	}

	rKey := routeKey(route)
	saCfg, ok := router.state[rKey]
	if !ok {
		t.Fatalf("Unable to find created serivce alias config for route %s", rKey)
	}
	if saCfg.Host != route.Spec.Host || saCfg.Path != route.Spec.Path {
		t.Fatalf("Route %v did not match serivce alias config %v", route, saCfg)
	}

	router.RemoveRoute(route)
	if _, ok := router.state[rKey]; ok {
		t.Errorf("Route %v was expected to be deleted but was still found", route)
	}
	if _, ok := router.state[routeKey(route2)]; !ok {
		t.Errorf("Route %v was expected to exist but was not found", route2)
	}
}

func TestShouldWriteCertificates(t *testing.T) {
	testCases := []struct {
		name             string
		cfg              *ServiceAliasConfig
		shouldWriteCerts bool
	}{
		{
			name: "no termination",
			cfg: &ServiceAliasConfig{
				TLSTermination: "",
			},
			shouldWriteCerts: false,
		},
		{
			name: "passthrough termination",
			cfg: &ServiceAliasConfig{
				TLSTermination: routev1.TLSTerminationPassthrough,
			},
			shouldWriteCerts: false,
		},
		{
			name: "edge termination true",
			cfg: &ServiceAliasConfig{
				Host:           "edgetermtrue",
				TLSTermination: routev1.TLSTerminationEdge,
				Certificates:   makeCertMap("edgetermtrue", true),
			},
			shouldWriteCerts: true,
		},
		{
			name: "edge termination false",
			cfg: &ServiceAliasConfig{
				Host:           "edgetermfalse",
				TLSTermination: routev1.TLSTerminationEdge,
				Certificates:   makeCertMap("edgetermfalse", false),
			},
			shouldWriteCerts: false,
		},
		{
			name: "reencrypt termination true",
			cfg: &ServiceAliasConfig{
				Host:           "reencrypttermtrue",
				TLSTermination: routev1.TLSTerminationReencrypt,
				Certificates:   makeCertMap("reencrypttermtrue", true),
			},
			shouldWriteCerts: true,
		},
		{
			name: "reencrypt termination false",
			cfg: &ServiceAliasConfig{
				Host:           "reencrypttermfalse",
				TLSTermination: routev1.TLSTerminationReencrypt,
				Certificates:   makeCertMap("reencrypttermfalse", false),
			},
			shouldWriteCerts: false,
		},
	}

	router := NewFakeTemplateRouter()
	for _, tc := range testCases {
		result := router.shouldWriteCerts(tc.cfg)
		if result != tc.shouldWriteCerts {
			t.Errorf("test case %s failed.  Expected shouldWriteCerts to return %t but found %t.  Cfg: %#v", tc.name, tc.shouldWriteCerts, result, tc.cfg)
		}
	}
}

func TestHasRequiredEdgeCerts(t *testing.T) {
	validCertMap := makeCertMap("host", true)
	cfg := &ServiceAliasConfig{
		Host:         "host",
		Certificates: validCertMap,
	}
	if !hasRequiredEdgeCerts(cfg) {
		t.Errorf("expected %#v to return true for valid edge certs", cfg)
	}

	invalidCertMap := makeCertMap("host", false)
	cfg.Certificates = invalidCertMap
	if hasRequiredEdgeCerts(cfg) {
		t.Errorf("expected %#v to return false for invalid edge certs", cfg)
	}
}

func makeCertMap(host string, valid bool) map[string]Certificate {
	privateKey := "private Key"
	if !valid {
		privateKey = ""
	}
	certMap := map[string]Certificate{
		host: {
			ID:         "host certificate",
			Contents:   "certificate",
			PrivateKey: privateKey,
		},
	}
	return certMap
}

// TestAddRouteEdgeTerminationInsecurePolicy tests adding an insecure edge
// terminated routes to a service unit
func TestAddRouteEdgeTerminationInsecurePolicy(t *testing.T) {
	router := NewFakeTemplateRouter()

	testCases := []struct {
		Name           string
		InsecurePolicy routev1.InsecureEdgeTerminationPolicyType
	}{
		{
			Name:           "none",
			InsecurePolicy: routev1.InsecureEdgeTerminationPolicyNone,
		},
		{
			Name:           "allow",
			InsecurePolicy: routev1.InsecureEdgeTerminationPolicyAllow,
		},
		{
			Name:           "redirect",
			InsecurePolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		},
		{
			Name:           "httpsec",
			InsecurePolicy: routev1.InsecureEdgeTerminationPolicyType("httpsec"),
		},
		{
			Name:           "hsts",
			InsecurePolicy: routev1.InsecureEdgeTerminationPolicyType("hsts"),
		},
	}

	for _, tc := range testCases {
		route := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      tc.Name,
			},
			Spec: routev1.RouteSpec{
				Host: fmt.Sprintf("%s-host", tc.Name),
				Path: "path",
				TLS: &routev1.TLSConfig{
					Termination:                   routev1.TLSTerminationEdge,
					Certificate:                   "abc",
					Key:                           "def",
					CACertificate:                 "ghi",
					DestinationCACertificate:      "jkl",
					InsecureEdgeTerminationPolicy: tc.InsecurePolicy,
				},
			},
		}

		router.AddRoute(route)

		routeKey := routeKey(route)
		saCfg, ok := router.state[routeKey]

		if !ok {
			t.Errorf("InsecureEdgeTerminationPolicy test %s: unable to find created service alias config for route %s",
				tc.Name, routeKey)
		} else {
			if saCfg.Host != route.Spec.Host || saCfg.Path != route.Spec.Path || !compareTLS(route, saCfg, t) || saCfg.InsecureEdgeTerminationPolicy != tc.InsecurePolicy {
				t.Errorf("InsecureEdgeTerminationPolicy test %s: route %v did not match serivce alias config %v",
					tc.Name, route, saCfg)
			}
		}
	}
}

func TestFilterNamespaces(t *testing.T) {
	router := NewFakeTemplateRouter()

	testCases := []struct {
		name         string
		serviceUnits map[ServiceUnitKey]ServiceUnit
		state        map[ServiceAliasConfigKey]ServiceAliasConfig

		filterNamespaces sets.String

		expectedServiceUnits map[ServiceUnitKey]ServiceUnit
		expectedState        map[ServiceAliasConfigKey]ServiceAliasConfig
		expectedStateChanged bool
	}{
		{
			name:                 "empty",
			serviceUnits:         map[ServiceUnitKey]ServiceUnit{},
			state:                map[ServiceAliasConfigKey]ServiceAliasConfig{},
			filterNamespaces:     sets.NewString("ns1"),
			expectedServiceUnits: map[ServiceUnitKey]ServiceUnit{},
			expectedState:        map[ServiceAliasConfigKey]ServiceAliasConfig{},
			expectedStateChanged: false,
		},
		{
			name: "valid, filter none",
			serviceUnits: map[ServiceUnitKey]ServiceUnit{
				endpointsKeyFromParts("ns1", "svc"): {},
				endpointsKeyFromParts("ns2", "svc"): {},
			},
			state: map[ServiceAliasConfigKey]ServiceAliasConfig{
				routeKeyFromParts("ns1", "svc"): {},
				routeKeyFromParts("ns2", "svc"): {},
			},
			filterNamespaces: sets.NewString("ns1", "ns2"),
			expectedServiceUnits: map[ServiceUnitKey]ServiceUnit{
				endpointsKeyFromParts("ns1", "svc"): {},
				endpointsKeyFromParts("ns2", "svc"): {},
			},
			expectedState: map[ServiceAliasConfigKey]ServiceAliasConfig{
				routeKeyFromParts("ns1", "svc"): {},
				routeKeyFromParts("ns2", "svc"): {},
			},
			expectedStateChanged: false,
		},
		{
			name: "valid, filter some",
			serviceUnits: map[ServiceUnitKey]ServiceUnit{
				endpointsKeyFromParts("ns1", "svc"): {},
				endpointsKeyFromParts("ns2", "svc"): {},
			},
			state: map[ServiceAliasConfigKey]ServiceAliasConfig{
				routeKeyFromParts("ns1", "svc"): {},
				routeKeyFromParts("ns2", "svc"): {},
			},
			filterNamespaces: sets.NewString("ns2"),
			expectedServiceUnits: map[ServiceUnitKey]ServiceUnit{
				endpointsKeyFromParts("ns2", "svc"): {},
			},
			expectedState: map[ServiceAliasConfigKey]ServiceAliasConfig{
				routeKeyFromParts("ns2", "svc"): {},
			},
			expectedStateChanged: true,
		},
		{
			name: "valid, filter all",
			serviceUnits: map[ServiceUnitKey]ServiceUnit{
				endpointsKeyFromParts("ns1", "svc"): {},
				endpointsKeyFromParts("ns2", "svc"): {},
			},
			state: map[ServiceAliasConfigKey]ServiceAliasConfig{
				routeKeyFromParts("ns1", "svc"): {},
				routeKeyFromParts("ns2", "svc"): {},
			},
			filterNamespaces:     sets.NewString("ns3"),
			expectedServiceUnits: map[ServiceUnitKey]ServiceUnit{},
			expectedState:        map[ServiceAliasConfigKey]ServiceAliasConfig{},
			expectedStateChanged: true,
		},
	}

	for _, tc := range testCases {
		router.serviceUnits = tc.serviceUnits
		router.state = tc.state
		router.FilterNamespaces(tc.filterNamespaces)
		if !reflect.DeepEqual(router.serviceUnits, tc.expectedServiceUnits) {
			t.Errorf("test %s: expected router serviceUnits:%v but got %v", tc.name, tc.expectedServiceUnits, router.serviceUnits)
		}
		if !reflect.DeepEqual(router.state, tc.expectedState) {
			t.Errorf("test %s: expected router state:%v but got %v", tc.name, tc.expectedState, router.state)
		}
		if router.stateChanged != tc.expectedStateChanged {
			t.Errorf("test %s: expected router stateChanged:%v but got %v", tc.name, tc.expectedStateChanged, router.stateChanged)
		}
	}
}

// TestCalculateServiceWeights tests calculating the service
// endpoint weights
func TestCalculateServiceWeights(t *testing.T) {
	suKey1 := ServiceUnitKey("ns/svc1")
	suKey2 := ServiceUnitKey("ns/svc2")
	ep1 := Endpoint{
		ID:       "ep1",
		IP:       "ip",
		Port:     "8080",
		PortName: "port",
		IdHash:   fmt.Sprintf("%x", md5.Sum([]byte("ep1ipport"))),
	}
	ep2 := Endpoint{
		ID:       "ep2",
		IP:       "ip",
		Port:     "8080",
		PortName: "port",
		IdHash:   fmt.Sprintf("%x", md5.Sum([]byte("ep2ipport"))),
	}
	ep3 := Endpoint{
		ID:       "ep3",
		IP:       "ip",
		Port:     "8080",
		PortName: "port",
		IdHash:   fmt.Sprintf("%x", md5.Sum([]byte("ep3ipport"))),
	}
	ep4 := Endpoint{
		ID:       "ep4",
		IP:       "ip2",
		Port:     "8081",
		PortName: "port2",
		IdHash:   fmt.Sprintf("%x", md5.Sum([]byte("ep3ipport"))),
	}

	testCases := []struct {
		name            string
		serviceUnits    map[ServiceUnitKey][]Endpoint
		routePort       string
		serviceWeights  map[ServiceUnitKey]int32
		expectedWeights map[ServiceUnitKey]int32
	}{
		{
			name:      "equally weighted services with same number of endpoints",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1},
				suKey2: {ep2},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 256,
				suKey2: 256,
			},
		},
		{
			name:      "unequally weighted services with same number of endpoints",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1},
				suKey2: {ep2},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 25,
				suKey2: 75,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 85,
				suKey2: 256,
			},
		},
		{
			name:      "services with equal weights and a different number of endpoints",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 128,
				suKey2: 256,
			},
		},
		{
			name:      "services with unequal weights and a different number of endpoints",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 20,
				suKey2: 60,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 42,
				suKey2: 256,
			},
		},
		{
			name:      "services with equal weights and a different number of endpoints, one of which is common",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
				suKey2: {ep2},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 128,
				suKey2: 256,
			},
		},
		{
			name:      "a single service with a single endpoint",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 1,
			},
		},
		{
			name:      "a single service with a multiple endpoints",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 1,
			},
		},
		{
			name:      "a single service with multiple endpoints, but no endpoint ports match the route port",
			routePort: "9090",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{},
		},
		{
			name:      "services with multiple endpoints with different ports",
			routePort: "8080",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep4},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 256,
				suKey2: 256,
			},
		},
		{
			name:      "services with multiple endpoints with different ports and route port is portName",
			routePort: "port",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep4},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 256,
				suKey2: 256,
			},
		},
		{
			name: "services with multiple endpoints with different ports and route has no port",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep4},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 128, // counts endpoints of all ports
				suKey2: 256,
			},
		},
		{
			name: "services with multiple endpoints and route has no port",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 50,
				suKey2: 50,
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 128, // counts endpoints of all ports
				suKey2: 256,
			},
		},
		{
			name: "services with too many endpoints to achieve desired weight",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {ep1, ep2, ep2, ep3, ep3},
				suKey2: {ep3},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 3,   // less than number of endpoints
				suKey2: 256, // maxed out at 256
			},
			expectedWeights: map[ServiceUnitKey]int32{
				suKey1: 1,
				suKey2: 256,
			},
		},
		{
			name:            "no services with no endpoints",
			routePort:       "port",
			serviceUnits:    map[ServiceUnitKey][]Endpoint{},
			serviceWeights:  map[ServiceUnitKey]int32{},
			expectedWeights: map[ServiceUnitKey]int32{},
		},
		{
			name:      "service with no endpoint",
			routePort: "port",
			serviceUnits: map[ServiceUnitKey][]Endpoint{
				suKey1: {},
			},
			serviceWeights: map[ServiceUnitKey]int32{
				suKey1: 100,
			},
			expectedWeights: map[ServiceUnitKey]int32{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewFakeTemplateRouter()

			for suKey, eps := range tc.serviceUnits {
				router.CreateServiceUnit(suKey)
				router.AddEndpoints(suKey, eps)
			}
			endpointWeights := router.calculateServiceWeights(tc.serviceWeights, tc.routePort)
			if !reflect.DeepEqual(endpointWeights, tc.expectedWeights) {
				t.Errorf("expected endpointWeights to be %v, got %v", tc.expectedWeights, endpointWeights)
			}
		})
	}
}

const (
	testWildcardCertificate = `-----BEGIN CERTIFICATE-----
MIIFJjCCAw4CCQCLGB4wxqgxHjANBgkqhkiG9w0BAQsFADBOMQswCQYDVQQGEwJV
UzELMAkGA1UECAwCTkMxEjAQBgNVBAoMCU9wZW5TaGlmdDEMMAoGA1UECwwDRW5n
MRAwDgYDVQQDDAdleGFtcGxlMB4XDTIxMTExMjAwMTI0M1oXDTIyMTExMjAwMTI0
M1owXDELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAk5DMRIwEAYDVQQKDAlPcGVuU2hp
ZnQxDDAKBgNVBAsMA0VuZzEeMBwGA1UEAwwVKi5hcHBzLm15Y2x1c3Rlci50ZXN0
MIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA24KR3M6HgM2j+dpNEwEt
/dAh5x1vNbFX6C2rZcY9FHpkRR4BCJJr9pl8BeemOqR/adRoVZZCCcp2ylRLLR+N
HvqKcBlsKt/2EZOhmpWI6I7vFMO6lt3OlyDgdmvrR5/W7c/bN7MZtL6F5hvtcD4G
XXGt8I00ok5OO3V4n8stWSHRS7cwKRDGT9A0TOSNtmSCqHZKF94fEseKGR+sZZeO
FE9qeJ+d+GFwuv/s9TvsEM0s9A/bVqEAoOKaXXQ6jUEtgXAW2lJgDgUqdisbpUEE
XvClvBMKnGgbWfs1J3U4yllA5/2wzJ8F2MWMq70nMCmJl1LaNII1sPbLQC1ppsDh
p//XviCT2zXxzNl43w5kQ8AUgvIpIlrZJU4UyWCihRJ5W6RI+kXXl0ewjkhhrWmc
9kpuNzFsby3JwWAUPDoVtgzOOICqgsGWYvGjxHH77Ia+NnAkT5kyiwWnwQm5bsV/
52MBlMBVl31h7v/SZwbkSnoF5FkjcQLRpYm48wI3lae2IAqxEDvZpoQlKKhWUJwc
Huz3JNdCJHyyIyMNQXg0DQSq3elqv1Dewl39tOpEWs5SrXiFhQeUVs1csdNC69NI
Bg7RGtLdJnaDYkwGanxvtaiRPj+HnPLLRpGBNPBHkFCZZVuFfoQPY69KVxiYfzDv
UfLPjrEwY7q3tE+PtPDjIMECAwEAATANBgkqhkiG9w0BAQsFAAOCAgEAS0F33vLP
5AFasmUeZXDtDYHCgaEPo4/8M8B4t2eS402Ylx3tqzRW1gkZbLRj0/NkOMyVReIN
lxBZueHiUwj5/+hDzZ2mGuydy3a1K/Ae4oxGcJ3LpYJWEjtpxUck+acgGTYvaSIX
jmzuoajpENBDmCvakJjUTLR/VQtHoD2lSKh1aMisQkjkPdDN1WR9k8SeRzWUjQxq
r58TYOCBX8K7G0tGJ2CDLaI4HB2Dxnjrbwver9CEYRuKWOlMT5FMTdQ1k68oYaVb
B9loy0z042fI27S6Vmvhrcqd4LIlWUEvn7xeqUapucbj1lZA1IZDUKiRIMhs+fDn
zlFcnaYireTX2gB+5N8S4ZdSKLkphPHwCXEGo0WAqFZnCXs1JvPTlPuEyOw6EZ8/
g18F+gpwHz2ZZ5goFrUukBpyI9CTR3UsYAKPCpFHq6IfsVEZqY5lJljWfsACG27q
FWvcB3JChAjSvji5G36HnhiCaDYdWqkfkJOUFgFM5j3tKcfllMZFH+R5P9WssxY+
sczR6elkJu9LU2f8PevBKxeqYiWfnzS7+no+FU7lJo4hoUeXwc5rz7OIlEUc+zex
6Q2TjLNa9Vi7lFOr1vX3U7Z5eZPhyrgYV/ChV82EE+Y7IUMwYJG8WUE0VwPZTv2z
2OvpOlxkzfR4ZknnMSBoCd8bPDs2F3RsVIk=
-----END CERTIFICATE-----`
	testWildcardCertificateKey = `-----BEGIN PRIVATE KEY-----
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
R3C+ByKc8zehNP33niYeMjRdaIZ/1q5skw==
-----END PRIVATE KEY-----`
)

// TestSecretToPem verifies that secretToPem correctly reads in the provided
// certificate and key and writes out a the expected PEM file for HAProxy.
func TestSecretToPem(t *testing.T) {
	tests := []struct {
		name        string
		cert, key   []byte
		expectedPEM []byte
		expectError bool
	}{
		{
			name:        "empty input",
			cert:        []byte(nil),
			key:         []byte(nil),
			expectError: false,
			expectedPEM: []byte(nil),
		},
		{
			name:        "normal input",
			cert:        []byte(testWildcardCertificate + "\n"),
			key:         []byte(testWildcardCertificateKey + "\n"),
			expectError: false,
			expectedPEM: []byte(testWildcardCertificate + "\n" + testWildcardCertificateKey + "\n"),
		},
		{
			name:        "missing line endings",
			cert:        []byte(testWildcardCertificate),
			key:         []byte(testWildcardCertificateKey),
			expectError: false,
			expectedPEM: []byte(testWildcardCertificate + "\n" + testWildcardCertificateKey + "\n"),
		},
	}
	var (
		secPath = t.TempDir()
		crtPath = filepath.Join(secPath, "tls.crt")
		keyPath = filepath.Join(secPath, "tls.key")
		outPath = filepath.Join(secPath, "cert.pem")
	)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ioutil.WriteFile(crtPath, tc.cert, 0644); err != nil {
				t.Fatal(err)
			}
			if err := ioutil.WriteFile(keyPath, tc.key, 0644); err != nil {
				t.Fatal(err)
			}
			// secretToPem uses file mode 0444, and non-root users
			// cannot write to files with mode 0444.  The router
			// runs as root, but tests do not, so we need to create
			// the file with mode 0644 before calling secretToPem so
			// that it doesn't create a file that it cannot then
			// write to.
			if err := ioutil.WriteFile(outPath, nil, 0644); err != nil {
				t.Fatal(err)
			}
			switch err := secretToPem(secPath, outPath); {
			case !tc.expectError && err != nil:
				t.Fatalf("%q: unexpected error: %v", tc.name, err)
			case tc.expectError && err == nil:
				t.Fatalf("%q: expected error, got nil", tc.name)
			}
			if actualPEM, err := ioutil.ReadFile(outPath); err != nil {
				t.Fatalf("%q: %v", tc.name, err)
			} else if !bytes.Equal(actualPEM, tc.expectedPEM) {
				t.Fatalf("%q: unexpected PEM; expected:\n%s\ngot:\n%s\n", tc.name, tc.expectedPEM, actualPEM)
			}
		})
	}
}
