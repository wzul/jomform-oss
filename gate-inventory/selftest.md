# `selftest` — selfcheck gate detail

Command (what the table row runs): `scripts/preflight_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the gate runner's OWN safety properties: an all-skipped run is refused rather than reported green, a missing prerequisite fails instead of skipping, this inventory cannot drift from the gates that can actually run, and the path-aware selection (`--for-diff`) is correct — its table covers every gate, a web-only diff really does skip the api gates and a single `api/` path brings them back, an unclaimed path widens to the full set, a gate marked "cannot narrow" always runs, and the four gate-INPUT prose files (`MCP-PARITY.md`, `GATE-INVENTORY.md`, `AGENTS.md`, `deploy/backup/README.md`) widen the run to the gate that reads them
