package rewritetarget_test

import (
	"github.com/openshift/router/pkg/router/template/util/rewritetarget"
	"testing"
)

func Test_SanitizeRewriteTargetInput(t *testing.T) {
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
			got := rewritetarget.SanitizeRewriteTargetInput(tc.input)
			if got != tc.output {
				t.Errorf("Failure: expected %s, got %s", tc.output, got)
			}
		})
	}
}

func Test_SanitizeRewritePathInput(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		output string
	}{
		{
			name:   "normal path",
			input:  `/foo/bar`,
			output: `/foo/bar`,
		},
		{
			name:   "single quotes should be escaped",
			input:  `'foo'foo\'`,
			output: `'\''foo'\''foo\\'\''`,
		},
		{
			name:   "number sign should NOT be escaped",
			input:  `/foo/bar#`,
			output: `/foo/bar#`,
		},
		{
			name:   "backslashes should be escaped",
			input:  `\foo\\foo\\\foo`,
			output: `\\foo\\\\foo\\\\\\foo`,
		},
		{
			name:   "special characters should be escaped",
			input:  `/foo\.+*?()|[]{}^$`,
			output: `/foo\\\.\+\*\?\(\)\|\[\]\{\}\^\$`,
		},
		{
			name:   "new line characters should be escaped",
			input:  "/foo\r\n",
			output: `/foo\r\n`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewritetarget.SanitizeRewritePathInput(tc.input)
			if got != tc.output {
				t.Errorf("Failure: expected %s, got %s", tc.output, got)
			}
		})
	}
}
