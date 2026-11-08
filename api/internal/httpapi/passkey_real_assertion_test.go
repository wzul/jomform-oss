package httpapi

// t_0a2d413c — a passkey could be REGISTERED and then never used to SIGN IN.
//
// THE MECHANISM, measured on prod (v1.21.44): register/start and register/finish
// answered 200 and the row landed in `passkeys` with a real provider's AAGUID
// (Bitwarden's — see nonASCIIProdAAGUID below), but every login/finish answered
// 401 "passkey tidak sah". The reason was
// not in the API log at all, because the verifier's error was discarded.
//
// go-webauthn's login validation compares the STORED credential's
// BackupEligible bit with the one in the assertion's authenticator data and
// rejects the ceremony when they disagree (webauthn/login.go, validateLogin:
// "Backup Eligible flag inconsistency detected during login validation"). The
// store hard-coded `flags = 0` on every write and dropped the column on the way
// back in, so the stored side was always false — and every SYNCED passkey
// (iCloud Keychain / Touch ID, Google Password Manager, Windows Hello) reports
// BE=1, which is the entire point of a passkey that works across devices. So the
// defect rejected exactly the credentials the feature exists for.
//
// It survived a green suite because the only login coverage was a hand-built
// assertion that set UP and nothing else (auth_passkey_legacy_workspace_
// test.go:assertionBody, whose comment even stated that BE was "left clear to
// match the credential row the store reads back"). A synthetic assertion that
// agrees with the broken store proves the verifier, not the feature. That is the
// same class as the sibling passkey defect (card t_df9233f6), where the
// hand-built payload that agreed with a broken parser hid a 100%-failing
// endpoint.
//
// WHAT IS ASSERTED HERE, and why each arm is needed:
//
//   - TestPasskeyLoginAcceptsARealAssertion runs the WHOLE ceremony in the shape
//     a real authenticator produces: register through the real endpoints with
//     UP|UV|AT|BE|BS and the prod AAGUID, then sign in with a real-shaped
//     assertion (UP|UV|BE|BS, non-empty userHandle). It asserts the stored flag
//     octet, that the stored AAGUID is the 16 raw bytes the authenticator
//     attested (read straight out of the column), that the stored flags are
//     restored by the store, that login/finish answers 200, and that the
//     session it minted is live. Every one of those
//     fails against the pre-fix store: BE never reaches the row, ByUser returns
//     the zero value, and the login is refused with the 401 the customer saw.
//
//   - TestPasskeyLoginRepairsACredentialRegisteredBeforeFlagsWereStored covers
//     the rows that already exist: before the fix EVERY row held the sentinel 0,
//     so a repair that only fixed new registrations would leave every user who
//     had already registered a passkey locked out exactly as before. The row is
//     written the way the old code wrote it (through the store, with no flags),
//     the real-shaped assertion must be accepted, and the adopted octet must be
//     persisted so the credential is recorded from then on.
//
//   - TestPasskeyLoginStillRejectsAChangedBackupEligibleFlag is the guard that
//     the repair above did not WEAKEN the check into "always adopt what the
//     assertion says": for a credential whose row records BackupEligible=1, an
//     assertion claiming BE=0 must still be refused. It also pins the read half
//     on its own — if ByUser stopped restoring flags, the stored value would
//     read as the sentinel, the assertion would be adopted, and this arm would
//     answer 200 where it demands 401.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// The authenticator-data flag octets a REAL synced passkey reports.
//
// This is the shape the suite never exercised. iCloud Keychain / Touch ID,
// Google Password Manager and Windows Hello all mint client-side discoverable
// credentials that are backed up and synced, so their authenticator data sets
// BE (0x08) and BS (0x10) as well as UP; a biometric ceremony also sets UV
// (0x04). Registration additionally sets AT (0x40) because it carries attested
// credential data. BE is the bit the login ceremony compares against the stored
// credential, and it is the one the old store could not record — see
// dbstore/passkey.go's flags section.
const (
	realPasskeyRegistrationFlags = protocol.FlagUserPresent |
		protocol.FlagUserVerified |
		protocol.FlagAttestedCredentialData |
		protocol.FlagBackupEligible |
		protocol.FlagBackupState // 0x5d
	realPasskeyAssertionFlags = protocol.FlagUserPresent |
		protocol.FlagUserVerified |
		protocol.FlagBackupEligible |
		protocol.FlagBackupState // 0x1d
)

