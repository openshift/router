package haproxy

import (
	"errors"
)

var (
	// ErrServerAlreadyExists is an attempt to add a new backend server whose server name was already used.
	ErrServerAlreadyExists = errors.New("ErrServerAlreadyExists")

	// ErrHealthCheckNotConfigured is an attempt to enable health check on a backend server whose health check was not configured.
	ErrHealthCheckNotConfigured = errors.New("ErrHealthCheckNotConfigured")
)

func NewError(kind error, message string) *responseError {
	return &responseError{
		kind:    kind,
		message: message,
	}
}

type responseError struct {
	kind    error
	message string
}

func (e *responseError) Error() string {
	return e.message
}

func (e *responseError) Unwrap() error {
	return e.kind
}
