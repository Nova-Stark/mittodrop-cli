package transfer

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// WriterOption configures a Writer.
type WriterOption func(*Writer)

// WithOverwrite configures whether existing destination files should be overwritten.
func WithOverwrite(overwrite bool) WriterOption {
	return func(w *Writer) {
		w.overwrite = overwrite
	}
}

// Writer stages, decompresses, and writes received chunks to disk atomically.
type Writer struct {
	meta        FileMetadata
	targetPath  string
	stagingPath string
	overwrite   bool
	file        *os.File
	decoder     *zstd.Decoder
	decompPool  sync.Pool
	written     int64
	chunkLens   map[int64]int64 // bytes written per chunk offset; dedups retransmitted chunks
	mu          sync.Mutex
	finished    bool
	closed      bool
	isDuplicate bool
	isRenamed   bool

	// Streaming TAR extraction fields (for IsDir == true)
	isDir       bool
	saveDir     string
	pipeWriter  *io.PipeWriter
	tarDone     chan error
	tarHasher   hash.Hash
}

// NewWriter initializes a Writer for incoming file transfer.
// It verifies destination path safety, creates a unique `.mittodrop` staging file, and pre-allocates disk space.
func NewWriter(saveDir string, meta FileMetadata, opts ...WriterOption) (*Writer, error) {
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

	chunkCap := meta.ChunkSize
	if chunkCap == 0 {
		chunkCap = DefaultChunkSize
	} else if chunkCap > 4*1024*1024 {
		chunkCap = 4 * 1024 * 1024
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("transfer: new zstd decoder: %w", err)
	}

	// If metadata indicates an on-the-fly streaming directory archive (TAR):
	if meta.IsDir {
		pr, pw := io.Pipe()
		tarDone := make(chan error, 1)

		w := &Writer{
			meta:        meta,
			isDir:       true,
			saveDir:     absSaveDir,
			targetPath:  targetPath,
			pipeWriter:  pw,
			tarDone:     tarDone,
			tarHasher:   sha256.New(),
			decoder:     dec,
			chunkLens:   make(map[int64]int64),
			decompPool: sync.Pool{
				New: func() any {
					b := make([]byte, 0, chunkCap)
					return &b
				},
			},
		}
		for _, opt := range opts {
			opt(w)
		}

		go func() {
			tarDone <- extractTarStream(pr, absSaveDir)
		}()

		return w, nil
	}

	// 2. Create unique staging file
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	stagingPath := filepath.Join(absSaveDir, fmt.Sprintf(".mittodrop-%s-%s.tmp", cleanName, hex.EncodeToString(nonce[:])))

	mode := os.FileMode(meta.Mode)
	if mode == 0 {
		mode = 0644
	}

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

	w := &Writer{
		meta:        meta,
		chunkLens:   make(map[int64]int64),
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
	for _, opt := range opts {
		opt(w)
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

// IsDuplicate returns true if the incoming file matched an existing identical file and was deduplicated.
func (w *Writer) IsDuplicate() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isDuplicate
}

// IsRenamed returns true if the incoming file collided with an existing file and was auto-disambiguated.
func (w *Writer) IsRenamed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.isRenamed
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

	if chunk.Offset < 0 {
		return fmt.Errorf("transfer: chunk %d has negative offset %d", chunk.Index, chunk.Offset)
	}
	if w.meta.Size > 0 && chunk.Offset+int64(len(dataToWrite)) > w.meta.Size {
		return fmt.Errorf("transfer: chunk %d (offset %d, len %d) exceeds declared file size %d", chunk.Index, chunk.Offset, len(dataToWrite), w.meta.Size)
	}

	if w.isDir {
		// Feed uncompressed TAR payload into untar stream pipe & update hash
		if _, err := w.pipeWriter.Write(dataToWrite); err != nil {
			return fmt.Errorf("transfer: write tar stream: %w", err)
		}
		w.tarHasher.Write(dataToWrite)
	} else {
		if _, err := w.file.WriteAt(dataToWrite, chunk.Offset); err != nil {
			return fmt.Errorf("transfer: write chunk %d at offset %d: %w", chunk.Index, chunk.Offset, err)
		}
	}

	// A retransmitted chunk at the same offset replaces, not adds to, the byte count.
	w.written += int64(len(dataToWrite)) - w.chunkLens[chunk.Offset]
	w.chunkLens[chunk.Offset] = int64(len(dataToWrite))
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

	if w.isDir {
		// Close pipe writer so tar.Reader reaches EOF
		if w.pipeWriter != nil {
			_ = w.pipeWriter.Close()
		}

		// Wait for untar worker to finish
		err := <-w.tarDone
		w.closed = true
		w.finished = true
		if err != nil {
			return fmt.Errorf("transfer: extract tar archive: %w", err)
		}

		var computedChecksum [32]byte
		copy(computedChecksum[:], w.tarHasher.Sum(nil))
		var zeroChecksum [32]byte
		if expectedChecksum != zeroChecksum && !bytes.Equal(computedChecksum[:], expectedChecksum[:]) {
			return fmt.Errorf("transfer: integrity check failed: expected %x, got %x", expectedChecksum, computedChecksum)
		}
		return nil
	}

	// 1. Flush OS page cache to disk
	if err := w.file.Sync(); err != nil {
		w.cleanup()
		return fmt.Errorf("transfer: disk sync: %w", err)
	}

	if w.written > 0 && w.written != w.meta.Size {
		_ = w.file.Truncate(w.written)
	}

	// 2. Validate whole-file checksum on disk
	computedChecksum, err := CalculateChecksum(w.file)
	if err != nil {
		w.cleanup()
		return fmt.Errorf("transfer: compute final checksum: %w", err)
	}

	var zeroChecksum [32]byte
	if expectedChecksum != zeroChecksum && !bytes.Equal(computedChecksum[:], expectedChecksum[:]) {
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

	// 5. Collision handling & atomic commit
	finalPath := w.targetPath
	if !w.overwrite {
		if _, err := os.Stat(finalPath); err == nil {
			// Destination file exists: check if checksum matches
			existingFile, openErr := os.Open(finalPath)
			if openErr == nil {
				existingSum, hashErr := CalculateChecksum(existingFile)
				existingFile.Close()
				if hashErr == nil && bytes.Equal(existingSum[:], computedChecksum[:]) {
					// Identical file already exists on disk; clean up staging file
					_ = os.Remove(w.stagingPath)
					w.isDuplicate = true
					return nil
				}
			}

			// Different content: auto-disambiguate name -> name (1).ext
			ext := filepath.Ext(w.targetPath)
			base := strings.TrimSuffix(w.targetPath, ext)
			for i := 1; ; i++ {
				candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
				if _, err := os.Stat(candidate); os.IsNotExist(err) {
					finalPath = candidate
					w.isRenamed = true
					break
				}
			}
		}
	}
	w.targetPath = finalPath

	if err := os.Rename(w.stagingPath, finalPath); err != nil {
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
	if w.isDir && w.pipeWriter != nil {
		_ = w.pipeWriter.CloseWithError(errors.New("transfer: writer aborted"))
	}
	w.closed = true
	if !w.finished && w.stagingPath != "" {
		_ = os.Remove(w.stagingPath)
	}
}

func extractTarStream(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar read header: %w", err)
		}

		// Zip-slip security: sanitize target path
		cleanName := filepath.Clean(filepath.FromSlash(header.Name))
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("tar path traversal detected: %q", header.Name)
		}

		targetPath := filepath.Join(destDir, cleanName)
		if !strings.HasPrefix(targetPath, destDir) {
			return fmt.Errorf("tar path traversal outside target: %q", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("create dir %q: %w", targetPath, err)
			}

		case tar.TypeReg:
			// Ensure parent dir exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("create parent dir %q: %w", targetPath, err)
			}

			mode := header.FileInfo().Mode()
			if mode == 0 {
				mode = 0644
			}

			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create file %q: %w", targetPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %q: %w", targetPath, err)
			}
			outFile.Close()

			if header.ModTime.Unix() > 0 {
				_ = os.Chtimes(targetPath, header.ModTime, header.ModTime)
			}
		}
	}
}
