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
)

type SendSpoeGroup struct {
	Engine string
	Group  string
}

func (f *SendSpoeGroup) Parse(parts []string) error {
	if len(parts) >= 3 {

		f.Engine = parts[1]
		f.Group = parts[2]

		return nil
	}

	return fmt.Errorf("not enough params")
}

func (f *SendSpoeGroup) String() string {
	return fmt.Sprintf("send-spoe-group %s %s", f.Engine, f.Group)
}
