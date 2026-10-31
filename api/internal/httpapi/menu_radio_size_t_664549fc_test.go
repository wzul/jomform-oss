package httpapi

// Card t_664549fc — the menu checkout sheet sizes RADIO inputs like buyer fields.
//
// `.sheetfoot input { width:100%; height:44px; … }` is the buyer-field rule
// (name / email / text / number), and it also matched the radio of every
// radio and image-choice option. Inside the option's flex row that made the
// control 399x44 of a 448px row, so the merchant's label ("Kecil") was pushed
// to the row's far edge — on a 375px viewport, clipped off the screen. The
// ordinary form renderer's radio is 13x13 (render_blocks.go), so this was the
// menu sheet's own rule, not a general form defect.
//
// What is pinned here is the CASCADE, not a string: the tests take the
// stylesheet the handler actually served, compile each selector with a real
// CSS engine (cascadia), match it against the RADIO INPUT NODE the template
// actually emitted, and resolve width/height/margin the way a browser does —
// highest specificity wins, ties go to the later rule. So the guard is red if
// the reset rule is dropped, if a later equal-specificity rule takes the
// declaration back, or if the reset is written in a way that also catches the
// buyer fields (that is the second test).
//
// Scope, stated plainly: this models the cascade over the served markup; it
// does not do layout. The live geometry (13x13 radio, 448x44 buyer field, both
// widths, verified in a browser against the built binary) is on the card.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// menuChoiceGroupsFixture renders the two arms this defect covered — a `radio`
// group and an `image-choice` group — through the menu checkout's own field
// derivation, so the guarded markup is the markup the handler serves.
func menuChoiceGroupsFixture(t *testing.T) workspaceMenuData {
	t.Helper()
	cfg := map[string]any{"mode": "response", "blocks": []any{
		map[string]any{"id": "sd1", "type": "radio", "value": "Pilih saiz", "required": true,
			"choices": []any{
				map[string]any{"label": "Kecil", "value": "k"},
				map[string]any{"label": "Besar", "value": "b"},
			}},
		map[string]any{"id": "ic1", "type": "image-choice", "value": "Saiz kopi", "required": true,
			"choices": []any{
				map[string]any{"label": "Kecil", "value": "k"},
				map[string]any{"label": "Besar", "value": "b"},
			}},
	}}
	contract := menuExtraContract(cfg)
	if len(contract) != 2 {
		t.Fatalf("fixture defect: menuExtraContract gave %d fields, want the radio and image-choice blocks", len(contract))
	}
	d := menu492Fixture(t)
	d.ExtraFields = menuExtraFields(contract, formLangs["en"])
	return d
}

// ── reading + resolving the served stylesheet ───────────────────────────────

// sheetRule is one declaration block of the served stylesheet, with the
// specificity of the single selector it was written under.
type sheetRule struct {
	selector string
	sel      cascadia.Sel
	spec     cascadia.Specificity
	decls    map[string]string
	order    int
}

// servedStyleRules returns every rule of every <style> element the page served,
// in document order (which is the cascade's tie-breaker). Comments are stripped
// first: a `/* … */` in the sheet would otherwise be read as a selector.
func servedStyleRules(t *testing.T, body string) []sheetRule {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse served HTML: %v", err)
	}
	var css strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "style" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				css.WriteString(c.Data)
				css.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	sheet := stripCSSComments(css.String())
	if strings.TrimSpace(sheet) == "" {
		t.Fatalf("the served page carries no <style> rules — nothing to resolve")
	}

	var rules []sheetRule
	order := 0
	for _, chunk := range strings.Split(sheet, "}") {
		open := strings.LastIndex(chunk, "{")
		if open < 0 {
			continue
		}
		selectorList := strings.TrimSpace(chunk[:open])
		body := chunk[open+1:]
		if selectorList == "" {
			continue
		}
		decls := cssDeclarations(body)
		for _, selector := range strings.Split(selectorList, ",") {
			selector = strings.TrimSpace(selector)
			if selector == "" {
				continue
			}
			sel, err := cascadia.Parse(selector)
			if err != nil {
				// A selector the engine cannot compile is not silently ignored
				// when it could compete for an input: that would let a losing
				// rule hide behind a parse error and make this guard agree
				// with itself instead of with the browser.
				if strings.Contains(selector, "input") {
					t.Fatalf("the served sheet carries an input selector this guard cannot compile: %q (%v)", selector, err)
				}
				continue
			}
			rules = append(rules, sheetRule{selector: selector, sel: sel, spec: sel.Specificity(), decls: decls, order: order})
			order++
		}
	}
	if len(rules) == 0 {
		t.Fatalf("parsed no rules out of the served stylesheet")
	}
	return rules
}

