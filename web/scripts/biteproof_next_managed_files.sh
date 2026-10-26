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

# The script's OWN absolute path is resolved BEFORE any `cd`, because the child
# this file re-execs under the lock needs a path that survives a changed CWD.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"

# The shared runner gate and verdict rule (web/scripts/lib/node_test_verdict.sh): every
# verdict this harness prints comes from POSITIVE runner evidence, and a node that cannot
# run the guard is a REFUSAL that names itself — never an answer (card t_b014807f).
. "$BITE_HOME/lib/node_test_verdict.sh" || {
  echo "BITEPROOF REFUSED: cannot source $BITE_HOME/lib/node_test_verdict.sh, so there is" >&2
  echo "no verdict rule to read a guard's output with." >&2
  exit 2
}

# ---- PER-WORKTREE LOCK (card t_df41e675) ----------------------------------
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate and restore src/components/SaveButton.tsx," >&2
    echo "src/app/settings/page.tsx (shared with biteproof_settings_tabs.sh) and" >&2
    echo "src/components/i18nDict.ts under each other's feet, which prints false verdicts" >&2
    echo "(t_df41e675, reproduced: 9 of 12 rounds). Install flock before trusting any" >&2
    echo "verdict from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised pair of runs prints false verdicts (t_df41e675)." >&2
    exit 2
  }
  BITE_KEY=$(printf '%s' "$BITE_GIT_DIR" | sha256sum | cut -c1-16)
  LOCK_DIR="$LOCK_BASE/jomform-biteproof-docs-bite"
  mkdir -p "$LOCK_DIR" || {
    echo "BITEPROOF REFUSED: cannot create the lock directory $LOCK_DIR, so two runs in" >&2
    echo "one worktree could not be serialised. Set BITEPROOF_LOCK_DIR to a writable dir." >&2
    exit 2
  }
  LOCK_FILE="$LOCK_DIR/$BITE_KEY.lock"
  export BITEPROOF_LOCKED=1 BITEPROOF_LOCK_FILE="$LOCK_FILE"
  exec 9>"$LOCK_FILE" || {
    echo "BITEPROOF REFUSED: cannot open the lock file $LOCK_FILE." >&2
    exit 2
  }
  if ! flock -n "$LOCK_FILE" true 2>/dev/null; then
    echo "  lock:   another bite harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (they mutate the same shared sources, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer mutating the same shared sources and" >&2
    echo "would either read its mutation or overwrite a source with its own backup." >&2
    echo "Nothing was mutated. Re-run once the other run finishes, or raise the wait with" >&2
    echo "BITEPROOF_LOCK_WAIT=<seconds>." >&2
    exit 75
  fi
  if [ "$_lock_rc" != 0 ]; then
    echo "BITEPROOF REFUSED: flock on $LOCK_FILE exited ${_lock_rc}, so the lock was not held" >&2
    echo "and this run would not be serialised against a peer in the same worktree." >&2
    exit 2
  fi
  bash "$SELF" "$@"
  exit "$?"
fi
# The child. Refuse if the lock it is supposed to have inherited cannot be shown to
# be held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same shared sources would give it false verdicts" >&2
  echo "and could be left restoring this run's bytes. Unset BITEPROOF_LOCKED and let the" >&2
  echo "harness take the lock itself." >&2
  exit 2
fi
LOCK_PROBE=$(mktemp "$(dirname "$LOCK_FILE")/probe.XXXXXX") || {
  echo "BITEPROOF REFUSED: cannot create a probe file beside $LOCK_FILE, so the check above" >&2
  echo "cannot be shown to be meaningful." >&2
  exit 2
}
if ! flock -n "$LOCK_PROBE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: flock could not lock a FRESH file ($LOCK_PROBE), so \"the lock is held\"" >&2
  echo "above proves nothing about flock here — it would read REFUSED for any file." >&2
  rm -f "$LOCK_PROBE"
  exit 2
fi
rm -f "$LOCK_PROBE"
echo "lock:   held for this worktree (${LOCK_FILE##*/})"

# CAPABILITY, not presence (card t_b014807f): `node --version` succeeding proves nothing
# here — v18.19.1 answers it and then rejects `--disable-warning`, and every leg below
# would read that empty capture as a verdict. The gate runs the invocation this harness
# depends on and refuses, naming the node, when that produces no test-runner summary.
nt_require_node

