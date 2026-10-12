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
#   4. No live process is using it: neither one whose ARGV names it (or the task
#      id), nor one whose WORKING DIRECTORY is inside it. See THE LIVE-PROCESS
#      GUARD READS ARGV AND THE KERNEL'S cwd below.
#   5. The work is DURABLE: the current commit is reachable from a
#      REMOTE-TRACKING ref — `refs/remotes/origin/*` (a pushed branch, or merged
#      into origin/main). Unpushed work is never removed — it is reported instead.
#      A LOCAL branch does not count (removing the worktree keeps the branch, so
#      it is reported as UNSAFE, not removed).
#
# THE LIVE-PROCESS GUARD READS ARGV AND THE KERNEL'S cwd (card t_ac768fb9)
#   SAFETY 4 says "no live process is using it", and a process can hold a worktree
#   two different ways. Both are read, and neither read is redundant:
#
#     * its ARGV carries the path — an editor opened on a file in there, a shell
#       that has not exec'd its last command yet, another agent's `git -C <wt> …`.
#       `has_process_matching` answers this one, over `ps -eo args=`.
#     * its WORKING DIRECTORY is the worktree, or anything inside it — the shape a
#       real build takes. `cd <wt> && npm run build` makes the shell EXEC the last
#       command of the `&&` list, so the path leaves argv entirely: the process is
#       `npm run build` with cwd = <wt>, and npm, node and everything they start
#       inherit it. Nothing in that argv names the worktree, so an argv-only guard
#       cannot see it. `has_process_cwd_matching` answers this one — and this is
#       the defect this comment exists for: a build that has not exec'd yet is
#       visible ONLY in argv, and an exec'd one ONLY in cwd.
#
#   For the CARD loop the argv half was incidentally covered by the worker guard
#   (`work kanban task <id>` is in a worker's argv). A STRAY has no worker, so
#   before this its fate was age + durability + `git status` alone: a stray was
#   removed with a build running inside it — `rm -rf` of 442M node_modules and
#   146M .next under a live `npm run build`. The same hole was open on the
#   artefact-reap path (`rm -rf web/node_modules`), which is why that call site
#   carries the cwd test too.
#
#   THE MECHANISM IS ONE `find` CALL FOR THE WHOLE TABLE — `find /proc -maxdepth 2
#   -name cwd -printf '%p\0%l\0'` — AND IT WAS DECIDED ON MEASURED NUMBERS, not on
#   taste. On the agent host (364 pids) `readlink /proc/<pid>/cwd` once per pid
#   costs ~1.3s and this costs ~0.05s, and the question is asked once per worktree:
#   the host carries 641 worktrees today, i.e. ~14 minutes against ~30s of a job
#   that takes ~1 minute end to end. `find -printf` also gives, per pid, the PATH
#   (so a failure can name the pid) and the RAW readlink target — unlike `ls -l` it
#   does not QUOTE the target, so a path containing a space compares exactly and a
#   path containing ` -> ` cannot be mis-split. Considered and rejected:
#     * `lsof` (and `fuser`): present on this host (measured) but not something a
#       bare host is guaranteed to have, and their directory forms (`lsof +D <dir>`)
#       walk the whole tree — seconds on a worktree with 442M of node_modules — to
#       answer a question the kernel answers directly. `lsof -a -d cwd -p <pid>`
#       also needs one call per pid, i.e. the same per-pid cost as readlink.
#     * an argv heuristic (`pgrep -f` on the worktree path): that is the input
#       source this defect is about. It cannot see a cwd at all.
#     * `cd -P /proc/<pid>/cwd` + `$PWD` is fork-free and exact, but it CHDIRS into
#       the process's directory, so a cwd on a hung mount can block the pruner —
#       reading a link cannot.
#     * `/proc/<pid>/cwd` is Linux-only, and that is accepted and stated rather
#       than hidden: this is a host script for the agent host (systemd Linux) and
#       it already relies on GNU/Linux-only tools (`ps -eo args=`, `stat -c`,
#       `date -d`, `du -xsh`, `find -printf`). On a host where the read yields no
#       rows at all the guard reports cannot-judge (below) instead of reading an
#       unreadable table as "nothing is inside" — so the portability gap fails
#       loudly and in the safe direction.
#
#   WHAT COUNTS AS A MATCH: the directory itself, or anything under it
#   (`<dir>` or `<dir>/*`), because `git worktree remove --force` deletes the
#   whole subtree. The argv test needs a trailing space to stop a SIBLING path
#   (`<…>/jf-live2`) counting as `<…>/jf-live`; readlink returns the whole path,
#   so the `/` boundary does that job here and no padding is needed. The one
#   suffix readlink can add, ` (deleted)`, is deliberately NOT normalised: a
#   process whose cwd was already unlinked holds a directory that is no longer
#   the one on disk, so it must not read as holding it. `$$` is skipped for the
#   same reason the argv scan skips it — the predicate must never be able to match
#   the process asking the question (the self-test drives it from a shell whose
#   cwd is the directory under test). No scanner-exclusion list is needed here:
#   an argv that merely MENTIONS `pgrep`/`awk` has nothing to do with a cwd read.
#
#   WHEN THE READ FAILS — a decision, not a fallback. `readlink` of a dead
#   process's cwd fails, and so does a read the kernel refuses for a process we
#   may not inspect. They are told apart by the process STATE (`State:` in
#   `/proc/<pid>/status`) and are NOT treated the same:
#     * state `Z` (zombie): the process is dead and holds nothing, so it is
#       SKIPPED silently — it is neither a reason to keep nor to refuse.
#       Measured on this host: 15 of 354 pids failed the read and every one was a
#       zombie, so a blanket "a failed read means keep" would have kept everything
#       and exited 1 on EVERY run.
#     * NO STATE LINE AT ALL: the pid was REAPED between `find`'s readdir and its
#       readlink, so there is no cwd link AND no `status` to read — nothing is
#       left of it that could hold the worktree, which makes it the same kind of
#       skip as a zombie (card t_d53b71b8, 2026-10-12). The window is not
#       theoretical: a churned /proc produced exactly this signature (measured, 1
#       hit in 188 scans of a table with 4000 short-lived processes behind it),
#       and read as a cannot-judge it FAILS EVERY worktree in the run — a red gate
#       on a tree that never touched the pruner.
#     * any other state: we cannot tell whether that live process is inside the
#       worktree, so the worktree is LEFT ON DISK AND THE RUN FAILS — a `FAIL` line
#       naming the pid, `failures+1`, exit 1. Deliberately neither a silent keep
#       nor a removal: a silent keep hides "the pruner stopped reclaiming this
#       worktree" (the shape the exit-1 contract exists for), and removing on a
#       read we could not make is the one irreversible mistake this script is
#       written to avoid. Same treatment as an unreadable card and an unresolvable
#       HEAD.
#     * the pid VANISHED between the readdir and the read (card t_e7ac4bcd — the
#       "reaped pid" of card t_d53b71b8, the same measurement): both
#       reads then fail — the cwd link is read after the process died, and so is
#       its `status` — which is the SAME shape a refused read gives. It is NOT a
#       cannot-judge: there is no process left to hold the worktree, so it is
#       SKIPPED like a zombie. The two are told apart by the pid's own directory,
#       re-read immediately before the verdict (listed by the scan a moment ago,
#       gone now): a refused read leaves the directory there. Without this, a busy
#       host's nightly run printed a FAIL and exited 1 having reclaimed nothing —
#       measured 2 of 80 predicate calls rc=2 with pids already absent from /proc,
#       at ~12 load, and the self-test read 184/3 where its own bytes read 187/0
#       minutes later.
#     * NO ROWS AT ALL (the tool is not GNU `find`, the root is unreadable): the
#       same cannot-judge, `?` instead of a pid. "The table came back empty" must
#       never be read as "nothing is inside" — that is precisely the silent removal
#       this guard exists to prevent.
#
#   `JF_PROC_ROOT` (default `/proc`) exists only as the self-test's seam: the suite
#   synthesises a table containing a live pid whose cwd cannot be read, which cannot
#   be produced ON DEMAND (root can read every live process's cwd here; the reads
#   that fail in practice are zombies and pids reaped mid-scan). That synthetic case
#   pins the SHAPE — a failed read on a pid whose state is not `Z` — not the EACCES
#   errno itself. The REAPED/VANISHED pid has its own case, and it needs a second
#   seam to be honest about it: a stub `find` on `PATH`, because a real readdir
#   cannot be made to list a pid whose directory is already gone. That fixture
#   prints the one row a real table hands the loop for a process that died between
#   the two reads — the pid's `cwd` with an EMPTY target — while the pid directory
#   does NOT exist, which is what tells that case apart from a refused read (where
#   the directory is still there).
#
#   WHERE THE cwd READ IS MADE, AND WHY THERE. It is made LAST in every loop —
#   immediately before the `git worktree remove` or the `rm -rf` it guards — while
#   the two argv reads stay in step 4. That is the cost, not a preference: the argv
#   reads are one ~30ms `ps` each, the cwd read is a ~0.1s scan of the whole process
#   table, and the host carries 641 worktrees (its last logged run: 33 card
#   removals, 38 artefact reaps). Made last, the scan is paid only where it can
#   still change the outcome, and it is the freshest available answer for the
#   decision it guards. It is still a guard — it runs BEFORE the destructive step,
#   never after — and the artefact path additionally skips the scan entirely for a
#   worktree with no artefacts to reap.
#
# EXIT CODES — WHAT A CALLER CAN ALERT ON
#   0  the run did its job: every removal it ATTEMPTED succeeded, and the work it
#      kept was kept on purpose. Keeping work is normal here (an unpushed local
#      branch, a hand-made mirror, a live worker, a too-recent card, a dirty tree)
#      — the log names each one and the summary line counts them, but a refusal is
#      not a failure of the run.
#   1  something went wrong: a `git worktree remove` FAILED, a card could not be
#      read (so its worktree can never be judged), a live process's cwd could not
#      be read (so no worktree could be proven free), or `git fetch` failed and the
#      durability verdict was taken on stale refs. These are the conditions that
#      mean the pruner could not do its job, and they are the ones to alarm on.
#   2  bad arguments.
#   3  no progress: the run reclaimed nothing while work whose ONLY copy is a
#      directory it refused to remove was kept. This is the "the pruner has
#      started keeping everything" alarm — deliberately narrow, so a board that
#      keeps a few at-risk directories while reclaiming the reclaimable rest stays
#      green. `--dry-run` reclaims nothing by design and is never judged on this.
#
# BEFORE THIS CONTRACT: any `unsafe` count exited 3. On a busy board that is every
# run (measured 2026-09-25: 11 of 20 logged runs, the last 7 of 7, 528 of 567
# refusals being a local branch whose commit still exists), so the code meant
# nothing and the status a human reads could never clear. Exit 3 now says a
# specific thing, and the deliberate refusals moved to 0 where they belong.
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
#   JF_PROC_ROOT          /proc root the cwd read uses  (default /proc; self-test seam)
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
    -h|--help)        sed -n '2,/^set -uo pipefail/p' "$0" | sed '$d'; exit 0;;
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

