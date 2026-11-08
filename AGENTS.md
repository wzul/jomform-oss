# AGENTS.md — JomForm repo agent guidance

## LOCAL BUILD/TEST BEFORE MERGE (RULE — there is NO CI; this gate is the gate)

**This repo has no GitHub Actions CI.** The workflows were removed
(`.github/workflows/` no longer exists) because every job failed with **0 steps**
on an account-wide Actions billing failure — a red check on a PR said nothing
about the code, so it was noise, not a gate. It will be rebuilt if/when the repo
goes public. Until then, **the local preflight is the only gate**, and there is
nothing upstream to catch what it misses: if it does not run, nothing ran.

**The FULL preflight is owed once per RELEASE, not once per merge.** `deploy/build-and-ship.sh`
runs it before any image exists, on the exact tree being shipped, and refuses to build on red
(reusing a whole-inventory green record for the same bytes when one exists):

```bash
source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}   # JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL
./scripts/preflight.sh
```

A **merge** may proceed on the evidence the branch's own iteration run produced, provided the
merge record says which evidence it was:

| evidence | when it is enough to merge |
|---|---|
| full green (all gates) | yes |
| scoped `--for-diff <base-ref>` green | yes — name the base-ref and the gates it skipped |
| whole-inventory reuse of a per-tree record | yes — name the record and its tree |
| partial (some gates ran, some never did) | yes — name the gates that did NOT run |
| nothing ran | no |

**A skipped gate is not a passing gate.** Merging on scoped or partial evidence is allowed; saying
it was full when it was not is a false report, and that is the one thing this rule forbids. Every
merge record names the gates that did not run, or states `full`.

Why the release carries the full run instead of the merge:

- the deploy gate runs on the exact bytes that ship, so a red there cannot be mistaken for a red
  somewhere in a long branch history;
- `deploy/build-and-ship.sh` already reuses a whole-inventory green record for the same tree, so a
  tree that WAS fully gated costs nothing at release time — the full run is only paid when no such
  record exists;
- measured: roughly one PR per release on this repo (300+ PRs merged over 20 active days vs ~15
  deploys a day), so per-release gating buys about the same number of full runs as per-merge
  gating. What this rule is for is cheaper ITERATION (scoped runs save 40–72%), not fewer full runs.

The release gate is CPU-bounded: `GATE_CPU_QUOTA` (default 400% = 4 of 6 cores) wraps the preflight
in a transient systemd scope, so a release cannot take the whole host. Measured: an uncapped cold
`go build ./...` peaked at 307% CPU and `npm run build` at 192%, so 400% costs no wall time; a
quota of 100% (one core) cost 4.5x and 2.9x. The quota does NOT cover `docker build` — that runs
inside dockerd, outside the shell's cgroup — which is why `--remote-build` exists.

`scripts/preflight.sh` is the single entry point — the ported CI gates, in the
same order as the three CI jobs they replace.

**The gate table is NOT here any more — it is `GATE-INVENTORY.md` (repo root).**
This paragraph used to carry a hand-written table that lagged the runner by five
gates, plus a warning about hand-written rot that had itself rotted. Both are
gone. The reason is mechanical: `AGENTS.md` is a **write-gated** agent-instruction
file, so a headless worker that adds a gate in `scripts/preflight.sh` **cannot add
the matching row** — its write is refused and its card blocks (observed repeatedly;
last: `t_59df76bb`). Every table update became a human-only edit, which is exactly
how it drifted.

The split is deliberate and is the one `MCP-PARITY.md` already uses: **the RULE
stays here, the DATA moves to a file agents can update.**

1. The table lives in `GATE-INVENTORY.md`.
2. `./scripts/preflight.sh --list` is the runtime authority for what can run.
3. The `selftest` gate **parses `GATE-INVENTORY.md`** and fails the run when the
   table and `--list` disagree in either direction — a gate absent from the table,
   or a name in the table that cannot run. A missing or unparseable table is a
   FAILURE, never a skip.

So when you add a gate to `scripts/preflight.sh`, add its row to
`GATE-INVENTORY.md` in the same change. That is now enforced, not remembered.

`deploy/build-and-ship.sh` calls `scripts/preflight.sh` itself, so a deploy
cannot skip it — the "check before deploy" instruction is now enforced by the
build script, not by asking the operator to remember. `--skip-tests` bypasses
the whole preflight and prints a loud warning; it is for a genuine emergency
only, never a routine release.

Every gate **fails loudly** when its prerequisite is missing rather than
silently skipping — an unset `JOMFORM_TEST_DATABASE_URL` makes `go test` skip
the DB-backed tests and report a green suite that proves nothing, and a worktree
without `web/node_modules` makes `npm run build` exit 0 with
`sh: 1: next: not found` having built nothing. Both shapes are treated as
failures.

Do NOT rely on (or wait for) GitHub checks — they do not exist for this repo
any more, and the branch-protection endpoint returns 403 on the free plan.
Local green is the source of truth.

## MCP FEATURE PARITY (RULE — jangan tertinggal/terlepas)

**Every dashboard feature MUST have a matching MCP tool.** When adding any
new feature to the web dashboard, you MUST:

1. Add/upate the corresponding MCP tool in `api/internal/mcpsrv/`.
2. Register it in `api/internal/mcpsrv/tools.go` (`registerTools`).
3. Re-check the FULL parity list below — don't ship half.

Parity checklist: **`MCP-PARITY.md`** (repo root). It is a separate, ungated file so a
worker can add a tool and its checklist line in one change — this file is write-gated,
which made every parity update a human-only edit.

