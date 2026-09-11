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
// not in any category are shown under an "All" section.
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
// Issue #492: the menu is now a real ordering surface — add to cart → basket →
// checkout → CHIP. Before, every item was an <a href> to the workspace's first
// published form (firstPublishedFormSlug), so clicking "Nasi Lemak RM18" landed
// the buyer on a form titled "Beli Produk Pertama" selling a different product
// at a different price. That link is gone: an item is now an add-to-cart
// control and the page checks out through POST /menu-order (issue #491, the
// workspace-scoped order endpoint that needs no form), sending
// items[{product_id, quantity}] + client{full_name,email} + table_number +
// affiliate_code. The basket persists in localStorage keyed by workspace, so
// filtering/searching or reloading never loses it. A sticky bar shows the live
// count/total and opens the basket sheet.
//
// SCOPE: this is RESTAURANT-MENU ONLY. Regular forms (RSVP, wedding, etc.)
// keep their existing form flow completely untouched — the cart lives entirely
// in this template + the menu-order endpoint.
//
// The page stays SERVER-RENDERED: the controls are plain HTML (so a GET submit
// filters server-side without JS) and a tiny inline script upgrades them to
// instant client-side filtering plus the cart. No build step, no client
// framework.
package httpapi

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// menuProductView is one product card / list row on the menu page.
type menuProductView struct {
	ID          string
	Name        string
	Description string
	// Price is the display price (e.g. "18.00"); PriceSen is the integer sen
	// amount (issue #492) shipped to the client so the basket can render a
	// display-only running total. The server remains the price authority: the
	// order endpoint re-reads every price from the products table.
	Price    string
	PriceSen int64
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
	// BestSeller marks the item for the best-seller badge.
	BestSeller bool
	// BestLabel is the localised best-seller badge text (issue #485). It is
	// resolved in Go because the inline {{define}} sub-templates receive the
	// product view as their data and cannot reach the page-level $.Ls.
	BestLabel string
	// AddLabel is the localised add-to-cart label (issue #492), resolved in Go
	// for the same reason.
	AddLabel string
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
	// Aff is the affiliate code read from ?aff=CODE (issue #444/#492). It is
	// echoed back on checkout so the sale is attributed to that affiliate, and
	// carried through the no-JS filter GET round-trip.
	Aff string
	// RenderTS is the page render time (Unix seconds), embedded so the cart
	// checkout can submit the same anti-spam timestamp every public form does.
	RenderTS int64
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
// redesigned in #485, cart in #492). It is SEO-friendly server-side HTML and
// reuses the landing-page layout language. All dynamic text is HTML-escaped by
// template autoescaping.
//
// The reusable product views are defined INLINE as {{define "menuCard"}} /
// {{define "menuRow"}} so the {{template ... .}} calls below resolve. A
// separate template.New("menuItem") is NOT visible to this template at execute
// time and made the whole menu page fail with `no such template "menuItem"`
// (issue #461/#438).
//
// Issue #492: a card/row is a <button type="button"> (whole surface tappable,
// mobile-first) carrying data-id/data-price — it is NOT an <a href> to a form
// any more. The inline script below owns the cart; the localised labels the
// script needs are shipped as data-* attributes on #menuSheet so no template
// action has to live inside the JS.
var menuPageTmpl = template.Must(template.New("workspace-menu").Parse(`{{define "menuCard"}}
<button class="card" type="button" data-id="{{.ID}}" data-name="{{.Name}}" data-desc="{{.Description}}" data-price="{{.PriceSen}}" data-add="{{.AddLabel}}" aria-label="{{.AddLabel}}: {{.Name}}">
  {{if .CardImage}}<img class="cimg" src="{{.CardImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
  <div class="cbody">
    <div class="cname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    <div class="cfoot"><span class="cprice">RM {{.Price}}</span><span class="plus" aria-hidden="true">+</span></div>
  </div>
  <span class="qtybadge" hidden></span>
</button>{{end}}{{define "menuRow"}}
<button class="row" type="button" data-id="{{.ID}}" data-name="{{.Name}}" data-desc="{{.Description}}" data-price="{{.PriceSen}}" data-add="{{.AddLabel}}" aria-label="{{.AddLabel}}: {{.Name}}">
  <div class="rtext">
    <div class="rname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    {{if .Description}}<div class="rdesc">{{.Description}}</div>{{end}}
    <div class="rprice">RM {{.Price}}<span class="plus" aria-hidden="true">+</span></div>
  </div>
  {{if .ThumbImage}}<img class="rthumb" src="{{.ThumbImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
  <span class="qtybadge" hidden></span>
</button>{{end}}<!doctype html>
<html lang="{{.Ls.Lang}}"{{if eq .Ls.Lang "ar"}} dir="rtl"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Lp.Hero}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  [hidden] { display:none !important; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:{{if .Lp.BackgroundColor}}{{.Lp.BackgroundColor}}{{else}}#f7f8f8{{end}};
         color:#17181c; min-height:100vh; }
  .wrap { max-width:860px; margin:0 auto; padding:10px 16px 64px; }
  body.has-cart .wrap { padding-bottom:104px; }
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
  .card { position:relative; display:block; width:100%; text-align:start; padding:0; border:0;
          background:transparent; font:inherit; color:inherit; cursor:pointer; }
  .card .cimg { width:100%; aspect-ratio:1 / 1; object-fit:cover; border-radius:12px;
                background:#f3f4f6; display:block; }
  .card .cbody { padding:8px 2px 0; }
  .card .cname { font-weight:700; font-size:14px; line-height:1.35;
                 display:flex; align-items:center; flex-wrap:wrap; gap:6px; }
  .card .cfoot { margin-top:4px; display:flex; align-items:center; justify-content:space-between; gap:8px; }
  .card .cprice { font-size:14px; font-weight:600; color:#3f434a; }
  .card:hover .cimg { opacity:.94; }
  .card:active .cimg { opacity:.88; }

  /* category items: list rows, text left + thumbnail right, separated */
  .rows { display:flex; flex-direction:column; }
  .row { position:relative; display:flex; align-items:flex-start; gap:14px; padding:14px 0;
         width:100%; text-align:start; border:0; background:transparent; font:inherit;
         color:inherit; cursor:pointer; }
  .row + .row { border-top:1px solid #e9ebef; }
  .row .rtext { flex:1 1 auto; min-width:0; }
  .row .rname { font-weight:700; font-size:15px; line-height:1.35;
                display:flex; align-items:center; flex-wrap:wrap; gap:6px; }
  .row .rdesc { margin-top:3px; font-size:13px; color:#6b7280; line-height:1.5; }
  .row .rprice { margin-top:6px; font-size:14px; font-weight:600; color:#3f434a;
                 display:flex; align-items:center; gap:8px; }
  .row .rthumb { flex:0 0 auto; width:100px; height:100px; object-fit:cover;
                 border-radius:12px; background:#f3f4f6; }
  .row:active { background:rgba(23,24,28,.03); }

  /* add-to-cart affordance + on-item quantity badge (issue #492) */
  .plus { flex:0 0 auto; width:22px; height:22px; border-radius:50%; display:inline-flex;
          align-items:center; justify-content:center; font-size:15px; font-weight:700;
          line-height:1; color:#fff; background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}}; }
  .qtybadge { position:absolute; top:8px; inset-inline-end:8px; min-width:22px; height:22px;
              padding:0 6px; border-radius:999px; display:inline-flex; align-items:center;
              justify-content:center; font-size:12px; font-weight:700; color:#fff;
              background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
              box-shadow:0 2px 6px rgba(23,24,28,.25); }

  .best { display:inline-block; background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
          color:#fff; font-size:10px; font-weight:700; padding:2px 8px; border-radius:999px;
          text-transform:uppercase; letter-spacing:.04em; }
  .hint { margin-top:20px; color:#9ca3af; font-size:14px; text-align:center; }
  .foot { margin-top:44px; text-align:center; color:#9ca3af; font-size:12px; }

  /* ---- sticky basket bar (issue #492) ---- */
  .bar { position:fixed; inset-inline:0; bottom:0; z-index:30; padding:10px 16px;
         padding-bottom:calc(10px + env(safe-area-inset-bottom)); pointer-events:none; }
  .bar .barin { max-width:860px; margin:0 auto; pointer-events:auto; }
  .baropen { width:100%; display:flex; align-items:center; justify-content:space-between; gap:12px;
             border:0; border-radius:14px; padding:14px 16px; font:inherit; font-size:15px;
             font-weight:700; color:#fff; cursor:pointer;
             background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
             box-shadow:0 8px 24px rgba(23,24,28,.22); }
  .baropen .barcount { display:inline-flex; align-items:center; gap:6px; }
  .baropen .barcta { opacity:.92; font-weight:600; }

  /* ---- basket sheet (issue #492) ---- */
  .sheet { position:fixed; inset:0; z-index:40; }
  .sheetback { position:absolute; inset:0; background:rgba(23,24,28,.42); }
  .sheetpanel { position:absolute; inset-inline:0; bottom:0; max-height:88vh; display:flex;
                flex-direction:column; background:#fff; border-radius:18px 18px 0 0;
                box-shadow:0 -8px 32px rgba(23,24,28,.24); }
  .sheethead { display:flex; align-items:center; justify-content:space-between; gap:8px;
               padding:14px 16px; border-bottom:1px solid #f0f1f4; }
  .sheethead h2 { margin:0; font-size:17px; font-weight:700; }
  .sheetclose { width:36px; height:36px; border:0; border-radius:50%; background:transparent;
                font-size:24px; line-height:1; color:#6b7280; cursor:pointer; }
  .sheetclose:hover { background:rgba(23,24,28,.06); }
  .sheetbody { overflow:auto; padding:4px 16px; }
  .sheetempty { padding:26px 16px; text-align:center; color:#9ca3af; font-size:14px; }
  .sheetfoot { padding:12px 16px; padding-bottom:calc(16px + env(safe-area-inset-bottom));
               border-top:1px solid #f0f1f4; }
  .subrow { display:flex; justify-content:space-between; gap:12px; font-size:14px; padding:3px 0; }
  .subrow.total { font-weight:700; font-size:16px; }
  .clearbtn { margin-top:6px; border:0; background:transparent; color:#9ca3af; font-size:13px;
              cursor:pointer; padding:4px 0; }
  .clearbtn:hover { color:#6b7280; }
  .sheetfoot label { display:block; font-size:13px; font-weight:600; margin:10px 0 0; }
  .sheetfoot input { width:100%; height:44px; margin-top:5px; border:1px solid #e6e8ec;
                     border-radius:10px; padding:0 12px; font:inherit; font-size:15px; color:#17181c;
                     background:#fff; }
  .sheetfoot input:focus { outline:2px solid {{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}};
                           outline-offset:1px; }
  .paybtn { width:100%; margin-top:14px; height:50px; border:0; border-radius:12px;
            font:inherit; font-size:16px; font-weight:700; color:#fff; cursor:pointer;
            background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}}; }
  .paybtn:disabled { opacity:.6; cursor:default; }
  .err { margin:10px 0 0; color:#b91c1c; font-size:13px; line-height:1.45; }

  /* basket line item */
  .line { display:flex; align-items:center; gap:10px; padding:12px 0; }
  .line + .line { border-top:1px solid #f0f1f4; }
  .line .ltext { flex:1 1 auto; min-width:0; }
  .line .lname { font-weight:600; font-size:14px; line-height:1.35; word-break:break-word; }
  .line .lprice { margin-top:2px; font-size:13px; color:#6b7280; }
  .stepper { flex:0 0 auto; display:flex; align-items:center; gap:2px;
             border:1px solid #e6e8ec; border-radius:999px; padding:2px; }
  .stepper button { width:28px; height:28px; border:0; border-radius:50%; background:transparent;
                    font:inherit; font-size:16px; line-height:1; color:#17181c; cursor:pointer; }
  .stepper button:hover { background:rgba(23,24,28,.06); }
  .stepper .qty { min-width:20px; text-align:center; font-size:14px; font-weight:600; }
  .lrm { flex:0 0 auto; border:0; background:transparent; color:#9ca3af; font-size:12px;
         cursor:pointer; padding:4px; }
  .lrm:hover { color:#b91c1c; }

  @media (min-width:560px) {
    .wrap { padding:18px 24px 72px; }
    body.has-cart .wrap { padding-bottom:112px; }
    .hname h1 { font-size:24px; }
    .searchwrap { flex:0 1 240px; }
    .sheetpanel { inset-inline:auto; inset-inline-start:50%; transform:translateX(-50%);
                  width:480px; bottom:24px; border-radius:18px; }
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
      {{if .Aff}}<input type="hidden" name="aff" value="{{.Aff}}">{{end}}
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

  {{/* Issue #492: sticky basket bar — shown by the script while the cart is non-empty. */}}
  <div class="bar" id="menuBar" hidden>
    <div class="barin">
      <button class="baropen" type="button" id="menuBarBtn">
        <span class="barcount"><span id="menuBarCount">0</span>&nbsp;<span>{{.Ls.MenuItems}}</span></span>
        <span class="bartotal" id="menuBarTotal">RM 0.00</span>
        <span class="barcta">{{.Ls.MenuViewBasket}}</span>
      </button>
    </div>
  </div>

  {{/* Issue #492: basket + checkout sheet. The localised labels the script needs
       ride on data-* attributes so no template action has to live inside the JS. */}}
  <div class="sheet" id="menuSheet" hidden
       data-slug="{{.Slug}}"
       data-table="{{.Table}}"
       data-aff="{{.Aff}}"
       data-ts="{{.RenderTS}}"
       data-lbl-inc="{{.Ls.MenuInc}}"
       data-lbl-dec="{{.Ls.MenuDec}}"
       data-lbl-remove="{{.Ls.MenuRemove}}"
       data-lbl-checkout="{{.Ls.MenuCheckout}}"
       data-lbl-submitting="{{.Ls.MenuSubmitting}}"
       data-lbl-error="{{.Ls.MenuSubmitError}}"
       data-lbl-required="{{.Ls.MenuRequired}}">
    <div class="sheetback" id="menuSheetBack"></div>
    <div class="sheetpanel" role="dialog" aria-modal="true" aria-label="{{.Ls.MenuBasket}}">
      <div class="sheethead">
        <h2>{{.Ls.MenuBasket}}</h2>
        <button class="sheetclose" type="button" id="menuSheetClose" aria-label="{{.Ls.MenuClose}}">&times;</button>
      </div>
      <div class="sheetbody">
        <div id="menuLines"></div>
        <p class="sheetempty" id="menuEmpty" hidden>{{.Ls.MenuBasketEmpty}}</p>
      </div>
      <div class="sheetfoot">
        <div class="subrow total"><span>{{.Ls.MenuSubtotal}}</span><span id="menuSubtotal">RM 0.00</span></div>
        {{if .Table}}<div class="subrow"><span>{{.Ls.MenuTable}}</span><span>{{.Table}}</span></div>{{end}}
        <button class="clearbtn" type="button" id="menuClear">{{.Ls.MenuClear}}</button>
        <form id="menuCheckout" novalidate>
          <label for="menuName">{{.Ls.MenuName}}
            <input id="menuName" name="full_name" type="text" autocomplete="name" required>
          </label>
          <label for="menuEmail">{{.Ls.EmailLabel}}
            <input id="menuEmail" name="email" type="email" autocomplete="email" required>
          </label>
          <div style="display:none" aria-hidden="true">
            <label>Website<input type="text" name="website" tabindex="-1" autocomplete="off"></label>
          </div>
          <button class="paybtn" type="submit" id="menuPay">{{.Ls.MenuCheckout}}</button>
          <p class="err" id="menuErr" hidden></p>
        </form>
      </div>
    </div>
  </div>

<script>
(function(){
  // Issue #485: progressive enhancement for the category filter + search.
  // The controls are plain HTML and work without JS (a GET submit filters
  // server-side); this script upgrades them to instant client-side filtering.
  var form=document.getElementById('menuFilters');
  if(form){
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
  }
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

// Issue #492: restaurant-menu cart + checkout.
//   - tapping a card/row adds the item (no navigation to a form);
//   - the cart persists in localStorage keyed by workspace, so filtering,
//     searching or reloading never loses the basket;
//   - the sticky bar shows the live count + total and opens the basket sheet;
//   - checkout posts to the workspace order endpoint (POST /menu-order, #491)
//     with items[{product_id,quantity}], client{full_name,email},
//     table_number and affiliate_code, then redirects to the CHIP checkout URL.
// Totals shown here are DISPLAY ONLY — the server re-reads every price from the
// products table and re-sums the authoritative total.
(function(){
  var sheet=document.getElementById('menuSheet');
  if(!sheet) return;
  var bar=document.getElementById('menuBar');
  var barBtn=document.getElementById('menuBarBtn');
  var barCount=document.getElementById('menuBarCount');
  var barTotal=document.getElementById('menuBarTotal');
  var linesEl=document.getElementById('menuLines');
  var emptyEl=document.getElementById('menuEmpty');
  var subEl=document.getElementById('menuSubtotal');
  var clearBtn=document.getElementById('menuClear');
  var payForm=document.getElementById('menuCheckout');
  var errEl=document.getElementById('menuErr');
  var payBtn=document.getElementById('menuPay');
  var sheetClose=document.getElementById('menuSheetClose');
  var sheetBack=document.getElementById('menuSheetBack');

  function attr(n){ return sheet.getAttribute(n)||''; }
  var slug=attr('data-slug');
  var ts=Number(attr('data-ts'))||0;
  var loadAt=Date.now();

  // Issue #492/#489/#444: carry ?table=N and ?aff=CODE through to the order.
  // They reach the page from the QR stand / affiliate link; read them
  // client-side (same pattern as the public forms) and fall back to the
  // server-rendered values.
  var qs=new URLSearchParams(location.search);
  var table=qs.get('table')||attr('data-table')||'';
  var aff=qs.get('aff')||attr('data-aff')||'';

  var KEY='jf.menu.cart.v1.'+slug;
  var cart=[];
  try{ var raw=localStorage.getItem(KEY); if(raw) cart=JSON.parse(raw)||[]; }catch(e){ cart=[]; }
  if(!Array.isArray(cart)) cart=[];
  cart=cart.filter(function(it){ return it&&it.id; });
  for(var ci=0;ci<cart.length;ci++){ cart[ci].qty=Math.max(1,parseInt(cart[ci].qty,10)||1); cart[ci].price=Math.max(0,parseInt(cart[ci].price,10)||0); }
  function save(){ try{ localStorage.setItem(KEY,JSON.stringify(cart)); }catch(e){} }
  function find(id){ for(var i=0;i<cart.length;i++){ if(cart[i].id===id) return cart[i]; } return null; }
  function count(){ var c=0; for(var i=0;i<cart.length;i++){ c+=cart[i].qty; } return c; }
  function totalSen(){ var t=0; for(var i=0;i<cart.length;i++){ t+=cart[i].price*cart[i].qty; } return t; }
  function rm(sen){ return 'RM '+(Math.round(sen)/100).toFixed(2); }

  // Part-3 hook: merchant-configured extra order fields (validated server-side
  // against the attached form's contract) can be merged in here.
  function extraFields(){ return {}; }
  window.jfMenuExtraFields=extraFields;

  function addItem(id,name,price){
    if(!id) return;
    var it=find(id);
    if(it){ it.qty++; } else { cart.push({id:id,name:name,price:Math.max(0,parseInt(price,10)||0),qty:1}); }
    save(); render();
  }
  function setQty(id,delta){
    var it=find(id);
    if(!it) return;
    it.qty+=delta;
    if(it.qty<1){ removeItem(id); return; }
    save(); render();
  }
  function removeItem(id){
    cart=cart.filter(function(it){ return it.id!==id; });
    save(); render();
  }
  function clearCart(){ cart=[]; save(); render(); }

  function renderBadges(){
    document.querySelectorAll('.card[data-id],.row[data-id]').forEach(function(el){
      var it=find(el.getAttribute('data-id'));
      var b=el.querySelector('.qtybadge');
      if(!b) return;
      if(it&&it.qty>0){ b.textContent=String(it.qty); b.hidden=false; }
      else { b.hidden=true; }
    });
  }
  function renderLines(){
    linesEl.textContent='';
    for(var i=0;i<cart.length;i++){
      (function(it){
        var row=document.createElement('div'); row.className='line';
        var text=document.createElement('div'); text.className='ltext';
        var nm=document.createElement('div'); nm.className='lname'; nm.textContent=it.name||'';
        var pr=document.createElement('div'); pr.className='lprice'; pr.textContent=rm(it.price)+' × '+it.qty+' = '+rm(it.price*it.qty);
        text.appendChild(nm); text.appendChild(pr);
        var step=document.createElement('div'); step.className='stepper';
        var dec=document.createElement('button'); dec.type='button'; dec.textContent='−';
        dec.setAttribute('aria-label',attr('data-lbl-dec'));
        dec.addEventListener('click',function(){ setQty(it.id,-1); });
        var q=document.createElement('span'); q.className='qty'; q.textContent=String(it.qty);
        var inc=document.createElement('button'); inc.type='button'; inc.textContent='+';
        inc.setAttribute('aria-label',attr('data-lbl-inc'));
        inc.addEventListener('click',function(){ setQty(it.id,1); });
        step.appendChild(dec); step.appendChild(q); step.appendChild(inc);
        var rmBtn=document.createElement('button'); rmBtn.type='button'; rmBtn.className='lrm';
        rmBtn.textContent=attr('data-lbl-remove');
        rmBtn.addEventListener('click',function(){ removeItem(it.id); });
        row.appendChild(text); row.appendChild(step); row.appendChild(rmBtn);
        linesEl.appendChild(row);
      })(cart[i]);
    }
  }
  function render(){
    var n=count();
    renderBadges();
    renderLines();
    emptyEl.hidden=n>0;
    subEl.textContent=rm(totalSen());
    barCount.textContent=String(n);
    barTotal.textContent=rm(totalSen());
    bar.hidden=n===0;
    if(n===0) closeSheet();
    document.body.classList.toggle('has-cart',n>0);
  }
  function openSheet(){ sheet.hidden=false; errEl.hidden=true; sheetClose.focus(); }
  function closeSheet(){ sheet.hidden=true; }
  document.addEventListener('keydown',function(e){
    if(e.key==='Escape'&&!sheet.hidden) closeSheet();
  });

  // The whole card/row is the add button (mobile-first).
  document.addEventListener('click',function(e){
    var el=e.target&&e.target.closest?e.target.closest('.card[data-id],.row[data-id]'):null;
    if(!el) return;
    addItem(el.getAttribute('data-id'),el.getAttribute('data-name'),el.getAttribute('data-price'));
  });
  barBtn.addEventListener('click',openSheet);
  sheetClose.addEventListener('click',closeSheet);
  sheetBack.addEventListener('click',closeSheet);
  clearBtn.addEventListener('click',clearCart);

  payForm.addEventListener('submit',function(e){
    e.preventDefault();
    if(!cart.length) return;
    var nameEl=payForm.querySelector('[name=full_name]');
    var emailEl=payForm.querySelector('[name=email]');
    var name=nameEl.value.trim(), email=emailEl.value.trim();
    errEl.hidden=true;
    // Name + email are BOTH mandatory: CHIP needs them to create a purchase.
    if(!name||!email){ errEl.textContent=attr('data-lbl-required'); errEl.hidden=false; return; }
    var fields=Object.assign({
      website:(payForm.querySelector('[name=website]')||{}).value||'',
      jf_ts:String(ts)
    },extraFields());
    var body={
      client:{full_name:name,email:email},
      items:cart.map(function(it){ return {product_id:it.id,quantity:it.qty}; }),
      payment_method:'chip',
      table_number:table||'',
      affiliate_code:aff||'',
      fields:fields
    };
    payBtn.disabled=true; payBtn.textContent=attr('data-lbl-submitting');
    function fail(msg){
      errEl.textContent=msg||attr('data-lbl-error'); errEl.hidden=false;
      payBtn.disabled=false; payBtn.textContent=attr('data-lbl-checkout');
    }
    // Anti-spam layer 3 rejects a submit arriving under 3s after the render
    // timestamp we send. Wait out the remainder here (measured on this clock,
    // so a skewed client clock cannot cause a false rejection) and keep the
    // basket on any failure so the buyer can retry.
    var wait=Math.max(0,3500-(Date.now()-loadAt));
    setTimeout(function(){
      fetch('/menu-order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
        .then(function(r){ return r.json().then(function(d){ return {ok:r.ok,d:d||{}}; }); })
        .then(function(res){
          var d=res.d;
          if(d.checkout_url||d.next){
            try{ localStorage.removeItem(KEY); }catch(e){}
            location.href=d.checkout_url||d.next;
            return;
          }
          fail(d.error);
        })
        .catch(function(){ fail(''); });
    },wait);
  });

  render();
})();
</script>
</body>
</html>`))

// renderWorkspaceMenu writes the restaurant-menu landing template (issue
// #438, redesigned in #485, cart in #492). It resolves the workspace's active
// products and groups them into the configured categories. Every item is an
// add-to-cart control — the page checks out through POST /menu-order (#491),
// so no item routes the buyer into a form. The table number from ?table=N and
// the affiliate code from ?aff=CODE are shown/echoed, and every user-facing
// string is localised to the workspace's default language.
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

	// Issue #485: the table number the QR stand encoded (?table=N, see qr.go),
	// carried on the order as sales.table_number. Shown in the header and
	// preserved on the filter/search round-trip.
	table := strings.TrimSpace(r.URL.Query().Get("table"))
	if runes := []rune(table); len(runes) > 24 {
		table = string(runes[:24])
	}
	// Issue #444/#492: the affiliate code the buyer landed with (?aff=CODE),
	// echoed on checkout so the sale is attributed to that affiliate.
	aff := strings.TrimSpace(r.URL.Query().Get("aff"))
	if runes := []rune(aff); len(runes) > 64 {
		aff = string(runes[:64])
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
				cv.Products = append(cv.Products, h.menuProductView(p, lp.Menu.IsBestSeller(p.ID), ls.MenuBestBadge, ls.MenuAdd))
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
			all = append(all, h.menuProductView(p, lp.Menu != nil && lp.Menu.IsBestSeller(p.ID), ls.MenuBestBadge, ls.MenuAdd))
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
			best = append(best, h.menuProductView(p, true, ls.MenuBestBadge, ls.MenuAdd))
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
		Aff:            aff,
		RenderTS:       antiSpamRenderTime(),
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
func (h *PublicHandlers) menuProductView(p *dbstore.Product, bestSeller bool, bestLabel, addLabel string) menuProductView {
	img := ""
	if len(p.Images) > 0 {
		img = p.Images[0]
	}
	// Issue #382/#471: resolve the media variant that fits each tier — the
	// square card wants the medium variant, the row thumbnail the small one.
	// A non-canonical URL (external, GIF/SVG) is returned unchanged.
	cardImg := productImageForSize(img, ProductImageSizeMedium)
	thumbImg := productImageForSize(img, ProductImageSizeSmall)
	return menuProductView{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Price:       formatPriceSen(p.PriceSen),
		PriceSen:    p.PriceSen,
		ImageURL:    img,
		CardImage:   cardImg,
		ThumbImage:  thumbImg,
		BestSeller:  bestSeller,
		BestLabel:   bestLabel,
		AddLabel:    addLabel,
	}
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
