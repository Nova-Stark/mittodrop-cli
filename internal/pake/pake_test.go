package pake

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestGenerateCodephrase(t *testing.T) {
	code, err := GenerateCodephrase()
	if err != nil {
		t.Fatalf("GenerateCodephrase failed: %v", err)
	}

	parts := strings.Split(code, "-")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d in %q", len(parts), code)
	}

	// First part must be integer prefix
	num, err := strconv.Atoi(parts[0])
	if err != nil || num < 10 || num > 99 {
		t.Errorf("expected 2-digit number prefix, got %q", parts[0])
	}

	// Second and third parts must be non-empty words
	if len(parts[1]) == 0 || len(parts[2]) == 0 {
		t.Errorf("words cannot be empty: %q, %q", parts[1], parts[2])
	}

	// RoomID extraction
	room := RoomID(code)
	if len(room) != 16 {
		t.Errorf("RoomID(%q) = %q, expected 16-hex length", code, room)
	}
}

func TestGenerateCodephrase_Randomness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		code, err := GenerateCodephrase()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen[code] {
			t.Fatalf("unexpected collision on codephrase generation: %s", code)
		}
		seen[code] = true
	}
}

func TestPAKE_SuccessfulHandshake(t *testing.T) {
	codephrase, err := GenerateCodephrase()
	if err != nil {
		t.Fatalf("GenerateCodephrase: %v", err)
	}

	// 1. Initiator starts
	initiator, initMsg, err := NewInitiator(codephrase)
	if err != nil {
		t.Fatalf("NewInitiator: %v", err)
	}
	if len(initMsg) == 0 {
		t.Fatal("expected non-empty initiator message")
	}

	// 2. Responder answers
	responder, respMsg, err := NewResponder(codephrase, initMsg)
	if err != nil {
		t.Fatalf("NewResponder: %v", err)
	}
	if len(respMsg) == 0 {
		t.Fatal("expected non-empty responder message")
	}

	// 3. Initiator finishes
	initKey, err := initiator.Finish(respMsg)
	if err != nil {
		t.Fatalf("initiator.Finish: %v", err)
	}

	// 4. Responder derives key
	respKey, err := responder.SessionKey()
	if err != nil {
		t.Fatalf("responder.SessionKey: %v", err)
	}

	// 5. Verify both derive identical 256-bit key
	if len(initKey) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(initKey))
	}
	if !bytes.Equal(initKey, respKey) {
		t.Fatalf("session keys do not match:\ninit: %x\nresp: %x", initKey, respKey)
	}
}

func TestPAKE_PasswordMismatch(t *testing.T) {
	codephrase1 := "12-guitar-alaska"
	codephrase2 := "99-different-phrase"

	initiator, initMsg, err := NewInitiator(codephrase1)
	if err != nil {
		t.Fatalf("NewInitiator: %v", err)
	}

	// Responder uses wrong codephrase
	responder, respMsg, err := NewResponder(codephrase2, initMsg)
	if err != nil {
		// Some implementations may fail immediately upon update
		return
	}

	// When initiator updates, it should fail or produce non-matching key
	initKey, err := initiator.Finish(respMsg)
	if err != nil {
		// Expected failure
		return
	}

	respKey, err := responder.SessionKey()
	if err != nil {
		// Expected failure
		return
	}

	if bytes.Equal(initKey, respKey) {
		t.Fatal("security violation: different codephrases produced identical session keys")
	}
}

func TestPAKE_Validation(t *testing.T) {
	_, _, err := NewInitiator("")
	if err == nil {
		t.Error("expected error with empty codephrase for initiator")
	}

	_, _, err = NewResponder("", []byte("test"))
	if err == nil {
		t.Error("expected error with empty codephrase for responder")
	}

	_, _, err = NewResponder("code", nil)
	if err == nil {
		t.Error("expected error with nil initiator bytes for responder")
	}
}
