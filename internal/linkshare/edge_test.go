package linkshare

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"mittodrop/internal/utils"
)

func TestEdgeCrypto_ZeroLengthPayload(t *testing.T) {
	key := [32]byte{42}
	var cipherBuf bytes.Buffer

	w, err := NewEncryptWriter(&cipherBuf, key)
	if err != nil {
		t.Fatalf("NewEncryptWriter failed: %v", err)
	}

	// Close immediately without writing any data
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed on empty encrypt writer: %v", err)
	}

	r, err := NewDecryptReader(&cipherBuf, key)
	if err != nil {
		t.Fatalf("NewDecryptReader failed: %v", err)
	}

	var plainBuf bytes.Buffer
	n, err := io.Copy(&plainBuf, r)
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if n != 0 || plainBuf.Len() != 0 {
		t.Errorf("expected 0 bytes decrypted, got %d", n)
	}
}

func TestEdgeCrypto_DoubleClose(t *testing.T) {
	key := [32]byte{1, 2, 3}
	var buf bytes.Buffer
	w, err := NewEncryptWriter(&buf, key)
	if err != nil {
		t.Fatalf("NewEncryptWriter failed: %v", err)
	}

	_, _ = w.Write([]byte("some data"))
	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close should be safe and return nil or EOF
	_ = w.Close()
}

func TestEdgeReceiver_UnauthorizedHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec, err := NewReceiver(ReceiverConfig{
		Identity:     utils.PeerIdentity{DeviceID: "rec1", DeviceName: "ReceiverOne", SessionID: "sess1"},
		SaveDir:      t.TempDir(),
		Port:         0,
		Token:        "secret123",
		RequireToken: true,
	})
	if err != nil {
		t.Fatalf("NewReceiver failed: %v", err)
	}

	go func() {
		_ = rec.Start(ctx)
	}()

	select {
	case <-rec.Ready():
	case <-time.After(3 * time.Second):
		t.Fatal("receiver failed to start")
	}

	urls := rec.CleanURLs()
	if len(urls) == 0 {
		t.Fatal("no clean URLs reported")
	}

	// 1. Handshake without token
	reqNoToken, _ := http.NewRequestWithContext(ctx, http.MethodGet, urls[0]+"/handshake", nil)
	resp, err := http.DefaultClient.Do(reqNoToken)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 without token, got %d", resp.StatusCode)
	}

	// 2. Handshake with wrong token
	reqBadToken, _ := http.NewRequestWithContext(ctx, http.MethodGet, urls[0]+"/handshake", nil)
	reqBadToken.Header.Set("X-LinkShare-Token", "wrong-token")
	resp2, err := http.DefaultClient.Do(reqBadToken)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401 with wrong token, got %d", resp2.StatusCode)
	}
}

func TestEdgeNormalizeURL_Variations(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", false},
		{"https://127.0.0.1:8080/", "https://127.0.0.1:8080", false},
		{"127.0.0.1:8080", "http://127.0.0.1:8080", false},
		{"   http://localhost:3000   ", "http://localhost:3000", false},
		{"", "", true},
	}

	for _, tc := range cases {
		got, err := NormalizeURL(tc.in)
		if (err != nil) != tc.wantErr {
			t.Fatalf("NormalizeURL(%q) err = %v, wantErr = %v", tc.in, err, tc.wantErr)
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
