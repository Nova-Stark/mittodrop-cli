package transfer_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"

	"mittodrop/internal/transfer"
)

func TestTransfer_CompressibleFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source_repetitive.txt")
	dstDir := filepath.Join(tmpDir, "downloads")

	// 1. Create a 2 MB repetitive text file
	pattern := []byte("The quick brown fox jumps over the lazy dog. Mittodrop fast peer-to-peer file transfer.\n")
	totalSize := 2 * 1024 * 1024
	srcData := make([]byte, totalSize)
	for i := 0; i < totalSize; i += len(pattern) {
		copy(srcData[i:], pattern)
	}

	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()

	// 2. Initialize Reader with 256 KB chunk size
	r, err := transfer.NewReader(ctx, srcPath, transfer.ReaderConfig{ChunkSize: 256 * 1024})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	meta := r.Metadata()
	if meta.Size != int64(totalSize) {
		t.Fatalf("meta.Size = %d, want %d", meta.Size, totalSize)
	}

	// 3. Initialize Writer
	w, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	compressedChunks := 0
	totalChunks := 0

	// 4. Pipe chunks through the Reader -> Writer pipeline
	for {
		chunk, err := r.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}

		totalChunks++
		if chunk.IsCompressed {
			compressedChunks++
			if chunk.CompSize >= chunk.RawSize {
				t.Fatalf("chunk marked compressed but CompSize %d >= RawSize %d", chunk.CompSize, chunk.RawSize)
			}
		}

		if err := w.WriteChunk(chunk); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		r.ReleaseChunk(chunk)
	}

	if compressedChunks == 0 {
		t.Fatal("expected repetitive text chunks to be compressed, but 0 were compressed")
	}

	// 5. Finalize and verify checksum
	if err := w.Finish(meta.Checksum); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// 6. Verify written file on disk matches source byte-for-byte
	dstData, err := os.ReadFile(w.TargetPath())
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}

	if !bytes.Equal(srcData, dstData) {
		t.Fatal("destination file contents do not match source")
	}

	// Verify staging file was renamed
	if _, err := os.Stat(w.StagingPath()); !os.IsNotExist(err) {
		t.Fatalf("expected staging file %s to be removed after finish", w.StagingPath())
	}
}

func TestTransfer_IncompressibleFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "source_random.bin")
	dstDir := filepath.Join(tmpDir, "downloads")

	// 1. Create a 512 KB high-entropy random file
	totalSize := 512 * 1024
	srcData := make([]byte, totalSize)
	if _, err := rand.Read(srcData); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := context.Background()

	// 2. Initialize Reader with 128 KB chunks
	r, err := transfer.NewReader(ctx, srcPath, transfer.ReaderConfig{ChunkSize: 128 * 1024})
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	meta := r.Metadata()
	w, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	for {
		chunk, err := r.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextChunk: %v", err)
		}

		// Random data should bypass compression
		if chunk.IsCompressed {
			t.Fatalf("chunk %d of high-entropy data was unexpectedly marked compressed", chunk.Index)
		}

		if err := w.WriteChunk(chunk); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
		r.ReleaseChunk(chunk)
	}

	if err := w.Finish(meta.Checksum); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	dstData, err := os.ReadFile(w.TargetPath())
	if err != nil {
		t.Fatalf("ReadFile dst: %v", err)
	}

	if !bytes.Equal(srcData, dstData) {
		t.Fatal("destination file contents do not match source")
	}
}

func TestTransfer_ChecksumMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "test.txt")
	dstDir := filepath.Join(tmpDir, "downloads")

	srcData := []byte("some test content")
	if err := os.WriteFile(srcPath, srcData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, err := transfer.NewReader(context.Background(), srcPath)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	meta := r.Metadata()
	w, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	chunk, err := r.NextChunk()
	if err != nil {
		t.Fatalf("NextChunk: %v", err)
	}

	if err := w.WriteChunk(chunk); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	// Intentionally provide a tampered checksum
	var fakeChecksum [32]byte
	copy(fakeChecksum[:], []byte("bad-checksum-0000000000000000000"))

	err = w.Finish(fakeChecksum)
	if err == nil {
		t.Fatal("expected Finish to fail with checksum mismatch, got nil")
	}

	// Verify staging file was cleaned up on failure
	if _, err := os.Stat(w.StagingPath()); !os.IsNotExist(err) {
		t.Fatalf("expected staging file %s to be deleted after failed finish", w.StagingPath())
	}
	if _, err := os.Stat(w.TargetPath()); !os.IsNotExist(err) {
		t.Fatalf("expected target file %s not to exist after failed finish", w.TargetPath())
	}
}

func TestTransfer_PathTraversalSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "downloads")

	testCases := []struct {
		name         string
		malicious    string
		expectedSafe string
	}{
		{"dotdot", "../../evil.sh", "evil.sh"},
		{"nested_dotdot", "a/b/../../c/d/payload.exe", "payload.exe"},
		{"unix_root", "/etc/shadow", "shadow"},
		{"windows_abs", `C:\Windows\System32\cmd.exe`, "cmd.exe"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			meta := transfer.FileMetadata{
				Name: tc.malicious,
				Size: 10,
			}

			w, err := transfer.NewWriter(dstDir, meta)
			if err != nil {
				t.Fatalf("NewWriter(%q) failed: %v", tc.malicious, err)
			}
			defer w.Abort()

			expectedTarget := filepath.Join(dstDir, tc.expectedSafe)
			if w.TargetPath() != expectedTarget {
				t.Fatalf("TargetPath = %q, want %q", w.TargetPath(), expectedTarget)
			}
		})
	}
}

func TestTransfer_Abort(t *testing.T) {
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "downloads")

	meta := transfer.FileMetadata{
		Name: "aborted.dat",
		Size: 1024,
	}

	w, err := transfer.NewWriter(dstDir, meta)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	stagingPath := w.StagingPath()
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("expected staging file to exist before abort: %v", err)
	}

	if err := w.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Fatalf("expected staging file to be deleted after abort")
	}
}
