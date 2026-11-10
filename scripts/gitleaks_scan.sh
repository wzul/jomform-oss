#!/usr/bin/env bash
#
# scripts/gitleaks_scan.sh — the `gitleaks` preflight gate's scanner driver:
# INCREMENTAL by default, with a periodic FULL sweep.
#
# WHY (card t_cf0f6063 — Wan asked whether this gate gets slower forever)
#   `gitleaks detect --source .` walks EVERY REF. Measured on origin/main
#   (7defcceb2): 4,091 commits / 101.16 MB / 115.7 s cold and 50.2 s with the
#   module cache warm — on EVERY gate run, and the cost grows with history,
#   forever. The cheap shape is a range: `--log-opts=<a>..<b>` measured 113
#   commits / 4.56 MB / 1.24 s.
#
#   Capping the scan at the last N commits was REJECTED. The case this gate
#   exists for is a secret committed long ago and "deleted" later: the blob is
#   still in the packfile and still fetchable, and a fixed window silently stops
#   covering exactly the commits that matter. The answer is incremental scanning,
#   not amnesia:
#
#     * a WATERMARK — the newest commit whose ancestry has been scanned — lives
#       next to the other gate state, at
#       ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/gitleaks-watermark;
#     * each run scans `<watermark>..HEAD` and advances the watermark ONLY on
#       success, so a run that finds a leak keeps rescanning the same range (a
#       later commit that deletes the file cannot hide it);
#     * a FULL sweep runs whenever the watermark cannot be trusted — absent or
#       unreadable, written by another checkout, missing from this clone (a fresh
#       or partial clone), not an ancestor of HEAD (force-push/rebase), or when
#       the last sweep is older than GITLEAKS_FULL_SWEEP_SECONDS (default 7 days)
#       — and it is the ONLY thing that covers non-HEAD refs, so it must never be
#       dropped.
#
# WHY THE WATERMARK IS merge-base(HEAD, main), NOT HEAD
#   The gate runs in per-card worktrees and merge-train branches that are
#   SIBLINGS of one another. Recording HEAD would make the next tree see a
#   watermark that is not its ancestor and force a full sweep — paying the 115 s
#   path on every run, which is the defect rather than the fix. The merge-base
#   with main is an ancestor of HEAD whenever the branch descends from main, and
#   IS HEAD when it does not, so the next card's `watermark..HEAD` stays short.
#   Soundness does not depend on that choice: `<wm>..HEAD` covers ancestry(HEAD)
#   minus ancestry(<wm>), and ancestry(<wm>) was covered by the runs that set it,
#   so the union is ancestry(HEAD) — every ancestor of the recorded commit is
#   covered.
#
# SHALLOW CLONE: refused, loudly. A shallow clone cannot answer this question; it
#   would scan a handful of commits and report "no leaks found", which is worse
#   than not running.
#
# WHAT A HOST-STATE PROBLEM DOES NOT DO: it never reds the gate. An unwritable
#   state directory only means the watermark cannot advance (and every run is then
#   a full sweep), which is announced, not fatal — same rule as the per-tree gate
#   record in scripts/gate_result.sh.
#
# Usage:  scripts/gitleaks_scan.sh [<repo-root>]     (default: this script's repo)
# Env:    GITLEAKS_FORCE_FULL=1        force a full sweep (operators, bite proof)
#         GITLEAKS_FULL_SWEEP_SECONDS  cadence in seconds (default 604800 = 7 days)
#         GITLEAKS_WATERMARK           explicit watermark file (bite proof)
#         GITLEAKS_SCANNER_BIN         a gitleaks binary instead of `go run …@latest`
# Exit:   the scanner's status (0 clean, non-zero = leaks or a scanner failure).
set -uo pipefail

SELF_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT="${1:-$SELF_ROOT}"

die() { echo "gitleaks: $*" >&2; exit 1; }

[ -d "$ROOT" ] || die "no such repo root: $ROOT"
cd "$ROOT" || die "cannot cd to $ROOT"
git rev-parse --git-dir >/dev/null 2>&1 || die "$ROOT is not a git checkout — the full-history scan is impossible"

