package transport_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

func createTestFile(t *testing.T, dir, name string, size int64, compressible bool) string {
	t.Helper()
	filePath := filepath.Join(dir, name)
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("create test file: %v", err)
	}
	defer f.Close()

	if compressible {
		pattern := bytes.Repeat([]byte("mittodrop-fast-file-transfer-test-pattern-1234567890\n"), 20)
		var written int64
		for written < size {
			toWrite := int64(len(pattern))
			if written+toWrite > size {
				toWrite = size - written
			}
			n, err := f.Write(pattern[:toWrite])
			if err != nil {
				t.Fatalf("write pattern: %v", err)
			}
			written += int64(n)
		}
	} else {
		if _, err := io.CopyN(f, rand.Reader, size); err != nil {
			t.Fatalf("write random data: %v", err)
		}
	}

	return filePath
}

func fileChecksum(t *testing.T, path string) [32]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open for hash: %v", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash file: %v", err)
	}
	var res [32]byte
	copy(res[:], h.Sum(nil))
	return res
}

func TestTransport_LoopbackPipe(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	fileSize := int64(2*1024*1024 + 12345) // ~2MB
	srcPath := createTestFile(t, srcDir, "video.mp4", fileSize, true)
	srcHash := fileChecksum(t, srcPath)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var sessionKey [32]byte
	_, _ = io.ReadFull(rand.Reader, sessionKey[:])

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var senderErr, receiverErr error
	var receivedMeta *transfer.FileMetadata
	var senderProgressCalls atomic.Uint64
	var receiverProgressCalls atomic.Uint64

	var wg sync.WaitGroup
	wg.Add(2)

	// Sender goroutine
	go func() {
		defer wg.Done()
		reader, err := transfer.NewReader(ctx, srcPath)
		if err != nil {
			senderErr = err
			return
		}
		defer reader.Close()

		senderErr = transport.SendFileStream(ctx, clientConn, sessionKey, reader, func(curr, tot int64, cIdx, tChunks uint64) {
			senderProgressCalls.Add(1)
		})
	}()

	// Receiver goroutine
	go func() {
		defer wg.Done()
		receivedMeta, receiverErr = transport.ReceiveFileStream(ctx, serverConn, sessionKey, dstDir, func(curr, tot int64, cIdx, tChunks uint64) {
			receiverProgressCalls.Add(1)
		})
	}()

	wg.Wait()

	if senderErr != nil {
		t.Fatalf("sender failed: %v", senderErr)
	}
	if receiverErr != nil {
		t.Fatalf("receiver failed: %v", receiverErr)
	}

	if receivedMeta == nil {
		t.Fatal("expected non-nil received metadata")
	}

	dstPath := filepath.Join(dstDir, receivedMeta.Name)
	dstHash := fileChecksum(t, dstPath)

	if !bytes.Equal(srcHash[:], dstHash[:]) {
		t.Fatalf("file checksum mismatch: src=%x, dst=%x", srcHash, dstHash)
	}

	if senderProgressCalls.Load() == 0 || receiverProgressCalls.Load() == 0 {
		t.Fatalf("expected progress callbacks: sender=%d, receiver=%d", senderProgressCalls.Load(), receiverProgressCalls.Load())
	}
}

func TestTransport_Orchestrator_ManualTCP(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	fileSize := int64(1*1024*1024 + 500)
	srcPath := createTestFile(t, srcDir, "archive.tar.gz", fileSize, false)
	srcHash := fileChecksum(t, srcPath)

	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	senderID := utils.PeerIdentity{
		DeviceID:   "dev-sender-tcp",
		DeviceName: "SenderTCP",
		SessionID:  "sess-sender-tcp",
	}
	receiverID := utils.PeerIdentity{
		DeviceID:   "dev-receiver-tcp",
		DeviceName: "ReceiverTCP",
		SessionID:  "sess-receiver-tcp",
	}

	// 1. Sender starts session listener
	session, err := transport.Listen(ctx, conn.Config{
		Codephrase: codephrase,
		Mode:       conn.ModeManual,
		Identity:   senderID,
	})
	if err != nil {
		t.Fatalf("transport.Listen: %v", err)
	}
	defer session.Close()

	endpoints := session.Listener().Endpoints()
	if len(endpoints) == 0 {
		t.Fatal("no endpoints discovered")
	}

	_, port, err := net.SplitHostPort(endpoints[0].Address)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	targetAddr := fmt.Sprintf("127.0.0.1:%s", port)

	var sErr, rErr error
	var meta *transfer.FileMetadata
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		sErr = session.AcceptAndSend(ctx, srcPath, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		meta, rErr = transport.ConnectAndReceive(ctx, conn.Config{
			Codephrase: codephrase,
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
			Identity:   receiverID,
		}, dstDir, nil)
	}()

	wg.Wait()

	if sErr != nil {
		t.Fatalf("sender failed: %v", sErr)
	}
	if rErr != nil {
		t.Fatalf("receiver failed: %v", rErr)
	}

	dstPath := filepath.Join(dstDir, meta.Name)
	dstHash := fileChecksum(t, dstPath)
	if !bytes.Equal(srcHash[:], dstHash[:]) {
		t.Fatalf("integrity checksum mismatch: src=%x, dst=%x", srcHash, dstHash)
	}
}

