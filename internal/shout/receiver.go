package shout

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"mittodrop/internal/netif"
	"mittodrop/internal/utils"
)

type DiscoveredDevice struct {
	DeviceID     string    `json:"device_id"`
	DeviceName   string    `json:"device_name"`
	SessionID    string    `json:"session_id"`
	InterfaceIP  string    `json:"interface_ip"`
	TransferPort int       `json:"transfer_port"`
	Endpoints    []string  `json:"endpoints,omitempty"`
	IsIPv6       bool      `json:"is_ipv6"`
	LastSeen     time.Time `json:"last_seen"`
}

// Addr returns dialable host:port address string
func (d DiscoveredDevice) Addr() string {
	if d.IsIPv6 {
		return fmt.Sprintf("[%s]:%d", d.InterfaceIP, d.TransferPort)
	}
	return fmt.Sprintf("%s:%d", d.InterfaceIP, d.TransferPort)
}

// Identity returns peer identity representation
func (d DiscoveredDevice) Identity() utils.PeerIdentity {
	return utils.PeerIdentity{
		DeviceID:   d.DeviceID,
		DeviceName: d.DeviceName,
		SessionID:  d.SessionID,
	}
}

type ReceiverConfig struct {
	SelfDeviceID  string
	SelfSessionID string
}

type Receiver struct {
	cfg     ReceiverConfig
	mu      sync.RWMutex
	devices []DiscoveredDevice
}

// NewReceiver creates a new discovery receiver.
func NewReceiver(cfg ReceiverConfig) (*Receiver, error) {
	if cfg.SelfDeviceID == "" {
		return nil, fmt.Errorf("shout receiver: self_device_id is required")
	}
	return &Receiver{
		cfg:     cfg,
		devices: make([]DiscoveredDevice, 0),
	}, nil
}

// Devices returns a copy of currently discovered devices slice, pruning entries older than 10 seconds.
func (r *Receiver) Devices() []DiscoveredDevice {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	active := r.devices[:0]
	for _, dev := range r.devices {
		if now.Sub(dev.LastSeen) <= 10*time.Second {
			active = append(active, dev)
		}
	}
	r.devices = active

	res := make([]DiscoveredDevice, len(r.devices))
	copy(res, r.devices)
	return res
}

// Start begins listening on multicast groups on all network interfaces.
// Runs until ctx is canceled.
func (r *Receiver) Start(ctx context.Context) error {
	networks := netif.GetNetworks()

	var conns []*net.UDPConn
	var wg sync.WaitGroup

	for _, nw := range networks {
		ifi, err := net.InterfaceByName(nw.InterfaceName)
		if err != nil {
			continue
		}

		// Try joining IPv4 multicast
		if conn, err := r.joinGroup("udp4", ifi, utils.DiscoveryGroupV4); err == nil {
			conns = append(conns, conn)
			wg.Add(1)
			go r.listenLoop(ctx, conn, &wg)
		}

		// Try joining IPv6 multicast
		if conn, err := r.joinGroup("udp6", ifi, utils.DiscoveryGroupV6); err == nil {
			conns = append(conns, conn)
			wg.Add(1)
			go r.listenLoop(ctx, conn, &wg)
		}
	}

	if len(conns) == 0 {
		return fmt.Errorf("shout receiver: unable to bind to any multicast interface")
	}

	<-ctx.Done()

	// Unblock all listeners by closing sockets
	for _, c := range conns {
		c.Close()
	}

	wg.Wait()
	return ctx.Err()
}

func (r *Receiver) joinGroup(network string, ifi *net.Interface, group utils.MulticastGroup) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr(network, group.String())
	if err != nil {
		return nil, err
	}
	return net.ListenMulticastUDP(network, ifi, addr)
}

func (r *Receiver) listenLoop(ctx context.Context, conn *net.UDPConn, wg *sync.WaitGroup) {
	defer wg.Done()

	buf := make([]byte, 2048)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Debug("shout receiver: read error", "err", err)
				return
			}
		}

		msg, err := DecodeMsgpack(buf[:n])
		if err != nil {
			// Drop garbage bytes from other apps
			continue
		}

		r.handleMessage(msg)
	}
}

// ProcessBeacon injects a shout message for peer discovery processing
func (r *Receiver) ProcessBeacon(msg *ShoutMessage) {
	r.handleMessage(msg)
}

// handleMessage updates the slice with one entry per session, enforcing IPv6 priority.
func (r *Receiver) handleMessage(msg *ShoutMessage) {
	// Filter out own beacons: prefer SelfSessionID if configured, else SelfDeviceID
	if r.cfg.SelfSessionID != "" && msg.SessionID == r.cfg.SelfSessionID {
		return
	} else if r.cfg.SelfSessionID == "" && msg.DeviceID == r.cfg.SelfDeviceID {
		return
	}

	parsedIP := net.ParseIP(msg.InterfaceIP)
	if parsedIP == nil {
		return
	}
	incomingIsIPv6 := parsedIP.To4() == nil

	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	idx := -1
	for i := range r.devices {
		// Identify by SessionID so multiple instances on same host are distinguished
		if r.devices[i].SessionID == msg.SessionID {
			idx = i
			break
		}
	}

	var addrStr string
	if incomingIsIPv6 {
		addrStr = fmt.Sprintf("[%s]:%d", msg.InterfaceIP, msg.TransferPort)
	} else {
		addrStr = fmt.Sprintf("%s:%d", msg.InterfaceIP, msg.TransferPort)
	}

	// Device not seen yet: add new entry
	if idx == -1 {
		r.devices = append(r.devices, DiscoveredDevice{
			DeviceID:     msg.DeviceID,
			DeviceName:   msg.DeviceName,
			SessionID:    msg.SessionID,
			InterfaceIP:  msg.InterfaceIP,
			TransferPort: msg.TransferPort,
			Endpoints:    []string{addrStr},
			IsIPv6:       incomingIsIPv6,
			LastSeen:     now,
		})
		return
	}

	// Device already in slice: apply IPv6 preference policy
	existing := &r.devices[idx]
	existing.LastSeen = now
	existing.DeviceName = msg.DeviceName
	existing.SessionID = msg.SessionID

	hasAddr := false
	for _, ep := range existing.Endpoints {
		if ep == addrStr {
			hasAddr = true
			break
		}
	}
	if !hasAddr {
		existing.Endpoints = append(existing.Endpoints, addrStr)
	}

	if incomingIsIPv6 {
		// IPv6 always updates or upgrades entry
		existing.InterfaceIP = msg.InterfaceIP
		existing.TransferPort = msg.TransferPort
		existing.IsIPv6 = true
	} else if !existing.IsIPv6 {
		// Incoming IPv4 only updates if existing is not IPv6
		existing.InterfaceIP = msg.InterfaceIP
		existing.TransferPort = msg.TransferPort
	}
	// If existing is IPv6 and incoming is IPv4, keep IPv6.
}
