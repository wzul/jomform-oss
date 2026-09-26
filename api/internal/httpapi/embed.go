// Embed endpoint: iframe-friendly form rendering (CSP frame-ancestors *) plus
// the submit route that the rendered page posts to.
package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// embedForm serves the public form with permissive framing for merchant sites.
// Path pattern is registered as GET /embed/f/{wsslug}/{slug}. The first segment
// is a WORKSPACE slug: the embed resolves a form through the same workspace-only
// lookup the public host uses (a workspace is served on its workspace subdomain
// and nothing else), so the segment must name the workspace, not the account.
func (h *PublicHandlers) embedForm(w http.ResponseWriter, r *http.Request) {
	f := h.embedFormBySlug(r.Context(), r.PathValue("wsslug"), r.PathValue("slug"))
	if f == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	// This route's own rules, applied to the RESOLVED form: a draft is never
	// served, and a form that forbids embedding is refused — an embed URL cannot
	// become a way around either.
	if f.Status == dbstore.FormStatusDraft {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if !f.AllowEmbed {
		writeErr(w, http.StatusForbidden, "embed dinyahaktifkan")
		return
	}
	// The BODY is rendered by the same path the canonical form URL uses, so the
	// two cannot diverge. This route used to call only renderBlocks (falling
	// back to serveForm), which meant three things differed from the form's own
	// URL while claiming to render the same form:
	//
	//   - MODE: a response-only form has no builder blocks to pay for, so it was
	//     rendered with the PAYMENT layout — a "Proceed to Payment" button and a
	//     "select at least one product" dead end on a form that collects no
	//     money. Measured against the same form on /{slug}: response layout there
	//     ("Submit", the form's own fields), payment layout here.
	//   - The PRIVATE-FORM password gate was not shown, so a private form's
	//     fields were rendered to anyone who knew the embed URL.
	//   - The scheduling / entry-limit RESTRICTIONS were not applied, so a form
	//     that had not opened yet or was already full rendered as if it were open.
	//
	// All three are the same root cause: this route had its own copy of the
	// render dispatch. Delegating removes the class rather than patching each
	// symptom, and it is what makes the documented snippet an embed a merchant
	// can actually rely on.
	//
	// The framing is this route's own and is applied by the wrapper below, so it
	// is exactly this route's headers that stay permissive (see
	// embedFramingWriter for why it cannot simply be set here).
	h.serveForm(&embedFramingWriter{ResponseWriter: w}, r, f)
}

// embedFramingWriter forces this route's permissive framing onto whatever the
// shared renderer writes.
//
// Two reasons it cannot be a plain w.Header().Set before the call:
//
//  1. A renderer may set a STRICTER policy of its own after it. The custom-HTML
//     response form (render_custom.go) pins `frame-ancestors 'self'` plus
//     X-Frame-Options: SAMEORIGIN, so setting ours first would silently lose to
//     it and the merchant's iframe would be refused — the very failure this
//     route exists to prevent.
//  2. The shared renderer must not have to know which route it is serving.
//
// So the framing is forced at the moment of the write, which is also the only
// point at which it is known to be the final value. Only the framing directives
// are touched: forceEmbedFraming leaves any other CSP directives the renderer
// set intact.
type embedFramingWriter struct {
	http.ResponseWriter
	forced bool
}

func (w *embedFramingWriter) WriteHeader(code int) {
	w.force()
	w.ResponseWriter.WriteHeader(code)
}

func (w *embedFramingWriter) Write(b []byte) (int, error) {
	w.force()
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying writer to http.ResponseController, so wrapping
// cannot hide optional interfaces the way the logging middleware did (issue
// #615).
func (w *embedFramingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *embedFramingWriter) force() {
	if w.forced {
		return
	}
	w.forced = true
	forceEmbedFraming(w.Header())
}

// forceEmbedFraming makes a response framable from any origin: it sets
// X-Frame-Options and makes sure the CSP carries `frame-ancestors *`.
//
// An existing CSP keeps every other directive it declares and only has
// frame-ancestors replaced (or added when absent) — a renderer's other
// protections are not this route's to drop.
func forceEmbedFraming(h http.Header) {
	h.Set("X-Frame-Options", "ALLOWALL")
	csp := h.Get("Content-Security-Policy")
	if strings.TrimSpace(csp) == "" {
		h.Set("Content-Security-Policy", "frame-ancestors *")
		return
	}
	directives := make([]string, 0, 8)
	replaced := false
	for _, d := range strings.Split(csp, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, "frame-ancestors") {
			directives = append(directives, "frame-ancestors *")
			replaced = true
			continue
		}
		directives = append(directives, d)
	}
	if !replaced {
		directives = append(directives, "frame-ancestors *")
	}
	h.Set("Content-Security-Policy", strings.Join(directives, "; "))
}

// embedSubmit accepts the submission an EMBEDDED form posts.
// Path pattern is registered as POST /embed/f/{wsslug}/{slug}/order.
//
// WHY THE ROUTE HAS TO EXIST. The rendered embed page posts to
// location.pathname + "/order" — its own address — so on this route that is
// /embed/f/{workspace}/{slug}/order. Nothing was registered there, which made
// the embed RENDER but never SUBMIT: measured in a real browser, a buyer
// filling an embedded payment form and pressing the button got a bare 404 from
// the fetch ("404 page not found") and the page sat there unchanged. A snippet
// whose submit path is dead is not one a merchant can paste, so documenting the
// embed URL without this route would only move the broken instruction from the
// URL to the button.
//
// WHY THE SCOPE IS THE PATH, NOT THE HOST. The canonical submit
// (POST /{slug}/order) resolves its form from the request Host —
// accountSubdomain / customDomainOwner over r.Host. That cannot work here: an
// embed is served on the MERCHANT's OWN site, so the Host is the merchant's
// domain and never names the workspace. This is also why the GET on this route
// already resolves by path. Measured on a running API, same form slug, Host
// naming a workspace that holds no such form:
//
//	Host: someoneelse.jomform.com  GET /embed/f/t46ws/<slug>  -> 200  (path scope)
//	Host: someoneelse.jomform.com  GET /<slug>                -> 404  (host scope)
//
// so a host-scoped submit could not resolve an embedded form at all.
//
// WHAT IS SHARED. Everything after resolution: submitResolvedForm applies the
// identical draft/private/entry-limit gates and dispatches to the same response
// collector, menu checkout or order core. Only the LOOKUP differs, and it
// differs because it must — so the two submit paths cannot drift.
func (h *PublicHandlers) embedSubmit(w http.ResponseWriter, r *http.Request) {
	f := h.embedFormBySlug(r.Context(), r.PathValue("wsslug"), r.PathValue("slug"))
	if f == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	// Mirrors the GET's own rule: embedding switched off must not leave a live
	// submit endpoint behind the 403 page.
	if !f.AllowEmbed {
		writeErr(w, http.StatusForbidden, "embed dinyahaktifkan")
		return
	}
	h.submitResolvedForm(w, r, f)
}

// embedUnlock accepts the password an EMBEDDED private form's gate posts.
// Path pattern is registered as POST /embed/f/{wsslug}/{slug}/unlock.
//
// WHY THE ROUTE HAS TO EXIST. The gate a private form renders lives inside the
// embed document, and it posts to its own address plus "/unlock"
// (location.pathname + "/unlock") — the same rule the submit button follows with
// "/order". Nothing was registered under /embed/f/, so an embedded private form
// could be LOOKED AT but never unlocked: the gate posted to the canonical
// /{slug}/unlock instead, which resolves its form from the request Host — and an
// embed's Host is the MERCHANT's own domain, so the request never named the
// workspace (measured: POST /embed/f/{ws}/{slug}/unlock -> "404 page not found",
// POST /{ws}-{slug}/unlock -> reached the handler). A form a merchant embeds and
// then makes private is unusable without this.
//
// WHY THE SCOPE IS THE PATH, NOT THE HOST. Same reason as embedSubmit and the
// embed GET: the frame is served on the merchant's site, so r.Host is not the
// workspace. Resolution goes through embedFormBySlug, the one helper all three
// /embed/f/ routes share, so the page a merchant frames, the gate that page
// renders and the endpoints they post to can only ever resolve to one form.
//
// WHAT IS SHARED. Everything after resolution: unlockResolvedForm applies the
// identical private-status gate, password check and unlock cookie. Only the
// LOOKUP differs, so the two unlock paths cannot drift. The allow_embed rule is
// repeated here exactly as embedSubmit repeats it — switching embedding off must
// not leave a live unlock endpoint behind the 403 page.
func (h *PublicHandlers) embedUnlock(w http.ResponseWriter, r *http.Request) {
	f := h.embedFormBySlug(r.Context(), r.PathValue("wsslug"), r.PathValue("slug"))
	if f == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	if !f.AllowEmbed {
		writeErr(w, http.StatusForbidden, "embed dinyahaktifkan")
		return
	}
	h.unlockResolvedForm(w, r, f)
}

// embedFormBySlug resolves the embed route's target with the SAME live-first
// order findPublicForm applies, returning nil when neither a live form nor a
// rename's temporary 301 matches. BOTH the GET above and the POST call it, so
// the page a merchant frames and the endpoint its button posts to can only
// resolve to the same form.
//
// The scope is the route's own {wsslug} path segment: the embed resolves a
// WORKSPACE slug, through the same workspace-only lookup the public host uses
// (a workspace is served on its workspace subdomain and nothing else). It is
// deliberately NOT findPublicForm: that helper
// reads the tenant scope off the request HOST, and swapping the scope would 404
// an embed URL that works today (measured above). The SCOPE must stay the path
// segment while the ORDER must not differ, so the ordering is repeated here
// verbatim.
//
// WITHOUT this fallback a renamed form's OLD embed URL 404s, so an <iframe> a
// merchant already pasted on his own site stops rendering — the same
// "orphaned URL already handed out" class the rename's 301 exists to prevent,
// and the reason this route must not keep a single-lookup resolver of its own
// while every other public slug lookup (render, submit, unlock, QR image, QR
// stand, OG image, menu order) resolves the old slug.
//
// THE ORDER IS THE RULE (Wan, 2026-09-20: "301 sementara. kalau ada form lain
// yg claim, maka url tu akan ke form yang baru"): the live lookup runs BEFORE
// the redirect table is consulted, so a form that later claims an old slug
// wins and a rename's history can never shadow a real, live form.
func (h *PublicHandlers) embedFormBySlug(ctx context.Context, scopeSlug, slug string) *dbstore.Form {
	if h.Forms == nil {
		return nil
	}
	if f, err := h.Forms.ByWorkspaceSlug(ctx, scopeSlug, slug); err == nil && f != nil {
		return f
	}
	// Fallback: a rename left a temporary 301 behind this slug. The scope slug
	// keeps the route's own account-or-workspace duality, so the fallback
	// mirrors that order rather than inventing a third interpretation.
	//
	// The store error is checked in BOTH arms, not just the workspace one:
	// scanOne returns its pre-allocated struct non-nil when the SCAN fails for
	// any reason other than "no rows" (a column-alignment slip is the realistic
	// one — see redirectFormColumns), and a zero-value Form read as a real hit
	// would serve the wrong form rather than 404.
	if f, err := h.Forms.RedirectedFormByAccountSlug(ctx, scopeSlug, slug); err == nil && f != nil {
		return f
	}
	if f, err := h.Forms.RedirectedFormByWorkspaceSlug(ctx, scopeSlug, slug); err == nil && f != nil {
		return f
	}
	return nil
}
