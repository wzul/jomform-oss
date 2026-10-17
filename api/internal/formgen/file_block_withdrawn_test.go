package formgen

// Card t_c28b45ea — a `file` block cannot be served by anything in this product,
// so the AI form generator must not mint one.
//
// THE DEFECT. `file` was the last surface still OFFERING a block the product
// cannot honour. A config carrying it is refused at every write path
// (formfields.UnsupportedBlock: there is no public upload route, so an
// anonymous buyer cannot deposit bytes and no renderer may collect an
// attachment — card t_4086b8f0, PR 1219), and both public renderers print the
// merchant's label plus a note for a form that stored the block before that
// refusal. What stayed behind was the generator: `ValidBlockTypes["file"]` sat
// in the allowlist and `Prompt` named `file` among the types the model may use,
// so an MCP agent asked for "a form with an upload field" got a config that
// `upsert_form` refuses two calls later. That is the same third-surface defect
// `coupon` had (card t_4975927e, coupon_withdrawn_test.go), and the builder
// palette no longer offers the item either (web/src/app/builder/page.tsx,
// pinned by web/src/lib/fileBlockWithdrawn.test.ts).
//
// WHAT IS DELIBERATELY UNCHANGED: the write-path refusal itself (an old client,
// a stored template or a hand-written MCP call must still be refused, not
// stored) and the two renderer arms that keep a legacy stored form honest.
//
// WHY THREE ASSERTIONS: each is a different way the type can come back — the
// allowlist, the prompt the model reads, and the validation of a model reply
// that names it anyway.

import (
	"sort"
	"strings"
	"testing"
)

func TestFileBlockIsNotAnAllowedGeneratorType(t *testing.T) {
	if ValidBlockTypes[BlockType("file")] {
		t.Errorf("ValidBlockTypes still allows `file` — the generator can mint a block no write path will store")
	}
	if p := Prompt("borang dengan medan muat naik fail"); strings.Contains(p, "file") {
		t.Errorf("the generator prompt still offers `file` to the model:\n%s", p)
	}
}

func TestGeneratorRefusesAModelReplyCarryingFileBlock(t *testing.T) {
	_, err := Parse(`{"title":"Mohon kerja","blocks":[{"id":"fl1","type":"file","value":"Resume"}]}`)
	if err == nil {
		t.Fatalf("Parse accepted a `file` block — the generator can still create one")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Errorf("the refusal does not name the block type: %v", err)
	}

	// CONTROL, both directions: a servable block still parses, so the rule
	// refuses the withdrawn type and not the shape.
	if _, err := Parse(`{"title":"Biasa","blocks":[{"id":"tx1","type":"text","value":"Nama"}]}`); err != nil {
		t.Errorf("Parse refused an ordinary text block: %v", err)
	}
}

// TestGeneratorPromptOffersExactlyTheAllowedTypes keeps the two lists in step in
// BOTH directions: a type the prompt offers but Parse rejects is a dead type the
// model will produce and the agent cannot save (the defect this card fixes), and
// a type Parse accepts but the prompt never offers is a type the generator can
// never use. Neither list can drift without this failing.
func TestGeneratorPromptOffersExactlyTheAllowedTypes(t *testing.T) {
	const marker = "Allowed block types (use only these): "
	p := Prompt("apa-apa")
	start := strings.Index(p, marker)
	if start < 0 {
		t.Fatalf("the prompt no longer carries its type list (marker %q missing)", marker)
	}
	rest := p[start+len(marker):]
	end := strings.Index(rest, ".\n")
	if end < 0 {
		t.Fatalf("the prompt's type list does not end with a full stop:\n%s", rest)
	}

	offered := map[string]bool{}
	for _, tt := range strings.Split(rest[:end], ", ") {
		offered[strings.TrimSpace(tt)] = true
	}
	if len(offered) < 10 {
		t.Fatalf("the prompt's type list parsed down to %d entries — check the list's shape:\n%s", len(offered), rest[:end])
	}

	var offeredOnly, allowedOnly []string
	for tt := range offered {
		if !ValidBlockTypes[BlockType(tt)] {
			offeredOnly = append(offeredOnly, tt)
		}
	}
	for tt := range ValidBlockTypes {
		if !offered[string(tt)] {
			allowedOnly = append(allowedOnly, string(tt))
		}
	}
	sort.Strings(offeredOnly)
	sort.Strings(allowedOnly)
	if len(offeredOnly) > 0 {
		t.Errorf("the prompt offers types Parse rejects: %v", offeredOnly)
	}
	if len(allowedOnly) > 0 {
		t.Errorf("ValidBlockTypes allows types the prompt never offers: %v", allowedOnly)
	}
}
