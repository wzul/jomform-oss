#!/usr/bin/env bash
#
# deploy/biteproof_ship_static_purge.sh — prove the SHIP-TIME `_next/static` purge
# bites, in both halves it is made of.
#
# THE DEFECT THIS INSTRUMENT EXISTS FOR (measured on prod 2026-11-09). Cloudflare
# caches a `/_next/static/*` response INCLUDING its header set for
# `public, max-age=31536000, immutable`. A release that changes headers but not
# asset BYTES keeps the same hashed chunk names (the hash covers the module graph,
# not the bytes), so the edge keeps answering with the OLD header set for up to a
# year. Measured that day: 39 of the shipped image's 60 assets came from the edge
# with ZERO security headers, ages up to 23.8 days, while the origin had carried
# them since v1.30.0; only a manual `purge_everything` closed it. Nothing on the
# ship path ever asked the CDN to drop them.
#
# THE TWO HALVES, and neither one alone is the fix:
#   A. deploy/edge-purge-static.sh  — builds ONE prefix purge for `/_next/static/`
#      on the deployment's own host, reads the CDN's answer, and reports
#      not-configured LOUDLY instead of silently doing nothing.
#   B. deploy/build-and-ship.sh § 8c — calls it ONLY after the ship is PROVEN.
#      A failed recreate leaves the OLD image serving, so purging then would just
#      refill the edge from the old origin.
#
# WHAT THIS PROVES, case by case (each is a direction a broken fix would fail):
#   --- case 0  CONTROL, AND PROD'S CURRENT STATE: with JF_CF_PURGE_TOKEN /
#              ZONE_ID unset-or-EMPTY (what prod measures TODAY — and where a
#              set-but-empty var is the trap: the NAME is in the compose env, the
#              VALUE is empty) the script prints the canonical loud sentence, exits
#              0, and makes NO request.
#   --- case 1  configured: the request is really made — POST to
#              /zones/<zone>/purge_cache, `Authorization: Bearer …`, body
#              {"prefixes":["https://<host>/_next/static/"]} — and `success=true`
#              is what the script logs.
#   --- case 2  a 200 carrying `success:false` is a REFUSAL, not a purge
#              (Cloudflare answers 200 with errors[] for rate limiting among
#              others): exit 3, the error code named, the canonical sentence
#              printed.
#   --- case 3  prod's own host list does not cover the apex: JF_CF_PURGE_HOSTS is
#              `*.jomform.com` for the per-workspace form hosts, and a wildcard
#              matches SUBDOMAINS ONLY — so the web app's apex host is dropped. The
#              script must say so, name the remedy, purge nothing, and exit 3 rather
#              than report a purge that never happened.
#   --- case 4  A FAILED ROLL PURGES NOTHING. The sandbox's ship proof fails, so
#              build-and-ship exits 7 having made its recreate call and NOT the
#              purge call.
#   --- case 5  A PROVEN ROLL PURGES: build-and-ship reaches § 8c, the stream it
#              pipes to the deployment host ends with THIS TREE's
#              deploy/edge-purge-static.sh bytes, the credentials are read out of
#              that host's compose `.env` (quotes stripped), the request lands on the
#              stub CDN carrying the token from that file, and a `$(…)` sitting in an
#              UNRELATED .env line never runs (the .env is READ, not sourced).
#   --- case 6  THE INSTRUMENT'S OWN BITE: a sandbox whose § 8c call is neutered
#              (`purge_static_edge` -> `:`) must FAIL case 5's checks. Without this
#              the instrument could be green while measuring nothing.
#
# WHAT IS REAL AND WHAT IS STUBBED, stated because it bounds the claim:
#   * The SCRIPT half runs for real: real curl, real HTTP, a python3 stub CDN on
#     loopback (port 0). Neither the request nor the verdict is mocked.
#   * The DEPLOY-HOST half is real too, in the only way it can be offline: the ssh
#     stub EXECUTES the stream build-and-ship pipes to it, so both the prologue that
#     reads `.env` and the purge bytes appended to it run. What is stubbed is the
#     transport (ssh) and the recreate (docker), not the logic under proof.
#   * Cloudflare itself is NOT contacted — that needs the token, which is empty on
#     prod today. A real purge is a deploy-time/live check, not this suite's claim.
#
# Nothing under the repo is mutated: the sandboxes live in $WORK, and
# deploy/build-and-ship.sh + deploy/edge-purge-static.sh are sha256-checked before
# and after.
#
# Run:  ./deploy/biteproof_ship_static_purge.sh
# Exit: 0 if every case bites, 1 otherwise, 2 on a capability refusal.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
cd "$REPO_ROOT" || exit 2

