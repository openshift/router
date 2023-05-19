package util

import (
	"errors"
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

var haproxyDurationRE = regexp.MustCompile(`^([0-9]+)(us|ms|s|m|h|d)?$`)

// HaproxyMaxTimeoutDuration is HaproxyMaxTimeout as a time.Duration value.
var HaproxyMaxTimeoutDuration = func() time.Duration {
	d, err := time.ParseDuration(HaproxyMaxTimeout)
	if err != nil {
		panic(err)
	}
	return d
}()

// OverflowError represents an overflow error from ParseHAProxyDuration.
// OverflowError is returned if the value is greater than what time.ParseDuration
// allows (value must be representable as uint64, e.g. approximately 2562047.79 hours
// or 9223372036854775807 nanoseconds).
type OverflowError struct {
	error
}

// InvalidInputError represents an error based on invalid input to ParseHAProxyDuration.
type InvalidInputError struct {
	error
}

// ParseHAProxyDuration is similar to time.ParseDuration, but modified with meaningful
// error messages for invalid input, with support for decimal points removed, with
// support for the "d" unit for days, and with "ms" as the default unit.
// It parses a duration string and returns a suitable duration. A duration string is a
// sequence of decimal numbers followed by a unit suffix,
// such as "300ms", "1h" or "45m".  If the unit suffix is omitted, "ms" is used.
// Valid time units are "us", "ms", "s", "m", "h", and "d".
func ParseHAProxyDuration(s string) (time.Duration, error) {
	orig := s
	var d uint64

	// Compare the input to the HAProxy duration regex.
	if !haproxyDurationRE.MatchString(s) {
		return 0, InvalidInputError{errors.New("invalid duration, no pattern match " + s)}
	}

	if s == "0" {
		return 0, nil
	}
	for s != "" {
		// The next character must be [0-9].
		if !('0' <= s[0] && s[0] <= '9') {
			return 0, InvalidInputError{errors.New("invalid duration, not a number " + orig)}
		}

		// Consume [0-9]*.
		var v uint64
		var err error
		v, s, err = leadingInt(s)
		if err != nil {
			// overflow.
			return 0, OverflowError{errors.New(err.Error() + orig)}
		}

		// Set the default HAProxy unit of "ms", in case a unit wasn't provided.
		u := "ms"
		// Consume unit.
		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if '0' <= c && c <= '9' {
				break
			}
		}
		if i > 0 {
			u = s[:i]
		}
		s = s[i:]
		unit, _ := unitMap[u]

		// Calculate the current value we've parsed, and check for overflow.
		if v > 1<<63/unit {
			// overflow.
			return HaproxyMaxTimeoutDuration, OverflowError{errors.New("invalid duration, overflow " + orig)}
		}
		v *= unit
		d += v
	}
	if d > 1<<63-1 {
		return HaproxyMaxTimeoutDuration, OverflowError{errors.New("invalid duration, overflow " + orig)}
	}
	return time.Duration(d), nil
}

// leadingInt consumes the leading [0-9]* from s, and comes from the time package,
// a private function used by time.ParseDuration, and needed by ParseHAProxyDuration above.
func leadingInt(s string) (x uint64, rem string, err error) {
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if x >= 1<<63/10 {
			// Potential for overflow on next iteration
			nextVal := x*10 + uint64(c) - '0'
			if nextVal > 1<<63-1 {
				overflowErr := OverflowError{errors.New("value too large to be represented as duration")}
				if i == len(s)-1 {
					return 0, "", overflowErr
				}
				return 0, s[i+1:], overflowErr
			}
		}
		x = x*10 + uint64(c) - '0'
	}
	return x, s[i:], nil
}

// unitMap is used by ParseHAProxyDuration for mapping a string-based unit
// to a time-based uint64.
var unitMap = map[string]uint64{
	"us": uint64(time.Microsecond),
	"ms": uint64(time.Millisecond),
	"s":  uint64(time.Second),
	"m":  uint64(time.Minute),
	"h":  uint64(time.Hour),
	"d":  uint64(time.Hour * 24), // HAProxy does accept d, convert to h for parsing
}
