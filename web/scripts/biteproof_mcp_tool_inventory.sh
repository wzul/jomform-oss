#!/usr/bin/env bash
# Bite proof for web/src/lib/mcpToolInventory.test.ts (card t_cde2e73a).
#
# A guard that only ever passes is a tautology. This does three things:
#
#   1. Runs the guard on the REAL pre-fix page (the revision at the merge-base
#      with origin/main, i.e. the tree the card was filed against) and requires
#      it RED naming all ten missing tools. That is the strongest case here: it
#      proves the guard catches the actual defect, not just synthetic mutations.
#   2. Mutates one fact at a time (including one case in the Go registry the page
#      mirrors) and requires the guard to FAIL naming its own reason.
#   3. Restores every file byte-identically (sha256 re-checked).
#
# Both DIRECTIONS are proven, because the whole point of this card is that only
# one of them was ever guarded: /docs/mcp could omit a registered tool forever
# with a green suite.
#
# Run:  bash web/scripts/biteproof_mcp_tool_inventory.sh
set -uo pipefail

# The script's OWN absolute path is resolved BEFORE the `cd` below, because the child
# this file re-execs under the lock needs a path that survives a changed CWD — a
# relative `$0` resolved after the `cd` names a file that is not there.
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
cd "$BITE_HOME/.." || exit 2               # -> web/

# ---- THE FAMILY'S SHARED-INPUT LOCK (t_76b3356d) ------------------------------------
#
# This harness MUTATES `src/app/docs/mcp/page.tsx` and `api/internal/mcpsrv/tools.go` and
# `mcpToolInventory.test.ts` walks the whole docs tree + the Go registry, so two runs that
# overlap in ONE worktree read each other's mutations and print FALSE verdict lines. It is
# the SAME shared input every /docs bite harness mutates, so the whole family takes ONE
# lock file per worktree, `jomform-biteproof-docs-bite`, taken from before the first case
# (which READS the page) until this process exits.
#
# The key is the WORKTREE (`git rev-parse --absolute-git-dir`), under a HOST-GLOBAL base
# (`/tmp`, never `$TMPDIR`), so two lanes' cards never queue behind each other. A contended
# run WAITS and says so rather than failing; a wait-out is exit 75. The PARENT takes it on
# fd 9 and re-execs this script; the child inherits the open descriptor and PROVES it holds
# it (a second take must be refused, a fresh file in the same directory must lock FINE)
# before it prints anything.
LOCK_BASE="${BITEPROOF_LOCK_DIR:-/tmp}"
LOCK_WAIT="${BITEPROOF_LOCK_WAIT:-1800}"

