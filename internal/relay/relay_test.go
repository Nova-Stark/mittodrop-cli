package relay_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
)

func startTestRelay(t *testing.T, opts ...relay.ServerOption) (*relay.Server, string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv := relay.NewServer(opts...)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Wait until listening
	for i := 0; i < 50; i++ {
		if srv.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if srv.Addr() == nil {
		cancel()
		t.Fatal("relay server failed to bind in time")
	}

	return srv, srv.Addr().String(), cancel
}

func TestRelay_PairAndEcho(t *testing.T) {
	srv, addr, cancel := startTestRelay(t)
	defer func() {
		cancel()
		srv.Close()
	}()

	ctx, testCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer testCancel()

	room := "test-room-pair"
	cli := relay.NewClient()

	var wg sync.WaitGroup
	var conn1, conn2 net.Conn
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		conn1, err1 = cli.Connect(ctx, addr, "", room)
	}()
	go func() {
		defer wg.Done()
		// Small delay to ensure peer 1 enters room first
		time.Sleep(50 * time.Millisecond)
		conn2, err2 = cli.Connect(ctx, addr, "", room)
	}()
	wg.Wait()

	if err1 != nil {
		t.Fatalf("client 1 connect: %v", err1)
	}
	defer conn1.Close()

	if err2 != nil {
		t.Fatalf("client 2 connect: %v", err2)
	}
	defer conn2.Close()

	// 1. Send from conn1 -> conn2
	msg1 := []byte("hello from client 1 through relay!")
	if _, err := conn1.Write(msg1); err != nil {
		t.Fatalf("conn1.Write: %v", err)
	}

	buf2 := make([]byte, len(msg1))
	if _, err := io.ReadFull(conn2, buf2); err != nil {
		t.Fatalf("conn2.Read: %v", err)
	}
	if !bytes.Equal(buf2, msg1) {
		t.Fatalf("got %q, want %q", buf2, msg1)
	}

	// 2. Reply from conn2 -> conn1
	msg2 := []byte("ack from client 2 through relay!")
	if _, err := conn2.Write(msg2); err != nil {
		t.Fatalf("conn2.Write: %v", err)
	}

	buf1 := make([]byte, len(msg2))
	if _, err := io.ReadFull(conn1, buf1); err != nil {
		t.Fatalf("conn1.Read: %v", err)
	}
	if !bytes.Equal(buf1, msg2) {
		t.Fatalf("got %q, want %q", buf1, msg2)
	}
}

func TestRelay_WrongPassword(t *testing.T) {
	srv, addr, cancel := startTestRelay(t, relay.WithPassword("supersecret"))
	defer func() {
		cancel()
		srv.Close()
	}()

	ctx, testCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer testCancel()

	cli := relay.NewClient()
	_, err := cli.Connect(ctx, addr, "badpass", "room-pwd")
	if err == nil {
		t.Fatal("expected error with bad password, got nil")
	}
}

func TestRelay_MultipleRooms(t *testing.T) {
	srv, addr, cancel := startTestRelay(t)
	defer func() {
		cancel()
		srv.Close()
	}()

	ctx, testCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer testCancel()

	cli := relay.NewClient()

	// Room A
	var cA1, cA2 net.Conn
	var errA1, errA2 error
	var wgA sync.WaitGroup
	wgA.Add(2)
	go func() { defer wgA.Done(); cA1, errA1 = cli.Connect(ctx, addr, "", "room-alpha") }()
	go func() {
		time.Sleep(30 * time.Millisecond)
		defer wgA.Done()
		cA2, errA2 = cli.Connect(ctx, addr, "", "room-alpha")
	}()

	// Room B
	var cB1, cB2 net.Conn
	var errB1, errB2 error
	var wgB sync.WaitGroup
	wgB.Add(2)
	go func() { defer wgB.Done(); cB1, errB1 = cli.Connect(ctx, addr, "", "room-beta") }()
	go func() {
		time.Sleep(30 * time.Millisecond)
		defer wgB.Done()
		cB2, errB2 = cli.Connect(ctx, addr, "", "room-beta")
	}()

	wgA.Wait()
	wgB.Wait()

	if errA1 != nil || errA2 != nil {
		t.Fatalf("room A error: %v / %v", errA1, errA2)
	}
	defer cA1.Close()
	defer cA2.Close()

	if errB1 != nil || errB2 != nil {
		t.Fatalf("room B error: %v / %v", errB1, errB2)
	}
	defer cB1.Close()
	defer cB2.Close()

	// Send message in Room A
	cA1.Write([]byte("data-room-A"))
	bufA := make([]byte, 11)
	io.ReadFull(cA2, bufA)
	if string(bufA) != "data-room-A" {
		t.Fatalf("room A mismatch: got %q", string(bufA))
	}

	// Send message in Room B
	cB1.Write([]byte("data-room-B"))
	bufB := make([]byte, 11)
	io.ReadFull(cB2, bufB)
	if string(bufB) != "data-room-B" {
		t.Fatalf("room B mismatch: got %q", string(bufB))
	}
}

