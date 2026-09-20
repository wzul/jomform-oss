// Language placement — the end-to-end acceptance test from the card:
//
//   "A public form belonging to a workspace set to English serves
//    <html lang="en"> with English chrome — not <html lang="ms">."
//
// This is the assertion that ties the WHOLE chain together against a real
// Postgres: the workspace's stored default_lang → resolveWorkspaceLang →
// effectiveLangStrings → the rendered page. The unit tests in
// lang_placement_test.go render the templates directly with a nil store, so they
// prove the templates follow whatever language they are handed; this file proves
// the language they are handed actually comes from the workspace's saved setting.
//
// Without this, the change could be "correct" in every isolated piece while the
// real path still served Malay (e.g. if resolveWorkspaceLang resolved to en but
// the handler kept passing a literal).

package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestPublicFormFollowsTheWorkspaceDefaultLanguage drives GET /{slug} on the
// workspace subdomain through the real handler and asserts the page language
// tracks the workspace's saved default_lang — including the negative, so an
// "always Malay" renderer cannot pass.
func TestPublicFormFollowsTheWorkspaceDefaultLanguage(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	h, workspaceSlug, formSlug := publicFixture(t)

	// publicFixture creates its own workspace; resolve its id so we can change
	// the language the same way /settings does (through the store).
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM workspaces WHERE slug = $1`, workspaceSlug).Scan(&workspaceID); err != nil {
		t.Fatalf("lookup workspace id: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	// The fixture form has no per-form language, so the workspace default is
	// what decides — exactly the merchant who sets one language for the whole
	// storefront.
	// The fixture form renders through the BLOCK renderer, whose chrome is the
	// submit button / total label — that is what must follow the language.
	cases := []struct {
		lang         string
		wantAttr     string
		wantChrome   string
		bannedChrome string
	}{
		{"en", `<html lang="en">`, "Proceed to Payment", "Teruskan ke Pembayaran"},
		{"ms", `<html lang="ms">`, "Teruskan ke Pembayaran", "Proceed to Payment"},
	}

	for _, tc := range cases {
		t.Run("workspace default "+tc.lang, func(t *testing.T) {
			st.DefaultLang = tc.lang
			if err := sp.SaveSettings(ctx, st); err != nil {
				t.Fatalf("save default_lang=%s: %v", tc.lang, err)
			}

			// The resolution reads the workspace's current setting on every
			// request, so the same handler serves the new language — no cache to
			// invalidate.
			rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /%s status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()

			if !strings.Contains(body, tc.wantAttr) {
				t.Errorf("workspace default_lang=%s but the page did not serve %s", tc.lang, tc.wantAttr)
			}
			// The bug: a workspace set to English still serving Malay chrome.
			for _, other := range []string{"en", "ms"} {
				if other == tc.lang {
					continue
				}
				if strings.Contains(body, `<html lang="`+other+`">`) {
					t.Errorf("workspace default_lang=%s but the page served <html lang=\"%s\">", tc.lang, other)
				}
			}
			if tc.wantChrome != "" && !strings.Contains(body, tc.wantChrome) {
				t.Errorf("workspace default_lang=%s but the page lacks the %q chrome", tc.lang, tc.wantChrome)
			}
			if tc.bannedChrome != "" && strings.Contains(body, tc.bannedChrome) {
				t.Errorf("workspace default_lang=%s but the page still shows the other language's chrome %q",
					tc.lang, tc.bannedChrome)
			}
		})
	}
}

// TestPublicFormPerFormLanguageBeatsTheWorkspaceDefault pins the precedence the
// card's fallback chain relies on: a form with its own config.lang must NOT be
// overridden by the workspace default. Without this, a workspace-wide default
// would silently clobber a merchant's deliberate per-form choice.
func TestPublicFormPerFormLanguageBeatsTheWorkspaceDefault(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h, workspaceSlug, formSlug := publicFixture(t)

	var workspaceID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM workspaces WHERE slug = $1`, workspaceSlug).Scan(&workspaceID); err != nil {
		t.Fatalf("lookup workspace id: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	st, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	// Workspace says Malay...
	st.DefaultLang = "ms"
	if err := sp.SaveSettings(ctx, st); err != nil {
		t.Fatalf("save default_lang=ms: %v", err)
	}

	// ...but this form explicitly says English.
	var formID string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM forms WHERE slug = $1`, formSlug).Scan(&formID); err != nil {
		t.Fatalf("lookup form id: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`UPDATE forms SET config = config || '{"lang":"en"}'::jsonb
		 WHERE id = $1 AND account_id = (SELECT account_id FROM forms WHERE id = $1)
		 RETURNING id`, formID).Scan(&formID); err != nil {
		t.Fatalf("set per-form lang: %v", err)
	}

	rec := publicServe(t, h, workspaceSlug+".jomform.com", "/"+formSlug)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /%s status = %d, want 200 (body %s)", formSlug, rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("a per-form config.lang=en did not override the workspace default (ms)")
	}
	if strings.Contains(body, `<html lang="ms">`) {
		t.Errorf("the workspace default overrode the form's own language choice")
	}
}
