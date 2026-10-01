package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	spake "github.com/schollz/pake/v3"
)

// Client handles dialing a croc-compatible relay server and joining a room.
type Client struct {
	DialTimeout time.Duration
}

// NewClient returns a Client with default options.
func NewClient() *Client {
	return &Client{
		DialTimeout: 20 * time.Second,
	}
}

// Connect dials the relay, executes the authentication and room join handshake,
// and returns the underlying net.Conn once paired with the peer.
func (c *Client) Connect(ctx context.Context, relayAddr, password, room string) (net.Conn, error) {
	if relayAddr == "" {
		return nil, errors.New("relay: address is required")
	}
	if room == "" {
		return nil, errors.New("relay: room ID is required")
	}

	dialer := &net.Dialer{
		Timeout: c.DialTimeout,
	}
	conn, err := dialer.DialContext(ctx, "tcp", relayAddr)
	if err != nil {
		return nil, fmt.Errorf("relay: dial failed: %w", err)
	}

	// Ensure cleanup on context cancellation during handshake
	handshakeDone := make(chan struct{})
	defer close(handshakeDone)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-handshakeDone:
		}
	}()

	// 1. PAKE handshake with relay using standard WeakKey and role 0
	A, err := spake.InitCurve(WeakKey, 0, "siec")
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: init PAKE: %w", err)
	}

	if err := WriteFrame(conn, A.Bytes()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: send PAKE A: %w", err)
	}

	Bbytes, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: receive PAKE B: %w", err)
	}

	if err := A.Update(Bbytes); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: update PAKE: %w", err)
	}

	strongKey, err := A.SessionKey()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: derive strong key: %w", err)
	}

	// 2. Generate salt and derive encryption key
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: generate salt: %w", err)
	}

	encKey := DeriveKey(strongKey, salt)

	// 3. Send salt to relay
	if err := WriteFrame(conn, salt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: send salt: %w", err)
	}

	// 4. Send encrypted password
	encPass, err := Encrypt(encKey, []byte(password))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: encrypt password: %w", err)
	}
	if err := WriteFrame(conn, encPass); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: send password: %w", err)
	}

	// 5. Send encrypted room ID
	encRoom, err := Encrypt(encKey, []byte(room))
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: encrypt room: %w", err)
	}
	if err := WriteFrame(conn, encRoom); err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: send room: %w", err)
	}

	// 6. Receive encrypted banner
	encBanner, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: receive banner: %w", err)
	}

	bannerBytes, err := Decrypt(encKey, encBanner)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: decrypt banner: %w", err)
	}

	if !strings.Contains(string(bannerBytes), "|||") {
		conn.Close()
		if bytes.Equal(bannerBytes, []byte("bad password")) {
			return nil, errors.New("relay: bad password")
		}
		return nil, fmt.Errorf("relay: unexpected banner response: %q", string(bannerBytes))
	}

	// 7. Receive room admission confirmation ("ok" when paired with peer)
	encConfirm, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: receive room confirmation: %w", err)
	}

	confirmBytes, err := Decrypt(encKey, encConfirm)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("relay: decrypt room confirmation: %w", err)
	}

	if !bytes.Equal(confirmBytes, []byte("ok")) {
		conn.Close()
		return nil, fmt.Errorf("relay: admission rejected: %s", string(confirmBytes))
	}

	// Successfully paired! Relay is now piping raw bytes between peers.
	return conn, nil
}
