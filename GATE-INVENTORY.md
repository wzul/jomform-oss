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
became a human-only edit, which is why the table lagged the runner by **five**
gates at one point and still lagged by two.

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
| `test` | api | `cd api && go test ./... -race -count=1` | regressions **and data races** |
| `govuln` | api | `govulncheck ./...` | known CVEs reachable from the module graph |
| `web` | web | `cd web && npm run build` | `test:unit` (incl. the cross-language Go-contract guards) then the Next build |
| `a11y` | web | `cd web && npm run a11y` | axe-core; fails on any **critical/serious** WCAG violation |
| `npmaudit` | web | `cd web && npm audit --audit-level=high` | high/critical npm advisories |
| `gitleaks` | secrets | `gitleaks detect` over the **full git history** | committed secrets (repo may go public) |
| `monitor` | watchdog | `deploy/monitoring/jomform-watch_test.sh` | the uptime watchdog alerts on a failure, stays quiet on a success, and never reports green when it has no way to alert |
| `tmpkey` | helper | `deploy/tmp-apikey_test.sh` | the temp-API-key helper cannot carry a psql tag in the key id, deletes by exact name, and treats a surviving row as a failure |
| `backup` | suite | `deploy/backup/jomform-backup_test.sh` | the nightly backup's own failure-path suite: a copy that "exits 0" having landed nothing, a same-size object whose bytes differ, an empty dump archive, a remote with no hash, a restore drill that must not overwrite the backup job's status, and the fail-closed override seam |
| `prunewt` | helper | `deploy/worktrees/prune-kanban-worktrees_test.sh` | the worktree pruner's durability test cannot be fooled by a hand-made `refs/remotes/pr/<N>` mirror: work held only by one is reported UNSAFE instead of being removed, while genuinely pushed/merged work is still removed and a live worker's worktree is still kept; and its exit-code contract is read off real runs — a deliberate keep with a reclaim is `0`, keeping at-risk work while reclaiming nothing is `3`, a failed removal / unreadable card / failed fetch is `1` |
| `refguard` | refs | `scripts/refguard_test.sh` | the hand-made `refs/remotes/<non-remote>/<N>` mirror guard BITES — the documented GitHub idiom is refused in the primary checkout and in a linked worktree, deletion stays allowed, a second remote's namespace stays allowed; a hook that is missing, non-executable or stale is reported NOT ARMED instead of quietly guarding nothing |
| `selftest` | selfcheck | `scripts/preflight_test.sh` | the gate runner's OWN safety properties: an all-skipped run is refused rather than reported green, a missing prerequisite fails instead of skipping, and this inventory cannot drift from the gates that can actually run |

`./scripts/preflight.sh --list` is the runtime authority; this table is the
documentation that the `selftest` gate keeps honest. When you add a gate to
`scripts/preflight.sh`, add its row here **in the same change** — that is now a
gate, not a convention.

See also: `deploy/BACKUP.md` (the backup suite), `MCP-PARITY.md` (the same
gated-file workaround for MCP tool parity).
