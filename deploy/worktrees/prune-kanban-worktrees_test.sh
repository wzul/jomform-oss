#!/usr/bin/env bash
#
# deploy/worktrees/prune-kanban-worktrees_test.sh — self-test for prune-kanban-worktrees.sh.
#
# WHY
#   This script DELETES worktrees, and its one irreversible mistake is removing
#   the last copy of someone's work. The rule it states is "unpushed work is
#   never removed — it is reported instead", and the way that rule was broken was
#   subtle enough to survive two clean-looking cron runs: the durability test used
#   `git branch -r --contains <sha>`, which lists EVERY ref under refs/remotes/,
#   including the hand-made `refs/remotes/pr/<N>` mirrors
#   (`git fetch origin pull/N/head:refs/remotes/pr/N`, the documented GitHub
#   idiom). Those are in no refspec, so `git fetch --prune` never removes them
#   (git-fetch, PRUNING) and they keep whatever a PR's head was when they were
#   made. Work held ONLY by such a mirror therefore read as
#   durable="pushed-branch" and its worktree was removed.
#
#   So the assertions below are about that class of failure, and each is an
#   OUTCOME read off a real run against a real git repository in the sandbox —
#   not a grep of the script's source:
#     * a real `git fetch --prune origin` is observed to leave a mirror alone
#       (the premise, measured rather than quoted from the docs);
#     * the OLD predicate is kept as a control and shown to call mirror-only work
#       pushed-branch while the script's predicate does not (the bite: revert the
#       fix and the outcome assertions below go red);
#     * with a mirror present and no origin ref, the worktree SURVIVES;
#     * the mirror is NAMED in the log rather than silently ignored;
#     * genuinely pushed work, and work merged into origin/main, are still
#       REMOVED — the fix must not turn the pruner into a no-op;
#     * work held only by a local branch, work with uncommitted changes, work
#       younger than --min-age-hours, a card still running, an unreadable card,
#       and a worktree with a live worker process all SURVIVE, each with its own
#       reason in the log;
#     * the STRAY loop (outside .worktrees, no card reference) gets the same
#       correction;
#     * --dry-run removes nothing, and a repo path that does not exist is a no-op.
#
#   Entirely offline: a throwaway repo plus its own bare "origin" remote, a fake
#   `hermes kanban show` answering `kanban show` from a state file, no real board,
#   no network.
#
#   The EXIT-CODE contract is asserted here too, because it is the other half of
#   "unpushed work is never removed": a pruner that keeps work on purpose is a
#   pruner doing its job, so `0` must be what a caller sees for it, `3` is reserved
#   for "reclaimed nothing while keeping at-risk work" (the pruner-has-started-
#   keeping-everything alarm) and `1` for a run that could not do its job — a failed
#   removal, an unreadable card, a failed fetch. Every one of those is reproduced
#   here as an OUTCOME (exit status read off a real run), including the pair that
#   keeps exit 3 honest: the SAME at-risk set with something reclaimed exits 0.
#
# Run:  bash deploy/worktrees/prune-kanban-worktrees_test.sh
# Env:  JF_PRUNE_SCRIPT=<path>   self-test a different copy of the script (this is
#                               how the suite's own BITE is proven: point it at a
#                               copy with the old `git branch -r` test restored).
# Exit: 0 when every assertion holds, 1 otherwise.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${JF_PRUNE_SCRIPT:-$HERE/prune-kanban-worktrees.sh}"
[ -f "$SCRIPT" ] || { echo "FAIL: $SCRIPT missing" >&2; exit 1; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # desc expected actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (want '$2', got '$3')"; fi
}
contains() { # desc haystack needle
  # NOTE: a herestring, NOT `printf '%s' "$2" | grep -q`. Under this file's
  # `set -uo pipefail`, grep -q exits at the FIRST match, the writer then dies of
  # SIGPIPE (141), and pipefail reports the whole pipeline as failed — so a MATCH
  # reads as a miss as soon as the haystack is big enough to fill the pipe buffer.
  # The pruner's log grew past that with the exit-code work, and this helper started
  # reporting "missing" for text that was plainly in the log.
  if grep -qF -- "$3" <<<"$2"; then ok "$1"
  else bad "$1 (missing '$3' in: $(printf '%s' "$2" | tr '\n' '|' | cut -c1-600))"; fi
}

SB="$(mktemp -d /tmp/jf-prunewt-test.XXXXXX)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

