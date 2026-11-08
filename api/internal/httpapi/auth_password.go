// Change password endpoint (session required).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/auth"
)

func (a *AuthHandlers) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "not signed in")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
		// Issue #458: a magic session owned by a 2FA user must also prove a
		// fresh TOTP code. Non-2FA / non-magic sessions ignore this field.
		TOTPCode string `json:"totp_code"`
	}
	if err := decodeJSON(r, &req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "old_password and new_password are required")
		return
	}
	if len(req.NewPassword) < 10 {
		writeErr(w, http.StatusBadRequest, "the new password must be at least 10 characters")
		return
	}
	// Issue #458 (option C): a magic-link session for a 2FA-enabled user must
	// step up with a fresh TOTP code before rotating the password.
	if !a.gateMagicStepUp(w, r, user, req.TOTPCode) {
		return
	}
	ok, err := auth.VerifyPassword(req.OldPassword, user.PasswordHash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "the old password is incorrect")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Use reset semantics (stamp pw_reset_at) so a password change invalidates
	// every prior session AND every trusted device (issues #62 + #63).
	if err := a.Users.ResetPassword(r.Context(), user.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if a.Sessions != nil {
		_ = a.Sessions.TrustedDevices().RevokeAll(r.Context(), user.ID)
	}
	clearTrustCookie(w)
	// Invalidate all sessions except current? MVP: kill all, user re-logins.
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}
