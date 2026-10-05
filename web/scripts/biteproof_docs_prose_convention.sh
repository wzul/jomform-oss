#!/usr/bin/env bash
#
# web/scripts/biteproof_docs_prose_convention.sh — the bite proof for
# web/src/lib/docsProseConvention.test.ts (card t_0447a70c).
#
# WHY THIS EXISTS
#   The guard asserts that no /docs prose carries a backtick marker, because every
#   /docs guide renders its section bodies as plain text nodes and the markers
#   therefore reach the reader. A guard like that has one failure mode that a green
#   gate cannot see: a walk that collects nothing (a wrong anchor, a parser that
#   stops at the module scope, a walker pointed at the wrong directory) satisfies
#   "no string carries a backtick" without looking at a single body. So this proves
#   the guard GOES RED when a marker is present, in TWO pages, and stays GREEN on
#   the shapes that must not be reported — a backtick in a COMMENT (a text scanner
#   would flag it; the AST does not), a JSX TEMPLATE LITERAL showing code, and a
#   template literal INSIDE <code>/<pre> even when its text carries a backtick.
#
#   CARD t_8580357e WIDENED THE GUARD past `ts.StringLiteral`-only, and this
#   harness now drives the two shapes that widening added, each of which the
#   string-literal-only walk let through GREEN:
#     * CASE 7 — a marker in RAW JSX TEXT (a `ts.JsxText`), e.g. <p>… `m` …</p>.
#     * CASE 8 — a marker ESCAPED inside a TEMPLATE LITERAL, which reaches the
#       reader as a backtick because `node.text` decodes `\``.
#   Both must be RED, and they must be RED *for the widened reason*: each case
#   asserts node's own count, one failing test (the findings rule) and zero on the
#   vacuity test, so a guard that only ever widened the FINDINGS walk (and left
#   the vacuity walk string-only) is caught by CASE 7/8 too. CASE 9 is the
#   negative control that keeps the widening honest: the SAME escaped-backtick
#   template literal inside a <code> element stays GREEN, because that is the
#   supported way to show code on /docs.
#
#   CARD t_8b3a508b WIDENED THE GUARD A FOURTH TIME — to a template literal WITH a
#   substitution (`ts.TemplateExpression`), the shape t_8580357e's widening could
#   not see (it is neither a NoSubstitutionTemplateLiteral nor a string literal):
#     * CASE 12 — a marker in the STATIC parts of a `${…}`-bearing template
#       literal (`` (x) => `prose ${x} with \`m\` inside` ``), which reaches the
#       reader exactly as CASE 8's shape does, must be RED naming the page with
#       the kind `[template expression]` and one failing test (the findings rule).
#     * CASE 13 — the negative control: the SAME escaped marker inside a <code>
#       element stays GREEN, because the code-sample exclusion covers both
#       template-literal shapes (a sample that shows code with a substitution is
#       still the supported shape).
#
#   CARD t_763c28c2 ADDED THE SECOND SURFACE — prose IMPORTED from a generated
#   module and printed with a JSX expression, which the DOCS_DIR walk cannot reach:
#     * CASE 10 — a marker in web/src/lib/apiExamples.gen.ts (the /docs/api worked
#       examples' notes, generated from api/internal/httpapi/openapi_examples.go),
#       which must be RED naming that file.
#     * CASE 11 — the negative control: a backtick inside `curl`, which the page
#       renders inside <code>, must stay GREEN, so the new surface does not flag
#       every code sample.
#
#   CARD t_8f684cfe CLOSED THE TWO FIELDS THE t_763c28c2 LEG MISSED — the page
#   prints two more imported strings as PLAIN JSX text, and the leg read only
#   {e.why}/{e.note} and {e.summary}/{e.description}, so a marker in either was
#   invisible to it (measured on the t_763c28c2 head: a marker in an example title
#   left the suite at 4 pass / 0 fail):
#     * CASE 14 — a marker in an example TITLE ({e.title}, the heading at
#       api/page.tsx:139), which must be RED naming apiExamples.gen.ts.
#     * CASE 15 — a marker in an endpoint's THROTTLE policy ({e.rateLimit}, the
#       x-jomform-rate-limit.policy the page prints at :388 and in the throttled
#       list at :291), which must be RED naming openapiSpec.gen.ts.
#     * CASE 16 — the negative control: a backtick in `path`, which the page
#       renders inside <code> like `curl`, must stay GREEN (so the surface does not
#       flag the other code samples either).
#
#   CARD t_cd81632d ADDED THE THIRD SURFACE — the /docs index's i18n CHROME LABELS,
#   which are dictionary VALUES outside src/app/docs printed through the `T` island
#   (src/app/docs/docsChrome.tsx: `const { t } = useI18n(); return <>{t(k)}</>` — a
#   plain text node, no markdown renderer), so they reach the reader exactly as a
#   body string does. The values live in src/components/i18nDict.ts, which the
#   DOCS_DIR walk cannot reach and the imported leg does not read:
#     * CASE 17 — a marker in a RENDERED `docs.*` value (docs.catSecurity, a key
#       the /docs index prints via `{g.labelKey}`), which must be RED naming
#       src/components/i18nDict.ts, with the label leg's own vacuity bound green.
#     * CASE 18 — the negative control: a marker in a `docs.*` value whose key NO
#       /docs page renders must stay GREEN, so the leg judges the render site rather
#       than every `docs.*` string in the dictionary.
#
#   CARD t_62da3451 CLOSED ONE MORE IMPORTED STRING the t_8f684cfe leg still missed
#   — each endpoint GROUP's HEADING (`{group.tag}` in an <h3>, api/page.tsx:368, NOT
#   inside <code>). `group` comes from endpointsByTag(), whose tag is the contract's
#   own `tags[]` in openapiSpec.gen.ts, so a marker in a tag reached the reader while
#   the leg reported GREEN: measured on the #984 head (5e3b8d58, tree daa6906),
#   editing the FIRST `"Profile"` tag left the suite at 4 pass / 0 fail:
#     * CASE 19 — a marker in an endpoint group's TAG must be RED naming
#       openapiSpec.gen.ts `[{group.tag}]`, one imported-findings failure. No
#       negative control beside it: unlike curl / path, the tag has NO <code>-
#       rendered render site (the only other use is `key={group.tag}`, an attribute),
#       so a control would assert a shape the page does not have.
#
# WHAT IT DOES
#   It snapshots each file it mutates into a scratch dir, applies one mutation,
#   runs the REAL guard, requires the verdict this case predicts, and restores the
#   file from its snapshot (a cmp check, not a guess — and deliberately NOT
#   `git checkout`, which would revert the other files in this branch's own diff).
#
# THE SHARED INPUT IS SERIALISED PER WORKTREE (card t_f4f4681c). Every case below
#   mutates the SAME shared files under `src/app/docs` (`chat/page.tsx`,
#   `security/page.tsx`, `sales/page.tsx`, `layout.tsx`) in place and `run_guard`
#   reads the WHOLE tree, but this harness took NO lock — while its two siblings
#   of the same shape (`biteproof_docs_tool_claims.sh`, card t_ed66094c, and
#   `biteproof_docs_index_render_sites.sh`, card t_03a646e2) each carry one for
#   exactly this. Reproduced in ONE worktree: two runs started together printed a
#   FALSE `6/6 HOLE cases RED, 0/3 CONTROLS GREEN` — the three CONTROLS read RED
#   because a peer's marker was on disk when `run_guard` read the tree — while
#   both exit codes stayed 1 (no false PASS is reachable; the harm is the wrong
#   verdict LINES), and the collision left two probe mutations ON DISK, so every
#   later `npm run test:unit` in that tree judged a mutated tree until they were
#   restored by hand. A solo run on the same bytes prints `6/6 HOLE cases RED,
#   3/3 CONTROLS GREEN` / `ALL CASES OK` — the counts the harness stood at when
#   this lock landed; on the tree this lock now sits on (cards `t_8b3a508b` and
#   `t_763c28c2` added CASE 10..13) it is EIGHT HOLE / FIVE CONTROLS over the
#   fourteen labelled cases `CASE 0`..`CASE 13`, and the `BITES:` line below
#   prints `8/8 HOLE cases RED, 5/5 CONTROLS GREEN`.
#
#   It now takes the family's per-worktree `flock`, by the same mechanism: keyed
#   by `git rev-parse --absolute-git-dir` (one value per worktree, stable from any
#   CWD, different for the primary tree and each linked worktree — `--show-toplevel`
#   is resolved against the CWD and would not be mutual), under a HOST-GLOBAL `/tmp`
#   base (never $TMPDIR: the run it must exclude usually belongs to another PROFILE,
#   which is how this class of defect was found). The PARENT takes it on fd 9 and
#   re-execs this script; the child inherits the descriptor and PROVES it holds it
#   before printing anything; a contended run WAITS and says so rather than failing;
#   and a wait-out is exit 75 rather than a red harness. It is held from before the
#   FIRST read — CASE 0's control reads the unmutated tree, so a peer's mutation
#   would red it (the shape rounds 2-3 of the reproduction showed).
#
#   THE KEY IS THE FAMILY'S, NOT THIS SCRIPT'S (card t_76b3356d). The lock directory
#   was once per harness, which serialised two runs of the SAME script and left two
#   DIFFERENT harnesses of the family overlapping — and they all mutate the SAME tree.
#   Measured 3/3 rounds with THIS harness and `biteproof_docs_tool_claims.sh` started
#   together in ONE worktree: tool_claims printed 6 false `***` lines against 4 for a
#   solo run, including a FALSE `registered name: NEW guard wanted GREEN, got RED` on
#   its OWN negative control. Every harness of the family now takes ONE file,
#   `jomform-biteproof-docs-bite`, so any two of them serialise; the two that had no
#   lock at all (comment_anchor, mcp_tool_inventory) were given the same one for the
#   same reason. `web/src/lib/harnessLock.test.ts` leg E drives that cross-harness
#   property, so a revert to a per-script directory turns it RED.
#
# NOT A GATE. It is on demand, run by the author of a change that touches the guard
#   or the docs prose it reads. See BITE-HARNESSES.md.
#
# Usage:  bash web/scripts/biteproof_docs_prose_convention.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD — a
# relative `$0` resolved after the `cd` names a file that is not there.
BITE_HOME=$(cd "$(dirname "$0")" 2>/dev/null && pwd)
[ -n "$BITE_HOME" ] || { echo "cannot resolve the directory this script lives in" >&2; exit 2; }
SELF="$BITE_HOME/$(basename "$0")"
cd "$BITE_HOME/.." || exit 2                # -> web/

