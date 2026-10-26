#!/usr/bin/env bash
# Self-test for deploy/host/bootstrap-host.sh.
#
# WHAT CAN BREAK HERE, and why each case is the guard it is:
#
#   1. IT CHANGES SOMETHING IN DRY-RUN. The whole reason this script is dry-run by
#      default is that a half-applied bootstrap on a fresh host is worse than a
#      manual one; a dry-run that writes (or that is really `--apply` with a
#      different name) defeats it silently.
#   2. IT ACCEPTS AN ENV FILE THAT CANNOT WORK. A file that is world-readable
#      leaks the DB and MinIO passwords; a file missing a key the compose reads
#      comes up with an EMPTY credential, which the stack reports as nothing at
#      all. The key list is DERIVED FROM THE COMPOSE, so the case that proves this
#      plants a key in a COPY of the compose and expects the script to name it —
#      otherwise the contract is a list someone wrote once.
#   3. IT CLOBBERS EXISTING STATE. `/opt/jomform/compose.yml` on a live host is
#      hand-maintained until it is not; overwriting a DIFFERENT file without
#      `--force` destroys the only copy of whatever was there.
#   4. IT CLAIMS TO HAVE DONE THE PARTS IT DOES NOT DO. DNS, the account
#      credentials and the data move are deliberately out of scope, and the
#      script must SAY so — an operator who believes the bootstrap moved the data
#      cuts over to an empty database.
#   5. THE TEMPLATE IT SENDS THE OPERATOR TO IS NOT IN THE REPO. Measured
#      2026-10-18: `deploy/HOST-SETUP.md` said `cp deploy/vps/.env.prod.example`
#      while no such file had ever been committed, so the first step of a
#      migration was a `cp` that failed — and the only template that did exist
#      (`deploy/.env.example`) is the DEV shape. The guards here are: the file
#      exists, git does not IGNORE it (`.gitignore` has `.env.*`, and a silenced
#      template is the same defect one layer down), its key set EQUALS the
#      compose's in BOTH directions, and a copy of it survives the documented
#      `cp` → `chmod 600` → bootstrap path.
#   6. THE RUNBOOK NAMES A PATH THAT IS NOT THERE. That is the defect above, in
#      general form, and it is cheap to check: every `deploy/...` path named in
#      `deploy/HOST-SETUP.md` must resolve in the repo.
#   7. IT AGREES WITH THE TEMPLATE AND STILL CONTRADICTS THE CODE. Cases 5 and 6
#      compare the compose with the template in BOTH directions, so the two can
#      stay perfectly in step and both be wrong about the API: SESSION_SECRET was
#      required at startup, read into the config struct, and its value reached
#      nothing — and every gate stayed green for months (card t_6f2b4462 removed
#      it; card t_0e8eb983 added the guard). So the contract is derived in the
#      OTHER direction too, from the API SOURCE: every env key the `api` service
#      sets must be READ by non-test Go code, and a key that is read only to
#      prove it is non-empty (the SESSION_SECRET shape) is refused as well. Each
#      check is bite-proofed against a fixture, never against the live tree. Code
#      is what counts: comments are stripped before the read set is derived, so a
#      read that was deleted and left behind in a comment does not pass either.
#
# Hermetic: a stubbed `docker`/`id` on PATH, and `--root` pointed at a temp dir.
# The only things read outside the temp dir are the repo's own files under
# deploy/ (cases 5 and 6) and under api/ (the env-contract cases at 7).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/bootstrap-host.sh"
COMPOSE="$HERE/../vps/compose.prod.yml"
# The API SOURCE, for the env contract in the other direction (case 7).
# Spelled `$HERE/../../api` rather than resolved through a subshell on purpose:
# scripts/preflight.sh DERIVES a gate's inputs from the path literals in its own
# sources, so this literal is what makes the `host` gate's dependency table claim
# api/* — a guard that reads the API source has to re-run when the API source
# changes, or it is a guard nobody runs on the change it guards.
API_SRC="$HERE/../../api"
[ -x "$SCRIPT" ] || { echo "bootstrap-host_test: $SCRIPT is not executable" >&2; exit 1; }
[ -s "$COMPOSE" ] || { echo "bootstrap-host_test: missing $COMPOSE" >&2; exit 1; }
[ -d "$API_SRC" ] || { echo "bootstrap-host_test: missing $API_SRC (the env-contract cases derive the read set from the API source)" >&2; exit 1; }

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

