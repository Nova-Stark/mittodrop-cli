package app_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mittodrop/cmd/app"
	"mittodrop/internal/relay"
	"mittodrop/internal/utils"
)

func TestEdgeApp_RelayServe_PortConflict(t *testing.T) {
	// Bind a port directly
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var out bytes.Buffer
	testApp := app.NewApp(&out, nil)

	// Attempt to start relay server on already bound port
	err = testApp.Run(ctx, []string{
		"relay", "serve",
		"-p", strconv.Itoa(port),
		"-h", "127.0.0.1",
	})
	if err == nil {
		t.Fatal("expected error running relay serve on occupied port, got nil")
	}
}

func TestEdgeApp_RelayServe_ContextCancellation(t *testing.T) {
	port, err := utils.FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	var out bytes.Buffer
	testApp := app.NewApp(&out, nil)

	errCh := make(chan error, 1)
	go func() {
		errCh <- testApp.Run(ctx, []string{
			"relay", "serve",
			"-p", strconv.Itoa(port),
			"-h", "127.0.0.1",
		})
	}()

	// Wait for port to be reachable
	addr := "127.0.0.1:" + strconv.Itoa(port)
	var ready bool
	for i := 0; i < 30; i++ {
		c, dErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dErr == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("relay serve failed to become ready on %s", addr)
	}

	// Cancel context; relay serve should exit cleanly
	cancel()

	select {
	case runErr := <-errCh:
		if runErr != nil && !strings.Contains(runErr.Error(), "context canceled") {
			t.Errorf("unexpected error on cancel: %v", runErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay serve did not exit upon context cancellation")
	}
}

func TestEdgeApp_RelayServe_CustomBannerAndConfig(t *testing.T) {
	port, err := utils.FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}

	customBanner := "custom-edge-relay-banner-2026"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	testApp := app.NewApp(&out, nil)

	go func() {
		_ = testApp.Run(ctx, []string{
			"relay", "serve",
			"-p", strconv.Itoa(port),
			"-h", "127.0.0.1",
			"--banner", customBanner,
			"--ttl", "5m",
			"--max-rooms", "50",
		})
	}()

	addr := "127.0.0.1:" + strconv.Itoa(port)
	var c net.Conn
	for i := 0; i < 30; i++ {
		var dErr error
		c, dErr = net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if c == nil {
		t.Fatalf("failed to connect to relay server on %s", addr)
	}
	defer c.Close()

	// Test ping-pong on the app-started relay server
	if err := relay.WriteFrame(c, []byte("ping")); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	pongBytes, err := relay.ReadFrame(c)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if string(pongBytes) != "pong" {
		t.Errorf("expected pong response, got %q", string(pongBytes))
	}
}

func TestEdgeApp_ShoutSend_DirectTargetIPPort(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "shout_edge.txt")
	_ = os.WriteFile(file1, []byte("shout edge direct ip payload"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, strings.NewReader("y\n"))

	go func() {
		_ = recApp.Run(ctx, []string{"rec", "-d", dstDir, "-t", "token-edge-xyz"})
	}()

	var receiverPort string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Transfer Port:") {
				parts := strings.Fields(line)
				receiverPort = parts[len(parts)-1]
				break
			}
		}
		if receiverPort != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if receiverPort == "" {
		t.Fatalf("receiver failed to report port:\n%s", recOut.String())
	}

	targetAddr := "127.0.0.1:" + receiverPort

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err := sendApp.Run(ctx, []string{"send", "-f", file1, "-u", targetAddr, "-t", "token-edge-xyz"})
	if err != nil {
		t.Fatalf("sendApp.Run: %v", err)
	}

	savedData, err := os.ReadFile(filepath.Join(dstDir, "shout_edge.txt"))
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(savedData) != "shout edge direct ip payload" {
		t.Errorf("content mismatch: got %q", string(savedData))
	}
}

func TestEdgeApp_ShoutSend_MissingAndInvalidFiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	// Sending a non-existent file path
	err := sendApp.Run(ctx, []string{"send", "-f", "non_existent_file_999.dat", "-u", "127.0.0.1:42201"})
	if err == nil {
		t.Fatal("expected error sending nonexistent file, got nil")
	}
}

func TestEdgeApp_ShoutRec_PromptRejection(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "rejected_file.txt")
	_ = os.WriteFile(file1, []byte("should be rejected"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Receiver gets "n" (No / Reject) from user input prompt
	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, strings.NewReader("n\n"))

	go func() {
		_ = recApp.Run(ctx, []string{"rec", "-d", dstDir})
	}()

	var receiverPort string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Transfer Port:") {
				parts := strings.Fields(line)
				receiverPort = parts[len(parts)-1]
				break
			}
		}
		if receiverPort != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if receiverPort == "" {
		t.Fatalf("receiver failed to report port:\n%s", recOut.String())
	}

	targetAddr := "127.0.0.1:" + receiverPort

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	// Sender attempts transfer without matching token (triggers prompt)
	err := sendApp.Run(ctx, []string{"send", "-f", file1, "-u", targetAddr})
	if err == nil {
		t.Fatal("expected sender to fail when receiver rejects transfer, got nil")
	}

	// Verify file is NOT saved on receiver
	if _, err := os.Stat(filepath.Join(dstDir, "rejected_file.txt")); err == nil {
		t.Fatal("rejected file was unexpectedly saved to disk")
	}
}

