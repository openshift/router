package router

import (
	"crypto/tls"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	templateplugin "github.com/openshift/router/pkg/router/template"
)

func TestParseHeadersToBeSetOrDeleted(t *testing.T) {
	testCases := []struct {
		description        string
		inputValue         string
		expectedValue      []templateplugin.HTTPHeader
		expectErrorMessage string
	}{
		{
			description: "should percent decode the single header value",
			inputValue:  "Accept:text%2Fplain%2C+text%2Fhtml:Set",
			expectedValue: []templateplugin.HTTPHeader{
				{Name: "'Accept'", Value: "'text/plain, text/html'", Action: routev1.Set},
			},
			expectErrorMessage: "",
		},
		{
			description:        "should not percent decode the invalid double encoded single header value as it can't be split either in 2 or 3 parts",
			inputValue:         "Content-Location%3A%252Fmy-first-blog-post%3ASet",
			expectedValue:      nil,
			expectErrorMessage: "invalid HTTP header input specification: Content-Location%3A%252Fmy-first-blog-post%3ASet",
		},
		{
			description: "should percent decode the multiple response header values",
			inputValue:  "X-Frame-Options:DENY:Set,X-XSS-Protection:1%3Bmode%3Dblock:Set,x-forwarded-client-cert:%25%7B%2BQ%7D%5Bssl_c_der%2Cbase64%5D:Set",
			expectedValue: []templateplugin.HTTPHeader{
				{Name: "'X-Frame-Options'", Value: "'DENY'", Action: routev1.Set},
				{Name: "'X-XSS-Protection'", Value: "'1;mode=block'", Action: routev1.Set},
				{Name: "'x-forwarded-client-cert'", Value: "'%{+Q}[ssl_c_der,base64]'", Action: routev1.Set},
			},
			expectErrorMessage: "",
		},
		{

			description: "should percent decode the multiple request header values",
			inputValue:  "Accept:text%2Fplain%2C+text%2Fhtml:Set,Accept-Encoding:Delete",
			expectedValue: []templateplugin.HTTPHeader{
				{Name: "'Accept'", Value: "'text/plain, text/html'", Action: routev1.Set},
				{Name: "'Accept-Encoding'", Action: routev1.Delete},
			},
			expectErrorMessage: "",
		},
		{
			description: "should percent decode the multiple header values with simple non encoded strings",
			inputValue:  "X-Frame-Options:DENY:Set",
			expectedValue: []templateplugin.HTTPHeader{
				{Name: "'X-Frame-Options'", Value: "'DENY'", Action: routev1.Set},
			},
			expectErrorMessage: "",
		},
		{
			description:        "when Action is `Set` it should not percent decode the incorrectly encoded value part Eg: percent is not followed by two hexadecimal digits",
			inputValue:         "x-forwarded-client-cert:%25%7B%2BQ%7D%5Bssl_c_der%2Cbase64%5:Set",
			expectedValue:      nil,
			expectErrorMessage: "failed to decode percent encoding: %25%7B%2BQ%7D%5Bssl_c_der%2Cbase64%5",
		},
		{
			description:        "invalid incorrectly encoded Action when header input spec has 2 parts",
			inputValue:         "Accept-Encoding:Delete%2",
			expectedValue:      nil,
			expectErrorMessage: "failed to decode percent encoding: Delete%2",
		},
		{
			description:        "invalid incorrectly encoded Action when header input spec has 3 parts",
			inputValue:         "X-Frame-Options:DENY:Delete%2",
			expectedValue:      nil,
			expectErrorMessage: "failed to decode percent encoding: Delete%2",
		},
		{
			description:        "invalid Action when header input spec has 3 parts",
			inputValue:         "X-Frame-Options:DENY:Bad",
			expectedValue:      nil,
			expectErrorMessage: "invalid action: Bad",
		},
		{
			description:        "invalid Action when header input spec has 2 parts",
			inputValue:         "X-Frame-Options:Bad",
			expectedValue:      nil,
			expectErrorMessage: "invalid action: Bad",
		},
		{
			description:        "should not allow specifying headers with no action or value",
			inputValue:         "X-Frame-Options",
			expectErrorMessage: "invalid HTTP header input specification: X-Frame-Options",
		},
		{
			description:        "should not allow blank values for headers",
			inputValue:         "",
			expectedValue:      nil,
			expectErrorMessage: "encoded header string not present",
		},
		{
			description:        "should fail with incorrect header name when HTTP header input specification has 3 parts",
			inputValue:         "X-Frame-Options**!=:DENY:Set",
			expectedValue:      nil,
			expectErrorMessage: "invalid HTTP header name: X-Frame-Options**!=",
		},
		{
			description:        "should fail with incorrect header name when HTTP header input specification has 2 parts",
			inputValue:         "X-Frame-Options**!=:Delete",
			expectedValue:      nil,
			expectErrorMessage: "invalid HTTP header name: X-Frame-Options**!=",
		},
		{
			description:        "should fail when header spec consists of more than 3 parts than Name:Value:Action",
			inputValue:         "X-Frame-Options:DENY:Set:Invalid",
			expectedValue:      nil,
			expectErrorMessage: "invalid HTTP header input specification: X-Frame-Options:DENY:Set:Invalid",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			switch actualValue, actualErrorMessage := parseHeadersToBeSetOrDeleted(tc.inputValue); {
			case !cmp.Equal(actualValue, tc.expectedValue):
				t.Errorf(" expected %s, got %s", tc.expectedValue, actualValue)
			case tc.expectErrorMessage == "" && actualErrorMessage != nil && actualErrorMessage.Error() != "":
				t.Fatalf("unexpected error: %v", actualErrorMessage)
			case tc.expectErrorMessage != "" && (actualErrorMessage == nil || actualErrorMessage.Error() == ""):
				t.Fatalf("got nil, expected %v", tc.expectErrorMessage)
			case actualErrorMessage != nil && tc.expectErrorMessage != actualErrorMessage.Error():
				t.Fatalf("unexpected error: %v, expected: %v", actualErrorMessage, tc.expectErrorMessage)
			}
		})
	}
}

