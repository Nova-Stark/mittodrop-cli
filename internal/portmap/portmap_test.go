package portmap

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestOutboundIP(t *testing.T) {
	ip, err := outboundIP()
	if err != nil {
		t.Skipf("cannot detect outbound IP (no internet route): %v", err)
	}
	if parsed := net.ParseIP(ip); parsed == nil {
		t.Errorf("outboundIP() returned invalid IP: %q", ip)
	}
	t.Logf("Detected outbound LAN IP: %s", ip)
}

func TestForward_Validation(t *testing.T) {
	ctx := context.Background()

	// Invalid port 0
	_, err := Forward(ctx, "127.0.0.1", 0, ProtocolTCP, "test")
	if err == nil {
		t.Error("expected error for port 0")
	}

	// Invalid port > 65535
	_, err = Forward(ctx, "127.0.0.1", 70000, ProtocolTCP, "test")
	if err == nil {
		t.Error("expected error for port > 65535")
	}

	// Invalid protocol
	_, err = Forward(ctx, "127.0.0.1", 42201, "SCTP", "test")
	if err == nil {
		t.Error("expected error for unsupported protocol")
	}
}

func TestForward_ShortTimeoutGraceful(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Probes local network for UPnP gateway; returns mapping if router supports UPnP,
	// or ErrNoGateway if disabled or absent. Must never hang or panic.
	mapping, err := Forward(ctx, "", 42201, ProtocolTCP, "mittodrop-test")
	if err != nil {
		if err != ErrNoGateway && ctx.Err() == nil {
			t.Logf("UPnP not available or refused on this router: %v (expected on restricted networks)", err)
		} else {
			t.Logf("No UPnP gateway found within timeout (normal behavior)")
		}
		return
	}

	defer mapping.Release()
	t.Logf("UPnP Port Mapping Successful! WAN IP: %s, Port: %d", mapping.ExternalIP, mapping.ExternalPort)
	if mapping.ExternalIP == "" || mapping.ExternalPort != 42201 {
		t.Errorf("unexpected mapping result: %+v", mapping)
	}
}
