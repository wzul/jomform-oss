#!/usr/bin/env bash
#
# deploy/selfhost/bootstrap_test.sh — self-test for the one-command self-host
# bootstrap and the artifacts it depends on (deploy/docker-compose.yml,
# deploy/selfhost/Caddyfile, deploy/.env.example).
#
# WHY THIS EXISTS
#   The bootstrap is the only thing standing between a stranger's clean host and
#   a deployment that comes up HEALTHY WITH A KEY EVERYBODY KNOWS. That failure
#   is silent in every direction: the stack reports healthy, the pages render,
#   and the only symptom is that the data is readable by anyone who has seen the
#   repository. So the refusal is not documented — it is executed here, in both
#   directions:
#     * a placeholder secret is REFUSED, named, and left on disk untouched;
#     * a file with no placeholder still gets started (a refusal that refuses
#       everything is not a guard, it is an outage).
#
# WHAT ELSE IS PINNED, and why each one has a real failure behind it:
#   * the compose must contain the WEB service. The pre-self-host compose shipped
#     api + postgres + valkey + minio and no web at all, so a stranger could get
#     an API and no product.
#   * nothing but the front door may be published to the host: the API, Postgres,
#     Valkey and MinIO are reachable only on the compose network. A published
#     5432/6379/9000 is an unauthenticated database on the internet.
#   * the Caddyfile's path split must match what the API actually serves
#     (/f/, /t/, /v1/, /media/, /embed/, /oauth/, /.well-known/oauth-*, /healthz)
#     and must NOT swallow /api/ — that is the dashboard's same-origin proxy, and
#     sending it to the API directly breaks the session cookie. Both sides of the
#     split are asserted, and the "everything else" default is asserted to be web.
#   * the install path must carry none of OUR hosts. A self-host instruction that
#     names our production IP is not a self-host instruction.
#
# Hermetic: a stubbed `docker` on PATH records every invocation, so "did it start
# anything" is read from a log instead of trusted. No daemon, no network, no
# image pull. The ONE real-docker step is `docker compose config`, which only
# parses and interpolates (no daemon contact) — it is the authoritative answer to
# "does this file accept this env", and a missing docker is a FAILURE, never a
# skip.
#
# Run:  bash deploy/selfhost/bootstrap_test.sh
# Exit: 0 when every property holds, 1 otherwise.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
BOOTSTRAP="$HERE/bootstrap.sh"
COMPOSE="$REPO_ROOT/deploy/docker-compose.yml"
CADDYFILE="$HERE/Caddyfile"
ENV_EXAMPLE="$REPO_ROOT/deploy/.env.example"
SELFHOST_DOC="$REPO_ROOT/docs/SELF-HOST.md"
README="$REPO_ROOT/README.md"
# The hosted production stack, "as installed". Present only to assert it does NOT
# declare self-host mode: setting JOMFORM_SELF_HOSTED there would serve merchant
# forms on the product apex (issue #77).
HOSTED_COMPOSE="$REPO_ROOT/deploy/vps/compose.prod.yml"

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$1"; }
check() { # desc expected actual
	if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$3', want '$2')"; fi
}
contains() { # desc haystack needle — herestring, never `| grep -q` (SIGPIPE)
	if grep -qF -- "$3" <<<"$2"; then ok "$1"; else bad "$1 (missing '$3')"; fi
}
not_contains() {
	if grep -qF -- "$3" <<<"$2"; then bad "$1 (unexpectedly contains '$3')"; else ok "$1"; fi
}

for f in "$BOOTSTRAP" "$COMPOSE" "$CADDYFILE" "$ENV_EXAMPLE" "$SELFHOST_DOC" "$README"; do
	[ -r "$f" ] || {
		echo "FAIL: required file missing: $f" >&2
		exit 1
	}
done

SB="$(mktemp -d)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

# ── the stubbed docker ──────────────────────────────────────────────────────
# Every invocation is appended to $SB/docker.log, one argv per line. The script
# under test asks `docker compose version` first, so that has to answer like the
# real plugin; everything else is a recorded no-op.
mkdir -p "$SB/bin"
cat >"$SB/bin/docker" <<STUB
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"$SB/docker.log"
if [ "\${1:-}" = "compose" ] && [ "\${2:-}" = "version" ]; then
  echo "Docker Compose version v2.99.0"
