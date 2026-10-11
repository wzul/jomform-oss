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
#     * an UNBORN HEAD — a valid gitdir whose HEAD points at a branch that no longer
#       exists, the shape a worktree takes when its branch is deleted — is a
#       "cannot judge this worktree" FAILURE (exit 1), not a silent keep under a
#       reason taken from the REPO's HEAD (card t_3fa15d1c), in both loops;
#     * a STRAY held by a live process whose argv carries its path SURVIVES while
#       that process is there, and the SAME stray is removed once it is gone. The
#       stray loop's live-process guard used to be called with `a |b ` as if the
#       predicate were an alternation; it is a literal substring test, so the guard
#       was dead code and a stray with a live build inside it was removed;
#     * a directory that is the WORKING DIRECTORY of a live process survives while
#       that process is there — the shape a real `cd <wt> && npm run build` has,
#       where the shell EXECS the build so the path is in the kernel's cwd and in NO
#       argv — and the same directory is removed once it is gone. A live pid whose
#       cwd cannot be read is a loud cannot-judge (exit 1), never a silent keep and
#       never a removal, while a ZOMBIE's unreadable cwd is skipped; the build-artefact
#       reap (`rm -rf web/node_modules`) carries the same guard (card t_ac768fb9);
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
#   removal, an unreadable card, a worktree whose HEAD cannot be resolved, a failed
#   fetch. Every one of those is reproduced here as an OUTCOME (exit status read off
#   a real run), including the pair that
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
#
#    The `grep -q .` here is the pre-fix shape being PRESERVED, so it is left as a
#    pipe on purpose (card t_39b04c34): the fix under control is the VERDICT rule
#    below, not this reader. It is a display of the old predicate on the sandbox's
#    handful of refs — no assertion depends on its exit status — so the SIGPIPE
#    false-negative this card fixes elsewhere cannot silently corrupt a check here.
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

echo "--- an UNBORN HEAD is a cannot-judge failure, not a wrong-reason keep ---"
# 8f. A card worktree whose HEAD is UNBORN — a valid gitdir whose `HEAD` points at
#     a branch that no longer exists, which is what a lane worktree becomes when its
#     branch is deleted (`git branch -D`, or a `fetch --prune` that drops the ref) —
#     must reach the SAME exit-1 branch as an unreadable card: with no commit of its
#     own there is nothing to judge durability on, so the run cannot decide (card
#     t_3fa15d1c).
#
#     It reached nothing before this fix, and the reason is a STDOUT difference, not
#     a difference in whether HEAD resolves:
#         plain  `git rev-parse HEAD`         -> rc 128, stdout = the literal `HEAD`
#         fixed  `git rev-parse --verify HEAD` -> rc 128, stdout = empty
#     The old `[ -z "$head" ]` guard could therefore never fire, and the literal
#     `HEAD` was handed to `durable_verdict`, where it resolves IN THE REPO'S
#     CONTEXT — a rev with nothing to do with the worktree decided its fate.
#     Measured on the host before the fix (same worktree both times): repo HEAD on an
#     unpushed local branch logged `head=HEAD only on LOCAL branch 'local-unpushed'`;
#     repo HEAD on main logged `has 1 uncommitted change(s)` (with no HEAD, `git
#     status --porcelain` reports every index entry as `A `). Both exited 0, kept the
#     worktree forever, and named a reason that was about the repo — or about a
#     phantom edit. That is the "the pruner silently stopped reclaiming this
#     worktree" shape the exit-1 contract exists to surface.
#
#     This runs in its OWN repo: the assertions above read exact `failures=` and
#     `at_risk=` counts off the shared sandbox, and a worktree that can never be
#     judged would red every one of them. It carries the two HEALTHY shapes too,
#     because the fix must not turn every worktree into a FAIL: a detached and a
#     branch-headed worktree on genuinely pushed work must still resolve to the same
#     sha and still be removed with the same verdict.
UNBORN_SB="$SB/unborn"
UNBORN_REPO="$UNBORN_SB/repo"
UNBORN_ORIGIN="$UNBORN_SB/origin.git"
UNBORN_LOG="$UNBORN_SB/prune.log"
mkdir -p "$UNBORN_REPO" "$UNBORN_SB/strayroot"
git init -q --bare "$UNBORN_ORIGIN"
git -C "$UNBORN_REPO" init -q -b main
printf 'base\n' >"$UNBORN_REPO/README"
git -C "$UNBORN_REPO" add README
git -C "$UNBORN_REPO" -c user.email=t@t -c user.name=t commit -q -m base
git -C "$UNBORN_REPO" remote add origin "$UNBORN_ORIGIN"
git -C "$UNBORN_REPO" push -q -u origin main
git -C "$UNBORN_REPO" fetch -q origin
ucommit() { # <message> -> a commit whose tree is main's, parented on main
  git -C "$UNBORN_REPO" -c user.email=t@t -c user.name=t \
    commit-tree "$(git -C "$UNBORN_REPO" rev-parse 'main^{tree}')" -p main -m "$1"
}
# The healthy shapes' commits, each pushed to a real origin branch => pushed-branch.
U13_SHA="$(ucommit 'healthy detached work')"
git -C "$UNBORN_REPO" push -q origin "$U13_SHA:refs/heads/wt/t_00000013-pushed"
U14_SHA="$(ucommit 'healthy branch-headed work')"
git -C "$UNBORN_REPO" push -q origin "$U14_SHA:refs/heads/wt/t_00000014-pushed"
USTRAY_SHA="$(ucommit 'healthy stray work')"
git -C "$UNBORN_REPO" push -q origin "$USTRAY_SHA:refs/heads/wt/jf-healthy-pushed"
git -C "$UNBORN_REPO" fetch -q origin

