package ui

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// callWithTimeout runs fn and fails if it panics or does not return in time.
func callWithTimeout(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan interface{}, 1)
	go func() {
		defer func() { done <- recover() }()
		fn()
	}()
	select {
	case r := <-done:
		if r != nil {
			t.Fatalf("panic: %v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return within 2s (hang)")
	}
}

func TestEdgeFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1 << 40, "1.0 TB"},
		{-5, "-5 B"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEdgeFormatBytesPetabyteAndMax(t *testing.T) {
	for _, v := range []int64{1 << 50, math.MaxInt64} {
		v := v
		callWithTimeout(t, func() { _ = FormatBytes(v) })
	}
}

func TestEdgeFormatSpeed(t *testing.T) {
	if got := FormatSpeed(0); got != "0 B/s" {
		t.Errorf("zero: %q", got)
	}
	if got := FormatSpeed(-10); got != "0 B/s" {
		t.Errorf("negative: %q", got)
	}
	if got := FormatSpeed(1536); got != "1.5 KB/s" {
		t.Errorf("1536: %q", got)
	}
	callWithTimeout(t, func() { _ = FormatSpeed(1 << 60) })
	callWithTimeout(t, func() { _ = FormatSpeed(math.NaN()) })
}

func TestEdgeFormatSpeedInf(t *testing.T) {
	callWithTimeout(t, func() { _ = FormatSpeed(math.Inf(1)) })
}

func TestEdgeFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{-time.Second, "0s"},
		{500 * time.Millisecond, "0s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 00s"},
		{65 * time.Second, "1m 05s"},
		{time.Hour, "1h 00m"},
		{25 * time.Hour, "25h 00m"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestEdgeRenderBar(t *testing.T) {
	if got := RenderBar(-50, 10); got != "["+strings.Repeat(" ", 10)+"]" {
		t.Errorf("below 0: %q", got)
	}
	if got := RenderBar(150, 10); got != "["+strings.Repeat("=", 10)+"]" {
		t.Errorf("above 100: %q", got)
	}
	if got := RenderBar(50, 0); len(got) != 22 {
		t.Errorf("width 0 should fall back to 20, got %q", got)
	}
	if got := RenderBar(50, -3); len(got) != 22 {
		t.Errorf("negative width should fall back to 20, got %q", got)
	}
	if got := RenderBar(50, 10); got != "[====>     ]" {
		t.Errorf("50%%: %q", got)
	}
}

func TestEdgeRenderBarNaN(t *testing.T) {
	callWithTimeout(t, func() { _ = RenderBar(math.NaN(), 10) })
}

func TestEdgeTrackerZeroTotal(t *testing.T) {
	tr := NewTracker("f", "s")
	snap := tr.Update(100, 0, 0, 0)
	if snap.Percent != 0 || snap.Done {
		t.Errorf("zero total: %+v", snap)
	}
}

func TestEdgeTrackerCurrentExceedsTotal(t *testing.T) {
	tr := NewTracker("f", "s")
	snap := tr.Update(200, 100, 1, 1)
	if snap.Percent != 100 {
		t.Errorf("percent should clamp to 100, got %v", snap.Percent)
	}
	if !snap.Done {
		t.Error("should be done")
	}
	if snap.ETA != 0 {
		t.Errorf("ETA should be 0, got %v", snap.ETA)
	}
}

func TestEdgeTrackerConcurrentUpdate(t *testing.T) {
	tr := NewTracker("f", "s")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr.Update(int64(i), 100, 0, 0)
		}(i)
	}
	wg.Wait()
}

func TestEdgeTrackerNilCallback(t *testing.T) {
	tr := NewTracker("f", "s")
	cb := tr.AsCallback(nil)
	cb(1, 10, 0, 1)
}

func TestEdgeSnapshotRender(t *testing.T) {
	s := Snapshot{Filename: "", TotalBytes: 0}
	if out := s.Render(20); !strings.Contains(out, "[TRANSFER]") {
		t.Errorf("empty filename: %q", out)
	}
	long := Snapshot{Filename: strings.Repeat("a", 500), TotalBytes: 10, CurrentBytes: 5, Percent: 50}
	_ = long.Render(20)
	done := Snapshot{Filename: "x.bin", SenderName: "bob", TotalBytes: 10, CurrentBytes: 10, Percent: 100, Done: true}
	if out := done.Render(20); !strings.Contains(out, "[DONE] [bob] x.bin") {
		t.Errorf("done: %q", out)
	}
}

func TestEdgeParseChoice(t *testing.T) {
	cases := map[string]PromptChoice{
		"y": ChoiceYes, "Y": ChoiceYes, " yes ": ChoiceYes, "YES": ChoiceYes,
		"a": ChoiceAlways, "ALWAYS": ChoiceAlways,
		"n": ChoiceNo, "": ChoiceNo, "maybe": ChoiceNo, "yy": ChoiceNo,
	}
	for in, want := range cases {
		if got := parseChoice(in); got != want {
			t.Errorf("parseChoice(%q)=%v want %v", in, got, want)
		}
	}
}

func TestEdgePromptEOF(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	p.SetInput(strings.NewReader(""))
	if got := p.Prompt("? "); got != ChoiceNo {
		t.Errorf("EOF should default to no, got %v", got)
	}
}

func TestEdgePromptNoNewline(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	p.SetInput(strings.NewReader("y"))
	if got := p.Prompt("? "); got != ChoiceYes {
		t.Errorf("input without newline: %v", got)
	}
}

func TestEdgePromptCRLF(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	p.SetInput(strings.NewReader("yes\r\n"))
	if got := p.Prompt("? "); got != ChoiceYes {
		t.Errorf("CRLF input: %v", got)
	}
}

func TestEdgePromptBuffersMessagesAndFlushes(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	pr, pw := io.Pipe()
	p.SetInput(pr)
	done := make(chan struct{})
	go func() { p.Prompt("? "); close(done) }()
	time.Sleep(100 * time.Millisecond)
	p.PrintStatus("bg")
	p.PrintSnapshot(Snapshot{Filename: "f"})
	if strings.Contains(out.String(), "[STATUS]") {
		t.Error("status leaked during prompt")
	}
	pw.Write([]byte("n\n"))
	<-done
	if !strings.Contains(out.String(), "[STATUS] bg") {
		t.Errorf("buffered status not flushed: %q", out.String())
	}
	if strings.Contains(out.String(), "[TRANSFER]") {
		t.Error("snapshot should be dropped during prompt")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestEdgePrinterFailingWriter(t *testing.T) {
	p := NewMultiPrinter(failWriter{})
	p.PrintStatus("a")
	p.PrintError("b")
	p.PrintWarn("c")
	p.PrintConnect("s", "addr", false)
	p.PrintComplete("", "f", "p")
	p.PrintSnapshot(Snapshot{Done: true})
}

func TestEdgePrinterNilWriterDefaultsToStdout(t *testing.T) {
	if p := NewMultiPrinter(nil); p.out == nil {
		t.Error("nil writer should default to stdout")
	}
}

func TestEdgePrinterConcurrent(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.PrintStatus("x")
			p.PrintError("y")
		}()
	}
	wg.Wait()
	if n := strings.Count(out.String(), "\n"); n != 100 {
		t.Errorf("expected 100 lines, got %d", n)
	}
}

func TestEdgePrintConnectUnauthenticated(t *testing.T) {
	var out bytes.Buffer
	p := NewMultiPrinter(&out)
	p.PrintConnect("bob", "1.2.3.4:5", false)
	if !strings.Contains(out.String(), "unauthenticated") {
		t.Errorf("%q", out.String())
	}
}
