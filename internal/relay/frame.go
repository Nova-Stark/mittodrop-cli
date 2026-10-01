package relay

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// MagicBytes identifies framed croc/relay protocol messages.
var MagicBytes = []byte("croc")

// MaxMessageSize prevents excessive memory allocation.
const MaxMessageSize = 64 * 1024 * 1024

// WeakKey is the standard default key used for PAKE handshake with the relay server.
var WeakKey = []byte{1, 2, 3}

// WriteFrame sends a length-prefixed frame with the croc magic header:
// [4 bytes "croc"][4 bytes uint32 little-endian length][payload]
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxMessageSize {
		return fmt.Errorf("relay: message size %d exceeds limit %d", len(payload), MaxMessageSize)
	}

	header := make([]byte, 8)
	copy(header[:4], MagicBytes)
	binary.LittleEndian.PutUint32(header[4:], uint32(len(payload)))

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("relay: write header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("relay: write payload: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a length-prefixed frame with the croc magic header.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("relay: read header: %w", err)
	}

	if !bytes.Equal(header[:4], MagicBytes) {
		return nil, errors.New("relay: invalid magic bytes in frame header")
	}

	length := binary.LittleEndian.Uint32(header[4:])
	if length > MaxMessageSize {
		return nil, fmt.Errorf("relay: frame size %d exceeds max %d", length, MaxMessageSize)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("relay: read payload: %w", err)
	}
	return buf, nil
}

// DeriveKey derives a 32-byte AES key from the PAKE session key and salt.
func DeriveKey(sessionKey, salt []byte) []byte {
	return pbkdf2.Key(sessionKey, salt, 100, 32, sha256.New)
}

// Encrypt encrypts plaintext with AES-GCM, prepending a 12-byte random nonce.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("relay: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("relay: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("relay: read nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts an AES-GCM message containing prepended nonce + ciphertext + tag.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("relay: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("relay: new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize+gcm.Overhead() {
		return nil, errors.New("relay: ciphertext too short")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("relay: decrypt failed: %w", err)
	}
	return plaintext, nil
}
