package customform

// Card t_f8a59664: an <embed> in a custom form body silently dropped every
// element AFTER it.
//
// buildSanitizer listed `embed` in SkipElementsContent("script", "iframe",
// "object", "embed"). bluemonday implements that as "drop every token until the
// matching end tag" (sanitize.go: StartTagToken sets skipElementContent, only
// the matching EndTagToken clears it). `embed` is a VOID element — the
// tokenizer never emits `</embed>` — so the skip ran to the end of the input:
//
//	<embed src="x"><button name="go" value="1">Go</button>  ->  ""
//
// A merchant who put one embed (video, PDF, anything) above their form lost
// every control below it while the page was served 200. Since #1360 the same
// bytes are refused at write time with `medan wajib tiada dalam HTML
// (name="…")` — a message naming the wrong cause, because the control IS in the
// HTML the merchant wrote.
//
// The `embed` entry bought nothing: embed is not in the element allowlist, so
// the start tag and every attribute are stripped regardless of the skip list,
// and a void element has no content to skip.

import (
	"strings"
	"testing"
)

// TestSanitizeHTMLKeepsControlAfterEmbed is the card's exact reproduction.
func TestSanitizeHTMLKeepsControlAfterEmbed(t *testing.T) {
	in := `<embed src="x"><button name="go" value="1">Go</button>`
	out := SanitizeHTML(in)

	if !strings.Contains(out, `<button name="go" value="1">Go</button>`) {
		t.Fatalf("the control after <embed> was dropped: in=%q out=%q", in, out)
	}
	// The fix must not make <embed> survive: the tag is still not allowed.
	if strings.Contains(strings.ToLower(out), "embed") {
		t.Errorf("embed element survived the sanitizer: %q", out)
	}
	if strings.Contains(out, `src="x"`) {
		t.Errorf("embed attribute survived the sanitizer: %q", out)
	}
}

// A body with content on both sides of the embed: what precedes it must stay
// too, and the whole tail must survive.
func TestSanitizeHTMLKeepsContentOnBothSidesOfEmbed(t *testing.T) {
	in := `<p>before</p><embed src="x" type="video/mp4"><label for="email">Email</label>` +
		`<input type="email" name="email" required><div>after</div>`
	out := SanitizeHTML(in)

	for _, want := range []string{`<p>before</p>`, `<label for="email">Email</label>`,
		`name="email"`, `<div>after</div>`} {
		if !strings.Contains(out, want) {
			t.Errorf("%s missing after sanitizing around an <embed>: out=%q", want, out)
		}
	}
}

// htmlVoidElements is the full void-element set of the HTML spec
// (https://html.spec.whatwg.org/multipage/syntax.html#void-elements). Each one
// is a tag the tokenizer emits without ever emitting a matching end tag.
var htmlVoidElements = []string{
	"area", "base", "br", "col", "embed", "hr", "img", "input",
	"link", "meta", "param", "source", "track", "wbr",
}

// The guard for the whole class, not just embed: no void element may ever sit
// in SkipElementsContent, or the same "everything after it vanishes" bug comes
// back under a different tag name. If this subtest fails, the element named in
// the failure was added to buildSanitizer's SkipElementsContent list.
func TestSanitizeHTMLKeepsFollowingContentAfterEveryVoidElement(t *testing.T) {
	const control = `<button name="go" value="1">Go</button>`
	for _, el := range htmlVoidElements {
		t.Run(el, func(t *testing.T) {
			in := `<` + el + ` data-probe="1">` + control
			out := SanitizeHTML(in)
			if !strings.Contains(out, control) {
				t.Fatalf("<%s> dropped the control after it: in=%q out=%q", el, in, out)
			}
		})
	}
}

// The impact the card describes, at the layer the merchant actually hits: the
// write-time required-control refusal must see a control that sits after an
// embed.
func TestValidateAcceptsRequiredControlAfterEmbed(t *testing.T) {
	cfg := &Config{
		HTML: `<embed src="x"><label for="email">Email</label><input type="email" name="email" required>`,
		Fields: []Field{
			{ID: "email", Type: "email", Label: "Email", Required: true},
		},
	}

	if missing := MissingRequiredControls(cfg); len(missing) != 0 {
		t.Fatalf("required control after <embed> reported missing: %v", missing)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("a satisfiable contract with an <embed> was refused: %v", err)
	}
}

// ...and the refusal still fires when the required control really is absent
// after an embed — the fix must not turn the guard off.
func TestValidateStillRefusesMissingControlAfterEmbed(t *testing.T) {
	cfg := &Config{
		HTML:   `<embed src="x"><input type="text" name="nama">`,
		Fields: []Field{{ID: "email", Type: "email", Label: "Email", Required: true}},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("a contract with no control for the required field was accepted")
	}
	if !strings.Contains(err.Error(), `medan wajib tiada dalam HTML (name="email")`) {
		t.Fatalf("refusal does not name the missing field: %v", err)
	}
}
