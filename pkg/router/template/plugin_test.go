package templaterouter

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	kapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router/controller"
)

const (
	testExpiredCAUnknownCertificate = `-----BEGIN CERTIFICATE-----
MIIDIjCCAgqgAwIBAgIBBjANBgkqhkiG9w0BAQUFADCBoTELMAkGA1UEBhMCVVMx
CzAJBgNVBAgMAlNDMRUwEwYDVQQHDAxEZWZhdWx0IENpdHkxHDAaBgNVBAoME0Rl
ZmF1bHQgQ29tcGFueSBMdGQxEDAOBgNVBAsMB1Rlc3QgQ0ExGjAYBgNVBAMMEXd3
dy5leGFtcGxlY2EuY29tMSIwIAYJKoZIhvcNAQkBFhNleGFtcGxlQGV4YW1wbGUu
Y29tMB4XDTE2MDExMzE5NDA1N1oXDTI2MDExMDE5NDA1N1owfDEYMBYGA1UEAxMP
d3d3LmV4YW1wbGUuY29tMQswCQYDVQQIEwJTQzELMAkGA1UEBhMCVVMxIjAgBgkq
hkiG9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20xEDAOBgNVBAoTB0V4YW1wbGUx
EDAOBgNVBAsTB0V4YW1wbGUwgZ8wDQYJKoZIhvcNAQEBBQADgY0AMIGJAoGBAM0B
u++oHV1wcphWRbMLUft8fD7nPG95xs7UeLPphFZuShIhhdAQMpvcsFeg+Bg9PWCu
v3jZljmk06MLvuWLfwjYfo9q/V+qOZVfTVHHbaIO5RTXJMC2Nn+ACF0kHBmNcbth
OOgF8L854a/P8tjm1iPR++vHnkex0NH7lyosVc/vAgMBAAGjDTALMAkGA1UdEwQC
MAAwDQYJKoZIhvcNAQEFBQADggEBADjFm5AlNH3DNT1Uzx3m66fFjqqrHEs25geT
yA3rvBuynflEHQO95M/8wCxYVyuAx4Z1i4YDC7tx0vmOn/2GXZHY9MAj1I8KCnwt
Jik7E2r1/yY0MrkawljOAxisXs821kJ+Z/51Ud2t5uhGxS6hJypbGspMS7OtBbw7
8oThK7cWtCXOldNF6ruqY1agWnhRdAq5qSMnuBXuicOP0Kbtx51a1ugE3SnvQenJ
nZxdtYUXvEsHZC/6bAtTfNh+/SwgxQJuL2ZM+VG3X2JIKY8xTDui+il7uTh422lq
wED8uwKl+bOj6xFDyw4gWoBxRobsbFaME8pkykP1+GnKDberyAM=
-----END CERTIFICATE-----`

	testExpiredCertPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIICWwIBAAKBgQDNAbvvqB1dcHKYVkWzC1H7fHw+5zxvecbO1Hiz6YRWbkoSIYXQ
EDKb3LBXoPgYPT1grr942ZY5pNOjC77li38I2H6Pav1fqjmVX01Rx22iDuUU1yTA
tjZ/gAhdJBwZjXG7YTjoBfC/OeGvz/LY5tYj0fvrx55HsdDR+5cqLFXP7wIDAQAB
AoGAfE7P4Zsj6zOzGPI/Izj7Bi5OvGnEeKfzyBiH9Dflue74VRQkqqwXs/DWsNv3
c+M2Y3iyu5ncgKmUduo5X8D9To2ymPRLGuCdfZTxnBMpIDKSJ0FTwVPkr6cYyyBk
5VCbc470pQPxTAAtl2eaO1sIrzR4PcgwqrSOjwBQQocsGAECQQD8QOra/mZmxPbt
bRh8U5lhgZmirImk5RY3QMPI/1/f4k+fyjkU5FRq/yqSyin75aSAXg8IupAFRgyZ
W7BT6zwBAkEA0A0ugAGorpCbuTa25SsIOMxkEzCiKYvh0O+GfGkzWG4lkSeJqGME
keuJGlXrZNKNoCYLluAKLPmnd72X2yTL7wJARM0kAXUP0wn324w8+HQIyqqBj/gF
Vt9Q7uMQQ3s72CGu3ANZDFS2nbRZFU5koxrggk6lRRk1fOq9NvrmHg10AQJABOea
pgfj+yGLmkUw8JwgGH6xCUbHO+WBUFSlPf+Y50fJeO+OrjqPXAVKeSV3ZCwWjKT4
9viXJNJJ4WfF0bO/XwJAOMB1wQnEOSZ4v+laMwNtMq6hre5K8woqteXICoGcIWe8
u3YLAbyW/lHhOCiZu2iAI8AbmXem9lW6Tr7p/97s0w==
-----END RSA PRIVATE KEY-----`

	testCertificate = `-----BEGIN CERTIFICATE-----
MIICwjCCAiugAwIBAgIBATANBgkqhkiG9w0BAQsFADBjMQswCQYDVQQGEwJVUzEL
MAkGA1UECAwCQ0ExETAPBgNVBAoMCFNlY3VyaXR5MRswGQYDVQQLDBJPcGVuU2hp
ZnQzIHRlc3QgQ0ExFzAVBgNVBAMMDmhlYWRlci50ZXN0IENBMB4XDTE2MDMxMjA0
MjEwM1oXDTM2MDMxMjA0MjEwM1owWDEUMBIGA1UEAwwLaGVhZGVyLnRlc3QxCzAJ
BgNVBAgMAkNBMQswCQYDVQQGEwJVUzERMA8GA1UECgwIU2VjdXJpdHkxEzARBgNV
BAsMCk9wZW5TaGlmdDMwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQD0
XEAzUMflZy8zluwzqMKnu8jYK3yUoEGLN0Bw0A/7ydno1g0E92ee8M9p59TCCWA6
nKnt1DEK5285xAKs9AveutSYiDkpf2px59GvCVx2ecfFBTECWHMAJ/6Y7pqlWOt2
hvPx5rP+jVeNLAfK9d+f57FGvWXrQAcBnFTegS6J910kbvDgNP4Nerj6RPAx2UOq
6URqA4j7qZs63nReeu/1t//BQHNokKddfxw2ZXcL/5itgpPug16thp+ugGVdjcFs
aasLJOjErUS0D+7bot98FL0TSpxWqwtCF117bSLY7UczZFNAZAOnZBFmSZBxcJJa
TZzkda0Oiqo0J3GPcZ+rAgMBAAGjDTALMAkGA1UdEwQCMAAwDQYJKoZIhvcNAQEL
BQADgYEACkdKRUm9ERjgbe6w0fw4VY1s5XC9qR1m5AwLMVVwKxHJVG2zMzeDTHyg
3cjxmfZdFU9yxmNUCh3mRsi2+qjEoFfGRyMwMMx7cduYhsFY3KA+Fl4vBRXAuPLR
eCI4ErCPi+Y08vOto9VVXg2f4YFQYLq1X6TiXD5RpQAN0t8AYk4=
-----END CERTIFICATE-----`

	testPrivateKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA9FxAM1DH5WcvM5bsM6jCp7vI2Ct8lKBBizdAcNAP+8nZ6NYN
BPdnnvDPaefUwglgOpyp7dQxCudvOcQCrPQL3rrUmIg5KX9qcefRrwlcdnnHxQUx
AlhzACf+mO6apVjrdobz8eaz/o1XjSwHyvXfn+exRr1l60AHAZxU3oEuifddJG7w
4DT+DXq4+kTwMdlDqulEagOI+6mbOt50Xnrv9bf/wUBzaJCnXX8cNmV3C/+YrYKT
7oNerYafroBlXY3BbGmrCyToxK1EtA/u26LffBS9E0qcVqsLQhdde20i2O1HM2RT
QGQDp2QRZkmQcXCSWk2c5HWtDoqqNCdxj3GfqwIDAQABAoIBAEfl+NHge+CIur+w
MXGFvziBLThFm1NTz9U5fZFz9q/8FUzH5m7GqMuASVb86oHpJlI4lFsw6vktXXGe
tbbT28Y+LJ1wv3jxT42SSwT4eSc278uNmnz5L2UlX2j6E7CA+E8YqCBN5DoKtm8I
PIbAT3sKPgP1aE6OuUEFEYeidOIMvjco2aQH0338sl6cObkQFEgnWf2ncun3KGnb
s+dMO5EdYLo0rOdDXY88sElfqiNYYl/FRu9O3OfqHvScA5uo9FlIhukcrRkbjFcq
j/7k4tt0iLs9B2j+4ihBWYo5eRFIde4Izj6a6ArEk0ShEUvwlZBuGMM/vs+jvbDK
l3+0NpECgYEA/+qxwvOGjmlYNKFK/rzxd51EnfCISnV+tb17pNyRmlGToi1/LmmV
+jcJfcwlf2o8mTFn3xAdD3fSaHF7t8Li7xDwH2S+sSuFE/8bhgHUvw1S7oILMYyO
hO6sWG+JocMhr8IejaAnQxav9VvP01YDfw/XBB0O1EIuzzr2KHq+AGMCgYEA9HCY
JGTcv7lfs3kcCAkDtjl8NbjNRMxRErG0dfYS+6OSaXOOMg1TsaSNEgjOGyUX+yQ4
4vtKcLwHk7+qz3ZPbhS6m7theZG9jUwMrQRGyCE7z3JUy8vmV/N+HP0V+boT+4KM
Tai3+I3hf9+QMHYx/Z/VA0K6f27LwP+kEL9C8hkCgYEAoiHeXNRL+w1ihHVrPdgW
YuGQBz/MGOA3VoylON1Eoa/tCGIqoQzjp5IWwUwEtaRon+VdGUTsJFCVTPYYm2Ms
wqjIeBsrdLNNrE2C8nNWhXO7hr98t/eEk1NifOStHX6yaNdi4/cC6M4GzDtOf2WO
8YDniAOg0Xjcjw2bxil9FmECgYBuUeq4cjUW6okArSYzki30rhka/d7WsAffEgjK
PFbw7zADG74PZOhjAksQ2px6r9EU7ZInDxbXrmUVD6n9m/3ZRs25v2YMwfP0s1/9
LjLr2+PsikMu/0VkaGaAmtCyNoMSPicoXX86VH5zgejHlnCVcO9oW1NkdBLNdhML
4+ZI8QKBgQDb+SH7i50Yu3adwvPkDSp3ACCzPoHXno79a7Y5S2JzpFtNq+cNLWEb
HP8gHJSZnaGrLKmjwNeQNsARYajKmDKO5HJ9g5H5Hae8enOb2yie541dneDT8rID
4054dMQJnijd8620yf8wiNy05ZPOQQ0JvA/rW3WWZc5PGm8c2PsVjg==
-----END RSA PRIVATE KEY-----`

	testCACertificate = `-----BEGIN CERTIFICATE-----
MIIClDCCAf2gAwIBAgIJAPU57OGhuqJtMA0GCSqGSIb3DQEBCwUAMGMxCzAJBgNV
BAYTAlVTMQswCQYDVQQIDAJDQTERMA8GA1UECgwIU2VjdXJpdHkxGzAZBgNVBAsM
Ek9wZW5TaGlmdDMgdGVzdCBDQTEXMBUGA1UEAwwOaGVhZGVyLnRlc3QgQ0EwHhcN
MTYwMzEyMDQyMTAzWhcNMzYwMzEyMDQyMTAzWjBjMQswCQYDVQQGEwJVUzELMAkG
A1UECAwCQ0ExETAPBgNVBAoMCFNlY3VyaXR5MRswGQYDVQQLDBJPcGVuU2hpZnQz
IHRlc3QgQ0ExFzAVBgNVBAMMDmhlYWRlci50ZXN0IENBMIGfMA0GCSqGSIb3DQEB
AQUAA4GNADCBiQKBgQCsdVIJ6GSrkFdE9LzsMItYGE4q3qqSqIbs/uwMoVsMT+33
pLeyzeecPuoQsdO6SEuqhUM1ivUN4GyXIR1+aW2baMwMXpjX9VIJu5d4FqtGi6SD
RfV+tbERWwifPJlN+ryuvqbbDxrjQeXhemeo7yrJdgJ1oyDmoM5pTiSUUmltvQID
AQABo1AwTjAdBgNVHQ4EFgQUOVuieqGfp2wnKo7lX2fQt+Yk1C4wHwYDVR0jBBgw
FoAUOVuieqGfp2wnKo7lX2fQt+Yk1C4wDAYDVR0TBAUwAwEB/zANBgkqhkiG9w0B
AQsFAAOBgQA8VhmNeicRnKgXInVyYZDjL0P4WRbKJY7DkJxRMRWxikbEVHdySki6
jegpqgJqYbzU6EiuTS2sl2bAjIK9nGUtTDt1PJIC1Evn5Q6v5ylNflpv6GxtUbCt
bGvtpjWA4r9WASIDPFsxk/cDEEEO6iPxgMOf5MdpQC2y2MU0rzF/Gg==
-----END CERTIFICATE-----`

	testDestinationCACertificate = testCACertificate
)

