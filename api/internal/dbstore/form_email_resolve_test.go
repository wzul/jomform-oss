// Issue #453 — per-form email settings override resolution.
//
// A form may override the workspace's email settings (mirroring #107 and the
// #57 language-override pattern). Resolution order is
// per-form override -> workspace setting -> built-in default. These are pure
// unit tests over dbstore.ResolveFormEmailSettings so they need no database.
package dbstore

import (
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// TestResolveFormEmailSettingsInheritsWorkspace asserts that a form with no
// override at all inherits every workspace setting unchanged.
func TestResolveFormEmailSettingsInheritsWorkspace(t *testing.T) {
	ws := &EmailSettings{
		ReceiptSubject: "WS subject", ReceiptBody: "WS body",
		ReceiptEnabled: false, NotifyMerchant: true,
		MerchantNotifyEmail: "store@shop.my", HTML: true,
	}
	form := &FormEmailSettings{FormID: "f1"}
	got := ResolveFormEmailSettings(form, ws, emailtext.En)

	if got.ReceiptSubject != "WS subject" || got.ReceiptBody != "WS body" {
		t.Errorf("subject/body = %q/%q, want workspace values", got.ReceiptSubject, got.ReceiptBody)
	}
	if got.ReceiptEnabled != false {
		t.Errorf("receipt_enabled = %v, want workspace false", got.ReceiptEnabled)
	}
	if got.NotifyMerchant != true {
		t.Errorf("notify_merchant = %v, want workspace true", got.NotifyMerchant)
	}
	if got.MerchantNotifyEmail != "store@shop.my" {
		t.Errorf("merchant email = %q, want workspace value", got.MerchantNotifyEmail)
	}
	if got.HTML != true {
		t.Errorf("html = %v, want workspace true", got.HTML)
	}
}

// TestResolveFormEmailSettingsPerFormWins asserts a per-form override wins
// field-by-field over the workspace setting.
func TestResolveFormEmailSettingsPerFormWins(t *testing.T) {
	ws := &EmailSettings{
		ReceiptSubject: "WS subject", ReceiptBody: "WS body",
		ReceiptEnabled: true, NotifyMerchant: false, HTML: false,
	}
	form := &FormEmailSettings{
		FormID:         "f1",
		ReceiptSubject: "Form subject",
		ReceiptBody:    "Form body",
	}
	got := ResolveFormEmailSettings(form, ws, emailtext.En)

	if got.ReceiptSubject != "Form subject" || got.ReceiptBody != "Form body" {
		t.Errorf("subject/body = %q/%q, want per-form values", got.ReceiptSubject, got.ReceiptBody)
	}
	// Toggles were not overridden → inherit the workspace values.
	if got.ReceiptEnabled != true || got.NotifyMerchant != false {
		t.Errorf("toggles = %v/%v, want inherited workspace values", got.ReceiptEnabled, got.NotifyMerchant)
	}
}

// TestResolveFormEmailSettingsToggleOnlyOverride asserts a form can override
// ONLY a toggle (no subject/body), which the pre-#453 logic could not do.
func TestResolveFormEmailSettingsToggleOnlyOverride(t *testing.T) {
	ws := &EmailSettings{
		ReceiptSubject: "WS subject", ReceiptBody: "WS body",
		ReceiptEnabled: true, NotifyMerchant: false,
	}
	form := &FormEmailSettings{
		FormID:                 "f1",
		ReceiptEnabledOverride: boolPtr(false), // turn buyer receipts OFF for this form
		NotifyMerchantOverride: boolPtr(true),  // but notify the merchant
	}
	got := ResolveFormEmailSettings(form, ws, emailtext.En)

	if got.ReceiptEnabled != false {
		t.Errorf("receipt_enabled = %v, want per-form override false", got.ReceiptEnabled)
	}
	if got.NotifyMerchant != true {
		t.Errorf("notify_merchant = %v, want per-form override true", got.NotifyMerchant)
	}
	// Subject/body still inherit the workspace.
	if got.ReceiptSubject != "WS subject" || got.ReceiptBody != "WS body" {
		t.Errorf("subject/body = %q/%q, want inherited workspace values", got.ReceiptSubject, got.ReceiptBody)
	}
}

// TestResolveFormEmailSettingsDefaults asserts the built-in defaults apply when
// neither a form override nor a workspace setting is present.
func TestResolveFormEmailSettingsDefaults(t *testing.T) {
	got := ResolveFormEmailSettings(nil, nil, emailtext.En)
	if got.ReceiptSubject == "" || got.ReceiptBody == "" {
		t.Errorf("expected built-in defaults, got subject=%q body=%q", got.ReceiptSubject, got.ReceiptBody)
	}
	// An unset/unknown language never silently resolves to the Malay literals
	// the database defaults carry: a never-customised workspace must mail a
	// buyer in English until someone chooses otherwise.
	if strings.Contains(got.ReceiptBody, "Terima kasih") {
		t.Errorf("default body is Malay for lang=%q: %q", emailtext.En, got.ReceiptBody)
	}
	if !strings.Contains(got.ReceiptBody, "Thank you") {
		t.Errorf("default body is not the English template: %q", got.ReceiptBody)
	}
	if !strings.Contains(got.ReceiptBody, "{{ref_code}}") {
		t.Errorf("default body should carry placeholders: %q", got.ReceiptBody)
	}
	if got.ReceiptEnabled != true {
		t.Errorf("default receipt_enabled = %v, want true", got.ReceiptEnabled)
	}
	if got.NotifyMerchant != false {
		t.Errorf("default notify_merchant = %v, want false", got.NotifyMerchant)
	}
}

// TestResolveFormEmailSettingsEmptyWorkspaceFallsToDefault asserts an empty
// workspace subject/body (row exists but unset) still resolves to the built-in
// default rather than sending an empty email.
func TestResolveFormEmailSettingsEmptyWorkspaceFallsToDefault(t *testing.T) {
	ws := &EmailSettings{ReceiptEnabled: true}
	got := ResolveFormEmailSettings(nil, ws, emailtext.En)
	if got.ReceiptSubject == "" || got.ReceiptBody == "" {
		t.Errorf("empty workspace subject/body should fall back to default, got %q/%q", got.ReceiptSubject, got.ReceiptBody)
	}
}

// TestFormEmailSettingsHasOverride asserts HasOverride only reports true when a
// real per-form override is present.
func TestFormEmailSettingsHasOverride(t *testing.T) {
	if (&FormEmailSettings{}).HasOverride() {
		t.Errorf("empty settings should not report an override")
	}
	if !(&FormEmailSettings{ReceiptSubject: "x"}).HasOverride() {
		t.Errorf("subject should report an override")
	}
	if !(&FormEmailSettings{ReceiptEnabledOverride: boolPtr(false)}).HasOverride() {
		t.Errorf("a toggle override should report an override")
	}
}

// TestResolveFormEmailSettingsPartiallyEditedRowFollowsTheLanguage pins the rule
// for a row that is only HALF the system's own text.
//
// A subject and a body are two separate merchant decisions, but the settings
// page submits them together: it loads both stored values into state and always
// posts both back on save. So a merchant who edits ONLY the body stores the
// untouched system subject verbatim, and a merchant who edits only the subject
// stores the untouched system body — both reachable without any hand-crafted
// row. Judged as a pair, neither row is "a system default", so before the
// per-field rule both halves passed through and the Malay literal the
// pre-language release wrote shipped to a buyer whose form is English.
//
// The rule: a half that is empty OR still equals a built-in template is rendered
// in the buyer's language; a half the merchant wrote is passed through verbatim.
// Each case below therefore asserts BOTH halves — that the system's own text
// followed the language, and that the merchant's own text was not rewritten
// under him.
func TestResolveFormEmailSettingsPartiallyEditedRowFollowsTheLanguage(t *testing.T) {
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.Receipt)
	if legacySubject == "" || legacyBody == "" {
		t.Fatal("test setup: the receipt legacy default is missing from the dictionary")
	}
	// The merchant's own wording for each half — chosen to look nothing like a
	// built-in template, so the per-field classifier must leave it alone.
	const merchantSubject = "Your order is confirmed"
	const merchantBody = "Thanks! Keep this note. Ref {{ref_code}}."

	wantEn := emailtext.Text(emailtext.En, emailtext.Receipt, nil)

	cases := []struct {
		name           string
		subject, body  string
		wantSubject    string
		wantBody       string
		wantSubjectNot []string
		wantBodyNot    []string
	}{
		{
			// Case A: the whole row is still the pre-language system literal.
			name:    "whole row is the system literal",
			subject: legacySubject, body: legacyBody,
			wantSubject: wantEn.Subject, wantBody: wantEn.Body,
		},
		{
			// Case B: the reachable UI path — the merchant edited only the body.
			name:    "body edited, subject untouched",
			subject: legacySubject, body: merchantBody,
			wantSubject: wantEn.Subject, wantBody: merchantBody,
			wantBodyNot: []string{"Terima kasih"},
		},
		{
			// Case C: the mirror — the merchant edited only the subject.
			name:    "subject edited, body untouched",
			subject: merchantSubject, body: legacyBody,
			wantSubject: merchantSubject, wantBody: wantEn.Body,
			wantSubjectNot: []string{"Resit pembayaran"},
		},
		{
			// Both halves edited: nothing of the system's remains, so nothing is
			// translated.
			name:    "both halves edited",
			subject: merchantSubject, body: merchantBody,
			wantSubject: merchantSubject, wantBody: merchantBody,
		},
		{
			// A merchant who typed the CURRENT English default back in must not
			// pin the language: the next buyer's language still governs.
			name:    "subject retyped as the English default",
			subject: wantEn.Subject, body: merchantBody,
			wantSubject: wantEn.Subject, wantBody: merchantBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws := &EmailSettings{
				ReceiptSubject: tc.subject, ReceiptBody: tc.body, ReceiptEnabled: true,
			}
			got := ResolveFormEmailSettings(nil, ws, emailtext.En)

			if got.ReceiptSubject != tc.wantSubject {
				t.Errorf("subject = %q, want %q", got.ReceiptSubject, tc.wantSubject)
			}
			if got.ReceiptBody != tc.wantBody {
				t.Errorf("body = %q, want %q", got.ReceiptBody, tc.wantBody)
			}
			for _, bad := range tc.wantSubjectNot {
				if strings.Contains(got.ReceiptSubject, bad) {
					t.Errorf("subject kept the untranslated system text %q: %q", bad, got.ReceiptSubject)
				}
			}
			for _, bad := range tc.wantBodyNot {
				if strings.Contains(got.ReceiptBody, bad) {
					t.Errorf("body kept the untranslated system text %q: %q", bad, got.ReceiptBody)
				}
			}
		})
	}
}

