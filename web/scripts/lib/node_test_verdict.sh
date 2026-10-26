#!/usr/bin/env bash
# node_test_verdict.sh — the runner-evidence gate and the verdict rule shared by the
# bite harnesses in web/scripts that drive a `node --test` guard.
#
# WHY THIS FILE EXISTS (card t_b014807f). The harnesses read a guard's verdict like
# this:
#
#   out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$1" 2>&1) || true
#   if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
#
# GREEN is the ELSE branch of "the runner reported a failure" — so a capture carrying
# NO test-runner output at all is read as GREEN. Every way `node` can fail to run the
# file (a node that predates the flag, one that cannot strip types, a broken install, a
# bad flag) therefore did not refuse: it answered. Measured on this host with the SAME
# harness bytes and the SAME base, only PATH differing: a shell whose `node` is
# /usr/bin/node v18.19.1 (it rejects `--disable-warning=…` with a 64-byte "bad option"
# line and rc=9) printed `BITEPROOF FAIL (5 failing check(s), 4 passing)` where node
# v24.19.0 printed `BITEPROOF FAIL (1 failing check(s), 6 passing)` — four legs that
# must see RED read GREEN, and the harness printed a verdict it never measured.
#
# THE RULE, IN TWO PARTS, because either half alone leaves a way for the instrument to
# answer without measuring:
#
#   1. nt_require_node — a CAPABILITY check, not a presence check. `node --version`
#      succeeding proves nothing here: v18.19.1 answers it and then cannot run the
#      file. The probe runs the thing the harness actually depends on (type stripping
#      plus the spec reporter) over a throwaway `.ts` test and requires the positive
#      shape the reader below reads. Measured on this host: v18.19.1 rejects the flag;
#      v20.20.2 accepts it and cannot load the `.ts` file (one failing "test" and a TAP
#      `# pass 0`); v22.23.3 runs it but emits the TAP reporter (`# pass 1`), and
#      v24.19.0 emits the spec reporter (`ℹ pass 1`). All three of the first cases are
#      REFUSALS here: this family reads the spec reporter and the `✖` / `ℹ …` lines.
#      The message names `command -v node` and `node --version`, so a lane that gets a
#      refusal knows WHICH node produced it instead of re-running from another shell.
#
#   2. nt_capture + nt_verdict_file — the verdict is read off POSITIVE evidence, never
#      off the absence of a failure line. GREEN needs rc 0 with `ℹ pass [1-9]`, `ℹ fail 0`
#      and a `✔ ` line; RED needs `ℹ fail [1-9]` AND `ℹ pass [1-9]` (a guard that cannot
#      load reports `tests 1 / pass 0 / fail 1`, and reading that as RED would print a
#      bite for a case that wanted RED while the instrument was broken); anything else is
#      REFUSED, and the caller exits 2 with the reason — no BITEPROOF PASS/FAIL line.
#      This is the rule biteproof_docs_tool_claims.sh and
#      biteproof_docs_index_render_sites.sh already applied (card t_420ca40c); this file
#      is that rule, plus the capability gate, in one place so the rest of the family
#      cannot drift back to the absence form one script at a time.
#
# STALE CAPTURES. nt_capture TRUNCATES the capture file (and writes the rc beside it)
# BEFORE it starts the runner, so a stale capture from an earlier run can never back a
# verdict. A capture that is empty, or that holds only the runner's own error line,
# takes the same REFUSED path as a missing runner: "no runner output = no verdict".
#
# NOT A HARNESS. The basename deliberately does not contain "bite": the inventory guard
# (api/internal/biteharness, inside the `api test` gate) treats every basename containing
# "bite" as an on-demand instrument that must have a BITE-HARNESSES.md row, and this file
# is a library the harnesses source, not something a lane runs.

# nt_verdict_file <capture-file> <rc> -> echoes GREEN | RED | REFUSED
# Pure: reads only its two arguments, so it is safe in the command substitutions the
# three docs-index harnesses call their runner through.
nt_verdict_file() {
  local cap="$1" rc="$2"
  # No spec-reporter summary at all: the runner never ran the file (or ran something
  # else entirely). This is the case the absence-form reader called GREEN.
  grep -qE '^ℹ ' "$cap" 2>/dev/null || { printf 'REFUSED'; return; }
  if grep -qE '^ℹ fail [1-9]' "$cap"; then
    if grep -qE '^ℹ pass [1-9]' "$cap"; then printf 'RED'; else printf 'REFUSED'; fi
    return
  fi
  if grep -qE '^ℹ pass [1-9]' "$cap" && grep -qE '^ℹ fail 0' "$cap" &&
     grep -qE '^✔ ' "$cap" && [ "$rc" = 0 ]; then
    printf 'GREEN'
    return
  fi
  printf 'REFUSED'
}

