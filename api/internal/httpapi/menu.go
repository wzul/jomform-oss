// Issue #438: QR ordering flow — restaurant-menu landing template.
//
// A merchant can enable a clean restaurant menu on their workspace root
// (Beep-style): categories, item images, prices and a best-seller highlight,
// built on the landing page (#360) + the workspace's published products. A
// customer scans a QR stand at the table, lands on this menu, picks items and
// pays online (CHIP) — the order reaches the merchant with the table number.
//
// The menu reuses the parent LandingPage's hero/logo/theme colors and groups
// the workspace's active products into the configured categories. Products
// not in any category are shown under an "All" section. Each product links to
// the form that sells it (the first published form in the workspace), so the
// customer taps an item and lands on the order form.
package httpapi

import (
	"context"
	"html/template"
	"net/http"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menuProductView is one product card on the menu page.
type menuProductView struct {
	ID          string
	Name        string
	Description string
	Price       string
	ImageURL    string
	FormSlug    string
	BestSeller  bool
}

// menuCategoryView is one named group of products on the menu page.
type menuCategoryView struct {
	Name     string
	Products []menuProductView
}

// workspaceMenuData is the template payload for the restaurant menu.
type workspaceMenuData struct {
	Slug       string
	Lp         *dbstore.LandingPage
	Categories []menuCategoryView
	// All is the "everything else" section (products not in any category).
	All []menuProductView
	// HasForms reports whether at least one published form exists (used to
	// decide the "no menu yet" hint).
	HasForms bool
}

// menuPageTmpl renders the restaurant-menu landing template (issue #438). It
// is SEO-friendly server-side HTML and reuses the landing-page layout language.
// All dynamic text is HTML-escaped by template autoescaping.
var menuPageTmpl = template.Must(template.New("workspace-menu").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Lp.Hero}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:{{.Lp.BackgroundColor}}; color:#17181c; min-height:100vh; }
  .wrap { max-width:860px; margin:0 auto; padding:40px 24px 72px; }
  .hero { text-align:center; padding:16px 0 8px; }
  .logo { max-width:120px; max-height:80px; object-fit:contain; margin:0 auto 12px;
          display:{{if .Lp.LogoURL}}block{{else}}none{{end}}; }
  .hero h1 { margin:0 0 8px; font-size:34px; line-height:1.15; letter-spacing:-.02em;
             color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}}; }
  .hero .sub { margin:0; font-size:16px; color:#6b7280; }
  .banner { width:100%; max-height:360px; object-fit:cover; border-radius:20px;
            margin:24px 0 0; display:{{if .Lp.ImageURL}}block{{else}}none{{end}}; }
  .cat { margin-top:36px; }
  .cat h2 { margin:0 0 16px; font-size:22px; color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}};
            border-bottom:2px solid {{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#e6e8ec{{end}}; padding-bottom:8px; }
  .grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(240px,1fr)); gap:16px; }
  .item { background:#fff; border:1px solid #e6e8ec; border-radius:16px; overflow:hidden;
          text-decoration:none; color:inherit; display:flex; flex-direction:column;
          transition:box-shadow .15s ease, transform .15s ease; }
  .item:hover { box-shadow:0 4px 16px rgba(23,24,28,0.10); transform:translateY(-1px); }
  .item .img { width:100%; height:150px; object-fit:cover; background:#f3f4f6;
               display:{{if .ImageURL}}block{{else}}none{{end}}; }
  .item .body { padding:14px 16px 16px; display:flex; flex-direction:column; gap:6px; flex:1; }
  .item .nm { font-weight:600; font-size:15px; display:flex; align-items:center; gap:6px; }
  .item .desc { font-size:13px; color:#6b7280; line-height:1.5; }
  .item .price { font-weight:700; font-size:15px; margin-top:auto;
                 color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}}; }
  .best { display:inline-block; background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
          color:#fff; font-size:10px; font-weight:700; padding:2px 8px; border-radius:999px;
          text-transform:uppercase; letter-spacing:.04em; }
  .hint { margin-top:16px; color:#9ca3af; font-size:14px; text-align:center; }
  .foot { margin-top:48px; text-align:center; color:#9ca3af; font-size:12px; }
</style>
</head>
<body>
  <div class="wrap">
    <div class="hero">
      <img class="logo" src="{{.Lp.LogoURL}}" alt="">
      <h1>{{.Lp.Hero}}</h1>
      {{if .Lp.Subtitle}}<p class="sub">{{.Lp.Subtitle}}</p>{{end}}
    </div>
    <img class="banner" src="{{.Lp.ImageURL}}" alt="{{.Lp.Hero}}">
    {{if .Categories}}
      {{range .Categories}}
      <section class="cat">
        <h2>{{.Name}}</h2>
        <div class="grid">
          {{range .Products}}{{template "menuItem" .}}{{end}}
        </div>
      </section>
      {{end}}
    {{end}}
    {{if .All}}
      <section class="cat">
        <h2>All</h2>
        <div class="grid">
          {{range .All}}{{template "menuItem" .}}{{end}}
        </div>
      </section>
    {{end}}
    {{if not .HasForms}}<p class="hint">Belum ada menu.</p>{{end}}
    <p class="foot">Powered by JomForm</p>
  </div>
</body>
</html>`))

// menuItemTmpl is the reusable product card on the menu page.
var menuItemTmpl = template.Must(template.New("menuItem").Parse(`
<a class="item" href="/{{.FormSlug}}">
  <img class="img" src="{{.ImageURL}}" alt="{{.Name}}">
  <div class="body">
    <div class="nm">{{.Name}}{{if .BestSeller}}<span class="best">Best-seller</span>{{end}}</div>
    {{if .Description}}<div class="desc">{{.Description}}</div>{{end}}
    <div class="price">RM {{.Price}}</div>
  </div>
</a>`))

// renderWorkspaceMenu writes the restaurant-menu landing template (issue
// #438). It resolves the workspace's active products, groups them into the
// configured categories, and links each item to the first published form in
// the workspace so a customer can order.
func (h *PublicHandlers) renderWorkspaceMenu(w http.ResponseWriter, r *http.Request, slug, workspaceID string, lp *dbstore.LandingPage) {
	// Resolve the workspace's account(s) to list their active products.
	acc, aerr := h.Accounts.ByWorkspaceID(r.Context(), workspaceID)
	if aerr != nil || acc == nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
		return
	}
	products, perr := h.Products.ListActiveByAccount(r.Context(), acc.ID)
	if perr != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
		return
	}
	// The first published form is the order target for every item.
	formSlug := h.firstPublishedFormSlug(r.Context(), acc.Slug)

	byID := map[string]*dbstore.Product{}
	for _, p := range products {
		byID[p.ID] = p
	}
	// Group products into the configured categories; track which are placed.
	placed := map[string]bool{}
	var cats []menuCategoryView
	if lp.Menu != nil {
		for _, c := range lp.Menu.Categories {
			cv := menuCategoryView{Name: c.Name}
			for _, pid := range c.ProductIDs {
				p, ok := byID[pid]
				if !ok {
					continue
				}
				cv.Products = append(cv.Products, h.menuProductView(p, lp.Menu.BestSeller == p.ID, formSlug))
				placed[pid] = true
			}
			if len(cv.Products) > 0 {
				cats = append(cats, cv)
			}
		}
	}
	// Everything not in a category goes under "All".
	var all []menuProductView
	for _, p := range products {
		if !placed[p.ID] {
			all = append(all, h.menuProductView(p, lp.Menu != nil && lp.Menu.BestSeller == p.ID, formSlug))
		}
	}

	data := workspaceMenuData{
		Slug:       slug,
		Lp:         lp,
		Categories: cats,
		All:        all,
		HasForms:   len(products) > 0,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := menuPageTmpl.Execute(w, data); err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
	}
}

// menuProductView builds one product card for the menu page.
func (h *PublicHandlers) menuProductView(p *dbstore.Product, bestSeller bool, formSlug string) menuProductView {
	img := ""
	if len(p.Images) > 0 {
		img = p.Images[0]
	}
	return menuProductView{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Price:       formatPriceSen(p.PriceSen),
		ImageURL:    img,
		FormSlug:    formSlug,
		BestSeller:  bestSeller,
	}
}

// firstPublishedFormSlug returns the slug of the first published form in the
// workspace (the order target for menu items), or "" when none.
func (h *PublicHandlers) firstPublishedFormSlug(ctx context.Context, accountSlug string) string {
	forms, err := h.Forms.ListPublishedByAccountSlug(ctx, accountSlug)
	if err != nil || len(forms) == 0 {
		return ""
	}
	return forms[0].Slug
}

// formatPriceSen renders an integer sen amount as "12.50".
func formatPriceSen(sen int64) string {
	neg := sen < 0
	if neg {
		sen = -sen
	}
	whole := sen / 100
	frac := sen % 100
	fracStr := "00"
	if frac < 10 {
		fracStr = "0" + itoa(frac)
	} else {
		fracStr = itoa(frac)
	}
	if neg {
		return "-" + itoa(whole) + "." + fracStr
	}
	return itoa(whole) + "." + fracStr
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
