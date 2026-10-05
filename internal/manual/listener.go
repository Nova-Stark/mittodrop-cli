package manual

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"mittodrop/internal/netif"
	"mittodrop/internal/pake"
	"mittodrop/internal/portmap"
	"mittodrop/internal/relay"
	"mittodrop/internal/utils"
)

// Endpoint describes a dialable target address for manual direct connections.
type Endpoint struct {
	Type        string // "upnp", "ipv6", "lan"
	Address     string // "IP:Port" or "[IPv6]:Port"
	Description string // Human-friendly label
}

// Listener handles listening and authenticating manual direct P2P connections.
type Listener struct {
	codephrase   string
	listener     net.Listener
	port         int
	endpoints    []Endpoint
	upnpMapping  *portmap.Mapping
	mu           sync.Mutex
	closed       bool
}

// ListenOptions configures the direct manual listener.
type ListenOptions struct {
	Codephrase    string
	PreferredPort int
	NoUPnP        bool
}

// Listen starts a TCP listener for manual direct connections.
// If preferredPort is 0, an available candidate port (42201-42215) is chosen.
// Also discovers LAN and IPv6 addresses and attempts automatic UPnP router port forwarding.
func Listen(ctx context.Context, codephrase string, preferredPort int, opts ...ListenOptions) (*Listener, error) {
	if codephrase == "" {
		return nil, errors.New("manual: codephrase is required")
	}

	var opt ListenOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Codephrase == "" {
		opt.Codephrase = codephrase
	}
	if preferredPort > 0 && opt.PreferredPort == 0 {
		opt.PreferredPort = preferredPort
	}

	port := preferredPort
	if port <= 0 {
		p, err := utils.FindAvailablePort("0.0.0.0")
		if err != nil {
			return nil, fmt.Errorf("manual: find port: %w", err)
		}
		port = p
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("manual: listen on port %d: %w", port, err)
	}

	actualPort := ln.Addr().(*net.TCPAddr).Port

	l := &Listener{
		codephrase: codephrase,
		listener:   ln,
		port:       actualPort,
	}

	// 1. Gather local network and IPv6 endpoints
	l.gatherLocalEndpoints()

	// 2. Attempt UPnP-IGD router port forwarding (bounded timeout to avoid blocking)
	if !opt.NoUPnP {
		upnpCtx, upnpCancel := context.WithTimeout(ctx, 2*time.Second)
		defer upnpCancel()

		// Find first private IPv4 to pass to UPnP
		lanIP := l.findFirstLANIPv4()
		if lanIP != "" {
			mapping, err := portmap.Forward(upnpCtx, lanIP, actualPort, portmap.ProtocolTCP, "mittodrop-manual")
			if err == nil {
				l.upnpMapping = mapping
				l.endpoints = append([]Endpoint{
					{
						Type:        "upnp",
						Address:     fmt.Sprintf("%s:%d", mapping.ExternalIP, mapping.ExternalPort),
						Description: "Public Internet (UPnP Router Port Map)",
					},
				}, l.endpoints...)
			}
		}
	}

	return l, nil
}

// Port returns the bound TCP port.
func (l *Listener) Port() int {
	return l.port
}

// Endpoints returns the candidate dialable addresses.
func (l *Listener) Endpoints() []Endpoint {
	l.mu.Lock()
	defer l.mu.Unlock()
	res := make([]Endpoint, len(l.endpoints))
	copy(res, l.endpoints)
	return res
}

// Accept waits for a receiver connection and executes mutual SPAKE2 PAKE authentication.
// Returns the authenticated net.Conn, derived 32-byte session key, and verified remote identity.
func (l *Listener) Accept(ctx context.Context, local ...utils.PeerIdentity) (net.Conn, [32]byte, utils.PeerIdentity, error) {
	type acceptResult struct {
		conn net.Conn
		err  error
	}

	ch := make(chan acceptResult, 1)
	go func() {
		conn, err := l.listener.Accept()
		ch <- acceptResult{conn: conn, err: err}
	}()

	var localID utils.PeerIdentity
	if len(local) > 0 {
		localID = local[0]
	}
	_ = localID.EnsureValid("")

	select {
	case <-ctx.Done():
		return nil, [32]byte{}, utils.PeerIdentity{}, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: accept: %w", res.err)
		}
		conn := res.conn

		// Perform PAKE handshake with receiver
		key, remoteID, err := l.authenticateSender(conn, localID)
		if err != nil {
			conn.Close()
			return nil, [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("manual: authentication failed: %w", err)
		}

		return conn, key, remoteID, nil
	}
}

