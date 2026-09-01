// Issue #13: ONE aggregated dashboard-bundle endpoint to kill the
// 6-10 fetch waterfall on the settings page. Each section is best-effort:
// a section that errors is returned as null — the bundle never fails.
package httpapi

import (
	"context"
	"net/http"

	"github.com/wzul/jomform/api/internal/cryptoutil"
)

// chipStatusData builds the STORED CHIP credential status for an account
// (no live CHIP probe — kept fast for aggregate reads). Extracted from
// chipStatus in rest_chip.go so both the standalone endpoint and the
// dashboard bundle share one core.
func (h *RESTHandlers) chipStatusData(ctx context.Context, accountID string) (map[string]any, error) {
	acc, err := h.Accounts.ByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	configured := acc.ChipBrandID != "" && acc.ChipSecretKeyEnc != ""
	resp := map[string]any{
		"configured": configured,
		"mode":       acc.ChipMode,
		"brand_id":   acc.ChipBrandID,
	}
	if configured {
		if secret, err := cryptoutil.Decrypt(h.SecretsKey, acc.ChipSecretKeyEnc); err == nil {
			// Wan request: merchant sees what they stored (Woo-style secret_key field).
			resp["secret_key"] = string(secret)
		}
	}
	resp["whitelist"] = acc.ChipMethodWhitelist
	resp["email_fallback"] = acc.ChipEmailFallback
	return resp, nil
}

// dashboardBundle: GET /v1/rest/dashboard-bundle — one authed round-trip
// returning me, spaces, chip_status (stored only), keys, domains, webhooks,
// social identities and e-invoice settings. Section errors → null.
func (h *RESTHandlers) dashboardBundle(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ctx := r.Context()
	userID := h.sessionOwnerID(r)
	if userID == "" {
		userID = actx.Account.OwnerUserID
	}

	bundle := struct {
		Me         *map[string]any  `json:"me"`
		Spaces     []map[string]any `json:"spaces"`
		ChipStatus map[string]any   `json:"chip_status"`
		Keys       []restKey        `json:"keys"`
		Domains    []map[string]any `json:"domains"`
		Webhooks   []map[string]any `json:"webhooks"`
		Social     []map[string]any `json:"social"`
		Einvoice   map[string]any   `json:"einvoice"`
	}{}

	// me — session user's email + name.
	if h.Users != nil {
		if u, err := h.Users.ByID(ctx, userID); err == nil && u != nil {
			bundle.Me = &map[string]any{"id": u.ID, "email": u.Email, "name": u.Name}
		}
	}

	// spaces owned by the session user.
	if h.Spaces != nil {
		if spaces, err := h.Spaces.ByOwner(ctx, userID); err == nil {
			out := make([]map[string]any, 0, len(spaces))
			for _, s := range spaces {
				out = append(out, map[string]any{"id": s.ID, "name": s.Name, "slug": s.Slug})
			}
			bundle.Spaces = out
		}
	}

	// chip_status — stored credentials only (no live CHIP probe: speed).
	if st, err := h.chipStatusData(ctx, actx.Account.ID); err == nil {
		bundle.ChipStatus = st
	}

	// API keys.
	if h.Keys != nil {
		if keys, err := h.Keys.ListByAccount(ctx, actx.Account.ID); err == nil {
			out := make([]restKey, 0, len(keys))
			for _, k := range keys {
				out = append(out, restKey{ID: k.ID, Name: k.Name, Scopes: k.Scopes})
			}
			bundle.Keys = out
		}
	}

	// custom domains.
	if h.Domains != nil {
		if domains, err := h.Domains.ListDomains(ctx, actx.Account.ID); err == nil {
			out := make([]map[string]any, 0, len(domains))
			for _, d := range domains {
				out = append(out, map[string]any{
					"id": d.ID, "domain": d.Domain, "provider": d.Provider, "verified": d.Verified,
				})
			}
			bundle.Domains = out
		}
	}

	// outbound webhooks.
	if h.Webhooks != nil {
		if hooks, err := h.Webhooks.ListByAccount(ctx, actx.Account.ID); err == nil {
			out := make([]map[string]any, 0, len(hooks))
			for _, wh := range hooks {
				out = append(out, map[string]any{"id": wh.ID, "url": wh.URL, "events": wh.Events})
			}
			bundle.Webhooks = out
		}
	}

	// linked social identities.
	if h.Users != nil && userID != "" {
		if links, err := h.Users.ListIdentities(ctx, userID); err == nil {
			out := make([]map[string]any, 0, len(links))
			for _, l := range links {
				out = append(out, map[string]any{
					"provider": l.Provider, "email": l.Email, "name": l.DisplayName,
					"picture": l.Picture, "linked_at": l.CreatedAt,
				})
			}
			bundle.Social = out
		}
	}

	// e-Invoice (LHDN) settings.
	if h.Accounts != nil {
		if tin, msic, err := h.Accounts.Einvoice(ctx, actx.Account.ID); err == nil {
			bundle.Einvoice = map[string]any{"tin": tin, "msic": msic}
		}
	}

	writeJSON(w, http.StatusOK, bundle)
}
