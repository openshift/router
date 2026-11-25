package templaterouter

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/openshift/router/pkg/router/template/util/rewritetarget"
)

const liveFuzzSeed int64 = 27741

var haproxyBinary = os.Getenv("HAPROXY_BINARY")

func init() {
	if haproxyBinary == "" {
		haproxyBinary = "/usr/sbin/haproxy"
	}
}

func requireHAProxy(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(haproxyBinary); err != nil {
		t.Skipf("HAProxy binary %q is unavailable: %v", haproxyBinary, err)
	}
}

func getFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type testConfig struct {
	specPath      string
	rewriteTarget string
	configType    string
	listenPort    int
	backendPort   int
}

func generateHAProxyConfig(tc testConfig) string {
	path := tc.specPath
	if tc.configType == "NEW" {
		path = rewritetarget.SanitizeRewritePathInput(path)
	} else {
		path = escapeSingleQuotesOnly(path)
	}

	matchSuffix := `(.*)$`
	if tc.rewriteTarget == "/" {
		matchSuffix = `/?(.*)$`
	}
	matchRegex := fmt.Sprintf(`^%s%s`, path, matchSuffix)

	return fmt.Sprintf(`
global

defaults
  mode http
  timeout connect 5s
  timeout client 5s
  timeout server 5s

frontend test_front
  bind 127.0.0.1:%d
  default_backend test_backend
  http-request replace-path '%s' '%s'

backend test_backend
  server backend 127.0.0.1:%d
`, tc.listenPort, matchRegex, rewritetarget.SanitizeInput(tc.rewriteTarget), tc.backendPort)
}

func escapeSingleQuotesOnly(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

func startBackendServer(t *testing.T, port int) func() {
	t.Helper()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("start backend: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend-Path", r.URL.Path)
		_, _ = fmt.Fprint(w, r.URL.Path)
	})}
	go server.Serve(listener)
	return func() { _ = server.Close() }
}

func validateHAProxyConfig(config string) error {
	tmpfile, err := os.CreateTemp("", "haproxy-*.cfg")
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer os.Remove(tmpfile.Name())
	if _, err := tmpfile.WriteString(config); err != nil {
		tmpfile.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmpfile.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	output, err := exec.Command(haproxyBinary, "-c", "-f", tmpfile.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("HAProxy config validation failed: %s", output)
	}
	return nil
}

func runHAProxy(t *testing.T, config string) func() {
	t.Helper()
	tmpfile, err := os.CreateTemp("", "haproxy-*.cfg")
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if _, err := tmpfile.WriteString(config); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		t.Fatalf("write config: %v", err)
	}
	tmpfile.Close()
	cmd := exec.Command(haproxyBinary, "-f", tmpfile.Name())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		os.Remove(tmpfile.Name())
		t.Fatalf("start HAProxy: %v", err)
	}
	return func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.Remove(tmpfile.Name())
	}
}

func requestBackendPath(t *testing.T, port int, path string) string {
	t.Helper()
	backendPath, err := requestBackendPathWithError(port, path)
	if err != nil {
		t.Fatalf("request %q: %v", path, err)
	}
	return backendPath
}

func requestBackendPathWithError(port int, path string) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(url)
		if err == nil {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return "", fmt.Errorf("status %d", response.StatusCode)
			}
			return response.Header.Get("X-Backend-Path"), nil
		}
		if time.Now().After(deadline) {
			return "", err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestRewritePathSanitizationWithLiveHAProxy verifies that for each representative
// spec.path the NEW (sanitized) config produces a loadable HAProxy configuration and
// rewrites the literal path to the expected backend path, preserving the suffix.
func TestRewritePathSanitizationWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)
	backendPort := getFreePort(t)
	defer startBackendServer(t, backendPort)()

	testCases := []struct {
		name          string
		specPath      string
		rewriteTarget string
	}{
		{"plain", "/bar", "/foo"},
		{"dot", "/api/v1.0", "/api/v2"},
		{"plus", "/bar+", "/foo"},
		{"star", "/bar*", "/foo"},
		{"dollar", "/bar$", "/foo"},
		{"parentheses", "/bar()", "/foo"},
		{"brackets", "/bar[a-z]", "/foo"},
		{"combined", "/api/v1.0+beta", "/api/v2"},
		{"c-plus-plus", "/c++", "/cplusplus"},
		{"root-target", "/bar+", "/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listenPort := getFreePort(t)
			config := generateHAProxyConfig(testConfig{tc.specPath, tc.rewriteTarget, "NEW", listenPort, backendPort})
			if err := validateHAProxyConfig(config); err != nil {
				t.Fatal(err)
			}
			cleanup := runHAProxy(t, config)
			defer cleanup()

			got := requestBackendPath(t, listenPort, tc.specPath+"/sub")
			want := tc.rewriteTarget + "/sub"
			if tc.rewriteTarget == "/" {
				want = "/sub"
			}
			if got != want {
				t.Fatalf("literal path rewrite: request %q, expected backend %q, got %q", tc.specPath+"/sub", want, got)
			}
		})
	}
}

