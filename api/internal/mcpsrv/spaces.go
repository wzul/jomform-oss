// MCP parity tools: spaces (multi-tenant storefronts) + per-space settings.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// ── Space context resolution ─────────────────────────────────────────────────

// spacesStore returns the space store, erroring when not wired.
func (s *Server) spacesStore() (*dbstore.SpaceStore, error) {
	if s.deps.Spaces == nil {
		return nil, errors.New("spaces tidak tersedia")
	}
	return s.deps.Spaces, nil
}

// spaceOwner resolves the user (spaces.owner_user_id) behind the MCP key's
// account — the same identity the dashboard's sessionOwnerID resolves.
func (s *Server) spaceOwner(ctx context.Context) (string, error) {
	key := KeyFromContext(ctx)
	if key == nil {
		return "", fmt.Errorf("api key tidak sah")
	}
	a, err := s.deps.Accounts.ByID(ctx, key.AccountID)
	if err != nil {
		return "", fmt.Errorf("db: %w", err)
	}
	return a.OwnerUserID, nil
}

// ownedSpace fetches a space by ID and verifies the caller owns it.
func (s *Server) ownedSpace(ctx context.Context, spaceID string) (*dbstore.Space, error) {
	sp, err := s.spacesStore()
	if err != nil {
		return nil, err
	}
	owner, err := s.spaceOwner(ctx)
	if err != nil {
		return nil, err
	}
	space, err := sp.ByID(ctx, spaceID)
	if err != nil || space == nil || space.Owner != owner {
		return nil, errors.New("space tidak dijumpai")
	}
	return space, nil
}

// ── list_spaces ──────────────────────────────────────────────────────────────

type spaceOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

func (s *Server) listSpaces(
	ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn,
) (*mcp.CallToolResult, []spaceOut, error) {
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.spaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	spaces, err := sp.ByOwner(ctx, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	out := make([]spaceOut, 0, len(spaces))
	for _, x := range spaces {
		out = append(out, spaceOut{
			ID: x.ID, Name: x.Name, Slug: x.Slug,
			URL: "https://" + x.Slug + ".jomform.com",
		})
	}
	return nil, out, nil
}

// ── create_space ─────────────────────────────────────────────────────────────

type createSpaceIn struct {
	Name string `json:"name" jsonschema:"Nama space / storefront"`
	Slug string `json:"slug" jsonschema:"Slug URL unik — 3-40 aksara, a-z 0-9 dash sahaja"`
}

// validateSpaceSlug mirrors the REST handler in httpapi/rest_spaces.go.
func validateSpaceSlug(slug string) error {
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

func (s *Server) createSpace(
	ctx context.Context, _ *mcp.CallToolRequest, in createSpaceIn,
) (*mcp.CallToolResult, map[string]string, error) {
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	owner, err := s.spaceOwner(ctx)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Slug) == "" {
		return nil, nil, errors.New("name dan slug wajib")
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if err := validateSpaceSlug(slug); err != nil {
		return nil, nil, err
	}
	if existing, _ := sp.BySlug(ctx, slug); existing != nil {
		return nil, nil, errors.New("slug sudah digunakan")
	}
	space, err := sp.Create(ctx, owner, strings.TrimSpace(in.Name), slug)
	if err != nil {
		// unique-constraint backstop (race between the pre-check and INSERT)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, nil, errors.New("slug sudah digunakan")
		}
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{
		"status": "created", "id": space.ID, "name": space.Name, "slug": space.Slug,
		"url": "https://" + space.Slug + ".jomform.com",
	}, nil
}

// ── get_space_settings ───────────────────────────────────────────────────────

type spaceRefIn struct {
	SpaceID string `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
}

func (s *Server) getSpaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in spaceRefIn,
) (*mcp.CallToolResult, *dbstore.SpaceSettings, error) {
	if _, err := s.ownedSpace(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	st, err := sp.Settings(ctx, in.SpaceID)
	if err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, st, nil
}

// ── set_space_settings ───────────────────────────────────────────────────────

type setSpaceSettingsIn struct {
	SpaceID    string  `json:"space_id" jsonschema:"ID space (dari list_spaces)"`
	FooterText *string `json:"footer_text,omitempty" jsonschema:"Teks footer storefront (omit = kekal, \"\" = kosongkan)"`
	CompanyReg *string `json:"company_reg,omitempty" jsonschema:"Nombor pendaftaran syarikat (omit = kekal)"`
	PrivacyURL *string `json:"privacy_url,omitempty" jsonschema:"URL polisi privasi — mesti https:// (omit = kekal)"`
	TermsURL   *string `json:"terms_url,omitempty" jsonschema:"URL terma & syarat — mesti https:// (omit = kekal)"`
	MenuJSON   *string `json:"menu_json,omitempty" jsonschema:"Menu navigasi JSON, cth [{\"label\":\"Home\",\"url\":\"/\"}] (omit = kekal)"`
}

func (s *Server) setSpaceSettings(
	ctx context.Context, _ *mcp.CallToolRequest, in setSpaceSettingsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if _, err := s.ownedSpace(ctx, in.SpaceID); err != nil {
		return nil, nil, err
	}
	// URL fields must be https:// (same rule as the REST handler).
	for _, u := range []*string{in.PrivacyURL, in.TermsURL} {
		if u != nil && *u != "" && !strings.HasPrefix(*u, "https://") {
			return nil, nil, errors.New("URL mesti https://")
		}
	}
	if in.MenuJSON != nil && *in.MenuJSON != "" && !jsonValid(*in.MenuJSON) {
		return nil, nil, errors.New("menu_json bukan JSON yang sah")
	}
	sp, err := s.spacesStore()
	if err != nil {
		return nil, nil, err
	}
	// Read-modify-write: omit a field = keep its current value.
	st, err := sp.Settings(ctx, in.SpaceID)
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
	if in.MenuJSON != nil {
		st.MenuJSON = *in.MenuJSON
	}
	if strings.TrimSpace(st.MenuJSON) == "" {
		st.MenuJSON = "[]"
	}
	if err := sp.SaveSettings(ctx, st); err != nil {
		return nil, nil, fmt.Errorf("db: %w", err)
	}
	return nil, map[string]string{"status": "saved", "space_id": in.SpaceID}, nil
}
