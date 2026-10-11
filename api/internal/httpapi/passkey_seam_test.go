package httpapi

// Card t_df9233f6 — the passkey browser→server seam.
//
// WHY THIS FILE EXISTS. The passkey suite was green while passkey registration
// failed 100% on prod, because every existing test hand-builds its own payload
// with `base64.RawURLEncoding` (auth_passkey_legacy_workspace_test.go:150-160)
// and never consults what the shipped CLIENT sends or what the server's
// registration OPTIONS ask the authenticator for. Both defects live exactly in
// that gap:
//
//  1. ENCODING. `web/src/app/{profile,login}/page.tsx` encoded every binary
//     field with `btoa`, i.e. standard base64 (`+`, `/`, `=`), while the parser
//     decodes with `base64.RawURLEncoding` (protocol/base64.go, and the `id`
//     pre-check in protocol/credential.go's `Parse()`). Measured through the
//     real UI: 8/8 registrations returned `400 {"error":"response tidak sah"}`.
//
//  2. RESIDENT KEY. `passkeyRegisterStart` called `wa.BeginRegistration(wu)`
//     with no RegistrationOption, so the returned options carried no
//     `authenticatorSelection` at all and the browser was free to mint a
//     NON-resident credential (`isResidentCredential: false`, read back from
//     the authenticator) — which the discoverable-only login
//     (`ValidateDiscoverableLogin`) can then never reach.
//
// WHAT IS ASSERTED HERE, and why each half is needed:
//
//   - TestPasskeyRegistrationAcceptsStandardBase64 feeds the handler a body
//     whose binary fields are spelled in the STANDARD alphabet — the exact
//     bytes `btoa` produces — and demands it is accepted. Hand-encoding with
//     base64url in the test is what let the defect ship, so the payload is
//     written with `base64.StdEncoding` and the test fails loudly if the
//     server stops tolerating it.
//
//   - TestPasskeyRegistrationOptionsRequireAResidentKey drives the REAL
//     /register/start endpoint (not the webauthn library in isolation) and
//     reads `authenticatorSelection` out of the JSON the browser would
//     receive, requiring a required/discoverable resident key. Asserting the
//     library call would not bite: the option is applied by the handler.
//
//   - TestPasskeyRegistrationBodyIsWrittenInBase64URL is the CLIENT-side pin,
//     read cross-language the way this repo's other contract guards are
//     (src/lib/apiErr.test.ts, src/lib/soundToggle.test.ts): the shipped pages
//     must not encode their WebAuthn fields with `btoa`, and must route them
//     through the shared helper. Source-level because there is no JSX test
//     runner in web/ — the same reason web/src/lib/*.test.ts reads Go sources.
//
//   - TestPasskeyLoginPageRendersAPasskeyControl covers defect 3: the login
//     page must keep a control that calls passkeyLogin, so dead code cannot
//     ship as a feature a second time.
//
//   - TestPasskeyDocsSectionIsDeepLinked covers t_254d8a81: the /docs#passkeys
//     anchor must really be reachable from the UI (AGENTS.md docs-parity rule 4),
//     and the docs page's own comment that claimed the login page deep-linked it
//     (which it never did) must stay gone. That claim read like documentation and
//     was checked by nothing — TestPasskeyDocsDescribeTheRecoveryPath asserts the
//     section TEXT only, so it stayed green while no surface linked the section.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── 1. the encoding seam ─────────────────────────────────────────────────────

// passkeyStdEncodedBody builds a registration/finish body exactly the way the
// PRE-FIX shipped pages built it: every binary field encoded with the STANDARD
// base64 alphabet. The bytes are chosen so the standard spelling really does
// contain `+` and `/` — otherwise the test would pass against the broken server
// too, which is the trap the existing suite fell into.
func passkeyStdEncodedBody(t *testing.T, key string) []byte {
	t.Helper()
	// 0xfb 0xff 0xfe -> "w/7+" in standard base64. 0xff 0xfe 0xfd -> "//79".
	// Every field is padded and carries one of the two characters.
	plusSlash := []byte{0xfb, 0xff, 0xfe, 0xfd}
	enc := base64.StdEncoding.EncodeToString(plusSlash)
	if !strings.ContainsAny(enc, "+/") {
		t.Fatalf("test fixture is not exercising the alphabet: %q has no + or /", enc)
	}
	return mustJSON(map[string]any{
		"key": key,
		"id":  enc, "rawId": enc, "type": "public-key",
		"response": map[string]any{
			"attestationObject": enc,
			"clientDataJSON":    enc,
		},
	})
}

