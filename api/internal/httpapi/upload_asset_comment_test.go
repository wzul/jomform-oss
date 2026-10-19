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
	// t_0fe14ba5: the url a CODE asset is handed is on the WORKSPACE host — the
	// canonical media host resolves to no account and 404s a code asset
	// (issue #379) — so the key is read from the host the response names. This
	// assertion is also the shape guard for that decision.
	mediaURLBase := "https://" + host + "/media/"
	key := strings.TrimPrefix(resp.URL, mediaURLBase)
	if key == resp.URL || key == "" {
		t.Fatalf("unexpected public url %q (want the workspace host %q prefix)", resp.URL, mediaURLBase)
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

// TestCommentLedMarkupUploadsAsItsDocumentType is t_1dbd027c's acceptance test:
// a document whose first line is a `/* ... */` banner must be stored under a
// `.html`/`.svg` key with the markup content-type — not smuggled into `.js` by
// the comment-led fallback, which a browser refuses to render under nosniff.
func TestCommentLedMarkupUploadsAsItsDocumentType(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw, host := cssCommentUploadFixture(t)

	cases := []struct {
		name       string
		filename   string
		partCT     string
		body       string
		wantSuffix string
		wantMIME   string
	}{
		{
			name:       "html-doctype-behind-block-comment",
			filename:   "page.html",
			partCT:     "text/html",
			body:       "/* Acme page banner 2026 */\n<!doctype html><html><body>hi</body></html>\n",
			wantSuffix: ".html",
			wantMIME:   "text/html; charset=utf-8",
		},
		{
			// t_20af3915: this case used a `/* ... */` banner, which classifies as
			// `.svg` correctly but is NOT a well-formed XML document (text before
			// the root), so the upload is now refused at validation — see
			// TestSVGUploadRefusesANonWellFormedDocument. The classification half
			// is exercised here with the XML-legal control that a browser really
			// renders; the `/* */` shape is still pinned as `.svg` by
			// TestSniffAssetTypeCommentLedMarkup, which is about the TYPE only.
			name:       "svg-behind-xml-comment",
			filename:   "logo.svg",
			partCT:     "image/svg+xml",
			body:       "<!-- Acme logo banner 2026 -->\n<svg xmlns=\"http://www.w3.org/2000/svg\"><rect width=\"1\" height=\"1\"/></svg>\n",
			wantSuffix: ".svg",
			wantMIME:   "image/svg+xml",
		},
	}

	mediaURLBase := "https://" + host + "/media/"
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := []byte(c.body)
			code, respBody := signatureUpload(t, h, raw, "/v1/rest/uploads/image", c.filename, c.partCT, body)
			if code != http.StatusOK {
				t.Fatalf("comment-led %s upload = %d, want 200 (body %s)", c.wantSuffix, code, respBody)
			}
			var resp struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal([]byte(respBody), &resp); err != nil {
				t.Fatalf("decode upload response %q: %v", respBody, err)
			}
			key := strings.TrimPrefix(resp.URL, mediaURLBase)
			if key == resp.URL || key == "" {
				t.Fatalf("unexpected public url %q (want the workspace host %q prefix)", resp.URL, mediaURLBase)
			}
			if !strings.HasSuffix(key, c.wantSuffix) {
				t.Fatalf("comment-led document stored under %q, want a %s key", key, c.wantSuffix)
			}

			stored, storedCT, err := h.Store.GetObject(ctx, key)
			if err != nil {
				t.Fatalf("GetObject(%q): %v", key, err)
			}
			if storedCT != c.wantMIME {
				t.Fatalf("stored content-type = %q, want %q", storedCT, c.wantMIME)
			}
			if !bytes.Equal(stored, body) {
				t.Fatalf("stored bytes differ from the upload (%d vs %d bytes)", len(stored), len(body))
			}

			m, err := h.Media.ByKey(ctx, accountID, key)
			if err != nil {
				t.Fatalf("media row ByKey(%q): %v", key, err)
			}
			if m == nil {
				t.Fatalf("no media row recorded for %q", key)
			}
			if m.Mime != c.wantMIME {
				t.Fatalf("media.mime = %q, want %q", m.Mime, c.wantMIME)
			}

			// Served on the workspace host: the markup content-type is what makes
			// the document render. A fresh code asset is withheld until the
			// scanner clears it (issue #364), which is a different question from
			// its TYPE — mark it scanned as the scanner would.
			if err := h.Media.SetScanStatus(ctx, accountID, m.ID, "ok", ""); err != nil {
				t.Fatalf("mark media row scanned: %v", err)
			}
			rec := restMediaServe(t, h, host, key)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /media/%s on %s = %d (%s), want 200", key, host, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != c.wantMIME {
				t.Fatalf("served content-type = %q, want %q", got, c.wantMIME)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if !bytes.Equal(rec.Body.Bytes(), body) {
				t.Fatalf("served body differs from the upload (%d vs %d bytes)", rec.Body.Len(), len(body))
			}
		})
	}
}
