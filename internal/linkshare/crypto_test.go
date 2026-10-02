package linkshare

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestCrypto_RoundTrip(t *testing.T) {
	var key [32]byte
	_, _ = io.ReadFull(rand.Reader, key[:])

	// 200 KB test payload across multiple 64KB chunk boundaries
	original := make([]byte, 200*1024)
	_, _ = io.ReadFull(rand.Reader, original)

	// Encrypt
	var encBuf bytes.Buffer
	encWriter, err := NewEncryptWriter(&encBuf, key)
	if err != nil {
		t.Fatalf("NewEncryptWriter: %v", err)
	}

	n, err := encWriter.Write(original)
	if err != nil || n != len(original) {
		t.Fatalf("Write: n=%d, err=%v", n, err)
	}
	if err := encWriter.Close(); err != nil {
		t.Fatalf("Close encWriter: %v", err)
	}

	// Decrypt
	decReader, err := NewDecryptReader(&encBuf, key)
	if err != nil {
		t.Fatalf("NewDecryptReader: %v", err)
	}

	decrypted, err := io.ReadAll(decReader)
	if err != nil {
		t.Fatalf("ReadAll decReader: %v", err)
	}

	if !bytes.Equal(original, decrypted) {
		t.Fatalf("decrypted bytes mismatch original (len %d vs %d)", len(decrypted), len(original))
	}
}

func TestCrypto_WrongKey(t *testing.T) {
	var key1, key2 [32]byte
	_, _ = io.ReadFull(rand.Reader, key1[:])
	_, _ = io.ReadFull(rand.Reader, key2[:])

	original := []byte("secret information payload")

	var encBuf bytes.Buffer
	encWriter, err := NewEncryptWriter(&encBuf, key1)
	if err != nil {
		t.Fatalf("NewEncryptWriter: %v", err)
	}
	_, _ = encWriter.Write(original)
	_ = encWriter.Close()

	// Attempt decrypt with wrong key2
	decReader, err := NewDecryptReader(&encBuf, key2)
	if err != nil {
		t.Fatalf("NewDecryptReader: %v", err)
	}

	_, err = io.ReadAll(decReader)
	if err == nil {
		t.Fatal("expected authentication error with wrong key, got nil")
	}
}

func TestCrypto_TamperedPayload(t *testing.T) {
	var key [32]byte
	_, _ = io.ReadFull(rand.Reader, key[:])

	original := []byte("important payload to tamper")

	var encBuf bytes.Buffer
	encWriter, _ := NewEncryptWriter(&encBuf, key)
	_, _ = encWriter.Write(original)
	_ = encWriter.Close()

	raw := encBuf.Bytes()
	// Tamper with last byte
	raw[len(raw)-1] ^= 0xFF

	decReader, _ := NewDecryptReader(bytes.NewReader(raw), key)
	_, err := io.ReadAll(decReader)
	if err == nil {
		t.Fatal("expected authentication error with tampered payload, got nil")
	}
}