# The unborn card worktree: created as a real worktree, then its HEAD is left
# pointing at a branch that does not exist. That IS the shape the card measured —
# the gitdir stays valid, only the ref is missing.
UNBORN_TASK="t_00000012"
UNBORN_WT="$UNBORN_REPO/.worktrees/$UNBORN_TASK"
git -C "$UNBORN_REPO" worktree add -q --detach "$UNBORN_WT" "$USTRAY_SHA"
UNBORN_REF="refs/heads/wt/$UNBORN_TASK-deleted"
printf 'ref: %s\n' "$UNBORN_REF" >"$(git -C "$UNBORN_WT" rev-parse --absolute-git-dir)/HEAD"
# ...and the same shape as a stray (outside .worktrees, no card reference), aged so
# only its HEAD can keep it: strays are judged on the directory's mtime.
UNBORN_STRAY="$UNBORN_SB/strayroot/jf-unborn"
UNBORN_STRAY_REF="refs/heads/wt/jf-unborn-deleted"
git -C "$UNBORN_REPO" worktree add -q --detach "$UNBORN_STRAY" "$USTRAY_SHA"
printf 'ref: %s\n' "$UNBORN_STRAY_REF" >"$(git -C "$UNBORN_STRAY" rev-parse --absolute-git-dir)/HEAD"
touch -d '2020-01-01' "$UNBORN_STRAY"
# The two healthy shapes, and a healthy stray.
git -C "$UNBORN_REPO" worktree add -q --detach "$UNBORN_REPO/.worktrees/t_00000013" "$U13_SHA"
git -C "$UNBORN_REPO" worktree add -q -b wt/t_00000014-pushed "$UNBORN_REPO/.worktrees/t_00000014" "$U14_SHA"
UNBORN_STRAY_OK="$UNBORN_SB/strayroot/jf-healthy"
git -C "$UNBORN_REPO" worktree add -q --detach "$UNBORN_STRAY_OK" "$USTRAY_SHA"
touch -d '2020-01-01' "$UNBORN_STRAY_OK"
for id in "$UNBORN_TASK" t_00000013 t_00000014; do
  printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$id"
done

# The premise, MEASURED rather than assumed: the gitdir is valid, the ref it names
# really is absent, and the two reads of HEAD differ exactly as the fix claims —
# otherwise the outcome assertions below could pass for an unrelated reason.
check "premise: the unborn worktree's gitdir is a real directory" "yes" \
  "$( [ -d "$(git -C "$UNBORN_WT" rev-parse --absolute-git-dir 2>/dev/null)" ] && echo yes || echo no )"
check "premise: ...and the ref its HEAD names does not exist" "no" \
  "$( git -C "$UNBORN_REPO" show-ref --verify --quiet "$UNBORN_REF" 2>/dev/null && echo yes || echo no )"
check "premise: the OLD read of that HEAD yields the literal string 'HEAD'" "HEAD" \
  "$(git -C "$UNBORN_WT" rev-parse HEAD 2>/dev/null || echo "")"
check "premise: ...which is NOT empty, so the old -z guard could never fire" "no" \
  "$( [ -z "$(git -C "$UNBORN_WT" rev-parse HEAD 2>/dev/null || echo "")" ] && echo yes || echo no )"
check "the script's own HEAD read yields nothing for it, so the guard fires" "" \
  "$(lib "worktree_head '$UNBORN_WT'")"

# The healthy shapes the fix must leave alone are measured HERE, while they are
# still on disk (the run below removes them): the read the pruner actually uses must
# give the same sha as a plain read for a detached worktree and for a branch-headed
# one, and it must be the commit the worktree was made at.
check "a healthy DETACHED worktree still resolves to its own commit" "$U13_SHA" \
  "$(lib "worktree_head '$UNBORN_REPO/.worktrees/t_00000013'")"
check "...i.e. the same sha a plain read gives (the fix changed no healthy read)" \
  "$(git -C "$UNBORN_REPO/.worktrees/t_00000013" rev-parse HEAD 2>/dev/null || echo "")" \
  "$(lib "worktree_head '$UNBORN_REPO/.worktrees/t_00000013'")"
check "a healthy BRANCH-HEADED worktree still resolves to its own commit" "$U14_SHA" \
  "$(lib "worktree_head '$UNBORN_REPO/.worktrees/t_00000014'")"
check "...i.e. the same sha a plain read gives" \
  "$(git -C "$UNBORN_REPO/.worktrees/t_00000014" rev-parse HEAD 2>/dev/null || echo "")" \
  "$(lib "worktree_head '$UNBORN_REPO/.worktrees/t_00000014'")"

run_unborn() { # extra args…
  env PATH="$SB/bin:$PATH" \
      JF_FAKE_HERMES_DIR="$SB/fake-hermes" \
      JF_PRUNE_REPO="$UNBORN_REPO" JF_PRUNE_BOARD=sandbox JF_PRUNE_LOG="$UNBORN_LOG" \
      JF_PRUNE_EXTRA_ROOTS="$UNBORN_SB/strayroot" \
      bash "$SCRIPT" "$@"
}
: >"$UNBORN_LOG"
run_unborn --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a card worktree whose HEAD is unborn makes the run exit 1, not 0" "1" "$RC"
contains "...logged as the cannot-resolve-HEAD FAIL, not as a keep with a reason" \
  "$(cat "$UNBORN_LOG")" "FAIL  $UNBORN_TASK (cannot resolve HEAD"
check "...and it is explicitly NOT a KEEP" "0" "$(grep -c "KEEP  $UNBORN_TASK" "$UNBORN_LOG")"
check "...and it is NOT reported as an UNSAFE dirty tree either (the old run's wrong reason)" "0" \
  "$(grep -c "UNSAFE $UNBORN_TASK" "$UNBORN_LOG")"
check "...and the worktree is still on disk (never act on one you could not judge)" "yes" \
  "$( [ -d "$UNBORN_WT" ] && echo yes || echo no )"
