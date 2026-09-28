package utils

import (
	"net"
	"strings"
	"testing"
)

func TestDiscoveryGroupV4_ValidMulticastAddress(t *testing.T) {
	ip := net.ParseIP(DiscoveryGroupV4.Address)
	if ip == nil {
		t.Fatalf("failed to parse V4 group address %q", DiscoveryGroupV4.Address)
	}
	if !ip.IsMulticast() {
		t.Errorf("%q is not a multicast address", DiscoveryGroupV4.Address)
	}
	if ip.To4() == nil {
		t.Errorf("%q is not IPv4", DiscoveryGroupV4.Address)
	}
	if !strings.HasPrefix(DiscoveryGroupV4.Address, "239.") {
		t.Errorf("%q not in admin-scoped 239.x.x.x range", DiscoveryGroupV4.Address)
	}
}

func TestDiscoveryGroupV6_ValidMulticastAddress(t *testing.T) {
	ip := net.ParseIP(DiscoveryGroupV6.Address)
	if ip == nil {
		t.Fatalf("failed to parse V6 group address %q", DiscoveryGroupV6.Address)
	}
	if !ip.IsMulticast() {
		t.Errorf("%q is not a multicast address", DiscoveryGroupV6.Address)
	}
	if ip.To4() != nil {
		t.Errorf("%q expected IPv6, got IPv4-mapped", DiscoveryGroupV6.Address)
	}
	if !strings.HasPrefix(strings.ToLower(DiscoveryGroupV6.Address), "ff02::") {
		t.Errorf("%q not in link-local scope (ff02::)", DiscoveryGroupV6.Address)
	}
}

func TestDiscoveryGroups_PortInRange(t *testing.T) {
	for _, g := range []MulticastGroup{DiscoveryGroupV4, DiscoveryGroupV6} {
		if g.Port < 1024 || g.Port > 65535 {
			t.Errorf("group %q: port %d out of valid range", g.Address, g.Port)
		}
	}
}

func TestMulticastGroup_String(t *testing.T) {
	g := MulticastGroup{Address: "239.255.77.1", Port: 49250}
	want := "239.255.77.1:49250"
	if g.String() != want {
		t.Errorf("String(): want %q, got %q", want, g.String())
	}
}

// TestDiscoveryGroupV4_Joinable tries to actually join the discovery group on a
// real interface. Skips if no multicast-capable interface is found.
func TestDiscoveryGroupV4_Joinable(t *testing.T) {
	iface := pickMulticastIface(t)
	g := DiscoveryGroupV4
	addr, err := net.ResolveUDPAddr("udp4", g.String())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenMulticastUDP("udp4", iface, addr)
	if err != nil {
		t.Skipf("cannot join %s on %s: %v", g, iface.Name, err)
	}
	conn.Close()
	t.Logf("joined %s on %s", g, iface.Name)
}

func pickMulticastIface(t *testing.T) *net.Interface {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("cannot list interfaces: %v", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}
		return &iface
	}
	t.Skip("no multicast-capable non-loopback interface found")
	return nil
}
