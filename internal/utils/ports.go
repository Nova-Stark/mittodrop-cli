package utils

import (
	"fmt"
	"net"
)

// candidatePorts is a predefined set of ports reserved for mittodrop file transfer.
// Chosen in the 42201-42215 range, well below Windows dynamic/Hyper-V exclusion
// range (>= 49152) and unlikely to collide with common services.
var candidatePorts = []int{
	42201, 42202, 42203, 42204, 42205,
	42206, 42207, 42208, 42209, 42210,
	42211, 42212, 42213, 42214, 42215,
}

// FindAvailablePort tries each candidate port on the given IP address and
// returns the first one that is free for TCP binding.
// If all candidate ports are busy/restricted, it falls back to an OS-assigned
// free ephemeral port (:0).
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

	// Fallback to ephemeral port
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:0", ip))
	if err != nil {
		return 0, fmt.Errorf("ports: no available port found on %s: %w", ip, err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("ports: unable to resolve TCPAddr on %s", ip)
	}
	return tcpAddr.Port, nil
}
