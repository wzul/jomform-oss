// Issue #497: the restaurant menu is a PER-FORM setting, served at the form's
// OWN public URL (/{formSlug}).
//
// Before #497 the menu config lived on the workspace landing page
// (workspace_settings.landing_page_json) and the workspace ROOT rendered it —
// which meant (a) turning the menu on hijacked the whole storefront and hid the
// merchant's forms, (b) the menu sold EVERY active product in the account
// (ListActiveByAccount) so unrelated junk leaked in, and (c) a workspace could
// only ever have one menu. Wan's call: the menu belongs to a FORM, the same
// shape as the optional order form in #493.
//
// Now:
//   - the config lives on the form (forms.config.menu, see dbstore.MenuFromConfig);
//   - the workspace root renders the normal landing page / form listing again;
//   - a menu form renders ONLY the products its own product blocks reference —
//     no account-wide leakage, no "All" section;
//   - two menu forms in one workspace coexist with different categories;
//   - non-menu forms (RSVP/wedding) are completely untouched.
//
// A menu form's own product blocks ARE the menu source of truth: the block's
// product_id and the block's label (the merchant's per-item display name) are
// read from the form config, and each referenced product resolves LIVE from the
// products table (#461) with the form snapshot as the archived/deleted
// fallback. A menu form with no product blocks renders an empty-state — it
// NEVER falls back to "every account product".
//
// The page stays SERVER-RENDERED with the INLINE template (menuPageTmpl; the
// {{define}} blocks are inline on purpose — do not split into a separate
// template.New, that bug is documented below). No React, no build step.
//
// SCOPE: this is RESTAURANT-MENU ONLY. Regular forms keep their existing form
// flow completely untouched — the cart lives entirely in this template + the
// form-scoped order endpoint.
package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/buyertext"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/formfields"
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
	// Issue #665: Variations are the product's variants, resolved LIVE from
	// product_variations. A non-empty list makes the card a variable item: the
	// buyer must choose one variant and the chosen variant's price is what is
	// charged. This is the same shape the ordinary form block renderer uses
	// (render_blocks.go) — one convention, not a second one.
	Variations []variationView
	// SelectLabel is the localised empty option of the variant selector (e.g.
	// "Select an option"), InStockLabel the localised stock suffix, and
	// PickLabel the localised prompt a variable item shows until the buyer
	// chooses a variant. All three are resolved in Go because the item
	// sub-templates receive this view as their data and cannot reach the
	// page-level $.Ls.
	SelectLabel  string
	InStockLabel string
	PickLabel    string
	// SoldOutLabel is the localised "sold out" message a pick made on a
	// disabled (sold-out) option shows instead of adding the line. Resolved
	// here for the same reason as the labels above: the sub-templates cannot
	// reach the page-level $.Ls.
	SoldOutLabel string
	// Variable reports whether this item has a variant selector, so the cart
	// script can require a choice instead of adding the base price.
	Variable bool
}

// menuCategoryView is one named group of products on the menu page.
type menuCategoryView struct {
	Name     string
	Products []menuProductView
}

// menuFieldView is one merchant-configured extra checkout field on the menu
// page (issue #493). The field blocks come from the published form the
// merchant attached to the menu (Settings → Storefront → Restaurant menu);
// the checkout renders them with the form's own labels/required flags and the
// backend re-validates them against the same contract.
//
// The buyer's name and email are ALWAYS collected separately and are never
// part of this list — CHIP requires both to create a purchase.
type menuFieldView struct {
	ID          string
	Type        string
	Label       string
	Required    bool
	Placeholder string
	Value       string
	Choices     []formfields.Choice
	// RequiredMark is the localised "required" marker (resolved in Go because
	// the inline {{define}} sub-template receives the field view as its data
	// and cannot reach the page-level $.Ls).
	RequiredMark string
	// SelectLabel is the localised empty-option label for a select field
	// (same resolution reason).
	SelectLabel string
}

// menuExtraContract derives the menu checkout's extra-field contract from the
// attached form's config (issue #493) with name/email blocks REMOVED. The
// checkout always collects the buyer's name + email as the mandatory
// full_name/email inputs (CHIP requires both), so a form that also declares a
// name/email block must not add a second, unrendered required field — that
// would make the order impossible to submit. Everything else (delivery
// address, phone, spice level, notes, pickup time…) is enforced as the
// merchant configured it.
func menuExtraContract(config map[string]any) []formfields.Field {
	var out []formfields.Field
	for _, f := range formfields.Contract(config) {
		if f.Type == "name" || f.Type == "email" {
			continue
		}
		// Card t_9e875bca: a block's conditional-logic rule is deliberately NOT
		// carried onto the menu checkout's contract. The menu page does not emit
		// the rule yet (menuExtraFields has no data-condition), so enforcing it
		// here would drop or excuse an answer for a field the buyer was shown —
		// the menu checkout keeps its pre-existing behaviour until its own
		// renderer can honour the rule.
		f.Condition = nil
		out = append(out, f)
	}
	return out
}

// menuExtraFields builds the checkout field views for the attached form's
// contract (issue #493). A name/email block is skipped: the checkout always
// collects both as the mandatory buyer fields, so rendering them again would
// duplicate inputs. Fields without a usable label fall back to their id.
func menuExtraFields(contract []formfields.Field, ls langStrings) []menuFieldView {
	var out []menuFieldView
	for _, f := range contract {
		if f.Type == "name" || f.Type == "email" {
			continue
		}
		label := strings.TrimSpace(f.Label)
		if label == "" {
			label = f.FieldID
		}
		out = append(out, menuFieldView{
			ID:           f.FieldID,
			Type:         f.Type,
			Label:        label,
			Required:     f.Required,
			Placeholder:  f.Placeholder,
			Value:        f.DefaultValue,
			Choices:      f.Choices,
			RequiredMark: ls.Required,
			SelectLabel:  ls.SelectOption,
		})
	}
	return out
}

// menuTheme is the resolved styling for a per-form menu page (issue #497). The
// menu is a FORM, so its hero/colors come from the form's theme map
// (set_form_theme / Form Settings → General) with the workspace's contact
// details as the info-panel fallback. A zero value is safe: every field is
// optional and the template has a default for each.
type menuTheme struct {
	// Hero is the menu heading — the form title.
	Hero string
	// Subtitle is the optional location line (workspace contact address).
	Subtitle string
	// PrimaryColor is the accent (hex, e.g. "#4f46e5"). Empty = default.
	PrimaryColor string
	// BackgroundColor is the page background (hex). Empty = default.
	BackgroundColor string
	// LogoURL is an optional brand logo.
	LogoURL string
}

