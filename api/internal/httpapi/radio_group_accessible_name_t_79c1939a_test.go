package httpapi

// Card t_79c1939a — a radio / image-choice group on a public form page had no
// accessible name.
//
// Both public block renderers (blockTmpl for a payment/product form,
// responseTmpl for a response form) published the group's heading as a SIBLING
//
//	<label class="lbl">Saiz kopi (required)</label>
//	<label class="rad"><input type="radio" name="notes" …> Kecil</label>
//	<label class="rad"><input type="radio" name="notes" …> Besar</label>
//
// That <label> has no `for` and no labelable descendant, so it labelled nothing
// and assistive technology had no name for the QUESTION — measured on the served
// page before the fix: `radioClosestFieldset=false`,
// `labelHasFor=false`, `fieldsetCount=0`, and the accessibility tree held four
// `radio` nodes with no enclosing group node at all.
//
// The fix is the native technique W3C APG names a radio group with — a
// <fieldset> whose first <legend> is the merchant's label
// (https://www.w3.org/WAI/ARIA/apg/practices/names-and-descriptions/) — applied
// to all four arms (radio + image-choice in both renderers), with the
// user-agent fieldset box reset in each page's own base sheet so the visible
// text, its position and the option spacing do not change.
//
// These tests drive the REAL call sites (renderBlocks / renderResponseForm via
// renderPaymentPage / renderResponsePage), never the templates by hand, and they
// read the SERVED document as a DOM — a template-level string match stays green
// while a call site drops the block before the template is reached.

import (
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// radioGroupBlock is one radio / image-choice block with two choices, in the
// shape the builder stores it.
func radioGroupBlock(typ, id, label string) map[string]any {
	return map[string]any{
		"id": id, "type": typ, "value": label, "required": true,
		"choices": []any{
			map[string]any{"label": "Kecil", "value": "k"},
			map[string]any{"label": "Besar", "value": "b"},
		},
	}
}

// parseServedDoc parses a served page into a DOM, so the assertions below are
// about the document a browser builds (where a <label> that labels nothing is
// visibly a different shape from a <legend> that names its fieldset) rather than
// about the template's bytes.
func parseServedDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse the served page: %v", err)
	}
	return doc
}

// attrOf returns an element's attribute, or "" when it is absent.
func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// trimmedText is an element's visible text with the surrounding whitespace the
// template's newlines add removed.
func trimmedText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// ancestorTag walks up to the nearest ancestor with the given tag.
func ancestorTag(n *html.Node, tag string) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == tag {
			return p
		}
	}
	return nil
}

// firstChildTag returns the first element child with the given tag.
func firstChildTag(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
	}
	return nil
}

// TestPublicFormRadioGroupsCarryAnAccessibleName is the card's guard: on BOTH
// public renderers, for BOTH radio-drawing arms, the group's radios sit inside
// one <fieldset> whose <legend> is the merchant's label plus the required
// marker, and nothing publishes that heading as a bare sibling <label>.
func TestPublicFormRadioGroupsCarryAnAccessibleName(t *testing.T) {
	surfaces := []struct {
		name string
		// render is the handler that serves the page, so a block dropped at the
		// call site is a red here rather than a template test that never runs.
		render func(t *testing.T, slug string, cfg map[string]any) string
		mode   string
		want   string // the block's own label in the served document
	}{
		{"payment", renderPaymentPage, "", ""},
		{"response", renderResponsePage, "response", ""},
	}

	for _, sf := range surfaces {
		cfg := map[string]any{
			"blocks": []any{
				radioGroupBlock("radio", "rd1", "Saiz kopi"),
				radioGroupBlock("image-choice", "ic1", "Pilih saiz"),
			},
		}
		if sf.mode != "" {
			cfg["mode"] = sf.mode
		}
		body := sf.render(t, "rg-"+sf.name, cfg)
		doc := parseServedDoc(t, body)

		for _, arm := range []struct{ typ, id, label string }{
			{"radio", "rd1", "Saiz kopi"},
			{"image-choice", "ic1", "Pilih saiz"},
		} {
			t.Run(sf.name+"/"+arm.typ, func(t *testing.T) {
				// Keyed on the BLOCK id: the response renderer names each radio
				// after its block, the payment renderer keys answers by data-id.
				var radios []*html.Node
				for _, in := range cascadia.QueryAll(doc, cascadia.MustCompile(`input[type=radio]`)) {
					if attrOf(in, "name") == arm.id || attrOf(in, "data-id") == arm.id {
						radios = append(radios, in)
					}
				}
				if len(radios) != 2 {
					t.Fatalf("the %s block rendered %d radios, want one per declared choice (2)\nserved page:\n%s",
						arm.typ, len(radios), body)
				}

				// Native constraint validation is the reason these forms need no
				// client-side guard: the group stays required.
				if _, ok := findAttr(radios[0], "required"); !ok {
					t.Errorf("the %s group's radios are no longer required — the browser's own constraint "+
						"validation was the only refusal path for an unanswered group", arm.typ)
				}

				fs := ancestorTag(radios[0], "fieldset")
				if fs == nil {
					t.Fatalf("the %s radios are NOT inside a fieldset — the group heading labels nothing, "+
						"so assistive technology has no name for the question", arm.typ)
				}
				if other := ancestorTag(radios[1], "fieldset"); other != fs {
					t.Errorf("the %s group's radios are split across different fieldsets (%p vs %p) — "+
						"one group must be one named question", arm.typ, fs, other)
				}

				legend := firstChildTag(fs, "legend")
				if legend == nil {
					t.Fatalf("the %s group's fieldset has no <legend>, so the group is unnamed", arm.typ)
				}
				wantHeading := arm.label + " (required)"
				if got := trimmedText(legend); got != wantHeading {
					t.Errorf("the %s group's legend = %q, want %q — the buyer must read the same question as before",
						arm.typ, got, wantHeading)
				}

				// The defect shape, stated positively: the element carrying the
				// heading text is that legend and nothing else. A heading <label>
				// with no `for` is what shipped.
				var carriers []*html.Node
				var walk func(*html.Node)
				walk = func(n *html.Node) {
					if n.Type == html.ElementNode && trimmedText(n) == wantHeading {
						carriers = append(carriers, n)
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c)
					}
				}
				walk(doc)
				if len(carriers) != 1 {
					var tags []string
					for _, c := range carriers {
						tags = append(tags, c.Data)
					}
					t.Fatalf("%d elements carry the %s heading %q (tags %v), want exactly one — the group's legend",
						len(carriers), arm.typ, wantHeading, tags)
				}
				if carriers[0] != legend {
					t.Errorf("the %s heading is carried by <%s> (%q is its `for`, %v it contains a control), "+
						"not by the group's <legend> — a heading label with no `for` labels nothing",
						arm.typ, carriers[0].Data, attrOf(carriers[0], "for"),
						firstChildTag(carriers[0], "input") != nil)
				}

				// The fieldset's own box is what the CSS reset is for; it is
				// asserted on the page's served stylesheet because this DOM has no
				// cascade. Without it every group gains a UA border and padding,
				// which is a visible layout change on every published form.
				for _, rule := range []string{
					".radset { border:0; margin:12px 0 8px; padding:0; min-width:0; }",
					".radset > legend { margin:0; padding:0; }",
					".radset > .rad:last-child { margin-bottom:0; }",
				} {
					if !strings.Contains(body, rule) {
						t.Errorf("the page does not reset the group's fieldset box (%q) — the user-agent "+
							"border/padding would move every radio group on the page", rule)
					}
				}
			})
		}
	}
}

// findAttr reports whether the element carries the attribute, and its value.
func findAttr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}
