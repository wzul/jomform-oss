package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriteLegalPageHTMLEscapesContent verifies the public legal page renderer
// escapes merchant content so no script can run (no XSS on the public page).
func TestWriteLegalPageHTMLEscapesContent(t *testing.T) {
	rec := httptest.NewRecorder()
	writeLegalPageHTML(rec, "Privacy Policy", "acme", "<script>alert('x')</script>\n\nSecond para")
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Errorf("legal page output contains raw <script> (XSS)")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("legal page output should escape <script>")
	}
	if !strings.Contains(body, "<p>Second para</p>") {
		t.Errorf("legal page output should render paragraphs")
	}
	if !strings.Contains(body, "Privacy Policy") {
		t.Errorf("legal page output should include the title")
	}
}
