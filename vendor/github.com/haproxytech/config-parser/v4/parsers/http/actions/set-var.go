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
	Cond     string
	CondTest string
	Comment  string
}

func (f *SetVar) Parse(parts []string, comment string) error {
	// we have filter trace [name <name>] [random-parsing] [random-forwarding] [hexdump]
	if comment != "" {
		f.Comment = comment
	}
	data := strings.TrimPrefix(parts[1], "set-var(")
	data = strings.TrimRight(data, ")")
	d := strings.SplitN(data, ".", 2)
	f.VarScope = d[0]
	f.VarName = d[1]
	if len(parts) >= 3 {
		command, condition := common.SplitRequest(parts[2:]) //  2 not 3 !
		if len(command) > 0 {
			expr := common.Expression{}
			err := expr.Parse(command)
			if err != nil {
				return fmt.Errorf("not enough params")
			}
			f.Expr = expr
		}
		if len(condition) > 1 {
			f.Cond = condition[0]
			f.CondTest = strings.Join(condition[1:], " ")
		}
		return nil
	}
	return fmt.Errorf("not enough params")
}

func (f *SetVar) String() string {
	condition := ""
	if f.Cond != "" {
		condition = fmt.Sprintf(" %s %s", f.Cond, f.CondTest)
	}
	return fmt.Sprintf("set-var(%s.%s) %s%s", f.VarScope, f.VarName, f.Expr.String(), condition)
}

func (f *SetVar) GetComment() string {
	return f.Comment
}
