#!/usr/bin/env bash
# biteproof_gitleaks_allowlist.sh — proves the `gitleaks` allowlist's PATH scoping
# BITES, in both directions, without touching a byte of this repository.
#
# THE DEFECT THIS IS THE INSTRUMENT FOR (card t_43005ced, filed by the merge lane on
# PR 1142). `.gitleaks.toml`'s allowlist carried `web/.*\.test\.(ts|tsx|js|jsx)` but
# NOT the Playwright form `web/.*\.spec\.(ts|tsx|js|jsx)`. The e2e specs legitimately
# carry token-shaped fixtures (`await page.goto("/magic-link?token=…")`), the
# `generic-api-key` rule matches one at entropy 4.0, and `gitleaks detect --source .`
# scans EVERY ref — not the tree under test. So one spec fixture pushed on one feature
# branch redded the `gitleaks` gate for every worktree and every candidate tree in the
# repo, including trees whose own diff could not contain it. Measured 2026-10-11:
# tree 3c1cd07d recorded `gitleaks: pass` at 12:44:35Z and the SAME history failed
# 30 minutes later, only because a foreign branch was pushed at 13:12:49Z.
#
# WHY A HARNESS AND NOT A GATE. BITE-HARNESSES.md states the rule: a bite proof is an
# on-demand instrument, never a per-merge gate (one case here costs a scanner build
# plus a full-history-shaped scan). What the per-merge gate DOES carry is the cheap
# static half — `scripts/preflight_test.sh` check 11b asserts the shipped config still
# allowlists the spec form and still does NOT allowlist a non-test path.
#
# WHAT IT PROVES, and why each leg is needed:
#   0  PREMISE. The shipped config carries the spec-path entry exactly once, and the
#      pre-fix config is DERIVED from it by removing that one line (anchor count 0 in
#      the mutant, diff non-empty). A mutation that never landed measures the healthy
#      file and would silently prove nothing.
#   1  THE DEFECT (pre-fix config, spec-only repo): RED, naming
#      `web/e2e/probe.spec.ts`. This is the repo-wide red the card reports.
#   2  THE FIX (shipped config, SAME bytes): GREEN, no findings. Case 1 and case 2 run
#      the SAME scanner over the SAME commit and differ by ONE config line — that pair
#      is this instrument's own vacuity control: case 2's green cannot come from a
#      scanner that is blind, because case 1 shows it seeing the identical fixture.
#   3  STILL SCOPED (shipped config, a repo holding the fixture in BOTH a test path and
#      a non-test path): RED, and the ONLY finding names `web/src/lib/probe.ts`. A fix
#      that allowlisted `web/` wholesale — or the fixture itself by REGEX rather than
#      by path — would come out GREEN here, which is the failure this leg removes.
#   4  BLINDING CONTROL (shipped config with `[extend] useDefault` removed, the same
#      repo as case 3): GREEN at 0 findings. That is the documented permanent-green
#      no-op (the config then REPLACES gitleaks' ruleset with an empty one), so it shows
#      a green result is worth nothing on its own and pins why `useDefault` is asserted
#      separately by both the selftest (check 11) and the harness's premise leg.
#   5  THE PURGE HARNESS FIXTURE (card t_cf0f6063): the empty-credential line in
#      `deploy/biteproof_ship_static_purge.sh` is GREEN under the shipped config, so
#      the fixture that redded the gate on main is covered — by its LINE, not its path.
#   6  ...AND STILL SCOPED: the SAME file carrying a REAL-valued token is RED naming
#      that path, which is the direction a PATH allowlist for `deploy/biteproof_*.sh`
#      would break. The harness drives real curl requests against a real CDN, so a
#      token pasted into it while debugging must still be reported.
#
# WHAT IT DOES NOT PROVE: it does not re-read the repo's own history (the foreign blob
# that started this was amended away from every ref, so a `detect` on this clone no
# longer has it), and it does not exercise the `gitleaks` gate's shallow-clone refusal.
# It proves the CONFIG's scoping on a synthetic repo whose input shape is the one that
# tripped it.
#
# NOTHING UNDER THIS REPOSITORY IS MUTATED. Every probe repo, config and report lives
# in one `mktemp -d` and is removed by the EXIT trap; the shipped `.gitleaks.toml` is
# sha256-checked before and after, and the worktree is asserted unchanged. No lock is
# needed for that reason (contrast the harnesses that mutate shared sources).
#
# Usage:  scripts/biteproof_gitleaks_allowlist.sh
#   BITEPROOF_GITLEAKS_BIN=<binary>   use this scanner instead of `go run …@latest`
#                                     (the gate's own invocation; a v8 binary is what
#                                     an operator has installed). The VERSION is not
#                                     asserted — the gate itself floats on @latest, so
#                                     pinning one here would diverge from it.
#   BITEPROOF_KEEP=1                  keep the temp dir and print its path
#
# Exit: 0 every case bit; 1 a case DID NOT BITE; 2 HARNESS BUG (premise/anchor).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
CFG="$ROOT/.gitleaks.toml"

