package tunnel_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"testing"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/tunnel"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
)

func setupTestDERP(t *testing.T) *tailcfg.DERPMap {
	t.Helper()
	return integration.RunDERPAndSTUN(t, t.Logf, "127.0.0.1")
}

func TestTunnel_BasicEcho(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing region 1 in test DERP map")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Create Server with test DERP region
	srv, err := tunnel.NewServer(
		tunnel.WithServerRegion(reg),
		tunnel.WithServerLogf(t.Logf),
	)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// 2. Start listener on port 42201
	ln, err := srv.Listen(ctx, 42201)
	if err != nil {
		t.Fatalf("srv.Listen(42201): %v", err)
	}
	defer ln.Close()

	// 3. Server accept loop: echo back input
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	// 4. Client dials server using tailcat address
	cli, err := tunnel.NewClient(srv.Addr(), tunnel.WithClientLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cli.Close()

	conn, err := cli.Dial(ctx, 42201)
	if err != nil {
		t.Fatalf("cli.Dial(42201): %v", err)
	}
	defer conn.Close()

	// 5. Verify data echo
	payload := []byte("hello from mittodrop tailcat tunnel!")
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("conn.Write: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("io.ReadFull: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("echo mismatch: got %q, want %q", buf, payload)
	}
}

func TestTunnel_PresharedKeyWithPAKE(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Simulate PAKE key exchange between sender and receiver using a 3-word phrase
	codephrase := "42-guitar-alaska"
	initiator, initMsg, err := pake.NewInitiator(codephrase)
	if err != nil {
		t.Fatalf("NewInitiator: %v", err)
	}
	responder, respMsg, err := pake.NewResponder(codephrase, initMsg)
	if err != nil {
		t.Fatalf("NewResponder: %v", err)
	}

	initKeyBytes, err := initiator.Finish(respMsg)
	if err != nil {
		t.Fatalf("initiator.Finish: %v", err)
	}
	respKeyBytes, err := responder.SessionKey()
	if err != nil {
		t.Fatalf("responder.SessionKey: %v", err)
	}

	var senderKey [32]byte
	copy(senderKey[:], initKeyBytes)
	var receiverKey [32]byte
	copy(receiverKey[:], respKeyBytes)

	if !bytes.Equal(senderKey[:], receiverKey[:]) {
		t.Fatal("derived PAKE keys do not match")
	}

	// 2. Sender sets up Server with derived 32-byte PAKE session key
	srv, err := tunnel.NewServer(
		tunnel.WithServerRegion(reg),
		tunnel.WithServerPresharedKey(senderKey),
		tunnel.WithServerLogf(t.Logf),
	)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	ln, err := srv.Listen(ctx, 42202)
	if err != nil {
		t.Fatalf("srv.Listen(42202): %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("pake-verified-stream"))
	}()

	// 3. Receiver connects using server's address (which carries matching PSK)
	cli, err := tunnel.NewClient(srv.Addr(), tunnel.WithClientLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cli.Close()

	conn, err := cli.Dial(ctx, 42202)
	if err != nil {
		t.Fatalf("cli.Dial(42202): %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 20)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("io.ReadFull: %v", err)
	}
	if string(buf) != "pake-verified-stream" {
		t.Fatalf("got %q, want 'pake-verified-stream'", string(buf))
	}

	// 4. Test client with bad PSK: must fail to exchange data
	var badKey [32]byte
	rand.Read(badKey[:])

	badAddr, err := tunnel.ApplyPresharedKey(srv.Addr(), badKey)
	if err != nil {
		t.Fatalf("ApplyPresharedKey: %v", err)
	}

	badCli, err := tunnel.NewClient(badAddr, tunnel.WithClientLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewClient(badAddr): %v", err)
	}
	defer badCli.Close()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shortCancel()

	badConn, dialErr := badCli.Dial(shortCtx, 42202)
	if dialErr == nil {
		badConn.SetDeadline(time.Now().Add(1 * time.Second))
		var b [1]byte
		_, rErr := badConn.Read(b[:])
		badConn.Close()
		if rErr == nil {
			t.Fatal("expected reading from bad PSK conn to fail, but it succeeded")
		}
	}
}

func TestTunnel_AutoPortAllocation(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	srv, err := tunnel.NewServer(
		tunnel.WithServerRegion(reg),
		tunnel.WithServerLogf(t.Logf),
	)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Port 0 auto allocates
	ln, err := srv.Listen(ctx, 0)
	if err != nil {
		t.Fatalf("srv.Listen(0): %v", err)
	}
	defer ln.Close()

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || tcpAddr.Port == 0 {
		t.Fatalf("expected allocated port > 0, got %v", ln.Addr())
	}
	allocatedPort := uint16(tcpAddr.Port)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("auto-port-ok"))
	}()

	cli, err := tunnel.NewClient(srv.Addr(), tunnel.WithClientLogf(t.Logf))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer cli.Close()

	conn, err := cli.Dial(ctx, allocatedPort)
	if err != nil {
		t.Fatalf("cli.Dial(%d): %v", allocatedPort, err)
	}
	defer conn.Close()

	buf := make([]byte, 12)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("io.ReadFull: %v", err)
	}
	if string(buf) != "auto-port-ok" {
		t.Fatalf("got %q, want 'auto-port-ok'", string(buf))
	}
}
