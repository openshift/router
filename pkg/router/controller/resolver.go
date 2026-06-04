package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"time"
)

const (
	// dnsResolveTimeout is the maximum time allowed for a single DNS
	// lookup attempt. Matches HAProxy's "timeout resolve 10s" setting
	// in the ingress_dns resolvers section.
	dnsResolveTimeout = 10 * time.Second

	// dnsRetryDelay is the time to wait before retrying a failed DNS
	// lookup. Matches HAProxy's "timeout retry 5s" setting.
	dnsRetryDelay = 5 * time.Second

	// dnsMaxAttempts is the total number of DNS lookup attempts
	// (initial + retries). HAProxy's "resolve_retries 1" means
	// 1 retry after the initial attempt, so 2 attempts total.
	dnsMaxAttempts = 2

	// endpointResolutionTimeout is the maximum total time allowed for
	// resolving all FQDN addresses in a single HandleEndpointSlice
	// call. This caps the total blocking time when an EndpointSlice
	// contains many FQDN addresses.
	endpointResolutionTimeout = 30 * time.Second
)

// EndpointResolver resolves hostnames to IP addresses for
// FQDN-typed EndpointSlice addresses.
type EndpointResolver interface {
	ResolveEndpointAddress(ctx context.Context, hostname string) ([]net.IP, error)
}

// DNSEndpointResolver resolves hostnames using the system DNS
// resolver with HAProxy-compatible timeout and retry settings.
type DNSEndpointResolver struct {
	resolver *net.Resolver
}

func NewDNSEndpointResolver() *DNSEndpointResolver {
	return &DNSEndpointResolver{
		resolver: net.DefaultResolver,
	}
}

// ResolveEndpointAddress resolves a hostname to IP addresses using the
// system DNS resolver with HAProxy-compatible retry and timeout settings.
func (r *DNSEndpointResolver) ResolveEndpointAddress(ctx context.Context, hostname string) ([]net.IP, error) {
	var lastErr error
	for attempt := range dnsMaxAttempts {
		if attempt > 0 {
			retryTimer := time.NewTimer(dnsRetryDelay)
			select {
			case <-ctx.Done():
				retryTimer.Stop()
				return nil, fmt.Errorf("resolving %q: %w (last error: %v)", hostname, ctx.Err(), lastErr)
			case <-retryTimer.C:
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, dnsResolveTimeout)
		addrs, err := r.resolver.LookupIPAddr(attemptCtx, hostname)
		cancel()

		if err == nil {
			return toSortedIPs(addrs), nil
		}
		lastErr = err

		if !isRetryableDNSError(err) {
			break
		}
	}
	return nil, fmt.Errorf("resolving %q: %w", hostname, lastErr)
}

func isRetryableDNSError(err error) bool {
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		return false
	}
	return dnsErr.IsTimeout && !dnsErr.IsNotFound
}

// toSortedIPs converts resolved addresses to net.IP and sorts them
// with IPv6 before IPv4, matching HAProxy's default resolver
// preference (no resolve-prefer directive means IPv6 is preferred).
func toSortedIPs(addrs []net.IPAddr) []net.IP {
	if len(addrs) == 0 {
		return nil
	}
	ips := make([]net.IP, len(addrs))
	for i, addr := range addrs {
		ips[i] = addr.IP
	}
	sortIPsIPv6First(ips)
	return ips
}

// sortIPsIPv6First sorts IPs with IPv6 addresses before IPv4,
// matching HAProxy's default resolver behavior where AAAA records
// (IPv6) are preferred over A records (IPv4) when no resolve-prefer
// directive is set.
func sortIPsIPv6First(ips []net.IP) {
	sort.SliceStable(ips, func(i, j int) bool {
		iIsV4 := ips[i].To4() != nil
		jIsV4 := ips[j].To4() != nil
		if iIsV4 != jIsV4 {
			return !iIsV4
		}
		return false
	})
}
