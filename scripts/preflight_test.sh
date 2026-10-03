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

# THIS SELF-TEST IS ITSELF RUN INSIDE `preflight.sh`, WHICH EXPORTS `_PREFLIGHT_LOCKED=1`.
# That variable tells a re-invoked copy "you are the locked child, run the gates" —
# so a nested `./scripts/preflight.sh` here inherits it and SKIPS the pre-lock block
# (it believes the lock is already held). The checks then measure a path a real lane
# never takes, and pass or fail for the wrong reason. Measured by the `selftest` gate
# in a full run: `_PREFLIGHT_LOCKED` leaked in and the pre-lock reuse never executed,
# so checks 25 and 26 failed there and nowhere else.
#
# A lane calls preflight.sh from a CLEAN shell, so every nested invocation below must
# start as one: unset it (and any lock env a caller exported) and keep it unset for
# the whole self-test.
unset _PREFLIGHT_LOCKED
export -n _PREFLIGHT_LOCKED 2>/dev/null || true
# THE SAME CLEAN-SHELL RULE FOR THE PATH-AWARE-SELECTION HANDOFF, and this one was
# measured as a false red rather than reasoned about. `--for-diff <ref>` EXPORTS
# the narrowed gate set (`SEL_DIFF` and its companions, `_PREFLIGHT_SEL`) for the
# flock child's handoff — a legitimate mechanism INSIDE one run — and `selected()`
# consults `SEL_DIFF` BEFORE it looks at `--only`/`--skip`. So a self-test that
# inherits it cannot address a single gate: `--skip <all>` then runs the OUTER
# narrowing instead of refusing the empty run, `--only web` runs that same
# narrowing instead of the web gate (refusing on the first missing prerequisite it
# happens to select, which is why the failures all read `web/Dockerfile is
# missing`), and every per-tree-record check writes and reads nothing at all
# because the sandbox tree never got a run. Measured: the `--for-diff origin/main`
# run of a real card (tree 0244c928, 15 gates, `selected=` empty in the record)
# recorded `selftest: fail` with **30 of 69 checks red**, every one of them a
# nested-invocation check, while the SAME bytes run with no `--for-diff` were
# 69/69 green — and the 09:16 record on this very tree shape (tree b43789be),
# which ran unnarrowed, has `selftest: pass`. A lane calls preflight.sh from a
# CLEAN shell, so every nested invocation below must start as one. Exactly the
# reasoning of the `_PREFLIGHT_LOCKED` note above, one handoff over.
unset SEL_DIFF SEL_DIFF_LINE SEL_DIFF_SKIPPED SEL_DIFF_FULL SEL_COUNT SEL_DIFF_REASON SEL_BASE _PREFLIGHT_SEL
# The same reasoning for the environment the runner reads: a caller that ran the
# whole gate with --log or a result dir set must not change what these checks key on.
unset PREFLIGHT_LOG

# Sandbox: a git repo containing only the script (plus the few repo files the
# script and this self-test read), so the script's own repo-root resolution works
# and no real build is ever invoked.
SB="$(mktemp -d /tmp/preflight_test.XXXXXX)"
# Sandbox state that must live OUTSIDE the sandbox repo: the gate lock file and
# the per-tree gate records. Both are written by an invocation of the script, and
# both are untracked, so writing them inside $SB would change the sandbox's
# WORKING-TREE sha on every run — and the record is keyed on that sha, so each
# check would look for a record the previous check had displaced (measured: six
# different tree shas across one self-test). Host state belongs outside the repo
# in production for the same reason, so this mirrors the real layout rather than
# working around it.
SB_STATE="$(mktemp -d /tmp/preflight_test_state.XXXXXX)"
# The per-tree gate RECORD store is pinned to the sandbox HERE, before the first
# nested runner invocation (card t_a50d854d).
#
# This export used to live ~1200 lines further down, beside the record-mechanism
# checks that read it. Every nested `./scripts/preflight.sh --only X` ABOVE that
# line therefore wrote its record into whatever store the CALLER had — and in
# production that is `/var/lib/jomform/gate` (gate_result.sh:108,
# `${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}`). The checks that plant a FAKE
# `go` on the PATH and drive `--only test` with it wrote records whose key_parts
# carry `go=FAKE-GO ARGV: env GOVERSION` and `testdb=placeholder` — a RED
# `test: fail` record, plus a `skip` entry for every other gate — INTO the
# production store, each naming the real worktree the run happened in.
#
# Measured on this host: 143 records, 55 carrying FAKE-GO (the card measured
# 47/109, so it was still growing). Those records are inert for reuse (their key
# carries the FAKE-GO fingerprint, which a real run never produces), but the store
# is meant to be evidence of what was actually gated, and a third of it was
# fixtures naming real worktrees.
#
# Pinned unconditionally and early, so a check added anywhere in this file is
# inside the sandbox by default rather than by remembering.
RECORD_DIR="$SB_STATE/gate-results"
export PREFLIGHT_RESULT_DIR="$RECORD_DIR"
export PREFLIGHT_RESULT_TTL=3600
cleanup() { rm -rf "$SB" "$SB_STATE"; }
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
# 15e-ii's fixture (the check explains why). Two repo-root registers — one whose
# NAME carries an ERE-active character (`+`), one plain-named CONTROL — and TWO gate
# sources, each naming exactly ONE of them through a quoted literal, the way
# api/internal/biteharness/inventory_test.go:64 reads BITE-HARNESSES.md. It lives in
# the SANDBOX on purpose: the live tree is never mutated by this self-test.
#
# THEY ARE SEPARATE FILES, and that is load-bearing for the bite. A single source
# naming BOTH names would keep the metachar register's file in the corpus under the
# UNFIXED runner (the plain name matches it), so the corpus search would never be
# shown to be the failing half and the check would pass on a broken runner —
# measured, in the first version of this fixture.
printf '# QA probe\n\nA repo-root register whose name carries an ERE-active character.\n' > "$SB/QA+PROBE.md"
printf '# QA probe (control)\n\nThe same register under a name with no metacharacter.\n' > "$SB/QAPLAIN.md"
mkdir -p "$SB/api/internal/qaprobe"
cat > "$SB/api/internal/qaprobe/probe_test.go" <<'QAPROBE'
package qaprobe

import "testing"

// metacharProbe is read out of the repo root, the shape a real guard uses. THE
// FILE NAMES ONLY THIS ONE NAME: a plain-named sibling in the same file would keep
// this file in the corpus for the wrong reason on a runner that still treats the
// name as a pattern.
const metacharProbe = "QA+PROBE.md"

func TestMetacharProbeIsNamed(t *testing.T) {
	if metacharProbe == "" {
		t.Fatal("empty probe name")
	}
}
QAPROBE
cat > "$SB/api/internal/qaprobe/plain_test.go" <<'QAPLAIN'
package qaprobe

import "testing"

// plainProbe is the CONTROL: same fixture, same reader shape, no metacharacter.
const plainProbe = "QAPLAIN.md"

func TestPlainProbeIsNamed(t *testing.T) {
	if plainProbe == "" {
		t.Fatal("empty probe name")
	}
}
QAPLAIN
git -C "$SB" init -q .
git -C "$SB" add -A
git -C "$SB" -c user.email=t@t -c user.name=t commit -q -m init
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
export PREFLIGHT_LOCK_FILE="$SB_STATE/preflight-selftest.lock"

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
#
#     THE STRIP STOPS AT A COMMENT-INTRODUCING `#` ONLY (card t_9c1c1518). The
#     pattern used to be `s/[[:space:]]*#.*$//` — `[[:space:]]*` is ZERO-or-more, so
#     it truncated at ANY `#`, including one inside `${...}` or mid-token. A line
#     like `if [ ${#FAILED[@]} -gt 0 ]; then exit 1; fi || true` was reduced to
#     `if [ ${` and the `|| true` behind the `#` never reached the pattern below —
#     measured on a disposable copy: the neutered runner reported this check GREEN.
#     Requiring a start-of-line or whitespace-preceded `#` keeps `#` inside a token
#     (parameter expansion, a URL fragment, a colour literal) part of the line.
active="$(sed -E 's/(^|[[:space:]])#.*$//' "$PREFLIGHT" | grep -vE '^[[:space:]]*$')"
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

# 15e. THE TRAP: the gate-INPUT prose files must select the gate that reads them.
#      Each is read by a test, so a change to it can break that gate; a classifier
#      that dropped it would make the run green on a tree whose gate is red.
#      A file is pinned to a SPECIFIC gate: MCP-PARITY.md and BITE-HARNESSES.md are
#      read by the api suite (the `test` gate — mcp_parity_test.go and
#      biteharness/inventory_test.go), GATE-INVENTORY.md and AGENTS.md by
#      scripts/preflight_test.sh (the `selftest` gate), and deploy/backup/README.md
#      by the backup suite (the `backup` gate).
#      BITE-HARNESSES.md is the newest member and it was missed for a while: it is
#      named through a `const` in a Go test, like MCP-PARITY.md, and its name was
#      not in the runner's hand-written list of repo-root files, so a diff touching
#      it alone widened a one-gate run to all 15. It is pinned here so the next
#      regression is RED instead of merely expensive.
trap_specs="MCP-PARITY.md:test GATE-INVENTORY.md:selftest AGENTS.md:selftest deploy/backup/README.md:backup BITE-HARNESSES.md:test"
trap_out=""
for spec in $trap_specs; do
  tfile="${spec%%:*}"; tgate="${spec##*:}"
  if [ ! -e "$LIVE_ROOT/$tfile" ]; then
    bad "gate-INPUT pin not checked: $tfile does not exist at $LIVE_ROOT, so this pin proves nothing"
    continue
  fi
  tsel="$(sel_classify "$tfile")"
  if grep -qx "$tgate" <<<"$tsel"; then
    trap_out="${trap_out}${tfile}->${tgate} ok; "
  else
    bad "$tfile alone must select '$tgate' (it is READ by that gate's test) — got: [$(printf '%s' "$tsel" | tr '\n' ' ')]"
    trap_out="${trap_out}${tfile}->${tgate} MISSING; "
  fi
done
if grep -qv 'MISSING' <<<"$trap_out" && [ -n "$trap_out" ]; then
  ok "the $(printf '%s\n' $trap_specs | wc -l | tr -d ' ') gate-INPUT prose files each select the gate that reads them ($trap_out)"
fi

# 15e-i. The pin above is NOT ENOUGH ON ITS OWN, and this is the half that makes it
#      bite. `test` is IN the full 15-gate set, so `grep -qx test` passes just as
#      happily on a WIDENED run as on a narrowed one: a regression that dropped the
#      claim would widen the run, the gate would still be named, and 15e would stay
#      green while the narrowing it exists to protect was gone. Measured: that is
#      exactly the state BITE-HARNESSES.md was in on 99c6aa79 — `test` named, and
#      all 15 gates selected by `--classify=BITE-HARNESSES.md`.
#      So assert the NARROWED-ness as well. A file whose ONLY reader is a
#      narrowable gate must not drag in the whole inventory.
bite_sel="$(sel_classify 'BITE-HARNESSES.md')"
bite_n="$(grep -cv '^#reason:' <<<"$bite_sel")"
if [ "$bite_n" = "$NGATES" ] || grep -q 'no gate claims' <<<"$bite_sel"; then
  bad "BITE-HARNESSES.md alone is read by the api suite only, so its diff must NARROW to the gates that read it — got the FULL $NGATES-gate set: [$(printf '%s' "$bite_sel" | tr '\n' ' ')]"
else
  ok "BITE-HARNESSES.md alone narrows to $bite_n gates INCLUDING test, not the full $NGATES — a repo-root file named by a gate source is derived as that gate's input"
fi

# 15e-ii. THE METACHARACTER HALF of the same trap, and this is the half that escaped.
#      `pw_root_literals` searches for the root names to find their readers, and the
#      name set comes from the TREE. Building a PATTERN out of a NAME is only correct
#      if every metacharacter in that name is neutralised: the old form escaped a fixed
#      character class `[][\.^$*/]` and left `+ ? ( ) { } |` — all ERE-active — alone,
#      so a root register named `QA+PROBE.md` became the pattern `QA+PROBE\.md`, which
#      matches `QAPROBE.md` (a name that does not exist) and NOT the real file. The
#      corpus came back empty, the awk pass saw no source, and
#      `--classify='QA+PROBE.md'` widened a one-gate run to the full inventory.
#      The fix removes the pattern language rather than escaping it further
#      (`git grep -lF`, one `-e` per name), because the escape's correctness depends on
#      enumerating grep's whole ERE engine and a missing member is silent.
#
#      DRIVEN AGAINST THE SANDBOX COPY. The property needs a tree that CONTAINS the
#      metachar register and a gate source naming it, and this self-test must never
#      mutate the live tree. The sandbox's scripts/preflight.sh is a byte copy of the
#      runner under test (checks 1 and 14 already rely on that), so the reading is
#      about this runner.
metachar_sel="$( (cd "$SB" && ./scripts/preflight.sh --classify='QA+PROBE.md' 2>/dev/null) )"
metachar_n="$(grep -cv '^#reason:' <<<"$metachar_sel")"
metachar_gates="$(grep -v '^#reason:' <<<"$metachar_sel" | sort | tr '\n' ' ')"
if [ "$metachar_n" = "$NGATES" ] || grep -q 'no gate claims' <<<"$metachar_sel"; then
  bad "a repo-root register whose NAME contains an ERE metacharacter ('QA+PROBE.md', read as the quoted literal \"QA+PROBE.md\" by api/internal/qaprobe/probe_test.go) must be DERIVED as its reader's input — got the full $NGATES-gate set / unclaimed: [$(printf '%s' "$metachar_sel" | tr '\n' ' ')]. A NAME used to build a PATTERN must be matched LITERALLY (or its every metacharacter escaped), or the corpus search matches a different filename and this register is never claimed."
