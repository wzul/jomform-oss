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
)

const sessionCookie = "jf_session"

// AuthHandlers bundles session store and DB for auth routes.
type AuthHandlers struct {
	Sessions *auth.Sessions
	Users    *dbstore.UserStore
	Accounts *dbstore.AccountStore
}

// RegisterRoutes wires auth endpoints onto the mux.
func (a *AuthHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/register", a.handleRegister)
	mux.HandleFunc("POST /v1/auth/login", a.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /v1/auth/me", a.handleMe)
	mux.HandleFunc("POST /v1/auth/2fa/setup", a.handle2FASetup)
	mux.HandleFunc("POST /v1/auth/2fa/enable", a.handle2FAEnable)
	mux.HandleFunc("POST /v1/auth/2fa/disable", a.handle2FADisable)
	mux.HandleFunc("POST /v1/auth/change-password", a.handleChangePassword)
}

// ─── request/response DTOs ───────────────────────────────────────────────────

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type meResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	TOTPEnabled bool    `json:"totp_enabled"`
	CreatedAt   string  `json:"created_at"`
	NextStep    *string `json:"next_step,omitempty"` // set when 2FA code is required
}

// ─── handlers ────────────────────────────────────────────────────────────────

func (a *AuthHandlers) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
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

	if err := a.startSession(w, r, user.ID, user.Email, user.TOTPSecret != ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPSecret != "", CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (a *AuthHandlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, err := a.Users.ByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil {
		// Uniform response — no user enumeration.
		writeErr(w, http.StatusUnauthorized, "maklumat log masuk tidak sah")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "maklumat log masuk tidak sah")
		return
	}

	if user.TOTPSecret != "" {
		if req.TOTPCode == "" {
			step := "totp_required"
			writeJSON(w, http.StatusUnauthorized, meResponse{NextStep: &step})
			return
		}
		if !auth.ValidateTOTPSecret(user.TOTPSecret, req.TOTPCode) {
			writeErr(w, http.StatusUnauthorized, "kod 2FA tidak sah")
			return
		}
	}

	if err := a.startSessionAndDefaultAccount(w, r, user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPSecret != "", CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (a *AuthHandlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err == nil {
		_ = a.Sessions.Destroy(r.Context(), c.Value)
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
		ID: user.ID, Email: user.Email, TOTPEnabled: user.TOTPSecret != "", CreatedAt: user.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// handle2FASetup generates a secret (not yet enabled) for the logged-in user.
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
	if err := a.Users.SetTOTPSecret(r.Context(), user.ID, secret); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_url": url})
}

// handle2FAEnable confirms the code and marks the account 2FA-enabled,
// issuing a fresh session.
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
	stored, err := a.Users.TOTPSecret(r.Context(), user.ID)
	if err != nil || stored == "" {
		writeErr(w, http.StatusBadRequest, "2fa belum di-setup")
		return
	}
	if !auth.ValidateTOTPSecret(stored, req.Code) {
		writeErr(w, http.StatusUnauthorized, "kod tidak sah")
		return
	}
	if err := a.Users.EnableTOTP(r.Context(), user.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = a.startSession(w, r, user.ID, user.Email, true) // fresh session post-2FA
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
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
	return a.startSession(w, r, user.ID, user.Email, user.TOTPSecret != "")
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
