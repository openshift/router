package router_test

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	fakesm "github.com/openshift/library-go/pkg/route/secretmanager/fake"

	projectfake "github.com/openshift/client-go/project/clientset/versioned/fake"
	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	routefake "github.com/openshift/client-go/route/clientset/versioned/fake"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

	kclientset "k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"k8s.io/klog/v2"

	haproxyconfparser "github.com/haproxytech/config-parser/v4"
	haproxyconfparseroptions "github.com/haproxytech/config-parser/v4/options"
	haproxyconfparsertypes "github.com/haproxytech/config-parser/v4/types"
	routercmd "github.com/openshift/router/pkg/cmd/infra/router"
	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/controller"
	templateplugin "github.com/openshift/router/pkg/router/template"
	"github.com/openshift/router/pkg/router/writerlease"
)

type harness struct {
	client      kclientset.Interface
	routeClient routeclient.Interface

	namespace string
	uidCount  int
	workdir   string
	dirs      map[string]string
}

func (h *harness) nextUID() types.UID {
	h.uidCount++
	return types.UID(fmt.Sprintf("%03d", h.uidCount))
}

var h *harness

var reloadInterval = time.Duration(100 * time.Millisecond)

func TestMain(m *testing.M) {
	logFlags := flag.FlagSet{}
	klog.InitFlags(&logFlags)
	if err := logFlags.Set("v", "6"); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Client stuff
	client := kubefake.NewSimpleClientset()
	routeClient := routefake.NewSimpleClientset()
	projectClient := projectfake.NewSimpleClientset()

	namespace := "default"

	h = &harness{
		client:      client,
		routeClient: routeClient,
		namespace:   namespace,
	}

	// Other shared junk
	routerSelection := &routercmd.RouterSelection{}
	factory := routerSelection.NewFactory(routeClient, projectClient.ProjectV1().Projects(), client)
	informer := factory.CreateRoutesSharedInformer()
	routeLister := routelisters.NewRouteLister(informer.GetIndexer())
	lease := writerlease.New(time.Minute, 3*time.Second)
	go lease.Run(wait.NeverStop)
	tracker := controller.NewSimpleContentionTracker(informer, namespace, 60*time.Second)
	tracker.SetConflictMessage(fmt.Sprintf("The router detected another process is writing conflicting updates to route status with name %q. Please ensure that the configuration of all routers is consistent. Route status will not be updated as long as conflicts are detected.", namespace))
	go tracker.Run(wait.NeverStop)

	var plugin router.Plugin

	workdir, err := ioutil.TempDir("", "router")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("router working directory: %s\n", workdir)

	h.workdir = workdir
	h.dirs = map[string]string{
		"whitelist": filepath.Join(workdir, "router", "whitelists"),
		"certs":     filepath.Join(workdir, "router", "certs"),
	}

	createRouterDirs()

	// The template plugin which is wrapped
	svcFetcher := templateplugin.NewListWatchServiceLookup(client.CoreV1(), 60*time.Second, namespace)
	pluginCfg := templateplugin.TemplatePluginConfig{
		WorkingDir: workdir,
		DefaultCertificate: `-----BEGIN CERTIFICATE-----
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
-----END CERTIFICATE-----
-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----
`,
		DefaultCertificateDir: h.dirs["certs"],
		ReloadFn:              func(shutdown bool) error { return nil },
		TemplatePath:          "../../images/router/haproxy/conf/haproxy-config.template",
		ReloadInterval:        reloadInterval,
		HTTPResponseHeaders: []templateplugin.HTTPHeader{{
			Name:   "x-foo",
			Value:  "'bar'",
			Action: "Set",
		}},
		HTTPRequestHeaders: []templateplugin.HTTPHeader{{
			Name:   "x-quoted",
			Value:  `'"shouldn'\''t break"'`,
			Action: "Set",
		}},
		SecretManager: &fakesm.SecretManager{},
	}
	plugin, err = templateplugin.NewTemplatePlugin(pluginCfg, svcFetcher)
	if err != nil {
		fmt.Println(err)
		os.RemoveAll(workdir)
		os.Exit(1)
	}

	// Wrap the template plugin with other stuff
	statusPlugin := controller.NewStatusAdmitter(plugin, routeClient.RouteV1(), routeLister, "default", "example.com", lease, tracker)
	plugin = statusPlugin
	plugin = controller.NewUniqueHost(plugin, routerSelection.DisableNamespaceOwnershipCheck, statusPlugin)
	plugin = controller.NewHostAdmitter(plugin, routerSelection.RouteAdmissionFunc(), false, false, statusPlugin)

	// Start the controller
	c := factory.Create(plugin, false, wait.NeverStop)
	c.Run()

	exitCode := m.Run()
	os.RemoveAll(workdir)
	os.Exit(exitCode)
}

