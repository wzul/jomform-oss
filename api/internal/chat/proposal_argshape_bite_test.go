package chat

// THE BITE PROOF for the argument check (card t_623676d1).
//
// WHAT IS UNDER TEST. `validateAgainstToolSchema` is the predicate that decides
// whether a proposal's arguments are ones the target tool will accept. It must
// bite on the REAL contract, not on a hand-written copy of it, and the mutation
// that must turn it red is exactly the one that recreates the shipped defect:
// WEAKEN the schema so the object key is optional.
//
// WHY THE SCHEMA COMES FROM THE LIVE REGISTRY. If this test built its own schema,
// it would prove the predicate agrees with the test — never with the tool. It
// reads update_product's input schema off the real tools/list instead, so the
// "refuses" half is a statement about the shipped tool's contract and would break
// the moment the tool stopped requiring product_id.
//
// The mutation is a VALUE change on a copy (drop `required`), which is the shape
// a regression takes when someone makes an object key optional "for flexibility":
// every other check stays green, and the mis-keyed proposal becomes a card again.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wzul/jomform/api/internal/mcpsrv"
)

// newSchemaProbeServer builds the REAL MCP server, with no stores: the schema
// check reads the registry's declared schemas, so no dependency is needed for it.
func newSchemaProbeServer(t *testing.T) *mcpsrv.Server {
	t.Helper()
	return mcpsrv.New(&mcpsrv.Deps{}, nil, nil, nil)
}

// liveInputSchema returns one tool's input schema from the REAL registry.
func liveInputSchema(t *testing.T, srv *mcp.Server, name string) map[string]any {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-probe", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer cs.Close()
	for tool, ierr := range cs.Tools(ctx, nil) {
		if ierr != nil {
			t.Fatalf("tools/list: %v", ierr)
		}
		if tool.Name != name {
			continue
		}
		raw, merr := json.Marshal(tool.InputSchema)
		if merr != nil {
			t.Fatalf("marshal %s schema: %v", name, merr)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal %s schema: %v", name, err)
		}
		return out
	}
	t.Fatalf("%s is not registered — the guard would pass vacuously", name)
	return nil
}

// TestProposalArgsGuardBitesOnTheRequiredKey is the card's own demand: "change a
// value, watch it go red, restore".
func TestProposalArgsGuardBitesOnTheRequiredKey(t *testing.T) {
	srv := newSchemaProbeServer(t)
	schema := liveInputSchema(t, srv.Raw(), "update_product")

	// The shipped contract really does require the object key: without this the
	// bite below could pass by the schema having nothing to drop.
	req, _ := schema["required"].([]any)
	if !containsString(req, "product_id") {
		t.Fatalf("update_product's registered schema does not require product_id (%v) — this test's "+
			"whole premise is gone, and so is the check it guards", schema["required"])
	}

	// The live defect's arguments: the object keyed `id`, as the READ tool returns
	// it (mcpsrv.handlers.go's productOut is `json:"id"`).
	args := map[string]any{"id": "11111111-2222-3333-4444-555555555555", "name": "renamed"}

	// ── ARM 1: the REAL schema refuses it. ──
	// Two independent schema rules reject this shape — `additionalProperties:false`
	// refuses the undeclared `id`, and `required` misses `product_id` — and either
	// one alone is a correct refusal, so the assertion is that the message names
	// SOMETHING the model can act on (the undeclared key or the required one), not
	// which of the two fired first.
	if err := validateAgainstToolSchema("update_product", schema, args); err == nil {
		t.Fatal("the mis-keyed arguments were ACCEPTED against the tool's own registered schema. " +
			"That is the shipped defect: the card renders naming no object, and Confirm spends the " +
			"token refusing it (t_623676d1).")
	} else if !strings.Contains(err.Error(), "id") && !strings.Contains(err.Error(), "product_id") {
		t.Errorf("the refusal names neither the offending key nor the missing one (%v) — the model, "+
			"which is the only party that can fix a mis-keyed object, is given nothing to correct", err)
	}

	// ── ARM 2: ONE value changed — the `required` entry dropped — and the
	// REQUIRED-key half flips. ──
	//
	// The arguments here are deliberately one that ONLY the `required` rule can
	// catch: `name` IS declared, so `additionalProperties` has nothing to refuse
	// and the missing object key is the sole objection. (ARM 1's `id` shape is
	// caught by BOTH rules, so it cannot tell them apart.) This is the mutation a
	// regression takes when an object key is made optional "for flexibility", and
	// it must be visible: if the predicate did not read `required`, this arm would
	// stay refused and the test would fail here.
	mutated := map[string]any{}
	for k, v := range schema {
		mutated[k] = v
	}
	kept := make([]any, 0, len(req))
	for _, r := range req {
		if s, ok := r.(string); ok && s == "product_id" {
			continue
		}
		kept = append(kept, r)
	}
	mutated["required"] = kept
	if len(kept) == len(req) {
		t.Fatalf("the mutation dropped nothing — `required` was %v", req)
	}
	onlyMissing := map[string]any{"name": "renamed"}
	if err := validateAgainstToolSchema("update_product", schema, onlyMissing); err == nil {
		t.Fatal("arguments naming NO object were accepted against the shipped schema — the object key " +
			"is not actually required by the contract this check reads")
	}
	if err := validateAgainstToolSchema("update_product", mutated, onlyMissing); err != nil {
		t.Fatalf("with `product_id` made OPTIONAL the same object-less arguments are still refused "+
			"(%v). The predicate is not reading the schema's `required`, so it is not checking the "+
			"tool's contract — the guard proves nothing about the shipped tool.", err)
	}

	// ── ARM 3 (the control that keeps ARMS 1–2 honest): the CORRECT key passes
	// against the REAL schema, so the refusal above is about the KEY and not about
	// the checker rejecting everything. ──
	good := map[string]any{"product_id": "11111111-2222-3333-4444-555555555555", "name": "renamed"}
	if err := validateAgainstToolSchema("update_product", schema, good); err != nil {
		t.Fatalf("the correctly keyed arguments were refused against the real schema (%v) — ARM 1 "+
			"would then be satisfied by a checker that refuses everything", err)
	}

	// ── ARM 4: an EXTRA key is refused too, which is what makes the check agree
	// with the tool rather than merely with `required`. Every write schema sets
	// additionalProperties:false, so the tool itself would reject an unknown key
	// before its handler ran; a proposal lane that carded one would be carding a
	// call that cannot execute. ──
	extra := map[string]any{
		"product_id": "11111111-2222-3333-4444-555555555555",
		"name":       "renamed",
		"nope":       "surprise",
	}
	if err := validateAgainstToolSchema("update_product", schema, extra); err == nil {
		t.Error("an argument set carrying a key the tool does not declare was accepted — the tool's " +
			"own schema sets additionalProperties:false, so it would refuse this call and the card " +
			"would again be one the user cannot carry out")
	}
}

// containsString reports whether a decoded JSON array holds s.
func containsString(arr []any, s string) bool {
	for _, v := range arr {
		if str, ok := v.(string); ok && str == s {
			return true
		}
	}
	return false
}
