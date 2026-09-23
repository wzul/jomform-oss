#!/usr/bin/env bash
# Prune Kanban worktrees whose card is finished, to stop .worktrees growing ~600M/card.
#
# WHY THIS IS IN THE REPO (it is a host script that runs outside the checkout)
#   It is one of the `monitor`/`tmpkey`/`backup` family: a script that only ever
#   runs on the agent host, whose failure modes are destructive and whose only
#   testable surface is its own harness. Keeping it outside the repo meant its
#   DURABILITY test could not be tested at all — a self-test living next to it on
#   the host is run by nobody (there is no CI here), so the defect below survived
#   two clean-looking cron runs. deploy/worktrees/prune-kanban-worktrees_test.sh
#   is the `prunewt` preflight gate; install this file to the host, do not edit a
#   host copy in place (the cron job `prune-kanban-worktrees` runs
#   $HERMES_HOME/scripts/prune-kanban-worktrees.sh).
#
#   Each JomForm worktree carries its own web/node_modules (442M) + web/.next (146M),
#   so ~15 cards/day = ~9G/day if nothing reclaims them. `hermes kanban gc` only
#   prunes events and logs, not worktrees — hence this script.
#
# SAFETY (in order, all must pass before anything is removed):
#   1. Not the board's main checkout.
#   2. The card's status is terminal (done/archived).
#   3. The card finished at least --min-age-hours ago (default 24h).
#   4. No live process references the worktree or the task id.
#   5. The work is DURABLE: the current commit is reachable from a
#      REMOTE-TRACKING ref — `refs/remotes/origin/*` (a pushed branch, or merged
#      into origin/main). Unpushed work is never removed — it is reported instead.
#      A LOCAL branch does not count (removing the worktree keeps the branch, so
#      it is reported as UNSAFE, not removed).
#
# WHAT "REMOTE-TRACKING" MEANS HERE, AND WHY THE TEST IS WRITTEN THE LONG WAY
#   `git branch -r --contains <sha>` lists every ref under refs/remotes/ — INCLUDING
#   hand-made mirrors like refs/remotes/pr/<N> (`git fetch origin pull/N/head:refs/remotes/pr/N`,
#   the documented GitHub idiom). Those are in NO refspec, so `git fetch --prune`
#   never removes them (git-fetch, PRUNING: pruning is "a function of the refspec
#   of the remote") and they keep pointing at whatever a PR's head was when they
#   were made — the pre-rebase/pre-force-push snapshot. Measured 2026-09-21: work
#   held ONLY by refs/remotes/pr/660 read as durable="pushed-branch", so a detached
#   worktree holding the last copy of that commit was removed. The durability test
#   below therefore names the refs that are actually remote-tracking
#   (`refs/remotes/origin`, which IS in the refspec +refs/heads/*:refs/remotes/origin/*
#   and therefore IS pruned) and explicitly REPORTS any other refs/remotes ref that
#   holds the commit, instead of letting it decide the verdict.
#
# Usage:
#   prune-kanban-worktrees.sh [--dry-run] [--min-age-hours N] [--board SLUG] [--repo PATH] [--no-fetch]
#
# Environment overrides (the self-test drives the whole script through these):
#   JF_PRUNE_REPO         repo to operate on            (default /root/wzul-git/jomform)
#   JF_PRUNE_BOARD        kanban board slug             (default jomform)
#   JF_PRUNE_LOG          log file to append to
#   JF_PRUNE_EXTRA_ROOTS  extra worktree roots to sweep (default "/tmp /root/wt")
#   JF_PRUNE_FETCH        1 = `git fetch --prune origin` first (default), 0 = skip
#   JF_PRUNE_LIB_ONLY     1 = define the functions and run nothing (self-test)
set -uo pipefail

DRY_RUN=0
MIN_AGE_HOURS=24
BOARD="${JF_PRUNE_BOARD:-jomform}"
REPO="${JF_PRUNE_REPO:-/root/wzul-git/jomform}"
LOG="${JF_PRUNE_LOG:-/root/agents/prune-kanban-worktrees.log}"
DO_FETCH="${JF_PRUNE_FETCH:-1}"
# Extra worktree roots the lanes create outside the repo's .worktrees dir
# (lane-typed paths like /tmp/jf551, /tmp/jf-gate-main, /root/wt/dev-t_xxxx).
# They are real git worktrees of the same repo, carry the same ~590M of
# node_modules, and no board card references them — so they are pruned by age
# and durability only.
EXTRA_ROOTS="${JF_PRUNE_EXTRA_ROOTS:-/tmp /root/wt}"

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run)        DRY_RUN=1; shift;;
    --min-age-hours)  MIN_AGE_HOURS="$2"; shift 2;;
    --board)          BOARD="$2"; shift 2;;
    --repo)           REPO="$2"; shift 2;;
    --no-fetch)       DO_FETCH=0; shift;;
    -h|--help)        sed -n '2,50p' "$0"; exit 0;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