// TestLiteralPlusDoesNotOvermatchWithLiveHAProxy verifies that a route with
// spec.path "/bar+" does not rewrite requests to "/barr/...", which the OLD
// unescaped regex would have matched via the + quantifier.
func TestLiteralPlusDoesNotOvermatchWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)
	backendPort := getFreePort(t)
	defer startBackendServer(t, backendPort)()
	listenPort := getFreePort(t)
	config := generateHAProxyConfig(testConfig{"/bar+", "/foo", "NEW", listenPort, backendPort})
	if err := validateHAProxyConfig(config); err != nil {
		t.Fatal(err)
	}
	cleanup := runHAProxy(t, config)
	defer cleanup()
	if got := requestBackendPath(t, listenPort, "/barr/sub"); got != "/barr/sub" {
		t.Fatalf("literal plus overmatched: expected backend path %q, got %q", "/barr/sub", got)
	}
}

// TestRewritePathConfigFuzzingWithLiveHAProxy generates 1,000 deterministic
// adversarial paths using metacharacter-rich characters and verifies that the
// NEW sanitized config is accepted by HAProxy for every path that the OLD
// unsanitized config also accepted, and for all paths the OLD config rejected.
// A NEW rejection is a test failure.
func TestRewritePathConfigFuzzingWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)
	rng := rand.New(rand.NewSource(liveFuzzSeed))
	const cases = 1000
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789/._-+*()[]{}$|"
	oldRejected := 0
	newRejected := 0
	for i := 0; i < cases; i++ {
		length := 1 + rng.Intn(40)
		path := make([]byte, length+1)
		path[0] = '/'
		for j := 1; j < len(path); j++ {
			path[j] = charset[rng.Intn(len(charset))]
		}
		specPath := string(path)
		oldErr := validateHAProxyConfig(generateHAProxyConfig(testConfig{specPath, "/rewritten", "OLD", 10000 + i, 18000 + i}))
		newErr := validateHAProxyConfig(generateHAProxyConfig(testConfig{specPath, "/rewritten", "NEW", 12000 + i, 20000 + i}))
		if oldErr != nil {
			oldRejected++
		}
		if newErr != nil {
			newRejected++
			t.Fatalf("NEW rejected seed=%d case=%d path=%q oldRejected=%t: %v", liveFuzzSeed, i, specPath, oldErr != nil, newErr)
		}
	}
	t.Logf("differential config fuzz seed=%d cases=%d oldRejected=%d oldAccepted=%d newRejected=%d newAccepted=%d",
		liveFuzzSeed, cases, oldRejected, cases-oldRejected, newRejected, cases-newRejected)
}

func compatibilityRequests(specPath string) []string {
	requests := map[string]struct{}{
		specPath + "/tail": {},
	}

	add := func(path string) {
		if path != specPath {
			requests[path+"/tail"] = struct{}{}
		}
	}

	add(strings.Replace(specPath, ".", "x", 1))
	add(strings.Replace(specPath, "+", "", 1))
	add(strings.Replace(specPath, "+", "r", 1))
	add(strings.Replace(specPath, "+", "rr", 1))
	add(strings.Replace(specPath, "*", "", 1))
	add(strings.Replace(specPath, "*", "x", 1))
	add(strings.Replace(specPath, "?", "", 1))
	add(strings.Replace(specPath, "?", "x", 1))
	add(strings.Replace(specPath, "[a-z]", "a", 1))
	add(strings.Replace(specPath, "[a-z]", "z", 1))
	add(strings.Replace(specPath, "|", "", 1))
	add(strings.Replace(specPath, "|", "alt", 1))
	add(strings.Replace(specPath, "()", "", 1))
	add(strings.Replace(specPath, "(", "", 1))
	add(strings.Replace(specPath, ")", "", 1))
	add(strings.Replace(specPath, "$", "", 1))
	add(strings.Replace(specPath, "^", "", 1))
	add(strings.Replace(specPath, "{2}", "xx", 1))

	result := make([]string, 0, len(requests))
	for request := range requests {
		result = append(result, request)
	}
	sort.Strings(result)
	return result
}

