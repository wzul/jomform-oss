// Auth HTTP handlers: register, login, logout, TOTP setup/verify.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
)

const sessionCookie = "jf_session"

// Trust cookie (issue #63): single-purpose — waives the TOTP step on a
// trusted device for 30 days. Never a session, password still required.
const trustCookie = "jf_trust"

// AuthHandlers bundles session store and DB for auth routes.
type AuthHandlers struct {
	Sessions *auth.Sessions
	Users    *dbstore.UserStore
	Accounts *dbstore.AccountStore
	// Issue #164: Spaces is used to validate a default-space choice belongs
	// to the user (owner or member) and to resolve the default space's slug.
	Spaces *dbstore.SpaceStore
	BaseURL  string
	// CookieDomain is the shared Domain attribute for the session cookie
	// (e.g. ".jomform.com"), allowing the login session to be sent to product
	// subdomains such as mcp.jomform.com (issue #140). Empty when the host is
	// a dev/IP/localhost host → the cookie stays host-only.
	CookieDomain string

	// Issue #42 hardening.
	//   LoginLimiter tracks failed logins per IP + email in Valkey (nil only in
	//   bare-mode tests). VerifyKey signs the /verify email-confirmation link.
	//   VerifyMailer delivers that email.
	LoginLimiter *auth.LoginLimiter
	VerifyKey    []byte
	VerifyMailer VerifyMailer

	// Issue #56 SES reputation hardening: Suppressions (never resend to a
	// bounced/complained address), Breaker (pause verification on high bounce
	// ratio), MXChecker (reject no-MX domains pre-create). All nil in
	// bare-mode tests; register/send verify handle nil defensively.
	Suppressions *dbstore.SuppressionStore
	Breaker      *email.Breaker
	MXChecker    *email.MXChecker
	// Issue #199: when non-nil, auth emails (verify, magic link, password
	// reset, 2FA codes) are enqueued and sent by a background worker instead of
	// synchronously in the request handler. Nil (bare-mode tests) → synchronous
	// send via VerifyMailer.
	EmailQueue *emailqueue.Queue
	// Issue #161: space membership store, used to redeem a pending invite
	// when a new user signs up via an invite link. Nil in bare-mode tests.
	SpaceMembers *dbstore.SpaceMemberStore

	// Issue #92: audit/behaviour log for auth actions (login/logout/2fa).
	// Nil in bare-mode tests; Record calls are no-ops when nil.
	Events *dbstore.EventStore
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
	// 2FA email backup code (issue #72): a 6-digit one-time code emailed during
	// login (gated on a 5-min pending flag armed only after a correct-password
	// totp_required). Tight per-IP limit (3/hour) so it can't be weaponized
	// into a mail-bomb, and always responds generically (no enumeration).
	mux.Handle("POST /v1/auth/2fa/email-code", middleware.RateLimitWith(3.0/3600.0, 3)(http.HandlerFunc(a.handleEmail2FACode)))
	// Passwordless magic link (issue #73): request mints a single-use 15-min
	// hashed token and emails it. Tight per-IP limit (mail-bomb + enumeration
	// are the same #56 reputation vector as registration/reset).
	mux.Handle("POST /v1/auth/magic-link/request", middleware.RateLimitWith(1.0/3600.0, 2)(http.HandlerFunc(a.handleMagicLinkRequest)))
	mux.HandleFunc("POST /v1/auth/magic-link/verify", a.handleMagicLinkVerify)
	mux.HandleFunc("POST /v1/auth/change-password", a.handleChangePassword)
	mux.HandleFunc("GET /v1/auth/profile", a.handleProfile)
	mux.HandleFunc("POST /v1/auth/profile/name", a.handleUpdateName)
	mux.HandleFunc("POST /v1/auth/profile/phone", a.handleUpdatePhone)
	mux.HandleFunc("POST /v1/auth/profile/timezone", a.handleUpdateTimezone)
	mux.HandleFunc("POST /v1/auth/profile/email", a.handleUpdateEmail)
	mux.HandleFunc("PUT /v1/auth/profile/email", a.handleUpdateEmail)
	// Issue #164: set/clear the user's default space (multi-space login picker).
	mux.HandleFunc("PUT /v1/auth/profile/default-space", a.handleSetDefaultSpace)
	// Email confirmation (issue #42): verifies an account is real before its
	// first password login. GET renders a small confirmation page; the same
	// token also works via POST if an app ever wants JSON back.
	mux.HandleFunc("GET /v1/auth/verify", a.handleVerify)
	mux.HandleFunc("POST /v1/auth/verify", a.handleVerify)
	// Password recovery (issue #62): forgot-password mints a single-use reset
	// link (always 200, no enumeration) and is tightly rate-limited per IP to
	// match the #56 suppression/breaker concern — reset emails can't be
	// weaponized into a mail-bomb.
	mux.Handle("POST /v1/auth/forgot-password", middleware.RateLimitWith(2.0/3600.0, 2)(http.HandlerFunc(a.handleForgotPassword)))
	mux.HandleFunc("POST /v1/auth/reset-password", a.handleResetPassword)
	// Trusted-device status (issue #63): reported for the Profile → Security
	// line; also lets the client forget the current device.
	mux.HandleFunc("GET /v1/auth/trust-status", a.handleTrustStatus)
	mux.HandleFunc("POST /v1/auth/trust/forget", a.handleTrustForget)
	mux.HandleFunc("POST /v1/auth/trust/forget-all", a.handleTrustForgetAll)
	// Social login (provider-generic OIDC). UI links use /api/... which the
	// Next proxy maps to /v1/... for same-origin cookies; direct /api/* also
	// works when the Go server is the public entrypoint.
	mux.HandleFunc("GET /v1/auth/oauth/{provider}", a.socialStart)
	mux.HandleFunc("GET /v1/auth/callback/{provider}", a.socialCallback)
	mux.HandleFunc("GET /v1/auth/social", a.listSocial)
	mux.HandleFunc("GET /v1/auth/social/providers", a.socialProviders)
	mux.HandleFunc("DELETE /v1/auth/social/{provider}", a.unlinkSocial)
	// Issue #302: explicit social account connect — a first social login whose
	// email matches an existing account is NOT auto-linked; the user proves
	// ownership of the email via a signed connect link, then socialConnect
	// binds the identity. connectResend re-emits that emailed link.
	mux.HandleFunc("POST /v1/auth/social/connect", a.socialConnect)
	mux.HandleFunc("POST /v1/auth/social/connect/resend", a.connectResend)
}

