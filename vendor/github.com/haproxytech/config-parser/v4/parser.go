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

package parser

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"sync"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/options"
)

type Section string

const (
	Comments   Section = "#"
	Defaults   Section = "defaults"
	Global     Section = "global"
	Resolvers  Section = "resolvers"
	UserList   Section = "userlist"
	Peers      Section = "peers"
	Mailers    Section = "mailers"
	Frontends  Section = "frontend"
	Backends   Section = "backend"
	Listen     Section = "listen"
	Cache      Section = "cache"
	Program    Section = "program"
	HTTPErrors Section = "http-errors"
	Ring       Section = "ring"
	// spoe sections
	SPOEAgent   Section = "spoe-agent"
	SPOEGroup   Section = "spoe-group"
	SPOEMessage Section = "spoe-message"
)

const (
	CommentsSectionName = "data"
	GlobalSectionName   = "data"
	DefaultSectionName  = "data"
)

type Parser interface {
	Process(reader io.Reader) error
	String() string
	Save(filename string) error
	StringWithHash() (string, error)
	Get(sectionType Section, sectionName string, attribute string, createIfNotExist ...bool) (common.ParserData, error)
	GetOne(sectionType Section, sectionName string, attribute string, index ...int) (common.ParserData, error)
	SectionsGet(sectionType Section) ([]string, error)
	SectionsDelete(sectionType Section, sectionName string) error
	SectionsCreate(sectionType Section, sectionName string) error
	Set(sectionType Section, sectionName string, attribute string, data common.ParserData, index ...int) error
	Delete(sectionType Section, sectionName string, attribute string, index ...int) error
	Insert(sectionType Section, sectionName string, attribute string, data common.ParserData, index ...int) error
	HasParser(sectionType Section, attribute string) bool
}

type UnlockError struct{}

func (e UnlockError) Error() string {
	return "failed funclock"
}

// configParser reads and writes configuration on given file
type configParser struct {
	Parsers map[Section]map[string]*Parsers
	Options options.Parser
	mutex   *sync.Mutex
}

func New(opt ...options.ParserOption) (Parser, error) {
	p := &configParser{
		Options: options.Parser{},
	}
	var err error

	for _, option := range opt {
		err = option.Set(&p.Options)
		if err != nil {
			return nil, err
		}
	}
	p = initParserMaps(p)
	if p.Options.Path != "" {
		dat, err := ioutil.ReadFile(p.Options.Path)
		if err != nil {
			return nil, err
		}
		return p, p.Process(bytes.NewReader(dat))
	}
	if p.Options.Reader != nil {
		return p, p.Process(p.Options.Reader)
	}

	return nil, errors.New("source not provided")
}