PASS=0; FAIL=0
ok()  { echo "  ok   $1"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $1" >&2; FAIL=$((FAIL + 1)); }

# ── assertions do not use a pipe (card t_242b43f3) ───────────────────────────
# These read `echo "$OUT" | grep -q PAT`. Under this file's `set -euo pipefail`
# that is a RACE, not an assertion: `grep -q` exits the moment it matches and
# closes its end of the pipe, and if echo's write lands after that, echo dies of
# SIGPIPE and the PIPELINE status is 141 — so `… && ok … || bad …` takes the
# `bad` branch and the gate reds naming a substring that IS in $OUT. Measured at
# the gate: two preflight runs red on it (three different assertions), 9 phantom
# rcs of 141 in 5000 evaluations of the real bytes under load. It reads as a
# flake because a plain re-run of the same tree passes.
#   `has`      = the old `grep -q`  (needle appears anywhere in the value)
#   `has_line` = the old `grep -qx` (needle is one COMPLETE line of a multi-line
#                value — the derived-key sets, where a prefix must not count)
# Both are bash pattern comparisons: no second process exists to be signalled,
# so the status depends only on the text. Needles are literal, never regexes.
has()      { case "$1" in *"$2"*) return 0 ;; *) return 1 ;; esac; }
has_line() { case $'\n'"$1"$'\n' in *$'\n'"$2"$'\n'*) return 0 ;; *) return 1 ;; esac; }
# line_of <blob> <needle> — 1-based number of the FIRST line containing the
# needle, or empty. Replaces `echo … | grep -n … | head -1 | cut -d: -f1`: the
# early-exit reader is the same hazard one layer WORSE, because the 141 lands on
# an ASSIGNMENT and `set -e` then aborts the whole suite instead of failing one
# assertion.
line_of() { awk -v needle="$2" 'index($0, needle) { print NR; exit }' <<<"$1"; }

# ── stubs ─────────────────────────────────────────────────────────────────────
mkdir -p "$WORK/bin" "$WORK/nodock"
cat > "$WORK/bin/docker" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  --version)        echo "Docker version 29.7.1, build stub" ;;
  "compose version") echo "Docker Compose version v2.40.0" ;;
  *) exit 0 ;;
esac
EOF
cat > "$WORK/bin/id" <<'EOF'
#!/usr/bin/env bash
# pretend to be an unprivileged operator, so the --apply root guard is testable
[ "${1:-}" = "-u" ] && { echo 1000; exit 0; }
exec /usr/bin/id "$@"
EOF
chmod +x "$WORK/bin/docker" "$WORK/bin/id"

# A PATH with the tools the script needs and deliberately NO docker, for the
# "docker is not installed" case. It must be EXCLUSIVE (prepending a dir cannot
# make docker absent — the real one is still further along PATH), so every tool
# the script calls is symlinked in rather than inherited.
for t in bash sh grep tr sort comm stat mktemp cmp install cp mv date sed awk cat \
         rm dirname basename wc id printf env timeout uname; do
  p="$(command -v "$t" 2>/dev/null || true)"
  [ -n "$p" ] && ln -sf "$p" "$WORK/nodock/$t"
done
ln -sf "$WORK/bin/id" "$WORK/nodock/id"

# A complete env file, generated FROM the compose (so it cannot rot).
grep -oE '\$\{[A-Z_]+' "$COMPOSE" | tr -d '${' | sort -u | sed 's/$/=stub/' > "$WORK/good.env"
chmod 600 "$WORK/good.env"
KEY_COUNT="$(grep -c '=' "$WORK/good.env")"

run() { # run <PATH-prepend> <args...> — the stub dir goes FIRST, the real PATH stays
  local path="$1"; shift
  set +e
  OUT="$(PATH="$path:$PATH" bash "$SCRIPT" "$@" 2>&1)"
  RC=$?
  set -e
}

run_exclusive() { # run_exclusive <dir> <args...> — PATH is ONLY <dir>
  local path="$1"; shift
  set +e
  OUT="$(PATH="$path" bash "$SCRIPT" "$@" 2>&1)"
  RC=$?
  set -e
}