elif ! grep -qx 'test' <<<"$metachar_sel"; then
  bad "the metacharacter register QA+PROBE.md must select \`test\` (its only reader is the api suite) — got: [$(printf '%s' "$metachar_sel" | tr '\n' ' ')]"
else
  ok "a metacharacter-bearing repo-root register is derived too: QA+PROBE.md -> $metachar_n gates INCLUDING test, not the full $NGATES ($metachar_gates)"
fi

#      The CONTROL: the same fixture under a plain name must read the SAME way. Without
#      it, \"QA+PROBE.md is claimed\" could hold for a reason unrelated to metacharacters
#      (a fixture that happens to be claimed by something else), and the assertion would
#      pass on a runner that is still name-pattern-sensitive.
plain_sel="$( (cd "$SB" && ./scripts/preflight.sh --classify='QAPLAIN.md' 2>/dev/null) )"
plain_gates="$(grep -v '^#reason:' <<<"$plain_sel" | sort | tr '\n' ' ')"
if [ -z "$metachar_gates" ] || [ "$plain_gates" != "$metachar_gates" ]; then
  bad "the plain-named CONTROL register QAPLAIN.md selects a DIFFERENT gate set from QA+PROBE.md — the two are read by the same source and differ only in their name, so the name is still being treated as a pattern: metachar=[$metachar_gates] plain=[$plain_gates]"
else
  ok "the plain-named control QAPLAIN.md selects the SAME $metachar_n gates — the metacharacter reading is a property of the fix, not of the fixture"
fi

#      NON-VACUITY, and the sharpest half: `QAPROBE.md` is the name the UNESCAPED `+`
#      actually matched and it does not exist in the tree, so it must stay UNCLAIMED and
#      widen. A runner that narrowed it would be claiming a file no gate can read — the
#      fail-closed branch traded for a phantom claim, which is the unsafe direction.
ghost_sel="$( (cd "$SB" && ./scripts/preflight.sh --classify='QAPROBE.md' 2>/dev/null) )"
if grep -q 'no gate claims' <<<"$ghost_sel" && [ "$(grep -cv '^#reason:' <<<"$ghost_sel")" = "$NGATES" ]; then
  ok "QAPROBE.md (does not exist; it is the name the unescaped '\\+' used to match) stays UNCLAIMED and widens to all $NGATES — no phantom claim from the literal match"
else
  bad "QAPROBE.md does not exist in the tree, so it must stay unclaimed and widen to all $NGATES gates — got: [$(printf '%s' "$ghost_sel" | tr '\n' ' ')]"
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
  # PREFLIGHT_RESULT_DIR is pinned to a throwaway dir for THIS invocation (card
  # t_eb6a7567). The stub makes the gate record `test = fail`, and without the pin
  # that RED record lands in the HOST store keyed on the LIVE tree sha — a record
  # carrying the stub's fingerprint (`go=FAKE-GO ARGV`), so it can never be reused
  # and it poisons the reuse evidence for the real tree. The self-test's own
  # private RECORD_DIR is exported much further down (its block starts after this
  # helper), so this nested run inherits the CALLER's store, which is the host
  # default when the selftest gate is driven by hand — measured, 51 such records.
  (cd "$LIVE_ROOT" && env ${1:+API_TEST_TIMEOUT="$1"} \
      PREFLIGHT_RESULT_DIR="$SB_STATE/nested-results" \
      PREFLIGHT_RESULT_TTL=1 \
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
      PREFLIGHT_RESULT_DIR="$SB_STATE/stripped-results" \
      PREFLIGHT_RESULT_TTL=1 \
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
  # PREFLIGHT_RESULT_DIR is pinned to a throwaway dir for THIS invocation: the stub
  # `go` makes this run record `test = fail`, and the record must not land in the
  # store the CALLER pointed the self-test at (card t_eb6a7567 — the same leak as
  # the check-17 helper, found by driving the gate by hand).
  (cd "$LIVE_ROOT" && env API_TEST_TIMEOUT="$1" PATH="$TIMEBIN:$PATH" \
      PREFLIGHT_RESULT_DIR="$SB_STATE/budget-results" \
      PREFLIGHT_RESULT_TTL=1 \
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
# EVERY SPELLING OF ZERO turns the alarm off, not only the bare `0` (card t_fb782d12).
# The guard compared against the literal string "0", so `0s`, `0m` and `0h` all passed
# it and `go test` was handed `-timeout 0s` — a genuinely hung package would block the
# gate with no bound instead of failing. Measured: the running gate's argv really was
# `go test ./... -count=1 -timeout 0s`.
zero_leaks=""
for z in 0s 0m 0h 00; do
  zrc="$(test_rc_and_argv "$z" | cut -f1)"
  if [ "$zrc" = 0 ] || ! grep -q "disables go test's timeout alarm" "$SB/budget.out" || \
     grep -q 'FAKE-GO ARGV' "$SB/budget.out"; then
    zero_leaks="${zero_leaks}[$z -> rc=$zrc $(sed -n 's/^FAKE-GO ARGV: /argv: /p' "$SB/budget.out" | sed -n '1p')] "
  fi
done
if [ -z "$zero_leaks" ]; then
  ok "every spelling of a ZERO API_TEST_TIMEOUT (0s, 0m, 0h, 00) is refused by the budget check — none reaches \`go test\` with the alarm disabled"
else
  bad "a zero API_TEST_TIMEOUT in another spelling was NOT refused, so \`go test\` ran with its timeout alarm OFF — the gate would hang instead of failing: $zero_leaks"
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

# 15-20. THE PER-TREE RESULT RECORD (scripts/gate_result.sh, consumed by
#        scripts/preflight_reuse.sh and written by scripts/preflight.sh).
#
# WHY THESE ARE HERE AND NOT IN A GO TEST: the record is a shell artefact of the
# gate runner; the properties below are what make it safe to skip work, and every
# one of them is a property that FAILS LOUDLY rather than degrading quietly. The
# whole feature is "do not spend host time re-gating a tree whose verdict is
# known", so its failure mode is a STALE OR WRONG VERDICT BEING BELIEVED — the
# worst possible failure for a gate. Each check below was written after breaking
# the corresponding property by hand and watching it go red.
#
# The private result dir + TTL are pinned at $SB_STATE, near the top of this file,
# so that EVERY nested runner invocation — not only the ones below — is inside the
# sandbox. See the pin's own comment for the measurements (t_eb6a7567, t_a50d854d).
export PREFLIGHT_REUSE_REPO="$SB"

# A gate name to vouch for, taken from the derived inventory (never hardcoded).
probe_gate="$(printf '%s\n' "$GATES" | sed -n '1p')"

# THE CHECKS BELOW CONTROL THEIR OWN FINGERPRINT. Every invocation passes
# PREFLIGHT_KEY_EXTRA explicitly, but a caller may have exported one already (a
# lane that runs `PREFLIGHT_KEY_EXTRA=<stamp> make preflight`), and an ambient
# value would silently change the key the checks compute and make 16b red for a
# reason that has nothing to do with the code. Pinned here so the checks are a
# property of the script, not of the caller's shell.
export PREFLIGHT_KEY_EXTRA=""

# 18. A completed run WRITES a record keyed on the TREE sha, naming the tree it
#     gated. This is the card's acceptance #1 and the thing every other check
#     depends on. `--only <one gate>` is used so the check is fast; it must still
#     finish the run, which is what "completed" means here.
. "$HERE/gate_result.sh"
probe_tree="$(gr_tree_sha "$SB")"
if [ -z "$probe_tree" ]; then
  bad "cannot compute a sandbox tree sha — the record checks below cannot run"
else
  rec="$(gr_file "$probe_tree")"
  sb_run=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" 2>&1)
  if [ -r "$rec" ] && [ "$(gr_read_field "$rec" tree)" = "$probe_tree" ]; then
    ok "a completed run wrote a record keyed on the TREE sha ($(printf '%.7s' "$probe_tree")), naming the tree it gated"
  else
    bad "no record keyed on the tree sha after a completed run — expected $rec naming [$probe_tree], got tree=[$(gr_read_field "$rec" tree)]"
  fi
fi

# 19. The record is INVALIDATED when the ENVIRONMENT changes, not just the tree.
#     A tree sha alone would hand a verdict observed under one toolchain, one web
#     dependency set or one gate script to a tree it never described.
#
#     The bump is applied through PREFLIGHT_KEY_EXTRA, the documented toolchain
#     stamp, rather than by editing preflight.sh: the script is a TRACKED file, so
#     editing it changes the tree sha as well, and the refusal would then come from
#     the tree check instead of the fingerprint — the check would go red (it does)
#     but for the wrong reason, which is not evidence for this property. The stamp
#     changes the fingerprint and NOTHING else, so a refusal here can only be the
#     fingerprint.
#
#     16b then pins the other half: the gate script's own hash must be one of the
#     components, so "edit preflight.sh" (the card's example) does change the
#     fingerprint. Without 16b a reader could not tell from 16 alone whether the
#     mechanism covers the script or only the stamp.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "pass", "seconds": 5, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "green",\n  "finished_at": "%s"\n}\n' \
    "$probe_tree" "$key" "$parts" "$probe_gate" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$rec"
  before="$(PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  after="$(PREFLIGHT_KEY_EXTRA=probe16-bumped gr_key "$SB")"
  sb_reuse=$(PREFLIGHT_KEY_EXTRA=probe16-bumped ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  sb_reuse_rc=$?
  if [ "$before" != "$after" ] && [ $sb_reuse_rc -ne 0 ] && \
     grep -q 'the environment changed since that record' <<<"$sb_reuse"; then
    ok "a changed environment fingerprint (toolchain stamp) is REFUSED, not reused (rc=$sb_reuse_rc)"
  else
    bad "an environment change must invalidate the record — key before=[$before] after=[$after] rc=$sb_reuse_rc [$(printf '%s' "$sb_reuse" | tail -n 1)]"
  fi
  # 16b. The fingerprint must actually CONTAIN the components check 19 relies on.
  #      Without this, dropping a component makes 16 vacuous rather than red: if
  #      `extra=` leaves the parts line, bumping the stamp changes nothing, and a
  #      stale pass is handed to an environment it never described. So the key is
  #      asserted to CHANGE when the stamp changes, and the two gate-script hashes
  #      are asserted to be the components named in the parts line.
  parts_now="$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")"
  key_with="$(PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  key_bumped="$(PREFLIGHT_KEY_EXTRA=probe15-bumped gr_key "$SB")"
  key_unset="$(gr_key "$SB")"
  script_hash="$(gr_sha256_file "$SB/scripts/preflight.sh")"
  lib_hash="$(gr_sha256_file "$SB/scripts/gate_result.sh")"
  if [ -n "$key_with" ] && [ "$key_with" != "$key_bumped" ] && [ "$key_unset" != "$key_with" ] && \
     [ "$script_hash" = "$(printf '%s' "$parts_now" | sed -nE 's/.*gatescript=([0-9a-f]+).*/\1/p')" ] && \
     [ "$lib_hash" = "$(printf '%s' "$parts_now" | sed -nE 's/.*resultlib=([0-9a-f]+).*/\1/p')" ]; then
    ok "the fingerprint contains the toolchain stamp and the hashes of scripts/preflight.sh + scripts/gate_result.sh"
  else
    bad "the fingerprint must contain the stamp and the gate-script hashes — unset=[$key_unset] stamp=[$key_with] bumped=[$key_bumped] script_hash=$script_hash lib_hash=$lib_hash parts=[$parts_now]"
  fi
fi

# 20. REUSE MUST NEVER TURN A RED INTO A GREEN. A record that holds a `fail` for
#     the very gate being asked about must refuse, even though the record names
#     this tree under this fingerprint. Written by hand rather than by running a
#     failing gate, so the check is about the READER's rule and stays fast — and
#     the hand-written record also pins the on-disk shape a future writer must
#     keep producing.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "fail", "seconds": 3, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "red",\n  "finished_at": "%s"\n}\n' \
    "$probe_tree" "$key" "$parts" "$probe_gate" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$rec"
  out=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  rc=$?
  if [ $rc -ne 0 ] && grep -q 'can never turn a red verdict green' <<<"$out"; then
    ok "a recorded FAIL is refused even on the matching tree+fingerprint (a red is never reused into a green)"
  else
    bad "a record holding a fail must be refused, got rc=$rc [$(printf '%s' "$out" | tail -n 1)]"
  fi
fi

# 21. A record for ANOTHER tree must not be usable, even when everything else
#     about it matches. This is the merge-tree vs branch-head case the card names
#     (2f961d7 vs 4bb8eae): the record has to name what it GATED.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  other="$(printf '0%.0s' $(seq 1 40))"
  sed -i "s/^  \"tree\": \"$probe_tree\",$/  \"tree\": \"$other\",/" "$rec"
  out=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  rc=$?
  if [ $rc -ne 0 ] && grep -q "names tree \[$other\]" <<<"$out"; then
    ok "a record naming a DIFFERENT tree is refused ($(printf '%.7s' "$probe_tree") vs $other)"
  else
    bad "a record for another tree must be refused, got rc=$rc [$(printf '%s' "$out" | tail -n 1)]"
  fi
fi

# 22. A stale record must be refused — "do not let a cached pass outlive its
#     usefulness". The TTL is read from PREFLIGHT_RESULT_TTL (the same variable
#     the runner honours), so this measures the real horizon logic and not a
#     hardcoded window.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  out=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_RESULT_TTL=1 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  rc=$?
  if [ $rc -ne 0 ] && grep -q 'older than the 1s horizon' <<<"$out"; then
    ok "a record past its TTL is refused (a cached pass does not outlive its usefulness)"
  else
    bad "a stale record must be refused, got rc=$rc [$(printf '%s' "$out" | tail -n 1)]"
  fi
fi

# 23. THE HAPPY PATH, MEASURED RATHER THAN ASSERTED. With a record that vouches
#     green for the WHOLE inventory, `--reuse-record` must (a) exit 0 without
#     executing a single gate — acceptance #2 of the card: a lane about to gate an
#     already-green tree does not queue behind the flock — (b) report the result as
#     REUSE and NOT as PASS, and (c) append to the reuse ledger, the same ledger
#     scripts/preflight_reuse.sh --report sums for the card's "measured saving".
#     A green that is not distinguished from a re-run, or a saving nobody can
#     count, is the feature not working.
#
#     The record is hand-written rather than produced by running the gates: the
#     sandbox tree cannot build, and not needing to build is the entire reason the
#     record exists. It is written for EVERY gate in the derived inventory, which
#     is what makes this the FULL path rather than the per-gate one.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
      "$probe_tree" "$key" "$parts"
    # One entry per gate in the derived inventory, comma-separated, no trailing
    # comma — built with sed so no subshell loses the last line.
    # shellcheck disable=SC2086  # deliberate word splitting on the newline-separated inventory
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  # The hand-written gates block is the fixture for the whole check, so assert it
  # parses as intended BEFORE concluding anything from the run below: a fixture
  # that silently lost its entries would make this check pass for no reason.
  fixture_green=0
  # shellcheck disable=SC2086
  for g in $GATES; do
    [ "$(gr_gate_status "$rec" "$g")" = "pass" ] && fixture_green=$((fixture_green+1))
  done
  rm -f "$RECORD_DIR/reuse-ledger.jsonl"
  out=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --reuse-record 2>&1)
  rc=$?
  reused_secs=$(( fixture_green * 42 ))
  # THE STRONGEST ASSERTION AVAILABLE: `gate [` is the banner the runner prints
  # for every gate it EXECUTES. Its total absence proves acceptance #2 directly —
  # no gate ran, so nothing queued for, or held, the host-wide flock.
  #
  # The reuse arrives by the PRE-LOCK path (every gate of the inventory is vouched
  # for, so it is taken before `flock`), which is why the ledger line says
  # `mode: full` and the seconds are the ledgered saving. If a future change moves
  # the decision back under the lock, check 28 goes red; this check would still pass
  # on the verdict, so the two are not redundant.
  if [ "$fixture_green" -eq "$NGATES" ] && [ $rc -eq 0 ] && \
     ! grep -q 'gate \[' <<<"$out" && \
     grep -q 'verdict: REUSED (green)' <<<"$out" && \
     grep -q "remembered ${reused_secs}s" <<<"$out" && \
     grep -q 'REUSE' <<<"$out" && \
     [ -r "$RECORD_DIR/reuse-ledger.jsonl" ] && \
     grep -q "\"tree\":\"$probe_tree\"" "$RECORD_DIR/reuse-ledger.jsonl" && \
     grep -q "\"mode\":\"full\"" "$RECORD_DIR/reuse-ledger.jsonl" && \
     grep -q "\"remembered_seconds\":$reused_secs" "$RECORD_DIR/reuse-ledger.jsonl"; then
    ok "a whole-inventory green record is REUSED without executing a single gate (no 'gate [' banner), labelled as reuse, $reused_secs host-seconds on the ledger"
  else
    bad "a green record for the whole inventory must be reused without executing anything and ledgered — fixture_green=$fixture_green/$NGATES rc=$rc gates_run=[$(printf '%s' "$out" | grep -c 'gate \[')] [$(printf '%s' "$out" | grep -E 'reuse:|REUSED|FAIL' | tr '\n' ' ')] ledger=[$(printf '%s' "$(cat "$RECORD_DIR/reuse-ledger.jsonl" 2>/dev/null)" | tr '\n' ' ')]"
  fi

  # 20b. The ledger is READ by the consumer, so the MEASURED saving can be
  #      reported from real files. Assert the report sums the seconds just written
  #      — not that some number is printed.
  rep=$(./scripts/preflight_reuse.sh --report 24 2>&1)
  if grep -q "host-seconds NOT spent : ${reused_secs}s" <<<"$rep"; then
    ok "--report sums the real ledger (${reused_secs}s from the reuse above)"
  else
    bad "--report must sum the real ledger, got [$(printf '%s' "$rep" | grep -E 'host-seconds|reuse events' | tr '\n' ' ')] expected ${reused_secs}s"
  fi

  # 20c. AND the consumer's saving must be on the ledger too. A lane's normal path
  #      is `preflight_reuse.sh` first and the runner second; if only the runner
  #      ledgered, the documented path would report a saving of zero. Counted as a
  #      SEPARATE line so the check measures the consumer's own write.
  rm -f "$RECORD_DIR/reuse-ledger.jsonl"
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight_reuse.sh --tree "$probe_tree" >/dev/null 2>&1
  if [ -r "$RECORD_DIR/reuse-ledger.jsonl" ] && \
     grep -q "\"remembered_seconds\":$reused_secs" "$RECORD_DIR/reuse-ledger.jsonl"; then
    ok "the consumer's decision also ledgers its ${reused_secs}s saving (the lane path is counted, not just the runner's)"
  else
    bad "the consumer must ledger the saving it reports, ledger=[$(printf '%s' "$(cat "$RECORD_DIR/reuse-ledger.jsonl" 2>/dev/null)" | tr '\n' ' ')]"
  fi
fi

# 24. The consumer must AGREE with the runner ON THE SAME QUESTION, and it must
#     answer the question it was actually asked. Both are asked about the one gate
#     (`--only $probe_gate`, the shape a lane uses to re-run what a record could
#     not vouch for); the consumer is told the same selection through
#     PREFLIGHT_ONLY. If it ignored that and answered over the whole inventory it
#     would call a run "partial" that the runner calls green (or hand a lane a
#     RERUN set for gates nobody asked about) — a consumer and a runner that
#     disagree about what is reusable is exactly how a stale pass gets believed.
if [ -n "$probe_tree" ]; then
  dec=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_ONLY="$probe_gate" ./scripts/preflight_reuse.sh --tree "$probe_tree" 2>&1)
  dec_rc=$?
  run=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  run_rc=$?
  # The runner has TWO reuse paths — the whole-inventory one (which takes the early
  # exit and prints `verdict: REUSED (green)`) and the per-gate one (which prints
  # `==> REUSED (not re-run, recorded pass on THESE bytes): <gates>`). Which one
  # fires depends on how much of the inventory the record covers, so the assertion
  # is what they must AGREE ON: the consumer calls $probe_gate reusable with no
  # reruns, and the runner reuses rather than gating. Any stronger coupling here
  # would pin a branch instead of the contract.
  dec_ok=0
  [ $dec_rc -eq 0 ] && \
    grep -qxF "PREFLIGHT_DECISION=full" <<<"$dec" && \
    grep -qxF "PREFLIGHT_TREE=$probe_tree" <<<"$dec" && \
    grep -qxF "PREFLIGHT_SKIP=$probe_gate" <<<"$dec" && \
    grep -qxF "PREFLIGHT_RERUN=" <<<"$dec" && dec_ok=1
  if [ "$dec_ok" -eq 1 ] && [ $run_rc -eq 0 ] && \
     grep -q 'REUSED' <<<"$run" && ! grep -q 'gate \[' <<<"$run"; then
    ok "the consumer answers the question it was asked ($probe_gate: full, no reruns) and the runner reuses rather than gating"
  else
    bad "the consumer and the runner must agree on what is reusable — consumer rc=$dec_rc [$(printf '%s' "$dec" | tr '\n' ' ')] runner rc=$run_rc [$(printf '%s' "$run" | grep -E 'REUSED|reuse:|FAIL' | tr '\n' ' ')]"
  fi
