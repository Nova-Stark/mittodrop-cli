package shout

import (
	"testing"
	"time"
)

func TestNewReceiver_Validation(t *testing.T) {
	_, err := NewReceiver(ReceiverConfig{})
	if err == nil {
		t.Error("expected error with empty SelfDeviceID, got nil")
	}

	r, err := NewReceiver(ReceiverConfig{SelfDeviceID: "my-dev"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.Devices()) != 0 {
		t.Errorf("expected empty devices slice initially, got %d", len(r.Devices()))
	}
}

func TestReceiver_IgnoresSelf(t *testing.T) {
	r, _ := NewReceiver(ReceiverConfig{SelfDeviceID: "self-uuid"})

	r.handleMessage(&ShoutMessage{
		DeviceID:     "self-uuid",
		DeviceName:   "my-own-device",
		SessionID:    "sess-1",
		InterfaceIP:  "192.168.1.5",
		TransferPort: 49201,
	})

	if len(r.Devices()) != 0 {
		t.Errorf("expected 0 devices (self beacon should be ignored), got %d", len(r.Devices()))
	}
}

func TestReceiver_SingleEntryPerDevice(t *testing.T) {
	r, _ := NewReceiver(ReceiverConfig{SelfDeviceID: "self-uuid"})

	peerMsg := &ShoutMessage{
		DeviceID:     "peer-uuid-1",
		DeviceName:   "peer-device",
		SessionID:    "sess-peer",
		InterfaceIP:  "192.168.1.100",
		TransferPort: 49201,
	}

	// Deliver 5 times
	for i := 0; i < 5; i++ {
		r.handleMessage(peerMsg)
	}

	devices := r.Devices()
	if len(devices) != 1 {
		t.Fatalf("expected exactly 1 entry in slice, got %d", len(devices))
	}
	if devices[0].DeviceID != "peer-uuid-1" {
		t.Errorf("expected peer-uuid-1, got %s", devices[0].DeviceID)
	}
}

func TestReceiver_IPv6PriorityPolicy(t *testing.T) {
	r, _ := NewReceiver(ReceiverConfig{SelfDeviceID: "self-uuid"})

	v4Msg := &ShoutMessage{
		DeviceID:     "peer-uuid-1",
		DeviceName:   "peer-device",
		SessionID:    "sess-1",
		InterfaceIP:  "192.168.1.50",
		TransferPort: 49201,
	}

	v6Msg := &ShoutMessage{
		DeviceID:     "peer-uuid-1",
		DeviceName:   "peer-device",
		SessionID:    "sess-1",
		InterfaceIP:  "fe80::1ff:fe00:3a60",
		TransferPort: 49201,
	}

	// 1. Send IPv4 first
	r.handleMessage(v4Msg)
	devs := r.Devices()
	if len(devs) != 1 || devs[0].IsIPv6 || devs[0].InterfaceIP != "192.168.1.50" {
		t.Fatalf("expected IPv4 entry, got %+v", devs)
	}

	// 2. Send IPv6 -> must UPGRADE entry to IPv6
	r.handleMessage(v6Msg)
	devs = r.Devices()
	if len(devs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(devs))
	}
	if !devs[0].IsIPv6 || devs[0].InterfaceIP != "fe80::1ff:fe00:3a60" {
		t.Fatalf("expected entry upgraded to IPv6, got %+v", devs[0])
	}

	// 3. Send IPv4 again -> must KEEP IPv6
	time.Sleep(10 * time.Millisecond)
	r.handleMessage(v4Msg)
	devs = r.Devices()
	if len(devs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(devs))
	}
	if !devs[0].IsIPv6 || devs[0].InterfaceIP != "fe80::1ff:fe00:3a60" {
		t.Errorf("expected IPv6 preserved over subsequent IPv4, got %+v", devs[0])
	}
}