# ---- PER-WORKTREE LOCK (card t_f4f4681c) ----------------------------------
#
# WHY (the bug this was written for): every case below mutates the SHARED files
# under `src/app/docs` in place and `run_guard` reads the whole tree, but this
# harness took NO lock, while its two siblings carried one for the same shape
# (cards t_ed66094c / t_03a646e2). Reproduced in ONE worktree: two runs started
# together printed a false `6/6 HOLE cases RED, 0/3 CONTROLS GREEN` (the shape the
# harness then stood at — six HOLE / three CONTROLS), both rc=1,
# and left two probe mutations on disk. No per-run channel can fix that: the
# shared thing is the INPUT.
#
# The lock is taken before the premise (CASE 0), which READS the tree, and is held
# until this process exits. It is keyed on the WORKTREE, so two lanes' cards never
# queue behind each other, and a contended run WAITS and says so rather than
# failing — a refusal would turn "someone else is running it" into a red harness.
#
# The PARENT takes the lock on fd 9 and re-execs this script; the child inherits the
# open descriptor, so the lock is held for exactly as long as the copy that mutates
# anything lives, and it is released when the parent exits.
#
# The child PROVES the lock is held before it prints anything: a second take on the
# same file from a separate process must be REFUSED, and a fresh file in the same
# directory must lock FINE — or "refused" would mean flock is inert here, not that
# this run holds the lock.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and two runs mutate the same docs pages and print FALSE" >&2
    echo "verdicts (t_f4f4681c, reproduced). Install flock before trusting any verdict" >&2
    echo "from it." >&2
    exit 2
  }
  case "$LOCK_WAIT" in ''|*[!0-9]*)
    echo "BITEPROOF REFUSED: BITEPROOF_LOCK_WAIT='$LOCK_WAIT' is not a whole number of seconds." >&2
    exit 2 ;;
  esac
  BITE_GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
    echo "BITEPROOF REFUSED: cannot resolve this worktree's git dir (git rev-parse" >&2
    echo "--absolute-git-dir), so there is no per-worktree key to serialise on, and an" >&2
    echo "unserialised run prints false verdicts when a peer runs here (t_f4f4681c)." >&2
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
    echo "  lock:   another run of this harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (both runs mutate the same docs pages, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer that mutates the same docs pages" >&2
    echo "and would print false verdicts. Nothing was mutated. Re-run once the other run" >&2
    echo "finishes, or raise the wait with BITEPROOF_LOCK_WAIT=<seconds>." >&2
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

