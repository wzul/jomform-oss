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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/customform"
	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/email"
	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/formfields"
	"github.com/wzul/jomform/api/internal/objstore"
	"github.com/wzul/jomform/api/internal/payments"
	"github.com/wzul/jomform/api/internal/spamjudge"
)

// PublicHandlers serves buyer-facing order forms.
type PublicHandlers struct {
	Forms    *dbstore.FormStore
	Sales    *dbstore.SaleStore
	Accounts *dbstore.AccountStore
	// Issue #236: multi-domain resolution via the account_domains table.
	// Nil in bare-mode tests; customDomainOwner then falls back to the
	// legacy accounts.custom_domain column.
	Domains           *dbstore.DomainStore
	Products          *dbstore.ProductStore
	ProductVariations *dbstore.ProductVariationStore
	Pay               *payments.Service
	SystemSettings    *dbstore.SystemSettingStore
	Workspaces        *dbstore.WorkspaceStore
	// Issue #171: response-only form submissions.
	Submissions *dbstore.SubmissionStore
	BaseURL     string
	// AccessKey (ENCRYPTION_KEY bytes) signs the private-form unlock cookie
	// (issue #176). Nil disables private-form access (gate always shown).
	AccessKey []byte
	// Issue #244: deterministic anti-spam layers on public form submissions.
	//   - Sessions + Users + WorkspaceMembers resolve whether the submitter is a
	//     logged-in owner/admin of the form's workspace, in which case the
	//     per-form rate limit and time-based check are skipped (owner testing
	//     the form shouldn't be throttled). All nil in bare-mode tests.
	//   - MXChecker validates the submitted email's domain accepts mail
	//     (issue #56 reuse). Nil disables the MX check (format still checked).
	//   - FormLimiter is the per-form-per-IP token bucket (15/hour). Nil
	//     disables the per-form rate limit (bare-mode tests).
	Sessions         *auth.Sessions
	Users            *dbstore.UserStore
	WorkspaceMembers *dbstore.WorkspaceMemberStore
	MXChecker        *email.MXChecker
	FormLimiter      *middleware.RateLimiter

	// Issue #245: background AI spam judge. SpamQueue is the Valkey-backed
	// background queue (#199) that spam-judge jobs are enqueued to; a worker
	// sends each submission to Ollama Cloud gemma4 and flags it as spam in the
	// background. Both nil in bare-mode tests → no judge runs (submissions are
	// accepted immediately, never flagged).
	SpamQueue *emailqueue.Queue
	// Issue #361: S3/MinIO store serving the workspace/custom-domain code-asset
	// route GET /assets/{key}. Nil when media storage isn't configured.
	Store ObjectStoreIface
	// Issue #364: media metadata store used to gate code-asset serving on the
	// background scan status. Only assets scanned as 'ok' are served. Nil-safe.
	Media *dbstore.MediaStore

	// Realtime (issue #447): in-memory pub/sub hub backing the SSE stream.
	// The response.submitted path publishes here so a merchant's open
	// dashboard updates live when a response form is submitted. Nil in
	// bare-mode tests → no publish (no-op).
	Realtime *RealtimeHub
}

// RegisterRoutes wires public endpoints.
func (h *PublicHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /f/{slug}", h.handleRender)
	// Issue #411: GitHub-style default Open Graph image per form, served at
	// /og/{slug}.png on the workspace subdomain / custom domain. Registered before
	// /{slug} so it wins over a form slug. NOTE: Go's ServeMux wildcard must be
	// a full path segment — `{slug}.png` is invalid and panics at startup — so
	// the route is /og/{slug} and the handler strips the .png suffix.
	mux.HandleFunc("GET /og/{slug}", h.handleOGImage)
	// Issue #438: QR ordering flow — a QR code pointing at a form's public
	// URL, served at /qr/{slug}.png on the workspace subdomain / custom
	// domain. Registered before /{slug} so it wins over a form slug. The
	// optional ?table=N query param is encoded into the QR so a merchant can
	// generate a per-table stand. NOTE: Go's ServeMux wildcard must be a full
	// path segment — `{slug}.png` is invalid and panics at startup — so the
	// route is /qr/{slug} and the handler strips the .png suffix.
	mux.HandleFunc("GET /qr/{slug}", h.handleQRImage)
	// Issue #438: QR ordering flow — a printable QR stand (Beep-style) the
	// merchant can put on a table. Served at /qr/{slug}/stand; the optional
	// ?table=N query param is encoded into the QR and shown on the stand.
	mux.HandleFunc("GET /qr/{slug}/stand", h.handleQRStand)
	// Issue #361: merchant code assets (CSS/JS/HTML/fonts) served on the
	// workspace/custom domain at /assets/{key}, so a landing page can reference
	// them same-origin (/styles/app.css). Registered before /{slug} so assets
	// win over a form slug.
	mux.HandleFunc("GET /assets/{key...}", h.handleAsset)
	mux.HandleFunc("GET /embed/f/{wsslug}/{slug}", h.embedForm)
	// The submit the embedded page posts to (location.pathname + "/order").
	// Registered alongside the GET so an embed can actually be SUBMITTED, and
	// on the same path scope its GET resolves with — an embed is served on the
	// merchant's own site, so the request Host never names the workspace.
	// Rate-limited like the canonical order routes: it creates sales.
	mux.Handle("POST /embed/f/{wsslug}/{slug}/order", middleware.RateLimit(http.HandlerFunc(h.embedSubmit)))
	// The unlock an EMBEDDED private form's gate posts to. Registered on the
	// same path scope as the GET and the /order above, because the gate lives
	// inside the embed document and must post to its OWN address (exactly as
	// the submit button posts to its own path + "/order"). Without this route
	// the gate hardcoded the canonical /{slug}/unlock, which is host-scoped —
	// and an embed's Host is the MERCHANT's domain, so the unlock could never
	// name the workspace. See embedUnlock.
	// NOTE: the wildcard is {wsslug} on all three /embed/f/ routes, matching the
	// handler reads — under a pattern named {accslug} a handler reading
	// PathValue("wsslug") resolves "", and the scope is lost. See embedForm.
	mux.HandleFunc("POST /embed/f/{wsslug}/{slug}/unlock", h.embedUnlock)
	// Issue #276: sitemap.xml for custom-domain workspaces (indexed workspaces only).
	mux.HandleFunc("GET /sitemap.xml", h.handleSitemap)
	// Issue #276: indexed custom-domain workspace URLs for the web sitemap.
	mux.HandleFunc("GET /sitemap-workspaces.json", h.handleSitemapWorkspaces)
	mux.Handle("POST /f/{slug}/order", middleware.RateLimit(http.HandlerFunc(h.handleSubmit)))
	// Workspace subdomains serve forms at /{slug} (GET /{slug} below), so the
	// buyer-facing submit must resolve there too. handleSubmit scopes by
	// accountSubdomain(host); on bare jomform.com it 404s (issue #77 — a
	// public payment link is reachable only via its workspace subdomain or a
	// custom domain), leaving dashboard routes (/api, /v1) intact.
	mux.Handle("POST /{slug}/order", middleware.RateLimit(http.HandlerFunc(h.handleSubmit)))
	// Issue #491: workspace-level order endpoint — the restaurant menu page
	// checks out here with NO form in the path. A dedicated single-segment
	// literal path is deliberately used: a workspace slug and a form slug share
	// one URL namespace, so /{workspaceSlug}/order would be ambiguous with the
	// form route above, and /menu/order would shadow the order endpoint of a
	// form whose slug is literally "menu". A literal segment is never a
	// wildcard match, so no form slug can reach it. Resolves the account from
	// the request host (workspace subdomain or verified custom domain).
	mux.Handle("POST /menu-order", middleware.RateLimit(http.HandlerFunc(h.handleMenuOrder)))
	// t_aa6e3596: the LIVE buyability of a menu form's products. The served menu
	// markup flags a sold-out variation with `disabled`, which is a RENDER-TIME
	// value — and the page stays open, so a dish can sell out while the buyer is
	// reading it. The page asks here before it adds a line, so the basket cannot
	// hold a line the order core will refuse with 409 "stok habis".
	//
	// The path is a LITERAL first segment for the reason POST /menu-order above
	// documents: a form slug and a workspace slug share one URL namespace, and a
	// literal segment can never be shadowed by a form whose slug is literally
	// "menu-stock". {slug} is the menu FORM's own public slug, so the read is
	// scoped to the same form the page was served from — and the lookup inside
	// (resolveMenuStockForm) is the same Host-scoped one POST /{slug}/order uses.
	//
	// Rate-limited like the routes that create sales: it is an unauthenticated
	// DB-backed read keyed on a guessable path, so it must not be a free way to
	// hammer the products/variations tables.
	mux.Handle("GET /menu-stock/{slug}", middleware.RateLimit(http.HandlerFunc(h.handleMenuStock)))
	mux.HandleFunc("GET /t/{ref}", h.handleThankYou)
	// Issue #202: lightweight read-only status endpoint the thank-you page
	// polls while a sale is pending (bounded client-side poll). Returns the
	// sale's current status; never mutates anything.
	mux.HandleFunc("GET /t/{ref}/status", h.handleThankYouStatus)
	mux.HandleFunc("GET /{$}", h.handleWorkspaceRoot)
	// Issue #184: hosted legal pages on workspace subdomains
	// ({workspace}.jomform.com/privacy, /terms, /refund). More specific than
	// GET /{slug}, so they win over the form-slug route.
	mux.HandleFunc("GET /privacy", h.handlePublicLegalPage)
	mux.HandleFunc("GET /terms", h.handlePublicLegalPage)
	mux.HandleFunc("GET /refund", h.handlePublicLegalPage)
	mux.HandleFunc("GET /{slug}", h.handleWorkspaceRootForm)
	// Issue #176: unlock a private form with its access password.
	//
	// BOTH paths a private form can be REACHED at need this, exactly as both
	// need a matching /order above: the gate posts to its own address plus
	// "/unlock" (location.pathname + "/unlock"), so the page served at
	// /f/{slug} posts to /f/{slug}/unlock and the page served at /{slug}
	// posts to /{slug}/unlock. Registering only one leaves the other gate
	// posting at a route that does not exist — the embed bug (#722's class),
	// which is how /f/{slug} silently became unusable. handleFormUnlock
	// resolves the form from the request HOST for both, so they share one
	// handler.
	mux.HandleFunc("POST /f/{slug}/unlock", h.handleFormUnlock)
	mux.HandleFunc("POST /{slug}/unlock", h.handleFormUnlock)
}

