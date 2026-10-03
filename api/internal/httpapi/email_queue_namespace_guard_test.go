package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/emailqueue"
	"github.com/wzul/jomform/api/internal/testdb"
)

// deadQueueFixtures are the helpers allowed to build a PRODUCTION queue with
// `emailqueue.NewQueue`, keyed by function name, each WITH its reason.
//
// An exemption is a deliberate, named decision; a new one has to be added on
// purpose. Silence is what this guard exists to remove.
var deadQueueFixtures = map[string]string{
	"deadQueue": "the DELIBERATELY-BROKEN queue fixture: it dials a Valkey that must refuse " +
		"(the helper asserts PING fails before anything relies on it), so it never writes a key " +
		"anywhere and cannot collide with a peer. It proves the failure path answers without a " +
		"live queue, which is not the per-test-isolation question this guard is about.",
	"TestFeedbackNotifyQueueKeysAreOwnedByThisProcess": "the preFix POSITIVE CONTROL in the " +
		"ownership guard: emailqueue.NewQueue(nil) is built PRECISELY because it produces the " +
		"production key, and the test requires the shape check to REJECT it. The client is nil, so " +
		"it writes nothing anywhere. Removing it would delete the control that proves the " +
		"ownership rule refuses a production queue at all.",
}

// NOTHING ASSERTED THAT THE EMAIL-QUEUE FIXTURES BUILD ON A PER-TEST NAMESPACE
// (card t_2082a9fe). PR 775 rewrote two fixtures to build their queue on
// `testdb.Valkey(t)`'s per-PID prefix, and two guards came close to pinning that
// without pinning it:
//
//   - `liveQueue`'s refusal is RUNTIME, inside one helper: it fires only for the
//     queue built THROUGH that helper, and only if it is reached.
//   - `emailqueue`'s own keys-production contract test pins the production
//     defaults and that a custom prefix REPLACES them, but says nothing about
//     whether a fixture in ANOTHER package passes a prefix at all.
//
// So a third file (or a re-added helper in either of the two) could rebuild the
// identical defect — read `JOMFORM_TEST_VALKEY_URL`, strip the index, `FlushDB`,
// `NewQueue` — and compile and pass green. That is the shape the pre-fix tree
// shipped, and the shape that made 10 of the file's own 11 queue guards answer
// `retry = 401, want 200` with the env pointed at /14.
//
// This guard is STATIC: it parses the package's test files and refuses a
// production-queue construction outside the exempt helpers. It runs in every
// `go test ./internal/httpapi/`, including with no Valkey configured — a guard
// that skips is a guard that stops guarding.
func TestEmailQueueFixtureHelpersBuildOnATestNamespace(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) < 10 {
		t.Fatalf("only %d test files found — the walk is looking in the wrong place, so this guard "+
			"would pass by seeing nothing", len(files))
	}

	var violations []string
	prefixed := 0
	exemptSeen := map[string]bool{}
	for _, name := range files {
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "emailqueue" {
					return true
				}
				switch sel.Sel.Name {
				case "NewQueueWithPrefix":
					prefixed++
				case "NewQueue":
					if _, exempt := deadQueueFixtures[fn.Name.Name]; exempt {
						exemptSeen[fn.Name.Name] = true
						return true
					}
					pos := fset.Position(call.Pos())
					violations = append(violations,
						name+":"+strconv.Itoa(pos.Line)+" in "+fn.Name.Name)
				}
				return true
			})
		}
	}

	// Both premises, stated so this guard cannot go vacuous silently.
	if prefixed == 0 {
		t.Fatalf("no emailqueue.NewQueueWithPrefix( in this package's test files — the fixtures no " +
			"longer build their queue on a test namespace, so the refusal below cannot tell whether " +
			"that is deliberate or an oversight. Fix the guard (or the fixtures), do not delete it: " +
			"the per-test namespace is what keeps one run's flush off another's job and off the " +
			"production keys")
	}
	for name := range deadQueueFixtures {
		if !exemptSeen[name] {
			t.Errorf("deadQueueFixtures lists %q, but no function of that name builds a production "+
				"queue any more — a stale exemption reads as permission that does not exist. Remove "+
				"the entry (or the reason it was granted is gone)", name)
		}
	}

	if len(violations) > 0 {
		t.Errorf("%d email-queue construction(s) use emailqueue.NewQueue, which builds the PRODUCTION "+
			"keys (%q):\n  %s\n\nA fixture that reads the production keys shares them with every peer "+
			"process and with any live worker on the same Valkey — the defect PR 775 fixed, where the "+
			"queue's key NAMES were the only isolation axis. Build it on testdb.Valkey(t)'s prefix "+
			"with emailqueue.NewQueueWithPrefix(rdb, prefix). A deliberately-broken fixture may be "+
			"exempted in deadQueueFixtures, WITH its reason (card t_2082a9fe).",
			len(violations), emailqueue.DefaultKeyPrefix, strings.Join(violations, "\n  "))
	}
}

// TestEmailQueueFixturePrefixIsNotTheProductionOne pins the OTHER half: the
// namespace a fixture is handed must be derived from the test process, not a
// literal that merely looks like a prefix.
func TestEmailQueueFixturePrefixIsNotTheProductionOne(t *testing.T) {
	for _, p := range []string{testdb.Prefix(), testdb.PrefixFor(t)} {
		if p == emailqueue.DefaultKeyPrefix {
			t.Fatalf("testdb gave the production prefix %q — every fixture built on it would be "+
				"writing the production keys", emailqueue.DefaultKeyPrefix)
		}
		if !strings.HasPrefix(p, "jftest:") {
			t.Fatalf("testdb prefix %q does not start with jftest: — it is not a per-process test "+
				"namespace", p)
		}
		q := emailqueue.NewQueueWithPrefix(nil, p)
		if !strings.HasPrefix(q.ReadyKey(), "jftest:") {
			t.Fatalf("a queue built on the test prefix %q has ready key %q, which is not under "+
				"jftest:", p, q.ReadyKey())
		}
	}
}