PURGE_SCRIPT="deploy/edge-purge-static.sh"
BUILD_SHIP="deploy/build-and-ship.sh"

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; }

for f in "$PURGE_SCRIPT" "$BUILD_SHIP"; do
  [ -f "$f" ] || { printf 'biteproof: %s not found — cannot prove anything about the ship-time purge\n' "$f" >&2; exit 2; }
done
command -v python3 >/dev/null 2>&1 || {
  printf 'biteproof: python3 is required for the stub CDN (refusing to skip)\n' >&2
  exit 2
}
command -v curl >/dev/null 2>&1 || {
  printf 'biteproof: curl is required — it is what the purge script uses (refusing to skip)\n' >&2
  exit 2
}

WANT_PURGE_SHA="$(sha256sum "$PURGE_SCRIPT" | awk '{print $1}')"
WANT_SHIP_SHA="$(sha256sum "$BUILD_SHIP" | awk '{print $1}')"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/biteproof_static_purge.XXXXXX")" || exit 2
CASE_PIDS=()
cleanup() {
  local p
  for p in "${CASE_PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done
  # BITEPROOF_KEEP_WORK=1 leaves $WORK behind so a red case can be read (the ship
  # log, the piped stream, what the stub CDN recorded) instead of re-derived.
  if [ "${BITEPROOF_KEEP_WORK:-0}" = "1" ]; then
    printf '\nbiteproof: BITEPROOF_KEEP_WORK=1 — work dir kept at %s\n' "$WORK"
    return 0
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

CANON='edge not purged: stale `_next/static` headers may be served for up to a year after a header-only release'

# ── the stub CDN ───────────────────────────────────────────────────────────────
# A real HTTP server on loopback: every request (path, auth header, raw body) is
# recorded beside the log, and the answer is what the case asked for. A 200 carrying
# `success:false` is a body the REAL endpoint returns for rate limiting, so the
# refusal case is a real exchange rather than a mocked return value.
cat >"$WORK/cdn.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODE, LOG = sys.argv[1], sys.argv[2]

class H(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("content-length") or 0)
        body = self.rfile.read(n).decode()
        with open(LOG, "a") as fh:
            fh.write(json.dumps({"path": self.path,
                                 "auth": self.headers.get("authorization") or "",
                                 "ctype": self.headers.get("content-type") or ""}) + "\n")
            fh.write(body + "\n")
        if MODE == "accept":
            out, code = json.dumps({"success": True, "errors": [], "result": {"id": "z"}}), 200
        elif MODE == "reject":
            out, code = json.dumps({"success": False,
                                    "errors": [{"code": 1134, "message": "rate limited"}]}), 200
        else:
            out, code = json.dumps({"success": False}), 400
        raw = out.encode()
        self.send_response(code)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def log_message(self, *a):
        pass

srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
print(srv.server_address[1], flush=True)
srv.serve_forever()
PY

CDN_PID=""
CDN_LOG=""
# start_cdn <mode> <logfile> — echoes the port.
start_cdn() {
  local mode="$1" port=""
  CDN_LOG="$2"
  : >"$CDN_LOG"
  python3 "$WORK/cdn.py" "$mode" "$CDN_LOG" >"$WORK/cdn.port" 2>"$WORK/cdn.err" &
  CDN_PID=$!
  CASE_PIDS+=("$CDN_PID")
  for _ in $(seq 1 100); do
    port="$(head -n 1 "$WORK/cdn.port" 2>/dev/null)"
    [ -n "$port" ] && break
    sleep 0.1
  done
  if [ -z "$port" ]; then
    printf 'biteproof: the stub CDN did not start: %s\n' "$(cat "$WORK/cdn.err" 2>/dev/null)" >&2
    exit 2
  fi
  printf '%s' "$port"
}
stop_cdn() {
  [ -n "$CDN_PID" ] && kill "$CDN_PID" 2>/dev/null
  wait "$CDN_PID" 2>/dev/null
  CDN_PID=""
}

# cdn_meta <logfile> — the recorded request line (path + auth), one per request.
cdn_meta() { sed -n '1p' "$1" 2>/dev/null; }
# cdn_body <logfile> — the raw request body of the first request.
cdn_body() { sed -n '2p' "$1" 2>/dev/null; }
cdn_requests() { grep -c . "$1" 2>/dev/null || printf '0'; }

# run_purge <out> <err> <env...> — run the shipped script with the env given.
run_purge() {
  local out="$1" err="$2"
  shift 2
  ( "$@" bash "$PURGE_SCRIPT" >"$out" 2>"$err" )
}

echo "biteproof: the ship-time /_next/static edge purge"
echo "  repo:  $(pwd)"
echo "  head:  $(git rev-parse --short HEAD 2>/dev/null || echo '?')"
echo "  purge: $PURGE_SCRIPT (sha256 ${WANT_PURGE_SHA:0:12})"
echo "  ship:  $BUILD_SHIP (sha256 ${WANT_SHIP_SHA:0:12})"
echo "---"

# ============================================================================
# --- case 0
# CONTROL, and prod's CURRENT state: unconfigured must be LOUD and must not
# pretend. `printenv JF_CF_PURGE_TOKEN | wc -c` = 1 on prod (a newline: the var is
# SET and EMPTY), so the empty-string form is driven too — "the name is in
# printenv" is exactly the reading trap this card names.
# ============================================================================
echo
echo "--- case 0: unconfigured (prod today: token and zone set-but-EMPTY) -> loud, no request"
PORT="$(start_cdn accept "$WORK/c0.cdn")"
run_purge "$WORK/c0.out" "$WORK/c0.err" env -u JF_CF_PURGE_TOKEN -u JF_CF_PURGE_ZONE_ID \
  JF_CF_PURGE_API_BASE="http://127.0.0.1:$PORT/client/v4" APP_BASE_URL="https://jomform.com"
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 0: unset env exits 0 (a purge is not allowed to fail a release)" \
  || bad "case 0: unset env exited $RC, expected 0"
grep -qF "$CANON" "$WORK/c0.err" \
  && ok "case 0: the canonical 'edge not purged' sentence is printed" \
  || bad "case 0: the canonical warning is MISSING from stderr"
[ -s "$WORK/c0.cdn" ] && bad "case 0: an unconfigured purge contacted the CDN" \
  || ok "case 0: no request was made while unconfigured"

PORT="$(start_cdn accept "$WORK/c0b.cdn")"
# The two credentials are SET and EMPTY — the exact shape prod measures. They are
# spelled `NAME=''` rather than a bare `NAME=`, and each sits on its OWN line, for a
# reason that is not cosmetic: a bare `A= B=` puts one empty variable's NAME where the
# other's VALUE would be, which is what a secret scanner sees as a 20-character
# credential (measured: `generic-api-key` read `JF_CF_PURGE_ZONE_ID=` as the secret at
# entropy 3.72). `''` is the same empty string to the shell and has a visible end.
run_purge "$WORK/c0b.out" "$WORK/c0b.err" env \
  JF_CF_PURGE_API_BASE="http://127.0.0.1:$PORT/client/v4" \
  APP_BASE_URL="https://jomform.com" \
  JF_CF_PURGE_TOKEN='' \
  JF_CF_PURGE_ZONE_ID=''
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 0: set-but-EMPTY reads as NOT configured (the prod reading trap)" \
  || bad "case 0: set-but-empty exited $RC, expected 0 (a name in printenv is not a value)"
grep -qF "$CANON" "$WORK/c0b.err" \
  && ok "case 0: set-but-EMPTY also prints the canonical sentence" \
  || bad "case 0: set-but-empty printed no warning — it would have reported a purge that never happened"

# ============================================================================
# --- case 1
# Configured: the request is real (real curl, real HTTP to the stub CDN).
# ============================================================================
echo
echo "--- case 1: configured -> one prefix purge, http accepted, success=true"
PORT="$(start_cdn accept "$WORK/c1.cdn")"
run_purge "$WORK/c1.out" "$WORK/c1.err" env \
  JF_CF_PURGE_TOKEN="tok-case1" JF_CF_PURGE_ZONE_ID="zone-case1" \
  JF_CF_PURGE_HOSTS="jomform.com" APP_BASE_URL="https://jomform.com" \
  JF_CF_PURGE_API_BASE="http://127.0.0.1:$PORT/client/v4"
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 1: a purged+accepted purge exits 0" || bad "case 1: exit $RC, expected 0"

META="$(cdn_meta "$WORK/c1.cdn")"
[ -n "$META" ] && ok "case 1: the CDN really received a request" || bad "case 1: the CDN received NOTHING"
case "$META" in
  *'"/client/v4/zones/zone-case1/purge_cache"'*) ok "case 1: POST /client/v4/zones/<zone>/purge_cache" ;;
  *) bad "case 1: wrong endpoint: $META" ;;
