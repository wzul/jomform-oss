#!/usr/bin/env bash
# Bite proof for web/src/lib/a11yScanProse.test.ts (card t_1c6aa36b).
#
# WHAT THIS PROVES. The card's defect is a hand-written route list in
# docs/accessibility.md that drifted from the spec the a11y gate runs. The claim
# of the guard is NOT only "the enumeration is gone" (a one-time edit does that)
# but "a hand-list coming back fails". That claim can only be shown by a
# comparison, so this harness mutates the document and requires:
#
#   * the PRE-FIX list restored verbatim  -> guard RED, naming the routes
#   * the document as it ships            -> guard GREEN
#   * the SHORTER old list (ten routes, the exact shape quoted in the card)
#                                         -> guard RED
#   * a spec file the document does not name -> GREEN, and one that is renamed
#     in the prose while the file is gone    -> RED, naming it
#
# The document is mutated in place, so the run is serialised per worktree on a
# flock and every restore is byte-checked with sha256 (the shape
# web/scripts/biteproof_docs_tool_claims.sh records: two runs reading each
# other's mutations print false verdicts).
#
# Run:  bash web/scripts/biteproof_a11y_scan_prose.sh
set -uo pipefail

BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve this script's directory" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                    # -> web/

LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"
if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so two runs in one worktree could" >&2
    echo "read each other's mutated document and print false verdicts." >&2
    exit 2
  }
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  # WHICH LOCK, AND WHY NOT THE /docs FAMILY'S (card t_76b3356d vs t_1c6aa36b).
  # The family key `jomform-biteproof-docs-bite` exists because those harnesses all
  # mutate files under src/app/docs and their guards walk that tree. This one mutates
  # docs/accessibility.md — a QA reference read by humans, outside that tree — so it
  # shares no input with them and takes its own lock. The reason it takes ANY lock is
  # narrower: two runs of THIS script in one worktree would read each other's mutated
  # document. Listed as a deliberate exclusion in BITE-HARNESSES.md so the split is
  # visible rather than implied by the absence of a harnessLock entry.
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-a11y-scan-prose"
  mkdir -p "$LOCK_DIR" || { echo "BITEPROOF REFUSED: cannot create $LOCK_DIR" >&2; exit 2; }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || { echo "BITEPROOF REFUSED: cannot open $LOCK_FILE" >&2; exit 2; }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   a peer run holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  rc=$?
  if [ "$rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer that mutates the same document" >&2
    echo "and would print false verdicts. Nothing was mutated. Re-run once the peer finishes," >&2
    echo "or raise the wait with BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  [ "$rc" = 0 ] || { echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${rc}" >&2; exit 2; }
  bash "$SELF" "$@"
  exit "$?"
fi

LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
[ -n "$LOCK_FILE" ] || {
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 but BITEPROOF_LOCK_FILE is empty." >&2; exit 2; }
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same document would give it false verdicts." >&2
  echo "Run the harness WITHOUT BITEPROOF_LOCKED set and let it take the lock itself." >&2
  exit 2
fi
# The lock is only observable as HELD if flock is inert here: a fresh file in the same
# directory must lock FINE, or "not held" above proves nothing.
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here — it would read REFUSED for any file." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

DOC="${BITEPROOF_DOC:-../docs/accessibility.md}"
GUARD=src/lib/a11yScanProse.test.ts
[ -f "$DOC" ] || { echo "missing $DOC — is this the right worktree?" >&2; exit 2; }
BAK=$(mktemp)
cp "$DOC" "$BAK"
DOC_SHA=$(sha256sum "$DOC" | cut -d' ' -f1)

fail=0
cleanup() {
  local rc=$?
  cp "$BAK" "$DOC" 2>/dev/null
  rm -f "$BAK"
  exit "$rc"
}
trap cleanup EXIT

run_guard() { # sets OUT and RC
  local rc=0
  OUT=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1) || rc=$?
  RC=$rc
}
# verdict_of <rc>: GREEN | RED | REFUSED. A column with no run behind it is not a
# column — the rule web/scripts/biteproof_docs_tool_claims.sh documents.
verdict_of() {
  local rc="$1" out="$OUT"
  V=''
  if ! grep -qE '^ℹ ' <<<"$out"; then V=REFUSED; return; fi
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then
    if grep -qE '^ℹ pass [1-9]' <<<"$out"; then V=RED; else V=REFUSED; fi
    return
  fi
  if grep -qE '^ℹ pass [1-9]' <<<"$out" && grep -qE '^✔ ' <<<"$out" && [ "$rc" = 0 ]; then
    V=GREEN; return
  fi
  V=REFUSED
}

expect() { # expect <label> <want> [needle]
  local label="$1" want="$2" needle="${3:-}"
  run_guard
  verdict_of "$RC"
  if [ "$V" = REFUSED ]; then
    echo "  *** ${label}: the guard DID NOT RUN — no verdict is available ***"
    grep -E '^ℹ ' <<<"$OUT" | head -4; fail=1; return
  fi
  if [ "$V" != "$want" ]; then
    echo "  *** ${label}: wanted ${want}, got ${V} ***"
    grep -E '^ℹ (pass|fail)' <<<"$OUT" | head -2; fail=1; return
  fi
  if [ -n "$needle" ] && ! grep -qF "$needle" <<<"$OUT"; then
    echo "  *** ${label}: ${V} but the failure does not name ${needle} ***"; fail=1; return
  fi
  echo "  ok   ${label}: ${V}${needle:+, naming $needle}"
}