func TestAdmissionEdgeCases(t *testing.T) {
	start := time.Now()

	tests := map[string][]expectation{
		"deletion promotes inactive routes": {
			mustCreateRoute{name: "a", host: "example.com", path: "", time: start},
			mustCreateRoute{name: "b", host: "example.com", path: "/foo", time: start.Add(1 * time.Minute)},
			mustCreateRoute{name: "c", host: "example.com", path: "/foo", time: start.Add(2 * time.Minute)},
			mustCreateRoute{name: "d", host: "example.com", path: "/foo", time: start.Add(3 * time.Minute)},
			mustCreateRoute{name: "e", host: "example.com", path: "/bar", time: start.Add(4 * time.Minute)},

			expectAdmitted{"a", "b", "e"},
			expectRejected{"c", "d"},

			mustDelete{"b"},

			expectAdmitted{"a", "c", "e"},
			expectRejected{"d"},

			mustDelete{"c"},

			expectAdmitted{"a", "d", "e"},

			mustDelete{"e"},

			expectAdmitted{"a", "d"},
		},
	}

	defer cleanUpRoutes(t)

	for name, expectations := range tests {
		for _, expectation := range expectations {
			err := expectation.Apply(h)
			if err != nil {
				t.Fatalf("%s failed: %v", name, err)
			}
		}
	}
}