// workspaceMenuData is the template payload for the per-form restaurant menu
// (issue #497). The menu is a FORM: Slug is the FORM's public slug (the page is
// served at /{formSlug}), Menu is the form's own config, and the products come
// from the form's own product blocks.
type workspaceMenuData struct {
	Slug string
	// Menu is the per-form menu config (forms.config.menu). Non-nil whenever
	// the page renders (renderFormMenu returns early otherwise).
	Menu *dbstore.MenuPage
	// Th is the resolved styling (form theme + workspace contact fallback).
	Th menuTheme
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
	// Query / Cat echo the active filter state so the controls keep their
	// value after a no-JS GET submit (issue #485).
	Query string
	Cat   string
	// HasItems reports whether the form referenced at least one renderable
	// product (used to decide the empty-state hint). A menu form with no
	// product blocks shows the hint instead of falling back to account
	// products (issue #497).
	HasItems bool
	// Issue #493: the optional form the merchant attached to the menu for
	// extra checkout info. ExtraFields are its field blocks (rendered in the
	// checkout step); ExtraFormSlug / ExtraFormID identify it and
	// ExtraFormEdit links into the form builder to edit those fields. When no
	// form is attached the checkout collects name + email only.
	ExtraFields   []menuFieldView
	ExtraFormSlug string
	// ExtraFormID is the attached form's id, so the helper's edit link can
	// open it directly in the builder.
	ExtraFormID string
	// ExtraFormEdit is the builder URL for the attached form.
	ExtraFormEdit string
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
var menuPageTmpl = template.Must(template.New("workspace-menu").
	Funcs(template.FuncMap{"isRTL": isRTL}).
	Parse(`{{define "menuCard"}}
<div class="item">
<button class="card" type="button" data-id="{{.ID}}" data-name="{{.Name}}" data-desc="{{.Description}}" data-price="{{.PriceSen}}" data-add="{{.AddLabel}}"{{if .Variable}} data-pick="{{.PickLabel}}"{{end}} aria-label="{{.AddLabel}}: {{.Name}}">
  {{if .CardImage}}<img class="cimg" src="{{.CardImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
  <div class="cbody">
    <div class="cname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    <div class="cfoot"><span class="cprice">RM {{.Price}}</span><span class="plus" aria-hidden="true">+</span></div>
  </div>
  <span class="qtybadge" hidden></span>
</button>
{{if .Variations}}<div class="mvarwrap"><select class="mvar" data-id="{{.ID}}" aria-label="{{.SelectLabel}}"><option value="" selected disabled>{{.SelectLabel}}</option>{{$it := .}}{{range .Variations}}<option value="{{.ID}}" data-vname="{{.Name}}" data-price="{{.PriceSen}}"{{if .SoldOut}} disabled{{end}}>{{.Name}} — RM {{.Price}}{{if .Stock}} ({{.Stock}} {{$it.InStockLabel}}){{end}}</option>{{end}}</select><div class="mvarstock" data-hint="{{.PickLabel}}" hidden>{{.PickLabel}}</div></div>{{end}}
</div>{{end}}{{define "menuRow"}}
<div class="item">
<button class="row" type="button" data-id="{{.ID}}" data-name="{{.Name}}" data-desc="{{.Description}}" data-price="{{.PriceSen}}" data-add="{{.AddLabel}}"{{if .Variable}} data-pick="{{.PickLabel}}"{{end}} aria-label="{{.AddLabel}}: {{.Name}}">
  <div class="rtext">
    <div class="rname">{{.Name}}{{if .BestSeller}}<span class="best">{{.BestLabel}}</span>{{end}}</div>
    {{if .Description}}<div class="rdesc">{{.Description}}</div>{{end}}
    <div class="rprice">RM {{.Price}}<span class="plus" aria-hidden="true">+</span></div>
  </div>
  {{if .ThumbImage}}<img class="rthumb" src="{{.ThumbImage}}" alt="{{.Name}}" data-base="{{.ImageURL}}" loading="lazy">{{end}}
  <span class="qtybadge" hidden></span>
</button>
{{if .Variations}}<div class="mvarwrap"><select class="mvar" data-id="{{.ID}}" aria-label="{{.SelectLabel}}"><option value="" selected disabled>{{.SelectLabel}}</option>{{$it := .}}{{range .Variations}}<option value="{{.ID}}" data-vname="{{.Name}}" data-price="{{.PriceSen}}"{{if .SoldOut}} disabled{{end}}>{{.Name}} — RM {{.Price}}{{if .Stock}} ({{.Stock}} {{$it.InStockLabel}}){{end}}</option>{{end}}</select><div class="mvarstock" data-hint="{{.PickLabel}}" hidden>{{.PickLabel}}</div></div>{{end}}
</div>{{end}}{{define "menuField"}}
  {{if eq .Type "select"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <select name="{{.ID}}"{{if .Required}} required{{end}}>
      <option value="" selected>{{.SelectLabel}}</option>
      {{range .Choices}}<option value="{{.Value}}">{{.Label}}</option>{{end}}
    </select>
  </label>
  {{/* Card t_7c931910: a radio group needs a programmatic name for the GROUP.
       The group heading used to be a bare sibling <label> with neither a for
       attribute nor a labelable descendant, which labels nothing — the buyer
       using a screen reader heard the options with no question. fieldset+legend
       is the native technique the W3C APG names the group with
       (https://www.w3.org/WAI/ARIA/apg/practices/names-and-descriptions/);
       the visible text and the required marker are unchanged. */}}
  {{else if eq .Type "radio"}}<fieldset class="radioswrap"><legend>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}</legend>
    <div class="radios">{{range .Choices}}<label class="rad"><input type="radio" name="{{$.ID}}" value="{{.Value}}"{{if $.Required}} required{{end}}> {{.Label}}</label>{{end}}</div>
  </fieldset>
  {{else if eq .Type "image-choice"}}{{if .Choices}}<fieldset class="radioswrap"><legend>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}</legend>
    <div class="radios">{{range .Choices}}<label class="rad"><input type="radio" name="{{$.ID}}" value="{{.Value}}"{{if $.Required}} required{{end}}> {{.Label}}</label>{{end}}</div>
  </fieldset>
  {{else}}{{/* Card t_efd99d75: a block that declares NO choices has nothing to draw a radio per choice for — an empty group would be a required field nobody can answer, which would block every order on this menu. Fall through to the text input so the field stays answerable (the same degenerate shape the public renderers leave blank). */}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="text" name="{{.ID}}" value="{{.Value}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>
  </label>{{end}}
  {{else if eq .Type "checkbox"}}<label class="chk"><input type="checkbox" name="{{.ID}}" value="1"{{if .Value}} checked{{end}}> {{.Label}}</label>
  {{else if eq .Type "consent"}}<label class="chk"><input type="checkbox" name="{{.ID}}" value="1"{{if .Required}} required{{end}}> {{.Label}}</label>
  {{else if eq .Type "textarea"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <textarea name="{{.ID}}" rows="2"{{if .Required}} required{{end}} placeholder="{{.Placeholder}}">{{.Value}}</textarea>
  </label>
  {{else if eq .Type "address"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <textarea name="{{.ID}}" rows="2"{{if .Required}} required{{end}} placeholder="{{.Placeholder}}">{{.Value}}</textarea>
  </label>
  {{else if eq .Type "list"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <textarea name="{{.ID}}" rows="3"{{if .Required}} required{{end}} placeholder="{{.Placeholder}}">{{.Value}}</textarea>
  </label>
  {{else if eq .Type "repeater"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <textarea name="{{.ID}}" rows="3"{{if .Required}} required{{end}} placeholder="{{.Placeholder}}">{{.Value}}</textarea>
  </label>
  {{else if eq .Type "number"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="number" name="{{.ID}}" value="{{.Value}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>
  </label>
  {{else if eq .Type "date"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="date" name="{{.ID}}" value="{{.Value}}"{{if .Required}} required{{end}}>
  </label>
  {{else if eq .Type "time"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="time" name="{{.ID}}" value="{{.Value}}"{{if .Required}} required{{end}}>
  </label>
  {{else if eq .Type "website"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="url" name="{{.ID}}" value="{{.Value}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>
  </label>
  {{else if eq .Type "hidden"}}<input type="hidden" name="{{.ID}}" value="{{.Value}}">
  {{else if eq .Type "phone"}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="tel" name="{{.ID}}" value="{{.Value}}" placeholder="{{.Placeholder}}" autocomplete="tel"{{if .Required}} required{{end}}>
  </label>
  {{else}}<label>{{.Label}}{{if .Required}} {{.RequiredMark}}{{end}}
    <input type="text" name="{{.ID}}" value="{{.Value}}" placeholder="{{.Placeholder}}"{{if .Required}} required{{end}}>
  </label>
  {{end}}{{end}}<!doctype html>
<html lang="{{.Ls.Lang}}"{{if isRTL .Ls.Lang}} dir="rtl"{{end}}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Th.Hero}}</title>
<style>
  :root { color-scheme: light; }
  * { box-sizing: border-box; }
  [hidden] { display:none !important; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:{{if .Th.BackgroundColor}}{{.Th.BackgroundColor}}{{else}}#f7f8f8{{end}};
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
  .info[open] summary { color:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; }
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
  .catsel:focus { outline:2px solid {{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
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
  .searchwrap:focus-within { border-color:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; }

  /* ---- sections ---- */
  .sec { margin-top:24px; }
  .sec h2 { margin:0 0 12px; padding-bottom:8px; font-size:18px; font-weight:700;
            letter-spacing:-.01em; color:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#17181c{{end}};
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
          line-height:1; color:#fff; background:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; }
  /* Issue #665: variant selector. A variable item's card/row is followed by a
     <select> (a select cannot legally live inside a <button>), so the card
     keeps its own wrapper for the quantity badge and the selector sits under
     it, full width.
     The class names are MENU-OWNED (mvar*/mvarstock), never the bare
     select.v/.varwrap the ordinary form block renderer uses: both scripts
     match on a class, so a shared name makes each script see the other's
     selectors when the two surfaces ever share one document (an embed or
     landing shell). See the menuPageTmpl note on the selector contract. */
  .item { position:relative; display:flex; flex-direction:column; }
  .item .qtybadge { top:8px; }
  .mvarwrap { padding:4px 2px 12px; }
  .mvarwrap select { width:100%; height:40px; border:1px solid #e6e8ec; border-radius:10px;
                     background:#fff; padding-inline:10px; font:inherit; font-size:14px;
                     color:#17181c; }
  .mvarwrap select:focus { outline:2px solid {{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
                           outline-offset:1px; }
  .mvarwrap select.missing { border-color:#dc2626; }
  .mvarstock { margin-top:5px; font-size:12px; color:#dc2626; }
  .rows .item + .item { border-top:1px solid #e9ebef; }
  .rows .item > .row { padding-bottom:4px; }
  .rows .item + .item > .row { border-top:0; }
  .qtybadge { position:absolute; top:8px; inset-inline-end:8px; min-width:22px; height:22px;
              padding:0 6px; border-radius:999px; display:inline-flex; align-items:center;
              justify-content:center; font-size:12px; font-weight:700; color:#fff;
              background:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
              box-shadow:0 2px 6px rgba(23,24,28,.25); }

  .best { display:inline-block; background:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
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
             background:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
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
  .sheetfoot input:focus { outline:2px solid {{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}};
                           outline-offset:1px; }
  /* Issue #493: extra checkout fields from the attached form. */
  .sheetfoot select, .sheetfoot textarea { width:100%; margin-top:5px; border:1px solid #e6e8ec;
                     border-radius:10px; padding:10px 12px; font:inherit; font-size:15px; color:#17181c;
                     background:#fff; }
  .sheetfoot select { height:44px; padding:0 12px; }
  .sheetfoot select:focus, .sheetfoot textarea:focus {
                     outline:2px solid {{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; outline-offset:1px; }
  .sheetfoot .chk { display:flex; align-items:center; gap:8px; font-size:14px; font-weight:500; margin-top:10px; }
  .sheetfoot .chk input { width:auto; height:auto; margin:0; }
  .sheetfoot .radios { display:flex; flex-direction:column; gap:4px; margin-top:5px; }
  .sheetfoot .radios .rad { display:flex; align-items:center; gap:8px; font-size:14px; font-weight:500; margin:0; }
  /* Card t_7c931910: the radio group is a <fieldset> so its <legend> names the
     group for assistive tech. Reset the user-agent box (border/padding/min-width)
     and give the legend the label styling the group heading had, so the page
     looks exactly as it did. */
  .sheetfoot .radioswrap { border:0; margin:10px 0 0; padding:0; min-width:0; }
  .sheetfoot .radioswrap legend { padding:0; font-size:13px; font-weight:600; }
  /* Card t_664549fc: the buyer-field rule above (width:100%; height:44px) also
     matches the radio of a radio / image-choice option, so each option's
     control measured 399x44 inside a flex row and pushed its label to the row
     edge (a plain radio is 13x13). Reset the option's control to its intrinsic
     size; .sheetfoot .radios input[type=radio] outranks .sheetfoot input on
     specificity, so it wins whatever the source order. */
  .sheetfoot .radios input[type=radio] { width:auto; height:auto; margin:0; padding:0;
                     border:0; background:none; flex:0 0 auto; }
  .extrahead { margin-top:12px; }
  .extrahead .extrahelp { margin:0; font-size:12px; color:#6b7280; line-height:1.5; }
  .extrahead .extralink { display:inline-block; margin-top:2px; font-size:12px; font-weight:600;
                          color:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; }
  .paybtn { width:100%; margin-top:14px; height:50px; border:0; border-radius:12px;
            font:inherit; font-size:16px; font-weight:700; color:#fff; cursor:pointer;
            background:{{if .Th.PrimaryColor}}{{.Th.PrimaryColor}}{{else}}#4f46e5{{end}}; }
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
        <h1>{{if .Th.Hero}}{{.Th.Hero}}{{else}}{{.Slug}}{{end}}</h1>
        <details class="info">
          <summary aria-label="{{.Ls.MenuInfo}}" title="{{.Ls.MenuInfo}}">
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><path d="M12 8h.01"/></svg>
          </summary>
          <div class="ipanel">
            {{if .Th.Hero}}<div class="irow">{{.Th.Hero}}</div>{{end}}
            {{if .Th.Subtitle}}<div class="irow">{{.Th.Subtitle}}</div>{{end}}
            {{if .ContactAddress}}<div class="irow">{{.ContactAddress}}</div>{{end}}
            {{if .ContactPhone}}<div class="irow">{{.ContactPhone}}</div>{{end}}
          </div>
        </details>
      </div>
      {{if .Th.Subtitle}}<p class="loc">{{.Th.Subtitle}}</p>{{end}}
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

    {{if not .HasItems}}<p class="hint">{{.Ls.MenuEmpty}}</p>{{end}}
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
       data-lbl-required="{{.Ls.MenuRequired}}"
       data-lbl-soldout="{{.Ls.MenuSoldOut}}"
       data-lbl-stockchanged="{{.Ls.MenuStockChanged}}">
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
          {{if .ExtraFields}}
          {{/* Issue #493: extra fields from the attached form, with a helper
               line that says where they come from and links to the builder. */}}
          <div class="extrahead">
            <p class="extrahelp">{{.Ls.MenuExtraHelp}}</p>
            {{if .ExtraFormEdit}}<a class="extralink" href="{{.ExtraFormEdit}}" target="_blank" rel="noreferrer">{{.Ls.MenuExtraEdit}}</a>{{end}}
          </div>
          {{range .ExtraFields}}{{template "menuField" .}}{{end}}
          {{end}}
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
//   - the cart persists in localStorage keyed by FORM SLUG (issue #497: the
//     menu is a form, so two menu forms in one workspace keep separate
//     baskets), so filtering, searching or reloading never loses the basket;
//   - the sticky bar shows the live count + total and opens the basket sheet;
//   - checkout posts to the FORM's own order endpoint (POST /{slug}/order,
//     issue #497) with items[{product_id,quantity}], client{full_name,email},
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

  // Issue #497: the cart is keyed by the FORM slug — the menu is a form, so
  // two menu forms in one workspace keep separate baskets.
  var KEY='jf.menu.cart.v2.'+slug;
  var cart=[];
  // t_aa6e3596: the LIVE sold-out set, keyed by product+variant. Empty means
  // "nothing extra known", so the rendered disabled attribute is the guard
  // until the server answers (see unbuyable/loadLiveStock). It is a cache of
  // the server's answer, never a second source of truth: the order core still
  // decides what can actually be bought.
  var liveStock={};
  try{ var raw=localStorage.getItem(KEY); if(raw) cart=JSON.parse(raw)||[]; }catch(e){ cart=[]; }
  if(!Array.isArray(cart)) cart=[];
  cart=cart.filter(function(it){ return it&&it.id; });
  for(var ci=0;ci<cart.length;ci++){
    cart[ci].qty=Math.max(1,parseInt(cart[ci].qty,10)||1);
    cart[ci].price=Math.max(0,parseInt(cart[ci].price,10)||0);
    // A basket saved before variant support (#665) has no key; backfill it
    // from the product id so an in-progress order is never lost on reload.
    if(!cart[ci].key) cart[ci].key=keyOf(cart[ci].id,cart[ci].vid||'');
    if(!cart[ci].vid) cart[ci].vid='';
  }
  function save(){ try{ localStorage.setItem(KEY,JSON.stringify(cart)); }catch(e){} }
  // Issue #665: a variable item is identified by product + variation, so two
  // sizes of one dish are two basket lines with their own prices (the ordinary
  // form cart keys on the variation too). A simple item keeps key = product id.
  function keyOf(id,vid){ return vid?id+'::'+vid:id; }
  function find(id,vid){ var k=keyOf(id,vid); for(var i=0;i<cart.length;i++){ if(cart[i].key===k) return cart[i]; } return null; }
  function count(){ var c=0; for(var i=0;i<cart.length;i++){ c+=cart[i].qty; } return c; }
  function totalSen(){ var t=0; for(var i=0;i<cart.length;i++){ t+=cart[i].price*cart[i].qty; } return t; }
  function rm(sen){ return 'RM '+(Math.round(sen)/100).toFixed(2); }

  // Issue #493: collect the merchant-configured extra fields from the attached
  // form. The inputs are rendered server-side from the form's field blocks
  // (the "menuField" sub-template), each named by its block id — the same key
  // the form's own submit path records and the server re-validates against the
  // same contract. The buyer's name + email are always collected separately.
  function extraFields(){
    var out={};
    var els=payForm.querySelectorAll('[name]');
    for(var i=0;i<els.length;i++){
      var el=els[i];
      var n=el.name;
      // The mandatory buyer fields, the honeypot and the anti-spam timestamp
      // are handled explicitly below — never folded into the extra fields.
      if(!n||n==='full_name'||n==='email'||n==='website'||n==='jf_ts') continue;
      if(el.type==='checkbox'){ out[n]=el.checked?(el.value||'1'):''; }
      else if(el.type==='radio'){ if(el.checked) out[n]=el.value; }
      else { out[n]=el.value; }
    }
    return out;
  }
  window.jfMenuExtraFields=extraFields;

  function addItem(id,name,price,vid,vname){
    if(!id) return;
    vid=vid||'';
    var it=find(id,vid);
    if(it){ it.qty++; } else {
      cart.push({key:keyOf(id,vid),id:id,vid:vid,vname:vname||'',name:vname?(name+' — '+vname):name,
                 price:Math.max(0,parseInt(price,10)||0),qty:1});
    }
    save(); render();
  }
  function setQty(key,delta){
    var it=findByKey(key);
    if(!it) return;
    it.qty+=delta;
    if(it.qty<1){ removeItem(key); return; }
    save(); render();
  }
  function findByKey(key){ for(var i=0;i<cart.length;i++){ if(cart[i].key===key) return cart[i]; } return null; }
  function removeItem(key){
    cart=cart.filter(function(it){ return it.key!==key; });
    save(); render();
  }
  function clearCart(){ cart=[]; save(); render(); }

  function renderBadges(){
    document.querySelectorAll('.card[data-id],.row[data-id]').forEach(function(el){
      var b=el.querySelector('.qtybadge');
      if(!b) return;
      // A variable item's badge is the sum of its variants: the card is one
      // product, so it shows how many of it are in the basket altogether.
      var n=0;
      for(var i=0;i<cart.length;i++){ if(cart[i].id===el.getAttribute('data-id')) n+=cart[i].qty; }
      if(n>0){ b.textContent=String(n); b.hidden=false; }
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
        dec.addEventListener('click',function(){ setQty(it.key,-1); });
        var q=document.createElement('span'); q.className='qty'; q.textContent=String(it.qty);
        var inc=document.createElement('button'); inc.type='button'; inc.textContent='+';
        inc.setAttribute('aria-label',attr('data-lbl-inc'));
        inc.addEventListener('click',function(){ setQty(it.key,1); });
        step.appendChild(dec); step.appendChild(q); step.appendChild(inc);
        var rmBtn=document.createElement('button'); rmBtn.type='button'; rmBtn.className='lrm';
        rmBtn.textContent=attr('data-lbl-remove');
        rmBtn.addEventListener('click',function(){ removeItem(it.key); });
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
  //
  // Issue #665: a variable item must NOT be added on the card tap alone — the
  // buyer has to choose a variant, and the chosen variant's price is what is
  // charged. So the tap on a variable card is a no-op that flags the selector
  // below it (and shows its localised hint); picking an option adds, or tops up
  // the line for that variant. A simple item behaves exactly as in #492.
  //
  // The selector is resolved from the TAPPED item's own container, never by a
  // page-wide data-id lookup: the same dish can be rendered more than once (a
  // best-seller card and its category row, or two rows in one category), and a
  // document-wide query always answers with the FIRST one — so the second
  // render's tap used to flag the first item's selector, or none at all.
  //
  // The class names this page matches on are MENU-OWNED (mvar/mvarwrap/
  // mvarstock), deliberately NOT the select.v / .varwrap / .varstock the
  // ordinary form block renderer uses. Both scripts match by class and both
  // listen on the document, so a shared name would make each script see the
  // other's selectors the moment the two surfaces share one document (an
  // embed/landing shell) — and they would genuinely collide, because the
  // menu's products ARE the attached form's product blocks, so the same
  // data-id appears on both. A distinct class keeps the two contracts
  // disjoint, whoever renders them.
  function varOf(el){
    if(!el) return null;
    var box=el.closest('.item')||el.parentNode;
    return box?box.querySelector('select.mvar'):null;
  }
  // Is the selector's currently chosen option buyable RIGHT NOW?
  //
  // The rendered "disabled" attribute is a RENDER-TIME value, and this page
  // stays open — that is how a buyer uses a menu. A dish can sell out while it
  // is being read, so the attribute is then simply wrong: the pick was accepted,
  // the line landed in the basket, and the buyer only found out at checkout
  // (409 stok habis). So the attribute is not the answer on its own; it is the
  // answer until the live state is known.
  //
  // liveStock is that state: a set keyed by product+variant for every variant
  // the server has since reported as sold out (see loadLiveStock). It is
  // refreshed when the page is read again and before each add, and it is
  // consulted IN ADDITION to the attribute — never instead of it — so a page
  // whose fetch fails, or a variant the payload does not mention, keeps exactly
  // the guard this page had before (the rendered attribute).
  //
  // This is a guard, not a reservation: the live state can itself be a moment
  // old, and the order core is still the authority — a line that sells out
  // between this check and the submit is caught by the unchanged 409, which the
  // checkout surfaces and which keeps the basket so a retry is possible.
  function unbuyable(sel){
    var o=sel.options[sel.selectedIndex];
    if(o&&o.disabled) return true;
    if(!o) return false;
    return soldOutLive(sel.getAttribute('data-id'), o.value);
  }
  // addIfBuyable is the ONE add path for a variable item, shared by the card tap
  // and the change handler so the two cannot drift (they already had to agree on
  // the sold-out refusal once — see the t_aa6e3596 guard).
  //
  // It refuses on what is known NOW (the rendered attribute plus the last
  // payload), then re-reads the live state and refuses again on the fresh
  // answer before the line is recorded. The re-read is what makes the guard
  // cover a dish that sold out while this page was open — the case the
  // render-time attribute cannot see. The read is bounded: done() always runs,
  // so a slow or failed fetch delays the add but never swallows the tap.
  function addIfBuyable(sel,host){
    if(!host||!sel||!sel.value) return;
    var o=sel.options[sel.selectedIndex];
    if(unbuyable(sel)){ flagPick(sel,attr('data-lbl-soldout')); return; }
    loadLiveStock(function(){
      if(unbuyable(sel)){ flagPick(sel,attr('data-lbl-soldout')); return; }
      addItem(host.getAttribute('data-id'),host.getAttribute('data-name'),o.getAttribute('data-price'),sel.value,o.getAttribute('data-vname'));
      clearPick(sel);
    });
  }
  // addIfSimpleBuyable is the ONE add path for a SIMPLE (non-variant) item —
  // the sibling of addIfBuyable, reached from the card/row tap when the item has
  // no selector.
  //
  // t_00b6a9cb: this shape had no guard at all. The tap added whatever the
  // served markup carried, and that markup is a RENDER-TIME value — a sold-out
  // simple dish is served the SAME button as an in-stock one (no disabled, no
  // marker), so the tap put an unbuyable line in the basket and the buyer only
  // found out at checkout (409 "stok habis"). The variable shape was guarded by
  // addIfBuyable; the simple shape was not.
  //
  // No server change is needed for it: the live-stock read already reports a
  // simple product's OWN row with variation_id:"" (menuLiveStock), and
  // soldOutLive(id,'') keys on exactly that — the same answer the variable path
  // consumes. So the simple tap asks the live state before it records a line,
  // exactly the way the variable tap does, and the rendered markup stops being
  // the only thing consulted.
  function addIfSimpleBuyable(id,host){
    if(!id||!host) return;
    if(soldOutLive(id,'')){ announceSoldOut(host); return; }
    loadLiveStock(function(){
      if(soldOutLive(id,'')){ announceSoldOut(host); return; }
      addItem(id,host.getAttribute('data-name'),host.getAttribute('data-price'));
    });
  }
  // announceSoldOut tells the buyer why a tap did nothing. A variable item can
  // flag its own selector (flagPick); a SIMPLE item has no selector, so its
  // refusal uses the page's other announcement surface — the basket sheet's
  // error line, the same element dropSoldOutLines and the checkout refusal
  // write to — with the localised sold-out label and the dish's name. The sheet
  // is opened FIRST, because openSheet clears errEl for a fresh sheet and would
  // otherwise wipe the message just written (the same order dropSoldOutLines
  // documents).
  function announceSoldOut(host){
    var name=host?host.getAttribute('data-name'):'';
    openSheet();
    errEl.textContent=attr('data-lbl-soldout')+(name?': '+name:'');
    errEl.hidden=false;
  }
  // soldOutLive answers whether the server's latest live-stock payload reports
  // this product+variant sold out. A missing entry answers false, so this can
  // only ever ADD a refusal — it can never make something buyable that the
  // rendered attribute already refuses.
  function soldOutLive(id,vid){
    if(!id) return false;
    return liveStock[keyOf(id,vid||'')]===true;
  }
  // loadLiveStock refreshes liveStock from the server. Best effort on purpose: a
  // failure leaves the previous state in place (empty on a first load), which
  // means the rendered attribute is still the fallback rather than a menu that
  // refuses everything because one fetch failed.
  //
  // done() runs EXACTLY ONCE, whichever way the request ends, so a caller that
  // defers an add on this read cannot end up never adding.
  function loadLiveStock(done){
    var finished=false;
    function finish(){ if(finished) return; finished=true; if(done) done(); }
    try{
      fetch('/menu-stock/'+encodeURIComponent(slug),{headers:{'Accept':'application/json'}})
        .then(function(r){ return r.ok?r.json():null; })
        .then(function(d){ applyLiveStock(d); finish(); })
        .catch(function(){ finish(); });
    }catch(e){ finish(); }
  }
  // applyLiveStock folds one payload into liveStock. Rows are per product+variant,
  // so a variant that has been bought back into stock drops its entry and the
  // rendered attribute (if the page was rendered sold out) is what still
  // decides — this never un-refuses a rendered-disabled option.
  //
  // The payload REPLACES the set rather than merging into it, so a variant that
  // came back into stock stops being refused on the next read.
  function applyLiveStock(d){
    var items=d&&d.items;
    if(!items||typeof items.length!=='number') return;
    var next={};
    for(var i=0;i<items.length;i++){
      var it=items[i];
      if(!it||!it.product_id) continue;
      if(it.sold_out) next[keyOf(it.product_id,it.variation_id||'')]=true;
    }
    liveStock=next;
  }
  // dropSoldOutLines re-checks the basket the page just restored from
  // localStorage against the live state. The basket outlives the page, so a line
  // added while the dish was in stock can be unbuyable by the time the page is
  // opened again — and the rendered markup for that line is not consulted by
  // anything, so without this the buyer would check out with it and only meet
  // the 409. A dropped line is ANNOUNCED rather than silently removed: the
  // buyer's basket changed without them touching it.
  function dropSoldOutLines(){
    var kept=[],dropped=[];
    for(var i=0;i<cart.length;i++){
      if(soldOutLive(cart[i].id,cart[i].vid)){ dropped.push(cart[i]); continue; }
      kept.push(cart[i]);
    }
    if(!dropped.length) return;
    cart=kept; save(); render();
    // openSheet FIRST: it clears errEl (a fresh sheet starts with no error), so
    // writing the notice before it would have it wiped. The buyer's basket
    // changed without them touching it, so the sheet is opened to say so.
    openSheet();
    errEl.textContent=attr('data-lbl-stockchanged')+(dropped[0].name?': '+dropped[0].name:'');
    errEl.hidden=false;
  }
  function flagPick(sel,msg){
    if(!sel) return;
    sel.classList.add('missing');
    // Resolve the hint through the selector's own CONTAINER, never through
    // parentNode alone: the template renders .mvarstock as a sibling inside
    // .mvarwrap today, so parentNode happens to work — but anything inserted
    // between the two (or a wrapper that loses that shape) makes a parentNode
    // lookup silently miss the hint, the same "reach out of your own
    // container" bug the tap and change paths already had.
    var wrap=sel.closest('.mvarwrap')||sel.parentNode;
    var hint=wrap?wrap.querySelector('.mvarstock'):null;
    if(hint){
      hint.textContent=msg||hint.getAttribute('data-hint')||'';
      hint.hidden=false;
    }
    try{ sel.focus(); }catch(e){}
  }
  function clearPick(sel){
    if(!sel) return;
    sel.classList.remove('missing');
    var wrap=sel.closest('.mvarwrap')||sel.parentNode;
    var hint=wrap?wrap.querySelector('.mvarstock'):null;
    if(hint){
      hint.hidden=true;
      // Restore the rendered prompt, so the hint that shows on a LATER
      // no-choice tap is the "choose a variation" text rather than whatever a
      // previous refusal left in it. The text is the server's (data-hint), so
      // it stays localised.
      hint.textContent=hint.getAttribute('data-hint')||'';
    }
  }
  document.addEventListener('click',function(e){
    var el=e.target&&e.target.closest?e.target.closest('.card[data-id],.row[data-id]'):null;
    if(!el) return;
    var id=el.getAttribute('data-id');
    var sel=varOf(el);
    if(sel){
      // Variable item: the card tap cannot decide a variant. If the buyer has
      // already picked one, top up that line; otherwise prompt for the choice.
      if(!sel.value){ flagPick(sel); return; }
      addIfBuyable(sel,el);
      return;
    }
    // Simple item: no variant to choose, so the tap adds it — but through the
    // same live-stock guard the variable path uses (t_00b6a9cb). A sold-out
    // simple dish is served the SAME button as an in-stock one, so the markup
    // cannot be the guard here; the tap asks the server instead.
    addIfSimpleBuyable(id,el);
  });
  // Picking a variant adds it immediately (matching the tap-to-add feel of a
  // simple item) and clears any pending prompt.
  //
  // A .mvarwrap resolves its OWN host. The .item wrapper is what each item is
  // rendered in, and previousElementSibling is that same button for a menu
  // rendered by an older build (or any HTML that lost the wrapper), so neither
  // path can reach out and return a sibling item's product.
  //
  // The handler matches the MENU's own selector class (mvar), never the
  // form renderer's bare v class: the two surfaces can share one document
  // (embed / landing shell), and matching a shared class would let the form's
  // selector drive the menu cart and vice versa.
  document.addEventListener('change',function(e){
    var t=e.target;
    if(!t||!t.classList||!t.classList.contains('mvar')) return;
    var box=t.closest('.item');
    var host=box?box.querySelector('.card[data-id],.row[data-id]'):t.previousElementSibling;
    if(!t.value){ if(host) flagPick(t); return; }
    // The disabled attribute is the guard where this page is not scripted, but
    // the page IS scripted and stays open: a variant can sell out after the
    // page rendered, and the attribute is then stale. The live re-read in
    // addIfBuyable is what refuses that pick; the previous line was only the
    // rendered state. Until it answers, the attribute still refuses.
    addIfBuyable(t,host);
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
    // Issue #493: the form is novalidate, so enforce the attached form's
    // required extra fields client-side too. The server re-validates the same
    // contract regardless, so this is only a friendlier first line.
    var missing=payForm.querySelector('[name][required]');
    if(missing){
      var reqs=payForm.querySelectorAll('[name][required]');
      for(var ri=0;ri<reqs.length;ri++){
        var re=reqs[ri], rn=re.name;
        if(rn==='full_name'||rn==='email') continue;
        var rv=re.type==='checkbox'?(re.checked?re.value:''):re.value;
        // t_7c931910: a radio's own .value belongs to the FIRST radio in
        // document order, not the checked one, so an unanswered required group
        // always looked answered and only the server's 400 stopped the order.
        // Read the GROUP: one checked radio of the same name, and its value.
        if(re.type==='radio'){
          rv='';
          var group=payForm.querySelectorAll('input[type=radio]');
          for(var gi=0;gi<group.length;gi++){
            if(group[gi].name===rn&&group[gi].checked){ rv=group[gi].value; break; }
          }
        }
        if(!String(rv||'').trim()){ errEl.textContent=attr('data-lbl-required'); errEl.hidden=false; return; }
      }
    }
    var fields=Object.assign({
      website:(payForm.querySelector('[name=website]')||{}).value||'',
      jf_ts:String(ts)
    },extraFields());
    var body={
      client:{full_name:name,email:email},
      items:cart.map(function(it){ return {product_id:it.id,variation_id:it.vid||'',quantity:it.qty}; }),
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
    var send=function(){
      fetch('/'+encodeURIComponent(slug)+'/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
        .then(function(r){ return r.json().then(function(d){ return {ok:r.ok,d:d||{}}; }); })
        .then(function(res){
          var d=res.d;
          if(d.checkout_url||d.next){
            try{ localStorage.removeItem(KEY); }catch(e){}
            location.href=d.checkout_url||d.next;
            return;
          }
          // Prefer the server's sentence in the buyer's language; fall back
          // to the English wire prose for an older api.
          fail(d.message||d.error);
        })
        .catch(function(){ fail(''); });
    };
    // t_aa6e3596: the last chance to catch a line that went unbuyable while the
    // basket sat there — re-read the live state and refuse to POST a line the
    // server will answer 409 for, naming the dish instead. The re-read cannot
    // fail the checkout: if it does not answer, the submit goes ahead and the
    // server's own refusal (surfaced, basket retained) is the guard, exactly as
    // before.
    loadLiveStock(function(){
      var bad=null;
      for(var i=0;i<cart.length;i++){
        if(soldOutLive(cart[i].id,cart[i].vid)){ bad=cart[i]; break; }
      }
      if(bad){
        openSheet();
        fail(attr('data-lbl-stockchanged')+(bad.name?': '+bad.name:''));
        render();
        return;
      }
      setTimeout(send,wait);
    });
  });

  render();

  // t_aa6e3596: the basket outlives the page, and so does the render-time
  // stock attribute the page was built with. On open, ask the server what is
  // buyable NOW; a line the buyer added while the dish was in stock is then
  // dropped and announced (dropSoldOutLines) instead of being discovered at
  // checkout as a 409. A failed read leaves the basket exactly as it was.
  loadLiveStock(dropSoldOutLines);
})();
</script>
</body>
</html>`))

// formMenuItems derives the menu's product list from a form's OWN product
// blocks (issue #497). Each product block references one product via its
// product_id and carries the merchant's per-item display label in value. The
// returned order follows the block order, so the merchant controls the menu
// order in the form builder; duplicates collapse to the first reference.
//
// A block with no product_id is skipped. Nothing here reads the account's
// product list, so an unrelated account product can never leak into the menu.
func formMenuItems(config map[string]any) []formMenuItem {
	if config == nil {
		return nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	var cfg struct {
		Blocks []struct {
			Type      string `json:"type"`
			ProductID string `json:"product_id"`
			Value     string `json:"value"`
		} `json:"blocks"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []formMenuItem
	for _, b := range cfg.Blocks {
		if b.Type != "product" {
			continue
		}
		pid := strings.TrimSpace(b.ProductID)
		if pid == "" || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, formMenuItem{ProductID: pid, Label: strings.TrimSpace(b.Value)})
	}
	return out
}

// renderFormMenu writes the per-form restaurant-menu page (issue #497). It is
// called from serveForm when the form's config carries an enabled menu. The
// menu's products come from the form's own product blocks — never from the
// account's product list — and each item resolves LIVE from the products table
// (#461) with the form's own snapshot as the archived/deleted fallback. The
// table number from ?table=N and the affiliate code from ?aff=CODE are
// shown/echoed, and every user-facing string is localised to the workspace's
// default language.
func (h *PublicHandlers) renderFormMenu(w http.ResponseWriter, r *http.Request, f *dbstore.Form, menu *dbstore.MenuPage) {
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

	// The workspace's default language + contact details style the page and
	// localise every system string. Nil-safe: a form with no linked workspace
	// (legacy) falls back to English and no contact panel.
	ls := langStringsForCode("")
	st := h.workspaceSettingsForForm(r, f)
	if st != nil {
		ls = langStringsForCode(st.WorkspaceLang())
	}

	// Issue #461: resolve every referenced product LIVE from the products
	// table, with the form's own snapshot as the fallback for an archived or
	// deleted product. A block whose product resolves to nothing (no live row,
	// no snapshot) is skipped — the menu never invents an item.
	live := productMapByID(h.liveProductsForForm(r.Context(), f))
	snapByID := map[string]productSnapshot{}
	for _, s := range configProductSnapshots(f.Config) {
		if s.ID != "" {
			snapByID[s.ID] = s
		}
	}

	// Issue #497: the menu's product set is the form's own product blocks. A
	// block's label overrides the product name only when the merchant typed
	// one; otherwise the live product name wins (#461).
	items := formMenuItems(f.Config)
	view := func(pid string, bestSeller bool) (menuProductView, bool) {
		name, desc, price, img, ok := "", "", int64(0), "", false
		if p, isLive := live[pid]; isLive {
			name, desc, price, img, ok = p.Name, p.Description, p.PriceSen, firstImage(p.Images), true
		} else if s, isSnap := snapByID[pid]; isSnap {
			name, desc, price, img, ok = s.Name, s.Description, s.PriceSen, firstImage(s.Images), true
		}
		if !ok {
			return menuProductView{}, false
		}
		v := h.menuProductView(pid, name, desc, price, img, bestSeller, ls.MenuBestBadge, ls.MenuAdd)
		if label := itemLabel(items, pid); label != "" {
			v.Name = label
		}
		// Issue #665: attach the product's LIVE variations so a variable item
		// offers the same choice the ordinary form does, and the chosen
		// variant's price/stock is what the order core applies. Resolved here
		// (not in the template) because it needs the DB.
		v = h.withVariations(r, v, ls)
		return v, true
	}

	// The set of product IDs this menu may render — the form's own references.
	// A category or best-seller entry for an unreferenced product is dropped,
	// so a stale ID can never pull an unrelated product into the menu.
	referenced := map[string]bool{}
	for _, it := range items {
		referenced[it.ProductID] = true
	}

	// Group the referenced products into the configured categories, in
	// category order. Only IDs the form references are considered.
	placed := map[string]bool{}
	var cats []menuCategoryView
	for _, c := range menu.Categories {
		cv := menuCategoryView{Name: c.Name}
		for _, pid := range c.ProductIDs {
			if !referenced[pid] {
				continue
			}
			v, ok := view(pid, menu.IsBestSeller(pid))
			if !ok {
				continue
			}
			cv.Products = append(cv.Products, v)
			placed[pid] = true
		}
		if len(cv.Products) > 0 {
			cats = append(cats, cv)
		}
	}
	// A referenced product that no category lists still belongs on the menu:
	// fall back to a single section so nothing the form references disappears.
	// (There is no account-wide "All" section — #497.)
	var unplaced []menuProductView
	for _, it := range items {
		if placed[it.ProductID] {
			continue
		}
		if v, ok := view(it.ProductID, menu.IsBestSeller(it.ProductID)); ok {
			unplaced = append(unplaced, v)
		}
	}
	if len(unplaced) > 0 {
		cats = append(cats, menuCategoryView{Name: ls.MenuAll, Products: unplaced})
	}
	// Best sellers: the top section, rendered as 2-column square-image cards.
	// BestSellerIDs (issue #485) wins; the legacy single BestSeller id is the
	// fallback. Only referenced products qualify.
	var best []menuProductView
	for _, pid := range menu.BestSellerProductIDs() {
		if !referenced[pid] {
			continue
		}
		if v, ok := view(pid, true); ok {
			best = append(best, v)
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
		}
	}

	data := workspaceMenuData{
		Slug:        f.Slug,
		Menu:        menu,
		Th:          h.formMenuTheme(f, st),
		Ls:          ls,
		Table:       table,
		Aff:         aff,
		RenderTS:    antiSpamRenderTime(),
		BestSellers: best,
		Categories:  cats,
		HasItems:    len(best) > 0 || len(cats) > 0,
		Query:       query,
		Cat:         cat,
	}
	if st != nil {
		data.ContactAddress = st.ContactAddress
		data.ContactPhone = st.ContactPhone
	}
	// Issue #493: the merchant may attach a published form to the menu whose
	// field blocks define the EXTRA fields the checkout collects. The same
	// form is what the order endpoint validates against, so the rendered
	// fields and the server contract can never drift.
	if extra := h.formOrderExtraForm(r, f, menu.OrderFormSlug); extra != nil {
		contract := menuExtraContract(extra.Config)
		data.ExtraFields = menuExtraFields(contract, ls)
		data.ExtraFormSlug = extra.Slug
		data.ExtraFormID = extra.ID
		// Link into the form builder so the merchant can edit the fields —
		// the attached form's fields ARE the checkout's extra fields.
		data.ExtraFormEdit = "/builder?form=" + extra.ID
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := menuPageTmpl.Execute(w, data); err != nil {
		plainErr(w, http.StatusInternalServerError, ls.Lang, buyertext.CodePageError)
	}
}

// menuStockItem is one buyability row of the live-stock payload (see
// handleMenuStock). VariationID is "" for a simple product's own row; a VARIABLE
// product reports one row per variation and NO product row, because the order
// core charges and decrements the VARIATION — the product's own stock is not
// what decides whether that line can be bought (order.go).
type menuStockItem struct {
	ProductID   string `json:"product_id"`
	VariationID string `json:"variation_id"`
	SoldOut     bool   `json:"sold_out"`
}

// handleMenuStock answers the LIVE buyability of the products a menu form sells,
// so the menu page can answer "can the buyer buy this NOW?" instead of trusting
// an attribute the server wrote when the page was rendered.
//
// WHY THIS EXISTS (t_aa6e3596): the served menu markup flags a sold-out
// variation with the `disabled` attribute, and `disabled` is a RENDER-TIME
// value. The menu page stays open — that is how a buyer uses it — so a dish can
// sell out while it is being read; the attribute is then simply wrong, the pick
// was accepted, the line landed in the basket, and the buyer only found out at
// checkout (409 "stok habis"). A page that is already open has no way to learn
// the current state except by asking, which is what this endpoint is for.
//
// The order core stays the authority for what is actually charged and for
// whether the stock can be taken: this read is the buyer's guard, not a
// reservation, so a variant can still sell out between this read and the submit
// and the unchanged 409 path is what catches that. Nothing here is cached
// (no-store), because a cached answer is the stale-attribute problem again.
//
// Only the form's OWN product blocks are reported (formMenuItems), resolved the
// same way the page resolves them (#461: live row, snapshot as the archived
// fallback), so this payload and the rendered menu cannot describe two
// different sets of products.
//
// Route: GET /menu-stock/{slug} (the menu form's own public URL's slug).
func (h *PublicHandlers) handleMenuStock(w http.ResponseWriter, r *http.Request) {
	f := h.resolveMenuStockForm(w, r)
	if f == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": h.menuLiveStock(r, f)})
}

// resolveMenuStockForm resolves the menu form a live-stock read is about, using
// the SAME lookup the menu page's checkout uses (POST /{slug}/order, see
// handleSubmit): the request Host names the workspace (its subdomain, or a
// verified custom domain) and the path names the form. Resolving it any other
// way would let the read and the checkout describe different forms.
//
// It writes the refusal itself and returns nil, so a caller cannot forget to
// stop. A form with no enabled menu is refused rather than answered with an
// empty list: this endpoint reports what a MENU sells, and it must not become a
// way to read the stock of a form that is not a menu.
func (h *PublicHandlers) resolveMenuStockForm(w http.ResponseWriter, r *http.Request) *dbstore.Form {
	if h.Forms == nil {
		buyerErr(w, h.buyerLangForHost(r.Context(), r), http.StatusNotFound, errFormNotFound)
		return nil
	}
	slug := r.PathValue("slug")
	var f *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		f, err = h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		f, err = nil, dbstore.ErrFormNotFound
	}
	if err != nil || f == nil {
		buyerErr(w, h.buyerLangForHost(r.Context(), r), http.StatusNotFound, errFormNotFound)
		return nil
	}
	// The form resolved, so every refusal below speaks its language.
	lang := h.buyerLang(r.Context(), f, f.AccountID)
	// A draft form is never publicly served (serveForm), so its stock is not
	// public either. A private form's stock is refused unless THIS request is
	// unlocked, so the read can never become a side door around the password
	// gate the page itself enforces.
	if f.Status == dbstore.FormStatusDraft {
		buyerErr(w, lang, http.StatusNotFound, errFormNotFound)
		return nil
	}
	if f.Status == dbstore.FormStatusPrivate && !h.privateUnlocked(r, f) {
		buyerErr(w, lang, http.StatusNotFound, errFormNotFound)
		return nil
	}
	if menu := dbstore.MenuFromConfig(f.Config); menu == nil || !menu.Enabled {
		buyerErr(w, lang, http.StatusNotFound, errOrderMenuNotFound)
		return nil
	}
	return f
}

// menuLiveStock reports the current buyability of every product the form's menu
// references. A product with variations reports one row per variation (the rows
// the order core would decrement); a product without variations reports its own
// row. A referenced product with no live row (archived/deleted, which the page
// renders from its snapshot) reports NOTHING: the page cannot know which stock
// that snapshot had, and inventing "sold out" would refuse a sale the server
// would accept — so the page keeps its rendered attribute for it instead.
//
// The sold-out rule is the renderer's own (soldOutStock, used by variationViews
// and the page): a limited stock that has reached zero. An unlimited (NULL)
// stock is never sold out.
func (h *PublicHandlers) menuLiveStock(r *http.Request, f *dbstore.Form) []menuStockItem {
	items := formMenuItems(f.Config)
	out := make([]menuStockItem, 0, len(items))
	live := productMapByID(h.liveProductsForForm(r.Context(), f))
	for _, it := range items {
		p, isLive := live[it.ProductID]
		if !isLive {
			continue
		}
		if h.ProductVariations != nil {
			// A lookup error must not be reported as "sold out": the page then
			// keeps its rendered attribute, which is the state before this
			// endpoint existed.
			if vs, err := h.ProductVariations.ListByProduct(r.Context(), p.ID); err == nil && len(vs) > 0 {
				for _, v := range variationViews(vs) {
					out = append(out, menuStockItem{ProductID: p.ID, VariationID: v.ID, SoldOut: v.SoldOut})
				}
				continue
			}
		}
		out = append(out, menuStockItem{ProductID: p.ID, SoldOut: soldOutStock(p.Stock)})
	}
	return out
}

// formMenuItem is one product reference from a form's product blocks (issue
// #497): the product id plus the merchant's optional per-item display label.
type formMenuItem struct {
	ProductID string
	Label     string
}

// itemLabel returns the merchant's display label for a referenced product (the
// block's value), or "" when the merchant typed none (the live product name is
// used instead).
func itemLabel(items []formMenuItem, productID string) string {
	for _, it := range items {
		if it.ProductID == productID {
			return it.Label
		}
	}
	return ""
}

// workspaceSettingsForForm reads the storefront settings of the workspace that
// owns the form's account (issue #497). Nil-safe: returns nil when the form
// has no linked workspace or the store is not wired, so the caller degrades to
// the English defaults / no contact panel.
func (h *PublicHandlers) workspaceSettingsForForm(r *http.Request, f *dbstore.Form) *dbstore.WorkspaceSettings {
	if h.Workspaces == nil || h.Accounts == nil || f == nil || f.AccountID == "" {
		return nil
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(r.Context(), f.AccountID)
	if err != nil || workspaceID == "" {
		return nil
	}
	st, err := h.Workspaces.Settings(r.Context(), workspaceID)
	if err != nil {
		return nil
	}
	return st
}

// formMenuTheme resolves the per-form menu styling (issue #497): the form's own
// theme map (set_form_theme / Form Settings → General) supplies the hero,
// colors and logo, with the workspace's contact address as the location line.
// Every value is optional; the template falls back to sensible defaults.
func (h *PublicHandlers) formMenuTheme(f *dbstore.Form, st *dbstore.WorkspaceSettings) menuTheme {
	th := menuTheme{}
	if f != nil {
		th.Hero = f.Title
		if v, ok := f.Theme["primary"].(string); ok {
			th.PrimaryColor = strings.TrimSpace(v)
		}
		if v, ok := f.Theme["background"].(string); ok {
			th.BackgroundColor = strings.TrimSpace(v)
		}
		if v, ok := f.Theme["logo_url"].(string); ok {
			th.LogoURL = strings.TrimSpace(v)
		}
	}
	if st != nil {
		th.Subtitle = strings.TrimSpace(st.ContactAddress)
	}
	return th
}

// formOrderExtraForm returns the published form the merchant attached to the
// menu for extra order info (issue #491/#493), or nil when none is attached (or
// the configured slug no longer resolves to a published form in the same
// account). The setting lives on the per-form menu config
// (MenuPage.OrderFormSlug) — a JSON blob on forms.config, so no migration is
// needed (issue #497 moved it from the workspace landing page).
//
// The reference is a SLUG, not an id, so renaming the attached order form would
// silently detach it: the menu would stop collecting its extra fields, and the
// order endpoint — which validates against this same contract — would stop
// accepting them. That is the "a rename must not break references to it" half of
// the card. So a miss on the live lookup falls through to the rename's temporary
// 301 (the same fallback every public slug lookup uses), and the target must
// still be published — a redirect can never resurrect a form that was since
// pulled back to draft.
func (h *PublicHandlers) formOrderExtraForm(r *http.Request, f *dbstore.Form, slug string) *dbstore.Form {
	if h.Forms == nil || f == nil {
		return nil
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	acc, err := h.Accounts.ByID(r.Context(), f.AccountID)
	if err != nil || acc == nil {
		return nil
	}
	extra, ferr := h.Forms.PublishedByAccountSlug(r.Context(), acc.Slug, slug)
	if ferr == nil && extra != nil {
		return extra
	}
	// The configured slug is not live: the order form may have been renamed.
	// Resolve it through the temporary 301 and require the target to be
	// published, which is the rule PublishedByAccountSlug enforces.
	redirected, rerr := h.Forms.RedirectedFormByAccountSlug(r.Context(), acc.Slug, slug)
	if rerr != nil || redirected == nil || redirected.Status != dbstore.FormStatusPublished {
		return nil
	}
	return redirected
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

// menuProductView builds one product card / list row for the menu page. It
// takes already-resolved name/price/image/description values (issue #497: the
// menu resolves each referenced product LIVE from the products table, #461,
// with the form snapshot as the archived fallback) so the caller owns
// resolution and this stays a pure view builder.
func (h *PublicHandlers) menuProductView(productID, name, description string, priceSen int64, image string, bestSeller bool, bestLabel, addLabel string) menuProductView {
	// Issue #382/#471: resolve the media variant that fits each tier — the
	// square card wants the medium variant, the row thumbnail the small one.
	// A non-canonical URL (external, GIF/SVG) is returned unchanged.
	cardImg := productImageForSize(image, ProductImageSizeMedium)
	thumbImg := productImageForSize(image, ProductImageSizeSmall)
	return menuProductView{
		ID:          productID,
		Name:        name,
		Description: description,
		Price:       formatPriceSen(priceSen),
		PriceSen:    priceSen,
		ImageURL:    image,
		CardImage:   cardImg,
		ThumbImage:  thumbImg,
		BestSeller:  bestSeller,
		BestLabel:   bestLabel,
		AddLabel:    addLabel,
	}
}

// withVariations returns v with its live variation list attached (issue #665).
// The menu resolves a product from a thinner view than the form does, and the
// difference was invisible until a merchant sold a variant: every menu order
// fell through to the simple-product branch, so the buyer was charged the base
// price and the variant's own stock was never decremented. The list resolved
// here is exactly what the form block renderer reads, so both surfaces offer
// the same choice. Prices are taken from the variation (the order core
// re-reads and re-validates them server-side regardless).
func (h *PublicHandlers) withVariations(r *http.Request, v menuProductView, ls langStrings) menuProductView {
	v.SelectLabel = ls.SelectVariation
	v.InStockLabel = ls.InStock
	v.PickLabel = ls.SelectVariationAlert
	v.SoldOutLabel = ls.MenuSoldOut
	if h.ProductVariations == nil || v.ID == "" {
		return v
	}
	vs, err := h.ProductVariations.ListByProduct(r.Context(), v.ID)
	if err != nil {
		return v
	}
	v.Variations = variationViews(vs)
	v.Variable = len(v.Variations) > 0
	return v
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
