#!/usr/bin/env bash
# Bite proof for web/src/lib/nextManagedFiles.test.ts.
#
# A guard that only ever passes is a tautology. This mutates each pinned fact
# one at a time, runs the suite, and requires the guard to FAIL — then restores
# the file byte-identically (verified with sha256).
#
# Cases 1-3 mutate the rule halves (config / .gitignore text / tsconfig).
# Cases 4-5 mutate the TRACKED half, which the rules cannot see:
#   * 4 re-tracks web/next-env.d.ts — the exact one-command regression that used
#     to leave the suite green at 264/0 with `A  web/next-env.d.ts` in
#     `git status --short`. Needs git, so this script REFUSES to run without a
#     repository rather than reporting a partial pass (the Docker build context
#     has no .git; the suite itself skips only that one check there).
#   * 5 negates the /next-env.d.ts rule with a later `!` line — the ignore RULES
#     still list the path, so cases 1-3's text assertions stay green while git
#     no longer ignores the file.
# Case 0 (before any mutation) proves the SKIP is the only case that may not run
# here: the same premise the suite asserts (no .git AND no repo root), so the
# two cannot drift and the skip cannot widen into a checkout that merely lost
# its git metadata.
#
# Run:  bash web/scripts/biteproof_next_managed_files.sh
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

CONFIG=next.config.mjs
GITIGNORE=.gitignore
TSCONFIG=tsconfig.json
# Bare names: this script cds into web/, and a git pathspec is cwd-relative.
MANAGED=(next-env.d.ts AGENTS.md CLAUDE.md)

# Case 0: is this the one environment the suite's tracked check may skip?
#
# The suite skips when git reports no work tree AND the tree looks like the web
# IMAGE build — no `.git` and no repo root. Requiring only the first half is the
# hole this case closes: a checkout that lost its git metadata (a pruned linked
# worktree, a renamed `.git`, a git off PATH) also answers "not a work tree",
# and `api/go.mod` — the old proxy — ships in EVERY checkout, so the skip fired
# there and the tracked half went green over the very tree it guards.
#
# The root entries below are the ones present in every checkout and absent from
# the image: `.dockerignore` lists Makefile/AGENTS.md (lines 29-30) and
# web/Dockerfile never copies the root at all, so the image's /repo is exactly
# {web, api, MCP-PARITY.md}. `.gitignore` is not in `.dockerignore`, which is
# why it is the load-bearing marker — its absence cannot be blamed on an ignore
# rule. Kept deliberately as a plain conjunction, mirroring the suite's
# CHECKOUT_ONLY_ROOT_ENTRIES.
CHECKOUT_ONLY_ROOT_ENTRIES=(.gitignore Makefile AGENTS.md)
looks_like_checkout=0
if [ -e ../.git ]; then looks_like_checkout=1; fi
for e in "${CHECKOUT_ONLY_ROOT_ENTRIES[@]}"; do
  if [ -e "../$e" ]; then looks_like_checkout=1; fi
done

if [ "$(git rev-parse --is-inside-work-tree 2>/dev/null)" != "true" ]; then
  if [ "$looks_like_checkout" -eq 1 ]; then
    echo "REFUSING: git reports no work tree, but this looks like a CHECKOUT, not the image build."
    echo "  web/../.git present: $([ -e ../.git ] && echo yes || echo no)"
    echo "  checkout-only root entries present: $(for e in "${CHECKOUT_ONLY_ROOT_ENTRIES[@]}"; do [ -e "../$e" ] && printf '%s ' "$e"; done)"
    echo "The suite would SKIP the tracked half here and report green over exactly the dirty tree"
    echo "it exists to catch (a pruned linked worktree, a renamed .git, a git off PATH)."
    echo "Restore the repository (git worktree repair) and re-run this script."
    exit 2
  fi
  echo "REFUSING: this is not a git work tree, so cases 4-5 (the tracked half) cannot be proven."
  echo "That is the Docker image build — no .git and no repo root; run this script from a checkout instead."
  exit 2
fi

