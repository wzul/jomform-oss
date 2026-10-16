// The chat's COST BOUND is arithmetic on two constants, and rest_chat.go states
// it in prose — twice.
//
// # WHAT THIS FILE EXISTS FOR
//
// chatRatePerHour bounds one user's chat MESSAGES per hour; the cost is that
// number times the MODEL CALLS one message can make. The comment beside the
// constant used to state the product as chatRatePerHour x chat.MaxSteps — 240 —
// but MaxSteps caps tool ROUND-TRIPS, and the runner makes one model call per
// round-trip plus one final tool-less call that answers from what the tools
// returned (the loop at chat.go:378, and chat.go:494 on its last step; measured by
// the chat package's TestModelCallsPerMessageIsMaxStepsPlusOne). So the ceiling is
// chatRatePerHour x (chat.MaxSteps + 1) — 300 — and the shipped comment understated
// it by 25% while every test in this repo stayed green, because nothing read the
// comment: it is the same drift the /docs/chat prose guard (web/src/lib/
// chatPage.test.ts, card t_306d1389) closes on the page, one layer in.
//
// # THE RULE
//
// costCommentProblems reads every `//` comment block in rest_chat.go and checks
// three things, so the prose cannot drift from the constants again:
//
//  1. the file states the ceiling ONCE, in MODEL CALLS ("at most <n> model calls
//     per user per hour"). 0 matches is a failure (the sentence is gone, or has
//     been reworded into something this rule cannot read) and 2+ is a failure too,
//     because a number stated twice is two numbers to keep in step — which is how
//     the 240 got there;
//  2. every comment that derives the per-message cost from the step cap derives it
//     from MaxSteps + 1. This is the UNIT the defect got wrong: the shipped header
//     said "up to ChatMaxSteps of them" and the const block said "times
//     chat.MaxSteps tool round-trips per message", each counting round-trips where
//     the reader is being told model calls — and ChatMaxSteps is a name that exists
//     nowhere in the repository;
//  3. the stated number EQUALS int(chatRatePerHour) * (chat.MaxSteps + 1), the
//     constants this package compiles against.
//
// It is pure over (source, rate, steps) so the bite proof below can move EITHER
// constant and ask the SAME rule for its problems — the idiom of chat_limits_test.go's
// enforcedChatCaps, where the value is an expression over the constant that governs
// it rather than a second copy of the number.
package httpapi

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/chat"
)

// restChatSourceFile is the file whose prose states the ceiling, read from the
// package directory (where these tests run).
const restChatSourceFile = "rest_chat.go"

// ceilingRe matches the ONE form the ceiling may be stated in: model calls per
// user per hour. A sentence that stops saying "model calls" — the unit half of the
// defect — stops matching, and rule 1 then reports the file as silent rather than
// letting a reworded sentence pass unread.
var ceilingRe = regexp.MustCompile(`at most (\d+) model calls per user per hour`)

// perMessageCostRe matches the two forms a comment derives the per-message cost
// in: the header's "up to … of them" and the const block's "times …".
var perMessageCostRe = regexp.MustCompile(`up to|times`)

// callNounRe matches the noun a per-message cost sentence is about, so the rule
// does not fire on an incidental mention of MaxSteps.
var callNounRe = regexp.MustCompile(`LLM call|model calls|of them`)

// maxStepsRe finds a comment that names the step cap at all — including the stale
// `ChatMaxSteps`, which is exactly the shape rule 2 has to judge (a bare `\b`
// around MaxSteps would step over it).
var maxStepsRe = regexp.MustCompile(`MaxSteps`)

// derivedFromPlusOne is the derivation rule 2 demands, compared with spaces
// removed so `MaxSteps+1` and `MaxSteps + 1` are the same claim.
const derivedFromPlusOne = "MaxSteps+1"

// commentBlocks returns each run of consecutive `//` comment lines in src, with
// the markers stripped and internal whitespace collapsed to single spaces, in file
// order.
//
// A BLOCK rather than a line, because both sites here wrap their sentence across
// lines: a line-based read sees "…and up" and "to chat.MaxSteps + 1 of them" and
// matches neither half, so the rule would pass vacuously.
func commentBlocks(src string) []string {
	var blocks []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		blocks = append(blocks, strings.Join(strings.Fields(strings.Join(cur, " ")), " "))
		cur = nil
	}
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") {
			cur = append(cur, strings.TrimSpace(strings.TrimPrefix(t, "//")))
			continue
		}
		flush()
	}
	flush()
	return blocks
}

