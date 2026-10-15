#!/usr/bin/env bash
#
# scripts/refguard.sh — keep `refs/remotes/` for the refs GIT ITSELF TRACKS.
#
# THE DEFECT THIS EXISTS TO CLOSE
#   `git fetch origin pull/<N>/head:refs/remotes/pr/<N>` is the GitHub-documented
#   idiom for checking out a PR (github docs, "Checking out pull requests
#   locally"). It creates a ref under `refs/remotes/` that is in NO refspec:
#   `remote.origin.fetch` is `+refs/heads/*:refs/remotes/origin/*`, and git
#   prunes "as a function of the refspec of the remote" (git-fetch, PRUNING), so
#   `git fetch --prune origin` and `git remote prune origin` can never remove it.
#   It therefore keeps pointing at whatever the PR's head was WHEN IT WAS MADE —
#   the pre-rebase / pre-force-push snapshot — while looking exactly like a
#   remote-tracking ref to every reader, including `git branch -r --contains`.
#   That is how a superseded commit gets read as "what the PR contains" or as
#   "pushed to a remote", and how a defect gets reported against code that is not
#   there.
#
#   Deleting the mirrors is NOT the fix and has already failed once: measured
#   after the 2026-09-21 02:13 UTC surgery that left `count=0`, the namespace
#   was re-populated within 20 minutes — 26 refs by 2026-09-22 03:34, 6 of them
#   already stale. A cleanup is a point in time; the thing that re-creates them
#   is a command any lane can type. So the guard REFUSES the creation.
#
# WHY A HOOK AND NOT A CLEANUP OR A CONVENTION
#   A convention ("use refs/prhead/<N>") is what the manual already prescribes
#   and it still did not hold. `git update-ref -d` loops are the cleanup that
#   failed. The only mechanism that can stop a ref from being created is git's
#   own `reference-transaction` hook, and git invokes it for each writer that
#   opens a reference transaction — fetch, update-ref, push, notes. Measured
#   (see scripts/refguard_test.sh, which runs it offline): with the hook
#   installed, the documented idiom fails LOUDLY (`rc=128`, `fatal: ref updates
#   aborted by hook`) in the primary checkout AND in every linked worktree, while
#   `fetch --prune`, commit, branch, checkout, merge, rebase, stash, tag, push,
#   worktree add/remove, gc and repack all still exit 0. The blast radius is
#   exactly "create a refs/remotes ref that git does not track".
#
# WHAT THE HOOK CANNOT SEE — the boundary of the rule above
#   The hook is a `reference-transaction` hook, so it sees a ref write ONLY if
#   the writer opens a reference transaction. `git symbolic-ref` does not: it
#   writes the symref file directly, the hook is invoked ZERO times, and the ref
#   IS created. Measured on git 2.43.0, the version on this host:
#     git symbolic-ref refs/remotes/pr/910 refs/remotes/origin/main   -> rc=0
#     git branch -r                                          -> pr/910 -> origin/main
#   which is a hand-made ref wearing exactly the shape of a remote-tracking one —
#   the reading this tool exists to stop — with the hook given no say in it. A
#   direct write into refs/ (echo > .git/refs/remotes/pr/1) is out of reach for
#   the same reason. So the promise is stated as what it is: the hook refuses
#   every TRANSACTIONAL writer; a writer that bypasses the transaction is a
#   `scan` matter, not a hook matter. `scan` counts such a symref (one whose
#   target resolves) and `scan --strict` exits 1 on it. A DANGLING symref is
#   skipped by `git for-each-ref`, so `scan` does not count that shape — and
#   `git branch -r` skips it too, so it is junk on disk rather than a false
#   reading. Every shape stays DELETABLE, which is what keeps the namespace
#   cleanable — but only in a form that does NOT DEREFERENCE. A symref is
#   removed with `git symbolic-ref -d` (measured rc=0, file ABSENT, target
#   intact, and it goes through the transaction) or `git update-ref --no-deref
#   -d` (measured the same). PLAIN `git update-ref -d <symref>` is NOT a
#   cleanup: git-update-ref(1) stores the value "possibly dereferencing the
#   symbolic refs" (`--no-deref` is "update <refname> not the one it points
#   to"), so it FOLLOWS the symref and deletes its TARGET — measured on git
#   2.43.0: rc=0, EMPTY stderr, a real `refs/remotes/origin/main` DESTROYED —
#   and leaves the symref dangling on disk, where `git for-each-ref` skips it,
#   so `scan` then reports the namespace CLEAN over a file that is still there.
#   An exit 0 is not evidence that anything was deleted. Both the safe forms
#   and that destruction are asserted in case 10. Upstream git has been teaching
#   the refs API to update symrefs
#   transactionally (`git update-ref --stdin`'s symref-create/symref-delete/
#   symref-verify; git 2.46 RelNotes, "Updates to symbolic refs can now be made
#   as a part of ref transaction"), so this boundary is git's and version
#   dependent, not a property of the guard. scripts/refguard_test.sh asserts the
#   boundary of the git it actually runs on.
#
# IT IS INSTALLED IN THE COMMON GIT DIR, NOT VIA core.hooksPath
#   `$(git rev-parse --git-path hooks)` resolves to the COMMON dir's hooks
#   (`<repo>/.git/hooks`) from the primary checkout AND from every linked
#   worktree, and the hook is run from both — which matters, because the lanes do
#   their work in worktrees. A RELATIVE `core.hooksPath` does NOT work: measured,
#   the hook fired in the primary checkout and was silently skipped in a linked
#   worktree (each worktree resolves a relative hooksPath against its own root).
#   The default `$GIT_DIR/hooks` location has no such trap.
#
# WHAT IT COSTS (measured, so a future reader does not have to re-derive it)
#   git invokes the hook once per ref update, in a fresh process. Measured on the
#   agent host: 15 ms for a state it ignores outright (bash startup), 21 ms for a
#   refs/heads update, 37 ms when the allow-list has to spawn `git remote`, 29 ms
#   for a refusal. Read-only commands invoke it ZERO times — measured on `git
#   status`, `git log`, `git stash list` and a no-op `git fetch --prune origin`.
#   So the cost is bounded by the number of refs a command actually MOVES, which
#   is a handful for daily work and is only conspicuous on a bulk initial fetch.
#   That is accepted deliberately: the defect this prevents cost whole lanes.
#
# WHAT IS STILL ALLOWED
#   * Any ref under a configured remote's namespace — `refs/remotes/<remote>/*`
#     for every name in `git remote`, plus the `refs/remotes/<remote>` symref.
#     The allow-list is built from `git remote`, so adding a second remote
#     (a fork, `gh pr checkout`'s fork remote) keeps working.
#   * DELETION of a mirror. The rule is one-way on purpose: you may remove a
#     hand-made ref, you may not create or move one. A guard that also refused
#     deletion would make the namespace uncleanable except by rm-ing pack files.
#     The FORM matters: remove a symref with `git symbolic-ref -d` (or
#     `git update-ref --no-deref -d`) — the boundary paragraph above says why
#     plain `git update-ref -d` is not one.
#
# USAGE
#   refguard.sh install [repo]        install the hook (idempotent)
#   refguard.sh ensure  [repo]        install the hook only if it is missing/not executable/stale
#   refguard.sh check   [repo]        assert the hook is installed AND current; exit 1 if not
#   refguard.sh scan    [repo]        report every non-remote `refs/remotes/*` ref
#              --live                    compare each mirror against the live refs/pull/<N>/head
#              --strict                  exit 1 when any such ref exists (cron / handoff)
#   refguard.sh hook <state>          the hook body; git calls the installed copy this way
#
# EXIT CODES
#   install/ensure/check/scan: 0 ok, 1 the property does not hold, 2 cannot measure
#   (a scan that cannot reach the remote exits 2 rather than reporting "0 stale").
#
# The `refguard` preflight gate runs scripts/refguard_test.sh, which proves every
# claim above offline (sandbox repos, no network): that the idiom is refused, in
# the primary checkout and in a worktree, that the prescribed alternatives still
# work, that normal git flow is untouched, and how far the rule reaches (the
# `git symbolic-ref` boundary, where `scan` — not the hook — is the detector).

