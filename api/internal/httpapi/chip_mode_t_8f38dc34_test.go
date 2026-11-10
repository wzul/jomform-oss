package httpapi

// Card t_8f38dc34 — CHIP test/live mode was inferred from a key prefix CHIP
// keys do not have.
//
// The door used to derive the mode from the secret's first characters
// (`sk_live_…` → live, `sk_test_…` → test, anything else → test). CHIP's own
// documentation says the merchant portal issues SEPARATE keys for test and live
// and never publishes a prefix, so a real merchant key matched neither branch:
// every workspace was stored as chip_mode='test' and the settings page showed
// "Test" beside a live key. Measured on prod: 24 test rows, zero live.
//
// This test drives the REAL handler over a real account row with a secret in the
// exact shape a merchant's key has (opaque, no prefix):
//
//   - choosing Live stores live (the reachable-live half of the bug),
//   - choosing Test with an `sk_live_…`-looking secret stores TEST (the prefix
//     must not win over an explicit choice — this is the assertion that fails on
//     the old code),
//   - omitting the mode keeps the stored one (editing the fallback email cannot
//     relabel live credentials as test),
//   - a mode that is neither test nor live is refused with 400 and nothing is
//     written,
//   - the ✓ tick still comes only from the CHIP API check, never from the label.
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

// chipModeProbe is the CHIP credential check at save time. A nil error means
// CHIP accepted the secret, which is what sets the "verified ✓" tick.
type chipModeProbe struct{ err error }

func (p chipModeProbe) ValidateCredentials(context.Context, string) error { return p.err }

func TestChipModeIsChosenNotInferredFromTheSecret(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping CHIP mode test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)
	_, slug, member, accountID := rbacFixture(t, ctx, pool, dbstore.RoleAdmin)
	h := rbacHandlers(t, pool)
	h.ChipProbe = chipModeProbe{}
	tok := rbacSessionCookie(t, h, member)
	path := "/v1/rest/settings/chip?workspace=" + slug

	// stored reads the row back, so every assertion below is about what the
	// database holds — not about what the handler echoed.
	stored := func() (mode string, verified bool) {
		t.Helper()
		acc, err := h.Accounts.ByID(ctx, accountID)
		if err != nil {
			t.Fatalf("read account: %v", err)
		}
		return acc.ChipMode, acc.ChipVerified
	}
	put := func(body map[string]any) (int, map[string]any) {
		t.Helper()
		rec := rbacDo(t, h, "PUT", path, tok, body)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// A merchant's key: opaque, no readable prefix. This is the value that used
	// to fall through every branch and be stored as test.
	const opaqueKey = "opaque-merchant-key-value"

	// 1. Choosing Live stores live. Before the fix this was UNREACHABLE: the
	//    prefix branches always assigned test, and in.Mode was only a fallback
	//    they overwrote.
	code, out := put(map[string]any{"brand_id": "br_mode_1", "secret_key": opaqueKey, "mode": "live"})
	if code != http.StatusOK {
		t.Fatalf("save with mode=live = %d, want 200 (body %v)", code, out)
	}
	if mode, _ := stored(); mode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after choosing live, want %q", mode, dbstore.ChipModeLive)
	}
	if out["mode"] != dbstore.ChipModeLive {
		t.Fatalf("response mode = %v, want %q", out["mode"], dbstore.ChipModeLive)
	}
	// The probe accepted the secret, so this save is verified — the tick is the
	// CHIP check's verdict, not the chosen label.
	if _, verified := stored(); !verified {
		t.Fatal("stored chip_verified = false after a probe that passed")
	}

	// 2. The old inference must be GONE, in the direction that proves it: a
	//    secret that looks like a live key plus an explicit choice of test
	//    stores test. On the old code the prefix branch forced live here.
	code, out = put(map[string]any{"brand_id": "br_mode_1", "secret_key": "sk_live_looking_key", "mode": "test"})
	if code != http.StatusOK {
		t.Fatalf("save with mode=test = %d, want 200 (body %v)", code, out)
	}
	if mode, _ := stored(); mode != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after choosing test with an sk_live-shaped secret, want %q", mode, dbstore.ChipModeTest)
	}

	// 3. Put the account back on live, then save again with NO mode: the stored
	//    mode must survive. A blank secret keeps the key, so this is the shape of
	//    "edit the fallback email only".
	if code, out = put(map[string]any{"brand_id": "br_mode_1", "secret_key": opaqueKey, "mode": "live"}); code != http.StatusOK {
		t.Fatalf("reset to live = %d, want 200 (body %v)", code, out)
	}
	code, out = put(map[string]any{"brand_id": "br_mode_1", "email_fallback": "store@example.test"})
	if code != http.StatusOK {
		t.Fatalf("save without a mode = %d, want 200 (body %v)", code, out)
	}
	if mode, _ := stored(); mode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after a save that omitted the mode, want %q kept", mode, dbstore.ChipModeLive)
	}

	// 4. A mode that is neither test nor live is refused, and the row is left
	//    exactly as it was. Coercing it to test is the silent half of this bug.
	code, out = put(map[string]any{"brand_id": "br_mode_1", "secret_key": opaqueKey, "mode": "production"})
	if code != http.StatusBadRequest {
		t.Fatalf("save with mode=production = %d, want 400 (body %v)", code, out)
	}
	if mode, _ := stored(); mode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after a rejected mode, want %q untouched", mode, dbstore.ChipModeLive)
	}

	// 5. A CHIP check that fails refuses the save and writes nothing — not even
	//    the chosen mode. A chosen mode is a label, never a verification, and a
	//    save that was refused must leave the row exactly as it was.
	beforeMode, beforeVerified := stored()
	h.ChipProbe = chipModeProbe{err: context.DeadlineExceeded}
	code, out = put(map[string]any{"brand_id": "br_mode_1", "secret_key": opaqueKey, "mode": "test"})
	if code != http.StatusBadRequest {
		t.Fatalf("save with a failing CHIP check = %d, want 400 (body %v)", code, out)
	}
	if mode, verified := stored(); mode != beforeMode || verified != beforeVerified {
		t.Fatalf("a refused save changed the row: mode %q→%q verified %v→%v",
			beforeMode, mode, beforeVerified, verified)
	}
}
