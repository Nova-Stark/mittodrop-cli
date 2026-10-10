package shout

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"mittodrop/internal/netif"
	"mittodrop/internal/utils"
)

const DefaultShoutInterval = 1500 * time.Millisecond

type SenderConfig struct {
	Identity     utils.PeerIdentity
	DeviceID     string
	DeviceName   string
	SessionID    string
	TransferPort int
	Interval     time.Duration
}

type Sender struct {
	cfg SenderConfig
}

func NewSender(cfg SenderConfig) (*Sender, error) {
	if cfg.Identity.DeviceID != "" {
		if cfg.DeviceID == "" {
			cfg.DeviceID = cfg.Identity.DeviceID
		}
		if cfg.DeviceName == "" {
			cfg.DeviceName = cfg.Identity.DeviceName
		}
		if cfg.SessionID == "" {
			cfg.SessionID = cfg.Identity.SessionID
		}
	} else if cfg.DeviceID != "" {
		cfg.Identity.DeviceID = cfg.DeviceID
		cfg.Identity.DeviceName = cfg.DeviceName
		cfg.Identity.SessionID = cfg.SessionID
	}

	if cfg.DeviceID == "" {
		return nil, fmt.Errorf("shout sender: device_id is required")
	}
	if cfg.DeviceName == "" {
		return nil, fmt.Errorf("shout sender: device_name is required")
	}
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("shout sender: session_id is required")
	}
	if cfg.TransferPort <= 0 || cfg.TransferPort > 65535 {
		return nil, fmt.Errorf("shout sender: invalid transfer_port %d", cfg.TransferPort)
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultShoutInterval
	}
	return &Sender{cfg: cfg}, nil
}

// NewAnnouncer initializes a shout sender for an active listening transfer port
func NewAnnouncer(port int, id utils.PeerIdentity) (*Sender, error) {
	_ = id.EnsureValid("")
	return NewSender(SenderConfig{
		Identity:     id,
		DeviceID:     id.DeviceID,
		DeviceName:   id.DeviceName,
		SessionID:    id.SessionID,
		TransferPort: port,
	})
}

func (s *Sender) Start(ctx context.Context) error {
	s.ShoutOnce()

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.ShoutOnce()
		}
	}
}

func (s *Sender) ShoutOnce() {
	networks := netif.GetNetworks()

	for _, nw := range networks {
		var topV4, topV6 *netif.IPitem

		for i := range nw.IPtems {
			item := &nw.IPtems[i]
			if item.IsIPv6 && topV6 == nil {
				topV6 = item
			} else if !item.IsIPv6 && topV4 == nil {
				topV4 = item
			}
			if topV4 != nil && topV6 != nil {
				break
			}
		}

		if topV4 != nil {
			if err := s.sendOnIP(nw.InterfaceName, *topV4); err != nil {
				slog.Debug("shout: failed sending IPv4 beacon", "interface", nw.InterfaceName, "ip", topV4.IP, "err", err)
			}
		}

		if topV6 != nil {
			if err := s.sendOnIP(nw.InterfaceName, *topV6); err != nil {
				slog.Debug("shout: failed sending IPv6 beacon", "interface", nw.InterfaceName, "ip", topV6.IP, "err", err)
			}
		}
	}
}

func (s *Sender) sendOnIP(ifaceName string, item netif.IPitem) error {
	msg := ShoutMessage{
		DeviceID:     s.cfg.DeviceID,
		DeviceName:   s.cfg.DeviceName,
		SessionID:    s.cfg.SessionID,
		InterfaceIP:  item.IP.String(),
		TransferPort: s.cfg.TransferPort,
	}

	payload, err := msg.EncodeMsgpack()
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	if item.IsIPv6 {
		return s.broadcastUDP("udp6", ifaceName, item.IP, utils.DiscoveryGroupV6, payload)
	}
	return s.broadcastUDP("udp4", ifaceName, item.IP, utils.DiscoveryGroupV4, payload)
}

func (s *Sender) broadcastUDP(network, ifaceName string, srcIP net.IP, group utils.MulticastGroup, payload []byte) error {
	zone := ""
	if network == "udp6" {
		zone = ifaceName
	}

	laddr := &net.UDPAddr{
		IP:   srcIP,
		Port: 0,
		Zone: zone,
	}

	raddr := &net.UDPAddr{
		IP:   net.ParseIP(group.Address),
		Port: group.Port,
		Zone: zone,
	}

	conn, err := net.DialUDP(network, laddr, raddr)
	if err != nil {
		return fmt.Errorf("dial multicast %s -> %s: %w", laddr, raddr, err)
	}
	defer conn.Close()

	_, err = conn.Write(payload)
	if err != nil {
		return fmt.Errorf("write udp: %w", err)
	}
	return nil
}
