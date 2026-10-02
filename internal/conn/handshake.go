package conn

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
	"mittodrop/internal/utils"
)

// PeerHello is the authenticated identity envelope exchanged during connection handshake.
type PeerHello struct {
	Magic    string             `json:"magic"` // "mittodrop-auth" or "mittodrop-auth-ack"
	Identity utils.PeerIdentity `json:"identity"`
}

// AuthenticateSender performs mutual SPAKE2 PAKE authentication and exchanges peer identities.
func AuthenticateSender(conn net.Conn, codephrase string, local ...utils.PeerIdentity) ([32]byte, utils.PeerIdentity, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	var localID utils.PeerIdentity
	if len(local) > 0 {
		localID = local[0]
	}
	_ = localID.EnsureValid("")

	initiator, initMsg, err := pake.NewInitiator(codephrase)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("init initiator: %w", err)
	}

	if err := relay.WriteFrame(conn, initMsg); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send init msg: %w", err)
	}

	respMsg, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read resp msg: %w", err)
	}

	rawKey, err := initiator.Finish(respMsg)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	// Send encrypted challenge carrying local identity
	helloBytes, err := json.Marshal(PeerHello{
		Magic:    "mittodrop-auth",
		Identity: localID,
	})
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("marshal peer hello: %w", err)
	}

	challenge, err := relay.Encrypt(sessionKey[:], helloBytes)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("encrypt challenge: %w", err)
	}
	if err := relay.WriteFrame(conn, challenge); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send challenge: %w", err)
	}

	// Read and verify receiver ack
	ackEnc, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read ack: %w", err)
	}

	ackBytes, err := relay.Decrypt(sessionKey[:], ackEnc)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, errors.New("bad codephrase or authentication mismatch")
	}

	var remoteID utils.PeerIdentity
	if bytes.Equal(ackBytes, []byte("mittodrop-auth-ack")) {
		// Legacy string ack compatibility
	} else {
		var ackHello PeerHello
		if err := json.Unmarshal(ackBytes, &ackHello); err != nil || ackHello.Magic != "mittodrop-auth-ack" {
			return [32]byte{}, utils.PeerIdentity{}, errors.New("invalid handshake ack response")
		}
		remoteID = ackHello.Identity
	}

	return sessionKey, remoteID, nil
}

// AuthenticateReceiver performs mutual SPAKE2 PAKE authentication and exchanges peer identities.
func AuthenticateReceiver(conn net.Conn, codephrase string, local ...utils.PeerIdentity) ([32]byte, utils.PeerIdentity, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	var localID utils.PeerIdentity
	if len(local) > 0 {
		localID = local[0]
	}
	_ = localID.EnsureValid("")

	initMsg, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read init msg: %w", err)
	}

	responder, respMsg, err := pake.NewResponder(codephrase, initMsg)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("init responder: %w", err)
	}

	if err := relay.WriteFrame(conn, respMsg); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send resp msg: %w", err)
	}

	rawKey, err := responder.SessionKey()
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	// Read and verify challenge from sender
	challengeEnc, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read challenge: %w", err)
	}

	challengeBytes, err := relay.Decrypt(sessionKey[:], challengeEnc)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, errors.New("bad codephrase or authentication mismatch")
	}

	var remoteID utils.PeerIdentity
	if bytes.Equal(challengeBytes, []byte("mittodrop-auth")) {
		// Legacy string challenge compatibility
	} else {
		var challengeHello PeerHello
		if err := json.Unmarshal(challengeBytes, &challengeHello); err != nil || challengeHello.Magic != "mittodrop-auth" {
			return [32]byte{}, utils.PeerIdentity{}, errors.New("invalid handshake challenge response")
		}
		remoteID = challengeHello.Identity
	}

	// Send encrypted challenge ack carrying receiver's identity
	ackBytes, err := json.Marshal(PeerHello{
		Magic:    "mittodrop-auth-ack",
		Identity: localID,
	})
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("marshal peer hello ack: %w", err)
	}

	ackEnc, err := relay.Encrypt(sessionKey[:], ackBytes)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("encrypt ack: %w", err)
	}

	if err := relay.WriteFrame(conn, ackEnc); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send ack: %w", err)
	}

	return sessionKey, remoteID, nil
}
