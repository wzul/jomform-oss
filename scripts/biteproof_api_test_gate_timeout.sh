#!/usr/bin/env bash
#
# scripts/biteproof_api_test_gate_timeout.sh — prove the api `test` gate's
# explicit per-package budget BITES.
#
# THE GUARD UNDER TEST. `scripts/preflight.sh` runs the api suite as
# `go test ./... -race -count=1 -timeout <API_TEST_TIMEOUT>` (default 30m). That
# `-timeout` is load-bearing: `go test` panics a package after 10 minutes, and
# that budget is PER PACKAGE, so without an explicit one `internal/httpapi` — the
# largest package, whose cost tracks host load — is reported as
# `panic: test timed out after 10m0s` while it is GREEN. Measured on this host:
# pristine `origin/main` (ca110813) rc=1 at 601.195s, the same package rc=0 at
# 620.593s. A red that looks exactly like a hung test is a red nobody can act on.
# The per-run half of the guard is `selftest` check 17 in
# scripts/preflight_test.sh; this harness proves that check can FAIL.
#
# WHY THE CASES READ THE **ARGV**, NOT THE FILE TEXT. A grep for "-timeout" is
# satisfied by a comment, by the gate's own human-readable label, and by this
# document. Case 3 below is exactly that shape: the budget kept in the label the
# runner prints and removed from the command it actually executes. Only reading
# the argv `go` was handed can see it — which is why the guard does the same.
#
# WHY A SCRIPT AND NOT A PER-RUN GATE: every case mutates scripts/preflight.sh in
# place. The live `selftest` gate runs against the tree under test, so it can
# never sabotage its own subject; that is precisely why the per-run half is a
# property check and the sabotage lives here. Cases are seconds each only because
# `go` is stubbed — the alternative (a real `go test`) is ~10 minutes a case, and
# this repo's rule is that a gate which costs that on every merge gets skipped.
#
# Run:  ./scripts/biteproof_api_test_gate_timeout.sh
# Exit: 0 if every case BITES, 1 otherwise. Never runs a real test suite.
#
# The mutated file is restored after EVERY case and re-verified by sha256; case 5
# additionally runs the real `selftest` gate against the mutated runner and
# restores it afterwards. Do not run this while another lane is gating out of the
# same checkout: the tree is briefly dirty by design.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

RUNNER="scripts/preflight.sh"
STUB="$REPO_ROOT/.biteproof-go-stub"
LOG=/tmp/biteproof_api_timeout_gate.log
ORIG=/tmp/biteproof_api_timeout.orig

[ -f "$RUNNER" ] || { echo "FAIL: $RUNNER not found — cannot prove anything about the gate" >&2; exit 2; }

# This harness must never queue on (or hold) the production gate lock: it is not a
# gate run, and on a busy board it would delay every card waiting behind it. The
# same rule and reason as scripts/preflight_test.sh check 16.
export PREFLIGHT_LOCK_FILE="${PREFLIGHT_LOCK_FILE:-/tmp/biteproof_api_timeout.lock}"

cp "$RUNNER" "$ORIG"
WANT="$(sha256sum "$RUNNER" | awk '{print $1}')"

# `go` is stubbed so no case can cost a real suite: it prints the argv it was
# handed and exits non-zero, and the argv is what the assertions read.
mkdir -p "$STUB"
cat > "$STUB/go" <<'FAKEGO'
#!/bin/sh
echo "FAKE-GO ARGV: $*"
exit 99
FAKEGO
chmod +x "$STUB/go"

restore() { cp "$ORIG" "$RUNNER"; }
cleanup() { restore; rm -rf "$STUB" "$ORIG"; }
trap cleanup EXIT

# restore_and_verify — a case that leaves the runner dirty would poison every lane
# after it, so the restore is asserted, not assumed.
restore_and_verify() {
  restore
  local got; got="$(sha256sum "$RUNNER" | awk '{print $1}')"
  if [ "$got" = "$WANT" ]; then
    ok "runner restored byte-identically (sha256 ${got:0:12})"
  else
    bad "$RUNNER was NOT restored byte-identically (sha256 $got != $WANT) — restore it from $ORIG BY HAND before doing anything else"
  fi
}

# gate_argv [API_TEST_TIMEOUT] — run the LIVE runner's `test` gate with the stubbed
# `go` and print the argv `go` was handed. Placeholder prerequisites so the run
# reaches the gate (an unset var would stop the runner at the env prerequisite
# instead, and the case would then be asserting the wrong thing). The runner is
# whatever the case has mutated it into.
gate_argv() {
  local budget="${1:-}"
  env PATH="$STUB:$PATH" ${budget:+API_TEST_TIMEOUT="$budget"} \
      JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder \
      ./scripts/preflight.sh --only test 2>&1 \
    | sed -n 's/^FAKE-GO ARGV: //p' | sed -n '1p'
}

