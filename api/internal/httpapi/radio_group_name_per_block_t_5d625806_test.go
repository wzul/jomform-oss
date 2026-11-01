package httpapi

// Card t_5d625806 — on a payment form, EVERY radio-drawing block was published
// with the same `name` attribute, so two questions were ONE HTML radio group.
//
// blockTmpl's radio and image-choice arms both emitted `name="notes"`, and the
// response renderer (`render_blocks.go` responseTmpl) does not. In HTML a radio
// button group is NOT scoped by markup nesting: per the WHATWG HTML standard an
// `input[type=radio]` `b` joins `a`'s group when they share a form owner, are in
// the same tree, and "both have a `name` attribute, their `name` attributes are
// not empty, and the value of a's `name` attribute equals the value of b's"
// (https://html.spec.whatwg.org/multipage/input.html#radio-button-group). A
// <fieldset> is not a condition — card t_79c1939a wrapped each group in one for
// the accessible name and the grouping was unaffected.
//
// So on a payment form with two radio blocks, picking an answer in the second
// cleared the first, and the `required` rule ("if an element in the group is
// required and all of the elements in the group are unchecked, then the element
// is suffering from being missing", same section) was satisfied by a selection
// in the OTHER question. Measured on the served page before this fix: after
// picking ra=k then rb=p, `document.querySelector('input[data-id=ra]:checked')`
// was null while `document.getElementById('of').checkValidity()` was true.
//
// The guard below is the grouping rule itself, read off the SERVED document:
// every radio of a block must carry that block's id as its `name` (the key the
// page's own submit script already sends the answer under — render_blocks.go's
// `fields[el.dataset.id] = el.value`), so no two blocks can share a group.

