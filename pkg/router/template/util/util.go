package util

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/router/pkg/router/routeapihelpers"

	logf "github.com/openshift/router/log"
)

const (
	// HaproxyMaxTimeout is the max timeout allowable by HAProxy.
	HaproxyMaxTimeout = "2147483647ms"

	// HaproxyDefaultTimeout is the default timeout to use when the
	// timeout value is not parseable for reasons other than it is too large.
	HaproxyDefaultTimeout = "5s"
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

	// The path could contain space characters, which must be escaped in
	// HAProxy map files.  See
	// <https://bugzilla.redhat.com/show_bug.cgi?id=2074304>.
	pathRE = strings.ReplaceAll(pathRE, ` `, `\x20`)
	pathRE = strings.ReplaceAll(pathRE, "\t", `\t`)
	pathRE = strings.ReplaceAll(pathRE, "\r", `\r`)
	pathRE = strings.ReplaceAll(pathRE, "\n", `\n`)

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

// HaproxyMaxTimeoutDuration is HaproxyMaxTimeout as a time.Duration value.
var HaproxyMaxTimeoutDuration = func() time.Duration {
	d, err := time.ParseDuration(HaproxyMaxTimeout)
	if err != nil {
		panic(err)
	}
	return d
}()