// costCommentProblems returns everything wrong with rest_chat.go's model-call
// cost prose, judged against perHour messages/hour and maxSteps tool round-trips
// per message, as sentences.
func costCommentProblems(src string, perHour float64, maxSteps int) []string {
	var out []string
	blocks := commentBlocks(src)
	flat := strings.Join(blocks, "\n")

	// The ceiling the constants imply. It is DERIVED here, not written: the whole
	// defect was a ceiling written by hand.
	want := int(perHour) * (maxSteps + 1)

	// ── 1. the ceiling is stated once, in model calls ─────────────────────────
	stated := ceilingRe.FindAllStringSubmatch(flat, -1)
	switch {
	case len(stated) == 0:
		out = append(out, fmt.Sprintf("%s no longer states the chat's per-message cost in model "+
			"calls (\"at most <n> model calls per user per hour\") — %d messages/hour x %d model "+
			"calls per message is the number a reader of that comment needs, so retarget this "+
			"guard at whatever replaced the sentence rather than deleting it",
			restChatSourceFile, int(perHour), maxSteps+1))
	case len(stated) > 1:
		out = append(out, fmt.Sprintf("%s states the model-call ceiling %d times — a number stated "+
			"twice is two numbers to keep in step, and the day one of them is edited the other is "+
			"already wrong; state it once", restChatSourceFile, len(stated)))
	default:
		n, err := strconv.Atoi(stated[0][1])
		if err != nil {
			// Unreachable with `\d+`; kept so a future regex edit cannot become a
			// silent `0` comparison.
			out = append(out, fmt.Sprintf("%s states a model-call ceiling this rule cannot read as a "+
				"number (%q) — fix the pattern, do not let it compare nothing", restChatSourceFile, stated[0][1]))
		} else if n != want {
			out = append(out, fmt.Sprintf("%s states at most %d model calls per user per hour, but "+
				"the constants say %d: chatRatePerHour %.0f messages/hour x (chat.MaxSteps %d + 1) "+
				"model calls per message. MaxSteps caps tool ROUND-TRIPS, and the runner also makes "+
				"the final tool-less call that answers from what they gathered (chat.go:378 and "+
				":494), so a ceiling built from MaxSteps alone understates the bill",
				restChatSourceFile, n, want, perHour, maxSteps))
		}
	}

	// ── 2. every derivation of the per-message cost names MaxSteps + 1 ────────
	judged := 0
	for _, b := range blocks {
		if !maxStepsRe.MatchString(b) || !perMessageCostRe.MatchString(b) || !callNounRe.MatchString(b) {
			continue
		}
		judged++
		if !strings.Contains(strings.ReplaceAll(b, " ", ""), derivedFromPlusOne) {
			out = append(out, fmt.Sprintf("%s derives the per-message model-call cost from MaxSteps "+
				"alone: %q — MaxSteps caps tool ROUND-TRIPS while one message can cost MaxSteps + 1 "+
				"MODEL calls (the loop at chat.go:378 plus the final answer call at chat.go:494), "+
				"so the derivation has to name MaxSteps + 1", restChatSourceFile, excerpt(b, "MaxSteps")))
		}
	}
	if judged == 0 {
		out = append(out, fmt.Sprintf("%s has no comment deriving the per-message cost from the step "+
			"cap any more — this rule is reading nothing, which is how a ceiling built from MaxSteps "+
			"alone survived here; retarget it rather than letting it pass vacuously", restChatSourceFile))
	}

	return out
}

// excerpt returns the part of s around the first occurrence of anchor, at most
// about 160 characters, with ellipses where it was cut.
//
// A failure message has to name the SENTENCE a human should edit. A comment block
// in this file can be a 22-line file header, and pasting that whole thing into a
// test failure buries the sentence the reader is looking for.
func excerpt(s, anchor string) string {
	i := strings.Index(s, anchor)
	if i < 0 {
		return s
	}
	const window = 80
	start, end := i-window, i+window
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	out := s[start:end]
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}

