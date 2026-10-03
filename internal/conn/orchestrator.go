package conn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"mittodrop/internal/manual"
	"mittodrop/internal/pake"
	"mittodrop/internal/relay"
	"mittodrop/internal/tunnel"
	"mittodrop/internal/utils"
)

// SessionListener manages inbound connection negotiation for a sender.
type SessionListener struct {
	cfg        Config
	manualLn   *manual.Listener
	tunnelSrv  *tunnel.Server
	tunnelLn   net.Listener
	tunnelAddr string
	mu         sync.Mutex
	closed     bool
}

// Listen initializes the listening side for connection negotiation based on Config.Mode.
func Listen(ctx context.Context, cfg Config) (*SessionListener, error) {
	if cfg.Codephrase == "" {
		return nil, errors.New("conn: codephrase is required")
	}

	_ = cfg.Identity.EnsureValid("")

	mode := cfg.Mode
	if mode == "" {
		if cfg.RelayAddr != "" {
			mode = ModeRelay
		} else {
			mode = ModeTunnel
		}
	}

	sl := &SessionListener{cfg: cfg}

	switch mode {
	case ModeManual:
		ln, err := manual.Listen(ctx, cfg.Codephrase, cfg.PreferredPort)
		if err != nil {
			return nil, err
		}
		sl.manualLn = ln

	case ModeRelay:
		if cfg.RelayAddr == "" {
			return nil, errors.New("conn: relay address is required in relay mode")
		}
		// Start local direct listener to prepare for candidate probing
		ln, err := manual.Listen(ctx, cfg.Codephrase, cfg.PreferredPort)
		if err != nil {
			return nil, err
		}
		sl.manualLn = ln

	case ModeTunnel, ModeAuto:
		var opts []tunnel.ServerOption
		if cfg.DERPRegion != nil {
			opts = append(opts, tunnel.WithServerRegion(cfg.DERPRegion))
		}

		srv, err := tunnel.NewServer(opts...)
		if err != nil {
			return nil, fmt.Errorf("conn: new tunnel server: %w", err)
		}
		sl.tunnelSrv = srv

		port := uint16(cfg.PreferredPort)
		tln, err := srv.Listen(ctx, port)
		if err != nil {
			srv.Close()
			return nil, fmt.Errorf("conn: tunnel listen: %w", err)
		}
		sl.tunnelLn = tln
		sl.tunnelAddr = string(srv.Addr())

	default:
		return nil, fmt.Errorf("conn: unsupported mode %q", mode)
	}

	return sl, nil
}

// Endpoints returns the candidate endpoints for manual or direct connections.
func (sl *SessionListener) Endpoints() []manual.Endpoint {
	if sl.manualLn != nil {
		return sl.manualLn.Endpoints()
	}
	return nil
}

// Port returns the bound port for manual direct listeners.
func (sl *SessionListener) Port() int {
	if sl.manualLn != nil {
		return sl.manualLn.Port()
	}
	return 0
}

// TunnelAddr returns the Tailcat address string if tunnel mode is active.
func (sl *SessionListener) TunnelAddr() string {
	return sl.tunnelAddr
}

