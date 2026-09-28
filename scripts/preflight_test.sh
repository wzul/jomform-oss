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
# CALLER: the `selftest` gate of scripts/preflight.sh (and `make preflight-selftest`).
# It MUST have one. Until 2026-09-21 it had none, and two checks rotted silently
# for exactly that reason: check 1 had stopped testing anything (its hardcoded
# skip-list predated the `tmpkey` gate, so `tmpkey` still ran, the run went
# green and the all-skipped branch was never reached) and the --list expectation
# was a hardcoded 9 names against 10 real gates. A regression net nobody runs is
# not a net.
#
# NO HARDCODED GATE INVENTORY: every gate name used below is derived from the
# script under test — `--list` for the inventory, the `run <name>` call sites for
# the gates that can actually execute. Adding gate #11 therefore cannot retire
# check 1 or trip a stale expectation, and check 2 fails if the two sources ever
# disagree (a gate listed but never run is a silent coverage loss: --only selects
# it, nothing executes, and the summary still reads green).
#
# THE SANDBOX USES A COPY OF preflight.sh, ON PURPOSE. The copy's repo root is
# the sandbox, which is what makes the properties testable without a real build:
# the script's own repo-root resolution runs for real, and each gate command that
# would be expensive finds its prerequisite missing and fails fast. Because a
# copy cannot prove anything about the LIVE file, check 1 re-runs the all-skipped
# property against the real scripts/preflight.sh from the real repo root — same
# derived skip-list, no hardcoding there either.
#
# WHY A SEPARATE SCRIPT, NOT `go test`: the properties are about a shell
# script's argument handling and exit codes, not about Go packages, and the
# people who need to run this (a deployer mid-incident) have a shell, not
# necessarily a working Go toolchain pointed at this repo.
#
# NO `printf … | grep -q` ANYWHERE IN THIS FILE (and no `… | head -n1` either).
# This script runs under `set -uo pipefail`, and that idiom is a NONDETERMINISTIC
# FAILURE: `grep -q` exits at the FIRST match, so the writer is still writing
# when the pipe closes, takes SIGPIPE (141), and `pipefail` makes the pipeline
# report 141 although grep MATCHED. The check then takes its `else`/`bad` branch
# and prints FAIL on output that plainly contains the pattern — a red `selftest`
# that does not mean the property was violated. Measured on this host at IDLE
# load, on the real payloads these checks grep: 1/500 runs at check 8, 11/500 at
# check 10. Feed grep a here-string (`grep -q PAT <<<"$out"`) — no writer process
# exists, so there is nothing to signal — or capture first and test the string.
# A red run must mean the property was violated, nothing else.
#
# THE SAME CLASS, READER-EXITS-EARLY: `… | grep -q .` (check 10's old shape) is
# the dangerous direction, because the failure lands on the `else` branch and
# reports ok — a green gate that cannot fail. Same remedy: capture, then test.
# `… | head -n1` is the same defect for an assignment (silently yields nothing).
#
# This is one shared rule, not per-site judgement: `deploy/tmp-apikey_test.sh`
# and `web/scripts/biteproof_next_managed_files.sh` carry the same note after
# being bitten by it, and this file was the only one still using the idiom.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFLIGHT="$HERE/preflight.sh"
LIVE_ROOT="$(cd "$HERE/.." && pwd)"
[ -x "$PREFLIGHT" ] || { echo "FAIL: $PREFLIGHT missing or not executable" >&2; exit 1; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

# Sandbox: a git repo containing only the script (plus the few repo files the
# script and this self-test read), so the script's own repo-root resolution works
# and no real build is ever invoked.
SB="$(mktemp -d /tmp/preflight_test.XXXXXX)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT
mkdir -p "$SB/scripts" "$SB/web/scripts"
cp "$HERE"/*.sh "$SB/scripts/" 2>/dev/null
# The web gate's dependency check (an .mjs, so `cp *.sh` above misses it) is a
# prerequisite of the gates this sandbox exercises. Without it in the sandbox the
# check would fail for "no such file" instead of for the reason under test, and a
# gate that fails for the wrong reason is not a gate.
cp "$HERE/../web/scripts/web-deps-check.mjs" "$SB/web/scripts/" 2>/dev/null
cp "$HERE/../web/package.json" "$SB/web/" 2>/dev/null
chmod +x "$SB/scripts/preflight.sh"
cp "$HERE/../AGENTS.md" "$SB/AGENTS.md" 2>/dev/null
# The gate-inventory table lives here (AGENTS.md is write-gated, so the DATA had
# to move out); check 12 reads it, so the sandbox needs a copy.
cp "$HERE/../GATE-INVENTORY.md" "$SB/GATE-INVENTORY.md" 2>/dev/null
cp "$HERE/../Makefile" "$SB/Makefile" 2>/dev/null
cp "$HERE/../.gitleaks.toml" "$SB/.gitleaks.toml" 2>/dev/null
git -C "$SB" init -q .
git -C "$SB" -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
cd "$SB" || exit 1

# ---------------------------------------------------------------------------
# PRIVATE LOCK FILE — the self-test must never queue behind a real gate.
#
# scripts/preflight.sh takes its concurrency lock BEFORE it validates arguments
# or checks prerequisites (deliberately: the banner prints once, in the locked
# child). So EVERY invocation below — including the ones whose whole point is to
# fail fast on an unset prerequisite — first waits up to PREFLIGHT_LOCK_WAIT
# (default 1800s) for the production lock. On a busy board that turned a
# sub-second self-test into a 10+ minute run that itself HELD the lock, delaying
# every card gate waiting behind it. Both shapes are avoided by pointing the
# self-test at its own lock file.
#
# This does not weaken anything under test: the self-test exercises argument
# handling, prerequisite failure and inventory drift — no property here is about
# concurrency — and none of its invocations runs a gate body (each either refuses
# outright or dies on a missing prerequisite), so it cannot contaminate another
# run's scratch schema. Check 14 proves the isolation holds.
# ---------------------------------------------------------------------------
export PREFLIGHT_LOCK_FILE="$SB/preflight-selftest.lock"

# ---------------------------------------------------------------------------
# Derive the gate inventories from the script under test. Nothing below names a
# gate or a gate count literally.
# ---------------------------------------------------------------------------

# The inventory the script advertises: what --only/--skip accept, what the
# header/summary print, and what check 1 must skip to reach the all-skipped
# branch. A failing or empty --list makes check 2 red (not this one).
listed="$(./scripts/preflight.sh --list 2>/dev/null)"
list_rc=$?
GATES="$(grep '[^[:space:]]' <<<"$listed")"
NGATES="$(grep -c '[^[:space:]]' <<<"$GATES")"

# The gates that can actually EXECUTE: the `run <name>` call sites. `run` is the
# only place the script invokes a gate, so this is the real surface, not a parse
# of its comments.
runnable="$(sed -E 's/[[:space:]]*#.*$//' "$PREFLIGHT" \
  | grep -oE '^[[:space:]]*run[[:space:]]+[A-Za-z0-9_]+' \
  | awk '{print $2}' | sort -u)"

echo "preflight self-test (sandbox $SB)"
# shellcheck disable=SC2086  # deliberate word splitting: these are newline-separated gate lists
echo "  gate inventory: $NGATES from --list: $(printf '%s ' $GATES)"
# shellcheck disable=SC2086
echo "  runnable gates: $(printf '%s ' $runnable)"
echo "--- safety properties ---"

# 1. Skipping EVERY gate must be refused, not reported green. The skip-list is
#    built from the inventory above, so a gate added later cannot slip past this
#    check by not being named here — which is exactly how this check stopped
#    testing anything when `tmpkey` was added. Asserted twice: on the sandbox
#    copy (isolated, cheap) and on the LIVE scripts/preflight.sh from the real
#    repo root, so a copy-versus-live divergence cannot hide.
all_skipped="${GATES//$'\n'/,}"
sb_out=$(./scripts/preflight.sh --skip "$all_skipped" 2>&1); sb_rc=$?
live_out=$(cd "$LIVE_ROOT" && ./scripts/preflight.sh --skip "$all_skipped" 2>&1); live_rc=$?
if [ $sb_rc -ne 0 ] && grep -q 'every gate was skipped' <<<"$sb_out" && \
   [ $live_rc -ne 0 ] && grep -q 'every gate was skipped' <<<"$live_out"; then
  ok "all $NGATES gates skipped -> refused: sandbox copy rc=$sb_rc, live script rc=$live_rc"
else
  bad "all gates skipped must be refused — sandbox copy rc=$sb_rc [$(printf '%s' "$sb_out" | tail -n 1)], live script rc=$live_rc [$(printf '%s' "$live_out" | tail -n 1)]"
fi

# 2. Every advertised gate must have a `run` call site, and every `run` call site
#    must be advertised. Either mismatch is a silent coverage loss. This is the
#    drift-proof replacement for the old hardcoded `--list` expectation: it
#    compares the script against itself, so it stays in step with GATE_NAMES
#    automatically, and a deleted gate still shows up here as a drop.
if [ $list_rc -ne 0 ] || [ -z "$GATES" ]; then
  bad "--list must exit 0 and name the gates (got rc=$list_rc, [$listed])"
elif [ "$(printf '%s\n' $GATES | sort -u)" = "$(printf '%s\n' $runnable | sort -u)" ]; then
  ok "--list and the \`run\` call sites agree on all $NGATES gates (no drift, no gate that cannot run)"
else
  bad "gate inventory drift — listed but never run: [$(comm -23 <(printf '%s\n' $GATES | sort -u) <(printf '%s\n' $runnable | sort -u) | tr '\n' ' ')] / run but not listed: [$(comm -13 <(printf '%s\n' $GATES | sort -u) <(printf '%s\n' $runnable | sort -u) | tr '\n' ' ')]"
fi

# 3. An unset JOMFORM_TEST_DATABASE_URL must fail the api gates, not skip them.
#    (Silent skipping is how a green suite proves nothing.)
out=$(env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL \
      ./scripts/preflight.sh --only test 2>&1)
rc=$?
if [ $rc -ne 0 ] && grep -q 'JOMFORM_TEST_DATABASE_URL is unset' <<<"$out"; then
  ok "unset test DB -> refused (rc=$rc)"
else
  bad "unset test DB must be refused, got rc=$rc"
fi

# 4. An unset Valkey URL must fail too (both are prerequisites, not just one).
out=$(JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      env -u JOMFORM_TEST_VALKEY_URL ./scripts/preflight.sh --only test 2>&1)
rc=$?
if [ $rc -ne 0 ] && grep -q 'JOMFORM_TEST_VALKEY_URL is unset' <<<"$out"; then
  ok "unset test Valkey -> refused (rc=$rc)"
else
  bad "unset test Valkey must be refused, got rc=$rc"
fi

# 5. A missing web/node_modules must fail the web gate. A worktree without it
#    makes `npm run build` exit 0 having built nothing.
out=$(./scripts/preflight.sh --only web 2>&1)
rc=$?
if [ $rc -ne 0 ] && grep -q 'web/node_modules/.bin/next is missing' <<<"$out"; then
  ok "missing web/node_modules -> refused (rc=$rc)"
else
  bad "missing web/node_modules must be refused, got rc=$rc"
fi

# 5b. A node_modules that EXISTS but is not INSTALLED must also fail the web gate.
#     This is the shape the primary checkout was found in (an empty directory)
#     and the shape an interrupted `npm ci` leaves (a partial tree). `.bin/next`
#     is planted here on purpose, because that is exactly what the check above
#     cannot see: with the tool present the gate used to proceed, and
#     `npm run test:unit` then under-reported (254 tests / 253 pass / 1 fail
#     instead of 260 / 260 / 0 at the same head) rather than failing.
#     The assertion is on the GATE's own refusal, not just on the message text:
#     the checker can also be reached from inside `npm run build`, so a check
#     that only grepped for "IS NOT INSTALLED IN THIS TREE" stayed GREEN with
#     the prerequisite removed (measured — the sabotage below is what found it).
#     Requiring the PREFLIGHT FAIL line AND the absence of the `gate [web]`
#     banner pins that the gate was stopped as a prerequisite, before it ran.
mkdir -p "$SB/web/node_modules/.bin"
printf '#!/bin/sh\nexit 1\n' > "$SB/web/node_modules/.bin/next"
chmod +x "$SB/web/node_modules/.bin/next"
out=$(./scripts/preflight.sh --only web 2>&1)
rc=$?
if [ $rc -ne 0 ] && \
   grep -q 'PREFLIGHT FAIL: the web dependencies for this tree are not installed' <<<"$out" && \
   grep -q 'npm ci' <<<"$out" && \
   ! grep -q 'gate \[web\]' <<<"$out"; then
  ok "an empty/uninstalled node_modules (with .bin/next present) -> refused by the PREREQUISITE, naming the fix (rc=$rc)"
else
  bad "an existing-but-uninstalled web/node_modules must be refused by the dependency PREREQUISITE (not by the gate running and failing), got rc=$rc [$(grep -iE 'IS NOT INSTALLED|PREFLIGHT FAIL|gate \[web\]' <<<"$out" | tr '\n' ' ')]"
fi
rm -rf "$SB/web/node_modules"

# 5c. The DEEPER depth of the same shape: a node_modules whose every declared
#     package DIRECTORY exists and holds only its package.json — an `npm ci`
#     killed after the directories are created. A presence-only check passed that
#     tree with "14 declared package(s) installed", and `npm run test:unit` then
#     printed 326 tests / 324 pass / 2 fail against 350 / 350 / 0 with a real
#     install, the failures being ERR_MODULE_NOT_FOUND for `react`. `.bin/next` is
#     planted here for the same reason as in 5b, and `next`'s own directory is one
#     of the hollow ones — so the executable check is not what refuses this.
#     The assertion that this is the PAYLOAD half firing is the `no payload:` line
#     together with the ABSENCE of a `missing:` line: if the declared list were not
#     fully on disk, presence would be what refused it and this check would be
#     pinning 5b again instead of the deeper shape.
mkdir -p "$SB/web/node_modules/.bin"
node -e '
  const { mkdirSync, writeFileSync, readFileSync } = require("node:fs");
  const { join } = require("node:path");
  const webDir = process.argv[1];
  const pkg = JSON.parse(readFileSync(join(webDir, "package.json"), "utf8"));
  const names = [...Object.keys(pkg.dependencies ?? {}), ...Object.keys(pkg.devDependencies ?? {})];
  for (const name of names) {
    const dir = join(webDir, "node_modules", name);
    mkdirSync(dir, { recursive: true });
    writeFileSync(join(dir, "package.json"), JSON.stringify({ name, version: "1.0.0", main: "index.js" }));
  }
' "$SB/web"
printf '#!/bin/sh\nexit 1\n' > "$SB/web/node_modules/.bin/next"
chmod +x "$SB/web/node_modules/.bin/next"
out=$(./scripts/preflight.sh --only web 2>&1)
rc=$?
if [ $rc -ne 0 ] && \
   grep -q 'PREFLIGHT FAIL: the web dependencies for this tree are not installed' <<<"$out" && \
   grep -q 'no payload:' <<<"$out" && \
   ! grep -q '^  missing:' <<<"$out" && \
   ! grep -q 'gate \[web\]' <<<"$out"; then
  ok "a HOLLOW node_modules (every package directory present, no payload) -> refused by the PREREQUISITE (rc=$rc)"
else
  bad "a hollow web/node_modules (directories present, no payload) must be refused by the dependency PREREQUISITE, got rc=$rc [$(grep -iE 'IS NOT INSTALLED|no payload:|missing:|PREFLIGHT FAIL|gate \[web\]' <<<"$out" | tr '\n' ' ')]"
fi
rm -rf "$SB/web/node_modules"

# 6. An unknown gate name is rejected rather than quietly ignored (a typo in
#    --only must not silently disable a real gate).
out=$(./scripts/preflight.sh --only nonsense 2>&1)
rc=$?
if [ $rc -ne 0 ] && grep -q 'unknown gate' <<<"$out"; then
  ok "unknown gate name -> refused (rc=$rc)"
else
  bad "unknown gate name must be refused, got rc=$rc"
fi

# 7. Same for --skip: a typo there is the more dangerous one, because it reads as
#    "that gate is off" while the gate actually runs, or vice versa.
out=$(./scripts/preflight.sh --skip nonsense 2>&1)
if [ $? -ne 0 ] && grep -q 'unknown gate' <<<"$out"; then
  ok "--skip with a typo -> refused"
else
  bad "--skip with a typo must be refused"
fi

# 8. A gate that was skipped must never be counted as passed. Check the summary
#    line directly: with one gate selected, another must read as SKIP and the run
#    must say it is NOT full coverage. The "another" gate comes from the derived
#    inventory, so this cannot go stale the way a literal name would.
other="$(printf '%s\n' "$GATES" | grep -vx build | sed -n '1p')"
if [ -z "$other" ]; then
  bad "cannot pick a gate other than 'build' from the inventory to check the SKIP label"
else
  out=$(env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL \
        ./scripts/preflight.sh --only build 2>&1)
  # The disclaimer lives in the WARNING block ("A skipped gate is NOT a passing
  # gate"), not in the closing summary line — an aborted run never reaches the
  # summary, and that is precisely the case this check runs.
  if grep -q "NOT RUN:.*$other" <<<"$out" && \
     grep -qiE 'not[[:space:]]+full[[:space:]]+coverage|not[[:space:]]+a[[:space:]]+passing[[:space:]]+gate' <<<"$out"; then
    ok "a skipped gate ($other) is labelled, and the run disclaims full coverage"
  else
    bad "a skipped gate must be labelled and disclaimed"
  fi
fi

# 9. The script refuses to run outside a git repo (repo-root resolution is what
#    makes the relative gate commands correct).
OUTSIDE="$(mktemp -d /tmp/preflight_outside.XXXXXX)"
mkdir -p "$OUTSIDE/scripts"
cp "$PREFLIGHT" "$OUTSIDE/scripts/preflight.sh"
out=$(cd "$OUTSIDE" && ./scripts/preflight.sh --only build 2>&1)
rc=$?
rm -rf "$OUTSIDE"
if [ $rc -ne 0 ] && grep -q 'inside the git repository' <<<"$out"; then
  ok "run outside a git repo -> refused (rc=$rc)"
else
  bad "running outside a git repo must be refused, got rc=$rc"
fi

# 10. No gate may be neutered with `|| true` / `continue-on-error`. This is the
#     check the pen-test checklist points at; assert it here so it cannot rot.
#     Only ACTIVE lines count: the phrases legitimately appear inside comments
#     (documenting the rule), so strip comments and blank lines first.
active="$(sed -E 's/[[:space:]]*#.*$//' "$PREFLIGHT" | grep -vE '^[[:space:]]*$')"
# CAPTURE, then test — not `grep -nE … | grep -q .`. A `grep -q` reader exits at
# the first line, so the writer grep can take SIGPIPE (141) and `pipefail` then
# fails the pipeline although it MATCHED; inside `if`, that lands on the `else`
# branch and reports ok — a FALSE GREEN on the one property that keeps a gate
# able to stop the run. (Measured with a multi-match payload; the shipped
# preflight.sh has a single match, so this window is narrow rather than absent.)
hits="$(grep -nE '\|\|[[:space:]]*true|continue-on-error' <<<"$active")"
if [ -n "$hits" ]; then
  bad "preflight.sh contains '|| true' or 'continue-on-error' in an ACTIVE line — a gate can no longer stop the run:"
  sed 's/^/        /' <<<"$hits"
else
  ok "no '|| true' / 'continue-on-error' in any active line of preflight.sh"
fi

# 11. The gitleaks CONFIG must actually contain rules. This is the check that
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

# 12. The test-env advice must name a DURABLE path — and the SAME path at every
#     durable site. Each prerequisite failure tells the operator where to get
#     the two env vars; until 2026-09-21 NOTHING asserted that string, so
#     reverting the advice to the old `/tmp/jf-env.sh` shipped the selftest gate
#     GREEN. Checks 3 and 4 above only assert the exit code and the variable
#     NAME, and `env -u` satisfies both whatever path the message advertises:
#     the advice text is printed, never checked. A /tmp advice path is the
#     original false reading — /tmp is wiped between sessions, so the next
#     reader sources nothing, the DB-backed tests SKIP, and `go test` prints a
#     green suite that proves nothing.
#     The canonical path is DERIVED from the script under test (the
#     `${JOMFORM_ENV:-<default>}` form), never hardcoded here, so this check
#     cannot go stale — and it covers every site that must agree: the script's
#     header comment, both die-messages, and the docs that repeat the advice.
# 12a — the script-side form: one default, overridable, not in /tmp.
advised_path() { sed -nE 's/.*Run: source (.*)$/\1/p' <<<"$1" | sed -n '1p'; }

die_db=$(grep -F 'JOMFORM_TEST_DATABASE_URL is unset' "$PREFLIGHT" | sed -n '1p')
die_vk=$(grep -F 'JOMFORM_TEST_VALKEY_URL is unset' "$PREFLIGHT" | sed -n '1p')
script_defaults=$(sed -nE 's/.*\$\{JOMFORM_ENV:-([^}]*)\}.*/\1/p' "$PREFLIGHT" | sort -u)
canon="$(printf '%s\n' "$script_defaults" | sed -n '1p')"
ndefaults=$(grep -c '[^[:space:]]' <<<"$script_defaults")
# The offending path, not the whole die-message: the verdict above is only
# readable if the FAIL line names the path that is wrong.
tmp_hit=""
case "/$canon" in /tmp/*) tmp_hit="$canon" ;; esac
if [ -z "$tmp_hit" ]; then
  tmp_hit="$(printf '%s\n%s\n' "$die_db" "$die_vk" \
    | sed -nE 's/.*Run: source (.*)"$/\1/p;s/.*Run: source (.*)$/\1/p' | grep '^/tmp/' | sort -u | tr '\n' ' ')"
  tmp_hit="${tmp_hit% }"
  [ -n "$tmp_hit" ] || tmp_hit=""
fi

if [ -z "$die_db" ] || [ -z "$die_vk" ]; then
  bad "the JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL die-messages are gone from preflight.sh — the advice an operator relies on is unverifiable"
elif [ -n "$tmp_hit" ]; then
  bad "the test-env advice points into /tmp (=[$tmp_hit]) — /tmp is wiped between sessions, so the next reader sources nothing, the DB-backed tests SKIP and the suite prints green. It must be a durable path"
elif ! grep -qF '${JOMFORM_ENV:-' <<<"$die_db$die_vk"; then
  bad "the env-prerequisite advice no longer names the \${JOMFORM_ENV:-...} form (it must stay overridable, not a hardcoded literal): DB=[$die_db] VALKEY=[$die_vk]"
elif [ "$ndefaults" -ne 1 ] || [ -z "$canon" ]; then
  bad "preflight.sh quotes $ndefaults different test-env defaults — the emitted advice cannot be one durable path: [$(printf '%s ' $script_defaults)]"
else
  ok "test-env advice is one overridable durable default (\${JOMFORM_ENV:-$canon}), named at every site in preflight.sh"
fi

# 12b — what the operator is ACTUALLY shown. Read it off a real run on both the
#       sandbox copy and the LIVE script (same shape as check 1: a
#       copy-versus-live divergence must not be able to hide), because the
#       emitted text is the surface the regression was invisible on.
sb_env_out=$(env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL -u JOMFORM_ENV \
             ./scripts/preflight.sh --only test 2>&1)
live_env_out=$(cd "$LIVE_ROOT" && env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL -u JOMFORM_ENV \
             ./scripts/preflight.sh --only test 2>&1)
sb_advice=$(advised_path "$sb_env_out")
live_advice=$(advised_path "$live_env_out")
# The die-message echoes the parameter expansion, so what the operator reads is
# the RESOLVED default — compare against that, not the literal ${...} text.
# With no default to compare against (already reported by 12a), fall back to the
# sandbox's own reading so this check still catches sandbox-vs-live drift
# instead of double-counting the same violation.
expected_advice="${canon//\$HOME/$HOME}"
[ -n "$expected_advice" ] || expected_advice="$sb_advice"
if [ -z "$sb_advice" ] || [ -z "$live_advice" ]; then
  bad "an unset prerequisite no longer prints a 'Run: source <path>' line — sandbox=[$sb_advice] live=[$live_advice]"
elif [ "$sb_advice" = "$expected_advice" ] && [ "$live_advice" = "$expected_advice" ]; then
  ok "the emitted prerequisite advice matches the script default (sandbox and live: $sb_advice)"
else
  bad "the path an operator is told to source drifted from the script default: sandbox=[$sb_advice] live=[$live_advice] expected=[$expected_advice]"
fi

# 12c — the docs must not drift from the same string. The site list is DERIVED
#       from the repo rather than hand-written here: a hardcoded list is the same
#       drift class this repo has been bitten by three times — the sites moved and
#       the list did not. A new doc that repeats the advice is covered the moment
#       it lands.
#       DISCOVERY IS BY THE ADVICE'S OWN NAMES, NOT BY THE ${JOMFORM_ENV:-...}
#       FORM: a site that drifts to a bare path (or loses the override) STOPS
#       matching the portable form, so discovering on that form silently drops the
#       drifted site from the sweep and the run goes green on exactly the
#       regression this check exists to catch. The keys are the UNION of the names
#       this advice can carry — the durable filename, the override variable, and
#       the /tmp filename the durable move replaced — so the drifted forms are
#       still in scope and the drift branch reports what they now say.
#       BOUNDARY, stated rather than implied: a site that renames its path to
#       something outside that union is no longer recognisable as a durable site
#       by any content derivation, so this sweep cannot see it. The emitted advice
#       itself stays covered in that case by 12a/12b, which read the script.
#       THIS FILE IS EXCLUDED (derived, not named literally): it names the same
#       strings in its own patterns, so including it would compare a regex against
#       itself and report a permanent false drift.
live_prefix="$LIVE_ROOT/"
checker_rel="${HERE#"$live_prefix"}/preflight_test.sh"
tracked_sites=$(git -C "$LIVE_ROOT" grep -lF -e 'test-env.sh' -e 'jf-env' -e 'JOMFORM_ENV' -- . ":(exclude)$checker_rel" 2>/dev/null || true)
site_count=$(grep -c '[^[:space:]]' <<<"$tracked_sites")
# A site DRIFTS when it does not quote the canonical path — whether it still
# quotes a `${JOMFORM_ENV:-...}` form (a different default) or names a bare path
# with no override at all. Report what the site actually says, so the FAIL line
# locates the drift instead of leaving an empty bracket.
site_drift=""
while IFS= read -r f; do
  [ -n "$f" ] && [ -f "$LIVE_ROOT/$f" ] || continue
  if ! grep -qF "\${JOMFORM_ENV:-$canon}" "$LIVE_ROOT/$f"; then
    quoted=$(sed -nE 's/.*\$\{JOMFORM_ENV:-([^}]*)\}.*/\1/p' "$LIVE_ROOT/$f" | sort -u | tr '\n' ' ')
    if [ -z "${quoted// /}" ]; then
      quoted=$(grep -oE 'source [^ )`]+(test-env|jf-env)\.sh' "$LIVE_ROOT/$f" | sort -u | tr '\n' ' ')
    fi
    site_drift="$site_drift $f=[${quoted% }]"
  fi
done <<<"$tracked_sites"
if [ -n "$tmp_hit" ] || [ -z "$canon" ]; then
  : # already reported by 12a; do not double-count the same violation
elif [ "$site_count" -lt 3 ]; then
  bad "only $site_count durable site(s) found ([$(printf '%s ' $tracked_sites)]) — the doc side of the advice is no longer covered (the sweep must find at least the script plus two docs)"
elif ! grep -qxF 'scripts/preflight.sh' <<<"$tracked_sites"; then
  bad "the derived sweep did not exclude this file correctly — scripts/preflight.sh is a durable site and must be in it, so an exclusion pattern that swallows a whole directory would otherwise pass unnoticed"
elif [ -n "$site_drift" ]; then
  bad "a durable site quotes a different test-env path than the emitted advice ($canon):$site_drift"
else
  ok "all $site_count durable sites quote the same test-env path ($canon)"
fi
# 13. THE WEB IMAGE BUILD MUST BE ONE OF THE GATES, and it must be the REAL
#     build. Until 2026-09-22 no gate built the shipping web artifact at all:
#     the preflight ran `npm run build` on the HOST and the image build happened
#     afterwards inside deploy/build-and-ship.sh, so a tree could be green
#     through every gate with an image build that dies (the MCP-PARITY.md
#     absent-from-context defect, and the `context=web/` defect).
#     Check 2 keeps --list and the `run` call sites in step, but it stays GREEN
#     if the image gate is deleted outright — both sides move together. So this
#     asserts the CAPABILITY, name-agnostically: some gate must run a real
#     `docker build` of web/Dockerfile.
#     It must NOT be a `--check` / `--call=` run: those validate the Dockerfile
#     text but do not execute it, and they do not resolve COPY sources against
#     the context. Measured on a healthy tree: `docker build --check -f
#     web/Dockerfile .` prints "Check complete, no warnings found" for a COPY
#     whose source does not exist. Asserting the class here is what stops a
#     future "it's slow, let's use --check" edit from turning this gate into a
#     tautology.
#     ONLY INVOCATIONS COUNT. A plain `grep 'docker build'` over the active
#     lines also matches the PROSE that necessarily mentions the command: the
#     gate's own `echo "webimage: docker build --file web/Dockerfile …"` line
#     and the human-readable description on the `run webimage "…"` line. Both
#     stay put when the real command is edited, so they satisfied the
#     `web/Dockerfile` assertion on their own — measured: repointing the real
#     command at `api/Dockerfile` left this check GREEN (21 passed / 0 failed)
#     while the gate built the wrong Dockerfile. Hence the matcher below: a real
#     invocation starts the line or follows a shell separator, which is exactly
#     what prose in quotes is not.
image_invocations="$(grep -E '(^[[:space:]]*docker build|[&;|][[:space:]]*docker build)' <<<"$active")"
if [ -z "$image_invocations" ]; then
  bad "no gate in preflight.sh runs 'docker build' — the shipping image (web/Dockerfile) is built by NO gate, so a green preflight does not prove the image builds"
elif ! grep -qE '(--file|-f)[[:space:]]+web/Dockerfile' <<<"$image_invocations"; then
  bad "a gate runs 'docker build' but not of web/Dockerfile — the web image is the shipping artifact and must be the one built: [$image_invocations]"
elif grep -qE '(--check|--call)' <<<"$image_invocations"; then
  bad "the image gate uses --check/--call, which validate the Dockerfile without executing it: that cannot see a COPY source missing from the context or excluded by .dockerignore (measured: --check reports 'no warnings found' for a COPY whose source does not exist). It must be a real build"
else
  ok "a gate runs a real 'docker build' of web/Dockerfile (the shipping web image)"
fi

# 14. The image gate's PREREQUISITES FAIL LOUDLY, they never skip. Same property
#     as checks 3/4/5, and it matters more here than anywhere else: this gate
#     covers the one artifact that ships, so a host where it silently turns
#     itself off is a host whose green preflight proves nothing about the image.
#     Both halves are deterministic and offline: a fake `docker` on the PATH
#     decides whether the daemon looks reachable, and the sandbox controls
#     whether web/Dockerfile exists.
FAKEBIN="$SB/fakebin"
mkdir -p "$FAKEBIN"
# `info` succeeds so the daemon check is satisfied; anything else is a hard
# error, so a gate that (wrongly) proceeded would be unmistakable in the log.
cat > "$FAKEBIN/docker" <<'FAKE'
#!/bin/sh
if [ "${1:-}" = "info" ]; then exit 0; fi
echo "FAKE DOCKER WAS CALLED: $*" >&2
exit 99
FAKE
chmod +x "$FAKEBIN/docker"

# 14a. No web/Dockerfile (the sandbox has none): refused, naming the file, and
#      the gate itself must never have started.
out=$(PATH="$FAKEBIN:$PATH" ./scripts/preflight.sh --only webimage 2>&1)
rc=$?
if [ $rc -ne 0 ] && grep -q 'web/Dockerfile is missing' <<<"$out" && \
   ! grep -q 'gate \[webimage\]' <<<"$out" && \
   ! grep -q 'FAKE DOCKER WAS CALLED' <<<"$out"; then
  ok "missing web/Dockerfile -> image gate refused as a prerequisite, before it ran (rc=$rc)"
else
  bad "a missing web/Dockerfile must refuse the image gate as a prerequisite (before it runs), got rc=$rc [$(grep -iE 'PREFLIGHT FAIL|gate \[webimage\]|FAKE DOCKER' <<<"$out" | tr '\n' ' ')]"
fi

# 14b. An unreachable docker daemon: refused with the daemon named, NOT skipped.
#      A skipped gate is not a passing gate, and this is the gate that covers
#      the shipping artifact — the one place a silent skip is least acceptable.
mkdir -p "$SB/web"
cp "$HERE/../web/Dockerfile" "$SB/web/Dockerfile" 2>/dev/null
cat > "$FAKEBIN/docker" <<'FAKE'
#!/bin/sh
if [ "${1:-}" = "info" ]; then echo "Cannot connect to the Docker daemon" >&2; exit 1; fi
echo "FAKE DOCKER WAS CALLED: $*" >&2
exit 99
FAKE
chmod +x "$FAKEBIN/docker"
out=$(PATH="$FAKEBIN:$PATH" ./scripts/preflight.sh --only webimage 2>&1)
rc=$?
if [ $rc -ne 0 ] && \
   grep -q 'docker daemon is unreachable' <<<"$out" && \
   ! grep -q 'gate \[webimage\]' <<<"$out"; then
  ok "unreachable docker daemon -> image gate refused (not skipped) naming the daemon (rc=$rc)"
else
  bad "an unreachable docker daemon must refuse the image gate rather than skip it, got rc=$rc [$(grep -iE 'PREFLIGHT FAIL|gate \[webimage\]' <<<"$out" | tr '\n' ' ')]"
fi
rm -f "$SB/web/Dockerfile"

# 15. The DOCUMENTED gate table (GATE-INVENTORY.md) must match what the runner can
#     actually do, in BOTH directions. This is the drift that motivated moving the
#     table out of AGENTS.md: that file is write-gated, so a headless worker that
#     adds a gate CANNOT add its row (its write is refused and the card blocks),
#     which is how the table fell five gates behind. The RULE stays in AGENTS.md;
#     the DATA lives here, where agents can edit it — same split as MCP-PARITY.md.
#     A missing or unparseable table is a FAILURE, never a skip: a guard that reads
#     nothing would report the exact green it is meant to prevent.
INV="$HERE/../GATE-INVENTORY.md"
if [ ! -f "$INV" ]; then
  bad "GATE-INVENTORY.md not found at $INV — the documented gate table is missing, so nothing checks it against --list"
else
  # The first backticked token at the start of a table row is a gate NAME. Parsing
  # must ignore the prose above the table and the "See also" line below it, and
  # must NOT pick up backticked commands in the other columns.
  documented="$(grep -E '^\|' "$INV" | grep -vE '^\|[[:space:]]*-' \
    | awk -F'|' '{print $2}' | grep -oE '`[A-Za-z0-9_]+`' | tr -d '`' | sort -u)"
  if [ -z "$documented" ]; then
    bad "GATE-INVENTORY.md has a table but no gate names could be parsed from column 1 — a guard that reads nothing proves nothing"
  elif [ "$documented" = "$(printf '%s\n' $GATES | sort -u)" ]; then
    ok "GATE-INVENTORY.md documents exactly the $NGATES gates --list advertises (no drift)"
  else
    bad "gate-table drift — in the runner but NOT documented: [$(comm -13 <(printf '%s\n' "$documented") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')] / documented but NOT runnable: [$(comm -23 <(printf '%s\n' "$documented") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')]"
  fi

fi

# 16. The self-test must run on its OWN lock file, never the production one.
#     Every invocation above goes through scripts/preflight.sh, which takes the
#     concurrency lock BEFORE it validates arguments or prerequisites — so on a
#     busy board this whole self-test would otherwise queue behind a card's gate
#     (up to PREFLIGHT_LOCK_WAIT, 1800s by default) and, once running, hold that
#     same lock against every card waiting behind IT. Measured: a self-test that
#     should take seconds sat 10+ minutes holding the production lock with four
#     cards running. Two properties, checked against the SCRIPT (not this file's
#     prose, which could be edited independently):
#       (a) the default is still the production path — a self-test that pointed
#           at a private lock by DEFAULT would be a real regression, because the
#           gate body would then be unserialised;
#       (b) this run's resolved lock is NOT that default — i.e. the override above
#           actually took effect and is still in the file.
#     (b) reads the live value, so deleting the export makes this RED.
default_lock="${PREFLIGHT_LOCK_FILE_DEFAULT:-/var/lock/jomform-preflight-gate.lock}"
if [ ! -f "$PREFLIGHT" ]; then
  bad "cannot check self-test lock isolation: $PREFLIGHT missing"
elif ! grep -qE 'PREFLIGHT_LOCK_FILE="\$\{PREFLIGHT_LOCK_FILE:-' "$PREFLIGHT"; then
  bad "preflight.sh no longer defaults PREFLIGHT_LOCK_FILE through \${PREFLIGHT_LOCK_FILE:-...} — the override this self-test relies on is gone"
elif [ -z "${PREFLIGHT_LOCK_FILE:-}" ]; then
  bad "PREFLIGHT_LOCK_FILE is unset in the self-test — it would queue on the production lock and hold it against live gates"
elif [ "$PREFLIGHT_LOCK_FILE" = "$default_lock" ]; then
  bad "the self-test is using the PRODUCTION lock ($default_lock) — it will serialise against (and delay) every card gate on the board"
else
  ok "self-test uses a private lock ($PREFLIGHT_LOCK_FILE), not the production path ($default_lock)"
fi

# 15. PATH-AWARE SELECTION (--for-diff / --classify) — the narrowing must BITE,
#     and it must be SAFE. These assertions are the reason the feature can be
#     trusted at all, so each one is written to fail when the property is broken:
#       * the table must cover every gate --list advertises (no gate is
#         unreachable-by-narrowing and no gate is silently always-run);
#       * a web-only diff must select `web` and must NOT select `test` — the BITE;
#       * adding one api/ path must bring `test` back — the diff decides;
#       * a path NO gate claims must widen to the FULL set — fail-closed;
#       * the four gate-INPUT prose files (MCP-PARITY.md, GATE-INVENTORY.md,
#         AGENTS.md, deploy/backup/README.md) must widen the run, because a
#         test READS them. A path-class rule that treats prose as inert is the
#         trap that turns a red gate green.
#     Driven through --classify, which classifies an EXPLICIT path list and runs
#     nothing, so none of this creates files or runs a gate.
#     NOTE: the sandbox has no web/ or api/ tree, so these run against the LIVE
#     script from the real repo root — the same place a real narrowing happens.
sel_classify() { (cd "$LIVE_ROOT" && ./scripts/preflight.sh --classify="$1" 2>/dev/null) ; }

# 15a. The documented table must name every gate, in a form the machinery reads.
deps="$(cd "$LIVE_ROOT" && ./scripts/preflight.sh --gate-deps 2>/dev/null)"
deps_gates="$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_]*\):.*/\1/p' | sort -u)"
if [ -z "$deps" ]; then
  bad "--gate-deps printed nothing — the path-aware table cannot be inspected, so its coverage cannot be checked"
elif [ "$deps_gates" = "$(printf '%s\n' $GATES | sort -u)" ]; then
  ok "--gate-deps covers exactly the $NGATES gates --list advertises (no gate missing from the path table)"
else
  bad "--gate-deps drift — in --list but NOT in the table: [$(comm -13 <(printf '%s\n' "$deps_gates") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')] / in the table but not in --list: [$(comm -23 <(printf '%s\n' "$deps_gates") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')]"
fi

# The gates that CANNOT narrow: their inputs are the whole checkout / the whole
# git history, so no path list can express them and they appear in EVERY
# selection (see pw_gate_blocked). Derived ONCE here, from the same table 15a
# checks, because three checks below need it: 15b/15c subtract it to read "which
# NARROWABLE gate reads this path", and 15f asserts each of them survives a
# narrowed run.
star_gates="$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_]*\):[[:space:]]*\*$/\1/p')"

# 15b. THE BITE: a diff whose ONLY reader is the `web` gate must select `web` and
#      must NOT select `test`. A selection that cannot drop a gate is not a
#      narrowing, and `test` is the expensive one (a full api suite).
#
#      THE ANCHOR IS DERIVED, NOT NAMED — and that is the FIX, not a weakening
#      (2026-09-27). This check used to name `web/tsconfig.json`, picked because
#      it had exactly one reader. It stopped having one when the api suite gained
#      a read of `web/`: `api/internal/httpapi/passkey_seam_test.go` builds
#      `filepath.Join(root, "web")`, and the deriver resolves a bare DIRECTORY
#      candidate to `<dir>/*` — deliberately, because api sources really do walk
#      `web/src/**` and read `web/public/openapi.json`, so hand-narrowing that
#      candidate would UNDER-include (the unsafe direction). The stale part was
#      the assertion's PREMISE, not the runner: `test` now legitimately claims
#      every path under `web/`, so naming another `web/...` path would assert the
#      same false premise again. The path is therefore FOUND here — the tracked
#      root-level files are scanned for one whose narrowable readers are exactly
#      `web` (today: root `.gitignore`, `Makefile` and `AGENTS.md`, each read by
#      `web/src/lib/nextManagedFiles.test.ts`).
#
#      IT CANNOT DEGRADE INTO "test may or may not appear": the assertion below is
#      unchanged, and when no such path exists the check goes RED BY NAME — which
#      is exactly the reading that was missing while this was green and stale.
#      The scanned set is the COMPLETE space a web-only claim can live in, not a
#      guess: a path is claimed by `web` only if a web-gate source reads it, and
#      those reads land either under `web/**` (every one of them claimed by `test`
#      through the directory candidate above), under `api/**` (claimed by all four
#      api gates), or on a repo-root file. So a web-only path must be a root file.
web_anchor=""; web_sel=""
for f in $(cd "$LIVE_ROOT" && git ls-files 2>/dev/null | grep -v '/'); do
  [ -n "$f" ] && [ -e "$LIVE_ROOT/$f" ] || continue
  cand_sel="$(sel_classify "$f")"
  cand_readers="$(grep -v '^#reason:' <<<"$cand_sel" | while IFS= read -r g; do
      [ -n "$g" ] || continue
      grep -qx "$g" <<<"$star_gates" || printf '%s\n' "$g"
    done)"
  if [ "$cand_readers" = "web" ]; then
    web_anchor="$f"; web_sel="$cand_sel"; break
  fi