func TestConfigTemplate(t *testing.T) {
	// watching for errors
	caughtErrors := []error{}
	errCh, stopCh := make(chan error), make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func(ch chan error, stop chan struct{}, wg *sync.WaitGroup) {
		defer wg.Done()
		for {
			select {
			case err := <-ch:
				caughtErrors = append(caughtErrors, err)
			case <-stop:
				return
			}
		}
	}(errCh, stopCh, wg)

	// adding custom handler which pipes to the error channel
	errHandlersBefore := utilruntime.ErrorHandlers
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, (&pipeErrorHandler{errCh}).handle)
	defer func() { utilruntime.ErrorHandlers = errHandlersBefore }()

	// create routes whose settings would add some additional blocks to the conf
	start := time.Now()
	tests := map[string][]mustCreateWithConfig{
		"Long whitelist of IPs": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "a",
					host: "aexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/ip_whitelist": getDummyIPs(100),
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "a"),
					attribute:   "acl",
					value:       "whitelist src -f " + filepath.Join(h.dirs["whitelist"], h.namespace+":a.txt"),
				},
			},
		},
		"Whitelist of mixed IPs": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "a1",
					host: "a1example.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/ip_whitelist": "192.168.1.0 2001:0db8:85a3:0000:0000:8a2e:0370:7334 172.16.14.10/24 2001:0db8:85a3::8a2e:370:10/64 64:ff9b::192.168.0.1 2600:14a0::/40",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "a1"),
					attribute:   "acl",
					value:       "whitelist src 192.168.1.0 2001:0db8:85a3:0000:0000:8a2e:0370:7334 172.16.14.10/24 2001:0db8:85a3::8a2e:370:10/64 64:ff9b::192.168.0.1 2600:14a0::/40",
				},
			},
		},
		"Simple HSTS header": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "b",
					host: "bexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "max-age=99999;includeSubDomains;preload",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "b"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age=99999;includeSubDomains;preload'`,
				},
			},
		},
		"Simple HSTS header 2": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "b2",
					host: "b2example.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "max-age=99999;includeSubDomains",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "b2"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age=99999;includeSubDomains'`,
				},
			},
		},
		"Case insensitive, with white spaces HSTS header": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "c",
					host: "cexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "max-age=99999 ;  includesubdomains;  PREload",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "c"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age=99999 ;  includesubdomains;  PREload'`,
				},
			},
		},
		"Quotes in HSTS header": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "d",
					host: "dexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": `max-age="99999"`,
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "d"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age="99999"'`,
				},
			},
		},
		"Equal sign with LWS in HSTS header": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "f",
					host: "fexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": `max-age  =  "99999"`,
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "f"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age  =  "99999"'`,
				},
			},
		},
		"Required directive missing": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "g",
					host: "gexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "min-age=99999",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "g"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security`,
					notFound:    true,
				},
			},
		},
		// test cases to be revised once HSTS pattern is fully compliant to RFC6797#section-6.1
		"Wrong HSTS header directive": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "h",
					host: "hexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "max-age=99999;includesubdomains;preload;wrongdirective",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "h"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age=99999;includesubdomains;preload;wrongdirective'`,
					notFound:    true,
				},
			},
		},
		"Typo in HSTS header directive": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "i",
					host: "iexample.com",
					path: "",
					time: start,
					annotations: map[string]string{
						"haproxy.router.openshift.io/hsts_header": "max-age=99999;includesubdomain",
					},
					tlsTermination: routev1.TLSTerminationEdge,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: edgeBackendName(h.namespace, "i"),
					attribute:   "http-response",
					value:       `set-header Strict-Transport-Security 'max-age=99999;includesubdomain'`,
					notFound:    true,
				},
			},
		},
		"Simple global HTTP request header": {
			mustCreateWithConfig{
				mustMatchConfig: mustMatchConfig{
					section:     "frontend",
					sectionName: "public",
					attribute:   "http-response",
					value:       `set-header x-foo 'bar'`,
				},
			},
		},
		"Quotes in global HTTP response header": {
			mustCreateWithConfig{
				mustMatchConfig: mustMatchConfig{
					section:     "frontend",
					sectionName: "public",
					attribute:   "http-request",
					value:       `set-header x-quoted '"shouldn'\''t break"'`,
				},
			},
		},
		"Route HTTP request header with a format": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "j",
					host: "jexample.com",
					httpHeaders: routev1.RouteHTTPHeaders{
						Actions: routev1.RouteHTTPHeaderActions{
							Request: []routev1.RouteHTTPHeader{{
								Name: "X-SSL-Client-Cert",
								Action: routev1.RouteHTTPHeaderActionUnion{
									Type: "Set",
									Set: &routev1.RouteSetHTTPHeader{
										Value: "%{+Q}[ssl_c_der,base64]",
									},
								},
							}},
						},
					},
					time: start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "j"),
					attribute:   "http-request",
					value:       `set-header 'X-SSL-Client-Cert' '%{+Q}[ssl_c_der,base64]'`,
				},
			},
		},
		"Route HTTP response header with 'if'": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "k",
					host: "kexample.com",
					httpHeaders: routev1.RouteHTTPHeaders{
						Actions: routev1.RouteHTTPHeaderActions{
							Response: []routev1.RouteHTTPHeader{{
								Name: "x-foo",
								Action: routev1.RouteHTTPHeaderActionUnion{
									Type: "Set",
									Set: &routev1.RouteSetHTTPHeader{
										Value: "foo if bar",
									},
								},
							}},
						},
					},
					time: start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "k"),
					attribute:   "http-response",
					value:       `set-header 'x-foo' 'foo if bar'`,
				},
			},
		},
		"Route HTTP response header with apostrophe, double-quotes, and backslash": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name: "l",
					host: "lexample.com",
					httpHeaders: routev1.RouteHTTPHeaders{
						Actions: routev1.RouteHTTPHeaderActions{
							Response: []routev1.RouteHTTPHeader{{
								Name: "x-quoted",
								Action: routev1.RouteHTTPHeaderActionUnion{
									Type: "Set",
									Set: &routev1.RouteSetHTTPHeader{
										Value: `"shouldn't break"\`,
									},
								},
							}},
						},
					},
					time: start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "l"),
					attribute:   "http-response",
					value:       `set-header 'x-quoted' '"shouldn'\''t break"\'`,
				},
			},
		},
		"two routes with different certificates": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name:           "m1",
					host:           "m1example.com",
					path:           "",
					time:           start,
					tlsTermination: routev1.TLSTerminationEdge,
					cert:           "m1example PEM data",
				},
				mustMatchConfig: mustMatchConfig{
					mapFile: "cert_config.map",
					value:   fmt.Sprintf("%s [alpn h2,http/1.1] m1example.com", filepath.Join(h.dirs["certs"], "default:m1.pem")),
				},
			},
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name:           "m2",
					host:           "m2example.com",
					path:           "",
					time:           start,
					tlsTermination: routev1.TLSTerminationEdge,
					cert:           "m2example PEM data",
				},
				mustMatchConfig: mustMatchConfig{
					mapFile: "cert_config.map",
					value:   fmt.Sprintf("%s [alpn h2,http/1.1] m2example.com", filepath.Join(h.dirs["certs"], "default:m2.pem")),
				},
			},
		},
		"two routes with the same certificate": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name:           "n1",
					host:           "n1example.com",
					path:           "",
					time:           start,
					tlsTermination: routev1.TLSTerminationEdge,
					cert:           "n1example PEM data",
				},
				mustMatchConfig: mustMatchConfig{
					mapFile: "cert_config.map",
					value:   fmt.Sprintf("%s n1example.com", filepath.Join(h.dirs["certs"], "default:n1.pem")),
				},
			},
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name:           "n2",
					host:           "n2example.com",
					path:           "",
					time:           start,
					tlsTermination: routev1.TLSTerminationEdge,
					cert:           "n1example PEM data",
				},
				mustMatchConfig: mustMatchConfig{
					mapFile: "cert_config.map",
					value:   fmt.Sprintf("%s n2example.com", filepath.Join(h.dirs["certs"], "default:n2.pem")),
				},
			},
		},
		"route with the default certificate": {
			mustCreateWithConfig{
				mustCreateRoute: mustCreateRoute{
					name:           "o",
					host:           "oexample.com",
					path:           "",
					time:           start,
					tlsTermination: routev1.TLSTerminationEdge,
					cert: func() string {
						defaultCertFileName := filepath.Join(h.workdir, "router", "certs", "default.pem")
						content, err := ioutil.ReadFile(defaultCertFileName)
						if err != nil {
							t.Fatal(err)
						}
						return string(content)
					}(),
				},
				mustMatchConfig: mustMatchConfig{
					mapFile: "cert_config.map",
					value:   fmt.Sprintf("%s oexample.com", filepath.Join(h.dirs["certs"], "default:o.pem")),
				},
			},
		},
		"route with appProtocol: unknown-value": {
			mustCreateWithConfig{
				mustCreateEndpointSlice: mustCreateEndpointSlice{
					name:        "servicep1",
					serviceName: "servicep1",
					appProtocol: "unknown-value",
				},
				mustCreateRoute: mustCreateRoute{
					name:              "p1",
					host:              "p1example.com",
					targetServiceName: "servicep1",
					time:              start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "p1"),
					attribute:   "server",
					value:       "proto h2",
					notFound:    true,
				},
			},
		},
		"route with appProtocol: h2c": {
			mustCreateWithConfig{
				mustCreateEndpointSlice: mustCreateEndpointSlice{
					name:        "servicep2",
					serviceName: "servicep2",
					appProtocol: "h2c",
				},
				mustCreateRoute: mustCreateRoute{
					name:              "p2",
					host:              "p2example.com",
					targetServiceName: "servicep2",
					time:              start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "p2"),
					attribute:   "server",
					value:       "proto h2",
				},
			},
		},
		"route with appProtocol: kubernetes.io/h2c": {
			mustCreateWithConfig{
				mustCreateEndpointSlice: mustCreateEndpointSlice{
					name:        "servicep3",
					serviceName: "servicep3",
					appProtocol: "kubernetes.io/h2c",
				},
				mustCreateRoute: mustCreateRoute{
					name:              "p3",
					host:              "p3example.com",
					targetServiceName: "servicep3",
					time:              start,
				},
				mustMatchConfig: mustMatchConfig{
					section:     "backend",
					sectionName: insecureBackendName(h.namespace, "p3"),
					attribute:   "server",
					value:       "proto h2",
				},
			},
		},
	}

	defer cleanUpRoutes(t)

	for name, expectations := range tests {
		for _, expectation := range expectations {
			if !reflect.DeepEqual(expectation.mustCreateEndpointSlice, mustCreateEndpointSlice{}) {
				err := expectation.mustCreateEndpointSlice.Apply(h)
				if err != nil {
					t.Fatalf("%s mustCreateEndpointSlice failed: %v", name, err)
				}
			}
			if !reflect.DeepEqual(expectation.mustCreateRoute, mustCreateRoute{}) {
				err := expectation.mustCreateRoute.Apply(h)
				if err != nil {
					t.Fatalf("%s mustCreateRoute failed: %v", name, err)
				}
			}
		}
	}

	// let the router reload
	time.Sleep(reloadInterval * 2)

	stopCh <- struct{}{}
	wg.Wait()

	// check for errors
	for _, e := range caughtErrors {
		if strings.Contains(e.Error(), "error executing template") {
			t.Fatalf("Template execution failed: %v", e)
		}
	}

	// check the generated config
	config := filepath.Join(h.workdir, "conf", "haproxy.config")
	parser, err := haproxyconfparser.New(haproxyconfparseroptions.Path(config))
	if err != nil {
		t.Fatalf("Failed to parse the generated config: %v", err)
	}

	for name, expectations := range tests {
		for _, expectation := range expectations {
			t.Run(name, func(t *testing.T) {
				if err := expectation.Match(parser); err != nil {
					fileName := config
					if len(expectation.mustMatchConfig.mapFile) != 0 {
						fileName = filepath.Join(h.workdir, "conf", expectation.mustMatchConfig.mapFile)
					}
					if content, err := ioutil.ReadFile(fileName); err != nil {
						t.Error(err)
					} else {
						t.Logf("%s:\n%s", fileName, string(content))
					}
					t.Fatal(err.Error())
				}
			})
		}
	}
}