REPO="$SB/repo"
ORIGIN="$SB/origin.git"
mkdir -p "$REPO" "$SB/bin" "$SB/fake-hermes" "$SB/strayroot"

# ─── the sandbox repository, with its own real bare "origin" ──────────────────
# A real remote (not a stub), so `git fetch --prune origin` genuinely runs: the
# claim that a fetch cannot remove a mirror is only worth something if the fetch
# actually happened and the origin refs really were refreshed by it.
git init -q --bare "$ORIGIN"
git -C "$REPO" init -q -b main
# A tracked file, so the DIRTY case below has something real to modify: an
# untracked marker would make every worktree dirty and prove nothing.
printf 'base\n' >"$REPO/README"
git -C "$REPO" add README
git -C "$REPO" -c user.email=t@t -c user.name=t commit -q -m "base"
git -C "$REPO" remote add origin "$ORIGIN"
git -C "$REPO" push -q -u origin main
git -C "$REPO" fetch -q origin

# A commit with main's tree, parented on main, and attached to NO ref: its only
# handle is whatever ref a test creates for it. That is exactly the shape of the
# defect — a commit that lives on a mirror and nowhere else.
fresh_commit() { # <message> -> sha
  local sha
  sha="$(git -C "$REPO" -c user.email=t@t -c user.name=t \
          commit-tree "$(git -C "$REPO" rev-parse 'main^{tree}')" -p main -m "$1")"
  printf '%s' "$sha"
}

MAIN_SHA="$(git -C "$REPO" rev-parse main)"
MERGED_SHA="$(git -C "$REPO" rev-list --max-parents=0 main | sed -n '1p')"   # an ancestor of main
MIRROR_SHA="$(fresh_commit 'mirror-only work')"
PUSHED_SHA="$(fresh_commit 'pushed work')"
LOCAL_SHA="$(fresh_commit 'local-only work')"
# The at-risk case: a commit on NO ref at all — no remote-tracking ref, no mirror,
# no local branch. The directory is then the only thing holding it, which is what
# the exit-3 "reclaimed nothing while keeping at-risk work" alarm is made of.
ORPHAN_SHA="$(fresh_commit 'orphan work')"

# The hand-made mirror, made exactly the documented way.
git -C "$REPO" update-ref refs/remotes/pr/999 "$MIRROR_SHA"
# A genuine pushed branch on the real remote.
git -C "$REPO" push -q origin "$PUSHED_SHA:refs/heads/wt/t_00000003-pushed"
git -C "$REPO" fetch -q origin
# A local branch holding a commit that is on no remote at all.
git -C "$REPO" branch -q wt/t_00000004-local "$LOCAL_SHA"

# ── the cases ────────────────────────────────────────────────────────────────
#   t_00000001 merged-into-main  commit reachable from origin/main       -> remove
#   t_00000002 MIRROR-ONLY       held by refs/remotes/pr/999 only        -> KEEP  <-- the defect
#   t_00000003 pushed            commit on a real origin/ branch         -> remove
#   t_00000004 local-only        held by a local branch, no remote       -> KEEP (UNSAFE)
#   t_00000005 dirty             pushed, with uncommitted changes        -> KEEP (UNSAFE)
#   t_00000006 fresh             pushed, card completed 5 min ago        -> KEEP (age)
#   t_00000007 open              pushed, card still running              -> KEEP (status)
#   <this run's worker id>       pushed, live worker process             -> KEEP (process)
#     Its id is derived from this run's PID (`t_0000<4 hex>`) rather than a fixed
#     constant, so a CONCURRENT copy of this suite (whose stand-in shares the same
#     argv shape) cannot be mistaken for this run's worker — see section 6.
#   t_00000009 unknowable        pushed, no card to read                 -> KEEP (status)
#   t_00000010 orphan            commit on NO ref at all, clean, old     -> KEEP (at-risk)
#   t_00000010 is the readable at-risk case: nothing under refs/ holds its
#   commit, so this directory may hold the only copy. It is what the exit-3
#   "reclaimed nothing while keeping at-risk work" alarm is made of, and it
#   must be a card the script CAN read, or the run is a failure (exit 1) instead.
for spec in "t_00000001:$MERGED_SHA" "t_00000002:$MIRROR_SHA" "t_00000003:$PUSHED_SHA" \
            "t_00000004:$LOCAL_SHA" "t_00000005:$PUSHED_SHA" "t_00000006:$PUSHED_SHA" \
            "t_00000007:$PUSHED_SHA" "t_00000009:$PUSHED_SHA" \
            "t_00000010:$ORPHAN_SHA"; do
  id="${spec%%:*}"; sha="${spec#*:}"
  git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/$id" "$sha"
