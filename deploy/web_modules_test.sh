#!/usr/bin/env bash
#
# deploy/web_modules_test.sh — the seeder's own suite.
#
# THE DEFECT IT PINS (card t_eaf5edd4, measured on the merge train of 2026-11-12
# 14:31 for PR #1733). wm_copy_from seeded a candidate with
# `cp -a "$src" "$wt/web/node_modules"`, and `cp -a` reproduces a SYMLINK as a
# symlink. When the DONOR's web/node_modules was a link — many long-lived
# worktrees on this host point theirs at /root/wzul-git/jomform/web/node_modules —
# the candidate was left with a LINK, nothing errored, `[ -x .bin/next ]` followed
# the link and agreed the tree was installed, and three minutes later a guard that
# WALKS the tree died:
#
#   model_default_guard_test.go: walk <candidate> for model ids:
#     read <candidate>/web/node_modules: is a directory
#
# `filepath.Walk` lstats each entry, so a symlink is never a directory: the
# skipDirs arm for `node_modules` never applies and ReadFile hits EISDIR. The
# batch went GREEN the moment TRAIN_NODE_MODULES named a REAL donor, so the
# seeding path was the cause and the tree was innocent — which is the reason this
# suite tests the SEEDER and not the guard.
#
# WHAT IS ASSERTED, and each case is a different door into the same copy:
#   1. a worktree donor that SYMLINKS its web/node_modules is not a candidate,
#      and the skip is REPORTED (naming the link and its target, not just failing);
#   2. the normal case still works: a real donor is chosen, and what lands is a
#      REAL directory — the test does not pass by refusing everything;
#   3. PASS 1 (the exact webdeps a gate record names) cannot be satisfied by a
#      symlinked donor either — a record must not vouch for a link;
#   4. every EXPLICIT donor (WM_TEMPLATE, RELEASE_NODE_MODULES, WM_CACHE_DIR, and
#      a WM_LOCK_CACHE entry) is refused when it is a link, and the same donor as a
#      real directory still seeds (the control that says the refusal is about the
#      LINK, not about the donor being unusable);
#   5. a tree whose OWN web/node_modules is a link is re-seeded, not read as
#      "already installed" — that is how one poisoned donor survived a re-seed;
#   6. wm_copy_from refuses a link even when called directly, so a donor source
#      added later cannot reintroduce the defect by forgetting the candidate check;
#   7. THE BITE: `wm_is_symlink` is neutered in a COPY of the seeder, and the same
#      fixtures must then produce exactly the defect (the link is chosen and copied
#      into the candidate). Without this the six cases above could pass on a seeder
#      whose rule is dead code.
#
# HERMETIC: every donor, worktree and lockfile below is a fixture under $TMPDIR, the
# "next" binary is a two-line stub and npm is /bin/false. No network, no install, no
# gate slot, nothing outside the sandbox is read or written. Runtime ~1s.
#
# Run:  bash deploy/web_modules_test.sh [-v]
# Exit: 0 green · 1 red. Called by the `selftest` gate via scripts/preflight_test.sh.
set -uo pipefail

VERBOSE=0
[ "${1:-}" = -v ] && VERBOSE=1

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# WM_TEST_LIB is a seam for the BEFORE/AFTER evidence: point this suite at an older
# copy of the seeder (`git show <rev>:deploy/web_modules.sh > /tmp/old.sh`) and it must
# go RED on cases 1-6. A regression suite nobody has ever seen fail is a guess.
LIB="${WM_TEST_LIB:-$HERE/web_modules.sh}"
[ -r "$LIB" ] || { echo "FAIL: $LIB missing — there is no seeder to test" >&2; exit 1; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

SB="$(mktemp -d "${TMPDIR:-/tmp}/webmodules-test.XXXXXX")" || exit 1
# shellcheck disable=SC2064  # $SB is expanded now, on purpose: the trap runs later.
trap "rm -rf '$SB'" EXIT

# has <haystack> <needle> — substring test without a reader process (a `… | grep -q`
# pipeline takes SIGPIPE and reports 141 under `set -o pipefail`, which reads as a
# red that is not a violated property).
has() { case "$1" in *"$2"*) return 0 ;; esac; return 1; }
# show <text> — the failing payload, indented, so a red is diagnosable from the log.
show() { printf '%s\n' "$1" | sed 's/^/      | /'; }

