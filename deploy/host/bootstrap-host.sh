#!/usr/bin/env bash
# deploy/host/bootstrap-host.sh — stand up the JomForm stack on a FRESH host.
#
# WHY THIS EXISTS. Everything the stack needs was in git EXCEPT the things a new
# host actually has to be told, and the two most expensive of those were wrong in
# the repo: the compose file was a DEV shape (default password `jomform_dev`,
# `pgdata` mounted at `/var/lib/postgresql/data` while the pinned postgres:18
# image puts its data dir under `/var/lib/postgresql` — so a fresh host would have
# come up with an EMPTY database or a known dev password), and the traefik pattern
# was written in Traefik v2 syntax while the live edge is v3. Measured 2026-10-12.
#
# DRY-RUN BY DEFAULT. It prints the plan and changes NOTHING unless `--apply` is
# given, because the only thing worse than a manual bootstrap is an automatic one
# that half-ran. `--apply` requires root.
#
# WHAT IT DELIBERATELY DOES NOT DO — it says so at the end rather than leaving the
# operator to discover it:
#   * it does not register DNS, nor obtain the Cloudflare / Google / SES / LLM
#     credentials — those are ACCOUNT secrets and they are INHERITED (see
#     `deploy/vps/.env.prod.example`);
#   * it does not MOVE THE DATA. The database and the media volume come from the
#     nightly backup's restore drill; that step needs the R2 credential and a
#     decision about the cutover, so it stays in the operator's hands. The exact
#     command is printed when the bootstrap finishes.
#
# USAGE
#   deploy/host/bootstrap-host.sh --env-file ~/jomform.env            # plan only
#   sudo deploy/host/bootstrap-host.sh --env-file ~/jomform.env --apply
#
#   --root DIR          install root (default /opt) — the seam the self-test uses
#   --compose-src FILE  the prod compose to install (default deploy/vps/compose.prod.yml)
#   --force             install over an existing DIFFERING file (it is backed up first)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VPS_DIR="$REPO_ROOT/deploy/vps"

APPLY=0
FORCE=0
ROOT_DIR="/opt"
ENV_FILE=""
COMPOSE_SRC="$VPS_DIR/compose.prod.yml"

while [ $# -gt 0 ]; do
  case "$1" in
    --apply)        APPLY=1; shift ;;
    --force)        FORCE=1; shift ;;
    --root)         ROOT_DIR="$2"; shift 2 ;;
    --env-file)     ENV_FILE="$2"; shift 2 ;;
    --compose-src)  COMPOSE_SRC="$2"; shift 2 ;;
    -h|--help)      sed -n '2,/^set -euo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \?//'; exit 0 ;;
    *) echo "bootstrap-host: unknown argument: $1" >&2; exit 2 ;;
  esac
done

fail() { echo "bootstrap-host: REFUSED — $*" >&2; exit 1; }

# step <description> <command...> — prints in dry-run, runs under --apply.
step() {
  local desc="$1"; shift
  if [ "$APPLY" -eq 1 ]; then
    echo "==> $desc"
    "$@"
  else
    printf '    would: %s\n' "$desc"
    printf '           %s\n' "$*"
  fi
}

TS="$(date -u +%Y%m%dT%H%M%SZ)"
echo "bootstrap-host — root=$ROOT_DIR  mode=$([ "$APPLY" -eq 1 ] && echo APPLY || echo DRY-RUN)"
echo

# ── 1. preconditions ──────────────────────────────────────────────────────────
[ -s "$COMPOSE_SRC" ] || fail "no prod compose at $COMPOSE_SRC"
command -v docker >/dev/null 2>&1 || fail "docker is not installed on this host"
docker compose version >/dev/null 2>&1 || fail "the docker 'compose' plugin is not available (need docker compose v2)"
if [ "$APPLY" -eq 1 ] && [ "$(id -u)" -ne 0 ]; then
  fail "--apply needs root (it writes $ROOT_DIR and installs cron)"
fi
if [ -z "$ENV_FILE" ]; then
  fail "--env-file is required (start from deploy/vps/.env.prod.example)"
fi
[ -s "$ENV_FILE" ] || fail "the env file '$ENV_FILE' does not exist or is empty"

# The file must not be readable by anyone else: it holds the DB and object-store
# credentials and the account credentials.
perms="$(stat -c '%a' "$ENV_FILE")"
case "$perms" in
  600|400|640|440) : ;;
  *) fail "$ENV_FILE is mode $perms — it holds credentials; chmod 600 it" ;;
esac
# …and 640/440 are only acceptable if the group is not a shared one; say so.
if [ "$perms" = "640" ] || [ "$perms" = "440" ]; then
  echo "    note: mode $perms — group members can read the credentials"
fi

