#!/usr/bin/env bash
# JomForm self-host bootstrap — one command from a fresh clone to a running
# instance you can open in a browser.
#
#   bash deploy/selfhost/bootstrap.sh --base-url https://forms.example.com
#
# What it does, in order:
#
#   1. checks the machine has what it needs (docker + the compose v2 plugin,
#      openssl) and says exactly what is missing rather than failing later;
#   2. resolves the public origin — the flag, the saved deploy/.env, or an
#      interactive prompt — and REFUSES if it cannot be known, before it writes
#      anything;
#   3. creates deploy/.env from deploy/.env.example (the example is the single
#      source of the key set, so this cannot drift from the compose) and mints
#      every secret that is still a placeholder with `openssl rand -hex 32`:
#      the postgres password, the MinIO root password and ENCRYPTION_KEY;
#   4. derives the front-door settings from that origin: SITE_ADDRESS (no port —
#      Caddy listens on 80/443 inside its container), the published ports, and
#      APP_ENV (https -> prod, http -> dev; a Secure session cookie is never sent
#      back over plain HTTP, so an http:// deployment that claimed prod would
#      "log in" and then forget the session);
#   5. VALIDATES the result and refuses to start anything while a secret or
#      APP_BASE_URL is still a placeholder — see validate_env();
#   6. `docker compose up -d --build --wait`, then prints the URL and the steps
#      for the first account.
#
# WHAT IT DELIBERATELY DOES NOT DO: mint a SESSION_SECRET or a VALKEY password.
#   * SESSION_SECRET is not part of the contract — the API has no reader for it
#     (sessions are opaque 32-byte Valkey tokens, not signed cookies) and
#     api/internal/config/config_test.go pins that. A generated secret nothing
#     consumes is exactly the "protected credential that protects nothing" class
#     this repo has already paid to remove.
#   * Valkey is not published to the host at all: it sits on the compose network
#     and only the API/worker containers reach it, so there is no password to
#     leak. Publishing it would need one; this compose does not.
#
# Idempotent: run it again and it validates the existing deploy/.env and restarts
# what changed. It never overwrites an existing secret, and it only rewrites the
# values it derives from --base-url.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/deploy/docker-compose.yml"
EXAMPLE_FILE="$REPO_ROOT/deploy/.env.example"
ENV_FILE="$REPO_ROOT/deploy/.env"

BASE_URL=""
DO_START=1
DO_BUILD=1
HELP=0

die() { echo "bootstrap: REFUSED — $*" >&2; exit 1; }
note() { echo "bootstrap: $*"; }
warn() { echo "bootstrap: WARNING — $*" >&2; }

usage() {
	cat <<'USAGE'
Usage: bash deploy/selfhost/bootstrap.sh --base-url <URL> [options]

  --base-url URL    the public origin users will load, e.g.
                    https://forms.example.com   or   http://localhost:8080
                    (it is also the passkey relying party — see APP_BASE_URL in
                    deploy/.env.example). Required on the first run.
  --env-file PATH   use another env file (default: deploy/.env)
  --no-start        prepare and VALIDATE deploy/.env, then stop
  --no-build        start without rebuilding the api/web images
  -h, --help        this text

On a machine that already has deploy/.env, --base-url is optional: the saved
APP_BASE_URL is reused, and the secrets in the file are never rewritten.
USAGE
}

while [ $# -gt 0 ]; do
	case "$1" in
	--base-url) BASE_URL="${2:-}"; shift 2 ;;
	--base-url=*) BASE_URL="${1#*=}"; shift ;;
	--env-file) ENV_FILE="${2:-}"; shift 2 ;;
	--env-file=*) ENV_FILE="${1#*=}"; shift ;;
	--no-start) DO_START=0; shift ;;
	--no-build) DO_BUILD=0; shift ;;
	-h | --help) HELP=1; shift ;;
	*) die "unknown argument '$1' (try --help)" ;;
	esac
