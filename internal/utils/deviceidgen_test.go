package utils

import (
	"strings"
	"testing"
)

func TestGenerateDeviceID_NonEmpty(t *testing.T) {
	id, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty device ID")
	}
}

func TestGenerateDeviceID_Deterministic(t *testing.T) {
	id1, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id2, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if id1 != id2 {
		t.Errorf("not deterministic: %q != %q", id1, id2)
	}
}

func TestGenerateDeviceID_UUIDFormat(t *testing.T) {
	id, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 UUID segments, got %d in %q", len(parts), id)
	}
	for i, want := range []int{8, 4, 4, 4, 12} {
		if len(parts[i]) != want {
			t.Errorf("segment %d: want len %d, got %d (%q)", i, want, len(parts[i]), parts[i])
		}
	}
}

func TestGenerateDeviceID_VersionBit(t *testing.T) {
	id, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		t.Fatal("malformed UUID")
	}
	if parts[2][0] != '5' {
		t.Errorf("expected version '5' in third segment, got %q", parts[2])
	}
}

func TestGenerateDeviceID_VariantBit(t *testing.T) {
	id, err := GenerateDeviceID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		t.Fatal("malformed UUID")
	}
	c := parts[3][0]
	if c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Errorf("expected RFC 4122 variant in fourth segment, got %q", parts[3])
	}
}
