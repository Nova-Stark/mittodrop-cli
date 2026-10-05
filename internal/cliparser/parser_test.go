package cliparser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestParse_OtinSend(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "alpha.txt")
	_ = os.WriteFile(f1, []byte("data"), 0644)

	t.Run("ValidFlags", func(t *testing.T) {
		args := []string{"otin", "send", "-a", "tc1-addr123", "-c", "42-guitar-alaska", "-f", f1}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionOtinSend {
			t.Fatalf("expected ActionOtinSend, got %v", cmd.Action)
		}
		cfg := cmd.OtinSend
		if cfg.Addr != "tc1-addr123" || cfg.Codephrase != "42-guitar-alaska" || len(cfg.Files) != 1 {
			t.Errorf("unexpected OtinSend config: %+v", cfg)
		}
	})

	t.Run("MissingFilesSkipsWithWarning", func(t *testing.T) {
		missing := filepath.Join(tempDir, "ghost.bin")
		args := []string{"otin", "send", "-a", "tc1-addr", "-c", "phrase", f1, missing}

		var warnings []string
		warnFn := func(msg string) {
			warnings = append(warnings, msg)
		}

		cmd, err := cliparser.Parse(args, warnFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cmd.OtinSend.Files) != 1 || cmd.OtinSend.Files[0] != f1 {
			t.Errorf("expected only valid file, got %v", cmd.OtinSend.Files)
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "ghost.bin") {
			t.Errorf("expected warning for ghost.bin, got %v", warnings)
		}
	})

	t.Run("AllFilesMissingErrors", func(t *testing.T) {
		missing := filepath.Join(tempDir, "none.bin")
		args := []string{"otin", "send", "-a", "tc1-addr", "-c", "phrase", "-f", missing}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error when all files are missing, got nil")
		}
	})

	t.Run("RelayFlag", func(t *testing.T) {
		args := []string{"otin", "send", "-r", "127.0.0.1:9007", "-c", "phrase", "-f", f1}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.OtinSend.RelayAddr != "127.0.0.1:9007" || cmd.OtinSend.Codephrase != "phrase" {
			t.Errorf("unexpected OtinSend config: %+v", cmd.OtinSend)
		}
	})
}

