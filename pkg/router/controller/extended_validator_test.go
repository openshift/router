package controller

import (
	"fmt"
	"net"
	"testing"

	kapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"

	routev1 "github.com/openshift/api/route/v1"
)

type fakeTestPlugin struct {
	endpoints []*kapi.Endpoints
}

func (p *fakeTestPlugin) HandleRoute(watch.EventType, *routev1.Route) error { return nil }
func (p *fakeTestPlugin) HandleEndpoints(eventType watch.EventType, endpoints *kapi.Endpoints) error {
	p.endpoints = append(p.endpoints, endpoints)
	return nil
}
func (p *fakeTestPlugin) HandleNamespaces(sets.String) error           { return nil }
func (p *fakeTestPlugin) HandleNode(watch.EventType, *kapi.Node) error { return nil }
func (p *fakeTestPlugin) Commit() error                                { return nil }

type fakeTestRecorder struct {
	rejections []string
}

func (r *fakeTestRecorder) RecordRouteRejection(route *routev1.Route, reason, message string) {
	r.rejections = append(r.rejections, fmt.Sprintf("%s: %s", reason, message))
}
func (r *fakeTestRecorder) RecordRouteUpdate(route *routev1.Route, reason, message string) {}
func (r *fakeTestRecorder) RecordRouteUnservableInFutureVersions(route *routev1.Route, reason, message string) {
}
func (r *fakeTestRecorder) RecordRouteUnservableInFutureVersionsClear(route *routev1.Route) {}

func TestCheckRestrictedIP(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		expectError bool
	}{
		{
			name:        "valid public IPv4",
			ip:          "1.2.3.4",
			expectError: false,
		},
		{
			name:        "valid private IPv4",
			ip:          "10.0.0.1",
			expectError: false,
		},
		{
			name:        "loopback IPv4",
			ip:          "127.0.0.1",
			expectError: true,
		},
		{
			name:        "loopback IPv6",
			ip:          "::1",
			expectError: true,
		},
		{
			name:        "link-local IPv4 metadata",
			ip:          "169.254.169.254",
			expectError: true,
		},
		{
			name:        "link-local IPv4 other",
			ip:          "169.254.1.1",
			expectError: true,
		},
		{
			name:        "Azure metadata IP",
			ip:          "168.63.129.16",
			expectError: true,
		},
		{
			name:        "valid IPv6",
			ip:          "2001:db8::1",
			expectError: false,
		},
		{
			name:        "link-local IPv6",
			ip:          "fe80::1",
			expectError: true,
		},
		{
			name:        "unspecified IPv4",
			ip:          "0.0.0.0",
			expectError: true,
		},
		{
			name:        "unspecified IPv6",
			ip:          "::",
			expectError: true,
		},
		{
			name:        "AWS IPv6 IMDS",
			ip:          "fd00:ec2::254",
			expectError: true,
		},
		{
			name:        "link-local multicast IPv6",
			ip:          "ff02::1",
			expectError: true,
		},
		{
			name:        "link-local multicast IPv4",
			ip:          "224.0.0.1",
			expectError: true,
		},
		{
			name:        "non-link-local multicast IPv4",
			ip:          "239.255.255.250",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			err := checkRestrictedIP(ip)
			if tc.expectError && err == nil {
				t.Errorf("expected error for IP %s, got nil", tc.ip)
			}
			if !tc.expectError && err != nil {
				t.Errorf("expected no error for IP %s, got %v", tc.ip, err)
			}
		})
	}
}

func TestValidateEndpointAddress(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		expectError bool
	}{
		{
			name:        "valid public IPv4",
			address:     "10.0.0.1",
			expectError: false,
		},
		{
			name:        "valid IPv6",
			address:     "2001:db8::1",
			expectError: false,
		},
		{
			name:        "restricted loopback IP",
			address:     "127.0.0.1",
			expectError: true,
		},
		{
			name:        "restricted link-local IP",
			address:     "169.254.169.254",
			expectError: true,
		},
		{
			name:        "restricted Azure metadata IP",
			address:     "168.63.129.16",
			expectError: true,
		},
		{
			name:        "unspecified IPv4",
			address:     "0.0.0.0",
			expectError: true,
		},
		{
			name:        "non-IP address rejected",
			address:     "evil.example.com",
			expectError: true,
		},
		{
			name:        "empty string rejected",
			address:     "",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEndpointAddress(tc.address)
			if tc.expectError && err == nil {
				t.Errorf("expected error for address %q, got nil", tc.address)
			}
			if !tc.expectError && err != nil {
				t.Errorf("expected no error for address %q, got %v", tc.address, err)
			}
		})
	}
}

