package templaterouter

import (
	"crypto/md5"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	kapi "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"

	unidlingapi "github.com/openshift/router/pkg/router/unidling"
)

const (
	// endpointsKeySeparator is used to uniquely generate key/ID for endpoints
	endpointsKeySeparator = "/"
)

// TemplatePlugin implements the router.Plugin interface to provide
// a template based, backend-agnostic router.
type TemplatePlugin struct {
	Router         RouterInterface
	IncludeUDP     bool
	ServiceFetcher ServiceLookup
}

func newDefaultTemplatePlugin(router RouterInterface, includeUDP bool, lookupSvc ServiceLookup) *TemplatePlugin {
	return &TemplatePlugin{
		Router:         router,
		IncludeUDP:     includeUDP,
		ServiceFetcher: lookupSvc,
	}
}

type TemplatePluginConfig struct {
	WorkingDir                    string
	TemplatePath                  string
	ReloadScriptPath              string
	ReloadFn                      func(shutdown bool) error
	ReloadInterval                time.Duration
	ReloadCallbacks               []func()
	DefaultCertificate            string
	DefaultCertificatePath        string
	DefaultCertificateDir         string
	DefaultDestinationCAPath      string
	StatsPort                     int
	StatsUsername                 string
	StatsPassword                 string
	IncludeUDP                    bool
	AllowWildcardRoutes           bool
	BindPortsAfterSync            bool
	MaxConnections                string
	Ciphers                       string
	StrictSNI                     bool
	DynamicConfigManager          ConfigManager
	CaptureHTTPRequestHeaders     []CaptureHTTPHeader
	CaptureHTTPResponseHeaders    []CaptureHTTPHeader
	CaptureHTTPCookie             *CaptureHTTPCookie
	HTTPHeaderNameCaseAdjustments []HTTPHeaderNameCaseAdjustment
	HTTPResponseHeaders           []HTTPHeader
	HTTPRequestHeaders            []HTTPHeader
}

// RouterInterface controls the interaction of the plugin with the underlying router implementation
type RouterInterface interface {
	// Mutative operations in this interface do not return errors.
	// The only error state for these methods is when an unknown
	// frontend key is used; all call sites make certain the frontend
	// is created.

	// SyncedAtLeastOnce indicates an initial sync has been performed
	SyncedAtLeastOnce() bool

	// CreateServiceUnit creates a new service named with the given id.
	CreateServiceUnit(id ServiceUnitKey)
	// FindServiceUnit finds the service with the given id.
	FindServiceUnit(id ServiceUnitKey) (v ServiceUnit, ok bool)

	// AddEndpoints adds new Endpoints for the given id.
	AddEndpoints(id ServiceUnitKey, endpoints []Endpoint)
	// DeleteEndpoints deletes the endpoints for the frontend with the given id.
	DeleteEndpoints(id ServiceUnitKey)

	// AddRoute attempts to add a route to the router.
	AddRoute(route *routev1.Route)
	// RemoveRoute removes the given route
	RemoveRoute(route *routev1.Route)
	// HasRoute indicates whether the router is configured with the given route
	HasRoute(route *routev1.Route) bool
	// Reduce the list of routes to only these namespaces
	FilterNamespaces(namespaces sets.String)
	// Commit applies the changes in the background. It kicks off a rate-limited
	// commit (persist router state + refresh the backend) that coalesces multiple changes.
	Commit()
}

// createTemplateWithHelper generates a new template with a map helper function.
func createTemplateWithHelper(t *template.Template) (*template.Template, error) {
	funcMap := template.FuncMap{
		"generateHAProxyMap": func(data templateData) []string {
			return generateHAProxyMap(filepath.Base(t.Name()), data)
		},
	}

	clone, err := t.Clone()
	if err != nil {
		return nil, err
	}

	return clone.Funcs(funcMap), nil
}