func stripCSSComments(s string) string {
	for {
		open := strings.Index(s, "/*")
		if open < 0 {
			return s
		}
		close := strings.Index(s[open+2:], "*/")
		if close < 0 {
			return s[:open]
		}
		s = s[:open] + s[open+2+close+2:]
	}
}

// cssDeclarations splits a rule body into `property: value` pairs. The last
// declaration for a property wins, exactly as in CSS, and the `margin` /
// `padding` shorthands are expanded into their four side properties — without
// that, a rule that says `margin:0` would look like it never touched
// `margin-top` and a losing `margin-top:5px` rule would win the comparison.
func cssDeclarations(body string) map[string]string {
	out := map[string]string{}
	expanded := func(prop, value string) {
		if prop != "margin" && prop != "padding" {
			out[prop] = value
			return
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return
		}
		top := fields[0]
		right, bottom, left := top, top, top
		switch len(fields) {
		case 2:
			right, left = fields[1], fields[1]
		case 3:
			right, bottom, left = fields[1], fields[2], fields[1]
		case 4:
			right, bottom, left = fields[1], fields[2], fields[3]
		}
		out[prop+"-top"] = top
		out[prop+"-right"] = right
		out[prop+"-bottom"] = bottom
		out[prop+"-left"] = left
	}
	for _, decl := range strings.Split(body, ";") {
		colon := strings.Index(decl, ":")
		if colon < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(decl[:colon]))
		value := strings.TrimSpace(decl[colon+1:])
		if prop == "" || value == "" {
			continue
		}
		expanded(prop, value)
	}
	return out
}

// cascadeWinner resolves `prop` for `node` the way the browser does: among the
// served rules whose selector matches the node, the highest specificity wins
// and equal specificity goes to the later rule in document order.
type cascadeWin struct {
	value    string
	selector string
	spec     cascadia.Specificity
	order    int
	found    bool
}

func cascadeWinner(rules []sheetRule, node *html.Node, prop string) cascadeWin {
	var win cascadeWin
	for _, r := range rules {
		value, ok := r.decls[prop]
		if !ok || !r.sel.Match(node) {
			continue
		}
		if !win.found || win.spec.Less(r.spec) || (r.spec == win.spec && r.order > win.order) {
			win = cascadeWin{value: value, selector: r.selector, spec: r.spec, order: r.order, found: true}
		}
	}
	return win
}

// ── the served page's real nodes ────────────────────────────────────────────

// servedNode returns the first element of `tag` in the served page matching
// `keep`, so the assertions resolve against the template's own markup.
func servedNode(t *testing.T, body, tag string, keep func(*html.Node) bool) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse served HTML: %v", err)
	}
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == tag && keep(n) {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if found == nil {
		t.Fatalf("the served page has no <%s> matching the guard's fixture — the markup this test resolves against is gone", tag)
	}
	return found
}

func attrValue(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// inChoiceGroup reports whether n sits inside the checkout's option group
// wrapper (`.radios`), which is what the reset rule is scoped to.
func inChoiceGroup(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode {
			for _, cls := range strings.Fields(attrValue(p, "class")) {
				if cls == "radios" {
					return true
				}
			}
		}
	}
	return false
}

// optionRadio resolves the radio input of the checkout's group named `name`.
func optionRadio(t *testing.T, body, name string) *html.Node {
	t.Helper()
	return servedNode(t, body, "input", func(n *html.Node) bool {
		return attrValue(n, "type") == "radio" && attrValue(n, "name") == name && inChoiceGroup(n)
	})
}

// ── the card ────────────────────────────────────────────────────────────────

