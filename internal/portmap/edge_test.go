package portmap

import (
	"context"
	"testing"
	"time"
)

func TestEdgeForward_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancelled immediately

	_, err := Forward(ctx, "127.0.0.1", 42201, ProtocolTCP, "cancelled-test")
	if err == nil {
		t.Error("expected error when context cancelled immediately")
	}
}

func TestEdgeForward_EmptyProtocol(t *testing.T) {
	ctx := context.Background()
	_, err := Forward(ctx, "127.0.0.1", 42201, "", "test")
	if err == nil {
		t.Error("expected error for empty protocol")
	}
}

func TestEdgeForward_NegativePort(t *testing.T) {
	ctx := context.Background()
	_, err := Forward(ctx, "127.0.0.1", -1, ProtocolTCP, "test")
	if err == nil {
		t.Error("expected error for negative port")
	}
}

func TestEdgeForward_DefaultDescription(t *testing.T) {
	// Verifies empty desc doesn't trigger a validation error before network call
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := Forward(ctx, "127.0.0.1", 42201, ProtocolUDP, "")
	// Should fail with network/gateway/timeout error, not validation error
	if err != nil && err.Error() == "portmap: invalid description" {
		t.Errorf("unexpected validation failure: %v", err)
	}
}