set -uo pipefail

ALL_ZERO=0000000000000000000000000000000000000000
# Resolve THIS script's own path, and install/compare against ITSELF rather than
# against a hardcoded "$HERE/refguard.sh". Two reasons, one of them a defect this
# caught: (a) the guard must be relocatable — a host copy under another name is a
# legitimate deployment, and a guard that can only install a file literally named
# `refguard.sh` silently fails (`cp` -> rc=2) anywhere else; (b) `check` has to
# answer "is the installed hook the same as the ME that is running", which is the
# only question that detects a stale installed copy.
GUARD_SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"

# ---------------------------------------------------------------------------
# Remotes whose refs/remotes/<name>/* namespace is legitimate.
# Built from `git remote` so a second remote (a fork, an `upstream`) is allowed
# without editing this file. Falls back to `origin` when the call fails (the
# hook runs with GIT_DIR exported; a repo with no remote is still safe because
# an empty allow-list only matters for refs/remotes/* refs, which then have no
# legitimate remote to belong to).
# ---------------------------------------------------------------------------
remote_names() { # <repo> -> remote names, one per line
  local repo="$1" names
  names="$(git -C "$repo" remote 2>/dev/null)"
  if [ -z "$names" ]; then printf 'origin\n'; else printf '%s\n' "$names"; fi
}