done
if [ -z "$web_anchor" ]; then
  bad "no tracked root-level file is read by EXACTLY ONE narrowable gate (the web gate), so the BITE below has no anchor — a web-only diff would never be able to skip \`test\`. This is the shape that made the old web/tsconfig.json anchor stale: an api source reading web/ widens every web/ path's reader set. A path that is genuinely web-only must exist"
elif grep -qx 'web' <<<"$web_sel" && ! grep -qx 'test' <<<"$web_sel"; then
  ok "a diff whose only narrowable reader is \`web\` ($web_anchor) selects web and SKIPS test (the narrowing bites)"
else
  bad "a web-only diff must select web and skip test — anchor [$web_anchor], got: [$(printf '%s' "$web_sel" | tr '\n' ' ')]"
fi

# 15c. Adding ONE api/ path must bring test BACK — the diff decides, not a static
#      rule. Anchored on the SAME derived path as 15b, so the pair reads as one
#      property: the web-only anchor drops `test` (15b), and one added api/ path
#      restores it (here). Naming a web/ path directly made this vacuous once the
#      api suite started reading web/ — `test` was already selected, so it could
#      not show that the diff, rather than the table, put it there.
if [ -z "$web_anchor" ]; then
  bad "cannot check that an api/ path brings test back: 15b found no web-only anchor to add it to"
