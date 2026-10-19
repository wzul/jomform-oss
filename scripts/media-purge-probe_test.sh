#!/usr/bin/env bash
#
# scripts/media-purge-probe_test.sh — offline proof that
# scripts/media-purge-probe.sh reaches the RIGHT verdict. Run by the
# `mediaprobe` preflight gate; also runnable by hand:
#
#   bash scripts/media-purge-probe_test.sh
#
# THE THREE FALSE / MISSED VERDICTS THIS PINS (all measured on the live stack):
#
#   1. A CDN may REWRITE the client-facing `cache-control` (Cloudflare's zone
#      setting "Browser Cache TTL"), so an edge can hand a browser
#      `max-age=14400` while the shared entry still expires on the origin's
#      300s. The script used to read that header as "the edge lifetime" and
#      return FAIL on a healthy stack — reproduced on prod 2026-10-18 with
#      `edge: not_configured`, which is a supported mode. Scenario A.
#   2. Probe-only mode (no --purge) is not a failed purge: the object is
#      SUPPOSED to still be served. The script used to print "the purge did not
#      run, or it names another id" and exit 1 for a read-only probe of a
#      healthy stack. Scenario B.
#   3. The header comparison is not always wrong, so the fix must not suppress
#      a genuine FAIL: an entry with `age=10577` under an origin that now
#      declares 300s was filled under an older 24h TTL and is still stranded.
#      Scenario C keeps that red. Scenario D keeps "the origin itself declares
#      longer than the window" red, and scenario E is the control the card asks
#      for: an edge that PINS a fresh entry while the origin is healthy — the
#      inference cannot see it (bounded by the origin's 2s), so `--await-expiry`
#      has to MEASURE it and must still go red.
#
# OFFLINE BY CONSTRUCTION: a stub origin+edge (python3, loopback, port 0) whose
# edge behaves like a CDN — fills on first request, serves `cf-cache-status`/`age`,
# expires on ITS OWN ttl, rewrites the client-facing header when told to — so
# every verdict below is a real HTTP exchange, not a mocked return value. No
# network, no prod, ~40s.
#
# PRE-FIX CONTROL: `PROBE_UNDER_TEST=/path/to/old/probe.sh` runs the whole suite
# against another copy. The false-FAIL scenarios (A, B) go RED against the
# pre-fix script, which is how the fix was shown to bite; get that copy with
# `git show <pre-fix-rev>:scripts/media-purge-probe.sh > /tmp/pre.sh`.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
PROBE="${PROBE_UNDER_TEST:-$HERE/media-purge-probe.sh}"

if [ ! -f "$PROBE" ]; then
  echo "media-purge-probe_test: probe not found at $PROBE" >&2
  exit 2
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "media-purge-probe_test: python3 is required for the stub origin/edge (refusing to skip)" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "media-purge-probe_test: curl is required (the probe itself uses it)" >&2
  exit 2
fi

CHECKS=0
FAILS=0
say() { printf '%s\n' "$*"; }
ok()  { CHECKS=$((CHECKS + 1)); printf 'ok   %s\n' "$1"; }
bad() {
  CHECKS=$((CHECKS + 1)); FAILS=$((FAILS + 1))
  printf 'FAIL %s\n' "$1"
  if [ -n "${OUT:-}" ]; then printf '%s\n' "$OUT" | sed 's/^/       | /'; fi
}

# ── the stub origin + edge ──────────────────────────────────────────────────
STUB_PID=""
STUB_DIR=""
ORIGIN=""
EDGE=""

