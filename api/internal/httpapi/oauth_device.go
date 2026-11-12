// RFC 8628 device authorization grant — HEADLESS MCP onboarding
// (card t_9a6cc85f).
//
//	POST /oauth/device_authorization — the harness asks for a code pair.
//	POST /oauth/token  (grant_type=urn:ietf:params:oauth:grant-type:device_code)
//	                                — the harness polls until a person
//	                                   approved the request.
//
// WHY THIS EXISTS. The authorization-code flow needs a browser exactly once per
// client, because that is where the human consent screen lives. An AI harness
// has no browser. Here the consent screen is an EMAIL to the account's
// registered address: it names the client, what that client will be allowed to
// do, where the request came from, and the user_code the harness is waiting for.
// The person approves it on jomform.com (the mail's link), and the harness — which
// never had a browser — picks up a token pair on its next poll.
//
// WHAT THIS FILE DOES NOT DO. It does not change /oauth/authorize, the
// authorization_code exchange, or the API-key scope vocabulary. The device
// grant is additive; a client that can drive a browser keeps using it.
package httpapi

import (
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailtext"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

const (
	// DeviceCodeTTL is the 10-minute life RFC 8628 §3.2's `expires_in` reports.
	DeviceCodeTTL = 10 * time.Minute
	// DevicePollIntervalS is the advertised `interval`, in seconds.
	DevicePollIntervalS = 5
	// deviceSlowDownPenaltyS is added to the row's interval on a too-fast poll
	// (RFC 8628 §3.5) and persists for the rest of the authorisation.
	deviceSlowDownPenaltyS = 5
	// deviceRateLimitPerHour bounds BOTH the per-account and the per-IP rate.
	// Three onboarding attempts an hour is more than a person needs and far less
	// than a mail bomb needs to be useful.
	deviceRateLimitPerHour = 3
	// deviceUserCodeAlphabet omits every character a human misreads (0/O, 1/I/L).
	// The code is read from an email and typed into a dashboard, so ambiguity is
	// a real cost.
	deviceUserCodeAlphabet = "BCDFGHJKLMNPQRSTVWXYZ23456789"
	deviceUserCodeLen      = 8
)

// deviceAuthResponse is the RFC 8628 §3.2 response. Field names are the RFC's.
type deviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// newUserCode mints the human-typed half of the pair: 8 characters from the
// unambiguous alphabet, shown as XXXX-XXXX. ~2^40 possibilities, useless to
// anyone who was not mailed it.
func newUserCode() string {
	b := make([]byte, deviceUserCodeLen)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not a condition this process can continue
		// with; fall back to a value that cannot collide into a real code.
		return ""
	}
	out := make([]byte, 0, deviceUserCodeLen+1)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, deviceUserCodeAlphabet[int(v)%len(deviceUserCodeAlphabet)])
	}
	return string(out)
}

// normalizeUserCode canonicalises what a person typed: upper-case, and dashes /
// spaces stripped before re-inserting the canonical dash. "bcdf ghjk" and
// "BCDF-GHJK" hash to the same row.
func normalizeUserCode(in string) string {
	up := strings.ToUpper(strings.TrimSpace(in))
	up = strings.NewReplacer("-", "", " ", "", "\t", "").Replace(up)
	if len(up) != deviceUserCodeLen {
		return ""
	}
	return up[:4] + "-" + up[4:]
}

// verificationURI is the dashboard page that renders the consent card. It is a
// real page on the web app, not a stub: the mail's link lands on it with the
// code already filled in.
func (o *OAuthHandlers) verificationURI(userCode string) (base, complete string) {
	base = strings.TrimRight(o.BaseURL, "/") + "/profile?tab=mcp"
	complete = base + "&user_code=" + url.QueryEscape(userCode)
	return base, complete
}

// isSuperAdmin reports whether an address is on the platform-admin allowlist.
// Nil allowlist (self-hosted without SUPER_ADMIN_EMAILS) means "no admins".
func (o *OAuthHandlers) isSuperAdmin(email string) bool {
	return o.SuperAdmin != nil && email != "" && o.SuperAdmin(email)
}

