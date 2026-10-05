package app_test

import (
	"bytes"
	"context"
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
	"tailscale.com/tstest/integration"
)

func TestApp_ShoutSendAndRecIntegration(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "payload.txt")
	_ = os.WriteFile(file1, []byte("hello from app integration test"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, strings.NewReader("y\n"))

	// 1. Run receiver in background
	go func() {
		_ = recApp.Run(ctx, []string{"rec", "-d", dstDir, "-t", "app-token-123"})
	}()

	// Wait for receiver port output
	var receiverPort string
	deadline := time.Now().Add(3 * time.Second)
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
		t.Fatalf("receiver failed to report port. Output:\n%s", recOut.String())
	}

	targetAddr := "127.0.0.1:" + receiverPort

	// 2. Sender transmits file using app runner
	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err := sendApp.Run(ctx, []string{"send", "-f", file1, "-u", targetAddr, "-t", "app-token-123"})
	if err != nil {
		t.Fatalf("sendApp.Run failed: %v", err)
	}

	// 3. Verify file saved and contents match
	savedPath := filepath.Join(dstDir, "payload.txt")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed reading saved file: %v", err)
	}
	if string(data) != "hello from app integration test" {
		t.Fatalf("content mismatch, got %q", string(data))
	}
}

func TestApp_OtinSendAndRecIntegration(t *testing.T) {
	dm := integration.RunDERPAndSTUN(t, t.Logf, "127.0.0.1")
	reg := dm.Regions[1]
	if reg == nil {
		t.Fatal("missing region 1 in test DERP map")
	}

	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "otin_file.txt")
	_ = os.WriteFile(file1, []byte("hello from otin tunnel integration test"), 0644)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	codephrase := "42-guitar-alaska"

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil, app.WithDERPRegion(reg))

	// 1. Run receiver in background
	go func() {
		_ = recApp.Run(ctx, []string{"otin", "rec", "-d", dstDir, "-c", codephrase})
	}()

	// Wait for receiver address output
	var tailcatAddr string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lines := strings.Split(recOut.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Tailcat Address:") {
				parts := strings.Fields(line)
				tailcatAddr = parts[len(parts)-1]
				break
			}
		}
		if tailcatAddr != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if tailcatAddr == "" {
		t.Fatalf("receiver failed to report tailcat address. Output:\n%s", recOut.String())
	}

	// 2. Sender transmits file using otin send
	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err := sendApp.Run(ctx, []string{"otin", "send", "-a", tailcatAddr, "-c", codephrase, "-f", file1})
	if err != nil {
		t.Fatalf("sendApp.Run failed: %v", err)
	}

	// 3. Verify file arrives in dstDir and contents match
	savedPath := filepath.Join(dstDir, "otin_file.txt")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed reading saved file: %v", err)
	}
	if string(data) != "hello from otin tunnel integration test" {
		t.Fatalf("content mismatch, got %q", string(data))
	}
}

func TestApp_OtinRelaySendAndRecIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Start in-process relay server on random port
	relayServer := relay.NewServer()
	go func() {
		_ = relayServer.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	defer relayServer.Close()

	for i := 0; i < 50; i++ {
		if relayServer.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if relayServer.Addr() == nil {
		t.Fatal("relay server failed to bind")
	}
	relayAddr := relayServer.Addr().String()

	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "doc1.txt")
	_ = os.WriteFile(file1, []byte("relay payload document one"), 0644)
	file2 := filepath.Join(srcDir, "doc2.txt")
	_ = os.WriteFile(file2, []byte("relay payload document two"), 0644)

	codephrase := "apple-banana-cherry"

	// 2. Start receiver
	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)

	go func() {
		_ = recApp.Run(ctx, []string{"otin", "rec", "-r", relayAddr, "-c", codephrase, "-d", dstDir})
	}()

	// Wait for receiver to indicate it is ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recOut.String(), "Waiting for senders") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 3. Run sender with multiple files
	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err := sendApp.Run(ctx, []string{"otin", "send", "-r", relayAddr, "-c", codephrase, "-f", file1, file2})
	if err != nil {
		t.Fatalf("sendApp.Run failed: %v", err)
	}

	// 4. Verify both files arrived and match
	data1, err := os.ReadFile(filepath.Join(dstDir, "doc1.txt"))
	if err != nil {
		t.Fatalf("doc1 read: %v", err)
	}
	if string(data1) != "relay payload document one" {
		t.Fatalf("doc1 content mismatch: %q", string(data1))
	}

	data2, err := os.ReadFile(filepath.Join(dstDir, "doc2.txt"))
	if err != nil {
		t.Fatalf("doc2 read: %v", err)
	}
	if string(data2) != "relay payload document two" {
		t.Fatalf("doc2 content mismatch: %q", string(data2))
	}
}

