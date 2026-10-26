package httpapi

import (
	"slices"
	"strings"
	"testing"
)

// WHY THIS READER EXISTS (card t_429c6097). `TestDocsEmbedPageDoesNotClaimThe
// GateReappears` asks `strings.Contains` for the sentences a READER is served,
// and it used to ask that of the page's RAW bytes. Nothing stripped comments, so
// a `//` line in the module's leading comment block satisfied every presence pin
// after the rendered copy was deleted — measured: with the PR-1200 clause removed
// from the JSX and the same words planted on ONE `//` line, the guard reported
// `ok ... 0.302s` on the tree that pin was added on. A guard satisfied by text no
// reader ever sees is not a guard; it is a substring read of a source file.
//
// The reader is therefore split in two, and the split is the point:
//
//	docsJsxCodeOnly  blanks every COMMENT, keeping every other byte (and every
//	                 newline) at its original offset.
//	docsReaderText   folds that through `normalizedDocsText`, so the pins match
//	                 the sentences a reader is served rather than a source layout.
//
// IT IS STRING-AWARE, and that is load-bearing here rather than polish: this page
// prints `https://yourworkspace.jomform.com/embed/f/...` inside template literals
// and in `metadata`. A scanner that read the `//` inside those as a line comment
// would blank the rest of the line — including the closing quote or backtick —
// and the file's remaining structure would be read through a shifted lexer.
//
// IT IS JSX-TEXT-AWARE, which is the difference between a reader and a source
// scan, and it is a defect already measured once in this repo: `web/src/lib/
// jsxSource.ts` records that a comment-opener scan that ignored JSX text read the
// prose `/v1/rest/*` on `docs/api/page.tsx` as the start of a block comment and
// blanked 41 lines — three findings were then reported against spans 180 lines
// away. A `/`+`/` in an element's CHILDREN is prose a reader reads; it can never
// open a comment. So `<…>` tags are tracked (with their attribute lists and
// `{…}` expressions) to know when bytes are children text, and only outside that
// text is `//` or `/*` honoured.
//
// WHAT IT DELIBERATELY KEEPS: JSX text, string literals, template literals and
// code. A sentence rendered through an expression (`{MESSAGE_CONTRACT.map(…)}`,
// `{'a literal'}`) is still text a reader is served, and dropping it would turn
// the guard red on a page that is correct — a loud failure, but a wrong one.
//
// WHAT IT CANNOT SEE, stated rather than assumed: a regex literal containing
// `//` or `/*` (this tree's pages carry none) would be read as a comment opener.
// The blast radius is bounded the same way `jsxSource.ts` bounds it — the damage
// stops at the line for `//`, and a false opener can only ever REMOVE text from
// the reader's view, which reds a presence pin (loud) instead of satisfying one
// silently.
func docsJsxCodeOnly(src string) string {
	const (
		inCode = iota
		inTag
		inJSXText
		inLineComment
		inBlockComment
		inString
	)
	// A `{…}` expression opened from inside a tag attribute list ('t'), from JSX
	// children ('j') or from a template literal's `${…}` ('m'). `depth` counts the
	// nested braces so the matching `}` is the one that ends the expression.
	type frame struct {
		kind  byte
		depth int
	}

	var (
		out        strings.Builder
		stack      []frame
		state      = inCode
		quote      byte
		returnTo   = inCode
		jsxDepth   int
		tagClosing bool
		tagLast    byte
	)
	out.Grow(len(src))

	// A blanked byte keeps the file's shape: newlines survive so a `file:line`
	// reported from an offset stays true.
	blank := func(b byte) {
		if b == '\n' {
			out.WriteByte('\n')
		} else {
			out.WriteByte(' ')
		}
	}

	for i := 0; i < len(src); i++ {
		c := src[i]
		var n byte
		if i+1 < len(src) {
			n = src[i+1]
		}
		switch state {
		case inCode:
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				switch {
				case c == '{':
					top.depth++
					out.WriteByte(c)
					continue
				case c == '}' && top.depth > 0:
					top.depth--
					out.WriteByte(c)
					continue
				case c == '}':
					kind := top.kind
					stack = stack[:len(stack)-1]
					out.WriteByte(c)
					switch kind {
					case 'j':
						state = inJSXText
					case 't':
						state = inTag
					case 'm':
						state = inString
						quote = '`'
					}
					continue
				}
			}
			if c == '/' && n == '/' {
				state = inLineComment
				out.WriteString("  ")
				i++
				continue
			}
			if c == '/' && n == '*' {
				state = inBlockComment
				out.WriteString("  ")
				i++
				continue
			}
			// `"`/backtick always open a literal; `'` only in a literal position, so
			// a prose apostrophe in JSX text (`don't`) cannot open a string.
			if c == '"' || c == '`' || (c == '\'' && !docsWordByte(docsByteAt(src, i-1))) {
				state = inString
				quote = c
				returnTo = inCode
				out.WriteByte(c)
				continue
			}
			if c == '<' && docsJSXTagMayStart(src, i) {
				tagClosing = n == '/'
				tagLast = 0
				state = inTag
				out.WriteByte(c)
				continue
			}
			out.WriteByte(c)

		case inTag:
			if c == '"' || c == '\'' || c == '`' {
				state = inString
				quote = c
				returnTo = inTag
				out.WriteByte(c)
				continue
			}
			if c == '{' {
				stack = append(stack, frame{kind: 't'})
				state = inCode
				out.WriteByte(c)
				continue
			}
			if c == '>' {
				out.WriteByte(c)
				selfClosing := tagLast == '/'
				switch {
				case tagClosing:
					if jsxDepth > 0 {
						jsxDepth--
					}
				case !selfClosing:
					jsxDepth++
				}
				if jsxDepth > 0 {
					state = inJSXText
				} else {
					state = inCode
				}
				continue
			}
			if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				tagLast = c
			}
			out.WriteByte(c)

		case inJSXText:
			if c == '{' {
				stack = append(stack, frame{kind: 'j'})
				state = inCode
				out.WriteByte(c)
				continue
			}
			// In children text, `<` + name/`/`/`>` opens a tag; a `/` here is prose.
			if c == '<' && docsJSXTagStartsHere(n) {
				tagClosing = n == '/'
				tagLast = 0
				state = inTag
				out.WriteByte(c)
				continue
			}
			out.WriteByte(c)

		case inLineComment:
			if c == '\n' {
				state = inCode
			}
			blank(c)

		case inBlockComment:
			if c == '*' && n == '/' {
				state = inCode
				out.WriteString("  ")
				i++
				continue
			}
			blank(c)

		case inString:
			if c == '\\' && n != 0 {
				out.WriteByte(c)
				out.WriteByte(n)
				i++
				continue
			}
			if quote == '`' && c == '$' && n == '{' {
				stack = append(stack, frame{kind: 'm'})
				state = inCode
				out.WriteString("${")
				i++
				continue
			}
			if c == quote {
				state = returnTo
				out.WriteByte(c)
				continue
			}
			// An unterminated quote stops at the newline rather than eating the file.
			if c == '\n' && quote != '`' {
				state = returnTo
				out.WriteByte(c)
				continue
			}
			out.WriteByte(c)
		}
	}
	return out.String()
}

