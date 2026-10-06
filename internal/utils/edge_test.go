package utils

import (
	"net"
	"regexp"
	"sync"
	"testing"
)

func TestEdgeEnsureValidFillsEmpty(t *testing.T) {
	id := PeerIdentity{}
	if err := id.EnsureValid(""); err != nil {
		t.Fatal(err)
	}
	if id.DeviceID == "" || id.SessionID == "" || id.DeviceName == "" {
		t.Fatalf("fields not filled: %+v", id)
	}
	if id.DeviceName != id.DeviceID[:8] {
		t.Errorf("name should default to first 8 of device id, got %q", id.DeviceName)
	}
}

func TestEdgeEnsureValidKeepsExisting(t *testing.T) {
	id := PeerIdentity{DeviceID: "dev", DeviceName: "name", SessionID: "sess"}
	if err := id.EnsureValid("other"); err != nil {
		t.Fatal(err)
	}
	if id.DeviceID != "dev" || id.DeviceName != "name" || id.SessionID != "sess" {
		t.Errorf("existing fields modified: %+v", id)
	}
}

func TestEdgeEnsureValidDefaultName(t *testing.T) {
	id := PeerIdentity{}
	if err := id.EnsureValid("fallback"); err != nil {
		t.Fatal(err)
	}
	if id.DeviceName != "fallback" {
		t.Errorf("got %q", id.DeviceName)
	}
}

func TestEdgeEnsureValidShortDeviceIDNoName(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic with short DeviceID and empty name: %v", r)
		}
	}()
	id := PeerIdentity{DeviceID: "abc", SessionID: "s"}
	_ = id.EnsureValid("")
}

func TestEdgeEnsureValidIdempotent(t *testing.T) {
	id := PeerIdentity{}
	_ = id.EnsureValid("")
	first := id
	_ = id.EnsureValid("")
	if id != first {
		t.Errorf("second call changed identity: %+v vs %+v", first, id)
	}
}

func TestEdgeGenerateDeviceIDFormatAndStable(t *testing.T) {
	a, err := GenerateDeviceID()
	if err != nil {
		t.Skipf("no usable interface: %v", err)
	}
	b, _ := GenerateDeviceID()
	if a != b {
		t.Errorf("not stable: %s vs %s", a, b)
	}
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(a) {
		t.Errorf("bad uuid5 format: %s", a)
	}
}

func TestEdgeGenerateSessionIDUniqueUnderConcurrency(t *testing.T) {
	const n = 2000
	var mu sync.Mutex
	seen := make(map[string]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := GenerateSessionID()
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			if seen[id] {
				t.Errorf("duplicate session id %s", id)
			}
			seen[id] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func TestEdgeGenerateSessionIDFormat(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatal(err)
	}
	if id[14] != '7' {
		t.Errorf("not uuid v7: %s", id)
	}
}

func TestEdgeFindAvailablePortInvalidIP(t *testing.T) {
	if p, err := FindAvailablePort("999.999.999.999"); err == nil {
		t.Errorf("expected error, got port %d", p)
	}
}

func TestEdgeFindAvailablePortSkipsBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:42201")
	if err != nil {
		t.Skip("42201 already busy")
	}
	defer ln.Close()
	p, err := FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if p == 42201 {
		t.Error("returned busy port")
	}
}

func TestEdgeFindAvailablePortAllBusyFallsBack(t *testing.T) {
	var lns []net.Listener
	for _, p := range candidatePorts {
		ln, err := net.Listen("tcp", "127.0.0.1:"+itoa(p))
		if err != nil {
			continue
		}
		lns = append(lns, ln)
	}
	defer func() {
		for _, l := range lns {
			l.Close()
		}
	}()
	p, err := FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if p <= 0 || p > 65535 {
		t.Errorf("bad port %d", p)
	}
	for _, c := range candidatePorts {
		if c == p {
			t.Errorf("returned occupied candidate %d", p)
		}
	}
}

func TestEdgeFindAvailablePortParallel(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := FindAvailablePort("127.0.0.1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestEdgeNetworkFor(t *testing.T) {
	cases := map[string]string{
		"192.168.1.1":    "udp4",
		"239.255.77.1":   "udp4",
		"ff02::77:1":     "udp6",
		"2001:db8::1":    "udp6",
		"::ffff:1.2.3.4": "udp4",
		"":               "udp6",
		"garbage":        "udp6",
	}
	for in, want := range cases {
		if got := networkFor(in); got != want {
			t.Errorf("networkFor(%q)=%s want %s", in, got, want)
		}
	}
}

func TestEdgeMulticastGroupString(t *testing.T) {
	if got := (MulticastGroup{Address: "239.255.77.1", Port: 49250}).String(); got != "239.255.77.1:49250" {
		t.Errorf("v4: %s", got)
	}
	// IPv6 string form lacks brackets, so it is not a dialable host:port.
	g := DiscoveryGroupV6
	if _, _, err := net.SplitHostPort(g.String()); err != nil {
		t.Errorf("IPv6 group String() %q is not a valid host:port: %v", g.String(), err)
	}
}
