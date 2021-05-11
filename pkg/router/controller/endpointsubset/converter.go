package endpointsubset

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// ConvertEndpointSlice converts items to a slice of EndpointSubset's.
func ConvertEndpointSlice(items []discoveryv1.EndpointSlice, addressOrderByFuncs []EndpointAddressLessFunc, portOrderByFuncs []EndpointPortLessFunc) []corev1.EndpointSubset {
	var subsets []corev1.EndpointSubset

	for i := range items {
		var ports []corev1.EndpointPort
		var addresses []corev1.EndpointAddress
		var notReadyAddresses []corev1.EndpointAddress

		for j := range items[i].Endpoints {
			for k := range items[i].Endpoints[j].Addresses {
				epa := corev1.EndpointAddress{
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
			endpointPort := corev1.EndpointPort{
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

		subsets = append(subsets, corev1.EndpointSubset{
			Addresses:         addresses,
			NotReadyAddresses: notReadyAddresses,
			Ports:             ports,
		})
	}

	return subsets
}
