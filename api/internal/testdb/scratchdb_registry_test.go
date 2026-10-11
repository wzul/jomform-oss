// Package testdb — guard that the throwaway-database FAMILY set is derived, not
// remembered (scratchdb.go).
//
// WHY THIS FILE EXISTS (card t_46165e15, requirement 2)
//
// The prune's family list is the difference between reclaiming a leak and
// reclaiming a fraction of it. The card measured 273 leaked databases in SIX
// families; a rule that only knew `jomform_migtest_` would have left ~245 of
// them and ~4 GB behind. A hand-written list is a second source of truth, and
// second sources of truth drift — the same way `db.SharedExtensions` drifted
// before it was derived from the migration set.
//
// This guard is what stops the drift here, in BOTH directions:
//
//   - a file that creates a database under a prefix nobody registered is a leak
//     the prune cannot see (the guard fails, naming the file and the prefix);
//   - a registered prefix no create site uses any more is a rule that reclaims
//     a family nothing produces (the guard fails, so the entry has to be
//     removed deliberately).
//
// It found two creators that the card's own list did not have — the
// `jomform_tdbguard_` fixtures in extensions_integration_test.go and the
// `jomform_testdb_own_` ones in prepare_extensions_ownership_test.go — which is
// the point: the set is read from the tree, not from anyone's memory of it.
package testdb

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// databaseCreatorRE identifies a file that CREATES a database. It is written
// with `\s+` between the keywords on purpose: a guard's own source must not
// match the pattern it looks for, or this file would be counted as a creator.
var databaseCreatorRE = regexp.MustCompile(`(?i)\bCREATE\s+DATABASE\b`)

// prefixDeclRE finds a declared prefix literal, both as `const x = "…"` and as
// a member of a const block (`\tx = "…"`, which is how
// prepare_extensions_ownership_test.go declares its two).
var prefixDeclRE = regexp.MustCompile(`(?m)^\s*(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)

// inlinePrefixRE finds a prefix literal used INLINE to build the name, i.e.
// `"jomform_tdbguard_" + …`. extensions_integration_test.go names its throwaway
// databases exactly this way, so a guard that only read const declarations
// would miss a real family — measured, not hypothetical.
var inlinePrefixRE = regexp.MustCompile(`"([A-Za-z][A-Za-z0-9_]*_)"\s*\+`)

// notDatabasePrefixes lists prefix-shaped literals that sit in a file which
// creates databases but name something ELSE. The guard cannot tell a
// database-name family from a role-name tail or a schema-name prefix by shape
// alone — all of them end in `_` and are concatenated — so the ones that are not
// families are named here, with what they are instead.
//
// The entry cannot rot silently: the guard fails if one of these literals
// disappears, and it fails if a literal is both exempted here AND registered as
// a database family.
var notDatabasePrefixes = map[string]string{
	"jf_testdb_own_":  "internal/testdb/prepare_extensions_ownership_test.go — the throwaway ROLE prefix (the databases there are named by ownedDBPrefix)",
	"in_":             "internal/testdb/prepare_extensions_ownership_test.go — a ROLE-name tail (`rolePrefix + \"in_\" + suffix`)",
	"ru_":             "internal/testdb/prepare_extensions_ownership_test.go — a ROLE-name tail (`rolePrefix + \"ru_\" + suffix`)",
	"test_misplaced_": "internal/testdb/prepare_extensions_ownership_test.go — a scratch SCHEMA name (the misplaced-extension fixture), not a database",
}

// databaseCreatorPrefixes returns, per file that creates a database, the
// prefix-shaped literals in its CODE (comments stripped) that could be naming
// that database.
func databaseCreatorPrefixes(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		code := stripGoComments(string(b))
		if !databaseCreatorRE.MatchString(code) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)

		seen := map[string]bool{}
		add := func(lit string) {
			// Only prefix-shaped literals are collected: a name builder
			// concatenates `<literal> + <tail>`, so the literal ends in `_`.
			// The declaration scan also collects a literal that does not end
			// in `_` when its identifier says "prefix" (a family spelled
			// without the trailing underscore), so this filter must not
			// re-reject what that scan deliberately kept — the caller decides.
			if lit == "" || seen[lit] {
				return
			}
			seen[lit] = true
			out[rel] = append(out[rel], lit)
		}
		for _, m := range prefixDeclRE.FindAllStringSubmatch(code, -1) {
			ident, lit := m[1], m[2]
			if strings.HasSuffix(lit, "_") || strings.Contains(strings.ToLower(ident), "prefix") {
				add(lit)
			}
		}
		for _, m := range inlinePrefixRE.FindAllStringSubmatch(code, -1) {
			add(m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the api tree for database creators: %v", err)
	}
	return out
}

// measuredLeakFamilies are the families the card measured on the shared test
// host (2026-09-25), with the count each one held. They are pinned here so the
// family set cannot be SHRUNK silently: deleting one of these entries as
// "stale" would leave hundreds of databases the prune never reclaims, and
// nothing else in the build would say so.
var measuredLeakFamilies = []struct {
	prefix string
	count  string
}{
	{"jf_", "123"},
	{"jomform_qa", "49"},
	{"jomform_test", "20"},
	{"jomform_migtest_", "28"},
	{"repro_", "16"},
	{"tcf_", "16"},
}

