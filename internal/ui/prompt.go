package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// PromptChoice represents the user's decision to an incoming transfer prompt.
type PromptChoice int

const (
	// ChoiceNo indicates rejection (default for any invalid or empty input).
	ChoiceNo PromptChoice = iota
	// ChoiceYes indicates acceptance for this single transfer task.
	ChoiceYes
	// ChoiceAlways indicates acceptance and trusting the device for the remainder of the session.
	ChoiceAlways
)

// String returns human-readable representation of choice.
func (c PromptChoice) String() string {
	switch c {
	case ChoiceYes:
		return "yes"
	case ChoiceAlways:
		return "always"
	default:
		return "no"
	}
}

// SetInput overrides the reader used for interactive prompts (defaults to os.Stdin).
func (p *MultiPrinter) SetInput(in io.Reader) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.in = in
}

// PrintWarn prints a warning tag message. If a prompt is active, it buffers until prompt completes.
func (p *MultiPrinter) PrintWarn(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg := fmt.Sprintf(format, a...)
	line := fmt.Sprintf("[WARN] %s\n", msg)
	if p.prompting {
		p.buffer = append(p.buffer, line)
		return
	}
	fmt.Fprint(p.out, line)
}

// PromptString displays an interactive prompt, locks terminal output from concurrent workers,
// buffers all background notifications, reads user input, and flushes buffered output upon return.
func (p *MultiPrinter) PromptString(promptText string) string {
	p.promptMu.Lock()
	defer p.promptMu.Unlock()

	p.mu.Lock()
	p.prompting = true

	// Ensure any in-flight progress carriage returns are cleared
	fmt.Fprintf(p.out, "\r%s", promptText)

	reader := p.in
	if reader == nil {
		reader = os.Stdin
	}
	p.mu.Unlock()

	bufReader := bufio.NewReader(reader)
	line, _ := bufReader.ReadString('\n')

	p.mu.Lock()
	// Release prompt mode and flush queued buffer
	p.prompting = false
	if len(p.buffer) > 0 {
		for _, bufferedLine := range p.buffer {
			fmt.Fprint(p.out, bufferedLine)
		}
		p.buffer = nil
	}
	p.mu.Unlock()

	return strings.TrimSpace(line)
}

// Prompt displays an interactive prompt and parses choice.
// Any invalid answer defaults strictly to ChoiceNo ('n').
func (p *MultiPrinter) Prompt(promptText string) PromptChoice {
	line := p.PromptString(promptText)
	return parseChoice(line)
}

// parseChoice converts raw input into PromptChoice. Invalid input defaults to ChoiceNo.
func parseChoice(input string) PromptChoice {
	clean := strings.ToLower(strings.TrimSpace(input))
	switch clean {
	case "y", "yes":
		return ChoiceYes
	case "a", "always":
		return ChoiceAlways
	default:
		return ChoiceNo
	}
}
