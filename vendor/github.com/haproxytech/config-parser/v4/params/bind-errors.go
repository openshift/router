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

package params

import "fmt"

type ErrParseBindOption interface {
	Error() string
}

type ErrParseServerOption interface {
	Error() string
}

// ErrNotFound struct for creating parse errors
type ErrNotFound struct {
	Have string
	Want string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("error: have [%s] want [%s]", e.Have, e.Want)
}

// ParseError struct for creating parse errors
type ErrNotEnoughParams struct {
}

func (e *ErrNotEnoughParams) Error() string {
	return "error: not enough params"
}
