#!/usr/bin/env bash
#
# scripts/biteproof_preflight_reuse.sh — prove the per-tree reuse guards BITE.
#
# THE GUARDS UNDER TEST. `scripts/preflight.sh` + `scripts/gate_result.sh` let a
# lane carry a recorded pass over instead of re-running a gate (the feature of
# card t_dbd93601; its rework is card t_2bed4f8f). Four properties are what make
# that safe, and each has a `selftest` check in scripts/preflight_test.sh:
#
#   31. The DOCUMENTED red-tree path (`--only <PREFLIGHT_RERUN> --reuse-record`)
#       must RUN. Judging the record's vouched set through the SELECTION first
#       emptied it and the run died with "vouches for NO gate" on the exact lane
#       shape the record exists for (measured: 148 of 206 repeated runs on this
#       host). Guard: the refusal is decided against the RECORD, before the filter.
#
#   32. ONE reuse event, ONE ledger line. The consumer (preflight_reuse.sh) and the
#       runner both observe the same reuse; when both wrote, `--report` — the
#       instrument the saving is read from — reported DOUBLE it (588s of real
#       saving read as 1176s, and every lane in the host ledger's window was
#       counted twice). Guard: gr_ledger suppresses the second write.
#
#   33. A record observed under a different NODE is still usable, and says so. On
#       this host node is a property of the SHELL (an interactive shell resolves
#       v24.19.0 via nvm; a worker lane resolves v26.7.0 from .hermes/tools), so
#       hashing it into the fingerprint refused records on IDENTICAL bytes and left
#       reuse dead for the one shape that runs the gates. Guard: node is recorded
#       and announced, but is not key material.
#
#   34. ONE DATABASE, ONE KEY. The test-DB host was hashed as the URL's HOST
#       STRING, so one Postgres reached as `127.0.0.1` and as `localhost` produced
#       two keys: the gate ran green on tree f98b5fcf and wrote
#       testdb=127.0.0.1:..., and the very next reader — same server, same
#       database, environment unchanged — was refused with "the environment
#       changed since that record", costing the lane the whole gate (the host-wide
#       flock whose waiter measures 50-86 minutes) to re-run gates that had
#       already passed on those bytes. Guard: the host is canonicalised to one
#       token for the loopback spellings only, and another port or another
#       database still yields a different key.
#
# WHY A SCRIPT AND NOT A PER-RUN GATE: every case mutates scripts/*.sh in place.
# The live `selftest` gate runs against the tree under test, so it can never
# sabotage its own subject; that is exactly why the per-run half is a property
# check and the sabotage lives here. Cases are minutes each because each one runs
# the REAL selftest end to end — the alternative is a second copy of the checks,
# which is a guard that can drift from the one it claims to back.
#
# Run:  ./scripts/biteproof_preflight_reuse.sh
# Exit: 0 if every case BITES, 1 otherwise.
#
# Each case mutates, runs the real selftest, requires it RED with every failure
# naming THAT case's property (so the red is attributable and not a sibling
# check), then restores the source byte-identically (sha256 re-verified). Do not
# run this while another lane is gating out of the same checkout: the tree is
# briefly dirty by design. `git status` is compared before/after instead of
# demanding a clean tree, because this harness runs on the branch that changes
# the same files.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }

RUNNER="scripts/preflight.sh"
RESULT="scripts/gate_result.sh"
WORK="$(mktemp -d /tmp/biteproof_preflight_reuse.XXXXXX)"

# This harness must never queue on (or hold) the production gate lock: it is not a
# gate run, and on a busy board it would delay every card waiting behind it. Same
# rule and reason as scripts/preflight_test.sh check 16.
export PREFLIGHT_LOCK_FILE="${PREFLIGHT_LOCK_FILE:-/tmp/biteproof_preflight_reuse.lock}"
# Private record dir: the cases must never write to the host's real records.
export PREFLIGHT_RESULT_DIR="$WORK/gate-results"
mkdir -p "$PREFLIGHT_RESULT_DIR"

for f in "$RUNNER" "$RESULT"; do
  [ -f "$f" ] || { echo "FAIL: $f not found — cannot prove anything about the guards" >&2; exit 2; }