else
  both_sel="$(sel_classify "$web_anchor,api/internal/httpapi/rest.go")"
  if grep -qx 'test' <<<"$both_sel" && grep -qx 'web' <<<"$both_sel"; then
    ok "adding one api/ path to the web-only diff ($web_anchor) brings test back"
  else
    bad "an api/+web/ diff must select BOTH test and web — got: [$(printf '%s' "$both_sel" | tr '\n' ' ')]"
  fi
fi

# 15c-ii. A DERIVED DIRECTORY candidate must claim everything under it. The api
#      suite walks web/src as a DIRECTORY, so a change to a file inside it is a
#      real input to `test`. This failed while PW_CLAIMS stored the unresolved
#      candidate (`web/src`) instead of the resolved pathspec (`web/src/*`): the
#      path came out claimed by NO gate and the run widened to all 14. Asserting
#      a DEEP path (not the directory itself) is what catches that shape.
deep_sel="$(sel_classify 'web/src/app/page.tsx')"
if grep -qx 'test' <<<"$deep_sel"; then
  ok "a deep file under a derived directory candidate (web/src/app/page.tsx) still selects test — the directory claim is <dir>/*, not <dir>"
else
  bad "web/src/app/page.tsx must select test (the api suite walks web/src/**) — got: [$(printf '%s' "$deep_sel" | tr '\n' ' ')]"