log() { printf '%s %s\n' "$(date '+%F %T %Z')" "$*" | tee -a "$LOG"; }

# ---------------------------------------------------------------------------
# Durability: refs that a `git fetch --prune` can actually remove.
#
# `refs/remotes/origin` is a for-each-ref PATTERN: per git-for-each-ref(1) a
# pattern matches "literally … completely or from the beginning up to a slash",
# so it selects refs/remotes/origin/<branch> and cannot match refs/remotes/pr/<N>.
# origin/HEAD is excluded explicitly so a symref (which just points at origin/main
# and is not itself pruned-then-re-fetched) can never be the thing that carries a
# verdict.
# ---------------------------------------------------------------------------
ORIGIN_REFS_PATTERN="refs/remotes/origin"
ORIGIN_EXCLUDE="refs/remotes/origin/HEAD"

refs_containing() { # <repo> <head> <ref-pattern> [<exclude-pattern>...] — refnames, one per line
  local repo="$1" head="$2" pattern="$3"; shift 3
  local args=("--format=%(refname)" "--contains=$head" "$pattern")
  local e
  for e in "$@"; do args+=("--exclude=$e"); done
  git -C "$repo" for-each-ref "${args[@]}" 2>/dev/null
}

# Verdicts, most durable first:
#   merged-into-main  reachable from origin/main
#   pushed-branch     on a remote-tracking ref (refs/remotes/origin/*)
#   local-branch      held only by a LOCAL branch — the worktree can go but the
#                     work is not on any remote, so the caller reports it UNSAFE
#   none              held by nothing we can name
durable_verdict() { # <repo> <head> -> verdict on stdout
  local repo="$1" head="$2"
  if git -C "$repo" merge-base --is-ancestor "$head" origin/main 2>/dev/null; then
    printf 'merged-into-main\n'; return 0
  fi
  if [ -n "$(refs_containing "$repo" "$head" "$ORIGIN_REFS_PATTERN" "$ORIGIN_EXCLUDE")" ]; then
    printf 'pushed-branch\n'; return 0
  fi
  if [ -n "$(local_holder "$repo" "$head")" ]; then
    printf 'local-branch\n'; return 0
  fi
  printf 'none\n'
}

local_holder() { # <repo> <head> -> first local branch containing head
  local repo="$1" head="$2"
  git -C "$repo" for-each-ref --format='%(refname:short)' --contains "$head" refs/heads 2>/dev/null \
    | sed -n '1p'
}

# Refs under refs/remotes/ that are NOT remote-tracking refs of an origin branch —
# the hand-made mirrors. Reported, never allowed to decide a verdict.
mirror_holder() { # <repo> <head> -> comma-separated refnames (possibly empty)
  local repo="$1" head="$2"
  git -C "$repo" for-each-ref --format='%(refname)' --contains "$head" refs/remotes 2>/dev/null \
    | grep -v "^${ORIGIN_REFS_PATTERN}/" | paste -sd, -
}

mirror_note() { # <repo> <head> -> " [ignoring non-remote-tracking ref(s): a,b]" or ""
  local m; m="$(mirror_holder "$1" "$2")"
  [ -n "$m" ] && printf ' [ignoring non-remote-tracking ref(s): %s]' "$m"
  return 0
}

