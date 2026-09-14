package params

import (
	"strconv"
	"strings"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
)

type BalanceParams interface {
	String() string
	Parse(parts []string) (BalanceParams, error)
}

type BalanceURI struct {
	Depth int64
	Len   int64
	Whole bool
}

func (b *BalanceURI) String() string {
	var result strings.Builder
	if b.Depth > 0 {
		result.WriteString(" depth ")
		result.WriteString(strconv.FormatInt(b.Depth, 10))
	}
	if b.Len > 0 {
		result.WriteString(" len ")
		result.WriteString(strconv.FormatInt(b.Len, 10))
	}
	if b.Whole {
		result.WriteString(" whole")
	}
	return result.String()
}

func (b *BalanceURI) Parse(parts []string) (bp BalanceParams, err error) {
	if len(parts) > 0 {

		for i := 0; i < len(parts); i++ {
			arg := parts[i]

			switch arg {
			case "whole":
				b.Whole = true
			case "len":
				if i+1 < len(parts) {
					i++
					if b.Len, err = strconv.ParseInt(parts[i], 10, 64); err != nil {
						return nil, &errors.ParseError{Parser: "Balance", Message: err.Error()}
					}
				}
			case "depth":
				if i+1 < len(parts) {
					i++
					if b.Depth, err = strconv.ParseInt(parts[i], 10, 64); err != nil {
						return nil, &errors.ParseError{Parser: "Balance", Message: err.Error()}
					}
				}
			}
		}
		return b, nil
	}
	return nil, errors.ErrInvalidData
}

type BalanceURLParam struct {
	Param     string
	CheckPost int64
	MaxWait   int64
}

func (u *BalanceURLParam) String() string {
	var result strings.Builder
	if u.Param != "" {
		result.WriteString(" ")
		result.WriteString(u.Param)
	}
	if u.CheckPost > 0 {
		result.WriteString(" check_post ")
		result.WriteString(strconv.FormatInt(u.CheckPost, 10))
	}
	if u.MaxWait > 0 {
		result.WriteString(" max_wait ")
		result.WriteString(strconv.FormatInt(u.MaxWait, 10))
	}
	return result.String()
}

func (u *BalanceURLParam) Parse(parts []string) (bp BalanceParams, err error) {
	if len(parts) > 0 {

		for i := 1; i < len(parts); i++ {
			arg := parts[i]

			switch arg {
			case "check_post":
				if i+1 < len(parts) {
					i++
					if u.CheckPost, err = strconv.ParseInt(parts[i], 10, 64); err != nil {
						return nil, &errors.ParseError{Parser: "Balance", Message: err.Error()}
					}
				}
			case "max_wait":
				if i+1 < len(parts) {
					i++
					if u.MaxWait, err = strconv.ParseInt(parts[i], 10, 64); err != nil {
						return nil, &errors.ParseError{Parser: "Balance", Message: err.Error()}
					}
				}
			default:
				if i == 1 {
					u.Param = arg
				}
			}
		}
		return u, nil
	}
	return nil, errors.ErrInvalidData
}

type BalanceHdr struct {
	Name          string
	UseDomainOnly bool
}

func (h *BalanceHdr) String() string {
	var result strings.Builder
	if h.Name != "" {
		result.WriteString("(")
		result.WriteString(h.Name)
		result.WriteString(")")
	}
	if h.UseDomainOnly {
		result.WriteString(" use_domain_only")
	}
	return result.String()
}

func (h *BalanceHdr) Parse(parts []string) (bp BalanceParams, err error) {
	if len(parts) > 0 {
		split := common.StringSplitIgnoreEmpty(parts[0], '(', ')')
		if len(split) < 2 {
			return nil, errors.ErrInvalidData
		}
		h.Name = split[1]
	}
	if len(parts) > 1 {
		if parts[1] != "use_domain_only" {
			return nil, errors.ErrInvalidData
		}
		h.UseDomainOnly = true
	}
	return h, nil
}

type BalanceRandom struct {
	Draws int64
}

func (h *BalanceRandom) String() string {
	var result strings.Builder
	if h.Draws > 0 {
		result.WriteString("(")
		result.WriteString(strconv.FormatInt(h.Draws, 10))
		result.WriteString(")")
	}
	return result.String()
}

func (h *BalanceRandom) Parse(parts []string) (bp BalanceParams, err error) {
	if len(parts) > 0 {
		split := common.StringSplitIgnoreEmpty(parts[0], '(', ')')
		if len(split) < 2 {
			return nil, errors.ErrInvalidData
		}

		if h.Draws, err = strconv.ParseInt(split[1], 10, 64); err != nil {
			return nil, &errors.ParseError{Parser: "Balance", Message: err.Error()}
		}
	}
	return h, nil
}

type BalanceRdpCookie struct {
	Name string
}

func (r *BalanceRdpCookie) String() string {
	var result strings.Builder
	if r.Name != "" {
		result.WriteString("(")
		result.WriteString(r.Name)
		result.WriteString(")")
	}
	return result.String()
}

func (r *BalanceRdpCookie) Parse(parts []string) (bp BalanceParams, err error) {
	if len(parts) > 0 {
		split := common.StringSplitIgnoreEmpty(parts[0], '(', ')')
		if len(split) < 2 {
			return nil, errors.ErrInvalidData
		}

		r.Name = split[1]
	}
	return r, nil
}