# ── deriving the env contract in the OTHER direction: compose → API source ────
# api_env_keys <compose> — the env keys the `api` SERVICE sets, one per line.
# Scoped to that ONE service block on purpose: postgres, valkey, minio and
# minio-init legitimately carry env the Go process never sees
# (POSTGRES_PASSWORD, MINIO_ROOT_PASSWORD), so a whole-file grep would demand the
# API read keys it cannot.
api_env_keys() {
  awk '
    /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ { svc = $1; sub(/:$/, "", svc); in_svc = (svc == "api"); in_env = 0; next }
    in_svc && /^    environment:[[:space:]]*$/ { in_env = 1; next }
    in_svc && in_env && /^    [A-Za-z]/ { in_env = 0 }
    in_svc && in_env && /^      [A-Z][A-Z0-9_]*:/ { k = $1; sub(/:$/, "", k); print k }
  ' "$1" | sort -u
}

# strip_comments <file> — the file's code with comments removed, string literals
# kept. A commented-out `os.Getenv("KEY")` is NOT a read: the key was taken out of
# the binary and left in the source as a comment, which is the same false green as
# never reading it (measured: commenting out the JF_CF_OAUTH_CLIENT_SECRET read
# kept the text-only version of this case green). Tracks interpreted strings (with
# escapes), line comments and block comments — enough for Go, and it only has to
# remove comments, never to parse them exactly.
strip_comments() {
  awk '
    { out = ""; i = 1; n = length($0)
      while (i <= n) {
        c = substr($0, i, 1)
        if (in_block) {
          if (c == "*" && substr($0, i+1, 1) == "/") { in_block = 0; i += 2 } else { i++ }
          continue
        }
        if (in_str) {
          out = out c
          if (c == "\\") { out = out substr($0, i+1, 1); i += 2; continue }
          if (c == "\"") { in_str = 0 }
          i++; continue
        }
        if (c == "\"") { in_str = 1; out = out c; i++; continue }
        if (c == "/" && substr($0, i+1, 1) == "*") { in_block = 1; i += 2; continue }
        if (c == "/" && substr($0, i+1, 1) == "/") { break }
        out = out c; i++
      }
      print out
    }' "$1"
}

# api_read_keys <api-src> — the env names the API source READS, one per line.
# Non-test files only: a key a TEST sets is not the binary reading it. Comments are
# stripped first, so a read that only survives in a comment does not count.
#   (a) the literal inside an env-read call — os.Getenv("X"),
#       os.LookupEnv("X"), getEnv("X", fallback). getEnv is config.go's
#       default-valued helper and it is how APP_ENV and PORT are read, so they are
#       FOUND here rather than allowlisted.
#   (b) the literal bound to an identifier or struct field whose NAME mentions env
#       — cdnpurge's EnvToken/EnvZoneID/EnvHosts, social.go's IDEnv/SecretEnv.
#       Those sites pass a VARIABLE to os.Getenv, so (a) cannot see them; reading
#       them as "unread" would be a false red, and allowlisting them is the rot
#       this case exists to avoid.
api_read_keys() {
  local dir="$1" f
  {
    find "$dir" -name '*.go' ! -name '*_test.go' -print0 2>/dev/null \
      | while IFS= read -r -d '' f; do strip_comments "$f"; done
  } | { grep -oE '(os\.(Getenv|LookupEnv)|getEnv)\([[:space:]]*"[A-Z][A-Z0-9_]*"|[A-Za-z0-9_]*[Ee][Nn][Vv][A-Za-z0-9_]*[[:space:]]*[:=][[:space:]]*"[A-Z][A-Z0-9_]*"' || true; } \
    | grep -oE '"[A-Z][A-Z0-9_]*"' | tr -d '"' | sort -u || true
}