restore_doc() {
  cp "$BAK" "$DOC"
  if [ "$(sha256sum "$DOC" | cut -d' ' -f1)" = "$DOC_SHA" ]; then
    echo "       restored byte-identical"
  else
    echo "  *** RESTORE MISMATCH $DOC ***"; fail=1
  fi
}

# Replace the whole scanned-pages bullet with $1, inserted where the current
# bullet starts. The bullet is the line beginning "- `/docs` and every guide".
swap_bullet() {
  awk -v repl="$1" '
    /^- `\/docs` and every guide/ { print repl; skip=1; next }
    skip && /^$/ { skip=0 }
    skip { next }
    { print }
  ' "$BAK" > "$DOC"
}
assert_applied() {
  if grep -qF "$1" "$DOC"; then echo "       mutation on disk: ${1:0:60}…"
  else echo "  *** MUTATION DID NOT APPLY ***"; fail=1; return 1; fi
}

echo
echo "== baseline (no mutation) =="
expect "baseline" GREEN

echo
echo "== CASE 1: the PRE-FIX enumeration, restored verbatim (the card's own text) =="
swap_bullet '- `/docs` and every page under `web/src/app/docs` (`/docs/builder`,
  `/docs/customize`, `/docs/embed`, `/docs/subscriptions`, `/docs/variations`,
  `/docs/spam-detection`, `/docs/ses-reputation`, `/docs/placeholders`,
  `/docs/mcp`, `/docs/feedback`, `/docs/chat`). The knowledge base is public.'
assert_applied '`/docs/ses-reputation`' && expect "pre-existing enumeration" RED "docs routes are spelled out"
restore_doc

echo
echo "== CASE 2: the SHORTER hand list (ten routes, as the card quotes it) =="
swap_bullet '- `/docs` and every page under `web/src/app/docs` (`/docs/builder`,
  `/docs/customize`, `/docs/embed`, `/docs/subscriptions`, `/docs/variations`,
  `/docs/spam-detection`, `/docs/ses-reputation`, `/docs/placeholders`,
  `/docs/mcp`).'
assert_applied '`/docs/mcp`).' && expect "ten-route list" RED "docs routes are spelled out"
restore_doc

echo
echo "== CASE 3: exactly MAX_EXAMPLE_ROUTES examples stay GREEN (the example form is allowed) =="
swap_bullet '- `/docs` and every guide under `web/src/app/docs` — for example `/docs`,
  `/docs/builder` and `/docs/customize`. `web/src/lib/docsA11y.test.ts` pins the
  full list against the pages on disk, so the list is not repeated here.'
assert_applied 'for example `/docs`,' && expect "three examples" GREEN
restore_doc

echo
echo "== CASE 3b: MAX_EXAMPLE_ROUTES examples sit exactly on the floor -> GREEN =="
swap_bullet '- `/docs` and every guide under `web/src/app/docs` — for example `/docs`,
  `/docs/builder`, `/docs/customize` and `/docs/embed`. `web/src/lib/docsA11y.test.ts`
  pins the full list against the pages on disk.'
assert_applied '`/docs/customize` and `/docs/embed`' && expect "four examples (the floor)" GREEN
restore_doc

echo
echo "== CASE 3c: one over the floor -> RED (the check is a real count) =="
swap_bullet '- `/docs` and every guide under `web/src/app/docs` — for example `/docs`,
  `/docs/builder`, `/docs/customize`, `/docs/embed` and `/docs/activity`.
  `web/src/lib/docsA11y.test.ts` pins the full list against the pages on disk.'
assert_applied '`/docs/embed` and `/docs/activity`' && expect "five examples" RED "docs routes are spelled out"
restore_doc

echo
echo "== CASE 4: a spec the document names is not there -> RED =="
sed 's|`web/e2e/builder-panel-choice-names.spec.ts`|`web/e2e/builder-panel-choice-NAMES.spec.ts`|' "$BAK" > "$DOC"
assert_applied 'builder-panel-choice-NAMES.spec.ts' && expect "renamed spec" RED "not there"
restore_doc

echo
echo "== NEGATIVE CONTROL: a bare basename (no directory) is checked the same way =="
sed 's|`web/e2e/builder-canvas-price-contrast.spec.ts`|`builder-canvas-price-contrast.spec.ts`|' "$BAK" > "$DOC"
assert_applied '`builder-canvas-price-contrast.spec.ts`' && expect "bare basename, file exists" GREEN
restore_doc

echo
echo "== NEGATIVE CONTROL: prose naming one route in a sentence stays GREEN =="
sed 's|written in prose at `/docs/api` was read|written in prose at `/docs/zz-not-a-route` was read|' "$BAK" > "$DOC"
assert_applied '/docs/zz-not-a-route' && expect "single route outside the list region" GREEN
restore_doc

echo
if [ "$fail" = "0" ]; then
  echo "BITEPROOF PASS — the pre-fix list and the ten-route list are RED, the shipped document is GREEN."
else
  echo "BITEPROOF FAIL (fail=$fail)"
fi
exit "$fail"
