# `speccounts` — web gate detail

Command (what the table row runs): `bash scripts/spec-counts.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the spec counts the `a11y`, `e2e-builder` and `e2e-core` rows above quote, RE-READ from the tree instead of trusted: it runs `cd web && ./node_modules/.bin/playwright test --project=<project> --list` (`a11y`, `feature-builder` and `feature-core`, in turn — no build, no server, no browser, ~5s), parses playwright's own `Total: N tests in M files`, and compares BOTH numbers against the CURRENT figure in each gate's DETAIL file (`gate-inventory/<gate>.md`; the row is still required to exist). It also asserts `web/e2e/areas.tsv` maps every feature spec (`web/scripts/e2e-scope.mjs --check-map`), so the narrowed run the map enables cannot rot into "always full" without saying so. It exists because those figures were typed and nobody re-read them: `selftest` parses this table for gate NAMES only, so a row could say any number at all and stay green, and it did — nine cards paid a lane run to correct one figure each (`t_9500edf3`, `t_df0857c6`, `t_07eb69c8`, `t_44c9a306`, `t_769a5cb9`, `t_c33effe4`, `t_212be8da`, `t_3839995c`, `t_c5c7f37a`), and on `origin/main` `0aa385c0` — the tree this gate was written on — BOTH rows were stale AGAIN at once (147 tests in 16 files vs 159 in 17; 274 in 54 vs 276 in 55) while every gate in the set passed. Fails CLOSED, never skips: a missing row, a row carrying no `**N tests in M files**` figure, a missing `web/node_modules/.bin/playwright`, and `--list` output with no `Total:` line are each RED with the reason printed. The rows' contract it relies on: the CURRENT figure is the FIRST `**N tests in M files**` span in the row and everything after it is history, so put the new number first and the old one after it. What it does NOT check: a row's quoted "gate's own summary" line (`2 skipped / 274 passed`) — that needs the suite to RUN, so the `a11y`/`e2e` gates print it and this gate does not pretend to have read it