# config_env_fields <api-src> — "<field> <KEY>" for every field in
# api/internal/config/config.go assigned from an env read. All three shapes in
# that file (`Field: os.Getenv("K")`, `Field: getEnv("K", …)`,
# `Field: parsePositiveInt(os.Getenv("K"), …)`) put the key in a quoted literal on
# the assignment line, so one pass over those lines is enough.
config_env_fields() {
  local cfg="$1/internal/config/config.go"
  if [ ! -s "$cfg" ]; then
    echo "bootstrap-host_test: missing $cfg — the env-contract case cannot derive the field map" >&2
    return 1
  fi
  grep -nE '^[[:space:]]+[A-Z][A-Za-z0-9]*:[[:space:]]' "$cfg" \
    | grep -E 'os\.Getenv\("[A-Z]|getEnv\("[A-Z]' \
    | sed -E 's/^[0-9]+:[[:space:]]+([A-Z][A-Za-z0-9]*):.*\("([A-Z][A-Z0-9_]*)".*/\1 \2/' \
    | sort -u
}

# unconsumed_env_keys <api-src> — keys that are READ and then never CONSUMED.
# Every reference to the field outside its own assignment is inspected: when all
# of them are emptiness tests (`if cfg.X == ""`), the value exists only to prove
# that it exists — the exact shape of SESSION_SECRET, which the "is the key named
# in the source" half cannot catch BECAUSE THE KEY WAS READ. A field with no
# reference at all is the same defect in its simpler form.
unconsumed_env_keys() {
  local dir="$1" field key refs consumers
  config_env_fields "$dir" | while read -r field key; do
    refs="$(grep -rnE --include='*.go' --exclude='*_test.go' "\.$field\b" "$dir" 2>/dev/null \
      | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' || true)"
    if [ -z "$refs" ]; then printf '%s\n' "$key"; continue; fi
    # A reference that is NOT a presence/emptiness test is a CONSUMER.
    consumers="$(printf '%s\n' "$refs" | grep -vE \
      "\.$field[[:space:]]*(==|!=)[[:space:]]*\"\"|\"\"[[:space:]]*(==|!=)[[:space:]]*\.$field|len\(\s*\.$field\s*\)[[:space:]]*(==|!=)[[:space:]]*0|\.$field[[:space:]]*=[[:space:]]*\"\"" || true)"
    if [ -z "$consumers" ]; then printf '%s\n' "$key"; fi
  done
}

echo "case: DRY-RUN is the default, prints the plan, and changes NOTHING"
mkdir -p "$WORK/opt"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
[ "$RC" -eq 0 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 0): $OUT"
has "$OUT" "mode=DRY-RUN" && ok "says it is a dry run" || bad "did not say DRY-RUN"
has "$OUT" "would: install compose.prod.yml → $WORK/opt/jomform/compose.yml" && ok "plans the prod compose install" || bad "did not plan the compose install"
has "$OUT" "env contract: all $KEY_COUNT keys" && ok "checks the env contract against the compose ($KEY_COUNT keys)" || bad "did not report the env contract"
has "$OUT" "dokploy-network" && ok "plans the external network (the compose declares it external)" || bad "did not plan the network"
has "$OUT" "compose.yml up -d" && ok "plans the stack up" || bad "did not plan the stack up"
has "$OUT" "jomform-backup.sh" && ok "plans the nightly-backup cron line" || bad "did not plan the backup cron"
has "$OUT" "jomform-container-prune.sh" && ok "plans the prune cron line" || bad "did not plan the prune cron"
has "$OUT" "DRY-RUN — nothing was changed" && ok "states nothing changed" || bad "did not state nothing changed"
if [ -z "$(ls -A "$WORK/opt" 2>/dev/null)" ]; then ok "the install root is still EMPTY (no write happened)"; else bad "dry-run WROTE to the install root: $(ls -A "$WORK/opt")"; fi

echo "case: it says out loud what it does NOT do (data move, DNS, credentials)"
has "$OUT" "MOVE THE DATA" && ok "names the data move as not-done" || bad "did not name the data move"
has "$OUT" "--restore" && ok "prints the restore command" || bad "did not print the restore command"
has "$OUT" "point DNS at this host" && ok "names DNS as not-done" || bad "did not name DNS"

echo "case: no --env-file is refused (there is no default to guess)"
run "$WORK/bin" --root "$WORK/opt"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected non-zero)"
has "$OUT" "REFUSED — --env-file is required" && ok "names the missing argument" || bad "did not name the missing argument"