esac
case "$META" in
  *'Bearer tok-case1'*) ok "case 1: Authorization: Bearer <token>" ;;
  *) bad "case 1: the Authorization header is missing or wrong: $META" ;;
esac
BODY="$(cdn_body "$WORK/c1.cdn")"
[ "$BODY" = '{"prefixes":["https://jomform.com/_next/static/"]}' ] \
  && ok "case 1: the body is exactly one /_next/static/ prefix" \
  || bad "case 1: wrong body: $BODY"
grep -q 'success=true' "$WORK/c1.out" \
  && ok "case 1: the script logs the CDN's http result (success=true)" \
  || bad "case 1: the http result was not logged: $(head -c 200 "$WORK/c1.out")"
grep -qF 'tok-case1' "$WORK/c1.out" "$WORK/c1.err" \
  && bad "case 1: THE TOKEN IS PRINTED in the purge output" \
  || ok "case 1: the token is not printed"

# ============================================================================
# --- case 2
# A 200 carrying success:false is a REFUSAL. Trusting the status code alone would
# report a purge that did not happen — the failure mode cdnpurge documents.
# ============================================================================
echo
echo "--- case 2: http 200 + success:false is a refusal, not a purge"
PORT="$(start_cdn reject "$WORK/c2.cdn")"
run_purge "$WORK/c2.out" "$WORK/c2.err" env \
  JF_CF_PURGE_TOKEN="tok-case2" JF_CF_PURGE_ZONE_ID="zone-case2" \
  JF_CF_PURGE_HOSTS="jomform.com" APP_BASE_URL="https://jomform.com" \
  JF_CF_PURGE_API_BASE="http://127.0.0.1:$PORT/client/v4"
