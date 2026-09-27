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
cd "$(dirname "$0")/.." || exit 2          # -> web/

PAGE=src/app/docs/mcp/page.tsx
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

# run_guard -> "GREEN" or "RED"; output kept in /tmp/bite.inv.out
# NOTE: the guard EXITS NON-ZERO when it fails, so capture the output first.
# A `node --test … | grep -q` pipe under `set -o pipefail` reads a real match as
# a failure — that trap produced a false verdict on this repo once already.
run_guard() {
  local out
  out=$(node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON "$GUARD" 2>&1) || true
  if grep -qE '^ℹ fail [1-9]' <<<"$out"; then printf 'RED'; else printf 'GREEN'; fi
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

# ── CASE 1: the DEFECT as it shipped — the real pre-fix page ──────────────────
BASE=$(git merge-base HEAD origin/main 2>/dev/null || true)
if [ -z "$BASE" ]; then
  echo "FAIL  cannot resolve a merge-base with origin/main — case 1 is the point of this file" >&2
  exit 2
fi
DELTA=$(git diff --numstat "$BASE" HEAD -- "$PAGE" | awk '{print $1" insertions, "$2" deletions"}')
git show "${BASE}:${PAGE_IN_REPO}" > "$PAGE" || { echo "FAIL  cannot read $PAGE at $BASE" >&2; exit 2; }
BASE_SHA=$(sha256sum "$PAGE" | cut -d' ' -f1)
if [ "$BASE_SHA" = "$PAGE_SHA" ]; then
  echo "FAIL  the page at ${BASE:0:8} is byte-identical to this tree's — case 1 would prove nothing" >&2
  exit 2
fi
# Both numbers are measured, and the omission count is the DIFFERENCE between
# them, so a registry that grows (or a page that gains a row) moves the needle
# with it instead of invalidating the case.
REG=$(reg_tools "$TOOLS")
OMITTED=$(omitted_count "$PAGE" "$TOOLS")
if ! [[ "$REG" =~ ^[0-9]+$ ]] || ! [[ "$OMITTED" =~ ^[0-9]+$ ]]; then
  echo "FAIL  cannot count the registry / the omissions for case 1 (registry='$REG' omitted='$OMITTED')" >&2
  exit 2
fi
if [ "$OMITTED" -lt 1 ]; then
  echo "FAIL  the pre-fix page at ${BASE:0:8} has a row for every one of $REG registered tool(s) — case 1 would prove nothing" >&2
  exit 2
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
    echo "FAIL  case 1 expects $t to be registered but missing from the page at ${BASE:0:8}; it is not — the needle list has gone stale" >&2
    exit 2
  fi
done
echo "CASE 1 pre-fix page from ${BASE:0:8} (${DELTA:-no delta}) sha ${BASE_SHA:0:12} registry=$REG → omits $OMITTED (derived from the two files)"
check_needles "guard vs the REAL pre-fix page" RED "${NEEDLES[@]}"
restore_all
echo

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

restore_all

echo
if [ "$fail" -eq 0 ]; then
  echo "BITEPROOF PASS — every case above left the guard RED for its own reason."
  echo "Page and registry restored byte-identically (sha256 re-checked)."
else
  echo "BITEPROOF FAIL — see the FAIL lines above." >&2
fi
exit "$fail"
