package endpointsubset

import (
	"sort"
	"strings"

	kapi "k8s.io/api/core/v1"
)

type EndpointPortCmpFunc func(x, y *kapi.EndpointPort) int
type EndpointPortLessFunc func(x, y *kapi.EndpointPort) bool

type endpointPortMultiSorter struct {
	ports []kapi.EndpointPort
	less  []EndpointPortLessFunc
}

var _ sort.Interface = &endpointPortMultiSorter{}

var (
	EndpointPortNameCmpFn = func(x, y *kapi.EndpointPort) int {
		return strings.Compare(x.Name, y.Name)
	}

	EndpointPortNameLessFn = func(x, y *kapi.EndpointPort) bool {
		return EndpointPortNameCmpFn(x, y) < 0
	}

	EndpointPortNumberCmpFn = func(x, y *kapi.EndpointPort) int {
		return int(x.Port - y.Port)
	}

	EndpointPortPortNumberLessFn = func(x, y *kapi.EndpointPort) bool {
		return EndpointPortNumberCmpFn(x, y) < 0
	}

	EndpointPortProtocolCmpFn = func(x, y *kapi.EndpointPort) int {
		return strings.Compare(string(x.Protocol), string(y.Protocol))
	}

	EndpointPortProtocolLessFn = func(x, y *kapi.EndpointPort) bool {
		return EndpointPortProtocolCmpFn(x, y) < 0
	}
)

// Sort sorts the argument slice according to the comparator functions
// passed to orderBy.
func (s *endpointPortMultiSorter) Sort(ports []kapi.EndpointPort) {
	s.ports = ports
	sort.Sort(s)
}

// endpointPortOrderBy returns a Sorter that sorts using a number
// of comparator functions.
func endpointPortOrderBy(less ...EndpointPortLessFunc) *endpointPortMultiSorter {
	return &endpointPortMultiSorter{
		less: less,
	}
}

// Len is part of sort.Interface.
func (s *endpointPortMultiSorter) Len() int {
	return len(s.ports)
}

// Swap is part of sort.Interface.
func (s *endpointPortMultiSorter) Swap(i, j int) {
	s.ports[i], s.ports[j] = s.ports[j], s.ports[i]
}

// Less is part of sort.Interface.
func (s *endpointPortMultiSorter) Less(i, j int) bool {
	p, q := s.ports[i], s.ports[j]

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

func DefaultEndpointPortOrderByFuncs() []EndpointPortLessFunc {
	return []EndpointPortLessFunc{
		EndpointPortPortNumberLessFn,
		EndpointPortProtocolLessFn,
		EndpointPortNameLessFn,
	}
}

func SortPorts(ports []kapi.EndpointPort, orderByFuncs []EndpointPortLessFunc) {
	endpointPortOrderBy(orderByFuncs...).Sort(ports)
}
