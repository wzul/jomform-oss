package httpapi

// t_5f6a5d37: a merchant code asset stored BEFORE t_8881000c keeps the type the
// old sniffer recorded. The visible case is a stylesheet that opens with a
// licence/theme comment: `/* ... */` is byte-identical in CSS and JS, it was
// read as a JS signature, and the file was stored `.js` /
// `text/javascript`. `/media` and `/assets` publish that type with
// `X-Content-Type-Options: nosniff`, so a browser refuses the sheet as
// `<link rel="stylesheet">` — silently: the page loads, nothing errors, and the
// merchant's CSS does not apply until they delete and re-upload the file.
//
// The serve path now trusts the bytes for the css/js pair (t_5f6a5d37) and
// corrects the media row with it, so the media library (GET /v1/rest/media) and
// the MCP listings — which all report media.mime — name the type the object is
// actually served with. These tests drive the real router against a real
// Postgres media row and an in-memory object store.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

const (
	legacyMimeCSS = "text/css; charset=utf-8"
	legacyMimeJS  = "text/javascript; charset=utf-8"

	// legacyCSSBody is the body measured on prod v1.21.220 during t_0c3b261d:
	// a comment-led stylesheet stored under a `.js` key.
	legacyCSSBody = "/* qa media probe t_0c3b261d: CSS with a leading comment */\n.jf-qa-probe{color:#123456;}\n"
)

type legacyAssetFixture struct {
	t       *testing.T
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *fakeObjectStore
	media   *dbstore.MediaStore
	rest    *RESTHandlers
	public  *PublicHandlers
	account string
	slug    string
	rawKey  string
}

// newLegacyAssetFixture seeds one user, one workspace + its account, and wires
// both serving handlers over an in-memory object store and the real media
// store, exactly as production does.
func newLegacyAssetFixture(t *testing.T) *legacyAssetFixture {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping legacy code-asset serve test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('leg-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	ws, err := sp.Create(ctx, userID, "Legacy assets", "leg-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	acc := accountIDForWorkspace(t, ctx, pool, ws.ID)
	if acc == "" {
		t.Fatalf("workspace %s owns no account", ws.ID)
	}

	store := newFakeObjectStore()
	media := dbstore.NewMediaStore(pool)
	accounts := dbstore.NewAccountStore(pool)
	domains := dbstore.NewDomainStore(pool)
	f := &legacyAssetFixture{
		t: t, ctx: ctx, pool: pool, store: store, media: media,
		account: acc, slug: ws.Slug,
		rawKey: bundleAPIKey(t, ctx, pool, acc),
		rest: &RESTHandlers{
			Accounts: accounts, Domains: domains, Workspaces: sp,
			Keys:  dbstore.NewAPIKeyStore(pool),
			Store: store, Media: media,
		},
		public: &PublicHandlers{
			Accounts: accounts, Domains: domains, Workspaces: sp,
			Store: store, Media: media,
		},
	}
	return f
}

// seed stores an object with the given metadata and records a media row for it
// with the given mime, then marks it scanned: the issue #364 code-asset gate
// withholds an unscanned asset for a reason unrelated to its type.
func (f *legacyAssetFixture) seed(name, storedType string, mime string, body []byte) string {
	f.t.Helper()
	key := "u/" + f.account + "/" + uuid.NewString() + name
	if err := f.store.PutObject(f.ctx, key, storedType, body); err != nil {
		f.t.Fatalf("seed object %q: %v", key, err)
	}
	row := &dbstore.Media{
		AccountID: f.account, Key: key, URL: f.store.PublicURL(key),
		Mime: mime, Size: int64(len(body)), Source: dbstore.MediaSourceMerchant,
	}
	if err := f.media.Create(f.ctx, row); err != nil {
		f.t.Fatalf("seed media row %q: %v", key, err)
	}
	if err := f.media.SetScanStatus(f.ctx, f.account, row.ID, "ok", ""); err != nil {
		f.t.Fatalf("mark %q scanned: %v", key, err)
	}
	return key
}

// recordedMime reads the media row back — the value every listing surface
// reports.
func (f *legacyAssetFixture) recordedMime(key string) string {
	f.t.Helper()
	row, err := f.media.ByKey(f.ctx, f.account, key)
	if err != nil {
		f.t.Fatalf("media ByKey(%q): %v", key, err)
	}
	if row == nil {
		f.t.Fatalf("no media row for %q", key)
	}
	return row.Mime
}

// serveMedia drives GET /media/{key} on the workspace subdomain (the tenant the
// key belongs to).
func (f *legacyAssetFixture) serveMedia(key string) *httptest.ResponseRecorder {
	f.t.Helper()
	return restMediaServe(f.t, f.rest, f.slug+".jomform.com", key)
}

// TestLegacyCommentLedStylesheetIsServedAsCSS is the card's acceptance test.
// Before the fix the stored type is published verbatim, so the browser refuses
// the sheet; after it the object is served text/css and the media row — read by
// GET /v1/rest/media and by MCP list_media — is corrected to match.
func TestLegacyCommentLedStylesheetIsServedAsCSS(t *testing.T) {
	f := newLegacyAssetFixture(t)
	key := f.seed(".js", legacyMimeJS, legacyMimeJS, []byte(legacyCSSBody))

	// Reproduce the legacy state before trusting any green.
	if got := f.recordedMime(key); got != legacyMimeJS {
		t.Fatalf("seeded media.mime = %q, want the legacy %q", got, legacyMimeJS)
	}

	rec := f.serveMedia(key)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/%s = %d (%s), want 200", key, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != legacyMimeCSS {
		t.Fatalf("legacy .js key holding a comment-led stylesheet served %q, want %q", got, legacyMimeCSS)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff (the header that makes the type decision visible)", got)
	}
	if got := rec.Body.String(); got != legacyCSSBody {
		t.Errorf("served body = %q, want the stored bytes unchanged %q", got, legacyCSSBody)
	}

	// The media library and the MCP listings report media.mime, so the row must
	// have moved with the served type.
	if got := f.recordedMime(key); got != legacyMimeCSS {
		t.Errorf("media.mime after serving = %q, want %q (library/MCP would keep naming the old type)", got, legacyMimeCSS)
	}

	items := f.listMedia()
	found := false
	for _, it := range items {
		if it["key"] == key {
			found = true
			if it["mime"] != legacyMimeCSS {
				t.Errorf("GET /v1/rest/media reports mime %q for the legacy stylesheet, want %q", it["mime"], legacyMimeCSS)
			}
		}
	}
	if !found {
		t.Fatalf("media library listing does not include %q", key)
	}

	// Serving again is stable: the correction is a one-shot rewrite, not a
	// per-request flip-flop.
	rec = f.serveMedia(key)
	if got := rec.Header().Get("Content-Type"); got != legacyMimeCSS {
		t.Errorf("second serve reported %q, want %q", got, legacyMimeCSS)
	}
}