// docsReaderText is what the docs guards match against: the page's comments
// blanked, then folded into the sentences a reader sees.
func docsReaderText(src string) string {
	return normalizedDocsText(docsJsxCodeOnly(src))
}

// docsByteAt is src[i], or 0 outside the string.
func docsByteAt(src string, i int) byte {
	if i < 0 || i >= len(src) {
		return 0
	}
	return src[i]
}

// docsWordByte reports whether b can be part of an identifier, which is what
// decides whether a `'` is a string delimiter or a prose apostrophe.
func docsWordByte(b byte) bool {
	return b == '_' || b == '$' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// docsJSXTagStartsHere reports whether a `<` inside JSX children text opens a
// tag — the JSX grammar's rule: an element, a fragment, or a closing tag.
func docsJSXTagStartsHere(n byte) bool {
	return n == '/' || n == '>' || n == '_' || n == '$' || docsWordByte(n)
}

// docsJSXTagMayStart reports whether a `<` in CODE position opens JSX rather than
// being a comparison or a type-argument list. `a < b` and `Array<string>` keep the
// `<` preceded by a word character, and neither is markup; `return <div>`,
// `cond ? <a/> : <b/>` and `=> <div>` are. A `<` that is not recognised here is
// simply read as code, so a miss costs the JSX-text awareness on that element —
// which the `{}`/string tracking still bounds — instead of corrupting the read.
func docsJSXTagMayStart(src string, i int) bool {
	n := docsByteAt(src, i+1)
	if !docsJSXTagStartsHere(n) {
		return false
	}
	// A generic parameter list opens like a tag and is CODE: `<T,>(x: T) => x` and
	// `<T extends object>(…)` both spell `<name` followed by `,` or `extends`,
	// which no JSX element ever does. Reading one as an element would push the
	// lexer into children text and keep the comments that follow — the silent
	// direction, so it is ruled out rather than left to the shape of today's file.
	if n != '/' && n != '>' {
		j := i + 2
		for j < len(src) && (docsWordByte(src[j]) || src[j] == '.' || src[j] == '-') {
			j++
		}
		k := j
		for k < len(src) && (src[k] == ' ' || src[k] == '	') {
			k++
		}
		if docsByteAt(src, k) == ',' || strings.HasPrefix(src[k:], "extends ") || strings.HasPrefix(src[k:], "extends>") {
			return false
		}
	}
	j := i - 1
	for j >= 0 && (src[j] == ' ' || src[j] == '\t' || src[j] == '\n' || src[j] == '\r') {
		j--
	}
	if j < 0 {
		return true
	}
	switch src[j] {
	case '(', ',', '=', '?', ':', '&', '|', '{', '[', ';', '>', '!', ')':
		return true
	}
	if !docsWordByte(src[j]) {
		return false
	}
	k := j
	for k >= 0 && docsWordByte(src[k]) {
		k--
	}
	switch src[k+1 : j+1] {
	case "return", "yield", "case", "default", "else", "do":
		return true
	}
	return false
}

// TestDocsJsxCodeOnlyBlanksCommentsAndKeepsReaderText pins the reader's whole
// contract on synthetic sources, because every failure it can have is silent:
// a comment it fails to blank satisfies a presence pin, and text it blanks by
// mistake reds one on a page that is correct.
func TestDocsJsxCodeOnlyBlanksCommentsAndKeepsReaderText(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		keep    []string
		blanked []string
	}{
		{
			name:    "line comment at module scope",
			src:     "// the gate reappears here\nconst a = 1;\n",
			keep:    []string{"const a = 1;"},
			blanked: []string{"gate reappears"},
		},
		{
			name:    "block comment at module scope, over two lines",
			src:     "/* the gate reappears\n   and again */\nconst a = 1;",
			keep:    []string{"const a = 1;"},
			blanked: []string{"gate reappears", "and again"},
		},
		{
			name:    "jsx comment in an element's children",
			src:     "const x = <div>{/* the gate reappears */}<p>kept prose</p></div>;",
			keep:    []string{"kept prose", "<p>", "</div>"},
			blanked: []string{"gate reappears"},
		},
		{
			name:    "a trailing line comment after code",
			src:     "const a = 1; // the gate reappears\nconst b = 2;",
			keep:    []string{"const a = 1;", "const b = 2;"},
			blanked: []string{"gate reappears"},
		},
		{
			// The measured `jsxSource.ts` defect: `//` and `/*` inside JSX prose.
			name:    "jsx prose keeps // and /* and a closing tag",
			src:     "const x = <p>see https://example.com/docs and /v1/rest/*</p>;",
			keep:    []string{"https://example.com/docs", "/v1/rest/*", "</p>"},
			blanked: nil,
		},
		{
			name:    "a string literal keeps its //",
			src:     "const u = \"https://yourworkspace.jomform.com/embed/f/w/s\"; // gone\n",
			keep:    []string{"https://yourworkspace.jomform.com/embed/f/w/s"},
			blanked: []string{"gone"},
		},
		{
			name:    "a template literal keeps its // and its interpolation",
			src:     "const u = `<a href=\"https://x.test/${name}\">go</a>`; // gone\n",
			keep:    []string{"https://x.test/", "go"},
			blanked: []string{"gone"},
		},
		{
			// A JSX expression can hold a real comment; the tag text around it stays.
			name:    "a comment inside a jsx attribute expression",
			src:     "const x = <a href={/* why */ url} title=\"t\">prose</a>;",
			keep:    []string{"href={", "url}", "title=\"t\"", "prose"},
			blanked: []string{"why"},
		},
		{
			name:    "a comparison and a type argument are not tags",
			src:     "const ok = a < b; const xs: Array<string> = []; const y = 1; /* c */\n",
			keep:    []string{"a < b", "Array<string>", "const y = 1;"},
			blanked: []string{"/* c */"},
		},
		{
			// A generic parameter list opens like a tag; reading it as one would
			// swallow the comment that follows into "children text".
			name:    "a generic arrow parameter list is not a tag",
			src:     "const f = <T,>(x: T) => x; // gone\nconst g = <T extends object>(x: T) => x; /* gone too */\n",
			keep:    []string{"const f = ", "const g = ", "extends object"},
			blanked: []string{"gone"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := docsJsxCodeOnly(tc.src)
			if len(got) != len(tc.src) {
				t.Fatalf("length %d != %d — the reader must keep every byte offset", len(got), len(tc.src))
			}
			if n := strings.Count(got, "\n"); n != strings.Count(tc.src, "\n") {
				t.Errorf("newlines %d != %d — blanking a newline shifts every line number after it", n, strings.Count(tc.src, "\n"))
			}
			for _, keep := range tc.keep {
				if !strings.Contains(got, keep) {
					t.Errorf("the reader dropped %q, which a reader IS served:\n%s", keep, got)
				}
			}
			for _, blanked := range tc.blanked {
				if strings.Contains(got, blanked) {
					t.Errorf("the reader kept %q, which sits in a comment a reader is never served:\n%s", blanked, got)
				}
			}
		})
	}
}