done

cp "$RUNNER" "$WORK/preflight.sh.orig"
cp "$RESULT" "$WORK/gate_result.sh.orig"
WANT_RUNNER="$(sha256sum "$RUNNER" | awk '{print $1}')"
WANT_RESULT="$(sha256sum "$RESULT" | awk '{print $1}')"
# THE MODE IS PART OF THE SOURCE. A mutation written through a temp file (`awk >
# tmp; mv tmp src`) replaces the file and the exec bit is LOST — and the selftest
# refuses a preflight.sh that is not executable, so every case AFTER the first
# would report an unattributable red. Measured: exactly that, on this harness's
# first run. So the mode is recorded here and re-applied on every restore.
MODE_RUNNER="$(stat -c '%a' "$RUNNER")"
MODE_RESULT="$(stat -c '%a' "$RESULT")"

STATUS_BEFORE="$(git status --porcelain | sort)"
restore() {
  cp "$WORK/preflight.sh.orig" "$RUNNER"; chmod "$MODE_RUNNER" "$RUNNER"
  cp "$WORK/gate_result.sh.orig" "$RESULT"; chmod "$MODE_RESULT" "$RESULT"
}
cleanup() { restore; rm -rf "$WORK"; }
trap cleanup EXIT

# restore_and_verify — a case that leaves a source dirty would poison every lane
# after it, so the restore is asserted, not assumed.
restore_and_verify() {
  restore
  local a b; a="$(sha256sum "$RUNNER" | awk '{print $1}')"; b="$(sha256sum "$RESULT" | awk '{print $1}')"
  if [ "$a" = "$WANT_RUNNER" ] && [ "$b" = "$WANT_RESULT" ]; then
    ok "both sources restored byte-identically (preflight.sh ${a:0:12}, gate_result.sh ${b:0:12})"
  else
    bad "SOURCES WERE NOT RESTORED byte-identically (preflight.sh $a vs $WANT_RUNNER, gate_result.sh $b vs $WANT_RESULT) — restore them from $WORK BY HAND before doing anything else"
  fi
}

# assert_mutated — a mutation that never landed is a silent GREEN. `--` so a
# needle beginning with a dash is not read as a grep option.
assert_mutated() {
  local f="$1" needle="$2"
  if grep -qF -- "$needle" "$f"; then return 0; fi
  bad "$f was not mutated (looked for: $needle) — the case would have reported a false green"
  return 1
}

# selftest_summary <logfile> — print the tally line and the FAIL lines, and return
# the selftest's own exit status (0 = green).
selftest_run() {
  local log="$1" rc=0
  ./scripts/preflight_test.sh >"$log" 2>&1 || rc=$?
  return "$rc"
}

# verdict <case> <log> <rc> <needle-regex> <what the needle means>
# A case BITES only when the selftest went RED *and* every failure names this
# case's property: an unattributable red (a crash, a sibling check breaking)
# would otherwise be counted as proof.
verdict() {
  local name="$1" log="$2" rc="$3" needle="$4" why="$5"
  local fails off_topic n_fail
  fails="$(grep -E '^  FAIL ' "$log")"
  n_fail="$(grep -c '[^[:space:]]' <<<"$fails")"
  off_topic="$(grep -E '^  FAIL ' "$log" | grep -vE -- "$needle")"
  if [ "$rc" -eq 0 ]; then
    bad "$name — the selftest stayed GREEN (rc=0) after the sabotage, so the guard does not actually fire. Log: $log"
  elif [ -z "$fails" ]; then
    bad "$name — the selftest exited $rc with NO FAIL line, so the red is unexplained. Log: $log"
  elif [ -n "$off_topic" ]; then
    bad "$name — the selftest went RED but for reasons OTHER than $why, so the red cannot be attributed to this sabotage: $(tr '\n' '|' <<<"$off_topic")"
  else
    ok "$name — $why: the REAL selftest went RED (rc=$rc) on $n_fail failure(s), every one naming it"
    sed 's/^/        /' <<<"$fails" | cut -c1-190
  fi
}