if [ "${BITEPROOF_LOCKED:-}" != "1" ]; then
  command -v flock >/dev/null 2>&1 || {
    echo "BITEPROOF REFUSED: flock is not installed, so this harness cannot serialise two" >&2
    echo "runs in one worktree — and overlapping runs mutate the same docs files and print" >&2
    echo "FALSE verdicts (t_76b3356d, reproduced). Install flock before trusting any" >&2
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
    echo "unserialised run prints false verdicts when a peer runs here." >&2
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
    echo "  lock:   another /docs bite harness holds this worktree's lock — waiting up to ${LOCK_WAIT}s"
    echo "          (the family shares its input, so concurrently they print FALSE verdicts)"
  fi
  flock -w "$LOCK_WAIT" -E 75 9
  _lock_rc=$?
  if [ "$_lock_rc" = 75 ]; then
    echo "BITEPROOF REFUSED: could not take the per-worktree lock $LOCK_FILE within ${LOCK_WAIT}s," >&2
    echo "so this run is NOT serialised against a peer that mutates the same docs files" >&2
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
  echo "NOT serialised — a peer mutating the same docs files would give it false verdicts." >&2
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

# CAPABILITY, not presence (card t_b014807f): `node --version` succeeding proves nothing
# here — v18.19.1 answers it and then rejects `--disable-warning`, and every leg below
# would read that empty capture as a verdict. The gate runs the invocation this harness
# depends on and refuses, naming the node, when that produces no test-runner summary.
nt_require_node

PAGE="${BITEPROOF_PAGE:-src/app/docs/mcp/page.tsx}"
TOOLS=../api/internal/mcpsrv/tools.go
GUARD=src/lib/mcpToolInventory.test.ts
# git's rev:path form is relative to the REPO ROOT, not this cwd — resolve it once.
TOP=$(git rev-parse --show-toplevel) || exit 2
PAGE_IN_REPO="${PWD#"$TOP"/}/$PAGE"

fail=0
PAGE_BAK=$(mktemp); TOOLS_BAK=$(mktemp)
# restore_all on EVERY exit, not just the clean one. The trap is installed
# here (before any file is touched) and again after restore_all is defined,
# because bash resolves a trap command at SIGNAL time: an exit 2 raised
# before restore_all existed would run a trap that calls an unknown
# function, and the mutated page would be left in the worktree. That is not
# hypothetical — the CASE 1 needle check exits 2 when its list goes stale,
# and an earlier version of this script did leave the pre-fix page in place.
trap 'rm -f "$PAGE_BAK" "$TOOLS_BAK"' EXIT

for f in "$PAGE" "$TOOLS"; do
  if [ ! -f "$f" ]; then echo "missing $f — is this the right worktree?" >&2; exit 2; fi
done
cp -f "$PAGE" "$PAGE_BAK"
cp -f "$TOOLS" "$TOOLS_BAK"
PAGE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)
TOOLS_SHA=$(sha256sum "$TOOLS" | cut -d' ' -f1)

# run_guard -> "GREEN", "RED" or "REFUSED"; output kept in /tmp/bite.inv.out.
# REFUSED means the guard did not demonstrably run (no summary, or all-fail) and is
# a HARD FAILURE at every call site — never a verdict about the mutation.
# NOTE: the guard EXITS NON-ZERO when it fails, so capture the output first.
# A `node --test … | grep -q` pipe under `set -o pipefail` reads a real match as
# a failure — that trap produced a false verdict on this repo once already.
# THE VERDICT NEEDS POSITIVE EVIDENCE THE GUARD RAN (card t_9ea606ac). This used to be
# `if grep -qE '^ℹ fail [1-9]' <<<"$out"; then RED; else GREEN; fi` — a decision from the
# ABSENCE of a failure line. So a guard that never started (a missing file, a file that
# cannot parse, an all-skipped guard) read GREEN, and on the BASE column GREEN means
# "the old guard missed this" — a BITES reported for an instrument that was not running.
# Measured shapes on node 26.7.0: PASS prints `ℹ tests 9 / pass 9` with `✔ ` lines; a
# mutated page prints `pass 8 / fail 1`; a guard that cannot load prints
# `tests 1 / pass 0 / fail 1`; an all-skipped guard prints `tests 1 / pass 0 / skipped 1`;
# a missing file prints one line and NO summary at all.
#
# The rule is the sibling `biteproof_docs_tool_claims.sh`'s `rule_of` (card t_420ca40c),
# applied here for the same reason: GREEN needs rc=0 with `pass [1-9]` AND a `✔ ` line;
# RED needs `fail [1-9]` AND `pass [1-9]`; anything else REFUSES, which is a hard failure
# rather than a verdict. WHAT IT CANNOT SEE, measured: node counts a .ts file that
# declares no tests as `tests 1 / pass 1` plus a `✔ <filename>` line — the same shape as
# a real run — so a guard emptied of its tests still reads GREEN. That case is the
# guard's own business (its "cannot pass vacuously" test).
run_guard() {
  local out rc
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1) || true
  rc=$?
  if ! grep -qE '^ℹ ' <<<"$out"; then printf 'REFUSED'; printf '%s' "$out" > /tmp/bite.inv.out; return; fi
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then
    if grep -qE '^ℹ pass [1-9]' <<<"$out"; then printf 'RED'; else printf 'REFUSED'; fi
  elif grep -qE '^ℹ pass [1-9]' <<<"$out" && grep -qE '^✔ ' <<<"$out"; then
    printf 'GREEN'
  else
    printf 'REFUSED'
  fi
  printf '%s' "$out" > /tmp/bite.inv.out
}

