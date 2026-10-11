package httpapi

// t_e28e4406 — the passkey login must apply the same legacy-onboarding rule as
// every other login path.
//
// WHY THIS FILE EXISTS. The hook that gives a form-owning but workspace-less
// user a workspace at login used to live only on AuthHandlers.startSession, and
// its doc comment claimed that covered "password, magic link, social, passkey,
// post-2FA". Passkey was not covered: the login ceremony is a REST endpoint and
// mints its session through RESTHandlers.startSessionFor, a different function
// that never provisioned. The consequence is permanent, not cosmetic — see
// TestPasskeyLoginProvisionsTheLegacyWorkspace's comment for the exact chain.
//
// The two tests here are deliberately different in kind:
//
//   - TestPasskeyLoginProvisionsTheLegacyWorkspace drives the REAL
//     passkeyLoginFinish handler with a cryptographically genuine ES256
//     assertion signed in-process, so the assertion exercises signature,
//     rpIdHash, flag and userHandle verification rather than a stub, and the
//     thing it asserts is the wiring: the provisioning runs on the passkey
//     login path.
//
//   - TestEverySessionIssuerProvisionsTheLegacyWorkspace is a completeness
//     guard. It enumerates the functions in this package that issue a session
//     and requires each to call provisionLegacyWorkspace, so a login path added
//     later cannot quietly repeat exactly this mistake. A store-level or
//     handler-level test cannot catch that class: the rule exists, its tests
//     pass, and the new path simply never calls it.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// passkeyOriginHost is the request Host the passkey endpoints are driven with.
// webAuthnInstance derives the Relying Party ID from it and the ceremony pins
// the origin to it, so the signed assertion and the request must agree.
const passkeyOriginHost = "jomform.com"

// passkeyTestAuthenticator is a throwaway software authenticator: an ES256 key
// pair plus the credential id the server has on file for it. It signs real
// assertions, so nothing about the ceremony's verification is bypassed.
type passkeyTestAuthenticator struct {
	priv *ecdsa.PrivateKey
	id   []byte
}

// newPasskeyTestAuthenticator generates a credential and returns it with the
// COSE public-key bytes to persist.
func newPasskeyTestAuthenticator(t *testing.T) (passkeyTestAuthenticator, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   int64(webauthncose.EllipticKey),
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  int64(webauthncose.P256),
		XCoord: priv.X.FillBytes(make([]byte, 32)),
		YCoord: priv.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatalf("marshal COSE key: %v", err)
	}
	return passkeyTestAuthenticator{priv: priv, id: id}, cose
}

// assertionBody builds the JSON body the shipped client posts to
// /v1/auth/passkeys/login/finish, signing authData || sha256(clientDataJSON),
// with user-present as its only flag: a credential that is not synced and not
// user-verified. That is a legitimate authenticator shape, and it is what these
// two tests need — they assert the legacy-workspace wiring, not the flags.
//
// The flags are a parameter, not a constant, because the flags are exactly what
// the hand-built shape used to hide. The authenticator data is the 37-byte
// assertion form: rpIdHash || flags || counter, with no attested credential
// data (that flag belongs to registration) and no extensions. A REAL synced
// passkey asserts UP|UV|BE|BS — see assertionBodyWithFlags and
// passkey_real_assertion_test.go, which drives that shape through the whole
// ceremony.
func (a passkeyTestAuthenticator) assertionBody(t *testing.T, key, rpID, origin, challenge string, userHandle []byte) []byte {
	t.Helper()
	return a.assertionBodyWithFlags(t, key, rpID, origin, challenge, userHandle, protocol.FlagUserPresent)
}