# worktree_head: the worktree's OWN commit, or nothing when that cannot be resolved.
#
# WHY `--verify` AND NOT A PLAIN `rev-parse HEAD` (card t_3fa15d1c). The two shapes
# of "this worktree's HEAD does not resolve" do not behave the same, and the
# difference is STDOUT:
#   * a valid gitdir whose HEAD points at a branch that no longer exists — what a
#     lane worktree becomes when its branch is deleted, or a `fetch --prune` drops
#     the ref — makes `git rev-parse HEAD` print the literal string `HEAD` to stdout
#     before failing (rc 128). `[ -z "$head" ]` is then FALSE, so the guard below is
#     never reached and that bogus rev is fed to `mirror_note`/`durable_verdict`,
#     where `HEAD` resolves IN THE REPO'S CONTEXT: a rev with nothing to do with the
#     worktree decides its fate. Measured 2026-10-11 (t_3fa15d1c): same worktree,
#     repo HEAD on an unpushed local branch -> `head=HEAD only on LOCAL branch
#     'local-unpushed'`; repo HEAD on main -> `has 1 uncommitted change(s)` (an
#     unborn HEAD makes `git status --porcelain` report every index entry as `A `,
#     since there is no HEAD to compare the index against). Both exit 0 and keep the
#     worktree forever, naming a reason that is about the repo or about a phantom
#     edit — the "the pruner silently stopped reclaiming this worktree" shape the
#     exit-1 contract exists to surface.
#   * a broken `.git` link yields no stdout either way, which is why that shape
#     reached the guard while this one did not.
# `git rev-parse --verify HEAD` prints nothing on failure and the same sha on
# success (measured on this host), so the guard fires for both shapes.
worktree_head() { # <worktree-path> -> sha on stdout, empty when unresolvable
  git -C "$1" rev-parse --verify HEAD 2>/dev/null || echo ""
}