fi

# 15d. FAIL-CLOSED: a path no gate claims must widen to the full set. The path is
#      a real tracked directory that no gate's area covers, so a rule that
#      silently ignored unknown paths would show up here as a narrowed set.
#      The count EXCLUDES the trailing "#reason:" line, which is not a gate.
unknown_sel="$(sel_classify 'docs/README.md')"
unknown_n="$(grep -cv '^#reason:' <<<"$unknown_sel")"
if [ "$unknown_n" = "$NGATES" ] && grep -q 'no gate claims' <<<"$unknown_sel"; then
  ok "a path no gate claims widens to the FULL $NGATES-gate set, naming the path (fail-closed)"
else
  bad "an unclaimed path must widen to all $NGATES gates (got $unknown_n) — [$(printf '%s' "$unknown_sel" | tr '\n' ' ')]"
fi

# 15e. THE TRAP: the gate-INPUT prose files must widen the run. Each is read by a
#      test, so a change to it can break that gate; a classifier that dropped it
#      would make the run green on a tree whose gate is red.
#      A file is pinned to a SPECIFIC gate: MCP-PARITY.md is read by
#      mcp_parity_test.go (the `test` gate), GATE-INVENTORY.md and AGENTS.md by
#      scripts/preflight_test.sh (the `selftest` gate), and deploy/backup/README.md
#      by the backup suite (the `backup` gate).
trap_out=""
for spec in "MCP-PARITY.md:test" "GATE-INVENTORY.md:selftest" "AGENTS.md:selftest" "deploy/backup/README.md:backup"; do
  tfile="${spec%%:*}"; tgate="${spec##*:}"
  if [ ! -e "$LIVE_ROOT/$tfile" ]; then
    bad "gate-INPUT pin not checked: $tfile does not exist at $LIVE_ROOT, so this pin proves nothing"
    continue
  fi
  tsel="$(sel_classify "$tfile")"
  if grep -qx "$tgate" <<<"$tsel"; then
    trap_out="${trap_out}${tfile}->${tgate} ok; "
  else
    bad "$tfile alone must widen the run and select '$tgate' (it is READ by that gate's test) — got: [$(printf '%s' "$tsel" | tr '\n' ' ')]"
    trap_out="${trap_out}${tfile}->${tgate} MISSING; "
  fi