echo "case: a missing or empty env file is refused"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/nope.env"
[ "$RC" -ne 0 ] && ok "missing file refused (exit=$RC)" || bad "missing file accepted"
: > "$WORK/empty.env"; chmod 600 "$WORK/empty.env"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/empty.env"
[ "$RC" -ne 0 ] && ok "empty file refused (exit=$RC)" || bad "empty file accepted"

echo "case: a WORLD-READABLE env file is refused (it holds the DB/MinIO passwords)"
cp "$WORK/good.env" "$WORK/loose.env"; chmod 644 "$WORK/loose.env"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/loose.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted a mode-644 credential file"
has "$OUT" "mode 644" && ok "names the mode" || bad "did not name the mode"

echo "case: an env file MISSING a key the compose reads is refused, and the key is NAMED"
grep -v '^SES_SMTP_PASS=' "$WORK/good.env" > "$WORK/missing.env"; chmod 600 "$WORK/missing.env"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/missing.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted an env file with a missing credential"
has "$OUT" "SES_SMTP_PASS" && ok "names the missing key" || bad "did not name the missing key"

echo "case: the key list comes FROM THE COMPOSE — a key added to the compose must be named"
# Plant an extra ${KEY} in a COPY of the compose and pass it with --compose-src.
sed 's/^      APP_ENV: prod$/      APP_ENV: prod\n      PLANTED_KEY: ${PLANTED_KEY}/' "$COMPOSE" > "$WORK/compose.planted.yml"
grep -q "PLANTED_KEY" "$WORK/compose.planted.yml" && ok "the planted compose carries the new key" || bad "the mutation did not apply"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --compose-src "$WORK/compose.planted.yml"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "a compose key no env file can supply was NOT refused"
has "$OUT" "PLANTED_KEY" && ok "names the planted key (the contract is derived, not written)" || bad "did not name the planted key"

echo "case: every env key the compose passes to the api service is READ by the API source"
# The cases above hold the compose and the template in step with each OTHER — and
# that is exactly why SESSION_SECRET rode through them green. They can agree with
# each other and both be wrong about the code. This asks the code.
API_KEYS="$(api_env_keys "$COMPOSE")"
READ_KEYS="$(api_read_keys "$API_SRC")"
API_KEYS_N="$(printf '%s\n' "$API_KEYS" | grep -c '[^[:space:]]' || true)"
READ_KEYS_N="$(printf '%s\n' "$READ_KEYS" | grep -c '[^[:space:]]' || true)"
if [ "${API_KEYS_N:-0}" -lt 10 ] || [ "${READ_KEYS_N:-0}" -lt 10 ]; then
  bad "the derivation produced almost nothing (api-service keys=$API_KEYS_N, read names=$READ_KEYS_N) — a deriver that has gone blind cannot be a green guard"
else
  ok "derived $API_KEYS_N api-service key(s) from the compose and $READ_KEYS_N name(s) read by api/ source"
fi
LEAKED="$(printf '%s\n' "$API_KEYS" | grep -xE 'POSTGRES_PASSWORD|MINIO_ROOT_PASSWORD|JOMFORM_API_URL' || true)"
[ -z "$LEAKED" ] && ok "the key set is SCOPED to the api block (no other service's env is demanded of the API)" \
  || bad "the api key set includes another service's key(s): $(printf '%s' "$LEAKED" | tr '\n' ' ')"
# The one escape hatch, and it is deliberately empty: a key belongs here only when
# the CONTAINER RUNTIME reads it (a timezone, a healthcheck's variable) and the Go
# process legitimately never will. A key the API simply forgot to read is the
# defect this case exists for, not an allowlist entry. Every entry is checked
# below, so an exemption for a key the compose has stopped passing also reds.
API_ENV_RUNTIME_ONLY=""
STALE_ALLOW=""
for k in $API_ENV_RUNTIME_ONLY; do
  has_line "$API_KEYS" "$k" || STALE_ALLOW="${STALE_ALLOW} $k"
done
[ -z "$STALE_ALLOW" ] && ok "the runtime-only allowlist is ${API_ENV_RUNTIME_ONLY:-EMPTY} and names no key the compose has stopped passing" \
  || bad "the runtime-only allowlist names key(s) the compose no longer passes (a stale exemption is an unchecked skip):${STALE_ALLOW}"
