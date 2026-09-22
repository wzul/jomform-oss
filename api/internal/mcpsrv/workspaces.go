// MCP parity tools: workspaces (multi-tenant storefronts) + per-workspace settings.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/urlguard"
)

// ── Workspace context resolution ─────────────────────────────────────────────────

// workspacesStore returns the workspace store, erroring when not wired.
func (s *Server) workspacesStore() (*dbstore.WorkspaceStore, error) {
	if s.deps.Workspaces == nil {
		return nil, errors.New("workspaces tidak tersedia")
	}
	return s.deps.Workspaces, nil
}

// workspaceOwner resolves the user behind the MCP caller — the same identity the
// dashboard's sessionOwnerID resolves. Issue #307: OAuth tokens are bound to
// the user directly; API-key auth resolves the account's owner.
func (s *Server) workspaceOwner(ctx context.Context) (string, error) {
	id := IdentityFromContext(ctx)
	if id == nil {
		return "", fmt.Errorf("token tiada atau tidak sah")
	}
	if id.UserID != "" {
		return id.UserID, nil
	}
	a, err := s.deps.Accounts.ByID(ctx, id.AccountID)
	if err != nil {
		return "", fmt.Errorf("db: %w", err)
	}
	return a.OwnerUserID, nil
}

// accessibleWorkspace fetches a workspace by ID and verifies the caller is the owner
// or a member (issue #162). Returns the workspace + the caller's role.
func (s *Server) accessibleWorkspace(ctx context.Context, workspaceID string) (*dbstore.Workspace, string, error) {
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, "", err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, "", err
	}
	workspace, err := sp.ByID(ctx, workspaceID)
	if err != nil || workspace == nil {
		return nil, "", errors.New("workspace tidak dijumpai")
	}
	acc, ok, err := sp.AccessByID(ctx, workspaceID, owner)
	if err != nil || !ok {
		return nil, "", errors.New("workspace tidak dijumpai")
	}
	return workspace, acc.Role, nil
}

// requireWorkspaceWriteRole is the ONE gate for a per-workspace write tool. It
// enforces both independent axes:
//
//   - the API-key WRITE SCOPE (requireWriteScope): a readonly key is refused.
//     Issue #645 — the axis the role-only gate dropped.
//   - the manage-or-admin ROLE in the named workspace (issue #162).
//
// It replaces the `_, role, err := s.accessibleWorkspace(...)` + inline role
// comparison that six write handlers used to repeat: that copy-paste is how the
// scope half went missing from all of them at once, so the check lives here now
// and each handler names it.
func (s *Server) requireWorkspaceWriteRole(ctx context.Context, workspaceID string) (*dbstore.Workspace, error) {
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, err
	}
	workspace, role, err := s.accessibleWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if role != "" && role != dbstore.RoleManage && role != dbstore.RoleAdmin {
		return nil, errors.New("perlu peranan manage atau admin")
	}
	return workspace, nil
}

// ── list_workspaces ──────────────────────────────────────────────────────────────

type workspaceOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
	Role string `json:"role,omitempty"`
}

func (s *Server) listWorkspaces(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, *listOutput[workspaceOut], error) {
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	workspaces, err := sp.ByMember(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]workspaceOut, 0, len(workspaces))
	for _, x := range workspaces {
		out = append(out, workspaceOut{
			ID: x.WorkspaceID, Name: x.Name, Slug: x.Slug,
			URL:  "https://" + x.Slug + ".jomform.com",
			Role: x.Role,
		})
	}
	return nil, newListOut(out), nil
}

// ── create_workspace ─────────────────────────────────────────────────────────────

type createWorkspaceIn struct {
	Name string `json:"name" jsonschema:"Nama workspace / storefront"`
	Slug string `json:"slug" jsonschema:"Slug URL unik — 3-40 aksara, a-z 0-9 dash sahaja"`
}

