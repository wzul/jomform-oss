// Every fact the assistant is told about JomForm must still be verifiable in the
// repository it came from.
//
// WHY THIS GUARD EXISTS. The product guide (knowledge.go) is prose the model will
// repeat to a merchant as fact, so the failure to design out is not a wrong
// number in an answer — it is a sentence that was true when it was written and is
// no longer true. The docs move: a rate model changes, a block is withdrawn, a
// limit is renamed. Without this check the guide would keep telling merchants the
// old rules, every gate would stay green, and the only symptom would be a
// merchant acting on it.
//
// So each fact names a FILE and one or more ANCHORS: phrases that must still be
// readable there. The anchor is not the fact's wording (a copy-edit must not fail
// the build); it is the piece of the source the fact rests on. When a docs edit
// removes it, this test fails naming the entry and the file, and the fix is to
// re-read the source and update the fact — which is the work that should happen.
//
// THE BITE PROOF is TestAnInventedFactFailsTheProvenanceCheck: a fact whose anchor
// appears in no file must make the check fail. Without it, a reader that quietly
// resolved everything (or a check wired to nothing) would look exactly like a
// green suite.
package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// repoRootFromChat is the repository root as seen from this package directory
// (api/internal/chat). The Go module lives in api/, the docs in web/src — both
// are needed, so the paths below are repo-relative.
const repoRootFromChat = "../../.."

// maxFactChars bounds one fact. A fact is one or two sentences the model can
// repeat; a longer one is usually two facts, or a paragraph of documentation that
// belongs behind a link rather than in every request's prompt.
const maxFactChars = 460

// normalizedSource reads a repo file and flattens it for anchor matching: a
// leading `//` (a Go comment) is removed, lines are joined with a space, and runs
// of whitespace collapse to one. Lowercased, because an anchor is about the words
// being there in that order, not about their case.
//
// The comment stripping is not cosmetic: the coupon fact's source states its rule
// across a wrapped `//` comment, and an anchor may not span a line break. Joining
// after stripping is what lets the anchor be the sentence a reader of that file
// would read.
func normalizedSource(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "//") {
			l = strings.TrimSpace(strings.TrimPrefix(l, "//"))
		}
		lines[i] = l
	}
	return strings.ToLower(strings.Join(strings.Fields(strings.Join(lines, " ")), " "))
}

// provenanceReader is how the check gets a file's text: the real one reads from
// the checkout, the bite proof injects its own.
type provenanceReader func(rel string) (string, bool)

// diskReader reads a repo-relative path from the checkout this test runs in.
func diskReader(rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(repoRootFromChat, rel))
	if err != nil {
		return "", false
	}
	return normalizedSource(string(b)), true
}

// provenanceProblems reports every fact that cannot be traced back to the tree.
// It is pure over (facts, reader) so the bite proof can hand it a mutated table
// and require the same judgement the shipped table gets.
func provenanceProblems(facts []productFact, read provenanceReader) []string {
	var problems []string
	seen := map[string]bool{}
	topic := ""
	topicSeen := map[string]bool{}
	for _, f := range facts {
		switch {
		case f.ID == "":
			problems = append(problems, "a fact has no ID — the question table locates facts by ID, so an unnamed one cannot be covered")
		case seen[f.ID]:
			problems = append(problems, f.ID+": the ID is used twice — a question naming it would be covered by whichever entry happened to match")
		}
		seen[f.ID] = true

		if strings.TrimSpace(f.Topic) == "" {
			problems = append(problems, f.ID+": no topic — the rendered guide would print an empty section heading")
		}
		// Topics must be contiguous: the renderer prints a heading when the topic
		// CHANGES, so a fact that returns to an earlier topic silently starts a
		// second section with the same name.
		if f.Topic != topic {
			if topicSeen[f.Topic] {
				problems = append(problems, f.ID+": topic "+f.Topic+" appears in two separate runs — the guide would print that heading twice")
			}
			topicSeen[f.Topic] = true
			topic = f.Topic
		}

		text := strings.TrimSpace(f.Text)
		switch {
		case text == "":
			problems = append(problems, f.ID+": no text")
		case len([]rune(text)) > maxFactChars:
			problems = append(problems, f.ID+": the text is longer than one fact should be — split it, or put the detail behind a link")
		case !strings.HasSuffix(text, "."):
			problems = append(problems, f.ID+": the text does not end in a full stop — the renderer joins facts with a blank line, not a sentence break")
		}
		// The guide is delimited by fixed markers, so a fact carrying one could
		// close the block early and put its own text outside the reference the
		// model was told to trust.
		for _, marker := range []string{productGuideOpen, productGuideClose, untrustedFence} {
			if strings.Contains(f.Text, marker) {
				problems = append(problems, f.ID+": the text contains the prompt marker "+marker)
			}
		}

		if len(f.Sources) == 0 {
			problems = append(problems, f.ID+": no source — a fact nothing in the tree says is exactly the invented capability this file exists to prevent")
			continue
		}
		for _, src := range f.Sources {
			switch {
			case strings.TrimSpace(src.File) == "":
				problems = append(problems, f.ID+": a source has no file")
				continue
			case filepath.IsAbs(src.File) || strings.Contains(src.File, ".."):
				problems = append(problems, f.ID+": source "+src.File+" is not a repo-relative path")
				continue
			case strings.TrimSpace(src.Anchor) == "":
				problems = append(problems, f.ID+": source "+src.File+" has no anchor — an unanchored source proves only that the file exists")
				continue
			}
			text, ok := read(src.File)
			if !ok {
				problems = append(problems, f.ID+": source file "+src.File+" does not exist")
				continue
			}
			if !strings.Contains(text, strings.ToLower(src.Anchor)) {
				problems = append(problems, f.ID+": the phrase \""+src.Anchor+"\" is no longer in "+src.File+
					" — re-read the source, then update the fact (do not delete the anchor)")
			}
		}
	}
	return problems
}