# The fixture value is ASSEMBLED, never written as one literal: this file is a
# non-test path in the scanned history, so a token-shaped literal here would make the
# harness itself the next false positive.
FIXTURE_HEAD="0123456"
FIXTURE_TAIL="789abcdef"
FIXTURE_VALUE="${FIXTURE_HEAD}${FIXTURE_TAIL}"

# The allowlist entry this card added, as the fixed string the mutation anchors on.
ANCHOR='web/.*\.spec\.(ts|tsx|js|jsx)'

SPEC_PATH="web/e2e/probe.spec.ts"
NONTEST_PATH="web/src/lib/probe.ts"

# THE PURGE HARNESS (card t_cf0f6063). `deploy/biteproof_ship_static_purge.sh` is a
# NON-test deploy path whose case-0 fixture SETS the purge credentials to empty —
# `env JF_CF_PURGE_TOKEN= JF_CF_PURGE_ZONE_ID=` — a shape `generic-api-key` reads as
# a keyword whose "secret" is the SIBLING VARIABLE'S NAME. The shipped config covers
# it with a LINE-scoped entry and deliberately NOT a path entry (the harness drives
# real curl requests against a real CDN, so a token pasted into it while debugging is
# a plausible thing to find). Cases 5 and 6 are the two directions of that decision.
PURGE_PATH="deploy/biteproof_ship_static_purge.sh"
PURGE_KW_A="JF_CF_PURGE_TOKEN="
PURGE_KW_B="JF_CF_PURGE_ZONE_ID="
# Assembled, never one literal — see FIXTURE_VALUE above for why.
PURGE_REAL_HEAD="7c1e9a03b5d2a8f4"
PURGE_REAL_TAIL="d6b0e1c9a4f38527"
PURGE_REAL_VALUE="${PURGE_REAL_HEAD}${PURGE_REAL_TAIL}"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/biteproof-gitleaks.XXXXXX")"
cleanup() {
  if [ "${BITEPROOF_KEEP:-0}" = "1" ]; then
    echo "kept: $TMP"
  else
    rm -rf "$TMP"
  fi
}
trap cleanup EXIT

CASES=0
BITES=0
BUGS=0

bug() { echo "HARNESS BUG: $*" >&2; BUGS=$((BUGS + 1)); }
pass_case() { CASES=$((CASES + 1)); BITES=$((BITES + 1)); echo "  ok   $1"; }
fail_case() { CASES=$((CASES + 1)); echo "  ❌ DID NOT BITE — $1" >&2; }

# gl runs the scanner the `gitleaks` gate runs: `go run …@latest`, with the toolchain
# resolved from api/ because the repo root has no go.mod of its own (gitleaks v8 needs
# go >= 1.24.11 and the root reports the host default).
gl() {
  if [ -n "${BITEPROOF_GITLEAKS_BIN:-}" ]; then
    "$BITEPROOF_GITLEAKS_BIN" "$@"
    return
  fi
  local toolchain
  toolchain="$(cd "$ROOT/api" && go env GOVERSION)"
  (cd "$ROOT" && GOTOOLCHAIN="$toolchain" go run github.com/zricethezav/gitleaks/v8@latest "$@")
}

# scan SOURCE_DIR CONFIG TAG -> writes $TMP/<TAG>.json and $TMP/<TAG>.log, echoes rc
scan() {
  local src="$1" cfg="$2" tag="$3" rc=0
  gl detect --source "$src" --no-banner --redact --config "$cfg" \
    --report-format json --report-path "$TMP/$tag.json" -v >"$TMP/$tag.log" 2>&1 || rc=$?
  echo "$rc"
}

