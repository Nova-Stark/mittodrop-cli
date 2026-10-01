package tunnel_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"mittodrop/internal/tunnel"
)

func TestTunnel_LivePublicDERP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live DERP network test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// 1. Start Server without region override (uses live public Tailscale DERP relays)
	srv, err := tunnel.NewServer(tunnel.WithServerLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	ln, err := srv.Listen(ctx, 0)
	if err != nil {
		t.Fatalf("srv.Listen: %v", err)
	}
	defer ln.Close()

	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	publicAddr := srv.Addr()
	t.Logf("Live Server Tailcat Address: %s", publicAddr)

	// Echo goroutine
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()

	// 2. Connect client using public Tailcat address across real internet DERP
	cli, err := tunnel.NewClient(publicAddr, tunnel.WithClientLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cli.Close()

	t.Log("Dialing server through public Tailscale DERP network...")
	conn, err := cli.Dial(ctx, port)
	if err != nil {
		t.Fatalf("cli.Dial: %v", err)
	}
	defer conn.Close()

	payload := []byte("hello across live Tailscale public DERP!")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("data mismatch: got %q, want %q", buf, payload)
	}

	t.Logf("Successfully exchanged data across live public DERP: %s", string(buf))

	// Test ping round trip through public DERP
	pingRes, err := cli.Ping(ctx)
	if err == nil {
		t.Logf("Public DERP Ping latency: %v", pingRes.Latency)
	}
}