fi
exit 0
STUB
chmod +x "$SB/bin/docker"

# run_bootstrap <env-file> [args...] — run the real script with the stub on PATH
# and stdin NOT a terminal (so the interactive prompt cannot fire by accident).
run_bootstrap() {
	local env_file="$1"
	shift
	PATH="$SB/bin:$PATH" bash "$BOOTSTRAP" --env-file "$env_file" "$@" </dev/null 2>&1
}

docker_log_reset() { : >"$SB/docker.log"; }
docker_calls() { grep -cF -- "$1" "$SB/docker.log" 2>/dev/null || true; }

# ═══ 1. IT REFUSES A PLACEHOLDER SECRET ═════════════════════════════════════
echo "--- 1. a placeholder secret is refused, named, and left alone"
# 1a. The exact value the pre-self-host compose DEFAULTED to. A deployment that
#     accepts this one is the defect the card was filed for.
ENVF="$SB/placeholder-enc.env"
printf '%s\n' \
	"POSTGRES_USER=jomform" \
	"POSTGRES_PASSWORD=$(printf 'a%.0s' $(seq 1 64))" \
	"POSTGRES_DB=jomform" \
	"MINIO_ROOT_USER=jomform" \
	"S3_SECRET_KEY=$(printf 'b%.0s' $(seq 1 64))" \
	"ENCRYPTION_KEY=dev_only_32byte_key_change_me_pls" \
	"APP_BASE_URL=https://forms.example.test" >"$ENVF"
before="$(sha256sum "$ENVF" | cut -d' ' -f1)"
docker_log_reset
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
after="$(sha256sum "$ENVF" | cut -d' ' -f1)"
[ "$RC" -ne 0 ] && contains "a compose-era default ENCRYPTION_KEY is refused" "$OUT" "ENCRYPTION_KEY" ||
	bad "the bootstrap accepted dev_only_32byte_key_change_me_pls (rc=$RC)"
contains "the refusal says the file was not generated over" "$OUT" "NOT generated over"
check "the refusal did not touch the operator's file" "$before" "$after"
check "the refusal started nothing" 0 "$(docker_calls ' up ')"
check "the refusal validated nothing either" 0 "$(docker_calls ' config ')"

# 1b. The template's own placeholder, which is what a hand-copied .env carries.
ENVF="$SB/placeholder-template.env"
printf '%s\n' \
	"POSTGRES_PASSWORD=$(printf 'c%.0s' $(seq 1 64))" \
	"S3_SECRET_KEY=$(printf 'd%.0s' $(seq 1 64))" \
	"ENCRYPTION_KEY=change_me_encryption_key" \
	"APP_BASE_URL=https://forms.example.test" >"$ENVF"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
[ "$RC" -ne 0 ] && contains "change_me_encryption_key is refused" "$OUT" "ENCRYPTION_KEY" ||
	bad "the bootstrap accepted change_me_encryption_key"

# 1c. A key of the wrong LENGTH — the shape a human types when told "32 bytes".
ENVF="$SB/short-key.env"
printf '%s\n' \
	"POSTGRES_PASSWORD=$(printf 'e%.0s' $(seq 1 64))" \
	"S3_SECRET_KEY=$(printf 'f%.0s' $(seq 1 64))" \
	"ENCRYPTION_KEY=$(printf 'a%.0s' $(seq 1 63))" \
	"APP_BASE_URL=https://forms.example.test" >"$ENVF"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
[ "$RC" -ne 0 ] && contains "a 63-char ENCRYPTION_KEY is refused by length" "$OUT" "must be exactly 64" ||
	bad "the bootstrap accepted a 63-char ENCRYPTION_KEY"

# 1d. A non-hex 64-char key: the length check alone would pass it, and the API
#     would then refuse to start with a message about hex — after a build.
ENVF="$SB/nonhex-key.env"
printf '%s\n' \
	"POSTGRES_PASSWORD=$(printf 'a%.0s' $(seq 1 64))" \
	"S3_SECRET_KEY=$(printf 'b%.0s' $(seq 1 64))" \
	"ENCRYPTION_KEY=$(printf 'z%.0s' $(seq 1 64))" \
	"APP_BASE_URL=https://forms.example.test" >"$ENVF"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
