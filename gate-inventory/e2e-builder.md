# `e2e-builder` — web gate detail

Command (what the table row runs): `cd web && npm run e2e:builder`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the `builder-*.spec.ts` half of the feature end-to-end family (playwright project
`feature-builder`, **56 tests in 17 files**), run by `scripts/preflight.sh` as the
`e2e-builder` gate. The other half — every feature spec whose name does not start
with `builder-` and is not an `a11y*` spec — is the `e2e-core` gate, and the axe
family is `a11y`.

WHY IT IS ITS OWN GATE (card `t_ec2aac42`). The whole feature family was ONE gate
that took 570s of a ~2,700s release run — 21% of it — and the 17 builder specs were
about half of that cost on their own: measured 2026-11-10 in a clean worktree on
this host (load average 23-25, CPUQuota 300%), `builder-*.spec.ts` reads 565s at
`--workers=1` and 303s at `--workers=3`, i.e. 1.86x and not 3x. The ceiling is the
ONE `next start` serving all three workers: a Node server is one process, so more
workers queue on it instead of adding throughput. Two halves on two claimed ports,
run CONCURRENTLY, remove that ceiling — and `a11y` runs alongside them rather than
after them (the three used to serialise on the host-wide suite lock even though
they share no resource but the CPU).

THE COST IS PAGE LOAD, NOT SLEEP. Measured on the same tree: 46 `waitForTimeout`
calls totalling 20.8s across the family, and `signIn` (which 80 of the 93 specs
call) only sets cookies — it does not navigate. What dominates is one page load +
hydration + settle per test, ~7.5s on these specs. That is why the answer to the
cost is fewer servers queueing / fewer specs per run, not deleting sleeps, and why
`--workers` above 3 is NOT chased here: 1.86x at three workers on a six-core host
with a 300% quota says the host is the ceiling, not the worker count.

The specs under this half drive the builder's own behaviours: a save whose fetch
is aborted at the transport level leaves both Save buttons enabled with the failure
sentence (`builder-save-failure.spec.ts`), a publish writes the unedited condition
rule back unchanged (`builder-condition-dangling-target.spec.ts`), the insertion
gap owns every pixel row of the box it paints (`builder-insertion-gap-drop.spec.ts`),
a stored prose value warns where it is edited and publishes byte-identical
(`builder-text-prose-intro.spec.ts`), and so on — see the files under `web/e2e/`.

## History

**56 tests in 17 files** on the split's tree (2026-11-10), read with
`cd web && ./node_modules/.bin/playwright test --project=feature-builder --list`.

The figure before the split was the whole family: **365 tests in 76 files** (the
`e2e` gate's figure, re-measured 2026-11-10 on `origin/main` `7defcceb2` with
`--project=feature-e2e --list`). Splitting it did not change a single test: the two
halves read 56 + 309 = 365 in 17 + 59 = 76 files, which is what the `speccounts`
gate re-reads on every run and what `selftest` check 39 asserts (every spec belongs
to exactly ONE playwright project — measured, because a project rule that matches
nothing is a silent coverage loss and one that matches twice runs the suite twice).

THE FAMILY IS NO LONGER "EVERY `*.spec.ts` UNDER web/e2e/" (card `t_440c9417`). One spec
there is owned by another gate: `menu-checkout-option-group.spec.ts` needs the
`menufixture` Go server (`MENU_FIXTURE_URL`), which these gates' `next start` cannot
serve, so `feature-core` `testIgnore`s it and the `menufixture` gate is its only
runner. THIS half is unchanged — 56 tests in 17 files, same `--list` — but the family's
total is now **373 tests in 78 files** (56 + 317 in 17 + 61). `selftest` check 39 now
judges ownership as `testMatch matches AND testIgnore does not`, and accepts a spec no
project runs ONLY when it is declared in the check's `OWNED_ELSEWHERE` table; that
declaration is verified in both directions, so a spec that matches nothing and is not
declared still fails, and so does one that is declared while a project still runs it.

THE SPLIT'S RULE IS THE FILENAME PREFIX, deliberately: a list of explicit spec paths
would have to be edited every time a spec is added, and a spec in no project simply
stops running. `builder-*.spec.ts` is the builder half by name; `a11y*` is the axe
gate by name; everything else is `e2e-core`.

The `a11y` gate's detail explains why the axe family was separated from these in the
first place (card `t_34fd25cb`); this file exists because card `t_ec2aac42` split
the remaining family in two so a release stops paying for the whole suite.
