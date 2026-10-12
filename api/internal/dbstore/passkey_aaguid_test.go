package dbstore

// t_b983de8c — a WebAuthn AAGUID is 16 raw octets, and the passkey row has to
// hold those octets exactly.
//
// THE DEFECT THIS PINS. `passkeys.aaguid` was declared TEXT (000013) and
// PasskeyStore.Save wrote the AAGUID with `string(c.Authenticator.AAGUID)` —
// the 16 raw bytes reinterpreted as a Go string and sent as a text parameter.
// Postgres validates text as UTF-8 and refuses anything else (SQLSTATE 22021,
// `invalid byte sequence for encoding "UTF8"`), so the INSERT failed for every
// real authenticator AFTER the browser ceremony had already succeeded:
//
//   - Apple Passwords / iCloud Keychain (fbfc3007-…): first byte 0xfb.
//   - Google Password Manager (ea9b8d66-…): contains a lone continuation byte
//     0x8c.
//   - The all-zero AAGUID (00000000-…), the spec's "no AAGUID reported" value:
//     0x00, which a text column cannot hold at all.
//
// Only an all-ASCII AAGUID — a Chromium virtual authenticator's
// 0102030405060708-… — could ever be stored, which is exactly why a harness
// driving a virtual authenticator measured 200 while a real iPhone or Android
// passkey was refused. The asymmetry is the whole reason the defect hid, so the
// virtual value is kept here as the CONTROL arm.
//
// WHAT IS ASSERTED, and why each part is needed:
//
//   - The guard goes through the two functions the feature uses, not through
//     SQL: PasskeyStore.Save persists each shape and PasskeyStore.ByUser reads
//     it back, and the 16 bytes must come back byte-equal. Save and ByUser are
//     two halves of one round trip, so asserting only one would let the other
//     drift.
//   - Each fixture's premise is asserted, not assumed: a shape counts as
//     discriminating only if a UTF-8-validating text column could NOT have held
//     it (invalid UTF-8, or a NUL byte), and the fixture set must contain both
//     shapes — otherwise this test could pass on a column that stores text.
//   - The schema half is pinned too: the column type must be BYTEA. Writing the
//     bytes is not enough on its own — on a TEXT column the same call is
//     refused — and the write half and the schema half are one fix.
//   - The no-attestation case stays a real row: a credential that attested no
//     AAGUID stores an empty byte string, never NULL, which is what the column's
//     NOT NULL DEFAULT '\x' says and what the string-typed write this replaced
//     happened to do.
//
// MEASURED BITE. Against the pre-fix revision (a31284dc, before migration
// 000075 — TEXT column, string-typed write) the first fixture dies in Save with
// SQLSTATE 22021. Against the current tree all four shapes round-trip
// byte-equal. Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/webauthn"
)

// passkeyAAGUIDFixtures are the AAGUIDs a real deployment meets, written in the
// UUID spelling the FIDO provider tables publish.
var passkeyAAGUIDFixtures = []struct {
	name string
	uuid string
}{
	{"apple_passwords_icloud_keychain", "fbfc3007-154e-4ecc-8c0b-6e020557d7bd"},
	{"google_password_manager", "ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4"},
	{"all_zero_none_attestation", "00000000-0000-0000-0000-000000000000"},
	{"chromium_virtual_authenticator", "01020304-0506-0708-0102-030405060708"},
}

