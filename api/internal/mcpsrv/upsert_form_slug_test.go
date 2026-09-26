package mcpsrv

// Regression guard for the MCP half of the carded defect: `upsert_form`'s own
// description documents `slug` as "Slug URL unik" and passes it through, but on
// the UPDATE path (FormID != "") the parameter was ignored. An agent told to
// "set the slug" got back {"status":"updated"} and no change — a tool whose
// description was false, on the merchant's own data.
//
// The fix routes the update path through the same FormStore.RenameSlug the
// dashboard's slug field uses, so the tool inherits the REST endpoint's whole
// contract rather than re-implementing it: the same validation, the same
// per-workspace uniqueness (a collision surfaces to the agent rather than
// silently no-op-ing) and the same temporary 301 for the old URL so a printed
// QR stand and a menu's order_form_slug reference keep resolving.
//
// Assertions (each one the card's own acceptance criterion, read from the
// DATABASE and the serving lookup — never from the tool's response):
//
//  1. An update with a slug MOVES forms.slug (the defect: it did not move).
//  2. An update with NO slug leaves the stored value alone.
//  3. A collision with another live form's slug is REFUSED, and neither form
//     moves.
//  4. The update response reports the slug the form now owns, so the agent is
//     not told about a URL that does not exist.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when
// unset.

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

type upsertSlugState struct {
	srv       *Server
	ctx       context.Context
	accountID string
	pool      *pgxpool.Pool
}

