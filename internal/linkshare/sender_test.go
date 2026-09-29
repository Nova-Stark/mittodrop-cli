package linkshare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