# ----------------------------------------------------------------- fixtures -----
# The dependency-set identity here is the CONTENT of web/package-lock.json (what
# wm_lock_sha hashes) and of node_modules/.package-lock.json (the webdeps). Two
# donors installed from the same lockfile therefore match, which is what PASS 2 is.
LOCK_BODY='{"name":"jomform-fixture","lockfileVersion":3,"packages":{}}'

# mkdonor <node_modules dir> — a complete, REAL donor: .bin/next (the binary the
# seeder and the gates require) plus the .package-lock.json that carries webdeps.
mkdonor() {
  mkdir -p "$1/.bin" || return 1
  printf '#!/bin/sh\nexit 0\n' > "$1/.bin/next"
  chmod +x "$1/.bin/next"
  printf '%s\n' "$LOCK_BODY" > "$1/.package-lock.json"
  printf 'this donor is a REAL directory\n' > "$1/DONOR_MARKER"
}

# mklock <worktree> — the tree's own web/package-lock.json (the lock hash).
mklock() {
  mkdir -p "$1/web" || return 1
  printf '%s\n' "$LOCK_BODY" > "$1/web/package-lock.json"
}

# mktarget <worktree> — a tree a lane would seed: it declares its dependencies and
# has no install.
mktarget() { mklock "$1"; }

