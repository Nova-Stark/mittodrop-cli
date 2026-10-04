package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mittodrop/internal/callsign"
	"mittodrop/internal/cliparser"
	"mittodrop/internal/conn"
	"mittodrop/internal/linkshare"
	"mittodrop/internal/netif"
	"mittodrop/internal/shout"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/ui"
	"mittodrop/internal/utils"
)

// App is the unified CLI runner that stitches together all transfer methods (shout, linkshare).
type App struct {
	printer *ui.MultiPrinter
}

// NewApp creates a new App runner writing output to out and reading interactive input from in.
func NewApp(out io.Writer, in io.Reader) *App {
	p := ui.NewMultiPrinter(out)
	if in != nil {
		p.SetInput(in)
	}
	return &App{printer: p}
}

// Run parses arguments and dispatches to the stitched method runner.
func (a *App) Run(ctx context.Context, args []string) error {
	warnFn := func(msg string) {
		a.printer.PrintWarn("%s", msg)
	}
	cmd, err := cliparser.Parse(args, warnFn)
	if err != nil {
		return err
	}

	switch cmd.Action {
	case cliparser.ActionHelp:
		a.printUsage()
		return nil

	case cliparser.ActionShoutSend:
		return a.runShoutSend(ctx, cmd.ShoutSend)

	case cliparser.ActionShoutRec:
		return a.runShoutRec(ctx, cmd.ShoutRec)

	case cliparser.ActionLinkShareServe:
		return a.runLinkShareServe(ctx, cmd.LinkShareServe)

	case cliparser.ActionLinkShareSend:
		return a.runLinkShareSend(ctx, cmd.LinkShareSend)

	default:
		return fmt.Errorf("unknown action: %s", cmd.Action)
	}
}

func (a *App) runShoutRec(ctx context.Context, cfg *cliparser.ShoutRecConfig) error {
	id, err := generateIdentity()
	if err != nil {
		return fmt.Errorf("shout receiver: generate identity: %w", err)
	}

	saveDir := cfg.Dir
	if saveDir == "" {
		saveDir = "."
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("shout receiver: prepare save dir: %w", err)
	}

	token := cfg.Token
	if token == "" {
		var tokenBytes [3]byte
		_, _ = rand.Read(tokenBytes[:])
		token = hex.EncodeToString(tokenBytes[:])
	}

	session, err := transport.Listen(ctx, conn.Config{
		Mode:          conn.ModeManual,
		PreferredPort: cfg.Port,
		Codephrase:    "mittodrop-lan-v1",
		Identity:      id,
	})
	if err != nil {
		return fmt.Errorf("shout receiver: listen: %w", err)
	}
	defer session.Close()

	actualPort := session.Listener().Port()

	announcer, err := shout.NewAnnouncer(actualPort, id)
	if err != nil {
		return fmt.Errorf("shout receiver: init announcer: %w", err)
	}
	go func() {
		_ = announcer.Start(ctx)
	}()

	a.printer.PrintStatus("Shout Receiver Active")
	a.printer.PrintStatus("Device Name:  %s", id.DeviceName)
	a.printer.PrintStatus("Transfer Port: %d", actualPort)
	a.printer.PrintStatus("Access Token:  %s", token)
	a.printer.PrintStatus("Save Dir:      %s", saveDir)
	a.printer.PrintStatus("Local Network Interfaces:")
	for _, nw := range netif.GetNetworks() {
		for _, item := range nw.IPtems {
			if !item.IsIPv6 {
				a.printer.PrintStatus("  -> %s (%s)", item.IP.String(), nw.InterfaceName)
			}
		}
	}
	a.printer.PrintStatus("Waiting for incoming transfers... (Press Ctrl+C to stop)")

	var (
		trustedMu       sync.Mutex
		trustedDevices  = make(map[string]bool)
		trustedSessions = make(map[string]bool)
		trustedBatches  = make(map[string]bool)
	)

	for {
		c, err := session.Listener().Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}

		go a.handleShoutInbound(ctx, c, saveDir, token, &trustedMu, trustedDevices, trustedSessions, trustedBatches)
	}
}

