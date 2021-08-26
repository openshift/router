package templaterouter

import (
	"crypto/md5"
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"

	routev1 "github.com/openshift/api/route/v1"
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
		},
	}

	config := *router.createServiceAliasConfig(route, "foo")

	suName := endpointsKeyFromParts(namespace, serviceName)
	expectedSUs := map[ServiceUnitKey]int32{
		suName: serviceWeight,
	}

	// Basic sanity, validate more fields as necessary
	if config.Host != route.Spec.Host || config.Path != route.Spec.Path || !compareTLS(route, config, t) ||
		config.PreferPort != route.Spec.Port.TargetPort.String() || !reflect.DeepEqual(expectedSUs, config.ServiceUnits) ||
		config.ActiveServiceUnits != 0 {
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
	router := NewFakeTemplateRouter()

	suKey1 := ServiceUnitKey("ns/svc1")
	suKey2 := ServiceUnitKey("ns/svc2")
	ep1 := Endpoint{
		ID:     "ep1",
		IP:     "ip",
		Port:   "port",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep1ipport"))),
	}
	ep2 := Endpoint{
		ID:     "ep2",
		IP:     "ip",
		Port:   "port",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep2ipport"))),
	}
	ep3 := Endpoint{
		ID:     "ep3",
		IP:     "ip",
		Port:   "port",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep3ipport"))),
	}

	testCases := []struct {
		name            string
		serviceUnits    map[ServiceUnitKey][]Endpoint
		serviceWeights  map[ServiceUnitKey]int32
		expectedWeights map[ServiceUnitKey]int32
	}{
		{
			name: "equally weighted services with same number of endpoints",
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
			name: "unequally weighted services with same number of endpoints",
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
			name: "services with equal weights and a different number of endpoints",
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
			name: "services with unequal weights and a different number of endpoints",
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
			name: "services with equal weights and a different number of endpoints, one of which is common",
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
	}

	for _, tc := range testCases {
		for suKey, eps := range tc.serviceUnits {
			router.CreateServiceUnit(suKey)
			router.AddEndpoints(suKey, eps)
		}
		endpointWeights := router.calculateServiceWeights(tc.serviceWeights)
		if !reflect.DeepEqual(endpointWeights, tc.expectedWeights) {
			t.Errorf("test %s: expected endpointWeights to be %v, got %v", tc.name, tc.expectedWeights, endpointWeights)
		}
		// Remove endpoints and service units so the same sample template router
		// can be re-used.
		for suKey := range tc.serviceUnits {
			router.DeleteEndpoints(suKey)
			router.DeleteServiceUnit(suKey)
		}
	}
}
