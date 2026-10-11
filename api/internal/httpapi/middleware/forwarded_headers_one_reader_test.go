// CLASS GUARD — the forwarding headers are read in exactly ONE place.
//
// clientip.go's trust model only holds while every consumer asks ClientIP (or
// trusts RealIP's answer). The defect this card fixes existed because ClientIP
// read CF-Connecting-IP directly, and the natural next regression is the same
// move somewhere else: a handler that wants "the client IP" reaches for
// r.Header.Get("CF-Connecting-IP") and re-opens the hole for its own surface,
// with no test in this package able to see it.
//
// So this is a SOURCE scan over the api module: a non-test .go file may not name
// any of the three headers except the one file that implements the rule. A scan
// rather than a behaviour test, deliberately: a behaviour test can only cover the
// call sites that exist today, so it cannot refuse the next one.
package middleware

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forwardedHeaders are the request headers that describe where a request came
// from. None of them is trustworthy on its own: they are written by whoever
// speaks last, and only a proxy we control writes the value we may believe.
var forwardedHeaders = []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"}

// oneReader is the only non-test file allowed to name one: the resolution rule
// itself.
const oneReader = "internal/httpapi/middleware/clientip.go"

func TestForwardedHeadersAreReadInOnePlace(t *testing.T) {
	// The api module root: this package is api/internal/httpapi/middleware.
	root := filepath.Join("..", "..", "..")

	hits := map[string][]string{}
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, h := range forwardedHeaders {
			if strings.Contains(string(b), `"`+h+`"`) {
				hits[rel] = append(hits[rel], h)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the api module: %v", err)
	}

	// The walker must be looking at the module, or a clean result is vacuous.
	if files < 100 {
		t.Fatalf("scanned only %d non-test .go file(s) under %s — the walker is broken, so this guard would pass vacuously", files, root)
	}

	readers := make([]string, 0, len(hits))
	for f, headers := range hits {
		readers = append(readers, f+": "+strings.Join(headers, ", "))
	}
	if len(readers) != 1 || !strings.HasPrefix(readers[0], oneReader+":") {
		t.Fatalf("the forwarding headers may be read in %s and nowhere else — a second reader\n"+
			"re-opens the hole this rule closes (a caller picks the value, so it picks its own\n"+
			"rate-limit bucket). Readers found: %v", oneReader, readers)
	}

	// And the one reader must really name all three, or the guard above is
	// measuring a file that stopped reading them.
	got := hits[oneReader]
	for _, h := range forwardedHeaders {
		if !contains(got, h) {
			t.Fatalf("%s no longer names %s — either the rule moved (update oneReader) or it stopped reading a header it claims to resolve", oneReader, h)
		}
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
