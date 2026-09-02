package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFaviconRoutes(t *testing.T) {
	srv := newTestServer()
	cases := []struct {
		path        string
		contentType string
	}{
		{"/favicon.ico", "image/svg+xml"},
		{"/icon.svg", "image/svg+xml"},
		{"/icon-192.png", "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Content-Type"); got != tc.contentType {
				t.Errorf("content-type = %q, want %q", got, tc.contentType)
			}
			if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "public") {
				t.Errorf("cache-control = %q, want public cache", cc)
			}
			if rec.Body.Len() == 0 {
				t.Error("body empty, want brand icon bytes")
			}
		})
	}
}
