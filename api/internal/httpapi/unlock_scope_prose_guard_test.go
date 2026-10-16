package httpapi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Guard for the DEVELOPER-FACING prose that describes the unlock cookie's scope
// (card t_a29148e5, the follow-up to card t_87697ea4 / PR 1200).
//
// THE DEFECT. PR 1200 corrected the reader-facing copy: a Partitioned (CHIPS)
// cookie is keyed by top-level SITE, never by page — MDN, "Cookies Having
// Independent Partitioned State": "a separate cookie jar per top-level site",
// and the partition key "is based on the site, including the scheme, of the
// top-level URL". The implementation agrees, and the two halves of this repo have
// to say the same thing: newFormAccessCookie sets Path "/" and the unlock token
// is bound to the FORM id (signFormAccessToken), so an unlock entered on one page
// of a merchant's site IS sent on that site's other pages, while a second
// merchant's site is a different partition and asks again.
//
// One copy survived that correction, because it is not on a reader-facing
// surface: the t.Errorf MESSAGE of TestEmbeddedPrivateFormUnlockRoundTrip in
// embed_docs_snippet_test.go, which told the developer who hit it that the unlock
// was scoped to "the one merchant page it was entered on". The docs guard that
// owns the sentence on the served copy
// (TestNoDocsClaimTheGateReappearsOnACookieBlockingBrowser) walks web/ ONLY, and
// it says why in its own comment: "an api/*.go comment may quote the wrong
// phrasing while describing the defect, so judging it would make the guard cry
// wolf on its own documentation". That is true of a SUBSTRING scan — and it is
// exactly the gap this guard closes, because a scan CAN tell the two apart.
//
// THE RULE. Every `.go` file git can see under api/ is read as prose spans: a
// `//` comment block (its consecutive lines joined, so a sentence wrapped across
// them is read as one sentence) and each `"`-delimited literal's CONTENT (its
// `" +` / `"` continuation chunks joined, and the literal's own delimiters
// dropped, so the message a developer is shown is judged as the sentence they
// read rather than as source). A hit of a scope shape is a MENTION — naming a
// claim in order to refute or pin it — and is allowed when either
//
//  1. the shape IS the whole literal (a claim PATTERN in a must-be-absent list,
//     the form the docs guard's own shape slice holds them in), or
//  2. a quotation mark sits immediately BESIDE it: before the claim, after it,
//     or after the `\"` an in-message quotation is written with. That is what a
//     quoted false sentence ("scoped to the one page it was entered on") and
//     both halves of a mutation pair (`"per top-level site" -> "per top-level
//     page"`) look like.
//
// Anything else STATES the claim, names the file, line, shape and prose, and is a
// failure. Quoting the claim ITSELF is the escape hatch, and it is deliberate:
// this guard forbids USING the false scope, not talking about it — a guard that
// forbade the mention would forbid the documentation of the defect and could be
// satisfied by deleting the wrong claim from the shapes that prove it is absent.
// The quote must be beside the CLAIM, not around a longer sentence that contains
// it: a longer quotation is prose this rule cannot tell from an assertion (it
// reads the characters beside the claim, not the intent), and the failure
// message says so.
//
// WHAT IS MEASURED ON THIS TREE (not assumed). 712 api/*.go files (711 tracked
// plus this one), 33 hits of the shapes below: 14 patterns and 19 mentions, and
// 0 STATEMENTS — the carded message was the only one, and the bite proof at the
// bottom of this file re-measures it both ways (put the old wording back and this
// guard reds naming it; narrow the corrected sentence back to a page scope and it
// reds naming that). The patterns are the must-be-absent slice in
// embed_docs_snippet_test.go (:493-500 — 5 of its 7 elements, :494-498, are
// shapes here) and the shape names in this file's own const block, which is
// judged by this rule like every other file. The mentions
// are the two comment blocks there that quote the corrected claim and the
// mutation that measured it, the message that quotes the false sentence as the
// assertion it failed, this file's own prose, and the doc comment on
// newFormAccessCookie in public.go, which quotes "the one merchant page" as the
// wording it REPLACED. The buyer-per-page rate-limit prose is deliberately not a
// shape: public.go:216 and unlock_ratelimit_test.go:272 say "one merchant page"
// about a buyer bucket, which is a different claim (that one page's buyers can
// have their own bucket), and the bare nouns "top-level page"/"top-level domain"
// are NOT shapes either — passkey.go:225 says "top-level domain" in ordinary
// WebAuthn prose, so a bare-noun shape would red an unrelated sentence. Every
// shape here is a SCOPE-STATING phrase ("scoped to …", "per top-level …", "the
// one merchant page … it was entered on"), which is the defect class. The list is
// not complete in principle — prose can carry the claim in a wording no shape
// matches, and no scan can see that — so a wording that gets past it is the
// signal to extend the list, and the behaviour half the message names is pinned
// by TestUnlockCookieCarriesToAnotherPageOfTheSameSite.
//
// The spans read are `//` comment blocks and `"`-delimited literal contents; a
// `/* */` comment or a backtick raw string is not read, and no shape occurs in
// either on this tree.
func TestNoAPISourceStatesTheUnlockCookieIsScopedToAPage(t *testing.T) {
	files := apiSourceFiles(t)
	if len(files) < 300 {
		t.Fatalf("only %d api/*.go files found — the file list is not seeing the tree, so "+
			"this guard would pass over prose it never read", len(files))
	}
	var problems []string
	for _, f := range files {
		problems = append(problems, unlockScopeProseProblems(f, readFileString(t, f))...)
	}
	if len(problems) > 0 {
		t.Errorf("%d place(s) in api/ STATE that the unlock cookie is scoped by a page or a "+
			"domain, which is not what a browser implements and not what this repo's cookie "+
			"does. CHIPS partitions a Partitioned cookie by the top-level SITE (scheme + "+
			"registrable domain) and never by a page; newFormAccessCookie sets Path \"/\" with a "+
			"token bound to the FORM id (signFormAccessToken), so an unlock entered on one page "+
			"of a merchant's site IS sent on that site's other pages. State the site rule "+
			"instead. If you are naming the claim as the wrong one, quote the CLAIM ITSELF "+
			"(as a mutation pair, or as a quoted sentence) — a quotation around a longer "+
			"sentence that merely contains it reads as an assertion here:\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// unlockScopeClaims are the page-/domain-scope shapes this repo must never STATE
// about the unlock cookie, each named so the bite cases below can assemble the
// pre-fix prose without this file carrying it as a statement (this file is judged
// by its own rule like every other).
const (
	claimUnlockOnOneMerchantPage = "one merchant page it was entered on"
	claimUnlockOnOnePage         = "one page it was entered on"
	claimScopedToOneMerchantPage = "scoped to the one merchant page"
	claimScopedToOnePage         = "scoped to the one page"
	claimScopedToThePage         = "scoped to the page"
	claimPerTopLevelPage         = "per top-level page"
	claimPerTopLevelDomain       = "per top-level domain"
	claimTheOneMerchantPage      = "the one merchant page"
)

// unlockScopeClaims is the shape list the scan uses, in the order the failure
// names them.
var unlockScopeClaims = []string{
	claimUnlockOnOneMerchantPage,
	claimTheOneMerchantPage,
	claimScopedToOneMerchantPage,
	claimUnlockOnOnePage,
	claimScopedToOnePage,
	claimScopedToThePage,
	claimPerTopLevelPage,
	claimPerTopLevelDomain,
}

// proseSpanKind says where a span came from, so a failure can tell a comment from
// a message a developer is shown.
type proseSpanKind string

const (
	proseSpanComment proseSpanKind = "comment"
	proseSpanLiteral proseSpanKind = "message/literal"
)

// proseSpan is one judged unit of prose: a joined `//` comment block, or the
// content of one `"`-delimited literal, with the source line the span STARTS on.
type proseSpan struct {
	line int
	kind proseSpanKind
	text string
}

// unlockScopeProseProblems returns one sentence per place in src that STATES a
// page-/domain-scope claim about the unlock cookie. It is pure over (path, src)
// so the bite proof below can hand it the pre-fix message and the corrected one.
func unlockScopeProseProblems(path, src string) []string {
	var out []string
	for _, span := range goProseSpans(src) {
		// A span that IS a claim (a pattern in a must-be-absent list) is a mention
		// whole: there is no sentence to assert anything in.
		pattern := span.isClaimPattern()
		for _, claim := range unlockScopeClaims {
			for _, at := range occurrenceOffsets(span.text, claim) {
				if pattern || span.isMention(claim, at) {
					continue
				}
				out = append(out, fmt.Sprintf("%s:%d [%s] states %q in %q",
					path, span.line, span.kind, claim, span.text))
			}
		}
	}
	return out
}

// isClaimPattern reports whether the span is nothing but a claim — the shape a
// must-be-absent list holds it in ("one page it was entered on",). That is the
// one mention that carries no quotation mark beside it: the marks around a bare
// pattern are the literal's own delimiters, which the span drops.
func (s proseSpan) isClaimPattern() bool {
	if s.kind != proseSpanLiteral {
		return false
	}
	body := strings.TrimRight(strings.TrimSpace(s.text), ",; ")
	for _, claim := range unlockScopeClaims {
		if body == claim {
			return true
		}
	}
	return false
}

// isMention reports whether the claim occurrence at offset at is written inside
// quotation marks — a quoted false sentence, or one half of a mutation pair —
// rather than asserted in a sentence. The mark may be the plain `"` of a comment
// or the `"` of the `\"` an in-message quotation is written with; the closing
// delimiter of the surrounding literal is not part of the span, so the end of a
// message is never read as a quotation mark.
func (s proseSpan) isMention(claim string, at int) bool {
	if at > 0 && s.text[at-1] == '"' {
		return true
	}
	end := at + len(claim)
	if end < len(s.text) && s.text[end] == '"' {
		return true
	}
	return end+1 < len(s.text) && s.text[end] == '\\' && s.text[end+1] == '"'
}

// goProseSpans reads src into the prose units this guard judges: `//` comment
// blocks (consecutive lines joined) and the contents of `"`-delimited literals.
// Code, `/* */` comments and backtick raw strings are not prose units here (see
// the guard comment above).
func goProseSpans(src string) []proseSpan {
	lines := goLogicalLines(src)
	var out []proseSpan
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i].text), "//") {
			out = append(out, goLiteralSpans(lines[i])...)
			continue
		}
		start, parts := lines[i].line, []string{}
		for ; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i].text)
			if !strings.HasPrefix(trimmed, "//") {
				break
			}
			parts = append(parts, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
		}
		i-- // the loop above consumed the first non-comment line
		out = append(out, proseSpan{
			line: start,
			kind: proseSpanComment,
			text: strings.Join(parts, " "),
		})
	}
	return out
}

