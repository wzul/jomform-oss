// DERIVED LINE PINS — the production line a failure message quotes is resolved
// from the source at TEST time, never typed into the message.
//
// # WHY THIS FILE EXISTS
//
// A guard that reports "the OUTER guard at auth_recovery.go:233" is telling its
// reader where to look, and the line number is the one part of that message that
// rots on its own: the guard stays, its behaviour stays asserted, and the pointer
// silently starts naming a different line. Measured, not assumed —
// auth_recovery.go was rewritten twice in the ten commits before this file, and
// at one #672 merge every pin in typed_nil_mailer_guard_test.go was 6-7 lines
// low while the whole package stayed green. The only diagnostic that file
// produces was sending people to the wrong code, and nothing in the repo noticed.
//
// So a pin is an ANCHOR: the site is named by the content a reader would grep for
// (derivedLineAnchor.Name / .Any) and its line is resolved from the source when
// the message is built. A moved file then cannot make a message point somewhere
// else, and a site the resolver cannot find EXACTLY ONCE fails BY NAME — the
// anchor's content — instead of printing a number that may mean nothing. That is
// the shape PR 826 / da538047 already set for the selftest BITE anchor, which
// used to name a web/ path that had moved.
//
// What makes the pins in typed_nil_mailer_guard_test.go resolvable at all is the
// SCOPE: a clause like `mailerUnavailable(a.VerifyMailer)` occurs four times in
// auth_recovery.go, so it names a site only inside the function that owns it
// (derivedLineAnchor.Within). The reported number is always the FILE's line, not
// an offset into the function, because that is the number a reader can act on.
//
// The pins themselves live with the messages that quote them; this file is the
// resolver and the guard on the resolver:
//
//	TestDerivedLineAnchorSiteLocation     one line, no line, more than one line,
//	                                      and a clause that is only nameable
//	                                      inside its function
//	TestDerivedLineAnchorTracksTheSource  the shift this exists for, run against
//	                                      the REAL auth_recovery.go: an insertion
//	                                      above a site moves its reported line by
//	                                      exactly that many lines, and every pin
//	                                      the package ships resolves to a line
//	                                      that really carries the text it names.
package httpapi

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// derivedLineAnchor is one production line pinned by the content a reader would
// grep for, so the number a failure message quotes can be derived rather than
// typed.
type derivedLineAnchor struct {
	// File is the production file, named relative to THIS test file's own
	// directory (a sibling is "auth_recovery.go", a file up the tree is
	// "../../../cmd/..."), so a pin does not depend on the working directory a
	// run happens to have.
	File string
	// Display is the file as a failure message should write it, so a reader can
	// open it without guessing: "auth_recovery.go" beside this package,
	// "cmd/jomform-api/main.go" for a file up the tree.
	Display string
	// Name is the site in the words a reader greps for — the role plus the
	// expression or literal that identifies it. It is also what a failure prints
	// when the site cannot be found, because that content is the only thing that
	// still identifies the guard once its line number is unknown.
	Name string
	// Any is the set of literals that identify the site: the site is the line
	// containing at least one of them. More than one spelling is allowed only
	// for the SAME site under a mutation this package's own tests perform — see
	// anchorOuterGuard, which must still name the guard when the guard has been
	// reverted to its pre-#672 spelling and THAT is the failure being reported.
	Any []string
	// Within narrows the site to one top-level function, given by any substring
	// of that function's declaration line that occurs exactly once in the file
	// (e.g. "func (a *AuthHandlers) sendMagicLinkEmail("). It is what makes a
	// clause repeated across several guards nameable: `mailerUnavailable(
	// a.VerifyMailer)` occurs four times in auth_recovery.go, and a pin that
	// could not say WHICH one it means is not a pin. Empty means the whole file.
	Within string
}

// testFileDir is the directory this test file lives in. Every anchor's source is
// resolved against it.
var testFileDir = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("derived line anchor: runtime.Caller cannot locate this test file")
	}
	return filepath.Dir(file)
}()

// anchorSourceCache holds the split source of each file an anchor points into.
// It is read once: a pin is resolved while a FAILING test builds its message,
// and a read that failed at that moment would replace the finding the test just
// made with a message about a file read.
var anchorSourceCache = map[string][]string{}

