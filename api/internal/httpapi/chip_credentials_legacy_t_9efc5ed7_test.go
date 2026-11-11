package httpapi

// Card t_9efc5ed7 — the CHIP Test/Live mode control is gone.
//
// THE DEFECT THIS REPLACES
//   `chip_mode` never chose the API endpoint (ChipAPIBaseUrl does), never blocked
//   anything and changed no payment: the key itself decides what CHIP does. Card
//   t_8f38dc34 had already stopped INFERRING the mode from the secret's prefix
//   (CHIP keys carry no published prefix), but it made the merchant CHOOSE it
//   instead — a question whose answer nobody read. The control is removed; the
//   column stays as write-only legacy.
//
// WHAT THIS PINS, against a real account row:
//
//   1. a save with NO `mode` stores Brand ID + Secret Key and answers 200;
//   2. an older client that still sends `mode: "live"` is NOT rejected (200) and
//      the request's value is NOT stored — the account keeps the mode it had
//      (test when it had none), because nothing reads it;
//   3. a row already holding `live` keeps `live` through a save that says
//      `test`; the caller cannot change the column any more;
//   4. a garbage `mode` ("production") is accepted and ignored, not refused —
//      refusing would break MCP clients written against the old docs;
//   5. POST /settings/chip/test runs the CHIP check without writing: a pasted
//      key is the one tested, an empty body tests the stored key, a failed check
//      answers 400 verified:false, and a server with no probe answers 503.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// recordingChipProbe is the CHIP credential check. It records every secret it
// is asked about, which is how "the stored key was tested" and "the pasted key
// was tested" are told apart.
type recordingChipProbe struct {
	err error
	got []string
}

func (p *recordingChipProbe) ValidateCredentials(_ context.Context, secret string) error {
	p.got = append(p.got, secret)
	return p.err
}

func TestChipCredentialsSaveWithoutAModeAndTestConnection(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping CHIP credential test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, slug, member, accountID := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	h := rbacHandlers(t, pool)
	probe := &recordingChipProbe{}
	h.ChipProbe = probe
	tok := rbacSessionCookie(t, h, member)
	path := "/v1/rest/settings/chip?workspace=" + slug

	stored := func() dbstore.Account {
		t.Helper()
		acc, err := h.Accounts.ByID(ctx, accountID)
		if err != nil {
			t.Fatalf("read account: %v", err)
		}
		return *acc
	}
	put := func(body map[string]any) (int, map[string]any) {
		t.Helper()
		rec := rbacDo(t, h, "PUT", path, tok, body)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	const opaqueKey = "opaque-merchant-key-value"

	// 1. The shipped client sends no mode at all. The save must still work and
	//    must store the credentials; chip_mode falls back to test (the column's
	//    old default), which is all a row that never had one can honestly say.
	code, out := put(map[string]any{"brand_id": "br_no_mode", "secret_key": opaqueKey})
	if code != http.StatusOK {
		t.Fatalf("save without a mode = %d, want 200 (body %v)", code, out)
	}
	acc := stored()
	if acc.ChipBrandID != "br_no_mode" {
		t.Fatalf("stored brand_id = %q, want br_no_mode", acc.ChipBrandID)
	}
	if acc.ChipSecretKeyEnc == "" {
		t.Fatal("stored chip_secret_key_enc is empty — the secret was not saved")
	}
	if acc.ChipMode != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after a save with no mode, want %q", acc.ChipMode, dbstore.ChipModeTest)
	}
	if !acc.ChipVerified {
		t.Fatal("stored chip_verified = false after a probe that passed")
	}

	// 2. An older client still sends mode:"live". It must be ACCEPTED (rejecting
	//    it would break clients that follow the old docs) and it must not write
	//    a column nothing reads.
	code, out = put(map[string]any{"brand_id": "br_no_mode", "secret_key": opaqueKey, "mode": "live"})
	if code != http.StatusOK {
		t.Fatalf("save from an older client sending mode=live = %d, want 200 (body %v)", code, out)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after an older client sent live, want the stored %q kept", acc.ChipMode, dbstore.ChipModeTest)
	}

	// 3. A row that already holds live keeps it. This is the value the column
	//    stores now: whatever the account had — never the request's.
	if _, err := pool.Exec(ctx, `UPDATE accounts SET chip_mode='live' WHERE id=$1`, accountID); err != nil {
		t.Fatalf("seed chip_mode=live: %v", err)
	}
	code, out = put(map[string]any{"brand_id": "br_no_mode", "secret_key": opaqueKey, "mode": "test"})
	if code != http.StatusOK {
		t.Fatalf("save over a live-mode row = %d, want 200 (body %v)", code, out)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after the caller sent test, want %q kept", acc.ChipMode, dbstore.ChipModeLive)
	}

	// 4. Garbage in the ignored field is ignored, not refused.
	code, out = put(map[string]any{"brand_id": "br_no_mode", "secret_key": opaqueKey, "mode": "production"})
	if code != http.StatusOK {
		t.Fatalf("save with mode=production = %d, want 200 (ignored, not refused; body %v)", code, out)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after an ignored mode, want %q untouched", acc.ChipMode, dbstore.ChipModeLive)
	}

	// 5. POST /settings/chip/test: a pasted key is the one tested.
	probe.got = nil
	rec := rbacDo(t, h, "POST", "/v1/rest/settings/chip/test?workspace="+slug, tok, map[string]any{"secret_key": "pasted-key-value"})
	if rec.Code != http.StatusOK {
		t.Fatalf("test with a pasted key = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(probe.got) != 1 || probe.got[0] != "pasted-key-value" {
		t.Fatalf("the CHIP check saw %q, want exactly the pasted key", probe.got)
	}
	// An empty body tests the STORED key — the decrypt path, which is what the
	// button does while the Replace field is closed.
	probe.got = nil
	rec = rbacDo(t, h, "POST", "/v1/rest/settings/chip/test?workspace="+slug, tok, map[string]any{})
	if rec.Code != http.StatusOK {
		t.Fatalf("test with no key = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(probe.got) != 1 || probe.got[0] != opaqueKey {
		t.Fatalf("the stored-key test saw %q, want the stored secret", probe.got)
	}
	// A failed check is a 400 with verified:false and writes nothing.
	before := stored()
	probe.err = context.DeadlineExceeded
	rec = rbacDo(t, h, "POST", "/v1/rest/settings/chip/test?workspace="+slug, tok, map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("test with a failing CHIP check = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	if after := stored(); after.ChipVerified != before.ChipVerified || after.ChipMode != before.ChipMode {
		t.Fatal("the connection test changed the account row — it must never write")
	}
	// No probe configured (a dev server without CHIP): a 503, never a fake 200.
	h.ChipProbe = nil
	rec = rbacDo(t, h, "POST", "/v1/rest/settings/chip/test?workspace="+slug, tok, map[string]any{})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("test with no CHIP probe = %d, want 503", rec.Code)
	}
}
