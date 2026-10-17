// Passkey HTTP handlers: register (authed) + login (anon) + list/delete.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// passkeyRegistrationSelection is the authenticator criteria the registration
// ceremony asks for, and it is NOT optional: passkeyLoginFinish verifies with
// ValidateDiscoverableLogin, so the login ceremony only ever reaches a
// credential that is a client-side discoverable (resident) key. go-webauthn
// applies no default here (protocol.AuthenticatorSelection.IsZero keeps an
// empty selection off the wire entirely), so a registration started without one
// leaves the browser free to mint a NON-resident credential — which it does —
// and that credential is then invisible to every discoverable get() the login
// page performs. The user's only symptom is "Passkey cancelled or not supported
// by this browser", with nothing stored server-side to explain it.
//
// UserVerification stays PREFERRED, not required: requiring it would demand a UV
// flag the assertion path does not ask for, and it would add friction to a
// ceremony that is already gated by the session + the #458 step-up.
var passkeyRegistrationSelection = protocol.AuthenticatorSelection{
	// RequireResidentKey is the deprecated-by-name member browsers still honour;
	// ResidentKey is the Level 2 spelling. Both are set because the resident-key
	// requirement is the difference between a passkey that can sign in and one
	// that cannot, and a client that reads only one of the two must still get it.
	RequireResidentKey: protocol.ResidentKeyRequired(),
	ResidentKey:        protocol.ResidentKeyRequirementRequired,
	UserVerification:   protocol.VerificationPreferred,
}

// ── base64url normalisation ──────────────────────────────────────────────────

// passkeyBinaryFields are the JSON members of a WebAuthn credential response
// that carry base64-encoded binary data, per path into the body. The parser
// decodes each of them with base64.RawURLEncoding (protocol/base64.go), which
// accepts ONLY the URL alphabet — no `+`, no `/`, no padding.
var passkeyBinaryFields = map[string][]string{
	"":         {"id", "rawId"},
	"response": {"authenticatorData", "clientDataJSON", "signature", "userHandle", "attestationObject", "publicKey"},
}

// normalizePasskeyBase64 rewrites every binary field of a credential response
// from STANDARD base64 to base64url, in place.
//
// WHY THIS IS ON THE SERVER, not only in the browser. The card's defect is a
// client one — the shipped pages encoded with `btoa`, which emits `+` and `/`
// — but the failure mode a merchant meets is a server-side 400, and the client
// that produces it is already downloaded: a fix that only ships new JavaScript
// leaves every browser holding the old bundle broken until it re-fetches, and
// the passkey code shipped with no login control at all, so "wait for the new
// bundle" was not a recovery path a user had. Normalising here makes the
// endpoint accept what the deployed clients actually send, and the browser fix
// (lib/webauthn.ts) makes new clients send the correct alphabet in the first
// place. Both halves are guarded separately.
//
// It is safe to accept both alphabets because they are disjoint in exactly the
// characters that differ: standard base64 never contains `-`/`_`, so a value
// that is already base64url is returned unchanged, and the two encodings of a
// given byte string decode to the same bytes. Verification is over the decoded
// bytes, so accepting the standard alphabet cannot admit a payload the URL
// alphabet would have rejected on content — and `id`/`rawId` are rewritten
// identically, so the parser's "ID does not match rawId" check still holds.
//
// A field that is absent, null, or not a string is left exactly as it was, so
// this cannot invent data for a malformed body: it will still fail the parser,
// which is the correct outcome.
func normalizePasskeyBase64(raw []byte) []byte {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return raw
	}
	for path, fields := range passkeyBinaryFields {
		if path == "" {
			rewriteB64Fields(top, fields)
			continue
		}
		sub, ok := top[path]
		if !ok {
			continue
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(sub, &nested); err != nil {
			continue
		}
		rewriteB64Fields(nested, fields)
		if out, err := json.Marshal(nested); err == nil {
			top[path] = out
		}
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}

// rewriteB64Fields normalises the named string members of a decoded JSON object.
func rewriteB64Fields(obj map[string]json.RawMessage, fields []string) {
	for _, name := range fields {
		v, ok := obj[name]
		if !ok {
			continue
		}
		// `null` is not a string but json.Unmarshal(null, *string) SUCCEEDS
		// leaving the zero value, so a bare decode would rewrite a null
		// userHandle into "". A null member is meaningful here (an assertion
		// carries no userHandle), so it is left byte-identical.
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			continue // a number, an object, an array — not ours to rewrite
		}
		if out, err := json.Marshal(normalizeB64(s)); err == nil {
			obj[name] = out
		}
	}
}

