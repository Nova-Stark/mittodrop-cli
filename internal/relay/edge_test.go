package relay

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func TestEdgeRelay_EmptyPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, []byte{}); err != nil {
		t.Fatalf("WriteFrame empty payload: %v", err)
	}

	payload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame empty payload: %v", err)
	}
	if len(payload) != 0 {
		t.Errorf("expected 0 bytes, got %d", len(payload))
	}
}

func TestEdgeRelay_BadMagicBytes(t *testing.T) {
	badHeader := []byte("fake\x00\x00\x00\x04test")
	_, err := ReadFrame(bytes.NewReader(badHeader))
	if err == nil {
		t.Fatal("expected error on invalid magic header, got nil")
	}
}

func TestEdgeRelay_OversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	oversized := make([]byte, MaxMessageSize+1)
	if err := WriteFrame(&buf, oversized); err == nil {
		t.Fatal("expected error writing oversized frame, got nil")
	}
}

func TestEdgeRelay_PingPong(t *testing.T) {
	srv := NewServer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	go func() {
		_ = srv.Serve(ctx, ln)
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send raw ping frame
	if err := WriteFrame(conn, []byte("ping")); err != nil {
		t.Fatalf("send ping: %v", err)
	}

	resp, err := ReadFrame(conn)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if string(resp) != "pong" {
		t.Errorf("expected pong response, got %q", string(resp))
	}
}

func TestEdgeRelay_ClientConnectValidation(t *testing.T) {
	cli := NewClient()
	ctx := context.Background()

	// Empty relay address
	_, err := cli.Connect(ctx, "", "pass", "room")
	if err == nil {
		t.Fatal("expected error for empty relay address")
	}

	// Empty room
	_, err = cli.Connect(ctx, "127.0.0.1:9007", "pass", "")
	if err == nil {
		t.Fatal("expected error for empty room ID")
	}
}

func TestEdgeRelay_PipeClosesBothEnds(t *testing.T) {
	c1, c2 := net.Pipe()
	c3, c4 := net.Pipe()

	done := make(chan struct{})
	go func() {
		Pipe(c2, c3)
		close(done)
	}()

	// Closing c1 should cascade through Pipe and close c4
	_ = c1.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Pipe did not terminate when endpoint closed")
	}

	_ = c4.Close()
}