# The child. Refuse if the lock it is supposed to have inherited cannot be shown to be
# held: an unverified lock is the same shape as an unverified verdict.
LOCK_FILE="${BITEPROOF_LOCK_FILE:-}"
if [ -z "$LOCK_FILE" ]; then
  echo "BITEPROOF REFUSED: BITEPROOF_LOCKED=1 is set but BITEPROOF_LOCK_FILE is empty, so this" >&2
  echo "run cannot show it holds the per-worktree lock. Unset both and re-run the harness." >&2
  exit 2
fi
if flock -n "$LOCK_FILE" true 2>/dev/null; then
  echo "BITEPROOF REFUSED: the per-worktree lock ($LOCK_FILE) was not held here, so this run is" >&2
  echo "NOT serialised — a peer mutating the same docs pages would give it false verdicts." >&2
  echo "Run the harness WITHOUT BITEPROOF_LOCKED set and let it take the lock itself." >&2
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

# The guard to drive. Overridable so a leg of web/src/lib/harnessLock.test.ts can
# reach this harness's own deterministic, node-free refusal AFTER it has taken its
# lock (point this at a path that does not exist), which is how that file proves the
# key is per-worktree without running the suite. Default is the real guard.
GUARD="${BITEPROOF_GUARD:-src/lib/docsProseConvention.test.ts}"
[ -f "$GUARD" ] || { echo "harness: $GUARD is missing — nothing to drive"; exit 2; }

