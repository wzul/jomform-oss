#!/usr/bin/env bash
#
# deploy/tmp-apikey_test.sh — self-test for deploy/tmp-apikey.sh.
#
# WHY
#   tmp-apikey.sh exists to make one class of failure impossible: a temporary
#   API key that survives its own cleanup. Every assertion below is about that
#   class — the id capture that cannot carry psql's `INSERT 0 1` tag, the delete
#   that cannot widen to a sibling card's key, and the post-conditions that are
#   read back with a FRESH query instead of trusted from the statement's output.
#
#   It runs entirely offline: a fake ssh/scp pair, a fake `docker` that answers
#   the queries from a state file, and a fake curl. No prod host, no network, no
#   real credentials. The canned psql replies deliberately reproduce the exact
#   shape that caused the bug (`<uuid>` on one line, `INSERT 0 1` on the next),
#   so the extraction is tested against the real failure, not a convenient one.
#
# Run:  bash deploy/tmp-apikey_test.sh
# Exit: 0 when every property holds, 1 otherwise.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/tmp-apikey.sh"
[ -f "$SCRIPT" ] || { echo "FAIL: $SCRIPT missing" >&2; exit 1; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL %s\n' "$1"; }
check() { # desc expected actual
  if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (got '$3', want '$2')"; fi
}
contains() { # desc haystack needle
  # A herestring, NOT `printf '%s' "$2" | grep -qF -- "$3"`. `grep -q` exits at
  # the first match, so under `set -o pipefail` the writer's SIGPIPE (141) wins
  # the pipeline's status and a haystack that DOES contain the needle is
  # reported as missing it. Measured on this host: 5 false negatives in 3000
  # calls at load ~33, 0 in 2000 idle — which is why it read as a load flake.
  # A herestring gives grep a file descriptor, so no writer process is ever
  # signalled and the assertion depends only on the text.
  if grep -qF -- "$3" <<<"$2"; then ok "$1"
  else bad "$1 (missing '$3' in: $(printf '%s' "$2" | tr '\n' ' ' | cut -c1-260))"; fi
}

SB="$(mktemp -d /tmp/jf-tmpapikey-test.XXXXXX)"
cleanup() { rm -rf "$SB"; }
trap cleanup EXIT

REPO_MOCK="$SB/repo"          # stands in for the compose dir on the "host"
mkdir -p "$REPO_MOCK" "$SB/remote" "$SB/bin" "$SB/out"
STATE="$SB/api_keys.state"    # 'id|name' lines, the fake api_keys table
: >"$STATE"

# ─── fake `docker compose exec … psql` ────────────────────────────────────────
# Answers the two statement families tmp-apikey.sh emits, from $STATE. The INSERT
# reply carries the command tag on its own line, exactly like real psql.
cat >"$SB/bin/psql-responder.sh" <<'PY'
#!/usr/bin/env bash
# Runs with pipefail ON deliberately: the whole point of this harness is to catch
# the class of bug where a pipe's reader exits early and the writer's SIGPIPE
# (status 141) is then reported as a failure even though the match succeeded.
# Without pipefail here, a regression to `… | head -1` would still look green.
set -uo pipefail
STATE="${JF_TEST_STATE:?JF_TEST_STATE must be set by the harness}"
count_name() { # name
  awk -F'|' -v n="$1" '$2==n' "$STATE" | wc -l | tr -d ' '
}
count_prefix() {
  awk -F'|' '$2 ~ /^deploy-verify-tmp-/' "$STATE" | wc -l | tr -d ' '
}
sql="$(cat)"
# psql's own command tag, on its own line — the thing that corrupts a capture.
tag() { printf '%s\n' "$1"; }
# `sed -n '1p'`, never `head -1`: see the note in tmp-apikey.sh's uuid_from.
name_in_sql() { printf '%s' "$sql" | sed -nE "s/.*name = '([^']+)'.*/\1/p" | sed -n '1p'; }

# Replies are emitted in the SAME ORDER the statements would execute, because a
# post-condition read that the real psql runs AFTER the delete must not be
# answered from the pre-delete state — that is exactly the "DELETE 0 looks like
# success" trap this suite exists to catch.
#
# Every match below is a herestring, not `printf … | grep`: `grep -q` exits at
# the first match, so a pipe would make the writer die of SIGPIPE (141) and
# pipefail would report a matching statement as absent. Herestrings have no
# writer process to signal.
if grep -q "SELECT 'BEFORE" <<<"$sql"; then
  tag "BEFORE|$(count_name "$(name_in_sql)")"
fi
if grep -q "SELECT 'ACCOUNT" <<<"$sql"; then
  tag "ACCOUNT|1"
fi
if grep -q "INSERT INTO api_keys" <<<"$sql"; then
  name="$(grep -oE "deploy-verify-tmp-[A-Za-z0-9._-]+" <<<"$sql" | sed -n '1p')"
  # A deterministic id per name so assertions can name it.
  id="$(printf '%s' "$name" | md5sum | sed -E 's/^(.{8})(.{4})(.{4})(.{4})(.{12}).*/\1-\2-\3-\4-\5/')"
  printf '%s|%s\n' "$id" "$name" >>"$STATE"
  tag "MINTED|$id"
  tag "INSERT 0 1"     # <-- the real psql behaviour that produced <uuid>INSERT01
fi
if grep -q "SELECT 'MINE" <<<"$sql"; then
  n="$(name_in_sql)"
  tag "MINE|$(awk -F'|' -v n="$n" '$2==n{print $1}' "$STATE" | paste -sd, -)"
fi
if grep -q "SELECT 'IDS" <<<"$sql"; then
  n="$(name_in_sql)"
  tag "IDS|$(awk -F'|' -v n="$n" '$2==n{print $1}' "$STATE" | paste -sd, -)"
fi
if grep -q "DELETE FROM api_keys" <<<"$sql"; then
  n="$(name_in_sql)"
  before="$(count_name "$n")"
  # JF_TEST_DELETE_NOOP models the failure the post-condition exists to catch: the
  # DELETE reports a row count but the row is still there afterwards. Without this
  # knob, no test can reach the "SURVIVED" branch at all.
  if [ "${JF_TEST_DELETE_NOOP:-0}" = "1" ]; then
    tag "DELETE $before"
  else
    # Delete ONLY rows whose name field is exactly $n — a prefix match here would
    # hide the very sweep the script is meant to refuse.
    awk -F'|' -v n="$n" '$2==n{next}{print}' "$STATE" >"$STATE.tmp" && mv "$STATE.tmp" "$STATE"
    tag "DELETE $before"
  fi
fi
if grep -q "SELECT 'AFTER" <<<"$sql"; then
  tag "AFTER|$(count_name "$(name_in_sql)")"
fi
if grep -q "SELECT 'LIVE" <<<"$sql"; then
  tag "LIVE|$(count_name "$(name_in_sql)")"
fi
if grep -q "SELECT 'NAMES" <<<"$sql"; then
  n="$(name_in_sql)"
  tag "NAMES|$(awk -F'|' -v n="$n" '$2==n{print $2}' "$STATE" | paste -sd, -)"
fi
if grep -q "SELECT 'FOREIGN" <<<"$sql"; then
  tag "FOREIGN|$(count_prefix)"
fi
exit 0
PY
chmod +x "$SB/bin/psql-responder.sh"

# `docker` is shadowed: any `docker …` call is answered by the responder above,
# with the SQL that arrived on stdin. The fake api_keys table lives in the sandbox.
cat >"$SB/bin/docker" <<EOF
#!/usr/bin/env bash
exec env JF_TEST_STATE="$STATE" bash "$SB/bin/psql-responder.sh"
EOF
chmod +x "$SB/bin/docker"

# ─── fake transport ──────────────────────────────────────────────────────────
# Copies each source file into $JF_REMOTE_DIR — the sandbox directory standing in
# for the host's remote dir, which the runner script then reads. Options (-o X,
# -q), the ssh target and the dest dir are skipped.
cat >"$SB/bin/fake-scp" <<'EOF'
#!/usr/bin/env bash
: "${JF_REMOTE_DIR:?JF_REMOTE_DIR must be set}"
mkdir -p "$JF_REMOTE_DIR"
for a in "$@"; do
  case "$a" in
    -*) continue ;;
    *:*) continue ;;                 # the user@host:path destination
  esac
  [ -f "$a" ] || continue
  cp "$a" "$JF_REMOTE_DIR/$(basename -- "$a")"
