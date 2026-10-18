package httpapi

// t_8881000c: end-to-end through the handler the dashboard media picker calls.
// A stylesheet that opens with a licence/theme comment must be stored under a
// `.css` key with `text/css`, recorded as text/css in media.mime, and served as
// text/css — because /media sends `X-Content-Type-Options: nosniff` and a
// browser refuses a `text/javascript` file as `<link rel="stylesheet">`.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL), skipped
// when unset. Uses a fake object store so no live S3/MinIO is needed.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// cssCommentUploadFixture seeds a user + workspace (each workspace owns its own
// account, issue #158) and returns handlers wired with a fake object store and a
// real MediaStore, plus the account id, an API key for it, and the workspace
// host that /media resolves the tenant from.
func cssCommentUploadFixture(t *testing.T) (h *RESTHandlers, accountID, rawKey, host string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, userID := bundleFixture(t, ctx, pool)

	ws, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID, "CSS comment", "cssc-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	accountID = accountIDForWorkspace(t, ctx, pool, ws.ID)
	if accountID == "" {
		t.Fatalf("workspace %q owns no account", ws.Slug)
	}

	h = newBundleHandlers(pool)
	h.Store = newFakeObjectStore()
	h.Media = dbstore.NewMediaStore(pool)
	rawKey = bundleAPIKey(t, ctx, pool, accountID)
	return h, accountID, rawKey, ws.Slug + ".jomform.com"
}

// TestCSSWithLeadingCommentUploadsAsCSS is the card's acceptance test: the
// comment-led stylesheet is stored, labelled and served as CSS end to end.
func TestCSSWithLeadingCommentUploadsAsCSS(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw, host := cssCommentUploadFixture(t)

	// The prod-measured body shape: a licence comment, then one real rule.
	body := []byte("/* Acme theme licence 2026 */\n.jf-qa-probe{color:#123456;}\n")

	code, respBody := signatureUpload(t, h, raw, "/v1/rest/uploads/image", "theme.css", "text/css", body)
	if code != http.StatusOK {
		t.Fatalf("comment-led CSS upload = %d, want 200 (body %s)", code, respBody)
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(respBody), &resp); err != nil {
		t.Fatalf("decode upload response %q: %v", respBody, err)
	}
	key := strings.TrimPrefix(resp.URL, fakeMediaPrefix)
	if key == resp.URL || key == "" {
		t.Fatalf("unexpected public url %q (want the fake store's %q prefix)", resp.URL, fakeMediaPrefix)
	}
	if !strings.HasSuffix(key, ".css") {
		t.Fatalf("comment-led stylesheet stored under %q, want a .css key", key)
	}

	// The stored bytes and metadata must be CSS and byte-for-byte the upload.
	stored, storedCT, err := h.Store.GetObject(ctx, key)
	if err != nil {
		t.Fatalf("GetObject(%q): %v", key, err)
	}
	if want := "text/css; charset=utf-8"; storedCT != want {
		t.Fatalf("stored content-type = %q, want %q", storedCT, want)
	}
	if !bytes.Equal(stored, body) {
		t.Fatalf("stored bytes differ from the upload (%d vs %d bytes)", len(stored), len(body))
	}

	// The media library row must badge the file as CSS too.
	m, err := h.Media.ByKey(ctx, accountID, key)
	if err != nil {
		t.Fatalf("media row ByKey(%q): %v", key, err)
	}
	if m == nil {
		t.Fatalf("no media row recorded for %q", key)
	}
	if m.Mime != "text/css; charset=utf-8" {
		t.Fatalf("media.mime = %q, want text/css; charset=utf-8", m.Mime)
	}

	// Served on the workspace host: CSS type + nosniff (the reason a wrong
	// type breaks the merchant's page instead of merely looking odd). A fresh
	// code asset is withheld until the scanner has cleared it (issue #364 gate),
	// which is a different question from its TYPE — mark it scanned as the
	// scanner would, then read what the route publishes.
	if err := h.Media.SetScanStatus(ctx, accountID, m.ID, "ok", ""); err != nil {
		t.Fatalf("mark media row scanned: %v", err)
	}
	rec := restMediaServe(t, h, host, key)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/%s on %s = %d (%s), want 200", key, host, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("served content-type = %q, want text/css; charset=utf-8", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Fatalf("served body differs from the upload (%d vs %d bytes)", rec.Body.Len(), len(body))
	}
}
