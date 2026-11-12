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
// Card t_983425e5 — THE SECOND HALF: the rule used to ask a DIFFERENT question
// from the one the buyer page answers, and that gap let a dead form publish
// cleanly. `formHasSellableItem` counted any `config.products` snapshot entry as
// "something to sell", but a BLOCK layout paints its price controls from
// `blocks` only: `blocks:[name,email,phone,product(value:"", no product_id)]`
// with the workspace catalogue still snapshotted in `products` was "ready" to
// the write path while `public.go` served the submit-less "This form is not
// accepting orders right now" page (measured on prod form
// e1fcc6cc-218d-4075-90b0-b2b8e03944c6 in workspace `test-wan`; the product block
// had `"value":""` and no `product_id` at all). The snapshot arm is now scoped
// to the LEGACY layout (no blocks), which is the only layout that paints one row
// per snapshot.
//
// The same card also asked the sibling question: a product block pointing at a
// product row that was later PURGED. The config still carries the id, so the
// config-only rule cannot see it; NotAcceptingOrders takes the account's product ids
// when the caller has them, and reports the block as unpointed too. An ARCHIVED
// or TRASHED product still has a row (and the renderer/order path fall back to
// the form's snapshot for it), so it is deliberately not reported — the prior
// decision that "a merchant whose product was archived must not be nagged by a
// publish guard" is preserved.
//
// Two deliberate non-rules, so this stays a rule and not a policy engine:
//
//   - `menu` mode is exempt. A form whose per-form restaurant menu is ENABLED
//     (#497) sells through its menu page, and the account's product list can
//     supply the items — requiring a `products` snapshot on the form config
//     would refuse a form that demonstrably works.
//   - when the caller passes no product ids (nil), the purged-product arm is
//     skipped rather than guessed. Every production write path passes the set.
//
// The rule lives in dbstore rather than in an individual handler because three
// write paths reach "published" (REST publishForm, REST setFormStatus, MCP
// publish_form) and they must not be able to disagree — and because the
// dashboard's form list asks the same question (NotAcceptingOrders) and must not
// answer it a second way.
package dbstore

import (
	"context"
	"encoding/json"
	"strings"
)

