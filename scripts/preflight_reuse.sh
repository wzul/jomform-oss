#!/usr/bin/env bash
#
# scripts/preflight_reuse.sh — read the per-tree gate RESULT RECORD.
#
# This is the CONSUMER side of scripts/gate_result.sh. It is what a lane runs
# BEFORE it queues for the gate: the gate runs behind a bounded host-wide POOL of
# `PREFLIGHT_SLOT_COUNT` slots (default 2, card t_79c0964a) and a waiter for a
# slot has been measured at 50-86 minutes, so a tree whose verdict is already
# known should not join the queue at all.
#
# It NEVER gates anything and it NEVER takes the gate lock. It only reads host
# state and prints a decision, so it is safe to call from anywhere and it cannot
# itself become the thing everything queues behind.
#
# THE TWO DECISIONS
#   * full    — every gate in the current inventory is recorded `pass` for THIS
#               tree, under the CURRENT environment fingerprint, inside the TTL.
#               `PREFLIGHT_SKIP=1` is then legal: the verdict already exists for
#               these bytes, and re-running it is the 39.4% waste this exists to
#               remove. Exit 0.
#   * partial — the record names this tree and this environment, and holds
#               `pass` entries for SOME gates (the rest failed, were skipped, or
#               were never observed). Exit 0 with PREFLIGHT_SKIP holding only the
#               observed passes: re-running a gate that already passed on these
#               exact bytes adds cost, not information. The failed gates are
#               re-run. A red can never come out green — the failed gates are in
#               PREFLIGHT_RERUN, not in PREFLIGHT_SKIP.
#   * none    — no usable record. Exit 1, and the caller gates normally.
#
# USAGE
#   scripts/preflight_reuse.sh                  # decide for the working tree
#   scripts/preflight_reuse.sh --tree <sha>     # decide for an explicit tree —
#                                               # <sha> must be a 40-character
#                                               # lowercase hex tree sha; any
#                                               # other value is REFUSED (exit 2)
#                                               # rather than answered about the
#                                               # working tree (see EXIT CODES)
#   scripts/preflight_reuse.sh --show           # print the record (evidence);
#                                               # rc 0 when it is reusable, 1
#                                               # when it is not or there is none
#   scripts/preflight_reuse.sh --report [hours] # the reuse ledger, real files
#   scripts/preflight_reuse.sh --help
#
# As a library (what a lane does):
#   . scripts/preflight_reuse.sh
#   preflight_reuse_decision   # sets PREFLIGHT_TREE, PREFLIGHT_RECORD,
#                              # PREFLIGHT_SKIP, PREFLIGHT_RERUN,
#                              # PREFLIGHT_DECISION, PREFLIGHT_EVIDENCE
#
# EVERY reuse is appended to the ledger ($PREFLIGHT_RESULT_DIR/reuse-ledger.jsonl)
# with the host-seconds it avoided, so the saving is a measurement over real
# files rather than an estimate.
#
# EXIT CODES
#   0  reusable (the decision is in the printed variables / stdout). `--show`
#      uses the same word for the same thing: the record it printed is reusable
#      for this tree under the CURRENT inventory and environment.
#   1  not reusable — no record, wrong tree, stale, or the environment changed.
#      `--show` exits 1 whenever it has nothing reusable to show: no record for
#      the tree, a tree that could not be identified, or a record that is not
#      reusable. The status is the machine-readable half of the answer, so it
#      REPEATS the verdict the tool printed instead of reporting success on a
#      message that says the record is missing. A PARTIAL record is `reusable:
#      NO` here and exits 1, while the DECIDE path calls it `partial` and exits
#      0: `--show` answers "is this record reusable", the decide path answers
#      "what may I skip". Both directions fail SAFE — rc=1 means "gate
#      normally", never "you may skip a gate you did not earn".
#   2  usage error — an unknown or misplaced argument, and EVERY unusable
#      EXPLICIT tree: `--tree` with no value, `--tree <value>` that is not a
#      40-character lowercase hex tree sha, and `--tree` in a mode that reads no
#      tree at all (`--report`, `--source`). An explicit tree is never traded
#      for the working tree: an unusable one is refused and the refusal NAMES
#      the value, so a lane that mistyped a sha learns it instead of reading a
#      verdict about its own checkout.

