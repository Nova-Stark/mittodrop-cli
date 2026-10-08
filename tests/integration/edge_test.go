package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/linkshare"
	"mittodrop/internal/manual"
	"mittodrop/internal/relay"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

func TestEdgeIntegration_LinkShare_ZeroByteUpload(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "linkshare-edge-zerobyte-*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	rcv, err := linkshare.NewReceiver(linkshare.ReceiverConfig{
		DeviceID:   "zero-byte-target",
		DeviceName: "zero-receiver",
		SessionID:  "zero-sess",
		SaveDir:    tempDir,
		Port:       0,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = rcv.Start(ctx) }()

	select {
	case <-rcv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("receiver failed to start")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", rcv.Port())

	// Upload a 0-byte file
	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("new req: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-File-Name", "empty.txt")
	req.Header.Set("X-File-Size", "0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload empty file request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for 0-byte upload, got %d", resp.StatusCode)
	}

	savedPath := filepath.Join(tempDir, "empty.txt")
	info, err := os.Stat(savedPath)
	if err != nil {
		t.Fatalf("empty file not found on disk: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected size 0, got %d", info.Size())
	}
}

func TestEdgeIntegration_LinkShare_TokenEnforcement(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "linkshare-edge-token-*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	secretToken := "top-secret-token-777"
	rcv, err := linkshare.NewReceiver(linkshare.ReceiverConfig{
		DeviceID:     "token-enforce-dev",
		DeviceName:   "token-receiver",
		SaveDir:      tempDir,
		Port:         0,
		Token:        secretToken,
		RequireToken: true,
	})
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = rcv.Start(ctx) }()

	select {
	case <-rcv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("receiver failed to start")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", rcv.Port())

	// 1. Upload without token -> must be 401 Unauthorized
	req1, _ := http.NewRequest(http.MethodPost, baseURL+"/upload", bytes.NewReader([]byte("data")))
	req1.Header.Set("X-File-Name", "file1.txt")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("req1: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", resp1.StatusCode)
	}

	// 2. Upload with invalid token -> must be 401 Unauthorized
	req2, _ := http.NewRequest(http.MethodPost, baseURL+"/upload?token=wrong-token", bytes.NewReader([]byte("data")))
	req2.Header.Set("X-File-Name", "file2.txt")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("req2: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong token, got %d", resp2.StatusCode)
	}

	// 3. Upload with correct token in header -> must succeed 200 OK
	req3, _ := http.NewRequest(http.MethodPost, baseURL+"/upload", bytes.NewReader([]byte("valid-data")))
	req3.Header.Set("X-File-Name", "file3.txt")
	req3.Header.Set("X-LinkShare-Token", secretToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("req3: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK with valid X-Token, got %d", resp3.StatusCode)
	}
}

func TestEdgeIntegration_Manual_CandidateFallback(t *testing.T) {
	codephrase := "candidate-fallback-pass"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sendDir := t.TempDir()
	recvDir := t.TempDir()

	srcPath, srcSum := createTestPayload(t, sendDir, "candidate_data.bin", 64*1024)

	// Receiver listens on Manual mode
	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeManual,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "recv-candidate-dev",
			DeviceName: "RecvCandidate",
		},
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer receiverSession.Close()

	validPort := receiverSession.Listener().Port()

	var wg sync.WaitGroup
	var recvMeta *transfer.FileMetadata
	var recvErr, sendErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		recvMeta, recvErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		// Provide a dead port first, followed by the valid working port
		deadEndpoint := manual.Endpoint{
			Address:     "127.0.0.1:59997",
			Type:        "lan",
			Description: "Dead Port",
		}
		workingEndpoint := manual.Endpoint{
			Address:     fmt.Sprintf("127.0.0.1:%d", validPort),
			Type:        "lan",
			Description: "Working Port",
		}

		senderID := utils.PeerIdentity{
			DeviceID:   "sender-candidate-dev",
			DeviceName: "SenderCandidate",
		}

		// Probe candidates
		c, k, _, probeErr := conn.ProbeCandidates(ctx, []manual.Endpoint{deadEndpoint, workingEndpoint}, codephrase, senderID)
		if probeErr != nil {
			sendErr = fmt.Errorf("probe failed: %w", probeErr)
			return
		}
		defer c.Close()

		reader, rErr := transfer.NewReader(ctx, srcPath)
		if rErr != nil {
			sendErr = rErr
			return
		}
		defer reader.Close()

		sendErr = transport.SendFileStream(ctx, c, k, reader, nil)
	}()

	wg.Wait()

	if recvErr != nil {
		t.Fatalf("receive failed: %v", recvErr)
	}
	if sendErr != nil {
		t.Fatalf("send failed: %v", sendErr)
	}

	if recvMeta == nil || recvMeta.Name != "candidate_data.bin" {
		t.Fatalf("metadata mismatch: %+v", recvMeta)
	}

	destPath := filepath.Join(recvDir, "candidate_data.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("checksum mismatch! got %x, want %x", destSum, srcSum)
	}
}