// normalizeB64 maps standard base64 onto the URL alphabet and strips padding.
// Idempotent: a value already in the URL alphabet has no `+`/`/` to map.
func normalizeB64(s string) string {
	return strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(s), "=")
}

// ── base64url normalisation end ──────────────────────────────────────────────

// ── relying party identity ───────────────────────────────────────────────────

// passkeyRelyingParty is the WebAuthn identity a ceremony runs under: the RP ID
// (a bare domain — no scheme, no port) and the origins whose ceremony responses
// the server will accept.
type passkeyRelyingParty struct {
	rpID    string
	origins []string
}

// errPasskeyOriginOutsideApp refuses a passkey request that did not arrive on
// the deployment's own domain. It is a sentinel so the handler can answer 403
// (the caller's origin is not this deployment) rather than 500 (this server's
// own configuration is broken) — two conditions that look alike from here.
var errPasskeyOriginOutsideApp = errors.New("passkey tidak tersedia pada hos ini (bukan domain aplikasi)")

// passkeyBrowserHost returns the host the BROWSER is on for this request.
//
// WHY THIS IS NOT r.Host. The dashboard's own /api/* proxy
// (web/src/app/api/[...path]/route.ts) builds a FRESH header set for its hop to
// the API, so the request the API receives carries the proxy's own target as
// `Host` — `api:8080` in the deployment (JOMFORM_API_URL) — and the browser's
// host is gone. Gating the ceremony on r.Host therefore refused the PRODUCT'S
// OWN passkey endpoints: v1.21.52 answered 403 `request.forbidden` to
// POST https://jomform.com/api/auth/passkeys/login/start, the path
// web/src/app/{login,profile}/page.tsx actually call,
// while https://jomform.com/v1/auth/passkeys/login/start — direct to the API,
// the URL no browser uses — kept working and hid the regression. The proxy now
// forwards the browser's host as X-Forwarded-Host, and that is the value the
// origin guard is applied to.
//
// The forwarded value is not trusted on its own: passkeyRPIdentity still
// requires it to be the app's own domain or a subdomain of it, so a caller can
// only ever select among the hosts this deployment already serves. The RP ID and
// the base origin come from APP_BASE_URL either way — a forged header cannot
// move the ceremony off the deployment's own domain.
func passkeyBrowserHost(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		// A proxy chain may append; the left-most value is the original host.
		if h := strings.TrimSpace(strings.Split(v, ",")[0]); h != "" {
			return h
		}
	}
	return r.Host
}

