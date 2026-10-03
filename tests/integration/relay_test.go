package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/relay"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

func startInProcessRelay(t *testing.T, opts ...relay.ServerOption) (*relay.Server, string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := relay.NewServer(opts...)

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

	return srv, srv.Addr().String(), cancel
}

func TestIntegration_Relay_SenderListensReceiverDials(t *testing.T) {
	srv, relayAddr, cancelRelay := startInProcessRelay(t)
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	codephrase := "relay-transfer-pass-101"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "relay-integ-send-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "relay-integ-recv-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	// Create 2 MB payload
	srcPath, srcSum := createTestPayload(t, sendDir, "relay_payload.bin", 2*1024*1024)

	// 1. Sender starts session via Relay
	senderSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeRelay,
		RelayAddr:  relayAddr,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-relay-sender",
			DeviceName: "SenderRelayBox",
			SessionID:  "sess-relay-send",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen sender: %v", err)
	}
	defer senderSession.Close()

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var progressCount int
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	// Sender waits on relay room and streams payload
	go func() {
		defer wg.Done()
		senderErr = senderSession.AcceptAndSend(ctx, srcPath, func(cur, total int64, curChunk, totalChunks uint64) {
			mu.Lock()
			progressCount++
			mu.Unlock()
		})
	}()

	// Receiver dials relay room and writes to disk via transfer.Writer
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		recvMeta, receiverErr = transport.ConnectAndReceive(ctx, conn.Config{
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
			Codephrase: codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-relay-receiver",
				DeviceName: "ReceiverRelayBox",
				SessionID:  "sess-relay-recv",
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

	mu.Lock()
	if progressCount == 0 {
		t.Error("expected sender progress callbacks to fire")
	}
	mu.Unlock()

	if recvMeta == nil {
		t.Fatal("nil receiver metadata")
	}
	if recvMeta.Name != "relay_payload.bin" {
		t.Errorf("filename: got %s, want relay_payload.bin", recvMeta.Name)
	}
	if recvMeta.Size != 2*1024*1024 {
		t.Errorf("file size: got %d, want %d", recvMeta.Size, 2*1024*1024)
	}

	destPath := filepath.Join(recvDir, "relay_payload.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 mismatch! dest=%x, src=%x", destSum, srcSum)
	}
}

func TestIntegration_Relay_ReceiverListensSenderDials(t *testing.T) {
	srv, relayAddr, cancelRelay := startInProcessRelay(t)
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	codephrase := "relay-transfer-pass-202"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "relay-integ-send2-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "relay-integ-recv2-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	srcPath, srcSum := createTestPayload(t, sendDir, "dataset_reverse.bin", 1*1024*1024)

	// 1. Receiver starts listening on Relay
	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeRelay,
		RelayAddr:  relayAddr,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-relay-rec2",
			DeviceName: "RecBox2",
			SessionID:  "sess-rec-2",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen receiver: %v", err)
	}
	defer receiverSession.Close()

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		recvMeta, receiverErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		senderErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
			Codephrase: codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-relay-send2",
				DeviceName: "SendBox2",
				SessionID:  "sess-send-2",
			},
		}, srcPath, nil)
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

	destPath := filepath.Join(recvDir, "dataset_reverse.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 mismatch! dest=%x, src=%x", destSum, srcSum)
	}
}

func TestIntegration_Relay_PasswordProtected(t *testing.T) {
	relayPassword := "super-secure-relay-pass-99"
	srv, relayAddr, cancelRelay := startInProcessRelay(t, relay.WithPassword(relayPassword))
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	codephrase := "relay-pass-auth-test"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Test failure when wrong password is supplied
	wrongCfg := conn.Config{
		Mode:          conn.ModeRelay,
		RelayAddr:     relayAddr,
		RelayPassword: "wrong-password",
		Codephrase:    codephrase,
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dialCancel()
	_, err := conn.Connect(dialCtx, wrongCfg)
	if err == nil {
		t.Fatal("expected failure when dialing relay with wrong password, but connect succeeded")
	}

	// 2. Test successful transfer when correct password is provided
	sendDir, err := os.MkdirTemp("", "relay-integ-pass-send-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "relay-integ-pass-recv-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	srcPath, srcSum := createTestPayload(t, sendDir, "authed_data.bin", 512*1024)

	senderSession, err := transport.Listen(ctx, conn.Config{
		Mode:          conn.ModeRelay,
		RelayAddr:     relayAddr,
		RelayPassword: relayPassword,
		Codephrase:    codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-relay-sender-auth",
			DeviceName: "SenderAuthBox",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen: %v", err)
	}
	defer senderSession.Close()

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		senderErr = senderSession.AcceptAndSend(ctx, srcPath, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		recvMeta, receiverErr = transport.ConnectAndReceive(ctx, conn.Config{
			Mode:          conn.ModeRelay,
			RelayAddr:     relayAddr,
			RelayPassword: relayPassword,
			Codephrase:    codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-relay-recv-auth",
				DeviceName: "RecvAuthBox",
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

	if recvMeta == nil || recvMeta.Name != "authed_data.bin" {
		t.Fatalf("unexpected receiver metadata: %+v", recvMeta)
	}

	destPath := filepath.Join(recvDir, "authed_data.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 mismatch! dest=%x, src=%x", destSum, srcSum)
	}
}

func TestIntegration_Relay_WrongCodephrase_NoPairing(t *testing.T) {
	srv, relayAddr, cancelRelay := startInProcessRelay(t)
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	// Short timeout: peers will wait in different rooms and never pair
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var sErr, rErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sl, err := conn.Listen(ctx, conn.Config{
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
			Codephrase: "codephrase-alpha",
		})
		if err != nil {
			sErr = err
			return
		}
		defer sl.Close()
		_, sErr = sl.Accept(ctx)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		_, rErr = conn.Connect(ctx, conn.Config{
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
			Codephrase: "codephrase-beta",
		})
	}()

	wg.Wait()

	// Both should fail because they never pair in the relay
	if sErr == nil && rErr == nil {
		t.Fatal("expected peers with different codephrases to fail pairing, but both succeeded")
	}
}
