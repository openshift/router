package haproxytime

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"time"
)

var (
	// OverflowError represents an overflow error from ParseDuration.
	// OverflowError is returned if the input value is greater than what ParseDuration
	// allows (value must be representable as int64, e.g. 9223372036854775807 nanoseconds).
	OverflowError = errors.New("overflow")

	// SyntaxError represents an error based on invalid input to ParseDuration.
	SyntaxError = errors.New("invalid duration")

	// durationRE regexp should match the one in $timeSpecPattern in the haproxy-config.template,
	// except that we use ^$ anchors and a capture group around the numeric part to simplify the
	// duration parsing.
	durationRE = regexp.MustCompile(`^([1-9][0-9]*)(us|ms|s|m|h|d)?$`)
)

// ParseDuration takes a string representing a duration in HAProxy's
// specific format and converts it into a time.Duration value. The
// string can include an optional unit suffix, such as "us", "ms",
// "s", "m", "h", or "d". If no suffix is provided, milliseconds are
// assumed. The function returns OverflowError if the value exceeds
// the maximum allowable input, or SyntaxError if the input string
// doesn't match the expected format.
func ParseDuration(input string) (time.Duration, error) {
	matches := durationRE.FindStringSubmatch(input)
	if matches == nil {
		return 0, SyntaxError
	}

	// Unit is milliseconds when left unspecified.
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
		// ParseInt is documented to return only ErrSyntax or
		// ErrRange when an error occurs. As we've already
		// covered the ErrSyntax case with the regex, we can
		// assume this is ErrRange.
		return 0, OverflowError
	}

	// Check for overflow conditions before multiplying 'value' by 'unit'.
	// 'value' is guaranteed to be >= 0, as ensured by the preceding regular expression.
	//
	// The maximum allowable 'value' is determined by dividing math.MaxInt64
	// by the nanosecond representation of 'unit'. This prevents overflow when
	// 'value' is later multiplied by 'unit'.
	//
	// Examples:
	//  1. If the 'unit' is time.Second (1e9 ns), then the maximum
	//     'value' allowed is math.MaxInt64 / 1e9.
	//  2. If the 'unit' is 24 * time.Hour (86400e9 ns), then the
	//     maximum 'value' allowed is math.MaxInt64 / 86400e9.
	//  3. If the 'unit' is time.Microsecond (1e3 ns), then the maximum
	//     'value' allowed is math.MaxInt64 / 1e3.
	//
	// Concrete examples with actual values:
	//   - No Overflow (days): "106751d" as input makes 'unit' 24 * time.Hour (86400e9 ns).
	//     The check ensures 106751 <= math.MaxInt64 / 86400e9.
	//     Specifically, 106751 <= 9223372036854775807 / 86400000000 (106751 <= 106751).
	//     This is the maximum 'value' for days that won't cause an overflow.
	//
	//   - Overflow (days): Specifying "106752d" makes 'unit' 24 * time.Hour (86400e9 ns).
	//     The check finds 106752 > math.MaxInt64 / 86400e9.
	//     Specifically, 106752 > 9223372036854775807 / 86400000000 (106752 > 106751), causing an overflow.
	//
	//   - No Overflow (us): "9223372036854775us" makes 'unit' time.Microsecond (1e3 ns).
	//     The check ensures 9223372036854775 <= math.MaxInt64 / 1e3.
	//     Specifically, 9223372036854775 <= 9223372036854775807 / 1000 (9223372036854775 <= 9223372036854775).
	//     This is the maximum 'value' for microseconds that won't cause an overflow.
	if value > math.MaxInt64/int64(unit) {
		return 0, OverflowError
	}

	duration := time.Duration(value) * unit
	return duration, nil
}