RC=$?
stop_cdn
[ "$RC" -eq 3 ] && ok "case 2: a 200/success:false purge exits 3" || bad "case 2: exit $RC, expected 3"
grep -qF "$CANON" "$WORK/c2.err" \
  && ok "case 2: the canonical sentence is printed for the refusal" \
  || bad "case 2: no canonical warning on a refused purge"
grep -q '1134' "$WORK/c2.err" \
  && ok "case 2: the CDN's own error code is named" \
  || bad "case 2: the refusal detail was swallowed"

# ============================================================================
# --- case 3
# The host list prod ships with is `*.jomform.com` (for the per-workspace form
# hosts). A wildcard does not cover the apex, and the web app IS the apex — so the
# static purge would find nothing in zone. It must say that, not report success.
# ============================================================================
echo
echo '--- case 3: JF_CF_PURGE_HOSTS=*.jomform.com does not cover the apex -> refused, named'
PORT="$(start_cdn accept "$WORK/c3.cdn")"
run_purge "$WORK/c3.out" "$WORK/c3.err" env \
  JF_CF_PURGE_TOKEN="tok-case3" JF_CF_PURGE_ZONE_ID="zone-case3" \
  JF_CF_PURGE_HOSTS='*.jomform.com' APP_BASE_URL="https://jomform.com" \
  JF_CF_PURGE_API_BASE="http://127.0.0.1:$PORT/client/v4"
RC=$?
stop_cdn
[ "$RC" -eq 3 ] && ok "case 3: an apex host outside the wildcard exits 3" || bad "case 3: exit $RC, expected 3"
grep -q 'NOT purging jomform.com' "$WORK/c3.err" \
  && ok "case 3: the dropped host is named" \
  || bad "case 3: the dropped host was not named"
grep -q 'SUBDOMAINS ONLY' "$WORK/c3.err" \
  && ok "case 3: the remedy is named (a wildcard covers subdomains only)" \
  || bad "case 3: the remedy was not printed"
[ -s "$WORK/c3.cdn" ] && bad "case 3: a request was made for an out-of-zone host" \
  || ok "case 3: nothing was asked of the CDN"