type expectation interface {
	Apply(h *harness) error
}

// mustCreateRoute represents a route that gets created in a unit test.
type mustCreateRoute struct {
	// name is the metadata.name of the route.  If name is empty, no route
	// is created.
	name string
	// host is the spec.host of the route.
	host string
	// path is the spec.path of the route.
	path string
	// targetServiceName is the spec.to.name of the route.  If this field
	// is empty, a name is generated based on the route's name.
	targetServiceName string
	// time is the metadata.creationTimestamp of the route.
	time time.Time
	// annotations is the metadata.annotations of the route.
	annotations map[string]string
	// tlsTermination is the spec.tls.type of the route.  If this is empty,
	// spec.tls will be nil.
	tlsTermination routev1.TLSTerminationType
	// cert is the spec.tls.certificate of the route.  It should be
	// specified only if tlsTermination is "edge" or "reencrypt".
	cert string
	// httpHeaders is the spec.httpHeaders of the route.
	httpHeaders routev1.RouteHTTPHeaders
}

func (e mustCreateRoute) Apply(h *harness) error {
	if e.name == "" {
		return nil
	}
	annotations := map[string]string{}
	if e.annotations != nil {
		annotations = e.annotations
	}
	tlsConfig := &routev1.TLSConfig{}
	if e.tlsTermination != "" {
		tlsConfig = &routev1.TLSConfig{
			Termination: routev1.TLSTerminationType(e.tlsTermination),
			Certificate: e.cert,
		}
	}
	serviceName := "service" + e.name
	if e.targetServiceName != "" {
		serviceName = e.targetServiceName
	}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: metav1.Time{Time: e.time},
			Namespace:         h.namespace,
			Name:              e.name,
			UID:               h.nextUID(),
			Annotations:       annotations,
		},
		Spec: routev1.RouteSpec{
			Host: e.host,
			Path: e.path,
			To: routev1.RouteTargetReference{
				Name:   serviceName,
				Weight: new(int32),
			},
			WildcardPolicy: routev1.WildcardPolicyNone,
			TLS:            tlsConfig,
			HTTPHeaders:    &e.httpHeaders,
		},
	}
	_, err := h.routeClient.RouteV1().Routes(route.Namespace).Create(context.TODO(), route, metav1.CreateOptions{})
	return err
}

