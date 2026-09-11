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
//
// Issue #485: the page was redesigned to match the reference layout —
//   - header: back arrow + table number (?table=N, encoded by the QR stand in
//     qr.go and carried on the order as sales.table_number), cafe name + an
//     info (i) affordance, then the location line;
//   - a filter row: a category <select> (native, progressive) + a search
//     input with a magnifier submit button;
//   - two tiers: best-sellers as a 2-column grid of square-image cards, and
//     every category as list rows (text left, ~100px thumbnail right) with a
//     separator between rows;
//   - items without an image render as clean text-only cards/rows — no empty
//     image slot.
//
// The page stays SERVER-RENDERED: the controls are plain HTML (so a GET submit
// filters server-side without JS) and a tiny inline script upgrades them to
// instant client-side filtering. No build step, no client framework.
package httpapi

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menuProductView is one product card / list row on the menu page.
type menuProductView struct {
	ID          string
	Name        string
	Description string
	Price       string
	// ImageURL is the LIVE product image (#461) at its base/full size. It is
	// shipped as the client-side fallback (data-base) for an image uploaded
	// before #382, which has no generated variants.
	ImageURL string
	// CardImage is the image URL for the square best-seller card: the medium
	// media variant (#382/#471) of the live product image.
	CardImage string
	// ThumbImage is the image URL for the ~100px list-row thumbnail: the
	// thumbnail media variant (#382/#471) of the live product image.
	ThumbImage string
	// Href is the order target (the workspace's first published form), with
	// the table number preserved when the page was opened with ?table=N.
	Href string
	// FormSlug is the raw order-form slug (kept for callers/tests).
	FormSlug string
	// BestSeller marks the item for the best-seller badge.
	BestSeller bool
	// BestLabel is the localised best-seller badge text (issue #485). It is
	// resolved in Go because the inline {{define}} sub-templates receive the
	// product view as their data and cannot reach the page-level $.Ls.
	BestLabel string
}

// menuCategoryView is one named group of products on the menu page.
type menuCategoryView struct {
	Name     string
	Products []menuProductView
}

// workspaceMenuData is the template payload for the restaurant menu.
type workspaceMenuData struct {
	Slug string
	Lp   *dbstore.LandingPage
	// Ls is the buyer-facing dictionary for the workspace's default language
	// (issue #485) — every user-facing string on the page is localised.
	Ls langStrings
	// Table is the table number read from ?table=N (issue #485), empty when
	// the page was not opened from a per-table QR stand.
	Table string
	// ContactAddress / ContactPhone feed the info (i) panel.
	ContactAddress string
	ContactPhone   string
	// BestSellers is the top section: 2-column square-image cards.
	BestSellers []menuProductView
	Categories  []menuCategoryView
	// All is the "everything else" section (products not in any category).
	All []menuProductView
	// Query / Cat echo the active filter state so the controls keep their
	// value after a no-JS GET submit (issue #485).
	Query string
	Cat   string
	// HasForms reports whether at least one published form exists (used to
	// decide the "no menu yet" hint).
	HasForms bool
}

// langStringsForCode returns the buyer-facing dictionary for a language code,
// falling back to English for an unknown/empty code (issue #485).
func langStringsForCode(code string) langStrings {
	if ls, ok := formLangs[code]; ok {
		return ls
	}
	return formLangs["en"]
}

