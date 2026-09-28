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
	IsIPv6       bool      `json:"is_ipv6"`
	LastSeen     time.Time `json:"last_seen"`
}

type ReceiverConfig struct {
	SelfDeviceID string
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

// Devices returns a copy of currently discovered devices slice.
func (r *Receiver) Devices() []DiscoveredDevice {
	r.mu.RLock()
	defer r.mu.RUnlock()

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

// handleMessage updates the slice with one entry per device, enforcing IPv6 priority.
func (r *Receiver) handleMessage(msg *ShoutMessage) {
	// Filter out own beacons
	if msg.DeviceID == r.cfg.SelfDeviceID {
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
		if r.devices[i].DeviceID == msg.DeviceID {
			idx = i
			break
		}
	}

	// Device not seen yet: add new entry
	if idx == -1 {
		r.devices = append(r.devices, DiscoveredDevice{
			DeviceID:     msg.DeviceID,
			DeviceName:   msg.DeviceName,
			SessionID:    msg.SessionID,
			InterfaceIP:  msg.InterfaceIP,
			TransferPort: msg.TransferPort,
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
