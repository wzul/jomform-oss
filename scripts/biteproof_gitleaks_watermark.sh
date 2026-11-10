#!/usr/bin/env bash
# biteproof_gitleaks_watermark.sh — proves the `gitleaks` gate's WATERMARK cannot
# be used to hide an old leak, and that the weekly FULL sweep is the thing that
# closes the window it leaves.
#
# THE DESIGN UNDER PROOF (card t_cf0f6063): `scripts/gitleaks_scan.sh` scans
# `<watermark>..HEAD` (measured 1-2s) instead of every ref over the whole history
# (measured 50-116s and growing), advances the watermark on SUCCESS ONLY, and
# falls back to a FULL sweep — the only shape that also covers non-HEAD refs —
# weekly and whenever the watermark cannot be trusted.
#
# WHY A HARNESS AND NOT A GATE. A bite proof is an on-demand instrument
# (BITE-HARNESSES.md): one case costs a scanner build plus a history-shaped scan,
# so the per-merge gates carry the cheap half instead (the gate itself prints its
# mode on every run, and `selftest` pins the gate's place in the always-run
# class). This proves the BEHAVIOUR, on a synthetic repo whose input shape is the
# one that matters: a secret committed, then deleted.
#
# WHAT IT PROVES, and why each leg is needed:
#   0  CONTROL. No watermark -> FULL sweep, GREEN, and the state file is written
#      (commit + full_at), so every leg below runs against a real state file
#      rather than a story about one.
#   1  INCREMENTAL REPORTS. At the commit that ADDS the secret, the run is
#      incremental since the clean commit and REDS, naming the file.
#   2  THE DELETION DOES NOT HIDE IT. At the commit that DELETES the file, the
#      incremental run still REDS — because leg 1 did not advance the watermark.
#   3  THE WINDOW, MEASURED. With the watermark hand-set to the secret's commit
#      (the state the gate REFUSES to write) the incremental run on the same tree
#      is GREEN with 0 commits scanned: an incremental-only design really can
#      hide a deleted secret. This leg is the reason the sweep exists.
#   4  THE SWEEP CLOSES IT. On the SAME tree as leg 3, a FULL sweep REDS on the
#      deleted secret. Legs 3 and 4 differ only in the mode, so the pair is this
#      instrument's own vacuity control for the sweep.
#   5  A NON-ANCESTOR WATERMARK FORCES A FULL SWEEP (force-push/rebase), instead
#      of scanning a range that excludes commits it never covered.
#   6  SHALLOW CLONE IS REFUSED, and the refusal is not readable as a clean scan.
#   7  A GREEN INCREMENTAL IS NOT VACUOUS: over a range holding one clean commit
#      it reports "1 commits scanned" (leg 3's green reported 0), so a green here
#      cannot come from a scan that read nothing.
#
# WHAT IT DOES NOT PROVE: it does not re-read this repository's own history, and
# it does not prove the gate's place in the inventory (that is `selftest`).
#
# NOTHING UNDER THIS REPOSITORY IS MUTATED: the probe repos and the watermark
# store live in one `mktemp -d` removed by the EXIT trap, and the shipped config
# plus the worktree status are checked before and after.
#
# Usage:  scripts/biteproof_gitleaks_watermark.sh
#   GITLEAKS_SCANNER_BIN=<binary>  use this scanner instead of `go run …@latest`
#   BITEPROOF_KEEP=1               keep the temp dir and print its path
#
# Exit: 0 every case bit; 1 a case DID NOT BITE; 2 HARNESS BUG.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
CFG="$ROOT/.gitleaks.toml"
SCAN="$ROOT/scripts/gitleaks_scan.sh"

# The fixture value is ASSEMBLED, never written as one literal: this file is a
# non-test path in the scanned history, so a token-shaped literal here would make
# the harness itself the next false positive.
#
# The HALVES must not be NAMED after a rule keyword either: `generic-api-key`
# reads a `*secret*`-named variable assigned a high-entropy hex literal as a
# keyword+value pair on its own, which is how this file became a finding even
# though neither half is a credential. Only the ASSEMBLED value below is written
# into the probe repo as `api_key = "…"`, so only it has to look like a secret.
# Keep these two names keyword-free.
HEX_A="9f3a2b7c1d4e"
HEX_B="5f60718293a4b5c6d7e8"
SECRET_VALUE="${HEX_A}${HEX_B}"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/biteproof-gitleaks-wm.XXXXXX")"
cleanup() {
  if [ "${BITEPROOF_KEEP:-0}" = "1" ]; then echo "kept: $TMP"; else rm -rf "$TMP"; fi
}
trap cleanup EXIT

REPO="$TMP/repo"
STATE="$TMP/state"
SHALLOW="$TMP/shallow"
mkdir -p "$REPO" "$STATE"