// TestResolveFormEmailSettingsMixedRowFollowsEachBuyerLanguage is the same mixed
// row rendered for the other locales: the rule is per FIELD, so the untouched
// half must change with the buyer's language while the merchant's half does not
// move at all. A fix that merely "kept English" for one language would pass the
// English case above and fail here.
func TestResolveFormEmailSettingsMixedRowFollowsEachBuyerLanguage(t *testing.T) {
	legacySubject, legacyBody := emailtext.LegacyDefault(emailtext.Receipt)
	if legacySubject == "" || legacyBody == "" {
		t.Fatal("test setup: the receipt legacy default is missing from the dictionary")
	}

	// Per-form override: the merchant wrote the subject, the body is inherited
	// and still the system literal.
	form := &FormEmailSettings{FormID: "f1", ReceiptSubject: "My shop's receipt"}

	for _, lang := range emailtext.Langs {
		t.Run(lang, func(t *testing.T) {
			ws := &EmailSettings{ReceiptSubject: legacySubject, ReceiptBody: legacyBody}
			got := ResolveFormEmailSettings(form, ws, lang)

			if got.ReceiptSubject != "My shop's receipt" {
				t.Errorf("lang %s: the merchant's subject was rewritten: %q", lang, got.ReceiptSubject)
			}
			want := emailtext.Text(lang, emailtext.Receipt, nil)
			if got.ReceiptBody != want.Body {
				t.Errorf("lang %s: inherited body was not rendered in the buyer's language", lang)
			}
			if strings.Contains(got.ReceiptBody, "Terima kasih") && lang != emailtext.Ms {
				t.Errorf("lang %s: body kept the Malay literal: %q", lang, got.ReceiptBody)
			}
		})
	}
}
