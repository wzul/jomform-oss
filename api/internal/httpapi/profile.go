// Profile endpoints: GET profile, update name, update email (password-verified).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
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
		"avatar_url": u.AvatarURL, "phone": u.Phone, "timezone": u.Timezone, "lang": u.Lang, "totp_enabled": u.TOTPEnabled, "created_at": u.CreatedAt,
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

// handleUpdatePhone: POST /v1/auth/profile/phone (issue #181). Optional — a
// user can set a phone number or leave it blank. Empty string clears it.
// Format is validated loosely (digits, +, workspaces, 8-15 chars) but not
// over-restricted, since the field is optional.
func (a *AuthHandlers) handleUpdatePhone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
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
	phone := strings.TrimSpace(req.Phone)
	if phone != "" {
		digits := 0
		for _, c := range phone {
			switch {
			case c >= '0' && c <= '9':
				digits++
			case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
				// allowed separators
			default:
				writeErr(w, http.StatusBadRequest, "nombor telefon tidak sah")
				return
			}
		}
		if digits < 8 || digits > 15 {
			writeErr(w, http.StatusBadRequest, "nombor telefon tidak sah")
			return
		}
	}
	if err := a.Users.UpdatePhone(r.Context(), uid, phone); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleUpdateTimezone: POST /v1/auth/profile/timezone (issue #191). Optional —
// a user can hard-code an IANA timezone or leave it blank (Auto = follow the
// browser/PC). Empty string clears it. The value is validated against the
// IANA timezone database via time.LoadLocation.
func (a *AuthHandlers) handleUpdateTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timezone string `json:"timezone"`
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
	tz := strings.TrimSpace(req.Timezone)
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			writeErr(w, http.StatusBadRequest, "zona waktu tidak sah")
			return
		}
	}
	if err := a.Users.UpdateTimezone(r.Context(), uid, tz); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleUpdateLang: POST /v1/auth/profile/lang (language placement). Sets the
// user's PER-PROFILE dashboard UI language. This is deliberately separate from
// the workspace's buyer-facing default_lang and from a form's config.lang: it
// decides what the DASHBOARD speaks to this person, in every browser. Empty
// string clears it (falls back to the legacy browser value, then English).
func (a *AuthHandlers) handleUpdateLang(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lang string `json:"lang"`
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
	lang := strings.TrimSpace(req.Lang)
	if !dbstore.ValidProfileLang(lang) {
		writeErr(w, http.StatusBadRequest, "bahasa tidak disokong")
		return
	}
	if err := a.Users.UpdateLang(r.Context(), uid, lang); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "lang": lang})
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

// handleSetDefaultWorkspace: PUT /v1/auth/profile/default-workspace (issue #164).
// Body: {"workspace_id": "<id>"} to set the default, or {"workspace_id": ""} to clear
// it. The workspace must belong to the user (owner or member) — otherwise 403.
func (a *AuthHandlers) handleSetDefaultWorkspace(w http.ResponseWriter, r *http.Request) {
	uid := a.profileUserID(r)
	if uid == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	var req struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)

	// Clearing the default: no ownership check needed.
	if workspaceID == "" {
		if err := a.Users.ClearDefaultWorkspace(r.Context(), uid); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
		return
	}

	// Setting the default: the workspace must belong to the user (owner or member).
	if a.Workspaces == nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	sp, err := a.Workspaces.ByID(r.Context(), workspaceID)
	if err != nil || sp == nil || sp.Owner != uid {
		writeErr(w, http.StatusForbidden, "workspace bukan milik anda")
		return
	}
	if err := a.Users.SetDefaultWorkspace(r.Context(), uid, workspaceID); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "workspace_id": workspaceID})
}
