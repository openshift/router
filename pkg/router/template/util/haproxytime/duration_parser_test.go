package haproxytime_test

import (
	"testing"
	"time"

	"github.com/openshift/router/pkg/router/template/util/haproxytime"
)

func TestParseHAProxyDuration(t *testing.T) {
	tests := []struct {
		input            string
		expectedDuration time.Duration
		expectedErr      error
	}{
		// Syntax error test cases.
		{" spaces are invalid", 0, haproxytime.SyntaxError},
		{"", 0, haproxytime.SyntaxError},
		{"+100", 0, haproxytime.SyntaxError},
		{"-100", 0, haproxytime.SyntaxError},
		{"-1us", 0, haproxytime.SyntaxError},
		{"/", 0, haproxytime.SyntaxError},
		{"123ns", 0, haproxytime.SyntaxError},
		{"invalid", 0, haproxytime.SyntaxError},

		// Validate default unit.
		{"0", 0 * time.Millisecond, nil},

		// Small values for each unit.
		{"0us", 0 * time.Microsecond, nil},
		{"1us", 1 * time.Microsecond, nil},
		{"0ms", 0 * time.Millisecond, nil},
		{"1ms", 1 * time.Millisecond, nil},
		{"0s", 0 * time.Second, nil},
		{"1s", 1 * time.Second, nil},
		{"0m", 0 * time.Minute, nil},
		{"1m", 1 * time.Minute, nil},
		{"0h", 0 * time.Hour, nil},
		{"1h", 1 * time.Hour, nil},
		{"0d", 0 * time.Hour, nil},
		{"1d", 24 * time.Hour, nil},

		// The maximum duration that can be represented in a
		// time.Duration value is determined by the limits of
		// int64, as time.Duration is just an alias for int64
		// where each unit represents a nanosecond.
		//
		// The maximum int64 value is 9223372036854775807.
		//
		// Therefore, the maximum durations for various units
		// are calculated as follows:
		//
		// - Nanoseconds: 9223372036854775807 (since the base unit is a nanosecond)
		// - Microseconds: 9223372036854775 (9223372036854775807 / 1000)
		// - Milliseconds: 9223372036854 (9223372036854775807 / 1000000)
		// - Seconds: 9223372036 (9223372036854775807 / 1000000000)
		// - Minutes: 153722867 (9223372036854775807 / 60000000000)
		// - Hours: 2562047 (9223372036854775807 / 3600000000000)
		// - Days: 106751 (9223372036854775807 / 86400000000000)

		// The largest representable value for each unit.
		{"9223372036854775807ns", 0, haproxytime.SyntaxError},
		{"9223372036854775us", 9223372036854775 * time.Microsecond, nil},
		{"9223372036854ms", 9223372036854 * time.Millisecond, nil},
		{"9223372036s", 9223372036 * time.Second, nil},
		{"153722867m", 153722867 * time.Minute, nil},
		{"2562047h", 2562047 * time.Hour, nil},
		{"106751d", 106751 * 24 * time.Hour, nil},

		// Overflow cases.
		{"9223372036854775808ns", 0, haproxytime.SyntaxError},
		{"9223372036854776us", 0, haproxytime.OverflowError},
		{"9223372036855ms", 0, haproxytime.OverflowError},
		{"9223372037s", 0, haproxytime.OverflowError},
		{"153722868m", 0, haproxytime.OverflowError},
		{"2562048h", 0, haproxytime.OverflowError},
		{"106752d", 0, haproxytime.OverflowError},

		// Test strconv.ParseInt errors as value is bigger
		// than int64 max.
		{"18446744073709551615us", 0, haproxytime.OverflowError},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			duration, err := haproxytime.ParseDuration(tc.input)
			if duration != tc.expectedDuration {
				t.Errorf("expected duration %v, got %v", tc.expectedDuration, duration)
			}
			if err != nil && tc.expectedErr == nil {
				t.Errorf("expected no error, got %v", err)
			} else if err == nil && tc.expectedErr != nil {
				t.Errorf("expected error %v, got none", tc.expectedErr)
			} else if err != nil && tc.expectedErr != nil && err.Error() != tc.expectedErr.Error() {
				t.Errorf("expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}
