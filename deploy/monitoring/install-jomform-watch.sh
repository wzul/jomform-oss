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
#   --bin <path>     install target (default /usr/local/bin/jomform-watch.sh) —
#                    the seam deploy/monitoring/jomform-watch_test.sh drives
#   --binary-only    do step 1 and stop (no env file, no delivery proof, no
#                    cron). The self-test needs exactly this: the rest of the run
#                    writes /etc, installs cron and sends a real alert, none of
#                    which a gate may do.
#
# What it does:
#   1. installs jomform-watch.sh to /usr/local/bin (root, 0755), having kept the
#      copy it replaces as <target>.prev (0700) — see "Rollback" below
#   2. writes /etc/jomform-watch.env (root, 0600) if it does not exist
#   3. sends a real test alert and FAILS if Telegram did not accept it
#   4. installs /etc/cron.d/jomform-watch (5-minute check + daily digest)
#
# ROLLBACK — re-running this used to overwrite the live watchdog in place with
# nothing kept, so a bad install could not be undone: measured on the agent host
# (t_a05ecb58) the copy installed 2026-10-13 was gone the moment the installer
# ran, and it did not match any committed revision either, so "reinstall it from
# git" was not a faithful rollback. Step 1 therefore saves the outgoing bytes
# before it overwrites them and prints the exact one-command revert. An existing
# .prev is NEVER overwritten silently: if it holds older bytes they are archived
# as <target>.prev.<UTC timestamp> first, and both sha256 values (outgoing and
# incoming) are printed so the run's host state is auditable from its own output.
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
BINARY_ONLY=0
PREV_WRITTEN=0
saved_sha=""

die() { echo "ERROR: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --telegram-token) TOKEN="${2:-}"; shift 2 ;;
    --telegram-chat)  CHAT="${2:-}";  shift 2 ;;
    --env-file)       SOURCE_ENV="${2:-}"; shift 2 ;;
    --bin)            BIN="${2:-}"; shift 2 ;;
    --binary-only)    BINARY_ONLY=1; shift ;;
    --dry-run)        DRY_RUN=1; shift ;;
    -h|--help)        sed -n '3,/^set -uo pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

[ "$(id -u)" = 0 ] || die "must run as root (writes /etc and installs cron)"
command -v sha256sum >/dev/null 2>&1 || die "sha256sum not found — the installer must record what it replaces"

# ── 1. the watchdog itself — SAVE the outgoing copy, then install ───────────
# Order matters and is the whole point of this step: the live copy is the only
# one that can carry host-local drift, so it is copied aside and VERIFIED before
# anything overwrites it. If the save fails, the install does not happen — an
# overwrite with no backup is the state this step exists to make impossible.
SRC="$HERE/jomform-watch.sh"
[ -f "$SRC" ] || die "$SRC not found"
PREV="$BIN.prev"
TS="$(date -u +%Y%m%d-%H%M%S)"
sha256_of() { sha256sum "$1" | cut -d' ' -f1; }

incoming_sha="$(sha256_of "$SRC")"
incoming_size="$(stat -c %s "$SRC")"
installed_sha=""
installed_size=""
if [ -f "$BIN" ]; then
  installed_sha="$(sha256_of "$BIN")"
  installed_size="$(stat -c %s "$BIN")"
fi

if [ -z "$installed_sha" ]; then
  drift=0
  echo "binary: incoming  sha256=$incoming_sha size=$incoming_size  ($SRC)"
  echo "binary: installed none — fresh install of $BIN, so there is nothing to keep"
elif [ "$installed_sha" != "$incoming_sha" ]; then
  drift=1
  echo "binary: outgoing  sha256=$installed_sha size=$installed_size  ($BIN)"
  echo "binary: incoming  sha256=$incoming_sha size=$incoming_size  ($SRC)"
  echo "binary: drift — installed sha != incoming sha"
else
  drift=0
  echo "binary: outgoing  sha256=$installed_sha size=$installed_size  ($BIN)"
  echo "binary: incoming  sha256=$incoming_sha size=$incoming_size  ($SRC)"
  echo "binary: no drift — installed sha == incoming sha"
fi