// passkeyRPIdentity derives the RP ID and the accepted origins from the app's
// configured base URL (APP_BASE_URL) and the host the request arrived on.
//
// WHY THE RP ID IS NOT A LITERAL. WebAuthn binds a ceremony to the page's
// ORIGIN: the RP ID must be EQUAL to the origin's effective domain or a
// registrable domain suffix of it, and the BROWSER enforces that itself with a
// SecurityError before an authenticator is ever reached (WebAuthn Level 2
// §5.1.3 step 8 — "given a Relying Party whose origin is
// https://login.example.com:1337, the following RP IDs are valid:
// login.example.com (default) and example.com"). This function used to answer
// "jomform.com" for every request host, inside an `if` whose two branches
// assigned the same literal — so it read like a subdomain rule while the value
// was constant — which meant a ceremony could only ever complete on a
// *.jomform.com origin. The staging deployment (APP_BASE_URL=
// https://jomform-staging.example.com) was served an rpId its browser
// refused outright, and the user's only symptom was "Passkey cancelled or not
// supported by this browser" (card t_bca19fe1).
//
// The RP ID is therefore the app base URL's HOST: that origin's own effective
// domain, i.e. the spec's DEFAULT value. It stays valid for the app host and for
// every subdomain of it (workspace and product subdomains such as
// mcp.jomform.com), and it is deliberately NOT the registrable domain the same
// rule would also permit ("example.com" for the staging host) — a public
// suffix that broad would scope the credentials to every unrelated service on
// that host.
//
// WHY THE REQUEST HOST IS READ, AND ONLY AS A REFUSAL OR AN INTERNAL HOP. The
// accepted origins must cover the host the browser is actually on, because the
// session is shared across the app's subdomains (SharedCookieDomain) and the
// dashboard — hence the ceremony — can run on mcp.jomform.com as well as
// jomform.com. Listing the request's own origin is what keeps that working, but
// it MUST be gated: the pre-fix code appended "https://" + r.Host
// unconditionally, so the origin allow-list was whatever a caller put in the Host
// header, and the only thing standing between a foreign origin and a ceremony
// was the browser's own SecurityError. A host outside the app's domain is
// refused here instead, on a LABEL boundary — "eviljomform.com" and
// "jomform.com.evil.example" are both refused, not just a host with an unrelated
// top-level domain. The one host that is accepted without being in the app's
// domain is the deployment's own proxy hop, and it is accepted as the APP host
// (never as itself): its own origin is never added to RPOrigins. See
// passkeyInternalHopHost.
func passkeyRPIdentity(appBaseURL, requestHost string) (passkeyRelyingParty, error) {
	u, err := url.Parse(strings.TrimSpace(appBaseURL))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return passkeyRelyingParty{}, fmt.Errorf("APP_BASE_URL %q tidak sah — RP ID passkey tidak dapat ditentukan", appBaseURL)
	}
	appHost := strings.ToLower(u.Hostname())
	if appHost == "" {
		return passkeyRelyingParty{}, fmt.Errorf("APP_BASE_URL %q tidak mempunyai hos", appBaseURL)
	}
	// A base URL spelled with a leading "www." is the same site, and the RP ID
	// must cover the bare host as well: an RP ID may not be a SUBdomain of the
	// origin, so a deployment configured at https://www.jomform.com that kept
	// "www." here would refuse every ceremony run from https://jomform.com —
	// where this app is served too (deploy/vps/traefik-jomform.yml routes both).
	// Trimming to the site's own domain accepts both, and is still the narrowest
	// value that does: the app host is a subdomain of the result, so the guard
	// below keeps accepting www.<host> requests as well.
	appHost = strings.TrimPrefix(appHost, "www.")

	reqHost := passkeyHostname(requestHost)
	switch {
	case reqHost != "" && (reqHost == appHost || strings.HasSuffix(reqHost, "."+appHost)):
		// An origin is scheme + host, and the scheme comes from the CONFIGURED
		// base URL rather than off the request: TLS ends at the reverse proxy in
		// front of the API, so r.TLS is nil and the request itself is plain HTTP
		// even when the browser's origin is https. The request's own host is
		// added as a second origin so a ceremony on a subdomain is accepted with
		// the origin it really has — the port included, since RPOrigins matches
		// an origin exactly.
		appOrigin := passkeyOriginFor(u.Scheme, u.Host)
		origins := []string{appOrigin}
		if reqOrigin := passkeyOriginFor(u.Scheme, requestHost); reqOrigin != appOrigin {
			origins = append(origins, reqOrigin)
		}
		return passkeyRelyingParty{rpID: appHost, origins: origins}, nil
	case passkeyInternalHopHost(reqHost):
		// THE PROXY HOP. The deployment's own web proxy dials the API on an
		// internal name (JOMFORM_API_URL = http://api:8080), so this request IS
		// the app's — and it is treated as the app host, not as its own origin:
		// the ceremony handed back is the app's, RP ID and RPOrigins both derived
		// from APP_BASE_URL. Adding the internal host as an origin instead would
		// put "https://api:8080" in RPOrigins, which no browser origin can ever
		// match, and would refuse the app's own host — the v1.21.52 regression.
		return passkeyRelyingParty{rpID: appHost, origins: []string{passkeyOriginFor(u.Scheme, u.Host)}}, nil
	default:
		return passkeyRelyingParty{}, errPasskeyOriginOutsideApp
	}
}

