package chat

// Guard for the system prompt's claim about WHERE a permanent delete is reached
// (card t_0a2f47fe).
//
// WHY THIS FILE EXISTS
//
// The prompt told the model to send the merchant to the dashboard for "deleting",
// with no exception:
//
//	Everything else is refused, and you should tell the user to do it themselves
//	on the dashboard: deleting, archiving, refunding, ...
//
// Measured live by QA (card t_7aa0d101), the model then told a merchant, three
// times and in escalating detail, to do the permanent workspace delete from
// "Workspaces → open that workspace → its settings → delete" and, when pressed,
// "settings/danger zone". No such control exists:
//
//   - `web/src/components/WorkspaceTab.tsx` renders ONE button per Trash row
//     (`ws.trashRestore` -> POST /api/workspaces/{id}/restore) and its own header
//     says "PERMANENT DELETE IS NOT OFFERED HERE";
//   - nothing in `web/src` calls DELETE /v1/workspaces/{id}/purge — the sibling
//     guard in api/internal/mcpsrv asserts that over the whole tree;
//   - the served client JS contains zero hits for `workspaces/<id>/purge`.
//
// So the model was instructed to state something untrue about the product, and it
// did. The merchant's consequence is the worst kind: they go looking for the control,
// do not find it, and conclude the product is broken — or that they are holding it
// wrong — with no error to show anyone.
//
// WHAT IS PINNED HERE, and why each assertion can fail
//
//   1. The permanent step is NAMED correctly where the prompt describes it (the REST
//      API / an MCP client for the owner). A prompt that quietly drops the sentence
//      is back to the pre-fix state where "deleting" was one undifferentiated verb.
//   2. "deleting" must NOT appear in the dashboard sentence, while "archiving" must.
//      That is the fix in one assertion: the class the model was told to redirect was
//      too broad by exactly one verb, and this stops it being put back.
//   3. The prompt must still tell the model to refuse, and to propose nothing —
//      dropping that would trade this fix for the guarantee the sibling guard
//      (mcpsrv, TestWorkspacePurgeClaimMatchesWhatTheRepoEnforces) exists to hold.
//   4. THE BITE PROOF lives in TestTheRedirectSentenceCannotReacquireDeleting: it
//      takes the PRE-FIX wording of the dashboard sentence, substitutes it into the
//      REAL prompt template in memory (nothing on disk is written), and requires the
//      check in (2) to go RED. A guard whose failure mode is only ever observed in a
//      shell harness rots quietly with every gate green; this one is exercised inside
//      the gate that runs it, on the real text.

import (
	"strings"
	"testing"
)

// dashboardRedirectSentence is the substring of the prompt that names the dashboard
// as the place to do things the chat cannot. It is located by its own stable opening
// rather than by line number, so a re-wrap of the surrounding paragraph does not
// silently move the assertions onto a different sentence.
const dashboardRedirectSentence = "tell the user to do it themselves on"