if [ "$DRY_RUN" = 1 ]; then
  if [ "$drift" = 1 ]; then
    echo "dry-run: would save $BIN → $PREV (0700) BEFORE overwriting it"
    if [ -f "$PREV" ]; then
      prev_sha="$(sha256_of "$PREV")"
      if [ "$prev_sha" = "$installed_sha" ]; then
        echo "dry-run: $PREV already holds the outgoing bytes (sha256=$prev_sha) — it would be kept as-is"
      else
        echo "dry-run: the existing $PREV (sha256=$prev_sha) would be ARCHIVED as $PREV.$TS first, never overwritten silently"
      fi
    fi
  elif [ -z "$installed_sha" ]; then
    echo "dry-run: would NOT create $PREV — nothing is installed at $BIN yet"
  else
    echo "dry-run: would NOT create $PREV — the installed bytes already match the incoming bytes"
  fi
  echo "dry-run: would install $SRC → $BIN (0755 root:root)"
else
  if [ "$drift" = 1 ]; then
    # An existing .prev is the only copy of the version before it: archive it
    # rather than write over it, so two consecutive installs leave two
    # recoverable versions instead of one.
    if [ -f "$PREV" ]; then
      prev_sha="$(sha256_of "$PREV")"
      if [ "$prev_sha" = "$installed_sha" ]; then
        echo "kept the existing $PREV (sha256=$prev_sha) — it already holds the outgoing bytes"
      else
        archive="$PREV.$TS"
        n=1
        while [ -e "$archive" ]; do archive="$PREV.$TS-$n"; n=$((n+1)); done
        mv -- "$PREV" "$archive" \
          || die "could not archive the existing $PREV → $archive — refusing to overwrite a backup holding an older watchdog"
        echo "archived the previous backup $PREV → $archive (sha256=$prev_sha, 0700)"
      fi
    fi
    install -m 0700 -o root -g root "$BIN" "$PREV" \
      || die "could not save $BIN → $PREV — refusing to overwrite the live watchdog with no backup"
    saved_sha="$(sha256_of "$PREV")"
    [ "$saved_sha" = "$installed_sha" ] \
      || die "the saved $PREV hashes $saved_sha, not the outgoing $installed_sha — refusing to overwrite the live watchdog"
    echo "saved the outgoing $BIN ($installed_size bytes) → $PREV (0700) sha256=$saved_sha"
    echo "revert (one command): install -m 0755 -o root -g root $PREV $BIN"
    PREV_WRITTEN=1
  elif [ -z "$installed_sha" ]; then
    echo "no backup written — nothing was installed at $BIN yet"
  else
    echo "no backup written — the installed bytes already match the incoming bytes, so nothing would be undone by a revert"
  fi
  install -m 0755 -o root -g root "$SRC" "$BIN" || {
    [ "$PREV_WRITTEN" = 1 ] && echo "the previous watchdog is still at $PREV — revert with: install -m 0755 -o root -g root $PREV $BIN" >&2
    die "install of $SRC → $BIN failed"
  }
  installed_after="$(sha256_of "$BIN")"
  if [ "$installed_after" != "$incoming_sha" ]; then
    echo "ERROR: $BIN hashes $installed_after, not the incoming $incoming_sha" >&2
    [ "$PREV_WRITTEN" = 1 ] && echo "  the previous watchdog is still at $PREV — revert with: install -m 0755 -o root -g root $PREV $BIN" >&2
    die "the install did not land the bytes that were verified"
  fi
  echo "installed $BIN (0755) sha256=$installed_after size=$(stat -c %s "$BIN")"
fi

if [ "$BINARY_ONLY" = 1 ]; then
  echo
  echo "binary-only: stopping after step 1 — $ENV_FILE, the delivery proof and $CRON_FILE were NOT touched"
  [ "$PREV_WRITTEN" = 1 ] && echo "revert (one command): install -m 0755 -o root -g root $PREV $BIN"
  exit 0
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
  # A herestring rather than `printf '%s' "$proof" | grep -q`: `grep -q` exits at
  # the first match, so the writer dies of SIGPIPE (141) under this file's
  # `set -uo pipefail` and the pipeline then reports failure although grep
  # MATCHED — a delivered alert would read as a dead notifier and refuse to arm
  # the timer. See deploy/monitoring/jomform-watch_test.sh's matcher note.
  if grep -q 'notify_sent' <<<"$proof"; then
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

if [ "$PREV_WRITTEN" = 1 ]; then
  cat <<EOF

ROLLBACK — this run REPLACED the watchdog that was here before it:
  install -m 0755 -o root -g root $PREV $BIN
That restores sha256=$saved_sha (the bytes this run found installed).
  sha256sum $BIN $PREV          # confirm what is running now
EOF
fi