// mustCreateEndpointSlice represents an endpointslice that gets created in a unit test.
type mustCreateEndpointSlice struct {
	// name is the metadata.name of the endpointslice.  If name is empty,
	// no endpointsslice is created.
	name string
	// serviceName is the name of the associated service.  This value is
	// used as the value of the kubernetes.io/service-name label.
	serviceName string
	// appProtocol is the appProtocol of the endpointslice.
	appProtocol string
}

func (e mustCreateEndpointSlice) Apply(h *harness) error {
	if e.name == "" {
		return nil
	}
	var appProtocol *string
	if e.appProtocol != "" {
		appProtocol = &e.appProtocol
	}
	ep := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: h.namespace,
			Name:      e.name,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: e.serviceName,
			},
			UID: h.nextUID(),
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"1.1.1.1"},
		}},
		Ports: []discoveryv1.EndpointPort{{
			AppProtocol: appProtocol,
		}},
	}
	_, err := h.client.DiscoveryV1().EndpointSlices(ep.Namespace).Create(context.TODO(), ep, metav1.CreateOptions{})
	return err
}

type mustCreateWithConfig struct {
	mustCreateEndpointSlice
	mustCreateRoute
	mustMatchConfig
}

// mustMatchConfig uses HAProxy's config parser to find config snippets
type mustMatchConfig struct {
	// mapFile specifies a map file to search.  If empty, haproxy.config is
	// searched.
	mapFile string
	// section specifies a section, such as "backend" or "frontend", to
	// match on in haproxy.config.  If empty, mapFile should be specified.
	section string
	// sectionName specifies a specific backend or frontend name to match on
	// in haproxy.config.
	sectionName string
	// attribute is an haproxy.config parameter to match on.
	attribute string
	// value specifies an haproxy.config attribute value or map file entry
	// to check for.
	value string
	// notFound indicates whether the expectation is that value be present
	// or that it be absent.
	notFound bool
}

