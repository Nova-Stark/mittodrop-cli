package sharepage

import (
	"bytes"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "peacefulsculpture918")
	if err != nil {
		t.Fatalf("Render() failed: %v", err)
	}

	html := buf.String()

	// Check device name injection
	if !strings.Contains(html, "peacefulsculpture918") {
		t.Errorf("expected device name in rendered HTML")
	}

	// Check styles are included
	if !strings.Contains(html, "--accent-go: #00add8;") {
		t.Errorf("expected CSS styles in rendered HTML")
	}

	// Check script is included
  if !strings.Contains(html, "uploadFiles") {
		t.Errorf("expected JS script in rendered HTML")
	}

	// Check dropzone element exists
	if !strings.Contains(html, "id=\"dropzone\"") {
		t.Errorf("expected dropzone container in rendered HTML")
	}

	// Check success card exists
	if !strings.Contains(html, "id=\"success-box\"") {
		t.Errorf("expected success card in rendered HTML")
	}

	// Check token UI elements exist
	if !strings.Contains(html, "id=\"token-input\"") {
		t.Errorf("expected token input in rendered HTML")
	}
	if !strings.Contains(html, "id=\"token-toggle\"") {
		t.Errorf("expected token toggle button in rendered HTML")
	}
	if !strings.Contains(html, "id=\"token-banner\"") {
		t.Errorf("expected token error banner in rendered HTML")
	}

	// Check token handling in JS
	if !strings.Contains(html, "X-LinkShare-Token") {
		t.Errorf("expected X-LinkShare-Token header in script")
	}
	if !strings.Contains(html, "mittodrop-token") {
		t.Errorf("expected sessionStorage token cache in script")
	}
}
