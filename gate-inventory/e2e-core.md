# `e2e-core` — web gate detail

Command (what the table row runs): `cd web && npm run e2e:core`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

every feature end-to-end spec under `web/e2e/` that is neither an `a11y*` spec (the
`a11y` gate) nor a `builder-*` spec (the `e2e-builder` gate) — playwright project
`feature-core`, **320 tests in 62 files**: sales, chat, forms, form settings,
settings/workspaces, products, media, templates, deliveries, activity, landing, login,
session, profile, docs and the cross-page presentation specs. The figure grew by
card `t_2b2da6a7`, whose `web/e2e/landing-header-mobile.spec.ts` is the 61st FILE and
holds 5 arms on the landing page's header at a real phone viewport (390x844,
isMobile/hasTouch/dpr 2): header height <= 72px with every control on one baseline,
no visible control under 44px, exactly one filled primary and it is `/builder`, the
phone sheet opening onto Docs + Log in + both languages as 44px rows (and picking
Bahasa Melayu really switching `html[lang]` and the CTA label), Escape closing it, the
1280px row still measuring Docs 34x20 · Log in 41x20 · CTA 113x36 · EN/BM 44x44 at
71px tall, and the 320px raised-root arm still not panning. Measured with
`cd web && ./node_modules/.bin/playwright test --project=feature-core --list`, which
read **314 in 60** on this same tree before that spec existed (measured, not inferred),
so the delta is exactly its 1 file and 5 arms. The figure before it was **314 tests in
60 files**, which card `t_f11d676f` / PR #1682 grew with
`web/e2e/settings-chip-mobile-actions.spec.ts` — the 60th FILE, 5 arms on the phone
`/settings` screen: its sticky Save bar, its tab strip's "it continues" cue and edge
arrows, and the twelve payment methods as labelled checkboxes. Card `t_b5ea9103` put
the NEWER figure on top of that, `web/e2e/docs-guide-fold.spec.ts` as the 62nd FILE:
one arm over all eight screenshot guides at 390x844 — each page's word count against
700, the y-offset of its first step's heading against the 844px first screen, the
computed `white-space` of its paragraphs and list items, and its lede sentence exactly
once. The figure before it was **319 tests in 61 files**; what the arm holds is under
History.

It runs CONCURRENTLY with `e2e-builder` and `a11y`, each on its own claimed port with
its own `next start` (card `t_ec2aac42`; see `gate-inventory/e2e-builder.md` for the
measurement that motivated the split). The three share the host-wide suite lock for
the GROUP, so one run's three suites overlap while two lane runs still serialise —
the property card `t_d0c0981d` added the lock for.

## The scope map: a `--for-diff` run narrows this gate

A full run of the feature family is the most expensive thing in the preflight
(measured 2026-11-10: 365 tests, 570s, 21% of a ~2,700s release run). A release
therefore runs this gate SCOPED to the diff since the previous release tag:

- `web/e2e/areas.tsv` is the DATA: one row per spec glob, naming the source paths
  whose change makes that spec necessary (`builder-*.spec.ts <-
  web/src/app/builder/**`, `sales-*.spec.ts <- web/src/app/sales/**`, …), with `*`
  rows for areas shared by the whole family.
- `web/scripts/e2e-scope.mjs` is the MATCHER. Narrowing is only ever allowed on a
  POSITIVE attribution. A changed path the map does not claim, a spec with no row, a
  change to `playwright.config.ts` / `web/e2e/*` helpers / the map itself, an empty
  diff, or a decision that selects ZERO specs all widen the run to the WHOLE family.
- the gate PRINTS its decision (`e2e-core: subset — 4/76 specs (matched
  web/src/app/sales/**)`, or `full — 76/76 specs (...)`), so a narrowed run can
  never be read as a full sweep. That is the same discipline as the
  full/scoped/reused labels the rest of the runner uses.