snap="$(mktemp -d)"
trap 'rm -rf "$snap"' EXIT

run_guard() { node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1; }

# RED / GREEN / REFUSED, never inferred from the absence of a failure line: a
# guard that cannot load reports `tests 1 / pass 0 / fail 1`, and calling that
# GREEN would print a bite for a case that wanted RED while the instrument was
# broken (the rule web/scripts/biteproof_docs_tool_claims.sh established).
verdict() {
  local o="$1"
  if grep -qE '^ℹ fail [1-9]' <<<"$o" && grep -qE '^ℹ pass [1-9]' <<<"$o"; then echo RED; return; fi
  if grep -qE '^ℹ pass [1-9]' <<<"$o" && grep -qE '^ℹ fail 0' <<<"$o" && grep -qE '^✔ ' <<<"$o"; then echo GREEN; return; fi
  echo REFUSED
}

fail=0
declare -a VERDICTS=()

snapshot() { cp -p "$1" "$snap/$(echo "$1" | tr / _)"; }
restore() {
  local f="$1" s="$snap/$(echo "$1" | tr / _)"
  cp -p "$s" "$f"
  cmp -s "$s" "$f" || { echo "  RESTORE MISMATCH: $f"; fail=1; }
}

echo "== CASE 0 (control): the unmutated tree must be GREEN =="
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case0=$v")
if [ "$v" = GREEN ]; then
  echo "  control: GREEN (the guard is not a tautology — it passes on the fixed tree)"
else
  echo "  control FAILED: expected GREEN, got $v"; printf '%s\n' "$out" | tail -25; exit 2
fi

# ── the HOLE cases: a marker present, the guard must go RED naming the page ────
echo "== CASE 1: the shipped defect re-inserted — a backtick span on /docs/chat =="
f="src/app/docs/chat/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/The fourth creates: upsert_form is proposable/The fourth creates: `upsert_form` is proposable/' "$f"
grep -q 'The fourth creates: `upsert_form`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case1=$v")
if [ "$v" = RED ] && grep -q 'docs/chat/page.tsx' <<<"$out"; then
  echo "  BITES: RED naming docs/chat/page.tsx (the exact sentence /docs/chat shipped)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/chat)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 2: a marker in a DIFFERENT page — the defect was site-wide (4 pages) =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/over MCP with list_trusted_devices, revoke_trusted_device/over MCP with `list_trusted_devices`, revoke_trusted_device/' "$f"
grep -q 'with `list_trusted_devices`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case2=$v")
if [ "$v" = RED ] && grep -q 'docs/security/page.tsx' <<<"$out"; then
  echo "  BITES: RED naming docs/security/page.tsx (the guard is not anchored on one file)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 3: a marker in a THIRD page (/docs/sales) =="
f="src/app/docs/sales/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/reports the mode as mode \(payment/reports the mode as `mode` (payment/' "$f"
grep -q 'as `mode` (payment' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case3=$v")
if [ "$v" = RED ] && grep -q 'docs/sales/page.tsx' <<<"$out"; then
  echo "  BITES: RED naming docs/sales/page.tsx"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/sales)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the NEGATIVE controls: shapes the guard must NOT report ───────────────────
echo "== CASE 4 (negative control): a backtick in a COMMENT stays GREEN =="
f="src/app/docs/chat/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's|^import type \{ Metadata \} from "next";|import type { Metadata } from "next";\n// qa-probe: a COMMENT naming `do_not_flag_me` and `also_not` — a text scanner would report this, an AST cannot.|' "$f"
grep -q 'qa-probe: a COMMENT naming' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case4=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a comment is not a string literal, so the AST walk never sees it"
else
  echo "  CONTROL FAILED: verdict=$v — the guard is a text scan, not a parse"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 5 (negative control): a JSX template literal showing code stays GREEN =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The probe is a JSX template literal — the supported way to show code on a docs
# page — inserted at module scope right after the import so it parses as TSX
# without needing to find a render site (the AST walk covers the whole file).
printf 'const QA_PROBE = <p>{`a code sample: get_form_analytics`}</p>;\n' > "$snap/probe.tsx"
sed -i "/^import type { Metadata } from \"next\";/r $snap/probe.tsx" "$f"
grep -q 'a code sample: get_form_analytics' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case5=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a template literal is NoSubstitutionTemplateLiteral, not a StringLiteral"
else
  echo "  CONTROL FAILED: verdict=$v — the supported way to show code on /docs was reported"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the guard must also be looking at the tree, not a cached set ──────────────