echo "biteproof: the per-tree gate result record's four safety guards"
echo "  repo: $(pwd)"
echo "  head: $(git rev-parse --short HEAD)"
echo "  srcs: $RUNNER (sha256 ${WANT_RUNNER:0:12}), $RESULT (sha256 ${WANT_RESULT:0:12})"
echo "---"

# --- case 0: THE CONTROL. The healthy tree must pass the whole selftest, so a red
# in the cases below is the sabotage and not this tree being broken. A guard that
# always fails is as useless as one that never fires, and the card requires both
# directions.
echo "== case 0: the healthy tree (control — the whole selftest must be green) =="
rc0=0
selftest_run "$WORK/case0.log" || rc0=$?
if [ "$rc0" -eq 0 ]; then
  ok "case 0 — the unmutated tree passes the whole selftest ($(grep -E '^preflight self-test:' "$WORK/case0.log" | tail -n 1)): the checks 31/32/33/34 assertions are reachable and green, so the cases below are not vacuous"
else
  bad "case 0 — the unmutated tree ALREADY fails the selftest (rc=$rc0), so no case below could attribute a red to its sabotage: $(grep -E '^  FAIL ' "$WORK/case0.log" | tr '\n' '|')"
fi

# --- case 1: DEFECT 1 REVERTED. The pre-fix shape: the record's vouched set is
# counted and judged AFTER the selection filter, so a record whose only `fail` is
# the selected gate has every recorded `pass` discarded and the run dies with
# "vouches for NO gate" — rc=1, no ledger line, on the documented path. The move
# is done with awk so it is a real relocation, not a comment: the count and the
# refusal block are lifted out of their pre-filter home and re-emitted after the
# filter, byte-for-byte.
echo "== case 1: the refusal moved back AFTER the selection filter (the pre-fix shape) =="
cat > "$WORK/move.awk" <<'AWKMOVE'
# Reproduce the pre-fix ordering: count + refuse AFTER the selection filter.
# Exact whole-line comparison (not a regex): the lines carry ${...} and [@], which
# a pattern would have to escape, and getting that wrong turns the case into a
# silent no-op that reports a false green.
BEGIN { c = "  _reuse_vouched=${#_reuse_ok[@]}"
        d = "  if [ \"$_reuse_vouched\" -eq 0 ]; then"
        f = "  _reuse_ok=(\"${_reuse_sel[@]}\")"
        indrop = 0 }
{
  if ($0 == c) next
  if ($0 == d) { indrop = 1; next }
  if (indrop) { if ($0 == "  fi") indrop = 0; next }
  print
  if ($0 == f) {
    print "  _reuse_vouched=${#_reuse_ok[@]}"
    print "  if [ \"$_reuse_vouched\" -eq 0 ]; then"
    print "    die \"--reuse-record was requested but the record for tree $GR_TREE_SHORT vouches for NO gate ($_reuse_reason) \xe2\x80\x94 refusing to run as if it did\""
    print "  fi"
  }
}
AWKMOVE
awk -f "$WORK/move.awk" "$RUNNER" > "$WORK/preflight.mutated" && cp "$WORK/preflight.mutated" "$RUNNER" && chmod "$MODE_RUNNER" "$RUNNER"
# The mutation is asserted in BOTH halves — gone from before the filter, present
# after it — and in order, because a mutation that never landed is a silent green.
if [ "$(grep -cF '_reuse_vouched=${#_reuse_ok[@]}' "$RUNNER")" != "1" ]; then
  bad "case 1 — the count line did not land exactly once (got $(grep -cF '_reuse_vouched=${#_reuse_ok[@]}' "$RUNNER")): the mutation is not the shape it claims"
elif [ "$(awk '/_reuse_vouched=\$\{#_reuse_ok\[@\]\}$/{c=NR} /^  _reuse_ok=\("\\$\{_reuse_sel\[@\]\}"\)$/{f=NR} END{print (c>f)?"after":"before"}' "$RUNNER")" != "after" ]; then
  bad "case 1 — the count line is still BEFORE the filter, so the pre-fix ordering was not reproduced and this case proves nothing"
elif ! bash -n "$RUNNER"; then
  bad "case 1 — the mutation left preflight.sh syntactically invalid, so any red below would be a parse error rather than the ordering under test"
