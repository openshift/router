package haproxytime

import (
	"errors"
	"regexp"
	"strconv"
	"time"
)

// MaxTimeout defines the maximum duration that can be represented in
// HAProxy's configuration.
const MaxTimeout = 2147483647 * time.Millisecond

var (
	// OverflowError is returned when the parsed value exceeds the
	// maximum allowed.
	OverflowError = errors.New("value out of range")

	// SyntaxError is returned when the input string doesn't match
	// HAProxy's duration format.
	SyntaxError = errors.New("invalid duration")

	durationRE = regexp.MustCompile(`^([0-9]+)(us|ms|s|m|h|d)?$`)
)

// ParseDuration takes a string representing a duration in HAProxy's
// specific format and converts it into a time.Duration value. The
// string can include an optional unit suffix, such as "us", "ms",
// "s", "m", "h", or "d". If no suffix is provided, milliseconds are
// assumed. The function returns an OverflowError if the value exceeds
// MaxTimeout, or a SyntaxError if the input string doesn't match the
// expected format.
func ParseDuration(input string) (time.Duration, error) {
	matches := durationRE.FindStringSubmatch(input)
	if matches == nil {
		return 0, SyntaxError
	}

	// Default unit is milliseconds, unless specified.
	unit := time.Millisecond

	numericPart := matches[1]
	unitPart := ""
	if len(matches) > 2 {
		unitPart = matches[2]
	}

	switch unitPart {
	case "us":
		unit = time.Microsecond
	case "ms":
		unit = time.Millisecond
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	}

	value, err := strconv.ParseInt(numericPart, 10, 32)
	if err != nil {
		// ParseInt is documented to return only ErrSyntax or
		// ErrRange when an error occurs. As we've already
		// covered the ErrSyntax case with the regex, we can
		// assume this is ErrRange.
		return 0, OverflowError
	}

	duration := time.Duration(value) * unit
	if duration > MaxTimeout {
		return 0, OverflowError
	}

	return duration, nil
}
