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
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// handleQRImage serves a PNG QR code pointing at a form's public URL.
// Route: GET /qr/{slug}.png (registered before /{slug} so it wins over a
// form slug). The optional ?table=N query param is encoded into the URL so
// the scanned QR opens the form with the table number pre-filled.
func (h *PublicHandlers) handleQRImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSuffix(r.PathValue("slug"), ".png")
	var f *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		f, err = h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		err = dbstore.ErrFormNotFound
	}
	if err != nil || f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Build the canonical public URL for this form on the current host.
	target := h.formPublicURL(r, f)
	if table := strings.TrimSpace(r.URL.Query().Get("table")); table != "" {
		u, perr := url.Parse(target)
		if perr == nil {
			q := u.Query()
			q.Set("table", table)
			u.RawQuery = q.Encode()
			target = u.String()
		}
	}
	png, err := qrcode.Encode(target, qrcode.Medium, 512)
	if err != nil {
		slog.Error("qr_render", "err", err, "slug", slug)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
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

// handleQRStand renders a printable QR stand (issue #438). Route:
// GET /qr/{slug}/stand (registered before /{slug} so it wins over a form
// slug). The optional ?table=N query param is encoded into the QR and shown
// on the stand. The page is self-contained (QR embedded as a data URL) and
// prints cleanly on A4.
func (h *PublicHandlers) handleQRStand(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var f *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		f, err = h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		err = dbstore.ErrFormNotFound
	}
	if err != nil || f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	table := strings.TrimSpace(r.URL.Query().Get("table"))
	target := h.formPublicURL(r, f)
	if table != "" {
		u, perr := url.Parse(target)
		if perr == nil {
			q := u.Query()
			q.Set("table", table)
			u.RawQuery = q.Encode()
			target = u.String()
		}
	}
	png, qerr := qrcode.Encode(target, qrcode.Medium, 512)
	if qerr != nil {
		slog.Error("qr_stand_render", "err", qerr, "slug", slug)
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	data := qrStandData{
		Title:    f.Title,
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