# check <label> <expected-verdict> <needle-the-guard-must-name>
# The mutation is applied by the caller and MUST have changed the file: a
# mutation that silently did not apply would read as a green guard, so a failed
# `mutate` is a hard failure here rather than a skipped case.
check() {
  local label="$1" want="$2" needle="$3" got
  got=$(run_guard)
  if [ "$got" != "$want" ]; then
    echo "FAIL  $label — guard was $got, expected $want"
    grep -E '^(✔|✖|ℹ (pass|fail))' /tmp/bite.inv.out | head -10
    fail=1
    return
  fi
  if [ -n "$needle" ] && ! grep -qF -- "$needle" /tmp/bite.inv.out; then
    echo "FAIL  $label — guard went $got but did not name: $needle"
    fail=1
    return
  fi
  echo "BITES $label  (guard $got${needle:+, naming: $needle})"
}

# check_needles <label> <expected-verdict> <needle>... — like `check`, but every
# needle must be named. Used by the one case whose claim is a LIST of tools
# rather than a single phrase: "omits N registered tool(s)" and then each name.
# A case that names the count but drops the names would pass `check` while
# proving the count and not the defect, so the names are asserted individually.
check_needles() {
  local label="$1" want="$2" got missing=()
  shift 2
  got=$(run_guard)
  if [ "$got" != "$want" ]; then
    echo "FAIL  $label — guard was $got, expected $want"
    grep -E '^(✔|✖|ℹ (pass|fail))' /tmp/bite.inv.out | head -10
    fail=1
    return
  fi
  for n in "$@"; do
    grep -qF -- "$n" /tmp/bite.inv.out || missing+=("$n")
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    echo "FAIL  $label — guard went $got but did not name: ${missing[*]}"
    fail=1
    return
  fi
  echo "BITES $label  (guard $got, naming all $# of: $*)"
}

# mutate <file> <old-literal> <new-literal>
# Literal replacement via perl with the values passed in the ENVIRONMENT, not
# interpolated into the one-liner: the tool descriptions on this page contain
# `/`, `{`, `}` and `|`, any of which silently breaks a pattern built by string
# interpolation (that happened on the first run of this script — one case
# aborted with a perl syntax error and was skipped). A non-matching `old` aborts
# loudly, because a mutation that did not apply proves nothing.
mutate() {
  local f="$1" before after
  before=$(sha256sum "$f" | cut -d' ' -f1)
  OLD="$2" NEW="$3" perl -0pi -e 's{\Q$ENV{OLD}\E}{$ENV{NEW}}' "$f" || return 1
  after=$(sha256sum "$f" | cut -d' ' -f1)
  if [ "$before" = "$after" ]; then
    echo "MUTATION DID NOT APPLY in $f" >&2
    return 1
  fi
}

# del_line <file> <the row's leading text> — removes the ONE line containing that
# text (the shape the card found ten of: a registered tool with no row at all),
# and fails unless exactly one line went away.
del_line() {
  local f="$1" marker="$2" before after n
  before=$(wc -l < "$f")
  n=$(grep -cF -- "$marker" "$f")
  if [ "$n" != "1" ]; then
    echo "MUTATION DID NOT APPLY: $n line(s) match the marker in $f (want exactly 1)" >&2
    return 1
  fi
  grep -vF -- "$marker" "$f" > "$f.tmp" && mv "$f.tmp" "$f"
  after=$(wc -l < "$f")
  if [ "$after" -ne "$((before - 1))" ]; then
    echo "MUTATION DID NOT APPLY: line count $before -> $after in $f" >&2
    return 1
  fi
}