# ── the sandboxes: build-and-ship's CALL SITE ─────────────────────────────────
# A throwaway git repo holding the SHIPPED build-and-ship.sh plus stubs, so the
# gate is a one-line argv and nothing is built. LOCK_FILE / OUT_DIR / the compose
# dir live OUTSIDE the repo: the script refuses a dirty tree, and the compose `.env`
# is a deployment-host file, not part of the checkout.
make_sandbox() { # make_sandbox <dir> <compose-dir> <api-base> <decoy-file> [set|empty]
  local d="$1" compose="$2" api="$3" decoy="$4" creds="${5:-set}"
  local tok='tok-from-dotenv' zone='zone-from-dotenv'
  if [ "$creds" = "empty" ]; then
    # prod's measured state on 2026-11-09: the NAMES are in the compose .env and
    # their VALUES are empty (`printenv … | wc -c` = 1), so `printenv | grep`
    # reports them as provisioned.
    tok=''
    zone=''
  fi
  mkdir -p "$d/scripts" "$d/deploy" "$d/api" "$compose"
  cp "$REPO_ROOT/$BUILD_SHIP" "$d/$BUILD_SHIP"
  cp "$REPO_ROOT/$PURGE_SCRIPT" "$d/$PURGE_SCRIPT"
  : >"$d/api/Dockerfile"

  # The compose `.env` of the (stub) deployment host: values are QUOTED (so the
  # strip is measured), the apex is listed beside the wildcard (the fix case 3
  # names), and UNRELATED_CMD must never run — the stream READS .env by name.
  cat >"$compose/.env" <<ENVEOF
APP_BASE_URL="https://jomform.com"
JF_CF_PURGE_TOKEN="$tok"
JF_CF_PURGE_ZONE_ID="$zone"
JF_CF_PURGE_HOSTS="*.jomform.com,jomform.com"
JF_CF_PURGE_API_BASE="$api"
UNRELATED_CMD=\$(touch "$decoy")
ENVEOF

  # The release-tag rule: this harness measures § 8c, not the tag rule (that is
  # deploy/release_selftest.sh's subject), so the version only has to be stable.
  cat >"$d/deploy/require-release-tag.sh" <<'RRT'
#!/usr/bin/env bash
printf 'v9.9.9\n'
RRT
  chmod +x "$d/deploy/require-release-tag.sh"

  # The ship proof, replaced by a switch: cases 4 and 5 differ in nothing else.
  cat >"$d/deploy/ship_proof.sh" <<'SP'
ship_proof_target() {
  SP_HOST=stub.invalid
  SP_USER=root
  SP_PORT=22
  SP_DEST="root@stub.invalid"
  SP_COMPOSE_DIR="${STUB_COMPOSE_DIR:?}"
  SP_SSH_OPTS=()
  SP_TARGET_KIND="STUB"
  return 0
}
ship_proof_live() {
  [ "${STUB_PROOF_RC:-0}" = "0" ] && return 0
  printf 'ship_proof: STUB says the %s service is NOT running %s\n' "${1:-}" "${3:-}" >&2
  return 1
}
SP
  cat >"$d/scripts/gate_result.sh" <<'GR'
gr_short_sha() { printf '%.7s\n' "$1"; }
GR
  cat >"$d/scripts/preflight_reuse.sh" <<'PR'
preflight_reuse_decision() {
  PREFLIGHT_TREE="stubtree1234567890"
  PREFLIGHT_RECORD="<none>"
  PREFLIGHT_EVIDENCE="no reusable record for tree stubtre — gating: stub"
  PREFLIGHT_DECISION="none"
  PREFLIGHT_SKIP=""
  PREFLIGHT_RERUN=""
  PREFLIGHT_REMEMBERED_SECONDS=0
  return 1
}
PR
  cat >"$d/scripts/preflight.sh" <<'PF'
#!/usr/bin/env bash
if [ "${1:-}" = "--list" ]; then printf 'build\nvet\n'; exit 0; fi
printf 'STUB preflight: 1 passed, 0 failed in 0s\n'
exit 0
PF
  chmod +x "$d/scripts/preflight.sh"
  git -C "$d" init -q
  git -C "$d" add -A
  git -C "$d" -c user.email=bite@example.invalid -c user.name=biteproof commit -qm sandbox
}

# The PATH stubs: docker builds nothing, scp copies nothing, and ssh EXECUTES the
# stream it is handed on stdin — that is the deployment host, minus the network.
make_bins() { # make_bins <dir>
  local b="$1"
  mkdir -p "$b"
  cat >"$b/docker" <<'DOCKER'
#!/usr/bin/env bash
case "${1:-}" in
  image) printf 'sha256:0000000000000000000000000000000000000000000000000000000000000000\n'; exit 0 ;;
  save)  cat /dev/null; exit 0 ;;
  *)     exit 0 ;;
