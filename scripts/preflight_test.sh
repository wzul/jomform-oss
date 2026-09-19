#!/usr/bin/env bash
#
# scripts/preflight_test.sh — self-test for scripts/preflight.sh.
#
# The preflight is now the ONLY gate between a broken or vulnerable tree and
# production (there is no CI to catch what it misses), so its own failure modes
# matter as much as the gates it runs. A gate runner that can be talked into a
# green result is worse than no gate, because it produces evidence that looks
# real. This proves the safety properties, and it is cheap: every check here
# either inspects the script's own selection logic or runs a gate that fails
# fast, so no full build/test is ever triggered.
#
# Run:  ./scripts/preflight_test.sh
# Exit: 0 if every property holds, 1 on the first violation.
#
# WHY A SEPARATE SCRIPT, NOT `go test`: the properties are about a shell
# script's argument handling and exit codes, not about Go packages, and the
# people who need to run this (a deployer mid-incident) have a shell, not
# necessarily a working Go toolchain pointed at this repo.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFLIGHT="$HERE/preflight.sh"
[ -x "$PREFLIGHT" ] || { echo "FAIL: $PREFLIGHT missing or not executable" >&2; exit 1; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

# Sandbox: a git repo containing only the script, so the script's own
# repo-root resolution works and no real build is ever invoked.
SB="$(mktemp -d /tmp/preflight_test.XXXXXX)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT
mkdir -p "$SB/scripts"
cp "$PREFLIGHT" "$SB/scripts/preflight.sh"
chmod +x "$SB/scripts/preflight.sh"
git -C "$SB" init -q .
git -C "$SB" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
cd "$SB" || exit 1

echo "preflight self-test (sandbox $SB)"
echo "--- safety properties ---"

# 1. Skipping EVERY gate must be refused, not reported green.
out=$(./scripts/preflight.sh --skip build,vet,test,govuln,web,a11y,npmaudit,gitleaks 2>&1)
rc=$?
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'every gate was skipped'; then
  ok "all gates skipped -> refused (rc=$rc)"
else
  bad "all gates skipped must be refused, got rc=$rc"
fi

# 2. An unset JOMFORM_TEST_DATABASE_URL must fail the api gates, not skip them.
#    (Silent skipping is how a green suite proves nothing.)
out=$(env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL \
      ./scripts/preflight.sh --only test 2>&1)
rc=$?
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'JOMFORM_TEST_DATABASE_URL is unset'; then
  ok "unset test DB -> refused (rc=$rc)"
else
  bad "unset test DB must be refused, got rc=$rc"
fi

# 3. An unset Valkey URL must fail too (both are prerequisites, not just one).
out=$(JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      env -u JOMFORM_TEST_VALKEY_URL ./scripts/preflight.sh --only test 2>&1)
rc=$?
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'JOMFORM_TEST_VALKEY_URL is unset'; then
  ok "unset test Valkey -> refused (rc=$rc)"
else
  bad "unset test Valkey must be refused, got rc=$rc"
fi

# 4. A missing web/node_modules must fail the web gate. A worktree without it
#    makes `npm run build` exit 0 having built nothing.
out=$(./scripts/preflight.sh --only web 2>&1)
rc=$?
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'web/node_modules/.bin/next is missing'; then
  ok "missing web/node_modules -> refused (rc=$rc)"
else
  bad "missing web/node_modules must be refused, got rc=$rc"
fi

# 5. An unknown gate name is rejected rather than quietly ignored (a typo in
#    --skip must not silently disable a real gate).
out=$(./scripts/preflight.sh --only nonsense 2>&1)
rc=$?
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'unknown gate'; then
  ok "unknown gate name -> refused (rc=$rc)"
else
  bad "unknown gate name must be refused, got rc=$rc"
fi
out=$(./scripts/preflight.sh --skip nonsense 2>&1)
if [ $? -ne 0 ] && printf '%s' "$out" | grep -q 'unknown gate'; then
  ok "--skip with a typo -> refused"
else
  bad "--skip with a typo must be refused"
fi

# 6. Every gate is still wired up: --list names all of them. If someone deletes
#    a gate from the array this drops, which is the silent-coverage-loss case.
expected="build vet test govuln web a11y npmaudit gitleaks"
got=$(./scripts/preflight.sh --list | tr '\n' ' ' | sed 's/ $//')
if [ "$got" = "$expected" ]; then
  ok "--list names all 8 gates"
else
  bad "--list drift: want [$expected] got [$got]"
fi

# 7. A gate that was skipped must never be counted as passed. Check the summary
#    line directly: with one gate selected, the other 7 must read as SKIP and
#    the run must say it is NOT full coverage.
out=$(env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL \
      ./scripts/preflight.sh --only build 2>&1)
if printf '%s' "$out" | grep -q 'NOT RUN:.*vet' && \
   printf '%s' "$out" | grep -qi 'not full coverage\|NOT a passing gate\|NOT full coverage'; then
  ok "skipped gates are labelled, and the run disclaims full coverage"
else
  bad "a skipped gate must be labelled and disclaimed"
fi

# 8. The script refuses to run outside a git repo (repo-root resolution is what
#    makes the relative gate commands correct).
OUTSIDE="$(mktemp -d /tmp/preflight_outside.XXXXXX)"
mkdir -p "$OUTSIDE/scripts"
cp "$PREFLIGHT" "$OUTSIDE/scripts/preflight.sh"
out=$(cd "$OUTSIDE" && ./scripts/preflight.sh --only build 2>&1)
rc=$?
rm -rf "$OUTSIDE"
if [ $rc -ne 0 ] && printf '%s' "$out" | grep -q 'inside the git repository'; then
  ok "run outside a git repo -> refused (rc=$rc)"
else
  bad "running outside a git repo must be refused, got rc=$rc"
fi

# 9. No gate may be neutered with `|| true` / `continue-on-error`. This is the
#    check the pen-test checklist points at; assert it here so it cannot rot.
#    Only ACTIVE lines count: the phrases legitimately appear inside comments
#    (documenting the rule), so strip comments and blank lines first.
active="$(sed -E 's/[[:space:]]*#.*$//' "$PREFLIGHT" | grep -vE '^[[:space:]]*$')"
if printf '%s' "$active" | grep -nE '\|\|[[:space:]]*true|continue-on-error' | grep -q .; then
  bad "preflight.sh contains '|| true' or 'continue-on-error' in an ACTIVE line — a gate can no longer stop the run:"
  printf '%s' "$active" | grep -nE '\|\|[[:space:]]*true|continue-on-error' | sed 's/^/        /'
else
  ok "no '|| true' / 'continue-on-error' in any active line of preflight.sh"
fi

# 10. The gitleaks CONFIG must actually contain rules. This is the check that
#     matters most: `--config <file>` REPLACES gitleaks' default ruleset, so a
#     config file with only an [allowlist] and no [[rules]] scans with an empty
#     ruleset — every secret is missed, the command exits 0, and the gate is a
#     permanent green no-op that looks exactly like a clean history. That is how
#     this repo's secrets gate behaved until 2026-09-19. Assert the config
#     either defines rules itself or extends the defaults.
CFG="$HERE/../.gitleaks.toml"
if [ ! -f "$CFG" ]; then
  bad ".gitleaks.toml not found at $CFG — the secrets gate has no config"
elif grep -qE '^\[\[rules\]\]' "$CFG" || grep -qE '^\s*useDefault\s*=\s*true' "$CFG"; then
  ok ".gitleaks.toml defines rules or [extend] useDefault (it is not an empty ruleset)"
else
  bad ".gitleaks.toml has NO [[rules]] and no [extend] useDefault — gitleaks would scan with an EMPTY ruleset and never report a leak (a green gate that cannot fail)"
fi

echo "---"
echo "preflight self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