# seed_report <worktree> <want> [VAR=value ...] — run wm_seed in a CLEAN child that
# carries exactly the named environment, then print its verdict. A child, not this
# shell, so nothing leaks from one case into the next (the caller's own WM_REPO / a
# PREFLIGHT_RESULT_DIR from a gate run would otherwise change the answer).
seed_report() {
  local wt=$1 want=${2:-}; shift 2
  ( env -u WM_REPO -u WM_TEMPLATE -u WM_CACHE_DIR -u WM_LOCK_CACHE \
        -u RELEASE_NODE_MODULES -u WM_NPM -u WM_NPM_LOG -u WM_LAUNCH -u WM_PROG \
        -u WM_LINK_SAID -u PREFLIGHT_RESULT_DIR -u WM_SEED_WEBDEPS -u WM_SEED_SOURCE \
        -u WM_MATCHED "$@" \
        bash -c 'set -o pipefail
                 lib=$1; wt=$2; want=$3
                 . "$lib"          # $0 is "wm-child", so the seeder stays a LIBRARY
                 if wm_seed "$wt" "$want"; then rc=0; else rc=$?; fi
                 printf "RC=%s\nMATCHED=%s\nSOURCE=%s\n" "$rc" "${WM_MATCHED:-}" "${WM_SEED_SOURCE:-}"
                 exit "$rc"' wm-child "$LIB" "$wt" "$want" )
}

# cands_report <worktree> [VAR=value ...] — the candidate list, one entry per line,
# with the skip notes on stderr.
cands_report() {
  local wt=$1; shift
  ( env -u WM_REPO -u WM_TEMPLATE -u WM_CACHE_DIR -u WM_LOCK_CACHE \
        -u RELEASE_NODE_MODULES -u WM_LINK_SAID "$@" \
        bash -c 'set -o pipefail
                 lib=$1; wt=$2
                 . "$lib"
                 wm_candidates "$wt"
                 printf "%s\n" ${WM_CANDS[@]+"${WM_CANDS[@]}"}' wm-child "$LIB" "$wt" )
}

field() { sed -n "s/^$1=//p" <<<"$2"; }

# isreal <path> — a REAL install directory (not a link, has the next binary).
isreal() { [ -d "$1" ] && [ ! -L "$1" ] && [ -x "$1/.bin/next" ]; }

# A git fixture: one repo with a REAL-donor worktree, a SYMLINKED-donor worktree and
# a second repo whose ONLY installed worktree is symlinked (so PASS 1 and PASS 2
# have nothing legitimate to work with and the link is the only thing on offer).
GITC="git -c user.email=fixture@example.invalid -c user.name=fixture"
mkdir -p "$SB/repoA" "$SB/repoB"
$GITC -C "$SB/repoA" init -q && $GITC -C "$SB/repoA" commit -q --allow-empty -m fixture
$GITC -C "$SB/repoB" init -q && $GITC -C "$SB/repoB" commit -q --allow-empty -m fixture
$GITC -C "$SB/repoA" worktree add -q --detach "$SB/wt-real" HEAD
$GITC -C "$SB/repoA" worktree add -q --detach "$SB/wt-link" HEAD
$GITC -C "$SB/repoB" worktree add -q --detach "$SB/wt-onlylink" HEAD

mktarget "$SB/wt-real" && mkdonor "$SB/wt-real/web/node_modules"
mktarget "$SB/wt-link"
ln -s "$SB/wt-real/web/node_modules" "$SB/wt-link/web/node_modules"
mktarget "$SB/wt-onlylink"
ln -s "$SB/wt-real/web/node_modules" "$SB/wt-onlylink/web/node_modules"

if [ ! -L "$SB/wt-link/web/node_modules" ] || [ ! -x "$SB/wt-real/web/node_modules/.bin/next" ]; then
  bad "the fixture could not be built under $SB — nothing below would measure the seeder"
  printf -- '---\nweb_modules_test: %s passed, %s failed\n' "$PASS" "$FAIL"
  exit 1
fi

printf 'web_modules self-test (sandbox %s)\n' "$SB"

# 1. A SYMLINKED WORKTREE DONOR IS NOT A CANDIDATE ------------------------------
#    The list is what every later pass reads, so this is the door itself.
mktarget "$SB/t1"
cands="$(cands_report "$SB/t1" WM_REPO="$SB/repoA" 2>"$SB/e1")"
err1="$(cat "$SB/e1")"
[ "$VERBOSE" = 1 ] && { show "$cands"; show "$err1"; }
if ! has "$cands" "$SB/wt-real/web/node_modules"; then
  bad "1. the REAL donor worktree is missing from the candidate list, so this fixture measures nothing: [$(printf '%s' "$cands" | tr '\n' ' ')]"
elif has "$cands" "$SB/wt-link/web/node_modules"; then
  bad "1. a SYMLINKED worktree donor IS a candidate: [$SB/wt-link/web/node_modules] — cp -a will reproduce the link in the candidate, which is the defect. Candidates: [$(printf '%s' "$cands" | tr '\n' ' ')]"
elif ! has "$err1" "SYMLINK"; then
  bad "1. the symlinked donor was skipped SILENTLY: the train log then shows only the EISDIR minutes later, with nothing naming the donor that caused it. stderr: [$(printf '%s' "$err1" | tr '\n' ' ')]"
elif ! has "$err1" "$SB/wt-real/web/node_modules"; then
  bad "1. the skip note does not name the link TARGET ($SB/wt-real/web/node_modules), so an operator cannot tell which install the worktree was borrowing. stderr: [$(printf '%s' "$err1" | tr '\n' ' ')]"
else
  ok "1. a symlinked worktree donor is NOT a candidate, and the skip names the link and its target (real donor still on the list)"
fi

# 2. THE CONTROL: a real donor still seeds, and what lands is a REAL DIRECTORY ---
#    Without this, case 1 would also pass if the seeder simply refused everything.
mktarget "$SB/t2"
out2="$(seed_report "$SB/t2" "" WM_REPO="$SB/repoA" WM_NPM=/bin/false 2>"$SB/e2")"; rc2=$?
err2="$(cat "$SB/e2")"
[ "$VERBOSE" = 1 ] && { show "$out2"; show "$err2"; }
if [ "$rc2" -ne 0 ]; then
  bad "2. seeding an uninstalled tree from a REAL donor failed (rc=$rc2): a seeder that cannot seed is not usable either. stderr: [$(printf '%s' "$err2" | tr '\n' ' ')]"
elif [ -L "$SB/t2/web/node_modules" ]; then
  bad "2. the seeded web/node_modules is a SYMLINK — the seeded tree is exactly the shape that reds the model-id guard"
elif ! isreal "$SB/t2/web/node_modules"; then
  bad "2. the seeded web/node_modules is not a usable install (no .bin/next, or not a directory): [$(ls -la "$SB/t2/web" 2>&1 | tr '\n' ' ')]"
elif ! has "$out2" "$SB/wt-real/web/node_modules"; then
  bad "2. the run does not say WHICH donor it used: SOURCE=[$(field SOURCE "$out2")]"
elif ! has "$out2" "REAL directory"; then
  bad "2. the seed note does not say the donor was a REAL directory, so a log cannot tell this case from the defect: [$(printf '%s' "$out2" | tr '\n' ' ')]"
elif [ ! -f "$SB/t2/web/node_modules/DONOR_MARKER" ]; then
  bad "2. the copy does not contain the donor's own files — node_modules was created, not copied"
else
  ok "2. CONTROL: a REAL donor still seeds — the tree gets a real directory, the donor is NAMED, and the note says it was a REAL directory"
fi

# 3. PASS 1 (the record's exact webdeps) CANNOT BE SATISFIED BY A LINK ----------
#    This is the poisoned-donor shape that matters most: a gate record names a
#    dependency set, a symlinked install READS as that set (the file is reachable
#    through the link), and the copy would then put a link in the candidate.
WANT="$(sha256sum "$SB/wt-real/web/node_modules/.package-lock.json" | awk '{print $1}')"
mktarget "$SB/t3"
out3="$(seed_report "$SB/t3" "$WANT" WM_REPO="$SB/repoB" WM_NPM=/bin/false 2>"$SB/e3")"; rc3=$?
err3="$(cat "$SB/e3")"
[ "$VERBOSE" = 1 ] && { show "$out3"; show "$err3"; }
if [ -z "$WANT" ]; then
  bad "3. the fixture has no webdeps to ask for, so PASS 1 was never exercised"
elif [ -e "$SB/t3/web/node_modules" ] || [ -L "$SB/t3/web/node_modules" ]; then
  bad "3. PASS 1 seeded the tree from a SYMLINKED donor (the record's webdeps are readable through the link): [$(ls -ld "$SB/t3/web/node_modules" 2>&1)] — a record would then vouch for a tree that cannot be gated"
elif [ "$(field MATCHED "$out3")" != 0 ]; then
  bad "3. WM_MATCHED=$(field MATCHED "$out3") although nothing was seeded — the caller would report the record as reused"
elif [ "$rc3" -eq 0 ]; then
  bad "3. wm_seed returned 0 with no donor and npm disabled — it claims a seed that did not happen"
elif ! has "$err3" "SYMLINK"; then
  bad "3. the refusal does not name the symlink as the reason: [$(printf '%s' "$err3" | tr '\n' ' ')]"
else
  ok "3. PASS 1 refuses a symlinked donor: the record's webdeps are NOT satisfied by a link, nothing is seeded, and the refusal names the link"
fi

# 4. EVERY EXPLICIT DONOR DOOR REFUSES A LINK (and the control still works) -----
#    TRAIN_NODE_MODULES (WM_TEMPLATE) is the door the measured defect came through,
#    so it is first; the other three are the same rule at the same function.
LOCK_SHA="$(sha256sum "$SB/t3/web/package-lock.json" | awk '{print $1}')"
mkdir -p "$SB/lockcache/$LOCK_SHA"
ln -s "$SB/wt-real/web/node_modules" "$SB/lockcache/$LOCK_SHA/node_modules"
ln -s "$SB/wt-real/web/node_modules" "$SB/cache-link"

d4_fail=""
d4_ctl=""
for door in TEMPLATE RELEASE CACHE LOCKCACHE; do
  tw="$SB/t4-$door"; mktarget "$tw"
  case "$door" in
    TEMPLATE)  vars=(WM_TEMPLATE="$SB/wt-link/web/node_modules") ;;
    RELEASE)   vars=(RELEASE_NODE_MODULES="$SB/wt-link/web/node_modules") ;;
    CACHE)     vars=(WM_CACHE_DIR="$SB/cache-link") ;;
    LOCKCACHE) vars=(WM_LOCK_CACHE="$SB/lockcache") ;;
  esac
  out4="$(seed_report "$tw" "" WM_REPO="$SB/not-a-repo" WM_NPM=/bin/false \
            "${vars[@]}" 2>"$SB/e4-$door")"; rc4=$?
  err4="$(cat "$SB/e4-$door")"
  [ "$VERBOSE" = 1 ] && { show "$out4"; show "$err4"; }
  if [ -e "$tw/web/node_modules" ] || [ -L "$tw/web/node_modules" ]; then
    d4_fail="${d4_fail}${door}(seeded a link) "
  elif [ "$rc4" -eq 0 ]; then
    d4_fail="${d4_fail}${door}(claims to have seeded) "
  elif ! has "$err4" "SYMLINK"; then
    d4_fail="${d4_fail}${door}(silent) "
  fi
