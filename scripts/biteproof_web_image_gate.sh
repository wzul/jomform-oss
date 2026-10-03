#!/usr/bin/env bash
#
# LOCK ISOLATION (card t_fda5ba74). This script invokes `./scripts/preflight.sh` three
# times (case 0, then once per case in `case_check`) and used to inherit the host-wide
# production lock, `/var/lock/jomform-preflight-gate.lock`, with a default wait of
# `PREFLIGHT_LOCK_WAIT=1800`. Each invocation could therefore sit up to 30 minutes
# waiting for a queue it does not need, AND occupy that queue while doing so —
# delaying every card gate behind it. Measured by QA: a run sat ~9 minutes still
# inside case 0 with no case completed.
#
# The gate under test still writes a REAL record (it is the real runner); only the
# LOCK is private, so two concurrent runs of this harness cannot collide with each
# other either. The host-wide queue is for cards gating a tree, not for an
# instrument proving the gate still bites — the same reasoning, and the same two
# variables, as scripts/biteproof_api_test_gate_timeout.sh and
# scripts/biteproof_preflight_reuse.sh.
export PREFLIGHT_LOCK_FILE="${PREFLIGHT_LOCK_FILE:-/tmp/biteproof_web_image_gate.lock}"
export PREFLIGHT_LOCK_WAIT="${PREFLIGHT_LOCK_WAIT:-0}"
#
# scripts/biteproof_web_image_gate.sh — prove the `webimage` preflight gate
# BITES, and that it is not a tautology.
#
# The gate under test is the `webimage` gate of scripts/preflight.sh: a real
# `docker build -f web/Dockerfile` (cache-only) whose whole purpose is to close
# the hole where the preflight ran `npm run build` on the HOST and never built
# the shipping web IMAGE — so a tree could be green through every gate while
# `docker build -f web/Dockerfile .` died at `npm run build` inside the image.
# Both recorded occurrences of that class are in web/Dockerfile's header
# (MCP-PARITY.md absent from the build context; context `web/` instead of the
# repo root).
#
# WHY A SCRIPT AND NOT A PER-RUN GATE: each case mutates a tracked file, runs
# the real gate and restores it. That is minutes of docker on every merge (and
# a cold cache pays a full `npm ci` + `next build` inside the image), which is
# how a gate gets skipped — the same reasoning that keeps
# deploy/backup/jomform-backup_bite.sh on demand. The cheap half (the gate
# exists, it is a real build and not a `--check`, and it REFUSES rather than
# skips when the docker daemon is unreachable or web/Dockerfile is missing) IS a
# per-run gate: `selftest` checks 13 and 14 in scripts/preflight_test.sh.
#
# EVERY case asserts BOTH directions, because one alone proves nothing:
#   * the GATE must go RED on the mutated tree — otherwise the gate does not
#     cover the defect; AND
#   * the HOST suite (`npm run test:unit`, the part of `npm run build` that
#     reads repo files — what the preflight ran before this gate existed) must
#     stay GREEN — otherwise the mutation is not the class this gate was added
#     for, and the gate would only be a second copy of a check that already
#     fires. A case that is red on the host suite is testing a different bug
#     and does not license this gate.
#
# Mutations are applied in place, one at a time, and the mutated file is
# restored after every case and re-verified by sha256 — the same convention as
# web/scripts/biteproof_next_managed_files.sh. Do not run this while another
# lane is gating out of the same checkout: the tree is briefly dirty by design.
#
# Run:  ./scripts/biteproof_web_image_gate.sh
# Exit: 0 if every case BITES, 1 otherwise.
#
# COST: ~1-4 min per case (the first case with a cold cache is the slow one,
# ~4 min; a warm cache is seconds). Cases run sequentially on purpose —
# parallel docker builds share one cache and the timings stop being readable.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

GATE_LOG=/tmp/biteproof_webimage_gate.log
HOST_LOG=/tmp/biteproof_webimage_host.log

# --- the files this script mutates, saved byte-for-byte up front -------------
MUTABLES=(.dockerignore web/Dockerfile)
declare -A WANT
for f in "${MUTABLES[@]}"; do
  [ -f "$f" ] || { echo "FAIL: $f not found — cannot prove anything about the image context" >&2; exit 2; }
  cp "$f" "/tmp/biteproof_image_gate.$(basename "$f").orig"
  WANT["$f"]="$(sha256sum "$f" | awk '{print $1}')"
done

restore_all() {
  for f in "${MUTABLES[@]}"; do cp "/tmp/biteproof_image_gate.$(basename "$f").orig" "$f"; done
}
cleanup() { restore_all; rm -f /tmp/biteproof_image_gate.*.orig; }
trap cleanup EXIT

# A mutation that was never applied is a silent GREEN, so every case asserts its
# edit actually landed before it runs anything.
assert_mutated() {
  local f="$1" needle="$2"
  if grep -qF "$needle" "$f"; then return 0; fi
  bad "$f was not mutated (looked for: $needle) — the case would have reported a false green"
  return 1
}

