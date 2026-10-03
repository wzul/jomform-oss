package biteharness

// Guard for the RUN COMMAND a bite harness tells its operator to type
// (card t_5035492a).
//
// WHY THIS EXISTS. `api/scripts/biteproof_model_default_guard.py` printed its
// remedy as `python3 scripts/biteproof_model_default_guard.py`, which resolves only
// from inside `api/`. From the repo root — where every other api harness documents
// that it is run — it exits 2 on a missing file:
//
//     $ python3 scripts/biteproof_model_default_guard.py
//     python3: can't open file '.../scripts/biteproof_model_default_guard.py':
//     [Errno 2] No such file or directory
//
// That is the worst moment for it: the remedy is printed at the END of a refusal,
// to an operator who has already spent minutes, and its whole job is to hand them
// the next command. A command that cannot run is worse than no command, because it
// reads as one.
//
// The file's four siblings all document `Run:  python3 api/scripts/<name>.py` — a
// convention stated in the harnesses themselves, not invented here.
//
// WHAT IS PINNED, and why each assertion can fail:
//
//   1. Every `Run:` line in the discovered harnesses names a path that EXISTS. The
//      walk is the same one the inventory uses, so a harness added tomorrow is
//      covered without being listed here — the point of pinning a CONVENTION rather
//      than one file. A file that moved, or a `Run:` line written against the wrong
//      root, fails loudly.
//   2. The path must be REPO-RELATIVE, with a directory in it (the same rule the
//      inventory's table rows already carry): a bare `biteproof_x.py` cannot be run
//      from the root either.
//   3. VACUITY. A walk that finds no `Run:` lines proves nothing, so the count is
//      asserted to be non-trivial. This is the failure mode that would otherwise let
//      the guard pass forever after a comment reflow dropped every `Run:` marker.
//
// SCOPE, stated so it is not over-claimed: this proves the documented COMMAND
// resolves from the repo root. It does not execute the harness, and it cannot see a
// command the harness PRINTS at runtime (the original defect was one of those) — it
// pins the convention that the printed one broke, and the printed one was fixed and
// is asserted by its own case in the inventory's sibling checks.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// runLineRe matches a documented run command line, capturing the script path.
// `Run:  python3 api/scripts/biteproof_x.py` — the interpreter is optional so a
// shell harness documenting `Run:  bash web/scripts/biteproof_y.sh` is covered too.
var runLineRe = regexp.MustCompile(`^\s*#?\s*Run:\s+(?:[A-Za-z0-9_.-]+\s+)?(\S+\.(?:py|sh))\s*$`)

func TestDocumentedRunCommandsResolveFromTheRepoRoot(t *testing.T) {
	root := repoRoot(t)
	harnesses, filesSeen := discoverHarnesses(t, root)
	if filesSeen == 0 {
		t.Fatalf("the harness walk saw no files at all, so this guard would pass vacuously")
	}
	if len(harnesses) == 0 {
		t.Fatalf("the harness walk found no harnesses; the naming convention or the walk is broken")
	}

	var checked int
	var broken []string
	for _, rel := range harnesses {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			m := runLineRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			checked++
			p := m[1]
			if !strings.Contains(p, "/") {
				broken = append(broken, rel+":"+strconv.Itoa(i+1)+": "+p+
					" is not a repo-relative path (a bare filename cannot be run from the root)")
				continue
			}
			if _, serr := os.Stat(filepath.Join(root, filepath.FromSlash(p))); serr != nil {
				broken = append(broken, rel+":"+strconv.Itoa(i+1)+": "+p+
					" does not exist from the repo root")
			}
		}
	}

	// 3. Vacuity, and it is a hard failure rather than a skip: if a comment reflow
	//    silently drops every `Run:` marker, this guard would otherwise report green
	//    while checking nothing — the same shape as the defect it exists for.
	if checked == 0 {
		t.Fatalf("no `Run:` line was found in any of the %d discovered harnesses — either the "+
			"convention was removed everywhere or this pattern no longer matches it, and in both "+
			"cases this guard is now decorative", len(harnesses))
	}
	if len(broken) > 0 {
		t.Errorf("a harness documents a run command that does not resolve from the repo root, so "+
			"the operator who follows it gets a missing-file error instead of the harness:\n  %s",
			strings.Join(broken, "\n  "))
	}
}