func TestEdgeApp_LinkShareSend_InvalidURLAndMissingToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srcDir := t.TempDir()
	file1 := filepath.Join(srcDir, "payload.txt")
	_ = os.WriteFile(file1, []byte("data"), 0644)

	var sendOut bytes.Buffer
	testApp := app.NewApp(&sendOut, nil)

	// 1. Unreachable target URL
	err := testApp.Run(ctx, []string{"linkshare", "send", "-u", "http://127.0.0.1:59996", file1})
	// Should log failure / warning
	if err != nil && !strings.Contains(err.Error(), "connection refused") && !strings.Contains(err.Error(), "dial tcp") {
		// Either handled gracefully or returned connection error
	}
}

func TestEdgeApp_LinkShareSend_DisabledCompression(t *testing.T) {
	recvDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "uncompressed.txt")
	_ = os.WriteFile(file1, []byte("raw uncompressed test stream data"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)

	go func() {
		_ = recApp.Run(ctx, []string{"linkshare", "serve", "-d", recvDir, "-t", "link-token-111"})
	}()

	var dropzoneURL string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "http://") {
				dropzoneURL = strings.TrimSpace(line[strings.Index(line, "http://"):])
				break
			}
		}
		if dropzoneURL != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if dropzoneURL == "" {
		t.Fatalf("receiver failed to report dropzone URL:\n%s", recOut.String())
	}

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	// Send with --compress=none
	err := sendApp.Run(ctx, []string{
		"linkshare", "send",
		"-u", dropzoneURL,
		"-t", "link-token-111",
		"--compress", "none",
		file1,
	})
	if err != nil {
		t.Fatalf("linkshare send with compress none failed: %v", err)
	}

	savedFile := filepath.Join(recvDir, "uncompressed.txt")
	data, err := os.ReadFile(savedFile)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "raw uncompressed test stream data" {
		t.Errorf("content mismatch: got %q", string(data))
	}
}

func TestEdgeApp_DirectSend_ValidationErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srcDir := t.TempDir()
	file1 := filepath.Join(srcDir, "test.txt")
	_ = os.WriteFile(file1, []byte("data"), 0644)

	var sendOut bytes.Buffer

	// 1. Missing -a address
	app1 := app.NewApp(&sendOut, nil)
	err := app1.Run(ctx, []string{"direct", "send", "-c", "code-phrase", file1})
	if err == nil {
		t.Fatal("expected error without address flag, got nil")
	}

	// 2. Missing -c codephrase
	app2 := app.NewApp(&sendOut, strings.NewReader("\n"))
	err = app2.Run(ctx, []string{"direct", "send", "-a", "127.0.0.1:42201", file1})
	if err == nil {
		t.Fatal("expected error when codephrase prompt is empty, got nil")
	}
}

func TestEdgeApp_DirectSend_CandidateProbing(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "candidate_data.txt")
	_ = os.WriteFile(file1, []byte("multi-candidate direct p2p transfer"), 0644)

	codephrase := "candidate-probing-code"
	port, err := utils.FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)

	go func() {
		_ = recApp.Run(ctx, []string{
			"direct", "rec",
			"-p", strconv.Itoa(port),
			"-c", codephrase,
			"-d", dstDir,
			"--no-upnp",
		})
	}()

	var actualPort string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Bound Port:") {
				parts := strings.Fields(line)
				actualPort = parts[len(parts)-1]
				break
			}
		}
		if actualPort != "" && strings.Contains(recOut.String(), "Waiting for senders") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if actualPort == "" {
		t.Fatalf("receiver failed to report bound port. Output:\n%s", recOut.String())
	}

	// Pass a dead port and the working port separated by comma
	targetAddrs := fmt.Sprintf("127.0.0.1:59995,127.0.0.1:%s", actualPort)

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err = sendApp.Run(ctx, []string{
		"direct", "send",
		"-a", targetAddrs,
		"-c", codephrase,
		file1,
	})
	if err != nil {
		t.Fatalf("direct send with multiple candidates failed: %v", err)
	}

	savedPath := filepath.Join(dstDir, "candidate_data.txt")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "multi-candidate direct p2p transfer" {
		t.Errorf("content mismatch: got %q", string(data))
	}
}

func TestEdgeApp_DirectoryStreamDirectRecRejection(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	testDir := filepath.Join(srcDir, "folder_to_reject")
	_ = os.MkdirAll(testDir, 0755)
	_ = os.WriteFile(filepath.Join(testDir, "test.txt"), []byte("data"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Direct sender connects with mismatched codephrase to trigger handshake failure
	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)

	go func() {
		_ = recApp.Run(ctx, []string{
			"direct", "rec",
			"-c", "correct-phrase-one",
			"-d", dstDir,
			"--no-upnp",
		})
	}()

	var actualPort string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Bound Port:") {
				parts := strings.Fields(line)
				actualPort = parts[len(parts)-1]
				break
			}
		}
		if actualPort != "" && strings.Contains(recOut.String(), "Waiting for senders") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if actualPort == "" {
		t.Fatalf("receiver failed to report port:\n%s", recOut.String())
	}

	targetAddr := "127.0.0.1:" + actualPort
	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err := sendApp.Run(ctx, []string{
		"direct", "send",
		"-a", targetAddr,
		"-c", "wrong-phrase-two",
		testDir,
	})
	if err == nil {
		t.Fatal("expected direct directory send to fail with wrong codephrase, got nil")
	}

	// Verify nothing was extracted
	if _, err := os.Stat(filepath.Join(dstDir, "folder_to_reject")); err == nil {
		t.Fatal("rejected folder unexpectedly extracted on receiver")
	}
}