done
# The live-worker case's worktree, on this run's PID-derived id (a valid `t_<8 hex>`
# name, so the pruner's extraction and the card fixture see a normal sandbox card).
WORKER_TASK="$(printf 't_%08x' $$)"
git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/$WORKER_TASK" "$PUSHED_SHA"
# The DIRTY case: a change to a TRACKED file, which is what `git status
# --porcelain` reports. (The proof-of-survival below is the worktree DIRECTORY,
# not an untracked file — an untracked marker would make every case dirty.)
printf 'modified\n' >>"$REPO/.worktrees/t_00000005/README"
check "the DIRTY case really is dirty (its tracked README is modified)" \
  "yes" "$( [ -n "$(git -C "$REPO/.worktrees/t_00000005" status --porcelain)" ] && echo yes || echo no )"

# ── the fake `hermes kanban show` ────────────────────────────────────────────
# Answers from $SB/fake-hermes/<task>: line 1 "status", line 2 "YYYY-MM-DD HH:MM".
# A card with no state file reads as unknown -> the script must KEEP it.
cat >"$SB/bin/hermes" <<'EOF'
#!/usr/bin/env bash
task=""
while [ $# -gt 0 ]; do
  case "$1" in
    show) task="${2:-}"; shift 2 ;;
    *) shift ;;
  esac
done
f="$JF_FAKE_HERMES_DIR/$task"
if [ -n "$task" ] && [ -f "$f" ]; then
  status="$(sed -n '1p' "$f")"
  completed="$(sed -n '2p' "$f")"
  printf 'Task %s: sandbox card\n' "$task"
  printf '  status:    %s\n' "$status"
  [ -n "$completed" ] && printf '  completed: %s\n' "$completed"
else
  printf 'Task %s: no such task\n' "$task"
fi
exit 0
EOF
chmod +x "$SB/bin/hermes"

OLD="2020-01-01 00:00"   # comfortably older than --min-age-hours
for id in t_00000001 t_00000002 t_00000003 t_00000004 t_00000005; do
  printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$id"
done
# t_00000010 is the readable at-risk case (its card is terminal and old), so it is
# what the exit-3 alarm is built from — it must NOT also be unreadable, or the run
# would be a failure (exit 1) and never reach the exit-3 verdict.
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/t_00000010"
printf 'done\n%s\n' "$(date -d '-5 minutes' '+%F %T')" >"$SB/fake-hermes/t_00000006"
printf 'running\n%s\n' "$OLD" >"$SB/fake-hermes/t_00000007"
# t_00000008 (live worker process) and t_00000009 (a card that cannot be read)
# deliberately have NO state file at this point: the first run's process is
# started later, and an unreadable card must survive that run. Both get a `done`
# card only AFTER the first run has proved they were kept.

# ── run the script ───────────────────────────────────────────────────────────
LOG="$SB/prune.log"
run_prune() { # extra args…
  env PATH="$SB/bin:$PATH" \
      JF_FAKE_HERMES_DIR="$SB/fake-hermes" \
      JF_PRUNE_REPO="$REPO" JF_PRUNE_BOARD=sandbox JF_PRUNE_LOG="$LOG" \
      JF_PRUNE_EXTRA_ROOTS="$SB/strayroot" \
      bash "$SCRIPT" "$@"
}
survives()       { [ -d "$REPO/.worktrees/$1" ] && echo yes || echo no; }
stray_survives() { [ -d "$SB/strayroot/$1" ] && echo yes || echo no; }

echo "prune-kanban-worktrees self-test (sandbox $SB, script $SCRIPT)"
echo "--- the premise, measured on the sandbox's real remote ---"

# 0. A real fetch prunes and refreshes the ORIGIN refs ... and leaves the mirror
#    exactly where it was. Both halves matter: the first says the fetch ran, the
#    second is the reason the defect exists.
git -C "$REPO" update-ref refs/remotes/pr/999 "$MIRROR_SHA"   # fresh, as a lane's fetch would
git -C "$REPO" fetch -q --prune origin
check "a real 'git fetch --prune origin' leaves refs/remotes/pr/999 in place" \
  "$MIRROR_SHA" "$(git -C "$REPO" rev-parse --verify -q refs/remotes/pr/999 2>/dev/null)"
