// MCP parity tools: per-form Open Graph image (issue #411).
package mcpsrv

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getFormOGImageIn is the input for get_form_og_image.
type getFormOGImageIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// getFormOGImage reads a form's custom Open Graph image (issue #411). When
// empty, the form falls back to the auto-generated default at /og/{slug}.png.
func (s *Server) getFormOGImage(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormOGImageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	return nil, map[string]string{
		"form_id": f.ID,
		"og_image": f.LogoURL,
	}, nil
}

// setFormOGImageIn is the input for set_form_og_image.
type setFormOGImageIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
	// ImageURL is the custom Open Graph image (must be https://). Empty =
	// clear the custom image and fall back to the generated default.
	ImageURL string `json:"image_url" jsonschema:"URL imej OG tersuai (mesti https://). Kosong = guna lalai janaan."`
}

// setFormOGImage saves a form's custom Open Graph image (issue #411). The
// image is used as og:image when sharing the form; empty falls back to the
// auto-generated default.
func (s *Server) setFormOGImage(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormOGImageIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	if in.ImageURL != "" && !strings.HasPrefix(in.ImageURL, "https://") {
		return nil, nil, errors.New("image_url mesti https://")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	// The custom og:image is stored as the form's logo_url (the same field
	// the dashboard's form-settings logo uses). UpdateSettings preserves the
	// other settings by reading them first.
	f, err := s.deps.Forms.GetByID(ctx, acc, in.FormID)
	if err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	if err := s.deps.Forms.UpdateSettings(ctx, acc, in.FormID,
		f.RedirectSuccess, f.RedirectFailure, f.AllowEmbed, in.ImageURL, f.FailureMessage); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}
