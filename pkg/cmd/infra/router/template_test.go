package router

import (
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/utils/ptr"

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

// TestFIPSEnabled verifies that fipsEnabled correctly interprets the content
// of the kernel's fips_enabled file.
func TestFIPSEnabled(t *testing.T) {
	testCases := []struct {
		name        string
		content     *string
		expected    bool
		expectError bool
	}{
		{name: "FIPS enabled", content: ptr.To("1\n"), expected: true},
		{name: "FIPS enabled without newline", content: ptr.To("1"), expected: true},
		{name: "FIPS disabled", content: ptr.To("0\n"), expected: false},
		{name: "missing file", content: nil, expected: false},
		{name: "empty file", content: ptr.To(""), expectError: true},
		{name: "unexpected number", content: ptr.To("2\n"), expectError: true},
		{name: "non-numeric content", content: ptr.To("yes\n"), expectError: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fips_enabled")
			if tc.content != nil {
				if err := os.WriteFile(path, []byte(*tc.content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			defer func(orig string) { fipsEnabledPath = orig }(fipsEnabledPath)
			fipsEnabledPath = path

			actual, err := fipsEnabled()
			if tc.expectError {
				if err == nil {
					t.Fatalf("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, actual)
			}
		})
	}

	t.Run("unreadable path", func(t *testing.T) {
		// Reading a directory fails with an error other than "not exist".
		defer func(orig string) { fipsEnabledPath = orig }(fipsEnabledPath)
		fipsEnabledPath = t.TempDir()

		if _, err := fipsEnabled(); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}

// Test_resolveQAT verifies that QAT is enabled only if it is requested and
// FIPS mode is known to be disabled.
func Test_resolveQAT(t *testing.T) {
	testCases := []struct {
		name      string
		requested bool
		fips      bool
		fipsErr   error
		expected  bool
	}{
		{
			name:      "not requested",
			requested: false,
			expected:  false,
		},
		{
			name:      "not requested, FIPS enabled",
			requested: false,
			fips:      true,
			expected:  false,
		},
		{
			name:      "requested, FIPS disabled",
			requested: true,
			fips:      false,
			expected:  true,
		},
		{
			name:      "requested, FIPS enabled",
			requested: true,
			fips:      true,
			expected:  false,
		},
		{
			name:      "requested, FIPS unknown",
			requested: true,
			fipsErr:   errors.New("permission denied"),
			expected:  false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isFIPSEnabled := func() (bool, error) { return tc.fips, tc.fipsErr }
			if actual := resolveQAT(tc.requested, isFIPSEnabled); actual != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, actual)
			}
		})
	}
}
