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
}