func TestParse_OtinRec(t *testing.T) {
	t.Run("Defaults", func(t *testing.T) {
		args := []string{"otin", "rec"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionOtinRec {
			t.Fatalf("expected ActionOtinRec, got %v", cmd.Action)
		}
		cfg := cmd.OtinRec
		if cfg.Dir != "." || cfg.Port != 42201 || cfg.Codephrase != "" {
			t.Errorf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("CustomFlags", func(t *testing.T) {
		args := []string{"otin", "receive", "-d", "/tmp/down", "-p", "50000", "-c", "my-secret-phrase", "-r", "127.0.0.1:9007"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionOtinRec {
			t.Fatalf("expected ActionOtinRec, got %v", cmd.Action)
		}
		cfg := cmd.OtinRec
		if cfg.Dir != "/tmp/down" || cfg.Port != 50000 || cfg.Codephrase != "my-secret-phrase" || cfg.RelayAddr != "127.0.0.1:9007" {
			t.Errorf("unexpected custom flags: %+v", cfg)
		}
	})

	t.Run("RelayPasswordFlag", func(t *testing.T) {
		args := []string{"otin", "rec", "-r", "127.0.0.1:9007", "--relay-pass", "secret123"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.OtinRec.RelayPassword != "secret123" {
			t.Errorf("expected RelayPassword 'secret123', got %q", cmd.OtinRec.RelayPassword)
		}
	})

	t.Run("RelayMissingArg", func(t *testing.T) {
		args := []string{"otin", "rec", "-r"}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error for missing relay address")
		}
	})
}

func TestParse_RelayServe(t *testing.T) {
	t.Run("Defaults_MittoRelayServe", func(t *testing.T) {
		args := []string{"relay", "serve"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionRelayServe {
			t.Fatalf("expected ActionRelayServe, got %v", cmd.Action)
		}
		cfg := cmd.RelayServe
		if cfg.Port != 9007 || cfg.Host != "0.0.0.0" || cfg.Banner != "mittodrop-relay" || cfg.Password != "" {
			t.Errorf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("Defaults_MittoRelayAlias", func(t *testing.T) {
		args := []string{"relay"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionRelayServe {
			t.Fatalf("expected ActionRelayServe, got %v", cmd.Action)
		}
	})

	t.Run("Defaults_MittoOtinRelayAlias", func(t *testing.T) {
		args := []string{"otin", "relay", "serve"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionRelayServe {
			t.Fatalf("expected ActionRelayServe, got %v", cmd.Action)
		}
	})

	t.Run("CustomFlags", func(t *testing.T) {
		args := []string{
			"relay", "serve",
			"-p", "9999",
			"-h", "127.0.0.1",
			"--pass", "super-secret",
			"--banner", "custom-node",
			"--ttl", "15m",
			"--max-rooms", "500",
			"--rate-limit", "120",
			"--rate-window", "30s",
		}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cfg := cmd.RelayServe
		if cfg.Port != 9999 || cfg.Host != "127.0.0.1" || cfg.Password != "super-secret" || cfg.Banner != "custom-node" {
			t.Errorf("unexpected custom flags: %+v", cfg)
		}
		if cfg.RoomTTL != 15*time.Minute || cfg.MaxWaitingRooms != 500 || cfg.RateLimit != 120 || cfg.RateWindow != 30*time.Second {
			t.Errorf("unexpected policy flags: %+v", cfg)
		}
	})

	t.Run("InvalidPort", func(t *testing.T) {
		args := []string{"relay", "serve", "-p", "invalid"}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error for invalid port")
		}
	})

	t.Run("InvalidTTL", func(t *testing.T) {
		args := []string{"relay", "serve", "--ttl", "notaduration"}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error for invalid ttl")
		}
	})
}

func TestParse_Direct(t *testing.T) {
	tempDir := t.TempDir()
	f1 := filepath.Join(tempDir, "data.bin")
	_ = os.WriteFile(f1, []byte("payload"), 0644)

	t.Run("DirectRecDefault", func(t *testing.T) {
		args := []string{"direct", "rec"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionDirectRec {
			t.Fatalf("expected ActionDirectRec, got %v", cmd.Action)
		}
		cfg := cmd.DirectRec
		if cfg.Dir != "." || cfg.Port != 0 || cfg.NoUPnP != false || cfg.Codephrase != "" {
			t.Errorf("unexpected defaults: %+v", cfg)
		}
	})

	t.Run("ManualRecAliasWithFlags", func(t *testing.T) {
		args := []string{"manual", "receive", "-d", "/tmp/recv", "-p", "42205", "-c", "lake-forest-mountain", "--no-upnp"}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionDirectRec {
			t.Fatalf("expected ActionDirectRec, got %v", cmd.Action)
		}
		cfg := cmd.DirectRec
		if cfg.Dir != "/tmp/recv" || cfg.Port != 42205 || cfg.Codephrase != "lake-forest-mountain" || !cfg.NoUPnP {
			t.Errorf("unexpected parsed config: %+v", cfg)
		}
	})

	t.Run("DirectSendWithPositionalFiles", func(t *testing.T) {
		args := []string{"direct", "send", "-a", "192.168.1.50:42201", "-c", "lake-forest-mountain", f1}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionDirectSend {
			t.Fatalf("expected ActionDirectSend, got %v", cmd.Action)
		}
		cfg := cmd.DirectSend
		if cfg.Addr != "192.168.1.50:42201" || cfg.Codephrase != "lake-forest-mountain" || len(cfg.Files) != 1 || cfg.Files[0] != f1 {
			t.Errorf("unexpected send config: %+v", cfg)
		}
	})

	t.Run("ManualSendAliasWithFlagFiles", func(t *testing.T) {
		args := []string{"manual", "send", "--addr=127.0.0.1:42201", "--code=code-123", "-f", f1}
		cmd, err := cliparser.Parse(args, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmd.Action != cliparser.ActionDirectSend {
			t.Fatalf("expected ActionDirectSend, got %v", cmd.Action)
		}
		cfg := cmd.DirectSend
		if cfg.Addr != "127.0.0.1:42201" || cfg.Codephrase != "code-123" || len(cfg.Files) != 1 {
			t.Errorf("unexpected send config: %+v", cfg)
		}
	})

	t.Run("DirectSendMissingAddr", func(t *testing.T) {
		args := []string{"direct", "send", "-c", "some-code", f1}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error for missing address")
		}
	})

	t.Run("DirectSendMissingCode", func(t *testing.T) {
		args := []string{"direct", "send", "-a", "127.0.0.1:42201", f1}
		_, err := cliparser.Parse(args, nil)
		if err == nil {
			t.Fatal("expected error for missing codephrase")
		}
	})
}