# ...and the stray loop's own copy of the branch carries the same correction.
contains "a STRAY whose HEAD is unborn is the same failure, named on the stray" \
  "$(cat "$UNBORN_LOG")" "FAIL  jf-unborn (no HEAD"
check "...and it is NOT a KEEP" "0" "$(grep -c 'KEEP  jf-unborn' "$UNBORN_LOG")"
check "...and the stray is still on disk" "yes" \
  "$( [ -d "$UNBORN_STRAY" ] && echo yes || echo no )"
# The count is exact, so this is an assertion about THESE two branches rather than
# about "an exit 1 happened somewhere": the healthy shapes below all resolve.
contains "...and the summary counts exactly the two unborn reads as failures" \
  "$(cat "$UNBORN_LOG")" "failures=2"
check "...with no other FAIL anywhere in the run" "2" "$(grep -c 'FAIL ' "$UNBORN_LOG")"

# The fix must not turn every worktree into a FAIL — the run above kept the two
# unborn reads as its ONLY failures and still removed every healthy shape, with the
# same verdict each had before (measured on disk).
check "the healthy detached worktree was still REMOVED" "no" \
  "$( [ -d "$UNBORN_REPO/.worktrees/t_00000013" ] && echo yes || echo no )"
check "the healthy branch-headed worktree was still REMOVED" "no" \
  "$( [ -d "$UNBORN_REPO/.worktrees/t_00000014" ] && echo yes || echo no )"
check "the healthy stray was still REMOVED" "no" \
  "$( [ -d "$UNBORN_STRAY_OK" ] && echo yes || echo no )"
line_of() { grep -F -- "$1" "$UNBORN_LOG" | sed -n '1p'; }
line_has() { case "$2" in *"$1"*) echo yes;; *) echo no;; esac; }
check "the healthy detached removal records the pushed verdict, as before" "yes" \
  "$(line_has 'durable=pushed-branch' "$(line_of "RM    t_00000013" "$UNBORN_LOG")")"
check "the healthy branch-headed removal records the pushed verdict, as before" "yes" \
  "$(line_has 'durable=pushed-branch' "$(line_of "RM    t_00000014" "$UNBORN_LOG")")"
check "...and the healthy stray's removal does too" "yes" \
  "$(line_has 'durable=pushed-branch' "$(line_of 'RM    jf-healthy (stray' "$UNBORN_LOG")")"

# Leave the sandbox as this section found it. A worktree whose HEAD cannot be
# resolved cannot be removed BY GIT either, so it is cleaned the way an operator
# would: drop the directory and prune the stale registration. Asserted, not assumed.
rm -rf "$UNBORN_WT" "$UNBORN_STRAY"
git -C "$UNBORN_REPO" worktree prune 2>/dev/null
rm -f "$SB/fake-hermes/$UNBORN_TASK"
check "...and the block leaves the unborn repo's worktree list with no unborn registration" "0" \
  "$(git -C "$UNBORN_REPO" worktree list --porcelain | grep -c -F -e "$UNBORN_WT" -e "$UNBORN_STRAY")"

OUT="$(run_prune --repo "$SB/does-not-exist" 2>&1)"; RC=$?
check "a missing repo exits 0 without touching anything" "0" "$RC"
contains "and says why" "$OUT" "no worktrees dir at"
: >"$LOG"
run_prune --no-fetch --dry-run --min-age-hours 24 >/dev/null 2>&1
contains "--no-fetch says the judgement is on unfetched refs" "$(cat "$LOG")" "--no-fetch"

echo "--- the four remaining exit-code branches, each pinned by its own real run ---"
# The contract at the top of the pruner names four failure conditions and one
# bad-argument code. Three of them were pinned above (the CARD loop's refused
# removal in 8c, an unreadable card, a failed fetch in 8d). The OTHER four were
# live behaviour with nothing behind them, so a one-armed mutant of any one of them
# still reported this suite green (card t_7360634f):
#   * the CARD loop's unresolvable HEAD      -> `failures+1` -> exit 1
#   * the STRAY loop's unresolvable HEAD     -> `failures+1` -> exit 1
#   * the STRAY loop's refused removal       -> `failures+1` -> exit 1
#   * an unknown argument                    -> exit 2
# Each is pinned below the way the rest of this file pins things: an OUTCOME read
# off a real run (exit status, the log line, and what is left on disk) — never a
# grep of the script's source, which would pass against a branch that is present
# but unable to change a run's result.

# 9a. Bad arguments: exit 2, and NOTHING was run. The unknown argument is rejected
#     while parsing, before main() is reached, so the empty log is what separates
#     "refused the invocation" from "started, then fell over" — both of which a
#     bare `rc != 0` would accept.
: >"$LOG"
OUT="$(run_prune --definitely-not-an-option 2>&1)"; RC=$?
check "an unknown argument is exit 2 (a bad invocation is not a run failure)" "2" "$RC"
contains "...and the rejection names the offending argument" "$OUT" "unknown arg: --definitely-not-an-option"
check "...and nothing ran at all (the log is still empty)" "" "$(cat "$LOG")"

# 9b. The STRAY loop's refused removal. 8c pins the same failure in the CARD loop;
#     the stray loop carries its own copy of the branch and its own counter, and
#     nothing pinned it. A locked stray reproduces it deterministically (git 2.43:
#     `remove --force` cannot remove a locked tree, only `--force --force` can).
#     No other branch can fail in this state, so `failures=1` is exact — that is
#     what makes it an assertion about THIS branch rather than about "an exit 1
#     happened somewhere".
STRAY_LOCKED="$SB/strayroot/jf-locked"
git -C "$REPO" worktree add -q --detach "$STRAY_LOCKED" "$PUSHED_SHA"
touch -d '2020-01-01' "$STRAY_LOCKED"
git -C "$REPO" worktree lock --reason sandbox "$STRAY_LOCKED"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a STRAY removal that git REFUSES is exit 1" "1" "$RC"
contains "...and the refusal is logged as a FAIL on the stray, not as a keep" \
  "$(cat "$LOG")" "FAIL  jf-locked (could not remove stray worktree)"