fi

# 25. A FAILED gate is RECORDED — the record is not a "green runs only" log. This
#     is the case the record matters most for: 148 of the 206 repeated runs
#     measured on this host were re-gating a RED tree (21.3 of the 38.3
#     host-hours), and per-gate reuse is what recovers that time. It only works if
#     a failure leaves behind the passes from the gates that DID succeed, so the
#     next attempt re-runs the failures and not the whole tree.
#
#     In the sandbox every gate fails fast on a missing prerequisite, so the run
#     below is a real red run: it must exit non-zero (a red must stay red) AND
#     still leave a record naming this tree with the failed gate marked `fail`.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  rm -f "$rec"
  out=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" 2>&1)
  rc=$?
  recorded="$(gr_gate_status "$rec" "$probe_gate")"
  if [ $rc -ne 0 ] && [ "$recorded" = "fail" ] && [ "$(gr_read_field "$rec" tree)" = "$probe_tree" ]; then
    ok "a FAILED gate exits non-zero and is still recorded as fail on the tree (the red-tree case can be reused per-gate)"
  else
    bad "a failed gate must be recorded as fail for the tree — rc=$rc recorded=[$recorded] rec=${rec} [$(printf '%s' "$out" | tail -n 1)]"
  fi
fi

# 26. `reused: true` MEANS "AN EARLIER RUN OBSERVED THIS", and nothing else. A gate
#     that no run ever observed must stay `skip` with `reused: false`; otherwise
#     the record claims a memory it does not have, and a lane reading `reused: true`
#     on a gate nobody ever ran is exactly the misreading this card exists to
#     remove. The same run also pins the tally: `skipped` must count the gates the
#     run did not execute, or the record's own summary contradicts its gate map.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  rm -f "$rec"
  # Two runs over the same single gate. In the sandbox that gate FAILS (missing
  # prerequisite), which is fine — this check is about the FLAGS, not the verdict,
  # and a red run is the more interesting case: it must still not mark an
  # unobserved gate as remembered.
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  observed="$(gr_gate_status "$rec" "$probe_gate")"
  observed_reused="$(gr_gate_entry_field "$rec" "$probe_gate" reused)"
  unseen="$(printf '%s\n' $GATES | grep -vx "$probe_gate" | sed -n 1p)"
  unseen_status="$(gr_gate_status "$rec" "$unseen")"
  unseen_reused="$(gr_gate_entry_field "$rec" "$unseen" reused)"
  tally="$(gr_read_int "$rec" skipped)"
  expected_skipped=$(( NGATES - 1 ))
  if [ "$observed_reused" = "false" ] && [ "$unseen_status" = "skip" ] && [ "$unseen_reused" = "false" ] && \
     [ "$tally" = "$expected_skipped" ]; then
    ok "an unobserved gate stays skip/reused=false and an observed one reused=false; the tally records $tally skipped of $NGATES"
  else
    bad "reused:true must mean 'an earlier run observed this' — observed($probe_gate)=$observed reused=$observed_reused unseen($unseen)=$unseen_status reused=$unseen_reused recorded_skipped=[$tally] expected=$expected_skipped"
  fi
fi

# 27. A REUSED GATE MUST NOT BE REPORTED AS A GAP. The output is where a lane
#     reads the verdict, so the failure mode is a *readable* contradiction: the
#     run prints `REUSE: tmpkey,prunewt` and, three lines later, lists those same
#     gates under `STILL SKIPPED AND NOT REUSED (no recorded pass)` — and under
#     `SKIP: ... (not evidence)` in the summary. Both lines are about gates with
#     NO evidence, so naming a just-vouched-for gate there inverts the meaning of
#     the report while every count still looks right.
#
#     Measured on the real worktree before this check existed: a 3-gate reuse
#     printed `REUSE: tmpkey prunewt refguard` and then
#     `STILL SKIPPED AND NOT REUSED (no recorded pass): build vet test ... refguard selftest`
#     — the full inventory, reused gates included.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  # A PARTIAL fixture: every gate green EXCEPT the first, which is `fail`. A `fail`
  # makes the record unusable as a whole-inventory vouch (so the early full path is
  # not taken) while leaving the other gates as recorded passes — which is exactly
  # the PER-GATE path that prints both the REUSE line and the summary, i.e. the
  # only shape where the contradiction can appear.
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$probe_tree" "$key" "$parts"
    # shellcheck disable=SC2086  # deliberate word splitting on the newline-separated inventory
    printf '%s\n' $GATES | while IFS= read -r g; do
      if [ "$g" = "$probe_gate" ]; then st=fail; else st=pass; fi
      printf '    "%s": { "status": "%s", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },\n' "$g" "$st"
    done | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "red",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  # The fixture must really be partial, or this check silently tests the full path.
  fx_pass=0
  # shellcheck disable=SC2086
  for g in $GATES; do [ "$(gr_gate_status "$rec" "$g")" = pass ] && fx_pass=$((fx_pass+1)); done
  r1=$(PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --reuse-record 2>&1)
  # Lines that name gates but mean "no evidence", and the line that means "remembered".
  gap_lines="$(printf '%s\n' "$r1" | grep -E 'STILL SKIPPED AND NOT REUSED|SKIP:.*not evidence' || true)"
  reuse_line="$(printf '%s\n' "$r1" | grep -E 'REUSE:' || true)"
  leaked=""
  # shellcheck disable=SC2086  # deliberate word splitting on the derived inventory
  for g in $GATES; do
    case "$reuse_line" in
      *"$g"*) printf '%s\n' "$gap_lines" | grep -qw "$g" && leaked="$leaked $g" ;;
    esac
  done
  if [ "$fx_pass" -ne $((NGATES-1)) ]; then
    bad "check 27's fixture is not partial ($fx_pass of $NGATES pass) — it would test the full path, not the per-gate one"
  elif [ -n "$leaked" ]; then
    bad "a reused gate was also printed under a 'no evidence' line — reused but listed as a gap:$leaked"
  elif [ -z "$reuse_line" ]; then
    bad "a per-gate reuse run printed no REUSE line at all (last output was [$(printf '%s' "$r1" | tail -n 1)])"
  else
    ok "on the per-gate path a reused gate is named on the REUSE line ($fx_pass reused) and never under a 'no recorded pass' / 'not evidence' line"
  fi