main() {
  [ -d "$REPO/.worktrees" ] || { log "no worktrees dir at $REPO/.worktrees"; exit 0; }
  cd "$REPO" || exit 1

  # Remote-tracking refs are stale until fetched, and the durability test below
  # depends on them. Without this, freshly pushed work reads as "unpushed" and is
  # held forever (or worse, a stale ref makes old work look durable). A mirror is
  # NOT refreshed by this: it is in no refspec, which is the whole point.
  if [ "$DO_FETCH" = 1 ]; then
    git fetch --prune origin >/dev/null 2>&1 || log "WARN: git fetch failed; durability judged on possibly-stale refs"
  else
    log "WARN: --no-fetch; durability judged on the refs as they are"
  fi

  NOW=$(date +%s)
  CUTOFF=$(( NOW - MIN_AGE_HOURS * 3600 ))

  removed=0 skipped=0 unsafe=0

  log "=== prune start (board=$BOARD repo=$REPO min-age=${MIN_AGE_HOURS}h dry-run=$DRY_RUN) ==="

  for wt in "$REPO"/.worktrees/*/; do
    [ -d "$wt" ] || continue
    name=$(basename "$wt")

    # Worktree names are either the task id (t_xxxxxxxx) or a lane-prefixed form
    # (qa-t_xxxxxxxx). Extract the t_ id from either.
    task=$(printf '%s' "$name" | grep -oE 't_[0-9a-f]{8}' | head -1)
    if [ -z "$task" ]; then
      log "SKIP  $name (no task id in name)"
      skipped=$((skipped+1)); continue
    fi

    # 4. live process guard — never yank a worktree out from under a worker.
    #    Match the WORKER INVOCATION only. A bare `pgrep -f "$name"` self-matches
    #    any shell whose command line merely mentions the path (including this
    #    script's own caller), which silently kept every old worktree forever.
    if pgrep -f "work kanban task $task" >/dev/null 2>&1; then
      log "KEEP  $name (worker process alive)"
      skipped=$((skipped+1)); continue
    fi
    # A process holding this specific worktree as its cwd (e.g. a build in it).
    if pgrep -f "worktrees/$name " >/dev/null 2>&1; then
      log "KEEP  $name (a process references this worktree)"
      skipped=$((skipped+1)); continue
    fi

    # 2+3. card status and completion time.
    raw=$(hermes kanban --board "$BOARD" show "$task" 2>&1)
    status=$(printf '%s' "$raw" | grep -oE '^  status: +[a-z_]+' | awk '{print $2}')
    case "$status" in
      done|archived) ;;
      *) log "KEEP  $name (card status=${status:-unknown})"; skipped=$((skipped+1)); continue;;
    esac

    completed=$(printf '%s' "$raw" | grep -oE '^  completed: +[0-9-]+ [0-9:]+' | sed 's/^  completed: *//')
    if [ -z "$completed" ]; then
      log "KEEP  $name (terminal but no completed timestamp)"
      skipped=$((skipped+1)); continue
    fi
    cepoch=$(date -d "$completed" +%s 2>/dev/null || echo 0)
    if [ "$cepoch" -eq 0 ] || [ "$cepoch" -gt "$CUTOFF" ]; then
      log "KEEP  $name (completed $completed < ${MIN_AGE_HOURS}h ago)"
      skipped=$((skipped+1)); continue
    fi

    # 5. DURABILITY — the whole point. Never delete work that exists nowhere else.
    head=$(git -C "$wt" rev-parse HEAD 2>/dev/null || echo "")
    if [ -z "$head" ]; then
      log "KEEP  $name (cannot resolve HEAD)"
      skipped=$((skipped+1)); continue
    fi

    durable=""
    note="$(mirror_note "$REPO" "$head")"
    case "$(durable_verdict "$REPO" "$head")" in
      merged-into-main) durable="merged-into-main" ;;
      pushed-branch)    durable="pushed-branch" ;;
      local-branch)
        # Not on any remote-tracking ref. Removing the WORKTREE loses nothing as
        # long as the local branch survives — but "nothing is on a remote" is the
        # condition this script reports rather than acts on, so it is UNSAFE.
        log "UNSAFE $name (card=$task head=${head:0:10} only on LOCAL branch '$(local_holder "$REPO" "$head")' — not pushed; leaving it)"
        unsafe=$((unsafe+1)); continue ;;
      *)
        log "UNSAFE $name (card=$task head=${head:0:10} on no ref at all — unpushed work, leaving it)$note"
        unsafe=$((unsafe+1)); continue ;;
    esac

    # Also confirm nothing is dirty in a way that would be lost.
    dirty=$(git -C "$wt" status --porcelain 2>/dev/null | wc -l)
    if [ "$dirty" != "0" ]; then
      log "UNSAFE $name (card=$task has $dirty uncommitted change(s) — leaving it)"
      unsafe=$((unsafe+1)); continue
    fi

    size=$(du -xsh "$wt" 2>/dev/null | awk '{print $1}')
    if [ "$DRY_RUN" = "1" ]; then
      log "WOULD $name (card=$task $size durable=$durable)$note"
    else
      if git worktree remove --force "$wt" 2>/dev/null; then
        log "RM    $name (card=$task $size durable=$durable)$note"
        removed=$((removed+1))
      else
        log "FAIL  $name (git worktree remove failed)"
        skipped=$((skipped+1))
      fi
    fi
  done

  if [ "$DRY_RUN" != "1" ]; then
    git worktree prune 2>/dev/null || true
  fi

  # ── Stray lane worktrees (outside .worktrees, no card reference) ─────────────
  # These are registered worktrees of this repo whose path the board does not know
  # about, so there is no card status to gate on. They are pruned on age
  # (mtime of the worktree dir) + durability alone, and only when clean.
  stray_removed=0 stray_unsafe=0
  for root in $EXTRA_ROOTS; do
    [ -d "$root" ] || continue
    for wt in $(git worktree list --porcelain 2>/dev/null \
                | awk -v r="$root" '$1=="worktree" && $2 ~ "^"r"/" {print $2}'); do
      [ -d "$wt" ] || continue
      name=$(basename "$wt")

      # Never touch the main checkout or a dir the board still points at.
      [ "$wt" = "$REPO" ] && continue
      if pgrep -f "worktrees/$name |$wt " >/dev/null 2>&1; then
        log "KEEP  $name (process references it)"
        continue
      fi

      # Age from the worktree dir's mtime — no card timestamp exists for these.
      mtime=$(stat -c %Y "$wt" 2>/dev/null || echo "$NOW")
      if [ "$mtime" -gt "$CUTOFF" ]; then
        log "KEEP  $name (modified < ${MIN_AGE_HOURS}h ago)"
        continue
      fi

      head=$(git -C "$wt" rev-parse HEAD 2>/dev/null || echo "")
      [ -z "$head" ] && { log "KEEP  $name (no HEAD)"; continue; }

      note="$(mirror_note "$REPO" "$head")"
      case "$(durable_verdict "$REPO" "$head")" in
        merged-into-main) dur="merged-into-main" ;;
        pushed-branch)    dur="pushed-branch" ;;
        *)
          log "UNSAFE $name (head=${head:0:10} on no remote-tracking ref — not pruning stray)$note"
          stray_unsafe=$((stray_unsafe+1)); continue ;;
      esac

      # Ignore build artefacts when judging dirtiness (node_modules, .next, tsbuildinfo).
      dirty=$(git -C "$wt" status --porcelain 2>/dev/null \
              | grep -vE '(node_modules|\.next/|tsbuildinfo)' | wc -l)
      if [ "$dirty" != "0" ]; then
        log "UNSAFE $name (source files modified: $dirty — leaving it)"
        stray_unsafe=$((stray_unsafe+1)); continue
      fi

      size=$(du -xsh "$wt" 2>/dev/null | awk '{print $1}')
      if [ "$DRY_RUN" = "1" ]; then
        log "WOULD $name (stray $size durable=$dur)$note"
      else
        if git worktree remove --force "$wt" 2>/dev/null; then
          log "RM    $name (stray $size durable=$dur)$note"
          stray_removed=$((stray_removed+1))
        else
          log "FAIL  $name (could not remove stray worktree)"
        fi
      fi
    done
  done
  if [ "$DRY_RUN" != "1" ] && [ "$stray_removed" -gt 0 ]; then
    git worktree prune 2>/dev/null || true
  fi

  # Reap build artefacts left behind in surviving stale worktrees (a .next dir can
  # be 146M and node_modules 442M). Only for worktrees older than the threshold,
  # and never while a worker holds the card.
  if [ "$DRY_RUN" != "1" ]; then
    for wt in "$REPO"/.worktrees/*/; do
      [ -d "$wt" ] || continue
      task=$(basename "$wt" | grep -oE 't_[0-9a-f]{8}' | head -1)
      [ -z "$task" ] && continue
      pgrep -f "work kanban task $task" >/dev/null 2>&1 && continue
      mtime=$(stat -c %Y "$wt" 2>/dev/null || echo "$NOW")
      [ "$mtime" -gt "$CUTOFF" ] && continue
      for sub in "$wt/web/.next" "$wt/web/test-results" "$wt/web/node_modules"; do
        if [ -d "$sub" ]; then
          sz=$(du -xsh "$sub" 2>/dev/null | awk '{print $1}')
          rm -rf "$sub" 2>/dev/null && log "RM    artefact $(basename "$(dirname "$sub")")/$(basename "$sub") in $(basename "$wt") ($sz)"
        fi
      done
    done
  fi

  log "=== prune done: removed=$removed skipped=$skipped unsafe=$unsafe stray_removed=$stray_removed stray_unsafe=$stray_unsafe ==="
  df -h / | sed -n '2p' | sed 's/^/  /' | tee -a "$LOG"

  # Exit non-zero when work exists only locally, so a cron wrapper can alert.
  [ "$unsafe" -gt 0 ] && exit 3
  [ "$stray_unsafe" -gt 0 ] && exit 3
  exit 0
}

[ "${JF_PRUNE_LIB_ONLY:-0}" = "1" ] || main "$@"
