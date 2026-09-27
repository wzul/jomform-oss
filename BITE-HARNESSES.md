# Bite-harness inventory — the proofs that a guard *bites*, and what runs them

This file answers one question that a green `./scripts/preflight.sh` cannot:

> **A guard runs on every gate. What proves the guard would fail if the behaviour
> it pins were removed?**

That is a **bite proof** (a *biteproof* / *bite harness*): it sabotages one thing
at a time in a guard's subject, runs the guard, and requires the guard to go RED —
then restores the source byte-identically. A guard nobody has ever seen fail is a
tautology, and it looks exactly like a guard that works.

**None of these harnesses is a per-merge gate.** The `scripts/preflight.sh`
table in `GATE-INVENTORY.md` names every gate a green run actually executed; this
file names the instruments that a green run did **not** execute. The two lists do
not overlap, and that is deliberate — see *Why they are not gates* below.

## The harnesses

One row per harness, the path backticked first. The `api test` gate checks this
table in both directions (`api/internal/biteharness`): a harness on disk that has
no row here fails the build, and a row naming a file that does not exist fails it
too. So the table cannot rot into a claim about instruments that were renamed away.

| Harness | Proves (the guard it backs) | Cases | Wall time here |
|---|---|---|---|
| `api/scripts/biteproof_menu_variation.py` | the menu-variation guards in `api/internal/httpapi/menu_variation_test.go`: the checkout payload carries the variant, live variation resolution, the selector markup, the card-tap path, per-row attribution, sold-out disabling. **STALE ON MAIN, measured 2026-09-26: 3 of its 8 cases run, then it aborts with `HARNESS BUG: 'card tap adds the base price...' occurs 0 times, expected 1, exit 2`. PR 770 took that path through the shared `addIfBuyable` helper, so the anchor it searches for is gone; `biteproof_menu_variant_gaps.py` covers that behaviour (and 22 more) and is green, so the net is intact, but this instrument needs re-anchoring before it proves anything again.** | 3 of 8 run | 110s (aborts) |
| `api/scripts/biteproof_menu_variant_gaps.py` | every guard declared in `api/internal/httpapi/menu_variant_scope_test.go` (each declared guard must be named by a case, and a case counts only when its named guard is among the failures; `TestMenuInStockVariantPickStillAdds` is the one listed control): the hint and clearPick lookups, the shared add path on both the change handler and the card tap, the live-stock re-checks (render-time attribute vs a live read, the basket on open, the checkout POST, the scoped live-stock route), the form/menu selector contract in both directions, and the sold-out locales | 23 | 486s |
| `api/scripts/biteproof_ses_replyto.py` | the SES Reply-To wiring guard in `api/internal/config/ses_replyto_wiring_guard_test.go`: both `email.SMTPConfig` senders in `cmd/jomform-api/main.go` carry `ReplyTo: cfg.SESReplyTo`, and an unset `SES_REPLY_TO` writes **no** header instead of falling back to `SES_FROM` | 5 | 122s |
| `web/scripts/biteproof_next_managed_files.sh` | the Next managed-file guard in `web/src/lib/nextManagedFiles.test.ts`: the `agentRules:false` switch, the `/AGENTS.md` + `/CLAUDE.md` ignore rules, the `tsconfig` include, `web/next-env.d.ts` staying untracked and ignored, and the skip's own premise | 6 | 82s |
| `web/scripts/operator-bite.sh` | the operator-identity guard in `web/src/lib/operatorNotice.test.ts`: the operator block on the legal surfaces, one definition of the registration number, the RTL isolation, the locales, and no platform identity leaking into a merchant's footer | 11 | 14s |
| `deploy/backup/jomform-backup_bite.sh` | the failure paths the nightly backup's suite asserts (`deploy/backup/jomform-backup_test.sh`, the gated `backup` suite): the fail-closed seam and the legacy refusal paths, sabotaged one at a time in a throwaway copy | 29 | 2110s |

The table carries one row more than the tree does: `biteproof_ses_replyto.py`
ships with PR 772, and its row is registered here (and in the guard's
`notYetOnMain` allowlist) so that landing it needs no doc edit. The guard consults
that allowlist only for a file that is ABSENT, and refuses an allowlisted name the
table does not carry — so it cannot be used to excuse a harness missing from both.

"Wall time here" is my own measurement on the agent host at load average 15-21 on 6
cores (2026-09-26), from a run I executed for EVERY row — none of these figures is
quoted from another document. Load matters: the same host measures one warm
incremental `go build ./...` at 35s where the gate header records 13s, so treat
these as *this host under load*, not as a hardware-independent constant. Each
harness was run against the tree as it stood on this file's branch, and each one
that completed ended with its sources restored byte-identically and `git status`
clean.