[ "$RC" -ne 0 ] && contains "a 64-char non-hex ENCRYPTION_KEY is refused" "$OUT" "not hex" ||
	bad "the bootstrap accepted a non-hex ENCRYPTION_KEY"

# 1e. Placeholder APP_BASE_URL: the passkey RP ID and the cookie origin both come
#     from it, so a placeholder is a deployment nobody can sign in to.
ENVF="$SB/placeholder-origin.env"
printf '%s\n' \
	"POSTGRES_PASSWORD=$(printf 'a%.0s' $(seq 1 64))" \
	"S3_SECRET_KEY=$(printf 'b%.0s' $(seq 1 64))" \
	"ENCRYPTION_KEY=$(printf '0%.0s' $(seq 1 64))" \
	"APP_BASE_URL=change_me_public_origin" >"$ENVF"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
[ "$RC" -ne 0 ] && contains "a placeholder APP_BASE_URL is refused" "$OUT" "APP_BASE_URL" ||
	bad "the bootstrap accepted change_me_public_origin"

# 1f. Placeholder POSTGRES_PASSWORD / S3_SECRET_KEY.
ENVF="$SB/placeholder-pg.env"
printf '%s\n' \
	"POSTGRES_PASSWORD=change_me_postgres_password" \
	"S3_SECRET_KEY=change_me_s3_secret_key" \
	"ENCRYPTION_KEY=$(printf '0%.0s' $(seq 1 64))" \
	"APP_BASE_URL=https://forms.example.test" >"$ENVF"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
[ "$RC" -ne 0 ] && contains "a placeholder POSTGRES_PASSWORD is refused" "$OUT" "POSTGRES_PASSWORD" ||
	bad "the bootstrap accepted change_me_postgres_password"
[ "$RC" -ne 0 ] && contains "a placeholder S3_SECRET_KEY is refused" "$OUT" "S3_SECRET_KEY" ||
	bad "the bootstrap accepted change_me_s3_secret_key"

# ═══ 2. IT REFUSES WHEN THE ORIGIN IS UNKNOWABLE ════════════════════════════
echo "--- 2. no origin, no file, no guess"
ENVF="$SB/never-created.env"
docker_log_reset
OUT="$(run_bootstrap "$ENVF")"; RC=$?
[ "$RC" -ne 0 ] && contains "a first run without --base-url is refused" "$OUT" "no public origin" ||
	bad "the bootstrap ran without an origin (rc=$RC)"
[ ! -e "$ENVF" ] && ok "that refusal wrote no .env" || bad "that refusal left a half-written .env behind"

# ═══ 3. THE HAPPY PATH ══════════════════════════════════════════════════════
echo "--- 3. a clean run generates every secret and derives the front door"
ENVF="$SB/fresh-https.env"
docker_log_reset
OUT="$(run_bootstrap "$ENVF" --base-url https://forms.example.test --no-start)"; RC=$?
check "the clean run succeeds" 0 "$RC"
[ -f "$ENVF" ] && ok "deploy/.env equivalent exists" || bad "no .env was created"
check "the generated .env is 0600" 600 "$(stat -c '%a' "$ENVF" 2>/dev/null)"
check "APP_BASE_URL is the origin given" "https://forms.example.test" "$(sed -n 's/^APP_BASE_URL=//p' "$ENVF")"
check "an https origin derives APP_ENV=prod (Secure cookies)" "prod" "$(sed -n 's/^APP_ENV=//p' "$ENVF")"
check "SITE_ADDRESS has no port (Caddy listens on 80/443 inside)" "https://forms.example.test" "$(sed -n 's/^SITE_ADDRESS=//p' "$ENVF")"
check "an https origin publishes 443" "443" "$(sed -n 's/^HTTPS_PORT=//p' "$ENVF")"
check "an https origin keeps 80 for the ACME challenge" "80" "$(sed -n 's/^HTTP_PORT=//p' "$ENVF")"
# A self-hosted install has ONE hostname, and that is the declaration the API
# needs before it will serve a published form from its own origin. Miss this key
# and the install comes up healthy, prints the form as live, and answers 404 to
# every buyer — a guard that silently becomes an outage.
check "the install declares self-host mode in the generated .env" "1" "$(sed -n 's/^JOMFORM_SELF_HOSTED=//p' "$ENVF")"
check "the .env declares it exactly once" 1 "$(grep -c '^JOMFORM_SELF_HOSTED=' "$ENVF")"
for key in POSTGRES_PASSWORD S3_SECRET_KEY ENCRYPTION_KEY; do
	val="$(sed -n "s/^${key}=//p" "$ENVF")"
	if printf '%s' "$val" | grep -Eq '^[0-9a-f]{64}$'; then
		ok "$key is 32 bytes of openssl rand -hex 32"
	else
		bad "$key is not 64 hex chars (got ${#val} chars)"
	fi
