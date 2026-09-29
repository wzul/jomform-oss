#!/usr/bin/env bash
#
# deploy/tmp-apikey.sh — the ONE supported way to mint and remove a TEMPORARY
# API key on a JomForm deployment host.
#
# WHY THIS EXISTS (read this before hand-rolling another psql INSERT)
#   Deploy, verify and QA cards each minted their own temp key: generate a raw
#   key, INSERT its sha256 into api_keys, capture the returned id, then DELETE by
#   that id. Three separate defects came out of that shape, all measured on prod
#   on 2026-09-18/19:
#
#   1. `psql -At -c "… RETURNING id"` prints the command tag on the SAME stdout
#      line, so the capture received `<uuid>INSERT 0 1`. `tr -d '[:space:]'`
#      cannot remove it, so every `-id` file in /tmp held `<uuid>INSERT01`; the
#      follow-up `DELETE … WHERE id='$KID'` then died with
#      `invalid input syntax for type uuid` and the temp key STAYED LIVE — the
#      exact leftover-credential failure. Six such files were left behind.
#   2. A key minted in the wrong shape (`jf_tmp_…`) is one the API can never
#      accept, so the "verification" it was minted for proved nothing. The real
#      shape is auth.GenerateAPIKey's: `jfk` + `_` + 48 hex
#      (api/internal/auth/apikey.go).
#   3. Cleanup written as a LIKE sweep would delete a SIBLING card's live key the
#      moment two cards ran at once.
#
#   So this script does not capture an id at all where it can avoid it: it
#   deletes by EXACT NAME and then proves with a fresh read that no row for that
#   name survives. Any id it does print is extracted with a uuid regex, so the
#   `INSERT 0 1` tag cannot ride along.
#
# USAGE
#   deploy/tmp-apikey.sh mint --task t_abc123 [--slug wan] [--scope admin] [--out-dir /tmp]
#   deploy/tmp-apikey.sh rm   --task t_abc123 [--key-file /tmp/jf-tmpkey-t_abc123.raw]
#                            [--base https://api.jomform.com]
#   deploy/tmp-apikey.sh --help
#
# NAME / SCOPE
#   The row is named `deploy-verify-tmp-<task>` — per CARD, never per lane — so
#   `rm` can only ever touch this card's row. A name containing a `%` or a `_`
#   that would act as a wildcard is refused; there is no LIKE mode.
#
# THE RAW KEY NEVER TOUCHES STDOUT. It is written to `<out-dir>/jf-tmpkey-<task>.raw`
# at mode 600; only its sha256 (safe) and the row id are printed. `rm` uses that
# file only to prove the deleted key no longer authenticates.
#
# EXIT CODES
#   0  the operation succeeded AND its post-condition was verified by a fresh read
#   1  refused / failed — a message on stderr says which assertion failed
#
# ENVIRONMENT
#   JF_HOST      ssh target of the deployment host (default root@203.0.113.10)
#   JF_DIR       compose directory on that host (default /opt/jomform)
#   JF_SSH       ssh command to use (default: ssh)   — overridden by the self-test
#   JF_SCP       scp command to use (default: scp)   — overridden by the self-test
#   JF_CURL      curl command to use (default: curl) — overridden by the self-test
#   JF_LIB_ONLY  set to 1 to source the functions without running main (self-test)

set -uo pipefail

JF_HOST="${JF_HOST:-root@203.0.113.10}"
JF_DIR="${JF_DIR:-/opt/jomform}"
JF_SSH="${JF_SSH:-ssh}"
JF_SCP="${JF_SCP:-scp}"
JF_CURL="${JF_CURL:-curl}"
SSH_OPTS="${JF_SSH_OPTS:--o BatchMode=yes -o StrictHostKeyChecking=accept-new}"
REMOTE_DIR="${JF_REMOTE_DIR:-/tmp}"