func TestEdgeIntegration_Manual_ZeroByteFile(t *testing.T) {
	codephrase := "manual-zero-byte-pass"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sendDir := t.TempDir()
	recvDir := t.TempDir()

	emptyPath := filepath.Join(sendDir, "zero.txt")
	if err := os.WriteFile(emptyPath, []byte{}, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeManual,
		Codephrase: codephrase,
	})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer receiverSession.Close()

	port := receiverSession.Listener().Port()
	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)

	var wg sync.WaitGroup
	var recvMeta *transfer.FileMetadata
	var recvErr, sendErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		recvMeta, recvErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		sendErr = transport.ConnectAndSend(ctx, conn.Config{
			Mode:       conn.ModeManual,
			TargetAddr: targetAddr,
			Codephrase: codephrase,
		}, emptyPath, nil)
	}()

	wg.Wait()

	if recvErr != nil {
		t.Fatalf("recv failed: %v", recvErr)
	}
	if sendErr != nil {
		t.Fatalf("send failed: %v", sendErr)
	}

	if recvMeta == nil || recvMeta.Size != 0 {
		t.Fatalf("expected 0 bytes, got: %+v", recvMeta)
	}

	destInfo, err := os.Stat(filepath.Join(recvDir, "zero.txt"))
	if err != nil {
		t.Fatalf("stat dest file: %v", err)
	}
	if destInfo.Size() != 0 {
		t.Errorf("expected size 0, got %d", destInfo.Size())
	}
}

func TestEdgeIntegration_Relay_MidstreamPeerDisconnect(t *testing.T) {
	srv, relayAddr, cancelRelay := startInProcessRelay(t)
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli1 := relay.NewClient()
	cli2 := relay.NewClient()

	roomID := "disconnect-test-room"

	var conn1, conn2 net.Conn
	var err1, err2 error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		conn1, err1 = cli1.Connect(ctx, relayAddr, "", roomID)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		conn2, err2 = cli2.Connect(ctx, relayAddr, "", roomID)
	}()

	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("connection to relay room failed: err1=%v, err2=%v", err1, err2)
	}
	defer conn1.Close()

	// Abruptly close conn2 mid-stream
	_ = conn2.Close()

	// Writing from conn1 or reading should detect closure within 3 seconds
	_ = conn1.SetDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 100)
	_, readErr := conn1.Read(buf)
	if readErr == nil {
		// Try writing
		_, writeErr := conn1.Write([]byte("ping"))
		if writeErr == nil {
			t.Fatal("expected read or write error after peer disconnected, got nil")
		}
	}
}

func TestEdgeIntegration_DirectoryStreamOverRelay(t *testing.T) {
	srv, relayAddr, cancelRelay := startInProcessRelay(t)
	defer func() {
		cancelRelay()
		srv.Close()
	}()

	codephrase := "dir-relay-integ-pass"
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	sendDir := t.TempDir()
	recvDir := t.TempDir()

	testFolder := filepath.Join(sendDir, "sample_dir")
	_ = os.MkdirAll(filepath.Join(testFolder, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(testFolder, "sub", "doc.txt"), []byte("folder relay test stream"), 0644)

	// Receiver starts session via Relay
	receiverSession, err := transport.Listen(ctx, conn.Config{
		Mode:       conn.ModeRelay,
		RelayAddr:  relayAddr,
		Codephrase: codephrase,
		Identity: utils.PeerIdentity{
			DeviceID:   "dev-relay-rec-dir",
			DeviceName: "RecvRelayDirBox",
		},
	})
	if err != nil {
		t.Fatalf("transport.Listen receiver: %v", err)
	}
	defer receiverSession.Close()

	var wg sync.WaitGroup
	var recvMeta *transfer.FileMetadata
	var senderErr, receiverErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		recvMeta, receiverErr = receiverSession.AcceptAndReceive(ctx, recvDir, nil)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)

		c, err := conn.Connect(ctx, conn.Config{
			Mode:       conn.ModeRelay,
			RelayAddr:  relayAddr,
			Codephrase: codephrase,
			Identity: utils.PeerIdentity{
				DeviceID:   "dev-relay-send-dir",
				DeviceName: "SenderRelayDirBox",
			},
		})
		if err != nil {
			senderErr = fmt.Errorf("connect relay: %w", err)
			return
		}
		defer c.Close()

		dr, err := transfer.NewDirReader(ctx, testFolder)
		if err != nil {
			senderErr = fmt.Errorf("new dir reader: %w", err)
			return
		}
		defer dr.Close()

		senderErr = transport.SendFileStream(ctx, c.Conn, c.SessionKey, dr.Reader, nil)
	}()

	wg.Wait()

	if receiverErr != nil {
		t.Fatalf("receiver failed: %v", receiverErr)
	}
	if senderErr != nil {
		t.Fatalf("sender failed: %v", senderErr)
	}

	if recvMeta == nil || !recvMeta.IsDir {
		t.Fatalf("expected IsDir metadata on receiver: %+v", recvMeta)
	}

	// Verify extracted file content
	extractedData, err := os.ReadFile(filepath.Join(recvDir, "sample_dir", "sub", "doc.txt"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(extractedData) != "folder relay test stream" {
		t.Errorf("content mismatch: got %q", string(extractedData))
	}
}

