// Issue #491: workspace-level order endpoint (menu checkout without a form).
//
// Historically every public order route is form-scoped — POST /f/{slug}/order
// and POST /{slug}/order both resolve a form and validate the submission
// against form.Config. The restaurant menu page (#438/#485), however, must be
// able to check out WITHOUT a form: the menu itself is the order target. That
// also fixes the wrong-product bug — menu items linked to
// firstPublishedFormSlug, so every item landed on the same first-published
// form (wrong name, wrong price, wrong product).
//
// This file holds the ONE shared order core plus the workspace-scoped entry
// point. handleSubmit (the form path) delegates to the same core, so there is
// exactly one submit implementation: the order target is a parameter.
//
// Route choice: POST /menu-order.
//
// A workspace slug and a form slug share the same URL namespace, so the
// workspace-scoped route must not be another single-segment /{slug}/order
// (which is already the form route) and must not be /menu/order either — the
// latter would shadow the order endpoint of a form whose slug is literally
// "menu". A dedicated single-segment literal path is unambiguous: Go's
// ServeMux keeps a literal segment distinct from every {slug} wildcard, and no
// form slug can ever reach it.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/payments"
)

// orderTarget is the destination of a public order. It is either a form (the
// existing form checkout) or a workspace/account (menu checkout, no form).
type orderTarget struct {
	// AccountID is the account that owns the sale. Always set.
	AccountID string
	// Form is the form the buyer is ordering through, or nil for a workspace
	// (menu) order. When nil, sale.form_id stays empty — the sale is still
	// fully valid (sales.form_id is nullable) and every downstream consumer
	// (CHIP purchase, paid webhook, receipt, notification) already handles a
	// sale with no form.
	Form *dbstore.Form
	// FieldsForm is the form whose registered-field contract (issue #254)
	// validates the submitted fields, if any. A form-scoped order validates
	// against the form itself; a menu order validates against an optional
	// attached extra-info form (part 3) and skips field validation entirely
	// when none is attached.
	FieldsForm *dbstore.Form
	// RateKey scopes the per-target anti-spam rate limit (issue #244, layer 2).
	// Form orders key on the form id; menu orders key on the workspace id, so
	// the protection is preserved on the formless path instead of silently
	// dropped.
	RateKey string
	// OwnerSkip skips the rate limit + time-based check for a logged-in
	// owner/admin of the owning workspace (merchant testing their own store).
	OwnerSkip bool
	// RequireName enforces a non-blank buyer name. CHIP needs a name AND an
	// email to create a purchase, so the menu path requires both (issue #491).
	// Form orders keep the historical rule (email required, name optional) so
	// existing forms — some of which have no name block at all — are unchanged.
	RequireName bool
}

