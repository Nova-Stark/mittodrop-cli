package cliparser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mittodrop/internal/cliparser"
)

func TestParse_Help(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"help"},
		{"-h"},
		{"--help"},
	} {
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error for %v: %v", args, err)
		}
		if cmd.Action != cliparser.ActionHelp {
			t.Errorf("expected ActionHelp, got %v", cmd.Action)
		}
	}
}

func TestParse_ShoutSend(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "file1.txt")
	f2 := filepath.Join(tempDir, "file2.txt")
	_ = os.WriteFile(f1, []byte("alpha"), 0644)
	_ = os.WriteFile(f2, []byte("beta"), 0644)

	t.Run("DefaultSendAlias", func(t *testing.T) {
		args := []string{"send", "-f", f1, f2, "-u", "peer-bob", "-t", "token123"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionShoutSend {
			t.Fatalf("expected ActionShoutSend, got %v", cmd.Action)
		}
		cfg := cmd.ShoutSend
		if len(cfg.Files) != 2 || cfg.Files[0] != f1 || cfg.Files[1] != f2 {
			t.Errorf("files mismatch: %v", cfg.Files)
		}
		if cfg.Target != "peer-bob" {
			t.Errorf("target = %q, want 'peer-bob'", cfg.Target)
		}
		if cfg.Token != "token123" {
			t.Errorf("token = %q, want 'token123'", cfg.Token)
		}
	})

	t.Run("ExplicitShoutSendWithPositionalFiles", func(t *testing.T) {
		args := []string{"shout", "send", f1, "--target=peer-alice"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionShoutSend {
			t.Fatalf("expected ActionShoutSend, got %v", cmd.Action)
		}
		if len(cmd.ShoutSend.Files) != 1 || cmd.ShoutSend.Files[0] != f1 {
			t.Errorf("files mismatch: %v", cmd.ShoutSend.Files)
		}
		if cmd.ShoutSend.Target != "peer-alice" {
			t.Errorf("target mismatch: %q", cmd.ShoutSend.Target)
		}
	})

	t.Run("MissingFileWarningAndFiltering", func(t *testing.T) {
		missing := filepath.Join(tempDir, "nonexistent.bin")
		args := []string{"send", "-f", f1, missing}

		var warnings []string
		warnFn := func(msg string) {
			warnings = append(warnings, msg)
		}

		cmd, err := cliparser.Parse(args, warnFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd.ShoutSend.Files) != 1 || cmd.ShoutSend.Files[0] != f1 {
			t.Errorf("expected only valid file, got %v", cmd.ShoutSend.Files)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "nonexistent.bin") {
			t.Errorf("expected warning about nonexistent.bin, got %v", warnings)
		}
	})

	t.Run("AllFilesMissingErrors", func(t *testing.T) {
		missing := filepath.Join(tempDir, "ghost.bin")
		args := []string{"send", "-f", missing}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error when all files are missing, got nil")
		}
	})
}

func TestParse_ShoutRec(t *testing.T) {
	t.Run("DefaultRecAlias", func(t *testing.T) {
		args := []string{"rec", "-d", "/tmp/save", "-p", "52199", "-t", "secrettoken"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionShoutRec {
			t.Fatalf("expected ActionShoutRec, got %v", cmd.Action)
		}
		if cmd.ShoutRec.Dir != "/tmp/save" || cmd.ShoutRec.Port != 52199 || cmd.ShoutRec.Token != "secrettoken" {
			t.Errorf("unexpected ShoutRec config: %+v", cmd.ShoutRec)
		}
	})

	t.Run("ExplicitShoutReceive", func(t *testing.T) {
		args := []string{"shout", "receive"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionShoutRec {
			t.Fatalf("expected ActionShoutRec, got %v", cmd.Action)
		}
		if cmd.ShoutRec.Dir != "." || cmd.ShoutRec.Port != 0 {
			t.Errorf("expected defaults, got %+v", cmd.ShoutRec)
		}
	})
}

func TestParse_LinkShareServe(t *testing.T) {
	args := []string{"linkshare", "serve", "-d", "/tmp/drop", "-p", "8080", "-t", "tok456"}
	cmd, err := cliparser.Parse(args, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Action != cliparser.ActionLinkShareServe {
		t.Fatalf("expected ActionLinkShareServe, got %v", cmd.Action)
	}
	if cmd.LinkShareServe.Dir != "/tmp/drop" || cmd.LinkShareServe.Port != 8080 || cmd.LinkShareServe.Token != "tok456" {
		t.Errorf("unexpected LinkShareServe config: %+v", cmd.LinkShareServe)
	}
}

func TestParse_LinkShareSend(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "doc.pdf")
	_ = os.WriteFile(f1, []byte("pdf data"), 0644)

	t.Run("FanOut", func(t *testing.T) {
		args := []string{"linkshare", "send", "-u", "192.168.1.10:8080", "-t", "tok1", "-f", f1}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionLinkShareSend {
			t.Fatalf("expected ActionLinkShareSend, got %v", cmd.Action)
		}
		if len(cmd.LinkShareSend.Targets) != 1 {
			t.Fatalf("expected 1 target, got %d", len(cmd.LinkShareSend.Targets))
		}
		target := cmd.LinkShareSend.Targets[0]
		if target.URL != "http://192.168.1.10:8080" || target.Token != "tok1" {
			t.Errorf("target mismatch: %+v", target)
		}
	})

	t.Run("UFPrecedence", func(t *testing.T) {
		args := []string{
			"linkshare", "send",
			"-u", "ignored.com",
			"-uf", "192.168.1.50:9000 -t tok-matrix " + f1,
		}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd.LinkShareSend.Targets) != 1 {
			t.Fatalf("expected 1 target from -uf, got %d", len(cmd.LinkShareSend.Targets))
		}
		target := cmd.LinkShareSend.Targets[0]
		if target.URL != "http://192.168.1.50:9000" || target.Token != "tok-matrix" {
			t.Errorf("target mismatch: %+v", target)
		}
	})
}