# budget_of — the `-timeout` VALUE, anchored on `-race -count=1 -timeout ` so no
# other flag's argument can satisfy it. Prints nothing when there is no budget.
budget_of() { sed -nE 's/.*-race -count=1 -timeout ([^ ]*).*/\1/p' <<<"$1" | sed -n '1p'; }

# assert_mutated — a mutation that never landed is a silent GREEN. `--` so a
# needle that begins with a dash (e.g. "-timeout '30m'") is not read as a grep
# option — that misread is how this case first reported a false green.
assert_mutated() {
  local f="$1" needle="$2"
  if grep -qF -- "$needle" "$f"; then return 0; fi
  bad "$f was not mutated (looked for: $needle) — the case would have reported a false green"
  return 1
}

# case_gate — the gate must lose its budget (or stop handing `go` anything) once
# the runner is mutated. $2 says what the case is, for the verdict line.
case_gate() {
  local name="$1" why="$2" argv budget
  argv="$(gate_argv)"
  budget="$(budget_of "$argv")"
  if [ -z "$budget" ]; then
    ok "$name — $why: the gate hands \`go test\` NO -timeout budget (argv: [${argv:-none}]), so the guard's 17a goes RED"
  else
    bad "$name — the gate STILL hands \`go test\` -timeout $budget after the sabotage, so 17a would stay green and the missing budget would ship. argv: [$argv]"
  fi
}

echo "bite proof: the api test gate's explicit -timeout budget"
echo "  repo: $(pwd)"
echo "  head: $(git rev-parse --short HEAD)"
echo "  runner: $RUNNER (sha256 ${WANT:0:12})"
echo "---"

# --- case 0: the healthy tree must CARRY a budget (a guard that always fails is as
# useless as one that never fires, and the card requires this direction too) ------
argv0="$(gate_argv)"; budget0="$(budget_of "$argv0")"
if [ -n "$budget0" ]; then
  ok "healthy tree at HEAD — the gate hands \`go test\` -timeout $budget0 (not a tautology: there IS a budget to remove, which is what makes the cases below meaningful)"
else
  bad "healthy tree at HEAD — the gate hands \`go test\` no -timeout at all, so this harness has nothing to prove and every case below would pass vacuously. argv: [$argv0]"
fi

# --- case 1: THE PRE-FIX SHAPE. The budget gone from the gate line entirely —
# byte-for-byte what `origin/main` did before this card, and what produced
# `panic: test timed out after 10m0s` for a green `internal/httpapi`.
echo "== case 1: -timeout removed from the gate line (the pristine-main shape) =="
sed -i 's/ -timeout \$API_TEST_TIMEOUT//g; s/ -timeout .\$API_TEST_TIMEOUT.//g' "$RUNNER"
# The gate line now streams through `tee` (the failure classifier, card
# t_abfc22b7), so the literal `…-count=1"` no longer ends it. What this case is
# about is the FLAG being gone from the BUDGET the gate hands `go test` — asserted
# through the harness's own argv instrumentation, NOT through a grep of the file:
# the file's comments legitimately mention `-timeout`, so a text grep reports a
# false RED (measured) and that is precisely the class this harness exists to catch.
mutated_budget="$(budget_of "$(gate_argv)")"
if [ -n "$mutated_budget" ]; then
  bad "the gate still hands \`go test\` -timeout $mutated_budget after the mutation — the case would have reported a false green"
else
case_gate "case 1" "with the flag gone every package inherits Go's 10m per-package default"
fi
restore_and_verify

# --- case 2: THE ALARM DISABLED. `-timeout 0` is legal to `go test` and turns the
# panic off entirely, so a genuinely hung package blocks the gate with no bound.
# The runner must REFUSE this rather than hand it to `go` — a budget that can be
# set to "never" is not a budget.
# The pattern reads the default's CURRENT value ([^}]*) instead of naming 30m: a
# deliberate change of the default must not turn this case into a false red, and
# the runner's own default is already exercised by case 0 and the per-run checks.
echo "== case 2: the default made '0' (the alarm disabled) =="
sed -i 's/^: "${API_TEST_TIMEOUT:=[^}]*}"/: "${API_TEST_TIMEOUT:=0}"/' "$RUNNER"
if assert_mutated "$RUNNER" 'API_TEST_TIMEOUT:=0'; then
  argv="$(gate_argv)"
  if [ -z "$argv" ]; then
    ok "case 2 — a default of '0' means \`go test\` is never invoked at all (the runner refuses it), so '-timeout 0' can never ship as the budget: argv none"
  else
    bad "case 2 — the runner handed \`go test\` [$argv] with a '0' default; '-timeout 0' DISABLES the alarm, so a hung package would block the gate with no bound"
  fi
fi
restore_and_verify