echo "== CASE 6: a marker in the shared docs LAYOUT (a different .tsx, not a page) =="
f="src/app/docs/layout.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/label: "Overview & features"/label: "Overview \& features `probe`"/' "$f"
grep -q 'features `probe`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case6=$v")
if [ "$v" = RED ] && grep -q 'docs/layout.tsx' <<<"$out"; then
  echo "  BITES: RED naming docs/layout.tsx (the walk covers the shared chrome too)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/layout.tsx)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# The widened cases (card t_8580357e) must be RED *for the widened reason*: node's
# own count is exactly ONE failing test, and the test that fails is the FINDINGS
# rule, not the vacuity rule. A guard that widened the findings walk but left the
# vacuity walk string-literal-only would fail this (the vacuity test would not see
# the new node and could not be the failing one), which is the point of asserting
# the name rather than the count alone.
fails_findings_only() {
  local o="$1"
  grep -qE '^ℹ fail 1$' <<<"$o" &&
    grep -q '^✖ no /docs prose carries a backtick marker' <<<"$o" &&
    ! grep -q '^✖ the guard is reading the real docs tree' <<<"$o"
}

# The IMPORTED-prose surface (card t_763c28c2) fails in its OWN test, not the
# DOCS_DIR one: a marker in a generated module the /docs/api page prints is
# reported by `no /docs imported prose carries a backtick marker`, so the
# attribution rule is the same shape with that test's name.
fails_imported_findings_only() {
  local o="$1"
  grep -qE '^ℹ fail 1$' <<<"$o" &&
    grep -q '^✖ no /docs imported prose carries a backtick marker' <<<"$o" &&
    ! grep -q '^✖ the imported-prose check is reading the real rendered data' <<<"$o"
}

# The i18n-LABEL surface (card t_cd81632d) fails in its OWN test too: a marker in
# a `docs.*` value the /docs index prints through the T island is reported by
# `no /docs i18n label carries a backtick marker`, not by the DOCS_DIR rule and
# not by the imported one.
fails_label_findings_only() {
  local o="$1"
  grep -qE '^ℹ fail 1$' <<<"$o" &&
    grep -q '^✖ no /docs i18n label carries a backtick marker' <<<"$o" &&
    ! grep -q '^✖ the i18n-label check is reading the real rendered keys' <<<"$o"
}

# The VACUITY BOUND for the i18n-label surface: the new leg's own vacuity test
# must have RUN and PASSED, positively — if it did not, a RED from it would say
# nothing about the reader (the parse could have found no label at all), and a
# GREEN would not prove the leg looked at anything.
i18n_vacuity_ran() {
  grep -q '^✔ the i18n-label check is reading the real rendered keys' <<<"$1"
}

# ── the two shapes card t_8580357e widened the guard to see ───────────────────
echo "== CASE 7: a marker in RAW JSX TEXT (ts.JsxText) must be RED ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# Raw JSX prose: neither a string literal nor a template literal, so the
# string-literal-only walk let it through GREEN. Inserted at module scope so it
# parses as TSX without a render site (the AST walk covers the whole file).
printf 'const QA_RAWTEXT = <p>raw jsx prose with `raw_marker` inside</p>;\n' > "$snap/p7.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p7.txt" "$f"
grep -q 'raw jsx prose with `raw_marker` inside' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case7=$v")
if [ "$v" = RED ] && grep -q 'docs/security/page.tsx' <<<"$out" && grep -q '\[raw JSX text\]' <<<"$out" && fails_findings_only "$out"; then
  echo "  BITES: RED naming docs/security/page.tsx [raw JSX text] (the JsxText hole is closed)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security, [raw JSX text], one findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 8: a marker ESCAPED inside a TEMPLATE LITERAL must be RED ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# A NoSubstitutionTemplateLiteral whose decoded `node.text` carries a backtick:
# the escaped form renders the marker exactly like the plain one. Before the
# widening this node kind was the guard's documented exclusion.
printf 'const QA_ESCAPED = `prose with \\`escaped_marker\\` inside`;\n' > "$snap/p8.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p8.txt" "$f"
grep -q 'prose with \\`escaped_marker\\` inside' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case8=$v")
if [ "$v" = RED ] && grep -q 'docs/security/page.tsx' <<<"$out" && grep -q '\[template literal\]' <<<"$out" && fails_findings_only "$out"; then
  echo "  BITES: RED naming docs/security/page.tsx [template literal] (the escaped-marker hole is closed)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security, [template literal], one findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 9 (negative control): the SAME escaped marker inside <code> stays GREEN ==="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The supported way to show code on /docs: a template literal inside <code>. A
# sample that prints a backtick is code the page MEANS to show, so the widening
# must not report it — this is the control that keeps CASE 8 from being "flag
# every template literal".
printf 'const QA_CODEBT = <code>{`a sample that prints a \\`marker\\``}</code>;\n' > "$snap/p9.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p9.txt" "$f"
grep -q 'a sample that prints a \\`marker\\`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case9=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a template literal inside <code> is the supported code-sample shape"
else
  echo "  CONTROL FAILED: verdict=$v — the widening flagged a code sample as prose"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the FOURTH shape card t_8b3a508b widened the guard to see ─────────────────
