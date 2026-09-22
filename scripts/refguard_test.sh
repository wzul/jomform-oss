#!/usr/bin/env bash
#
# scripts/refguard_test.sh — offline proof that scripts/refguard.sh does what it
# claims. Run by the `refguard` preflight gate; also runnable by hand:
#
#   bash scripts/refguard_test.sh
#
# THE POINT OF THE SUITE, and why it is a gate and not a script
#   The defect it guards (`refs/remotes/pr/<N>` hand-made mirrors that no
#   `--prune` can ever remove, going silently stale) was "fixed" once by deleting
#   the refs and RE-ARMED WITHIN 20 MINUTES by a lane typing the documented
#   GitHub idiom again. A cleanup is a point in time; the fix has to be something
#   that refuses the creation. A hook that is silently NOT installed is exactly
#   the "green gate that cannot fail" shape this repo has been bitten by before
#   (see preflight_test.sh check 11), so the suite asserts BOTH directions:
#     * armed  -> the idiom is refused, in the primary checkout AND in a worktree;
#     * unarmed -> `check` FAILS LOUDLY rather than reporting the namespace clean.
#
# OFFLINE BY CONSTRUCTION: every sandbox is a local bare remote + a clone, so
# `git fetch`/`git ls-remote`/`git push` all run over a path, never the network.
# No repo, no credentials, no host state is touched. ~10s.
#
# THE NON-OBVIOUS CASES (each one is a defect that would otherwise ship):
#   1. A LINKED WORKTREE. The lanes work in worktrees, and a RELATIVE
#      `core.hooksPath` is silently ignored from a linked worktree — measured, in
#      probe E of this very investigation the hook fired in the primary checkout
#      and the worktree created the mirror unchecked. If this suite only tested
#      the primary checkout it would certify a guard that does not cover the
#      environment the mirrors are actually made in.
#   2. THE WHOLE TRANSACTION MUST BE READ. The hook gets one line per ref update
#      on stdin. A hook that returns as soon as it finds a violation SIGPIPEs the
#      writer (git), and the hook's status then becomes 141 — under `pipefail`
#      that reads as a failure for the wrong reason, and in general the verdict
#      is taken from a signal instead of from the rule. So a transaction whose
#      forbidden ref is the LAST line must still be refused.
#   3. DELETION MUST STAY ALLOWED. You may remove a hand-made ref, you may not
#      create one. A guard that refused deletion too would make the namespace
#      uncleanable except by rm-ing pack files.
#   4. A SECOND REMOTE's namespace is legitimate (`gh pr checkout` adds a fork
#      remote), so the allow-list comes from `git remote`, not the word "origin".
#   5. `scan` MUST REFUSE TO REPORT when it cannot reach the live namespace.
#      "0 stale" from no data is the same false reading the mirrors cause.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The guard under test. Overridable so a SABOTAGED copy can be run through this
# suite to prove the assertions bite (see the bite proof in the gate notes /
# AGENTS.md): a suite that stays green against a guard with its check removed is
# not evidence, it is decoration.
GUARD="${REFGUARD_UNDER_TEST:-$HERE/refguard.sh}"
[ -x "$GUARD" ] || { echo "FAIL: $GUARD missing or not executable" >&2; exit 1; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # label expected actual
  if [ "$2" = "$3" ]; then ok "$1 ($3)"; else bad "$1: expected '$2', got '$3'"; fi
}
contains() { # label needle file
  if grep -qF -- "$2" "$3" 2>/dev/null; then ok "$1"; else bad "$1: '$2' not found in $3"; fi
}
not_contains() { # label needle file
  if grep -qF -- "$2" "$3" 2>/dev/null; then bad "$1: '$2' unexpectedly found"; else ok "$1"; fi
}

SB="$(mktemp -d /tmp/refguard_test.XXXXXX)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

echo "refguard self-test (sandbox $SB, git $(git --version | awk '{print $3}'))"

# --------------------------------------------------------------------------
# Sandbox A — a bare "remote" with a main branch and a refs/pull/1/head, a
# clone of it, and a linked worktree. This is the real shape: origin has a pull
# namespace, and lanes make mirrors inside worktrees.
# --------------------------------------------------------------------------
git init -q --bare "$SB/remote.git"
git init -q "$SB/work"
cd "$SB/work" || exit 1
git config user.email t@t; git config user.name t
printf 'one\n' > a.txt; git add a.txt; git commit -qm one
git remote add origin "$SB/remote.git"
MAIN="$(git symbolic-ref --short HEAD)"
git push -q origin "$MAIN"
git push -q origin "$MAIN:refs/pull/1/head"
git push -q origin "$MAIN:refs/pull/2/head"
git fetch -q origin
BASE_SHA="$(git rev-parse HEAD)"

