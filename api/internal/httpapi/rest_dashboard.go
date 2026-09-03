// Issue #13: ONE aggregated dashboard-bundle endpoint to kill the
// 6-10 fetch waterfall on the settings page. Each section is best-effort:
// a section that errors is returned as null — the bundle never fails.
package httpapi

import (
	"context"
	"net/http"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// bundleSalesLimit caps the recent-sales page embedded in the bundle; the
// web dashboard shows 10 rows per page and pages through /v1/rest/sales.
const bundleSalesLimit = 10

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

// summaryData computes the sales rollup over a set of accounts. Shared by
// GET /v1/rest/summary and the dashboard bundle (issue #29) so both stay
// identical.
func (h *RESTHandlers) summaryData(ctx context.Context, accountIDs []string) (map[string]any, error) {
	sales, err := h.Sales.ListByAccounts(ctx, accountIDs, 200)
	if err != nil {
		return nil, err
	}
	sum := map[string]int64{"pending": 0, "paid": 0, "confirmed": 0, "failed": 0, "cancelled": 0}
	var revenue int64
	for _, s := range sales {
		sum[s.Status]++
		if s.Status == dbstore.SalePaid || s.Status == dbstore.SaleConfirmed {
			revenue += s.TotalSen
		}
	}
	return map[string]any{"counts": sum, "revenue_sen": revenue}, nil
}

// dashboardBundle: GET /v1/rest/dashboard-bundle — one authed round-trip
// returning me, spaces, chip_status (stored only), keys, domains, webhooks,
// social identities, e-invoice settings AND the dashboard landing data
// (products, summary, recent sales, form/product analytics — issue #29).
// Section errors → null; the bundle never fails.
func (h *RESTHandlers) dashboardBundle(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	ctx := r.Context()
	userID := h.sessionOwnerID(r)
	if userID == "" {
		userID = actx.Account.OwnerUserID
	}

	bundle := struct {
		Me               *map[string]any        `json:"me"`
		Spaces           []map[string]any       `json:"spaces"`
		ChipStatus       map[string]any         `json:"chip_status"`
		Keys             []restKey              `json:"keys"`
		Domains          []map[string]any       `json:"domains"`
		Webhooks         []map[string]any       `json:"webhooks"`
		Social           []map[string]any       `json:"social"`
		Einvoice         map[string]any         `json:"einvoice"`
		Products         []restProduct          `json:"products"`
		Summary          map[string]any         `json:"summary"`
		Sales            map[string]any         `json:"sales"`
		FormAnalytics    []dbstore.FormStats    `json:"form_analytics"`
		ProductAnalytics []dbstore.ProductStats `json:"product_analytics"`
	}{}

	// me — session user's email + name (+ 2FA status + avatar, issue #133).
	if h.Users != nil {
		if u, err := h.Users.ByID(ctx, userID); err == nil && u != nil {
			// Issue #164: surface the user's default space so the frontend
			// knows whether to show the space-picker after login.
			defSpace := ""
			if ds, derr := h.Users.DefaultSpace(ctx, userID); derr == nil {
				defSpace = ds
			}
			bundle.Me = &map[string]any{
				"id": u.ID, "email": u.Email, "name": u.Name,
				"avatar_url": u.AvatarURL, "totp_enabled": u.TOTPEnabled, "created_at": u.CreatedAt,
				"default_space_id": defSpace,
			}
		}
	}

	// spaces owned by OR a member of the session user (issue #162).
	if h.Spaces != nil {
		if spaces, err := h.Spaces.ByMember(ctx, userID); err == nil {
			out := make([]map[string]any, 0, len(spaces))
			for _, s := range spaces {
				out = append(out, map[string]any{"id": s.SpaceID, "name": s.Name, "slug": s.Slug, "role": s.Role})
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

	// Issue #29: dashboard landing data in the same round-trip, so the
	// heaviest navigation (me → products+sales+summary+2×analytics waterfall)
	// becomes ONE request. Honours ?space=<slug> like the standalone list
	// endpoints; a scope error leaves these sections null.
	if h.Products != nil || h.Sales != nil {
		if scope, err := h.resolveAccountScope(r, actx); err == nil {
			if h.Products != nil {
				if products, err := h.Products.ListActiveByAccounts(ctx, scope.Accounts); err == nil {
					out := make([]restProduct, 0, len(products))
					for _, p := range products {
						out = append(out, restProduct{
							ID: p.ID, Name: p.Name, PriceSen: p.PriceSen,
							Type: p.Type, BillingPeriod: p.BillingPeriod, Active: p.Active,
						})
					}
					bundle.Products = out
				}
			}
			if h.Sales != nil {
				if sum, err := h.summaryData(ctx, scope.Accounts); err == nil {
					bundle.Summary = sum
				}
				if sales, total, err := h.Sales.ListByAccountsPage(ctx, scope.Accounts, 1, bundleSalesLimit); err == nil {
					rows := restSaleRows(sales)
					// Issue #106: surface the most recent receipt delivery
					// event per sale in the initial bundle too.
					emailStatus := h.latestEmailStatuses(ctx, sales)
					for i := range rows {
						rows[i].EmailStatus = emailStatus[rows[i].RefCode]
					}
					bundle.Sales = map[string]any{
						"items": rows, "page": 1,
						"limit": bundleSalesLimit, "total": total,
					}
				}
				if fs, err := h.Sales.FormAnalyticsAccounts(ctx, scope.Accounts); err == nil {
					bundle.FormAnalytics = fs
				}
				if ps, err := h.Sales.ProductAnalyticsAccounts(ctx, scope.Accounts); err == nil {
					bundle.ProductAnalytics = ps
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, bundle)
}