ACCOUNT_SLUG_DEFAULT="wan"
SCOPE_DEFAULT="admin"
NAME_PREFIX="deploy-verify-tmp-"

die()  { printf 'tmp-apikey: %s\n' "$*" >&2; exit 1; }
warn() { printf 'tmp-apikey: WARNING: %s\n' "$*" >&2; }
info() { printf 'tmp-apikey: %s\n' "$*"; }

usage() { sed -n '2,/^set -uo pipefail$/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }

# ─── the two extractors that make the INSERT01 class impossible ──────────────

# uuid_from — pull the FIRST uuid out of arbitrary psql stdout. `psql -At -c
# "… RETURNING id"` emits the id and the `INSERT 0 1` command tag on separate
# lines of the same stream; a naive whitespace strip glues them together. This
# cannot: it returns a bare uuid or fails.
#
# NOTE: `sed -n '1p'`, not `head -1`. `head -1` closes the pipe at the first
# match, so grep can die of SIGPIPE; under the `set -o pipefail` above, a
# non-zero grep status turns a SUCCESSFUL match into a non-zero pipeline status.
# Measured on a large multi-match input this does NOT change what uuid_from
# returns here (it reads all matches into a command substitution, where the
# failed `head` contributes no bytes), but the class is real and observed
# elsewhere: it is what makes the self-test's `contains` assertion flaky under
# load (see deploy/tmp-apikey_test.sh). Both readers now consume to EOF — that
# is the rule, and it is why neither can be raced.
uuid_from() {
  local text="$1" id
  id="$(printf '%s' "$text" | grep -oE '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}' | sed -n '1p')"
  [ -n "$id" ] || return 1
  printf '%s' "$id"
}

# valid_name — a temp-key name must be exactly this card's. Anything with a
# LIKE metacharacter, or a bare prefix, is refused: that is what turns a cleanup
# into a sweep of a sibling card's live key.
valid_name() {
  local name="$1"
  case "$name" in
    *%*|*'*'*|*'?'*|*'['*) return 1 ;;
  esac
  grep -qE "^${NAME_PREFIX}[A-Za-z0-9._-]+$" <<<"$name"
}

# valid_key_shape — the shape auth.GenerateAPIKey produces (jfk + 48 hex).
# A key outside this shape can never authenticate, so minting one would produce
# a verification that proves nothing.
valid_key_shape() {
  grep -qE '^jfk_[0-9a-f]{48}$' <<<"$1"
}

# ─── transport: SQL goes to the host as a FILE, never as nested shell quoting ─

# remote_sql <local-sql-file> — run it through the host's compose postgres and
# print psql's stdout. The SQL is scp'd and piped into `docker compose exec` from
# a runner script, so (a) `docker compose exec` cannot swallow the caller's stdin
# and (b) no SQL is interpolated through nested quoting.
remote_sql() {
  local sql="$1" work sql_name runner_name
  sql_name="$(basename "$sql")"
  runner_name="${sql_name%.sql}.run.sh"
  work="$(mktemp -d "${JF_STAGE_ROOT:-/tmp}/jf-tmpkey.XXXXXX")" || die "could not create a staging dir"

  cat >"$work/$runner_name" <<RUNNER
set -uo pipefail
cd '$JF_DIR'
docker compose exec -T postgres sh -c 'psql -U "\$POSTGRES_USER" -d "\$POSTGRES_DB" -v ON_ERROR_STOP=1 -At -F"|"' < '$REMOTE_DIR/$sql_name'
RUNNER

  $JF_SCP $SSH_OPTS "$sql" "$work/$runner_name" "$JF_HOST:$REMOTE_DIR/" >/dev/null \
    || die "scp of the query to $JF_HOST failed"
  $JF_SSH $SSH_OPTS "$JF_HOST" "bash '$REMOTE_DIR/$runner_name'" 2>&1 \
    | grep -v 'JF_CF_OAUTH_CLIENT_SECRET'
  rm -rf "$work"
}

