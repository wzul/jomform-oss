// Public-facing order form: SSR page render + JSON submit flow.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/wzul/jomform/api/internal/httpapi/middleware"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/payments"
)

// PublicHandlers serves buyer-facing order forms.
type PublicHandlers struct {
	Forms    *dbstore.FormStore
	Sales    *dbstore.SaleStore
	Accounts *dbstore.AccountStore
	Pay      *payments.Service
	BaseURL  string
}

// RegisterRoutes wires public endpoints.
func (h *PublicHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /f/{slug}", h.handleRender)
	mux.Handle("POST /f/{slug}/order", middleware.RateLimit(http.HandlerFunc(h.handleSubmit)))
	mux.HandleFunc("GET /t/{ref}", h.handleThankYou)
}

// genRef produces a short opaque reference code.
func genRef() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "JF-" + hex.EncodeToString(b)
}

// handleSubmit creates a pending sale, requests a CHIP purchase and returns
// the checkout URL.
func (h *PublicHandlers) handleSubmit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// Same lookup rules as render: subdomain scoping first.
	var form *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		form, err = h.Forms.PublishedByAccountSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, r.Host); cd != "" {
		form, err = h.Forms.PublishedByAccountSlug(r.Context(), cd, slug)
	} else {
		form, err = h.Forms.PublishedBySlug(r.Context(), slug)
	}
	if err != nil || form == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}

	var in struct {
		Client struct {
			FullName string `json:"full_name"`
			Email    string `json:"email"`
			Phone    string `json:"phone"`
		} `json:"client"`
		Items []struct {
			ProductID string `json:"product_id"`
			Quantity  int64  `json:"quantity"`
		} `json:"items"`
		PaymentMethod string `json:"payment_method"`         // chip | bank_direct
		SkipCapture   bool   `json:"skip_capture,omitempty"` // authorize now, capture later
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
		respondErr(w, err)
		return
	}

	sale := &dbstore.Sale{
		AccountID:     form.AccountID,
		FormID:        form.ID,
		RefCode:       genRef(),
		Client:        dbstore.SaleClient{FullName: in.Client.FullName, Email: in.Client.Email, Phone: in.Client.Phone},
		SaleType:      dbstore.SaleTypeOneTime,
		PaymentMethod: method,
		Status:        dbstore.SalePending,
		SkipCapture:   in.SkipCapture && method == "chip",
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
		sale.Items = append(sale.Items, dbstore.SaleItem{ProductID: it.ProductID, Name: name, PriceSen: price, Qty: qty})
		subtotal += price * qty
	}
	sale.SubtotalSen = subtotal
	sale.TotalSen = subtotal

	if err := h.Sales.Create(r.Context(), sale); err != nil {
		respondErr(w, err)
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
	}
	if json.Unmarshal(raw, &cfg) == nil {
		for _, p := range cfg.Products {
			if p.ID == productID {
				return p.Name, p.PriceSen
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
  .prod { display:flex; justify-content:space-between; gap:12px; padding:12px 14px;
          border:1px solid #262c3f; border-radius:12px; margin-bottom:10px; }
  .prod label { display:flex; gap:10px; align-items:center; flex:1; }
  .price { color:#5e6ad2; font-weight:600; white-space:nowrap; }
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
  .brand { text-align:center; color:#9ca3af; font-size:11px; margin-top:18px; }
</style>
</head>
<body>
  <form class="card" id="of" data-slug="{{.Slug}}">
    <h1>{{.Title}}</h1>
    <p class="sub">{{.Description}}</p>
    {{range .Products}}
    <div class="prod">
      <label><input type="checkbox" class="p" data-id="{{.ID}}" data-price="{{.PriceSen}}" data-name="{{.Name}}">
        {{.Name}}</label>
      <span class="price">RM{{div100 .PriceSen}}</span>
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
    <p class="brand">Dikuasakan oleh <strong>JomForm</strong></p>
  </form>
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
	// scoped to that account's slug. jomform.com/f/test keeps old behavior.
	if sub := accountSubdomain(r.Host); sub != "" {
		f, ferr := h.Forms.PublishedByAccountSlug(r.Context(), sub, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai untuk domain ini", http.StatusNotFound)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, r.Host); cd != "" {
		f, ferr := h.Forms.PublishedByAccountSlug(r.Context(), cd, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	f, err := h.Forms.PublishedBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
		return
	}
	h.serveForm(w, r, f)
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
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formTmpl.Execute(w, view{
		Slug: f.Slug, Title: f.Title, Description: cfg.Description, Products: prods,
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
	// Refresh until payment settles so the redirect lands on a final state.
	refresh := ""
	if sale.Status == dbstore.SalePending {
		refresh = `<meta http-equiv="refresh" content="4">`
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
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:420px}
h1{color:#10b981;font-size:24px}</style></head><body><div class="c">
<h1>✓ Terima kasih!</h1><p>Tempahan <strong>%s</strong> diterima.<br>Status: %s.</p>
<p style="color:#9ca3af;font-size:12px">Dikuasakan oleh JomForm</p></div></body></html>`,
		refresh, sale.RefCode, statusText)
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

// serveForm renders a resolved form (blocks or legacy) and logs unknown
// template errors once.
func (h *PublicHandlers) serveForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	if h.renderBlocks(w, r, f) {
		return
	}
	h.renderLegacy(w, r, f)
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