done
check "the validation pass ran compose config" 1 "$(docker_calls ' config ')"
check "--no-start started nothing" 0 "$(docker_calls ' up ')"

# 3b. An http:// origin on a non-default port: the trial shape. APP_ENV MUST
#     fall back to dev, because a Secure cookie is never returned over plain
#     HTTP and the login would silently not stick.
ENVF="$SB/fresh-http.env"
OUT="$(run_bootstrap "$ENVF" --base-url http://localhost:18080 --no-start)"; RC=$?
check "an http origin run succeeds" 0 "$RC"
check "an http origin derives APP_ENV=dev" "dev" "$(sed -n 's/^APP_ENV=//p' "$ENVF")"
check "an http origin keeps SITE_ADDRESS without the port" "http://localhost" "$(sed -n 's/^SITE_ADDRESS=//p' "$ENVF")"
check "the origin's port becomes the published HTTP port" "18080" "$(sed -n 's/^HTTP_PORT=//p' "$ENVF")"

# 3c. Two fresh runs must not produce the same secrets. A generator that emits a
#     constant (or reuses a template value) passes every other check here.
ENVF2="$SB/fresh-2.env"
run_bootstrap "$ENVF2" --base-url https://forms.example.test --no-start >/dev/null 2>&1
[ "$(sed -n 's/^ENCRYPTION_KEY=//p' "$SB/fresh-https.env")" != "$(sed -n 's/^ENCRYPTION_KEY=//p' "$ENVF2")" ] &&
	ok "two fresh runs generate different ENCRYPTION_KEYs" ||
	bad "two fresh runs generated the SAME ENCRYPTION_KEY"

# 3d. Idempotent: a second run adopts the file and must NOT rotate a key that
#     encrypts stored credentials — rotating it silently orphans the data.
key_before="$(sha256sum "$ENVF" | cut -d' ' -f1)"
OUT="$(run_bootstrap "$ENVF" --no-start)"; RC=$?
key_after="$(sha256sum "$ENVF" | cut -d' ' -f1)"
check "a second run succeeds" 0 "$RC"
check "a second run does not rewrite any secret" "$key_before" "$key_after"
not_contains "a second run does not claim to generate anything" "$OUT" "generated secrets"

# 3e. A good .env needs no --base-url at all: the saved origin is reused.
ENVF3="$SB/adopt.env"
run_bootstrap "$ENVF3" --base-url https://saved.example.test --no-start >/dev/null 2>&1
OUT="$(run_bootstrap "$ENVF3" --no-start)"; RC=$?
check "an existing .env is validated without --base-url" 0 "$RC"

# 3f. A PORT THE OPERATOR MOVED STAYS MOVED. This is not a nicety: on a host where
#     80/443 already belong to something else, publishing either one aborts the
#     whole `docker compose up` with "Bind for 0.0.0.0:443 failed: port is already
#     allocated" — measured on the agent host, where a traefik holds 443. An
#     http:// trial publishes 443 too (nothing serves TLS there), so the unused
#     port is exactly the one that breaks a first install.
ENVF4="$SB/ports.env"
run_bootstrap "$ENVF4" --base-url https://ports.example.test --no-start >/dev/null 2>&1
env_set_fixture() { # rewrite a key in the fixture file, the way an operator would
	awk -v k="$1" -v v="$2" 'BEGIN{d=0} $0 ~ "^"k"="{print k"="v; d=1; next} {print} END{if(!d) print k"="v}' "$ENVF4" >"$ENVF4.tmp"
	cat "$ENVF4.tmp" >"$ENVF4"
	rm -f "$ENVF4.tmp"
}
env_set_fixture HTTPS_PORT 18443
env_set_fixture HTTP_PORT 18081
OUT="$(run_bootstrap "$ENVF4" --no-start)"; RC=$?
check "a run that keeps an existing .env succeeds" 0 "$RC"
check "an operator's HTTPS_PORT override survives the bootstrap" "18443" "$(sed -n 's/^HTTPS_PORT=//p' "$ENVF4")"
check "an operator's HTTP_PORT override survives the bootstrap" "18081" "$(sed -n 's/^HTTP_PORT=//p' "$ENVF4")"
# …but a port the ORIGIN names is authoritative: that is the URL people load.
run_bootstrap "$ENVF4" --base-url https://ports.example.test:9443 --no-start >/dev/null 2>&1
check "an explicit port in --base-url beats the file" "9443" "$(sed -n 's/^HTTPS_PORT=//p' "$ENVF4")"

