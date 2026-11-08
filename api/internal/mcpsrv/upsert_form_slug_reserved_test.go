package mcpsrv

// Card t_f76f81eb on the MCP door: upsert_form's create and update halves must
// be held to the SAME form-slug rule the REST routes enforce, because both reach
// the same storage. Before this fix the MCP create path stored whatever the
// agent sent (a slug with spaces or a "/" produced a public URL no router could
// serve) and the update path accepted a RESERVED label, so an agent could put a
// merchant's form on a path JomForm itself answers.
//
// Assertions are read from the DATABASE and from the tool's own error, never from
// a success payload: a refusal that still wrote would be the defect wearing an
// error message.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped when
// unset (testdb.Open).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestUpsertFormCreateRefusesAnUnusableSlug: the values prod accepted through
// REST must be refused through the tool too, and nothing may be inserted.
func TestUpsertFormCreateRefusesAnUnusableSlug(t *testing.T) {
	st := newUpsertSlugFixture(t)

	for _, tc := range []struct {
		slug string
		want error
		why  string
	}{
		{"bad slug!!/x", dbstore.ErrInvalidSlug, "the value prod stored with a 201: space, bang, slash"},
		{"has_underscore", dbstore.ErrInvalidSlug, "underscore is outside a-z0-9-"},
		{"login", dbstore.ErrFormSlugReserved, "the reserved label prod's PATCH accepted"},
		{"privacy", dbstore.ErrFormSlugReserved, "a route this API serves on the same host"},
	} {
		_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
			Title: "probe " + tc.slug, Slug: tc.slug, Config: map[string]any{"blocks": []any{}},
		})
		if err == nil {
			t.Errorf("upsert_form CREATE accepted the slug %q (%s)", tc.slug, tc.why)
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("upsert_form CREATE slug %q = %v, want %v — the tool must report the store's named "+
				"refusal, not an opaque message", tc.slug, err, tc.want)
		}
		var n int
		if qerr := st.pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND title=$2 AND deleted_at IS NULL`,
			st.accountID, "probe "+tc.slug).Scan(&n); qerr != nil {
			t.Fatalf("count forms: %v", qerr)
		}
		if n != 0 {
			t.Errorf("upsert_form refused the slug %q but stored %d row(s) — the agent is told the write "+
				"failed while the row exists", tc.slug, n)
		}
	}
}

// TestUpsertFormUpdateRefusesAReservedSlug: the rename half. Neither the slug nor
// the title may move on a refused rename (the title write used to land before the
// slug write, card t_0b7980f5).
func TestUpsertFormUpdateRefusesAReservedSlug(t *testing.T) {
	st := newUpsertSlugFixture(t)
	slug := "i-upt-reserved-" + randHex(4)
	formID := st.seedForm(t, slug)

	_, _, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		FormID: formID, Title: "Renamed by a refused tool call", Slug: "login",
	})
	if err == nil {
		t.Fatalf("upsert_form renamed a form onto the reserved label \"login\" — on a custom domain its "+
			"public path becomes %q", "/login")
	}
	if !errors.Is(err, dbstore.ErrFormSlugReserved) {
		t.Fatalf("upsert_form onto a reserved label = %v, want dbstore.ErrFormSlugReserved", err)
	}
	if !strings.Contains(err.Error(), "login") {
		t.Errorf("the refusal %q does not name the refused label, so the agent cannot tell what to change", err)
	}
	if got := st.storedSlug(t, formID); got != slug {
		t.Errorf("forms.slug = %q after a REFUSED rename, want %q untouched", got, slug)
	}
	if got := st.storedTitle(t, formID); got != "Western menu" {
		t.Errorf("forms.title = %q after a REFUSED rename, want the seeded \"Western menu\"", got)
	}
}

// TestUpsertFormCreateGeneratedSlugAvoidsReservedLabels: the tool's blank-slug
// contract still works, and the generator cannot mint a value the rule refuses.
func TestUpsertFormCreateGeneratedSlugAvoidsReservedLabels(t *testing.T) {
	st := newUpsertSlugFixture(t)

	_, out, err := st.srv.upsertForm(st.ctx, nil, upsertFormIn{
		Title: "Login", Config: map[string]any{"blocks": []any{}},
	})
	if err != nil {
		t.Fatalf("upsert_form CREATE with a blank slug = %v, want it to generate one", err)
	}
	slug := out["slug"]
	if slug == "" {
		t.Fatalf("the create reported no slug: %v", out)
	}
	if _, verr := dbstore.ValidateFormSlug(slug); verr != nil {
		t.Fatalf("upsert_form generated the slug %q, which its own write path refuses: %v", slug, verr)
	}
	if slug == "login" {
		t.Fatalf("upsert_form generated the RESERVED label \"login\" for the title \"Login\" — the create " +
			"would have failed for a reason the caller never typed")
	}
}