// The shipped page is the reader's real input, so the same invariants are held
// against it: same length, same newline count, and every sentence the paragraph
// guard pins still present while the leading comment block's prose is gone.
func TestDocsJsxCodeOnlyOnTheEmbedPage(t *testing.T) {
	src := readFileString(t, docsEmbedPagePath(t))
	code := docsJsxCodeOnly(src)
	if len(code) != len(src) {
		t.Fatalf("length %d != %d — offsets moved", len(code), len(src))
	}
	if strings.Count(code, "\n") != strings.Count(src, "\n") {
		t.Fatalf("newline count changed — line numbers would shift")
	}
	// The leading comment block's prose is blanked…
	if strings.Contains(code, "THE SNIPPET USES /embed/f/") {
		t.Error("the leading comment block is still readable — comments are not being blanked on the page")
	}
	// …and the rendered prose that carries the comment's own hazard is not:
	// `/embed/f/` is printed as JSX text and in a template literal, and both of
	// those must survive for the snippet guards and these pins to read them.
	if strings.Count(code, "/embed/f/") < 2 {
		t.Errorf("/embed/f/ survived %d time(s) in the reader output — the page prints it in prose AND in "+
			"the snippet, so a comment scan that ate JSX text is blanking rendered copy", strings.Count(code, "/embed/f/"))
	}
	if !strings.Contains(code, "https://yourworkspace.jomform.com/embed/f/your-workspace/your-form-slug") {
		t.Error("the iframe snippet's address is gone — a `//` inside a string or template literal was read as a comment opener")
	}
	// The plain-URL guard reads the same output now, and its two presence pins
	// must survive the reader or it would red on correct copy.
	for _, want := range []string{"X-Frame-Options", "/embed/f/"} {
		if !strings.Contains(docsReaderText(src), want) {
			t.Errorf("%q is gone from the reader output — the plain-URL guard's own pin would red on a page that "+
				"states it", want)
		}
	}
}