UNREAD="$(comm -23 <(printf '%s\n' "$API_KEYS") <(printf '%s\n' "$READ_KEYS" $API_ENV_RUNTIME_ONLY))"
[ -z "$UNREAD" ] && ok "no api key is passed to the API and read by no non-test source file" \
  || bad "the compose passes key(s) no API code reads: $(printf '%s' "$UNREAD" | tr '\n' ' ')"

echo "case: that READ check BITES — an unread key planted in a COPY of the compose is NAMED"
sed 's/^      APP_ENV: prod$/      APP_ENV: prod\n      PLANTED_UNREAD_KEY: ${PLANTED_UNREAD_KEY}/' "$COMPOSE" > "$WORK/compose.unread.yml"
grep -q 'PLANTED_UNREAD_KEY' "$WORK/compose.unread.yml" \
  && ok "the planted compose carries the new key" \
  || bad "the mutation did not apply — the assertion below would pass vacuously"
PLANTED_UNREAD="$(comm -23 <(api_env_keys "$WORK/compose.unread.yml") <(printf '%s\n' "$READ_KEYS"))"
has_line "$PLANTED_UNREAD" 'PLANTED_UNREAD_KEY' \
  && ok "the check names the planted key ($(printf '%s' "$PLANTED_UNREAD" | tr '\n' ' '))" \
  || bad "an unread key planted in the api service's environment was NOT named — the check cannot see the class it exists for"

echo "case: a read that only survives in a COMMENT is not a read"
# Measured on the live tree while building this case: commenting out the
# `JF_CF_OAUTH_CLIENT_SECRET` read in api/internal/httpapi/cf_oauth.go left every
# assertion green, because the literal was still in the comment. Text is not code.
mkdir -p "$WORK/commentsrc/internal/x"
cat > "$WORK/commentsrc/internal/x/x.go" <<'GOFIX'
package x

import "os"

// token := os.Getenv("LINE_COMMENTED_KEY")
/*
	old := os.Getenv("BLOCK_COMMENTED_KEY")
*/
var Token = os.Getenv("LIVE_READ_KEY")
GOFIX
COMMENT_KEYS="$(api_read_keys "$WORK/commentsrc")"
has_line "$COMMENT_KEYS" 'LIVE_READ_KEY' \
  && ok "still sees a live read" \
  || bad "lost the live read in the same fixture — the exclusion below would prove nothing"
for ghost in LINE_COMMENTED_KEY BLOCK_COMMENTED_KEY; do
  has_line "$COMMENT_KEYS" "$ghost" \
    && bad "counted $ghost as read, though it appears only in a comment — a commented-out read is not a read" \
    || ok "does not count $ghost (it appears only in a comment)"
done

echo "case: a key that is READ and then never CONSUMED is refused (the SESSION_SECRET shape)"
# The half above cannot see this shape: SESSION_SECRET was read (os.Getenv) into
# config, and its value reached nothing. A grep for the key says "read" and the
# gate stays green, which is precisely how it rode through for months.
# Scoped to api/internal/config/ on purpose: that is the one place this repo turns
# env into typed config, so it is where a value can be stored and then orphaned.
# A key read anywhere else is consumed at the point it is read.
FIELD_MAP_N="$(config_env_fields "$API_SRC" | grep -c '[^[:space:]]' || true)"
[ "${FIELD_MAP_N:-0}" -ge 20 ] \
  && ok "derived $FIELD_MAP_N config field(s) assigned from an env read" \
  || bad "derived only ${FIELD_MAP_N:-0} field(s) from api/internal/config/config.go — the map is broken, so 'nothing unconsumed' would prove nothing"
UNCONSUMED="$(unconsumed_env_keys "$API_SRC")"
[ -z "$UNCONSUMED" ] && ok "every key read into the config is consumed by code, not just checked for emptiness" \
  || bad "these key(s) are read and nothing consumes them: $(printf '%s' "$UNCONSUMED" | tr '\n' ' ')"
# The bite: the historical shape in a FIXTURE (the live tree is never mutated), with
# a consumed field beside it as the control so the red stays attributable.
mkdir -p "$WORK/bitesrc/internal/config" "$WORK/bitesrc/internal/dbstore"
cat > "$WORK/bitesrc/internal/config/config.go" <<'GOFIX'
package config