# Every case must APPLY its mutation: a mutation that silently failed would skip
# its `check` and leave this proof reporting PASS without having tested anything.
# (That happened on an earlier run of this script — a `mutate` with a `&&` chain
# that failed on a multi-line pattern.) `must_apply` is what makes that loud.
must_apply() {
  local label="$1"; shift
  if ! "$@"; then
    echo "FAIL  $label — the mutation did not apply, so the case proved nothing"
    fail=1
    return 1
  fi
  return 0
}

restore_all() {
  cp -f "$PAGE_BAK" "$PAGE"
  cp -f "$TOOLS_BAK" "$TOOLS"
  [ "$(sha256sum "$PAGE" | cut -d' ' -f1)" = "$PAGE_SHA" ] || { echo "PAGE NOT RESTORED"; fail=1; }
  [ "$(sha256sum "$TOOLS" | cut -d' ' -f1)" = "$TOOLS_SHA" ] || { echo "TOOLS NOT RESTORED"; fail=1; }
}
# The trap for the rest of the run: restore, then clean up. `restore_all` now
# exists, so an abort anywhere below leaves both files as it found them.
restore_and_cleanup() {
  restore_all
  rm -f "$PAGE_BAK" "$TOOLS_BAK"
}
trap restore_and_cleanup EXIT

echo "guard: $GUARD"
echo "page:  $PAGE  sha ${PAGE_SHA:0:12}"
echo

# Baseline: the guard must be GREEN on the real tree before any mutation, or the
# RED column below proves nothing.
got=$(run_guard); if [ "$got" != "GREEN" ]; then
  echo "FAIL  baseline is $got, not GREEN — fix the guard before proving it bites"
  grep -E '^(✖|ℹ (pass|fail))' /tmp/bite.inv.out | head -10
  exit 1
fi
echo "BASELINE GREEN"
echo

# reg_tools <tools.go> -> the number of `&mcp.Tool{` literals in the registry.
# A count off the FILE, not off the guard's extracted name set.
reg_tools() {
  grep -c -- '&mcp.Tool{' "$1"
}