echo "bite proof: the webimage gate (docker build of web/Dockerfile)"
echo "  repo: $(pwd)"
echo "  head: $(git rev-parse --short HEAD)"
for f in "${MUTABLES[@]}"; do echo "  mutable: $f (sha256 ${WANT[$f]})"; done
echo "---"

# case_check <name> — the gate must be RED, the host suite must stay GREEN, and
# every mutated file must come back byte-identical.
case_check() {
  local name="$1" gate_rc host_rc f
  ( ./scripts/preflight.sh --only webimage ) >"$GATE_LOG" 2>&1
  gate_rc=$?
  ( cd web && npm run test:unit ) >"$HOST_LOG" 2>&1
  host_rc=$?
  if [ "$gate_rc" -ne 0 ] && [ "$host_rc" -eq 0 ]; then
    ok "$name — gate rc=$gate_rc (RED, as required), host test:unit rc=0 (the host suite really does NOT see this: that is the hole the gate fills)"
  else
    bad "$name — gate rc=$gate_rc (must be non-zero), host test:unit rc=$host_rc (must be 0). A host-red case is a DIFFERENT bug and does not license this gate. Logs: $GATE_LOG $HOST_LOG"
    printf '        gate log tail:\n'
    tail -n 6 "$GATE_LOG" | sed 's/^/          /'
  fi
  restore_all
  for f in "${MUTABLES[@]}"; do
    local got; got="$(sha256sum "$f" | awk '{print $1}')"
    if [ "$got" != "${WANT[$f]}" ]; then
      bad "$f was NOT restored byte-identically after '$name' (sha256 $got != ${WANT[$f]}) — restore it from /tmp/biteproof_image_gate.$(basename "$f").orig BY HAND before doing anything else"
    fi
  done
}

# --- case 0: the healthy tree must be GREEN (a gate that always fails is as
# useless as one that never fails, and the card requires this direction too) --
( ./scripts/preflight.sh --only webimage ) >"$GATE_LOG" 2>&1
if [ $? -eq 0 ]; then
  ok "healthy tree at HEAD — gate rc=0 (not a tautology: it passes when nothing is broken)"
else
  bad "healthy tree at HEAD — gate exited non-zero, so a red from this gate proves nothing. Log: $GATE_LOG"
  tail -n 6 "$GATE_LOG" | sed 's/^/          /'
fi

# --- case 1: a `.dockerignore` rule that filters a COPY source out -----------
# THE SHARPEST case, and a real one. MCP-PARITY.md sits at the repo root and is
# copied into the image because langPlacement.test.ts reads it. Excluding it from
# the context is invisible to every host-based check: the file is right there on
# disk, `git check-ignore` knows nothing about .dockerignore (measured: rc=1 on
# the healthy tree and rc=1 with the rule in place), and buildContext.test.ts's
# COPY-set rule passes because the literal `COPY MCP-PARITY.md /repo/MCP-PARITY.md`
# line is still in web/Dockerfile. The image build then dies with
# `failed to compute cache key: "/MCP-PARITY.md": not found`.
# This is the MCP-PARITY.md defect one layer out from how it originally bit.
echo "== case 1: .dockerignore excludes MCP-PARITY.md (a COPY source filtered out of the context) =="
printf '\nMCP-PARITY.md\n' >> .dockerignore
assert_mutated .dockerignore 'MCP-PARITY.md' && \
  case_check "MCP-PARITY.md excluded by .dockerignore"

# --- case 2: a runtime-stage COPY --from=build pointing at a path the build
# stage never produced --------------------------------------------------------
# A different failure mode from case 1 and one no host check can see:
# buildContext.test.ts parses the COPY instructions of the stage that runs
# `npm run build` ONLY (it stops at that RUN line), so a broken runtime stage is
# entirely outside its reach, and `npm run build` on the host never reads the
# Dockerfile's second stage at all. The image fails at
# `failed to compute cache key: "/repo/web/.next/standalone": not found` — the
# exact shape of a Dockerfile edit that silently breaks the artifact.
echo "== case 2: runtime stage COPIES a path its build stage never produced =="
sed -i 's|^COPY --from=build /repo/web/.next/standalone ./$|COPY --from=build /repo/web/.next/standalone-TYPO ./|' web/Dockerfile
assert_mutated web/Dockerfile 'standalone-TYPO' && \
  case_check "runtime-stage COPY --from=build names a path the build stage never produced"

echo "---"
if [ "$FAIL" -eq 0 ]; then
  echo "biteproof: $PASS checks passed, 0 failed — the webimage gate BITES on every case, is green on a healthy tree, and the host suite never sees either mutation"
else
  echo "biteproof: $PASS passed, $FAIL failed — the webimage gate does NOT cover something it claims to"
fi
[ "$FAIL" -eq 0 ] || exit 1