// TestRouter provides an implementation of the plugin's router interface suitable for unit testing.
type TestRouter struct {
	State        map[ServiceAliasConfigKey]ServiceAliasConfig
	ServiceUnits map[ServiceUnitKey]ServiceUnit
}

// NewTestRouter creates a new TestRouter and registers the initial state.
func newTestRouter(state map[ServiceAliasConfigKey]ServiceAliasConfig) *TestRouter {
	return &TestRouter{
		State:        state,
		ServiceUnits: make(map[ServiceUnitKey]ServiceUnit),
	}
}

// CreateServiceUnit creates an empty service unit identified by id
func (r *TestRouter) CreateServiceUnit(id ServiceUnitKey) {
	su := ServiceUnit{
		Name:          string(id),
		EndpointTable: []Endpoint{},
	}

	r.ServiceUnits[id] = su
}

// FindServiceUnit finds the service unit in the state
func (r *TestRouter) FindServiceUnit(id ServiceUnitKey) (v ServiceUnit, ok bool) {
	v, ok = r.ServiceUnits[id]
	return
}

// AddEndpoints adds the endpoints to the service unit identified by id
func (r *TestRouter) AddEndpoints(id ServiceUnitKey, endpoints []Endpoint) {
	su, _ := r.FindServiceUnit(id)

	// simulate the logic that compares endpoints
	if reflect.DeepEqual(su.EndpointTable, endpoints) {
		return
	}
	su.EndpointTable = endpoints
	r.ServiceUnits[id] = su
}

