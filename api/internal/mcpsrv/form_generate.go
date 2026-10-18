// MCP tool: generate_form (issue #256) — MCP parity for the AI form builder.
// A merchant's agent can type a natural-language prompt and get a validated
// JomForm form config (blocks + styling) drafted by deepseek-v4.1-flash:1017
// (Ollama Cloud, via the internal llm client). The output is validated
// server-side against the known block types (formgen) before it is returned;
// product prices always resolve from the product DB (server authority), never
// the LLM. The agent then reviews/edits and saves via upsert_form.
package mcpsrv

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/formgen"
)

// generateFormIn is the input for generate_form.
type generateFormIn struct {
	Prompt string `json:"prompt" jsonschema:"Arahan semula jadi untuk bina borang, cth 'a booking form for a hair salon with name, phone, date, service dropdown'"`
}

// generateForm drafts a validated form config from a natural-language prompt
// (issue #256). The LLM output is treated as untrusted and validated against
// the known block types before it is returned. Prices never come from the LLM.
func (s *Server) generateForm(
	ctx context.Context, _ *mcp.CallToolRequest, in generateFormIn,
) (*mcp.CallToolResult, *formgen.Config, error) {
	if _, err := s.requireWriteRole(ctx); err != nil {
		return nil, nil, err
	}
	if s.deps.LLM == nil {
		return nil, nil, errors.New("penjanaan AI tidak tersedia")
	}
	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		return nil, nil, errors.New("prompt wajib")
	}
	if len(prompt) > 2000 {
		return nil, nil, errors.New("prompt terlalu panjang (maks 2000 aksara)")
	}
	raw, err := s.deps.LLM.GenerateFormConfig(ctx, formgen.Prompt(prompt))
	if err != nil {
		return nil, nil, err
	}
	cfg, err := formgen.Parse(raw)
	if err != nil {
		return nil, nil, errors.New("penjanaan tidak sah: " + err.Error())
	}
	return nil, cfg, nil
}