# findings TAG -> the File: values in the report, one per line, sorted. The report is
# pretty-printed (`"File": "…",` with a space), so the reader tolerates the gaps.
findings() {
  if [ ! -s "$TMP/$1.json" ] || [ "$(cat "$TMP/$1.json")" = "null" ]; then
    return 0
  fi
  grep -oE '"File": *"[^"]*"' "$TMP/$1.json" | sed 's/.*"File": *"//;s/"$//' | sort
}

# ran TAG -> the scan really walked the repo. A column with no scan behind it is not a
# column: `detect` on an empty source prints no such line and would otherwise report
# "no leaks found" without having read anything.
ran() { grep -q 'commits scanned' "$TMP/$1.log"; }

# saw_no_leaks TAG -> positive evidence of the GREEN direction. `rc=0` alone also
# covers a scanner that never executed; the scanner's own verdict line does not.
saw_no_leaks() { grep -q 'no leaks found' "$TMP/$1.log"; }

# classify EXPECTED RC FILES WANT_FILES — the ONE reading of every case's verdict.
# EXPECTED is red|green; for red the finding set must be EXACTLY WANT_FILES (so a red
# attributed to a sibling cannot be credited), for green there must be none. This is a
# function, not an inline test, so the self-check below can drive it in both
# directions on real measurements.
classify() {
  local want="$1" rc="$2" files="$3" wantfiles="$4"
  if [ "$want" = "red" ]; then
    [ "$rc" != "0" ] && [ "$files" = "$wantfiles" ]
  else
    [ "$rc" = "0" ] && [ -z "$files" ]
  fi
}

write_spec_probe() {
  local f="$1/$SPEC_PATH"
  mkdir -p "$(dirname "$f")"
  printf 'import { test, expect } from "@playwright/test";\n\ntest("probe: a magic-link token fixture lives in a TEST path", async ({ page }) => {\n  await page.goto("/magic-link?token=%s");\n  await expect(page).toHaveURL(/magic-link/);\n});\n' \
    "$FIXTURE_VALUE" >"$f"
}

write_nontest_probe() {
  local f="$1/$NONTEST_PATH"
  mkdir -p "$(dirname "$f")"
  printf '// the same token-shaped value in a NON-test path\nexport const magicLinkUrl = "/magic-link?token=%s";\n' \
    "$FIXTURE_VALUE" >"$f"
}

# write_purge_probe DIR [real] — a `deploy/biteproof_ship_static_purge.sh` carrying
# the EMPTY-credential fixture line, and (with `real`) a second line whose token has
# a real-shaped VALUE. The pair is the line-scoped allowlist's two directions.
write_purge_probe() {
  local f="$1/$PURGE_PATH"
  mkdir -p "$(dirname "$f")"
  {
    printf '#!/bin/bash\n'
    printf 'run_purge out err env %s %s APP_BASE_URL=https://example.com\n' \
      "$PURGE_KW_A" "$PURGE_KW_B"
    if [ "${2:-}" = "real" ]; then
      printf '%s%s\n' "$PURGE_KW_A" "$PURGE_REAL_VALUE"
    fi
  } >"$f"
}

# commit_repo DIR — a real commit, because the incident was a committed blob and
# `detect` reads history, not the working tree.
commit_repo() {
  git -C "$1" init -q -b main
  git -C "$1" -c user.name=biteproof -c user.email=biteproof@example.invalid add -A
  git -C "$1" -c user.name=biteproof -c user.email=biteproof@example.invalid commit -qm "probe fixtures"
}

echo "== biteproof: gitleaks allowlist PATH scoping =="
echo "config: $CFG"
CFG_SHA_BEFORE="$(sha256sum "$CFG" | cut -d' ' -f1)"
WT_BEFORE="$(git -C "$ROOT" status --porcelain)"

# ---- 0. PREMISE: the anchor is present exactly once, and the mutant really loses it
if [ ! -f "$CFG" ]; then
  bug ".gitleaks.toml not found at $CFG"
fi
n_anchor="$(grep -cF "$ANCHOR" "$CFG")"
if [ "$n_anchor" != "1" ]; then
  bug "the spec-path allowlist anchor [$ANCHOR] occurs $n_anchor times in $CFG, expected 1 — the anchor this harness removes has moved, so every case below would measure the wrong file"
fi