import (
	"sort"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// choiceBlock is a radio / image-choice block with the given option values.
func choiceBlock(typ, id, label string, values ...string) map[string]any {
	choices := make([]any, 0, len(values))
	for _, v := range values {
		choices = append(choices, map[string]any{"label": "Option " + strings.ToUpper(v), "value": v})
	}
	return map[string]any{"id": id, "type": typ, "value": label, "required": true, "choices": choices}
}

// radioGroupsByFormOwner applies the standard's grouping rule to a served
// document: radios that share a form owner and a non-empty `name` are one
// group. It returns the groups in the order the blocks are rendered, plus every
// radio it saw, so a test can tell "one group per question" from "no radios".
func radioGroupsByFormOwner(t *testing.T, doc *html.Node) (groupNames []string, total int) {
	t.Helper()
	formOf := func(n *html.Node) *html.Node {
		// A form-associated element's owner is its nearest ancestor <form>
		// (none of these controls carries a `form=` attribute).
		return ancestorTag(n, "form")
	}
	byKey := map[string][]*html.Node{}
	var nameOrder []string
	for _, in := range cascadia.QueryAll(doc, cascadia.MustCompile(`input[type=radio]`)) {
		total++
		form := formOf(in)
		if form == nil {
			t.Fatalf("a radio is outside every <form> — it has no form owner, so it can never be part of the block's group")
		}
		name := attrOf(in, "name")
		if name == "" {
			// Not a group at all per the same rule ("their name attributes
			// are not empty"): the question would be unanswerable and
			// unvalidatable.
			t.Errorf("a radio of block %q has no name attribute — it is in no radio button group, so the browser "+
				"cannot enforce that the question is answered", attrOf(in, "data-id"))
			continue
		}
		key := attrOf(form, "id") + "\x00" + name
		if _, seen := byKey[key]; !seen {
			nameOrder = append(nameOrder, name)
		}
		byKey[key] = append(byKey[key], in)
	}
	return nameOrder, total
}

// TestPaymentRadioBlocksAreDistinctRadioGroups is the card's guard. It drives
// the REAL call site (renderBlocks via renderPaymentPage) so a block dropped
// before the template is a red, and it reads the served document as a DOM.
func TestPaymentRadioBlocksAreDistinctRadioGroups(t *testing.T) {
	blocks := []any{
		choiceBlock("radio", "ra", "Saiz kopi", "k", "b"),
		choiceBlock("radio", "rb", "Gula", "y", "n"),
		choiceBlock("image-choice", "ic", "Pilih saiz", "s", "m", "l"),
	}
	body := renderPaymentPage(t, "rg-names", map[string]any{
		"mode": "payment", "blocks": blocks, "products": []any{},
	})
	doc := parseServedDoc(t, body)

	// Which radios each block drew, keyed by the block id the page submits.
	perBlock := map[string][]*html.Node{}
	for _, in := range cascadia.QueryAll(doc, cascadia.MustCompile(`input[type=radio]`)) {
		id := attrOf(in, "data-id")
		perBlock[id] = append(perBlock[id], in)
	}
	for _, b := range []struct {
		id   string
		want int
	}{{"ra", 2}, {"rb", 2}, {"ic", 3}} {
		if got := len(perBlock[b.id]); got != b.want {
			t.Fatalf("block %q rendered %d radios, want one per choice (%d)\nserved page:\n%s", b.id, got, b.want, body)
		}
	}

	// The defect, stated positively: one name per block, and that name is the
	// block id. Two blocks sharing a name — "notes" included — is the bug.
	for id, radios := range perBlock {
		for _, in := range radios {
			if got := attrOf(in, "name"); got != id {
				t.Errorf("a radio of block %q carries name=%q, want name=%q (the block id, which is the key the "+
					"page's submit script sends the answer under) — a name shared with another block makes the two "+
					"questions one radio group", id, got, id)
			}
		}
	}

	// And the grouping rule as a browser applies it: exactly one group per
	// question, none of them shared. This is what lets the buyer hold an
	// answer in both `ra` and `rb` at once.
	groups, total := radioGroupsByFormOwner(t, doc)
	if total != 7 {
		t.Fatalf("the page carries %d radios, want 7 (2+2+3)", total)
	}
	want := []string{"ra", "rb", "ic"}
	sort.Strings(groups)
	sort.Strings(want)
	if strings.Join(groups, ",") != strings.Join(want, ",") {
		t.Errorf("the served page forms radio groups %v, want one group per question %v.\n"+
			"A browser would let a selection in one of these groups clear the other, and `required` on the "+
			"unanswered question would be satisfied by the other's answer.\nserved page:\n%s",
			groups, want, body)
	}

	// The regression shape the card names, spelled out: no radio may be named
	// "notes" (the shared placeholder every non-radio arm still uses).
	if strings.Contains(body, `<input type="radio" name="notes"`) {
		t.Error(`the page still serves <input type="radio" name="notes"> — every radio block on the payment form ` +
			`is then ONE HTML radio group, whatever markup wraps it`)
	}
}

// TestResponseRadioBlocksAreDistinctRadioGroups holds the same invariant for the
// response renderer, which already named each radio after its field. It is here
// so that a future "make both renderers use one name" change is red on both
// surfaces, not just on the payment one.
func TestResponseRadioBlocksAreDistinctRadioGroups(t *testing.T) {
	blocks := []any{
		choiceBlock("radio", "ra", "Saiz kopi", "k", "b"),
		choiceBlock("image-choice", "ic", "Pilih saiz", "s", "m", "l"),
	}
	body := renderResponsePage(t, "rg-names-resp", map[string]any{
		"mode": "response", "blocks": blocks,
	})
	doc := parseServedDoc(t, body)

	groups, total := radioGroupsByFormOwner(t, doc)
	if total != 5 {
		t.Fatalf("the page carries %d radios, want 5 (2+3)\nserved page:\n%s", total, body)
	}
	sort.Strings(groups)
	if got := strings.Join(groups, ","); got != "ic,ra" {
		t.Errorf("the served response page forms radio groups %v, want [ic ra]\nserved page:\n%s", groups, body)
	}
}
