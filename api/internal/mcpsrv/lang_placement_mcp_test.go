// Language placement — MCP coverage for `update_profile_language`.
//
// WHY THIS EXISTS
//   mcp_parity_test.go asserts the tool is REGISTERED. That is a presence check:
//   a handler that registered and then rejected every call, or wrote to the wrong
//   column, would still pass it. This file invokes the real handler and asserts
//   the effect on the DATABASE — the per-profile dashboard language must actually
//   round-trip, and an unsupported code must be refused rather than stored.
//
//   The handler is called directly, with the identity in the context, exactly as
//   the repo's other tool tests do (e.g. issue622MCPFixture): the in-memory MCP
//   transport does not carry a per-call Go context through to the server-side
//   handler, so a protocol round trip cannot assert identity-scoped behaviour.
//
//   The distinction this tool must not blur (and which the workspace default
//   language test covers): update_profile_language sets the MERCHANT's own
//   dashboard language on users.lang. It must never touch
//   workspace_settings.default_lang, which is what the merchant's CUSTOMERS see.

package mcpsrv

import (
	"context"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

type langState struct {
	srv    *Server
	ctx    context.Context
	userID string
	users  *dbstore.UserStore
}

func langFixture(t *testing.T) langState {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('lang-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	users := dbstore.NewUserStore(pool)
	srv := &Server{deps: &Deps{Users: users}}
	// The tool resolves the person from the call identity (issue #307: an OAuth
	// token is bound to the user directly).
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{UserID: userID, Scopes: []string{"readwrite"}})
	return langState{srv: srv, ctx: callCtx, userID: userID, users: users}
}

// setLang runs the real tool handler and returns its error, if any.
func (f langState) setLang(t *testing.T, lang string) error {
	t.Helper()
	_, _, err := f.srv.updateProfileLanguage(f.ctx, nil, updateProfileLanguageIn{Lang: lang})
	return err
}

func (f langState) storedLang(t *testing.T) string {
	t.Helper()
	u, err := f.users.ByID(context.Background(), f.userID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	return u.Lang
}

// TestMCPUpdateProfileLanguageRoundTrips drives the tool and reads the column
// back, so a registered-but-broken handler (wrong column, silent no-op) fails.
func TestMCPUpdateProfileLanguageRoundTrips(t *testing.T) {
	st := langFixture(t)

	for _, code := range []string{"ms", "zh", "ar"} {
		if err := st.setLang(t, code); err != nil {
			t.Fatalf("update_profile_language(%q): %v", code, err)
		}
		if got := st.storedLang(t); got != code {
			t.Errorf("MCP set %q but users.lang = %q", code, got)
		}
	}

	// Clearing is how the dashboard returns to "Auto" (browser value, then en).
	if err := st.setLang(t, ""); err != nil {
		t.Fatalf("update_profile_language(\"\"): %v", err)
	}
	if got := st.storedLang(t); got != "" {
		t.Errorf("MCP clear left users.lang = %q, want empty", got)
	}
}

// TestMCPUpdateProfileLanguageRejectsUnsupported: a code outside the five must be
// refused with an error, not written — the column's CHECK would reject it, and a
// silent no-op would leave the caller believing it worked.
func TestMCPUpdateProfileLanguageRejectsUnsupported(t *testing.T) {
	st := langFixture(t)

	// Seed a known good value so "unchanged" is distinguishable from "never set".
	if err := st.setLang(t, "ms"); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	for _, code := range []string{"fr", "de", "EN", "english"} {
		err := st.setLang(t, code)
		if err == nil {
			t.Errorf("update_profile_language(%q) was accepted; an unsupported code must be rejected", code)
			continue
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("update_profile_language(%q) error = %q, want an unsupported-language message", code, err)
		}
		if got := st.storedLang(t); got != "ms" {
			t.Errorf("rejected code %q changed users.lang to %q, want \"ms\" unchanged", code, got)
		}
	}
}

// TestMCPUpdateProfileLanguageDoesNotTouchTheWorkspaceDefault is the guard against
// the two settings being conflated: a merchant changing THEIR OWN dashboard
// language must never change what their CUSTOMERS see on a public form.
func TestMCPUpdateProfileLanguageDoesNotTouchTheWorkspaceDefault(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Open(t)
	st := langFixture(t)

	var workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'lang-ws-' || md5(random()::text), 'lang-ws-' || md5(random()::text))
		 RETURNING id`, st.userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	sp := dbstore.NewWorkspaceStore(pool)
	before, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if before.DefaultLang != "en" {
		t.Fatalf("fresh workspace default_lang = %q, want \"en\"", before.DefaultLang)
	}

	if err := st.setLang(t, "ar"); err != nil {
		t.Fatalf("update_profile_language(ar): %v", err)
	}

	after, err := sp.Settings(ctx, workspaceID)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if after.DefaultLang != before.DefaultLang {
		t.Errorf("setting the profile dashboard language changed the workspace's buyer-facing "+
			"default_lang from %q to %q — the two settings are being conflated",
			before.DefaultLang, after.DefaultLang)
	}
}