func TestRelay_EndToEndWithMittodropPAKE(t *testing.T) {
	srv, addr, cancel := startTestRelay(t)
	defer func() {
		cancel()
		srv.Close()
	}()

	ctx, testCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer testCancel()

	codephrase := "42-guitar-alaska"
	roomID := pake.RoomID(codephrase)
	cli := relay.NewClient()

	var conn1, conn2 net.Conn
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var err error
		conn1, err = cli.Connect(ctx, addr, "", roomID)
		if err != nil {
			t.Errorf("conn1 error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		var err error
		conn2, err = cli.Connect(ctx, addr, "", roomID)
		if err != nil {
			t.Errorf("conn2 error: %v", err)
		}
	}()

	wg.Wait()
	if conn1 == nil || conn2 == nil {
		t.Fatal("failed to establish relay connections")
	}
	defer conn1.Close()
	defer conn2.Close()

	// Perform SPAKE2 mutual authentication across the relayed connection
	initiator, initMsg, err := pake.NewInitiator(codephrase)
	if err != nil {
		t.Fatalf("NewInitiator: %v", err)
	}

	var responderKey, initiatorKey []byte
	var pakeWg sync.WaitGroup
	pakeWg.Add(2)

	// Initiator side (conn1)
	go func() {
		defer pakeWg.Done()
		// 1. Send initiator curve point
		if err := relay.WriteFrame(conn1, initMsg); err != nil {
			t.Errorf("send initMsg: %v", err)
			return
		}
		// 2. Receive responder curve point
		respMsg, err := relay.ReadFrame(conn1)
		if err != nil {
			t.Errorf("read respMsg: %v", err)
			return
		}
		// 3. Derive key
		k, err := initiator.Finish(respMsg)
		if err != nil {
			t.Errorf("initiator.Finish: %v", err)
			return
		}
		initiatorKey = k
	}()

	// Responder side (conn2)
	go func() {
		defer pakeWg.Done()
		// 1. Read initiator curve point
		receivedInitMsg, err := relay.ReadFrame(conn2)
		if err != nil {
			t.Errorf("read initMsg: %v", err)
			return
		}
		// 2. Initialize responder and send responder curve point
		responder, respMsg, err := pake.NewResponder(codephrase, receivedInitMsg)
		if err != nil {
			t.Errorf("NewResponder: %v", err)
			return
		}
		if err := relay.WriteFrame(conn2, respMsg); err != nil {
			t.Errorf("send respMsg: %v", err)
			return
		}
		// 3. Derive key
		k, err := responder.SessionKey()
		if err != nil {
			t.Errorf("responder.SessionKey: %v", err)
			return
		}
		responderKey = k
	}()

	pakeWg.Wait()

	if !bytes.Equal(initiatorKey, responderKey) {
		t.Fatalf("keys derived over relay do not match: %x vs %x", initiatorKey, responderKey)
	}

	// Verify authenticated encrypted data transfer using the derived PAKE key
	encData, err := relay.Encrypt(initiatorKey, []byte("super-secure-payload-across-relay"))
	if err != nil {
		t.Fatalf("encrypt payload: %v", err)
	}

	if err := relay.WriteFrame(conn1, encData); err != nil {
		t.Fatalf("send encData: %v", err)
	}

	receivedEncData, err := relay.ReadFrame(conn2)
	if err != nil {
		t.Fatalf("read encData: %v", err)
	}

	decrypted, err := relay.Decrypt(responderKey, receivedEncData)
	if err != nil {
		t.Fatalf("decrypt payload: %v", err)
	}

	if string(decrypted) != "super-secure-payload-across-relay" {
		t.Fatalf("payload mismatch: got %q", string(decrypted))
	}
}