done
if [ "$HELP" = 1 ]; then
	usage
	exit 0
fi

# ─── 1. the machine ─────────────────────────────────────────────────────────
command -v docker >/dev/null 2>&1 || die "docker is not installed (https://docs.docker.com/engine/install/)"
docker compose version >/dev/null 2>&1 ||
	die "the docker 'compose' plugin (v2) is not available — this stack uses \`docker compose\`, not the old \`docker-compose\`"
command -v openssl >/dev/null 2>&1 || die "openssl is not installed — it generates this deployment's secrets"
[ -r "$COMPOSE_FILE" ] || die "compose file not found at $COMPOSE_FILE (is this a checkout of the repo?)"
[ -r "$EXAMPLE_FILE" ] || die "env template not found at $EXAMPLE_FILE"

# env_get KEY — the value of KEY in the env file, or empty. Safe on a missing
# file: the caller may be resolving APP_BASE_URL before the file exists.
env_get() {
	[ -f "$ENV_FILE" ] || return 0
	sed -n "s/^${1}=//p" "$ENV_FILE" | tail -n 1
}

# env_set KEY VALUE — replace KEY's line, or append it. awk (not `sed -i`) so the
# value is never interpreted as a sed expression.
env_set() {
	local key="$1" val="$2" tmp
	tmp="$(mktemp)"
	awk -v k="$key" -v v="$val" '
		BEGIN { done = 0 }
		$0 ~ "^" k "=" { if (!done) { print k "=" v; done = 1 }; next }
		{ print }
		END { if (!done) print k "=" v }
	' "$ENV_FILE" >"$tmp"
	cat "$tmp" >"$ENV_FILE" # cat > keeps the file's inode and its 0600 mode
	rm -f "$tmp"
}

# is_placeholder VALUE — the values that must never reach a running deployment.
# `change_me*` is what deploy/.env.example ships; the rest are the defaults this
# repo's older compose files handed out (dev_only_*), plus the shapes somebody
# types in a hurry. Empty counts as a placeholder for every secret.
is_placeholder() {
	case "${1:-}" in
	"" | change_me* | CHANGEME* | changeme* | replace_me* | REPLACE_ME* | dev_only* | jomform_dev | jomform_dev_change_me | __* | xxx*)
		return 0
		;;
	esac
	return 1
}

# validate_env — FAIL CLOSED. Called TWICE: once before anything is written (so a
# refusal leaves the operator's .env byte-identical) and once at the end, over the
# finished file. A deployment with a known or malformed key is worse than one that
# did not start: it comes up healthy, serves traffic, and everyone believes it is
# safe.
validate_env() {
	local problems="" key cur
	if [ -n "${PLACEHOLDERS// /}" ]; then
		problems="${problems}  - placeholder secret(s) in $ENV_FILE:$PLACEHOLDERS
    That file already existed, so it was NOT generated over. Replace each with:
      openssl rand -hex 32
    (or delete $ENV_FILE and re-run this script to have them generated).
"
	fi
	cur="$(env_get APP_BASE_URL)"
	if is_placeholder "$cur"; then
		problems="${problems}  - APP_BASE_URL is a placeholder ('$cur')
    It must be this deployment's public origin; pass --base-url.
"
	fi
	cur="$(env_get ENCRYPTION_KEY)"
	if [ -n "$cur" ]; then
		case "$cur" in
		*[!0-9a-fA-F]*) problems="${problems}  - ENCRYPTION_KEY is not hex (it must be 64 hex chars = 32 bytes)
" ;;
		esac
		if [ "${#cur}" -ne 64 ]; then
			problems="${problems}  - ENCRYPTION_KEY is ${#cur} chars, must be exactly 64 (32 bytes, hex)
"
		fi
	fi
	for key in POSTGRES_PASSWORD S3_SECRET_KEY; do
		cur="$(env_get "$key")"
		if is_placeholder "$cur"; then
			problems="${problems}  - $key is a placeholder or empty
