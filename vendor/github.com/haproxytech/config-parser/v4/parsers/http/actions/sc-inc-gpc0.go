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

//nolint:dupl
package actions

import (
	"fmt"
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
)

type ScIncGpc0 struct {
	ID       string
	Cond     string
	CondTest string
	Comment  string
}

func (f *ScIncGpc0) Parse(parts []string, comment string) error {
	if comment != "" {
		f.Comment = comment
	}
	f.ID = strings.TrimPrefix(parts[1], "sc-inc-gpc0(")
	f.ID = strings.TrimRight(f.ID, ")")
	if len(parts) >= 4 {
		_, condition := common.SplitRequest(parts[2:])
		if len(condition) > 1 {
			f.Cond = condition[0]
			f.CondTest = strings.Join(condition[1:], " ")
		}
		return nil
	} else if len(parts) == 2 {
		return nil
	}
	return fmt.Errorf("not enough params")
}

func (f *ScIncGpc0) String() string {
	var result strings.Builder
	result.WriteString("sc-inc-gpc0(")
	result.WriteString(f.ID)
	result.WriteString(")")
	if f.Cond != "" {
		result.WriteString(" ")
		result.WriteString(f.Cond)
		result.WriteString(" ")
		result.WriteString(f.CondTest)
	}
	return result.String()
}

func (f *ScIncGpc0) GetComment() string {
	return f.Comment
}