# field <psql-output> <MARKER> — read a `MARKER|value` line out of psql stdout.
# The pipeline stays: `tail -1` must read to EOF to know the last line, so the
# upstream grep is never the reader that exits early, and `cut` writes one short
# line. Measured on a 20000-marker haystack: 0 wrong values in 300 calls. This is
# NOT the `grep -q`-with-an-unread-writer shape the rest of this file avoids.
field() {
  printf '%s' "$1" | grep -F "$2|" | tail -1 | cut -d'|' -f2
}

# name_exact <names-value> <name> — is a read-back value EXACTLY this card's name?
# A whole-line match; a prefix match would hide the sweep this script refuses.
# The value is read from a herestring, NOT piped: the caller's value is a
# haystack, and `grep -qx` exits at the first match, so a pipe would let the
# writer die of SIGPIPE (141) under the `set -uo pipefail` above and report a
# MATCH as a failure. As shipped the query is exact-name so the value is one
# line and this never fired (measured 0 false reads in 3000 calls on that value).
# On a multi-line read-back (one name per line, needle on line 1) the pipe form
# does report the present name as missing — 65/300 at 2000 lines, 300/300 at
# 5000 — so the herestring is what keeps a future edit to a list-shaped query
# from turning this into the class of bug the file exists to prevent.
name_exact() {
  grep -qx -- "$2" <<<"$1"
}

# ─── mint ────────────────────────────────────────────────────────────────────

cmd_mint() {
  local task="" slug="$ACCOUNT_SLUG_DEFAULT" scope="$SCOPE_DEFAULT" out_dir="/tmp" force=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --task)    task="${2:-}"; shift ;;
      --slug)    slug="${2:-}"; shift ;;
      --scope)   scope="${2:-}"; shift ;;
      --out-dir) out_dir="${2:-}"; shift ;;
      --force)   force=1 ;;
      *) die "mint: unknown argument '$1'" ;;
    esac
    shift
  done
  [ -n "$task" ] || die "mint: --task <card id> is required (the name is per CARD)"
  grep -qE '^[A-Za-z0-9._-]+$' <<<"$task" || die "mint: --task '$task' has characters outside [A-Za-z0-9._-]"
  grep -qE '^(readonly|readwrite|admin)$' <<<"$scope" || die "mint: --scope must be readonly|readwrite|admin"
  local name="${NAME_PREFIX}${task}"
  valid_name "$name" || die "mint: refusing to use the name '$name'"

  local key_file="$out_dir/jf-tmpkey-$task.raw"
  if [ -e "$key_file" ] && [ "$force" != 1 ]; then
    die "mint: $key_file already exists — reuse that key, or pass --force to mint a new one (and rm the old row first)"
  fi

  local raw hash
  raw="jfk_$(openssl rand -hex 24)"
  valid_key_shape "$raw" || die "mint: generated key does not match the API's expected shape (jfk_ + 48 hex)"
  hash="$(printf '%s' "$raw" | sha256sum | cut -d' ' -f1)"

  # PRECONDITION FIRST, in its own query. Checking "does a row already exist"
  # after the INSERT would mean a refused mint still left a live key behind —
  # the failure mode this script exists to prevent.
  local pre; pre="$(mktemp /tmp/jf-tmpkey-pre.XXXXXX.sql)"
  cat >"$pre" <<SQL
