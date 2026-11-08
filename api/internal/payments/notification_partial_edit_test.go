// A PARTIALLY-EDITED notification rule must still follow the buyer's language on
// the half the merchant did not touch.
//
// The rule editor (web/src/app/form-settings/page.tsx openEditNotification /
// saveNotification) loads the stored subject AND body into one form and posts
// both back on save. A merchant who edits only the body therefore sends the
// untouched system subject back verbatim, and the row that reaches the database
// holds the Malay literal #597's EnsureDefault wrote next to copy he wrote
// himself. Judging that row as a PAIR answers "the merchant wrote all of this",
// so the Malay subject used to go out to a buyer whose form is English.
//
// These tests observe the message that actually leaves: a real *email.Sender
// talking to a fake SESv2 endpoint (AWS_ENDPOINT_URL_SESV2), through the real
// sendNotification path. The oracle is the SES request body, not a mock of the
// translation rule — a unit test over the dictionary cannot show which bytes a
// buyer receives.
package payments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailtext"
)

// sesRequest is the part of the SESv2 SendEmail request body these tests read.
// The nested shape mirrors the SDK's EmailContent/Message/Content/Body types.
type sesRequest struct {
	Destination struct {
		ToAddresses []string `json:"ToAddresses"`
	} `json:"Destination"`
	Content struct {
		Simple struct {
			Subject struct {
				Data string `json:"Data"`
			} `json:"Subject"`
			Body struct {
				Text struct {
					Data string `json:"Data"`
				} `json:"Text"`
				HTML struct {
					Data string `json:"Data"`
				} `json:"Html"`
			} `json:"Body"`
		} `json:"Simple"`
	} `json:"Content"`
}

// sesRecorder is a fake SESv2 endpoint that captures every SendEmail body. A
// captured request is proof the message left the process, and its subject/body
// are the exact strings the buyer would read.
type sesRecorder struct {
	requests []sesRequest
}

// startSESRecorder points the SDK at a local endpoint and returns the recorder.
func startSESRecorder(t *testing.T) *sesRecorder {
	t.Helper()
	rec := &sesRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body sesRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec.requests = append(rec.requests, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MessageId":"fake-ses-message-id"}`))
	}))
	t.Cleanup(srv.Close)
	// The SDK reads this when it builds the client (sesv2 endpoints resolver), so
	// the sender's every send lands here and never at AWS.
	t.Setenv("AWS_ENDPOINT_URL_SESV2", srv.URL)
	return rec
}

// newObservedService wires a Service whose notification sends are observed. No
// EmailQueue and no Suppressor: the synchronous send path runs, which is the
// same branch a bare deployment (and this test) takes.
func newObservedService(t *testing.T) (*Service, *sesRecorder) {
	t.Helper()
	rec := startSESRecorder(t)
	sender := email.New(email.SMTPConfig{
		From: "noreply@jomform.com", Region: "ap-southeast-1",
		// Static credentials so the client never probes the instance metadata
		// service (which would hang this test).
		Username: "AKIAEXAMPLE", Password: "secretexample",
	})
	return &Service{Receipts: &Receipts{Sender: sender}}, rec
}

// fixedRules is a FormNotificationLister that returns one prepared rule.
type fixedRules struct{ rules []*dbstore.FormNotification }

func (f fixedRules) ListByFormEvent(_ context.Context, _, _ string) ([]*dbstore.FormNotification, error) {
	return f.rules, nil
}

// buyerAxisRef is a notification ref on the buyer axis: the form is English.
func buyerAxisRef(formID string) NotificationRef {
	return NotificationRef{
		FormID: formID, RefCode: "JF-MIX-1",
		AmountSen: 2500, ItemsSummary: "1x Tee",
		BuyerName: "Aisyah", FormURL: "https://acme.jomform.com/order",
		Fields: map[string]string{"full_name": "Aisyah", "email": "aisyah@example.test"},
		// The form's own config.lang — the axis a notification follows.
		FormLang: emailtext.En,
	}
}

