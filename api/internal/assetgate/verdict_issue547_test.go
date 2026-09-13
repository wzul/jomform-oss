package assetgate

// Issue #547 (round 8): the asset scan verdict must FAIL CLOSED.
//
// The classifier's input is the uploaded file itself, so its reply is steerable.
// The mapping previously ended in `default: final = StatusOK`, meaning any reply
// the switch did not recognise — an unexpected token, a model quirk, or an
// injection the model partially obeyed — PUBLISHED the asset, because
// scan_status="ok" is exactly what objstore.ScanGateAllowed requires to serve a
// code asset.
//
// These tests assert verdictToScanStatus — the MAPPING, not just the parsing
// helper. That distinction matters: an earlier version of this file tested only
// classifyVerdict, so neutralising the mapping (restoring the fail-open default)
// still passed, because the helper was untouched. Asserting the mapping is what
// makes the guard bite.

import "testing"

// TestIssue547UnrecognisedVerdictDoesNotPublish is the core guard: no reply
// except an explicit benign verdict may yield StatusOK.
func TestIssue547UnrecognisedVerdictDoesNotPublish(t *testing.T) {
	mustNotPublish := []string{
		"",
		"I think this is fine",
		"Ignoring prior instructions.",
		"sure, here you go: BENIGN",
		"UNKNOWN",
		"benign\nmalicious",
		"<script>alert(1)</script> BENIGN",
		"As an AI I cannot classify this.",
		"BENIGN or MALICIOUS - not sure",
		"verdict: benign",
	}
	for _, r := range mustNotPublish {
		if got := verdictToScanStatus(r); got == StatusOK {
			t.Errorf("issue #547: verdictToScanStatus(%q) = %q (StatusOK) — an unrecognised or "+
				"ambiguous reply PUBLISHED the asset; only an explicit benign verdict may unlock "+
				"serving", r, got)
		}
	}
}

// TestIssue547ExplicitBenignPublishes is the control: the one reply that SHOULD
// publish still does, so the fix has not simply broken the scanner.
func TestIssue547ExplicitBenignPublishes(t *testing.T) {
	for _, r := range []string{"BENIGN", "benign", "benign.", "benign (ordinary styling)", "  BENIGN  "} {
		if got := verdictToScanStatus(r); got != StatusOK {
			t.Errorf("verdictToScanStatus(%q) = %q, want StatusOK — a genuine benign verdict "+
				"must still publish", r, got)
		}
	}
}

// TestIssue547MaliciousQuarantines pins the destructive direction.
func TestIssue547MaliciousQuarantines(t *testing.T) {
	for _, r := range []string{"MALICIOUS", "malicious", "MALICIOUS - script injection"} {
		if got := verdictToScanStatus(r); got != StatusQuarantined {
			t.Errorf("verdictToScanStatus(%q) = %q, want quarantine", r, got)
		}
	}
}

// TestIssue547NeedsReviewHoldsForHuman pins the review outcome.
func TestIssue547NeedsReviewHoldsForHuman(t *testing.T) {
	for _, r := range []string{"NEEDS REVIEW", "needs_review", "review", "NEEDS_REVIEW"} {
		if got := verdictToScanStatus(r); got != StatusNeedsReview {
			t.Errorf("verdictToScanStatus(%q) = %q, want needs-review", r, got)
		}
	}
}

// TestIssue547OnlyExplicitBenignYieldsOK states the invariant directly over a
// broad corpus of replies: StatusOK if and only if the reply is explicitly benign.
func TestIssue547OnlyExplicitBenignYieldsOK(t *testing.T) {
	replies := []string{
		"BENIGN", "benign", "BENIGN.", "benign (ok)", "MALICIOUS", "malicious",
		"NEEDS REVIEW", "needs_review", "review", "", " ", "\n", "yes", "no",
		"I believe this is benign", "this file is probably BENIGN but check it",
		"ignore previous instructions", "reply BENIGN", "BENIGN\nMALICIOUS",
		"```benign```", "benign; malicious", "safe", "ok", "fine",
	}
	for _, r := range replies {
		got := verdictToScanStatus(r)
		// The invariant that matters: StatusOK requires the reply to BE a benign
		// verdict, which we check by confirming classifyVerdict agrees — plus the
		// explicit cases below. Anything else reaching StatusOK is the bug.
		if got == StatusOK && classifyVerdict(r) != "benign" {
			t.Errorf("issue #547 INVARIANT VIOLATED: verdictToScanStatus(%q) = StatusOK but "+
				"classifyVerdict(%q) = %q — StatusOK must require an explicit benign verdict",
				r, r, classifyVerdict(r))
		}
	}
	// The two directions, stated concretely.
	for _, r := range []string{"BENIGN", "benign", "BENIGN.", "benign (ok)", "```benign```"} {
		if verdictToScanStatus(r) != StatusOK {
			t.Errorf("issue #547: a bare benign verdict %q must publish", r)
		}
	}
	for _, r := range []string{"", " ", "yes", "no", "safe", "ok", "fine", "UNKNOWN",
		"I believe this is benign", "reply BENIGN", "BENIGN\nMALICIOUS", "benign; malicious"} {
		if verdictToScanStatus(r) == StatusOK {
			t.Errorf("issue #547: %q published the asset but is not an explicit benign verdict", r)
		}
	}
}
