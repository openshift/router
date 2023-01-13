package router

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/authenticatorfactory"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	"k8s.io/apiserver/pkg/server/healthz"
	authoptions "k8s.io/apiserver/pkg/server/options"
	authenticationclient "k8s.io/client-go/kubernetes/typed/authentication/v1"
	authorizationclient "k8s.io/client-go/kubernetes/typed/authorization/v1"

	routev1 "github.com/openshift/api/route/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned"
	routeclientset "github.com/openshift/client-go/route/clientset/versioned"
	routelisters "github.com/openshift/client-go/route/listers/route/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/proc"

	"github.com/openshift/router/pkg/router"
	"github.com/openshift/router/pkg/router/controller"
	"github.com/openshift/router/pkg/router/metrics"
	"github.com/openshift/router/pkg/router/metrics/haproxy"
	"github.com/openshift/router/pkg/router/shutdown"
	templateplugin "github.com/openshift/router/pkg/router/template"
	haproxyconfigmanager "github.com/openshift/router/pkg/router/template/configmanager/haproxy"
	"github.com/openshift/router/pkg/router/writerlease"
	"github.com/openshift/router/pkg/version"
)

// defaultReloadInterval is how often to do reloads in seconds.
const defaultReloadInterval = 5

// defaultCommitInterval is how often (in seconds) to commit the "in-memory"
// router changes made using the dynamic configuration manager.
const defaultCommitInterval = 60 * 60

var routerLong = heredoc.Doc(`
	Start a router

	This command launches a router connected to your cluster master. The router listens for routes and endpoints
	created by users and keeps a local router configuration up to date with those changes.

	You may customize the router by providing your own --template and --reload scripts.

	The router must have a default certificate in pem format. You may provide it via --default-cert otherwise
	one is automatically created.

	You may restrict the set of routes exposed to a single project (with --namespace), projects your client has
	access to with a set of labels (--project-labels), namespaces matching a label (--namespace-labels), or all
	namespaces (no argument). You can limit the routes to those matching a --labels or --fields selector. Note
	that you must have a cluster-wide administrative role to view all namespaces.

	For certain template routers, you can specify if a dynamic configuration
	manager should be used.  Certain template routers like haproxy and
	its associated haproxy config manager, allow route and endpoint changes
	to be propogated to the underlying router via a dynamic API.
	In the case of haproxy, the haproxy-manager uses this dynamic config
	API to modify the operational state of haproxy backends.
	Any endpoint changes (scaling, node evictions, etc) are handled by
	provisioning each backend with a pool of dynamic servers, which can
	then be used as needed. The max-dynamic-servers option (and/or
	ROUTER_MAX_DYNAMIC_SERVERS environment variable) controls the size
	of this pool.
	For new routes to be made available immediately, the haproxy-manager
	provisions a pre-allocated pool of routes called blueprints. A backend
	from this blueprint pool is used if the new route matches a specific blueprint.
	The default set of blueprints support for passthrough, insecure (or http)
	and edge secured routes using the default certificates.
	The blueprint-route-pool-size option (and/or the
	ROUTER_BLUEPRINT_ROUTE_POOL_SIZE environment variable) control the
	size of this pre-allocated pool.

	These blueprints can be extended or customized by using the blueprint route
	namespace and the blueprint label selector. Those options allow selected routes
	from a certain namespace (matching the label selection criteria) to
	serve as custom blueprints.`)

type TemplateRouterOptions struct {
	Config *Config

	TemplateRouter
	RouterStats
	RouterSelection
}

type TemplateRouter struct {
	WorkingDir                          string
	TemplateFile                        string
	ReloadScript                        string
	ReloadInterval                      time.Duration
	DefaultCertificate                  string
	DefaultCertificatePath              string
	DefaultCertificateDir               string
	DefaultDestinationCAPath            string
	BindPortsAfterSync                  bool
	MaxConnections                      string
	Ciphers                             string
	StrictSNI                           bool
	MetricsType                         string
	CaptureHTTPRequestHeadersString     string
	CaptureHTTPResponseHeadersString    string
	CaptureHTTPCookieString             string
	CaptureHTTPRequestHeaders           []templateplugin.CaptureHTTPHeader
	CaptureHTTPResponseHeaders          []templateplugin.CaptureHTTPHeader
	CaptureHTTPCookie                   *templateplugin.CaptureHTTPCookie
	HTTPHeaderNameCaseAdjustmentsString string
	HTTPHeaderNameCaseAdjustments       []templateplugin.HTTPHeaderNameCaseAdjustment
	HTTPResponseHeadersString           string
	HTTPResponseHeaders                 []templateplugin.HTTPHeader
	HTTPRequestHeadersString            string
	HTTPRequestHeaders                  []templateplugin.HTTPHeader

	TemplateRouterConfigManager
}

type TemplateRouterConfigManager struct {
	UseHAProxyConfigManager     bool
	CommitInterval              time.Duration
	BlueprintRouteNamespace     string
	BlueprintRouteLabelSelector string
	BlueprintRoutePoolSize      int
	MaxDynamicServers           int
}

// isTrue here has the same logic as the function within package pkg/router/template
func isTrue(s string) bool {
	v, _ := strconv.ParseBool(s)
	return v
}

