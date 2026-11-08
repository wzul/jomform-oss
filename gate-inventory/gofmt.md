# `gofmt` — api gate detail

Command (what the table row runs): `cd api && test -z "$(gofmt -l .)"`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

files that are not gofmt-clean — a diff nobody reviewed, and the class two REAL defects (a docs paragraph read as a `#` heading, a whole test body left at column 0) hid in until cards `t_40bc6f95` / `t_d60f987b`. `go vet` does not check formatting, so nothing did
