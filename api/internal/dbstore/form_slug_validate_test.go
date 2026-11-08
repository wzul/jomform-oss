package dbstore

// Card t_f76f81eb: a form's slug had NO validator on the create path, and the
// rename leg only checked shape. Measured on prod v1.25.0 before this rule:
//
//	POST  /v1/rest/forms      {"slug":"bad slug!!/x"} -> 201, url ".../bad slug!!/x"
//	PATCH /v1/rest/forms/{id} {"slug":"login"}        -> 200
//
// The first is a public address with raw spaces and an extra path segment that
// no router can serve; the second puts the merchant's form on a label JomForm
// owns (or, for /privacy, on a route the API serves on the same host).
//
// WHAT THIS FILE PINS
//
//  1. The rule itself, as a table: shape (charset, 1-40) and the reserved leg
//     (workspace-claim labels reused + the API's own single-segment GET routes),
//     including the positive direction so the rule does not become a blanket
//     denial ("kedai-kopi", "order", "menu" must stay valid).
//  2. FormStore.Create applies it — the choke point every create path reaches —
//     and writes the NORMALISED value, and a refusal inserts nothing.
//  3. FormStore.RenameSlug applies it to the policy leg too, and a refused
//     rename leaves the row and the redirect table untouched.
//  4. FormStore.UniqueSlug (the one generator) never returns a slug its own
//     validator refuses, including for a title that slugifies onto a reserved
//     label ("Login" -> "login").
//
// The DB-backed half runs against JOMFORM_TEST_DATABASE_URL and skips when it is
// unset (testPool).

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestValidateFormSlugRule is the rule, read as a table.
func TestValidateFormSlugRule(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		wantErr error
		why     string
	}{
		// ── shape ──────────────────────────────────────────────────────────
		{in: "kedai-kopi", want: "kedai-kopi", why: "an ordinary merchant slug"},
		{in: "  My-Form  ", want: "my-form", why: "trimmed and lower-cased, and the write uses the normalised value"},
		{in: "order", want: "order", why: "a form named after what it does is legitimate"},
		{in: "menu", want: "menu", why: "the menu flow moved to the literal /menu-order so /menu is a free form slug"},
		{in: "a", want: "a", why: "the documented window is 1-40"},
		{in: strings.Repeat("a", 40), want: strings.Repeat("a", 40), why: "40 is the cap"},
		{in: "", wantErr: ErrInvalidSlug, why: "empty is not a path segment"},
		{in: "   ", wantErr: ErrInvalidSlug, why: "whitespace-only is empty after normalisation"},
		{in: strings.Repeat("a", 41), wantErr: ErrInvalidSlug, why: "41 is over the cap"},
		{in: "bad slug!!/x", wantErr: ErrInvalidSlug, why: "the exact value prod accepted: space, bang, slash"},
		{in: "has_underscore", wantErr: ErrInvalidSlug, why: "underscore is not in a-z0-9-"},
		{in: "dot.dot", wantErr: ErrInvalidSlug, why: "a dot would make the path a different route"},
		{in: "UPPER-CASE-DASH", want: "upper-case-dash", why: "upper case is normalised, not refused"},

		// ── reserved: the labels JomForm owns or protects (reused list) ────
		{in: "login", wantErr: ErrFormSlugReserved, why: "the exact value prod's PATCH accepted"},
		{in: "Login", wantErr: ErrFormSlugReserved, why: "the reserved leg runs after normalisation"},
		{in: "api", wantErr: ErrFormSlugReserved, why: "the API host label"},
		{in: "docs", wantErr: ErrFormSlugReserved, why: "a routed JomForm surface"},
		{in: "billing", wantErr: ErrFormSlugReserved, why: "an impersonation label"},
		{in: "jomform-anything", wantErr: ErrFormSlugReserved, why: "the jomform- prefix rule is reused, so a future surface is covered"},

		// ── reserved: routes the API serves on the SAME host ───────────────
		{in: "privacy", wantErr: ErrFormSlugReserved, why: "GET /privacy is the hosted legal page and wins over /{slug}"},
		{in: "terms", wantErr: ErrFormSlugReserved, why: "same, /terms"},
		{in: "refund", wantErr: ErrFormSlugReserved, why: "same, /refund"},
		{in: "healthz", wantErr: ErrFormSlugReserved, why: "the liveness probe answers on every host"},
	} {
		got, err := ValidateFormSlug(tc.in)
		if tc.wantErr != nil {
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ValidateFormSlug(%q) error = %v, want %v (%s)", tc.in, err, tc.wantErr, tc.why)
			}
			if got != "" {
				t.Errorf("ValidateFormSlug(%q) returned the value %q alongside a refusal — a refusal must not hand a caller something to write", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateFormSlug(%q) = %v, want it accepted (%s)", tc.in, err, tc.why)
			continue
		}
		if got != tc.want {
			t.Errorf("ValidateFormSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// The policy refusal must NAME the label and say why, because the merchant
	// otherwise reads it as their own input being malformed and retries a
	// variation that is still reserved.
	_, err := ValidateFormSlug("login")
	if err == nil || !strings.Contains(err.Error(), "login") || !strings.Contains(strings.ToLower(err.Error()), "reserved") {
		t.Errorf("the reserved refusal %q does not name the label and say it is reserved", err)
	}
	// And it must be English: the API never receives the caller's language.
	msg := err.Error()
	for _, malay := range []string{"tidak", "sudah", "borang", "pilih"} {
		if strings.Contains(strings.ToLower(msg), malay) {
			t.Errorf("the reserved refusal carries a Malay literal (%q): %q — the API does not know the caller's language (#6177)", malay, msg)
		}
	}
}

// slugFixture seeds a workspace-linked account (the per-workspace slug index is
// only reachable through one — see createCollideFixture's control).
func slugFixture(t *testing.T) (context.Context, *FormStore, string) {
	t.Helper()
	ctx := context.Background()
	p := testPool(t)
	acc, _ := createCollideFixture(t, ctx)
	return ctx, NewFormStore(p), acc
}

// TestFormCreateRefusesAnUnusableSlug is the create half of the card: what prod
// accepted with a 201 must never reach storage.
func TestFormCreateRefusesAnUnusableSlug(t *testing.T) {
	ctx, fs, acc := slugFixture(t)

	for _, bad := range []string{"bad slug!!/x", "login", "privacy", ""} {
		f := &Form{AccountID: acc, Slug: bad, Title: "probe", Status: FormStatusDraft}
		err := fs.Create(ctx, f)
		if err == nil {
			t.Errorf("Create accepted the slug %q — it was stored and served as a public URL", bad)
			continue
		}
		// Nothing was inserted for the refused create.
		var n int
		if qerr := fs.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM forms WHERE account_id=$1 AND deleted_at IS NULL AND title='probe'`,
			acc).Scan(&n); qerr != nil {
			t.Fatalf("count forms: %v", qerr)
		}
		if n != 0 {
			t.Errorf("Create refused the slug %q but left %d row(s) behind — the refusal must not half-write", bad, n)
		}
	}

	// The control: a legitimate slug still creates, and the value written is the
	// NORMALISED one (so the URL the response reports is the URL that is served).
	f := &Form{AccountID: acc, Slug: "  Kedai-Kopi  ", Title: "probe", Status: FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("Create refused a legitimate slug: %v", err)
	}
	if f.Slug != "kedai-kopi" {
		t.Errorf("Create stored/reported %q, want the normalised %q", f.Slug, "kedai-kopi")
	}
	var stored string
	if err := fs.pool.QueryRow(ctx, `SELECT slug FROM forms WHERE id=$1`, f.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored slug: %v", err)
	}
	if stored != "kedai-kopi" {
		t.Errorf("forms.slug = %q, want %q", stored, "kedai-kopi")
	}
}

// TestFormRenameRefusesAReservedSlug is the update half: prod answered 200 for
// {"slug":"login"}. The refusal must be the policy sentinel, and neither the row
// nor the redirect table may move.
func TestFormRenameRefusesAReservedSlug(t *testing.T) {
	ctx, fs, acc := slugFixture(t)

	slug := "it-rename-keep"
	f := &Form{AccountID: acc, Slug: slug, Title: "rename me", Status: FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("seed form: %v", err)
	}

	old, err := fs.RenameSlug(ctx, acc, f.ID, "login")
	if err == nil {
		t.Fatalf("RenameSlug onto the reserved label \"login\" succeeded — on a custom domain that form's " +
			"public path becomes the app's own route (the exact defect)")
	}
	if !errors.Is(err, ErrFormSlugReserved) {
		t.Fatalf("RenameSlug onto a reserved label = %v, want ErrFormSlugReserved (the API maps that sentinel "+
			"to 400 form.slugReserved)", err)
	}
	if old != "" {
		t.Errorf("a refused rename reported the previous slug as %q — nothing changed, so it must be empty", old)
	}

	var stored string
	if err := fs.pool.QueryRow(ctx, `SELECT slug FROM forms WHERE id=$1`, f.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored slug: %v", err)
	}
	if stored != slug {
		t.Errorf("forms.slug = %q after a refused rename, want %q untouched", stored, slug)
	}
	var redirects int
	if err := fs.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM form_slug_redirects WHERE form_id=$1`, f.ID).Scan(&redirects); err != nil {
		t.Fatalf("count redirects: %v", err)
	}
	if redirects != 0 {
		t.Errorf("a REFUSED rename left %d redirect row(s) — the old URL now 301s to a slug the form never took",
			redirects)
	}
}

// TestRenameSlugLeavesAPreRuleSlugAlone is the compatibility half of the
// ordering: a form stored BEFORE this rule existed can carry a name the rule now
// refuses (prod answered 200 for "login"), and its settings must stay saveable.
// Re-sending the slug it already has claims nothing, so it is a no-op — while
// CLAIMING such a name, or keeping one after moving away, is still refused.
func TestRenameSlugLeavesAPreRuleSlugAlone(t *testing.T) {
	ctx, fs, acc := slugFixture(t)

	f := &Form{AccountID: acc, Slug: "legacy-row", Title: "stored before the rule", Status: FormStatusDraft}
	if err := fs.Create(ctx, f); err != nil {
		t.Fatalf("seed form: %v", err)
	}
	// Reach the state the defect produced: a reserved slug already in the table.
	// Written directly because Create now refuses it — which is the point.
	if _, err := fs.pool.Exec(ctx, `UPDATE forms SET slug='login' WHERE id=$1`, f.ID); err != nil {
		t.Fatalf("simulate a pre-rule row: %v", err)
	}

	old, err := fs.RenameSlug(ctx, acc, f.ID, "login")
	if err != nil {
		t.Fatalf("re-sending the slug the form already has = %v, want a no-op — otherwise a form stored "+
			"before the rule can never have its settings saved again", err)
	}
	if old != "" {
		t.Errorf("a no-op rename reported the previous slug as %q, want empty (nothing moved)", old)
	}
	// Normalisation is part of the same comparison: "LOGIN" is the same claim.
	if _, err := fs.RenameSlug(ctx, acc, f.ID, "  LOGIN  "); err != nil {
		t.Errorf("re-sending the same slug in another case = %v, want a no-op", err)
	}
	var redirects int
	if err := fs.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM form_slug_redirects WHERE form_id=$1`, f.ID).Scan(&redirects); err != nil {
		t.Fatalf("count redirects: %v", err)
	}
	if redirects != 0 {
		t.Errorf("a no-op rename wrote %d redirect row(s)", redirects)
	}

	// But it is still refused when it IS a claim: moving onto another reserved
	// label, and coming back to this one after leaving it.
	if _, err := fs.RenameSlug(ctx, acc, f.ID, "privacy"); !errors.Is(err, ErrFormSlugReserved) {
		t.Errorf("renaming a legacy row onto another reserved label = %v, want ErrFormSlugReserved", err)
	}
	if _, err := fs.RenameSlug(ctx, acc, f.ID, "legacy-row-2"); err != nil {
		t.Fatalf("rename a legacy row onto a legitimate slug: %v", err)
	}
	if _, err := fs.RenameSlug(ctx, acc, f.ID, "login"); !errors.Is(err, ErrFormSlugReserved) {
		t.Errorf("re-claiming the reserved label after moving away = %v, want ErrFormSlugReserved "+
			"(only an UNCHANGED slug is a no-op)", err)
	}
}

// TestUniqueSlugNeverMintsAValueTheRuleRefuses pins the generator against the
// rule it feeds. A title that slugifies onto a reserved label is the case that
// made create fail for a reason the caller never typed once the create path
// started validating.
func TestUniqueSlugNeverMintsAValueTheRuleRefuses(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc, _ := createCollideFixture(t, ctx)
	fs := NewFormStore(p)

	for _, title := range []string{"Login", "privacy", "api docs", "   ", strings.Repeat("x", 60), "Kedai Kopi"} {
		got := fs.UniqueSlug(ctx, acc, title)
		if _, err := ValidateFormSlug(got); err != nil {
			t.Errorf("UniqueSlug(%q) = %q, which its own validator refuses (%v) — a create with a blank slug "+
				"would fail for a reason the caller never typed", title, got, err)
		}
	}
	if got := fs.UniqueSlug(ctx, acc, "Login"); got == "login" {
		t.Errorf("UniqueSlug(\"Login\") = %q — the reserved label was minted verbatim", got)
	}

	// The uniqueness half still works: seeding the slug the generator returned
	// makes the next call for the same title return something else.
	a := fs.UniqueSlug(ctx, acc, "Kedai Kopi")
	if err := fs.Create(ctx, &Form{AccountID: acc, Slug: a, Title: "first", Status: FormStatusDraft}); err != nil {
		t.Fatalf("seed the slug UniqueSlug returned: %v", err)
	}
	if b := fs.UniqueSlug(ctx, acc, "Kedai Kopi"); b == a {
		t.Errorf("UniqueSlug returned the already-taken %q twice", a)
	}
}
