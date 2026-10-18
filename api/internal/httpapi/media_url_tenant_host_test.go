package httpapi

// t_0fe14ba5 — the media library advertised a URL that 404s.
//
// For a CODE asset the API handed the merchant `h.Store.PublicURL(key)` — e.g.
// https://jomform.com/media/u/<acct>/x.css — while /media refuses a code asset
// unless the request Host resolves to the key's OWN account (issue #379). Bare
// jomform.com resolves to no account, so the row's View button rendered
// `{"code":"request.notFound","error":"media tidak tersedia"}`, Copy URL pasted
// a dead link, and a <link rel="stylesheet"> pointing at it could never apply
// inside a form served from <workspace>.jomform.com (measured on v1.21.224:
// same key, jomform.com → 404, wan.jomform.com → 200 text/css).
//
// The fix advertises a code asset on the account's public host and keeps the
// canonical media host for images. These tests are deliberately NOT "the key is
// in the list": each one takes the url the API actually emitted, asks the REAL
// REST mux for it on the host that url names, and requires 200 + the stored
// content-type — plus the canonical host still 404ing the code asset, so the
// test cannot pass by pointing every asset at a host that serves anything.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset. The object store is a fake, so no live S3/MinIO is required.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

var seededCSS = []byte("h1{color:#123456}\n")

// mediaURLFixture seeds a user + ONE workspace (so the account has a public
// host) and returns the handlers, the pool, the account id, the workspace slug
// and a fake store holding a CSS asset and a PNG under that account's keys.
func mediaURLFixture(t *testing.T) (h *RESTHandlers, pool *pgxpool.Pool, accountID, slug, cssKey, pngKey string, store *fakeObjectStore) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping media-URL test")
	}
	ctx := context.Background()
	pool = bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('mul-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	ws, err := dbstore.NewWorkspaceStore(pool).Create(ctx, userID, "Media URL", "mul-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	accountID = accountIDForWorkspace(t, ctx, pool, ws.ID)
	if accountID == "" {
		t.Fatalf("workspace %s owns no account; the fixture needs a public host", ws.Slug)
	}

	store = newFakeObjectStore()
	cssKey = "u/" + accountID + "/11111111-1111-1111-1111-111111111111.css"
	pngKey = "u/" + accountID + "/22222222-2222-2222-2222-222222222222.png"
	if err := store.PutObject(ctx, cssKey, "text/css; charset=utf-8", seededCSS); err != nil {
		t.Fatalf("seed css object: %v", err)
	}
	if err := store.PutObject(ctx, pngKey, "image/png", []byte("png")); err != nil {
		t.Fatalf("seed png object: %v", err)
	}

	h = newBundleHandlers(pool)
	h.Store = store
	h.Media = dbstore.NewMediaStore(pool)
	return h, pool, accountID, ws.Slug, cssKey, pngKey, store
}