# is_remote_tracking <repo> <refname> -> 0 if the ref belongs to a configured remote
is_remote_tracking() {
  local repo="$1" ref="$2" name
  while IFS= read -r name; do
    [ -z "$name" ] && continue
    case "$ref" in
      "refs/remotes/$name" | "refs/remotes/$name/"*) return 0 ;;
    esac
  done < <(remote_names "$repo")
  return 1
}

# ---------------------------------------------------------------------------
# HOOK BODY — invoked by git as the installed `reference-transaction` hook.
#
# git calls this with preparing|prepared|committed|aborted, and passes one line
# per ref UPDATE on stdin:
#     <old-value> SP <new-value> SP <ref-name> LF
# The exit status is honoured for "preparing" and "prepared" only; a non-zero
# exit aborts the whole transaction. This acts on "prepared" (refs are locked,
# nothing is committed yet, so the abort is clean).
#
# THE STDIN READ MUST CONSUME TO EOF, ALWAYS. If the loop exits early, the
# writer of that pipe (git) dies of SIGPIPE and the hook's status is that
# signal instead — so the verdict is computed over the WHOLE transaction and
# returned only after the last line is read. This is also a correctness
# requirement, not just a hygiene one: a transaction whose forbidden ref is the
# LAST line must still be refused.
# ---------------------------------------------------------------------------
hook_main() {
  local state="${1:-}" rc=0 ref new old repo="."
  case "$state" in
    preparing|prepared|committed|aborted) ;;
    *) return 0 ;;
  esac
  [ "$state" = "prepared" ] || return 0

  while IFS=' ' read -r old new ref; do
    [ -n "${ref:-}" ] || continue
    case "$ref" in
      refs/remotes/*) ;;
      *) continue ;;
    esac
    # A deletion is allowed: it only shrinks the namespace.
    case "$new" in "$ALL_ZERO" | "") continue ;; esac
    if is_remote_tracking "$repo" "$ref"; then continue; fi
    printf 'refguard: refusing %s\n' "$ref" >&2
    printf '  refs/remotes/ is for the refs git tracks; this name belongs to no configured remote.\n' >&2
    printf '  A hand-made mirror there is in NO refspec, so --prune can never remove it and it\n' >&2
    printf '  silently goes stale. For a PR head use:  git fetch origin "refs/pull/<N>/head:refs/prhead/<N>"\n' >&2
    printf '  or read it without creating a ref:       git ls-remote origin refs/pull/<N>/head\n' >&2
    rc=1
  done
  return "$rc"
}

# ---------------------------------------------------------------------------
# install
# ---------------------------------------------------------------------------
hooks_dir() { git -C "$1" rev-parse --git-path hooks 2>/dev/null; }

install_main() {
  local repo="$1" hooks target
  hooks="$(hooks_dir "$repo")" || return 2
  [ -n "$hooks" ] || return 2
  case "$hooks" in /*) ;; *) hooks="$repo/$hooks" ;; esac
  target="$hooks/reference-transaction"

  if ! mkdir -p "$hooks" 2>/dev/null; then
    echo "refguard: cannot create $hooks" >&2; return 2
  fi
  if [ -f "$target" ] && cmp -s "$GUARD_SELF" "$target"; then
    echo "refguard: hook already current at $target"
    return 0
  fi
  if [ -e "$target" ] && ! cmp -s "$GUARD_SELF" "$target"; then
    echo "refguard: replacing an existing, DIFFERENT $target" >&2
  fi
  # A copy, not a symlink: a symlink breaks the moment this file moves, and git
  # skips a hook that is not executable, which is a silent no-guard.
  #
  # WRITE VIA A TEMP FILE AND RENAME. `cp` onto the live hook truncates and
  # rewrites in place, and this runs from every lane's preflight: two concurrent
  # lanes can leave a PARTIALLY WRITTEN hook on disk, which git then fails to
  # parse — breaking an unrelated git command with a hook error. A rename within
  # the same directory is atomic, so a reader sees either the old hook or the new
  # one, never a half of one.
  local tmp="$target.refguard-$$"
  if ! cp "$GUARD_SELF" "$tmp"; then
    echo "refguard: cannot write $tmp" >&2; rm -f "$tmp"; return 2
  fi
  if ! chmod 755 "$tmp"; then
    echo "refguard: cannot chmod $tmp" >&2; rm -f "$tmp"; return 2
  fi
  if ! mv -f "$tmp" "$target"; then
    echo "refguard: cannot install $target" >&2; rm -f "$tmp"; return 2
  fi
  echo "refguard: installed $target"
  echo "refguard: all worktrees of $repo share it (git resolves the hooks dir in the common git dir)"
}

# ---------------------------------------------------------------------------
# check — is the guard ARMED? A missing, non-executable or stale hook is the
# failure this function exists to make visible: the mirrors are merely reported,
# and nothing would stop the next one being created.
#
# The exit status answers the GUARD question only (0 armed, 1 not armed, 2 cannot
# measure). It deliberately does not fail on mirrors being present: "is the guard
# armed" and "is the namespace clean" are two questions, and the gate below needs
# the first one to be answerable per-repo. `scan --strict` is the second.
# ---------------------------------------------------------------------------
check_main() {
  local repo="$1" hooks target rc=0
  hooks="$(hooks_dir "$repo")" || { echo "refguard: cannot resolve the hooks dir" >&2; return 2; }
  case "$hooks" in /*) ;; *) hooks="$repo/$hooks" ;; esac
  target="$hooks/reference-transaction"

  if [ ! -e "$target" ]; then
    echo "refguard: NOT ARMED — $target does not exist: nothing refuses a new refs/remotes mirror" >&2
    rc=1
  elif [ ! -x "$target" ]; then
    echo "refguard: NOT ARMED — $target is not executable, and git ignores a non-executable hook" >&2
    rc=1
  elif ! cmp -s "$GUARD_SELF" "$target"; then
    echo "refguard: NOT ARMED — $target differs from $GUARD_SELF (installed copy is stale)" >&2
    rc=1
  else
    echo "refguard: armed ($target, current)"
  fi

  echo "refguard: $(mirror_count "$repo") hand-made ref(s) under refs/remotes/ right now (see \`scan\`)"
  return "$rc"
}

mirror_count() { # <repo> -> count of non-remote refs/remotes refs
  local repo="$1" ref n=0
  while IFS= read -r ref; do
    [ -z "$ref" ] && continue
    is_remote_tracking "$repo" "$ref" || n=$((n+1))
  done < <(git -C "$repo" for-each-ref --format='%(refname)' refs/remotes/ 2>/dev/null)
  printf '%s\n' "$n"
}

# ---------------------------------------------------------------------------
# ensure — ARM the guard if it is not already armed. This is what the preflight
# gate calls.
#
# WHY "ARM AUTOMATICALLY" IS THE RIGHT ANSWER HERE, and why it is not a
# fail-open. The alternative (a human runs `install` once after the PR merges) is
# exactly the shape that failed before: a manual step in a lane's path is a step
# that does not happen, and then there is no guard and nothing says so. This
# command makes an unarmed repo armed by running the repo's OWN rule from the
# repo's OWN file, and it still FAILS (rc=1) when it cannot — no `|| true`,
# no silent skip.
#
# It refuses to arm a STALE checkout over a good one only in the sense that it
# always installs the file it is running; `check` is the command that reports a
# divergence instead of fixing it, and the gate runs `check` first so a
# divergence is announced.
# ---------------------------------------------------------------------------
ensure_main() {
  local repo="$1" hooks target
  hooks="$(hooks_dir "$repo")" || { echo "refguard: cannot resolve the hooks dir" >&2; return 2; }
  case "$hooks" in /*) ;; *) hooks="$repo/$hooks" ;; esac
  target="$hooks/reference-transaction"

  if [ -f "$target" ] && cmp -s "$GUARD_SELF" "$target" && [ -x "$target" ]; then
    echo "refguard: already armed ($target)"
    return 0
  fi
  if [ ! -e "$target" ]; then
    echo "refguard: no hook at $target — arming it now"
  elif [ ! -x "$target" ]; then
    echo "refguard: hook at $target is not executable (git would ignore it) — arming it now"
  else
    echo "refguard: hook at $target differs from the repo's rule — reinstalling it"
  fi
  install_main "$repo"
}

# ---------------------------------------------------------------------------
# scan — the measurement a lane/verify card runs, replacing a hand-rolled one.
# ---------------------------------------------------------------------------
scan_main() {
  local repo="$1" live=0 strict=0
  shift || true
  while [ $# -gt 0 ]; do
    case "$1" in
      --live)   live=1 ;;
      --strict) strict=1 ;;
      *) echo "refguard scan: unknown argument $1" >&2; return 2 ;;
    esac
    shift
  done

  # The allow-list is `git remote`, not the literal word "origin", so the filter
  # is the same helper the hook uses rather than a pattern that could drift.
  local -a mirrors=()
  local line
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    is_remote_tracking "$repo" "${line%% *}" || mirrors+=("$line")
  done < <(git -C "$repo" for-each-ref --format='%(refname) %(objectname)' refs/remotes/ 2>/dev/null)

  local total=${#mirrors[@]}
  echo "=== hand-made refs/remotes/* refs (not under any configured remote) ==="
  if [ "$total" -eq 0 ]; then
    echo "none — the namespace is clean"
  fi

  local live_refs="" live_ok=1
  # The live namespace is needed by BOTH the mirror table and the sibling report,
  # so it is fetched before either. (An earlier cut returned from the "no mirrors"
  # branch before reaching the sibling report, which made the report unreachable
  # in exactly the case where a stale sibling is the ONLY trap left.)
  if [ "$live" = 1 ]; then
    if live_refs="$(git -C "$repo" ls-remote origin 'refs/pull/*/head' 2>/dev/null)" && [ -n "$live_refs" ]; then
      live_ok=0
    else
      echo "refguard: CANNOT MEASURE — 'git ls-remote origin refs/pull/*/head' failed or returned nothing." >&2
      echo "refguard: refusing to report a staleness verdict from no live data (that is how a" >&2
      echo "refguard: hand-made mirror reads as current)." >&2
      return 2
    fi
  fi

  local stale=0 hazard=0
  if [ "$total" -gt 0 ]; then
    printf '%-26s %-12s %s\n' REF SHA VERDICT
    local name num want others oref
    for line in "${mirrors[@]}"; do
    ref="${line%% *}"; sha="${line##* }"
    name="${ref#refs/remotes/}"
    # Other holders, EXCLUDING every hand-made refs/remotes ref (this one and any
    # sibling mirror, however it is spelled): a SHA held only by mirrors is the
    # 05a50e0 class, where deleting the mirror leaves the commit unreachable.
    others=""
    while IFS= read -r oref; do
      [ -z "$oref" ] && continue
      [ "$oref" = "$ref" ] && continue
      case "$oref" in
        refs/remotes/*) is_remote_tracking "$repo" "$oref" || continue ;;
      esac
      others="${others:+$others,}$oref"
    done < <(git -C "$repo" for-each-ref --format='%(refname)' --contains "$sha" 2>/dev/null)
    if [ -z "$others" ]; then
      hazard=$((hazard+1))
      printf '%-26s %-12s DELETE-LOSES-COMMIT (held by no other ref)\n' "$name" "${sha:0:10}"
      continue
    fi
    if [ "$live" = 1 ]; then
      num="$(pr_number_of "$ref")"
      want=""
      [ -n "$num" ] && want="$(printf '%s\n' "$live_refs" | awk -v r="refs/pull/$num/head" '$2==r{print $1}')"
      if [ -z "$num" ]; then
        printf '%-26s %-12s name encodes no PR number (ad-hoc handle)\n' "$name" "${sha:0:10}"
      elif [ -z "$want" ]; then
        printf '%-26s %-12s NO-LIVE-REF (merged/closed)\n' "$name" "${sha:0:10}"
      elif [ "$want" = "$sha" ]; then
        printf '%-26s %-12s current\n' "$name" "${sha:0:10}"
      else
        stale=$((stale+1))
        printf '%-26s %-12s STALE (live refs/pull/%s/head = %s)\n' "$name" "${sha:0:10}" "$num" "${want:0:10}"
      fi
    else
      printf '%-26s %-12s (not compared; pass --live)\n' "$name" "${sha:0:10}"
    fi
    done
  fi

  echo "---"
  echo "mirrors=$total stale=$stale delete_loses_commit=$hazard"

  # SIBLING NAMESPACES — refs/<something>/<pr-number> that are NOT under
  # refs/remotes/. This guard deliberately allows these (it refuses the dangerous
  # PLACEMENT, not the idea of holding a PR head), and this repo's convention is
  # `refs/prhead/<N>`. But nothing prunes them either, so the same trap — a ref
  # frozen at a superseded head while reading like a current handle — applies the
  # moment one is left behind. Reporting them is not scope creep: the manual
  # prescribes this namespace, and prescribing a name with no way to detect a
  # stale ref in it would move the trap rather than remove it.
  local sib_note=""
  sib_note="$(scan_siblings "$repo" "$live" "$live_refs")"
  if [ -n "$sib_note" ]; then
    printf '%s\n' "$sib_note"
  fi

  if [ "$live" = 1 ]; then
    echo "compared against the live refs/pull/<N>/head namespace (ls-remote)."
  fi
  if [ "$hazard" -gt 0 ]; then
    echo "A DELETE-LOSES-COMMIT ref is the 05a50e0 class: preserve the SHA as a ref or a patch BEFORE deleting it."
  fi
  # --strict is about the DEFECT only: hand-made refs/remotes refs. The advisory
  # sibling report must not make it fail (those refs are allowed), and a repo with
  # zero mirrors must exit 0 — which is why this checks the count and not just the
  # flag. (An earlier restructure returned 1 whenever --strict was set at all.)
  if [ "$strict" = 1 ] && [ "$total" -gt 0 ]; then return 1; fi
  return 0
}

# ---------------------------------------------------------------------------
# pr_number_of <refname> -> the PR number a ref name encodes, or empty.
#   Accepts the shapes this repo actually uses: refs/remotes/pr/705, pr696v,
#   refs/prhead/739, refs/tmp/pr675, refs/qa/612, refs/archive/pr660-prerework-05a50e0.
# Empty means "this name is not a PR number", which the caller reports as such
# rather than inventing a verdict for it — an earlier cut took the trailing digits
# of `...pr660-prerework-05a50e0` and reported "refs/pull/0/head", a misleading
# reading of the same class this whole tool exists to stop.
# ---------------------------------------------------------------------------
pr_number_of() {
  local ref="$1" n
  # `pr<digits>` anywhere (covers pr/705 -> no, pr696v -> 696, pr660-... -> 660)
  n="$(printf '%s' "$ref" | sed -n 's/.*[^0-9a-zA-Z]pr\([0-9][0-9]*\).*/\1/p')"
  [ -n "$n" ] && { printf '%s\n' "$n"; return 0; }
  # the whole last path segment is a number (refs/prhead/739, refs/qa/612)
  n="$(printf '%s' "$ref" | sed -n 's#.*/\([0-9][0-9]*\)$#\1#p')"
  [ -n "$n" ] && { printf '%s\n' "$n"; return 0; }
  # the last segment STARTS with a number (pr/705-now)
  n="$(printf '%s' "$ref" | sed -n 's#.*/\([0-9][0-9]*\)[^0-9].*#\1#p')"
  [ -n "$n" ] && { printf '%s\n' "$n"; return 0; }
  return 0
}

