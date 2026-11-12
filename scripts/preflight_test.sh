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
# THE POOL, NOT A LOCK (card t_79c0964a). scripts/preflight.sh used to serialise
# every gate run behind ONE host-wide `flock`, on the stated premise that two
# concurrent runs "share one scratch schema and one scratch valkey index". Both
# are per-PROCESS now (`test_<pkg>_p<pid>`, `jftest:p<pid>:` on a pid-derived DB),
# so the premise was stale and the lock only serialised LANES — measured: a card
# held it 4h45m for 4m42s of real CPU, 99 minutes of that `sleep`. It is now a
# POOL of PREFLIGHT_SLOT_COUNT slots (default 2), so two lanes gate in parallel
# while the host stays capped. Checks 16/16b/16c/16d are what keep that honest:
# the self-test still must not join the production POOL, the cap must really cap,
# a waiter must still get to run, an inherited slot must not be queued on, and a
# sabotage case proves the cap check can go RED.
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
# load, on the real payloads these checks grep: 1/500 runs at check 8. (Check 10's
# figure is NOT quoted here: the `11/500` that once stood in this line was RETRACTED
# by the implementer — it was a pattern artefact, the BRE `\|…` passed without `-E`,
# which matched bare `true`/`false` across an 11 KB payload. On the shipped `-nE`
# pattern it is 0 flips, and the old shape only fails green on a MULTI-match payload
# (300 matches -> 119/800, 5000 -> 800/800), which is the direction justification and
# not a flake. Quoting a retracted number in this file — whose whole subject is not
# printing a claim the evidence does not support — is the defect card t_5058fb49 fixed.)
# Feed grep a here-string (`grep -q PAT <<<"$out"`) — no writer process
# exists, so there is nothing to signal — or capture first and test the string.
# A red run must mean the property was violated, nothing else.
#
# THE SAME CLASS, READER-EXITS-EARLY: `… | grep -q .` (check 10's old shape) is
# the dangerous direction, because the failure lands on the `else` branch and
# reports ok — a green gate that cannot fail. Same remedy: capture, then test.
# `… | head -n1` is the same defect for an assignment (silently yields nothing).
#
# This is one shared rule, not per-site judgement: `deploy/tmp-apikey_test.sh`
# and `web/scripts/biteproof_next_managed_files.sh` carry the same note after being
# bitten by it.
#
# THE SWEEP RECIPE (card t_0633ea1d). The rule is repo-wide, so the recipe that
# finds the live sites belongs here rather than in a card body. Run it and read
# every hit that is not a comment:
#
#   git grep -nE '\| *grep +(-[a-zA-Z]*q|.*-m *1)|\|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1\b' \
#     -- '*.sh' '*.py' '*.mjs' '*.js' '*.ts' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*#'
#
# The `\|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1\b` alternative is LOAD-BEARING, and it was
# ABSENT from the sweep that closed cards t_7953b76a / t_39b04c34. That recipe was
# `'\| *grep +(-[a-zA-Z]*q|.*-m *1)'`, which matches `grep -q` / `grep -m1` but NOT
# `| head -n1` — even though the class named in this very paragraph says `| head -n1`
# "has the identical shape". Two live `| head -n1` sites then survived in
# `deploy/backup/jomform-backup.sh` (`env_value`, `container_for`) while that sweep
# reported itself complete; card t_0633ea1d fixed them. `-n?[[:space:]]*1\b` also
# matches the `head -1` spelling that `deploy/tmp-apikey.sh` documents, and the
# SPACED `head -n 1` that the earlier `-n?1` MISSED (card t_809575fc): the recipe
# and this file's own self-scan now carry the identical alternative, so a
# reader's sweep and the file's own scan cannot disagree on what a live site is.
#
# FOUR HONEST LIMITS OF THAT RECIPE, stated so its output is not over-read:
#   1. `… | grep -q .` (a bare dot, no flag letter) is NOT matched by the
#      `+(-[a-zA-Z]*q|…)` branch; check 10's old shape is found by reading for it.
#   2. It is a SHAPE detector, not a verdict: `|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1\b`
#      matches every `| head -1`, so a hit whose writer has no PROCESS (a
#      here-string — `… <<<"$out" | head -1`) cannot SIGPIPE anything and is
#      display formatting, as is a writer whose output is a handful of lines.
#      Separate a real site by its WRITER, not by the pattern: `git grep … | head
#      -1`, `sed … | head -1`, `docker ps … | head -1` and `rclone … | head -1`
#      are the class (a separate writer process, output that can exceed the 64KB
#      pipe buffer); `… <<<"$out" | sort -u | head -1` is not.
#   3. The `grep` branch ALSO matches a logical OR: `… || grep -q x` is not a pipe
#      at all, but the pattern is unanchored, so it matches the SECOND `|` and
#      reports a line that has no reader and nothing to SIGPIPE (measured on
#      `if [ "$a" = b ] || grep -q x f; then` — one hit, no pipe). Read the HIT
#      before believing it; a genuine site is `writer | grep -q`, never `|| grep`.
#   4. The `1\b` anchor means a MULTI-line reader (`| head -n 10`) is NOT matched:
#      the alternative names the ONE-line spellings (`head -n1`, `head -n 1`,
#      `head -1`). That is a stated scope, not an oversight — a `| head -n 10`
#      early-exit reader is the same shape, so it is a deliberate omission you
#      still have to read for by eye.
#
# THIS FILE IS NOT THE ONLY ONE — that claim was false when it was written (card
# t_5058fb49). At the time, live non-comment sites still existed in files that
# preflight gates EXECUTE, so "the sweep is complete" was already untrue. What is
# true of this file is narrower and checkable: it contains no such site, and the
# selftest asserts that. For the rest of the repo the honest statement is that the
# idiom keeps reappearing — fix it where you find it rather than believe it is gone.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SELF="$HERE/$(basename "${BASH_SOURCE[0]}")"
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
#      THE SAME CLEAN-SHELL RULE FOR THE POOL INHERITANCE MARKERS, and this one was
#      MEASURED as a false reading rather than reasoned about. A run that holds a slot
#      exports `PREFLIGHT_SLOT_FD` / `_HOLDER` for its nested invocations, and THIS
#      self-test is such a nested invocation — so the markers leak in, and every nested
#      `./scripts/preflight.sh` below then reports "ALREADY INSIDE the gate pool" and
#      runs its gates instead of taking a slot. Checks 16b and 16e measure exactly that
#      take, so they were defeated: measured inside the gate (checks 16b/16e red with
#      "ALREADY INSIDE the gate pool" in the output) while `bash scripts/preflight_test.sh`
#      alone was green — the identical shape the `_PREFLIGHT_LOCKED` and SEL_DIFF notes
#      above describe, one handoff over. A lane calls preflight.sh from a CLEAN shell, so
#      every nested invocation below must start as one: the markers (and any pool size a
#      caller exported) are cleared here — and ALSO named with `env -u` at the two probes
#      that measure the TAKE, because those run as separate PROCESSES where a clear in
#      this shell is not enough on its own. Check 16d re-establishes them explicitly,
#      and deliberately, for the one check that is about inheritance.
unset PREFLIGHT_SLOT_FD PREFLIGHT_SLOT_HOLDER PREFLIGHT_SLOT_N PREFLIGHT_SLOT_LOCK
unset PREFLIGHT_SLOT_COUNT PREFLIGHT_LOCK_WAIT
export -n PREFLIGHT_SLOT_FD PREFLIGHT_SLOT_HOLDER PREFLIGHT_SLOT_N PREFLIGHT_SLOT_LOCK 2>/dev/null || true
export -n PREFLIGHT_SLOT_COUNT PREFLIGHT_LOCK_WAIT 2>/dev/null || true
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
# 15p's fixture (card t_315a5f91). A Go source whose ONLY repo-wide mention sits
# inside a BACKTICK raw string — i.e. a MESSAGE, which the reduction that decides a
# repo-wide read (`pw_code_lines` in the runner, `probe_code_lines` in this file)
# strips. It is the exact false positive the backtick arm was added for, and it is
# deliberately inert: no statement of this file runs the command. Without the arm,
# the raw line matches the pattern, `api/*` looks like a whole-checkout reader, and
# `test` becomes unable to narrow (measured: 9 '*' rows instead of 4, `selftest`
# anchors RED). Planted in the SANDBOX, like the metachar registers above.
cat > "$SB/api/internal/qaprobe/backtick_msg.go" <<'PWBMSG'
package qaprobe

// backtickMsg is a MESSAGE, not a read: the command it names runs in NO statement
// of this file. Counting it as a read is the false positive the backtick arm of
// pw_code_lines/probe_code_lines exists to strip (card t_315a5f91).
var backtickMsg = `run git ls-files to see it`
PWBMSG
git -C "$SB" init -q .
git -C "$SB" add -A
git -C "$SB" -c user.email=t@t -c user.name=t commit -q -m init
cd "$SB" || exit 1

# ---------------------------------------------------------------------------
# PRIVATE LOCK PATH — the self-test must never join the production gate pool.
#
# scripts/preflight.sh takes a slot from the host's gate POOL before it runs any
# gate, and on a busy board every slot can be busy. So every invocation below
# would otherwise wait up to PREFLIGHT_LOCK_WAIT (default 1800s) for a slot, and
# while running it would HOLD one, delaying every card gate waiting behind it.
# Both shapes are avoided by pointing the self-test at its own pool path.
#
# This does not weaken anything under test: the self-test exercises argument
# handling, prerequisite failure and inventory drift, and none of its invocations
# runs a real gate body (each either refuses outright or dies on a missing
# prerequisite), so an extra slot here cannot contend for a real gate's CPU.
# Check 16 proves the isolation holds; checks 16b/16c/16d prove the pool's own
# properties (the cap caps, a waiter gets to run, and an inherited slot runs
# rather than queueing behind its own parent).
# ---------------------------------------------------------------------------
export PREFLIGHT_LOCK_FILE="$SB_STATE/preflight-selftest.lock"
# THE HOST-WIDE BROWSER-SUITE LOCK IS PINNED HERE TOO, AND THIS ONE IS NOT OPTIONAL
# (card t_d0c0981d). Its default is ONE file for the whole host — that is the point of
# it — so a self-test that inherited the default would queue behind every live lane's
# a11y/e2e gate AND hold the lock against them while its own checks ran. Pinned to a
# private path it measures the same mechanism without joining the production queue, the
# same reason the pool path above is private. Check 45 asserts this stayed private.
export PREFLIGHT_WEBSERVER_LOCK_FILE="$SB_STATE/preflight-selftest-webserver.lock"
# A WAIT THAT CANNOT OUTLAST THE SELF-TEST: if a check ever leaves a stub suite holding
# the private lock, the next one must fail in seconds rather than sit for the 1800s a
# real lane is allowed.
export PREFLIGHT_WEBSERVER_WAIT=120
# TWO SLOTS, the default, is what these checks measure — and it is the number a
# real lane gets, so the shape under test is the shipped one.

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
# THE STRIP STOPS AT A COMMENT-INTRODUCING `#` ONLY (card t_6b291041). This line
# carried `s/[[:space:]]*#.*$//` — `[[:space:]]*` is ZERO-or-more, so it truncated at
# ANY `#`, including one inside `${...}`. A runner line like
# `X="${X#y}"; run monitor` was reduced to `X="${X`, and the `run monitor` call site
# behind the `#` never reached the pattern below — so a gate the runner EXECUTES was
# invisible to this read. Measured: the old pattern yields `X="${X`, the new one keeps
# the whole line.
#
# HONEST SCOPE, measured: this reader is anchored `^[[:space:]]*run`, and in this
# runner every `run` call site begins its line (0 exceptions today), so the escape
# cannot CURRENTLY lose a call site here — it would take a line with a `#` before a
# line-leading `run`. It is fixed anyway so the two comment-readers of this runner
# (here and check 10) apply the same rule, and so the next un-anchored use of the idom
# cannot hide one. The reachable instance of the class is the Go reader in
# api/internal/biteharness/inventory_test.go, where the strip runs over BODY lines and
# a harness behind `${X#y}` was genuinely invisible (card t_6b291041).
runnable="$(sed -E 's/(^|[[:space:]])#.*$//' "$PREFLIGHT" \
  | grep -oE '^[[:space:]]*run[[:space:]]+[A-Za-z0-9_-]+' \
  | awk '{print $2}' | sort -u)"
# The CONCURRENT group's gates have no `run` line: `run` is synchronous and the
# group forks. They are declared in the runner as `BROWSER_GATES=(…)` and added
# here, so "listed but never run" stays a real signal instead of flagging a gate
# that the group dispatches (card t_ec2aac42).
group_gates="$(sed -n 's/^BROWSER_GATES=(\(.*\))$/\1/p' "$PREFLIGHT" | tr ' ' '\n' | sed '/^$/d')"
if [ -n "$group_gates" ]; then
  runnable="$(printf '%s\n%s\n' "$runnable" "$group_gates" | sort -u)"
fi

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

# NODE STUBS for checks 5–5d (card t_f96c7a44).
#
# The node-VERSION prerequisite now sits FIRST in `preflight_env`'s web branch, so
# every check below that is really about the DEPENDENCY prerequisite would report
# the node refusal on a host whose node is too old for the suite — the wrong
# reason, and a RED `selftest` on bytes with nothing wrong. Checks 5/5b/5c
# therefore pin a node AT the floor, and 5d pins versions below it on purpose.
#
# A stub reports <version> for -v/--version and DELEGATES every other argv to the
# real node, so `web-deps-check.mjs` still executes for real: the VERSION is the
# only faked fact, and it is the fact these checks are about.
REAL_NODE_BIN="$(command -v node)"
[ -n "$REAL_NODE_BIN" ] || { echo "FAIL: no node on PATH — the web gates cannot be exercised" >&2; exit 1; }
mk_node_stub() {  # <dir> <version>
  mkdir -p "$1"
  printf '#!/bin/sh\ncase "$1" in\n  -v|--version) printf "%%s\\n" "%s" ;;\n  *) exec "%s" "$@" ;;\nesac\n' \
    "$2" "$REAL_NODE_BIN" > "$1/node"
  chmod +x "$1/node"
}
# The floor is READ from the runner under test, never typed here, so a floor bump
# there cannot leave these checks asserting a stale boundary (and the derived
# "one minor below" version moves with it).
NODE_FLOOR_RAW="$(sed -nE 's/^NODE_FLOOR_TESTS="([0-9]+)\.([0-9]+)".*/\1 \2/p' "$SB/scripts/preflight.sh" | sed -n 1p)"
NODE_STUB_DIR="$SB_STATE/nodestub"
AT_FLOOR="v24.19.0"       # fallback if the floor cannot be read; 5d reports that
BELOW_FLOOR="v20.20.2"
if [ -n "$NODE_FLOOR_RAW" ]; then
  NF_MAJ="${NODE_FLOOR_RAW% *}"; NF_MIN="${NODE_FLOOR_RAW#* }"
  AT_FLOOR="v${NF_MAJ}.${NF_MIN}.0"
  BELOW_FLOOR="v${NF_MAJ}.$((NF_MIN - 1)).0"
  [ "$NF_MIN" -eq 0 ] && BELOW_FLOOR="v$((NF_MAJ - 1)).99.0"
fi
mk_node_stub "$NODE_STUB_DIR/good" "$AT_FLOOR"
mk_node_stub "$NODE_STUB_DIR/below-floor" "$BELOW_FLOOR"
# The TRAP version: playwright, next 16 and eslint 9 all accept it, and it still
# cannot run `test:unit` (node 20.20.2 is the one measured on this host).
mk_node_stub "$NODE_STUB_DIR/v20" "v20.20.2"
mk_node_stub "$NODE_STUB_DIR/v18" "v18.19.1"

# 5. A missing web/node_modules must fail the web gate. A worktree without it
#    makes `npm run build` exit 0 having built nothing.
#    (PATH pins a node AT the floor — see the stub block above — so this check
#    measures the DEPENDENCY prerequisite on every host, including one whose own
#    node is too old for the suite.)
out=$(PATH="$NODE_STUB_DIR/good:$PATH" ./scripts/preflight.sh --only web 2>&1)
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
out=$(PATH="$NODE_STUB_DIR/good:$PATH" ./scripts/preflight.sh --only web 2>&1)
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
out=$(PATH="$NODE_STUB_DIR/good:$PATH" ./scripts/preflight.sh --only web 2>&1)
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

# 5d. THE NODE-VERSION PREREQUISITE (card t_f96c7a44). The `web` gate is
#     `npm run build`, whose FIRST half is `node --test … src/lib/*.test.ts` —
#     TypeScript through node, which needs type stripping ON BY DEFAULT (node
#     22.18.0+; 23.6.0+ on the 23 line; 22.6–22.17 only behind a flag the script
#     does not pass). EVERY OTHER web tool's floor is LOWER: playwright refuses
#     < 20, next 16 declares >= 20.9.0, eslint 9 accepts ^20.9.0 — so node 20 is
#     a version a lane can land on for a legitimate reason and it still reds the
#     SAME gate, seconds in, with `TypeError [ERR_UNKNOWN_FILE_EXTENSION]: Unknown
#     file extension ".ts"` on every test file. Measured on this host 2026-10-24:
#     v20.20.2 → 99 tests / 0 pass / 99 fail; v18.19.1 → `node: bad option:
#     --disable-warning=MODULE_TYPELESS_PACKAGE_JSON` before a test runs. Both
#     shapes read like a code defect, and one full 490s gate run was spent on it.
#
#     PINNED IN SIX LEGS, because "it refuses" alone is not the property:
#       a. a node ONE MINOR BELOW the declared floor is refused, and the message
#          names the node found, the floor, and that it is a version floor;
#       b. node 20.20.2 — the TRAP that satisfies playwright/next/eslint — is
#          refused the same way (a naive "major >= 20" check would pass this leg);
#       c. node 18.19.1, where the suite cannot even start, is refused;
#       d. a node AT the floor is NOT refused — the run must fall through to the
#          DEPENDENCY prerequisite, so the check is not a blanket refusal that
#          would pass a–c by refusing everything;
#       e. SABOTAGE: the same below-floor node against a COPY of the runner with
#          the node block stripped must NOT be refused as a version problem, so
#          a–c are attributable to the block under test and not to something else
#          in the script that happens to say "cannot run the web gate";
#       f. an a11y-ONLY selection under the same v20 node is NOT refused: that
#          gate never runs `test:unit`, so its floor is playwright's, and a single
#          global floor would restore the false red one layer down.
#     The stubs (see the block before check 5) fake only `-v/--version`.
node_probe() {  # <stub-dir> [repo] -> PROBE_OUT / PROBE_RC
  local dir="$1" repo="${2:-$SB}"
  PROBE_OUT="$(cd "$repo" && PATH="$dir:$PATH" ./scripts/preflight.sh --only web 2>&1)"
  PROBE_RC=$?
}
node_refusal_ok() {  # <stub-dir> -> 0 when THIS node is refused AS the version floor
  node_probe "$1" || true
  [ $PROBE_RC -ne 0 ] &&
    grep -q 'cannot run the web gate' <<<"$PROBE_OUT" &&
    grep -q 'VERSION floor, not a code defect' <<<"$PROBE_OUT" &&
    grep -q 'Playwright' <<<"$PROBE_OUT" &&
    ! grep -q 'gate \[web\]' <<<"$PROBE_OUT"
}
if [ -z "$NODE_FLOOR_RAW" ]; then
  bad "check 5d — NODE_FLOOR_TESTS could not be read out of the runner under test, so the node-version prerequisite's boundary cannot be asserted at all"
else
  # (a) one minor below the floor
  if node_refusal_ok "$NODE_STUB_DIR/below-floor" && grep -qF "$BELOW_FLOOR" <<<"$PROBE_OUT" \
     && grep -qF "$NF_MAJ.$NF_MIN" <<<"$PROBE_OUT"; then
    ok "check 5d — node $BELOW_FLOOR (one minor below the floor $AT_FLOOR) is refused as a PREREQUISITE naming the node found and both floors, before the gate runs (rc=$PROBE_RC)"
  else
    bad "check 5d — node $BELOW_FLOOR must be refused as a node-version prerequisite (rc=$PROBE_RC) [$(grep -iE 'PREFLIGHT FAIL|gate \[web\]' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  # (b) the trap: a node every other web tool accepts
  if node_refusal_ok "$NODE_STUB_DIR/v20" && grep -qF 'v20.20.2' <<<"$PROBE_OUT"; then
    ok "check 5d — node v20.20.2 (playwright/next/eslint all accept it) is refused too: the floor is the TYPE-STRIPPING one, not the tool-chain major"
  else
    bad "check 5d — node v20.20.2 must be refused (it satisfies every other web tool and cannot run test:unit), got rc=$PROBE_RC [$(grep -iE 'PREFLIGHT FAIL|gate \[web\]' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  # (c) a node where the suite cannot even start
  if node_refusal_ok "$NODE_STUB_DIR/v18" && grep -qF 'v18.19.1' <<<"$PROBE_OUT"; then
    ok "check 5d — node v18.19.1 ('node: bad option: --disable-warning=…') is refused the same way"
  else
    bad "check 5d — node v18.19.1 must be refused, got rc=$PROBE_RC [$(grep -iE 'PREFLIGHT FAIL|gate \[web\]' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  # (d) AT the floor: no refusal from the node check — the dependency
  #     prerequisite (no web/node_modules in this sandbox) is what stops it.
  node_probe "$NODE_STUB_DIR/good" || true
  if [ $PROBE_RC -ne 0 ] && ! grep -q 'cannot run the web gate' <<<"$PROBE_OUT" \
     && grep -q 'web/node_modules/.bin/next is missing' <<<"$PROBE_OUT"; then
    ok "check 5d — a node AT the floor ($AT_FLOOR) is NOT refused: the run falls through to the dependency prerequisite, so the check cannot pass by refusing everything"
  else
    bad "check 5d — a node at the floor ($AT_FLOOR) must NOT be refused by the version check; expected the dependency prerequisite (rc=$PROBE_RC) [$(grep -iE 'PREFLIGHT FAIL|gate \[web\]' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  # (f) THE FLOOR IS PER SELECTION. An a11y-only run never executes `test:unit`, so
  #     the same v20 trap node must NOT be refused there — its gate's floors are
  #     playwright's 20 and next's 20.9. Without this leg, simplifying the check to
  #     one global floor would reintroduce the false red one layer down.
  PROBE_OUT="$(PATH="$NODE_STUB_DIR/v20:$PATH" ./scripts/preflight.sh --only a11y 2>&1)"
  PROBE_RC=$?
  if [ $PROBE_RC -ne 0 ] && ! grep -q 'cannot run the a11y/e2e gate' <<<"$PROBE_OUT" \
     && grep -q 'web/node_modules/.bin/next is missing' <<<"$PROBE_OUT"; then
    ok "check 5d — an a11y-ONLY run under the same v20.20.2 is NOT refused: that gate never runs test:unit, so its floor is playwright's 20 / next's 20.9 (a single global floor would red it for nothing)"
  else
    bad "check 5d — an a11y-only run under v20.20.2 must NOT be refused on node version (its floor is playwright's), got rc=$PROBE_RC [$(grep -iE 'PREFLIGHT FAIL' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  # (e) sabotage: strip the block from a COPY outside $SB (a file added inside the
  #     sandbox would move the tree sha that later checks key their records on).
  MUT="$SB_STATE/nodefloor-mutant"
  rm -rf "$MUT"
  cp -a "$SB" "$MUT"
  MUT_SCRIPT="$MUT/scripts/preflight.sh"
  OPEN_HITS="$(grep -c '>>> node-version prerequisite' "$MUT_SCRIPT")"
  CLOSE_HITS="$(grep -c '<<< node-version prerequisite' "$MUT_SCRIPT")"
  sed -i '/>>> node-version prerequisite/,/<<< node-version prerequisite/d' "$MUT_SCRIPT"
  MUT_LEFT="$(grep -c 'node-version prerequisite' "$MUT_SCRIPT")"
  PROBE_OUT="$(cd "$MUT" && PREFLIGHT_RESULT_DIR="$SB_STATE/nodefloor-mutant-records" \
    PATH="$NODE_STUB_DIR/below-floor:$PATH" ./scripts/preflight.sh --only web 2>&1)"
  PROBE_RC=$?
  if [ "$OPEN_HITS" = "1" ] && [ "$CLOSE_HITS" = "1" ] && [ "$MUT_LEFT" = "0" ] \
     && [ $PROBE_RC -ne 0 ] && ! grep -q 'cannot run the web gate' <<<"$PROBE_OUT" \
     && grep -q 'web/node_modules/.bin/next is missing' <<<"$PROBE_OUT"; then
    ok "check 5d SABOTAGE — with the node block stripped from a copy, node $BELOW_FLOOR is NOT refused as a version problem (it reaches the dependency prerequisite): the refusals above are attributable to those bytes"
  else
    bad "check 5d SABOTAGE — the stripped-block copy must NOT refuse on node version: markers($OPEN_HITS/$CLOSE_HITS, left=$MUT_LEFT) rc=$PROBE_RC [$(grep -iE 'PREFLIGHT FAIL|gate \[web\]|node-version' <<<"$PROBE_OUT" | tr '\n' ' ')]"
  fi
  rm -rf "$MUT"
  unset PROBE_OUT PROBE_RC
fi

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

# 11b. The allowlist must COVER the Playwright spec form and must stay SCOPED to test
#      paths (card t_43005ced). WHY THIS IS A SEPARATE CHECK AND WHY IT IS HERE: the
#      instrument that proves this properly is `scripts/biteproof_gitleaks_allowlist.sh`,
#      and BITE-HARNESSES.md rules that a bite proof is an ON-DEMAND instrument, never
#      a per-merge gate — so without a check at this level NOTHING that runs on every
#      merge would notice the spec-path entry being deleted again.
#      THE DEFECT, MEASURED 2026-10-11: the allowlist carried `web/.*\.test\.(ts|tsx|js|jsx)`
#      but not the Playwright form, so ONE e2e fixture — an `await page.goto("/magic-link?token=…")`
#      line, matched by `generic-api-key` at entropy 4.0 — redded the `gitleaks` gate on
#      every tree in the repo, because `gitleaks detect --source .` scans EVERY REF and
#      not the tree under test. A merge lane blocked on it could not fix it from its own
#      diff: tree `3c1cd07d` recorded `gitleaks: pass` at 12:44:35Z and the SAME history
#      failed 30 minutes later only because a foreign branch was pushed.
#      THE PATTERNS ARE EVALUATED, NOT GREPPED FOR AS LITERALS: an entry re-typed with a
#      typo (`web/.*\.specs\.`) or one widened to `web/.*` would satisfy any
#      `grep -F`-style "does the line exist / does it not exist" test, so the check
#      matches sample PATHS through the pattern the way gitleaks does. The negative
#      direction is half the property: a pattern that also allowlists `web/src/…` would
#      stop a real secret there from ever being reported.
C11B_SPEC="web/e2e/magic-link-next.spec.ts"
C11B_NONTEST="web/src/app/page.tsx"
C11B_ANCHOR='web/.*\.spec\.(ts|tsx|js|jsx)'