# 3g. SELF-HOST MODE on an ADOPTED file — the outage this gate exists to prevent.
#     A .env written before this key existed (or hand-made from an older
#     template) is adopted as-is; if the bootstrap does not add the declaration,
#     the install comes up healthy, prints its forms as live, and answers 404 to
#     every buyer. The fresh-file case above passes from the template alone, so
#     this is the only shape that proves the WRITE.
ENVF5="$SB/pre-flag.env"
grep -v '^JOMFORM_SELF_HOSTED=' "$ENVF" >"$ENVF5"
check "the fixture really lacks the flag" "" "$(sed -n 's/^JOMFORM_SELF_HOSTED=//p' "$ENVF5")"
OUT="$(run_bootstrap "$ENVF5" --no-start)"; RC=$?
check "adopting a pre-flag .env succeeds" 0 "$RC"
check "…and the bootstrap gives it the self-host declaration" "1" "$(sed -n 's/^JOMFORM_SELF_HOSTED=//p' "$ENVF5")"
contains "…exactly once" "1" "$(grep -c '^JOMFORM_SELF_HOSTED=' "$ENVF5")"

# 3h. An operator who turned it OFF KEEPS it off: several tenants behind one
#     shared origin is a legitimate deployment, and a routine bootstrap run must
#     not re-open a host they closed (that would serve their forms on a shared
#     apex — issue #77).
ENVF6="$SB/opted-out.env"
grep -v '^JOMFORM_SELF_HOSTED=' "$ENVF" >"$ENVF6"
printf '%s\n' "JOMFORM_SELF_HOSTED=0" >>"$ENVF6"
OUT="$(run_bootstrap "$ENVF6" --no-start)"; RC=$?
check "a bootstrap run with the flag explicitly 0 succeeds" 0 "$RC"
check "…and leaves the operator's 0 alone" "0" "$(sed -n 's/^JOMFORM_SELF_HOSTED=//p' "$ENVF6")"

# ═══ 4. THE COMPOSE CONTRACT ════════════════════════════════════════════════
echo "--- 4. the compose file is a complete self-host stack"
SERVICES="$(grep -E '^  [a-z][a-z0-9-]*:$' "$COMPOSE" | tr -d ' :' | tr '\n' ' ')"
# CODE = the compose with comment lines stripped: this file's own header records
# the defaults and images that WERE removed, and a guard that reads history as
# behaviour would force the reason out of the file.
COMPOSE_CODE="$(grep -vE '^[[:space:]]*#' "$COMPOSE")"
for svc in caddy web api postgres valkey s3; do
	contains "the stack contains the $svc service" "$SERVICES" "$svc"
