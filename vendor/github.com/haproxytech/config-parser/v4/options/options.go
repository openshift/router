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

package options

import "io"

type Parser struct {
	UseV2HTTPCheck bool
	UseMd5Hash     bool
	Path           string
	Reader         io.Reader
}

type ParserOption interface {
	Set(p *Parser) error
}

type useMd5Hash struct{}

func (u useMd5Hash) Set(p *Parser) error {
	p.UseMd5Hash = true
	return nil
}

// UseMd5Hash sets flag to use md5 hash
var UseMd5Hash = useMd5Hash{} //nolint:gochecknoglobals

type useV2HTTPCheck struct{}

func (u useV2HTTPCheck) Set(p *Parser) error {
	p.UseV2HTTPCheck = true
	return nil
}

// UseV2HTTPCheck sets flag to use deprecated HTTPCheck
var UseV2HTTPCheck = useV2HTTPCheck{} //nolint:gochecknoglobals

type filename struct {
	Path string
}

func (f filename) Set(p *Parser) error {
	p.Path = f.Path
	return nil
}

// Reader takes path where configuration is stored
func Path(path string) filename { //nolint:golint
	return filename{
		Path: path,
	}
}

type reader struct {
	Reader io.Reader
}

func (f reader) Set(p *Parser) error {
	p.Reader = f.Reader
	return nil
}

// Reader takes io.Reader that will be used to parse data
func Reader(ioReader io.Reader) reader { //nolint:golint
	return reader{
		Reader: ioReader,
	}
}