done
if grep -qv 'MISSING' <<<"$trap_out" && [ -n "$trap_out" ]; then
  ok "the 4 gate-INPUT prose files each widen the run to the gate that reads them ($trap_out)"
fi

# 15f. A gate that CANNOT narrow must always run. `gitleaks` scans the whole git
#      history, so no path list can express its inputs. Asserted from the TABLE
#      (a gate marked `*` must be in every selection), which is the invariant the
#      machinery implements — not the gate's name. `$star_gates` was derived once
#      above (15b needs it too), so there is a single reading of the class here.
if [ -z "$star_gates" ]; then
  bad "no gate is marked '*' (cannot narrow) in --gate-deps — the always-run class is empty, which cannot be right for a full-history secret scan"
elif [ "$(printf '%s\n' "$star_gates" | wc -l)" != "$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_]*\):[[:space:]]*\*$/\1/p' | sort -u | wc -l)" ]; then
  bad "the '*' (cannot narrow) set has duplicates in --gate-deps — the table is being built inconsistently"
else
  miss_star=""
  for g in $star_gates; do
    grep -qx "$g" <<<"$web_sel" || miss_star="${miss_star}${g} "
  done
  if [ -z "$miss_star" ]; then
    ok "every '*' gate (cannot narrow) runs in a narrowed selection: [$(printf '%s ' $star_gates)]"
  else
    bad "a '*' gate (cannot narrow) was DROPPED from a narrowed run: [${miss_star% }] — it must always run"
  fi