func (m mustMatchConfig) Match(parser haproxyconfparser.Parser) error {
	switch {
	case len(m.mapFile) != 0:
		return matchMapFile(m.mapFile, m.value, m.notFound)
	case len(m.section) != 0:
		return matchConfig(m, parser)
	default:
		return fmt.Errorf("match config does not specify a map file or config section: %v", m)
	}
}

func matchConfig(m mustMatchConfig, parser haproxyconfparser.Parser) error {
	data, err := parser.Get(haproxyconfparser.Section(m.section), m.sectionName, m.attribute)
	if err != nil {
		if m.notFound {
			return nil
		}
		return fmt.Errorf("unable to find requested config attribute: [%s], error: %v", m, err)
	}

	contains := false
	switch data := data.(type) {
	case []haproxyconfparsertypes.HTTPAction:
		for _, a := range data {
			if a.String() == m.value {
				contains = true
				break
			}
		}
	case []haproxyconfparsertypes.ACL:
		for _, a := range data {
			if a.Name+" "+a.Criterion+" "+a.Value == m.value {
				contains = true
				break
			}
		}
	case []haproxyconfparsertypes.Server:
		for _, a := range data {
			for _, b := range a.Params {
				contains = contains || b.String() == m.value
			}
		}

	}

	if !contains && !m.notFound {
		return fmt.Errorf("config from section %s is expected but not found: [%s]", m.Section(), m)
	}

	if contains && m.notFound {
		return fmt.Errorf("config from section %s is unexpected but found: [%s]", m.Section(), m)
	}

	return nil
}

