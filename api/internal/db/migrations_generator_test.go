// Package db — pins the repo's own migration generator to the naming guard
// (t_77c9a06d).
//
// `make migrate-new NAME=...` (Makefile:38) is the workflow AGENTS.md tells a
// dev to use, and migrationFileRE (migrations_fresh_db_test.go) is what that
// dev's next `go test` runs. Nothing asserted the two agreed, and they did not:
// golang-migrate's createCmd writes
//
//	basename := fmt.Sprintf("%s_%s.%s%s", version, name, direction, ext)
//	internal/cli/commands.go:122 (upstream master, read 2026-10-14)
//
// i.e. NAME is echoed VERBATIM into the filename — the CLI does no slugifying —
// while the guard's slug class was [a-z0-9_]+, which has no hyphen. So
// `make migrate-new NAME=add-column` produced 000071_add-column.up.sql and the
// guard reddened on a correctly-numbered migration, with a message that blamed
// the number (reproduced on this tree with a planted 000900_add-column pair
// before the fix). The guard's class now accepts '-' too, and this test is what
// keeps the pair in step.

package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// migrationNames returns the file names currently sitting in dir.
func migrationNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names
}

// TestMigrateNewOutputSatisfiesMigrationFileRE drives the REAL `make
// migrate-new` target with a `migrate` shim first on PATH, so it sees exactly
// the argv the golang-migrate CLI would receive for a hyphenated NAME, and
// checks that what comes back out can only name files migrationFileRE accepts.
//
// It fails if the guard stops accepting the slug the generator passes through,
// if the target stops asking for a sequential (six-digit) version, or if the
// target writes into a directory other than the one the guard reads — the three
// ways the generator and the guard can disagree. The shim means no real
// migration file is ever created; any file that does appear is removed before
// the test returns, because a stray *.up.sql in this directory is applied by
// the API on its next start.
//
// The six-digit prefix in the synthesised filename below stands in for the
// version `migrate create` prepends: `-seq` plus golang-migrate's default
// `-digits 6` (internal/cli/main.go:161). The test asserts that -seq is passed,
// so the prefix's shape is the generator's contract, not an assumption about a
// filename the shim cannot see.
func TestMigrateNewOutputSatisfiesMigrationFileRE(t *testing.T) {
	guardDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// api/internal/db -> repo root.
	repoRoot, err := filepath.Abs(filepath.Join(guardDir, "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root from %s: %v", guardDir, err)
	}
	makefile := filepath.Join(repoRoot, "Makefile")
	if _, err := os.Stat(makefile); err != nil {
		t.Fatalf("expected the repo Makefile at %s (this guard reads migrations from %s): %v", makefile, guardDir, err)
	}

	before := migrationNames(t, guardDir)

	shimDir := t.TempDir()
	argvPath := filepath.Join(shimDir, "argv")
	shim := filepath.Join(shimDir, "migrate")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+argvPath+"\"\n"), 0o755); err != nil {
		t.Fatalf("write the migrate shim: %v", err)
	}

	// A hyphen is the case the guard rejected, and the shape a dev writes first
	// when they name a migration in prose.
	const name = "add-column"

	cmd := exec.Command("make", "-f", makefile, "migrate-new", "NAME="+name)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()

	// Whatever happened, do not leave a planted migration behind.
	for f := range migrationNames(t, guardDir) {
		if before[f] {
			continue
		}
		if rmErr := os.Remove(filepath.Join(guardDir, f)); rmErr != nil {
			t.Errorf("remove %s created by `make migrate-new`: %v", f, rmErr)
			continue
		}
		t.Logf("removed %s — the shim was bypassed and a real `migrate` wrote a file", f)
	}

	if err != nil {
		t.Fatalf("`make migrate-new NAME=%s` failed: %v\nmake output:\n%s", name, err, out)
	}

	raw, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("the `migrate` shim never ran, so `make migrate-new` did not reach a `migrate` on PATH "+
			"(shim dir %s): %v\nmake output:\n%s", shimDir, err, out)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	valueOf := func(flag string) (string, bool) {
		for i, a := range args {
			if a == flag && i+1 < len(args) {
				return args[i+1], true
			}
		}
		return "", false
	}

	// The guard requires a six-digit number; only -seq produces one (without it
	// `migrate create` writes a 14-digit timestamp that migrationFileRE then
	// rejects).
	if _, ok := valueOf("-seq"); !ok {
		t.Fatalf("`make migrate-new` no longer passes -seq (argv: %q) — without it `migrate create` names the file "+
			"with a timestamp, which migrationFileRE rejects", args)
	}
	ext, ok := valueOf("-ext")
	if !ok || ext != "sql" {
		t.Fatalf("`make migrate-new` must pass -ext sql (got %q, argv: %q) — the guard only sees *.up.sql / "+
			"*.down.sql", ext, args)
	}
	gotName, ok := valueOf("-seq")
	if !ok {
		t.Fatalf("no migration name after -seq (argv: %q)", args)
	}
	if gotName != name {
		t.Errorf("`make migrate-new NAME=%s` handed `migrate` the name %q — the generator must pass the slug "+
			"through unchanged (or sanitize it), not the two disagree silently", name, gotName)
	}

	dirArg, ok := valueOf("-dir")
	if !ok {
		t.Fatalf("`make migrate-new` no longer passes -dir (argv: %q)", args)
	}
	// The target cd's into the Makefile's API_DIR, so -dir is relative to api/.
	// The guard reads <this package>/migrations.
	gotDir := filepath.Clean(filepath.Join(repoRoot, "api", dirArg))
	guardMigrations := filepath.Join(guardDir, "migrations")
	if gotDir != guardMigrations {
		t.Errorf("`make migrate-new` writes into %s but this guard reads %s — the generator must write where the "+
			"guard looks", gotDir, guardMigrations)
	}

	for _, direction := range []string{"up", "down"} {
		generated := "000071_" + gotName + "." + direction + "." + ext
		if !migrationFileRE.MatchString(generated) {
			t.Errorf("`make migrate-new NAME=%s` produces %s, which migrationFileRE (%s) rejects — the repo's own "+
				"generator and its naming guard must agree; widen the guard or sanitize the slug in the Makefile",
				name, generated, migrationFileRE.String())
		}
	}
}
