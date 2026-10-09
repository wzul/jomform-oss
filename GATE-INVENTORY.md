# Gate inventory — every gate `scripts/preflight.sh` can run

This file holds the **gate table** parsed by the `selftest` gate
(`scripts/preflight_test.sh`). It is the machine-checked half of the rule in
`AGENTS.md` § "LOCAL BUILD/TEST BEFORE MERGE".

**Why this is a separate file.** The table used to live in `AGENTS.md`, which is a
**write-gated** agent-instruction file (`tools/file_tools_write_guards.py`,
`_PROTECTED_INSTRUCTION_BASENAMES`): a headless Kanban worker has no interactive
approval channel, so its write is **refused** and the card blocks. That is not a
theoretical failure — `t_59df76bb` (dev) blocked on exactly this, and the same
class of edit has been attempted repeatedly (`t_7f89fdf4`, `t_14d9456f`,
`t_fe89f2f6`, `t_a170e5de`, `t_0060e4f0`). Every gate-table update therefore
became a human-only edit, which is why the table fell **five** gates behind the
runner at one point. It is level again (measured 2026-10-07: 18 rows for 18 gates
from `scripts/preflight.sh --list`), and the `selftest` gate compares the two so it
cannot drift silently — but the history is why the table lives here.

The RULE stays in `AGENTS.md`. The DATA the guard parses lives here, where agents
can update it. This is the same split `MCP-PARITY.md` uses, for the same reason.

## How the guard reads this file

`scripts/preflight_test.sh` parses **the first backticked token of each row** in
the table below and compares that set against `./scripts/preflight.sh --list`:

- a gate the runner can run but that is **absent here** → the docs are lying, RED;
- a name here that the runner **cannot** run → a gate nobody has, RED.

So: one row per gate, the gate name in backticks first, and **do not** add rows
for anything that is not a runnable gate. The name must match `--list` exactly.

An empty or unparseable table is a FAILURE, never a pass — a guard that reads
nothing proves nothing.

## The gates