// goLogicalLines returns each source line with an interpreted string literal's
// continuations joined: a line ending in `" +` continues on the next line, whose
// leading `"` only closes the chunk before it and reopens the same literal. The
// joined line keeps the number of the line the literal STARTED on, so a failure
// points at the start of the sentence rather than at its last chunk.
func goLogicalLines(src string) []goLine {
	lines := strings.Split(src, "\n")
	var out []goLine
	for i := 0; i < len(lines); i++ {
		start := i + 1
		text := strings.TrimRight(lines[i], " \t\r")
		for strings.HasSuffix(text, `" +`) && i+1 < len(lines) {
			text = strings.TrimRight(strings.TrimSuffix(text, `" +`), " \t")
			i++
			text += strings.TrimPrefix(strings.TrimLeft(lines[i], " \t"), `"`)
		}
		out = append(out, goLine{line: start, text: text})
	}
	return out
}

// goLine is one logical source line: its text with string-literal continuations
// joined, and the physical line it starts on.
type goLine struct {
	line int
	text string
}

// goLiteralSpans returns the CONTENTS of the `"`-delimited literals on one logical
// line. The content, not the source, is what a developer or a reader is shown —
// and dropping the literal's own delimiters is what keeps the closing quote of a
// message from being read as the quote that makes a stated claim a mention.
func goLiteralSpans(l goLine) []proseSpan {
	var out []proseSpan
	for i := 0; i < len(l.text); i++ {
		if l.text[i] != '"' || (i > 0 && l.text[i-1] == '\\') {
			continue
		}
		j := i + 1
		for j < len(l.text) {
			if l.text[j] == '"' && l.text[j-1] != '\\' {
				break
			}
			j++
		}
		if j >= len(l.text) {
			break // the literal does not close on this line: nothing to judge
		}
		out = append(out, proseSpan{line: l.line, kind: proseSpanLiteral, text: l.text[i+1 : j]})
		i = j
	}
	return out
}