func TestApp_OtinRelayUnreachable(t *testing.T) {
	// Pick an unused port
	deadAddr := "127.0.0.1:59998"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)
	err := recApp.Run(ctx, []string{"otin", "rec", "-r", deadAddr})
	if err == nil {
		t.Fatal("expected receiver to fail on unreachable relay, got nil")
	}

	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)
	srcDir := t.TempDir()
	f := filepath.Join(srcDir, "test.txt")
	_ = os.WriteFile(f, []byte("test"), 0644)

	err = sendApp.Run(ctx, []string{"otin", "send", "-r", deadAddr, "-c", "code", "-f", f})
	if err == nil {
		t.Fatal("expected sender to fail on unreachable relay, got nil")
	}
}

func TestApp_OtinRelayMultiSenderSequential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	relayServer := relay.NewServer()
	go func() {
		_ = relayServer.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	defer relayServer.Close()

	for i := 0; i < 50; i++ {
		if relayServer.Addr() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if relayServer.Addr() == nil {
		t.Fatal("relay server failed to bind")
	}
	relayAddr := relayServer.Addr().String()

	dstDir := t.TempDir()
	srcDir1 := t.TempDir()
	srcDir2 := t.TempDir()

	file1 := filepath.Join(srcDir1, "sender1.txt")
	_ = os.WriteFile(file1, []byte("data from sender one"), 0644)
	file2 := filepath.Join(srcDir2, "sender2.txt")
	_ = os.WriteFile(file2, []byte("data from sender two"), 0644)

	codephrase := "apple-banana-cherry"

	// Start receiver
	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)
	go func() {
		_ = recApp.Run(ctx, []string{"otin", "rec", "-r", relayAddr, "-c", codephrase, "-d", dstDir})
	}()

	// Wait for receiver
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recOut.String(), "Waiting for senders") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 1. Sender 1 transmits
	var send1Out bytes.Buffer
	send1App := app.NewApp(&send1Out, nil)
	err := send1App.Run(ctx, []string{"otin", "send", "-r", relayAddr, "-c", codephrase, "-f", file1})
	if err != nil {
		t.Fatalf("sender 1 failed: %v", err)
	}

	// Brief pause for receiver to reconnect to relay room
	time.Sleep(200 * time.Millisecond)

	// 2. Sender 2 transmits to same receiver
	var send2Out bytes.Buffer
	send2App := app.NewApp(&send2Out, nil)
	err = send2App.Run(ctx, []string{"otin", "send", "-r", relayAddr, "-c", codephrase, "-f", file2})
	if err != nil {
		t.Fatalf("sender 2 failed: %v", err)
	}

	// 3. Verify both files received
	d1, err := os.ReadFile(filepath.Join(dstDir, "sender1.txt"))
	if err != nil {
		t.Fatalf("read sender1.txt: %v", err)
	}
	if string(d1) != "data from sender one" {
		t.Fatalf("sender1.txt mismatch: %q", string(d1))
	}

	d2, err := os.ReadFile(filepath.Join(dstDir, "sender2.txt"))
	if err != nil {
		t.Fatalf("read sender2.txt: %v", err)
	}
	if string(d2) != "data from sender two" {
		t.Fatalf("sender2.txt mismatch: %q", string(d2))
	}
}

