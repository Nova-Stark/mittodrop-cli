package ui_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"mittodrop/internal/ui"
)

func TestUI_Prompt_DefaultToNo(t *testing.T) {
	tests := []struct {
		input    string
		expected ui.PromptChoice
	}{
		{"", ui.ChoiceNo},
		{"   ", ui.ChoiceNo},
		{"n\n", ui.ChoiceNo},
		{"no\n", ui.ChoiceNo},
		{"invalid\n", ui.ChoiceNo},
		{"x\n", ui.ChoiceNo},
		{"y\n", ui.ChoiceYes},
		{"yes\n", ui.ChoiceYes},
		{"YES\n", ui.ChoiceYes},
		{"a\n", ui.ChoiceAlways},
		{"always\n", ui.ChoiceAlways},
		{"ALWAYS\n", ui.ChoiceAlways},
	}

	for _, tc := range tests {
		var out bytes.Buffer
		p := ui.NewMultiPrinter(&out)
		p.SetInput(strings.NewReader(tc.input))

		choice := p.Prompt("Accept? [y/n/a]: ")
		if choice != tc.expected {
			t.Errorf("input %q: expected %v, got %v", tc.input, tc.expected, choice)
		}
	}
}

func TestUI_Prompt_BufferWhilePrompting(t *testing.T) {
	var out bytes.Buffer
	p := ui.NewMultiPrinter(&out)

	// Custom reader that delays providing input
	r, w := newPipe()
	p.SetInput(r)

	var wg sync.WaitGroup
	wg.Add(1)

	var choice ui.PromptChoice
	go func() {
		defer wg.Done()
		choice = p.Prompt("Incoming file. Accept? [y/n/a]: ")
	}()

	// Give prompt goroutine time to lock and start prompting
	time.Sleep(50 * time.Millisecond)

	// Concurrent worker sends notifications while user is thinking
	workerDone := make(chan struct{})
	go func() {
		p.PrintStatus("Background indexing complete")
		p.PrintWarn("Disk space low")
		p.PrintConnect("alice", "192.168.1.50:9001", true)
		p.PrintComplete("alice", "doc.pdf", "/tmp/doc.pdf")
		p.PrintSnapshot(ui.Snapshot{Filename: "bg.zip", Percent: 50.0})
		close(workerDone)
	}()

	<-workerDone

	// At this point, out should ONLY contain the prompt text, nothing else!
	currOutput := out.String()
	if strings.Contains(currOutput, "[STATUS]") ||
		strings.Contains(currOutput, "[WARN]") ||
		strings.Contains(currOutput, "[CONNECT]") ||
		strings.Contains(currOutput, "[COMPLETE]") ||
		strings.Contains(currOutput, "[TRANSFER]") {
		t.Fatalf("background output was printed during prompt! Got:\n%s", currOutput)
	}

	// Now user enters 'y'
	w.Write([]byte("y\n"))
	wg.Wait()

	if choice != ui.ChoiceYes {
		t.Fatalf("expected ChoiceYes, got %v", choice)
	}

	// Now check that all buffered messages were flushed in order
	finalOutput := out.String()
	if !strings.Contains(finalOutput, "[STATUS] Background indexing complete") {
		t.Errorf("missing buffered status in final output: %s", finalOutput)
	}
	if !strings.Contains(finalOutput, "[WARN] Disk space low") {
		t.Errorf("missing buffered warn in final output: %s", finalOutput)
	}
	if !strings.Contains(finalOutput, "[CONNECT] Client 192.168.1.50:9001 (alice) connected (valid token).") {
		t.Errorf("missing buffered connect in final output: %s", finalOutput)
	}
	if !strings.Contains(finalOutput, "[COMPLETE] [alice] doc.pdf saved -> /tmp/doc.pdf (SHA-256 OK)") {
		t.Errorf("missing buffered complete in final output: %s", finalOutput)
	}
}

type syncPipe struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  []byte
}

func newPipe() (*syncPipe, *syncPipe) {
	sp := &syncPipe{}
	sp.cond = sync.NewCond(&sp.mu)
	return sp, sp
}

func (p *syncPipe) Read(b []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.buf) == 0 {
		p.cond.Wait()
	}
	n = copy(b, p.buf)
	p.buf = p.buf[n:]
	return n, nil
}

func (p *syncPipe) Write(b []byte) (n int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	p.cond.Signal()
	return len(b), nil
}
