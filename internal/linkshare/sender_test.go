package linkshare

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{
			input:   "192.168.1.10:42201",
			want:    "http://192.168.1.10:42201",
			wantErr: false,
		},
		{
			input:   "http://192.168.1.10:42201/",
			want:    "http://192.168.1.10:42201",
			wantErr: false,
		},
		{
			input:   "[2001:db8::1]:42201",
			want:    "http://[2001:db8::1]:42201",
			wantErr: false,
		},
		{
			input:   "http://[2001:db8::1]:42201/upload",
			want:    "http://[2001:db8::1]:42201",
			wantErr: false,
		},
		{
			input:   "192.168.1.10",
			wantErr: true, // missing port
		},
		{
			input:   "",
			wantErr: true, // empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeURL(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConnect_Success(t *testing.T) {
	rcv, tempDir := createTestReceiver(t)
	defer os.RemoveAll(tempDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = rcv.Start(ctx)
	}()

	select {
	case <-rcv.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("receiver did not start")
	}

	rawURL := fmt.Sprintf("127.0.0.1:%d", rcv.Port())

	session, err := Connect(ctx, rawURL, ClientConfig{
		Timeout:          2 * time.Second,
		SenderDeviceID:   "sender-id-123",
		SenderDeviceName: "sender-box",
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if session.Receiver.DeviceID != "test-dev-id" {
		t.Errorf("expected DeviceID test-dev-id, got %s", session.Receiver.DeviceID)
	}
	if session.Receiver.DeviceName != "test-dev-name" {
		t.Errorf("expected DeviceName test-dev-name, got %s", session.Receiver.DeviceName)
	}
	if session.Receiver.Status != "ready" {
		t.Errorf("expected status ready, got %s", session.Receiver.Status)
	}
}

func TestConnect_Unreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Try connecting to a port where nothing is listening
	_, err := Connect(ctx, "127.0.0.1:42299", ClientConfig{Timeout: 500 * time.Millisecond})
	if err == nil {
		t.Error("expected error dialing unreachable port, got nil")
	}
}

func TestConnect_NonMittodropEndpoint(t *testing.T) {
	// Fake server that returns 404
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := Connect(ctx, ts.URL, ClientConfig{Timeout: 1 * time.Second})
	if err == nil {
		t.Error("expected error for non-mittodrop server, got nil")
	}
}

func TestNodeToNode_UploadWithZstdAndEncryption(t *testing.T) {
	var key [32]byte
	_, _ = io.ReadFull(rand.Reader, key[:])

	tempDir, err := os.MkdirTemp("", "linkshare-recv-*")
	if err != nil {
		t.Fatalf("tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	rcv, err := NewReceiver(ReceiverConfig{
		DeviceID:   "recv-id",
		DeviceName: "recv-box",
		SessionID:  "recv-sess",
		SaveDir:    tempDir,
		SessionKey: key,
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

	rawURL := fmt.Sprintf("127.0.0.1:%d", rcv.Port())
	session, err := Connect(ctx, rawURL, ClientConfig{
		Timeout:          2 * time.Second,
		SenderDeviceID:   "sender-1",
		SenderDeviceName: "sender-box",
		SessionKey:       key,
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Create test file (50 KB text file)
	sendDir, err := os.MkdirTemp("", "linkshare-send-*")
	if err != nil {
		t.Fatalf("sendDir: %v", err)
	}
	defer os.RemoveAll(sendDir)

	testContent := strings.Repeat("mittodrop high performance zstd compressed encrypted upload line\n", 800)
	srcFile := filepath.Join(sendDir, "test_document.txt")
	if err := os.WriteFile(srcFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	var progressCalled bool
	var maxBytesSent int64
	uploadRes, err := session.UploadFile(ctx, srcFile, UploadOptions{
		SessionKey: key,
		OnProgress: func(bytesSent, totalBytes int64) {
			progressCalled = true
			if bytesSent > maxBytesSent {
				maxBytesSent = bytesSent
			}
		},
	})
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}

	if uploadRes.Filename != "test_document.txt" {
		t.Errorf("expected filename test_document.txt, got %s", uploadRes.Filename)
	}
	if uploadRes.Bytes != int64(len(testContent)) {
		t.Errorf("expected %d bytes, got %d", len(testContent), uploadRes.Bytes)
	}

	if !progressCalled || maxBytesSent != int64(len(testContent)) {
		t.Errorf("onProgress failed: called=%v, maxBytes=%d, want=%d", progressCalled, maxBytesSent, len(testContent))
	}

	// Verify file content on receiver disk
	receivedPath := filepath.Join(tempDir, "test_document.txt")
	receivedData, err := os.ReadFile(receivedPath)
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	if string(receivedData) != testContent {
		t.Fatalf("received file content mismatch with original")
	}
}

func TestNodeToNode_WrongKeyRejected(t *testing.T) {
	var keyA, keyB [32]byte
	_, _ = io.ReadFull(rand.Reader, keyA[:])
	_, _ = io.ReadFull(rand.Reader, keyB[:])

	tempDir, _ := os.MkdirTemp("", "linkshare-recv-*")
	defer os.RemoveAll(tempDir)

	rcv, err := NewReceiver(ReceiverConfig{
		DeviceID:   "recv-id",
		DeviceName: "recv-box",
		SessionID:  "recv-sess",
		SaveDir:    tempDir,
		SessionKey: keyA, // Receiver configured with keyA
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

	rawURL := fmt.Sprintf("127.0.0.1:%d", rcv.Port())
	session, err := Connect(ctx, rawURL, ClientConfig{
		Timeout:          2 * time.Second,
		SenderDeviceID:   "sender-1",
		SenderDeviceName: "sender-box",
		SessionKey:       keyB, // Sender configured with wrong keyB
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	sendDir, _ := os.MkdirTemp("", "linkshare-send-*")
	defer os.RemoveAll(sendDir)

	srcFile := filepath.Join(sendDir, "secret.txt")
	_ = os.WriteFile(srcFile, []byte("classified payload"), 0644)

	_, err = session.UploadFile(ctx, srcFile, UploadOptions{
		SessionKey: keyB,
	})
	if err == nil {
		t.Fatal("expected UploadFile to fail with wrong encryption key, got nil")
	}

	// Verify no file was committed to receiver directory
	if _, err := os.Stat(filepath.Join(tempDir, "secret.txt")); err == nil {
		t.Fatal("corrupt or unauthenticated file was committed on disk")
	}
}

func TestNodeToNode_PlainUploadWithoutEncryption(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "linkshare-recv-*")
	defer os.RemoveAll(tempDir)

	rcv, err := NewReceiver(ReceiverConfig{
		DeviceID:   "recv-id",
		DeviceName: "recv-box",
		SessionID:  "recv-sess",
		SaveDir:    tempDir,
		Port:       0, // unencrypted receiver
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

	rawURL := fmt.Sprintf("127.0.0.1:%d", rcv.Port())
	session, err := Connect(ctx, rawURL, ClientConfig{
		Timeout:          2 * time.Second,
		SenderDeviceID:   "sender-1",
		SenderDeviceName: "sender-box",
	})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	sendDir, _ := os.MkdirTemp("", "linkshare-send-*")
	defer os.RemoveAll(sendDir)

	srcFile := filepath.Join(sendDir, "plain.txt")
	testData := strings.Repeat("plain unencrypted zstd stream line\n", 200)
	_ = os.WriteFile(srcFile, []byte(testData), 0644)

	res, err := session.UploadFile(ctx, srcFile, UploadOptions{})
	if err != nil {
		t.Fatalf("plain UploadFile failed: %v", err)
	}

	if res.Filename != "plain.txt" {
		t.Errorf("filename: got %s, want plain.txt", res.Filename)
	}

	saved, err := os.ReadFile(filepath.Join(tempDir, "plain.txt"))
	if err != nil {
		t.Fatalf("read saved plain file: %v", err)
	}
	if string(saved) != testData {
		t.Fatalf("saved content mismatch")
	}
}