import (
	"fmt"
	"os"
)

type Config struct {
	SessionSecret string
	DatabaseURL   string
}

func Load() *Config {
	return &Config{
		SessionSecret: os.Getenv("SESSION_SECRET"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
	}
}

// The shape card t_6f2b4462 removed: the value is read at startup and reaches
// nothing — the only reference to it proves it is not empty.
func (c *Config) Req() error {
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}
	return nil
}
GOFIX
cat > "$WORK/bitesrc/internal/dbstore/store.go" <<'GOFIX'
package dbstore

import "example/internal/config"

// A real consumer of a config value: the control arm, and the reason the fixture
// is not "flag every field".
func Open(c *config.Config) { _ = c.DatabaseURL }
GOFIX
BITESRC="$(unconsumed_env_keys "$WORK/bitesrc")"
has_line "$BITESRC" 'SESSION_SECRET' \
  && ok "names SESSION_SECRET (read at startup, its value consumed by nothing)" \
  || bad "did NOT name SESSION_SECRET in the historical shape — the check cannot see the defect it was filed for"
has_line "$BITESRC" 'DATABASE_URL' \
  && bad "also flagged DATABASE_URL, which dbstore consumes — the check is not reading consumption, so its red would not be attributable" \
  || ok "does not flag DATABASE_URL (a consumed field) — the red above is about consumption, not about being in a struct"

echo "case: the repo SHIPS the env template the runbook tells an operator to copy"
EXAMPLE="$HERE/../vps/.env.prod.example"
ROOT_DIR_REPO="$(cd "$HERE/../.." && pwd)"
if [ ! -s "$EXAMPLE" ]; then
  bad "deploy/vps/.env.prod.example is missing — the runbook's first migration step is a cp that cannot work"
else
  ok "deploy/vps/.env.prod.example exists and is not empty"
  # ...and git does not IGNORE it. A silenced template is the same defect one
  # layer down: it sits in the author's tree, this harness reads it, and the PR
  # does not contain it. Probe the PATTERNS (--no-index), not the index, so the
  # question is still asked after the file is tracked.
  if git -C "$HERE" rev-parse --git-dir >/dev/null 2>&1; then
    if git -C "$HERE" check-ignore --no-index -q "$EXAMPLE" 2>/dev/null; then
      bad "deploy/vps/.env.prod.example is GITIGNORED — it would never be committed"
    else
      ok "not gitignored (it actually ships)"
    fi
  fi

  echo "case: the template's key set EQUALS the compose's, in BOTH directions"
  EXAMPLE_KEYS="$(grep -oE '^[A-Z_]+=' "$EXAMPLE" | tr -d '=' | sort -u)"
  COMPOSE_KEYS="$(grep -oE '\$\{[A-Z_]+' "$COMPOSE" | tr -d '${' | sort -u)"
  MISSING_FROM_EXAMPLE="$(comm -23 <(echo "$COMPOSE_KEYS") <(echo "$EXAMPLE_KEYS"))"
  STALE_IN_EXAMPLE="$(comm -13 <(echo "$COMPOSE_KEYS") <(echo "$EXAMPLE_KEYS"))"
  [ -z "$MISSING_FROM_EXAMPLE" ] && ok "no key the compose reads is missing from the example" \
    || bad "the example is missing compose key(s): $(echo "$MISSING_FROM_EXAMPLE" | tr '\n' ' ')"
  [ -z "$STALE_IN_EXAMPLE" ] && ok "the example carries no key the compose does not read" \
    || bad "the example carries stale key(s): $(echo "$STALE_IN_EXAMPLE" | tr '\n' ' ')"

  echo "case: the documented procedure (cp example → chmod 600 → bootstrap) works"
  cp "$EXAMPLE" "$WORK/copied.env"
  chmod 600 "$WORK/copied.env"
  run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/copied.env"
  [ "$RC" -eq 0 ] && ok "the bootstrap accepts a copy of the shipped example (exit=$RC)" \
    || bad "the bootstrap REFUSED a copy of the shipped example (exit=$RC): $OUT"
  has "$OUT" "env contract: all $KEY_COUNT keys" \
    && ok "and reports the derived key count ($KEY_COUNT)" \
    || bad "did not report the derived key count"
