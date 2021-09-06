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
	"strconv"
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
)

type ScSetGpt0 struct {
	ID       string
	Int      *int64
	Expr     common.Expression
	Cond     string
	CondTest string
	Comment  string
}

func (f *ScSetGpt0) Parse(parts []string, comment string) error {
	if comment != "" {
		f.Comment = comment
	}
	f.ID = strings.TrimPrefix(parts[1], "sc-set-gpt0(")
	f.ID = strings.TrimRight(f.ID, ")")
	if len(parts) >= 3 {
		command, condition := common.SplitRequest(parts[2:])
		if len(command) < 1 {
			return errors.ErrInvalidData
		}
		i, err := strconv.ParseInt(command[0], 10, 64)
		if err == nil {
			f.Int = &i
		} else {
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

func (f *ScSetGpt0) String() string {
	var result strings.Builder
	result.WriteString("sc-set-gpt0(")
	result.WriteString(f.ID)
	result.WriteString(") ")
	if f.Int != nil {
		result.WriteString(strconv.FormatInt(*f.Int, 10))
	} else {
		result.WriteString(f.Expr.String())
	}
	if f.Cond != "" {
		result.WriteString(" ")
		result.WriteString(f.Cond)
		result.WriteString(" ")
		result.WriteString(f.CondTest)
	}
	return result.String()
}

func (f *ScSetGpt0) GetComment() string {
	return f.Comment
}