fi

# 28. A WHOLE-INVENTORY REUSE MUST NOT TAKE THE HOST LOCK — the runner's reason for
#     existing. Check 20 proves no GATE runs, but it runs under the self-test's
#     PRIVATE lock ($PREFLIGHT_LOCK_FILE points into $SB_STATE), so it can never see
#     whether the gate body had to WAIT. Measured on this host with a real record:
#     the reuse sat in `flock` for the full lock wait (151s, killed) because the
#     reuse decision was taken INSIDE the lock — re-gating nothing, but queueing
#     behind whatever held it, which is exactly the cost the record removes.
#
#     This check holds the lock for real (a background flock on the SAME path the
#     script would use) and requires the reuse to answer WITHOUT waiting. The lock
#     is a private path, so the check is hermetic and never touches the host's gate.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$probe_tree" "$key" "$parts"
    # shellcheck disable=SC2086
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  held_lock="$SB_STATE/held-gate.lock"
  : > "$held_lock"
  # Hold it for the whole check. `-w 5` so an unexpected failure to take it is
  # visible rather than hanging the self-test.
  flock -w 5 "$held_lock" sleep 60 &
  holder=$!
  sleep 1
  if flock -n "$held_lock" true 2>/dev/null; then
    bad "check 28 could not hold its lock ($held_lock) — the no-wait property cannot be measured"
  else
    # A wait of 0 makes "did it queue?" binary: if the reuse still reaches the
    # lock it exits 75 instead of answering, so the check cannot hang.
    t0=$(date +%s)
    r2=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_FILE="$held_lock" PREFLIGHT_LOCK_WAIT=0 \
         ./scripts/preflight.sh --reuse-record 2>&1)
    r2_rc=$?
    t1=$(date +%s)
    if [ "$r2_rc" -eq 0 ] && grep -q 'took NO lock' <<<"$r2" && ! grep -q 'could not acquire' <<<"$r2"; then
      ok "a whole-inventory reuse answers WITHOUT taking the gate lock (rc=0 in $((t1-t0))s while the lock was held)"
    else
      bad "a whole-inventory reuse must not queue on the gate lock — rc=$r2_rc in $((t1-t0))s, output=[$(printf '%s' "$r2" | grep -E 'lock|could not acquire|REUSED' | tr '\n' ' ')]"
    fi
  fi
  kill "$holder" 2>/dev/null || true
  wait "$holder" 2>/dev/null || true
fi

# 29. A SUBSET SELECTION MUST NOT BORROW A WHOLE-INVENTORY REUSE. With a green
#     record and `--only <one gate>`, the pre-lock path must ask about THE GATE
#     THE CALLER SELECTED. Asking about the whole inventory instead reports a
#     14-gate reuse — and ledgers 14 gates' worth of seconds as saved — for a
#     one-gate request. Measured on the real worktree before this check existed:
#     `--only vet --reuse-record` printed
#     `REUSE: build vet test govuln web a11y npmaudit gitleaks monitor tmpkey
#      backup prunewt refguard selftest (REMEMBERED ...)`
#     for a run that only ever asked about `vet`.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$probe_tree" "$key" "$parts"
    # shellcheck disable=SC2086
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 77, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  r3=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  r3_rc=$?
  r3_reuse="$(printf '%s\n' "$r3" | grep -E 'REUSE:' | tail -n 1)"
  # The reused set must be exactly $probe_gate: no other gate may be named as
  # remembered, and the saved seconds must be this gate's alone (77), not the sum.
  leaked_gates=""
  # shellcheck disable=SC2086
  for g in $GATES; do
    [ "$g" = "$probe_gate" ] && continue
    case "$r3_reuse" in *"$g"*) leaked_gates="$leaked_gates $g" ;; esac
  done
  case "$r3" in
    *"remembered 77s"*) secs_ok=1 ;;
    *)                  secs_ok=0 ;;
  esac
  if [ "$r3_rc" -eq 0 ] && [ -z "$leaked_gates" ] && [ "$secs_ok" -eq 1 ] && \
     printf '%s\n' "$r3_reuse" | grep -qw "$probe_gate"; then
    ok "a --only subset reuses only the selected gate ($probe_gate, 77s) — not the whole inventory"
  else
    bad "a subset selection must not borrow a whole-inventory reuse — rc=$r3_rc leaked=[$leaked_gates] secs_ok=$secs_ok line=[$r3_reuse]"
  fi
fi

# 30. THE SELF-TEST MUST NOT INHERIT THE RUNNER'S LOCK FLAG — see the guard at the
#     top of this file. `preflight.sh` exports `_PREFLIGHT_LOCKED=1` to its flock
#     child, and this self-test runs as that child's own `selftest` gate, so every
#     nested `./scripts/preflight.sh` here would inherit "you are the locked child"
#     and skip the pre-lock reuse — measuring a path no lane ever takes. Measured for
#     real: the full gate's selftest run failed checks 28 and 29 while
#     `bash scripts/preflight_test.sh` passed them, because only the nested run had
#     the flag set.
#
#     This assertion BITE only in the nested case, which is exactly the case that
#     broke: run standalone (no parent flag) it passes trivially, but run as the gate
#     runs it with the guard deleted, the flag is still set here and it goes red.
if [ -n "${_PREFLIGHT_LOCKED:-}" ]; then
  bad "_PREFLIGHT_LOCKED is still set when the checks run — this self-test inherited the runner's flag, so checks 28/29 measured a path no lane takes (is the guard at the top of this file still there?)"
else
  ok "the self-test clears an inherited _PREFLIGHT_LOCKED before any check (nested preflight.sh calls take the real pre-lock reuse path)"
fi

# 30b. THE SECOND INHERITED HANDOFF, asserted the same way and for the same reason
#      as the one above. `--for-diff` exports SEL_DIFF (plus _PREFLIGHT_SEL and its
#      companions) for its own flock child, and `selected()` reads SEL_DIFF BEFORE
#      `--only`/`--skip` — so a self-test that inherits it silently runs the OUTER
#      narrowing instead of the gate each check asks for. Measured: 30 of 69 checks
#      red on a `--for-diff origin/main` run of the same bytes that were 69/69 green
#      unnarrowed (tree 0244c928 vs tree b43789be's `selftest: pass`). The nested
#      assertions here cannot see it (they run `preflight.sh --only …`, and an
#      inherited SEL_DIFF wins over --only), which is exactly why this one is
#      explicit rather than inferred from a downstream check.
#
#      It BITES in the nested case and only there: run standalone the variable was
#      never set and it passes trivially; delete the guard at the top of this file
#      and run it as the gate runs it, and this is red naming the leak.
if [ -n "${SEL_DIFF:-}" ] || [ -n "${_PREFLIGHT_SEL:-}" ]; then
  bad "the path-aware-selection handoff is STILL SET when the checks run (SEL_DIFF=[${SEL_DIFF:-}] _PREFLIGHT_SEL=[${_PREFLIGHT_SEL:-}]) — selected() reads SEL_DIFF before --only/--skip, so every nested check would run an OUTER narrowing instead of the gate it asks for, and the per-tree-record checks would write and read nothing. Is the guard at the top of this file still there?"
else
  ok "the self-test clears an inherited --for-diff selection handoff (SEL_DIFF / _PREFLIGHT_SEL) before any check"
fi

# 30c. AN EXPLICIT --only/--skip BEATS AN INHERITED SELECTION (card t_fef2c0f7).
#
#      `selected()` reads SEL_DIFF BEFORE --only/--skip, and a `--for-diff` run
#      exports it for its own flock child. This self-test clears the handoff for its
#      OWN checks (the guard at the top of this file, asserted by 30b), but the
#      runner it drives can still be entered as a DESCENDANT of a --for-diff run by
#      anyone else — a lane's `selftest` gate, or a hand-run check — and in that
#      position every nested `./scripts/preflight.sh --only X` ran the OUTER
#      narrowing instead of X, and `--skip <all 15>` did not reach the all-skipped
#      refusal at all: it ran the inherited gates, real `test` included. Measured:
#      the `selftest` leg of a `--for-diff origin/main` run nested a real
#      `go test ./... -race -timeout 30m` (a full internal/httpapi.test) several
#      levels down and hung ~45 minutes, while the SAME self-test driven directly
#      finished in under a minute.
#
#      The two directions are asserted against the LIVE runner, in a private lock so
#      this cannot queue on the host gate:
#        * `--skip <every gate>` with the handoff inherited must be REFUSED (the
#          empty run is not a pass) — on the unfixed runner it starts `[build]`;
#        * `--only <gate>` with the handoff inherited must run EXACTLY that gate.
if [ -n "${PREFLIGHT_REUSE_REPO:-}" ]; then
  _s30c_lock="$SB_STATE/s30c.lock"
  # ISOLATION: these probes must not read the sandbox's record store or write a
  # reuse line into the ledger the checks below count (measured: sharing them made
  # the red-tree / ledger / node-version checks red, and sent the --skip probe down
  # the pre-lock reuse path so it never reached the refusal at all).
  _s30c_state="$SB_STATE/s30c-state"; rm -rf "$_s30c_state"; mkdir -p "$_s30c_state"
  # `$GATES` is the inventory this file already read from `--list` (one name per
  # line); join it with commas for --skip. Using a name that does not exist here
  # would silently pass an EMPTY filter, which selects everything — the opposite of
  # what this probe needs.
  _s30c_all="$(printf '%s,' $GATES)"; _s30c_all="${_s30c_all%,}"
  _s30c_skip_out="$(timeout 60 env \
      PREFLIGHT_LOCK_FILE="$_s30c_lock" PREFLIGHT_LOCK_WAIT=0 \
      PREFLIGHT_RESULT_DIR="$_s30c_state" PREFLIGHT_RESULT_TTL=1 \
      PREFLIGHT_REUSE_REPO="$SB_STATE/s30c-repo" \
      _PREFLIGHT_LOCKED=1 _PREFLIGHT_SEL=1 \
      SEL_DIFF=' build vet test govuln webimage gitleaks prunewt selftest ' \
      SEL_COUNT=8 SEL_DIFF_FULL=0 SEL_BASE=origin/main \
      ./scripts/preflight.sh --skip "$_s30c_all" 2>&1)"; _s30c_skip_rc=$?
  _s30c_only_out="$(timeout 180 env \
      PREFLIGHT_LOCK_FILE="$_s30c_lock" PREFLIGHT_LOCK_WAIT=0 \
      PREFLIGHT_RESULT_DIR="$_s30c_state" PREFLIGHT_RESULT_TTL=1 \
      PREFLIGHT_REUSE_REPO="$SB_STATE/s30c-repo" \
      _PREFLIGHT_LOCKED=1 _PREFLIGHT_SEL=1 \
      SEL_DIFF=' build vet test govuln webimage gitleaks prunewt selftest ' \
      SEL_COUNT=8 SEL_DIFF_FULL=0 SEL_BASE=origin/main \
      ./scripts/preflight.sh --only build 2>&1)"; _s30c_only_rc=$?
  _s30c_only_ran="$(printf '%s\n' "$_s30c_only_out" | grep -oE '^==> gate \[[a-z]+\]' | sort -u | tr '\n' ' ')"
  # The card's property is WHICH GATES RUN, not the selected gate's verdict: the
  # verdict depends on the surrounding environment (a shell without the test-env
  # file fails the prerequisite check before any gate body), and asserting on it
  # would make this check red for a reason that has nothing to do with the
  # inherited selection. What must hold is that exactly the named gate ran — on the
  # unfixed runner this leg runs the eight inherited gates, `test` among them.
  if [ "$_s30c_skip_rc" -ne 1 ] || ! grep -q 'every gate was skipped' <<<"$_s30c_skip_out"; then
    bad "30c does not bite: with SEL_DIFF inherited, \`--skip <every gate>\` was NOT refused (rc=$_s30c_skip_rc) — it ran the inherited narrowing instead of the empty-run refusal, which is the recursion this check exists to stop. First gate line: [$(printf '%s\n' "$_s30c_skip_out" | grep -m1 '==> gate')]"
  elif [ "${_s30c_only_ran% }" != "==> gate [build]" ]; then
    bad "30c does not bite: with SEL_DIFF inherited, \`--only build\` ran [${_s30c_only_ran% }] — an explicit --only must run EXACTLY the gate it names, not the outer narrowing (rc=$_s30c_only_rc)"
  else
    ok "30c — an explicit --only/--skip beats an inherited --for-diff selection: with SEL_DIFF inherited, \`--skip <every gate>\` is REFUSED as an empty run and \`--only build\` runs exactly [build]"
  fi
fi

