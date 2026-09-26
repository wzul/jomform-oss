// Workspace management: list/create/switch + per-workspace settings.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// listWorkspaces: GET /v1/workspaces — workspaces owned by OR a member of the current
// user (issue #162). Each row carries the caller's role so the frontend can
// gate UI on it.
func (h *RESTHandlers) listWorkspaces(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	workspaces, err := h.Workspaces.ByMember(r.Context(), userID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	type row struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	out := make([]row, 0, len(workspaces))
	for _, s := range workspaces {
		out = append(out, row{ID: s.WorkspaceID, Name: s.Name, Slug: s.Slug, Role: s.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

// createWorkspace: POST /v1/workspaces
func (h *RESTHandlers) createWorkspace(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return
	}
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.Slug == "" {
		writeErr(w, http.StatusBadRequest, "name dan slug wajib")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	// Reserved subdomains (t_586ff0c3): a slug becomes a live host on the tenant
	// wildcard, so names JomForm owns or must protect (infra, routed products,
	// support/billing-looking) cannot be claimed. The list and the refusal live
	// in ONE place — internal/urlguard, the same package the scheme rule uses —
	// so this and the MCP validator cannot drift into two copies. The message
	// says WHY the name is reserved, and the stable `code` lets the dashboard
	// render its own localised sentence instead of printing this prose (#694).
	//
	// This runs FIRST, before the length and charset rules. Order is not
	// cosmetic: a reserved label can be shorter than the 3-char minimum ("s3"
	// is 2), and if the length rule won, the merchant would be told their
	// input was malformed and would try variations of the same sensitive name
	// instead of learning the name is JomForm's. Policy refusal beats shape
	// refusal.
	if urlguard.ReservedSubdomain(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": urlguard.ReservedSubdomainMessage(slug),
			"code":  urlguard.ReservedSubdomainCode,
		})
		return
	}
	if len(slug) < 3 || len(slug) > 40 {
		writeErr(w, http.StatusBadRequest, "slug 3-40 aksara")
		return
	}
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			writeErr(w, http.StatusBadRequest, "slug: a-z, 0-9, dash sahaja")
			return
		}
	}
	if strings.HasSuffix(slug, "jomform") {
		writeErr(w, http.StatusBadRequest, "slug tidak sah")
		return
	}
	// Ask-first onboarding (Wan's decision, 2026-09-20): a signup creates an
	// account and NO workspace, and a user with zero workspaces is asked to
	// create one before they can reach any create surface. When this is that
	// user's FIRST workspace, link the account the signup already made instead of
	// letting Workspaces.Create insert a second one (issue #158) — otherwise the
	// user ends up owning two accounts, the signup one left with workspace_id
	// NULL and reachable by nothing.
	//
	// The reuse is deliberately narrow: the account must be the caller's ONLY
	// account, unlinked, and carry no CONTENT (forms/products/sales). A legacy
	// workspace-less account that already holds content is a data-migration
	// decision (its published forms resolve through the account slug —
	// PublicHostSlugByAccount) and is left alone here.
	//
	// An API key is NOT content and does not disqualify the account: it has no
	// public host slug and no published URL, CreateForAccount carries it into the
	// new workspace, and every key-holding user must keep the one-account
	// behaviour. See dbstore.WorkspaceStore.SoleEmptyUnscopedAccount for why.
	if prior, _ := h.Workspaces.ByMember(r.Context(), userID); len(prior) == 0 {
		if accID, err := h.Workspaces.SoleEmptyUnscopedAccount(r.Context(), userID); err == nil && accID != "" {
			sp, err := h.Workspaces.CreateForAccount(r.Context(), userID, strings.TrimSpace(in.Name), slug, accID)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23505" {
					writeErr(w, http.StatusConflict, "slug sudah digunakan")
					return
				}
				respondErr(w, r, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]string{
				"id": sp.ID, "name": sp.Name, "slug": sp.Slug,
				"url": "https://" + sp.Slug + ".jomform.com",
			})
			return
		}
	}

	sp, err := h.Workspaces.Create(r.Context(), userID, strings.TrimSpace(in.Name), slug)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "slug sudah digunakan")
			return
		}
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id": sp.ID, "name": sp.Name, "slug": sp.Slug,
		"url": "https://" + sp.Slug + ".jomform.com",
	})
}

