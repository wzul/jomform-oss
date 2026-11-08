package mcpsrv

// Card t_56a2bd7f — get_chip_settings published the CHIP secret almost in full.
//
// This tool has its own copy of the mask (maskSecretMCP), a byte-for-byte twin
// of httpapi.maskSecret, and it carried the same defect: it returned
// `s[:len(s)-4] + "••••" + s[len(s)-4:]` — every character except the last
// four. So an MCP caller asking for the account's settings got 84 of an
// 88-character key, while the tool description (tools.go) promises "Secret
// penuh tidak pernah dipulangkan" — the full secret is never returned.
//
// The tool now calls cryptoutil.MaskSecret, the ONE rule the REST doors use.
// This test drives the REAL handler (not the in-memory transport, which does
// not carry the per-call auth scope) over a real account row and compares the
// answer against the stored ciphertext it unseals itself, so a future
// second copy of the mask in this package fails here.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL).

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

func TestGetChipSettingsMasksTheSecretBody(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping MCP get_chip_settings mask test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID, accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('chip-mask-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'chip-mask-' || md5(random()::text), 'chip-mask-' || md5(random()::text))
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	// Prod-shaped: sk_test_ + 76-character body + Kw== = 88 bytes.
	const secretBody = "wXlgutZT0OomZWba7U/vFYO3TezMtkdahoMJyJth8DASzM2RmPa+6PiIXAG3kJFRAneL3boSLpAd76"
	secret := "sk_test_" + secretBody[:76] + "Kw=="
	sealed, err := cryptoutil.Encrypt(key, []byte(secret))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	store := dbstore.NewAccountStore(pool)
	if err := store.SetChipCredentials(ctx, accountID, "br_mcp_mask", sealed, "test", "", nil, true); err != nil {
		t.Fatalf("set chip creds: %v", err)
	}

	// Unseal what is actually stored — the comparison source.
	stored, err := store.ByID(ctx, accountID)
	if err != nil {
		t.Fatalf("read account back: %v", err)
	}
	plaintext, err := cryptoutil.Decrypt(key, stored.ChipSecretKeyEnc)
	if err != nil {
		t.Fatalf("decrypt stored secret: %v", err)
	}
	if string(plaintext) != secret {
		t.Fatalf("stored secret round-trip = %q, want %q", plaintext, secret)
	}

	srv := &Server{deps: &Deps{Accounts: store}, decKey: key}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{AccountID: accountID, Scopes: []string{"readwrite"}})

	_, out, err := srv.getChipSettings(callCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_chip_settings: %v", err)
	}
	if !out.Configured || !out.Verified || out.Mode != "test" || out.BrandID != "br_mcp_mask" {
		t.Fatalf("status fields changed: %+v — the mask fix must not touch configured/verified/mode/brand_id", out)
	}
	if want := "sk_test_••••Kw=="; out.SecretKeyMasked != want {
		t.Fatalf("secret_key_masked = %q, want %q", out.SecretKeyMasked, want)
	}
	interior := string(plaintext)[len("sk_test_") : len(plaintext)-4]
	for i := 0; i+4 <= len(interior); i++ {
		if window := interior[i : i+4]; strings.Contains(out.SecretKeyMasked, window) {
			t.Fatalf("get_chip_settings leaked interior window %q (offset %d of the stored secret) in %q",
				window, i, out.SecretKeyMasked)
		}
	}
}