// Publish readiness outcomes. Ready is the zero value: anything this rule does
// not recognise as unready stays publishable, so a future form shape can never
// be blocked by omission.
const (
	// PublishReady means the form can go live with no caveat.
	PublishReady = ""
	// PublishWarningNoProducts means a payment form has nothing to sell at all
	// (no product block, no legacy snapshot, no priced pricing block). The caller
	// may proceed, but only when the user explicitly acknowledges it.
	PublishWarningNoProducts = "no_products"
	// PublishWarningNoProductSelected means a payment form HAS a product block,
	// but that block is not pointed at a product the account can sell (its
	// `product_id` is empty, or the product row no longer exists). This is the
	// shape card t_983425e5 measured: the merchant believed a product was
	// selected, and the buyer page answered "This form is not accepting orders
	// right now". The code is distinct from `no_products` so the dashboard can
	// name the block and link to it instead of talking about a form that "has no
	// products".
	PublishWarningNoProductSelected = "no_product_selected"
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
// there before (the 404 arm gets the CDN's default few-minute TTL, which is why
// a publish flips within minutes). The name is the card's because that was the
// first artefact this rule guarded; the set is "forms whose generated artefacts
// are publicly cacheable", not "forms that have a card".
func OGImagePubliclyCached(status string) bool {
	switch status {
	case FormStatusPublished, FormStatusUnlisted:
		return true
	}
	return false
}

// PublishWarning reports why moving f to `status` needs the caller's
// acknowledgement, as a machine code (PublishReady when it does not), and the id
// of the block the merchant has to fix when there is one.
//
// `liveProductIDs` is the set of product ids the form's account still HAS A ROW
// FOR (archived and trashed included — those still resolve through the form's
// snapshot). It is what lets the rule see a block pointing at a PURGED product.
// nil means "the caller has no product table", which skips that arm rather than
// guessing; every production write path passes the set.
//
// It is a TRANSITION rule, and that is load-bearing: a form that is already
// live is never asked again. Saving an unrelated setting re-sends the same
// status (the dashboard's status control posts it on every save), so a
// non-transition guard would demand an acknowledgement every time a merchant
// edited the title of a form it had deliberately published empty.
func PublishWarning(f *Form, status string, liveProductIDs map[string]bool) (code, blockID string) {
	if f == nil || f.Status == status {
		return PublishReady, ""
	}
	if !IsLiveStatus(status) || IsLiveStatus(f.Status) {
		return PublishReady, ""
	}
	if FormMode(f) == FormModeResponse {
		return PublishReady, ""
	}
	if m := MenuFromConfig(f.Config); m != nil && m.Enabled {
		return PublishReady, ""
	}
	id, dead := NotAcceptingOrders(f, liveProductIDs)
	if !dead {
		return PublishReady, ""
	}
	if len(productBlockRefs(f.Config)) > 0 {
		return PublishWarningNoProductSelected, id
	}
	return PublishWarningNoProducts, ""
}

// NotAcceptingOrders answers the DASHBOARD's question, with the block id to
// point the merchant at: is this payment form's public page going to tell every
// buyer "This form is not accepting orders right now"?
//
// It is the same predicate the publish guard uses (PublishWarning calls it), so
// a form the list marks dead is a form the publish route refuses to take live
// silently — and a form whose publish was acknowledged still shows the badge.
// The buyer page itself is not involved: it asks the block renderer what it can
// actually paint (httpapi.blockLayoutPaintsNoLineItemControl), and this rule is
// written to agree with it.
func NotAcceptingOrders(f *Form, liveProductIDs map[string]bool) (blockID string, dead bool) {
	if f == nil || FormMode(f) == FormModeResponse {
		return "", false
	}
	if m := MenuFromConfig(f.Config); m != nil && m.Enabled {
		return "", false
	}
	refs := productBlockRefs(f.Config)
	if PaymentFormWithoutProducts(f) {
		// Nothing sellable at the config level. Point at the product block that
		// is empty when there is one; otherwise there is no single control to
		// blame and the caller falls back to its own sentence.
		for _, r := range refs {
			if r.ProductID == "" {
				return r.ID, true
			}
		}
		if len(refs) > 0 {
			return refs[0].ID, true
		}
		return "", true
	}
	// The config-level rule says sellable, so the only remaining way to be dead
	// is a product block naming a product row that no longer exists.
	if liveProductIDs != nil {
		for _, r := range refs {
			if r.ProductID != "" && !liveProductIDs[r.ProductID] {
				return r.ID, true
			}
		}
	}
	return "", false
}

// productBlockRef is one `type:"product"` block: the id the builder and the
// dashboard link to, and the product it tries to sell ("" when unset).
type productBlockRef struct{ ID, ProductID string }

// productBlockRefs lists the product blocks of a config, in block order.
func productBlockRefs(config map[string]any) []productBlockRef {
	blocks, _ := config["blocks"].([]any)
	var out []productBlockRef
	for _, raw := range blocks {
		b, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(idString(b["type"])) != "product" {
			continue
		}
		out = append(out, productBlockRef{ID: idString(b["id"]), ProductID: idString(b["product_id"])})
	}
	return out
}

// formHasSellableItem reports whether a form's config carries at least one
// thing a buyer could pay for.
//
// TWO LAYOUTS, TWO SOURCES — and they must not be mixed:
//
//   - a config with BLOCKS is the block layout. The renderer paints its price
//     controls from `blocks` alone, so a product block with a product id (or a
//     pricing block with a selectable choice) is the only thing that can become
//     a line item. A `products` snapshot with an id does NOT make that page
//     sellable — measured for card t_bfdcafbc: `blocks:[name,email]` +
//     `products:[{id:…}]` paints "Proceed to Payment" with zero product/option/
//     shipping controls. Counting the snapshot here is what let card
//     t_983425e5's form publish while its buyer page said "not accepting
//     orders".
//   - a config with NO blocks is the legacy layout, whose formTmpl paints one
//     checkbox per `products` snapshot entry. There the snapshot IS the source.
func formHasSellableItem(config map[string]any) bool {
	if config == nil {
		return false
	}
	blocks, _ := config["blocks"].([]any)
	if len(blocks) > 0 {
		return blockLayoutHasLineItem(config)
	}
	products, _ := config["products"].([]any)
	for _, raw := range products {
		if p, ok := raw.(map[string]any); ok && idString(p["id"]) != "" {
			return true
		}
	}
	return false
}

// BlockLayoutPaintsNoLineItemControl reports whether a payment form's BLOCK
// layout paints no control the buyer can add to the cart — the page whose submit
// handler can only reach `if(!items.length){alert(...);return;}`. httpapi asks
// this before rendering (it then serves the submit-less "not accepting orders"
// page), and the publish rule above answers the same question, so the page and
// the guard cannot disagree.
//
// The three control kinds the block payment template paints, and what makes each
// buyable:
//
//   - product: the checkbox posts `data-id` verbatim, so an empty product id is
//     not a buyable control (the order path refuses it: `produk tidak sah: `);
//   - option: `select.opt` pushes an item only for a chosen non-empty value, and
//     the template's placeholder option IS empty;
//   - shipping: a ZONE/COURIER-sourced block's options are the zone set's (loaded
//     by the renderer), so it is assumed live; a static one needs its own choice
//     rows with non-empty values.
//
// A config this function cannot decode is NOT a block layout (the renderer
// unmarshals the same fields and falls through to the legacy layout), so it is
// never restricted here — a parse failure must not blank a page.
func BlockLayoutPaintsNoLineItemControl(f *Form) bool {
	if f == nil || f.Config == nil || FormMode(f) != FormModePayment {
		return false
	}
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return false
	}
	var cfg struct {
		Blocks []struct {
			Type      string `json:"type"`
			ProductID string `json:"product_id"`
			Source    string `json:"source"`
			Choices   []struct {
				Value string `json:"value"`
			} `json:"choices"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) != nil || len(cfg.Blocks) == 0 {
		return false
	}
	return !blockLayoutViewIsSellable(cfg.Blocks)
}

// blockLayoutHasLineItem is the map-based twin of
// BlockLayoutPaintsNoLineItemControl for configs already decoded from a store.
// Both read the same three rules; they are kept side by side so a change to one
// is visibly a change to the other.
func blockLayoutHasLineItem(config map[string]any) bool {
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
		case "option":
			if anySelectableChoiceMap(b["choices"]) {
				return true
			}
		case "shipping":
			if len(selectableChoicesMap(b["choices"])) > 0 ||
				firstNonEmptyString(idString(b["source"]), shippingSourceStatic) != shippingSourceStatic {
				return true
			}
		}
	}
	return false
}

// blockLayoutViewIsSellable is the decoded-struct half of the same rule.
func blockLayoutViewIsSellable(blocks []struct {
	Type      string `json:"type"`
	ProductID string `json:"product_id"`
	Source    string `json:"source"`
	Choices   []struct {
		Value string `json:"value"`
	} `json:"choices"`
}) bool {
	for _, b := range blocks {
		switch strings.TrimSpace(b.Type) {
		case "product":
			if strings.TrimSpace(b.ProductID) != "" {
				return true
			}
		case "option":
			for _, c := range b.Choices {
				if strings.TrimSpace(c.Value) != "" {
					return true
				}
			}
		case "shipping":
			if firstNonEmptyString(strings.TrimSpace(b.Source), shippingSourceStatic) != shippingSourceStatic {
				return true
			}
			for _, c := range b.Choices {
				if strings.TrimSpace(c.Value) != "" {
					return true
				}
			}
		}
	}
	return false
}

// shippingSourceStatic mirrors shipping.SourceStatic without importing the
// package (dbstore must not depend on the shipping wiring for one string).
const shippingSourceStatic = "static"

func anySelectableChoiceMap(v any) bool {
	return len(selectableChoicesMap(v)) > 0
}

// selectableChoicesMap returns the choices whose `value` the buyer can submit.
// The page's submit handler skips an empty value (`if(o&&o.value)`), and the
// template's placeholder option is empty, so only non-empty values count.
func selectableChoicesMap(v any) []map[string]any {
	choices, _ := v.([]any)
	var out []map[string]any
	for _, raw := range choices {
		if m, ok := raw.(map[string]any); ok && idString(m["value"]) != "" {
			out = append(out, m)
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// PublishNeedsAckIDs returns the subset of ids that a bulk publish must not
// flip live silently: payment forms that cannot take money and are not already
// live. The caller reports them back to the user instead of publishing them.
//
// The check is one query for the rows, one for the account's product ids, then
// the same Go rule (PublishWarning) — not a second SQL copy of it — so a bulk
// publish cannot disagree with the single-form publish (issue #594 already set
// this pattern for the gallery skip). Ids the account does not own are simply
// absent, which is how the bulk path treats them anyway.
func (s *FormStore) PublishNeedsAckIDs(ctx context.Context, accountID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	live, err := ProductIDsForAccount(ctx, s.pool, accountID)
	if err != nil {
		return nil, err
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
		if code, _ := PublishWarning(f, FormStatusPublished, live); code != PublishReady {
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
