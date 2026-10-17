#!/usr/bin/env bash
# scripts/biteproof_a11y_port_claim.sh — does the web-server gates' PORT CLAIM bite?
# (card t_df1719c1)
#
# THE DEFECT THIS INSTRUMENT PINS (measured on the merge lane of PR 1203, 2026-10-13):
# the a11y and e2e gates each start the app's own production server, and the port was
# "reserved" with `nc -l -p <port>` and RELEASED immediately before `A11Y_PORT=<port>`
# was handed to npm — reserve -> release -> bind. `next start` does not listen until it
# has booted, so the window between the release and the bind is seconds wide, and this
# host runs up to PREFLIGHT_SLOT_COUNT (2) gate runs at once. A second gate probing
# inside that window found the port free and took it too; the loser cannot fall back
# (`reuseExistingServer` is false BY DESIGN) and died with
#
#     [WebServer] Error: listen EADDRINUSE: address already in use :::3100
#
# THE FIX (scripts/preflight.sh): an exclusive per-port `flock` claim, held open until
# the gate's server has exited, plus ONE LABELLED retry on EADDRINUSE for a holder the
# claim cannot exclude.
#
# WHY THE INTERLEAVING IS A PARAMETER OF THIS HARNESS (STUB_STAGGER). The defect is a
# race, and the pre-fix code survives some interleavings and not others — which is the
# whole reason it was found by a merge lane and not by a test. Its `nc` reservation is
# released 0.3s in and the server binds seconds later, so a peer that probes the port
# BETWEEN those two moments takes a port the first gate is about to bind. A peer that
# probes EARLIER hits the `nc` and correctly moves on; one that probes LATER finds a
# listening server and moves on too. So the repro has to place the second run inside
# that window, and this harness says so, prints the margin it achieved, and runs the
# TREE at the very same interleaving (CASE 3) — which is what makes CASE 2's red
# attributable to the BYTES (base) rather than to the stagger.
#
# WHAT IT DRIVES, AND WHAT IT STUBS — stated so the evidence is not over-read:
#   * the SHIPPED `scripts/preflight.sh`, real argument parsing, the real slot pool, the
#     real `a11y` gate body: `./scripts/preflight.sh --only a11y`, twice, concurrently;
#   * a STUB `npm`, first on PATH. It stands in for `next start`: it binds the
#     `A11Y_PORT` it was handed after STUB_DELAY (default 3s — `next start` does not
#     listen for seconds either) and holds it for STUB_HOLD. THE PORT IS THE SUBJECT
#     HERE, the app is not: playwright and the app are out of scope, and the live
#     two-worktree run of the real suite is the card's other piece of evidence;
#   * a FABRICATED `web/node_modules` (every package `web/package.json` declares, each
#     with a manifest and an entry point) plus `.bin/next` / `.bin/playwright`, so the
#     runner's own prerequisite checks are satisfied rather than skipped. A skipped gate
#     reads as a green one.
#
# THE CASES (each one is a required property; a case that cannot be shown is a REFUSAL
# with a non-zero exit at the end, never a pass):
#   1. TREE, two SIMULTANEOUS runs of EACH web-server gate (`a11y` and `e2e`): both exit 0,
#      the two runs claim DIFFERENT ports, and each log names the claim file it held.
#   2. BASE (the pre-fix reserve code, extracted from BITEPROOF_BASE, default
#      origin/main), the staggered interleaving: BOTH runs must aim at the SAME port and
#      at least one must FAIL with EADDRINUSE. A base run that happens to pick different
#      ports is a REFUSAL (the window was missed), never a pass.
#   3. TREE at the SAME staggered interleaving as case 2: both exit 0, ports differ.
#      Without this leg a red in case 2 could be blamed on the stagger.
#   4. NEGATIVE CONTROL, a FOREIGN listener holding the first free candidate: the gate
#      must claim a DIFFERENT port, still exit 0, and leave the foreign listener alive
#      and holding its port (`reuseExistingServer` stays false — a server this tree did
#      not start may be serving bytes this tree never built).
#   5. RESIDUE, a holder that takes the claimed port between the claim and the bind (the
#      stub squats on it on purpose): the gate must print its LABELLED retry, name both
#      ports, and still exit 0.
#
# OFFLINE, no network, no web/node_modules, no build, ~60s. It mutates NOTHING in the
# repo: everything happens in a throwaway sandbox whose `scripts/` copy is the only
# thing written, and the base leg overwrites that copy.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
BASE_REF="${BITEPROOF_BASE:-origin/main}"
STUB_DELAY="${STUB_DELAY:-3}"
# The server must outlive the stagger, or the two stubs bind the port one after the
# other and the collision never happens (measured while writing this: at a 1s hold and a
# 1.2s stagger the second bind landed after the first had exited, both runs green).
# A real `next start` holds for the whole gate, which is minutes.
STUB_HOLD="${STUB_HOLD:-6}"
# The pre-fix `nc` reservation lives 0.3s (a literal `sleep 0.3` in every revision of
# the reserve code); the danger window is (0.3, STUB_DELAY). The default sits inside it.
STUB_STAGGER="${STUB_STAGGER:-1.2}"
NC_RELEASE=0.3

