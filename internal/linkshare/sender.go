package linkshare

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"mittodrop/internal/transfer"
	"mittodrop/internal/utils"
)

const DefaultConnectTimeout = 5 * time.Second

// ClientConfig holds settings for dialing linkshare receiver.
type ClientConfig struct {
	Timeout          time.Duration
	Identity         utils.PeerIdentity
	SenderDeviceID   string
	SenderDeviceName string
	SenderSessionID  string
	SessionKey       [32]byte
}

// PeerSession represents verified connection established with a linkshare receiver.
type PeerSession struct {
	BaseURL     string             `json:"base_url"`
	Receiver    HandshakeResponse  `json:"receiver"`
	Remote      utils.PeerIdentity `json:"remote"`
	SessionKey  [32]byte           `json:"session_key"`
	ConnectedAt time.Time          `json:"connected_at"`
}

// NormalizeURL ensures URL has http scheme, valid host/port, and no trailing slash.
func NormalizeURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("linkshare: empty url")
	}

	// Default to http if scheme omitted
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "http://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("linkshare: invalid url %q: %w", rawURL, err)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("linkshare: missing host in url %q", rawURL)
	}

	// Verify host has port
	_, _, err = net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", fmt.Errorf("linkshare: url %q missing port: %w", rawURL, err)
	}

	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host), nil
}

// Connect dials receiver URL, executes pre-flight handshake, and verifies readiness.
func Connect(ctx context.Context, rawURL string, cfg ClientConfig) (*PeerSession, error) {
	baseURL, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, err
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}

	client := &http.Client{
		Timeout: timeout,
	}

	handshakeURL := baseURL + "/handshake"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, handshakeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("linkshare: create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	senderID := cfg.SenderDeviceID
	senderName := cfg.SenderDeviceName
	senderSession := cfg.SenderSessionID
	if cfg.Identity.DeviceID != "" {
		senderID = cfg.Identity.DeviceID
	}
	if cfg.Identity.DeviceName != "" {
		senderName = cfg.Identity.DeviceName
	}
	if cfg.Identity.SessionID != "" {
		senderSession = cfg.Identity.SessionID
	}

	if senderID != "" {
		req.Header.Set("X-Mittodrop-Sender-ID", senderID)
	}
	if senderName != "" {
		req.Header.Set("X-Mittodrop-Sender-Name", senderName)
	}
	if senderSession != "" {
		req.Header.Set("X-Mittodrop-Sender-Session", senderSession)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linkshare: connect to %s failed: %w", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linkshare: handshake returned status %d from %s", resp.StatusCode, baseURL)
	}

	var hs HandshakeResponse
	if err := json.NewDecoder(resp.Body).Decode(&hs); err != nil {
		return nil, fmt.Errorf("linkshare: invalid handshake response from %s: %w", baseURL, err)
	}

	if hs.DeviceID == "" || hs.DeviceName == "" {
		return nil, fmt.Errorf("linkshare: incomplete peer metadata from %s", baseURL)
	}

	if hs.Status != "ready" {
		return nil, fmt.Errorf("linkshare: peer at %s is not ready (status: %q)", baseURL, hs.Status)
	}

	return &PeerSession{
		BaseURL:  baseURL,
		Receiver: hs,
		Remote: utils.PeerIdentity{
			DeviceID:   hs.DeviceID,
			DeviceName: hs.DeviceName,
			SessionID:  hs.SessionID,
		},
		SessionKey:  cfg.SessionKey,
		ConnectedAt: time.Now(),
	}, nil
}

// UploadOptions configures linkshare file upload parameters.
type UploadOptions struct {
	SessionKey          [32]byte
	DisableCompression bool
	OnProgress          func(bytesSent, totalBytes int64)
}