// menuPageTmpl renders the restaurant-menu landing template (issue #438,
// redesigned in #485). It is SEO-friendly server-side HTML and reuses the
// landing-page layout language. All dynamic text is HTML-escaped by template
// autoescaping.
//
// The reusable product views are defined INLINE as {{define "menuCard"}} /
// {{define "menuRow"}} so the {{template ... .}} calls below resolve. A
// separate template.New("menuItem") is NOT visible to this template at execute
// time and made the whole menu page fail with `no such template "menuItem"`
// (issue #461/#438).
var menuPageTmpl = template.Must(template.New("workspace-menu").Parse(`{{define "menuCard"}}
<a class="card" href="{{.Href}}" data-name="{{.Name}}" data-desc="{{.Description}}">
  {{if .CardImage}}<img class="cimg" src="{{.CardImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
  <div class="cbody">
    <div class="cname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    <div class="cprice">RM {{.Price}}</div>
  </div>
</a>{{end}}{{define "menuRow"}}
<a class="row" href="{{.Href}}" data-name="{{.Name}}" data-desc="{{.Description}}">
  <div class="rtext">
    <div class="rname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    {{if .Description}}<div class="rdesc">{{.Description}}</div>{{end}}
    <div class="rprice">RM {{.Price}}</div>
  </div>
  {{if .ThumbImage}}<img class="rthumb" src="{{.ThumbImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
</a>{{end}}<!doctype html>
<html lang="{{.Ls.Lang}}"{{if eq .Ls.Lang "ar"}} dir="rtl"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Lp.Hero}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:{{if .Lp.BackgroundColor}}{{.Lp.BackgroundColor}}{{else}}#f7f8f8{{end}};
         color:#17181c; min-height:100vh; }
  .wrap { max-width:860px; margin:0 auto; padding:10px 16px 64px; }
  a { color:inherit; }

  /* ---- header: back arrow + table number, cafe name + info, location ---- */
  .hrow { display:flex; align-items:center; justify-content:space-between; min-height:40px; }
  .back { width:40px; height:40px; margin-inline-start:-8px; display:inline-flex;
          align-items:center; justify-content:center; border-radius:50%; color:#17181c;
          text-decoration:none; }
  .back:hover { background:rgba(23,24,28,.06); }
  .tbl { font-weight:700; font-size:15px; letter-spacing:-.01em; }
  .hname { display:flex; align-items:center; gap:8px; margin-top:2px; }
  .hname h1 { margin:0; font-size:20px; font-weight:700; line-height:1.25; letter-spacing:-.02em; }
  .info { position:relative; }
  .info summary { list-style:none; cursor:pointer; width:22px; height:22px; border-radius:50%;
                  display:inline-flex; align-items:center; justify-content:center;
                  color:#6b7280; border:1px solid #d7dae0; background:#fff; }
  .info summary::-webkit-details-marker { display:none; }
  .info[open] summary { color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}}; }
  .ipanel { position:absolute; inset-inline-end:0; top:30px; z-index:5; min-width:210px; max-width:78vw;
            background:#fff; border:1px solid #e6e8ec; border-radius:12px; padding:10px 12px;
            box-shadow:0 8px 24px rgba(23,24,28,.12); font-size:13px; color:#3f434a; }
  .ipanel .irow { padding:2px 0; }
  .ipanel .irow + .irow { border-top:1px solid #f0f1f4; margin-top:2px; padding-top:6px; }
  .loc { margin:6px 0 0; font-size:13px; color:#6b7280; line-height:1.5; }

  /* ---- filter row: category pill + search ---- */
  .filters { display:flex; align-items:center; gap:10px; margin:18px 0 6px; }
  .pill { position:relative; flex:1 1 auto; min-width:0; }
  .pill .book, .pill .chev { position:absolute; top:50%; transform:translateY(-50%);
                             pointer-events:none; color:#6b7280; }
  .pill .book { inset-inline-start:14px; }
  .pill .chev { inset-inline-end:14px; }
  .catsel { width:100%; height:44px; appearance:none; -webkit-appearance:none; margin:0;
            border:1px solid #e6e8ec; border-radius:999px; background:#fff;
            padding-inline:42px; font:inherit; font-size:14px; font-weight:600; color:#17181c;
            text-overflow:ellipsis; }
  .catsel:focus { outline:2px solid {{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
                  outline-offset:1px; }
  .searchwrap { flex:0 1 168px; min-width:0; display:flex; align-items:center; gap:4px;
                height:44px; border:1px solid #e6e8ec; border-radius:999px; background:#fff;
                padding-inline:14px 4px; }
  .search { flex:1 1 auto; min-width:0; border:0; outline:0; background:transparent;
            font:inherit; font-size:14px; color:#17181c; }
  .mag { flex:0 0 auto; width:36px; height:36px; display:inline-flex; align-items:center;
         justify-content:center; border:0; border-radius:50%; background:transparent;
         color:#6b7280; cursor:pointer; }
  .mag:hover { background:rgba(23,24,28,.06); }
  .searchwrap:focus-within { border-color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}}; }

  /* ---- sections ---- */
  .sec { margin-top:24px; }
  .sec h2 { margin:0 0 12px; padding-bottom:8px; font-size:18px; font-weight:700;
            letter-spacing:-.01em; color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}};
            border-bottom:1px solid #e6e8ec; }

  /* best sellers: 2-column square-image cards */
  .cards { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
  .card { display:block; text-decoration:none; }
  .card .cimg { width:100%; aspect-ratio:1 / 1; object-fit:cover; border-radius:12px;
                background:#f3f4f6; display:block; }
  .card .cbody { padding:8px 2px 0; }
  .card .cname { font-weight:700; font-size:14px; line-height:1.35;
                 display:flex; align-items:center; flex-wrap:wrap; gap:6px; }
  .card .cprice { margin-top:4px; font-size:14px; font-weight:600; color:#3f434a; }
  .card:hover .cimg { opacity:.94; }

  /* category items: list rows, text left + thumbnail right, separated */
  .rows { display:flex; flex-direction:column; }
  .row { display:flex; align-items:flex-start; gap:14px; padding:14px 0;
         text-decoration:none; }
  .row + .row { border-top:1px solid #e9ebef; }
  .row .rtext { flex:1 1 auto; min-width:0; }
  .row .rname { font-weight:700; font-size:15px; line-height:1.35;
                display:flex; align-items:center; flex-wrap:wrap; gap:6px; }
  .row .rdesc { margin-top:3px; font-size:13px; color:#6b7280; line-height:1.5; }
  .row .rprice { margin-top:6px; font-size:14px; font-weight:600; color:#3f434a; }
  .row .rthumb { flex:0 0 auto; width:100px; height:100px; object-fit:cover;
                 border-radius:12px; background:#f3f4f6; }

  .best { display:inline-block; background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
          color:#fff; font-size:10px; font-weight:700; padding:2px 8px; border-radius:999px;
          text-transform:uppercase; letter-spacing:.04em; }
  .hint { margin-top:20px; color:#9ca3af; font-size:14px; text-align:center; }
  .foot { margin-top:44px; text-align:center; color:#9ca3af; font-size:12px; }

  @media (min-width:560px) {
    .wrap { padding:18px 24px 72px; }
    .hname h1 { font-size:24px; }
    .searchwrap { flex:0 1 240px; }
  }
</style>
</head>
<body>
  <div class="wrap">
    <header>
      <div class="hrow">
        <a class="back" href="/" aria-label="{{.Ls.MenuBack}}"
           onclick="if(history.length&gt;1){history.back();return false;}">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6"/></svg>
        </a>
        {{if .Table}}<div class="tbl">{{.Ls.MenuTable}} {{.Table}}</div>{{end}}
      </div>
      <div class="hname">
        <h1>{{if .Lp.Hero}}{{.Lp.Hero}}{{else}}{{.Slug}}{{end}}</h1>
        <details class="info">
          <summary aria-label="{{.Ls.MenuInfo}}" title="{{.Ls.MenuInfo}}">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 8h.01"/></svg>
          </summary>
          <div class="ipanel">
            {{if .Lp.Hero}}<div class="irow">{{.Lp.Hero}}</div>{{end}}
            {{if .Lp.Subtitle}}<div class="irow">{{.Lp.Subtitle}}</div>{{end}}
            {{if .ContactAddress}}<div class="irow">{{.ContactAddress}}</div>{{end}}
            {{if .ContactPhone}}<div class="irow">{{.ContactPhone}}</div>{{end}}
          </div>
        </details>
      </div>
      {{if .Lp.Subtitle}}<p class="loc">{{.Lp.Subtitle}}</p>{{end}}
    </header>

    <form class="filters" id="menuFilters" method="get" action="" role="search">
      {{if .Table}}<input type="hidden" name="table" value="{{.Table}}">{{end}}
      <div class="pill">
        <svg class="book" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 5a2 2 0 0 1 2-2h4v18H6a2 2 0 0 1-2-2z"/><path d="M20 5a2 2 0 0 0-2-2h-4v18h4a2 2 0 0 0 2-2z"/></svg>
        <select class="catsel" name="cat" aria-label="{{.Ls.MenuFilterLabel}}">
          <option value="">{{.Ls.MenuBestSeller}}</option>
          {{range .Categories}}<option value="{{.Name}}"{{if eq $.Cat .Name}} selected{{end}}>{{.Name}}</option>{{end}}
        </select>
        <svg class="chev" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>
      </div>
      <div class="searchwrap">
        <input class="search" type="search" name="q" value="{{.Query}}" placeholder="{{.Ls.MenuSearchPlaceholder}}" aria-label="{{.Ls.MenuSearchPlaceholder}}">
        <button class="mag" type="submit" aria-label="{{.Ls.MenuSearchPlaceholder}}">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg>
        </button>
      </div>
    </form>

    {{if .BestSellers}}
    <section class="sec" data-best="1" data-cat="">
      <h2>{{.Ls.MenuBestSeller}}</h2>
      <div class="cards">
        {{range .BestSellers}}{{template "menuCard" .}}{{end}}
      </div>
    </section>
    {{end}}

    {{range .Categories}}
    <section class="sec" data-cat="{{.Name}}">
      <h2>{{.Name}}</h2>
      <div class="rows">
        {{range .Products}}{{template "menuRow" .}}{{end}}
      </div>
    </section>
    {{end}}

    {{if .All}}
    <section class="sec" data-cat="{{.Ls.MenuAll}}">
      <h2>{{.Ls.MenuAll}}</h2>
      <div class="rows">
        {{range .All}}{{template "menuRow" .}}{{end}}
      </div>
    </section>
    {{end}}

    {{if not .HasForms}}<p class="hint">{{.Ls.MenuEmpty}}</p>{{end}}
    <p class="foot">{{.Ls.MenuPoweredBy}}</p>
  </div>
<script>
(function(){
  // Issue #485: progressive enhancement for the category filter + search.
  // The controls are plain HTML and work without JS (a GET submit filters
  // server-side); this script upgrades them to instant client-side filtering.
  var form=document.getElementById('menuFilters');
  if(!form) return;
  var sel=form.querySelector('select[name=cat]');
  var q=form.querySelector('input[name=q]');
  var secs=document.querySelectorAll('.sec');
  function each(list,fn){ for(var i=0;i<list.length;i++){ fn(list[i]); } }
  function norm(s){ return (s||'').toLowerCase(); }
  function apply(){
    var qv=norm(q?q.value.trim():'');
    var catv=sel?sel.value:'';
    each(secs,function(sec){
      var isBest=sec.getAttribute('data-best')==='1';
      var secCat=sec.getAttribute('data-cat')||'';
      var secShown=!catv || (!isBest && secCat===catv);
      var any=false;
      each(sec.querySelectorAll('.card,.row'),function(it){
        var hay=norm(it.getAttribute('data-name')+' '+(it.getAttribute('data-desc')||''));
        var ok=secShown && (!qv || hay.indexOf(qv)>=0);
        it.style.display=ok?'':'none';
        if(ok) any=true;
      });
      sec.style.display=(secShown&&any)?'':'none';
    });
  }
  if(sel) sel.addEventListener('change',apply);
  if(q) q.addEventListener('input',apply);
  form.addEventListener('submit',function(e){ e.preventDefault(); apply(); });
})();
// Issue #471: product image variant fallback. The cards/rows point at the
// medium/thumbnail media variant (#382). An image uploaded before #382 has no
// generated variants, so if that variant object 404s we fall back to the base
// image (data-base) — one attempt only, so a broken URL cannot loop.
document.querySelectorAll('img[data-base]').forEach(function(im){
  im.addEventListener('error',function(){
    var base=im.getAttribute('data-base');
    if(base&&im.getAttribute('src')!==base){im.setAttribute('src',base);}
  },{once:true});
});
</script>
</body>
</html>`))