done
exit 0
EOF
chmod +x "$SB/bin/fake-scp"

# Runs the remote command. The sandbox doubles as the "host's" remote dir, so the
# runner script's own reference to the scp'd SQL resolves inside the sandbox —
# no path rewriting, and the paths on both sides stay identical to a real run.
cat >"$SB/bin/fake-ssh" <<'EOF'
#!/usr/bin/env bash
cmd="${!#}"
PATH="$(dirname "$0"):$PATH" bash -c "$cmd"
EOF
chmod +x "$SB/bin/fake-ssh"

# curl is only used for the dead-key proof; its answer is switched by $SB/curl_code.
cat >"$SB/bin/fake-curl" <<EOF
#!/usr/bin/env bash
code="\$(cat "$SB/curl_code" 2>/dev/null || echo 401)"
for a in "\$@"; do case "\$a" in %{http_code}) printf '%s' "\$code" ;; esac; done
exit 0
EOF
chmod +x "$SB/bin/fake-curl"
echo 401 >"$SB/curl_code"

# ─── run under the fakes ─────────────────────────────────────────────────────
run_key() { # extra args…
  env JF_HOST="fakehost" JF_DIR="$REPO_MOCK" JF_REMOTE_DIR="$SB/remote" \
      JF_STAGE_ROOT="$SB" ${JF_TEST_DELETE_NOOP:+JF_TEST_DELETE_NOOP="$JF_TEST_DELETE_NOOP"} \
      JF_SSH="$SB/bin/fake-ssh" JF_SCP="$SB/bin/fake-scp" JF_CURL="$SB/bin/fake-curl" \
      PATH="$SB/bin:$PATH" bash "$SCRIPT" "$@"
}

