package integration_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mittodrop/internal/linkshare"
)

func TestIntegration_LinkShare_BrowserGzipDropzone(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "linkshare-browser-integ-*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	rcv, err := linkshare.NewReceiver(linkshare.ReceiverConfig{
		DeviceID:   "browser-target-id",
		DeviceName: "browser-receiver",
		SessionID:  "browser-sess",
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

	// 1. Verify Dropzone Webpage
	rootResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer rootResp.Body.Close()

	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rootResp.StatusCode)
	}
	body, _ := io.ReadAll(rootResp.Body)
	if !strings.Contains(string(body), "browser-receiver") || !strings.Contains(string(body), "dropzone") {
		t.Fatalf("dropzone page missing expected device or container elements")
	}

	// 2. Prepare compressible payload (100 KB)
	uncompressed := strings.Repeat("mittodrop browser adaptive gzip stream test line\n", 2000)
	sum := sha256.Sum256([]byte(uncompressed))
	checksumHex := hex.EncodeToString(sum[:])

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write([]byte(uncompressed))
	_ = gw.Close()

	// 3. Post to /upload with gzip & checksum headers
	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", &gzBuf)
	if err != nil {
		t.Fatalf("new upload req: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-File-Name", "browser_report.txt")
	req.Header.Set("X-File-Size", fmt.Sprintf("%d", len(uncompressed)))
	req.Header.Set("X-File-Checksum", checksumHex)

	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("upload failed (%d): %s", uploadResp.StatusCode, string(body))
	}

	// 4. Verify file content and hash on disk
	receivedFile := filepath.Join(tempDir, "browser_report.txt")
	receivedBytes, err := os.ReadFile(receivedFile)
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	if string(receivedBytes) != uncompressed {
		t.Fatalf("disk content mismatch for browser upload")
	}
}

func TestIntegration_LinkShare_NodeToNodeEncryptedTransfer(t *testing.T) {
	var sessionKey [32]byte
	_, _ = io.ReadFull(rand.Reader, sessionKey[:])

	recvDir, err := os.MkdirTemp("", "linkshare-recv-integ-*")
	if err != nil {
		t.Fatalf("recvDir: %v", err)
	}
	defer os.RemoveAll(recvDir)

	sendDir, err := os.MkdirTemp("", "linkshare-send-integ-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	rcv, err := linkshare.NewReceiver(linkshare.ReceiverConfig{
		DeviceID:   "recv-peer-id",
		DeviceName: "recv-peer-box",
		SessionID:  "recv-sess-id",
		SaveDir:    recvDir,
		SessionKey: sessionKey,
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

	// Create 2.5 MB multi-chunk payload (mix of repetitive and variable data)
	srcPath, srcSum := createTestPayload(t, sendDir, "dataset_archive.bin", 2500*1024)

	// Sender dials receiver URL
	rawURL := fmt.Sprintf("127.0.0.1:%d", rcv.Port())
	session, err := linkshare.Connect(ctx, rawURL, linkshare.ClientConfig{
		Timeout:          3 * time.Second,
		SenderDeviceID:   "sender-peer-id",
		SenderDeviceName: "sender-peer-box",
		SessionKey:       sessionKey,
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	var progressEvents int
	var maxSent int64
	uploadRes, err := session.UploadFile(ctx, srcPath, linkshare.UploadOptions{
		SessionKey: sessionKey,
		OnProgress: func(bytesSent, totalBytes int64) {
			progressEvents++
			if bytesSent > maxSent {
				maxSent = bytesSent
			}
		},
	})
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	if uploadRes.Filename != "dataset_archive.bin" {
		t.Errorf("filename: got %s, want dataset_archive.bin", uploadRes.Filename)
	}
	if uploadRes.Bytes != 2500*1024 {
		t.Errorf("bytes: got %d, want %d", uploadRes.Bytes, 2500*1024)
	}
	if progressEvents == 0 || maxSent != 2500*1024 {
		t.Errorf("progress tracking failed: events=%d, maxSent=%d", progressEvents, maxSent)
	}

	// Verify disk integrity and whole-file hash on receiver
	destPath := filepath.Join(recvDir, "dataset_archive.bin")
	destSum := fileHash(t, destPath)
	if destSum != srcSum {
		t.Fatalf("SHA-256 hash mismatch! got %x, want %x", destSum, srcSum)
	}
}
