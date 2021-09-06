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

package actions

import (
	"fmt"
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
)

type DoResolve struct {
	Var       string
	Resolvers string
	Protocol  string
	Expr      common.Expression
}

func (f *DoResolve) Parse(parts []string) error {
	data := strings.TrimPrefix(parts[1], "do-resolve(")
	data = strings.TrimRight(data, ")")
	d := strings.SplitN(data, ".", 2)

	f.Var = d[0]
	f.Resolvers = d[1]

	if len(parts) >= 3 {
		command, _ := common.SplitRequest(parts[2:]) // 2 not 3 !
		if len(command) > 0 {
			expr := common.Expression{}
			err := expr.Parse(command)
			if err != nil {
				return fmt.Errorf("not enough params")
			}
			f.Expr = expr
		}
		return nil
	}
	return fmt.Errorf("not enough params")
}

func (f *DoResolve) String() string {
	return fmt.Sprintf("do-resolve(%s.%s,%s) %s", f.Var, f.Resolvers, f.Protocol, f.Expr.String())
}
