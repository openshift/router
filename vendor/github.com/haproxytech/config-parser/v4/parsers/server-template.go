package parsers

import (
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
	"github.com/haproxytech/config-parser/v4/params"
	"github.com/haproxytech/config-parser/v4/types"
)

type ServerTemplate struct {
	data        []types.ServerTemplate
	preComments []string // comments that appear before the the actual line
}

func (h *ServerTemplate) parse(line string, parts []string, comment string) (*types.ServerTemplate, error) {
	if len(parts) < 4 {
		return nil, &errors.ParseError{Parser: "ServerTemplate", Line: line}
	}
	data := &types.ServerTemplate{
		Prefix:     parts[1],
		NumOrRange: parts[2],
		Fqdn:       parts[3],
		Comment:    comment,
	}
	if len(parts) >= 4 {
		data.Params = params.ParseServerOptions(parts[4:])
	}
	return data, nil
}

func (h *ServerTemplate) Result() ([]common.ReturnResultLine, error) {
	if len(h.data) == 0 {
		return nil, errors.ErrFetch
	}
	result := make([]common.ReturnResultLine, len(h.data))
	for index, req := range h.data {
		var sb strings.Builder
		sb.WriteString("server-template ")
		sb.WriteString(req.Prefix)
		sb.WriteString(" ")
		sb.WriteString(req.NumOrRange)
		sb.WriteString(" ")
		sb.WriteString(req.Fqdn)
		params := params.ServerOptionsString(req.Params)
		if params != "" {
			sb.WriteString(" ")
			sb.WriteString(params)
		}
		result[index] = common.ReturnResultLine{
			Data:    sb.String(),
			Comment: req.Comment,
		}
	}
	return result, nil
}