// TestNormalizePasskeyBase64RewritesTheAlphabet is the unit-level guard on the
// normaliser: a standard-base64 value becomes base64url, a value already in the
// URL alphabet is untouched, and a non-string member is left alone (so the
// normaliser cannot fabricate data for a malformed body).
func TestNormalizePasskeyBase64RewritesTheAlphabet(t *testing.T) {
	raw := []byte(`{
		"key":"k1",
		"id":"w/7+/Q==",
		"rawId":"w/7+/Q==",
		"type":"public-key",
		"response":{
			"attestationObject":"w/7+/Q==",
			"clientDataJSON":"w_7-_Q",
			"signature":null,
			"userHandle":42
		}
	}`)
	var got map[string]any
	if err := json.Unmarshal(normalizePasskeyBase64(raw), &got); err != nil {
		t.Fatalf("normalised body is not JSON: %v", err)
	}

	// standard base64 -> base64url, padding stripped.
	for _, field := range []string{"id", "rawId"} {
		if got[field] != "w_7-_Q" {
			t.Errorf("%s = %v, want %q (+ -> -, / -> _, = stripped)", field, got[field], "w_7-_Q")
		}
	}
	resp, ok := got["response"].(map[string]any)
	if !ok {
		t.Fatalf("response is not an object: %T", got["response"])
	}
	if resp["attestationObject"] != "w_7-_Q" {
		t.Errorf("attestationObject = %v, want %q", resp["attestationObject"], "w_7-_Q")
	}
	// already-base64url stays byte-identical: the rewrite is idempotent.
	if resp["clientDataJSON"] != "w_7-_Q" {
		t.Errorf("a value already in the URL alphabet was changed: %v", resp["clientDataJSON"])
	}
	// A member that is not a string is left exactly as it was.
	if resp["signature"] != nil {
		t.Errorf("a null member was rewritten to %v", resp["signature"])
	}
	if resp["userHandle"] != float64(42) {
		t.Errorf("a non-string member was rewritten to %v", resp["userHandle"])
	}

	// The rewrite must not silently drop the rest of the body: the session key
	// the /finish leg depends on survives.
	if got["key"] != "k1" {
		t.Errorf("key = %v, want k1 — normalisation lost a non-binary member", got["key"])
	}
}

// TestNormalizePasskeyBase64IsIdempotentAndLossless proves the two alphabets
// decode to the SAME bytes, which is the property that makes accepting both
// safe: a standard-base64 body and its base64url spelling must produce an
// identical decoded value, not a value that merely parses.
func TestNormalizePasskeyBase64IsIdempotentAndLossless(t *testing.T) {
	// Bytes whose standard spelling carries both characters and padding.
	want := []byte{0xfb, 0xff, 0xfe, 0xfd}
	std := base64.StdEncoding.EncodeToString(want) // "w/7+/Q=="
	url := base64.RawURLEncoding.EncodeToString(want)

	body := func(v string) []byte {
		return mustJSON(map[string]any{"id": v, "response": map[string]any{"clientDataJSON": v}})
	}
	decode := func(raw []byte) []byte {
		t.Helper()
		var got struct {
			ID       string `json:"id"`
			Response struct {
				ClientDataJSON string `json:"clientDataJSON"`
			} `json:"response"`
		}
		if err := json.Unmarshal(normalizePasskeyBase64(raw), &got); err != nil {
			t.Fatalf("decode normalised body: %v", err)
		}
		out, err := base64.RawURLEncoding.DecodeString(got.ID)
		if err != nil {
			t.Fatalf("the normalised id is still not RawURLEncoded (%q): %v", got.ID, err)
		}
		if got.Response.ClientDataJSON != got.ID {
			t.Fatalf("the two members diverged: %q vs %q", got.ID, got.Response.ClientDataJSON)
		}
		return out
	}

	fromStd := decode(body(std))
	fromURL := decode(body(url))
	if string(fromStd) != string(want) {
		t.Fatalf("standard base64 decoded to %x, want %x", fromStd, want)
	}
	if string(fromStd) != string(fromURL) {
		t.Fatalf("the two alphabets decoded differently: %x vs %x", fromStd, fromURL)
	}
	// Re-normalising an already-normalised body is a no-op.
	once := normalizePasskeyBase64(body(std))
	twice := normalizePasskeyBase64(once)
	if string(once) != string(twice) {
		t.Fatalf("normalisation is not idempotent:\n once: %s\ntwice: %s", once, twice)
	}
}

// seamCredIDForRun appends this run's entropy to a fixed credential-id prefix,
// so a repeat pass of the same test does not collide on the stored row while the
// prefix keeps providing the base64 alphabet the arm asserts on.
func seamCredIDForRun(prefix []byte) []byte {
	out := make([]byte, 0, len(prefix)+len(runKey()))
	out = append(out, prefix...)
	out = append(out, []byte(runKey())...)
	return out
}

