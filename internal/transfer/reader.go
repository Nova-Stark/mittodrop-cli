package transfer

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// ReaderConfig configures the transfer Reader pipeline.
type ReaderConfig struct {
	ChunkSize uint32 // Size of each chunk in bytes (default 1 MB)
	QueueSize int    // Number of in-flight chunks in the ring buffer queue (default 8)
}

// Reader streams and adaptively compresses file chunks with a bounded ring buffer queue.
type Reader struct {
	cfg       ReaderConfig
	file      *os.File
	meta      FileMetadata
	encoder   *zstd.Encoder
	hasher    hash.Hash
	chunkChan chan *Chunk
	errChan   chan error
	ctx       context.Context
	cancel    context.CancelFunc
	rawPool   sync.Pool
	compPool  sync.Pool
	mu        sync.Mutex
	closed    bool
}

// NewReader initializes a Reader for the given file path.
// It inspects file info and begins filling the bounded chunk pipeline in the background.
func NewReader(ctx context.Context, filePath string, cfg ...ReaderConfig) (*Reader, error) {
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

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("transfer: open file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("transfer: stat file: %w", err)
	}
	if info.IsDir() {
		f.Close()
		return nil, errors.New("transfer: directories cannot be read directly as a single file")
	}

	totalChunks := uint64(info.Size() / int64(config.ChunkSize))
	if info.Size()%int64(config.ChunkSize) != 0 || info.Size() == 0 {
		totalChunks++
	}

	// Calculate checksum ahead of transfer
	checksum, err := CalculateChecksum(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("transfer: calculate checksum: %w", err)
	}

	// Seek back to start of file for chunk streaming
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, fmt.Errorf("transfer: seek file: %w", err)
	}

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithZeroFrames(true))
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("transfer: new zstd encoder: %w", err)
	}

	readCtx, cancel := context.WithCancel(ctx)

	r := &Reader{
		cfg:       config,
		file:      f,
		encoder:   enc,
		hasher:    sha256.New(),
		chunkChan: make(chan *Chunk, config.QueueSize),
		errChan:   make(chan error, 1),
		ctx:       readCtx,
		cancel:    cancel,
		meta: FileMetadata{
			Name:        filepath.Base(filePath),
			Size:        info.Size(),
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
				// Allocate up to chunk size + zstd overhead
				b := make([]byte, 0, config.ChunkSize+1024)
				return &b
			},
		},
	}

	go r.fillLoop()
	return r, nil
}

// SetTransferInfo sets sender metadata such as sender name, token, and batch details.
func (r *Reader) SetTransferInfo(senderName, token, batchID string, batchIndex, batchTotal int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.SenderName = senderName
	r.meta.Token = token
	r.meta.BatchID = batchID
	r.meta.BatchIndex = batchIndex
	r.meta.BatchTotal = batchTotal
}

// Metadata returns the file transfer metadata and checksum.
func (r *Reader) Metadata() FileMetadata {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.meta
}

// NextChunk retrieves the next processed chunk from the pipeline.
// Returns io.EOF when all chunks have been read.
func (r *Reader) NextChunk() (*Chunk, error) {
	select {
	case <-r.ctx.Done():
		return nil, r.ctx.Err()
	case err := <-r.errChan:
		return nil, err
	case chunk, ok := <-r.chunkChan:
		if !ok {
			// Check if there was an underlying error recorded
			select {
			case err := <-r.errChan:
				return nil, err
			default:
				return nil, io.EOF
			}
		}
		return chunk, nil
	}
}

// ReleaseChunk returns chunk buffers to the memory pool to reduce GC pressure.
func (r *Reader) ReleaseChunk(c *Chunk) {
	if c == nil || c.Data == nil {
		return
	}
	// Buffers allocated from pools are recycled if capacities match
	if cap(c.Data) >= int(r.cfg.ChunkSize) {
		b := c.Data[:0]
		r.compPool.Put(&b)
	}
}

// Close terminates reading, closes the file handle, and frees resources.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.cancel()
	_ = r.encoder.Close()
	return r.file.Close()
}

func (r *Reader) fillLoop() {
	defer close(r.chunkChan)

	var index uint64
	var offset int64

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		rawBufPtr := r.rawPool.Get().(*[]byte)
		rawBuf := *rawBufPtr

		n, err := io.ReadFull(r.file, rawBuf)
		if n > 0 {
			rawSlice := rawBuf[:n]

			// Compression evaluation
			compBufPtr := r.compPool.Get().(*[]byte)
			compBuf := *compBufPtr
			compressed := r.encoder.EncodeAll(rawSlice, compBuf[:0])

			var chunkData []byte
			var isCompressed bool
			var compSize uint32

			if float64(len(compressed)) < CompressionThreshold*float64(len(rawSlice)) {
				// 5%+ savings: send compressed chunk
				chunkData = make([]byte, len(compressed))
				copy(chunkData, compressed)
				isCompressed = true
				compSize = uint32(len(compressed))
			} else {
				// Incompressible: send raw chunk
				chunkData = make([]byte, n)
				copy(chunkData, rawSlice)
				isCompressed = false
				compSize = uint32(n)
			}

			// Recycle raw and comp scratch buffers
			*compBufPtr = compBuf[:0]
			r.compPool.Put(compBufPtr)
			r.rawPool.Put(rawBufPtr)

			chunk := &Chunk{
				Index:        index,
				Offset:       offset,
				RawSize:      uint32(n),
				CompSize:     compSize,
				IsCompressed: isCompressed,
				Data:         chunkData,
			}

			select {
			case r.chunkChan <- chunk:
			case <-r.ctx.Done():
				return
			}

			offset += int64(n)
			index++
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			r.errChan <- fmt.Errorf("transfer: read error: %w", err)
			return
		}
	}
}
