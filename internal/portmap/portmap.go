package portmap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway1"
)

var (
	// ErrNoGateway indicates no UPnP-IGD compatible router responded.
	ErrNoGateway = errors.New("portmap: no UPnP-IGD gateway found on local network")
)

// Protocol represents transport protocol for port forwarding.
type Protocol string

const (
	ProtocolTCP Protocol = "TCP"
	ProtocolUDP Protocol = "UDP"
)

// Mapping represents an active UPnP port forwarding lease.
type Mapping struct {
	ExternalIP   string
	ExternalPort int
	InternalPort int
	InternalIP   string
	Protocol     Protocol
	Release      func() error
}

// Forward attempts to discover an IGD gateway on the local network and map
// externalPort -> internalPort for the specified protocol.
// If internalIP is empty, automatically detects local outbound LAN IP.
func Forward(ctx context.Context, internalIP string, internalPort int, proto Protocol, desc string) (*Mapping, error) {
	if internalPort <= 0 || internalPort > 65535 {
		return nil, fmt.Errorf("portmap: invalid internal port: %d", internalPort)
	}
	if proto != ProtocolTCP && proto != ProtocolUDP {
		return nil, fmt.Errorf("portmap: unsupported protocol: %s", proto)
	}
	if desc == "" {
		desc = "mittodrop"
	}

	if internalIP == "" {
		var err error
		internalIP, err = outboundIP()
		if err != nil {
			return nil, fmt.Errorf("portmap: detect internal IP: %w", err)
		}
	}

	// 1. Try WANIPConnection1 clients
	ipClients, _, _ := internetgateway1.NewWANIPConnection1ClientsCtx(ctx)
	for _, client := range ipClients {
		extIP, err := client.GetExternalIPAddressCtx(ctx)
		if err != nil || extIP == "" || extIP == "0.0.0.0" {
			continue
		}

		extPort := uint16(internalPort)
		err = client.AddPortMappingCtx(
			ctx,
			"",                   // RemoteHost (wildcard)
			extPort,              // ExternalPort
			string(proto),        // Protocol (TCP/UDP)
			uint16(internalPort), // InternalPort
			internalIP,           // InternalClient
			true,                 // Enabled
			desc,                 // Description
			3600,                 // LeaseDuration (1 hour)
		)
		if err != nil {
			continue
		}

		releaseFunc := func() error {
			ctxRelease, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return client.DeletePortMappingCtx(ctxRelease, "", extPort, string(proto))
		}

		return &Mapping{
			ExternalIP:   extIP,
			ExternalPort: int(extPort),
			InternalPort: internalPort,
			InternalIP:   internalIP,
			Protocol:     proto,
			Release:      releaseFunc,
		}, nil
	}

	// 2. Try WANPPPConnection1 clients (DSL / PPPoE routers)
	pppClients, _, _ := internetgateway1.NewWANPPPConnection1ClientsCtx(ctx)
	for _, client := range pppClients {
		extIP, err := client.GetExternalIPAddressCtx(ctx)
		if err != nil || extIP == "" || extIP == "0.0.0.0" {
			continue
		}

		extPort := uint16(internalPort)
		err = client.AddPortMappingCtx(
			ctx,
			"",
			extPort,
			string(proto),
			uint16(internalPort),
			internalIP,
			true,
			desc,
			3600,
		)
		if err != nil {
			continue
		}

		releaseFunc := func() error {
			ctxRelease, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			return client.DeletePortMappingCtx(ctxRelease, "", extPort, string(proto))
		}

		return &Mapping{
			ExternalIP:   extIP,
			ExternalPort: int(extPort),
			InternalPort: internalPort,
			InternalIP:   internalIP,
			Protocol:     proto,
			Release:      releaseFunc,
		}, nil
	}

	return nil, ErrNoGateway
}

func outboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}