# ---- 1. Shallow clone -------------------------------------------------------
# Kept verbatim in intent from the gate this replaces: a shallow checkout cannot
# answer a history question, and reporting "no leaks found" from one is a lie.
if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = "true" ]; then
  echo "this is a SHALLOW clone — the full-history secret scan is impossible; it would scan a handful of commits and report 'no leaks found'. In CI this was fetch-depth: 0. Unshallow first:" >&2
  echo "  git fetch --unshallow" >&2
  exit 1
fi

head_sha="$(git rev-parse --verify HEAD 2>/dev/null)" || die "no HEAD commit in $ROOT — nothing can be scanned"
head_short="$(git rev-parse --short HEAD)"

# ---- 2. State ---------------------------------------------------------------
STATE_DIR="${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}"
WATERMARK_FILE="${GITLEAKS_WATERMARK:-$STATE_DIR/gitleaks-watermark}"
FULL_SWEEP_SECONDS="${GITLEAKS_FULL_SWEEP_SECONDS:-604800}"

# A watermark is evidence about THIS object store: two checkouts (or a fresh
# clone at another path) have their own. The git COMMON dir is the shared object
# store of a repo and all its worktrees, which is exactly the scope a commit sha
# is meaningful in.
common_dir="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
[ -n "$common_dir" ] || common_dir="$(git rev-parse --git-common-dir 2>/dev/null)"
repo_key="$(printf '%s' "$common_dir" | sha256sum | cut -c1-16)"

# state_get KEY — one key=value field of the watermark file, or empty.
state_get() {
  [ -r "$WATERMARK_FILE" ] || return 0
  awk -v k="$1" 'index($0, k "=") == 1 { print substr($0, length(k) + 2); exit }' "$WATERMARK_FILE" 2>/dev/null
}

wm_commit="$(state_get commit)"
wm_repokey="$(state_get repokey)"
wm_full_at="$(state_get full_at)"
wm_mode="$(state_get mode)"
wm_at="$(state_get at)"

now="$(date +%s)"
mode="incremental"
reason=""

# ---- 3. Which mode, and why --------------------------------------------------
if [ "${GITLEAKS_FORCE_FULL:-0}" = "1" ]; then
  mode="full"; reason="forced: GITLEAKS_FORCE_FULL=1"
elif [ ! -r "$WATERMARK_FILE" ]; then
  mode="full"; reason="no watermark at $WATERMARK_FILE"
elif [ "$(state_get format)" != "1" ] || [ -z "$wm_commit" ] || [ -z "$wm_repokey" ]; then
  mode="full"; reason="watermark file is unreadable or not format=1"
elif [ "$wm_repokey" != "$repo_key" ]; then
  mode="full"; reason="watermark belongs to another checkout (repokey $wm_repokey, this checkout $repo_key)"
elif ! git cat-file -e "${wm_commit}^{commit}" 2>/dev/null; then
  mode="full"; reason="watermark commit ${wm_commit:0:12} is not in this clone (fresh or partial clone)"
elif ! git merge-base --is-ancestor "$wm_commit" HEAD 2>/dev/null; then
  mode="full"; reason="watermark ${wm_commit:0:12} is not an ancestor of HEAD (force-push or rebase)"
elif [[ ! "$wm_full_at" =~ ^[0-9]+$ ]]; then
  mode="full"; reason="no recorded full sweep"
else
  age=$(( now - wm_full_at ))
  if [ "$age" -ge "$FULL_SWEEP_SECONDS" ]; then
    mode="full"; reason="last full sweep was $(( age / 86400 )) day(s) ago (cadence ${FULL_SWEEP_SECONDS}s)"
  else
    reason="watermark ${wm_commit:0:12} (last full sweep $(( age / 86400 )) day(s) ago)"
  fi
fi

