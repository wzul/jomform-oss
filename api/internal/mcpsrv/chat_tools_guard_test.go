package mcpsrv

// The in-app chat's tool-exposure guard (phase 1 read-only → phase 2 propose-only).
//
// THE MEANING CHANGED, THE GUARD DID NOT GET LOOSER.
//
// Phase 1 asserted "nothing but reads and submit_feedback is allowed". Phase 2
// adds a PROPOSAL channel, so the claim is restated as the pair:
//
//	the model may PROPOSE a write in the proposable set;
//	the model may never EXECUTE a write — every registered tool that reaches a
//	store write is UNAVAILABLE to the dispatch path, whatever it is called with.
//
// The second half is the one that matters, and it is now asserted over the WHOLE
// registry rather than over a hand-written list: for every tool this server
// registers that reaches a write sink, ChatToolIsAllowed must be FALSE. That is
// strictly stronger than the phase-1 test, which only checked that the read list
// contained no writer — a widened allowlist could have passed it in one direction
// while quietly making a writer dispatchable in the other.
//
// The proposal half is guarded too, in the opposite direction: a name may only be
// proposable if it IS a registered writer behind the write-scope gate, so
// "proposable" cannot be used to sneak a tool into a state where nothing
// authorises it, and a typo cannot make a proposal permanently unrenderable.
//
// WHAT THIS DELIBERATELY DOES NOT PROVE. That the proposable set is well chosen.
// No predicate can tell whether a given reversible edit belongs in front of a
// model — that is a judgement, written out class by class in chat_tools.go and
// reviewable there. What is checkable IS checked here: the excluded CLASSES are
// enforced by name, and the set is kept small enough that each addition has to
// argue with a number.
//
// It reuses the call graph from scope_gate_guard_test.go (same package), so the
// chat's exposure is measured by the SAME parse that polices every registered
// tool's authorization — not by a second, weaker reader.

import (
	"sort"
	"strings"
	"testing"
)