// seamCredIDStandard / seamCredIDURL are the credential ids the two arms of
// TestPasskeyRegistrationAcceptsStandardBase64 register.
//
// They are FIXED, not random, and their bytes are chosen so that
// base64.StdEncoding spells each of them with BOTH `+` and `/` (measured:
// "+//++//++//++//++//++//++//++//++//++//++wA=" and "...wE="). Two properties
// matter and each was got wrong once:
//
//   - DETERMINISM. The first version used the random id
//     newPasskeyTestAuthenticator generates and asserted afterwards that the
//     alphabet difference was present. A random 32-byte id carries a `+` or `/`
//     only about three runs in four, so that assertion failed ~1 run in 4 for a
//     reason unrelated to the code. A guard that fails randomly teaches people
//     to re-run it.
//
//   - DISTINCTNESS. The two ids must differ, because PasskeyStore.Save is
//     `INSERT ... ON CONFLICT (id) DO UPDATE`. With one shared id the second arm
//     would find the row the FIRST arm inserted and pass without storing
//     anything of its own — the upsert would mask exactly the failure the arm
//     exists to catch.
var (
	seamCredIDStandard = []byte{
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe,
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe,
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0x00,
	}
	seamCredIDURL = []byte{
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe,
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe,
		0xfb, 0xff, 0xfe, 0xfb, 0xff, 0xfe, 0xfb, 0x01,
	}
)

// passkeyRegistrationFixture is a genuine attested credential: an ES256 key,
// a real CBOR attestation object carrying it, and the client data the ceremony
// is signed over. Nothing is fabricated — the same shape auth_passkey_legacy_
// workspace_test.go signs assertions with, extended to the create() side.
type passkeyRegistrationFixture struct {
	authn   passkeyTestAuthenticator
	cose    []byte
	key     string
	rpID    string
	origin  string
	aaguid  []byte
	session webauthn.SessionData
	// flags is the authenticator-data flag octet the attestation carries.
	// newPasskeyRegistrationFixture sets UP|AT — the shape of a credential that
	// is neither synced nor user-verified. A caller exercising a REAL synced
	// passkey (iCloud Keychain / Touch ID, Google Password Manager, Windows
	// Hello) overrides it with UP|UV|AT|BE|BS, which is also the shape that
	// carries a non-ASCII AAGUID; see passkey_real_assertion_test.go.
	flags protocol.AuthenticatorFlags
}

