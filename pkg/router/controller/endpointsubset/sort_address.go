package endpointsubset

import (
	"bytes"
	"net"
	"sort"
	"strings"

	kapi "k8s.io/api/core/v1"
)

type EndpointAddressCmpFunc func(x, y *kapi.EndpointAddress) int
type EndpointAddressLessFunc func(x, y *kapi.EndpointAddress) bool

type endpointAddressMultiSorter struct {
	addresses []kapi.EndpointAddress
	less      []EndpointAddressLessFunc
}

var (
	EndpointAddressHostnameCmpFn = func(x, y *kapi.EndpointAddress) int {
		return strings.Compare(x.Hostname, y.Hostname)
	}

	EndpointAddressIPCmpFn = func(x, y *kapi.EndpointAddress) int {
		return bytes.Compare(net.ParseIP(x.IP), net.ParseIP(y.IP))
	}

	EndpointAddressHostnameLessFn = func(x, y *kapi.EndpointAddress) bool {
		return EndpointAddressHostnameCmpFn(x, y) < 0
	}

	EndpointAddressIPLessFn = func(x, y *kapi.EndpointAddress) bool {
		return EndpointAddressIPCmpFn(x, y) < 0
	}
)

var _ sort.Interface = &endpointAddressMultiSorter{}

// Sort sorts the argument slice according to the comparator functions
// passed to orderBy.
func (s *endpointAddressMultiSorter) Sort(addresses []kapi.EndpointAddress) {
	s.addresses = addresses
	sort.Sort(s)
}

// newEndpointAddressOrderBy returns a Sorter that sorts using a number
// of comparator functions.
func newEndpointAddressOrderBy(less ...EndpointAddressLessFunc) *endpointAddressMultiSorter {
	return &endpointAddressMultiSorter{
		less: less,
	}
}

// Len is part of sort.Interface.
func (s *endpointAddressMultiSorter) Len() int {
	return len(s.addresses)
}

// Swap is part of sort.Interface.
func (s *endpointAddressMultiSorter) Swap(i, j int) {
	s.addresses[i], s.addresses[j] = s.addresses[j], s.addresses[i]
}

// Less is part of sort.Interface.
func (s *endpointAddressMultiSorter) Less(i, j int) bool {
	p, q := s.addresses[i], s.addresses[j]

	// Try all but the last comparison.
	var k int
	for k = 0; k < len(s.less)-1; k++ {
		less := s.less[k]
		switch {
		case less(&p, &q):
			return true
		case less(&q, &p):
			return false
		}
		// p == q; try the next comparison.
	}

	return s.less[k](&p, &q)
}

func DefaultEndpointAddressOrderByFuncs() []EndpointAddressLessFunc {
	return []EndpointAddressLessFunc{
		EndpointAddressIPLessFn,
		EndpointAddressHostnameLessFn,
	}
}

func SortAddresses(addresses []kapi.EndpointAddress, orderByFuncs []EndpointAddressLessFunc) {
	newEndpointAddressOrderBy(orderByFuncs...).Sort(addresses)
}
