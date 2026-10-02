package conn

import (
	"net"

	"mittodrop/internal/manual"
	"mittodrop/internal/utils"
	"tailscale.com/tailcfg"
)

// Mode defines the connection negotiation strategy.
type Mode string

const (
	// ModeAuto tries rendezvous (relay or tunnel), probes direct endpoints (IPv6, LAN, UPnP),
	// and upgrades to a direct link if reachable, else stays on the fallback tunnel/relay.
	ModeAuto Mode = "auto"

	// ModeManual forces direct connection to a known IP:port without relay.
	ModeManual Mode = "manual"

	// ModeRelay routes via a self-hosted croc-compatible relay server.
	ModeRelay Mode = "relay"

	// ModeTunnel uses Tailcat WireGuard data plane with DERP bootstrap and Magicsock P2P.
	ModeTunnel Mode = "tunnel"
)

// Config configures the connection orchestrator.
type Config struct {
	Codephrase    string              // Shared 3-word PAKE secret
	Mode          Mode                // Desired connection mode (default ModeAuto)
	RelayAddr     string              // Relay host:port (required for ModeRelay, optional rendezvous for ModeAuto)
	RelayPassword string              // Optional password for relay
	TargetAddr    string              // Manual target IP:port (required for ModeManual receiver)
	PreferredPort int                 // Preferred local TCP listening port (0 for auto)
	DERPRegion    *tailcfg.DERPRegion // Optional DERP region override (used for testing or self-hosted DERP)
	Identity      utils.PeerIdentity  // Local peer identity
}

// Connection represents an established, authenticated, encrypted transport stream.
type Connection struct {
	Conn       net.Conn           // Underlying network connection
	SessionKey [32]byte           // 256-bit symmetric key derived from SPAKE2 PAKE
	PathType   string             // Transport path: "direct-ipv6", "direct-lan", "direct-upnp", "tunnel-p2p", "tunnel-derp", "relay", "manual"
	Local      utils.PeerIdentity // Local peer identity (DeviceID, DeviceName, SessionID)
	Remote     utils.PeerIdentity // Remote authenticated peer identity (DeviceID, DeviceName, SessionID)
}

// Close closes the underlying connection.
func (c *Connection) Close() error {
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return nil
}

// CandidatePayload describes candidate endpoints exchanged over rendezvous.
type CandidatePayload struct {
	Candidates []manual.Endpoint `json:"candidates"`
}