// Accept waits for a receiver and coordinates candidate probing / connection upgrade.
func (sl *SessionListener) Accept(ctx context.Context) (*Connection, error) {
	sl.mu.Lock()
	if sl.closed {
		sl.mu.Unlock()
		return nil, errors.New("conn: listener is closed")
	}
	sl.mu.Unlock()

	mode := sl.cfg.Mode
	if mode == "" {
		if sl.cfg.RelayAddr != "" {
			mode = ModeRelay
		} else {
			mode = ModeTunnel
		}
	}

	switch mode {
	case ModeManual:
		conn, key, remoteID, err := sl.manualLn.Accept(ctx, sl.cfg.Identity)
		if err != nil {
			return nil, err
		}
		return &Connection{
			Conn:       conn,
			SessionKey: key,
			PathType:   "manual",
			Local:      sl.cfg.Identity,
			Remote:     remoteID,
		}, nil

	case ModeRelay:
		cli := relay.NewClient()
		roomID := pake.RoomID(sl.cfg.Codephrase)
		relayConn, err := cli.Connect(ctx, sl.cfg.RelayAddr, sl.cfg.RelayPassword, roomID)
		if err != nil {
			return nil, fmt.Errorf("conn: relay connect: %w", err)
		}

		sessionKey, remoteID, err := AuthenticateSender(relayConn, sl.cfg.Codephrase, sl.cfg.Identity)
		if err != nil {
			relayConn.Close()
			return nil, fmt.Errorf("conn: relay auth failed: %w", err)
		}

		// Exchange candidate endpoints with receiver over relay
		if err := SendCandidates(relayConn, sl.manualLn.Endpoints()); err != nil {
			relayConn.Close()
			return nil, err
		}

		type acceptDirectResult struct {
			conn     net.Conn
			key      [32]byte
			remoteID utils.PeerIdentity
			err      error
		}
		directCh := make(chan acceptDirectResult, 1)
		go func() {
			c, k, r, err := sl.manualLn.Accept(ctx, sl.cfg.Identity)
			directCh <- acceptDirectResult{conn: c, key: k, remoteID: r, err: err}
		}()

		// Wait for receiver's decision
		decision, err := relay.ReadFrame(relayConn)
		if err != nil {
			_ = sl.manualLn.Close()
			relayConn.Close()
			return nil, fmt.Errorf("conn: read upgrade decision: %w", err)
		}

		if bytes.Equal(decision, []byte("upgrade-direct")) {
			// Receiver successfully dialed our direct endpoint; collect accepted conn
			relayConn.Close() // Disconnect from relay to conserve bandwidth
			select {
			case <-ctx.Done():
				_ = sl.manualLn.Close()
				return nil, ctx.Err()
			case res := <-directCh:
				if res.err != nil {
					return nil, fmt.Errorf("conn: accept upgraded direct conn: %w", res.err)
				}
				if res.remoteID.DeviceID == "" {
					res.remoteID = remoteID
				}
				return &Connection{
					Conn:       res.conn,
					SessionKey: res.key,
					PathType:   "direct-p2p",
					Local:      sl.cfg.Identity,
					Remote:     res.remoteID,
				}, nil
			}
		}

		// Receiver could not reach direct endpoints; stay on relayed stream
		_ = sl.manualLn.Close()
		return &Connection{
			Conn:       relayConn,
			SessionKey: sessionKey,
			PathType:   "relay",
			Local:      sl.cfg.Identity,
			Remote:     remoteID,
		}, nil

	case ModeTunnel, ModeAuto:
		type acceptResult struct {
			conn net.Conn
			err  error
		}
		ch := make(chan acceptResult, 1)
		go func() {
			conn, err := sl.tunnelLn.Accept()
			ch <- acceptResult{conn: conn, err: err}
		}()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-ch:
			if res.err != nil {
				return nil, fmt.Errorf("conn: tunnel accept: %w", res.err)
			}

			key, remoteID, err := AuthenticateSender(res.conn, sl.cfg.Codephrase, sl.cfg.Identity)
			if err != nil {
				res.conn.Close()
				return nil, fmt.Errorf("conn: tunnel auth failed: %w", err)
			}

			return &Connection{
				Conn:       res.conn,
				SessionKey: key,
				PathType:   "tunnel",
				Local:      sl.cfg.Identity,
				Remote:     remoteID,
			}, nil
		}

	default:
		return nil, fmt.Errorf("conn: unsupported mode %q", mode)
	}
}

// Close cleans up listener resources.
func (sl *SessionListener) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.closed {
		return nil
	}
	sl.closed = true

	var err error
	if sl.manualLn != nil {
		err = sl.manualLn.Close()
	}
	if sl.tunnelLn != nil {
		_ = sl.tunnelLn.Close()
	}
	if sl.tunnelSrv != nil {
		_ = sl.tunnelSrv.Close()
	}
	return err
}

