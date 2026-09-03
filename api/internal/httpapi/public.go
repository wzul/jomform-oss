// Public-facing order form: SSR page render + JSON submit flow.
package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// PublicHandlers serves buyer-facing order forms.
type PublicHandlers struct {
	Forms              *dbstore.FormStore
	Sales              *dbstore.SaleStore
	Accounts           *dbstore.AccountStore
	Products           *dbstore.ProductStore
	ProductVariations  *dbstore.ProductVariationStore
	Pay                *payments.Service
	SystemSettings     *dbstore.SystemSettingStore
	Spaces             *dbstore.SpaceStore
	// Issue #171: response-only form submissions.
	Submissions *dbstore.SubmissionStore
	BaseURL     string
	// AccessKey (ENCRYPTION_KEY bytes) signs the private-form unlock cookie
	// (issue #176). Nil disables private-form access (gate always shown).
	AccessKey []byte
}

// RegisterRoutes wires public endpoints.
func (h *PublicHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /f/{slug}", h.handleRender)
	mux.HandleFunc("GET /embed/f/{accslug}/{slug}", h.embedForm)
	mux.Handle("POST /f/{slug}/order", middleware.RateLimit(http.HandlerFunc(h.handleSubmit)))
	// Space subdomains serve forms at /{slug} (GET /{slug} below), so the
	// buyer-facing submit must resolve there too. handleSubmit scopes by
	// accountSubdomain(host); on bare jomform.com it 404s (issue #77 — a
	// public payment link is reachable only via its space subdomain or a
	// custom domain), leaving dashboard routes (/api, /v1) intact.
	mux.Handle("POST /{slug}/order", middleware.RateLimit(http.HandlerFunc(h.handleSubmit)))
	mux.HandleFunc("GET /t/{ref}", h.handleThankYou)
	// Issue #202: lightweight read-only status endpoint the thank-you page
	// polls while a sale is pending (bounded client-side poll). Returns the
	// sale's current status; never mutates anything.
	mux.HandleFunc("GET /t/{ref}/status", h.handleThankYouStatus)
	mux.HandleFunc("GET /{$}", h.handleSpaceRoot)
	// Issue #184: hosted legal pages on space subdomains
	// ({space}.jomform.com/privacy, /terms, /refund). More specific than
	// GET /{slug}, so they win over the form-slug route.
	mux.HandleFunc("GET /privacy", h.handlePublicLegalPage)
	mux.HandleFunc("GET /terms", h.handlePublicLegalPage)
	mux.HandleFunc("GET /refund", h.handlePublicLegalPage)
	mux.HandleFunc("GET /{slug}", h.handleSpaceRootForm)
	// Issue #176: unlock a private form with its access password.
	mux.HandleFunc("POST /{slug}/unlock", h.handleFormUnlock)
}

// genRef produces a short opaque reference code.
func genRef() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "JF-" + hex.EncodeToString(b)
}

// formMode returns the form's collection mode from its config: "payment"
// (default, current behaviour) or "response" (collect-only, no payment).
func formMode(f *dbstore.Form) string {
	if f == nil || f.Config == nil {
		return "payment"
	}
	if raw, err := json.Marshal(f.Config); err == nil {
		var cfg struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(raw, &cfg) == nil && cfg.Mode == "response" {
			return "response"
		}
	}
	return "payment"
}