check "...while that same fetch is what created origin/wt/t_00000003-pushed" \
  "$PUSHED_SHA" "$(git -C "$REPO" rev-parse --verify -q origin/wt/t_00000003-pushed 2>/dev/null)"

echo "--- the durability predicate: the old one, and this script's ---"

# 1. The control: the pre-fix predicate, verbatim, kept here so the suite can
#    never claim the fix matters without showing the disagreement.
old_durable() { git -C "$REPO" branch -r --contains "$1" 2>/dev/null | grep -q . && echo pushed-branch || echo UNSAFE; }
check "control: 'git branch -r --contains' calls the mirror-only commit pushed-branch" \
  "pushed-branch" "$(old_durable "$MIRROR_SHA")"
check "control: that commit is on NO origin ref" \
  "0" "$(git -C "$REPO" for-each-ref --format='%(refname)' --contains "$MIRROR_SHA" refs/remotes/origin | wc -l | tr -d ' ')"
check "control: and not on main either" \
  "no" "$(git -C "$REPO" merge-base --is-ancestor "$MIRROR_SHA" origin/main 2>/dev/null && echo yes || echo no)"

# 2. This script's predicate, through its own library entry point.
lib() { env JF_PRUNE_LIB_ONLY=1 bash -c ". '$SCRIPT'; $1"; }
check "the script's verdict for mirror-only work is not durable" \
  "none" "$(lib "durable_verdict '$REPO' '$MIRROR_SHA'")"
check "the script's verdict for a genuinely pushed commit is pushed-branch" \
  "pushed-branch" "$(lib "durable_verdict '$REPO' '$PUSHED_SHA'")"
check "the script's verdict for a commit on origin/main is merged-into-main" \
  "merged-into-main" "$(lib "durable_verdict '$REPO' '$MERGED_SHA'")"
check "the script's verdict for local-only work is local-branch" \
  "local-branch" "$(lib "durable_verdict '$REPO' '$LOCAL_SHA'")"
check "the local holder is named" \
  "wt/t_00000004-local" "$(lib "local_holder '$REPO' '$LOCAL_SHA'")"
contains "mirror_note names the ref it refuses to trust" \
  "$(lib "mirror_note '$REPO' '$MIRROR_SHA'")" "refs/remotes/pr/999"
check "mirror_note is empty when nothing non-remote-tracking holds the commit" \
  "" "$(lib "mirror_note '$REPO' '$PUSHED_SHA'")"

echo "--- a real run: what it removes, and what it refuses to ---"

: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
# Run 1 has TWO cards it cannot read (t_00000008/09 have no state file yet), which
# under the contract is exit 1 — a worktree that can never be judged is retained
# forever, i.e. the pruner silently stopped reclaiming it.
check "an unreadable card is a FAILURE, not a silent keep: the run exits 1" "1" "$RC"

# 3. THE CARD'S DEFECT, as an outcome on disk: the mirror-only worktree survives.
check "mirror-only worktree SURVIVES (its only ref is a mirror)" "yes" "$(survives t_00000002)"
contains "it is reported UNSAFE" "$(cat "$LOG")" "UNSAFE t_00000002"
contains "...naming refs/remotes/pr/999 as the ref it refuses to trust" \
  "$(cat "$LOG")" "ignoring non-remote-tracking ref(s): refs/remotes/pr/999"
contains "...and saying the commit is on no ref at all" "$(cat "$LOG")" "on no ref at all"

# 4. The fix must not neuter the pruner: real durability still prunes.
check "merged-into-main worktree was REMOVED" "no" "$(survives t_00000001)"
check "genuinely pushed worktree was REMOVED" "no" "$(survives t_00000003)"
contains "the removal records the merged verdict" "$(cat "$LOG")" "RM    t_00000001"
contains "...as durable=merged-into-main" "$(cat "$LOG")" "durable=merged-into-main"
contains "the removal records the pushed verdict" "$(cat "$LOG")" "RM    t_00000003"
contains "...as durable=pushed-branch" "$(cat "$LOG")" "durable=pushed-branch"

# 5. Every other refusal keeps its own reason, so a later edit cannot collapse
#    them all into one indistinguishable "kept for some reason".
check "local-only worktree SURVIVES" "yes" "$(survives t_00000004)"
contains "local-only work is reported with its branch named" \
  "$(cat "$LOG")" "only on LOCAL branch 'wt/t_00000004-local'"