start_stub() { # start_stub <config-json>
  STUB_DIR="$(mktemp -d)"
  printf '%s' "$1" > "$STUB_DIR/cfg.json"
  cat > "$STUB_DIR/stub.py" <<'PYEOF'
import json, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

CFG = json.load(open(sys.argv[1]))
PORTS = sys.argv[2]

lock = threading.RLock()
state = {"purged": False}
edge_cache = {}


def origin_result(path):
    with lock:
        purged = state["purged"]
    status = int(CFG.get("origin_status", 200))
    if status != 200:
        return status, {}
    if purged and CFG.get("purge_sets_404", True):
        return 404, {}
    return 200, {"Cache-Control": "public, max-age=%d" % int(CFG.get("origin_ttl", 300))}


class Base(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def reply(self, code, headers, body=b""):
        self.send_response(code)
        if not body and code == 200:
            body = b"x"
        hdrs = dict(headers)
        hdrs.setdefault("Content-Length", str(len(body)))
        for k, v in hdrs.items():
            self.send_header(k, str(v))
        self.end_headers()
        self.wfile.write(body)


class Origin(Base):
    def do_GET(self):
        code, hdrs = origin_result(self.path)
        self.reply(code, hdrs)

    def do_DELETE(self):
        if not (self.path.startswith("/v1/rest/media/") and self.path.endswith("/purge")):
            return self.reply(404, {})
        with lock:
            state["purged"] = True
            if CFG.get("edge_purge_on_purge"):
                edge_cache.clear()
        body = json.dumps({"status": "purged", "edge": "not_configured",
                           "edge_max_age_s": int(CFG.get("origin_ttl", 300))}).encode()
        self.reply(200, {"Content-Type": "application/json"}, body)


class Edge(Base):
    def do_GET(self):
        now = time.time()
        key = self.path
        ttl = float(CFG.get("edge_ttl", 300))
        client_cc = CFG.get("client_cc", "public, max-age=300")
        with lock:
            entry = edge_cache.get(key)
            if entry is None and CFG.get("prefill_age") is not None:
                entry = {"fill": now - float(CFG["prefill_age"])}
                edge_cache[key] = entry
            if entry is not None:
                age = now - entry["fill"]
                if age <= ttl:
                    return self.reply(200, {"Cache-Control": client_cc,
                                            "CF-Cache-Status": "HIT",
                                            "Age": str(int(age))})
                code, _ = origin_result(key)
                if code != 200:
                    del edge_cache[key]
                    return self.reply(code, {"CF-Cache-Status": "EXPIRED",
                                             "Cache-Control": "no-store"})
                entry["fill"] = now
                return self.reply(200, {"Cache-Control": client_cc,
                                        "CF-Cache-Status": "MISS", "Age": "0"})
            code, _ = origin_result(key)
            if code != 200:
                return self.reply(code, {"CF-Cache-Status": "MISS",
                                         "Cache-Control": "no-store"})
            edge_cache[key] = {"fill": now}
            return self.reply(200, {"Cache-Control": client_cc,
                                    "CF-Cache-Status": "MISS", "Age": "0"})


origin_srv = ThreadingHTTPServer(("127.0.0.1", 0), Origin)
edge_srv = ThreadingHTTPServer(("127.0.0.1", 0), Edge)
with open(PORTS, "w") as fh:
    json.dump({"origin": origin_srv.server_address[1],
               "edge": edge_srv.server_address[1]}, fh)
threading.Thread(target=origin_srv.serve_forever, daemon=True).start()
edge_srv.serve_forever()
PYEOF
  python3 "$STUB_DIR/stub.py" "$STUB_DIR/cfg.json" "$STUB_DIR/ports.json" &
  STUB_PID=$!
  local i
  for i in $(seq 1 100); do
    [ -s "$STUB_DIR/ports.json" ] && break
    sleep 0.1
  done
  if [ ! -s "$STUB_DIR/ports.json" ]; then
    echo "media-purge-probe_test: the stub origin/edge did not start" >&2
    exit 2
  fi
  ORIGIN="http://127.0.0.1:$(sed -n 's/.*"origin":[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$STUB_DIR/ports.json")"
  EDGE="http://127.0.0.1:$(sed -n 's/.*"edge":[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$STUB_DIR/ports.json")"
}

stop_stub() {
  if [ -n "$STUB_PID" ]; then
    kill "$STUB_PID" 2>/dev/null
    wait "$STUB_PID" 2>/dev/null
    STUB_PID=""
  fi
  if [ -n "$STUB_DIR" ]; then
    rm -rf "$STUB_DIR"
    STUB_DIR=""
  fi
}

cleanup() { stop_stub; }
trap cleanup EXIT

MEDIA="/media/u/acct/0123abcd-0000-4000-8000-000000000000.jpg"

# run_probe <args...> — sets OUT (stdout+stderr) and RC.
run_probe() {
  OUT="$("$PROBE" "$@" 2>&1)"
  RC=$?
}