// DeleteEndpoints removes all endpoints from the service unit
func (r *TestRouter) DeleteEndpoints(id ServiceUnitKey) {
	if su, ok := r.FindServiceUnit(id); !ok {
		return
	} else {
		su.EndpointTable = []Endpoint{}
		r.ServiceUnits[id] = su
	}
}

func (r *TestRouter) calculateServiceWeights(serviceUnits map[ServiceUnitKey]int32) map[ServiceUnitKey]int32 {
	var serviceWeights = make(map[ServiceUnitKey]int32)
	for key := range serviceUnits {
		serviceWeights[key] = serviceUnits[key]
	}
	return serviceWeights
}

// AddRoute adds a ServiceAliasConfig and associated ServiceUnits for the route
func (r *TestRouter) AddRoute(route *routev1.Route) {
	routeKey := getKey(route)

	config := ServiceAliasConfig{
		Host:         route.Spec.Host,
		Path:         route.Spec.Path,
		ServiceUnits: getServiceUnits(route),
	}
	config.ServiceUnitNames = r.calculateServiceWeights(config.ServiceUnits)

	for key := range config.ServiceUnits {
		r.CreateServiceUnit(key)
	}

	r.State[routeKey] = config
}

// RemoveRoute removes the service alias config for Route
func (r *TestRouter) RemoveRoute(route *routev1.Route) {
	routeKey := getKey(route)
	_, ok := r.State[routeKey]
	if !ok {
		return
	} else {
		delete(r.State, routeKey)
	}
}

