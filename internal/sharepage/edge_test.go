package sharepage

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failWriter struct{}

func (f failWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

func TestEdgeRender_HTMLInjection(t *testing.T) {
	var buf bytes.Buffer
	xss := `<script>alert('pwned')</script>`
	if err := Render(&buf, xss); err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, `<script>alert('pwned')</script>`) {
		t.Errorf("Render did not sanitize/escape script injection in deviceName: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(&#39;pwned&#39;)&lt;/script&gt;") &&
		!strings.Contains(out, "&lt;script&gt;alert('pwned')&lt;/script&gt;") {
		t.Logf("Escaped output: %s", out)
	}
}

func TestEdgeRender_EmptyDeviceName(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, ""); err != nil {
		t.Fatalf("Render failed for empty deviceName: %v", err)
	}
	if buf.Len() == 0 {
		t.Errorf("rendered empty template output")
	}
}

func TestEdgeRender_UnicodeSpecialCharacters(t *testing.T) {
	var buf bytes.Buffer
	name := "🚀 Device / 日本語 & 'quoted' \""
	if err := Render(&buf, name); err != nil {
		t.Fatalf("Render failed with unicode/special chars: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "🚀 Device / 日本語") {
		t.Errorf("Unicode content not properly preserved: %s", out)
	}
}

func TestEdgeRender_WriterError(t *testing.T) {
	fw := failWriter{}
	err := Render(fw, "MyDevice")
	if err == nil {
		t.Fatal("expected error when writer fails, got nil")
	}
}