// getIntervalFromEnv returns a interval value based on an environment
// variable or the default.
func getIntervalFromEnv(name string, defaultValSecs int) time.Duration {
	interval := env(name, fmt.Sprintf("%vs", defaultValSecs))

	value, err := time.ParseDuration(interval)
	if err != nil {
		log.V(0).Info("invalid interval, using default", "name", name, "interval", interval, "default", defaultValSecs)
		value = time.Duration(time.Duration(defaultValSecs) * time.Second)
	}
	return value
}

func (o *TemplateRouter) Bind(flag *pflag.FlagSet) {
	flag.StringVar(&o.WorkingDir, "working-dir", "/var/lib/haproxy", "The working directory for the router plugin")
	flag.StringVar(&o.DefaultCertificate, "default-certificate", env("DEFAULT_CERTIFICATE", ""), "The contents of a default certificate to use for routes that don't expose a TLS server cert; in PEM format")
	flag.StringVar(&o.DefaultCertificatePath, "default-certificate-path", env("DEFAULT_CERTIFICATE_PATH", ""), "A path to default certificate to use for routes that don't expose a TLS server cert; in PEM format")
	flag.StringVar(&o.DefaultCertificateDir, "default-certificate-dir", env("DEFAULT_CERTIFICATE_DIR", ""), "A path to a directory that contains a file named tls.crt. If tls.crt is not a PEM file which also contains a private key, it is first combined with a file named tls.key in the same directory. The PEM-format contents are then used as the default certificate. Only used if default-certificate and default-certificate-path are not specified.")
	flag.StringVar(&o.DefaultDestinationCAPath, "default-destination-ca-path", env("DEFAULT_DESTINATION_CA_PATH", ""), "A path to a PEM file containing the default CA bundle to use with re-encrypt routes. This CA should sign for certificates in the Kubernetes DNS space (service.namespace.svc).")
	flag.StringVar(&o.TemplateFile, "template", env("TEMPLATE_FILE", ""), "The path to the template file to use")
	flag.StringVar(&o.ReloadScript, "reload", env("RELOAD_SCRIPT", ""), "The path to the reload script to use")
	flag.DurationVar(&o.ReloadInterval, "interval", getIntervalFromEnv("RELOAD_INTERVAL", defaultReloadInterval), "Controls how often router reloads are invoked. Mutiple router reload requests are coalesced for the duration of this interval since the last reload time.")
	flag.BoolVar(&o.BindPortsAfterSync, "bind-ports-after-sync", env("ROUTER_BIND_PORTS_AFTER_SYNC", "") == "true", "Bind ports only after route state has been synchronized")
	flag.StringVar(&o.MaxConnections, "max-connections", env("ROUTER_MAX_CONNECTIONS", ""), "Specifies the maximum number of concurrent connections.")
	flag.StringVar(&o.Ciphers, "ciphers", env("ROUTER_CIPHERS", ""), "Specifies the cipher suites to use. You can choose a predefined cipher set ('modern', 'intermediate', or 'old') or specify exact cipher suites by passing a : separated list.")
	flag.BoolVar(&o.StrictSNI, "strict-sni", isTrue(env("ROUTER_STRICT_SNI", "")), "Use strict-sni bind processing (do not use default cert).")
	flag.StringVar(&o.MetricsType, "metrics-type", env("ROUTER_METRICS_TYPE", ""), "Specifies the type of metrics to gather. Supports 'haproxy'.")
	flag.BoolVar(&o.UseHAProxyConfigManager, "haproxy-config-manager", isTrue(env("ROUTER_HAPROXY_CONFIG_MANAGER", "")), "Use the the haproxy config manager (and dynamic configuration API) to configure route and endpoint changes. Reduces the number of haproxy reloads needed on configuration changes.")
	flag.DurationVar(&o.CommitInterval, "commit-interval", getIntervalFromEnv("COMMIT_INTERVAL", defaultCommitInterval), "Controls how often to commit (to the actual config) all the changes made using the router specific dynamic configuration manager.")
	flag.StringVar(&o.BlueprintRouteNamespace, "blueprint-route-namespace", env("ROUTER_BLUEPRINT_ROUTE_NAMESPACE", ""), "Specifies the namespace which contains the routes that serve as blueprints for the dynamic configuration manager.")
	flag.StringVar(&o.BlueprintRouteLabelSelector, "blueprint-route-labels", env("ROUTER_BLUEPRINT_ROUTE_LABELS", ""), "A label selector to apply to the routes in the blueprint route namespace. These selected routes will serve as blueprints for the dynamic dynamic configuration manager.")
	flag.IntVar(&o.BlueprintRoutePoolSize, "blueprint-route-pool-size", int(envInt("ROUTER_BLUEPRINT_ROUTE_POOL_SIZE", 10, 1)), "Specifies the size of the pre-allocated pool for each route blueprint managed by the router specific dynamic configuration manager. This can be overriden by an annotation router.openshift.io/pool-size on an individual route.")
	flag.IntVar(&o.MaxDynamicServers, "max-dynamic-servers", int(envInt("ROUTER_MAX_DYNAMIC_SERVERS", 5, 1)), "Specifies the maximum number of dynamic servers added to a route for use by the router specific dynamic configuration manager.")
	flag.StringVar(&o.CaptureHTTPRequestHeadersString, "capture-http-request-headers", env("ROUTER_CAPTURE_HTTP_REQUEST_HEADERS", ""), "A comma-delimited list of HTTP request header names and maximum header value lengths that should be captured for logging. Each item must have the following form: name:maxLength")
	flag.StringVar(&o.CaptureHTTPResponseHeadersString, "capture-http-response-headers", env("ROUTER_CAPTURE_HTTP_RESPONSE_HEADERS", ""), "A comma-delimited list of HTTP response header names and maximum header value lengths that should be captured for logging. Each item must have the following form: name:maxLength")
	flag.StringVar(&o.CaptureHTTPCookieString, "capture-http-cookie", env("ROUTER_CAPTURE_HTTP_COOKIE", ""), "Name and maximum length of HTTP cookie that should be captured for logging.  The argument must have the following form: name:maxLength. Append '=' to the name to indicate that an exact match should be performed; otherwise a prefix match will be performed.  The value of first cookie that matches the name is captured.")
	flag.StringVar(&o.HTTPHeaderNameCaseAdjustmentsString, "http-header-name-case-adjustments", env("ROUTER_H1_CASE_ADJUST", ""), "A comma-delimited list of HTTP header names that should have their case adjusted. Each item must be a valid HTTP header name and should have the desired capitalization.")
	flag.StringVar(&o.HTTPResponseHeadersString, "set-delete-http-response-header", env("ROUTER_HTTP_RESPONSE_HEADERS", ""), "A comma-delimited list of HTTP response header names and values that should be set/deleted.")
	flag.StringVar(&o.HTTPRequestHeadersString, "set-delete-http-request-header", env("ROUTER_HTTP_REQUEST_HEADERS", ""), "A comma-delimited list of HTTP request header names and values that should be set/deleted.")
}