\pset pager off
SELECT 'BEFORE|' || count(*) FROM api_keys WHERE name = '$name';
SELECT 'LIVE|' || count(*) FROM api_keys WHERE name = '$name' AND revoked_at IS NULL;
SELECT 'ACCOUNT|' || count(*) FROM accounts WHERE slug = '$slug';
SQL
  local pre_out; pre_out="$(remote_sql "$pre")" || die "mint: the precondition query failed"
  rm -f "$pre"
  local before live account
  before="$(field "$pre_out" BEFORE)"
  live="$(field "$pre_out" LIVE)"
  account="$(field "$pre_out" ACCOUNT)"
  [ "$account" = "1" ] || die "mint: expected exactly 1 account with slug '$slug', got '${account:-0}'"
  [ "$before" = "0" ] || die "mint: $before row(s) named '$name' already exist (live: ${live:-?}) — rm this card's temp key first; minting again would leave more than one live credential"

  umask 077
  printf '%s' "$raw" >"$key_file" || die "mint: could not write $key_file"
  chmod 600 "$key_file"

  local sql; sql="$(mktemp /tmp/jf-tmpkey-mint.XXXXXX.sql)"
  cat >"$sql" <<SQL
\pset pager off
INSERT INTO api_keys (account_id, key_hash, name, scopes)
SELECT id, '$hash', '$name', ARRAY['$scope'] FROM accounts WHERE slug = '$slug' LIMIT 1
RETURNING 'MINTED|' || id;
SELECT 'AFTER|' || count(*) FROM api_keys WHERE name = '$name';
SELECT 'LIVE|' || count(*) FROM api_keys WHERE name = '$name' AND revoked_at IS NULL;
SELECT 'NAMES|' || COALESCE(string_agg(name, ','), '') FROM api_keys WHERE name = '$name';
SELECT 'MINE|' || COALESCE(string_agg(id::text, ','), '') FROM api_keys WHERE name = '$name';
SQL

  local out; out="$(remote_sql "$sql")" || die "mint: the insert query failed"
  rm -f "$sql"

  local after minted id
  after="$(field "$out" AFTER)"
  live="$(field "$out" LIVE)"
  minted="$(field "$out" MINTED)"

  [ -n "$minted" ] || die "mint: no row was inserted (psql said: $(printf '%s' "$out" | tr '\n' ' ' | cut -c1-300))"
  id="$(uuid_from "$minted")" || die "mint: could not extract a uuid from the RETURNING output '[$(printf '%s' "$minted" | tr -d '\n')]' — a uuid regex is required because psql appends its command tag"
  [ "$after" = "1" ] || die "mint: expected exactly 1 row named '$name' after insert, got '$after'"
  [ "$live" = "1" ] || die "mint: the row named '$name' is not live (revoked?)"
  name_exact "$(field "$out" NAMES)" "$name" || die "mint: the row names read back as '$(field "$out" NAMES)' — expected exactly '$name'"
  [ "$(field "$out" MINE)" = "$id" ] || die "mint: the row just read back has id '$(field "$out" MINE)', not the returned '$id' — the RETURNING id and the stored row disagree"

  info "minted: name=$name id=$id scope=$scope"
  info "raw key: $key_file (mode 600, never printed)  sha256=$hash"
  info "clean up with: $0 rm --task $task --key-file $key_file --base https://api.jomform.com"
}

# ─── rm ──────────────────────────────────────────────────────────────────────

cmd_rm() {
  local task="" key_file="" base="" allow_missing=0
  while [ $# -gt 0 ]; do
    case "$1" in
      --task)         task="${2:-}"; shift ;;
      --key-file)     key_file="${2:-}"; shift ;;
      --base)         base="${2:-}"; shift ;;
      --allow-missing) allow_missing=1 ;;
      *) die "rm: unknown argument '$1'" ;;
    esac
    shift
  done
  [ -n "$task" ] || die "rm: --task <card id> is required"
  grep -qE '^[A-Za-z0-9._-]+$' <<<"$task" || die "rm: --task '$task' has characters outside [A-Za-z0-9._-]"
  local name="${NAME_PREFIX}${task}"
  valid_name "$name" || die "rm: refusing to delete by the name '$name' (a LIKE sweep can take a sibling card's live key)"

  # Every precondition is checked BEFORE the delete. A guard that only fires
  # afterwards would refuse a run whose credential is already gone — the state
  # (a key minted, unproven, deleted) that leaves an operator guessing.
  local raw=""
  if [ -n "$base" ]; then
    [ -n "$key_file" ] && [ -r "$key_file" ] || die "rm: --base given but --key-file is missing/unreadable ($key_file), so the dead-key proof cannot run. Pass the file mint printed, or drop --base to skip the proof (the row is still deleted, but nothing then proves the credential is dead)."
    raw="$(cat "$key_file")"
    valid_key_shape "$raw" || die "rm: $key_file does not hold a key of the API's shape (jfk_ + 48 hex) — the dead-key proof would be meaningless"
    $JF_CURL -s -o /dev/null -w '' --max-time 10 "$base/healthz" >/dev/null 2>&1 \
      || warn "rm: $base/healthz did not answer — the dead-key proof may fail; the row delete will still run"
  fi

  local sql; sql="$(mktemp /tmp/jf-tmpkey-rm.XXXXXX.sql)"
  cat >"$sql" <<SQL
