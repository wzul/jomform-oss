#!/usr/bin/env bash
#
# install-jomform-watch.sh — put the uptime watchdog on a host and arm it.
#
# Idempotent: re-running replaces the script and re-checks the cron entry. It
# refuses to arm the timer until it has PROVEN an alert can be delivered — an
# installed-but-mute watchdog is the exact failure this work exists to remove.
#
# Usage:
#   deploy/monitoring/install-jomform-watch.sh --telegram-token <token> \
#       --telegram-chat <chat-id>[,<chat-id>...] [--dry-run]
#
#   The token/chat can instead be pre-placed in /etc/jomform-watch.env, in which
#   case no flags are needed (re-installs use this path).
#
#   On a host that already holds the token in an env file elsewhere, pass
#   --env-file <path> and the installer will read the two JF_WATCH_TELEGRAM_*
#   keys from it instead.
#
# What it does:
#   1. installs jomform-watch.sh to /usr/local/bin (root, 0755)
#   2. writes /etc/jomform-watch.env (root, 0600) if it does not exist
#   3. sends a real test alert and FAILS if Telegram did not accept it
#   4. installs /etc/cron.d/jomform-watch (5-minute check + daily digest)
#
# It never prints the token.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN=/usr/local/bin/jomform-watch.sh
ENV_FILE=/etc/jomform-watch.env
CRON_FILE=/etc/cron.d/jomform-watch
LOG_DIR=/var/log

TOKEN=""
CHAT=""
SOURCE_ENV=""
DRY_RUN=0

die() { echo "ERROR: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --telegram-token) TOKEN="${2:-}"; shift 2 ;;
    --telegram-chat)  CHAT="${2:-}";  shift 2 ;;
    --env-file)       SOURCE_ENV="${2:-}"; shift 2 ;;
    --dry-run)        DRY_RUN=1; shift ;;
    -h|--help)        sed -n '3,30p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ "$(id -u)" = 0 ] || die "must run as root (writes /etc and installs cron)"

# ── 1. the watchdog itself ──────────────────────────────────────────────────
[ -f "$HERE/jomform-watch.sh" ] || die "$HERE/jomform-watch.sh not found"
if [ "$DRY_RUN" = 0 ]; then
  install -m 0755 -o root -g root "$HERE/jomform-watch.sh" "$BIN"
  echo "installed $BIN"
else
  echo "dry-run: would install $BIN"
fi

# ── 2. the env file ─────────────────────────────────────────────────────────
if [ -n "$SOURCE_ENV" ]; then
  [ -r "$SOURCE_ENV" ] || die "cannot read --env-file $SOURCE_ENV"
  # Accept this host's existing Telegram keys as well as the watchdog's own, so
  # an install on an agent host can reuse its bot without copying secrets around
  # by hand. Values are read from the file and never echoed.
  TOKEN="${TOKEN:-$(sed -n 's/^JF_WATCH_TELEGRAM_TOKEN=//p' "$SOURCE_ENV" | tail -1)}"
  TOKEN="${TOKEN:-$(sed -n 's/^TELEGRAM_BOT_TOKEN=//p' "$SOURCE_ENV" | tail -1)}"
  CHAT="${CHAT:-$(sed -n 's/^JF_WATCH_TELEGRAM_CHAT=//p' "$SOURCE_ENV" | tail -1)}"
  CHAT="${CHAT:-$(sed -n 's/^TELEGRAM_HOME_CHANNEL=//p' "$SOURCE_ENV" | tail -1)}"
fi

if [ ! -f "$ENV_FILE" ]; then
  [ -n "$TOKEN" ] && [ -n "$CHAT" ] || die "no $ENV_FILE and no --telegram-token/--telegram-chat given"
  if [ "$DRY_RUN" = 0 ]; then
    ( umask 077; cat >"$ENV_FILE" <<EOF
# JomForm watchdog configuration (see deploy/monitoring/jomform-watch.env.example)
JF_WATCH_TELEGRAM_TOKEN=$TOKEN
JF_WATCH_TELEGRAM_CHAT=$CHAT
EOF
    )
    chmod 600 "$ENV_FILE"
    echo "wrote $ENV_FILE (0600)"
  else
    echo "dry-run: would write $ENV_FILE (0600)"
  fi
else
  chmod 600 "$ENV_FILE" 2>/dev/null
  echo "$ENV_FILE already exists — keeping it"
fi

# ── 3. PROVE an alert can be delivered before arming the timer ──────────────
if [ "$DRY_RUN" = 1 ]; then
  echo "dry-run: skipping the delivery proof"
else
  echo "sending a test alert (delivery proof)…"
  # Deliberately point the web probe at a port that cannot answer, one attempt,
  # so the run is a guaranteed failure and the ALERT path is what gets exercised.
  # This uses the installed script itself, not a hand-rolled curl that could
  # drift from the real notifier.
  PROOF_STATE="$(mktemp -d)"
  proof=$(JF_WATCH_ENV_FILE="$ENV_FILE" \
    JF_WATCH_WEB_URL="http://127.0.0.1:1/proof" \
    JF_WATCH_HEALTH_URL="" \
    JF_WATCH_ATTEMPTS=1 JF_WATCH_ATTEMPT_DELAY=0 JF_WATCH_TIMEOUT=5 \
    JF_WATCH_STATE_DIR="$PROOF_STATE" JF_WATCH_LOG_FILE=/dev/null \
    "$BIN" 2>&1)
  rc=$?
  rm -rf "$PROOF_STATE"
  if printf '%s' "$proof" | grep -q 'notify_sent'; then
    echo "delivery proof OK — Telegram accepted the test alert (exit $rc from the injected failure)"
  else
    echo "delivery proof FAILED — the watchdog cannot alert on this host:" >&2
    printf '%s\n' "$proof" | tail -5 >&2
    die "refusing to arm the timer until an alert can actually be delivered"
  fi
fi

# ── 4. cron ─────────────────────────────────────────────────────────────────
if [ "$DRY_RUN" = 0 ]; then
  install -m 0644 -o root -g root "$HERE/cron.d-jomform-watch" "$CRON_FILE"
  touch "$LOG_DIR/jomform-watch.cron.log"
  echo "installed $CRON_FILE (check every 5 min, digest daily 09:17)"
else
  echo "dry-run: would install $CRON_FILE"
fi

cat <<EOF

Watchdog armed. Verify with:
  $BIN                          # one pass, prints its own log lines
  tail -f /var/log/jomform-watch.log
  JF_WATCH_MODE=digest $BIN     # the heartbeat message
Prove it BITES (the only real test):
  docker stop jomform-api-1     # on the prod host; expect an alert within ~5 min
Then confirm recovery:
  docker start jomform-api-1
EOF