# nt_runner_lines <capture-file> -> the reporter's summary lines, one line, for messages.
nt_runner_lines() {
  local cap="$1" spec tap
  spec=$(grep -E '^ℹ (tests|pass|fail|skipped|todo) ' "$cap" 2>/dev/null | tr '\n' ' ')
  [ -n "$spec" ] || spec=$(grep -E '^# (tests|pass|fail|skipped|todo) ' "$cap" 2>/dev/null | tr '\n' ' ')
  printf '%s' "${spec% }"
}

# nt_why_file <capture-file> <rc> -> one line naming why this capture is not a verdict.
nt_why_file() {
  local cap="$1" rc="$2" lines
  lines=$(nt_runner_lines "$cap")
  if grep -qE '^ℹ ' "$cap" 2>/dev/null; then
    if grep -qE '^ℹ fail [1-9]' "$cap" && ! grep -qE '^ℹ pass [1-9]' "$cap"; then
      printf 'every test failed and none passed (%s) — the guard could not load, so this is not it catching a mutation' "${lines}"
      return
    fi
    printf 'node exited %s with %s — no passing test to show the guard ran' "$rc" "${lines:-no summary line}"
    return
  fi
  if grep -qE '^# (tests|pass|fail) ' "$cap" 2>/dev/null; then
    printf 'the runner emitted the TAP reporter (%s), not the spec reporter this harness reads — this harness reads `ℹ …` and `✖` lines, so a TAP capture is not a verdict here' "${lines}"
    return
  fi
  printf 'the capture carries NO test-runner output at all (node exited %s) — the guard never ran, so there is nothing to read' "$rc"
}

# nt_capture <capture-file> <guard-file> -> echoes GREEN | RED | REFUSED.
# Writes the full runner output to <capture-file> and its exit status to
# <capture-file>.rc, so a caller in a SUBSHELL can still report the reason.
#
# WHY `env -u NODE_TEST_CONTEXT`. When a harness is started from inside another
# `node --test` run (which is how web/src/lib/harnessLock.test.ts drives the family, and
# how a lane running `npm run test:unit` sees it), node sets NODE_TEST_CONTEXT in the test
# file's environment. A child `node --test` that inherits it refuses to run: it prints
# "node:test run() is being called recursively within a test file. skipping running files."
# and exits 0 with NO test summary — i.e. exactly the capture this reader exists to refuse,
# produced by the environment rather than by the guard. Clearing it lets the nested runner
# actually run the file (measured on this host, node v24.19.0: kept → no summary, removed →
# `ℹ tests 1 / ℹ pass 1`). The gate probe above is cleared the same way for the same reason.
nt_capture() {
  local cap="$1" guard="$2" rc=0 out
  # Truncate BEFORE the run: a stale capture is the other way this reader could answer
  # without measuring, and the `>` happens before node is even started.
  : > "$cap" || { printf 'REFUSED'; return; }
  out=$(env -u NODE_TEST_CONTEXT node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$guard" 2>&1) || rc=$?
  printf '%s\n' "$out" > "$cap"
  printf '%s\n' "$rc" > "$cap.rc"
  nt_verdict_file "$cap" "$rc"
}

# nt_refuse_print <label> <capture-file> <rc> -> prints the refusal block and exits 2.
nt_refuse_print() {
  local label="$1" cap="$2" rc="$3" why node_path node_ver
  why=$(nt_why_file "$cap" "$rc")
  node_path=$(command -v node 2>/dev/null) || node_path='<not on PATH>'
  node_ver=$(node --version 2>&1 | head -1) || node_ver='<cannot run>'
  {
    echo
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT) — ${label}"
    echo "  why:     ${why}."
    echo "  node:    ${node_path} (${node_ver})"
    local first
    first=$(grep -vE '^[[:space:]]*$' "$cap" 2>/dev/null | head -3 | sed 's/^/           /')
    [ -z "$first" ] || { echo "  capture: (first lines)"; printf '%s\n' "$first"; }
  } >&2
  exit 2
}

