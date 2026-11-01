// Issue #438: QR ordering flow — QR code generation for a form's public URL.
//
// A merchant can generate a QR that points to a form/menu (e.g. the F&B
// template) so a customer scans it at the table and lands on the order form.
// The QR optionally encodes a ?table=N query param so the order carries the
// table number and the merchant knows where to serve.
//
// The endpoint is GET /qr/{slug}.png on the workspace subdomain / custom
// domain (mirroring /og/{slug}.png). It resolves the form the same way the
// renderer does, builds the canonical public URL, and renders a PNG QR code
// server-side with the skip2/go-qrcode library — no headless browser, no
// client dependency. A ?table=N query param on the request is folded into the
// encoded URL so a merchant can generate a per-table QR.
package httpapi

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/wzul/jomform/api/internal/cdnpurge"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// qrPublicAccess is the status gate the two public QR routes apply, and it is
// the SAME rule the form's own page applies (serveForm), because both routes
// publish something about the form that its page would not:
//
//   - the printable stand carries the form's TITLE (its identity);
//   - the PNG carries the form's public URL (its existence).
//
// findPublicForm is deliberately status-agnostic (issue #176): a rename's 301
// fallback and the merchant's edit paths must still resolve a draft, so the gate
// belongs to the CALLER — and these callers had none. Measured 2026-10-26 on the
// built binary: a draft form's page answered 404 while /qr/{slug}/stand answered
// 200 with its title on it, and /qr/{slug}.png answered 200 as well.
//
//   - draft: 404 with the SAME body an unknown slug gets from these handlers, so
//     neither route is an existence oracle for a slug.
//   - private: served only to a caller holding THIS form's unlock cookie
//     (privateUnlocked compares the token's form id), i.e. the browser that has
//     already passed the password gate. Everyone else gets the same 404: neither
//     an image route nor a printable artefact has a gate page to render.
//   - unlisted: served. An unlisted form is LIVE at its direct URL and is only
//     left out of the workspace listing, so it is on the serving side of the
//     gate (this is why the rule is not "published only").
//   - published: served.
//
// A private arm that IS served must never be handed to a shared cache: the
// caller reports that through the returned private flag (the stand writes
// `private, no-store`, the PNG replaces its 24h public TTL).
//
// The merchant's own print flow does NOT come through here for a draft: the
// dashboard's "Print stand" action fetches the authed REST route instead (see
// RESTHandlers.getFormQRStand), because a draft's stand must stay printable for
// the merchant while being unreachable for everyone else.
func (h *PublicHandlers) qrPublicAccess(w http.ResponseWriter, r *http.Request, f *dbstore.Form) (allowed, private bool) {
	switch f.Status {
	case dbstore.FormStatusDraft:
		http.Error(w, "not found", http.StatusNotFound)
		return false, false
	case dbstore.FormStatusPrivate:
		if !h.privateUnlocked(r, f) {
			http.Error(w, "not found", http.StatusNotFound)
			return false, false
		}
		return true, true
	}
	return true, false
}

