package dbstore

// The reserved-LIST leg of the workspace-slug rule, and the SENTINEL the two
// transports classify it by (card t_b7f81f5e, the rework of PR 691).
//
// WHY THIS FILE EXISTS. The sibling guard (workspace_slug_rule_guard_test.go)
// pins the reserved leg's ARCHITECTURE by reading source: it proves the leg
// lives in the ONE rule and that the shared urlguard wording is used. Neither of
// those assertions can see the property the TRANSPORTS actually consume — that
// the refusal satisfies a sentinel. Every transport switch matches by
// `errors.Is(err, <sentinel>)`, and the reserved leg used to return a FRESH
// `errors.New(urlguard.ReservedSubdomainMessage(slug))`, which satisfied none of
// them. The rule was enforced and the refusal was unreportable:
//
//	REST rename (rest_workspaces.go)  -> no arm matched -> respondErr -> 500
//	                                     {"code":"internal"} + an ERROR log and
//	                                     a stack trace for plain user input;
//	MCP rename (mcpsrv/workspaces.go) -> fell to `fmt.Errorf("db: %w", err)`, so
//	                                     an agent read `db: "<label>" is
//	                                     reserved for JomForm: …`.
//
// So the property pinned here is IDENTITY, not spelling: the refusal must be
// recognisable as the reserved-LIST refusal, must carry the shared wording, and
// must NOT be confused with the *jomform TAIL refusal (a different rule with a
// different wire answer — the generic slug wording and the generic slug code).
// A guard on the source text passes over both of those failures.
//
// Source- and table-only: no database, so it runs in the plain `go test ./...`
// pass rather than only in the integration suite.

import (
	"errors"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/urlguard"
)

// TestReservedLegRefusalSatisfiesASentinel is the core assertion: a reserved
// label is refused with an error the transports can classify with errors.Is.
func TestReservedLegRefusalSatisfiesASentinel(t *testing.T) {
	_, err := ValidateWorkspaceSlug("billing")
	if err == nil {
		t.Fatalf("ValidateWorkspaceSlug(%q) accepted a reserved label", "billing")
	}
	if !errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
		t.Errorf("ValidateWorkspaceSlug(%q) error %v does NOT satisfy ErrWorkspaceSlugReservedSubdomain. "+
			"Every transport classifies a refusal with errors.Is, so a refusal that satisfies no sentinel "+
			"cannot be reported: REST rename falls through its switch to respondErr (500 internal, with an "+
			"ERROR log and a stack trace for user input) and MCP rename wraps it as `db: …`. This is the "+
			"defect the card exists for — do not fix it by adding a prose match in the transports.",
			"billing", err)
	}

	// The shared wording, verbatim: the transports forward the rule's own text to
	// the wire, and urlguard's message is what says WHY the name is refused (and
	// names the label). A fixed sentinel string here would answer the operator
	// with no label at all.
	if got, want := err.Error(), urlguard.ReservedSubdomainMessage("billing"); got != want {
		t.Errorf("the reserved refusal reads %q, want the shared urlguard wording %q — a second "+
			"wording is a second rule, and the wire `error` is what curl/API-key/MCP callers read", got, want)
	}

	// NOT the tail rule. ErrWorkspaceSlugReserved is the "*jomform" refusal: the
	// caller's input is shaped badly, so it answers the generic slug wording and
	// the generic slug code. Collapsing the two would tell a merchant with a
	// well-formed reserved name that their typing was wrong — and would send the
	// dashboard to its "slug must be 3-40 chars" sentence instead of the reserved
	// one (#694).
	if errors.Is(err, ErrWorkspaceSlugReserved) {
		t.Errorf("the reserved-LIST refusal also satisfies ErrWorkspaceSlugReserved. The two legs are "+
			"different rules with different wire answers: the tail rule answers the generic slug "+
			"wording/code, the reserved list answers %q", urlguard.ReservedSubdomainCode)
	}
	// Nor is it a shape refusal.
	for _, shape := range []error{ErrWorkspaceSlugLength, ErrWorkspaceSlugChars} {
		if errors.Is(err, shape) {
			t.Errorf("the reserved-LIST refusal also satisfies %v — a well-formed name would be reported "+
				"as malformed input, and the merchant would retry a variation of a name JomForm owns", shape)
		}
	}
}