// readRestChatSource reads the file these tests judge. A read failure is a FAILURE,
// never a skip: a guard that cannot see its subject proves nothing.
func readRestChatSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(restChatSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v — these tests run in the package directory, so a miss means the "+
			"guard's subject moved, not that the claim is fine", restChatSourceFile, err)
	}
	return string(b)
}

// TestChatCostCommentMatchesTheRunnerCeiling is the guard: the file's prose and the
// two constants that set the ceiling have to agree, in the unit a reader is told.
func TestChatCostCommentMatchesTheRunnerCeiling(t *testing.T) {
	src := readRestChatSource(t)

	// Vacuity, in the neighbours' idiom: prove this is the file the rule means to
	// judge before any verdict below is believed.
	if !strings.Contains(src, "chatRatePerHour = 60.0") || !strings.Contains(src, "handleChatConfirm") {
		t.Fatalf("%s is not the chat REST file this guard reads the cost comment from — "+
			"retarget the guard rather than letting it judge an unrelated source",
			restChatSourceFile)
	}

	problems := costCommentProblems(src, chatRatePerHour, chat.MaxSteps)
	if len(problems) != 0 {
		t.Fatalf("the chat's cost prose and the constants that set it disagree:\n  - %s",
			strings.Join(problems, "\n  - "))
	}
}