// validateWorkspaceSlug mirrors the REST handler in httpapi/rest_workspaces.go.
func validateWorkspaceSlug(slug string) error {
	// Reserved subdomains (t_586ff0c3) run FIRST, exactly as they do in the REST
	// handler this mirrors. This mirrors the REST handler in
	// httpapi/rest_workspaces.go, and BOTH must consume the ONE list in
	// internal/urlguard — a hand-rolled copy here would let a name be refused on
	// one surface and claimable through the other.
	//
	// Order is not cosmetic: "s3" is a reserved label AND shorter than the
	// 3-char minimum, so if the length rule ran first the caller would be told
	// the slug was malformed and would retry a variation of the sensitive name.
	if urlguard.ReservedSubdomain(slug) {
		return errors.New(urlguard.ReservedSubdomainMessage(slug))
	}
	if len(slug) < 3 || len(slug) > 40 {
		return errors.New("slug 3-40 aksara")
	}
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return errors.New("slug: a-z, 0-9, dash sahaja")
		}
	}
	if strings.HasSuffix(slug, "jomform") {
		return errors.New("slug tidak sah")
	}
	return nil
}

func (s *Server) createWorkspace(
	ctx context.Context, _ *mcp.CallToolRequest, in createWorkspaceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: creating a workspace is a write. It has no target workspace to
	// take a role from yet, so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Slug) == "" {
		return nil, nil, errors.New("name dan slug wajib")
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if err := validateWorkspaceSlug(slug); err != nil {
		return nil, nil, err
	}
	// The slug pre-check must read the NOT-FOUND case as "free", not as "taken".
	// dbstore.scanWorkspace returns a non-nil *Workspace even when the query
	// matched no row (it returns the struct it was filling, alongside
	// pgx.ErrNoRows), so testing `existing != nil` was ALWAYS true and
	// create_workspace answered "slug sudah digunakan" for every slug — the tool
	// could never create anything. Treat only a real row as taken; any other
	// error is left to the INSERT below, which has the unique-constraint backstop.
	if existing, berr := sp.BySlug(ctx, slug); berr == nil && existing != nil {
		return nil, nil, errors.New("slug sudah digunakan")
	} else if berr != nil && !errors.Is(berr, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("db: %w", berr)
	}
	// Ask-first onboarding parity with the REST create path (httpapi
	// rest_workspaces.go): a caller creating their FIRST workspace reuses the
	// account signup already made instead of minting a second one. Workspaces.Create
	// always inserts a fresh account (issue #158), so without this the first
	// workspace created through MCP would leave the user owning two accounts —
	// one of them workspace-less and unreachable. Bounded exactly like REST: only
	// when the caller has no workspace yet, and only for an account that carries
	// no CONTENT (a legacy account holding forms/products/sales is a
	// data-migration decision, not a side effect of creating a workspace).
	//
	// The caller's own API key is NOT such content: an /mcp caller always has
	// one (auth resolves the Bearer through the api_keys table), so treating it
	// as a boundary made this branch unreachable on the one transport that needs
	// it and minted a second account with the credential stranded. See
	// dbstore.WorkspaceStore.SoleEmptyUnscopedAccount.
	var workspace *dbstore.Workspace
	if prior, _ := sp.ByMember(ctx, owner); len(prior) == 0 {
		if accID, aerr := sp.SoleEmptyUnscopedAccount(ctx, owner); aerr == nil && accID != "" {
			workspace, err = sp.CreateForAccount(ctx, owner, strings.TrimSpace(in.Name), slug, accID)
		}
	}
	if workspace == nil && err == nil {
		workspace, err = sp.Create(ctx, owner, strings.TrimSpace(in.Name), slug)
	}
	if err != nil {
		// unique-constraint backstop (race between the pre-check and INSERT)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil, errors.New("slug sudah digunakan")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{
		"status": "created", "id": workspace.ID, "name": workspace.Name, "slug": workspace.Slug,
		"url": "https://" + workspace.Slug + ".jomform.com",
	}, nil
}

// ── set_default_workspace (issue #164) ──────────────────────────────────────────

type setDefaultWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces). Kosong = buang default."`
}

