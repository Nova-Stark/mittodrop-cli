package linkshare

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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
}

// PeerSession represents verified connection established with a linkshare receiver.
type PeerSession struct {
	BaseURL     string             `json:"base_url"`
	Receiver    HandshakeResponse  `json:"receiver"`
	Remote      utils.PeerIdentity `json:"remote"`
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
		ConnectedAt: time.Now(),
	}, nil
}
