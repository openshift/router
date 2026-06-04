package controller

import (
	"net"
	"testing"
)

func TestSortIPsIPv6First(t *testing.T) {
	tests := []struct {
		name     string
		ips      []net.IP
		expected []string
	}{
		{
			name:     "IPv6 before IPv4",
			ips:      []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("2001:db8::1")},
			expected: []string{"2001:db8::1", "10.0.0.1"},
		},
		{
			name:     "already sorted",
			ips:      []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("10.0.0.1")},
			expected: []string{"2001:db8::1", "10.0.0.1"},
		},
		{
			name:     "multiple mixed",
			ips:      []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("2001:db8::2"), net.ParseIP("10.0.0.1"), net.ParseIP("2001:db8::1")},
			expected: []string{"2001:db8::2", "2001:db8::1", "1.2.3.4", "10.0.0.1"},
		},
		{
			name:     "only IPv4",
			ips:      []net.IP{net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4")},
			expected: []string{"10.0.0.1", "1.2.3.4"},
		},
		{
			name:     "only IPv6",
			ips:      []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")},
			expected: []string{"2001:db8::1", "2001:db8::2"},
		},
		{
			name:     "single IP",
			ips:      []net.IP{net.ParseIP("10.0.0.1")},
			expected: []string{"10.0.0.1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortIPsIPv6First(tc.ips)
			for i, ip := range tc.ips {
				if ip.String() != tc.expected[i] {
					t.Errorf("position %d: expected %s, got %s", i, tc.expected[i], ip.String())
				}
			}
		})
	}
}
