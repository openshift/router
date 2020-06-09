package templaterouter

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"

	routev1 "github.com/openshift/api/route/v1"

	"github.com/openshift/router/pkg/router/routeapihelpers"
	templateutil "github.com/openshift/router/pkg/router/template/util"
	haproxyutil "github.com/openshift/router/pkg/router/template/util/haproxy"
)

const (
	certConfigMap = "cert_config.map"
)

func isTrue(s string) bool {
	v, _ := strconv.ParseBool(s)
	return v
}

func firstMatch(pattern string, values ...string) string {
	log.V(7).Info("firstMatch called", "pattern", pattern, "values", values)
	if re, err := regexp.Compile(`\A(?:` + pattern + `)\z`); err == nil {
		for _, value := range values {
			if re.MatchString(value) {
				log.V(7).Info("firstMatch returning", "value", value)
				return value
			}
		}
		log.V(7).Info("firstMatch returning empty string")
	} else {
		log.Error(err, "error with regex pattern in call to firstMatch")
	}
	return ""
}

func env(name string, defaults ...string) string {
	if envValue := os.Getenv(name); envValue != "" {
		return envValue
	}

	for _, val := range defaults {
		if val != "" {
			return val
		}
	}

	return ""
}

func isInteger(s string) bool {
	_, err := strconv.Atoi(s)
	return (err == nil)
}

func matchValues(s string, allowedValues ...string) bool {
	log.V(7).Info("matchValues called", "s", s, "allowedValues", allowedValues)
	for _, value := range allowedValues {
		if value == s {
			log.V(7).Info("matchValues finds matching string", "s", s)
			return true
		}
	}
	log.V(7).Info("matchValues cannot match string", "s", s)
	return false
}

func matchPattern(pattern, s string) bool {
	log.V(7).Info("matchPattern called", "pattern", pattern, "s", s)
	status, err := regexp.MatchString(`\A(?:`+pattern+`)\z`, s)
	if err == nil {
		log.V(7).Info("matchPattern returning", "foundMatch", status)
		return status
	}
	log.Error(err, "error with regex pattern in call to matchPattern")
	return false
}

// genSubdomainWildcardRegexp is now legacy and around for backward
// compatibility and allows old templates to continue running.
// Generate a regular expression to match wildcard hosts (and paths if any)
// for a [sub]domain.
func genSubdomainWildcardRegexp(hostname, path string, exactPath bool) string {
	subdomain := routeapihelpers.GetDomainForHost(hostname)
	if len(subdomain) == 0 {
		log.V(0).Info("generating subdomain wildcard regexp - invalid host name", "hostname", hostname)
		return fmt.Sprintf("%s%s", hostname, path)
	}

	expr := regexp.QuoteMeta(fmt.Sprintf(".%s%s", subdomain, path))
	if exactPath {
		return fmt.Sprintf(`^[^\.]*%s$`, expr)
	}

	return fmt.Sprintf(`^[^\.]*%s(|/.*)$`, expr)
}

// generateRouteRegexp is now legacy and around for backward
// compatibility and allows old templates to continue running.
// Generate a regular expression to match route hosts (and paths if any).
func generateRouteRegexp(hostname, path string, wildcard bool) string {
	return templateutil.GenerateRouteRegexp(hostname, path, wildcard)
}

// genCertificateHostName is now legacy and around for backward
// compatibility and allows old templates to continue running.
// Generates the host name to use for serving/certificate matching.
// If wildcard is set, a wildcard host name (*.<subdomain>) is generated.
func genCertificateHostName(hostname string, wildcard bool) string {
	return templateutil.GenCertificateHostName(hostname, wildcard)
}

// processEndpointsForAlias returns the list of endpoints for the given route's service
// action argument further processes the list e.g. shuffle
// The default action is in-order traversal of internal data structure that stores
//   the endpoints (does not change the return order if the data structure did not mutate)
func processEndpointsForAlias(alias ServiceAliasConfig, svc ServiceUnit, action string) []Endpoint {
	endpoints := endpointsForAlias(alias, svc)
	if strings.ToLower(action) == "shuffle" {
		for i := len(endpoints) - 1; i >= 0; i-- {
			rIndex := rand.Intn(i + 1)
			endpoints[i], endpoints[rIndex] = endpoints[rIndex], endpoints[i]
		}
	}
	return endpoints
}