type RouterStats struct {
	StatsPortString   string
	StatsPasswordFile string
	StatsUsernameFile string
	StatsPassword     string
	StatsUsername     string

	StatsPort int
}

func (o *RouterStats) Bind(flag *pflag.FlagSet) {
	flag.StringVar(&o.StatsPortString, "stats-port", env("STATS_PORT", ""), "If the underlying router implementation can provide statistics this is a hint to expose it on this port. Ignored if listen-addr is specified.")
	flag.StringVar(&o.StatsPasswordFile, "stats-password-file", env("STATS_PASSWORD_FILE", ""), "If the underlying router implementation can provide statistics this is the requested password file for auth.")
	flag.StringVar(&o.StatsUsernameFile, "stats-user-file", env("STATS_USERNAME_FILE", ""), "If the underlying router implementation can provide statistics this is the requested username file for auth.")
	flag.StringVar(&o.StatsPassword, "stats-password", env("STATS_PASSWORD", ""), "If the underlying router implementation can provide statistics this is the requested password for auth.")
	flag.StringVar(&o.StatsUsername, "stats-user", env("STATS_USERNAME", ""), "If the underlying router implementation can provide statistics this is the requested username for auth.")
}

// NewCommndTemplateRouter provides CLI handler for the template router backend
func NewCommandTemplateRouter(name string) *cobra.Command {
	options := &TemplateRouterOptions{
		Config: NewConfig(),
	}

	cmd := &cobra.Command{
		Use:   name,
		Short: "Start a router",
		Long:  routerLong,
		RunE: func(c *cobra.Command, args []string) error {
			options.RouterSelection.Namespace = c.Flags().Lookup("namespace").Value.String()
			// if the user did not specify a destination ca path, and the file does not exist, disable the default in order
			// to preserve backwards compatibility with older clusters
			if !c.Flags().Lookup("default-destination-ca-path").Changed && env("DEFAULT_DESTINATION_CA_PATH", "") == "" {
				if _, err := os.Stat(options.TemplateRouter.DefaultDestinationCAPath); err != nil {
					options.TemplateRouter.DefaultDestinationCAPath = ""
				}
			}
			if err := options.Complete(); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			return options.Run(shutdown.SetupSignalHandler())
		},
	}

	cmd.AddCommand(newCmdVersion(name, version.String(), os.Stdout))

	flag := cmd.Flags()
	options.Config.Bind(flag)
	options.TemplateRouter.Bind(flag)
	options.RouterStats.Bind(flag)
	options.RouterSelection.Bind(flag)

	return cmd
}

// validTokenRE matches valid tokens as defined in section 2.2 of RFC 2616.
// A token comprises 1 or more non-control and non-separator characters:
//
//	token          = 1*<any CHAR except CTLs or separators>
//	CHAR           = <any US-ASCII character (octets 0 - 127)>
//	CTL            = <any US-ASCII control character
//	                 (octets 0 - 31) and DEL (127)>
//	separators     = "(" | ")" | "<" | ">" | "@"
//	               | "," | ";" | ":" | "\" | <">
//	               | "/" | "[" | "]" | "?" | "="
//	               | "{" | "}" | SP | HT
//	SP             = <US-ASCII SP, space (32)>
//	HT             = <US-ASCII HT, horizontal-tab (9)>
var validTokenRE *regexp.Regexp = regexp.MustCompile(`^[\x21\x23-\x27\x2a\x2b\x2d\x2e\x30-\x39\x41-\x5a\x5e-\x7a\x7c\x7e]+$`)

