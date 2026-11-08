package dbstore

// Card t_1768cb11 — a form's default notification rule must not be born in one
// language, and no email may change language when that stops happening.
//
// WHAT THIS PINS
//
//  1. EnsureDefault (the rule every new form gets) stores an EMPTY subject and
//     body. Before migration 000084 those rows held the MALAY rendering of the
//     message (000032 for payment forms, 000062 for response forms), and the
//     dashboard renders the stored value verbatim (form settings -> Notifications
//     -> Edit), so an English merchant opening a rule he never wrote saw Malay in
//     Subject and Body and it read as his own content.
//  2. The 000084 backfill blanks ONLY a half that is byte-for-byte the
//     pre-language literal, per field — for BOTH kinds the system ever wrote
//     (sale.paid and response.submitted). A merchant's own wording survives
//     verbatim, including the reachable half-edited shape the rule editor
//     produces (it posts both halves back on save).
//  3. The resolved text of an empty rule is still the dictionary's template in
//     the form's language, so blanking the row is a display fix: the message that
//     leaves is the same one the sender produced from the Malay literal (see
//     payments.TestUnwrittenNotificationRuleSendsTheSameBytesAsTheLegacyLiteral
//     for the byte-for-byte, all-five-locales half of that claim).
//
// The backfill cases EXECUTE the shipped migration file (read through
// db.MigrationStatements, the same embed the API applies at boot) rather than a
// copy of its SQL: if the literal in the migration stops matching
// emailtext.LegacyDefault by one byte, the "still holds the default" row is not
// rewritten and case 2 goes RED.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/db"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// notificationFixture creates one form and returns it, so each case owns the
// rule row it asserts on.
func notificationFixture(t *testing.T, ctx context.Context, p *pgxpool.Pool, acc, slug string, cfg map[string]any) *Form {
	t.Helper()
	f := &Form{
		AccountID: acc, Slug: slug, Title: slug,
		Config: cfg, Theme: map[string]any{}, Status: FormStatusDraft,
	}
	if err := NewFormStore(p).Create(ctx, f); err != nil {
		t.Fatalf("create form %s: %v", slug, err)
	}
	return f
}

// readNotificationRow returns the stored subject/body of a form's only rule,
// asserting the row exists (a fixture bug must not read as "empty").
func readNotificationRow(t *testing.T, ctx context.Context, p *pgxpool.Pool, formID string) (subject, body string) {
	t.Helper()
	if err := p.QueryRow(ctx,
		`SELECT subject, body FROM form_notifications WHERE form_id=$1`, formID,
	).Scan(&subject, &body); err != nil {
		t.Fatalf("read form_notifications for %s: %v", formID, err)
	}
	return subject, body
}

// TestNewFormStoresNoNotificationTemplate is the card's core: the rule a new
// form gets holds nothing, so no language is pinned in the row and the dashboard
// has nothing Malay to render on an English page.
func TestNewFormStoresNoNotificationTemplate(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)
	notif := NewFormNotificationStore(p)

	for _, tc := range []struct {
		mode  string
		event string
		kind  emailtext.Kind
	}{
		{"payment", NotifyEventSalePaid, emailtext.NotificationSale},
		{"response", NotifyEventResponseSubmitted, emailtext.NotificationResponse},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := notificationFixture(t, ctx, p, acc,
				"no-template-"+tc.mode+"-"+acc[:8], map[string]any{"mode": tc.mode, "blocks": []any{}})
			if err := notif.EnsureDefault(ctx, f); err != nil {
				t.Fatalf("EnsureDefault: %v", err)
			}
			subject, body := readNotificationRow(t, ctx, p, f.ID)
			if subject != "" || body != "" {
				t.Errorf("a new form's default rule stores a template: subject=%q body=%q — a language is "+
					"pinned into a field the dashboard renders verbatim", subject, body)
			}

			// The default definition must agree with the row it writes.
			_, event, wantSubject, wantBody := DefaultNotification(tc.mode)
			if event != tc.event {
				t.Errorf("DefaultNotification(%q) event = %q, want %q", tc.mode, event, tc.event)
			}
			if wantSubject != "" || wantBody != "" {
				t.Errorf("DefaultNotification(%q) still carries a template: subject=%q body=%q",
					tc.mode, wantSubject, wantBody)
			}

			// Empty is not "no message": it resolves to the dictionary's template
			// in the form's language, which is what the sender does.
			en := emailtext.Text(emailtext.En, tc.kind, nil)
			if en.Subject == "" || en.Body == "" {
				t.Fatalf("test setup: the dictionary has no English %s template", tc.kind)
			}
			if subject == en.Subject || body == en.Body {
				t.Errorf("the stored row holds the English template — the point is that it holds NO language")
			}
		})
	}
}