# A mirror created BEFORE the hook exists: the state the guard must then allow
# deleting (case 3) and must recognise as hand-made when scanning (cases 10-12).
git update-ref refs/remotes/pr/1 "$BASE_SHA"
git for-each-ref --format='%(refname)' >"$SB/pre-hook-refs.txt" refs/remotes/

echo "--- the guard is not armed yet ---"
"$GUARD" check "$SB/work" >"$SB/check-unarmed.txt" 2>&1; rc=$?
check "check: an UNINSTALLED hook is reported NOT ARMED (rc=1)" 1 "$rc"
contains "check: ...and says so by name" "NOT ARMED" "$SB/check-unarmed.txt"
contains "check: ...and counts the hand-made refs it can see" "1 hand-made ref(s)" "$SB/check-unarmed.txt"

echo "--- install ---"
"$GUARD" install "$SB/work" >"$SB/install.txt" 2>&1; rc=$?
check "install: succeeds (rc=0)" 0 "$rc"
# ABSOLUTE on purpose: the suite `cd`s into several later sandboxes, and a
# relative `.git/hooks/...` silently stops resolving after the first `cd`.
HOOK="$(cd "$SB/work" && cd "$(git rev-parse --git-path hooks)" && pwd)/reference-transaction"
[ -x "$HOOK" ] && ok "install: hook is written and executable (git ignores a non-executable hook)" \
              || bad "install: $HOOK missing or not executable"
"$GUARD" install "$SB/work" >"$SB/install2.txt" 2>&1; rc=$?
check "install: is idempotent (second run rc=0)" 0 "$rc"

# --------------------------------------------------------------------------
# ensure — the gate calls this, so it must arm an unarmed repo AND be a no-op
# when already armed. A repo left unarmed (fresh clone, `git clean`, hook file
# deleted) has NO guard and nothing else says so.
# --------------------------------------------------------------------------
echo "--- ensure arms an unarmed repo (the gate's path) ---"
SB4="$SB/ensure-work"
git init -q "$SB4"
git -C "$SB4" config user.email t@t; git -C "$SB4" config user.name t
printf 'one\n' >"$SB4/a.txt"; git -C "$SB4" add a.txt; git -C "$SB4" commit -qm one
git -C "$SB4" remote add origin "$SB/remote.git"
EHOOK="$(cd "$SB4" && cd "$(git rev-parse --git-path hooks)" && pwd)/reference-transaction"
"$GUARD" ensure "$SB4" >"$SB/ensure1.txt" 2>&1; rc=$?
check "ensure: succeeds on an unarmed repo (rc=0)" 0 "$rc"
[ -x "$EHOOK" ] && ok "ensure: the hook is now installed and executable" || bad "ensure: $EHOOK still missing"
contains "ensure: ...and says it armed it" "no hook at" "$SB/ensure1.txt"
# ...and the armed repo now actually refuses the idiom: ensure must not print
# success while leaving a hook that does nothing.
git -C "$SB4" fetch origin refs/pull/1/head:refs/remotes/pr/8 >/dev/null 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "ensure: the freshly armed repo refuses the idiom (rc=$rc)" \
                || bad "ensure: repo armed by 'ensure' ALLOWED the idiom"
"$GUARD" ensure "$SB4" >"$SB/ensure2.txt" 2>&1; rc=$?
check "ensure: is a no-op when already armed (rc=0)" 0 "$rc"
contains "ensure: ...and says so" "already armed" "$SB/ensure2.txt"

# --------------------------------------------------------------------------
# Case 1 — the documented GitHub idiom is REFUSED, in the primary checkout.
# --------------------------------------------------------------------------
echo "--- the documented idiom (primary checkout) ---"
git fetch origin pull/2/head:refs/remotes/pr/2 >"$SB/idiom.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "idiom: refused (rc=$rc, not 0)" || bad "idiom: was ALLOWED (rc=0)"
contains "idiom: the refusal names the ref" "refs/remotes/pr/2" "$SB/idiom.txt"
contains "idiom: the refusal names a working alternative" "refs/prhead/" "$SB/idiom.txt"
check "idiom: no ref was created" "" "$(git rev-parse --verify -q refs/remotes/pr/2 || true)"

