package manual

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
)

// Dial connects to a remote sender at targetAddr and executes mutual SPAKE2 PAKE authentication.
// Returns the authenticated net.Conn and derived 32-byte session key.
func Dial(ctx context.Context, targetAddr, codephrase string) (net.Conn, [32]byte, error) {
	if targetAddr == "" {
		return nil, [32]byte{}, errors.New("manual: target address is required")
	}
	if codephrase == "" {
		return nil, [32]byte{}, errors.New("manual: codephrase is required")
	}

	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("manual: dial failed: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// 1. Read sender's Initiator curve point
	initMsg, err := relay.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: read init msg: %w", err)
	}

	// 2. Initialize Responder with codephrase and send responder curve point
	responder, respMsg, err := pake.NewResponder(codephrase, initMsg)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: init responder: %w", err)
	}

	if err := relay.WriteFrame(conn, respMsg); err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: send resp msg: %w", err)
	}

	// 3. Derive 32-byte session key
	rawKey, err := responder.SessionKey()
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	// 4. Read challenge from sender
	challengeEnc, err := relay.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: read challenge: %w", err)
	}

	challengeBytes, err := relay.Decrypt(sessionKey[:], challengeEnc)
	if err != nil || !bytes.Equal(challengeBytes, []byte("mittodrop-auth")) {
		conn.Close()
		return nil, [32]byte{}, errors.New("manual: bad codephrase or authentication mismatch")
	}

	// 5. Send challenge ack back to sender
	ackEnc, err := relay.Encrypt(sessionKey[:], []byte("mittodrop-auth-ack"))
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: encrypt ack: %w", err)
	}

	if err := relay.WriteFrame(conn, ackEnc); err != nil {
		conn.Close()
		return nil, [32]byte{}, fmt.Errorf("manual: send ack: %w", err)
	}

	return conn, sessionKey, nil
}