// redirectSection returns the whole passage the model is told about dashboard
// actions — from that opening up to the next section heading.
//
// It deliberately does NOT stop at the first blank line. The pre-fix wording was a
// single paragraph, but the corrected wording is two (the sentence, then the
// exception that names where the permanent step really is), and a helper that
// stopped at the blank line would judge only the first half while reporting on the
// whole — the assertions would pass or fail on text the reader was not shown.
func redirectSection(prompt string) string {
	i := strings.Index(prompt, dashboardRedirectSentence)
	if i < 0 {
		return ""
	}
	rest := prompt[i:]
	// The passage ends at the next section heading (CONFIRMATION / HOW TO ANSWER).
	if j := strings.Index(rest, "\n\nCONFIRMATION"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// normalizeWS collapses runs of whitespace to a single space, so an assertion about a
// SENTENCE does not fail merely because the prompt template was re-wrapped. The
// template is one long raw string literal whose line breaks are presentational; a
// guard that pins them would report a re-wrap as a behaviour change.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func TestSystemPromptSendsPermanentDeletesToTheApiNotTheDashboard(t *testing.T) {
	prompt := systemPromptText("")
	para := normalizeWS(redirectSection(prompt))
	if para == "" {
		t.Fatalf("the prompt no longer contains %q at all, so this guard cannot judge where "+
			"dashboard actions are said to live; a guard that reads nothing proves nothing",
			dashboardRedirectSentence)
	}

	// 1. The permanent step is named, and named where it really is. Both halves are
	//    required: naming it without saying "not the dashboard" leaves the model free
	//    to volunteer a dashboard route from its own priors, which is what it did.
	for _, want := range []string{"REST API", "MCP client"} {
		if !strings.Contains(para, want) {
			t.Errorf("the dashboard-redirect passage no longer names %q as where the permanent "+
				"delete is reached. The model has no other source for this fact, and without it "+
				"it invents a dashboard control (measured: QA card t_7aa0d101)", want)
		}
	}
	if !strings.Contains(para, "no dashboard control does that") {
		t.Error("the passage no longer states that NO dashboard control performs a permanent delete. " +
			"That negative is the load-bearing half: the live replies did not merely omit the API, " +
			"they asserted a dashboard route that does not exist")
	}

	// 2. THE FIX, as one assertion. `deleting` was the over-broad verb: it swept the
	//    permanent step into a sentence about dashboard actions, and the permanent step
	//    is the one thing no dashboard performs. `archiving` must stay, because archiving
	//    IS a dashboard action — so this cannot be satisfied by gutting the sentence.
	if strings.Contains(para, "deleting") {
		t.Error("the dashboard-redirect passage has `deleting` back in it. The in-app chat " +
			"refuses to propose a permanent delete AND no dashboard control performs one, so " +
			"grouping `deleting` with `archiving`/`refunding` tells the model to send the " +
			"merchant to a control that does not exist (card t_0a2f47fe, measured live)")
	}
	if !strings.Contains(para, "archiving") {
		t.Error("the passage no longer mentions `archiving`, which IS a dashboard action — " +
			"removing the real dashboard actions is not the fix, redirecting the permanent step is")
	}

	// 3. The refusal duty survives. This fix must not be bought by widening what the
	//    model will offer to do.
	if !strings.Contains(normalizeWS(prompt), "say plainly that you cannot do it") {
		t.Error("the prompt no longer tells the model to refuse plainly — the sibling guard in " +
			"api/internal/mcpsrv pins that the chat may never PROPOSE a permanent delete; this " +
			"prompt must keep telling the model to refuse rather than comply")
	}
}

// TestTheRedirectSentenceCannotReacquireDeleting is the bite proof.
//
// It puts the PRE-FIX wording of the dashboard sentence back into the REAL prompt
// template, in memory, and requires the check from the test above to go RED. Nothing
// on disk is written: the mutated text is only ever a local string.
//
// This is what separates a guard from a comment. Without it, deleting the assertion
// above — or reverting the prompt and deleting the test together — would leave every
// gate green, which is exactly how this defect shipped in the first place.
func TestTheRedirectSentenceCannotReacquireDeleting(t *testing.T) {
	preFix := "tell the user to do it themselves on\n" +
		"the dashboard: deleting, archiving, refunding, cancelling a subscription,\n" +
		"changing a password, keys, members or settings, sending email, publishing or\n" +
		"unpublishing a form, and any bulk operation."

	// The mutation must actually land, or the proof below is vacuous. Assert that
	// against the real template, not against a hand-written copy of it.
	mutated := strings.Replace(systemPromptText(""), redirectSection(systemPromptText("")), preFix, 1)
	if mutated == systemPromptText("") {
		t.Fatal("the pre-fix wording could not be substituted into the real prompt — this bite " +
			"proof would pass without testing anything, which is the failure mode it exists to " +
			"prevent. Fix the substitution, do not delete the proof")
	}
	if !strings.Contains(mutated, "deleting") {
		t.Fatal("the substituted text does not carry `deleting`, so the mutation did not reproduce " +
			"the pre-fix state this proof is about")
	}

	// The same judgement the guard above makes, applied to the mutated text. It MUST
	// find the defect.
	para := redirectSection(mutated)
	if !strings.Contains(para, "deleting") {
		t.Error("with the pre-fix wording substituted in, the guard still does not see `deleting` " +
			"in the dashboard-redirect paragraph — so the live check above cannot fail on a " +
			"regression and this guard is decorative")
	}
}
