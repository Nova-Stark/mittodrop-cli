package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
)

func TestEdgeTunnel_ApplyPresharedKey(t *testing.T) {
	nodeKey := key.NewNode()
	ci := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: nodeKey.Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(nodeKey),
		RegionID:          1,
	}
	addr := ci.Addr()
	psk := [32]byte{1, 2, 3, 4, 5}

	addrWithKey, err := ApplyPresharedKey(addr, psk)
	if err != nil {
		t.Fatalf("ApplyPresharedKey failed: %v", err)
	}

	if string(addrWithKey) == "" {
		t.Error("empty addr string after applying PSK")
	}

	parsed, err := tailcat.ParseAddr(addrWithKey)
	if err != nil {
		t.Fatalf("ParseAddr failed: %v", err)
	}
	if [32]byte(parsed.PresharedKey) != psk {
		t.Errorf("psk mismatch: got %v, want %v", parsed.PresharedKey, psk)
	}
}

func TestEdgeTunnel_ClientCloseIdempotent(t *testing.T) {
	nodeKey := key.NewNode()
	ci := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: nodeKey.Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(nodeKey),
		RegionID:          1,
	}
	cli, err := NewClient(ci.Addr())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	// First close
	if err := cli.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close
	if err := cli.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestEdgeTunnel_ServerCloseIdempotent(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestEdgeTunnel_DialUnmappedPort(t *testing.T) {
	nodeKey := key.NewNode()
	ci := tailcat.ConnInfo{
		ServerPublic:      tailcat.NodePublic{NodePublic: nodeKey.Public()},
		ServerDiscoPublic: tailcat.DiscoPublicForNode(nodeKey),
		RegionID:          1,
	}
	cli, err := NewClient(ci.Addr())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Port 59999 has no active listener on the tunnel
	_, err = cli.Dial(ctx, 59999)
	if err == nil {
		t.Fatal("expected error dialing non-listening virtual port, got nil")
	}
}
