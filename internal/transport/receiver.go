package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
)

// ReceiveFile processes incoming file stream over an established conn.Connection
func ReceiveFile(ctx context.Context, c *conn.Connection, saveDir string, onProgress ProgressCallback) (*transfer.FileMetadata, error) {
	if c == nil || c.Conn == nil {
		return nil, errors.New("transport: nil connection")
	}
	return ReceiveFileStream(ctx, c.Conn, c.SessionKey, saveDir, onProgress)
}

// ReceiveFileStream receives encrypted frames over raw net.Conn, verifies, and stages to disk
func ReceiveFileStream(ctx context.Context, netConn net.Conn, sessionKey [32]byte, saveDir string, onProgress ProgressCallback) (*transfer.FileMetadata, error) {
	framer, err := NewFramer(netConn, sessionKey)
	if err != nil {
		return nil, fmt.Errorf("transport: init framer: %w", err)
	}

	// 1. Read metadata frame
	msgType, metaBytes, err := framer.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("transport: read file metadata: %w", err)
	}
	if msgType == MsgAbort {
		return nil, fmt.Errorf("transport: sender aborted before transfer: %s", string(metaBytes))
	}
	if msgType != MsgFileMeta {
		return nil, fmt.Errorf("transport: expected MsgFileMeta, got type %d", msgType)
	}

	var meta transfer.FileMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		_ = framer.WriteFrame(MsgAbort, []byte("invalid metadata json"))
		return nil, fmt.Errorf("transport: unmarshal metadata: %w", err)
	}

	// 2. Prepare staging writer
	writer, err := transfer.NewWriter(saveDir, meta)
	if err != nil {
		_ = framer.WriteFrame(MsgAbort, []byte(err.Error()))
		return nil, fmt.Errorf("transport: init writer: %w", err)
	}
	defer writer.Close()

	// 3. Acknowledge ready to sender
	if err := framer.WriteFrame(MsgFileAck, nil); err != nil {
		return nil, fmt.Errorf("transport: send ready ack: %w", err)
	}

	var receivedBytes int64

	// 4. Stream and process incoming chunk frames
	for {
		select {
		case <-ctx.Done():
			_ = framer.WriteFrame(MsgAbort, []byte("receiver context canceled"))
			return nil, ctx.Err()
		default:
		}

		mType, payload, err := framer.ReadFrame()
		if err != nil {
			return nil, fmt.Errorf("transport: read frame: %w", err)
		}

		if mType == MsgFileDone {
			break
		}

		if mType == MsgAbort {
			return nil, fmt.Errorf("transport: sender aborted transfer: %s", string(payload))
		}

		if mType != MsgChunk {
			_ = framer.WriteFrame(MsgAbort, []byte("unexpected message type"))
			return nil, fmt.Errorf("transport: unexpected message type %d", mType)
		}

		idx, offset, rawSize, compressed, data, err := DecodeChunkWire(payload)
		if err != nil {
			_ = framer.WriteFrame(MsgAbort, []byte("corrupt chunk wire data"))
			return nil, fmt.Errorf("transport: decode chunk wire: %w", err)
		}

		chunk := &transfer.Chunk{
			Index:        idx,
			Offset:       offset,
			RawSize:      rawSize,
			IsCompressed: compressed,
			Data:         data,
		}

		if err := writer.WriteChunk(chunk); err != nil {
			_ = framer.WriteFrame(MsgAbort, []byte(err.Error()))
			return nil, fmt.Errorf("transport: write chunk: %w", err)
		}

		receivedBytes += int64(rawSize)
		if onProgress != nil {
			onProgress(receivedBytes, meta.Size, idx+1, meta.TotalChunks)
		}
	}

	// 5. Verify whole-file SHA-256 and atomically commit staging file
	if err := writer.Finish(meta.Checksum); err != nil {
		_ = framer.WriteFrame(MsgAbort, []byte(err.Error()))
		return nil, fmt.Errorf("transport: integrity verification: %w", err)
	}

	// 6. Send final success ack to sender
	if err := framer.WriteFrame(MsgFileAck, nil); err != nil {
		return nil, fmt.Errorf("transport: send final ack: %w", err)
	}

	return &meta, nil
}