# ---- 4. Scan -----------------------------------------------------------------
scan_gitleaks() {
  if [ -n "${GITLEAKS_SCANNER_BIN:-}" ]; then
    "$GITLEAKS_SCANNER_BIN" "$@"
    return $?
  fi
  local toolchain
  # The repo's go directive lives in api/go.mod; the root has none, so resolving
  # the toolchain from the root reports the host default and gitleaks v8 (which
  # needs go >= 1.24.11) then fails to BUILD — a gate red for a reason that has
  # nothing to do with secrets.
  toolchain="$(cd "$SELF_ROOT/api" 2>/dev/null && go env GOVERSION)"
  [ -n "$toolchain" ] || toolchain="$(go env GOVERSION)"
  GOTOOLCHAIN="$toolchain" go run github.com/zricethezav/gitleaks/v8@latest "$@"
}

if [ "$mode" = "full" ]; then
  scan_commits="$(git rev-list --count --all 2>/dev/null)"
  echo "gitleaks: full sweep: ${scan_commits:-?} commits across all refs ($reason)"
  # -v so a RED run NAMES the file, rule and commit it found: a gate that prints
  # only "leaks found: 1" makes the reader hunt, and makes a finding impossible to
  # attribute (the bite proof asserts the finding names its own file). Matches are
  # still redacted by --redact.
  scan_gitleaks detect --source . --no-banner --redact -v --config "$ROOT/.gitleaks.toml"
  rc=$?
else
  scan_commits="$(git rev-list --count "$wm_commit..HEAD" 2>/dev/null)"
  echo "gitleaks: incremental: ${scan_commits:-?} commits since ${wm_commit:0:12} ($reason)"
  scan_gitleaks detect --source . --no-banner --redact -v --config "$ROOT/.gitleaks.toml" \
    --log-opts="$wm_commit..HEAD"
  rc=$?
fi

if [ "$rc" -ne 0 ]; then
  # NOTHING is advanced on a red run: the next run rescans the same range, so a
  # later commit that deletes the file cannot hide the finding.
  echo "gitleaks: scan FAILED (rc=$rc) — the watermark was NOT advanced; the next run rescans from $( [ "$mode" = full ] && echo 'the full sweep' || echo "${wm_commit:0:12}" )" >&2
  exit "$rc"
fi

# ---- 5. Advance the watermark (success only) ---------------------------------
# merge-base(HEAD, main): HEAD itself when HEAD is on main, otherwise the point
# the branch diverged — see the header for why recording HEAD would force a full
# sweep on every sibling tree.
main_ref=""
for r in refs/remotes/origin/main refs/heads/main; do
  if git rev-parse --verify --quiet "$r^{commit}" >/dev/null 2>&1; then main_ref="$r"; break; fi
done
new_wm=""
if [ -n "$main_ref" ]; then
  new_wm="$(git merge-base HEAD "$main_ref" 2>/dev/null)"
fi
# No main ref, or no common ancestor: HEAD is still covered by the scan that just
# passed, so recording it is sound — it is only less economical.
[ -n "$new_wm" ] || new_wm="$head_sha"

if [ "$mode" = "full" ]; then
  new_full_at="$now"
else
  new_full_at="$wm_full_at"
fi

write_state() {
  local dir tmp
  dir="$(dirname "$WATERMARK_FILE")"
  if ! mkdir -p "$dir" 2>/dev/null; then
    echo "gitleaks: WARNING — cannot create $dir; the watermark will not advance and every run will be a full sweep." >&2
    return 0
  fi
  tmp="$WATERMARK_FILE.tmp.$$"
  if ! {
    echo "format=1"
    echo "repokey=$repo_key"
    echo "commit=$new_wm"
    echo "at=$now"
    echo "full_at=$new_full_at"
    echo "mode=$mode"
    echo "commits=${scan_commits:-0}"
  } > "$tmp" 2>/dev/null; then
    rm -f "$tmp" 2>/dev/null
    echo "gitleaks: WARNING — cannot write $tmp; the watermark will not advance and every run will be a full sweep." >&2
    return 0
  fi
  if ! mv -f "$tmp" "$WATERMARK_FILE" 2>/dev/null; then
    rm -f "$tmp" 2>/dev/null
    echo "gitleaks: WARNING — cannot replace $WATERMARK_FILE; the watermark will not advance and every run will be a full sweep." >&2
  fi
  return 0
}
write_state

echo "gitleaks: clean ($mode) — watermark now ${new_wm:0:12}, head $head_short, last full sweep $(date -u -d "@$new_full_at" '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null)"
exit 0
