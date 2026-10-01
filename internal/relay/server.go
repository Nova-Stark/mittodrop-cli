package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	spake "github.com/schollz/pake/v3"
)

type waitingPeer struct {
	conn   net.Conn
	encKey []byte
	paired chan struct{}
}

// Server provides a self-hosted croc-compatible relay server.
type Server struct {
	Password string
	Banner   string

	mu       sync.Mutex
	rooms    map[string]*waitingPeer
	listener net.Listener
	closed   bool
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithPassword configures a required password on the relay server.
func WithPassword(password string) ServerOption {
	return func(s *Server) {
		s.Password = password
	}
}

// WithBanner configures the banner message sent upon authentication.
func WithBanner(banner string) ServerOption {
	return func(s *Server) {
		s.Banner = banner
	}
}

// NewServer initializes a new relay Server.
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		Banner: "mittodrop-relay",
		rooms:  make(map[string]*waitingPeer),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListenAndServe starts the TCP relay listener and handles rooms.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("relay: listen on %s: %w", addr, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	defer s.Close()

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return fmt.Errorf("relay: accept: %w", err)
		}

		go s.handleConn(ctx, conn)
	}
}

// Addr returns the listener's network address, or nil if not listening.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Close closes the listener and active rooms.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}

	for room, peer := range s.rooms {
		peer.conn.Close()
		delete(s.rooms, room)
	}

	return err
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	// 1. Initial PAKE with WeakKey in role 1
	B, err := spake.InitCurve(WeakKey, 1, "siec")
	if err != nil {
		conn.Close()
		return
	}

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	Abytes, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	// Ping support
	if bytes.Equal(Abytes, []byte("ping")) {
		_ = WriteFrame(conn, []byte("pong"))
		conn.Close()
		return
	}

	if err := B.Update(Abytes); err != nil {
		conn.Close()
		return
	}

	if err := WriteFrame(conn, B.Bytes()); err != nil {
		conn.Close()
		return
	}

	strongKey, err := B.SessionKey()
	if err != nil {
		conn.Close()
		return
	}

	// 2. Read salt and derive encryption key
	salt, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	encKey := DeriveKey(strongKey, salt)

	// 3. Read and check password
	encPass, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	passBytes, err := Decrypt(encKey, encPass)
	if err != nil {
		conn.Close()
		return
	}

	if strings.TrimSpace(string(passBytes)) != strings.TrimSpace(s.Password) {
		encErr, _ := Encrypt(encKey, []byte("bad password"))
		_ = WriteFrame(conn, encErr)
		conn.Close()
		return
	}

	// 4. Read room ID
	encRoom, err := ReadFrame(conn)
	if err != nil {
		conn.Close()
		return
	}

	roomBytes, err := Decrypt(encKey, encRoom)
	if err != nil {
		conn.Close()
		return
	}
	room := string(roomBytes)

	// 5. Send banner
	bannerMsg := s.Banner + "|||" + conn.RemoteAddr().String()
	encBanner, err := Encrypt(encKey, []byte(bannerMsg))
	if err != nil {
		conn.Close()
		return
	}
	if err := WriteFrame(conn, encBanner); err != nil {
		conn.Close()
		return
	}

	// Clear handshake deadline for pipe transfer
	_ = conn.SetDeadline(time.Time{})

	// 6. Room pairing logic
	s.mu.Lock()
	existing, ok := s.rooms[room]
	if !ok {
		// First peer in room: wait for second peer
		wp := &waitingPeer{
			conn:   conn,
			encKey: encKey,
			paired: make(chan struct{}),
		}
		s.rooms[room] = wp
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			s.mu.Lock()
			delete(s.rooms, room)
			s.mu.Unlock()
			conn.Close()
		case <-wp.paired:
			// Second peer has joined, confirmation sent, pipe is active
		}
		return
	}

	// Second peer in room: pair with existing peer
	delete(s.rooms, room)
	s.mu.Unlock()

	firstConn := existing.conn
	firstKey := existing.encKey

	// Send encrypted "ok" confirmation to both peers
	encOkFirst, _ := Encrypt(firstKey, []byte("ok"))
	if err := WriteFrame(firstConn, encOkFirst); err != nil {
		firstConn.Close()
		conn.Close()
		return
	}

	encOkSecond, _ := Encrypt(encKey, []byte("ok"))
	if err := WriteFrame(conn, encOkSecond); err != nil {
		firstConn.Close()
		conn.Close()
		return
	}

	// Signal first peer goroutine
	close(existing.paired)

	// 7. Full-duplex pipe between the two peers
	Pipe(firstConn, conn)
}

// Pipe connects two net.Conns together in full duplex mode until EOF or error.
func Pipe(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c1, c2)
		_ = c1.Close()
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c2, c1)
		_ = c2.Close()
	}()

	wg.Wait()
}