CASES=0
BITES=0
BUGS=0

bug() { echo "HARNESS BUG: $*" >&2; BUGS=$((BUGS + 1)); }
pass_case() { CASES=$((CASES + 1)); BITES=$((BITES + 1)); echo "  ok   $1"; }
fail_case() { CASES=$((CASES + 1)); echo "  ❌ DID NOT BITE — $1" >&2; }

gitc() { git -C "$REPO" -c user.name=biteproof -c user.email=biteproof@example.invalid "$@"; }

# scan TAG [ENV=VAL ...] — run the SHIPPED driver against $REPO, capture the log,
# echo its exit status. Nothing about the driver is reimplemented here: every
# verdict below is the gate's own bytes making the decision.
scan() {
  local tag="$1"; shift
  ( cd "$REPO" && env GITLEAKS_WATERMARK="$STATE/gitleaks-watermark" "$@" \
      bash "$SCAN" "$REPO" ) >"$TMP/$tag.log" 2>&1
  echo $?
}

# state_get KEY — one field of the watermark file the driver wrote.
state_get() {
  local f="$STATE/gitleaks-watermark"
  [ -r "$f" ] || return 0
  awk -v k="$1" 'index($0, k "=") == 1 { print substr($0, length(k) + 2); exit }' "$f" 2>/dev/null
}

# set_state COMMIT FULL_AT — rewrite the watermark file keeping the driver's own
# repokey (a hand-set state with the wrong repokey would force a full sweep for
# the wrong reason and the leg would measure nothing).
#
# THE REPOKEY IS READ BEFORE THE REDIRECTION IS OPENED: `{ ... } > file` truncates
# the file before the block runs, so a `$(state_get repokey)` INSIDE it reads an
# empty file and writes `repokey=`. That is not hypothetical — it was this
# harness's first shape, and every hand-set leg then measured the driver's
# "watermark file is unreadable" branch instead of the branch under test.
set_state() {
  local commit="$1" full_at="$2" rk
  rk="$(state_get repokey)"
  [ -n "$rk" ] || bug "set_state: the driver's state file carries no repokey, so a hand-set watermark would read as another checkout"
  {
    echo "format=1"
    echo "repokey=$rk"
    echo "commit=$commit"
    echo "at=$(date +%s)"
    echo "full_at=$full_at"
    echo "mode=hand-set"
    echo "commits=0"
  } > "$STATE/gitleaks-watermark"
}

# mode_of TAG — the driver's OWN mode line, or empty. One reading for every leg,
# and self-checked below so a reader that answered a constant cannot pass.
mode_of() {
  local line
  line="$(grep -m1 '^gitleaks: ' "$TMP/$1.log" 2>/dev/null)"
  case "$line" in
    "gitleaks: full sweep:"*) echo full ;;
    "gitleaks: incremental:"*) echo incremental ;;
    *) echo "" ;;
  esac
}

# ran TAG — the scanner's own positive evidence that it walked something.
ran() { grep -q 'commits scanned' "$TMP/$1.log" 2>/dev/null; }
# saw_leaks TAG — the scanner's red verdict line (not just a non-zero rc).
saw_leaks() { grep -q 'leaks found: [1-9]' "$TMP/$1.log" 2>/dev/null; }
# saw_no_leaks TAG — the scanner's green verdict line.
saw_no_leaks() { grep -q 'no leaks found' "$TMP/$1.log" 2>/dev/null; }
# names_file TAG FILE — the finding names the probe file.
names_file() { grep -q "$2" "$TMP/$1.log" 2>/dev/null; }

echo "== biteproof: gitleaks watermark (incremental + full sweep) =="
echo "driver: $SCAN"
echo "config: $CFG"

if [ ! -x "$SCAN" ] && [ ! -f "$SCAN" ]; then
  bug "the gate's driver is missing at $SCAN — nothing below can measure the shipped behaviour"
fi
CFG_SHA_BEFORE="$(sha256sum "$CFG" | cut -d' ' -f1)"
WT_BEFORE="$(git -C "$ROOT" status --porcelain)"

# ---- the probe repo. It is built INCREMENTALLY, and that is load-bearing: the
# control's FULL sweep reads EVERY REF, so the secret must not exist yet when case
# 0 runs — otherwise the control is red for the right reason and measures nothing.
# Here: one clean commit, and a SIBLING branch for case 5's non-ancestor watermark.
git -C "$REPO" init -q -b main
cp "$CFG" "$REPO/.gitleaks.toml"
printf 'harmless\n' > "$REPO/README.md"
gitc add -A && gitc commit -qm "A: clean"
A="$(gitc rev-parse HEAD)"
gitc checkout -q -b side "$A"
printf 'side\n' > "$REPO/side.txt"
gitc add -A && gitc commit -qm "S: side branch"
S="$(gitc rev-parse HEAD)"
gitc checkout -q main