# --------------------------------------------------------------------------
# Case 2 — the same idiom, in a LINKED WORKTREE (where the lanes actually are).
# --------------------------------------------------------------------------
echo "--- the documented idiom (linked worktree) ---"
git worktree add -q "$SB/wt" -b wt-branch >/dev/null 2>&1
WT_HOOK="$(cd "$SB/wt" && git rev-parse --git-path hooks)/reference-transaction"
# Compare RESOLVED paths, not raw strings: the hook path from a worktree and from
# the primary checkout are spelled differently (relative vs absolute) but must be
# the same file — that shared location is what makes one install cover every lane.
WT_HOOK_REAL="$(cd "$(dirname "$WT_HOOK")" && pwd)/$(basename "$WT_HOOK")"
HOOK_REAL="$(cd "$(dirname "$HOOK")" && pwd)/$(basename "$HOOK")"
check "worktree: resolves the SAME hooks file as the primary checkout" "$HOOK_REAL" "$WT_HOOK_REAL"
( cd "$SB/wt" && git fetch origin refs/pull/1/head:refs/remotes/pr/9 ) >"$SB/idiom-wt.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "worktree: refused (rc=$rc, not 0)" || bad "worktree: was ALLOWED (rc=0)"
check "worktree: no ref was created" "" "$(git rev-parse --verify -q refs/remotes/pr/9 || true)"
( cd "$SB/wt" && git update-ref refs/remotes/pr/9 "$BASE_SHA" ) >"$SB/upd-wt.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "worktree: a raw update-ref is refused too (rc=$rc)" || bad "worktree: update-ref was allowed"

# --------------------------------------------------------------------------
# Case 2b — AN ATOMIC TRANSACTION: a forbidden ref anywhere in it (here, LAST)
# aborts the whole transaction, and the hook reads EVERY line before deciding.
#
# Measured context: `git fetch` with N refspecs is N separate transactions (probe:
# each ref got its own hook call), so the multi-refspec fetch below is NOT the
# atomic case and must NOT be asserted as one. `git update-ref --stdin` IS one
# transaction, which is what makes the "read to EOF, then decide" rule testable.
# --------------------------------------------------------------------------
echo "--- an atomic multi-ref transaction (forbidden ref last) ---"
printf 'create refs/prhead/keep %s\ncreate refs/remotes/pr/3 %s\n' "$BASE_SHA" "$BASE_SHA" \
  | git update-ref --stdin >"$SB/multi.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "atomic: refused (rc=$rc, not 0)" || bad "atomic: was ALLOWED (rc=0)"
contains "atomic: the refusal names the forbidden ref" "refs/remotes/pr/3" "$SB/multi.txt"
contains "atomic: the refusal gives a working alternative" "refs/prhead/" "$SB/multi.txt"
check "atomic: the forbidden ref was not created" "" "$(git rev-parse --verify -q refs/remotes/pr/3 || true)"
# The transaction is refused as a unit, so the ALLOWED ref in the same
# transaction must not land: a hook that acted only on the offending line would
# leave a partial write behind.
check "atomic: the whole transaction was aborted, no partial write" "" \
  "$(git rev-parse --verify -q refs/prhead/keep || true)"

# The same transaction with only allowed refs must SUCCEED — the mirror rule must
# not cost the atomic form its normal use.
printf 'create refs/prhead/ok %s\n' "$BASE_SHA" \
  | git update-ref --stdin >"$SB/atomic-ok.txt" 2>&1; rc=$?
check "atomic: an all-allowed transaction still succeeds (rc=0)" 0 "$rc"
check "atomic: ...and its ref landed" "$BASE_SHA" "$(git rev-parse --verify -q refs/prhead/ok || true)"