cd "$(dirname "$0")/.." || exit 2

CONFIG=${BITEPROOF_CONFIG:-next.config.mjs}
GITIGNORE=${BITEPROOF_GITIGNORE:-.gitignore}
TSCONFIG=${BITEPROOF_TSCONFIG:-tsconfig.json}
# Bare names: this script cds into web/, and a git pathspec is cwd-relative.
MANAGED=(next-env.d.ts AGENTS.md CLAUDE.md)

# PER-RUN, and created HERE — in the child, after the lock — not on the parent path
# before it (card t_4a599d51). On the parent path the `mktemp -d` was never
# referenced again: the parent took the lock, re-exec'd the child (which created,
# used and removed its OWN scratch dir) and exited, so EVERY invocation leaked one
# directory into $TMPDIR — and a concurrency battery watching for a dirty tree read
# that leftover as corruption. Every other locked harness of the family already
# creates its per-run temp on the child path; this is the same shape.
TMP="$(mktemp -d)" || { echo "cannot create a per-run scratch dir" >&2; exit 2; }
# The trap goes on IMMEDIATELY, not at the case-6 block far below, because this
# child has several refusals between here and there (the git-work-tree check, the
# missing-config check) that each `exit 2` — and every one of them would leak $TMP
# with the trap not yet installed. `trap - EXIT` at the end still disarms it, but
# only AFTER the explicit `rm -rf "$TMP"` on the success path.
trap 'rm -rf "$TMP"' EXIT

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

for f in "$CONFIG" "$GITIGNORE" "$TSCONFIG"; do
  [ -f "$f" ] || { echo "BITEPROOF REFUSED: $f is missing — nothing to mutate." >&2; exit 2; }
done

fail=0
for f in "$CONFIG" "$GITIGNORE" "$TSCONFIG"; do
  cp "$f" "$TMP/$(basename "$f").bak"
  sha256sum "$f" | cut -d' ' -f1 > "$TMP/$(basename "$f").sha"
done

# The tracked/ignored state the tree must be back in at the end (usually empty).
tracked_before="$(git ls-files -z -- "${MANAGED[@]}" | tr '\0' '\n')"
present_before="no"
[ -f next-env.d.ts ] && present_before="yes"

run_case() {
  local name="$1" expect_line="$2"
  local out rc
  out=$(env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR npm run test:unit 2>&1); rc=$?
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
    cp "$TMP/$(basename "$f").bak" "$f"
    want=$(cat "$TMP/$(basename "$f").sha")
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
# Capture the count, then test — not `grep -c "^!" "$GITIGNORE" | grep -qx 1`
# (card t_39b04c34). This file runs under `set -uo pipefail`; the reader `grep -qx`
# exits at its first match while the writer `grep -c` may still be reading the file,
# so the pipeline can report 141 although the count IS 1 and the case would abort with
# "the negation did not apply" on a negation that DID apply. Measured on this host on a
# realistic 404-line .gitignore: 0/3000, because GNU grep writes a two-byte "1\n" in a
# single write() that lands before the reader can exit — but the shape is the same one
# this card fixes at the sites where the writer is multi-write, and capturing first is
# free and provably correct.
neg_count="$(grep -c "^!" "$GITIGNORE")"
[ "$neg_count" -eq 1 ] || { echo "the negation did not apply"; exit 2; }
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
trap 'rm -rf "$PROBE" "$TMP"' EXIT

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
  probe_out="$(cd "$PROBE/web" && env -u BITEPROOF_LOCKED -u BITEPROOF_LOCK_FILE -u BITEPROOF_LOCK_DIR node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON \
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
# `trap - EXIT` cancels EVERY trap, so the per-run TMP is removed here
# explicitly — leaving it to a trap that is then disarmed left the directory
# behind on every run (caught by a concurrency battery reading it as a dirty tree).
rm -rf "$TMP"
trap - EXIT

if [ "$fail" -ne 0 ]; then echo "BITE PROOF FAILED"; exit 1; fi
echo "all 6 mutations bite; sources restored byte-identical and the tracked set is back"
