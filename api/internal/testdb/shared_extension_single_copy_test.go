package testdb

// Guard for the DUPLICATED shared-extension rule (card t_00bac923).
//
// WHY THIS FILE EXISTS
//
// The rule — "the shared extensions (citext, pgcrypto) must live in `public`,
// serialized against concurrent creation, and a misplaced one gets relocated" —
// was written THREE times, independently:
//
//	1. api/internal/testdb/testdb.go   prepareExtensions / ensureExtensions
//	2. api/internal/db/extensions.go   EnsureSharedExtensions + relocateSharedExtension
//	3. (a third copy, ensureExtensions, arrived with PR #717)
//
// and the copies had drifted in exactly the ways that produced defects:
//
//   - different advisory-lock keys. testdb used
//     `pg_advisory_xact_lock(0x4A6F6D466F726D)` and db used
//     `pg_advisory_xact_lock(hashtext('jomform:shared-extensions'))`. Two locks that
//     do NOT serialize against each other on the same database-global state, so a
//     test process and a migrating application could still race
//     `CREATE EXTENSION IF NOT EXISTS` and collide on pg_extension_name_index
//     (SQLSTATE 23505) — which is the exact failure the lock was added to prevent.
//   - different extension lists (a hand-written literal in one copy vs the
//     migration-derived list the other carried behind a completeness guard).
//   - an unconditional `ALTER EXTENSION … SET SCHEMA public` in one copy, which is
//     a semantic no-op when the extension is already in `public` but NOT a
//     privilege no-op: it requires ownership, so it failed every reset on a
//     database whose extensions are owned by another role (SQLSTATE 42501).
//
// Each new copy got one of those properties right and another wrong. That is what
// the card asks to end: ONE implementation, one lock, one list, with `testdb`
// consuming it rather than restating it. testdb.ensureExtensions now calls
// db.EnsureSharedExtensions, and the interface it needs is a type ALIAS of
// db.ExtensionProvisioner rather than a second declaration.
//
// WHAT THIS GUARD PROVES, AND WHAT IT DOES NOT
//
// Proves: the production tree contains exactly one implementation of the rule —
// the provisioning statement and the database-scoped advisory lock that serializes
// it appear in api/internal/db and NOWHERE ELSE; and testdb has no rule of its own.
//
// Does NOT prove: that the one implementation behaves correctly. That is the
// behavioural guard's job (prepare_extensions_ownership_test.go drives the real
// privilege path, extensions_test.go the relocations). The two are complementary:
// this file catches a second COPY, the behavioural one catches the single copy
// being neutered.
//
// A mutation that adds a second `CREATE EXTENSION IF NOT EXISTS` or a second
// `pg_advisory_xact_lock` for this rule elsewhere in the tree compiles fine and
// goes RED here, without touching db/extensions.go.

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// sharedExtensionRuleRoot returns the api tree root.
func sharedExtensionRuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the api tree")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("api root %s does not contain go.mod: %v", root, err)
	}
	return root
}

// sharedExtensionCopy is one file that looks like it implements the rule.
type sharedExtensionCopy struct {
	rel      string // slash-separated path relative to the api tree
	creates  bool   // issues a CREATE EXTENSION for a shared extension
	advLocks int    // advisory-lock calls in this file
}