// deviceAuthorization implements POST /oauth/device_authorization.
//
// ENUMERATION SAFETY. The request carries `login_hint` (RFC 8628 §3.1) — the
// address the user believes the code should go to. Whether or not that address
// belongs to an account MUST NOT be observable: an attacker who can tell "this
// email is registered" from "this email is not" has an account-enumeration
// oracle, and this endpoint is open. So the success response is built from
// random values plus constants only, and the account lookup changes nothing
// about it — the same status, the same key set, the same lengths, the same
// expires_in/interval. A mismatch simply means no mail goes out and the row is
// left unbound (it can still be approved by a signed-in person typing the code
// on the dashboard, which is RFC 8628's own "enter the code at the verification
// URI" path).
func (o *OAuthHandlers) deviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if o.Store == nil {
		oauthErr(w, http.StatusServiceUnavailable, "temporarily_unavailable", "device authorization is not active yet")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "invalid form encoding")
		return
	}
	clientID := strings.TrimSpace(r.Form.Get("client_id"))
	if clientID == "" {
		oauthErr(w, http.StatusBadRequest, "invalid_client", "client_id is required")
		return
	}
	client, err := o.Store.ClientByID(r.Context(), clientID)
	if err != nil {
		// Unknown client: no row can reference it, and a token issued against a
		// phantom client would be unrevocable by the dashboard. Refusing here
		// leaks nothing about USERS.
		oauthErr(w, http.StatusBadRequest, "invalid_client", "client_id not identified")
		return
	}
	if !resourceAllowed(r.Form.Get("resource"), r.Host) {
		oauthErr(w, http.StatusBadRequest, "invalid_target",
			"the resource parameter does not point to this MCP endpoint")
		return
	}
	// The scope ladder's entry point: absent scope resolves to READ here (not
	// readwrite, which is what the browser flow defaults to). A requested value
	// that this server does not advertise is refused rather than narrowed.
	scope, ok := oauthmeta.DeviceScopes(r.Form.Get("scope"))
	if !ok {
		oauthErr(w, http.StatusBadRequest, "invalid_scope",
			"unsupported scope (use 'read' or 'readwrite')")
		return
	}

	// Per-IP first: it is the only limit that can answer 429, because it does not
	// depend on whether the address resolved to an account.
	ip := middleware.ClientIP(r)
	since := time.Now().UTC().Add(-time.Hour)
	if ip != "" {
		if n, cerr := o.Store.CountDeviceCodesByIP(r.Context(), ip, since); cerr == nil && n >= deviceRateLimitPerHour {
			oauthErr(w, http.StatusTooManyRequests, "temporarily_unavailable",
				"too many device authorisations from this address; try again later")
			return
		}
	}

	// Resolve the account behind login_hint. Any failure — no hint, unknown
	// address, store error — collapses to "unbound"; nothing about it escapes.
	var user *dbstore.User
	if hint := strings.TrimSpace(strings.ToLower(r.Form.Get("login_hint"))); hint != "" && o.Users != nil {
		if u, uerr := o.Users.ByEmail(r.Context(), hint); uerr == nil {
			user = u
		}
	}

	// THE MAIL IS OPTIONAL, THE RESPONSE IS NOT. send decides whether a code goes
	// out; nothing below branches on it once the row exists.
	send := user != nil
	if send && o.isSuperAdmin(user.Email) {
		// An emailed code must not be the only thing between an attacker and a
		// platform-admin token, and this deployment has no second factor on this
		// path, so device onboarding is simply not offered for those accounts.
		send = false
	}
	if send {
		// Per-account limit: exceeded → no mail this hour, identical response.
		if n, cerr := o.Store.CountDeviceCodesByUser(r.Context(), user.ID, since); cerr == nil && n >= deviceRateLimitPerHour {
			send = false
		}
	}

	accountID := ""
	if user != nil && o.Accounts != nil {
		if accs, aerr := o.Accounts.ByOwner(r.Context(), user.ID); aerr == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}

	deviceCode := newOAuthToken()
	userCode := newUserCode()
	if userCode == "" {
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not generate a device code")
		return
	}
	expiresAt := time.Now().UTC().Add(o.deviceTTL())
	row := &dbstore.OAuthDeviceCode{
		DeviceHash: hashToken(deviceCode), UserCodeHash: hashToken(userCode),
		ClientID: clientID, Scope: scope, IP: ip, UserAgent: r.UserAgent(),
		ExpiresAt: expiresAt, IntervalS: DevicePollIntervalS, CreatedAt: time.Now().UTC(),
	}
	// The bind is recorded ONLY when a mail will go out: an unbound row is the
	// "somebody may type this code" state, and binding a row we never mailed
	// would let a stale device_code from an unknown address claim an account.
	if send {
		row.UserID, row.AccountID = user.ID, accountID
	}
	if err := o.Store.CreateDeviceCode(r.Context(), row); err != nil {
		oauthErr(w, http.StatusInternalServerError, "server_error", "failed to store the device code",
			"err", err, "client_id", clientID)
		return
	}
	if send {
		// The city comes from middleware.CloudflareCity, which believes the
		// edge header only when the request demonstrably arrived through the
		// Cloudflare edge (see the RealIP middleware).
		o.deliverDeviceCode(user, client, userCode, scope, ip, middleware.CloudflareCity(r))
	}

	vb, vc := o.verificationURI(userCode)
	writeJSON(w, http.StatusOK, deviceAuthResponse{
		DeviceCode: deviceCode, UserCode: userCode,
		VerificationURI: vb, VerificationURIComplete: vc,
		ExpiresIn: int(o.deviceTTL().Seconds()), Interval: DevicePollIntervalS,
	})
}

