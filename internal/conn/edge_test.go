package conn

import (
	"context"
	"net"
	"testing"
	"time"

	"mittodrop/internal/manual"
)

func TestEdgeListen_MissingCodephrase(t *testing.T) {
	_, err := Listen(context.Background(), Config{Codephrase: ""})
	if err == nil {
		t.Fatal("expected error for empty codephrase in Listen")
	}
}

func TestEdgeConnect_MissingCodephrase(t *testing.T) {
	_, err := Connect(context.Background(), Config{Codephrase: ""})
	if err == nil {
		t.Fatal("expected error for empty codephrase in Connect")
	}
}

func TestEdgeConnect_ManualMissingTargetAddr(t *testing.T) {
	_, err := Connect(context.Background(), Config{
		Codephrase: "test-code",
		Mode:       ModeManual,
		TargetAddr: "",
	})
	if err == nil {
		t.Fatal("expected error when TargetAddr is missing in ModeManual")
	}
}

func TestEdgeConnect_RelayMissingAddress(t *testing.T) {
	_, err := Connect(context.Background(), Config{
		Codephrase: "test-code",
		Mode:       ModeRelay,
		RelayAddr:  "",
	})
	if err == nil {
		t.Fatal("expected error when RelayAddr is missing in ModeRelay")
	}
}

func TestEdgeProbeCandidates_EmptyCandidates(t *testing.T) {
	_, _, _, err := ProbeCandidates(context.Background(), nil, "test-code")
	if err == nil {
		t.Fatal("expected error when probing empty candidates")
	}
}

func TestEdgeCandidatesSerialization_RoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	candidates := []manual.Endpoint{
		{Type: "lan", Address: "192.168.1.50:42201", Description: "Local LAN"},
		{Type: "ipv6", Address: "[2001:db8::1]:42201", Description: "Global IPv6"},
		{Type: "upnp", Address: "203.0.113.195:42201", Description: "UPnP Gateway"},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- SendCandidates(c1, candidates)
	}()

	recv, err := ReceiveCandidates(c2)
	if err != nil {
		t.Fatalf("ReceiveCandidates failed: %v", err)
	}
	if sendErr := <-errCh; sendErr != nil {
		t.Fatalf("SendCandidates failed: %v", sendErr)
	}

	if len(recv) != len(candidates) {
		t.Fatalf("candidates length mismatch: %d vs %d", len(recv), len(candidates))
	}
	for i := range candidates {
		if recv[i].Type != candidates[i].Type || recv[i].Address != candidates[i].Address {
			t.Errorf("candidate %d mismatch: %+v vs %+v", i, recv[i], candidates[i])
		}
	}
}

func TestEdgeCandidatesSerialization_EmptySlice(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go func() {
		_ = SendCandidates(c1, []manual.Endpoint{})
	}()

	recv, err := ReceiveCandidates(c2)
	if err != nil {
		t.Fatalf("ReceiveCandidates on empty slice failed: %v", err)
	}
	if len(recv) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(recv))
	}
}

func TestEdgeSessionListener_CloseIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	l, err := Listen(ctx, Config{
		Codephrase: "some-test-codephrase",
		Mode:       ModeManual,
	})
	if err != nil {
		t.Skipf("cannot bind listener: %v", err)
	}

	// First close
	if err := l.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close must not panic or error
	if err := l.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestEdgeProbeCandidates_UnreachableTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	candidates := []manual.Endpoint{
		{Type: "lan", Address: "192.0.2.1:42201", Description: "Unreachable TEST-NET-1"},
	}

	_, _, _, err := ProbeCandidates(ctx, candidates, "test-phrase")
	if err == nil {
		t.Fatal("expected error probing unreachable candidate")
	}
}