check "a worktree with uncommitted changes SURVIVES" "yes" "$(survives t_00000005)"
contains "uncommitted changes are reported" "$(cat "$LOG")" "uncommitted change(s)"
check "a card completed 5 minutes ago SURVIVES (min-age)" "yes" "$(survives t_00000006)"
contains "the age refusal quotes the completion time" "$(cat "$LOG")" "KEEP  t_00000006 (completed"
check "a card still running SURVIVES" "yes" "$(survives t_00000007)"
contains "the status refusal names the status" "$(cat "$LOG")" "KEEP  t_00000007 (card status=running)"
check "a card that cannot be read SURVIVES (never act on a card you cannot read)" \
  "yes" "$(survives t_00000009)"
contains "...and is named as a FAILURE, not assumed terminal" \
  "$(cat "$LOG")" "FAIL  t_00000009 (card status unreadable"
# ...and t_00000009's card is then made terminal. It was deliberately unreadable to
# prove the never-act-on-an-unreadable-card guard; leaving it unreadable would make
# every later run exit 1 (correctly) and hide the exit-code assertions built below.
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/t_00000009"

# 6. The live-process guard: a worker holding the card keeps the worktree even
#    though the work is durable and the card is old. The card is made terminal
#    FIRST (it was unreadable above, which is a different guard), and the second
#    run is the one that must keep it.
#
#    THE STAND-IN'S TASK ID IS UNIQUE TO THIS RUN (card t_48f08867). The guard
#    matches a WHOLE process-table pattern, and this suite's sandbox task ids used to
#    be the same constants in every copy, so a peer run at its own live-worker stage
#    put a byte-identical argv (`work kanban task t_00000008`) on the shared table.
#    This run's "worker gone -> must be removed" step then saw the PEER and
#    false-KEEPed — reported by QA as `with the worker gone, that same worktree is
#    removed (want 'no', got 'yes')`, reproduced deterministically here by starting a
#    peer stand-in before the run (1/1). The id below is derived from this run's PID,
#    so a peer's stand-in can never satisfy this run's pattern. It is still a valid
#    `t_<8 hex>` id, so the pruner's name->task extraction and the card fixture are
#    unchanged.
WORKER_PAT="work kanban task $WORKER_TASK"
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$WORKER_TASK"
env bash -c "exec -a \"$WORKER_PAT\" sleep 40" &
WORKER_PID=$!
sleep 0.6
# The assertion that a match is genuinely visible — with a plain, non-early-exiting
# grep, so the sanity check cannot itself be a false miss (which is the bug below).
check "the stand-in worker process is visible in the process table" "yes" \
  "$(ps -eo args= 2>/dev/null | grep -F -- "$WORKER_PAT" >/dev/null 2>&1 && echo yes || echo no)"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "a worktree with a live worker process SURVIVES" "yes" "$(survives "$WORKER_TASK")"
contains "the live-process refusal is logged" "$(cat "$LOG")" "KEEP  $WORKER_TASK (worker process alive)"
kill "$WORKER_PID" 2>/dev/null
wait "$WORKER_PID" 2>/dev/null
# ... and it is only the process holding it: with the process gone, the same
# worktree is removable, so the guard is not simply refusing everything. A peer
# run's stand-in carries a different pid-derived id, so it cannot keep this one alive.
run_prune --min-age-hours 24 >/dev/null 2>&1
check "with the worker gone, that same worktree is removed" "no" "$(survives "$WORKER_TASK")"