# c11b_verdict CFG -> one config file's verdict:
#   ok:<pattern>     a PATH pattern matches a spec path, and none matches a non-test path
#   noscopepattern   no PATH pattern matches the spec form (the carded defect)
#   widen:<pattern>  some PATH pattern also allowlists a non-test path (the over-wide fix)
#   nopatterns       the [allowlist] paths block could not be read at all
c11b_verdict() {
  local cfg="$1" region paths pat spec="" widen=""
  region="$(tr '\n' ' ' < "$cfg" | sed -nE 's/.*paths[[:space:]]*=[[:space:]]*\[([^]]*)\].*/\1/p')"
  paths="$(grep -oE "'''[^']+'''" <<<"$region" | sed -e "s/'''//g")"
  if [ -z "$paths" ]; then
    printf 'nopatterns\n'
    return 0
  fi
  while IFS= read -r pat; do
    [ -n "$pat" ] || continue
    grep -Eq "$pat" <<<"$C11B_SPEC" && spec="$pat"
    grep -Eq "$pat" <<<"$C11B_NONTEST" && widen="$pat"
  done <<<"$paths"
  if [ -n "$widen" ]; then
    printf 'widen:%s\n' "$widen"
  elif [ -n "$spec" ]; then
    printf 'ok:%s\n' "$spec"
  else
    printf 'noscopepattern\n'
  fi
}

# THE CHECK'S OWN BITE, with no repo mutation: the SAME reader on a sabotaged copy of
# the same config (the spec-path entry removed) must NOT answer ok. A reader that
# answered ok for both the healthy and the sabotaged file could never go RED, which is
# the exact failure mode this file exists to catch. The mutant lives in the host state
# dir, never in the sandbox repo — a file added to $SB would move the tree sha that
# later checks key their records on.
c11b_live="$(c11b_verdict "$CFG")"
c11b_live_anchor="$(grep -cF "$C11B_ANCHOR" "$CFG")"
c11b_mut="$SB_STATE/gitleaks-nospec.toml"
grep -vF "$C11B_ANCHOR" "$CFG" > "$c11b_mut"
c11b_mut_left="$(grep -cF "$C11B_ANCHOR" "$c11b_mut")"
c11b_mut_verdict="$(c11b_verdict "$c11b_mut")"
if [ ! -f "$CFG" ]; then
  bad "check 11b — .gitleaks.toml is missing at $CFG, so the secrets allowlist's scoping cannot be asserted at all"
elif [ "$c11b_live_anchor" = "1" ] && [ "$c11b_mut_left" != "0" ]; then
  bad "check 11b is VACUOUS — the sabotaged config still carries the spec-path entry ($c11b_mut_left time(s)), so the bite leg below would be measuring the healthy file"
elif [ "$c11b_mut_verdict" != "noscopepattern" ]; then
  bad "check 11b CANNOT GO RED — the same reader answers [$c11b_mut_verdict] for a config with the spec-path entry removed, so its green on the live config proves nothing about the entry"
else
  case "$c11b_live" in
    ok:*)
      ok "check 11b — the [allowlist] covers the Playwright spec form by PATH ([${c11b_live#ok:}] matches $C11B_SPEC) and stays scoped to test files (no PATH pattern matches $C11B_NONTEST)"
      ;;
    widen:*)
      bad "check 11b — the [allowlist] PATH pattern [${c11b_live#widen:}] matches the NON-test path $C11B_NONTEST as well: an over-wide entry stops a real secret there being reported. Test paths and only test paths belong in the list"
      ;;
    noscopepattern)
      bad "check 11b — no [allowlist] PATH pattern matches a Playwright spec path ($C11B_SPEC). The e2e specs carry deliberately token-shaped fixtures and the secrets scan reads EVERY ref (not the tree under test), so one such fixture reds the gate on every tree in the repo — add '''web/.*\\.spec\\.(ts|tsx|js|jsx)''' to the paths list"
      ;;
    *)
      bad "check 11b — the [allowlist] paths block could not be read out of $CFG (verdict [$c11b_live]); the scoping of the secrets allowlist is unverifiable in this shape"
      ;;
  esac
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
#
#     THE DETAIL HALF (card t_d6ce3d60). The long prose — what each gate catches, the
#     spec counts, the history — lives in one file per gate under `gate-inventory/`,
#     because the rows are one physical line each and the `a11y`/`e2e` rows are
#     adjacent: two branches that each rewrote one row conflicted on every merge
#     (thirteen rework cards). So the row set and the FILE set are checked in BOTH
#     directions here — a row whose detail file is gone has lost the only place its
#     figure may be edited, and a file with no row is a gate nobody can run — and no
#     table row may carry a spec-count figure (a figure in a row is the edit that
#     starts the conflict again). Check 50 proves the split MERGES where the old
#     shape conflicts.
INV="$HERE/../GATE-INVENTORY.md"
NOTES="$HERE/../gate-inventory"
if [ ! -f "$INV" ]; then
  bad "GATE-INVENTORY.md not found at $INV — the documented gate table is missing, so nothing checks it against --list"
else
  # The first backticked token at the start of a table row is a gate NAME. Parsing
  # must ignore the prose above the table and the "See also" line below it, and
  # must NOT pick up backticked commands in the other columns.
  documented="$(grep -E '^\|' "$INV" | grep -vE '^\|[[:space:]]*-' \
    | awk -F'|' '{print $2}' | grep -oE '`[A-Za-z0-9_-]+`' | tr -d '`' | sort -u)"
  if [ -z "$documented" ]; then
    bad "GATE-INVENTORY.md has a table but no gate names could be parsed from column 1 — a guard that reads nothing proves nothing"
  elif [ "$documented" = "$(printf '%s\n' $GATES | sort -u)" ]; then
    ok "GATE-INVENTORY.md documents exactly the $NGATES gates --list advertises (no drift)"
  else
    bad "gate-table drift — in the runner but NOT documented: [$(comm -13 <(printf '%s\n' "$documented") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')] / documented but NOT runnable: [$(comm -23 <(printf '%s\n' "$documented") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')]"
  fi

  # 15-i. The detail files, both directions.
  if [ ! -d "$NOTES" ]; then
    bad "gate-inventory/ is missing at $NOTES — each gate's detail (what it catches, its spec counts, its history) is supposed to live in its own file so that two gates' edits cannot conflict; with no directory the table has no detail behind it"
  elif [ -z "$documented" ]; then
    bad "cannot check the gate detail files: no gate name could be parsed from GATE-INVENTORY.md's table"
  else
    missing_notes=""; orphan_notes=""
    for g in $documented; do
      [ -f "$NOTES/$g.md" ] || missing_notes="$missing_notes $g"
    done
    nfiles=0
    for f in "$NOTES"/*.md; do
      [ -e "$f" ] || continue
      nfiles=$((nfiles + 1))
      b="$(basename "$f" .md)"
      grep -qxF -- "$b" <<<"$documented" || orphan_notes="$orphan_notes $b"
    done
    if [ "$nfiles" -eq 0 ]; then
      bad "gate-inventory/ holds no .md file — every gate's detail file is missing, so a guard that reads nothing would pass"
    elif [ -n "$missing_notes" ]; then
      bad "GATE-INVENTORY.md has no gate-inventory/<gate>.md detail for:$missing_notes — add that file in the same change as the row. The detail (prose, spec counts, history) belongs there and NOT in the row, because the rows are adjacent one-liners whose edits conflict on merge (card t_d6ce3d60)"
    elif [ -n "$orphan_notes" ]; then
      bad "gate-inventory/ has a detail file for a gate that is NOT a runnable, documented gate:$orphan_notes — every file there must be named for a row in GATE-INVENTORY.md, or it is a gate nobody can run"
    elif [ "$nfiles" -ne "$NGATES" ]; then
      bad "gate-inventory/ holds $nfiles detail files for $NGATES gates — the sets must agree exactly (no shared file, no missing gate)"
    else
      ok "gate-inventory/ carries exactly one detail file per documented gate ($nfiles), and every row has its own — no gate's detail shares a file with another's"
    fi
  fi

  # 15-ii. A figure in a ROW is the edit that re-creates the adjacency trap: those
  #        two rows are one line each and next to each other, so a count edit in one
  #        conflicts with a count edit in the other. The figure lives in the detail
  #        file; `speccounts` reads it there.
  row_figs="$(grep -cE '^\|.*\*\*[0-9]+ tests? in [0-9]+ files?\*\*' "$INV" 2>/dev/null || true)"
  if [ "${row_figs:-0}" -ne 0 ]; then
    bad "$row_figs table row(s) in GATE-INVENTORY.md carry a \`**N tests in M files**\` figure. The rows are adjacent one-liners: a count in one row is what made every a11y/e2e rewrite a merge conflict (13 rework cards). Put the figure in that gate's gate-inventory/<gate>.md — the FIRST such span there is the current one"
  else
    ok "no table row carries a spec-count figure — the mutating detail (incl. \`**N tests in M files**\`) lives only in the per-gate files, so a count edit cannot land in the a11y/e2e adjacency"
  fi
fi

# 16. The self-test must run on its OWN pool path, never the production one.
#     Every invocation above goes through scripts/preflight.sh, which takes a slot
#     from the host pool BEFORE it validates arguments or prerequisites — so on a
#     busy board this whole self-test would otherwise wait for a slot (up to
#     PREFLIGHT_LOCK_WAIT, 1800s by default) and, once running, hold that same
#     slot against every card waiting behind IT. Measured (before the pool existed,
#     on the single lock): a self-test that should take seconds sat 10+ minutes
#     holding the production lock with four cards running. Two properties, checked
#     against the SCRIPT (not this file's prose, which could be edited
#     independently):
#       (a) the default is still the production path — a self-test that pointed at
#           a private path by DEFAULT would be a real regression, because the gate
#           body would then be uncapped;
#       (b) this run's resolved path is NOT that default — i.e. the override above
#           actually took effect and is still in the file, so the pool the live
#           gates use is not the pool this self-test drives.
#     (b) reads the live value, so deleting the export makes this RED.
default_lock="${PREFLIGHT_LOCK_FILE_DEFAULT:-/var/lock/jomform-preflight-gate.lock}"
default_count="${PREFLIGHT_SLOT_COUNT_DEFAULT:-2}"
if [ ! -f "$PREFLIGHT" ]; then
  bad "cannot check self-test pool isolation: $PREFLIGHT missing"
elif ! grep -qE 'PREFLIGHT_LOCK_FILE="\$\{PREFLIGHT_LOCK_FILE:-' "$PREFLIGHT"; then
  bad "preflight.sh no longer defaults PREFLIGHT_LOCK_FILE through \${PREFLIGHT_LOCK_FILE:-...} — the override this self-test relies on is gone"
elif ! grep -qE 'PREFLIGHT_SLOT_COUNT="\$\{PREFLIGHT_SLOT_COUNT:-' "$PREFLIGHT"; then
  bad "preflight.sh no longer derives the pool size through \${PREFLIGHT_SLOT_COUNT:-...} — the cap this self-test measures is gone, and a hardcoded 1 would be the old single lock again"
elif [ -z "${PREFLIGHT_LOCK_FILE:-}" ]; then
  bad "PREFLIGHT_LOCK_FILE is unset in the self-test — it would join the production pool and hold a slot against live gates"
elif [ "$PREFLIGHT_LOCK_FILE" = "$default_lock" ]; then
  bad "the self-test is using the PRODUCTION pool path ($default_lock) — it will contend with (and hold a slot against) every card gate on the board"
else
  ok "self-test uses a private pool path ($PREFLIGHT_LOCK_FILE), not the production one ($default_lock)"
fi

# 16b. THE POOL MUST ACTUALLY CAP, AND A WAITER MUST STILL GET TO RUN.
#
#     This is the card's own subject, so it is asserted in both directions rather
#     than reasoned about:
#       (a) with every slot held, a run must NOT execute its gate — the cap is
#           real, and the old single lock's guarantee (never more runs at once
#           than the cap) is preserved. Asserted on the SUMMARY LINE and on the
#           gate banner, both of which are printed only after the gate body runs;
#       (b) once a slot frees, that same run must RUN the gate rather than fail —
#           a waiter is the correct answer, and this leg is what stops (a) from
#           being satisfied by a runner that simply refuses under contention.
#     Held by a real `flock` on the SAME slot paths the runner derives (one file
#     per slot, `<path>.slot<N>`), in the self-test's own private pool dir, so this
#     is hermetic and never touches the host's gate.
#
#     A HOLDER IS TAKEN ON AN INDEPENDENTLY-OPENED DESCRIPTOR. That is not a
#     detail: flock is OPEN-FILE-DESCRIPTION scoped, so a holder that inherited a
#     descriptor would share it and the take would succeed — the check would then
#     report "capped" about a pool that never capped. The runner's own take is
#     re-entrant BY DESIGN for exactly that reason (a nested invocation must not
#     queue behind its own parent), so a holder which is not a descendant is what
#     makes this leg measure the CAP rather than the re-entrancy.
pool_dir="$SB_STATE/pool"
rm -rf "$pool_dir"; mkdir -p "$pool_dir"
pool_lock="$pool_dir/gate.lock"
s16_gofmt_label='cd api && test -z "$(gofmt -l .)"'
# The readiness bound: a LIVENESS bound, like $s16_bound below, never a measured
# quantity. The holder needs two `flock` takes, so this only fires for a holder
# that never got scheduled.
s16_ready_bound=15
# The holder takes every slot the runner would try, in ITS OWN process, and holds
# them while the subject run takes its turn. `-o` so the lock lives on this
# descriptor rather than dying with an inner `true`.
#
# NO `2>/dev/null` on any redirection-only `exec` here either: it would apply to
# the SHELL, not to a command (that mistake made 21 checks in this file report an
# empty output while the exit code still changed).
# `$1` (optional) is a READINESS SENTINEL path: the holder writes it only after
# BOTH slots are held and the premise probe below has proved a fresh take is
# refused — i.e. the moment the pool is genuinely full for anyone else. The
# caller waits on that file instead of a fixed sleep (card t_458c2578): a
# `sleep 1` was a start BUDGET, not a guarantee, so on a loaded host the holder's
# two `flock` takes could take longer than 1s, the subject then started against a
# not-yet-full pool, took the free slot, answered rc=0, and the check reported a
# spurious RED about the cap. The sentinel turns that race into a wait, and it is
# written HERE (after the probe) rather than at the start, so its presence is
# evidence the pool is full and not merely that the subshell has been scheduled.
pool_hold() {
  local ready="${1:-}" n fd
  for n in $(seq 1 "$default_count"); do
    fd=""
    exec {fd}>>"$pool_lock.slot$n" || return 1
    if ! flock -o -n "$fd"; then exec {fd}>&-; return 1; fi
    # Keep the descriptor open for the lifetime of this subshell.
    eval "hold_fd_$n=\$fd"
  done
  # Prove the premise: the pool this function just filled must refuse a fresh take
  # on a NEWLY opened descriptor, or the run below would take a slot and this leg
  # would be measuring nothing.
  local probe=""
  exec {probe}>>"$pool_lock.slot1" || return 1
  if flock -n "$probe" 2>/dev/null; then
    exec {probe}>&-
    return 2
  fi
  exec {probe}>&-
  [ -n "$ready" ] && : >"$ready"
  sleep 4
}
# Wait, BOUNDED, for a holder's readiness sentinel (card t_458c2578). 0 = the
# sentinel appeared (pool full, premise proved); 1 = the holder exited WITHOUT
# writing it (its `pool_hold` returned 1 or 2); 2 = neither within the bound. The
# caller must treat 1/2 as "the holder failed to fill the pool" and report THAT,
# rather than running the subject against a pool that may be empty and
# mis-attributing the result — which is the second half of the defect the fixed
# `sleep 1` caused.
#
# THE BOUND IS ONE LITERAL (`$s16_ready_bound`), not a number repeated in each
# message: a message that hardcodes "15s" can outlive the bound it describes, and
# a wrong CAUSE named in a gate log is what sends an operator chasing host load
# instead of the pool (card t_d0cceae1).
wait_pool_hold_ready() {  # $1 sentinel path, $2 holder pid, $3 bound (default $s16_ready_bound)
  local sentinel="$1" holder="$2" bound="${3:-$s16_ready_bound}" deadline
  deadline=$(( $(date +%s) + bound ))
  while [ ! -f "$sentinel" ]; do
    kill -0 "$holder" 2>/dev/null || return 1
    [ "$(date +%s)" -lt "$deadline" ] || return 2
    sleep 0.1
  done
  return 0
}
s16_out=""
s16_ready="$SB_STATE/s16b.ready"
rm -f "$s16_ready"
pool_hold "$s16_ready" &
s16_holder=$!
s16_ready_rc=0
wait_pool_hold_ready "$s16_ready" "$s16_holder" || s16_ready_rc=$?
# WAIT=0 so the assertion cannot hang the self-test: a runner that (wrongly)
# proceeds answers immediately, and one that waits exits 74 immediately.
# `env -u` for the inheritance markers: the guard at the top of this file clears
# them for THIS shell, but an exported one would still reach the child, and this is
# the check that measures the TAKE.
s16_out="$( (cd "$LIVE_ROOT" && env -u PREFLIGHT_SLOT_FD -u PREFLIGHT_SLOT_HOLDER -u PREFLIGHT_SLOT_N -u PREFLIGHT_SLOT_LOCK \
    PREFLIGHT_RESULT_DIR="$SB_STATE/pool-results" PREFLIGHT_RESULT_TTL=1 \
    PREFLIGHT_LOCK_FILE="$pool_lock" PREFLIGHT_LOCK_WAIT=0 \
    ./scripts/preflight.sh --only gofmt 2>&1) )"
s16_rc=$?
s16_gate_ran=0
grep -qF "$s16_gofmt_label" <<<"$s16_out" && s16_gate_ran=1
s16_waited=0
grep -q "all ${default_count} gate slot(s) busy" <<<"$s16_out" && s16_waited=1
s16_hold_rc=0
wait "$s16_holder" 2>/dev/null; s16_hold_rc=$?
# THE HOLDER IS CHECKED, and this is the SECOND half of the flake (card
# t_458c2578). `wait … 2>/dev/null` used to discard the holder's status, so when
# `pool_hold` returned non-zero — 1 if it could not take a slot, 2 if its own
# premise probe found slot1 still takeable — this check ran the subject against a
# pool that was NOT full and reported whatever came back, mis-attributing a
# broken HOLDER to the runner under test. The subject must not be read at all when
# the premise failed; a holder that did not fill the pool proves nothing about the
# runner. Reported as its own clear red (a `bad`), never as a silent pass-through.
if [ "$s16_ready_rc" -eq 1 ]; then
  bad "16b: the pool HOLDER exited without filling the pool (pool_hold rc=$s16_hold_rc — its slot take or premise probe failed), so the subject run cannot be attributed to a full pool — nothing about the runner's cap is measured by this run"
elif [ "$s16_ready_rc" -eq 2 ]; then
  bad "16b: the pool HOLDER did not report the pool full within ${s16_ready_bound}s (pool_hold rc=$s16_hold_rc) while the host is loaded — raising a fixed start budget would not fix this (that was the old bug); the host could not schedule two flock takes in ${s16_ready_bound}s, so the check cannot measure the runner here"
elif [ "$s16_hold_rc" -ne 0 ]; then
  bad "16b: the pool HOLDER wrote its readiness sentinel but then returned rc=$s16_hold_rc (pool_hold should hold for its sleep and return 0) — the holder's own premise is not trustworthy, so the runner's rc is not a statement about the cap"
elif [ "$s16_rc" -eq 0 ]; then
  bad "16b does not bite: with all $default_count slot(s) held the run COMPLETED (rc=0) instead of waiting — the pool is not capping, so two lanes would run at once with no bound (output: [$(printf '%s' "$s16_out" | grep -E 'slots:|gate \[gofmt\]|summary' | tr '\n' ' ')]"
elif [ "$s16_gate_ran" -ne 0 ]; then
  bad "16b does not bite: the run reported it was waiting for a slot AND executed the gate — the cap is not real (output: [$(printf '%s' "$s16_out" | grep -E 'slots:|gate \[gofmt\]' | tr '\n' ' ')]"
elif [ "$s16_waited" -ne 1 ]; then
  bad "16b: with every slot held the run did not report the wait, so this check cannot tell a capped runner from a failing one (rc=$s16_rc, output: [$(printf '%s' "$s16_out" | tail -n 3 | tr '\n' ' ')]"
elif [ "$s16_rc" -ne 74 ]; then
  bad "16b: a full pool must be a WAIT, not a failure — expected rc=74 (could not get a slot) with PREFLIGHT_LOCK_WAIT=0, got rc=$s16_rc"
else
  ok "16b BITES: with all $default_count slot(s) held the run REPORTS the busy pool and exits 74 without executing a gate — the cap is real, and it is a wait rather than a refusal"
fi

# 16c AND 16f. THE OTHER DIRECTION OF 16b: once a slot frees, the waiter must RUN
#      — and it must run while SLOT 1 IS STILL HELD (card t_1c59f9f5). Without
#      this leg 16b is satisfied by a runner that always refuses under contention,
#      which is the false-red shape the pool exists to avoid. The wait used to be
#      one blocking `flock` on slot 1, so a run proceeded only when SLOT 1 freed
#      and exited 74 while another slot was free; the run must instead proceed on
#      the freed slot 2..N, which is exactly the ordering a slot-1-only wait is
#      blind to.
#
#      MEASURED AS A PROPERTY, NOT AGAINST A WALL-CLOCK CONSTANT (card
#      t_92e2e4ae). This leg used to assert `elapsed < 12s`, where `elapsed` was
#      the TOTAL wall clock of the subject run — its own start, the wait, the
#      gate, the record write — against a fixed 12s. That constant was never the
#      measured quantity, and everything OUTSIDE the wait inflates with host load.
#      Measured on this host at load 33 on 6 cores, the SAME bytes gave
#      elapsed=16.24s with the wait demonstrably CORRECT: the run announced its
#      gate 4.0s after slot 2 was freed and 8s BEFORE slot 1 was released (the
#      gate itself then cost 7s). The old assertion read that as "it waited out
#      the slot-1 hold" and turned a real full gate run RED (card t_d7553726, tree
#      fb8aea76, 17 passed / 1 failed) — a false red that costs every unlucky lane
#      a 200s re-run and reads as a defect in the tree under test.
#
#      So the assertion is now the property that sentence always meant: THE RUN
#      ANNOUNCED ITS GATE WHILE SLOT 1 WAS STILL HELD, with a FRESH non-blocking
#      take on slot 1 REFUSED at that moment to prove it. Slot 1 is released BY
#      THE CHECK, only after that moment, so the assertion holds at any host speed
#      — and a slot-1-only waiter cannot satisfy it, at any host speed.
#
#      THE PREMISE IS A WAIT, NOT A HOPE (the 16b/16e pattern, card t_458c2578).
#      The pool stays FULL until the SUBJECT ITSELF reports it is waiting, so "the
#      pool was full at the subject's own decision" cannot be lost to a loaded
#      host delaying the subject's start. The old form freed slot 2 on a 2s timer
#      whatever the subject was doing, so a subject that reached the pool after
#      that simply took the free slot and never waited at all.
#
# The two markers these legs key on, both taken from the runner's OWN output
# rather than assumed: its wait line, and its gate announcement.
s16_poolfull='gate slot(s) busy'
s16_gofmt_banner='==> gate [gofmt]:'
# $1 of wait_file_line and of pool_wake_leg is a LIVENESS bound, never the
# measured quantity: the runner polls its slots at <=1s intervals, so a bound an
# order of magnitude over that only fires for a subject that never gets there.
s16_bound=20
# The subject's own slot-wait budget must OUTLAST that bound: were the runner's
# deadline to expire first, the leg would be reading the runner's timeout rather
# than the pool.
s16_wait=90

# Wait, BOUNDED, for a fixed string to appear in a file a subject is writing AS IT
# RUNS. 0 = the line appeared, 1 = the bound ran out (a file that does not exist
# yet counts as "not yet", so this is a wait rather than a race).
wait_file_line() {  # $1 file, $2 fixed string, $3 seconds
  local f="$1" pat="$2" deadline
  deadline=$(( $(date +%s) + $3 ))
  while :; do
    grep -qF "$pat" "$f" 2>/dev/null && return 0
    [ "$(date +%s)" -lt "$deadline" ] || return 1
    sleep 0.1
  done
}

# pool_wake_leg <tag> <bound> <subject argv...> — drive ONE runner against a pool
# that stays FULL until the SUBJECT ITSELF reports it is waiting, then free slot
# 2..N while slot 1 stays held, and record WHAT the runner did.
#
# THE SAME MEASUREMENT IS APPLIED TO TWO SUBJECTS — the live runner (16c) and a
# copy whose pool is a single lock on slot 1 (16f) — so neither verdict can come
# from a difference in the measurement itself.
#
# THE PREMISE IS PROVED BEFORE THE SUBJECT IS CREATED AT ALL (card t_d0cceae1).
# This function used to start the holder and the subject CONCURRENTLY, then wait
# for the holder's readiness sentinel — i.e. it asked the subject to behave as if
# the pool were full while both processes were racing for the same slots with a
# NON-BLOCKING `flock`. Whichever reached the pool first took a slot and the LOSER
# exited: when the subject won (a loaded host delaying the holder's fork/`flock`
# past the subject's own startup), the holder's `flock -o -n` was refused, the
# holder `exit 1`-ed, and the leg reported
#     "the pool HOLDER did not fill the pool within 15s (readiness rc=1)"
# while the subject had in fact taken the free slot, RUN its gate and exited 0 —
# a wrong cause AND a subject read off a pool that was never full. Measured by
# delaying only the holder's slot take by 0.3s (standing in for a loaded host):
# 16c and 16f both red on that exact line, 3 of the 5 leg checks green. In a full
# gate run this reds a 3464s run and reads as a defect in the tree under test
# (card t_a940a635, tree db7130a8). So: fill and PROVE the pool first, and start
# no subject while the premise is unproven.
#
# Sets, for the caller:
#   PW_RESULT    held | held_unverified | after | never | nofull
#                | holder_died | holder_late
#   PW_RC        the subject's exit status — EMPTY when no subject was started
#   PW_READY_RC  the holder's readiness status (0 = the pool was full)
#   PW_HOLDER_RC the holder's OWN exit status (empty until it has been reaped)
#   PW_DETAIL    one line naming WHY a premise failed (holder_died/holder_late),
#                so the verdict a caller prints can never name the wrong cause
#   PW_OUT       the file the subject's output is streamed to, read WHILE it runs;
#                it does not EXIST when no subject was started
#   PW_ELAPSED   the subject's wall clock — REPORTED in the messages and never
#                asserted on: it is the number whose load dependence reddened a
#                real full gate run while the wait was correct.
pool_wake_leg() {  # $1 tag, $2 bound seconds, $3.. the subject's argv
  local tag="$1" bound="$2"; shift 2
  local ready="$SB_STATE/$tag.ready" sfree="$SB_STATE/$tag.free" srel="$SB_STATE/$tag.release"
  local holder pid="" start
  PW_OUT="$SB_STATE/$tag.out"
  PW_RESULT=never; PW_RC=""; PW_ELAPSED=""; PW_READY_RC=0; PW_HOLDER_RC=""; PW_DETAIL=""
  rm -f "$ready" "$sfree" "$srel" "$PW_OUT"
  # THE HOLDER: every slot, held until the subject reports the full pool (bounded),
  # then slots 2..N freed and slot 1 held until the CHECK releases it (bounded).
  # Both triggers are FILES, so the ordering this leg measures is the subject's own
  # observation rather than a timer.
  # FIXED DESCRIPTOR NUMBERS, closed by NUMBER — no `eval` and no descriptor this
  # shell might already be using. `eval "exec \${var}>&-"` looks like the obvious
  # close-by-variable, but it is not reliably a redirection: when it degrades to
  # `exec <num>` bash tries to EXECUTE a command named by the digit, and a failed
  # `exec` in a non-interactive shell EXITS the subshell — which releases EVERY
  # slot it holds, so the leg would measure an empty pool instead of a hold.
  # Slot `n` therefore lives on fd `$((100+n))`, which nothing else here opens.
  (
    for n in $(seq 1 "$default_count"); do
      eval "exec $((100+n))>>\"\$pool_lock.slot\$n\"" || exit 1
      flock -o -n "$((100+n))" || exit 1
    done
    # The same premise probe as pool_hold (card t_458c2578): a FRESH take on a slot
    # this process holds must be REFUSED, or the pool being measured is not full
    # for anyone else and the subject's wait line would mean something else.
    probe=""
    exec {probe}>>"$pool_lock.slot1" || exit 1
    if flock -n "$probe" 2>/dev/null; then exec {probe}>&-; exit 2; fi
    exec {probe}>&-
    echo "  $tag: all $default_count slot(s) held"
    : >"$ready"
    _w=0
    while [ ! -f "$sfree" ] && [ "$_w" -lt 1200 ]; do sleep 0.1; _w=$((_w+1)); done
    for n in $(seq 2 "$default_count"); do
      eval "exec $((100+n))>&-"
    done
    echo "  $tag: slot(s) 2..$default_count FREED on the subject's own pool-full report; slot1 still HELD"
    _w=0
    while [ ! -f "$srel" ] && [ "$_w" -lt 1800 ]; do sleep 0.1; _w=$((_w+1)); done
    echo "  $tag: slot1 released"
  ) &
  holder=$!
  # THE PREMISE FIRST, AND NO SUBJECT UNTIL IT HOLDS (card t_d0cceae1 — see the
  # header). 16b's rule, one handoff over: a reading taken against a pool that was
  # not full proves nothing, so the subject process is not created at all while the
  # premise is unproven.
  wait_pool_hold_ready "$ready" "$holder" "$s16_ready_bound" || PW_READY_RC=$?
  if [ "$PW_READY_RC" -ne 0 ]; then
    # End the holder's holds BEFORE reaping it, exactly as the outcome path below
    # does: a bound that expires leaves a LIVE holder, and parking this leg on the
    # holder's own 120s/180s bounds would leave the sandbox pool occupied (16d's
    # premise takes a fresh look at a held slot).
    : >"$sfree"
    : >"$srel"
    wait "$holder" 2>/dev/null; PW_HOLDER_RC=$?
    # TWO FAILURE EXITS, TWO CAUSES — named, never merged into one "15s" story.
    #   1 = the holder process was GONE (its slot take or premise probe failed);
    #   2 = the readiness BOUND expired under a holder that was still alive.
    # They route an operator to different places (the pool vs the host), so the
    # verdict must not report one as the other.
    if [ "$PW_READY_RC" -eq 1 ]; then
      PW_RESULT=holder_died
      PW_DETAIL="the pool HOLDER exited before it had filled the pool (holder status ${PW_HOLDER_RC:-?}) — its own slot take or its premise probe failed, so NO subject was started and nothing about the runner is measured here"
    else
      PW_RESULT=holder_late
      PW_DETAIL="the pool HOLDER was still alive but had not proved the pool full within ${s16_ready_bound}s (holder status ${PW_HOLDER_RC:-?}), so the readiness BOUND expired and NO subject was started — this is the harness waiting out its own bound, not the runner's cap"
    fi
    return 0
  fi
  # `env -u` for the inheritance markers, exactly as 16b/16e do: this is the leg
  # that measures the TAKE and the WAIT, so an inherited marker must not
  # short-circuit it.
  (
    cd "$LIVE_ROOT" || exit 1
    exec env -u PREFLIGHT_SLOT_FD -u PREFLIGHT_SLOT_HOLDER -u PREFLIGHT_SLOT_N -u PREFLIGHT_SLOT_LOCK \
      PREFLIGHT_RESULT_DIR="$SB_STATE/pool-results" PREFLIGHT_RESULT_TTL=1 \
      PREFLIGHT_LOCK_FILE="$pool_lock" PREFLIGHT_LOCK_WAIT="$s16_wait" \
      "$@"
  ) >"$PW_OUT" 2>&1 &
  pid=$!
  start=$(date +%s)
  if ! wait_file_line "$PW_OUT" "$s16_poolfull" "$bound"; then
    PW_RESULT=nofull
  else
    # The subject has reported a FULL pool, so the free that follows is the FIRST
    # slot it can possibly wake on — the ordering a slot-1-only wait is blind to.
    : >"$sfree"
    if wait_file_line "$PW_OUT" "$s16_gofmt_banner" "$bound"; then
      # PROVE slot 1 was still held AT THAT MOMENT rather than trusting the holder
      # to still be holding it: flock refusal is open-file-description scoped, so a
      # fresh non-blocking take by this process is REFUSED while the holder lives.
      # If it SUCCEEDS the pool had already emptied and the proceed proves nothing
      # — and the probe is closed at once, because this leg leaves the pool as it
      # found it.
      probe=""
      if exec {probe}>>"$pool_lock.slot1"; then
        if flock -n "$probe" 2>/dev/null; then PW_RESULT=held_unverified; else PW_RESULT=held; fi
        exec {probe}>&-
      fi
    else
      # Nothing ran its gate while slot 1 was held. The subject's output is only
      # ever APPENDED to, so whatever appears after the release below appeared
      # AFTER it — the attribution needs no clock, only that ordering.
      PW_RESULT=after_or_never
    fi
  fi
  # End the holder's holds whatever the outcome, BEFORE waiting for it, or a
  # premise failure would park this leg on the holder's own bound and leave the
  # sandbox pool occupied (16d's premise takes a fresh look at a held slot).
  : >"$sfree"
  : >"$srel"
  # THE HOLDER'S OWN STATUS IS READ, NEVER DISCARDED (card t_458c2578's rule, which
  # 16b and 16e already apply and this leg did not): the holder IS the pool this leg
  # measures, so a holder that died mid-leg means the pool was not full from that
  # point on and the subject's behaviour cannot be attributed to the wait. Reported
  # as a premise failure naming the status — never as a verdict about the runner.
  wait "$holder" 2>/dev/null; PW_HOLDER_RC=$?
  wait "$pid" 2>/dev/null; PW_RC=$?
  PW_ELAPSED=$(( $(date +%s) - start ))
  if [ "$PW_RESULT" = after_or_never ]; then
    if grep -qF "$s16_gofmt_banner" "$PW_OUT" 2>/dev/null; then PW_RESULT=after; else PW_RESULT=never; fi
  fi
  if [ "$PW_HOLDER_RC" -ne 0 ]; then
    PW_RESULT=holder_died
    PW_DETAIL="the pool HOLDER exited mid-leg (holder status $PW_HOLDER_RC), so the pool stopped being full while the subject ran and its rc=$PW_RC cannot be attributed to the wait this leg measures — the reading is discarded"
  fi
}
# 16c. THE WAIT WATCHES EVERY SLOT. The LIVE runner is the subject: with the pool
#      held until it says it is waiting, then slot 2..N freed while slot 1 stays
#      held, it must run its gate — and a FRESH take on slot 1 must be refused at
#      that moment. A runner that watched slot 1 alone could not do that; that is
#      the defect card t_1c59f9f5 fixed, and 16f is the copy that proves this
#      assertion still rejects it.
pool_wake_leg s16c "$s16_bound" ./scripts/preflight.sh --only gofmt
s16c_out="$(cat "$PW_OUT" 2>/dev/null)"
case "$PW_RESULT" in
  held)
    ok "16c BITES: with slot 2..$default_count freed while slot1 stayed held, the run RUNS its gate anyway and a FRESH take on slot1 was still refused at that moment — the wait watches EVERY slot, not slot 1 alone (rc=$PW_RC, ${PW_ELAPSED}s total)"
    ;;
  holder_died|holder_late)
    bad "16c PREMISE: $PW_DETAIL"
    ;;
  nofull)
    bad "16c PREMISE: the subject never reported a full pool within ${s16_bound}s (rc=$PW_RC), so the pool was not full at the subject's own decision and this leg measured NOTHING about the wait — output: [$(printf '%s' "$s16c_out" | grep -E 'slots:|gate \[gofmt\]|preflight:' | tr '\n' ' ')]"
    ;;
  held_unverified)
    bad "16c: the run ran a gate while slot 2..$default_count were free, but slot1 was free TOO at that moment (a fresh take succeeded) — the pool had already emptied, so the proceed cannot be attributed to the wait (rc=$PW_RC)"
    ;;
  after)
    bad "16c: the run did not run a gate while slot1 was held (${s16_bound}s with slot 2..$default_count free) and only ran it after slot1 was RELEASED — the wait is blind to the other slot(s) (rc=$PW_RC, ${PW_ELAPSED}s total)"
    ;;
  never)
    bad "16c: a queued run must execute its gate once ANY slot frees (slot1 held, slot 2..$default_count freed on the subject's own pool-full report) — it never ran one (rc=$PW_RC), output: [$(printf '%s' "$s16c_out" | grep -E 'slots:|gate \[gofmt\]|preflight:' | tr '\n' ' ')]"
    ;;
  *)
    bad "16c: the pool leg returned an unexpected result [$PW_RESULT]"
    ;;
esac

# 16d. AN INHERITED SLOT MUST NOT BE QUEUED ON. A nested invocation that inherits
#      its ancestor's slot is already inside the pool — it must run its gates, not
#      wait for a slot its own ancestor is holding. That is the self-deadlock shape
#      card t_0094bf00 was about (a lane's queue grew 3 -> 9 runs with no gate
#      executing), one level down, and the `selftest` gate IS such a nested
#      invocation, so this is the path a real full run takes.
#
#      DRIVEN WITH THE REAL INHERITANCE, not a simulation: a wrapper takes every
#      slot, exports the marker its own child would inherit, and runs the subject
#      preflight.sh from there with the pool full. A runner that ignores the
#      ancestry would exit 74 on the full pool; the correct one runs its gate.
#      The wrapper asserts its premise first — a FRESH open of a slot is refused
#      while it holds them — so a run that proceeds did so because of the
#      inheritance rather than because the pool was empty.
#
#      A LOCK TAKEN BY A DESCENDANT CANNOT STAND IN FOR THE ANCESTOR'S SLOT: flock
#      refusal is open-file-description scoped (measured), so the descendant's take
#      on the same file is refused and it has to be the ANCESTOR's slot that is
#      recognised. That is exactly what the marker's holder pid encodes.
#
#      THE WRAPPER IS THE `bash -c` BELOW, so when the ancestor's markers are
#      already inherited (`selftest` driven by a holding run) it re-uses them and
#      still asserts the pool is full; when they are not, it takes them itself.
s16d_out="$( (cd "$LIVE_ROOT" && env PREFLIGHT_RESULT_DIR="$SB_STATE/pool-results" PREFLIGHT_RESULT_TTL=1 \
  PREFLIGHT_LOCK_FILE="$pool_lock" PREFLIGHT_LOCK_WAIT=0 bash -c '
    set -uo pipefail
    export PREFLIGHT_SLOT_COUNT='"$default_count"'
    # Inherited markers (this self-test running as a nested invocation of a holding
    # run): the holder is already my ancestor, so the pool is already full for a
    # plain child and I must NOT take it again — a descendant cannot, by design.
    if [ -n "${PREFLIGHT_SLOT_HOLDER:-}" ] && [ -n "${PREFLIGHT_SLOT_FD:-}" ]; then
      echo "16d: using the INHERITED slot (holder=$PREFLIGHT_SLOT_HOLDER fd=$PREFLIGHT_SLOT_FD)"
      ./scripts/preflight.sh --only gofmt
      exit $?
    fi
    for n in $(seq 1 "$PREFLIGHT_SLOT_COUNT"); do
      fd=""
      exec {fd}>>"'"$pool_lock"'".slot$n || exit 1
      flock -o -n "$fd" || exit 1
      eval "own_fd_$n=$fd"
    done
    # Premise: a fresh take on a NEWLY opened descriptor is refused, so the subject
    # run below cannot be proceeding because a slot looked free.
    probe=""
    exec {probe}>>"'"$pool_lock"'".slot1 || exit 1
    if flock -n "$probe" 2>/dev/null; then echo PREMISE-FAILED; exit 1; fi
    exec {probe}>&-
    # What a nested preflight.sh inherits from the run that took the slot.
    export PREFLIGHT_SLOT_FD="$own_fd_1" PREFLIGHT_SLOT_N=1
    export PREFLIGHT_SLOT_HOLDER="$$"
    export PREFLIGHT_SLOT_LOCK="'"$pool_lock"'.slot1"
    ./scripts/preflight.sh --only gofmt
  ' 2>&1) )"
s16d_rc=$?
if grep -q PREMISE-FAILED <<<"$s16d_out"; then
  bad "16d's premise failed: a fresh open of a held slot was NOT refused, so this check cannot attribute a run to inheritance — the pool does not actually exclude (output: [$s16d_out])"
elif [ "$s16d_rc" -eq 0 ] && grep -qF "$s16_gofmt_label" <<<"$s16d_out" \
     && grep -q 'ALREADY INSIDE the gate pool' <<<"$s16d_out"; then
  ok "16d: a nested invocation that INHERITED a slot runs its gates instead of waiting on its own ancestor (rc=0, gate executed, announced as already inside the pool) — no self-deadlock"
else
  bad "16d: an inherited slot must run the gates, not queue (rc=$s16d_rc, output: [$(printf '%s' "$s16d_out" | grep -E 'slots:|gate \[gofmt\]|PREFLIGHT FAIL|PREMISE' | tr '\n' ' ')]"
fi

# 16e. THE BITE FOR 16b/16c: a copy whose pool size is 1 must be DETECTED as a
#      1-slot pool. A guard that reads the cap but cannot see the cap being wrong
#      is not a guard, and a 1-slot pool is exactly the shape a future "just make
#      it a lock again" edit would produce. Driven on a COPY (never the live
#      runner, which this gate runs on) under the same held-pool condition, and
#      the count is read off the copy's own printed wait line rather than assumed.
sed 's/^PREFLIGHT_SLOT_COUNT="\${PREFLIGHT_SLOT_COUNT:-2}"/PREFLIGHT_SLOT_COUNT="${PREFLIGHT_SLOT_COUNT:-1}"/' \
  "$PREFLIGHT" > "$SB/scripts/preflight_slotmut.sh" 2>/dev/null
if ! grep -qF 'PREFLIGHT_SLOT_COUNT="${PREFLIGHT_SLOT_COUNT:-1}"' "$SB/scripts/preflight_slotmut.sh"; then
  bad "16e's mutation did not land (the default slot count is no longer the literal 2 this check rewrites) — the case would have reported a false green"
else
  s16e_ready="$SB_STATE/s16e.ready"
  rm -f "$s16e_ready"
  pool_hold "$s16e_ready" &
  s16e_holder=$!
  s16e_ready_rc=0
  wait_pool_hold_ready "$s16e_ready" "$s16e_holder" || s16e_ready_rc=$?
  s16e_out="$( (cd "$LIVE_ROOT" && env -u PREFLIGHT_SLOT_COUNT \
      -u PREFLIGHT_SLOT_FD -u PREFLIGHT_SLOT_HOLDER -u PREFLIGHT_SLOT_N -u PREFLIGHT_SLOT_LOCK \
      PREFLIGHT_RESULT_DIR="$SB_STATE/pool-results" PREFLIGHT_RESULT_TTL=1 \
      PREFLIGHT_LOCK_FILE="$pool_lock" PREFLIGHT_LOCK_WAIT=0 \
      bash "$SB/scripts/preflight_slotmut.sh" --only gofmt 2>&1) )"
  s16e_rc=$?
  s16e_hold_rc=0
  wait "$s16e_holder" 2>/dev/null; s16e_hold_rc=$?
  s16e_slots="$(sed -n 's/.*all \([0-9]*\) gate slot(s) busy.*/\1/p' <<<"$s16e_out" | sed -n '1p')"
  # The SAME holder premise as 16b (card t_458c2578): with the holder's status
  # discarded, a pool_hold that failed to fill the pool made the subject run
  # against a free slot, and the "all N busy" line then came from a DIFFERENT
  # state than the check intends — so the count read off it proves nothing.
  if [ "$s16e_ready_rc" -ne 0 ] || [ "$s16e_hold_rc" -ne 0 ]; then
    bad "16e: the pool HOLDER did not fill the pool (readiness rc=$s16e_ready_rc, pool_hold rc=$s16e_hold_rc), so the count read off the subject is not a statement about a 1-slot pool's cap"
  elif [ "$s16e_slots" = "1" ]; then
    ok "16e BITES: a copy that defaults to a 1-slot pool reports \"all 1 gate slot(s) busy\" (rc=$s16e_rc), so checks 16b/16c read the cap from the script under test and a silent return to the single lock goes RED"
  else
    bad "16e does not bite: a copy defaulting to a 1-slot pool did not report a 1-slot pool (read [$s16e_slots], shipped default $default_count) — the cap is not read from the script under test, so a wrong cap would pass"
  fi
fi
rm -f "$SB/scripts/preflight_slotmut.sh"

# 16f. THE BITE FOR 16c, ON THE SAME LEG. 16c's PASS is a property — "it ran its
#      gate while slot 1 was still held" — and a property claim needs a case that
#      shows it is not free. The case is a COPY of the runner whose pool is a
#      SINGLE LOCK on slot 1 (the exact defect card t_1c59f9f5 fixed), driven
#      through the SAME pool_wake_leg as 16c on the same pool: it must NOT proceed
#      while slot 1 is held, and it must proceed once slot 1 is RELEASED — the
#      second half is what stops the leg being satisfied by a copy that is simply
#      broken. Driven on a COPY (never the live runner, which this gate runs on).
#
#      BOTH candidate scans have to be rewritten, not just the poll's: the TAKE
#      re-scans every slot on EVERY call, which is the other half of why the live
#      wait cannot be blind to a freed slot. A mutation of the poll alone leaves
#      the take's scan intact and the copy then behaves like the live runner —
#      measured while writing this. So the mutation is applied to the literal both
#      scans share, and the check asserts that exactly the two rewritten loops
#      landed, so a future third scan reds by name instead of silently weakening
#      the case.
s16f_mut="$SB/scripts/preflight_slot1only.sh"
sed 's/\$(seq 1 "\$PREFLIGHT_SLOT_COUNT")/1/g' "$PREFLIGHT" > "$s16f_mut" 2>/dev/null
if [ "$(grep -cF 'for n in 1; do' "$s16f_mut" 2>/dev/null)" != "1" ] \
   || [ "$(grep -cF 'for _slot_n in 1; do' "$s16f_mut" 2>/dev/null)" != "1" ]; then
  bad "16f's mutation did not land (the pool's candidate scans are no longer the two literal \$(seq 1 \"\$PREFLIGHT_SLOT_COUNT\") loops this check rewrites — the take's and the poll's — so the case would have reported a false green)"
else
  pool_wake_leg s16f "$s16_bound" bash "$s16f_mut" --only gofmt
  s16f_out="$(cat "$PW_OUT" 2>/dev/null)"
  case "$PW_RESULT" in
    after)
      ok "16f BITES: a copy whose pool is a SINGLE LOCK on slot1 does NOT proceed while slot1 is held and runs its gate only once slot1 is RELEASED (rc=$PW_RC, ${PW_ELAPSED}s) — 16c's PASS is not reachable by the slot-1-only wait it exists to catch"
      ;;
    held|held_unverified)
      bad "16f does not bite: a copy whose pool is a single lock on slot1 still ran its gate while slot1 was held ([$PW_RESULT]) — so 16c's PASS IS reachable by the very defect it exists to catch"
      ;;
    nohold|holder_died|holder_late)
      bad "16f PREMISE: $PW_DETAIL"
      ;;
    nofull)
      bad "16f: the mutated copy never reported a full pool within ${s16_bound}s (rc=$PW_RC) — output: [$(printf '%s' "$s16f_out" | grep -E 'slots:|gate \[gofmt\]|preflight:' | tr '\n' ' ')]"
      ;;
    never)
      bad "16f: the mutated copy never ran a gate even after slot1 was released (rc=$PW_RC) — a copy that is simply broken cannot show what a slot-1-only wait does, output: [$(printf '%s' "$s16f_out" | grep -E 'slots:|gate \[gofmt\]|preflight:' | tr '\n' ' ')]"
      ;;
    *)
      bad "16f: the pool leg returned an unexpected result [$PW_RESULT]"
      ;;
  esac
  rm -f "$s16f_mut"
fi

# 16g. A HOLDER THAT CANNOT FILL THE POOL IS NAMED BY ITS CAUSE, AND NO SUBJECT IS
#      READ OFF THE POOL IT FAILED TO FILL (card t_d0cceae1).
#
#      Two defects in one line of gate output are pinned here, and the case is
#      DETERMINISTIC — it does not depend on host scheduling:
#        (a) the verdict must say the HOLDER exited (with its status) and must not
#            report a bound that was never reached; the old message said "did not
#            fill the pool within 15s (readiness rc=1)" for an rc that means "the
#            holder process is gone", which sends an operator chasing host load
#            instead of the pool;
#        (b) no subject may be started, and none may be read, while the premise is
#            unproven — the old code started the subject BEFORE it waited for the
#            premise, so the subject took the slot the holder could not and its rc
#            was reported as though it measured the wait.
#      The failure is produced with a FOREIGN holder on the last slot — this check
#      takes it itself, from its own process, so the leg's holder's non-blocking
#      `flock` on that slot is refused and it exits 1. That is the same end state a
#      subject winning the start race produces (measured: 16c/16f red on the old
#      line when the holder's slot take was delayed 0.3s), without needing a loaded
#      host to reproduce it.
s16g_fd=""
if exec {s16g_fd}>>"$pool_lock.slot$default_count" && flock -o -n "$s16g_fd"; then
  # Assert OUR OWN premise before trusting the case: a fresh take (a new open, as a
  # descendant or a foreign process would make) must be REFUSED, or the slot is not
  # really held and the leg's holder would succeed — the case would then measure
  # nothing and report a false green.
  s16g_probe=""
  exec {s16g_probe}>>"$pool_lock.slot$default_count"
  if flock -n "$s16g_probe" 2>/dev/null; then
    exec {s16g_probe}>&-
    bad "16g's premise failed: this check's own hold on slot $default_count did not exclude a fresh take, so the leg's HOLDER would succeed and the case proves nothing"
  else
    exec {s16g_probe}>&-
    pool_wake_leg s16g "$s16_bound" ./scripts/preflight.sh --only gofmt
    s16g_subject_created=0
    [ -e "$PW_OUT" ] && s16g_subject_created=1
    # `${PW_DETAIL:-}` so a leg that does not set a detail reports the missing detail
    # instead of aborting this file on `set -u`.
    s16g_detail="${PW_DETAIL:-<no detail reported by the leg>}"
    if [ "$PW_RESULT" != holder_died ]; then
      bad "16g does not bite: with slot $default_count held by a FOREIGN process the leg returned [$PW_RESULT] (detail: [$s16g_detail]) instead of naming the holder's death as the reason nothing was measured"
    elif ! grep -q 'exited' <<<"$s16g_detail" || ! grep -q 'status 1' <<<"$s16g_detail"; then
      bad "16g: the verdict must NAME the cause and the holder's own status — expected 'exited' and 'status 1' in the detail, got [$s16g_detail]"
    elif grep -qE '(within|in) [0-9]+s' <<<"$s16g_detail"; then
      bad "16g: a HOLDER THAT EXITED must never be reported as a bound that expired (the old defect: 'within 15s' for a holder that was already gone) — got [$s16g_detail]"
    elif [ "$s16g_subject_created" -ne 0 ] || [ -n "$PW_RC" ]; then
      bad "16g: a subject was STARTED (output file present: $s16g_subject_created, subject rc=[$PW_RC]) against a pool the holder never filled — its reading would be mis-attributed to the runner; the leg must not create a subject while the premise is unproven"
    else
      ok "16g BITES: a holder that cannot take a slot is reported as 'exited (status 1)' — not as an expired bound — and NO subject is started, so nothing is read off a pool that was never full"
    fi
  fi
  exec {s16g_fd}>&-
else
  bad "16g could not take slot $default_count on $pool_lock to stand up the foreign-holder case"
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
deps_gates="$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_-]*\):.*/\1/p' | sort -u)"
if [ -z "$deps" ]; then
  bad "--gate-deps printed nothing — the path-aware table cannot be inspected, so its coverage cannot be checked"
elif [ "$deps_gates" = "$(printf '%s\n' $GATES | sort -u)" ]; then
  ok "--gate-deps covers exactly the $NGATES gates --list advertises (no gate missing from the path table)"
else
  bad "--gate-deps drift — in --list but NOT in the table: [$(comm -13 <(printf '%s\n' "$deps_gates") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')] / in the table but not in --list: [$(comm -23 <(printf '%s\n' "$deps_gates") <(printf '%s\n' $GATES | sort -u) | tr '\n' ' ')]"
fi

# 15a-i. THE FIVE GO GATES MUST DECLARE A REAL AREA, NOT FALL THROUGH TO `*`.
#      A gate with no row in `pw_declared` hits the `*)` arm, which means "cannot
#      narrow — always run": every `--for-diff` run then pays it whatever the diff
#      touches. Measured on PR #1701 (20 PNGs + one docs page, no Go file): `build`
#      + `vet` + `gofmt` + `test` + `govuln` ran and cost 632 s, because `gofmt` had
#      NO row at all — the other four had one, but a `*` would have been just as
#      invisible. This arm is the thing that cannot be forgotten: it reads the
#      PUBLIC table, so a gate that loses its row (or gains the `*` default) is RED
#      here instead of merely expensive on every future card.
go_unmapped=""
for g in build vet gofmt test govuln; do
  go_row="$(printf '%s\n' "$deps" | sed -n "s/^$g:[[:space:]]*//p" | sed -n '1p')"
  if [ -z "$go_row" ]; then
    go_unmapped="${go_unmapped}${g}=MISSING "
  elif [ "$go_row" = "*" ]; then
    go_unmapped="${go_unmapped}${g}=STAR "
  fi
done
if [ -z "$go_unmapped" ]; then
  ok "the five Go gates each declare a REAL path area in --gate-deps (none falls through to '*')"
else
  bad "a Go gate falls through to the '*' default in --gate-deps: [${go_unmapped% }] — --for-diff can then never narrow it, so a docs-only diff pays its entire run (PR #1701: 632 s)"
fi

# 15a-ii. THE MAPPING MUST BITE — and, just as important, it must NOT bite away a
#      dependency that is real. Both halves are one property, so they are one arm:
#        * a page under `web/src` must NOT drag build/vet/gofmt/govuln. Those gates
#          compile, format or scan Go; none of them opens a .tsx. Before the
#          `_test.go` rule in pw_build_table they were selected through a Go TEST's
#          walk of web/src, which is an input to `test` and to nothing else
#          (measured on PR #1701: build 30 s + vet 34 s + govuln 15 s + gofmt 2 s).
#        * the SAME page MUST select `test`, because api/internal/httpapi's
#          passkey seam resolves `../../../web/src` and api/internal/mcpsrv walks
#          web/src/** and reads every .ts/.tsx/.js it finds. Dropping `test` here
#          would be a real hole (a new page may contain a scanned claim), so this
#          half pins the dependency the card's "no Go gate at all" would remove.
page_sel="$(sel_classify 'web/src/app/panduan/surau/page.tsx')"
page_stale=""
for g in build vet gofmt govuln; do
  grep -qx "$g" <<<"$page_sel" && page_stale="${page_stale}${g} "
done
if [ -n "$page_stale" ]; then
  bad "a web/src page drags [${page_stale% }] — no Go source reads it, so those gates cannot narrow and every docs/web diff pays them"
elif ! grep -qx 'test' <<<"$page_sel"; then
  bad "web/src/app/panduan/surau/page.tsx must still select \`test\` (api/internal/httpapi's passkey seam and api/internal/mcpsrv's walk read web/src/**) — got: [$(printf '%s' "$page_sel" | tr '\n' ' ')]"
else
  ok "a web/src page selects \`test\` (a real reader) and SKIPS build/vet/gofmt/govuln (no Go gate can open a .tsx)"
fi

# The gates that CANNOT narrow: their inputs are the whole checkout / the whole
# git history, so no path list can express them and they appear in EVERY
# selection (see pw_gate_blocked). Derived ONCE here, from the same table 15a
# checks, because three checks below need it: 15b/15c subtract it to read "which
# NARROWABLE gate reads this path", and 15f asserts each of them survives a
# narrowed run.
star_gates="$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_-]*\):[[:space:]]*\*$/\1/p')"

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
#      scripts/preflight_test.sh (the `selftest` gate), a per-gate detail file
#      (gate-inventory/a11y.md) by scripts/spec-counts.sh (the `speccounts` gate,
#      which re-reads the figure it carries — card t_d6ce3d60), and
#      deploy/backup/README.md by the backup suite (the `backup` gate).
#      BITE-HARNESSES.md is the newest member and it was missed for a while: it is
#      named through a `const` in a Go test, like MCP-PARITY.md, and its name was
#      not in the runner's hand-written list of repo-root files, so a diff touching
#      it alone widened a one-gate run to all 15. It is pinned here so the next
#      regression is RED instead of merely expensive.
trap_specs="MCP-PARITY.md:test GATE-INVENTORY.md:selftest gate-inventory/a11y.md:speccounts AGENTS.md:selftest deploy/backup/README.md:backup BITE-HARNESSES.md:test"
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

# 15e-iii. THE SAME TRAP FOR A NON-PROSE FILE OUTSIDE THE GATE'S OWN AREA (card
#      t_7184eede). `deploy/build-and-ship.sh` is the only place a release sets
#      the provenance stamps it passes to `docker build`, and
#      api/internal/version/dockerfile_wiring_test.go reads it to fail when a
#      stamp api/Dockerfile needs is not passed as --build-arg. A change to that
#      script can therefore red the api suite, so the classifier must select
#      `test` for it.
#
#      IT DID NOT, AND THAT IS THE BUG THIS CHECK PINS. The script was claimed by
#      the deploy/scripts gates only (their area is `deploy/*`), so a PR that
#      rewrote its --build-arg wiring ran every deploy gate and skipped the ONE
#      test that reads it. The derived read was absent because the Go test named
#      the script in its prose and its failure messages but never READ it — a
#      dependency nothing could derive. Both halves are needed for that reason:
#      the test must read the file, and the classifier must then select `test`
#      for a diff to it. Fixing only the first leaves the second to luck.
#
#      NARROWED-ness is asserted too, for the reason 15e-i gives: `test` is IN the
#      full set, so a widened run satisfies a bare `grep -qx test` while the claim
#      this check exists to prove is gone.
ship_sel="$(sel_classify 'deploy/build-and-ship.sh')"
ship_n="$(grep -cv '^#reason:' <<<"$ship_sel")"
if ! grep -qx 'test' <<<"$ship_sel"; then
  bad "deploy/build-and-ship.sh must select \`test\`: api/internal/version/dockerfile_wiring_test.go reads that script (it fails when a stamp api/Dockerfile requires is not passed as --build-arg), so a diff to it can red the api suite — got: [$(printf '%s' "$ship_sel" | tr '\n' ' ')]"
elif [ "$ship_n" = "$NGATES" ] || grep -q 'no gate claims' <<<"$ship_sel"; then
  bad "deploy/build-and-ship.sh must NARROW to the gates that read it, not drag in the full $NGATES set (a widened run would name \`test\` for the wrong reason): [$(printf '%s' "$ship_sel" | tr '\n' ' ')]"
else
  ok "deploy/build-and-ship.sh selects test (the api suite reads it through dockerfile_wiring_test.go) and still narrows to $ship_n gates, not the full $NGATES"
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
elif [ "$(printf '%s\n' "$star_gates" | wc -l)" != "$(printf '%s\n' "$deps" | sed -n 's/^\([A-Za-z0-9_-]*\):[[:space:]]*\*$/\1/p' | sort -u | wc -l)" ]; then
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
      | sed -n 's/^\([A-Za-z0-9_-]*\):[[:space:]]*\*$/\1/p' | sort | tr '\n' ' ')"$'\n'
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
    build|vet|gofmt|test|govuln) printf '%s\n' 'api/*' ;;
    web)      printf '%s\n' 'web/package.json' 'web/src/*' 'web/next.config.mjs' 'web/tsconfig.json' 'web/scripts/web-deps-check.mjs' ;;
    lint)     printf '%s\n' 'web/eslint.config.mjs' 'web/package.json' ;;
    a11y)     printf '%s\n' 'web/e2e/a11y*.spec.ts' 'web/playwright.config.ts' ;;
    e2e-builder) printf '%s\n' 'web/e2e/builder-*.spec.ts' 'web/e2e/areas.tsv' 'web/scripts/e2e-scope.mjs' 'web/playwright.config.ts' ;;
    e2e-core) printf '%s\n' 'web/e2e/*.spec.ts' 'web/e2e/areas.tsv' 'web/scripts/e2e-scope.mjs' 'web/playwright.config.ts' ;;
    menufixture) printf '%s\n' 'scripts/menu-fixture-e2e.sh' 'api/cmd/menu-fixture/*' 'api/internal/menufixture/*' 'web/e2e/menu-checkout-option-group.spec.ts' 'web/playwright.menufixture.config.ts' ;;
    speccounts) printf '%s\n' 'scripts/spec-counts.sh' ;;
    npmaudit) printf '%s\n' 'web/package.json' 'web/package-lock.json' 'web/npm-audit-exceptions.json' 'web/scripts/npm-audit-gate.mjs' ;;
    monitor)  printf '%s\n' 'deploy/monitoring/*' ;;
    tmpkey)   printf '%s\n' 'deploy/tmp-apikey.sh' 'deploy/tmp-apikey_test.sh' ;;
    mediaprobe) printf '%s\n' 'scripts/media-purge-probe.sh' 'scripts/media-purge-probe_test.sh' ;;
    design)   printf '%s\n' 'scripts/design-guard.py' 'scripts/design-guard_test.sh' ;;
    backup)   printf '%s\n' 'deploy/backup/*' ;;
    host)     printf '%s\n' 'deploy/host/*' 'deploy/vps/*' ;;
    prunewt)  printf '%s\n' 'deploy/worktrees/*' ;;
    prunesync) printf '%s\n' 'deploy/worktrees/check-prune-kanban-worktrees-host*.sh' 'deploy/worktrees/install-prune-kanban-worktrees.sh' ;;
    prunehost) printf '%s\n' 'deploy/host/*' ;;
    refguard) printf '%s\n' 'scripts/refguard.sh' 'scripts/refguard_test.sh' ;;
    selfhost) printf '%s\n' 'deploy/selfhost/*' 'deploy/docker-compose.yml' 'deploy/.env.example' 'docs/SELF-HOST.md' 'README.md' ;;
    selftest) printf '%s\n' 'scripts/preflight.sh' 'scripts/preflight_test.sh' ;;
    gitleaks) printf '%s\n' '.gitleaks.toml' 'scripts/gitleaks_scan.sh' ;;
  esac
}
# GATES THAT LEGITIMATELY HAVE NO SOURCE ARM, declared rather than left silent (card
# t_31482a39). An empty `pw_src_of` glob list means the probe below scans NOTHING for
# that gate and passes, so a gate that is merely MISSING an arm is indistinguishable
# from one that genuinely cannot narrow — and the runner treats the two differently:
# an arm-less gate is cannot-narrow there, so it runs unconditionally. Declaring it
# here is what makes "no arm" a decision instead of an omission.
#
#   webimage — the shipping image is a function of the whole tree, so it cannot narrow.
#
# `gofmt` USED to be here, on the claim that `gofmt -l .` "reads EVERY tracked Go
# file; it cannot narrow". It reads every Go file under api/ and nothing else — the
# repo has no root go.mod — so it is arm-carrying now, with the same `api/*` arm the
# other Go gates use. It had no row in the runner's `pw_declared` either, which is
# what actually made it un-narrowable: a gate with no row falls through to `*` and
# runs on every scoped run whatever the diff (PR #1701 paid it for a docs page).
#
# Adding a gate to GATE_NAMES without an arm and without a line here now FAILS the
# check: the mirror and the probe are kept in step by assertion, not by whoever
# remembers. That is the defect this closes — THE MIRROR HAD ALREADY DRIFTED `e2e`
# carried a runner arm but no mirror arm, so the probe was BLIND to it, and `a11y` was
# mirrored as all of `web/e2e/*` where the runner reads only `a11y*.spec.ts`. The
# mirror is deliberately INDEPENDENT of the runner (that independence is why a runner
# derivation that goes blind cannot satisfy this check), so it must be COMPLETE here
# rather than derived.
PW_SRC_NONE="webimage"
# The independent probe's pattern. It is deliberately a DIFFERENT literal from the
# runner's (`REPOWIDE_RE` there), and it runs as its own code path: the verdict
# comes from an independent reading, so a derivation that goes BLIND cannot satisfy
# it by finding no offenders at all.
REPOWIDE_ANY_RE='git[[:space:]]+([^|]*[[:space:]])?(grep|ls-files|rev-list)[[:space:]]'
# THE PROBE'S OWN LINE REDUCTION, and it is the half that must agree with the
# runner on the RULE while staying independent in the CODE. Before card
# t_bd79c727 the probe grepped the raw line, exactly as the runner did, so both
# were wrong the same way — and a mirror that shares the defect CANNOT catch it
# (measured: this check stayed GREEN on the degraded tree, which is why the
# regression was invisible to 15k and only 15b/15e-i went red). The rule applied
# here is the runner's, stated in scripts/preflight.sh at pw_code_lines: a
# repo-wide read is a STATEMENT, so (a) a comment is not a read and (b) in a
# language where a quoted span is DATA (Go/TS/JS) a message that QUOTES the
# command is not a read either — and the DATA set is the interpreted pair
# `"…"`/`'…'` AND the backtick raw string `` `…` `` (card t_7e703b11: dropping
# only the interpreted pair made this probe blind to a backtick MESSAGE, exactly
# as the shared raw-line defect had left the whole check blind before PR 961).
# A shell source keeps its quoted spans, where
# `"$(git …)"` holds code whose quote state is re-opened inside the
# substitution. If this reduction ever drifts toward stripping MORE than the
# runner strips, the verdict is the SAFE direction — a gate demanded into '*',
# never one dropped.
probe_code_lines() {
  case "$1" in
    *.go|*.ts|*.tsx|*.js|*.mjs|*.cjs)
      sed -e '/^[[:space:]]*\(#\|\/\/\|\/\*\|\*\)/d' -e 's/"[^"]*"//g' -e "s/'[^']*'//g" -e 's/`[^`]*`//g' "$1" 2>/dev/null ;;
    *)
      sed -e '/^[[:space:]]*\(#\|\/\/\|\/\*\|\*\)/d' "$1" 2>/dev/null ;;
  esac
}
# FIRST: every gate must be either arm-carrying or explicitly declared as having none.
# A gate in NEITHER is the silent-coverage hole this addition closes.
pw_unarmed=""
for g in $GATES; do
  if [ -z "$(pw_src_of "$g")" ] && ! grep -qw "$g" <<<"$PW_SRC_NONE"; then
    pw_unarmed="${pw_unarmed}${g} "
  fi
done
if [ -n "$pw_unarmed" ]; then
  bad "these gates have NO arm in pw_src_of and are not declared in PW_SRC_NONE: [$(printf '%s' "$pw_unarmed")] — the probe below would scan NOTHING for them and pass, so a gate missing an arm is indistinguishable from one that cannot narrow. Give the gate an arm, or add it to PW_SRC_NONE with the reason it cannot narrow."
fi
# And the declaration must not rot: a gate named in PW_SRC_NONE that DOES carry an arm
# is a stale exemption, which would hide a real arm the probe should be reading.
for g in $PW_SRC_NONE; do
  if [ -n "$(pw_src_of "$g")" ]; then
    bad "PW_SRC_NONE lists \`$g\`, but it now carries a source arm — the exemption is stale and must be removed, or the probe reads the wrong set for it"
  fi
done
# FIRST: every gate must be either arm-carrying or explicitly declared as having none.
# A gate in NEITHER is the silent-coverage hole this addition closes.
pw_unarmed=""
for g in $GATES; do
  if [ -z "$(pw_src_of "$g")" ] && ! grep -qw "$g" <<<"$PW_SRC_NONE"; then
    pw_unarmed="${pw_unarmed}${g} "
  fi
done
if [ -n "$pw_unarmed" ]; then
  bad "these gates have NO arm in pw_src_of and are not declared in PW_SRC_NONE: [$(printf '%s' "$pw_unarmed")] — the probe below would scan NOTHING for them and pass, so a gate missing an arm is indistinguishable from one that cannot narrow. Give the gate an arm, or add it to PW_SRC_NONE with the reason it cannot narrow."
else
  ok "every gate is either arm-carrying in pw_src_of or declared in PW_SRC_NONE (which is what makes an arm-less gate a decision, not a silent hole): arm-less = [$(printf '%s' "$PW_SRC_NONE")], and the mirror covers all $NGATES gates"
fi
# The declaration must not rot either: a gate named in PW_SRC_NONE that DOES carry an
# arm is a stale exemption, which would hide a real arm the probe should be reading.
stale_exempt=""
for g in $PW_SRC_NONE; do
  if [ -n "$(pw_src_of "$g")" ]; then
    stale_exempt="${stale_exempt}${g} "
    bad "PW_SRC_NONE lists \`$g\`, but it now carries a source arm — the exemption is stale and must be removed, or the probe reads the wrong set for it"
  fi
done
[ -n "$stale_exempt" ] || ok "no stale exemption: every gate named in PW_SRC_NONE still genuinely has no arm in pw_src_of"
rw_any=""
for g in $GATES; do
  while IFS= read -r glob; do
    [ -n "$glob" ] || continue
    for f in $(cd "$LIVE_ROOT" && git ls-files -- "$glob" 2>/dev/null); do
      [ -f "$LIVE_ROOT/$f" ] || continue
      # A BINARY file cannot carry a shell/Go read STATEMENT, and `git grep` — which
      # is how the runner FINDS these reads — skips binary content by default. The
      # probe must skip it too: capturing the bytes into a command substitution makes
      # bash print "ignored null byte in input" for every file, and reads noise as
      # code. Surfaced when `gofmt` gained an `api/*` arm (it used to be exempt), so
      # this scan now reaches api/internal/httpapi/favicon/icon-192.png.
      grep -qI . "$LIVE_ROOT/$f" 2>/dev/null || continue
      if grep -qE "$REPOWIDE_ANY_RE" <<<"$(probe_code_lines "$LIVE_ROOT/$f")"; then
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

# 15p. THE BACKTICK RAW STRING IS DATA, AND BOTH READERS MUST SAY SO (card t_315a5f91).
#
#      Both reductions drop a Go/TS/JS BACKTICK span (`` `…` ``) as well as the
#      interpreted `"…"`/`'…'`, because in those languages a raw string is DATA like
#      any other quoted span: a MESSAGE that quotes a repo-wide command is not a read
#      of the checkout. That arm carries the fix of t_7e703b11, and until this check
#      it was UNGUARDED — measured at tag v1.20.2 (tree 04d2639d): neutering the arm
#      in the runner kept `bash scripts/preflight_test.sh` at 79 passed / 0 failed,
#      byte-identical modulo the tree sha check 38 prints. The same held when only the
#      PROBE's arm was neutered. 15k runs `probe_code_lines` only over the LIVE tree's
#      real sources, so it exercises the arm's OUTCOME but can never flip on its
#      removal; nothing planted a fixture whose ONLY repo-wide mention was a backtick
#      MESSAGE. This does, for BOTH readers, and it is the guard the t_7e703b11 arm
#      was missing.
#
#      WHY THE NEUTRALISATION IS `awk index()` AND NOT `sed`. The clause is `s/`[^`]*`//g`
#      — itself a sed program, so a sed that edits it has to escape `[`, `]` and `*`
#      correctly (two rounds of quoting) and silently no-ops when it does not. `awk`'s
#      `index()` takes the literal and replaces it by hand, so the edit cannot miss.
#      Measured while writing this check: a `sed` over the `[` bytes left the clause
#      intact and the copy still stripped the fixture, which would have read as "the
#      check does not bite".
pwb_fixture="$SB/api/internal/qaprobe/backtick_msg.go"
# The literal both readers drop. It is repeated here on purpose: the check is about
# the READERS' behaviour, so the edit is driven by the pattern the readers carry
# rather than by importing their sed line and mutating the copy that produced it.
PWB_CLAUSE='s/`[^`]*`//g'
PWB_RE='git[[:space:]]+([^|]*[[:space:]])?(grep|ls-files|rev-list)[[:space:]]'
# The clause in the runner's `pw_code_lines` and in this file's `probe_code_lines`,
# value-replaced by a literal no fixture can contain. A whole-file `sed` would also
# reach the backtick spans in the PROSE, so the replacement is confined to the
# function body — that is where the clause the reduction executes lives.
pwb_swap_clause() {
  awk -v from="$PWB_CLAUSE" -v to='s/PWB_QA_NOPE//g' '
    { line=$0; out=""
      while ((p=index(line, from)) > 0) { out=out substr(line,1,p-1) to; line=substr(line,p+length(from)) }
      print out line }'
}
# THE BITE, ON THIS FILE'S OWN PROBE. Extract the shipped `probe_code_lines` verbatim,
# rename the copy so both can be sourced in one shell (`probe_code_lines` is a live
# function here — redefining it would break 15k, which runs ABOVE this line and would
# not care, and every use below, which would), and read the fixture with each. The
# rename is scoped to the function's own line.
pwb_probe_shipped="$SB_STATE/pwb_probe_shipped.sh"
pwb_probe_neutral="$SB_STATE/pwb_probe_neutral.sh"
sed -n '/^probe_code_lines() {/,/^}/p' "$SELF" > "$pwb_probe_shipped"
pwb_swap_clause < "$pwb_probe_shipped" \
  | sed 's/probe_code_lines/probe_code_lines_pwb_neutral/' > "$pwb_probe_neutral"
pwb_shipped_hits=0; pwb_neutral_hits=0
if [ -f "$pwb_fixture" ] && [ -s "$pwb_probe_shipped" ] && [ -s "$pwb_probe_neutral" ]; then
  pwb_shipped_hits="$(. "$pwb_probe_shipped" && probe_code_lines "$pwb_fixture" | grep -cE "$PWB_RE")"
  pwb_neutral_hits="$(. "$pwb_probe_neutral" && probe_code_lines_pwb_neutral "$pwb_fixture" | grep -cE "$PWB_RE")"
fi
if [ ! -f "$pwb_fixture" ]; then
  bad "15p cannot read its fixture [$pwb_fixture] — the backtick-arm check has nothing to bite, so a removal would pass green (the exact gap card t_315a5f91 filed)"
elif [ ! -s "$pwb_probe_shipped" ] || [ ! -s "$pwb_probe_neutral" ]; then
  bad "15p could not extract \`probe_code_lines\` from $SELF — the neutered copy is empty, so comparing the two readers proves nothing"
elif [ "$pwb_shipped_hits" != 0 ]; then
  bad "15p fixture is not the shape under test: this file's shipped \`probe_code_lines\` counted $pwb_shipped_hits repo-wide match(es) in a file whose ONLY mention is a backtick MESSAGE — expected 0. The fixture, not the reader, is wrong"
elif [ "$pwb_neutral_hits" = 0 ]; then
  bad "15p does NOT bite: with the backtick clause removed, \`probe_code_lines\` STILL reads no repo-wide match ($pwb_neutral_hits) in a file whose raw line matches once — so the neutered copy is not actually neutered and this check can never catch the arm's removal"
else
  ok "15p BITES: this file's own \`probe_code_lines\` strips the backtick MESSAGE (0 repo-wide matches), while a copy with the backtick clause value-replaced reports $pwb_neutral_hits — the arm the t_7e703b11 fix added is guarded, not merely exercised"
fi
# AND THE RUNNER'S ARM, ON THE SAME FIXTURE, THROUGH ITS OWN DERIVATION. The probe
# leg above reads this file's mirror; the runner has a SEPARATE copy of the clause
# (scripts/preflight.sh's `pw_code_lines`), and the card's repro neutered exactly
# that one. It is asserted through the runner's PUBLIC surface — `--gate-deps`, whose
# `test: api/*` row is only printed when `api/*` is NARROWABLE — against a copy whose
# clause is value-replaced the same way. Both directions: the shipped copy must still
# report `test:` with a real area, and the neutered copy must widen it to `*`.
pwb_runner_shipped="$SB/scripts/preflight_pwb_shipped.sh"
pwb_runner_neutral="$SB/scripts/preflight_pwb_neutral.sh"
cp "$PREFLIGHT" "$pwb_runner_shipped"; chmod +x "$pwb_runner_shipped"
awk -v from="$PWB_CLAUSE" -v to='s/PWB_QA_NOPE//g' '
  { line=$0; out=""
    while ((p=index(line, from)) > 0) { out=out substr(line,1,p-1) to; line=substr(line,p+length(from)) }
    print out line }' "$PREFLIGHT" > "$pwb_runner_neutral"
chmod +x "$pwb_runner_neutral"
pwb_live_row="$( (cd "$SB" && PREFLIGHT_RESULT_DIR="$SB_STATE/pwb-live" ./scripts/preflight_pwb_shipped.sh --gate-deps 2>/dev/null) | sed -n 's/^test: //p' | sed -n '1p')"
pwb_neutral_row="$( (cd "$SB" && PREFLIGHT_RESULT_DIR="$SB_STATE/pwb-neutral" ./scripts/preflight_pwb_neutral.sh --gate-deps 2>/dev/null) | sed -n 's/^test: //p' | sed -n '1p')"
if grep -qF "$PWB_CLAUSE" "$pwb_runner_neutral"; then
  bad "15p cannot neuter the runner's backtick clause: the literal [$PWB_CLAUSE] is still present in the copy, so the two readings below (shipped vs neutered) are the same file and the comparison is vacuous"
elif [ -z "$pwb_live_row" ]; then
  bad "15p could not read a \`test:\` row from the shipped runner copy's --gate-deps — the sandbox runner did not produce the table this check reads"
elif [ "$pwb_live_row" = '*' ]; then
  # The shipped runner already calls `api/*` unable to narrow on a tree whose only
  # repo-wide match is the backtick MESSAGE. Two causes look identical from the row, so
  # discriminate them on the runner's own text: the arm is PRESENT (the sandbox carries
  # another whole-checkout reader and this check is measuring that instead) or it was
  # REMOVED (the arm's absence, which is the regression this check exists to catch).
  if grep -qF "$PWB_CLAUSE" "$PREFLIGHT"; then
    bad "15p's sandbox does not exercise the arm: the SHIPPED runner (clause present) already reports \`test: *\` on a tree whose only repo-wide match is the backtick fixture, so a neutered copy could not show a difference. Some OTHER api/ source in the sandbox reads the whole checkout (a staged/untracked file, or a tree fixture) and this check is measuring that instead of pw_code_lines"
  else
    bad "15p bites: the runner reports \`test: *\` on the backtick-MESSAGE fixture and $PREFLIGHT no longer carries the backtick clause [$PWB_CLAUSE] at all — pw_code_lines' backtick arm is REMOVED, so a MESSAGE is counted as a read, \`api/*\` stops being narrowable and removing the arm the t_7e703b11 fix added is RED (this is the gap card t_315a5f91 filed)"
  fi
elif [ "$pwb_neutral_row" != '*' ]; then
  bad "15p does NOT bite the RUNNER: with scripts/preflight.sh's backtick clause value-replaced, \`--gate-deps\` still reports \`test: $pwb_neutral_row\` (a real area) instead of \`*\` — so removing pw_code_lines' backtick arm leaves the always-run verdict unchanged and the selftest green, which is the gap card t_315a5f91 filed"
else
  ok "15p BITES the RUNNER: the shipped pw_code_lines keeps \`test: $pwb_live_row\` narrowable on a backtick-MESSAGE fixture, while the value-replaced copy widens it to \`test: *\` — the always-run verdict moves, so the arm's removal is RED"
fi

# 15q. --test-readers: WHICH PACKAGES read a DATA-ONLY diff (card t_bf9ce4e2).
#
#      A diff with no api/ path can still be an input of the `test` gate: api
#      *_test.go files walk web/src and docs, and api/internal/llm's model-id guard
#      walks the WHOLE tree. The merge train hands preflight the packages that own
#      those readers instead of paying `go test ./...` (measured 551-710 s for a
#      20-screenshot guide). The list must be DERIVED from the same read table the
#      selection uses — a hand-written list would rot the day a guard is added.
#
#      ASSERTED ON A PURPOSE-BUILT REPO, not the live tree: the fixture needs a
#      KNOWN reader set, and the live tree's changes with every PR. The repo is its
#      own git root under $SB_STATE, carries a copy of the runner, and has:
#        api/internal/reader     a *_test.go that resolves ../../../web/src and walks it
#        api/internal/treewide   a *_test.go that resolves the REPO ROOT and walks it
#        api/internal/metrics    NO test file at all — it cannot read anything
#        web/src/page.tsx        the changed DATA file
#      A package with no test file cannot be a reader, and both real readers must
#      be there — that is the derivation, not a list of names.
#      THE BITE: with the repo-root arm of the derivation value-replaced, the
#      tree-wide reader DISAPPEARS from the answer while the other reader survives.
#      That is the unsafe direction this check exists to catch — remove a reader and
#      its guard stops running with nothing to show for it.
RWQ="$SB_STATE/readers-repo"
rm -rf "$RWQ"
mkdir -p "$RWQ/scripts" "$RWQ/api/internal/reader" "$RWQ/api/internal/treewide" \
         "$RWQ/api/internal/metrics" "$RWQ/web/src"
cp "$PREFLIGHT" "$RWQ/scripts/preflight.sh"; chmod +x "$RWQ/scripts/preflight.sh"
printf 'module example.test/api\n\ngo 1.21\n' > "$RWQ/api/go.mod"
printf '<p>fixture</p>\n' > "$RWQ/web/src/page.tsx"
printf 'package metrics\n\n// no test file: this package cannot be a reader of anything\n' \
  > "$RWQ/api/internal/metrics/metrics.go"
cat > "$RWQ/api/internal/reader/reader_test.go" <<'RWREADER'
package reader

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The shape every api test in this repo uses to reach the web tree.
func TestReadsWebSrc(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "web", "src")
	if _, err := os.ReadDir(dir); err != nil {
		t.Fatal(err)
	}
}
RWREADER
cat > "$RWQ/api/internal/treewide/walk_test.go" <<'RWTREE'
package treewide

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Resolves the REPO ROOT — the shape the model-id guard in api/internal/llm uses —
// so every path in the tree is an input of this package's test.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestWalksTheWholeTree(t *testing.T) {
	if _, err := os.Stat(repoRoot()); err != nil {
		t.Fatal(err)
	}
}
RWTREE
git_init=("git" "-C" "$RWQ" "-c" "user.email=t@t" "-c" "user.name=t")
"${git_init[@]}" init -q .
"${git_init[@]}" add -A
"${git_init[@]}" commit -q -m init
RWQ_BASE="$("${git_init[@]}" rev-parse HEAD)"
# CHANGE THE DATA, not the fixture: an uncommitted edit is exactly what a batch's
# diff looks like to `--test-readers`.
printf '<p>changed</p>\n' > "$RWQ/web/src/page.tsx"
rwq_out="$( (cd "$RWQ" && ./scripts/preflight.sh --test-readers "$RWQ_BASE") 2>"$SB_STATE/rwq.err" )"
rwq_rc=$?
rwq_reason="$(sed -n '1p' "$SB_STATE/rwq.err")"
if [ "$rwq_rc" != 0 ]; then
  bad "15q --test-readers exited $rwq_rc on a fixture whose readers are known: [$rwq_reason] — a docs-only diff would fall back to 'go test ./...' with no path to the narrowing"
elif ! grep -qx './internal/reader' <<<"$rwq_out"; then
  bad "15q the reader that walks web/src is NOT in the derived set — a real reader dropped is a red gate reported green. Got: [$(printf '%s' "$rwq_out" | tr '\n' ' ')]"
elif ! grep -qx './internal/treewide' <<<"$rwq_out"; then
  bad "15q the package that resolves the REPO ROOT is NOT in the derived set — it reads every file in the tree, so a docs change can break it. Got: [$(printf '%s' "$rwq_out" | tr '\n' ' ')]"
elif grep -qx './internal/metrics' <<<"$rwq_out"; then
  bad "15q a package with NO test file was reported as a reader of the changed data — the derivation is matching something other than the reads: [$(printf '%s' "$rwq_out" | tr '\n' ' ')]"
elif ! grep -q '^select: 2 of 3 package(s)' <<<"$rwq_reason"; then
  bad "15q the scope summary does not state the true counts (want 'select: 2 of 3 package(s)'), so the verdict's 'the other N were never run' would be a guess: [$rwq_reason]"
else
  ok "15q --test-readers derives the reader PACKAGES of a data-only diff from the reads ($(printf '%s' "$rwq_out" | tr '\n' ' ' | sed 's/ $//')) and names the true 2-of-3 scope; a package with no test file is not one"
fi
# The BITE: remove the repo-root arm and the tree-walking reader must vanish.
# `awk index()` for the same reason 15p uses it: a sed over the clause would need
# two rounds of escaping and would silently no-op.
RWQ_NEUTRAL="$SB_STATE/readers-preflight-neutral.sh"
awk -v from='[ -z "$pw_root" ] && PW_TREERW[$file]=1' '
  { line=$0; out=""
    while ((p=index(line, from)) > 0) {
      out=out substr(line,1,p-1) ": # 15q mutation: repo-root arm removed"; line=substr(line,p+length(from)) }
    print out line }' "$PREFLIGHT" > "$RWQ_NEUTRAL"
chmod +x "$RWQ_NEUTRAL"
rwq_shipped_has_rootarm=0
grep -qF '[ -z "$pw_root" ] && PW_TREERW[$file]=1' "$PREFLIGHT" && rwq_shipped_has_rootarm=1
if [ "$rwq_shipped_has_rootarm" != 1 ]; then
  bad "15q cannot mutate the repo-root arm: the shipped runner no longer carries it, so this check has nothing to bite and the tree-walking reader could be dropped silently"
else
  cp "$RWQ_NEUTRAL" "$RWQ/scripts/preflight.sh"; chmod +x "$RWQ/scripts/preflight.sh"
  rwq_neutral_out="$( (cd "$RWQ" && ./scripts/preflight.sh --test-readers "$RWQ_BASE") 2>/dev/null )"
  cp "$PREFLIGHT" "$RWQ/scripts/preflight.sh"; chmod +x "$RWQ/scripts/preflight.sh"
  if grep -qx './internal/treewide' <<<"$rwq_neutral_out"; then
    bad "15q does NOT bite: with the repo-root arm removed the tree-walking reader is STILL listed, so the arm is not what supplies it and its removal would go unnoticed"
  elif ! grep -qx './internal/reader' <<<"$rwq_neutral_out"; then
    bad "15q the mutation removed MORE than the root arm: the web/src reader is gone too, so the two halves of the derivation are not independent and this comparison proves nothing"
  else
    ok "15q BITES: with the repo-root arm value-replaced the tree-walking reader disappears from the list and the web/src reader stays — dropping a reader from the derivation is RED, not a silent loss of coverage"
  fi
fi
# An api/ path is NOT this question: a package that never reads the data can still
# fail to COMPILE against it, and only the import graph knows which. Refuse.
"${git_init[@]}" checkout -q -- .
printf 'package metrics\n\n// changed\n' > "$RWQ/api/internal/metrics/metrics.go"
rwq_api_rc=0
rwq_api_out="$( (cd "$RWQ" && ./scripts/preflight.sh --test-readers "$RWQ_BASE") 2>&1 )" || rwq_api_rc=$?
if [ "$rwq_api_rc" = 1 ] && grep -q 'touches api/' <<<"$rwq_api_out"; then
  ok "15q an api/ path is REFUSED (changed+importers is a different question): the caller keeps 'go test ./...'"
else
  bad "15q an api/ change must be refused by --test-readers (rc=1, naming api/), got rc=$rwq_api_rc: [$(sed -n '1p' <<<"$rwq_api_out")]"
fi
# A FULL selection must NOT ride a package list: the run says FULL everywhere else, so
# narrowing `test` inside it would make one gate narrower than the record claims.
"${git_init[@]}" checkout -q -- .
printf 'unclaimed\n' > "$RWQ/zz_unclaimed.bin"
rwq_full_rc=0
rwq_full_out="$( (cd "$RWQ" && ./scripts/preflight.sh --test-readers "$RWQ_BASE") 2>&1 )" || rwq_full_rc=$?
if [ "$rwq_full_rc" = 1 ] && grep -q 'FULL inventory' <<<"$rwq_full_out"; then
  ok "15q a diff whose selection is the FULL inventory (fail-closed) is REFUSED a package list — one gate cannot be narrower than the run"
else
  bad "15q a FULL (fail-closed) selection must not be handed a package list (rc=$rwq_full_rc): [$(sed -n '1p' <<<"$rwq_full_out")]"
fi

# THE LIST RIDES A NARROWED RUN ONLY. This is the same guard the merge train's
# selftest asserts from the caller's side; it must also hold in the runner itself,
# because an exported variable in a lane's shell is exactly how a RELEASE would get
# a narrowed suite while its record says './...'.
rwq_default_rc=0
rwq_default_out="$( (cd "$RWQ" && PREFLIGHT_RESULT_DIR="$SB_STATE/rwq-records" \
  PREFLIGHT_TEST_PACKAGES=./internal/reader ./scripts/preflight.sh) 2>&1 )" || rwq_default_rc=$?
if [ "$rwq_default_rc" = 1 ] && grep -q 'no narrowing flag' <<<"$rwq_default_out" \
   && ! grep -q '^gate \[' <<<"$rwq_default_out"; then
  ok "15q a package list on a DEFAULT run is refused before any gate runs (a narrowed suite cannot be reported as a full inventory)"
else
  bad "15q a package list on a no-flag run must be refused before any gate (rc=$rwq_default_rc): [$(sed -n '1,2p' <<<"$rwq_default_out")]"
fi

# 15g. The default must stay a FULL run. With no selection flag the script must
#      print no narrowing notice and report no skipped gate, and its banner must
#      list the whole inventory. Asserted on the LIVE script, far enough into the
#      run to print its banner and SKIPPED list, and stopped before any gate body
#      by an unset prerequisite.
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
# THE HEADER'S OWN CLAIM, CHECKED (card t_5058fb49). The block above says "NO
# `printf … | grep -q` ANYWHERE IN THIS FILE" — a claim in a file whose whole subject
# is not printing a claim the evidence does not support. Pinned here so the sentence
# cannot become false while every check still passes.
#
# It was FALSE when it was written: three live sites stood in this very file
# (a `-timeout 0` probe, a gap-line leak check, a reuse probe), each of the class the
# header forbids. They are fixed; this keeps them fixed.
#
# Only THIS file is asserted. The header also used to say this file "was the only one
# still using the idiom", which was untrue of the repo; what belongs here is what the
# file can prove about itself.
#
# The scan skips comment lines and any line mentioning "header" (this block and its
# two messages necessarily quote the pattern), so it counts live shell sites only.
# `-n?1` (card t_0633ea1d): the original `-n[[:space:]]*1` matched `head -n1` and
# `head -n 1` but NOT `head -1`, which is the same early-exit reader and the
# spelling this file's own sweep recipe is documented against.
self_sites="$(grep -nE 'printf .*\| *grep +-[a-zA-Z]*q|\|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1' "$SELF" 2>/dev/null \
  | grep -vE '^[0-9]+:[[:space:]]*#' \
  | grep -v 'grep -nE' \
  | grep -v '…' || true)"
if [ -z "$self_sites" ]; then
  ok "the header's own claim holds: this file contains NO live \`printf … | grep -q\` site (nor \`| head -n1\`), so the sentence is true rather than remembered"
else
  bad "the header says this file has no \`printf … | grep -q\` site, but a live one is back — the claim and the file disagree:\n$self_sites"
fi
# The HEADER'S OWN SWEEP RECIPE, checked (card t_0633ea1d). The header documents a
# repo-wide recipe to find this class, and the recipe that closed the two earlier
# sweep cards omitted the `| head -n1` alternative its own paragraph names — which
# is how two live sites in deploy/backup/jomform-backup.sh survived that sweep. A
# recipe nothing reads is the rot this repo has been bitten by repeatedly, so it is
# pinned here: drop the alternative from the recipe COMMAND and the run says so.
#
# The pin reads the RECIPE LINE, not the header block. An earlier draft of this
# check grepped the whole header, which the two prose paragraphs that NAME the
# alternative also satisfy — so it stayed green with the command deleted, i.e. it
# pinned the mention and not the recipe. Same trap in miniature as the file's own
# "a pin that pins itself is not a pin".
recipe_line="$(sed -n '/^#   git grep -nE /p' "$SELF" | sed -n '1p')"
selfscan_line="$(sed -n '/^self_sites=/p' "$SELF" | sed -n '1p')"
# The needle is the alternative as it must appear in a live ERE. Assembled from two
# pieces for the same reason the file assembles other literals: a single literal
# here would also match this check's own source text.
#
# It carries `[[:space:]]*` between `-n?` and `1` (card t_809575fc). The earlier
# `-n?1` matched `head -n1` and `head -1` but NOT the spaced `head -n 1`, while
# this file's own self-scan (line ~1878) DID — so a reader running the documented
# recipe would miss a spelling the file's own scan still saw, the exact drift
# (recipe read as complete while a spelling it names goes unfound) the sibling card
# t_0633ea1d closed for the `-n1` spelling.
recipe_needle='\|[[:space:]]*head'"[[:space:]]+-n?[[:space:]]*1"
# The recipe ERE itself, lifted from between the quotes on the command line. The
# check EXECUTES it (below) rather than only grepping for the needle, so what is
# pinned is the SPELLINGS the recipe finds, not the shape of its text: a
# needle-only check stays green on a recipe that names the alternative but has
# dropped a token, which is precisely the class this file exists to catch.
recipe_ere="$(printf '%s\n' "$recipe_line" | sed -n "s/^#   git grep -nE '\(.*\)' \\\\\$/\1/p")"
# The one-line spellings the recipe MUST cover, held WITHOUT the pipe so no single
# line of this check matches the very self-scan it pins (`writer | ` is joined at
# run time, and a here-string writer has no process for the recipe to flag).
head_spellings=('head -n1' 'head -n 1' 'head -1')
if [ -z "$recipe_line" ]; then
  bad "the header's sweep recipe command is gone — nothing in this file tells a reader how to find a writer-into-head site"
elif [ -z "$recipe_ere" ]; then
  bad "cannot lift the sweep recipe's own ERE from its command line — the extraction this check needs has rotted, so the check would pass without testing anything ([$recipe_line])"
elif ! grep -qF "$recipe_needle" <<<"$recipe_line"; then
  bad "the header's sweep recipe no longer covers a trailing 'head -1' — the recipe would again be read as complete while writer-into-head sites stay live (deploy/backup/jomform-backup.sh env_value/container_for were exactly that): [$recipe_line]"
elif ! grep -qF "$recipe_needle" <<<"$selfscan_line"; then
  bad "this file's own self-scan no longer carries the same trailing-head alternative, so the recipe and the scan it is checked against have drifted apart: [$selfscan_line]"
else
  missed=""
  for _sp in "${head_spellings[@]}"; do
    grep -qE "$recipe_ere" <<<"writer | $_sp" || missed="${missed}[writer | $_sp] "
  done
  if [ -z "$missed" ]; then
    ok "the header's sweep recipe covers every one-line trailing-head spelling the file's own self-scan does ('head -n1', 'head -n 1', 'head -1'), so a reader's sweep and the in-file scan cannot disagree on what a live site is"
  else
    bad "the header's sweep recipe MISSES a trailing-head spelling this file's own self-scan still finds, so a reader running the documented sweep would report it complete with a live site standing: $missed"
  fi
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
elif grep -q -- '-timeout 0' <<<"$argv"; then
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
  # 18b. THE RUN FINISHED, SO ITS RECORD MUST SAY SO (card t_d49a99f1).
  #      Without this arm the reuse lane could die with EVERY OTHER CHECK GREEN:
  #      if the runner stopped reaching gr_record_final, every record a real run
  #      left would say `complete: false`, `preflight_reuse.sh` would refuse each
  #      one as INCOMPLETE, every lane would re-gate — and nothing would red,
  #      because refusing an unfinished record is the correct behaviour. The
  #      assertion is taken from the record the REAL runner above just wrote, not
  #      from a fixture: a fixture cannot see this wiring.
  rec_complete="$(gr_read_bool "$rec" complete)"
  if [ "$rec_complete" = "true" ]; then
    ok "a finished run marks its record complete (complete=true), so the record is a verdict and not a mid-run snapshot"
  else
    bad "the record a finished run left says complete=[$rec_complete] — every lane's --reuse-record would be refused as INCOMPLETE and the reuse feature would be dead while every other check stayed green (the runner is not reaching gr_record_final on its normal end path)"
  fi
  if PREFLIGHT_KEY_EXTRA=probe15 gr_record_usable "$probe_tree" >/dev/null 2>&1; then
    ok "the finished run's record is USABLE — gr_record_usable accepts it (key, freshness and completeness all hold)"
  else
    bad "the finished run's record is not usable: [$(PREFLIGHT_KEY_EXTRA=probe15 gr_record_usable "$probe_tree" 2>&1)] — a record its own runner refuses cannot be the evidence a lane reuses"
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
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "pass", "seconds": 5, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "finished_at": "%s"\n}\n' \
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
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "fail", "seconds": 3, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "red",\n  "complete": true,\n  "finished_at": "%s"\n}\n' \
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
#
#     THE RECORD'S AGE IS PINNED BY THE FIXTURE, NOT BY ELAPSING WALL-CLOCK TIME
#     (card t_491cbf28). This check used to write a record with one real run and
#     read it back under `PREFLIGHT_RESULT_TTL=1`, relying on the second (equally
#     real) invocation to have spent more than a second before it read — and a real
#     invocation runs whatever the SELECTED GATE is, so the window it had to
#     outlast was one gate's own runtime (`$probe_gate` is `build`, first in
#     `--list`). Nothing guaranteed that window: when the read landed INSIDE the 1s
#     horizon the record was still fresh, the refusal came from the RED-verdict
#     branch instead of the horizon, the message did not match and the check
#     reported `bad` for a defect that was not there. Measured: it reddened a real
#     full preflight run on a loaded host (tree 42d7ed7d, `selftest` failed on this
#     one assertion, 84/1) and passed solo on the same bytes — a flake in the only
#     merge gate this repo has. The horizon is a function of (finished_at, TTL) and
#     both are the FIXTURE's to set, so the record is hand-written with a
#     `finished_at` two hours in the past — exactly as check 20 hand-writes one to
#     pin the on-disk shape — and the verdict can no longer be won by a fast read
#     or lost by a slow host.
#
#     AND THE REFUSAL IS ATTRIBUTED, NOT MERELY COUNTED. A refusal for ANY reason
#     satisfies a bare `rc -ne 0`, which is how a record that is unusable for some
#     other cause would read as "the horizon refused it" — the false-green shape
#     this file keeps finding. So the second leg reads the SAME aged record under a
#     horizon that CONTAINS it (the documented 86400s default) and requires it to be
#     REUSED: a record refused under the 1s horizon but refused again under 86400
#     would be a broken fixture, and this check would say so instead of passing.
if [ -n "$probe_tree" ]; then
  rec="$(gr_file "$probe_tree")"
  # A real run materialises the record for the key's sake ONLY: the fixture must
  # carry the fingerprint the runner computes for itself, or the read refuses for a
  # moved key (a different, uninteresting reason) instead of for the horizon. The
  # run's own verdict is irrelevant — the file is rewritten wholesale below.
  PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" >/dev/null 2>&1
  key="$(gr_read_field "$rec" key)"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe15 gr_key_parts "$SB")")"
  aged="$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ)"
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "pass", "seconds": 3, "at": "%s", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "finished_at": "%s"\n}\n' \
    "$probe_tree" "$key" "$parts" "$probe_gate" "$aged" "$aged" > "$rec"
  # The fixture IS the check: assert the observed pass landed before reading, or a
  # lost entry would make both legs agree for no reason (the same guard check 23
  # applies to its own hand-written record).
  fixture_pass="$(gr_gate_status "$rec" "$probe_gate")"
  out=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_RESULT_TTL=1 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
  rc=$?
  if [ "$fixture_pass" != "pass" ]; then
    bad "check 22's fixture did not land ($probe_gate=$fixture_pass) — the horizon assertion below would be about a record this check never wrote"
  elif [ $rc -ne 0 ] && grep -q 'older than the 1s horizon' <<<"$out"; then
    # THE ATTRIBUTION LEG. Same bytes, same key, same tree — a horizon wide enough
    # to contain the 2h-old record. A record that is unusable for any OTHER reason
    # refuses here too, so this leg cannot pass on a fixture that merely looks stale.
    reused_out=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_RESULT_TTL=86400 ./scripts/preflight.sh --only "$probe_gate" --reuse-record 2>&1)
    reused_rc=$?
    if [ $reused_rc -eq 0 ] && grep -q 'REUSED' <<<"$reused_out" && ! grep -q 'gate \[' <<<"$reused_out"; then
      ok "a record past its TTL is refused naming the horizon, and the SAME record inside a wider horizon is reused without running a gate (so the refusal is the horizon, not the fixture)"
    else
      bad "the aged fixture is refused under a 1s horizon but NOT reusable under 86400s (rc=$reused_rc) — the record is unusable for a reason other than the horizon, so the refusal above does not evidence the horizon logic: [$(printf '%s' "$reused_out" | tail -n 1)]"
    fi
  else
    bad "a stale record must be refused, got rc=$rc [$(printf '%s' "$out" | tail -n 1)]"
  fi
fi

# 23. THE HAPPY PATH, MEASURED RATHER THAN ASSERTED. With a record that vouches
#     green for the WHOLE inventory, `--reuse-record` must (a) exit 0 without
#     executing a single gate — acceptance #2 of the card: a lane about to gate an
#     already-green tree does not queue for a pool slot — (b) report the result as
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
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
  # no gate ran, so nothing queued for, or held, a pool slot.
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
    printf '  },\n  "runs": 1,\n  "verdict": "red",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
      *"$g"*) grep -qw "$g" <<<"$gap_lines" && leaked="$leaked $g" ;;
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

# 28. A WHOLE-INVENTORY REUSE MUST NOT TAKE A GATE SLOT — the runner's reason for
#     existing. Check 20 proves no GATE runs, but it runs under the self-test's
#     PRIVATE pool path ($PREFLIGHT_LOCK_FILE points into $SB_STATE), so it can
#     never see whether the gate body had to WAIT. Measured on this host with a
#     real record (under the single lock this replaces): the reuse sat in `flock`
#     for the full lock wait (151s, killed) because the reuse decision was taken
#     INSIDE the lock — re-gating nothing, but queueing behind whatever held it,
#     which is exactly the cost the record removes.
#
#     This check holds the pool for real (a background flock on the SAME slot path
#     the script derives) and requires the reuse to answer WITHOUT waiting. The pool
#     path is private, so the check is hermetic and never touches the host's gate.
#
#     THE ASSERTION READS THE RUNNER'S OWN ANNOUNCEMENT, not a wording of this
#     file's: the pre-pool path prints "took NO slot and ran NO gate". The second
#     leg — the run did NOT exit 74 (a slot wait-out) — is what makes the first one
#     meaningful, since a runner that answered without the phrase would otherwise
#     be read as a pass.
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
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec"
  # Hold EVERY slot for the whole check, on descriptors this shell keeps: the pool
  # is what the reuse must not need. `-w 5` on the first take so an unexpected
  # failure to hold is visible rather than hanging the self-test.
  held_lock="$SB_STATE/held-gate.lock"
  : > "$held_lock"
  exec {h1}>>"$held_lock.slot1"
  exec {h2}>>"$held_lock.slot2"
  flock -w 5 -o "$h1" && flock -w 5 -o "$h2"
  if flock -n "$held_lock.slot1" true 2>/dev/null; then
    bad "check 28 could not hold the pool ($held_lock.slot1) — the no-wait property cannot be measured"
  else
    # A wait of 0 makes "did it queue?" binary: if the reuse still reaches the pool
    # it exits 74 instead of answering, so the check cannot hang.
    t0=$(date +%s)
    r2=$(PREFLIGHT_KEY_EXTRA=probe15 PREFLIGHT_LOCK_FILE="$held_lock" PREFLIGHT_LOCK_WAIT=0 \
         ./scripts/preflight.sh --reuse-record 2>&1)
    r2_rc=$?
    t1=$(date +%s)
    if [ "$r2_rc" -eq 0 ] && grep -q 'took NO slot' <<<"$r2" && ! grep -q 'could not get a gate slot' <<<"$r2"; then
      ok "a whole-inventory reuse answers WITHOUT taking a gate slot (rc=0 in $((t1-t0))s while every slot was held)"
    else
      bad "a whole-inventory reuse must not wait for a gate slot — rc=$r2_rc in $((t1-t0))s, output=[$(printf '%s' "$r2" | grep -E 'slots:|could not get a gate slot|REUSED' | tr '\n' ' ')]"
    fi
  fi
  exec {h1}>&-
  exec {h2}>&-
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
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
     grep -qw "$probe_gate" <<<"$r3_reuse"; then
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
    printf '  },\n  "runs": 1,\n  "verdict": "red",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
  printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n    "%s": { "status": "pass", "seconds": 5, "at": "2026-01-01T00:00:00Z", "reused": false }\n  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "finished_at": "%s"\n}\n' \
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
#     behind the host-wide slot pool — a waiter has been measured at 50-86 minutes — to
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
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe.log",\n  "finished_at": "%s"\n}\n' \
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
    # A FINISHED run: the record is only reusable when it says so (card t_d49a99f1).
    GR_COMPLETE=1
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
    # A FINISHED run: the record is only reusable when it says so (card t_d49a99f1).
    GR_COMPLETE=1
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
#
#     THE PATH, NOT THE BASENAME (card t_ec2aac42). Playwright evaluates a RegExp
#     `testMatch` against the spec's FULL PATH, so the split's patterns are written
#     against that path (`/\/builder-.*\.spec\.ts$/`). Matching them with `re.match`
#     against a bare basename reported every `builder-*` spec as unowned — measured
#     on this tree while the config also listed 0 builder tests under
#     `--project=feature-builder`, which is the same fact seen from the other side.
#     So each spec is matched as a synthetic path whose tail is the real path shape,
#     with `re.search` (a pattern may or may not be anchored).
#
#     testIgnore IS PART OF THE JUDGEMENT (card t_440c9417, option B: one owner per
#     test). A spec can be matched by a project's `testMatch` AND excluded by that
#     same project's `testIgnore`; the project then does not run it, so counting the
#     match alone as ownership is a lie in the direction that matters — the check
#     would keep calling a spec covered after it stopped being enumerated. So the
#     ownership test is `testMatch matches AND testIgnore does not`, and a spec no
#     project runs is allowed ONLY when it is declared in OWNED_ELSEWHERE below. That
#     declaration is verified in BOTH directions (the file must still exist; no
#     project may still run it), because "owned by another gate" and "run here" at the
#     same time is two owners again, which is the defect being fixed.
s39_out="$(python3 - "$LIVE_ROOT" <<'PYEOF' 2>&1
import re, sys, glob, os
root = sys.argv[1]
# Specs under web/e2e/ that a DIFFERENT gate owns, so no playwright project in this
# config may enumerate them. Each entry is checked, not trusted: the file must exist,
# and every project whose testMatch matches it MUST testIgnore it.
OWNED_ELSEWHERE = {
    # Needs the menufixture Go server (MENU_FIXTURE_URL, api/cmd/menu-fixture), which
    # this config's `next start` cannot serve at all; scripts/menu-fixture-e2e.sh runs
    # it through web/playwright.menufixture.config.ts as the `menufixture` gate.
    "menu-checkout-option-group.spec.ts": "menufixture",
}
cfg = os.path.join(root, "web", "playwright.config.ts")
specs = sorted(os.path.basename(p) for p in glob.glob(os.path.join(root, "web", "e2e", "*.spec.ts")))
if not specs:
    print("NO_SPECS"); sys.exit(0)
src = open(cfg, encoding="utf-8").read()
# Read the projects from the TEXT, not one line at a time: a project object may be
# formatted over several lines, and a line-based parse silently found NO project the
# moment `feature-core` was written that way (measured while adding testIgnore).
def unesc(s):
    # The config is TypeScript source, so its regex literal is JS-escaped: `\\.`
    # means a literal dot. Undo that one level before using it as a pattern — a
    # pattern that still says `\\.` matches nothing.
    return s.replace("\\\\", "\\")
marks = [m for m in re.finditer(r'\bname:\s*"([^"]+)"', src)]
projs = []
for k, m in enumerate(marks):
    chunk = src[m.end(): marks[k + 1].start() if k + 1 < len(marks) else len(src)]
    t = re.search(r'testMatch:\s*/(.+?)/(?=\s*[,}])', chunk, re.S)
    i = re.search(r'testIgnore:\s*/(.+?)/(?=\s*[,}])', chunk, re.S)
    if t:
        projs.append((m.group(1), unesc(t.group(1)), unesc(i.group(1)) if i else None))
if not projs:
    print("NO_PROJECTS"); sys.exit(0)
unowned, double, twoowners = [], [], []
for sp in specs:
    # The full path Playwright matches against, shaped like this tree's (`web/e2e/`).
    path = os.path.join(root, "web", "e2e", sp)
    runners = [name for name, pat, ign in projs
               if re.search(pat, path) and not (ign and re.search(ign, path))]
    if not runners:
        if sp not in OWNED_ELSEWHERE:
            unowned.append(sp)
    elif len(runners) > 1:
        double.append("%s(%s)" % (sp, ",".join(runners)))
    elif sp in OWNED_ELSEWHERE:
        twoowners.append("%s(%s+%s)" % (sp, runners[0], OWNED_ELSEWHERE[sp]))
stale = [sp for sp in OWNED_ELSEWHERE if sp not in specs]
if twoowners:
    print("TWOOWNERS " + " ".join(twoowners))
elif stale:
    print("STALE " + " ".join(stale))
elif unowned:
    print("UNOWNED " + " ".join(unowned))
elif double:
    print("DOUBLE " + " ".join(double))
else:
    declared = ",".join("%s=%s" % (sp, OWNED_ELSEWHERE[sp]) for sp in sorted(OWNED_ELSEWHERE)) or "(none)"
    print("OK %d %d %d %s" % (len(specs), len(projs), len(OWNED_ELSEWHERE), declared))
PYEOF
)"
case "$s39_out" in
  OK\ *)
    # shellcheck disable=SC2086
    set -- $s39_out
    ok "check 39 — every one of the $2 web/e2e specs is run by exactly ONE of the $3 playwright projects (no spec unowned, none double-run) and the $4 declared as owned by another gate ($5) is testIgnored by every project that matches it, so the split cannot drop coverage"
    ;;
  NO_SPECS)
    bad "check 39 — no *.spec.ts under $LIVE_ROOT/web/e2e, so the project split cannot be judged (a guard that reads nothing proves nothing)"
    ;;
  NO_PROJECTS)
    bad "check 39 — no project in web/playwright.config.ts declares a testMatch, so the split cannot be proven; an unnamed project would run EVERY spec and the a11y gate would silently be the whole directory again"
    ;;
  UNOWNED\ *)
    bad "check 39 — spec(s) match NO playwright project, so no gate runs them:${s39_out#UNOWNED}. A spec outside every project is a silent coverage loss; add it to a family's testMatch — or, when another gate really owns it, declare it (with its owner) in OWNED_ELSEWHERE of this check"
    ;;
  DOUBLE\ *)
    bad "check 39 — spec(s) match MORE THAN ONE project, so they run twice and the family split is not a split:${s39_out#DOUBLE}"
    ;;
  TWOOWNERS\ *)
    bad "check 39 — spec(s) declared as owned by ANOTHER gate are ALSO run by a playwright project here:${s39_out#TWOOWNERS}. One test with two owners is the defect this split exists to prevent: either the project owns it (drop the declaration) or it does not (testIgnore it there)"
    ;;
  STALE\ *)
    bad "check 39 — OWNED_ELSEWHERE names spec(s) that no longer exist under web/e2e:${s39_out#STALE}. A stale declaration hides a rename — the new file would then be judged unowned, or worse, silently re-owned"
    ;;
  *)
    bad "check 39 — the project/spec judgement did not run: [$s39_out]"
    ;;
esac

# 40. `--tree <sha>` MUST REACH THE DECISION, NOT JUST `--show` (card t_912df3a4).
#     `scripts/preflight_reuse.sh --tree <sha>` is documented as "decide for an
#     explicit tree", and a lane needs exactly that for bytes that are no longer
#     in its working tree: the tree it just pushed, or the tree before/after a
#     bite mutation. The decide path accepted the flag and DROPPED it — the
#     decision fell back to the WORKING tree, so both the answer and the tree
#     named in the exit-1 PREFLIGHT_EVIDENCE line were about a tree the caller had
#     not asked about (measured: `--tree <sha>` -> "no reusable record for tree
#     <worktree short sha>", rc=1, while `--show --tree <sha>` on the same bytes
#     said `reusable: YES`). It failed SAFE — rc=1 means "gate normally", so it
#     could not hand out a pass for ungated bytes — but it cost the lane a full
#     gate run (the host-wide pool's waiter measures 50-86 minutes) for bytes that
#     were already vouched for.
#
#     THE FIXTURE MUST NAME A TREE THAT IS NOT THE WORKING TREE, or this check
#     would pass on the pre-fix script: check 24's `--tree "$probe_tree"` happens
#     to name exactly the tree the broken fallback answers about (the sandbox
#     working tree), which is measured why the defect survived a check that
#     already passed a `--tree` through the consumer. So a record is hand-written
#     here for a DIFFERENT tree sha, and the check asserts the discriminating
#     premise — the record IS green under the current inventory and environment,
#     and WITHOUT the flag the consumer answers about the working tree — before it
#     asserts anything about the flag. A red here can then only be the flag.
if [ -n "$probe_tree" ]; then
  # Derived, not invented: the same bytes give the same fixture tree on every run.
  other40="$(printf 'preflight_test check 40' | sha256sum | cut -c1-40)"
  work40="$(gr_tree_sha "$SB")"
  rec40="$(gr_file "$other40")"
  key40="$(PREFLIGHT_KEY_EXTRA=probe40 gr_key "$SB")"
  parts40="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe40 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
      "$other40" "$key40" "$parts40"
    # shellcheck disable=SC2086  # deliberate word splitting on the newline-separated inventory
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe40.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec40"
  # The hand-written gates block is the whole fixture, so assert it landed as
  # intended BEFORE reading anything from the runs below.
  fixture40_pass=0
  # shellcheck disable=SC2086
  for g in $GATES; do
    [ "$(gr_gate_status "$rec40" "$g")" = "pass" ] && fixture40_pass=$((fixture40_pass+1))
  done
  show40="$(PREFLIGHT_KEY_EXTRA=probe40 ./scripts/preflight_reuse.sh --show --tree "$other40" 2>&1)"
  dec40="$(PREFLIGHT_KEY_EXTRA=probe40 ./scripts/preflight_reuse.sh --tree "$other40" 2>&1)"; rc40=$?
  dec40eq="$(PREFLIGHT_KEY_EXTRA=probe40 ./scripts/preflight_reuse.sh "--tree=$other40" 2>&1)"; rc40eq=$?
  noflag40="$(PREFLIGHT_KEY_EXTRA=probe40 ./scripts/preflight_reuse.sh 2>&1)"; nrc40=$?
  if [ "$other40" = "$work40" ]; then
    bad "check 40's fixture is VACUOUS: the fabricated tree sha equals the sandbox working tree ($work40), which is exactly the tree the pre-fix fallback answers about"
  elif [ -z "$work40" ]; then
    bad "check 40 cannot identify the sandbox working tree, so the fallback it must discriminate against (the WORKING tree) cannot be asserted"
  elif [ "$fixture40_pass" != "$NGATES" ]; then
    bad "check 40's fixture did not land: only $fixture40_pass of $NGATES gate entries are 'pass' in $rec40, so a 'none' from the decide path would not evidence anything about the flag"
  elif ! grep -qxF -- '--- reusable: YES' <<<"$show40"; then
    bad "check 40's fixture record is not reusable under the CURRENT inventory and environment (--show said [$(printf '%s' "$show40" | tail -n 1)]), so 'none' from the decide path would not be the flag being dropped"
  elif grep -qF "$other40" <<<"$noflag40"; then
    bad "check 40's premise is broken: WITHOUT --tree the consumer already answered about $other40, so this fixture does not discriminate the flag"
  elif ! grep -qF "for tree $(printf '%.7s' "$work40")" <<<"$noflag40"; then
    bad "check 40's premise is broken: WITHOUT --tree (rc=$nrc40) the consumer did not answer about the WORKING tree ($work40), so the fallback this check discriminates is not the one under test: [$(printf '%s' "$noflag40" | tr '\n' ' ')]"
  elif [ "$rc40" -ne 0 ] || \
       ! grep -qxF "PREFLIGHT_TREE=$other40" <<<"$dec40" || \
       ! grep -qxF "PREFLIGHT_DECISION=full" <<<"$dec40" || \
       ! grep -qF "tree $(printf '%.7s' "$other40")" <<<"$dec40"; then
    bad "check 40 — \`--tree <sha>\` must decide about the tree it was given: rc=$rc40 [$(printf '%s' "$dec40" | tr '\n' ' ')] (expected rc=0, PREFLIGHT_TREE=$other40, PREFLIGHT_DECISION=full, and the evidence naming tree $(printf '%.7s' "$other40"))"
  elif [ "$rc40eq" -ne 0 ] || ! grep -qxF "PREFLIGHT_TREE=$other40" <<<"$dec40eq"; then
    bad "check 40 — the \`--tree=<sha>\` spelling is the same flag and must answer the same: rc=$rc40eq [$(printf '%s' "$dec40eq" | tr '\n' ' ')]"
  else
    ok "check 40 — \`--tree <sha>\` decides about THAT tree (both spellings: rc=0, PREFLIGHT_DECISION=full, PREFLIGHT_TREE=$other40, evidence naming tree $(printf '%.7s' "$other40")) on a record green under the current inventory and environment, while the same command without the flag answers about the WORKING tree ($work40)"
  fi
fi

# 41. `--show`'s EXIT STATUS IS PART OF ITS ANSWER (card t_c0022f86). The file's
#     own USAGE contract says exit 0 = reusable, 1 = not reusable ("no record,
#     wrong tree, stale, or the environment changed") — and the EXECUTED `--show`
#     printed "no record at <file>" and exited 0. `pr_show` itself DID return 1
#     (its "no record" and "cannot identify the working tree" returns predate this
#     card, and leg (8) below is that third return), but the `show)` branch called
#     it and DROPPED the status: the sourced-only `if` at the bottom of the file
#     is FALSE when executed, and a false `if` is what the shell leaves as the
#     script's status. Measured on this check's own parent revision, in this
#     repository's worktree:
#       out=$(bash scripts/preflight_reuse.sh --show --tree $FAB 2>&1); echo $?
#       -> "no record at /var/lib/jomform/gate/5f704744….json", rc=0
#     while `pr_show` sourced on the same tree returned 1. So the machine-readable
#     half of the tool said "yes" to a caller whose own output said there was
#     nothing there.
#
#     THE FIX IS THAT THE STATUS REPEATS THE VERDICT, which is why this check is
#     discriminating rather than "always 1": a hand-written full-green fixture must
#     exit 0 with `reusable: YES`, the SAME record read under a changed fingerprint
#     must exit 1 with `reusable: NO`, and every shape with nothing to show must
#     exit 1 with its own message. Legs (4)/(5) pin the two spellings the USAGE
#     advertises, and leg (8) drives the third return in an UNBORN repo — all three
#     "nothing to show" returns ride the one propagation line.
if [ -n "$probe_tree" ]; then
  other41="$(printf 'preflight_test check 41' | sha256sum | cut -c1-40)"
  absent41="$(printf 'preflight_test check 41 absent' | sha256sum | cut -c1-40)"
  work41="$(gr_tree_sha "$SB")"
  rec41="$(gr_file "$other41")"
  key41="$(PREFLIGHT_KEY_EXTRA=probe41 gr_key "$SB")"
  parts41="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe41 gr_key_parts "$SB")")"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
      "$other41" "$key41" "$parts41"
    # shellcheck disable=SC2086  # deliberate word splitting on the newline-separated inventory
    printf '%s\n' $GATES | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' \
      | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "log": "/tmp/probe41.log",\n  "finished_at": "%s"\n}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$rec41"
  # The hand-written gates block is the whole fixture: assert that it LANDED (every
  # inventory gate `pass`) before reading anything out of the runs below, or a
  # `reusable: NO` there would not evidence the status carrying the verdict.
  fixture41_pass=0
  # shellcheck disable=SC2086
  for g in $GATES; do
    [ "$(gr_gate_status "$rec41" "$g")" = "pass" ] && fixture41_pass=$((fixture41_pass+1))
  done
  show_yes="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show --tree "$other41" 2>&1)"; rc_yes=$?
  show_yes_eq="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show --tree="$other41" 2>&1)"; rc_yes_eq=$?
  show_env="$(PREFLIGHT_KEY_EXTRA=probe41-bumped ./scripts/preflight_reuse.sh --show --tree "$other41" 2>&1)"; rc_env=$?
  show_none="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show --tree "$absent41" 2>&1)"; rc_none=$?
  show_none_eq="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show --tree="$absent41" 2>&1)"; rc_none_eq=$?
  # The WORKING tree with no record — the exact shape the card was filed from.
  rm -f "$(gr_file "$probe_tree")"
  show_work="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show 2>&1)"; rc_work=$?
  show_work_empty="$(PREFLIGHT_KEY_EXTRA=probe41 ./scripts/preflight_reuse.sh --show --tree "" 2>&1)"; rc_work_empty=$?
  # The THIRD way of having nothing to show: a repo with no HEAD at all, so the
  # working tree cannot be identified. Built OUTSIDE the sandbox repo (it is host
  # state, like the record store) so planting it cannot move the tree sha that this
  # check and its siblings read.
  unborn41="$SB_STATE/unborn41"
  mkdir -p "$unborn41/scripts"
  cp "$SB"/scripts/*.sh "$unborn41/scripts/" 2>/dev/null
  git -C "$unborn41" init -q . 2>/dev/null
  show_unborn="$(cd "$unborn41" && PREFLIGHT_KEY_EXTRA=probe41 bash ./scripts/preflight_reuse.sh --show 2>&1)"; rc_unborn=$?
  if [ "$other41" = "$work41" ]; then
    bad "check 41's fixture is VACUOUS: the fabricated tree sha equals the sandbox working tree ($work41), so the reusable/missing legs could not tell a record from no record"
  elif [ -z "$work41" ]; then
    bad "check 41 cannot identify the sandbox working tree, so the 'no record for the WORKING tree' leg cannot be asserted at all"
  elif [ "$fixture41_pass" != "$NGATES" ]; then
    bad "check 41's fixture did not land: only $fixture41_pass of $NGATES gate entries are 'pass' in $rec41, so a 'reusable: NO' below would not evidence the status carrying the verdict"
  elif [ "$rc_yes" -ne 0 ] || ! grep -qxF -- '--- reusable: YES' <<<"$show_yes"; then
    bad "check 41 — a record that IS reusable must exit 0 and say so: rc=$rc_yes [$(printf '%s' "$show_yes" | tail -n 1)] (the status must repeat the verdict, so it cannot be 'always 1')"
  elif [ "$rc_yes_eq" -ne 0 ] || ! grep -qxF -- '--- reusable: YES' <<<"$show_yes_eq"; then
    bad "check 41 — the \`--tree=<sha>\` spelling is the same flag and must answer the same: rc=$rc_yes_eq [$(printf '%s' "$show_yes_eq" | tail -n 1)]"
  elif [ "$rc_env" -ne 1 ] || ! grep -qxF -- '--- reusable: NO' <<<"$show_env" || \
       ! grep -qF 'the environment changed since that record' <<<"$show_env"; then
    bad "check 41 — a record that is NOT reusable must exit 1 (it just printed 'reusable: NO'): rc=$rc_env [$(printf '%s' "$show_env" | tail -n 2 | tr '\n' '|')]"
  elif [ "$rc_none" -ne 1 ] || ! grep -qxF "no record at $(gr_file "$absent41")" <<<"$show_none"; then
    bad "check 41 — \`--show --tree <sha>\` with no record must exit 1 and say which record is missing: rc=$rc_none [$show_none]"
  elif [ "$rc_none_eq" -ne 1 ] || ! grep -qxF "no record at $(gr_file "$absent41")" <<<"$show_none_eq"; then
    bad "check 41 — the \`--tree=<sha>\` spelling must exit 1 on the same missing record: rc=$rc_none_eq [$show_none_eq]"
  elif [ "$rc_work" -ne 1 ] || ! grep -qxF "no record at $(gr_file "$probe_tree")" <<<"$show_work"; then
    bad "check 41 — \`--show\` for the WORKING tree with no record must exit 1 (the card's own shape: the process reported success on a message that said the record was missing): rc=$rc_work [$(printf '%s' "$show_work" | tail -n 2 | tr '\n' '|')]"
  elif [ "$rc_work_empty" -ne 1 ] || ! grep -qxF "no record at $(gr_file "$probe_tree")" <<<"$show_work_empty"; then
    bad "check 41 — \`--show --tree \"\"\` falls back to the working tree and must exit 1 there too: rc=$rc_work_empty [$(printf '%s' "$show_work_empty" | tail -n 2 | tr '\n' '|')]"
  elif [ "$rc_unborn" -ne 1 ] || ! grep -qxF 'cannot identify the working tree' <<<"$show_unborn"; then
    bad "check 41 — a tree that cannot be identified is the third 'nothing to show' return and must exit 1: rc=$rc_unborn [$show_unborn]"
  else
    ok "check 41 — \`--show\`'s exit status repeats the verdict it printed: rc=0 + 'reusable: YES' on a full-green record for an explicit tree (both spellings), rc=1 + 'reusable: NO' on the SAME record under a changed fingerprint, and rc=1 with its own message for every shape that has nothing to show (no record for the given tree, no record for the WORKING tree, an empty --tree, and a tree that cannot be identified)"
  fi
fi

# 42. AN UNUSABLE EXPLICIT TREE IS A USAGE ERROR, NEVER A VERDICT ABOUT THE WORKING
#     TREE (card t_916280c4). `--tree <sha>` names the bytes the caller cares
#     about — usually bytes that are NOT in its working tree — and the value is
#     ALSO the record's FILENAME (`gr_file` builds `<store>/<value>.json`), so a
#     value that cannot be a tree sha has to be refused BEFORE any mode answers it.
#     Measured on the parent revision, from a repo root with the test env sourced:
#
#       $ bash scripts/preflight_reuse.sh --tree          # flag written, no value
#       -> "no reusable record for tree <this checkout>", rc=1   # silent fallback
#       $ bash scripts/preflight_reuse.sh --tree banana
#       -> "no reusable record for tree banana", rc=1     # a VERDICT-shaped answer
#                                                         # about something that is
#                                                         # not a tree at all
#       $ bash scripts/preflight_reuse.sh --report 1 --tree <sha>
#       -> the ledger, rc=0                               # the flag is dropped
#
#     The first two are decisions about the WORKING tree — the one question this
#     tool must never answer by accident (`preflight.sh`'s own drift check 21 is
#     the same class) — and the third is a report that was asked about a tree.
#     `../gate/<sha>` is the sharpest form of the second: the value built a path
#     OUTSIDE the record store. That direction fails safe for the reuse decision
#     itself (`gr_record_usable` refuses a record whose own `tree` field is not the
#     requested string), but a consumer that cannot be asked about a tree is not a
#     consumer this repo can rely on, and the value reaches `gr_ledger`'s JSON
#     unescaped once a record does match.
#
#     THE WIRING HALF IS CHECK 40's JOB, NOT THIS ONE'S. Check 40 pins that
#     `--tree <sha>` REACHES the decision, and the bite harness's case 9 requires
#     check 40 — and only check 40 — to be the red when that wiring is removed. A
#     leg here asserting "the answer names the requested tree" would make case 9's
#     red unattributable, so every leg below is about the REFUSAL, and the one leg
#     that drives a well-formed sha asserts only that it is NOT refused as a usage
#     error (rc=1, the documented "gate normally" path) — true on the shipped bytes
#     and on a mutant that lost the wiring.
if [ -n "$probe_tree" ]; then
  work42="$probe_tree"
  short42="$(printf '%.7s' "$work42")"
  hex42="$(printf 'preflight_test check 42' | sha256sum | cut -c1-40)"
  hex7_42="$(printf '%.7s' "$hex42")"
  esc42="../gate/$hex42"
  noval42="$(./scripts/preflight_reuse.sh --tree 2>&1)"; rc_noval42=$?
  ban42="$(./scripts/preflight_reuse.sh --tree banana 2>&1)"; rc_ban42=$?
  esc_out42="$(./scripts/preflight_reuse.sh --tree "$esc42" 2>&1)"; rc_esc42=$?
  show_esc42="$(./scripts/preflight_reuse.sh --show --tree ../../etc/passwd 2>&1)"; rc_showesc42=$?
  abbr42="$(./scripts/preflight_reuse.sh --tree "$hex7_42" 2>&1)"; rc_abbr42=$?
  rep42="$(./scripts/preflight_reuse.sh --report 1 --tree "$hex42" 2>&1)"; rc_rep42=$?
  rep_ok42="$(./scripts/preflight_reuse.sh --report 1 2>&1)"; rc_repok42=$?
  src42="$(./scripts/preflight_reuse.sh --source --tree "$hex42" 2>&1)"; rc_src42=$?
  ok42="$(./scripts/preflight_reuse.sh --tree "$hex42" 2>&1)"; rc_ok42=$?
  if [ -z "$short42" ]; then
    bad "check 42 cannot identify the sandbox working tree, so the fallback every leg below discriminates against (the WORKING tree) cannot be asserted"
  elif [ "$rc_noval42" -ne 2 ] || ! grep -qF -- '--tree needs a tree sha' <<<"$noval42" || \
       grep -qF "for tree $short42" <<<"$noval42"; then
    bad "check 42 — \`--tree\` with NO value must be a usage error (rc=2) naming the flag, never a fallback to the WORKING tree ($short42): rc=$rc_noval42 [$(printf '%s' "$noval42" | tr '\n' ' ')]"
  elif [ "$rc_ban42" -ne 2 ] || ! grep -qF 'is not a tree sha' <<<"$ban42" || \
       ! grep -qF 'banana' <<<"$ban42" || grep -qF "for tree $short42" <<<"$ban42"; then
    bad "check 42 — \`--tree banana\` must be refused (rc=2) NAMING the value, rather than answered as a decision about it or about the working tree: rc=$rc_ban42 [$(printf '%s' "$ban42" | tr '\n' ' ')]"
  elif [ "$rc_esc42" -ne 2 ] || ! grep -qF 'is not a tree sha' <<<"$esc_out42" || \
       ! grep -qF "$esc42" <<<"$esc_out42"; then
    bad "check 42 — a \`--tree\` carrying a path ($esc42 — the value that escapes the record store) must be refused (rc=2) naming it: rc=$rc_esc42 [$(printf '%s' "$esc_out42" | tr '\n' ' ')]"
  elif [ "$rc_showesc42" -ne 2 ] || grep -qF '/etc/passwd.json' <<<"$show_esc42"; then
    bad "check 42 — a value that is NOT a tree sha must be refused BEFORE it is turned into a record path: \`--show --tree ../../etc/passwd\` must exit 2 without ever naming that path: rc=$rc_showesc42 [$(printf '%s' "$show_esc42" | tr '\n' ' ')]"
  elif [ "$rc_abbr42" -ne 2 ] || ! grep -qF 'is 7 characters' <<<"$abbr42"; then
    bad "check 42 — an ABBREVIATED sha ($hex7_42) can never name a record (a record file is named by \`git write-tree\`'s full 40-hex output), so it must be refused (rc=2) rather than answered as 'no record': rc=$rc_abbr42 [$(printf '%s' "$abbr42" | tr '\n' ' ')]"
  elif [ "$rc_rep42" -ne 2 ] || ! grep -qF -- '--report' <<<"$rep42" || \
       ! grep -qF -- '--tree' <<<"$rep42"; then
    bad "check 42 — \`--tree\` is meaningless in \`--report\` mode (the report sums the reuse ledger, not a tree) and must be refused (rc=2) rather than silently ignored: rc=$rc_rep42 [$(printf '%s' "$rep42" | tr '\n' ' ')]"
  elif [ "$rc_repok42" -ne 0 ]; then
    bad "check 42 — the CONTROL for the leg above: \`--report\` WITHOUT \`--tree\` must still exit 0, else 'always 2' would satisfy it: rc=$rc_repok42 [$(printf '%s' "$rep_ok42" | tr '\n' ' ')]"
  elif [ "$rc_src42" -ne 2 ] || ! grep -qF -- '--source' <<<"$src42"; then
    bad "check 42 — \`--tree\` is meaningless in \`--source\` mode too (it only hands the library to the caller) and must be refused (rc=2), not ignored: rc=$rc_src42 [$(printf '%s' "$src42" | tr '\n' ' ')]"
  elif [ "$rc_ok42" -ne 1 ] || grep -qF 'is not a tree sha' <<<"$ok42" || \
       grep -qF -- '--tree needs a tree sha' <<<"$ok42" || grep -qF 'meaningless with' <<<"$ok42"; then
    bad "check 42 — the OTHER direction: a well-formed 40-hex tree sha must NOT be refused as a usage error, it takes the documented rc=1 'gate normally' path: rc=$rc_ok42 [$(printf '%s' "$ok42" | tr '\n' ' ')]"
  else
    ok "check 42 — an unusable \`--tree\` is a USAGE error, never a verdict about the WORKING tree ($short42): no value / 'banana' / '$esc42' / '../../etc/passwd' / a 7-character abbreviation each exit 2 naming the value and never name a record path, \`--tree\` is refused in \`--report\` (while plain \`--report\` still exits 0) and in \`--source\`, and a well-formed 40-hex value still takes the documented rc=1 'no record' path"
  fi
fi

# 43. THE RECORD'S PROVENANCE LABEL MUST NAME THE BYTES ITS `tree` COVERS (card
#     t_6c9cca01). `tree` is the reuse key and it is the WORKING TREE, so a run can
#     gate bytes no commit names — and the merge lane is the systematic case: it
#     merges into the index and working tree with HEAD still at the PRE-MERGE base
#     tip, so `git rev-parse --short HEAD` names the commit whose bytes were NOT
#     gated. Measured on this host before the fix: the record for tree 08a5120f
#     (the v1.21.197 merge, == `a5ab7eca^{tree}`) carried `commit 087f1d3e`, which is
#     the v1.21.196 merge, tree 064fb586 — a deploy lane citing `commit` cites the
#     wrong artefact. Reuse itself was never wrong (it keys on `tree`); the LABEL
#     lied, and the record is the evidence a lane quotes.
#
#     THE SHAPE IS REPRODUCED, NOT DESCRIBED: a two-commit repo, `git worktree add
#     --detach` at the base tip, then a NON-COMMITTING merge into that worktree —
#     exactly what a merge lane leaves behind. The REAL writer (`gr_flush`) records
#     it, and the record must not label the merged bytes with HEAD's commit.
#
#     THE CONTROL IS LOAD-BEARING: a clean worktree at that same commit must still
#     read `commit=<sha>` (no marker) with `gated_is_head: true`. Without it,
#     "always append +uncommitted" would satisfy every leg below while making the
#     honest case unreadable.
mw_root="$SB_STATE/provenance43"
mw_repo="$mw_root/repo"
mw_wt="$mw_root/wt-merged"
mw_clean="$mw_root/wt-clean"
mkdir -p "$mw_root"
mw_err=""
mw_base=""; mw_topic=""; mw_tree=""; mw_head_tree=""; mw_head_short=""
mw_commit=""; mw_rec_head_tree=""; mw_rec_is_head=""; mw_rec_tree=""
mw_ctl_tree=""; mw_ctl_head_short=""; mw_ctl_commit=""; mw_ctl_rec_head_tree=""; mw_ctl_is_head=""

# mw_fixture — build the merge-lane shape with every step's failure NAMED, so a
# fixture that did not land is a loud `bad` instead of a vacuous green.
mw_fixture() {
  git init -q -b base "$mw_repo" >/dev/null 2>&1 || { mw_err="git init"; return 1; }
  git -C "$mw_repo" config user.email preflight-test@example.invalid || { mw_err="git config user.email"; return 1; }
  git -C "$mw_repo" config user.name preflight-test || { mw_err="git config user.name"; return 1; }
  printf 'base\n' > "$mw_repo/base.txt"
  git -C "$mw_repo" add -A >/dev/null 2>&1 || { mw_err="git add base.txt"; return 1; }
  git -C "$mw_repo" commit -q -m base >/dev/null 2>&1 || { mw_err="commit base"; return 1; }
  mw_base="$(git -C "$mw_repo" rev-parse HEAD)" || { mw_err="rev-parse the base commit"; return 1; }
  git -C "$mw_repo" checkout -q -b topic >/dev/null 2>&1 || { mw_err="checkout -b topic"; return 1; }
  printf 'topic\n' > "$mw_repo/topic.txt"
  git -C "$mw_repo" add -A >/dev/null 2>&1 || { mw_err="git add topic.txt"; return 1; }
  git -C "$mw_repo" commit -q -m topic >/dev/null 2>&1 || { mw_err="commit topic"; return 1; }
  mw_topic="$(git -C "$mw_repo" rev-parse HEAD)" || { mw_err="rev-parse the topic commit"; return 1; }
  git -C "$mw_repo" checkout -q base >/dev/null 2>&1 || { mw_err="checkout base"; return 1; }
  git -C "$mw_repo" worktree add -q --detach "$mw_wt" "$mw_base" >/dev/null 2>&1 \
    || { mw_err="git worktree add --detach (the merge-lane worktree)"; return 1; }
  git -C "$mw_wt" merge --no-ff --no-commit "$mw_topic" >/dev/null 2>&1 \
    || { mw_err="git merge --no-commit (which is how a lane holds merged content with HEAD unmoved)"; return 1; }
  git -C "$mw_repo" worktree add -q --detach "$mw_clean" "$mw_base" >/dev/null 2>&1 \
    || { mw_err="git worktree add --detach (the CLEAN control worktree)"; return 1; }
  return 0
}

# mw_flush <tree> <commit-label> <result-dir> <repo> — drive the REAL writer (never
# a re-implementation) in a subshell so its GR_* globals cannot leak into the
# checks below.
mw_flush() {
  (
    GR_GATE_NAMES=("$probe_gate")
    GR_STATUS[$probe_gate]=pass
    GR_SECONDS[$probe_gate]=1
    GR_AT[$probe_gate]="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    GR_REUSED[$probe_gate]=false
    GR_TREE="$1"; GR_TREE_SHORT="$(gr_short_sha "$1")"; GR_REPO="$4"
    GR_COMMIT="$2"; GR_BRANCH=HEAD; GR_PID=$$
    GR_PASSED=1; GR_FAILED=0; GR_SKIPPED=0; GR_SELECTED=""
    # This fixture models a run that finished, which is the only kind that is
    # reusable (card t_d49a99f1) — the legs below read the record's LABELS, but a
    # record that claimed otherwise would be a different fixture than the one the
    # comment describes.
    GR_COMPLETE=1
    PREFLIGHT_RESULT_DIR="$3" gr_flush 1
  )
}

if ! mw_fixture; then
  bad "check 43 — the merge-lane fixture could not be built ($mw_err), so the provenance label was never exercised; a fixture that does not land proves nothing"
else
  mw_tree="$(gr_tree_sha "$mw_wt")"
  mw_head_tree="$(git -C "$mw_wt" rev-parse 'HEAD^{tree}' 2>/dev/null)"
  mw_head_short="$(git -C "$mw_wt" rev-parse --short HEAD 2>/dev/null)"
  mw_ctl_tree="$(gr_tree_sha "$mw_clean")"
  mw_ctl_head_short="$(git -C "$mw_clean" rev-parse --short HEAD 2>/dev/null)"
  mw_flush "$mw_tree" "$mw_head_short" "$mw_root/records-merged" "$mw_wt" || true
  mw_flush "$mw_ctl_tree" "$mw_ctl_head_short" "$mw_root/records-clean" "$mw_clean" || true
  mw_rec="$mw_root/records-merged/${mw_tree}.json"
  mw_ctl_rec="$mw_root/records-clean/${mw_ctl_tree}.json"
  mw_rec_tree="$(gr_read_field "$mw_rec" tree)"
  mw_commit="$(gr_read_field "$mw_rec" commit)"
  mw_rec_head_tree="$(gr_read_field "$mw_rec" head_tree)"
  mw_rec_is_head="$(gr_read_bool "$mw_rec" gated_is_head)"
  mw_ctl_commit="$(gr_read_field "$mw_ctl_rec" commit)"
  mw_ctl_rec_head_tree="$(gr_read_field "$mw_ctl_rec" head_tree)"
  mw_ctl_is_head="$(gr_read_bool "$mw_ctl_rec" gated_is_head)"
  if [ -z "$mw_tree" ] || [ -z "$mw_head_tree" ] || [ -z "$mw_ctl_tree" ]; then
    bad "check 43 — the fixture's tree shas could not be computed (merged=[$mw_tree] head_tree=[$mw_head_tree] clean=[$mw_ctl_tree]), so nothing about the label can be asserted"
  elif [ "$mw_tree" = "$mw_head_tree" ]; then
    bad "check 43 — THE FIXTURE DOES NOT REPRODUCE THE DEFECT: the merged worktree's working-tree sha [$mw_tree] equals its HEAD^{tree}, so HEAD's commit WOULD name these bytes and there is nothing for the label to get wrong"
  elif [ ! -r "$mw_rec" ] || [ ! -r "$mw_ctl_rec" ]; then
    bad "check 43 — the real gr_flush wrote no record for the merged tree [$mw_rec] and/or the control [$mw_ctl_rec], so the label was never produced"
  elif [ "$mw_rec_tree" != "$mw_tree" ]; then
    bad "check 43 — the merged record names tree [$mw_rec_tree], not the tree it gated [$mw_tree]"
  elif [ "$mw_commit" != "${mw_head_short}+uncommitted" ]; then
    bad "check 43 — a record whose gated bytes are NOT HEAD's labels them with commit [$mw_commit]: HEAD is ${mw_head_short} (tree ${mw_head_tree:0:7}) while the gated tree is ${mw_tree:0:7}, so a reader who trusts \`commit\` cites the pre-merge base commit — the measured v1.21.197 shape (record tree 08a5120f, commit 087f1d3e)"
  elif [ "$mw_rec_is_head" != "false" ]; then
    bad "check 43 — the merged record says gated_is_head=[$mw_rec_is_head]; it must say false, or the record reads as if HEAD's commit described the merged bytes"
  elif [ "$mw_rec_head_tree" != "$mw_head_tree" ]; then
    bad "check 43 — the merged record's head_tree is [$mw_rec_head_tree], not HEAD^{tree} at the write [$mw_head_tree], so \`tree == head_tree\` is NOT decidable from the record alone"
  elif [ "$mw_ctl_tree" != "$mw_head_tree" ]; then
    bad "check 43 — the CONTROL worktree's tree [$mw_ctl_tree] is not the base tip's tree [$mw_head_tree], so it is not a clean checkout of the same commit and cannot control for the marker"
  elif [ "$mw_ctl_commit" != "$mw_ctl_head_short" ]; then
    bad "check 43 — the CONTROL broke the honest case: a clean worktree whose bytes ARE HEAD's must record commit [$mw_ctl_head_short] with no marker, got [$mw_ctl_commit] — a marker applied unconditionally makes every record unreadable"
  elif [ "$mw_ctl_is_head" != "true" ] || [ "$mw_ctl_rec_head_tree" != "$mw_ctl_tree" ]; then
    bad "check 43 — the CONTROL's record says gated_is_head=[$mw_ctl_is_head] head_tree=[$mw_ctl_rec_head_tree] for tree [$mw_ctl_tree]; a clean checkout must read true with head_tree == tree"
  else
    ok "check 43 — the record's provenance label names the bytes its \`tree\` covers: a merge-lane worktree (HEAD ${mw_head_short} unmoved at the base, working tree ${mw_tree:0:7} holding the merge, HEAD^{tree} ${mw_head_tree:0:7}) records commit [$mw_commit], head_tree [$mw_rec_head_tree] and gated_is_head [$mw_rec_is_head], while the CLEAN control at the same commit records commit [$mw_ctl_commit] with gated_is_head [$mw_ctl_is_head] — so \`tree == HEAD^{tree}\` is decidable from the record alone"
  fi
fi

# 44. A NARROWED RUN MUST NOT RE-EXECUTE WHAT ITS RECORD VOUCHED FOR (card
#     t_33570a36). `--for-diff <base> --reuse-record` is the shape the merge lane
#     gates with: narrow to what the diff can break, then re-run only what the
#     per-tree record cannot vouch for. When the record vouches for the WHOLE
#     narrowed selection there is nothing left to execute — and the run must
#     really execute nothing.
#
#     MEASURED BEFORE THE FIX, on a real tree whose `--for-diff origin/main`
#     selection was 9 gates, all recorded `pass`: the runner printed
#         ==> nothing to execute: every gate in this run's selection is a recorded pass
#     and three lines later `==> gate [build]: cd api && go build ./...`, then all
#     nine (790s); the summary named every one of them under BOTH `PASS` and
#     `REUSE`, and the ledger had already recorded the saving at second 0 for work
#     the same run then paid for. Cause: `selected()` returned out of the
#     inherited-SEL_DIFF arm before it ever read SKIP, and the reuse fold skips a
#     gate by writing into SKIP — so under `--for-diff` the fold was announced and
#     ledgered but could not take effect.
#
#     THE SANDBOX GATES FAIL FAST, AND THAT IS FINE HERE: the property asserted is
#     the ABSENCE of execution (`gate [` is the banner printed for every gate the
#     runner executes), and the exit status tells the same story from the other
#     side — before the fix this run went RED on a prerequisite of a gate it had
#     just announced as reused, which a record read can never be.
#
#     THE FIXTURE MUST NARROW: a selection that covers the whole inventory takes the
#     pre-pool whole-inventory path instead, so a full selection here would prove
#     nothing about the per-gate fold. That is asserted before anything is concluded
#     from the run, the same way check 43 refuses a fixture that does not reproduce
#     its defect.
s44_dir="$SB_STATE/s44"
rm -rf "$s44_dir"; mkdir -p "$s44_dir"
(
  cd "$SB" || exit 1
  # TWO COMMITS, and the first one is load-bearing: the sandbox may carry changes
  # from earlier checks, and `--for-diff` diffs the WORKING TREE against the base —
  # so basing on the init commit collected every one of them and the selection came
  # out as the whole inventory (measured: 22 of 22), which is the pre-pool path and
  # proves nothing here. Committing the current state first makes the base describe
  # "everything so far", and the second commit the only thing the diff can see.
  printf '\n<!-- probe: baseline -->\n' >> GATE-INVENTORY.md
  git add -A >/dev/null 2>&1 || exit 1
  git -c user.email=t@t -c user.name=t commit -q -m "probe: baseline" >/dev/null 2>&1 || exit 1
  # The narrowing change itself: a path that selects a subset of the inventory.
  printf '\n<!-- probe: narrows the selection -->\n' >> GATE-INVENTORY.md
  git add -A >/dev/null 2>&1 || exit 1
  git -c user.email=t@t -c user.name=t commit -q -m "probe: narrowing change" >/dev/null 2>&1 || exit 1
  . ./scripts/gate_result.sh
  tree="$(gr_tree_sha "$SB")"
  key="$(PREFLIGHT_KEY_EXTRA=probe44 gr_key "$SB")"
  parts="$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe44 gr_key_parts "$SB")")"
  # THE SELECTION IS TAKEN FROM THE RUNNER ITSELF (`--explain-diff`, the same
  # computation `--for-diff` does), never re-derived here: the sandbox carries
  # changes from earlier checks, so `--classify` of the one path this check touches
  # is NOT the selection the run will use — an earlier draft vouched for 4 gates
  # while the run selected 5 and then (correctly) wanted to execute the fifth.
  sel="$(./scripts/preflight.sh --explain-diff=HEAD~1 2>/dev/null | sed -n 's/^  gates selected ([0-9]* of [0-9]*): //p' | sed -n '1p')"
  printf '%s\n' "$sel" > "$s44_dir/sel"
  [ -n "${sel//[[:space:]]/}" ] || exit 1
  mkdir -p "$RECORD_DIR"
  {
    printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
      "$tree" "$key" "$parts"
    printf '%s\n' $sel | sed 's/.*/    "&": { "status": "pass", "seconds": 42, "at": "2026-01-01T00:00:00Z", "reused": false },/' | sed '$ s/,$//'
    printf '  },\n  "runs": 1,\n  "verdict": "green",\n  "complete": true,\n  "finished_at": "%s"\n}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "$RECORD_DIR/$tree.json"
  rm -f "$RECORD_DIR/reuse-ledger.jsonl"
  PREFLIGHT_KEY_EXTRA=probe44 ./scripts/preflight.sh --for-diff HEAD~1 --reuse-record > "$s44_dir/out" 2>&1
  echo $? > "$s44_dir/rc"
) >/dev/null 2>&1
s44_sel="$(cat "$s44_dir/sel" 2>/dev/null)"
s44_count="$(printf '%s' "$s44_sel" | wc -w)"
s44_rc="$(cat "$s44_dir/rc" 2>/dev/null)"
s44_out="$(cat "$s44_dir/out" 2>/dev/null)"
s44_secs=$(( s44_count * 42 ))
if [ -z "$s44_sel" ] || [ "${s44_count:-0}" -eq 0 ] || [ "${s44_count:-0}" -ge "$NGATES" ]; then
  bad "check 44 — the fixture did not narrow: the sandbox diff selected ${s44_count:-0} of $NGATES gates, so this run is not the per-gate reuse path and proves nothing about it (a whole-inventory selection is taken by the pre-pool path)"
elif [ "$s44_rc" != 0 ]; then
  bad "check 44 — a narrowed run whose record vouches for its whole selection ($s44_count gate(s): $(printf '%s' "$s44_sel" | tr '\n' ' ')) must execute nothing and stay green, but it exited $s44_rc — it demanded work for gates it had already announced as reused: [$(printf '%s\n' "$s44_out" | grep -E 'nothing to execute|gate \[|PREFLIGHT FAIL|REUSED' | tr '\n' ' ')]"
elif grep -q 'gate \[' <<<"$s44_out"; then
  bad "check 44 — the run announced a record read and then EXECUTED $(grep -c 'gate \[' <<<"$s44_out") gate(s): [$(printf '%s\n' "$s44_out" | grep 'gate \[' | tr '\n' ' ')] — the reuse fold wrote into SKIP but selected() never read it (this is the exact defect)"
elif ! grep -q 'nothing to execute' <<<"$s44_out"; then
  bad "check 44 — the run neither executed a gate nor announced that it had nothing to execute; a verdict with no execution and no announcement is unreadable: [$(printf '%s\n' "$s44_out" | tail -n 3 | tr '\n' ' ')]"
elif ! grep -q '^  REUSE: ' <<<"$s44_out" || grep -q '^  PASS: ' <<<"$s44_out"; then
  bad "check 44 — the vouched-for gates must be reported as REUSE and never as PASS (nothing was executed): [$(printf '%s\n' "$s44_out" | grep -E '^  (PASS|REUSE|SKIP):' | tr '\n' ' ')]"
elif ! grep -q "\"remembered_seconds\":$s44_secs" "$RECORD_DIR/reuse-ledger.jsonl" 2>/dev/null; then
  bad "check 44 — the saving this run really made (${s44_secs}s = $s44_count gates x 42s) is not on the ledger: [$(cat "$RECORD_DIR/reuse-ledger.jsonl" 2>/dev/null | tr '\n' ' ')]"
else
  ok "check 44 — a narrowed run whose record vouches for its whole selection ($s44_count of $NGATES gates) executes nothing, says so, counts those gates as REUSE and not PASS, and ledgers the ${s44_secs}s it really saved (rc=0)"
fi
# The ledger file is shared with checks 23/32; empty it so a later reader of this
# sandbox store cannot mistake check 44's fixture line for the happy-path one.
rm -f "$RECORD_DIR/reuse-ledger.jsonl"

# 45. TWO BROWSER SUITES MUST NEVER RUN AT ONCE ON THIS HOST (card t_d0c0981d).
#
#     THE DEFECT. The a11y and e2e gates each start the app's own `next start` on a port
#     claimed from 3100-3140 (see the claim block in the runner). The claim is PER PORT
#     on purpose, so two concurrent runs take DIFFERENT ports and BOTH PROCEED — which
#     is right for a port and wrong for a browser suite. Playwright's default worker
#     count is cpus/2, so on this 6-core host each suite starts three browsers, and two
#     suites at once put six browsers plus two servers on six cores. Measured
#     2026-10-28 02:24-03:12 UTC, two runs of `scripts/preflight.sh` on this host in the
#     same window (ports 3101 and 3100) both came back RED, each on a DIFFERENT spec:
#
#       port 3101: form-settings-header-actions.spec.ts:308
#                    expect(page.locator("a.jf-btn-ghost")).toHaveCount(3) -> received 0
#                    after the full 45s (89 polls — the page never had the anchors)
#                  stale-workspace-slug.spec.ts:253
#                    "/ never sent workspace=ws-gone, so this run reproduced nothing: []"
#                    (the page issued NO request at all, so nothing had rendered)
#       port 3100: form-settings-status-chips.spec.ts:165 "chips fit their labels in en"
#
#     Both trees' diffs were Go-only, so neither could reach either spec, and `--only
#     e2e` on the same tree ALONE ran 274 passed / 2 skipped / 0 failed in 615s with
#     both specs green. A page that renders nothing for 45s is a starved process, not a
#     false assertion — and a false red on e2e costs a lane 13 minutes and, on the merge
#     lane, bounces a card back to dev for a defect that does not exist.
#
#     WHAT THIS CHECK DRIVES. The SHIPPED ./scripts/preflight.sh, `--only e2e-core`, TWICE and
#     SIMULTANEOUSLY, in a THROWAWAY sandbox of its own — NOT $SB: the fixture plants an
#     untracked web/node_modules and its own git history, and doing that inside $SB would
#     move the working-tree sha the record checks (27-44) plant their fixtures against.
#     A STUB `npm` stands in for `playwright test`: it records the interval it was live
#     in and binds no port (the port claim is a different subject, pinned by
#     scripts/biteproof_a11y_port_claim.sh). A stub `node` answers the version floor and
#     the dependency check, and `.bin/next` + `.bin/playwright` are planted so the gate
#     reaches its BODY instead of dying on a prerequisite — a gate refused for a missing
#     prerequisite is not evidence about serialisation.
#
#     THE PROPERTY, in both directions: (a) both runs exit 0 — the loser WAITS, it is not
#     refused; (b) at least one of them announces the wait in the runner's own words; and
#     (c) the two stub INTERVALS DO NOT OVERLAP, i.e. two suites were never alive at the
#     same time. The stub also RENDEZVOUSES (each waits, boundedly, for a peer stub to
#     start) precisely so that (c) is a statement about the lock and not about how fast
#     this host happened to schedule two processes: a run that wants to be concurrent has
#     tried, and the lock is what stops it.
#
#     THE ARM THAT MUST FAIL. The same pair is then run against a copy of the same tree
#     with the lock take REPLACED BY `:` — the pre-fix behaviour, byte for byte, in the
#     one line that matters. That leg must report OVERLAP. If it reports no overlap, this
#     check cannot go red and proves nothing, and it says so instead of passing.
if [ -z "${PREFLIGHT_WEBSERVER_LOCK_FILE:-}" ]; then
  bad "check 45 cannot run: PREFLIGHT_WEBSERVER_LOCK_FILE is unset, so the browser-suite lock this check is about does not exist in this shell"
elif [ "$PREFLIGHT_WEBSERVER_LOCK_FILE" = "${PREFLIGHT_WEBSERVER_LOCK_FILE_DEFAULT:-/var/lock/jomform-preflight-webserver.lock}" ]; then
  bad "check 45: the self-test is using the PRODUCTION browser-suite lock path ($PREFLIGHT_WEBSERVER_LOCK_FILE). That is ONE file for the whole host, so this self-test would queue behind — and hold the lock against — every live lane's a11y/e2e gate. It must be pinned to a private path like the pool above"
elif ! command -v flock >/dev/null 2>&1; then
  bad "check 45 cannot run: flock is missing, so the browser-suite lock cannot be taken (or measured). The web-server gates refuse in that state rather than guessing"
else
  ws45="$SB_STATE/ws45"
  rm -rf "$ws45"
  mkdir -p "$ws45/base/scripts" "$ws45/base/web/scripts" "$ws45/base/web/node_modules/.bin" "$ws45/base/bin"
  cp "$HERE"/*.sh "$ws45/base/scripts/" 2>/dev/null
  cp "$HERE/../web/package.json" "$ws45/base/web/" 2>/dev/null
  cp "$HERE/../web/scripts/web-deps-check.mjs" "$ws45/base/web/scripts/" 2>/dev/null
  cp "$HERE/../AGENTS.md" "$ws45/base/AGENTS.md" 2>/dev/null
  cp "$HERE/../GATE-INVENTORY.md" "$ws45/base/GATE-INVENTORY.md" 2>/dev/null
  chmod +x "$ws45/base/scripts/preflight.sh"
  # The two executables `preflight_env` refuses without; the dependency check itself runs
  # through the stub node below and is satisfied by its exit status.
  printf '#!/bin/sh\nexit 0\n' > "$ws45/base/web/node_modules/.bin/next"
  printf '#!/bin/sh\nexit 0\n' > "$ws45/base/web/node_modules/.bin/playwright"
  chmod +x "$ws45/base/web/node_modules/.bin/next" "$ws45/base/web/node_modules/.bin/playwright"
  # A stub node: answers the version floor (`node --version`) and lets the dependency
  # check through. Every other argv exits 0, which is what a satisfied prerequisite is.
  cat > "$ws45/base/bin/node" <<'WS45NODE'
#!/bin/sh
case "$1" in
  --version|-v) echo v24.19.0 ;;
esac
exit 0
WS45NODE
  # The stub suite. WS45_LOG is that RUN's own interval file; WS45_SHARED is the rendezvous
  # the two stubs use to establish that they really do want to run at the same time; and
  # WS45_RELEASE makes the HOLDER wait until the check has SEEN its peer announce the
  # wait. That last one is what makes the ordering a fact instead of a race: on a loaded
  # host the peer's start could otherwise land after the holder had already exited, and
  # the check would then be reading "no wait line" from a run that never had a peer.
  cat > "$ws45/base/bin/npm" <<'WS45NPM'
#!/usr/bin/env bash
set -uo pipefail
case "$*" in
  *"run e2e:core"*|*"run e2e"*) ;;
  *) echo "ws45 stub npm: unexpected invocation: $*" >&2; exit 9 ;;
esac
[ -n "${WS45_LOG:-}" ] || { echo "ws45 stub npm: WS45_LOG is unset" >&2; exit 9; }
printf 'START %s %s port=%s\n' "$(date +%s.%N)" "$$" "${A11Y_PORT:-none}" >>"$WS45_LOG"
if [ "${WS45_RENDEZVOUS:-0}" = 1 ] && [ -n "${WS45_SHARED:-}" ]; then
  [ -f "$WS45_SHARED" ] || : >"$WS45_SHARED"
  printf 'START %s\n' "$$" >>"$WS45_SHARED"
  _ws45_i=0
  while [ "$(grep -c '^START' "$WS45_SHARED")" -lt 2 ] && [ "$_ws45_i" -lt 80 ]; do
    sleep 0.05
    _ws45_i=$((_ws45_i+1))
  done
fi
if [ -n "${WS45_RELEASE:-}" ]; then
  _ws45_r=0
  while [ ! -f "$WS45_RELEASE" ] && [ "$_ws45_r" -lt 1200 ]; do
    sleep 0.05
    _ws45_r=$((_ws45_r+1))
  done
fi
sleep "${WS45_HOLD:-2}"
if [ "${WS45_LINGER:-0}" -gt 0 ]; then
  # A SUITE THAT LEAVES SOMETHING BEHIND, like a `next-server` outliving playwright. The
  # child is detached from this run's stdout (its own session, both streams to /dev/null)
  # so it cannot hold the gate's pipe: all it can possibly hold is an inherited descriptor.
  setsid sleep "$WS45_LINGER" </dev/null >/dev/null 2>&1 &
fi
printf 'END %s %s\n' "$(date +%s.%N)" "$$" >>"$WS45_LOG"
exit 0
WS45NPM
  chmod +x "$ws45/base/bin/node" "$ws45/base/bin/npm"
  git -C "$ws45/base" init -q .
  git -C "$ws45/base" add -A
  git -C "$ws45/base" -c user.email=t@t -c user.name=t commit -q -m init
  cp -a "$ws45/base" "$ws45/tree"
  cp -a "$ws45/base" "$ws45/sab"
  # THE SABOTAGE: the one line that takes the host-wide lock becomes `:`. Everything else
  # — pool, port claim, retry, gate body — is the shipped bytes. The line moved into
  # `browser_group_lock` when the browser gates became one concurrent group (card
  # t_ec2aac42); the sabotage follows it there, and the assertion below proves the
  # substitution landed exactly once rather than assuming it did.
  sed 's/^  webserver_lock_take "\$group_name" || return 1$/  :  # ws45 SABOTAGE: the host-wide suite lock take is removed here/' \
    "$ws45/tree/scripts/preflight.sh" > "$ws45/sab/scripts/preflight.sh"
  chmod +x "$ws45/sab/scripts/preflight.sh"
  ws45_sab_landed=0
  if [ "$(grep -c 'ws45 SABOTAGE' "$ws45/sab/scripts/preflight.sh")" -eq 1 ] &&
     [ "$(grep -c 'webserver_lock_take "\$group_name"' "$ws45/sab/scripts/preflight.sh")" -eq 0 ]; then
    ws45_sab_landed=1
  fi
  ws45_ts() {  # $1 = interval file, $2 = START|END -> the first such timestamp
    awk -v k="$2" '$1 == k { print $2; exit }' "$1"
  }
  ws45_overlap() {  # $1,$2 = the two interval files -> overlap | serial | incomplete
    awk -v a1="$(ws45_ts "$1" START)" -v a2="$(ws45_ts "$1" END)" \
        -v b1="$(ws45_ts "$2" START)" -v b2="$(ws45_ts "$2" END)" 'BEGIN {
      if (a1 == "" || a2 == "" || b1 == "" || b2 == "") { print "incomplete"; exit }
      if (a1 + 0 < b2 + 0 && b1 + 0 < a2 + 0) { print "overlap" } else { print "serial" }
    }'
  }
  ws45_pair() {  # $1 = label, $2 = the sandbox tree to run, $3 = release file ("" = none)
    local label="$1" dir="$2" release="${3:-}" i deadline
    : >"$ws45/$label.shared"
    [ -n "$release" ] && rm -f "$release"
    for i in 1 2; do
      : >"$ws45/$label-$i.npm"
      (
        cd "$dir" || exit 1
        env PATH="$dir/bin:$PATH" \
          PREFLIGHT_WEBSERVER_LOCK_FILE="$ws45/$label.websuite.lock" \
          PREFLIGHT_WEBSERVER_WAIT=120 \
          PREFLIGHT_LOCK_FILE="$ws45/$label.pool.lock" \
          PREFLIGHT_RESULT_DIR="$ws45/$label-records-$i" \
          PREFLIGHT_RESULT_TTL=1 \
          WS45_LOG="$ws45/$label-$i.npm" WS45_SHARED="$ws45/$label.shared" \
          WS45_RELEASE="$release" WS45_HOLD=2 WS45_LINGER="${WS45_LINGER:-0}" \
          WS45_RENDEZVOUS="${WS45_RENDEZVOUS:-0}" \
          ./scripts/preflight.sh --only e2e-core >"$ws45/$label-$i.log" 2>&1
        echo "EXIT=$?" >>"$ws45/$label-$i.log"
      ) &
    done
    if [ -n "$release" ]; then
      # The holder cannot finish until this file appears, so "the peer announced the
      # wait" is observed BEFORE the holder exits rather than hoped for. Bounded, and the
      # release happens either way, so a peer that never waits fails the assertion below
      # instead of hanging this check.
      deadline=$(( $(date +%s) + 90 ))
      while [ "$(date +%s)" -lt "$deadline" ]; do
        if grep -q 'another playwright suite is running on this host' "$ws45/$label-1.log" ||
           grep -q 'another playwright suite is running on this host' "$ws45/$label-2.log"; then
          break
        fi
        sleep 0.1
      done
      : >"$release"
    fi
    wait
  }
  ws45_pair tree "$ws45/tree" "$ws45/tree.release"
  ws45_e1="$(grep -o 'EXIT=[0-9]*' "$ws45/tree-1.log" | tail -n 1)"
  ws45_e2="$(grep -o 'EXIT=[0-9]*' "$ws45/tree-2.log" | tail -n 1)"
  ws45_v="$(ws45_overlap "$ws45/tree-1.npm" "$ws45/tree-2.npm")"
  ws45_waited=0
  grep -q 'another playwright suite is running on this host' "$ws45/tree-1.log" && ws45_waited=1
  grep -q 'another playwright suite is running on this host' "$ws45/tree-2.log" && ws45_waited=1
  if [ "$ws45_e1" != "EXIT=0" ] || [ "$ws45_e2" != "EXIT=0" ]; then
    bad "check 45 — a simultaneous e2e pair on the sandboxed runner did not both exit 0 ($ws45_e1 / $ws45_e2): the second suite must WAIT for the first, not refuse or fail. Output: [$(grep -hE 'PREFLIGHT FAIL|browser-suite|gate \[e2e-core\]' "$ws45/tree-1.log" "$ws45/tree-2.log" | tr '\n' ' ')]"
  elif [ "$ws45_v" = "incomplete" ]; then
    bad "check 45 could not measure: a stub suite never recorded both a START and an END ([$(grep -hE '^START|^END|stub npm' "$ws45/tree-1.npm" "$ws45/tree-2.npm" | tr '\n' ' ')]), so the intervals this check compares do not exist"
  elif [ "$ws45_v" = "overlap" ]; then
    bad "check 45 does not bite: the two sandboxed e2e runs were ALIVE AT THE SAME TIME ($ws45_v) — two playwright suites at once on one host is the defect (two runs, two ports, both proceeding) that red-ed the real gate on unrelated specs. The host-wide browser-suite lock is not serialising them"
  elif [ "$ws45_waited" -ne 1 ]; then
    bad "check 45 — the pair was serialised but NEITHER run printed the runner's own wait line, so the ordering cannot be attributed to the lock (it could be two runs that simply happened to be scheduled one after the other): [$ws45_v]"
  else
    ok "check 45 — two SIMULTANEOUS e2e runs on one host are SERIALISED: the holder could not finish until its peer had announced the wait, both exit 0, and the two stub intervals do not overlap"
  fi
  if [ "$ws45_sab_landed" -ne 1 ]; then
    bad "check 45's sabotage did not land: the lock-take line in a11y_gate_run no longer matches '  webserver_lock_take \"\$name\" || return 1' exactly once, so the arm that must FAIL was never built and the green above would be untested"
  else
    WS45_RENDEZVOUS=1 ws45_pair sab "$ws45/sab" ""
    ws45_sv="$(ws45_overlap "$ws45/sab-1.npm" "$ws45/sab-2.npm")"
    ws45_se1="$(grep -o 'EXIT=[0-9]*' "$ws45/sab-1.log" | tail -n 1)"
    ws45_se2="$(grep -o 'EXIT=[0-9]*' "$ws45/sab-2.log" | tail -n 1)"
    if [ "$ws45_sv" = "overlap" ]; then
      ok "check 45 BITES — with the suite-lock take removed from the same tree the SAME measurement reports OVERLAP ($ws45_se1 / $ws45_se2): the two suites ran at once, which is exactly what the lock above prevents"
    elif [ "$ws45_sv" = "incomplete" ]; then
      bad "check 45's sabotage leg could not measure: a stub suite recorded no interval at all ([$(grep -hE '^START|^END|stub npm' "$ws45/sab-1.npm" "$ws45/sab-2.npm" | tr '\n' ' ')]); the run output was [$ws45_se1 / $ws45_se2]"
    else
      bad "check 45 CANNOT GO RED: with the lock REMOVED ($ws45_se1 / $ws45_se2) the pair still reported $ws45_sv, so the property this check asserts is not what the lock does — a green above it would prove nothing"
    fi
  fi
  # ---- THE LOCK'S LIFETIME IS THE GATE'S, NOT ITS CHILDREN'S ------------------------
  # `flock` is held until the LAST descriptor on the open file description closes, so a
  # suite child that outlives the gate — a `next-server` playwright did not reap, a worker
  # that did not exit — keeps the HOST-WIDE lock after the gate has returned. The next gate
  # on this host would then wait PREFLIGHT_WEBSERVER_WAIT and FAIL with a wait-out: a false
  # red manufactured by this lock instead of one removed by it. Measured before the fix
  # (/tmp probe): a 4s child left behind after the holder exited still held the lock.
  # The runner therefore closes the descriptor in BOTH pipeline children; this arm measures
  # it with a stub suite that leaves a detached child, and repeats the measurement against a
  # copy with exactly those redirections removed, requiring the lock to still be HELD.
  sed 's/ {PREFLIGHT_WEBSERVER_LOCK_FD}>&-//g' "$ws45/tree/scripts/preflight.sh" > "$ws45/leaksab.scripts.preflight.sh"
  cp -a "$ws45/tree" "$ws45/leaksab"
  cp "$ws45/leaksab.scripts.preflight.sh" "$ws45/leaksab/scripts/preflight.sh"
  chmod +x "$ws45/leaksab/scripts/preflight.sh"
  ws45_lifetime_sab=0
  if [ "$(grep -c 'PREFLIGHT_WEBSERVER_LOCK_FD}>&-' "$ws45/tree/scripts/preflight.sh")" -ge 2 ] &&
     [ "$(grep -c 'PREFLIGHT_WEBSERVER_LOCK_FD}>&-' "$ws45/leaksab/scripts/preflight.sh")" -eq 0 ]; then
    ws45_lifetime_sab=1
  fi
  ws45_lock_free() { flock -n "$1" true 2>/dev/null; }   # 0 = nobody holds it
  WS45_LINGER=25 ws45_pair linger "$ws45/tree" ""
  ws45_lg1="$(grep -o 'EXIT=[0-9]*' "$ws45/linger-1.log" | tail -n 1)"
  ws45_lg2="$(grep -o 'EXIT=[0-9]*' "$ws45/linger-2.log" | tail -n 1)"
  if [ "$ws45_lg1" != "EXIT=0" ] || [ "$ws45_lg2" != "EXIT=0" ]; then
    bad "check 45's lifetime arm did not run cleanly ($ws45_lg1 / $ws45_lg2): a suite that leaves a child behind must still gate green, otherwise this arm measures the wrong thing ([$(grep -hE 'PREFLIGHT FAIL|browser-suite' "$ws45/linger-1.log" "$ws45/linger-2.log" | tr '\n' ' ')])"
  elif ws45_lock_free "$ws45/linger.websuite.lock"; then
    if [ "$ws45_lifetime_sab" -ne 1 ]; then
      bad "check 45's lifetime sabotage did not land: the runner no longer closes the suite-lock descriptor in its pipeline children (found $(grep -c 'PREFLIGHT_WEBSERVER_LOCK_FD}>&-' "$ws45/tree/scripts/preflight.sh") occurrences, need >= 2), so the arm that must FAIL was never built"
    else
      WS45_LINGER=25 ws45_pair lingersab "$ws45/leaksab" ""
      if ws45_lock_free "$ws45/lingersab.websuite.lock"; then
        bad "check 45 CANNOT GO RED on the lock's LIFETIME: with the child-close redirection REMOVED the lock was free the moment both runs exited, so the assertion above is not what keeps it free and a stranded suite child could still hold the host-wide lock"
      else
        ok "check 45 BITES (lifetime) — with the child-close removed, the same pair leaves a stranded child HOLDING $ws45/lingersab.websuite.lock after both gates exited; with the shipped bytes the lock was free immediately, so a suite that leaves a server behind cannot stall the next gate on this host"
      fi
    fi
  else
    bad "check 45 — the host-wide suite lock OUTLIVED the gate ($ws45/linger.websuite.lock was still held after both runs exited): a suite child inherited the lock descriptor, so the NEXT gate on this host would wait up to PREFLIGHT_WEBSERVER_WAIT and fail with a wait-out — a false red caused by this lock rather than removed by it"
  fi
fi
# ---------------------------------------------------------------------------
# 46. THE `speccounts` CHECKER CAN ACTUALLY GO RED, AND IT READS THE CURRENT
#     FIGURE, NOT A HISTORICAL ONE (card t_74e52a43).
#
# The checker (scripts/spec-counts.sh) is the only thing in the gate set that
# enforces the spec counts the `a11y` and `e2e` gates quote. A checker that always
# exits 0 would put those figures straight back where they were for nine cards:
# green everywhere, wrong in the file. So it is driven here with a STUB playwright —
# the sandbox has no node_modules and `selftest` is offline by contract, and none of
# these arms needs a browser, a server or a network. The stub prints what a real
# `--list` prints, which is the only thing the checker parses.
#
# THE FIGURE'S HOME (card t_d6ce3d60): each gate's detail file under
# `gate-inventory/<gate>.md`, not the table row. Every arm therefore writes BOTH the
# row (the checker still requires it) and the note, and arm E removes only the note to
# prove the new home is enforced rather than merely documented.
#
# ARMS, each measured (not reasoned about):
#   current   figures that match --list                    -> rc 0
#   stale     a11y figure 147/16 where --list says 159/17  -> rc 1, names BOTH
#   history   the STALE figure is first and a matching one
#             follows it in the note (the shape every real
#             note has: "**147 …**, the figure before it
#             was …")                                      -> rc 1, names 147
#             This is the arm that fails if the parse is greedy: a `s/.*\*\*//`
#             reads the LAST bold span, i.e. the history, and would pass a note
#             whose current figure is wrong — the exact rot this gate exists for.
#   no row    the e2e row is gone                          -> rc 1
#   no note   the e2e row is there, its detail file is gone -> rc 1
#   no figure the figure is present but not bolded          -> rc 1
#   --list 1  the enumeration exits non-zero                -> rc 1
#   no total  the enumeration prints no `Total:` line       -> rc 1
#
# Every arm runs the SANDBOX copy (`$SB/scripts/spec-counts.sh`), i.e. the bytes
# this commit would ship, against a throwaway root OUTSIDE the sandbox repo so the
# git state later checks depend on is untouched.
s45="$SB_STATE/s45"
rm -rf "$s45"
mkdir -p "$s45/web/node_modules/.bin"
S45_PROBLEMS=""
s45_stub() {  # <ok|fail|nototal> — the fake `playwright` the checker will exec
  case "$1" in
    ok)
      cat > "$s45/web/node_modules/.bin/playwright" <<'S45PW'
#!/bin/sh
case "$*" in
  *--project=a11y*)           echo "Total: 159 tests in 17 files" ;;
  *--project=feature-builder*) echo "Total: 78 tests in 17 files" ;;
  *--project=feature-core*)   echo "Total: 276 tests in 55 files" ;;
  *) exit 3 ;;
esac
S45PW
      ;;
    fail)
      printf '%s\n' '#!/bin/sh' 'echo "Error: Cannot find module @playwright/test" >&2' 'exit 1' \
        > "$s45/web/node_modules/.bin/playwright"
      ;;
    nototal)
      printf '%s\n' '#!/bin/sh' 'echo "Listing 4 tests in 2 files"' \
        > "$s45/web/node_modules/.bin/playwright"
      ;;
  esac
  chmod 755 "$s45/web/node_modules/.bin/playwright"
}
s45_arm() {  # <label> <want-rc> <want-substring> [<stub-mode>]; inventory pre-written
  local rc out mode
  mode="${4:-ok}"
  s45_stub "$mode"
  out="$(bash "$SB/scripts/spec-counts.sh" "$s45" 2>&1)"
  rc=$?
  if [ "$rc" = "$2" ] && grep -qF -- "$3" <<<"$out"; then
    return 0
  fi
  S45_PROBLEMS="${S45_PROBLEMS}${S45_PROBLEMS:+; }$1 [rc=$rc, wanted rc=$2 naming \"$3\"; got: $(printf '%s' "$out" | tail -n 2 | tr '\n' ' ')]"
  return 1
}
s45_row() { printf '| `%s` | web | `x` | see [`gate-inventory/%s.md`](gate-inventory/%s.md) |\n' "$1" "$1" "$1"; }
# The FIGURE lives in the gate's detail file (card t_d6ce3d60), not in the row: the
# rows are adjacent one-liners and a count edit in one conflicted with a count edit in
# its neighbour on every merge. So each arm writes the table row AND the note.
s45_note() {
  mkdir -p "$s45/gate-inventory"
  printf '# `%s` — web gate detail\n\nCommand (what the table row runs): `x`\n\n## What it catches\n\n%s\n' \
    "$1" "$2" > "$s45/gate-inventory/$1.md"
}
s45_both() { s45_row a11y; s45_note a11y "$1"; s45_row e2e-builder; s45_note e2e-builder "$2"; s45_row e2e-core; s45_note e2e-core "$3"; }
# A. current figures -> PASS (a red-only checker would pass every arm below and
#    still be useless: this one has to be able to say yes).
{ s45_both '**159 tests in 17 files**' '**78 tests in 17 files**' '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm current 0 'every spec count the gate detail files quote is what this tree runs'
# B. a stale a11y figure -> RED, naming both numbers.
{ s45_both '**147 tests in 16 files**' '**78 tests in 17 files**' '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm stale 1 '`a11y`: the detail file says **147 tests in 16 files**; --list reads **159 tests in 17 files**'
# C. the SAME stale figure followed by a matching historical one -> still RED.
{ s45_both '**147 tests in 16 files**, re-measured by a card that forgot; the figure before it was **159 tests in 17 files**.' '**78 tests in 17 files**' '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm history 1 '`a11y`: the detail file says **147 tests in 16 files**; --list reads **159 tests in 17 files**'
# D. one feature half's ROW is gone -> RED (a guard that reads nothing must not pass).
{ s45_row a11y; s45_note a11y '**159 tests in 17 files**'
  s45_row e2e-builder; s45_note e2e-builder '**78 tests in 17 files**'
  s45_note e2e-core '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm norow 1 'GATE-INVENTORY.md has no row for the `e2e-core` gate'
# E. one feature half's DETAIL FILE is gone (the row is there) -> RED naming the file.
#    This is the arm that pins the figure's new home: without it, "the row no longer
#    carries the number" could degrade into "nobody carries it" and stay green.
{ s45_row a11y; s45_note a11y '**159 tests in 17 files**'
  s45_row e2e-builder; s45_note e2e-builder '**78 tests in 17 files**'
  s45_row e2e-core; rm -f "$s45/gate-inventory/e2e-core.md"; } > "$s45/GATE-INVENTORY.md"
s45_arm nonote 1 'gate-inventory/e2e-core.md is missing'
# F. the figure is there but not in the parseable form -> RED, never a skip.
{ s45_both '159 tests in 17 files' '**78 tests in 17 files**' '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm nofigure 1 'carries no `**N tests in M files**` figure'
# G. the enumeration itself fails -> RED, and it says the count was not read.
{ s45_both '**159 tests in 17 files**' '**78 tests in 17 files**' '**276 tests in 55 files**'; } > "$s45/GATE-INVENTORY.md"
s45_arm listfail 1 'exited 1 on this tree' fail
# H. the enumeration prints no Total line -> RED (a parse that finds nothing
#    must not be mistaken for a match).
s45_arm nototal 1 "carries no 'Total: N tests in M files' line" nototal
if [ -n "$S45_PROBLEMS" ]; then
  bad "check 46 — the speccounts checker must pass on a current figure and RED on each way of being unreadable; it did not: $S45_PROBLEMS"
else
  ok "check 46 — the speccounts checker passes on figures that match --list and REDS naming both numbers on a stale FIRST figure (including when a matching historical figure follows it, which a greedy parse would read), on a missing row, on a missing DETAIL FILE, on an un-bolded figure, on a --list that exits non-zero and on one with no Total line (8 arms, stub playwright, offline)"
fi
rm -rf "$s45"

# 47. A GATE RECORD MAY CARRY ONLY WHAT WAS OBSERVED UNDER ITS OWN KEY, AND ONLY
#     A FINISHED RUN IS A VERDICT (card t_d49a99f1).
#
#     THE MEASURED DEFECT. Gating the merge of PR #1477, the record for tree
#     45432d4e was read while a run was in progress:
#       * the previous record (another lane, key 25042aea) held 23/23 `pass`;
#       * a new run started under a DIFFERENT key (f2be5fdd — `web/node_modules`
#         re-installed, so the webdeps hash moved);
#       * after five gates, the file read `key=f2be5fdd, verdict=green, passed=5`,
#         with EIGHTEEN gates still carrying `at` from the other lane's run and
#         never run under f2be5fdd at all;
#       * the carried entries printed `reused: false`, the one label that would
#         have said they were remembered rather than run.
#     Two independent cause, both in gr_flush:
#       (i) the carry-over loop never looked at the existing record's `key`, so a
#           key change carried the PREVIOUS environment's passes into the new one —
#           the exact reuse hole `gr_record_usable` exists to close, opened by the
#           writer one function away from the reader;
#       (ii) it wrote carried values into GR_STATUS (the map that means "this run
#           observed it"), and because the file is rewritten after EVERY gate, the
#           next flush took the OBSERVED branch and relabelled them `reused: false`
#           while keeping the older `at`.
#     And `finished_at` was rewritten on every flush, so a run killed mid-flight
#     left a record indistinguishable from a finished one.
#
#     WHAT THIS CHECK ASSERTS, each with the control that stops it being satisfiable
#     by a blunt "never carry anything" (which would break the documented
#     `--only <PREFLIGHT_RERUN> --reuse-record` lane):
#       (a) the fixture: a FINISHED full-inventory run under key A is green,
#           `complete: true`, and IS reusable — if this leg fails the rest proves
#           nothing about key B;
#       (c) SAME KEY: two flushes of a partial run under key A carry the other
#           gates with `reused: true` and their ORIGINAL `at`, while the gates this
#           run observed read `reused: false` with a fresh `at` (pre-fix, the second
#           flush flips the carried ones to `reused: false`);
#       (b) KEY CHANGE: a mid-run flush under key B records exactly ONE pass — the
#           gate that run observed — and carries nothing, its verdict is not
#           `green`, and the consumer refuses it (pre-fix: 23 passes, green);
#       (d) COMPLETE: that mid-run record is refused naming INCOMPLETE, and the same
#           file becomes reusable once a run under key B finishes its inventory.
#
#     THE SANDBOX STORE IS USED, so this check writes no host state (see RECORD_DIR),
#     and the REAL writer is driven (never a re-implementation), in a subshell so no
#     GR_* map leaks into the checks around it.
r47_dir="$SB_STATE/record47"
mkdir -p "$r47_dir"
r47_rec="$r47_dir/$probe_tree.json"
r47_all="$GATES"
r47_one="$(printf '%s\n' "$GATES" | sed -n '1p')"
r47_two="$(printf '%s\n' "$GATES" | sed -n '1,2p')"
r47_third="$(printf '%s\n' "$GATES" | sed -n '3p')"
r47_untouched="$(printf '%s\n' "$GATES" | sed -n '3,$p')"
# r47_run <key-extra> <finish 0|1> <observed gates, newline separated>
# Drives the REAL gr_flush once per observed gate (exactly like run() in
# preflight.sh does), then — when <finish> is 1 — the final flush of a run that
# reached the end of its inventory, which is the only one that says complete: true.
r47_run() {
  (
    export PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA="$1"
    # shellcheck source=scripts/gate_result.sh
    . "$SB/scripts/gate_result.sh"
    GR_REPO="$SB"
    GR_TREE="$probe_tree"
    GR_GATE_NAMES=()
    while IFS= read -r r47g; do
      if [ -n "$r47g" ]; then GR_GATE_NAMES+=("$r47g"); fi
    done <<<"$GATES"
    r47n=0
    while IFS= read -r r47g; do
      if [ -z "$r47g" ]; then continue; fi
      r47n=$((r47n+1))
      GR_STATUS["$r47g"]=pass; GR_SECONDS["$r47g"]=42; GR_AT["$r47g"]="$(gr_now)"
      GR_PASSED=$r47n; GR_FAILED=0; GR_SKIPPED=0
      gr_flush 1 || exit 1
    done <<<"$3"
    if [ "$2" = 1 ]; then
      GR_COMPLETE=1
      gr_flush 1 || exit 1
    fi
  )
}
r47_status() {  # <gate> — the record's status for <gate>, read the way a lane does
  gr_gate_status "$r47_rec" "$1"
}
r47_reused() { gr_gate_entry_field "$r47_rec" "$1" reused; }
r47_at()     { gr_gate_entry_field "$r47_rec" "$1" at; }
r47_count_pass() {
  local c=0 g
  # shellcheck disable=SC2086
  for g in $GATES; do [ "$(r47_status "$g")" = "pass" ] && c=$((c+1)); done
  printf '%s' "$c"
}

r47_a_rc=0; r47_a_out="$(r47_run runA 1 "$r47_all" 2>&1)" || r47_a_rc=$?
r47_a_pass="$(r47_count_pass)"
r47_a_complete="$(gr_read_bool "$r47_rec" complete)"
r47_a_reuse_rc=0; r47_a_reuse="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runA gr_reusable "$probe_tree" $GATES 2>&1)" || r47_a_reuse_rc=$?
if [ "${r47_a_rc:-1}" != 0 ] || [ "$r47_a_pass" != "$NGATES" ] || [ "$r47_a_complete" != true ] || [ "${r47_a_reuse_rc:-1}" != 0 ]; then
  bad "check 47 cannot run: the key-A fixture (a FINISHED full-inventory run) did not land as a reusable green record — flush rc=$r47_a_rc, pass=$r47_a_pass/$NGATES, complete=[$r47_a_complete], reusable rc=$r47_a_reuse_rc [$(printf '%s' "$r47_a_reuse" | tail -n 1)] [$(printf '%s' "$r47_a_out" | tail -n 1)]. A key-B leg below would then prove nothing about carrying."
else
  # ---- (c) SAME KEY: a carry is labelled, and keeps the AT of the run that observed it.
  declare -A r47_at_before=()
  # shellcheck disable=SC2086
  for r47g in $r47_untouched; do r47_at_before[$r47g]="$(r47_at "$r47g")"; done
  r47_at_third_before="${r47_at_before[$r47_third]}"
  r47_c_rc=0; r47_c_out="$(r47_run runA 0 "$r47_two" 2>&1)" || r47_c_rc=$?
  r47_c_bad=""
  r47_c_reused_ok=0; r47_c_reused_bad=0; r47_c_fresh_ok=0
  # shellcheck disable=SC2086
  for r47g in $r47_untouched; do
    if [ "$(r47_status "$r47g")" = "pass" ] && [ "$(r47_reused "$r47g")" = "true" ] && [ "$(r47_at "$r47g")" = "${r47_at_before[$r47g]}" ]; then
      r47_c_reused_ok=$((r47_c_reused_ok+1))
    else
      r47_c_reused_bad=$((r47_c_reused_bad+1))
      r47_c_bad="$r47_c_bad $r47g(status=$(r47_status "$r47g") reused=$(r47_reused "$r47g") at=$(r47_at "$r47g") was=${r47_at_before[$r47g]})"
    fi
  done
  r47_c_observed_ok=0
  r47_ntouched="$(printf '%s\n' "$r47_untouched" | grep -c '[^[:space:]]')"
  # shellcheck disable=SC2086
  for r47g in $r47_two; do
    if [ "$(r47_reused "$r47g")" = "false" ]; then r47_c_observed_ok=$((r47_c_observed_ok+1)); fi
  done

  # ---- (b) KEY CHANGE: nothing observed under key A may be written under key B.
  r47_keyA="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runA gr_key "$SB")"
  r47_keyB="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runB gr_key "$SB")"
  r47_b_rc=0; r47_b_out="$(r47_run runB 0 "$r47_one" 2>&1)" || r47_b_rc=$?
  r47_b_key="$(gr_read_field "$r47_rec" key)"
  r47_b_pass="$(r47_count_pass)"
  r47_b_verdict="$(gr_read_field "$r47_rec" verdict)"
  r47_b_complete="$(gr_read_bool "$r47_rec" complete)"
  r47_b_obs="$(r47_status "$r47_one")"
  r47_b_reuse_rc=0; r47_b_reuse="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runB gr_reusable "$probe_tree" $GATES 2>&1)" || r47_b_reuse_rc=$?

  # ---- (d) COMPLETE: a run that did not finish is not a verdict, and finishing fixes it.
  r47_d_rc=0; r47_d_msg="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runB gr_record_usable "$probe_tree" 2>&1)" || r47_d_rc=$?
  r47_e_rc=0; r47_e_out="$(r47_run runB 1 "$r47_all" 2>&1)" || r47_e_rc=$?
  r47_e_reuse_rc=0; r47_e_reuse="$(PREFLIGHT_RESULT_DIR="$r47_dir" PREFLIGHT_KEY_EXTRA=runB gr_reusable "$probe_tree" $GATES 2>&1)" || r47_e_reuse_rc=$?

  if [ "${r47_c_rc:-1}" != 0 ]; then
    bad "check 47 (c) — the same-key partial run could not be flushed (rc=$r47_c_rc) [$(printf '%s' "$r47_c_out" | tail -n 1)], so the reuse labelling was never exercised"
  elif [ "$r47_c_reused_ok" != "$r47_ntouched" ] || [ "$r47_c_observed_ok" != 2 ] || [ -z "$r47_at_third_before" ]; then
    bad "check 47 (c) — a gate carried from an earlier run under the SAME key must stay labelled 'reused: true' with the 'at' of the run that OBSERVED it, and a gate this run did run must read 'reused: false': $r47_c_reused_ok/$r47_ntouched carried entries are labelled (third gate's recorded at=[$r47_at_third_before]), $r47_c_observed_ok/2 observed entries read 'reused: false'. Mislabelled:$r47_c_bad (the pre-fix writer relabelled carried entries 'reused: false' one flush later, because it wrote them into the observed-run map)"
  elif [ "${r47_b_rc:-1}" != 0 ]; then
    bad "check 47 (b) — the key-B mid-run flush failed (rc=$r47_b_rc) [$(printf '%s' "$r47_b_out" | tail -n 1)]"
  elif [ -z "$r47_keyA" ] || [ "$r47_keyA" = "$r47_keyB" ]; then
    bad "check 47 cannot discriminate a key change: key A=[$r47_keyA] key B=[$r47_keyB] — PREFLIGHT_KEY_EXTRA no longer separates two fingerprints, so this check would prove nothing either way"
  elif [ "$r47_b_key" != "$r47_keyB" ]; then
    bad "check 47 (b) — after the key-B run the record's key is [$r47_b_key], not key B [$r47_keyB]: the file never moved to the new key, so the carry leg was never exercised"
  elif [ "$r47_b_obs" != "pass" ]; then
    bad "check 47 (b) — the ONE gate the key-B run really observed ($r47_one) must be recorded 'pass', got [$r47_b_obs]: a record that drops the run's own observation is not conservative, it is broken"
  elif [ "$r47_b_pass" != 1 ]; then
    bad "check 47 (b) — the key-B run observed ONE gate, but its record holds $r47_b_pass pass entries under key $(printf '%.12s' "$r47_b_key"): $((r47_b_pass-1)) gate(s) were carried from the key-A record, i.e. another environment's passes were written under THIS key — exactly what gr_record_usable promises a reader never happens"
  elif [ "$r47_b_verdict" = "green" ]; then
    bad "check 47 (b) — the key-B record's verdict is [$r47_b_verdict] with $r47_b_pass of $NGATES gates observed under key B (this is the measured defect: 'verdict: green, passed: 5' over a key its unrun gates were never gated under)"
  elif [ "${r47_b_reuse_rc:-0}" = 0 ]; then
    bad "check 47 (b) — a record holding ONE observed gate under the current key must not be reusable for the whole inventory; gr_reusable said [$(printf '%s' "$r47_b_reuse" | tail -n 1)]"
  elif [ "${r47_d_rc:-0}" = 0 ]; then
    bad "check 47 (d) — a record written by a run that did not finish (complete=[$r47_b_complete]) must be REFUSED by gr_record_usable, but it accepted it: a killed run's file would be handed to a lane as a verdict"
  elif ! grep -qF 'INCOMPLETE' <<<"$r47_d_msg"; then
    bad "check 47 (d) — the refusal of an unfinished record must NAME what it refused (complete), got [$(printf '%s' "$r47_d_msg" | tail -n 1)]"
  elif [ "$r47_b_complete" = "true" ]; then
    bad "check 47 (d) — the mid-run record says complete=[$r47_b_complete]; a run that has observed $r47_b_pass of $NGATES gates has NOT finished, and finished_at is rewritten on every flush so it cannot be the tell"
  elif [ "${r47_e_rc:-1}" != 0 ] || [ "${r47_e_reuse_rc:-1}" != 0 ]; then
    bad "check 47 (d) — once a run under key B finishes its whole inventory the record must become reusable, but flush rc=$r47_e_rc and reusable rc=$r47_e_reuse_rc [$(printf '%s' "$r47_e_reuse" | tail -n 1)] [$(printf '%s' "$r47_e_out" | tail -n 1)]"
  else
    ok "check 47 — a gate record carries only what was observed under ITS OWN key, labels every carried entry 'reused: true' on every flush, and is a verdict only when the run finished — measured with the REAL writer: key A finished full run = $r47_a_pass/$NGATES green, complete, reusable; a same-key partial run keeps $r47_c_reused_ok/$r47_ntouched carried entries labelled (original 'at') and both observed gates fresh; a mid-run flush under key B records exactly $r47_b_pass pass (the gate it ran), verdict [$r47_b_verdict], complete [$r47_b_complete], and the consumer refuses it — as INCOMPLETE before the run finishes and reusable only after it does"
  fi
fi
rm -rf "$r47_dir"

# 48. `runs` COUNTS GATE RUNS, NOT FLUSHES (card t_68fa3c5c).
#
#     THE MEASURED DEFECT. The record is rewritten after EVERY gate — that is how it
#     stays current — and the writer incremented `runs` on every one of those writes.
#     One COMPLETE 23-gate run therefore left `runs: 24` in the record, a following
#     two-gate run on the same tree took it to 27, the host record for tree 45432d4e
#     read 46 for two runs, and the single green run for tree 4c9a5d01 read 24.
#     `runs` is the field a merger reads to ask "how many gate runs is this verdict
#     standing on", and it was answering "how many times the file was written" —
#     so one fresh run looked like 24 runs and a record reused by five lanes looked
#     like 25.
#
#     WHY NO EXISTING CHECK SAW IT: each fixture in this file either drives gr_flush
#     ONCE per run or reads only the per-gate entries, so the count was never the
#     subject. The property needs TWO runs AND an observation after EVERY flush of
#     one run:
#       (a) ONE run that flushes after each of three gates, then finishes, must read
#           `runs: 1` at EVERY write — an intermediate flush must not add one;
#       (b) a SECOND run on the same tree and the same key must read 2 at every
#           write: a whole run adds exactly one, whatever it flushed;
#       (c) a THIRD run under a DIFFERENT key must read 1 again — a key change
#           carries no gate entry, so the record's verdict stands on that run alone
#           and a count that kept climbing would claim support the record does not
#           have.
#     (b) and (c) are the control on "just stop incrementing": a writer that never
#     increments would satisfy (a) alone and report 1 forever.
#
#     Each r48_run is a SUBSHELL, i.e. its own process, i.e. one gate run — exactly
#     the boundary the count is defined on. The REAL writer is driven (never a
#     re-implementation) and the sandbox store is used, so this check writes no host
#     state.
r48_dir="$SB_STATE/record48"
mkdir -p "$r48_dir"
r48_rec="$r48_dir/$probe_tree.json"
r48_g1="$(printf '%s\n' "$GATES" | sed -n '1p')"
r48_g2="$(printf '%s\n' "$GATES" | sed -n '2p')"
r48_g3="$(printf '%s\n' "$GATES" | sed -n '3p')"
# r48_run <key-extra> <gate>... — one process: flush after each named gate (like
# run() in preflight.sh), then the final flush of a run that reached the end of its
# inventory. Prints `runs` as the file carries it after EVERY write, space separated,
# so the sequence itself shows whether an individual flush added one.
r48_run() {
  (
    export PREFLIGHT_RESULT_DIR="$r48_dir" PREFLIGHT_KEY_EXTRA="$1"
    # shellcheck source=scripts/gate_result.sh
    . "$SB/scripts/gate_result.sh"
    GR_REPO="$SB"
    GR_TREE="$probe_tree"
    GR_GATE_NAMES=("$r48_g1" "$r48_g2" "$r48_g3")
    shift
    r48n=0
    for r48g in "$@"; do
      r48n=$((r48n+1))
      GR_STATUS["$r48g"]=pass; GR_SECONDS["$r48g"]=5; GR_AT["$r48g"]="$(gr_now)"
      GR_PASSED=$r48n; GR_FAILED=0; GR_SKIPPED=0
      gr_flush 1 || exit 1
      printf '%s ' "$(gr_read_int "$r48_rec" runs)"
    done
    GR_COMPLETE=1
    gr_flush 1 || exit 1
    printf '%s' "$(gr_read_int "$r48_rec" runs)"
  )
}
r48_a_rc=0; r48_a="$(r48_run runA "$r48_g1" "$r48_g2" "$r48_g3" 2>"$SB_STATE/r48-a.err")" || r48_a_rc=$?
r48_b_rc=0; r48_b="$(r48_run runA "$r48_g1" "$r48_g2" 2>"$SB_STATE/r48-b.err")" || r48_b_rc=$?
# Run C changes the key, so the writer prints its one-per-process key-change note on
# stderr. It is captured to a FILE, never merged into stdout: the sequences compared
# below are the `runs` values the record holds, and a diagnostic line inside them
# would fail this check for a reason that has nothing to do with the count.
r48_c_rc=0; r48_c="$(r48_run runB "$r48_g1" 2>"$SB_STATE/r48-c.err")" || r48_c_rc=$?

if [ "${r48_a_rc:-1}" != 0 ] || [ "${r48_b_rc:-1}" != 0 ] || [ "${r48_c_rc:-1}" != 0 ]; then
  bad "check 48 cannot run: a fixture flush failed (rc=$r48_a_rc/$r48_b_rc/$r48_c_rc) [$(tail -n 1 "$SB_STATE/r48-a.err" 2>/dev/null) | $(tail -n 1 "$SB_STATE/r48-c.err" 2>/dev/null)], so the \`runs\` sequences below would be about a record this check never wrote"
elif [ "$r48_a" != "1 1 1 1" ]; then
  bad "check 48 (a) — ONE gate run that flushed after each of three gates and then finished must read 'runs: 1' after every one of its FOUR writes, got [$r48_a]: an intermediate flush added to the count, so a single fresh 23-gate run reports 24 runs and the field cannot answer how many runs a verdict stands on"
elif [ "$r48_b" != "2 2 2" ]; then
  bad "check 48 (b) — a SECOND run on the same tree and the same key must read 'runs: 2' after every write, got [$r48_b]: either a whole run did not add exactly one, or its intermediate flushes added more (the measured defect took a second two-gate run from 24 to 27)"
elif [ "$r48_c" != "1 1" ]; then
  bad "check 48 (c) — a THIRD run under a DIFFERENT key must read 'runs: 1' after every write, got [$r48_c]: a key change carries no gate entry, so the record's verdict stands on this run alone and the count must restart rather than claim the earlier runs' support"
else
  ok "check 48 — \`runs\` counts gate RUNS, not flushes: one run writing four times reads [$r48_a], a second run on the same tree/key reads [$r48_b], and a run under a changed key restarts at [$r48_c]"
fi
rm -rf "$r48_dir"

  # ---------------------------------------------------------------- 49
  # The harness cache. `monitor` re-runs the watchdog's suite, whose inputs are the
  # files under deploy/monitoring; paying it on every card re-learns an answer that
  # cannot have changed (measured 2% of a real 4,819s run, every card).
  # `selftest` ALSO re-runs the gate-runner's own suite — and that suite READS THE
  # PRODUCT TREE: check 15d/15e derive gates' claimed paths by grepping quoted
  # repo-root-relative literals out of api/ web/ scripts/ deploy/, check 39 reads
  # web/e2e specs and web/playwright.config.ts, check 12c sweeps every tracked file
  # and 15o walks the files of every pw_sources glob. The runner itself classifies it
  # `selftest: *` ("reads the whole checkout, cannot narrow") — measured 2026-11-11:
  # a key over the scripts alone told a tree whose web/ carried a bare `"docs/"`
  # literal (which REDS 15d) that `selftest` was PASS, in 2s, from another tree's
  # record. So (a)-(f) pin the glob-keyed half, (g) pins the `:tree:` half — a MISS
  # and a re-run when only web/ changes, with a mutated copy proving that comparison
  # can go RED — and (h) pins the DECLARATION against the runner's own classifier.
  r49_dir="$(mktemp -d "${TMPDIR:-/tmp}/jf-hcache.XXXXXX")"
  mkdir -p "$r49_dir/repo/scripts" "$r49_dir/results"
  printf '#!/bin/sh\nexit 0\n' > "$r49_dir/repo/scripts/a.sh"
  sed -n '/^hcached() {/,/^}$/p' "$PREFLIGHT" > "$r49_dir/hcached.sh"
  if ! grep -q '^hcached() {' "$r49_dir/hcached.sh"; then
    bad "check 49 cannot run: hcached() is not extractable from preflight.sh, so the harness cache is untested"
  else
    r49="$(REPO_ROOT="$r49_dir/repo" PREFLIGHT_RESULT_DIR="$r49_dir/results" bash -c '
      . "$1/hcached.sh"
      d="$1"; c="$d/count"; : > "$c"
      _bump() { echo x >> "$c"; }
      hcached probe "scripts/*.sh" -- _bump > "$d/o1" 2>&1
      n1=$(wc -l < "$c")
      hcached probe "scripts/*.sh" -- _bump > "$d/o2" 2>&1
      n2=$(wc -l < "$c")
      mark=""
      for _hf in "$d"/results/harness/.hit.probe.*; do
        if [ -r "$_hf" ]; then mark=$(cat "$_hf"); break; fi
      done
      printf "\n# one more byte\n" >> "$d/repo/scripts/a.sh"
      hcached probe "scripts/*.sh" -- _bump > "$d/o3" 2>&1
      n3=$(wc -l < "$c")
      PREFLIGHT_HARNESS_CACHE=0 hcached probe "scripts/*.sh" -- _bump > "$d/o4" 2>&1
      n4=$(wc -l < "$c")
      # THE CONTROL FOR THE BUG THIS CACHE SHIPPED WITH: the same bytes in a
      # DIFFERENT absolute path must hit. The first version fed `sha256sum "$path"`
      # into the key, so every worktree looked different and nine real runs after
      # the merge paid 574-814s each for a suite that could not have changed.
      cp -a "$d/repo" "$d/other-root"
      REPO_ROOT="$d/other-root"
      hcached probe "scripts/*.sh" -- _bump > "$d/o5" 2>&1
      n5=$(wc -l < "$c")
      hit5=no; grep -q "harness cache HIT" "$d/o5" && hit5=yes
      hit2=no; grep -q "harness cache HIT" "$d/o2" && hit2=yes
      hit3=no; grep -q "harness cache HIT" "$d/o3" && hit3=yes
      markok=no; case "$mark" in [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T*Z) markok=yes;; esac
      printf "%s:%s:%s:%s:%s|%s|%s|%s|%s\n" "$n1" "$n2" "$n3" "$n4" "$n5" "$hit2" "$hit3" "$markok" "$hit5" | tail -1
    ' _ "$r49_dir")"
    r49_n="${r49%%|*}"; r49_rest="${r49#*|}"
    r49_hit2="${r49_rest%%|*}"; r49_rest="${r49_rest#*|}"
    r49_hit3="${r49_rest%%|*}"; r49_rest="${r49_rest#*|}"
    r49_mark="${r49_rest%%|*}"; r49_hit5="${r49_rest#*|}"
    r49_c1="${r49_n%%:*}"; r49_t="${r49_n#*:}"; r49_c2="${r49_t%%:*}"; r49_t="${r49_t#*:}"
    r49_c3="${r49_t%%:*}"; r49_t="${r49_t#*:}"; r49_c4="${r49_t%%:*}"; r49_c5="${r49_t#*:}"
    if [ "$r49_c1" != "1" ] || [ "$r49_c2" != "1" ]; then
      bad "check 49 (a) — an unchanged harness must run ONCE and then hit the cache: it ran $r49_c1 then $r49_c2 times (a hit must not re-run the suite, and a miss must not be silently skipped)"
    elif [ "$r49_hit2" != "yes" ]; then
      bad "check 49 (b) — the second call served a hit but did not SAY so: a cached gate that cannot be told from a fresh one is a gate nobody can audit"
    elif [ "$r49_mark" != "yes" ]; then
      bad "check 49 (c) — a hit must leave the OBSERVING run's time where run() can read it (an ISO-8601 marker file), or the record dates a remembered observation to the run that did not execute it; got [$r49_mark]"
    elif [ "$r49_c3" != "2" ] || [ "$r49_hit3" != "no" ]; then
      bad "check 49 (d) — one changed byte in a file under test must invalidate the cache and re-run (it ran $r49_c3 times, hit=$r49_hit3); an invalidation that does not fire serves a stale pass for changed bytes"
    elif [ "$r49_c4" != "3" ]; then
      bad "check 49 (e) — PREFLIGHT_HARNESS_CACHE=0 must run the gate as before, got $r49_c4 runs (expected 3)"
    elif [ "$r49_hit5" != "yes" ] || [ "$r49_c5" != "3" ]; then
      bad "check 49 (f) — the same bytes under a DIFFERENT absolute path must hit (hit=$r49_hit5, runs=$r49_c5, expected run count to stay 3): a key that carries the worktree path makes every card a miss, which is the defect nine real runs measured after the first merge"
    else
      ok "check 49 — harness cache: runs once ($r49_c1) then hits without re-running ($r49_c2), says HIT, leaves the observation time, re-runs when a byte under test changes (run $r49_c3), obeys the kill switch (run $r49_c4), and hits for the same bytes in another absolute path (run $r49_c5)"
    fi
  fi

  # 49(g). THE TREE INPUT. `selftest` reads the product tree (15d/15e derive claims
  # from quoted literals under api/ web/ scripts/ deploy/, 39 reads web/e2e and the
  # playwright config, 12c sweeps every tracked file), so a key over the scripts alone
  # can vouch for a tree it never observed. THE BITE IS THE SECOND HALF: the same
  # fixture is driven through a COPY whose key material drops `tree=` — byte-for-byte
  # the shipped defect — and that copy MUST serve the stale HIT (runs stay put after
  # only web/ changed). If it stops doing so, this comparison is no longer about
  # anything and is RED rather than quietly vacuous.
  r49t="$r49_dir/tree"
  mkdir -p "$r49t/base/scripts" "$r49t/base/web"
  printf '#!/bin/sh\nexit 0\n' > "$r49t/base/scripts/a.sh"
  printf 'export const area = "web/src";\n' > "$r49t/base/web/x.ts"
  git -C "$r49t/base" init -q 2>/dev/null
  git -C "$r49t/base" add -A 2>/dev/null
  sed "/printf 'tree=%s/d" "$r49_dir/hcached.sh" > "$r49_dir/hcached.notree.sh"
  r49t_ship=$(grep -cF "printf 'tree=%s" "$r49_dir/hcached.sh" 2>/dev/null || true)
  r49t_mut=$(grep -cF "printf 'tree=%s" "$r49_dir/hcached.notree.sh" 2>/dev/null || true)

  # r49_tree <hcached-file> <variant> -> "<n1>:<n2>:<n3>:<n4>:<n5>|<hit2>|<hit3>|<hit4>|<hit5>"
  # Each variant gets its OWN copy of the fixture and its own scratch dir, so the two
  # runs cannot see each other's record (nor leave an untracked file inside the tree,
  # which would trip the dirty guard and turn both runs into misses).
  r49_tree() {
    cp -a "$r49t/base" "$r49t/run-$2" 2>/dev/null
    mkdir -p "$r49t/scratch-$2" 2>/dev/null
    REPO_ROOT="$r49t/run-$2" PREFLIGHT_RESULT_DIR="$r49t/scratch-$2/results" bash -c '
      . "$1"
      d="$2"; s="$3"; c="$s/count"; : > "$c"
      _bump() { echo x >> "$c"; }
      hcached probe :tree: -- _bump >/dev/null 2>&1
      n1=$(wc -l < "$c")
      hcached probe :tree: -- _bump > "$s/t2" 2>&1
      n2=$(wc -l < "$c")
      # The ONLY change is under web/: the scripts stay byte-identical.
      printf "export const claim = \"docs/\";\n" >> "$d/web/x.ts"
      git -C "$d" add -A
      hcached probe :tree: -- _bump > "$s/t3" 2>&1
      n3=$(wc -l < "$c")
      # A working-tree edit that was never staged: the key cannot describe it.
      printf "\n// a working-tree edit, never staged\n" >> "$d/web/x.ts"
      hcached probe :tree: -- _bump > "$s/t4" 2>&1
      n4=$(wc -l < "$c")
      git -C "$d" checkout -- web/x.ts
      cp -a "$d" "$4"
      REPO_ROOT="$4"
      hcached probe :tree: -- _bump > "$s/t5" 2>&1
      n5=$(wc -l < "$c")
      hit2=no; grep -q "harness cache HIT" "$s/t2" && hit2=yes
      hit3=no; grep -q "harness cache HIT" "$s/t3" && hit3=yes
      hit4=no; grep -q "harness cache HIT" "$s/t4" && hit4=yes
      hit5=no; grep -q "harness cache HIT" "$s/t5" && hit5=yes
      printf "%s:%s:%s:%s:%s|%s|%s|%s|%s\n" "$n1" "$n2" "$n3" "$n4" "$n5" "$hit2" "$hit3" "$hit4" "$hit5"
    ' _ "$1" "$r49t/run-$2" "$r49t/scratch-$2" "$r49t/other-$2"
  }
  if [ "$r49t_ship" != 1 ] || [ "$r49t_mut" != 0 ]; then
    bad "check 49 (g) cannot run: the stale-key mutation did not land — the \`tree=\` key material appears $r49t_ship time(s) in the shipped hcached and $r49t_mut time(s) in the mutated copy (expected 1 then 0), so the control below would be comparing a copy against itself"
  else
    r49t_got=$(r49_tree "$r49_dir/hcached.sh" ship)
    r49t_bad=$(r49_tree "$r49_dir/hcached.notree.sh" mut)
    if [ "$r49t_got" != "1:1:2:3:3|yes|no|no|yes" ]; then
      bad "check 49 (g) — a \`:tree:\`-keyed gate must run once (1), HIT the same tree (1), MISS and RE-RUN when only web/ changes (want runs 2, hit no), refuse the cache on a dirty tree (want runs 3, hit no), and HIT the same bytes under another absolute path (want runs 3, hit yes); got [$r49t_got]"
    elif [ "$r49t_bad" != "1:1:1:2:2|yes|yes|no|yes" ]; then
      bad "check 49 (g) does NOT bite: the control copy whose key drops \`tree=\` must serve the STALE HIT when only web/ changed (want 1:1:1:2:2|yes|yes|no|yes), because that is the key shape this check exists to catch; got [$r49t_bad]"
    else
      ok "check 49 (g) BITES: a \`:tree:\`-keyed gate MISSES and re-runs when ONLY web/ changes while its scripts are byte-identical (runs 1 -> 2, no HIT), refuses the cache on a dirty tree (run 3), and still HITs the same bytes under another absolute path (run 3); the same fixture through a key without \`tree=\` serves that stale HIT (runs stay 1), so the measured false pass is RED here"
    fi
  fi

  # 49(h). THE DECLARATION MUST FOLLOW THE CLASSIFIER — derived, not hand-listed.
  #      `:tree:` is only safe where the runner itself says the gate reads the whole
  #      checkout (`<gate>: *` in --gate-deps). A cached gate in that class without
  #      `:tree:` is the defect; a cache key that can no longer see its `*` gate is
  #      this pin going vacuous, and both are RED.
  r49h_calls=0; r49h_star=""; r49h_missing=""
  while IFS= read -r _line; do
    _g=$(sed -nE 's/^[[:space:]]*hcached[[:space:]]+([A-Za-z0-9_-]+).*/\1/p' <<<"$_line")
    [ -n "$_g" ] || continue
    r49h_calls=$((r49h_calls+1))
    if grep -qxF "$_g: *" <<<"$deps"; then
      r49h_star="${r49h_star}${_g} "
      case "$_line" in
        *':tree:'*) : ;;
        *) r49h_missing="${r49h_missing}${_g} " ;;
      esac
    fi
  done < <(grep '^[[:space:]]*hcached[[:space:]]' "$PREFLIGHT")
  r49h_sel=$(grep -F 'selftest: ' <<<"$deps" | sed -n '1p')
  if [ "$r49h_calls" -eq 0 ]; then
    bad "check 49 (h) cannot run: no \`hcached\` call site was found in preflight.sh, so the cache's declaration is untested"
  elif [ -n "$r49h_missing" ]; then
    bad "check 49 (h) — a gate the runner classifies \`*\` (reads the whole checkout, so a key over a fixed file set cannot vouch for it) is cached WITHOUT the \`:tree:\` input: [${r49h_missing% }]. Give its hcached call site \`:tree:\`, or the cache can serve a pass for a tree the gate never saw"
  elif ! grep -qF ' selftest ' <<<" ${r49h_star} "; then
    bad "check 49 (h) — no cached gate in the \`*\` class is \`selftest\` any more (--gate-deps: [$r49h_sel]), so this pin no longer guards the gate it was written for; re-read whether selftest may narrow now, and update this pin rather than leave it vacuous"
  else
    ok "check 49 (h) — every cached gate the runner classifies \`*\` declares \`:tree:\` ([${r49h_star% }] of $r49h_calls hcached call sites; \`monitor\`'s fixed file set stays glob-keyed), checked against --gate-deps rather than a list here"
  fi
  rm -rf "$r49_dir"

# 50. TWO GATE EDITS CANNOT CONFLICT (card t_d6ce3d60).
#
#     THE DEFECT THIS PINS. Every gate row used to be ONE physical line carrying what
#     the gate catches, the size of the spec family it runs and the history of every
#     figure it had ever quoted. The `a11y` and `e2e` rows are ADJACENT, so a branch
#     that rewrote `a11y` and a main that rewrote `e2e` touched the same merge region
#     and `git merge` reported a conflict with both sides separately correct.
#     Measured: thirteen rework cards, each a full lane run whose only product was a
#     one-line splice, each holding the merge train until it landed. The cure is a
#     SHAPE: the rows are a stable index and each gate's mutable detail lives in its
#     own file under gate-inventory/.
#
#     This check proves the shape with a REAL git merge, driven on the LIVE bytes
#     (GATE-INVENTORY.md and gate-inventory/*.md copied into a throwaway repo — the
#     live tree is never mutated):
#       * arm 1: one branch grows the `a11y` figure in its own detail file, another
#         grows `e2e-core` in its own — the merge must be CLEAN, both figures must survive,
#         and the shared table must be untouched (neither side edited it);
#       * arm 2, the CONTROL: the SAME pair of edits in the OLD one-line-row shape
#         must CONFLICT. Without it a clean merge could be git being tolerant rather
#         than the split working.
#     Both arms refuse to conclude from a fixture that did not reproduce: if the edit
#     does not apply (a figure moved and the premise is stale), or the control merges
#     cleanly, that is RED rather than a pass — the same discipline check 43 uses.
s50="$SB_STATE/s50"
rm -rf "$s50"
mkdir -p "$s50/live/gate-inventory" "$s50/old"
cp "$HERE/../GATE-INVENTORY.md" "$s50/live/" 2>/dev/null
cp "$HERE"/../gate-inventory/*.md "$s50/live/gate-inventory/" 2>/dev/null
# s50_fig <file> — the FIRST `**N tests in M files**` span's text, or empty. Read with
# awk's LEFTMOST match, the contract spec-counts.sh uses, so this check's premise is
# the number the gate would actually compare.
s50_fig() {
  awk '{ if (match($0, /\*\*[0-9]+ tests? in [0-9]+ files?\*\*/)) { print substr($0, RSTART + 2, RLENGTH - 4); exit } }' "$1"
}
# s50_next <"N tests in M files"> — the same figure one test larger, so each arm's
# edit is exactly what a PR adding one spec does to its gate's file.
s50_next() {
  local n m
  n="${1%% test*}"; m="${1##* in }"; m="${m%% file*}"
  printf '%s tests in %s files\n' "$((n + 1))" "$m"
}
s50_git() { local d="$1"; shift; git -C "$d" -c user.email=t@t -c user.name=t "$@"; }

s50_a="$(s50_fig "$s50/live/gate-inventory/a11y.md")"
s50_e="$(s50_fig "$s50/live/gate-inventory/e2e-core.md")"
if [ -z "$s50_a" ] || [ -z "$s50_e" ]; then
  bad "check 50 cannot run: gate-inventory/{a11y,e2e-core}.md do not both carry a \`**N tests in M files**\` figure (a11y=[$s50_a] e2e-core=[$s50_e]), so the two edits this check merges would not be the edits that used to conflict"
else
  s50_a_new="$(s50_next "$s50_a")"
  s50_e_new="$(s50_next "$s50_e")"

  # ---- arm 1: the live bytes, each side in its own gate's detail file ----------
  s50_git "$s50/live" init -q .
  s50_git "$s50/live" add -A >/dev/null 2>&1
  s50_git "$s50/live" commit -q -m base >/dev/null 2>&1
  s50_base="$(s50_git "$s50/live" rev-parse --abbrev-ref HEAD)"
  s50_git "$s50/live" checkout -q -b side-a
  sed -i "s/\*\*$s50_a\*\*/**$s50_a_new**/" "$s50/live/gate-inventory/a11y.md"
  if grep -qF "**$s50_a_new**" "$s50/live/gate-inventory/a11y.md"; then
    s50_a_bit=yes
  else
    s50_a_bit=no
  fi
  s50_git "$s50/live" commit -qam "a11y detail grows" >/dev/null 2>&1
  s50_git "$s50/live" checkout -q "$s50_base"
  s50_git "$s50/live" checkout -q -b side-b
  sed -i "s/\*\*$s50_e\*\*/**$s50_e_new**/" "$s50/live/gate-inventory/e2e-core.md"
  if grep -qF "**$s50_e_new**" "$s50/live/gate-inventory/e2e-core.md"; then
    s50_e_bit=yes
  else
    s50_e_bit=no
  fi
  s50_git "$s50/live" commit -qam "e2e detail grows" >/dev/null 2>&1
  s50_git "$s50/live" checkout -q side-a
  s50_out="$(s50_git "$s50/live" merge --no-edit side-b 2>&1)"
  s50_rc=$?
  if [ "$s50_a_bit" != yes ] || [ "$s50_e_bit" != yes ]; then
    bad "check 50 premise broken: a figure edit did not apply (a11y=[$s50_a] -> [$s50_a_new] bit=$s50_a_bit, e2e=[$s50_e] -> [$s50_e_new] bit=$s50_e_bit). The live figure moved and the fixture no longer reproduces a count edit, so nothing can be concluded from the merge below"
  elif [ "$s50_rc" -ne 0 ]; then
    bad "check 50 — two branches that each grew ONE gate's detail file CONFLICTED (git merge rc=$s50_rc): $(printf '%s' "$s50_out" | tr '\n' ' ') — the split is not delivering the property it exists for"
  elif ! grep -qF "**$s50_a_new**" "$s50/live/gate-inventory/a11y.md"; then
    bad "check 50 — the merge was clean but the a11y edit ($s50_a_new) is not in the merged file: a merge that silently drops one side is worse than a conflict"
  elif ! grep -qF "**$s50_e_new**" "$s50/live/gate-inventory/e2e-core.md"; then
    bad "check 50 — the merge was clean but the e2e-core edit ($s50_e_new) is not in the merged file: a merge that silently drops one side is worse than a conflict"
  elif ! s50_git "$s50/live" diff --quiet "$s50_base" -- GATE-INVENTORY.md; then
    bad "check 50 — the shared table CHANGED in a merge that only grew two detail files, so a detail edit still reaches the adjacent one-liner rows"
  else
    ok "check 50 arm 1 — two branches that each grew ONE gate's figure ($s50_a_new in gate-inventory/a11y.md, $s50_e_new in gate-inventory/e2e-core.md) merged CLEAN with both edits surviving and the table untouched"
  fi

  # ---- arm 2: the CONTROL, in the old one-line-row shape ----------------------
  cat > "$s50/old/GATE-INVENTORY.md" <<'S50OLD'
| Gate | Job | Command | What it catches |
|---|---|---|---|
| `a11y` | web | `cd web && npm run a11y` | the axe family only, **159 tests in 17 files**, re-measured on a tree; the figure before it was **145 tests in 16 files** |
| `e2e` | web | `cd web && npm run e2e` | the feature specs, **276 tests in 55 files**, re-measured on a tree; the figure before it was **274 in 54** |
S50OLD
  s50_git "$s50/old" init -q .
  s50_git "$s50/old" add -A >/dev/null 2>&1
  s50_git "$s50/old" commit -q -m base >/dev/null 2>&1
  s50_obase="$(s50_git "$s50/old" rev-parse --abbrev-ref HEAD)"
  s50_git "$s50/old" checkout -q -b side-a
  sed -i '/^| `a11y` |/ s/159 tests in 17 files/162 tests in 17 files/' "$s50/old/GATE-INVENTORY.md"
  s50_git "$s50/old" commit -qam "a11y row grows" >/dev/null 2>&1
  s50_git "$s50/old" checkout -q "$s50_obase"
  s50_git "$s50/old" checkout -q -b side-b
  sed -i '/^| `e2e` |/ s/276 tests in 55 files/279 tests in 56 files/' "$s50/old/GATE-INVENTORY.md"
  s50_git "$s50/old" commit -qam "e2e row grows" >/dev/null 2>&1
  s50_git "$s50/old" checkout -q side-a
  s50_oout="$(s50_git "$s50/old" merge --no-edit side-b 2>&1)"
  s50_orc=$?
  if [ "$s50_orc" -eq 0 ]; then
    bad "check 50 CONTROL did not reproduce: the OLD one-line-row shape merged the two count edits CLEANLY, so the fixture proves nothing about the defect the split exists to remove"
  elif ! grep -q 'CONFLICT' <<<"$s50_oout"; then
    bad "check 50 CONTROL failed for the wrong reason: git merge exited $s50_orc without reporting a CONFLICT: $(printf '%s' "$s50_oout" | tr '\n' ' ')"
  else
    ok "check 50 arm 2 CONTROL — the SAME pair of count edits in the old one-line-row shape CONFLICTS (git merge rc=$s50_orc, 'CONFLICT (content)'), so arm 1's clean merge is the per-gate split's doing and not git's tolerance"
  fi
fi
rm -rf "$s50"

# 51. A RED GATE MUST NOT DELETE THE EVIDENCE THAT SAYS WHAT FAILED (card t_e2a3cf23).
#
#     THE DEFECT. `gate_record_fail_exit` used a gate's captured output only to
#     CLASSIFY the failure and then deleted it, so a red record carried `"log": ""`.
#     Measured on the v1.36.0 release tree (db4cae6 / commit 4c7ab94d7): `test` failed
#     after 710s and `e2e-core` after 850s, and no record could say WHICH package or
#     spec failed. Both had to be re-run blind (426s + 391s) and both PASSED on the
#     byte-identical tree, so two real reds became unattributable intermittents. A
#     release that ships on a bypassed gate cannot afford "the gate was red
#     yesterday, re-run it" as its only diagnostic.
#
#     THE PROPERTY, asserted on a REAL failed run in the sandbox (every gate there
#     fails fast on a missing prerequisite, so `--only <one gate>` is a genuine red):
#       (a) the record's `log` names a file that EXISTS and is NOT EMPTY, that file
#           carries the gate's own failing line (the first line the run streamed for
#           it — what the human at the terminal saw), and the record's `log_tail`
#           holds exactly the last 80 lines of that file, escaped once;
#       (b) THE BITE: the same run against a COPY whose preserve step is a no-op
#           must leave `log: ""` and keep no file. Without this leg the check could
#           stay green on a runner that preserves nothing;
#       (c) the kept-log store is BOUNDED: planted files older than the age window
#           and beyond the count cap are deleted by the pruner the preserve step
#           calls, so a host with 1781 records cannot grow logs for ever.
if [ -n "$probe_tree" ]; then
  rm -rf "$RECORD_DIR/logs"
  # THE RECORD IS KEYED ON THE TREE AT RUN TIME, NOT ON $probe_tree. Checks after
  # line 2735 legitimately change the sandbox (check 44 appends to GATE-INVENTORY.md
  # and commits, to give `--for-diff` a base), so $probe_tree describes the sandbox
  # as it WAS. Computing the path from the current tree is what makes this check
  # measure its own run instead of reading a file that run never wrote. The pre-run
  # delete keeps a record from an earlier check from being offered as a reuse.
  rm -f "$(gr_file "$(gr_tree_sha "$SB" 2>/dev/null)")"
  out51="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" 2>&1) )"
  rc51=$?
  rec="$(gr_file "$(gr_tree_sha "$SB" 2>/dev/null)")"
  log51=""; tail51=""
  if [ -r "$rec" ]; then
    log51="$(gr_read_field "$rec" log)"
    tail51="$(gr_read_field "$rec" log_tail)"
  fi
  # THE FAILING LINE AS THE RUN PRINTED IT: the first non-blank line after the
  # gate's banner. Taken from the RUN's output, not from the kept file, so the two
  # are independent evidence.
  fail51="$(printf '%s\n' "$out51" | awk -v pat="==> gate [$probe_gate]:" 'index($0, pat) { f=1; next } f && /[^[:space:]]/ { print; exit }')"
  s51_bad=""
  if [ "$rc51" -eq 0 ]; then
    s51_bad="the run exited 0 — every gate is expected to fail fast in this sandbox, so there is no red gate to attribute"
  elif [ ! -r "$rec" ]; then
    s51_bad="the run wrote NO record for the sandbox tree (rc=$rc51, expected [$rec]) — nothing can be attributed from a record that does not exist; last output [$(printf '%s' "$out51" | tail -n 1)]"
  elif [ -z "$log51" ]; then
    s51_bad="the RED gate recorded log:\"\" — its output was deleted, so nobody can say what failed (the defect this check exists for)"
  elif [ ! -s "$log51" ]; then
    s51_bad="the record names [$log51] but that file is missing or empty — it points at evidence that does not exist"
  elif [ -z "$fail51" ]; then
    s51_bad="premise broken: the run streamed no gate output between '$probe_gate' and its failure, so 'contains the failing line' cannot be measured"
  elif ! grep -qxF -- "$fail51" "$log51"; then
    s51_bad="the kept log does not contain the failing line the run printed ([$fail51]) — the record points at the wrong output"
  else
    s51_exp="$(tail -n 80 "$log51" 2>/dev/null | tail -c 12000)"
    s51_exp_json="$(gr_json_escape_text "$s51_exp")"
    if [ -z "$tail51" ]; then
      s51_bad="the record's log_tail is empty — a reader with only the JSON still cannot name the failure"
    elif [ "$tail51" != "$s51_exp_json" ]; then
      s51_bad="log_tail is not the last 80 lines of the kept log escaped once: got [$(printf '%.140s' "$tail51")] expected [$(printf '%.140s' "$s51_exp_json")]"
    elif command -v python3 >/dev/null 2>&1 && ! python3 -c 'import json,sys
json.load(open(sys.argv[1]))' "$rec" >/dev/null 2>&1; then
      s51_bad="the record is not valid JSON once log_tail is present — a reader that parses it (jq, python, a dashboard) cannot read the failure the field exists for"
    fi
    # THE ESCAPER ITSELF, on the shapes a gate log actually contains: quotes,
    # backslashes, tabs and blank lines. A one-line sandbox gate would not exercise
    # any of them, and a mangled escape corrupts the WHOLE record (every reader in
    # gate_result.sh is a line-anchored sed).
    if [ -z "$s51_bad" ] && command -v python3 >/dev/null 2>&1; then
      s51_raw="$(printf 'first "quoted" \\ back\nsecond\tline\n\nlast')"
      s51_esc="$(gr_json_escape_text "$s51_raw")"
      if ! python3 -c 'import json,sys
sys.exit(0 if json.loads("\"" + sys.argv[1] + "\"") == sys.argv[2] else 1)' "$s51_esc" "$s51_raw"; then
        s51_bad="gr_json_escape_text does not round-trip a multi-line value with quotes, backslashes and tabs (got [$s51_esc])"
      fi
    fi
  fi
  if [ -n "$s51_bad" ]; then
    bad "check 51 (a) — $s51_bad"
  else
    ok "check 51 (a) — a red gate keeps its output: the record's log names [$log51] ($(wc -c < "$log51") bytes), the file carries the failing line the run printed, and log_tail is exactly its last 80 lines"
  fi

  # ---- (b) THE BITE: remove the preserve step and the record must say log:"" ----
  s51_mut="$SB_STATE/s51-mutant"
  rm -rf "$s51_mut"
  cp -a "$SB" "$s51_mut"
  s51_mut_script="$s51_mut/scripts/preflight.sh"
  sed -i '/^gate_keep_failure_log() {/a\  return 0  # S51 MUTANT: the preserve step removed' "$s51_mut_script"
  s51_landed=no
  s51_next=""
  if grep -qF 'S51 MUTANT' "$s51_mut_script"; then
    s51_next="$(sed -n '/^gate_keep_failure_log() {/{n;p;q;}' "$s51_mut_script")"
    case "$s51_next" in *'return 0'*) s51_landed=yes ;; esac
  fi
  if [ "$s51_landed" != yes ]; then
    bad "check 51 (b) — the sabotage did not land: gate_keep_failure_log()'s first statement in the mutant is [${s51_next:-<none>}]. The arm that must FAIL was never built, so the green above would be untested."
  else
    s51_mut_dir="$SB_STATE/s51-mut-records"
    rm -rf "$s51_mut_dir"
    out51m="$( (cd "$s51_mut" && PREFLIGHT_RESULT_DIR="$s51_mut_dir" PREFLIGHT_KEY_EXTRA=probe15 ./scripts/preflight.sh --only "$probe_gate" 2>&1) )"
    rc51m=$?
    s51_mrec="$(find "$s51_mut_dir" -maxdepth 1 -type f -name '*.json' 2>/dev/null | sed -n 1p)"
    if [ "$rc51m" -eq 0 ] || [ -z "$s51_mrec" ]; then
      bad "check 51 (b) — the mutant run produced no red record (rc=$rc51m, records=[${s51_mrec:-none}]); the bite cannot be measured on a broken copy. Output: [$(printf '%s' "$out51m" | tail -n 1)]"
    else
      s51_mlog="$(gr_read_field "$s51_mrec" log)"
      s51_mfiles=0
      if [ -d "$s51_mut_dir/logs" ]; then
        s51_mfiles="$(find "$s51_mut_dir/logs" -maxdepth 1 -type f -name '*.log' 2>/dev/null | wc -l)"
      fi
      if [ -n "$s51_mlog" ] || [ "${s51_mfiles:-0}" -ne 0 ]; then
        bad "check 51 (b) — with the preserve step removed the record STILL carries log=[$s51_mlog] and $s51_mfiles kept file(s): the assertion in (a) would stay green for a runner that preserves nothing, so it is not measuring this fix"
      else
        ok "check 51 (b) — the bite holds: the SAME red run with the preserve step made a no-op leaves log:\"\" and no kept file, so (a) is a real assertion and not a tautology"
      fi
    fi
  fi
  rm -rf "$s51_mut"

  # ---- (c) THE STORE IS BOUNDED: age and count both prune kept logs ----
  s51_pr="$SB_STATE/s51-prune"
  rm -rf "$s51_pr"
  mkdir -p "$s51_pr/logs"
  sed -n '/^gate_log_dir() {/,/^gate_log_prune() {/p' "$PREFLIGHT" | sed '$d' > "$s51_pr/fns.sh"
  sed -n '/^gate_log_prune() {/,/^}$/p' "$PREFLIGHT" >> "$s51_pr/fns.sh"
  if ! grep -q '^gate_log_prune() {' "$s51_pr/fns.sh"; then
    bad "check 51 (c) — gate_log_prune() is not extractable from preflight.sh, so the bound on the kept-log store is untested"
  else
    : > "$s51_pr/logs/old.log";    touch -d '40 days ago' "$s51_pr/logs/old.log"
    : > "$s51_pr/logs/mid.log";    touch -d '10 days ago' "$s51_pr/logs/mid.log"
    : > "$s51_pr/logs/new-1.log";  touch -d '3 days ago'  "$s51_pr/logs/new-1.log"
    : > "$s51_pr/logs/new-2.log";  touch -d '2 days ago'  "$s51_pr/logs/new-2.log"
    : > "$s51_pr/logs/new-3.log";  touch -d '1 day ago'   "$s51_pr/logs/new-3.log"
    PREFLIGHT_RESULT_DIR="$s51_pr" PREFLIGHT_LOG_KEEP_DAYS=14 PREFLIGHT_LOG_KEEP_MAX=3 \
      bash -c '. "$1/fns.sh"; gate_log_prune' _ "$s51_pr"
    s51_left="$(cd "$s51_pr/logs" && ls -1 2>/dev/null | sort | tr '\n' ' ')"
    case "$s51_left" in
      "new-1.log new-2.log new-3.log ")
        ok "check 51 (c) — the kept-log store is bounded: a 40-day-old log is deleted by the age bound, a 10-day-old one by the count cap (max 3), and the three newest survive [$s51_left]" ;;
      *)
        bad "check 51 (c) — pruning left [$s51_left]; expected only the three newest logs. An unpruned store grows for ever on a host that already carries 1781 records." ;;
    esac
  fi
  rm -rf "$s51_pr"
fi

# 52. A REMEMBERED `web` PASS IS NOT A REMEMBERED BUILD (card t_7ad889f7).
#
#     THE DEFECT, measured on the deploy lane 2026-11-12. The record is keyed on the
#     TREE plus the environment fingerprint, which is the right key for a gate that
#     only READS. `web` also PRODUCES web/.next, and a11y/e2e-builder/e2e-core start
#     `next start` against it (the runner says so at the a11y gate: "Needs web/build
#     to have produced .next"). deploy/cut-release.sh --ship-only re-seeds a FRESH
#     release tree with the SAME tree sha and only web/node_modules carried over, so
#     on a recovery run the record said `web: pass`, nothing rebuilt, and both
#     browser gates died in 6s on
#         [WebServer] Error: Could not find a production build in the '.next'
#         directory
#     — two reds with no product assertion behind them, and the ship aborted (exit 5).
#
#     THE PROPERTY: a remembered producer pass is not reused when its ARTEFACT is
#     absent and a gate that reads it is going to run, and it IS still reused when
#     either condition does not hold. Six legs, every one on a REAL record written
#     for the sandbox tree and read by the REAL consumer / the REAL runner:
#       (a) consumer, no build, a browser gate in the rerun set -> `web` moves from
#           PREFLIGHT_SKIP to PREFLIGHT_RERUN and the evidence says why;
#       (b) CONTROL, same record and same selection with web/.next/BUILD_ID present
#           -> `web` stays remembered (a rule that never remembers web would be a
#           silent 90s tax on every fresh release tree);
#       (c) CONTROL, no build but NO consumer in the run (only `lint` failed, so
#           only `lint` + `web` are asked for) -> `web` stays remembered;
#       (d) runner, no build: the SAME record with `--only web,a11y,e2e-core
#           --reuse-record` must NOT fold the recorded `web` pass into SKIP — it
#           must try to run `web`, which the sandbox refuses on web's OWN missing
#           prerequisite, not on a11y's;
#       (e) CONTROL for (d): the same argv with web/.next/BUILD_ID present folds
#           `web` (REUSED names it) and the refusal is a11y's prerequisite;
#       (f) the runner's own net: a reuse run that asks for a browser gate WITHOUT
#           `web` and without a build refuses UP FRONT, naming web/.next and the
#           gate — instead of letting Next.js's line be the only signal.
#     (d)/(e) discriminate on the FOLD (`==> REUSED`, `==> NOT REUSING 'web'`, and the
#     `STILL SKIPPED AND NOT REUSED` line), not on which prerequisite the sandbox
#     trips: a11y sets need_web=1 too, so both arms end on the same next-binary
#     refusal and that text cannot tell them apart. No gate body is executed either
#     way — the routing is what is being measured.
s52_gates=" $(printf '%s' "$GATES" | tr '\n' ' ') "
s52_missing=""
for s52_g in $GR_ARTEFACT_PRODUCER $GR_ARTEFACT_CONSUMERS; do
  case "$s52_gates" in
    *" ${s52_g} "*) ;;
    *) s52_missing="$s52_missing $s52_g" ;;
  esac
done
s52_web_call="$(sed -n "s/^run $GR_ARTEFACT_PRODUCER \"\\(.*\\)\".*/\\1/p" "$PREFLIGHT" | sed -n 1p)"
s52_pw_start="$(sed -n 's/.*"start": *"\([^"]*\)".*/\1/p' "$LIVE_ROOT/web/package.json" 2>/dev/null | sed -n 1p)"
s52_pw_server="$(grep -c 'npm run start' "$LIVE_ROOT/web/playwright.config.ts" 2>/dev/null)"
if [ -n "$s52_missing" ]; then
  bad "check 52 — the artefact table names gate(s) that \`--list\` does not advertise:${s52_missing}. A renamed gate leaves the rule pointing at nothing, so the browser gates would again be remembered past a missing build without any check noticing"
elif [ -z "$s52_web_call" ] || [ "${s52_web_call#*npm run build}" = "$s52_web_call" ]; then
  bad "check 52 — \`run $GR_ARTEFACT_PRODUCER\` is no longer a BUILD call site (read [$s52_web_call] from $PREFLIGHT), so the gate the rule protects is not the one that produces web/.next any more — re-read the rule rather than leaving it aimed at the wrong gate"
elif [ "$s52_pw_start" != "next start" ]; then
  bad "check 52 — web/package.json's 'start' script is [$s52_pw_start], not 'next start', so the server the browser gates boot is not the Next production server the artefact table reasons about"
elif [ "${s52_pw_server:-0}" -lt 1 ]; then
  bad "check 52 — web/playwright.config.ts no longer starts the app's own server (\`npm run start\` not found), so a11y/e2e-builder/e2e-core may not read web/.next at all and the table needs re-deriving"
else
  # s52_fixture <tree> <"gate=pass gate=fail ..."> — a record the REAL reader accepts
  # under the CURRENT key. Statuses not named are `skip` (never observed), which is
  # exactly what a partial record holds.
  s52_fixture() {
    local tree="$1" spec=" $2 " rec g st sec first=1
    rec="$(gr_file "$tree")"
    mkdir -p "$RECORD_DIR"
    {
      printf '{\n  "format": 1,\n  "tree": "%s",\n  "key": "%s",\n  "key_parts": "%s",\n  "gates": {\n' \
        "$tree" "$(PREFLIGHT_KEY_EXTRA=probe52 gr_key "$SB")" \
        "$(gr_json_escape "$(PREFLIGHT_KEY_EXTRA=probe52 gr_key_parts "$SB")")"
      for g in $GATES; do
        st=skip; sec=0
        case "$spec" in
          *" $g=pass "*) st=pass; sec=42 ;;
          *" $g=fail "*) st=fail; sec=7 ;;
        esac
        [ "$first" = 1 ] || printf ',\n'
        printf '    "%s": { "status": "%s", "seconds": %s, "at": "2026-01-01T00:00:00Z", "reused": false }' "$g" "$st" "$sec"
        first=0
      done
      printf '\n  },\n  "runs": 1,\n  "verdict": "red",\n  "complete": true,\n  "log": "",\n  "finished_at": "%s"\n}\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    } > "$rec"
  }
  # s52_kv <output> <VAR> — the decide path prints VAR=value lines.
  s52_kv() { sed -n "s/^$2=//p" <<<"$1" | sed -n 1p; }
  # s52_has <csv> <name> — "is this gate in the list", with commas, not substrings.
  s52_has() { case ",${1:-}," in *",$2,"*) return 0 ;; esac; return 1; }

  s52_dir="$SB_STATE/s52"
  rm -rf "$s52_dir"; mkdir -p "$s52_dir"
  # web/.next must not already be there (an earlier state of the sandbox must not
  # decide the premise of legs (a)/(c)).
  rm -rf "$SB/web/.next"

  # ---- (a) no build + a consumer in the rerun set -> web is NOT remembered ------
  s52_tree_a="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_a" "web=pass a11y=fail e2e-builder=pass e2e-core=fail lint=pass"
  s52_sel="web,a11y,e2e-builder,e2e-core,lint"
  s52_rec_a="$(gr_file "$s52_tree_a")"
  s52_usable="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 gr_record_usable "$s52_tree_a" 2>&1) )" && s52_ok_a=yes || s52_ok_a=no
  dec52a="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 PREFLIGHT_ONLY="$s52_sel" ./scripts/preflight_reuse.sh 2>&1) )"
  rc52a=$?
  s52a_skip="$(s52_kv "$dec52a" PREFLIGHT_SKIP)"
  s52a_rerun="$(s52_kv "$dec52a" PREFLIGHT_RERUN)"
  s52a_dec="$(s52_kv "$dec52a" PREFLIGHT_DECISION)"
  if [ "$s52_ok_a" != yes ]; then
    bad "check 52 premise broken — the fixture record for tree $s52_tree_a is not usable under the current key ([$s52_usable]), so no decision below could be about it"
  elif [ "$rc52a" -ne 0 ] || [ "$s52a_dec" != "partial" ]; then
    bad "check 52 (a) — the consumer must decide from this record (rc=$rc52a, decision=[$s52a_dec]): [$(printf '%s' "$dec52a" | tr '\n' ' ')]"
  elif s52_has "$s52a_skip" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (a) — \`$GR_ARTEFACT_PRODUCER\` is REMEMBERED (PREFLIGHT_SKIP=$s52a_skip) although the tree has no $GR_ARTEFACT_FILE and a11y/e2e-core (PREFLIGHT_RERUN=$s52a_rerun) are about to start the production server: this is the measured defect — the record's pass is evidence about the BYTES, not about a build directory"
  elif ! s52_has "$s52a_rerun" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (a) — \`$GR_ARTEFACT_PRODUCER\` is not in PREFLIGHT_RERUN=[$s52a_rerun], so nothing in this pass would build the artefact the browser gates are about to read"
  elif ! s52_has "$s52a_rerun" "a11y" || ! s52_has "$s52a_skip" "lint"; then
    bad "check 52 (a) — the rest of the decision is wrong (SKIP=[$s52a_skip] RERUN=[$s52a_rerun]): the recorded passes that have no artefact to produce must still be remembered"
  elif ! grep -qF 'NOT remembering' <<<"$dec52a"; then
    bad "check 52 (a) — the evidence line does not say the producer's pass was NOT reused, so a reader sees a rebuild in PREFLIGHT_RERUN with no reason attached: [$(printf '%s' "$dec52a" | tr '\n' ' ')]"
  else
    ok "check 52 (a) — a remembered \`$GR_ARTEFACT_PRODUCER\` pass is NOT reused when the tree has no $GR_ARTEFACT_FILE and a11y is about to run: SKIP=[$s52a_skip] RERUN=[$s52a_rerun], and the evidence names the rebuild"
  fi

  # ---- (b) CONTROL: the build IS there -> web stays remembered ------------------
  mkdir -p "$SB/web/.next" && printf 's52\n' > "$SB/web/.next/BUILD_ID"
  s52_tree_b="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_b" "web=pass a11y=fail e2e-builder=pass e2e-core=fail lint=pass"
  dec52b="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 PREFLIGHT_ONLY="$s52_sel" ./scripts/preflight_reuse.sh 2>&1) )"
  rc52b=$?
  s52b_skip="$(s52_kv "$dec52b" PREFLIGHT_SKIP)"
  s52b_rerun="$(s52_kv "$dec52b" PREFLIGHT_RERUN)"
  if [ "$s52_tree_b" = "$s52_tree_a" ]; then
    bad "check 52 (b) — creating web/.next/BUILD_ID did not change the sandbox tree sha, so this control is the same tree as (a) and its record could not be the 'build present' shape"
  elif [ "$rc52b" -ne 0 ] || ! s52_has "$s52b_skip" "$GR_ARTEFACT_PRODUCER" || s52_has "$s52b_rerun" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (b) CONTROL lost — with $GR_ARTEFACT_FILE PRESENT the recorded \`$GR_ARTEFACT_PRODUCER\` pass must still be reused (rc=$rc52b SKIP=[$s52b_skip] RERUN=[$s52b_rerun]); a rule that never remembers it would add a full web build to every release recovery for nothing"
  else
    ok "check 52 (b) — CONTROL: with $GR_ARTEFACT_FILE present the same record and selection still REMEMBER \`$GR_ARTEFACT_PRODUCER\` (SKIP=[$s52b_skip] RERUN=[$s52b_rerun]), so the rule only rebuilds when the artefact is really missing"
  fi
  rm -rf "$SB/web/.next"

  # ---- (c) CONTROL: no consumer will run -> web stays remembered ---------------
  s52_tree_c="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_c" "web=pass lint=fail"
  dec52c="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 PREFLIGHT_ONLY="web,lint" ./scripts/preflight_reuse.sh 2>&1) )"
  rc52c=$?
  s52c_skip="$(s52_kv "$dec52c" PREFLIGHT_SKIP)"
  s52c_rerun="$(s52_kv "$dec52c" PREFLIGHT_RERUN)"
  if [ "$rc52c" -ne 0 ] || ! s52_has "$s52c_skip" "$GR_ARTEFACT_PRODUCER" || s52_has "$s52c_rerun" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (c) CONTROL lost — with NO browser gate in this run (PREFLIGHT_ONLY=web,lint) the recorded \`$GR_ARTEFACT_PRODUCER\` pass must be reused (rc=$rc52c SKIP=[$s52c_skip] RERUN=[$s52c_rerun]): rebuilding a build nobody reads is cost without coverage"
  else
    ok "check 52 (c) — CONTROL: when no gate reads the artefact the recorded \`$GR_ARTEFACT_PRODUCER\` pass is still reused (SKIP=[$s52c_skip] RERUN=[$s52c_rerun]), so a fresh release tree is not charged a build it does not need"
  fi

  # ---- (d) the RUNNER must not fold the recorded web pass ----------------------
  # The record is rewritten for this tree with a11y FAILING so the pre-pool
  # whole-selection exit cannot answer for it: this run really reaches the per-gate
  # fold, which is the mechanism under test.
  s52_tree_d="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_d" "web=pass a11y=fail e2e-core=pass"
  out52d="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 ./scripts/preflight.sh --only "web,a11y,e2e-core" --reuse-record 2>&1) )"
  rc52d=$?
  s52d_reused="$(sed -n 's/^==> REUSED (not re-run, recorded pass on THESE bytes): //p' <<<"$out52d" | sed -n 1p)"
  s52d_skipped="$(sed -n 's/^    STILL SKIPPED AND NOT REUSED (no recorded pass): //p' <<<"$out52d" | sed -n 1p)"
  # The routing is read off the FOLD, not off which prerequisite the run trips: the
  # sandbox has no web/node_modules, and a11y's own prerequisite (need_web=1) refuses
  # with the SAME next-binary message, so the refusal text cannot discriminate the two
  # arms — the REUSED line can, and so can "STILL SKIPPED AND NOT REUSED".
  if [ "$rc52d" -eq 0 ]; then
    bad "check 52 (d) — the reuse run exited 0 in a sandbox where web/a11y have no node_modules, so this leg measured nothing: [$(printf '%s' "$out52d" | tail -n 2 | tr '\n' ' ')]"
  elif ! grep -qF "NOT REUSING '$GR_ARTEFACT_PRODUCER'" <<<"$out52d"; then
    bad "check 52 (d) — the runner did not refuse to reuse \`$GR_ARTEFACT_PRODUCER\` (no 'NOT REUSING' line) although $GR_ARTEFACT_FILE is absent and a11y is in the same run: [$(printf '%s' "$out52d" | grep -E 'REUSED|NOT REUSING|PREFLIGHT FAIL' | tr '\n' ' ')]"
  elif s52_has "$s52d_reused" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (d) — the runner folded the recorded \`$GR_ARTEFACT_PRODUCER\` pass into SKIP (REUSED=[$s52d_reused]) although $GR_ARTEFACT_FILE is absent and a11y is in the same run: asking for web BY NAME would not build it, which is the second half of the measured defect"
  elif s52_has "$s52d_skipped" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (d) — \`$GR_ARTEFACT_PRODUCER\` is still on the SKIPPED line [$s52d_skipped], so it was skipped for having no recorded pass rather than executed: nothing would build the artefact"
  else
    ok "check 52 (d) — the RUNNER re-runs \`$GR_ARTEFACT_PRODUCER\` instead of folding its recorded pass: asked for web,a11y,e2e-core on a tree with no build it refuses to reuse web ('NOT REUSING'), folds only e2e-core (REUSED=[${s52d_reused:-none}]) and executes web — which is where the sandbox stops it, on web's own missing prerequisite"
  fi

  # ---- (e) CONTROL for (d): with the build present web IS folded ---------------
  mkdir -p "$SB/web/.next" && printf 's52\n' > "$SB/web/.next/BUILD_ID"
  s52_tree_e="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_e" "web=pass a11y=fail e2e-core=pass"
  out52e="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 ./scripts/preflight.sh --only "web,a11y,e2e-core" --reuse-record 2>&1) )"
  rc52e=$?
  s52e_reused="$(sed -n 's/^==> REUSED (not re-run, recorded pass on THESE bytes): //p' <<<"$out52e" | sed -n 1p)"
  if [ "$s52_tree_e" = "$s52_tree_d" ]; then
    bad "check 52 (e) — the tree sha did not move when web/.next appeared, so this control cannot discriminate the two shapes"
  elif [ "$rc52e" -eq 0 ]; then
    bad "check 52 (e) — the control run exited 0 where a11y is selected and has no node_modules: [$(printf '%s' "$out52e" | tail -n 2 | tr '\n' ' ')]"
  elif grep -qF "NOT REUSING '$GR_ARTEFACT_PRODUCER'" <<<"$out52e"; then
    bad "check 52 (e) CONTROL lost — with $GR_ARTEFACT_FILE PRESENT the recorded \`$GR_ARTEFACT_PRODUCER\` pass must be folded (the build exists), but the run refused to reuse it: a rule that always rebuilds costs a web build on every release recovery"
  elif ! s52_has "$s52e_reused" "$GR_ARTEFACT_PRODUCER"; then
    bad "check 52 (e) CONTROL lost — with $GR_ARTEFACT_FILE PRESENT the SAME argv must still fold the recorded pass; REUSED=[${s52e_reused:-none}] does not name \`$GR_ARTEFACT_PRODUCER\`"
  else
    ok "check 52 (e) — CONTROL: with $GR_ARTEFACT_FILE present the SAME argv folds the recorded pass (REUSED=[${s52e_reused:-none}], no 'NOT REUSING'), so (d)'s rebuild is caused by the missing artefact and nothing else"
  fi
  rm -rf "$SB/web/.next"

  # ---- (f) the runner's own net: reuse + a browser gate + no producer ---------
  # A hand-copied PREFLIGHT_RERUN (or a narrower PREFLIGHT_ONLY) can ask for a
  # browser gate without `web`; the consumer cannot fix that argv, so the runner
  # must refuse UP FRONT and name the artefact instead of letting Next.js's
  # "Could not find a production build" read as the gate's verdict.
  s52_tree_f="$(gr_tree_sha "$SB")"
  s52_fixture "$s52_tree_f" "lint=pass a11y=fail"
  out52f="$( (cd "$SB" && PREFLIGHT_KEY_EXTRA=probe52 ./scripts/preflight.sh --only "a11y,lint" --reuse-record 2>&1) )"
  rc52f=$?
  if [ "$rc52f" -eq 0 ]; then
    bad "check 52 (f) — the run exited 0 with a11y selected and no build in the tree, so the net did not fire: [$(printf '%s' "$out52f" | tail -n 2 | tr '\n' ' ')]"
  elif ! grep -qF "$GR_ARTEFACT_FILE" <<<"$out52f"; then
    bad "check 52 (f) — a reuse run that executes a11y with no build must NAME $GR_ARTEFACT_FILE and the cause; instead it failed with [$(grep -E 'PREFLIGHT FAIL' <<<"$out52f" | sed -n 1p)] — an operator cannot tell that from a product red"
  elif ! grep -qF 'a11y' <<<"$out52f" || ! grep -qF 'not in this run' <<<"$out52f"; then
    bad "check 52 (f) — the refusal must name the gate that needs the build and why \`$GR_ARTEFACT_PRODUCER\` is not in the run: [$(grep -E 'PREFLIGHT FAIL' <<<"$out52f" | sed -n 1p)]"
  elif grep -qF '==> gate [' <<<"$out52f"; then
    bad "check 52 (f) — the run STARTED a gate before refusing, so a suite was launched against a tree with no build: [$(grep -E '==> gate \[' <<<"$out52f" | tr '\n' ' ')]"
  else
    ok "check 52 (f) — a reuse run that would execute a11y without a build refuses before any gate, naming $GR_ARTEFACT_FILE and that \`$GR_ARTEFACT_PRODUCER\` is not in the run (rc=$rc52f) — not a product red"
  fi
  rm -rf "$s52_dir"
fi

# 53. deploy/web_modules.sh: A SYMLINKED web/node_modules IS NOT A DONOR (card
#     t_eaf5edd4). The seeder hands a fresh worktree its `web/node_modules` with
#     `cp -a`, and `cp -a` reproduces a symlink AS a symlink — so a donor whose
#     own web/node_modules was a link (many long-lived worktrees on this host point
#     it at the main checkout) left the CANDIDATE with a link, `cp -a` exited 0,
#     `[ -x .bin/next ]` followed the link and agreed the tree was installed, and
#     three minutes later the `test` gate died in a guard that WALKS the tree:
#
#       model_default_guard_test.go: walk <candidate> for model ids:
#         read <candidate>/web/node_modules: is a directory
#
#     (`filepath.Walk` lstats each entry, so a symlink is never a directory and the
#     skipDirs arm for node_modules never applies.) Measured on the train run of
#     2026-11-12 14:31 for PR #1733 — the batch went GREEN the moment
#     TRAIN_NODE_MODULES named a REAL donor, so the seeder was the cause.
#
#     That failure lands in a LANE whose own change is innocent, minutes later and
#     one gate away from the cause, which is exactly the shape this self-test
#     exists for. The assertions live in deploy/web_modules_test.sh (the seeder's
#     own suite) so they can be run on their own; this makes the `selftest` gate
#     run them, because a regression net nobody runs is not a net.
#
#     The suite's own bite is inside it (case 7): `wm_is_symlink` is neutered in a
#     copy of the seeder and the defect must then reappear, so a green here cannot
#     be a suite whose rule is dead code. Pointed at the PRE-FIX seeder through its
#     WM_TEST_LIB seam (git show origin/main:deploy/web_modules.sh > /tmp/old.sh,
#     then WM_TEST_LIB=/tmp/old.sh bash deploy/web_modules_test.sh) it is 0 of 7,
#     so the suite has been seen to fail on the bytes that caused the measured red.
if [ ! -r "$LIVE_ROOT/deploy/web_modules_test.sh" ]; then
  bad "check 53 — $LIVE_ROOT/deploy/web_modules_test.sh is MISSING: nothing proves a symlinked donor is refused, and an absent guard is a failure, never a skip (the seeder would silently re-open the web/node_modules-as-a-link class that reds the model-id guard one gate later)"
elif out53="$(bash "$LIVE_ROOT/deploy/web_modules_test.sh" 2>&1)"; then
  ok "check 53 — the seeder refuses a symlinked donor at every door (worktree, WM_TEMPLATE, RELEASE_NODE_MODULES, WM_CACHE_DIR, WM_LOCK_CACHE, and a tree's own link) and the suite's own bite re-creates the defect when the rule is neutered: [$(printf '%s\n' "$out53" | sed -n '$p')]"
else
  bad "check 53 — deploy/web_modules_test.sh FAILED: a symlinked web/node_modules can reach a seeded tree again, and the failure will surface in the NEXT lane as 'read .../web/node_modules: is a directory' in the model-id guard: [$(printf '%s\n' "$out53" | sed -n '/FAIL/p' | tr '\n' '|')]"
fi

echo "---"
echo "preflight self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
