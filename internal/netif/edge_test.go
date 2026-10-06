package netif

import (
	"net"
	"testing"
)

func TestEdgeIPWeight(t *testing.T) {
	tests := []struct {
		name     string
		item     IPitem
		expected int
	}{
		{
			name: "IPv6 Global Unicast",
			item: IPitem{
				IP:          net.ParseIP("2607:f8b0:4005:805::200e"),
				IsIPv6:      true,
				IsLinkLocal: false,
			},
			expected: 0,
		},
		{
			name: "IPv4 Global/Private",
			item: IPitem{
				IP:          net.ParseIP("192.168.1.100"),
				IsIPv6:      false,
				IsLinkLocal: false,
			},
			expected: 1,
		},
		{
			name: "IPv6 Link Local",
			item: IPitem{
				IP:          net.ParseIP("fe80::1"),
				IsIPv6:      true,
				IsLinkLocal: true,
			},
			expected: 2,
		},
		{
			name: "IPv4 Link Local (APIPA)",
			item: IPitem{
				IP:          net.ParseIP("169.254.10.5"),
				IsIPv6:      false,
				IsLinkLocal: true,
			},
			expected: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := ipWeight(tc.item)
			if w != tc.expected {
				t.Errorf("ipWeight(%+v) = %d, expected %d", tc.item, w, tc.expected)
			}
		})
	}
}

func TestEdgeGetNetworks_DoesNotCrash(t *testing.T) {
	networks := GetNetworks()
	for _, nw := range networks {
		if nw.InterfaceName == "" {
			t.Errorf("empty interface name encountered")
		}
		for _, ip := range nw.IPtems {
			if ip.IP == nil {
				t.Errorf("nil IP found in network %s", nw.InterfaceName)
			}
			if ip.IP.IsLoopback() {
				t.Errorf("loopback IP %s was not filtered out on %s", ip.IP, nw.InterfaceName)
			}
			if ip.IP.IsUnspecified() {
				t.Errorf("unspecified IP %s was not filtered out on %s", ip.IP, nw.InterfaceName)
			}
		}
	}
}
