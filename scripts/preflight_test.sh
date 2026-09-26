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
GATES="$(printf '%s\n' "$listed" | grep '[^[:space:]]')"
NGATES="$(printf '%s\n' "$GATES" | grep -c '[^[:space:]]')"

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
  bad "an existing-but-uninstalled web/node_modules must be refused by the dependency PREREQUISITE (not by the gate running and failing), got rc=$rc [$(printf '%s' "$out" | grep -iE 'IS NOT INSTALLED|PREFLIGHT FAIL|gate \[web\]' | tr '\n' ' ')]"
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
ndefaults=$(printf '%s\n' "$script_defaults" | grep -c '[^[:space:]]')
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
site_count=$(printf '%s\n' "$tracked_sites" | grep -c '[^[:space:]]')
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

# 13. The DOCUMENTED gate table (GATE-INVENTORY.md) must match what the runner can
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

# 14. The self-test must run on its OWN lock file, never the production one.
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

echo "---"
echo "preflight self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