// NewTemplatePlugin creates a new TemplatePlugin.
func NewTemplatePlugin(cfg TemplatePluginConfig, lookupSvc ServiceLookup) (*TemplatePlugin, error) {
	templateBaseName := filepath.Base(cfg.TemplatePath)
	masterTemplate, err := template.New("config").Funcs(helperFunctions).ParseFiles(cfg.TemplatePath)
	if err != nil {
		return nil, err
	}

	templates := map[string]*template.Template{}

	for _, template := range masterTemplate.Templates() {
		if template.Name() == templateBaseName {
			continue
		}
		templateWithHelper, err := createTemplateWithHelper(template)
		if err != nil {
			return nil, err
		}

		templates[template.Name()] = templateWithHelper
	}

	templateRouterCfg := templateRouterCfg{
		dir:                           cfg.WorkingDir,
		templates:                     templates,
		reloadScriptPath:              cfg.ReloadScriptPath,
		reloadFn:                      cfg.ReloadFn,
		reloadInterval:                cfg.ReloadInterval,
		reloadCallbacks:               cfg.ReloadCallbacks,
		defaultCertificate:            cfg.DefaultCertificate,
		defaultCertificatePath:        cfg.DefaultCertificatePath,
		defaultCertificateDir:         cfg.DefaultCertificateDir,
		defaultDestinationCAPath:      cfg.DefaultDestinationCAPath,
		statsUser:                     cfg.StatsUsername,
		statsPassword:                 cfg.StatsPassword,
		statsPort:                     cfg.StatsPort,
		allowWildcardRoutes:           cfg.AllowWildcardRoutes,
		bindPortsAfterSync:            cfg.BindPortsAfterSync,
		dynamicConfigManager:          cfg.DynamicConfigManager,
		captureHTTPRequestHeaders:     cfg.CaptureHTTPRequestHeaders,
		captureHTTPResponseHeaders:    cfg.CaptureHTTPResponseHeaders,
		captureHTTPCookie:             cfg.CaptureHTTPCookie,
		httpHeaderNameCaseAdjustments: cfg.HTTPHeaderNameCaseAdjustments,
		httpResponseHeaders:           cfg.HTTPResponseHeaders,
		httpRequestHeaders:            cfg.HTTPRequestHeaders,
	}
	router, err := newTemplateRouter(templateRouterCfg)
	return newDefaultTemplatePlugin(router, cfg.IncludeUDP, lookupSvc), err
}

// Stop instructs the router plugin to stop invoking the reload method, and waits until no further
// reloads will occur. It then invokes the reload script one final time with the ROUTER_SHUTDOWN
// environment variable set with true.
func (p *TemplatePlugin) Stop() error {
	p.Router.(*templateRouter).rateLimitedCommitFunction.Stop()
	return p.Router.(*templateRouter).reloadRouter(true)
}

// HandleEndpoints processes watch events on the Endpoints resource.
func (p *TemplatePlugin) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	key := endpointsKey(endpoints)

	log.V(4).Info("processing endpoints", "endpointCount", len(endpoints.Subsets), "namespace", endpoints.Namespace, "name", endpoints.Name, "eventType", eventType)

	for i, s := range endpoints.Subsets {
		log.V(4).Info("processing subset", "index", i, "subset", s)
	}

	if _, ok := p.Router.FindServiceUnit(key); !ok {
		p.Router.CreateServiceUnit(key)
	}

	switch eventType {
	case watch.Added, watch.Modified:
		log.V(4).Info("modifying endpoints", "key", key)
		routerEndpoints := createRouterEndpoints(endpoints, !p.IncludeUDP, p.ServiceFetcher)
		key := endpointsKey(endpoints)
		p.Router.AddEndpoints(key, routerEndpoints)
	case watch.Deleted:
		log.V(4).Info("deleting endpoints", "key", key)
		p.Router.DeleteEndpoints(key)
	}

	return nil
}

// HandleNode processes watch events on the Node resource
// The template type of plugin currently does not need to act on such events
// so the implementation just returns without error
func (p *TemplatePlugin) HandleNode(eventType watch.EventType, node *kapi.Node) error {
	return nil
}

// HandleRoute processes watch events on the Route resource.
// TODO: this function can probably be collapsed with the router itself, as a function that
// determines which component needs to be recalculated (which template) and then does so
// on demand.
func (p *TemplatePlugin) HandleRoute(eventType watch.EventType, route *routev1.Route) error {
	switch eventType {
	case watch.Added, watch.Modified:
		p.Router.AddRoute(route)
	case watch.Deleted:
		log.V(4).Info("deleting route", "namespace", route.Namespace, "name", route.Name)
		p.Router.RemoveRoute(route)
	}
	return nil
}

// HandleNamespaces limits the scope of valid routes to only those that match
// the provided namespace list.
func (p *TemplatePlugin) HandleNamespaces(namespaces sets.String) error {
	p.Router.FilterNamespaces(namespaces)
	return nil
}

func (p *TemplatePlugin) Commit() error {
	p.Router.Commit()
	return nil
}

// endpointsKey returns the internal router key to use for the given Endpoints.
func endpointsKey(endpoints *kapi.Endpoints) ServiceUnitKey {
	return endpointsKeyFromParts(endpoints.Namespace, endpoints.Name)
}

func endpointsKeyFromParts(namespace, name string) ServiceUnitKey {
	return ServiceUnitKey(fmt.Sprintf("%s%s%s", namespace, endpointsKeySeparator, name))
}

