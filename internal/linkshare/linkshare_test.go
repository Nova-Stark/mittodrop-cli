package linkshare

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createTestReceiver(t *testing.T) (*Receiver, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "linkshare-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	rcv, err := NewReceiver(ReceiverConfig{
		DeviceID:   "test-dev-id",
		DeviceName: "test-dev-name",
		SessionID:  "test-sess-id",
		SaveDir:    tempDir,
		Port:       0, // auto-select
	})
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("NewReceiver failed: %v", err)
	}

	return rcv, tempDir
}

func TestResolveLinks(t *testing.T) {
	links := ResolveLinks(49201)
	if len(links) == 0 {
		t.Skip("no network interfaces available on machine")
	}

	for _, l := range links {
		if l.IsIPv6 {
			if !strings.HasPrefix(l.URL, "http://[") {
				t.Errorf("IPv6 link %q missing RFC 3986 brackets", l.URL)
			}
		} else {
			if !strings.HasPrefix(l.URL, "http://") || strings.Contains(l.URL, "[") {
				t.Errorf("IPv4 link %q malformed", l.URL)
			}
		}
		if l.IsLinkLocal {
			t.Errorf("link-local link %q should be excluded from linkshare", l.URL)
		}
		if !strings.Contains(l.URL, ":49201/") {
			t.Errorf("expected port 49201 in %q", l.URL)
		}
	}
}

func TestReceiver_HandshakeAndUpload(t *testing.T) {
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
		t.Fatal("receiver failed to start listening in time")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", rcv.Port())

	// 0. Test Webpage Render (Browser Persona GET /)
	rootResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer rootResp.Body.Close()
	if rootResp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status %d, want 200", rootResp.StatusCode)
	}
	rootBody, _ := io.ReadAll(rootResp.Body)
	if !strings.Contains(string(rootBody), "test-dev-name") || !strings.Contains(string(rootBody), "dropzone") {
		t.Errorf("GET / missing expected UI contents")
	}

	// 1. Test Handshake
	resp, err := http.Get(baseURL + "/handshake")
	if err != nil {
		t.Fatalf("GET /handshake: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handshake status %d, want 200", resp.StatusCode)
	}

	var hs HandshakeResponse
	if err := json.NewDecoder(resp.Body).Decode(&hs); err != nil {
		t.Fatalf("decode handshake: %v", err)
	}
	if hs.DeviceID != "test-dev-id" || hs.DeviceName != "test-dev-name" || hs.Status != "ready" {
		t.Errorf("unexpected handshake response: %+v", hs)
	}

	// 2. Test Raw Stream Upload (CLI Persona)
	testContent := "hello world binary payload"
	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", strings.NewReader(testContent))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-File-Name", "raw_test.txt")

	uploadResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /upload raw stream: %v", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("raw upload status %d: %s", uploadResp.StatusCode, string(body))
	}

	savedRawPath := filepath.Join(tempDir, "raw_test.txt")
	savedContent, err := os.ReadFile(savedRawPath)
	if err != nil {
		t.Fatalf("read saved raw file: %v", err)
	}
	if string(savedContent) != testContent {
		t.Errorf("content mismatch: got %q, want %q", string(savedContent), testContent)
	}

	// 3. Test Multipart Stream Upload (Browser Persona)
	var mpBuf bytes.Buffer
	mpWriter := multipart.NewWriter(&mpBuf)
	part, err := mpWriter.CreateFormFile("files", "multipart_file.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write([]byte("multipart file content"))
	mpWriter.Close()

	mpReq, err := http.NewRequest(http.MethodPost, baseURL+"/upload", &mpBuf)
	if err != nil {
		t.Fatalf("new multipart request: %v", err)
	}
	mpReq.Header.Set("Content-Type", mpWriter.FormDataContentType())

	mpResp, err := http.DefaultClient.Do(mpReq)
	if err != nil {
		t.Fatalf("POST /upload multipart: %v", err)
	}
	defer mpResp.Body.Close()

	if mpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(mpResp.Body)
		t.Fatalf("multipart upload status %d: %s", mpResp.StatusCode, string(body))
	}

	savedMpPath := filepath.Join(tempDir, "multipart_file.txt")
	savedMpContent, err := os.ReadFile(savedMpPath)
	if err != nil {
		t.Fatalf("read saved multipart file: %v", err)
	}
	if string(savedMpContent) != "multipart file content" {
		t.Errorf("content mismatch: got %q, want %q", string(savedMpContent), "multipart file content")
	}

	// 4. Test Path Traversal Protection
	travReq, _ := http.NewRequest(http.MethodPost, baseURL+"/upload", strings.NewReader("bad"))
	travReq.Header.Set("X-File-Name", "../../evil.exe")
	travResp, err := http.DefaultClient.Do(travReq)
	if err != nil {
		t.Fatalf("path traversal req: %v", err)
	}
	defer travResp.Body.Close()

	if _, err := os.Stat(filepath.Join(tempDir, "evil.exe")); err == nil {
		t.Logf("Cleaned to evil.exe inside tempDir")
	}
	// Check it was NOT created in parent directory
	parentEvil := filepath.Join(filepath.Dir(tempDir), "evil.exe")
	if _, err := os.Stat(parentEvil); err == nil {
		t.Errorf("SECURITY: path traversal escaped saveDir to %s", parentEvil)
		os.Remove(parentEvil)
	}
}