if [ -z "$A" ] || [ -z "$S" ]; then
  bug "the probe repo was not built (A=$A S=$S)"
  echo "HARNESS BUG — no verdict" >&2
  exit 2
fi
echo "probe: A=${A:0:12} S=${S:0:12} (B, C and D are committed by the cases that need them)"

# --- case 0 — CONTROL: no watermark -> full sweep, green, state written
gitc checkout -q "$A"
rc0="$(scan c0)"
if [ "$rc0" = "0" ] && [ "$(mode_of c0)" = "full" ] && saw_no_leaks c0 && ran c0 \
  && [ -n "$(state_get commit)" ] && [ "$(state_get mode)" = "full" ] \
  && [[ "$(state_get full_at)" =~ ^[0-9]+$ ]]; then
  pass_case "case 0 (control): no watermark -> FULL sweep, GREEN, and the state file records commit ${A:0:12} + a full-sweep time"
else
  fail_case "case 0: expected a green full sweep and a written state file — rc=$rc0, mode=[$(mode_of c0)], state=[$(cat "$STATE/gitleaks-watermark" 2>/dev/null | tr '\n' ' ')]"
fi
# the state the driver wrote is what every leg below stands on; pin its commit.
if [ "$(state_get commit)" != "$A" ]; then
  bug "after case 0 the watermark is [$(state_get commit)], expected the scanned commit $A — later legs would measure a different window"
fi
FULL_AT_KEPT="$(state_get full_at)"

# ---- the secret is ADDED here, after the control's full sweep, so case 0 could
# not have seen it: `detect --source .` reads every ref, so a secret committed
# before the control would be found there and the control would not be green.
gitc checkout -q main
printf 'api_key = "%s"\n' "$SECRET_VALUE" > "$REPO/config.sh"
gitc add -A && gitc commit -qm "B: adds the secret-shaped fixture"
B="$(gitc rev-parse HEAD)"

# --- case 1 — the commit that ADDS the secret: incremental, RED, watermark NOT advanced
gitc checkout -q "$B"
rc1="$(scan c1)"
if [ "$rc1" != "0" ] && [ "$(mode_of c1)" = "incremental" ] && saw_leaks c1 && names_file c1 "config.sh"; then
  if [ "$(state_get commit)" = "$A" ]; then
    pass_case "case 1 (at the secret's commit): INCREMENTAL since ${A:0:12} REDS, naming config.sh, and the watermark was NOT advanced"
  else
    fail_case "case 1: the red run advanced the watermark to $(state_get commit) — a later deletion could then hide the leak"
  fi
else
  fail_case "case 1: expected an incremental RED naming config.sh — rc=$rc1, mode=[$(mode_of c1)]"
fi

# ---- the file is DELETED now: the deletion commit IS this case's subject.
gitc checkout -q main
gitc rm -q config.sh && gitc commit -qm "C: deletes it"
C="$(gitc rev-parse HEAD)"

# --- case 2 — the DELETION must not hide it
gitc checkout -q "$C"
rc2="$(scan c2)"
if [ "$rc2" != "0" ] && [ "$(mode_of c2)" = "incremental" ] && names_file c2 "config.sh"; then
  pass_case "case 2 (after the file is DELETED): the incremental run STILL reds on the deleted secret — the watermark did not move past it"
else
  fail_case "case 2: the deletion hid the leak — rc=$rc2, mode=[$(mode_of c2)] (an incremental-only design that advanced the watermark would answer green here)"
fi

# --- case 3 — THE WINDOW, measured: hand-set the watermark to the secret's commit
set_state "$B" "$FULL_AT_KEPT"
rc3="$(scan c3)"
if [ "$rc3" = "0" ] && [ "$(mode_of c3)" = "incremental" ] && saw_no_leaks c3 \
  && grep -q '0 commits scanned' "$TMP/c3.log"; then
  pass_case "case 3 (watermark hand-set to the secret's commit): incremental B..C is GREEN at 0 commits scanned — this is the window the sweep exists to close"
else
  fail_case "case 3: expected a green incremental over an empty range (the window) — rc=$rc3, mode=[$(mode_of c3)]"
fi

# --- case 4 — the FULL sweep on the SAME tree catches it
rc4="$(scan c4 GITLEAKS_FORCE_FULL=1)"
if [ "$rc4" != "0" ] && [ "$(mode_of c4)" = "full" ] && saw_leaks c4 && names_file c4 "config.sh"; then
  pass_case "case 4 (same tree as case 3, FULL sweep): REDS on the secret that case 3 could not see — the sweep is what stops the watermark hiding an old leak"