// TestEveryReservedLabelIsClassified proves the sentinel covers the WHOLE list
// rather than the one label a hand-written test happened to name, and that the
// message names the label that was asked for.
func TestEveryReservedLabelIsClassified(t *testing.T) {
	// A sample spanning every category in the list (infrastructure, routed
	// JomForm surfaces, impersonation names) so a leg that only classifies one
	// group fails here.
	labels := []string{
		"www", "api", "app", "mail", "s3",
		"docs", "mcp", "templates", "custom",
		"support", "help", "status", "billing",
		"admin", "account", "accounts", "login", "signup", "security", "blog",
		"payments", "pay", "checkout", "cdn", "assets",
	}
	for _, label := range labels {
		if !urlguard.ReservedSubdomain(label) {
			t.Fatalf("fixture is wrong: %q is not on the reserved list, so this loop no longer proves "+
				"the leg classifies the whole list", label)
		}
		norm, err := ValidateWorkspaceSlug(label)
		if err == nil {
			t.Errorf("ValidateWorkspaceSlug(%q) = (%q, nil), want a refusal", label, norm)
			continue
		}
		if !errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
			t.Errorf("ValidateWorkspaceSlug(%q) error %v does not satisfy "+
				"ErrWorkspaceSlugReservedSubdomain — a transport cannot classify it and answers 500/db:", label, err)
		}
		// The WHY must name THIS label: a message that says only "reserved for
		// JomForm" leaves the caller guessing which of their inputs was refused.
		if !strings.Contains(err.Error(), label) {
			t.Errorf("ValidateWorkspaceSlug(%q) refusal %q does not name the label it refused", label, err)
		}
	}

	// The normalisation still happens BEFORE the list is consulted (the leg reads
	// the trimmed, lower-cased value), so a caller's "BILLING" is classified too.
	// Asserted because the sentinel is what the transports match on: a leg that
	// only classified the already-normalised spelling would leave the mixed-case
	// path answering 500 on REST and `db:` on MCP.
	if _, err := ValidateWorkspaceSlug("  BILLING  "); !errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
		t.Errorf("ValidateWorkspaceSlug(%q) = %v, want ErrWorkspaceSlugReservedSubdomain — the list is "+
			"consulted after trimming/lower-casing, so a caller's own casing must classify the same way",
			"  BILLING  ", err)
	}
}

// TestReservedLegStillPrecedesTheShapeRule is the behaviour behind the sibling
// guard's source-order assertion, stated as the answer a caller receives: "s3"
// is reserved AND shorter than the 3-character minimum, so the POLICY refusal
// must win. If the length leg ran first the caller would be told their input was
// malformed and would retry a variation of a name JomForm owns — and the
// transports would answer the generic slug code instead of reserved_subdomain.
func TestReservedLegStillPrecedesTheShapeRule(t *testing.T) {
	if len("s3") >= 3 {
		t.Fatal("fixture is wrong: \"s3\" must be shorter than the 3-character minimum for this test to mean anything")
	}
	_, err := ValidateWorkspaceSlug("s3")
	if !errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
		t.Errorf("ValidateWorkspaceSlug(\"s3\") = %v, want the reserved-LIST refusal. It is both reserved and "+
			"under the minimum length: the shape rule answered first, so the merchant is told their input "+
			"was malformed and retries a variation of a name JomForm owns", err)
	}
	// The control: an equally short but NON-reserved label still gets the shape
	// refusal, so this test cannot pass by the length leg having been deleted.
	if _, err := ValidateWorkspaceSlug("ab"); !errors.Is(err, ErrWorkspaceSlugLength) {
		t.Errorf("ValidateWorkspaceSlug(\"ab\") = %v, want ErrWorkspaceSlugLength — the length rule was lost", err)
	}
}

// TestNonReservedLabelsAreStillAccepted is the other half: the sentinel must not
// have turned the rule into a blanket refusal. A leg that classified everything
// as reserved would satisfy every assertion above.
func TestNonReservedLabelsAreStillAccepted(t *testing.T) {
	for _, ok := range []string{"acme", "kedai-bunga", "a-b", "shop1"} {
		norm, err := ValidateWorkspaceSlug(ok)
		if err != nil {
			t.Errorf("ValidateWorkspaceSlug(%q) = %v, want it accepted — the reserved leg is over-reaching "+
				"and refusing a legitimate merchant address", ok, err)
			continue
		}
		if norm != ok {
			t.Errorf("ValidateWorkspaceSlug(%q) = %q, want the normalised value back", ok, norm)
		}
	}
}