// handleQRImage serves a PNG QR code pointing at a form's public URL.
// Route: GET /qr/{slug}.png (registered before /{slug} so it wins over a
// form slug). The optional ?table=N query param is encoded into the URL so
// the scanned QR opens the form with the table number pre-filled.
func (h *PublicHandlers) handleQRImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	// Live-first resolution with the rename fallback: a QR stand printed before
	// a rename keeps generating a code that resolves (see findPublicForm).
	f := h.findPublicForm(r.Context(), r, slug)
	if f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// STATUS GATE (see qrPublicAccess): the PNG discloses whether a form exists
	// at this slug, so a draft 404s here exactly as its page does. The merchant
	// does not need it — the dashboard draws its QR client-side and prints the
	// stand through the authed route.
	allowed, private := h.qrPublicAccess(w, r, f)
	if !allowed {
		return
	}
	// Build the canonical public URL for this form on the current host.
	target := qrStandTarget(h.formPublicURL(r, f), strings.TrimSpace(r.URL.Query().Get("table")))
	png, err := qrcode.Encode(target, qrcode.Medium, 512)
	if err != nil {
		slog.Error("qr_render", "err", err, "slug", slug)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// CACHING RULE: a cacheable response must be a function of its cache key,
	// and the key here is the REQUEST PATH while the image encodes the form's
	// CURRENT slug (formPublicURL reads f.Slug). The two agree only when the
	// requested slug IS the form's live slug. Anything else arrived through a
	// rename's temporary 301 fallback (findPublicForm), and those bytes are NOT
	// a function of the path: a CDN holding them under the old slug serves a
	// code the form has already left, for the whole TTL, after every later
	// rename — measured on a workspace behind Cloudflare, where a cached
	// /qr/{old}.png kept encoding a slug the form had moved off twice.
	// So cache the live-slug arm and never the fallback arm. Same test the
	// serving path uses to spot a fallback resolution: f.Slug != slug
	// (handleWorkspaceRootForm). The fallback arm still renders the canonical
	// URL — a stand reprinted, or an embedded PNG re-fetched, after a rename
	// points at the address the form actually serves.
	//
	// A PRIVATE form's code is a third arm that must not be cached publicly
	// either: it was released only because this caller holds the form's unlock
	// cookie, and a CDN copy would serve it to the next caller, who never
	// entered the password (same rule as the unlocked og:image).
	//
	// THE PUBLIC ARM'S LIFETIME is bounded, not a day (issue measured on prod
	// 2026-10-31: a draft form's PNG kept answering `cf-cache-status: HIT` for
	// 26 minutes after the origin started 404ing, and would have for a day
	// under the old 86400). It is the freshness window cdnpurge names beside the
	// purge — the same one the share card declares — because one purge drops
	// both artefacts and the reply reports ONE residual window
	// (`edge_max_age_s`); a longer value here would make that report a lie. What
	// closes the window outright is the purge on the status change
	// (httpapi purgeLeftPublicArtefacts); this bound is what remains when no CDN
	// credential is configured, which is prod's state today.
	cacheControl := fmt.Sprintf("public, max-age=%d", cdnpurge.QRImageMaxAgeSeconds)
	if private {
		cacheControl = "private, no-store"
	} else if f.Slug != slug {
		cacheControl = "no-store"
	}
	w.Header().Set("Cache-Control", cacheControl)
	w.Write(png)
}

// qrStandData is the template payload for the printable QR stand (issue #438).
type qrStandData struct {
	Title string
	Table string
	// QRData is the base64 PNG data URL emitted into the <img src> attribute.
	//
	// It is template.URL, NOT string, on purpose (issue #499): html/template
	// auto-escapes by CONTEXT, the src attribute is a URL context, and a
	// `data:` URL is an unsafe scheme — a plain string is silently replaced
	// with the sentinel `#ZgotmplZ`, so the printed stand showed a broken
	// image (HTTP 200, no error anywhere). template.URL is the documented way
	// to hand the engine a URL the caller has already validated.
	//
	// The value is ALWAYS produced by qrPNGDataURL from a PNG this server
	// generated with qrcode.Encode — it is never built from caller input (the
	// ?table=N query param only influences the URL ENCODED INSIDE the QR, and
	// goes through url.Query().Set, never into this field). Trusting it is
	// therefore safe; the trusted wrapper is confined to qrPNGDataURL so an
	// arbitrary string cannot be assigned here by accident.
	QRData   template.URL
	Target   string
	HasTable bool
}

// qrPNGDataURL wraps a server-generated PNG in a base64 data URL. It is the
// only constructor for qrStandData.QRData, which keeps the trusted
// template.URL value provably server-generated (issue #499): the PNG bytes
// come from qrcode.Encode, and base64 output can never contain a quote or a
// `<`, so the trusted URL cannot break out of the attribute it is emitted in.
func qrPNGDataURL(png []byte) template.URL {
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
}