// occurrenceOffsets returns every offset of needle in s, so a shape repeated in
// one sentence is reported once per occurrence.
func occurrenceOffsets(s, needle string) []int {
	var out []int
	for at := 0; at <= len(s)-len(needle); {
		i := strings.Index(s[at:], needle)
		if i < 0 {
			break
		}
		out = append(out, at+i)
		at += i + 1
	}
	return out
}

// apiSourceFiles lists the `.go` files git can see under api/ — tracked plus
// untracked-but-not-ignored, like the docs-claim walk, so a new authored file is
// judged and a generated artefact cannot enter. It fails rather than skipping
// when git or the tree is missing, so a checkout without git cannot turn this
// guard into a green test that read nothing.
func apiSourceFiles(t *testing.T) []string {
	t.Helper()
	root := apiSourceRoot(t)
	cmd := exec.Command("git", "-C", root, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard", "--", "*.go")
	cmd.Env = cleanGitEnv()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files in %s: %v — the api-prose walk reads git's file list so a "+
			"generated artefact is never judged and a new file cannot be missed", root, err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
	}
	sort.Strings(files)
	return files
}

// apiSourceRoot locates the api tree relative to this test file
// (api/internal/httpapi -> ../..), and fails when it is not there.
func apiSourceRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate the api tree")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if st, err := os.Stat(root); err != nil {
		t.Fatalf("stat api tree %s: %v", root, err)
	} else if !st.IsDir() {
		t.Fatalf("%s is not a directory", root)
	}
	return root
}

