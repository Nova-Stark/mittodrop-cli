package utils

import (
	"strings"
	"testing"
)

func TestGenerateSessionID_NonEmpty(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestGenerateSessionID_Randomness(t *testing.T) {
	id1, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id2, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id1 == id2 {
		t.Errorf("two session IDs should not collide: %q == %q", id1, id2)
	}
}

func TestGenerateSessionID_UUIDFormat(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 segments, got %d in %q", len(parts), id)
	}
	for i, want := range []int{8, 4, 4, 4, 12} {
		if len(parts[i]) != want {
			t.Errorf("segment %d: want len %d, got %d (%q)", i, want, len(parts[i]), parts[i])
		}
	}
}

func TestGenerateSessionID_Version7AndVariantBits(t *testing.T) {
	id, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		t.Fatal("malformed UUID")
	}
	// Third segment must start with '7' for UUID v7
	if parts[2][0] != '7' {
		t.Errorf("expected version 7, got %q", parts[2])
	}
	// Fourth segment must start with 8/9/a/b
	c := parts[3][0]
	if c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Errorf("expected RFC 4122 variant, got %q", parts[3])
	}
}