// ─── request/response DTOs ───────────────────────────────────────────────────

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Honeypot field (issue #42): a hidden form field bots fill, humans don't.
	// When populated the server silently accepts the request but discards it
	// (and logs the hit) instead of creating an account.
	Website string `json:"website"`
	// Invite (issue #161): a space-invite redemption token from the signup
	// link. When present and valid, the new user is added to the inviting
	// space with the invited role after their account is created.
	Invite string `json:"invite,omitempty"`
	// Lang (issue #206): the user's chosen locale from the signup form. It is
	// carried on the verification link so the confirmation page renders in
	// the user's language. Empty = unknown → English.
	Lang string `json:"lang,omitempty"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
	// Remember (issue #62): unchecked (default) = browser-session cookie +
	// 24h server TTL; checked = 14-day persistent session.
	Remember bool `json:"remember,omitempty"`
	// TOTPRemember (issue #63): when true and the 2FA code is correct, issue
	// a jf_trust cookie waiving TOTP on this device for 30 days.
	TOTPRemember bool `json:"totp_remember,omitempty"`
}

type meResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name,omitempty"`
	AvatarURL   string  `json:"avatar_url,omitempty"` // issue #133
	// Issue #181: the user's optional phone number ("" when not set).
	Phone       string  `json:"phone,omitempty"`
	// Issue #191: the user's preferred IANA timezone ("" = Auto — follow the
	// browser/PC default).
	Timezone    string  `json:"timezone,omitempty"`
	TOTPEnabled bool    `json:"totp_enabled"`
	CreatedAt   string  `json:"created_at"`
	NextStep    *string `json:"next_step,omitempty"` // set when 2FA code is required
	// Issue #164: the user's default space id ("" when none). Lets the
	// frontend skip the space-picker after login when a default is set.
	DefaultSpaceID string `json:"default_space_id,omitempty"`
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
	if req.Password != "" && len(req.Password) < 10 {
		writeErr(w, http.StatusBadRequest, "kata laluan mesti sekurang-kurangnya 10 aksara")
		return
	}
	if req.Password == "" {
		// Issue #303: password is optional at signup. A blank password leaves
		// the account with no usable credential, so assign a cryptographically
		// random UUID as the password. It flows through the same argon2id
		// hashing path as a user-chosen password (hash computed below), so the
		// random value is never stored in plaintext. The user can set a real
		// password later via forgot-password / profile settings, and can
		// always log in via magic link or social.
		req.Password = uuid.NewString()
	}

	// Issue #56: MX pre-check — reject domains that accept no mail BEFORE a
	// user row is created, so a fake-address flood never reaches SES and never
	// leaves orphan accounts. Only when an MX checker is wired.
	if a.MXChecker != nil {
		domain := auth.EmailDomain(req.Email)
		if domain == "" {
			writeErr(w, http.StatusBadRequest, "email tidak sah")
			return
		}
		ok, err := a.MXChecker.Check(r.Context(), domain)
		if err != nil {
			// Conservative: fail closed — if we can't verify the domain accepts
			// mail, reject rather than risk a reputation-harming bounce.
			slog.Warn("register_mx_check_failed", "err", err, "email", obfuscateEmail(req.Email))
			writeErr(w, http.StatusBadRequest, "domain emel tidak menerima emel")
			return
		}
		if !ok {
			writeErr(w, http.StatusBadRequest, "domain emel tidak menerima emel")
			return
		}
	}

	// Issue #56: verification circuit breaker — if the trailing-hour bounce
	// ratio is above threshold, pause verification mail (503) instead of
	// firing more bounces that could take down the SES identity.
	if a.Breaker != nil {
		tripped, err := a.Breaker.Tripped(r.Context())
		if err != nil {
			// fail open on cache error (availability), but surface in logs
			slog.Warn("breaker_status_error", "err", err, "ip", ip)
		} else if tripped {
			slog.Warn("breaker_open_paused_verification", "ip", ip)
			writeErr(w, http.StatusServiceUnavailable, email.BreakerOpenMessage)
			return
		}
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

	// Issue #161: redeem a pending space invite, if the signup came from an
	// invite link. Adds the new user to the inviting space with the invited
	// role. A missing/invalid/expired token is non-fatal — the account is
	// still created, the invite is simply not redeemed.
	if req.Invite != "" && a.SpaceMembers != nil {
		if err := a.SpaceMembers.RedeemInvite(r.Context(), req.Invite, user.ID); err != nil {
			slog.Warn("invite_redeem_failed", "err", err, "user", user.ID)
		} else {
			slog.Info("invite_redeemed", "user", user.ID)
		}
	}

	// Email verification (issue #42): new accounts are verified=false and may
	// not log in until they confirm. Email a signed 24h verification link; a
	// missing mailer is logged but doesn't block registration (a /verify link
	// can always be requested again later).
	if err := a.sendVerifyEmail(r.Context(), user.ID, user.Email, req.Lang); err != nil {
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
		recordAuthFailure("unverified")
		writeErr(w, http.StatusForbidden, "sila sahkan email anda sebelum log masuk (semak peti masuk untuk pautan pengesahan)")
		return
	}

	// TOTP is only challenged for users who have *enabled* 2FA. Users with a
	// stale secret but totp_enabled=false (abandoned setup) must NOT be held at
	// the auth step — that would lock them out (see issue #39).
	if user.TOTPEnabled {
		// Trusted device (issue #63): a valid jf_trust cookie bound to THIS
		// user + the password just verified waives the TOTP challenge. The
		// cookie is single-purpose — it never grants a session, and the
		// password is still required every login.
		trusted := a.trustMatchesUser(r, user.ID)
		if req.TOTPCode == "" && !trusted {
			// Issue #72: this correct-password 2FA challenge is the ONLY path
			// that arms the email-backup-code pending flag. The 2FA email-code
			// endpoint refuses to send without a live jf:2fa_pending flag, so a
			// blind attacker who knows an email but not the password can never
			// harvest backup codes.
			if e2fa := a.email2FA(); e2fa != nil {
				if err := e2fa.ArmPending(r.Context(), ip, email); err != nil {
					slog.Warn("email2fa_arm_pending", "err", err, "ip", ip)
				}
			}
			step := "totp_required"
			writeJSON(w, http.StatusUnauthorized, meResponse{NextStep: &step})
			return
		}
		if !trusted {
			// Accept EITHER the authenticator TOTP or an emailed backup code
			// (issue #72). TOTP is tried first; on failure the code is checked
			// against the emailed-code store (single-use, hashed, 10-min).
			totpOK := auth.ValidateTOTPSecret(user.TOTPSecret, req.TOTPCode)
			emailOK := false
			if !totpOK {
				if e2fa := a.email2FA(); e2fa != nil {
					err := e2fa.Consume(r.Context(), user.ID, req.TOTPCode)
					emailOK = err == nil
					if err != nil && !errors.Is(err, auth.ErrEmail2FAInvalid) {
						slog.Warn("email2fa_consume_error", "err", err, "user", user.ID)
					}
				}
			}
			if !totpOK && !emailOK {
				a.failLogin(w, r, ip, email, true)
				return
			}
			// Issue #72: trust-device (jf_trust) is NEVER issued for an
			// email-code login — only a real authenticator TOTP proves the user
			// holds the second factor. A backup code is a recovery path, not a
			// basis for a 30-day trust waiver.
			if totpOK && req.TOTPRemember {
				a.issueTrustCookie(w, r, user.ID)
			}
		}
	}

	// Success resets the failure counters so a legitimate owner who later
	// mistypes can't be locked out by stale history.
	if a.LoginLimiter != nil {
		if err := a.LoginLimiter.Reset(r.Context(), ip, email); err != nil {
			slog.Warn("loginfail_reset_error", "err", err, "ip", ip)
		}
	}

	if err := a.startSession(w, r, user.ID, user.Email, user.TOTPEnabled, req.Remember); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Issue #92: audit login (with the user's first owned account).
	a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.login", map[string]any{
		"method": "password", "totp": user.TOTPEnabled,
	})
	// Issue #164: include the user's default space so the frontend can skip
	// the space-picker when a default is set.
	defSpace := ""
	if a.Users != nil {
		if ds, err := a.Users.DefaultSpace(r.Context(), user.ID); err == nil {
			defSpace = ds
		}
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
		Phone: user.Phone, Timezone: user.Timezone, DefaultSpaceID: defSpace,
	})
}

