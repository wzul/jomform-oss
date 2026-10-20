// t_b3991178 — GET /v1/rest/media?url=<url> names ONE row.
//
// THE DEFECT this closes, measured on the real page before this card:
// /form-settings persists only `logo_url`. After a reload it learned that row's
// MIME by reading the library page (`?limit=100`, newest first) and matching
// `url` exactly. Two ordinary states are NOT in that read:
//
//  1. the logo row is older than the newest 100 rows of the workspace;
//  2. the stored url stopped matching the row's advertised url (the public host
//     changed: a renamed workspace, or a verified custom domain).
//
// Both leave the MIME unknown, and an unknown MIME is drawn as an <img> — for a
// code asset on the tenant host (Cross-Origin-Resource-Policy: same-origin)
// that is a broken tile with HTTP 200 and no error anywhere.
//
// These tests are integration tests (real Postgres) and use the fixture from
// media_url_tenant_host_test.go: it seeds an account WITH a public host, a
// canonical media host, and a fake object store. The url the page would have
// stored is the ADVERTISED one (tenant host) — never the row's stored `url`,
// which is the whole point of the second case above.

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// lookupByURL asks the handler exactly what the page asks it, through the query
// string, and decodes the list envelope the ?url= read answers with.
func lookupByURL(t *testing.T, h *RESTHandlers, accountID, rawURL string) ([]mediaListItem, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/media?url="+url.QueryEscape(rawURL), nil)
	h.listMedia(rec, req, &AccountCtx{Account: &dbstore.Account{ID: accountID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media?url=%s = %d, want 200 (body %s)", rawURL, rec.Code, rec.Body.String())
	}
	var body struct {
		Items []mediaListItem `json:"items"`
		Page  int             `json:"page"`
		Limit int             `json:"limit"`
		Total int             `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode ?url= response: %v (%s)", err, rec.Body.String())
	}
	// The single-row read keeps the list envelope on purpose: one schema for
	// both reads. A second schema is how the page would learn the wrong shape.
	if body.Page != 1 || body.Limit != 1 {
		t.Fatalf("?url= envelope page/limit = %d/%d, want 1/1", body.Page, body.Limit)
	}
	if body.Total != len(body.Items) {
		t.Fatalf("?url= total = %d but items = %d", body.Total, len(body.Items))
	}
	return body.Items, rec.Code
}

// TestMediaLookupByURLFindsARowOlderThanThePageWindow is the card's first case:
// the logo is not in the newest 100 rows, so the page-based read cannot contain
// it — while the by-url read still names it.
func TestMediaLookupByURLFindsARowOlderThanThePageWindow(t *testing.T) {
	h, pool, accountID, slug, _, _, store := mediaURLFixture(t)
	ctx := context.Background()

	svgKey := "u/" + accountID + "/33333333-3333-3333-3333-333333333333.svg"
	svgRow := &dbstore.Media{
		AccountID: accountID, Key: svgKey, URL: store.PublicURL(svgKey),
		Mime: "image/svg+xml", Size: 64, Source: dbstore.MediaSourceMerchant,
	}
	if err := h.Media.Create(ctx, svgRow); err != nil {
		t.Fatalf("create svg media row: %v", err)
	}
	// Older than everything the fillers below add, which is the state a logo
	// that has been in the library for a while is actually in.
	if _, err := pool.Exec(ctx,
		`UPDATE media SET created_at = now() - interval '1 day' WHERE id = $1`, svgRow.ID); err != nil {
		t.Fatalf("age the svg row: %v", err)
	}
	// The newest 100 rows — a full library page that does not include the logo.
	if _, err := pool.Exec(ctx,
		`INSERT INTO media (account_id, key, url, mime, size, source)
		 SELECT $1,
		        'u/' || $2 || '/filler-' || g || '.png',
		        'https://jomform.com/media/u/' || $2 || '/filler-' || g || '.png',
		        'image/png', 1, 'merchant'
		   FROM generate_series(1, 100) g`, accountID, accountID); err != nil {
		t.Fatalf("seed 100 newer rows: %v", err)
	}

	// The url the PAGE persisted: the advertised one, on the workspace host.
	want := "https://" + slug + ".jomform.com/media/" + svgKey
	if svgRow.URL == want {
		t.Fatalf("fixture problem: the stored url already equals the advertised url (%q), so this "+
			"test would not show that an equality match cannot work", want)
	}

	// 1. The picker's page read — the only read the page had — does NOT contain
	//    the row. This is the measured pre-fix state, reproduced.
	rec := httptest.NewRecorder()
	h.listMedia(rec, httptest.NewRequest(http.MethodGet, "/v1/rest/media?limit=100", nil),
		&AccountCtx{Account: &dbstore.Account{ID: accountID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media?limit=100 = %d (body %s)", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []mediaListItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Items) != 100 {
		t.Fatalf("page has %d rows, want the 100 newest", len(page.Items))
	}
	for _, it := range page.Items {
		if it.Key == svgKey {
			t.Fatalf("the svg row IS on the library page (%d items) — the fixture no longer "+
				"reproduces the gap this card is about", len(page.Items))
		}
	}

	// 2. The by-url read names it anyway: mime included, so the preview can
	//    apply the shared code-asset rule instead of falling back to <img>.
	items, _ := lookupByURL(t, h, accountID, want)
	if len(items) != 1 {
		t.Fatalf("?url=%s returned %d rows, want exactly 1 (the logo older than the page window)",
			want, len(items))
	}
	if items[0].Key != svgKey {
		t.Fatalf("?url= resolved key %q, want %q", items[0].Key, svgKey)
	}
	if items[0].Mime != "image/svg+xml" {
		t.Fatalf("?url= mime = %q, want image/svg+xml — the page needs the row's real mime",
			items[0].Mime)
	}
	// The reply advertises the same url the caller asked with, so a caller that
	// stores urls keeps getting a working one back.
	if items[0].URL != want {
		t.Fatalf("?url= advertised %q, want the requested %q", items[0].URL, want)
	}
}

// TestMediaLookupByURLSurvivesAHostChange is the card's second case: the stored
// url and the row's advertised url differ by host only. Resolving by url string
// answers "unknown"; resolving by object key does not care which host was
// advertised when the logo was picked.
func TestMediaLookupByURLSurvivesAHostChange(t *testing.T) {
	h, _, accountID, slug, cssKey, _, store := mediaURLFixture(t)
	ctx := context.Background()

	row := &dbstore.Media{
		AccountID: accountID, Key: cssKey, URL: store.PublicURL(cssKey),
		Mime: "text/css; charset=utf-8", Size: int64(len(seededCSS)), Source: dbstore.MediaSourceMerchant,
	}
	if err := h.Media.Create(ctx, row); err != nil {
		t.Fatalf("create css media row: %v", err)
	}

	// What a caller stores before a rename, and what the library advertises
	// after it: same key, different host. Neither string is the other.
	stored := "https://old-name.jomform.com/media/" + cssKey
	nowAdvertised := "https://" + slug + ".jomform.com/media/" + cssKey
	if stored == nowAdvertised {
		t.Fatalf("fixture problem: the two hosts are equal (%q)", stored)
	}

	items, _ := lookupByURL(t, h, accountID, stored)
	if len(items) != 1 || items[0].Key != cssKey {
		t.Fatalf("?url=%s (the stored, pre-rename url) returned %d rows, want the row for key %q",
			stored, len(items), cssKey)
	}
	if items[0].Mime != "text/css; charset=utf-8" {
		t.Fatalf("?url= mime = %q, want text/css; charset=utf-8", items[0].Mime)
	}
	// The reply speaks in TODAY's url, not the stale one it was asked with.
	if items[0].URL != nowAdvertised {
		t.Fatalf("?url= advertised %q, want today's url %q", items[0].URL, nowAdvertised)
	}
}

// TestMediaLookupByURLIsScopedDeletedAndNotALibrary: the read answers "this
// account's row, if there is one" — and every other input is an empty list,
// never an error. The page relies on that: empty means "keep the <img>", which
// is the right answer for a pasted external url.
func TestMediaLookupByURLIsScopedDeletedAndNotALibrary(t *testing.T) {
	h, pool, accountID, _, cssKey, _, store := mediaURLFixture(t)
	ctx := context.Background()

	row := &dbstore.Media{
		AccountID: accountID, Key: cssKey, URL: store.PublicURL(cssKey),
		Mime: "text/css; charset=utf-8", Size: int64(len(seededCSS)), Source: dbstore.MediaSourceMerchant,
	}
	if err := h.Media.Create(ctx, row); err != nil {
		t.Fatalf("create css media row: %v", err)
	}
	ownURL := "https://jomform.com/media/" + cssKey

	// 1. A url this library does not own (a pasted external link), and inputs
	//    that are not a media url at all: 200 with NO rows, so the caller keeps
	//    its own fallback instead of guessing a mime from the extension.
	for _, raw := range []string{
		"https://cdn.example/logo.png",
		"https://jomform.com/logo.png",
		"not a url",
		"javascript:alert(1)",
		"file:///media/" + cssKey,
		"https://jomform.com/media/u/../etc/passwd",
		"https://jomform.com/media/u/%2e%2e/x.svg",
	} {
		items, _ := lookupByURL(t, h, accountID, raw)
		if len(items) != 0 {
			t.Fatalf("?url=%s returned %d rows, want 0 (not a library row)", raw, len(items))
		}
	}

	// 2. Account scope: the SAME url asked by another account resolves nothing.
	//    Media keys carry the owner account, so a lookup must never cross it.
	other := &dbstore.Account{ID: "00000000-0000-0000-0000-0000000000aa"}
	rec := httptest.NewRecorder()
	h.listMedia(rec, httptest.NewRequest(http.MethodGet, "/v1/rest/media?url="+url.QueryEscape(ownURL), nil),
		&AccountCtx{Account: other})
	if rec.Code != http.StatusOK {
		t.Fatalf("?url= for a foreign account = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var foreign struct {
		Items []mediaListItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &foreign); err != nil {
		t.Fatalf("decode foreign response: %v", err)
	}
	if len(foreign.Items) != 0 {
		t.Fatalf("another account resolved %d rows from this account's url — the lookup is not scoped",
			len(foreign.Items))
	}
	// …and the owner still does, so the assertion above is about scope and not
	// about the url being unresolvable.
	if items, _ := lookupByURL(t, h, accountID, ownURL); len(items) != 1 {
		t.Fatalf("the owning account resolved %d rows from its own url, want 1", len(items))
	}

	// 3. A trashed row is not a library row for this read (the library list does
	//    not show it either), so the page falls back rather than badging a file
	//    the merchant has thrown away.
	if _, err := pool.Exec(ctx, `UPDATE media SET deleted_at = now() WHERE id = $1`, row.ID); err != nil {
		t.Fatalf("soft-delete the row: %v", err)
	}
	if items, _ := lookupByURL(t, h, accountID, ownURL); len(items) != 0 {
		t.Fatalf("?url= returned %d rows for a soft-deleted row, want 0", len(items))
	}
}

// TestMediaKeyFromStoredURL pins the url → object-key extraction on its own:
// the lookup must accept the url whatever host advertises it (that is the
// host-change case) and must refuse anything that is not a media key.
func TestMediaKeyFromStoredURL(t *testing.T) {
	const key = "u/11111111-1111-1111-1111-111111111111/abc.svg"
	for _, raw := range []string{
		"https://jomform.com/media/" + key,
		"http://127.0.0.1:8080/media/" + key,
		"https://ws.jomform.com/media/" + key,
		"https://custom.example.com/media/" + key + "?v=2",
		"https://custom.example.com/media/" + key + "#frag",
	} {
		if got := mediaKeyFromStoredURL(raw); got != key {
			t.Fatalf("mediaKeyFromStoredURL(%q) = %q, want %q", raw, got, key)
		}
	}
	for _, raw := range []string{
		"",
		"not a url",
		"https://cdn.example/logo.png",
		"javascript:alert(1)",
		"data:image/svg+xml;base64,AAAA",
		"https://jomform.com/media/u/../" + "abc.svg",
		"https://jomform.com/media/u/%2e%2e/abc.svg",
		"https://jomform.com/media/u/acc/",
	} {
		if got := mediaKeyFromStoredURL(raw); got != "" {
			t.Fatalf("mediaKeyFromStoredURL(%q) = %q, want \"\" (not a media key)", raw, got)
		}
	}
}