check "...the locked stray is still on disk" "yes" "$(stray_survives jf-locked)"
contains "...and the summary counts exactly that one failure" "$(cat "$LOG")" "failures=1"
git -C "$REPO" worktree unlock "$STRAY_LOCKED" 2>/dev/null
check "the lock is what caused it (unlocking makes the same stray removable)" "no" \
  "$( git -C "$REPO" worktree remove --force "$STRAY_LOCKED" >/dev/null 2>&1; [ -d "$STRAY_LOCKED" ] && echo yes || echo no )"

# 9c. The STRAY loop's unresolvable HEAD. Its twin in the card loop is 9d; the stray
#     loop reads HEAD through its own copy of the branch.
#
#     HOW THE HEAD IS BROKEN DECIDES WHETHER THIS BRANCH IS REACHED AT ALL, and that
#     was measured, not assumed. The pruner's read is
#     `head=$(git -C "$wt" rev-parse HEAD 2>/dev/null || echo "")`, so the branch
#     fires exactly when that read yields no STDOUT — and the two candidate shapes
#     differ in stdout, not in whether HEAD resolves:
#       * the worktree's `.git` FILE overwritten with `ref: refs/heads/<missing>`
#         (the shape the card measured, used here): git writes `fatal: invalid
#         gitfile format` to stderr and NOTHING to stdout, so `head` is empty and
#         this branch fires. The directory and every file in it are untouched, which
#         is what makes the on-disk assertions below meaningful.
#       * a VALID gitdir whose HEAD points at a missing branch — the shape a worktree
#         whose branch was deleted leaves behind — does NOT reach this branch:
#         `git rev-parse HEAD` prints the literal `HEAD` on stdout, so the run then
#         judges the worktree by a rev belonging to the REPO rather than refusing to
#         judge it. That is a separate defect with its own card; this assertion
#         deliberately claims only the shape measured here.
STRAY_NOREF="$SB/strayroot/jf-noref"
git -C "$REPO" worktree add -q --detach "$STRAY_NOREF" "$PUSHED_SHA"
touch -d '2020-01-01' "$STRAY_NOREF"
printf 'ref: refs/heads/no-such-branch\n' >"$STRAY_NOREF/.git"
# The premise, measured: the directory is still there and the pruner's OWN read of
# its HEAD really does come back empty — otherwise the assertion below could be
# green for a reason that has nothing to do with the branch.
check "premise: the stray's directory exists but the pruner's read of its HEAD is empty" "yes" \
  "$( [ -d "$STRAY_NOREF" ] && [ -z "$(git -C "$STRAY_NOREF" rev-parse HEAD 2>/dev/null)" ] && echo yes || echo no )"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a STRAY whose HEAD cannot be resolved is exit 1" "1" "$RC"
contains "...and it is a FAIL, not a silent keep" "$(cat "$LOG")" "FAIL  jf-noref (no HEAD"
check "...and it is NOT reported as a KEEP (a keep would say it judged a worktree it could not read)" "0" \
  "$(grep -c 'KEEP  jf-noref' "$LOG")"
check "...the stray is still on disk (never act on a worktree you could not judge)" "yes" "$(stray_survives jf-noref)"
contains "...and the summary counts exactly that one failure" "$(cat "$LOG")" "failures=1"
# A worktree whose git link is broken cannot be removed BY GIT (the same broken read
# makes every git command in it fail), so the sandbox is cleaned the way an operator
# would: drop the directory and prune the stale registration. Asserted, not assumed —
# a leftover here would red the exit-code assertions on every later run.
rm -rf "$STRAY_NOREF"
git -C "$REPO" worktree prune 2>/dev/null
check "...and the sandbox is left clean (the stray is gone and no longer registered)" "yes" \
  "$( [ ! -d "$STRAY_NOREF" ] && [ -z "$(git -C "$REPO" worktree list --porcelain | grep -F "$STRAY_NOREF")" ] && echo yes || echo no )"

# 9d. The CARD loop's unresolvable HEAD. A card the script CAN read, terminal and
#     comfortably past --min-age-hours, whose worktree's HEAD is gone: with no HEAD
#     there is no commit to judge durability on, so the run cannot decide — and a
#     silent KEEP there is the "the pruner quietly stopped reclaiming this worktree"
#     shape the exit-1 contract exists for. Asserted as a FAIL AND explicitly as NOT
#     a KEEP: the branch the card measured under its mutant logged the very same FAIL
#     line while the run exited 0, so the log line alone pins nothing.
NOREF_TASK="t_00000011"
git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/$NOREF_TASK" "$PUSHED_SHA"
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$NOREF_TASK"
printf 'ref: refs/heads/no-such-branch\n' >"$REPO/.worktrees/$NOREF_TASK/.git"
check "premise: the card is readable and terminal" "done" "$(sed -n '1p' "$SB/fake-hermes/$NOREF_TASK")"
check "premise: its worktree exists but the pruner's read of its HEAD is empty" "yes" \
  "$( [ -d "$REPO/.worktrees/$NOREF_TASK" ] && [ -z "$(git -C "$REPO/.worktrees/$NOREF_TASK" rev-parse HEAD 2>/dev/null)" ] && echo yes || echo no )"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "a readable card whose worktree HEAD cannot be resolved is exit 1" "1" "$RC"
contains "...and says it cannot judge this worktree" \
  "$(cat "$LOG")" "FAIL  $NOREF_TASK (cannot resolve HEAD"
check "...and it is NOT a KEEP (the same branch reported as a silent keep is invisible to a caller)" "0" \
  "$(grep -c "KEEP  $NOREF_TASK" "$LOG")"
