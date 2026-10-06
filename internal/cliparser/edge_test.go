package cliparser

import (
	"testing"
)

func TestEdgeParse_EmptyArgs(t *testing.T) {
	cmd, err := Parse([]string{}, nil)
	if err != nil {
		t.Fatalf("unexpected error on empty args: %v", err)
	}
	if cmd.Action != ActionHelp {
		t.Errorf("expected ActionHelp, got %v", cmd.Action)
	}
}

func TestEdgeParse_HelpFlags(t *testing.T) {
	for _, flag := range []string{"help", "-h", "--help"} {
		cmd, err := Parse([]string{flag}, nil)
		if err != nil {
			t.Fatalf("unexpected error on %s: %v", flag, err)
		}
		if cmd.Action != ActionHelp {
			t.Errorf("expected ActionHelp for %s, got %v", flag, cmd.Action)
		}
	}
}

func TestEdgeParse_UnknownCommand(t *testing.T) {
	_, err := Parse([]string{"unknowncommand"}, nil)
	if err == nil {
		t.Fatal("expected error on unknown command, got nil")
	}
}

func TestEdgeParse_SubcommandMissing(t *testing.T) {
	for _, root := range []string{"shout", "linkshare", "otin", "direct"} {
		_, err := Parse([]string{root}, nil)
		if err == nil {
			t.Errorf("expected error when %s called without subcommand", root)
		}
	}
}

func TestEdgeParse_ShoutUnknownSubcommand(t *testing.T) {
	_, err := Parse([]string{"shout", "dance"}, nil)
	if err == nil {
		t.Fatal("expected error on unknown shout subcommand")
	}
}

func TestEdgeParse_LinkShareInvalidPort(t *testing.T) {
	// Negative port
	_, err := Parse([]string{"linkshare", "serve", "-p", "-1"}, nil)
	if err == nil {
		t.Fatal("expected error for negative port in linkshare serve")
	}

	// Out of range port
	_, err = Parse([]string{"linkshare", "serve", "-p", "70000"}, nil)
	if err == nil {
		t.Fatal("expected error for port > 65535 in linkshare serve")
	}

	// Missing port value
	_, err = Parse([]string{"linkshare", "serve", "-p"}, nil)
	if err == nil {
		t.Fatal("expected error when -p flag missing value")
	}
}

func TestEdgeParse_MissingFilesNonDestructive(t *testing.T) {
	var warnings []string
	warnFn := func(msg string) {
		warnings = append(warnings, msg)
	}

	// Supply existing file (edge_test.go) + non-existent file
	cmd, err := Parse([]string{"send", "edge_test.go", "non-existent-file-12345.dat"}, warnFn)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(warnings) == 0 {
		t.Errorf("expected warning for missing file")
	}
	if len(cmd.ShoutSend.Files) != 1 || cmd.ShoutSend.Files[0] != "edge_test.go" {
		t.Errorf("expected only edge_test.go in parsed files, got %v", cmd.ShoutSend.Files)
	}
}

func TestEdgeExtractAndNormalizeURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantURL   string
		wantToken string
		wantErr   bool
	}{
		{
			name:      "Standard HTTP with port and token",
			raw:       "http://192.168.1.10:8080?token=mytoken",
			wantURL:   "http://192.168.1.10:8080",
			wantToken: "mytoken",
			wantErr:   false,
		},
		{
			name:      "Missing scheme auto-adds http",
			raw:       "192.168.1.10:8080/drop?token=secret",
			wantURL:   "http://192.168.1.10:8080/drop",
			wantToken: "secret",
			wantErr:   false,
		},
		{
			name:      "Trailing slash trimmed",
			raw:       "http://localhost:3000/",
			wantURL:   "http://localhost:3000",
			wantToken: "",
			wantErr:   false,
		},
		{
			name:      "Empty input errors",
			raw:       "",
			wantURL:   "",
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "Whitespace trimmed",
			raw:       "  http://example.com:9000?token=abc   ",
			wantURL:   "http://example.com:9000",
			wantToken: "abc",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotToken, err := ExtractAndNormalizeURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ExtractAndNormalizeURL(%q) err = %v, wantErr = %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr {
				if gotURL != tt.wantURL {
					t.Errorf("gotURL = %q, want %q", gotURL, tt.wantURL)
				}
				if gotToken != tt.wantToken {
					t.Errorf("gotToken = %q, want %q", gotToken, tt.wantToken)
				}
			}
		})
	}
}

func TestEdgeParse_DirectSendMissingAddress(t *testing.T) {
	_, err := Parse([]string{"direct", "send"}, nil)
	if err == nil {
		t.Fatal("expected error when direct send missing target address")
	}
}