expect_rc() { # expect_rc <code> <label>
  if [ "$RC" -eq "$1" ]; then ok "$2"; else bad "$2 (exit ${RC}, wanted $1)"; fi
}
expect_in() { # expect_in <text> <label>
  if printf '%s' "$OUT" | grep -qF -- "$1"; then ok "$2"; else bad "$2 (output did not contain '$1')"; fi
}
expect_not_in() { # expect_not_in <text> <label>
  if printf '%s' "$OUT" | grep -qF -- "$1"; then bad "$2 (output contained '$1')"; else ok "$2"; fi
}

say "media-purge-probe_test  probe=${PROBE}"

# ── A. the reported false FAIL: purge done, edge rewrites the client header ──
say ""
say "A. CDN rewrites cache-control to 14400 while the edge entry lives 3s (the reported false FAIL)"
start_stub '{"origin_ttl": 3, "edge_ttl": 3, "client_cc": "public, max-age=14400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 11111111 --api "$ORIGIN" --token test-token --window 3
expect_rc 0 "A: purge + fresh edge entry under a rewritten 14400 header is PASS"
expect_in "verdict: PASS" "A: verdict names PASS"
expect_in "bounded" "A: the copy is reported as bounded, not stale"
expect_in "REWRITES the client-facing header" "A: the 14400 header is named as a CDN rewrite, not the edge lifetime"
expect_not_in "STALE beyond the documented window" "A: the rewritten header is not read as the edge lifetime"
expect_in "edge=200" "A: the edge really was still serving (the stub is the no-credential case)"
stop_stub

# ── B. probe-only mode is not a failed purge ────────────────────────────────
say ""
say "B. probe-only mode (no --purge): a live object at origin AND edge is healthy"
start_stub '{"origin_ttl": 300, "edge_ttl": 300, "client_cc": "public, max-age=14400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" --window 300
expect_rc 0 "B: a read-only probe of a healthy stack exits 0"
expect_in "probe only" "B: the run names itself as probe-only"
expect_not_in "was not actually removed" "B: a live object is not reported as a failed purge"
expect_not_in "STALE beyond the documented window" "B: the rewritten 14400 header is not judged as the edge lifetime"
stop_stub

# ── C. the genuine pre-fix entry must STAY red ──────────────────────────────
say ""
say "C. a pre-fix edge entry (age 10577, filled under a 24h TTL) stays a FAIL"
start_stub '{"origin_ttl": 300, "edge_ttl": 86400, "client_cc": "public, max-age=86400", "prefill_age": 10577}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" --window 300
expect_rc 1 "C: an entry older than the origin's declared lifetime is a FAIL"
expect_in "OLDER than the lifetime" "C: the FAIL names the entry age as the reason"
expect_in "age=10577" "C: the measured age is printed"
expect_not_in "was not actually removed" "C: probe-only mode still does not claim a purge failed"
stop_stub

# ── D. control: the ORIGIN itself declares longer than the window ───────────
say ""
say "D. control: an origin that serves max-age=86400 cannot self-heal inside a 300s window"
start_stub '{"origin_ttl": 86400, "edge_ttl": 86400, "client_cc": "public, max-age=86400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 22222222 --api "$ORIGIN" --token test-token --window 300
expect_rc 1 "D: an 86400s origin lifetime is a FAIL at window 300"
expect_in "longer than the 300s window" "D: the FAIL names the lifetime and the window"
stop_stub

# ── E. control: an edge that PINS a fresh entry the headers cannot reveal ───
say ""
say "E. control: the edge pins a fresh entry (3600s) while the origin declares 2s — measured, not inferred"
start_stub '{"origin_ttl": 2, "edge_ttl": 3600, "client_cc": "public, max-age=14400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 33333333 --api "$ORIGIN" --token test-token --window 2 --await-expiry --poll 1
expect_rc 1 "E: --await-expiry still FAILS a pinned entry the declaration cannot bound"
expect_in "STILL SERVING after the 2s window" "E: the FAIL says the entry was measured still serving"
stop_stub