else
  rc1=0
  selftest_run "$WORK/case1.log" || rc1=$?
  verdict "case 1" "$WORK/case1.log" "$rc1" "vouches for NO gate|documented red-tree path" \
    "with the refusal judged on the filtered set, the documented red-tree path dies with 'vouches for NO gate'"
fi
restore_and_verify

# --- case 2: THE LEDGER DEDUPE DISABLED. Every event is written, so the consumer
# and the runner each add a line for the SAME reuse and `--report` — the instrument
# the measured saving is read from — reports double. One line, one window.
echo "== case 2: the ledger dedupe window forced to 0 (every write lands) =="
sed -i 's|local window="${PREFLIGHT_LEDGER_DEDUPE_WINDOW:-[0-9]*}"|local window="0"|' "$RESULT"
chmod "$MODE_RESULT" "$RESULT"
if assert_mutated "$RESULT" 'local window="0"'; then
  rc2=0
  selftest_run "$WORK/case2.log" || rc2=$?
  verdict "case 2" "$WORK/case2.log" "$rc2" "consumer-then-runner|ledger" \
    "with the dedupe off, the documented consumer-then-runner path ledgers the same event twice and --report doubles the saving"
fi
restore_and_verify

# --- case 3: NODE PUT BACK INTO THE KEY MATERIAL. The pre-fix shape: the key
# MATERIAL carries node, so a record written from an interactive shell is refused
# by a worker lane on byte-identical trees — reuse dead for the shape that runs the
# gates.
echo "== case 3: node hashed back into the fingerprint (node key MATERIAL again) =="
# The mutation must be ONE-SIDED. A first attempt swapped gr_key's body to
# `gr_key_parts` — but the reader (`gr_record_usable`) calls gr_key too, so writer
# and reader moved TOGETHER and the sabotage self-cancelled: the selftest stayed
# green (measured). The real pre-fix shape is that the MATERIAL carries node, so
# the material itself is what changes here: the original is renamed and a wrapper
# appends `;node=`, exactly as gr_key_parts does — and because gr_key hashes the
# material, the fingerprint moves for the recorded node without the reader knowing.
sed -i 's/^gr_key_material() {/gr_key_material_orig() {/' "$RESULT"
{
  printf '\n'
  printf '# PRE-FIX SHAPE (sabotage): node is key MATERIAL again — see gr_key_material.\n'
  printf 'gr_key_material() { local r="${1:-$PWD}"; printf "%%s;node=%%s" "$(gr_key_material_orig "$r")" "$(gr_node_version)"; }\n'
} >> "$RESULT"
if ! assert_mutated "$RESULT" 'gr_key_material_orig() {'; then
  :
elif ! assert_mutated "$RESULT" 'node=%s" "$(gr_key_material_orig'; then
  :
elif ! bash -n "$RESULT"; then
  bad "case 3 — the mutation left gate_result.sh syntactically invalid, so any red below would be a parse error rather than the fingerprint under test"
elif ! ( . "$RESULT"; case "$(gr_key_material "$REPO_ROOT")" in *node=*) exit 0 ;; *) exit 1 ;; esac ); then
  bad "case 3 — the mutated material still does NOT carry node, so node was never put back into the fingerprint and this case proves nothing"
else
  rc3=0
  selftest_run "$WORK/case3.log" || rc3=$?
  verdict "case 3" "$WORK/case3.log" "$rc3" "node" \
    "with node back in the key material, a differing node refuses the record instead of being announced as an advisory"
fi
restore_and_verify

