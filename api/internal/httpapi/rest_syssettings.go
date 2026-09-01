// Public system settings (footer text, landing badge) + admin CRUD.
package httpapi

import (
	"net/http"
	"strings"
)

// publicSystemSettings serves whitelisted keys to anonymous callers
// (landing + public form footer).
func (h *RESTHandlers) publicSystemSettings(w http.ResponseWriter, r *http.Request) {
	allow := map[string]bool{"site_footer_text": true, "landing_badge": true}
	out := map[string]string{}
	for k := range allow {
		if v, err := h.SystemSettings.Get(r.Context(), k); err == nil && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// listSystemSettings (admin): all settings.
func (h *RESTHandlers) listSystemSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	all, err := h.SystemSettings.All(r.Context())
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, all)
}

// setSystemSetting (admin): upsert one key.
func (h *RESTHandlers) setSystemSetting(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Key == "" {
		writeErr(w, http.StatusBadRequest, "key wajib")
		return
	}
	if !strings.HasPrefix(in.Key, "site_") && !strings.HasPrefix(in.Key, "landing_") {
		writeErr(w, http.StatusBadRequest, "key tidak dibenarkan")
		return
	}
	if err := h.SystemSettings.Set(r.Context(), in.Key, in.Value); err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
