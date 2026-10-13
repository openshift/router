package router

import (
	"testing"

	"github.com/google/go-cmp/cmp"
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