fi

echo "case: every deploy/ path named in the runbook exists in the repo"
RUNBOOK="$HERE/../HOST-SETUP.md"
if [ ! -s "$RUNBOOK" ]; then
  bad "deploy/HOST-SETUP.md is missing"
else
  MISSING_PATHS=""
  while IFS= read -r p; do
    [ -e "$ROOT_DIR_REPO/$p" ] || MISSING_PATHS="$MISSING_PATHS $p"
  done < <(grep -oE 'deploy/[A-Za-z0-9_./-]+' "$RUNBOOK" | sed 's/[.,)]$//' | sort -u)
  [ -z "$MISSING_PATHS" ] && ok "every deploy/... path it names resolves" \
    || bad "the runbook names path(s) that do not exist:$MISSING_PATHS"
fi

echo "case: a host that is not running docker is refused"
run_exclusive "$WORK/nodock" --root "$WORK/opt" --env-file "$WORK/good.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted a host with no docker"
has "$OUT" "docker is not installed" && ok "names the missing prerequisite" || bad "did not name the missing prerequisite"

echo "case: --apply as a non-root user is refused"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --apply
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted --apply as an unprivileged user"
has "$OUT" "--apply needs root" && ok "names the root requirement" || bad "did not name the root requirement"

echo "case: an existing DIFFERING file is not clobbered without --force"
mkdir -p "$WORK/opt/jomform"
echo "hand-edited on the host" > "$WORK/opt/jomform/compose.yml"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "clobbered a differing live file"
has "$OUT" "refusing to clobber" && ok "says why it refused" || bad "did not say why"
grep -q "hand-edited on the host" "$WORK/opt/jomform/compose.yml" && ok "the existing file is untouched" || bad "the existing file was modified"

echo "case: with --force the same host plans a BACKUP before the install"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --force
[ "$RC" -eq 0 ] && ok "exit code (exit=$RC)" || bad "still refused with --force: $OUT"
has "$OUT" "would: back up the differing" && ok "plans the backup" || bad "did not plan a backup"
BK_LINE="$(line_of "$OUT" "would: back up the differing")"
IN_LINE="$(line_of "$OUT" "would: install compose.prod.yml")"
[ -n "$BK_LINE" ] && [ -n "$IN_LINE" ] && [ "$BK_LINE" -lt "$IN_LINE" ] && ok "the backup is planned BEFORE the install (order matters)" || bad "the install is planned before the backup"

echo "case: an already-identical file is a no-op, not a backup churn"
cp "$COMPOSE" "$WORK/opt/jomform/compose.yml"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
has "$OUT" "already identical: $WORK/opt/jomform/compose.yml" && ok "says it is already identical (idempotent)" || bad "did not report the identical file"

echo "case: this suite does not assert through a pipe (the phantom red cannot return)"
# Every case above passes even if a piped early-exit reader is back in this file,
# because the race is rare — so the file checks ITSELF, the shape
# scripts/preflight_test.sh uses for its own header claim. A piped `grep -q` is a
# status decided by a race; a piped `head -n1` is the same early-exit reader and,
# under `set -e`, its 141 aborts the suite instead of failing one assertion.
# Only this file is asserted; comment lines are skipped because the block above
# necessarily names the idiom it forbids.
SELF="$HERE/$(basename "${BASH_SOURCE[0]}")"
LIVE_PIPED="$(grep -nE '(echo|printf)[^|]*\|[[:space:]]*grep[[:space:]]+-[A-Za-z]*q|\|[[:space:]]*head[[:space:]]+-n?[[:space:]]*1' "$SELF" 2>/dev/null \
  | grep -vE '^[0-9]+:[[:space:]]*#' | grep -v 'grep -nE' || true)"
if [ -z "$LIVE_PIPED" ]; then
  ok "this file has no live piped early-exit reader (no assertion status decided by a race)"
else
  bad "a piped early-exit reader is back in this file, so its status is a race and a MATCH can read as a miss: $LIVE_PIPED"
fi

echo
echo "bootstrap-host_test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