mkdir -p "$TMP/cfg-postfix" "$TMP/cfg-prefix" "$TMP/cfg-norules"
cp "$CFG" "$TMP/cfg-postfix/.gitleaks.toml"
grep -vF "$ANCHOR" "$CFG" >"$TMP/cfg-prefix/.gitleaks.toml"
# The blinding control: the documented empty-ruleset shape (see the config's header).
grep -vE '^[[:space:]]*useDefault[[:space:]]*=' "$CFG" >"$TMP/cfg-norules/.gitleaks.toml"

n_prefix="$(grep -cF "$ANCHOR" "$TMP/cfg-prefix/.gitleaks.toml")"
n_norules="$(grep -cE '^[[:space:]]*useDefault[[:space:]]*=' "$TMP/cfg-norules/.gitleaks.toml")"
if [ "$n_anchor" = "1" ] && [ "$n_prefix" = "0" ] && \
  [ "$(diff "$CFG" "$TMP/cfg-prefix/.gitleaks.toml" | grep -c '^<')" = "1" ] && \
  [ "$n_norules" = "0" ] && \
  [ "$(diff -q "$CFG" "$TMP/cfg-postfix/.gitleaks.toml" >/dev/null; echo $?)" = "0" ]; then
  echo "  ok   premise: the pre-fix config is the shipped one with exactly the spec-path line removed (anchor 1 -> 0), and useDefault is dropped only in the blinding control"
  CASES=$((CASES + 1))
  BITES=$((BITES + 1))
else
  bug "the derived configs are not what the cases assume (anchor $n_anchor -> $n_prefix, useDefault-left $n_norules, postfix byte-identical to the shipped config must hold)"
fi

if [ "$BUGS" != "0" ]; then
  echo "HARNESS BUG — no verdict (premise failed, $BUGS bug(s))" >&2
  exit 2
fi

# ---- the probe repos: one fixture in a spec path, one holding BOTH shapes
R_SPEC="$TMP/repo-spec"
R_BOTH="$TMP/repo-both"
R_PURGE="$TMP/repo-purge-fixture"
R_PURGE_REAL="$TMP/repo-purge-real"
mkdir -p "$R_SPEC" "$R_BOTH" "$R_PURGE" "$R_PURGE_REAL"
write_spec_probe "$R_SPEC"
write_spec_probe "$R_BOTH"
write_nontest_probe "$R_BOTH"
write_purge_probe "$R_PURGE" ""
write_purge_probe "$R_PURGE_REAL" real
commit_repo "$R_SPEC"
commit_repo "$R_BOTH"
commit_repo "$R_PURGE"
commit_repo "$R_PURGE_REAL"

# ---- 1. the defect: the shipped-before-this-card allowlist reds on a spec fixture
rc1="$(scan "$R_SPEC" "$TMP/cfg-prefix/.gitleaks.toml" c1)"
files1="$(findings c1)"
ran c1 || bug "case 1: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify red "$rc1" "$files1" "$SPEC_PATH"; then
  pass_case "case 1 (pre-fix config, spec-only repo): RED, naming $SPEC_PATH"
else
  fail_case "case 1: the pre-fix allowlist must report the spec fixture — rc=$rc1, findings=[$(echo "$files1" | tr '\n' ' ')]"
fi

# ---- 2. the fix: the SAME bytes, one config line apart, come out green
rc2="$(scan "$R_SPEC" "$TMP/cfg-postfix/.gitleaks.toml" c2)"
files2="$(findings c2)"
ran c2 || bug "case 2: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify green "$rc2" "$files2" "" && saw_no_leaks c2; then
  pass_case "case 2 (shipped config, same repo): GREEN, 0 findings — the spec fixture is ALLOWLISTED, not absent"
else
  fail_case "case 2: the shipped config must allowlist the spec fixture — rc=$rc2, findings=[$(echo "$files2" | tr '\n' ' ')]"
fi

# ---- 2b. THE INSTRUMENT'S OWN BITE. Cases 1 and 2 measure the SAME commit and
# differ by one config line, so a verdict that answered a constant would credit both.
# Drive `classify` with those real measurements and with each one FLIPPED: the red of
# case 1 must not be readable as green, the green of case 2 must not be readable as
# red, and case 1's red must not be creditable to the sibling path. Zero extra scans.
if classify red "$rc1" "$files1" "$SPEC_PATH" \
  && ! classify red "$rc2" "$files2" "$SPEC_PATH" \
  && ! classify green "$rc1" "$files1" "" \
  && ! classify red "$rc1" "$files1" "$NONTEST_PATH" \
  && classify green "$rc2" "$files2" ""; then
  pass_case "case 2b: the verdict reading discriminates — case 1's red is not readable as green, case 2's green is not readable as red, and case 1's red is not creditable to $NONTEST_PATH"