func parseCaptureHeaders(in string) ([]templateplugin.CaptureHTTPHeader, error) {
	var captureHeaders []templateplugin.CaptureHTTPHeader

	if len(in) > 0 {
		for _, header := range strings.Split(in, ",") {
			parts := strings.Split(header, ":")
			if len(parts) != 2 {
				return captureHeaders, fmt.Errorf("invalid HTTP header capture specification: %v", header)
			}
			headerName := parts[0]
			// RFC 2616, section 4.2, states that the header name
			// must be a valid token.
			if !validTokenRE.MatchString(headerName) {
				return captureHeaders, fmt.Errorf("invalid HTTP header name: %v", headerName)
			}
			maxLength, err := strconv.Atoi(parts[1])
			if err != nil {
				return captureHeaders, err
			}
			capture := templateplugin.CaptureHTTPHeader{
				Name:      headerName,
				MaxLength: maxLength,
			}
			captureHeaders = append(captureHeaders, capture)
		}
	}

	return captureHeaders, nil
}

func parseHeadersToBeSetOrDeleted(in string) ([]templateplugin.HTTPHeader, error) {
	var captureHeaders []templateplugin.HTTPHeader
	var capture templateplugin.HTTPHeader
	var err error
	if len(in) == 0 {
		return captureHeaders, fmt.Errorf("encoded header string not present.")
	}
	if len(in) > 0 {
		for _, header := range strings.Split(in, ",") {
			parts := strings.Split(header, ":")
			num := len(parts)
			switch num {
			default:
				return captureHeaders, fmt.Errorf("invalid HTTP header input specification: %v", header)
			case 3:
				{
					headerName, err := url.QueryUnescape(parts[0])
					if err != nil {
						return captureHeaders, fmt.Errorf("failed to decode percent encoding: %v", parts[0])
					}
					err = checkValidHeaderName(headerName)
					if err != nil {
						return captureHeaders, err
					}
					headerValue, err := url.QueryUnescape(parts[1])
					if err != nil {
						return captureHeaders, fmt.Errorf("failed to decode percent encoding: %v", parts[1])
					}
					sanitizedHeaderValue := templateplugin.SanitizeHeaderValue(headerValue)
					action, err := url.QueryUnescape(parts[2])
					if err != nil {
						return captureHeaders, fmt.Errorf("failed to decode percent encoding: %v", parts[2])
					}
					err = checkValidAction(action)
					if err != nil {
						return captureHeaders, err
					}
					capture = templateplugin.HTTPHeader{
						Name:   headerName,
						Value:  sanitizedHeaderValue,
						Action: routev1.RouteHTTPHeaderActionType(action),
					}
					captureHeaders = append(captureHeaders, capture)
				}
			case 2:
				{
					headerName, err := url.QueryUnescape(parts[0])
					if err != nil {
						return captureHeaders, fmt.Errorf("failed to decode percent encoding: %v", parts[0])
					}
					err = checkValidHeaderName(headerName)
					if err != nil {
						return captureHeaders, err
					}
					action, err := url.QueryUnescape(parts[1])
					if err != nil {
						return captureHeaders, fmt.Errorf("failed to decode percent encoding: %v", parts[1])
					}
					err = checkValidAction(action)
					if err != nil {
						return captureHeaders, err
					}
					capture = templateplugin.HTTPHeader{
						Name:   headerName,
						Action: routev1.RouteHTTPHeaderActionType(action),
					}
					captureHeaders = append(captureHeaders, capture)
				}
			}
		}
	}

	return captureHeaders, err
}

func checkValidAction(action string) error {
	if action != string(routev1.Set) && action != string(routev1.Delete) {
		return fmt.Errorf("invalid action %s", action)
	} else {
		return nil
	}
}

func checkValidHeaderName(headerName string) error {
	// RFC 2616, section 4.2, states that the header name
	// must be a valid token.
	if !validTokenRE.MatchString(headerName) {
		return fmt.Errorf("invalid HTTP header name: %s", headerName)
	} else {
		return nil
	}
}

func parseCaptureCookie(in string) (*templateplugin.CaptureHTTPCookie, error) {
	if len(in) == 0 {
		return nil, nil
	}

	parts := strings.Split(in, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid HTTP cookie capture specification: %v", in)
	}
	cookieName := parts[0]
	matchType := templateplugin.CookieMatchTypePrefix
	if strings.HasSuffix(cookieName, "=") {
		cookieName = cookieName[:len(cookieName)-1]
		matchType = templateplugin.CookieMatchTypeExact
	}
	// RFC 6265 section 4.1 states that the cookie name must be a
	// valid token.
	if !validTokenRE.MatchString(cookieName) {
		return nil, fmt.Errorf("invalid HTTP cookie name: %v", cookieName)
	}
	maxLength, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, err
	}

	return &templateplugin.CaptureHTTPCookie{
		Name:      cookieName,
		MaxLength: maxLength,
		MatchType: matchType,
	}, nil
}