func (r *TestRouter) HasRoute(route *routev1.Route) bool {
	// Not used
	return false
}

func (r *TestRouter) SyncedAtLeastOnce() bool {
	// Not used
	return false
}

func (r *TestRouter) FilterNamespaces(namespaces sets.String) {
	if len(namespaces) == 0 {
		r.State = make(map[ServiceAliasConfigKey]ServiceAliasConfig)
		r.ServiceUnits = make(map[ServiceUnitKey]ServiceUnit)
	}
	for k := range r.ServiceUnits {
		// TODO: the id of a service unit should be defined inside this class, not passed in from the outside
		//   remove the leak of the abstraction when we refactor this code
		ns, _ := getPartsFromEndpointsKey(k)
		if namespaces.Has(ns) {
			continue
		}
		delete(r.ServiceUnits, k)
	}

	for k := range r.State {
		ns, _ := getPartsFromRouteKey(k)
		if namespaces.Has(ns) {
			continue
		}
		delete(r.State, k)
	}
}

// getKey create an identifier for the route consisting of host-path
func getKey(route *routev1.Route) ServiceAliasConfigKey {
	return routeKeyFromParts(route.Spec.Host, route.Spec.Path)
}

func (r *TestRouter) Commit() {
	// No op
}

// TestHandleEndpoints test endpoint watch events
func TestHandleEndpoints(t *testing.T) {
	testCases := []struct {
		name                string          //human readable name for test case
		eventType           watch.EventType //type to be passed to the HandleEndpoints method
		endpoints           *kapi.Endpoints //endpoints to be passed to the HandleEndpoints method
		expectedServiceUnit *ServiceUnit    //service unit that will be compared against.
		excludeUDP          bool
	}{
		{
			name:      "Endpoint add",
			eventType: watch.Added,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test", //kapi.endpoints inherits the name of the service
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "1.1.1.1"}},
					Ports:     []kapi.EndpointPort{{Port: 345, Name: "port"}},
				}}, //not specifying a port to force the port 80 assumption
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test", //service name from kapi.endpoints object
				EndpointTable: []Endpoint{
					{
						ID:   "ept:test:port:1.1.1.1:345",
						IP:   "1.1.1.1",
						Port: "345",
					},
				},
			},
		},
		{
			name:      "Endpoint mod",
			eventType: watch.Modified,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "2.2.2.2", TargetRef: &kapi.ObjectReference{Kind: "Pod", Name: "pod-1"}}},
					Ports:     []kapi.EndpointPort{{Port: 8080, Name: "port"}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test",
				EndpointTable: []Endpoint{
					{
						ID:   "pod:pod-1:test:port:2.2.2.2:8080",
						IP:   "2.2.2.2",
						Port: "8080",
					},
				},
			},
		},
		{
			name:      "Endpoint delete",
			eventType: watch.Deleted,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "3.3.3.3"}},
					Ports:     []kapi.EndpointPort{{Port: 0}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name:          "foo/test",
				EndpointTable: []Endpoint{},
			},
		},
	}

	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	for _, tc := range testCases {
		plugin.HandleEndpoints(tc.eventType, tc.endpoints)

		su, ok := router.FindServiceUnit(ServiceUnitKey(tc.expectedServiceUnit.Name))

		if !ok {
			t.Errorf("TestHandleEndpoints test case %s failed.  Couldn't find expected service unit with name %s", tc.name, tc.expectedServiceUnit.Name)
		} else {
			if len(su.EndpointTable) != len(tc.expectedServiceUnit.EndpointTable) {
				t.Errorf("TestHandleEndpoints test case %s failed. endpoints: %d expected %d", tc.name, len(su.EndpointTable), len(tc.expectedServiceUnit.EndpointTable))
			}
			for expectedKey, expectedEp := range tc.expectedServiceUnit.EndpointTable {
				actualEp := su.EndpointTable[expectedKey]

				if expectedEp.ID != actualEp.ID || expectedEp.IP != actualEp.IP || expectedEp.Port != actualEp.Port {
					t.Errorf("TestHandleEndpoints test case %s failed.  Expected endpoint didn't match actual endpoint %v : %v", tc.name, expectedEp, actualEp)
				}
			}
		}
	}
}