func endpointsForAlias(alias ServiceAliasConfig, svc ServiceUnit) []Endpoint {
	if len(alias.PreferPort) == 0 {
		return svc.EndpointTable
	}
	endpoints := make([]Endpoint, 0, len(svc.EndpointTable))
	for i := range svc.EndpointTable {
		endpoint := svc.EndpointTable[i]
		if endpoint.PortName == alias.PreferPort || endpoint.Port == alias.PreferPort {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

// backendConfig returns a haproxy backend config for a given service alias.
func backendConfig(name string, cfg ServiceAliasConfig, hascert bool) *haproxyutil.BackendConfig {
	return &haproxyutil.BackendConfig{
		Name:           name,
		Host:           cfg.Host,
		Path:           cfg.Path,
		IsWildcard:     cfg.IsWildcard,
		Termination:    cfg.TLSTermination,
		InsecurePolicy: cfg.InsecureEdgeTerminationPolicy,
		HasCertificate: hascert,
	}
}

// generateHAProxyCertConfigMap generates haproxy certificate config map contents.
func generateHAProxyCertConfigMap(td templateData) []string {
	lines := make([]string, 0)
	for k, cfg := range td.State {
		hascert := false
		if len(cfg.Host) > 0 {
			cert, ok := cfg.Certificates[cfg.Host]
			hascert = ok && len(cert.Contents) > 0
		}

		backendConfig := backendConfig(string(k), cfg, hascert)
		if entry := haproxyutil.GenerateMapEntry(certConfigMap, backendConfig); entry != nil {
			fqCertPath := path.Join(td.WorkingDir, certDir, entry.Key)
			if td.DisableHTTP2 {
				lines = append(lines, strings.Join([]string{fqCertPath, entry.Value}, " "))
			} else {
				lines = append(lines, strings.Join([]string{fqCertPath, "[alpn h2,http/1.1]", entry.Value}, " "))
			}
		}
	}

	sort.Sort(sort.Reverse(sort.StringSlice(lines)))
	return lines
}

// validateHAProxyWhiteList validates a whitelist for use with an haproxy acl.
func validateHAProxyWhiteList(value string) bool {
	_, valid := haproxyutil.ValidateWhiteList(value)
	return valid
}

// generateHAProxyWhiteListFile generates a whitelist file for use with an haproxy acl.
func generateHAProxyWhiteListFile(workingDir, id, value string) string {
	name := path.Join(workingDir, whitelistDir, fmt.Sprintf("%s.txt", id))
	cidrs, _ := haproxyutil.ValidateWhiteList(value)
	data := []byte(strings.Join(cidrs, "\n") + "\n")
	if err := ioutil.WriteFile(name, data, 0644); err != nil {
		log.Error(err, "error writing haproxy whitelist contents")
		return ""
	}

	return name
}

// getHTTPAliasesGroupedByHost returns HTTP(S) aliases grouped by their host.
func getHTTPAliasesGroupedByHost(aliases map[ServiceAliasConfigKey]ServiceAliasConfig) map[string]map[ServiceAliasConfigKey]ServiceAliasConfig {
	result := make(map[string]map[ServiceAliasConfigKey]ServiceAliasConfig)

	for k, a := range aliases {
		if a.TLSTermination == routev1.TLSTerminationPassthrough {
			continue
		}

		if _, exists := result[a.Host]; !exists {
			result[a.Host] = make(map[ServiceAliasConfigKey]ServiceAliasConfig)
		}
		result[a.Host][k] = a
	}

	return result
}

// getPrimaryAliasKey returns the key of the primary alias for a group of aliases.
// It is assumed that all input aliases have the same host.
// In case of a single alias, the primary alias is the alias itself.
// In case of multiple alias with no TSL termination (Edge or Passthrough),
// the primary alias is the alphabetically last alias.
// In case of multiple aliases, some of them with TLS termination, the primary alias is
// the alphabetically last alias among the TLS aliases.
func getPrimaryAliasKey(aliases map[string]ServiceAliasConfig) string {
	if len(aliases) == 0 {
		return ""
	}

	if len(aliases) == 1 {
		for k := range aliases {
			return k
		}
	}

	keys := make([]string, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	for _, k := range keys {
		if aliases[k].TLSTermination == routev1.TLSTerminationEdge || aliases[k].TLSTermination == routev1.TLSTerminationReencrypt {
			return k
		}
	}

	return keys[0]
}

// generateHAProxyMap generates a named haproxy certificate config map contents.
func generateHAProxyMap(name string, td templateData) []string {
	if name == certConfigMap {
		return generateHAProxyCertConfigMap(td)
	}

	lines := make([]string, 0)
	for k, cfg := range td.State {
		backendConfig := backendConfig(string(k), cfg, false)
		if entry := haproxyutil.GenerateMapEntry(name, backendConfig); entry != nil {
			lines = append(lines, fmt.Sprintf("%s %s", entry.Key, entry.Value))
		}
	}

	return templateutil.SortMapPaths(lines, `^[^\.]*\.`)
}

var helperFunctions = template.FuncMap{
	"endpointsForAlias":        endpointsForAlias,        //returns the list of valid endpoints
	"processEndpointsForAlias": processEndpointsForAlias, //returns the list of valid endpoints after processing them
	"env":                      env,                      //tries to get an environment variable, returns the first non-empty default value or "" on failure
	"matchPattern":             matchPattern,             //anchors provided regular expression and evaluates against given string
	"isInteger":                isInteger,                //determines if a given variable is an integer
	"matchValues":              matchValues,              //compares a given string to a list of allowed strings

	"genSubdomainWildcardRegexp": genSubdomainWildcardRegexp,             //generates a regular expression matching the subdomain for hosts (and paths) with a wildcard policy
	"generateRouteRegexp":        generateRouteRegexp,                    //generates a regular expression matching the route hosts (and paths)
	"genCertificateHostName":     genCertificateHostName,                 //generates host name to use for serving/matching certificates
	"genBackendNamePrefix":       templateutil.GenerateBackendNamePrefix, //generates the prefix for the backend name

	"isTrue":     isTrue,     //determines if a given variable is a true value
	"firstMatch": firstMatch, //anchors provided regular expression and evaluates against given strings, returns the first matched string or ""

	"getHTTPAliasesGroupedByHost": getHTTPAliasesGroupedByHost, //returns HTTP(S) aliases grouped by their host
	"getPrimaryAliasKey":          getPrimaryAliasKey,          //returns the key of the primary alias for a group of aliases

	"generateHAProxyMap":           generateHAProxyMap,           //generates a haproxy map content
	"validateHAProxyWhiteList":     validateHAProxyWhiteList,     //validates a haproxy whitelist (acl) content
	"generateHAProxyWhiteListFile": generateHAProxyWhiteListFile, //generates a haproxy whitelist file for use in an acl
}