// assertionBodyWithFlags is assertionBody with the authenticator-data flag octet
// chosen by the caller, so a test can sign the shape a real authenticator
// actually produces rather than one that happens to agree with the server's
// stored state.
func (a passkeyTestAuthenticator) assertionBodyWithFlags(t *testing.T, key, rpID, origin, challenge string, userHandle []byte, flags protocol.AuthenticatorFlags) []byte {
	t.Helper()

	clientDataJSON, err := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   challenge,
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}

	rpIDHash := sha256.Sum256([]byte(rpID))
	authData := make([]byte, 0, 37)
	authData = append(authData, rpIDHash[:]...)
	authData = append(authData, byte(flags))
	authData = binary.BigEndian.AppendUint32(authData, 1)

	clientHash := sha256.Sum256(clientDataJSON)
	// ecdsa.SignASN1 signs the bytes it is handed as an already-computed digest,
	// while the verifier hashes the concatenation itself (see
	// webauthncose.EC2PublicKeyData.Verify, which writes the signed data through
	// the algorithm's hash). So the value handed to SignASN1 is the digest of
	// authData || sha256(clientDataJSON), not the concatenation.
	sigData := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.priv, sigData[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	return mustJSON(map[string]any{
		"key":   key,
		"id":    base64.RawURLEncoding.EncodeToString(a.id),
		"rawId": base64.RawURLEncoding.EncodeToString(a.id),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(sig),
			"userHandle":        base64.RawURLEncoding.EncodeToString(userHandle),
		},
	})
}

// passkeyHandlers wires the REST handlers the passkey ceremony needs over real
// Postgres + Valkey: the passkey store (credentials + challenges), the user and
// workspace stores, and a session issuer.
func passkeyHandlers(t *testing.T, pool *pgxpool.Pool) *RESTHandlers {
	t.Helper()
	h := newBundleHandlers(pool)
	h.Passkeys = dbstore.NewPasskeyStore(pool)
	h.Sessions = authTestValkey(t)
	return h
}

// startPasskeyLogin drives the real login/start endpoint and returns the
// challenge key plus the challenge and Relying Party ID the server recorded, so
// the assertion is signed over exactly what the server will verify against.
func startPasskeyLogin(t *testing.T, h *RESTHandlers, ip string) (key, challenge, rpID string) {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "http://"+passkeyOriginHost+"/v1/auth/passkeys/login/start", nil)
	req.Header.Set("X-Real-IP", ip)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login/start status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var st struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Key == "" {
		t.Fatalf("login/start body has no key: %v (%s)", err, rec.Body.String())
	}

	// Read the session the server stored, rather than rebuilding it: the
	// challenge the assertion must be signed over is the server's, not ours.
	var raw string
	if err := bundleTestPool(t).QueryRow(context.Background(),
		`SELECT value FROM passkey_challenges WHERE key=$1 AND kind='login'`, st.Key).Scan(&raw); err != nil {
		t.Fatalf("read stored challenge: %v", err)
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal([]byte(raw), &sd); err != nil {
		t.Fatalf("decode stored session: %v", err)
	}
	if sd.Challenge == "" || sd.RelyingPartyID == "" {
		t.Fatalf("stored session is incomplete: %+v", sd)
	}
	if sd.UserID != nil {
		t.Fatalf("login/start is not a discoverable ceremony: session.UserID = %q", sd.UserID)
	}
	return st.Key, sd.Challenge, sd.RelyingPartyID
}

// finishPasskeyLogin posts the signed assertion to the real login/finish
// endpoint and returns the recorder.
func finishPasskeyLogin(t *testing.T, h *RESTHandlers, body []byte, ip string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost,
		"http://"+passkeyOriginHost+"/v1/auth/passkeys/login/finish", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", ip)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// sessionCookieFrom pulls the session cookie token out of a response.
func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no %s cookie on the response (headers %v)", sessionCookie, rec.Result().Header)
	return ""
}