// deviceTTL returns the authorization lifetime (10 min unless a test narrowed it).
func (o *OAuthHandlers) deviceTTL() time.Duration {
	if o.DeviceTTL > 0 {
		return o.DeviceTTL
	}
	return DeviceCodeTTL
}

// deliverDeviceCode renders + sends the consent mail. It never touches the HTTP
// response: a mail failure must not turn into a different answer (that would be
// an enumeration oracle again), and the harness is polling regardless.
//
// Production sends OFF the request goroutine so that "address exists" and
// "address does not" take the same wall-clock time. Tests flip
// SendDeviceMailAsync off so the send is observable without a race.
func (o *OAuthHandlers) deliverDeviceCode(user *dbstore.User, client *dbstore.OAuthClient, userCode string, scope []string, ip, city string) {
	if o.Mailer == nil || user == nil {
		slog.Warn("device_mail_skipped", "reason", "no mailer or no user")
		return
	}
	lang := emailtext.NormalizeLang(user.Lang)
	clientName := strings.TrimSpace(client.Name)
	if clientName == "" {
		clientName = emailtext.Phrase(lang, emailtext.PhraseUnnamedClient)
	}
	where := strings.TrimSpace(city)
	if where == "" {
		where = emailtext.Phrase(lang, emailtext.PhraseUnknownLocation)
	}
	_, complete := o.verificationURI(userCode)
	msg := emailtext.Text(lang, emailtext.DeviceAuthorization, map[string]string{
		"client": clientName,
		"scope":  deviceScopeProse(lang, scope),
		"where":  where,
		"ip":     ip,
		"time":   time.Now().UTC().Format("2006-01-02 15:04 MST"),
		"code":   userCode,
		"link":   complete,
	})
	send := func() {
		if err := o.Mailer.Send(user.Email, msg.Subject, msg.Body, msg.Lang); err != nil {
			// Logged WITHOUT the address: an onboarding failure rate is an
			// operational signal, the recipient is PII.
			slog.Error("device_mail_failed", "err", err, "client_id", client.ClientID)
		}
	}
	if o.SendDeviceMailAsync {
		go send()
		return
	}
	send()
}

