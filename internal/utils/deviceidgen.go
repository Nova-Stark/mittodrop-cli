package utils

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"runtime"
)

// from MAC address, hostname, and OS. Output is a UUID v5-style string
func GenerateDeviceID() (string, error) {
	mac, err := primaryMAC()
	if err != nil {
		return "", fmt.Errorf("deviceid: get MAC: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("deviceid: get hostname: %w", err)
	}

	combined := mac + "|" + hostname + "|" + runtime.GOOS

	hash := sha256.Sum256([]byte(combined))

	hash[6] = (hash[6] & 0x0f) | 0x50
	hash[8] = (hash[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		hash[0:4],
		hash[4:6],
		hash[6:8],
		hash[8:10],
		hash[10:16],
	), nil
}

func primaryMAC() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}
		return iface.HardwareAddr.String(), nil
	}

	return "", fmt.Errorf("no suitable network interface found")
}