# --- case 4: THE TEST-DB HOST SPELLING PUT BACK INTO THE KEY. The reported defect
# (card t_7add0cfa): the fingerprint hashed the URL's HOST STRING verbatim, so one
# server written as `127.0.0.1` and as `localhost` produced two keys. Measured on
# this host: the gate ran green on tree f98b5fcf and wrote testdb=127.0.0.1:...,
# and the very next reader — same server, same database, environment unchanged —
# was refused with "the environment changed since that record", costing the lane
# the whole gate (the host-wide flock's waiter has been measured at 50-86 min) to
# re-run fifteen gates that had already passed on those bytes.
#
# The mutation restores the pre-fix shape one-sidedly, the same way case 3 does:
# the fixed component function is renamed and a wrapper re-emits the RAW
# scheme/credentials/query-stripped string, so the two keys diverge again without
# the reader knowing. The mutation is asserted to have landed BOTH ways — the
# wrapper is present AND the two spellings then disagree — because a mutation that
# never landed is a silent green.
echo "== case 4: the DB host spelling hashed back into the key (the pre-fix shape) =="
sed -i 's/^gr_testdb_component() {/gr_testdb_component_canon() {/' "$RESULT"
# A QUOTED heredoc so the sed expression reaches the file unescaped — the first
# attempt built it from nested printf escapes and produced a sed that no longer
# stripped the query string. The appended body is the pre-fix shape verbatim.
cat >> "$RESULT" <<'EOMUT'

# PRE-FIX SHAPE (sabotage): the host STRING is key material again — see gr_testdb_component.
gr_testdb_component() {
  local url="${JOMFORM_TEST_DATABASE_URL:-}"
  if [ -z "$url" ]; then printf 'unset'; return 0; fi
  printf '%s' "$url" \
    | sed -E 's#^[A-Za-z0-9+.-]+://##; s#^[^@/]*@##; s#\?.*$##; s#/$##'
}
EOMUT
if ! assert_mutated "$RESULT" 'the host STRING is key material again'; then
  :
elif [ "$(grep -cE '^gr_testdb_component\(\) \{' "$RESULT")" != "1" ] || \
     [ "$(grep -cE '^gr_testdb_component_canon\(\) \{' "$RESULT")" != "1" ]; then
  bad "case 4 — the mutation is not the shape it claims (canon=$(grep -cE '^gr_testdb_component_canon\(\) \{' "$RESULT"), raw=$(grep -cE '^gr_testdb_component\(\) \{' "$RESULT") definitions): a rename that missed or an append that failed would leave the FIXED function in place and report a false green"
elif ! bash -n "$RESULT"; then
  bad "case 4 — the mutation left gate_result.sh syntactically invalid, so any red below would be a parse error rather than the fingerprint under test"
elif ! ( . "$RESULT"
        a="$(JOMFORM_TEST_DATABASE_URL='postgres://u:p@127.0.0.1:5432/jomform_test?sslmode=disable' gr_testdb_component)"
        b="$(JOMFORM_TEST_DATABASE_URL='postgres://u:p@localhost:5432/jomform_test?sslmode=disable' gr_testdb_component)"
        [ "$a" != "$b" ] ); then
  bad "case 4 — the mutated component still answers the SAME for both loopback spellings, so the spelling was never put back into the key and this case proves nothing"
else
  rc4=0
  selftest_run "$WORK/case4.log" || rc4=$?
  verdict "case 4" "$WORK/case4.log" "$rc4" "one server, two keys|still carries the caller's SPELLING|the SAME server and database spelled the other way was NOT reused" \
    "with the host spelling back in the key, one server yields two keys and the record is refused across them"
fi
restore_and_verify

# --- the tree must be as dirty as it was found: a case that leaked a mutation past
# its restore would poison every lane after it, and the sha checks above cannot see
# an ADDED file.
STATUS_AFTER="$(git status --porcelain | sort)"
if [ "$STATUS_BEFORE" = "$STATUS_AFTER" ]; then
  ok "the tree's git status is unchanged by the run (this harness added nothing)"
else
  bad "git status CHANGED across the run — something was left behind:
$(diff <(printf '%s\n' "$STATUS_BEFORE") <(printf '%s\n' "$STATUS_AFTER") | sed 's/^/        /')"
fi

echo "---"
if [ "$FAIL" -eq 0 ]; then
  echo "biteproof: $PASS checks passed, 0 failed — the reuse guards BITE: the healthy tree passes, and moving the refusal back after the selection filter, disabling the ledger dedupe, hashing node into the fingerprint, or hashing the test-DB host's SPELLING into it each turns the REAL selftest gate red, naming that property"
else
  echo "biteproof: $PASS passed, $FAIL failed — a reuse guard does NOT hold something it claims to"
fi
[ "$FAIL" -eq 0 ] || exit 1