func runCompatibilityConfig(t *testing.T, specPath string, configType string, backendPort int, requests []string) map[string]string {
	t.Helper()
	listenPort := getFreePort(t)
	config := generateHAProxyConfig(testConfig{specPath, "/rewritten", configType, listenPort, backendPort})
	if err := validateHAProxyConfig(config); err != nil {
		return nil
	}
	cleanup := runHAProxy(t, config)
	defer cleanup()

	results := make(map[string]string, len(requests))
	for _, request := range requests {
		backendPath, err := requestBackendPathWithError(listenPort, request)
		if err != nil {
			results[request] = "ERROR: " + err.Error()
			continue
		}
		results[request] = backendPath
	}
	return results
}

// TestRewritePathRouteMapDispatchWithLiveHAProxy validates that the route-map
// backend selection (which already uses regexp.QuoteMeta via GenerateRouteRegexp)
// prevents OLD-only rewrite overmatches from ever reaching the metacharacter
// backend end-to-end.  The test mirrors what the HAProxy router template emits:
//
//   - A metacharacter route for /bar+ with rewrite-target /rewritten, where the
//     route-map key is the correctly-escaped regex ^host(:[0-9]+)?/bar\+(/.*)?$
//   - A catch-all route for / on the same host
//   - A frontend that selects backends via map_reg (as the router template does)
//
// Assertions:
//  1. /bar+/tail  → metachar backend, rewritten to /rewritten/tail  (literal path works)
//  2. /barr/tail  → catchall backend, NOT rewritten                  (OLD overmatch unreachable)
//  3. /bar/tail   → catchall backend, NOT rewritten                  (OLD overmatch unreachable)
func TestRewritePathRouteMapDispatchWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)

	metacharBackendPort := getFreePort(t)
	catchallBackendPort := getFreePort(t)
	defer startBackendServer(t, metacharBackendPort)()
	defer startBackendServer(t, catchallBackendPort)()

	mapFile, err := os.CreateTemp("", "haproxy-dispatch-*.map")
	if err != nil {
		t.Fatalf("create map file: %v", err)
	}
	defer os.Remove(mapFile.Name())

	// Route-map entries mirror what GenerateRouteRegexp produces for:
	//   host=dispatch.example.com, path=/bar+  → be_metachar  (escaped: /bar\+)
	//   host=dispatch.example.com, path=/       → be_catchall  (catch-all)
	// Longer prefix is listed first so map_reg longest-match selects be_metachar
	// for requests that begin with /bar+ and be_catchall for everything else.
	mapEntries := "^dispatch\\.example\\.com\\.?(:[0-9]+)?/bar\\+(/.*)?$ be_metachar\n" +
		"^dispatch\\.example\\.com\\.?(:[0-9]+)?(/.*)?$ be_catchall\n"
	if _, err := mapFile.WriteString(mapEntries); err != nil {
		mapFile.Close()
		t.Fatalf("write map file: %v", err)
	}
	mapFile.Close()

	listenPort := getFreePort(t)

	sanitizedPath := rewritetarget.SanitizeRewritePathInput("/bar+")
	replacePathRegex := fmt.Sprintf(`^%s(.*)$`, sanitizedPath)
	// SanitizeInput already appends \1 for the capture group reference.
	replacePathTarget := rewritetarget.SanitizeInput("/rewritten")

	config := fmt.Sprintf(`
global

defaults
  mode http
  timeout connect 5s
  timeout client 5s
  timeout server 5s

frontend dispatch_front
  bind 127.0.0.1:%d
  use_backend %%[base,map_reg(%s)]
  default_backend be_catchall

backend be_metachar
  http-request replace-path '%s' '%s'
  server backend 127.0.0.1:%d

backend be_catchall
  server backend 127.0.0.1:%d
`, listenPort, mapFile.Name(), replacePathRegex, replacePathTarget, metacharBackendPort, catchallBackendPort)

	if err := validateHAProxyConfig(config); err != nil {
		t.Fatalf("dispatch config rejected by HAProxy: %v", err)
	}
	cleanup := runHAProxy(t, config)
	defer cleanup()

	makeRequest := func(path string) (int, string) {
		t.Helper()
		client := &http.Client{Timeout: time.Second}
		url := fmt.Sprintf("http://127.0.0.1:%d%s", listenPort, path)
		deadline := time.Now().Add(5 * time.Second)
		for {
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Host = "dispatch.example.com"
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				return resp.StatusCode, resp.Header.Get("X-Backend-Path")
			}
			if time.Now().After(deadline) {
				t.Fatalf("request to %s timed out: %v", url, err)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}

	// 1. Literal /bar+/tail must reach be_metachar and be rewritten.
	if status, got := makeRequest("/bar+/tail"); status != http.StatusOK || got != "/rewritten/tail" {
		t.Errorf("literal /bar+/tail: status=%d backendPath=%q, want status=200 backendPath=/rewritten/tail", status, got)
	}

	// 2. /barr/tail is an OLD-only overmatch candidate: must reach be_catchall, not be rewritten.
	if status, got := makeRequest("/barr/tail"); status != http.StatusOK || got != "/barr/tail" {
		t.Errorf("overmatch /barr/tail: status=%d backendPath=%q, want status=200 backendPath=/barr/tail (catchall, unrewritten)", status, got)
	}

	// 3. /bar/tail is an OLD-only overmatch candidate: must reach be_catchall, not be rewritten.
	if status, got := makeRequest("/bar/tail"); status != http.StatusOK || got != "/bar/tail" {
		t.Errorf("overmatch /bar/tail: status=%d backendPath=%q, want status=200 backendPath=/bar/tail (catchall, unrewritten)", status, got)
	}
}