// genRef produces a short opaque reference code.
func genRef() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "JF-" + hex.EncodeToString(b)
}

// formMode is the httpapi-local name for the shared dbstore.FormMode rule.
// One delegating line, so the call sites in this package read as before while
// exactly ONE implementation of the rule exists.
func formMode(f *dbstore.Form) string { return dbstore.FormMode(f) }

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

// ─── Issue #244: deterministic anti-spam layers ──────────────────────────────
//
// Four self-built, zero-token layers that catch dumb bots before any AI is
// involved. Each layer REJECTS the submission (returns an error) rather than
// flagging it — issue #245 (AI spam detection) is a separate follow-up that
// adds is_spam/spam_reason fields + a background judge.

// antiSpamHoneypotField is the hidden field name rendered into every public
// form (CSS display:none). Bots auto-fill it; humans never see it. A filled
// value marks a bot.
const antiSpamHoneypotField = "website"

// antiSpamMinRenderTime is the minimum time a human needs to fill a form.
// Bots auto-submit immediately after the page loads, so a submit arriving
// faster than this is rejected (issue #244, layer 3).
const antiSpamMinRenderTime = 3 * time.Second

// antiSpamFormLimitPerHour is the max submissions per form per IP per hour
// (issue #244, layer 2). Wan suggested 15.
const antiSpamFormLimitPerHour = 15

// antiSpamRenderField is the hidden timestamp field name rendered into every
// public form. Its value is the render time (Unix seconds); the submit must
// arrive at least antiSpamMinRenderTime later.
const antiSpamRenderField = "jf_ts"

// submitInput is the JSON body of a public form submit (payment mode). Shared
// by handleSubmit and the anti-spam checks so the field shape stays in sync.
type submitInput struct {
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
	PaymentMethod string            `json:"payment_method"`         // chip | bank_direct
	SkipCapture   bool              `json:"skip_capture,omitempty"` // authorize now, capture later
	Fields        map[string]string `json:"fields,omitempty"`
	// Issue #438: QR ordering flow — the table number the QR stand encoded
	// (e.g. "8"). The buyer's client JS reads ?table=N from the URL and
	// echoes it back so the merchant knows where to serve.
	TableNumber string `json:"table_number,omitempty"`
	// Issue #444: affiliate marketing — the affiliate code (?aff=CODE) the
	// buyer landed with. The client JS reads ?aff=CODE from the URL and echoes
	// it back so the sale is attributed to that affiliate.
	AffiliateCode string `json:"affiliate_code,omitempty"`
}

// antiSpamRenderTime returns the current render timestamp (Unix seconds) to
// embed in the hidden anti-spam field.
func antiSpamRenderTime() int64 {
	return time.Now().Unix()
}

// antiSpamRenderFields returns the hidden honeypot + timestamp fields to
// inject into a rendered public form. The honeypot is a display:none text
// input; the timestamp is a hidden input carrying the render time. Both carry
// a name AND a data-id so the block renderer (which collects [data-id] keyed by
// data-id) and the response renderer (which collects [name] keyed by name)
// both submit them under the same field keys the server checks.
func antiSpamRenderFields() string {
	return `<div style="display:none" aria-hidden="true">
  <label>Website<input type="text" name="` + antiSpamHoneypotField + `" data-id="` + antiSpamHoneypotField + `" tabindex="-1" autocomplete="off"></label>
  <input type="hidden" name="` + antiSpamRenderField + `" data-id="` + antiSpamRenderField + `" value="` + strconv.FormatInt(antiSpamRenderTime(), 10) + `">
</div>`
}

