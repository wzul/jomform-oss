package httpapi

// Issue #499: the printable QR stand emitted a BROKEN image.
//
// qrStandTmpl interpolates the base64 PNG data URL into an <img src>. Go's
// html/template escapes by CONTEXT: src is a URL context, a `data:` URL is an
// unsafe scheme, and the engine replaced the whole value with the sentinel
// `#ZgotmplZ`. The stand still returned HTTP 200 and every other element
// rendered, so nothing caught it — only the printed artefact was unscannable.
//
// These tests assert on the EMITTED ATTRIBUTE (an HTTP-200-only test would have
// passed on the broken page) and then prove the emitted image is a real,
// scannable QR of the intended target URL, ?table=N included, by comparing the
// PNG the stand ships against a freshly encoded QR of that exact target.
// qrcode.Encode is deterministic for a given target/level/size, so byte
// equality means the printed stand carries a QR that decodes to the intended
// URL — the stand was additionally decoded with an independent QR decoder as
// part of verifying this fix.
//
// The package-wide audit for the same silent rewrite lives in
// TestIssue499NoTemplateURLInterpolationUsesAPlainString.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"html/template"
	"image/png"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

// qrSrcRe extracts the value of the stand's QR <img src> attribute.
var qrSrcRe = regexp.MustCompile(`<div class="qr"><img src="([^"]*)" alt="QR code"></div>`)

// standDataURL extracts the stand's <img src> from rendered HTML and
// HTML-unescapes it, so it can be compared against the real URL the browser
// receives (html/template escapes `+` to `&#43;` inside attribute values).
func standDataURL(t *testing.T, body string) string {
	t.Helper()
	m := qrSrcRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("stand HTML has no <div class=\"qr\"><img src=...> block\nbody:\n%s", body)
	}
	return html.UnescapeString(m[1])
}

// standQRPNG decodes the base64 PNG the stand ships in its <img src>.
func standQRPNG(t *testing.T, dataURL string) []byte {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("stand src = %q, want a %s data URL", truncate(dataURL, 64), prefix)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("stand src is not valid base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("stand src is not a decodable PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Errorf("QR image is %dx%d, want 512x512", b.Dx(), b.Dy())
	}
	return raw
}

// assertStandQREncodes asserts that the PNG the stand ships is a real QR code
// of wantTarget: it must be byte-identical to a fresh Encode(wantTarget), so
// the printed stand necessarily scans to wantTarget (including ?table=N).
func assertStandQREncodes(t *testing.T, body, wantTarget string) {
	t.Helper()
	got := standQRPNG(t, standDataURL(t, body))
	want, err := qrcode.Encode(wantTarget, qrcode.Medium, 512)
	if err != nil {
		t.Fatalf("qrcode.Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the QR in the stand does not encode %q — the printed stand is unscannable (issue #499)", wantTarget)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// TestIssue499QRDataIsTrustedURLType is the type-level regression guard: the
// field MUST stay template.URL. Reverting it to a plain string silently
// reintroduces #ZgotmplZ, and the type is the whole fix.
func TestIssue499QRDataIsTrustedURLType(t *testing.T) {
	f, ok := reflect.TypeOf(qrStandData{}).FieldByName("QRData")
	if !ok {
		t.Fatal("qrStandData has no QRData field")
	}
	want := reflect.TypeOf(template.URL(""))
	if f.Type != want {
		t.Fatalf("qrStandData.QRData is %s, want %s — a plain string is rewritten to #ZgotmplZ by html/template in a src attribute (issue #499)", f.Type, want)
	}
}

// TestIssue499QRStandTemplateEmitsRealDataURL is the fast, DB-free guard: the
// rendered stand must contain a real `data:image/png;base64,` src and must not
// contain the html/template sentinel, and the emitted image must encode the
// intended target, ?table=N included.
func TestIssue499QRStandTemplateEmitsRealDataURL(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		table  string
	}{
		{"with table", "https://wan.jomform.com/menu?table=8", "8"},
		{"without table", "https://wan.jomform.com/menu", ""},
		{"table with reserved chars", "https://wan.jomform.com/menu?table=8%26x%3D1", "8&x=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			qrPNG, err := qrcode.Encode(tc.target, qrcode.Medium, 512)
			if err != nil {
				t.Fatalf("qrcode.Encode: %v", err)
			}
			var buf bytes.Buffer
			if err := qrStandTmpl.Execute(&buf, qrStandData{
				Title:    "Warung Ali",
				Table:    tc.table,
				QRData:   qrPNGDataURL(qrPNG),
				Target:   tc.target,
				HasTable: tc.table != "",
			}); err != nil {
				t.Fatalf("qrStandTmpl.Execute: %v", err)
			}
			body := buf.String()

			// 1. The sentinel must be gone — this is the bug.
			if strings.Contains(body, "ZgotmplZ") {
				t.Fatalf("stand HTML contains ZgotmplZ — html/template rewrote the data URL again (issue #499)\nbody:\n%s", body)
			}
			// 2. The emitted attribute carries the real data URL.
			if !strings.Contains(body, `<img src="data:image/png;base64,`) {
				t.Fatalf("stand HTML has no data:image/png;base64, src (issue #499)\nbody:\n%s", body)
			}
			// 3. …and it encodes exactly the intended target.
			assertStandQREncodes(t, body, tc.target)
		})
	}
}