esac
DOCKER
  cat >"$b/scp" <<'SCP'
#!/usr/bin/env bash
exit 0
SCP
  cat >"$b/ssh" <<'SSH'
#!/usr/bin/env bash
set -uo pipefail
# Record what the caller handed us, then act like the deployment host.
printf 'SSH_ARGV: %s\n' "$*" >>"${SSH_STUB_LOG:?}"
if [ "${@: -1}" = "bash -s" ]; then
  t="$(mktemp "${TMPDIR:-/tmp}/ssh-stream.XXXXXX")"
  cat >"$t"
  cp "$t" "${SSH_STUB_STREAM:?}"
  rc=0
  if [ "${STUB_SSH_EXEC:-0}" = "1" ]; then bash "$t" || rc=$?; fi
  rm -f "$t"
  exit "$rc"
fi
# The recreate half: recorded, not executed (this harness measures § 8c).
exit 0
SSH
  chmod +x "$b/docker" "$b/scp" "$b/ssh"
}
make_bins "$WORK/bin"

# run_ship <dir> <logdir> <out> <err> [env...]
run_ship() {
  local d="$1" logs="$2" out="$3" err="$4"
  shift 4
  mkdir -p "$logs"
  ( cd "$d" && env -u JF_CF_PURGE_TOKEN -u JF_CF_PURGE_ZONE_ID -u JF_CF_PURGE_HOSTS \
      -u JF_CF_PURGE_API_BASE -u APP_BASE_URL \
      PATH="$WORK/bin:$PATH" \
      LOCK_FILE="$logs/build.lock" OUT_DIR="$logs/out" \
      SSH_HOST="stub.invalid" SSH_USER=root SSH_PORT=22 SSH_PATH=/tmp \
      REQUIRE_SHIP=1 GATE_CPU_QUOTA="" SKIP_RELEASE_SELFTEST=1 \
      SSH_STUB_LOG="$logs/ssh.log" SSH_STUB_STREAM="$logs/ssh.stream" \
      "$@" \
      bash "$d/$BUILD_SHIP" >"$out" 2>"$err" )
}

# proven_roll_checks <logdir> <cdn-log> <decoy-file> — the assertions case 5 must PASS
# and case 6 must FAIL. Sets CHECKS_WHY to the first reason it did not hold.
proven_roll_checks() {
  local logs="$1" cdn="$2" decoy="$3" stream="$1/ssh.stream" log="$1/ssh.log" tail_sha
  CHECKS_WHY=""
  [ -r "$log" ] || { CHECKS_WHY="no ssh log"; return 1; }
  grep -q 'SSH_ARGV: .*bash -s' "$log" \
    || { CHECKS_WHY="build-and-ship never piped a stream to the deploy host (§ 8c missing or unreachable)"; return 1; }
  [ -s "$stream" ] || { CHECKS_WHY="the stream it piped was empty"; return 1; }
  grep -qF 'JF_CF_PURGE_TOKEN="$(unq "$(val JF_CF_PURGE_TOKEN)")"' "$stream" \
    || { CHECKS_WHY="the stream does not read the purge credentials out of the host's .env"; return 1; }
  tail_sha="$(tail -c "$(stat -c %s "$REPO_ROOT/$PURGE_SCRIPT")" "$stream" | sha256sum | awk '{print $1}')"
  [ "$tail_sha" = "$WANT_PURGE_SHA" ] \
    || { CHECKS_WHY="the stream does not end with this tree's $PURGE_SCRIPT bytes (${tail_sha:0:12} != ${WANT_PURGE_SHA:0:12})"; return 1; }
  [ -s "$cdn" ] || { CHECKS_WHY="the purge request never reached the CDN"; return 1; }
  case "$(cdn_meta "$cdn")" in
    *'Bearer tok-from-dotenv'*) : ;;
    *) CHECKS_WHY="the request did not carry the token found in the host's .env"; return 1 ;;
  esac
  [ "$(cdn_body "$cdn")" = '{"prefixes":["https://jomform.com/_next/static/"]}' ] \
    || { CHECKS_WHY="the request body was not the apex /_next/static/ prefix: $(cdn_body "$cdn")"; return 1; }
  [ -e "$decoy" ] && { CHECKS_WHY="an UNRELATED .env line was EXECUTED — the .env was sourced, not read"; return 1; }
  return 0
}

