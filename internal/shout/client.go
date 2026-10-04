package shout

import (
	"context"
	"fmt"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

// ConnectToDevice dials a peer discovered via shout multicast discovery.
func ConnectToDevice(ctx context.Context, dev DiscoveredDevice, codephrase string, localID utils.PeerIdentity) (*conn.Connection, error) {
	phrase := codephrase
	if phrase == "" {
		phrase = "mittodrop-lan-v1"
	}

	targetAddr := dev.Addr()
	cfg := conn.Config{
		Mode:       conn.ModeManual,
		TargetAddr: targetAddr,
		Codephrase: phrase,
		Identity:   localID,
	}

	c, err := conn.Connect(ctx, cfg)
	if err == nil {
		return c, nil
	}

	// Try alternative endpoints if primary failed
	for _, alt := range dev.Endpoints {
		if alt == targetAddr {
			continue
		}
		cfg.TargetAddr = alt
		if altConn, altErr := conn.Connect(ctx, cfg); altErr == nil {
			return altConn, nil
		}
	}

	return nil, fmt.Errorf("shout: connect to device %s (%s): %w", dev.DeviceName, targetAddr, err)
}

// SendToDevice dials a discovered peer and transmits a single file stream.
func SendToDevice(ctx context.Context, dev DiscoveredDevice, codephrase string, localID utils.PeerIdentity, filePath string, onProgress transport.ProgressCallback) error {
	c, err := ConnectToDevice(ctx, dev, codephrase, localID)
	if err != nil {
		return err
	}
	defer c.Close()

	reader, err := transfer.NewReader(ctx, filePath)
	if err != nil {
		return fmt.Errorf("shout: open reader: %w", err)
	}
	defer reader.Close()

	return transport.SendFile(ctx, c, reader, onProgress)
}
