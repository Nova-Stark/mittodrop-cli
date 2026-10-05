package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	spake "github.com/schollz/pake/v3"
)

type waitingPeer struct {
	conn      net.Conn
	encKey    []byte
	paired    chan struct{}
	createdAt time.Time
}

type ipLimiter struct {
	mu      sync.Mutex
	history map[string][]time.Time
	limit   int
	window  time.Duration
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{
		history: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

func (l *ipLimiter) Allow(ip string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	times := l.history[ip]
	valid := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= l.limit {
		l.history[ip] = valid
		return false
	}
	l.history[ip] = append(valid, now)
	return true
}

func (l *ipLimiter) Clean() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-l.window)
	for ip, times := range l.history {
		valid := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(l.history, ip)
		} else {
			l.history[ip] = valid
		}
	}
}

// Server provides a self-hosted croc-compatible relay server.
type Server struct {
	Password        string
	Banner          string
	RoomTTL         time.Duration
	MaxWaitingRooms int
	IPLimit         int
	IPWindow        time.Duration

	mu       sync.Mutex
	rooms    map[string]*waitingPeer
	listener net.Listener
	closed   bool
	limiter  *ipLimiter
	Logger   *slog.Logger
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithLogger configures a structured slog.Logger.
func WithLogger(logger *slog.Logger) ServerOption {
	return func(s *Server) {
		s.Logger = logger
	}
}

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

// WithRoomTTL configures maximum time an unpaired waiting room lives before cleanup.
func WithRoomTTL(ttl time.Duration) ServerOption {
	return func(s *Server) {
		s.RoomTTL = ttl
	}
}

// WithMaxWaitingRooms limits maximum number of concurrent unpaired waiting rooms.
func WithMaxWaitingRooms(max int) ServerOption {
	return func(s *Server) {
		s.MaxWaitingRooms = max
	}
}

// WithRateLimit configures max connections per IP within window.
func WithRateLimit(limit int, window time.Duration) ServerOption {
	return func(s *Server) {
		s.IPLimit = limit
		s.IPWindow = window
	}
}

// NewServer initializes a new relay Server.
func NewServer(opts ...ServerOption) *Server {
	s := &Server{
		Banner:          "mittodrop-relay",
		rooms:           make(map[string]*waitingPeer),
		RoomTTL:         30 * time.Minute,
		MaxWaitingRooms: 1000,
		IPLimit:         60,
		IPWindow:        1 * time.Minute,
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	s.limiter = newIPLimiter(s.IPLimit, s.IPWindow)
	return s
}

// ListenAndServe starts the TCP relay listener and handles rooms.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("relay: listen on %s: %w", addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve accepts connections on listener and handles rooms until ctx cancellation or listener close.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.Logger.Info("relay server listening", "addr", ln.Addr().String(), "ttl", s.RoomTTL.String(), "max_rooms", s.MaxWaitingRooms, "rate_limit", s.IPLimit)
	defer s.Close()

	go func() {
		<-ctx.Done()
		s.Logger.Info("relay server context canceled, stopping")
		s.Close()
	}()

	go s.reapOldRooms(ctx)

	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			s.Logger.Error("relay accept error", "error", err)
			return fmt.Errorf("relay: accept: %w", err)
		}

		go s.handleConn(ctx, conn)
	}
}

func (s *Server) reapOldRooms(ctx context.Context) {
	interval := s.RoomTTL / 4
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	} else if interval > 1*time.Minute {
		interval = 1 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanExpiredRooms()
			if s.limiter != nil {
				s.limiter.Clean()
			}
		}
	}
}

func (s *Server) cleanExpiredRooms() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.RoomTTL <= 0 {
		return
	}

	cutoff := time.Now().Add(-s.RoomTTL)
	for room, wp := range s.rooms {
		if wp.createdAt.Before(cutoff) {
			s.Logger.Debug("unpaired room expired by reaper", "room", room)
			_ = wp.conn.Close()
			delete(s.rooms, room)
		}
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
	// IP rate limiting
	if host, _, err := net.SplitHostPort(conn.RemoteAddr().String()); err == nil {
		if s.limiter != nil && !s.limiter.Allow(host) {
			s.Logger.Warn("client rate limited, connection closed", "remote_ip", host)
			_ = conn.Close()
			return
		}
	}
	s.Logger.Debug("client connected", "remote_addr", conn.RemoteAddr().String())

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
		// Evict oldest waiting room if at capacity
		if s.MaxWaitingRooms > 0 && len(s.rooms) >= s.MaxWaitingRooms {
			var oldestRoom string
			var oldestWP *waitingPeer
			for r, wp := range s.rooms {
				if oldestWP == nil || wp.createdAt.Before(oldestWP.createdAt) {
					oldestRoom = r
					oldestWP = wp
				}
			}
			if oldestWP != nil {
				s.Logger.Warn("evicting oldest waiting room due to capacity", "room", oldestRoom)
				_ = oldestWP.conn.Close()
				delete(s.rooms, oldestRoom)
			}
		}

		// First peer in room: wait for second peer
		wp := &waitingPeer{
			conn:      conn,
			encKey:    encKey,
			paired:    make(chan struct{}),
			createdAt: time.Now(),
		}
		s.rooms[room] = wp
		s.mu.Unlock()
		s.Logger.Info("room opened, waiting for peer", "room", room, "remote_addr", conn.RemoteAddr().String())

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
	s.Logger.Info("room paired, starting stream pipe", "room", room, "peer1", firstConn.RemoteAddr().String(), "peer2", conn.RemoteAddr().String())

	// 7. Full-duplex pipe between the two peers
	Pipe(firstConn, conn)
	s.Logger.Info("room stream pipe closed", "room", room)
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
