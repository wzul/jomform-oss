# `webimage` — web gate detail

Command (what the table row runs): `docker build -f web/Dockerfile .` (cache-only)

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the **shipping web artifact** — no other gate built it, so a tree could be green while `docker build -f web/Dockerfile .` died