done
# CONTROL: the SAME door, same fixtures, a REAL directory — it must seed.
mktarget "$SB/t4-ctl"
out4c="$(seed_report "$SB/t4-ctl" "" WM_REPO="$SB/not-a-repo" WM_NPM=/bin/false \
          WM_TEMPLATE="$SB/wt-real/web/node_modules" 2>"$SB/e4c")"; rc4c=$?
[ "$VERBOSE" = 1 ] && { show "$out4c"; show "$(cat "$SB/e4c")"; }
if ! isreal "$SB/t4-ctl/web/node_modules" || [ "$rc4c" -ne 0 ]; then
  d4_ctl="CONTROL lost: a REAL WM_TEMPLATE no longer seeds (rc=$rc4c) — the refusal is not about the link"
fi
if [ -n "$d4_fail" ]; then
  bad "4. an explicit symlinked donor was accepted: [$d4_fail] — every one of WM_TEMPLATE / RELEASE_NODE_MODULES / WM_CACHE_DIR / WM_LOCK_CACHE must refuse a link and say so"
elif [ -n "$d4_ctl" ]; then
  bad "4. $d4_ctl"
else
  ok "4. all four explicit donors (WM_TEMPLATE, RELEASE_NODE_MODULES, WM_CACHE_DIR, WM_LOCK_CACHE) refuse a symlink and name it; the same doors still seed from a REAL directory"