else
  fail_case "case 4: the full sweep did not report the deleted secret — rc=$rc4, mode=[$(mode_of c4)]"
fi

# --- case 5 — a watermark that is not an ancestor of HEAD forces a full sweep
set_state "$S" "$FULL_AT_KEPT"
gitc checkout -q "$C"
rc5="$(scan c5)"
if [ "$(mode_of c5)" = "full" ] && grep -q 'not an ancestor of HEAD' "$TMP/c5.log"; then
  pass_case "case 5 (watermark on a sibling branch): forces a FULL sweep and says why — a force-push/rebase cannot shrink the range to one nothing ever covered"
else
  fail_case "case 5: a non-ancestor watermark did not force a full sweep — rc=$rc5, mode=[$(mode_of c5)]"
fi

# --- case 6 — shallow clone is refused, and cannot read as clean
gitc checkout -q main
git clone -q --depth 1 "file://$REPO" "$SHALLOW"
if [ -d "$SHALLOW/.git" ]; then
  ( cd "$SHALLOW" && env GITLEAKS_WATERMARK="$TMP/shallow-state/gitleaks-watermark" \
      bash "$SCAN" "$SHALLOW" ) >"$TMP/c6.log" 2>&1
  rc6=$?
  if [ "$rc6" != "0" ] && grep -q 'SHALLOW clone' "$TMP/c6.log" && ! grep -q 'commits scanned' "$TMP/c6.log"; then
    pass_case "case 6 (shallow clone): REFUSED with the unshallow instruction and NO scan line — a shallow history cannot answer the question and must never read as a clean scan"
  else
    fail_case "case 6: the shallow clone was not refused properly — rc=$rc6"
  fi
else
  bug "case 6: 'git clone --depth 1 file://…' produced no clone, so the shallow leg measured nothing"
fi

# ---- one more CLEAN commit, so the green direction has a range with real content
gitc checkout -q main
printf 'nothing to see\n' > "$REPO/notes.md"
gitc add -A && gitc commit -qm "D: clean"
D="$(gitc rev-parse HEAD)"

# --- case 7 — a green incremental is not vacuous
set_state "$C" "$FULL_AT_KEPT"
gitc checkout -q "$D"
rc7="$(scan c7)"
if [ "$rc7" = "0" ] && [ "$(mode_of c7)" = "incremental" ] && saw_no_leaks c7 \
  && grep -q '1 commits scanned' "$TMP/c7.log" && [ "$(state_get commit)" = "$D" ]; then
  pass_case "case 7 (one clean commit since the watermark): GREEN and the scanner reports 1 commit scanned — a green incremental reads a real range, unlike case 3's 0"
else
  fail_case "case 7: expected a green incremental over exactly one commit, advancing the watermark to ${D:0:12} — rc=$rc7, mode=[$(mode_of c7)], scanned=$(grep -o '[0-9]* commits scanned' "$TMP/c7.log" 2>/dev/null), wm=$(state_get commit)"
fi

# --- case 8 — the verdict READER must discriminate, on these REAL logs
if [ "$(mode_of c0)" = "full" ] && [ "$(mode_of c1)" = "incremental" ] \
  && [ "$(mode_of c4)" = "full" ] && [ -z "$(mode_of missing)" ]; then
  pass_case "reader self-check: the mode reader answers full / incremental / full on cases 0, 1 and 4 and EMPTY on a log that does not exist — it is not a constant"
else
  fail_case "reader self-check: the mode reader does not discriminate ([$(mode_of c0)]/[$mode_of c1])"
fi

# ---- the repository must be exactly as it was
CFG_SHA_AFTER="$(sha256sum "$CFG" | cut -d' ' -f1)"
WT_AFTER="$(git -C "$ROOT" status --porcelain)"
if [ "$CFG_SHA_BEFORE" = "$CFG_SHA_AFTER" ] && [ "$WT_BEFORE" = "$WT_AFTER" ]; then
  echo "  ok   the repository is unchanged (config sha256 ${CFG_SHA_BEFORE:0:12}, worktree status identical)"
else
  bug "the repository changed during the run ($CFG_SHA_BEFORE -> $CFG_SHA_AFTER; worktree status differs)"
fi

echo
echo "CASES: $CASES   BITES: $BITES"
if [ "$BUGS" != "0" ]; then
  echo "RESULT: HARNESS BUG — exit 2" >&2
  exit 2
fi
if [ "$BITES" = "$CASES" ]; then
  echo "RESULT: ALL CASES BIT — exit 0"
  exit 0
fi
echo "RESULT: A CASE DID NOT BITE — exit 1" >&2
exit 1