func parseHTTPHeaderNameCaseAdjustments(in string) ([]templateplugin.HTTPHeaderNameCaseAdjustment, error) {
	var adjustments []templateplugin.HTTPHeaderNameCaseAdjustment

	if len(in) > 0 {
		for _, headerName := range strings.Split(in, ",") {
			// RFC 2616, section 4.2, states that the header name
			// must be a valid token.
			if !validTokenRE.MatchString(headerName) {
				return adjustments, fmt.Errorf("invalid HTTP header name: %v", headerName)
			}
			adjustment := templateplugin.HTTPHeaderNameCaseAdjustment{
				From: strings.ToLower(headerName),
				To:   headerName,
			}
			adjustments = append(adjustments, adjustment)
		}
	}

	return adjustments, nil
}

func (o *TemplateRouterOptions) Complete() error {
	routerSvcName := env("ROUTER_SERVICE_NAME", "")
	routerSvcNamespace := env("ROUTER_SERVICE_NAMESPACE", "")
	if len(routerSvcName) > 0 {
		if len(routerSvcNamespace) == 0 {
			return fmt.Errorf("ROUTER_SERVICE_NAMESPACE is required when ROUTER_SERVICE_NAME is specified")
		}
	}

	if len(o.StatsPortString) > 0 {
		statsPort, err := strconv.Atoi(o.StatsPortString)
		if err != nil {
			return fmt.Errorf("stat port is not valid: %v", err)
		}
		o.StatsPort = statsPort
	}
	if len(o.ListenAddr) > 0 {
		_, port, err := net.SplitHostPort(o.ListenAddr)
		if err != nil {
			return fmt.Errorf("listen-addr is not valid: %v", err)
		}
		// stats port on listen-addr overrides stats port argument
		statsPort, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("listen-addr port is not valid: %v", err)
		}
		o.StatsPort = statsPort
	} else {
		if o.StatsPort != 0 {
			o.ListenAddr = fmt.Sprintf("0.0.0.0:%d", o.StatsPort)
		}
	}

	if nsecs := int(o.ReloadInterval.Seconds()); nsecs < 1 {
		return fmt.Errorf("invalid reload interval: %v - must be a positive duration", nsecs)
	}

	if nsecs := int(o.CommitInterval.Seconds()); nsecs < 1 {
		return fmt.Errorf("invalid dynamic configuration manager commit interval: %v - must be a positive duration", nsecs)
	}

	captureHTTPRequestHeaders, err := parseCaptureHeaders(o.CaptureHTTPRequestHeadersString)
	if err != nil {
		return err
	}
	o.CaptureHTTPRequestHeaders = captureHTTPRequestHeaders

	captureHTTPResponseHeaders, err := parseCaptureHeaders(o.CaptureHTTPResponseHeadersString)
	if err != nil {
		return err
	}
	o.CaptureHTTPResponseHeaders = captureHTTPResponseHeaders

	if len(o.HTTPResponseHeadersString) != 0 {
		httpResponseHeaders, err := parseHeadersToBeSetOrDeleted(o.HTTPResponseHeadersString)
		if err != nil {
			return err
		}
		o.HTTPResponseHeaders = httpResponseHeaders
	}

	if len(o.HTTPRequestHeadersString) != 0 {
		httpRequestHeaders, err := parseHeadersToBeSetOrDeleted(o.HTTPRequestHeadersString)
		if err != nil {
			return err
		}
		o.HTTPRequestHeaders = httpRequestHeaders
	}

	captureHTTPCookie, err := parseCaptureCookie(o.CaptureHTTPCookieString)
	if err != nil {
		return err
	}
	o.CaptureHTTPCookie = captureHTTPCookie

	httpHeaderNameCaseAdjustments, err := parseHTTPHeaderNameCaseAdjustments(o.HTTPHeaderNameCaseAdjustmentsString)
	if err != nil {
		return err
	}
	o.HTTPHeaderNameCaseAdjustments = httpHeaderNameCaseAdjustments

	return o.RouterSelection.Complete()
}

// supportedMetricsTypes is the set of supported metrics arguments
var supportedMetricsTypes = sets.NewString("haproxy")

func (o *TemplateRouterOptions) Validate() error {
	if len(o.MetricsType) > 0 && !supportedMetricsTypes.Has(o.MetricsType) {
		return fmt.Errorf("supported metrics types are: %s", strings.Join(supportedMetricsTypes.List(), ", "))
	}
	if len(o.RouterName) == 0 && o.UpdateStatus {
		return errors.New("router must have a name to identify itself in route status")
	}
	if len(o.TemplateFile) == 0 {
		return errors.New("template file must be specified")
	}
	if len(o.TemplateRouter.DefaultDestinationCAPath) != 0 {
		if _, err := os.Stat(o.TemplateRouter.DefaultDestinationCAPath); err != nil {
			return fmt.Errorf("unable to load default destination CA certificate: %v", err)
		}
	}
	if len(o.ReloadScript) == 0 {
		return errors.New("reload script must be specified")
	}
	return nil
}

