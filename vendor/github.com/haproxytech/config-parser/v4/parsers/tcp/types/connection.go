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

package types

import (
	"fmt"
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
	"github.com/haproxytech/config-parser/v4/parsers/tcp/actions"
	"github.com/haproxytech/config-parser/v4/types"
)

type Connection struct {
	Action   types.TCPAction
	Cond     string
	CondTest string
	Comment  string
}

func (f *Connection) ParseAction(action types.TCPAction, parts []string) error {
	err := action.Parse(parts)
	if err != nil {
		return &errors.ParseError{Parser: "TCPRequestConnection", Line: ""}
	}

	f.Action = action

	return nil
}

func (f *Connection) Parse(parts []string, comment string) error {
	if comment != "" {
		f.Comment = comment
	}

	if len(parts) >= 3 {

		command, condition := common.SplitRequest(parts[2:])

		var err error

		if len(command) > 0 {

			switch command[0] {
			case "accept":
				err = f.ParseAction(&actions.Accept{}, command)
			case "reject":
				err = f.ParseAction(&actions.Reject{}, command)
			case "expect-proxy":
				err = f.ParseAction(&actions.ExpectProxy{}, command)
			case "expect-netscaler-cip":
				err = f.ParseAction(&actions.ExpectNetscalerCip{}, command)
			case "capture":
				err = f.ParseAction(&actions.Capture{}, command)
			case "track-sc0":
				err = f.ParseAction(&actions.TrackSc0{}, command)
			case "track-sc1":
				err = f.ParseAction(&actions.TrackSc1{}, command)
			case "track-sc2":
				err = f.ParseAction(&actions.TrackSc2{}, command)
			case "set-src":
				err = f.ParseAction(&actions.SetSrc{}, command)
			default:
				switch {
				case strings.HasPrefix(command[0], "lua."):
					err = f.ParseAction(&actions.Lua{}, command)
				case strings.HasPrefix(command[0], "sc-inc-gpc0"):
					err = f.ParseAction(&actions.ScIncGpc0{}, command)
				case strings.HasPrefix(command[0], "sc-inc-gpc1"):
					err = f.ParseAction(&actions.ScIncGpc1{}, command)
				case strings.HasPrefix(command[0], "sc-set-gpt0"):
					err = f.ParseAction(&actions.ScSetGpt0{}, command)
				default:
					return err
				}

			}

			if err != nil {
				return err
			}

		} else {
			return fmt.Errorf("not enough params")
		}

		if len(condition) > 1 {
			f.Cond = condition[0]
			f.CondTest = strings.Join(condition[1:], " ")
		}

		return nil
	}
	return fmt.Errorf("not enough params")
}

func (f *Connection) String() string {
	var result strings.Builder

	result.WriteString("connection")
	result.WriteString(" ")
	result.WriteString(f.Action.String())

	if f.Cond != "" {
		result.WriteString(" ")
		result.WriteString(f.Cond)
		result.WriteString(" ")
		result.WriteString(f.CondTest)
	}

	if f.Comment != "" {
		result.WriteString(" # ")
		result.WriteString(f.Comment)
	}

	return result.String()
}

func (f *Connection) GetComment() string {
	return f.Comment
}
