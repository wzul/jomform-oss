// Wiring guard for the derived stores' key namespace (card t_6004c826).
//
// This is the package the leak was measured in: a full `go test ./internal/httpapi`
// run left 12 jf:magic:, 3 jf:2fa_pending:, 1 jf:totp_pending: and 2
// jf:stepup: keys in PRODUCTION layout on the shared test Valkey, because the
// stores Sessions hands out ignored its namespace. auth's own tests pin the
// mechanism; this one pins the wiring every fixture in this package uses —
// authTestValkey (testSessionOptions) — so a store built without the namespace
// cannot quietly reintroduce the leak here.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

// derivedNamespaceKeyHash is the SHA-256 hex a magic-link, step-up or
// reset-token key is suffixed with. The auth package pins that layout in
// TestDerivedStoreKeyLayoutIsUnchangedInProduction; here it is only used to
// name the key a fixture just minted, without scanning the workspace.
func derivedNamespaceKeyHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TestHttpapiDerivedStoresWriteUnderTheProcessNamespace drives every derived
// store through the package fixture and asserts where each key landed: under
// THIS process's namespace, never in the production layout the exit sweep
// cannot reach.
func TestHttpapiDerivedStoresWriteUnderTheProcessNamespace(t *testing.T) {
	ctx := context.Background()
	sess := authTestValkey(t) // skips when JOMFORM_TEST_VALKEY_URL is unset
	ns := testSessionKeyNamespace

	uid := fmt.Sprintf("derived-ns-probe-%d", os.Getpid())
	stepToken := fmt.Sprintf("derived-stepup-%d", os.Getpid())
	const email = "derived-ns-probe@example.test"
	ip := testIP(t)

	if err := sess.PendingTOTP().Set(ctx, uid, "seed"); err != nil {
		t.Fatalf("pending totp: %v", err)
	}
	if err := sess.StepUp().Grant(ctx, stepToken, uid); err != nil {
		t.Fatalf("step-up: %v", err)
	}
	resetToken, err := sess.PasswordReset().Generate(ctx, uid)
	if err != nil {
		t.Fatalf("password reset: %v", err)
	}
	magicToken, err := sess.MagicLink().Generate(ctx, uid)
	if err != nil {
		t.Fatalf("magic link: %v", err)
	}
	if err := sess.Email2FA().Store(ctx, uid, "123456"); err != nil {
		t.Fatalf("email code: %v", err)
	}
	if err := sess.Email2FA().ArmPending(ctx, ip, email); err != nil {
		t.Fatalf("email pending: %v", err)
	}

	type family struct {
		name       string
		namespaced string
		production string
	}
	families := []family{
		{"totp_pending", "jf:totp_pending:" + ns + ":" + uid, "jf:totp_pending:" + uid},
		{"stepup", "jf:stepup:" + ns + ":" + derivedNamespaceKeyHash(stepToken), "jf:stepup:" + derivedNamespaceKeyHash(stepToken)},
		{"pwreset", "jf:pwreset:" + ns + ":" + derivedNamespaceKeyHash(resetToken), "jf:pwreset:" + derivedNamespaceKeyHash(resetToken)},
		{"magic", "jf:magic:" + ns + ":" + derivedNamespaceKeyHash(magicToken), "jf:magic:" + derivedNamespaceKeyHash(magicToken)},
		{"2faemail", "jf:2faemail:" + ns + ":" + uid, "jf:2faemail:" + uid},
		{"2fa_pending", "jf:2fa_pending:" + ns + ":" + ip + ":" + email, "jf:2fa_pending:" + ip + ":" + email},
	}

	for _, f := range families {
		if n, err := sess.Client().Exists(ctx, f.namespaced).Result(); err != nil || n != 1 {
			t.Errorf("%s key %q is missing (exists=%d err=%v) — the fixture's store did not write under the process namespace %q",
				f.name, f.namespaced, n, err, ns)
		}
		if n, err := sess.Client().Exists(ctx, f.production).Result(); err != nil || n != 0 {
			t.Errorf("%s wrote the PRODUCTION-layout key %q (exists=%d err=%v) — no exit sweep can see it (t_6004c826)",
				f.name, f.production, n, err)
		}
	}

	// The keys are under this process's namespace, so TestMain reclaims them;
	// deleting here too keeps a FAILING run from leaving them for the TTL.
	t.Cleanup(func() {
		keys := make([]string, 0, len(families))
		for _, f := range families {
			keys = append(keys, f.namespaced)
		}
		_ = sess.Client().Del(context.Background(), keys...).Err()
	})
}
