package utils

import (
	"fmt"
	"net"
)


var candidatePorts = []int{
	49201, 49202, 49203, 49204, 49205,
	49206, 49207, 49208, 49209, 49210,
	49211, 49212, 49213, 49214, 49215,
}


// ip can be an IPv4 or IPv6 address string ("192.168.1.5" or "2001:db8::1").
func FindAvailablePort(ip string) (int, error) {
	for _, port := range candidatePorts {
		addr := fmt.Sprintf("%s:%d", ip, port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		ln.Close()
		return port, nil
	}
	return 0, fmt.Errorf("ports: no available port found on %s (tried %d candidates)", ip, len(candidatePorts))
}
