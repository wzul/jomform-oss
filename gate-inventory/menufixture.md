# `menufixture` — web gate detail

Command (what the table row runs): `bash scripts/menu-fixture-e2e.sh`

Edit THIS file for what this gate catches and its history. The row in
[`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes only
when the gate is added, removed, renamed or its command changes — that split is
why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

**A menu checkout whose option-group LAYOUT is broken for a real buyer.** The
gate starts `api/cmd/menu-fixture` — the REAL public handler served over real
HTTP from a scratch schema of the shared test database, seeded through the real
stores, carrying one `radio` group (`sd1`) and one `image-choice` group (`ic1`)
— then drives Chromium (`web/e2e/menu-checkout-option-group.spec.ts` through
`web/playwright.menufixture.config.ts`) at **1200px and 375px** and measures
geometry, not markup:

- every option control is intrinsic size (12–16px), NOT the row's size;
- the gap from a control's own edge to ITS label text is the row's ~8px;
- the buyer name/email fields are still full width (row width ±1px) and 44px tall
  — the other half of the same rule, so a fix that widens the reset to every
  `input` fails here too;
- the pay button spans the row;
- the document does not scroll sideways (`scrollWidth <= clientWidth + 1`);
- the **payment-method picker** (card `t_453489b9`) on its own terms, because its
  radio is a deliberately larger control (authored 20x20 in a 44px row): 12–24px,
  each row the full width of its group (±3px) and at least 44px tall, and the label
  beside the control in `.payrowtext` at the row's ~10px gap;
- the two measured sets together account for **every** `.sheetfoot
  input[type=radio]`, so a new kind of radio fails instead of going unmeasured;
- and the two groups are separately named, so answering the second does not clear
  the first (`t_5d625806`).

WHY THE PICKER IS NOT IN THE 12–16 BAND (card `t_484f19c9`). When the picker
landed, this spec measured EVERY radio in the sheet foot against the 12–16 band
that was written for option-group controls, so #1686 reded the `menufixture`
gate on a control that was not defective — measured on the candidate tree,
`pay_method` came back 20x20 while `sd1`/`ic1` were still the correct 13x13 with
an 8px gap. Widening the band to 24 would have blunted the guard the gate
exists for, and simply excluding `.paypick` would have left a radio nobody
measures. The split above keeps the option-group band exactly as it was and
gives the picker a real assertion of its own, tied together by the coverage
count.

## Why it is a gate and not an on-demand script (card `t_ef32ea3b`)

Card `t_664549fc` was `.sheetfoot input { width:100%; height:44px }` — the
buyer-field rule — also matching each option's radio. The control measured
399x44 inside its flex row (294x44 at 375px), which pushed the merchant's label
to the row's edge and off a 375px screen. Every guard in the tree was green:

- `api/internal/menufixture/fixture_test.go` proves the served page carries both
  groups and that the order route stores the answer — markup and storage, which
  is all a Go process can check;
- `api/internal/httpapi/menu_radio_size_t_664549fc_test.go` pins the CASCADE of
  the anti-defect rule — still not layout;
- and `web/e2e/menu-checkout-option-group.spec.ts` **skipped** inside the `e2e`
  gate, because it needs `MENU_FIXTURE_URL` and only this script sets it.

A flex row and an intrinsic control size are resolved by a layout engine, so a
Go process cannot see this class at all. The script's header used to say "NOT A
GATE" on cost grounds; MEASURED 2026-11-09: `3 passed (3.3s)`, real **0m18.4s**
with the Go build warm, and it leaves no schema, no listener and no process
behind. 18s is cheaper than `speccounts`, so the only guard that can see a
layout defect in a menu option group now runs on every release instead of when a
lane remembers.

## Prerequisites, and why a missing one is RED

The gate refuses — never skips — when any of these is absent, with the fixing
command in the message (`preflight_env` in `scripts/preflight.sh`, and the script
re-checks the same three):

- `JOMFORM_TEST_DATABASE_URL` — the fixture seeds a scratch schema of the shared
  test database (`source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}`);
- `go` on `PATH` — the fixture is built from `api/cmd/menu-fixture`;
- `web/node_modules/.bin/playwright` — `cd web && npm ci && npx playwright install chromium`.

A missing Chromium binary fails inside playwright loudly
(`Executable doesn't exist … npx playwright install`), which is a red gate too.
There is no path through this gate that exits 0 without having measured the page.

## Isolation — it collides with nothing and leaves nothing

- **Schema**: the fixture names its own `test_menufixture_fx_p<pid>` schema of the
  shared test database (NOT `CREATE DATABASE`, never the `test` gate's
  `test_<pkg>_p<pid>` schemas), and drops it on exit. The script's `trap cleanup
  EXIT INT TERM` kills the fixture gracefully and waits for it to drop its own
  schema before removing the work directory, so a FAILING run cleans up too; a
  `kill -9` fallback is the last resort and a killed run's schema is still
  reclaimable by `internal/testdb`'s sweeper.
- **Listener**: an EPHEMERAL loopback port (`-addr 127.0.0.1:0`), so it does not
  contend with the `a11y`/`e2e` server port claim; two lanes may run it at once.
- **Process**: the fixture process, its binary and its work directory all live
  under a `mktemp -d` work directory removed by the same trap.

Measured after a PASS and after a BITE-proof failure (2026-11-10): no
`test_menufixture%` schema in `pg_namespace`, no listener on the fixture's port,
no `menu-fixture` process.

## Cost

~18s in a full run (Go build warm, browser cache warm from the `e2e` gate it runs
after). 39s cold on a worktree whose fixture package had never been built.

## Ordering

It runs immediately after `e2e` in `scripts/preflight.sh` so it uses the browser
cache that gate warmed, and before the `prunewt`/`prunehost` tail.

## The bite proof (on demand, not part of the gate)

Restore the defect and this gate goes RED while a Go-shaped guard cannot see it:
delete the anti-defect reset rule in `api/internal/httpapi/menu.go`

```
.sheetfoot .radios input[type=radio] { width:auto; height:auto; ... }
```

So `.sheetfoot input { width:100%; height:44px }` wins again, and run
`./scripts/preflight.sh --only menufixture`. Both widths fail with the control's
measured size named in the assertion. Restore the rule and the gate is green
again. Recorded for card `t_ef32ea3b` at **399x44 at 1200px / 294x44 at 375px**
(the `t_664549fc` geometry).

Re-measured 2026-11-10 while splitting the picker out (card `t_484f19c9`), by
`bash scripts/menu-fixture-e2e.sh` on the branch tree, three readings:

| mutation | reading |
|---|---|
| none (the shipping tree) | `3 passed (3.4s)` |
| delete `.sheetfoot .radios input[type=radio] { … }` | **RED** both widths: `Expected: <= 16, Received: 399.4` (1200px) / `294.4` (375px) at the option-control width assertion |
| delete `.sheetfoot .paypick .payrow input[type=radio] { … }` | **RED** both widths: `the picker's fpx control is 218.7px wide, want <= 24` (1200px) / `140.5` (375px) |

The second mutation is the reason the picker has its own band rather than being
excluded: without its reset rule the picker's radio takes the buyer-field rule
too, and a spec that had merely dropped `.paypick` from the selector would have
stayed green while the row's radio filled the row.
