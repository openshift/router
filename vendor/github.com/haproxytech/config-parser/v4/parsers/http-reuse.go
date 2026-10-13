/*
Copyright 2019 HAProxy Technologies

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parsers

import (
	"fmt"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
	"github.com/haproxytech/config-parser/v4/types"
)

type HTTPReuse struct {
	data        *types.HTTPReuse
	preComments []string // comments that appear before the the actual line
}

func (p *HTTPReuse) Parse(line string, parts, previousPats []string, comment string) (changeState string, err error) {
	if parts[0] == "http-reuse" {
		switch parts[1] {
		case "aggressive", "always", "never", "safe":
			p.data = &types.HTTPReuse{ShareType: parts[1], Comment: comment}
			return "", nil
		default:
			return "", &errors.ParseError{Parser: "HTTPReuse", Line: line}
		}
	}
	return "", &errors.ParseError{Parser: "HTTPReuse", Line: line}
}

func (p *HTTPReuse) Result() ([]common.ReturnResultLine, error) {
	if p.data == nil {
		return nil, errors.ErrFetch
	}
	return []common.ReturnResultLine{
		{
			Data:    fmt.Sprintf("http-reuse %s", p.data.ShareType),
			Comment: p.data.Comment,
		},
	}, nil
}