// expectedMessage runs the production pipeline for one kind/lang/ref past the
// language step: the dictionary renders the template, then the notification
// template engine substitutes the {{placeholders}}. The tests assert on THIS,
// because the placeholders are substituted before the message leaves, so
// comparing the sent subject against the bare template would be wrong (and would
// hide a broken substitution).
func expectedMessage(t *testing.T, lang string, kind emailtext.Kind, ref NotificationRef) (subject, body string) {
	t.Helper()
	def := emailtext.Text(lang, kind, nil)
	return renderNotification(def.Subject, def.Body, ref)
}

// TestPartiallyEditedNotificationFollowsTheLanguageOnTheUntouchedHalf is the
// defect pinned end to end: the rule holds the system's Malay subject beside a
// body the merchant wrote, and the buyer's form is English.
func TestPartiallyEditedNotificationFollowsTheLanguageOnTheUntouchedHalf(t *testing.T) {
	legacySubject, _ := emailtext.LegacyDefault(emailtext.NotificationSale)
	if legacySubject == "" {
		t.Fatal("test setup: the sale notification legacy default is missing")
	}
	const merchantBody = "Someone bought something. Pack it and post it. Order {{ref_code}}, RM {{amount}}."

	svc, rec := newObservedService(t)
	svc.Notifications = fixedRules{rules: []*dbstore.FormNotification{{
		ID: "n1", FormID: "form-mix", Event: dbstore.NotifyEventSalePaid,
		ToEmail: "merchant@example.test",
		// The stored row: system default subject, merchant-authored body.
		Subject: legacySubject, Body: merchantBody, Enabled: true,
	}}}

	svc.FireFormNotifications(context.Background(), buyerAxisRef("form-mix"),
		dbstore.NotifyEventSalePaid)

	if len(rec.requests) != 1 {
		t.Fatalf("SES received %d requests, want exactly 1", len(rec.requests))
	}
	sent := rec.requests[0].Content.Simple

	// The half the merchant did NOT touch must have followed the buyer's
	// language — this is the defect: it used to stay Malay. The expectation is
	// the full production chain (dictionary then placeholder substitution).
	wantSubject, _ := expectedMessage(t, emailtext.En, emailtext.NotificationSale,
		buyerAxisRef("form-mix"))
	if sent.Subject.Data != wantSubject {
		t.Errorf("sent subject = %q, want %q (the English default)", sent.Subject.Data, wantSubject)
	}
	if strings.Contains(sent.Subject.Data, "Jualan baharu") {
		t.Errorf("sent subject kept the Malay system default for an English form: %q", sent.Subject.Data)
	}
	// The half he DID write must be untouched — the words he chose, and the
	// placeholder the template engine still substitutes because he left the token
	// in.
	if !strings.Contains(sent.Body.Text.Data, "Someone bought something. Pack it and post it.") {
		t.Errorf("the merchant's own body was rewritten:\n%s", sent.Body.Text.Data)
	}
	if !strings.Contains(sent.Body.Text.Data, "Order JF-MIX-1, RM 25.00.") {
		t.Errorf("the {{ref_code}}/{{amount}} tokens he left in his own body were not substituted:\n%s",
			sent.Body.Text.Data)
	}
	// The message must still be a working notification: the placeholders the
	// template engine owns are substituted, whatever language it is in.
	if strings.Contains(sent.Body.Text.Data, "{{") {
		t.Errorf("sent body still carries an unsubstituted placeholder:\n%s", sent.Body.Text.Data)
	}
}

