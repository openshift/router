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

package parsers

import (
	"fmt"
	"strconv"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
	"github.com/haproxytech/config-parser/v4/types"
)

type Peer struct {
	data        []types.Peer
	preComments []string // comments that appear before the the actual line
}

func (l *Peer) parse(line string, parts []string, comment string) (*types.Peer, error) {
	if len(parts) >= 2 {
		adr := common.StringSplitIgnoreEmpty(parts[2], ':')
		if len(adr) >= 2 {
			if port, err := strconv.ParseInt(adr[1], 10, 64); err == nil {
				return &types.Peer{
					Name:    parts[1],
					IP:      adr[0],
					Port:    port,
					Comment: comment,
				}, nil
			}
		}
	}
	return nil, &errors.ParseError{Parser: "PeerLines", Line: line}
}

func (l *Peer) Result() ([]common.ReturnResultLine, error) {
	if len(l.data) == 0 {
		return nil, errors.ErrFetch
	}
	result := make([]common.ReturnResultLine, len(l.data))
	for index, peer := range l.data {
		result[index] = common.ReturnResultLine{
			Data:    fmt.Sprintf("peer %s %s:%d", peer.Name, peer.IP, peer.Port),
			Comment: peer.Comment,
		}
	}
	return result, nil
}