// TestHandleTCPEndpoints test endpoint watch events with UDP excluded
func TestHandleTCPEndpoints(t *testing.T) {
	testCases := []struct {
		name                string          //human readable name for test case
		eventType           watch.EventType //type to be passed to the HandleEndpoints method
		endpoints           *kapi.Endpoints //endpoints to be passed to the HandleEndpoints method
		expectedServiceUnit *ServiceUnit    //service unit that will be compared against.
	}{
		{
			name:      "Endpoint add",
			eventType: watch.Added,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test", //kapi.endpoints inherits the name of the service
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "1.1.1.1", TargetRef: &kapi.ObjectReference{Kind: "Pod", Name: "pod-1"}}},
					Ports: []kapi.EndpointPort{
						{Port: 345, Name: "tcp"},
						{Port: 346, Protocol: kapi.ProtocolUDP, Name: "udp"},
					},
				}}, //not specifying a port to force the port 80 assumption
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test", //service name from kapi.endpoints object
				EndpointTable: []Endpoint{
					{
						ID:   "pod:pod-1:test:tcp:1.1.1.1:345",
						IP:   "1.1.1.1",
						Port: "345",
					},
				},
			},
		},
		{
			name:      "Endpoint mod",
			eventType: watch.Modified,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "2.2.2.2"}},
					Ports: []kapi.EndpointPort{
						{Port: 8080, Name: "tcp"},
						{Port: 8081, Protocol: kapi.ProtocolUDP, Name: "udp"},
					},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name: "foo/test",
				EndpointTable: []Endpoint{
					{
						ID:   "ept:test:tcp:2.2.2.2:8080",
						IP:   "2.2.2.2",
						Port: "8080",
					},
				},
			},
		},
		{
			name:      "Endpoint delete",
			eventType: watch.Deleted,
			endpoints: &kapi.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "test",
				},
				Subsets: []kapi.EndpointSubset{{
					Addresses: []kapi.EndpointAddress{{IP: "3.3.3.3"}},
					Ports:     []kapi.EndpointPort{{Port: 0}},
				}},
			},
			expectedServiceUnit: &ServiceUnit{
				Name:          "foo/test",
				EndpointTable: []Endpoint{},
			},
		},
	}

	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, false, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	for _, tc := range testCases {
		plugin.HandleEndpoints(tc.eventType, tc.endpoints)

		su, ok := router.FindServiceUnit(ServiceUnitKey(tc.expectedServiceUnit.Name))

		if !ok {
			t.Errorf("TestHandleEndpoints test case %s failed.  Couldn't find expected service unit with name %s", tc.name, tc.expectedServiceUnit.Name)
		} else {
			for expectedKey, expectedEp := range tc.expectedServiceUnit.EndpointTable {
				actualEp := su.EndpointTable[expectedKey]

				if expectedEp.ID != actualEp.ID || expectedEp.IP != actualEp.IP || expectedEp.Port != actualEp.Port {
					t.Errorf("TestHandleEndpoints test case %s failed.  Expected endpoint didn't match actual endpoint %v : %v", tc.name, expectedEp, actualEp)
				}
			}
		}
	}
}