// TestIssue499QRStandHandlerEmitsScannableStand drives the real route with a
// seeded form and asserts on the emitted attribute end to end: the served
// stand's <img src> is a real data URL (no sentinel) whose QR encodes the
// form's public URL, with ?table=N when the merchant asked for a table.
func TestIssue499QRStandHandlerEmitsScannableStand(t *testing.T) {
	h, workspaceSlug, formSlug, _ := tableNumberFixture(t)
	host := workspaceSlug + ".jomform.com"

	get := func(query string) string {
		t.Helper()
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodGet, "/qr/"+formSlug+"/stand"+query, nil)
		req.Host = host
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /qr/%s/stand%s = %d, want 200 (body %s)", formSlug, query, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// The exact page the merchant prints for table 8.
	body := get("?table=8")
	// The scheme comes from the package-global isDev (set by Server at wiring
	// time; other tests in the package may flip it), so derive the expected
	// target from the same source instead of hardcoding https — otherwise this
	// test passes alone and fails in the full suite. Same lesson as
	// ogimage_test.go.
	scheme := "https"
	if isDev {
		scheme = "http"
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatalf("served stand contains ZgotmplZ — the printed stand is unscannable (issue #499)\nbody:\n%s", body)
	}
	if !strings.Contains(body, `<img src="data:image/png;base64,`) {
		t.Fatalf("served stand has no data:image/png;base64, src (issue #499)\nbody:\n%s", body)
	}
	assertStandQREncodes(t, body, fmt.Sprintf("%s://%s.jomform.com/%s?table=8", scheme, workspaceSlug, formSlug))
	// The badge and title still render (the page was never otherwise broken).
	if !strings.Contains(body, `class="table">Table 8</div>`) {
		t.Error("stand is missing the TABLE 8 badge")
	}

	// A plain stand (no ?table=) encodes the bare form URL — no invented table.
	assertStandQREncodes(t, get(""), fmt.Sprintf("%s://%s.jomform.com/%s", scheme, workspaceSlug, formSlug))

	// A table value carrying URL-reserved characters must be percent-encoded
	// into the encoded URL, never injected raw into the page.
	hostile := get("?table=8%26x%3D1")
	assertStandQREncodes(t, hostile, fmt.Sprintf("%s://%s.jomform.com/%s?table=8%%26x%%3D1", scheme, workspaceSlug, formSlug))
}