// TestPasskeyLoginProvisionsTheLegacyWorkspace is the end-to-end guard: a
// form-owning, workspace-less user who logs in WITH A PASSKEY ends up able to
// reach his forms.
//
// The failure it pins is not a missing workspace on its own — it is permanent
// stranding. After a passkey login that skips provisioning the user has 0
// workspaces, so he is shown /workspace-picker and creates one by hand. That
// create cannot attach his forms: SoleEmptyUnscopedAccount refuses an account
// that carries forms, so it falls through to Workspaces.Create, which inserts a
// SECOND account and leaves the forms on the first one. From then on every
// recovery route is gone — legacyUnscopedAccountSQL requires that the owner
// still have no workspace (workspace.go), the operator CLI answers
// "not applicable" for the same reason, and a further manual create hits the
// same refusal. The forms are unreachable with no in-product way back.
//
// Neutralise the fix by deleting the provisionLegacyWorkspace call from
// RESTHandlers.startSessionFor and this test goes red on the workspace count,
// the account count and the unscoped-form count, while every store-level test
// and the password-login test stay green.
func TestPasskeyLoginProvisionsTheLegacyWorkspace(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	// The prod shape, created through the real registration + verification flow:
	// a user, the account signup made, accounts.workspace_id IS NULL, and
	// published forms hanging off that account.
	ah := authFixtureWithWorkspaces(t)
	user, account, _ := legacyLoginFixture(t, ah, 2)

	authn, cose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, user, webauthn.Credential{
		ID:        authn.id,
		PublicKey: cose,
	}); err != nil {
		t.Fatalf("store passkey: %v", err)
	}

	var preWorkspaces int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&preWorkspaces); err != nil {
		t.Fatalf("count workspaces before: %v", err)
	}
	if preWorkspaces != 0 {
		t.Fatalf("fixture is wrong: the user already owns %d workspaces", preWorkspaces)
	}

	h := passkeyHandlers(t, pool)
	key, challenge, rpID := startPasskeyLogin(t, h, uniqueTestIP())
	if rpID != passkeyOriginHost {
		t.Fatalf("ceremony bound to RP ID %q, want %q", rpID, passkeyOriginHost)
	}

	body := authn.assertionBody(t, key, rpID, "https://"+passkeyOriginHost, challenge, []byte(user))
	rec := finishPasskeyLogin(t, h, body, uniqueTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("passkey login status = %d (body %s) — the assertion the test signed was not accepted",
			rec.Code, rec.Body.String())
	}
	token := sessionCookieFrom(t, rec)

	// The user can now REACH a workspace — the point of the rule.
	list, err := dbstore.NewWorkspaceStore(pool).ByMember(ctx, user)
	if err != nil {
		t.Fatalf("ByMember: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("after the passkey login the user sees %d workspaces, want exactly 1 — a user with forms "+
			"and no workspace must be provisioned on EVERY login path, and the passkey login reaches its "+
			"session through RESTHandlers.startSessionFor rather than AuthHandlers.startSession", len(list))
	}

	// And his forms came with it: a link that moved only the ACCOUNT would leave
	// every form unscoped with nothing erroring.
	var unscoped int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM forms WHERE account_id=$1 AND workspace_id IS NULL`, account).Scan(&unscoped); err != nil {
		t.Fatalf("count unscoped forms: %v", err)
	}
	if unscoped != 0 {
		t.Fatalf("%d form(s) kept workspace_id IS NULL after the passkey login — the account was linked but "+
			"its forms were not moved (trg_forms_sync_workspace_id does not fire on an account UPDATE)", unscoped)
	}

	// Exactly one workspace and one account. A provision that went through
	// Workspaces.Create would leave the user owning two accounts.
	var workspaces, accounts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE owner_user_id=$1`, user).Scan(&accounts); err != nil {
		t.Fatalf("count accounts: %v", err)
	}
	if workspaces != 1 || accounts != 1 {
		t.Fatalf("after the passkey login: %d workspaces / %d accounts, want 1 / 1 — a second account means the "+
			"forms were left behind on the first one", workspaces, accounts)
	}

	// The session the passkey login minted is live and scoped to that one
	// workspace, which is what the dashboard reads.
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "http://"+passkeyOriginHost+"/v1/workspaces", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, req)
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /v1/workspaces with the passkey session = %d (body %s)", recList.Code, recList.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(recList.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode workspaces: %v (%s)", err, recList.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("the passkey session lists %d workspaces, want 1 — the session is live but the user cannot "+
			"reach his content through it", len(got))
	}
}

