// Language placement — the workspace's buyer-facing default language
// (workspace_settings.default_lang), driven through the REAL REST handler.
//
// THE BUG THIS PINS
//   workspace_settings.default_lang holds the buyer-facing language. SaveSettings
//   writes EVERY column from the struct it is handed, so the obvious
//   implementation — `DefaultLang: in.DefaultLang` in the fresh literal — means
//   any request that omitted the field supplies Go's zero value "", which
//   SaveSettings persists. It does not error: WorkspaceLang() coerces "" (and any
//   unrecognised code) to "en" on the way into the statement, so the merchant's
//   choice is SILENTLY reset to English by the next unrelated save ("change my
//   footer") and their language appears not to stick.
//
//   DefaultLang therefore has to be read-modify-written like currency: seed the
//   literal from the CURRENT settings row and only overwrite it when the request
//   actually supplied a code. This test asserts that, plus the 400 on an
//   unsupported code, so a stored "fr" (a language the renderer has no strings
//   for) can never reach the storefront.
//
// Plus the store-level round trip, so a SELECT/Scan column misalignment (the
// repo's silent-401 class) fails here instead of on a live request.

package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// workspaceLangFixture creates an isolated user + workspace + account and the
// workspace's settings row, returning the ids the REST route needs.
func workspaceLangFixture(t *testing.T, ctx context.Context, prefix string) (userID, workspaceID string) {
	t.Helper()
	p := bundleTestPool(t)

	if err := p.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ($1 || '-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`, prefix).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceSlug string
	if err := p.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, $2 || '-' || md5(random()::text), $2 || '-' || md5(random()::text))
		 RETURNING id, slug`, userID, prefix).Scan(&workspaceID, &workspaceSlug); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := p.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 SELECT $1, $2 || '-acc-' || md5(random()::text), $2 || '-' || md5(random()::text), id
		 FROM workspaces WHERE slug = $3
		 RETURNING id`, userID, prefix, workspaceSlug).Scan(new(string)); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := dbstore.NewWorkspaceStore(p).Settings(ctx, workspaceID); err != nil {
		t.Fatalf("materialise settings row: %v", err)
	}
	return userID, workspaceID
}

// TestWorkspaceDefaultLangSavesAndSurvivesUnrelatedSaves is the read-modify-write
// guard. It drives the real PUT /v1/workspaces/{id}/settings handler.
func TestWorkspaceDefaultLangSavesAndSurvivesUnrelatedSaves(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	userID, workspaceID := workspaceLangFixture(t, ctx, "lang-ws")

	sp := dbstore.NewWorkspaceStore(pool)

	h := newBundleHandlers(pool)
	sess := authTestValkey(t)
	h.Sessions = sess
	token, err := sess.Create(ctx, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/v1/workspaces/"+workspaceID+"/settings",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. Set a non-default language. This is the merchant choosing Mandarin for
	//    their storefront.
	if rec := put(`{"default_lang":"zh"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT default_lang=zh status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if st.DefaultLang != "zh" {
		t.Fatalf("default_lang after save = %q, want \"zh\"", st.DefaultLang)
	}

	// 2. Now save something COMPLETELY UNRELATED, exactly as the Storefront tab
	//    does. Two failure modes are possible here and both are fatal:
	//      * 500 — the omitted field wrote "" into the NOT NULL CHECK column;
	//      * 200 but zh silently reset to en.
	rec := put(`{"footer_text":"saved by an unrelated settings change"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("unrelated PUT status = %d, want 200 — an omitted default_lang must not "+
			"write the zero value into a NOT NULL CHECK column (body %s)", rec.Code, rec.Body.String())
	}
	st, err = sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if st.DefaultLang != "zh" {
		t.Errorf("workspace %s: an unrelated settings save reset default_lang to %q (want \"zh\") — "+
			"the read-modify-write no longer preserves the field, so a merchant's language choice "+
			"appears not to stick", workspaceID, st.DefaultLang)
	}

	// 3. An unsupported code is refused, not stored — a stored \"fr\" would leave
	//    the storefront on a language the renderer has no strings for.
	if rec := put(`{"default_lang":"fr"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT default_lang=fr status = %d, want 400", rec.Code)
	}
	st, err = sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if st.DefaultLang != "zh" {
		t.Errorf("rejected code changed stored default_lang to %q, want \"zh\" unchanged", st.DefaultLang)
	}
}

// TestWorkspaceDefaultLangStoreRoundTrip is the column-alignment guard: the
// workspace_settings select + scan must carry default_lang correctly, or this
// reads back the wrong value (or errors) rather than failing to compile.
func TestWorkspaceDefaultLangStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, workspaceID := workspaceLangFixture(t, ctx, "lang-rt")
	sp := dbstore.NewWorkspaceStore(pool)

	base, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	// A fresh workspace must default to a real supported code, never "" — the
	// column is NOT NULL.
	if !dbstore.ValidProfileLang(base.DefaultLang) || base.DefaultLang == "" {
		t.Fatalf("fresh workspace default_lang = %q, want a supported code", base.DefaultLang)
	}

	for _, code := range []string{"en", "ms", "zh", "ta", "ar"} {
		base.DefaultLang = code
		if err := sp.SaveSettings(ctx, base); err != nil {
			t.Fatalf("SaveSettings(%q): %v", code, err)
		}
		got, err := sp.Settings(ctx, workspaceID)
		if err != nil {
			t.Fatalf("Settings after SaveSettings(%q): %v", code, err)
		}
		if got.DefaultLang != code {
			t.Errorf("default_lang round-trip: wrote %q, read %q", code, got.DefaultLang)
		}
	}
}
