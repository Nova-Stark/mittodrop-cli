package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
	"tailscale.com/tailcfg"
	"tailscale.com/tstest/integration"
)

func setupTestDERP(t *testing.T) *tailcfg.DERPMap {
	t.Helper()
	return integration.RunDERPAndSTUN(t, t.Logf, "127.0.0.1")
}

func TestIntegration_Tunnel_ReceiverListensSenderDials(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing region 1 in test DERP map")
	}

	codephrase := "tunnel-integ-pass-789"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "tunnel-integ-send-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "tunnel-integ-recv-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	// Create 2 MB test payload
	srcPath, srcSum := createTestPayload(t, sendDir, "tunnel_dataset.bin", 2*1024*1024)

	// 1. Receiver starts listening on ModeTunnel
	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:          conn.ModeTunnel,
		Codephrase:    codephrase,
		DERPRegion:    reg,
		PreferredPort: 42207,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-tunnel-receiver",
			DeviceName: "TunnelReceiverBox",
			SessionID:  "sess-tunnel-recv",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen receiver: %v", err)
	}
	defer receiverSession.Close()

	tunnelAddr := receiverSession.TunnelAddr()
	if tunnelAddr == "" {
		t.Fatal("expected non-empty tunnel address from receiver session")
	}

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var progressCount int
	var wg sync.WaitGroup
	wg.Add(2)

	// Receiver accepts connection and receives file over WireGuard tunnel
	go func() {
		defer wg.Done()
		recvMeta, receiverErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	// Sender dials tunnel address and transmits file over WireGuard tunnel
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		senderErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:          conn.ModeTunnel,
			TargetAddr:    tunnelAddr,
			PreferredPort: 42207,
			Codephrase:    codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-tunnel-sender",
				DeviceName: "TunnelSenderBox",
				SessionID:  "sess-tunnel-send",
			},
		}, srcPath, func(cur, tot int64, cIdx, tChunks uint64) {
			progressCount++
		})
	}()

	wg.Wait()

	if receiverErr != nil {
		t.Fatalf("receiver failed: %v", receiverErr)
	}
	if senderErr != nil {
		t.Fatalf("sender failed: %v", senderErr)
	}

	if recvMeta == nil {
		t.Fatal("nil receiver metadata")
	}
	if recvMeta.Name != "tunnel_dataset.bin" {
		t.Errorf("expected filename tunnel_dataset.bin, got %s", recvMeta.Name)
	}
	if progressCount == 0 {
		t.Errorf("expected progress events, got 0")
	}

	// Verify file integrity on disk
	destPath := filepath.Join(recvDir, "tunnel_dataset.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 hash mismatch! got %x, want %x", destSum, srcSum)
	}
}

func TestIntegration_Tunnel_SenderListensReceiverDials(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing region 1 in test DERP map")
	}

	codephrase := "tunnel-integ-pass-reverse"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "tunnel-integ-send2-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "tunnel-integ-recv2-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	srcPath, srcSum := createTestPayload(t, sendDir, "reverse_tunnel.bin", 1*1024*1024)

	// Sender starts listening on ModeTunnel
	senderSession, err := transport.Listen(ctx, conn.Config{
		Mode:          conn.ModeTunnel,
		Codephrase:    codephrase,
		DERPRegion:    reg,
		PreferredPort: 42209,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-tunnel-sender-rev",
			DeviceName: "TunnelSenderRev",
			SessionID:  "sess-tunnel-sender-rev",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen sender: %v", err)
	}
	defer senderSession.Close()

	tunnelAddr := senderSession.TunnelAddr()
	if tunnelAddr == "" {
		t.Fatal("expected non-empty tunnel address from sender session")
	}

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	// Sender accepts connection and streams file
	go func() {
		defer wg.Done()
		senderErr = senderSession.AcceptAndSend(ctx, srcPath, nil)
	}()

	// Receiver dials tunnel address and downloads file
	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		recvMeta, receiverErr = transport.ConnectAndReceive(ctx, conn.Config{
			Mode:          conn.ModeTunnel,
			TargetAddr:    tunnelAddr,
			PreferredPort: 42209,
			Codephrase:    codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-tunnel-recv-rev",
				DeviceName: "TunnelRecvRev",
				SessionID:  "sess-tunnel-recv-rev",
			},
		}, recvDir, nil)
	}()

	wg.Wait()

	if senderErr != nil {
		t.Fatalf("sender failed: %v", senderErr)
	}
	if receiverErr != nil {
		t.Fatalf("receiver failed: %v", receiverErr)
	}

	if recvMeta == nil {
		t.Fatal("nil receiver metadata")
	}

	destPath := filepath.Join(recvDir, "reverse_tunnel.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 hash mismatch! got %x, want %x", destSum, srcSum)
	}
}

func TestIntegration_Tunnel_BadCodephraseRejected(t *testing.T) {
	dm := setupTestDERP(t)
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing region 1 in test DERP map")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sendDir, _ := os.MkdirTemp("", "tunnel-integ-send3-*")
	defer os.RemoveAll(sendDir)
	srcPath, _ := createTestPayload(t, sendDir, "secret.bin", 64*1024)

	// Listener with codephrase A
	listenerSession, err := transport.Listen(ctx, conn.Config{
		Mode:          conn.ModeTunnel,
		Codephrase:    "valid-tunnel-codephrase",
		DERPRegion:    reg,
		PreferredPort: 42211,
	})
	if err != nil {
		t.Fatalf("transport.Listen: %v", err)
	}
	defer listenerSession.Close()

	tunnelAddr := listenerSession.TunnelAddr()

	var listenerErr, dialerErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		listenerErr = listenerSession.AcceptAndSend(ctx, srcPath, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(100 * time.Millisecond)
		// Dialer with wrong codephrase B
		dialerErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:          conn.ModeTunnel,
			TargetAddr:    tunnelAddr,
			PreferredPort: 42211,
			Codephrase:    "wrong-tunnel-codephrase",
		}, srcPath, nil)
	}()

	wg.Wait()

	if dialerErr == nil && listenerErr == nil {
		t.Fatal("expected failure with mismatched codephrase, but both succeeded")
	}
}
