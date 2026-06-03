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
MIID2TCCAsGgAwIBAgIUYJ0wIIT146/6EWey/S+YLeKWTIgwDQYJKoZIhvcNAQEL
BQAwfDEYMBYGA1UEAwwPd3d3LmV4YW1wbGUuY29tMRAwDgYDVQQKDAdFeGFtcGxl
MRAwDgYDVQQLDAdFeGFtcGxlMQswCQYDVQQIDAJTQzELMAkGA1UEBhMCVVMxIjAg
BgkqhkiG9w0BCQEWE2V4YW1wbGVAZXhhbXBsZS5jb20wHhcNMjYwNjAzMDkzODA2
WhcNNDYwNTI5MDkzODA2WjB8MRgwFgYDVQQDDA93d3cuZXhhbXBsZS5jb20xEDAO
BgNVBAoMB0V4YW1wbGUxEDAOBgNVBAsMB0V4YW1wbGUxCzAJBgNVBAgMAlNDMQsw
CQYDVQQGEwJVUzEiMCAGCSqGSIb3DQEJARYTZXhhbXBsZUBleGFtcGxlLmNvbTCC
ASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKouO1HftwSIDLR9cw+hZtht
QcumMvp1wvQRCR1o9nbTGtmLbmyEO+tOVc4dout1hNvWF1XDzT4NbhImOKjj/b2C
ybvayv0gL3UULD8LtCgLJRbxBHAtgY8G5GBjOf9J7lcex/ptA2iWXKvmZAAHjYfU
ond07JSY4NwQ+q2GSaCSE+NsVfqd7oDIUcXRdd83j6XyeLJMdMweHSBs4Z35rI6t
3D3ymQQ4y5W18BDAxwwjbhYrUeLT9wfjSNmtP8mOI5kGhE/mUwWCz+Rkx3Xzz1Rj
LvfxWGM6P28BYg3Lo/F0MA8zi1Ifyies1SCz0n0eKhfbGdBrVLlt7jG63kWgXbMC
AwEAAaNTMFEwHQYDVR0OBBYEFKtuRsh2Ih23gmmsWwkrwqlvpIEIMB8GA1UdIwQY
MBaAFKtuRsh2Ih23gmmsWwkrwqlvpIEIMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZI
hvcNAQELBQADggEBABjdNKypPNDUJqVYZwwjxXvFavS9fS+K5XKMeHmpF60Fl60c
2UHXG/R55N8MVgjm7rckMzEt4Lx2J8N4aMZVOEMK3/Ir9tnBKQYlhTSUa5nKG5nD
WBCtqzK4u6CG22J2JZWlCQsvxJsYR5cl1p6uIQQWEut7MTQj1ffKmQx7GNIdjNA6
dbIjZH2RCLAkAp2VAZpd32Wycya8PIWieZBgREEVlbqCDWEq6U+tQHWpemJ6Ly/L
UpqCnKBYRPbBSiIEWbmZP7BSux4KSxEp0jdaNoqFBgbvPSJ3qG1Ki8STIiY3SSHV
Q2Mbpn2iILyNw1h3rNjkrh0Ou4SfqulE5HozoZw=
-----END CERTIFICATE-----`

	testExpiredCertPrivateKey = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCqLjtR37cEiAy0
fXMPoWbYbUHLpjL6dcL0EQkdaPZ20xrZi25shDvrTlXOHaLrdYTb1hdVw80+DW4S
Jjio4/29gsm72sr9IC91FCw/C7QoCyUW8QRwLYGPBuRgYzn/Se5XHsf6bQNollyr
5mQAB42H1KJ3dOyUmODcEPqthkmgkhPjbFX6ne6AyFHF0XXfN4+l8niyTHTMHh0g
bOGd+ayOrdw98pkEOMuVtfAQwMcMI24WK1Hi0/cH40jZrT/JjiOZBoRP5lMFgs/k
ZMd1889UYy738VhjOj9vAWINy6PxdDAPM4tSH8onrNUgs9J9HioX2xnQa1S5be4x
ut5FoF2zAgMBAAECggEAOmoPG43mdOw8LDIJbjqRIkXyeTRNuFH2vq8gSVOPkf7p
bvXgy+fh52Wmp07d7uOSXKFStjI0/5E9kIZFGZfUr5m2pEA4QAWttIrdmzBpwPr+
Wq8VPmooWA9eEcXNkRbv9ECRFSEZM+u02J6HAcmV56Nxtv5P/LuzJ2a+nRSErlQV
pXuqwUDDGh3349qVPSNGAdnnzxTcJTAPMhy3aoOmzUX7qEt5sLu6L9+qux0WeKti
Oo52UGSZH3Ll0OyKVJfCYCAgfR9aN+yqq2KBmy3YbKFXhhfN+xjKqkKeb2S/5bUB
1pPuJnlAlGiH9BmWwKsAe4YJfe9lRwk8e4btABht9QKBgQDrOv2K8vNBZZDtY0zP
qCTHdHXNa7PIm4iTaGQXUKH6j+nPntjyQjN4y0jZSysNdNXDGdHwuQ+yH9onj80K
PG/XRRQQuFy3kQJrtntRmpSMxtmBODOxz7CSKrh8GrCbOpweFO4LBzGfws0MyA5Y
blcXdOuJwhEdL8UEJsJTQpNstwKBgQC5NOSqO8mDGST3vdIVc+B5jkOM/OclmmBg
m0LIXWkRrIB5r0xWnIqhWJlLKB2M9hKLQ7eLlvKqW+mvFgkeGeN7D7k/loPfnlMU
oGHFOZl5NLO7YJmO2Zt9Q9xHcra3Lq1U+JWNMW3CFpy9FBqVvJ3OmEc0vioFHoLB
tvKBbf7S5QKBgQC7h2jQGEWTsjvq9Ios1niTxiWQIbfPSyeDlOqOp8qqbYbR7Wo5
IEvWlgG6sbFd5fHwuynihjacI8aQWZT1/x6OeNS5S7Em6uUKKA2CDgE1heWqnbqg
m9nBfWtcDQ8UgZIqbTck9ZQ7MFq2QNsm5rhpy91nEp8ALLAdUiUDqYTMWwKBgQCL
M9AyiyFYocuBUXDXovKzKlRnYaayQqfxtICrbFoOaKNf0nwEFUC1KIx/SrV7P3CM
r+cCyf+2P8MST/OmZjruQdEwlAamSq+TL0CNJk/OI+h7C44fKjuOGTU1lmjyoeix
lu2A5Afk+23vR2774HqTzyyl3dBjbJ1G0CTRV0VSaQKBgGjk8Ud/ee1N1Er7iNgU
1iON1TVWxZarl3cGCKVvl2Ru2RlG2QMev/QU9A0IECYTL4d4vUCx77J97sh9cWGQ
FQp9lCcyez5+ILaUFimQC22nTfF2cJRAbgDZJFDjTq43MWF3vsHrEhsGUcRdWSYI
96MDBzQ54uYShsa052Cs23vx
-----END PRIVATE KEY-----`

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