func matchMapFile(mapFileName, entry string, notFound bool) error {
	fileName := filepath.Join(h.workdir, "conf", mapFileName)

	content, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	contains := false
	for _, line := range strings.Split(string(content), "\n") {
		if line == entry {
			contains = true
			break
		}
	}

	if !contains && !notFound {
		return fmt.Errorf("expected entry not found in map file %s: %s", mapFileName, entry)
	}

	if contains && notFound {
		return fmt.Errorf("unexpected entry found in map file %s: %s", mapFileName, entry)
	}

	return nil
}

func (m mustMatchConfig) Section() string {
	return m.section + " " + m.sectionName
}

func (m mustMatchConfig) String() string {
	return m.attribute + " " + m.value
}

type mustDelete []string

func (e mustDelete) Apply(h *harness) error {
	for _, name := range e {
		if err := h.routeClient.RouteV1().Routes(h.namespace).Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

type expectAdmitted []string

func (e expectAdmitted) Apply(h *harness) error {
	for i := range e {
		if err := assertAdmitted(h, e[i], true); err != nil {
			return err
		}
	}
	return nil
}

type expectRejected []string

func (e expectRejected) Apply(h *harness) error {
	for i := range e {
		if err := assertAdmitted(h, e[i], false); err != nil {
			return err
		}
	}
	return nil
}

func assertAdmitted(h *harness, name string, admitted bool) error {
	err := wait.PollImmediate(1*time.Second, 10*time.Second, func() (bool, error) {
		route, err := h.routeClient.RouteV1().Routes(h.namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if len(route.Status.Ingress) == 0 || len(route.Status.Ingress[0].Conditions) == 0 {
			return false, nil
		}
		cond := route.Status.Ingress[0].Conditions[0]
		var expected corev1.ConditionStatus
		if admitted {
			expected = corev1.ConditionTrue
		} else {
			expected = corev1.ConditionFalse
		}
		done := cond.Type == routev1.RouteAdmitted && cond.Status == expected
		return done, nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for route %s/%s to be admitted=%v", h.namespace, name, admitted)
	}
	return nil
}

type pipeErrorHandler struct {
	pipe chan error
}

func (e *pipeErrorHandler) handle(err error) {
	e.pipe <- err
}

func getDummyIPs(num int) string {
	subnet := "192.168.0."
	list := make([]string, 0, num)
	for i := 0; i < num; i++ {
		list = append(list, subnet+strconv.Itoa(i))
	}
	return strings.Join(list, " ")
}

func cleanUpRoutes(t *testing.T) {
	routes, err := h.routeClient.RouteV1().Routes(h.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Errorf("Failed to list routes: %v", err)
		return
	}
	for _, r := range routes.Items {
		if err := h.routeClient.RouteV1().Routes(h.namespace).Delete(context.TODO(), r.Name, metav1.DeleteOptions{}); err != nil {
			t.Errorf("Failed to delete route: %v", err)
		}
	}
}

// creates dirs used by the router, best effort for now (just to not see error logs)
func createRouterDirs() {
	for _, d := range h.dirs {
		os.MkdirAll(d, 0775)
	}
}

// edgeBackendName contructs the HAProxy config's backend name for an edge route
func edgeBackendName(ns, route string) string {
	return "be_edge_http:" + ns + ":" + route
}

// insecureBackendName contructs the HAProxy config's backend name for an
// insecure route.
func insecureBackendName(ns, route string) string {
	return "be_http:" + ns + ":" + route
}
