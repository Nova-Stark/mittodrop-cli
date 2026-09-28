package utils

import (
	"fmt"
	"net"
	"testing"
)

func TestFindAvailablePort_ReturnsValidPort(t *testing.T) {
	port, err := FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("expected available port, got error: %v", err)
	}
	found := false
	for _, p := range candidatePorts {
		if p == port {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("returned port %d not in candidatePorts", port)
	}
}

func TestFindAvailablePort_PortIsBindable(t *testing.T) {
	port, err := FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Errorf("port %d reported free but bind failed: %v", port, err)
		return
	}
	ln.Close()
}

func TestFindAvailablePort_SkipsTakenPort(t *testing.T) {
	first := candidatePorts[0]
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", first))
	if err != nil {
		t.Skipf("cannot bind port %d for test setup: %v", first, err)
	}
	defer ln.Close()

	port, err := FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("expected fallback port, got error: %v", err)
	}
	if port == first {
		t.Errorf("returned taken port %d, expected a different one", first)
	}
}

func TestFindAvailablePort_InvalidIP(t *testing.T) {
	_, err := FindAvailablePort("999.999.999.999")
	if err == nil {
		t.Error("expected error for invalid IP, got nil")
	}
}
