// "Connected apps" — the dashboard inventory of OAuth credentials the signed-in
// person has handed to AI agents (card t_9a6cc85f).
//
// WHY A PER-TOKEN LIST AND NOT A PER-CLIENT ONE. Profile → MCP has listed one
// row per connected CLIENT since issue #307, which is enough to say "Claude is
// connected". It is not enough to answer the two questions a person actually
// has after a scope change: WHAT can this credential do, and WHICH credential
// is it. A scope change rotates the pair (the old one dies), so a client can
// legitimately hold several historical rows that are already dead — this list
// shows only LIVE pairs, one row each, with the scopes that pair carries.
//
// THE SCOPE LADDER LIVES HERE (route (a), the preferred one). The person is
// already authenticated on jomform.com, so one confirmed request is consent
// enough to WIDEN a grant — no email round trip. Every change is a ROTATION:
// the old row is revoked in the same transaction that inserts the new one, so a
// leaked refresh token stops working at the moment the user believes the
// credential changed. Downgrades need no confirmation (narrowing is always
// allowed) and are immediate for the same reason.
package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/oauthmeta"
)

// connectedAppOut is one live token pair as the dashboard renders it.
type connectedAppOut struct {
	ID               string   `json:"id"`
	ClientID         string   `json:"client_id"`
	Name             string   `json:"name"`
	Scopes           []string `json:"scopes"`
	CreatedAt        string   `json:"created_at,omitempty"`
	LastUsedAt       string   `json:"last_used_at,omitempty"`
	AccessExpiresAt  string   `json:"access_expires_at,omitempty"`
	RefreshExpiresAt string   `json:"refresh_expires_at,omitempty"`
}

// oauthAccessTTL / oauthRefreshTTL are the lifetimes a rotation issues. They
// mirror what the OAuth handler set uses, so a pair rotated from the dashboard
// lives exactly as long as one minted by the token endpoint.
func (a *AuthHandlers) oauthAccessTTL() time.Duration {
	if a.OAuthAccessTTL > 0 {
		return a.OAuthAccessTTL
	}
	return time.Hour
}

func (a *AuthHandlers) oauthRefreshTTL() time.Duration {
	if a.OAuthRefreshTTL > 0 {
		return a.OAuthRefreshTTL
	}
	return 30 * 24 * time.Hour
}