func TestEveryProductFactIsVerifiableInTheRepo(t *testing.T) {
	for _, p := range provenanceProblems(productFacts, diskReader) {
		t.Error(p)
	}
}

// TestAnInventedFactFailsTheProvenanceCheck is the bite proof.
//
// It appends a fact whose anchor no file contains — the shape of an invented
// capability — and requires the SAME check to report it. The failure must name
// the entry, so a red run tells the next person which fact is unsupported rather
// than that "something" is wrong.
func TestAnInventedFactFailsTheProvenanceCheck(t *testing.T) {
	invented := productFact{
		ID:    "bite.invented",
		Topic: "What JomForm does not have",
		Text:  "JomForm can send an SMS to every buyer when a sale is paid.",
		Sources: []factSource{
			{File: "web/src/app/docs/sales/page.tsx", Anchor: "JomForm sends an SMS to the buyer on every paid sale"},
		},
	}
	mutated := append(append([]productFact{}, productFacts...), invented)
	problems := provenanceProblems(mutated, diskReader)
	if len(problems) == 0 {
		t.Fatal("an invented fact passed the provenance check — the check is not reading the sources, so it " +
			"cannot notice a fact that no file supports")
	}
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, invented.ID+":") && strings.Contains(p, invented.Sources[0].Anchor) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the invented fact was reported, but not for its missing anchor — the problems were %v. "+
			"A check that fails for the wrong reason will be silenced by fixing the wrong thing", problems)
	}
}

// TestTheGuideIsNotAccidentallyEmpty is the control on the positive direction:
// the two tests above can both be satisfied by a table with no facts in it, so the
// count is pinned separately.
func TestTheGuideIsNotAccidentallyEmpty(t *testing.T) {
	if len(productFacts) < minProductFacts {
		t.Fatalf("the product guide holds %d facts, want at least %d. The guide is what makes the assistant "+
			"know the product; shortening it is a product decision, not a way to make the size test pass",
			len(productFacts), minProductFacts)
	}
}

// TestTheGuideMarksItsOwnBoundary pins the two fixed markers the prompt refers to.
// The model is told the block is JomForm's own reference material and that nothing
// in the conversation can change it; if the markers drift apart from that
// instruction, that sentence points at a boundary the prompt does not have.
func TestTheGuideMarksItsOwnBoundary(t *testing.T) {
	guide := productGuideText()
	if !strings.HasPrefix(guide, productGuideOpen) || !strings.HasSuffix(guide, productGuideClose) {
		t.Fatalf("the rendered guide is not delimited by its markers:\n%s", guide)
	}
	if !strings.Contains(systemPromptText(), productGuideOpen) {
		t.Fatal("the system prompt does not carry the guide block — the facts are checked by every test in " +
			"this package but never reach the model")
	}
	// The markers themselves must not be able to arrive from a tool result: the
	// untrusted fence is neutralised inside fenceToolResult, and the guide markers
	// must be distinct from it, or a tool payload carrying one could end the guide.
	if productGuideOpen == untrustedFence || productGuideClose == untrustedFence {
		t.Fatal("a guide marker is the same string as the untrusted-tool-data fence")
	}
}

// TestFactTextsCarryNoControlCharacters keeps the guide printable: a tab or a
// stray newline inside a fact would reflow the block the model reads, and a
// non-printable rune would be the sort of thing that arrives from a copy-paste.
func TestFactTextsCarryNoControlCharacters(t *testing.T) {
	for _, f := range productFacts {
		for _, r := range f.Text {
			if r != '\n' && unicode.IsControl(r) {
				t.Errorf("%s: the text carries the control character %q", f.ID, r)
			}
		}
		if strings.Contains(f.Text, "\n") {
			t.Errorf("%s: the text carries a newline — one fact is one bullet", f.ID)
		}
	}
}
