package transport

import (
	"encoding/binary"
	"errors"
)

// ProgressCallback informs callers of transfer progression
type ProgressCallback func(currentBytes, totalBytes int64, currentChunk, totalChunks uint64)

// ChunkWireHeader size in bytes:
// Index(8) + Offset(8) + RawSize(4) + Compressed(1) = 21 bytes
const ChunkWireHeaderSize = 21

// EncodeChunkWire packs chunk headers into 21 bytes prepended to chunk data
func EncodeChunkWire(index uint64, offset int64, rawSize uint32, compressed bool, data []byte) []byte {
	buf := make([]byte, ChunkWireHeaderSize+len(data))
	binary.BigEndian.PutUint64(buf[0:8], index)
	binary.BigEndian.PutUint64(buf[8:16], uint64(offset))
	binary.BigEndian.PutUint32(buf[16:20], rawSize)
	if compressed {
		buf[20] = 1
	} else {
		buf[20] = 0
	}
	copy(buf[ChunkWireHeaderSize:], data)
	return buf
}

// DecodeChunkWire unpacks chunk header and data slice
func DecodeChunkWire(buf []byte) (index uint64, offset int64, rawSize uint32, compressed bool, data []byte, err error) {
	if len(buf) < ChunkWireHeaderSize {
		return 0, 0, 0, false, nil, errors.New("transport: buffer too small for chunk wire header")
	}
	index = binary.BigEndian.Uint64(buf[0:8])
	offset = int64(binary.BigEndian.Uint64(buf[8:16]))
	rawSize = binary.BigEndian.Uint32(buf[16:20])
	compressed = buf[20] == 1
	data = buf[ChunkWireHeaderSize:]
	return index, offset, rawSize, compressed, data, nil
}
