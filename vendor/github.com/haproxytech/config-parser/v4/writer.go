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
	"crypto/md5"
	"fmt"
	"io"
	"strings"

	"github.com/gofrs/flock"
	"github.com/google/renameio"
	"github.com/haproxytech/config-parser/v4/types"
)

// String returns configuration in writable form
func (p *configParser) String() string {
	p.lock()
	defer p.unLock()
	var result strings.Builder

	p.writeParsers("", p.Parsers[Comments][CommentsSectionName], &result, false)
	p.writeParsers("global", p.Parsers[Global][GlobalSectionName], &result, true)
	p.writeParsers("defaults", p.Parsers[Defaults][DefaultSectionName], &result, true)

	sections := []Section{UserList, Peers, Mailers, Resolvers, Cache, Ring, HTTPErrors, Frontends, Backends, Listen, Program}

	for _, section := range sections {
		sortedSections := p.getSortedList(p.Parsers[section])
		for _, sectionName := range sortedSections {
			p.writeParsers(fmt.Sprintf("%s %s", section, sectionName), p.Parsers[section][sectionName], &result, true)
		}
	}
	return result.String()
}

func (p *configParser) Save(filename string) error {
	if p.Options.UseMd5Hash {
		data, err := p.StringWithHash()
		if err != nil {
			return err
		}
		return p.save([]byte(data), filename)
	}
	return p.save([]byte(p.String()), filename)
}

func (p *configParser) save(data []byte, filename string) error {
	f := flock.New(filename)
	if err := f.Lock(); err != nil {
		return err
	}
	err := renameio.WriteFile(filename, data, 0644)
	if err != nil {
		f.Unlock() //nolint:errcheck
		return err
	}
	if err := f.Unlock(); err != nil {
		errMsg := err.Error()
		return fmt.Errorf("%w %s", UnlockError{}, errMsg)
	}
	return nil
}

func (p *configParser) StringWithHash() (string, error) {
	var result strings.Builder
	content := p.String()
	//nolint:gosec
	hash := md5.Sum([]byte(content))
	result.WriteString(fmt.Sprintf("# _md5hash=%x\n", hash))
	result.WriteString(content)
	if err := p.Set(Comments, CommentsSectionName, "# _md5hash", &types.ConfigHash{Value: fmt.Sprintf("%x", hash)}); err != nil {
		return "", err
	}

	return result.String(), nil
}

func (p *configParser) writeSection(sectionName string, comments []string, result io.StringWriter) {
	_, _ = result.WriteString("\n")
	for _, line := range comments {
		_, _ = result.WriteString("# ")
		_, _ = result.WriteString(line)
		_, _ = result.WriteString("\n")
	}
	_, _ = result.WriteString(sectionName)
	_, _ = result.WriteString(" \n")
}

func (p *configParser) writeParsers(sectionName string, parsersData *Parsers, result io.StringWriter, useIndentation bool) {
	sectionNameWritten := false
	switch sectionName {
	case "":
		sectionNameWritten = true
	case "global", "defaults":
		break
	default:
		p.writeSection(sectionName, parsersData.PreComments, result)
		sectionNameWritten = true
	}
	for _, parserName := range parsersData.ParserSequence {
		parser := parsersData.Parsers[string(parserName)]
		lines, comments, err := parser.ResultAll()
		if err != nil {
			continue
		}
		if !sectionNameWritten {
			p.writeSection(sectionName, parsersData.PreComments, result)
			sectionNameWritten = true
		}
		for _, line := range comments {
			if useIndentation {
				_, _ = result.WriteString("  ")
			}
			_, _ = result.WriteString("# ")
			_, _ = result.WriteString(line)
			_, _ = result.WriteString("\n")
		}
		for _, line := range lines {
			if useIndentation {
				_, _ = result.WriteString("  ")
			}
			_, _ = result.WriteString(line.Data)
			if line.Comment != "" {
				_, _ = result.WriteString(" # ")
				_, _ = result.WriteString(line.Comment)
			}
			_, _ = result.WriteString("\n")
		}
	}
	for _, line := range parsersData.PostComments {
		if useIndentation {
			_, _ = result.WriteString("  ")
		}
		_, _ = result.WriteString("# ")
		_, _ = result.WriteString(line)
		_, _ = result.WriteString("\n")
	}
}