# 31. THE DOCUMENTED RED-TREE PATH MUST RUN. `docs/PREFLIGHT-REUSE.md` tells a lane
#     whose run was red to re-run exactly what the record could not vouch for:
#
#         ./scripts/preflight.sh --only <PREFLIGHT_RERUN> --reuse-record
#
#     On a record whose only `fail` IS that gate, every recorded `pass` is off the
#     selection, and the runner used to filter the vouched set through the selection
#     BEFORE asking "does this record vouch for anything?". The filtered set came
#     out empty, and the run DIED with "vouches for NO gate": rc=1, zero ledger
#     lines, on the exact lane shape (148 of the 206 repeated runs measured on this
#     host) the record exists for. Measured on the real record for tree 53854bd:
#     without `--only` it ran `vet` once and carried the other thirteen; with the
#     documented `--only vet` it refused.
#
#     THE FIXTURE IS CHECK 27's, reused verbatim: every gate `pass` except
#     $probe_gate, which is `fail`. That is the shape both failure modes need (the
#     early whole-inventory path declines, so the per-gate path is taken).
#
#     WHY CHECK 24 DOES NOT ALREADY COVER THIS: 24 runs the same `--only <gate>
#     --reuse-record` argv, but against a record where THAT GATE IS `pass` — so the
#     selection and the vouched set overlap and the empty-intersection branch is
#     never reached. This check is the one that would have caught the defect.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$probe_tree" "$key" "$parts"
    # shellcheck disable=SC2086  # deliberate word splitting on the newline-separated inventory
    printf '%s\n' $GATES | while IFS= read -r g; do
      if [ "$g" = "$probe_gate" ]; then st=fail; else st=pass; fi
      printf '    "%s": { "status": "%s", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },\n' "$g" "$st"
    done | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "red",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  # fixture sanity, same rule as check 27: a fixture that lost its entries would
  # make this check pass for no reason.
  fx31_pass=0
  # shellcheck disable=SC2086
  for g in $GATES; do [ "$(gr_gate_status "$rec" "$g")" = pass ] && fx31_pass=$((fx31_pass+1)); done
  rm -f "$RECORD_DIR/reuse-ledger.jsonl"
  # The documented argv, verbatim. PREFLIGHT_LOCK_WAIT=0 so a run that (wrongly)
  # queued would exit 75 instead of hanging this self-test.
  r31=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 \
        ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  r31_rc=$?
  r31_lines=0
  [ -r "$RECORD_DIR/reuse-ledger.jsonl" ] && r31_lines=$(grep -c '[^[:space:]]' "$RECORD_DIR/reuse-ledger.jsonl")
  # THE ASSERTION IS "THE RUN REACHED THE GATE", NOT "THE GATE PASSED". Every gate
  # fails fast in this sandbox (no api/, no web install), so an rc of 0 is not
  # available for a shape that EXECUTES a gate — and demanding it would make this
  # check red on a correct runner. What the defect was about is visible and exact:
  # the runner must not refuse the reuse, and it must get as far as running the gate
  # it was asked for. rc=75 would mean it queued instead, which is a third shape.
  if [ "$fx31_pass" -ne $((NGATES-1)) ]; then
    bad "check 31's fixture is not the red-tree shape ($fx31_pass of $NGATES pass) — the documented path cannot be measured"
  elif grep -q 'vouches for NO gate' <<<"$r31"; then
    bad "the documented red-tree path refused with 'vouches for NO gate' — the vouched set is being filtered through the SELECTION before the refusal is decided. output=[$(printf '%s' "$r31" | tail -n 1)]"
  elif [ "$r31_rc" -eq 75 ]; then
    bad "the documented red-tree path queued on the gate lock (rc=75) instead of running — with PREFLIGHT_LOCK_WAIT=0 that is a run that never reached its gate"
  elif ! grep -q "gate \[$probe_gate\]" <<<"$r31"; then
    bad "the documented red-tree path must EXECUTE the gate it was asked to re-run ($probe_gate) — it did not run. rc=$r31_rc output=[$(printf '%s' "$r31" | tail -n 1)]"
  elif [ "$r31_lines" -ne 0 ]; then
    bad "the carried passes are OUTSIDE this run's selection, so nothing may be ledgered for them — got $r31_lines ledger line(s)"
  else
    ok "the documented red-tree path runs: --only $probe_gate --reuse-record reaches and executes $probe_gate, names the $fx31_pass carried pass(es) as outside the selection, and ledgers nothing for them"
  fi
fi

# 32. ONE REUSE EVENT, ONE LEDGER LINE. The documented lane path is consumer FIRST
#     and runner SECOND (`docs/PREFLIGHT-REUSE.md`), and both ends decide from the
#     same record: preflight_reuse.sh ledgers the saving, then preflight.sh
#     --reuse-record ledgers it again. Measured, isolated (private result dir and
#     private lock, all gates `pass` at 42s ⇒ 588s of saving):
#
#         consumer only           1 line   588s
#         runner --reuse-record   1 line   588s
#         consumer THEN runner    2 lines  1176s      <- the documented path
#
#     `--report` is the instrument acceptance is read from, so the number a human
#     or a merger reads was DOUBLE the saving actually made; the host ledger showed
#     the same shape on every lane in its window (tree 53854bd at 1405s written by
#     pids 391575/395208/1985442/2071621). The ledger is documented as "one line per
#     reuse", so the assertion is the LINE COUNT and the summed seconds, not that
#     some number was printed.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$probe_tree" "$key" "$parts"
    # shellcheck disable=SC2086
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  fx32_green=0
  # shellcheck disable=SC2086
  for g in $GATES; do [ "$(gr_gate_status "$rec" "$g")" = pass ] && fx32_green=$((fx32_green+1)); done
  expected_secs=$(( fx32_green * 42 ))
  rm -f "$RECORD_DIR/reuse-ledger.jsonl"
  # THE DOCUMENTED TWO-STEP PATH, in order: consumer, then runner. The consumer's
  # own decision must ledger; the runner must NOT write the same event again.
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight_reuse.sh --tree "$probe_tree" >/dev/null 2>&1
  PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 \
    ./scripts/preflight.sh --reuse-record >/dev/null 2>&1
  path_lines=0
  [ -r "$RECORD_DIR/reuse-ledger.jsonl" ] && path_lines=$(grep -c '[^[:space:]]' "$RECORD_DIR/reuse-ledger.jsonl")
  rep32=$(./scripts/preflight_reuse.sh --report 24 2>&1)
  if [ "$fx32_green" -ne "$NGATES" ]; then
    bad "check 32's fixture is not a whole-inventory green record ($fx32_green of $NGATES pass) — the two-step path cannot be measured"
  elif [ "$path_lines" -ne 1 ]; then
    bad "the DOCUMENTED consumer-then-runner path must ledger ONE line per reuse event — got $path_lines: [$(printf '%s' "$(cat "$RECORD_DIR/reuse-ledger.jsonl" 2>/dev/null)" | tr '\n' ' ')]"
  elif ! grep -q "host-seconds NOT spent : ${expected_secs}s" <<<"$rep32"; then
    bad "the two-step path must report the saving ONCE (${expected_secs}s), not double it — --report said [$(printf '%s' "$rep32" | grep -E 'host-seconds|reuse events' | tr '\n' ' ')]"
  else
    ok "the documented consumer-then-runner path ledgers exactly 1 line and --report sums ${expected_secs}s once (not ${expected_secs}*2)"
  fi
fi

# 33. A RECORD OBSERVED UNDER A DIFFERENT NODE IS STILL USABLE, AND SAYS SO. On this
#     host the node version is a property of the SHELL, not of the tree: an
#     INTERACTIVE shell resolves `node=v24.19.0` (nvm is appended by /root/.bashrc)
#     while a Hermes worker lane resolves `node=v26.7.0` from
#     /root/.hermes/tools/node-26.7.0-linux-x64/bin. Keyed on node, a record written
#     by one shape is REFUSED by the other with "the environment changed since that
#     record" on IDENTICAL bytes, database and webdeps — i.e. reuse was dead for the
#     one shape that runs the gates, which is the whole point of the feature.
#     `preflight.sh` deliberately does not source test-env.sh (the pre-lock path
#     explains why), and no host file ships with a PR, so "pin node in test-env.sh"
#     is not available from this repo. node is therefore RECORDED as evidence and
#     announced as an ADVISORY, not hashed into the key.
#
#     Both directions are asserted, because either one alone can be faked: the KEY
#     must not move when node moves (else the refusal is still there), and the key
#     MUST still move when the key material moves (else "drop node" degenerated into
#     "drop everything"). The second leg is what makes the first leg safe to make.
if [ -n "$probe_tree" ]; then
  parts_now="$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")"
  key_now="$(PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  # The advisory is driven off a real record whose key_parts names another node.
  rec="$(gr_file "$probe_tree")"
  key="$(gr_read_field "$rec" key)"
  sed_parts="$(printf '%s' "$parts_now" \
    | sed -E "s/(^|;)node=[^;]*/\\1node=v0.0.0-probe/")"
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "pass", "seconds": 5, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "green",\n  "finished_at": "%s"\n}\n' \
    "$probe_tree" "$key" "$(gr_json_escape "$sed_parts")" "$probe_gate" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$rec"
  r33=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 \
        ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  r33_rc=$?
  # A node that differs must NOT appear in the key material at all.
  node_in_material=0
  case "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")" in *node=*) node_in_material=1 ;; esac
  if [ "$node_in_material" -ne 0 ]; then
    bad "the node version is still part of the key MATERIAL — a worker lane's node would refuse a record written from an interactive shell on identical bytes (see gr_key_material)"
  elif [ "$r33_rc" -ne 0 ] && grep -q 'the environment changed since that record' <<<"$r33"; then
    bad "a differing node version refused the record ('the environment changed since that record') — node must be advisory, not key material"
  elif ! grep -q "OBSERVED under node" <<<"$r33"; then
    bad "a differing node version was accepted but NOT announced — the reader must be able to see the web-side gates were observed under another toolchain. output=[$(printf '%s' "$r33" | grep -E 'note|FAIL' | tr '\n' ' ')]"
  else
    ok "a differing node version does NOT invalidate the record and is announced as an advisory (node=$sed_parts vs $(gr_node_version))"
  fi
  # The COUNTER-LEG: the other fingerprint components must still bite, so dropping
  # node cannot be read as \"the fingerprint was emptied\".
  if [ "$key_now" = "$(PREFLIGHT_KEY_EXTRA=probe15-bumped gr_key "$SB")" ]; then
    bad "the fingerprint no longer changes when its key MATERIAL changes (\`extra\` bump was ignored) — the node demotion must not have emptied the key"
  elif ! grep -q 'gatescript=' <<<"$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")" \
    || ! grep -q 'webdeps=' <<<"$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")"; then
    bad "the key material dropped gatescript/webdeps while dropping node — the fingerprint must still hash the gate scripts and the installed web dependency set"
  else
    ok "the key still changes on its key material (\`extra\`, gatescript, webdeps) — node was demoted without emptying the fingerprint"
  fi
fi

