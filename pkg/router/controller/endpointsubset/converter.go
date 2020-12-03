package endpointsubset

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/discovery/v1beta1"
)

// ConvertEndpointSlice converts items to a slice of EndpointSubset's.
func ConvertEndpointSlice(items []v1beta1.EndpointSlice, addressOrderByFuncs []EndpointAddressLessFunc, portOrderByFuncs []EndpointPortLessFunc) []v1.EndpointSubset {
	var subsets []v1.EndpointSubset

	for i := range items {
		var ports []v1.EndpointPort
		var addresses []v1.EndpointAddress
		var notReadyAddresses []v1.EndpointAddress

		for j := range items[i].Endpoints {
			for k := range items[i].Endpoints[j].Addresses {
				epa := v1.EndpointAddress{
					IP:        items[i].Endpoints[j].Addresses[k],
					TargetRef: items[i].Endpoints[j].TargetRef,
				}
				if items[i].Endpoints[j].Hostname != nil {
					epa.Hostname = *items[i].Endpoints[j].Hostname
				}
				// A nil Ready condition indicates an unknown state and should be interpreted as ready.
				if items[i].Endpoints[j].Conditions.Ready != nil && !*items[i].Endpoints[j].Conditions.Ready {
					notReadyAddresses = append(notReadyAddresses, epa)
				} else {
					addresses = append(addresses, epa)
				}
			}
		}

		for j := range items[i].Ports {
			endpointPort := v1.EndpointPort{
				AppProtocol: items[i].Ports[j].AppProtocol,
			}
			if items[i].Ports[j].Name != nil {
				endpointPort.Name = *items[i].Ports[j].Name
			}
			if items[i].Ports[j].Port != nil {
				endpointPort.Port = *items[i].Ports[j].Port
			}
			if items[i].Ports[j].Protocol != nil {
				endpointPort.Protocol = *items[i].Ports[j].Protocol
			}
			ports = append(ports, endpointPort)
		}

		SortAddresses(addresses, addressOrderByFuncs)
		SortAddresses(notReadyAddresses, addressOrderByFuncs)
		SortPorts(ports, portOrderByFuncs)

		subsets = append(subsets, v1.EndpointSubset{
			Addresses:         addresses,
			NotReadyAddresses: notReadyAddresses,
			Ports:             ports,
		})
	}

	return subsets
}