// TestEditedSubjectKeepsItsOwnWordsWhileTheBodyFollowsTheLanguage is the mirror
// case, and the guard against over-correcting: the merchant's subject must never
// be translated, and the untouched body must still localise.
func TestEditedSubjectKeepsItsOwnWordsWhileTheBodyFollowsTheLanguage(t *testing.T) {
	_, legacyBody := emailtext.LegacyDefault(emailtext.NotificationSale)
	if legacyBody == "" {
		t.Fatal("test setup: the sale notification legacy default is missing")
	}
	const merchantSubject = "New paid order — pack it"

	svc, rec := newObservedService(t)
	svc.Notifications = fixedRules{rules: []*dbstore.FormNotification{{
		ID: "n2", FormID: "form-mix2", Event: dbstore.NotifyEventSalePaid,
		ToEmail: "merchant@example.test",
		Subject: merchantSubject, Body: legacyBody, Enabled: true,
	}}}

	for _, tc := range []struct{ lang string }{
		{emailtext.En},
		{emailtext.Ms},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			rec.requests = nil
			ref := buyerAxisRef("form-mix2")
			ref.FormLang = tc.lang
			svc.FireFormNotifications(context.Background(), ref, dbstore.NotifyEventSalePaid)

			if len(rec.requests) != 1 {
				t.Fatalf("SES received %d requests, want exactly 1", len(rec.requests))
			}
			sent := rec.requests[0].Content.Simple
			// The merchant's subject is not a template, so it is sent verbatim
			// with no substitution — asserted exactly.
			if sent.Subject.Data != merchantSubject {
				t.Errorf("the merchant's subject was rewritten for a %s buyer: %q", tc.lang, sent.Subject.Data)
			}
			// The untouched body is a system template, so it must render in the
			// buyer's language AND still have its placeholders substituted.
			_, wantBody := expectedMessage(t, tc.lang, emailtext.NotificationSale, ref)
			if sent.Body.Text.Data != wantBody {
				t.Errorf("lang %s: untouched body is not the %s template", tc.lang, tc.lang)
			}
			if !strings.Contains(sent.Body.Text.Data, "JF-MIX-1") {
				t.Errorf("lang %s: localised body lost its reference code:\n%s", tc.lang, sent.Body.Text.Data)
			}
			if !strings.Contains(sent.Body.Text.Data, "RM 25.00") {
				t.Errorf("lang %s: localised body lost its amount (integer sen):\n%s", tc.lang, sent.Body.Text.Data)
			}
		})
	}
}

// TestWholeSystemRuleStillLocalises and TestFullyAuthoredRuleIsUntouched are the
// two end points the per-field rule must not move: a never-edited rule still
// translates, and a rule the merchant wrote in full is passed through verbatim.
func TestWholeSystemRuleStillLocalises(t *testing.T) {
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.NotificationSale)

	svc, rec := newObservedService(t)
	svc.Notifications = fixedRules{rules: []*dbstore.FormNotification{{
		ID: "n3", FormID: "form-sys", Event: dbstore.NotifyEventSalePaid,
		ToEmail: "merchant@example.test",
		Subject: legacySubject, Body: legacyBody, Enabled: true,
	}}}

	ref := buyerAxisRef("form-sys")
	ref.FormLang = emailtext.Ar
	svc.FireFormNotifications(context.Background(), ref, dbstore.NotifyEventSalePaid)

	if len(rec.requests) != 1 {
		t.Fatalf("SES received %d requests, want exactly 1", len(rec.requests))
	}
	sent := rec.requests[0].Content.Simple
	wantSubject, wantBody := expectedMessage(t, emailtext.Ar, emailtext.NotificationSale, ref)
	if sent.Subject.Data != wantSubject || sent.Body.Text.Data != wantBody {
		t.Errorf("a never-edited rule did not render in Arabic.\nsubject: %q\nwant:    %q",
			sent.Subject.Data, wantSubject)
	}
	// The branded HTML part must declare the language it is in, so a mail client
	// or screen reader reads it correctly — and Arabic must be right-to-left.
	if !strings.Contains(sent.Body.HTML.Data, `dir="rtl"`) {
		t.Errorf("Arabic notification HTML is not right-to-left")
	}
}

func TestFullyAuthoredRuleIsUntouched(t *testing.T) {
	const subject = "Order in"
	const body = "You have an order. Handle it. Ref {{ref_code}}"

	svc, rec := newObservedService(t)
	svc.Notifications = fixedRules{rules: []*dbstore.FormNotification{{
		ID: "n4", FormID: "form-own", Event: dbstore.NotifyEventSalePaid,
		ToEmail: "merchant@example.test",
		Subject: subject, Body: body, Enabled: true,
	}}}

	svc.FireFormNotifications(context.Background(), buyerAxisRef("form-own"),
		dbstore.NotifyEventSalePaid)

	if len(rec.requests) != 1 {
		t.Fatalf("SES received %d requests, want exactly 1", len(rec.requests))
	}
	sent := rec.requests[0].Content.Simple
	if sent.Subject.Data != subject {
		t.Errorf("a fully-authored subject was rewritten: %q", sent.Subject.Data)
	}
	if !strings.Contains(sent.Body.Text.Data, body[:30]) {
		t.Errorf("a fully-authored body was rewritten:\n%s", sent.Body.Text.Data)
	}
	if strings.Contains(sent.Body.Text.Data, "Thank you") {
		t.Errorf("a fully-authored body was replaced by a built-in template:\n%s", sent.Body.Text.Data)
	}
}