func TestTLSGroupsToCurveIDs(t *testing.T) {
	testCases := []struct {
		description       string
		groups            []configv1.TLSGroup
		expectedCurves    []tls.CurveID
		expectedUnsupport []string
	}{
		{
			description:       "empty input",
			groups:            []configv1.TLSGroup{},
			expectedCurves:    nil,
			expectedUnsupport: nil,
		},
		{
			description:       "valid standard curves",
			groups:            []configv1.TLSGroup{configv1.TLSGroupX25519, configv1.TLSGroupSecP256r1, configv1.TLSGroupSecP384r1},
			expectedCurves:    []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
			expectedUnsupport: nil,
		},
		{
			description:       "mixed valid and invalid curves",
			groups:            []configv1.TLSGroup{configv1.TLSGroupSecP521r1, "invalidCurve", configv1.TLSGroupX25519MLKEM768},
			expectedCurves:    []tls.CurveID{tls.CurveP521, tls.X25519MLKEM768},
			expectedUnsupport: []string{"invalidCurve"},
		},
		{
			description:       "all unsupported curves",
			groups:            []configv1.TLSGroup{"invalidGroupA", "invalidGroupB"},
			expectedCurves:    nil,
			expectedUnsupport: []string{"invalidGroupA", "invalidGroupB"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actualCurves, actualUnsupport := tlsGroupsToCurveIDs(tc.groups)
			if !cmp.Equal(actualCurves, tc.expectedCurves) {
				t.Errorf("expected curves %v, got %v", tc.expectedCurves, actualCurves)
			}
			if !cmp.Equal(actualUnsupport, tc.expectedUnsupport) {
				t.Errorf("expected unsupported %v, got %v", tc.expectedUnsupport, actualUnsupport)
			}
		})
	}
}

func TestParseCurvePreferences(t *testing.T) {
	testCases := []struct {
		description  string
		inputValue   string
		expected     []tls.CurveID
		expectErr    bool
		errSubstring string
	}{
		{
			description: "comma-separated valid curves",
			inputValue:  string(configv1.TLSGroupX25519) + "," + string(configv1.TLSGroupSecP256r1),
			expected:    []tls.CurveID{tls.X25519, tls.CurveP256},
			expectErr:   false,
		},
		{
			description: "colon-separated valid curves",
			inputValue:  string(configv1.TLSGroupSecP384r1) + ":" + string(configv1.TLSGroupSecP521r1),
			expected:    []tls.CurveID{tls.CurveP384, tls.CurveP521},
			expectErr:   false,
		},
		{
			description: "mixed delimiters with whitespace",
			inputValue:  "  " + string(configv1.TLSGroupX25519) + " , " + string(configv1.TLSGroupSecP256r1) + " : " + string(configv1.TLSGroupSecP384r1) + "  ",
			expected:    []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
			expectErr:   false,
		},
		{
			description:  "unsupported curve in list causes error",
			inputValue:   string(configv1.TLSGroupX25519) + ",InvalidCurve",
			expected:     nil,
			expectErr:    true,
			errSubstring: "are not supported: [InvalidCurve]",
		},
		{
			description:  "empty or whitespace only causes error",
			inputValue:   "  ,  :  ",
			expected:     nil,
			expectErr:    true,
			errSubstring: "no valid curves found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			actual, err := parseCurvePreferences(tc.inputValue)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.errSubstring)
				}
				if tc.errSubstring != "" && !strings.Contains(err.Error(), tc.errSubstring) {
					t.Errorf("expected error containing %q, got %q", tc.errSubstring, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !cmp.Equal(actual, tc.expected) {
					t.Errorf("expected %v, got %v", tc.expected, actual)
				}
			}
		})
	}
}