# ============================================================================
# --- case 4
# A FAILED roll purges NOTHING: the old image is still serving, so a purge would
# refill the edge from it. build-and-ship must exit 7 and never reach § 8c.
# ============================================================================
echo
echo "--- case 4: a FAILED roll purges nothing"
PORT="$(start_cdn accept "$WORK/c4.cdn")"
make_sandbox "$WORK/c4" "$WORK/c4-compose" "http://127.0.0.1:$PORT/client/v4" "$WORK/c4-decoy-fired"
run_ship "$WORK/c4" "$WORK/c4-logs" "$WORK/c4.out" "$WORK/c4.err" \
  STUB_COMPOSE_DIR="$WORK/c4-compose" STUB_PROOF_RC=1
RC=$?
stop_cdn
[ "$RC" -eq 7 ] && ok "case 4: an unproven ship exits 7" || bad "case 4: exit $RC, expected 7 (the proof must refuse)"
grep -q 'SHIP NOT PROVEN' "$WORK/c4.err" && ok "case 4: the refusal names the unproven ship" \
  || bad "case 4: the run did not refuse with 'SHIP NOT PROVEN'"
grep -q 'SSH_ARGV: .*docker load' "$WORK/c4-logs/ssh.log" 2>/dev/null \
  && ok "case 4: the roll really ran (the recreate reached the deploy host)" \
  || bad "case 4: the recreate never ran, so 'no purge' below would prove nothing"
grep -q 'SSH_ARGV: .*bash -s' "$WORK/c4-logs/ssh.log" 2>/dev/null \
  && bad "case 4: § 8c ran on a FAILED roll (a stream was piped to the deploy host)" \
  || ok "case 4: no purge stream was piped after the failed roll"
[ -s "$WORK/c4.cdn" ] && bad "case 4: a purge request was made after a FAILED roll" \
  || ok "case 4: the CDN was not contacted after a failed roll"

# ============================================================================
# --- case 5
# A PROVEN roll purges: the stream is this tree's script, the credentials come out
# of the host's `.env`, and the request really lands (the ssh stub executes it).
# ============================================================================
echo
echo "--- case 5: a PROVEN roll purges the apex host with the token from the host's .env"
PORT="$(start_cdn accept "$WORK/c5.cdn")"
make_sandbox "$WORK/c5" "$WORK/c5-compose" "http://127.0.0.1:$PORT/client/v4" "$WORK/c5-decoy-fired"
run_ship "$WORK/c5" "$WORK/c5-logs" "$WORK/c5.out" "$WORK/c5.err" \
  STUB_COMPOSE_DIR="$WORK/c5-compose" STUB_PROOF_RC=0 STUB_SSH_EXEC=1
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 5: a proven ship still exits 0" || bad "case 5: exit $RC, expected 0"
if proven_roll_checks "$WORK/c5-logs" "$WORK/c5.cdn" "$WORK/c5-decoy-fired"; then
  ok "case 5: § 8c piped THIS TREE's script, read .env by name, and the CDN got the prefix"
else
  bad "case 5: $CHECKS_WHY"
fi
grep -qF "$CANON" "$WORK/c5.out" "$WORK/c5.err" \
  && bad "case 5: a SUCCESSFUL purge still printed the 'edge not purged' warning" \
  || ok "case 5: no spurious 'edge not purged' warning on a successful purge"

# ============================================================================
# --- case 6
# THE INSTRUMENT'S OWN BITE. Case 5's checks must go RED when § 8c's call is
# neutered, or the instrument is green because it measures nothing.
# ============================================================================
echo
echo "--- case 6: neutering § 8c's call makes case 5's checks go RED"
PORT="$(start_cdn accept "$WORK/c6.cdn")"
make_sandbox "$WORK/c6" "$WORK/c6-compose" "http://127.0.0.1:$PORT/client/v4" "$WORK/c6-decoy-fired"
# The sabotage: the CALL becomes a no-op, with the function left in place (deleting
# the function would fail for a shell reason instead of the behaviour).
perl -0pi -e 's/^  purge_static_edge$/  :  # SABOTAGE: the 8c call neutered/m' "$WORK/c6/$BUILD_SHIP"
grep -q 'SABOTAGE: the 8c call neutered' "$WORK/c6/$BUILD_SHIP" \
  || bad "case 6: the sabotage did not land on the call site"