// newPasskeyRegistrationFixture drives the REAL /register/start endpoint for a
// freshly registered user and returns everything needed to answer it correctly.
//
// The challenge the attestation must be bound to is read from the SERVER's
// stored session rather than invented, so the only variable between the passing
// and failing halves of the encoding test is the alphabet.
func newPasskeyRegistrationFixture(t *testing.T, h *RESTHandlers, ah *AuthHandlers, pool *pgxpool.Pool, user, session string, credID []byte) passkeyRegistrationFixture {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "http://"+passkeyOriginHost+"/v1/auth/passkeys/register/start", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	setTestClientAddr(req, uniqueTestIP())
	rec := httptest.NewRecorder()
	h.passkeyRegisterStart(rec, req, &AccountCtx{Account: accountForUser(t, h, user)})
	if rec.Code != http.StatusOK {
		t.Fatalf("register/start status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var st struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Key == "" {
		t.Fatalf("register/start body has no key: %v (%s)", err, rec.Body.String())
	}

	// The session the server stored — the challenge, RP ID and user handle the
	// attestation must match. Read, never rebuilt.
	var raw string
	if err := pool.QueryRow(context.Background(),
		`SELECT value FROM passkey_challenges WHERE key=$1 AND kind='register'`, st.Key).Scan(&raw); err != nil {
		t.Fatalf("read stored register challenge: %v", err)
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sd); err != nil {
		t.Fatalf("decode stored register session: %v", err)
	}
	if sd.Challenge == "" || sd.RelyingPartyID == "" || len(sd.UserID) == 0 {
		t.Fatalf("stored register session is incomplete: %+v", sd)
	}

	authn, cose := newPasskeyTestAuthenticator(t)
	// A fixed id, not the generated one: the caller passes one whose
	// standard-base64 spelling reliably carries `+` and `/`, and which differs
	// between the two arms so the Save upsert cannot mask a failure.
	authn.id = credID
	// The AAGUID a REAL browser sends: Chrome's own, whose first byte is 0xad,
	// i.e. not valid UTF-8. It cannot be taken from the browser: Chrome's VIRTUAL
	// authenticator ignores a requested aaguid and attests its own all-ASCII
	// default, which is exactly the one shape the old write path could store.
	// `string(AAGUID)` reinterpreted the raw bytes as text, so Postgres refused
	// the INSERT with SQLSTATE 22021 for every real authenticator (Chrome 0xad,
	// Google Password Manager 0x9d, Windows Hello 0x98, and the spec's all-zero
	// none-attestation value 0x00); the all-ASCII default is why this stayed
	// hidden. The byte-exact assertion below is what pins it.
	aaguid := []byte{
		0xad, 0xce, 0x00, 0x02, 0x35, 0xbc, 0xc6, 0x0a,
		0x64, 0x8b, 0x0b, 0x25, 0xf1, 0xf0, 0x55, 0x03,
	}
	return passkeyRegistrationFixture{
		authn: authn, cose: cose, key: st.Key,
		rpID: sd.RelyingPartyID, origin: "https://" + passkeyOriginHost,
		aaguid: aaguid, session: sd,
		flags: protocol.FlagUserPresent | protocol.FlagAttestedCredentialData,
	}
}

// attestationBody builds the JSON body the shipped client posts to
// /v1/auth/passkeys/register/finish, with every binary field encoded by `enc`.
//
// Passing base64.StdEncoding reproduces the pre-fix client byte-for-byte;
// passing base64.RawURLEncoding is what the fixed client sends. The authData is
// the attested-credential-data form: rpIdHash || flags || counter || AAGUID ||
// credIdLen || credId || COSE public key, where flags is the fixture's own (see
// the flags field; UP|AT by default). The CBOR attestation object
// declares fmt "none" with an empty attStmt, which is what a browser sends when
// no attestation is requested — and go-webauthn accepts it (VerifyAttestation
// short-circuits on AttestationFormatNone once the statement is empty).
func (f passkeyRegistrationFixture) attestationBody(t *testing.T, enc *base64.Encoding) []byte {
	t.Helper()

	if !f.flags.HasAttestedCredentialData() {
		// The body below embeds attested credential data, so a fixture whose
		// flags clear AT would be rejected by go-webauthn's parser with
		// "Attested credential flag not set" — a confusing failure for a
		// fixture mistake.
		t.Fatalf("fixture flags %#02x clear the AT bit but the body carries attested credential data", byte(f.flags))
	}

	clientDataJSON, err := json.Marshal(map[string]any{
		"type":        "webauthn.create",
		"challenge":   f.session.Challenge,
		"origin":      f.origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}

	rpIDHash := sha256.Sum256([]byte(f.rpID))
	authData := make([]byte, 0, 55+len(f.authn.id)+len(f.cose))
	authData = append(authData, rpIDHash[:]...)
	authData = append(authData, byte(f.flags)) // the fixture's own flag octet
	authData = binary.BigEndian.AppendUint32(authData, 0)
	authData = append(authData, f.aaguid...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(f.authn.id)))
	authData = append(authData, f.authn.id...)
	authData = append(authData, f.cose...)

	attObj, err := webauthncbor.Marshal(map[string]any{
		"authData": authData,
		"fmt":      "none",
	})
	if err != nil {
		t.Fatalf("marshal attestation object: %v", err)
	}

	// The alphabet under test, applied to EVERY binary member the parser reads.
	return mustJSON(map[string]any{
		"key":   f.key,
		"id":    enc.EncodeToString(f.authn.id),
		"rawId": enc.EncodeToString(f.authn.id),
		"type":  "public-key",
		"response": map[string]any{
			"attestationObject": enc.EncodeToString(attObj),
			"clientDataJSON":    enc.EncodeToString(clientDataJSON),
		},
	})
}

// TestPasskeyRegistrationAcceptsStandardBase64 is the seam guard for defect 1.
//
// It performs a CRYPTOGRAPHICALLY GENUINE registration ceremony twice — same
// authenticator shape, same server-issued challenge, same code path — changing
// ONLY the encoding of the body:
//
//	standard base64 (what `btoa` emits, i.e. what shipped)  -> must be accepted
//	base64url      (what the fixed client sends)            -> must be accepted
//
// The assertion is the credential ROW the handler persists, not the status
// code: a server that rejects the standard alphabet with `400 response tidak
// sah` (the measured pre-fix behaviour) fails here, and so does one that
// answers 200 without storing anything.
//
// Why a real attestation and not a hand-built stub: the previous passkey tests
// hand-encoded with RawURLEncoding only, so they proved the verifier and never
// the alphabet. A test that feeds garbage bytes cannot tell "wrong alphabet"
// from "not a credential" — both surface as the same `response tidak sah` — so
// it would pass against a server that had only been fixed to say something
// different. Binding a valid attestation to the server's own challenge is what
// makes the alphabet the sole variable.
func TestPasskeyRegistrationAcceptsStandardBase64(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	h := passkeyHandlers(t, pool)
	h.Events = dbstore.NewEventStore(pool)

	mux := http.NewServeMux()
	ah.RegisterRoutes(mux)
	pw := postJSON(mux, "/v1/auth/login",
		map[string]any{"email": emailOfUser(t, pool, user), "password": "password123"}, uniqueTestIP())
	session := sessionTokenValue(pw)
	if session == "" {
		t.Fatalf("no session cookie on login (status %d body %s)", pw.Code, pw.Body.String())
	}
	actx := &AccountCtx{Account: accountForUser(t, h, user)}

	for _, tc := range []struct {
		name   string
		enc    *base64.Encoding
		credID []byte
	}{
		// The credential id varies per RUN (card t_5299cae1). The stored rows are
		// keyed by credential id and `-test.count` re-runs in the same process
		// against the same database, so a fixed id makes pass 2's insert collide
		// and the endpoint answer 200 while persisting nothing — which is exactly
		// what the assertion below this loop reads as "0 row(s)". The fixed
		// PREFIX is kept because the arm's `+`/`/` check below depends on it.
		{"standard base64 (what the shipped client sent)", base64.StdEncoding, seamCredIDForRun(seamCredIDStandard)},
		{"base64url (what the fixed client sends)", base64.RawURLEncoding, seamCredIDForRun(seamCredIDURL)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newPasskeyRegistrationFixture(t, h, ah, pool, user, session, tc.credID)
			body := fx.attestationBody(t, tc.enc)

			// The fixture must actually exercise the alphabet that differs,
			// otherwise the standard-base64 arm would be a duplicate of the
			// base64url one and prove nothing. The ids are chosen to make this
			// hold on EVERY run; the check stays because a future edit could
			// silently make the arm vacuous.
			std := base64.StdEncoding.EncodeToString(tc.credID)
			if !strings.Contains(std, "+") || !strings.Contains(std, "/") {
				t.Fatalf("the %s arm no longer carries both + and / (%q) — the arm would prove nothing; "+
					"pick an id whose StdEncoding spelling has both", tc.name, std)
			}

			// The /register/start row is still in passkey_challenges (PutChallenge
			// only expired it), and /finish consumes it by the key /start issued,
			// so the attestation is bound to exactly the challenge the server
			// recorded. Nothing needs to be re-stored here — re-storing would be
			// how this test stops testing the real flow.
			req := httptest.NewRequest(http.MethodPost,
				"http://"+passkeyOriginHost+"/v1/auth/passkeys/register/finish", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
			setTestClientAddr(req, uniqueTestIP())
			rec := httptest.NewRecorder()
			h.passkeyRegisterFinish(rec, req, actx)

			if rec.Code != http.StatusOK {
				t.Fatalf("register/finish with a valid attestation answered %d (%s) — a body in %s "+
					"must be accepted; the parser decodes with base64.RawURLEncoding and the shipped pages "+
					"encoded with btoa (the standard alphabet), which is why registration failed 8/8 on prod "+
					"with exactly this `response tidak sah`",
					rec.Code, rec.Body.String(), tc.name)
			}

			// The credential must be PERSISTED, not merely acknowledged.
			creds, err := h.Passkeys.ByUser(ctx, user)
			if err != nil {
				t.Fatalf("read back credentials: %v", err)
			}
			var stored *webauthn.Credential
			for i, c := range creds {
				if string(c.ID) == string(fx.authn.id) {
					stored = &creds[i]
					break
				}
			}
			if stored == nil {
				t.Fatalf("register/finish answered 200 but stored no credential for the authenticator "+
					"(have %d row(s)) — a 200 that persists nothing is the shape this guard exists to catch",
					len(creds))
			}
			// The AAGUID must survive the round trip BYTE-EXACT. The fixture's
			// first byte is 0xad, so a write path that reinterprets the raw bytes
			// as text either fails the INSERT outright or stores a mangled value;
			// asserting only that a row exists would miss both.
			if !bytes.Equal(stored.Authenticator.AAGUID, fx.aaguid) {
				t.Fatalf("the stored AAGUID is %x, want %x — the 16 raw bytes did not survive the "+
					"Save/read round trip", stored.Authenticator.AAGUID, fx.aaguid)
			}
		})
	}
}

// emailOfUser resolves a fixture user's email so the login leg can be a real one.
func emailOfUser(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var email string
	if err := pool.QueryRow(context.Background(), `SELECT email FROM users WHERE id=$1`, userID).Scan(&email); err != nil {
		t.Fatalf("resolve email for %s: %v", userID, err)
	}
	return email
}

// ── 2. the resident-key seam ─────────────────────────────────────────────────

// TestPasskeyRegistrationOptionsRequireAResidentKey drives the REAL
// /register/start endpoint and reads the options the BROWSER would receive.
//
// The login ceremony verifies with ValidateDiscoverableLogin, so a credential
// that is not client-side discoverable can never sign in. Registration must
// therefore ASK for one. Asserting the webauthn library in isolation would not
// bite — an empty selection is the library's own default, so only the options
// the handler actually returns prove the requirement reached the wire.
func TestPasskeyRegistrationOptionsRequireAResidentKey(t *testing.T) {
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	h := passkeyHandlers(t, pool)
	h.Events = dbstore.NewEventStore(pool)

	mux := http.NewServeMux()
	ah.RegisterRoutes(mux)
	pw := postJSON(mux, "/v1/auth/login",
		map[string]any{"email": emailOfUser(t, pool, user), "password": "password123"}, uniqueTestIP())
	session := sessionTokenValue(pw)
	if session == "" {
		t.Fatalf("no session cookie on login (status %d body %s)", pw.Code, pw.Body.String())
	}

	// The registered-key requirement must reach the wire at the deployment's own
	// host: passkey endpoints refuse a request that did not arrive on the app's
	// domain (passkeyRPIdentity), so driving this with the httptest default host
	// would answer 403 and measure nothing.
	req := httptest.NewRequest(http.MethodPost,
		"http://"+passkeyOriginHost+"/v1/auth/passkeys/register/start", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	setTestClientAddr(req, uniqueTestIP())
	rec := httptest.NewRecorder()
	h.passkeyRegisterStart(rec, req, &AccountCtx{Account: accountForUser(t, h, user)})
	if rec.Code != http.StatusOK {
		t.Fatalf("register/start status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The shape the client consumes: {"options":{"publicKey":{...}},"key":"..."}.
	var out struct {
		Options struct {
			PublicKey struct {
				AuthenticatorSelection *struct {
					RequireResidentKey *bool  `json:"requireResidentKey"`
					ResidentKey        string `json:"residentKey"`
				} `json:"authenticatorSelection"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode register/start options: %v (%s)", err, rec.Body.String())
	}

	sel := out.Options.PublicKey.AuthenticatorSelection
	if sel == nil {
		t.Fatalf("register/start returned NO authenticatorSelection (%s). The login ceremony is "+
			"discoverable-only (ValidateDiscoverableLogin), so a credential minted without the resident-key "+
			"requirement is invisible to navigator.credentials.get() and the user's only symptom is "+
			"\"Passkey cancelled or not supported by this browser\".", rec.Body.String())
	}
	if sel.ResidentKey != "required" {
		t.Errorf("authenticatorSelection.residentKey = %q, want \"required\" — the login ceremony is "+
			"discoverable-only", sel.ResidentKey)
	}
	if sel.RequireResidentKey == nil || !*sel.RequireResidentKey {
		t.Errorf("authenticatorSelection.requireResidentKey = %v, want true (the deprecated-by-name member "+
			"browsers still honour must agree with residentKey, or a client reading only one of the two mints "+
			"a non-discoverable credential)", sel.RequireResidentKey)
	}
}

// ── 3. the client half, read cross-language ──────────────────────────────────

// webRootFromAPI resolves the repo's web/ directory from this package's path.
// The tests run with the package as the working directory, so the tree is three
// levels up — the same relative shape web/src/lib/*.test.ts uses in reverse.
func webRootFromAPI(t *testing.T) string {
	t.Helper()
	// The literal is the SOURCE TREE `../../../web/src`, not the repo root joined
	// with "web". The preflight's path deriver turns a joined directory literal
	// into a claim on that whole directory (scripts/preflight.sh, pw_derive_all),
	// so `web` made the `api test` gate claim EVERY path under web/ — including
	// ones this guard never opens. Measured: with the joined form,
	// `./scripts/preflight.sh --classify=web/tsconfig.json` returned
	// `build vet test govuln web …`, i.e. `test` for a file no Go test reads, and
	// the `selftest` gate's narrowing check goes red on exactly that. Naming the
	// subdirectory the guard really reads keeps the claim true; `web/src/app/**`
	// still selects `test`, which it must, because this guard does read it.
	webSrc, err := filepath.Abs("../../../web/src")
	if err != nil {
		t.Fatalf("resolve the web source tree: %v", err)
	}
	web := filepath.Dir(webSrc)
	if _, err := os.Stat(filepath.Join(webSrc, "lib", "webauthn.ts")); err != nil {
		t.Skipf("web tree not present at %s (%v) — the cross-language half needs the full repo", web, err)
	}
	return web
}

// webSource reads a file out of web/ and fails the test if it is missing.
func webSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(webRootFromAPI(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// btoaCall matches `btoa(` at a call position (not inside an identifier).
var btoaCall = regexp.MustCompile(`\bbtoa\s*\(`)

// TestPasskeyRegistrationBodyIsWrittenInBase64URL is the client-side pin.
//
// The shipped pages encoded their WebAuthn fields with `btoa`, which emits the
// STANDARD alphabet; the server decodes with RawURLEncoding. Nothing in the Go
// suite could see that, which is why a green gate coexisted with a 100%-failing
// endpoint. The pages must therefore encode through the one shared helper
// (web/src/lib/webauthn.ts) and must not call `btoa` themselves.
func TestPasskeyRegistrationBodyIsWrittenInBase64URL(t *testing.T) {
	web := webRootFromAPI(t)

	for _, rel := range []string{
		"src/app/profile/page.tsx",
		"src/app/login/page.tsx",
	} {
		src := webSource(t, rel)
		if btoaCall.MatchString(src) {
			t.Errorf("%s calls btoa() — standard base64 (+, /) is rejected by go-webauthn's "+
				"base64.RawURLEncoding decode, which is the exact defect that made passkey "+
				"registration return 400 \"response tidak sah\". Encode through lib/webauthn.ts's "+
				"b64url()", rel)
		}
		// The positive half: the helper is imported and actually used, so the
		// fields cannot have been dropped instead of re-encoded.
		if !strings.Contains(src, `from "../../lib/webauthn"`) {
			t.Errorf("%s does not import the shared WebAuthn encoder from lib/webauthn.ts — the "+
				"browser→server alphabet contract must have one implementation, not a copy per page", rel)
		}
		if !strings.Contains(src, "b64url(") {
			t.Errorf("%s imports the WebAuthn helper but never calls b64url() — the ceremony's binary "+
				"fields would be encoded by something else", rel)
		}
	}

	// The helper itself must emit the URL alphabet with padding stripped.
	helper := webSource(t, "src/lib/webauthn.ts")
	for _, want := range []string{`replace(/\+/g, "-")`, `replace(/\//g, "_")`, `replace(/=+$/, "")`} {
		if !strings.Contains(helper, want) {
			t.Errorf("src/lib/webauthn.ts no longer performs %s — the encoder would emit the standard "+
				"alphabet and every passkey ceremony would fail to parse server-side", want)
		}
	}
	if v := filepath.Join(web, "src", "lib", "webauthn.ts"); v == "" {
		t.Fatal("unreachable")
	}
}

// TestPasskeyLoginPageRendersAPasskeyControl is the reachability guard for
// defect 3.
//
// `passkeyLogin()` existed in web/src/app/login/page.tsx with NO caller in any
// commit (`git log --all -S 'onClick={passkeyLogin}'` was empty), so the shipped
// login page rendered no passkey control at all and the whole ceremony was
// unreachable from the UI. A guard that only asserted the function exists would
// have passed the whole time; this one requires a control that calls it.
func TestPasskeyLoginPageRendersAPasskeyControl(t *testing.T) {
	src := webSource(t, "src/app/login/page.tsx")

	if !strings.Contains(src, "async function passkeyLogin(") {
		t.Fatalf("web/src/app/login/page.tsx no longer defines passkeyLogin()")
	}
	// The handler must be wired to a rendered control, not merely declared.
	// JSX attribute spelling is exactly what makes a function dead code, so the
	// assertion is on the attribute rather than on the identifier.
	if !strings.Contains(src, "onClick={passkeyLogin}") {
		t.Errorf("web/src/app/login/page.tsx defines passkeyLogin() but renders no control that calls it " +
			"(no `onClick={passkeyLogin}`) — passkey sign-in is unreachable from the UI, which is exactly how " +
			"it shipped: the function was added in 6816eff and never referenced")
	}
}

// TestPasskeyDocsDescribeTheRecoveryPath covers the half of this change a user
// actually reads.
//
// AGENTS.md's documentation-parity rule requires a feature to be documented, and
// the card asks specifically that the consequence for EXISTING rows be stated
// rather than left implicit: a credential registered before the resident-key fix
// is non-discoverable, so the login button cannot reach it, and the only recovery
// is to delete the row and register again. Without that sentence a returning user
// meets a button that silently fails and has no way to know why — the same
// dead-end as the original defect, one layer out.
func TestPasskeyDocsDescribeTheRecoveryPath(t *testing.T) {
	docs := webSource(t, "src/app/docs/page.tsx")

	if !strings.Contains(docs, `slug: "passkeys"`) {
		t.Fatalf("web/src/app/docs/page.tsx has no passkeys section — the feature is undocumented " +
			"(AGENTS.md documentation parity)")
	}
	start := strings.Index(docs, `slug: "passkeys"`)
	section := docs[start:]
	if end := strings.Index(section, "\n  },"); end > 0 {
		section = section[:end]
	}
	// Both halves the user needs: that a control exists, and what to do when an
	// older passkey cannot be used by it.
	for _, want := range []struct{ needle, why string }{
		{"login page", "the docs must say where the passkey sign-in control is, or the feature reads as absent"},
		{"non-discoverable", "the docs must name the reason an older passkey cannot be used"},
		{"register a new passkey", "the docs must state the recovery, not leave it implicit"},
	} {
		if !strings.Contains(section, want.needle) {
			t.Errorf("the docs passkeys section does not mention %q — %s", want.needle, want.why)
		}
	}
}

// TestPasskeyDocsSectionIsDeepLinked is the reachability + honesty guard for the
// /docs#passkeys anchor (card t_254d8a81, AGENTS.md docs-parity rule 4).
//
// WHAT IT EXISTS FOR. The passkeys section shipped with NO inline "?" doc link on
// any surface (`grep -rn '#passkeys' web/src` returned nothing), so the anchor was
// unreachable from the dashboard — and the /docs page's own comment claimed
// otherwise ("The login page's passkey control deep-links this section"), a link
// the login page has never had (`grep -c docs web/src/app/login/page.tsx` == 0).
// TestPasskeyDocsDescribeTheRecoveryPath asserts the section TEXT only, so the
// suite stayed green the whole time the claim was false. This test makes the claim
// checkable: the link must EXIST, and the false sentence must stay gone.
//
// The assertions are on the page SOURCE because this Go package is the
// cross-language seam that reads web/ (webSource / webRootFromAPI); there is no
// JSX runner in web/. The web twin, web/src/lib/passkeyDocs.test.ts, pins the same
// contract locally, in the suite that reads the dictionary too.
func TestPasskeyDocsSectionIsDeepLinked(t *testing.T) {
	profile := webSource(t, "src/app/profile/page.tsx")
	docs := webSource(t, "src/app/docs/page.tsx")
	login := webSource(t, "src/app/login/page.tsx")

	// 1. The destination is real: the section exists and its slug is rendered as the
	//    element id, or the link would land at the top of /docs without scrolling.
	if !strings.Contains(docs, `slug: "passkeys"`) {
		t.Fatalf("web/src/app/docs/page.tsx has no `slug: \"passkeys\"` section — /docs#passkeys would land at the top of the page")
	}
	if !strings.Contains(docs, "id={f.slug}") {
		t.Fatalf("web/src/app/docs/page.tsx does not render the slug as an id — /docs#passkeys would not scroll")
	}

	// 2. The anchor is reachable: a Profile → Passkeys inline "?" link, the shape the
	//    2FA row on the Security tab carries, points at it. The passkeys tab block is
	//    sliced out so an `href` on another row cannot satisfy this.
	const tabGuard = `{tab === "passkeys" && (`
	start := strings.Index(profile, tabGuard)
	if start < 0 {
		t.Fatalf("web/src/app/profile/page.tsx no longer renders a passkeys tab (`%s`) — the surface the passkey docs link belongs on moved", tabGuard)
	}
	block := profile[start:]
	if end := strings.Index(block, "{/* Connected accounts tab"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, `href="/docs#passkeys"`) {
		t.Errorf("the Profile → Passkeys tab renders no `href=\"/docs#passkeys\"` — the passkeys feature has no " +
			"AGENTS.md docs-parity rule-4 inline \"?\" doc link, so its docs section is unreachable from the UI " +
			"(this is exactly how it shipped: `grep -rn '#passkeys' web/src` returned nothing)")
	}
	// The link is labelled with its own dictionary key, not left as a bare circle.
	if !strings.Contains(block, `title={t("prof.passkeysDocs")}`) {
		t.Errorf("the passkeys \"?\" link carries no title={t(\"prof.passkeysDocs\")} — copy the 2FA row's shape " +
			"(title={t(\"prof.twoFaDocs\")}), or the link has no accessible label")
	}

	// 3. THE FALSE CLAIM must not come back. It was never true, and a test that only
	//    asserted the link (above) would not stop the comment re-asserting it as a
	//    premise a reader would believe instead of checking.
	//
	//    The claim is matched against a NORMALISED view of the source — comment
	//    markers dropped, whitespace collapsed — because the sentence originally
	//    shipped WRAPPED across two `//` lines, and a raw-source regex silently
	//    misses that shape (an earlier cut of this guard did, and the bite harness's
	//    wrapped case is what caught it).
	normalised := strings.Join(strings.Fields(strings.ReplaceAll(docs, "//", " ")), " ")
	if passkeyClaim := regexp.MustCompile(`(?i)login page'?s passkey control deep-links`); passkeyClaim.MatchString(normalised) {
		t.Errorf("web/src/app/docs/page.tsx again claims the login page's passkey control deep-links #passkeys — " +
			"that is the false comment this card removed; nothing on the login page links /docs")
	}
	if strings.Contains(login, "/docs") {
		t.Errorf("web/src/app/login/page.tsx now links /docs — the docs comment's honest boundary (the login page " +
			"carries no doc link) has changed, so the comment and this guard must be revisited together")
	}
}
