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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	// against the form itself.
	FieldsForm *dbstore.Form
	// FieldsContract is an explicit field contract (issue #493) used when the
	// order target has no form of its own: a menu order validates against the
	// contract derived from the optional attached extra-info form's builder
	// blocks. Nil/empty means no field validation at all.
	FieldsContract []formfields.Field
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
	// against the form's registered contract before accepting the order.
	// Issue #493: a formless menu order validates against the contract derived
	// from the attached extra-info form's builder blocks (FieldsContract); a
	// menu with no form attached has no contract — nothing to enforce.
	if tgt.FieldsForm != nil {
		if err := formfields.Validate(tgt.FieldsForm.Config, in.Fields); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if len(tgt.FieldsContract) > 0 {
		if err := formfields.ValidateFields(tgt.FieldsContract, in.Fields); err != nil {
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
		// Issue #560: the checkout used to hardcode one_time, so a subscription
		// product could never create a subscription — activateSubscription only
		// runs for SaleTypeSubscription, and the card is only tokenized for
		// future charges when the sale is a subscription. The product's own type
		// now decides.
		// Issue #560: finalised after the cart is resolved — the product type
		// decides it. Default one_time so a cart with no subscription product
		// is unchanged.
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
	// Issue #560: the resolution of the subscription product for this order (nil
	// when no line item is a subscription). See subItems below.
	var product *dbstore.Product
	// Issue #623: the variation bought of that subscription product, when the
	// buyer chose one. It carries the subscription cycle the sale bills on.
	var subVariation *dbstore.ProductVariation
	var subItems []dbstore.SaleItem
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
		// Issue #560: a line item's product type decides whether this order is a
		// subscription. The products table is the authority (same source as the
		// price above); a pricing/add-on block is not a product row and is
		// therefore never a subscription.
		if h.Products != nil && product == nil {
			if pr, perr := h.Products.Get(r.Context(), tgt.AccountID, it.ProductID); perr == nil && pr != nil {
				product = pr
			}
		}
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
			// Issue #623: remember the chosen variation so the sale records the
			// cycle it actually bills on. Only the subscription product's own
			// variation counts — a variation on a one-time item in the same cart
			// must not change the recurring terms.
			if product != nil && product.Type == dbstore.SaleTypeSubscription && subVariation == nil {
				subVariation = v
			}
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
		if product != nil && product.Type == dbstore.SaleTypeSubscription {
			subItems = append(subItems, item)
		}
		subtotal += item.PriceSen * qty
	}
	// Issue #560: a subscription order is one where a PURCHASED PRODUCT is
	// type=subscription. The recurring amount and schedule come from THAT
	// product (its own price x quantity), not the whole order total: any
	// one-time items in the same cart are charged once, in this first payment
	// only.
	//
	// Issue #623: the schedule also follows the VARIATION the buyer chose when
	// that variation carries its own cycle — so "Size M monthly" and "Size L
	// yearly" from one product each bill on their own terms. A variation with no
	// cycle of its own inherits the product's, which is why a product whose
	// variations carry none is unchanged.
	//
	// Persisted on the sale so activation never has to re-derive the terms from
	// a mixed cart, and never falls back to a hardcoded monthly period. The
	// variation id rides along so the recurring engine resolves the fixed charge
	// count through the SAME rule (dbstore.EffectiveCycle) from the second
	// charge onward.
	if st, amount, cycle := resolveSubscriptionTerms(product, subVariation, subItems); st != "" {
		sale.SaleType = st
		sale.SubscriptionProductID = product.ID
		sale.SubscriptionAmountSen = amount
		sale.SubscriptionPeriod = cycle.Period
		if subVariation != nil {
			sale.SubscriptionVariationID = subVariation.ID
		}
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

// handleMenuOrder is the legacy workspace-scoped order endpoint (issue #491).
//
// Issue #497 moved the menu onto the FORM, so the menu checkout now posts to
// the form's own order route (POST /{slug}/order) and there is ONE order path.
// This route is kept as a thin compatibility shim for a cached menu page or an
// old QR stand that still posts here: it resolves the menu FORM from the
// request (a `form_slug` in the JSON body + the host's account) and delegates
// to the exact same form-scoped menu order path. It no longer accepts a
// formless order — a menu order is always attributed to its form.
//
// Route: POST /menu-order (deprecated; prefer POST /{slug}/order).
func (h *PublicHandlers) handleMenuOrder(w http.ResponseWriter, r *http.Request) {
	acc, _, ok := h.resolveWorkspaceAccount(r)
	if !ok {
		writeErr(w, http.StatusNotFound, "kedai tidak dijumpai")
		return
	}
	// Buffer the body once: we must read form_slug to resolve the target and
	// then hand the SAME bytes to the shared order core.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxSubmitBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	var probe struct {
		FormSlug string `json:"form_slug"`
	}
	if jerr := json.Unmarshal(body, &probe); jerr != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	slug := strings.TrimSpace(probe.FormSlug)
	if slug == "" {
		writeErr(w, http.StatusBadRequest, "form_slug diperlukan")
		return
	}
	if h.Forms == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	f, ferr := h.Forms.PublishedByAccountSlug(r.Context(), acc.Slug, slug)
	if ferr != nil || f == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	menu := dbstore.MenuFromConfig(f.Config)
	if menu == nil || !menu.Enabled {
		writeErr(w, http.StatusNotFound, "menu tidak dijumpai")
		return
	}
	// Replay the buffered body for the shared core (anti-spam layer 3 reads the
	// jf_ts field from it, and submitOrder decodes it).
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.submitMenuOrderForForm(w, r, f, menu)
}

// submitMenuOrderForForm runs a menu form's checkout through the ONE shared
// order core (issue #497). The menu is a FORM, so:
//   - the sale is attributed to the menu form (sale.form_id = the form id), not
//     left formless as in the workspace-scoped #491 shape;
//   - the extra checkout fields come from the form's ATTACHED order form
//     (menu.OrderFormSlug), never from the menu form's own product blocks — a
//     product block is not an input field;
//   - name + email are both mandatory (CHIP needs them).
func (h *PublicHandlers) submitMenuOrderForForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form, menu *dbstore.MenuPage) {
	var contract []formfields.Field
	if extra := h.formOrderExtraForm(r, f, menu.OrderFormSlug); extra != nil {
		contract = menuExtraContract(extra.Config)
	}
	h.submitOrder(w, r, orderTarget{
		AccountID:      f.AccountID,
		Form:           f,
		FieldsContract: contract,
		RateKey:        f.ID,
		OwnerSkip:      h.antiSpamSubmitterIsOwner(r, f),
		RequireName:    true,
	})
}

// maxSubmitBodyBytes bounds the buffered legacy /menu-order body (issue #497).
const maxSubmitBodyBytes = 1 << 20 // 1 MiB

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

// workspaceOrderExtraForm was removed in issue #497: the menu is a per-form
// setting, so the attached order form now resolves from the menu form itself
// (see PublicHandlers.formOrderExtraForm in menu.go). Keeping a workspace-scoped
// resolver here would leave two overlapping menu-order paths.

// resolveSubscriptionTerms maps a resolved cart to the subscription terms for the
// sale (issue #560, extended by issue #623 for per-variation cycles).
//
// It returns ("", 0, "", dbstore.SubscriptionCycle{}) when no purchased product
// is a subscription, so the sale stays one_time. Otherwise the sale is a
// subscription and the terms come from THAT product:
//
//   - amount: the subscription product's own price x quantity — NOT the order
//     total, because a cart may also contain one-time items, and those must be
//     charged once rather than re-billed every period;
//   - period: the EFFECTIVE cycle of the product, or of the chosen variation when
//     that variation carries its own (issue #623). A yearly product billed
//     monthly is a real, silent billing error.
//
// v is the variation the buyer chose, or nil. The cycle is resolved in exactly
// ONE place — dbstore.EffectiveCycle — so a variation's own cycle cannot be
// honoured at checkout and ignored by the recurring engine (or vice versa).
//
// Extracted so the rule is directly testable: when it lived inline in submitOrder,
// a test could only re-implement it and would pass even with the production rule
// removed.
func resolveSubscriptionTerms(product *dbstore.Product, v *dbstore.ProductVariation,
	subItems []dbstore.SaleItem) (saleType string, amountSen int64, cycle dbstore.SubscriptionCycle) {
	if product == nil || product.Type != dbstore.SaleTypeSubscription {
		return "", 0, dbstore.SubscriptionCycle{}
	}
	var total int64
	for _, it := range subItems {
		total += it.PriceSen * it.Qty
	}
	return dbstore.SaleTypeSubscription, total, dbstore.EffectiveCycle(product, v)
}
