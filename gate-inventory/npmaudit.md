# `npmaudit` — web gate detail

Command (what the table row runs): `cd web && npm run audit:gate`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

high/critical npm advisories — `npm audit --audit-level=high` PLUS a narrow, self-expiring exception list (`web/npm-audit-exceptions.json`, read by `web/scripts/npm-audit-gate.mjs`). An advisory NOT named in that file fails here with npm's own report; an exception with no `expires` is refused, an expired one fails, and a no-longer-reported one is printed as STALE. The exception list is currently EMPTY, and that is the healthy state: no high/critical advisory is reported at all. The last entry — `braces@3.0.3`, GHSA-vfj7-8cjw-p6xm, reached only through the DEV-ONLY eslint toolchain, with NO published fixed release (`npm view braces@3.0.4` is a 404; npm's only suggestion was a downgrade of `eslint-config-next`) — was retired when GitHub WITHDREW that advisory upstream on 2026-10-23; `braces` is still 3.0.3, so nothing was upgraded and there is nothing left to except (card `t_6cc165c6`)
