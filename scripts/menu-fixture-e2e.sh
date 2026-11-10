#!/usr/bin/env bash
# menu-fixture-e2e.sh — render a NON-PROD menu checkout that really carries a
# radio group AND an image-choice group, and measure it in Chromium.
#
# WHY THIS EXISTS (card t_2244109a). No published form in production carries a
# `radio` or `image-choice` block, and manufacturing one on a live workspace is
# forbidden, so two QA cards each improvised a way to get a server-rendered
# option group and deleted it with the card. The reusable half is now committed:
#
#   api/internal/menufixture       seeds a scratch schema of the shared test
#                                  database and serves the REAL public handler
#                                  (real HTTP, real Postgres rows, no injection)
#   api/cmd/menu-fixture           that fixture as a command; prints its URL
#   web/e2e/menu-checkout-option-group.spec.ts
#                                  the browser half: control size, the gap to
#                                  each control's own label, full-width buyer
#                                  fields, no sideways scroll — at 1200 and 375
#
# This script is the one command that puts them together:
#
#   bash scripts/menu-fixture-e2e.sh            # start, measure, tear down
#   bash scripts/menu-fixture-e2e.sh --serve    # start and wait (Ctrl-C to stop)
#   bash scripts/menu-fixture-e2e.sh --keep-schema
#
# WHAT IT TOUCHES. One scratch schema of $JOMFORM_TEST_DATABASE_URL (named
# test_menufixture_fx_p<pid>, dropped on exit unless --keep-schema), one loopback
# listener, and Chromium. It never writes to production data and it never
# constructs a client pointed at production CHIP (the fixture's payment client is
# pinned to a dead loopback port; `bank_direct` needs no provider at all).
#
# PREREQUISITES, and they FAIL LOUDLY rather than skipping:
#   - JOMFORM_TEST_DATABASE_URL  (source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh})
#   - go on PATH                 (the fixture is a Go command)
#   - web/node_modules/.bin/playwright, which needs the browser too:
#       cd web && npm ci && npx playwright install chromium
#
# A GATE, as of card t_ef32ea3b: `menufixture` in scripts/preflight.sh, ordered
# right after `e2e` so it reuses the browser cache that gate just warmed. This
# header used to say "NOT A GATE" on cost grounds; that conclusion was about the
# price, and the price was MEASURED on 2026-11-09: 3 passed (3.3s), real 0m18.4s
# with the Go build warm, leaving no schema, listener or process behind. What
# changed the answer is the defect, not the cost: the spec SKIPS inside `e2e`
# (no MENU_FIXTURE_URL there), so until this was a gate the ONLY guard that can
# see a layout defect in a menu option group — card t_664549fc, a radio sized to
# the whole row by the buyer-field rule, label pushed off a 375px screen — ran
# when a lane remembered, which is never. The Go half still lives in the `test`
# gate (api/internal/menufixture/fixture_test.go: the page really carries both
# groups and the order route stores the answer); this script carries the half
# that needs a layout engine, and it is a gate now so that half is measured on
# every release.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

serve=0
keep_schema=0
for arg in "$@"; do
  case "$arg" in
    --serve) serve=1 ;;
    --keep-schema) keep_schema=1 ;;
    -h|--help) sed -n '2,/^set -uo pipefail/{/^set -uo pipefail/d;p}' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "menu-fixture-e2e: unknown argument: $arg (see --help)" >&2; exit 2 ;;
  esac
done

die() { echo "menu-fixture-e2e: $*" >&2; exit 1; }

# ── prerequisites ───────────────────────────────────────────────────────────
[ -n "${JOMFORM_TEST_DATABASE_URL:-}" ] || die \
  "JOMFORM_TEST_DATABASE_URL is unset — the fixture seeds a SCRATCH SCHEMA of the shared test database and cannot run without it. Run: source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}"
command -v go >/dev/null 2>&1 || die "go is not on PATH — the fixture is built from api/cmd/menu-fixture"
PLAYWRIGHT="$REPO_ROOT/web/node_modules/.bin/playwright"
[ -x "$PLAYWRIGHT" ] || die "$PLAYWRIGHT is missing — seed it with: cd web && npm ci && npx playwright install chromium"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/menu-fixture-e2e.XXXXXX")" || die "cannot create a work directory"
BIN="$WORK/menu-fixture"
INFO="$WORK/fixture.json"
LOG="$WORK/fixture.err"
pid=""
failing=0

cleanup() {
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    # Wait for the fixture to drop its own schema before the work dir goes away,
    # so a killed run does not leave a test schema behind on a shared host.
    for _ in $(seq 1 100); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
    kill -9 "$pid" 2>/dev/null || true
  fi
  if [ "$failing" = 1 ] && [ -s "$LOG" ]; then
    echo "--- menu-fixture stderr ---" >&2
    cat "$LOG" >&2
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

# ── build + start the fixture ───────────────────────────────────────────────
echo "menu-fixture-e2e: building the fixture from api/cmd/menu-fixture" >&2
( cd "$REPO_ROOT/api" && go build -o "$BIN" ./cmd/menu-fixture ) || die "go build ./cmd/menu-fixture failed"

fixture_args=(-quiet)
[ "$keep_schema" = 1 ] && fixture_args+=(-keep)
"$BIN" "${fixture_args[@]}" >"$INFO" 2>"$LOG" &
pid=$!

# The command prints ONE JSON line on stdout once it is serving; everything it
# says on the way there goes to stderr.
for _ in $(seq 1 600); do
  [ -s "$INFO" ] && break
  kill -0 "$pid" 2>/dev/null || die "the fixture exited before it served anything (see stderr above)"
  sleep 0.2
done
[ -s "$INFO" ] || die "the fixture did not print its URL within 120s"

URL="$("${PYTHON:-python3}" - "$INFO" <<'PY'
import json,sys
with open(sys.argv[1]) as fh:
    print(json.loads(fh.readline())["url"])
PY
)" || die "could not read the fixture's URL from $INFO"

echo "menu-fixture-e2e: fixture is serving $URL" >&2

if [ "$serve" = 1 ]; then
  cat >&2 <<EOF
menu-fixture-e2e: serving (Ctrl-C to stop).

  page:        $URL
  (the URL works in any browser on THIS host — the fixture pins the request Host
   to its workspace subdomain, so no /etc/hosts entry and no DNS is involved.)

  Open it, add a dish, click the basket bar, and measure the sheet:
    - every option control ~13x13, ~8px to its own label
    - buyer name/email full width and 44px tall
    - no sideways scroll at 375px
EOF
  wait "$pid"
  exit 0
fi

# ── measure it ──────────────────────────────────────────────────────────────
cd "$REPO_ROOT/web" || die "cannot enter $REPO_ROOT/web"
MENU_FIXTURE_URL="$URL" "$PLAYWRIGHT" test --config playwright.menufixture.config.ts
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "menu-fixture-e2e: PASS — the menu checkout renders both option groups with intrinsic controls" >&2
else
  failing=1
  echo "menu-fixture-e2e: FAIL (playwright rc=$rc)" >&2
fi
exit "$rc"