# 34. ONE DATABASE, ONE KEY — THE TEST-DB HOST IS A FACT ABOUT THE SERVER, NOT
#     ABOUT ITS SPELLING. This is the card's defect, reproduced on this host: the
#     gate ran green on tree f98b5fcf, wrote `testdb=127.0.0.1:5432/jomform_test`
#     into the record's key material, and the very next READER — the same server,
#     the same database, the CALLER'S ENVIRONMENT UNCHANGED — was refused with
#     "the environment changed since that record" (recorded key 3c1cf0fc vs
#     current b34aadac) because it spelled the host `localhost`. Both spellings are
#     normal here: the test-env file a caller is told to source
#     (${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}) exports 127.0.0.1, while
#     a lane that exports the URL itself spells it localhost. The
#     refusal is SILENT about its cause and costs the WHOLE gate: the lane re-queues
#     behind the host-wide flock — a waiter has been measured at 50-86 minutes — to
#     re-run fifteen gates that had already passed on those exact bytes.
#
#     Note the two shapes this check deliberately does NOT key on: `localhost` and
#     the pre-fix `127.0.0.1` line are NOT compared here. The property is that the
#     two spellings cannot disagree, and that a genuinely different database still
#     can — pinning the canonical token would make any future change to it red for
#     no reason, which is the stale-expectation failure this file warns about.
#
#     BOTH DIRECTIONS, on one host, because either one alone can be faked:
#       * the two loopback spellings must produce ONE component and ONE key — and
#         the component must not still carry either raw spelling (that is checked
#         too: a function that returned a constant would satisfy equality alone and
#         would be blind in the other direction);
#       * a SECOND SERVER (another port) and a SECOND DATABASE (another name) must
#         still produce a DIFFERENT key, and `testdb=` must still be in the key
#         MATERIAL at all — otherwise "canonicalise the host" degenerated into
#         "drop the database from the fingerprint", which is the property
#         gr_testdb_component's own comment claims and which this check is what
#         keeps honest.
#     The last leg drives the READER, not the function: a whole-inventory green
#     record written under one spelling must be REUSED under the other (rc=0, gate
#     not executed) and REFUSED under the other database (rc!=0, naming the
#     environment) — end to end, which is the shape the defect was reported in.
if [ -n "$probe_tree" ]; then
  url_a='postgres://u:p@127.0.0.1:5432/jomform_test?sslmode=disable'
  url_b='postgres://u:p@localhost:5432/jomform_test?sslmode=disable'
  url_c='postgres://u:p@localhost:5433/jomform_test'    # another SERVER, same name
  url_d='postgres://u:p@localhost:5432/other_test'      # same server, another DATABASE
  comp_a="$(JOMFORM_TEST_DATABASE_URL="$url_a" gr_testdb_component)"
  comp_b="$(JOMFORM_TEST_DATABASE_URL="$url_b" gr_testdb_component)"
  comp_c="$(JOMFORM_TEST_DATABASE_URL="$url_c" gr_testdb_component)"
  comp_d="$(JOMFORM_TEST_DATABASE_URL="$url_d" gr_testdb_component)"
  # The key is computed in a subshell so neither spelling leaks into the checks
  # below (or into anything after this one) — this file's standing rule.
  key_a="$(export JOMFORM_TEST_DATABASE_URL="$url_a"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  key_b="$(export JOMFORM_TEST_DATABASE_URL="$url_b"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  key_c="$(export JOMFORM_TEST_DATABASE_URL="$url_c"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  key_d="$(export JOMFORM_TEST_DATABASE_URL="$url_d"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  # Does the canonically-spelled component STILL name the raw spelling? A function
  # that returned `loopback` unconditionally would pass the equality above.
  raw_left=0
  case "$comp_a" in *localhost*|*127.0.0.1*) raw_left=1 ;; esac
  if [ "$raw_left" -ne 0 ]; then
    bad "check 34 — the test-db component still carries the caller's SPELLING of a loopback host ([$comp_a]), so the key is still a fact about how the URL was written"
  elif [ -z "$comp_a" ] || [ "$comp_a" != "$comp_b" ] || [ "$key_a" != "$key_b" ]; then
    bad "check 34 — one server, two keys: the two loopback spellings must produce ONE component and ONE key, got component [$comp_a] vs [$comp_b] and key [$key_a] vs [$key_b]. A record written under one spelling is then refused under the other with 'the environment changed since that record', which costs the whole gate (the 50-86 min queue) to re-run gates that already passed on those bytes"
  elif [ "$comp_c" = "$comp_a" ] || [ "$comp_d" = "$comp_a" ] || \
       [ "$key_c" = "$key_a" ] || [ "$key_d" = "$key_a" ]; then
    bad "check 34 — the guard went BLIND in the other direction: another SERVER ([$comp_c] key $key_c) or another DATABASE ([$comp_d] key $key_d) produced the key of [${comp_a}] — a record made against one database would be reused as evidence about another"
  elif ! grep -q 'testdb=' <<<"$(export JOMFORM_TEST_DATABASE_URL="$url_a"; PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")"; then
    bad "check 34 — the key material dropped the test database entirely; canonicalising the host must not mean dropping what the record is evidence ABOUT"
  else
    ok "one server, one key: [${url_a}] and [${url_b}] both give [${comp_a}] and key ${key_a:0:12}, while another server ([${comp_c}], key ${key_c:0:12}) and another database ([${comp_d}], key ${key_d:0:12}) still differ — and testdb= is still key material"
  fi

  # The SAME property, for a MULTI-HOST DSN list (card t_54342924). A host list is
  # how a DSN names an HA pair, and two aliases of ONE server written as a list
  # (`127.0.0.1,127.0.0.1:5432/db`) escaped the loopback rule entirely, because the
  # anchored pattern has no `,` in it — so one server still gave TWO keys, which is
  # the refusal this whole check exists to prevent. Asserted as the RELATION the
  # defect breaks (the list and either single spelling agree) rather than by pinning
  # a token, so a future spelling change cannot red it for no reason.
  url_list='postgres://u:***@127.0.0.1,127.0.0.1:5432/jomform_test'
  comp_list="$(JOMFORM_TEST_DATABASE_URL="$url_list" gr_testdb_component)"
  key_list="$(export JOMFORM_TEST_DATABASE_URL="$url_list"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  case "$comp_list" in *127.0.0.1*|*localhost*) list_raw=1 ;; *) list_raw=0 ;; esac
  if [ "$list_raw" -ne 0 ]; then
    bad "check 34's multi-host leg — the component still carries the caller's SPELLING of a loopback host inside a DSN list ([$comp_list]), so the key is a fact about how the list was written"
  elif [ "$comp_list" != "$comp_a" ] || [ "$key_list" != "$key_a" ]; then
    bad "check 34's multi-host leg — TWO ALIASES OF ONE SERVER IN A DSN LIST still yield two keys: [${comp_list}] vs [${comp_a}] and key [${key_list}] vs [${key_a}]. A record written with the single spelling is then refused under the list with 'the environment changed since that record', costing the whole gate — the same class as the single-host defect this check already covers (t_54342924)"
  else
    ok "a MULTI-HOST DSN list of two aliases of one server gives the SAME component and key as either single spelling ([${comp_list}], key ${key_list:0:12}) — an HA list cannot cost a lane its record"
  fi

  # End to end on the READER. A whole-inventory green record written under
  # spelling A, then read under spelling B (must be REUSED without executing a
  # gate) and under the other database (must be REFUSED, naming the environment).
  rec="$(gr_file "$probe_tree")"
  parts_a="$(gr_json_escape "$(export JOMFORM_TEST_DATABASE_URL="$url_a"; PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
      "$probe_tree" "$key_a" "$parts_a"
    # shellcheck disable=SC2086
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  fx34_green=0
  # shellcheck disable=SC2086
  for g in $GATES; do [ "$(gr_gate_status "$rec" "$g")" = pass ] && fx34_green=$((fx34_green+1)); done
  r34_b="$(export JOMFORM_TEST_DATABASE_URL="$url_b"; PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 \
          ./scripts/preflight.sh --reuse-record 2>&1)"
  rc34_b=$?
  r34_d="$(export JOMFORM_TEST_DATABASE_URL="$url_d"; PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_WAIT=0 \
          ./scripts/preflight.sh --reuse-record 2>&1)"
  rc34_d=$?
  if [ "$fx34_green" -ne "$NGATES" ]; then
    bad "check 34's fixture is not a whole-inventory green record ($fx34_green of $NGATES pass) — the reader leg cannot be measured"
  elif [ "$rc34_b" -ne 0 ] || ! grep -q 'REUSED' <<<"$r34_b" || grep -q 'gate \[' <<<"$r34_b"; then
    bad "check 34 — the SAME server and database spelled the other way was NOT reused (rc=$rc34_b): [$(printf '%s' "$r34_b" | tail -n 1)] — this is the reported defect, and it costs the lane the whole gate"
  elif [ "$rc34_d" -eq 0 ] || ! grep -q 'the environment changed since that record' <<<"$r34_d"; then
    bad "check 34 — a record made against ANOTHER database must be refused, naming the environment — rc=$rc34_d [$(printf '%s' "$r34_d" | tail -n 1)]"
  else
    ok "the READER agrees: a whole-inventory green record written under 127.0.0.1 is reused under localhost (rc=0, no gate executed), and refused for a different database (rc=$rc34_d, 'the environment changed since that record')"
  fi
fi
# 35. THE CREDENTIAL STRIP COVERS THE SHAPE IT IS FOR, AND IT IS STILL A
#     FINGERPRINT. Two properties in one check, because either one alone can be
#     faked:
#
#       (a) NO CREDENTIAL REACHES THE COMPONENT, IN THE SHAPE THIS STRIP OWNS. A
#           URL-form value whose authority (the text before the first `/`) carries
#           userinfo loses everything through the authority's LAST `@`, so only
#           the `<host>[:port][/database]` the record is evidence about is stored.
#           The pre-fix expression (`s#^[^@/]*@##`) was ALL-OR-NOTHING: it consumed
#           userinfo only when an `@` was preceded by neither an `@` nor a `/`, so a
#           password containing either character was not stripped AT ALL and the
#           credential walked into the record — measured on this host, through
#           gr_key_material: `postgres://u1:***@...` -> `testdb=2@...` and the
#           `/` shape survived whole. Records are written under
#           ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate} in a world-readable dir,
#           and a leaked password is KEY MATERIAL: rotating it would move the key
#           and silently refuse a valid green record. THIS IS THE LEG THAT KEEPS
#           THE EQUALITY LEGS HONEST: a function returning a constant, or one that
#           matched two URLs for the wrong reason, passes the equalities below and
#           fails here (the BLIND leg) or at the suffix leg (b). "TOTAL" is NOT
#           claimed: check 36 refuses the scheme-less shape no strip can cover, and
#           check 37 widens the anchor for the no-userinfo shape; the three
#           together are what the function's own comment states per shape.
#
#       (b) THE FINGERPRINT STILL BITES. A second database and a second port must
#           still produce a DIFFERENT component and a DIFFERENT key, and `testdb=`
#           must still be in the key MATERIAL — otherwise "strip harder" had
#           degenerated into "drop the database from the fingerprint", which is the
#           property gr_testdb_component's own comment claims.
#
#     WHY THIS IS NUMBERED 35 AND NOT 34: check 34 is the test-db host SPELLING
#     check (card t_7add0cfa, PR 863), a change to the SAME function that has
#     already been verified on its exact bytes. Taking 34 here would force whoever
#     lands second to renumber, which rewrites bytes another card's verdict was
#     taken on. Skipping to 35 leaves both changes composable in either merge
#     order: 34 arrives from that PR, 35 is this one, and neither number moves.
#
#     The credential is ASSEMBLED AT RUNTIME from its character codes, so no
#     secret-shaped literal sits in a tracked file (the gitleaks gate scans the
#     full history). `$at` and `$sl` are the two characters the defect is about.
if [ -n "$probe_tree" ]; then
  at=$(printf '\100')   # @ — the userinfo delimiter, and the char a base64 password carries
  sl=$(printf '\057')   # / — the path separator, and the char a base64 password carries
  # The three shapes the defect was reported in: a simple password, one containing
  # `@`, and one containing `/`. All three must strip to the SAME component.
  u_simple="postgres://u1:pw1${at}127.0.0.1:5432/jomform_test"
  u_at="postgres://u1:pw${at}2${at}127.0.0.1:5432/jomform_test"
  u_sl="postgres://u1:pw${sl}2${at}127.0.0.1:5432/jomform_test"
  c_simple="$(JOMFORM_TEST_DATABASE_URL="$u_simple" gr_testdb_component)"
  c_at="$(JOMFORM_TEST_DATABASE_URL="$u_at" gr_testdb_component)"
  c_sl="$(JOMFORM_TEST_DATABASE_URL="$u_sl" gr_testdb_component)"
  # The component's TAIL — the port and the database, the thing the record is
  # evidence ABOUT. The HOST token itself is check 34's business (PR 863
  # canonicalises the loopback spellings to one token there) and is deliberately
  # NOT pinned here, for the reason check 34 gives: pinning the token makes any
  # future change to it red for no reason. What this check has to keep is that the
  # credential is gone AND that the thing the verdict is about is still named.
  want35_tail=":5432/jomform_test"

  # (a) THE RAW COMPONENT, for a credential-bearing URL, must carry none of it.
  #     Asserted per URL and not by comparing the three to each other: three equal
  #     WRONG values would satisfy a comparison. `u1` is the user name and `@` the
  #     delimiter, so either appearing means the credential survived.
  cred_left=""
  for pair in "simple:$c_simple" "at:$c_at" "slash:$c_sl"; do
    shape="${pair%%:*}"; got="${pair#*:}"
    case "$got" in
      *"$at"*|*u1*) cred_left="${cred_left}[$shape -> $got] " ;;
    esac
  done
  # (b) and the component must still NAME what the record is evidence about — the
  #     port and the database — not something that merely lacks the credential.
  [ "$c_simple" = "${c_simple%$want35_tail}$want35_tail" ] && [ -n "${c_simple%$want35_tail}" ] \
    || cred_left="${cred_left}[simple -> $c_simple does not end in $want35_tail] "

  # The counter-legs: the fingerprint must still separate servers and databases.
  c_port="$(JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at}127.0.0.1:5433/jomform_test" gr_testdb_component)"
  c_db="$(JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at}127.0.0.1:5432/other_test" gr_testdb_component)"
  k_simple="$(export JOMFORM_TEST_DATABASE_URL="$u_simple"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  k_port="$(export JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at}127.0.0.1:5433/jomform_test"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  k_db="$(export JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at}127.0.0.1:5432/other_test"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  # Rotating the credential must NOT move the key — the defect's own failure mode.
  k_at_rot="$(export JOMFORM_TEST_DATABASE_URL="postgres://u1:ROTATED${at}9${at}127.0.0.1:5432/jomform_test"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  k_sl_rot="$(export JOMFORM_TEST_DATABASE_URL="postgres://u1:ROT${sl}9${at}127.0.0.1:5432/jomform_test"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"

  if [ -n "$cred_left" ]; then
    bad "check 35 — the test-db component still carries the caller's CREDENTIAL for a password containing \`${at}\` or \`${sl}\`: ${cred_left}— gr_testdb_component must strip the userinfo from its right anchor, and this string is written to a world-readable record AND hashed into the key, so rotating that password would silently refuse a valid green record"
  elif [ "$c_at" != "$c_simple" ] || [ "$c_sl" != "$c_simple" ]; then
    bad "check 35 — one server, three spellings must give ONE component: got simple=[$c_simple] at=[$c_at] slash=[$c_sl]"
  elif [ "$c_port" = "$c_simple" ] || [ "$c_db" = "$c_simple" ] || \
       [ "$k_port" = "$k_simple" ] || [ "$k_db" = "$k_simple" ]; then
    bad "check 35 — the strip went BLIND in the other direction: another PORT ([$c_port] key $k_port) or another DATABASE ([$c_db] key $k_db) produced the component/key of [$c_simple] ($k_simple) — a record made against one database would be reused as evidence about another"
  elif [ "$k_at_rot" != "$k_simple" ] || [ "$k_sl_rot" != "$k_simple" ]; then
    bad "check 35 — rotating a password that contains \`${at}\` or \`${sl}\` MOVED the key (${k_at_rot:0:12} / ${k_sl_rot:0:12} vs ${k_simple:0:12}) — the credential is still key material, which is the silent 'the environment changed since that record' failure for a password change"
  elif ! grep -q 'testdb=' <<<"$(export JOMFORM_TEST_DATABASE_URL="$u_simple"; PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")"; then
    bad "check 35 — the key material dropped the test database entirely; stripping the credential must not mean dropping what the record is evidence ABOUT"
  else
    ok "the credential strip covers the shape it owns and is still a fingerprint: a password containing \`${at}\` or \`${sl}\` strips to [$c_simple] like a simple one, rotating it leaves the key unchanged (${k_simple:0:12}), while another port ([$c_port]) and another database ([$c_db]) still differ and testdb= is still key material"
  fi
