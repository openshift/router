package router_test

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"

	projectfake "github.com/openshift/client-go/project/clientset/versioned/fake"
	routeclient "github.com/openshift/client-go/route/clientset/versioned"
	routefake "github.com/openshift/client-go/route/clientset/versioned/fake"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"

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
	}

	createRouterDirs()

	// The template plugin which is wrapped
	svcFetcher := templateplugin.NewListWatchServiceLookup(client.CoreV1(), 60*time.Second, namespace)
	pluginCfg := templateplugin.TemplatePluginConfig{
		WorkingDir:            workdir,
		DefaultCertificateDir: workdir,
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
			mustCreate{name: "a", host: "example.com", path: "", time: start},
			mustCreate{name: "b", host: "example.com", path: "/foo", time: start.Add(1 * time.Minute)},
			mustCreate{name: "c", host: "example.com", path: "/foo", time: start.Add(2 * time.Minute)},
			mustCreate{name: "d", host: "example.com", path: "/foo", time: start.Add(3 * time.Minute)},
			mustCreate{name: "e", host: "example.com", path: "/bar", time: start.Add(4 * time.Minute)},

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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
				mustCreate: mustCreate{
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
					value:       `set-header X-SSL-Client-Cert '%{+Q}[ssl_c_der,base64]'`,
				},
			},
		},
		"Route HTTP response header with 'if'": {
			mustCreateWithConfig{
				mustCreate: mustCreate{
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
					value:       `set-header x-foo 'foo if bar'`,
				},
			},
		},
		"Route HTTP response header with apostrophe, double-quotes, and backslash": {
			mustCreateWithConfig{
				mustCreate: mustCreate{
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
					value:       `set-header x-quoted '"shouldn'\''t break"\'`,
				},
			},
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
					if content, err := ioutil.ReadFile(config); err != nil {
						t.Error(err)
					} else {
						t.Log("haproxy.config:", string(content))
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

// mustCreate represents a route that gets created in a unit test.
type mustCreate struct {
	// name is the metadata.name of the route.  If name is empty, no route
	// is created.
	name string
	// host is the spec.host of the route.
	host string
	// path is the spec.path of the route.
	path string
	// time is the metadata.creationTimestamp of the route.
	time time.Time
	// annotations is the metadata.annotations of the route.
	annotations map[string]string
	// tlsTermination is the spec.tls.type of the route.  If this is empty,
	// spec.tls will be nil.
	tlsTermination routev1.TLSTerminationType
	// httpHeaders is the spec.httpHeaders of the route.
	httpHeaders routev1.RouteHTTPHeaders
}

func (e mustCreate) Apply(h *harness) error {
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
		}
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
				Name:   "service" + e.name,
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

type mustCreateWithConfig struct {
	mustCreate
	mustMatchConfig
}

// mustMatchConfig uses HAProxy's config parser to find config snippets
type mustMatchConfig struct {
	section     string
	sectionName string
	attribute   string
	value       string
	notFound    bool
}

func (m mustMatchConfig) Match(parser haproxyconfparser.Parser) error {
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
	}

	if !contains && !m.notFound {
		return fmt.Errorf("config from section %s is expected but not found: [%s]", m.Section(), m)
	}

	if contains && m.notFound {
		return fmt.Errorf("config from section %s is unexpected but found: [%s]", m.Section(), m)
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