func TestTransport_TamperDetection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var key [32]byte
	_, _ = io.ReadFull(rand.Reader, key[:])

	senderFramer, err := transport.NewFramer(clientConn, key)
	if err != nil {
		t.Fatalf("NewFramer sender: %v", err)
	}

	receiverFramer, err := transport.NewFramer(serverConn, key)
	if err != nil {
		t.Fatalf("NewFramer receiver: %v", err)
	}

	// Normal write and read
	msg := []byte("clean payload message")
	go func() {
		_ = senderFramer.WriteFrame(transport.MsgChunk, msg)
	}()

	mType, payload, err := receiverFramer.ReadFrame()
	if err != nil {
		t.Fatalf("read clean frame: %v", err)
	}
	if mType != transport.MsgChunk || !bytes.Equal(payload, msg) {
		t.Fatalf("payload mismatch")
	}

	// Tampered frame test: write with wrong key
	var wrongKey [32]byte
	_, _ = io.ReadFull(rand.Reader, wrongKey[:])
	wrongSender, _ := transport.NewFramer(clientConn, wrongKey)

	go func() {
		_ = wrongSender.WriteFrame(transport.MsgChunk, []byte("tampered data"))
	}()

	_, _, err = receiverFramer.ReadFrame()
	if err == nil {
		t.Fatal("expected GCM auth failure on wrong key, got nil error")
	}
}

func TestTransport_EmptyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := createTestFile(t, srcDir, "empty.txt", 0, false)
	srcHash := fileChecksum(t, srcPath)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var sessionKey [32]byte
	_, _ = io.ReadFull(rand.Reader, sessionKey[:])

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var senderErr, receiverErr error
	var meta *transfer.FileMetadata
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		reader, err := transfer.NewReader(ctx, srcPath)
		if err != nil {
			senderErr = err
			return
		}
		defer reader.Close()
		senderErr = transport.SendFileStream(ctx, clientConn, sessionKey, reader, nil)
	}()

	go func() {
		defer wg.Done()
		meta, receiverErr = transport.ReceiveFileStream(ctx, serverConn, sessionKey, dstDir, nil)
	}()

	wg.Wait()

	if senderErr != nil {
		t.Fatalf("sender error: %v", senderErr)
	}
	if receiverErr != nil {
		t.Fatalf("receiver error: %v", receiverErr)
	}

	dstPath := filepath.Join(dstDir, meta.Name)
	dstHash := fileChecksum(t, dstPath)
	if !bytes.Equal(srcHash[:], dstHash[:]) {
		t.Fatalf("hash mismatch on empty file: src=%x, dst=%x", srcHash, dstHash)
	}
}

func TestTransport_ContextCancel(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcPath := createTestFile(t, srcDir, "large.bin", 5*1024*1024, false)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var sessionKey [32]byte
	_, _ = io.ReadFull(rand.Reader, sessionKey[:])

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-ctx.Done()
		clientConn.Close()
		serverConn.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		reader, _ := transfer.NewReader(ctx, srcPath)
		if reader != nil {
			defer reader.Close()
		}
		_ = transport.SendFileStream(ctx, clientConn, sessionKey, reader, func(curr, tot int64, cIdx, tChunks uint64) {
			if cIdx >= 1 {
				cancel() // cancel mid-transfer
			}
		})
	}()

	go func() {
		defer wg.Done()
		_, _ = transport.ReceiveFileStream(ctx, serverConn, sessionKey, dstDir, nil)
	}()

	wg.Wait()
}