fi

# 5. A TREE WHOSE OWN web/node_modules IS A LINK IS RE-SEEDED -------------------
#    `[ -x .bin/next ]` follows the link and calls this installed, and that is how
#    one poisoned donor stayed in a tree for the rest of the run.
mktarget "$SB/t5"
ln -s "$SB/wt-real/web/node_modules" "$SB/t5/web/node_modules"
out5="$(seed_report "$SB/t5" "" WM_REPO="$SB/repoA" WM_NPM=/bin/false 2>"$SB/e5")"; rc5=$?
err5="$(cat "$SB/e5")"
[ "$VERBOSE" = 1 ] && { show "$out5"; show "$err5"; }
if [ -L "$SB/t5/web/node_modules" ]; then
  bad "5. the tree still has a SYMLINKED web/node_modules after seeding (rc=$rc5): it was read as 'already installed', so a lane gates through a link"
elif ! isreal "$SB/t5/web/node_modules"; then
  bad "5. the tree did not end up with a real install (rc=$rc5): [$(ls -la "$SB/t5/web" 2>&1 | tr '\n' ' ')]"
elif ! has "$out5" "re-seeding"; then
  bad "5. the replacement of the tree's own link is not reported, so a log cannot tell it from a normal seed: [$(printf '%s' "$out5" | tr '\n' ' ')]"
