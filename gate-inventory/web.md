# `web` — web gate detail

Command (what the table row runs): `cd web && npm run build`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

`test:unit` (incl. the cross-language Go-contract guards) then the Next build. The gate also REFUSES before running when the shell's node cannot run it (card `t_f96c7a44`): `test:unit` is `node --test … src/lib/*.test.ts`, so it needs node with TYPE STRIPPING ON BY DEFAULT (22.18.0+; the 23 line 23.6.0+ — 22.6–22.17 only behind `--experimental-strip-types`, which the script does not pass), while every OTHER web tool's floor is lower (playwright refuses < 20, next 16 declares `>=20.9.0`, eslint 9 accepts `^20.9.0`). So node 20 is a version a lane can legitimately land on and it still reds the SAME gate, seconds in, with `TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown file extension ".ts"` on every test file — a version floor reported in the shape of a code defect (measured on the agent host 2026-10-24: v20.20.2 → 99 tests / 0 pass / 99 fail; v18.19.1 → `node: bad option: --disable-warning=…` before a test runs; re-measured 2026-10-26 on card `t_f96c7a44`'s own tree: v20.20.2 → 124 tests / 0 pass / 124 fail, v22.23.3 → 1004/1004, and v24.19.0 → `web` PASS 88s at 1004/1004 — which is why the refusal text names no test count, the suite's size moves and the gate must not). The refusal names both floors, the node found and a PATH fix, `web/package.json` declares the same range in `engines.node`, and `selftest` check 5d pins it in six legs (just-below-floor, the v20 trap, v18, a node AT the floor NOT refused, an a11y-only run NOT refused, and a sabotage leg that strips the block). An `a11y`-only run uses the lower playwright floor, since it never runs `test:unit`

## The design-system meta-test (card `t_46ae7ad7`)

`web/src/lib/designTokens.test.ts` runs inside `test:unit`, so it is this gate that
fails a design-system regression — `npm run build` starts with `test:unit`, which is
why the meta-test cannot be skipped by building instead of testing. It is the
complement of the `design` gate, not a duplicate of it: `scripts/design-guard.py`
scans the tree for a literal at a call site and proves its own bite per run, while
this file owns the rules that need the TOKEN DATA to decide, and that no Python
scanner can express:

- **a `--jf-*` declared outside `app/globals.css`** (two declaration sites are two
  token layers);
- **an arbitrary `[<n>px]` where a scale step already names `<n>`** — 44px is
  `--jf-space-11`, so `min-h-[44px]` is `min-h-jf-11`. The step set is read from
  `designTokenScale.ts`, so adding a step widens the rule in the same commit, and a
  length no step names (`w-[720px]`) is outside it by construction;
- **a raw Tailwind palette colour utility** — `bg-indigo-600`, `text-slate-500`,
  `border-emerald-200`, `bg-white/95` (`scanPalette`, card `t_2fb8ae1b`, landed once the
  six worst pages had consumed the vocabulary). It reports the seven colour prefixes
  Tailwind spells `<prefix>-<family>-<step>` (`bg`, `text`, `border`, `ring`, `divide`,
  `fill`, `stroke`, with an optional `/<alpha>`) plus `bg-white` / `text-white` /
  `bg-black`; it does NOT report `bg-[var(--jf-…)]` (the replacement — a control plants
  both on one line and requires the finding for only the first), and it honours
  `design-tokens-exempt:` exactly as the hex rule does. Same shrink-only allowlist,
  seeded at **793 findings in 47 files** on the tree that landed it (re-measured there:
  `components/FormModeControl.tsx` fell 8 → 1 when card `t_cff48545` tokenised its last
  seven palette utilities, so the seed followed the tree down rather than shipping at 800;
  the six migrated pages carry 1 between them, against 601 of the 1,449 before), printed as
  `design-tokens: raw palette: N finding(s) in M file(s), K allowlisted (seeded S)`;
- **the shrink-only allowlist** (`web/src/lib/designTokenAllowlist.ts`): a per-file
  COUNT seeded from the tree it was measured on. Unlisted findings fail, a count above
  the seed fails, and an entry whose file is now clean fails until it is deleted. The
  totals are printed (`design-tokens: arbitrary px: N finding(s) in M file(s), K
  allowlisted (seeded S)`) so the shrink is visible in this gate's output. The colour
  list is empty and asserted empty — the end state — and the px list is seeded at 7
  `min-h-[44px]` occurrences across 4 files for the migration card to empty;
- **every colour token has a measured pair, and every printed ratio is recomputed
  from `globals.css`** on every run (`LIGHT_CONTRASTS`, `DARK_CONTRASTS`), so a
  documentation page cannot quote a ratio the app no longer paints;
- **the pairings that FAIL as text** (`FORBIDDEN_TEXT_PAIRS`) are held to failing
  their floor on their real surface, with a replacement token that must PASS on that
  same surface — the check that `--jf-primary` (4.70:1 on white) is wrong as text on
  `--jf-surface-2` (4.20:1) and that `--jf-primary-strong` is the right answer there;
- **the token groups** (type scale with paired line-heights, the 12-step 4px grid, the
  radius ladder, three elevations re-tuned for dark, and a dark pair for every colour
  role except the two documented theme-invariant families);
- **the primitive layer's completeness**: every `components/ui` export is literal-free,
  is a `forwardRef` component, is shown at `/design`, and has a state-matrix entry
  (`web/src/lib/designStates.ts`) answering all six states — a state it has renders a
  specimen, a state it does not have prints a reason of 12+ characters. The `/design`
  page's specimen type is derived from that table, so an applicable cell with no
  specimen is a `tsc` error rather than something a reader has to notice.

Run it alone with `cd web && npm run test:unit` (it is one file among many; the full
suite is what this gate runs).