// TestRewritePathCompatibilityDifferentialWithLiveHAProxy runs OLD and NEW
// rewrite configs side-by-side for a representative set of metacharacter paths
// and asserts that:
//   - NEW config is always accepted by HAProxy (never rejects).
//   - NEW never rewrites a request that does not begin with the literal spec.path
//     (i.e. NEW must not overmatch).
//   - NEW always rewrites the exact literal spec.path correctly.
//
// OLD-only overmatches are logged but do not fail the test — they represent
// the known compatibility difference between OLD accidental-regex and NEW
// literal-path semantics. This matrix operates at the isolated rewrite-rule
// level; full-router dispatch behaviour requires a separate end-to-end test.
func TestRewritePathCompatibilityDifferentialWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)
	backendPort := getFreePort(t)
	defer startBackendServer(t, backendPort)()

	testCases := []string{
		"/bar",
		"/api/v1.0",
		"/bar+",
		"/bar*",
		"/bar?",
		"/bar$",
		"/bar^",
		"/bar()",
		"/bar[a-z]",
		"/bar{2}",
		"/foo|bar",
		"/api/v1.0+beta",
		"/c++",
	}

	totalRequests := 0
	compatibilityDifferences := 0
	oldRejected := 0

	for _, specPath := range testCases {
		t.Run(strings.ReplaceAll(specPath, "/", "_"), func(t *testing.T) {
			requests := compatibilityRequests(specPath)
			totalRequests += len(requests)

			newResults := runCompatibilityConfig(t, specPath, "NEW", backendPort, requests)
			if newResults == nil {
				t.Fatalf("NEW config rejected for %q — sanitizer must produce a valid HAProxy config for every spec.path", specPath)
			}

			oldResults := runCompatibilityConfig(t, specPath, "OLD", backendPort, requests)
			if oldResults == nil {
				oldRejected++
				t.Logf("path=%q oldConfig=REJECTED newConfig=ACCEPTED requests=%d", specPath, len(requests))
				return
			}

			literalRequest := specPath + "/tail"
			wantLiteral := "/rewritten/tail"

			// wireDeliverable is true when specPath contains only characters that
			// a standard HTTP client can send literally as a raw request-path.
			// Characters such as ? ^ | { } are not RFC 3986 pchar and require
			// percent-encoding; for those paths we skip the end-to-end assertions
			// because the isolated HAProxy test cannot represent real wire behaviour.
			wireDeliverable := !strings.ContainsAny(specPath, "?^|{}")

			if wireDeliverable {
				// NEW must correctly rewrite the exact literal path.
				if got := newResults[literalRequest]; got != wantLiteral {
					t.Errorf("NEW did not correctly rewrite literal path: path=%q request=%q got=%q want=%q",
						specPath, literalRequest, got, wantLiteral)
				}
			}

			for _, request := range requests {
				oldResult := oldResults[request]
				newResult := newResults[request]

				if wireDeliverable {
					// NEW must never rewrite a request that is not the literal spec.path.
					if request != literalRequest && newResult == wantLiteral {
						t.Errorf("NEW overmatched non-literal request: path=%q request=%q newBackend=%q",
							specPath, request, newResult)
					}
				}

				if oldResult != newResult {
					compatibilityDifferences++
					t.Logf("COMPATIBILITY_DIFFERENCE path=%q request=%q oldBackend=%q newBackend=%q",
						specPath, request, oldResult, newResult)
				}
			}
		})
	}
	t.Logf("compatibility differential paths=%d candidateRequests=%d oldRejected=%d oldNewDifferences=%d",
		len(testCases), totalRequests, oldRejected, compatibilityDifferences)
}