// nonASCIIProdAAGUID is the AAGUID the production row carried
// (d548826e79b4db40a3d811116f7e8349, 16 bytes, read back from `passkeys` on
// prod). It is named for the property this test depends on — the bytes are
// non-ASCII, which is what made the pre-fix TEXT column reject the INSERT (card
// t_df9233f6, migration 000075) — and not for a vendor, because the vendor this
// constant used to be named after was wrong: it is Bitwarden's passkey-provider
// AAGUID, not Apple's (and not an iCloud Keychain value):
//
//   - Bitwarden's client declares it for its own provider —
//     `export const AAGUID = new Uint8Array([0xd5, 0x48, …])` in
//     libs/common/src/platform/services/fido2/fido2-authenticator.service.ts
//     (bitwarden/clients), and the same value in the desktop plugin
//     authenticator's authenticator-info blob.
//   - The community AAGUID list maps d548826e-79b4-db40-a3d8-11116f7e8349 to
//     "Bitwarden" (github.com/passkeydeveloper/passkey-authenticator-aaguids,
//     aaguid.json). Apple's iCloud Keychain entries there are the separate
//     fbfc3007-154e-4ecc-8c0b-6e020557d7bd ("Apple Passwords") and
//     dd4ec289-e01d-41c9-bb89-70fa845d4bf2 ("iCloud Keychain (Managed)") —
//     different values, so this constant must not be read as an attribution.
//
// Using it here keeps the registration half of this test on the shape prod
// actually stores.
var nonASCIIProdAAGUID = []byte{
	0xd5, 0x48, 0x82, 0x6e, 0x79, 0xb4, 0xdb, 0x40,
	0xa3, 0xd8, 0x11, 0x11, 0x6f, 0x7e, 0x83, 0x49,
}

// realPasskeyCredID builds a per-run credential id. The stored rows are keyed by
// credential id and `-test.count` re-runs in the same process against the same
// database, so a fixed id would collide on pass 2 and every arm would read arm
// 1's row (see seamCredIDForRun, which does the same for the alphabet arms).
func realPasskeyCredID(prefix string) []byte {
	return []byte(prefix + "-" + runKey())
}

// passkeyPasswordSession signs the fixture user in with his password through the
// real /v1/auth/login endpoint and returns the session cookie. Registration is
// an authed endpoint (issue #458 step-up included), so the passkey ceremony has
// to start from a genuine session rather than a hand-minted one.
func passkeyPasswordSession(t *testing.T, ah *AuthHandlers, pool *pgxpool.Pool, user string) string {
	t.Helper()
	mux := http.NewServeMux()
	ah.RegisterRoutes(mux)
	pw := postJSON(mux, "/v1/auth/login",
		map[string]any{"email": emailOfUser(t, pool, user), "password": "password123"}, uniqueTestIP())
	session := sessionTokenValue(pw)
	if session == "" {
		t.Fatalf("no session cookie on the password login (status %d body %s)", pw.Code, pw.Body.String())
	}
	return session
}

