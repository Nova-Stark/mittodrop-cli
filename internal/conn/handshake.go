package conn

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"time"

	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
)

// AuthenticateSender performs mutual SPAKE2 PAKE authentication from the sender side.
func AuthenticateSender(conn net.Conn, codephrase string) ([32]byte, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	initiator, initMsg, err := pake.NewInitiator(codephrase)
	if err != nil {
		return [32]byte{}, fmt.Errorf("init initiator: %w", err)
	}

	if err := relay.WriteFrame(conn, initMsg); err != nil {
		return [32]byte{}, fmt.Errorf("send init msg: %w", err)
	}

	respMsg, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, fmt.Errorf("read resp msg: %w", err)
	}

	rawKey, err := initiator.Finish(respMsg)
	if err != nil {
		return [32]byte{}, fmt.Errorf("derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	challenge, err := relay.Encrypt(sessionKey[:], []byte("mittodrop-auth"))
	if err != nil {
		return [32]byte{}, fmt.Errorf("encrypt challenge: %w", err)
	}
	if err := relay.WriteFrame(conn, challenge); err != nil {
		return [32]byte{}, fmt.Errorf("send challenge: %w", err)
	}

	ackEnc, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, fmt.Errorf("read ack: %w", err)
	}

	ack, err := relay.Decrypt(sessionKey[:], ackEnc)
	if err != nil || !bytes.Equal(ack, []byte("mittodrop-auth-ack")) {
		return [32]byte{}, errors.New("bad codephrase or authentication mismatch")
	}

	return sessionKey, nil
}

// AuthenticateReceiver performs mutual SPAKE2 PAKE authentication from the receiver side.
func AuthenticateReceiver(conn net.Conn, codephrase string) ([32]byte, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	initMsg, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, fmt.Errorf("read init msg: %w", err)
	}

	responder, respMsg, err := pake.NewResponder(codephrase, initMsg)
	if err != nil {
		return [32]byte{}, fmt.Errorf("init responder: %w", err)
	}

	if err := relay.WriteFrame(conn, respMsg); err != nil {
		return [32]byte{}, fmt.Errorf("send resp msg: %w", err)
	}

	rawKey, err := responder.SessionKey()
	if err != nil {
		return [32]byte{}, fmt.Errorf("derive key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	challengeEnc, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, fmt.Errorf("read challenge: %w", err)
	}

	challenge, err := relay.Decrypt(sessionKey[:], challengeEnc)
	if err != nil || !bytes.Equal(challenge, []byte("mittodrop-auth")) {
		return [32]byte{}, errors.New("bad codephrase or authentication mismatch")
	}

	ackEnc, err := relay.Encrypt(sessionKey[:], []byte("mittodrop-auth-ack"))
	if err != nil {
		return [32]byte{}, fmt.Errorf("encrypt ack: %w", err)
	}

	if err := relay.WriteFrame(conn, ackEnc); err != nil {
		return [32]byte{}, fmt.Errorf("send ack: %w", err)
	}

	return sessionKey, nil
}