// TestDocsEmbedParagraphPinsReadOnlyRenderedText is the BITE PROOF for the reader
// above, driven through the same helper the guard calls (card t_429c6097).
//
// The defect it proves fixed: the paragraph's pins were asked of the page's RAW
// bytes, so with the whole clause deleted from the JSX and the same words planted
// on ONE `//` line in the module's leading comment block, the guard reported
// `ok ... 0.302s` on the tree the clause pin shipped on. Each comment shape below
// is that probe, in memory: the rendered copy is removed and the words are left
// where no reader is ever served.
//
// Two measured details are encoded rather than assumed, because a sloppy probe
// reports a false "the guard bit":
//
//   - the literal must SURVIVE the comment marker, so the plant is on ONE line. A
//     clause split across two `//` lines is not contiguous once the markers are
//     between the halves, so `strings.Count` on the rendered literal would never
//     match it and the probe would "pass" without proving anything;
//   - every anchor is asserted to occur EXACTLY ONCE, so a mutation that silently
//     does nothing fails here instead of reading as a green proof.
func TestDocsEmbedParagraphPinsReadOnlyRenderedText(t *testing.T) {
	src := readFileString(t, docsEmbedPagePath(t))

	// The shipped page satisfies every pin through the reader: the fix must not
	// have turned a guard that was green into one that reds on correct copy.
	if missing := docsEmbedParagraphPinsMissing(src); len(missing) != 0 {
		t.Fatalf("the shipped page no longer satisfies %d paragraph pin(s): %v — the reader is "+
			"dropping text a reader IS served", len(missing), docsEmbedPinNames(missing))
	}

	// The clause under test in the two forms the probe needs: as a reader reads it,
	// and as the page's source lays it out (split across a line break).
	const (
		clauseRendered = "while a second page of your own site that embeds the same form does not ask for the password again"
		clauseSource   = "while a second page of your own site\n        that embeds the same form does not ask for the password again."
	)
	const (
		anchorLeadingComment = "// Issue #456: iframe embed docs. Follow-up from #60 (embed payment flow) and\n"
		anchorImport         = "import type { Metadata } from \"next\";\n"
		anchorRootDiv        = "    <div className=\"max-w-3xl\">\n"
	)
	for _, a := range []struct{ what, anchor string }{
		{"the module's leading comment block", anchorLeadingComment},
		{"the module's import line", anchorImport},
		{"the root element", anchorRootDiv},
		{"the clause itself", clauseSource},
	} {
		if n := strings.Count(src, a.anchor); n != 1 {
			t.Fatalf("anchor for %s occurs %d times in %s, want exactly 1 — the mutation below would "+
				"not do what it claims", a.what, n, docsEmbedPagePath(t))
		}
	}
	if !strings.Contains(docsReaderText(src), clauseRendered) {
		t.Fatalf("the shipped page does not carry the clause as rendered text — this proof would be vacuous")
	}

	// The deletion the guard exists to catch: the rendered copy is gone.
	deleted := strings.Replace(src, clauseSource, "", 1)
	if strings.Contains(docsReaderText(deleted), clauseRendered) {
		t.Fatalf("the clause still reads as served text after its source span was removed — the deletion " +
			"probe is wrong")
	}

	// 1. Control: deleted with nothing standing in for it, the pin reds.
	if !slices.Contains(docsEmbedPinNames(docsEmbedParagraphPinsMissing(deleted)), "second-page-consequence") {
		t.Fatalf("deleting the clause did not red its own pin — the pin below is not the thing doing the work")
	}

	// 2. Deleted from the JSX and planted in each COMMENT shape: the pin still
	// reds, because no reader is served a comment.
	for _, tc := range []struct{ shape, mutant string }{
		{
			shape: "a `//` line in the module's leading comment block",
			mutant: strings.Replace(deleted, anchorLeadingComment,
				anchorLeadingComment+"// "+clauseRendered+"\n", 1),
		},
		{
			shape: "a `{/* … */}` comment in the element's children",
			mutant: strings.Replace(deleted, anchorRootDiv,
				anchorRootDiv+"      {/* "+clauseRendered+" */}\n", 1),
		},
		{
			shape: "a module-scope `/* … */` block comment",
			mutant: strings.Replace(deleted, anchorImport,
				anchorImport+"/* "+clauseRendered+" */\n", 1),
		},
	} {
		if strings.Contains(docsReaderText(tc.mutant), clauseRendered) {
			t.Errorf("the reader still sees the clause when it is planted in %s — a comment is satisfying "+
				"the pin with the rendered copy deleted", tc.shape)
		}
		if missing := docsEmbedPinNames(docsEmbedParagraphPinsMissing(tc.mutant)); !slices.Contains(missing, "second-page-consequence") {
			t.Errorf("the clause planted in %s satisfies the 'second-page-consequence' pin although the "+
				"rendered copy is deleted — that is the measured defect (ok 0.302s on tree a85c6fc3): the "+
				"guard is reading source no reader is served", tc.shape)
		}
	}

	// 3. The same words as rendered JSX stay green. Put the clause back as an
	// element's own text (not a comment, not a string) and the pin is satisfied
	// again — so the fix is a reader, not a blanket ban on the sentence.
	rendered := strings.Replace(deleted, anchorRootDiv,
		anchorRootDiv+"      <p>\n        "+clauseRendered+"\n      </p>\n", 1)
	if missing := docsEmbedParagraphPinsMissing(rendered); len(missing) != 0 {
		t.Errorf("the clause rendered as JSX text no longer satisfies %v — the reader is now blanking text "+
			"a reader IS served", docsEmbedPinNames(missing))
	}
}

// docsEmbedPinNames names the pins in a failure message, so a red says WHICH
// sentence went missing instead of dumping six structs.
func docsEmbedPinNames(pins []docsEmbedParagraphPin) []string {
	out := make([]string, 0, len(pins))
	for _, p := range pins {
		out = append(out, p.name)
	}
	return out
}
