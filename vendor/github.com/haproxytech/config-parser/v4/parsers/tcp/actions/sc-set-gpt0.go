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
)

type ScSetGpt0 struct {
	ScID  string
	Value string
}

func (f *ScSetGpt0) Parse(parts []string) error {
	if len(parts) != 2 {
		return fmt.Errorf("not enough params")
	}

	data := strings.TrimPrefix(parts[0], "sc-set-gpt0(")

	data = strings.TrimRight(data, ")")

	f.ScID = data
	f.Value = parts[1]

	return nil
}

func (f *ScSetGpt0) String() string {
	return fmt.Sprintf("sc-set-gpt0(%s) %s", f.ScID, f.Value)
}