// renderWorkspaceMenu writes the restaurant-menu landing template (issue
// #438, redesigned in #485). It resolves the workspace's active products,
// groups them into the configured categories, and links each item to the first
// published form in the workspace so a customer can order. The table number
// from ?table=N is shown in the header, and every user-facing string is
// localised to the workspace's default language.
func (h *PublicHandlers) renderWorkspaceMenu(w http.ResponseWriter, r *http.Request, slug, workspaceID string, st *dbstore.WorkspaceSettings) {
	lp := st.LandingPage
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

	// Issue #485: the table number the QR stand encoded (?table=N, see qr.go),
	// carried on the order as sales.table_number. Shown in the header and
	// preserved on the order links so the filter/search round-trip keeps it.
	table := strings.TrimSpace(r.URL.Query().Get("table"))
	if runes := []rune(table); len(runes) > 24 {
		table = string(runes[:24])
	}
	ls := langStringsForCode(st.WorkspaceLang())

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
				cv.Products = append(cv.Products, h.menuProductView(p, lp.Menu.IsBestSeller(p.ID), formSlug, table, ls.MenuBestBadge))
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
			all = append(all, h.menuProductView(p, lp.Menu != nil && lp.Menu.IsBestSeller(p.ID), formSlug, table, ls.MenuBestBadge))
		}
	}
	// Best sellers: the top section, rendered as 2-column square-image cards.
	// BestSellerIDs (issue #485, an optional JSON-blob extension) wins; the
	// legacy single BestSeller id is the fallback.
	var best []menuProductView
	if lp.Menu != nil {
		for _, pid := range lp.Menu.BestSellerProductIDs() {
			p, ok := byID[pid]
			if !ok {
				continue
			}
			best = append(best, h.menuProductView(p, true, formSlug, table, ls.MenuBestBadge))
		}
	}

	// Server-side filtering so the controls work without JS: ?q= matches an
	// item's name/description, ?cat= narrows to one category (ignored when it
	// matches no category, so a stale value never blanks the page).
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cat := strings.TrimSpace(r.URL.Query().Get("cat"))
	if query != "" {
		lq := strings.ToLower(query)
		best = filterMenuItems(best, lq)
		for i := range cats {
			cats[i].Products = filterMenuItems(cats[i].Products, lq)
		}
		all = filterMenuItems(all, lq)
		var kept []menuCategoryView
		for _, c := range cats {
			if len(c.Products) > 0 {
				kept = append(kept, c)
			}
		}
		cats = kept
	}
	if cat != "" {
		var only []menuCategoryView
		for _, c := range cats {
			if strings.EqualFold(c.Name, cat) {
				only = append(only, c)
			}
		}
		if len(only) > 0 {
			cats = only
			best = nil
			all = nil
		}
	}

	data := workspaceMenuData{
		Slug:           slug,
		Lp:             lp,
		Ls:             ls,
		Table:          table,
		ContactAddress: st.ContactAddress,
		ContactPhone:   st.ContactPhone,
		BestSellers:    best,
		Categories:     cats,
		All:            all,
		HasForms:       len(products) > 0,
		Query:          query,
		Cat:            cat,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := menuPageTmpl.Execute(w, data); err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
	}
}

