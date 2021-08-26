package router

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	kclientset "k8s.io/client-go/kubernetes"

	routev1 "github.com/openshift/api/route/v1"
	projectclient "github.com/openshift/client-go/project/clientset/versioned/typed/project/v1"
	routeclientset "github.com/openshift/client-go/route/clientset/versioned"

	logf "github.com/openshift/router/log"
	"github.com/openshift/router/pkg/router/controller"
	controllerfactory "github.com/openshift/router/pkg/router/controller/factory"
)

var log = logf.Logger.WithName("router")

// RouterSelection controls what routes and resources on the server are considered
// part of this router.
type RouterSelection struct {
	RouterName              string
	RouterCanonicalHostname string

	ResyncInterval time.Duration

	UpdateStatus bool

	HostnameTemplate string
	OverrideHostname bool
	OverrideDomains  []string
	RedactedDomains  sets.String

	LabelSelector string
	FieldSelector string

	Namespace              string
	NamespaceLabelSelector string
	NamespaceLabels        labels.Selector

	ProjectLabelSelector string
	ProjectLabels        labels.Selector

	IncludeUDP bool

	DeniedDomains      []string
	BlacklistedDomains sets.String

	AllowedDomains     []string
	WhitelistedDomains sets.String

	AllowWildcardRoutes bool

	DisableNamespaceOwnershipCheck bool

	ExtendedValidation bool

	ListenAddr string

	// WatchEndpoints when true will watch Endpoints instead of
	// EndpointSlices.
	WatchEndpoints bool
}

// Bind sets the appropriate labels
func (o *RouterSelection) Bind(flag *pflag.FlagSet) {
	flag.StringVar(&o.RouterName, "name", env("ROUTER_SERVICE_NAME", "public"), "The name the router will identify itself with in the route status")
	flag.StringVar(&o.RouterCanonicalHostname, "router-canonical-hostname", env("ROUTER_CANONICAL_HOSTNAME", ""), "CanonicalHostname is the external host name for the router that can be used as a CNAME for the host requested for this route. This value is optional and may not be set in all cases.")
	flag.BoolVar(&o.UpdateStatus, "update-status", isTrue(env("ROUTER_UPDATE_STATUS", "true")), "If true, the router will update admitted route status.")
	flag.DurationVar(&o.ResyncInterval, "resync-interval", controllerfactory.DefaultResyncInterval, "The interval at which the route list should be fully refreshed")
	flag.StringVar(&o.HostnameTemplate, "hostname-template", env("ROUTER_SUBDOMAIN", ""), "If specified, a template that should be used to generate the hostname for a route without spec.host (e.g. '${name}-${namespace}.myapps.mycompany.com')")
	flag.BoolVar(&o.OverrideHostname, "override-hostname", isTrue(env("ROUTER_OVERRIDE_HOSTNAME", "")), "Override the spec.host value for a route with --hostname-template")
	flag.StringSliceVar(&o.OverrideDomains, "override-domains", envVarAsStrings("ROUTER_OVERRIDE_DOMAINS", "", ","), "List of comma separated domains to override if present in any routes. This overrides the spec.host value in any matching routes with --hostname-template")
	flag.StringVar(&o.LabelSelector, "labels", env("ROUTE_LABELS", ""), "A label selector to apply to the routes to watch")
	flag.StringVar(&o.FieldSelector, "fields", env("ROUTE_FIELDS", ""), "A field selector to apply to routes to watch")
	flag.StringVar(&o.ProjectLabelSelector, "project-labels", env("PROJECT_LABELS", ""), "A label selector to apply to projects to watch; if '*' watches all projects the client can access")
	flag.StringVar(&o.NamespaceLabelSelector, "namespace-labels", env("NAMESPACE_LABELS", ""), "A label selector to apply to namespaces to watch")
	flag.BoolVar(&o.IncludeUDP, "include-udp-endpoints", false, "If true, UDP endpoints will be considered as candidates for routing")
	flag.StringSliceVar(&o.DeniedDomains, "denied-domains", envVarAsStrings("ROUTER_DENIED_DOMAINS", "", ","), "List of comma separated domains to deny in routes")
	flag.StringSliceVar(&o.AllowedDomains, "allowed-domains", envVarAsStrings("ROUTER_ALLOWED_DOMAINS", "", ","), "List of comma separated domains to allow in routes. If specified, only the domains in this list will be allowed routes. Note that domains in the denied list take precedence over the ones in the allowed list")
	flag.BoolVar(&o.AllowWildcardRoutes, "allow-wildcard-routes", isTrue(env("ROUTER_ALLOW_WILDCARD_ROUTES", "")), "Allow wildcard host names for routes")
	flag.BoolVar(&o.DisableNamespaceOwnershipCheck, "disable-namespace-ownership-check", isTrue(env("ROUTER_DISABLE_NAMESPACE_OWNERSHIP_CHECK", "")), "Disables the namespace ownership checks for a route host with different paths or for overlapping host names in the case of wildcard routes. Please be aware that if namespace ownership checks are disabled, routes in a different namespace can use this mechanism to 'steal' sub-paths for existing domains. This is only safe if route creation privileges are restricted, or if all the users can be trusted.")
	flag.BoolVar(&o.ExtendedValidation, "extended-validation", isTrue(env("EXTENDED_VALIDATION", "true")), "If set, then an additional extended validation step is performed on all routes admitted in by this router. Defaults to true and enables the extended validation checks.")
	flag.Bool("enable-ingress", false, "Enable configuration via ingress resources.")
	flag.MarkDeprecated("enable-ingress", "Ingress resources are now synchronized to routes automatically.")
	flag.StringVar(&o.ListenAddr, "listen-addr", env("ROUTER_LISTEN_ADDR", ""), "The name of an interface to listen on to expose metrics and health checking. If not specified, will not listen. Overrides stats port.")
	flag.BoolVar(&o.WatchEndpoints, "watch-endpoints", isTrue(env("ROUTER_WATCH_ENDPOINTS", "")), "Watch Endpoints instead of the EndpointSlice resource.")
}

