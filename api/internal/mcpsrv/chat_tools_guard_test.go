package mcpsrv

// The in-app chat's tool-exposure guard (phase 1).
//
// WHAT THIS PROVES, and what it deliberately does not:
//
//	PROVES  every name in the chat's READ allowlist is a REGISTERED tool, and
//	        that the tool it names reaches no store write — i.e. the list cannot
//	        have been widened into a writer by a later edit.
//	PROVES  the chat's one write (submit_feedback) is registered, IS gated by the
//	        write scope, and is NOT in the read list (so "the chat can read" and
//	        "the chat can file feedback" stay two separate claims).
//	DOES NOT prove the list is well-chosen. A predicate cannot tell whether
//	        list_api_keys belongs in front of a model — that is a judgement, made
//	        in mcpsrv/chat_tools.go and reviewable there.
//
// It reuses the call graph from scope_gate_guard_test.go (same package), so the
// chat's exposure is measured by the SAME parse that polices the 161 registered
// tools' authorization — not by a second, weaker reader.

import (
	"sort"
	"strings"
	"testing"
)

// TestChatReadAllowlistContainsNoWriter is the guard on the chat's read list.
//
// A read tool that can write is the phase-1 failure mode: the chat is supposed
// to be incapable of writing at all, and the allowlist is the layer that decides
// what it may even attempt. The identity layer (readonly scopes) is the second
// net, but a guard that only relied on it would let the allowlist quietly become
// "everything", at which point the refusal a user sees is an error message
// rather than the absence of the capability.
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
			"Phase 1 must be incapable of writing. Either move the tool out of the list or — if it "+
			"is genuinely a read — fix the write-sink classification, but do not leave a writer "+
			"reachable by the chat.", len(writers), strings.Join(writers, "\n  "))
	}
	t.Logf("chat read allowlist: %d tools, all registered, none reaches a store write", len(read))
}

// TestChatSubmitFeedbackIsRegisteredAndScopeGated pins the ONE write.
//
// Two properties, both required:
//
//	registered  — AGENTS.md parity: the dashboard capability has an MCP tool, and
//	              the parity guard reads the registered set.
//	scope-gated — filing is a write, so a readonly credential must be refused.
//	              This is the same gate issue #645 restored; a tool that files
//	              feedback without it would let a readonly key flood the queue.
//
// And it must NOT be in the read list: the chat's "read-only apart from one
// write" claim only means something if the two sets are disjoint.
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
		t.Fatalf("%s is not registered — the in-app chat's one write would exist on the "+
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
			t.Errorf("%s appears in the READ allowlist as well as being the one write", ChatSubmitFeedbackTool)
		}
	}
	// Nothing else may be allowed: ChatToolIsAllowed is what the dispatch path
	// consults, so it must agree with the two sets above.
	for _, tool := range pkg.tools {
		want := tool.Name == ChatSubmitFeedbackTool
		for _, n := range ChatReadTools() {
			if n == tool.Name {
				want = true
			}
		}
		if got := ChatToolIsAllowed(tool.Name); got != want {
			t.Errorf("ChatToolIsAllowed(%q) = %v, want %v — the exposure predicate and the "+
				"allowlist sets have drifted apart", tool.Name, got, want)
		}
	}
}

// TestChatExposesAFractionOfTheRegistry states the scale claim honestly: the
// chat is not "the agent with a few restrictions", it is a small surface. A
// future edit that opens the allowlist to everything should have to argue with
// this number.
func TestChatExposesAFractionOfTheRegistry(t *testing.T) {
	pkg := parsePackage(t)
	exposed := len(ChatReadTools()) + 1 // + the one write
	if len(pkg.tools) < 100 {
		t.Fatalf("parsed only %d registered tools — the comparison would be meaningless", len(pkg.tools))
	}
	// A third of the registry would already be a different product decision.
	if exposed > len(pkg.tools)/3 {
		t.Errorf("the chat exposes %d of %d registered tools; phase 1 is meant to be a small "+
			"allowlist, not most of the registry", exposed, len(pkg.tools))
	}
	t.Logf("chat exposes %d of %d registered tools (%d read + 1 write)",
		exposed, len(pkg.tools), len(ChatReadTools()))
}
