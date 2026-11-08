# `backup` — suite gate detail

Command (what the table row runs): `deploy/backup/jomform-backup_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the nightly backup's own failure-path suite: a copy that "exits 0" having landed nothing, a same-size object whose bytes differ, an empty dump archive, a remote with no hash, a restore drill that must not overwrite the backup job's status, and the fail-closed override seam
