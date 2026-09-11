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
	got := ResolveFormEmailSettings(form, ws)

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
	got := ResolveFormEmailSettings(form, ws)

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
	got := ResolveFormEmailSettings(form, ws)

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
	got := ResolveFormEmailSettings(nil, nil)
	if got.ReceiptSubject == "" || got.ReceiptBody == "" {
		t.Errorf("expected built-in defaults, got subject=%q body=%q", got.ReceiptSubject, got.ReceiptBody)
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
	got := ResolveFormEmailSettings(nil, ws)
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