// listConnectedApps returns every live token pair the signed-in user holds.
func (a *AuthHandlers) listConnectedApps(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if a.OAuth == nil {
		writeJSON(w, http.StatusOK, []connectedAppOut{})
		return
	}
	apps, err := a.OAuth.ConnectedAppsByUser(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	out := make([]connectedAppOut, 0, len(apps))
	for _, app := range apps {
		row := connectedAppOut{
			ID: app.ID, ClientID: app.ClientID, Name: app.ClientName, Scopes: app.Scope,
			CreatedAt:       app.CreatedAt.UTC().Format(time.RFC3339),
			AccessExpiresAt: app.AccessExpiresAt.UTC().Format(time.RFC3339),
		}
		if app.LastUsedAt != nil && !app.LastUsedAt.IsZero() {
			row.LastUsedAt = app.LastUsedAt.UTC().Format(time.RFC3339)
		}
		if app.RefreshExpiresAt != nil {
			row.RefreshExpiresAt = app.RefreshExpiresAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeConnectedApp kills ONE token pair — its access token AND its refresh
// token — in a single UPDATE, because they are columns of the same row. That is
// the promise the button makes; a revoke that left the refresh token alive
// would hand the agent a fresh pair on its next refresh.
func (a *AuthHandlers) revokeConnectedApp(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if a.OAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "connected apps are not active")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	if _, perr := uuid.Parse(id); perr != nil {
		// A malformed id names no row. Answering 404 keeps it the same answer
		// as "not yours", instead of letting Postgres reject the uuid
		// comparison and turning a bad path into a 500.
		writeErr(w, http.StatusNotFound, "this connection is not active")
		return
	}
	if err := a.OAuth.RevokeTokenByIDForUser(r.Context(), userID, id); err != nil {
		if errors.Is(err, dbstore.ErrOAuthTokenMissing) {
			// Already revoked and somebody else's row are the same answer.
			writeErr(w, http.StatusNotFound, "this connection is not active")
			return
		}
		respondErr(w, r, err)
		return
	}
	if a.Accounts != nil {
		if accs, aerr := a.Accounts.ByOwner(r.Context(), userID); aerr == nil && len(accs) > 0 {
			a.recordAuthEvent(r, accs[0].ID, userID, "oauth.token_revoked", map[string]any{
				"token_id": id, "route": "dashboard",
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// setConnectedAppScope changes the scope of one live pair by ROTATING it: the
// old pair is revoked and a new one is issued and returned exactly once.
//
// An UPGRADE (read → readwrite) is the one direction that hands the credential
// new powers, so it requires `confirm: true` in the request: the user is already
// authenticated, and that flag is the "one confirm" that replaces an email round
// trip. A DOWNGRADE needs no flag — narrowing cannot be abused.
func (a *AuthHandlers) setConnectedAppScope(w http.ResponseWriter, r *http.Request) {
	userID := a.currentUserID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if a.OAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "connected apps are not active")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	if _, perr := uuid.Parse(id); perr != nil {
		writeErr(w, http.StatusNotFound, "this connection is not active")
		return
	}
	var in struct {
		Scope   string `json:"scope"`
		Confirm bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Exactly one advertised value. Membership is exact (the same rule the
	// authorize/token resolvers use) — no prefix matching, no unknown scope.
	newScope := []string{}
	for _, s := range oauthmeta.ScopesSupported() {
		if s == in.Scope {
			newScope = []string{s}
		}
	}
	if len(newScope) == 0 {
		writeErr(w, http.StatusBadRequest, "scope must be 'read' or 'readwrite'")
		return
	}

	apps, err := a.OAuth.ConnectedAppsByUser(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	var current *dbstore.ConnectedApp
	for i := range apps {
		if apps[i].ID == id {
			current = &apps[i]
			break
		}
	}
	if current == nil {
		writeErr(w, http.StatusNotFound, "this connection is not active")
		return
	}

	upgrade := scopeWider(newScope, current.Scope)
	downgrade := scopeWider(current.Scope, newScope)
	if upgrade && !in.Confirm {
		// A widening grant must be asked for explicitly. This is the "one
		// confirm" of route (a); without it the call is refused rather than
		// silently widening a credential.
		writeErr(w, http.StatusBadRequest, "widening a connection's scope needs confirm=true")
		return
	}
	if !upgrade && !downgrade {
		// Same width: still rotate (a no-op edit would leave the old pair live
		// while telling the caller nothing changed).
		if scopeSame(newScope, current.Scope) {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "unchanged", "scope": dbstore.ScopeString(current.Scope),
			})
			return
		}
	}

	now := time.Now().UTC()
	access := newOAuthToken()
	refresh := newOAuthToken()
	nt, oldScope, err := a.OAuth.RotateTokenScope(r.Context(), userID, id, newScope,
		hashToken(access), hashToken(refresh),
		now.Add(a.oauthAccessTTL()), now.Add(a.oauthRefreshTTL()))
	if err != nil {
		if errors.Is(err, dbstore.ErrOAuthTokenMissing) {
			writeErr(w, http.StatusNotFound, "this connection is not active")
			return
		}
		respondErr(w, r, err)
		return
	}

	accountID := nt.AccountID
	if accountID == "" && a.Accounts != nil {
		if accs, aerr := a.Accounts.ByOwner(r.Context(), userID); aerr == nil && len(accs) > 0 {
			accountID = accs[0].ID
		}
	}
	a.recordAuthEvent(r, accountID, userID, "oauth.scope_changed", map[string]any{
		"client_id": nt.ClientID, "token_id": nt.ID, "revoked_token_id": id,
		"from": dbstore.ScopeString(oldScope), "to": dbstore.ScopeString(newScope),
		"route": "dashboard",
	})

	// The new secrets are shown ONCE — the harness has to be given them (or it
	// must start a fresh device authorisation). This is the price of rotating
	// instead of editing: the old credential is dead the moment this returns.
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "rotated",
		"id":             nt.ID,
		"scope":          dbstore.ScopeString(nt.Scope),
		"previous_scope": dbstore.ScopeString(oldScope),
		"route":          "dashboard",
		"access_token":   access,
		"refresh_token":  refresh,
		"token_type":     "Bearer",
		"expires_in":     int(a.oauthAccessTTL().Seconds()),
		"note":           "The previous credential is revoked. Copy these now — they are shown once.",
	})
}

// scopeSame reports set equality for two scope slices (order-insensitive).
func scopeSame(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !scopeHas(b, s) {
			return false
		}
	}
	return true
}