# ---------------------------------------------------------------------------
# scan_siblings — PR-head refs under a namespace that is NOT refs/remotes/.
# Reports what the manual prescribes (refs/prhead/<N>) plus the older ad-hoc
# ones, with the same staleness verdict the mirrors get. Advisory: it never
# changes scan's exit status, because these are ALLOWED refs, not the defect.
# ---------------------------------------------------------------------------
scan_siblings() { # <repo> <live:0|1> <live_refs>
  local repo="$1" live="$2" live_refs="$3"
  local -a sibs=()
  local ref sha
  while IFS= read -r ref; do
    [ -z "$ref" ] && continue
    case "$ref" in
      refs/remotes/*) continue ;;
      refs/heads/*)   continue ;;
      refs/tags/*)    continue ;;
    esac
    # Report ANY ref in these namespaces, not only the ones whose name parses as a
    # PR number: a handle a future reader cannot resolve is exactly what this
    # report is for, and the verdict column says which is which. (An earlier cut
    # filtered on a trailing digit, which silently hid the unparseable ones.)
    sibs+=("$ref")
  done < <(git -C "$repo" for-each-ref --format='%(refname)' \
             refs/prhead refs/tmp refs/qa refs/archive 2>/dev/null)

  [ "${#sibs[@]}" -eq 0 ] && return 0
  printf '%s\n' "---"
  printf '%s\n' "advisory: PR-head refs outside refs/remotes/ (allowed, but nothing prunes them):"
  printf '%-34s %-12s %s\n' REF SHA VERDICT
  local num want verdict
  for ref in "${sibs[@]}"; do
    sha="$(git -C "$repo" rev-parse --verify -q "$ref" 2>/dev/null)" || continue
    verdict="(not compared; pass --live)"
    if [ "$live" = 1 ]; then
      num="$(pr_number_of "$ref")"
      want=""
      [ -n "$num" ] && want="$(printf '%s\n' "$live_refs" | awk -v r="refs/pull/$num/head" '$2==r{print $1}')"
      if [ -z "$num" ]; then
        verdict="name encodes no PR number (ad-hoc handle; check it by hand)"
      elif [ -z "$want" ]; then
        verdict="no live refs/pull/$num/head (merged/closed)"
      elif [ "$want" = "$sha" ]; then
        verdict="current"
      else
        verdict="STALE (live refs/pull/$num/head = ${want:0:10})"
      fi
    fi
    printf '%-34s %-12s %s\n' "$ref" "${sha:0:10}" "$verdict"
  done
  return 0
}

# ---------------------------------------------------------------------------
# dispatch
# ---------------------------------------------------------------------------
cmd="${1:-}"
case "$cmd" in
  preparing|prepared|committed|aborted)
    hook_main "$@" ;;
  install|check|scan|ensure)
    shift
    repo="."
    case "${1:-}" in
      ""|--*) ;;
      *) repo="$1"; shift ;;
    esac
    [ -d "$repo" ] || { echo "refguard: no such directory: $repo" >&2; exit 2; }
    # Resolve to the repo ROOT: hooks and refs are repo-wide, and a lane that
    # runs this from a subdirectory of a worktree must get the same answer.
    repo="$(git -C "$repo" rev-parse --show-toplevel 2>/dev/null)" || {
      echo "refguard: $repo is not inside a git repository" >&2; exit 2; }
    case "$cmd" in
      install) install_main "$repo" ;;
      ensure)  ensure_main "$repo" ;;
      check)   check_main "$repo" ;;
      scan)    scan_main "$repo" "$@" ;;
    esac ;;
  ""|-h|--help)
    sed -n '2,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//' ;;
  *)
    echo "refguard: unknown command: $cmd (see --help)" >&2; exit 2 ;;
esac