check "...the worktree is still on disk (never act on a worktree you could not judge)" "yes" "$(survives "$NOREF_TASK")"
contains "...and the summary counts exactly that one failure" "$(cat "$LOG")" "failures=1"
# Leave the sandbox as this section found it: a later run must not inherit a
# worktree that can never be judged (it would red every exit-code assertion above).
rm -rf "$REPO/.worktrees/$NOREF_TASK"
git -C "$REPO" worktree prune 2>/dev/null
rm -f "$SB/fake-hermes/$NOREF_TASK"
check "...and the sandbox is left clean (the extra worktree is gone)" "no" "$(survives "$NOREF_TASK")"

echo "--- the STRAY loop's live-process guard (a dead guard, now pinned) ---"
# 10. THE CARD'S DEFECT (t_8cdb8111), as an outcome on disk. The stray loop's
#     live-process guard was called as
#         has_process_matching "worktrees/$name |$wt "
#     — as if the argument were an alternation. The predicate tests its argument
#     with awk `index($0, pat)`: a LITERAL substring, no regex, so ` |` had to
#     appear verbatim in some argv for the guard to fire. Nothing ever does, so the
#     guard was dead code: measured on the host, a stray with a stand-in still alive
#     inside it (`cd <stray> && npm run build`) was REMOVED and the run logged
#     `RM    jf-live (stray …)`. The CARD loop's two call sites each pass a single
#     literal and were never affected, which is why only the stray loop is pinned
#     here.
#
#     The stray's NAME is unique to this run by construction — the whole sandbox is
#     a fresh `mktemp -d` — so a peer suite's stand-in cannot satisfy this run's
#     pattern. That is the same cross-run hazard the card loop's pid-derived worker
#     id exists for (section 6), handled here by the sandbox being per-run.

# The predicate's own shape, pinned on a SYNTHETIC table so this is deterministic
# and load-independent: the pre-fix pattern is a literal no argv can hold, the path
# literal alone is what fires, and the `.worktrees/<name>` literal cannot be what
# fires for a stray (a stray is not under .worktrees). The pattern travels through
# the ENVIRONMENT, never on the caller's argv: a pattern on the command line makes
# the predicate match its OWN CALLER, which is how a first attempt at the probe
# read "the guard fired" when nothing but the probe itself was there.
guard_literal() { # <synthetic argv row> <literal> — the script's predicate, rc on stdout
  env JF_PRUNE_LIB_ONLY=1 JF_STUB_PS_ROWS="$1" JF_PAT="$2" PATH="$STUBDIR:$PATH" \
    bash -c '. "$0"; has_process_matching "$JF_PAT"; echo $?' "$SCRIPT" 2>/dev/null | sed -n '$p'
}
# The same predicate against the REAL process table, for the premise that the
# stand-in is (and then is not) there. NOT `ps -eo args= | grep -F -- <literal>`:
# grep's own argv carries the literal, so ps lists it and the check matches ITSELF
# — an assertion of absence written that way can never fail. The literal travels
# through the ENVIRONMENT instead, and the predicate's own `awk -v pat=…` line is
# skipped by its scanner exclusion.
guard_real() { # <literal> -> match | nomatch, via the script's own predicate
  env JF_PRUNE_LIB_ONLY=1 JF_PAT="$1" bash -c \
    '. "$0"; if has_process_matching "$JF_PAT"; then echo match; else echo nomatch; fi' "$SCRIPT"
}
STRAY_LIVE="$SB/strayroot/jf-live"
git -C "$REPO" worktree add -q --detach "$STRAY_LIVE" "$STRAY_PUSHED_SHA"
touch -d '2020-01-01' "$STRAY_LIVE"
# A realistic lane invocation, so the argv carries the path the way a lane's shell
# does (the shape the card measured): `cd <stray> && npm run build`.
STRAY_LIVE_ARGV="cd $STRAY_LIVE && npm run build"
check "premise: the stray's work is durable (pushed), so ONLY the process can be keeping it" \
  "pushed-branch" "$(lib "durable_verdict '$REPO' '$STRAY_PUSHED_SHA'")"
check "control: the pre-fix 'worktrees/<name> |<path> ' argument is one literal, so it does NOT match an argv holding the path" \
  "1" "$(guard_literal "$STRAY_LIVE_ARGV" "worktrees/jf-live |$STRAY_LIVE ")"
check "the path literal ALONE does match that same argv (the predicate is not broken)" \
  "0" "$(guard_literal "$STRAY_LIVE_ARGV" "$STRAY_LIVE ")"
check "...while the '.worktrees/<name>' literal does not (a stray is not under .worktrees)" \
  "1" "$(guard_literal "$STRAY_LIVE_ARGV" "worktrees/jf-live ")"

# The stand-in, alive in the real process table for the run below.
env bash -c "exec -a \"$STRAY_LIVE_ARGV\" sleep 40" &
STRAY_LIVE_PID=$!
sleep 0.6
check "premise: the stand-in is visible to the pruner's OWN predicate (real process table)" "match" \
  "$(guard_real "$STRAY_LIVE_ARGV")"

: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "a STRAY with a live process whose argv carries its path SURVIVES" "yes" "$(stray_survives jf-live)"
contains "...and the refusal is logged as a KEEP on the process" \
  "$(cat "$LOG")" "KEEP  jf-live (process references it)"
check "...and nothing tried to remove it" "0" "$(grep -c 'RM    jf-live' "$LOG")"

# ...and it is the PROCESS holding it, not a guard that refuses everything: with the
# process gone the SAME stray is removable — the other half of the card's acceptance.
kill "$STRAY_LIVE_PID" 2>/dev/null
wait "$STRAY_LIVE_PID" 2>/dev/null
check "premise: the SAME stray is still on disk, so the second half starts from it" "yes" "$(stray_survives jf-live)"
check "premise: its stand-in is gone from the process table" "nomatch" \
  "$(guard_real "$STRAY_LIVE_ARGV")"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "with that process gone, the SAME stray is removed" "no" "$(stray_survives jf-live)"
