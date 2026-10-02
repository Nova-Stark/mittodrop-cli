package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"

	"mittodrop/internal/conn"
	"mittodrop/internal/transfer"
)

// SendFile transmits file from Reader over an established conn.Connection
func SendFile(ctx context.Context, c *conn.Connection, reader *transfer.Reader, onProgress ProgressCallback) error {
	if c == nil || c.Conn == nil {
		return errors.New("transport: nil connection")
	}
	return SendFileStream(ctx, c.Conn, c.SessionKey, reader, onProgress)
}

// SendFileStream transmits file over raw net.Conn with authenticated AES-256-GCM framing
func SendFileStream(ctx context.Context, netConn net.Conn, sessionKey [32]byte, reader *transfer.Reader, onProgress ProgressCallback) error {
	if reader == nil {
		return errors.New("transport: nil reader")
	}

	framer, err := NewFramer(netConn, sessionKey)
	if err != nil {
		return fmt.Errorf("transport: init framer: %w", err)
	}

	meta := reader.Metadata()

	// 1. Send file metadata envelope
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("transport: marshal metadata: %w", err)
	}

	if err := framer.WriteFrame(MsgFileMeta, metaBytes); err != nil {
		return fmt.Errorf("transport: send file metadata: %w", err)
	}

	// 2. Wait for receiver ready ack
	msgType, ackPayload, err := framer.ReadFrame()
	if err != nil {
		return fmt.Errorf("transport: wait receiver ready: %w", err)
	}
	if msgType == MsgAbort {
		return fmt.Errorf("transport: receiver rejected transfer: %s", string(ackPayload))
	}
	if msgType != MsgFileAck {
		return fmt.Errorf("transport: unexpected response from receiver (type %d)", msgType)
	}

	var sentBytes int64

	// 3. Stream chunks over encrypted frames
	for {
		select {
		case <-ctx.Done():
			_ = framer.WriteFrame(MsgAbort, []byte("sender context canceled"))
			return ctx.Err()
		default:
		}

		chunk, err := reader.NextChunk()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = framer.WriteFrame(MsgAbort, []byte(err.Error()))
			return fmt.Errorf("transport: read chunk: %w", err)
		}

		wireData := EncodeChunkWire(chunk.Index, chunk.Offset, chunk.RawSize, chunk.IsCompressed, chunk.Data)
		if err := framer.WriteFrame(MsgChunk, wireData); err != nil {
			reader.ReleaseChunk(chunk)
			return fmt.Errorf("transport: write chunk %d: %w", chunk.Index, err)
		}

		sentBytes += int64(chunk.RawSize)
		if onProgress != nil {
			onProgress(sentBytes, meta.Size, chunk.Index+1, meta.TotalChunks)
		}

		reader.ReleaseChunk(chunk)
	}

	// 4. Send completion signal
	if err := framer.WriteFrame(MsgFileDone, nil); err != nil {
		return fmt.Errorf("transport: send file done: %w", err)
	}

	// 5. Wait for receiver verification ack
	msgType, finalPayload, err := framer.ReadFrame()
	if err != nil {
		return fmt.Errorf("transport: wait final ack: %w", err)
	}
	if msgType == MsgAbort {
		return fmt.Errorf("transport: receiver reported integrity verification failed: %s", string(finalPayload))
	}
	if msgType != MsgFileAck {
		return fmt.Errorf("transport: expected final MsgFileAck, got type %d", msgType)
	}

	return nil
}