# READ-TO-EOF, in its OBSERVABLE form. A hook that acts on the first violation and
# stops reading has two real costs: the lane sees only the first offender (and
# discovers the rest one fix at a time), and the writer of the stdin pipe dies of
# SIGPIPE, so the hook's status becomes 141 instead of its own verdict. Both are
# why the loop consumes the whole transaction before returning. This asserts the
# first, which is the one a user can see.
echo "--- a transaction with TWO forbidden refs (both must be named) ---"
printf 'create refs/remotes/pr/6 %s\ncreate refs/remotes/pr/7 %s\n' "$BASE_SHA" "$BASE_SHA" \
  | git update-ref --stdin >"$SB/atomic-two.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "two-ref transaction: refused (rc=$rc, not 0)" || bad "two-ref transaction: was ALLOWED"
contains "two-ref transaction: the FIRST offending ref is named" "refs/remotes/pr/6" "$SB/atomic-two.txt"
contains "two-ref transaction: ...and the SECOND is named too (the whole transaction is read)" \
  "refs/remotes/pr/7" "$SB/atomic-two.txt"
check "two-ref transaction: neither ref was created" "" \
  "$(git rev-parse --verify -q refs/remotes/pr/6 || true)$(git rev-parse --verify -q refs/remotes/pr/7 || true)"

# --------------------------------------------------------------------------
# Case 2c — the real-world shape of the reported defect: a multi-refspec fetch
# where one refspec is a mirror. The fetch must FAIL (so a lane cannot miss it)
# and the mirror must not be created.
# --------------------------------------------------------------------------
echo "--- a multi-refspec fetch that includes the documented idiom ---"
git fetch origin \
  "refs/pull/1/head:refs/prhead/1" \
  "refs/pull/2/head:refs/remotes/pr/5" >"$SB/idiom-multi.txt" 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "multi-refspec fetch: FAILS (rc=$rc, not 0)" || bad "multi-refspec fetch: was ALLOWED"
check "multi-refspec fetch: the mirror was not created" "" "$(git rev-parse --verify -q refs/remotes/pr/5 || true)"

# --------------------------------------------------------------------------
# Case 3 — ALTERNATIVES still work (the prescription must be usable).
# --------------------------------------------------------------------------
echo "--- the prescribed alternatives ---"
git fetch origin "refs/pull/1/head:refs/prhead/2" >"$SB/alt1.txt" 2>&1; rc=$?
check "alt: refs/prhead/<N> works (rc=0)" 0 "$rc"
check "alt: refs/prhead/2 holds the PR head" "$BASE_SHA" "$(git rev-parse refs/prhead/2)"
git fetch origin "refs/pull/1/head:refs/heads/pr-1" >"$SB/alt2.txt" 2>&1; rc=$?
check "alt: a plain local branch works (rc=0)" 0 "$rc"
out="$(git ls-remote origin refs/pull/1/head 2>&1)"; rc=$?
check "alt: ls-remote creates no ref and succeeds (rc=0)" 0 "$rc"
case "$out" in *refs/pull/1/head*) ok "alt: ls-remote returned the head" ;; *) bad "alt: ls-remote returned nothing" ;; esac

# --------------------------------------------------------------------------
# Case 3b — DELETION of a pre-existing mirror is ALLOWED (one-way rule).
# --------------------------------------------------------------------------
echo "--- deletion stays allowed ---"
git update-ref -d refs/remotes/pr/1 >"$SB/del.txt" 2>&1; rc=$?
check "delete: removing a hand-made ref is allowed (rc=0)" 0 "$rc"
check "delete: the ref is gone" "" "$(git rev-parse --verify -q refs/remotes/pr/1 || true)"

# --------------------------------------------------------------------------
# Case 4 — a SECOND remote's namespace is legitimate.
# --------------------------------------------------------------------------
echo "--- a configured second remote ---"
git init -q --bare "$SB/fork.git"
git remote add fork "$SB/fork.git"
git push -q fork "$MAIN" >/dev/null 2>&1
git fetch -q fork >/dev/null 2>&1; rc=$?
check "second remote: 'git fetch fork' works (rc=0)" 0 "$rc"
check "second remote: refs/remotes/fork/<branch> was created" "$BASE_SHA" \
  "$(git rev-parse --verify -q "refs/remotes/fork/$MAIN" || echo MISSING)"
# ...but the un-tracked namespace is still refused while a fork remote exists.
git fetch origin refs/pull/1/head:refs/remotes/pr/4 >/dev/null 2>&1; rc=$?
[ "$rc" -ne 0 ] && ok "second remote: refs/remotes/pr/* is still refused (rc=$rc)" \
                || bad "second remote: the guard stopped guarding"

