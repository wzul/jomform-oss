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
#
# Hermetic: a stubbed `docker`/`id` on PATH, and `--root` pointed at a temp dir.
# The only things read outside the temp dir are the repo's own files under
# deploy/, which is what cases 5 and 6 are about.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/bootstrap-host.sh"
COMPOSE="$HERE/../vps/compose.prod.yml"
[ -x "$SCRIPT" ] || { echo "bootstrap-host_test: $SCRIPT is not executable" >&2; exit 1; }
[ -s "$COMPOSE" ] || { echo "bootstrap-host_test: missing $COMPOSE" >&2; exit 1; }

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

PASS=0; FAIL=0
ok()  { echo "  ok   $1"; PASS=$((PASS + 1)); }
bad() { echo "  FAIL $1" >&2; FAIL=$((FAIL + 1)); }

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

echo "case: DRY-RUN is the default, prints the plan, and changes NOTHING"
mkdir -p "$WORK/opt"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
[ "$RC" -eq 0 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected 0): $OUT"
echo "$OUT" | grep -q "mode=DRY-RUN" && ok "says it is a dry run" || bad "did not say DRY-RUN"
echo "$OUT" | grep -q "would: install compose.prod.yml → $WORK/opt/jomform/compose.yml" && ok "plans the prod compose install" || bad "did not plan the compose install"
echo "$OUT" | grep -q "env contract: all $KEY_COUNT keys" && ok "checks the env contract against the compose ($KEY_COUNT keys)" || bad "did not report the env contract"
echo "$OUT" | grep -q "dokploy-network" && ok "plans the external network (the compose declares it external)" || bad "did not plan the network"
echo "$OUT" | grep -q "compose.yml up -d" && ok "plans the stack up" || bad "did not plan the stack up"
echo "$OUT" | grep -q "jomform-backup.sh" && ok "plans the nightly-backup cron line" || bad "did not plan the backup cron"
echo "$OUT" | grep -q "jomform-container-prune.sh" && ok "plans the prune cron line" || bad "did not plan the prune cron"
echo "$OUT" | grep -q "DRY-RUN — nothing was changed" && ok "states nothing changed" || bad "did not state nothing changed"
if [ -z "$(ls -A "$WORK/opt" 2>/dev/null)" ]; then ok "the install root is still EMPTY (no write happened)"; else bad "dry-run WROTE to the install root: $(ls -A "$WORK/opt")"; fi

echo "case: it says out loud what it does NOT do (data move, DNS, credentials)"
echo "$OUT" | grep -q "MOVE THE DATA" && ok "names the data move as not-done" || bad "did not name the data move"
echo "$OUT" | grep -q -- "--restore" && ok "prints the restore command" || bad "did not print the restore command"
echo "$OUT" | grep -q "point DNS at this host" && ok "names DNS as not-done" || bad "did not name DNS"

echo "case: no --env-file is refused (there is no default to guess)"
run "$WORK/bin" --root "$WORK/opt"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "exit code (exit=$RC, expected non-zero)"
echo "$OUT" | grep -q "REFUSED — --env-file is required" && ok "names the missing argument" || bad "did not name the missing argument"

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
echo "$OUT" | grep -q "mode 644" && ok "names the mode" || bad "did not name the mode"

echo "case: an env file MISSING a key the compose reads is refused, and the key is NAMED"
grep -v '^SES_SMTP_PASS=' "$WORK/good.env" > "$WORK/missing.env"; chmod 600 "$WORK/missing.env"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/missing.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted an env file with a missing credential"
echo "$OUT" | grep -q "SES_SMTP_PASS" && ok "names the missing key" || bad "did not name the missing key"

echo "case: the key list comes FROM THE COMPOSE — a key added to the compose must be named"
# Plant an extra ${KEY} in a COPY of the compose and pass it with --compose-src.
sed 's/^      APP_ENV: prod$/      APP_ENV: prod\n      PLANTED_KEY: ${PLANTED_KEY}/' "$COMPOSE" > "$WORK/compose.planted.yml"
grep -q "PLANTED_KEY" "$WORK/compose.planted.yml" && ok "the planted compose carries the new key" || bad "the mutation did not apply"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --compose-src "$WORK/compose.planted.yml"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "a compose key no env file can supply was NOT refused"
echo "$OUT" | grep -q "PLANTED_KEY" && ok "names the planted key (the contract is derived, not written)" || bad "did not name the planted key"

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
  echo "$OUT" | grep -q "env contract: all $KEY_COUNT keys" \
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
echo "$OUT" | grep -q "docker is not installed" && ok "names the missing prerequisite" || bad "did not name the missing prerequisite"

echo "case: --apply as a non-root user is refused"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --apply
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "accepted --apply as an unprivileged user"
echo "$OUT" | grep -q -- "--apply needs root" && ok "names the root requirement" || bad "did not name the root requirement"

echo "case: an existing DIFFERING file is not clobbered without --force"
mkdir -p "$WORK/opt/jomform"
echo "hand-edited on the host" > "$WORK/opt/jomform/compose.yml"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
[ "$RC" -ne 0 ] && ok "exit code (exit=$RC)" || bad "clobbered a differing live file"
echo "$OUT" | grep -q "refusing to clobber" && ok "says why it refused" || bad "did not say why"
grep -q "hand-edited on the host" "$WORK/opt/jomform/compose.yml" && ok "the existing file is untouched" || bad "the existing file was modified"

echo "case: with --force the same host plans a BACKUP before the install"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env" --force
[ "$RC" -eq 0 ] && ok "exit code (exit=$RC)" || bad "still refused with --force: $OUT"
echo "$OUT" | grep -q "would: back up the differing" && ok "plans the backup" || bad "did not plan a backup"
BK_LINE="$(echo "$OUT" | grep -n "would: back up the differing" | head -1 | cut -d: -f1)"
IN_LINE="$(echo "$OUT" | grep -n "would: install compose.prod.yml" | head -1 | cut -d: -f1)"
[ -n "$BK_LINE" ] && [ -n "$IN_LINE" ] && [ "$BK_LINE" -lt "$IN_LINE" ] && ok "the backup is planned BEFORE the install (order matters)" || bad "the install is planned before the backup"

echo "case: an already-identical file is a no-op, not a backup churn"
cp "$COMPOSE" "$WORK/opt/jomform/compose.yml"
run "$WORK/bin" --root "$WORK/opt" --env-file "$WORK/good.env"
echo "$OUT" | grep -q "already identical: $WORK/opt/jomform/compose.yml" && ok "says it is already identical (idempotent)" || bad "did not report the identical file"

echo
echo "bootstrap-host_test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