# --- case 3: THE GREP-PROOF CASE. The budget kept in the label the runner PRINTS
# ("cd api && go test ./... -race -count=1 -timeout 30m" — what a reader, and a
# grep, sees) and removed from the command it actually EXECUTES. A guard that read
# the file's text, its comments or its label would call this tree healthy.
echo "== case 3: -timeout kept in the printed label, removed from the command that RUNS =="
sed -i "s|go test ./... \$RACE -count=1 -timeout '\$API_TEST_TIMEOUT'|go test ./... \$RACE -count=1|g" "$RUNNER"
label_kept=""
grep -qF 'go test ./... $RACE -count=1 -timeout $API_TEST_TIMEOUT' "$RUNNER" && label_kept=1
if [ -z "$label_kept" ]; then
  bad "case 3 — the mutation removed the label's budget too, so this case is not the shape it claims (case 1 already covers that one); nothing is proven about a grep-based guard here"
else
  case_gate "case 3" "the label still advertises the budget while the EXECUTED command has none — only reading the ARGV sees it"
fi
restore_and_verify

# --- case 4: THE OVERRIDE DEFEATED. The flag is still passed but HARDCODED, so
# API_TEST_TIMEOUT no longer reaches `go test`. Check 17b exists for exactly this:
# a host slower than the default could then not raise its ceiling without editing
# the runner, and nothing would notice the knob had gone dead.
echo "== case 4: -timeout passed as a hardcoded literal (the override no longer reaches go test) =="
sed -i "s/-timeout '\$API_TEST_TIMEOUT'/-timeout '30m'/g" "$RUNNER"
if assert_mutated "$RUNNER" "-timeout '30m'"; then
  argv="$(gate_argv 45m)"; budget="$(budget_of "$argv")"
  if [ -z "$budget" ] || [ "$budget" = "45m" ]; then
    bad "case 4 — asking for 45m produced [${budget:-no budget}] against a hardcoded literal, so the mutation is not the shape this case claims and it proves nothing. argv: [$argv]"
  else
    ok "case 4 — with the literal in place API_TEST_TIMEOUT=45m no longer reaches \`go test\` (it was handed -timeout $budget instead), so the override is dead and 17b goes RED"
  fi
fi
restore_and_verify

# --- case 5: THE CHEAP HALF, END TO END. Every case above reproduces the guard's
# own predicate; this one runs the REAL guard (the `selftest` gate) against the
# mutated runner and requires it to go RED, NAMING the missing budget — and, so
# the red is attributable, requires every failure it reports to be a budget
# failure. This is the case that proves the per-run gate is the thing that fires,
# not just this script's reading of it.
echo "== case 5: the REAL selftest gate against the mutated runner (end to end) =="
sed -i "s/-timeout '\$API_TEST_TIMEOUT'//g; s/ -timeout \$API_TEST_TIMEOUT//g" "$RUNNER"
mutated_budget="$(budget_of "$(gate_argv)")"
if [ -n "$mutated_budget" ]; then
  bad "the gate still hands \`go test\` -timeout $mutated_budget after the mutation — the case would have reported a false green"
else
  sel_rc=0
  ./scripts/preflight_test.sh >"$LOG" 2>&1 || sel_rc=$?
  fails="$(grep -E '^  FAIL ' "$LOG")"
  n_fail="$(grep -c '[^[:space:]]' <<<"$fails")"
  off_topic="$(grep -E '^  FAIL ' "$LOG" | grep -vE 'timeout|budget|API_TEST_TIMEOUT')"
  if [ "$sel_rc" -eq 0 ]; then
    bad "case 5 — the selftest gate stayed GREEN (rc=0) with the budget stripped from the runner, so the guard does not actually fire. Log: $LOG"
  elif [ -z "$fails" ]; then
    bad "case 5 — the selftest gate exited $sel_rc without a single FAIL line, so the red is unexplained. Log: $LOG"
  elif [ -n "$off_topic" ]; then
    bad "case 5 — the selftest gate went RED but for reasons OTHER than the budget, so the red cannot be attributed to this sabotage: $(tr '\n' '|' <<<"$off_topic")"
  else
    ok "case 5 — the REAL selftest gate went RED (rc=$sel_rc) on $n_fail budget failure(s), every one naming the -timeout budget: the per-run guard fires end to end"
    sed 's/^/        /' <<<"$fails" | cut -c1-190
  fi
fi
restore_and_verify

echo "---"
if [ "$FAIL" -eq 0 ]; then
  echo "biteproof: $PASS checks passed, 0 failed — the api test gate's -timeout budget BITES: it is present on a healthy tree, and removing it (from the line, from the executed command while the label keeps it, by disabling the alarm, or by hardcoding it) is caught by the real selftest gate"
else
  echo "biteproof: $PASS passed, $FAIL failed — the api test gate's budget does NOT hold something it claims to"
fi
[ "$FAIL" -eq 0 ] || exit 1