func anchorSource(t *testing.T, file string) []string {
	t.Helper()
	if lines, ok := anchorSourceCache[file]; ok {
		return lines
	}
	path := filepath.Join(testFileDir, filepath.FromSlash(file))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the production source the line pins in this package are derived from: %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")
	anchorSourceCache[file] = lines
	return lines
}

// matches reports whether one source line is the anchor's site.
func (a derivedLineAnchor) matches(line string) bool {
	for _, want := range a.Any {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

// scope returns the half-open line range [from, to) the site may live in: the
// whole file when Within is empty, otherwise the body of the one function that
// declaration names. It refuses to guess for the same reason siteIn does — a
// scope that matches nothing, or several declarations, cannot name a site.
func (a derivedLineAnchor) scope(lines []string) (int, int, error) {
	if a.Within == "" {
		return 0, len(lines), nil
	}
	var decls []int
	for i, line := range lines {
		if strings.Contains(line, a.Within) {
			decls = append(decls, i)
		}
	}
	switch len(decls) {
	case 0:
		return 0, 0, fmt.Errorf("no line of %s declares %s (an anchor scopes its site to that function, and without "+
			"the function the site cannot be located)", a.Display, a.Within)
	case 1:
	default:
		return 0, 0, fmt.Errorf("%d lines of %s match the function %s (lines %v), so the scope of the site is ambiguous",
			len(decls), a.Display, a.Within, lineNumbers(decls))
	}
	from := decls[0]
	to := len(lines)
	for i := from + 1; i < len(lines); i++ {
		// A top-level declaration starts in column 0; anything indented is a
		// nested literal or closure, not the end of the function.
		if strings.HasPrefix(lines[i], "func ") {
			to = i
			break
		}
	}
	return from, to, nil
}

// siteIn returns the 1-based line of the anchor's site within the half-open
// range [from, to) of lines, and refuses to guess: a site that is not there, or
// that is there more than once, cannot be named by a single line number at all,
// and either one is an error rather than a best-effort number.
func (a derivedLineAnchor) siteIn(lines []string, from, to int) (int, error) {
	var hits []int
	for i := from; i < to && i < len(lines); i++ {
		if a.matches(lines[i]) {
			hits = append(hits, i+1)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return 0, fmt.Errorf("no line of %s is %s", a.Display, a.Name)
	default:
		return 0, fmt.Errorf("%d lines of %s are %s (lines %v), so no single line names it",
			len(hits), a.Display, a.Name, hits)
	}
}

// lineIn resolves the site against one set of source lines — scope first, then
// exactly one match. The result is the FILE's line number, never an offset into
// the function, because the file line is what a reader can act on.
func (a derivedLineAnchor) lineIn(lines []string) (int, error) {
	from, to, err := a.scope(lines)
	if err != nil {
		return 0, err
	}
	return a.siteIn(lines, from, to)
}

// Line returns the "file:line" a failure message should quote, or fails the test
// NAMING THE ANCHOR when its site cannot be found exactly once.
//
// It never falls back to a number: a stale line number is the defect this
// mechanism replaces, and a message that prints one sends the reader to code
// that has nothing to do with the guard being reported.
func (a derivedLineAnchor) Line(t *testing.T) string {
	t.Helper()
	line, err := a.lineIn(anchorSource(t, a.File))
	if err != nil {
		t.Fatalf("cannot derive the line pin for %s: %v. The line numbers in this package's failure messages are "+
			"DERIVED from the production source (PR 826 / da538047 is the precedent), so a pin the resolver cannot find "+
			"is reported BY NAME here instead of as a line number that may point somewhere else. Repoint the anchor at "+
			"the site's new spelling (derivedLineAnchor.Any), or at the function that now owns it "+
			"(derivedLineAnchor.Within) — if the site is really gone, the assertion that quotes it has lost the thing it names.",
			a.Name, err)
	}
	return a.Display + ":" + strconv.Itoa(line)
}

func lineNumbers(zeroBased []int) []int {
	out := make([]int, 0, len(zeroBased))
	for _, i := range zeroBased {
		out = append(out, i+1)
	}
	return out
}

// TestDerivedLineAnchorSiteLocation pins the resolver's answers: exactly one site
// (the number, 1-based), no site (an error, and it NAMES the anchor), more than
// one site (an error too — a pin that cannot say WHICH line a reader should
// follow is not a pin), and the scope that makes a repeated clause nameable.
func TestDerivedLineAnchorSiteLocation(t *testing.T) {
	src := []string{
		"package x",
		"",
		"func f() {",
		"\tif a.Sessions == nil || mailerUnavailable(a.VerifyMailer) {",
		"\t\treturn",
		"\t}",
		"\treturn",
		"}",
		"",
		"func g() {",
		"\tif mailerUnavailable(a.VerifyMailer) {",
		"\t\treturn",
		"\t}",
		"\treturn",
		"}",
	}

	one := derivedLineAnchor{
		Display: "x.go",
		Name:    "the guard (`a.Sessions == nil || mailerUnavailable(a.VerifyMailer)`)",
		Any:     []string{"a.Sessions == nil || mailerUnavailable(a.VerifyMailer)"},
	}
	got, err := one.siteIn(src, 0, len(src))
	if err != nil {
		t.Fatalf("siteIn with exactly one site: %v", err)
	}
	if got != 4 {
		t.Fatalf("siteIn = %d, want 4 — the result is a 1-based LINE, and an off-by-one here would put every message "+
			"in this package one line off, which is the failure mode the pins are derived to avoid", got)
	}

	// No site: the error must name the anchor, and must NOT look like a line pin.
	none := derivedLineAnchor{
		Display: "x.go",
		Name:    "the guard (`a.VerifyMailer == nil`)",
		Any:     []string{"a.VerifyMailer == nil"},
	}
	_, err = none.siteIn(src, 0, len(src))
	if err == nil {
		t.Fatal("siteIn resolved a site that is not in the source — an anchor that always resolves pins nothing")
	}
	if !strings.Contains(err.Error(), "the guard") {
		t.Fatalf("siteIn error = %q, want it to NAME the anchor (\"the guard\"), because the content is what identifies "+
			"the site once the line number is unknown", err)
	}
	if strings.Contains(err.Error(), "x.go:") {
		t.Fatalf("siteIn error = %q, want it to carry NO file:line — a resolver that invents a location is exactly the "+
			"stale-number failure this mechanism replaces", err)
	}

	// More than one site: also an error. `return` is on four lines.
	many := derivedLineAnchor{Display: "x.go", Name: "the `return` statement", Any: []string{"return"}}
	_, err = many.siteIn(src, 0, len(src))
	if err == nil {
		t.Fatal("siteIn resolved an ambiguous anchor — with several matching lines no number can say which one a " +
			"reader should follow")
	}
	if !strings.Contains(err.Error(), "4 lines") {
		t.Fatalf("siteIn error = %q, want it to report the 4 matching lines", err)
	}

	// THE SCOPE. The same clause is a site in f (line 4) and in g (line 11), so
	// the file alone cannot name it — and each function's scope can. This is the
	// shape that makes the inner magic-link guard resolvable, so it is asserted
	// rather than relied on.
	scoped := derivedLineAnchor{
		Display: "x.go",
		Name:    "the guard inside g (`mailerUnavailable(a.VerifyMailer)`)",
		Any:     []string{"mailerUnavailable(a.VerifyMailer)"},
		Within:  "func g()",
	}
	from, to, err := scoped.scope(src)
	if err != nil {
		t.Fatalf("scope(func g): %v", err)
	}
	if from != 9 || to != len(src) {
		t.Fatalf("scope(func g) = [%d,%d), want [9,%d) — the scope is the function's body, up to the next top-level "+
			"declaration (or the end of the file)", from, to, len(src))
	}
	got, err = scoped.lineIn(src)
	if err != nil {
		t.Fatalf("lineIn with a scope: %v", err)
	}
	if got != 11 {
		t.Fatalf("lineIn with a scope = %d, want 11 — the FILE's line, not an offset into the function (an offset "+
			"would be 2, and a reader cannot act on that)", got)
	}

	// A scope that matches nothing, or matches twice, is an error naming it —
	// the same refusal as an unresolvable site.
	for _, bad := range []derivedLineAnchor{
		{Display: "x.go", Name: "the guard", Any: []string{"mailerUnavailable"}, Within: "func h()"},
		{Display: "x.go", Name: "the guard", Any: []string{"mailerUnavailable"}, Within: "func "},
	} {
		if _, _, err := bad.scope(src); err == nil {
			t.Fatalf("scope(%q) succeeded; a scope that matches nothing or several declarations cannot name a site, "+
				"and both are errors rather than a best-effort range", bad.Within)
		}
	}
}

// TestDerivedLineAnchorTracksTheSource is the shift this mechanism exists for,
// run against the REAL file the package's pins point into: inserting blank lines
// at the top of a function must move every pin inside it by exactly that many
// lines. A typed pin cannot do that, and that is precisely the defect the pins
// this file replaces had.
//
// It also asserts the other half — every pin the package ships resolves to a line
// that really carries the text the anchor is named by — so a pin cannot pass by
// resolving to somewhere that merely parses.
func TestDerivedLineAnchorTracksTheSource(t *testing.T) {
	if len(pins) == 0 {
		t.Fatal("the package ships no line pins — this guard would pass vacuously, and the messages it exists to keep " +
			"honest are the ones that carry the numbers")
	}

	for _, a := range pins {
		src := anchorSource(t, a.File)
		got := a.Line(t)

		want := a.Display + ":"
		if !strings.HasPrefix(got, want) {
			t.Fatalf("Line() = %q, want the %q form a message quotes", got, want)
		}
		n, err := strconv.Atoi(strings.TrimPrefix(got, want))
		if err != nil {
			t.Fatalf("Line() = %q: the line after %q is not a number: %v", got, want, err)
		}
		if n < 1 || n > len(src) {
			t.Fatalf("Line() = %q is outside %s (%d lines)", got, a.File, len(src))
		}
		if !a.matches(src[n-1]) {
			t.Fatalf("Line() = %q names a line that does NOT carry %s: %q — every failure message in this package "+
				"would point at code that is not the thing being reported", got, a.Name, src[n-1])
		}
	}

	// The shift, applied INSIDE each pin's own scope, so a pin scoped to one
	// function is shifted by an insertion in that function — which is the edit
	// this repo actually makes to move these lines.
	const inserted = 7
	shifted := map[string][]string{}
	for _, a := range pins {
		base := anchorSource(t, a.File)
		if _, ok := shifted[a.File]; ok {
			continue
		}
		marker := a.Within
		if marker == "" {
			marker = "func "
		}
		shifted[a.File] = insertBlankLinesAfter(t, base, marker, inserted)
	}

	for _, a := range pins {
		base := anchorSource(t, a.File)
		before, err := a.lineIn(base)
		if err != nil {
			t.Fatalf("lineIn before the shift: %v", err)
		}
		after, err := a.lineIn(shifted[a.File])
		if err != nil {
			t.Fatalf("lineIn after a %d-line insertion inside its own function: %v — the pin must follow the file, "+
				"which is the whole reason it is derived", inserted, err)
		}
		if after != before+inserted {
			t.Fatalf("%s reported line %d in the shifted source, want %d (was %d before a %d-line insertion above it) — "+
				"the number must track the file, not the revision it was typed into",
				a.Name, after, before+inserted, before, inserted)
		}
	}
}

// insertBlankLinesAfter returns a copy of lines with n empty lines inserted
// directly below the one line containing marker. It is how the shift above is
// built, so that the test shifts the REAL source rather than a fixture that can
// only agree with itself.
func insertBlankLinesAfter(t *testing.T, lines []string, marker string, n int) []string {
	t.Helper()
	at := -1
	for i, line := range lines {
		if strings.Contains(line, marker) {
			if at >= 0 {
				t.Fatalf("%q is on lines %d and %d — the shift cannot pick one", marker, at+1, i+1)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no line of the source contains %q, so the shift below it has nothing to insert after — the function "+
			"this test shifts has been renamed or removed", marker)
	}
	out := make([]string, 0, len(lines)+n)
	out = append(out, lines[:at+1]...)
	for i := 0; i < n; i++ {
		out = append(out, "")
	}
	out = append(out, lines[at+1:]...)
	return out
}