# 6b. THE BITE for the process-scan predicate (card t_48f08867). The guard reads the
#     process table through a scanner-excluding predicate. The version that shipped
#     used `… | grep -vE … | grep -Fq` under `set -uo pipefail`, which reports a
#     MATCH as a MISS: `grep -Fq` exits at its FIRST match while the upstream `grep`
#     is still writing its next stdio block, the writer takes SIGPIPE (141), and
#     pipefail fails the WHOLE pipeline for a search that plainly matched. That is
#     the gate's data-loss direction — a live worker's worktree pruned — and it bites
#     as a function of table SIZE, which is why a loaded full preflight reds while the
#     isolated run (73/0) does not. Measured on this host: 0/400 misses at 377 procs,
#     197/400 at 978, 343/400 at 2395.
#     The table is SYNTHESISED by a stub `ps` on PATH, so this is deterministic and
#     load-independent: it reproduces the exact byte shape (the match EARLY, > one
#     stdio block after it) that a real loaded table has. Three checks:
#       (i)   the pre-fix predicate, kept verbatim as a CONTROL, mis-reads it;
#       (ii)  the SCRIPT'S OWN has_process_matching reads it correctly;
#       (iii) the scanner exclusion that has always been there still holds, and a
#             real worker's argv still matches.
STUBDIR="$SB/stub"; mkdir -p "$STUBDIR"
cat >"$STUBDIR/ps" <<'STUB'
#!/usr/bin/env bash
# A synthetic `ps -eo args=` table. $JF_STUB_PS_ROWS (one argv per line) is printed
# FIRST — the match early, which is the shape that starves an early-exiting reader —
# then FAR more than a Linux pipe buffer (64K) of argv after it. That margin is what
# makes the control below deterministic rather than racy: a writer that still has
# 64K+ to send cannot finish before the reader exits, so it MUST block and then take
# SIGPIPE once the reader is gone. Below ~64K the writer can finish first and the
# pre-fix pipeline spuriously reads as a match.
[ -n "${JF_STUB_PS_ROWS:-}" ] && printf '%s\n' "$JF_STUB_PS_ROWS"
_i=0; while [ "$_i" -lt 2000 ]; do
  printf 'worker-proc-%d --flag 0123456789 abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789\n' "$_i"
  _i=$((_i+1))
done
STUB
chmod +x "$STUBDIR/ps"
GUARD_PAT="work kanban task t_bite0001"
SCANNER_EXCLUDE='^[[:space:]]*([^[:space:]]*/)?(pgrep|pkill|ps|grep|awk|sed)([[:space:]]|$)'
# The pre-fix predicate, verbatim (the `t_f454cd2b` shape), kept as a control so the
# bite below can never claim to matter without showing the disagreement — the same
# discipline as the `old_durable` control earlier in this file.
old_matching_pipe() { grep -vE "$SCANNER_EXCLUDE" | grep -Fq -- "$GUARD_PAT"; }
guard_via_script() { # <rows> — the script's own predicate, against the stub table
  env JF_PRUNE_LIB_ONLY=1 JF_STUB_PS_ROWS="$1" PATH="$STUBDIR:$PATH" \
    bash -c ". '$SCRIPT'; has_process_matching '$GUARD_PAT'; echo \$?" 2>/dev/null | sed -n '$p'
}
# Premise, measured: the synthesised table really is larger than a Linux pipe buffer,
# otherwise the control below would not bite for the reason it claims.
check "the synthetic table is larger than a Linux pipe buffer (so an early-exiting reader MUST starve the writer)" "yes" \
  "$( [ "$( "$STUBDIR/ps" | wc -c )" -gt 65536 ] && echo yes || echo no )"
if JF_STUB_PS_ROWS="$GUARD_PAT" "$STUBDIR/ps" | old_matching_pipe; then
  bad "control: the pre-fix '… | grep -vE … | grep -Fq' predicate was expected to report this early match as a MISS (SIGPIPE under pipefail), but it MATCHED — the bite for the process scan is no longer live"
else
  ok "control: the pre-fix '… | grep -vE … | grep -Fq' predicate reports a MATCH as a MISS on an early match (SIGPIPE/pipefail)"
fi
check "the script's predicate reads that SAME early-match table correctly" "0" "$(guard_via_script "$GUARD_PAT")"
# ...the scanner exclusion must survive the rewrite: a table whose only matching row
# is a peer's SCANNER argv is NOT a live worker.
check "a scanner's own argv is still not a worker" "1" \
  "$(guard_via_script "pgrep -f $GUARD_PAT")"
# ...and a real worker's argv must still match.
check "...while a real worker's argv still matches" "0" \
  "$(guard_via_script "hermes -p jf-dev work kanban task t_bite0001")"

# 7. The STRAY loop (outside .worktrees, no card reference) carries the same
#    correction. Strays are judged by the directory's mtime, so both are aged.
STRAY_MIRROR_SHA="$(fresh_commit 'stray mirror-only work')"
git -C "$REPO" update-ref refs/remotes/pr/998 "$STRAY_MIRROR_SHA"
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-mirror" "$STRAY_MIRROR_SHA"
STRAY_PUSHED_SHA="$(fresh_commit 'stray pushed work')"
git -C "$REPO" push -q origin "$STRAY_PUSHED_SHA:refs/heads/wt/jf-stray-pushed"
git -C "$REPO" fetch -q origin
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-pushed" "$STRAY_PUSHED_SHA"
# A stray held by a LOCAL branch (not durable, but not a last copy either) and a
# stray held by NO ref at all (at-risk). The two must be counted differently: the
# branch outlives the worktree, the ref-less commit does not.
STRAY_LOCAL_SHA="$(fresh_commit 'stray local-only work')"
git -C "$REPO" branch -q wt/jf-stray-local "$STRAY_LOCAL_SHA"
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-local" "$STRAY_LOCAL_SHA"
STRAY_ORPHAN_SHA="$(fresh_commit 'stray orphan work')"
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-orphan" "$STRAY_ORPHAN_SHA"
touch -d '2020-01-01' "$SB/strayroot/jf-mirror" "$SB/strayroot/jf-pushed" \
                     "$SB/strayroot/jf-local" "$SB/strayroot/jf-orphan"