// UploadFile streams a file from the local filesystem to the remote linkshare receiver.
func (s *PeerSession) UploadFile(ctx context.Context, filePath string, opts UploadOptions) (UploadResult, error) {
	trReader, err := transfer.NewReader(ctx, filePath)
	if err != nil {
		return UploadResult{}, fmt.Errorf("linkshare: read file %q: %w", filePath, err)
	}
	defer trReader.Close()

	meta := trReader.Metadata()
	f, err := os.Open(filePath)
	if err != nil {
		return UploadResult{}, fmt.Errorf("linkshare: open file %q: %w", filePath, err)
	}
	defer f.Close()

	return s.UploadStream(ctx, meta.Name, meta.Size, meta.Checksum, f, opts)
}

// UploadStream streams an arbitrary io.Reader to the receiver with optional Zstandard compression and AES-256-GCM encryption.
func (s *PeerSession) UploadStream(ctx context.Context, name string, size int64, checksum [32]byte, src io.Reader, opts UploadOptions) (UploadResult, error) {
	if s == nil || s.BaseURL == "" {
		return UploadResult{}, errors.New("linkshare: nil or uninitialized peer session")
	}

	key := opts.SessionKey
	var zeroKey [32]byte
	if key == zeroKey {
		key = s.SessionKey
	}

	isEncrypted := key != zeroKey
	useZstd := !opts.DisableCompression

	uploadURL := s.BaseURL + "/upload"

	pr, pw := io.Pipe()

	go func() {
		var pumpErr error
		defer func() {
			if pumpErr != nil {
				_ = pw.CloseWithError(pumpErr)
			} else {
				_ = pw.Close()
			}
		}()

		var currentWriter io.Writer = pw

		// 1. If encrypted, wrap with EncryptWriter first (outer layer)
		var encWriter io.WriteCloser
		if isEncrypted {
			encWriter, pumpErr = NewEncryptWriter(currentWriter, key)
			if pumpErr != nil {
				return
			}
			defer encWriter.Close()
			currentWriter = encWriter
		}

		// 2. If compressed, wrap with ZstdWriter (inner layer)
		var zstdWriter *zstd.Encoder
		if useZstd {
			zstdWriter, pumpErr = zstd.NewWriter(currentWriter)
			if pumpErr != nil {
				return
			}
			defer zstdWriter.Close()
			currentWriter = zstdWriter
		}

		// 3. Pump src into pipeline with progress tracking
		buf := make([]byte, 64*1024)
		var totalSent int64
		for {
			select {
			case <-ctx.Done():
				pumpErr = ctx.Err()
				return
			default:
			}

			n, err := src.Read(buf)
			if n > 0 {
				if _, werr := currentWriter.Write(buf[:n]); werr != nil {
					pumpErr = werr
					return
				}
				totalSent += int64(n)
				if opts.OnProgress != nil {
					opts.OnProgress(totalSent, size)
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				pumpErr = err
				return
			}
		}

		// Flush zstd if active
		if zstdWriter != nil {
			if err := zstdWriter.Close(); err != nil {
				pumpErr = err
				return
			}
		}
		// Flush encWriter if active
		if encWriter != nil {
			if err := encWriter.Close(); err != nil {
				pumpErr = err
				return
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, pr)
	if err != nil {
		_ = pr.Close()
		return UploadResult{}, fmt.Errorf("linkshare: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-File-Name", url.PathEscape(name))
	if size > 0 {
		req.Header.Set("X-File-Size", strconv.FormatInt(size, 10))
	}
	var zeroChecksum [32]byte
	if checksum != zeroChecksum {
		req.Header.Set("X-File-Checksum", hex.EncodeToString(checksum[:]))
	}
	if useZstd {
		req.Header.Set("Content-Encoding", "zstd")
	}
	if isEncrypted {
		req.Header.Set("X-Mittodrop-Encrypted", "true")
	}

	client := &http.Client{
		Timeout: 0, // no timeout for streaming transfer
	}

	resp, err := client.Do(req)
	if err != nil {
		return UploadResult{}, fmt.Errorf("linkshare: upload to %s: %w", uploadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return UploadResult{}, fmt.Errorf("linkshare: upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var results []UploadResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return UploadResult{}, fmt.Errorf("linkshare: decode response: %w", err)
	}
	if len(results) == 0 {
		return UploadResult{Filename: name, Bytes: size}, nil
	}
	return results[0], nil
}