// TestMenuCheckoutOptionControlsKeepTheirIntrinsicSize is the card: the sheet's
// buyer-field rule must not size the option's control. Both drawing arms are
// driven, because both paint `label.rad > input[type=radio]`.
func TestMenuCheckoutOptionControlsKeepTheirIntrinsicSize(t *testing.T) {
	body := renderMenu492(t, menuChoiceGroupsFixture(t))
	rules := servedStyleRules(t, body)

	for _, arm := range []struct{ block, name string }{
		{"radio", "sd1"},
		{"image-choice", "ic1"},
	} {
		t.Run(arm.block, func(t *testing.T) {
			radio := optionRadio(t, body, arm.name)
			for _, prop := range []string{"width", "height"} {
				win := cascadeWinner(rules, radio, prop)
				if !win.found {
					t.Fatalf("no served rule sets %s on the %s option's radio — the control falls back to the user "+
						"agent's size, which is not what the sheet's buyer-field rule did", prop, arm.block)
				}
				if win.value != "auto" {
					t.Errorf("the %s option's radio resolves %s:%s from `%s`, want auto: the buyer-field rule "+
						"(`.sheetfoot input{width:100%%;height:44px}`) also matches the option's control, so it fills "+
						"the row and pushes the merchant's label to the row edge (card t_664549fc)", arm.block, prop, win.value, win.selector)
				}
			}
			// The margin was part of the same rule: 5px on the control shifted
			// the option row's baseline as well as its height (49px vs 16px).
			if win := cascadeWinner(rules, radio, "margin-top"); win.found && win.value != "0" && win.value != "0px" {
				t.Errorf("the %s option's radio resolves margin-top:%s from `%s`, want 0", arm.block, win.value, win.selector)
			}
		})
	}
}

// TestMenuCheckoutBuyerFieldsStayFullWidth is the other side of the same rule:
// the reset must be scoped to the choice group. Widening it to every input in
// the sheet would leave the buyer typing their name into a 13px box.
func TestMenuCheckoutBuyerFieldsStayFullWidth(t *testing.T) {
	body := renderMenu492(t, menuChoiceGroupsFixture(t))
	rules := servedStyleRules(t, body)

	for _, field := range []struct{ id, name string }{
		{"menuName", "full_name"},
		{"menuEmail", "email"},
	} {
		node := servedNode(t, body, "input", func(n *html.Node) bool {
			return attrValue(n, "id") == field.id || attrValue(n, "name") == field.name
		})
		width := cascadeWinner(rules, node, "width")
		if !width.found || width.value != "100%" {
			t.Errorf("the buyer %s field resolves width:%q from `%s`, want 100%% — the option-control reset must not "+
				"catch the buyer fields (card t_664549fc)", field.name, width.value, width.selector)
		}
		height := cascadeWinner(rules, node, "height")
		if !height.found || height.value != "44px" {
			t.Errorf("the buyer %s field resolves height:%q from `%s`, want 44px", field.name, height.value, height.selector)
		}
	}
}

// TestServedStyleRulesResolveASizesRadioNotTheBaseRule is the guard's own
// self-check: if the cascade model stopped seeing the generic `.sheetfoot input`
// rule (a renamed selector, a stylesheet the parser skipped), the two tests
// above would pass for the wrong reason. A bare radio input under `.sheetfoot`,
// outside the option group, must still resolve to the buyer-field size.
func TestServedStyleRulesResolveASizesRadioNotTheBaseRule(t *testing.T) {
	body := renderMenu492(t, menuChoiceGroupsFixture(t))
	rules := servedStyleRules(t, body)

	doc, err := html.Parse(strings.NewReader(`<div class="sheetfoot"><input type="radio" name="bare"></div>`))
	if err != nil {
		t.Fatalf("parse synthetic markup: %v", err)
	}
	var bare *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "input" {
			bare = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if bare == nil {
		t.Fatalf("synthetic markup defect: no input node")
	}
	width := cascadeWinner(rules, bare, "width")
	if !width.found || width.value != "100%" {
		t.Fatalf("a radio input under .sheetfoot but outside .radios resolves width:%q from `%s`, want 100%% — either "+
			"the sheet's buyer-field rule is gone (so the card's fix cannot be measured) or the option reset is no longer "+
			"scoped to the choice group: %s", width.value, width.selector, fmt.Sprintf("rules=%d", len(rules)))
	}
}
