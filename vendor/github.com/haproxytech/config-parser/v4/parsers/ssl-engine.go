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
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
	"github.com/haproxytech/config-parser/v4/types"
)

type SslEngine struct {
	data        *types.SslEngine
	preComments []string
}

func (p *SslEngine) Parse(line string, parts, previousParts []string, comment string) (changeState string, err error) {
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "ssl-engine" {
		return "", &errors.ParseError{Parser: "ssl-engine", Line: line}
	}
	p.data = &types.SslEngine{
		Name: parts[1],
		Algorithms: func() (res []string) {
			if len(parts) == 3 {
				res = strings.Split(parts[2], ",")
			}
			return
		}(),
	}
	return "", nil
}

func (p *SslEngine) Result() ([]common.ReturnResultLine, error) {
	if p.data == nil {
		return nil, errors.ErrFetch
	}

	var algs string
	if len(p.data.Algorithms) > 0 {
		algs = " " + strings.Join(p.data.Algorithms, ",")
	}

	return []common.ReturnResultLine{
		{
			Data: fmt.Sprintf("ssl-engine %s%s", p.data.Name, algs),
		},
	}, nil
}