// TestTheSharedExtensionRuleIsImplementedExactlyOnce is the card's acceptance:
// one implementation, one lock, one list.
func TestTheSharedExtensionRuleIsImplementedExactlyOnce(t *testing.T) {
	root := sharedExtensionRuleRoot(t)

	// The ONE file allowed to hold the rule.
	const owner = "internal/db/extensions.go"

	// The implementation is identified by WHAT IT DOES, not by which extension
	// names it spells. db/extensions.go never writes the word "citext" in code —
	// it iterates SharedExtensions — so matching extension names would miss the
	// one real implementation and count only files that TALK about it.
	createRe := regexp.MustCompile(`(?i)\bCREATE\s+EXTENSION\b`)
	advLockRe := regexp.MustCompile(`(?i)\bpg_advisory_xact_lock\s*\(`)
	// The shared-extension list, by identifier. \b keeps this from matching
	// inside the CALL `EnsureSharedExtensions(` — calling the rule is not
	// implementing it, which is precisely what testdb now does.
	sharedListRe := regexp.MustCompile(`\b(SharedExtensions|sharedExtensions|sharedExtensionLockKey)\b`)

	var copies []sharedExtensionCopy
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil // production tree only: a test may legitimately stage a copy
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		src := string(b)
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		// CODE only, never comments. Every copy that was deleted still NAMES the
		// rule in its comments — db/migrate.go discusses `CREATE EXTENSION` at
		// length and migrate.go's own detector is a regexp matching that phrase —
		// so matching raw source would keep counting files that explain the rule
		// as files that implement it. That is the same class of false positive
		// this repo has hit before (a page echoed into a bite-proof's own log).
		code := stripGoComments(src)

		c := sharedExtensionCopy{rel: rel}
		// Creates the shared set: issues CREATE EXTENSION over the shared list.
		c.creates = createRe.MatchString(code) && sharedListRe.MatchString(code)
		c.advLocks = len(advLockRe.FindAllString(code, -1))

		// Holds the rule if it provisions the shared set, or serializes work over
		// it with an advisory lock.
		if c.creates || (c.advLocks > 0 && sharedListRe.MatchString(code)) {
			copies = append(copies, c)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk api tree: %v", err)
	}

	if len(copies) == 0 {
		t.Fatalf("no file implements the shared-extension rule — expected exactly one, %s.\n"+
			"This guard scans for a CREATE EXTENSION naming a shared extension, or an\n"+
			"advisory lock taken over them. Finding none means either the rule moved or\n"+
			"this guard stopped matching; both need a look.", owner)
	}

	var found []string
	for _, c := range copies {
		found = append(found, c.rel)
	}
	if len(copies) != 1 || copies[0].rel != owner {
		t.Fatalf("the shared-extension rule must be implemented in exactly ONE place: %s.\n"+
			"Found %d implementation(s):\n  %s\n\n"+
			"Two copies is what produced the defects this rule exists to prevent: two\n"+
			"advisory-lock keys that do not serialize against each other on the same\n"+
			"database-global state (so concurrent CREATE EXTENSION still collides on\n"+
			"pg_extension_name_index, SQLSTATE 23505), two extension lists that drift, and\n"+
			"one copy issuing an unconditional ALTER that fails without ownership\n"+
			"(SQLSTATE 42501). Delete the extra copy and call db.EnsureSharedExtensions.",
			owner, len(copies), strings.Join(found, "\n  "))
	}

	// One lock: the owner must hold exactly one advisory-lock call for this rule.
	if copies[0].advLocks != 1 {
		t.Errorf("%s holds %d advisory-lock call(s), want exactly 1.\n"+
			"A second lock key does not serialize against the first — the whole point of\n"+
			"the lock is that every writer of this database-global state waits on the\n"+
			"SAME key.", owner, copies[0].advLocks)
	}
}

// TestOnlyOneExtensionProvisionerDeclaration pins the interface half of the same
// finding: testdb declared its own `extensionProvisioner` interface beside db's
// `ExtensionProvisioner`, and it was that duplication which let the two
// implementations drift in the first place. testdb's is now a type alias.
func TestOnlyOneExtensionProvisionerDeclaration(t *testing.T) {
	root := sharedExtensionRuleRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "internal", "testdb", "testdb.go"))
	if err != nil {
		t.Fatalf("read testdb.go: %v", err)
	}
	s := string(src)

	aliasRe := regexp.MustCompile(`(?m)^type\s+extensionProvisioner\s*=\s*db\.ExtensionProvisioner\s*$`)
	if !aliasRe.MatchString(s) {
		t.Errorf("testdb no longer aliases db.ExtensionProvisioner.\n\n" +
			"It must be `type extensionProvisioner = db.ExtensionProvisioner` (an alias,\n" +
			"not a fresh declaration). A second declaration is how the two copies of the\n" +
			"rule drifted apart in the first place — see t_00bac923.")
	}

	declRe := regexp.MustCompile(`(?m)^type\s+extensionProvisioner\s+interface\s*\{`)
	if declRe.MatchString(s) {
		t.Errorf("testdb declares its own extensionProvisioner interface again.\n\n" +
			"That is the duplication t_00bac923 removed. Use the alias.")
	}

	// And ensureExtensions must delegate, not restate.
	if !strings.Contains(s, "db.EnsureSharedExtensions(ctx, admin, nil)") {
		t.Errorf("testdb.ensureExtensions no longer delegates to db.EnsureSharedExtensions.\n\n" +
			"It must be a one-line call. Anything longer means the rule has been restated\n" +
			"here, which is what the card forbids.")
	}
}

// stripGoComments removes // and /* */ comments so a comment NAMING citext does
// not make a file look like an implementation.
func stripGoComments(src string) string {
	src = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	src = regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(src, "")
	return src
}
