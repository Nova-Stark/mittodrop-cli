package shout

import (
	"context"
	"testing"
	"time"
)

func TestEdgeShoutMessage_InvalidIP(t *testing.T) {
	msg := &ShoutMessage{
		DeviceID:     "dev1",
		DeviceName:   "name1",
		SessionID:    "sess1",
		InterfaceIP:  "999.999.999.999",
		TransferPort: 42201,
	}
	if err := msg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid IP")
	}
}

func TestEdgeShoutMessage_PortBoundaries(t *testing.T) {
	msgZero := &ShoutMessage{
		DeviceID:     "dev1",
		DeviceName:   "name1",
		SessionID:    "sess1",
		InterfaceIP:  "192.168.1.1",
		TransferPort: 0,
	}
	if err := msgZero.Validate(); err == nil {
		t.Fatal("expected error for port 0")
	}

	msgLarge := &ShoutMessage{
		DeviceID:     "dev1",
		DeviceName:   "name1",
		SessionID:    "sess1",
		InterfaceIP:  "192.168.1.1",
		TransferPort: 70000,
	}
	if err := msgLarge.Validate(); err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestEdgeReceiver_DevicePruning(t *testing.T) {
	rec, err := NewReceiver(ReceiverConfig{
		SelfDeviceID: "me",
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}

	// Inject beacon from peer
	rec.ProcessBeacon(&ShoutMessage{
		DeviceID:     "peer1",
		DeviceName:   "OldPeer",
		SessionID:    "sess-old",
		InterfaceIP:  "192.168.1.50",
		TransferPort: 42201,
	})

	if len(rec.Devices()) != 1 {
		t.Fatalf("expected 1 discovered device, got %d", len(rec.Devices()))
	}

	// Manually age the discovered device past 10s
	rec.mu.Lock()
	rec.devices[0].LastSeen = time.Now().Add(-11 * time.Second)
	rec.mu.Unlock()

	// Devices() must prune devices inactive for > 10s
	if len(rec.Devices()) != 0 {
		t.Fatalf("expected device to be pruned after 11s, got %d devices", len(rec.Devices()))
	}
}

func TestEdgeReceiver_ContextCancellation(t *testing.T) {
	rec, err := NewReceiver(ReceiverConfig{
		SelfDeviceID: "self-dev",
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = rec.Start(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
