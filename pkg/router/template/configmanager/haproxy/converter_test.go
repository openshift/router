package haproxy

import (
	"bytes"
	"fmt"
	"testing"
)

// TestNewConverter tests a new converter.
func TestNewConverter(t *testing.T) {
	testCases := []struct {
		name    string
		headers string
		fn      ByteConverterFunc
	}{
		{
			name:    "empty headers",
			headers: "",
			fn:      noopConverter,
		},
		{
			name:    "nil converter",
			headers: "#a",
		},
		{
			name:    "noop converter",
			headers: "#no o p",
			fn:      noopConverter,
		},
		{
			name:    "removing leading hash converter",
			headers: "#d e l",
			fn:      removeLeadingHashConverter,
		},
		{
			name:    "comment first line converter",
			headers: "#comment",
			fn:      commentFirstLineConverter,
		},
		{
			name:    "remove first line converter",
			headers: "#rm - r f",
			fn:      removeFirstLineConverter,
		},
		{
			name:    "error converter",
			headers: "#raise throw error",
			fn:      errorConverter,
		},
	}

	for _, tc := range testCases {
		entries := []*infoEntry{}
		if c := NewCSVConverter(tc.headers, entries, tc.fn); c == nil {
			t.Errorf("TestNewConverter test case %s failed.  Unexpected error", tc.name)
		}
	}
}

// TestShowInfoCommandConverter tests show info command output with a converter.
func TestShowInfoCommandConverter(t *testing.T) {
	infoCommandOutput := `Name: converter-test
Version: 0.0.1
Nbproc: 1
Process_num: 1
Pid: 42
`

	testCases := []struct {
		name            string
		commandOutput   string
		header          string
		converter       ByteConverterFunc
		failureExpected bool
	}{
		{
			name:            "info parser",
			commandOutput:   infoCommandOutput,
			header:          "name value",
			converter:       nil,
			failureExpected: false,
		},
		{
			name:            "info parser with noop converter",
			commandOutput:   infoCommandOutput,
			header:          "name value",
			converter:       noopConverter,
			failureExpected: false,
		},
		{
			name:            "info parser with comment header",
			commandOutput:   infoCommandOutput,
			header:          "#name value",
			converter:       noopConverter,
			failureExpected: false,
		},
		{
			name:            "output with header",
			commandOutput:   "#name value\n" + infoCommandOutput,
			header:          "",
			converter:       removeLeadingHashConverter,
			failureExpected: false,
		},
		{
			name:            "output without header",
			commandOutput:   "name value\n" + infoCommandOutput,
			header:          "",
			converter:       commentFirstLineConverter,
			failureExpected: false,
		},
		{
			name:            "output with error converter",
			commandOutput:   infoCommandOutput,
			header:          "#name value",
			converter:       errorConverter,
			failureExpected: true,
		},
		{
			name:            "output with bad header",
			commandOutput:   infoCommandOutput,
			header:          "# name value extra1 extra2",
			converter:       nil,
			failureExpected: true,
		},
		{
			name:            "output with bad header 2",
			commandOutput:   infoCommandOutput,
			header:          "# name value extra1 extra2",
			converter:       removeLeadingHashConverter,
			failureExpected: true,
		},
		{
			name:            "output with empty header",
			commandOutput:   "name value\n" + infoCommandOutput,
			header:          "",
			converter:       commentFirstLineConverter,
			failureExpected: false,
		},
		{
			name:            "bad command output with header",
			commandOutput:   "command error 404 - check params",
			header:          "field1 field2 field3",
			converter:       nil,
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		entries := []*infoEntry{}
		c := NewCSVConverter(tc.header, &entries, tc.converter)
		response, err := c.Convert([]byte(tc.commandOutput))
		if tc.failureExpected && err == nil {
			t.Errorf("TestShowInfoCommandConverter test case %s expected a failure but got none, response=%s",
				tc.name, string(response))
		}
		if !tc.failureExpected && err != nil {
			t.Errorf("TestShowInfoCommandConverter test case %s expected no failure but got one: %v", tc.name, err)
		}
	}
}

// TestShowMapCommandConverter tests show map command output with a converter.
func TestShowMapCommandConverter(t *testing.T) {
	listMapOutput := `# id (file) description
1 (/var/lib/haproxy/conf/os_route_http_redirect.map) pattern loaded from file '/var/lib/haproxy/conf/os_route_http_redirect.map' used by map at file '/var/lib/haproxy/conf/haproxy.config' line 68
5 (/var/lib/haproxy/conf/os_sni_passthrough.map) pattern loaded from file '/var/lib/haproxy/conf/os_sni_passthrough.map' used by map at file '/var/lib/haproxy/conf/haproxy.config' line 87
-1 (/var/lib/haproxy/conf/os_http_be.map) pattern loaded from file '/var/lib/haproxy/conf/os_http_be.map' used by map at file '/var/lib/haproxy/conf/haproxy.config' line 71
`

	testCases := []struct {
		name            string
		commandOutput   string
		header          string
		converter       ByteConverterFunc
		failureExpected bool
	}{
		{
			name:            "show map",
			commandOutput:   listMapOutput,
			header:          "id (file) description",
			converter:       fixupMapListOutput,
			failureExpected: false,
		},
		{
			name:            "show map with no converter",
			commandOutput:   listMapOutput,
			header:          "id (file) description",
			converter:       nil,
			failureExpected: true,
		},
		{
			name:            "show map without map fixup",
			commandOutput:   listMapOutput,
			header:          "id (file) description",
			converter:       removeFirstLineConverter,
			failureExpected: true,
		},
		{
			name:            "show map with error converter",
			commandOutput:   listMapOutput,
			header:          "id (file) description",
			converter:       errorConverter,
			failureExpected: true,
		},
		{
			name:            "show map with error converter 2",
			commandOutput:   "",
			header:          "id (file) description",
			converter:       errorConverter,
			failureExpected: true,
		},
		{
			name:            "show map bad output",
			commandOutput:   "error fetching list of maps: connection failed",
			header:          "id (file) description",
			converter:       fixupMapListOutput,
			failureExpected: true,
		},
	}

	for _, tc := range testCases {
		entries := []*mapListEntry{}
		c := NewCSVConverter(tc.header, &entries, tc.converter)
		response, err := c.Convert([]byte(tc.commandOutput))
		if tc.failureExpected && err == nil {
			t.Errorf("TestShowMapCommandConverter test case %s expected a failure but got none, response=%s",
				tc.name, string(response))
		}
		if !tc.failureExpected && err != nil {
			t.Errorf("TestShowMapCommandConverter test case %s expected no failure but got one: %v", tc.name, err)
		}
	}
}

func noopConverter(data []byte) ([]byte, error) {
	return data, nil
}

func removeLeadingHashConverter(data []byte) ([]byte, error) {
	prefix := []byte("#")
	idx := 0
	if len(data) > 0 && !bytes.HasPrefix(data, prefix) {
		idx = 1
	}

	return data[idx:], nil
}

func commentFirstLineConverter(data []byte) ([]byte, error) {
	return bytes.Join([][]byte{[]byte("#"), data}, []byte("")), nil
}

func removeFirstLineConverter(data []byte) ([]byte, error) {
	if len(data) > 0 {
		idx := bytes.Index(data, []byte("\n"))
		if idx > -1 {
			if idx+1 < len(data) {
				return data[idx+1:], nil
			}
		}
	}
	return []byte(""), nil
}

func errorConverter(data []byte) ([]byte, error) {
	return data, fmt.Errorf("converter test error")
}