// setDefaultWorkspace sets (or clears, when workspace_id is empty) the user's default
// workspace — the workspace they land on after login, skipping the workspace-picker. The
// workspace must belong to the user (owner or member).
func (s *Server) setDefaultWorkspace(
	ctx context.Context, _ *mcp.CallToolRequest, in setDefaultWorkspaceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: this is a user-level preference write (the user row, not the
	// account), so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users tidak tersedia")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	if in.WorkspaceID == "" {
		if err := s.deps.Users.ClearDefaultWorkspace(ctx, owner); err != nil {
			return nil, nil, fmt.Errorf("db: %w", err)
		}
		return nil, map[string]string{"status": "cleared"}, nil
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.ByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		return nil, nil, errors.New("workspace tidak dijumpai")
	}
	// The workspace must belong to the user (owner or member, issue #162).
	if _, ok, aerr := sp.AccessByID(ctx, in.WorkspaceID, owner); aerr != nil || !ok {
		return nil, nil, errors.New("workspace tidak dijumpai")
	}
	if err := s.deps.Users.SetDefaultWorkspace(ctx, owner, in.WorkspaceID); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}

// mcpExemptKeyID returns the id of the API key authenticating this MCP call, or
// "" for a non-key identity (OAuth token). It is passed to the workspace
// emptiness guard as the ONE key exempt from the live-key blocker — see
// dbstore.emptyCheckArgs for why that exemption is required rather than
// convenient.
func mcpExemptKeyID(ctx context.Context) string {
	id := IdentityFromContext(ctx)
	if id == nil || id.APIKey == nil {
		return ""
	}
	return id.APIKey.ID
}

// ── delete_workspace / check_workspace_deletion ─────────────────────────────────
//
// Parity for DELETE /v1/workspaces/{id} and GET /v1/workspaces/{id}/deletion-check.
//
// OWNER ONLY. An admin member manages a workspace but does not own its public
// identity, its trading history, or the CHIP account holding its credentials;
// they leave through remove_workspace_member. The MCP path enforces the same
// rule as REST rather than relying on the manage-or-admin role gate, because
// "admin member" would otherwise be enough to destroy someone else's workspace.

// deleteWorkspaceIn confirms a permanent workspace delete.
//
// The confirmation is the workspace SLUG, not a boolean — the same level as
// revoke_api_key's confirm=true. This destroys the workspace, everything under
// it, and its CHIP account, and cannot be undone.
type deleteWorkspaceIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	ConfirmSlug string `json:"confirm_slug,omitempty" jsonschema:"Slug workspace itu sendiri, untuk sahkan pemadaman. Salah/tiada = ditolak."`
}