// Connect establishes the receiving side of a connection.
func Connect(ctx context.Context, cfg Config) (*Connection, error) {
	if cfg.Codephrase == "" {
		return nil, errors.New("conn: codephrase is required")
	}

	_ = cfg.Identity.EnsureValid("")

	mode := cfg.Mode
	if mode == "" {
		if cfg.RelayAddr != "" {
			mode = ModeRelay
		} else {
			mode = ModeTunnel
		}
	}

	switch mode {
	case ModeManual:
		if cfg.TargetAddr == "" {
			return nil, errors.New("conn: target address is required in manual mode")
		}
		conn, key, remoteID, err := manual.Dial(ctx, cfg.TargetAddr, cfg.Codephrase, cfg.Identity)
		if err != nil {
			return nil, err
		}
		return &Connection{
			Conn:       conn,
			SessionKey: key,
			PathType:   "manual",
			Local:      cfg.Identity,
			Remote:     remoteID,
		}, nil

	case ModeRelay:
		if cfg.RelayAddr == "" {
			return nil, errors.New("conn: relay address is required in relay mode")
		}
		cli := relay.NewClient()
		roomID := pake.RoomID(cfg.Codephrase)
		relayConn, err := cli.Connect(ctx, cfg.RelayAddr, cfg.RelayPassword, roomID)
		if err != nil {
			return nil, fmt.Errorf("conn: relay connect: %w", err)
		}

		sessionKey, remoteID, err := AuthenticateReceiver(relayConn, cfg.Codephrase, cfg.Identity)
		if err != nil {
			relayConn.Close()
			return nil, fmt.Errorf("conn: relay auth failed: %w", err)
		}

		// Receive candidate endpoints from sender
		candidates, err := ReceiveCandidates(relayConn)
		if err != nil {
			relayConn.Close()
			return nil, err
		}

		// Probe candidate endpoints
		directConn, directKey, pathType, probeErr := ProbeCandidates(ctx, candidates, cfg.Codephrase, cfg.Identity)
		if probeErr == nil {
			// Direct probe succeeded! Notify sender to upgrade and switch.
			_ = relay.WriteFrame(relayConn, []byte("upgrade-direct"))
			relayConn.Close()

			return &Connection{
				Conn:       directConn,
				SessionKey: directKey,
				PathType:   pathType,
				Local:      cfg.Identity,
				Remote:     remoteID,
			}, nil
		}

		// Direct probe failed; stay on relay
		_ = relay.WriteFrame(relayConn, []byte("stay-rendezvous"))
		return &Connection{
			Conn:       relayConn,
			SessionKey: sessionKey,
			PathType:   "relay",
			Local:      cfg.Identity,
			Remote:     remoteID,
		}, nil

	case ModeTunnel, ModeAuto:
		if cfg.TargetAddr == "" {
			return nil, errors.New("conn: target tunnel address is required")
		}

		cli, err := tunnel.NewClient(tunnel.Addr(cfg.TargetAddr))
		if err != nil {
			return nil, fmt.Errorf("conn: new tunnel client: %w", err)
		}

		port := uint16(cfg.PreferredPort)
		if port == 0 {
			port = 42201
		}

		conn, err := cli.Dial(ctx, port)
		if err != nil {
			cli.Close()
			return nil, fmt.Errorf("conn: tunnel dial: %w", err)
		}

		key, remoteID, err := AuthenticateReceiver(conn, cfg.Codephrase, cfg.Identity)
		if err != nil {
			conn.Close()
			cli.Close()
			return nil, fmt.Errorf("conn: tunnel auth failed: %w", err)
		}

		return &Connection{
			Conn:       conn,
			SessionKey: key,
			PathType:   "tunnel",
			Local:      cfg.Identity,
			Remote:     remoteID,
		}, nil

	default:
		return nil, fmt.Errorf("conn: unsupported mode %q", mode)
	}
}
