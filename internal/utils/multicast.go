package utils

import (
	"fmt"
	"net"
)

type MulticastGroup struct {
	Address string
	Port    int
}

func (g MulticastGroup) String() string {
	return fmt.Sprintf("%s:%d", g.Address, g.Port)
}

var DiscoveryGroupV4 = MulticastGroup{Address: "239.255.77.1", Port: 49250}

var DiscoveryGroupV6 = MulticastGroup{Address: "ff02::77:1", Port: 49250}

func networkFor(address string) string {
	if net.ParseIP(address).To4() != nil {
		return "udp4"
	}
	return "udp6"
}