command -v python3 >/dev/null 2>&1 || { echo "REFUSING: python3 is needed for the stub server" >&2; exit 2; }
command -v flock   >/dev/null 2>&1 || { echo "REFUSING: flock is needed (the pool and the port claim both use it)" >&2; exit 2; }
command -v node    >/dev/null 2>&1 || { echo "REFUSING: node is needed to fabricate the sandbox's node_modules" >&2; exit 2; }
command -v nc      >/dev/null 2>&1 || { echo "REFUSING: nc is needed (the pre-fix reserve code uses it)" >&2; exit 2; }

# The stagger must sit INSIDE the pre-fix window, or the base leg would prove nothing
# about the base. Checked here rather than left to the reader.
if ! python3 -c "import sys;a=float(sys.argv[1]);d=float(sys.argv[2]);r=float(sys.argv[3]);sys.exit(0 if r < a < d else 1)" "$STUB_STAGGER" "$STUB_DELAY" "$NC_RELEASE"; then
  echo "REFUSING: STUB_STAGGER=$STUB_STAGGER is outside the pre-fix window ($NC_RELEASE, $STUB_DELAY) — the base leg would not be exercising the defect" >&2
  exit 2
fi

WORK="$(mktemp -d /tmp/a11y-port-claim.XXXXXX)"
STATE="$(mktemp -d /tmp/a11y-port-claim-state.XXXXXX)"
cleanup() { rm -rf "$WORK" "$STATE"; }
trap cleanup EXIT

FAILS=0
BITES=0
ok()  { echo "  ok   - $1"; BITES=$((BITES+1)); }
bad() { echo "  FAIL - $1" >&2; FAILS=$((FAILS+1)); }
refuse() { echo "  REFUSED - $1" >&2; FAILS=$((FAILS+1)); }