// TestPasskeyLoginDoesNotProvisionAFreshUser pins the other half of the boundary
// on the passkey path too, so the ask-first prompt cannot be turned into dead
// code by provisioning everyone who signs in with a passkey: a user with no
// forms is ASKED to create a workspace, never given one.
func TestPasskeyLoginDoesNotProvisionAFreshUser(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, account, _ := legacyLoginFixture(t, ah, 0)

	authn, cose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, user, webauthn.Credential{
		ID:        authn.id,
		PublicKey: cose,
	}); err != nil {
		t.Fatalf("store passkey: %v", err)
	}

	h := passkeyHandlers(t, pool)
	key, challenge, rpID := startPasskeyLogin(t, h, uniqueTestIP())
	body := authn.assertionBody(t, key, rpID, "https://"+passkeyOriginHost, challenge, []byte(user))
	rec := finishPasskeyLogin(t, h, body, uniqueTestIP())
	if rec.Code != http.StatusOK {
		t.Fatalf("passkey login status = %d (body %s)", rec.Code, rec.Body.String())
	}

	var workspaces int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE owner_user_id=$1`, user).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaces != 0 {
		t.Fatalf("a user with NO forms was provisioned %d workspace(s) by the passkey login — he must be ASKED "+
			"to create one (the /workspace-picker first-run flow)", workspaces)
	}
	var wsID *string
	if err := pool.QueryRow(ctx, `SELECT workspace_id FROM accounts WHERE id=$1`, account).Scan(&wsID); err != nil {
		t.Fatalf("read account: %v", err)
	}
	if wsID != nil {
		t.Fatalf("the fresh user's account was linked to workspace %q by the passkey login", *wsID)
	}
}

// ── completeness guard ───────────────────────────────────────────────────────

// legacyProvisionCallName is the function every session issuer must call.
const legacyProvisionCallName = "provisionLegacyWorkspace"

// sessionIssuer is one function in this package that hands out a session.
type sessionIssuer struct {
	name string
	// provisions records whether its own body applies the legacy rule.
	provisions bool
	// where names the source construct that identified it, for failure text.
	where string
}

// findSessionIssuers scans this package's non-test sources and returns every
// function that issues a session, with whether it applies the legacy rule.
//
// A function is an issuer in either of two senses, and both are checked because
// they are the two ways a login path can hand out a session here:
//
//   - it MINTS one — it calls Sessions.CreateSession / CreateRemembered /
//     CreateRememberedWithTTL;
//   - it WRITES the session cookie — an http.SetCookie call whose argument
//     mentions sessionCookie and which is not a clear (an empty Value).
//
// The second sense is what catches a future issuer that assembles the cookie
// itself, as RESTHandlers.startSessionFor does.
func findSessionIssuers(t *testing.T) []sessionIssuer {
	t.Helper()
	dir := "."
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	var out []sessionIssuer
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			src, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := func(n ast.Node) string {
				return string(src[fset.Position(n.Pos()).Offset:fset.Position(n.End()).Offset])
			}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				// The receiver-qualified name keeps the two issuers apart in the
				// failure message, since one is on AuthHandlers and the other on
				// RESTHandlers.
				name := fd.Name.Name
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					name = text(fd.Recv.List[0].Type) + "." + name
				}

				var mints, writes bool
				var why []string
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					switch sel.Sel.Name {
					case "CreateSession", "CreateRemembered", "CreateRememberedWithTTL":
						mints = true
						why = append(why, "mints a session")
					case "SetCookie":
						arg := text(call)
						// Match the identifier, not the const's value: the
						// sources spell it `sessionCookie`, whose value is
						// "jf_session".
						if strings.Contains(arg, "sessionCookie") && !strings.Contains(arg, `Value: ""`) {
							writes = true
							why = append(why, "writes the "+sessionCookie+" cookie")
						}
					}
					return true
				})
				if !mints && !writes {
					continue
				}
				provisions := false
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && id.Name == legacyProvisionCallName {
						provisions = true
					}
					return true
				})
				out = append(out, sessionIssuer{name: name, provisions: provisions, where: strings.Join(why, " + ")})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// TestEverySessionIssuerProvisionsTheLegacyWorkspace is the guard that bites for
// the CLASS of defect behind t_e28e4406, not just the one instance: every
// function in this package that issues a session must apply the legacy-onboarding
// rule.
//
// The original defect was a doc comment asserting an invariant ("this is the one
// function every login path funnels through") that was simply false — the passkey
// path used a second issuer. A test of the passkey handler alone would pin that
// one call site and still let a third login path repeat it. Enumerating the
// issuers makes the invariant checkable, and the doc comment now describes this
// test rather than claiming a property nothing verified.
//
// Neutralise the fix by removing the provisionLegacyWorkspace call from either
// issuer and this test names the offender.
func TestEverySessionIssuerProvisionsTheLegacyWorkspace(t *testing.T) {
	issuers := findSessionIssuers(t)

	// A scanner that matched nothing would pass vacuously, so require the two
	// issuers this package is known to have to be among the findings. The names
	// are receiver-qualified as `*AuthHandlers.startSession`.
	var names []string
	for _, is := range issuers {
		names = append(names, is.name)
	}
	for _, want := range []string{"*AuthHandlers.startSession", "*RESTHandlers.startSessionFor"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("the issuer scan did not find %s — it found [%s]. The guard would otherwise pass "+
				"vacuously, so fix the scan before trusting it", want, strings.Join(names, ", "))
		}
	}

	for _, is := range issuers {
		if !is.provisions {
			t.Errorf("%s %s but does not call %s — a form-owning workspace-less user logging in through this "+
				"path keeps zero workspaces, and the manual create he is then shown cannot attach his forms "+
				"(SoleEmptyUnscopedAccount refuses an account that carries them, so it inserts a second account "+
				"and the forms are stranded for good). Every session issuer must apply the rule; see "+
				"provisionLegacyWorkspace",
				is.name, is.where, legacyProvisionCallName)
		}
	}
}

// TestSessionIssuerScanSeesACookielessIssuer keeps the completeness guard honest:
// it checks the scanner recognises both senses of "issuer" on real source, so a
// future rewrite of the scan cannot quietly narrow to one of them and let a
// cookie-writing issuer through.
func TestSessionIssuerScanSeesACookielessIssuer(t *testing.T) {
	issuers := findSessionIssuers(t)
	var minted, wrote []string
	for _, is := range issuers {
		if strings.Contains(is.where, "mints a session") {
			minted = append(minted, is.name)
		}
		if strings.Contains(is.where, "cookie") {
			wrote = append(wrote, is.name)
		}
	}
	if len(minted) == 0 {
		t.Error("no issuer was identified by minting a session")
	}
	if len(wrote) == 0 {
		t.Error("no issuer was identified by writing the session cookie")
	}

	// Both real issuers write the cookie; at least one of them also mints
	// through the session store. If neither sense ever fires the guard is inert.
	if len(issuers) < 2 {
		t.Fatalf("the scan found %d issuers, want at least the 2 this package has (%s)",
			len(issuers), strings.Join(func() []string {
				var n []string
				for _, is := range issuers {
					n = append(n, fmt.Sprintf("%s: %s", is.name, is.where))
				}
				return n
			}(), "; "))
	}
}