# ── E2. the same measurement, on an edge that really does expire ────────────
say ""
say "E2. --await-expiry PASSES when the entry really stops serving inside the window"
start_stub '{"origin_ttl": 2, "edge_ttl": 2, "client_cc": "public, max-age=14400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 44444444 --api "$ORIGIN" --token test-token --window 3 --await-expiry --poll 1
expect_rc 0 "E2: an entry that expires inside the window is a PASS"
expect_in "measured: the entry stopped serving" "E2: the PASS is an observation, not an inference"
stop_stub

# ── F. a CDN purge that drops the entry: both ends 404 ──────────────────────
say ""
say "F. with a purge credential the edge entry is dropped: origin and edge both 404"
start_stub '{"origin_ttl": 3, "edge_ttl": 3, "client_cc": "public, max-age=14400", "edge_purge_on_purge": true}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 55555555 --api "$ORIGIN" --token test-token --window 3
expect_rc 0 "F: a real purge (edge gone) is PASS"
expect_in "gone at both ends" "F: the verdict says the copy is gone at both ends"
stop_stub

# ── G. the purge did not remove the object at the origin ────────────────────
say ""
say "G. a purge that leaves the object at the origin is still a FAIL"
start_stub '{"origin_ttl": 3, "edge_ttl": 3, "client_cc": "public, max-age=14400", "purge_sets_404": false}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" \
  --purge 66666666 --api "$ORIGIN" --token test-token --window 3
expect_rc 1 "G: origin still 200 after the purge is a FAIL"
expect_in "still answers 200" "G: the FAIL names the origin status"
stop_stub

# ── H. an object already gone at both ends (probe-only) ─────────────────────
say ""
say "H. probe-only against an object that is already gone at both ends"
start_stub '{"origin_ttl": 300, "origin_status": 404, "edge_ttl": 3, "client_cc": "public, max-age=14400"}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" --window 300
expect_rc 0 "H: an object gone at the origin and the edge is PASS"
expect_in "gone at the origin and at the edge" "H: the verdict says so"
stop_stub

# ── I. a served copy with no readable lifetime, and the await escape hatch ──
say ""
say "I. a served edge copy whose lifetime cannot be read anywhere is UNDECIDED (exit 2)"
start_stub '{"origin_ttl": 0, "origin_status": 404, "edge_ttl": 3600, "client_cc": "public, max-age=14400", "prefill_age": 0}'
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" --window 2
expect_rc 2 "I: nothing could be decided, so the exit code is 2 (not a false PASS or FAIL)"
expect_in "verdict: UNDECIDED" "I: the verdict says UNDECIDED"
say ""
say "I2. ... and --await-expiry decides the same shape by measurement (it never expires -> FAIL)"
run_probe --origin "$ORIGIN" --edge "$EDGE" --path "$MEDIA" --window 1 --await-expiry --poll 1
expect_rc 1 "I2: --await-expiry turns an undecidable copy into a measured FAIL"
expect_in "STILL SERVING after the 1s window" "I2: the FAIL says the entry was measured still serving"
stop_stub

# ── J. transport failure is not a verdict ──────────────────────────────────
say ""
say "J. a probe that cannot complete is exit 2, never a PASS or FAIL"
start_stub '{"origin_ttl": 300, "edge_ttl": 300, "client_cc": "public, max-age=300"}'
run_probe --origin "$ORIGIN" --edge "http://127.0.0.1:1" --path "$MEDIA" --window 300
expect_rc 2 "J: an unreachable edge is exit 2"
expect_in "nothing was decided" "J: the verdict says nothing was decided"
stop_stub

# ── K. usage errors ────────────────────────────────────────────────────────
say ""
say "K. usage errors exit 2 without measuring anything"
run_probe --origin "http://127.0.0.1:1" --edge "http://127.0.0.1:1"
expect_rc 2 "K: a missing --path is exit 2"
run_probe --origin "http://127.0.0.1:1" --edge "http://127.0.0.1:1" --path "$MEDIA" --window 0
expect_rc 2 "K: --window 0 is exit 2"
run_probe --origin "http://127.0.0.1:1" --edge "http://127.0.0.1:1" --path "$MEDIA" --purge 1
expect_rc 2 "K: --purge without --token is exit 2"

say ""
if [ "$FAILS" -eq 0 ]; then
  say "RESULT: $CHECKS checks passed, 0 failed"
  exit 0
fi
say "RESULT: $FAILS of $CHECKS checks FAILED"
exit 1
