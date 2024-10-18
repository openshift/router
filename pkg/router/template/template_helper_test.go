package templaterouter

import (
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"regexp"
	"strings"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	templateutil "github.com/openshift/router/pkg/router/template/util"
)

func buildServiceAliasConfig(name, namespace, host, path string, termination routev1.TLSTerminationType, policy routev1.InsecureEdgeTerminationPolicyType, wildcard bool) ServiceAliasConfig {
	certs := make(map[string]Certificate)
	if termination != routev1.TLSTerminationPassthrough {
		certs[host] = Certificate{
			ID:       fmt.Sprintf("id_%s", host),
			Contents: "abcdefghijklmnopqrstuvwxyz",
		}
	}

	return ServiceAliasConfig{
		Name:         name,
		Namespace:    namespace,
		Host:         host,
		Path:         path,
		IsWildcard:   wildcard,
		Certificates: certs,

		TLSTermination:                termination,
		InsecureEdgeTerminationPolicy: policy,
	}
}

func buildTestTemplateState() map[ServiceAliasConfigKey]ServiceAliasConfig {
	state := make(map[ServiceAliasConfigKey]ServiceAliasConfig)

	state["stg:api-route"] = buildServiceAliasConfig("api-route", "stg", "api-stg.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect, false)
	state["prod:api-route"] = buildServiceAliasConfig("api-route", "prod", "api-prod.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect, false)
	state["test:api-route"] = buildServiceAliasConfig("api-route", "test", "zzz-production.wildcard.test", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect, false)
	state["dev:api-route"] = buildServiceAliasConfig("api-route", "dev", "3dev.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyAllow, false)
	state["prod:api-path-route"] = buildServiceAliasConfig("api-path-route", "prod", "api-prod.127.0.0.1.nip.io", "/x/y/z", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyNone, false)

	state["prod:pt-route"] = buildServiceAliasConfig("pt-route", "prod", "passthrough-prod.127.0.0.1.nip.io", "", routev1.TLSTerminationPassthrough, routev1.InsecureEdgeTerminationPolicyNone, false)

	state["prod:wildcard-route"] = buildServiceAliasConfig("wildcard-route", "prod", "api-stg.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyNone, true)
	state["devel2:foo-wildcard-route"] = buildServiceAliasConfig("foo-wildcard-route", "devel2", "devel1.foo.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyAllow, true)
	state["devel2:foo-wildcard-test"] = buildServiceAliasConfig("foo-wildcard-test", "devel2", "something.foo.wildcard.test", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyAllow, true)
	state["dev:pt-route"] = buildServiceAliasConfig("pt-route", "dev", "passthrough-dev.127.0.0.1.nip.io", "", routev1.TLSTerminationPassthrough, routev1.InsecureEdgeTerminationPolicyNone, false)
	state["dev:reencrypt-route"] = buildServiceAliasConfig("reencrypt-route", "dev", "reencrypt-dev.127.0.0.1.nip.io", "", routev1.TLSTerminationReencrypt, routev1.InsecureEdgeTerminationPolicyRedirect, false)

	state["dev:admin-route"] = buildServiceAliasConfig("admin-route", "dev", "3app-admin.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyNone, false)

	state["prod:backend-route"] = buildServiceAliasConfig("backend-route", "prod", "backend-app.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect, false)
	state["zzz:zed-route"] = buildServiceAliasConfig("zed-route", "zzz", "zed.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyAllow, false)

	return state
}

func checkExpectedOrderPrefixes(lines, expectedOrder []string) error {
	if len(lines) != len(expectedOrder) {
		return fmt.Errorf("sorted data length %d did not match expected length %d", len(lines), len(expectedOrder))
	}

	for idx, prefix := range expectedOrder {
		if !strings.HasPrefix(lines[idx], prefix) {
			return fmt.Errorf("sorted data %s at index %d did not match prefix expectation %s", lines[idx], idx, prefix)
		}
	}

	return nil
}

func checkExpectedOrderSuffixes(lines, expectedOrder []string) error {
	if len(lines) != len(expectedOrder) {
		return fmt.Errorf("sorted data length %d did not match expected length %d", len(lines), len(expectedOrder))
	}

	for idx, suffix := range expectedOrder {
		if !strings.HasSuffix(lines[idx], suffix) {
			return fmt.Errorf("sorted data %s at index %d did not match suffix expectation %s", lines[idx], idx, suffix)
		}
	}

	return nil
}

func TestFirstMatch(t *testing.T) {
	testCases := []struct {
		name    string
		pattern string
		inputs  []string
		match   string
	}{
		// Make sure we are anchoring the regex at the start and end
		{
			name:    "exact match no-substring",
			pattern: `asd`,
			inputs:  []string{"123asd123", "asd456", "123asd", "asd"},
			match:   "asd",
		},
		// Test that basic regex stuff works
		{
			name:    "don't match newline",
			pattern: `.*asd.*`,
			inputs:  []string{"123\nasd123", "123asd123", "asd"},
			match:   "123asd123",
		},
		{
			name:    "match newline",
			pattern: `(?s).*asd.*`,
			inputs:  []string{"123\nasd123", "123asd123"},
			match:   "123\nasd123",
		},
		{
			name:    "match multiline",
			pattern: `(?m)(^asd\d$\n?)+`,
			inputs:  []string{"asd1\nasd2\nasd3\n", "asd1"},
			match:   "asd1\nasd2\nasd3\n",
		},
		{
			name:    "don't match multiline",
			pattern: `(^asd\d$\n?)+`,
			inputs:  []string{"asd1\nasd2\nasd3\n", "asd1", "asd2"},
			match:   "asd1",
		},
		// Make sure that we group their pattern separately from the anchors
		{
			name:    "prefix alternation",
			pattern: `|asd`,
			inputs:  []string{"anything"},
			match:   "",
		},
		{
			name:    "postfix alternation",
			pattern: `asd|`,
			inputs:  []string{"anything"},
			match:   "",
		},
		// Make sure that a change in anchor behaviors doesn't break us
		{
			name:    "substring behavior",
			pattern: `(?m)asd`,
			inputs:  []string{"asd\n123"},
			match:   "",
		},
	}

	for _, tt := range testCases {
		match := firstMatch(tt.pattern, tt.inputs...)
		if match != tt.match {
			t.Errorf("%s: expected match of %v to %s is '%s', but didn't", tt.name, tt.inputs, tt.pattern, tt.match)
		}
	}
}

func TestGenerateRouteRegexp(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		path     string
		wildcard bool

		match   []string
		nomatch []string
	}{
		{
			name:     "no path",
			hostname: "example.com",
			path:     "",
			wildcard: false,
			match: []string{
				"example.com",
				"example.com:80",
				"example.com/",
				"example.com/sub",
				"example.com/sub/",
			},
			nomatch: []string{"other.com"},
		},
		{
			name:     "root path with trailing slash",
			hostname: "example.com",
			path:     "/",
			wildcard: false,
			match: []string{
				"example.com",
				"example.com:80",
				"example.com/",
				"example.com/sub",
				"example.com/sub/",
			},
			nomatch: []string{"other.com"},
		},
		{
			name:     "subpath with trailing slash",
			hostname: "example.com",
			path:     "/sub/",
			wildcard: false,
			match: []string{
				"example.com/sub/",
				"example.com/sub/subsub",
			},
			nomatch: []string{
				"other.com",
				"example.com",
				"example.com:80",
				"example.com/",
				"example.com/sub",    // path with trailing slash doesn't match URL without
				"example.com/subpar", // path segment boundary match required
			},
		},
		{
			name:     "subpath without trailing slash",
			hostname: "example.com",
			path:     "/sub",
			wildcard: false,
			match: []string{
				"example.com/sub",
				"example.com/sub/",
				"example.com/sub/subsub",
			},
			nomatch: []string{
				"other.com",
				"example.com",
				"example.com:80",
				"example.com/",
				"example.com/subpar", // path segment boundary match required
			},
		},
		{
			name:     "wildcard",
			hostname: "www.example.com",
			path:     "/",
			wildcard: true,
			match: []string{
				"www.example.com",
				"www.example.com/",
				"www.example.com/sub",
				"www.example.com/sub/",
				"www.example.com:80",
				"www.example.com:80/",
				"www.example.com:80/sub",
				"www.example.com:80/sub/",
				"foo.example.com",
				"foo.example.com/",
				"foo.example.com/sub",
				"foo.example.com/sub/",
			},
			nomatch: []string{
				"wwwexample.com",
				"foo.bar.example.com",
			},
		},
		{
			name:     "non-wildcard",
			hostname: "www.example.com",
			path:     "/",
			wildcard: false,
			match: []string{
				"www.example.com",
				"www.example.com/",
				"www.example.com/sub",
				"www.example.com/sub/",
				"www.example.com:80",
				"www.example.com:80/",
				"www.example.com:80/sub",
				"www.example.com:80/sub/",
			},
			nomatch: []string{
				"foo.example.com",
				"foo.example.com/",
				"foo.example.com/sub",
				"foo.example.com/sub/",
				"wwwexample.com",
				"foo.bar.example.com",
			},
		},
	}

	for _, tt := range tests {
		r := regexp.MustCompile(generateRouteRegexp(tt.hostname, tt.path, tt.wildcard))
		for _, s := range tt.match {
			if !r.Match([]byte(s)) {
				t.Errorf("%s: expected %s to match %s, but didn't", tt.name, r, s)
			}
		}
		for _, s := range tt.nomatch {
			if r.Match([]byte(s)) {
				t.Errorf("%s: expected %s not to match %s, but did", tt.name, r, s)
			}
		}
	}
}

func TestMatchPattern(t *testing.T) {
	testMatches := []struct {
		name    string
		pattern string
		input   string
	}{
		// Test that basic regex stuff works
		{
			name:    "exact match",
			pattern: `asd`,
			input:   "asd",
		},
		{
			name:    "basic regex",
			pattern: `.*asd.*`,
			input:   "123asd123",
		},
		{
			name:    "match newline",
			pattern: `(?s).*asd.*`,
			input:   "123\nasd123",
		},
		{
			name:    "match multiline",
			pattern: `(?m)(^asd\d$\n?)+`,
			input:   "asd1\nasd2\nasd3\n",
		},
	}

	testNoMatches := []struct {
		name    string
		pattern string
		input   string
	}{
		// Make sure we are anchoring the regex at the start and end
		{
			name:    "no-substring",
			pattern: `asd`,
			input:   "123asd123",
		},
		// Make sure that we group their pattern separately from the anchors
		{
			name:    "prefix alternation",
			pattern: `|asd`,
			input:   "anything",
		},
		{
			name:    "postfix alternation",
			pattern: `asd|`,
			input:   "anything",
		},
		// Make sure that a change in anchor behaviors doesn't break us
		{
			name:    "substring behavior",
			pattern: `(?m)asd`,
			input:   "asd\n123",
		},
		// Check some other regex things that should fail
		{
			name:    "don't match newline",
			pattern: `.*asd.*`,
			input:   "123\nasd123",
		},
		{
			name:    "don't match multiline",
			pattern: `(^asd\d$\n?)+`,
			input:   "asd1\nasd2\nasd3\n",
		},
	}

	for _, tt := range testMatches {
		match := matchPattern(tt.pattern, tt.input)
		if !match {
			t.Errorf("%s: expected %s to match %s, but didn't", tt.name, tt.input, tt.pattern)
		}
	}

	for _, tt := range testNoMatches {
		match := matchPattern(tt.pattern, tt.input)
		if match {
			t.Errorf("%s: expected %s not to match %s, but did", tt.name, tt.input, tt.pattern)
		}
	}
}

func createTempMapFile(prefix string, data []string) (string, error) {
	name := ""
	tempFile, err := ioutil.TempFile("", prefix)
	if err != nil {
		return "", fmt.Errorf("unexpected error creating temp file: %v", err)
	}

	name = tempFile.Name()
	if err = tempFile.Close(); err != nil {
		return name, fmt.Errorf("unexpected error creating temp file: %v", err)
	}

	if err := ioutil.WriteFile(name, []byte(strings.Join(data, "\n")), 0664); err != nil {
		return name, fmt.Errorf("unexpected error writing temp file %s: %v", name, err)
	}

	return name, nil
}

func TestGenerateHAProxyCertConfigMap(t *testing.T) {
	td := templateData{
		WorkingDir:   "/path/to",
		State:        buildTestTemplateState(),
		ServiceUnits: make(map[ServiceUnitKey]ServiceUnit),
	}

	expectedOrder := []string{
		"/path/to/router/certs/zzz:zed-route.pem",
		"/path/to/router/certs/test:api-route.pem",
		"/path/to/router/certs/stg:api-route.pem",
		"/path/to/router/certs/prod:wildcard-route.pem",
		"/path/to/router/certs/prod:backend-route.pem",
		"/path/to/router/certs/prod:api-route.pem",
		"/path/to/router/certs/prod:api-path-route.pem",
		"/path/to/router/certs/devel2:foo-wildcard-test.pem",
		"/path/to/router/certs/devel2:foo-wildcard-route.pem",
		"/path/to/router/certs/dev:reencrypt-route.pem",
		"/path/to/router/certs/dev:api-route.pem",
		"/path/to/router/certs/dev:admin-route.pem",
	}

	lines := generateHAProxyCertConfigMap(td)
	if err := checkExpectedOrderPrefixes(lines, expectedOrder); err != nil {
		t.Errorf("TestGenerateHAProxyCertConfigMap error: %v", err)
	}
}

func TestGenerateHAProxyMap(t *testing.T) {
	td := templateData{
		WorkingDir:   "/path/to",
		State:        buildTestTemplateState(),
		ServiceUnits: make(map[ServiceUnitKey]ServiceUnit),
	}

	wildcardDomainOrder := []string{
		`^[^\.]*\.foo\.wildcard\.test\.?(:[0-9]+)?(/.*)?$`,
		`^[^\.]*\.foo\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$`,
		`^[^\.]*\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$`,
	}

	lines := generateHAProxyMap("os_wildcard_domain.map", td)
	if err := checkExpectedOrderPrefixes(lines, wildcardDomainOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_tcp_be.map error: %v", err)
	}

	httpBackendOrder := []string{
		"be_edge_http:zzz:zed-route",
		"be_edge_http:dev:api-route",
		"be_edge_http:devel2:foo-wildcard-test",
		"be_edge_http:devel2:foo-wildcard-route",
	}

	lines = generateHAProxyMap("os_http_be.map", td)
	if err := checkExpectedOrderSuffixes(lines, httpBackendOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_http_be.map error: %v", err)
	}

	edgeReencryptOrder := []string{
		"be_edge_http:test:api-route",
		"be_edge_http:zzz:zed-route",
		"be_secure:dev:reencrypt-route",
		"be_edge_http:prod:backend-route",
		"be_edge_http:stg:api-route",
		"be_edge_http:prod:api-path-route",
		"be_edge_http:prod:api-route",
		"be_edge_http:dev:api-route",
		"be_edge_http:dev:admin-route",
		"be_edge_http:devel2:foo-wildcard-test",
		"be_edge_http:devel2:foo-wildcard-route",
		"be_edge_http:prod:wildcard-route",
	}

	lines = generateHAProxyMap("os_edge_reencrypt_be.map", td)
	if err := checkExpectedOrderSuffixes(lines, edgeReencryptOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_edge_reencrypt_be.map error: %v", err)
	}

	// Need to add these as now we add all the routes in the redirect map which don't have Redirect policy
	// but we differentiate them based on  values 1 and 0 where 1 means Redirect Policy is enabled.
	httpRedirectOrder := []string{
		`^zzz-production\.wildcard\.test\.?(:[0-9]+)?(/.*)?$ 1`,
		`^zed\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^reencrypt-dev\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 1`,
		`^passthrough-prod\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^passthrough-dev\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^backend-app\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 1`,
		`^api-stg\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 1`,
		`^api-prod\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?/x/y/z(/.*)?$ 0`,
		`^api-prod\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 1`,
		`^3dev\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^3app-admin\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^[^\.]*\.foo\.wildcard\.test\.?(:[0-9]+)?(/.*)?$ 0`,
		`^[^\.]*\.foo\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
		`^[^\.]*\.127\.0\.0\.1\.nip\.io\.?(:[0-9]+)?(/.*)?$ 0`,
	}

	lines = generateHAProxyMap("os_route_http_redirect.map", td)
	if err := checkExpectedOrderSuffixes(lines, httpRedirectOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_route_http_redirect.map error: %v", err)
	}

	passthroughOrder := []string{
		"dev:reencrypt-route",
		"prod:pt-route",
		"dev:pt-route",
	}

	lines = generateHAProxyMap("os_tcp_be.map", td)
	if err := checkExpectedOrderSuffixes(lines, passthroughOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_tcp_be.map error: %v", err)
	}

	sniPassthroughOrder := []string{
		`^passthrough-prod\.127\.0\.0\.1\.nip\.io$`,
		`^passthrough-dev\.127\.0\.0\.1\.nip\.io$`,
	}

	lines = generateHAProxyMap("os_sni_passthrough.map", td)
	if err := checkExpectedOrderPrefixes(lines, sniPassthroughOrder); err != nil {
		t.Errorf("TestGenerateHAProxyMap os_sni_passthrough.map error: %v", err)
	}

	certBackendOrder := []string{
		"/path/to/router/certs/zzz:zed-route.pem",
		"/path/to/router/certs/test:api-route.pem",
		"/path/to/router/certs/stg:api-route.pem",
		"/path/to/router/certs/prod:wildcard-route.pem",
		"/path/to/router/certs/prod:backend-route.pem",
		"/path/to/router/certs/prod:api-route.pem",
		"/path/to/router/certs/prod:api-path-route.pem",
		"/path/to/router/certs/devel2:foo-wildcard-test.pem",
		"/path/to/router/certs/devel2:foo-wildcard-route.pem",
		"/path/to/router/certs/dev:reencrypt-route.pem",
		"/path/to/router/certs/dev:api-route.pem",
		"/path/to/router/certs/dev:admin-route.pem",
	}

	for _, tc := range []struct {
		DisableHTTP2       bool
		ExpectedSSLBinding string
	}{
		{
			DisableHTTP2: true,
		},
		{
			DisableHTTP2:       false,
			ExpectedSSLBinding: "[alpn h2,http/1.1]",
		},
	} {
		td.DisableHTTP2 = tc.DisableHTTP2
		lines := generateHAProxyMap("cert_config.map", td)
		if err := checkExpectedOrderPrefixes(lines, certBackendOrder); err != nil {
			t.Errorf("TestGenerateHAProxyMap cert_config.map error: %v", err)
		}
		if tc.ExpectedSSLBinding != "" {
			for _, line := range lines {
				if !strings.Contains(line, tc.ExpectedSSLBinding) {
					t.Errorf("line %q does not contain expected SSL binding %q", line, tc.ExpectedSSLBinding)
				}
			}
		}
	}
}

func TestGetHTTPAliasesGroupedByHost(t *testing.T) {
	aliases := map[ServiceAliasConfigKey]ServiceAliasConfig{
		"project1:route1": {
			Host: "example.com",
			Path: "/",
		},
		"project2:route1": {
			Host: "example.org",
			Path: "/v1",
		},
		"project2:route2": {
			Host: "example.org",
			Path: "/v2",
		},
		"project3.route3": {
			Host:           "example.net",
			TLSTermination: routev1.TLSTerminationPassthrough,
		},
	}

	expected := map[string]map[ServiceAliasConfigKey]ServiceAliasConfig{
		"example.com": {
			"project1:route1": {
				Host: "example.com",
				Path: "/",
			},
		},
		"example.org": {
			"project2:route1": {
				Host: "example.org",
				Path: "/v1",
			},
			"project2:route2": {
				Host: "example.org",
				Path: "/v2",
			},
		},
	}

	result := getHTTPAliasesGroupedByHost(aliases)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("TestGroupAliasesByHost failed. Got %v expected %v", result, expected)
	}
}

func TestGetPrimaryAliasKey(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]ServiceAliasConfig
		expected string
	}{
		{
			name:     "zero input",
			input:    make(map[string]ServiceAliasConfig),
			expected: "",
		},
		{
			name: "Single alias",
			input: map[string]ServiceAliasConfig{
				"project2:route1": {
					Host: "example.org",
					Path: "/v1",
				},
			},
			expected: "project2:route1",
		},
		{
			name: "Aliases with Edge Termination",
			input: map[string]ServiceAliasConfig{
				"project1:route-3": {
					Host:           "example.com",
					Path:           "/",
					TLSTermination: routev1.TLSTerminationEdge,
				},
				"project1:route-1": {
					Host:           "example.com",
					Path:           "/path1",
					TLSTermination: routev1.TLSTerminationEdge,
				},
				"project1:route-2": {
					Host:           "example.com",
					Path:           "/path2",
					TLSTermination: routev1.TLSTerminationEdge,
				},
				"project1:route-4": {
					Host: "example.com",
					Path: "/path4",
				},
			},
			expected: "project1:route-3",
		},
		{
			name: "Aliases with Reencrypt Termination",
			input: map[string]ServiceAliasConfig{
				"project1:route-3": {
					Host:           "example.com",
					Path:           "/",
					TLSTermination: routev1.TLSTerminationReencrypt,
				},
				"project1:route-1": {
					Host:           "example.com",
					Path:           "/path1",
					TLSTermination: routev1.TLSTerminationReencrypt,
				},
				"project1:route-2": {
					Host:           "example.com",
					Path:           "/path2",
					TLSTermination: routev1.TLSTerminationReencrypt,
				},
				"project1:route-4": {
					Host: "example.com",
					Path: "/path4",
				},
			},
			expected: "project1:route-3",
		},
		{
			name: "Non-TLS aliases",
			input: map[string]ServiceAliasConfig{
				"project1:route-3": {
					Host: "example.com",
					Path: "/",
				},
				"project1:route-1": {
					Host: "example.com",
					Path: "/path1",
				},
				"project1:route-2": {
					Host: "example.com",
					Path: "/path2",
				},
				"project1:route-4": {
					Host: "example.com",
					Path: "/path4",
				},
			},
			expected: "project1:route-4",
		},
	}

	for _, test := range testCases {
		result := getPrimaryAliasKey(test.input)

		if result != test.expected {
			t.Errorf("getPrimaryAliasKey failed. When testing for %v got %v expected %v", test.name, result, test.expected)
		}
	}
}

func TestProcessEndpointsForAlias(t *testing.T) {
	router := NewFakeTemplateRouter()
	alias := buildServiceAliasConfig("api-route", "stg", "api-stg.127.0.0.1.nip.io", "", routev1.TLSTerminationEdge, routev1.InsecureEdgeTerminationPolicyRedirect, false)
	suKey := ServiceUnitKey("stg/svc")
	router.CreateServiceUnit(suKey)
	ep1 := Endpoint{
		ID:     "ep1",
		IP:     "ip",
		Port:   "foo",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep1ipport"))),
	}
	ep2 := Endpoint{
		ID:     "ep2",
		IP:     "ip",
		Port:   "foo",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep2ipport"))),
	}
	ep3 := Endpoint{
		ID:     "ep3",
		IP:     "ip",
		Port:   "bar",
		IdHash: fmt.Sprintf("%x", md5.Sum([]byte("ep3ipport"))),
	}

	testCases := []struct {
		name           string
		preferPort     string
		endpoints      []Endpoint
		expectedLength int
	}{
		{
			name:           "2 basic endpoints with same Port string",
			preferPort:     "foo",
			endpoints:      []Endpoint{ep1, ep2},
			expectedLength: 2,
		},
		{
			name:           "3 basic endpoints with different Port string",
			preferPort:     "foo",
			endpoints:      []Endpoint{ep1, ep2, ep3},
			expectedLength: 2,
		},
	}

	for _, tc := range testCases {
		alias.PreferPort = tc.preferPort
		endpointsCopy := make([]Endpoint, len(tc.endpoints))
		for i := range tc.endpoints {
			endpointsCopy[i] = tc.endpoints[i]
		}
		router.AddEndpoints(suKey, endpointsCopy)
		svc, _ := router.FindServiceUnit(suKey)
		endpoints := processEndpointsForAlias(alias, svc, "")
		if len(endpoints) != tc.expectedLength {
			t.Errorf("test %s: got wrong number of endpoints. Expected %d got %d", tc.name, tc.expectedLength, len(endpoints))
		}
		if len(tc.endpoints) == tc.expectedLength {
			if !reflect.DeepEqual(tc.endpoints, endpoints) {
				t.Errorf("test %s: endpoints out of order. Expected %v got %v", tc.name, tc.endpoints, endpoints)
			}
		}
		router.DeleteEndpoints(suKey)
	}
}

func TestClipHAProxyTimeoutValue(t *testing.T) {
	testCases := []struct {
		value    string
		expected string
	}{
		{
			value:    "",
			expected: "",
		},
		{
			value:    "s",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "0",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "01s",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "1.5.8.9",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "1.5s",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "+-+",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "24d1",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "1d12h",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "foo",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "2562047.99h",
			expected: "",
			// Invalid input produces blank output.
		},
		{
			value:    "10",
			expected: "10",
		},
		{
			value:    "10s",
			expected: "10s",
		},
		{
			value:    "10d",
			expected: "10d",
		},
		{
			value:    "24d",
			expected: "24d",
		},
		{
			value:    "2147483647ms",
			expected: "2147483647ms",
		},
		{
			value:    "100d",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the HAProxy maximum.
		},
		{
			value:    "1000h",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the HAProxy maximum.
		},
		{
			value:    "2147483648",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the HAProxy maximum and has no unit.
		},
		{
			value:    "9223372036855ms",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the haproxytime.ParseDuration maximum.
		},
		{
			value:    "9223372036854776us",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the haproxytime.ParseDuration maximum.
		},
		{
			value:    "100000000000s",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the haproxytime.ParseDuration maximum.
		},
		{
			value:    "922337203685477581ms",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the haproxytime.ParseDuration maximum.
		},
		{
			value:    "9223372036854775807",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the int64 maximum and has no unit.
		},
		{
			value:    "9999999999999999",
			expected: templateutil.HaproxyMaxTimeout,
			// Exceeds the haproxytime.ParseDuration maximum and has no unit.
		},
	}
	for _, tc := range testCases {
		actual := clipHAProxyTimeoutValue(tc.value)
		if actual != tc.expected {
			t.Errorf("clipHAProxyTimeoutValue yielded incorrect result: expected %s but got %s", tc.expected, actual)
		}
	}
}

func Test_generateHAProxyAllowlistFile(t *testing.T) {
	workDir := t.TempDir()

	err := os.MkdirAll(path.Join(workDir, allowlistDir), 0740)
	if err != nil {
		t.Fatal("Unable to create the allowlist directory")
	}

	testCases := []struct {
		name              string
		workDir           string
		id                ServiceAliasConfigKey
		expectedAllowlist []string
		failureExpected   bool
	}{
		{
			name:    "Nominal",
			workDir: workDir,
			id:      ServiceAliasConfigKey("test1"),
			expectedAllowlist: []string{
				"192.168.0.1",
				"192.168.0.2",
				"192.168.0.3",
			},
		},
		{
			name:    "Nominal failure",
			workDir: workDir + "-notexisting",
			id:      ServiceAliasConfigKey("test2"),
			expectedAllowlist: []string{
				"192.168.0.1",
				"192.168.0.2",
				"192.168.0.3",
			},
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			file := generateHAProxyAllowlistFile(tc.workDir, tc.id, strings.Join(tc.expectedAllowlist, " "))
			if tc.failureExpected {
				if file != "" {
					t.Fatal("Failure expected but didn't happen")
				}
				return
			} else {
				if file == "" {
					t.Fatal("Unexpected failure")
				}
			}

			contents, err := ioutil.ReadFile(file)
			if err != nil {
				t.Fatalf("Unable to read from the generated file: %v", err)
			}
			gotAllowlist := strings.Fields(string(contents))
			if !reflect.DeepEqual(tc.expectedAllowlist, gotAllowlist) {
				t.Errorf("Wrong allowlist written: expected %q, got %q", tc.expectedAllowlist, gotAllowlist)
			}
		})
	}
}

func TestParseIPList(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		expectedEmpty bool
	}{
		{
			name:  "All mixed",
			input: "192.168.1.0 2001:0db8:85a3:0000:0000:8a2e:0370:7334 172.16.14.10/24 2001:0db8:85a3::8a2e:370:10/64 64:ff9b::192.168.0.1 2600:14a0::/40",
		},
		{
			name:  "IPs only",
			input: "192.168.1.0 2001:0db8:85a3:0000:0000:8a2e:0370:7334 64:ff9b::192.168.0.1 172.16.14.10",
		},
		{
			name:  "CIDRs only",
			input: "192.168.1.0/16 2001:0db8:85a3:0000:0000:8a2e:0370:7334/48 172.16.14.10/24 2001:0db8:85a3::8a2e:0370:10/64 2600:14a0::/40",
		},
		{
			name:  "IPv6 only",
			input: "2001:0db8:85a3:0000:0000:8a2e:0370:7334 2001:0db8:85a3::8a2e:370:10/64 2001:db8::2:1 ::ffff:192.168.0.1 2600:14a0::/40",
		},
		{
			name:  "IPv4 only",
			input: "192.168.10.10 10.168.12.10/8 8.8.8.8 172.16.0.0/24",
		},
		{
			name:  "Single IP",
			input: "192.168.15.15",
		},
		{
			// as behavior as the previous (regexp) approach
			name:          "Leading and trailing spaces",
			input:         " 192.168.10.10  ",
			expectedEmpty: true,
		},
		{
			name:          "Only white spaces",
			input:         "   ",
			expectedEmpty: true,
		},
		{
			name:          "Empty",
			input:         "",
			expectedEmpty: true,
		},
		{
			name:          "Wrong IPv4",
			input:         "192.168.",
			expectedEmpty: true,
		},
		{
			name:          "Wrong IPv6",
			input:         "2001:0db8:",
			expectedEmpty: true,
		},
		{
			name:          "Wrong IPv4 CIDR",
			input:         "192.168.10.5/64",
			expectedEmpty: true,
		},
		{
			name:          "Wrong IPv6 CIDR",
			input:         "2600:14a0::/256",
			expectedEmpty: true,
		},
		{
			name:          "Wrong IP in a list",
			input:         "192.168.1.0 2001:0db8:85a3:0000:0000:8a2e:0370:7334 172.16.14.10/24 2001:0db8:85a3::8a2e:370:10/64 64:ff9b::192.168.0.1 10.",
			expectedEmpty: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseIPList(tc.input)
			if tc.expectedEmpty {
				if got != "" {
					t.Errorf("Expected empty got %q", got)
				}
				return
			}
			if got != tc.input {
				t.Errorf("Failure: expected %q, got %q", tc.input, got)
			}
		})
	}
}