// TestUnwrittenNotificationRuleSendsTheSameBytesAsTheLegacyLiteral is card
// t_1768cb11's safety half, measured at the last point the repo controls: the
// SES request body.
//
// Migration 000084 blanks the Malay literal out of every never-customised rule so
// the dashboard stops presenting the system's text as the merchant's. That is
// only safe if the MAIL does not change, so this test sends the same rule twice in
// each of the five locales — once as the pre-language release stored it (the
// Malay literal) and once as this release stores it (empty) — and compares the
// subject and body byte for byte. A blank half must resolve to exactly the text
// the literal resolved to.
//
// It also pins the other direction in the same run: a rule the merchant DID write
// is sent verbatim in every locale, with no translation and no substitution
// beyond the template engine's own placeholders.
func TestUnwrittenNotificationRuleSendsTheSameBytesAsTheLegacyLiteral(t *testing.T) {
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.NotificationSale)
	if legacySubject == "" || legacyBody == "" {
		t.Fatal("test setup: the sale notification legacy default is missing")
	}

	for _, lang := range emailtext.Langs {
		t.Run(lang, func(t *testing.T) {
			svc, rec := newObservedService(t)
			ref := buyerAxisRef("form-same-bytes")
			ref.FormLang = lang

			// send fires one rule through the real sendNotification path and
			// returns the exact subject/body SES was handed.
			send := func(subject, body string) (sentSubject, sentBody string) {
				t.Helper()
				rec.requests = nil
				svc.Notifications = fixedRules{rules: []*dbstore.FormNotification{{
					ID: "n-same", FormID: ref.FormID, Event: dbstore.NotifyEventSalePaid,
					ToEmail: "merchant@example.test",
					Subject: subject, Body: body, Enabled: true,
				}}}
				svc.FireFormNotifications(context.Background(), ref, dbstore.NotifyEventSalePaid)
				if len(rec.requests) != 1 {
					t.Fatalf("SES received %d requests, want exactly 1", len(rec.requests))
				}
				return rec.requests[0].Content.Simple.Subject.Data,
					rec.requests[0].Content.Simple.Body.Text.Data
			}

			// The never-customised rule, in the two shapes the database can hold.
			emptySubject, emptyBody := send("", "")
			legacySentSubject, legacySentBody := send(legacySubject, legacyBody)

			if emptySubject != legacySentSubject || emptyBody != legacySentBody {
				t.Errorf("lang %s: blanking the stored default CHANGED the email.\n"+
					"empty  subject=%q\nlegacy subject=%q\nempty  body=%q\nlegacy body=%q",
					lang, emptySubject, legacySentSubject, emptyBody, legacySentBody)
			}
			// And the resolved bytes really are this locale's template — otherwise
			// the comparison above could be two identical wrong messages.
			wantSubject, wantBody := expectedMessage(t, lang, emailtext.NotificationSale, ref)
			if emptySubject != wantSubject || emptyBody != wantBody {
				t.Errorf("lang %s: the never-customised rule is not the %s template.\n"+
					"got  subject=%q body=%q\nwant subject=%q body=%q",
					lang, lang, emptySubject, emptyBody, wantSubject, wantBody)
			}
			if lang == emailtext.En && strings.Contains(emptySubject, "Jualan baharu") {
				t.Errorf("lang en: the resolved subject is Malay: %q", emptySubject)
			}

			// The merchant's own words, in the same locale: verbatim, both halves.
			const merchantSubject = "Your order needs packing"
			const merchantBody = "Please pack and post this order today."
			gotSubject, gotBody := send(merchantSubject, merchantBody)
			if gotSubject != merchantSubject {
				t.Errorf("lang %s: a merchant-authored subject was rewritten: %q", lang, gotSubject)
			}
			if gotBody != merchantBody {
				t.Errorf("lang %s: a merchant-authored body was rewritten: %q", lang, gotBody)
			}
		})
	}
}
