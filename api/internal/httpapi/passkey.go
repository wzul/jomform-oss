// Passkey HTTP handlers: register (authed) + login (anon) + list/delete.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

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

// webAuthnInstance builds the WebAuthn config for the request's origin.
func (h *RESTHandlers) webAuthnInstance(r *http.Request) *webauthn.WebAuthn {
	rpID := "jomform.com"
	if host := r.Host; strings.HasSuffix(host, "jomform.com") && !strings.HasPrefix(host, "jomform.com") {
		// subdomain logins also bind to parent RP ID (id is the same registrable domain)
		rpID = "jomform.com"
	}
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "JomForm",
		RPID:          rpID,
		RPOrigins:     []string{"https://" + r.Host, "https://jomform.com"},
	})
	if err != nil {
		return nil
	}
	return w
}

// passkeyRegisterStart: POST /v1/auth/passkeys/register/start (authed)
// Issue #458: a magic-link session owned by a 2FA user must supply a fresh TOTP
// code (totp_code in the JSON body) before a passkey can be planted. Non-2FA /
// non-magic sessions are unchanged.
func (h *RESTHandlers) passkeyRegisterStart(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	wa := h.webAuthnInstance(r)
	if wa == nil {
		writeErr(w, http.StatusInternalServerError, "webauthn config error")
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
	wa := h.webAuthnInstance(r)
	uid := h.sessionOwnerID(r)
	prof, _ := h.profileUser(r)
	if wa == nil || uid == "" || prof == nil {
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
	wa := h.webAuthnInstance(r)
	if wa == nil {
		writeErr(w, http.StatusInternalServerError, "webauthn config error")
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
	wa := h.webAuthnInstance(r)
	if wa == nil {
		writeErr(w, http.StatusInternalServerError, "webauthn config error")
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
	creds, err := h.Passkeys.ByUser(r.Context(), uid)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]row, 0, len(creds))
	for _, c := range creds {
		out = append(out, row{ID: base64URL(c.ID)})
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
