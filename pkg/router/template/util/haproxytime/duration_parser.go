package haproxytime

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"
)

var (
	// OverflowError is returned when the parsed value exceeds the
	// maximum allowed.
	OverflowError = errors.New("overflow")

	// SyntaxError is returned when the input string doesn't match
	// HAProxy's duration format.
	SyntaxError = errors.New("invalid duration")

	durationRE = regexp.MustCompile(`^([0-9]+)(us|ms|s|m|h|d)?$`)
)

// ParseDuration takes a string representing a duration in HAProxy's
// specific format, which permits days ("d"), and converts it into a
// time.Duration value. The input string can include an optional unit
// suffix, such as "us", "ms", "s", "m", "h", or "d". If no suffix is
// provided, milliseconds are assumed. The function returns an
// OverflowError if the value would result in a 64-bit integer
// overflow, or a SyntaxError if the input string doesn't match the
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

	value, err := strconv.ParseInt(numericPart, 10, 64)
	if err != nil {
		return 0, OverflowError
	}

	if value > math.MaxInt64/int64(unit) {
		return 0, OverflowError
	}

	return time.Duration(value) * unit, nil
}
