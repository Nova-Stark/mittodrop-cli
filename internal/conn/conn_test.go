package conn_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/manual"
	"mittodrop/internal/relay"
	"tailscale.com/tstest/integration"
)

func startTestRelay(t *testing.T) (string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := relay.NewServer()

	go func() {
		_ = srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	for i := 0; i < 50; i++ {
		if srv.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if srv.Addr() == nil {
		cancel()
		t.Fatal("test relay server failed to bind")
	}

	return srv.Addr().String(), cancel
}

func TestOrchestrator_Manual(t *testing.T) {
	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sl, err := conn.Listen(ctx, conn.Config{
		Codephrase: codephrase,
		Mode:       conn.ModeManual,
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer sl.Close()

	endpoints := sl.Endpoints()
	if len(endpoints) == 0 {
		t.Fatal("expected discovered endpoints, got 0")
	}

	// Use bound listener port with 127.0.0.1 for local test
	targetAddr := fmt.Sprintf("127.0.0.1:%d", sl.Port())

	var sConn, rConn *conn.Connection
	var sErr, rErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sConn, sErr = sl.Accept(ctx)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		rConn, rErr = conn.Connect(ctx, conn.Config{
			Codephrase: codephrase,
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
		})
	}()

	wg.Wait()

	if sErr != nil {
		t.Fatalf("sender accept failed: %v", sErr)
	}
	defer sConn.Close()

	if rErr != nil {
		t.Fatalf("receiver connect failed: %v", rErr)
	}
	defer rConn.Close()

	if !bytes.Equal(sConn.SessionKey[:], rConn.SessionKey[:]) {
		t.Fatalf("key mismatch: %x vs %x", sConn.SessionKey, rConn.SessionKey)
	}

	if sConn.Remote.DeviceID == "" || rConn.Remote.DeviceID == "" {
		t.Fatalf("remote identity missing: sender.Remote=%+v, recv.Remote=%+v", sConn.Remote, rConn.Remote)
	}
	if sConn.Remote.DeviceID != rConn.Local.DeviceID || rConn.Remote.DeviceID != sConn.Local.DeviceID {
		t.Fatalf("identity exchange mismatch: sender.Remote=%+v, recv.Local=%+v", sConn.Remote, rConn.Local)
	}

	// Echo test
	payload := []byte("hello manual orchestrator stream!")
	if _, err := sConn.Conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(rConn.Conn, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("got %q, want %q", buf, payload)
	}
}

func TestOrchestrator_RelayUpgradeDirect(t *testing.T) {
	relayAddr, cancelRelay := startTestRelay(t)
	defer cancelRelay()

	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sl, err := conn.Listen(ctx, conn.Config{
		Codephrase: codephrase,
		Mode:       conn.ModeRelay,
		RelayAddr:  relayAddr,
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer sl.Close()

	var sConn, rConn *conn.Connection
	var sErr, rErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sConn, sErr = sl.Accept(ctx)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		rConn, rErr = conn.Connect(ctx, conn.Config{
			Codephrase: codephrase,
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
		})
	}()

	wg.Wait()

	if sErr != nil {
		t.Fatalf("sender accept failed: %v", sErr)
	}
	defer sConn.Close()

	if rErr != nil {
		t.Fatalf("receiver connect failed: %v", rErr)
	}
	defer rConn.Close()

	t.Logf("Negotiated path: sender=%s, receiver=%s", sConn.PathType, rConn.PathType)

	if !bytes.Equal(sConn.SessionKey[:], rConn.SessionKey[:]) {
		t.Fatalf("key mismatch: %x vs %x", sConn.SessionKey, rConn.SessionKey)
	}

	// Echo test
	payload := []byte("hello direct p2p upgraded stream!")
	if _, err := sConn.Conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(rConn.Conn, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("got %q, want %q", buf, payload)
	}
}

func TestOrchestrator_Tunnel(t *testing.T) {
	dm := integration.RunDERPAndSTUN(t, t.Logf, "127.0.0.1")
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing test DERP region")
	}

	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sl, err := conn.Listen(ctx, conn.Config{
		Codephrase:    codephrase,
		Mode:          conn.ModeTunnel,
		DERPRegion:    reg,
		PreferredPort: 42203,
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer sl.Close()

	tunnelAddr := sl.TunnelAddr()
	if tunnelAddr == "" {
		t.Fatal("expected non-empty tunnel address")
	}

	var sConn, rConn *conn.Connection
	var sErr, rErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sConn, sErr = sl.Accept(ctx)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		rConn, rErr = conn.Connect(ctx, conn.Config{
			Codephrase:    codephrase,
			Mode:          conn.ModeTunnel,
			TargetAddr:    tunnelAddr,
			PreferredPort: 42203,
		})
	}()

	wg.Wait()

	if sErr != nil {
		t.Fatalf("sender accept failed: %v", sErr)
	}
	defer sConn.Close()

	if rErr != nil {
		t.Fatalf("receiver connect failed: %v", rErr)
	}
	defer rConn.Close()

	if !bytes.Equal(sConn.SessionKey[:], rConn.SessionKey[:]) {
		t.Fatalf("key mismatch: %x vs %x", sConn.SessionKey, rConn.SessionKey)
	}

	// Echo test
	payload := []byte("hello tunnel orchestrator stream!")
	if _, err := sConn.Conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(rConn.Conn, buf); err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if !bytes.Equal(buf, payload) {
		t.Fatalf("got %q, want %q", buf, payload)
	}
}

func TestProbeCandidates_DirectSuccess(t *testing.T) {
	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := manual.Listen(ctx, codephrase, 0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	candidates := []manual.Endpoint{
		{Type: "lan", Address: fmt.Sprintf("127.0.0.1:%d", ln.Port()), Description: "Loopback LAN"},
	}

	var acceptedConn net.Conn
	var acceptErr error
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		acceptedConn, _, _, acceptErr = ln.Accept(ctx)
	}()

	directConn, _, pathType, err := conn.ProbeCandidates(ctx, candidates, codephrase)
	if err != nil {
		t.Fatalf("ProbeCandidates failed: %v", err)
	}
	defer directConn.Close()

	wg.Wait()

	if acceptErr != nil {
		t.Fatalf("accept failed: %v", acceptErr)
	}
	defer acceptedConn.Close()

	if pathType != "direct-lan" {
		t.Fatalf("expected direct-lan, got %s", pathType)
	}
}

func extractPort(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return "42201"
}