// TestChatReadAllowlistContainsNoWriter is the guard on the chat's read list.
//
// A read tool that can write is the phase-1 failure mode: the allowlist is the
// layer that decides what the chat may even attempt. The identity layer (readonly
// scopes) is the second net, but a guard that only relied on it would let the
// allowlist quietly become "everything", at which point the refusal a user sees
// is an error message rather than the absence of the capability.
func TestChatReadAllowlistContainsNoWriter(t *testing.T) {
	pkg := parsePackage(t)

	byName := map[string]toolReg{}
	for _, tool := range pkg.tools {
		byName[tool.Name] = tool
	}

	read := ChatReadTools()
	if len(read) < 15 {
		t.Fatalf("only %d read tools in the chat allowlist — the guard would pass vacuously "+
			"(the chat needs the merchant's own read surface)", len(read))
	}

	var writers, unregistered []string
	seen := map[string]bool{}
	for _, name := range read {
		if seen[name] {
			t.Errorf("chat read allowlist lists %q twice", name)
		}
		seen[name] = true

		tool, ok := byName[name]
		if !ok {
			// A typo here would not fail anything else: the tool would simply
			// never be offered, and the model would answer "I have no tool for
			// that" forever. So an unregistered name is a failure.
			unregistered = append(unregistered, name)
			continue
		}
		if isWrite, sinks := pkg.reachesWriteSink(tool.Handler); isWrite {
			writers = append(writers, name+" → "+strings.Join(sinks, ", "))
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("the chat read allowlist names %d tool(s) that are NOT REGISTERED: %v. "+
			"A typo'd name is silently unreachable — the model would keep answering \"I have "+
			"no tool for that\" with the whole suite green.", len(unregistered), unregistered)
	}
	if len(writers) > 0 {
		sort.Strings(writers)
		t.Errorf("the chat's READ allowlist contains %d tool(s) that can reach a store write:\n  %s\n"+
			"Either move the tool out of the list or — if it is genuinely a read — fix the "+
			"write-sink classification, but do not leave a writer reachable by the chat.",
			len(writers), strings.Join(writers, "\n  "))
	}
	t.Logf("chat read allowlist: %d tools, all registered, none reaches a store write", len(read))
}

// TestChatSubmitFeedbackIsRegisteredAndScopeGated pins the ONE in-process write.
//
// Two properties, both required:
//
//	registered  — AGENTS.md parity: the dashboard capability has an MCP tool, and
//	              the parity guard reads the registered set.
//	scope-gated — filing is a write, so a readonly credential must be refused.
//	              This is the same gate issue #645 restored; a tool that files
//	              feedback without it would let a readonly key flood the queue.
//
// And it must NOT be in the read list: "read-only apart from one in-process write"
// only means something if the two sets are disjoint.
func TestChatSubmitFeedbackIsRegisteredAndScopeGated(t *testing.T) {
	pkg := parsePackage(t)

	var found *toolReg
	for i := range pkg.tools {
		if pkg.tools[i].Name == ChatSubmitFeedbackTool {
			found = &pkg.tools[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("%s is not registered — the in-app chat's filing write would exist on the "+
			"dashboard with no MCP counterpart (AGENTS.md MCP FEATURE PARITY)", ChatSubmitFeedbackTool)
	}
	if !pkg.reachesScopeGate(found.Handler) {
		t.Errorf("%s reaches the store without requireWriteScope — a readonly credential could "+
			"file feedback (the issue #645 shape, on the chat's own write path)", ChatSubmitFeedbackTool)
	}
	if isWrite, _ := pkg.reachesWriteSink(found.Handler); !isWrite {
		t.Errorf("%s is classified as reaching NO store write, but it files a feedback thread. "+
			"The write-sink predicate has lost a case; fix the predicate rather than this test "+
			"(exactWriteMethods / nonSinkMethods in scope_gate_guard_test.go).", ChatSubmitFeedbackTool)
	}
	for _, name := range ChatReadTools() {
		if name == ChatSubmitFeedbackTool {
			t.Errorf("%s appears in the READ allowlist as well as being the in-process write", ChatSubmitFeedbackTool)
		}
	}
}

// TestChatCannotDispatchAnyRegisteredWriter is THE phase-2 guarantee, asserted over
// the whole registry.
//
// The card's central requirement is that the model can never EXECUTE a write, with
// or without a `confirm` argument of its own invention. That is not a statement
// about a hand-written list of read tools — it is a statement about every tool the
// server registers: each one that can reach a store write must be unavailable to
// the chat's dispatch path.
//
// This is the direction phase 1 did not check, and it is the one that would have
// caught the tempting mistake for phase 2 — "let the model call update_product,
// it will propose instead" — because update_product reaches a store write and so
// must answer false here no matter how the proposal lane is built.
func TestChatCannotDispatchAnyRegisteredWriter(t *testing.T) {
	pkg := parsePackage(t)
	if len(pkg.tools) < 100 {
		t.Fatalf("parsed only %d registered tools — the sweep would be meaningless", len(pkg.tools))
	}

	var dispatchable []string
	checked := 0
	for _, tool := range pkg.tools {
		isWrite, sinks := pkg.reachesWriteSink(tool.Handler)
		if !isWrite {
			continue
		}
		// The chat's own in-process filing tool is the single registered writer
		// the chat may run — it is executed through feedbacksvc, not by naming a
		// tool for the model to reach, and it is pinned separately above.
		if tool.Name == ChatSubmitFeedbackTool {
			continue
		}
		checked++
		if ChatToolIsAllowed(tool.Name) {
			dispatchable = append(dispatchable, tool.Name+" → "+strings.Join(sinks, ", "))
		}
	}
	if checked < 20 {
		t.Fatalf("only %d registered writer(s) found — the sweep found almost nothing, so it "+
			"would pass even if every writer were dispatchable", checked)
	}
	if len(dispatchable) > 0 {
		sort.Strings(dispatchable)
		t.Errorf("the chat can DISPATCH %d registered tool(s) that reach a store write:\n  %s\n"+
			"Phase 2 lets the model PROPOSE a write; it must never let it RUN one. A writer "+
			"reachable here is a write the model can perform by itself, which is exactly the "+
			"guarantee the proposal lane exists to provide.", len(dispatchable),
			strings.Join(dispatchable, "\n  "))
	}
	t.Logf("swept %d registered writers; none is dispatchable by the chat", checked)
}

// TestChatProposableSetIsRegisteredWriterBehindTheScopeGate guards the OTHER
// direction: what may be proposed.
//
// Three properties per name, so "proposable" can never mean "a tool with no gate
// that nothing authorises":
//
//	registered     — a proposable name that is not a real tool produces a card
//	                 whose confirm always fails. The user would be shown a
//	                 decision that cannot be carried out.
//	reaches a write — a proposable READ is a contradiction: the model can already
//	                 call it, so proposing it would put a no-op confirmation in
//	                 front of the user.
//	scope-gated    — a proposable writer without the write-scope gate would be a
//	                 tool the confirm path could run with a credential that may
//	                 not write, i.e. the #645 shape reachable through the chat.
func TestChatProposableSetIsRegisteredWriterBehindTheScopeGate(t *testing.T) {
	pkg := parsePackage(t)

	byName := map[string]toolReg{}
	for _, tool := range pkg.tools {
		byName[tool.Name] = tool
	}

	prop := ChatProposableTools()
	if len(prop) == 0 {
		t.Fatal("the chat's proposable set is empty — the proposal lane has nothing to offer")
	}
	seen := map[string]bool{}
	for _, name := range prop {
		if seen[name] {
			t.Errorf("proposable set lists %q twice", name)
		}
		seen[name] = true
		tool, ok := byName[name]
		if !ok {
			t.Errorf("%q is in the chat's proposable set but is NOT a registered tool — the "+
				"confirm would always fail", name)
			continue
		}
		if isWrite, _ := pkg.reachesWriteSink(tool.Handler); !isWrite {
			t.Errorf("%q is proposable but reaches NO store write — the model can already call "+
				"a read, so proposing it asks the user to confirm a no-op", name)
		}
		if !pkg.reachesScopeGate(tool.Handler) {
			t.Errorf("%q is proposable but does not reach the write-scope gate — the confirm "+
				"path could run it with a credential that may not write (issue #645)", name)
		}
		if ChatToolIsAllowed(name) {
			t.Errorf("%q is BOTH proposable and dispatchable — the model could execute it "+
				"directly, which is the one thing phase 2 must prevent", name)
		}
	}
	t.Logf("proposable set: %d tools, all registered, all writers, all behind the scope gate, "+
		"none dispatchable", len(prop))
}

// TestChatProposableExcludesTheDestructiveClasses pins the product decision by
// CLASS, not by example.
//
// The card requires the phase-2 set to start small and reversible, with
// delete/refund/archive explicitly excluded. A future edit that added
// delete_form "because it is just another edit" would have to defeat this test and
// therefore argue with the exclusions written beside it in chat_tools.go.
func TestChatProposableExcludesTheDestructiveClasses(t *testing.T) {
	pkg := parsePackage(t)
	byName := map[string]toolReg{}
	for _, tool := range pkg.tools {
		byName[tool.Name] = tool
	}

	// Verb fragments whose presence in a proposable name means the exclusion
	// above has been broken. These are the classes the card names, plus the
	// credential/email/publication ones chat_tools.go documents.
	destructive := []string{
		"delete", "archive", "revoke", "purge", "remove", "unspam",
		"refund", "capture", "release", "confirm_sale",
		"cancel", "retry",
		"password", "api_key", "passkey", "invite", "member_role", "cloudflare", "signing_key",
		"email_settings", "notifications", "reply_feedback", "send_",
		"publish", "landing_page", "menu_page", "restrictions",
		"bulk", "create_", "upsert", "use_template",
	}

	// LIFTED EXCLUSIONS — a list, not a loophole, and deliberately per-NAME.
	//
	// Card t_804040be is the one case where Wan said the exclusion above should be
	// lifted, verbatim: "Assistant boleh buat sampai masuk ke bin. tapi bukan
	// permanently delete dlm bin" / "Manusia sahaja boleh purge kekal". That is the
	// explicit say-so the failure message below demands, and the reason it is safe is
	// that the bin gives the creation an INVERSE: delete_form bins a form and
	// restore_form returns it with its original slug, while purge_form — the only
	// permanent step — stays human-only and is refused by
	// TestChatNeverProposesAPermanentDelete's predicate.
	//
	// The exemption is keyed on the NAME, so it cannot widen by accident: a fragment
	// stays in the list above and every OTHER name matching it is still refused. An
	// entry needs the card that lifted it and the reason it is reversible, or the
	// next reader cannot tell a decision from drift.
	lifted := map[string]string{
		"upsert_form": "t_804040be — Wan's C: the assistant may create a form, and the bin " +
			"(delete_form → restore_form) makes a mistaken confirm recoverable",
	}

	var bad []string
	for _, name := range ChatProposableTools() {
		if _, ok := lifted[name]; ok {
			continue
		}
		for _, frag := range destructive {
			if strings.Contains(name, frag) {
				bad = append(bad, name+" (matches "+frag+")")
				break
			}
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Errorf("the proposable set contains tool(s) from an EXCLUDED class:\n  %s\n"+
			"The phase-2 set is reversible field edits only. If this exclusion is being "+
			"deliberately lifted, the card says Wan must say so explicitly — do not lift it "+
			"here.", strings.Join(bad, "\n  "))
	}

	// Every lifted exclusion must be REVERSIBLE through the chat, or the reason
	// recorded beside it is not true. The bin is what makes upsert_form admissible,
	// and it is a claim about the registry, so it is checked against the registry
	// rather than trusted: both halves of the round trip must exist as registered
	// tools, and the permanent half must NOT be proposable.
	for name, why := range lifted {
		if _, ok := byName[name]; !ok {
			t.Errorf("lifted exclusion %q is not a REGISTERED tool — the entry is stale, and the "+
				"reason beside it (%s) cannot be checked", name, why)
			continue
		}
		_, hasDelete := byName["delete_form"]
		_, hasRestore := byName["restore_form"]
		if !hasDelete || !hasRestore {
			t.Errorf("lifted exclusion %q is justified by the bin, but delete_form/restore_form "+
				"are not both registered — the creation has no inverse and the exemption is "+
				"no longer sound", name)
		}
		if ChatToolIsProposable("purge_form") {
			t.Errorf("lifted exclusion %q is only sound while purge_form stays UNPROPOSABLE; it "+
				"is proposable now, so the chat can reach a permanent delete", name)
		}
	}

	// And the shape claim: each proposable name should exist as a registered
	// writer that some OTHER name pairs with as its inverse. We assert the weak,
	// checkable half: the set stays small.
	if len(ChatProposableTools()) > 6 {
		t.Errorf("the proposable set has grown to %d tools; phase 2 is meant to start with a "+
			"small reversible set so each addition is a decision rather than a drift",
			len(ChatProposableTools()))
	}
	_ = byName
}

// TestChatNeverProposesAPermanentDelete is Wan's hard rule, enforced by name.
//
//	"Manusia sahaja boleh purge kekal — assistant HANYA boleh hantar ke bin,
//	 tak pernah padam kekal"
//
// The workspace bin (and the form bin before it) makes destruction RECOVERABLE,
// which is exactly why a mistake in the propose/confirm lane can be tolerated at
// all: the user sends the result to the bin and restores it. That safety net only
// holds while the lane can never reach a PERMANENT delete — a purge has no
// inverse through any tool the chat can propose, so one wrong confirm would
// destroy data no restore can bring back.
//
// WHY THIS IS A SECOND TEST ON TOP OF TestChatProposableExcludesTheDestructiveClasses.
// That test checks the CURRENT proposable set against a list of verb fragments,
// so it catches a bad name TODAY. This one asserts the rule over EVERY
// registered tool, so it also fails the moment a purge tool is ADDED to the
// registry and anything makes it proposable — including a name whose verb the
// fragment list happens not to carry (e.g. `destroy_workspace`,
// `erase_workspace`, `permanent_*`). It reads the registry, not a hand-written
// list, for the same reason the scope guard does: a new tool must be covered
// automatically rather than being silently skipped.
//
// The rule is one-directional on purpose: it is fine (and expected) for the chat
// to reach a tool that moves something to the BIN, because the bin is reversible
// and that is the whole point of the lane. It is never fine to reach permanent
// destruction.
func TestChatNeverProposesAPermanentDelete(t *testing.T) {
	pkg := parsePackage(t)
	if len(pkg.tools) < 100 {
		t.Fatalf("parsed only %d registered tools — the sweep would be meaningless", len(pkg.tools))
	}

	// Permanent-destruction verbs. Deliberately NOT including "delete" or
	// "trash": those move things to the bin in this product, which is the
	// capability the chat is allowed to reach. The words below are the ones that
	// mean the data is GONE.
	permanent := []string{"purge", "destroy", "permanent", "erase", "wipe", "hard_delete"}

	// Every registered tool whose NAME carries a permanent verb: none of them may
	// be proposable, now or later.
	var offenders []string
	var permanentTools []string
	for _, tool := range pkg.tools {
		lower := strings.ToLower(tool.Name)
		isPermanent := false
		for _, frag := range permanent {
			if strings.Contains(lower, frag) {
				isPermanent = true
				break
			}
		}
		if !isPermanent {
			continue
		}
		permanentTools = append(permanentTools, tool.Name)
		if ChatToolIsProposable(tool.Name) {
			offenders = append(offenders, tool.Name)
		}
	}
	// A floor: if the sweep found no permanent tool at all, the registry parse
	// changed shape and this guard would pass vacuously. purge_unverified_accounts
	// and purge_form are both registered today, so this is honest.
	if len(permanentTools) == 0 {
		t.Fatal("the sweep found no permanent-destruction tool in the registry — the parse is " +
			"not reading the tool names, so this guard proves nothing")
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("PERMANENT-destruction tool(s) are proposable to the chat: %v\n"+
			"Wan's rule: the assistant may send things to the BIN and must NEVER delete permanently. "+
			"A purge cannot be undone by any tool the chat can propose, so a single wrong confirm "+
			"would destroy data no restore can recover. Remove it from chatProposableTools.",
			offenders)
	}

	// And the same rule stated as a predicate, so the guard is not merely a
	// property of today's list: every permanent tool must be refused by the
	// exposure check the proposal path consults TWICE (when proposed, and again
	// at confirm).
	for _, name := range permanentTools {
		if ChatToolIsProposable(name) {
			t.Errorf("ChatToolIsProposable(%q) = true — permanent destruction must never be "+
				"proposable, whatever the tool is called", name)
		}
	}

	// The bin tools ARE allowed to be proposable in principle (the card that
	// dissolves the old objection depends on it), so the guard must not be
	// satisfiable by banning every tool with "delete" in its name. Assert the
	// rule is about PURGE specifically by naming a bin-shaped tool that exists
	// today and confirming this test does not object to its existence.
	binTool := "delete_form"
	found := false
	for _, tool := range pkg.tools {
		if tool.Name == binTool {
			found = true
		}
	}
	if !found {
		t.Fatalf("%s is no longer registered — update this guard, which uses it to prove the rule "+
			"targets PERMANENT destruction and not the bin", binTool)
	}
}

// TestChatExposurePredicateAgreesWithTheSets guards the single-predicate rule.
//
// ChatToolIsAllowed is what the dispatch path consults, and its answer must be
// exactly "read tool, or one of the two in-process tools". A name that is
// proposable is NOT dispatchable — that is the phase-2 invariant — so the two
// claims are asserted together here.
func TestChatExposurePredicateAgreesWithTheSets(t *testing.T) {
	pkg := parsePackage(t)

	inProcess := map[string]bool{
		ChatSubmitFeedbackTool: true,
		ChatProposeWriteTool:   true,
	}
	read := map[string]bool{}
	for _, n := range ChatReadTools() {
		read[n] = true
	}

	if ChatToolIsAllowed(ChatProposeWriteTool) != true {
		t.Errorf("%s must be dispatchable: it records intent and executes nothing, and it is "+
			"the only route to a proposal", ChatProposeWriteTool)
	}
	if ChatToolIsAllowed(ChatSubmitFeedbackTool) != true {
		t.Errorf("%s must be dispatchable: it is the chat's in-process filing path", ChatSubmitFeedbackTool)
	}

	for _, tool := range pkg.tools {
		want := inProcess[tool.Name] || read[tool.Name]
		if got := ChatToolIsAllowed(tool.Name); got != want {
			t.Errorf("ChatToolIsAllowed(%q) = %v, want %v — the exposure predicate and the "+
				"allowlist sets have drifted apart", tool.Name, got, want)
		}
	}

	// The proposal tool is not a registered MCP tool (see chat_tools.go: a token
	// handed to an MCP caller could never be confirmed by one), so it must not
	// appear in the registered set — a registered propose_write would ship a tool
	// whose only outcome is a dead token.
	for _, tool := range pkg.tools {
		if tool.Name == ChatProposeWriteTool {
			t.Errorf("%s is registered as an MCP tool, but the confirm step is session-only: "+
				"an MCP caller could never click. Either it is a product decision to expose it "+
				"(then build the confirm story) or it should not be registered.",
				ChatProposeWriteTool)
		}
	}
}

// TestChatExposesAFractionOfTheRegistry states the scale claim honestly: the chat
// is not "the agent with a few restrictions", it is a small surface. A future edit
// that opens the allowlist to everything should have to argue with this number.
func TestChatExposesAFractionOfTheRegistry(t *testing.T) {
	pkg := parsePackage(t)
	exposed := len(ChatReadTools()) + 2 // + filing + proposing
	if len(pkg.tools) < 100 {
		t.Fatalf("parsed only %d registered tools — the comparison would be meaningless", len(pkg.tools))
	}
	// A third of the registry would already be a different product decision.
	if exposed > len(pkg.tools)/3 {
		t.Errorf("the chat exposes %d of %d registered tools; it is meant to be a small "+
			"allowlist, not most of the registry", exposed, len(pkg.tools))
	}
	t.Logf("chat exposes %d of %d registered tools (%d read + filing + proposing); "+
		"proposable: %d", exposed, len(pkg.tools), len(ChatReadTools()), len(ChatProposableTools()))
}
