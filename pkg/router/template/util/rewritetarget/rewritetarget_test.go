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
