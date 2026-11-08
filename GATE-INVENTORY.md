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
runner at one point. It is level again: **25 rows for the 25 gates**
`./scripts/preflight.sh --list` prints (measured 2026-11-01, after card
`t_28a06494` added `selfhost` and card `t_e2f3a553` added `design`; 23 for 23
when measured 2026-10-28, after card `t_87634d31` added `mediaprobe`;
`t_d9cf1ddd` added `prunesync`; 20 for 20 when measured 2026-10-12 (card
`t_13f1c904` added `prunehost`, PR #1167 added `host`), and 18 for 18 when
measured 2026-10-07), and the `selftest` gate compares the two so it cannot drift
silently.

The RULE stays in `AGENTS.md`. The DATA the guard parses lives here, where agents
can update it. This is the same split `MCP-PARITY.md` uses, for the same reason.

## Why the detail lives in one file per gate

Every row used to be ONE physical line carrying several kilobytes of prose — what the
gate catches, the size of the spec family it runs, and the history of every figure it
had ever quoted. The `a11y` and `e2e` rows sit next to each other, so two branches
that each rewrote ONE row — a PR that adds an axe arm rewrites `a11y`, a PR that adds
a `web/e2e` spec rewrites `e2e` — touched the same region of the same file, and
`git merge` reported a conflict even though BOTH sides were separately correct.
Measured: thirteen rework cards, each a full lane run whose only product was a
one-line splice, and each one then held the merge train until it landed.

The shape below removes the class rather than one instance of it. The table is a
**stable index** — gate name, area, command, one-line summary, link — and everything
that changes as a gate evolves (the prose, the spec counts, the history) lives in that
gate's own file under `gate-inventory/`. Two gates' details are in one diff only if
they are in one file, and there is no such file.

The rule that keeps it that way:

- a row changes only when the gate is **added, removed, renamed**, or when its
  **command** changes — and that same commit creates/edits/removes
  `gate-inventory/<gate>.md`;
- everything else — what it catches, the `**N tests in M files**` figure, what grew
  and by which card — goes in `gate-inventory/<gate>.md`.

`selftest` enforces both halves: the row set and the file set must agree in both
directions, no table row may carry a spec-count figure, and a synthetic merge of two
edits (one gate's detail on each side) must merge cleanly — with the SAME pair of
edits in the old one-line-row shape as the CONTROL, where it must conflict. Without
that control a clean merge could be git being tolerant; with it, the clean merge is
the split's doing.

## How the guard reads this file

`scripts/preflight_test.sh` parses **the first backticked token of each row** in the
table below and compares that set against `./scripts/preflight.sh --list`:

- a gate the runner can run but that is **absent here** → the docs are lying, RED;
- a name here that the runner **cannot** run → a gate nobody has, RED;
- a row with no `gate-inventory/<gate>.md`, or a file there with no row → RED, in
  BOTH directions: the index and the detail are one statement.

So: one row per gate, the gate name in backticks first, and **do not** add rows
for anything that is not a runnable gate. The name must match `--list` exactly.

An empty or unparseable table is a FAILURE, never a pass — a guard that reads
nothing proves nothing.

### The counted rows: two gates carry MEASURED numbers, and a gate re-reads them

`selftest` reads the table for **names only**. The `a11y` and `e2e` gates also run a
spec family whose size is known, and that figure is read by the **`speccounts`** gate
(`scripts/spec-counts.sh`): it runs `playwright test --project=a11y --list` /
`--project=feature-e2e --list` on the tree under test and compares playwright's
`Total: N tests in M files` against the figure the gate's detail file carries.

The contract that makes it parseable: **the CURRENT figure is the FIRST
`**N tests in M files**` span in `gate-inventory/<gate>.md`.** Everything after it is
history ("the figure before it was **145 tests in 16 files**"), so when the family
grows you put the new number first, keep the old one after it with what grew and by
which card — and the gate tells you, in ~5s, instead of the reader having to
remember. A detail file with no such span is RED. The figure belongs in the detail
file and **not** in the row: the one-line rows are what made every count edit a merge
conflict.

## The gates

| Gate | Job | Command | What it catches |
|---|---|---|---|
| `build` | api | `cd api && go build ./...` | compile errors — see [`gate-inventory/build.md`](gate-inventory/build.md) |
| `vet` | api | `cd api && go vet ./...` | suspicious constructs `go build` skips (it does not compile `_test.go`) — see [`gate-inventory/vet.md`](gate-inventory/vet.md) |
| `gofmt` | api | `cd api && test -z "$(gofmt -l .)"` | files that are not gofmt-clean — an unreviewed diff, and a class `go vet` does not check — see [`gate-inventory/gofmt.md`](gate-inventory/gofmt.md) |
| `test` | api | `cd api && go test ./... -race -count=1 -timeout 30m` (a `--for-diff` run may narrow the package list via `PREFLIGHT_TEST_PACKAGES`, record stamped `test=scoped`) | api regressions **and data races**, with an explicit per-package `-timeout` ceiling — see [`gate-inventory/test.md`](gate-inventory/test.md) |
| `govuln` | api | `govulncheck ./...` | known CVEs reachable from the module graph — see [`gate-inventory/govuln.md`](gate-inventory/govuln.md) |
| `web` | web | `cd web && npm run build` | `test:unit` (incl. the cross-language Go-contract guards) then the Next build — see [`gate-inventory/web.md`](gate-inventory/web.md) |
| `lint` | web | `cd web && npm run lint` | `eslint .` over the whole web tree — the errors only — see [`gate-inventory/lint.md`](gate-inventory/lint.md) |
| `design` | web | `bash scripts/design-guard_test.sh` | design-system drift in `web/src/**/*.tsx`: raw hex literals and arbitrary px font sizes — see [`gate-inventory/design.md`](gate-inventory/design.md) |
| `webimage` | web | `docker build -f web/Dockerfile .` (cache-only) | the shipping web artifact — no other gate builds the web image — see [`gate-inventory/webimage.md`](gate-inventory/webimage.md) |
| `a11y` | web | `cd web && npm run a11y` | axe-core critical/serious violations — the axe family only (project `a11y`) — see [`gate-inventory/a11y.md`](gate-inventory/a11y.md) |
| `e2e` | web | `cd web && npm run e2e` | the feature end-to-end specs under `web/e2e/` (project `feature-e2e`) — see [`gate-inventory/e2e.md`](gate-inventory/e2e.md) |
| `speccounts` | web | `bash scripts/spec-counts.sh` | the two spec counts the `a11y` and `e2e` detail files quote, re-read from the tree — see [`gate-inventory/speccounts.md`](gate-inventory/speccounts.md) |
| `npmaudit` | web | `cd web && npm run audit:gate` | high/critical npm advisories, plus a self-expiring exception list — see [`gate-inventory/npmaudit.md`](gate-inventory/npmaudit.md) |
| `gitleaks` | secrets | `gitleaks detect` over the **full git history** | committed secrets over the full git history (the repo may go public) — see [`gate-inventory/gitleaks.md`](gate-inventory/gitleaks.md) |
| `monitor` | watchdog | `deploy/monitoring/jomform-watch_test.sh` | the uptime watchdog alerts on failure, stays quiet on success, and is never green without a way to alert — see [`gate-inventory/monitor.md`](gate-inventory/monitor.md) |
| `tmpkey` | helper | `deploy/tmp-apikey_test.sh` | the temp-API-key helper's key-id rule, exact-name deletion and fail-closed verdict — see [`gate-inventory/tmpkey.md`](gate-inventory/tmpkey.md) |
| `mediaprobe` | suite | `scripts/media-purge-probe_test.sh` | the media purge probe's verdict, against a stub origin+edge (no network) — see [`gate-inventory/mediaprobe.md`](gate-inventory/mediaprobe.md) |
| `backup` | suite | `deploy/backup/jomform-backup_test.sh` | the nightly backup's own failure-path suite — see [`gate-inventory/backup.md`](gate-inventory/backup.md) |
| `host` | suite | `deploy/host/bootstrap-host_test.sh` | the fresh-host bootstrap, repo/host drift check and media migration harnesses — see [`gate-inventory/host.md`](gate-inventory/host.md) |
| `selfhost` | suite | `deploy/selfhost/bootstrap_test.sh` | the one-command self-host path from a fresh clone, offline — see [`gate-inventory/selfhost.md`](gate-inventory/selfhost.md) |
| `prunewt` | helper | `deploy/worktrees/prune-kanban-worktrees_test.sh` | the worktree pruner's durability, exit-code and stand-in contract — see [`gate-inventory/prunewt.md`](gate-inventory/prunewt.md) |
| `prunesync` | host | `deploy/worktrees/check-prune-kanban-worktrees-host_test.sh` then `deploy/worktrees/check-prune-kanban-worktrees-host.sh` | the copy the cron job **RUNS** must be the revision the branch **MERGED** — see [`gate-inventory/prunesync.md`](gate-inventory/prunesync.md) |
| `prunehost` | host | `deploy/host/jomform-container-prune_test.sh` | the prod container-prune guard's decoy suite, in a private dockerd and netns — see [`gate-inventory/prunehost.md`](gate-inventory/prunehost.md) |
| `refguard` | refs | `scripts/refguard_test.sh` | the hand-made `refs/remotes/<non-remote>/<N>` mirror guard bites — see [`gate-inventory/refguard.md`](gate-inventory/refguard.md) |
| `selftest` | selfcheck | `scripts/preflight_test.sh` | the gate runner's own safety properties, incl. this inventory's drift checks — see [`gate-inventory/selftest.md`](gate-inventory/selftest.md) |

`./scripts/preflight.sh --list` is the runtime authority; this table is the
documentation that the `selftest` gate keeps honest. When you add a gate to
`scripts/preflight.sh`, add its row here **and** its `gate-inventory/<name>.md`
detail file **in the same change** — that is now a gate, not a convention, and it
fails in BOTH directions.

Two DOCUMENTS quote one gate's command and are **not** parsed by any gate, so
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