fi

# 15j. The always-run CLASS ITSELF must be stable across table builds. The table
#      is derived per invocation, and the derivation reads which of a gate's
#      sources reads the whole checkout. That read was a `grep ... | grep -qE ...`
#      under `set -o pipefail`, where a MATCH makes `grep -q` exit early, the
#      writer die of SIGPIPE (141), and pipefail report the pipeline as FAILED —
#      measured 2 misses in 40 runs of the same line. A miss drops a repo-wide
#      reader from PW_RW_FILES, so its gate is treated as NARROWABLE and can be
#      dropped from a narrowed run: a red gate would have gone green, and
#      `prunewt` is the gate it happened to. Repeated builds are the only way to
#      see a nondeterministic verdict, so this asserts the SET, not one reading.
if [ -z "$star_gates" ]; then
  bad "cannot check the stability of the always-run class: no '*' gate was found"
else
  star_runs=""
  for _i in 1 2 3 4 5 6; do
    star_runs="${star_runs}$(cd "$LIVE_ROOT" && ./scripts/preflight.sh --gate-deps 2>/dev/null \
      | sed -n 's/^\([A-Za-z0-9_]*\):[[:space:]]*\*$/\1/p' | sort | tr '\n' ' ')"$'\n'
  done
  star_uniq="$(grep -v '^$' <<<"$star_runs" | sort -u | wc -l)"
  if [ "$star_uniq" = 1 ]; then
    ok "the always-run ('*') class is IDENTICAL across 6 table builds: [$(grep -v '^$' <<<"$star_runs" | sed -n '1p')]"
  else
    bad "the always-run ('*') class CHANGES between table builds — it is nondeterministic, so a gate that cannot narrow can be silently dropped from a narrowed run. Observed sets: [$(grep -v '^$' <<<"$star_runs" | sort -u | tr '\n' '|')]"
  fi
fi

# 15k. The always-run class must contain every gate whose sources READ THE WHOLE
#      CHECKOUT, re-derived here INDEPENDENTLY of the runner's own derivation. 15j
#      only catches a class that CHANGES between builds; a probe that is
#      permanently blind would look stable and pass it. This is the deterministic
#      half: read each gate's source set, look for an active `git grep` /
#      `git ls-files` / `git rev-list` line in those sources, and assert that
#      every gate that has one is in the '*' (always-run) class.
#      `selftest`'s own sources include preflight.sh and preflight_test.sh, both of
#      which run `git grep` over this checkout — so a probe blind to its own
#      self-hit is caught here even when every build agrees with itself.
pw_src_of() {  # mirrors pw_sources in the runner under test
  case "$1" in
    build|vet|test|govuln) printf '%s\n' 'api/*' ;;
    web)      printf '%s\n' 'web/package.json' 'web/src/*' 'web/next.config.mjs' 'web/tsconfig.json' 'web/scripts/web-deps-check.mjs' ;;
    a11y)     printf '%s\n' 'web/e2e/*' 'web/playwright.config.ts' ;;
    npmaudit) printf '%s\n' 'web/package.json' 'web/package-lock.json' ;;
    monitor)  printf '%s\n' 'deploy/monitoring/*' ;;
    tmpkey)   printf '%s\n' 'deploy/tmp-apikey.sh' 'deploy/tmp-apikey_test.sh' ;;
    backup)   printf '%s\n' 'deploy/backup/*' ;;
    prunewt)  printf '%s\n' 'deploy/worktrees/*' ;;
    refguard) printf '%s\n' 'scripts/refguard.sh' 'scripts/refguard_test.sh' ;;
    selftest) printf '%s\n' 'scripts/preflight.sh' 'scripts/preflight_test.sh' ;;
    gitleaks) printf '%s\n' '.gitleaks.toml' ;;
  esac
}
# The independent probe's pattern. It is deliberately a DIFFERENT literal from the
# runner's (`REPOWIDE_RE` there), and it runs as its own code path: the verdict
# comes from an independent reading, so a derivation that goes BLIND cannot satisfy
# it by finding no offenders at all. Comments are stripped exactly as the runner
# strips them — without that, a comment mentioning the shape over-reports (measured:
# a false positive on `web`).
REPOWIDE_ANY_RE='git[[:space:]]+([^|]*[[:space:]])?(grep|ls-files|rev-list)[[:space:]]'
rw_any=""
for g in $GATES; do
  while IFS= read -r glob; do
    [ -n "$glob" ] || continue
    for f in $(cd "$LIVE_ROOT" && git ls-files -- "$glob" 2>/dev/null); do
      [ -f "$LIVE_ROOT/$f" ] || continue
      if grep -qE "$REPOWIDE_ANY_RE" <<<"$(grep -avE '^[[:space:]]*(#|//|\*|/\*)' "$LIVE_ROOT/$f" 2>/dev/null)"; then
        rw_any="${rw_any}${g} "
      fi
    done
  done < <(pw_src_of "$g")
