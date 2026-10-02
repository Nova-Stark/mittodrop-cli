package transfer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Writer stages, decompresses, and writes received chunks to disk atomically.
type Writer struct {
	meta        FileMetadata
	targetPath  string
	stagingPath string
	file        *os.File
	decoder     *zstd.Decoder
	decompPool  sync.Pool
	written     int64
	mu          sync.Mutex
	finished    bool
	closed      bool
}

// NewWriter initializes a Writer for incoming file transfer.
// It verifies destination path safety, creates a `.mittodrop` staging file, and pre-allocates disk space.
func NewWriter(saveDir string, meta FileMetadata) (*Writer, error) {
	if meta.Name == "" {
		return nil, errors.New("transfer: empty file name in metadata")
	}

	// 1. Sanitize file name to prevent directory traversal
	cleanName := filepath.Base(filepath.Clean(meta.Name))
	if cleanName == "." || cleanName == "/" || cleanName == "\\" {
		return nil, errors.New("transfer: invalid target file name")
	}

	absSaveDir, err := filepath.Abs(saveDir)
	if err != nil {
		return nil, fmt.Errorf("transfer: abs save dir: %w", err)
	}

	targetPath := filepath.Join(absSaveDir, cleanName)
	if !strings.HasPrefix(targetPath, absSaveDir) {
		return nil, errors.New("transfer: path traversal detected")
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(absSaveDir, 0755); err != nil {
		return nil, fmt.Errorf("transfer: create save dir: %w", err)
	}

	stagingPath := targetPath + ".mittodrop"

	mode := os.FileMode(meta.Mode)
	if mode == 0 {
		mode = 0644
	}

	// 2. Create staging file
	f, err := os.OpenFile(stagingPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, mode)
	if err != nil {
		return nil, fmt.Errorf("transfer: create staging file: %w", err)
	}

	// 3. Pre-allocate disk space
	if meta.Size > 0 {
		if err := f.Truncate(meta.Size); err != nil {
			f.Close()
			_ = os.Remove(stagingPath)
			return nil, fmt.Errorf("transfer: pre-allocate disk space (%d bytes): %w", meta.Size, err)
		}
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		f.Close()
		_ = os.Remove(stagingPath)
		return nil, fmt.Errorf("transfer: new zstd decoder: %w", err)
	}

	chunkCap := meta.ChunkSize
	if chunkCap == 0 {
		chunkCap = DefaultChunkSize
	}

	w := &Writer{
		meta:        meta,
		targetPath:  targetPath,
		stagingPath: stagingPath,
		file:        f,
		decoder:     dec,
		decompPool: sync.Pool{
			New: func() any {
				b := make([]byte, 0, chunkCap)
				return &b
			},
		},
	}

	return w, nil
}

// TargetPath returns the final destination file path.
func (w *Writer) TargetPath() string {
	return w.targetPath
}

// StagingPath returns the current active temporary staging file path.
func (w *Writer) StagingPath() string {
	return w.stagingPath
}

// WriteChunk decompresses (if necessary) and writes the chunk payload at its exact offset.
func (w *Writer) WriteChunk(chunk *Chunk) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("transfer: writer is closed")
	}

	var dataToWrite []byte

	if chunk.IsCompressed {
		// Decompress using Zstandard
		bufPtr := w.decompPool.Get().(*[]byte)
		decompBuf := *bufPtr
		decompressed, err := w.decoder.DecodeAll(chunk.Data, decompBuf[:0])
		if err != nil {
			*bufPtr = decompBuf[:0]
			w.decompPool.Put(bufPtr)
			return fmt.Errorf("transfer: decompress chunk %d: %w", chunk.Index, err)
		}

		if len(decompressed) != int(chunk.RawSize) {
			*bufPtr = decompBuf[:0]
			w.decompPool.Put(bufPtr)
			return fmt.Errorf("transfer: chunk %d decompressed size %d != expected %d", chunk.Index, len(decompressed), chunk.RawSize)
		}

		dataToWrite = decompressed
		defer func() {
			*bufPtr = decompBuf[:0]
			w.decompPool.Put(bufPtr)
		}()
	} else {
		dataToWrite = chunk.Data
		if len(dataToWrite) != int(chunk.RawSize) {
			return fmt.Errorf("transfer: chunk %d raw size %d != expected %d", chunk.Index, len(dataToWrite), chunk.RawSize)
		}
	}

	if _, err := w.file.WriteAt(dataToWrite, chunk.Offset); err != nil {
		return fmt.Errorf("transfer: write chunk %d at offset %d: %w", chunk.Index, chunk.Offset, err)
	}

	w.written += int64(len(dataToWrite))
	return nil
}

// Finish flushes disk buffers, validates whole-file SHA-256 integrity,
// sets original modification timestamp, and atomically renames `.mittodrop` to final file.
func (w *Writer) Finish(expectedChecksum [32]byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("transfer: writer is closed")
	}

	// 1. Flush OS page cache to disk
	if err := w.file.Sync(); err != nil {
		w.cleanup()
		return fmt.Errorf("transfer: disk sync: %w", err)
	}

	// 2. Validate whole-file checksum on disk
	computedChecksum, err := CalculateChecksum(w.file)
	if err != nil {
		w.cleanup()
		return fmt.Errorf("transfer: compute final checksum: %w", err)
	}

	if !bytes.Equal(computedChecksum[:], expectedChecksum[:]) {
		w.cleanup()
		return fmt.Errorf("transfer: integrity check failed: expected %x, got %x", expectedChecksum, computedChecksum)
	}

	// 3. Close staging file handle before rename
	_ = w.file.Close()
	w.closed = true
	w.finished = true

	// 4. Restore original file timestamp if provided
	if w.meta.ModTime > 0 {
		modTime := time.Unix(0, w.meta.ModTime)
		_ = os.Chtimes(w.stagingPath, modTime, modTime)
	}

	// 5. Atomically rename to final destination
	if err := os.Rename(w.stagingPath, w.targetPath); err != nil {
		_ = os.Remove(w.stagingPath)
		return fmt.Errorf("transfer: atomic rename: %w", err)
	}

	return nil
}

// Abort cancels the transfer, closes the file handle, and deletes the staging file.
func (w *Writer) Abort() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.cleanup()
	return nil
}

// Close satisfies io.Closer by aborting uncommitted staging files if not already finished.
func (w *Writer) Close() error {
	return w.Abort()
}

func (w *Writer) cleanup() {
	if w.file != nil {
		_ = w.file.Close()
	}
	w.closed = true
	if !w.finished {
		_ = os.Remove(w.stagingPath)
	}
}
