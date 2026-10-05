package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"mittodrop/internal/transport"
)

// Snapshot captures the instantaneous progress metrics of an active transfer.
type Snapshot struct {
	Filename     string
	SenderName   string
	CurrentBytes int64
	TotalBytes   int64
	CurrentChunk uint64
	TotalChunks  uint64
	Percent      float64
	SpeedBytes   float64
	ETA          time.Duration
	Done         bool
}

type sample struct {
	timestamp time.Time
	bytes     int64
}

// Tracker calculates real-time metrics (speed, ETA, percent) for a file transfer.
type Tracker struct {
	mu           sync.Mutex
	filename     string
	senderName   string
	startTime    time.Time
	lastUpdate   time.Time
	currentBytes int64
	totalBytes   int64
	currentChunk uint64
	totalChunks  uint64
	samples      []sample
	windowSize   time.Duration
}

// NewTracker initializes a Tracker for a given file and sender.
func NewTracker(filename, senderName string) *Tracker {
	now := time.Now()
	return &Tracker{
		filename:   filename,
		senderName: senderName,
		startTime:  now,
		lastUpdate: now,
		windowSize: 2 * time.Second,
		samples: []sample{
			{timestamp: now, bytes: 0},
		},
	}
}

// Update records new byte progress and returns the updated Snapshot.
func (t *Tracker) Update(currentBytes, totalBytes int64, currentChunk, totalChunks uint64) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	t.currentBytes = currentBytes
	t.totalBytes = totalBytes
	t.currentChunk = currentChunk
	t.totalChunks = totalChunks
	t.lastUpdate = now

	// Prune samples older than window
	t.samples = append(t.samples, sample{timestamp: now, bytes: currentBytes})
	cutoff := now.Add(-t.windowSize)
	idx := 0
	for i, s := range t.samples {
		if s.timestamp.After(cutoff) {
			idx = i
			break
		}
	}
	if idx > 0 {
		t.samples = t.samples[idx:]
	}

	// Calculate speed from rolling window
	var speed float64
	if len(t.samples) >= 2 {
		first := t.samples[0]
		last := t.samples[len(t.samples)-1]
		elapsed := last.timestamp.Sub(first.timestamp).Seconds()
		if elapsed > 0.05 {
			speed = float64(last.bytes-first.bytes) / elapsed
		}
	}

	// Fallback to overall speed if rolling window too small
	if speed <= 0 {
		totalElapsed := now.Sub(t.startTime).Seconds()
		if totalElapsed > 0.05 {
			speed = float64(currentBytes) / totalElapsed
		}
	}

	// Calculate percentage
	var percent float64
	if totalBytes > 0 {
		percent = (float64(currentBytes) / float64(totalBytes)) * 100.0
		if percent > 100.0 {
			percent = 100.0
		}
	}

	// Calculate ETA
	var eta time.Duration
	remainingBytes := totalBytes - currentBytes
	if speed > 0 && remainingBytes > 0 {
		etaSeconds := float64(remainingBytes) / speed
		eta = time.Duration(etaSeconds * float64(time.Second))
	}

	done := totalBytes > 0 && currentBytes >= totalBytes

	return Snapshot{
		Filename:     t.filename,
		SenderName:   t.senderName,
		CurrentBytes: currentBytes,
		TotalBytes:   totalBytes,
		CurrentChunk: currentChunk,
		TotalChunks:  totalChunks,
		Percent:      percent,
		SpeedBytes:   speed,
		ETA:          eta,
		Done:         done,
	}
}

// AsCallback returns a transport.ProgressCallback function connected to this tracker.
func (t *Tracker) AsCallback(onSnapshot func(Snapshot)) transport.ProgressCallback {
	return func(currentBytes, totalBytes int64, currentChunk, totalChunks uint64) {
		snap := t.Update(currentBytes, totalBytes, currentChunk, totalChunks)
		if onSnapshot != nil {
			onSnapshot(snap)
		}
	}
}

// Render returns a formatted ASCII string for terminal display.
func (s Snapshot) Render(barWidth int) string {
	bar := RenderBar(s.Percent, barWidth)
	speedStr := FormatSpeed(s.SpeedBytes)
	etaStr := FormatDuration(s.ETA)
	curStr := FormatBytes(s.CurrentBytes)
	totStr := FormatBytes(s.TotalBytes)

	senderPrefix := ""
	if s.SenderName != "" {
		senderPrefix = fmt.Sprintf("[%s] ", s.SenderName)
	}

	if s.Done {
		return fmt.Sprintf("[DONE] %s%s (100%%, %s)", senderPrefix, s.Filename, totStr)
	}

	return fmt.Sprintf("[TRANSFER] %s%s %s %5.1f%%  %9s  ETA: %-7s (%s / %s)",
		senderPrefix, s.Filename, bar, s.Percent, speedStr, etaStr, curStr, totStr)
}

// MultiPrinter handles thread-safe terminal status and progress printing without emoji.
type MultiPrinter struct {
	mu        sync.Mutex
	promptMu  sync.Mutex
	out       io.Writer
	in        io.Reader
	prompting bool
	buffer    []string
}

// NewMultiPrinter creates a MultiPrinter writing to stdout.
func NewMultiPrinter(out ...io.Writer) *MultiPrinter {
	w := io.Writer(os.Stdout)
	if len(out) > 0 && out[0] != nil {
		w = out[0]
	}
	return &MultiPrinter{out: w}
}

// PrintStatus prints an informational status message.
func (p *MultiPrinter) PrintStatus(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	line := fmt.Sprintf("[STATUS] %s\n", msg)
	if p.prompting {
		p.buffer = append(p.buffer, line)
		return
	}
	fmt.Fprint(p.out, line)
}

// PrintConnect prints when a client connects.
func (p *MultiPrinter) PrintConnect(sender, clientAddr string, authed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	authNote := "valid token"
	if !authed {
		authNote = "unauthenticated"
	}
	line := fmt.Sprintf("[CONNECT] Client %s (%s) connected (%s).\n", clientAddr, sender, authNote)
	if p.prompting {
		p.buffer = append(p.buffer, line)
		return
	}
	fmt.Fprint(p.out, line)
}

// PrintComplete prints when a file transfer successfully completes.
func (p *MultiPrinter) PrintComplete(sender, filename, savePath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	senderPrefix := ""
	if sender != "" {
		senderPrefix = fmt.Sprintf("[%s] ", sender)
	}
	line := fmt.Sprintf("[COMPLETE] %s%s saved -> %s (SHA-256 OK)\n", senderPrefix, filename, savePath)
	if p.prompting {
		p.buffer = append(p.buffer, line)
		return
	}
	fmt.Fprint(p.out, line)
}

// PrintError prints an error tag message.
func (p *MultiPrinter) PrintError(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	line := fmt.Sprintf("[ERROR] %s\n", msg)
	if p.prompting {
		p.buffer = append(p.buffer, line)
		return
	}
	fmt.Fprint(p.out, line)
}

// PrintSnapshot prints an active transfer progress snapshot.
func (p *MultiPrinter) PrintSnapshot(snap Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prompting {
		// When user is reading/answering a prompt, don't write progress updates over prompt
		return
	}
	fmt.Fprintf(p.out, "\r%s", snap.Render(20))
	if snap.Done {
		fmt.Fprintln(p.out)
	}
}