# --------------------------------------------------------------------------
# Case 5 — NORMAL GIT FLOW IS UNTOUCHED. A guard that breaks a lane's workflow
# gets removed, and then there is no guard.
# --------------------------------------------------------------------------
echo "--- normal git flow is untouched ---"
flow_fail=""
flow() { # label cmd...
  local label="$1"; shift
  if "$@" >"$SB/flow.txt" 2>&1; then ok "flow: $label"; else
    flow_fail="$flow_fail $label"; bad "flow: $label — $(tail -n1 "$SB/flow.txt")"
  fi
}
flow "fetch --prune origin"   git fetch --prune origin
flow "fetch origin"           git fetch origin
flow "commit"                 bash -c 'cd "$1" && printf "two\n" > b.txt && git add b.txt && git commit -qm two' _ "$SB/work"
flow "checkout -b"            git checkout -qb feature
flow "commit on a new branch" bash -c 'cd "$1" && printf "three\n" > c.txt && git add c.txt && git commit -qm three' _ "$SB/work"
flow "checkout back"          git checkout -q "$MAIN"
flow "merge"                  git merge -q --no-edit feature
flow "rebase"                 git rebase -q "$MAIN"
flow "stash"                  bash -c 'cd "$1" && printf "x\n" > d.txt && git add d.txt && git stash -q' _ "$SB/work"
flow "tag"                    git tag -a -m tag v1
flow "worktree add"           git worktree add -q "$SB/wt2" -b wt2
flow "worktree remove"        git worktree remove --force "$SB/wt2"
flow "push"                   git push -q origin "$MAIN"
flow "repack"                 git repack -q
flow "gc"                     git gc -q
flow "branch -D"              bash -c 'cd "$1" && git branch -D -q feature 2>/dev/null || git branch -d feature' _ "$SB/work"
flow "status"                 git status --porcelain
flow "log"                    git log --oneline -1
[ -z "$flow_fail" ] && ok "flow: nothing in the normal workflow was disturbed" \
                    || bad "flow: disturbed:$flow_fail"

# --------------------------------------------------------------------------
# Case 6 — an installed hook that is NOT executable is caught. Git ignores a
# non-executable hook with no warning, so this is a silent no-guard.
# --------------------------------------------------------------------------
echo "--- a non-executable installed hook ---"
chmod 644 "$HOOK"
"$GUARD" check "$SB/work" >"$SB/check-noexec.txt" 2>&1; rc=$?
check "check: a non-executable hook is NOT ARMED (rc=1)" 1 "$rc"
contains "check: ...and names the reason" "not executable" "$SB/check-noexec.txt"
chmod 755 "$HOOK"

# --------------------------------------------------------------------------
# Case 7 — a STALE installed hook is caught (the guard was edited, the copy was
# not re-installed). Otherwise a fixed rule keeps running the old rule.
# --------------------------------------------------------------------------
echo "--- a stale installed hook ---"
printf '\n# local edit not re-installed\n' >>"$HOOK"
"$GUARD" check "$SB/work" >"$SB/check-stale.txt" 2>&1; rc=$?
check "check: a stale installed copy is NOT ARMED (rc=1)" 1 "$rc"
contains "check: ...and says it is stale" "stale" "$SB/check-stale.txt"
"$GUARD" install "$SB/work" >/dev/null 2>&1
"$GUARD" check "$SB/work" >"$SB/check-armed.txt" 2>&1
contains "check: after re-install the guard reports armed" "armed" "$SB/check-armed.txt"

# --------------------------------------------------------------------------
# Sandbox B — scan. Needs mirrors to exist, so the hook is not installed here.
# --------------------------------------------------------------------------
echo "--- scan ---"
SB2="$SB/scan-work"
git init -q "$SB2"
cd "$SB2" || exit 1
git config user.email t@t; git config user.name t
printf 'one\n' > a.txt; git add a.txt; git commit -qm one
git remote add origin "$SB/remote.git"
git fetch -q origin
git fetch -q origin refs/pull/1/head:refs/remotes/pr/1 # CURRENT mirror
git update-ref refs/remotes/pr/999 "$(git rev-parse HEAD)"  # no such PR number