EXIT_UNUSED=""
# Applied only when EXECUTED: sourcing must not change the caller's shell options
# (preflight.sh sets these itself, but a lane sourcing this library should not
# inherit `-u`/`pipefail` from it silently).
if [ "${BASH_SOURCE[0]}" = "$0" ]; then set -uo pipefail; fi

PR_TREE=""
PR_TREE_MISSING=0     # 1 when `--tree` was written with NO value at all
PR_MODE="decide"      # decide | show | report | source
PR_SHOW_TREE=""
PR_REPORT_HOURS="24"

pr_usage() { sed -n '2,/^EXIT_UNUSED=/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }

pr_args() {
  local a
  while [ $# -gt 0 ]; do
    case "$1" in
      -h|--help)    pr_usage; exit 0 ;;
      --show)       PR_MODE="show" ;;
      --report)     PR_MODE="report"
                    case "${2:-}" in [0-9]*) PR_REPORT_HOURS="$2"; shift ;; esac ;;
      --decide)     PR_MODE="decide" ;;
      --source)     PR_MODE="source" ;;
      --tree)       PR_TREE=""; PR_TREE_MISSING=0
                    if [ $# -ge 2 ]; then PR_TREE="$2"; else PR_TREE_MISSING=1; fi
                    shift ;;
      --tree=*)     PR_TREE="${1#*=}" ;;
      -*)           echo "unknown argument: $1 (see --help)" >&2; exit 2 ;;
      *)            echo "unexpected argument: $1 (see --help)" >&2; exit 2 ;;
    esac
    shift
  done
}

# ---- the explicit tree is validated before ANY mode answers -------------------
# `--tree <sha>` is the flag a lane reaches for when the bytes it cares about are
# NOT in its working tree (the tree it just pushed, a merge-tree result, the bytes
# a bite mutation is about), so the answer must belong to the tree it named. Two
# shapes used to be answered with a decision about the WORKING tree instead — the
# one question this tool exists to answer correctly:
#
#   * `--tree` with NO value fell through to the library fallback silently
#     (measured: rc=1 and "no reusable record for tree <this checkout>");
#   * `--tree <anything>` was accepted as a tree name, and the value is ALSO the
#     record's FILENAME (`gr_file` builds `<store>/<value>.json`), so a value that
#     is not a sha was answered as "no record for tree <that value>" — a
#     decision-shaped answer about something that is not a tree — and a value
#     carrying `/` or `.` built a path OUTSIDE the store (both fail SAFE for the
#     reuse decision, because `gr_record_usable` refuses a record whose own `tree`
#     field is not the requested string, but "the consumer and the runner
#     answering different questions" is the defect class this file's neighbours
#     are built around: `preflight.sh`'s drift check 21, docs/PREFLIGHT-REUSE.md
#     rule 6).
#
# So an unusable EXPLICIT tree is a USAGE error (exit 2), naming the value, before
# any mode runs. EVERY such shape is refused by THIS function, behind one call
# site, so the pre-fix behaviour is exactly "that call site is gone" — no part of
# the policy is left in the argument parser, where a bite could not delete it
# alone. The rule is the record format's own rule: a record file is named by
# `git write-tree`'s output, which is exactly 40 lowercase hex characters, so
# anything else can never name a record — it can only be a mistake, and a mistake
# must not be answered as a verdict. The one shape deliberately NOT an error is an
# EMPTY value (`--tree ""`, `--tree=`): that is the documented "this shell did not
# ask for a tree" spelling (`--tree "$MAYBE_EMPTY"` with MAYBE_EMPTY unset), and
# the library fallback still applies — pinned by `selftest` check 41's
# `--show --tree ""` leg. `--tree` with NO value at all is a DIFFERENT shape
# (`PR_TREE_MISSING`) and is refused: nobody writes the flag to mean nothing, and
# it used to fall back to the working tree silently. `--report`/`--source` read no
# tree at all, so there the flag is refused rather than silently ignored (a report
# that was asked about a tree and answered about the ledger is the same shape one
# mode over).
pr_tree_arg_guard() {
  if [ "${PR_TREE_MISSING:-0}" = 1 ]; then
    echo "preflight_reuse: --tree needs a tree sha (see --help)" >&2
    return 2
  fi
  local v="${PR_TREE:-}"
  [ -n "$v" ] || return 0
  case "$PR_MODE" in
    report|source)
      echo "preflight_reuse: --tree $v is meaningless with --$PR_MODE, which reads no tree (the report sums the reuse ledger) — refusing rather than ignoring the flag" >&2
      return 2 ;;
  esac
  case "$v" in
    *[!0-9a-f]*)
      echo "preflight_reuse: --tree $v is not a tree sha (40 lowercase hex characters) — refusing, rather than answering about the working tree" >&2
      return 2 ;;
  esac
  if [ "${#v}" -ne 40 ]; then
    echo "preflight_reuse: --tree $v is ${#v} characters; a tree sha is 40 lowercase hex characters (the record file is named by \`git write-tree\`) — refusing, rather than answering about the working tree" >&2
    return 2
  fi
  return 0
}