done
rw_any="$(printf '%s\n' "$(printf '%s' "$rw_any" | tr ' ' '\n')" | grep -v '^$' | sort -u | tr '\n' ' ')"
if [ -z "$rw_any" ]; then
  bad "NON-VACUITY: the independent probe found no gate source with a whole-checkout read at all — this check would pass trivially, so the probe is broken (expected at least 'prunewt' and 'selftest')"
else
  rw_missing=""
  for g in $rw_any; do
    grep -qx "$g" <<<"$star_gates" || rw_missing="${rw_missing}${g} "
  done
  if [ -z "$rw_missing" ]; then
    ok "every gate whose sources read the whole checkout is in the '*' (always-run) class: [$(printf '%s' "$rw_any")] (independently derived, not off the runner's own verdict)"
  else
    bad "these gates read the whole checkout, independently derived, but are NOT in the always-run class, so a narrowed run can drop them: [${rw_missing% }] — an under-inclusion, the unsafe direction"
  fi
fi

# 15g. The default must stay a FULL run. With no selection flag the script must
#      print no narrowing notice and report no skipped gate, and its banner must
#      list the whole inventory. Asserted on the LIVE script, far enough into the
#      run to print its banner and SKIPPED list, and stopped before any gate body
#      by an unset prerequisite.
#
#      THIS CHECK MUST BE ABLE TO GO RED. The first version of it ended every
#      branch in `ok`, INCLUDING the failure branch, so it printed green while
#      asserting nothing — a guard with no bite, which is the exact rot this file
#      exists to catch. Every branch below now fails or passes on its own
#      evidence, and check 15i drives the same predicate against a deliberately
#      narrowed run to prove it detects one.
nounset_out="$( (cd "$LIVE_ROOT" && env -u JOMFORM_TEST_DATABASE_URL -u JOMFORM_TEST_VALKEY_URL \
  timeout 120 ./scripts/preflight.sh 2>&1; printf 'rc=%s\n' "$?") || true)"
narrow_note=""; skip_note=""; banner=""; rc_line=""
grep -qE 'NARROWED RUN|--for-diff' <<<"$nounset_out" && narrow_note=1
grep -q 'GATE(S) SKIPPED' <<<"$nounset_out" && skip_note=1
grep -q "gates:  ${GATES//$'\n'/ }" <<<"$nounset_out" && banner=1
rc_line="$(printf '%s\n' "$nounset_out" | sed -n 's/^rc=//p' | sed -n '1p')"
if [ "$narrow_note" = 1 ]; then
  bad "a no-flag run printed a narrowing notice — the default must be a full run"
elif [ "$skip_note" = 1 ]; then
  bad "a no-flag run reported skipped gates — the default must be a full run"
elif [ "$banner" != 1 ]; then
  bad "a no-flag run's banner does not list all $NGATES gates (expected 'gates:  ${GATES//$'\n'/ }') — the default may no longer be a full run"
elif [ "$rc_line" != 1 ]; then
  bad "a no-flag run stopped in a state other than the fail-fast prerequisite (rc=$rc_line, want 1) — this check must observe the real banner, not a run that never started"
else
  ok "the default (no --for-diff) is still a FULL run: all $NGATES gates in the banner, no narrowing notice, no skipped gate"
fi

# 15h. A bare --for-diff (no ref) must be REFUSED, not silently widened to a full
#      run: an empty base is indistinguishable from "flag absent" unless the
#      GIVEN-ness is tracked separately, which is how this was found.
bare_out="$(cd "$LIVE_ROOT" && ./scripts/preflight.sh --for-diff 2>&1)"; bare_rc=$?
if [ $bare_rc -ne 0 ] && grep -q -- '--for-diff needs a base ref' <<<"$bare_out"; then
  ok "--for-diff with no ref is refused (rc=$bare_rc), not silently widened"
else
  bad "a bare --for-diff must be refused — got rc=$bare_rc [$(printf '%s' "$bare_out" | tail -n 1)]"
fi

# 15i. 15g's predicate must BITE. A no-flag run is asserted to print no narrowing
#      notice; the way that assertion fails is by seeing that notice. Rather than
#      sabotage the live script (this gate runs on the tree under test), drive the
#      SAME predicate over a run that IS narrowed — a one-path --classify selection
#      is not a run, so the notice is simulated with the exact banner text
#      preflight.sh emits, and the check must report it as a violation.
narrow_sample='  #  !!  NARROWED RUN: --for-diff origin/main'
if grep -qE 'NARROWED RUN|--for-diff' <<<"$narrow_sample"; then
  ok "15g's predicate BITES: the exact NARROWED RUN banner text is detected as a narrowing notice (so a default that narrowed would be RED)"
else
  bad "15g's predicate does not recognise preflight.sh's own NARROWED RUN banner — 15g can never go red"
fi
# and the counter-case: a full-run banner must NOT trip it. Read from a here-string
# like every other assertion in this file, so the writer cannot die of SIGPIPE and
# turn this "must be silent" case into a false red.
if grep -qE 'NARROWED RUN|--for-diff' <<<'  gates:  build vet test'; then
  bad "15g's predicate fires on a full-run banner — it would fail every default run"
else
  ok "15g's predicate is silent on a full-run banner (no false red)"
fi

# 17. THE api `test` GATE MUST CARRY AN EXPLICIT PER-PACKAGE BUDGET (a -timeout).
#
#     `go test` panics a package after 10 minutes, and that default is PER
#     PACKAGE, applied to every package (`go help testflag`: "-timeout d ... The
#     default is 10 minutes (10m)"). `internal/httpapi` is the largest package and
#     its cost tracks host load, so on a busy host it crosses 600s and the gate
#     reports `panic: test timed out after 10m0s` for a package that is GREEN —
#     measured on this host: pristine `origin/main` (ca110813) rc=1 at 601.195s,
#     the same package rc=0 at 620.593s. A red that looks exactly like a hung test
#     is a red nobody can act on, so the budget must be explicit in the gate, not
#     inherited.
#
#     WHY THIS IS ASSERTED BY RUNNING THE GATE, NOT BY GREPPING ITS TEXT: a
#     grep for "-timeout" is satisfied by the prose of this very check and by a
#     `-timeout` written anywhere in the file, including a comment. What has to
#     hold is that the command `go test` is ACTUALLY handed carries it. So `go`
#     itself is stubbed on the PATH and the gate is run for real against the LIVE
#     script — the stub prints the argv it was given, and that argv is the thing
#     read here. Cost: the stub never compiles anything, so this is seconds.
TIMEBIN="$SB/timebin"
mkdir -p "$TIMEBIN"
cat > "$TIMEBIN/go" <<'FAKEGO'
#!/bin/sh
echo "FAKE-GO ARGV: $*"
exit 99
FAKEGO
chmod +x "$TIMEBIN/go"

# The argv the gate handed `go test`, read off a real run of the LIVE runner.
# `--only test` keeps every other gate out of the run, and the stub makes the
# gate fail FAST — so the assertion below is about the COMMAND, never about the
# suite passing here (this self-test must not cost a full api suite).
test_gate_argv() {  # $1 = value for API_TEST_TIMEOUT (empty means "let the default apply")
  (cd "$LIVE_ROOT" && env ${1:+API_TEST_TIMEOUT="$1"} \
      PATH="$TIMEBIN:$PATH" \
      JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder \
      ./scripts/preflight.sh --only test 2>&1 \
    | sed -n 's/^FAKE-GO ARGV: //p' | sed -n '1p')
}

# A -timeout VALUE is a bare duration: the flag, then -race, -count=1 in the
# order the gate writes them. Anchored on `-race -count=1 -timeout ` so this
# cannot be satisfied by some other flag's argument.
gate_budget() { sed -nE 's/.*-race -count=1 -timeout ([^ ]*).*/\1/p' <<<"$1" | sed -n '1p'; }

live_argv="$(test_gate_argv '')"
live_budget="$(gate_budget "$live_argv")"
if [ -z "$live_argv" ]; then
  bad "the api \`test\` gate handed no recognizable argv to \`go test\` on the LIVE runner (stubbed go saw nothing) — this check cannot read the budget, so the missing -timeout it exists to catch is now invisible"