// qrStandTmpl renders a printable QR stand (Beep-style) the merchant can put
// on a table. It embeds the QR as a data URL so the page is fully
// self-contained and prints cleanly on A4.
var qrStandTmpl = template.Must(template.New("qr-stand").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} — QR stand</title>
<style>
  * { box-sizing: border-box; }
  body { margin:0; font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:#f7f8f8; color:#17181c; display:flex; align-items:center;
         justify-content:center; min-height:100vh; padding:24px; }
  .stand { background:#fff; border:1px solid #e6e8ec; border-radius:24px;
           max-width:420px; width:100%; padding:32px; text-align:center;
           box-shadow:0 8px 30px rgba(23,24,28,0.08); }
  .table { display:inline-block; background:#4f46e5; color:#fff; font-weight:700;
           font-size:14px; padding:6px 18px; border-radius:999px; margin-bottom:16px;
           text-transform:uppercase; letter-spacing:.05em; }
  .qr { width:260px; height:260px; margin:0 auto; }
  .qr img { width:100%; height:100%; object-fit:contain; }
  h1 { margin:16px 0 6px; font-size:22px; }
  .sub { margin:0 0 20px; color:#6b7280; font-size:14px; line-height:1.5; }
  .steps { text-align:left; margin:20px 0 0; padding:0; list-style:none; }
  .steps li { display:flex; gap:10px; align-items:flex-start; font-size:13px;
              color:#374151; padding:6px 0; }
  .steps .n { background:#eef2ff; color:#4f46e5; font-weight:700; width:22px; height:22px;
              border-radius:50%; display:flex; align-items:center; justify-content:center;
              font-size:12px; flex-shrink:0; }
  @media print {
    body { background:#fff; padding:0; }
    .stand { box-shadow:none; border:none; }
  }
</style>
</head>
<body>
  <div class="stand">
    {{if .HasTable}}<div class="table">Table {{.Table}}</div>{{end}}
    <div class="qr"><img src="{{.QRData}}" alt="QR code"></div>
    <h1>{{.Title}}</h1>
    <p class="sub">Scan to view the menu &amp; order online</p>
    <ol class="steps">
      <li><span class="n">1</span>Scan the QR code with your phone camera</li>
      <li><span class="n">2</span>Pick your items from the menu</li>
      <li><span class="n">3</span>Pay online &amp; we'll serve you at your table</li>
    </ol>
  </div>
</body>
</html>`))

// qrStandTarget folds an optional table number into a form's public URL, so the
// QR encodes ?table=N with the value percent-encoded rather than spliced in.
func qrStandTarget(target, table string) string {
	if table == "" {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := u.Query()
	q.Set("table", table)
	u.RawQuery = q.Encode()
	return u.String()
}

// writeQRStand renders the printable stand (issue #438) for one form at the
// given public URL, with the optional table number folded into the QR.
//
// It is the ONE builder behind both serving paths: the public
// /qr/{slug}/stand route (status-gated, see qrPublicAccess) and the merchant's
// authed /v1/rest/forms/{id}/qr-stand route (RESTHandlers.getFormQRStand), which
// is what the dashboard's "Print stand" action opens. Two builders would drift
// into two different printed artefacts.
func writeQRStand(w http.ResponseWriter, title, table, target string) {
	target = qrStandTarget(target, table)
	png, err := qrcode.Encode(target, qrcode.Medium, 512)
	if err != nil {
		slog.Error("qr_stand_render", "err", err)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	data := qrStandData{
		Title:    title,
		Table:    table,
		QRData:   qrPNGDataURL(png),
		Target:   target,
		HasTable: table != "",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := qrStandTmpl.Execute(w, data); err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
	}
}

// handleQRStand renders a printable QR stand (issue #438). Route:
// GET /qr/{slug}/stand (registered before /{slug} so it wins over a form
// slug). The optional ?table=N query param is encoded into the QR and shown
// on the stand. The page is self-contained (QR embedded as a data URL) and
// prints cleanly on A4.
func (h *PublicHandlers) handleQRStand(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	// Live-first resolution with the rename fallback (see findPublicForm). The
	// stand is regenerated from the form's LIVE slug, so printing it again after
	// a rename produces a QR that points at the current URL.
	f := h.findPublicForm(r.Context(), r, slug)
	if f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// STATUS GATE (see qrPublicAccess): the stand prints the form's TITLE, so a
	// draft must answer 404 here just as its page does, and a private form must
	// answer only to a caller that has already unlocked it.
	allowed, private := h.qrPublicAccess(w, r, f)
	if !allowed {
		return
	}
	if private {
		// Served only to this buyer's own browser (it holds the unlock cookie):
		// a shared cache must never hand the stand to the next caller.
		w.Header().Set("Cache-Control", "private, no-store")
	}
	writeQRStand(w, f.Title, strings.TrimSpace(r.URL.Query().Get("table")), h.formPublicURL(r, f))
}