# ── 2. the env contract: every ${KEY} the compose reads must be in the file ────
# DERIVED FROM THE COMPOSE, not from a list written here: a key added to the
# compose without being added to the example is a silent empty credential.
#
# THE NAME PATTERN INCLUDES DIGITS. It used to be [A-Z_]+, which stops at the
# first digit: `S3_ACCESS_KEY` was derived as `S`, so a CORRECT env file was
# refused for missing a key called `S`, and the contract looked satisfied by a
# key nobody has. Env names with digits are ordinary (S3_*, SES_*), so the class
# is [A-Z0-9_]+ on both sides.
compose_keys="$(grep -oE '\$\{[A-Z0-9_]+' "$COMPOSE_SRC" | tr -d '${' | sort -u)"
env_keys="$(grep -oE '^[A-Z0-9_]+=' "$ENV_FILE" | tr -d '=' | sort -u)"
missing="$(comm -23 <(echo "$compose_keys") <(echo "$env_keys") || true)"
if [ -n "$missing" ]; then
  echo "bootstrap-host: REFUSED — $ENV_FILE is missing $(echo "$missing" | wc -l) key(s) the compose reads:" >&2
  echo "$missing" | sed 's/^/    /' >&2
  echo "    (the compose reads: $(echo "$compose_keys" | tr '\n' ' '))" >&2
  exit 1
fi
echo "    env contract: all $(echo "$compose_keys" | wc -l) keys the compose reads are present"

# ── 3. plan (or perform) ──────────────────────────────────────────────────────
install_file() { # <src> <dest>
  local src="$1" dest="$2"
  if [ -f "$dest" ] && cmp -s "$src" "$dest"; then
    echo "    already identical: $dest"
    return 0
  fi
  if [ -f "$dest" ] && [ "$FORCE" -ne 1 ]; then
    fail "$dest exists and DIFFERS from $src — refusing to clobber it without --force (that file is hand-maintained state until it is not)"
  fi
  # BACK UP FIRST, then install: the other order would copy the freshly installed
  # file over the backup (measured in review of this script's first draft).
  if [ -f "$dest" ]; then
    step "back up the differing $dest → $dest.bak-$TS" cp -a "$dest" "$dest.bak-$TS"
  fi
  step "install $(basename "$src") → $dest" install -m 0644 "$src" "$dest"
}

# The cron the stack assumes, installed idempotently: the marker line is removed
# first so re-running does not append duplicates. `crontab -l` fails on a host
# with no crontab at all, which is the NORMAL state on a fresh host — hence the
# `|| true`, which is about that specific expected failure and nothing else.
install_cron_line() { # <marker> <line>
  local marker="$1" line="$2"
  step "install the cron line for $marker" bash -c "
    set -eu
    tmp=\$(mktemp)
    crontab -l 2>/dev/null | grep -v -- '$marker' > \"\$tmp\" || true
    echo \"$line\" >> \"\$tmp\"
    crontab \"\$tmp\"
    rm -f \"\$tmp\"
  "
}

install_file "$COMPOSE_SRC"                    "$ROOT_DIR/jomform/compose.yml"
install_file "$VPS_DIR/traefik-jomform.yml"    "$ROOT_DIR/traefik/dynamic/jomform.yml"
install_file "$VPS_DIR/traefik-docker-compose.yml" "$ROOT_DIR/traefik/docker-compose.yml"
step "install the env file → $ROOT_DIR/jomform/.env (mode 600)" install -m 0600 "$ENV_FILE" "$ROOT_DIR/jomform/.env"

# The compose declares `dokploy-network` EXTERNAL, so a fresh host has nothing to
# attach to and `compose up` fails with a network-not-found error. Create it.
step "create the external network dokploy-network (declared external by the compose)" \
     docker network create dokploy-network
step "bring the stack up: docker compose -f $ROOT_DIR/jomform/compose.yml up -d" \
     docker compose -f "$ROOT_DIR/jomform/compose.yml" --env-file "$ROOT_DIR/jomform/.env" up -d

# ── 4. the cron the stack assumes ─────────────────────────────────────────────
# The nightly backup is a host cron job, not a container: a fresh host without it
# looks healthy and has no backups at all. Same for the container prune.
step "install the nightly backup cron line (17 2 * * *)" \
     install_cron_line jomform-backup.sh '17 2 * * * /usr/local/bin/jomform-backup.sh >> /var/log/jomform-backup.log 2>&1'
step "install the container-prune cron line (40 3 * * *)" \
     install_cron_line jomform-container-prune.sh '40 3 * * * /usr/local/bin/jomform-container-prune.sh >> /var/log/jomform-container-prune.log 2>&1'

echo
echo "NEXT, IN THIS ORDER (not done by this script — see the header):"
echo "  1. install /usr/local/bin/jomform-backup.sh + its rclone remote"
echo "     (deploy/backup/README.md § Install on the host), or the new host has no backups"
echo "  2. install /usr/local/bin/jomform-container-prune.sh"
echo "     (deploy/host/README.md § Installing it) — the cron line installed above already"
echo "     points at it, so without this step that job fails silently and the pile grows:"
echo "       deploy/host/install-host-container-prune.sh"
echo "  3. point DNS at this host and let the edge issue certificates"
echo "  4. MOVE THE DATA — the restore drill, from the nightly backup:"
echo "       /usr/local/bin/jomform-backup.sh --restore /var/backups/jomform/jomform-daily-<date>.dump.gz"
echo "     plus the media archive, then verify with deploy/backup/README.md § Restore drill"
echo "  5. verify the edge: deploy/vps/drift-check.sh --host <user>@<this-host>"
[ "$APPLY" -eq 1 ] && echo && echo "bootstrap-host: applied." || { echo; echo "bootstrap-host: DRY-RUN — nothing was changed. Re-run with --apply."; }