// chipMappingFromConfig reads the form's merchant-configured CHIP field
// mapping (issue #186): a map of CHIP client field → form block id. When the
// merchant hasn't configured one, the implicit default mapping is used
// (name block → full_name, email block → email, phone block → phone), which
// preserves the pre-#186 behaviour.
func chipMappingFromConfig(f *dbstore.Form) map[string]string {
	if f == nil || f.Config == nil {
		return nil
	}
	raw, err := json.Marshal(f.Config)
	if err != nil {
		return nil
	}
	var cfg struct {
		ChipMapping map[string]string `json:"chip_mapping"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return cfg.ChipMapping
}

// resolveChipClient builds the buyer's CHIP client details from the submitted
// form field values (issue #186). For each CHIP client field the merchant
// mapped to a form block, the value of that block (from the submitted
// `fields` map) is used. Fields with no mapping fall back to the hardcoded
// `client` object, so the default mapping preserves current behaviour.
func resolveChipClient(f *dbstore.Form, fields map[string]string, fallback dbstore.SaleClient) dbstore.SaleClient {
	out := fallback
	mapping := chipMappingFromConfig(f)
	if len(mapping) == 0 {
		return out
	}
	// CHIP client field → SaleClient field setter.
	set := func(chipField string, dst *string) {
		if blockID, ok := mapping[chipField]; ok && blockID != "" {
			if v, ok := fields[blockID]; ok && v != "" {
				*dst = v
			}
		}
	}
	set("full_name", &out.FullName)
	set("email", &out.Email)
	set("phone", &out.Phone)
	set("street_address", &out.StreetAddress)
	set("city", &out.City)
	set("state", &out.State)
	set("zip_code", &out.ZipCode)
	set("country", &out.Country)
	set("legal_name", &out.LegalName)
	set("registration_number", &out.RegistrationNum)
	set("tax_number", &out.TaxNumber)
	return out
}

// handleSubmit creates a pending sale, requests a CHIP purchase and returns
// the checkout URL. In response mode (issue #171) it instead stores the
// submitted field values as a submission — no sale, no payment, no checkout.
func (h *PublicHandlers) handleSubmit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// Same lookup rules as render: subdomain scoping first. On bare
	// jomform.com (no subdomain, no custom domain) a published form is NOT
	// served — issue #77: a public payment link is reachable only via its
	// registered space subdomain or a custom domain.
	var form *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		form, err = h.Forms.BySpaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, r.Host); cd != "" {
		form, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		form, err = nil, dbstore.ErrFormNotFound
	}
	if err != nil || form == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	// Issue #176: draft forms are never submittable; private forms require a
	// valid unlock cookie (the buyer must have passed the password gate).
	if form.Status == dbstore.FormStatusDraft {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if form.Status == dbstore.FormStatusPrivate && !h.privateUnlocked(r, form) {
		writeErr(w, http.StatusForbidden, "kata laluan diperlukan")
		return
	}
	// Issue #207: entry limit. Reject submissions once the form's entry count
	// (sales for payment forms, submissions for response forms) reaches the
	// configured limit.
	if reached, rerr := h.entryLimitReached(r.Context(), form); rerr == nil && reached {
		writeErr(w, http.StatusConflict, h.restrictionMessage(r, form, restrictionLimitReached))
		return
	}

	// Issue #171: response-only forms collect field values, not orders.
	if formMode(form) == "response" {
		h.handleResponseSubmit(w, r, form)
		return
	}

	var in struct {
		Client struct {
			FullName        string `json:"full_name"`
			Email           string `json:"email"`
			Phone           string `json:"phone"`
			StreetAddress   string `json:"street_address"`
			City            string `json:"city"`
			State           string `json:"state"`
			ZipCode         string `json:"zip_code"`
			Country         string `json:"country"`
			LegalName       string `json:"legal_name"`
			RegistrationNum string `json:"registration_number"`
			TaxNumber       string `json:"tax_number"`
		} `json:"client"`
		Items []struct {
			ProductID   string `json:"product_id"`
			VariationID string `json:"variation_id"`
			Quantity    int64  `json:"quantity"`
		} `json:"items"`
		PaymentMethod string `json:"payment_method"`         // chip | bank_direct
		SkipCapture   bool   `json:"skip_capture,omitempty"` // authorize now, capture later
		// Issue #186: block_id → submitted value for every input block, so
		// the merchant's chip_mapping can resolve CHIP client fields from the
		// mapped form blocks (not just the hardcoded client object).
		Fields map[string]string `json:"fields,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Client.Email == "" || len(in.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	method := in.PaymentMethod
	if method != "chip" && method != "bank_direct" {
		writeErr(w, http.StatusBadRequest, "kaedah pembayaran tidak sah")
		return
	}

	// Validate totals against the account currency (MVP: MYR only).
	if _, err := h.Accounts.ByID(r.Context(), form.AccountID); err != nil {
		respondErr(w, r, err)
		return
	}

	sale := &dbstore.Sale{
		AccountID:     form.AccountID,
		FormID:        form.ID,
		RefCode:       genRef(),
		Client: resolveChipClient(form, in.Fields, dbstore.SaleClient{
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
	}
	// Issue #185: record the currency the sale is transacted in from the
	// space that owns the form's account (default MYR). Historical sales keep
	// this original currency — never auto-converted on a later change.
	if h.Spaces != nil {
		if cur, cerr := h.Spaces.SpaceCurrencyByAccount(r.Context(), form.AccountID); cerr == nil && cur != "" {
			sale.Currency = cur
		}
	}
	if sale.Currency == "" {
		sale.Currency = "MYR"
	}
	var subtotal int64
	for _, it := range in.Items {
		// For MVP the form config carries product id + authoritative price.
		name, price := h.lookupProduct(form, it.ProductID)
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
		} else if h.Products != nil {
			// Simple product: decrement product-level stock atomically.
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

	// CHIP flow: create purchase & return checkout URL.
	if method == "chip" {
		// Per-form override takes precedence over account credentials.
		var ov *payments.ChipOverride
		if h.Forms != nil {
			if b, s, ef, wl, oerr := h.Forms.ChipOverrideFor(r.Context(), form.AccountID, form.ID); oerr == nil && b != "" && s != "" {
				ov = &payments.ChipOverride{BrandID: b, SecretEnc: s, EmailFallback: ef, MethodWhitelist: wl}
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

// handleResponseSubmit stores a response-only form's field values (issue
// #171). The buyer submits {client, fields:{block_id:value}}; we persist a
// submission row (no sale, no payment, no CHIP) and return a success/thank-you
// with no checkout URL.
func (h *PublicHandlers) handleResponseSubmit(w http.ResponseWriter, r *http.Request, form *dbstore.Form) {
	var in struct {
		Client struct {
			FullName string `json:"full_name"`
			Email    string `json:"email"`
			Phone    string `json:"phone"`
		} `json:"client"`
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	if in.Fields == nil {
		in.Fields = map[string]any{}
	}
	// Always capture the buyer identity alongside the field answers so the
	// merchant can see who responded even when no name/email block exists.
	if in.Client.FullName != "" {
		in.Fields["full_name"] = in.Client.FullName
	}
	if in.Client.Email != "" {
		in.Fields["email"] = in.Client.Email
	}
	if in.Client.Phone != "" {
		in.Fields["phone"] = in.Client.Phone
	}
	sub := &dbstore.Submission{
		FormID:    form.ID,
		AccountID: form.AccountID,
		Data:      in.Fields,
	}
	if err := h.Submissions.Create(r.Context(), sub); err != nil {
		respondErr(w, r, err)
		return
	}
	respond(w, http.StatusCreated, map[string]string{
		"status": "submitted",
		"id":     sub.ID,
	})
}

// lookupProduct resolves product name/price from the form snapshot config.
// MVP stores products inline in form config [{id,name,price_sen}]; proper
// product references land with the full builder.
func (h *PublicHandlers) lookupProduct(form *dbstore.Form, productID string) (string, int64) {
	raw, _ := json.Marshal(form.Config)
	var cfg struct {
		Products []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			PriceSen int64  `json:"price_sen"`
		} `json:"products"`
		// Issue #173: pricing blocks (option/shipping) carry their own price
		// and submit with product_id = the block's own id. Resolve those here
		// so the server stays the price authority for add-ons/shipping.
		Blocks []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Value    string `json:"value"`
			PriceSen int64  `json:"price_sen"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) == nil {
		for _, p := range cfg.Products {
			if p.ID == productID {
				return p.Name, p.PriceSen
			}
		}
		for _, b := range cfg.Blocks {
			if b.ID == productID && (b.Type == "option" || b.Type == "shipping") {
				return b.Value, b.PriceSen
			}
		}
	}
	return "", 0
}

// ─── render + thank-you (SSR, buyer-facing) ──────────────────────────────────

// formFuncs provides template helpers.
var formFuncs = template.FuncMap{
	"divf": func(total, div int64) float64 { return float64(total) / float64(div) },
	"div100": func(v any) string {
		switch n := v.(type) {
		case int64:
			return fmt.Sprintf("%.2f", float64(n)/100)
		case int:
			return fmt.Sprintf("%.2f", float64(n)/100)
		default:
			return "0.00"
		}
	},
}

var formTmpl = template.Must(template.New("form").Funcs(formFuncs).Parse(`<!doctype html>
<html lang="ms">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:#f7f8f8; color:#17181c; min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { background:#ffffff; border:1px solid #e6e8ec; border-radius:20px;
          max-width:460px; width:100%; padding:32px; }
  h1 { margin:0 0 6px; font-size:22px; }
  .sub { color:#6b7280; font-size:14px; margin-bottom:22px; }
  .prod { display:block; padding:0; border:1px solid #e6e8ec; border-radius:14px;
          margin:0 0 12px; background:#ffffff; box-shadow:0 1px 2px rgba(23,24,28,0.04);
          transition:border-color .15s ease, box-shadow .15s ease; }
  .prod:hover { border-color:#4f46e5; box-shadow:0 2px 8px rgba(79,70,229,0.12); }
  .prod:active { background:#eef2ff; }
  .prod:has(input[type=checkbox]:checked) { border-color:#4f46e5; box-shadow:0 2px 8px rgba(79,70,229,0.12); background:#eef2ff; }
  .prod label { display:flex; gap:12px; align-items:center; width:100%;
                min-height:44px; padding:12px 14px; font-size:15px; color:#17181c;
                cursor:pointer; -webkit-tap-highlight-color:transparent; }
  .prod input[type=checkbox] { width:20px; height:20px; flex:0 0 auto; accent-color:#4f46e5; margin:0; }
  .pname { flex:1; font-weight:500; }
  .price { color:#4f46e5; font-weight:600; white-space:nowrap; margin-left:auto; text-align:right; }
  input[type=text], input[type=email], input[type=tel] {
    width:100%; padding:11px 14px; margin:6px 0 14px; border-radius:10px;
    border:1px solid #e6e8ec; background:#ffffff; color:#17181c; font-size:14px; }
  label.top { font-size:12px; color:#8b93a7; }
  button { width:100%; padding:13px; border:0; border-radius:12px; font-size:15px;
           font-weight:600; color:white; cursor:pointer; margin-top:8px;
           background:linear-gradient(90deg,#6366f1,#8b5cf6); }
  button:hover { filter:brightness(1.1); }
  .total { display:flex; justify-content:space-between; padding-top:12px;
           border-top:1px dashed #e6e8ec; margin-top:6px; font-weight:600; }
  /* Issue #183: optional per-space footer. */
  .jf-footer { max-width:460px; width:100%; margin:16px auto 0; padding:0 8px;
               color:#6b7280; font-size:12px; line-height:1.6; text-align:center; }
  .jf-fbiz { font-weight:600; color:#374151; }
  .jf-ftext { margin-top:4px; }
  .jf-flinks { display:flex; gap:14px; justify-content:center; flex-wrap:wrap; margin-top:6px; }
  .jf-flinks a { color:#4f46e5; text-decoration:none; }
  .jf-flinks a:hover { text-decoration:underline; }
</style>
</head>
<body>
  <form class="card" id="of" data-slug="{{.Slug}}">
    <h1>{{.Title}}</h1>
    <p class="sub">{{.Description}}</p>
    {{range .Products}}
    <div class="prod">
      <label><input type="checkbox" class="p" data-id="{{.ID}}" data-price="{{.PriceSen}}" data-name="{{.Name}}"><span class="pname">{{.Name}}</span><span class="price">RM{{div100 .PriceSen}}</span></label>
    </div>
    {{end}}
    <label class="sub">Nama penuh</label>
    <input type="text" name="full_name" required>
    <label class="sub">Emel</label>
    <input type="email" name="email" required>
    <label class="sub">No. telefon</label>
    <input type="tel" name="phone">
    <div class="total"><span>Jumlah</span><span id="total">RM0.00</span></div>
    <button type="submit">Teruskan ke Pembayaran</button>
  </form>
  {{if .Footer.HasFooter}}<footer class="jf-footer">
    {{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
    {{if .Footer.ContactPhone}}<div class="jf-frow">{{.Footer.ContactPhone}}</div>{{end}}
    {{if .Footer.ContactAddress}}<div class="jf-frow">{{.Footer.ContactAddress}}</div>{{end}}
    {{if .Footer.FooterText}}<div class="jf-ftext">{{.Footer.FooterText}}</div>{{end}}
    {{if or .Footer.PrivacyURL .Footer.TermsURL .Footer.RefundURL}}<div class="jf-flinks">
      {{if .Footer.PrivacyURL}}<a href="{{.Footer.PrivacyURL}}" target="_blank" rel="noopener">Polisi Privasi</a>{{end}}
      {{if .Footer.TermsURL}}<a href="{{.Footer.TermsURL}}" target="_blank" rel="noopener">Terma &amp; Syarat</a>{{end}}
      {{if .Footer.RefundURL}}<a href="{{.Footer.RefundURL}}" target="_blank" rel="noopener">Polisi Bayaran Balik</a>{{end}}
    </div>{{end}}
  </footer>{{end}}
<script>
const boxes = document.querySelectorAll('.p');
function update() {
  let total = 0;
  boxes.forEach(b => { if (b.checked) total += Number(b.dataset.price); });
  document.getElementById('total').textContent = 'RM' + (total/100).toFixed(2);
}
boxes.forEach(b => b.addEventListener('change', update));
document.getElementById('of').addEventListener('submit', async (e) => {
  e.preventDefault();
  const items = [];
  boxes.forEach(b => { if (b.checked) items.push({product_id: b.dataset.id, quantity: 1}); });
  if (!items.length) { alert('Pilih sekurang-kurangnya satu produk'); return; }
  const body = {
    client: { full_name: e.target.full_name.value, email: e.target.email.value, phone: e.target.phone.value },
    items, payment_method: 'chip'
  };
  const r = await fetch(location.pathname + '/order', {
    method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body)
  });
  const data = await r.json();
  if (data.checkout_url) { location.href = data.checkout_url; }
  else if (data.next) { location.href = data.next; }
  else { alert(data.error || 'Ralat'); }
});
</script>
</body></html>`))

type tmplProduct struct {
	ID       string
	Name     string
	PriceSen int64
}

func (h *PublicHandlers) handleRender(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	// Subdomain routing: wanzulhosting.jomform.com/f/test resolves the form
	// scoped to that account's slug. On bare jomform.com (no subdomain, no
	// custom domain) a published form is NOT served — issue #77: a public
	// payment link is reachable only via its registered space subdomain
	// ({space}.jomform.com/{slug}) or a custom domain. The legacy global
	// /f/{slug} lookup is gone.
	if sub := accountSubdomain(r.Host); sub != "" {
		f, ferr := h.Forms.BySpaceSlug(r.Context(), sub, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai untuk domain ini", http.StatusNotFound)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, r.Host); cd != "" {
		f, ferr := h.Forms.ByAccountSlug(r.Context(), cd, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	// Bare apex (jomform.com): no space subdomain, no custom domain — 404.
	http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
}

// renderLegacy renders the old product-checkbox layout.
func (h *PublicHandlers) renderLegacy(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	var cfg struct {
		Description string `json:"description"`
		Products    []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			PriceSen int64  `json:"price_sen"`
			Active   bool   `json:"active"`
		} `json:"products"`
	}
	if raw, merr := json.Marshal(f.Config); merr == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	var prods []tmplProduct
	for _, p := range cfg.Products {
		if p.ID != "" {
			prods = append(prods, tmplProduct{ID: p.ID, Name: p.Name, PriceSen: p.PriceSen})
		}
	}
	type view struct {
		Slug, Title, Description string
		Products                 []tmplProduct
		// Issue #183: optional per-space footer.
		Footer footerInfo
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formTmpl.Execute(w, view{
		Slug: f.Slug, Title: f.Title, Description: cfg.Description, Products: prods,
		Footer: h.spaceFooter(r.Context(), f),
	}); err != nil {
		slog.Error("form_render", "err", err)
	}
}

func (h *PublicHandlers) handleThankYou(w http.ResponseWriter, r *http.Request) {
	sale, err := h.Sales.ByRefCode(r.Context(), r.PathValue("ref"))
	if err != nil {
		http.Error(w, "Tempahan tidak dijumpai", http.StatusNotFound)
		return
	}
	// Issue #202: bounded poll instead of an indefinite 4s meta-refresh. While
	// the sale is still pending, the page polls a lightweight status endpoint
	// every 4s and stops after a bounded window (default 60s) so a slow or
	// missing callback can't keep the buyer's tab refreshing forever. The
	// redirect stays read-only — the callback (not this page) settles the sale.
	refresh := ""
	if sale.Status == dbstore.SalePending {
		refresh = `<script>
const ref = document.querySelector('meta[name="jf-ref"]').content;
let polls = 0;
const maxPolls = 15; // 15 × 4s = 60s bounded wait
const iv = setInterval(async () => {
  polls++;
  try {
    const res = await fetch('/t/' + ref + '/status');
    if (res.ok) {
      const data = await res.json();
      if (data.status !== 'pending') { clearInterval(iv); location.reload(); return; }
    }
  } catch (e) {}
  if (polls >= maxPolls) { clearInterval(iv); }
}, 4000);
</script>`
	}
	statusText := "menunggu pengesahan"
	switch sale.Status {
	case dbstore.SalePaid:
		statusText = "pembayaran diterima"
	case dbstore.SaleConfirmed:
		statusText = "disahkan"
	case dbstore.SaleFailed:
		statusText = "pembayaran gagal"
	case dbstore.SaleCancelled:
		statusText = "dibatalkan"
	}
	fmt.Fprintf(w, `<!doctype html><html lang="ms"><head><meta charset="utf-8">%s
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Terima kasih</title>
<meta name="jf-ref" content="%s">
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:420px}
h1{color:#10b981;font-size:24px}</style></head><body><div class="c">
<h1>✓ Terima kasih!</h1><p>Tempahan <strong>%s</strong> diterima.<br>Status: %s.</p>
<p style="color:#9ca3af;font-size:12px">Dikuasakan oleh JomForm</p></div></body></html>`,
		refresh, sale.RefCode, sale.RefCode, statusText)
}

// handleThankYouStatus is the read-only status endpoint the thank-you page
// polls while a sale is pending (issue #202). It returns the sale's current
// status as JSON and never mutates anything — the callback settles the sale.
func (h *PublicHandlers) handleThankYouStatus(w http.ResponseWriter, r *http.Request) {
	sale, err := h.Sales.ByRefCode(r.Context(), r.PathValue("ref"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"status": "not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": sale.Status})
}

var _ = time.Now

// accountSubdomain extracts the account slug from a wildcard host
// (e.g. wanzulhosting from wanzulhosting.jomform.com). Empty for apex
// (jomform.com) and custom domains (custom domains resolve the account via
// the accounts.custom_domain column).
func accountSubdomain(host string) string {
	h := host
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	base := ".jomform.com"
	if !strings.HasSuffix(h, base) {
		return ""
	}
	sub := strings.TrimSuffix(h, base)
	if sub == "" || sub == "www" || sub == "app" || sub == "api" || sub == "mail" || sub == "s3" {
		return ""
	}
	return strings.ToLower(sub)
}

// footerInfo is the optional per-space public-form footer (issue #183). Only
// fields the merchant set are populated; HasFooter is false when nothing is
// set, so the renderer emits no footer at all (preserving #80's clean default).
type footerInfo struct {
	HasFooter      bool
	BusinessName   string
	CompanyReg     string
	ContactPhone   string
	ContactAddress string
	PrivacyURL     string
	TermsURL       string
	RefundURL      string
	FooterText     string
}

// spaceFooter resolves the optional footer for a form's space. Returns a
// footerInfo with HasFooter=false when the space is unresolvable or the
// merchant set nothing.
func (h *PublicHandlers) spaceFooter(ctx context.Context, f *dbstore.Form) footerInfo {
	var fi footerInfo
	if h.Spaces == nil {
		return fi
	}
	spaceID, err := h.Spaces.SpaceIDByAccount(ctx, f.AccountID)
	if err != nil || spaceID == "" {
		return fi
	}
	st, err := h.Spaces.Settings(ctx, spaceID)
	if err != nil {
		return fi
	}
	fi.BusinessName = st.BusinessName
	fi.CompanyReg = st.CompanyReg
	fi.ContactPhone = st.ContactPhone
	fi.ContactAddress = st.ContactAddress
	fi.PrivacyURL = st.PrivacyURL
	fi.TermsURL = st.TermsURL
	fi.RefundURL = st.RefundURL
	fi.FooterText = st.FooterText
	fi.HasFooter = fi.BusinessName != "" || fi.CompanyReg != "" || fi.ContactPhone != "" ||
		fi.ContactAddress != "" || fi.PrivacyURL != "" || fi.TermsURL != "" || fi.RefundURL != "" ||
		fi.FooterText != ""
	return fi
}

// serveForm renders a resolved form (blocks or legacy) and logs unknown
// template errors once. Response-only forms (issue #171) render a
// collect-only layout with no product list and no payment step.
func (h *PublicHandlers) serveForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	// Draft forms are never publicly served (issue #176). Only published and
	// private forms are reachable; private forms show a password gate.
	if f.Status == dbstore.FormStatusDraft {
		http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
		return
	}
	// Issue #176: a private form shows a password gate until the buyer
	// supplies the correct access password (verified via a signed cookie).
	if f.Status == dbstore.FormStatusPrivate {
		if !h.privateUnlocked(r, f) {
			h.renderPrivateGate(w, r, f)
			return
		}
	}
	// Issue #207: form restrictions. A scheduled form shows a pending/expired
	// message outside its window; a form at its entry limit shows a
	// limit-reached message. Either way the form itself is not rendered.
	if st := h.checkRestrictionsRender(r, f); st != restrictionOpen {
		h.renderRestriction(w, r, f, st)
		return
	}
	if formMode(f) == "response" {
		if h.renderResponseForm(w, r, f) {
			return
		}
	}
	if h.renderBlocks(w, r, f) {
		return
	}
	h.renderLegacy(w, r, f)
}

// privateUnlocked reports whether the request carries a valid signed cookie
// proving the buyer entered the correct password for this private form.
func (h *PublicHandlers) privateUnlocked(r *http.Request, f *dbstore.Form) bool {
	if h.AccessKey == nil {
		return false
	}
	c, err := r.Cookie(formAccessCookie)
	if err != nil {
		return false
	}
	formID, ok := verifyFormAccessToken(h.AccessKey, c.Value)
	return ok && formID == f.ID
}

// handleFormUnlock verifies the submitted password against the form's stored
// argon2id hash (constant-time) and, on success, sets a signed cookie that
// unlocks the form for this browser. Wrong passwords are rejected.
func (h *PublicHandlers) handleFormUnlock(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var f *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		f, err = h.Forms.BySpaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, r.Host); cd != "" {
		f, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		f, err = nil, dbstore.ErrFormNotFound
	}
	if err != nil || f == nil || f.Status != dbstore.FormStatusPrivate {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if h.AccessKey == nil {
		writeErr(w, http.StatusForbidden, "akses tidak tersedia")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	hash, err := h.Forms.AccessPassword(r.Context(), f.AccountID, f.ID)
	if err != nil || hash == "" {
		writeErr(w, http.StatusForbidden, "kata laluan salah")
		return
	}
	ok, err := auth.VerifyPassword(in.Password, hash)
	if err != nil || !ok {
		writeErr(w, http.StatusForbidden, "kata laluan salah")
		return
	}
	token, err := signFormAccessToken(h.AccessKey, f.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal buka kunci")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     formAccessCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7, // 7 days
	})
	respond(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

// renderPrivateGate renders the password gate screen for a private form.
func (h *PublicHandlers) renderPrivateGate(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="ms"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:400px;width:100%%}
h1{font-size:20px;margin:0 0 4px}.m{color:#6b7280;font-size:14px;margin:0 0 20px}
input{width:100%%;box-sizing:border-box;padding:10px 12px;border:1px solid #d1d5db;border-radius:8px;font-size:14px;margin-bottom:12px}
button{width:100%%;background:#5e6ad2;color:#fff;border:0;border-radius:8px;padding:11px;font-size:14px;font-weight:600;cursor:pointer}
.err{color:#dc2626;font-size:13px;margin:10px 0 0;min-height:18px}
.f{color:#9ca3af;font-size:12px;margin-top:20px}</style></head><body><div class="c">
<h1>🔒 %s</h1><p class="m">Borang ini dilindungi kata laluan.</p>
<form id="g" onsubmit="return unlock(event)">
<input type="password" id="pw" placeholder="Kata laluan" autocomplete="current-password" required>
<button type="submit">Buka kunci</button>
<p class="err" id="err"></p>
</form><p class="f">Dikuasakan oleh JomForm</p></div>
<script>
async function unlock(e){e.preventDefault();var p=document.getElementById('pw').value;
var r=await fetch('/%s/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:p})});
if(r.ok){location.reload();}else{var d=await r.json().catch(function(){return{};});document.getElementById('err').textContent=d.error||'Kata laluan salah';}
return false;}
</script></body></html>`,
		f.Title, f.Title, f.Slug)
}

// formAccessCookie names the signed cookie that unlocks a private form.
const formAccessCookie = "jf_form_access"

// signFormAccessToken mints a signed, expiring token binding a browser to a
// private form. Payload: "<formID>|<expiryUnix>|<hmac>".
func signFormAccessToken(key []byte, formID string) (string, error) {
	if len(key) != 32 {
		return "", errors.New("access key must be 32 bytes")
	}
	expiry := time.Now().Add(7 * 24 * time.Hour).Unix()
	payload := formID + "|" + strconv.FormatInt(expiry, 10)
	mac := hmacSHA256(key, payload)
	raw := payload + "|" + mac
	return base64.RawURLEncoding.EncodeToString([]byte(raw)), nil
}

// verifyFormAccessToken validates a signed private-form access token and
// returns the form id it was minted for ("" when invalid/expired).
func verifyFormAccessToken(key []byte, token string) (string, bool) {
	if len(key) != 32 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", false
	}
	formID, expiryStr, mac := parts[0], parts[1], parts[2]
	if formID == "" {
		return "", false
	}
	payload := formID + "|" + expiryStr
	if !hmac.Equal([]byte(mac), []byte(hmacSHA256(key, payload))) {
		return "", false
	}
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", false
	}
	return formID, true
}

// hmacSHA256 returns a base64url HMAC-SHA256 of data under key.
func hmacSHA256(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// customDomainOwner returns the account slug that claimed this host via
// custom_domain, or "" when the host is not a registered custom domain.
func customDomainOwner(ctx context.Context, s *dbstore.AccountStore, host string) string {
	h := host
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	slug, err := s.SlugByCustomDomain(ctx, strings.ToLower(h))
	if err != nil {
		return ""
	}
	return slug
}

// handleSpaceRootForm serves a form by slug on a SPACE subdomain
// (e.g. wanzulhosting.jomform.com/form-baru). On jomform.com it 404s so
// dashboard routes are unaffected. Private forms (issue #176) resolve here so
// their password gate can render; serveForm enforces the status rules.
func (h *PublicHandlers) handleSpaceRootForm(w http.ResponseWriter, r *http.Request) {
	sub := accountSubdomain(r.Host)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	slug := r.PathValue("slug")
	f, err := h.Forms.BySpaceSlug(r.Context(), sub, slug)
	if err != nil {
		http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
		return
	}
	h.serveForm(w, r, f)
}

// handleSpaceRoot serves the space storefront (list of published forms).
func (h *PublicHandlers) handleSpaceRoot(w http.ResponseWriter, r *http.Request) {
	sub := accountSubdomain(r.Host)
	if sub == "" {
		http.NotFound(w, r)
		return
	}
	forms, err := h.Forms.ListPublishedBySpace(r.Context(), sub)
	if err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, "<!doctype html><html><head><meta charset='utf-8'><title>"+sub+"</title>")
	io.WriteString(w, "<style>body{font-family:system-ui;background:#f7f8f8;margin:0;padding:40px}")
	io.WriteString(w, ".g{max-width:640px;margin:0 auto;display:grid;gap:12px}")
	io.WriteString(w, ".g a{background:#fff;border:1px solid #e6e8ec;border-radius:14px;padding:18px 20px;")
	io.WriteString(w, "color:#17181c;text-decoration:none;font-weight:600;display:block}")
	io.WriteString(w, "</style></head><body><div class='g'><h1 style='font-size:22px'>Kedai "+sub+"</h1>")
	if len(forms) == 0 {
		io.WriteString(w, "<p>Belum ada borang.</p>")
	}
	for _, f := range forms {
		io.WriteString(w, "<a href='/"+f.Slug+"'>"+f.Title+"</a>")
	}
	io.WriteString(w, "</div></body></html>")
}