// TestTheMeasuredLeakFamiliesAreAllRegistered pins the SET, not just its
// consistency with the tree: the in-tree families are enforced by the guard
// above, but every legacy family has no in-tree creator and would therefore be
// invisible if someone removed it.
func TestTheMeasuredLeakFamiliesAreAllRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, f := range scratchDBFamilies {
		registered[f.prefix] = true
	}
	for _, m := range measuredLeakFamilies {
		if !registered[m.prefix] {
			t.Errorf("the measured leak family %q (%s databases on the shared test host) is no longer "+
				"registered in scratchDBFamilies — the prune would leave that whole family behind, which is "+
				"the failure this card exists to fix. If a family is genuinely obsolete, remove it from "+
				"measuredLeakFamilies in the same change and say why.", m.prefix, m.count)
		}
	}
}

// TestEveryDatabaseCreatorPrefixIsRegistered is the derivation guard described
// in the file header. Every direction below is a way the family set can stop
// matching the tree.
func TestEveryDatabaseCreatorPrefixIsRegistered(t *testing.T) {
	root := sharedExtensionRuleRoot(t)
	creators := databaseCreatorPrefixes(t, root)

	// A guard that silently stopped matching would pass everything below, so
	// the detection itself is asserted first.
	if len(creators) == 0 {
		t.Fatalf("no file in %s issues a database-creating statement — the suites that build throwaway "+
			"databases are still there (migrations_fresh_db_test.go creates one), so this guard's detection "+
			"has broken; fix it rather than deleting the guard", root)
	}

	registered := map[string]string{} // prefix -> createdBy
	for _, f := range scratchDBFamilies {
		if prev, dup := registered[f.prefix]; dup {
			t.Errorf("scratchDBFamilies lists %q twice (%s and %s)", f.prefix, prev, f.createdBy)
		}
		registered[f.prefix] = f.createdBy
	}

	// Direction 1: every prefix a create site uses must be registered (or
	// explicitly exempted as naming something else), or the prune leaks it.
	for file, prefixes := range creators {
		for _, p := range prefixes {
			if reason, exempt := notDatabasePrefixes[p]; exempt {
				t.Logf("%s: %q is exempted (%s)", file, p, reason)
				continue
			}
			if _, ok := registered[p]; !ok {
				t.Errorf("%s creates databases under %q, which is NOT in scratchDBFamilies — the prune in "+
					"scratchdb.go would never reclaim one of them, so every killed run leaks that database "+
					"forever.\n\nRegister it in scratchDBFamilies (with createdBy: %q), or — if the literal "+
					"names something other than a throwaway DATABASE — add it to notDatabasePrefixes with what "+
					"it names instead.", file, p, file)
			}
		}
	}

	// Direction 2: every in-tree family must still have the create site it
	// names, so the registry cannot keep reclaiming a family nothing produces.
	for _, f := range scratchDBFamilies {
		if f.createdBy == "" {
			continue
		}
		if _, ok := creators[f.createdBy]; !ok {
			t.Errorf("scratchDBFamilies claims %q is created by %s, but that file issues no "+
				"database-creating statement any more — either the fixture moved (update createdBy) or it is "+
				"gone (remove the entry, so the prune stops reclaiming a family nothing produces)",
				f.prefix, f.createdBy)
			continue
		}
		found := false
		for _, p := range creators[f.createdBy] {
			if p == f.prefix {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is registered as the creator of %q, but no such prefix literal is declared there. "+
				"Every throwaway-database creator names its databases with a constant whose literal ends in "+
				"`_` (or with an identifier mentioning \"prefix\"), or inline as `\"<family>\" + …`; use one "+
				"of those shapes so this guard can see it.", f.createdBy, f.prefix)
		}
	}

	// Direction 3: a family with no in-tree creator must not have one now. If a
	// fixture starts creating, say, `jf_` databases, the entry has to be
	// promoted to a repo-owned one so the pid convention is checked there too.
	for _, f := range scratchDBFamilies {
		if f.createdBy != "" {
			continue
		}
		for file, prefixes := range creators {
			for _, p := range prefixes {
				if p == f.prefix {
					t.Errorf("%s creates databases under %q, which scratchDBFamilies still lists as having NO "+
						"in-tree creator — set createdBy to that file, so this family gets the same "+
						"pid-naming and ownership checks as the others", file, f.prefix)
				}
			}
		}
	}

	// Direction 4: an in-tree creator must attach its OWN pid to the name. That
	// is what lets the prune attribute a leftover database to a process instead
	// of guessing from the clock (scratchdb.go), and what keeps it from
	// reclaiming a database whose run is still going.
	for _, f := range scratchDBFamilies {
		if f.createdBy == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.createdBy)))
		if err != nil {
			t.Errorf("read %s (createdBy of %q): %v", f.createdBy, f.prefix, err)
			continue
		}
		code := stripGoComments(string(b))
		if !strings.Contains(code, "strconv.Itoa(os.Getpid())") {
			t.Errorf("%s creates %q databases without attaching this process's pid to the name "+
				"(want `… + \"_p\" + strconv.Itoa(os.Getpid())`).\n\n"+
				"Without the pid the prune cannot tell a database a live run is about to use from an "+
				"abandoned one, and has to fall back to the %s grace period — which delays every reclaim and "+
				"leaves the live-run arm with nothing to read.", f.createdBy, f.prefix, legacyDatabaseGrace)
		}
	}

	// Direction 5: the exemptions cannot rot either — one that no longer exists
	// is a stale claim about the tree.
	for lit, reason := range notDatabasePrefixes {
		found := false
		for _, prefixes := range creators {
			for _, p := range prefixes {
				if p == lit {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("notDatabasePrefixes exempts %q (%s), but no database-creating file declares that "+
				"literal any more — remove the exemption", lit, reason)
		}
		if _, isFamily := registered[lit]; isFamily {
			t.Errorf("%q is BOTH exempted (%s) and registered as a database family — pick one", lit, reason)
		}
	}
}