# COMMIT IT. build-and-ship refuses a dirty tree at § 2, so an uncommitted sabotage
# would make this case red for "working tree is dirty" — a bite that proves nothing,
# which is the failure mode this case exists to rule out.
git -C "$WORK/c6" -c user.email=bite@example.invalid -c user.name=biteproof \
  commit -qam "sabotage: the 8c call neutered" \
  || bad "case 6: could not commit the sabotage"
git -C "$WORK/c6" status --porcelain | grep -q . \
  && bad "case 6: the sabotaged tree is still dirty — the run would refuse at § 2" \
  || ok "case 6: the sabotage is committed (the run cannot fail for a dirty tree)"
run_ship "$WORK/c6" "$WORK/c6-logs" "$WORK/c6.out" "$WORK/c6.err" \
  STUB_COMPOSE_DIR="$WORK/c6-compose" STUB_PROOF_RC=0 STUB_SSH_EXEC=1
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 6: the sabotaged tree still SHIPS (only the purge is gone)" \
  || bad "case 6: the sabotaged run exited $RC — it failed for something other than the missing call"
grep -q 'SSH_ARGV: .*docker load' "$WORK/c6-logs/ssh.log" 2>/dev/null \
  && ok "case 6: its roll really ran (the missing purge is the only difference)" \
  || bad "case 6: the sabotaged run never reached the roll"
if proven_roll_checks "$WORK/c6-logs" "$WORK/c6.cdn" "$WORK/c6-decoy-fired"; then
  bad "case 6: the checks PASSED on a tree whose § 8c call is gone — the instrument does not bite"
else
  ok "case 6: the checks went RED with the call neutered ($CHECKS_WHY)"
fi

# ============================================================================
# --- case 7
# PROD'S CURRENT STATE, END TO END: the deployment host's .env carries the two
# variable NAMES with EMPTY values. A proven roll must still exit 0, must print the
# canonical warning into the ship's own log, and must not invent a purge.
# ============================================================================
echo
echo "--- case 7: prod today (token/zone present but EMPTY) -> loud warning, ship still exits 0"
PORT="$(start_cdn accept "$WORK/c7.cdn")"
make_sandbox "$WORK/c7" "$WORK/c7-compose" "http://127.0.0.1:$PORT/client/v4" "$WORK/c7-decoy-fired" empty
run_ship "$WORK/c7" "$WORK/c7-logs" "$WORK/c7.out" "$WORK/c7.err" \
  STUB_COMPOSE_DIR="$WORK/c7-compose" STUB_PROOF_RC=0 STUB_SSH_EXEC=1
RC=$?
stop_cdn
[ "$RC" -eq 0 ] && ok "case 7: an unprovisioned purge does NOT fail the release" \
  || bad "case 7: exit $RC — an empty token must never fail a release that already shipped"
grep -qF "$CANON" "$WORK/c7.out" "$WORK/c7.err" \
  && ok "case 7: the ship log carries the canonical 'edge not purged' warning" \
  || bad "case 7: the ship log does NOT warn that the edge was left stale"
[ -s "$WORK/c7.cdn" ] && bad "case 7: a purge request was made with an EMPTY token" \
  || ok "case 7: no request was made with empty credentials"
grep -q 'SSH_ARGV: .*bash -s' "$WORK/c7-logs/ssh.log" 2>/dev/null \
  && ok "case 7: § 8c ran (the warning comes from the step, not from its absence)" \
  || bad "case 7: § 8c never ran, so the warning above proves nothing"

# ── the repo is byte-identical ────────────────────────────────────────────────
echo
[ "$(sha256sum "$REPO_ROOT/$PURGE_SCRIPT" | awk '{print $1}')" = "$WANT_PURGE_SHA" ] \
  && ok "restore: $PURGE_SCRIPT is byte-identical (sha256 ${WANT_PURGE_SHA:0:12})" \
  || bad "restore: $PURGE_SCRIPT CHANGED during the run"
[ "$(sha256sum "$REPO_ROOT/$BUILD_SHIP" | awk '{print $1}')" = "$WANT_SHIP_SHA" ] \
  && ok "restore: $BUILD_SHIP is byte-identical (sha256 ${WANT_SHIP_SHA:0:12})" \
  || bad "restore: $BUILD_SHIP CHANGED during the run"

echo
printf 'SUMMARY: %d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ] || exit 1
echo "BITEPROOF PASS"
exit 0