// TestValidate verifies the checks performed by TemplateRouterOptions.Validate.
// Each test case starts from a set of options that pass validation and mutates
// only the fields relevant to the check being exercised.  Checks that need
// heavy setup or that have a large number of cases of their own should get a
// dedicated test function instead.
func TestValidate(t *testing.T) {
	const reloadScript = "/var/lib/haproxy/reload-haproxy"

	// validOptions returns options that pass Validate.
	validOptions := func() *TemplateRouterOptions {
		return &TemplateRouterOptions{
			TemplateRouter: TemplateRouter{
				TemplateFile: "/var/lib/haproxy/conf/haproxy-config.template",
				ReloadScript: reloadScript,
			},
		}
	}

	testCases := []struct {
		description string
		// mutate modifies the valid options to exercise a specific check.
		mutate func(t *testing.T, o *TemplateRouterOptions)
		// expectedError is a substring of the expected error message, or
		// empty if Validate is expected to succeed.
		expectedError string
	}{
		{
			description: "valid options",
			mutate:      func(t *testing.T, o *TemplateRouterOptions) {},
		},
		{
			description: "supported metrics type",
			mutate:      func(t *testing.T, o *TemplateRouterOptions) { o.MetricsType = "haproxy" },
		},
		{
			description:   "unsupported metrics type",
			mutate:        func(t *testing.T, o *TemplateRouterOptions) { o.MetricsType = "foo" },
			expectedError: "supported metrics types are: haproxy",
		},
		{
			description: "update status with router name",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.UpdateStatus = true
				o.RouterName = "default"
			},
		},
		{
			description:   "update status without router name",
			mutate:        func(t *testing.T, o *TemplateRouterOptions) { o.UpdateStatus = true },
			expectedError: "router must have a name to identify itself in route status",
		},
		{
			description:   "missing template file",
			mutate:        func(t *testing.T, o *TemplateRouterOptions) { o.TemplateFile = "" },
			expectedError: "template file must be specified",
		},
		{
			description: "existing default destination CA",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				path := filepath.Join(t.TempDir(), "ca.crt")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("failed to create CA file: %v", err)
				}
				o.DefaultDestinationCAPath = path
			},
		},
		{
			description: "missing default destination CA",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.DefaultDestinationCAPath = filepath.Join(t.TempDir(), "missing.crt")
			},
			expectedError: "unable to load default destination CA certificate",
		},
		{
			description: "neither reload script nor admin socket",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.ReloadScript = ""
				o.HAProxyAdminUnixSocket = ""
			},
			expectedError: "either the reload script or the HAProxy admin unix socket must be specified",
		},
		{
			description: "reload script only",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.ReloadScript = reloadScript
				o.HAProxyAdminUnixSocket = ""
			},
		},
		{
			description: "admin socket only",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.ReloadScript = ""
				o.HAProxyAdminUnixSocket = newUnixSocket(t)
			},
		},
		{
			description: "missing admin socket",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.HAProxyAdminUnixSocket = filepath.Join(t.TempDir(), "missing.sock")
			},
			expectedError: "unable to find the HAProxy admin unix socket",
		},
		{
			description: "admin socket is not a unix socket",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				path := filepath.Join(t.TempDir(), "admin.sock")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("failed to create file: %v", err)
				}
				o.HAProxyAdminUnixSocket = path
			},
			expectedError: "is not a unix socket",
		},
		{
			description: "both reload script and admin socket",
			mutate: func(t *testing.T, o *TemplateRouterOptions) {
				o.ReloadScript = reloadScript
				o.HAProxyAdminUnixSocket = newUnixSocket(t)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			o := validOptions()
			tc.mutate(t, o)
			err := o.Validate()
			switch {
			case tc.expectedError == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.expectedError != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tc.expectedError)
			case err != nil && !strings.Contains(err.Error(), tc.expectedError):
				t.Fatalf("expected error containing %q, got %q", tc.expectedError, err.Error())
			}
		})
	}
}

// newUnixSocket creates a listening unix socket in a temporary directory and
// returns its path. The socket is closed when the test finishes.
func newUnixSocket(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("failed to create unix socket: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return path
}
