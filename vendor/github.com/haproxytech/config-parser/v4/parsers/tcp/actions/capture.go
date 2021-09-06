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

	"github.com/haproxytech/config-parser/v4/common"
)

type Capture struct {
	Expr common.Expression
	Len  int64
}

func (f *Capture) Parse(parts []string) error {
	expr := common.Expression{}

	err := expr.Parse([]string{parts[1]})
	if err != nil {
		return fmt.Errorf("invalid expression")
	}

	f.Expr = expr

	if len(parts) != 4 {
		return fmt.Errorf("not enough params")
	}

	if len, err := strconv.ParseInt(parts[3], 10, 64); err == nil {
		f.Len = len
	} else {
		return fmt.Errorf("invalid value for len")
	}

	return nil
}

func (f *Capture) String() string {
	return fmt.Sprintf("capture %s len %d", f.Expr.String(), f.Len)
}
