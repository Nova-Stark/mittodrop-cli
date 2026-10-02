package linkshare

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"mittodrop/internal/sharepage"
	"mittodrop/internal/utils"
)

// ReceiverConfig configures HTTP link sharing receiver.
type ReceiverConfig struct {
	Identity   utils.PeerIdentity
	DeviceID   string // for backwards compatibility
	DeviceName string
	SessionID  string
	SaveDir    string
	Port       int // 0 to auto-pick candidate port
}

// Receiver runs dual-stack HTTP server for direct link uploads.
type Receiver struct {
	cfg        ReceiverConfig
	port       int
	listener   net.Listener
	server     *http.Server
	links      []NetworkLink
	mu         sync.Mutex
	readyCh    chan struct{}
}

// NewReceiver validates config, ensures save directory exists, and prepares receiver.
func NewReceiver(cfg ReceiverConfig) (*Receiver, error) {
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

	_ = cfg.Identity.EnsureValid("")
	cfg.DeviceID = cfg.Identity.DeviceID
	cfg.DeviceName = cfg.Identity.DeviceName
	cfg.SessionID = cfg.Identity.SessionID

	if cfg.DeviceID == "" {
		return nil, fmt.Errorf("linkshare: device_id is required")
	}
	if cfg.DeviceName == "" {
		return nil, fmt.Errorf("linkshare: device_name is required")
	}
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("linkshare: session_id is required")
	}
	if cfg.SaveDir == "" {
		cfg.SaveDir = "."
	}

	if err := os.MkdirAll(cfg.SaveDir, 0755); err != nil {
		return nil, fmt.Errorf("linkshare: create save dir %q: %w", cfg.SaveDir, err)
	}

	var ln net.Listener
	var err error

	if cfg.Port > 0 {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err != nil {
			return nil, fmt.Errorf("linkshare: listen on port %d: %w", cfg.Port, err)
		}
	} else {
		// Try candidate port first
		port, portErr := utils.FindAvailablePort("127.0.0.1")
		if portErr == nil {
			ln, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
		}
		// If candidate failed or was busy, fall back to ephemeral port
		if ln == nil {
			ln, err = net.Listen("tcp", ":0")
			if err != nil {
				return nil, fmt.Errorf("linkshare: bind fallback port: %w", err)
			}
		}
	}

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, fmt.Errorf("linkshare: unable to cast listener to TCPAddr")
	}
	actualPort := tcpAddr.Port

	r := &Receiver{
		cfg:      cfg,
		port:     actualPort,
		listener: ln,
		links:    ResolveLinks(actualPort),
		readyCh:  make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/handshake", r.handleHandshake)
	mux.HandleFunc("/upload", r.handleUpload)
	mux.HandleFunc("/", r.handleRoot)

	r.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  0, // allow infinite stream for giant files
		WriteTimeout: 30 * time.Second,
	}

	return r, nil
}

// Port returns bound port.
func (r *Receiver) Port() int {
	return r.port
}

// Links returns discovered network URLs.
func (r *Receiver) Links() []NetworkLink {
	return r.links
}

// Ready returns channel signaled when server is listening and ready for connections.
func (r *Receiver) Ready() <-chan struct{} {
	return r.readyCh
}

// Start begins dual-stack listening and serves HTTP requests until ctx cancel.
func (r *Receiver) Start(ctx context.Context) error {
	r.mu.Lock()
	ln := r.listener
	close(r.readyCh)
	r.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := r.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = r.server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (r *Receiver) handleRoot(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := sharepage.Render(w, r.cfg.DeviceName); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