| Gate | Job | Command | What it catches |
|---|---|---|---|
| `build` | api | `cd api && go build ./...` | compile errors |
| `vet` | api | `cd api && go vet ./...` | suspicious constructs `go build` skips (it does not compile `_test.go`) |
| `gofmt` | api | `cd api && test -z "$(gofmt -l .)"` | files that are not gofmt-clean — a diff nobody reviewed, and the class two REAL defects (a docs paragraph read as a `#` heading, a whole test body left at column 0) hid in until cards `t_40bc6f95` / `t_d60f987b`. `go vet` does not check formatting, so nothing did |
| `test` | api | `cd api && go test ./... -race -count=1 -timeout 30m` | regressions **and data races**. The `-timeout` is an explicit per-package ceiling (30m, overridable with `API_TEST_TIMEOUT`): without it every package inherits Go's 10m default and `internal/httpapi` — the largest package, whose cost tracks host load — is reported as `panic: test timed out after 10m0s` for a package that is green (measured: pristine main rc=1 at 601.195s, green rc=0 at 620.593s on the same host). A `panic: test timed out` red cannot be told from a hung test. The `selftest` gate asserts this budget is present, that `API_TEST_TIMEOUT` reaches the argv, and that a stripped copy goes RED |
| `govuln` | api | `govulncheck ./...` | known CVEs reachable from the module graph |
| `web` | web | `cd web && npm run build` | `test:unit` (incl. the cross-language Go-contract guards) then the Next build |
| `lint` | web | `cd web && npm run lint` | `eslint .` over the whole web tree — the errors only (`--max-warnings` is not set, so the advisory tail stays advisory). Nothing checked the web tree for anything but types and the unit tests until this gate; the tooling is eslint 9 + `eslint-config-next` with a flat config at `web/eslint.config.mjs`. `eslint .` exits non-zero on an error, which is what makes it a gate rather than a report. The React Compiler diagnostics that `eslint-config-next` 16 pulls in are off per-rule with the reason at each line (no compiler is configured here) |
| `webimage` | web | `docker build -f web/Dockerfile .` (cache-only) | the **shipping web artifact** — no other gate built it, so a tree could be green while `docker build -f web/Dockerfile .` died |
| `a11y` | web | `cd web && npm run a11y` | axe-core critical/serious violations — the axe family only (playwright project `a11y`, `a11y*.spec.ts`, 39 tests). The count is MEASURED, never typed: `cd web && npx playwright test --project=a11y --list` says 39 in 5 files without needing a build (use it to re-measure when the family grows — a bare typed number is exactly what rotted here: this row said 29 while the gate ran 35). This branch's own `a11y-products.spec.ts` scans the /products list and create row, which is why the family reads 39 and not main's pre-PR 36 |
| `e2e` | web | `cd web && npm run e2e` | the feature end-to-end specs under `web/e2e/` (playwright project `feature-e2e`, 180 tests, of which 2 self-skip when their block type has no input/select to unname — the gate's own summary reads "178 passed", "2 skipped"; measured on 2026-10-09 on the tree of card `t_fb9942b7` (base `c51abd18`; card `t_d1c9a43a` measured 175/173+2 before the status-change confirmation arm was added), and `cd web && npx playwright test --project=feature-e2e --list` reports the same 180 in 31 files with no build needed): a binned workspace restores, a refused bulk publish keeps its sentence, a stale workspace slug does not bounce the dashboard, a hung session read is bounded and offers the retry, a settled 5xx on `/chat` or `/forms` shows the fault sentence + Retry (card `t_d1c9a43a`), a status change shows the emerald confirmation after the reload (card `t_fb9942b7`), …. Re-measure the count when the specs grow — a bare typed number is what rotted here (this row said 134 while the gate ran 164, then said 164 while it ran 172, then 172 while it ran 175, then 175 while it ran 180). These ran inside `a11y` until the split, which is why the split also added this gate — moving them out without a new owner would have been a silent coverage loss |
| `npmaudit` | web | `cd web && npm run audit:gate` | high/critical npm advisories — `npm audit --audit-level=high` PLUS a narrow, self-expiring exception list (`web/npm-audit-exceptions.json`, read by `web/scripts/npm-audit-gate.mjs`). An advisory NOT named in that file fails here with npm's own report; an exception with no `expires` is refused, an expired one fails, and a no-longer-reported one is printed as STALE. The one entry is `braces@3.0.3` (GHSA-vfj7-8cjw-p6xm), reached only through the DEV-ONLY eslint toolchain, with NO published fixed release (`npm view braces@3.0.4` is a 404; npm's only suggestion is a downgrade of `eslint-config-next`) |
| `gitleaks` | secrets | `gitleaks detect` over the **full git history** | committed secrets (repo may go public) |
| `monitor` | watchdog | `deploy/monitoring/jomform-watch_test.sh` | the uptime watchdog alerts on a failure, stays quiet on a success, and never reports green when it has no way to alert |
| `tmpkey` | helper | `deploy/tmp-apikey_test.sh` | the temp-API-key helper cannot carry a psql tag in the key id, deletes by exact name, and treats a surviving row as a failure |
| `backup` | suite | `deploy/backup/jomform-backup_test.sh` | the nightly backup's own failure-path suite: a copy that "exits 0" having landed nothing, a same-size object whose bytes differ, an empty dump archive, a remote with no hash, a restore drill that must not overwrite the backup job's status, and the fail-closed override seam |
| `prunewt` | helper | `deploy/worktrees/prune-kanban-worktrees_test.sh` | the worktree pruner's durability test cannot be fooled by a hand-made `refs/remotes/pr/<N>` mirror: work held only by one is reported UNSAFE instead of being removed, while genuinely pushed/merged work is still removed and a live worker's worktree is still kept; and its exit-code contract is read off real runs — a deliberate keep with a reclaim is `0`, keeping at-risk work while reclaiming nothing is `3`, a failed removal / unreadable card / failed fetch is `1` |
| `refguard` | refs | `scripts/refguard_test.sh` | the hand-made `refs/remotes/<non-remote>/<N>` mirror guard BITES — the documented GitHub idiom is refused in the primary checkout and in a linked worktree, deletion stays allowed, a second remote's namespace stays allowed; a hook that is missing, non-executable or stale is reported NOT ARMED instead of quietly guarding nothing |
| `selftest` | selfcheck | `scripts/preflight_test.sh` | the gate runner's OWN safety properties: an all-skipped run is refused rather than reported green, a missing prerequisite fails instead of skipping, this inventory cannot drift from the gates that can actually run, and the path-aware selection (`--for-diff`) is correct — its table covers every gate, a web-only diff really does skip the api gates and a single `api/` path brings them back, an unclaimed path widens to the full set, a gate marked "cannot narrow" always runs, and the four gate-INPUT prose files (`MCP-PARITY.md`, `GATE-INVENTORY.md`, `AGENTS.md`, `deploy/backup/README.md`) widen the run to the gate that reads them |

`./scripts/preflight.sh --list` is the runtime authority; this table is the
documentation that the `selftest` gate keeps honest. When you add a gate to
`scripts/preflight.sh`, add its row here **in the same change** — that is now a
gate, not a convention.

Two DOCUMENTS quote this gate's command and are **not** parsed by any gate, so
nothing goes red when they drift from the row above: `deploy/DEPLOY.md` (line 91)
and `e2e/README.md` / `docs/CHIP-SANDBOX-E2E.md` all abbreviate it to
`go test ./... -race`. They were already abbreviated before the budget existed,
they are prose rather than runnable commands, and none of them is read by
`selftest` — so they are recorded here rather than edited from this file. The
row above is the copy an operator should take.

**What this table is NOT: the list of every proof in the repo.** A gate proves a
guard *runs*. It does not prove the guard would *fail* if the behaviour it pins
were removed — that is a **bite proof** (a *biteproof* / *bite harness*), and the
bite proofs are **deliberately on demand, not gates** (measured: one case costs a
full `go test`, 2m02s–5m08s per api harness on the agent host, so wiring them
would roughly double a full run). Read `BITE-HARNESSES.md` for the register of
which harnesses exist, what each one proves, and who re-runs them when; the `api`
test gate parses that file in both directions, so a harness on disk with no row
there fails the build. Do not infer from a green run that a bite proof was
re-checked.

See also: `BITE-HARNESSES.md` (the bite-harness register),
`deploy/backup/README.md` (the backup suite and its bite proof), `MCP-PARITY.md`
(the same gated-file workaround for MCP tool parity).