\pset pager off
SELECT 'BEFORE|' || count(*) FROM api_keys WHERE name = '$name';
SELECT 'IDS|' || COALESCE(string_agg(id::text, ','), '') FROM api_keys WHERE name = '$name';
DELETE FROM api_keys WHERE name = '$name';
SELECT 'AFTER|' || count(*) FROM api_keys WHERE name = '$name';
SELECT 'FOREIGN|' || count(*) FROM api_keys WHERE name LIKE '${NAME_PREFIX}%';
SQL

  local out; out="$(remote_sql "$sql")" || die "rm: the delete query failed"
  rm -f "$sql"

  local before after ids foreign
  before="$(field "$out" BEFORE)"; after="$(field "$out" AFTER)"
  ids="$(field "$out" IDS)"; foreign="$(field "$out" FOREIGN)"

  # THE post-condition: a fresh read must show nothing left under this exact
  # name. A `DELETE 0` looks like success in a log, so count it explicitly.
  [ "$after" = "0" ] || die "rm: $after row(s) named '$name' SURVIVED the delete — the temp key is still live. ids before: '${ids:-none}'"
  if [ "$before" = "0" ]; then
    if [ "$allow_missing" = 1 ]; then
      warn "nothing was named '$name' (already cleaned up); nothing deleted"
    else
      die "rm: no row named '$name' existed, so nothing was deleted — if that is expected (a re-run), pass --allow-missing; otherwise the name is wrong and the real row is still live"
    fi
  else
    info "deleted $before row(s) named '$name' (ids: $ids)"
  fi
  info "verified by fresh read: rows named '$name' = 0 (rows under the tmp prefix overall: $foreign — siblings untouched by construction)"

  # Live proof that the credential is dead. Worth doing separately: a row count
  # proves the DELETE ran, not that the key stopped authenticating.
  if [ -n "$base" ]; then
    local code
    code="$($JF_CURL -s -o /dev/null -w '%{http_code}' --max-time 15 -H "Authorization: Bearer $raw" "$base/v1/rest/forms" || echo 000)"
    case "$code" in
      401|403) info "dead-key proof: GET $base/v1/rest/forms with the deleted key -> $code (revoked)" ;;
      000)     warn "could not reach $base to prove the key is dead — the row is gone, but the dead-key half is UNPROVEN" ;;
      *)       die "rm: the deleted key STILL answers $code on $base/v1/rest/forms — the row is gone but the credential is not revoked" ;;
    esac
    info "raw key file $key_file is no longer a credential; shred it when you are done"
  fi
}

# ─── main ────────────────────────────────────────────────────────────────────

main() {
  local cmd="${1:-}"
  case "$cmd" in
    mint) shift; cmd_mint "$@" ;;
    rm)   shift; cmd_rm "$@" ;;
    -h|--help|help|"") usage; [ -n "$cmd" ] || exit 1 ;;
    *) die "unknown command '$cmd' (try --help)" ;;
  esac
}

if [ "${JF_LIB_ONLY:-0}" != "1" ]; then
  main "$@"
fi