func TestApp_RelayServeCommandIntegration(t *testing.T) {
	port, err := utils.FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}

	relayAddr := "127.0.0.1:" + strconv.Itoa(port)
	password := "testpass123"

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. Launch relay server via 'mitto relay serve' command
	var relayOut bytes.Buffer
	relayApp := app.NewApp(&relayOut, nil)

	go func() {
		_ = relayApp.Run(ctx, []string{
			"relay", "serve",
			"-p", strconv.Itoa(port),
			"-h", "127.0.0.1",
			"--pass", password,
		})
	}()

	// Wait for relay port to be open
	var connected bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", relayAddr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			connected = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !connected {
		t.Fatalf("relay server failed to start on %s", relayAddr)
	}

	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "served_file.txt")
	_ = os.WriteFile(file1, []byte("hello from hosted relay serve!"), 0644)

	codephrase := "apple-banana-cherry"

	// 2. Start receiver connecting to this relay with correct password
	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)
	go func() {
		_ = recApp.Run(ctx, []string{
			"otin", "rec",
			"-r", relayAddr,
			"--relay-pass", password,
			"-c", codephrase,
			"-d", dstDir,
		})
	}()

	// Wait for receiver to indicate waiting
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(recOut.String(), "Waiting for senders") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 3. Sender with WRONG password must fail
	var sendFailOut bytes.Buffer
	sendFailApp := app.NewApp(&sendFailOut, nil)
	err = sendFailApp.Run(ctx, []string{
		"otin", "send",
		"-r", relayAddr,
		"--relay-pass", "wrongpass",
		"-c", codephrase,
		"-f", file1,
	})
	if err == nil {
		t.Fatal("expected sender with wrong relay password to fail, got nil")
	}

	// 4. Sender with CORRECT password must succeed
	var sendPassOut bytes.Buffer
	sendPassApp := app.NewApp(&sendPassOut, nil)
	err = sendPassApp.Run(ctx, []string{
		"otin", "send",
		"-r", relayAddr,
		"--relay-pass", password,
		"-c", codephrase,
		"-f", file1,
	})
	if err != nil {
		t.Fatalf("sender with correct password failed: %v", err)
	}

	// 5. Verify file content matches
	savedPath := filepath.Join(dstDir, "served_file.txt")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "hello from hosted relay serve!" {
		t.Fatalf("file content mismatch: got %q", string(data))
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestApp_DirectSendAndRecIntegration(t *testing.T) {
	dstDir := t.TempDir()
	srcDir := t.TempDir()

	file1 := filepath.Join(srcDir, "direct_data.txt")
	_ = os.WriteFile(file1, []byte("direct p2p file transfer payload!"), 0644)

	codephrase := "river-sun-crystal"
	port, err := utils.FindAvailablePort("127.0.0.1")
	if err != nil {
		t.Fatalf("find port: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var recOut bytes.Buffer
	recApp := app.NewApp(&recOut, nil)

	// 1. Run direct receiver in background
	go func() {
		_ = recApp.Run(ctx, []string{
			"direct", "rec",
			"-p", strconv.Itoa(port),
			"-c", codephrase,
			"-d", dstDir,
			"--no-upnp",
		})
	}()

	// Wait for receiver to indicate waiting and extract bound port
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

	// 2. Run direct sender using loopback address
	targetAddr := net.JoinHostPort("127.0.0.1", actualPort)
	var sendOut bytes.Buffer
	sendApp := app.NewApp(&sendOut, nil)

	err = sendApp.Run(ctx, []string{
		"direct", "send",
		"-a", targetAddr,
		"-c", codephrase,
		file1,
	})
	if err != nil {
		t.Fatalf("direct send failed: %v", err)
	}

	// 3. Verify file received cleanly
	savedPath := filepath.Join(dstDir, "direct_data.txt")
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "direct p2p file transfer payload!" {
		t.Fatalf("content mismatch: got %q", string(data))
	}
}




