# `lint` — web gate detail

Command (what the table row runs): `cd web && npm run lint`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

`eslint .` over the whole web tree — the errors only (`--max-warnings` is not set, so the advisory tail stays advisory). Nothing checked the web tree for anything but types and the unit tests until this gate; the tooling is eslint 9 + `eslint-config-next` with a flat config at `web/eslint.config.mjs`. `eslint .` exits non-zero on an error, which is what makes it a gate rather than a report. The React Compiler diagnostics that `eslint-config-next` 16 pulls in are off per-rule with the reason at each line (no compiler is configured here)