func (a *App) handleShoutInbound(
	ctx context.Context,
	conn *conn.Connection,
	saveDir, token string,
	trustedMu *sync.Mutex,
	trustedDevices, trustedSessions, trustedBatches map[string]bool,
) {
	defer conn.Close()

	remoteAddr := conn.Conn.RemoteAddr().String()
	senderName := conn.Remote.DeviceName
	if senderName == "" {
		senderName = "Unknown"
	}

	var activeMeta transfer.FileMetadata
	var metaMu sync.Mutex

	acceptHook := func(meta transfer.FileMetadata) bool {
		metaMu.Lock()
		activeMeta = meta
		metaMu.Unlock()

		// 1. Auto-accept if sender presented matching token
		if meta.Token != "" && meta.Token == token {
			a.printer.PrintConnect(senderName, remoteAddr, true)
			return true
		}

		// 2. Auto-accept if already trusted for this session
		trustedMu.Lock()
		trusted := trustedDevices[conn.Remote.DeviceID] ||
			trustedSessions[conn.Remote.SessionID] ||
			(meta.BatchID != "" && trustedBatches[meta.BatchID])
		trustedMu.Unlock()

		if trusted {
			a.printer.PrintConnect(senderName, remoteAddr, true)
			return true
		}

		// 3. Prompt user (MultiPrinter buffers any concurrent logs during prompt)
		a.printer.PrintConnect(senderName, remoteAddr, false)
		batchNote := ""
		if meta.BatchTotal > 1 {
			batchNote = fmt.Sprintf(" [File %d of %d]", meta.BatchIndex, meta.BatchTotal)
		}

		promptText := fmt.Sprintf("\n[INCOMING] %s wants to send %s (%s)%s. Accept? [y/n/a]: ",
			senderName, meta.Name, ui.FormatBytes(meta.Size), batchNote)

		choice := a.printer.Prompt(promptText)

		switch choice {
		case ui.ChoiceAlways:
			trustedMu.Lock()
			trustedDevices[conn.Remote.DeviceID] = true
			trustedSessions[conn.Remote.SessionID] = true
			if meta.BatchID != "" {
				trustedBatches[meta.BatchID] = true
			}
			trustedMu.Unlock()
			return true

		case ui.ChoiceYes:
			if meta.BatchID != "" {
				trustedMu.Lock()
				trustedBatches[meta.BatchID] = true
				trustedMu.Unlock()
			}
			return true

		default:
			return false
		}
	}

	tracker := ui.NewTracker("", senderName)
	progressCallback := func(currentBytes, totalBytes int64, currentChunk, totalChunks uint64) {
		metaMu.Lock()
		name := activeMeta.Name
		metaMu.Unlock()

		snap := tracker.Update(currentBytes, totalBytes, currentChunk, totalChunks)
		snap.Filename = name
		a.printer.PrintSnapshot(snap)
	}

	receivedMeta, err := transport.ReceiveFile(ctx, conn, saveDir, progressCallback, acceptHook)
	if err != nil {
		if strings.Contains(err.Error(), "rejected") {
			a.printer.PrintWarn("Transfer from %s declined.", senderName)
		} else {
			a.printer.PrintWarn("Transfer error from %s: %v", senderName, err)
		}
		return
	}

	savedPath := filepath.Join(saveDir, receivedMeta.Name)
	a.printer.PrintComplete(senderName, receivedMeta.Name, savedPath)
}