type rejection struct {
	route   *routev1.Route
	reason  string
	message string
}

type fakeRejections struct {
	rejections []rejection
}

func (r *fakeRejections) RecordRouteRejection(route *routev1.Route, reason, message string) {
	r.rejections = append(r.rejections, rejection{route: route, reason: reason, message: message})
}

// TestHandleRoute test route watch events
func TestHandleRoute(t *testing.T) {
	rejections := &fakeRejections{}
	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, rejections)

	uidCount := 0
	nextUID := func() types.UID {
		uidCount++
		return types.UID(fmt.Sprintf("%03d", uidCount))
	}
	original := metav1.Time{Time: time.Now()}

	//add
	fooTest1 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: original,
			Namespace:         "foo",
			Name:              "test",
			UID:               nextUID(),
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}
	serviceUnitKey := endpointsKeyFromParts(fooTest1.Namespace, fooTest1.Spec.To.Name)

	plugin.HandleRoute(watch.Added, fooTest1)

	_, ok := router.FindServiceUnit(serviceUnitKey)

	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1.Spec.To.Name)
	} else {
		serviceAliasCfg, ok := router.State[getKey(fooTest1)]

		if !ok {
			t.Errorf("TestHandleRoute expected route key %s", getKey(fooTest1))
		} else {
			if serviceAliasCfg.Host != fooTest1.Spec.Host || serviceAliasCfg.Path != fooTest1.Spec.Path {
				t.Errorf("Expected route did not match service alias config %v : %v", fooTest1, serviceAliasCfg)
			}
		}
	}

	if len(rejections.rejections) > 0 {
		t.Fatalf("did not expect a recorded rejection: %#v", rejections)
	}

	// attempt to add a second route with a newer time, verify it is ignored
	fooDupe2 := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: original.Add(time.Hour)},
			Namespace:         "foo",
			Name:              "dupe",
			UID:               nextUID(),
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService2",
				Weight: new(int32),
			},
		},
	}
	if err := plugin.HandleRoute(watch.Added, fooDupe2); err != nil {
		t.Fatal("unexpected error")
	}

	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2")); ok {
		t.Fatalf("unexpected second unit: %#v", router)
	}
	if r, ok := plugin.RoutesForHost("www.example.com"); !ok || r[0].Name != "test" {
		t.Fatalf("unexpected claimed routes: %#v", r)
	}
	if len(rejections.rejections) != 1 ||
		rejections.rejections[0].route.Name != "dupe" ||
		rejections.rejections[0].reason != "HostAlreadyClaimed" ||
		rejections.rejections[0].message != "route test already exposes www.example.com and is older" {
		t.Fatalf("did not record rejection: %#v", rejections)
	}
	rejections.rejections = nil

	// attempt to remove the second route that is not being used, verify it is ignored
	if err := plugin.HandleRoute(watch.Deleted, fooDupe2); err != nil {
		t.Fatal("unexpected error")
	}

	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2")); ok {
		t.Fatalf("unexpected second unit: %#v", router)
	}
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); !ok {
		t.Fatalf("unexpected first unit: %#v", router)
	}
	if r, ok := plugin.RoutesForHost("www.example.com"); !ok || r[0].Name != "test" {
		t.Fatalf("unexpected claimed routes: %#v", r)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("did not record rejection: %#v", rejections)
	}
	rejections.rejections = nil

	// add a third route with an older time, verify it takes effect
	copied := *fooDupe2
	fooDupe3 := &copied
	fooDupe3.UID = nextUID()
	fooDupe3.CreationTimestamp = metav1.Time{Time: original.Add(-time.Hour)}
	if err := plugin.HandleRoute(watch.Added, fooDupe3); err != nil {
		t.Fatal("unexpected error")
	}
	_, ok = router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService2"))
	if !ok {
		t.Fatalf("missing second unit: %#v", router)
	}
	if len(rejections.rejections) != 1 ||
		rejections.rejections[0].route.Name != "test" ||
		rejections.rejections[0].reason != "HostAlreadyClaimed" ||
		rejections.rejections[0].message != "replaced by older route dupe" {
		t.Fatalf("did not record rejection: %#v", rejections)
	}
	rejections.rejections = nil

	//mod
	copied2 := *fooTest1
	fooTest1NewHost := &copied2
	fooTest1NewHost.Spec.Host = "www.example2.com"
	if err := plugin.HandleRoute(watch.Modified, fooTest1NewHost); err != nil {
		t.Fatal("unexpected error")
	}

	key := getKey(fooTest1NewHost)
	_, ok = router.FindServiceUnit(serviceUnitKey)
	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1NewHost.Spec.To.Name)
	} else {
		serviceAliasCfg, ok := router.State[key]
		if !ok {
			t.Errorf("TestHandleRoute expected route key %s", key)
		} else {
			if serviceAliasCfg.Host != fooTest1NewHost.Spec.Host || serviceAliasCfg.Path != fooTest1NewHost.Spec.Path {
				t.Errorf("Expected route did not match service alias config %v : %v", fooTest1NewHost, serviceAliasCfg)
			}
		}
	}
	if plugin.HostLen() != 2 {
		t.Fatalf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected rejection: %#v", rejections)
	}

	plugin.HandleRoute(watch.Deleted, fooDupe3)

	if plugin.HostLen() != 1 {
		t.Fatalf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected rejection: %#v", rejections)
	}

	//delete
	if err := plugin.HandleRoute(watch.Deleted, fooTest1NewHost); err != nil {
		t.Fatal("unexpected error")
	}
	_, ok = router.FindServiceUnit(serviceUnitKey)
	if !ok {
		t.Errorf("TestHandleRoute was unable to find the service unit %s after HandleRoute was called", fooTest1NewHost.Spec.To.Name)
	} else {

		_, ok := router.State[key]

		if ok {
			t.Errorf("TestHandleRoute did not expect route key %s", key)
		}
	}
	if plugin.HostLen() != 0 {
		t.Errorf("did not clear claimed route: %#v", plugin)
	}
	if len(rejections.rejections) != 0 {
		t.Fatalf("unexpected rejection: %#v", rejections)
	}
}