Rows go stale when the source a harness sabotages moves, which is exactly what this
file exists to make visible — and it has already happened once, **measured**: at
this file's first cut `biteproof_menu_variant_gaps.py` had 14 cases and the backup
proof had 20; by the time the branch was re-cut onto current main they had 23 and
29, because landings took their subjects (and their case lists) with them. Both
were re-run against the new tree and their rows re-derived above. The
`biteproof_menu_variation.py` row carries the other outcome of the same check: its
anchor no longer matches, so it now aborts part-way and its row says so rather than
quoting a stale pass.

Two documented figures in the repo are out of date, both caught by running the
things instead of reading about them:

- `deploy/backup/README.md` documents the backup bite proof as `~8 minutes`; the
  measured run is **35m10s** (29/29 caught). That README was not edited — it is the
  backup lane's page and its "so it is not a gate" conclusion still holds — but the
  register carries the measured number.
- `biteproof_menu_variation.py`'s own output still advertises 8 cases; on this tree
  3 of them run and it exits 2. Filed as its own card, since re-anchoring it is a
  code change to a file a different landing moved.

## Why they are not gates

The card that created this file asked for the wall time before deciding. Measured,
on this host, at load ~15 (6 cores):

| Unit of work | Measured |
|---|---|
| one warm incremental `go build ./...` | 35s |
| one `go test ./internal/config/ -count=1` (the SES harness's unit) | 1.3s |
| one named `httpapi` test, warm test binary | 7.2s |
| the same, after touching a source file in the package | 8.7s |

`deploy/backup/jomform-backup_test.sh` — the *suite*, not its bite proof — is
already a per-run gate, and its bite proof is deliberately left on demand for the
same reason: `deploy/backup/README.md` records ~8 minutes, "8 minutes on every
merge is how a gate gets skipped". The bite proofs above are the same shape and
strictly more expensive per case, because each case costs a `go test` (or `npm
run test:unit`) that has to re-run the guard from a mutated source:

- the three `api` harnesses are **3m20s – 5m** each, and they are the cheap end:
  each mutation runs one full package or a named test, and the `api test` gate they
  would sit next to is 2m35s on an idle host;
- the two `web` harnesses need an installed `web/node_modules` (the `web` gate's
  own prerequisite) and re-run `node --test`;
- the SES harness only exists once PR 772 lands, and is the cheapest at 2m02s —
  still over the cost of the gate it would duplicate.

Wiring all of them would roughly double a full gate, and the merge lane already
waits on a serialised gate lock. So: **option 2 of the card — declared manual, and
made machine-checkable** rather than silently absent. What was actually wrong was
not that they were manual; it was that nothing said so, and nothing noticed when
the set changed.

## Who re-runs them, and when

The author of a change that either (a) touches a guard named in the table above, or
(b) touches the source that guard pins — for the `api` harnesses that is
`api/internal/httpapi/menu.go`, `render_blocks.go`, `formLang.go`, `order.go`,
`api/cmd/jomform-api/main.go` and `api/internal/config/config.go`; for the web ones
`web/next.config.mjs`, `web/.gitignore`, `web/tsconfig.json`,
`web/next-env.d.ts`; for the backup one the seam and failure paths in
`deploy/backup/jomform-backup.sh`.

**When:** in the same PR, before handing it to verify. Re-run the harness whose
subject you touched, and quote its summary line (every harness prints a per-case
verdict and a final restored/`BITES` line). A harness whose subject moved and was
not re-run is the exact shape this file exists to make visible — the guard still
runs on every gate, but nothing has checked that it still fails when it should.

The merge lane runs them when a PR's diff touches one of those subjects and the
handoff does not already quote a run. They are on demand, never a merge blocker on
their own: a bite proof that was not re-run is a *gap in evidence*, not a red gate.

## Adding a harness

Add the file, add its row here in the same change, and name the guard it backs —
the `api test` gate refuses a `*bite*` file with no row (`api/internal/biteharness`),
and a row whose file is gone. Do not add a row for anything that is not a runnable
bite harness, and do not wire one as a gate without also correcting its row here:
`GATE-INVENTORY.md` is the list of what a green run executed, and this file is the
list of what it did not.

See also: `GATE-INVENTORY.md` (the gate table the `selftest` gate parses),
`deploy/backup/README.md` (the backup suite and its bite proof),
`api/internal/mcpsrv/mcp_parity_test.go` and `MCP-PARITY.md` (the same
gated-data-file split, for MCP tool parity).
