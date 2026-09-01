// Auth HTTP handlers: register, login, logout, TOTP setup/verify.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"log/slog"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

const sessionCookie = "jf_session"

// AuthHandlers bundles session store and DB for auth routes.
type AuthHandlers struct {
	Sessions *auth.Sessions
	Users    *dbstore.UserStore
	Accounts *dbstore.AccountStore
	BaseURL  string

	// Issue #42 hardening.
	//   LoginLimiter tracks failed logins per IP + email in Valkey (nil only in
	//   bare-mode tests). VerifyKey signs the /verify email-confirmation link.
	//   VerifyMailer delivers that email.
	LoginLimiter *auth.LoginLimiter
	VerifyKey    []byte
	VerifyMailer VerifyMailer
}

// RegisterRoutes wires auth endpoints onto the mux.
//
// Every /v1/auth/* route is wrapped in the per-IP RateLimit middleware as the
// coarse outer layer (issue #42): anonymous and sessionful auth routes alike
// are throttled so a single IP cannot hammer the endpoints at network speed.
// Registration additionally uses a much tighter bucket (RegistrationLimit)
// since it is fully open; login brute-force is further gated by a Valkey
// failed-attempt tracker inside handleLogin.
func (a *AuthHandlers) RegisterRoutes(mux *http.ServeMux) {
	// Public (open) auth entry points — tightened most.
	mux.Handle("POST /v1/auth/register", middleware.RegistrationLimit(http.HandlerFunc(a.handleRegister)))
	mux.Handle("POST /v1/auth/login", middleware.RateLimit(http.HandlerFunc(a.handleLogin)))
	mux.HandleFunc("POST /v1/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /v1/auth/me", a.handleMe)
	mux.HandleFunc("POST /v1/auth/2fa/setup", a.handle2FASetup)
	mux.HandleFunc("POST /v1/auth/2fa/enable", a.handle2FAEnable)
	mux.HandleFunc("POST /v1/auth/2fa/disable", a.handle2FADisable)
	mux.HandleFunc("POST /v1/auth/change-password", a.handleChangePassword)
	mux.HandleFunc("GET /v1/auth/profile", a.handleProfile)
	mux.HandleFunc("POST /v1/auth/profile/name", a.handleUpdateName)
	mux.HandleFunc("POST /v1/auth/profile/email", a.handleUpdateEmail)
	mux.HandleFunc("PUT /v1/auth/profile/email", a.handleUpdateEmail)
	// Email confirmation (issue #42): verifies an account is real before its
	// first password login. GET renders a small confirmation page; the same
	// token also works via POST if an app ever wants JSON back.
	mux.HandleFunc("GET /v1/auth/verify", a.handleVerify)
	mux.HandleFunc("POST /v1/auth/verify", a.handleVerify)
	// Social login (provider-generic OIDC). UI links use /api/... which the
	// Next proxy maps to /v1/... for same-origin cookies; direct /api/* also
	// works when the Go server is the public entrypoint.
	mux.HandleFunc("GET /v1/auth/oauth/{provider}", a.socialStart)
	mux.HandleFunc("GET /v1/auth/callback/{provider}", a.socialCallback)
	mux.HandleFunc("GET /v1/auth/social", a.listSocial)
	mux.HandleFunc("GET /v1/auth/social/providers", a.socialProviders)
	mux.HandleFunc("DELETE /v1/auth/social/{provider}", a.unlinkSocial)
}

// ─── request/response DTOs ───────────────────────────────────────────────────

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Honeypot field (issue #42): a hidden form field bots fill, humans don't.
	// When populated the server silently accepts the request but discards it
	// (and logs the hit) instead of creating an account.
	Website string `json:"website"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type meResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name,omitempty"`
	TOTPEnabled bool    `json:"totp_enabled"`
	CreatedAt   string  `json:"created_at"`
	NextStep    *string `json:"next_step,omitempty"` // set when 2FA code is required
}

// ─── handlers ────────────────────────────────────────────────────────────────