// TestPasskeyAAGUIDRoundTripsThroughSaveAndByUser is the guard described above.
func TestPasskeyAAGUIDRoundTripsThroughSaveAndByUser(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	user := testOwnerUser(t, ctx, pool)
	store := NewPasskeyStore(pool)

	// The schema half of the fix. `text` here is the pre-fix declaration, and a
	// text column is what made Save refuse the bytes below.
	var dataType string
	if err := pool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'passkeys'
		   AND column_name = 'aaguid'`).Scan(&dataType); err != nil {
		t.Fatalf("read the aaguid column type: %v", err)
	}
	if dataType != "bytea" {
		t.Errorf("passkeys.aaguid is %s, want bytea — an AAGUID is 16 arbitrary bytes, and a "+
			"UTF-8-validating text column refuses every real authenticator's value (SQLSTATE 22021)", dataType)
	}

	discriminating, storable := 0, 0
	for _, f := range passkeyAAGUIDFixtures {
		raw, err := hex.DecodeString(strings.ReplaceAll(f.uuid, "-", ""))
		if err != nil {
			t.Fatalf("%s: bad fixture %q: %v", f.name, f.uuid, err)
		}
		if len(raw) != 16 {
			t.Fatalf("%s: fixture is %d bytes, want 16", f.name, len(raw))
		}

		// The premise. `utf8.Valid` is the check Postgres runs on a text value,
		// and a NUL is rejected by text even though it is a valid code point —
		// that is the all-zero AAGUID's arm.
		textCouldHoldIt := utf8.Valid(raw) && bytes.IndexByte(raw, 0) < 0
		if textCouldHoldIt {
			storable++
		} else {
			discriminating++
		}

		credID := []byte("aaguid-guard-" + f.name)
		if err := store.Save(ctx, user, webauthn.Credential{
			ID:        credID,
			PublicKey: []byte{0x01, 0x02, 0x03},
			Authenticator: webauthn.Authenticator{
				SignCount: 7,
				AAGUID:    raw,
			},
		}); err != nil {
			t.Fatalf("%s (%s): Save refused the credential: %v — this is the failure a real "+
				"registration meets after the ceremony has already succeeded", f.name, f.uuid, err)
		}

		creds, err := store.ByUser(ctx, user)
		if err != nil {
			t.Fatalf("%s: ByUser: %v", f.name, err)
		}
		var got []byte
		found := false
		for i := range creds {
			if bytes.Equal(creds[i].ID, credID) {
				got, found = creds[i].Authenticator.AAGUID, true
				break
			}
		}
		if !found {
			t.Fatalf("%s: the credential Save just stored is not in ByUser's result (%d row(s)) — "+
				"Save and ByUser are one round trip", f.name, len(creds))
		}
		if !bytes.Equal(got, raw) {
			t.Errorf("%s (%s): ByUser returned AAGUID % x, want % x — the authenticator's identity "+
				"has to survive the round trip byte for byte", f.name, f.uuid, got, raw)
		}

		// And the bytes are in the row, not just in memory.
		var stored []byte
		if err := pool.QueryRow(ctx, `SELECT aaguid FROM passkeys WHERE id=$1`, credID).Scan(&stored); err != nil {
			t.Fatalf("%s: read the stored aaguid: %v", f.name, err)
		}
		if !bytes.Equal(stored, raw) {
			t.Errorf("%s (%s): the row holds % x, want % x", f.name, f.uuid, stored, raw)
		}
	}

	// Anti-vacuity: the guard only means something while both shapes are present.
	// Drop the >= 0x80 / NUL shapes and it is a test of a text column; drop the
	// all-ASCII shape and it stops documenting the asymmetry that hid the defect.
	if discriminating < 2 {
		t.Fatalf("premise broken: only %d fixture(s) are values a text column could not hold; this "+
			"guard would pass on the pre-fix schema", discriminating)
	}
	if storable < 1 {
		t.Fatalf("premise broken: no all-ASCII fixture, so the guard no longer covers the " +
			"virtual-authenticator value that stored fine while every real authenticator was refused")
	}

	// The no-attestation credential: a real row, an empty byte string, never NULL.
	nilID := []byte("aaguid-guard-no-aaguid")
	if err := store.Save(ctx, user, webauthn.Credential{
		ID:            nilID,
		PublicKey:     []byte{0x04, 0x05},
		Authenticator: webauthn.Authenticator{SignCount: 1},
	}); err != nil {
		t.Fatalf("Save of a credential that attested no AAGUID: %v", err)
	}
	var isNull bool
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT aaguid IS NULL, aaguid FROM passkeys WHERE id=$1`, nilID).
		Scan(&isNull, &stored); err != nil {
		t.Fatalf("read the stored no-AAGUID value: %v", err)
	}
	if isNull {
		t.Errorf("a credential with no AAGUID stored NULL, want the empty byte string — the column " +
			"is NOT NULL and ByUser scans this column on every login")
	}
	if len(stored) != 0 {
		t.Errorf("a credential with no AAGUID stored % x, want empty", stored)
	}
}