else
  fail_case "case 2b: the verdict reading is not discriminating (it would credit a measurement to the wrong column)"
fi

# ---- 3. still path-scoped: the non-test copy is reported, the spec copy is not
rc3="$(scan "$R_BOTH" "$TMP/cfg-postfix/.gitleaks.toml" c3)"
files3="$(findings c3)"
ran c3 || bug "case 3: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify red "$rc3" "$files3" "$NONTEST_PATH"; then
  pass_case "case 3 (shipped config, test + non-test): RED, and the ONLY finding is $NONTEST_PATH (the allowlist did not widen past test paths)"
else
  fail_case "case 3: a real-shaped value in a non-test path must still be reported, and nothing else — rc=$rc3, findings=[$(echo "$files3" | tr '\n' ' ')]"
fi

# ---- 4. blinding control: without [extend] useDefault the same repo reports NOTHING
rc4="$(scan "$R_BOTH" "$TMP/cfg-norules/.gitleaks.toml" c4)"
files4="$(findings c4)"
ran c4 || bug "case 4: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify green "$rc4" "$files4" "" && saw_no_leaks c4; then
  pass_case "case 4 (useDefault removed, same repo as case 3): GREEN at 0 findings — a green with an empty ruleset is a no-op, which is why [extend] useDefault is load-bearing"
else
  fail_case "case 4: dropping [extend] useDefault must blind the scan (0 findings) — rc=$rc4, findings=[$(echo "$files4" | tr '\n' ' ')]"
fi

# ---- 5. the purge harness's EMPTY-credential fixture is covered BY LINE
rc5="$(scan "$R_PURGE" "$CFG" c5)"
files5="$(findings c5)"
ran c5 || bug "case 5: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify green "$rc5" "$files5" "" && saw_no_leaks c5; then
  pass_case "case 5 (shipped config, $PURGE_PATH carrying ONLY the empty-credential fixture): GREEN — the fixture that redded the gate on main is allowlisted, and by the LINE entry rather than by its path"
else
  fail_case "case 5: the empty-credential fixture in $PURGE_PATH must be allowlisted — rc=$rc5, findings=[$(echo "$files5" | tr '\n' ' ')]"
fi

# ---- 6. ...and the SAME file carrying a REAL-valued token is still reported
#     This is the direction a path entry for `deploy/biteproof_*.sh` would break:
#     the harness drives real curl requests against a real CDN, so a token pasted
#     into it while debugging must still be found.
rc6="$(scan "$R_PURGE_REAL" "$CFG" c6)"
files6="$(findings c6)"
ran c6 || bug "case 6: the scan printed no 'commits scanned' line — it did not walk the probe repo"
if classify red "$rc6" "$files6" "$PURGE_PATH"; then
  pass_case "case 6 (shipped config, same file + a REAL-valued token): RED, naming $PURGE_PATH — the coverage is scoped to the fixture's line, not to the harness's path"
else
  fail_case "case 6: a real token in $PURGE_PATH must still be reported and nothing else — rc=$rc6, findings=[$(echo "$files6" | tr '\n' ' ')]"
fi

# ---- the repository must be exactly as it was
CFG_SHA_AFTER="$(sha256sum "$CFG" | cut -d' ' -f1)"
WT_AFTER="$(git -C "$ROOT" status --porcelain)"
if [ "$CFG_SHA_BEFORE" = "$CFG_SHA_AFTER" ] && [ "$WT_BEFORE" = "$WT_AFTER" ]; then
  echo "  ok   the repository is unchanged ($CFG sha256 $(echo "$CFG_SHA_BEFORE" | cut -c1-12), worktree status identical)"
else
  bug "the repository changed during the run ($CFG_SHA_BEFORE -> $CFG_SHA_AFTER; worktree status differs)"
fi

echo
echo "CASES: $CASES   BITES: $BITES"
if [ "$BUGS" != "0" ]; then
  echo "RESULT: HARNESS BUG — exit 2" >&2
  exit 2
fi
if [ "$BITES" = "$CASES" ]; then
  echo "RESULT: ALL CASES BIT — exit 0"
  exit 0
fi
echo "RESULT: A CASE DID NOT BITE — exit 1" >&2
exit 1