// RouteUpdate updates the route before it is seen by the cache.
func (o *RouterSelection) RouteUpdate(route *routev1.Route) {
	if len(o.HostnameTemplate) == 0 {
		return
	}
	if !o.OverrideHostname && len(route.Spec.Host) > 0 && !hostInDomainList(route.Spec.Host, o.RedactedDomains) {
		return
	}
	s, err := expandStrict(o.HostnameTemplate, func(key string) (string, bool) {
		switch key {
		case "name":
			return route.Name, true
		case "namespace":
			return route.Namespace, true
		default:
			return "", false
		}
	})
	if err != nil {
		return
	}

	s = strings.Trim(s, "\"'")
	log.V(4).Info("changing route", "fromHost", route.Spec.Host, "toHost", s)
	route.Spec.Host = s
}

func (o *RouterSelection) AdmissionCheck(route *routev1.Route) error {
	if len(route.Spec.Host) < 1 {
		return nil
	}

	if hostInDomainList(route.Spec.Host, o.BlacklistedDomains) {
		log.V(4).Info("host in list of denied domains", "routeName", route.Name, "host", route.Spec.Host)
		return fmt.Errorf("host in list of denied domains")
	}

	if o.WhitelistedDomains.Len() > 0 {
		log.V(4).Info("checking if host is in the list of allowed domains", "routeName", route.Name, "host", route.Spec.Host)
		if hostInDomainList(route.Spec.Host, o.WhitelistedDomains) {
			log.V(4).Info("host admitted - in the list of allowed domains", "routeName", route.Name, "host", route.Spec.Host)
			return nil
		}

		log.V(4).Info("host rejected - not in the list of allowed domains", "routeName", route.Name, "host", route.Spec.Host)
		return fmt.Errorf("host not in the allowed list of domains")
	}
	return nil
}

// RouteAdmissionFunc returns a func that checks if a route can be admitted
// based on blacklist & whitelist checks and wildcard routes policy setting.
// Note: The blacklist settings trumps the whitelist ones.
func (o *RouterSelection) RouteAdmissionFunc() controller.RouteAdmissionFunc {
	return func(route *routev1.Route) error {
		if err := o.AdmissionCheck(route); err != nil {
			return err
		}

		switch route.Spec.WildcardPolicy {
		case routev1.WildcardPolicyNone:
			return nil

		case routev1.WildcardPolicySubdomain:
			if o.AllowWildcardRoutes {
				return nil
			}
			return fmt.Errorf("wildcard routes are not allowed")
		}

		return fmt.Errorf("unknown wildcard policy %v", route.Spec.WildcardPolicy)
	}
}

