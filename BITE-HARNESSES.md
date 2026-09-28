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
| `api/scripts/biteproof_model_default_guard.py` | the docs-parity half of `api/internal/llm/model_default_guard_test.go` (card `t_8853fc29`): that guard read the pinned MCP tool descriptions out of ONE file (`api/internal/mcpsrv/tools.go`) while the registry the rest of the repo agrees on is the PACKAGE (#732 moved `disable_2fa` into `parity_issue396.go`). Sabotages a MOVE of `generate_form` / `generate_legal_page` into a sibling file and requires each reader to answer about the registry: the one-file reader falsely blames `tools.go` when the literal merely moved (and passes against a STALE copy left behind), the package reader resolves the move, refuses to pick between two pairs for one name, and refuses a registry file in a subdirectory instead of under-reading it. Each case's pair is LIFTED from `tools.go` with the guard's own pattern rather than hand-typed, every mutation must still compile, and both legs run at a base revision (`BITEPROOF_BASE`, default `origin/main`) against the working tree — the harness exits 2 rather than compare a reader with itself | 7 | see the measured line below the table |
| `api/scripts/biteproof_verify_fail_page.py` | the GET /v1/auth/verify failure-response guards in `api/internal/httpapi/verify_fail_page_test.go` (card `t_8c67149d`): a GET on a bad/expired link answers a localized HTML page rather than the machine JSON envelope, POST keeps the envelope, the three failure classes read differently, Arabic sets `dir=rtl`, and the page stays over the browser's 512-byte discard floor | 6 | 201s |
| `api/scripts/biteproof_unlock_class_guard.py` | the unlock class guard in `api/internal/httpapi/unlock_ratelimit_class_test.go` (cards `t_b2888b6c`, `t_5c6413aa`, `t_36bbfec4`): every `.../unlock` pattern the SERVED route table holds answers a password burst with a 429, however its registration was spelled. Seven sabotages, each a compiling revert with `go build ./...` asserted OK: an INDIRECT unwrapped route (the shape the guard's FIRST, text-regex version could not see), the same route WRAPPED as a control that must stay green, a LITERAL unwrapped route (the shape the old guard did catch, so reworking it regressed nothing), a SHIPPED route losing its wrapper, an INDIRECT route registered from ANOTHER FILE in the package, and — added by `t_5c6413aa` — an UNWRAPPED route registered INLINE on the served table in `server.go` (the shape the SECOND guard's hand-re-listed mirror could not see) plus the same inline route WRAPPED as its control. A red must NAME the offending route, so a failure cannot be attributed to a sibling. Every case — the controls included — must also show the guard RAN and DROVE the route (added by `t_36bbfec4`): the named test's `--- PASS:`/`--- FAIL:` marker and the route inside the guard's own enumeration block, which needs `-v` because `go test` swallows `t.Logf` on a passing run. Without that a control's green could not be told from "the guard never ran" — `go test -run` matching nothing exits 0 with "no tests to run". The evidence check is itself self-checked on every run against a REAL vacuous output (must be rejected) and a REAL driven one (must be accepted) | 7 | 304s |
| `web/scripts/biteproof_next_managed_files.sh` | the Next managed-file guard in `web/src/lib/nextManagedFiles.test.ts`: the `agentRules:false` switch, the `/AGENTS.md` + `/CLAUDE.md` ignore rules, the `tsconfig` include, `web/next-env.d.ts` staying untracked and ignored, and the skip's own premise | 6 | 82s |
| `web/scripts/operator-bite.sh` | the operator-identity guard in `web/src/lib/operatorNotice.test.ts`: the operator block on the legal surfaces, one definition of the registration number, the RTL isolation, the locales, and no platform identity leaking into a merchant's footer | 11 | 14s |
| `web/scripts/biteproof_docs_tool_claims.sh` | the tool-claim shapes in `web/src/lib/docsToolClaims.test.ts`: the two phrasings the guard MISSED (an underscore-free name in `as the X tool`, and the header-comment parenthetical `… pairs with (propose_write)`, both one-line and wrapped across two comment lines), plus the two it already caught, plus two negative controls (a registered name stays quiet, real prose stays quiet). Each case runs the guard at the branch's BASE (`origin/main`, overridable with `BITEPROOF_BASE`) and requires it GREEN on the mutated tree — that is the "it missed this" column — then the guard in the working tree and requires it RED naming the offender, then restores the page byte-identically (sha256). The base guard must itself be GREEN on the unmutated tree or the script exits 2 without a verdict (a red base would make every case read "does not reproduce"), so it is not a comparison against a known-broken revision | 6 | 84s |
| `scripts/biteproof_web_image_gate.sh` | the `webimage` preflight gate (a real cache-only `docker build -f web/Dockerfile`), which is the only gate that builds the SHIPPING web artifact. Three cases, each requiring the gate to go RED **and** the host suite (`npm run test:unit`) to stay GREEN — that second direction is what proves the host never saw the class: a healthy tree at HEAD is green (not a tautology), a `.dockerignore` rule that filters a `COPY` source out of the build context (the MCP-PARITY.md defect one layer out), and a runtime-stage `COPY --from=build` naming a path the build stage never produced (outside `buildContext.test.ts`'s reach, which parses the build stage's COPYs only) | 3 | 1st run on this host, load ~30: see the measured line below the table |
| `web/scripts/biteproof_mcp_tool_inventory.sh` | `web/src/lib/mcpToolInventory.test.ts`, both directions: case 1 runs the guard on the REAL pre-fix page (the revision at the merge-base with `origin/main`) and requires it RED naming all ten tools that page omitted, then the mutation cases — a registered tool whose row is deleted (forward), a new Go tool with no row (forward), a row that is not registered (reverse), a duplicated row, the em-dash placeholder row, a hand-typed count, a note-only group counted as a category, and the CLAIM case (card `t_5f8083eb`): a single FILE named as the registry in the page's metadata description, proven on both legs — the file that shipped the claim (`tools.go`) and the file that falsified it (`parity_issue396.go`) — each RED for its own named reason, the needle derived from the current per-file `&mcp.Tool{` counts. Every file restored byte-identically (sha256 re-checked). **Case 1 is coupled to a merge-base with `origin/main`, so it refuses on a tree cut from current main (there the page at the merge-base is byte-identical to this tree's, and the case would prove nothing). It is run LAST for that reason: it used to sit first and its `exit 2` took every case after it down, which is how case 7 was unreachable on this card's own branch.** | 9 | see the measured line below the table |
| `web/scripts/biteproof_settings_tabs.sh` | the settings-tab-strip guards in `web/e2e/settings-tabs.spec.ts` (card `t_dc06`): the strip class, the reveal wait, the inset focus-ring rule, and the tab-count wait — each mutated one at a time, with the page-side cases REBUILDING the app first so the spec measures this tree's build rather than a previous one. Every case must produce a red at a NAMED assertion, not a crash or a failed build | 4 | see the measured line below the table |
| `deploy/backup/jomform-backup_bite.sh` | the failure paths the nightly backup's suite asserts (`deploy/backup/jomform-backup_test.sh`, the gated `backup` suite): the fail-closed seam and the legacy refusal paths, sabotaged one at a time in a throwaway copy | 29 | 2110s |