// passkeyInternalHopHost reports whether host (already port-stripped and
// lower-cased by passkeyHostname) is an address the deployment's own web proxy
// dials the API on — a name a BROWSER origin can never be.
//
// WHAT QUALIFIES, and why each is safe to read as "this deployment":
//
//   - a bare label with no dot: "api" (the deployment in this repo's compose),
//     "jomform-api-1" (the same container under its compose/swarm name), "web".
//     A single-label name cannot be a public DNS name, so no page can ever have
//     an origin on one, and the browser path is what this guard is about.
//   - loopback / RFC1918 private / link-local / unspecified IP literals: the
//     same case for a deployment configured with an address instead of a name
//     (http://172.17.0.5:8080).
//
// WHAT DOES NOT QUALIFY: anything carrying a dot that is not the app's own
// domain — "eviljomform.com", "jomform.com.evil.example", "attacker.example".
// The refusal this guard exists for is unchanged.
//
// THE TRADE-OFF, stated because it is deliberate. Admitting such a host does NOT
// admit its origin: the internal-hop arm above returns the APP's ceremony, so a
// caller who reaches the API with `Host: api` is handed options for
// https://jomform.com — an RP ID its own page can never use, because the browser
// raises a SecurityError for any RP ID that is not equal to (or a registrable
// domain suffix of) the page's own origin. Nothing foreign enters RPOrigins.
// The alternative — recognising the hop only by a header the proxy sets — leaves
// the endpoint refusing the product's own path whenever the api image and the
// web image are not the same revision (a rolling deploy, or an api-only bump),
// which is precisely how v1.21.52 took passkey sign-in off prod.
func passkeyInternalHopHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	return !strings.Contains(host, ".")
}

// passkeyOriginFor serialises scheme + host as an origin: the host lower-cased
// (an origin's host is case-insensitive, and a browser serialises it lower-cased
// — the library's own comparison folds case, but the recorded value should not
// depend on how the caller spelled the Host header) with the port preserved
// when there is one.
func passkeyOriginFor(scheme, host string) string {
	host = strings.TrimSpace(host)
	if h, p, err := net.SplitHostPort(host); err == nil {
		if strings.Contains(h, ":") { // IPv6 literal — keep the brackets
			return scheme + "://[" + strings.ToLower(h) + "]:" + p
		}
		return scheme + "://" + strings.ToLower(h) + ":" + p
	}
	return scheme + "://" + strings.ToLower(host)
}

// passkeyHostname strips the port from a Host header value, so "jomform.com:8080"
// still matches the deployment's domain, and lower-cases it: host names are
// case-insensitive, and the library's own origin comparison folds case too.
func passkeyHostname(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return strings.ToLower(strings.TrimSpace(h))
	}
	return strings.ToLower(strings.TrimSpace(host))
}

// webAuthnInstance builds the WebAuthn config for the deployment the request
// arrived at. It returns errPasskeyOriginOutsideApp when the request did not
// come from the app's domain (or from the deployment's own proxy hop — see
// passkeyInternalHopHost), and a wrapped error when APP_BASE_URL cannot supply
// the RP ID (an IP address is not a valid RP ID — protocol.ValidateRPID).
func (h *RESTHandlers) webAuthnInstance(r *http.Request) (*webauthn.WebAuthn, error) {
	rp, err := passkeyRPIdentity(h.BaseURL, passkeyBrowserHost(r))
	if err != nil {
		return nil, err
	}
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "JomForm",
		RPID:          rp.rpID,
		RPOrigins:     rp.origins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn config error: %w", err)
	}
	return w, nil
}

// writeWebAuthnUnavailable maps webAuthnInstance's error onto the wire: a host
// outside the app's domain is the caller's own 403, anything else is this
// server's misconfiguration. The two are distinguishable so a deployment that
// cannot supply an RP ID is diagnosable, but neither answers with a body that
// implies a ceremony could have run.
func writeWebAuthnUnavailable(w http.ResponseWriter, err error) {
	if errors.Is(err, errPasskeyOriginOutsideApp) {
		writeErr(w, http.StatusForbidden, errPasskeyOriginOutsideApp.Error())
		return
	}
	writeErr(w, http.StatusInternalServerError, "webauthn config error")
}