- `PREFLIGHT_E2E=full` forces every spec on any run. That is the on-demand full
  sweep, and it is the only run whose result may be quoted as "the whole suite".

THE BLIND SPOT, NAMED: a narrowed run does not exercise a spec whose area the diff
never touches. That is the trade the scope map makes, it is why the decision line is
printed, and it is why the full sweep is on demand rather than removed. See the PR
body for the bite proof in both directions (a regression in an area the diff claims
reds the scoped run; the same regression in an area it does not claim is caught by
the full run and by nothing else).

## History

**320 tests in 62 files** on the tree that merges card `t_b5ea9103` onto `origin/main`
`494ab6bf1` (the landing-header tree), read with `cd web && ./node_modules/.bin/playwright
test --project=feature-core --list`. The SAME command reads **319 in 61** on that tree
with `web/e2e/docs-guide-fold.spec.ts` moved aside — BOTH measured, so the delta is
exactly the 62nd FILE and its one arm over the eight screenshot guides at 390x844
(`/docs/guides/<slug>`): each page's `innerText` word count against 700, the first
step's heading y-offset against the 844px first screen, the computed `white-space` of
every paragraph and list item (must be `normal`), and the guide's lede sentence
appearing exactly once — the four readings card t_b5ea9103 was verified on after the
intro stopped being printed twice.

**314 tests in 60 files** on the tree that carries card `t_f11d676f` / PR #1682
(`origin/main` `3d1e30ff` with that PR merged and the retired `gate-inventory/e2e.md`
dropped: tree `d76fa3cd7`), read with `cd web && ./node_modules/.bin/playwright test
--project=feature-core --list`. The SAME command reads **309 in 59** on that tree
with `web/e2e/settings-chip-mobile-actions.spec.ts` moved aside — BOTH measured, so
the delta is exactly the 60th FILE and its 5 arms on the phone `/settings` screen.
Arm 1 is the sticky Save bar: pinned to the bottom of the first viewport on a 390px
phone, carrying the connection state in the card badge's own words, disabled until
the form is dirty, submitting the SAME form through `form="chip-creds-form"` (one
PUT, no second write path) and re-disabled once the server holds what the form
holds. Arms 2 and 4 are the tab strip's cue and its edge arrows: a thin scrollbar
plus an edge fade plus a NAMED arrow button on whichever edge still hides tabs (the
arm scrolls to the end of the row and requires the cue to MOVE to the other edge),
mirrored under RTL so Arabic hides its tabs at the LEFT edge — and deliberately NO
`scroll-snap`: measured, snapping puts a keyboard-focused strip tab 36px outside the
strip's clip box and reds `settings-tabs.spec.ts` RULE 8, so the arm asserts
`scroll-snap-type` stays off. Arm 3 is the twelve payment methods as labelled native
checkboxes with a painted ✓, an accessibility-reported checked state (not an
`aria-checked` custom widget) and Select all / Clear all, with the toggle's own label
flipping to Clear all once every method is ticked and back again when cleared. Arm 5
is the CONTROL that keeps the bar scoped to the phone: at desktop the sticky bar is
gone and the in-flow Save is the one action.

**309 tests in 59 files** on the split's tree (2026-11-10), read with
`cd web && ./node_modules/.bin/playwright test --project=feature-core --list`.

The figure before the split was the whole family: **365 tests in 76 files** (the
`e2e` gate's figure, re-measured 2026-11-10 on `origin/main` `7defcceb2` with
`--project=feature-e2e --list`). 56 + 309 = 365 and 17 + 59 = 76: the split moved
no test, it only put half of them on a second server. The `speccounts` gate re-reads
both figures on every run (`scripts/spec-counts.sh`), and `selftest` check 39 proves
every spec belongs to exactly one project.

The family's earlier history (what grew it from 134 to 365 and which card added
each spec) is in this file's predecessor, the `e2e` gate detail; the figures quoted
there are the WHOLE family's, i.e. this half plus `e2e-builder`'s.