fi
# 36. A TEST-DB VALUE THAT CANNOT BE STRIPPED IS REFUSED, AND THE RECORD IS READ
#     BACK TO PROVE THE CREDENTIAL IS NOT IN IT. The credential strip in check 35
#     applies to a value that carries a `scheme://`: there is a scheme to drop and
#     an authority to anchor the userinfo on. PostgreSQL's KEYWORD/VALUE DSN
#     (`host=… password=… user=…`) has neither — every `key=value` pair is
#     indistinguishable from any other, and `password=` is not even syntactically
#     special, because the driver takes an arbitrary key as a runtime parameter
#     (measured with this repo's pinned pgx v5.10.0; so a whitelist of credential
#     keys could not be shown complete). The API's own driver ACCEPTS that form, so
#     a lane can gate with it, and before the scheme guard the value was returned
#     VERBATIM: the password was written into
#     ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/<tree>.json — a world-readable
#     file — AND hashed into the key, so rotating that password silently refused a
#     valid green record.
#
#     THE FIX IS BY SHAPE: a value with no `scheme://` never reaches the fingerprint
#     at all. Two legs, because an equality-only fixture would pass for a function
#     returning a constant:
#       (a) THE RAW COMPONENT for a credential-bearing keyword/value DSN must not
#           carry the credential — read from the function, not compared to another
#           component. AND THE RECORD FILE must not carry it either: the real
#           `gr_flush` (the writer, not a re-implementation) is driven with the
#           caller's test-DB value, and the file it writes is read back and searched
#           for the password. That is the leg the defect was reported in, and the
#           only one that catches it end to end.
#       (b) THE FINGERPRINT IS UNTOUCHED FOR THE FORMS THAT DO STRIP: the ordinary
#           URL form must still produce its host/port/database component and still
#           carry `testdb=`, so "refuse the odd shape" did not degenerate into
#           "drop the database" for the DSN every gate on this host actually uses.
#           The HOST token in that component is NOT pinned here: check 34 owns the
#           loopback canonicalisation (PR 863) and check 35 owns the credential
#           strip, so this check reads the TAIL — the port and the database — which
#           is what the record is evidence about.
#     The password is ASSEMBLED AT RUNTIME from two pieces, so the whole token never
#     appears as a literal in a tracked file (the gitleaks gate scans the full
#     history). A bare `@` needs no such care — it is a delimiter that already
#     appears throughout this file and in the expressions under test.
if [ -n "$probe_tree" ]; then
  at6='@'
  kv_secret="$(printf '%s%s' 's3cret-' 'probe36')"
  kv_dsn="host=127.0.0.1 port=5432 dbname=jomform_test password=${kv_secret} user=jf"
  kv_comp="$(JOMFORM_TEST_DATABASE_URL="$kv_dsn" gr_testdb_component)"
  # The record the REAL writer leaves behind, for THIS tree, under the private
  # result dir. Driven in a subshell so the caller's test-DB value cannot leak into
  # the checks around it, and no GR_* state leaks out of it either.
  #
  # GR_GATE_NAMES IS SET, and the flush's exit code is ASSERTED. `gr_flush` keys
  # off it, so without it the flush writes NOTHING — and the read below would then
  # find whatever record was left by an earlier check and report on that instead,
  # which is a vacuous pass measured while this check was being written (the file
  # it read still said `testdb=localhost:5432/jomform_test`). A missing record is
  # caught below by the `grep -q 'testdb='` test on the file itself.
  kv_flush_rc=0
  kv_rec_out="$(
    export JOMFORM_TEST_DATABASE_URL="$kv_dsn" GR_REPO="$SB" GR_TREE="$probe_tree"
    GR_GATE_NAMES=("$probe_gate")
    GR_STATUS["$probe_gate"]=pass GR_SECONDS["$probe_gate"]=1 GR_AT["$probe_gate"]="$(gr_now)"
    GR_PASSED=1 GR_FAILED=0 GR_SKIPPED=0
    gr_flush 1 2>&1
  )" || kv_flush_rc=$?
  kv_rec="$(gr_file "$probe_tree")"
  kv_in_record=0
  [ -r "$kv_rec" ] && grep -qF "$kv_secret" "$kv_rec" && kv_in_record=1
  kv_in_comp=0
  case "$kv_comp" in *"$kv_secret"*) kv_in_comp=1 ;; esac
  # (b) the URL form is untouched — the shape the strip has always served. The
  #     HOST token is not pinned (check 34's business); the TAIL is.
  url_comp="$(JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at6}127.0.0.1:5432/jomform_test" gr_testdb_component)"
  if [ "$kv_in_comp" -ne 0 ]; then
    bad "check 36 — the test-db component still carries the CREDENTIAL from a keyword/value DSN ([$kv_comp]) — that form is accepted by the API's own driver (pgx v5.10.0), so a lane can gate with it, and the component is written to a world-readable record AND hashed into the key"
  elif [ "$kv_flush_rc" -ne 0 ]; then
    bad "check 36 — the real gr_flush exited $kv_flush_rc under a keyword/value DSN ([$kv_rec_out]), so the record leg below would have read a record left by an EARLIER check and reported on the wrong bytes"
  elif [ "$kv_in_record" -ne 0 ]; then
    bad "check 36 — the real gr_flush wrote the caller's keyword/value DSN CREDENTIAL into the record file $kv_rec (a world-readable file), as [$(grep -o 'testdb=[^;]*' "$kv_rec" | tail -n 1)] — an un-strippable value must never reach the fingerprint at all"
  elif [ -z "$kv_comp" ]; then
    bad "check 36 — a keyword/value DSN produced an EMPTY component, which would key an empty test-db field; it must produce a stable non-credential token"
  elif ! grep -q 'testdb=refused' "$kv_rec"; then
    bad "check 36 — the record written under a keyword/value DSN ([$kv_rec]) does not carry the refusal token: got [$(grep -o 'testdb=[^;]*' "$kv_rec" | tail -n 1)], expected [testdb=refused], so the value did not reach the fingerprint the way this check says"
  elif [ -z "${url_comp%:5432/jomform_test}" ] || [ "${url_comp%:5432/jomform_test}" = "$url_comp" ]; then
    bad "check 36 — the URL form regressed while carving out the keyword/value form: [postgres://u1:pw1${at6}127.0.0.1:5432/jomform_test] -> [$url_comp], expected a component ending in [:5432/jomform_test]"
  elif ! grep -q 'testdb=' <<<"$(export JOMFORM_TEST_DATABASE_URL="postgres://u1:pw1${at6}127.0.0.1:5432/jomform_test"; PREFLIGHT_KEY_EXTRA=probe15 gr_key_material "$SB")"; then
    bad "check 36 — refusing the un-strippable form dropped testdb= from the key material for the URL form too; the fingerprint must still name the database the record is evidence ABOUT"
  else
    ok "an un-strippable test-db value is REFUSED and the RECORD proves it end to end: the keyword/value DSN (which pgx v5.10.0 accepts) gives the component [$kv_comp], and the REAL gr_flush driven with it (rc=$kv_flush_rc) wrote [$(grep -o 'testdb=[^;]*' "$kv_rec" | tail -n 1)] with the password absent from the file; the URL form is untouched ([$url_comp]) and testdb= is still key material"
  fi
fi
# 37. A DSN WITH NO USERINFO WHOSE PATH CARRIES AN `@` MUST KEY ON THE SERVER THE
#     DRIVER REACHES — NOT ON THE TAIL OF THE STRING. The step-2 strip dropped
#     everything through the LAST `@` in the WHOLE value, so for
#     `postgres://hostA:5432/db@x` — no userinfo anywhere, the `@` belongs to the
#     DATABASE name — the host and port went with it. Measured on the parent of
#     this change, through gr_testdb_component AND gr_key: three DSNs naming
#     hostA/db, hostB/db and hostA/other all produced the component `x` with ONE
#     key, so the record a lane wrote against hostA was accepted by gr_reusable as
#     evidence about hostB. That is the FALSE-REUSE direction — the one that
#     silently reuses a verdict about the wrong database instead of re-gating it —
#     and it is the opposite of the direction the old comment claimed ("a
#     truncated component can only refuse a record").
#
#     THE FIX IS DECIDED BY THE AUTHORITY, NOT BY LOOKING FOR AN `@`. The text
#     before the first `/` is what PostgreSQL's own driver reads as the ADDRESS
#     (measured with this repo's pinned pgx v5.10.0: `hostA:5432` -> host=hostA
#     port=5432; `127.0.0.1` -> host=127.0.0.1), so where that text IS
#     `host[:port]` the whole value is kept — it is the driver's own reading — and
#     where it is not (it reads as userinfo, a shape the same driver REFUSES with
#     "invalid port ... after host") the strip is kept, which can only refuse a
#     record.
#
#     FOUR LEGS, because any one alone can be satisfied by a degenerate function:
#       (a) THE RAW COMPONENT for a host-bearing input still CARRIES THE HOST —
#           read from the function. A function returning a constant, or one that
#           matched the three inputs by hashing the whole string, passes an
#           equality-only fixture and fails here.
#       (b) THREE SERVERS/DATABASES -> THREE COMPONENTS AND THREE KEYS. Counted as
#           distinct VALUES, not by comparing pairs, so a component that is equal
#           for a wrong reason (all empty) is not counted as three.
#       (c) THE FALSE REUSE ITSELF, END TO END: a record written by the REAL
#           gr_flush under hostA must be REUSABLE under hostA and REFUSED under
#           hostB and under hostA/other, with the reader's "the environment changed
#           since that record". The hostA leg is the counter-leg: a function that
#           refused everything would satisfy the refusals alone.
#       (d) THE SHAPE THE STRIP IS FOR STILL STRIPS. A password containing `@` or
#           `/` (the t_c65ef5a6 / PR-865 acceptance) must give the same component
#           as a simple one, and rotating it must not move the key — so widening
#           the anchor for (a)-(c) did not reopen the credential hole.
#     The `@` is assembled at runtime from its character code, so no
#     secret-shaped literal sits in a tracked file (the gitleaks gate scans the
#     full history).
if [ -n "$probe_tree" ]; then
  at37="$(printf '\100')"
  sl37="$(printf '\057')"
  uA="postgres://hostA:5432/db${at37}x"
  uB="postgres://hostB:5432/db${at37}x"
  uA2="postgres://hostA:5432/other${at37}x"
  c_A="$(JOMFORM_TEST_DATABASE_URL="$uA" gr_testdb_component)"
  c_B="$(JOMFORM_TEST_DATABASE_URL="$uB" gr_testdb_component)"
  c_A2="$(JOMFORM_TEST_DATABASE_URL="$uA2" gr_testdb_component)"
  k_A="$(export JOMFORM_TEST_DATABASE_URL="$uA"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  k_B="$(export JOMFORM_TEST_DATABASE_URL="$uB"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  k_A2="$(export JOMFORM_TEST_DATABASE_URL="$uA2"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  # (a2) A BRACKETED IPv6 authority is an address the driver resolves too (measured:
  #      pgx v5.10.0 gives host=::1 port=5432 for `[::1]:5432`), and `[::1]` is a
  #      spelling this host's callers use — so it must keep its port and database
  #      for the same reason, not fall into the credential branch and collapse to
  #      the bare tail `db@x`. The HOST slot itself is canonicalised by check 34's
  #      rule (PR 863 rewrites `[::1]` to the same loopback token as the other two
  #      spellings), so the token is read from the component rather than pinned.
  uV6="postgres://[::1]:5432/db${at37}x"
  c_V6="$(JOMFORM_TEST_DATABASE_URL="$uV6" gr_testdb_component)"

  # (b) distinct VALUES, counted — not pairwise equality, which a set of equal
  # wrong components satisfies.
  n_comp="$(printf '%s\n%s\n%s\n' "$c_A" "$c_B" "$c_A2" | sort -u | grep -c '[^[:space:]]')"
  # The IPv6 leg: the component must still carry the PORT+DATABASE — the thing the
  # record is evidence about. Its host token differs from hostA's on purpose (they
  # are different servers; `[::1]` is loopback and is canonicalised to the same
  # token as `localhost`/`127.0.0.1` by check 34's rule), so the leg is on the TAIL,
  # and the failure it excludes is the old collapse to the bare `db@x`.
  v6_ok=0
  case "$c_V6" in
    *":5432/db${at37}x") [ "$c_V6" != "db${at37}x" ] && v6_ok=1 ;;
  esac
  n_key="$(printf '%s\n%s\n%s\n' "$k_A" "$k_B" "$k_A2" | sort -u | grep -c '[^[:space:]]')"

  # (c) the REAL writer, then the REAL reader, under each server. GR_GATE_NAMES is
  # set and the flush's exit code is ASSERTED: gr_flush keys off it, so without it
  # the flush writes nothing and the refusals below would pass for a reason that
  # has nothing to do with the fix.
  at_flush_rc=0
  at_rec_out="$(
    export JOMFORM_TEST_DATABASE_URL="$uA" GR_REPO="$SB" GR_TREE="$probe_tree"
    GR_GATE_NAMES=("$probe_gate")
    GR_STATUS["$probe_gate"]=pass GR_SECONDS["$probe_gate"]=1 GR_AT["$probe_gate"]="$(gr_now)"
    GR_PASSED=1 GR_FAILED=0 GR_SKIPPED=0
    gr_flush 1 2>&1
  )" || at_flush_rc=$?
  at_rec="$(gr_file "$probe_tree")"
  at_self_rc=0; at_self_msg="$(JOMFORM_TEST_DATABASE_URL="$uA" gr_reusable "$probe_tree" "$probe_gate" 2>&1)" || at_self_rc=$?
  at_other_rc=0; at_other_msg="$(JOMFORM_TEST_DATABASE_URL="$uB" gr_reusable "$probe_tree" "$probe_gate" 2>&1)" || at_other_rc=$?
  at_db_rc=0;    at_db_msg="$(JOMFORM_TEST_DATABASE_URL="$uA2" gr_reusable "$probe_tree" "$probe_gate" 2>&1)" || at_db_rc=$?

  # (d) the strip's own acceptance, restated here so a change to this function that
  # widens the anchor cannot quietly reopen the credential hole.
  d_simple="postgres://u1:pw1${at37}127.0.0.1:5432/jomform_test"
  d_at="postgres://u1:pw${at37}2${at37}127.0.0.1:5432/jomform_test"
  d_sl="postgres://u1:pw${sl37}2${at37}127.0.0.1:5432/jomform_test"
  d_rot="postgres://u1:ROTATED${at37}9${at37}127.0.0.1:5432/jomform_test"
  d_c_simple="$(JOMFORM_TEST_DATABASE_URL="$d_simple" gr_testdb_component)"
  d_c_at="$(JOMFORM_TEST_DATABASE_URL="$d_at" gr_testdb_component)"
  d_c_sl="$(JOMFORM_TEST_DATABASE_URL="$d_sl" gr_testdb_component)"
  d_k_simple="$(export JOMFORM_TEST_DATABASE_URL="$d_simple"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"
  d_k_rot="$(export JOMFORM_TEST_DATABASE_URL="$d_rot"; PREFLIGHT_KEY_EXTRA=probe15 gr_key "$SB")"

  if [ "$c_A" != "hostA:5432/db${at37}x" ]; then
    bad "check 37 — the component for a host-bearing no-userinfo DSN does not keep the HOST: [postgres://hostA:5432/db${at37}x] -> [$c_A], expected [hostA:5432/db${at37}x]. The text before the first \`/\` is the address the driver resolves (pgx v5.10.0), so a component that lost it is not a fact about the server the record would be evidence for"
  elif [ "$n_comp" != "3" ] || [ "$n_key" != "3" ]; then
    bad "check 37 — DIFFERENT servers/databases collapsed onto one component or one key ($n_comp distinct components, $n_key distinct keys, expected 3 of each): hostA=[$c_A $k_A] hostB=[$c_B $k_B] hostA/other=[$c_A2 $k_A2] — a record written for one server would be reused as evidence about another, which is the FALSE-REUSE direction (it reuses a wrong verdict instead of re-gating it)"
  elif [ "$at_flush_rc" -ne 0 ]; then
    bad "check 37 — the real gr_flush exited $at_flush_rc under a host-bearing no-userinfo DSN ([$at_rec_out]), so the reader legs below would have read a record left by an EARLIER check and reported on the wrong bytes"
  elif [ "$at_self_rc" -ne 0 ]; then
    bad "check 37 — the counter-leg failed: the record written under [postgres://hostA:5432/db${at37}x] is NOT reusable under the SAME server (rc=$at_self_rc: $(printf '%s' "$at_self_msg" | head -c 200)) — the refusals this check requires would then pass for a function that refuses everything, not for one that keys on the server"
  elif [ "$at_other_rc" -eq 0 ]; then
    bad "check 37 — FALSE REUSE: the record written under [postgres://hostA:5432/db${at37}x] was REUSED (rc=0) for [postgres://hostB:5432/db${at37}x]. A record made against another server must be refused, with the reader's 'the environment changed since that record' — this is the leg the defect was reported in"
  elif ! grep -q 'the environment changed since that record' <<<"$at_other_msg"; then
    bad "check 37 — the other-server reader refused (rc=$at_other_rc) but not for the fingerprint: [$(printf '%s' "$at_other_msg" | head -c 200)] — the refusal has to be the key mismatch, or this leg proves nothing about the component"
  elif [ "$at_db_rc" -eq 0 ]; then
    bad "check 37 — FALSE REUSE: the record written under [postgres://hostA:5432/db${at37}x] was REUSED (rc=0) for [postgres://hostA:5432/other${at37}x] — another DATABASE on the same server must not inherit it either"
  elif [ "$d_c_at" != "$d_c_simple" ] || [ "$d_c_sl" != "$d_c_simple" ] || \
       [ "$d_k_rot" != "$d_k_simple" ] || [ -z "$d_c_simple" ]; then
    bad "check 37 — widening the anchor for the no-userinfo case reopened the CREDENTIAL hole: simple=[$d_simple -> $d_c_simple] at=[$d_at -> $d_c_at] slash=[$d_sl -> $d_c_sl] rotated-key=${d_k_rot:0:12} vs ${d_k_simple:0:12} — a password containing \`${at37}\` or \`${sl37}\` must strip to the same component as a simple one and must not be key material"
  elif [ "$v6_ok" -ne 1 ]; then
    bad "check 37 — a BRACKETED IPv6 authority lost its PORT+DATABASE: [postgres://[::1]:5432/db${at37}x] -> [$c_V6], expected a component ending in [:5432/db${at37}x]. [::1] is an address the driver resolves (pgx v5.10.0: host=::1 port=5432) and a spelling this host's callers use, so reading it as userinfo collapses it to the bare tail [db${at37}x] — the same collision, for exactly that spelling"
  else
    ok "a no-userinfo DSN whose path carries \`${at37}\` keys on the SERVER: [hostA:5432/db${at37}x], [hostB:5432/db${at37}x] and [hostA:5432/other${at37}x] give 3 distinct components and 3 distinct keys, the REAL gr_flush record from hostA is reusable under hostA (rc=$at_self_rc) and REFUSED for hostB and for hostA/other with 'the environment changed since that record', while a password containing \`${at37}\` or \`${sl37}\` still strips to [$d_c_simple] and rotating it leaves the key unchanged, and a bracketed IPv6 authority keeps its port+database ([$c_V6])"
  fi