# nt_refuse_if <label> <capture-file> — exits 2 when the last capture on disk is not a
# verdict; returns 0 silently when it is. Reads <capture-file>.rc, so it works from a
# caller that ran nt_capture inside a command substitution.
nt_refuse_if() {
  local label="$1" cap="$2" rc verdict
  [ -f "$cap" ] || {
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): ${label}: the capture file ${cap} does not" >&2
    echo "exist, so there is nothing to read a verdict off." >&2
    exit 2
  }
  rc=$(cat "$cap.rc" 2>/dev/null) || rc='?'
  verdict=$(nt_verdict_file "$cap" "$rc")
  [ "$verdict" = REFUSED ] || return 0
  nt_refuse_print "$label" "$cap" "$rc"
}

# nt_require_node [tap] — the CAPABILITY gate. Returns 0 when this host's `node` can run a
# `.ts` test through the exact invocation the harness uses AND that run produced a test
# summary; otherwise prints why and exits 2. Call it once, before any mutation.
#
# WHAT THIS GATE DOES AND DOES NOT DECIDE. It decides "node can run the guard": type
# stripping, the `--disable-warning` flag, and a real test summary. It deliberately does
# NOT decide the REPORTER — a node whose default reporter is TAP (v22.23.3 on this host)
# passes here, because it really did run the file, and the verdict rule then REFUSES its
# capture with "the runner emitted the TAP reporter, not the spec reporter this harness
# reads". Two causes, two named refusals, instead of one gate guessing at the other.
# The optional `tap` argument mirrors the one caller that asks for `--test-reporter=tap`.
nt_require_node() {
  local reporter="${1:-spec}" probe rc=0 out node_path node_ver
  local -a extra=()
  [ "$reporter" = tap ] && extra=(--test-reporter=tap)
  if ! command -v node >/dev/null 2>&1; then
    echo "BITEPROOF REFUSED: node is not on PATH, so no guard can be driven here." >&2
    exit 2
  fi
  node_path=$(command -v node)
  node_ver=$(node --version 2>&1 | head -1)
  probe=$(mktemp "${TMPDIR:-/tmp}/node-test-capability.XXXXXX.ts") || {
    echo "BITEPROOF REFUSED: cannot create the capability probe file in ${TMPDIR:-/tmp}." >&2
    exit 2
  }
  cat >"$probe" <<'TS'
import { test } from "node:test";
import assert from "node:assert/strict";
test("the bite-harness node capability probe", () => {
  assert.equal(1, 1);
});
TS
  out=$(env -u NODE_TEST_CONTEXT node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON ${extra[@]+"${extra[@]}"} "$probe" 2>&1) || rc=$?
  rm -f "$probe"
  if [ "$rc" = 0 ] &&
     { { grep -qE '^ℹ pass 1' <<<"$out" && grep -qE '^ℹ fail 0' <<<"$out"; } ||
       { grep -qE '^# pass 1' <<<"$out" && grep -qE '^# fail 0' <<<"$out"; }; }; then
    return 0
  fi
  {
    echo
    echo "BITEPROOF REFUSED (exit 2, NO VERDICT): this host's node cannot run the guard"
    echo "this harness drives, so every leg below would answer without measuring."
    echo "  node:    ${node_path} (${node_ver})"
    echo "  probe:   node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON <trivial .ts>"
    echo "           exited ${rc}; a real run prints \`ℹ tests 1\`, \`ℹ pass 1\`, \`ℹ fail 0\` and a"
    echo "           \`✔ \` line. Got:"
    printf '%s\n' "$out" | grep -vE '^[[:space:]]*$' | head -4 | sed 's/^/             /'
    echo "  A node that only answers --version is not enough: v18.19.1 rejects"
    echo "  --disable-warning, v20 cannot load a .ts test, and a node whose default"
    echo "  reporter is TAP prints \`# pass\`, which this harness does not read. Point PATH"
    echo "  at a node whose --test runs TypeScript with the SPEC reporter (measured on this"
    echo "  host: v24.19.0 prints \`ℹ pass 1\`; v22.23.3 runs the file but prints \`# pass 1\`)"
    echo "  and re-run. Nothing below this line was mutated."
  } >&2
  exit 2
}