func (a *AuthHandlers) handleRegister(w http.ResponseWriter, r *http.Request) {
	ip := middleware.ClientIP(r)
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	// Honeypot (issue #42): a filled hidden field marks a bot — accept the
	// request silently (2xx so the bot thinks it worked) but create nothing.
	if req.Website != "" {
		slog.Warn("register_honeypot_caught", "ip", ip)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	// Per-IP registration rate observability (issue #42): every attempt is
	// logged, anomalous volumes are surfaced by log aggregation.
	slog.Info("register_attempt", "ip", ip, "email", obfuscateEmail(req.Email))

	if !auth.ValidEmail(req.Email) {
		writeErr(w, http.StatusBadRequest, "email tidak sah")
		return
	}
	if len(req.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "kata laluan mesti sekurang-kurangnya 10 aksara")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	user, err := a.Users.Create(r.Context(), req.Email, hash)
	if err != nil {
		if errors.Is(err, dbstore.ErrEmailTaken) {
			writeErr(w, http.StatusConflict, "email sudah didaftarkan")
			return
		}
		slog.Error("register_failed", "err", err, "email", req.Email)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Auto-provision the default account so new users can build immediately.
	if a.Accounts != nil {
		acc := &dbstore.Account{
			OwnerUserID: user.ID,
			Name:        strings.Split(req.Email, "@")[0] + " Store",
			Slug:        slugify(strings.Split(req.Email, "@")[0], user.ID),
		}
		if err := a.Accounts.Create(r.Context(), acc); err != nil {
			// Slug collision: append random suffix and retry once.
			acc.Slug = acc.Slug + "-" + shortRand()
			if err2 := a.Accounts.Create(r.Context(), acc); err2 != nil {
				slog.Error("default_account_create", "err", err2, "user", user.ID)
			}
		}
	}

	// Email verification (issue #42): new accounts are verified=false and may
	// not log in until they confirm. Email a signed 24h verification link; a
	// missing mailer is logged but doesn't block registration (a /verify link
	// can always be requested again later).
	if err := a.sendVerifyEmail(r.Context(), user.ID, user.Email); err != nil {
		slog.Warn("verify_email_send_failed", "err", err, "user", user.ID)
	}

	// No session is started: the account must be email-verified before first
	// login, so the user is directed to check their inbox.
	writeJSON(w, http.StatusCreated, map[string]string{
		"status":             "created",
		"email_verification": "required",
	})
}

func (a *AuthHandlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := middleware.ClientIP(r)
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Brute-force gate (issue #42): after FailLockoutThreshold failures from an
	// IP within 15min the login is refused outright with 429 + Retry-After.
	// When the Valkey limiter is unavailable we fail open (availability beats
	// brute-force rate here); the per-IP RateLimit + argon2 cost remain.
	if a.LoginLimiter != nil {
		locked, _, _, err := a.LoginLimiter.Status(r.Context(), ip, email)
		if err != nil {
			// limiter backend down → log, don't block logins.
			slog.Warn("loginfail_status_error", "err", err, "ip", ip)
		} else if locked {
			w.Header().Set("Retry-After", auth.LoginLockoutRetryAfter)
			writeErr(w, http.StatusTooManyRequests, auth.LoginLockoutMessage)
			return
		}
	}

	user, err := a.Users.ByEmail(r.Context(), email)
	if err != nil {
		// Uniform 401 — no user enumeration. For an unknown email we still run
		// argon2 against a fixed dummy hash so the CPU cost (and therefore the
		// response timing) matches a real password check; a brute-forcer cannot
		// distinguish "no such user" from "wrong password" by timing either.
		_, _ = auth.VerifyPassword(req.Password, auth.DummyPasswordHash)
		a.failLogin(w, r, ip, "", false)
		return
	}

	// Verify the password. Unknown-email attempts (above) already paid the same
	// argon2 cost, so response timing stays uniform across known/unknown.
	ok, verifyErr := auth.VerifyPassword(req.Password, user.PasswordHash)
	if verifyErr != nil || !ok {
		a.failLogin(w, r, ip, email, true)
		return
	}

	// Email verification (issue #42): an unverified account may not log in.
	if !user.Verified {
		// Deliberately NOT recorded as a failed password attempt — the
		// credential was correct; the user just hasn't confirmed their email.
		writeErr(w, http.StatusForbidden, "sila sahkan email anda sebelum log masuk (semak peti masuk untuk pautan pengesahan)")
		return
	}

	// TOTP is only challenged for users who have *enabled* 2FA. Users with a
	// stale secret but totp_enabled=false (abandoned setup) must NOT be held at
	// the auth step — that would lock them out (see issue #39).
	if user.TOTPEnabled {
		if req.TOTPCode == "" {
			step := "totp_required"
			writeJSON(w, http.StatusUnauthorized, meResponse{NextStep: &step})
			return
		}
		if !auth.ValidateTOTPSecret(user.TOTPSecret, req.TOTPCode) {
			a.failLogin(w, r, ip, email, true)
			return
		}
	}

	// Success resets the failure counters so a legitimate owner who later
	// mistypes can't be locked out by stale history.
	if a.LoginLimiter != nil {
		if err := a.LoginLimiter.Reset(r.Context(), ip, email); err != nil {
			slog.Warn("loginfail_reset_error", "err", err, "ip", ip)
		}
	}

	if err := a.startSessionAndDefaultAccount(w, r, user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// failLogin handles a rejected login attempt: record the failure in Valkey and
// emit a uniform 401. A recorded (real) user takes an extra artificial delay so
// a brute-forcer cannot distinguish "no such user" from "wrong password" by
// timing alone — unknown-email attempts return immediately.
func (a *AuthHandlers) failLogin(w http.ResponseWriter, r *http.Request, ip, email string, known bool) {
	if a.LoginLimiter != nil {
		rec := email
		if !known {
			// Unknown emails share the per-IP counter only — never key the
			// per-account counter with a non-existent user.
			rec = ""
		}
		if err := a.LoginLimiter.RecordFailure(r.Context(), ip, rec); err != nil {
			slog.Warn("loginfail_record_error", "err", err, "ip", ip)
		}
	}
	// Uniform 401, identical body for both cases → no enumeration either from
	// the message or from the status code.
	writeErr(w, http.StatusUnauthorized, "maklumat log masuk tidak sah")
}

func (a *AuthHandlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err == nil {
		_ = a.Sessions.Destroy(r.Context(), c.Value)
		invalidateSessionAcctCache(c.Value)
	}
	http.SetCookie(w, a.clearCookie())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *AuthHandlers) handleMe(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// handle2FASetup generates a secret (not yet enabled) for the logged-in user.
// The secret is stored as PENDING in Valkey with a short TTL and NOT written to
// the users table, so an abandoned setup never arms the login TOTP challenge.
func (a *AuthHandlers) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	secret, url, err := auth.GenerateTOTPSecret(user.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.pendingTOTP().Set(r.Context(), user.ID, secret); err != nil {
		slog.Error("totp_pending_set", "err", err, "user", user.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_url": url})
}

// handle2FAEnable validates the code against the PENDING secret, then
// atomically moves it to the active column and enables 2FA, issuing a fresh
// session. The pending key is consumed (deleted) on success.
func (a *AuthHandlers) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeErr(w, http.StatusBadRequest, "kod diperlukan")
		return
	}
	pending, err := a.pendingTOTP().Get(r.Context(), user.ID)
	if err != nil || pending == "" {
		// No pending secret (never set up, expired, or already consumed).
		writeErr(w, http.StatusBadRequest, "2fa belum di-setup atau ralat")
		return
	}
	if !auth.ValidateTOTPSecret(pending, req.Code) {
		writeErr(w, http.StatusUnauthorized, "kod tidak sah")
		return
	}
	if err := a.Users.EnableTOTP(r.Context(), user.ID, pending); err != nil {
		slog.Error("totp_enable", "err", err, "user", user.ID)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Consume the pending secret so it can't be re-enabled.
	_ = a.pendingTOTP().Delete(r.Context(), user.ID)
	if err := a.startSession(w, r, user.ID, user.Email, true); err != nil { // fresh session post-2FA
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

// pendingTOTP returns the pending-2FA store, or nil when Valkey is absent.
func (a *AuthHandlers) pendingTOTP() *auth.PendingTOTP {
	if a.Sessions == nil {
		return nil
	}
	return a.Sessions.PendingTOTP()
}

// handle2FADisable turns 2FA off after password confirmation.
func (a *AuthHandlers) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "kata laluan tidak sah")
		return
	}
	if err := a.Users.DisableTOTP(r.Context(), user.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// ─── session plumbing ────────────────────────────────────────────────────────

func (a *AuthHandlers) startSession(w http.ResponseWriter, r *http.Request, userID, _ string, _ bool) error {
	token, err := a.Sessions.Create(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(authSessionTTL / time.Second),
	})
	return nil
}

func (a *AuthHandlers) startSessionAndDefaultAccount(w http.ResponseWriter, r *http.Request, user *dbstore.User) error {
	// Default account creation belongs to onboarding (M3); sessions only here.
	return a.startSession(w, r, user.ID, user.Email, user.TOTPEnabled)
}

func (a *AuthHandlers) clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func (a *AuthHandlers) currentUser(r *http.Request) *dbstore.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	userID, err := a.Sessions.Get(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	user, err := a.Users.ByID(r.Context(), userID)
	if err != nil {
		return nil
	}
	return user
}

// isDev controls cookie Secure flag; set by Server at wiring time.
var isDev bool

var authSessionTTL = 14 * 24 * time.Hour