// TestLegacyScriptRecordedAsCSSIsServedAsJS is the converse direction: a script
// stored under a `.css` key is refused by a browser as an external script under
// nosniff, so the bytes decide here too.
func TestLegacyScriptRecordedAsCSSIsServedAsJS(t *testing.T) {
	f := newLegacyAssetFixture(t)
	body := "/* licence */\nconst answer = 42;\n"
	key := f.seed(".css", legacyMimeCSS, legacyMimeCSS, []byte(body))

	rec := f.serveMedia(key)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /media/%s = %d (%s), want 200", key, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != legacyMimeJS {
		t.Fatalf("legacy .css key holding a script served %q, want %q", got, legacyMimeJS)
	}
	if got := f.recordedMime(key); got != legacyMimeJS {
		t.Errorf("media.mime after serving = %q, want %q", got, legacyMimeJS)
	}
}

// TestTruthfulCodeAssetsKeepTheirType guards the correction from over-reaching:
// a correctly stored script stays a script (including a minified IIFE, which a
// brace heuristic alone reads as CSS), and a correctly stored stylesheet stays
// a stylesheet.
func TestTruthfulCodeAssetsKeepTheirType(t *testing.T) {
	f := newLegacyAssetFixture(t)
	cases := []struct {
		name string
		key  string
		mime string
		body string
	}{
		{"genuine-js", ".js", legacyMimeJS, "const x = 1;\n"},
		{"minified-iife-js", ".js", legacyMimeJS, "/*! Bootstrap v5.3.0 */\n!function(e){var t={};e.x=t}();"},
		{"comment-led-js", ".js", legacyMimeJS, "/* theme */\nmodule.exports = {a: 1};\n"},
		{"genuine-css", ".css", legacyMimeCSS, "/* licence */\n.jf{color:#123456}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key := f.seed(c.key, c.mime, c.mime, []byte(c.body))
			rec := f.serveMedia(key)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /media/%s = %d (%s), want 200", key, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != c.mime {
				t.Errorf("truthful object served %q, want its recorded %q", got, c.mime)
			}
			if got := f.recordedMime(key); got != c.mime {
				t.Errorf("media.mime = %q, want it untouched at %q", got, c.mime)
			}
		})
	}
}

// TestPublicAssetRouteMakesTheSameCorrection: the workspace/custom-domain asset
// route serves the same objects with the same header, so a stylesheet linked
// from a customised landing page is corrected there too.
func TestPublicAssetRouteMakesTheSameCorrection(t *testing.T) {
	f := newLegacyAssetFixture(t)
	key := f.seed(".js", legacyMimeJS, legacyMimeJS, []byte(legacyCSSBody))

	rec := publicAssetServe(t, f.public, f.slug+".jomform.com", key)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/%s = %d (%s), want 200", key, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != legacyMimeCSS {
		t.Fatalf("GET /assets served the legacy stylesheet as %q, want %q", got, legacyMimeCSS)
	}
	if got := f.recordedMime(key); got != legacyMimeCSS {
		t.Errorf("media.mime after the asset route served it = %q, want %q", got, legacyMimeCSS)
	}
}

// listMedia drives the authenticated media library endpoint and returns its
// items.
func (f *legacyAssetFixture) listMedia() []map[string]any {
	f.t.Helper()
	mux := http.NewServeMux()
	f.rest.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/rest/media", nil)
	req.Header.Set("Authorization", "Bearer "+f.rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("GET /v1/rest/media = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		f.t.Fatalf("decode media listing: %v (%s)", err, rec.Body.String())
	}
	return body.Items
}