// workspaceAccessByID resolves a workspace by id and verifies the caller is the owner
// or a member (issue #162). Returns the workspace + the caller's role.
func (h *RESTHandlers) workspaceAccessByID(w http.ResponseWriter, r *http.Request) (*dbstore.Workspace, string, bool) {
	sID := r.PathValue("id")
	if _, perr := uuid.Parse(sID); perr != nil {
		writeErr(w, http.StatusBadRequest, "id tidak sah")
		return nil, "", false
	}
	userID := h.sessionOwnerID(r)
	if userID == "" {
		writeErr(w, http.StatusUnauthorized, "auth diperlukan")
		return nil, "", false
	}
	s, err := h.Workspaces.ByID(r.Context(), sID)
	if err != nil || s == nil {
		writeErr(w, http.StatusNotFound, "workspace tidak dijumpai")
		return nil, "", false
	}
	acc, ok, err := h.Workspaces.AccessByID(r.Context(), sID, userID)
	if err != nil || !ok {
		writeErr(w, http.StatusForbidden, "bukan workspace anda")
		return nil, "", false
	}
	return s, acc.Role, true
}

// ── Delete a workspace ───────────────────────────────────────────────────────────

// deleteWorkspace: DELETE /v1/workspaces/{id}
//
// Two invariants, both enforced in the store inside one transaction:
//
//  1. A user always has AT LEAST ONE workspace. The guard counts the remaining
//     workspaces the caller owns-or-is-a-member-of rather than flagging a
//     particular "original" workspace, so whichever workspace is last is the one
//     that is protected — and a member of someone else's workspace may still
//     delete their own last one, because they still have somewhere to land.
//  2. The workspace must be EMPTY. A refusal lists every blocking category with
//     its count (forms — including trashed ones, products, sales, custom
//     domains, webhooks, live API keys, active subscriptions) so the merchant
//     knows exactly what to clear. Sales in particular may never be destroyed by
//     this call: they are trading history.
//
// OWNER ONLY. An admin member manages the workspace but does not own its public
// identity, its trading history, or the CHIP account that holds its credentials;
// they leave through the existing member-removal path instead.
//
// The confirmation is the workspace SLUG, not a boolean, on both the REST and the
// MCP path (same precedent as revoke_api_key's confirm=true): a destructive,
// irreversible call should require the caller to name what it is destroying.
func (h *RESTHandlers) deleteWorkspace(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	// The API-key write scope is the credential axis; it is checked before
	// anything else so a readonly key cannot even probe the guards.
	if !h.requireWriteScope(w, actx) {
		return
	}
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if role != dbstore.RoleAdmin {
		writeErr(w, http.StatusForbidden, "hanya pemilik workspace boleh memadamnya")
		return
	}
	userID := h.sessionOwnerID(r)
	if userID == "" || s.Owner != userID {
		writeErr(w, http.StatusForbidden, "hanya pemilik workspace boleh memadamnya")
		return
	}
	// Typed confirmation: {"confirm_slug":"<slug>"}. A missing or mismatching
	// slug is refused — the point is that the caller names the workspace.
	var in struct {
		ConfirmSlug string `json:"confirm_slug"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	if strings.TrimSpace(in.ConfirmSlug) != s.Slug {
		writeErr(w, http.StatusBadRequest,
			"taip slug workspace ("+s.Slug+") untuk sahkan pemadaman")
		return
	}
	deleted, err := h.Workspaces.Delete(r.Context(), s.ID, userID, actx.KeyID)
	if err != nil {
		var de *dbstore.WorkspaceDeleteError
		if errors.As(err, &de) {
			switch de.Reason {
			case "last_workspace":
				writeErr(w, http.StatusBadRequest,
					"anda perlu sekurang-kurangnya satu workspace — cipta workspace baharu dahulu")
			default:
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":    "workspace tidak kosong — kosongkan dahulu sebelum memadam",
					"code":     CodeWorkspaceNotEmpty,
					"blockers": de.Blockers,
				})
			}
			return
		}
		if errors.Is(err, dbstore.ErrWorkspaceNotFound) {
			writeErr(w, http.StatusNotFound, "workspace tidak dijumpai")
			return
		}
		respondErr(w, r, err)
		return
	}
	// Audit: names the workspace and what went with it. Recorded AFTER the
	// delete, and NOT against actx.Account: for an unscoped session request the
	// resolved account IS the account this call just removed, so attributing the
	// event to it made Postgres refuse the insert (events_account_id_fkey,
	// SQLSTATE 23503) — swallowed by recordEvent's best-effort path, so the
	// delete answered 200 and left no audit trail. The event is attributed to an
	// account that survives instead.
	if acct := h.auditAccountAfterDelete(r, userID, actx.Account.ID); acct != "" {
		h.recordEventFor(r, acct, userID, "workspace.deleted", map[string]any{
			"workspace_id": s.ID, "slug": s.Slug, "name": s.Name, "deleted": deleted,
		})
	} else {
		// The delete removed the caller's only account (an account-less
		// workspace beside it) — there is no account left to file the event
		// under, and writing it blind would be the FK failure again. Say so
		// rather than dropping it silently.
		slog.Warn("workspace_deleted_event_skipped",
			"workspace", s.ID, "slug", s.Slug, "user", userID,
			"reason", "no surviving account to attribute the event to")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "deleted", "id": s.ID, "slug": s.Slug, "deleted": deleted,
	})
}

// workspaceDeletionCheck: GET /v1/workspaces/{id}/deletion-check
//
// The read half of the delete path, for the UI's Danger zone. It reports the
// SAME emptiness definition the delete enforces (one query, shared), plus
// whether this is the caller's last workspace, so the button can be disabled
// with a stated reason instead of failing after it is pressed.
func (h *RESTHandlers) workspaceDeletionCheck(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	userID := h.sessionOwnerID(r)
	_, blockers, err := h.Workspaces.CountEmpty(r.Context(), s.ID, actx.KeyID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	if blockers == nil {
		blockers = []dbstore.WorkspaceBlocker{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": s.ID, "name": s.Name, "slug": s.Slug,
		"is_owner":       userID != "" && s.Owner == userID,
		"role":           role,
		"last_workspace": h.isLastWorkspace(r, s.ID, userID),
		"empty":          len(blockers) == 0,
		"blockers":       blockers,
		"can_delete":     userID != "" && s.Owner == userID && len(blockers) == 0 && !h.isLastWorkspace(r, s.ID, userID),
	})
}

// auditAccountAfterDelete picks the account a post-delete event must be
// attributed to. Events are account-scoped and `events.account_id` is a FK to
// `accounts(id)`, so an event about a workspace delete cannot be written against
// an account the delete just destroyed.
//
// The resolved account is used when it is one the caller OWNS and still has —
// for the scoped request shape (`?workspace=<one of the caller's own
// workspaces>`) that resolved account is a different, surviving workspace's
// account of the caller's, so it is the most faithful attribution. Only when the
// resolved account is gone (the unscoped shape, which resolves to the account of
// the workspace being deleted) or is not the caller's at all does this fall back
// to another account the caller still has.
//
// OWNERSHIP, not reachability. The scope that produced `resolved` is resolved
// through Workspaces.AccessBySlug / AccessByID (resolveSessionAccount), and
// WorkspaceStore.access admits owner OR MEMBER — a member reaches a stranger's
// workspace and resolves to the STRANGER's account, which survives the call. An
// existence-only test therefore accepts it, and the caller's own
// workspace.deleted row is written into the stranger's account-scoped audit log:
// the stranger reads the caller's delete out of their own audit log while the
// caller cannot see it. Attribution must never leave the caller's own tenancy,
// and a stranger's account is not a tenancy to file our events in merely because
// the request resolved to it.
//
// The fallback is likewise restricted to accounts the caller OWNS. It must NOT
// walk Workspaces.ByMember: that list is "owned OR JOINED", so for a caller who
// merely members a stranger's workspace the stranger's account is a candidate —
// and if it sorts first (ByMember orders by workspace created_at), the same
// cross-tenant leak follows.
//
// "" means there is none; the caller then skips the event and says so.
func (h *RESTHandlers) auditAccountAfterDelete(r *http.Request, userID, resolved string) string {
	ctx := r.Context()
	if h.ownsAccount(ctx, userID, resolved) {
		return resolved
	}
	if userID == "" {
		return ""
	}
	if h.Accounts != nil {
		if accs, err := h.Accounts.ByOwner(ctx, userID); err == nil {
			for _, a := range accs {
				if a != nil && a.ID != "" && h.accountExists(ctx, a.ID) {
					return a.ID
				}
			}
		}
	}
	return ""
}

// ownsAccount reports whether accountID is an account the caller OWNS and that
// still names a row.
//
// Ownership is the test, not mere accessibility: a ?workspace=<slug|id> scope
// resolves for owners AND members alike, so "this request resolved to that
// account" is evidence of REACHABILITY and nothing more. Reading ownership off
// Accounts.ByOwner is also what makes the surviving-row check free — ByOwner
// lists live rows, so a match cannot name a row the delete just removed.
//
// A store error is treated as "not owned": the caller then falls back rather
// than filing its event in a tenancy it cannot prove.
func (h *RESTHandlers) ownsAccount(ctx context.Context, userID, accountID string) bool {
	if accountID == "" || userID == "" || h.Accounts == nil {
		return false
	}
	accs, err := h.Accounts.ByOwner(ctx, userID)
	if err != nil {
		return false
	}
	for _, a := range accs {
		if a != nil && a.ID == accountID {
			return true
		}
	}
	return false
}

// accountExists reports whether an account id still names a row. A store error
// is treated as "does not exist": the caller then picks another account rather
// than gambling that the row survives and re-arming the FK violation.
func (h *RESTHandlers) accountExists(ctx context.Context, accountID string) bool {
	if accountID == "" || h.Accounts == nil {
		return false
	}
	a, err := h.Accounts.ByID(ctx, accountID)
	return err == nil && a != nil
}

// isLastWorkspace reports whether deleting workspaceID would leave userID with
// no workspace at all (owned or joined). Shared by the delete guard's read half
// so the UI and the write agree.
func (h *RESTHandlers) isLastWorkspace(r *http.Request, workspaceID, userID string) bool {
	if userID == "" {
		return true
	}
	list, err := h.Workspaces.ByMember(r.Context(), userID)
	if err != nil {
		// Fail CLOSED: if we cannot prove the user has another workspace, treat
		// this as the last one and do not invite the delete.
		return true
	}
	for _, w := range list {
		if w.WorkspaceID != workspaceID {
			return false
		}
	}
	return true
}

// getWorkspaceSettings GET/PUT for the storefront builder.
func (h *RESTHandlers) getWorkspaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Workspaces.Settings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveWorkspaceSettings: PUT /v1/workspaces/{id}/settings
func (h *RESTHandlers) saveWorkspaceSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		FooterText     string `json:"footer_text"`
		CompanyReg     string `json:"company_reg"`
		PrivacyURL     string `json:"privacy_url"`
		TermsURL       string `json:"terms_url"`
		RefundURL      string `json:"refund_url"`
		ContactPhone   string `json:"contact_phone"`
		ContactAddress string `json:"contact_address"`
		BusinessName   string `json:"business_name"`
		MenuJSON       string `json:"menu_json"`
		DefaultLang    string `json:"default_lang"`
		Currency       string `json:"currency"`
		// Issue #276: per-workspace SEO indexing toggle. Omitted (nil) = keep
		// current value; explicit true/false sets it.
		Indexing *bool `json:"indexing"`
		// Issue #360: customizable landing-page config. Omitted (nil) = keep
		// current value; provided = replace. Enabled=false (or empty) falls
		// back to the auto-generated bare listing.
		LandingPage *dbstore.LandingPage `json:"landing_page"`
		// Issue #447: per-workspace realtime notification sound toggle.
		// Omitted (nil) = keep current value; explicit true/false sets it.
		SoundEnabled *bool `json:"sound_enabled"`
		// Per-workspace AI spam-judge toggle. Omitted (nil) =
		// keep current value; explicit true/false sets it. Default is true.
		SpamJudgeEnabled *bool `json:"spam_judge_enabled"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	// Issue #501: ONE shared scheme rule for every caller-supplied URL field
	// (see internal/urlguard). The landing-page image_url/logo_url are
	// rendered into buyer-facing `<img src>` attributes (public.go), so an
	// unsafe scheme must be rejected on this path too — previously only the
	// MCP landing tool validated them.
	for _, f := range []struct{ name, u string }{
		{"privacy_url", in.PrivacyURL},
		{"terms_url", in.TermsURL},
		{"refund_url", in.RefundURL},
	} {
		if !urlguard.Valid(f.u) {
			writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage(f.name))
			return
		}
	}
	if in.LandingPage != nil {
		for _, f := range []struct{ name, u string }{
			{"image_url", in.LandingPage.ImageURL},
			{"logo_url", in.LandingPage.LogoURL},
		} {
			if !urlguard.Valid(f.u) {
				writeErr(w, http.StatusBadRequest, urlguard.InvalidMessage("landing_page."+f.name))
				return
			}
		}
	}
	// Issue #185: currency must be a supported code (default MYR). Empty is
	// treated as "keep current" by the read-modify-write below.
	if in.Currency != "" && !dbstore.IsSupportedCurrency(in.Currency) {
		writeErr(w, http.StatusBadRequest, "mata wang tidak disokong")
		return
	}
	// Language placement: the workspace's buyer-facing default language must be
	// one of the five supported codes. Empty is treated as "keep current" by the
	// read-modify-write below, so an unsupported value is rejected rather than
	// silently persisted (a stored "fr" would leave the storefront on a language
	// the renderer has no strings for).
	if in.DefaultLang != "" && !dbstore.ValidProfileLang(in.DefaultLang) {
		writeErr(w, http.StatusBadRequest, "bahasa tidak disokong")
		return
	}
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	// Read-modify-write: omitted fields keep their current value (issue #185
	// currency change applies to new products/sales going forward; historical
	// sales keep their original currency).
	cur, err := h.Workspaces.Settings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st := &dbstore.WorkspaceSettings{
		WorkspaceID:    s.ID,
		FooterText:     in.FooterText,
		CompanyReg:     in.CompanyReg,
		PrivacyURL:     in.PrivacyURL,
		TermsURL:       in.TermsURL,
		RefundURL:      in.RefundURL,
		ContactPhone:   in.ContactPhone,
		ContactAddress: in.ContactAddress,
		BusinessName:   in.BusinessName,
		MenuJSON:       in.MenuJSON,
		// Language placement: preserve the current buyer-facing default language
		// unless one is supplied. This MUST be seeded from cur (like currency),
		// not from in: SaveSettings writes default_lang on every save, so the
		// zero value from a request that omitted the field would be stored —
		// and WorkspaceLang() coerces any unrecognised value (including "") to
		// "en", so an unrelated "save my footer" would silently reset the
		// merchant's language choice to English instead of erroring. The
		// assignment below the literal only overrides it when a code was sent.
		DefaultLang: cur.DefaultLang,
		Currency:    cur.Currency,
		Indexing:    cur.Indexing,
		// Issue #360: preserve the current landing page unless one is supplied.
		LandingPage: cur.LandingPage,
		// Issue #447: preserve the current sound toggle unless one is supplied.
		SoundEnabled: cur.SoundEnabled,
		// Preserve the current AI spam-judge toggle unless one is
		// supplied. This line is load-bearing: SaveSettings writes every
		// column from this struct, so a fresh literal that omitted the field
		// would persist false and silently switch spam detection off for the
		// workspace on the next unrelated settings save.
		SpamJudgeEnabled: cur.SpamJudgeEnabled,
	}
	if in.Currency != "" {
		st.Currency = in.Currency
	}
	if in.Indexing != nil {
		st.Indexing = *in.Indexing
	}
	if in.LandingPage != nil {
		st.LandingPage = in.LandingPage
	}
	if in.SoundEnabled != nil {
		st.SoundEnabled = *in.SoundEnabled
	}
	// Language placement: only an explicitly supplied code replaces the current
	// default ("" = keep current, matching the currency pattern above).
	if in.DefaultLang != "" {
		st.DefaultLang = in.DefaultLang
	}
	if in.SpamJudgeEnabled != nil {
		st.SpamJudgeEnabled = *in.SpamJudgeEnabled
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := h.Workspaces.SaveSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ── Per-workspace email settings (issue #107) ────────────────────────────────────

// getWorkspaceEmailSettings: GET /v1/workspaces/{id}/email-settings
func (h *RESTHandlers) getWorkspaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	s, _, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	set, err := h.Workspaces.EmailSettings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// saveWorkspaceEmailSettings: PUT /v1/workspaces/{id}/email-settings
func (h *RESTHandlers) saveWorkspaceEmailSettings(w http.ResponseWriter, r *http.Request, actx *AccountCtx) {
	var in struct {
		ReceiptSubject      string `json:"receipt_subject"`
		ReceiptBody         string `json:"receipt_body"`
		ReceiptEnabled      *bool  `json:"receipt_enabled"`
		NotifyMerchant      *bool  `json:"notify_merchant"`
		MerchantNotifyEmail string `json:"merchant_notify_email"`
		HTML                *bool  `json:"html"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "payload tidak sah")
		return
	}
	s, role, ok := h.workspaceAccessByID(w, r)
	if !ok {
		return
	}
	if !roleAtLeast(role, dbstore.RoleManage) {
		writeErr(w, http.StatusForbidden, "perlu peranan manage atau admin")
		return
	}
	// Read-modify-write: omitted fields keep their current value.
	st, err := h.Workspaces.EmailSettings(r.Context(), s.ID)
	if err != nil {
		respondErr(w, r, err)
		return
	}
	st.ReceiptSubject = in.ReceiptSubject
	st.ReceiptBody = in.ReceiptBody
	st.MerchantNotifyEmail = in.MerchantNotifyEmail
	if in.ReceiptEnabled != nil {
		st.ReceiptEnabled = *in.ReceiptEnabled
	}
	if in.NotifyMerchant != nil {
		st.NotifyMerchant = *in.NotifyMerchant
	}
	if in.HTML != nil {
		st.HTML = *in.HTML
	}
	if err := h.Workspaces.SaveEmailSettings(r.Context(), st); err != nil {
		respondErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}
