# `mediaprobe` — suite gate detail

Command (what the table row runs): `scripts/media-purge-probe_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the media purge probe's VERDICT, against a stub origin+edge on loopback (python3, port 0, no network, ~35s). It exists because the probe shipped a FALSE FAIL that failed a healthy stack: a CDN may REWRITE the client-facing `cache-control` (Cloudflare's Browser Cache TTL — 14400s) while the shared edge entry still expires on the origin's 300s, so reading that header as "the edge lifetime" reported STALE on every purge taken without a CDN credential (`edge: not_configured`, a supported mode). Pinned in BOTH directions: a rewritten 14400 header over a 3s edge entry is PASS and is named as a rewrite, not as staleness; probe-only mode (no `--purge`) with a live object is PASS instead of "the purge did not run"; an object that is really gone at origin+edge is PASS; a pre-fix entry (`age=10577` under an origin that now declares 300s) stays FAIL naming the age; an origin that declares 86400 stays FAIL at window 300; a purge that leaves the object at the origin is FAIL; a copy with no readable lifetime anywhere is UNDECIDED (exit 2, never a false PASS/FAIL) unless `--await-expiry` measures it; and the card's control — an edge that PINS a fresh 3600s entry while the origin declares 2s, which no header can reveal — is FAIL by MEASUREMENT (`--await-expiry` observes it still serving past the window), with the healthy twin (an edge that really expires) PASS as an observation. Transport failure is exit 2, and the usage errors (`--window 0`, `--purge` without `--token`, missing `--path`) are exit 2 before any request. `PROBE_UNDER_TEST=<path>` runs the same suite against another copy: against the pre-fix script 21 of 35 checks are red, and the false-FAIL scenarios are red on their exit codes