contains "...and the removal records its verdict" "$(cat "$LOG")" "RM    jf-live (stray"

echo
echo "--- the cwd half of the live-process guard: the exec'd build argv cannot describe ---"
# 11. THE CARD'S DEFECT (t_ac768fb9), as outcomes on disk and on a real process
#     table. SAFETY 4 is "no live process is using it", and the guard read ARGV
#     ONLY. A real build is `cd <wt> && npm run build`: the shell EXECS the last
#     command of the `&&` list, so the path leaves argv entirely — the process is
#     `npm run build`, with cwd = <wt>, and npm, node and everything they start
#     inherit that cwd. NO argv names the worktree, so the argv predicate cannot see
#     it, and a stray (or a card worktree) was removed out from under a running
#     build: `rm -rf` of 442M node_modules + 146M .next. In the CARD loop the worker
#     guard masked it (`work kanban task <id>` is in a worker's argv); a stray has no
#     worker, so its fate was age + durability + `git status` alone.
#
#     The three outcomes of the new read are pinned here — the directory SURVIVES
#     while the stand-in is alive (with a KEEP line naming the cwd as the reason and
#     removed=0/stray_removed=0 in the summary), the SAME directory is removed once
#     it is gone (the guard is not a blanket refusal), and a LIVE pid whose cwd
#     cannot be read is a loud cannot-judge — neither a silent keep nor a removal.
#     The build-ARTEFACT path (`rm -rf web/node_modules`, same class of process,
#     same damage) carries the same guard and is pinned with it.
#
#     The unreadable-cwd case CANNOT be produced on this host: root can read every
#     live process's cwd here, and the only reads that fail are zombies (measured: 15
#     of 354 pids, every one `Z`). So that case — and the ZOMBIE case the read must
#     not mistake it for — is pinned on a SYNTHETIC process table: a root of
#     `<pid>/cwd` entries plus a `<pid>/status` with a `State:` line, which is all
#     the predicate reads. Its pids are drawn above the host's pid_max, so a
#     synthetic entry can never be this suite's own process.
#
# The script's OWN predicate, driven through the ENVIRONMENT (a literal on the
# caller's argv would let the scan match its own caller) -> inside | none | denied:<pid>
cwd_check() { # <dir> [proc-root]
  env JF_PRUNE_LIB_ONLY=1 JF_DIR="$1" JF_PROC_ROOT="${2:-/proc}" bash -c '
    . "$0"
    out="$(has_process_cwd_matching "$JF_DIR")"; rc=$?
    case "$rc" in
      0) echo inside ;;
      2) printf "denied:%s\n" "$out" ;;
      *) echo none ;;
    esac
  ' "$SCRIPT" 2>/dev/null | sed -n '$p'
}
# The `$$` rule, pinned the only way it CAN be: a table whose single entry is the
# asking process's own pid and cwd. Without the skip this reads "inside" — the scan
# would be matching the process that is asking the question. <scratch-root> must be
# FRESH (one call per run): a stale `<pid>` directory from an earlier call would be
# an entry for a pid that has since died, whose cwd link still points at the target.
cwd_self_check() { # <dir> <fresh scratch-root>
  env JF_PRUNE_LIB_ONLY=1 JF_DIR="$1" JF_SELF_ROOT="$2" bash -c '
    . "$0"
    root="$JF_SELF_ROOT/$$"
    mkdir -p "$root"
    ln -s "$JF_DIR" "$root/cwd"
    printf "State:\tS (sleeping)\n" >"$root/status"
    export JF_PROC_ROOT="$JF_SELF_ROOT"
    out="$(has_process_cwd_matching "$JF_DIR")"; rc=$?
    case "$rc" in
      0) echo inside ;;
      2) printf "denied:%s\n" "$out" ;;
      *) echo none ;;
    esac
  ' "$SCRIPT" 2>/dev/null | sed -n '$p'
}
# The stand-ins cannot be started synchronously (`&`), and the premises below are
# about where they ARE, so they are waited for rather than slept on: a loaded host
# made the first version of this section flaky (3 of 4 reds, all in the premises).
wait_for_cwd() { # <pid> <dir> [tries of 0.1s] -> 0 once /proc/<pid>/cwd is <dir>
  local i=0
  while [ "$i" -lt "${3:-100}" ]; do
    [ "$(readlink "/proc/$1/cwd" 2>/dev/null)" = "$2" ] && return 0
    sleep 0.1
    i=$((i+1))
  done
  return 1
}

CWD_ARGV="npm run build"
CWD_SHA="$(ucommit 'cwd-held work')"
git -C "$UNBORN_REPO" push -q origin "$CWD_SHA:refs/heads/wt/t_00000015-cwd"
git -C "$UNBORN_REPO" fetch -q origin
CWD_TASK="t_00000015"
CWD_WT="$UNBORN_REPO/.worktrees/$CWD_TASK"
git -C "$UNBORN_REPO" worktree add -q --detach "$CWD_WT" "$CWD_SHA"
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$CWD_TASK"
CWD_STRAY="$UNBORN_SB/strayroot/jf-cwdlive"
git -C "$UNBORN_REPO" worktree add -q --detach "$CWD_STRAY" "$CWD_SHA"
touch -d '2020-01-01' "$CWD_STRAY"
# A third worktree, for the same guard on the ARTEFACT path: `rm -rf web/node_modules`
# under a live build damages the build in flight rather than the worktree.
CWD_ART_TASK="t_00000016"
CWD_ART_WT="$UNBORN_REPO/.worktrees/$CWD_ART_TASK"
git -C "$UNBORN_REPO" worktree add -q --detach "$CWD_ART_WT" "$CWD_SHA"
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/$CWD_ART_TASK"
mkdir -p "$CWD_ART_WT/web/node_modules"
printf 'built\n' >"$CWD_ART_WT/web/node_modules/.jf-suite-marker"
touch -d '2020-01-01' "$CWD_ART_WT"