func (a *App) runShoutSend(ctx context.Context, cfg *cliparser.ShoutSendConfig) error {
	id, err := generateIdentity()
	if err != nil {
		return fmt.Errorf("shout sender: generate identity: %w", err)
	}

	recv, err := shout.NewReceiver(shout.ReceiverConfig{
		SelfDeviceID:  id.DeviceID,
		SelfSessionID: id.SessionID,
	})
	if err != nil {
		return fmt.Errorf("shout sender: init discovery receiver: %w", err)
	}

	discCtx, cancelDisc := context.WithCancel(ctx)
	defer cancelDisc()
	go func() {
		_ = recv.Start(discCtx)
	}()

	var targetDev shout.DiscoveredDevice

	if cfg.Target != "" {
		if strings.Contains(cfg.Target, ":") {
			host, portStr, splitErr := net.SplitHostPort(cfg.Target)
			if splitErr == nil {
				port, pErr := strconv.Atoi(portStr)
				if pErr == nil && port > 0 {
					targetDev = shout.DiscoveredDevice{
						InterfaceIP:  host,
						TransferPort: port,
						DeviceName:   host,
						Endpoints:    []string{cfg.Target},
					}
				}
			}
		}

		if targetDev.TransferPort == 0 {
			a.printer.PrintStatus("Searching for receiver matching %q...", cfg.Target)
			found := false
			deadline := time.Now().Add(2500 * time.Millisecond)

			for time.Now().Before(deadline) {
				for _, dev := range recv.Devices() {
					if strings.EqualFold(dev.DeviceName, cfg.Target) ||
						strings.EqualFold(dev.InterfaceIP, cfg.Target) ||
						dev.DeviceID == cfg.Target {
						targetDev = dev
						found = true
						break
					}
				}
				if found {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}

			if !found {
				return fmt.Errorf("shout: receiver %q not found on local network", cfg.Target)
			}
		}
	} else {
		a.printer.PrintStatus("Scanning local network for receivers (1.5s)...")
		time.Sleep(1500 * time.Millisecond)

		devices := recv.Devices()
		if len(devices) == 0 {
			return errors.New("shout: no receivers found on local network")
		}

		if len(devices) == 1 {
			targetDev = devices[0]
		} else {
			a.printer.PrintStatus("Discovered receivers:")
			for idx, dev := range devices {
				a.printer.PrintStatus("  [%d] %s (%s)", idx+1, dev.DeviceName, dev.Addr())
			}

			choiceStr := a.printer.PromptString(fmt.Sprintf("Select target [1-%d]: ", len(devices)))
			cleanChoice := strings.TrimSpace(choiceStr)
			num, err := strconv.Atoi(cleanChoice)
			if err != nil || num < 1 || num > len(devices) {
				return fmt.Errorf("shout: invalid selection %q", cleanChoice)
			}
			targetDev = devices[num-1]
		}
	}

	a.printer.PrintStatus("Target selected: %s (%s)", targetDev.DeviceName, targetDev.Addr())

	batchID, _ := utils.GenerateSessionID()
	totalFiles := len(cfg.Files)

	for idx, filePath := range cfg.Files {
		fileBase := filepath.Base(filePath)
		a.printer.PrintStatus("Sending [%d/%d] %s...", idx+1, totalFiles, fileBase)

		reader, err := transfer.NewReader(ctx, filePath)
		if err != nil {
			a.printer.PrintWarn("Failed to read file %s: %v", fileBase, err)
			continue
		}

		reader.SetTransferInfo(id.DeviceName, cfg.Token, batchID, idx+1, totalFiles)

		conn, err := shout.ConnectToDevice(ctx, targetDev, "mittodrop-lan-v1", id)
		if err != nil {
			reader.Close()
			return fmt.Errorf("shout: connect to %s failed: %w", targetDev.DeviceName, err)
		}

		tracker := ui.NewTracker(fileBase, targetDev.DeviceName)
		err = transport.SendFile(ctx, conn, reader, func(curr, tot int64, curChunk, totChunks uint64) {
			snap := tracker.Update(curr, tot, curChunk, totChunks)
			a.printer.PrintSnapshot(snap)
		})
		conn.Close()
		reader.Close()

		if err != nil {
			a.printer.PrintWarn("Transfer failed for %s: %v", fileBase, err)
			return err
		}

		a.printer.PrintComplete("", fileBase, targetDev.DeviceName)
	}

	a.printer.PrintStatus("All transfers completed successfully.")
	return nil
}

func (a *App) runLinkShareServe(ctx context.Context, cfg *cliparser.LinkShareServeConfig) error {
	id, err := generateIdentity()
	if err != nil {
		return fmt.Errorf("linkshare server: generate identity: %w", err)
	}

	saveDir := cfg.Dir
	if saveDir == "" {
		saveDir = "."
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("linkshare server: prepare dir: %w", err)
	}

	recCfg := linkshare.ReceiverConfig{
		Identity:     id,
		SaveDir:      saveDir,
		Port:         cfg.Port,
		Token:        cfg.Token,
		RequireToken: true,
		OnConnect: func(sender, addr string, authed bool) {
			a.printer.PrintConnect(sender, addr, authed)
		},
		OnComplete: func(sender, filename, path string) {
			a.printer.PrintComplete(sender, filename, path)
		},
	}

	receiver, err := linkshare.NewReceiver(recCfg)
	if err != nil {
		return fmt.Errorf("linkshare server: init receiver: %w", err)
	}

	go func() {
		_ = receiver.Start(ctx)
	}()

	<-receiver.Ready()

	token := receiver.Token()
	a.printer.PrintStatus("LinkShare Dropzone Active")
	a.printer.PrintStatus("Access Token: %s", token)
	a.printer.PrintStatus("Save Dir:     %s", saveDir)
	a.printer.PrintStatus("Reachable Dropzone URLs:")
	cleanURLs := receiver.CleanURLs()
	for _, u := range cleanURLs {
		a.printer.PrintStatus("  -> %s", u)
	}
	if len(cleanURLs) > 0 {
		a.printer.PrintStatus("CLI Send Command:")
		a.printer.PrintStatus("  mitto linkshare send -u %s -t %s <files...>", cleanURLs[0], token)
	}
	a.printer.PrintStatus("Waiting for uploads... (Press Ctrl+C to stop)")

	<-ctx.Done()
	return nil
}

func (a *App) runLinkShareSend(ctx context.Context, cfg *cliparser.LinkShareSendConfig) error {
	id, err := generateIdentity()
	if err != nil {
		return fmt.Errorf("linkshare sender: generate identity: %w", err)
	}

	disableComp := (cfg.Compress == "none")

	for _, target := range cfg.Targets {
		a.printer.PrintStatus("Connecting to %s...", target.URL)

		session, err := linkshare.Connect(ctx, target.URL, linkshare.ClientConfig{
			Token:    target.Token,
			Identity: id,
		})
		if err != nil {
			a.printer.PrintWarn("Failed to connect to %s: %v", target.URL, err)
			continue
		}

		targetLabel := session.Receiver.DeviceName
		if targetLabel == "" {
			targetLabel = target.URL
		}

		for idx, filePath := range target.Files {
			fileBase := filepath.Base(filePath)
			stat, err := os.Stat(filePath)
			if err != nil {
				a.printer.PrintWarn("Failed to stat %s: %v", filePath, err)
				continue
			}

			a.printer.PrintStatus("[%d/%d] Uploading %s (%s)...", idx+1, len(target.Files), fileBase, ui.FormatBytes(stat.Size()))

			tracker := ui.NewTracker(fileBase, targetLabel)
			opts := linkshare.UploadOptions{
				DisableCompression: disableComp,
				OnProgress: func(bytesSent, totalBytes int64) {
					snap := tracker.Update(bytesSent, totalBytes, 0, 0)
					a.printer.PrintSnapshot(snap)
				},
			}

			_, err = session.UploadFile(ctx, filePath, opts)
			if err != nil {
				a.printer.PrintWarn("Failed to upload %s to %s: %v", fileBase, target.URL, err)
				return err
			}

			a.printer.PrintComplete(targetLabel, fileBase, target.URL)
		}
	}

	a.printer.PrintStatus("All LinkShare transfers completed.")
	return nil
}

func (a *App) printUsage() {
	fmt.Print(`mittodrop - High-performance encrypted P2P file transfer

Usage:
  mitto send [-u <target>] [-t <token>] -f <files...>
      Send files over LAN (default: multicast shout discovery)
  mitto rec [-d <dir>] [-p <port>] [-t <token>]
      Receive files over LAN (default: multicast shout discovery)

Commands:
  send               Send files over local network (alias for 'shout send')
  rec, receive       Receive files over local network (alias for 'shout rec')

  shout send         Send files using zero-touch LAN multicast discovery
  shout rec          Receive files using zero-touch LAN multicast discovery

  linkshare serve    Host an HTTP dropzone for browser and CLI uploads
  linkshare send     Upload files to a LinkShare dropzone

Options:
  -f, --files        One or more file paths to send
  -u, --target       Target receiver device name or IP
  -t, --token        Authorization token (auto-accepts transfer without prompt)
  -d, --dir          Destination directory for received files (default: .)
  -p, --port         Listening port (default: 0 = automatic)
  -c, --compress     Compression algorithm: zstd | gzip | none (default: zstd)
  -h, --help         Show this help message
`)
}

func generateIdentity() (utils.PeerIdentity, error) {
	devID, err := utils.GenerateDeviceID()
	if err != nil {
		return utils.PeerIdentity{}, err
	}
	sessID, err := utils.GenerateSessionID()
	if err != nil {
		return utils.PeerIdentity{}, err
	}
	name, err := callsign.GenerateDeviceName()
	if err != nil {
		name = devID[:8]
	}
	return utils.PeerIdentity{
		DeviceID:   devID,
		DeviceName: name,
		SessionID:  sessID,
	}, nil
}