// antiSpamSubmitterIsOwner reports whether the request is from a logged-in
// owner/admin of the form's workspace. When true, the per-form rate limit and
// time-based check are skipped (owner testing the form shouldn't be
// throttled). Returns false when any dependency is nil (bare-mode tests) or
// the session/workspace cannot be resolved.
func (h *PublicHandlers) antiSpamSubmitterIsOwner(r *http.Request, form *dbstore.Form) bool {
	if h.Sessions == nil || h.Users == nil || h.WorkspaceMembers == nil || h.Workspaces == nil {
		return false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	uid, issued, err := h.Sessions.GetCreated(r.Context(), c.Value)
	if err != nil || uid == "" {
		return false
	}
	// Password-reset invalidation (issue #62): a session issued before the
	// user's last password reset is dead.
	if u, e := h.Users.ByID(r.Context(), uid); e == nil && u != nil && u.PwResetAt != nil && !auth.ValidAfter(issued, *u.PwResetAt) {
		return false
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(r.Context(), form.AccountID)
	if err != nil || workspaceID == "" {
		return false
	}
	admin, err := h.WorkspaceMembers.IsAdmin(r.Context(), workspaceID, uid)
	return err == nil && admin
}

// antiSpamSubmitterIsOwnerForAccount was removed in issue #497 along with the
// formless workspace-scoped menu order: every menu order is now attributed to a
// form, so antiSpamSubmitterIsOwner (form-scoped) covers it.

// antiSpamCheckHoneypot rejects a submit whose hidden honeypot field is
// filled (issue #244, layer 1). Returns true when the submit is spam.
func (h *PublicHandlers) antiSpamCheckHoneypot(r *http.Request, in *submitInput) bool {
	// The honeypot rides on the top-level fields map (the renderer emits it
	// as a named input, and the client JS collects every [data-id] element).
	// A filled value marks a bot.
	if v, ok := in.Fields[antiSpamHoneypotField]; ok && strings.TrimSpace(v) != "" {
		slog.Warn("form_honeypot_caught", "ip", middleware.ClientIP(r))
		return true
	}
	return false
}

// antiSpamCheckTime rejects a submit that arrives too fast after the form was
// rendered (issue #244, layer 3). The renderer embeds a hidden timestamp; a
// submit whose elapsed time is under antiSpamMinRenderTime is a bot. Returns
// true when the submit is spam.
func (h *PublicHandlers) antiSpamCheckTime(r *http.Request, in *submitInput) bool {
	raw, ok := in.Fields[antiSpamRenderField]
	if !ok {
		// No render timestamp — nothing to measure. Skip the time check
		// (a real renderer always embeds it, but API clients and legacy
		// callers may not; they are not necessarily bots). The honeypot,
		// rate limit, and email layers still apply.
		return false
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false
	}
	elapsed := time.Since(time.Unix(ts, 0))
	return elapsed < antiSpamMinRenderTime
}

// antiSpamCheckRateLimit enforces the per-form-per-IP rate limit (issue #244,
// layer 2): max antiSpamFormLimitPerHour submissions per form per IP per hour.
// Returns true when the submit is over the limit.
func (h *PublicHandlers) antiSpamCheckRateLimit(r *http.Request, form *dbstore.Form) bool {
	if form == nil {
		return false
	}
	return h.antiSpamCheckRateLimitKey(r, form.ID)
}

// antiSpamCheckRateLimitKey is antiSpamCheckRateLimit over an arbitrary target
// key. The formless (menu) order path (issue #491) has no form id, so it keys
// the same token bucket on the workspace id — the protection is preserved on
// the formless path instead of silently dropped. An empty key never throttles.
// Returns true when the submit is over the limit.
func (h *PublicHandlers) antiSpamCheckRateLimitKey(r *http.Request, key string) bool {
	if h.FormLimiter == nil || key == "" {
		return false
	}
	return !h.FormLimiter.Allow(key + "|" + middleware.ClientIP(r))
}

// antiSpamCheckEmail validates the submitted email format + MX (issue #244,
// layer 5). Returns true when the email is invalid (reject the submit).
func (h *PublicHandlers) antiSpamCheckEmail(r *http.Request, emailAddr string) bool {
	emailAddr = strings.TrimSpace(emailAddr)
	if !auth.ValidEmail(emailAddr) {
		return true
	}
	// MX check (issue #56 reuse): reject a domain that accepts no mail. Only
	// when a checker is wired (nil in bare-mode tests → format-only).
	if h.MXChecker == nil {
		return false
	}
	domain := email.DomainOf(emailAddr)
	if domain == "" {
		return true
	}
	ok, err := h.MXChecker.Check(r.Context(), domain)
	if err != nil {
		// Fail closed (conservative): a domain we cannot verify is rejected.
		return true
	}
	return !ok
}

// enqueueSpamJudge enqueues a background AI spam-judge job (issue #245) for a
// submission or sale. Best-effort: a failure is logged and never fails the
// submit response. The judge is skipped when the submitter is the workspace
// owner/admin (testing their own store), when no queue is wired, or when the
// workspace has turned the judge off.
//
// accountID is passed explicitly (rather than read off the form) so the
// formless menu-order path (issue #491) can enqueue a judge job too; form is
// nil on that path and its form id/title are then omitted from the job.
//
// The per-workspace toggle gates the ENQUEUE, not the worker — a
// workspace with the judge off sends nothing to the queue, so no AI work is
// spent and is_spam is never set on a new entry. The workspace is resolved
// from accountID, which is exactly what this signature already carries, so the
// formless path is gated identically to the form path.
func (h *PublicHandlers) enqueueSpamJudge(r *http.Request, accountID string, form *dbstore.Form, owner bool, subID, saleID, emailAddr string, data map[string]any) {
	if owner || h.SpamQueue == nil || accountID == "" {
		return
	}
	// Per-workspace AI spam-judge toggle. A lookup error fails
	// toward running the judge (the store returns true with the error): the
	// toggle removes work, so a database hiccup must not silently disable spam
	// protection platform-wide, and running is the pre-existing behaviour.
	// Never fails the submission.
	if h.Workspaces != nil {
		on, err := h.Workspaces.SpamJudgeEnabledByAccount(r.Context(), accountID)
		if err != nil {
			slog.Warn("spamjudge_toggle_lookup_failed", "account", accountID, "err", err)
		}
		if !on {
			return
		}
	}
	job := spamjudge.Job{
		SubmissionID: subID,
		SaleID:       saleID,
		AccountID:    accountID,
		Data:         data,
		Email:        emailAddr,
		IP:           middleware.ClientIP(r),
	}
	if form != nil {
		job.FormID = form.ID
		job.FormTitle = form.Title
	}
	if err := spamjudge.Enqueue(r.Context(), h.SpamQueue, job); err != nil {
		slog.Warn("spamjudge_enqueue_failed", "account", accountID, "err", err)
	}
}

// handleSubmit creates a pending sale, requests a CHIP purchase and returns
// the checkout URL. In response mode (issue #171) it instead stores the
// submitted field values as a submission — no sale, no payment, no checkout.
func (h *PublicHandlers) handleSubmit(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// Same lookup rules as render: subdomain scoping first. On bare
	// jomform.com (no subdomain, no custom domain) a published form is NOT
	// served — issue #77: a public payment link is reachable only via its
	// registered workspace subdomain or a custom domain.
	//
	// Renamed slugs: this runs through the same live-first resolver the serving
	// path uses, so a submission posted to a form's OLD URL (a QR stand printed
	// before the rename, a page still open in a buyer's tab) reaches the right
	// form instead of 404ing. Attribution is by the resolved form, so the sale
	// lands on the form the buyer actually filled in.
	form := h.findPublicForm(r.Context(), r, slug)
	if form == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	h.submitResolvedForm(w, r, form)
}

// submitResolvedForm is everything a form-scoped submit does once the form is
// resolved: the draft/private/entry-limit gates, then the dispatch to the
// response collector, the menu checkout or the order core.
//
// It exists so the two submit routes cannot drift. handleSubmit resolves the
// form from the request Host (/{slug}/order); embedSubmit resolves it from the
// route's own {wsslug} path segment (/embed/f/{wsslug}/{slug}/order), which
// is the same scope its GET uses. Those lookups legitimately differ — a Host
// naming a workspace that holds no such form must 404 while the path-scoped
// embed URL serves — but the GATES and the DISPATCH must be identical, so they
// live here rather than in each handler.
func (h *PublicHandlers) submitResolvedForm(w http.ResponseWriter, r *http.Request, form *dbstore.Form) {
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
	if formMode(form) == dbstore.FormModeResponse {
		h.handleResponseSubmit(w, r, form)
		return
	}

	// Issue #497: a menu form's own URL is its order target. The menu is a
	// FORM now, so the checkout posts here (POST /{slug}/order) — the same
	// route every form uses — and the shared menu path attributes the sale to
	// the form and validates against the form's ATTACHED order form (if any),
	// never against the menu form's own product blocks.
	if menu := dbstore.MenuFromConfig(form.Config); menu != nil && menu.Enabled {
		h.submitMenuOrderForForm(w, r, form, menu)
		return
	}

	// Issue #491: the order core is shared with the menu checkout — the target
	// is a parameter. A form order keeps the historical rule (email required,
	// name optional), validates against the form's own registered-field
	// contract, and rate-limits per form.
	h.submitOrder(w, r, orderTarget{
		AccountID:   form.AccountID,
		Form:        form,
		FieldsForm:  form,
		RateKey:     form.ID,
		OwnerSkip:   h.antiSpamSubmitterIsOwner(r, form),
		RequireName: false,
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
	// Issue #254: server-side re-validation of registered fields. Agent JS is
	// client-side only and bypassable, so the server re-validates every field
	// against the form's registered contract before storing the submission.
	{
		strFields := make(map[string]string, len(in.Fields))
		for k, v := range in.Fields {
			switch t := v.(type) {
			case string:
				strFields[k] = t
			case float64:
				strFields[k] = fmt.Sprintf("%.0f", t)
			case bool:
				strFields[k] = fmt.Sprintf("%v", t)
			default:
				if b, err := json.Marshal(v); err == nil {
					strFields[k] = string(b)
				}
			}
		}
		if err := formfields.Validate(form.Config, strFields); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Issue #244: deterministic anti-spam layers on response-mode submits too.
	// The honeypot + render timestamp ride on the fields map (the renderer
	// emits them as named inputs). The per-form rate limit and time-based
	// check are skipped for a logged-in owner/admin of the form's workspace.
	owner := h.antiSpamSubmitterIsOwner(r, form)
	// Layer 1 — honeypot: a filled hidden field marks a bot.
	if v, ok := in.Fields[antiSpamHoneypotField]; ok {
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
			slog.Warn("form_honeypot_caught", "ip", middleware.ClientIP(r))
			writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
			return
		}
	}
	// Layer 5 — email/domain validation (format + MX) when an email is given.
	if in.Client.Email != "" && h.antiSpamCheckEmail(r, in.Client.Email) {
		writeErr(w, http.StatusBadRequest, "emel tidak sah")
		return
	}
	// Layer 2 — per-form rate limit (15/form/IP/hour), skipped for owner.
	if !owner && h.antiSpamCheckRateLimit(r, form) {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak cubaan. Cuba lagi kemudian.")
		return
	}
	// Layer 3 — time-based check (reject too-fast submits), skipped for owner.
	if !owner {
		if raw, ok := in.Fields[antiSpamRenderField]; ok {
			if s, isStr := raw.(string); isStr {
				if ts, perr := strconv.ParseInt(s, 10, 64); perr == nil && time.Since(time.Unix(ts, 0)) < antiSpamMinRenderTime {
					writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
					return
				}
			}
		}
	}
	// Issue #255: for a custom-HTML response form, only the registered fields
	// are read from the submitted data. Arbitrary HTML inputs (unregistered
	// field_ids) are dropped — the server never trusts the agent's HTML to
	// define what gets recorded. The anti-spam fields (honeypot + timestamp)
	// are consumed above and excluded from the stored submission.
	if customform.Enabled(form.Config) {
		cc := customform.FromConfig(form.Config)
		in.Fields = customform.Collect(cc, in.Fields)
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
	// Issue #245: enqueue a background AI spam-judge job for this submission.
	// The submission is accepted immediately; the judge flags it as spam in
	// the background (skipped for the workspace owner/admin testing their own
	// form). Never blocks the submit response.
	h.enqueueSpamJudge(r, form.AccountID, form, owner, sub.ID, "", in.Client.Email, in.Fields)
	// Issue #193: fire per-form notifications on response.submitted. The
	// submitted field values (block_id → value) drive conditional logic and
	// {field_id} recipients. Best-effort — never blocks the submit response.
	h.fireResponseNotifications(r.Context(), form, in.Fields)
	// Issue #447: push a response.submitted event to the merchant's connected
	// dashboards so an open page updates live (no manual refresh). Best-effort
	// — never blocks the submit response.
	if h.Realtime != nil {
		h.Realtime.Publish(form.AccountID, "response.submitted", map[string]any{
			"submission_id": sub.ID,
			"form_id":       form.ID,
			"buyer_email":   in.Client.Email,
		})
	}
	// Issue #179: resolve the form's confirmation against the submitted field
	// values and return it so the buyer-facing JS can render the message or
	// redirect. The default confirmation always matches.
	conf := h.resolveConfirmation(r.Context(), form, in.Fields)
	if conf.Confirmation.Type == dbstore.ConfirmationTypeRedirect && conf.RedirectURL != "" {
		respond(w, http.StatusCreated, map[string]string{
			"status":       "submitted",
			"id":           sub.ID,
			"confirmation": "redirect",
			"redirect_url": conf.RedirectURL,
		})
		return
	}
	respond(w, http.StatusCreated, map[string]string{
		"status":       "submitted",
		"id":           sub.ID,
		"confirmation": "message",
		"message":      conf.Message,
	})
}

// fireResponseNotifications evaluates a response form's notification rules for
// the response.submitted event and enqueues the matching emails.
func (h *PublicHandlers) fireResponseNotifications(ctx context.Context, form *dbstore.Form, fields map[string]any) {
	if h.Pay == nil || h.Pay.Notifications == nil {
		return
	}
	// Flatten submitted values to strings for conditional logic + placeholders.
	strFields := map[string]string{}
	for k, v := range fields {
		switch t := v.(type) {
		case string:
			strFields[k] = t
		case float64:
			strFields[k] = fmt.Sprintf("%.0f", t)
		case bool:
			strFields[k] = fmt.Sprintf("%v", t)
		default:
			if b, err := json.Marshal(v); err == nil {
				strFields[k] = string(b)
			}
		}
	}
	workspaceID := ""
	wsLang := ""
	if h.Workspaces != nil {
		if sid, serr := h.Workspaces.WorkspaceIDByAccount(ctx, form.AccountID); serr == nil {
			workspaceID = sid
		}
		if workspaceID != "" {
			if st, werr := h.Workspaces.Settings(ctx, workspaceID); werr == nil && st != nil {
				wsLang = st.WorkspaceLang()
			}
		}
	}
	formURL := ""
	if h.Workspaces != nil {
		// The notification's form link must be a host that SERVES the form; the
		// workspace-only lookup answered "" for signup-created accounts, and the
		// fallback below is the apex /f/{slug} redirect, not the form's own URL.
		if slug, serr := h.Workspaces.PublicHostSlugByAccount(ctx, form.AccountID); serr == nil && slug != "" {
			formURL = "https://" + slug + ".jomform.com/" + form.Slug
		}
	}
	if formURL == "" {
		formURL = h.BaseURL + "/f/" + form.Slug
	}
	h.Pay.FireFormNotifications(ctx, payments.NotificationRef{
		FormID:      form.ID,
		WorkspaceID: workspaceID,
		AccountID:   form.AccountID,
		FormURL:     formURL,
		// Issue #597: a response's default notification shows WHO responded, so
		// the responder's name is carried on BuyerName as well as on the fields
		// map below. BuyerName is the always-defined {{full_name}}/{{buyer_name}}
		// value, whereas a field value only exists when the submission actually
		// carried that block id.
		BuyerName: responderName(strFields),
		Fields:    strFields,
		// Language consistency: the BUYER-facing axis — the form's own
		// config.lang (which the page the responder just used was rendered in)
		// then the workspace default. This is the form row itself, so no extra
		// query is needed.
		FormLang:      dbstore.FormLangOf(form),
		WorkspaceLang: wsLang,
	}, dbstore.NotifyEventResponseSubmitted)
}

// responderName picks the responder's display name out of a response form's
// submitted field values (issue #597). The public submit path always mirrors
// the submitted client identity onto `full_name` (and `email`/`phone`), so a
// form with a name block resolves through the first key; a form that only
// collected an email falls back to it. Both empty is a legitimate submission —
// the caller then renders an empty name rather than a literal placeholder.
func responderName(fields map[string]string) string {
	for _, key := range []string{"full_name", "name", "email"} {
		if v := strings.TrimSpace(fields[key]); v != "" {
			return v
		}
	}
	return ""
}

// isProductRow reports whether productID is a real row in the account's
// products table. Pricing blocks (option/shipping) submit their own block id as
// product_id, which is never a product row, so inventory is skipped for them
// (issue #461). A lookup error (including a non-uuid id) is treated as "not a
// product row".
func (h *PublicHandlers) isProductRow(ctx context.Context, accountID, productID string) bool {
	if h.Products == nil || accountID == "" || productID == "" {
		return false
	}
	p, err := h.Products.Get(ctx, accountID, productID)
	return err == nil && p != nil
}

// lookupProduct resolves a submitted line item's name/price. A real product
// reference resolves LIVE from the products table (issue #461) so the server
// stays the price authority for the CURRENT product price, matching what the
// buyer sees on the form — the form config snapshot is only the fallback for an
// archived/deleted product. Pricing blocks (option/shipping, issue #173) carry
// their own price in the config and stay snapshot-based: they are not product
// references and must not be re-priced from the products table.
//
// accountID is passed explicitly and form may be nil (issue #491, menu
// checkout): a formless order has no snapshot, so it resolves live only and
// rejects an unknown product id.
func (h *PublicHandlers) lookupProduct(ctx context.Context, accountID string, form *dbstore.Form, productID string) (string, int64) {
	// 1. Live product row wins (active product in the account).
	if h.Products != nil && accountID != "" {
		if p, err := h.Products.Get(ctx, accountID, productID); err == nil && p != nil {
			return p.Name, p.PriceSen
		}
	}
	// 2. Fallback: the form's saved snapshot (archived/deleted product, or a
	// pricing block that carries its own price). No form → nothing to fall
	// back to.
	if form == nil {
		return "", 0
	}
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
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .NoIndex}}<meta name="robots" content="noindex, nofollow">{{end}}
<title>{{.Title}}</title>
{{.OGMeta}}
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
  .price { color:#4f46e5; font-weight:600; white-workspace:nowrap; margin-left:auto; text-align:right; }
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
  /* Issue #183: optional per-workspace footer. */
  .jf-footer { max-width:460px; width:100%; margin:16px auto 0; padding:0 8px;
               color:#6b7280; font-size:12px; line-height:1.6; text-align:center;
               align-self:center; }
  .jf-fbiz { font-weight:600; color:#374151; }
  .jf-ftext { margin-top:4px; }
  .jf-flinks { display:flex; width:100%; gap:14px; justify-content:center;
               flex-wrap:wrap; margin-top:6px; text-align:center; }
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
    <label class="sub">{{.Ls.NameLabel}}</label>
    <input type="text" name="full_name" required>
    <label class="sub">{{.Ls.EmailLabel}}</label>
    <input type="email" name="email" required>
    <label class="sub">{{.Ls.PhoneLabel}}</label>
    <input type="tel" name="phone">
    <div class="total"><span>{{.Ls.Total}}</span><span id="total">RM0.00</span></div>
    {{.AntiSpam}}
    <button type="submit">{{.Ls.Submit}}</button>
  </form>
  {{if .Footer.HasFooter}}<footer class="jf-footer">
    {{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
    {{if .Footer.ContactPhone}}<div class="jf-frow">{{.Footer.ContactPhone}}</div>{{end}}
    {{if .Footer.ContactAddress}}<div class="jf-frow">{{.Footer.ContactAddress}}</div>{{end}}
    {{if .Footer.FooterText}}<div class="jf-ftext">{{.Footer.FooterText}}</div>{{end}}
    {{if or .Footer.PrivacyURL .Footer.TermsURL .Footer.RefundURL}}<div class="jf-flinks">
      {{if .Footer.PrivacyURL}}<a href="{{.Footer.PrivacyURL}}" target="_blank" rel="noopener">{{.Ls.PrivacyLabel}}</a>{{end}}
      {{if .Footer.TermsURL}}<a href="{{.Footer.TermsURL}}" target="_blank" rel="noopener">{{.Ls.TermsLabel}}</a>{{end}}
      {{if .Footer.RefundURL}}<a href="{{.Footer.RefundURL}}" target="_blank" rel="noopener">{{.Ls.RefundLabel}}</a>{{end}}
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
  if (!items.length) { alert('{{.Ls.NoProductAlert}}'); return; }
  const body = {
    client: { full_name: e.target.full_name.value, email: e.target.email.value, phone: e.target.phone.value },
    items, payment_method: 'chip',
    fields: {
      website: (document.querySelector('[name=website]')||{}).value || '',
      jf_ts: (document.querySelector('[name=jf_ts]')||{}).value || ''
    },
    // Issue #489: QR ordering — echo the ?table=N the QR stand / menu page
    // carried into the form URL so sales.table_number is populated.
    table_number: (new URLSearchParams(location.search).get('table') || '')
  };
  const r = await fetch(location.pathname + '/order', {
    method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body)
  });
  const data = await r.json();
  if (data.checkout_url) { location.href = data.checkout_url; }
  else if (data.next) { location.href = data.next; }
  else { alert(data.error || '{{.Ls.ErrorAlert}}'); }
});
</script>
{{.HeightJS}}
</body></html>`))

type tmplProduct struct {
	ID       string
	Name     string
	PriceSen int64
}

// redirectToRenamedForm answers a rename's OLD URL with its temporary 301,
// carrying the request's own query string to the form's live URL.
//
// path is the CANONICAL, server-chosen path for the route that was hit
// ("/<live-slug>", "/f/<live-slug>"); it is always built from the form's stored
// slug, never from anything the caller sent. Only the query already present on
// the request is appended, so the destination is same-origin by construction:
// no caller-supplied host or scheme can reach the Location, and a query value
// that looks like an absolute URL stays a query value.
//
// The query MUST survive. A printed QR stand encodes ?table=N (qr.go) and an
// affiliate campaign link carries ?aff=CODE. menu.go reads both back off the
// request and records them on the sale (sales.table_number, from the QR stand
// the merchant printed for THAT table, and sales.affiliate_code, which is how
// the promoter is paid). http.Redirect writes the Location it is given verbatim
// and never re-appends r.URL.RawQuery, so a path-only Location silently drops
// the table number the stand exists to convey and the promoter's attribution
// with it — the sale is recorded, but attributed to nobody.
//
// RawQuery (not a re-encoded Query()) is carried deliberately: it is what the
// caller actually sent, so the percent-encoding round-trips unchanged.
func redirectToRenamedForm(w http.ResponseWriter, r *http.Request, path string) {
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}
	http.Redirect(w, r, path, http.StatusMovedPermanently)
}

func (h *PublicHandlers) handleRender(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	// Subdomain routing: wanzulhosting.jomform.com/f/test resolves the form
	// scoped to that account's slug. On bare jomform.com (no subdomain, no
	// custom domain) a published form is NOT served — issue #77: a public
	// payment link is reachable only via its registered workspace subdomain
	// ({workspace}.jomform.com/{slug}) or a custom domain. The legacy global
	// /f/{slug} lookup is gone.
	//
	// A rename's OLD URL is answered with a temporary 301 to the form's live
	// slug (same rule as the bare /{slug} route), so every URL already handed
	// out keeps resolving instead of 404-ing.
	if sub := accountSubdomain(r.Host); sub != "" {
		f, ferr := h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
		if ferr != nil || f == nil {
			// Fallback: a rename's temporary 301 behind this old slug.
			f, ferr = h.Forms.RedirectedFormByWorkspaceSlug(r.Context(), sub, slug)
		}
		if ferr != nil || f == nil {
			http.Error(w, "Borang tidak dijumpai untuk domain ini", http.StatusNotFound)
			return
		}
		if f.Slug != slug {
			// A rename's OLD URL: same-origin 301 to the live slug, with this
			// request's own query carried across (?table=N from a printed QR
			// stand, ?aff=CODE from an affiliate link) — see
			// redirectToRenamedForm.
			redirectToRenamedForm(w, r, "/f/"+f.Slug)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, ferr := h.Forms.ByAccountSlug(r.Context(), cd, slug)
		if ferr != nil || f == nil {
			f, ferr = h.Forms.RedirectedFormByAccountSlug(r.Context(), cd, slug)
		}
		if ferr != nil || f == nil {
			http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
			return
		}
		if f.Slug != slug {
			redirectToRenamedForm(w, r, "/f/"+f.Slug)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	// Bare apex (jomform.com): no workspace subdomain, no custom domain — 404.
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
	// Issue #461: resolve the rendered product name/price LIVE from the products
	// table (the config order still decides which products appear and in what
	// order). The snapshot is the fallback for an archived/deleted product.
	live := productMapByID(h.liveProductsForForm(r.Context(), f))
	var prods []tmplProduct
	for _, p := range cfg.Products {
		if p.ID == "" {
			continue
		}
		name, price := p.Name, p.PriceSen
		if lp, ok := live[p.ID]; ok {
			name, price = lp.Name, lp.PriceSen
		}
		prods = append(prods, tmplProduct{ID: p.ID, Name: name, PriceSen: price})
	}
	type view struct {
		Slug, Title, Description string
		Products                 []tmplProduct
		// Issue #411: Open Graph + Twitter meta for social sharing.
		OGMeta template.HTML
		// Issue #183: optional per-workspace footer.
		Footer footerInfo
		// Issue #244: hidden honeypot + render-timestamp fields.
		AntiSpam template.HTML
		// Issue #276: noindex on jomform.com subdomains / when the workspace opts
		// out of indexing.
		NoIndex bool
		// Issue #456: postMessage auto-height for the iframe embed. No-op on a
		// direct visit (the script only runs when framed). template.HTML so the
		// <script> block is emitted verbatim.
		HeightJS template.HTML
		// Language placement: the resolved buyer-facing language (per-form
		// config.lang ?? workspace default ?? "en") and its chrome strings. The
		// legacy layout used to be hardcoded Malay regardless of the workspace.
		Lang string
		Ls   langStrings
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ls := h.effectiveLangStrings(r.Context(), f)
	if err := formTmpl.Execute(w, view{
		Slug: f.Slug, Title: f.Title, Description: cfg.Description, Products: prods,
		OGMeta:   h.formOGMeta(r, f),
		Footer:   h.workspaceFooter(r.Context(), f),
		AntiSpam: template.HTML(antiSpamRenderFields()),
		NoIndex:  h.formNoIndex(r, f),
		HeightJS: template.HTML(iframeHeightJS),
		Lang:     ls.Lang,
		Ls:       ls,
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
	// Issue #179: resolve the form's confirmation against the submitted field
	// values. A redirect confirmation sends the buyer to the configured URL; a
	// message confirmation is rendered (HTML-sanitized) on this page. The
	// default confirmation always matches, so there is always a fallback.
	var conf *resolvedConfirmation
	var form *dbstore.Form
	if h.Forms != nil {
		if f, ferr := h.Forms.GetByID(r.Context(), sale.AccountID, sale.FormID); ferr == nil && f != nil {
			form = f
			fields, _ := h.Sales.SaleFields(r.Context(), sale.ID)
			conf = h.resolveConfirmation(r.Context(), form, fields)
		}
	}
	if conf != nil && conf.Confirmation.Type == dbstore.ConfirmationTypeRedirect && conf.RedirectURL != "" {
		http.Redirect(w, r, conf.RedirectURL, http.StatusFound)
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
	// Language placement: the thank-you page belongs to a workspace, so its
	// chrome follows the workspace's language (there is no form override on this
	// path when the form cannot be resolved). Previously this page was hardcoded
	// Malay, so an English workspace still showed BM to its buyers.
	ls := h.effectiveLangStringsByAccount(r.Context(), sale.AccountID)
	if form != nil {
		ls = h.effectiveLangStrings(r.Context(), form)
	}
	statusText := ls.StatusPending
	switch sale.Status {
	case dbstore.SalePaid:
		statusText = ls.StatusPaid
	case dbstore.SaleConfirmed:
		statusText = ls.StatusConfirmed
	case dbstore.SaleFailed:
		statusText = ls.StatusFailed
	case dbstore.SaleCancelled:
		statusText = ls.StatusCancelled
	}
	// The confirmation message (sanitized) replaces the hardcoded thank-you
	// text. Fall back to the default message when none resolved.
	// Issue #393: on a failed payment, show the merchant's per-form failure
	// message (HTML-sanitized) instead of the success confirmation. When the
	// merchant has not set one, fall back to the buyer-facing default.
	message := dbstore.DefaultConfirmationMessage
	if sale.Status == dbstore.SaleFailed {
		message = dbstore.DefaultFailureMessage
		if form != nil && form.FailureMessage != "" {
			message = confirmationSanitizer.Sanitize(form.FailureMessage)
		}
	} else if conf != nil && conf.Message != "" {
		message = conf.Message
	}
	// Issue #537: raw fmt.Fprintf has no autoescaping. The values here are
	// server-generated today, but they ride through the same escape helper so the
	// sink cannot become an XSS if a caller-supplied value is ever added.
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8">%s
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<meta name="jf-ref" content="%s">
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:420px}
h1{color:#10b981;font-size:24px}
.msg{color:#17181c;font-size:15px;line-height:1.6;margin-top:8px}
.msg a{color:#4f46e5}
.ref{color:#9ca3af;font-size:12px;margin-top:12px}</style></head><body><div class="c">
<h1>✓ %s</h1><div class="msg">%s</div>
<p class="ref">%s <strong>%s</strong> · %s: %s.</p>
<p style="color:#9ca3af;font-size:12px">%s</p></div></body></html>`,
		htmlEsc(ls.Lang), refresh, htmlEsc(ls.ThankYou), htmlEsc(sale.RefCode), htmlEsc(ls.ThankYou), message,
		htmlEsc(ls.OrderLabel), htmlEsc(sale.RefCode), htmlEsc(ls.StatusLabel), htmlEsc(statusText), htmlEsc(ls.PoweredBy))
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

// footerInfo is the optional per-workspace public-form footer (issue #183). Only
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

// workspaceFooter resolves the optional footer for a form's workspace. Returns a
// footerInfo with HasFooter=false when the workspace is unresolvable or the
// merchant set nothing.
func (h *PublicHandlers) workspaceFooter(ctx context.Context, f *dbstore.Form) footerInfo {
	var fi footerInfo
	if h.Workspaces == nil {
		return fi
	}
	workspaceID, err := h.Workspaces.WorkspaceIDByAccount(ctx, f.AccountID)
	if err != nil || workspaceID == "" {
		return fi
	}
	st, err := h.Workspaces.Settings(ctx, workspaceID)
	if err != nil {
		return fi
	}
	// Issue #231: the merchant's stored PrivacyURL/TermsURL/RefundURL may
	// point at a stale/wrong workspace subdomain (e.g. wanzulhosting.jomform.com
	// when the workspace is actually "wan"). When a link targets one of the
	// workspace's own hosted legal pages ({workspace}.jomform.com/privacy|terms|refund),
	// rewrite it to the CURRENT workspace's subdomain so it always resolves.
	slug, _ := h.Workspaces.WorkspaceSlugByAccount(ctx, f.AccountID)
	fi.BusinessName = st.BusinessName
	fi.CompanyReg = st.CompanyReg
	fi.ContactPhone = st.ContactPhone
	fi.ContactAddress = st.ContactAddress
	fi.PrivacyURL = rewriteHostedLegalURL(st.PrivacyURL, slug)
	fi.TermsURL = rewriteHostedLegalURL(st.TermsURL, slug)
	fi.RefundURL = rewriteHostedLegalURL(st.RefundURL, slug)
	fi.FooterText = st.FooterText
	fi.HasFooter = fi.BusinessName != "" || fi.CompanyReg != "" || fi.ContactPhone != "" ||
		fi.ContactAddress != "" || fi.PrivacyURL != "" || fi.TermsURL != "" || fi.RefundURL != "" ||
		fi.FooterText != ""
	return fi
}

// rewriteHostedLegalURL rewrites a stored legal-page URL that points at one of
// the workspace's own hosted pages ({workspace}.jomform.com/privacy|terms|refund) to
// the current workspace's subdomain. External URLs (any other host) are returned
// unchanged. A stale slug in the stored URL (issue #231) is corrected so the
// footer link always resolves to the current workspace.
func rewriteHostedLegalURL(raw, workspaceSlug string) string {
	if raw == "" || workspaceSlug == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	host := u.Hostname()
	if !strings.HasSuffix(host, ".jomform.com") {
		return raw
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path != dbstore.LegalPagePrivacy && path != dbstore.LegalPageTerms && path != dbstore.LegalPageRefund {
		return raw
	}
	u.Host = workspaceSlug + ".jomform.com"
	return u.String()
}

// formNoIndex reports whether a public form must be noindexed (issue #276).
// Policy:
//   - jomform.com subdomains ({workspace}.jomform.com) are ALWAYS noindex —
//     privacy: payment links shouldn't appear on Google.
//   - custom domains are indexed only when the workspace's indexing toggle is on.
//   - bare jomform.com apex never serves forms (issue #77), so it's moot.
func (h *PublicHandlers) formNoIndex(r *http.Request, f *dbstore.Form) bool {
	if accountSubdomain(r.Host) != "" {
		return true
	}
	if customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host) != "" {
		if h.Workspaces == nil {
			return false
		}
		idx, err := h.Workspaces.IndexingByAccount(r.Context(), f.AccountID)
		if err != nil {
			return false
		}
		return !idx
	}
	return true
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
	// Issue #497: the restaurant menu is a PER-FORM setting. When the form's
	// config carries an enabled menu, the form's OWN public URL renders the
	// menu page (products from the form's own product blocks) instead of the
	// normal form layout. The workspace root is untouched by this — it renders
	// the landing page / form listing as before. Non-menu forms fall straight
	// through to the normal renderers below, completely unaffected.
	if menu := dbstore.MenuFromConfig(f.Config); menu != nil && menu.Enabled {
		h.renderFormMenu(w, r, f, menu)
		return
	}
	if formMode(f) == dbstore.FormModeResponse {
		// Issue #255: a custom-HTML response form (agent-authored body in a
		// sandboxed iframe) takes precedence over the block renderer.
		if h.renderCustomForm(w, r, f) {
			return
		}
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

// handleFormUnlock verifies the submitted password for a form resolved from the
// request HOST — the canonical (host-scoped) private-form unlock. An embedded
// form cannot use it (an embed's Host is the merchant's domain), so the embed
// route resolves by PATH and shares everything after that; see embedUnlock.
func (h *PublicHandlers) handleFormUnlock(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	// Same live-first resolution as the serving path, so a buyer whose gate page
	// was loaded before a rename can still unlock (see findPublicForm). The
	// resolver returns nil on a miss (unknown slug, bare apex, h.Forms == nil),
	// and unlockResolvedForm reads f.Status, so the miss is answered here — a
	// bogus unlock must be a 404, not a panic recovered into a 500.
	f := h.findPublicForm(r.Context(), r, slug)
	if f == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
	h.unlockResolvedForm(w, r, f)
}

// unlockResolvedForm is everything a private-form unlock does once the form is
// resolved: the private-status gate, the argon2id password check and the signed
// unlock cookie. It exists so the canonical unlock (POST /{slug}/unlock, host
// scope) and the embed unlock (POST /embed/f/{wsslug}/{slug}/unlock, path
// scope) cannot drift — the same reason submitResolvedForm is shared by the two
// submit routes. Only the LOOKUP differs between them, and it differs because it
// must.
func (h *PublicHandlers) unlockResolvedForm(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	if f.Status != dbstore.FormStatusPrivate {
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
	http.SetCookie(w, newFormAccessCookie(token))
	respond(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

// newFormAccessCookie builds the signed cookie that unlocks a private form.
//
// SameSite=None + Partitioned (CHIPS), NOT Lax.
//
// A private form can be embedded on the MERCHANT's own site, so the gate's
// fetch is a cross-site subresource request from a frame whose "site for
// cookies" is the empty string — every ancestor document is cross-site, so the
// frame is cross-site with its own target (draft-ietf-httpbis-cookie-same-site
// §2.1.1, and Set-Cookie/SameSite on MDN: Lax is sent only on same-site requests
// and cross-site TOP-LEVEL navigations). A Lax cookie is therefore neither
// STORED from such a response nor SENT on such a request, so the gate re-rendered
// forever. Measured in this browser against a cross-site iframe: a Lax cookie set
// by the frame's fetch was not stored at all, while a SameSite=None control was
// stored AND sent on the frame's next request.
//
// A plain None cookie would fix that half but is exactly the third-party cookie
// Chrome is restricting, so the cookie is marked Partitioned to opt into CHIPS —
// double-keyed by the top-level site, which is the mechanism browsers provide for
// an embedded widget's own state (MDN: "Cookies Having Independent Partitioned
// State (CHIPS)"). It also keeps an unlock scoped to the ONE merchant page it was
// entered on instead of being shared across every site the form is embedded on.
// A browser that predates CHIPS ignores the unknown attribute and keeps a plain
// SameSite=None cookie, so this is never worse than None alone.
//
// BOTH attributes require Secure, so they are emitted only where Secure is set
// (i.e. not dev). Browsers that enforce the pairing refuse SameSite=None without
// Secure, and a partitioned cookie MUST carry Secure, so emitting either on a
// plain-http dev host would produce a cookie the browser silently discards. Dev
// keeps Lax, which is correct there: a dev host is same-site with itself, so the
// gate's own fetch is same-site and needs no relaxation.
//
// The value is the signed "<formID>|<expiry>|<hmac>" token
// (signFormAccessToken), never the password.
func newFormAccessCookie(token string) *http.Cookie {
	c := &http.Cookie{
		Name:     formAccessCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7, // 7 days
	}
	if !isDev {
		c.SameSite = http.SameSiteNoneMode
		c.Partitioned = true
	}
	return c
}

// renderPrivateGate renders the password gate screen for a private form.
//
// The gate posts to its OWN address (location.pathname + "/unlock"), exactly as
// the submit button posts to its own address + "/order". It used to hardcode
// fetch('/<slug>/unlock') — the CANONICAL, host-scoped route — which is the same
// path/route drift #722 fixed for the submit button, and it is what made an
// embedded private form permanently locked: an embed is served on the MERCHANT's
// origin, so its gate must post to the embed scope
// (/embed/f/{wsslug}/{slug}/unlock), where the request Host never names the
// workspace. Every route that can render this gate therefore needs its matching
// POST .../unlock registered: /{slug} and /f/{slug} (host scope) and
// /embed/f/{wsslug}/{slug} (path scope).
func (h *PublicHandlers) renderPrivateGate(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Language placement: the gate must speak the same language as the form
	// behind it — it was hardcoded Malay, so a workspace set to English showed
	// a BM password prompt to its buyers.
	ls := h.effectiveLangStrings(r.Context(), f)
	// Issue #537: raw fmt.Fprintf has no html/template autoescaping, so every
	// merchant-controlled value must be escaped for the context it lands in.
	// f.Title is HTML text; anything interpolated into the fetch() below is a JS
	// single-quoted string literal, so it needs JS-context escaping — HTML
	// escaping alone would still allow a `'` to break out of the string. (The
	// fetch target is the page's own pathname since the embed fix, so no
	// merchant value reaches that literal at all; the language strings still do.)
	fmt.Fprintf(w, `<!doctype html><html lang="%s"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s · %s</title>
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:400px;width:100%%}
h1{font-size:20px;margin:0 0 4px}.m{color:#6b7280;font-size:14px;margin:0 0 20px}
input{width:100%%;box-sizing:border-box;padding:10px 12px;border:1px solid #d1d5db;border-radius:8px;font-size:14px;margin-bottom:12px}
button{width:100%%;background:#5e6ad2;color:#fff;border:0;border-radius:8px;padding:11px;font-size:14px;font-weight:600;cursor:pointer}
.err{color:#dc2626;font-size:13px;margin:10px 0 0;min-height:18px}
.f{color:#9ca3af;font-size:12px;margin-top:20px}</style></head><body><div class="c">
<h1>🔒 %s</h1><p class="m">%s</p>
<form id="g" onsubmit="return unlock(event)">
<input type="password" id="pw" placeholder="%s" autocomplete="current-password" required>
<button type="submit">%s</button>
<p class="err" id="err"></p>
</form><p class="f">%s</p></div>
<script>
async function unlock(e){e.preventDefault();var p=document.getElementById('pw').value;
var r=await fetch(location.pathname+'/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:p})});
if(r.ok){location.reload();}else{var d=await r.json().catch(function(){return{};});document.getElementById('err').textContent=d.error||'%s';}
return false;}
</script></body></html>`,
		htmlEsc(ls.Lang), htmlEsc(f.Title), htmlEsc(ls.UnlockTitle), htmlEsc(f.Title), htmlEsc(ls.UnlockHelp),
		htmlEsc(ls.UnlockAction), htmlEsc(ls.UnlockAction), htmlEsc(ls.PoweredBy),
		jsSingleQuoted(ls.UnlockError))
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

// customDomainOwner returns the account slug that claimed this host via a
// verified custom domain, or "" when the host is not a registered custom
// domain. Resolution order (issue #236):
//  1. account_domains table (multi-domain, migration 000007) — the current
//     storage for custom domains.
//  2. Legacy accounts.custom_domain column — backward compat for accounts
//     that claimed a single domain before multi-domain support.
func customDomainOwner(ctx context.Context, s *dbstore.AccountStore, d *dbstore.DomainStore, host string) string {
	h := host
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	h = strings.ToLower(h)
	// Multi-domain: map the verified domain to its account, then to the slug.
	if d != nil {
		if dom, err := d.ByDomain(ctx, h); err == nil && dom != nil {
			acc, err := s.ByID(ctx, dom.AccountID)
			if err == nil && acc != nil {
				return acc.Slug
			}
		}
	}
	// Legacy single-domain fallback.
	slug, err := s.SlugByCustomDomain(ctx, h)
	if err != nil {
		return ""
	}
	return slug
}

// handleWorkspaceRootForm serves a form by slug on a WORKSPACE subdomain
// (e.g. wanzulhosting.jomform.com/form-baru) or a verified custom domain
// (issue #297). On jomform.com it 404s so dashboard routes are unaffected.
// Private forms (issue #176) resolve here so their password gate can render;
// serveForm enforces the status rules.
//
// Renamed slugs: the OLD URL of a renamed form is handled deliberately, not
// 404-with-no-trace. resolutionOrder below is the rule — the live form is asked
// first, and a rename's temporary 301 is only consulted as a fallback.
func (h *PublicHandlers) handleWorkspaceRootForm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// Same lookup rules as render/submit (#77): subdomain scoping first,
	// then a verified custom domain's owning account. On bare jomform.com
	// (no subdomain, no custom domain) a published form is NOT served —
	// a public payment link is reachable only via its registered workspace
	// subdomain or a custom domain (issue #297).
	f := h.findPublicForm(r.Context(), r, slug)
	if f == nil {
		http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
		return
	}
	// The request arrived on a slug this form no longer serves: it is a rename's
	// OLD URL, resolved through the temporary 301 fallback. Answer deliberately
	// with a redirect to the form's live slug rather than serving the form under
	// a URL that is not its own — a buyer who bookmarks what he sees must land
	// on the canonical address. Temporary (301 with an expiring table row, see
	// FormStore.RenameSlug), not a permanent alias.
	//
	// The query travels with it: this is the route a printed QR stand points at
	// (the stand encodes ?table=N into the form's URL), so dropping the query
	// here would land the buyer on the right menu with the table number gone.
	// See redirectToRenamedForm.
	if f.Slug != slug {
		redirectToRenamedForm(w, r, "/"+f.Slug)
		return
	}
	h.serveForm(w, r, f)
}

// findPublicForm resolves a form on the request's host, LIVE FIRST and a
// rename's temporary 301 second, returning nil when neither matches. It writes
// nothing — the caller decides what a resolution means (serve it, redirect to
// its canonical slug, or process a submission).
//
// THE ORDER IS THE RULE (Wan, 2026-09-20: "301 sementara. kalau ada form lain yg
// claim, maka url tu akan ke form yang baru"): every live lookup runs BEFORE the
// redirect table is consulted, so a form that later claims an old slug wins — it
// is found first — and a rename's history can never shadow a real, live form.
// That is also why the redirect table needs no expiry sweep and no reservation:
// an expired or superseded row is simply never reached, and the old URL 404s
// like any other unknown slug.
//
// Every public path that resolves a form BY SLUG goes through here, so a rename
// cannot orphan a URL already handed out — a printed QR stand, a shared link, or
// a menu's order_form_slug reference (#491), which resolves by slug and not by
// id. A submission arriving on the old URL is therefore attributed to the right
// form instead of being dropped.
func (h *PublicHandlers) findPublicForm(ctx context.Context, r *http.Request, slug string) *dbstore.Form {
	if h.Forms == nil {
		return nil
	}
	var sub, customDomain string
	if s := accountSubdomain(r.Host); s != "" {
		sub = s
	} else if cd := customDomainOwner(ctx, h.Accounts, h.Domains, r.Host); cd != "" {
		customDomain = cd
	}
	if sub == "" && customDomain == "" {
		// Bare apex: no workspace subdomain, no custom domain. A public
		// payment link is not served here (issue #77), and neither is a
		// rename's redirect — there is no workspace to scope it to.
		return nil
	}

	// 1. The live form, by the same scoping the serving path has always used.
	//
	// The store error MUST be honoured, not discarded. The two stores disagree
	// on what a miss looks like: ByWorkspaceSlug returns (nil, err), but
	// ByAccountSlug scans into a pre-allocated struct and returns it non-nil
	// ALONGSIDE its error. Discarding err therefore hands the custom-domain
	// branch a non-nil *Form with an EMPTY Slug, which read as "found a form
	// whose slug is ''" — /{slug} redirected every such request to the bare
	// workspace root instead of 404-ing, and the rename's 301 to the live slug
	// was never reached at all. Every sibling caller already checks the error
	// (order.go, menu.go); only this one did not.
	var f *dbstore.Form
	var err error
	if sub != "" {
		f, err = h.Forms.ByWorkspaceSlug(ctx, sub, slug)
	} else {
		f, err = h.Forms.ByAccountSlug(ctx, customDomain, slug)
	}
	if err == nil && f != nil {
		return f
	}

	// 2. Fallback: a rename left a temporary 301 behind this slug.
	//
	// Same rule as above, for the same reason: scanOne also returns its
	// pre-allocated struct non-nil when the SCAN fails for any reason other
	// than "no rows" (a column-alignment slip is the realistic one — see
	// redirectFormColumns), and a zero-value Form read as a real hit would
	// redirect to "/". Only a row that actually decoded counts as a hit.
	var rf *dbstore.Form
	if sub != "" {
		rf, err = h.Forms.RedirectedFormByWorkspaceSlug(ctx, sub, slug)
	} else {
		rf, err = h.Forms.RedirectedFormByAccountSlug(ctx, customDomain, slug)
	}
	if err != nil {
		return nil
	}
	return rf
}

// handleWorkspaceRoot serves the workspace storefront root: a customized landing page
// when the merchant has configured one (issue #360), otherwise the auto
// listing. It handles both a workspace subdomain ({workspace}.jomform.com — issue #77)
// and a verified custom domain mapped to a workspace's account (issue #286). Bare
// jomform.com (no subdomain, no custom domain) 404s so dashboard routes are
// unaffected. The lookup is by workspace slug for the subdomain and by account
// slug for a custom domain (they are different namespaces).
func (h *PublicHandlers) handleWorkspaceRoot(w http.ResponseWriter, r *http.Request) {
	var slug string
	var byAccount bool
	if sub := accountSubdomain(r.Host); sub != "" {
		slug = sub
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		slug = cd
		byAccount = true
	} else {
		http.NotFound(w, r)
		return
	}
	var forms []*dbstore.Form
	var err error
	if byAccount {
		forms, err = h.Forms.ListPublishedByAccountSlug(r.Context(), slug)
	} else {
		forms, err = h.Forms.ListPublishedByWorkspace(r.Context(), slug)
	}
	if err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
		return
	}

	// Issue #360: when the workspace has a landing page configured, render it
	// instead of the bare listing. Resolution differs by namespace: the
	// subdomain lookup uses the workspace slug, the custom-domain lookup uses the
	// account slug -> resolve the owning workspace, read its settings.
	if h.Workspaces != nil {
		var workspaceID string
		if byAccount {
			if acc, aerr := h.Accounts.BySlug(r.Context(), slug); aerr == nil && acc != nil {
				workspaceID, _ = h.Workspaces.WorkspaceIDByAccount(r.Context(), acc.ID)
			}
		} else {
			workspaceID, _ = h.Workspaces.IDBySlug(r.Context(), slug)
		}
		if workspaceID != "" {
			if st, serr := h.Workspaces.Settings(r.Context(), workspaceID); serr == nil &&
				st.LandingPage != nil && st.LandingPage.Enabled {
				// Issue #497: the restaurant menu is a PER-FORM setting, served
				// at the form's own URL — it no longer hijacks the workspace
				// root. A legacy landing_page_json.menu is ignored here (and
				// everywhere else) rather than rendered, so a stray blob from
				// #438 can never blank the merchant's storefront again.
				h.renderWorkspaceLanding(w, r, slug, forms, st.LandingPage)
				return
			}
		}
	}

	// Fallback: the auto-generated bare listing (existing behavior).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, "<!doctype html><html><head><meta charset='utf-8'><title>"+slug+"</title>")
	io.WriteString(w, "<style>body{font-family:system-ui;background:#f7f8f8;margin:0;padding:40px}")
	io.WriteString(w, ".g{max-width:640px;margin:0 auto;display:grid;gap:12px}")
	io.WriteString(w, ".g a{background:#fff;border:1px solid #e6e8ec;border-radius:14px;padding:18px 20px;")
	io.WriteString(w, "color:#17181c;text-decoration:none;font-weight:600;display:block}")
	io.WriteString(w, "</style></head><body><div class='g'><h1 style='font-size:22px'>Kedai "+slug+"</h1>")
	if len(forms) == 0 {
		io.WriteString(w, "<p>Belum ada borang.</p>")
	}
	for _, f := range forms {
		io.WriteString(w, "<a href='/"+f.Slug+"'>"+f.Title+"</a>")
	}
	io.WriteString(w, "</div></body></html>")
}

// workspaceLandingData is the template payload for the customizable storefront
// root (issue #360).
type workspaceLandingData struct {
	Slug string
	Lp   *dbstore.LandingPage
	// HasForms reports whether at least one published form exists (used to
	// decide the "no forms yet" hint behind the hero).
	HasForms bool
	// Embeds are the published forms configured to render inline (issue #362).
	// Only slugs that resolve to a published form in the workspace are included;
	// unknown/draft/private slugs are dropped server-side so they never render.
	Embeds []landingEmbedView
}

// landingEmbedView is one inline-embedded form on the landing page (issue
// #362). The form is framed same-origin at /{slug} so it reuses the full
// render + validation + submit path of a direct visit (fields render, anti
// spam applies, submissions land in the same form's submissions/sales).
type landingEmbedView struct {
	Label    string
	FormSlug string
}

// landingPageTmpl renders the customizable workspace landing page (issue #360).
// It is SEO-friendly server-side HTML and reuses the same layout language as
// the public form renderer. All dynamic text is HTML-escaped by template
// autoescaping.
var landingPageTmpl = template.Must(template.New("workspace-landing").Parse(`<!doctype html>
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
  .wrap { max-width:860px; margin:0 auto; padding:48px 24px 72px; }
  .hero { padding:24px 0 8px; }
  .hero h1 { margin:0 0 10px; font-size:40px; line-height:1.15; letter-spacing:-.02em;
             color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}}; }
  .hero .sub { margin:0; font-size:18px; color:#6b7280; }
  .logo { max-width:120px; max-height:80px; object-fit:contain; margin-bottom:16px;
          display:{{if .Lp.LogoURL}}block{{else}}none{{end}}; }
  .banner { width:100%; max-height:420px; object-fit:cover; border-radius:20px;
            margin:28px 0 0; display:{{if .Lp.ImageURL}}block{{else}}none{{end}}; }
  .body { margin-top:28px; font-size:17px; line-height:1.7; color:#3f434a;
          white-workspace:pre-wrap; }
  .ctas { display:flex; flex-wrap:wrap; gap:12px; margin-top:28px; }
  .cta { background:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#4f46e5{{end}}; color:#fff;
         font-weight:600; text-decoration:none; padding:14px 26px; border-radius:12px;
         display:inline-block; transition:opacity .15s ease, transform .15s ease; }
  .cta:hover { opacity:.9; transform:translateY(-1px); }
  .hint { margin-top:16px; color:#9ca3af; font-size:14px; }
  /* Issue #362: inline-embedded published forms (e.g. RSVP). */
  .embeds { margin-top:36px; display:flex; flex-direction:column; gap:36px; }
  .embed { display:block; }
  .embed h2 { margin:0 0 12px; font-size:20px; color:{{if .Lp.PrimaryColor}}{{.Lp.PrimaryColor}}{{else}}#17181c{{end}}; }
  .embed iframe { width:100%; height:720px; border:0; border-radius:16px;
                  background:#ffffff; box-shadow:0 1px 3px rgba(23,24,28,0.08); }
</style>
</head>
<body>
  <div class="wrap">
    <div class="hero">
      <img class="logo" src="{{.Lp.LogoURL}}" alt="">
      <h1>{{.Lp.Hero}}</h1>
      <p class="sub">{{.Lp.Subtitle}}</p>
    </div>
    <img class="banner" src="{{.Lp.ImageURL}}" alt="{{.Lp.Hero}}">
    {{if .Lp.Body}}<div class="body">{{.Lp.Body}}</div>{{end}}
    <div class="ctas">
      {{range .Lp.CTAs}}<a class="cta" href="/{{.FormSlug}}">{{.Label}}</a>{{end}}
    </div>
    {{if not .HasForms}}<p class="hint">Belum ada borang.</p>{{end}}
    {{if .Embeds}}<div class="embeds">
      {{range .Embeds}}<div class="embed">
        <h2>{{.Label}}</h2>
        <iframe src="/{{.FormSlug}}" title="{{.Label}}" loading="lazy"
                allow="payment" referrerpolicy="no-referrer-when-downgrade"></iframe>
      </div>{{end}}
    </div>{{end}}
  </div>
</body>
</html>`))

// renderWorkspaceLanding writes the customized landing page (issue #360).
func (h *PublicHandlers) renderWorkspaceLanding(w http.ResponseWriter, r *http.Request, slug string, forms []*dbstore.Form, lp *dbstore.LandingPage) {
	// Issue #362: resolve the embedded-form blocks against the workspace's
	// published forms. Only slugs that match a published form are rendered;
	// draft/private/unknown slugs are dropped server-side. Embeds are
	// framed same-origin at /{slug} so they reuse the full form render +
	// validation + submit path, keeping server authority on form semantics.
	data := workspaceLandingData{
		Slug:     slug,
		Lp:       lp,
		HasForms: len(forms) > 0,
		Embeds:   resolveLandingEmbeds(lp.Forms, forms),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := landingPageTmpl.Execute(w, data); err != nil {
		http.Error(w, "ralat", http.StatusInternalServerError)
	}
}

// resolveLandingEmbeds filters the landing page's configured embedded-form
// blocks down to the slugs that resolve to a published form in the workspace
// (issue #362). Slugs that don't match a published form are dropped so they
// never render on the public page.
func resolveLandingEmbeds(configured []dbstore.LandingEmbed, published []*dbstore.Form) []landingEmbedView {
	bySlug := map[string]bool{}
	for _, f := range published {
		bySlug[f.Slug] = true
	}
	var embeds []landingEmbedView
	for _, em := range configured {
		if bySlug[em.FormSlug] {
			embeds = append(embeds, landingEmbedView{Label: em.Label, FormSlug: em.FormSlug})
		}
	}
	return embeds
}

// requestingAccountID resolves the account that owns the requesting workspace
// subdomain or verified custom domain (issue #379). It returns "" when the
// host is not a workspace/custom domain or the owning account cannot be resolved.
// The subdomain namespace is the workspace slug (issue #158: each workspace owns its
// own account), so the account is resolved via the workspace; the custom-domain
// namespace is the account slug directly.
func requestingAccountID(ctx context.Context, accounts *dbstore.AccountStore, domains *dbstore.DomainStore, workspaces *dbstore.WorkspaceStore, host string) string {
	if sub := accountSubdomain(host); sub != "" {
		if workspaces == nil {
			return ""
		}
		workspaceID, err := workspaces.IDBySlug(ctx, sub)
		if err != nil || workspaceID == "" {
			return ""
		}
		acc, err := accounts.ByWorkspaceID(ctx, workspaceID)
		if err != nil || acc == nil {
			return ""
		}
		return acc.ID
	}
	if cd := customDomainOwner(ctx, accounts, domains, host); cd != "" {
		acc, err := accounts.BySlug(ctx, cd)
		if err != nil || acc == nil {
			return ""
		}
		return acc.ID
	}
	return ""
}

// assetOwnedByRequestingTenant reports whether the object key's account
// matches the account that owns the requesting workspace/domain (issue #379).
// A key whose account segment is missing or differs from the requesting
// tenant is rejected so a merchant can never serve another account's asset
// on their own domain.
func assetOwnedByRequestingTenant(key, requestingAccountID string) bool {
	if requestingAccountID == "" {
		return false
	}
	return accountIDFromKey(key) == requestingAccountID
}

// handleAsset serves GET /assets/{key...} on a workspace/custom domain — merchant
// code assets (CSS/JS/HTML/fonts) referenced same-origin by a customised
// landing/workspace page (issue #361). The route only resolves on a workspace
// subdomain ({workspace}.jomform.com) or a verified custom domain mapped to a
// workspace's account, so uploaded code is tenant-scoped: it is never exposed on
// the bare app origin and never executable server-side (served as static
// bytes with the restrictive hardening in handleMedia).
//
// The key is the same account-scoped `u/...` key generated at upload, so it
// can never resolve to an object outside the uploading account's workspace. The
// stored content-type is preserved and restrictive headers (nosniff, no
// framing/embedding) are applied exactly as on /media.
//
// Issue #379: the requesting workspace/domain's account is resolved and the
// asset key's account must match it — a cross-tenant key (another account's
// uploaded code) is rejected with 404 even if the requesting tenant is a
// valid workspace/custom domain.
func (h *PublicHandlers) handleAsset(w http.ResponseWriter, r *http.Request) {
	if storeUnavailable(h.Store) {
		writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
		return
	}
	// Tenant-scope check: only a workspace subdomain or a custom domain mapped to
	// a workspace serves assets. Bare jomform.com (no subdomain) 404s so dashboard
	// routes are unaffected.
	requestingAccount := requestingAccountID(r.Context(), h.Accounts, h.Domains, h.Workspaces, r.Host)
	if requestingAccount == "" {
		http.NotFound(w, r)
		return
	}
	key := mediaKeyFromPath(r.PathValue("key"))
	if key == "" {
		writeErr(w, http.StatusBadRequest, "kunci media tidak sah")
		return
	}
	// Issue #379: the asset must belong to the requesting tenant. A key whose
	// account differs from the requesting workspace/domain's account is rejected
	// (404) so a merchant cannot serve another account's code asset.
	if !assetOwnedByRequestingTenant(key, requestingAccount) {
		http.NotFound(w, r)
		return
	}
	body, ct, err := h.Store.GetObject(r.Context(), key)
	if err != nil {
		// Same one-meaning/one-status rule as handleMedia: a store that
		// reports itself unconfigured is a deployment gap (503), never a 500.
		if errors.Is(err, objstore.ErrNotConfigured) {
			writeErr(w, http.StatusServiceUnavailable, "media storage tidak dikonfigurasi")
			return
		}
		var nf *types.NoSuchKey
		if errors.As(err, &nf) {
			writeErr(w, http.StatusNotFound, "media tidak dijumpai")
			return
		}
		slog.Error("asset_get", "err", err, "key", key)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Issue #364: code-asset scan gate. A not-yet-scanned, needs-review, or
	// quarantined code asset is never served (404). Images are unaffected.
	if !scanGateAllowed(r.Context(), h.Media, key, ct) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