# ---- library mode: no argument handling, no output, no side effects -----------
# Sourced by a lane, which then calls preflight_reuse_decision itself.
pr_lib_init() {
  PREFLIGHT_RESULT_LIB="${PREFLIGHT_RESULT_LIB:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gate_result.sh}"
  # shellcheck source=scripts/gate_result.sh
  . "$PREFLIGHT_RESULT_LIB" || { echo "preflight_reuse: cannot source $PREFLIGHT_RESULT_LIB" >&2; return 1; }
  PREFLIGHT_GATE_NAMES=()
  local g
  while IFS= read -r g; do
    [ -n "$g" ] && PREFLIGHT_GATE_NAMES+=("$g")
  done < <("$PREFLIGHT_REUSE_REPO/scripts/preflight.sh" --list 2>/dev/null)
  [ "${#PREFLIGHT_GATE_NAMES[@]}" -gt 0 ] || return 1
  return 0
}

# preflight_reuse_decision — the whole decision, as variables.
#   PREFLIGHT_TREE      the tree sha the decision is about
#   PREFLIGHT_RECORD    the record file consulted ("" when there is none)
#   PREFLIGHT_DECISION  full | partial | none
#   PREFLIGHT_SKIP      comma-separated gates remembered as pass (may be "")
#   PREFLIGHT_RERUN     comma-separated gates that must run
#   PREFLIGHT_REMEMBERED_SECONDS  the host-seconds the skip avoids
#   PREFLIGHT_EVIDENCE  one line a human reads in the lane's handoff
# Returns 0 when there is a usable record (full or partial), 1 otherwise.
preflight_reuse_decision() {
  local repo="${PREFLIGHT_REUSE_REPO:-$PWD}"
  PREFLIGHT_REUSE_REPO="$repo"
  PREFLIGHT_DECISION="none"
  PREFLIGHT_SKIP=""
  PREFLIGHT_RERUN=""
  PREFLIGHT_RECORD=""
  PREFLIGHT_REMEMBERED_SECONDS=0
  PREFLIGHT_EVIDENCE=""

  # WHICH GATES THE CALLER IS ABOUT TO ASK FOR. The default is the whole
  # inventory, which is what a lane running a full gate wants. A caller that
  # intends `preflight.sh --only <set>` must pass the same set here, or the
  # consumer and the runner are answering different questions: the consumer would
  # report "partial" (the rest of the inventory is unobserved) for a run the
  # runner would call green. Same shape, one source of truth.
  local gates=()
  if [ -n "${PREFLIGHT_ONLY:-}" ]; then
    local g
    for g in ${PREFLIGHT_ONLY//,/ }; do
      case " ${PREFLIGHT_GATE_NAMES[*]} " in
        *" $g "*) gates+=("$g") ;;
        *) echo "preflight_reuse: unknown gate in PREFLIGHT_ONLY: $g" >&2; return 2 ;;
      esac
    done
  else
    gates=("${PREFLIGHT_GATE_NAMES[@]}")
  fi

  local tree="${PREFLIGHT_TREE:-}"
  if [ -z "$tree" ]; then
    tree="$(gr_tree_sha "$repo")" || {
      PREFLIGHT_EVIDENCE="preflight-reuse: this tree could not be identified (no working tree sha) — gating"
      return 1
    }
  fi
  PREFLIGHT_TREE="$tree"
  local file; file="$(gr_file "$tree")"
  PREFLIGHT_RECORD="$file"

  local reason
  if reason="$(gr_reusable "$tree" "${gates[@]}" 2>&1)"; then
    PREFLIGHT_DECISION="full"
    PREFLIGHT_SKIP="$(printf '%s,' "${gates[@]}")"; PREFLIGHT_SKIP="${PREFLIGHT_SKIP%,}"
    PREFLIGHT_REMEMBERED_SECONDS="$(gr_gates_reused_seconds "$file" "${gates[@]}")"
    PREFLIGHT_EVIDENCE="REUSED RECORD (full green, not re-run): tree ${tree:0:7} key $(gr_read_field "$file" key | cut -c1-12) finished $(gr_read_field "$file" finished_at) — remembered ${PREFLIGHT_REMEMBERED_SECONDS}s from $(gr_read_field "$file" log)"
    local led=0
    gr_ledger "$tree" full "$PREFLIGHT_REMEMBERED_SECONDS" "$PREFLIGHT_SKIP" || led=$?
    [ "$led" -eq 0 ] || echo "preflight_reuse: WARNING — could not append to the reuse ledger (rc=$led); the measured saving under-counts." >&2
    return 0
  fi

  # Not a full green. A PARTIAL record is still useful: reuse the passes, re-run
  # the rest. It is only usable when the record describes THIS tree, under THIS
  # environment fingerprint, inside the TTL — otherwise a stale pass could be
  # handed to a tree it never described. gr_record_usable enforces exactly that
  # (and prints why), and the verdict is recomputed here rather than parsed out of
  # an error message: a message that changes wording must not be able to silently
  # widen what may be skipped.
  if gr_record_usable "$tree" 2>/dev/null; then
    local g passes=() reruns=()
    for g in "${gates[@]}"; do
      if [ "$(gr_gate_status "$file" "$g")" = "pass" ]; then passes+=("$g"); else reruns+=("$g"); fi
    done
    if [ "${#passes[@]}" -gt 0 ]; then
      PREFLIGHT_DECISION="partial"
      PREFLIGHT_SKIP="$(printf '%s,' "${passes[@]}")"; PREFLIGHT_SKIP="${PREFLIGHT_SKIP%,}"
      PREFLIGHT_RERUN="$(printf '%s,' "${reruns[@]}")"; PREFLIGHT_RERUN="${PREFLIGHT_RERUN%,}"
      PREFLIGHT_REMEMBERED_SECONDS="$(gr_gates_reused_seconds "$file" "${passes[@]}")"
      PREFLIGHT_EVIDENCE="REUSED RECORD (partial: ${#passes[@]} remembered pass, ${#reruns[@]} re-run): tree ${tree:0:7} finished $(gr_read_field "$file" finished_at) — remembered ${PREFLIGHT_REMEMBERED_SECONDS}s [$reason]"
      local led=0
      gr_ledger "$tree" partial "$PREFLIGHT_REMEMBERED_SECONDS" "$PREFLIGHT_SKIP" || led=$?
      [ "$led" -eq 0 ] || echo "preflight_reuse: WARNING — could not append to the reuse ledger (rc=$led); the measured saving under-counts." >&2
      return 0
    fi
  fi

  PREFLIGHT_EVIDENCE="no reusable record for tree ${tree:0:7} — gating: $reason"
  return 1
}