// filterMenuItems keeps the items whose name or description contains q
// (already lower-cased) — the no-JS server-side search (issue #485).
func filterMenuItems(items []menuProductView, q string) []menuProductView {
	var out []menuProductView
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Name), q) ||
			strings.Contains(strings.ToLower(it.Description), q) {
			out = append(out, it)
		}
	}
	return out
}

// menuProductView builds one product card / list row for the menu page.
func (h *PublicHandlers) menuProductView(p *dbstore.Product, bestSeller bool, formSlug, table, bestLabel string) menuProductView {
	img := ""
	if len(p.Images) > 0 {
		img = p.Images[0]
	}
	// Issue #382/#471: resolve the media variant that fits each tier — the
	// square card wants the medium variant, the row thumbnail the small one.
	// A non-canonical URL (external, GIF/SVG) is returned unchanged.
	cardImg := productImageForSize(img, ProductImageSizeMedium)
	thumbImg := productImageForSize(img, ProductImageSizeSmall)
	href := "/"
	if formSlug != "" {
		href = "/" + formSlug
	}
	if table != "" {
		href += "?" + url.Values{"table": {table}}.Encode()
	}
	return menuProductView{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Price:       formatPriceSen(p.PriceSen),
		ImageURL:    img,
		CardImage:   cardImg,
		ThumbImage:  thumbImg,
		Href:        href,
		FormSlug:    formSlug,
		BestSeller:  bestSeller,
		BestLabel:   bestLabel,
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