// TestUnlockScopeProseGuardBites proves the rule above against the prose it was
// written for, rather than only against the tree that happens to be clean. Every
// input here is ASSEMBLED from the shapes (or from a corrected sentence) instead
// of copied, so this file never itself states the claim — it is read by its own
// rule, in TestNoAPISourceStatesTheUnlockCookieIsScopedToAPage, like every other
// file under api/.
func TestUnlockScopeProseGuardBites(t *testing.T) {
	// The message embed_docs_snippet_test.go shipped before card t_a29148e5
	// corrected it, assembled from the shape constant so this file does not carry
	// the claim itself.
	preFix := "t.Errorf(\"the unlock cookie is not Partitioned (Set-Cookie: %q). A bare " +
		"SameSite=None cookie is exactly the third-party cookie browsers are " +
		"restricting; CHIPS (Partitioned) is what an embedded widget must use for " +
		"its own state, and it also scopes the unlock to the " + claimUnlockOnOneMerchantPage +
		"\", setCookie)"
	// The corrected message: the site rule, with the mechanism that makes it true.
	corrected := "t.Errorf(\"the unlock cookie is not Partitioned (Set-Cookie: %q). A bare " +
		"SameSite=None cookie is exactly the third-party cookie browsers are " +
		"restricting; CHIPS (Partitioned) is what an embedded widget must use for " +
		"its own state, and it also scopes the unlock to the top-level SITE it was " +
		"entered on — the cookie's Path is \\\"/\\\" and the token is bound to the form id, " +
		"so a second page of that same site is unlocked too\", setCookie)"
	// The narrower swap of the corrected sentence: the mutation the docs guard
	// reds on when it lands in the copy.
	narrowed := strings.Replace(corrected, "top-level SITE", claimPerTopLevelPage, 1)

	cases := []struct {
		name     string
		src      string
		wantFail bool
	}{
		{"the pre-fix failure message this card was filed for", preFix, true},
		{"the corrected message (site rule + the form-id mechanism)", corrected, false},
		{"the corrected sentence narrowed back to a page scope", narrowed, true},
		{"a comment stating the page scope (the reader-facing variant)", "// it also scopes the unlock to the " + claimTheOneMerchantPage + " it was entered on", true},
		{"a quoted mention of the claim (naming it as wrong, not using it)", "t.Error(\"the sentence said it was \\\"" + claimUnlockOnOnePage + "\\\" and that was wrong\")", false},
		{"a bare claim pattern in a must-be-absent list", "for _, claim := range []string{\n\t\"" + claimScopedToThePage + "\",\n} {\n\t_ = claim\n}", false},
	}
	for _, tc := range cases {
		problems := unlockScopeProseProblems("bite_probe_test.go", tc.src)
		if tc.wantFail && len(problems) == 0 {
			t.Errorf("case %q: the guard is GREEN on prose that states the claim — it would not "+
				"have caught the defect it exists for. src=%q", tc.name, tc.src)
		}
		if !tc.wantFail && len(problems) != 0 {
			t.Errorf("case %q: the guard is RED on prose that only MENTIONS the claim "+
				"(a quoted form, or a claim pattern), so a correct file cannot be kept green: "+
				"%v", tc.name, problems)
		}
	}
}
