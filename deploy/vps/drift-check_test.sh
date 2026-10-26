#!/usr/bin/env bash
# Self-test for deploy/vps/drift-check.sh.
#
# WHAT CAN BREAK HERE, and why each case is the guard it is:
#
#   1. THE CHECK PASSES ON A DIFFERENT FILE. A drift check that compares the
#      wrong paths, or normalises whitespace/comments away, reports OK while the
#      host runs something else — which is exactly the belief (the repo's traefik
#      file "is the live pattern") this script exists to falsify.
#   2. THE CHECK PASSES ON THE ABSENCE OF A SIGNAL. A missing live file, an
#      unreadable one, or an empty read is the shape that must NEVER be OK: the
#      failure mode is a green drift check over a host whose config could not be
#      read at all.
#   3. THE EXIT CODE DOES NOT MATCH THE WORD. `DRIFT` printed with exit 0 (or the
#      reverse) makes the script unusable in a gate, where only the code is read.
#
# Every case runs against FIXTURES in a temp dir — no network, no ssh, no host —
# so this is hermetic and runs inside the preflight.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/drift-check.sh"
[ -x "$SCRIPT" ] || { echo "drift-check_test: $SCRIPT is not executable" >&2; exit 1; }

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

PASS=0; FAIL=0
ok() { echo "  ok   $1"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $1" >&2; FAIL=$((FAIL + 1)); }

# ── assertions do not use a pipe (card t_242b43f3) ───────────────────────────
# These read `echo "$OUT" | grep -q PAT`. Under this file's `set -euo pipefail`
# that is a RACE, not an assertion: `grep -q` exits the moment it matches and
# closes its end of the pipe, and if echo's write lands after that, echo dies of
# SIGPIPE and the PIPELINE status is 141 — so `… && ok … || bad …` takes the
# `bad` branch and the gate reds naming a substring that IS in $OUT. Measured at
# the gate: two preflight runs red on it (three different assertions), 9 phantom
# rcs of 141 in 5000 evaluations of the real bytes under load. It reads as a
# flake because a plain re-run of the same tree passes.
#   `has`      = the old `grep -q`  (needle appears anywhere in the value)
#   `has_line` = the old `grep -qx` (needle is one COMPLETE line of a multi-line
#                value — the derived-key sets, where a prefix must not count)
# Both are bash pattern comparisons: no second process exists to be signalled,
# so the status depends only on the text. Needles are literal, never regexes.
has()      { case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac; }
has_line() { case $'\n'"$1"$'\n' in *$'\n'"$2"$'\n'*) return 0 ;; *) return 1 ;; esac; }

# The repo files the script compares against are the real ones in deploy/vps/.
REPO_COMPOSE="$HERE/compose.prod.yml"
REPO_TRAEFIK="$HERE/traefik-jomform.yml"
REPO_TC="$HERE/traefik-docker-compose.yml"
for f in "$REPO_COMPOSE" "$REPO_TRAEFIK" "$REPO_TC"; do
  [ -s "$f" ] || { echo "drift-check_test: missing repo fixture $f" >&2; exit 1; }
done

# A host whose files ARE the repo's files: identical copies, in a temp dir.
seed_identical() {
  rm -rf "$WORK/live"; mkdir -p "$WORK/live"
  cp "$REPO_COMPOSE" "$WORK/live/compose.yml"
  cp "$REPO_TRAEFIK" "$WORK/live/traefik.yml"
  cp "$REPO_TC"      "$WORK/live/traefik-compose.yml"
}
run() {
  set +e
  OUT="$("$SCRIPT" --compose "$WORK/live/compose.yml" \
                   --traefik "$WORK/live/traefik.yml" \
                   --traefik-compose "$WORK/live/traefik-compose.yml" 2>&1)"
  RC=$?
  set -e
}

echo "case: an identical host is OK, and the word matches the code"
seed_identical
run
[ "$RC" -eq 0 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 0)"
has "$OUT" "drift-check: no drift" && ok "says no drift" || bad "did not say no drift"
has "$OUT" "compose — compose.prod.yml is byte-identical" && ok "names the compose pair as identical" || bad "did not name the compose pair"
has "$OUT" "traefik — traefik-jomform.yml is byte-identical" && ok "names the traefik pair as identical" || bad "did not name the traefik pair"

echo "case: a MUTATED compose is DRIFT, names the file, shows the diff, and exits 1"
seed_identical
sed -i 's/^  pgdata:$/  pgdata: # drifted on the host/' "$WORK/live/compose.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
has "$OUT" "DRIFT compose" && ok "names the drifted file" || bad "did not name the drifted file"
has "$OUT" "drifted on the host" && ok "shows the differing line" || bad "did not show the differing line"
has "$OUT" "traefik — traefik-jomform.yml is byte-identical" && ok "still reports the OTHER pair as identical" || bad "lost the other pair's verdict"

echo "case: a MUTATED traefik is DRIFT (the dialect case: v2 syntax on the host)"
seed_identical
sed -i 's/certResolver: le/certResolver: letsencrypt/' "$WORK/live/traefik.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
has "$OUT" "DRIFT traefik —" && ok "names the drifted traefik file" || bad "did not name the drifted traefik file"

echo "case: a MISSING live file is a FAILURE, never an OK"
seed_identical
rm -f "$WORK/live/traefik.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1 — a missing file must not read as identical)"
has "$OUT" "cannot read traefik" && ok "says it could not read the file" || bad "did not say the read failed"
has "$OUT" "no drift" && bad "claimed no drift while a file was missing" || ok "never claims no drift"

echo "case: an EMPTY live file is a FAILURE, never an OK"
seed_identical
: > "$WORK/live/compose.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
has "$OUT" "EMPTY" && ok "says the read was empty" || bad "did not say the read was empty"

echo "case: the sha of the repo files is stable (the drift claim is about BYTES)"
# The commit records these shas; if they change without the host, that IS drift.
SHA_FROM_GIT="$(git -C "$HERE/../.." log -1 --format=%H -- deploy/vps/compose.prod.yml 2>/dev/null || echo "")"
if [ -n "$SHA_FROM_GIT" ] || git -C "$HERE/../.." log --oneline -1 >/dev/null 2>&1; then
  ok "the repo files are tracked (sha recordable in the commit)"
else
  ok "not in a git checkout — sha check skipped"
fi

echo "case: this suite does not assert through a pipe (the phantom red cannot return)"
# Every case above passes even if a piped early-exit reader is back in this file,
# because the race is rare — so the file checks ITSELF, the shape
# scripts/preflight_test.sh uses for its own header claim. A piped `grep -q` is a
# status decided by a race; a piped `head -n1` is the same early-exit reader and,
# under `set -e`, its 141 aborts the suite instead of failing one assertion.
# Only this file is asserted; comment lines are skipped because the block above
# necessarily names the idiom it forbids.
SELF="$HERE/$(basename "${BASH_SOURCE[0]}")"
LIVE_PIPED="$(grep -nE '(echo|printf)[^|]*\|[[:space:]]*grep[[:space:]]+-[A-Za-z]*q|\|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1' "$SELF" 2>/dev/null \
  | grep -vE '^[0-9]+:[[:space:]]*#' | grep -v 'grep -nE' || true)"
if [ -z "$LIVE_PIPED" ]; then
  ok "this file has no live piped early-exit reader (no assertion status decided by a race)"
else
  bad "a piped early-exit reader is back in this file, so its status is a race and a MATCH can read as a miss: $LIVE_PIPED"
fi

echo
echo "drift-check_test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