# The stand-ins are shaped exactly like the real thing — `cd <dir> && exec -a "npm
# run build" sleep` — so the shell EXECS and the argv is `npm run build 300` while
# the path lives in the kernel's cwd and nowhere else. (The sleep is long on purpose:
# an earlier version used 60s, which a loaded host could outlive before the run below,
# and a stand-in that dies early reads as "the guard refuses nothing".)
env bash -c "cd \"$CWD_STRAY\" && exec -a \"$CWD_ARGV\" sleep 300" &
CWD_STRAY_PID=$!
env bash -c "cd \"$CWD_WT\" && exec -a \"$CWD_ARGV\" sleep 300" &
CWD_WT_PID=$!
env bash -c "cd \"$CWD_ART_WT\" && exec -a \"$CWD_ARGV\" sleep 300" &
CWD_ART_PID=$!

# Premises, measured: the stand-ins really ARE inside those directories, and NO argv
# carries the path. The first is WAITED for (the stand-ins are background jobs, and a
# loaded host needs more than a fixed sleep); the second half is the premise the card
# asks for — if an argv did carry the path, the EXISTING argv guard would be what kept
# them and these assertions would prove nothing about the cwd read. It is measured
# through the script's OWN argv predicate on the real table (guard_real, literal via
# the ENVIRONMENT).
check "premise: the stray's stand-in is inside it (readlink /proc/<pid>/cwd)" "yes" \
  "$( wait_for_cwd "$CWD_STRAY_PID" "$CWD_STRAY" && echo yes || echo no )"
check "premise: the card worktree's stand-in is inside IT" "yes" \
  "$( wait_for_cwd "$CWD_WT_PID" "$CWD_WT" && echo yes || echo no )"
check "premise: the artefact worktree's stand-in is inside IT" "yes" \
  "$( wait_for_cwd "$CWD_ART_PID" "$CWD_ART_WT" && echo yes || echo no )"
check "premise: the stand-in's argv does NOT carry the stray's path (only a cwd read can see it)" \
  "nomatch" "$(guard_real "$CWD_STRAY ")"
check "premise: ...nor the '.worktrees/<name>' form of it" "nomatch" \
  "$(guard_real "worktrees/$(basename "$CWD_STRAY") ")"
check "premise: ...nor the card worktree's path" "nomatch" "$(guard_real "$CWD_WT ")"
check "premise: ...nor its '.worktrees/<name>' form" "nomatch" "$(guard_real "worktrees/$CWD_TASK ")"
check "premise: ...nor the artefact worktree's path" "nomatch" "$(guard_real "$CWD_ART_WT ")"
check "premise: while the argv predicate DOES see the stand-in's own argv (it is not blind to the process)" \
  "match" "$(guard_real "$CWD_ARGV")"

check "the cwd predicate finds a process inside the stray" "inside" "$(cwd_check "$CWD_STRAY")"
check "the cwd predicate finds a process inside the card worktree" "inside" "$(cwd_check "$CWD_WT")"
check "...and says nothing is inside a directory no process is in" "none" "$(cwd_check "$SB/stub")"

# The synthetic table. Each sub-root isolates ONE case, so an assertion cannot pass
# because of a different pid in the same table.
FAKE="$SB/fake-proc"
FAKE_TARGET="$SB/cwd-target"
mkdir -p "$FAKE_TARGET/sub" "$FAKE_TARGET-sibling"
mkdir -p "$FAKE/live/9999001" "$FAKE/zombie-only/9999002" "$FAKE/deny-only/9999003" \
         "$FAKE/sub-only/9999004" "$FAKE/sibling-only/9999005" "$FAKE/empty" "$FAKE/self"
ln -s "$FAKE_TARGET"               "$FAKE/live/9999001/cwd"
printf 'State:\tS (sleeping)\n' >"$FAKE/live/9999001/status"
# The zombie: a cwd entry whose target cannot be read (a dead process's /proc entry
# behaves the same way at the syscall find uses) with `State: Z`.
printf 'not-a-link\n'          >"$FAKE/zombie-only/9999002/cwd"
printf 'State:\tZ (zombie)\n'  >"$FAKE/zombie-only/9999002/status"
# ...the same unreadable cwd, but a LIVE state: this is the cannot-judge.
printf 'not-a-link\n'          >"$FAKE/deny-only/9999003/cwd"
printf 'State:\tS (sleeping)\n' >"$FAKE/deny-only/9999003/status"
ln -s "$FAKE_TARGET/sub"       "$FAKE/sub-only/9999004/cwd"
printf 'State:\tS (sleeping)\n' >"$FAKE/sub-only/9999004/status"
ln -s "$FAKE_TARGET-sibling"   "$FAKE/sibling-only/9999005/cwd"
printf 'State:\tS (sleeping)\n' >"$FAKE/sibling-only/9999005/status"
check "premise: the synthetic 'inside' entry is a cwd link the kernel can read" "$FAKE_TARGET" \
  "$(readlink "$FAKE/live/9999001/cwd")"
check "premise: the synthetic zombie's cwd cannot be read at all" "" \
  "$(readlink "$FAKE/zombie-only/9999002/cwd" 2>/dev/null)"
check "premise: ...and its State: line says Z, so the state is the only thing that can tell it apart" "Z" \
  "$(awk '/^State:/{print $2}' "$FAKE/zombie-only/9999002/status")"
check "premise: the third pid's cwd cannot be read either, but its state is NOT Z" "S" \
  "$(awk '/^State:/{print $2}' "$FAKE/deny-only/9999003/status")"

check "a synthetic live pid inside the target reads 'inside'" "inside" \
  "$(cwd_check "$FAKE_TARGET" "$FAKE/live")"