# ---- report: the measured saving, off the real ledger ------------------------
pr_report() {
  local dir ledger hours cutoff now
  dir="$(gr_dir)"; ledger="$dir/reuse-ledger.jsonl"; hours="${1:-24}"
  now="$(date +%s)"; cutoff=$((now - hours*3600))
  echo "preflight reuse ledger — $ledger"
  echo "window: last ${hours}h"
  if [ ! -r "$ledger" ]; then
    echo "  (no ledger file yet — no lane has reused a record)"
    return 0
  fi
  # Counted in awk with a real ISO8601->epoch conversion done by date(1) per
  # line: the line count is small (one per reuse) and correctness beats speed.
  local total=0 infull=0 inpart=0 secs=0 lines=0 at ts
  while IFS= read -r line; do
    [ -n "$line" ] || continue
    lines=$((lines+1))
    at="$(printf '%s' "$line" | sed -nE 's/.*"at":"([^"]*)".*/\1/p')"
    ts="$(date -d "$at" +%s 2>/dev/null)"
    [ -n "$ts" ] || continue
    [ "$ts" -ge "$cutoff" ] || continue
    total=$((total+1))
    case "$line" in *'"mode":"full"'*) infull=$((infull+1)) ;; *'"mode":"partial"'*) inpart=$((inpart+1)) ;; esac
    local s; s="$(printf '%s' "$line" | sed -nE 's/.*"remembered_seconds":([0-9]+).*/\1/p')"
    secs=$((secs + ${s:-0}))
  done < "$ledger"
  echo "  reuse events in window : $total  (full-green $infull, partial $inpart)"
  echo "  host-seconds NOT spent : ${secs}s  ($(awk -v s="$secs" 'BEGIN{printf "%.2f", s/3600}') host-hours)"
  echo "  ledger lines total     : $lines"
  [ "$total" -gt 0 ] || echo "  NOTE: no reuse in this window yet — the saving is 0, not an estimate."
}

