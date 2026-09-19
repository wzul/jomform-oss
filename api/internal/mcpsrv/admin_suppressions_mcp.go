// Super-admin suppression list MCP tools (issue #601).
//
// The suppression list (issue #56) is the set of addresses JomForm has stopped
// mailing — hard bounces, spam complaints, and addresses a super-admin added by
// hand. Until #601 nothing could read it: no MCP tool, no REST route, no
// screen, so a platform owner's agent could not answer "which addresses are we
// refusing to mail?" and could not fix a false positive without a psql session.
//
// These three tools are the agent-facing half of the /admin suppression
// section (the dashboard uses the matching REST endpoints in
// api/internal/httpapi/rest_admin_suppressions.go). They are gated on the
// super-admin role (#385) via adminGate, which audits the call.
//
// List tools return *listPageOutput[T] / *listOutput[T] — never a bare slice —
// because the go-sdk reflects the handler's Out type into the advertised
// outputSchema, and a slice yields a non-object schema that makes every
// schema-strict MCP client reject the whole ListToolsResult
// (TestMCPToolsHaveObjectOutputSchema guards this).
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/auth"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// suppressionsStore returns the suppression store, or an error when the server
// was built without a database (bare-mode tests, or SES unconfigured).
func (s *Server) suppressionsStore() (*dbstore.SuppressionStore, error) {
	if s.deps == nil || s.deps.Suppressions == nil {
		return nil, errors.New("senarai suppression tidak dikonfigurasi")
	}
	return s.deps.Suppressions, nil
}

// ── admin_list_suppressions ─────────────────────────────────────────────────

type adminListSuppressionsIn struct {
	Q     string `json:"q,omitempty" jsonschema:"Optional — cari dalam emel atau nota (tak sensitif huruf besar/kecil)"`
	Page  int    `json:"page,omitempty" jsonschema:"Optional — muka surat (mulai 1)"`
	Limit int    `json:"limit,omitempty" jsonschema:"Optional — had setiap muka (1-100, lalai 20)"`
}

type adminSuppressionOut struct {
	Email          string `json:"email"`
	Reason         string `json:"reason"` // hard_bounce | complaint | manual
	Source         string `json:"source"` // bounce | complaint | manual
	Note           string `json:"note,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	CreatedByEmail string `json:"created_by_email,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// adminListSuppressions lists the suppression list (newest first), optionally
// filtered by a search term matched against the address and the note.
// Super-admin only, read-only, audited.
func (s *Server) adminListSuppressions(ctx context.Context, _ *mcp.CallToolRequest, in adminListSuppressionsIn) (*mcp.CallToolResult, *listPageOutput[adminSuppressionOut], error) {
	if err := s.adminGate(ctx, "admin.list_suppressions"); err != nil {
		return nil, nil, err
	}
	store, err := s.suppressionsStore()
	if err != nil {
		return nil, nil, err
	}
	page, limit := in.Page, in.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, total, err := store.Search(ctx, strings.TrimSpace(in.Q), page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]adminSuppressionOut, 0, len(rows))
	for _, se := range rows {
		out = append(out, toAdminSuppressionOut(se))
	}
	return nil, newListPageOut(out, page, limit, total), nil
}

// ── admin_add_suppression ───────────────────────────────────────────────────

type adminAddSuppressionIn struct {
	Email  string `json:"email" jsonschema:"Wajib — alamat emel untuk disekat"`
	Reason string `json:"reason,omitempty" jsonschema:"Optional — hard_bounce | complaint | manual (lalai manual)"`
	Note   string `json:"note,omitempty" jsonschema:"Optional — nota bebas untuk rujukan operator"`
}

// adminAddSuppression adds an address to the suppression list by hand so no
// further email is sent to it. Super-admin only, audited.
//
// Idempotent: adding an address already on the list reports created=false
// rather than failing, because the address is suppressed either way.
func (s *Server) adminAddSuppression(ctx context.Context, _ *mcp.CallToolRequest, in adminAddSuppressionIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.add_suppression"); err != nil {
		return nil, nil, err
	}
	store, err := s.suppressionsStore()
	if err != nil {
		return nil, nil, err
	}
	addr := strings.ToLower(strings.TrimSpace(in.Email))
	if !auth.ValidEmail(addr) {
		return nil, nil, errors.New("emel tidak sah")
	}
	reason := strings.ToLower(strings.TrimSpace(in.Reason))
	if reason == "" {
		reason = dbstore.SuppressionReasonManual
	}
	if !dbstore.IsCanonicalReason(reason) {
		return nil, nil, errors.New("sebab tidak sah (hard_bounce | complaint | manual)")
	}
	created, err := store.Add(ctx, addr, reason, dbstore.SuppressionSourceManual,
		strings.TrimSpace(in.Note), s.userIDFor(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]any{
		"status": "suppressed", "email": addr, "created": created,
	}, nil
}

// ── admin_remove_suppression ────────────────────────────────────────────────

type adminRemoveSuppressionIn struct {
	Email string `json:"email" jsonschema:"Wajib — alamat emel untuk dibuang dari senarai"`
}

// adminRemoveSuppression removes an address from the suppression list so mail
// to it resumes. Super-admin only, audited.
func (s *Server) adminRemoveSuppression(ctx context.Context, _ *mcp.CallToolRequest, in adminRemoveSuppressionIn) (*mcp.CallToolResult, map[string]any, error) {
	if err := s.adminGate(ctx, "admin.remove_suppression"); err != nil {
		return nil, nil, err
	}
	store, err := s.suppressionsStore()
	if err != nil {
		return nil, nil, err
	}
	addr := strings.ToLower(strings.TrimSpace(in.Email))
	if addr == "" {
		return nil, nil, errors.New("emel wajib diisi")
	}
	removed, err := store.RemoveIfPresent(ctx, addr)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	if !removed {
		return nil, nil, errors.New("tiada dalam senarai")
	}
	return nil, map[string]any{
		"status": "removed", "email": addr,
		"note": "emel ke alamat ini akan dihantar semula",
	}, nil
}

// toAdminSuppressionOut maps a store row onto the tool's output shape.
func toAdminSuppressionOut(se dbstore.SuppressedEmail) adminSuppressionOut {
	return adminSuppressionOut{
		Email:          se.Email,
		Reason:         se.Reason,
		Source:         se.Source,
		Note:           se.Note,
		CreatedBy:      se.CreatedBy,
		CreatedByEmail: se.CreatedByEmail,
		CreatedAt:      se.CreatedAt.UTC().Format(time.RFC3339),
	}
}
