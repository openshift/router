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

type SetVar struct {
	VarScope string
	VarName  string
	Expr     common.Expression
}

func (f *SetVar) Parse(parts []string) error {
	if len(parts) < 2 {
		return fmt.Errorf("not enough params")
	}

	data := strings.TrimPrefix(parts[0], "set-var(")
	data = strings.TrimRight(data, ")")
	d := strings.SplitN(data, ".", 2)
	if len(d) != 2 {
		return fmt.Errorf("incorrect variable name")
	}
	f.VarScope = d[0]
	f.VarName = d[1]

	command, _ := common.SplitRequest(parts[1:])
	expr := common.Expression{}
	err := expr.Parse(command)
	if err != nil {
		return err
	}
	f.Expr = expr

	return nil
}

func (f *SetVar) String() string {
	return fmt.Sprintf("set-var(%s.%s) %s", f.VarScope, f.VarName, f.Expr.String())
}