// deviceScopeProse turns the granted scope set into the plain words the consent
// mail shows. readwrite wins when both are present (it is the wider one).
func deviceScopeProse(lang string, scope []string) string {
	for _, s := range scope {
		if s == oauthmeta.ScopeReadWrite {
			return emailtext.Phrase(lang, emailtext.PhraseScopeReadWrite)
		}
	}
	return emailtext.Phrase(lang, emailtext.PhraseScopeRead)
}

// ─── POST /oauth/token, device_code grant ────────────────────────────────────

// tokenDevice implements the device_code grant: one poll of an authorisation.
//
// The RFC's error vocabulary (§3.5) is answered verbatim, and each code means
// what the RFC says it means:
//
//	authorization_pending — the person has not decided yet; keep polling
//	slow_down             — polled faster than `interval`; the interval has
//	                        been extended by 5s and the client must honour it
//	access_denied         — the person denied it; stop
//	expired_token         — past expires_in; stop and start over
//	invalid_grant         — unknown or already-consumed device_code
func (o *OAuthHandlers) tokenDevice(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.Form.Get("device_code"))
	if raw == "" {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "device_code is required")
		return
	}
	d, err := o.Store.PollDeviceCode(r.Context(), hashToken(raw), strings.TrimSpace(r.Form.Get("client_id")))
	switch {
	case errors.Is(err, dbstore.ErrDevicePending):
		oauthErr(w, http.StatusBadRequest, "authorization_pending",
			"the user has not approved this request yet")
		return
	case errors.Is(err, dbstore.ErrDeviceSlowDown):
		oauthErr(w, http.StatusBadRequest, "slow_down",
			"polled faster than the advertised interval")
		return
	case errors.Is(err, dbstore.ErrDeviceDenied):
		oauthErr(w, http.StatusBadRequest, "access_denied", "the user denied this request")
		return
	case errors.Is(err, dbstore.ErrDeviceExpired):
		oauthErr(w, http.StatusBadRequest, "expired_token", "the device code has expired")
		return
	case err != nil:
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "the device code is invalid or has already been used")
		return
	}
	if d.UserID == "" {
		// An approved row always carries the approver. Defensive: never mint a
		// token whose principal is blank.
		oauthErr(w, http.StatusBadRequest, "access_denied", "the user denied this request")
		return
	}
	o.issueTokenPair(w, r, d.ClientID, d.UserID, d.AccountID, d.Scope)
}

// ─── the dashboard's consent card ────────────────────────────────────────────
//
// These two handlers are the "verification URI" half of RFC 8628: the person is
// already signed in to jomform.com (the mail's link sent them there), so the
// card can show exactly what the harness asked for and take approve/deny. They
// live on AuthHandlers because they need the browser session; the OAuth
// handler set has no notion of one.

// deviceScopeInvalidMessage is the wire wording for a refused access level on
// the consent card. It is a named constant because errcode.go's table is keyed
// on this exact string (see errCodeByMessage): the refusal carries the machine
// code device.scopeInvalid so a dashboard can translate it instead of printing
// this prose.
const deviceScopeInvalidMessage = "the access level must be 'read' or 'readwrite'"

// deviceConsentOut is what the consent card renders.
type deviceConsentOut struct {
	UserCode  string   `json:"user_code"`
	ClientID  string   `json:"client_id"`
	Client    string   `json:"client"`
	Scopes    []string `json:"scopes"`
	Scope     string   `json:"scope_prose"`
	ExpiresAt string   `json:"expires_at"`
	State     string   `json:"state"` // pending | approved | denied | expired | used
	// Choices is the set of scopes the PERSON may approve this request as, in
	// the order the card should show them. It is oauthmeta.ScopesSupported() —
	// the same list the server advertises and can mint — so a card cannot offer
	// a value this deployment would then refuse, and cannot drift from the
	// server's vocabulary. The card renders one radio per entry (card
	// t_cf45b84c); `scopes` above is what the CLIENT asked for, which is only
	// the default the card opens on.
	Choices []string `json:"choices"`
}

