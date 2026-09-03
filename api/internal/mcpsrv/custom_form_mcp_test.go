package mcpsrv

// Issue #255: MCP parity for the fully-custom HTML response-only form.
// upsert_form accepts custom_html + custom_fields and stores them under the
// form's config; get_form returns them via the config map. These unit tests
// exercise the config-merge helper without a database.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/customform"
)

func TestApplyCustomFormConfig(t *testing.T) {
	cfg := map[string]any{"mode": "payment", "blocks": []any{}}
	fields := []customform.Field{
		{ID: "name", Type: "text", Label: "Name", Required: true},
		{ID: "email", Type: "email", Label: "Email"},
	}
	if err := applyCustomFormConfig(cfg, "<div>hi</div>", fields); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if cfg["mode"] != "response" {
		t.Errorf("mode = %v, want response", cfg["mode"])
	}
	cc := customform.FromConfig(cfg)
	if cc == nil || cc.HTML != "<div>hi</div>" || len(cc.Fields) != 2 {
		t.Fatalf("custom config = %+v", cc)
	}
	// Existing keys (blocks) must be preserved.
	if _, ok := cfg["blocks"]; !ok {
		t.Errorf("blocks key lost on merge")
	}
}

func TestApplyCustomFormConfigClears(t *testing.T) {
	cfg := map[string]any{
		"mode": "response",
		customform.ConfigKey: map[string]any{"html": "<div>x</div>", "fields": []any{}},
	}
	if err := applyCustomFormConfig(cfg, "  ", nil); err != nil {
		t.Fatalf("apply clear: %v", err)
	}
	if _, ok := cfg[customform.ConfigKey]; ok {
		t.Errorf("custom_html not cleared")
	}
}

func TestApplyCustomFormConfigRejectsBadContract(t *testing.T) {
	cfg := map[string]any{}
	// Duplicate field_id must be rejected.
	if err := applyCustomFormConfig(cfg, "<div></div>", []customform.Field{
		{ID: "a", Type: "text"}, {ID: "a", Type: "text"},
	}); err == nil {
		t.Fatal("duplicate field_id accepted")
	}
	// Invalid type must be rejected.
	if err := applyCustomFormConfig(cfg, "<div></div>", []customform.Field{
		{ID: "a", Type: "bogus"},
	}); err == nil {
		t.Fatal("invalid type accepted")
	}
}
