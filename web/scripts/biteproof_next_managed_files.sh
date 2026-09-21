#!/usr/bin/env bash
# Bite proof for web/src/lib/nextManagedFiles.test.ts.
#
# A guard that only ever passes is a tautology. This mutates each pinned fact
# one at a time, runs the suite, and requires the guard to FAIL — then restores
# the file byte-identically (verified with sha256).
#
# Run:  bash web/scripts/biteproof_next_managed_files.sh
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

CONFIG=next.config.mjs
GITIGNORE=.gitignore
TSCONFIG=tsconfig.json

fail=0
for f in "$CONFIG" "$GITIGNORE" "$TSCONFIG"; do
  cp "$f" "/tmp/biteproof.$(basename "$f").bak"
  sha256sum "$f" | cut -d' ' -f1 > "/tmp/biteproof.$(basename "$f").sha"
done

run_case() {
  local name="$1" expect_line="$2"
  local out
  out=$(npm run test:unit 2>&1)
  if printf '%s' "$out" | grep -q "✖ failing tests"; then
    if printf '%s' "$out" | grep -q "$expect_line"; then
      echo "BITES  $name  (guard failed as required, naming: $expect_line)"
    else
      echo "BITES? $name  (guard failed but not for the expected reason)"
      printf '%s' "$out" | grep -A3 "✖ failing tests" | head -8
      fail=1
    fi
  else
    echo "GREEN  $name  *** MUTATION NOT DETECTED — the guard does not bite ***"
    fail=1
  fi
}

restore() {
  for f in $CONFIG $GITIGNORE $TSCONFIG; do
    cp "/tmp/biteproof.$(basename "$f").bak" "$f"
    want=$(cat "/tmp/biteproof.$(basename "$f").sha")
    got=$(sha256sum "$f" | cut -d' ' -f1)
    if [ "$want" != "$got" ]; then echo "RESTORE MISMATCH $f"; fail=1; fi
  done
  echo "restored byte-identical (sha256 re-checked): $CONFIG $GITIGNORE $TSCONFIG"
}

echo "== case 1: agentRules:false removed from next.config.mjs =="
sed -i 's/^  agentRules: false,$/  \/\/ MUTATION: switch removed/' "$CONFIG"
grep -q "MUTATION: switch removed" "$CONFIG" || { echo "sed did not apply"; exit 2; }
run_case "config switch removed" "must export .agentRules: false"

echo "== case 2: /AGENTS.md + /CLAUDE.md rules dropped from web/.gitignore =="
sed -i '/^\/AGENTS\.md$/d; /^\/CLAUDE\.md$/d' "$GITIGNORE"
run_case "agent-file ignore rules dropped" "no longer ignores /AGENTS.md, /CLAUDE.md"

echo "== case 3: next-env.d.ts removed from tsconfig include =="
sed -i 's/^    "next-env.d.ts",$/    "MUTATED-away.d.ts",/' "$TSCONFIG"
run_case "tsconfig include entry dropped" "must keep .next-env.d.ts. in"

restore
if [ "$fail" -ne 0 ]; then echo "BITE PROOF FAILED"; exit 1; fi
echo "all 3 mutations bite; tree restored"
