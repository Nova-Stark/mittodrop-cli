package manual

import (
	"context"
	"strings"
	"testing"
	"time"

	"mittodrop/internal/utils"
)

func TestEdgeManual_DialInvalidAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, _, _, err := Dial(ctx, "999.999.999.999:99999", "codephrase")
	if err == nil {
		t.Fatal("expected error dialing invalid address")
	}
}

func TestEdgeManual_DialClosedPort(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Dial port 59998 which is closed
	_, _, _, err := Dial(ctx, "127.0.0.1:59998", "codephrase")
	if err == nil {
		t.Fatal("expected connection error dialing closed port")
	}
}

func TestEdgeManual_ListenerCloseIdempotent(t *testing.T) {
	ctx := context.Background()
	ln, err := Listen(ctx, "secret-pass", 0)
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestEdgeManual_EndpointsFormatting(t *testing.T) {
	ctx := context.Background()
	ln, err := Listen(ctx, "secret-pass", 0)
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()

	eps := ln.Endpoints()
	for _, ep := range eps {
		if ep.Address == "" {
			t.Errorf("empty endpoint address: %+v", ep)
		}
		if ep.Type == "ipv6" && !strings.HasPrefix(ep.Address, "[") {
			t.Errorf("IPv6 endpoint missing bracket notation: %s", ep.Address)
		}
	}
}

func TestEdgeManual_AcceptCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := Listen(ctx, "secret-pass", 0)
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	defer ln.Close()

	cancel() // Cancel before Accept

	_, _, _, err = ln.Accept(ctx, utils.PeerIdentity{})
	if err == nil {
		t.Fatal("expected error when context is cancelled during Accept")
	}
}
