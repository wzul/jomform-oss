# `selftest` — selfcheck gate detail

Command (what the table row runs): `scripts/preflight_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the gate runner's OWN safety properties: an all-skipped run is refused rather than reported green, a missing prerequisite fails instead of skipping, this inventory cannot drift from the gates that can actually run, and the path-aware selection (`--for-diff`) is correct — its table covers every gate, a web-only diff really does skip the api gates and a single `api/` path brings them back, an unclaimed path widens to the full set, a gate marked "cannot narrow" always runs, and the four gate-INPUT prose files (`MCP-PARITY.md`, `GATE-INVENTORY.md`, `AGENTS.md`, `deploy/backup/README.md`) widen the run to the gate that reads them

It also pins the harness cache (check 49), because that cache is how this gate is skipped without running: a gate whose inputs are a fixed file set is keyed on their bytes, a re-run happens when one changes, and a **gate the runner itself classifies `*`** (reads the whole checkout, cannot narrow — `selftest` is one) must declare the `:tree:` input instead, so its key covers the tracked tree and the lookup is refused on a dirty or non-git tree. The arms are driven, not read: a fixture whose scripts stay byte-identical while only `web/` changes must MISS and re-run, and a copy whose key drops the tree material must serve the stale hit — measured on the tree that shipped the scripts-only key, that stale hit was the defect.

It also pins what a RED gate leaves behind (card `t_e2a3cf23`): a failed run must
keep its output, the record's `log` must name a file that exists and carries the
failing line, `log_tail` must hold that output's last lines inline — and a copy of
the runner with the preserve step removed must come back with `log: ""`, so the
assertion cannot stay green on a runner that preserves nothing. The kept-log store
is bounded (age, count and per-file size), and that pruner is exercised directly.
