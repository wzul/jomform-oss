// MCP parity tools: per-form confirmations (issue #179).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getFormConfirmationsIn is the input for get_form_confirmations.
type getFormConfirmationsIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// confirmationOut mirrors dbstore.Confirmation for the MCP response.
type confirmationOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // message | redirect
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url"`
	Condition   any    `json:"condition,omitempty"`
	IsDefault   bool   `json:"is_default"`
	Position    int    `json:"position"`
}

// getFormConfirmations reads a form's confirmations (issue #179). The default
// confirmation is always present.
func (s *Server) getFormConfirmations(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormConfirmationsIn,
) (*mcp.CallToolResult, map[string]any, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	acc, err := s.resolveFormAccount(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	confs, err := s.deps.Forms.Confirmations(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]confirmationOut, 0, len(confs))
	for _, c := range confs {
		co := confirmationOut{
			ID: c.ID, Name: c.Name, Type: c.Type, Message: c.Message,
			RedirectURL: c.RedirectURL, IsDefault: c.IsDefault, Position: c.Position,
		}
		if c.Condition != nil {
			co.Condition = c.Condition
		}
		out = append(out, co)
	}
	return nil, map[string]any{"confirmations": out}, nil
}

// setFormConfirmationsIn replaces a form's confirmations (issue #179). The
// default confirmation is always preserved server-side.
type setFormConfirmationsIn struct {
	FormID        string           `json:"form_id" jsonschema:"ID borang"`
	Confirmations []confirmationIn `json:"confirmations" jsonschema:"Senarai pengesahan (ganti semua; lalai sentiasa dikekalkan)"`
}

// confirmationIn is one confirmation to save.
type confirmationIn struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name" jsonschema:"Nama pengesahan"`
	Type        string `json:"type" jsonschema:"message | redirect"`
	Message     string `json:"message,omitempty" jsonschema:"Mesej HTML (disanitasi, tiada XSS) bila type=message; kosong = mesej lalai terbina, dipaparkan ikut bahasa pembeli"`
	RedirectURL string `json:"redirect_url,omitempty" jsonschema:"URL https:// bila type=redirect"`
	Condition   *struct {
		FieldID string `json:"field_id"`
		Op      string `json:"op"`
		Value   string `json:"value"`
	} `json:"condition,omitempty" jsonschema:"Logik bersyarat (kosong = sentiasa padan)"`
	IsDefault bool `json:"is_default,omitempty"`
}

func (s *Server) setFormConfirmations(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormConfirmationsIn,
) (*mcp.CallToolResult, map[string]string, error) {
	if s.deps.Forms == nil {
		return nil, nil, errors.New("forms tidak tersedia")
	}
	if in.FormID == "" {
		return nil, nil, errors.New("form_id wajib")
	}
	acc, err := s.requireFormWrite(ctx, in.FormID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := s.deps.Forms.GetByID(ctx, acc, in.FormID); err != nil {
		return nil, nil, errors.New("borang tidak dijumpai")
	}
	if err := s.applyConfirmations(ctx, in.FormID, in.Confirmations); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}