// TestChatCostCommentGuardBites drives the SAME rule over mutated sources: each row
// is a way the prose can be wrong while the constants are what they are.
//
// Every mutation is DERIVED from the shipped source or the shipped constants — the
// undercount is the same arithmetic with MaxSteps instead of MaxSteps + 1, not the
// literal 240 — so a row cannot rot into a no-op the day the shipped value changes.
func TestChatCostCommentGuardBites(t *testing.T) {
	src := readRestChatSource(t)
	perHour, steps := chatRatePerHour, chat.MaxSteps
	want := int(perHour) * (steps + 1)
	under := int(perHour) * steps

	// CONTROL: the shipped file and the shipped constants are green, so every red
	// below is attributable to its own mutation.
	if p := costCommentProblems(src, perHour, steps); len(p) != 0 {
		t.Fatalf("CONTROL: the shipped %s and the shipped constants are not green:\n  - %s",
			restChatSourceFile, strings.Join(p, "\n  - "))
	}

	// The two anchors the rows below mutate. If either is absent the rows land
	// nowhere and would prove nothing while staying green.
	stated := fmt.Sprintf("at most %d model calls per user per hour", want)
	if strings.Count(src, stated) != 1 {
		t.Fatalf("the shipped file states %q %d times, want exactly 1 — re-anchor this proof",
			stated, strings.Count(src, stated))
	}
	if !strings.Contains(src, "MaxSteps + 1") {
		t.Fatalf("the shipped file never writes %q — the derivation this guard demands is not in "+
			"the sentence it is supposed to be judging", "MaxSteps + 1")
	}

	// (1) THE DEFECT, DERIVED: every `MaxSteps + 1` reverted to `MaxSteps` and the
	// product reverted to chatRatePerHour x MaxSteps — the shipped comment's state,
	// which is the 25% undercount the card measured.
	defective := strings.ReplaceAll(src, "MaxSteps + 1", "MaxSteps")
	defective = strings.Replace(defective, stated,
		fmt.Sprintf("at most %d model calls per user per hour", under), 1)
	if defective == src {
		t.Fatal("the defect mutation changed nothing — this row would be vacuous")
	}
	defectProblems := costCommentProblems(defective, perHour, steps)
	if !hasProblem(defectProblems, strconv.Itoa(under)) || !hasProblem(defectProblems, strconv.Itoa(want)) {
		t.Fatalf("the undercounted ceiling (%d against the constants' %d) is NOT reported:\n  - %s",
			under, want, strings.Join(defectProblems, "\n  - "))
	}
	if !hasProblem(defectProblems, "MaxSteps + 1") {
		t.Fatalf("the derivation-from-MaxSteps-alone half is NOT reported, so the unit defect walks "+
			"through the guard:\n  - %s", strings.Join(defectProblems, "\n  - "))
	}

	// (2) THE NUMBER ALONE, with the derivation correct: reds on the arithmetic
	// only, so the two rules are not one rule wearing two names.
	wrongNumber := strings.Replace(src, stated,
		fmt.Sprintf("at most %d model calls per user per hour", under), 1)
	numberProblems := costCommentProblems(wrongNumber, perHour, steps)
	if len(numberProblems) != 1 {
		t.Fatalf("only the number moved and the guard reports %d problems, want exactly 1 (the "+
			"arithmetic):\n  - %s", len(numberProblems), strings.Join(numberProblems, "\n  - "))
	}
	if !hasProblem(numberProblems, strconv.Itoa(under)) || !hasProblem(numberProblems, strconv.Itoa(want)) {
		t.Fatalf("the arithmetic complaint does not name both numbers (%d, %d): %q",
			under, want, numberProblems[0])
	}

	// (3) THE UNIT ALONE: the number stays the ceiling the constants imply but is
	// called tool round-trips — the half of the defect that is not arithmetic.
	wrongUnit := strings.Replace(src, stated,
		fmt.Sprintf("at most %d tool round-trips per user per hour", want), 1)
	unitProblems := costCommentProblems(wrongUnit, perHour, steps)
	if !hasProblem(unitProblems, "model calls") {
		t.Fatalf("a ceiling stated in tool round-trips instead of model calls is NOT reported:\n  - %s",
			strings.Join(unitProblems, "\n  - "))
	}

	// (4) THE HEADER ALONE: the first `MaxSteps + 1` in the file (the header's
	// derivation) loses its +1 while the const block and the number stay correct.
	// The header is judged, and it is judged by the derivation rule rather than by
	// the arithmetic (which has nothing to complain about here).
	headerOnly := strings.Replace(src, "MaxSteps + 1", "MaxSteps", 1)
	if headerOnly == src {
		t.Fatal("the header mutation changed nothing — this row would be vacuous")
	}
	headerProblems := costCommentProblems(headerOnly, perHour, steps)
	if len(headerProblems) != 1 || !hasProblem(headerProblems, "up to chat.MaxSteps of them") {
		t.Fatalf("the header's derivation is not judged on its own (want exactly one problem, "+
			"naming the header's sentence):\n  - %s", strings.Join(headerProblems, "\n  - "))
	}

	// (5) THE CONSTANTS MOVE, the prose does not: MaxSteps up, then the rate up.
	// This is the drift the guard exists for — the comment was correct at 60 x 4 and
	// became wrong the day either number moved.
	movedSteps := steps + 3
	stepProblems := costCommentProblems(src, perHour, movedSteps)
	if !hasProblem(stepProblems, strconv.Itoa(int(perHour)*(movedSteps+1))) {
		t.Fatalf("MaxSteps moved to %d and the comment still states %d, yet the guard reports no "+
			"problem naming %d:\n  - %s", movedSteps, want, int(perHour)*(movedSteps+1),
			strings.Join(stepProblems, "\n  - "))
	}
	movedRate := perHour + 30
	rateProblems := costCommentProblems(src, movedRate, steps)
	if !hasProblem(rateProblems, strconv.Itoa(int(movedRate)*(steps+1))) {
		t.Fatalf("chatRatePerHour moved to %.0f and the comment still states %d, yet the guard "+
			"reports no problem naming %d:\n  - %s", movedRate, want, int(movedRate)*(steps+1),
			strings.Join(rateProblems, "\n  - "))
	}

	// (6) THE SENTENCE GONE, and (7) a SECOND COPY of it: neither may pass quietly.
	silent := strings.Replace(src, stated, "a bounded number of model calls per user per hour", 1)
	if !hasProblem(costCommentProblems(silent, perHour, steps), "no longer states") {
		t.Fatalf("a file that stops stating the ceiling does not red the guard — a reword would " +
			"retire the check silently")
	}
	twice := src + "\n// " + stated + ".\n"
	if !hasProblem(costCommentProblems(twice, perHour, steps), "states the model-call ceiling 2 times") {
		t.Fatalf("a second copy of the ceiling does not red the guard — two statements of one " +
			"number is the shape that produced the 240")
	}
}

// hasProblem reports whether any problem sentence contains substr.
func hasProblem(problems []string, substr string) bool {
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}