# has_process_matching: is any process's cmdline carrying PATTERN?
#
# WHY NOT A BARE `pgrep -f PATTERN` (card t_f454cd2b). `pgrep -f` matches its own
# pattern string, so it matches the argv of ANY other `pgrep` that is looking for
# the same thing at the same moment. A concurrent preflight run's prunewt gate
# execs `pgrep -f work kanban task t_00000003`, and THIS gate's
# `pgrep -f "work kanban task t_00000003"` therefore sees it as a live worker —
# measured: `3785067 pgrep -f work kanban task t_00000003`, and the run then logged
# `KEEP t_00000003 (worker process alive)` for a card whose worker is a fictional
# stand-in in this suite. Reproduced at 1-in-6 with six concurrent
# `--only prunewt` runs, and exactly one gate red every time.
#
# The failure is not cosmetic: that phantom keep removes three real verdicts
# (`RM t_00000001`, `RM t_00000003`, and their `durable=…` records), so the gate
# reds on assertions about removals it never took.
#
# A SCANNER IS NEVER THE WORKER, so every process that merely MENTIONS the pattern
# as an argument to a scanning command is skipped: `pgrep`, `pkill`, `ps`, `grep`,
# `awk`, `sed`. Both observed matches were of this kind — a peer run's `pgrep -f`
# and a measurement probe's `awk -v pat=…`. A real worker's argv is
# `hermes … work kanban task <id>` or `exec -a "work kanban task <id>" …`, i.e.
# none of these. `$$` is skipped so this script's own call site (which carries the
# pattern) cannot match itself.
#
# WHY ONE `awk` AND NOT `… | grep -vE … | grep -Fq` (card t_48f08867). That pipeline
# reported a MATCH as a MISS, and this is the gate's data-loss direction: a false
# miss means a worktree held by a LIVE worker is pruned. The cause is a pipe, not a
# pattern: `ps` lists by pid ASCENDING, so a stand-in started early sits early in the
# stream; `grep -Fq` exits at its FIRST match while the upstream `grep -vE` is still
# writing its next stdio block, the writer takes SIGPIPE (141), and under
# `set -uo pipefail` the failed writer fails the WHOLE pipeline (`$?` = 141) for a
# search that plainly matched. Measured on this host, the pruner's own predicate
# against a live stand-in that a plain `grep` can see:
#
#   377 procs   0 /  400 misses     (the isolated case: green, 73/0)
#   978 procs  197 /  400 misses    (49%)
#  2395 procs  343 /  400 misses    (86%)
#
# which is exactly the observed shape: green in isolation, red under a loaded full
# preflight, and one real gate run captured red at 1-in-6 with host churn. `awk`
# reads its whole input before `END` decides, so there is no early reader and no
# SIGPIPE, and the scanner exclusion is an awk pattern instead of a second `grep`.
# `-v` (not string concatenation) keeps a pattern with regex metacharacters literal.
#
# THE ARGUMENT IS ONE LITERAL SUBSTRING, NEVER AN ALTERNATION (card t_8cdb8111).
# It is tested with `index()`, so `a |b` is a single literal that has to appear
# verbatim in an argv — no argv contains ` |`, so such a call site can never fire
# and is dead code that reads like a live guard. A call site that wants two
# alternatives makes TWO CALLS. `$wt ` (the path plus a trailing space) is the
# deliberate shape for a worktree path: it stops an argv holding a SIBLING path
# (`/tmp/x/jf-live2`) from counting as `/tmp/x/jf-live`.
has_process_matching() { # <literal substring>
  ps -eo args= 2>/dev/null | awk -v pat="$1" '
    $0 !~ /^[[:space:]]*([^[:space:]]*\/)?(pgrep|pkill|ps|grep|awk|sed)([[:space:]]|$)/ \
      && index($0, pat) > 0 { found = 1 }
    END { exit(found ? 0 : 1) }
  '
}

