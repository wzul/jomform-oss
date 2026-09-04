// User-level connected MCP agents (issue #307). The OAuth token is bound to
// the user, so the dashboard can list which MCP clients a user has connected
// and disconnect any of them (revoking its tokens) — independent of which
// space is active. This is surfaced on the Profile → Connected/MCP tab.
package httpapi

import (
	"net/http"
)

// mcpAgentsOut is the JSON shape for one connected MCP agent.
type mcpAgentOut struct {
	ClientID   string `json:"client_id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at,omitempty"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// listMCPAgents returns the MCP clients the signed-in user has an active
// (non-revoked, unexpired) token for. User-level — it is the same across all
// of the user's spaces (issue #307).
func (a *AuthHandlers) listMCPAgents(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	if a.OAuth == nil {
		writeJSON(w, http.StatusOK, []mcpAgentOut{})
		return
	}
	tokens, err := a.OAuth.ConnectedAgentsByUser(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]mcpAgentOut, 0, len(tokens))
	for _, t := range tokens {
		row := mcpAgentOut{ClientID: t.ClientID, Name: t.ClientName}
		if !t.CreatedAt.IsZero() {
			row.CreatedAt = t.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		}
		if t.LastUsedAt != nil && !t.LastUsedAt.IsZero() {
			row.LastUsedAt = t.LastUsedAt.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeMCPAgent disconnects a connected MCP agent for the signed-in user by
// revoking every active token for that client (issue #307).
func (a *AuthHandlers) revokeMCPAgent(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	clientID := r.PathValue("client_id")
	if clientID == "" || a.OAuth == nil {
		writeErr(w, http.StatusBadRequest, "client_id wajib")
		return
	}
	if err := a.OAuth.RevokeAllByUserClient(r.Context(), userID, clientID); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #92: audit the disconnect. Record against the user's first owned
	// account (events.account_id is NOT NULL).
	if a.Accounts != nil {
		if accs, aerr := a.Accounts.ByOwner(r.Context(), userID); aerr == nil && len(accs) > 0 {
			a.recordAuthEvent(r, accs[0].ID, userID, "mcp.agent_disconnected", map[string]any{"client_id": clientID})
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
