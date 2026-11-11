package mcpsrv

// Card t_9efc5ed7 — set_chip_settings no longer carries a Test/Live choice.
//
// The MCP door used to own a copy of the mode rule (t_8f38dc34 made both doors
// call dbstore.ResolveChipMode); the control has now been removed from the
// product because the answer changed no payment, so the door stores the value
// the account already holds and IGNORES whatever `mode` an older client sends.
//
// This test drives the REAL handler over a real account row and asserts what the
// database holds:
//
//   1. a save with no mode stores Brand ID + Secret Key;
//   2. `mode: "live"` is accepted (an older client is not broken) and NOT
//      stored — the row keeps its previous value;
//   3. a row already holding live keeps it through a save that says `test`;
//   4. a garbage mode is ignored, not refused;
//   5. test_chip_connection refuses honestly when this server has no CHIP probe
//      (rather than answering ok), and get_chip_settings still reports the row.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"os"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestSetChipSettingsIgnoresTheLegacyMode(t *testing.T) {
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

	stored := func() dbstore.Account {
		t.Helper()
		a, err := store.ByID(ctx, accountID)
		if err != nil {
			t.Fatalf("read account: %v", err)
		}
		return *a
	}
	set := func(in setChipSettingsIn) error {
		t.Helper()
		_, _, err := srv.setChipSettings(callCtx, nil, in)
		return err
	}

	const opaqueKey = "opaque-merchant-key-value"

	// 1. The shipped client sends no mode; the credentials must be stored.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_legacy", SecretKey: opaqueKey}); err != nil {
		t.Fatalf("set_chip_settings without a mode: %v", err)
	}
	acc := stored()
	if acc.ChipBrandID != "br_mcp_legacy" || acc.ChipSecretKeyEnc == "" {
		t.Fatalf("credentials were not stored: brand=%q secret-set=%v", acc.ChipBrandID, acc.ChipSecretKeyEnc != "")
	}
	if acc.ChipMode != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after a save with no mode, want %q", acc.ChipMode, dbstore.ChipModeTest)
	}

	// 2. An older client sends mode:"live" — accepted, and not stored.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_legacy", SecretKey: opaqueKey, Mode: "live"}); err != nil {
		t.Fatalf("set_chip_settings with a legacy mode=live must not be refused: %v", err)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeTest {
		t.Fatalf("stored chip_mode = %q after a legacy client sent live, want %q kept", acc.ChipMode, dbstore.ChipModeTest)
	}

	// 3. A row already holding live keeps it, even when the caller says test.
	if _, err := pool.Exec(ctx, `UPDATE accounts SET chip_mode='live' WHERE id=$1`, accountID); err != nil {
		t.Fatalf("seed chip_mode=live: %v", err)
	}
	if err := set(setChipSettingsIn{BrandID: "br_mcp_legacy", SecretKey: opaqueKey, Mode: "test"}); err != nil {
		t.Fatalf("set_chip_settings over a live-mode row: %v", err)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after the caller sent test, want %q kept", acc.ChipMode, dbstore.ChipModeLive)
	}

	// 4. Garbage is ignored, not an error a client would have to handle.
	if err := set(setChipSettingsIn{BrandID: "br_mcp_legacy", SecretKey: opaqueKey, Mode: "production"}); err != nil {
		t.Fatalf("set_chip_settings with mode=production must be ignored, not refused: %v", err)
	}
	if acc = stored(); acc.ChipMode != dbstore.ChipModeLive {
		t.Fatalf("stored chip_mode = %q after an ignored mode, want %q untouched", acc.ChipMode, dbstore.ChipModeLive)
	}

	// 5. The connection test must not invent a result on a server with no probe.
	if _, _, err := srv.testChipConnection(callCtx, nil, testChipConnectionIn{}); err == nil {
		t.Fatal("test_chip_connection answered ok with no CHIP probe configured")
	}

	// get_chip_settings still reads the row (older integrations keep working).
	_, out, err := srv.getChipSettings(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_chip_settings: %v", err)
	}
	if out.Mode != dbstore.ChipModeLive {
		t.Fatalf("get_chip_settings mode = %q, want %q", out.Mode, dbstore.ChipModeLive)
	}
	if !out.Configured {
		t.Fatal("get_chip_settings reports configured=false after a successful save")
	}
}