// Run launches a template router using the provided options. It never exits.
func (o *TemplateRouterOptions) Run(stopCh <-chan struct{}) error {
	log.V(0).Info("starting router", "version", version.String())
	var ptrTemplatePlugin *templateplugin.TemplatePlugin

	var reloadCallbacks []func()

	statsPort := o.StatsPort
	switch {
	case o.MetricsType == "haproxy" && statsPort != 0:
		// Exposed to allow tuning in production if this becomes an issue
		var timeout time.Duration
		if t := env("ROUTER_METRICS_HAPROXY_TIMEOUT", ""); len(t) > 0 {
			d, err := time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("ROUTER_METRICS_HAPROXY_TIMEOUT is not a valid duration: %v", err)
			}
			timeout = d
		}
		// Exposed to allow tuning in production if this becomes an issue
		var baseScrapeInterval time.Duration
		if t := env("ROUTER_METRICS_HAPROXY_BASE_SCRAPE_INTERVAL", ""); len(t) > 0 {
			d, err := time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("ROUTER_METRICS_HAPROXY_BASE_SCRAPE_INTERVAL is not a valid duration: %v", err)
			}
			baseScrapeInterval = d
		}
		// Exposed to allow tuning in production if this becomes an issue
		var serverThreshold int
		if t := env("ROUTER_METRICS_HAPROXY_SERVER_THRESHOLD", ""); len(t) > 0 {
			i, err := strconv.Atoi(t)
			if err != nil {
				return fmt.Errorf("ROUTER_METRICS_HAPROXY_SERVER_THRESHOLD is not a valid integer: %v", err)
			}
			serverThreshold = i
		}
		// Exposed to allow tuning in production if this becomes an issue
		var exported []int
		if t := env("ROUTER_METRICS_HAPROXY_EXPORTED", ""); len(t) > 0 {
			for _, s := range strings.Split(t, ",") {
				i, err := strconv.Atoi(s)
				if err != nil {
					return errors.New("ROUTER_METRICS_HAPROXY_EXPORTED must be a comma delimited list of column numbers to extract from the HAProxy configuration")
				}
				exported = append(exported, i)
			}
		}

		collector, err := haproxy.NewPrometheusCollector(haproxy.PrometheusOptions{
			// Only template router customizers who alter the image should need this
			ScrapeURI: env("ROUTER_METRICS_HAPROXY_SCRAPE_URI", ""),
			// Only template router customizers who alter the image should need this
			PidFile:            env("ROUTER_METRICS_HAPROXY_PID_FILE", ""),
			Timeout:            timeout,
			ServerThreshold:    serverThreshold,
			BaseScrapeInterval: baseScrapeInterval,
			ExportedMetrics:    exported,
		})
		if err != nil {
			return err
		}

		// Metrics will handle healthz on the stats port, and instruct the template router to disable stats completely.
		// The underlying router must provide a custom health check if customized which will be called into.
		statsPort = -1
		httpURL := env("ROUTER_METRICS_READY_HTTP_URL", fmt.Sprintf("http://%s:%s/_______internal_router_healthz", "localhost", env("ROUTER_SERVICE_HTTP_PORT", "80")))
		u, err := url.Parse(httpURL)
		if err != nil {
			return fmt.Errorf("ROUTER_METRICS_READY_HTTP_URL must be a valid URL or empty: %v", err)
		}
		checkBackend := metrics.HTTPBackendAvailable(u)
		if isTrue(env("ROUTER_USE_PROXY_PROTOCOL", "")) {
			checkBackend = metrics.ProxyProtocolHTTPBackendAvailable(u)
		}
		checkSync, err := metrics.HasSynced(&ptrTemplatePlugin)
		if err != nil {
			return err
		}
		checkController := metrics.ControllerLive()
		liveChecks := []healthz.HealthChecker{checkController}
		if !(isTrue(env("ROUTER_BIND_PORTS_BEFORE_SYNC", ""))) {
			liveChecks = append(liveChecks, checkBackend)
		}

		kubeconfig, _, err := o.Config.KubeConfig()
		if err != nil {
			return err
		}
		client, err := authorizationclient.NewForConfig(kubeconfig)
		if err != nil {
			return err
		}
		authz, err := authorizerfactory.DelegatingAuthorizerConfig{
			SubjectAccessReviewClient: client,
			AllowCacheTTL:             2 * time.Minute,
			DenyCacheTTL:              5 * time.Second,
			WebhookRetryBackoff:       authoptions.DefaultAuthWebhookRetryBackoff(),
		}.New()
		if err != nil {
			return err
		}
		tokenClient, err := authenticationclient.NewForConfig(kubeconfig)
		if err != nil {
			return err
		}
		authn, _, err := authenticatorfactory.DelegatingAuthenticatorConfig{
			Anonymous:               true,
			TokenAccessReviewClient: tokenClient,
			CacheTTL:                10 * time.Second,
			WebhookRetryBackoff:     authoptions.DefaultAuthWebhookRetryBackoff(),
		}.New()
		if err != nil {
			return err
		}

		statsUsername, statsPassword, err := getStatsAuth(o.StatsUsernameFile, o.StatsPasswordFile, o.StatsUsername, o.StatsPassword)
		if err != nil {
			return err
		}
		l := metrics.Listener{
			Addr:          o.ListenAddr,
			Username:      statsUsername,
			Password:      statsPassword,
			Authenticator: authn,
			Authorizer:    authz,
			Record: authorizer.AttributesRecord{
				ResourceRequest: true,
				APIGroup:        "route.openshift.io",
				Resource:        "routers",
				Name:            o.RouterName,
			},
			LiveChecks:  liveChecks,
			ReadyChecks: []healthz.HealthChecker{checkBackend, checkSync, metrics.ProcessRunning(stopCh)},
		}

		if tlsConfig, err := makeTLSConfig(30 * time.Second); err != nil {
			return err
		} else {
			l.TLSConfig = tlsConfig
		}

		l.Listen()

		// on reload, invoke the collector to preserve whatever metrics we can
		reloadCallbacks = append(reloadCallbacks, collector.CollectNow)
	}

	kc, err := o.Config.Clients()
	if err != nil {
		return err
	}
	config, _, err := o.Config.KubeConfig()
	if err != nil {
		return err
	}
	routeclient, err := routeclientset.NewForConfig(config)
	if err != nil {
		return err
	}
	projectclient, err := projectclient.NewForConfig(config)
	if err != nil {
		return err
	}

	var cfgManager templateplugin.ConfigManager
	var blueprintPlugin router.Plugin
	if o.UseHAProxyConfigManager {
		blueprintRoutes, err := o.blueprintRoutes(routeclient)
		if err != nil {
			return err
		}
		cmopts := templateplugin.ConfigManagerOptions{
			ConnectionInfo:         "unix:///var/lib/haproxy/run/haproxy.sock",
			CommitInterval:         o.CommitInterval,
			BlueprintRoutes:        blueprintRoutes,
			BlueprintRoutePoolSize: o.BlueprintRoutePoolSize,
			MaxDynamicServers:      o.MaxDynamicServers,
			WildcardRoutesAllowed:  o.AllowWildcardRoutes,
			ExtendedValidation:     o.ExtendedValidation,
		}
		cfgManager = haproxyconfigmanager.NewHAProxyConfigManager(cmopts)
		if len(o.BlueprintRouteNamespace) > 0 {
			blueprintPlugin = haproxyconfigmanager.NewBlueprintPlugin(cfgManager)
		}
	}

	statsUsername, statsPassword, err := getStatsAuth(o.StatsUsernameFile, o.StatsPasswordFile, o.StatsUsername, o.StatsPassword)
	if err != nil {
		return err
	}

	pluginCfg := templateplugin.TemplatePluginConfig{
		WorkingDir:                    o.WorkingDir,
		TemplatePath:                  o.TemplateFile,
		ReloadScriptPath:              o.ReloadScript,
		ReloadInterval:                o.ReloadInterval,
		ReloadCallbacks:               reloadCallbacks,
		DefaultCertificate:            o.DefaultCertificate,
		DefaultCertificatePath:        o.DefaultCertificatePath,
		DefaultCertificateDir:         o.DefaultCertificateDir,
		DefaultDestinationCAPath:      o.DefaultDestinationCAPath,
		StatsPort:                     statsPort,
		StatsUsername:                 statsUsername,
		StatsPassword:                 statsPassword,
		BindPortsAfterSync:            o.BindPortsAfterSync,
		IncludeUDP:                    o.RouterSelection.IncludeUDP,
		AllowWildcardRoutes:           o.RouterSelection.AllowWildcardRoutes,
		MaxConnections:                o.MaxConnections,
		Ciphers:                       o.Ciphers,
		StrictSNI:                     o.StrictSNI,
		DynamicConfigManager:          cfgManager,
		CaptureHTTPRequestHeaders:     o.CaptureHTTPRequestHeaders,
		CaptureHTTPResponseHeaders:    o.CaptureHTTPResponseHeaders,
		CaptureHTTPCookie:             o.CaptureHTTPCookie,
		HTTPHeaderNameCaseAdjustments: o.HTTPHeaderNameCaseAdjustments,
		HTTPResponseHeaders:           o.HTTPResponseHeaders,
		HTTPRequestHeaders:            o.HTTPRequestHeaders,
	}

	svcFetcher := templateplugin.NewListWatchServiceLookup(kc.CoreV1(), o.ResyncInterval, o.Namespace)
	templatePlugin, err := templateplugin.NewTemplatePlugin(pluginCfg, svcFetcher)
	if err != nil {
		return err
	}
	ptrTemplatePlugin = templatePlugin

	factory := o.RouterSelection.NewFactory(routeclient, projectclient.ProjectV1().Projects(), kc)
	factory.RouteModifierFn = o.RouteUpdate

	var plugin router.Plugin = templatePlugin
	var recorder controller.RejectionRecorder = controller.LogRejections
	if o.UpdateStatus {
		lease := writerlease.New(time.Minute, 3*time.Second)
		go lease.Run(stopCh)
		informer := factory.CreateRoutesSharedInformer()
		tracker := controller.NewSimpleContentionTracker(informer, o.RouterName, o.ResyncInterval/10)
		tracker.SetConflictMessage(fmt.Sprintf("The router detected another process is writing conflicting updates to route status with name %q. Please ensure that the configuration of all routers is consistent. Route status will not be updated as long as conflicts are detected.", o.RouterName))
		go tracker.Run(stopCh)
		routeLister := routelisters.NewRouteLister(informer.GetIndexer())
		status := controller.NewStatusAdmitter(plugin, routeclient.RouteV1(), routeLister, o.RouterName, o.RouterCanonicalHostname, lease, tracker)
		recorder = status
		plugin = status
	}
	if o.ExtendedValidation {
		plugin = controller.NewExtendedValidator(plugin, recorder)
	}
	plugin = controller.NewUniqueHost(plugin, o.RouterSelection.DisableNamespaceOwnershipCheck, recorder)
	plugin = controller.NewHostAdmitter(plugin, o.RouteAdmissionFunc(), o.AllowWildcardRoutes, o.RouterSelection.DisableNamespaceOwnershipCheck, recorder)

	controller := factory.Create(plugin, false, stopCh)
	controller.Run()

	if blueprintPlugin != nil {
		// f is like factory but filters the routes based on the
		// blueprint route namespace and label selector (if any).
		f := o.RouterSelection.NewFactory(routeclient, projectclient.ProjectV1().Projects(), kc)
		f.LabelSelector = o.BlueprintRouteLabelSelector
		f.Namespace = o.BlueprintRouteNamespace
		f.ResyncInterval = o.ResyncInterval
		c := f.Create(blueprintPlugin, false, stopCh)
		c.Run()
	}

	proc.StartReaper(6 * time.Second)

	select {
	case <-stopCh:
		// 45s is the default interval that almost all cloud load balancers require to take an unhealthy
		// endpoint out of rotation.
		delay := getIntervalFromEnv("ROUTER_GRACEFUL_SHUTDOWN_DELAY", 45)
		log.Info(fmt.Sprintf("Shutdown requested, waiting %s for new connections to cease", delay))
		time.Sleep(delay)
		log.Info("Instructing the template router to terminate")
		if err := templatePlugin.Stop(); err != nil {
			log.Error(err, "Router did not shut down cleanly")
		} else {
			log.Info("Shutdown complete, exiting")
		}
		// wait one second to let any remaining actions settle
		time.Sleep(time.Second)
	}
	return nil
}

