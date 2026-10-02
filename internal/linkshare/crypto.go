package linkshare

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
)

const (
	chunkPlaintextSize = 64 * 1024 // 64 KB plaintext chunks
	gcmNonceSize       = 12
	gcmTagSize         = 16
)

type encryptWriter struct {
	dst    io.Writer
	aead   cipher.AEAD
	buf    []byte
	seq    atomic.Uint64
	salt   [4]byte
	closed bool
}

// NewEncryptWriter returns an io.WriteCloser that encrypts data into AES-256-GCM chunks.
func NewEncryptWriter(dst io.Writer, key [32]byte) (io.WriteCloser, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("linkshare cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("linkshare gcm: %w", err)
	}

	w := &encryptWriter{
		dst:  dst,
		aead: aead,
		buf:  make([]byte, 0, chunkPlaintextSize),
	}
	if _, err := io.ReadFull(rand.Reader, w.salt[:]); err != nil {
		return nil, fmt.Errorf("linkshare rand salt: %w", err)
	}
	return w, nil
}

func (w *encryptWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("linkshare: writer is closed")
	}
	total := len(p)
	for len(p) > 0 {
		room := chunkPlaintextSize - len(w.buf)
		if room > len(p) {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
		p = p[room:]

		if len(w.buf) == chunkPlaintextSize {
			if err := w.flushChunk(); err != nil {
				return 0, err
			}
		}
	}
	return total, nil
}

func (w *encryptWriter) flushChunk() error {
	if len(w.buf) == 0 {
		return nil
	}
	seq := w.seq.Add(1)
	var nonce [gcmNonceSize]byte
	binary.BigEndian.PutUint64(nonce[0:8], seq)
	copy(nonce[8:12], w.salt[:])

	ciphertext := w.aead.Seal(nil, nonce[:], w.buf, nil)
	payloadLen := uint32(len(nonce) + len(ciphertext))

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], payloadLen)

	if _, err := w.dst.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.dst.Write(nonce[:]); err != nil {
		return err
	}
	if _, err := w.dst.Write(ciphertext); err != nil {
		return err
	}

	w.buf = w.buf[:0]
	return nil
}

func (w *encryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.flushChunk()
}

type decryptReader struct {
	src    io.Reader
	aead   cipher.AEAD
	buf    []byte
	offset int
	eof    bool
}

// NewDecryptReader returns an io.Reader that reads and decrypts AES-256-GCM chunks from src.
func NewDecryptReader(src io.Reader, key [32]byte) (io.Reader, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("linkshare cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("linkshare gcm: %w", err)
	}
	return &decryptReader{
		src:  src,
		aead: aead,
	}, nil
}

func (r *decryptReader) Read(p []byte) (int, error) {
	if r.offset < len(r.buf) {
		n := copy(p, r.buf[r.offset:])
		r.offset += n
		return n, nil
	}

	if r.eof {
		return 0, io.EOF
	}

	var header [4]byte
	if _, err := io.ReadFull(r.src, header[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			r.eof = true
			return 0, io.EOF
		}
		return 0, fmt.Errorf("read frame header: %w", err)
	}

	payloadLen := binary.BigEndian.Uint32(header[:])
	if payloadLen < gcmNonceSize+gcmTagSize || payloadLen > 10*1024*1024 {
		return 0, fmt.Errorf("invalid frame payload length: %d", payloadLen)
	}

	frameBuf := make([]byte, payloadLen)
	if _, err := io.ReadFull(r.src, frameBuf); err != nil {
		return 0, fmt.Errorf("read frame payload: %w", err)
	}

	nonce := frameBuf[:gcmNonceSize]
	ciphertext := frameBuf[gcmNonceSize:]

	plaintext, err := r.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return 0, fmt.Errorf("gcm decrypt failed: %w", err)
	}

	r.buf = plaintext
	r.offset = 0

	n := copy(p, r.buf[r.offset:])
	r.offset += n
	return n, nil
}