Three rows above (`scripts/biteproof_web_image_gate.sh`,
`web/scripts/biteproof_mcp_tool_inventory.sh`, `web/scripts/biteproof_settings_tabs.sh`)
were added by the card that found them unregistered: all three files had been on
main since #758 / #766 / #787 without a row, and none of those three changes
touched this file — so `TestEveryBiteHarnessIsRegisteredInTheInventory` failed the
`api test` gate on a pristine main.

`api/scripts/biteproof_model_default_guard.py` is the same shape one card later:
the guard it covers (`api/internal/llm/model_default_guard_test.go`) had no row
because no harness had ever backed it, which is why widening its registry scope
could only be proved by writing the first one. **Measured on this host (card
`t_8853fc29`, load ~25 on 6 cores): 2m43s, ALL 7 CASES BITES, EXIT=0.** Each case
printed its two legs at their own named needle and the unmutated tree was green
under BOTH readers first (the control), so no case's red is this tree being
broken. The three sabotaged/created files came back byte-identical (`tools.go`
`fdc1caf043712ce6`, `parity_issue396.go` `27eca0bf9ce259c7`, the guard
`2a09eec123dacaca`), the throwaway `api/internal/mcpsrv/sub/` directory was
removed, and `git status` held **3 entries before and 3 after, 0 introduced by the
run** — the tree was already dirty because this harness runs on the branch that
changes the guard, which is why it compares status before/after instead of
demanding a clean tree.

What was measured on this host for those three rows, and what was not — stated
apart, because the two are not the same claim:

- `web/scripts/biteproof_settings_tabs.sh`: **1088s, ALL CASES OK.** All four cases
  went red at their own NAMED assertion (the strip wrapping to 5 rows at 320px, the
  selected tab scrolled out of the strip's window, the focus ring clipped at the
  left edge, and the count wait never reaching the 12 tabs) and the three mutated
  files came back byte-identical (sha256 re-checked).
- `web/scripts/biteproof_mcp_tool_inventory.sh`: **exit 2, 1s** — not a pass and not
  a failure of the guard. Case 1 resolves the pre-fix page from `git merge-base HEAD
  origin/main`, and this branch is cut from current main, so that revision is
  byte-identical to the tree's own page; the harness refuses to run a case that
  would prove nothing rather than reporting green. On a branch with the page
  changed, or on the primary tree, it runs all nine.
  **Re-measured 2026-09-27 by the card that added case 7** (`t_5f8083eb`), on a branch
  cut from `08854c2c`: **all 9 mutation cases BITES, 41s, then exit 2 on case 1** —
  case 1 is now run LAST, because while it sat first its refusal called `exit 2` and
  took the whole mutation set down with it (which is how case 7 was unreachable). The
  seven pre-existing cases kept their verdicts, case 7's two legs went RED naming
  `holds 176 of the 177` and `holds 1 of the 177` respectively, and the page came back
  byte-identical (`76c5c3889823`).
- `scripts/biteproof_web_image_gate.sh`: **case 0 completed GREEN** (the healthy
  tree at HEAD passed the real `docker build` gate, so the gate is not a
  tautology), then cases 1-2 did not complete on this host: each case runs
  `./scripts/preflight.sh --only webimage`, which takes the same host-wide
  preflight lock that the merge and QA lanes were queuing on (~25 load average on 6
  cores), and the run was still waiting on it when this card had to hand off. Its
  wall time is therefore **unmeasured**, and this cell says so rather than quoting a
  number nobody produced.

**None of the three carries a wall-time figure that was not actually run on this
host**, which is the one thing this document exists to prevent.

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
`verify.go`, `public.go` and `server.go` (the unlock class guard walks the served
route table `Server.routes()` there, so a route added to it moves that harness's
subject), `api/cmd/jomform-api/main.go`, `api/internal/config/config.go`,
`api/internal/llm/llm.go` and the tool registry `api/internal/mcpsrv/*.go`; for
the web ones
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