// getDeviceConsent resolves a user_code for the signed-in person, for display.
func (a *AuthHandlers) getDeviceConsent(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if a.OAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "device authorization is not active")
		return
	}
	code := normalizeUserCode(r.URL.Query().Get("user_code"))
	if code == "" {
		writeErr(w, http.StatusBadRequest, "user_code is required")
		return
	}
	d, err := a.OAuth.DeviceCodeByUserCode(r.Context(), hashToken(code))
	if err != nil {
		// Unknown code and somebody else's code are the same answer.
		writeErr(w, http.StatusNotFound, "this code is not valid")
		return
	}
	if d.UserID != "" && d.UserID != userID {
		writeErr(w, http.StatusNotFound, "this code is not valid")
		return
	}
	out := deviceConsentOut{
		UserCode: code, ClientID: d.ClientID, Client: d.ClientID, Scopes: d.Scope,
		ExpiresAt: d.ExpiresAt.UTC().Format(time.RFC3339),
		Choices:   oauthmeta.ScopesSupported(),
	}
	lang := emailtext.En
	if u, uerr := a.Users.ByID(r.Context(), userID); uerr == nil && u != nil {
		lang = emailtext.NormalizeLang(u.Lang)
	}
	out.Scope = deviceScopeProse(lang, d.Scope)
	if c, cerr := a.OAuth.ClientByID(r.Context(), d.ClientID); cerr == nil && strings.TrimSpace(c.Name) != "" {
		out.Client = c.Name
	} else {
		out.Client = emailtext.Phrase(lang, emailtext.PhraseUnnamedClient)
	}
	switch {
	case d.ConsumedAt != nil:
		out.State = "used"
	case d.DeniedAt != nil:
		out.State = "denied"
	case d.ApprovedAt != nil:
		out.State = "approved"
	case !time.Now().Before(d.ExpiresAt):
		out.State = "expired"
	default:
		out.State = "pending"
	}
	writeJSON(w, http.StatusOK, out)
}