fi

# 38. THE SELF-TEST MUST NOT HAND ITS STUB'S RECORD TO THE CALLER'S STORE.
#
#     Checks 17a/17c/17d drive the LIVE runner under a stubbed `go`, and the stub
#     exits 99 — so each of those runs records `test = fail`. The self-test's own
#     private RECORD_DIR is exported only with the record-mechanism block further
#     down, so before this fix those nested runs inherited the CALLER's store: the
#     host default /var/lib/jomform/gate whenever the `selftest` gate is driven by
#     hand (the recommended no-queue path, since this script takes its own lock).
#     The record carries the stub's fingerprint (`go=FAKE-GO ARGV`) and is keyed on
#     a real tree sha, so it can never be reused: on the host store the only thing
#     it does is refuse the reuse that mechanism exists to provide. Measured on this
#     host, 2026-10-02: 51 of the 125 records in /var/lib/jomform/gate carried that
#     fingerprint, ~20 naming a real worktree under .worktrees/.
#
#     Asserted on a store this CHECK owns, never on the host store. Both directions,
#     because either alone can be faked:
#       * the LEAK must be reachable — the same helper driven with the store pointed
#         at a dir the check owns, but with the pin defeated (the caller's value
#         restored), must land a FAKE-GO record there. Without this leg a green
#         below would only mean the helper wrote nothing at all.
#       * the HELPER as shipped must leave that same dir free of 'FAKE-GO ARGV' AND
#         still return the argv it exists to read.
hoststore_leak=0     # the leak is reachable when the pin is defeated
hoststore_helper=0   # the shipped helper still leaks into the caller's store
helper_answered=0    # the helper still produces the argv it exists to read
if ! git -C "$SB" rev-parse --git-dir >/dev/null 2>&1; then
  ok "check 38 — the sandbox is not a git repo, so this check does not apply"
else
  CALLERSTORE="$SB_STATE/s38-caller-store"
  live_tree="$(gr_tree_sha "$LIVE_ROOT" 2>/dev/null || true)"

  # --- 38a. Reachability: the same shape WITHOUT the pin leaks.
  rm -rf "$CALLERSTORE"; mkdir -p "$CALLERSTORE"
  ( cd "$LIVE_ROOT" && env \
      PREFLIGHT_RESULT_DIR="$CALLERSTORE" \
      PREFLIGHT_RESULT_TTL=3600 \
      PATH="$TIMEBIN:$PATH" \
      JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder \
      ./scripts/preflight.sh --only test >/dev/null 2>&1 ) || true
  grep -rls 'FAKE-GO ARGV' "$CALLERSTORE" >/dev/null 2>&1 && hoststore_leak=1

  # --- 38b. The shipped helper must leave that dir free, and still answer.
  rm -rf "$CALLERSTORE"; mkdir -p "$CALLERSTORE"
  _s38_out="$( PREFLIGHT_RESULT_DIR="$CALLERSTORE" test_gate_argv "" 2>/dev/null || true )"
  [ -n "$_s38_out" ] && helper_answered=1
  grep -rls 'FAKE-GO ARGV' "$CALLERSTORE" >/dev/null 2>&1 && hoststore_helper=1

  if [ "$hoststore_leak" -ne 1 ]; then
    bad "check 38 — a stub-driven run with the store pointed at a caller-named dir wrote NO 'FAKE-GO ARGV' record, so the leak this check is about is not reachable here and a green below would be vacuous (looked in [$CALLERSTORE])"
  elif [ "$helper_answered" -ne 1 ]; then
    bad "check 38 — the argv helper returned nothing, so this check cannot tell a fixed helper from a broken one"
  elif [ "$hoststore_helper" -eq 1 ]; then
    bad "check 38 — the argv helper still writes its STUB's record into the caller's store: 'FAKE-GO ARGV' found under [$CALLERSTORE]${live_tree:+ (live tree ${live_tree:0:7})}. That record can never be reused, so on the host store it only refuses the reuse this mechanism exists to provide"
  else
    ok "check 38 — the argv helper cannot reach the caller's store: the unpinned shape DOES leave a stub record in a caller-named dir (so the leak is real), while the shipped helper leaves it free of 'FAKE-GO ARGV' and still returns the argv${live_tree:+ (live tree ${live_tree:0:7})}"
  fi
fi

# 39. EVERY web/e2e SPEC BELONGS TO EXACTLY ONE PLAYWRIGHT PROJECT (t_34fd25cb).
#
#     The `a11y` gate was split from the feature specs by giving each family its own
#     named playwright project. A project rule that silently matches NO spec is a
#     coverage loss with no signal at all — the specs would simply stop running —
#     and one that matches TWO runs them twice and makes the split a lie.
#
#     THE JUDGEMENT IS DONE IN PYTHON, NOT `grep -E`, and that is not a preference.
#     Measured while writing this check: the patterns in the config carry JS-level
#     escaping (`\\.`), and the feature family is expressed as a negative lookahead
#     — which POSIX ERE has no notion of. `grep -E` warned "? at start of expression"
#     and matched nothing, so the shell version reported EVERY feature spec as
#     unowned. Both are properties of the pattern language, not of the split.
s39_out="$(python3 - "$LIVE_ROOT" <<'PYEOF' 2>&1
import re, sys, glob, os
root = sys.argv[1]
cfg = os.path.join(root, "web", "playwright.config.ts")
specs = sorted(os.path.basename(p) for p in glob.glob(os.path.join(root, "web", "e2e", "*.spec.ts")))
if not specs:
    print("NO_SPECS"); sys.exit(0)
projs = []
for line in open(cfg, encoding="utf-8"):
    if "testMatch" not in line:
        continue
    n = re.search(r'name:\s*"([^"]+)"', line)
    t = re.search(r'testMatch:\s*/(.+)/\s*\}?\s*,?\s*$', line.rstrip())
    if n and t:
        # The config is TypeScript source, so its regex literal is JS-escaped: `\\.`
        # means a literal dot. Undo that one level before using it as a pattern — a
        # pattern that still says `\\.` matches nothing.
        projs.append((n.group(1), t.group(1).replace("\\\\", "\\")))
if not projs:
    print("NO_PROJECTS"); sys.exit(0)
unowned, double = [], []
for sp in specs:
    who = [name for name, pat in projs if re.match(pat, sp)]
    if not who:
        unowned.append(sp)
    elif len(who) > 1:
        double.append("%s(%s)" % (sp, ",".join(who)))
if unowned:
    print("UNOWNED " + " ".join(unowned))
elif double:
    print("DOUBLE " + " ".join(double))
else:
    print("OK %d %d" % (len(specs), len(projs)))
PYEOF
)"
case "$s39_out" in
  OK\ *)
    # shellcheck disable=SC2086
    set -- $s39_out
    ok "check 39 — every one of the $2 web/e2e specs belongs to exactly ONE of the $3 playwright projects (no spec unowned, none double-run), so the a11y/feature split cannot drop coverage"
    ;;
  NO_SPECS)
    bad "check 39 — no *.spec.ts under $LIVE_ROOT/web/e2e, so the project split cannot be judged (a guard that reads nothing proves nothing)"
    ;;
  NO_PROJECTS)
    bad "check 39 — no project in web/playwright.config.ts declares a testMatch, so the split cannot be proven; an unnamed project would run EVERY spec and the a11y gate would silently be the whole directory again"
    ;;
  UNOWNED\ *)
    bad "check 39 — spec(s) match NO playwright project, so no gate runs them:${s39_out#UNOWNED}. A spec outside every project is a silent coverage loss; add it to a family's testMatch"
    ;;
  DOUBLE\ *)
    bad "check 39 — spec(s) match MORE THAN ONE project, so they run twice and the family split is not a split:${s39_out#DOUBLE}"
    ;;
  *)
    bad "check 39 — the project/spec judgement did not run: [$s39_out]"
    ;;
esac

echo "---"
echo "preflight self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
