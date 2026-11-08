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

The guard applies only where gates RUN. `--gate-deps`, `--classify` and `--explain-diff`
run no gate and are driven BY the gates (`selftest` asserts the table through them), so a
scoped run's exported list must not make those invocations die — it did once, and the
`selftest` gate of a legitimate scoped run went red with "the path-aware table cannot be
inspected". The train also unsets the list before any non-scoped call, so a bisect prefix
cannot inherit a previous prefix's narrowing.

## What it catches

regressions **and data races**. The `-timeout` is an explicit per-package ceiling (30m, overridable with `API_TEST_TIMEOUT`): without it every package inherits Go's 10m default and `internal/httpapi` — the largest package, whose cost tracks host load — is reported as `panic: test timed out after 10m0s` for a package that is green (measured: pristine main rc=1 at 601.195s, green rc=0 at 620.593s on the same host). A `panic: test timed out` red cannot be told from a hung test. The `selftest` gate asserts this budget is present, that `API_TEST_TIMEOUT` reaches the argv, and that a stripped copy goes RED