done
# Only the front door is published. Everything else on the compose network.
PUBLISHED="$(grep -cE '^\s+- "[0-9$][^"]*:[0-9]+"' "$COMPOSE")"
check "exactly two published host ports (Caddy's http + https)" 2 "$PUBLISHED"
for svc in api postgres valkey s3; do
	block="$(awk -v s="  $svc:" '$0==s{f=1;next} f&&/^  [a-z]/{f=0} f' "$COMPOSE")"
	not_contains "$svc publishes no host port" "$block" "ports:"
done
check "every long-running service restarts unless stopped" 6 "$(grep -c 'restart: unless-stopped' "$COMPOSE")"
check "every long-running service has a healthcheck" 6 "$(grep -c 'healthcheck:' "$COMPOSE")"
# The bucket is provisioned by the API (objstore.EnsureBucket), so there must be
# no one-shot init container — the image it needed (`minio/mc`) no longer exists,
# and a service whose only job is to create a bucket the API creates anyway is a
# moving part that can only fail.
not_contains "no one-shot bucket-init service" "$COMPOSE_CODE" "minio-init"
contains "the api starts only once the S3 endpoint is serving" "$(awk '/^  api:/{f=1} f&&/^  postgres:/{f=0} f' "$COMPOSE")" "service_healthy"
for gone in 'minio/minio' 'minio/mc' 'MINIO_ROOT'; do
	not_contains "no unpullable MinIO reference ($gone)" "$COMPOSE_CODE" "$gone"
done
contains "the web image is built from the repo root" "$(awk '/^  web:/{f=1} f&&/^  api:/{f=0} f' "$COMPOSE")" "context: .."
# Next's standalone server binds to $HOSTNAME (Docker sets it to the container
# id), so without HOSTNAME=0.0.0.0 it listens on eth0 only and a loopback
# healthcheck can never pass — measured: `container jomform-web-1 is unhealthy`
# while /login answered 200 through the front door, aborting `--wait` on a stack
# that was up.
contains "the web container binds all interfaces (HOSTNAME=0.0.0.0)" "$(awk '/^  web:/{f=1} f&&/^  api:/{f=0} f' "$COMPOSE")" 'HOSTNAME: "0.0.0.0"'
contains "the web healthcheck probes loopback" "$(awk '/^  web:/{f=1} f&&/^  api:/{f=0} f' "$COMPOSE")" "127.0.0.1:3000"
# NO WORKING DEFAULT SECRET. Asserted on the file, not on the bootstrap, because
# `docker compose up` is a documented way to start this stack too (COMPOSE_CODE is
# defined above, with the comment lines stripped).
for bad_default in ':-dev_only' ':-jomform_dev' ':-change_me'; do
	not_contains "no working default secret ($bad_default)" "$COMPOSE_CODE" "$bad_default"
done
contains "ENCRYPTION_KEY has no default at all" "$COMPOSE_CODE" 'ENCRYPTION_KEY: ${ENCRYPTION_KEY:?'
contains "the postgres password has no default" "$COMPOSE_CODE" 'POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?'
contains "the S3 secret key has no default" "$COMPOSE_CODE" 'S3_SECRET_KEY: ${S3_SECRET_KEY:?'
contains "the S3 access key has no default either" "$COMPOSE_CODE" 'S3_ACCESS_KEY: ${S3_ACCESS_KEY:?'
# Self-host mode is a DECLARATION the .env makes, never something this file
# assumes: the default stays 0, so the API's hosted-safe default holds and a
# missing key in .env is caught here instead of silently serving nothing.
contains "the compose file passes self-host mode through without turning it on" \
	"$COMPOSE_CODE" 'JOMFORM_SELF_HOSTED: ${JOMFORM_SELF_HOSTED:-0}'
# The other direction: the HOSTED production stack must never declare it. Its
# APP_BASE_URL is the product apex, and self-host mode there serves merchant
# forms on the apex — issue #77, the regression this whole flag exists to close.
if [ -r "$HOSTED_COMPOSE" ]; then
	not_contains "the hosted production compose never sets self-host mode" \
		"$(cat "$HOSTED_COMPOSE")" "JOMFORM_SELF_HOSTED"
else
	bad "the hosted production compose is missing ($HOSTED_COMPOSE) — cannot prove it does not set self-host mode"
fi

# The real validator: parse + interpolate the file with the real docker. No
# daemon is contacted; a missing docker is a FAILURE (the artifact cannot be
# shipped unvalidated) rather than a silent skip.
if docker compose version >/dev/null 2>&1; then
	CFG="$(docker compose -f "$COMPOSE" --env-file "$ENVF" config 2>"$SB/cfg.err")"; CFG_RC=$?
	if [ "$CFG_RC" -eq 0 ] && [ ! -s "$SB/cfg.err" ]; then
		ok "docker compose config accepts the file with a generated .env, with no warning"
	else
		bad "docker compose config failed or warned: $(head -3 "$SB/cfg.err" | tr '\n' ' ')"
	fi
	# No env value may interpolate to null: an unset variable in a bare
	# `KEY: ${VAR}` renders as null and the container gets no value at all.
	NULLENV="$(printf '%s\n' "$CFG" | awk '/^    environment:/{f=1;next} f&&/^    [a-z]/{f=0} f&&/null/{print}')"
	check "no environment value interpolates to null" "" "$NULLENV"
	# …and the flag must REACH the api container, not just the .env file: the
	# compose hop is a second place it can be dropped, and dropping it leaves a
	# self-hosted install unable to serve a single form while everything above
	# this line stays green.
	API_ENV="$(printf '%s\n' "$CFG" | awk '/^  api:/{f=1;next} f&&/^  [a-z][a-z0-9-]*:$/{f=0} f')"
	contains "the api container receives JOMFORM_SELF_HOSTED=1 from the generated .env" \
		"$API_ENV" 'JOMFORM_SELF_HOSTED: "1"'
else
	bad "docker is not installed — the compose file cannot be validated (this is a failure, not a skip)"
fi

# ═══ 5. THE FRONT DOOR'S PATH SPLIT ═════════════════════════════════════════
echo "--- 5. the front door sends each surface to the service that serves it"
CADDY="$(cat "$CADDYFILE")"
for surface in '/f/*' '/t/*' '/v1/*' '/media/*' '/embed/*' '/oauth/*' '/.well-known/oauth-*' '/healthz'; do
	contains "the Caddyfile routes $surface to the API" "$CADDY" "$surface"
done
contains "the API block proxies to api:8080" "$CADDY" "reverse_proxy api:8080"
contains "everything else (incl. /api/) proxies to web:3000" "$CADDY" "reverse_proxy web:3000"
not_contains "/api/* is NOT sent to the API directly (session cookie stays first-party)" "$CADDY" "@api path /api/"
contains "the Caddyfile is mounted into the front-door container" "$(cat "$COMPOSE")" "./selfhost/Caddyfile:/etc/caddy/Caddyfile:ro"
contains "the front door terminates TLS for the configured site" "$CADDY" '{$SITE_ADDRESS:http://localhost}'

# ═══ 6. NO HOST OF OURS IN THE INSTALL PATH ═════════════════════════════════
echo "--- 6. the install path carries none of our hosts"
# Two different rules, deliberately:
#   * our infrastructure (the production IP, the agent host, our wildcard zone)
#     must not appear ANYWHERE in the install path or the docs a self-hoster
#     reads — a "self-host" instruction naming our production IP is not one;
#   * the product DOMAIN is refused in the machine-read install artifacts
#     (compose, env template, front door, this directory). README.md may name it
#     where it describes the hosted service — that is a product fact, not an
#     installation default.
# The scan is by SHAPE, not by a list of our addresses, and that is deliberate
# twice over: this file ships in a public repository, so a guard that names the
# addresses it guards against publishes them itself — the exact leak it exists to
# prevent. Any address in the install path is a defect, so the rule is "no
# address", with only the addresses that are structurally required allowed:
#   0.0.0.0 / 127.0.0.1 (a listen address and a container healthcheck) and the
#   RFC 5737 documentation ranges (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24),
#   which exist precisely to be written in examples.
LEAKS="$(grep -rnEo --exclude=bootstrap_test.sh --exclude-dir=node_modules \
	'\b([0-9]{1,3}\.){3}[0-9]{1,3}\b' \
	"$HERE" "$COMPOSE" "$ENV_EXAMPLE" "$SELFHOST_DOC" "$README" 2>/dev/null \
	| awk -F: '{ a = $NF
	    if (a == "0.0.0.0" || a == "127.0.0.1") next
	    if (a ~ /^192\.0\.2\./ || a ~ /^198\.51\.100\./ || a ~ /^203\.0\.113\./) next
	    print }' || true)"
check "no address of ours in deploy/selfhost|compose|.env.example|docs/SELF-HOST.md|README.md (0.0.0.0, 127.0.0.1 and RFC 5737 doc ranges excepted)" "" "$LEAKS"
DOMAIN_LEAKS="$(grep -rnE --exclude=bootstrap_test.sh 'jomform\.com' "$HERE" "$COMPOSE" "$ENV_EXAMPLE" "$SELFHOST_DOC" 2>/dev/null || true)"
check "no product domain hardcoded in the machine-read install path" "" "$DOMAIN_LEAKS"

echo "---"
echo "selfhost bootstrap self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
