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
	Workspaces            *dbstore.WorkspaceStore
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
	Sessions     *auth.Sessions
	Users        *dbstore.UserStore
	WorkspaceMembers *dbstore.WorkspaceMemberStore
	MXChecker    *email.MXChecker
	FormLimiter  *middleware.RateLimiter

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
	mux.HandleFunc("GET /embed/f/{accslug}/{slug}", h.embedForm)
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
	mux.HandleFunc("POST /{slug}/unlock", h.handleFormUnlock)
}

// genRef produces a short opaque reference code.
func genRef() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "JF-" + hex.EncodeToString(b)
}

// formMode returns the form's collection mode from its config: "payment"
// (default, current behaviour) or "response" (collect-only, no payment).
func formMode(f *dbstore.Form) string {
	if f == nil || f.Config == nil {
		return "payment"
	}
	if raw, err := json.Marshal(f.Config); err == nil {
		var cfg struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(raw, &cfg) == nil && cfg.Mode == "response" {
			return "response"
		}
	}
	return "payment"
}

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
	if h.FormLimiter == nil {
		return false
	}
	key := form.ID + "|" + middleware.ClientIP(r)
	return !h.FormLimiter.Allow(key)
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
// owner/admin (testing their own form) or when no queue is wired.
func (h *PublicHandlers) enqueueSpamJudge(r *http.Request, form *dbstore.Form, owner bool, subID, saleID, emailAddr string, data map[string]any) {
	if owner || h.SpamQueue == nil {
		return
	}
	job := spamjudge.Job{
		SubmissionID: subID,
		SaleID:       saleID,
		AccountID:    form.AccountID,
		FormID:       form.ID,
		FormTitle:    form.Title,
		Data:         data,
		Email:        emailAddr,
		IP:           middleware.ClientIP(r),
	}
	if err := spamjudge.Enqueue(r.Context(), h.SpamQueue, job); err != nil {
		slog.Warn("spamjudge_enqueue_failed", "account", form.AccountID, "err", err)
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
	var form *dbstore.Form
	var err error
	if sub := accountSubdomain(r.Host); sub != "" {
		form, err = h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
	} else if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		form, err = h.Forms.ByAccountSlug(r.Context(), cd, slug)
	} else {
		form, err = nil, dbstore.ErrFormNotFound
	}
	if err != nil || form == nil {
		writeErr(w, http.StatusNotFound, "borang tidak dijumpai")
		return
	}
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
	if formMode(form) == "response" {
		h.handleResponseSubmit(w, r, form)
		return
	}

	var in submitInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Client.Email == "" || len(in.Items) == 0 {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	// Issue #254: server-side re-validation of registered fields. Agent JS is
	// client-side only and bypassable, so the server re-validates every field
	// against the form's registered contract before accepting the order.
	if err := formfields.Validate(form.Config, in.Fields); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Issue #244: deterministic anti-spam layers. Each layer REJECTS the
	// submission (returns an error) rather than flagging it. The per-form
	// rate limit and time-based check are skipped for a logged-in owner/admin
	// of the form's workspace (owner testing the form shouldn't be throttled).
	owner := h.antiSpamSubmitterIsOwner(r, form)
	// Layer 1 — honeypot: a filled hidden field marks a bot.
	if h.antiSpamCheckHoneypot(r, &in) {
		writeErr(w, http.StatusBadRequest, "butiran tidak lengkap")
		return
	}
	// Layer 5 — email/domain validation (format + MX).
	if h.antiSpamCheckEmail(r, in.Client.Email) {
		writeErr(w, http.StatusBadRequest, "emel tidak sah")
		return
	}
	// Layer 2 — per-form rate limit (15/form/IP/hour), skipped for owner.
	if !owner && h.antiSpamCheckRateLimit(r, form) {
		writeErr(w, http.StatusTooManyRequests, "terlalu banyak cubaan. Cuba lagi kemudian.")
		return
	}
	// Layer 3 — time-based check (reject too-fast submits), skipped for owner.
	if !owner && h.antiSpamCheckTime(r, &in) {
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
		respondErr(w, r, err)
		return
	}

	sale := &dbstore.Sale{
		AccountID: form.AccountID,
		FormID:    form.ID,
		RefCode:   genRef(),
		Client: resolveChipClient(form, in.Fields, dbstore.SaleClient{
			FullName: in.Client.FullName, Email: in.Client.Email, Phone: in.Client.Phone,
			StreetAddress: in.Client.StreetAddress, City: in.Client.City, State: in.Client.State,
			ZipCode: in.Client.ZipCode, Country: in.Client.Country,
			LegalName: in.Client.LegalName, RegistrationNum: in.Client.RegistrationNum,
			TaxNumber: in.Client.TaxNumber,
		}),
		SaleType:      dbstore.SaleTypeOneTime,
		PaymentMethod: method,
		Status:        dbstore.SalePending,
		SkipCapture:   in.SkipCapture && method == "chip",
		// Issue #438: QR ordering flow — carry the table number the QR stand
		// encoded so the merchant knows where to serve.
		TableNumber: strings.TrimSpace(in.TableNumber),
		// Issue #444: affiliate marketing — carry the affiliate code the buyer
		// landed with so the sale is attributed to that affiliate.
		AffiliateCode: strings.TrimSpace(in.AffiliateCode),
	}
	// Issue #185: record the currency the sale is transacted in from the
	// workspace that owns the form's account (default MYR). Historical sales keep
	// this original currency — never auto-converted on a later change.
	if h.Workspaces != nil {
		if cur, cerr := h.Workspaces.WorkspaceCurrencyByAccount(r.Context(), form.AccountID); cerr == nil && cur != "" {
			sale.Currency = cur
		}
	}
	if sale.Currency == "" {
		sale.Currency = "MYR"
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
		item := dbstore.SaleItem{ProductID: it.ProductID, Name: name, PriceSen: price, Qty: qty}
		// Issue #153: when the buyer chose a variation, validate it belongs to
		// the product, use the variation's price, and decrement the variation's
		// stock (not the product-level stock). The variation is persisted on the
		// sale so the receipt/merchant view shows which variation was bought.
		if it.VariationID != "" {
			if h.ProductVariations == nil {
				writeErr(w, http.StatusBadRequest, "variasi tidak sah: "+it.VariationID)
				return
			}
			v, verr := h.ProductVariations.GetByProduct(r.Context(), it.ProductID, it.VariationID)
			if verr != nil {
				if verr == dbstore.ErrVariationNotFound {
					writeErr(w, http.StatusBadRequest, "variasi tidak sah: "+it.VariationID)
					return
				}
				respondErr(w, r, verr)
				return
			}
			item.PriceSen = v.PriceSen
			item.VariationID = v.ID
			item.VariationName = v.Name
			// Inventory: limited-stock variations decrement atomically; SOLD OUT rejects.
			if err := h.ProductVariations.DecrementStock(r.Context(), v.ID, int(qty)); err != nil {
				if err == dbstore.ErrOutOfStock {
					writeErr(w, http.StatusConflict, "stok habis untuk "+name+" ("+v.Name+")")
					return
				}
				respondErr(w, r, err)
				return
			}
		} else if h.Products != nil && h.isProductRow(r.Context(), form.AccountID, it.ProductID) {
			// Simple product: decrement product-level stock atomically. Pricing
			// blocks (option/shipping) submit with product_id = the block's own
			// id, which is not a product row — skip inventory for them (issue
			// #461; before, the non-uuid block id blew up the stock query).
			if err := h.Products.DecrementStock(r.Context(), it.ProductID, int(qty)); err != nil {
				if err == dbstore.ErrOutOfStock {
					writeErr(w, http.StatusConflict, "stok habis untuk "+name)
					return
				}
				respondErr(w, r, err)
				return
			}
		}
		sale.Items = append(sale.Items, item)
		subtotal += item.PriceSen * qty
	}
	sale.SubtotalSen = subtotal
	// Issue #99 money invariant: total_sen = subtotal + shipping - discount.
	// The submit path currently has no shipping/discount inputs, so total
	// equals subtotal; the formula is centralized in payments.SumItems so the
	// invariant is enforced and unit-tested in one place.
	sale.TotalSen = payments.SumItems(subtotal, 0, 0)

	if err := h.Sales.Create(r.Context(), sale); err != nil {
		respondErr(w, r, err)
		return
	}
	// Issue #179: persist the submitted field values on the sale so the
	// thank-you page can evaluate confirmation conditional logic against them.
	if len(in.Fields) > 0 {
		fm := make(map[string]any, len(in.Fields))
		for k, v := range in.Fields {
			fm[k] = v
		}
		_ = h.Sales.SetSaleFields(r.Context(), sale.ID, fm)
	}
	// Issue #245: enqueue a background AI spam-judge job for this sale. The
	// sale is accepted immediately; the judge flags it as spam in the
	// background (skipped for the workspace owner/admin testing their own form).
	// Never blocks the submit response.
	judgeData := make(map[string]any, len(in.Fields))
	for k, v := range in.Fields {
		judgeData[k] = v
	}
	h.enqueueSpamJudge(r, form, owner, "", sale.ID, in.Client.Email, judgeData)

	// CHIP flow: create purchase & return checkout URL.
	if method == "chip" {
		// Issue #208: resolve the CHIP feed for this submission. A form's
		// chip_feeds are evaluated in order against the submitted field
		// values; the first matching feed (default feed = no condition
		// always matches) supplies the brand/secret. When no feed matches,
		// fall back to the legacy single per-form override, then account.
		var ov *payments.ChipOverride
		if h.Forms != nil {
			if feeds, ferr := h.Forms.ChipFeedsFor(r.Context(), form.AccountID, form.ID); ferr == nil {
				if feed := resolveChipFeed(feeds, in.Fields); feed != nil && feed.BrandID != "" && feed.SecretKeyEnc != "" {
					ov = &payments.ChipOverride{
						BrandID: feed.BrandID, SecretEnc: feed.SecretKeyEnc,
						EmailFallback: feed.EmailFallback, MethodWhitelist: feed.MethodWhitelist,
					}
				}
			}
			if ov == nil {
				if b, s, ef, wl, oerr := h.Forms.ChipOverrideFor(r.Context(), form.AccountID, form.ID); oerr == nil && b != "" && s != "" {
					ov = &payments.ChipOverride{BrandID: b, SecretEnc: s, EmailFallback: ef, MethodWhitelist: wl}
				}
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
	h.enqueueSpamJudge(r, form, owner, sub.ID, "", in.Client.Email, in.Fields)
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
	if h.Workspaces != nil {
		if sid, serr := h.Workspaces.WorkspaceIDByAccount(ctx, form.AccountID); serr == nil {
			workspaceID = sid
		}
	}
	formURL := ""
	if h.Workspaces != nil {
		if slug, serr := h.Workspaces.WorkspaceSlugByAccount(ctx, form.AccountID); serr == nil && slug != "" {
			formURL = "https://" + slug + ".jomform.com/" + form.Slug
		}
	}
	if formURL == "" {
		formURL = h.BaseURL + "/f/" + form.Slug
	}
	h.Pay.FireFormNotifications(ctx, payments.NotificationRef{
		FormID:    form.ID,
		WorkspaceID:   workspaceID,
		AccountID: form.AccountID,
		FormURL:   formURL,
		Fields:    strFields,
	}, dbstore.NotifyEventResponseSubmitted)
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
func (h *PublicHandlers) lookupProduct(form *dbstore.Form, productID string) (string, int64) {
	// 1. Live product row wins (active product in the form's account).
	if h.Products != nil && form != nil && form.AccountID != "" {
		if p, err := h.Products.Get(context.Background(), form.AccountID, productID); err == nil && p != nil {
			return p.Name, p.PriceSen
		}
	}
	// 2. Fallback: the form's saved snapshot (archived/deleted product, or a
	// pricing block that carries its own price).
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
<html lang="ms">
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
    <label class="sub">Nama penuh</label>
    <input type="text" name="full_name" required>
    <label class="sub">Emel</label>
    <input type="email" name="email" required>
    <label class="sub">No. telefon</label>
    <input type="tel" name="phone">
    <div class="total"><span>Jumlah</span><span id="total">RM0.00</span></div>
    {{.AntiSpam}}
    <button type="submit">Teruskan ke Pembayaran</button>
  </form>
  {{if .Footer.HasFooter}}<footer class="jf-footer">
    {{if .Footer.BusinessName}}<div class="jf-fbiz">{{.Footer.BusinessName}}{{if .Footer.CompanyReg}} · {{.Footer.CompanyReg}}{{end}}</div>{{end}}
    {{if .Footer.ContactPhone}}<div class="jf-frow">{{.Footer.ContactPhone}}</div>{{end}}
    {{if .Footer.ContactAddress}}<div class="jf-frow">{{.Footer.ContactAddress}}</div>{{end}}
    {{if .Footer.FooterText}}<div class="jf-ftext">{{.Footer.FooterText}}</div>{{end}}
    {{if or .Footer.PrivacyURL .Footer.TermsURL .Footer.RefundURL}}<div class="jf-flinks">
      {{if .Footer.PrivacyURL}}<a href="{{.Footer.PrivacyURL}}" target="_blank" rel="noopener">Polisi Privasi</a>{{end}}
      {{if .Footer.TermsURL}}<a href="{{.Footer.TermsURL}}" target="_blank" rel="noopener">Terma &amp; Syarat</a>{{end}}
      {{if .Footer.RefundURL}}<a href="{{.Footer.RefundURL}}" target="_blank" rel="noopener">Polisi Bayaran Balik</a>{{end}}
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
  if (!items.length) { alert('Pilih sekurang-kurangnya satu produk'); return; }
  const body = {
    client: { full_name: e.target.full_name.value, email: e.target.email.value, phone: e.target.phone.value },
    items, payment_method: 'chip',
    fields: {
      website: (document.querySelector('[name=website]')||{}).value || '',
      jf_ts: (document.querySelector('[name=jf_ts]')||{}).value || ''
    }
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
{{.HeightJS}}
</body></html>`))

type tmplProduct struct {
	ID       string
	Name     string
	PriceSen int64
}

func (h *PublicHandlers) handleRender(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	// Subdomain routing: wanzulhosting.jomform.com/f/test resolves the form
	// scoped to that account's slug. On bare jomform.com (no subdomain, no
	// custom domain) a published form is NOT served — issue #77: a public
	// payment link is reachable only via its registered workspace subdomain
	// ({workspace}.jomform.com/{slug}) or a custom domain. The legacy global
	// /f/{slug} lookup is gone.
	if sub := accountSubdomain(r.Host); sub != "" {
		f, ferr := h.Forms.ByWorkspaceSlug(r.Context(), sub, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai untuk domain ini", http.StatusNotFound)
			return
		}
		h.serveForm(w, r, f)
		return
	}
	if cd := customDomainOwner(r.Context(), h.Accounts, h.Domains, r.Host); cd != "" {
		f, ferr := h.Forms.ByAccountSlug(r.Context(), cd, slug)
		if ferr != nil {
			http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
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
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := formTmpl.Execute(w, view{
		Slug: f.Slug, Title: f.Title, Description: cfg.Description, Products: prods,
		OGMeta:    h.formOGMeta(r, f),
		Footer:    h.workspaceFooter(r.Context(), f),
		AntiSpam:  template.HTML(antiSpamRenderFields()),
		NoIndex:   h.formNoIndex(r, f),
		HeightJS:  template.HTML(iframeHeightJS),
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
	fmt.Fprintf(w, `<!doctype html><html lang="ms"><head><meta charset="utf-8">%s
<meta name="viewport" content="width=device-width,initial-scale=1"><title>Terima kasih</title>
<meta name="jf-ref" content="%s">
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:420px}
h1{color:#10b981;font-size:24px}
.msg{color:#17181c;font-size:15px;line-height:1.6;margin-top:8px}
.msg a{color:#4f46e5}
.ref{color:#9ca3af;font-size:12px;margin-top:12px}</style></head><body><div class="c">
<h1>✓ Terima kasih!</h1><div class="msg">%s</div>
<p class="ref">Tempahan <strong>%s</strong> · Status: %s.</p>
<p style="color:#9ca3af;font-size:12px">Dikuasakan oleh JomForm</p></div></body></html>`,
		refresh, sale.RefCode, message, sale.RefCode, statusText)
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
	if formMode(f) == "response" {
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

// handleFormUnlock verifies the submitted password against the form's stored
// argon2id hash (constant-time) and, on success, sets a signed cookie that
// unlocks the form for this browser. Wrong passwords are rejected.
func (h *PublicHandlers) handleFormUnlock(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || f == nil || f.Status != dbstore.FormStatusPrivate {
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
	http.SetCookie(w, &http.Cookie{
		Name:     formAccessCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   !isDev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 7, // 7 days
	})
	respond(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

// renderPrivateGate renders the password gate screen for a private form.
func (h *PublicHandlers) renderPrivateGate(w http.ResponseWriter, r *http.Request, f *dbstore.Form) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="ms"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<style>body{background:#f7f8f8;color:#17181c;font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}
.c{background:#ffffff;border:1px solid #e6e8ec;border-radius:16px;padding:40px;text-align:center;max-width:400px;width:100%%}
h1{font-size:20px;margin:0 0 4px}.m{color:#6b7280;font-size:14px;margin:0 0 20px}
input{width:100%%;box-sizing:border-box;padding:10px 12px;border:1px solid #d1d5db;border-radius:8px;font-size:14px;margin-bottom:12px}
button{width:100%%;background:#5e6ad2;color:#fff;border:0;border-radius:8px;padding:11px;font-size:14px;font-weight:600;cursor:pointer}
.err{color:#dc2626;font-size:13px;margin:10px 0 0;min-height:18px}
.f{color:#9ca3af;font-size:12px;margin-top:20px}</style></head><body><div class="c">
<h1>🔒 %s</h1><p class="m">Borang ini dilindungi kata laluan.</p>
<form id="g" onsubmit="return unlock(event)">
<input type="password" id="pw" placeholder="Kata laluan" autocomplete="current-password" required>
<button type="submit">Buka kunci</button>
<p class="err" id="err"></p>
</form><p class="f">Dikuasakan oleh JomForm</p></div>
<script>
async function unlock(e){e.preventDefault();var p=document.getElementById('pw').value;
var r=await fetch('/%s/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:p})});
if(r.ok){location.reload();}else{var d=await r.json().catch(function(){return{};});document.getElementById('err').textContent=d.error||'Kata laluan salah';}
return false;}
</script></body></html>`,
		f.Title, f.Title, f.Slug)
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
func (h *PublicHandlers) handleWorkspaceRootForm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	// Same lookup rules as render/submit (#77): subdomain scoping first,
	// then a verified custom domain's owning account. On bare jomform.com
	// (no subdomain, no custom domain) a published form is NOT served —
	// a public payment link is reachable only via its registered workspace
	// subdomain or a custom domain (issue #297).
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
		http.Error(w, "Borang tidak dijumpai", http.StatusNotFound)
		return
	}
	h.serveForm(w, r, f)
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
				// Issue #438: QR ordering flow — when the landing page has a
				// restaurant-menu template enabled, render the clean menu
				// (categories, images, prices, best-seller) instead of the
				// generic landing page.
				if st.LandingPage.Menu != nil && st.LandingPage.Menu.Enabled {
					h.renderWorkspaceMenu(w, r, slug, workspaceID, st.LandingPage)
					return
				}
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
	if h.Store == nil {
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