func TestReceiver_BrowserGzipAndChecksumUpload(t *testing.T) {
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
		t.Fatal("receiver failed to start listening in time")
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", rcv.Port())

	// 1. Prepare raw compressible content and calculate uncompressed SHA-256
	rawContent := strings.Repeat("mittodrop browser dropzone gzip high-speed transfer test line\n", 500)
	rawBytes := []byte(rawContent)
	sum := sha256.Sum256(rawBytes)
	checksumHex := hex.EncodeToString(sum[:])

	// Gzip compress payload
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(rawBytes)
	_ = gw.Close()

	// 2. Upload with gzip and checksum
	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", &gzBuf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-File-Name", "browser_doc.txt")
	req.Header.Set("X-File-Size", fmt.Sprintf("%d", len(rawBytes)))
	req.Header.Set("X-File-Checksum", checksumHex)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /upload: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload failed %d: %s", resp.StatusCode, string(body))
	}

	// Verify saved content on disk matches uncompressed rawBytes
	savedPath := filepath.Join(tempDir, "browser_doc.txt")
	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if !bytes.Equal(savedData, rawBytes) {
		t.Fatalf("content mismatch on disk: got %d bytes, want %d bytes", len(savedData), len(rawBytes))
	}

	// 3. Test Checksum Mismatch rejects upload
	var badGzBuf bytes.Buffer
	gw2 := gzip.NewWriter(&badGzBuf)
	_, _ = gw2.Write([]byte("tampered content"))
	_ = gw2.Close()

	badReq, _ := http.NewRequest(http.MethodPost, baseURL+"/upload", &badGzBuf)
	badReq.Header.Set("Content-Type", "application/octet-stream")
	badReq.Header.Set("Content-Encoding", "gzip")
	badReq.Header.Set("X-File-Name", "bad_doc.txt")
	badReq.Header.Set("X-File-Checksum", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff") // mismatched non-zero checksum

	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatalf("bad req: %v", err)
	}
	defer badResp.Body.Close()

	if badResp.StatusCode == http.StatusOK {
		t.Fatalf("expected error on checksum mismatch, got 200 OK")
	}

	// Ensure bad_doc.txt was NOT saved
	if _, err := os.Stat(filepath.Join(tempDir, "bad_doc.txt")); err == nil {
		t.Fatalf("bad_doc.txt was saved despite checksum mismatch")
	}
}