run_prune --dry-run --min-age-hours 24 >/dev/null 2>&1
check "a STRAY worktree held only by a mirror SURVIVES" "yes" "$(stray_survives jf-mirror)"
contains "the stray mirror refusal is logged, with the ref named" \
  "$(cat "$LOG")" "UNSAFE jf-mirror"
contains "...naming refs/remotes/pr/998" "$(cat "$LOG")" "refs/remotes/pr/998"
check "a STRAY worktree held by a LOCAL branch SURVIVES" "yes" "$(stray_survives jf-local)"
contains "...and is reported with its branch named, not as at-risk" \
  "$(cat "$LOG")" "only on LOCAL branch 'wt/jf-stray-local' — not pruning stray"
check "a STRAY worktree held by NO ref at all SURVIVES" "yes" "$(stray_survives jf-orphan)"
# The whole point of the split: the at-risk count is the two ref-less keeps — the card
# orphan (t_00000010) and the stray orphan (jf-orphan) — and NOT the local-branch stray
# just above. A board full of unpushed branches must not trip the exit-3 "pruner is
# keeping everything" alarm.
contains "...and the at-risk count is the two ref-less keeps only (card orphan + stray orphan)" \
  "$(cat "$LOG")" "at_risk=2"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "a STRAY worktree with a real pushed branch was REMOVED" "no" "$(stray_survives jf-pushed)"
contains "the stray removal records its verdict" "$(cat "$LOG")" "RM    jf-pushed (stray"

echo "--- --dry-run must delete nothing ---"
# Recreate a removable worktree (the run above removed it), so the dry run has
# something it WOULD remove.
git -C "$REPO" worktree prune
git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/t_00000003" "$PUSHED_SHA"
BEFORE="$(git -C "$REPO" worktree list --porcelain | grep -c '^worktree ')"
: >"$LOG"
run_prune --dry-run --min-age-hours 24 >/dev/null 2>&1; DRY_RC=$?
AFTER="$(git -C "$REPO" worktree list --porcelain | grep -c '^worktree ')"
check "a dry run leaves the worktree count unchanged" "$BEFORE" "$AFTER"
check "a dry run leaves the removable worktree on disk" "yes" "$(survives t_00000003)"
contains "a dry run says WOULD, not RM" "$(cat "$LOG")" "WOULD t_00000003"
if grep -q "RM    t_00000003" "$LOG"; then
  bad "a dry run performed a removal"
else
  ok "a dry run performed no removal"
fi
# A dry run is never judged on progress: it reclaims nothing BY DESIGN, so "reclaimed
# nothing" cannot mean "the pruner is keeping everything" there. Reporting at-risk
# keeps in an exploratory run's exit status would make every hand-run `--dry-run`
# look like a failure. It reports them in the log (above) and exits 0; the alarm is
# asserted on real runs below.
check "a dry run reporting work that exists only locally is still exit 0 (a dry run cannot judge progress)" "0" "$DRY_RC"

echo "--- the exit-code contract: what a caller can alert on ---"
# Exit 3 used to mean "any unsafe count", which on a busy board is EVERY run — the
# signal a human reads could never clear (measured on the real host: the last 7 of 7
# runs, and 528 of 567 refusals being a local branch whose commit still exists in a
# ref). It now means one narrow thing: reclaimed nothing while keeping work whose
# only copy may be this directory. The four cases below pin all four codes.

# 8a. A keep on purpose, with progress: exit 0. This is the case the real board is in
#     almost every day, and it is the one that must NOT be an error. The dry run above
#     left the pushed t_00000003 on disk, so this real run reclaims it while keeping the
#     at-risk work (t_00000010 and the jf-orphan stray — 2 ref-less commits).
check "the reclaimable worktree is still on disk from the dry run" "yes" "$(survives t_00000003)"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a run that keeps at-risk work AND reclaims is exit 0 — kept on purpose is not a failure" "0" "$RC"
contains "...the reclaim is recorded" "$(cat "$LOG")" "RM    t_00000003"
contains "...and the at-risk keeps are counted separately from the unsafe total" \
  "$(cat "$LOG")" "at_risk=2"
