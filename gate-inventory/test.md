# `test` — api gate detail

Command (what the table row runs): `cd api && go test ./... -race -count=1 -timeout 30m`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## Scoped runs (the merge train)

`deploy/merge-train.sh` gates a batch of PRs once, on the merged candidate. A batch that
moves `api/` is gated at the **affected package set** — the changed packages plus every
package whose build or *test* imports them, transitively — instead of `./...`, and it hands
that list over in `PREFLIGHT_TEST_PACKAGES`. An api-touching PR therefore no longer pays
the whole module's suite at merge time; the FULL inventory is still owed once per RELEASE
(`deploy/build-and-ship.sh` runs it on the bytes that ship).

The gate keeps the honesty checks itself, so a scoped run can never be mistaken for a full
one:

* `PREFLIGHT_TEST_PACKAGES` on a run with **no narrowing flag** is refused (exit 1)
  before any gate runs — the release gate's shape (a default run) must never be narrowed
  by an inherited variable;
* every token must be a package path (a leading `-` would become a `go test` flag);
* the record is stamped `test=scoped` in `PREFLIGHT_KEY_EXTRA`, so a scoped pass is not
  reusable as evidence for a full one — the same mechanism `--quick` uses for `-race`;
* the gate label names the package list that actually ran.

Anything the package graph cannot answer for (a migration `.sql`, `go.mod`, a `testdata`
fixture, a tree `go list` cannot enumerate) makes the train run the full inventory instead.

## Data-only diffs (the readers set)

A diff with **no `api/` path** still has `test` as a real dependency: api `*_test.go`
files walk `web/src` (`webWorkspacePurgeCallers` in `internal/mcpsrv`,
`passkey_seam_test.go` in `internal/httpapi`) and the model-id guard in
`internal/llm` walks the **whole repo**. Dropping the gate would turn a real red
green, so it runs — but `select_test_packages` has no api file to map, and without a
list the gate falls back to `go test ./...` (measured 551 s, and 710 s on a later
write of the same tree, for a 20-screenshot guide).

So the train asks `./scripts/preflight.sh --test-readers <base>` which PACKAGES own
those readers. The set is **derived from the same read table the gate selection
uses** — a `*_test.go` whose derived read claims a changed path, plus any `*_test.go`
that resolves the repo root (a whole-tree reader) — never a hand-written list, so a
new guard appears in it the day it is added. `--test-readers` exits 1 with the reason,
and the train then passes **no list at all**, for every case it cannot prove: an
`api/` path (changed + importers is a different question), a non-test reader (its
importers are unknown), a reader at the module root (that package path cannot be
handed to `go test`), a diff whose gate selection is the FULL inventory (one gate
cannot be narrower than the run it sits in), or a tree `go list` cannot enumerate.
The verdict names the scope (`readers-scoped: the N/M api package(s) whose tests READ
the changed data — NOT full coverage; NEVER ran: go test ./... for the other M-N
package(s)`), and preflight prints its own scoped banner so the run itself cannot be
read as `./...`.

The derivation is deliberately CONSERVATIVE in one direction: a test file that
resolves the repo root (`filepath.Join(filepath.Dir(thisFile), "..", "..", "..")`)
reads one path we cannot see, so its package is included even when the path it
ultimately opens is a specific file. The set is therefore larger than the true reader
set — the expensive packages dominate either way — but it can never be smaller, which
is the direction that would report a red gate green.

Measured on the surau guide diff plus this branch's own files (no `api/` path, this
host): `--test-readers` returns 10 of 51 packages. With that list handed to the gate
(`--only test --quick`, i.e. without `-race`), a failing test added inside
`internal/version` — a derived reader — turns the run RED (`--- FAIL:
TestBiteProofReaderRuns`); the same failing test moved to `internal/currency` — a
package whose tests read none of the changed data — is not in the gate's argv at all
(`grep -c internal/currency` = 0) and the gate stays GREEN, printing "SCOPED to 10
package(s)" and naming what was never run. The 10-package run took 105 s, of which
`internal/httpapi` alone is 99 s. The whole module for the same tree is the cost
this avoids (551 s, and 710 s on a later write of the same tree, with `-race`).

The guard applies only where gates RUN. `--gate-deps`, `--classify` and `--explain-diff`
run no gate and are driven BY the gates (`selftest` asserts the table through them), so a
scoped run's exported list must not make those invocations die — it did once, and the
`selftest` gate of a legitimate scoped run went red with "the path-aware table cannot be
inspected". The train also unsets the list before any non-scoped call, so a bisect prefix
cannot inherit a previous prefix's narrowing.

## What it catches

regressions **and data races**. The `-timeout` is an explicit per-package ceiling (30m, overridable with `API_TEST_TIMEOUT`): without it every package inherits Go's 10m default and `internal/httpapi` — the largest package, whose cost tracks host load — is reported as `panic: test timed out after 10m0s` for a package that is green (measured: pristine main rc=1 at 601.195s, green rc=0 at 620.593s on the same host). A `panic: test timed out` red cannot be told from a hung test. The `selftest` gate asserts this budget is present, that `API_TEST_TIMEOUT` reaches the argv, and that a stripped copy goes RED
