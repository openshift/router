package templaterouter

import (
	"strings"
	"time"

	routev1 "github.com/openshift/api/route/v1"
)

// ServiceUnit represents a service and its endpoints.
type ServiceUnit struct {
	// Name corresponds to a service name & namespace.  Uniquely identifies the ServiceUnit
	Name string
	// Hostname is the name of this service.
	Hostname string
	// EndpointTable are endpoints that back the service, this translates into a final backend
	// implementation for routers.
	EndpointTable []Endpoint
	// ServiceAliasAssociations indicates what service aliases are
	// associated with this service unit.
	ServiceAliasAssociations map[ServiceAliasConfigKey]bool
}

type ServiceUnitKey string

// ServiceAliasConfig is a route for a service.  Uniquely identified by host + path.
type ServiceAliasConfig struct {
	// Name is the user-specified name of the route.
	Name string
	// Namespace is the namespace of the route.
	Namespace string
	// Host is a required host name ie. www.example.com
	Host string
	// Path is an optional path ie. www.example.com/myservice where "myservice" is the path
	Path string
	// TLSTermination is the termination policy for this backend and drives the mapping files and router configuration
	TLSTermination routev1.TLSTerminationType
	// Certificates used for securing this backend.  Keyed by the cert id
	Certificates map[string]Certificate
	// VerifyServiceHostname is true if the backend service(s) are expected to have serving certificates that sign for
	// the name "service.namespace.svc".
	VerifyServiceHostname bool
	// Indicates the status of configuration that needs to be persisted.  Right now this only
	// includes the certificates and is not an indicator of being written to the underlying
	// router implementation
	Status ServiceAliasConfigStatus
	// Indicates the port the user wishes to expose. If empty, a port will be selected for the service.
	PreferPort string
	// InsecureEdgeTerminationPolicy indicates desired behavior for
	// insecure connections to an edge-terminated route:
	//   none (or disable), allow or redirect
	InsecureEdgeTerminationPolicy routev1.InsecureEdgeTerminationPolicyType

	// Hash of the route name - used to obscure cookieId
	RoutingKeyName string

	// IsWildcard indicates this service unit needs wildcarding support.
	IsWildcard bool

	// Annotations attached to this route
	Annotations map[string]string

	// ServiceUnits is the weight for each service assigned to the route.
	// It is used in calculating the weight for the server that is found in ServiceUnitNames
	ServiceUnits map[ServiceUnitKey]int32

	// ServiceUnitNames is the weight to apply to each endpoint of each service supporting this route.
	// The value is the scaled portion of the service weight to assign
	// to each endpoint in the service.
	ServiceUnitNames map[ServiceUnitKey]int32

	// ActiveServiceUnits is a count of the service units with a non-zero weight
	ActiveServiceUnits int

	// ActiveEndpoints is a count of the route endpoints that are part of a service unit with a non-zero weight
	ActiveEndpoints int
}

type ServiceAliasConfigStatus string

const (
	// ServiceAliasConfigStatusSaved indicates that the necessary files for this config have
	// been persisted to disk.
	ServiceAliasConfigStatusSaved ServiceAliasConfigStatus = "saved"
)

type ServiceAliasConfigKey string

// Certificate represents a pub/private key pair.  It is identified by ID which will become the file name.
// A CA certificate will not have a PrivateKey set.
type Certificate struct {
	ID         string
	Contents   string
	PrivateKey string
}

// Endpoint is an internal representation of a k8s endpoint.
type Endpoint struct {
	ID            string
	IP            string
	Port          string
	TargetName    string
	PortName      string
	IdHash        string
	NoHealthCheck bool
}

// certificateManager provides the ability to write certificates for a ServiceAliasConfig
type certificateManager interface {
	// WriteCertificatesForConfig writes all certificates for all ServiceAliasConfigs in config
	WriteCertificatesForConfig(config *ServiceAliasConfig) error
	// DeleteCertificatesForConfig deletes all certificates for all ServiceAliasConfigs in config
	DeleteCertificatesForConfig(config *ServiceAliasConfig) error
	// Commit commits all the changes made to the certificateManager.
	Commit() error
	// CertificateWriter provides direct access to the underlying writer if required
	CertificateWriter() certificateWriter
}

// certManagerConfig provides the configuration necessary for certmanager to manipulate certificates.
type certificateManagerConfig struct {
	// certKeyFunc is used to find the edge certificate (which also has the key) from the cert map
	// of the ServiceAliasConfig
	certKeyFunc certificateKeyFunc
	// caCertKeyFunc is used to find the edge ca certificate from the cert map of the ServiceAliasConfig
	caCertKeyFunc certificateKeyFunc
	// destCertKeyFunc is used to find the ca certificate of a destination (pod) from the cert map
	// of the ServiceAliasConfig
	destCertKeyFunc certificateKeyFunc
	// certDir is where the edge certificates will be written.
	certDir string
	// caCertDir is where the edge certificates will be written.  It must be different than certDir
	caCertDir string
}

// certificateKeyFunc provides the certificateManager a way to create keys the same way the template
// router creates them so it can retrieve the certificates from a ServiceAliasConfig correctly
type certificateKeyFunc func(config *ServiceAliasConfig) string

// certificateWriter is used by a certificateManager to perform the actual writing.  It is abstracteed
// out in order to provide the ability to inject a test writer for unit testing
type certificateWriter interface {
	WriteCertificate(directory string, id string, cert []byte) error
	DeleteCertificate(directory, id string) error
}