echo "tmp-apikey self-test (sandbox $SB)"
echo "--- the id capture cannot carry psql's command tag ---"

# 1. The BITE. Reproduce the exact prod failure and require each half to behave
#    differently: the naive capture must yield something that is not a uuid, and
#    uuid_from must return the clean id.
# shellcheck disable=SC1090
( JF_LIB_ONLY=1 . "$SCRIPT" ) >/dev/null 2>&1
LIB_OUT="$( JF_LIB_ONLY=1 bash -c ". '$SCRIPT'; printf '%s' \"\$(uuid_from \"\$1\")\" || echo EXTRACT_FAILED" _ "$(printf 'a7167e2f-db05-4c9e-8ebe-9e2adf7b53c7\nINSERT 0 1')" )"
check "uuid_from extracts a clean uuid from psql's two-line output" \
  "a7167e2f-db05-4c9e-8ebe-9e2adf7b53c7" "$LIB_OUT"

NAIVE="$(printf 'a7167e2f-db05-4c9e-8ebe-9e2adf7b53c7\nINSERT 0 1' | tr -d '[:space:]')"
if grep -qE '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' <<<"$NAIVE"; then
  bad "the naive tr -d capture should NOT be a valid uuid (got '$NAIVE')"
else
  ok "control: the old 'tr -d [:space:]' capture is '$NAIVE' — not a uuid, so a DELETE by it would fail and leave the key live"
fi
check "uuid_from on a tag-only input fails instead of returning junk" \
  "EXTRACT_FAILED" "$( JF_LIB_ONLY=1 bash -c ". '$SCRIPT'; uuid_from 'INSERT 0 1' || echo EXTRACT_FAILED" )"

# 1b. The load-independence control. `contains` used to match its haystack with
#     `printf '%s' "$2" | grep -qF -- "$3"`, and `grep -q` EXITS AT THE FIRST
#     MATCH: the writer is then still writing and dies of SIGPIPE, and under
#     `set -o pipefail` that makes a SUCCESSFUL match report failure. That is the
#     flake this test exists to pin (measured: 1 in 8 full runs under a concurrent
#     build, 0 in 256 idle). The race needs the writer to have more to write when
#     grep exits, so a haystack of MANY MATCHING LINES reproduces it WITHOUT any
#     load — measured 200/200 failures with the pipe, 0/200 with the herestring.
#     A herestring gives grep a descriptor, so there is no writer to signal.
#     Driven through `contains` itself (quietly) so the control exercises the
#     real helper, then reported as ONE assertion: present needle FOUND, absent
#     needle MISSED (without the second half, a `contains` that matched
#     everything would also look green).
BIG_HAY="$(for i in $(seq 1 20000); do printf 'probe-needle line %s\n' "$i"; done)"
PASS_BEFORE=$PASS; FAIL_BEFORE=$FAIL
contains "probe: present needle" "$BIG_HAY" "probe-needle line 1" >/dev/null 2>&1
contains "probe: absent needle"  "$BIG_HAY" "probe-needle line 99999" >/dev/null 2>&1
CONTROL_GOT="$((PASS - PASS_BEFORE))/$((FAIL - FAIL_BEFORE))"
PASS=$PASS_BEFORE; FAIL=$FAIL_BEFORE
check "control: contains() is pipe-free — FOUND the present needle and MISSED the absent one on a 20000-line haystack" \
  "1/1" "$CONTROL_GOT"

