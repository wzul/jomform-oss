// MCP parity tools: per-form payment-failed message (issue #393).
package mcpsrv

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getFormFailureMessageIn is the input for get_form_failure_message.
type getFormFailureMessageIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
}

// getFormFailureMessage reads a form's payment-failed message (issue #393).
// Empty means the buyer-facing default is shown on a failed payment.
func (s *Server) getFormFailureMessage(
	ctx context.Context, _ *mcp.CallToolRequest, in getFormFailureMessageIn,
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
	return nil, map[string]string{"form_id": f.ID, "failure_message": f.FailureMessage}, nil
}

// setFormFailureMessageIn is the input for set_form_failure_message.
type setFormFailureMessageIn struct {
	FormID string `json:"form_id" jsonschema:"ID borang"`
	// Message is the HTML shown to a buyer when a payment fails. It is
	// sanitized on render (no XSS). Empty = fall back to the default.
	Message string `json:"message" jsonschema:"Mesej HTML (disanitasi, tiada XSS) bila pembayaran gagal. Kosong = guna lalai."`
}

// setFormFailureMessage saves a form's payment-failed message (issue #393).
func (s *Server) setFormFailureMessage(
	ctx context.Context, _ *mcp.CallToolRequest, in setFormFailureMessageIn,
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
	if err := s.deps.Forms.SetFailureMessage(ctx, acc, in.FormID, in.Message); err != nil {
		return nil, nil, err
	}
	return nil, map[string]string{"status": "saved", "form_id": in.FormID}, nil
}