// Complete converts string representations of field and label selectors to their parsed equivalent, or
// returns an error.
func (o *RouterSelection) Complete() error {
	if len(o.HostnameTemplate) == 0 && o.OverrideHostname {
		return fmt.Errorf("--override-hostname requires that --hostname-template be specified")
	}

	o.RedactedDomains = sets.NewString(o.OverrideDomains...)
	if len(o.RedactedDomains) > 0 && len(o.HostnameTemplate) == 0 {
		return fmt.Errorf("--override-domains requires that --hostname-template be specified")
	}

	if len(o.LabelSelector) > 0 {
		if _, err := labels.Parse(o.LabelSelector); err != nil {
			return fmt.Errorf("label selector is not valid: %v", err)
		}
	}

	if len(o.FieldSelector) > 0 {
		if _, err := fields.ParseSelector(o.FieldSelector); err != nil {
			return fmt.Errorf("field selector is not valid: %v", err)
		}
	}

	if len(o.ProjectLabelSelector) > 0 {
		if len(o.Namespace) > 0 {
			return fmt.Errorf("only one of --project-labels and --namespace may be used")
		}
		if len(o.NamespaceLabelSelector) > 0 {
			return fmt.Errorf("only one of --namespace-labels and --project-labels may be used")
		}

		if o.ProjectLabelSelector == "*" {
			o.ProjectLabels = labels.Everything()
		} else {
			s, err := labels.Parse(o.ProjectLabelSelector)
			if err != nil {
				return fmt.Errorf("--project-labels selector is not valid: %v", err)
			}
			o.ProjectLabels = s
		}
	}

	if len(o.NamespaceLabelSelector) > 0 {
		if len(o.Namespace) > 0 {
			return fmt.Errorf("only one of --namespace-labels and --namespace may be used")
		}
		s, err := labels.Parse(o.NamespaceLabelSelector)
		if err != nil {
			return fmt.Errorf("--namespace-labels selector is not valid: %v", err)
		}
		o.NamespaceLabels = s
	}

	o.BlacklistedDomains = sets.NewString(o.DeniedDomains...)
	o.WhitelistedDomains = sets.NewString(o.AllowedDomains...)

	if routerCanonicalHostname := o.RouterCanonicalHostname; len(routerCanonicalHostname) > 0 {
		if errs := validation.IsDNS1123Subdomain(routerCanonicalHostname); len(errs) != 0 {
			return fmt.Errorf("invalid canonical hostname: %s", routerCanonicalHostname)
		}
		if errs := validation.IsValidIP(routerCanonicalHostname); len(errs) == 0 {
			return fmt.Errorf("canonical hostname must not be an IP address: %s", routerCanonicalHostname)
		}
	}

	return nil
}

// NewFactory initializes a factory that will watch the requested routes
func (o *RouterSelection) NewFactory(routeclient routeclientset.Interface, projectclient projectclient.ProjectInterface, kc kclientset.Interface) *controllerfactory.RouterControllerFactory {
	factory := controllerfactory.NewDefaultRouterControllerFactory(routeclient, projectclient, kc, o.WatchEndpoints)
	factory.LabelSelector = o.LabelSelector
	factory.FieldSelector = o.FieldSelector
	factory.Namespace = o.Namespace
	factory.ResyncInterval = o.ResyncInterval
	switch {
	case o.NamespaceLabels != nil:
		log.V(0).Info("router is only using routes in namespaces matching labels", "labels", o.NamespaceLabels.String())
		factory.NamespaceLabels = o.NamespaceLabels
	case o.ProjectLabels != nil:
		log.V(0).Info("router is only using routes in projects matching labels", "labels", o.ProjectLabels.String())
		factory.ProjectLabels = o.ProjectLabels
	case len(factory.Namespace) > 0:
		log.V(0).Info("router is only using resources in namespace", "namespace", factory.Namespace)
	default:
		log.V(0).Info("router is including routes in all namespaces")
	}
	return factory
}

func envVarAsStrings(name, defaultValue, separator string) []string {
	strlist := []string{}
	if env := env(name, defaultValue); env != "" {
		values := strings.Split(env, separator)
		for i := range values {
			if val := strings.TrimSpace(values[i]); val != "" {
				strlist = append(strlist, val)
			}
		}
	}
	return strlist
}

func hostInDomainList(host string, domains sets.String) bool {
	if domains.Has(host) {
		return true
	}

	if idx := strings.IndexRune(host, '.'); idx > 0 {
		return hostInDomainList(host[idx+1:], domains)
	}

	return false
}

// newCmdVersion provides a shim around version for
// non-client packages that require version information
func newCmdVersion(fullName string, versionInfo string, out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version",
		Long:  "Display version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(out, "%s\n\n%v\n", fullName, versionInfo)
		},
	}

	return cmd
}

// env returns an environment variable or a default value if not specified.
func env(key string, defaultValue string) string {
	val := os.Getenv(key)
	if len(val) == 0 {
		return defaultValue
	}
	return val
}

func envInt(key string, defaultValue int32, minValue int32) int32 {
	value, err := strconv.ParseInt(env(key, fmt.Sprintf("%d", defaultValue)), 10, 32)
	if err != nil || int32(value) < minValue {
		return defaultValue
	}
	return int32(value)
}

// KeyFunc returns the value associated with the provided key or false if no
// such key exists.
type KeyFunc func(key string) (string, bool)

// expandStrict expands a string using a series of common format functions
func expandStrict(s string, fns ...KeyFunc) (string, error) {
	unmatched := []string{}
	result := os.Expand(s, func(key string) string {
		for _, fn := range fns {
			val, ok := fn(key)
			if !ok {
				continue
			}
			return val
		}
		unmatched = append(unmatched, key)
		return ""
	})

	switch len(unmatched) {
	case 0:
		return result, nil
	case 1:
		return "", fmt.Errorf("the key %q in %q is not recognized", unmatched[0], s)
	default:
		return "", fmt.Errorf("multiple keys in %q were not recognized: %s", s, strings.Join(unmatched, ", "))
	}
}