// ConfigManagerOptions is the options passed to a template router's
// configuration manager.
type ConfigManagerOptions struct {
	// ConnectionInfo specifies how to connect to the underlying router.
	ConnectionInfo string

	// CommitInterval specifies how often to commit changes made to the
	// underlying router via the configuration manager.
	CommitInterval time.Duration

	// BlueprintRoutes are a list of routes blueprints pre-allocated by
	// the config manager to dynamically manage route additions.
	BlueprintRoutes []*routev1.Route

	// BlueprintRoutePoolSize is the size of the pre-allocated pool for
	// each route blueprint. This can be overriden on an individual
	// route basis with a route annotation:
	//    router.openshift.io/pool-size
	BlueprintRoutePoolSize int

	// MaxDynamicServers is the maximum number of dynamic servers we
	// will allocate on a per-route basis.
	MaxDynamicServers int

	// WildcardRoutesAllowed indicates if wildcard routes are allowed.
	WildcardRoutesAllowed bool

	// ExtendedValidation indicates if extended route validation is enabled.
	ExtendedValidation bool
}

// ConfigManager is used by the router to make configuration changes using
// the template router's dynamic configuration API (if any).
// Please note that the code calling the ConfigManager interface methods
// needs to ensure that a lock is acquired and released in order to
// guarantee Config Manager consistency.
// The haproxy specific implementation of the ConfigManager itself does
// guarantee consistency with internal locks but it is not a hard
// requirement for a ConfigManager "provider".
type ConfigManager interface {
	// Initialize initializes the config manager.
	Initialize(router RouterInterface, certPath string)

	// AddBlueprint adds a new (or replaces an existing) route blueprint.
	AddBlueprint(route *routev1.Route) error

	// RemoveBlueprint removes a route blueprint.
	RemoveBlueprint(route *routev1.Route)

	// Register registers an id to be associated with a route.
	Register(id ServiceAliasConfigKey, route *routev1.Route)

	// AddRoute adds a new route or updates an existing route.
	AddRoute(id ServiceAliasConfigKey, routingKey string, route *routev1.Route) error

	// RemoveRoute removes a route.
	RemoveRoute(id ServiceAliasConfigKey, route *routev1.Route) error

	// ReplaceRouteEndpoints replaces a subset (the ones associated with
	// a single service unit) of a route endpoints.
	ReplaceRouteEndpoints(id ServiceAliasConfigKey, oldEndpoints, newEndpoints []Endpoint, weight int32) error

	// RemoveRouteEndpoints removes a set of endpoints from a route.
	RemoveRouteEndpoints(id ServiceAliasConfigKey, endpoints []Endpoint) error

	// Notify notifies a configuration manager of a router event.
	// Currently the only ones that are received are on reload* events,
	// which indicates whether or not the configuration manager should
	// reset all the dynamically applied changes it is keeping track of.
	Notify(event RouterEventType)

	// ServerTemplateName returns the dynamic server template name.
	ServerTemplateName(id ServiceAliasConfigKey) string

	// ServerTemplateSize returns the dynamic server template size.
	ServerTemplateSize(id ServiceAliasConfigKey) string

	// GenerateDynamicServerNames generates the dynamic server names.
	GenerateDynamicServerNames(id ServiceAliasConfigKey) []string
}

// CaptureHTTPHeader specifies an HTTP header that should be captured for access
// logs.
type CaptureHTTPHeader struct {
	// Name specifies an HTTP header name.
	Name string

	// MaxLength specifies a maximum length for the header value.
	MaxLength int
}

// CaptureHTTPCookie specifies an HTTP cookie that should be captured
// for access logs.
type CaptureHTTPCookie struct {
	// Name specifies an HTTP cookie name.
	Name string

	// MaxLength specifies a maximum length for the cookie value.
	MaxLength int

	// MatchType specifies the type of match to be performed on the cookie
	// name.
	MatchType CookieMatchType
}

// CookieMatchType indicates the type of matching used against cookie names to
// select a cookie for capture.
type CookieMatchType string

const (
	// CookieMatchTypeExact indicates that an exact match should be performed.
	CookieMatchTypeExact CookieMatchType = "exact"

	// CookieMatchTypePrefix indicates that a prefix match should be performed.
	CookieMatchTypePrefix CookieMatchType = "prefix"
)

// HTTPHeaderNameCaseAdjustment specifies an HTTP header that should have its
// capitalization adjusted, and how the header should be adjusted.
type HTTPHeaderNameCaseAdjustment struct {
	// From specifies the original header name.  It must be a valid HTTP
	// header name in lower case.
	From string

	// To specifies the desired header name.  It should be the same as From
	// but with the desired capitalization.
	To string
}

// RouterEventType indicates the type of event fired by the router.
type RouterEventType string

const (
	// RouterEventReloadStart indicates start of a template router reload.
	RouterEventReloadStart = "reload-start"

	// RouterEventReloadEnd indicates end of a template router reload.
	RouterEventReloadEnd = "reload-end"

	// RouterEventReloadError indicates error on a template router reload.
	RouterEventReloadError = "reload-error"
)

//TemplateSafeName provides a name that can be used in the template that does not contain restricted
//characters like / which is used to concat namespace and name in the service unit key
func (s ServiceUnit) TemplateSafeName() string {
	return strings.Replace(s.Name, "/", "-", -1)
}
