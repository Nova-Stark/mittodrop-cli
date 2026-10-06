package pake

import (
	"bytes"
	"testing"
)

func TestEdgeRoomID_Deterministic(t *testing.T) {
	phrase := "42-apple-banana"
	r1 := RoomID(phrase)
	r2 := RoomID(phrase)
	if r1 == "" || r1 != r2 {
		t.Fatalf("RoomID should be non-empty and deterministic: %q vs %q", r1, r2)
	}
	if r1 != "42" {
		t.Fatalf("RoomID(%q) = %q, want %q", phrase, r1, "42")
	}

	// Without hyphen, returns verbatim string
	if RoomID("customroom") != "customroom" {
		t.Fatalf("unexpected RoomID for non-hyphenated phrase")
	}
}

func TestEdgePAKE_CorruptedInitiatorBytes(t *testing.T) {
	_, _, err := NewResponder("codephrase", []byte("garbage-bytes-not-a-curve-point"))
	if err == nil {
		t.Fatal("expected error when responder fed invalid initiator bytes, got nil")
	}
}

func TestEdgePAKE_CorruptedResponderBytes(t *testing.T) {
	init, initBytes, err := NewInitiator("secret-phrase")
	if err != nil {
		t.Fatalf("NewInitiator failed: %v", err)
	}
	if len(initBytes) == 0 {
		t.Fatal("empty initiator bytes")
	}

	_, err = init.Finish([]byte("garbage-bytes"))
	if err == nil {
		t.Fatal("expected error when initiator finishes with corrupted responder bytes")
	}
}

func TestEdgePAKE_SpecialCharactersCodephrase(t *testing.T) {
	phrase := "🌟-Ünïcøde-p@ssw0rd!#%^&*()_+"
	init, initBytes, err := NewInitiator(phrase)
	if err != nil {
		t.Fatalf("NewInitiator with unicode failed: %v", err)
	}

	resp, respBytes, err := NewResponder(phrase, initBytes)
	if err != nil {
		t.Fatalf("NewResponder with unicode failed: %v", err)
	}

	keyInit, err := init.Finish(respBytes)
	if err != nil {
		t.Fatalf("Finish failed: %v", err)
	}

	keyResp, err := resp.SessionKey()
	if err != nil {
		t.Fatalf("SessionKey failed: %v", err)
	}

	if !bytes.Equal(keyInit, keyResp) {
		t.Fatalf("derived session keys do not match: %x vs %x", keyInit, keyResp)
	}
}
