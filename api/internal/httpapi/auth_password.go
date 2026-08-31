// Change password endpoint (session required).
package httpapi

import (
	"net/http"

	"github.com/wzul/jomform/api/internal/auth"
)

func (a *AuthHandlers) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := a.currentUser(r)
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "tidak log masuk")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "old_password dan new_password diperlukan")
		return
	}
	if len(req.NewPassword) < 10 {
		writeErr(w, http.StatusBadRequest, "kata laluan baharu mesti sekurang-kurangnya 10 aksara")
		return
	}
	ok, err := auth.VerifyPassword(req.OldPassword, user.PasswordHash)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "kata laluan lama tidak sah")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.Users.SetPasswordHash(r.Context(), user.ID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Invalidate all sessions except current? MVP: kill all, user re-logins.
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
}
