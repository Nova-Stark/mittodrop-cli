package tunnel

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/logger"
)

// Server wraps a tailcat.Server providing encrypted WireGuard tunnel listening.
type Server struct {
	server *tailcat.Server
	mu     sync.Mutex
	closed bool
}

// ServerOption configures a Server.
type ServerOption func(*tailcat.Server)

// WithServerPresharedKey sets a 32-byte WireGuard pre-shared key (e.g. derived from PAKE).
func WithServerPresharedKey(psk [32]byte) ServerOption {
	return func(s *tailcat.Server) {
		s.PresharedKey = tailcat.PresharedKey(psk)
	}
}

// WithServerRegion sets a specific DERP region (useful for integration tests or private DERPs).
func WithServerRegion(reg *tailcfg.DERPRegion) ServerOption {
	return func(s *tailcat.Server) {
		s.Region = reg
	}
}

// WithServerLogf sets a custom logger.
func WithServerLogf(logf logger.Logf) ServerOption {
	return func(s *tailcat.Server) {
		s.Logf = logf
	}
}

// NewServer initializes a new tunnel Server.
func NewServer(opts ...ServerOption) (*Server, error) {
	ts := &tailcat.Server{
		Logf: logger.Discard,
	}
	for _, opt := range opts {
		opt(ts)
	}
	return &Server{
		server: ts,
	}, nil
}

// Listen starts listening on the WireGuard tunnel for the specified virtual port.
// Port 0 auto-assigns an ephemeral port.
func (s *Server) Listen(ctx context.Context, port uint16) (net.Listener, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("server is closed")
	}

	addrStr := ":" + strconv.Itoa(int(port))
	return s.server.Listen(ctx, "tcp", addrStr)
}

// Addr returns the compact tailcat address string.
func (s *Server) Addr() tailcat.Addr {
	return s.server.TailcatAddr()
}

// Close shuts down the server and cleans up resources.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.server.Close()
}
