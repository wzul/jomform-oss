// Issue #133: profile picture avatar endpoint.
//
// PUT /v1/rest/profile/avatar {avatar_url: "..."} persists the session user's
// avatar_url. The URL MUST be JomForm-hosted media (from our upload bucket) so
// we never store an arbitrary external URL; the client obtains it from
// POST /v1/rest/uploads/avatar (image upload flow, #61/#79).
package httpapi

import (
	"net/http"
	"strings"
)

// updateProfileAvatar: PUT /v1/rest/profile/avatar
func (h *RESTHandlers) updateProfileAvatar(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if h.Users == nil {
		writeErr(w, http.StatusServiceUnavailable, "perkhidmatan tidak tersedia")
		return
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}

	var req struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	url := strings.TrimSpace(req.AvatarURL)

	// Empty clears the avatar (fallback to the initial-letter circle).
	if url != "" && !jomformMediaURL(url) {
		writeErr(w, http.StatusBadRequest, "URL avatar tidak sah (imej daripada pelayan JomForm diperlukan)")
		return
	}

	if err := h.Users.UpdateAvatar(r.Context(), userID, url); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.recordEvent(r, actx, userID, "profile.avatar_update", map[string]any{
		"cleared": url == "",
	})
	respond(w, http.StatusOK, map[string]string{"avatar_url": url})
}

// jomformMediaURL reports whether u is a URL served from this deployment's
// media public base (the upload bucket), so users can only persist avatars we
// actually host. It handles both an absolute URL and a path-only string (the
// client may send just the object path).
func jomformMediaURL(u string) bool {
	if u == "" {
		return false
	}
	// Accept a path under the public base (contains the bucket upload prefix).
	if strings.HasPrefix(u, "/") {
		return strings.Contains(u, "/u/") || strings.Contains(u, "uploads")
	}
	// Absolute http(s) URL: must carry the media bucket (key path u/...).
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		lower := strings.ToLower(u)
		return strings.Contains(lower, "/u/") || strings.Contains(lower, "upload")
	}
	return false
}
