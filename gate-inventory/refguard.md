# `refguard` — refs gate detail

Command (what the table row runs): `scripts/refguard_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the hand-made `refs/remotes/<non-remote>/<N>` mirror guard BITES — the documented GitHub idiom is refused in the primary checkout and in a linked worktree, deletion stays allowed, a second remote's namespace stays allowed; a hook that is missing, non-executable or stale is reported NOT ARMED instead of quietly guarding nothing; and the BOUNDARY of the rule is asserted rather than assumed — a writer that opens no reference transaction (`git symbolic-ref`, measured 0 hook invocations) cannot be refused by the hook at all, so `scan` is its detector and the guard's own header must state that, which is the row that fails on a guard whose header over-reads the hook's reach; and the DELETION forms are asserted the same way — only a non-dereferencing one (`git symbolic-ref -d`, `git update-ref --no-deref -d`) leaves the symref file ABSENT with its target INTACT, while plain `git update-ref -d <symref>` exits 0 having DESTROYED the target ref it pointed at and left the symref on disk reading as CLEAN
