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

// http-check connect [default] [port <expr>] [addr <ip>] [send-proxy]
//                    [via-socks4] [ssl] [sni <sni>] [alpn <alpn>] [linger]
//                    [proto <name>] [comment <msg>]
type CheckConnect struct {
	Port         string
	Addr         string
	SNI          string
	ALPN         string
	Proto        string
	CheckComment string
	Comment      string
	Default      bool
	SendProxy    bool
	ViaSOCKS4    bool
	SSL          bool
	Linger       bool
}

func (c *CheckConnect) Parse(parts []string, comment string) error {
	if comment != "" {
		c.Comment = comment
	}

	// Note: "http-check connect" with no further params is allowed by HAProxy
	if len(parts) < 2 {
		return fmt.Errorf("not enough params")
	}

	for i := 2; i < len(parts); i++ {
		el := parts[i]
		switch el {
		case "default":
			c.Default = true
		case "port":
			checkParsePair(parts, &i, &c.Port)
		case "addr":
			checkParsePair(parts, &i, &c.Addr)
		case "send-proxy":
			c.SendProxy = true
		case "via-socks4":
			c.ViaSOCKS4 = true
		case "ssl":
			c.SSL = true
		case "sni":
			checkParsePair(parts, &i, &c.SNI)
		case "alpn":
			checkParsePair(parts, &i, &c.ALPN)
		case "linger":
			c.Linger = true
		case "proto":
			checkParsePair(parts, &i, &c.Proto)
		case "comment":
			checkParsePair(parts, &i, &c.CheckComment)
		}
	}

	return nil
}

func (c *CheckConnect) String() string {
	sb := &strings.Builder{}

	sb.WriteString("connect")

	if c.Default {
		checkWritePair(sb, "", "default")
	}
	checkWritePair(sb, "port", c.Port)
	checkWritePair(sb, "addr", c.Addr)
	if c.SendProxy {
		checkWritePair(sb, "", "send-proxy")
	}
	if c.ViaSOCKS4 {
		checkWritePair(sb, "", "via-socks4")
	}
	if c.SSL {
		checkWritePair(sb, "", "ssl")
	}
	checkWritePair(sb, "sni", c.SNI)
	checkWritePair(sb, "alpn", c.ALPN)

	if c.Linger {
		checkWritePair(sb, "", "linger")
	}
	checkWritePair(sb, "proto", c.Proto)
	checkWritePair(sb, "comment", c.CheckComment)

	return sb.String()
}

func (c *CheckConnect) GetComment() string {
	return c.Comment
}
