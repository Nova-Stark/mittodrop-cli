package transport

import (
	"context"
	"errors"
	"fmt"

	"mittodrop/internal/conn"
	"mittodrop/internal/shout"
	"mittodrop/internal/transfer"
	"mittodrop/internal/utils"
)

// Session manages the listening lifecycle and can perform transfer upon connection accept
type Session struct {
	sl *conn.SessionListener
}

// Listen starts inbound connection listener for transfer
func Listen(ctx context.Context, cfg conn.Config) (*Session, error) {
	sl, err := conn.Listen(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("transport: listen: %w", err)
	}
	return &Session{sl: sl}, nil
}

// Listener returns the underlying conn.SessionListener
func (s *Session) Listener() *conn.SessionListener {
	return s.sl
}

// Close cleans up listener endpoints
func (s *Session) Close() error {
	if s.sl != nil {
		return s.sl.Close()
	}
	return nil
}

// AcceptAndSend waits for peer, connects reader, and transmits file
func (s *Session) AcceptAndSend(ctx context.Context, filePath string, onProgress ProgressCallback) error {
	c, err := s.sl.Accept(ctx)
	if err != nil {
		return fmt.Errorf("transport: accept: %w", err)
	}
	defer c.Close()

	reader, err := transfer.NewReader(ctx, filePath)
	if err != nil {
		return fmt.Errorf("transport: open reader: %w", err)
	}
	defer reader.Close()

	return SendFile(ctx, c, reader, onProgress)
}

// AcceptAndReceive waits for peer and receives incoming file
func (s *Session) AcceptAndReceive(ctx context.Context, saveDir string, onProgress ProgressCallback) (*transfer.FileMetadata, error) {
	c, err := s.sl.Accept(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport: accept: %w", err)
	}
	defer c.Close()

	return ReceiveFile(ctx, c, saveDir, onProgress)
}

// ConnectAndReceive connects to listening peer and downloads file
func ConnectAndReceive(ctx context.Context, cfg conn.Config, saveDir string, onProgress ProgressCallback) (*transfer.FileMetadata, error) {
	c, err := conn.Connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("transport: connect: %w", err)
	}
	defer c.Close()

	return ReceiveFile(ctx, c, saveDir, onProgress)
}

// ConnectAndSend connects to receiving peer and uploads file
func ConnectAndSend(ctx context.Context, cfg conn.Config, filePath string, onProgress ProgressCallback) error {
	c, err := conn.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("transport: connect: %w", err)
	}
	defer c.Close()

	reader, err := transfer.NewReader(ctx, filePath)
	if err != nil {
		return fmt.Errorf("transport: open reader: %w", err)
	}
	defer reader.Close()

	return SendFile(ctx, c, reader, onProgress)
}

// ConnectToDevice connects to a peer discovered via shout discovery
func ConnectToDevice(ctx context.Context, dev shout.DiscoveredDevice, codephrase string, localID utils.PeerIdentity) (*conn.Connection, error) {
	phrase := codephrase
	if phrase == "" {
		phrase = dev.Codephrase
	}
	if phrase == "" {
		return nil, errors.New("transport: codephrase required to connect to device")
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

	// If primary failed, try alternative endpoints
	for _, alt := range dev.Endpoints {
		if alt == targetAddr {
			continue
		}
		cfg.TargetAddr = alt
		if altConn, altErr := conn.Connect(ctx, cfg); altErr == nil {
			return altConn, nil
		}
	}

	return nil, fmt.Errorf("transport: connect to device %s (%s): %w", dev.DeviceName, targetAddr, err)
}

// SendToDevice connects to a discovered shout peer and transmits a file
func SendToDevice(ctx context.Context, dev shout.DiscoveredDevice, codephrase string, localID utils.PeerIdentity, filePath string, onProgress ProgressCallback) error {
	c, err := ConnectToDevice(ctx, dev, codephrase, localID)
	if err != nil {
		return err
	}
	defer c.Close()

	reader, err := transfer.NewReader(ctx, filePath)
	if err != nil {
		return fmt.Errorf("transport: open reader: %w", err)
	}
	defer reader.Close()

	return SendFile(ctx, c, reader, onProgress)
}

// ReceiveFromDevice connects to a discovered shout peer and downloads a file
func ReceiveFromDevice(ctx context.Context, dev shout.DiscoveredDevice, codephrase string, localID utils.PeerIdentity, saveDir string, onProgress ProgressCallback) (*transfer.FileMetadata, error) {
	c, err := ConnectToDevice(ctx, dev, codephrase, localID)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	return ReceiveFile(ctx, c, saveDir, onProgress)
}
