package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/conn"
	"mittodrop/internal/shout"
	"mittodrop/internal/transfer"
	"mittodrop/internal/transport"
	"mittodrop/internal/utils"
)

func createTestPayload(t *testing.T, dir, name string, size int64) (string, [32]byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create payload: %v", err)
	}
	defer f.Close()

	h := sha256.New()
	mw := io.MultiWriter(f, h)

	// Mixture of compressible text and random data
	pattern := bytes.Repeat([]byte("mittodrop-integration-test-chunk-pattern-2026\n"), 10)
	var written int64
	for written < size {
		toWrite := int64(len(pattern))
		if written+toWrite > size {
			toWrite = size - written
		}
		n, err := mw.Write(pattern[:toWrite])
		if err != nil {
			t.Fatalf("write payload: %v", err)
		}
		written += int64(n)
	}

	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return path, sum
}

func fileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatalf("hash file: %v", err)
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// TestIntegration_Shout_ReceiverSendsFileToSender tests:
// Node A shouts on LAN and listens for incoming files.
// Node B (shout receiver) discovers Node A and connects back to Node A to upload a file.
func TestIntegration_Shout_ReceiverSendsFileToSender(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	fileSize := int64(1*1024*1024 + 4321) // ~1MB
	srcPath, srcHash := createTestPayload(t, srcDir, "report_from_receiver.pdf", fileSize)

	codephrase := "42-guitar-alaska"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	idA := utils.PeerIdentity{
		DeviceID:   "device-node-a-shout-listener",
		DeviceName: "NodeAListener",
		SessionID:  "session-node-a-111",
	}
	idB := utils.PeerIdentity{
		DeviceID:   "device-node-b-shout-uploader",
		DeviceName: "NodeBUploader",
		SessionID:  "session-node-b-222",
	}

	// 1. Node A starts listening for inbound transfer
	sessionA, err := transport.Listen(ctx, conn.Config{
		Codephrase: codephrase,
		Mode:       conn.ModeManual,
		Identity:   idA,
	})
	if err != nil {
		t.Fatalf("transport.Listen A: %v", err)
	}
	defer sessionA.Close()

	portA := sessionA.Listener().Endpoints()[0].Address
	_, portStr, err := net.SplitHostPort(portA)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var actualPort int
	fmt.Sscanf(portStr, "%d", &actualPort)

	// 2. Node B runs discovery receiver
	receiverB, err := shout.NewReceiver(shout.ReceiverConfig{
		SelfDeviceID:  idB.DeviceID,
		SelfSessionID: idB.SessionID,
	})
	if err != nil {
		t.Fatalf("shout.NewReceiver: %v", err)
	}

	// Simulate discovery beacon arrival (loopback safe across all CI/OS firewalls)
	beaconMsg := &shout.ShoutMessage{
		DeviceID:     idA.DeviceID,
		DeviceName:   idA.DeviceName,
		SessionID:    idA.SessionID,
		InterfaceIP:  "127.0.0.1",
		TransferPort: actualPort,
	}
	beaconBytes, err := beaconMsg.EncodeMsgpack()
	if err != nil {
		t.Fatalf("encode beacon: %v", err)
	}
	decodedBeacon, err := shout.DecodeMsgpack(beaconBytes)
	if err != nil {
		t.Fatalf("decode beacon: %v", err)
	}

	// Node B processes beacon from Node A
	receiverB.ProcessBeacon(decodedBeacon)

	devices := receiverB.Devices()
	if len(devices) == 0 {
		t.Fatal("expected at least 1 device discovered by receiverB")
	}

	discoveredDev := devices[0]
	t.Logf("Node B discovered peer: %s at %s (port %d)", discoveredDev.DeviceName, discoveredDev.InterfaceIP, discoveredDev.TransferPort)

	var sErr, rErr error
	var metaA *transfer.FileMetadata
	var wg sync.WaitGroup
	wg.Add(2)

	// Node A accepts connection and receives incoming file
	go func() {
		defer wg.Done()
		metaA, rErr = sessionA.AcceptAndReceive(ctx, dstDir, nil)
	}()

	// Node B connects to discovered Node A and sends the file
	go func() {
		defer wg.Done()
		time.Sleep(40 * time.Millisecond)
		sErr = shout.SendToDevice(ctx, discoveredDev, codephrase, idB, srcPath, nil)
	}()

	wg.Wait()

	if sErr != nil {
		t.Fatalf("Node B send failed: %v", sErr)
	}
	if rErr != nil {
		t.Fatalf("Node A receive failed: %v", rErr)
	}

	if metaA == nil {
		t.Fatal("expected non-nil file metadata on Node A")
	}

	dstPath := filepath.Join(dstDir, metaA.Name)
	dstHash := fileHash(t, dstPath)
	if !bytes.Equal(srcHash[:], dstHash[:]) {
		t.Fatalf("file hash mismatch: src=%x, dst=%x", srcHash, dstHash)
	}
	t.Logf("Success: Node B connected to shout sender Node A and transferred %d bytes cleanly.", fileSize)
}
