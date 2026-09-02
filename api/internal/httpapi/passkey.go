// Passkey HTTP handlers: register (authed) + login (anon) + list/delete.
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

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
	wu, err := h.Passkeys.UserFor(r.Context(), uid, prof.Email)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	opts, session, err := wa.BeginRegistration(wu)
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Key == "" {
		writeErr(w, http.StatusBadRequest, "key wajib")
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
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "response tidak sah")
		return
	}
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
	_, err = wa.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (user webauthn.User, err error) {
		u, gerr := h.passkeyLookupUser(r.Context(), string(userHandle))
		if gerr != nil {
			return nil, gerr
		}
		foundUID = u.ID
		return u, nil
	}, *session, parsed)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "passkey tidak sah")
		return
	}
	if foundUID == "" {
		writeErr(w, http.StatusUnauthorized, "passkey tidak sah")
		return
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
