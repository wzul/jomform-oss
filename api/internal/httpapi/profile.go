// Profile endpoints: GET profile, update name, update email (password-verified).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/auth"
)

// profileUserID resolves the current session user ("" when anonymous).
func (a *AuthHandlers) profileUserID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" || a.Sessions == nil {
		return ""
	}
	uid, issued, gerr := a.Sessions.GetCreated(r.Context(), c.Value)
	if gerr != nil {
		return ""
	}
	// Password reset invalidation (issue #62): a session issued before the
	// user's last password reset is dead.
	if u, e := a.Users.ByID(r.Context(), uid); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		_ = a.Sessions.Destroy(r.Context(), c.Value)
		return ""
	}
	return uid
}

// handleProfile: GET /v1/auth/profile
func (a *AuthHandlers) handleProfile(w http.ResponseWriter, r *http.Request) {
	uid := a.profileUserID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	u, err := a.Users.ByID(r.Context(), uid)
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "pengguna tidak dijumpai")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "email": u.Email, "name": u.Name,
		"totp_enabled": u.TOTPEnabled, "created_at": u.CreatedAt,
	})
}

// handleUpdateName: POST /v1/auth/profile/name
func (a *AuthHandlers) handleUpdateName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := a.profileUserID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > 80 {
		name = name[:80]
	}
	if err := a.Users.UpdateName(r.Context(), uid, name); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleUpdateEmail: POST /v1/auth/profile/email (requires current password)
func (a *AuthHandlers) handleUpdateEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := a.profileUserID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !auth.ValidEmail(email) {
		writeErr(w, http.StatusBadRequest, "email tidak sah")
		return
	}
	u, err := a.Users.ByID(r.Context(), uid)
	if err != nil || u == nil {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	if ok, _ := auth.VerifyPassword(req.Password, u.PasswordHash); !ok {
		writeErr(w, http.StatusForbidden, "kata laluan salah")
		return
	}
	if existing, e2 := a.Users.ByEmail(r.Context(), email); e2 == nil && existing != nil && existing.ID != uid {
		writeErr(w, http.StatusConflict, "email sudah digunakan")
		return
	}
	if err := a.Users.UpdateEmail(r.Context(), uid, email); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
