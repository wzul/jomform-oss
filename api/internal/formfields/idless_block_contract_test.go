package formfields

// Card t_e854b64c — a required builder block with NO id was unenforced
// server-side. Contract skipped every block whose id was empty while both
// public renderers still painted its control with key "", so a required marker
// on one of those legacy blocks was an HTML attribute a direct POST could
// ignore (the #254 bypass class, measured on prod: 6 required id-less blocks on
// 4 published forms).
//
// The fix is ONE derivation both halves share — SubmitKeys — and these are its
// unit-level arms: the key a block is validated under is the key its control is
// painted under, for a block that carries no id at all.

import (
	"reflect"
	"strings"
	"testing"
)

func TestSubmitKeysKeepsRealIDsAndSynthesisesForAnIdlessBlock(t *testing.T) {
	got := SubmitKeys([]string{"nm", "", "  em  "})
	want := []string{"nm", "_block_1", "em"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SubmitKeys = %q, want %q (a real id is kept and trimmed; an id-less block is keyed by its position)", got, want)
	}
}

// The synthesized key must not collide with a REAL block id that happens to
// look synthetic — otherwise two blocks would be validated/stored under one
// key and one of them would be silently unenforced.
func TestSubmitKeysAvoidsCollidingWithARealID(t *testing.T) {
	got := SubmitKeys([]string{"_block_1", "", "", "_block_2"})
	want := []string{"_block_1", "_block_1_2", "_block_2_2", "_block_2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SubmitKeys = %q, want %q", got, want)
	}
}

// Stability is load-bearing: the key is painted into data-id/name, collected by
// the page's own script, re-derived on the submit side and stored on the
// submission. A per-request value would make the page and the route disagree.
func TestSubmitKeysIsStable(t *testing.T) {
	ids := []string{"", "b2", ""}
	first := SubmitKeys(ids)
	second := SubmitKeys(ids)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("SubmitKeys is not deterministic: %q then %q", first, second)
	}
	if first[0] != "_block_0" || first[2] != "_block_2" {
		t.Fatalf("SubmitKeys = %q, want the id-less blocks keyed _block_0 / _block_2", first)
	}
}

// TestContractCarriesAnIdlessRequiredBlock is the contract half of the card:
// the block reaches the contract under the same synthesized key, so
// ValidateFields has something to enforce and the refusal names the merchant's
// label.
func TestContractCarriesAnIdlessRequiredBlock(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{"type": "section", "value": "Daftar"},                    // display block, no id, not collectable
		map[string]any{"type": "name", "value": "Nama Penuh", "required": true}, // no id — the legacy shape
		map[string]any{"type": "email", "value": "Emel", "required": true},      // no id
	}}

	contract := Contract(cfg)
	if len(contract) != 2 {
		t.Fatalf("Contract = %+v, want the two id-less input blocks", contract)
	}
	if contract[0].FieldID != "_block_1" || contract[1].FieldID != "_block_2" {
		t.Fatalf("Contract field ids = %q, %q; want _block_1, _block_2 (positional, shared with the renderers)",
			contract[0].FieldID, contract[1].FieldID)
	}

	err := Validate(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "Nama Penuh is required") {
		t.Fatalf("Validate(no fields) = %v, want a refusal naming the id-less block's label", err)
	}
	// Partial: exactly the reported bypass shape — the first required block
	// answered, the second left empty.
	err = Validate(cfg, map[string]string{"_block_1": "Ali"})
	if err == nil || !strings.Contains(err.Error(), "Emel is required") {
		t.Fatalf("Validate(one answer) = %v, want a refusal for the second id-less required block", err)
	}
	// Fully answered under the same keys: accepted, so the fix does not turn a
	// real visitor's filled form into an over-refusal.
	if err := Validate(cfg, map[string]string{"_block_1": "Ali", "_block_2": "ali@example.test"}); err != nil {
		t.Fatalf("Validate(filled) = %v, want nil", err)
	}
}

// An id-less block that is NOT required still enters the contract (it is
// collectable), which is what lets its answer be type-checked — the same rule
// an id-carrying optional block gets.
func TestContractCarriesAnIdlessOptionalBlock(t *testing.T) {
	cfg := map[string]any{"blocks": []any{
		map[string]any{"type": "email", "value": "Emel"},
	}}
	contract := Contract(cfg)
	if len(contract) != 1 || contract[0].FieldID != "_block_0" || contract[0].Required {
		t.Fatalf("Contract = %+v, want one optional _block_0 field", contract)
	}
	if err := Validate(cfg, map[string]string{"_block_0": "not-an-email"}); err == nil ||
		!strings.Contains(err.Error(), "Invalid email") {
		t.Fatalf("Validate(invalid email) = %v, want the type refusal", err)
	}
}
