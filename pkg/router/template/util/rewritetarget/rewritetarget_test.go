package rewritetarget_test

import (
	"github.com/openshift/router/pkg/router/template/util/rewritetarget"
	"testing"
)

func Test_SanitizeInput(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		output string
	}{
		{
			name:   "single percent should be doubled",
			input:  `%foo`,
			output: `%%foo\1`,
		},
		{
			name:   "double percent should be not changed",
			input:  `%%foo`,
			output: `%%foo\1`,
		},
		{
			name:   "triple percent should be doubled entirely",
			input:  `%%%foo`,
			output: `%%%%%%foo\1`,
		},
		{
			name:   "quad percent should be not changed",
			input:  `%%%%foo`,
			output: `%%%%foo\1`,
		},
		{
			name:   "single quotes should be removed, unless escaped",
			input:  `'foo'foo\'`,
			output: `foofoo'\''\1`,
		},
		{
			name:   "single backslash should be dropped",
			input:  `\foo\`,
			output: `foo\1`,
		},
		{
			name:   "double backslash should be single",
			input:  `\\foo\\`,
			output: `\foo\\1`,
		},
		{
			name:   "triple backslash should be double",
			input:  `\\\foo\\\`,
			output: `\\foo\\\1`,
		},
		{
			name:   "quad backslash should be double",
			input:  `\\\\foo\\\\`,
			output: `\\foo\\\1`,
		},
		{
			name:   "comment in beginning should be remove everything",
			input:  `# foo # foo`,
			output: ``,
		},
		{
			name:   "don't remove escaped comments",
			input:  `\# foo # foo`,
			output: `# foo `,
		},
		{
			name:   "comment in middle should remove everything following",
			input:  `foo # foo`,
			output: `foo `,
		},
		{
			name:   "double quotes should get removed, unless escaped",
			input:  `"foo"foo\"`,
			output: `foofoo"\1`,
		},
		{
			name:   "combination",
			input:  `\\foo\"foo"\#foo\foo'foo\'#foo`,
			output: `\foo"foo#foofoofoo'\''`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewritetarget.SanitizeInput(tc.input)
			if got != tc.output {
				t.Errorf("Failure: expected %s, got %s", tc.output, got)
			}
		})
	}
}

func Test_EscapeSingleQuotes(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		output string
	}{
		{
			name:   "valid string",
			input:  `/foo`,
			output: `/foo`,
		},
		{
			name:   "valid string with bar",
			input:  `/foo/bar`,
			output: `/foo/bar`,
		},
		{
			name:   "escape haproxy single quote string",
			input:  `/it's`,
			output: `/it'\''s`,
		},
		{
			name:   "escape haproxy single quote",
			input:  `'`,
			output: `'\''`,
		},
		{
			name:   "escape haproxy double quotes",
			input:  `''`,
			output: `'\'''\''`,
		},
		{
			name:   "delete haproxy newline",
			input:  "/foo\nbar",
			output: `/foobar`,
		},
		{
			name:   "escape haproxy carriage return",
			input:  "/foo\rbar",
			output: `/foobar`,
		},
		{
			name:   "escape haproxy carriage return and line feed",
			input:  "/foo\r\nbar",
			output: `/foobar`,
		},
		{
			name:   "escape haproxy carriage return and line feed with single quote",
			input:  "/it's\r\n",
			output: `/it'\''s`,
		},
		{
			name:   "escape haproxy with multiple newlines",
			input:  "/foo\n\n\nbar",
			output: `/foobar`,
		},
		{
			name:   "escape haproxy with carriage return and line feed only",
			input:  "\r\n",
			output: ``,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewritetarget.EscapeSingleQuotes(tc.input)
			if got != tc.output {
				t.Errorf("Failure: expected %s, got %s", tc.output, got)
			}
		})
	}
}