# ---- show: the raw evidence a human reads ------------------------------------
# EXIT STATUS IS PART OF THE ANSWER (card t_c0022f86): 0 when the record it
# printed is reusable for this tree under the CURRENT inventory and environment,
# 1 otherwise — no record at all, a tree that could not be identified, or a
# record that exists but is not reusable. It REPEATS the `reusable:` line above,
# so the machine-readable half of the tool can never say "yes" while the printed
# half says the record is missing, stale, red, or partial.
pr_show() {
  local tree="${PR_SHOW_TREE:-$PR_TREE}"
  [ -n "$tree" ] || tree="$(gr_tree_sha "${PREFLIGHT_REUSE_REPO:-$PWD}")" || {
    echo "cannot identify the working tree" >&2; return 1; }
  local file; file="$(gr_file "$tree")"
  if [ ! -r "$file" ]; then
    echo "no record at $file"
    return 1
  fi
  echo "record: $file"
  cat "$file"
  echo "--- verdict against the CURRENT gate inventory and environment: $(gr_verdict "$file" "${PREFLIGHT_GATE_NAMES[@]}")"
  local reason
  if reason="$(gr_reusable "$tree" "${PREFLIGHT_GATE_NAMES[@]}" 2>&1)"; then
    echo "--- reusable: YES"
    return 0
  else
    echo "--- reusable: NO"
    echo "    $reason"
    return 1
  fi
}