# omitted_count <page.tsx> <tools.go> -> how many REGISTERED tools that page has
# no row for. CASE 1 uses it to word the count in the guard's message, and that
# count must be DERIVED from the two files it is talking about: it was a typed
# `10` in this file, and the moment main's registry grew (PR #753, 168 -> 169)
# the real page omitted 11, the guard still went RED for exactly the right
# reason, and this proof FAILED because nobody had re-typed the needle. A
# hand-maintained copy of a number is the defect this whole card is about, one
# level up from the page itself.
#
# NOTE it is NOT `registry - rows`: the pre-fix page's 164 rows contain five
# names twice and one em-dash phantom, so 164 rows are only 158 registered
# tools. The set difference is what the guard reports, so that is what is
# measured here — and `check_needles` below then requires the guard's own
# message to carry this exact phrase, which is what makes the two agree.
omitted_count() {
  node -e '
    const fs=require("fs");
    const [page,go]=process.argv.slice(1);
    const src=fs.readFileSync(page,"utf8");
    const s=src.indexOf("const TOOL_GROUPS"), e=src.indexOf("\n];", s);
    const block=s>=0&&e>s?src.slice(s,e):"";
    const code=block.split("\n").filter((l)=>!l.trimStart().startsWith("//")).join("\n");
    const rows=[...code.matchAll(/\{\s*name:\s*"([^"]*)"/g)].map((m)=>m[1]);
    const reg=[...fs.readFileSync(go,"utf8").matchAll(/&mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"/g)].map((m)=>m[1]);
    const have=new Set(rows);
    process.stdout.write(String(reg.filter((n)=>!have.has(n)).length));
  ' "$1" "$2" 2>/dev/null
}

# claim_needle <file-basename> -> the guard's own per-file phrase "holds N of the
# M", DERIVED from the two counts it is talking about (the `&mcp.Tool{` literal
# count in that file and the count over the whole non-test package). Typed here it
# would go stale the moment the registry moves — the same hand-maintained-copy
# defect the page's own count guard exists to stop, one level up.
claim_needle() {
  node -e '
    const fs=require("fs");
    const dir="../api/internal/mcpsrv/";
    const files=fs.readdirSync(dir).filter((f)=>f.endsWith(".go")&&!f.endsWith("_test.go"));
    const n=(f)=>((fs.readFileSync(dir+f,"utf8").match(/&mcp\.Tool\{/g))||[]).length;
    const total=files.reduce((s,f)=>s+n(f),0);
    const want=process.argv[1];
    if(!files.includes(want)){process.stderr.write("not a non-test mcpsrv source file: "+want+"\n");process.exit(1);}
    process.stdout.write("holds "+n(want)+" of the "+total);
  ' "$1" 2>/dev/null
}

# ── The mutation cases run FIRST ──────────────────────────────────────────────
# CASE 1 (the real pre-fix page from a merge-base with origin/main) is the
# strongest case and is deliberately placed LAST, because it is the only case
# that is structurally impossible on some trees: on a tree cut from current main
# the page at the merge-base is byte-identical to this tree's, and the case is
# refused rather than reported green. While it sat first, that refusal called
# `exit 2` and took every case after it — the whole mutation set — down with it.
# On the branch cut from current main that is exactly what happened: CASE 7 below
# was unreachable. Deferred to the end, the same tree now runs every mutation case
# and THEN reports the refusal, honestly and as its own verdict. The case's own
# rule is unchanged — it still never reports a green it did not measure.

# ── CASE 2: a registered tool whose row is really DELETED (the missing direction)
must_apply "row DELETED (submit_feedback)" \
  del_line "$PAGE" '      { name: "submit_feedback", desc:' \
  && check "row DELETED (submit_feedback) -> forward direction" RED "omits 1 registered tool"

# ── CASE 3: a NEW registered Go tool with no docs row ─────────────────────────
restore_all
must_apply "new Go tool renamed in the registry" \
  mutate "$TOOLS" '"submit_feedback",' '"brand_new_tool",' \
  && check "new Go tool with no docs row -> forward direction" RED "brand_new_tool"

# ── CASE 4: a row that is not registered (the direction that WAS guarded) ─────
restore_all
must_apply "phantom row" \
  mutate "$PAGE" '{ name: "list_products", desc:' '{ name: "ghost_tool_xyz", desc:' \
  && check "phantom row -> reverse direction" RED "ghost_tool_xyz"

# ── CASE 5: the count is a REAL count ─────────────────────────────────────────
restore_all
must_apply "duplicated row" \
  mutate "$PAGE" '{ name: "list_products", desc:' '{ name: "set_form_theme", desc:' \
  && check "duplicated row -> count" RED "more than once"

restore_all
must_apply "em-dash placeholder row" \
  mutate "$PAGE" '    group: "Keselamatan sesi",' '    tools: [{ name: "—", desc: "placeholder" }],
    group: "Keselamatan sesi",' \
  && check "em-dash placeholder row -> count" RED "not a tool identifier"

# ── CASE 6: the number must stay derived, not typed ───────────────────────────
restore_all
must_apply "hand-typed count" \
  mutate "$PAGE" '{TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0)} tools across' \
                 '164 tools across' \
  && check "hand-typed count -> derived claim" RED "no longer derives its tool total"

restore_all
must_apply "note-only group counted as a category" \
  mutate "$PAGE" '{TOOL_GROUPS.filter((g) => g.tools.length > 0).length} categories.' \
                 '{TOOL_GROUPS.length} categories.' \
  && check "note-only group counted as a category -> derived claim" RED "counts every group as a category"

# ── CASE 7: the CLAIM, not just the row list (card t_5f8083eb) ────────────────
# The card's defect: the page's <meta name="description"> named a single FILE as
# the registry while the page's body named the package, and nothing looked at the
# head. Both legs are proven, because the check's whole content is "claimed file
# == whole registry":
#   7a  the file name that SHIPPED (tools.go, 180 of the 181 literals) -> RED
#   7b  the file that exposed the drift (parity_issue396.go, 1 of 181) -> RED
# A check that only caught the first would be pinned to one filename rather than
# to the claim, so 7b is what makes it a rule instead of a blocklist. Both
# needles are DERIVED from the current per-file literal counts (claim_needle), so
# a registry that grows moves them with it.
#   7c (card t_69bd144f) the SAME claim in its BARE spelling — no
#      `api/internal/mcpsrv/` in front of it — which the guard shipped BLIND to:
#      it measured GREEN on "the tool registry in tools.go" while the whole web
#      unit suite stayed 510/510. The extraction was widened for that shape, so
#      7a is re-driven in this same run: a widen that lost the path form shows up
#      as a FAIL on 7a next to the pass here, which is the point of carrying both.
#   7d/7e are the NEGATIVE controls the widened extraction must keep: dropping
#      the file name entirely ("the tool registry") is a legitimate state the
#      card allows, and a bare `.go` token that is NOT a source file of this
#      package is prose rather than a claim about this registry — both must stay
#      GREEN, or the widening reds sentences it has no business reding.
restore_all
C7A=$(claim_needle tools.go)
C7B=$(claim_needle parity_issue396.go)
if [ -z "$C7A" ] || [ -z "$C7B" ]; then
  echo "FAIL  cannot derive the per-file literal counts for case 7 — the case would prove nothing" >&2
  exit 2
fi
if [ "$C7A" = "$C7B" ]; then
  echo "FAIL  case 7's two legs derived the same needle ($C7A) — one of them cannot be the subset case" >&2
  exit 2
fi
echo "CASE 7 needles derived: tools.go '$C7A', parity_issue396.go '$C7B'"
must_apply "file name in the metadata description" \
  mutate "$PAGE" 'the tool registry in the api/internal/mcpsrv package.' \
                 'the tool registry in api/internal/mcpsrv/tools.go.' \
  && check "a single file named as the registry (tools.go) -> claim" RED "$C7A"

restore_all
must_apply "a subset file named as the registry" \
  mutate "$PAGE" 'the tool registry in the api/internal/mcpsrv package.' \
                 'the tool registry in api/internal/mcpsrv/parity_issue396.go.' \
  && check "a subset file named as the registry (parity_issue396.go) -> claim" RED "$C7B"

# 7c: the SAME claim in its BARE spelling — the shape this card was filed for.
# No path, so the path-anchored pass cannot see it; the widened bare pass must,
# and must report the same derived count the path form reports for tools.go.
restore_all
must_apply "the file name bare in the metadata description" \
  mutate "$PAGE" 'the tool registry in the api/internal/mcpsrv package.' \
                 'the tool registry in tools.go.' \
  && check "a single file named as the registry, BARE (tools.go) -> claim" RED "$C7A"

# 7d: the legitimate state the card allows — the file name dropped entirely. An
# extraction that reds this has over-reached: "the tool registry" claims nothing
# about one file.
restore_all
must_apply "the file name dropped entirely" \
  mutate "$PAGE" 'the tool registry in the api/internal/mcpsrv package.' \
                 'the tool registry.' \
  && check "no file named at all -> claim (legitimate state, must stay GREEN)" GREEN ""

# 7e: prose that happens to carry a `.go` token which is NOT a source file of
# this package (the widened bare pass reads only names the per-file literal map
# knows, so this is not a claim and must stay GREEN). Without this control the
# widen could red every `.go` string on the page and still pass 7c.
restore_all
must_apply "a bare .go token that is not a source file of the package" \
  mutate "$PAGE" 'the tool registry in the api/internal/mcpsrv package.' \
                 'the tool registry in the api/internal/mcpsrv package, never a stale mirror such as tools_snapshot.go.' \
  && check "a non-package .go token in prose -> claim (must stay GREEN)" GREEN ""

restore_all

# ── CASE 1 (LAST — see the note above the mutation cases): the DEFECT as it
# shipped — the real pre-fix page, the strongest case in this file ────────────
#
# `refuse` rather than `exit 2`: on a tree where this case cannot run, the run
# must still report the cases that DID run, and end with the refusal as its
# verdict. `exit 2` here would be the same silent loss the note above describes.
refuse() { echo "$1" >&2; exit 2; }
BASE=$(git merge-base HEAD origin/main 2>/dev/null || true)
if [ -z "$BASE" ]; then
  refuse "FAIL  cannot resolve a merge-base with origin/main — case 1 is the point of this file"
fi
DELTA=$(git diff --numstat "$BASE" HEAD -- "$PAGE" | awk '{print $1" insertions, "0+$2" deletions"}')
git show "${BASE}:${PAGE_IN_REPO}" > "$PAGE" || refuse "FAIL  cannot read $PAGE at $BASE"
BASE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)
if [ "$BASE_SHA" = "$PAGE_SHA" ]; then
  refuse "FAIL  the page at ${BASE:0:8} is byte-identical to this tree's — case 1 would prove nothing. Every mutation case above DID run; this one cannot, and is reported rather than reported green."
fi
# Both numbers are measured, and the omission count is the DIFFERENCE between
# them, so a registry that grows (or a page that gains a row) moves the needle
# with it instead of invalidating the case.
REG=$(reg_tools "$TOOLS")
OMITTED=$(omitted_count "$PAGE" "$TOOLS")
if ! [[ "$REG" =~ ^[0-9]+$ ]] || ! [[ "$OMITTED" =~ ^[0-9]+$ ]]; then
  refuse "FAIL  cannot count the registry / the omissions for case 1 (registry='$REG' omitted='$OMITTED')"
fi
if [ "$OMITTED" -lt 1 ]; then
  refuse "FAIL  the pre-fix page at ${BASE:0:8} has a row for every one of $REG registered tool(s) — case 1 would prove nothing"
fi
NEEDLE="omits ${OMITTED} registered tool(s)"
# The ten tools the card is actually about (the pre-#753 set) — the substantive
# claim, so it is asserted by NAME. `retry_feedback_notify` is NOT one of them:
# #753 registered it after this page was written, so it is the eleventh omission
# and the count above already carries it. Only names that are really registered
# AND really absent from this base page are asserted, so a future rebase cannot
# leave a stale name in the list.
TEN_MISSING="submit_feedback list_feedback get_feedback_thread reply_feedback set_feedback_status list_affiliates create_affiliate get_affiliate_sales set_affiliate_commission set_domain_custom_cert"
NEEDLES=("$NEEDLE")
for t in $TEN_MISSING; do
  if grep -qE -- "Name:[[:space:]]*\"$t\"" "$TOOLS" && ! grep -qF -- "name: \"$t\"" "$PAGE"; then
    NEEDLES+=("$t")
  else
    refuse "FAIL  case 1 expects $t to be registered but missing from the page at ${BASE:0:8}; it is not — the needle list has gone stale"
  fi
done
echo "CASE 1 pre-fix page from ${BASE:0:8} (${DELTA:-no delta}) sha ${BASE_SHA:0:12} registry=$REG → omits $OMITTED (derived from the two files)"
check_needles "guard vs the REAL pre-fix page" RED "${NEEDLES[@]}"
restore_all

echo
if [ "$fail" -eq 0 ]; then
  echo "BITEPROOF PASS — every case above left the guard RED for its own reason."
  echo "Page and registry restored byte-identically (sha256 re-checked)."
else
  echo "BITEPROOF FAIL — see the FAIL lines above." >&2
fi
exit "$fail"
