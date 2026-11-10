# `gitleaks` — secrets gate detail

Command (what the table row runs): `bash scripts/gitleaks_scan.sh` — the gate's
scanner driver. Each run is either an **incremental** scan of
`<watermark>..HEAD` or a **full sweep** across every ref, and it prints which.

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

committed secrets (repo may go public) — including ones committed long ago and
deleted later, which are still in the packfile and still fetchable.

## Why the scan is incremental, and why it is not capped

`gitleaks detect --source .` walks **every ref**, not just `HEAD`. Measured on
`origin/main` (`7defcceb2`): **4,091 commits / 101.16 MB / 115.7 s** cold and
**50.2 s** with the module cache warm, on every gate run — and the cost grows
with history forever. The cheap shape is a range: `--log-opts=<a>..<b>` measured
**1.6 s** on the same tree.

Capping the scan at the last N commits was rejected. The case this gate exists
for is a secret committed long ago and "deleted" later: the blob is still in the
packfile, so a fixed window stops covering exactly the commits that matter. The
answer is incremental scanning, not amnesia.

## How it works

- **Watermark.** `${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/gitleaks-watermark`
  (the same state directory as the per-tree gate records) holds one `key=value`
  file: `format`, `repokey` (the git common dir, so a watermark can never vouch
  for another checkout), `commit`, `at`, `full_at`, `mode`, `commits`.
- **Incremental run.** `gitleaks detect --source . --log-opts=<watermark>..HEAD`.
  The watermark advances **on success only**, so a run that finds a leak keeps
  rescanning the same range and a later commit that deletes the file cannot hide
  it.
- **Full sweep.** Runs whenever the watermark cannot be trusted — absent or
  unreadable, written by another checkout, missing from this clone (fresh or
  partial clone), not an ancestor of `HEAD` (force-push/rebase), no recorded
  sweep, or the last sweep older than `GITLEAKS_FULL_SWEEP_SECONDS` (default
  604800 = 7 days) — and via `GITLEAKS_FORCE_FULL=1`. It is also the only shape
  that covers non-`HEAD` refs, so it must never be dropped.
- **Shallow clone.** Refused loudly, before any scan: a shallow checkout cannot
  answer a history question and would report "no leaks found" from a handful of
  commits.
- **Mode is printed** every run (`gitleaks: full sweep: N commits across all
  refs …` / `gitleaks: incremental: N commits since <sha> …`), so an incremental
  pass can never be read as a full scan — the same discipline as the preflight's
  full/scoped/reused labels.
- **Host state never reds the gate.** An unwritable state directory only stops
  the watermark advancing (announced), which makes every run a full sweep.

The watermark recorded is `merge-base(HEAD, main)`, not `HEAD`: the gate runs in
per-card worktrees and merge-train branches that are *siblings*, so recording
`HEAD` would make every next tree see a non-ancestor watermark and force the full
115 s path — the defect, not the fix. Soundness does not depend on that choice:
`<wm>..HEAD` covers `ancestry(HEAD)` minus `ancestry(<wm>)`, and `ancestry(<wm>)`
was covered by the runs that set it, so every ancestor of the recorded commit is
covered.

## What proves it

- `scripts/biteproof_gitleaks_watermark.sh` (on demand, **not** a gate) — eight
  cases on a synthetic repo whose shape is a secret committed then deleted: the
  incremental run reds on both, the watermark does not advance on a red run, a
  hand-set watermark past the leak leaves a green window, and the **full sweep
  on the same tree reds on the deleted secret** — plus the non-ancestor fallback,
  the shallow-clone refusal and a non-vacuous green incremental.
- `selftest` check 11 / 11b pin the config (`[extend] useDefault` and the
  allowlist scoping), and the `'*'` (cannot-narrow) class keeps the gate in every
  narrowed run: the gate reads the whole history, which no diff path list can
  express.