// TestNotificationBackfillClearsOnlyTheSystemDefault executes the SHIPPED 000084
// migration over rows that simulate a pre-upgrade database.
func TestNotificationBackfillClearsOnlyTheSystemDefault(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	acc := uniqueAccount(t, ctx, p)

	saleSubject, saleBody := emailtext.LegacyDefault(emailtext.NotificationSale)
	respSubject, respBody := emailtext.LegacyDefault(emailtext.NotificationResponse)
	if saleSubject == "" || saleBody == "" || respSubject == "" || respBody == "" {
		t.Fatal("test setup: a notification legacy default is missing from the dictionary")
	}

	const merchantSubject = "Your order is confirmed"
	const merchantBody = "Thanks! Keep this note."

	// The pre-upgrade payment default (000032's backfill, and EnsureDefault
	// before this release).
	untouchedSale := notificationFixture(t, ctx, p, acc, "legacy-sale-"+acc[:8],
		map[string]any{"mode": "payment", "blocks": []any{}})
	seedNotificationRow(t, ctx, p, untouchedSale.ID, saleSubject, saleBody)
	// The pre-upgrade response default (000062's repair).
	untouchedResponse := notificationFixture(t, ctx, p, acc, "legacy-resp-"+acc[:8],
		map[string]any{"mode": "response", "blocks": []any{}})
	seedNotificationRow(t, ctx, p, untouchedResponse.ID, respSubject, respBody)
	// A fully customised rule: both halves are the merchant's words.
	custom := notificationFixture(t, ctx, p, acc, "custom-"+acc[:8], map[string]any{"mode": "payment"})
	seedNotificationRow(t, ctx, p, custom.ID, merchantSubject, merchantBody)
	// The reachable half-edited shape: the editor posts both halves back, so a
	// merchant who edited only the body stores the system subject verbatim.
	half := notificationFixture(t, ctx, p, acc, "half-"+acc[:8], map[string]any{"mode": "payment"})
	seedNotificationRow(t, ctx, p, half.ID, saleSubject, merchantBody)

	// Apply migration 000084 exactly as the API would at boot.
	migrations, err := db.MigrationStatements()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	const want = "000084_notification_default_language_neutral.up.sql"
	var sql string
	for _, m := range migrations {
		if m.Name == want {
			sql = m.SQL
			break
		}
	}
	if sql == "" {
		t.Fatalf("%s is not in the embedded migration set — the backfill never runs in production either", want)
	}
	if _, err := p.Exec(ctx, sql); err != nil {
		t.Fatalf("apply %s: %v", want, err)
	}

	// 1. Both untouched defaults are emptied — the system's own text is not
	// content, in either kind.
	if s, b := readNotificationRow(t, ctx, p, untouchedSale.ID); s != "" || b != "" {
		t.Errorf("the backfill left the sale default in place: subject=%q body=%q", s, b)
	}
	if s, b := readNotificationRow(t, ctx, p, untouchedResponse.ID); s != "" || b != "" {
		t.Errorf("the backfill left the response default in place: subject=%q body=%q", s, b)
	}
	// 2. A merchant's wording is never rewritten.
	if s, b := readNotificationRow(t, ctx, p, custom.ID); s != merchantSubject || b != merchantBody {
		t.Errorf("the backfill rewrote merchant copy: subject=%q body=%q, want %q/%q",
			s, b, merchantSubject, merchantBody)
	}
	// 3. Judged per half: the system subject goes, the merchant body stays.
	if s, b := readNotificationRow(t, ctx, p, half.ID); s != "" || b != merchantBody {
		t.Errorf("the backfill did not judge each half on its own: subject=%q body=%q, want \"\"/%q",
			s, b, merchantBody)
	}
}

// seedNotificationRow writes a rule with the given stored halves, so the backfill
// test can reproduce a pre-upgrade row exactly.
func seedNotificationRow(t *testing.T, ctx context.Context, p *pgxpool.Pool, formID, subject, body string) {
	t.Helper()
	if _, err := p.Exec(ctx,
		`INSERT INTO form_notifications (form_id, name, event, to_email, subject, body, is_default)
		 VALUES ($1, 'Admin Notification', 'sale.paid', '{admin_email}', $2, $3, true)`,
		formID, subject, body); err != nil {
		t.Fatalf("seed notification row for %s: %v", formID, err)
	}
}
