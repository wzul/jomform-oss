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
#   scripts/preflight_reuse.sh --tree <sha>     # decide for an explicit tree
#   scripts/preflight_reuse.sh --show           # print the record (evidence)
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
#   0  reusable (the decision is in the printed variables / stdout)
#   1  not reusable — no record, wrong tree, stale, or the environment changed
#   2  usage error

EXIT_UNUSED=""
# Applied only when EXECUTED: sourcing must not change the caller's shell options
# (preflight.sh sets these itself, but a lane sourcing this library should not
# inherit `-u`/`pipefail` from it silently).
if [ "${BASH_SOURCE[0]}" = "$0" ]; then set -uo pipefail; fi

PR_TREE=""
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
      --tree)       PR_TREE="${2:-}"; shift ;;
      --tree=*)     PR_TREE="${1#*=}" ;;
      -*)           echo "unknown argument: $1 (see --help)" >&2; exit 2 ;;
      *)            echo "unexpected argument: $1 (see --help)" >&2; exit 2 ;;
    esac
    shift
  done
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
  else
    echo "--- reusable: NO"
    echo "    $reason"
  fi
}

# ---- main (only when EXECUTED, never when sourced) ---------------------------
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  pr_args "$@"
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
      pr_show
      ;;
    decide)
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