# 2. Name safety: a bare prefix, or anything with a LIKE metacharacter, must be
#    refused — that is what stops a cleanup taking a sibling card's key. The quote
#    case is the sharpest one: the name is interpolated into the SQL, so a name
#    that escapes the character class is an injection path, not a cosmetic issue.
for evil in "deploy-verify-tmp-" "deploy-verify-tmp-%" "deploy-verify-tmp-t_a%" "other-prefix-t_abc" \
            "deploy-verify-tmp-t_a'b" "deploy-verify-tmp-t_a b" "deploy-verify-tmp-t_a;DROP"; do
  if ( JF_LIB_ONLY=1 . "$SCRIPT"; valid_name "$evil" ); then
    bad "valid_name accepted '$evil' (a LIKE sweep risk)"
  else
    ok "valid_name refuses '$evil'"
  fi
done
if ( JF_LIB_ONLY=1 . "$SCRIPT"; valid_name "deploy-verify-tmp-t_abc123" ); then
  ok "valid_name accepts this card's exact name"
else
  bad "valid_name rejected a legitimate per-card name"
fi

# 3. Key shape: only jfk + 48 hex can ever authenticate.
if ( JF_LIB_ONLY=1 . "$SCRIPT"; valid_key_shape "jfk_$(printf 'a%.0s' $(seq 1 48))" ); then
  ok "valid_key_shape accepts the API's real shape"
else
  bad "valid_key_shape rejected the API's real shape"
fi
for bad_key in "jf_tmp_abcdef" "jfk_short" "jfk_$(printf 'g%.0s' $(seq 1 48))"; do
  if ( JF_LIB_ONLY=1 . "$SCRIPT"; valid_key_shape "$bad_key" ); then
    bad "valid_key_shape accepted '$bad_key'"
  else
    ok "valid_key_shape refuses '$bad_key'"
  fi
done

echo "--- mint ---"
OUT="$(run_key mint --task t_test001 --out-dir "$SB/out" 2>&1)"; RC=$?
check "mint exits 0" 0 "$RC"
contains "mint reports the per-card name" "$OUT" "name=deploy-verify-tmp-t_test001"
contains "mint prints a clean id (no INSERT01 anywhere)" "$OUT" "id=$(printf 'deploy-verify-tmp-t_test001' | md5sum | sed -E 's/^(.{8})(.{4})(.{4})(.{4})(.{12}).*/\1-\2-\3-\4-\5/')"
if grep -q 'INSERT01\|INSERT 0 1' <<<"$OUT"; then
  bad "mint's output carries psql's command tag"
else
  ok "mint's output carries no command tag"
fi
KEYFILE="$SB/out/jf-tmpkey-t_test001.raw"
[ -f "$KEYFILE" ] && ok "raw key written to $KEYFILE" || bad "raw key file missing"
check "raw key file mode is 600" "600" "$(stat -c '%a' "$KEYFILE" 2>/dev/null)"
if grep -qF -- "$(cat "$KEYFILE" 2>/dev/null)" <<<"$OUT"; then
  bad "mint PRINTED the raw key"
else
  ok "mint never prints the raw key"
fi
check "the minted key has the API's real shape" "yes" \
  "$( ( JF_LIB_ONLY=1 . "$SCRIPT"; valid_key_shape "$(cat "$KEYFILE")" ) && echo yes || echo no )"

echo "--- mint's guards ---"
OUT="$(run_key mint --task t_test001 --out-dir "$SB/out" 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a second mint without --force is refused" "$OUT" "already exists" \
  || bad "a second mint was allowed to overwrite the key file"
OUT="$(run_key mint --task t_test001 --out-dir "$SB/out" --force 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "minting over a LIVE row of the same name is refused" "$OUT" "already exist" \
  || bad "mint stacked a second live row under the same name"
OUT="$(run_key mint --task 'bad%name' --out-dir "$SB/out" 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a task id with a LIKE metacharacter is refused" "$OUT" "outside [A-Za-z0-9._-]" \
  || bad "a wildcard-shaped task id was accepted"
OUT="$(run_key mint --task t_test002 --scope root --out-dir "$SB/out" 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "an unknown scope is refused" "$OUT" "--scope must be" \
  || bad "an unknown scope was accepted"

