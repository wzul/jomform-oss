package mcpsrv

// Card t_8f38dc34 — set_chip_settings carried its own copy of the CHIP mode
// inference: `sk_live_…` → live, `sk_test_…` → test, anything else → test.
// CHIP issues a separate key per mode and publishes no prefix, so a real key
// was always stored as test. The REST door and this one must agree, so both now
// call dbstore.ResolveChipMode.
//
// This test drives the REAL handler over a real account row (the in-memory
// transport does not carry the per-call auth scope) and asserts what the
// database holds, not what the tool echoed.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestSetChipSettingsModeIsChosenNotInferred(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP set_chip_settings mode test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('chip-mode-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'chip-mode-' || md5(random()::text), 'chip-mode-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 7)
	}
	store := dbstore.NewAccountStore(pool)
	srv := &Server{deps: &Deps{Accounts: store}, decKey: key}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	stored := func() string {
		t.Helper()
		a, err := store.ByID(ctx, accountID)
		if err != nil {
			t.Fatalf("read account: %v", err)
		}
		return a.ChipMode
	}
	set := func(in setChipSettingsIn) error {
		t.Helper()
		_, _, err := srv.setChipSettings(callCtx, nil, in)
		return err
	}

	// A merchant's key: opaque, no prefix. Choosing live must store live; the
	// old code made live unreachable from this door too.
	const opaqueKey = "opaque-merchant-key-value"
	if err := set(setChipSettingsIn{BrandID: "br_mcp_mode", SecretKey: opaqueKey, Mode: "live"}); err != nil {
		t.Fatalf("set_chip_settings mode=live: %v", err)
	}
	if got := stored(); got != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after choosing live, want %q", got, dbstore.ChipModeLive)
	}

	// An omitted mode keeps the stored one, so editing the fallback email does
	// not relabel live credentials as test.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_mode", EmailFallback: "store@example.test"}); err != nil {
		t.Fatalf("set_chip_settings without a mode: %v", err)
	}
	if got := stored(); got != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after a save that omitted the mode, want %q kept", got, dbstore.ChipModeLive)
	}

	// An explicit choice beats the shape of the secret: this is the assertion
	// that fails on the old prefix code.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_mode", SecretKey: "sk_live_looking_key", Mode: "test"}); err != nil {
		t.Fatalf("set_chip_settings mode=test: %v", err)
	}
	if got := stored(); got != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after choosing test with an sk_live-shaped secret, want %q", got, dbstore.ChipModeTest)
	}

	// A mode that is neither test nor live is refused and nothing is written.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_mode", SecretKey: opaqueKey, Mode: "production"}); err == nil {
		t.Fatal("set_chip_settings accepted mode=production, want an error")
	} else if !strings.Contains(err.Error(), "mode must be") {
		t.Fatalf("error for a bad mode = %v, want it to name the rule", err)
	}
	if got := stored(); got != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after a rejected mode, want %q untouched", got, dbstore.ChipModeTest)
	}

	// get_chip_settings reports the stored choice, so the two MCP tools agree.
	_, out, err := srv.getChipSettings(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_chip_settings: %v", err)
	}
	if out.Mode != dbstore.ChipModeTest {
		t.Fatalf("get_chip_settings mode = %q, want %q", out.Mode, dbstore.ChipModeTest)
	}
	if out.Verified {
		t.Fatal("get_chip_settings reports verified without any CHIP API check — the tick must come from the probe, not the label")
	}
}
