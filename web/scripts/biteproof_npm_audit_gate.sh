#!/usr/bin/env bash
# Bite proof for web/src/lib/npmAuditGate.test.ts — the guard that pins
# `web/scripts/npm-audit-gate.mjs` (the `npmaudit` gate's command) FAILING CLOSED.
#
# A guard that only ever passes is a tautology, and this one guards the gate that
# sits MID-LIST in scripts/preflight.sh — the worst place for a silent pass,
# because a green here hides every gate after it (that is why PR 1025 existed).
#
# WHAT IT RE-CREATES, ONE MUTATION AT A TIME
#   M1  the PRE-FIX VERDICT, byte for byte: `reconcile()` removed from the
#       verdict and the unexcepted/excused/stale sets derived from the RAW parsed
#       advisories (no severity scoping). This is the QA-reproduced fail-open:
#       a report with `vulnerabilities:{}` and `metadata.high = 5` at rc=1 made
#       the gate print PASS.
#   M2  THE SAME FILE'S SECOND HALF, restored on its own: only the unexcepted set
#       is derived from the raw advisories while `reconcile()` stays — so the
#       moderate-only PASS case and the severity scoping are what this mutation
#       moves, and the reconciliation cases still bite. It exists because M1
#       moves every case at once and cannot tell the two halves apart.
#   M3  the printNpmReport() `try`/`catch` REMOVED — the pre-fix shape whose RED
#       path printed a raw Node stack instead of npm's report (card part 4). The
#       guard's own stack assertion must be what catches it.
#   M4  THE DISCRIMINATION CASE, and its own control: the `via` WALK reduced to
#       one level (names pushed from the chain are not followed on). The
#       "one judgeable advisory beside one untraceable package" case must go RED
#       while the "everything is untraceable" case stays caught — i.e. the walk
#       really adds reachability, it is not just an existence test.
#
# The guard drives the SHIPPED `web/scripts/npm-audit-gate.mjs` (it reads the
# live bytes into a sandbox per case) and needs only node builtins, so this
# harness needs no `npm ci` and no network — it runs from any checkout.
#
# Run:  bash web/scripts/biteproof_npm_audit_gate.sh
set -uo pipefail
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }

# The shared runner gate (web/scripts/lib/node_test_verdict.sh): a node that cannot run
# the guard is a REFUSAL that names itself, never a verdict (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no capability gate to prove the runner can run this guard." >&2
  exit 2
}
cd "$BITE_HOME/.." || exit 2

GATE=scripts/npm-audit-gate.mjs
TEST=src/lib/npmAuditGate.test.ts
# CAPABILITY, not presence: `node --version` succeeding proves nothing (v18.19.1 answers
# it and then rejects `--disable-warning`). This harness asks for the TAP reporter
# explicitly, so the probe does too.
nt_require_node tap
[ -f "$GATE" ] || { echo "REFUSING: $GATE not found"; exit 2; }
[ -f "$TEST" ] || { echo "REFUSING: $TEST not found"; exit 2; }

sha_of() { sha256sum "$1" | cut -d' ' -f1; }
GATE_SHA_BEFORE="$(sha_of "$GATE")"
BACKUP="$(mktemp "${TMPDIR:-/tmp}/npm-audit-gate.bite.XXXXXX")"
trap 'cp "$BACKUP" "$GATE"; rm -f "$BACKUP"' EXIT
cp "$GATE" "$BACKUP"

fail=0

# run_case <name> <expected-needle> [test-name-substring]
# Each case runs ONLY the new arm, with the TAP reporter so failing case NAMES are
# readable (the default reporter prints bare stack traces).
run_case() {
  local name="$1" expect="$2" only="${3:-}"
  local args=(--test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON --test-reporter=tap)
  [ -n "$only" ] && args+=(--test-name-pattern="$only")
  local out rc
  out="$(node "${args[@]}" "$TEST" 2>&1)"; rc=$?
  if ! grep -qE "^(ok|not ok) [0-9]+" <<<"$out"; then
    echo "REFUSED  $name  *** the test runner produced no case results, so this case proves nothing ***"
    printf '%s\n' "$out" | sed -n '1,6p' | sed 's/^/         /'
    fail=1; return
  fi
  if [ "$rc" -eq 0 ]; then
    echo "GREEN    $name  *** MUTATION NOT DETECTED — the guard does not bite ***"
    fail=1; return
  fi
  if grep -qF "$expect" <<<"$out"; then
    echo "BITES    $name  (guard failed as required, naming: $expect)"
  else
    echo "BITES?   $name  (guard failed but not for the expected reason)"
    grep -E "^not ok" <<<"$out" | sed -n '1,6p' | sed 's/^/         /'
    fail=1
  fi
}

