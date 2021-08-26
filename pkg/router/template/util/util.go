package util

import (
	"fmt"
	"regexp"
	"strings"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/openshift/router/pkg/router/routeapihelpers"

	logf "github.com/openshift/router/log"
)

var log = logf.Logger.WithName("util")

// generateRouteHostRegexp generates a regular expression to match route hosts.
func generateRouteHostRegexp(hostname string, wildcard bool) string {
	hostRE := regexp.QuoteMeta(hostname)
	if wildcard {
		subdomain := routeapihelpers.GetDomainForHost(hostname)
		if len(subdomain) == 0 {
			log.V(0).Info("generating subdomain wildcard regexp - invalid host name", "hostname", hostname)
		} else {
			subdomainRE := regexp.QuoteMeta(fmt.Sprintf(".%s", subdomain))
			hostRE = fmt.Sprintf(`[^\.]*%s`, subdomainRE)
		}
	}
	return hostRE
}

// GenerateRouteRegexp generates a regular expression to match routes, including
// host, optional port, and optional path.
func GenerateRouteRegexp(hostname, path string, wildcard bool) string {
	hostRE := fmt.Sprintf("%s\\.?", generateRouteHostRegexp(hostname, wildcard))

	portRE := "(:[0-9]+)?"

	// build the correct subpath regex, depending on whether path ends with a segment separator
	var pathRE, subpathRE string
	switch {
	case len(strings.TrimRight(path, "/")) == 0:
		// Special-case paths consisting solely of "/" to match a root request to "" as well
		pathRE = ""
		subpathRE = "(/.*)?"
	case strings.HasSuffix(path, "/"):
		pathRE = regexp.QuoteMeta(path)
		subpathRE = "(.*)?"
	default:
		pathRE = regexp.QuoteMeta(path)
		subpathRE = "(/.*)?"
	}

	return "^" + hostRE + portRE + pathRE + subpathRE + "$"
}

// GenerateSNIRegexp generates a regular expression to match route hosts against
// a server name in a TLS client hello message.
func GenerateSNIRegexp(hostname string, wildcard bool) string {
	return "^" + generateRouteHostRegexp(hostname, wildcard) + "$"
}

// GenCertificateHostName generates the host name to use for serving/certificate matching.
// If wildcard is set, a wildcard host name (*.<subdomain>) is generated.
func GenCertificateHostName(hostname string, wildcard bool) string {
	if wildcard {
		if idx := strings.IndexRune(hostname, '.'); idx > 0 {
			return fmt.Sprintf("*.%s", hostname[idx+1:])
		}
	}

	return hostname
}

// GenerateBackendNamePrefix generates the backend name prefix based on the termination.
func GenerateBackendNamePrefix(termination routev1.TLSTerminationType) string {
	prefix := "be_http"
	switch termination {
	case routev1.TLSTerminationEdge:
		prefix = "be_edge_http"
	case routev1.TLSTerminationReencrypt:
		prefix = "be_secure"
	case routev1.TLSTerminationPassthrough:
		prefix = "be_tcp"
	}

	return prefix
}
