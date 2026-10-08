package transfer

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// DirReader streams a directory as an on-the-fly TAR archive through the transfer Reader pipeline.
type DirReader struct {
	*Reader
	cleanupFn func()
}

// Close closes the underlying Reader and releases any temporary resources.
func (dr *DirReader) Close() error {
	err := dr.Reader.Close()
	if dr.cleanupFn != nil {
		dr.cleanupFn()
	}
	return err
}

// NewDirReader creates a Reader that streams the directory at dirPath as a TAR archive on-the-fly.
func NewDirReader(ctx context.Context, dirPath string, cfg ...ReaderConfig) (*DirReader, error) {
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: abs dir: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: stat dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("transfer: %q is not a directory", dirPath)
	}

	config := ReaderConfig{
		ChunkSize: DefaultChunkSize,
		QueueSize: 8,
	}
	if len(cfg) > 0 {
		if cfg[0].ChunkSize > 0 {
			config.ChunkSize = cfg[0].ChunkSize
		}
		if cfg[0].QueueSize > 0 {
			config.QueueSize = cfg[0].QueueSize
		}
	}

	// 1. Calculate uncompressed TAR size and SHA-256 checksum in a dry run pass
	totalSize, checksum, err := calculateDirTarStats(ctx, absPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: calculate dir tar stats: %w", err)
	}

	totalChunks := uint64(totalSize / int64(config.ChunkSize))
	if totalSize%int64(config.ChunkSize) != 0 || totalSize == 0 {
		totalChunks++
	}

	// 2. Set up live streaming pipe
	pr, pw := io.Pipe()

	streamCtx, cancel := context.WithCancel(ctx)

	// Background worker walks directory and writes TAR stream into pipe
	go func() {
		tw := tar.NewWriter(pw)
		walkErr := walkAndWriteTar(streamCtx, absPath, tw)
		if closeErr := tw.Close(); walkErr == nil {
			walkErr = closeErr
		}
		if walkErr != nil {
			_ = pw.CloseWithError(walkErr)
		} else {
			_ = pw.Close()
		}
	}()

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithZeroFrames(true))
	if err != nil {
		_ = pr.Close()
		cancel()
		return nil, fmt.Errorf("transfer: new zstd encoder: %w", err)
	}

	baseDirName := filepath.Base(absPath)

	r := &Reader{
		cfg:       config,
		source:    pr,
		encoder:   enc,
		hasher:    sha256.New(),
		chunkChan: make(chan *Chunk, config.QueueSize),
		errChan:   make(chan error, 1),
		ctx:       streamCtx,
		cancel:    cancel,
		meta: FileMetadata{
			Name:        baseDirName,
			IsDir:       true,
			Size:        totalSize,
			Mode:        uint32(info.Mode()),
			ModTime:     info.ModTime().UnixNano(),
			Checksum:    checksum,
			ChunkSize:   config.ChunkSize,
			TotalChunks: totalChunks,
		},
		rawPool: sync.Pool{
			New: func() any {
				b := make([]byte, config.ChunkSize)
				return &b
			},
		},
		compPool: sync.Pool{
			New: func() any {
				b := make([]byte, 0, config.ChunkSize+1024)
				return &b
			},
		},
	}

	go r.fillLoop()

	return &DirReader{
		Reader: r,
		cleanupFn: func() {
			_ = pr.Close()
		},
	}, nil
}

func calculateDirTarStats(ctx context.Context, rootDir string) (int64, [32]byte, error) {
	hasher := sha256.New()
	counter := &countingWriter{w: hasher}
	tw := tar.NewWriter(counter)

	if err := walkAndWriteTar(ctx, rootDir, tw); err != nil {
		return 0, [32]byte{}, err
	}
	if err := tw.Close(); err != nil {
		return 0, [32]byte{}, err
	}

	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	return counter.count, sum, nil
}

type countingWriter struct {
	w     io.Writer
	count int64
}

func (cw *countingWriter) Write(p []byte) (n int, err error) {
	n, err = cw.w.Write(p)
	cw.count += int64(n)
	return n, err
}

func walkAndWriteTar(ctx context.Context, rootDir string, tw *tar.Writer) error {
	baseDir := filepath.Dir(rootDir)

	return filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Calculate relative path from parent of rootDir so root folder name is preserved in tar
		relPath, err := filepath.Rel(baseDir, path)
		if err != nil {
			return err
		}
		// Convert to clean slash representation for portable TAR format
		tarName := filepath.ToSlash(relPath)

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("tar header %q: %w", path, err)
		}
		header.Name = tarName

		if d.IsDir() {
			if !strings.HasSuffix(header.Name, "/") {
				header.Name += "/"
			}
			header.Typeflag = tar.TypeDir
			return tw.WriteHeader(header)
		}

		// Regular file
		header.Typeflag = tar.TypeReg
		header.Size = info.Size()

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("write header %q: %w", path, err)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open file %q: %w", path, err)
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("copy file %q to tar: %w", path, err)
		}

		return nil
	})
}
