// Issue #597: migration 000062 must repair the dead default notification on a
// RESPONSE-mode form, and must not touch anything else.
//
// The existing fresh-DB guard (TestMigrationsApplyFromZero) applies every up
// migration to an EMPTY database, where this migration's UPDATE matches nothing
// — so it cannot prove the part that only matters on a database that already
// holds rows (production holds a default rule on every non-deleted form). This
// test covers exactly that gap, and it covers the BLAST RADIUS, which matters
// more here than the happy path: the migration must move only the form's own
// untouched default rule on a response-mode form.
//
// Seeded, all in one scratch database:
//   - a response form with its untouched default rule   → repaired
//   - a response form whose default rule was EDITED      → untouched
//   - a response form with a CUSTOM (non-default) rule   → untouched
//   - a payment/legacy form with its default rule        → untouched
//   - a deleted response form with its default rule      → untouched
//
// Then the DOWN is applied and must reverse exactly the rows the UP changed.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set (same convention as the rest
// of the package).
package db

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// responseDefaultBody and saleDefaultBody mirror the templates the migration
// rewrote. They are duplicated here on purpose: a test that imported the values
// from the code under test would pass even if the migration's WHERE clause
// stopped matching, because both sides would move together.
const (
	saleDefaultSubject = "Jualan baharu — {{ref_code}}"
	saleDefaultBody    = "Jualan baharu diterima!\n\nRujukan : {{ref_code}}\nJumlah  : RM {{amount}}\nItem    : {{items}}\nPembeli : {{buyer_name}}\nBorang  : {{form_url}}"
	responseSubject    = "Pendaftaran baharu diterima"
	responseBody       = "Pendaftaran baharu diterima!\n\nNama   : {{full_name}}\nEmel   : {{email}}\nBorang : {{form_url}}\nTarikh : {{date}}"
)