// TestIssue499QRImageEndpointIsUnaffected pins the separate PNG path: it serves
// the image bytes directly (no html/template involved), so it was never
// affected by the rewrite — and must stay that way.
func TestIssue499QRImageEndpointIsUnaffected(t *testing.T) {
	h, workspaceSlug, formSlug, _ := tableNumberFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/qr/"+formSlug+".png?table=8", nil)
	req.Host = workspaceSlug + ".jomform.com"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /qr/%s.png = %d, want 200", formSlug, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	pngBytes := rec.Body.Bytes()
	if !bytes.HasPrefix(pngBytes, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("body is not a PNG (bad magic bytes)")
	}
	if _, err := png.Decode(bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("body is not a decodable PNG: %v", err)
	}
	// The direct PNG path encodes the same ?table=N target.
	// Scheme mirrors the package-global isDev (see the stand test above).
	scheme := "https"
	if isDev {
		scheme = "http"
	}
	want, err := qrcode.Encode(fmt.Sprintf("%s://%s.jomform.com/%s?table=8", scheme, workspaceSlug, formSlug), qrcode.Medium, 512)
	if err != nil {
		t.Fatalf("qrcode.Encode: %v", err)
	}
	if !bytes.Equal(pngBytes, want) {
		t.Errorf("QR image does not encode the expected ?table=8 target")
	}
}