func getPartsFromEndpointsKey(key ServiceUnitKey) (string, string) {
	tokens := strings.SplitN(string(key), endpointsKeySeparator, 2)
	if len(tokens) != 2 {
		log.Error(nil, "expected separator not found in endpoints key", "separator", endpointsKeySeparator, "key", key)
	}
	namespace := tokens[0]
	name := tokens[1]
	return namespace, name
}

// subsetHasAddresses returns true if subsets has any addresses.
func subsetHasAddresses(subsets []kapi.EndpointSubset) bool {
	for i := range subsets {
		if len(subsets[i].Addresses) > 0 {
			return true
		}
	}
	return false
}

// serviceIsIdled return true if the service has been annotated with
// unidlingapi.IdledAtAnnotation.
func serviceIsIdled(service *kapi.Service) bool {
	value, _ := service.Annotations[unidlingapi.IdledAtAnnotation]
	return len(value) > 0
}

// createRouterEndpoints creates openshift router endpoints based on k8s endpoints
func createRouterEndpoints(endpoints *kapi.Endpoints, excludeUDP bool, lookupSvc ServiceLookup) []Endpoint {
	// check if this service is currently idled
	wasIdled := false
	subsets := endpoints.Subsets

	if !subsetHasAddresses(subsets) {
		service, err := lookupSvc.LookupService(endpoints)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to find service %s/%s: %v", endpoints.Namespace, endpoints.Name, err))
			return []Endpoint{}
		}

		if serviceIsIdled(service) {
			if !isServiceIPSet(service) {
				utilruntime.HandleError(fmt.Errorf("headless service %s/%s was marked as idled, but cannot setup unidling without a cluster IP", endpoints.Namespace, endpoints.Name))
				return []Endpoint{}
			}

			svcSubset := kapi.EndpointSubset{
				Addresses: []kapi.EndpointAddress{
					{
						IP: service.Spec.ClusterIP,
					},
				},
			}

			for _, port := range service.Spec.Ports {
				endptPort := kapi.EndpointPort{
					Name:        port.Name,
					Port:        port.Port,
					Protocol:    port.Protocol,
					AppProtocol: port.AppProtocol,
				}
				svcSubset.Ports = append(svcSubset.Ports, endptPort)
			}

			subsets = []kapi.EndpointSubset{svcSubset}
			wasIdled = true
		}
	}

	out := make([]Endpoint, 0, len(endpoints.Subsets)*4)
	// For checking if the endpoints ID is duplicated.
	duplicated := map[string]bool{}

	// Return address as "[<address>]" if an IPv6 address,
	// otherwise address is returned unadorned.
	formatIPAddr := func(address string) string {
		if ip := net.ParseIP(address); ip != nil {
			if ip.To4() == nil && strings.Count(address, ":") >= 2 {
				return "[" + address + "]"
			}
		}
		return address
	}

	// Now build the actual endpoints we pass to the template
	for _, s := range subsets {
		for _, p := range s.Ports {
			if excludeUDP && p.Protocol == kapi.ProtocolUDP {
				continue
			}
			for _, a := range s.Addresses {
				ep := Endpoint{
					IP:   formatIPAddr(a.IP),
					Port: strconv.Itoa(int(p.Port)),

					PortName: p.Name,

					NoHealthCheck: wasIdled,
				}

				if a.TargetRef != nil {
					ep.TargetName = a.TargetRef.Name
					if a.TargetRef.Kind == "Pod" {
						ep.ID = fmt.Sprintf("pod:%s:%s:%s:%s:%d", ep.TargetName, endpoints.Name, p.Name, a.IP, p.Port)
					} else {
						ep.ID = fmt.Sprintf("ept:%s:%s:%s:%d", endpoints.Name, p.Name, a.IP, p.Port)
					}
				} else {
					ep.TargetName = a.IP
					ep.ID = fmt.Sprintf("ept:%s:%s:%s:%d", endpoints.Name, p.Name, a.IP, p.Port)
				}

				if p.AppProtocol != nil {
					ep.AppProtocol = *p.AppProtocol
				}

				// IdHash contains an obfuscated internal IP address
				// that is the value passed in the cookie. The IP address
				// is made more difficult to extract by including other
				// internal information in the hash.
				s := ep.ID
				ep.IdHash = fmt.Sprintf("%x", md5.Sum([]byte(s)))

				// Add only not duplicated endpoints.
				if !duplicated[ep.ID] {
					out = append(out, ep)
					duplicated[ep.ID] = true
				} else {
					log.V(4).Info("skip a duplicated endpoints to add", ep.ID)
				}
			}
		}
	}

	return out
}

func isServiceIPSet(service *kapi.Service) bool {
	return service.Spec.ClusterIP != kapi.ClusterIPNone && service.Spec.ClusterIP != ""
}