// registerRealPasskey drives the REAL /register/start + /register/finish
// endpoints for user with the given authenticator shape and returns the fixture
// (the software authenticator, whose ES256 key pair the later assertion is
// signed with).
func registerRealPasskey(t *testing.T, h *RESTHandlers, ah *AuthHandlers, pool *pgxpool.Pool,
	user, session string, flags protocol.AuthenticatorFlags, credID, aaguid []byte) passkeyRegistrationFixture {
	t.Helper()

	fx := newPasskeyRegistrationFixture(t, h, ah, pool, user, session, credID)
	fx.flags = flags
	fx.aaguid = aaguid
	body := fx.attestationBody(t, base64.RawURLEncoding)

	req := httptest.NewRequest(http.MethodPost,
		"http://"+passkeyOriginHost+"/v1/auth/passkeys/register/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	setTestClientAddr(req, uniqueTestIP())
	rec := httptest.NewRecorder()
	h.passkeyRegisterFinish(rec, req, &AccountCtx{Account: accountForUser(t, h, user)})
	if rec.Code != http.StatusOK {
		t.Fatalf("register/finish of a real-shaped attestation answered %d (%s)", rec.Code, rec.Body.String())
	}
	return fx
}

// storedPasskeyFlags reads the raw `flags` octet back out of the column the
// login ceremony reads.
func storedPasskeyFlags(t *testing.T, pool *pgxpool.Pool, credID []byte) int {
	t.Helper()
	var flags int
	if err := pool.QueryRow(context.Background(),
		`SELECT flags FROM passkeys WHERE id=$1`, credID).Scan(&flags); err != nil {
		t.Fatalf("read the stored flags octet: %v", err)
	}
	return flags
}

// storedPasskeyAAGUID reads the raw `aaguid` bytes back out of the row, the way
// storedPasskeyFlags reads the flags octet.
//
// It reads the COLUMN and not what PasskeyStore.ByUser returns, because those
// are two different code paths and the defect class this file guards lives
// between them: the store used to hard-code the value it WROTE and drop the
// column on the way back IN, so the ceremony compared the assertion against
// something that had never been in the row. Asserting only the read path would
// leave the write path's representation pinned by nothing but agreement between
// the two halves.
func storedPasskeyAAGUID(t *testing.T, pool *pgxpool.Pool, credID []byte) []byte {
	t.Helper()
	var aaguid []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT aaguid FROM passkeys WHERE id=$1`, credID).Scan(&aaguid); err != nil {
		t.Fatalf("read the stored AAGUID: %v", err)
	}
	return aaguid
}

// TestPasskeyLoginAcceptsARealAssertion is the regression guard for the card: a
// passkey registered in the shape a real authenticator attests must sign in.
//
// Neutralise the fix — write `0` in PasskeyStore.Save, or drop the Flags
// assignment in PasskeyStore.ByUser — and this test goes red at the stored-flag
// assertion or at the 401 from login/finish, while every pre-existing passkey
// test stays green, because each of those signs an assertion that agrees with
// the broken store.
//
// The AAGUID arm is the third half of the same row: byte-reverse the value
// PasskeyStore.Save writes — a pure value change, so the bytes stay bytea-legal
// and the signature is untouched — and this test goes red at the stored-AAGUID
// assertion below, before it ever reaches the ceremony. The flags and the
// ceremony are unaffected by that mutation; the row is what a later ceremony,
// or any other reader of `passkeys.aaguid`, gets.
func TestPasskeyLoginAcceptsARealAssertion(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	h := passkeyHandlers(t, pool)
	h.Events = dbstore.NewEventStore(pool)

	session := passkeyPasswordSession(t, ah, pool, user)

	// ── the registration half ───────────────────────────────────────────────
	fx := registerRealPasskey(t, h, ah, pool, user, session,
		realPasskeyRegistrationFlags, realPasskeyCredID("real-assert"), nonASCIIProdAAGUID)

	// The flags must reach the COLUMN, not merely the in-memory credential: the
	// login ceremony reads them back out of Postgres through a different
	// function (PasskeyStore.ByUser) than the one that wrote them.
	if got := storedPasskeyFlags(t, pool, fx.authn.id); got != int(realPasskeyRegistrationFlags) {
		t.Fatalf("the stored flags octet is %#02x, want %#02x (UP|UV|AT|BE|BS) — the credential's flags "+
			"never reached the row, so the login ceremony compares the assertion's BackupEligible bit "+
			"against a value that was never recorded and refuses every synced passkey", got,
			byte(realPasskeyRegistrationFlags))
	}

	// The other 16 bytes of the same row. The AAGUID is what tells a relying
	// party WHICH authenticator attested the credential, and the column holds it
	// as the raw bytes it is: the fixture's are non-ASCII on purpose, so a write
	// path that reinterpreted them as text either fails the INSERT
	// (SQLSTATE 22021) or stores a re-spelled value — and neither of those
	// shows up in the flags above, nor in the login ceremony, which never reads
	// this column at all (only the row does).
	//
	// TestPasskeyRegistrationAcceptsStandardBase64 asserts the same bytes
	// through PasskeyStore.ByUser, i.e. what the store HANDS the ceremony. This
	// arm pins what the row HOLDS, so the ceremony guard pins the
	// representation it stores without depending on a sibling test for it.
	if got := storedPasskeyAAGUID(t, pool, fx.authn.id); !bytes.Equal(got, fx.aaguid) {
		t.Fatalf("the stored AAGUID is %x, want %x — the 16 raw AAGUID bytes the authenticator "+
			"attested did not reach the row (or reached it re-spelled), so nothing that reads "+
			"`passkeys.aaguid` gets what the credential was created with", got, fx.aaguid)
	}

	// And they must come back out, in the bits the library reads.
	creds, err := h.Passkeys.ByUser(ctx, user)
	if err != nil {
		t.Fatalf("read back credentials: %v", err)
	}
	var stored *webauthn.Credential
	for i := range creds {
		if bytes.Equal(creds[i].ID, fx.authn.id) {
			stored = &creds[i]
			break
		}
	}
	if stored == nil {
		t.Fatalf("the registered credential is not in ByUser's result (%d row(s))", len(creds))
	}
	if !stored.Flags.BackupEligible || !stored.Flags.BackupState || !stored.Flags.UserVerified {
		t.Fatalf("ByUser restored flags %+v, want backupEligible/backupState/userVerified all true — the "+
			"credential row the login verifies against must carry what the authenticator attested",
			stored.Flags)
	}

	// ── the sign-in half, in the shape a real authenticator asserts ──────────
	key, challenge, rpID := startPasskeyLogin(t, h, uniqueTestIP())
	body := fx.authn.assertionBodyWithFlags(t, key, rpID, "https://"+passkeyOriginHost, challenge,
		[]byte(user), realPasskeyAssertionFlags)
	rec := finishPasskeyLogin(t, h, body, uniqueTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("signing in with a real-shaped assertion answered %d (%s) — a synced passkey (iCloud "+
			"Keychain / Touch ID, Google Password Manager, Windows Hello) asserts UP|UV|BE|BS, and the "+
			"ceremony must accept it; this is the exact 401 a customer meets as 'Invalid passkey.'",
			rec.Code, rec.Body.String())
	}
	token := sessionCookieFrom(t, rec)

	// The session it minted must be live, or "landed signed in" is not proven.
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	listReq := httptest.NewRequest(http.MethodGet, "http://"+passkeyOriginHost+"/v1/workspaces", nil)
	listReq.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, listReq)
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /v1/workspaces with the passkey session = %d (body %s) — the login answered 200 but "+
			"the session it handed out is not usable", recList.Code, recList.Body.String())
	}

	// The assertion's state is written back, so the NEXT ceremony's
	// BackupEligible comparison is made against a recorded value again.
	if got := storedPasskeyFlags(t, pool, fx.authn.id); got&int(protocol.FlagBackupEligible) == 0 {
		t.Fatalf("after a successful login the stored flags octet is %#02x, with no BackupEligible bit — "+
			"the state go-webauthn requires be written back (its storage contract) was dropped", got)
	}
}

// TestPasskeyLoginRepairsACredentialRegisteredBeforeFlagsWereStored covers the
// rows that already exist in `passkeys`.
//
// Before this fix EVERY row held the sentinel 0 (PasskeyStore.Save hard-coded
// it), including the credential measured on prod (whose AAGUID is Bitwarden's —
// see nonASCIIProdAAGUID). Fixing only
// the write path would leave every user who had already registered a passkey
// locked out, with the same 401 and the same missing reason in the log — so the
// login path adopts the assertion's flags for a row that holds no recorded
// value, and persists them.
func TestPasskeyLoginRepairsACredentialRegisteredBeforeFlagsWereStored(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	// EXACTLY the row the pre-fix store wrote: no flags at all, for a credential
	// that is in reality a synced one.
	authn, cose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, user, webauthn.Credential{
		ID:        authn.id,
		PublicKey: cose,
	}); err != nil {
		t.Fatalf("store the legacy-shaped passkey: %v", err)
	}
	// The premise of this test is the sentinel; if the store stops writing it for
	// a credential with no flags, this arm stops testing the repair and must be
	// revisited rather than pass vacuously.
	if got := storedPasskeyFlags(t, pool, authn.id); got != 0 {
		t.Fatalf("the fixture row's flags octet is %#02x, want the legacy sentinel 0 — this test no longer "+
			"reproduces a row written before flags were persisted", got)
	}

	h := passkeyHandlers(t, pool)
	key, challenge, rpID := startPasskeyLogin(t, h, uniqueTestIP())
	body := authn.assertionBodyWithFlags(t, key, rpID, "https://"+passkeyOriginHost, challenge,
		[]byte(user), realPasskeyAssertionFlags)
	rec := finishPasskeyLogin(t, h, body, uniqueTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("a passkey registered before flags were persisted answered %d (%s) — such a row holds the "+
			"sentinel 0, which is not a value a real registration can report, so the assertion's flags are "+
			"adopted rather than compared against it; without that repair every already-registered synced "+
			"passkey stays unusable with no way back but deleting and registering again",
			rec.Code, rec.Body.String())
	}

	// The repair must be STICKY: once the adopted octet is stored, the row is a
	// recorded one and the strict consistency check applies to it again.
	if got := storedPasskeyFlags(t, pool, authn.id); got&int(protocol.FlagBackupEligible) == 0 {
		t.Fatalf("the repaired row still holds flags %#02x with no BackupEligible bit — the adopted state "+
			"was not written back, so the next ceremony would depend on the repair again", got)
	}
}

// TestPasskeyLoginStillRejectsAChangedBackupEligibleFlag is the counterweight to
// the repair above: the BackupEligible consistency check must still REFUSE an
// assertion that disagrees with a RECORDED value.
//
// The repair keys on the sentinel 0, which a real registration can never store,
// so it cannot be used to make the server accept whatever an authenticator
// claims. This arm pins that, and it pins the read half by itself: were
// PasskeyStore.ByUser to stop restoring flags, the stored value would read as
// the sentinel, the assertion would be adopted, and this test would answer 200
// where it demands 401.
func TestPasskeyLoginStillRejectsAChangedBackupEligibleFlag(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	// A credential whose row records BackupEligible=1 — the state a synced
	// passkey registered after this fix carries.
	authn, cose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, user, webauthn.Credential{
		ID:        authn.id,
		PublicKey: cose,
		Flags: webauthn.NewCredentialFlags(protocol.FlagUserPresent |
			protocol.FlagAttestedCredentialData | protocol.FlagBackupEligible),
	}); err != nil {
		t.Fatalf("store a backup-eligible passkey: %v", err)
	}
	if got := storedPasskeyFlags(t, pool, authn.id); got&int(protocol.FlagBackupEligible) == 0 {
		t.Fatalf("the fixture row does not record BackupEligible (flags %#02x) — the arm would prove nothing", got)
	}

	h := passkeyHandlers(t, pool)
	key, challenge, rpID := startPasskeyLogin(t, h, uniqueTestIP())
	// The assertion claims the credential is NOT backup eligible: a change the
	// specification says can never happen for one credential.
	body := authn.assertionBodyWithFlags(t, key, rpID, "https://"+passkeyOriginHost, challenge,
		[]byte(user), protocol.FlagUserPresent|protocol.FlagUserVerified)
	rec := finishPasskeyLogin(t, h, body, uniqueTestIP())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an assertion whose BackupEligible bit contradicts the recorded credential answered %d "+
			"(%s), want 401 — the repair of unrecorded rows must not have turned the consistency check "+
			"into 'adopt whatever the authenticator claims'", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode the 401 body: %v (%s)", err, rec.Body.String())
	}
	if out["error"] != "invalid passkey" {
		t.Fatalf("the 401 body is %v, want the passkey error — the refusal must stay the passkey path's",
			rec.Body.String())
	}
}
