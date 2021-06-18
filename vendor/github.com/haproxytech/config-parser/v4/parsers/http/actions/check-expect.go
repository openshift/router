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
	"io"
	"strconv"
	"strings"

	"github.com/haproxytech/config-parser/v4/errors"
)

// http-check expect [min-recv <int>] [comment <msg>]
//                   [ok-status <st>] [error-status <st>] [tout-status <st>]
//                   [on-success <fmt>] [on-error <fmt>] [status-code <expr>]
//                   [!] <match> <pattern>
type CheckExpect struct {
	MinRecv         *int64
	CheckComment    string
	OKStatus        string
	ErrorStatus     string
	TimeoutStatus   string
	OnSuccess       string
	OnError         string
	StatusCode      string
	ExclamationMark bool
	Match           string
	Pattern         string
	Comment         string
}

func (c *CheckExpect) Parse(parts []string, comment string) error {
	if comment != "" {
		c.Comment = comment
	}

	if len(parts) < 3 {
		return fmt.Errorf("not enough params")
	}

	var i int
LoopExpect:
	for i = 2; i < len(parts); i++ {
		el := parts[i]
		switch el {
		case "min-recv":
			if (i + 1) < len(parts) {
				// a *int64 is used as opposed to just int64 since 0 has a special meaning for min-recv
				minRecv, err := strconv.ParseInt(parts[i+1], 10, 64)
				if err != nil {
					return err
				}
				c.MinRecv = &minRecv
				i++
			}
		case "comment":
			checkParsePair(parts, &i, &c.CheckComment)
		case "ok-status":
			checkParsePair(parts, &i, &c.OKStatus)
		case "error-status":
			checkParsePair(parts, &i, &c.ErrorStatus)
		case "tout-status":
			checkParsePair(parts, &i, &c.TimeoutStatus)
		case "on-success":
			checkParsePair(parts, &i, &c.OnSuccess)
		case "on-error":
			checkParsePair(parts, &i, &c.OnError)
		case "status-code":
			checkParsePair(parts, &i, &c.StatusCode)
		case "!":
			c.ExclamationMark = true
		default:
			break LoopExpect
		}
	}

	// if we broke out of the loop, whatever is leftover should be
	// `<match> <pattern>`. Prevent panics with bounds checks for safety.
	if i >= len(parts) {
		return &errors.ParseError{Parser: "HttpCheck", Message: "http-check expect: match not provided"}
	}
	c.Match = parts[i]

	if i+1 >= len(parts) {
		return &errors.ParseError{Parser: "HttpCheck", Message: "http-check expect: pattern not provided"}
	}
	// Since pattern is always the last option provided, we can safely join
	// the remainder as part of the pattern.
	pattern := strings.Join(parts[i+1:], " ")
	c.Pattern = pattern

	return nil
}

func (c *CheckExpect) String() string {
	sb := &strings.Builder{}

	sb.WriteString("expect")

	if c.MinRecv != nil {
		checkWritePair(sb, "min-recv", strconv.Itoa(int(*c.MinRecv)))
	}
	checkWritePair(sb, "comment", c.CheckComment)
	checkWritePair(sb, "ok-status", c.OKStatus)
	checkWritePair(sb, "error-status", c.ErrorStatus)
	checkWritePair(sb, "tout-status", c.TimeoutStatus)
	checkWritePair(sb, "on-success", c.OnSuccess)
	checkWritePair(sb, "on-error", c.OnError)
	checkWritePair(sb, "status-code", c.StatusCode)

	if c.ExclamationMark {
		checkWritePair(sb, "", "!")
	}
	checkWritePair(sb, "", c.Match)
	checkWritePair(sb, "", c.Pattern)

	return sb.String()
}

func (c *CheckExpect) GetComment() string {
	return c.Comment
}

func checkWritePair(sb io.StringWriter, key string, value string) {
	if value != "" {
		_, _ = sb.WriteString(" ")
		if key != "" {
			_, _ = sb.WriteString(key)
			_, _ = sb.WriteString(" ")
		}
		_, _ = sb.WriteString(value)
	}
}

func checkParsePair(parts []string, i *int, str *string) {
	if (*i + 1) < len(parts) {
		*str = parts[*i+1]
		*i++
	}
}