# ---------------------------------------------------------------------------
# The sandbox: a git repo holding the runner plus the few files a11y's
# prerequisite checks read, and NOTHING that can run a real build.
# ---------------------------------------------------------------------------
mkdir -p "$WORK/scripts" "$WORK/web/scripts" "$WORK/bin"
cp "$HERE"/*.sh "$WORK/scripts/" 2>/dev/null
cp "$REPO/web/package.json" "$WORK/web/package.json"
cp "$REPO/web/scripts/web-deps-check.mjs" "$WORK/web/scripts/" 2>/dev/null
cp "$REPO/AGENTS.md" "$WORK/AGENTS.md" 2>/dev/null
cp "$REPO/GATE-INVENTORY.md" "$WORK/GATE-INVENTORY.md" 2>/dev/null
chmod +x "$WORK/scripts/preflight.sh"

# Fabricate the declared packages so `web-deps-check.mjs` reads a real install instead
# of a refusal. It judges PRESENCE (a manifest per declared package) and PAYLOAD (the
# entry point that manifest declares is reachable on disk), so each package gets both.
node -e '
const fs=require("fs"),path=require("path");
const w=process.argv[1];
const pkg=JSON.parse(fs.readFileSync(path.join(w,"web/package.json"),"utf8"));
const names=Object.keys({...(pkg.dependencies||{}),...(pkg.devDependencies||{})});
for(const n of names){
  const d=path.join(w,"web/node_modules",n);
  fs.mkdirSync(d,{recursive:true});
  fs.writeFileSync(path.join(d,"package.json"),JSON.stringify({name:n,version:"0.0.0",main:"index.js"}));
  fs.writeFileSync(path.join(d,"index.js"),"module.exports = {};\n");
}
const bin=path.join(w,"web/node_modules/.bin");
fs.mkdirSync(bin,{recursive:true});
for(const b of ["next","playwright"]){
  fs.writeFileSync(path.join(bin,b),"#!/bin/sh\nexit 0\n");
  fs.chmodSync(path.join(bin,b),0o755);
}
console.log(`  ok   - sandbox: ${names.length} declared package(s) fabricated`);
' "$WORK" || { echo "REFUSING: could not fabricate the sandbox install" >&2; exit 2; }

# The STUB npm. It records the timestamp at which the gate handed it a port — the delta
# between the two runs' first hand-off is the concurrency MARGIN this run achieved, and
# it is printed so a refused BASE case can be diagnosed instead of guessed at.
cat > "$WORK/bin/npm" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\t%s\t%s\n' "$(date +%s.%N)" "$$" "$*" >> "${STUB_LOG:-/dev/null}"
case "$*" in
  *"run a11y"*|*"run e2e"*)
    port="${A11Y_PORT:-}"
    [ -n "$port" ] || { echo "stub npm: A11Y_PORT is unset — the gate must hand one over" >&2; exit 9; }
    echo "stub-npm: serving on port $port (A11Y_PORT=$port)"
    if [ "${STUB_SQUAT_FIRST:-0}" = 1 ] && [ ! -e "${STUB_SQUAT_MARK:-/nonexistent}" ]; then
      : > "${STUB_SQUAT_MARK}"
      echo "stub-npm: SQUATTING on ${STUB_SQUAT_PORT:-$port} (a holder the claim cannot exclude)"
      ( python3 - "${STUB_SQUAT_PORT:-$port}" "${STUB_SQUAT_HOLD:-15}" >/dev/null 2>&1 <<'SQPY'
import socket, sys, time
port, hold = int(sys.argv[1]), float(sys.argv[2])
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", port)); s.listen(1)
time.sleep(hold)
SQPY
      ) &
    fi
    sleep "${STUB_DELAY:-3}"
    python3 - "$port" "${STUB_HOLD:-1}" <<'PY'
import socket, sys, time
port, hold = int(sys.argv[1]), float(sys.argv[2])
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    s.bind(("0.0.0.0", port))
except OSError:
    print(f"Error: listen EADDRINUSE: address already in use :::{port}", file=sys.stderr)
    sys.exit(1)
s.listen(1)
time.sleep(hold)
PY
    ;;
  *)
    echo "stub npm: this harness only drives the web-server gates; got: $*" >&2
    exit 9
    ;;
esac
STUB
chmod +x "$WORK/bin/npm"

git -C "$WORK" init -q .
git -C "$WORK" add -A
git -C "$WORK" -c user.email=t@t -c user.name=t commit -q -m init

# The base bytes: the pre-fix reserve code, from the revision the caller names.
if ! git -C "$REPO" show "$BASE_REF:scripts/preflight.sh" > "$STATE/base-preflight.sh" 2>/dev/null; then
  echo "REFUSING: cannot read $BASE_REF:scripts/preflight.sh" >&2; exit 2
fi
[ -s "$STATE/base-preflight.sh" ] || { echo "REFUSING: $BASE_REF:scripts/preflight.sh is empty" >&2; exit 2; }
if cmp -s "$STATE/base-preflight.sh" "$WORK/scripts/preflight.sh"; then
  echo "REFUSING: the base revision's preflight.sh is byte-identical to this tree's — there is nothing to bite" >&2
  exit 2
fi
grep -q 'a11y_port_reserve' "$STATE/base-preflight.sh" || {
  echo "REFUSING: $BASE_REF:scripts/preflight.sh does not carry the pre-fix reserve code" >&2; exit 2; }
grep -q 'a11y_port_claim' "$WORK/scripts/preflight.sh" || {
  echo "REFUSING: this tree's scripts/preflight.sh does not carry the port claim" >&2; exit 2; }

# ---------------------------------------------------------------------------
# The driver: two `--only a11y` runs in the sandbox, the second started STUB_STAGGER
# seconds after the first (0 = simultaneous).
# ---------------------------------------------------------------------------
pair() {  # $1 = label, $2 = stagger seconds, $3 = gate (a11y|e2e, default a11y)
  local label="$1" stagger="$2" gate="${3:-a11y}" i
  for i in 1 2; do
    : > "$STATE/$label-$i.npm"
    # A PER-RUN record store: with one shared store a run could REUSE a peer's recorded
    # a11y pass (same sandbox tree = same key) and never start a server at all — a green
    # that proves nothing about ports. Per-run stores make reuse impossible here.
    (
      cd "$WORK" || exit 1
      if [ "$i" = 2 ] && python3 -c "import sys;sys.exit(0 if float(sys.argv[1])>0 else 1)" "$stagger"; then sleep "$stagger"; fi
      env PATH="$WORK/bin:$PATH" \
        PREFLIGHT_LOCK_FILE="$STATE/$label.lock" \
        PREFLIGHT_RESULT_DIR="$STATE/gate-results-$label-$i" \
        PREFLIGHT_RESULT_TTL=3600 \
        STUB_LOG="$STATE/$label-$i.npm" STUB_DELAY="$STUB_DELAY" STUB_HOLD="$STUB_HOLD" \
        ./scripts/preflight.sh --only "$gate" >"$STATE/$label-$i.log" 2>&1
      echo "EXIT=$?" >>"$STATE/$label-$i.log"
    ) &
  done
  wait
}

port_of() { grep -oE "on port [0-9]+" "$1" | head -1 | grep -oE "[0-9]+"; }
first_stub_ts() { [ -s "$1" ] && head -1 "$1" | cut -f1 || echo ""; }
margin() {  # $1,$2 = the two stub logs -> the seconds between the two hand-offs
  local a b
  a="$(first_stub_ts "$1")"; b="$(first_stub_ts "$2")"
  if [ -n "$a" ] && [ -n "$b" ]; then
    python3 -c "import sys;print(f'{abs(float(sys.argv[1])-float(sys.argv[2])):.3f}')" "$a" "$b"
  else
    echo "?"
  fi
}
verdict() {  # $1 = label, $2 = case name
  local label="$1" name="$2" p1 p2 e1 e2
  p1="$(port_of "$STATE/$label-1.log")"; p2="$(port_of "$STATE/$label-2.log")"
  e1="$(grep -o 'EXIT=[0-9]*' "$STATE/$label-1.log" | tail -1)"
  e2="$(grep -o 'EXIT=[0-9]*' "$STATE/$label-2.log" | tail -1)"
  echo "   $name: run1 port=${p1:-none} $e1 | run2 port=${p2:-none} $e2 | hand-off margin=$(margin "$STATE/$label-1.npm" "$STATE/$label-2.npm")s"
  grep -hE "starting the app's own server|RETRY|EADDRINUSE" "$STATE/$label-1.log" "$STATE/$label-2.log" | sed 's/^/     /'
}

echo "== sandbox: $WORK"
echo "== base ref: $BASE_REF  (pre-fix window: nc release ${NC_RELEASE}s .. server bind ${STUB_DELAY}s; stagger ${STUB_STAGGER}s)"

# ---------------------------------------------------------------------------
# CASE 1 — the TREE, two SIMULTANEOUS runs of EACH web-server gate.
#
# BOTH gates are driven, not just a11y: they share one runner (`a11y_gate_run`), and the
# card's requirement is that the fix covers the `e2e` gate too — so a green here is a
# statement about the runner the two gates actually call, for both of them.
# ---------------------------------------------------------------------------
for CASE1_GATE in a11y e2e; do
  CASE1_LABEL="tree-$CASE1_GATE"
  echo "-- CASE 1: two simultaneous runs of the $CASE1_GATE gate on this tree"
  pair "$CASE1_LABEL" 0 "$CASE1_GATE"
  verdict "$CASE1_LABEL" "CASE 1 ($CASE1_GATE)"
  t1="$(port_of "$STATE/$CASE1_LABEL-1.log")"; t2="$(port_of "$STATE/$CASE1_LABEL-2.log")"
  v1="$(grep -o 'EXIT=[0-9]*' "$STATE/$CASE1_LABEL-1.log" | tail -1)"; v2="$(grep -o 'EXIT=[0-9]*' "$STATE/$CASE1_LABEL-2.log" | tail -1)"
  if [ "$v1" = "EXIT=0" ] && [ "$v2" = "EXIT=0" ]; then ok "CASE 1: both simultaneous $CASE1_GATE runs exited 0"
  else bad "CASE 1: a simultaneous $CASE1_GATE run did not exit 0 ($v1 / $v2)"; fi
  if [ -n "$t1" ] && [ -n "$t2" ] && [ "$t1" != "$t2" ]; then ok "CASE 1: the two $CASE1_GATE runs used DIFFERENT ports ($t1, $t2) — the claim excluded the peer"
  else bad "CASE 1: the two $CASE1_GATE runs did not use different ports (${t1:-?}, ${t2:-?})"; fi
  if [ "$(grep -c 'exclusive claim' "$STATE/$CASE1_LABEL-1.log")" -ge 1 ] && [ "$(grep -c 'exclusive claim' "$STATE/$CASE1_LABEL-2.log")" -ge 1 ]; then
    ok "CASE 1: both $CASE1_GATE runs name the claim file they held (the mechanism is visible in the gate's own output)"
  else bad "CASE 1: a $CASE1_GATE run did not name the claim it held"; fi
done

# ---------------------------------------------------------------------------
# CASE 2 — the BASE at the staggered interleaving. This is the bite.
# ---------------------------------------------------------------------------
echo "-- CASE 2: two runs on the BASE ($BASE_REF) at the pre-fix window's interleaving"
cp "$STATE/base-preflight.sh" "$WORK/scripts/preflight.sh"
chmod +x "$WORK/scripts/preflight.sh"
[ "$(grep -c 'a11y_port_claim' "$WORK/scripts/preflight.sh")" -eq 0 ] || bad "CASE 2: the base copy still carries the claim"
pair base "$STUB_STAGGER"
verdict base "CASE 2"
b1="$(port_of "$STATE/base-1.log")"; b2="$(port_of "$STATE/base-2.log")"
c1="$(grep -o 'EXIT=[0-9]*' "$STATE/base-1.log" | tail -1)"; c2="$(grep -o 'EXIT=[0-9]*' "$STATE/base-2.log" | tail -1)"
if [ -n "$b1" ] && [ "$b1" = "$b2" ] && { [ "$c1" != "EXIT=0" ] || [ "$c2" != "EXIT=0" ]; }; then
  ok "CASE 2: the pre-fix bytes put BOTH runs on port $b1 and one FAILED ($c1 / $c2) — the defect this card fixes, reproduced"
elif [ "$b1" != "$b2" ]; then
  refuse "CASE 2: the pre-fix runs picked different ports (${b1:-?}, ${b2:-?}) — the window was MISSED at margin $(margin "$STATE/base-1.npm" "$STATE/base-2.npm")s, so nothing was proved about the base. Re-run; a miss is not a pass."
else
  refuse "CASE 2: both pre-fix runs used port ${b1:-?} but neither failed ($c1 / $c2)"
fi

# ---------------------------------------------------------------------------
# CASE 3 — the TREE at the SAME interleaving (the control that makes case 2's red
# attributable to the bytes, not to the stagger).
# ---------------------------------------------------------------------------
echo "-- CASE 3: this tree at the SAME interleaving as CASE 2"
cp "$HERE/preflight.sh" "$WORK/scripts/preflight.sh"
chmod +x "$WORK/scripts/preflight.sh"
pair treeS "$STUB_STAGGER"
verdict treeS "CASE 3"
s1="$(port_of "$STATE/treeS-1.log")"; s2="$(port_of "$STATE/treeS-2.log")"
w1="$(grep -o 'EXIT=[0-9]*' "$STATE/treeS-1.log" | tail -1)"; w2="$(grep -o 'EXIT=[0-9]*' "$STATE/treeS-2.log" | tail -1)"
if [ "$w1" = "EXIT=0" ] && [ "$w2" = "EXIT=0" ]; then ok "CASE 3: both staggered runs on this tree exited 0 (the same interleaving that reds the base)"
else bad "CASE 3: a staggered run on this tree did not exit 0 ($w1 / $w2)"; fi
if [ -n "$s1" ] && [ -n "$s2" ] && [ "$s1" != "$s2" ]; then ok "CASE 3: the staggered pair still used DIFFERENT ports ($s1, $s2)"
else bad "CASE 3: the staggered pair collided on port ${s1:-?}"; fi

# ---------------------------------------------------------------------------
# CASE 4 — NEGATIVE CONTROL: a FOREIGN listener on the first free candidate.
# ---------------------------------------------------------------------------
echo "-- CASE 4: a foreign listener holds the first free candidate port"
FOREIGN_PORT=""
for c in $(seq 3100 3140); do
  if ! ss -ltn 2>/dev/null | grep -q ":$c "; then FOREIGN_PORT="$c"; break; fi
done
if [ -z "$FOREIGN_PORT" ]; then
  refuse "CASE 4: no free candidate port on this host to run the control on"
else
  python3 - "$FOREIGN_PORT" 40 <<'PY' >/dev/null 2>&1 &
import socket, sys, time
port, hold = int(sys.argv[1]), float(sys.argv[2])
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", port)); s.listen(1); time.sleep(hold)
PY
  FOREIGN_PID=$!
  sleep 1
  if ! ss -ltn 2>/dev/null | grep -q ":$FOREIGN_PORT "; then
    refuse "CASE 4: the foreign listener did not come up on $FOREIGN_PORT"
  else
    echo "   foreign listener pid=$FOREIGN_PID holding $FOREIGN_PORT"
    (
      cd "$WORK" || exit 1
      env PATH="$WORK/bin:$PATH" \
        PREFLIGHT_LOCK_FILE="$STATE/negctl.lock" PREFLIGHT_RESULT_DIR="$STATE/gate-results-negctl" \
        PREFLIGHT_RESULT_TTL=3600 STUB_LOG="$STATE/negctl.npm" STUB_DELAY="$STUB_DELAY" STUB_HOLD="$STUB_HOLD" \
        ./scripts/preflight.sh --only a11y >"$STATE/negctl.log" 2>&1
      echo "EXIT=$?" >>"$STATE/negctl.log"
    )
    np="$(port_of "$STATE/negctl.log")"; ne="$(grep -o 'EXIT=[0-9]*' "$STATE/negctl.log" | tail -1)"
    echo "   gate: port=${np:-none} $ne"
    grep -hE "starting the app's own server" "$STATE/negctl.log" | sed 's/^/     /'
    if [ "$ne" = "EXIT=0" ]; then ok "CASE 4: the gate exited 0 with a foreign listener on $FOREIGN_PORT"
    else bad "CASE 4: the gate exited $ne with a foreign listener on $FOREIGN_PORT"; fi
    if [ -n "$np" ] && [ "$np" != "$FOREIGN_PORT" ]; then ok "CASE 4: the gate claimed a DIFFERENT port ($np) — it did not reuse the foreign server"
    else bad "CASE 4: the gate claimed the foreign port (${np:-?}) instead of skipping it"; fi
    if kill -0 "$FOREIGN_PID" 2>/dev/null && ss -ltn 2>/dev/null | grep -q ":$FOREIGN_PORT "; then
      ok "CASE 4: the foreign listener is still alive and still holds $FOREIGN_PORT (never reused, never killed)"
    else bad "CASE 4: the foreign listener did not survive the gate"; fi
  fi
  kill "$FOREIGN_PID" 2>/dev/null; wait "$FOREIGN_PID" 2>/dev/null
fi

# ---------------------------------------------------------------------------
# CASE 5 — RESIDUE: a holder that takes the claimed port between claim and bind.
# ---------------------------------------------------------------------------
echo "-- CASE 5: a holder squats on the claimed port (the labelled retry's job)"
rm -f "$STATE/squat.mark"
(
  cd "$WORK" || exit 1
  env PATH="$WORK/bin:$PATH" \
    PREFLIGHT_LOCK_FILE="$STATE/squat.lock" PREFLIGHT_RESULT_DIR="$STATE/gate-results-squat" \
    PREFLIGHT_RESULT_TTL=3600 STUB_LOG="$STATE/squat.npm" STUB_DELAY="$STUB_DELAY" STUB_HOLD="$STUB_HOLD" \
    STUB_SQUAT_FIRST=1 STUB_SQUAT_MARK="$STATE/squat.mark" STUB_SQUAT_HOLD=15 \
    ./scripts/preflight.sh --only a11y >"$STATE/squat.log" 2>&1
  echo "EXIT=$?" >>"$STATE/squat.log"
)
sqe="$(grep -o 'EXIT=[0-9]*' "$STATE/squat.log" | tail -1)"
grep -hE "starting the app's own server|RETRY|EADDRINUSE|SQUATTING" "$STATE/squat.log" | sed 's/^/     /'
if [ "$sqe" = "EXIT=0" ]; then ok "CASE 5: the run still exited 0 through the residue"
else bad "CASE 5: the run exited $sqe — the labelled retry did not recover"; fi
if grep -q "RETRY 1/1" "$STATE/squat.log"; then
  ok "CASE 5: the retry is LABELLED in the gate's own output (it names the port it abandoned and the one it took)"
else bad "CASE 5: no labelled retry line — the residue was either not hit or not announced"; fi

echo "--------------------------------------------------------------"
echo "BITES: $BITES case(s) proved, $FAILS refused/failed"
if [ "$FAILS" -eq 0 ]; then
  echo "RESULT: ALL CASES PROVED — exit 0"
  exit 0
fi
echo "RESULT: $FAILS CASE(S) NOT PROVED — exit 1"
exit 1
