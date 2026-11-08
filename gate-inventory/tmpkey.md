# `tmpkey` — helper gate detail

Command (what the table row runs): `deploy/tmp-apikey_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the temp-API-key helper cannot carry a psql tag in the key id, deletes by exact name, and treats a surviving row as a failure