"
		fi
	done
	if [ -n "$problems" ]; then
		die "this deployment cannot be started safely:
$problems
See docs/SELF-HOST.md."
	fi
}

# ─── 2. the public origin ───────────────────────────────────────────────────
# Resolved BEFORE anything is written: a refusal here must leave the checkout
# exactly as it was, because a half-written .env is what a second run would then
# adopt as "the operator's file".
if [ -z "$BASE_URL" ]; then
	BASE_URL="$(env_get APP_BASE_URL)"
	if is_placeholder "$BASE_URL"; then
		BASE_URL=""
	fi
fi
if [ -z "$BASE_URL" ] && [ -t 0 ]; then
	printf 'bootstrap: public origin of this instance (e.g. https://forms.example.com): '
	read -r BASE_URL
fi
[ -n "$BASE_URL" ] || die "no public origin. APP_BASE_URL cannot be guessed: it is the origin users load, it becomes the passkey relying party, and it is what the TLS certificate is issued for. Re-run with --base-url https://forms.example.com"
if is_placeholder "$BASE_URL"; then
	die "APP_BASE_URL is still the placeholder from deploy/.env.example ('$BASE_URL'). Pass the real public origin with --base-url."
fi
BASE_URL="${BASE_URL%/}"
case "$BASE_URL" in
http://* | https://*) ;;
*) die "APP_BASE_URL must start with http:// or https:// (got '$BASE_URL')" ;;
esac
scheme="${BASE_URL%%://*}"
rest="${BASE_URL#*://}"
host="${rest%%/*}"
[ -n "$host" ] || die "APP_BASE_URL has no host ('$BASE_URL')"
case "$rest" in
*/*) die "APP_BASE_URL must be an origin — no path ('$BASE_URL'). Put a reverse proxy in front if you need one." ;;
esac
case "$host" in
*:*:*) die "APP_BASE_URL looks like an IPv6 literal ('$host'); use a hostname — the TLS certificate and the passkey RP ID both need one" ;;
esac

# Ports. Caddy listens on 80/443 INSIDE its container, so a port in APP_BASE_URL
# is only ever the PUBLISHED one. Precedence, highest first:
#
#   1. an explicit port in --base-url  — the operator said it, it wins;
#   2. a numeric value already in the env file — an OVERRIDE, preserved: a host
#      whose 80/443 already belong to something else (an existing nginx, a
#      traefik, another stack) must be able to move these without the bootstrap
#      putting them back on every run;
#   3. the scheme default.
#
# The unused counterpart of the scheme is the one that matters for a trial: an
# http:// install still publishes 443 (nothing serves TLS there), so on a busy
# host that publish is what fails — with `Bind for 0.0.0.0:443 failed: port is
# already allocated`, the whole `up` aborting.
env_port() { # env_port KEY DEFAULT — a numeric existing value, else the default
	local cur
	cur="$(env_get "$1")"
	case "$cur" in
	'' | *[!0-9]*) printf '%s' "$2" ;;
	*) printf '%s' "$cur" ;;
	esac
}

# Split the origin into host + (optional) published port. Done AFTER the scheme
# is known so the port rules above can consult the env file.
port=""
host_only="$host"
case "$host" in
*:*)
	port="${host##*:}"
	host_only="${host%:*}"
	case "$port" in
	'' | *[!0-9]*) die "APP_BASE_URL port '$port' is not a number" ;;
	esac
	;;
esac
[ -n "$host_only" ] || die "APP_BASE_URL has no host ('$BASE_URL')"

if [ "$scheme" = "https" ]; then
	SITE_ADDRESS="https://$host_only"
	HTTPS_PORT="${port:-$(env_port HTTPS_PORT 443)}"
	# Let's Encrypt HTTP-01 challenge + the http->https redirect.
	HTTP_PORT="$(env_port HTTP_PORT 80)"
	APP_ENV_VALUE="prod"
else
	SITE_ADDRESS="http://$host_only"
	HTTP_PORT="${port:-$(env_port HTTP_PORT 80)}"
	# Published but unused: Caddy serves no TLS for an http:// address. Kept
	# overridable for the same reason as the http port — on a host where 443 is
	# already taken, publishing it aborts the whole `up`.
	HTTPS_PORT="$(env_port HTTPS_PORT 443)"
	APP_ENV_VALUE="dev"
	warn "http:// origin: APP_ENV is set to 'dev' so session cookies are not Secure — a Secure cookie is never returned over plain HTTP, and every login would silently fail. Fine for a trial on localhost or a LAN; use https:// for anything real."
fi

# ─── 3. env file ────────────────────────────────────────────────────────────
# CREATING vs ADOPTING. A .env that does not exist yet is ours to fill in. One
# that DOES exist is the operator's: it is validated, and the front-door keys are
# derived from the origin, but no secret is ever rewritten — silently replacing
# the key that encrypts stored payment credentials would make that data
# unreadable.
#
# THE PRE-CHECK COMES FIRST, before a single byte is written: a bootstrap that
# refuses must leave the file exactly as it found it, or the operator's next run
# adopts a half-edited file as "theirs".
placeholder_secrets() {
	local key cur out=""
	for key in POSTGRES_PASSWORD S3_SECRET_KEY ENCRYPTION_KEY; do
		cur="$(env_get "$key")"
		is_placeholder "$cur" && out="$out $key"
	done
	printf '%s' "$out"
}
PLACEHOLDERS=""
if [ -f "$ENV_FILE" ]; then
	PLACEHOLDERS="$(placeholder_secrets)"
	validate_env
fi

CREATED=0
if [ ! -f "$ENV_FILE" ]; then
	# umask 077 so the file exists with 0600 from its first byte, not after a
	# chmod that a crash could skip.
	umask 077
	mkdir -p "$(dirname "$ENV_FILE")"
	cp "$EXAMPLE_FILE" "$ENV_FILE"
	CREATED=1
	note "created $ENV_FILE from deploy/.env.example"
fi
chmod 600 "$ENV_FILE" 2>/dev/null || warn "could not chmod 600 $ENV_FILE (check it is not group/world readable)"

env_set APP_BASE_URL "$BASE_URL"
env_set SITE_ADDRESS "$SITE_ADDRESS"
env_set HTTP_PORT "$HTTP_PORT"
env_set HTTPS_PORT "$HTTPS_PORT"
env_set APP_ENV "$APP_ENV_VALUE"
# THIS INSTALL IS THE ONLY ORIGIN ITS BUYERS LOAD: one host, no tenant
# subdomains, no custom domains. That is the one condition under which the API
# may serve a public form from APP_BASE_URL's host (PublicHandlers.SelfHosted),
# and it can only be asserted here — the deployment's own operator. Without it
# every published form 404s on the only host the install has; with it set on a
# deployment whose APP_BASE_URL is a shared product apex, the apex would start
# serving merchant forms (issue #77), which is why the API defaults it to OFF
# and the hosted compose never sets it.
#
# Written when ABSENT rather than always: a file that predates this key (adopted
# from an older install) must not come up unable to serve a form, and an operator
# who set it to 0 on purpose — several tenants behind one shared origin — must
# not have it re-opened by a routine bootstrap run.
if [ -z "$(env_get JOMFORM_SELF_HOSTED)" ]; then
	env_set JOMFORM_SELF_HOSTED 1
fi

# ─── 4. secrets ─────────────────────────────────────────────────────────────
# Generation applies ONLY to a file this script created. An adopted file keeps
# whatever it has (or is refused by validate_env below).
gen_list=""
if [ "$CREATED" = 1 ]; then
	PLACEHOLDERS=""
	for key in POSTGRES_PASSWORD S3_SECRET_KEY ENCRYPTION_KEY; do
		cur="$(env_get "$key")"
		if is_placeholder "$cur"; then
			env_set "$key" "$(openssl rand -hex 32)"
			gen_list="$gen_list $key"
		fi
	done
fi

# ─── 5. validate — FAIL CLOSED ──────────────────────────────────────────────
# validate_env() is defined ABOVE, before the first write: a refusal must leave
# the operator's file byte-identical. This second call is the FINAL gate over the
# file as it now stands (generated secrets included), and it is the one that
# guards `docker compose up`: a key that is known, malformed or absent is worse
# than a stack that did not start, because this one comes up healthy, serves
# traffic, and everyone believes it is safe.
validate_env

if [ -n "$gen_list" ]; then
	note "generated secrets with 'openssl rand -hex 32' for:$gen_list"
fi
note "APP_BASE_URL=$BASE_URL  (APP_ENV=$(env_get APP_ENV), published ${HTTP_PORT}/${HTTPS_PORT}, Caddy site $SITE_ADDRESS)"
note "SESSION_SECRET is not generated: the API has no reader for it (sessions are Valkey-backed opaque tokens) — see the header of this script."
note "VALKEY has no password on purpose: it is not published to the host — only the API/worker containers reach it over the compose network."

# ─── 6. compose ─────────────────────────────────────────────────────────────
# `config -q` is the cheapest proof that the .env satisfies the compose file: any
# ${VAR:?...} still unset fails HERE, with the message written in the compose
# file, instead of half-starting a stack.
note "validating the compose file against $ENV_FILE"
docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" config -q ||
	die "docker compose rejected the configuration above"

if [ "$DO_START" = 0 ]; then
	note "--no-start: $ENV_FILE is ready and the compose file validates; nothing was started."
	exit 0
fi

note "building and starting the stack (the first build compiles the Go API and the Next.js app; several minutes is normal)"
if [ "$DO_BUILD" = 1 ]; then
	docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --build --wait --wait-timeout 900
else
	docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d --wait --wait-timeout 900
fi

# ─── 7. hand over ───────────────────────────────────────────────────────────
cat <<REPORT

────────────────────────────────────────────────────────────────────────
JomForm is up at          $BASE_URL

First account
  1. Open $BASE_URL and choose "Create account" on the sign-in page.
  2. Register with an email address you control and a password of at least
     10 characters.
  3. Confirm the account:
REPORT
if [ "$(env_get JOMFORM_LOG_VERIFY_LINK)" = "1" ]; then
	cat <<REPORT
     no email sender is configured, so the confirmation link is in the API log:
       docker compose -f deploy/docker-compose.yml logs api \\
         | grep -o 'http[^ ]*/v1/auth/verify?token=[^ "]*'
     open it (it is valid for 24 hours). Configure SES_* in deploy/.env and set
     JOMFORM_LOG_VERIFY_LINK=0 once email should work instead.
REPORT
else
	cat <<REPORT
     open the link in the verification email sent through your SES identity.
REPORT
fi
cat <<REPORT
  4. Sign in, create a workspace, then create a form and publish it.
  5. Open the published form at $BASE_URL/f/<slug> in a private window and submit
     it — that is a buyer's path end to end.

Operate it
  logs       docker compose -f deploy/docker-compose.yml logs -f
  state      docker compose -f deploy/docker-compose.yml ps
  stop       docker compose -f deploy/docker-compose.yml down
  upgrade    git pull && bash deploy/selfhost/bootstrap.sh

KEEP deploy/.env SAFE. It holds ENCRYPTION_KEY, which decrypts the merchant
payment credentials stored in the database: a Postgres backup without it is not
restorable. See docs/SELF-HOST.md for backups.
────────────────────────────────────────────────────────────────────────
REPORT