# has_process_cwd_matching: is any live process WORKING inside <dir> — i.e. is <dir>,
# or anything under it, some process's working directory? (the OTHER half of the
# live-process guard; card t_ac768fb9)
#
# WHY A cwd READ AT ALL, WHY BOTH ARGV AND cwd stay, and what a DENIED read means:
# all three are decided and argued in the script header, under "THE LIVE-PROCESS
# GUARD READS ARGV AND THE KERNEL'S cwd". Summary:
#
#   0  a live process's cwd is <dir> or under it — the caller must not remove it.
#      The shape this exists for is a real build: `cd <wt> && npm run build` execs
#      the build, so the path is in the cwd and in NO argv.
#   1  nothing is inside it.
#   2  the question could not be answered: a LIVE process's cwd could not be read,
#      or the process table produced nothing at all. The pid(s) are printed on
#      stdout (`?` for the empty-table case). The caller must neither keep silently
#      nor remove — see the header (a zombie is skipped instead: it holds nothing).
#
# THE MECHANISM IS ONE `find` CALL FOR THE WHOLE TABLE, AND THE NUMBERS DECIDED IT
# (measured on the agent host, 364 pids): `readlink /proc/<pid>/cwd` in a per-pid
# loop costs ~1.3s, this costs ~0.05s, and the question is asked once per worktree
# — 641 of them on the host today, i.e. ~14 minutes against ~30s. `lsof`/`fuser`
# were rejected in the header (not guaranteed present, and their directory forms
# walk the whole tree). `find -printf '%p\0%l\0'` gives, per pid, the PATH (so the
# pid is still known for the failure message) and the RAW readlink target: unlike
# `ls -l` it does not QUOTE the target, so a path containing a space is compared
# exactly and a path containing ` -> ` cannot be mis-split. NUL-separated rather
# than line-based for the same reason: a target containing a tab or a newline
# cannot turn one pid into two records. An empty target means the read failed.
#
# `JF_PROC_ROOT` (default /proc) is the self-test's seam.
has_process_cwd_matching() { # <dir>
  local dir="${1%/}" root="${JF_PROC_ROOT:-/proc}" path target pdir pid state sl
  local rows=0 denied=""
  while IFS= read -r -d '' path && IFS= read -r -d '' target; do
    rows=$((rows+1))
    pdir="${path%/*}"                          # <root>/<pid>
    pid="${path#"$root"/}"; pid="${pid%%/*}"   # -> <pid>
    # Never the process asking the question (see the header).
    [ "$pid" = "$$" ] && continue
    if [ -z "$target" ]; then
      # No cwd to read. A ZOMBIE has none to hold — dead, so it can be neither a
      # reason to keep nor to refuse — and it is skipped. A pid that has ALREADY
      # VANISHED reads exactly the same way and is skipped too (below) — and
      # "vanished" means the pid's own DIRECTORY is gone, not merely that its
      # `status` could not be read. Any other state is a read we could not make:
      # that is a cannot-judge, never a keep and never a removal. The state letter
      # is the second field of the `State:` line in /proc/<pid>/status, and it is
      # read with the shell's own `read` rather than an `awk`: this path runs once
      # per unreadable pid, and the fork per pid is what made a call cost 273ms
      # instead of 100ms (measured on the host).
      state=""
      while IFS= read -r sl; do
        case "$sl" in State:*) state="${sl#State:}"; break ;; esac
      done <"$pdir/status" 2>/dev/null
      # "State:\tZ (zombie)" -> leading whitespace off -> first character is it.
      state="${state#"${state%%[![:space:]]*}"}"
      [ "${state:0:1}" = "Z" ] && continue
      # A VANISHED PID IS NOT A DENIED READ (card t_e7ac4bcd — the "reaped pid" of
      # card t_d53b71b8, same measurement, and its fix is subsumed here). A process
      # that dies between the `find` above and this loop leaves BOTH reads failing:
      # its cwd link is gone by the time `find` reads it (empty target — the row we
      # are in now) and /proc/<pid>/status is gone with it (no `State:` line, so
      # `state` is exactly as empty as it is for a REFUSED read). Counting that
      # as "a LIVE pid whose cwd could not be read" is a cannot-judge about a
      # process that no longer exists: it keeps the worktree AND fails the run
      # (`FAIL … cannot read the cwd of live pid(s) <pid>`, exit 1 — the "the
      # pruner could not do its job" alarm), so on a busy host the nightly run
      # alerted and reclaimed nothing. Measured on the agent host: 80 predicate
      # calls under a 600-process churn gave 2 rc=2 samples whose pids were both
      # absent from /proc immediately afterwards, and it cost the v1.21.74
      # installer gate its first run (184 passed / 3 failed; the same bytes read
      # 187/0 minutes later).
      #
      # THE PID'S OWN DIRECTORY IS THE DISCRIMINATOR, AND IT IS READ HERE — after
      # the status read, immediately before the verdict — so the gap between the
      # last evidence and the decision is one `stat` instead of a whole file read
      # (the order matters: checking first would leave the slow read inside the
      # window this exists to close). `find` listed an entry under this directory
      # a moment ago, so this user could traverse it then; a directory that is
      # gone now means GONE, not refused. A directory that is still there — the
      # status read failed for any other reason, e.g. a read the kernel refused —
      # keeps the cannot-judge below exactly as it was: an EXISTING live pid whose
      # cwd cannot be read must still leave the worktree on disk and fail the run.
      [ -d "$pdir" ] || continue
      denied="${denied:+$denied,}$pid"
      continue
    fi
    case "$target" in
      # The directory itself, or anything under it: `git worktree remove --force`
      # deletes the whole subtree, and the `/` boundary here is what keeps a
      # SIBLING path (`<…>/jf-live2`) from counting as `<…>/jf-live`.
      "$dir"|"$dir"/*) return 0 ;;
    esac
  done < <(find "$root" -maxdepth 2 -name cwd -printf '%p\0%l\0' 2>/dev/null)
  # An empty table is a table that could not be read (a `find` that is not GNU
  # find, an unreadable root, a bogus JF_PROC_ROOT) — a cannot-judge, NOT "nothing
  # is inside", because the difference between those two is the difference between
  # keeping a worktree and deleting one out from under a live process.
  [ "$rows" -eq 0 ] && { printf '?\n'; return 2; }
  [ -n "$denied" ] && { printf '%s\n' "$denied"; return 2; }
  return 1
}

main() {
  [ -d "$REPO/.worktrees" ] || { log "no worktrees dir at $REPO/.worktrees"; exit 0; }
  cd "$REPO" || exit 1

  removed=0 skipped=0 unsafe=0
  # Failure counters, kept apart from the "kept on purpose" ones on purpose:
  # `failures`/`fetch_failed` are what exit 1 is for (see EXIT CODES at the top of
  # this file). Initialised BEFORE the fetch below, which is what can set the second.
  failures=0 fetch_failed=0
  # Directories refused because NOTHING under refs/ — no remote-tracking ref, no
  # hand-made mirror — carries their commit, so the directory itself may hold the
  # only copy. Counted for the exit-3 "reclaimed nothing while keeping at-risk
  # work" check only. A mirror-held commit is NOT counted: the mirror ref survives
  # the removal, which is why it is merely not trusted as *durability*.
  at_risk_total=0

  # Remote-tracking refs are stale until fetched, and the durability test below
  # depends on them. Without this, freshly pushed work reads as "unpushed" and is
  # held forever (or worse, a stale ref makes old work look durable). A mirror is
  # NOT refreshed by this: it is in no refspec, which is the whole point.
  if [ "$DO_FETCH" = 1 ]; then
    if ! git fetch --prune origin >/dev/null 2>&1; then
      # A failed fetch is a FAILURE, not a warning: the durability verdict below is
      # taken on possibly-stale remote-tracking refs, which is exactly the state that
      # makes pushed work read as unpublished (kept forever) or a stale ref read as
      # durable (removed). Exit 1 so a caller can tell this apart from "kept on
      # purpose" — see EXIT CODES above.
      log "FAIL  git fetch failed; durability judged on possibly-stale refs — this run cannot be trusted"
      fetch_failed=1
    fi
  else
    log "WARN: --no-fetch; durability judged on the refs as they are"
  fi

  NOW=$(date +%s)
  CUTOFF=$(( NOW - MIN_AGE_HOURS * 3600 ))

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
    #    TWO reads here, both cheap (`ps` is one ~30ms call), and the THIRD one —
    #    a process WORKING inside the worktree, the exec'd build whose argv is
    #    nothing but `npm run build` — is made last, just before the removal: it is
    #    the only one that scans the whole process table (~0.1s) and the host
    #    carries 641 worktrees, so it is made where a scan can still change the
    #    outcome and where it is the freshest answer for the decision. See the
    #    header, "THE LIVE-PROCESS GUARD READS ARGV AND THE KERNEL'S cwd".
    #      a) it IS the worker — `work kanban task <id>` is in its argv. A bare
    #         `pgrep -f "$name"` here self-matches any shell whose command line
    #         merely mentions the path (including this script's own caller), which
    #         silently kept every old worktree forever.
    #      b) its ARGV names the worktree — an editor opened on a file in there, a
    #         shell that has not exec'd its last command yet, another agent's
    #         `git -C <wt> …` (`has_process_matching`, a LITERAL substring test).
    if has_process_matching "work kanban task $task"; then
      log "KEEP  $name (worker process alive)"
      skipped=$((skipped+1)); continue
    fi
    if has_process_matching "worktrees/$name "; then
      log "KEEP  $name (a process references this worktree)"
      skipped=$((skipped+1)); continue
    fi

    # 2+3. card status and completion time.
    raw=$(hermes kanban --board "$BOARD" show "$task" 2>&1)
    status=$(printf '%s' "$raw" | grep -oE '^  status: +[a-z_]+' | awk '{print $2}')
    case "$status" in
      done|archived) ;;
      "")
        # An unreadable card is a FAILURE, not a deliberate keep: with no status we
        # can never judge this worktree, so it is retained forever and the pruner
        # silently stops reclaiming it. That is the shape the `--dry-run` runs on the
        # real host kept hitting (3 of them logged `card status=unknown`). Exit 1 so
        # a caller can tell it apart from "kept on purpose".
        log "FAIL  $name (card status unreadable — cannot judge this worktree; leaving it)"
        failures=$((failures+1)); continue;;
      *)
        log "KEEP  $name (card status=${status})"; skipped=$((skipped+1)); continue;;
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
    #    The read is `worktree_head`, i.e. `rev-parse --verify HEAD`, NOT a plain
    #    `rev-parse HEAD`: an unborn HEAD makes the plain form print the literal
    #    `HEAD`, which reaches the verdict and is resolved in the REPO's context
    #    (card t_3fa15d1c — the guard below never fired).
    head=$(worktree_head "$wt")
    if [ -z "$head" ]; then
      log "FAIL  $name (cannot resolve HEAD — cannot judge this worktree; leaving it)"
      failures=$((failures+1)); continue
    fi

    durable=""
    note="$(mirror_note "$REPO" "$head")"
    case "$(durable_verdict "$REPO" "$head")" in
      merged-into-main) durable="merged-into-main" ;;
      pushed-branch)    durable="pushed-branch" ;;
      local-branch)
        # Not on any remote-tracking ref. Removing the WORKTREE loses nothing as
        # long as the local branch survives — and it does: the branch is not part of
        # the worktree `git worktree remove` deletes. So this is a keep on purpose,
        # reported with its branch named, NOT a failure of the run (exit 0).
        log "UNSAFE $name (card=$task head=${head:0:10} only on LOCAL branch '$(local_holder "$REPO" "$head")' — not pushed; leaving it)"
        unsafe=$((unsafe+1)); continue ;;
      *)
        # Nothing under refs/ names this commit, so this directory may hold the only
        # copy. Refused, at-risk, and counted for the exit-3 "reclaimed nothing
        # while keeping at-risk work" check — but only when no mirror holds it
        # either, since a mirror ref survives the removal.
        log "UNSAFE $name (card=$task head=${head:0:10} on no ref at all — unpushed work, leaving it)$note"
        unsafe=$((unsafe+1))
        [ -z "$note" ] && at_risk_total=$((at_risk_total+1))
        continue ;;
    esac

    # Also confirm nothing is dirty in a way that would be lost.
    dirty=$(git -C "$wt" status --porcelain 2>/dev/null | wc -l)
    if [ "$dirty" != "0" ]; then
      log "UNSAFE $name (card=$task has $dirty uncommitted change(s) — leaving it)"
      unsafe=$((unsafe+1)); continue
    fi

    # THE LAST CHECK, IMMEDIATELY BEFORE THE DESTRUCTIVE STEP: is a live process
    # WORKING inside it? This is the third read of SAFETY 4, the one the argv tests
    # cannot make, and the one made here rather than in step 4 on purpose — it scans
    # the whole process table (~0.1s measured) and the host carries 641 worktrees,
    # while the argv tests are one ~30ms `ps` call each. Placed here it runs only
    # where a scan can still change the outcome, and it is the freshest possible
    # answer at the moment the removal is decided (card t_ac768fb9; the header
    # argues the mechanism and what a denied read means).
    cwd_pid="$(has_process_cwd_matching "$wt")"; cwd_rc=$?
    case "$cwd_rc" in
      0) log "KEEP  $name (a process's working directory is inside this worktree)"
         skipped=$((skipped+1)); continue ;;
      2) log "FAIL  $name (cannot read the cwd of live pid(s) ${cwd_pid:-?} — cannot prove nothing is working inside this worktree; leaving it)"
         failures=$((failures+1)); continue ;;
    esac

    size=$(du -xsh "$wt" 2>/dev/null | awk '{print $1}')
    if [ "$DRY_RUN" = "1" ]; then
      log "WOULD $name (card=$task $size durable=$durable)$note"
    else
      if git worktree remove --force "$wt" 2>/dev/null; then
        log "RM    $name (card=$task $size durable=$durable)$note"
        removed=$((removed+1))
      else
        log "FAIL  $name (git worktree remove failed)"
        failures=$((failures+1))
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
      # A process referring to this stray: the `.worktrees/<name>` form a lane
      # writes, or this directory's own path. TWO calls, not one `a |b` string:
      # has_process_matching is a LITERAL substring test, so the old
      # `"worktrees/$name |$wt "` was a single literal that no argv contains — the
      # guard was dead code and a stray with a live build inside it was REMOVED
      # (card t_8cdb8111). The trailing space in each literal keeps a sibling path
      # (`<…>/jf-live2`) from counting as this one.
      if has_process_matching "worktrees/$name " || has_process_matching "$wt "; then
        log "KEEP  $name (process references it)"
        continue
      fi

      # Age from the worktree dir's mtime — no card timestamp exists for these.
      mtime=$(stat -c %Y "$wt" 2>/dev/null || echo "$NOW")
      if [ "$mtime" -gt "$CUTOFF" ]; then
        log "KEEP  $name (modified < ${MIN_AGE_HOURS}h ago)"
        continue
      fi

      # Same read as the card loop, and for the same reason: `--verify` is what
      # makes an UNBORN HEAD come back empty instead of as the literal `HEAD`
      # (card t_3fa15d1c).
      head=$(worktree_head "$wt")
      [ -z "$head" ] && { log "FAIL  $name (no HEAD — cannot judge this stray; leaving it)"; failures=$((failures+1)); continue; }

      note="$(mirror_note "$REPO" "$head")"
      case "$(durable_verdict "$REPO" "$head")" in
        merged-into-main) dur="merged-into-main" ;;
        pushed-branch)    dur="pushed-branch" ;;
        local-branch)
          # Same reasoning as the card loop: a local branch is not durable, but it is
          # not a last copy either — the branch outlives the worktree — so a stray
          # held by one is a refusal, NOT an at-risk keep.
          log "UNSAFE $name (head=${head:0:10} only on LOCAL branch '$(local_holder "$REPO" "$head")' — not pruning stray)"
          stray_unsafe=$((stray_unsafe+1)); continue ;;
        *)
          # Nothing under refs/ names this commit at all, so this directory may hold
          # the only copy. This is the at-risk shape (and only when no hand-made
          # mirror holds it, since a mirror ref survives the removal).
          log "UNSAFE $name (head=${head:0:10} on no remote-tracking ref — not pruning stray)$note"
          stray_unsafe=$((stray_unsafe+1))
          [ -z "$note" ] && at_risk_total=$((at_risk_total+1))
          continue ;;
      esac

      # Ignore build artefacts when judging dirtiness (node_modules, .next, tsbuildinfo).
      dirty=$(git -C "$wt" status --porcelain 2>/dev/null \
              | grep -vE '(node_modules|\.next/|tsbuildinfo)' | wc -l)
      if [ "$dirty" != "0" ]; then
        log "UNSAFE $name (source files modified: $dirty — leaving it)"
        stray_unsafe=$((stray_unsafe+1)); continue
      fi

      # THE LAST CHECK BEFORE THE DESTRUCTIVE STEP — the half NO argv literal can see
      # (card t_ac768fb9). A real build IS `cd <wt> && npm run build`: the shell
      # execs it, so the path is in the kernel's cwd and in NO argv, which is
      # exactly the shape this loop used to remove while it ran. Same three outcomes
      # as the card loop, and the same reason for the position; see the header.
      cwd_pid="$(has_process_cwd_matching "$wt")"; cwd_rc=$?
      case "$cwd_rc" in
        0) log "KEEP  $name (a process's working directory is inside this stray)"
           continue ;;
        2) log "FAIL  $name (cannot read the cwd of live pid(s) ${cwd_pid:-?} — cannot prove nothing is working inside this stray; leaving it)"
           failures=$((failures+1)); continue ;;
      esac

      size=$(du -xsh "$wt" 2>/dev/null | awk '{print $1}')
      if [ "$DRY_RUN" = "1" ]; then
        log "WOULD $name (stray $size durable=$dur)$note"
      else
        if git worktree remove --force "$wt" 2>/dev/null; then
          log "RM    $name (stray $size durable=$dur)$note"
          stray_removed=$((stray_removed+1))
        else
          log "FAIL  $name (could not remove stray worktree)"
          failures=$((failures+1))
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
      has_process_matching "work kanban task $task" && continue
      mtime=$(stat -c %Y "$wt" 2>/dev/null || echo "$NOW")
      [ "$mtime" -gt "$CUTOFF" ] && continue
      # Nothing to reap here? Then do not pay for the process-table scan at all —
      # it is the expensive read of SAFETY 4 (see the header), and on the host 35 of
      # 641 worktrees carry artefacts.
      if [ -d "$wt/web/.next" ] || [ -d "$wt/web/test-results" ] || [ -d "$wt/web/node_modules" ]; then
        # The same guard, on the same class of process, for the same reason: the
        # worker check above is argv-only, so `rm -rf web/node_modules` under a live
        # `npm run build` (argv = `npm run build`, cwd = this worktree) would break
        # the build in progress (card t_ac768fb9). A denied cwd read leaves the
        # artefacts alone and FAILs — the third outcome of has_process_cwd_matching,
        # argued in the header.
        cwd_pid="$(has_process_cwd_matching "$wt")"; cwd_rc=$?
        case "$cwd_rc" in
          0) continue ;;
          2) log "FAIL  artefact in $(basename "$wt") (cannot read the cwd of live pid(s) ${cwd_pid:-?} — cannot prove nothing is building there; leaving its build artefacts)"
             failures=$((failures+1)); continue ;;
        esac
      fi
      for sub in "$wt/web/.next" "$wt/web/test-results" "$wt/web/node_modules"; do
        if [ -d "$sub" ]; then
          sz=$(du -xsh "$sub" 2>/dev/null | awk '{print $1}')
          rm -rf "$sub" 2>/dev/null && log "RM    artefact $(basename "$(dirname "$sub")")/$(basename "$sub") in $(basename "$wt") ($sz)"
        fi
      done
    done
  fi

  log "=== prune done: removed=$removed skipped=$skipped unsafe=$unsafe stray_removed=$stray_removed stray_unsafe=$stray_unsafe failures=$failures at_risk=$at_risk_total fetch_failed=$fetch_failed ==="
  df -h / | sed -n '2p' | sed 's/^/  /' | tee -a "$LOG"

  # EXIT CODES — see the header. In order of who should look:
  #
  # 1  the pruner could not do its job. A removal failed, a card could not be read
  #    (so that worktree can never be judged), or the fetch failed and the verdict
  #    was taken on stale refs. Alert on this.
  [ "$failures" -gt 0 ] && exit 1
  [ "$fetch_failed" -gt 0 ] && exit 1
  #
  # 3  "no progress": nothing was reclaimed even though work whose only copy may be
  #    this directory was kept. Narrow ON PURPOSE — a board that keeps a few at-risk
  #    directories while still reclaiming the reclaimable rest is a normal busy board
  #    (exit 0). A pruner that suddenly keeps EVERYTHING is the different thing worth
  #    waking someone for, and it is the only thing this code means now. A dry run
  #    reclaims nothing by design, so it is never judged on this.
  if [ "$DRY_RUN" != "1" ] && [ "$at_risk_total" -gt 0 ] \
     && [ "$removed" -eq 0 ] && [ "$stray_removed" -eq 0 ]; then
    exit 3
  fi
  #
  # 0  the run did its job: every removal it attempted succeeded, and whatever it
  #    kept it kept on purpose (an unpushed local branch, a hand-made mirror, a live
  #    worker, a too-recent card, a dirty tree) — each named in the log above.
  exit 0
}

[ "${JF_PRUNE_LIB_ONLY:-0}" = "1" ] || main "$@"