"$GUARD" scan "$SB2" >"$SB/scan-plain.txt" 2>&1; rc=$?
check "scan: exit 0 without --strict when mirrors exist (rc=0)" 0 "$rc"
contains "scan: lists the mirror" "pr/1" "$SB/scan-plain.txt"
contains "scan: reports a count" "mirrors=2" "$SB/scan-plain.txt"
contains "scan: does not claim a staleness verdict without --live" "not compared" "$SB/scan-plain.txt"

"$GUARD" scan "$SB2" --live >"$SB/scan-live.txt" 2>&1; rc=$?
check "scan --live: exits 0 in report mode (rc=0)" 0 "$rc"
contains "scan --live: pr/1 is current against the live refs/pull/1/head" "current" "$SB/scan-live.txt"
contains "scan --live: pr/999 has no live PR ref and says so" "NO-LIVE-REF" "$SB/scan-live.txt"

"$GUARD" scan "$SB2" --live --strict >/dev/null 2>&1; rc=$?
check "scan --live --strict: exits 1 when mirrors exist (rc=1)" 1 "$rc"

# Make pr/1 genuinely STALE: move the remote's PR head, mirror left behind.
cd "$SB/work" || exit 1
printf 'two\n' >> a.txt; git add a.txt; git commit -qm moved-head
git push -q origin "$MAIN:refs/pull/1/head"
NEW_SHA="$(git rev-parse HEAD)"
cd "$SB2" || exit 1
"$GUARD" scan "$SB2" --live >"$SB/scan-stale.txt" 2>&1
contains "scan --live: a mirror behind the live PR head is reported STALE" "STALE" "$SB/scan-stale.txt"
contains "scan --live: ...and names the live SHA it is behind" "${NEW_SHA:0:10}" "$SB/scan-stale.txt"
contains "scan --live: ...and counts it" "stale=1" "$SB/scan-stale.txt"

# A mirror holding a SHA nothing else holds is the 05a50e0 class.
ORPHAN="$(git commit-tree "$(git rev-parse HEAD^{tree})" -m orphan)"
git update-ref refs/remotes/pr/500 "$ORPHAN"
"$GUARD" scan "$SB2" >"$SB/scan-orphan.txt" 2>&1
contains "scan: a mirror-only SHA is flagged DELETE-LOSES-COMMIT" "DELETE-LOSES-COMMIT" "$SB/scan-orphan.txt"
contains "scan: ...and counts it" "delete_loses_commit=1" "$SB/scan-orphan.txt"
contains "scan: ...and tells you to preserve it first" "preserve the SHA" "$SB/scan-orphan.txt"

# A clean namespace must read as clean.
git update-ref -d refs/remotes/pr/1
git update-ref -d refs/remotes/pr/999
git update-ref -d refs/remotes/pr/500
"$GUARD" scan "$SB2" --live >"$SB/scan-clean.txt" 2>&1; rc=$?
check "scan: a clean namespace exits 0 (rc=0)" 0 "$rc"
contains "scan: ...and says so" "the namespace is clean" "$SB/scan-clean.txt"
"$GUARD" scan "$SB2" --live --strict >/dev/null 2>&1; rc=$?
check "scan --strict: a clean namespace exits 0 (rc=0)" 0 "$rc"

# --------------------------------------------------------------------------
# Case 7b — SIBLING namespaces: the refs the guard ALLOWS but nothing prunes.
# The prescribed name is refs/prhead/<N>, so a stale one there is the same trap
# moved rather than removed; scan has to be able to say so.
# --------------------------------------------------------------------------
echo "--- sibling namespaces (allowed, but reported) ---"
# A CURRENT prescribed ref, and a STALE one left behind after its PR moved.
git fetch -q origin refs/pull/1/head:refs/prhead/1
git fetch -q origin refs/pull/1/head:refs/prhead/2
# Move what prhead/2 was fetched from, leaving prhead/2 behind.
( cd "$SB/work" && printf 'sib\n' >> a.txt && git add a.txt && git commit -qm sibling-move \
  && git push -q origin "$MAIN:refs/pull/2/head" )