# A TemplateExpression — a template literal WITH a substitution — is neither a
# NoSubstitutionTemplateLiteral (it HAS a `${…}`) nor a string literal, so the
# walk t_8580357e widened still reported GREEN on it while the reader saw the
# marker (measured on the PR #975 head, guard unmodified: a probe of exactly
# CASE 12's line left the suite at 2 pass / 0 fail). CASE 12 drives that hole;
# CASE 13 is the negative control that keeps the code-sample exclusion covering
# BOTH template-literal shapes.
echo "== CASE 12: a marker in a TEMPLATE EXPRESSION (with a \${…}) must be RED =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The static parts around a `${x}` carry the backtick; `head` and each span
# `literal` decode the backslash-backtick exactly as CASE 8's `node.text` does,
# so the reader sees the marker. Inserted at module scope so it parses as TSX
# without a render site (the AST walk covers the whole file). printf sees `\\`
# and writes ONE backslash, so the file holds a real `\`` escape — the same
# escaping convention CASE 8/9 use. Inside single quotes bash never expands
# `${x}`, so the file holds a literal `${x}` the parser reads as a substitution.
printf 'const QA_TMPLEXPR = (x: string) => `prose ${x} with \\`te_marker\\` inside`;\n' > "$snap/p12.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p12.txt" "$f"
grep -qF 'prose ${x} with \`te_marker\` inside' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case12=$v")
if [ "$v" = RED ] && grep -q 'docs/security/page.tsx' <<<"$out" && grep -q '\[template expression\]' <<<"$out" && grep -q 'te_marker' <<<"$out" && fails_findings_only "$out"; then
  echo "  BITES: RED naming docs/security/page.tsx [template expression] (the TemplateExpression hole is closed)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming docs/security, [template expression], one findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 13 (negative control): a \${…} template literal inside <code> stays GREEN =="
f="src/app/docs/security/page.tsx"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The code-sample exclusion must cover BOTH template-literal shapes: a sample
# that shows code with a substitution is still the supported way to show code on
# /docs. Without this the widening would report every interpolation that prints a
# backtick as part of the code it shows.
printf 'const QA_CODEBTEXPR = (n: number) => <code>{`sample ${n} with \\`marker\\``}</code>;\n' > "$snap/p13.txt"
sed -i "/^import type { Metadata } from \"next\";/r $snap/p13.txt" "$f"
grep -qF 'sample ${n} with \`marker\`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case13=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a \${…} template literal inside <code> is the supported code-sample shape"
else
  echo "  CONTROL FAILED: verdict=$v — the widening flagged a code sample as prose"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the IMPORTED-prose surface (card t_763c28c2) ──────────────────────────────
# /docs/api's prose is not in the page .tsx: it is imported from the generated
# modules (web/src/lib/apiExamples.gen.ts, openapiSpec.gen.ts) and printed with a
# JSX expression, so the DOCS_DIR walk above — the whole guard before this card —
# reported GREEN while the live page rendered 20 literal backticks (measured on
# jomform.com 2026-10-05, fresh browser state). CASE 10 drives that hole: a marker
# in the generated DATA module must turn the guard RED naming that file. CASE 11 is
# the negative control that keeps it from flagging code: a backtick inside `curl`,
# which the page renders inside <code>, must stay GREEN.
echo "== CASE 10: a marker in the IMPORTED data module (apiExamples.gen.ts) must be RED ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/Leave slug out and it is generated/Leave `slug` out and it is generated/' "$f"
grep -q 'Leave `slug` out' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case10=$v")
if [ "$v" = RED ] && grep -q 'src/lib/apiExamples.gen.ts' <<<"$out" && fails_imported_findings_only "$out"; then
  echo "  BITES: RED naming src/lib/apiExamples.gen.ts (the imported prose the DOCS_DIR walk cannot see)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/lib/apiExamples.gen.ts, one imported-findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 11 (negative control): a backtick inside the curl CODE SAMPLE stays GREEN ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# `curl` is rendered inside <code> — the supported way to show code — so a backtick
# in it is code the page MEANS to show, not a markdown marker leaked into prose.
# Without this the imported check would flag every code sample.
perl -0pi -e 's/curl -sS/curl -sS `/' "$f"
grep -q 'curl -sS `' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case11=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a backtick in a curl code sample is not judged as prose"
else
  echo "  CONTROL FAILED: verdict=$v — the imported check flagged a code sample"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the two fields the t_763c28c2 leg missed (card t_8f684cfe) ─────────────────