// passkeyRegisterStart: POST /v1/auth/passkeys/register/start (authed)
// Issue #458: a magic-link session owned by a 2FA user must supply a fresh TOTP
// code (totp_code in the JSON body) before a passkey can be planted. Non-2FA /
// non-magic sessions are unchanged.
func (h *RESTHandlers) passkeyRegisterStart(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	wa, err := h.webAuthnInstance(r)
	if err != nil {
		writeWebAuthnUnavailable(w, err)
		return
	}
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	prof, err := h.profileUser(r)
	if err != nil || prof == nil {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	var in struct {
		TOTPCode string `json:"totp_code"`
	}
	// The body is optional for the common case, so a decode error is not fatal.
	_ = decodeJSON(r, &in)
	if !h.gateMagicStepUpREST(w, r, actx, prof, in.TOTPCode) {
		return
	}
	wu, err := h.Passkeys.UserFor(r.Context(), uid, prof.Email)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// The selection is mandatory here, not cosmetic: the login ceremony is
	// discoverable-only (see passkeyLoginFinish), so a credential minted without
	// the resident-key requirement can never be used to sign in. See
	// passkeyRegistrationSelection.
	opts, session, err := wa.BeginRegistration(wu,
		webauthn.WithAuthenticatorSelection(passkeyRegistrationSelection))
	if err != nil {
		respondErr(w, r, err)
		return
	}
	key := randTokenHex(16)
	if err := h.Passkeys.PutChallenge(r.Context(), key, string(mustJSON(session)), "register"); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts, "key": key})
}