// postDeviceConsent approves or denies an authorisation from the dashboard.
func (a *AuthHandlers) postDeviceConsent(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if a.OAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "device authorization is not active")
		return
	}
	var in struct {
		UserCode string `json:"user_code"`
		Decision string `json:"decision"`
		// Scope is the access level the PERSON picked on the consent card
		// (card t_cf45b84c). Omitted means "whatever the client asked for",
		// which is exactly what every dashboard before this card sent, so an
		// older build keeps working unchanged.
		Scope string `json:"scope"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	code := normalizeUserCode(in.UserCode)
	if code == "" {
		writeErr(w, http.StatusBadRequest, "user_code is required")
		return
	}
	// Super-admin accounts are refused here as well as at the mail step: this is
	// the last point before a token exists, and "no device flow for platform
	// admins" has to hold even if a code was typed by hand.
	if a.SuperAdmin != nil {
		if u, uerr := a.Users.ByID(r.Context(), userID); uerr == nil && u != nil && u.Email != "" && a.SuperAdmin(u.Email) {
			writeErr(w, http.StatusForbidden,
				"device onboarding is not available for platform-admin accounts; use the browser flow")
			return
		}
	}
	switch in.Decision {
	case "deny":
		if err := a.OAuth.DenyDeviceCode(r.Context(), hashToken(code), userID); err != nil {
			writeErr(w, http.StatusNotFound, "this code is not valid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
	case "approve":
		accountID := ""
		if a.Accounts != nil {
			if accs, aerr := a.Accounts.ByOwner(r.Context(), userID); aerr == nil && len(accs) > 0 {
				accountID = accs[0].ID
			}
		}
		pre, err := a.OAuth.DeviceCodeByUserCode(r.Context(), hashToken(code))
		if err != nil {
			writeErr(w, http.StatusNotFound, "this code is not valid")
			return
		}
		// The scope to grant. Absent = the client's request, which is what every
		// dashboard before this card sent (backwards compatible); present = the
		// person's choice, in EITHER direction. Validated against the scopes this
		// deployment can issue, and refused with a machine code rather than
		// clamped: silently granting something other than the radio the user
		// picked is the defect this card exists to close.
		chosen := pre.Scope
		if strings.TrimSpace(in.Scope) != "" {
			s, ok := oauthmeta.ChosenScope(in.Scope)
			if !ok {
				writeErr(w, http.StatusBadRequest, deviceScopeInvalidMessage)
				return
			}
			chosen = s
		}
		d, err := a.OAuth.ApproveDeviceCode(r.Context(), hashToken(code), userID, accountID, chosen)
		if err != nil {
			switch {
			case errors.Is(err, dbstore.ErrDeviceDenied):
				writeErr(w, http.StatusConflict, "this request was already denied")
			case errors.Is(err, dbstore.ErrDeviceExpired):
				writeErr(w, http.StatusGone, "this code has expired")
			case errors.Is(err, dbstore.ErrDeviceScopeUnsupported):
				writeErr(w, http.StatusBadRequest, deviceScopeInvalidMessage)
			default:
				writeErr(w, http.StatusNotFound, "this code is not valid")
			}
			return
		}
		// An UPGRADE is a new grant, and the old pair dies with it: a leaked
		// refresh token must stop working the moment the user believes the
		// credential changed. Route: device (the email was the consent screen).
		a.rotateOnUpgrade(r, userID, d.ClientID, accountID, d.Scope, "device")
		a.recordAuthEvent(r, accountID, userID, "oauth.device_approved", map[string]any{
			"client_id": d.ClientID, "scope": dbstore.ScopeString(d.Scope),
			// The request is recorded separately from the grant: at this card the
			// two can differ in either direction, and an operator reading the
			// audit needs to see that a person WIDENED (or narrowed) it.
			"requested": dbstore.ScopeString(pre.Scope),
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	default:
		writeErr(w, http.StatusBadRequest, "decision must be approve or deny")
	}
}

// rotateOnUpgrade revokes the user's existing grant for this client when the
// new grant is WIDER, so the old credential cannot outlive the upgrade. It is
// best-effort: the new grant is already valid, and a rotation failure must not
// turn an approved onboarding into an error the user cannot act on.
func (a *AuthHandlers) rotateOnUpgrade(r *http.Request, userID, clientID, accountID string, newScope []string, route string) {
	if a.OAuth == nil {
		return
	}
	apps, err := a.OAuth.ConnectedAppsByUser(r.Context(), userID)
	if err != nil {
		slog.Warn("oauth_upgrade_rotation_lookup_failed", "err", err)
		return
	}
	for _, app := range apps {
		if app.ClientID != clientID || !scopeWider(newScope, app.Scope) {
			continue
		}
		if err := a.OAuth.RevokeTokenByIDForUser(r.Context(), userID, app.ID); err != nil {
			slog.Warn("oauth_upgrade_rotation_failed", "err", err, "client_id", clientID)
			continue
		}
		a.recordAuthEvent(r, accountID, userID, "oauth.scope_upgraded", map[string]any{
			"client_id": clientID, "from": dbstore.ScopeString(app.Scope),
			"to": dbstore.ScopeString(newScope), "route": route,
			// The replaced pair is named so an operator can match the audit to
			// the row that no longer works.
			"revoked_token_id": app.ID,
		})
	}
}

// scopeWider reports whether `next` grants more than `prev`. readwrite is the
// only widening step in the ladder; read↔readwrite is the whole ordering.
func scopeWider(next, prev []string) bool {
	return scopeHas(next, oauthmeta.ScopeReadWrite) && !scopeHas(prev, oauthmeta.ScopeReadWrite)
}

func scopeHas(scope []string, want string) bool {
	for _, s := range scope {
		if s == want {
			return true
		}
	}
	return false
}

// decodeJSON is the shared small-body decoder (httpapi/rest.go). Nothing local
// to this file: one decoder, one body-size limit.