SIB_SHA="$(git -C "$SB/work" rev-parse HEAD)"
"$GUARD" scan "$SB2" --live >"$SB/scan-sib.txt" 2>&1; rc=$?
check "siblings: scan still exits 0 (they are allowed, not a defect) (rc=0)" 0 "$rc"
contains "siblings: the report names the prescribed namespace" "refs/prhead/1" "$SB/scan-sib.txt"
contains "siblings: a current prescribed ref is reported current" "current" "$SB/scan-sib.txt"
contains "siblings: a stale prescribed ref is reported STALE" "STALE" "$SB/scan-sib.txt"
contains "siblings: ...naming the live head it is behind" "${SIB_SHA:0:10}" "$SB/scan-sib.txt"
contains "siblings: the report is labelled advisory" "advisory" "$SB/scan-sib.txt"
# Siblings must NOT change the strict verdict: strict is about the defect.
"$GUARD" scan "$SB2" --live --strict >/dev/null 2>&1; rc=$?
check "siblings: they do not make --strict fail (rc=0 when no mirrors)" 0 "$rc"
# A name that encodes no PR number must say so rather than invent digits.
git update-ref refs/prhead/deadbeef "$BASE_SHA"
"$GUARD" scan "$SB2" --live >"$SB/scan-sib-nonum.txt" 2>&1
contains "siblings: a ref whose name is no PR number says so" "name encodes no PR number" "$SB/scan-sib-nonum.txt"
git update-ref -d refs/prhead/deadbeef

# --------------------------------------------------------------------------
# Case 8 — scan must REFUSE a verdict when it cannot reach the live namespace.
# "0 stale" from no data is the same false reading the mirrors cause.
# --------------------------------------------------------------------------
echo "--- scan with no live namespace to compare against ---"
SB3="$SB/nolive"
git init -q "$SB3"; cd "$SB3" || exit 1
git config user.email t@t; git config user.name t
printf 'one\n' > a.txt; git add a.txt; git commit -qm one
git remote add origin "$SB/emptyremote.git"
git init -q --bare "$SB/emptyremote.git"
git update-ref refs/remotes/pr/1 "$(git rev-parse HEAD)"
"$GUARD" scan "$SB3" --live >"$SB/scan-nolive.txt" 2>&1; rc=$?
check "scan --live: exits 2 (cannot measure) when the remote has no pull refs" 2 "$rc"
contains "scan --live: ...and refuses to report a staleness verdict" "CANNOT MEASURE" "$SB/scan-nolive.txt"
# The needle must be a verdict WORD, not the substring "current" — every line of
# the refusal text contains "current", which is what made this check lie before.
not_contains "scan --live: ...and reports no per-ref verdict at all" "VERDICT" "$SB/scan-nolive.txt"

# --------------------------------------------------------------------------
# Case 9 — `check` against a repo with no hook at all, and against a directory
# that is not a repo (a tool that cannot measure must not report success).
# --------------------------------------------------------------------------
echo "--- check against a bare/unarmed repo, and a non-repo ---"
"$GUARD" check "$SB3" >"$SB/check2.txt" 2>&1; rc=$?
check "check: FAILS on an unarmed repo even when a mirror exists (rc=1)" 1 "$rc"
# The exit status must answer ONLY "is the guard armed": `check` still has to go
# GREEN on a repo that is armed but happens to carry old mirrors, or no worktree
# preflight could ever pass while a cleanup is outstanding.
# NOTE: the mirror has to be created with the hook briefly moved aside — an armed
# guard refuses even this test's setup, which is the behaviour under test. This is
# also the realistic state: the 26 mirrors on the real repo predate the guard.
mv "$HOOK" "$HOOK.off"
git -C "$SB/work" update-ref refs/remotes/pr/77 "$BASE_SHA"
mv "$HOOK.off" "$HOOK"
"$GUARD" check "$SB/work" >"$SB/check-armed-with-mirror.txt" 2>&1; rc=$?
check "check: still exits 0 when ARMED but mirrors exist (the two questions are separate)" 0 "$rc"
contains "check: ...and reports the mirror count without failing" "1 hand-made ref(s)" "$SB/check-armed-with-mirror.txt"
mv "$HOOK" "$HOOK.off"
git -C "$SB/work" update-ref -d refs/remotes/pr/77
mv "$HOOK.off" "$HOOK"
NOTREPO="$(mktemp -d /tmp/refguard_norepo.XXXXXX)"
"$GUARD" check "$NOTREPO" >/dev/null 2>&1; rc=$?
check "check: a directory that is not a git repo exits 2 (cannot measure)" 2 "$rc"
"$GUARD" bogus-command >/dev/null 2>&1; rc=$?
check "an unknown command exits 2 rather than passing silently" 2 "$rc"
rm -rf "$NOTREPO"

echo "---"
echo "refguard self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