type fakePlugin struct {
	Route *routev1.Route
	Err   error
}

func (p *fakePlugin) HandleRoute(event watch.EventType, route *routev1.Route) error {
	p.Route = route
	return p.Err
}

func (p *fakePlugin) HandleEndpoints(watch.EventType, *kapi.Endpoints) error {
	return p.Err
}

func (p *fakePlugin) HandleNamespaces(namespaces sets.String) error {
	return p.Err
}

func (p *fakePlugin) HandleNode(watch.EventType, *kapi.Node) error {
	return p.Err
}

func (p *fakePlugin) Commit() error {
	return p.Err
}

// TestHandleRouteExtendedValidation test route watch events with extended route configuration validation.
func TestHandleRouteExtendedValidation(t *testing.T) {
	rejections := &fakeRejections{}
	fake := &fakePlugin{}
	plugin := controller.NewExtendedValidator(fake, rejections)

	original := metav1.Time{Time: time.Now()}

	//add
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: original,
			Namespace:         "foo",
			Name:              "test",
		},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}

	plugin.HandleRoute(watch.Added, route)
	if fake.Route != route {
		t.Fatalf("unexpected route: %#v", fake.Route)
	}

	if len(rejections.rejections) > 0 {
		t.Fatalf("did not expect a recorded rejection: %#v", rejections)
	}

	tests := []struct {
		name          string
		route         *routev1.Route
		errorExpected bool
	}{
		{
			name: "No TLS Termination",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.no.tls.test",
					TLS: &routev1.TLSConfig{
						Termination: "",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination OK",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.test",
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationPassthrough,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination OK with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.com",

					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              testCertificate,
						Key:                      testPrivateKey,
						CACertificate:            testCACertificate,
						DestinationCACertificate: testDestinationCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination OK with bad config",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencypt.badconfig.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              "def",
						Key:                      "ghi",
						CACertificate:            "jkl",
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Reencrypt termination OK without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencypt.nocerts.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: testDestinationCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Reencrypt termination bad config without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencypt.badconfignocerts.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Reencrypt termination no dest cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencypt.nodestcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationReencrypt,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination OK with certs without host",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination OK with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.example.com",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination bad config with certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.badconfig.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   "abc",
						Key:           "abc",
						CACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination mismatched key and cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.mismatchdkeyandcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testCertificate,
						Key:           testExpiredCertPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination expired cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.expiredcert.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testExpiredCAUnknownCertificate,
						Key:           testExpiredCertPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination expired cert key mismatch",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.expiredcertkeymismatch.test",
					TLS: &routev1.TLSConfig{
						Termination:   routev1.TLSTerminationEdge,
						Certificate:   testExpiredCAUnknownCertificate,
						Key:           testPrivateKey,
						CACertificate: testCACertificate,
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Edge termination OK without certs",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.nocerts.test",
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "Edge termination, bad dest cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.edge.baddestcert.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationEdge,
						DestinationCACertificate: "abc",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badcert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, Certificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad key",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badkey.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, Key: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad ca cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.badcacert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, CACertificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Passthrough termination, bad dest ca cert",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.passthrough.baddestcacert.test",
					TLS:  &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough, DestinationCACertificate: "test"},
				},
			},
			errorExpected: true,
		},
		{
			name: "Invalid termination type",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					TLS: &routev1.TLSConfig{
						Termination: "invalid",
					},
				},
			},
			errorExpected: true,
		},
		{
			name: "Double escaped newlines",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "www.reencrypt.doubleescapednewlines.test",
					TLS: &routev1.TLSConfig{
						Termination:              routev1.TLSTerminationReencrypt,
						Certificate:              "d\\nef",
						Key:                      "g\\nhi",
						CACertificate:            "j\\nkl",
						DestinationCACertificate: "j\\nkl",
					},
				},
			},
			errorExpected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := plugin.HandleRoute(watch.Added, tc.route)
			if tc.errorExpected {
				if err == nil {
					t.Fatalf("test case %s: expected an error, got none", tc.name)
				}
			} else {
				if err != nil {
					t.Fatalf("test case %s: expected no errors, got %v", tc.name, err)
				}
			}
		})
	}
}

