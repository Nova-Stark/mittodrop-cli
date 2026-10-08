package transfer

import (
	"crypto/sha256"
	"io"
	"os"
)

// DefaultChunkSize is 1 MB per chunk, balancing low framing overhead with low memory footprint.
const DefaultChunkSize = 1024 * 1024

// CompressionThreshold defines the minimum size reduction (5%) required to send compressed data.
// If len(compressed) >= 0.95 * len(raw), the raw data is sent instead.
const CompressionThreshold = 0.95

// Chunk represents a discrete unit of file transfer.
type Chunk struct {
	Index        uint64 `json:"index"`         // Sequential chunk index (0-based)
	Offset       int64  `json:"offset"`        // Byte offset within the original file
	RawSize      uint32 `json:"raw_size"`      // Size of the uncompressed data in bytes
	CompSize     uint32 `json:"comp_size"`     // Size of the payload data (if compressed, else equals RawSize)
	IsCompressed bool   `json:"is_compressed"` // True if Data is Zstandard compressed
	Data         []byte `json:"data"`          // Chunk payload (either compressed or raw)
}

// FileMetadata describes the source file attributes and integrity checksum.
type FileMetadata struct {
	Name        string   `json:"name"`         // Base file name (sanitized)
	Size        int64    `json:"size"`         // Total file size in bytes
	Mode        uint32   `json:"mode"`         // File mode / permissions
	ModTime     int64    `json:"mod_time"`     // Unix nano modification timestamp
	Checksum    [32]byte `json:"checksum"`               // SHA-256 checksum of the entire uncompressed file
	ChunkSize   uint32   `json:"chunk_size"`             // Chunk size used for this transfer
	TotalChunks uint64   `json:"total_chunks"`           // Total number of chunks
	IsDir       bool     `json:"is_dir,omitempty"`       // True if transfer is a streaming directory archive
	SenderName  string   `json:"sender_name,omitempty"`  // Display name of sender
	Token       string   `json:"token,omitempty"`        // Authorization token
	BatchID     string   `json:"batch_id,omitempty"`     // Identifier for multi-file batch
	BatchIndex  int      `json:"batch_index,omitempty"`  // Index in batch (1-based)
	BatchTotal  int      `json:"batch_total,omitempty"`  // Total files in batch
}

// CalculateChecksum computes the SHA-256 checksum of an open file.
func CalculateChecksum(f *os.File) ([32]byte, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