check "...and a pid sitting one level DOWN counts too (git removes the whole subtree)" "inside" \
  "$(cwd_check "$FAKE_TARGET" "$FAKE/sub-only")"
check "...while a SIBLING that merely shares a prefix does not" "none" \
  "$(cwd_check "$FAKE_TARGET" "$FAKE/sibling-only")"
check "a ZOMBIE's unreadable cwd is SKIPPED, not read as a cannot-judge" "none" \
  "$(cwd_check "$SB/stub" "$FAKE/zombie-only")"
check "...because a LIVE pid whose cwd cannot be read IS the cannot-judge, naming the pid" "denied:9999003" \
  "$(cwd_check "$SB/stub" "$FAKE/deny-only")"
check "a process table that comes back empty is a cannot-judge, NEVER 'nothing is inside'" "denied:?" \
  "$(cwd_check "$SB/stub" "$FAKE/empty")"
check "the scan does not match the process asking the question (its own cwd, at a fake pid)" "none" \
  "$(cwd_self_check "$SB/stub" "$FAKE/self")"
check "premise: the self-check really did put an entry for its own pid in the table" "yes" \
  "$( [ -n "$(find "$FAKE/self" -maxdepth 2 -name cwd -print -quit)" ] && echo yes || echo no )"

# ── run 1: both directories are the cwd of a live stand-in, so both SURVIVE ──────
: >"$UNBORN_LOG"
run_unborn --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "run 1: the run reclaims nothing and is exit 0 (a keep on purpose is not a failure)" "0" "$RC"
check "the CARD worktree whose directory is a live process's cwd SURVIVES" "yes" \
  "$( [ -d "$CWD_WT" ] && echo yes || echo no )"
contains "...and the refusal names the cwd as the reason" "$(cat "$UNBORN_LOG")" \
  "KEEP  $CWD_TASK (a process's working directory is inside this worktree)"
check "the STRAY whose directory is a live process's cwd SURVIVES" "yes" \
  "$( [ -d "$CWD_STRAY" ] && echo yes || echo no )"
contains "...and its refusal names the cwd too" "$(cat "$UNBORN_LOG")" \
  "KEEP  jf-cwdlive (a process's working directory is inside this stray)"
check "...and nothing tried to remove either" "0" \
  "$(grep -c -e "RM    jf-cwdlive" -e "RM    $CWD_TASK" "$UNBORN_LOG")"
contains "...with removed=0 in the summary" "$(cat "$UNBORN_LOG")" " removed=0 skipped="
contains "...and stray_removed=0" "$(cat "$UNBORN_LOG")" "stray_removed=0"
check "...and the run records no failure at all (an unreadable cwd would be one)" "0" \
  "$(grep -c 'FAIL ' "$UNBORN_LOG")"
check "the third worktree's build artefacts SURVIVE while its build runs (the guard on the reap path)" "yes" \
  "$( [ -f "$CWD_ART_WT/web/node_modules/.jf-suite-marker" ] && echo yes || echo no )"

# ── the other half: it is the PROCESS, not a guard that refuses everything ───────
kill "$CWD_STRAY_PID" "$CWD_WT_PID" "$CWD_ART_PID" 2>/dev/null
wait "$CWD_STRAY_PID" 2>/dev/null
wait "$CWD_WT_PID" 2>/dev/null
wait "$CWD_ART_PID" 2>/dev/null
check "premise: the stand-in's pid is gone from the process table" "no" \
  "$( [ -d "/proc/$CWD_WT_PID" ] && echo yes || echo no )"
check "premise: the cwd predicate now finds nothing in the card worktree" "none" "$(cwd_check "$CWD_WT")"
check "premise: ...nor in the stray" "none" "$(cwd_check "$CWD_STRAY")"
check "premise: and both directories are still on disk, so the second half starts from them" "yes" \
  "$( [ -d "$CWD_WT" ] && [ -d "$CWD_STRAY" ] && echo yes || echo no )"

: >"$UNBORN_LOG"
run_unborn --min-age-hours 24 >/dev/null 2>&1; RC=$?
check "run 2: with the processes gone, the SAME card worktree is removed" "no" \
  "$( [ -d "$CWD_WT" ] && echo yes || echo no )"
check "run 2: ...and the SAME stray is removed" "no" \
  "$( [ -d "$CWD_STRAY" ] && echo yes || echo no )"
contains "...the stray removal records its verdict" "$(cat "$UNBORN_LOG")" "RM    jf-cwdlive (stray"
contains "...and the card removal records the pushed verdict" "$(cat "$UNBORN_LOG")" "durable=pushed-branch"
contains "...with removed=1 in the summary (the guard does not refuse everything)" "$(cat "$UNBORN_LOG")" " removed=1 skipped="
contains "...and stray_removed=1" "$(cat "$UNBORN_LOG")" "stray_removed=1"
check "the third worktree's build artefacts are reaped now that nothing is building" "no" \
  "$( [ -f "$CWD_ART_WT/web/node_modules/.jf-suite-marker" ] && echo yes || echo no )"
check "...while that worktree itself stays: its artefacts are untracked, so it is dirty" "yes" \
  "$( [ -d "$CWD_ART_WT" ] && echo yes || echo no )"
check "...and run 2 exits 0" "0" "$RC"
# Leave the sandbox as this section found it (the dirty worktree cannot be removed by
# the pruner, so it is dropped the way an operator would) — a leftover would show up
# in a later run's counts.
rm -rf "$CWD_ART_WT"
git -C "$UNBORN_REPO" worktree prune 2>/dev/null
rm -f "$SB/fake-hermes/$CWD_ART_TASK"
check "...and the sandbox is left clean (the artefact worktree is gone)" "no" \
  "$( [ -d "$CWD_ART_WT" ] && echo yes || echo no )"

echo
printf 'prune-kanban-worktrees self-test: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
exit 0
