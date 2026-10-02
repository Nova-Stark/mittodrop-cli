package manual

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
	"mittodrop/internal/utils"
)

// Dial connects to a remote sender at targetAddr and executes mutual SPAKE2 PAKE authentication.
// Returns the authenticated net.Conn, derived 32-byte session key, and verified remote identity.
func Dial(ctx context.Context, targetAddr, codephrase string, local ...utils.PeerIdentity) (net.Conn, [32]byte, utils.PeerIdentity, error) {
	if targetAddr == "" {
		return nil, [32]byte{}, utils.PeerIdentity{}, errors.New("manual: target address is required")
	}
	if codephrase == "" {
		return nil, [32]byte{}, utils.PeerIdentity{}, errors.New("manual: codephrase is required")
	}

	var localID utils.PeerIdentity
	if len(local) > 0 {
		localID = local[0]
	}
	_ = localID.EnsureValid("")

	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: dial failed: %w", err)
	}

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// 1. Read sender's Initiator curve point
	initMsg, err := relay.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: read init msg: %w", err)
	}

	// 2. Initialize Responder with codephrase and send responder curve point
	responder, respMsg, err := pake.NewResponder(codephrase, initMsg)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: init responder: %w", err)
	}

	if err := relay.WriteFrame(conn, respMsg); err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: send resp msg: %w", err)
	}

	// 3. Derive 32-byte session key
	rawKey, err := responder.SessionKey()
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	// 4. Read challenge from sender
	challengeEnc, err := relay.ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: read challenge: %w", err)
	}

	challengeBytes, err := relay.Decrypt(sessionKey[:], challengeEnc)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, errors.New("manual: bad codephrase or authentication mismatch")
	}

	var remoteID utils.PeerIdentity
	if bytes.Equal(challengeBytes, []byte("mittodrop-auth")) {
		// Legacy string challenge compatibility
	} else {
		var challengeHello utils.PeerHello
		if err := json.Unmarshal(challengeBytes, &challengeHello); err != nil || challengeHello.Magic != "mittodrop-auth" {
			conn.Close()
			return nil, [32]byte{}, utils.PeerIdentity{}, errors.New("manual: invalid handshake challenge response")
		}
		remoteID = challengeHello.Identity
	}

	// 5. Send challenge ack back to sender carrying receiver's identity
	ackBytes, err := json.Marshal(utils.PeerHello{
		Magic:    "mittodrop-auth-ack",
		Identity: localID,
	})
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: marshal ack: %w", err)
	}

	ackEnc, err := relay.Encrypt(sessionKey[:], ackBytes)
	if err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: encrypt ack: %w", err)
	}

	if err := relay.WriteFrame(conn, ackEnc); err != nil {
		conn.Close()
		return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: send ack: %w", err)
	}

	return conn, sessionKey, remoteID, nil
}
