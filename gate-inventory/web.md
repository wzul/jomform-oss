# `web` — web gate detail

Command (what the table row runs): `cd web && npm run build`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

`test:unit` (incl. the cross-language Go-contract guards) then the Next build. The gate also REFUSES before running when the shell's node cannot run it (card `t_f96c7a44`): `test:unit` is `node --test … src/lib/*.test.ts`, so it needs node with TYPE STRIPPING ON BY DEFAULT (22.18.0+; the 23 line 23.6.0+ — 22.6–22.17 only behind `--experimental-strip-types`, which the script does not pass), while every OTHER web tool's floor is lower (playwright refuses < 20, next 16 declares `>=20.9.0`, eslint 9 accepts `^20.9.0`). So node 20 is a version a lane can legitimately land on and it still reds the SAME gate, seconds in, with `TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown file extension ".ts"` on every test file — a version floor reported in the shape of a code defect (measured on the agent host 2026-10-24: v20.20.2 → 99 tests / 0 pass / 99 fail; v18.19.1 → `node: bad option: --disable-warning=…` before a test runs; re-measured 2026-10-26 on card `t_f96c7a44`'s own tree: v20.20.2 → 124 tests / 0 pass / 124 fail, v22.23.3 → 1004/1004, and v24.19.0 → `web` PASS 88s at 1004/1004 — which is why the refusal text names no test count, the suite's size moves and the gate must not). The refusal names both floors, the node found and a PATH fix, `web/package.json` declares the same range in `engines.node`, and `selftest` check 5d pins it in six legs (just-below-floor, the v20 trap, v18, a node AT the floor NOT refused, an a11y-only run NOT refused, and a sabotage leg that strips the block). An `a11y`-only run uses the lower playwright floor, since it never runs `test:unit`