func TestNamespaceScopingFromEmpty(t *testing.T) {
	router := newTestRouter(make(map[ServiceAliasConfigKey]ServiceAliasConfig))
	templatePlugin := newDefaultTemplatePlugin(router, true, nil)
	// TODO: move tests that rely on unique hosts to pkg/router/controller and remove them from
	// here
	plugin := controller.NewUniqueHost(templatePlugin, false, controller.LogRejections)

	// no namespaces allowed
	plugin.HandleNamespaces(sets.String{})

	//add
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "test"},
		Spec: routev1.RouteSpec{
			Host: "www.example.com",
			To: routev1.RouteTargetReference{
				Name:   "TestService",
				Weight: new(int32),
			},
		},
	}

	// ignores all events for namespace that doesn't match
	for _, s := range []watch.EventType{watch.Added, watch.Modified, watch.Deleted} {
		plugin.HandleRoute(s, route)
		if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
			t.Errorf("unexpected router state %#v", router)
		}
	}

	// allow non matching
	plugin.HandleNamespaces(sets.NewString("bar"))
	for _, s := range []watch.EventType{watch.Added, watch.Modified, watch.Deleted} {
		plugin.HandleRoute(s, route)
		if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
			t.Errorf("unexpected router state %#v", router)
		}
	}

	// allow foo
	plugin.HandleNamespaces(sets.NewString("foo", "bar"))
	plugin.HandleRoute(watch.Added, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); !ok || plugin.HostLen() != 1 {
		t.Errorf("unexpected router state %#v", router)
	}

	// forbid foo, and make sure it's cleared
	plugin.HandleNamespaces(sets.NewString("bar"))
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
	plugin.HandleRoute(watch.Modified, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
	plugin.HandleRoute(watch.Added, route)
	if _, ok := router.FindServiceUnit(endpointsKeyFromParts("foo", "TestService")); ok || plugin.HostLen() != 0 {
		t.Errorf("unexpected router state %#v", router)
	}
}