else
  ok "5. a tree whose OWN web/node_modules is a link is re-seeded (reported), so a poisoned tree cannot be gated through the link"
fi

# 6. wm_copy_from REFUSES A LINK WHEN CALLED DIRECTLY (the second door) ---------
( . "$LIB"; wm_copy_from "$SB/wt-link/web/node_modules" "$SB/t6" ) >"$SB/o6" 2>"$SB/e6"; rc6=$?
[ "$VERBOSE" = 1 ] && { show "$(cat "$SB/o6")"; show "$(cat "$SB/e6")"; }
if [ "$rc6" -eq 0 ]; then
  bad "6. wm_copy_from reported success on a SYMLINKED source — the last door before the write is open"
elif [ -e "$SB/t6/web/node_modules" ] || [ -L "$SB/t6/web/node_modules" ]; then
  bad "6. wm_copy_from created [$SB/t6/web/node_modules] from a symlinked source: [$(ls -ld "$SB/t6/web/node_modules" 2>&1)]"
elif ! has "$(cat "$SB/e6")" "SYMLINK"; then
  bad "6. the direct refusal does not name the symlink: [$(printf '%s' "$(cat "$SB/e6")" | tr '\n' ' ')]"
else
  ok "6. wm_copy_from refuses a symlinked source directly, so a donor source added later cannot reopen the hole by forgetting the candidate check"
fi

# 7. THE BITE: neuter wm_is_symlink and the defect comes back -------------------
#    The six cases above could all pass on a seeder whose symlink rule is dead code
#    (e.g. a candidate list that happens to exclude the link for another reason).
#    So the rule itself is removed, in a COPY, and the same fixtures must then
#    produce exactly the measured defect: the link is chosen and copied in.
MUT="$SB/web_modules_mutated.sh"
awk '/^wm_is_symlink\(\)/{
       print "wm_is_symlink() { return 1; }   # BITE: neutered by web_modules_test.sh case 7"
       done=1; next
     }
     { print }
     END { if (!done) exit 9 }' "$LIB" > "$MUT"; mut_rc=$?
if [ "$mut_rc" -ne 0 ] || [ ! -s "$MUT" ]; then
  bad "7. BITE could not be applied: no single-line \`wm_is_symlink() { … }\` definition in $LIB (awk rc=$mut_rc), so this suite cannot prove its own guard is load-bearing — the rule was probably rewritten across lines; keep it on one line, or update this case with it"
elif cmp -s "$LIB" "$MUT"; then
  bad "7. BITE is a no-op: the mutated copy is byte-identical to $LIB, so the removal below proves nothing"
elif ! has "$(cat "$MUT")" "BITE: neutered"; then
  bad "7. BITE's marker is not in the mutated copy — the mutation did not land and the comparison would be vacuous"