`api/internal/mcpsrv/mcp_parity_test.go` parses that file, not this one. Keep the two in
step: a tool registered without its checklist line, or a checklist line without its
tool, fails the build.

## DOCUMENTATION PARITY (RULE — jangan tertinggal/terlepas)

**Every feature MUST be documented** — not just have MCP parity. When adding any
new feature (dashboard, MCP tool, placeholder, setting, or behavior), you MUST
add/update the corresponding docs page in the knowledge base (`docs.jomform.com`,
issue #395) so users know what JomForm offers and how to use it:

1. Document the feature in the docs site (how it works, how to use it).
2. Document any new **MCP tool** (name, purpose, inputs, outputs) — keep it in
   sync with the tool registry in `api/internal/mcpsrv/`.
3. Document any new **dynamic placeholder** (e.g. `{admin_email}`, `{{ref_code}}`)
   in the placeholders reference.
4. Add an **inline "?" doc link** in the UI next to the feature/field where
   relevant (e.g. next to the `{admin_email}` field → placeholders doc).
5. Re-check the FULL docs coverage — don't ship a feature with no docs.

## Conventions

- Money = integer **sen** everywhere.
- DB select/scan MUST stay column-aligned (every migration that adds
  columns: update `accountSelect` + ALL Scan call sites — ByID, ByOwner,
  ByEmail — or you get silent 401 storms).
- CHIP create-purchase: `success_redirect`/`failure_redirect`/
  `cancel_redirect`/`success_callback`/`reference` are **TOP-LEVEL** fields.
- Never auto-revoke a card token on cancellation (Wan's rule).
- Repo must stay free of any OnPay mention (open-source constraint).
- **Caller-supplied image/URL fields MUST be scheme-validated `https://`-only**
  through `api/internal/urlguard` — ONE shared rule; never hand-roll another
  `strings.HasPrefix` copy. Applies to EVERY write path, REST **and** MCP:
  form theme `logo_url`/`og_image_url`, landing `image_url`/`logo_url`,
  product `images` + variation images, builder image-block `src`, and template
  `config`/`theme`. Reject a non-https value with 400 / `urlguard.InvalidMessage`;
  empty or omitted means "keep existing" and stays valid. `ObjectStore.PublicURL`
  returns an absolute config-derived https URL, so media uploads pass unchanged —
  do not add a relative-path exception. Do NOT type caller input `template.URL`,
  and do NOT use `ssrfguard.ValidateURL` here (it allows http and resolves DNS —
  it is for outbound webhooks).
  **Why:** an unsafe scheme reaching `<img src>` is silently rewritten by
  `html/template` to the `#ZgotmplZ` sentinel — the image never loads, HTTP is
  200, and nothing errors, so it only surfaces when someone looks at the page
  (#499/#501). The exception is a SERVER-GENERATED data URL (e.g. the QR stand
  PNG): type that field `template.URL` so it is emitted verbatim.

## Adding a migration safely (checklist)

The migration/scan-drift guard in `api/internal/dbstore/migration_scan_drift_test.go`
fails the build if a migration adds a column to a table that has a `*Select`
constant without that column appearing in the select (and struct + scan sites).
Follow this checklist so the guard stays green:

1. Create `api/internal/db/migrations/NNNNN_<name>.up.sql` (and `.down.sql`).
   Use `make migrate-new NAME=<name>` to get the next sequence number.
2. If you `ALTER TABLE ... ADD COLUMN` on a table with a `*Select` constant
   (`accounts`, `forms`, `products`, `sales`, `spaces`, `subscriptions`):
   - Add the column to the matching `*Select` constant.
   - Add the field to the matching struct (`Account`, `Form`, `Product`,
     `Sale`, `Space`, `Subscription`).
   - Update EVERY Scan call site that scans that select (the `*Select`'s
     scan functions — e.g. `accountSelect` → `AccountStore.ByID` +
     `AccountStore.ByOwner`; `formSelect` → `FormStore.scanOne`; etc.).
   - If the column is read only via a dedicated accessor query (e.g.
     `Tracking`, `SigningKeys`, `CFToken`, `ChipOverrideFor`), add it to the
     `intentionallyUnselected` allowlist in the guard test instead — do NOT
     add it to the `*Select`.
3. Run `go test ./internal/dbstore/ -run 'TestSelectScanColumnAlignment|TestMigrationColumnsAppearInSelect'`
   — both must pass.
4. Run the data-integrity invariants against a real Postgres:
   `JOMFORM_TEST_DATABASE_URL=... go test ./internal/dbstore/ -run 'TestMoneyIsIntegerSen|TestRefCodeUniqueness|TestNoOrphanedSalesOnFormDelete|TestSlugUniquenessPerSpace|TestSuppressionListConsistency'`.
5. Run the fresh-DB migration guard — it creates a throwaway database, applies
   **every** `*.up.sql` from zero via the same `db.Migrate` runner the API uses
   at startup, and fails on the first error. This is what catches the 000047
   class of bug (a migration that only fails on a database where the object does
   not already exist — e.g. a `TEXT` column with a FK to a `uuid` PK,
   `SQLSTATE 42804`):
   `JOMFORM_TEST_DATABASE_URL=... go test ./internal/db/ -run 'TestMigrationsApplyFromZero|TestEveryMigrationHasDownFile'`.
   `TestEveryMigrationHasDownFile` also requires every new migration to ship a
   matching `.down.sql`; the two pre-existing gaps (`000005_form_overrides`,
   `000012_spaces`) are pinned in the test's allowlist.
6. Run the full suite: `go build ./... && go test ./...` in `api/`.

CI runs the migration + data-integrity tests against a fresh Postgres service,
so any column/scan mismatch or invariant violation fails the build.