restore_gate() {
  cp "$BACKUP" "$GATE"
  local now; now="$(sha_of "$GATE")"
  if [ "$now" != "$GATE_SHA_BEFORE" ]; then
    echo "RESTORE MISMATCH $GATE (want $GATE_SHA_BEFORE, got $now)"
    fail=1
  fi
}

echo "== case 0 (CONTROL): the unmutated gate — the guard must be GREEN =="
control_out="$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$TEST" 2>&1)"; control_rc=$?
if [ "$control_rc" -ne 0 ]; then
  echo "REFUSED  control  *** the guard is red on the UNMUTATED gate, so a red below proves nothing ***"
  printf '%s\n' "$control_out" | grep -E "tests |pass |fail |not ok" | sed -n '1,8p' | sed 's/^/         /'
  fail=1
else
  echo "GREEN    control  (unmutated gate: $(grep -oE 'pass [0-9]+' <<<"$control_out" | head -1))"
fi

echo "== case 1: the PRE-FIX VERDICT restored (reconcile() out, raw-advisory sets back) =="
python3 - "$GATE" <<'PY' || { echo "the mutation did not apply"; exit 2; }
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text(); o = s
s = s.replace("const notJudgeable = reconcile();", "const notJudgeable = [];")
s = s.replace("const unexcepted = [...judgeable.entries()]", "const unexcepted = [...advisories.entries()]")
s = s.replace("const excused = [...judgeable.entries()]", "const excused = [...advisories.entries()]")
s = s.replace("const stale = [...exceptions.keys()].filter((url) => !judgeable.has(url));",
              "const stale = [...exceptions.keys()].filter((url) => !advisories.has(url));")
assert s != o, "no replacement landed"
p.write_text(s)
PY
run_case "pre-fix verdict restored" "the fail-open shape must be RED"
restore_gate

echo "== case 2: only the RAW-advisory unexcepted set restored (reconcile() kept) =="
python3 - "$GATE" <<'PY' || { echo "the mutation did not apply"; exit 2; }
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text(); o = s
s = s.replace("const unexcepted = [...judgeable.entries()]", "const unexcepted = [...advisories.entries()]")
assert s != o, "no replacement landed"
p.write_text(s)
PY
run_case "raw-advisory unexcepted set" "a moderate-only report PASSes"
restore_gate

echo "== case 3: printNpmReport()'s try/catch removed (the raw-stack RED path) =="
python3 - "$GATE" <<'PY' || { echo "the mutation did not apply"; exit 2; }
import pathlib, re, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text(); o = s
start = s.index("  let plain = \"\";")
end = s.index("  if (plain.trim()) {", start)
new = ("  const plain = execFileSync(\"npm\", [\"audit\", \"--audit-level=high\"], {\n"
       "    cwd: WEB_DIR, encoding: \"utf8\", stdio: [\"ignore\", \"pipe\", \"pipe\"],\n"
       "  });\n")
assert start != -1 and end > start, "anchors not found"
s = s[:start] + new + s[end:]
assert s != o
p.write_text(s)
PY
run_case "printNpmReport try/catch removed" "not a raw Node stack" "unexcepted advisory still fails"
restore_gate

echo "== case 4: the via WALK reduced to one level (no transitive reachability) =="
python3 - "$GATE" <<'PY' || { echo "the mutation did not apply"; exit 2; }
import pathlib, sys
p = pathlib.Path(sys.argv[1]); s = p.read_text(); o = s
s = s.replace(
    "      if (typeof via === \"string\" && via) queue.push(via);",
    "      // MUTATION: the propagation chain is no longer followed.",
)
assert s != o, "no replacement landed"
p.write_text(s)
PY
run_case "via walk reduced to one level" "a TRANSITIVE chain"
restore_gate

if [ "$fail" -ne 0 ]; then echo "BITE PROOF FAILED"; exit 1; fi
echo "all 4 mutations bite; $GATE restored byte-identical (sha256 $GATE_SHA_BEFORE)"