else
  mktarget "$SB/t7"
  mutc="$( ( env -u WM_REPO -u WM_TEMPLATE -u WM_CACHE_DIR -u WM_LOCK_CACHE -u RELEASE_NODE_MODULES \
                 WM_REPO="$SB/repoB" bash -c 'set -o pipefail
                 lib=$1; wt=$2
                 . "$lib"
                 wm_candidates "$wt"
                 printf "%s\n" ${WM_CANDS[@]+"${WM_CANDS[@]}"}' wm-child "$MUT" "$SB/t7" ) 2>"$SB/e7")"
  # PASS 1 through the mutated seeder: only symlinked donors are on offer.
  ( env -u WM_REPO -u WM_TEMPLATE -u WM_CACHE_DIR -u WM_LOCK_CACHE -u RELEASE_NODE_MODULES \
        WM_REPO="$SB/repoB" WM_NPM=/bin/false \
        bash -c 'set -o pipefail
                 lib=$1; wt=$2; want=$3
                 . "$lib"
                 wm_seed "$wt" "$want"' wm-child "$MUT" "$SB/t7" "$WANT" ) \
      >"$SB/o7" 2>>"$SB/e7"
  if ! has "$mutc" "$SB/wt-onlylink/web/node_modules"; then
    bad "7. BITE did not bite: with wm_is_symlink neutered the symlinked worktree donor is STILL not a candidate, so that exclusion is caused by something other than the rule — the guard being removed would go unnoticed. Candidates: [$(printf '%s' "$mutc" | tr '\n' ' ')]"
  elif [ ! -L "$SB/t7/web/node_modules" ]; then
    bad "7. BITE did not bite: the mutated seeder chose the link but did not put one at $SB/t7/web/node_modules — the fixture, not the rule, is what case 3 was measuring: [$(ls -ld "$SB/t7/web/node_modules" 2>&1)]"
  else
    ok "7. BITE: with \`wm_is_symlink\` neutered the symlinked donor IS chosen and a LINK lands in the candidate — so cases 1-6 fail on the rule's removal, not on the fixture"
  fi
fi

# 8. THE LAST RESORT FAILS LOUDLY, NOT AS A BASH CRASH -------------------------
#    Found while proving this card's fix live: with no donor left and `set -u` in the
#    caller, `[ "${#WM_LAUNCH[@]}" -gt 0 ]` on an UNSET array aborted the seed with
#    "WM_LAUNCH: unbound variable" — the caller then cannot tell a broken seeder from
#    a failed install. WM_LAUNCH is documented as optional, so the refusal must be the
#    install's own ("npm ci failed"), not the shell's.
mktarget "$SB/t8"
out8="$( ( env -u WM_REPO -u WM_TEMPLATE -u WM_CACHE_DIR -u WM_LOCK_CACHE \
              -u RELEASE_NODE_MODULES -u WM_LAUNCH -u WM_NPM_LOG \
              WM_REPO="$SB/not-a-repo" WM_NPM=/bin/false \
              bash -c 'set -uo pipefail
                       lib=$1; wt=$2
                       . "$lib"
                       if wm_seed "$wt"; then echo "RC=0"; else echo "RC=$?"; fi' \
              wm-child "$LIB" "$SB/t8" ) 2>"$SB/e8" )"
err8="$(cat "$SB/e8")"
[ "$VERBOSE" = 1 ] && { show "$out8"; show "$err8"; }
if has "$err8" "unbound variable"; then
  bad "8. the seeder CRASHED under a caller's \`set -u\` with WM_LAUNCH unset ([$(printf '%s' "$err8" | tr '\n' ' ')]): WM_LAUNCH is optional, so an unset array must read as 'no launcher'"
elif ! has "$out8" "RC=1"; then
  bad "8. with no donor and a failing npm the seed did not return 1: [$(printf '%s' "$out8" | tr '\n' ' ')]"
elif ! has "$err8" "npm ci failed"; then
  bad "8. the refusal does not name the install as the reason, so a lane cannot tell a broken seeder from a failed npm ci: [$(printf '%s' "$err8" | tr '\n' ' ')]"
else
  ok "8. the last resort refuses LOUDLY under \`set -u\` (WM_LAUNCH unset): rc=1 and the reason is the failed install, not a bash crash"
fi

printf -- '---\nweb_modules self-test: %s passed, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