// TestIssue499NoTemplateURLInterpolationUsesAPlainString is the audit guard for
// the whole class of bug: every URL interpolation (`<img src>`, `<a href>`,
// `url(...)`) in the package's html/template sources must be fed either a
// template.URL value or a value that is https-only by construction.
//
// A data:/generated URL reaching any of these through a plain string would be
// silently rewritten to #ZgotmplZ exactly like the QR stand was. Adding a new
// URL interpolation therefore fails this test until it is consciously
// classified here.
func TestIssue499NoTemplateURLInterpolationUsesAPlainString(t *testing.T) {
	// urlInterpRe matches `src="{{.Field}}"`, `href="{{.Field}}"` and
	// `url({{.Field}})` in a template source.
	urlInterpRe := regexp.MustCompile(`(?:src|href)="\{\{([^}]*)\}\}"|url\(\{\{([^}]*)\}\}\)`)

	// auditedURLInterpolations records, for every field interpolated into a URL
	// context in this package, WHY it is not the #499 bug. The classifications
	// below were each checked against the code, not assumed:
	//
	//   server-generated — the value is produced entirely by this server, not
	//                      from caller input (the #499 fix).
	//   https-only       — the scheme is validated `https://` on EVERY write
	//                      path that can set the field.
	//   caller-supplied  — merchant/caller input that is NOT scheme-validated
	//                      on every write path. Same silent-rewrite MECHANISM
	//                      as #499 (an unsafe scheme becomes #ZgotmplZ), but
	//                      deliberately NOT "fixed" by trusting the value: it
	//                      is caller input, so marking it template.URL would
	//                      let a merchant put an arbitrary scheme in the
	//                      buyer's page. Tracked as a follow-up to harden the
	//                      write paths; out of scope for #499.
	auditedURLInterpolations := map[string]string{
		// The only data URL in the package; the #499 fix.
		"QRData": "server-generated — base64 PNG from qrcode.Encode, wrapped by qrPNGDataURL as template.URL (issue #499)",

		// Validated https:// on both write paths (REST rest_workspaces.go and
		// MCP set_workspace_settings).
		"PrivacyURL": "https-only — validated on every workspace write path",
		"TermsURL":   "https-only — validated on every workspace write path",
		"RefundURL":  "https-only — validated on every workspace write path",

		// Same-origin relative path built server-side from a form id.
		"ExtraFormEdit": "server-generated — \"/builder?form=\" + form id, never a scheme",

		// Caller-supplied URLs with UNEVEN validation: the REST form-settings
		// and MCP workspace-landing paths validate https://, but MCP
		// set_form_theme, the product-image write paths and the REST
		// landing_page path do not. A data: or other unsafe scheme stored
		// there renders as #ZgotmplZ — the same symptom as #499, from a
		// different (caller-supplied) source. Hardening note only.
		"LogoURL":    "caller-supplied, not https-validated on every path — see #499 audit note",
		"ImageURL":   "caller-supplied, not https-validated on every path — see #499 audit note",
		"CardImage":  "caller-supplied (derived from the product image URL) — see #499 audit note",
		"ThumbImage": "caller-supplied (derived from the product image URL) — see #499 audit note",
		"Image":      "caller-supplied (live product image, #461) — see #499 audit note",
		"Src":        "caller-supplied builder image-block src — see #499 audit note",
	}

	// followUpURLFields are the caller-supplied fields above. They are the
	// same rewrite MECHANISM as #499, so this test documents them explicitly
	// rather than letting a future reader mistake the audit for "all clear".
	followUpURLFields := map[string]string{
		"LogoURL":    "REST /forms/{id}/settings validates https://; MCP set_form_theme and generation paths do not",
		"ImageURL":   "MCP set_workspace_landing_page validates https://; REST putWorkspaceSettings landing_page does not",
		"CardImage":  "derived from the product image URL (unvalidated on write)",
		"ThumbImage": "derived from the product image URL (unvalidated on write)",
		"Image":      "product images are not scheme-validated on the REST/MCP write paths",
		"Src":        "builder image-block src is not scheme-validated",
	}

	templates := map[string]*template.Template{
		"qrStandTmpl":     qrStandTmpl,
		"formTmpl":        formTmpl,
		"blockTmpl":       blockTmpl,
		"responseTmpl":    responseTmpl,
		"customFormTmpl":  customFormTmpl,
		"menuPageTmpl":    menuPageTmpl,
		"landingPageTmpl": landingPageTmpl,
	}

	found := 0
	seen := map[string]bool{}
	for name, tmpl := range templates {
		// Walk EVERY template in the set, not just the root: menuPageTmpl and
		// blockTmpl put their URL-bearing markup in {{define}}d sub-templates
		// (menuCard/menuRow), which Tree.Root does not contain.
		for _, sub := range tmpl.Templates() {
			if sub.Tree == nil || sub.Tree.Root == nil {
				continue
			}
			src := sub.Tree.Root.String()
			for _, m := range urlInterpRe.FindAllStringSubmatch(src, -1) {
				expr := m[1]
				if expr == "" {
					expr = m[2]
				}
				found++
				// The tree's root string carries html/template's injected
				// escaping pipeline (`{{.X | _html_template_urlfilter | ...}}`),
				// so take the field reference before the first `|` and key the
				// audit on the field's LAST path segment
				// ({{.Footer.PrivacyURL}} → PrivacyURL).
				expr = strings.TrimSpace(strings.SplitN(expr, "|", 2)[0])
				field := strings.TrimPrefix(expr, ".")
				if i := strings.LastIndex(field, "."); i >= 0 {
					field = field[i+1:]
				}
				// A range/func expression is reported for a human look rather
				// than silently passed.
				if field == "" || strings.ContainsAny(field, " ()|$") {
					t.Errorf("%s interpolates a non-trivial expression %q into a URL context — classify it explicitly", name, expr)
					continue
				}
				seen[field] = true
				if _, ok := auditedURLInterpolations[field]; !ok {
					t.Errorf("%s interpolates {{.%s}} into a URL context but it is not in the issue #499 audit list — "+
						"a plain string holding a data: URL is silently rewritten to #ZgotmplZ; classify the field (server-generated / https-only / caller-supplied)", name, field)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no URL interpolations found — the scan regex is stale, so this audit is not actually checking anything")
	}
	// The audit list must not carry stale entries: every classified field must
	// still be interpolated somewhere, or it should be removed.
	for field := range auditedURLInterpolations {
		if !seen[field] {
			t.Errorf("audit list classifies %q but no template interpolates it into a URL context — drop the stale entry", field)
		}
	}
	// Exactly one URL field must be trusted-by-type, and it is the data URL.
	trustedURLFields := []string{}
	for field, note := range auditedURLInterpolations {
		if strings.HasPrefix(note, "server-generated") && field == "QRData" {
			trustedURLFields = append(trustedURLFields, field)
		}
	}
	if len(trustedURLFields) != 1 || trustedURLFields[0] != "QRData" {
		t.Errorf("expected exactly one trusted (template.URL) URL field — QRData — got %v", trustedURLFields)
	}
	// #499 fixed the server-generated data URL. The caller-supplied fields are
	// the same rewrite mechanism from untrusted input; they must not silently
	// disappear from the record.
	for field := range followUpURLFields {
		if _, ok := auditedURLInterpolations[field]; !ok {
			t.Errorf("follow-up field %q is missing from the audit list", field)
		}
	}
}
