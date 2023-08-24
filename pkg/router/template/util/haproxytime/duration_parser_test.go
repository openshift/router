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
		// Success Cases
		{"0", 0, nil},
		{"123us", 123 * time.Microsecond, nil},
		{"456ms", 456 * time.Millisecond, nil},
		{"789s", 789 * time.Second, nil},
		{"5m", 5 * time.Minute, nil},
		{"2h", 2 * time.Hour, nil},
		{"1d", 24 * time.Hour, nil},
		{"24d", 24 * 24 * time.Hour, nil},
		{"2147483646", haproxytime.MaxTimeout - time.Millisecond, nil},
		{"2147483647", haproxytime.MaxTimeout, nil},

		// Syntax Error Cases
		{"", 0, haproxytime.SyntaxError},
		{"invalid", 0, haproxytime.SyntaxError},
		{"+100", 0, haproxytime.SyntaxError},
		{"-100", 0, haproxytime.SyntaxError},
		{"/", 0, haproxytime.SyntaxError},
		{" spaces are invalid", 0, haproxytime.SyntaxError},

		// Overflow Error Cases
		{"25d", 0, haproxytime.OverflowError},
		{"9999999999", 0, haproxytime.OverflowError},
		{"1000000000000000000000000000", 0, haproxytime.OverflowError},
		{"1000000000000000000000000000h", 0, haproxytime.OverflowError},
		{"2147483648", 0, haproxytime.OverflowError},
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