// deleteWorkspaceMCP permanently deletes a workspace the caller OWNS.
//
// It is not a thin wrapper over the store call: both guards the store enforces
// are surfaced as named refusals here, so an agent gets a reason it can act on
// (which categories block, or "you need at least one workspace") instead of an
// opaque failure.
func (s *Server) deleteWorkspaceMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in deleteWorkspaceIn,
) (*mcp.CallToolResult, map[string]any, error) {
	// Issue #645: the scope axis. Checked first so a readonly key cannot probe.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := sp.ByID(ctx, in.WorkspaceID)
	if err != nil || workspace == nil {
		return nil, nil, errors.New("workspace tidak dijumpai")
	}
	// Ownership, not membership: the caller must be the OWNER. A member who is
	// an admin is refused with the same message as REST.
	if workspace.Owner != owner {
		return nil, nil, errors.New("hanya pemilik workspace boleh memadamnya")
	}
	// Confirmation: the caller must name the workspace it is destroying.
	if strings.TrimSpace(in.ConfirmSlug) != workspace.Slug {
		return nil, map[string]any{
			"status": "refused",
			"error": "taip slug workspace untuk sahkan pemadaman: " + workspace.Slug +
				". Pemadaman workspace tidak boleh dibatalkan.",
			"workspace_id": workspace.ID, "slug": workspace.Slug,
		}, nil
	}
	deleted, err := sp.Delete(ctx, workspace.ID, owner, mcpExemptKeyID(ctx))
	if err != nil {
		var de *dbstore.WorkspaceDeleteError
		if errors.As(err, &de) {
			if de.Reason == "last_workspace" {
				return nil, map[string]any{
					"status": "refused",
					"error": "anda perlu sekurang-kurangnya satu workspace — cipta workspace baharu dahulu " +
						"sebelum memadam yang terakhir.",
					"workspace_id": workspace.ID, "last_workspace": true,
				}, nil
			}
			blockers := make([]map[string]any, 0, len(de.Blockers))
			for _, b := range de.Blockers {
				blockers = append(blockers, map[string]any{"kind": b.Kind, "count": b.Count, "label": b.Label})
			}
			return nil, map[string]any{
				"status": "refused",
				"error": "workspace tidak kosong — kosongkan setiap kategori di bawah dahulu " +
					"(borang: trash dahulu, kemudian purge_form).",
				"workspace_id": workspace.ID, "empty": false, "blockers": blockers,
			}, nil
		}
		if errors.Is(err, dbstore.ErrWorkspaceNotFound) {
			return nil, nil, errors.New("workspace tidak dijumpai")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "deleted", "workspace_id": workspace.ID, "slug": workspace.Slug,
		"name": workspace.Name, "deleted": deleted,
	}, nil
}

// checkWorkspaceDeletionMCP is the READ half of delete_workspace: it reports the
// SAME emptiness definition the delete enforces, plus whether this is the
// caller's last workspace, so an agent can state what must be cleared instead of
// discovering it by attempting the delete.
func (s *Server) checkWorkspaceDeletionMCP(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, map[string]any, error) {
	workspace, role, err := s.accessibleWorkspace(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	_, blockers, err := sp.CountEmpty(ctx, workspace.ID, mcpExemptKeyID(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]map[string]any, 0, len(blockers))
	for _, b := range blockers {
		out = append(out, map[string]any{"kind": b.Kind, "count": b.Count, "label": b.Label})
	}
	listed, err := sp.ByMember(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	last := true
	for _, w := range listed {
		if w.WorkspaceID != workspace.ID {
			last = false
			break
		}
	}
	isOwner := workspace.Owner == owner
	return nil, map[string]any{
		"workspace_id": workspace.ID, "name": workspace.Name, "slug": workspace.Slug,
		"role": role, "is_owner": isOwner,
		"last_workspace": last, "empty": len(out) == 0,
		"blockers":   out,
		"can_delete": isOwner && len(out) == 0 && !last,
	}, nil
}

// ── update_profile_phone (issue #181) ───────────────────────────────────────

type updateProfilePhoneIn struct {
	Phone string `json:"phone" jsonschema:"Nombor telefon pilihan (kosong = buang). Format longgar: digit, +, ruang, 8-15 digit."`
}

// updateProfilePhone sets (or clears, when phone is empty) the account owner's
// optional phone number — MCP parity for the dashboard Profile → Identity tab.
func (s *Server) updateProfilePhone(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfilePhoneIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: the profile writes edit the USER row (no account role applies),
	// so the API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users tidak tersedia")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	phone := strings.TrimSpace(in.Phone)
	if phone != "" {
		digits := 0
		for _, c := range phone {
			switch {
			case c >= '0' && c <= '9':
				digits++
			case c == '+' || c == ' ' || c == '-' || c == '(' || c == ')':
			default:
				return nil, nil, errors.New("nombor telefon tidak sah")
			}
		}
		if digits < 8 || digits > 15 {
			return nil, nil, errors.New("nombor telefon tidak sah")
		}
	}
	if err := s.deps.Users.UpdatePhone(ctx, owner, phone); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "phone": phone}, nil
}

// ── update_profile_timezone (issue #191) ───────────────────────────────────

type updateProfileTimezoneIn struct {
	Timezone string `json:"timezone" jsonschema:"Zona waktu IANA pilihan (kosong = Auto, ikut pelayar/PC). Contoh: Asia/Kuala_Lumpur, UTC."`
}

// updateProfileTimezone sets (or clears, when timezone is empty) the account
// owner's preferred IANA timezone — MCP parity for the dashboard Profile →
// Identity tab. Empty = Auto (follow the browser/PC default).
func (s *Server) updateProfileTimezone(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfileTimezoneIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: user-row write — API-key write scope is the gate.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users tidak tersedia")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	tz := strings.TrimSpace(in.Timezone)
	if tz != "" {
		if _, lerr := time.LoadLocation(tz); lerr != nil {
			return nil, nil, errors.New("zona waktu tidak sah")
		}
	}
	if err := s.deps.Users.UpdateTimezone(ctx, owner, tz); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "timezone": tz}, nil
}

// ── update_profile_language (language placement) ──────────────────────────

type updateProfileLanguageIn struct {
	Lang string `json:"lang" jsonschema:"Bahasa UI dashboard pilihan anda: en, ms, zh, ta atau ar. Kosong = buang tetapan (kembali ikut pelayar, kemudian English). Ini bahasa DASHBOARD sahaja — bahasa borang awam ditetapkan per-borang/per-workspace."`
}

// updateProfileLanguage sets (or clears, when lang is empty) the account owner's
// preferred DASHBOARD UI language — MCP parity for the dashboard Profile →
// Identity language picker. This is per-PROFILE (per-user): the setting follows
// the person into every browser. It is deliberately NOT the workspace's
// buyer-facing default_lang, which is set via set_workspace_settings.
func (s *Server) updateProfileLanguage(
	ctx context.Context, _ *mcp.CallToolRequest, in updateProfileLanguageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: this edits the USER row (the caller's own profile), not the
	// account, so no workspace role applies — the API-key write scope is the
	// gate, exactly as for the neighbouring profile writes.
	if err := s.requireWriteScope(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.Users == nil {
		return nil, nil, errors.New("users tidak tersedia")
	}
	owner, err := s.workspaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	lang := strings.TrimSpace(in.Lang)
	if !dbstore.ValidProfileLang(lang) {
		return nil, nil, errors.New("bahasa tidak disokong (en, ms, zh, ta, ar)")
	}
	if err := s.deps.Users.UpdateLang(ctx, owner, lang); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "lang": lang}, nil
}

// ── get_workspace_settings ───────────────────────────────────────────────────────

type workspaceRefIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

func (s *Server) getWorkspaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceRefIn,
) (*mcp.CallToolResult, *dbstore.WorkspaceSettings, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, st, nil
}

// ── set_workspace_settings ───────────────────────────────────────────────────────

type setWorkspaceSettingsIn struct {
	WorkspaceID    string  `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	FooterText     *string `json:"footer_text,omitempty" jsonschema:"Teks footer storefront (omit = kekal, \"\" = kosongkan)"`
	CompanyReg     *string `json:"company_reg,omitempty" jsonschema:"Nombor pendaftaran syarikat (omit = kekal)"`
	PrivacyURL     *string `json:"privacy_url,omitempty" jsonschema:"URL polisi privasi — mesti https:// (omit = kekal)"`
	TermsURL       *string `json:"terms_url,omitempty" jsonschema:"URL terma & syarat — mesti https:// (omit = kekal)"`
	RefundURL      *string `json:"refund_url,omitempty" jsonschema:"URL polisi bayaran balik — mesti https:// (omit = kekal)"`
	ContactPhone   *string `json:"contact_phone,omitempty" jsonschema:"Nombor telefon perniagaan untuk footer (omit = kekal)"`
	ContactAddress *string `json:"contact_address,omitempty" jsonschema:"Alamat perniagaan untuk footer (omit = kekal)"`
	BusinessName   *string `json:"business_name,omitempty" jsonschema:"Nama perniagaan untuk footer (omit = kekal)"`
	MenuJSON       *string `json:"menu_json,omitempty" jsonschema:"Menu navigasi JSON, cth [{\"label\":\"Home\",\"url\":\"/\"}] (omit = kekal)"`
	// Issue #276: per-workspace SEO indexing toggle. Omitted (nil) = keep current.
	Indexing *bool `json:"indexing,omitempty" jsonschema:"Benarkan enjin carian/AI mengindeks storefront workspace ini (omit = kekal)"`
	// Issue #447: per-workspace realtime notification sound toggle. Omitted
	// (nil) = keep current; explicit true/false sets it.
	SoundEnabled *bool `json:"sound_enabled,omitempty" jsonschema:"Mainkan bunyi beep pada dashboard bila jualan/respon baharu tiba (omit = kekal)"`
	// Per-workspace AI spam-judge toggle. Omitted (nil) = keep
	// current; explicit true/false sets it. Default true.
	SpamJudgeEnabled *bool `json:"spam_judge_enabled,omitempty" jsonschema:"Hidupkan AI spam detection (judge gemma4) untuk workspace ini — true = hidup (lalai), false = MATI: tiada job AI dihantar langsung dan is_spam tidak lagi ditetapkan, jadi /spam kekal kosong (omit = kekal)"`
	// Language placement: the workspace's buyer-facing default language, used by
	// PUBLIC forms when the form itself sets no config.lang (form-level wins).
	// Omitted (nil) = keep current. This is the STOREFRONT language, not the
	// dashboard UI language (that one is per-profile, update_profile_language).
	DefaultLang *string `json:"default_lang,omitempty" jsonschema:"Bahasa lalai borang AWAM untuk workspace ini bila borang itu tidak menetapkan bahasa sendiri: en, ms, zh, ta atau ar. Ini bahasa pembeli, bukan bahasa dashboard anda (omit = kekal)."`
}

func (s *Server) setWorkspaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setWorkspaceSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	// URL fields are https-only (issue #501): ONE shared rule from
	// internal/urlguard, the same validator the REST handlers import — so the
	// policy cannot drift between the two write layers again.
	// A nil (omitted) field keeps its stored value and is never rejected.
	for _, f := range []struct {
		name string
		u    *string
	}{
		{"privacy_url", in.PrivacyURL},
		{"terms_url", in.TermsURL},
		{"refund_url", in.RefundURL},
	} {
		if f.u != nil && !urlguard.Valid(*f.u) {
			return nil, nil, errors.New(urlguard.InvalidMessage(f.name))
		}
	}
	if in.MenuJSON != nil && *in.MenuJSON != "" && !jsonValid(*in.MenuJSON) {
		return nil, nil, errors.New("menu_json bukan JSON yang sah")
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	// Read-modify-write: omit a field = keep its current value.
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if in.FooterText != nil {
		st.FooterText = *in.FooterText
	}
	if in.CompanyReg != nil {
		st.CompanyReg = *in.CompanyReg
	}
	if in.PrivacyURL != nil {
		st.PrivacyURL = *in.PrivacyURL
	}
	if in.TermsURL != nil {
		st.TermsURL = *in.TermsURL
	}
	if in.RefundURL != nil {
		st.RefundURL = *in.RefundURL
	}
	if in.ContactPhone != nil {
		st.ContactPhone = *in.ContactPhone
	}
	if in.ContactAddress != nil {
		st.ContactAddress = *in.ContactAddress
	}
	if in.BusinessName != nil {
		st.BusinessName = *in.BusinessName
	}
	if in.MenuJSON != nil {
		st.MenuJSON = *in.MenuJSON
	}
	if in.Indexing != nil {
		st.Indexing = *in.Indexing
	}
	if in.SoundEnabled != nil {
		st.SoundEnabled = *in.SoundEnabled
	}
	if in.SpamJudgeEnabled != nil {
		st.SpamJudgeEnabled = *in.SpamJudgeEnabled
	}
	// Language placement: the workspace's buyer-facing default language. An
	// unsupported code is rejected rather than silently stored, so a typo cannot
	// leave the storefront on an unrenderable language.
	if in.DefaultLang != nil {
		if !dbstore.ValidProfileLang(*in.DefaultLang) || strings.TrimSpace(*in.DefaultLang) == "" {
			return nil, nil, errors.New("bahasa tidak disokong (en, ms, zh, ta, ar)")
		}
		st.DefaultLang = strings.TrimSpace(*in.DefaultLang)
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}

// ── get_workspace_landing_page / set_workspace_landing_page (issue #360) ─────────────

type workspaceLandingRefIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
}

func (s *Server) getWorkspaceLandingPage(
	ctx context.Context, _ *mcp.CallToolRequest, in workspaceLandingRefIn,
) (*mcp.CallToolResult, *dbstore.LandingPage, error) {
	if _, _, err := s.accessibleWorkspace(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if st.LandingPage == nil {
		st.LandingPage = &dbstore.LandingPage{}
	}
	return nil, st.LandingPage, nil
}

type setWorkspaceLandingPageIn struct {
	WorkspaceID     string                 `json:"workspace_id" jsonschema:"ID workspace (dari list_workspaces)"`
	Enabled         bool                   `json:"enabled" jsonschema:"true = papar landing page; false = jatuh ke senarai borang"`
	Hero            string                 `json:"hero,omitempty" jsonschema:"Tajuk utama (hero)"`
	Subtitle        string                 `json:"subtitle,omitempty" jsonschema:"Sub-tajuk di bawah hero"`
	Body            string                 `json:"body,omitempty" jsonschema:"Teks badan landing page"`
	ImageURL        string                 `json:"image_url,omitempty" jsonschema:"URL imej banner (mesti https://)"`
	PrimaryColor    string                 `json:"primary_color,omitempty" jsonschema:"Warna aksen tema, cth #7c3aed"`
	BackgroundColor string                 `json:"background_color,omitempty" jsonschema:"Warna latar, cth #ffffff"`
	LogoURL         string                 `json:"logo_url,omitempty" jsonschema:"URL logo jenama (mesti https://)"`
	CTAs            []dbstore.LandingCTA   `json:"ctas,omitempty" jsonschema:"Butang CTA menghubung ke borang diterbitkan, cth [{\"label\":\"RSVP\",\"form_slug\":\"rsvp\"}]"`
	Forms           []dbstore.LandingEmbed `json:"forms,omitempty" jsonschema:"Borang diterbitkan dipapar inline dalam landing page, cth [{\"label\":\"RSVP\",\"form_slug\":\"rsvp\"}]"`
}

func (s *Server) setWorkspaceLandingPage(
	ctx context.Context, _ *mcp.CallToolRequest, in setWorkspaceLandingPageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	// Issue #645: write gate = API-key write scope AND manage-or-admin role.
	if _, err := s.requireWorkspaceWriteRole(ctx, in.WorkspaceID); err != nil {
		return nil, nil, err
	}
	// Media URLs are https-only (issue #501), through the ONE shared rule in
	// internal/urlguard (same validator as the REST landing-page path). An
	// empty value means "no image" and stays allowed.
	for _, f := range []struct{ name, u string }{
		{"image_url", in.ImageURL},
		{"logo_url", in.LogoURL},
	} {
		if !urlguard.Valid(f.u) {
			return nil, nil, errors.New(urlguard.InvalidMessage(f.name))
		}
	}
	sp, err := s.workspacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.WorkspaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	lp := &dbstore.LandingPage{
		Enabled:         in.Enabled,
		Hero:            in.Hero,
		Subtitle:        in.Subtitle,
		Body:            in.Body,
		ImageURL:        in.ImageURL,
		PrimaryColor:    in.PrimaryColor,
		BackgroundColor: in.BackgroundColor,
		LogoURL:         in.LogoURL,
		CTAs:            in.CTAs,
		Forms:           in.Forms,
	}
	if lp.CTAs == nil {
		lp.CTAs = []dbstore.LandingCTA{}
	}
	if lp.Forms == nil {
		lp.Forms = []dbstore.LandingEmbed{}
	}
	st.LandingPage = lp
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "workspace_id": in.WorkspaceID}, nil
}