fail=0
for f in "$CONFIG" "$GITIGNORE" "$TSCONFIG"; do
  cp "$f" "/tmp/biteproof.$(basename "$f").bak"
  sha256sum "$f" | cut -d' ' -f1 > "/tmp/biteproof.$(basename "$f").sha"
done

# The tracked/ignored state the tree must be back in at the end (usually empty).
tracked_before="$(git ls-files -z -- "${MANAGED[@]}" | tr '\0' '\n')"
present_before="no"
[ -f next-env.d.ts ] && present_before="yes"

run_case() {
  local name="$1" expect_line="$2"
  local out rc
  out=$(npm run test:unit 2>&1); rc=$?
  # FIRST: did the suite RUN at all? (card t_05d31dbd)
  #
  # `test:unit` starts with web/scripts/web-deps-check.mjs, which REFUSES in a tree
  # that has no web/node_modules — the normal state of a fresh worktree. When it
  # refuses there is no test result to read, so "no `✖ failing tests`" is the
  # ABSENCE of a signal, not a green run. Reporting that as "MUTATION NOT DETECTED"
  # tells a future worker the guard does not bite, when the truth is that nothing
  # was proven. The exit stays non-zero either way (it fails closed already), so the
  # defect is the DIAGNOSIS: it sends the reader to the guard instead of the install.
  #
  # The marker is `ℹ tests` (node:test's own summary line), present on every real
  # run and absent on a refusal; the deps-check's own message is looked for too, so
  # the REFUSED line can name the missing install rather than guess at it.
  if ! grep -q "ℹ tests" <<<"$out"; then
    echo "REFUSED $name  *** the suite could not run, so this case proves NOTHING ***"
    if grep -qi "node_modules" <<<"$out"; then
      echo "        the web tree is not installed — run: cd web && npm ci"
    else
      echo "        no 'ℹ tests' summary in the output, so no test result exists to judge (rc=$rc)"
    fi
    grep -iE "node_modules|missing|refus" <<<"$out" | sed -n '1,4p' | sed 's/^/        /'
    fail=1
    return
  fi
  if grep -q "✖ failing tests" <<<"$out"; then
    if grep -q "$expect_line" <<<"$out"; then
      echo "BITES  $name  (guard failed as required, naming: $expect_line)"
    else
      echo "BITES? $name  (guard failed but not for the expected reason)"
      grep -A3 "✖ failing tests" <<<"$out" | sed -n '1,8p'
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
restore

echo "== case 2: /AGENTS.md + /CLAUDE.md rules dropped from web/.gitignore =="
sed -i '/^\/AGENTS\.md$/d; /^\/CLAUDE\.md$/d' "$GITIGNORE"
run_case "agent-file ignore rules dropped" "no longer ignores /AGENTS.md, /CLAUDE.md"
restore

echo "== case 3: next-env.d.ts removed from tsconfig include =="
sed -i 's/^    "next-env.d.ts",$/    "MUTATED-away.d.ts",/' "$TSCONFIG"
run_case "tsconfig include entry dropped" "must keep .next-env.d.ts. in"
restore

# --- cases 4-5: the TRACKED / actually-ignored half -------------------------
# `restore` after each case is not decoration: without it the CONFIG mutation
# from case 1 is still on disk when a later case runs, so the guard fails for
# THAT reason and the case reads as "failed but not for the expected reason" —
# the wrong-reason shape this proof exists to make visible.

restore_tracking() {
  git rm --cached -q -- next-env.d.ts 2>/dev/null
  if [ "$present_before" = "no" ]; then rm -f next-env.d.ts; fi
  local now
  now="$(git ls-files -z -- "${MANAGED[@]}" | tr '\0' '\n')"
  if [ "$now" != "$tracked_before" ]; then
    echo "RESTORE MISMATCH tracked set (want [$tracked_before], got [$now])"
    git ls-files -- "${MANAGED[@]}"
    fail=1
  fi
  if grep -q . <<<"$(git ls-files -- next-env.d.ts)"; then
    echo "RESTORE MISMATCH next-env.d.ts is still tracked"
    fail=1
  fi
}

echo "== case 4: web/next-env.d.ts re-tracked (git add -f) =="
if [ "$present_before" = "no" ]; then
  # A fresh checkout has no copy (Next regenerates it); write the generated
  # shape so `git add -f` has something to track, exactly as a re-track would.
  printf '/// <reference types="next" />\n' > next-env.d.ts
fi
git add -f -- next-env.d.ts || { echo "git add -f did not apply"; exit 2; }
if ! grep -q . <<<"$(git ls-files -- next-env.d.ts)"; then echo "the re-track did not apply"; exit 2; fi
git status --short | sed -n '1,5p'
run_case "next-env.d.ts re-tracked" "git TRACKS next-env.d.ts"
restore_tracking

echo "== case 5: /next-env.d.ts rule negated by a later ! line =="
printf '!/next-env.d.ts\n' >> "$GITIGNORE"
grep -c "^!" "$GITIGNORE" | grep -qx 1 || { echo "the negation did not apply"; exit 2; }
run_case "ignore rule negated" "git does not ignore next-env.d.ts"

restore
if grep -q . <<<"$(git ls-files -- "${MANAGED[@]}")"; then
  echo "RESTORE MISMATCH: the repo still tracks ${MANAGED[*]}"
  fail=1
fi

# --- case 6: the SKIP itself, in the shape that used to satisfy it -----------
# The skip's premise is "no .git AND no repo root". This case builds the OTHER
# shape — a checkout that lost its git metadata (.git absent, root entries
# present, web/ and api/ intact, exactly a pruned linked worktree) — and
# requires the guard to FAIL there. Before the fix the same tree was GREEN: the
# skip accepted `api/go.mod` as proof of the image build, and `api/go.mod` ships
# in every checkout, so the tracked half was skipped over precisely the dirty
# tree it exists to catch.
#
# The probe is a throwaway tree OUTSIDE the repo (hardlinked, so it costs no
# real disk), because the shape cannot be produced inside this worktree without
# breaking it: removing web/../.git here would take the whole suite down with it.
# Nothing in this repo is mutated by this case.
PROBE="$(mktemp -d "${TMPDIR:-/tmp}/biteproof-skip.XXXXXX")"
trap 'rm -rf "$PROBE"' EXIT

echo "== case 6: a checkout that lost its git metadata (the shape the skip used to accept) =="
# This script cds into web/, so the repo-root entries are `../<name>`.
cp -al ../web "$PROBE/web" 2>/dev/null || cp -a ../web "$PROBE/web"
for entry in api MCP-PARITY.md .gitignore Makefile AGENTS.md; do
  cp -al "../$entry" "$PROBE/$entry" 2>/dev/null || cp -a "../$entry" "$PROBE/$entry"
done
# The shape under test: a repo root that is clearly a checkout (so the probe
# proves it is not vacuous), with no .git for git to find.
if [ -z "$(ls -A "$PROBE")" ]; then echo "probe tree is empty — the copy did not apply"; exit 2; fi
if [ "$(cd "$PROBE/web" && git rev-parse --is-inside-work-tree 2>/dev/null)" = "true" ]; then
  echo "SKIPPING case 6: the probe somehow landed inside a work tree, so it does not test the skip"
  fail=1
else
  probe_out="$(cd "$PROBE/web" && node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON \
    src/lib/nextManagedFiles.test.ts 2>&1)"
  probe_rc=$?
  if [ "$probe_rc" -eq 0 ]; then
    echo "GREEN  checkout without git metadata  *** THE GUARD SKIPPED THE TRACKED HALF ***"
    grep -m1 "git:" <<<"$probe_out" | sed 's/^/       /'
    fail=1
  elif grep -q "does not look like the image build" <<<"$probe_out"; then
    echo "BITES  checkout without git metadata  (guard failed as required, naming the skip's premise)"
  else
    echo "BITES? checkout without git metadata  (guard failed but not for the expected reason)"
    grep -A3 "✖ failing tests" <<<"$probe_out" | sed -n '1,8p'
    fail=1
  fi
fi
rm -rf "$PROBE"
trap - EXIT

if [ "$fail" -ne 0 ]; then echo "BITE PROOF FAILED"; exit 1; fi
echo "all 6 mutations bite; sources restored byte-identical and the tracked set is back"