# The imported leg read {e.why}/{e.note} and {e.summary}/{e.description} only, so a
# marker in the example HEADING or in an endpoint's THROTTLE policy — both printed
# as plain JSX text — was invisible. CASE 14 drives the title, CASE 15 the policy,
# and CASE 16 is the negative control that keeps the widened leg from flagging the
# other <code>-rendered code samples (path).
echo "== CASE 14: a marker in an example TITLE ({e.title}) must be RED ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/"title": "3\. Create a form"/"title": "3. Create a `form`"/' "$f"
grep -q '3. Create a `form`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case14=$v")
if [ "$v" = RED ] && grep -q 'src/lib/apiExamples.gen.ts' <<<"$out" && grep -q '\[{e.title}\]' <<<"$out" && fails_imported_findings_only "$out"; then
  echo "  BITES: RED naming src/lib/apiExamples.gen.ts [{e.title}] (the example heading is prose too)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/lib/apiExamples.gen.ts, [{e.title}], one imported-findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 15: a marker in an endpoint's THROTTLE policy ({e.rateLimit}) must be RED ==="
f="src/lib/openapiSpec.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
perl -0pi -e 's/"policy": "2 per second per IP, burst 6"/"policy": "2 per `second` per IP, burst 6"/g' "$f"
grep -q '2 per `second` per IP' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case15=$v")
if [ "$v" = RED ] && grep -q 'src/lib/openapiSpec.gen.ts' <<<"$out" && grep -q '\[{e.rateLimit}\]' <<<"$out" && fails_imported_findings_only "$out"; then
  echo "  BITES: RED naming src/lib/openapiSpec.gen.ts [{e.rateLimit}] (the throttle policy is prose too)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/lib/openapiSpec.gen.ts, [{e.rateLimit}], one imported-findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 16 (negative control): a backtick inside the path CODE SAMPLE stays GREEN ==="
f="src/lib/apiExamples.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# `path` is rendered inside <code> (api/page.tsx:377), exactly like `curl`: a
# backtick there is code the page MEANS to show. Without this the widened imported
# check would flag a second code-sample field.
perl -0pi -e 's/"path": "\/v1\/rest\/keys"/"path": "\/v1\/rest\/`keys`"/' "$f"
grep -q '/v1/rest/`keys`' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case16=$v")
if [ "$v" = GREEN ]; then
  echo "  CONTROL GREEN: a backtick in the path code sample is not judged as prose"
else
  echo "  CONTROL FAILED: verdict=$v — the imported check flagged a code sample"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the i18n-LABEL surface (card t_cd81632d) ──────────────────────────────────
# The /docs index's chrome labels are not in any .tsx the DOCS_DIR walk reads: they
# are dictionary VALUES in src/components/i18nDict.ts printed through the T island
# (src/app/docs/docsChrome.tsx: `const { t } = useI18n(); return <>{t(k)}</>`), a
# plain text node. Measured on this guard's own head (af607fe3, clean worktree): a
# marker planted in `docs.catSecurity` — a key the index renders via
# `{g.labelKey}` at page.tsx:428 — left the suite at 4 pass / 0 fail, and the guard
# never named i18nDict.ts. CASE 17 drives that hole: a marker in a RENDERED
# `docs.*` value must turn the guard RED naming that file. CASE 18 is the negative
# control the card asks for: a marker in a `docs.*` value whose key the /docs pages
# do NOT render must stay GREEN, so the new leg judges the render site (the keys
# the page prints) rather than every `docs.*` string in the dictionary.
echo "== CASE 17: a marker in a RENDERED i18n label value (i18nDict.ts) must be RED ==="
f="src/components/i18nDict.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# `docs.catSecurity` is a key the /docs index DOES render: it is declared as a
# GROUPS `labelKey` in src/app/docs/page.tsx and printed by `<T k={g.labelKey} />`.
perl -0pi -e 's/"docs\.catSecurity": \{ en: "Security",/"docs.catSecurity": { en: "Security `label_probe`",/' "$f"
grep -qF '"docs.catSecurity": { en: "Security `label_probe`"' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case17=$v")
if [ "$v" = RED ] && grep -q 'src/components/i18nDict.ts' <<<"$out" && \
   grep -q 'label_probe' <<<"$out" && fails_label_findings_only "$out" && i18n_vacuity_ran "$out"; then
  echo "  BITES: RED naming src/components/i18nDict.ts (the label the DOCS_DIR walk cannot see),"
  echo "         one failing test, and it is the label-findings rule; the vacuity bound ran GREEN"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/components/i18nDict.ts, one label-findings"
  echo "  failure, vacuity bound green)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== CASE 18 (negative control): a marker in a docs.* key NO page renders stays GREEN ==="
