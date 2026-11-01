// Publish readiness — the ONE rule that decides whether a form may go live,
// and what the user is told when it may not.
//
// THE GAP (onboarding drop-off, not a crash). Production showed three outside
// signups building a form, saving it as a draft, and never publishing. Nothing
// blocked them: the publish handler is reachable and returns 200. Two of the
// three drafts had no `products` key at all in their config while still being
// payment-mode forms — so had they published, the form could never record a
// sale and no surface would have said so. Publishing succeeded silently and the
// mistake only surfaced later (at the first real buyer, or never).
//
// This file is the missing answer to "can this form actually do its job?":
//
//   - a RESPONSE form needs nothing — it collects answers, it is a legitimate
//     form with no products, and it must never be blocked (over-blocking here
//     would be the worse failure: the mode exists precisely for product-free
//     forms);
//   - a PAYMENT form with nothing to sell has no way to take money. It is
//     reported as unready, and the merchant has to say they meant it.
//
// Two deliberate non-rules, so this stays a rule and not a policy engine:
//
//   - `menu` mode is exempt. A form whose per-form restaurant menu is ENABLED
//     (#497) sells through its menu page, and the account's product list can
//     supply the items — requiring a `products` snapshot on the form config
//     would refuse a form that demonstrably works.
//   - the rule never consults the account's products table. The snapshot in the
//     config is what the builder writes, and it is what the form's own blocks
//     reference (public.go's renderer and lookupProduct both read the config).
//     A form with no blocks referencing anything but an account-level product
//     list is the legacy shape and is out of scope here.
//
// The rule lives in dbstore rather than in an individual handler because three
// write paths reach "published" (REST publishForm, REST setFormStatus, MCP
// publish_form) and they must not be able to disagree.
package dbstore

import (
	"context"
	"strings"
)

// Publish readiness outcomes. Ready is the zero value: anything this rule does
// not recognise as unready stays publishable, so a future form shape can never
// be blocked by omission.
const (
	// PublishReady means the form can go live with no caveat.
	PublishReady = ""
	// PublishWarningNoProducts means a payment form has nothing to sell. The
	// caller may proceed, but only when the user explicitly acknowledges it.
	PublishWarningNoProducts = "no_products"
)

// PaymentFormWithoutProducts reports whether this form is a payment form that
// has nothing to sell (no products in its config, no product to buy as a line
// item) — the shape that publishes fine today and can never take money.
//
// A response form is never reported: collecting answers is its whole job.
func PaymentFormWithoutProducts(f *Form) bool {
	if f == nil {
		return false
	}
	if FormMode(f) == FormModeResponse {
		return false
	}
	// An enabled per-form menu is its own storefront (issue #497); its items
	// come from the form's product blocks and the account's product list, so a
	// form without a `products` snapshot still sells.
	if m := MenuFromConfig(f.Config); m != nil && m.Enabled {
		return false
	}
	return !formHasSellableItem(f.Config)
}

// IsLiveStatus reports whether a status serves the form to buyers at its public
// URL — published, unlisted and private all do (private behind its password
// gate); draft does not.
func IsLiveStatus(status string) bool {
	switch status {
	case FormStatusPublished, FormStatusUnlisted, FormStatusPrivate:
		return true
	}
	return false
}

// OGImagePubliclyCached reports whether the form's generated social card
// (/og/{slug}.png) is served to ANYONE for this status, and therefore may be
// sitting in a shared CDN cache under a public freshness lifetime.
//
// published and unlisted are the two arms httpapi.handleOGImage serves with
// `Cache-Control: public`; draft answers 404 with the same body as an unknown
// slug, and private answers 404 unless the caller holds that form's unlock
// cookie (and is served `private, no-store`), so neither can be in a shared
// cache. This is a different question from IsLiveStatus — private IS live but
// is never publicly cached — and it is what decides whether a status change has
// to purge the edge: a cached card keeps naming the form's title, workspace and
// host after its page has stopped serving it (measured on prod: a draft form's
// card answered from Cloudflare with `age: 1243` while the cache-busted origin
// answered 404).
//
// A write path that moves a form OUT of this set must purge `/og/<slug>.png` AND
// `/qr/<slug>.png` in the same request (see internal/cdnpurge — the QR PNG is
// served on exactly these two arms by httpapi.handleQRImage, so it shares this
// predicate); one that moves it IN does not — nothing publicly cacheable was
// there before (the 404 arm gets the CDN's default few-minute TTL, which is why a
// publish flips within minutes). The name is the card's because that was the
// first artefact this rule guarded; the set is "forms whose generated artefacts
// are publicly cacheable", not "forms that have a card".
func OGImagePubliclyCached(status string) bool {
	switch status {
	case FormStatusPublished, FormStatusUnlisted:
		return true
	}
	return false
}

