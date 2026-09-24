package templaterouter

import (
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/openshift/router/pkg/router/template/util/rewritetarget"
)

const liveFuzzSeed int64 = 27741

var semanticFuzzSeeds = []int64{27741, 6174, 8675309, 424242}

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
	t.Logf("differential config fuzz seed=%d cases=%d oldRejected=%d oldAccepted=%d newRejected=%d newAccepted=%d", liveFuzzSeed, cases, oldRejected, cases-oldRejected, newRejected, cases-newRejected)
}

func TestRewritePathSemanticFuzzingWithLiveHAProxy(t *testing.T) {
	requireHAProxy(t)
	backendPort := getFreePort(t)
	defer startBackendServer(t, backendPort)()

	const cases = 100
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._~-!$&'()*+,;=:@"
	for _, seed := range semanticFuzzSeeds {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			oldRejected := 0
			oldCorrect := 0
			oldDifferent := 0
			newRejected := 0
			newDifferent := 0

			for i := 0; i < cases; i++ {
				length := 1 + rng.Intn(24)
				path := make([]byte, length+1)
				path[0] = '/'
				for j := 1; j < len(path); j++ {
					path[j] = charset[rng.Intn(len(charset))]
				}
				specPath := string(path)
				requestPath := specPath + "/tail"
				want := "/rewritten/tail"

				for _, configType := range []string{"OLD", "NEW"} {
					listenPort := getFreePort(t)
					config := generateHAProxyConfig(testConfig{specPath, "/rewritten", configType, listenPort, backendPort})
					if err := validateHAProxyConfig(config); err != nil {
						if configType == "OLD" {
							oldRejected++
							continue
						}
						newRejected++
						t.Fatalf("seed=%d case=%d path=%q type=NEW outcome=CONFIG_REJECTED error=%v", seed, i, specPath, err)
					}
					cleanup := runHAProxy(t, config)
					got, err := requestBackendPathWithError(listenPort, requestPath)
					cleanup()
					if err != nil {
						if configType == "NEW" {
							t.Fatalf("seed=%d case=%d path=%q type=NEW request error=%v", seed, i, specPath, err)
						}
						oldDifferent++
						continue
					}
					if got == want {
						if configType == "OLD" {
							oldCorrect++
						}
						continue
					}
					if configType == "OLD" {
						oldDifferent++
						continue
					}
					newDifferent++
					t.Fatalf("seed=%d case=%d path=%q type=NEW outcome=SEMANTIC_DIFFERENCE backend=%q expected=%q", seed, i, specPath, got, want)
				}
			}
			t.Logf("semantic differential fuzz seed=%d cases=%d oldRejected=%d oldCorrect=%d oldDifferent=%d newRejected=%d newDifferent=%d", seed, cases, oldRejected, oldCorrect, oldDifferent, newRejected, newDifferent)
		})
	}
}

func TestRewritePathOldVsNewWithLiveHAProxy(t *testing.T) {
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requestPath := tc.specPath + "/sub"
			want := tc.rewriteTarget + "/sub"
			for _, configType := range []string{"OLD", "NEW"} {
				listenPort := getFreePort(t)
				config := generateHAProxyConfig(testConfig{tc.specPath, tc.rewriteTarget, configType, listenPort, backendPort})
				if err := validateHAProxyConfig(config); err != nil {
					t.Logf("path=%q type=%s config=REJECTED error=%v", tc.specPath, configType, err)
					continue
				}
				cleanup := runHAProxy(t, config)
				got, err := requestBackendPathWithError(listenPort, requestPath)
				cleanup()
				if err != nil {
					t.Logf("path=%q type=%s request=ERROR error=%v", tc.specPath, configType, err)
					continue
				}
				outcome := "CORRECT"
				if got != want {
					outcome = "DIFFERENT"
				}
				t.Logf("path=%q type=%s request=%q backend=%q expected=%q outcome=%s", tc.specPath, configType, requestPath, got, want, outcome)
			}
		})
	}
}
