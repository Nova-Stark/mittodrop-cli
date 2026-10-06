package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
)

// Wire protocol framing constants
const (
	FrameMagic0 byte = 0x4D // 'M'
	FrameMagic1 byte = 0x44 // 'D'

	// FrameHeaderSize: Magic(2) + Type(1) + PayloadLen(4) + Nonce(12) = 19 bytes
	FrameHeaderSize = 19
	GCMNonceSize    = 12
	GCMTagSize      = 16

	MaxFramePayload = 32 * 1024 * 1024 // 32 MB ceiling
)

// MsgType identifies frame message intent
type MsgType byte

const (
	MsgFileMeta MsgType = 0x01 // File metadata envelope
	MsgChunk    MsgType = 0x02 // File chunk payload
	MsgFileDone MsgType = 0x03 // Sender finished all chunks
	MsgFileAck  MsgType = 0x04 // Receiver ready or verified ack
	MsgAbort    MsgType = 0x05 // Error or abort cancellation
)

// Framer manages AES-256-GCM authenticated frame transport over net.Conn
type Framer struct {
	conn    net.Conn
	aead    cipher.AEAD
	sendSeq atomic.Uint64
	recvSeq atomic.Uint64
	salt    [4]byte
}

// NewFramer creates Framer with hardware-accelerated AES-256-GCM cipher
func NewFramer(conn net.Conn, key [32]byte) (*Framer, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("transport: aes cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("transport: new gcm: %w", err)
	}

	f := &Framer{
		conn: conn,
		aead: aead,
	}

	if _, err := io.ReadFull(rand.Reader, f.salt[:]); err != nil {
		return nil, fmt.Errorf("transport: gen salt: %w", err)
	}

	return f, nil
}

func (f *Framer) nextNonce() [GCMNonceSize]byte {
	seq := f.sendSeq.Add(1)
	var nonce [GCMNonceSize]byte
	binary.BigEndian.PutUint64(nonce[0:8], seq)
	copy(nonce[8:12], f.salt[:])
	return nonce
}

// WriteFrame encrypts plaintext with AES-256-GCM and writes wire frame
func (f *Framer) WriteFrame(msgType MsgType, plaintext []byte) error {
	nonce := f.nextNonce()
	ciphertextLen := len(plaintext) + f.aead.Overhead()
	if ciphertextLen > MaxFramePayload {
		return fmt.Errorf("transport: payload exceeds max size (%d > %d)", ciphertextLen, MaxFramePayload)
	}

	var header [FrameHeaderSize]byte
	header[0] = FrameMagic0
	header[1] = FrameMagic1
	header[2] = byte(msgType)
	binary.BigEndian.PutUint32(header[3:7], uint32(ciphertextLen))
	copy(header[7:19], nonce[:])

	// AAD: unencrypted header bytes 0..7 (Magic + Type + Len)
	aad := header[0:7]
	ciphertext := f.aead.Seal(nil, nonce[:], plaintext, aad)

	if _, err := f.conn.Write(header[:]); err != nil {
		return fmt.Errorf("transport: write header: %w", err)
	}
	if _, err := f.conn.Write(ciphertext); err != nil {
		return fmt.Errorf("transport: write payload: %w", err)
	}

	return nil
}

// ReadFrame reads next frame from connection and decrypts payload
func (f *Framer) ReadFrame() (MsgType, []byte, error) {
	var header [FrameHeaderSize]byte
	if _, err := io.ReadFull(f.conn, header[:]); err != nil {
		return 0, nil, err
	}

	if header[0] != FrameMagic0 || header[1] != FrameMagic1 {
		return 0, nil, errors.New("transport: invalid frame magic")
	}

	msgType := MsgType(header[2])
	payloadLen := binary.BigEndian.Uint32(header[3:7])
	if payloadLen > MaxFramePayload {
		return 0, nil, fmt.Errorf("transport: payload too large (%d)", payloadLen)
	}

	var nonce [GCMNonceSize]byte
	copy(nonce[:], header[7:19])
	seq := binary.BigEndian.Uint64(nonce[0:8])
	lastSeq := f.recvSeq.Load()
	if seq <= lastSeq {
		return 0, nil, fmt.Errorf("transport: replayed or out-of-order frame (seq %d <= %d)", seq, lastSeq)
	}

	ciphertext := make([]byte, payloadLen)
	if _, err := io.ReadFull(f.conn, ciphertext); err != nil {
		return 0, nil, fmt.Errorf("transport: read payload: %w", err)
	}

	aad := header[0:7]
	plaintext, err := f.aead.Open(nil, nonce[:], ciphertext, aad)
	if err != nil {
		return 0, nil, fmt.Errorf("transport: gcm auth decrypt failed: %w", err)
	}
	f.recvSeq.Store(seq)

	return msgType, plaintext, nil
}