// PublishNeedsAck reports whether moving f to `status` requires the caller to
// acknowledge that a payment form has nothing to sell.
//
// It is a TRANSITION rule, and that is load-bearing: a form that is already
// live is never asked again. Saving an unrelated setting re-sends the same
// status (the dashboard's status control posts it on every save), so a
// non-transition guard would demand an acknowledgement every time a merchant
// edited the title of a form it had deliberately published empty.
func PublishNeedsAck(f *Form, status string) bool {
	if f == nil || f.Status == status {
		return false
	}
	if !IsLiveStatus(status) || IsLiveStatus(f.Status) {
		return false
	}
	return PaymentFormWithoutProducts(f)
}

// formHasSellableItem reports whether a form's config carries at least one
// thing a buyer could pay for: a snapshot product with an id, a product block
// pointing at a product id, or a pricing block (option/shipping) carrying its
// own price — the same three sources the renderer and the order path resolve
// line items from.
func formHasSellableItem(config map[string]any) bool {
	if config == nil {
		return false
	}
	products, _ := config["products"].([]any)
	for _, raw := range products {
		if p, ok := raw.(map[string]any); ok && idString(p["id"]) != "" {
			return true
		}
	}
	blocks, _ := config["blocks"].([]any)
	for _, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(idString(b["type"])) {
		case "product":
			if idString(b["product_id"]) != "" {
				return true
			}
		case "option", "shipping":
			// A pricing block is a line item in its own right; its choices can
			// carry the price (issue #173).
			if priceSen(b["price_sen"]) > 0 {
				return true
			}
			if choices, ok := b["choices"].([]any); ok {
				for _, c := range choices {
					if cm, ok := c.(map[string]any); ok && priceSen(cm["price_sen"]) > 0 {
						return true
					}
				}
			}
		}
	}
	return false
}

// PublishNeedsAckIDs returns the subset of ids that a bulk publish must not
// flip live silently: payment forms with nothing to sell that are not already
// live. The caller reports them back to the user instead of publishing them.
//
// The check is one query then the same Go rule (PaymentFormWithoutProducts) —
// not a second SQL copy of it — so a bulk publish cannot disagree with the
// single-form publish (issue #594 already set this pattern for the gallery
// skip). Ids the account does not own are simply absent, which is how the
// bulk path treats them anyway.
func (s *FormStore) PublishNeedsAckIDs(ctx context.Context, accountID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, config, status FROM forms
		  WHERE account_id=$1 AND deleted_at IS NULL AND id = ANY($2)`,
		accountID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			id     string
			config map[string]any
			status string
		)
		if err := rows.Scan(&id, &config, &status); err != nil {
			return nil, err
		}
		f := &Form{Config: config, Status: status}
		if PublishNeedsAck(f, FormStatusPublished) {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// idString trims a JSON-decoded string value, or "" for any other type.
func idString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// priceSen reads a JSON number as sen. encoding/json decodes every number into
// float64; a string form ("500") is accepted too because an agent-authored
// config can carry either. Anything else is 0.
func priceSen(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case string:
		var out int64
		for _, r := range strings.TrimSpace(n) {
			if r < '0' || r > '9' {
				return 0
			}
			out = out*10 + int64(r-'0')
		}
		return out
	default:
		return 0
	}
}