f="src/components/i18nDict.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# Measured: the 19 `docs.*` keys the dictionary ships are EXACTLY the 19 keys a
# /docs page renders (the eight GROUPS `labelKey`s, the five QUICK_LINKS
# `labelKey`s, and the six literal `<T k="…" />` keys), so no existing key is an
# unrendered one. This case therefore ADDS one — a realistic "key someone added to
# the dictionary" shape, with all five locales — plants a marker in it, and
# requires GREEN. The new leg is keyed on the RENDER SITE, so a marker in a value
# no /docs page prints must NOT be reported: without this control the leg would be
# "flag every docs.* string", which is the assertion the card forbids, and its
# coverage would grow the moment an unrelated key appeared in the dictionary.
perl -0pi -e 's|("docs\.linkAPI": \{[^\n]*\n)|\1  "docs.qaUnrendered": { en: "QA `unrendered_probe`", ms: "QA", zh: "QA", ta: "QA", ar: "QA" },\n|' "$f"
grep -qF '"docs.qaUnrendered": { en: "QA `unrendered_probe`"' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
# The premise, asserted rather than assumed: the probe key must really be rendered
# NOWHERE under src/app/docs, or this control would be testing the opposite of what
# it claims.
if grep -rqF 'docs.qaUnrendered' src/app/docs; then
  echo "  MUTATION DID NOT APPLY — docs.qaUnrendered is rendered by a docs page, so this is not an unrendered key"; fail=1
fi
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case18=$v")
if [ "$v" = GREEN ] && i18n_vacuity_ran "$out"; then
  echo "  CONTROL GREEN: a docs.* key no /docs page renders is not asserted (the leg is keyed on the render site)"
else
  echo "  CONTROL FAILED: verdict=$v — the label leg flagged a key the page prints nowhere, or its"
  echo "  vacuity bound did not run"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

# ── the endpoint GROUP HEADINGS (card t_62da3451) ─────────────────────────────
# The imported leg read {e.why}/{e.note}/{e.title} and {e.summary}/{e.description}/
# {e.rateLimit}, but /docs/api also prints ONE more imported string as plain JSX
# text: each endpoint GROUP's heading, `{group.tag}` in an <h3> at api/page.tsx:368
# (NOT inside <code>). `group` comes from endpointsByTag(), whose tag is the
# contract's own `tags[]` in openapiSpec.gen.ts, so a marker in a tag reached the
# reader while the leg reported GREEN — measured on the #984 head (5e3b8d58, tree
# daa6906): editing the FIRST `"Profile"` tag left the suite at 4 pass / 0 fail.
# CASE 19 drives that hole. There is no negative control beside it: unlike curl /
# path, the tag has NO <code>-rendered render site — the only other use of a group
# tag is `key={group.tag}`, an attribute the reader never sees — so a control would
# assert a shape the page does not have.
echo "== CASE 19: a marker in an endpoint GROUP tag ({group.tag}) must be RED ==="
f="src/lib/openapiSpec.gen.ts"; snapshot "$f"; sha="$(sha256sum "$f" | awk '{print $1}')"
# The tag line is uniquely 12-space-indented (the declared tags[] names at :8250+
# use 6). Anchored to the session route's tag block so the substitution cannot drift
# onto another operation.
perl -0pi -e 's/"tags": \[\n          "Profile"\n        \],\n        "x-jomform-access": "session"/"tags": [\n          "Pro`file"\n        ],\n        "x-jomform-access": "session"/' "$f"
grep -q '"Pro`file"' "$f" || { echo "  MUTATION DID NOT APPLY — re-anchor this case"; fail=1; }
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("case19=$v")
if [ "$v" = RED ] && grep -q 'src/lib/openapiSpec.gen.ts' <<<"$out" && grep -q '\[{group.tag}\]' <<<"$out" && fails_imported_findings_only "$out"; then
  echo "  BITES: RED naming src/lib/openapiSpec.gen.ts [{group.tag}] (the group heading is prose too)"
else
  echo "  DID NOT BITE: verdict=$v (wanted RED naming src/lib/openapiSpec.gen.ts, [{group.tag}], one imported-findings failure)"; printf '%s\n' "$out" | tail -20; fail=1
fi
restore "$f"; echo "  restored, sha $(sha256sum "$f" | awk '{print $1}') == $sha"

echo "== FINAL: every mutated file restored, tree GREEN again ==="
out="$(run_guard)"; v="$(verdict "$out")"; VERDICTS+=("final=$v")
[ "$v" = GREEN ] || { fail=1; printf '%s\n' "$out" | tail -25; }

echo
echo "verdicts: ${VERDICTS[*]}"
echo "BITES: $(printf '%s\n' "${VERDICTS[@]}" | grep -cE '^case(1|2|3|6|7|8|10|12|14|15|17|19)=RED')/12 HOLE cases RED, $(printf '%s\n' "${VERDICTS[@]}" | grep -cE '^case(4|5|9|11|13|16|18)=GREEN')/7 CONTROLS GREEN"
echo "RESULT: $([ "$fail" = 0 ] && echo 'ALL CASES OK' || echo 'FAILURES PRESENT')"
exit "$fail"
