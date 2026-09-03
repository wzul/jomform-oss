package formgen

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	raw := `{
		"title": "Hair Salon Booking",
		"blocks": [
			{"id": "name", "type": "name", "value": "Full name", "required": true},
			{"id": "phone", "type": "phone", "value": "Phone number", "required": true},
			{"id": "date", "type": "date", "value": "Preferred date", "required": true},
			{"id": "service", "type": "select", "value": "Service", "required": true,
			 "choices": [{"label": "Cut", "value": "cut"}, {"label": "Colour", "value": "colour"}]},
			{"id": "prod", "type": "product", "value": "Package", "price_sen": 99999}
		],
		"theme": {"primary": "#5e6ad2", "layout": "centered", "spacing": "comfortable"}
	}`
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Title != "Hair Salon Booking" {
		t.Errorf("title = %q", cfg.Title)
	}
	if len(cfg.Blocks) != 5 {
		t.Fatalf("blocks = %d", len(cfg.Blocks))
	}
	// Product price must be stripped (server authority).
	for _, b := range cfg.Blocks {
		if b.Type == "product" && b.PriceSen != 0 {
			t.Errorf("product price not stripped: %d", b.PriceSen)
		}
	}
	// Choice value defaults to label.
	for _, b := range cfg.Blocks {
		if b.Type == "select" && b.Choices[0].Value != "cut" {
			t.Errorf("choice value = %q", b.Choices[0].Value)
		}
	}
}

func TestParseFence(t *testing.T) {
	raw := "```json\n{\"title\": \"T\", \"blocks\": [{\"id\": \"a\", \"type\": \"text\", \"value\": \"A\"}]}\n```"
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Title != "T" {
		t.Errorf("title = %q", cfg.Title)
	}
}

func TestParseRejectsUnknownType(t *testing.T) {
	raw := `{"title": "T", "blocks": [{"id": "a", "type": "hacker", "value": "A"}]}`
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestParseRejectsBadID(t *testing.T) {
	raw := `{"title": "T", "blocks": [{"id": "a b!", "type": "text", "value": "A"}]}`
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error for bad id")
	}
}

func TestParseRejectsDuplicateID(t *testing.T) {
	raw := `{"title": "T", "blocks": [{"id": "a", "type": "text", "value": "A"}, {"id": "a", "type": "email", "value": "B"}]}`
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if _, err := Parse("not json"); err == nil {
		t.Fatal("expected error for non-json")
	}
}

func TestParseRejectsBadLayout(t *testing.T) {
	raw := `{"title": "T", "blocks": [{"id": "a", "type": "text", "value": "A"}], "theme": {"layout": "weird"}}`
	if _, err := Parse(raw); err == nil {
		t.Fatal("expected error for bad layout")
	}
}

func TestPromptConstrainsTypes(t *testing.T) {
	p := Prompt("a contact form")
	if !strings.Contains(p, "Allowed block types") {
		t.Error("prompt missing block-type allowlist")
	}
	if !strings.Contains(p, "Do NOT invent prices") {
		t.Error("prompt missing price rule")
	}
}
