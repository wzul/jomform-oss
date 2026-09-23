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
#   `hermes` answering `kanban show` from a state file, no real board, no network.
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
  if printf '%s' "$2" | grep -qF -- "$3"; then ok "$1"
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
#   t_00000008 busy              pushed, live worker process             -> KEEP (process)
#   t_00000009 unknowable        pushed, no card to read                 -> KEEP (status)
for spec in "t_00000001:$MERGED_SHA" "t_00000002:$MIRROR_SHA" "t_00000003:$PUSHED_SHA" \
            "t_00000004:$LOCAL_SHA" "t_00000005:$PUSHED_SHA" "t_00000006:$PUSHED_SHA" \
            "t_00000007:$PUSHED_SHA" "t_00000008:$PUSHED_SHA" "t_00000009:$PUSHED_SHA"; do
  id="${spec%%:*}"; sha="${spec#*:}"
  git -C "$REPO" worktree add -q --detach "$REPO/.worktrees/$id" "$sha"
done
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
check "the run exits 3 — work exists only locally, for a cron wrapper to alert on" "3" "$RC"

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
contains "...and is reported as unknown, not assumed terminal" \
  "$(cat "$LOG")" "KEEP  t_00000009 (card status=unknown)"

# 6. The live-process guard: a worker holding the card keeps the worktree even
#    though the work is durable and the card is old. The card is made terminal
#    FIRST (it was unreadable above, which is a different guard), and the second
#    run is the one that must keep it.
printf 'done\n%s\n' "$OLD" >"$SB/fake-hermes/t_00000008"
env bash -c 'exec -a "work kanban task t_00000008" sleep 40' &
WORKER_PID=$!
sleep 0.6
check "the stand-in worker process is matchable by the script's own guard" "yes" \
  "$(pgrep -f "work kanban task t_00000008" >/dev/null 2>&1 && echo yes || echo no)"
: >"$LOG"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "a worktree with a live worker process SURVIVES" "yes" "$(survives t_00000008)"
contains "the live-process refusal is logged" "$(cat "$LOG")" "KEEP  t_00000008 (worker process alive)"
kill "$WORKER_PID" 2>/dev/null
wait "$WORKER_PID" 2>/dev/null
# ... and it is only the process holding it: with the process gone, the same
# worktree is removable, so the guard is not simply refusing everything.
run_prune --min-age-hours 24 >/dev/null 2>&1
check "with the worker gone, that same worktree is removed" "no" "$(survives t_00000008)"

# 7. The STRAY loop (outside .worktrees, no card reference) carries the same
#    correction. Strays are judged by the directory's mtime, so both are aged.
STRAY_MIRROR_SHA="$(fresh_commit 'stray mirror-only work')"
git -C "$REPO" update-ref refs/remotes/pr/998 "$STRAY_MIRROR_SHA"
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-mirror" "$STRAY_MIRROR_SHA"
STRAY_PUSHED_SHA="$(fresh_commit 'stray pushed work')"
git -C "$REPO" push -q origin "$STRAY_PUSHED_SHA:refs/heads/wt/jf-stray-pushed"
git -C "$REPO" fetch -q origin
git -C "$REPO" worktree add -q --detach "$SB/strayroot/jf-pushed" "$STRAY_PUSHED_SHA"
touch -d '2020-01-01' "$SB/strayroot/jf-mirror" "$SB/strayroot/jf-pushed"
run_prune --min-age-hours 24 >/dev/null 2>&1
check "a STRAY worktree held only by a mirror SURVIVES" "yes" "$(stray_survives jf-mirror)"
contains "the stray mirror refusal is logged, with the ref named" \
  "$(cat "$LOG")" "UNSAFE jf-mirror"
contains "...naming refs/remotes/pr/998" "$(cat "$LOG")" "refs/remotes/pr/998"
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
check "a dry run still reports work that exists only locally" "3" "$DRY_RC"

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
