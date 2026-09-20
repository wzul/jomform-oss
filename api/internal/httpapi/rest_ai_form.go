// AI form builder (issue #256): the merchant types a natural-language prompt
// in the builder UI; the backend calls deepseek-v4-flash:0731 (Ollama Cloud,
// via the internal llm client) to draft a form config (blocks + styling),
// validates it against the known block types, and returns it for the user to
// review/edit before saving. The LLM output is treated as untrusted — it is
// validated server-side (formgen) before it becomes a form, and product
// prices always resolve from the product DB (server authority), never the
// LLM. The endpoint is rate-limited because LLM calls cost money.
package httpapi

import (
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/formgen"
)

// aiGenerateForm: POST /v1/rest/forms/ai-generate
// {prompt: "a booking form for a hair salon with name, phone, date, service dropdown"}
// Calls the LLM to draft a form config, validates it, and returns it for the
// user to review/edit before saving. Rate-limited per IP (LLM calls cost
// money). Returns 503 when the LLM client is not configured.
func (h *RESTHandlers) aiGenerateForm(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	if _, ok := h.requireWriteRole(w, r, actx); !ok {
		return
	}
	if h.LLM == nil {
		writeErr(w, http.StatusServiceUnavailable, "penjanaan AI tidak tersedia")
		return
	}
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		writeErr(w, http.StatusBadRequest, "prompt wajib")
		return
	}
	if len(prompt) > 2000 {
		writeErr(w, http.StatusBadRequest, "prompt terlalu panjang (maks 2000 aksara)")
		return
	}
	raw, err := h.LLM.GenerateFormConfig(r.Context(), formgen.Prompt(prompt))
	if err != nil {
		// A rejected credential means "AI generation is not available" — the
		// same 503 as the nil-client branch above — rather than a bare 500 that
		// tells the merchant nothing and hides a deployment problem.
		respondAIErr(w, r, err, "penjanaan AI tidak tersedia")
		return
	}
	cfg, err := formgen.Parse(raw)
	if err != nil {
		// The model produced something we can't validate — surface it as a
		// client error so the user can retry with a clearer prompt.
		writeErr(w, http.StatusUnprocessableEntity, "penjanaan tidak sah: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}