# ---- main (only when EXECUTED, never when sourced) ---------------------------
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  pr_args "$@"
  # THE EXPLICIT TREE IS CHECKED BEFORE ANY MODE ANSWERS (card t_916280c4). One
  # line on purpose: it is the whole guard, so a bite can restore the pre-fix
  # shape by deleting exactly it and nothing else. See pr_tree_arg_guard for why
  # a value that cannot be a tree sha must be an error rather than a verdict
  # about the working tree.
  pr_tree_arg_guard || exit $?
  PREFLIGHT_REUSE_REPO="$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." rev-parse --show-toplevel 2>/dev/null)"
  if [ -z "${PREFLIGHT_REUSE_REPO:-}" ] || [ ! -d "$PREFLIGHT_REUSE_REPO" ]; then
    echo "preflight_reuse: must run from inside the git repository" >&2
    exit 2
  fi
  cd "$PREFLIGHT_REUSE_REPO" || exit 2
  if [ "$PR_MODE" = "source" ]; then
    # Sourced by a caller that wants the library; do nothing else.
    exit 0
  fi
  pr_lib_init || { echo "preflight_reuse: cannot read the gate inventory from scripts/preflight.sh --list" >&2; exit 2; }
  case "$PR_MODE" in
    report)
      pr_report "$PR_REPORT_HOURS"
      ;;
    show)
      # THE RECORD'S STATUS IS THE PROCESS'S STATUS (card t_c0022f86). `pr_show`
      # returns non-zero when it has nothing reusable to show — the "no record at
      # <file>" return, the "cannot identify the working tree" return, and the
      # "reusable: NO" return — and this branch used to CALL it and drop that
      # status: the sourced-only `if` at the bottom of this file is FALSE when
      # executed, and a false `if` is what the shell leaves as the script's
      # status, so `--show` exited 0 on the message "no record at <file>". A
      # consumer that reads the exit code (the machine-readable half) was told
      # "yes" when there was nothing there at all. Measured on these bytes before
      # the fix:
      #   out=$(bash scripts/preflight_reuse.sh --show --tree $FAB 2>&1); echo $?
      #   -> "no record at /var/lib/jomform/gate/5f704744….json", rc=0
      #      (and `pr_show` itself returned 1 when sourced — the intent was
      #      already in the function; it just never reached the caller).
      # The propagation is ONE line on purpose: it is the whole defect, so the
      # bite below can restore the pre-fix shape by deleting exactly it.
      pr_show || exit $?
      ;;
    decide)
      # THE EXPLICIT --tree REACHES THE DECISION, not just --show. `--tree <sha>`
      # is documented above as "decide for an explicit tree", and the library
      # answers about `PREFLIGHT_TREE` — so without this wiring the flag was
      # accepted, documented, and then silently dropped: the decision fell back to
      # the WORKING tree, and its answer (and the tree named in the exit-1
      # PREFLIGHT_EVIDENCE line) was about a tree the caller had not asked about.
      # Measured on a record that IS green under the current inventory and
      # environment: `--tree <sha>` -> "no reusable record for tree <worktree
      # short sha>", rc=1, while `--show --tree <sha>` on the same bytes said
      # `reusable: YES`. The defect failed SAFE (rc=1 means "gate normally"), but
      # it cost the lane a full gate run for bytes that were already vouched for.
      # An explicit --tree wins over an inherited PREFLIGHT_TREE; an empty one
      # means "this shell did not ask for a tree", so the library's own fallback
      # (the working tree) still applies.
      if [ -n "$PR_TREE" ]; then PREFLIGHT_TREE="$PR_TREE"; fi
      if preflight_reuse_decision; then
        echo "$PREFLIGHT_EVIDENCE"
        echo "PREFLIGHT_DECISION=$PREFLIGHT_DECISION"
        echo "PREFLIGHT_TREE=$PREFLIGHT_TREE"
        echo "PREFLIGHT_SKIP=$PREFLIGHT_SKIP"
        echo "PREFLIGHT_RERUN=$PREFLIGHT_RERUN"
        echo "PREFLIGHT_REMEMBERED_SECONDS=$PREFLIGHT_REMEMBERED_SECONDS"
        exit 0
      else
        echo "$PREFLIGHT_EVIDENCE" >&2
        exit 1
      fi
      ;;
  esac
fi

# When SOURCED, initialise the library and the gate inventory, so the caller can
# call preflight_reuse_decision immediately.
if [ "${BASH_SOURCE[0]}" != "$0" ]; then
  PREFLIGHT_REUSE_REPO="$(git -C "$(dirname "${BASH_SOURCE[0]}")/.." rev-parse --show-toplevel 2>/dev/null)"
  [ -n "${PREFLIGHT_REUSE_REPO:-}" ] || PREFLIGHT_REUSE_REPO="$PWD"
  pr_lib_init || true
fi
