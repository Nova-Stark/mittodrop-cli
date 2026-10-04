package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mittodrop/cmd/app"
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