// serveAdvertised asks the REAL REST mux for an advertised media url, on the
// host the url names (or on `overrideHost`, to show what the OLD url did).
// This is the browser's half of the contract: click View → GET that url.
func serveAdvertised(t *testing.T, h *RESTHandlers, rawURL, overrideHost string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.Path == "" {
		t.Fatalf("advertised url %q does not parse into host+path (%v)", rawURL, err)
	}
	host := u.Host
	if overrideHost != "" {
		host = overrideHost
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

type mediaListItem struct {
	Key  string `json:"key"`
	URL  string `json:"url"`
	Mime string `json:"mime"`
}

// TestMediaListCodeAssetURLServesOnItsOwnHost is the acceptance test: the url
// GET /v1/rest/media hands the merchant for a CSS asset, fetched on its own
// host, answers 200 text/css — and the canonical media host still refuses the
// same object (it resolved to no account), which is the defect this card fixed.
func TestMediaListCodeAssetURLServesOnItsOwnHost(t *testing.T) {
	h, pool, accountID, slug, cssKey, pngKey, store := mediaURLFixture(t)
	ctx := context.Background()

	cssRow := &dbstore.Media{
		AccountID: accountID, Key: cssKey, URL: store.PublicURL(cssKey),
		Mime: "text/css; charset=utf-8", Size: int64(len(seededCSS)), Source: dbstore.MediaSourceMerchant,
	}
	if err := h.Media.Create(ctx, cssRow); err != nil {
		t.Fatalf("create css media row: %v", err)
	}
	// A code asset is served only once the scan has flipped it to 'ok'
	// (issue #364). The library lists it regardless; the advertised url is what
	// a merchant clicks, and it must work for a serveable asset.
	if _, err := pool.Exec(ctx, `UPDATE media SET scan_status='ok' WHERE id=$1`, cssRow.ID); err != nil {
		t.Fatalf("mark css scanned: %v", err)
	}
	imgRow := &dbstore.Media{
		AccountID: accountID, Key: pngKey, URL: store.PublicURL(pngKey),
		Mime: "image/png", Size: 3, Source: dbstore.MediaSourceMerchant,
	}
	if err := h.Media.Create(ctx, imgRow); err != nil {
		t.Fatalf("create png media row: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/media", nil)
	h.listMedia(rec, req, &AccountCtx{Account: &dbstore.Account{ID: accountID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []mediaListItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v (%s)", err, rec.Body.String())
	}
	byKey := map[string]mediaListItem{}
	for _, it := range body.Items {
		byKey[it.Key] = it
	}
	css, ok := byKey[cssKey]
	if !ok {
		t.Fatalf("css asset %q is not in the list (%s)", cssKey, rec.Body.String())
	}
	img, ok := byKey[pngKey]
	if !ok {
		t.Fatalf("png asset %q is not in the list (%s)", pngKey, rec.Body.String())
	}

	// 1. A code asset is advertised on the account's PUBLIC host.
	wantCSS := "https://" + slug + ".jomform.com/media/" + cssKey
	if css.URL != wantCSS {
		t.Fatalf("code-asset url = %q, want %q (the account's public host)", css.URL, wantCSS)
	}

	// 2. The emitted url, fetched on its own host, serves the bytes.
	served := serveAdvertised(t, h, css.URL, "")
	if served.Code != http.StatusOK {
		t.Fatalf("GET %s (Host %s) = %d, want 200 (body %s)",
			css.URL, slug+".jomform.com", served.Code, served.Body.String())
	}
	if ct := served.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Fatalf("GET %s content-type = %q, want text/css; charset=utf-8", css.URL, ct)
	}
	if served.Body.String() != string(seededCSS) {
		t.Fatalf("GET %s body = %q, want the stored bytes", css.URL, served.Body.String())
	}

	// 3. The OLD advertised url (the canonical media host) still refuses the
	//    code asset: that is why the host had to change, and it keeps this test
	//    from passing if the fix had simply pointed the url at a host that
	//    serves anything.
	canonical := store.PublicURL(cssKey)
	if canonical == css.URL {
		t.Fatalf("code-asset url is still the canonical media-host url %q", canonical)
	}
	refused := serveAdvertised(t, h, canonical, "jomform.com")
	if refused.Code != http.StatusNotFound {
		t.Fatalf("GET %s (canonical host) = %d, want 404 — the object is tenant-scoped (issue #379)",
			canonical, refused.Code)
	}

	// 4. Images are untouched: canonical url, served on the canonical host.
	pngCanonical := store.PublicURL(pngKey)
	if img.URL != pngCanonical {
		t.Fatalf("image url = %q, want the unchanged canonical %q", img.URL, pngCanonical)
	}
	imgServed := serveAdvertised(t, h, img.URL, "")
	if imgServed.Code != http.StatusOK {
		t.Fatalf("GET %s (image) = %d, want 200 (body %s)", img.URL, imgServed.Code, imgServed.Body.String())
	}
	if ct := imgServed.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Fatalf("GET %s content-type = %q, want image/png", img.URL, ct)
	}
}

// TestAssetUploadURLServesOnItsOwnHost proves the UPLOAD response is wired the
// same way as the list: the url POST /v1/rest/uploads/asset returns — the first
// url a merchant ever sees for the file — is the one that serves it once the
// scan has cleared the asset.
func TestAssetUploadURLServesOnItsOwnHost(t *testing.T) {
	h, pool, accountID, slug, _, _, _ := mediaURLFixture(t)
	ctx := context.Background()
	raw := bundleAPIKey(t, ctx, pool, accountID)

	code, body := signatureUpload(t, h, raw, "/v1/rest/uploads/asset", "theme.css", "text/css", seededCSS)
	if code != http.StatusOK {
		t.Fatalf("POST /v1/rest/uploads/asset = %d, want 200 (body %s)", code, body)
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode upload response %q: %v", body, err)
	}
	wantPrefix := "https://" + slug + ".jomform.com/media/u/" + accountID + "/"
	if !strings.HasPrefix(resp.URL, wantPrefix) {
		t.Fatalf("upload url = %q, want a url on the account's public host (%q …)", resp.URL, wantPrefix)
	}

	// The background scan is what makes a code asset serveable; do its job, then
	// fetch the url the merchant was handed.
	key := strings.TrimPrefix(resp.URL, "https://"+slug+".jomform.com/media/")
	if _, err := pool.Exec(ctx,
		`UPDATE media SET scan_status='ok' WHERE account_id=$1 AND key=$2`, accountID, key); err != nil {
		t.Fatalf("mark uploaded asset scanned: %v", err)
	}
	served := serveAdvertised(t, h, resp.URL, "")
	if served.Code != http.StatusOK {
		t.Fatalf("GET %s (its own host) = %d, want 200 (body %s)", resp.URL, served.Code, served.Body.String())
	}
	if ct := served.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Fatalf("GET %s content-type = %q, want text/css; charset=utf-8", resp.URL, ct)
	}
}