// blueprintRoutes returns all the routes in the blueprint namespace.
func (o *TemplateRouterOptions) blueprintRoutes(routeclient *routeclientset.Clientset) ([]*routev1.Route, error) {
	blueprints := make([]*routev1.Route, 0)
	if len(o.BlueprintRouteNamespace) == 0 {
		return blueprints, nil
	}

	options := metav1.ListOptions{}
	if len(o.BlueprintRouteLabelSelector) > 0 {
		options.LabelSelector = o.BlueprintRouteLabelSelector
	}

	routeList, err := routeclient.RouteV1().Routes(o.BlueprintRouteNamespace).List(context.TODO(), options)
	if err != nil {
		return blueprints, err
	}
	for _, r := range routeList.Items {
		blueprints = append(blueprints, r.DeepCopy())
	}

	return blueprints, nil
}

// makeTLSConfig checks whether metrics TLS is configured and
// if so returns a tls.Config whose certificate is automatically
// reloaded on the given period.
func makeTLSConfig(reloadPeriod time.Duration) (*tls.Config, error) {
	certFile := env("ROUTER_METRICS_TLS_CERT_FILE", "")
	if len(certFile) == 0 {
		return nil, nil
	}
	keyFile := env("ROUTER_METRICS_TLS_KEY_FILE", "")

	// Load the initial certificate contents.
	certBytes, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	keyBytes, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	certificate, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, err
	}

	// Safely reload the certificate on a fixed period for simplicity.
	ticker := time.NewTicker(reloadPeriod)
	var lock sync.Mutex
	go func() {
		for {
			select {
			case <-ticker.C:
				latestCertBytes, err := ioutil.ReadFile(certFile)
				if err != nil {
					log.Error(err, "failed to read certificate file", "cert", certFile, "key", keyFile)
					break
				}
				latestKeyBytes, err := ioutil.ReadFile(keyFile)
				if err != nil {
					log.Error(err, "failed to read key file", "cert", certFile, "key", keyFile)
					break
				}

				if bytes.Equal(latestCertBytes, certBytes) && bytes.Equal(latestKeyBytes, keyBytes) {
					// Nothing changed, try later.
					break
				}

				// Something changed, reload the certificate.
				latest, err := tls.X509KeyPair(latestCertBytes, latestKeyBytes)
				if err != nil {
					log.Error(err, "failed to reload certificate", "cert", certFile, "key", keyFile)
					break
				}
				certBytes = latestCertBytes
				keyBytes = latestKeyBytes

				lock.Lock()
				certificate = latest
				lock.Unlock()

				log.V(0).Info("reloaded metrics certificate", "cert", certFile, "key", keyFile)
			}
		}
	}()

	return crypto.SecureTLSConfig(&tls.Config{
		GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
			lock.Lock()
			defer lock.Unlock()
			return &certificate, nil
		},
		ClientAuth: tls.RequestClientCert,
	}), nil
}

// getStatsAuth returns the available stats username and password.
// If both statsUsernameFile and statsPasswordFile are non-empty, statsUsername
// and statsPassword are ignored.
// Returns the available stats username and password as strings, as well an error when appropriate.
func getStatsAuth(statsUsernameFile, statsPasswordFile, statsUsername, statsPassword string) (string, string, error) {
	if len(statsUsernameFile) > 0 && len(statsPasswordFile) > 0 {
		usernameBytes, err := ioutil.ReadFile(statsUsernameFile)
		if err != nil {
			return "", "", err
		}
		passwordBytes, err := ioutil.ReadFile(statsPasswordFile)
		if err != nil {
			return "", "", err
		}
		statsUsername = string(usernameBytes)
		statsPassword = string(passwordBytes)
	}

	return statsUsername, statsPassword, nil
}
