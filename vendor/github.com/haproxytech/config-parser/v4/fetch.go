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
	"fmt"
	"sort"

	"github.com/haproxytech/config-parser/v4/common"
	"github.com/haproxytech/config-parser/v4/errors"
)

func (p *configParser) lock() {
	p.mutex.Lock()
}

func (p *configParser) unLock() {
	p.mutex.Unlock()
}

// Get get attribute from defaults section
func (p *configParser) Get(sectionType Section, sectionName string, attribute string, createIfNotExist ...bool) (common.ParserData, error) {
	p.lock()
	defer p.unLock()
	st, ok := p.Parsers[sectionType]
	if !ok {
		return nil, errors.ErrSectionMissing
	}
	section, ok := st[sectionName]
	if !ok {
		return nil, errors.ErrSectionMissing
	}
	createNew := false
	if len(createIfNotExist) > 0 && createIfNotExist[0] {
		createNew = true
	}
	return section.Get(attribute, createNew)
}

// GetOne get attribute from defaults section
func (p *configParser) GetOne(sectionType Section, sectionName string, attribute string, index ...int) (common.ParserData, error) {
	p.lock()
	defer p.unLock()
	setIndex := -1
	if len(index) > 0 && index[0] > -1 {
		setIndex = index[0]
	}
	st, ok := p.Parsers[sectionType]
	if !ok {
		return nil, errors.ErrSectionMissing
	}
	section, ok := st[sectionName]
	if !ok {
		return nil, errors.ErrSectionMissing
	}
	return section.GetOne(attribute, setIndex)
}

// SectionsGet lists all sections of certain type
func (p *configParser) SectionsGet(sectionType Section) ([]string, error) {
	p.lock()
	defer p.unLock()
	st, ok := p.Parsers[sectionType]
	if !ok {
		return nil, errors.ErrSectionMissing
	}
	result := make([]string, len(st))
	index := 0
	for sectionName := range st {
		result[index] = sectionName
		index++
	}
	return result, nil
}

// SectionsDelete deletes one section of sectionType
func (p *configParser) SectionsDelete(sectionType Section, sectionName string) error {
	p.lock()
	defer p.unLock()
	_, ok := p.Parsers[sectionType]
	if !ok {
		return errors.ErrSectionMissing
	}
	delete(p.Parsers[sectionType], sectionName)
	return nil
}

// SectionsCreate creates one section of sectionType
func (p *configParser) SectionsCreate(sectionType Section, sectionName string) error {
	p.lock()
	defer p.unLock()
	st, ok := p.Parsers[sectionType]
	if !ok {
		return errors.ErrSectionMissing
	}
	_, ok = st[sectionName]
	if ok {
		return errors.ErrSectionAlreadyExists
	}

	parsers := ConfiguredParsers{
		State:    "",
		Active:   p.Parsers[Comments][CommentsSectionName],
		Comments: p.Parsers[Comments][CommentsSectionName],
		Defaults: p.Parsers[Defaults][DefaultSectionName],
		Global:   p.Parsers[Global][GlobalSectionName],
	}

	previousLine := []string{}
	parts := []string{string(sectionType), sectionName}
	comment := ""
	p.ProcessLine(fmt.Sprintf("%s %s", sectionType, sectionName), parts, previousLine, comment, parsers)
	return nil
}

// Set sets attribute from defaults section, can be nil to disable/remove
func (p *configParser) Set(sectionType Section, sectionName string, attribute string, data common.ParserData, index ...int) error {
	p.lock()
	defer p.unLock()
	setIndex := -1
	if len(index) > 0 && index[0] > -1 {
		setIndex = index[0]
	}
	st, ok := p.Parsers[sectionType]
	if !ok {
		return errors.ErrSectionMissing
	}
	section, ok := st[sectionName]
	if !ok {
		return errors.ErrSectionMissing
	}
	return section.Set(attribute, data, setIndex)
}

// Delete remove attribute on defined index, in case of single attributes, index is ignored
func (p *configParser) Delete(sectionType Section, sectionName string, attribute string, index ...int) error {
	p.lock()
	defer p.unLock()
	setIndex := -1
	if len(index) > 0 && index[0] > -1 {
		setIndex = index[0]
	}
	st, ok := p.Parsers[sectionType]
	if !ok {
		return errors.ErrSectionMissing
	}
	section, ok := st[sectionName]
	if !ok {
		return errors.ErrSectionMissing
	}
	return section.Delete(attribute, setIndex)
}

// Insert put attribute on defined index, in case of single attributes, index is ignored
func (p *configParser) Insert(sectionType Section, sectionName string, attribute string, data common.ParserData, index ...int) error {
	p.lock()
	defer p.unLock()
	setIndex := -1
	if len(index) > 0 && index[0] > -1 {
		setIndex = index[0]
	}
	st, ok := p.Parsers[sectionType]
	if !ok {
		return errors.ErrSectionMissing
	}
	section, ok := st[sectionName]
	if !ok {
		return errors.ErrSectionMissing
	}
	return section.Insert(attribute, data, setIndex)
}

// HasParser checks if we have a parser for attribute
func (p *configParser) HasParser(sectionType Section, attribute string) bool {
	p.lock()
	defer p.unLock()
	st, ok := p.Parsers[sectionType]
	if !ok {
		return false
	}
	sectionName := ""
	for name := range st {
		sectionName = name

		break
	}
	section, ok := st[sectionName]
	if !ok {
		return false
	}
	return section.HasParser(attribute)
}

func (p *configParser) getSortedList(data map[string]*Parsers) []string {
	result := make([]string, len(data))
	index := 0
	for parserSectionName := range data {
		result[index] = parserSectionName
		index++
	}
	sort.Strings(result)
	return result
}