// submitOrder is the single order core shared by the form-scoped routes
// (handleSubmit) and the workspace-scoped menu checkout (handleMenuOrder). It
// decodes the body, applies the mandatory-field, field-contract and anti-spam
// checks, resolves every line item's name/price LIVE from the products table,
// records the sale and returns the CHIP checkout URL (or the bank-direct
// next step).
//
// Behaviour for an existing form target is byte-for-byte the old handleSubmit:
// same validation, same anti-spam layers, same sale.form_id.
func (h *PublicHandlers) submitOrder(w http.ResponseWriter, r *http.Request, tgt orderTarget) {
	var in submitInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	if len(in.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	// Issue #491: CHIP requires a buyer name + email to create a purchase. The
	// formless (menu) path enforces both up front; a form order keeps its
	// historical rule (email required, name optional) so existing forms are
	// unchanged.
	if tgt.RequireName {
		if strings.TrimSpace(in.Client.FullName) == "" || strings.TrimSpace(in.Client.Email) == "" {
			writeErr(w, http.StatusBadRequest, "nama dan emel wajib")
			return
		}
	} else if in.Client.Email == "" {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	// Issue #254: server-side re-validation of registered fields. Agent JS is
	// client-side only and bypassable, so the server re-validates every field
	// against the form's registered contract before accepting the order. A
	// formless order with no attached form has no contract — nothing to
	// enforce (part 3 wires the optional attachment).
	if tgt.FieldsForm != nil {
		if err := formfields.Validate(tgt.FieldsForm.Config, in.Fields); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Issue #244: deterministic anti-spam layers. Each layer REJECTS the
	// submission (returns an error) rather than flagging it. The rate limit and
	// time-based check are skipped for a logged-in owner/admin of the owning
	// workspace (owner testing the store shouldn't be throttled).
	owner := tgt.OwnerSkip
	// Layer 1 — honeypot: a filled hidden field marks a bot.
	if h.antiSpamCheckHoneypot(r, &in) {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	// Layer 5 — email/domain validation (format + MX).
	if h.antiSpamCheckEmail(r, in.Client.Email) {
		writeErr(w, http.StatusBadRequest, "emel tidak sah")
		return
	}
	// Layer 2 — per-target rate limit (15/target/IP/hour), skipped for owner.
	if !owner && h.antiSpamCheckRateLimitKey(r, tgt.RateKey) {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak cubaan. Cuba lagi kemudian.")
		return
	}
	// Layer 3 — time-based check (reject too-fast submits), skipped for owner.
	if !owner && h.antiSpamCheckTime(r, &in) {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	method := in.PaymentMethod
	if method != "chip" && method != "bank_direct" {
		writeErr(w, http.StatusBadRequest, "kaedah pembayaran tidak sah")
		return
	}

	// Validate totals against the account currency (MVP: MYR only).
	if _, err := h.Accounts.ByID(r.Context(), tgt.AccountID); err != nil {
		respondErr(w, r, err)
		return
	}

	sale := &dbstore.Sale{
		AccountID: tgt.AccountID,
		RefCode:   genRef(),
		Client: resolveChipClient(tgt.Form, in.Fields, dbstore.SaleClient{
			FullName: in.Client.FullName, Email: in.Client.Email, Phone: in.Client.Phone,
			StreetAddress: in.Client.StreetAddress, City: in.Client.City, State: in.Client.State,
			ZipCode: in.Client.ZipCode, Country: in.Client.Country,
			LegalName: in.Client.LegalName, RegistrationNum: in.Client.RegistrationNum,
			TaxNumber: in.Client.TaxNumber,
		}),
		SaleType:      dbstore.SaleTypeOneTime,
		PaymentMethod: method,
		Status:        dbstore.SalePending,
		SkipCapture:   in.SkipCapture && method == "chip",
		// Issue #438: QR ordering flow — carry the table number the QR stand
		// encoded so the merchant knows where to serve.
		TableNumber: strings.TrimSpace(in.TableNumber),
		// Issue #444: affiliate marketing — carry the affiliate code the buyer
		// landed with so the sale is attributed to that affiliate.
		AffiliateCode: strings.TrimSpace(in.AffiliateCode),
	}
	// Issue #491: a formless order records no form — sales.form_id stays NULL.
	// Every consumer keys off a non-empty form id, so nothing else changes.
	if tgt.Form != nil {
		sale.FormID = tgt.Form.ID
	}
	// Issue #185: record the currency the sale is transacted in from the
	// workspace that owns the account (default MYR). Historical sales keep this
	// original currency — never auto-converted on a later change.
	if h.Workspaces != nil {
		if cur, cerr := h.Workspaces.WorkspaceCurrencyByAccount(r.Context(), tgt.AccountID); cerr == nil && cur != "" {
			sale.Currency = cur
		}
	}
	if sale.Currency == "" {
		sale.Currency = "MYR"
	}
	var subtotal int64
	for _, it := range in.Items {
		// Issue #461: the products table is the price authority — the name and
		// price resolve LIVE from the product row, with the form snapshot only
		// as the fallback for an archived/deleted product or a pricing block.
		// A formless (menu) order has no snapshot, so live is the only source.
		name, price := h.lookupProduct(r.Context(), tgt.AccountID, tgt.Form, it.ProductID)
		if price <= 0 && name == "" {
			writeErr(w, http.StatusBadRequest, "produk tidak sah: "+it.ProductID)
			return
		}
		qty := it.Quantity
		if qty < 1 {
			qty = 1
		}
		item := dbstore.SaleItem{ProductID: it.ProductID, Name: name, PriceSen: price, Qty: qty}
		// Issue #153: when the buyer chose a variation, validate it belongs to
		// the product, use the variation's price, and decrement the variation's
		// stock (not the product-level stock). The variation is persisted on the
		// sale so the receipt/merchant view shows which variation was bought.
		if it.VariationID != "" {
			if h.ProductVariations == nil {
				writeErr(w, http.StatusBadRequest, "variasi tidak sah: "+it.VariationID)
				return
			}
			v, verr := h.ProductVariations.GetByProduct(r.Context(), it.ProductID, it.VariationID)
			if verr != nil {
				if verr == dbstore.ErrVariationNotFound {
					writeErr(w, http.StatusBadRequest, "variasi tidak sah: "+it.VariationID)
					return
				}
				respondErr(w, r, verr)
				return
			}
			item.PriceSen = v.PriceSen
			item.VariationID = v.ID
			item.VariationName = v.Name
			// Inventory: limited-stock variations decrement atomically; SOLD OUT rejects.
			if err := h.ProductVariations.DecrementStock(r.Context(), v.ID, int(qty)); err != nil {
				if err == dbstore.ErrOutOfStock {
					writeErr(w, http.StatusConflict, "stok habis untuk "+name+" ("+v.Name+")")
					return
				}
				respondErr(w, r, err)
				return
			}
		} else if h.Products != nil && h.isProductRow(r.Context(), tgt.AccountID, it.ProductID) {
			// Simple product: decrement product-level stock atomically. Pricing
			// blocks (option/shipping) submit with product_id = the block's own
			// id, which is not a product row — skip inventory for them (issue
			// #461; before, the non-uuid block id blew up the stock query).
			if err := h.Products.DecrementStock(r.Context(), it.ProductID, int(qty)); err != nil {
				if err == dbstore.ErrOutOfStock {
					writeErr(w, http.StatusConflict, "stok habis untuk "+name)
					return
				}
				respondErr(w, r, err)
				return
			}
		}
		sale.Items = append(sale.Items, item)
		subtotal += item.PriceSen * qty
	}
	sale.SubtotalSen = subtotal
	// Issue #99 money invariant: total_sen = subtotal + shipping - discount.
	// The submit path currently has no shipping/discount inputs, so total
	// equals subtotal; the formula is centralized in payments.SumItems so the
	// invariant is enforced and unit-tested in one place.
	sale.TotalSen = payments.SumItems(subtotal, 0, 0)

	if err := h.Sales.Create(r.Context(), sale); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #179: persist the submitted field values on the sale so the
	// thank-you page can evaluate confirmation conditional logic against them.
	if len(in.Fields) > 0 {
		fm := make(map[string]any, len(in.Fields))
		for k, v := range in.Fields {
			fm[k] = v
		}
		_ = h.Sales.SetSaleFields(r.Context(), sale.ID, fm)
	}
	// Issue #245: enqueue a background AI spam-judge job for this sale. The
	// sale is accepted immediately; the judge flags it as spam in the
	// background (skipped for the workspace owner/admin testing their own
	// store). Never blocks the submit response.
	judgeData := make(map[string]any, len(in.Fields))
	for k, v := range in.Fields {
		judgeData[k] = v
	}
	h.enqueueSpamJudge(r, tgt.AccountID, tgt.Form, owner, "", sale.ID, in.Client.Email, judgeData)

	// CHIP flow: create purchase & return checkout URL.
	if method == "chip" {
		// Issue #208: resolve the CHIP feed for this submission. A form's
		// chip_feeds are evaluated in order against the submitted field values;
		// the first matching feed (default feed = no condition always matches)
		// supplies the brand/secret. When no feed matches, fall back to the
		// legacy single per-form override, then account. A formless order has no
		// form feeds — it uses the account default.
		var ov *payments.ChipOverride
		if tgt.Form != nil && h.Forms != nil {
			if feeds, ferr := h.Forms.ChipFeedsFor(r.Context(), tgt.AccountID, tgt.Form.ID); ferr == nil {
				if feed := resolveChipFeed(feeds, in.Fields); feed != nil && feed.BrandID != "" && feed.SecretKeyEnc != "" {
					ov = &payments.ChipOverride{
						BrandID: feed.BrandID, SecretEnc: feed.SecretKeyEnc,
						EmailFallback: feed.EmailFallback, MethodWhitelist: feed.MethodWhitelist,
					}
				}
			}
			if ov == nil {
				if b, s, ef, wl, oerr := h.Forms.ChipOverrideFor(r.Context(), tgt.AccountID, tgt.Form.ID); oerr == nil && b != "" && s != "" {
					ov = &payments.ChipOverride{BrandID: b, SecretEnc: s, EmailFallback: ef, MethodWhitelist: wl}
				}
			}
		}
		if err := h.Pay.CreateChipPurchaseForSaleWithOverride(r.Context(), sale, ov); err != nil {
			slog.Error("chip_create_for_sale", "err", fmt.Sprintf("%+v", err), "sale", sale.RefCode)
			writeErr(w, http.StatusBadGateway, "gagal mencipta pembayaran")
			return
		}
		respond(w, http.StatusCreated, map[string]string{
			"ref_code":     sale.RefCode,
			"checkout_url": sale.ChipCheckoutURL,
			"status":       "redirect",
		})
		return
	}

	// Bank direct: buyer sees instructions & upload proof next.
	respond(w, http.StatusCreated, map[string]string{
		"ref_code": sale.RefCode,
		"status":   "bank_direct",
		"next":     "/t/" + sale.RefCode,
	})
}

// handleMenuOrder is the workspace-scoped order endpoint (issue #491): the
// restaurant menu page checks out here with no form in the path. It resolves
// the account from the request host (workspace subdomain or verified custom
// domain), optionally attaches the merchant's extra-info form, and hands off
// to the SAME order core the form path uses.
//
// Route: POST /menu-order.
func (h *PublicHandlers) handleMenuOrder(w http.ResponseWriter, r *http.Request) {
	acc, workspaceID, ok := h.resolveWorkspaceAccount(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "kedai tidak dijumpai")
		return
	}
	// Part 3 hooks in here: when the merchant attached a form to the menu for
	// extra info, its registered-field contract validates the submitted fields.
	// No attachment → no field validation at all.
	extra := h.workspaceOrderExtraForm(r, acc, workspaceID)
	// The rate-limit bucket keys on the workspace (the formless equivalent of
	// the per-form key) so the protection survives without a form. Fall back to
	// the account id when the account has no linked workspace (legacy).
	rateKey := workspaceID
	if rateKey == "" {
		rateKey = acc.ID
	}
	h.submitOrder(w, r, orderTarget{
		AccountID:   acc.ID,
		Form:        nil,
		FieldsForm:  extra,
		RateKey:     "ws:" + rateKey,
		OwnerSkip:   h.antiSpamSubmitterIsOwnerForAccount(r, acc.ID),
		RequireName: true,
	})
}

// resolveWorkspaceAccount resolves the account a workspace-scoped public route
// serves, from the request host: a workspace subdomain
// ({workspace}.jomform.com, issue #77) or a verified custom domain mapped to a
// workspace's account (issue #286/#297). Bare jomform.com (no subdomain, no
// custom domain) resolves nothing, so dashboard routes are unaffected. The
// second return is the owning workspace id ("" when unresolvable).
func (h *PublicHandlers) resolveWorkspaceAccount(r *http.Request) (*dbstore.Account, string, bool) {
	if h.Accounts == nil {
		return nil, "", false
	}
	if sub := accountSubdomain(r.Host); sub != "" {
		workspaceID := ""
		if h.Workspaces != nil {
			workspaceID, _ = h.Workspaces.IDBySlug(r.Context(), sub)
		}
		if workspaceID == "" {
			return nil, "", false
		}
		acc, err := h.Accounts.ByWorkspaceID(r.Context(), workspaceID)
		if err != nil || acc == nil {
			return nil, "", false
		}
		return acc, workspaceID, true
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		acc, err := h.Accounts.BySlug(r.Context(), cd)
		if err != nil || acc == nil {
			return nil, "", false
		}
		workspaceID := ""
		if h.Workspaces != nil {
			workspaceID, _ = h.Workspaces.WorkspaceIDByAccount(r.Context(), acc.ID)
		}
		return acc, workspaceID, true
	}
	return nil, "", false
}

// workspaceOrderExtraForm returns the published form the merchant attached to
// the menu for extra order info (part 3), or nil when none is attached (or the
// configured slug no longer resolves to a published form in this account).
//
// The setting lives on the menu config (MenuPage.OrderFormSlug) — a JSON blob
// on workspace_settings, so no migration is needed. Part 3 wires the config UI
// and the MCP tool; the endpoint already honours it.
func (h *PublicHandlers) workspaceOrderExtraForm(r *http.Request, acc *dbstore.Account, workspaceID string) *dbstore.Form {
	if h.Forms == nil || h.Workspaces == nil || workspaceID == "" || acc == nil {
		return nil
	}
	st, err := h.Workspaces.Settings(r.Context(), workspaceID)
	if err != nil || st == nil || st.LandingPage == nil || st.LandingPage.Menu == nil {
		return nil
	}
	slug := strings.TrimSpace(st.LandingPage.Menu.OrderFormSlug)
	if slug == "" {
		return nil
	}
	f, ferr := h.Forms.PublishedByAccountSlug(r.Context(), acc.Slug, slug)
	if ferr != nil || f == nil {
		return nil
	}
	return f
}
