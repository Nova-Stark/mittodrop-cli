package manual_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/manual"
	"mittodrop/internal/utils"
)

func TestManual_DirectConnect(t *testing.T) {
	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := manual.Listen(ctx, codephrase, 0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	if ln.Port() <= 0 {
		t.Fatalf("expected positive port, got %d", ln.Port())
	}

	targetAddr := fmt.Sprintf("127.0.0.1:%d", ln.Port())

	senderID := utils.PeerIdentity{
		DeviceID:   "device-sender-111",
		DeviceName: "SenderManualTest",
		SessionID:  "session-sender-111",
	}
	receiverID := utils.PeerIdentity{
		DeviceID:   "device-receiver-222",
		DeviceName: "ReceiverManualTest",
		SessionID:  "session-receiver-222",
	}

	var senderConn, receiverConn net.Conn
	var senderKey, receiverKey [32]byte
	var senderRemote, receiverRemote utils.PeerIdentity
	var senderErr, receiverErr error
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		senderConn, senderKey, senderRemote, senderErr = ln.Accept(ctx, senderID)
	}()

	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		receiverConn, receiverKey, receiverRemote, receiverErr = manual.Dial(ctx, targetAddr, codephrase, receiverID)
	}()

	wg.Wait()

	if senderErr != nil {
		t.Fatalf("sender accept failed: %v", senderErr)
	}
	defer senderConn.Close()

	if receiverErr != nil {
		t.Fatalf("receiver dial failed: %v", receiverErr)
	}
	defer receiverConn.Close()

	// Verify derived keys match
	if !bytes.Equal(senderKey[:], receiverKey[:]) {
		t.Fatalf("PAKE session key mismatch: %x vs %x", senderKey, receiverKey)
	}

	// Verify identity exchange
	if senderRemote.DeviceID != receiverID.DeviceID || senderRemote.SessionID != receiverID.SessionID {
		t.Fatalf("sender received wrong remote identity: %+v, want %+v", senderRemote, receiverID)
	}
	if receiverRemote.DeviceID != senderID.DeviceID || receiverRemote.SessionID != senderID.SessionID {
		t.Fatalf("receiver received wrong remote identity: %+v, want %+v", receiverRemote, senderID)
	}

	// Verify bidirectional raw data transmission
	msg := []byte("hello direct manual connection!")
	if _, err := senderConn.Write(msg); err != nil {
		t.Fatalf("sender write: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(receiverConn, buf); err != nil {
		t.Fatalf("receiver read: %v", err)
	}
	if !bytes.Equal(buf, msg) {
		t.Fatalf("got %q, want %q", buf, msg)
	}
}

func TestManual_BadCodephrase(t *testing.T) {
	correctPhrase := "42-guitar-alaska"
	wrongPhrase := "99-robot-texas"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := manual.Listen(ctx, correctPhrase, 0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	targetAddr := fmt.Sprintf("127.0.0.1:%d", ln.Port())

	var wg sync.WaitGroup
	var dialErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		conn, _, _, _ := ln.Accept(ctx)
		if conn != nil {
			conn.Close()
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(30 * time.Millisecond)
		var conn net.Conn
		conn, _, _, dialErr = manual.Dial(ctx, targetAddr, wrongPhrase)
		if conn != nil {
			conn.Close()
		}
	}()

	wg.Wait()

	if dialErr == nil {
		t.Fatal("expected error with mismatched codephrase, got nil")
	}
}

func TestManual_Endpoints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := manual.Listen(ctx, "42-guitar-alaska", 0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	endpoints := ln.Endpoints()
	t.Logf("Discovered %d endpoints for port %d:", len(endpoints), ln.Port())
	for _, ep := range endpoints {
		t.Logf("  [%s] %s (%s)", ep.Type, ep.Address, ep.Description)
	}
}

func TestManual_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln, err := manual.Listen(ctx, "42-guitar-alaska", 0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	cancel() // cancel immediately
	_, _, _, err = ln.Accept(ctx)
	if err == nil {
		t.Fatal("expected context canceled error, got nil")
	}
}