func TestResponseFormDefaultNotificationMigration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dsn := os.Getenv("JOMFORM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping response-form notification migration test")
	}

	name := createScratchDB(t, ctx, dsn)
	pool, err := Connect(ctx, dsnFor(t, dsn, name))
	if err != nil {
		t.Fatalf("connect to scratch database %s: %v", name, err)
	}
	defer pool.Close()

	up := migrationFile(t, "000062_response_form_default_notification.up.sql")
	down := migrationFile(t, "000062_response_form_default_notification.down.sql")

	// Current schema, through the same runner the API uses at startup. This
	// includes 000062 itself (the embed covers every up), so its effect is then
	// undone below to reproduce the pre-release state the migration targets.
	if err := Migrate(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("apply migrations to scratch database: %v", err)
	}
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("000062 down did not apply — the rollback must be runnable: %v", err)
	}
	// The down must also be re-runnable (a rollback nobody dares run twice is a
	// rollback nobody dares run).
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("000062 down is not idempotent: %v", err)
	}

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('it-597-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).
		Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'it-597-' || md5(random()::text), 'it-' || md5(random()::text)) RETURNING id`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// form inserts one form and its default rule, returning the rule id.
	form := func(slug, mode string, deleted bool) (formID, ruleID string) {
		t.Helper()
		cfg := `{"blocks":[]}`
		if mode != "" {
			cfg = `{"mode":"` + mode + `","blocks":[]}`
		}
		deletedExpr := "NULL"
		if deleted {
			deletedExpr = "now()"
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO forms (account_id, slug, title, config, theme, status, deleted_at)
			 VALUES ($1, $2, $3, $4::jsonb, '{}'::jsonb, 'published', `+deletedExpr+`)
			 RETURNING id`, accountID, slug, slug, cfg).Scan(&formID); err != nil {
			t.Fatalf("create form %s: %v", slug, err)
		}
		if err := pool.QueryRow(ctx,
			`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
			 VALUES ($1, 'Admin Notification', 'sale.paid', '{admin_email}', $2, $3, true)
			 RETURNING id`, formID, saleDefaultSubject, saleDefaultBody).Scan(&ruleID); err != nil {
			t.Fatalf("create default rule for %s: %v", slug, err)
		}
		return formID, ruleID
	}

	respID, respRuleID := form("it-597-resp", "response", false)
	_, editedRuleID := form("it-597-edited", "response", false)
	// A merchant edit: the subject changed, so the migration must leave it be.
	if _, err := pool.Exec(ctx,
		`UPDATE form_notifications SET subject = 'My own subject', updated_at = now() WHERE id = $1`,
		editedRuleID); err != nil {
		t.Fatalf("edit rule: %v", err)
	}
	payID, payRuleID := form("it-597-pay", "payment", false)
	legacyID, legacyRuleID := form("it-597-legacy", "", false)
	deletedID, deletedRuleID := form("it-597-deleted", "response", true)

	// A merchant's own extra rule on the response form — not flagged default, so
	// the migration must not touch it even though it too is a sale.paid rule.
	var customRuleID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
		 VALUES ($1, 'My rule', 'sale.paid', 'me@example.test', $2, $3, false)
		 RETURNING id`, respID, saleDefaultSubject, saleDefaultBody).Scan(&customRuleID); err != nil {
		t.Fatalf("create custom rule: %v", err)
	}

	// ── UP, on a database that already holds rows ──
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("000062 failed on a database that already holds rows: %v", err)
	}

	rule := func(id string) (event, subject, body string) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT event, subject, body FROM form_notifications WHERE id = $1`, id).
			Scan(&event, &subject, &body); err != nil {
			t.Fatalf("read rule %s: %v", id, err)
		}
		return
	}

	// Repaired: the reported case.
	ev, subj, bod := rule(respRuleID)
	if ev != "response.submitted" {
		t.Errorf("response form %s default rule event = %q, want %q — leave it on sale.paid and the "+
			"merchant is never told anyone responded", respID, ev, "response.submitted")
	}
	if subj != responseSubject || bod != responseBody {
		t.Errorf("response form default rule content was not updated:\nsubject=%q\nbody=%q", subj, bod)
	}

	// Untouched: a merchant edit.
	if ev, subj, _ := rule(editedRuleID); ev != "sale.paid" || subj != "My own subject" {
		t.Errorf("the migration seized a merchant-EDITED default rule (event %q, subject %q)", ev, subj)
	}
	// Untouched: a merchant's non-default rule.
	if ev, subj, _ := rule(customRuleID); ev != "sale.paid" || subj != saleDefaultSubject {
		t.Errorf("the migration seized a merchant's own (non-default) rule (event %q, subject %q)", ev, subj)
	}
	// Untouched: a payment form — the 31 live sale.paid rules in production.
	if ev, _, _ := rule(payRuleID); ev != "sale.paid" {
		t.Errorf("the migration changed a PAYMENT form's default rule event to %q — every sales form "+
			"must keep sale.paid (%s)", ev, payID)
	}
	// Untouched: a legacy form with no mode key — it is a payment form too.
	if ev, _, _ := rule(legacyRuleID); ev != "sale.paid" {
		t.Errorf("the migration changed a LEGACY (no mode key) form's default rule to %q — those are "+
			"payment forms with real sales (%s)", ev, legacyID)
	}
	// Untouched: a deleted response form.
	if ev, _, _ := rule(deletedRuleID); ev != "sale.paid" {
		t.Errorf("the migration changed a DELETED form's default rule to %q (%s)", ev, deletedID)
	}

	// The event CHECK still holds the new value.
	if _, err := pool.Exec(ctx,
		`UPDATE form_notifications SET event = 'nonsense' WHERE id = $1`, respRuleID); err == nil {
		t.Error("the event CHECK no longer rejects an unknown event")
	}

	// ── DOWN, on the populated table ──
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("000062 down failed on a populated table: %v", err)
	}
	ev, subj, bod = rule(respRuleID)
	if ev != "sale.paid" || subj != saleDefaultSubject || bod != saleDefaultBody {
		t.Errorf("the down did not reverse the repaired rule (event %q, subject %q)", ev, subj)
	}
	// And it left the rows it never touched alone.
	if ev, subj, _ := rule(editedRuleID); ev != "sale.paid" || subj != "My own subject" {
		t.Errorf("the down damaged a merchant-edited rule (event %q, subject %q)", ev, subj)
	}
	if ev, _, _ := rule(payRuleID); ev != "sale.paid" {
		t.Errorf("the down changed a payment form's rule event to %q", ev)
	}
}