elif [ -z "$live_budget" ]; then
  bad "the api \`test\` gate runs \`go test\` WITHOUT -timeout, so every package inherits Go's 10m per-package default and a green-but-slow \`internal/httpapi\` is reported as \`panic: test timed out after 10m0s\` (measured: pristine main rc=1 at 601.195s, green at 620.593s with a larger budget). argv was: [$live_argv]"
else
  ok "the api \`test\` gate hands \`go test\` an explicit budget by default (-timeout $live_budget), instead of inheriting Go's 10m per-package default"
fi

# 17b. The budget must come from API_TEST_TIMEOUT, so a host slower than this one
#      can raise the ceiling without editing the gate. A HARDCODED literal passes
#      17a and fails here — that is the whole reason this leg exists.
over_argv="$(test_gate_argv '45m')"
over_budget="$(gate_budget "$over_argv")"
if [ "$over_budget" = "45m" ]; then
  ok "API_TEST_TIMEOUT is honoured: the gate handed \`go test\` -timeout 45m when asked (it is an override, not a hardcoded literal)"
else
  bad "API_TEST_TIMEOUT does not reach the gate's \`go test\` argv — got [${over_budget:-none}] for 45m (so raising the ceiling on a slower host is impossible without editing the runner). argv was: [$over_argv]"
fi

# 17c. THE BITE. 17a reads a budget off a real run; the way that assertion fails
#      is by reading NO budget. Rather than sabotage the live runner (this gate
#      runs on the tree under test), the same predicate is driven over a real gate
#      line with `-timeout` STRIPPED — a compiling copy of the runner in the
#      sandbox, with an api/ directory so the gate body actually reaches the stub.
#      Both directions are asserted: the intact copy carries a budget, the
#      stripped one carries none and is REPORTED as the violation.
mkdir -p "$SB/api"
intact_budget="$(gate_budget "$live_argv")"
sed -i 's/ -timeout \$API_TEST_TIMEOUT//g; s/ -timeout .\$API_TEST_TIMEOUT.//g' "$SB/scripts/preflight.sh"
stripped_argv="$(cd "$SB" && env JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder PATH="$TIMEBIN:$PATH" \
      ./scripts/preflight.sh --only test 2>&1 | sed -n 's/^FAKE-GO ARGV: //p' | sed -n '1p')"
stripped_budget="$(gate_budget "$stripped_argv")"
if [ -z "$intact_budget" ]; then
  bad "cannot run 17c's bite: the intact runner yielded no budget to strip, so comparing it against a stripped copy proves nothing (see 17a)"
elif [ -z "$stripped_argv" ]; then
  bad "17c could not read the stripped copy's argv (stubbed go saw nothing) — the bite leg never ran, so a missing -timeout would still read green"
elif [ -n "$stripped_budget" ]; then
  bad "17c does not bite: with \`-timeout\` removed from the copy the predicate STILL reads a budget ([$stripped_budget]) — the check cannot detect the removal it exists to catch"
else
  ok "17c BITES: the same predicate reads no budget once \`-timeout\` is stripped from a copy (intact=[$intact_budget], stripped=[none]) — 17a can go RED"
fi
# The sandbox copy is shared with the checks above; put it back as it was.
cp "$PREFLIGHT" "$SB/scripts/preflight.sh" && chmod +x "$SB/scripts/preflight.sh"

# 17d. The budget must fail CLOSED. `-timeout 0` turns the alarm OFF, so a hung
#      package would block the gate with no bound — refused rather than accepted.
#      And a value that is not a Go duration must be refused BEFORE it reaches
#      `go test`, where it fails every package on the flag alone and reads as a
#      broken suite.
#      Driven against the LIVE runner with the placeholder prerequisites the
#      env-preflight requires (otherwise the runner stops on those FIRST and this
#      check would be asserting the wrong refusal) and with the STUBBED `go`, so a
#      value that is accepted can never reach a real `go test`.
test_rc_and_argv() {  # $1 = API_TEST_TIMEOUT; prints "rc<TAB>argv"
  (cd "$LIVE_ROOT" && env API_TEST_TIMEOUT="$1" PATH="$TIMEBIN:$PATH" \
      JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder \
      ./scripts/preflight.sh --only test 2>&1; printf 'rc=%s\n' "$?") > "$SB/budget.out" 2>&1
  printf '%s\t%s' \
    "$(sed -n 's/^rc=//p' "$SB/budget.out" | sed -n '1p')" \
    "$(sed -n 's/^FAKE-GO ARGV: //p' "$SB/budget.out" | sed -n '1p')"
}
out_rc="$(test_rc_and_argv 0)"; rc="${out_rc%%$'\t'*}"
if [ "$rc" != 0 ] && grep -q 'API_TEST_TIMEOUT=0 disables' "$SB/budget.out" && \
   ! grep -q 'JOMFORM_TEST_DATABASE_URL is unset' "$SB/budget.out"; then
  ok "API_TEST_TIMEOUT=0 (no timeout at all) -> refused by the budget check itself (rc=$rc), not handed to \`go test\`"
else
  bad "API_TEST_TIMEOUT=0 must be refused by the budget check (rc=$rc) — got: [$(sed -n 's/^FAKE-GO ARGV/UNEXPECTED go test invocation: &/p' "$SB/budget.out" | sed -n '1p')$(grep -E 'PREFLIGHT FAIL' "$SB/budget.out" | sed -n '1p')]"
fi
out_rc="$(test_rc_and_argv banana)"; rc="${out_rc%%$'\t'*}"; argv="${out_rc##*$'\t'}"
if [ "$rc" != 0 ] && grep -q 'is not a Go duration' "$SB/budget.out" && [ -z "$argv" ]; then
  ok "a non-duration API_TEST_TIMEOUT -> refused (rc=$rc) with no \`go test\` invocation at all, so it cannot fail packages on the flag alone"
else
  bad "a non-duration API_TEST_TIMEOUT must be refused before it reaches \`go test\` — rc=$rc argv=[$argv]"
fi
# An EMPTY value is "unset", so the default must still apply — otherwise a caller
# that exports the variable empty (`API_TEST_TIMEOUT= ./preflight.sh`) silently
# loses the whole budget this card added. Asserted on the argv actually handed to
# `go test`, same as 17a.
#
# The EXPECTED default is DERIVED from the runner (`API_TEST_TIMEOUT:=<value>`),
# never written here: this file's standing rule is that a hardcoded expectation
# is what goes stale. A literal `30m` would also go RED on a deliberate change of
# the gate's default — punishing exactly the edit it should allow — so what is
# asserted is the PROPERTY (empty falls back to the same budget a bare run gets),
# not the number. The `-timeout 0` half of this leg is asserted against `-timeout`
# rather than that derived value, because the runner's own die-message for `0`
# necessarily names it in prose.
declared_default="$(sed -nE 's/^: "\$\{API_TEST_TIMEOUT:=([^}]*)\}".*/\1/p' "$PREFLIGHT" | sed -n '1p')"
empty_rc="$(test_rc_and_argv '')"; argv="${empty_rc##*$'\t'}"
if [ -z "$declared_default" ]; then
  bad "cannot read the api \`test\` gate's default budget from $PREFLIGHT (expected a ': \"\\\${API_TEST_TIMEOUT:=<value>}\"' line) — without it the fallback below cannot be compared against anything"
fi
if [ -n "$declared_default" ] && [ "$(gate_budget "$argv")" = "$declared_default" ]; then
  ok "an EMPTY API_TEST_TIMEOUT falls back to the declared default (\`go test\` was handed -timeout $declared_default), it does not disable the budget"
elif printf '%s' "$argv" | grep -q -- '-timeout 0'; then
  bad "an empty API_TEST_TIMEOUT handed \`go test\` '-timeout 0' — that DISABLES the timeout alarm, so a hung package would block the gate with no bound at all"
else
  bad "an empty API_TEST_TIMEOUT must fall back to the declared default budget ($declared_default) — argv was [$argv]"
fi
rm -f "$SB/budget.out"

# 17e. The gate command is the one an OPERATOR COPIES, so the documented row for
#      it must name the same budget. GATE-INVENTORY.md's table is otherwise kept
#      honest by check 15 (names, both directions) — which CANNOT see this: a row
#      that keeps its name and silently drops the budget stays green there, and
#      the operator then copies a command that reds a healthy tree on a busy host.
inv_row="$(grep -E '^\|[[:space:]]*`test`[[:space:]]*\|' "$INV" 2>/dev/null | sed -n '1p')"
if [ -z "$inv_row" ]; then
  bad "GATE-INVENTORY.md has no row for the \`test\` gate — the command an operator copies is unverifiable"
elif [ -z "$(gate_budget "$inv_row")" ]; then
  bad "GATE-INVENTORY.md's \`test\` row quotes the gate command WITHOUT -timeout, so the command an operator copies reds a healthy tree on a busy host: [$inv_row]"
else
  ok "GATE-INVENTORY.md's \`test\` row quotes the budget too (an operator copying the command gets the same ceiling)"
fi

echo "---"
echo "preflight self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1