func newUpsertSlugFixture(t *testing.T) upsertSlugState {
	t.Helper()
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, workspaceID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('i-slug-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'i-slug-ws-' || md5(random()::text), 'i-slug-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1, 'i-slug-acc-' || md5(random()::text), 'i-slug-' || md5(random()::text), $2)
		 RETURNING id`, userID, workspaceID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	srv := &Server{deps: &Deps{
		Accounts:   dbstore.NewAccountStore(pool),
		Forms:      dbstore.NewFormStore(pool),
		Workspaces: dbstore.NewWorkspaceStore(pool),
	}}
	return upsertSlugState{
		srv:       srv,
		ctx:       WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}}),
		accountID: accountID,
		pool:      pool,
	}
}

func (st upsertSlugState) seedForm(t *testing.T, slug string) string {
	t.Helper()
	f := &dbstore.Form{
		AccountID: st.accountID, Slug: slug, Title: "Western menu",
		Status: dbstore.FormStatusDraft, Config: map[string]any{"blocks": []any{}}, Theme: map[string]any{},
	}
	if err := dbstore.NewFormStore(st.pool).Create(context.Background(), f); err != nil {
		t.Fatalf("seed form %s: %v", slug, err)
	}
	return f.ID
}

func (st upsertSlugState) storedSlug(t *testing.T, formID string) string {
	t.Helper()
	var slug string
	if err := st.pool.QueryRow(context.Background(),
		`SELECT slug FROM forms WHERE id=$1`, formID).Scan(&slug); err != nil {
		t.Fatalf("read stored slug: %v", err)
	}
	return slug
}

// TestUpsertFormUpdateHonoursSlug: the tool's documented parameter must actually
// move the form on the update path.
func TestUpsertFormUpdateHonoursSlug(t *testing.T) {
	st := newUpsertSlugFixture(t)
	oldSlug := "i-upt-old-" + randHex(4)
	formID := st.seedForm(t, oldSlug)
	newSlug := "i-upt-new-" + randHex(4)

	_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Western menu", Slug: newSlug,
	})
	if err != nil {
		t.Fatalf("upsert_form update with a slug: %v", err)
	}
	if got := st.storedSlug(t, formID); got != newSlug {
		t.Fatalf("forms.slug = %q after upsert_form with slug=%q, want %q.\n"+
			"The tool documents `slug` as \"Slug URL unik\"; before this it was honoured only on "+
			"create, so an agent setting the slug got {\"status\":\"updated\"} and no change.", got, newSlug, newSlug)
	}
	if out["slug"] != newSlug {
		t.Errorf("upsert_form reported slug=%v, want %q — the agent must not be told about a URL the form does not serve", out["slug"], newSlug)
	}
	if out["status"] != "updated" {
		t.Errorf("upsert_form status=%v, want \"updated\"", out["status"])
	}

	// The old URL is not orphaned: it resolves to the form through the rename's
	// temporary 301, so a printed QR or a menu reference keeps working.
	acc, err := st.srv.deps.Accounts.ByID(context.Background(), st.accountID)
	if err != nil || acc == nil {
		t.Fatalf("read account: %v", err)
	}
	redirected, rerr := st.srv.deps.Forms.RedirectedFormByAccountSlug(context.Background(), acc.Slug, oldSlug)
	if rerr != nil || redirected == nil {
		t.Fatalf("upsert_form's rename left no trace for the old slug %q: %v", oldSlug, rerr)
	}
	if redirected.ID != formID {
		t.Errorf("the old slug resolves to form %s, want %s", redirected.ID, formID)
	}
}

// TestUpsertFormUpdateWithoutSlugKeepsIt: an agent that only edits config must
// not move the form's URL.
func TestUpsertFormUpdateWithoutSlugKeepsIt(t *testing.T) {
	st := newUpsertSlugFixture(t)
	slug := "i-upt-keep-" + randHex(4)
	formID := st.seedForm(t, slug)

	_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Renamed title",
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Hi"}}},
	})
	if err != nil {
		t.Fatalf("upsert_form update without a slug: %v", err)
	}
	if got := st.storedSlug(t, formID); got != slug {
		t.Fatalf("forms.slug = %q after an upsert_form that omitted the slug, want the stored %q.\n"+
			"An omitted field must mean \"leave unchanged\", or every config edit moves the form's URL.", got, slug)
	}
	if out["slug"] != slug {
		t.Errorf("upsert_form reported slug=%v, want the unchanged %q", out["slug"], slug)
	}
}

// TestUpsertFormUpdateSlugCollisionIsRefused: two forms in one workspace must
// not share a slug, and the refusal must be an ERROR the agent sees — never a
// silent no-op reported as success.
func TestUpsertFormUpdateSlugCollisionIsRefused(t *testing.T) {
	st := newUpsertSlugFixture(t)
	firstSlug := "i-upt-first-" + randHex(4)
	firstForm := st.seedForm(t, firstSlug)
	takenSlug := "i-upt-taken-" + randHex(4)
	otherForm := st.seedForm(t, takenSlug)

	_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: firstForm, Title: "Western menu", Slug: takenSlug,
	})
	if err == nil {
		t.Fatalf("upsert_form onto a slug another live form serves succeeded; a collision must be refused "+
			"(%v)", dbstore.ErrSlugTaken)
	}
	if !strings.Contains(err.Error(), "slug") {
		t.Errorf("the refusal does not mention the slug: %v", err)
	}
	// Neither form moved.
	if got := st.storedSlug(t, firstForm); got != firstSlug {
		t.Errorf("the renamed form's slug is %q on a refused rename, want %q untouched", got, firstSlug)
	}
	if got := st.storedSlug(t, otherForm); got != takenSlug {
		t.Errorf("the form owning the slug now has %q, want %q untouched", got, takenSlug)
	}
}

// TestUpsertFormUpdateSlugRejectsUnusableValue: the same validation the REST
// endpoint applies, reached through the MCP tool.
func TestUpsertFormUpdateSlugRejectsUnusableValue(t *testing.T) {
	st := newUpsertSlugFixture(t)
	slug := "i-upt-valid-" + randHex(4)
	formID := st.seedForm(t, slug)

	for _, bad := range []string{"has/slash", "dot.dot", strings.Repeat("a", 41)} {
		if _, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
			FormID: formID, Title: "Western menu", Slug: bad,
		}); err == nil {
			t.Errorf("upsert_form accepted the unusable slug %q", bad)
		}
	}
	if got := st.storedSlug(t, formID); got != slug {
		t.Errorf("a rejected slug changed the stored value to %q, want %q", got, slug)
	}
}