echo "--- rm (per-card, with a fresh-read post-condition) ---"
# A sibling card's live key must be in the table, so a sweep would be visible.
printf '%s|%s\n' "11111111-2222-3333-4444-555555555555" "deploy-verify-tmp-t_sibling" >>"$STATE"
OUT="$(run_key rm --task t_test001 --key-file "$KEYFILE" --base https://api.example.test 2>&1)"; RC=$?
check "rm exits 0" 0 "$RC"
contains "rm deletes this card's row by exact name" "$OUT" "deleted 1 row(s) named 'deploy-verify-tmp-t_test001'" \
  || bad "rm did not report the exact-name delete"
contains "rm proves the post-condition with a fresh read" "$OUT" "rows named 'deploy-verify-tmp-t_test001' = 0"
contains "rm reports the sibling rows it did NOT touch" "$OUT" "overall: 1"
check "the sibling card's key is untouched in the table" 1 "$(awk -F'|' '$2=="deploy-verify-tmp-t_sibling"' "$STATE" | wc -l | tr -d ' ')"
contains "rm proves the deleted key no longer authenticates (401)" "$OUT" "-> 401 (revoked)"

echo "--- the post-condition must actually bite ---"
# The mock is told to answer the DELETE with a row count while leaving the row in
# place — exactly the shape a wrong WHERE clause or a failed transaction produces.
# If the script's post-condition is weakened or removed, this test goes red.
printf '%s|%s\n' "22222222-3333-4444-5555-666666666666" "deploy-verify-tmp-t_survives" >>"$STATE"
JF_TEST_DELETE_NOOP=1 run_key mint --task t_survives --out-dir "$SB/out" >/dev/null 2>&1
OUT="$(JF_TEST_DELETE_NOOP=1 run_key rm --task t_survives 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a row that survives the delete is a FAILURE, not a quiet success" "$OUT" "SURVIVED the delete" \
  || bad "rm reported success even though the row was still there"
check "the surviving row is still in the table (the mock really did skip the delete)" 1 \
  "$(awk -F'|' '$2=="deploy-verify-tmp-t_survives"' "$STATE" | wc -l | tr -d ' ')"

echo "--- rm's guards ---"
echo 200 >"$SB/curl_code"
# Re-mint so the 200-case has a real row + a real key file to work with.
run_key mint --task t_test003 --out-dir "$SB/out" >/dev/null 2>&1
KEY3="$SB/out/jf-tmpkey-t_test003.raw"
OUT="$(run_key rm --task t_test003 --key-file "$KEY3" --base https://api.example.test 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a key that still answers 200 is reported as NOT revoked" "$OUT" "STILL answers 200" \
  || bad "rm accepted a key that still authenticates"
check "a refused dead-key proof still removed the row (no orphan credential)" 0 \
  "$(grep -cF 'deploy-verify-tmp-t_test003' "$STATE" || true)"
echo 401 >"$SB/curl_code"
OUT="$(run_key rm --task t_test001 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "rm on a missing row is refused (a DELETE 0 is not success)" "$OUT" "nothing was deleted" \
  || bad "rm treated a DELETE 0 as success"
OUT="$(run_key rm --task t_test001 --allow-missing 2>&1)"; RC=$?
[ $RC -eq 0 ] && contains "rm --allow-missing is the explicit opt-in for a re-run" "$OUT" "nothing deleted" \
  || bad "rm --allow-missing should succeed on an already-clean name"
OUT="$(run_key rm --task 'deploy-verify-tmp-%' 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a name with a LIKE metacharacter is refused" "$OUT" "outside [A-Za-z0-9._-]" \
  || bad "rm accepted a wildcard-shaped task id"
OUT="$(run_key rm --task t_sibling --base https://api.example.test 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "the dead-key proof refuses when the key file is absent" "$OUT" "key-file is missing" \
  || bad "the dead-key proof ran without the raw key"
check "the precondition refusal left the sibling row alone" 1 \
  "$(grep -cF 'deploy-verify-tmp-t_sibling' "$STATE" || true)"
# The wrong-shape key file must be refused BEFORE the delete, not after.
printf 'jf_tmp_not-a-real-key' >"$SB/out/wrongshape.raw"
OUT="$(run_key rm --task t_sibling --key-file "$SB/out/wrongshape.raw" --base https://api.example.test 2>&1)"; RC=$?
[ $RC -ne 0 ] && contains "a key file outside the API's shape is refused" "$OUT" "does not hold a key of the API's shape" \
  || bad "a wrongly-shaped key file was accepted"
check "that refusal also left the sibling row alone" 1 \
  "$(grep -cF 'deploy-verify-tmp-t_sibling' "$STATE" || true)"

echo "---"
echo "tmp-apikey self-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] || exit 1
