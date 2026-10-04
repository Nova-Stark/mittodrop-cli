package transport

import (
	"context"
	"fmt"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
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

// TunnelAddr returns the Tailcat address string if tunnel mode is active.
func (s *Session) TunnelAddr() string {
	if s.sl != nil {
		return s.sl.TunnelAddr()
	}
	return ""
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
