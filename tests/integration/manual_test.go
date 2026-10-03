package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

func TestIntegration_Manual_ReceiverListensSenderDials(t *testing.T) {
	codephrase := "manual-integ-pass-123"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "manual-integ-send-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "manual-integ-recv-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	// Create 2 MB test payload
	srcPath, srcSum := createTestPayload(t, sendDir, "manual_data.bin", 2*1024*1024)

	// 1. Receiver starts manual listening session
	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeManual,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-receiver-1",
			DeviceName: "ReceiverNode",
			SessionID:  "sess-receiver-1",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen receiver: %v", err)
	}
	defer receiverSession.Close()

	port := receiverSession.Listener().Port()
	if port <= 0 {
		t.Fatalf("invalid listener port: %d", port)
	}
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var progressCount int
	var wg sync.WaitGroup
	wg.Add(2)

	// Receiver accepts and writes to disk via transfer.Writer
	go func() {
		defer wg.Done()
		recvMeta, receiverErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	// Sender dials target address and streams file via transfer.Reader
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		senderErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
			Codephrase: codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-sender-1",
				DeviceName: "SenderNode",
				SessionID:  "sess-sender-1",
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
	if recvMeta.Name != "manual_data.bin" {
		t.Errorf("expected filename manual_data.bin, got %s", recvMeta.Name)
	}
	if progressCount == 0 {
		t.Errorf("expected progress callbacks, got 0")
	}

	// Verify disk integrity
	destPath := filepath.Join(recvDir, "manual_data.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 mismatch! dest=%x, src=%x", destSum, srcSum)
	}
}

func TestIntegration_Manual_SenderListensReceiverDials(t *testing.T) {
	codephrase := "manual-integ-pass-456"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sendDir, err := os.MkdirTemp("", "manual-integ-send2-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	recvDir, err := os.MkdirTemp("", "manual-integ-recv2-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	srcPath, srcSum := createTestPayload(t, sendDir, "sender_listens.bin", 1*1024*1024)

	// Sender starts manual listening session
	senderSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeManual,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-sender-2",
			DeviceName: "SenderNode2",
			SessionID:  "sess-sender-2",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen sender: %v", err)
	}
	defer senderSession.Close()

	port := senderSession.Listener().Port()
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)

	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	// Sender waits for dialer and pushes file
	go func() {
		defer wg.Done()
		senderErr = senderSession.AcceptAndSend(ctx, srcPath, nil)
	}()

	// Receiver dials sender and downloads file
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		recvMeta, receiverErr = transport.ConnectAndReceive(ctx, conn.Config{
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
			Codephrase: codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-receiver-2",
				DeviceName: "ReceiverNode2",
				SessionID:  "sess-receiver-2",
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

	destPath := filepath.Join(recvDir, "sender_listens.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 mismatch! dest=%x, src=%x", destSum, srcSum)
	}
}

func TestIntegration_Manual_BadCodephraseRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sendDir, _ := os.MkdirTemp("", "manual-integ-send3-*")
	defer os.RemoveAll(sendDir)
	srcPath, _ := createTestPayload(t, sendDir, "secret.bin", 64*1024)

	// Listener with codephrase A
	listenerSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeManual,
		Codephrase: "secret-code-alpha",
	})
	if err != nil {
		t.Fatalf("transport.Listen: %v", err)
	}
	defer listenerSession.Close()

	port := listenerSession.Listener().Port()
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)

	var listenerErr, dialerErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		listenerErr = listenerSession.AcceptAndSend(ctx, srcPath, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		// Dialer with wrong codephrase B
		dialerErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
			Codephrase: "wrong-code-bravo",
		}, srcPath, nil)
	}()

	wg.Wait()

	// Authentication must fail
	if dialerErr == nil && listenerErr == nil {
		t.Fatal("expected authentication failure with mismatched codephrase, but both succeeded")
	}
}