// Close terminates the listener and releases any UPnP port mapping.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true

	var err error
	if l.listener != nil {
		err = l.listener.Close()
	}
	if l.upnpMapping != nil {
		_ = l.upnpMapping.Release()
		l.upnpMapping = nil
	}
	return err
}

func (l *Listener) authenticateSender(conn net.Conn, localID utils.PeerIdentity) ([32]byte, utils.PeerIdentity, error) {
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	defer conn.SetDeadline(time.Time{})

	// 1. Sender is Initiator
	initiator, initMsg, err := pake.NewInitiator(l.codephrase)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("init initiator: %w", err)
	}

	if err := relay.WriteFrame(conn, initMsg); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send init msg: %w", err)
	}

	// 2. Read responder curve point
	respMsg, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read resp msg: %w", err)
	}

	// 3. Derive 32-byte session key
	rawKey, err := initiator.Finish(respMsg)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("derive session key: %w", err)
	}

	var sessionKey [32]byte
	copy(sessionKey[:], rawKey)

	// 4. Send encrypted challenge carrying local identity
	helloBytes, err := json.Marshal(utils.PeerHello{
		Magic:    "mittodrop-auth",
		Identity: localID,
	})
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("marshal challenge: %w", err)
	}

	challenge, err := relay.Encrypt(sessionKey[:], helloBytes)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("encrypt challenge: %w", err)
	}
	if err := relay.WriteFrame(conn, challenge); err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("send challenge: %w", err)
	}

	// 5. Receive and verify ack from receiver
	ackEnc, err := relay.ReadFrame(conn)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, fmt.Errorf("read challenge ack: %w", err)
	}

	ackBytes, err := relay.Decrypt(sessionKey[:], ackEnc)
	if err != nil {
		return [32]byte{}, utils.PeerIdentity{}, errors.New("bad codephrase or authentication mismatch")
	}

	var remoteID utils.PeerIdentity
	if bytes.Equal(ackBytes, []byte("mittodrop-auth-ack")) {
		// Legacy string ack compatibility
	} else {
		var ackHello utils.PeerHello
		if err := json.Unmarshal(ackBytes, &ackHello); err != nil || ackHello.Magic != "mittodrop-auth-ack" {
			return [32]byte{}, utils.PeerIdentity{}, errors.New("manual: invalid challenge ack")
		}
		remoteID = ackHello.Identity
	}

	return sessionKey, remoteID, nil
}

func (l *Listener) gatherLocalEndpoints() {
	networks := netif.GetNetworks()
	for _, netw := range networks {
		for _, item := range netw.IPtems {
			if item.IP.IsLoopback() {
				continue
			}

			if item.IsIPv6 && !item.IsLinkLocal && item.IP.IsGlobalUnicast() {
				l.endpoints = append(l.endpoints, Endpoint{
					Type:        "ipv6",
					Address:     fmt.Sprintf("[%s]:%d", item.IP.String(), l.port),
					Description: fmt.Sprintf("Global IPv6 (%s)", netw.InterfaceName),
				})
			} else if !item.IsIPv6 && item.IP.IsPrivate() {
				l.endpoints = append(l.endpoints, Endpoint{
					Type:        "lan",
					Address:     fmt.Sprintf("%s:%d", item.IP.String(), l.port),
					Description: fmt.Sprintf("Local LAN (%s)", netw.InterfaceName),
				})
			}
		}
	}
}

func (l *Listener) findFirstLANIPv4() string {
	for _, ep := range l.endpoints {
		if ep.Type == "lan" {
			host, _, err := net.SplitHostPort(ep.Address)
			if err == nil {
				return host
			}
		}
	}
	return ""
}