func TestExtendedValidator_HandleEndpoints(t *testing.T) {
	tests := []struct {
		name              string
		endpoints         *kapi.Endpoints
		expectedAddrCount int
		expectedNRCount   int
	}{
		{
			name: "valid IP passes through",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "1.2.3.4"}},
					},
				},
			},
			expectedAddrCount: 1,
		},
		{
			name: "restricted link-local IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "169.254.169.254"}},
					},
				},
			},
			expectedAddrCount: 0,
		},
		{
			name: "restricted loopback IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "127.0.0.1"}},
					},
				},
			},
			expectedAddrCount: 0,
		},
		{
			name: "restricted IP in NotReadyAddresses filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{{IP: "169.254.169.254"}},
					},
				},
			},
			expectedAddrCount: 0,
			expectedNRCount:   0,
		},
		{
			name: "valid IP in NotReadyAddresses passes through",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						NotReadyAddresses: []kapi.EndpointAddress{{IP: "10.0.0.5"}},
					},
				},
			},
			expectedAddrCount: 0,
			expectedNRCount:   1,
		},
		{
			name: "mixed valid and restricted keeps only valid",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "10.0.0.1"}},
					},
					{
						Addresses: []kapi.EndpointAddress{{IP: "127.0.0.1"}},
					},
				},
			},
			expectedAddrCount: 1,
		},
		{
			name: "Azure metadata IP filtered out",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{{IP: "168.63.129.16"}},
					},
				},
			},
			expectedAddrCount: 0,
		},
		{
			name: "mixed valid and restricted in same subset",
			endpoints: &kapi.Endpoints{
				Subsets: []kapi.EndpointSubset{
					{
						Addresses: []kapi.EndpointAddress{
							{IP: "10.0.0.1"},
							{IP: "127.0.0.1"},
							{IP: "10.0.0.2"},
						},
					},
				},
			},
			expectedAddrCount: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := &fakeTestPlugin{}
			recorder := &fakeTestRecorder{}
			validator := NewExtendedValidator(inner, recorder, true)

			err := validator.HandleEndpoints(watch.Added, tc.endpoints)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(inner.endpoints) != 1 {
				t.Fatalf("expected 1 endpoint event, got %d", len(inner.endpoints))
			}

			totalAddrs := 0
			totalNR := 0
			for _, subset := range inner.endpoints[0].Subsets {
				totalAddrs += len(subset.Addresses)
				totalNR += len(subset.NotReadyAddresses)
			}
			if totalAddrs != tc.expectedAddrCount {
				t.Errorf("expected %d addresses, got %d", tc.expectedAddrCount, totalAddrs)
			}
			if tc.expectedNRCount > 0 && totalNR != tc.expectedNRCount {
				t.Errorf("expected %d not-ready addresses, got %d", tc.expectedNRCount, totalNR)
			}
		})
	}
}

func TestExtendedValidator_EndpointValidationWithRouteValidationDisabled(t *testing.T) {
	inner := &fakeTestPlugin{}
	recorder := &fakeTestRecorder{}
	validator := NewExtendedValidator(inner, recorder, false)

	endpoints := &kapi.Endpoints{
		Subsets: []kapi.EndpointSubset{{
			Addresses: []kapi.EndpointAddress{
				{IP: "10.0.0.1"},
				{IP: "127.0.0.1"},
			},
		}},
	}

	err := validator.HandleEndpoints(watch.Added, endpoints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inner.endpoints) != 1 {
		t.Fatalf("expected 1 endpoint event, got %d", len(inner.endpoints))
	}

	totalAddrs := 0
	for _, subset := range inner.endpoints[0].Subsets {
		totalAddrs += len(subset.Addresses)
	}
	if totalAddrs != 1 {
		t.Errorf("expected 1 valid address (loopback filtered), got %d", totalAddrs)
	}
}