// firstAccountID resolves the user's first owned account id ("" when none).
func (a *AuthHandlers) firstAccountID(r *http.Request, userID string) string {
	if a.Accounts == nil {
		return ""
	}
	accs, err := a.Accounts.ByOwner(r.Context(), userID)
	if err != nil || len(accs) == 0 {
		return ""
	}
	return accs[0].ID
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
	recordAuthFailure("bad_credentials")
	writeErr(w, http.StatusUnauthorized, "maklumat log masuk tidak sah")
}

func (a *AuthHandlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err == nil {
		// Issue #92: audit logout before destroying the session.
		if user := a.currentUser(r); user != nil {
			a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.logout", nil)
		}
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
	// Issue #164: surface the user's default space so the frontend knows
	// whether to show the space-picker after login.
	defSpace := ""
	if a.Users != nil {
		if ds, err := a.Users.DefaultSpace(r.Context(), user.ID); err == nil {
			defSpace = ds
		}
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, Name: user.Name, AvatarURL: user.AvatarURL, TOTPEnabled: user.TOTPEnabled, CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
		Phone: user.Phone, Timezone: user.Timezone, DefaultSpaceID: defSpace,
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
	// Issue #69: a fresh session is issued to upgrade the session's 2FA
	// posture — but it MUST inherit the original login's remember flag, never
	// hardcode false. A "remember me" (14d) login enabling 2FA keeps its 14d
	// session; an unchecked (24h/browser-session) login stays short. Otherwise
	// the rotation silently downgrades persistence (sidebar + stale 2FA status
	// broke because the frontend raced this new cookie).
	remember := false
	if c, err := r.Cookie(sessionCookie); err == nil {
		remember = a.Sessions.Remembered(r.Context(), c.Value)
	}
	if err := a.startSession(w, r, user.ID, user.Email, true, remember); err != nil { // fresh session post-2FA, inherit remember
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Issue #72: confirm 2FA is now active and explain the email-code fallback
	// trade-off (a backup code is a recovery path that bypasses the authenticator).
	if err := a.send2FAActiveEmail(r.Context(), user.Email); err != nil {
		slog.Warn("2fa_active_email_failed", "err", err, "user", user.ID)
	}
	// Issue #92: audit 2FA enable.
	a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.2fa_enabled", nil)
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
	// Issue #63: disabling 2FA invalidates every trusted device — there must
	// be no trust cookie lingering that would waive a (now-removed) 2FA step.
	if a.Sessions != nil {
		_ = a.Sessions.TrustedDevices().RevokeAll(r.Context(), user.ID)
	}
	clearTrustCookie(w)
	// Issue #92: audit 2FA disable.
	a.recordAuthEvent(r, a.firstAccountID(r, user.ID), user.ID, "auth.2fa_disabled", nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// ─── session plumbing ────────────────────────────────────────────────────────
//
// Session TTL (issue #62): unchecked remember-me = 24h server TTL + a
// browser-session cookie (MaxAge -1, dies when the browser closes); checked =
// 14d persistent cookie + server TTL. The server side always enforces the TTL
// regardless of the cookie, so a replayed cookie can't outlive its server key.
const (
	rememberShortTTL = 24 * time.Hour
	rememberLongTTL  = 14 * 24 * time.Hour
)

// startSession creates a session for userID, honoring the remember flag for
// cookie MaxAge and server TTL, and persists remember so a later session
// rotation (2FA enable, issue #69) can inherit the persistence choice. The
// email/totpEnabled args are accepted for historical compatibility with call
// sites that threaded them through; they no longer influence the session.
func (a *AuthHandlers) startSession(w http.ResponseWriter, r *http.Request, userID, _ string, _ bool, remember bool) error {
	ttl := rememberLongTTL
	maxAge := int(rememberLongTTL.Seconds())
	if !remember {
		ttl = rememberShortTTL
		// Go net/http Cookie.MaxAge semantics (issue #70): MaxAge < 0 emits
		// Max-Age=0 (RFC 6265 §4.1.2.2 → the browser discards the cookie on
		// arrival). Only MaxAge == 0 OMITS the attribute, producing a true
		// browser-session cookie. The 24h server TTL below remains the real
		// security control; the cookie attribute only controls client
		// persistence, so the unticked login must not emit Max-Age at all.
		maxAge = 0
	}
	token, err := a.Sessions.CreateRememberedWithTTL(r.Context(), userID, ttl, remember)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Domain:   a.CookieDomain,
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	return nil
}

func (a *AuthHandlers) clearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		Domain:   a.CookieDomain,
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}

func (a *AuthHandlers) currentUser(r *http.Request) *dbstore.User {
	// Honors pw_reset_at: sessions issued before a password reset are dead
	// (issue #62). Implemented in auth_ux.go.
	return a.sessionUser(r)
}

// recordAuthEvent appends an audit/behaviour event for a user's account.
// Best-effort: a nil Events store or a write error is logged, never fatal.
// accountID may be empty (e.g. a failed login with no resolved account) — the
// event is then skipped since events.account_id is NOT NULL.
func (a *AuthHandlers) recordAuthEvent(r *http.Request, accountID, userID, eventName string, props map[string]any) {
	if a.Events == nil || accountID == "" {
		return
	}
	if err := a.Events.Record(r.Context(), accountID, userID, eventName, props,
		middleware.ClientIP(r), r.UserAgent()); err != nil {
		slog.Error("event_record", "err", err, "event", eventName, "account", accountID)
	}
}

// isDev controls cookie Secure flag; set by Server at wiring time.
var isDev bool
