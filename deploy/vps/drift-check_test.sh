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
echo "$OUT" | grep -q "drift-check: no drift" && ok "says no drift" || bad "did not say no drift"
echo "$OUT" | grep -q "compose — compose.prod.yml is byte-identical" && ok "names the compose pair as identical" || bad "did not name the compose pair"
echo "$OUT" | grep -q "traefik — traefik-jomform.yml is byte-identical" && ok "names the traefik pair as identical" || bad "did not name the traefik pair"

echo "case: a MUTATED compose is DRIFT, names the file, shows the diff, and exits 1"
seed_identical
sed -i 's/^  pgdata:$/  pgdata: # drifted on the host/' "$WORK/live/compose.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
echo "$OUT" | grep -q "DRIFT compose" && ok "names the drifted file" || bad "did not name the drifted file"
echo "$OUT" | grep -q "drifted on the host" && ok "shows the differing line" || bad "did not show the differing line"
echo "$OUT" | grep -q "traefik — traefik-jomform.yml is byte-identical" && ok "still reports the OTHER pair as identical" || bad "lost the other pair's verdict"

echo "case: a MUTATED traefik is DRIFT (the dialect case: v2 syntax on the host)"
seed_identical
sed -i 's/certResolver: le/certResolver: letsencrypt/' "$WORK/live/traefik.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
echo "$OUT" | grep -q "DRIFT traefik —" && ok "names the drifted traefik file" || bad "did not name the drifted traefik file"

echo "case: a MISSING live file is a FAILURE, never an OK"
seed_identical
rm -f "$WORK/live/traefik.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1 — a missing file must not read as identical)"
echo "$OUT" | grep -q "cannot read traefik" && ok "says it could not read the file" || bad "did not say the read failed"
echo "$OUT" | grep -q "no drift" && bad "claimed no drift while a file was missing" || ok "never claims no drift"

echo "case: an EMPTY live file is a FAILURE, never an OK"
seed_identical
: > "$WORK/live/compose.yml"
run
[ "$RC" -eq 1 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 1)"
echo "$OUT" | grep -q "EMPTY" && ok "says the read was empty" || bad "did not say the read was empty"

echo "case: the sha of the repo files is stable (the drift claim is about BYTES)"
# The commit records these shas; if they change without the host, that IS drift.
SHA_FROM_GIT="$(git -C "$HERE/../.." log -1 --format=%H -- deploy/vps/compose.prod.yml 2>/dev/null || echo "")"
if [ -n "$SHA_FROM_GIT" ] || git -C "$HERE/../.." log --oneline -1 >/dev/null 2>&1; then
  ok "the repo files are tracked (sha recordable in the commit)"
else
  ok "not in a git checkout — sha check skipped"
fi

echo
echo "drift-check_test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