// passkeyRegisterFinish: POST /v1/auth/passkeys/register/finish (authed)
// Issue #458: the step-up gate also applies here (defense in depth). In the
// normal flow the TOTP code was already supplied at /start, which minted a
// short-lived grant that this call inherits; a direct /finish without a grant
// must carry its own totp_code.
func (h *RESTHandlers) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	wa, err := h.webAuthnInstance(r)
	if err != nil {
		writeWebAuthnUnavailable(w, err)
		return
	}
	uid := h.sessionOwnerID(r)
	prof, _ := h.profileUser(r)
	if uid == "" || prof == nil {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	// The WebAuthn parsers below re-read the body, so read it exactly once
	// into memory: decode the session key from the buffer and hand the same
	// bytes to the parser via a fresh reader. (decodeJSON consumes+closes
	// r.Body, which would leave the parser an empty body.)
	//
	// normalizePasskeyBase64 accepts the standard base64 alphabet as well as
	// base64url, so a client still running a bundle built before lib/webauthn.ts
	// can register. See its comment for why that belongs on the server.
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
	raw = normalizePasskeyBase64(raw)
	var in struct {
		Key string `json:"key"`
		// Issue #458: optional step-up code for a magic + 2FA session.
		TOTPCode string `json:"totp_code"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Key == "" {
		writeErr(w, http.StatusBadRequest, "key wajib")
		return
	}
	if !h.gateMagicStepUpREST(w, r, actx, prof, in.TOTPCode) {
		return
	}
	sessRaw, err := h.Passkeys.TakeChallenge(r.Context(), in.Key, "register")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "sesi luput — cuba lagi")
		return
	}
	session := &webauthn.SessionData{}
	if err := json.Unmarshal([]byte(sessRaw), session); err != nil {
		writeErr(w, http.StatusBadRequest, "sesi rosak")
		return
	}
	wu, err := h.Passkeys.UserFor(r.Context(), uid, prof.Email)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if err != nil {
		respondErr(w, r, err)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(raw))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
	cred, err := wa.CreateCredential(wu, *session, parsed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "gagal sahkan passkey")
		return
	}
	if err := h.Passkeys.Save(r.Context(), uid, *cred); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

// passkeyLoginStart: POST /v1/auth/passkeys/login/start (anon, email optional)
func (h *RESTHandlers) passkeyLoginStart(w http.ResponseWriter, r *http.Request) {
	wa, err := h.webAuthnInstance(r)
	if err != nil {
		writeWebAuthnUnavailable(w, err)
		return
	}
	// discoverable-credential (resident key) login
	opts, session, err := wa.BeginDiscoverableLogin()
	if err != nil {
		respondErr(w, r, err)
		return
	}
	key := randTokenHex(16)
	if err := h.Passkeys.PutChallenge(r.Context(), key, string(mustJSON(session)), "login"); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": opts, "key": key})
}

// passkeyLoginFinish: POST /v1/auth/passkeys/login/finish — creates a session.
func (h *RESTHandlers) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	wa, err := h.webAuthnInstance(r)
	if err != nil {
		writeWebAuthnUnavailable(w, err)
		return
	}
	// The WebAuthn parser below re-reads the body, so read it exactly once
	// into memory: decode the session key from the buffer and hand the same
	// bytes to the parser via a fresh reader. (decodeJSON consumes+closes
	// r.Body, which would leave the parser an empty body.)
	//
	// normalizePasskeyBase64 accepts the standard base64 alphabet as well as
	// base64url, so a signed-in client running a bundle built before
	// lib/webauthn.ts is not locked out of passkey sign-in. See its comment.
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
	raw = normalizePasskeyBase64(raw)
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Key == "" {
		writeErr(w, http.StatusBadRequest, "key wajib")
		return
	}
	sessRaw, err := h.Passkeys.TakeChallenge(r.Context(), in.Key, "login")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "sesi luput — cuba lagi")
		return
	}
	session := &webauthn.SessionData{}
	if err := json.Unmarshal([]byte(sessRaw), session); err != nil {
		writeErr(w, http.StatusBadRequest, "sesi rosak")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(raw))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
	var foundUID string
	var repairedLegacyFlags bool
	cred, err := wa.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (user webauthn.User, err error) {
		u, gerr := h.passkeyLookupUser(r.Context(), string(userHandle))
		if gerr != nil {
			return nil, gerr
		}
		// A row written before the store persisted flags holds the sentinel 0,
		// which the credential could never have reported itself, so adopt the
		// assertion's flags for it before the library compares BackupEligible.
		// Without this every synced passkey registered before the fix stays
		// unusable; with it, the comparison is made against a real value and
		// the row is persisted as recorded below. See
		// dbstore.PasskeyUser.RestoreUnrecordedFlags.
		repairedLegacyFlags = u.RestoreUnrecordedFlags(rawID, parsed.Response.AuthenticatorData.Flags)
		foundUID = u.ID
		return u, nil
	}, *session, parsed)
	if err != nil {
		// The verifier's own error is the ONLY place the server-side reason
		// exists: the client is told a generic 401, and until now the API log
		// carried nothing either, so a rejected real-authenticator login (card
		// t_0a2d413c: "Backup Eligible flag inconsistency") was undiagnosable
		// from prod. protocol.Error renders only its Details through Error();
		// its Type, DevInfo and wrapped cause are logged separately or they are
		// lost.
		attrs := []any{"err", err}
		var perr *protocol.Error
		if errors.As(err, &perr) {
			attrs = append(attrs, "error_type", perr.Type, "error_debug", perr.DevInfo)
			if perr.Err != nil {
				attrs = append(attrs, "error_cause", perr.Err)
			}
		}
		slog.Warn("passkey_login_rejected", attrs...)
		writeErr(w, http.StatusUnauthorized, "passkey tidak sah")
		return
	}
	if foundUID == "" {
		slog.Warn("passkey_login_rejected", "reason", "verify reported no user")
		writeErr(w, http.StatusUnauthorized, "passkey tidak sah")
		return
	}
	if repairedLegacyFlags {
		slog.Info("passkey_login_legacy_flags_adopted")
	}
	// Write back what the assertion advanced (counter + flags). The ceremony
	// has already verified, so a failure here is logged rather than turned into
	// a 401 the user cannot act on; it must still be visible, because the flags
	// it stores are what the next ceremony's BackupEligible check compares
	// against.
	if err := h.Passkeys.RecordAssertion(r.Context(), foundUID, *cred); err != nil {
		slog.Warn("passkey_login_state_writeback_failed", "err", err)
	}
	if err := h.startSessionFor(w, r, foundUID); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_in"})
}

// passkeyList: GET /v1/auth/passkeys (authed)
func (h *RESTHandlers) passkeyList(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	creds, err := h.Passkeys.ListCreatedAt(r.Context(), uid)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	// `created_at` is the row's real timestamp, formatted here rather than left
	// to the encoder: the column is timestamptz, so its representation in a
	// public response is the endpoint's contract, and pinning it to RFC 3339 in
	// UTC keeps it stable whatever the server's location or the driver's
	// default. Before card t_96c7c227 the field was declared and never set, so
	// every row carried "" — the store read no timestamp and this loop had
	// nothing to put in it.
	type row struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]row, 0, len(creds))
	for _, c := range creds {
		out = append(out, row{ID: base64URL(c.ID), CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, out)
}

// passkeyDelete: DELETE /v1/auth/passkeys/{id} (authed)
func (h *RESTHandlers) passkeyDelete(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	uid := h.sessionOwnerID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	id := r.PathValue("id")
	raw, err := base64URLDecode(id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return
	}
	if err := h.Passkeys.Delete(r.Context(), uid, raw); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