contains "...while the unsafe total still counts them" "$(cat "$LOG")" "unsafe="
contains "...and nothing in that run is called a failure" "$(grep -c 'FAIL ' "$LOG")" "0"

# 8b. The alarm: at-risk work kept and NOTHING reclaimed at all. A pruner that has
#     started keeping everything looks exactly like this. Everything reclaimable is
#     already gone, so the only thing left is the at-risk work.
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "at-risk work kept with no reclaim at all is exit 3 (the pruner-is-keeping-everything alarm)" "3" "$RC"
contains "...and the summary names the at-risk keeps" "$(cat "$LOG")" "at_risk=2"

# 8c. A FAILED removal is exit 1, not a silent keep: the worktree is retained because
#     git refused, which is a different thing from a deliberate refusal. A locked
#     worktree reproduces it deterministically (git 2.43; `remove --force` cannot
#     remove a locked tree, only `remove --force --force` can).
git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/t_00000003" "$PUSHED_SHA"
git -C "$REPO" worktree lock --reason sandbox "$REPO/.worktrees/t_00000003"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a removal that git REFUSES is exit 1" "1" "$RC"
contains "...and the refusal is logged as a FAIL, not as a keep" \
  "$(cat "$LOG")" "FAIL  t_00000003 (git worktree remove failed)"
check "...and the locked worktree is still on disk" "yes" "$(survives t_00000003)"
contains "...and the summary counts it as a failure" "$(cat "$LOG")" "failures=1"
git -C "$REPO" worktree unlock "$REPO/.worktrees/t_00000003" 2>/dev/null
check "the lock is what caused it (unlocking makes the same worktree removable)" "no" \
  "$( git -C "$REPO" worktree remove --force "$REPO/.worktrees/t_00000003" >/dev/null 2>&1; [ -d "$REPO/.worktrees/t_00000003" ] && echo yes || echo no )"

# 8d. A failed FETCH is exit 1 too: the durability verdict was then taken on
#     possibly-stale refs, which is the state that both keeps pushed work forever and
#     can call stale refs durable. A repo whose origin cannot be reached reproduces it
#     deterministically and offline.
: >"$LOG"
UNREACHABLE="$SB/unreachable-repo"
git init -q -b main "$UNREACHABLE"
git -C "$UNREACHABLE" remote add origin "$SB/no-such-origin.git"
mkdir -p "$UNREACHABLE/.worktrees"
env JF_PRUNE_REPO="$UNREACHABLE" JF_PRUNE_BOARD=sandbox JF_PRUNE_LOG="$LOG" \
    JF_PRUNE_EXTRA_ROOTS="$SB/strayroot" \
    bash "$SCRIPT" --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a FAILED git fetch is exit 1 — the run's verdict cannot be trusted" "1" "$RC"
contains "...and says so in the log" "$(cat "$LOG")" "FAIL  git fetch failed"
contains "...and the summary records it" "$(cat "$LOG")" "fetch_failed=1"

# 8e. --no-fetch is NOT a failure: the caller asked for it, and the log says so.
: >"$LOG"
env JF_PRUNE_REPO="$UNREACHABLE" JF_PRUNE_BOARD=sandbox JF_PRUNE_LOG="$LOG" \
    JF_PRUNE_EXTRA_ROOTS="$SB/strayroot" \
    bash "$SCRIPT" --no-fetch --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "--no-fetch is a deliberate judgement on the refs as they are: exit 0" "0" "$RC"
contains "...and the log still states it" "$(cat "$LOG")" "--no-fetch"

echo "--- a repo path it cannot use, and the no-fetch path ---"
OUT="$(run_prune --repo "$SB/does-not-exist" 2>&1)"; RC=$?
check "a missing repo exits 0 without touching anything" "0" "$RC"
contains "and says why" "$OUT" "no worktrees dir at"
: >"$LOG"
run_prune --no-fetch --dry-run --min-age-hours 24 >/dev/null 2>&1
contains "--no-fetch says the judgement is on unfetched refs" "$(cat "$LOG")" "--no-fetch"

echo
printf 'prune-kanban-worktrees self-test: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
exit 0
