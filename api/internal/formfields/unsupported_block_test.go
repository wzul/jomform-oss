package formfields

// Card t_4086b8f0 — the unsupported-block rule itself.
//
// A builder block whose type no renderer can honour must be refused by every
// write path that stores a form/template config, or the merchant builds a form
// that looks complete and serves nothing (response mode) or a control whose
// input is thrown away (payment mode: the file picker's bytes were never sent).
// `file` is the type this rule owns, and the reason is in unsupported_block.go.
//
// These are unit tests, so the rule cannot drift between the REST and MCP write
// paths that both import it.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUnsupportedBlockRefusesFileInAnyFormMode: the block cannot be served in
// EITHER mode, so the refusal is not conditional on the form's mode.
func TestUnsupportedBlockRefusesFileInAnyFormMode(t *testing.T) {
	for _, cfg := range []map[string]any{
		{"mode": "response", "blocks": []any{map[string]any{"id": "fl1", "type": "file"}}},
		{"mode": "payment", "blocks": []any{map[string]any{"id": "fl1", "type": "file"}}},
		{"blocks": []any{map[string]any{"id": "fl1", "type": "file"}}}, // mode absent = payment
		{"mode": "response", "blocks": []any{
			map[string]any{"id": "tx1", "type": "text", "value": "Nama"},
			map[string]any{"id": "fl1", "type": "file"},
		}},
	} {
		if got := UnsupportedBlock(cfg); got != "file" {
			t.Errorf("UnsupportedBlock(%v) = %q, want \"file\"", cfg, got)
		}
	}
}

// TestUnsupportedBlockAcceptsEveryServableBlock: the rule must not become a
// blanket refusal. Every palette type a renderer CAN honour — including the
// static and pricing types, which are not field types at all — passes.
func TestUnsupportedBlockAcceptsEveryServableBlock(t *testing.T) {
	servable := []string{
		"text", "textarea", "number", "select", "radio", "checkbox", "name", "email",
		"phone", "date", "time", "website", "section", "html", "image", "divider",
		"address", "consent", "hidden", "list", "repeater", "image-choice", "captcha",
		"product", "quantity", "option", "shipping", "total", "coupon",
	}
	for _, typ := range servable {
		cfg := map[string]any{"blocks": []any{map[string]any{"id": "b1", "type": typ}}}
		if got := UnsupportedBlock(cfg); got != "" {
			t.Errorf("UnsupportedBlock reported %q for the servable type %q", got, typ)
		}
	}
}

// TestUnsupportedBlockIgnoresWhatItDoesNotOwn: an absent / null / non-array
// `blocks` is the SHAPE rule's refusal (urlguard.CheckConfigShape), and a block
// entry that is not an object is nobody's. This rule reports only the type it
// recognises, so a shape problem cannot be reported with the wrong message.
func TestUnsupportedBlockIgnoresWhatItDoesNotOwn(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"nil config":      nil,
		"no blocks key":   {"mode": "response"},
		"null blocks":     {"blocks": nil},
		"object blocks":   {"blocks": map[string]any{"oops": true}},
		"string blocks":   {"blocks": "oops"},
		"empty array":     {"blocks": []any{}},
		"non-object":      {"blocks": []any{"oops", 42}},
		"unknown type":    {"blocks": []any{map[string]any{"id": "b1", "type": "quantum"}}},
		"padded file-ish": {"blocks": []any{map[string]any{"id": "b1", "type": "filex"}}},
	} {
		if got := UnsupportedBlock(cfg); got != "" {
			t.Errorf("%s: UnsupportedBlock = %q, want no violation", name, got)
		}
	}
}

// TestUnsupportedBlockReadsAConfigThatCameFromTheDatabase: the same rule is
// asked about a config decoded from JSONB (where the blocks array is []any of
// map[string]any after one round trip) — the answer must not depend on how the
// config was built.
func TestUnsupportedBlockReadsAConfigThatCameFromTheDatabase(t *testing.T) {
	const stored = `{"mode":"response","blocks":[{"id":"tx1","type":"text"},{"id":"fl1","type":"file"}]}`
	var cfg map[string]any
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		t.Fatalf("decode the stored config: %v", err)
	}
	if got := UnsupportedBlock(cfg); got != "file" {
		t.Errorf("UnsupportedBlock on a JSON-decoded config = %q, want \"file\"", got)
	}
	// Surrounding whitespace in the type is not a way around the rule.
	if got := UnsupportedBlock(map[string]any{"blocks": []any{
		map[string]any{"id": "fl1", "type": " file "},
	}}); got != "file" {
		t.Errorf("UnsupportedBlock with a padded type = %q, want \"file\"", got)
	}
}

// TestUnsupportedBlockMessageNamesTheTypeAndTheReason: the acceptance for this
// card is "refused at save time with a message naming the type", so the wording
// is asserted, not just the code path.
func TestUnsupportedBlockMessageNamesTheTypeAndTheReason(t *testing.T) {
	msg := UnsupportedBlockMessage("file")
	if !strings.Contains(msg, "file") {
		t.Errorf("the message does not name the block type: %q", msg)
	}
	if !strings.Contains(msg, "tidak disokong") {
		t.Errorf("the message does not say the block is unsupported: %q", msg)
	}
	if !strings.Contains(msg, "lampiran") {
		t.Errorf("the message does not say why (no attachment path): %q", msg)
	}
	if !strings.Contains(msg, "padam blok") {
		t.Errorf("the message does not tell the merchant what to do: %q", msg)
	}
	if got := UnsupportedBlockMessage(""); got != "" {
		t.Errorf("UnsupportedBlockMessage(\"\") = %q, want \"\" — no type was refused", got)
	}
}
