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

type LoadServerStateFromFile struct {
	data        *types.LoadServerStateFromFile
	preComments []string // comments that appear before the the actual line
}

func (p *LoadServerStateFromFile) Parse(line string, parts, previousPats []string, comment string) (changeState string, err error) {
	if l := len(parts); l == 2 && parts[0] == "load-server-state-from-file" {
		a := parts[1]
		switch a {
		case "global":
			break
		case "local":
			break
		case "none":
			break
		default:
			return "", &errors.ParseError{Parser: "HTTPReuse", Line: line}
		}
		p.data = &types.LoadServerStateFromFile{Argument: a}
		return
	}
	return "", &errors.ParseError{Parser: "load-server-state-from-file", Line: line}
}

func (p *LoadServerStateFromFile) Result() ([]common.ReturnResultLine, error) {
	if p.data == nil {
		return nil, errors.ErrFetch
	}
	return []common.ReturnResultLine{
		{
			Data: fmt.Sprintf("load-server-state-from-file %s", p.data.Argument),
		},
	}, nil
}
