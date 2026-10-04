package ui_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"mittodrop/internal/ui"
)

func TestUI_FormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{10 * 1024 * 1024, "10.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	}

	for _, tc := range tests {
		got := ui.FormatBytes(tc.bytes)
		if got != tc.expected {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.bytes, got, tc.expected)
		}
	}
}

func TestUI_FormatSpeed(t *testing.T) {
	tests := []struct {
		speed    float64
		expected string
	}{
		{0, "0 B/s"},
		{512, "512 B/s"},
		{1024 * 1024, "1.0 MB/s"},
		{25.4 * 1024 * 1024, "25.4 MB/s"},
	}

	for _, tc := range tests {
		got := ui.FormatSpeed(tc.speed)
		if got != tc.expected {
			t.Errorf("FormatSpeed(%f) = %q, want %q", tc.speed, got, tc.expected)
		}
	}
}

func TestUI_FormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m 05s"},
		{125 * time.Second, "2m 05s"},
		{3700 * time.Second, "1h 01m"},
	}

	for _, tc := range tests {
		got := ui.FormatDuration(tc.d)
		if got != tc.expected {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.expected)
		}
	}
}

func TestUI_RenderBar(t *testing.T) {
	bar0 := ui.RenderBar(0, 10)
	if bar0 != "[          ]" {
		t.Errorf("RenderBar(0, 10) = %q, want [          ]", bar0)
	}

	bar50 := ui.RenderBar(50, 10)
	if bar50 != "[====>     ]" {
		t.Errorf("RenderBar(50, 10) = %q, want [====>     ]", bar50)
	}

	bar100 := ui.RenderBar(100, 10)
	if bar100 != "[==========]" {
		t.Errorf("RenderBar(100, 10) = %q, want [==========]", bar100)
	}
}

func TestUI_Tracker_WithTransportCallback(t *testing.T) {
	tracker := ui.NewTracker("dataset.bin", "PeerAlice")

	var lastSnap ui.Snapshot
	cb := tracker.AsCallback(func(snap ui.Snapshot) {
		lastSnap = snap
	})

	total := int64(10 * 1024 * 1024) // 10 MB

	// Simulate chunk 1 (2 MB)
	cb(2*1024*1024, total, 1, 5)
	if lastSnap.Percent < 19.9 || lastSnap.Percent > 20.1 {
		t.Errorf("snap.Percent = %f, want ~20", lastSnap.Percent)
	}
	if lastSnap.Done {
		t.Error("snap.Done should be false")
	}

	// Render line check
	rendered := lastSnap.Render(10)
	if !strings.Contains(rendered, "[TRANSFER]") {
		t.Errorf("expected [TRANSFER] prefix, got %q", rendered)
	}
	if !strings.Contains(rendered, "[PeerAlice]") {
		t.Errorf("expected [PeerAlice] sender name, got %q", rendered)
	}
	if !strings.Contains(rendered, "dataset.bin") {
		t.Errorf("expected filename, got %q", rendered)
	}

	// Simulate completion (10 MB)
	cb(total, total, 5, 5)
	if !lastSnap.Done {
		t.Error("snap.Done should be true upon completion")
	}
	doneRendered := lastSnap.Render(10)
	if !strings.Contains(doneRendered, "[DONE]") {
		t.Errorf("expected [DONE] prefix, got %q", doneRendered)
	}
}

func TestUI_MultiPrinter_NoEmojis(t *testing.T) {
	var buf bytes.Buffer
	printer := ui.NewMultiPrinter(&buf)

	printer.PrintStatus("Server started on port %d", 8080)
	printer.PrintConnect("MacBook", "192.168.1.100", true)
	printer.PrintComplete("MacBook", "test.txt", "./downloads/test.txt")

	out := buf.String()

	// Verify ASCII tags exist
	if !strings.Contains(out, "[STATUS]") {
		t.Error("missing [STATUS] tag")
	}
	if !strings.Contains(out, "[CONNECT]") {
		t.Error("missing [CONNECT] tag")
	}
	if !strings.Contains(out, "[COMPLETE]") {
		t.Error("missing [COMPLETE] tag")
	}

	// Verify no emojis in output (ASCII range only)
	for _, r := range out {
		if r > 127 {
			t.Errorf("non-ASCII character found in UI output: %c (unicode %U)", r, r)
		}
	}
}
