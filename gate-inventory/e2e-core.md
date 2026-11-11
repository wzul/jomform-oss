# `e2e-core` — web gate detail

Command (what the table row runs): `cd web && npm run e2e:core`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

every feature end-to-end spec under `web/e2e/` that is neither an `a11y*` spec (the
`a11y` gate), a `builder-*` spec (the `e2e-builder` gate), nor a spec owned by another
gate (`web/e2e/menu-checkout-option-group.spec.ts`, owned by `menufixture` and
`testIgnore`d here — card `t_440c9417`) — playwright project `feature-core`,
**324 tests in 62 files**: sales, chat, forms, form settings, settings/workspaces,
products, media, templates, deliveries, activity, landing, login, session, profile,
docs and the cross-page presentation specs.

The 63 → 62 FILE and 325 → 324 test drop is this PR (card `t_9efc5ed7`,
wzul/jomform #1708): it deletes `web/e2e/chip-mode-choice.spec.ts` — ONE arm, "a
stored mode renders as the selected choice, and choosing Live saves live" — and that
spec's `web/e2e/areas.tsv` row, because the Test/Live mode control it drove is gone
from the settings CHIP card. That is the only spec and the only area mapping the PR
removes and nothing else moved (the `a11y` family on the same tree reads 217 in 23,
and `e2e-builder` 56 in 17). Measured on the MERGED tree (`origin/main` `11ce49ab6` +
this PR, merge commit `2ba0e541a`) with `cd web && ./node_modules/.bin/playwright test
--project=feature-core --list`. The figure before it was **325 tests in 63 files**
(that figure, and the paragraph below that counts `panduan-surau-shot-scale.spec.ts`
as the 63rd file, are the shape of the family BEFORE this deletion — the file it names
is this family's 62nd now).

The 63rd FILE is `web/e2e/panduan-surau-shot-scale.spec.ts` (card `t_e1ea03a2`): 2 arms on the
nine figures of `/panduan/surau` — at a 390px phone viewport every figure renders at >= 0.45x
its own natural width, edge to edge, and links to the full 1900px capture, which answers 2xx;
at 1280px the figures stay in the article column. Read with `playwright test
--project=feature-core --list` on the tree that merges that card.

The figure before that was **323 tests in 62 files**; before it, **317 tests in 61 files**, which card `t_516bab9d` grew with
`web/e2e/landing-usecases-mobile.spec.ts` — the 62nd FILE, 6 arms on the seven use-case
cards of `/landing` at a real 390px viewport (isMobile/hasTouch/dpr 2): one arm per locale
(en/ms/zh/ta/ar) asserting per card that it renders <= 55 words, a lead <= 20 words, four
bullets of <= 5 words each, a height <= 460px, exactly one filled CTA and one guide link,
no `Read the full … guide` sentence, and that the seven cards are within 10 words of each
other; plus one arm on the block's one-action shape and the 390px no-pan. Measured with
`cd web && ./node_modules/.bin/playwright test --project=feature-core --list`, which read
**317 in 61** on this same tree before that spec existed, so the delta is exactly its 1
file and 6 arms.

THE SPEC THIS PROJECT DOES NOT OWN (card `t_440c9417`, option B: one owner per test).
`web/e2e/menu-checkout-option-group.spec.ts` measures the menu checkout's option-group
geometry against `api/cmd/menu-fixture`, a Go server this gate's `next start` cannot
serve and which the spec finds through `MENU_FIXTURE_URL`. Its owner is the
`menufixture` gate (`scripts/menu-fixture-e2e.sh` + `playwright.menufixture.config.ts`),
which runs all 3 of its arms. Here it used to be enumerated by `feature-core` and then
skip itself at file level ("MENU_FIXTURE_URL is unset"), so this gate reported a skip
it could never turn into a pass — and had `menufixture` stopped running the file,
nothing in this suite would have noticed. Measured on the tree that carries the change
(`playwright test --project=feature-core --list`): **320 tests in 62 files** before,
**317 in 61** after — exactly that file's 3 arms, and no other test moved. The spec's
own file-level skip STAYS: run by hand without the fixture it still has to say why it
measures nothing. What `testIgnore` decides is who ENUMERATES it.

The figure before that was **320 tests in 62 files**, which grew by card
`t_2b2da6a7`, whose `web/e2e/landing-header-mobile.spec.ts` is the 61st FILE and
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
once. That arm's 1 test is what separates the 319-in-61 figure from the 320-in-62 one;
what it holds is under History.

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
- a spec owned by ANOTHER gate has NO row and selects nothing here: a diff that
  touches only `web/e2e/menu-checkout-option-group.spec.ts` decides a subset of ZERO
  and this gate prints NOT RUN. It must never be handed to playwright as a spec path,
  because `feature-core` `testIgnore`s it and playwright would then run nothing while
  the gate reported a pass (card `t_440c9417`; the declaration lives in the matcher's
  `OTHER_GATE_SPECS` and is checked against the tree, in both directions).
- `web/scripts/e2e-scope.mjs` is the MATCHER. Narrowing is only ever allowed on a
  POSITIVE attribution. A changed path the map does not claim, a spec with no row, a
  change to `playwright.config.ts` / `web/e2e/*` helpers / the map itself, an empty
  diff, or a decision that selects ZERO specs all widen the run to the WHOLE family.
  The ONE positively-attributed zero is a path the TREE derives to a route no feature
  spec names (rule 6a, card `t_902bd1f7`): a new page, a screenshot under
  `web/public/`, a module only such a page imports, or a `web/scripts/` file no npm
  script in the build/start chain runs. The chain is `asset -> the page that names
  its URL -> that page's route -> the specs whose own text names the route`, and
  none means NOT RUN. It widens whenever the route is named by code serving a
  DIFFERENT route, by an e2e helper, or cannot be spelled statically. `--check-map`
  and `--self-test` (both run by the `speccounts` gate) re-read those rules against
  the tree, including the bite arm that removes a spec's route literal and watches
  that spec leave the selection.
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
