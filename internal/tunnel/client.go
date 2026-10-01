package tunnel

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/logger"
)

// Addr is the compact tailcat address string format.
type Addr = tailcat.Addr

// Client wraps a tailcat.Client to establish connections over a WireGuard tunnel.
type Client struct {
	client *tailcat.Client
	mu     sync.Mutex
	closed bool
}

// ClientOption configures a Client.
type ClientOption func(*tailcat.Client)

// WithClientLogf sets a custom logger.
func WithClientLogf(logf logger.Logf) ClientOption {
	return func(c *tailcat.Client) {
		c.Logf = logf
	}
}

// WithClientDERPMapURL sets a custom DERP map URL.
func WithClientDERPMapURL(url string) ClientOption {
	return func(c *tailcat.Client) {
		c.DERPMapURL = url
	}
}

// NewClient creates a new tunnel Client targeting the provided server address.
func NewClient(serverAddr tailcat.Addr, opts ...ClientOption) (*Client, error) {
	if serverAddr == "" {
		return nil, errors.New("server address is required")
	}
	tc := tailcat.NewClient(serverAddr)
	tc.Logf = logger.Discard
	for _, opt := range opts {
		opt(tc)
	}
	return &Client{
		client: tc,
	}, nil
}

// Dial connects to the server on the specified virtual port.
func (c *Client) Dial(ctx context.Context, port uint16) (net.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("client is closed")
	}
	return c.client.DialTCPPort(ctx, port)
}

// Ping tests connectivity to the server and measures round-trip time.
func (c *Client) Ping(ctx context.Context) (tailcat.PingResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return tailcat.PingResult{}, errors.New("client is closed")
	}
	return c.client.Ping(ctx)
}

// Close closes the client and shuts down active connections.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.client.Close()
}

// ApplyPresharedKey returns a copy of the tailcat.Addr with the given 32-byte pre-shared key embedded.
func ApplyPresharedKey(addr tailcat.Addr, psk [32]byte) (tailcat.Addr, error) {
	ci, err := tailcat.ParseAddr(addr)
	if err != nil {
		return "", err
	}
	ci.PresharedKey = tailcat.PresharedKey(psk)
	return ci.Addr(), nil
}
