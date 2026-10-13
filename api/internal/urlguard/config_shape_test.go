package urlguard

// The SHAPE rule — the write-side half of the `config.blocks` JSONB class
// (card t_8fcb546d).
//
// dbstore's gallery-metadata predicate expands `config->'blocks'` with
// `jsonb_array_elements`, which raises SQLSTATE 22023 on anything that is not an
// array — and because BlockedGalleryTemplates / SetStatusMany expand the key for
// a whole id set (`id = ANY($2)`), ONE such row failed a bulk publish for every
// id in the batch. The old guard was `COALESCE(config->'blocks','[]'::jsonb)`,
// which replaces SQL NULL only, so an object/string/number — and a JSON null —
// went straight to the extraction.
//
// These tests pin the rule at its single home: a PRESENT non-array `blocks` is
// refused, while absent / JSON null / a real array stay accepted ("empty or
// omitted = keep existing"). They are unit tests, so the write paths importing
// this package (REST + MCP) cannot drift from each other.

import "testing"

func TestCheckConfigShapeAcceptsAbsentEmptyAndArray(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  map[string]any
	}{
		{"nil config", nil},
		{"no blocks key", map[string]any{"mode": "response"}},
		{"empty array", map[string]any{"blocks": []any{}}},
		{"array of blocks", map[string]any{"blocks": []any{
			map[string]any{"id": "b1", "type": "text", "value": "Hi"},
		}}},
		// JSON null: what `"blocks": null` decodes to. It is "no blocks" for
		// every reader (the predicate renders it as the empty array), and the
		// caller writing it is clearing the key rather than supplying a shape,
		// so it is not a rejection — only a NON-ARRAY VALUE is.
		{"json null", map[string]any{"blocks": nil}},
	} {
		if field, want := CheckConfigShape(tc.cfg); field != "" {
			t.Errorf("%s: CheckConfigShape = (%q, %q), want no violation", tc.name, field, want)
		}
	}
}

func TestCheckConfigShapeRejectsPresentNonArray(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks any
	}{
		{"object", map[string]any{"oops": true}},
		{"empty object", map[string]any{}},
		{"string", "oops"},
		{"number", float64(42)},
		{"bool", true},
	} {
		field, want := CheckConfigShape(map[string]any{"mode": "response", "category": "qa", "blocks": tc.blocks})
		if field != "blocks" || want != "array" {
			t.Errorf("%s: CheckConfigShape = (%q, %q), want (blocks, array)", tc.name, field, want)
		}
	}
}

func TestInvalidShapeMessageNamesTheFieldAndTheWantedType(t *testing.T) {
	msg := InvalidShapeMessage("config.blocks", "array")
	if msg == "" {
		t.Fatal("InvalidShapeMessage returned empty string")
	}
	for _, want := range []string{"config.blocks", "array"} {
		if !contains(msg, want) {
			t.Errorf("InvalidShapeMessage = %q, must mention %q", msg, want)
		}
	}
	// It must NOT state the scheme rule: the value is of the wrong JSON type,
	// not of a rejected URL scheme, so telling the caller it has to start with
	// https:// would be a false statement (measured reason this is not
	// InvalidMessage).
	if contains(msg, "https://") {
		t.Errorf("InvalidShapeMessage = %q states the https:// scheme rule, which is not the "+
			"violation — a shape rejection must not tell the caller their object must be a URL", msg)
	}
}
