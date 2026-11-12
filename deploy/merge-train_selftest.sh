#!/usr/bin/env bash
# Bite proof for deploy/merge-train.sh.
#
# Proves all four behaviours on throwaway refs in a local clone (never the real repo,
# never the shared gate record store):
#   A. a clean batch            -> exit 0, candidate tree printed, tree != base
#   B. a batch with one bad ref -> exit 1 and BISECT names THAT ref (attribution works)
#   C. a batch whose refs clash -> exit 3 (conflict = eject, never a silent merge)
#   D. a batch that adds nothing-> exit 0, "identical to $BASE"
#
# The scope decisions, which is where the merge cost lives:
#   G. a non-sensitive batch is gated DIFF-MAPPED (--for-diff), and says so.
#   H. a batch that moves api/ is gated at the AFFECTED PACKAGE SET: preflight is
#      handed PREFLIGHT_TEST_PACKAGES, that set CONTAINS a package the changed one
#      does not import but which imports it (internal/httpapi) and OMITS a package
#      with no path to it (internal/i18n), and the verdict says `go test ./...` for
#      the rest was NEVER run. N/N package count is asserted too.
#   H2. an api/ change that cannot be mapped to packages (a non-Go file) escalates to
#      the FULL inventory instead of a narrowed run with a hole in it.
#   H2b. a DATA-ONLY batch (no api/ path) keeps `test` but hands preflight the PACKAGES
#      whose tests READ the changed data, names the scope, and says the rest of
#      `go test ./...` was never run. The set is DERIVED by preflight (--test-readers)
#      and its membership is recomputed here from the tree, not from a list.
#   H3. a documentation asset whose NAME carries a payment keyword is NOT an
#      escalation — PR #1701's `04-chip-kunci.png` turned an 11-gate diff-mapped run
#      into the 27-gate inventory (13 min) on that alone.
#   H4. the CONTROL: the same keyword on a CODE path still escalates.
#   H5. the OTHER CONTROL: the same keyword on a CODE path that happens to live
#      under a `docs/` DIRECTORY still escalates — the inert class is a file
#      extension, never a directory name (`web/src/app/docs/**` is 50 `.ts/.tsx`
#      source files, and H4's path has no `docs/` component to catch this).
#   I. --full still forces the inventory.
#   N. --select-test-packages prints that set for <base>..HEAD (the same code path,
#      no worktree, no gate).
#
# The gate's CPU budget, and the attribution it feeds (card t_d1bc5164):
#   K.  a red batch of ONE names that ref and never says "combination" — the old label named
#       no culprit at all (two train reds on an unchanged tree were really a11y CPU
#       starvation at the old 300% cap) — and a red under a sub-400% cap is flagged as
#       possible contention, while a red at 400% is not.
#   P.  the quota the gate ACTUALLY receives is CPUQuota=400% (read off the argv a
#       `systemd-run` stub is handed), and TRAIN_CPU_QUOTA still overrides it.
#
# The guard rails on the seam:
#   Q. preflight REFUSES a package list with no --for-diff, and refuses a token that
#      is not a package path (a leading '-' would become a `go test` flag); the train
#      REFUSES an inherited PREFLIGHT_TEST_PACKAGES instead of adopting it silently.
#
# And the author-identity guard, on local commits only (no network, no landing):
#   L. a range holding a foreign author -> exit 6, AUTHOR MISMATCH naming that address; the
#      repo identity -> exit 0 with the count it read; an unreadable range -> exit 2.
#   M. --land with a personal/corporate or empty identity -> exit 2 BEFORE the gate is paid
#      for, because a landing may only ever carry a *@users.noreply.github.com identity.
#
# The land token (--land serialises only the base-read→push window):
#   R. free -> the run proceeds past the token to base resolution; held -> exit 7 (LAND-BUSY)
#      with "nothing was gated or landed" and NO base read (the token is taken FIRST); a lane
#      that arrives while it is held WAITS and then proceeds; a verify-only run is never
#      blocked by it.
#   R2. the token is NOT inherited by the train's children: with a long-running gate stub the
#      token is HELD while the train lives and FREE the instant the train is killed — even
#      though the stub is still running. A token carried by an inherited descriptor (the
#      `exec 9>>` shape) outlives the train, and the next --land lane waits LAND_WAIT for a
#      train that is already gone.
#
# The gate is narrowed to `gofmt` (2 s) because this proves the TRAIN's decision
# logic, not gate coverage. A batch may only LAND on a full green — the script
# refuses `--land --only`.
set -o pipefail
PROG=${0##*/}
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(git -C "$HERE" rev-parse --show-toplevel)
S=${SELF_TEST_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/merge-train-selftest.XXXXXX")}
CLONE=$S/repo
pass=0; fail=0
# A run clones the repo and builds candidate worktrees: ~300 MB. Leaving that behind in
# TMPDIR per run is a leak, not a leftover — 9 sandboxes from one afternoon were 2.7 GB.
# A GREEN run cleans up; a RED one is kept (its logs and its clone are the evidence you
# need), and SELF_TEST_DIR=… / SELF_TEST_KEEP=1 keep it too.
if [ -z "${SELF_TEST_DIR:-}" ] && [ -z "${SELF_TEST_KEEP:-}" ]; then
  trap '[ "$_selftest_keep" = 1 ] && { echo "$PROG: sandbox kept for inspection: $S" >&2; exit 1; }; rm -rf "$S"' EXIT
fi
_selftest_keep=0
ok()   { printf '  ok   %s\n' "$*"; pass=$((pass+1)); }
bad()  { printf '  FAIL %s\n' "$*"; fail=$((fail+1)); }

git clone -q --no-hardlinks "$ROOT" "$CLONE" || { echo "$PROG: clone failed" >&2; exit 1; }
cd "$CLONE" || exit 1
# The clone carries the COMMITTED tree, so the script under test (which may be
# uncommitted in the developer's worktree) has to be copied in for the cases that RUN
# it — case Q drives preflight's own refusals. The copy is taken RIGHT BEFORE those
# cases, not here: fixture building commits with `git add -A`, which would sweep a dirty
# copy into a fixture branch, and the next `git checkout -B` would then restore the
# COMMITTED preflight — every real-preflight case would silently test the previous
# version (measured: it did, and case Q asserted the OLD refusal text). The train under
# test is always read from $HERE, never from the clone.
# The identity the train demands, READ FROM THE TRAIN so this file cannot drift from the default
# it is testing — and not a personal address: this file ships with the repo.
IDENTITY=$(sed -n 's/^MERGE_AUTHOR_EMAIL=${MERGE_AUTHOR_EMAIL-\(.*\)}$/\1/p' "$HERE/merge-train.sh")
[ -n "$IDENTITY" ] || {
  echo "$PROG: cannot read the MERGE_AUTHOR_EMAIL default from merge-train.sh" >&2; exit 1; }
git config user.email "$IDENTITY"
git config user.name  "Wan Zulkarnain"
# The gate must not touch the shared record store or the host lock.
export PREFLIGHT_RESULT_DIR=$S/gate PREFLIGHT_LOCK_FILE=$S/gate.lock PREFLIGHT_LOCK_WAIT=60
mkdir -p "$PREFLIGHT_RESULT_DIR"
BASE=$(git rev-parse HEAD)

# --- fixtures -----------------------------------------------------------------
# good: a doc-only change. bad: a .go file gofmt will red. clash: same line as good.
mk() { git checkout -q -B "$1" "$BASE"; }

mk train-good1
printf 'train selftest one %s\n' "$RANDOM" > selftest_good1.txt
git add -A && git commit -q -m "selftest: harmless change one"

mk train-bad
printf 'package version\n\nfunc  Unformatted( )  {   }\n' > api/internal/version/zz_train_selftest.go
git add -A && git commit -q -m "selftest: breaks gofmt"

mk train-good2
printf 'train selftest two %s\n' "$RANDOM" > selftest_good2.txt
git add -A && git commit -q -m "selftest: harmless change two"

mk train-clash
printf 'CONFLICT MARKER A\n' > selftest_clash.txt
git add -A && git commit -q -m "selftest: clash A"

mk train-clash2
git checkout -q -B train-clash2 "$BASE"
printf 'CONFLICT MARKER B\n' > selftest_clash.txt
git add -A && git commit -q -m "selftest: clash B"

# A change in ONE package that many other packages import (directly, or through
# their TEST imports) — the fixture the affected-package-set cases are built on.
# Comment-only and syntactically clean, so `go list ./...` succeeds: the selector
# must succeed on purpose, not by accident of a broken tree.
mk train-payments
printf 'package payments\n\n// selftest marker %s: a change in a package others import\n' "$RANDOM" \
  > api/internal/payments/zz_train_selftest.go
git add -A && git commit -q -m "selftest: touches internal/payments"

# An api/ change with NO package of its own: a package graph cannot answer for it,
# so the train must escalate to the full inventory rather than guess a narrow set.
mk train-api-notes
printf 'selftest notes, not a Go source file\n' > api/internal/payments/zz_train_notes.md
git add -A && git commit -q -m "selftest: a non-Go change under api/"

# A DOCUMENTATION/ASSET path whose NAME carries a payment keyword. The keyword arm
# of the escalation exists to catch CODE with that blast radius; a screenshot named
# after the rail is DATA. Escalating on it made a docs-only batch pay the whole
# 27-gate inventory — PR #1701 (20 PNGs + one docs page, no Go file) took 13 min
# because `04-chip-kunci.png` matched "chip".
mk train-docs-chip
mkdir -p docs/guides/surau-donation
printf 'not a real png\n' > docs/guides/surau-donation/04-chip-kunci.png
git add -A && git commit -q -m "selftest: a documentation screenshot named after the payment rail"

# The CONTROL for that split: the same keyword on a CODE path must still escalate.
# The fix is "tell documentation from code", never "switch the valve off".
mk train-session-page
mkdir -p web/src/app/session
printf 'export default function Page() { return null }\n' > web/src/app/session/page.tsx
git add -A && git commit -q -m "selftest: a session page in the web app"

# The OTHER control: a CODE path that carries a `docs/` DIRECTORY component. The inert
# class used to be "anything under a directory called docs", so the 50 `.tsx` files
# under `web/src/app/docs/` were marked inert — and this page's name carries "oauth".
# H4 cannot catch that shape: `web/src/app/session/page.tsx` has no `docs/` component.
mk train-web-docs-page
mkdir -p web/src/app/docs/mcp/oauth
printf 'export default function Page() { return null }\n' > web/src/app/docs/mcp/oauth/page.tsx
git add -A && git commit -q -m "selftest: an oauth page under the web app's docs directory"

# A DATA-ONLY change (card t_bf9ce4e2): no api/ path, so `select_test_packages` has
# nothing to map — yet `test` is a real dependency, because api *_test.go files walk
# web/src and one of them walks the whole tree. The train must hand preflight the
# READER packages derived by `preflight.sh --test-readers`, and the verdict must say
# the rest of `go test ./...` was never run.
# The path carries no ESCALATE_RE keyword on purpose: it must reach the diff-mapped
# branch, not the escalated one.
mk train-docs
mkdir -p web/src/app/zz_selftest
printf 'docs-only fixture: no api/ path, no gate keyword\n' > web/src/app/zz_selftest/probe.txt
git add -A && git commit -q -m "selftest: a data-only change under web/src"

run() { local t rc; t=$(mktemp); "$HERE/merge-train.sh" "$@" >"$t" 2>&1; rc=$?; cat "$t"; return $rc; }

echo "== A. clean batch (one ref)"
OUT=$(run --base "$BASE" --only gofmt train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 on a clean batch" || bad "expected exit 0, got $RC"
grep -q '^merge-train\.sh: GREEN candidate tree ' <<<"$OUT" && ok "prints GREEN candidate tree" || bad "no GREEN line"
CTREE=$(sed -n 's/^merge-train\.sh: GREEN candidate tree \([0-9a-f]*\).*/\1/p' <<<"$OUT")
[ -n "$CTREE" ] && [ "$CTREE" != "$(git rev-parse "$BASE^{tree}")" ] && ok "candidate tree differs from base" || bad "candidate tree == base"

echo "== B. one bad ref in a batch of three (attribution)"
OUT=$(run --base "$BASE" --only gofmt train-good1 train-bad train-good2); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "exit 1 when the batch is red" || bad "expected exit 1, got $RC"
grep -q '^merge-train\.sh: BISECT=train-bad$' <<<"$OUT" && ok "BISECT names train-bad (not the whole batch)" || bad "BISECT did not name train-bad"

echo "== C. conflicting refs"
OUT=$(run --base "$BASE" --only gofmt train-clash train-clash2); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 3 ] && ok "exit 3 on conflict (eject, never silent)" || bad "expected exit 3, got $RC"
grep -q '^merge-train\.sh: CONFLICT train-clash2' <<<"$OUT" && ok "names the conflicting ref" || bad "conflict ref not named"

echo "== D. an empty batch (ref already in base)"
OUT=$(run --base "$BASE" --only gofmt "$BASE"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the candidate equals base" || bad "expected exit 0, got $RC"
grep -q 'identical to' <<<"$OUT" && ok "says nothing to test" || bad "no 'identical to' line"

echo "== E. --land refuses a narrowed selection"
OUT=$(run --base "$BASE" --only gofmt --land 1:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "a batch can only land on a FULL green" || bad "expected exit 2, got $RC"
grep -q 'refuses a narrowed selection' <<<"$OUT" && ok "says why" || bad "no refusal message"

echo "== F. --land splits <pr>:<ref> before the build"
OUT=$(run --base "$BASE" --land 1:no-such-ref-anywhere); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "exit 2 for an unresolvable ref" || bad "expected exit 2, got $RC"
grep -q 'ref not found: no-such-ref-anywhere$' <<<"$OUT" && ok "the PR prefix was stripped before rev-parse (it would read '1:no-such-...' otherwise)" || bad "pr: prefix not split — the build saw the whole entry"

echo "== G. the batch is gated DIFF-MAPPED by default (not the full inventory)"
# A stub gate records the argv the train would have used, so this asserts the SCOPE
# decision without paying ~72 min for a real full inventory.
STUB="$S/gate-stub.sh"
# heredoc, not printf: a printf FORMAT containing %s silently ate the stub's own %s and
# made every argv assertion below vacuously true (the stub printed an empty line).
cat > "$STUB" <<'STUBEOF'
#!/usr/bin/env bash
printf 'STUB-ARGV: %s\n' "$*"
# The set arrives as one import path per line; flatten it so it is ONE line the
# caller can read back (the train prints only the tail of its gate log).
# shellcheck disable=SC2086
printf 'STUB-PKGS: %s\n' "$(echo ${PREFLIGHT_TEST_PACKAGES:-<unset>})"
STUBEOF
chmod +x "$STUB"
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 with a green stub gate" || bad "expected exit 0, got $RC"
grep -q "diff-mapped vs $(git rev-parse "$BASE")" <<<"$OUT" && ok "says it is diff-mapped against the base it lands on" || bad "no diff-mapped line"
grep -q -- "--for-diff $(git rev-parse "$BASE")" <<<"$OUT" && ok "preflight was handed --for-diff <base>" || bad "preflight did not get --for-diff"
grep -q 'NOT full coverage' <<<"$OUT" && ok "the verdict refuses to read as full coverage" || bad "verdict did not disclaim full coverage"

echo "== H. a batch that moves api/ is gated at the AFFECTED PACKAGE SET, not ./..."
# The cost fix this guards: an api-touching batch used to pay `go test ./...` at merge
# time, so merge throughput was 1 PR per full suite. It now runs the changed packages
# plus everything that imports them — so the set must contain the importer and must
# not contain a package with no path to the change.
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-payments); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the api-scoped run is green" || bad "expected exit 0, got $RC"
grep -q 'api-scoped' <<<"$OUT" && ok "names the scope it chose" || bad "no api-scoped line"
grep -q -- "--for-diff $(git rev-parse "$BASE")" <<<"$OUT" \
  && ok "the gate is still DIFF-MAPPED (package scoping rides the diff, never replaces it)" \
  || bad "the api-scoped run did not pass --for-diff"
PKGS_SEEN=$(sed -n 's/^ *| STUB-PKGS: //p' <<<"$OUT" | sed -n '1p')
[ -n "$PKGS_SEEN" ] && [ "$PKGS_SEEN" != "<unset>" ] \
  && ok "preflight was handed a package list (PREFLIGHT_TEST_PACKAGES)" \
  || bad "preflight got no package list — it would have run ./..."
grep -q 'wzul/jomform/api/internal/httpapi' <<<"$(tr ' ' '\n' <<<"$PKGS_SEEN")" \
  && ok "the set CONTAINS internal/httpapi (it imports internal/payments; the changed package does not import it)" \
  || bad "the set omits internal/httpapi — an importer that can fail on the change would not be tested"
grep -q 'wzul/jomform/api/internal/i18n' <<<"$(tr ' ' '\n' <<<"$PKGS_SEEN")" \
  && bad "the set contains internal/i18n, which has no path to internal/payments — the narrowing bought nothing" \
  || ok "the set OMITS internal/i18n (no import path to the change)"
grep -qE 'api-scoped: changed\+importers, [0-9]+/[0-9]+ packages' <<<"$OUT" \
  && ok "the verdict states the real scope as N/M packages" \
  || bad "verdict does not state the package counts"
grep -q 'NEVER ran: go test ./...' <<<"$OUT" \
  && ok "the verdict says the untouched packages were never tested" \
  || bad "verdict hides that ./... was not run"

echo "== H2. an api/ change that cannot be mapped to packages escalates to the FULL inventory"
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-api-notes); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the escalated run is green" || bad "expected exit 0, got $RC"
grep -q 'cannot map' <<<"$OUT" && ok "says why it could not narrow" || bad "no 'cannot map' line"
grep -q -- '--for-diff' <<<"$OUT" && bad "unmappable api change still passed --for-diff (blind run)" \
  || ok "no --for-diff: the full inventory runs"
grep -q 'STUB-PKGS: <unset>' <<<"$OUT" && ok "no package list was handed to preflight" \
  || bad "a package list was passed for a change with no package to map"
grep -q 'full inventory' <<<"$OUT" && ok "verdict says full inventory" || bad "verdict did not say full inventory"

echo "== H2b. a DATA-ONLY batch hands preflight the READER packages, not ./..."
# The cost fix this guards (card t_bf9ce4e2): with no api/ path in the diff,
# select_test_packages has nothing to map, so the batch used to pay `go test ./...`
# for the whole module — measured 551 s, and 710 s on a later write of the same
# tree — for a docs/guide diff whose only Go dependency is a handful of tests that
# READ the changed data. The set must be DERIVED (preflight --test-readers); the
# membership asserted below is recomputed from the tree, not from a list.
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-docs); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the readers-scoped run is green" || bad "expected exit 0, got $RC"
grep -q 'readers-scoped' <<<"$OUT" && ok "names the scope it chose (readers-scoped)" || bad "no readers-scoped line"
grep -q -- "--for-diff $(git rev-parse "$BASE")" <<<"$OUT" \
  && ok "the gate is still DIFF-MAPPED (package scoping rides the diff, never replaces it)" \
  || bad "the readers-scoped run did not pass --for-diff"
PKGS_SEEN=$(sed -n 's/^ *| STUB-PKGS: //p' <<<"$OUT" | sed -n '1p')
[ -n "$PKGS_SEEN" ] && [ "$PKGS_SEEN" != "<unset>" ] \
  && ok "preflight was handed a package list (PREFLIGHT_TEST_PACKAGES)" \
  || bad "preflight got no package list — it would have run ./..."
PKGS_ONE=$(tr ' ' '\n' <<<"$PKGS_SEEN")
# Every test that reads the DIRECTORY web/src is a reader of this fixture's path,
# recomputed here from the sources (the two-string Join shape, which is how the
# real readers spell it) — not from the package list the runner printed.
WEB_SRC_DIRS=$(cd "$CLONE" && grep -rlE --include='*_test.go' '"web",[[:space:]]*"src"' api 2>/dev/null \
  | xargs -rn1 dirname | sed 's|^api/|./|' | sort -u)
if [ -z "$WEB_SRC_DIRS" ]; then
  bad "cannot check the readers set: no api *_test.go names the web/src directory, so the assertion would be vacuous"
else
  _missing=""
  for _d in $WEB_SRC_DIRS; do grep -qx "$_d" <<<"$PKGS_ONE" || _missing="${_missing}${_d} "; done
  [ -z "$_missing" ] \
    && ok "the set CONTAINS every package whose test reads web/src: [$(printf '%s ' $WEB_SRC_DIRS)]" \
    || bad "the set omits reader package(s): [${_missing% }] — their guards would not run on this change"
fi
# And the package that walks the WHOLE TREE: it reads the changed file whatever it
# is, so it must run for a diff that touches no api/ path at all.
TREE_DIRS=$(cd "$CLONE" && grep -rlE --include='*_test.go' \
  'filepath\.Join\(filepath\.Dir\([^)]*\),[[:space:]]*"\.\."[[:space:]]*,[[:space:]]*"\.\."[[:space:]]*,[[:space:]]*"\.\."\)' \
  api 2>/dev/null | xargs -rn1 dirname | sed 's|^api/|./|' | sort -u)
if [ -z "$TREE_DIRS" ]; then
  bad "cannot check the tree-walking reader: no api *_test.go resolves the repo root from its own directory, so the assertion would be vacuous"
else
  _missing=""
  for _d in $TREE_DIRS; do grep -qx "$_d" <<<"$PKGS_ONE" || _missing="${_missing}${_d} "; done
  [ -z "$_missing" ] \
    && ok "the set CONTAINS every package whose test resolves the REPO ROOT: [$(printf '%s ' $TREE_DIRS)] — a docs change can break those" \
    || bad "the set omits tree-walking reader(s): [${_missing% }] — they read every file, so this diff is an input of theirs"
fi
# A package with NO test file cannot read anything and must not be run.
NOTEST_PKG=$(cd "$CLONE" && for _d in api/internal/*/; do
    ls "$_d"*_test.go >/dev/null 2>&1 || { printf './%s' "${_d#api/}"; break; }
  done)
NOTEST_PKG="${NOTEST_PKG%/}"
if [ -z "$NOTEST_PKG" ]; then
  bad "cannot find a package without test files to prove the set is not just 'everything'"
elif grep -qx "$NOTEST_PKG" <<<"$PKGS_ONE"; then
  bad "the set contains $NOTEST_PKG, which has no test file and cannot read the change — the narrowing bought nothing"
else
  ok "the set OMITS $NOTEST_PKG (no test file: it reads nothing)"
fi
grep -qE 'readers-scoped: the [0-9]+/[0-9]+ api package' <<<"$OUT" \
  && ok "the verdict states the real scope as N/M packages" \
  || bad "verdict does not state the package counts"
grep -q 'NEVER ran: go test ./...' <<<"$OUT" \
  && ok "the verdict says the untouched packages were never tested" \
  || bad "verdict hides that ./... was not run"

echo "== H3. a documentation asset named after a payment rail is NOT an escalation"
# PR #1701 measured this: `docs/guides/surau-donation/04-chip-kunci.png` matched the
# "chip" keyword, so a batch that touched no Go, deploy or auth file paid the whole
# 27-gate inventory (632 s of Go gates, both e2e halves) instead of the 11 gates its
# diff maps to. A docs or binary asset is DATA, and `--for-diff` already widens on a
# path no gate claims, so keeping the keyword arm out of those paths loses nothing.
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-docs-chip); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the doc-asset run is green" || bad "expected exit 0, got $RC"
grep -q "diff-mapped vs $(git rev-parse "$BASE")" <<<"$OUT" \
  && ok "a docs PNG whose NAME contains 'chip' is gated DIFF-MAPPED, not escalated" \
  || bad "a documentation asset still escalated the whole inventory on a keyword in its filename"
grep -q -- "--for-diff $(git rev-parse "$BASE")" <<<"$OUT" && ok "preflight was handed --for-diff" || bad "preflight did not get --for-diff"

echo "== H4. the SAME keyword on a CODE path still escalates to the FULL inventory"
# The control for H3: the fix separates documentation from code, it does not switch
# the valve off. `web/src/app/session/page.tsx` is a real auth surface, and the Go
# package graph says nothing about it.
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-session-page); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the escalated run is green" || bad "expected exit 0, got $RC"
grep -q 'escalated' <<<"$OUT" && ok "a session path under web/src still escalates (the valve still fires)" || bad "a sensitive CODE path stopped escalating — the split went too far"
grep -q -- '--for-diff' <<<"$OUT" && bad "an escalated path still passed --for-diff" || ok "no --for-diff on the escalated run"

echo "== H5. a CODE path under a docs/ DIRECTORY still escalates (inert is an EXTENSION, not a directory)"
# The shape H4 could not reach: `web/src/app/docs/**` is 50 `.ts/.tsx` SOURCE files, and
# the old `(^|/)docs/` arm marked every one of them inert. `.../docs/mcp/oauth/page.tsx`
# carries "oauth", so a directory name must not buy it an escalation waiver.
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-web-docs-page); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when the escalated run is green" || bad "expected exit 0, got $RC"
grep -q 'escalated (web/src/app/docs/mcp/oauth/page.tsx' <<<"$OUT" \
  && ok "a code path under web/src/app/docs escalates again, and the verdict NAMES that path" \
  || bad "a docs/ DIRECTORY component marked CODE inert — the keyword arm was waived by the path"
grep -q -- '--for-diff' <<<"$OUT" && bad "an escalated docs-directory code path still passed --for-diff" \
  || ok "no --for-diff on the escalated run (the full inventory runs)"

echo "== I. --full forces the inventory even when the diff would allow a mapped run"
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" --full train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0" || bad "expected exit 0, got $RC"
grep -q -- '--for-diff' <<<"$OUT" && bad "--full still passed --for-diff" || ok "--full passes no --for-diff"

echo "== J. a batch that reds on a BASE that is already red is not attributed to the batch"
# The record store is keyed on the tree, so a recorded RED for the base tree is proof the
# batch did not cause it. Without this guard the prefix bisect names the first ref — a
# rework card for innocent work.
REDSTUB="$S/gate-stub-red.sh"
cat > "$REDSTUB" <<'STUBEOF'
#!/usr/bin/env bash
echo "STUB: this gate is red on purpose"
exit 1
STUBEOF
chmod +x "$REDSTUB"
BASE_TREE=$(git rev-parse "$BASE^{tree}")
python3 - "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json" <<'PY'
import json, sys
json.dump({"format": 1, "tree": sys.argv[1].split("/")[-1][:-5], "passed": 0, "failed": 1,
           "skipped": 0, "gates": {"prunewt": {"status": "fail"}, "gofmt": {"status": "pass"}}},
          open(sys.argv[1], "w"))
PY
OUT=$(TRAIN_GATE_CMD="bash $REDSTUB" run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 5 ] && ok "exit 5 (base red) rather than 1 (batch red)" || bad "expected exit 5, got $RC"
grep -q 'BASE-RED .*prunewt' <<<"$OUT" && ok "names the base and the gate that was already failing" || bad "base red not named with its gate"
grep -q 'BISECT=' <<<"$OUT" && bad "bisect ran anyway — an innocent ref would be blamed" || ok "no BISECT line: nothing was attributed to the batch"
rm -f "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json"

echo "== J2. a RED base is never gated at a narrowed package set"
# The scoped mode must not be able to hide a red base: if the store already says the
# base tree fails a gate, a batch that narrows past that package would report GREEN on
# a tree main already fails. It pays the inventory instead.
python3 - "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json" <<'PY'
import json, sys
json.dump({"format": 1, "tree": sys.argv[1].split("/")[-1][:-5], "passed": 0, "failed": 1,
           "skipped": 0, "gates": {"test": {"status": "fail"}, "gofmt": {"status": "pass"}}},
          open(sys.argv[1], "w"))
PY
OUT=$(TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-payments); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 (the stub is green)" || bad "expected exit 0, got $RC"
grep -q 'already red' <<<"$OUT" && ok "names the red base that blocked the narrowing" || bad "no red-base escalation"
grep -q 'STUB-PKGS: <unset>' <<<"$OUT" && ok "no package list was handed to preflight" \
  || bad "it narrowed even though the base tree was already red"
grep -q -- '--for-diff' <<<"$OUT" && bad "a narrowed run was still used on a red base" \
  || ok "the full inventory ran instead"
rm -f "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json"

echo "== K. with no recorded red for the base, a red batch still names its culprit"
OUT=$(TRAIN_GATE_CMD="bash $REDSTUB" run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "exit 1 when the base has no recorded red" || bad "expected exit 1, got $RC"
# THE DEFECT THIS GUARDS (card t_d1bc5164): a batch of ONE used to fall through the prefix
# bisect (its loop starts at k=1 and never runs for a single ref) and print
# `BISECT=combination (eject any one of: <the only ref>)`. That named NO culprit — it sent a
# rework card after an innocent PR #1722 whose tree was green in isolation — on what was
# really the gate's CPU starvation. One ref can only ever be itself.
grep -qx 'merge-train\.sh: BISECT=train-good1 (the batch holds one ref — there is nothing to bisect)' <<<"$OUT" \
  && ok "names the ONLY ref in a batch of one" \
  || bad "the single ref is not named: [$(grep 'BISECT' <<<"$OUT")]"
grep -qi 'combination' <<<"$OUT" \
  && bad "still calls a batch of one a COMBINATION — the label names no culprit" \
  || ok "no COMBINATION claim for a batch of one"
grep -q 'green on its own, the red was the gate' <<<"$OUT" \
  && ok "says how to tell a red ref from a red gate (re-run the same tree alone)" \
  || bad "no re-run-alone hint"

echo "== K2. a red under a sub-400% cap says it may be CONTENTION; at 400% it does not"
# The same defect from the other side: at 300% the three CONCURRENT browser gates this
# host runs starve, and the red is indistinguishable from a failing suite. A run that
# cannot see the difference must say so instead of blaming the ref.
PBIN="$S/pbin"; mkdir -p "$PBIN"
# A `systemd-run` stub: record the argv the train really builds, then run the gate
# unwrapped (the selftest never needs a real cgroup, and never touches the host's).
cat > "$PBIN/systemd-run" <<'FAKESDR'
#!/bin/sh
{ printf 'SDR-ARGV:'; printf ' %s' "$@"; printf '\n'; } >> "${SDR_LOG:?SDR_LOG unset}"
while [ $# -gt 0 ]; do
  case "$1" in
    --scope|-q) shift ;;
    -p) shift 2 ;;
    *) break ;;
  esac
done
exec "$@"
FAKESDR
chmod +x "$PBIN/systemd-run"
SDRLOG=$S/sdr.log
: > "$SDRLOG"
OUT=$(PATH="$PBIN:$PATH" SDR_LOG="$SDRLOG" TRAIN_CPU_QUOTA=300% TRAIN_GATE_CMD="bash $REDSTUB" \
      run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "exit 1 under a 300% cap (a real red is still a red)" || bad "expected exit 1, got $RC"
grep -q 'CPUQuota=300%' "$SDRLOG" && ok "the cap was really applied to the gate (300%)" \
  || bad "the gate was not wrapped at 300%: [$(cat "$SDRLOG")]"
grep -q 'can be CONTENTION' <<<"$OUT" && ok "warns that a red under the cap may be contention" \
  || bad "no contention warning under a sub-400% cap — the next lane sees only BISECT"
grep -q 'TRAIN_CPU_QUOTA=400%' <<<"$OUT" && ok "says what to re-run with" || bad "warning names no remedy"
OUT=$(PATH="$PBIN:$PATH" SDR_LOG="$SDRLOG" TRAIN_CPU_QUOTA=400% TRAIN_GATE_CMD="bash $REDSTUB" \
      run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "exit 1 at 400% too (the gate is not weakened)" || bad "expected exit 1, got $RC"
grep -q 'can be CONTENTION' <<<"$OUT" \
  && bad "still blames contention at 400%, where the measurement says the browser group fits" \
  || ok "no contention warning at 400% — a red there is a real red"

echo "== P. the gate runs under CPUQuota=400% by default (300% starved the browser group)"
# Asserted where the value is USED, not where it is written: the stub above records the
# argv the train hands systemd-run. Card t_d1bc5164: at 300% the same unchanged tree redded
# a11y in 817-1014 s and PASSED in 287 s alone; 400% is the value that landed it.
: > "$SDRLOG"
OUT=$(PATH="$PBIN:$PATH" SDR_LOG="$SDRLOG" TRAIN_GATE_CMD="bash $STUB" run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 with a green gate under the quota wrapper" || bad "expected exit 0, got $RC"
grep -q 'CPUQuota=400%' "$SDRLOG" \
  && ok "the default the gate actually receives is CPUQuota=400%" \
  || bad "the gate's default quota is not 400%: [$(cat "$SDRLOG")]"
grep -q 'under CPUQuota=400%' <<<"$OUT" && ok "the run states the quota it gated under" \
  || bad "the run does not say which quota it used"
: > "$SDRLOG"
OUT=$(PATH="$PBIN:$PATH" SDR_LOG="$SDRLOG" TRAIN_CPU_QUOTA=500% TRAIN_GATE_CMD="bash $STUB" \
      run --base "$BASE" train-good1); RC=$?
[ $RC -eq 0 ] && ok "exit 0 with the quota overridden" || bad "expected exit 0, got $RC"
grep -q 'CPUQuota=500%' "$SDRLOG" && ok "TRAIN_CPU_QUOTA still overrides the default" \
  || bad "the override was ignored: [$(cat "$SDRLOG")]"

echo "== L. the author-identity guard BITES on a foreign author"
# The defect this guards: every squash on main was authored by the personal gmail because
# `gh pr merge` lets GitHub author the commit from the MERGING ACCOUNT, so a checkout's git
# config never reaches that path. A green landing must therefore also prove WHO wrote it.
mk train-foreign
printf 'train selftest foreign %s\n' "$RANDOM" > selftest_foreign.txt
git add -A
git -c user.name='Somebody Else' -c user.email='somebody@example.com' \
  commit -q --author='Somebody Else <somebody@example.com>' -m "selftest: foreign author"
OUT=$(run --check-authors "$BASE..train-foreign"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 6 ] && ok "exit 6 when a commit is not the repo identity" || bad "expected exit 6, got $RC"
grep -q '^merge-train\.sh: AUTHOR MISMATCH' <<<"$OUT" && ok "says AUTHOR MISMATCH" || bad "no AUTHOR MISMATCH line"
grep -q 'somebody@example\.com' <<<"$OUT" && ok "names the foreign address" || bad "foreign address not named"

OUT=$(run --check-authors "$BASE..train-good1"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 when every commit is the repo identity" || bad "expected exit 0, got $RC"
grep -q "author identity $IDENTITY on all 1 commit(s)" <<<"$OUT" \
  && ok "names the identity and the count it actually read" || bad "identity/count line missing"
OUT=$(run --check-authors no-such-range-anywhere); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "exit 2 rather than certify a range it could not read" || bad "expected exit 2, got $RC"
grep -q 'refusing to certify' <<<"$OUT" && ok "says why" || bad "no refusal message"

echo "== M. --land refuses an identity that is not a GitHub no-reply (before paying for a gate)"
OUT=$(MERGE_AUTHOR_EMAIL=someone@gmail.com run --land 1:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "exit 2 for a personal address" || bad "expected exit 2, got $RC"
grep -q "refuses 'someone@gmail.com'" <<<"$OUT" && ok "names the address it refused" || bad "refused address not named"
grep -q 'must be a \*@users\.noreply\.github\.com identity' <<<"$OUT" && ok "names the rule" || bad "rule not named"
grep -q 'GREEN candidate tree\|landing #' <<<"$OUT" && bad "refused AFTER gating/landing" \
  || ok "refused before any gate or landing happened"
OUT=$(MERGE_AUTHOR_EMAIL= run --land 1:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "exit 2 for an explicitly empty identity" || bad "expected exit 2, got $RC"
grep -q 'refuses an empty MERGE_AUTHOR_EMAIL' <<<"$OUT" && ok "says why an empty one is refused" || bad "empty-identity refusal not explained"
unset MERGE_AUTHOR_EMAIL

echo "== N. --select-test-packages prints the affected set for <base>..HEAD (no gate)"
git checkout -q train-payments
OUT=$(run --select-test-packages "$BASE"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 and prints the set" || bad "expected exit 0, got $RC"
grep -qx 'github.com/wzul/jomform/api/internal/httpapi' <<<"$OUT" \
  && ok "the printed set contains the importer internal/httpapi" \
  || bad "the printed set omits internal/httpapi"
grep -qx 'github.com/wzul/jomform/api/internal/payments' <<<"$OUT" \
  && ok "the printed set contains the CHANGED package itself" \
  || bad "the printed set omits the changed package"
grep -qx 'github.com/wzul/jomform/api/internal/i18n' <<<"$OUT" \
  && bad "the printed set contains an unrelated package (internal/i18n)" \
  || ok "the printed set omits internal/i18n"
N_SEL=$(grep -c '^github.com/wzul/jomform/api/' <<<"$OUT")
N_ALL=$( (cd "$CLONE/api" && go list ./... | wc -l) )
[ "$N_SEL" -gt 0 ] && [ "$N_SEL" -lt "$N_ALL" ] \
  && ok "the set is a strict subset of the module ($N_SEL of $N_ALL packages), so the saving is real" \
  || bad "the selected set is not a subset ($N_SEL of $N_ALL)"
# ... and an unmappable api change prints nothing and exits 1 (the caller escalates).
git checkout -q train-api-notes
OUT=$(run --select-test-packages "$BASE"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "exit 1 when the api/ change has no package to map" || bad "expected exit 1, got $RC"
grep -q 'not a Go source file' <<<"$OUT" && ok "says why it cannot map" || bad "no reason printed"
git checkout -q "$BASE"

echo "== Q. the narrowed-suite seam is REFUSED where it would lie"
# Last moment before anything RUNS preflight: put the tree under test in the clone (see
# the note at the top of this file — an earlier copy would be reverted by the next
# fixture checkout).
cp "$HERE/../scripts/preflight.sh" "$CLONE/scripts/preflight.sh"
# A default run must never be silently narrowed by an inherited variable: this is the
# one shape that could make a RELEASE report './...' while testing eight packages.
OUT=$(cd "$CLONE" && PREFLIGHT_TEST_PACKAGES=./internal/version ./scripts/preflight.sh 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "preflight exits 1 (its own die) for a package list on a default run" || bad "expected exit 1, got $RC"
grep -q 'no narrowing flag' <<<"$OUT" && ok "says why a narrowed suite needs an explicitly narrowed run" || bad "no refusal message"
grep -q 'gate \[' <<<"$OUT" && bad "it ran gates before refusing" || ok "refused before any gate ran"
# A token that would become a FLAG: `go test -run=x` is not a package list.
OUT=$(cd "$CLONE" && PREFLIGHT_TEST_PACKAGES='-run=Evil' ./scripts/preflight.sh --for-diff "$BASE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "preflight exits 1 for a token starting with '-'" || bad "expected exit 1, got $RC"
grep -q "leading '-'" <<<"$OUT" && ok "names the flag-injection shape it refused" || bad "refusal does not name the shape"
# The train does not adopt an inherited list either: it computes its own.
OUT=$(PREFLIGHT_TEST_PACKAGES=./internal/version run --base "$BASE" train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "the train exits 2 when PREFLIGHT_TEST_PACKAGES is already set" || bad "expected exit 2, got $RC"
grep -q 'already set in this environment' <<<"$OUT" && ok "says why it refuses an inherited scope" || bad "no inherited-scope refusal"
# `VAR=x func` leaves VAR set in this shell; do not poison the next reader.
unset PREFLIGHT_TEST_PACKAGES

echo "== Q2. preflight RUNS every package in the list, not just the first line"
# The list arrives one import path per LINE. `read -r -a` stops at the first newline,
# so a three-package list ran ONE package and still labelled the scope "3" — a narrowed
# suite that is narrower than it says. `go` is stubbed so build/vet pass and the `test`
# argv can be read without paying for a suite: this asserts the ARGV, not a verdict.
Q2BIN="$S/q2bin"; mkdir -p "$Q2BIN"
cat > "$Q2BIN/go" <<'FAKEGO'
#!/bin/sh
case "$1" in
  build|vet) exit 0 ;;
  test) echo "FAKE-GO-TEST-ARGV: $*"; exit 1 ;;
  *) exec /usr/local/go/bin/go "$@" ;;
esac
FAKEGO
chmod +x "$Q2BIN/go"
Q2PKGS="github.com/wzul/jomform/api/internal/version
github.com/wzul/jomform/api/internal/i18n
github.com/wzul/jomform/api/internal/payments"
# `--only test` keeps every other gate out of the run (and needs no node_modules), so
# the assertion is about the ARGV `go test` receives, never about a suite passing here.
OUT=$(cd "$CLONE" && PATH="$Q2BIN:$PATH" PREFLIGHT_TEST_PACKAGES="$Q2PKGS" \
      JOMFORM_TEST_DATABASE_URL=postgres://placeholder \
      JOMFORM_TEST_VALKEY_URL=valkey://placeholder \
      ./scripts/preflight.sh --only test 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
ARGV=$(sed -n 's/^FAKE-GO-TEST-ARGV: //p' <<<"$OUT" | sed -n '1p')
[ $RC -eq 1 ] && ok "the stubbed test gate reds the run (as designed)" || bad "expected exit 1, got $RC"
[ "$(grep -o 'internal/[a-z0-9]*' <<<"$ARGV" | wc -l)" -eq 3 ] \
  && ok "go test was handed ALL THREE packages from a newline-separated list" \
  || bad "go test got $(grep -o 'internal/[a-z0-9]*' <<<"$ARGV" | wc -l) package(s) from a 3-line list — a multi-line list is being truncated: [$ARGV]"
grep -q 'SCOPED to 3 package(s)' <<<"$OUT" && ok "the banner states the same count it ran" \
  || bad "the banner count does not match the 3 packages handed over"
# The record stamp is what stops a narrowed pass being REUSED as a full one: the key
# material hashes PREFLIGHT_KEY_EXTRA, so the assertion is on that hash.
EXPECT_EXTRA=$(printf '%s' 'test=scoped' | sha256sum | awk '{print $1}')
if grep -qs "extra=$EXPECT_EXTRA" "$PREFLIGHT_RESULT_DIR"/*.json; then
  ok "the gate record is stamped test=scoped (a narrowed pass cannot be reused as a full one)"
else
  bad "the record carries no test=scoped stamp — a narrowed pass could be reused as evidence for a full run"
fi
grep -q -- '-race -count=1 -timeout' <<<"$ARGV" \
  && ok "the narrowed run is still -race with an explicit -timeout" \
  || bad "the narrowed run lost -race or the timeout budget: [$ARGV]"

echo "== Q3. the narrow-list seam does not leak into the modes the gates themselves call"
# `selftest` asserts the gate table by DRIVING preflight: `--gate-deps` and `--classify`.
# A scoped run exports PREFLIGHT_TEST_PACKAGES, so if the seam's guard fires in those
# modes too, every nested call dies and the selftest gate reds on a run that only meant
# to narrow the api suite (measured: 14 assertions failed exactly that way). Inspection
# modes run no gate, so they must keep working with the list set.
OUT=$(cd "$CLONE" && PREFLIGHT_TEST_PACKAGES="$Q2PKGS" ./scripts/preflight.sh --gate-deps 2>&1); RC=$?
[ $RC -eq 0 ] && ok "--gate-deps still exits 0 with a package list exported" || bad "--gate-deps exited $RC with the list exported: $(printf '%s' "$OUT" | head -2)"
grep -q 'test' <<<"$OUT" && ok "--gate-deps still prints the dependency table" || bad "--gate-deps printed nothing — a scoped run would red its own selftest gate"
OUT=$(cd "$CLONE" && printf 'api/internal/payments/fee.go\n' | PREFLIGHT_TEST_PACKAGES="$Q2PKGS" ./scripts/preflight.sh --classify 2>&1); RC=$?
[ $RC -eq 0 ] && ok "--classify still exits 0 with a package list exported" || bad "--classify exited $RC with the list exported: $(printf '%s' "$OUT" | head -2)"
grep -qx 'test' <<<"$OUT" && ok "--classify still classifies api/ to the test gate" || bad "--classify no longer names test for an api path: [$OUT]"

echo "== R. --land holds the land token: free proceeds, held exits 7 with nothing gated, a lane waits"
LANDTOK=$PREFLIGHT_RESULT_DIR/train-land.lock
# R1: token free -> the run proceeds to base resolution (proves the token is not a wall when
# nobody holds it). The bogus base is deliberate: it fails AFTER the token step.
OUT=$(LAND_WAIT=2 run --base no-such-base --land 999:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "token free: exit 2 at the bogus base, not blocked" || bad "expected exit 2, got $RC"
grep -q 'base ref not found' <<<"$OUT" && ok "token free: reached base resolution" || bad "did not reach base resolution"
grep -q 'LAND-BUSY' <<<"$OUT" && bad "reported LAND-BUSY with no holder" || ok "no false LAND-BUSY"

# R-2: token HELD -> exit 7 and NOTHING past the token (no base read, no gate). The absence of
# the base error is the ordering proof: the token is taken BEFORE the base is resolved.
flock -x "$LANDTOK" -c 'sleep 6' &
HOLDER=$!
sleep 0.3
OUT=$(LAND_WAIT=1 run --base no-such-base --land 999:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 7 ] && ok "token held: exit 7 (LAND-BUSY), not a silent success" || bad "expected exit 7, got $RC"
grep -q 'LAND-BUSY waited 1s' <<<"$OUT" && ok "names the wait it paid" || bad "wait not named"
grep -q 'nothing was gated or landed' <<<"$OUT" && ok "says nothing was gated or landed" || bad "no explicit no-op claim"
grep -q 'base ref not found' <<<"$OUT" \
  && bad "read the base BEFORE taking the token (ordering is wrong)" \
  || ok "never read the base — the token is taken first"

# R-3: a run that arrives WHILE the token is held WAITS, then proceeds — it does not die and it
# does not read a base it cannot land on. This is the behaviour the two merge lanes need.
wait $HOLDER 2>/dev/null
flock -x "$LANDTOK" -c 'sleep 3' &
HOLDER3=$!
sleep 0.3
t0=$(date +%s)
OUT=$(LAND_WAIT=30 run --base no-such-base --land 999:train-good1); RC=$?
t1=$(date +%s)
wait $HOLDER3 2>/dev/null
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "after the holder released, the run proceeded (exit 2 at the bogus base)" \
  || bad "expected exit 2 after release, got $RC"
grep -q 'LAND-BUSY' <<<"$OUT" && bad "gave up at the token instead of waiting it out" \
  || ok "waited the holder out instead of dying"
[ $((t1-t0)) -ge 2 ] && ok "actually waited for the token ($((t1-t0))s)" \
  || bad "did not wait ($((t1-t0))s) — the token is not serialising anything"

# R-4: a VERIFY-ONLY run must not be blocked by the land token (it never pushes, and blocking
# it would serialise work that is safe to parallelise).
flock -x "$LANDTOK" -c 'sleep 5' &
HOLDER2=$!
sleep 0.3
t0=$(date +%s)
OUT=$(TRAIN_GATE_CMD="bash $STUB" LAND_WAIT=1 run --base "$BASE" --only gofmt train-good1); RC=$?
t1=$(date +%s)
printf '%s\n' "$OUT" | sed 's/^/    /'
wait $HOLDER2 2>/dev/null
[ $RC -eq 0 ] && ok "verify-only run ignored the held land token (exit 0)" || bad "expected exit 0, got $RC"
[ $((t1-t0)) -lt 5 ] && ok "verify-only run did not wait on the land token ($((t1-t0))s)" \
  || bad "verify-only run waited $((t1-t0))s on a token it does not need"

echo "== R2. the land token is HELD for the gate and FREE the moment the train dies (never inherited)"
# The defect this guards: `exec 9>>"$LAND_LOCK"` + `flock -w … 9` leaks a descriptor into
# every child born after the token, and FLOCK lives on the open file DESCRIPTION — so a
# gate stub (or a hung npm/playwright/preflight) keeps the lock after the train is gone.
# The next `--land` lane then waits the whole LAND_WAIT (5400 s) for a train that no longer
# exists, and a hung gate holds it without limit. The token must therefore be taken by a
# WRAPPER that closes the descriptor in the child (`flock --close`), and the release must
# be observable while the gate is still running.
#
# This case is also a bite proof for the ABSENCE of a token: if `--land` never takes one,
# the "held during the gate" assertion below fails.
RLAND_DIR="$S/gate-leak"; mkdir -p "$RLAND_DIR"
RLAND_TOK=$RLAND_DIR/train-land.lock
RLONG_STUB=$S/gate-stub-long.sh
RPIDFILE=$S/gate-stub-long.pid
rm -f "$RPIDFILE"
cat > "$RLONG_STUB" <<STUBEOF
#!/usr/bin/env bash
printf '%s\n' "\$\$" > "$RPIDFILE"
exec sleep 300
STUBEOF
chmod +x "$RLONG_STUB"
RLOG=$S/leak.log
PREFLIGHT_RESULT_DIR=$RLAND_DIR TRAIN_GATE_CMD="bash $RLONG_STUB" LAND_WAIT=10 \
  "$HERE/merge-train.sh" --base "$BASE" --land 999:train-good1 >"$RLOG" 2>&1 &
RPID=$!
_i=0
while [ ! -s "$RPIDFILE" ] && [ "$_i" -lt 300 ]; do sleep 0.1; _i=$((_i+1)); done
STUBPID=$(sed -n 1p "$RPIDFILE" 2>/dev/null)
if [ -n "$STUBPID" ] && kill -0 "$STUBPID" 2>/dev/null; then
  ok "the gate stub is running (pid $STUBPID) — the token window is open"
  # R1: the token is held for the WHOLE gate, not just for a moment at the start.
  if flock -n "$RLAND_TOK" -c true 2>/dev/null; then
    bad "the land token was FREE while the gate was running — --land did not take it (or the gate runs outside it)"
  else
    ok "the land token is HELD while the gate runs"
  fi
  # Kill the TRAIN only (never the stub, never a process group): the stub must outlive it.
  train_pid() {   # $1 = candidate pid -> prints it when it is the merge-train script
    [ "$(cat "/proc/$1/comm" 2>/dev/null)" = bash ] || return 1
    tr '\0' ' ' <"/proc/$1/cmdline" 2>/dev/null | grep -q 'merge-train\.sh' || return 1
    printf '%s\n' "$1"
  }
  find_train() {  # DEEPEST process under $1 that is the train (the flock wrapper mentions it too)
    local p=$1 c r found=""
    train_pid "$p" >/dev/null && found=$p
    for c in $(pgrep -P "$p" 2>/dev/null); do
      r=$(find_train "$c") && found=$r
    done
    [ -n "$found" ] && printf '%s\n' "$found"
  }
  TPID=$(find_train "$RPID")
  if [ -z "$TPID" ]; then
    bad "could not find the train process under pid $RPID"
  else
    kill -TERM "$TPID" 2>/dev/null
    _i=0
    while kill -0 "$TPID" 2>/dev/null && [ "$_i" -lt 100 ]; do sleep 0.1; _i=$((_i+1)); done
    wait "$RPID" 2>/dev/null
    kill -0 "$TPID" 2>/dev/null && bad "the train (pid $TPID) ignored SIGTERM" \
      || ok "the train (pid $TPID) is gone"
    # R2: the stub is STILL ALIVE and the token must already be free.
    if kill -0 "$STUBPID" 2>/dev/null; then
      ok "the gate stub outlived the train (pid $STUBPID still running)"
      RNFD=0
      for _f in "/proc/$STUBPID/fd"/*; do
        [ "$(readlink "$_f" 2>/dev/null)" = "$RLAND_TOK" ] && RNFD=$((RNFD+1))
      done
      [ "$RNFD" = 0 ] \
        && ok "the stub inherited no descriptor on the token (0 fds)" \
        || bad "the stub still holds $RNFD fd(s) on the token — the lock outlives the train"
      if flock -n "$RLAND_TOK" -c true 2>/dev/null; then
        ok "the token is FREE while the orphaned gate still runs (the next lane can land)"
      else
        bad "the token is still HELD by the orphaned gate — a second --land lane would wait LAND_WAIT for a train that is gone"
      fi
    else
      bad "the gate stub died with the train — this case needs the stub to survive it"
    fi
    kill -TERM "$STUBPID" 2>/dev/null
    _i=0
    while kill -0 "$STUBPID" 2>/dev/null && [ "$_i" -lt 100 ]; do sleep 0.1; _i=$((_i+1)); done
    flock -n "$RLAND_TOK" -c true 2>/dev/null \
      && ok "the token is still free after the stub was cleaned up" \
      || bad "the token stayed held after every holder was killed"
  fi
else
  bad "the gate stub never started — the train did not reach the gate"
  sed 's/^/    /' "$RLOG"
  kill -TERM "$RPID" 2>/dev/null
  wait "$RPID" 2>/dev/null
fi
rm -f "$RLONG_STUB"

echo "== S. --land appends ONE landing receipt per landed PR, and only after the asserts"
# THE DEFECT THIS GUARDS. The per-tree gate record
# (${PREFLIGHT_RESULT_DIR}/<tree>.json) is ONE record per TREE, and every later run on
# that tree REWRITES it — so the record that justified a merge dies the moment anything
# re-gates the same bytes. Measured on PR #1732: the train landed tree 690e1d75… at
# 11:04Z, an unrelated run re-gated the same tree at 12:16Z and wrote
# `verdict: red, failed: 1 (prunehost)` over it, so a cleanly-landed merge read as though
# it had landed on a red tree. The fix is a landing EVENT stream that nothing rewrites.
#
# THIS DRIVES THE REAL LANDING PATH, end to end, with no network and no PR: a local bare
# repository stands in for GitHub and a fake `gh` writes the squash commit the merge API
# would write (same tree, same parent, same author). The train's own ls-remote / fetch /
# tree-compare / author-assert therefore run unmodified, and the receipt is written by
# the same function the production path uses. The gate is a stub that prints a
# preflight-shaped SUMMARY, because the receipt's `scope` is parsed from that summary —
# a stub printing nothing would only exercise the `unreported` fallback.
LAND_REMOTE=$S/land-remote.git
git init -q --bare "$LAND_REMOTE"
git push -q "$LAND_REMOTE" "$BASE:refs/heads/main" \
  && ok "a local bare remote stands in for GitHub (main = $BASE)" \
  || bad "cannot seed the local bare remote"
git remote set-url origin "$LAND_REMOTE"
RECEIPT=$PREFLIGHT_RESULT_DIR/landings.jsonl
rm -f "$RECEIPT"

mkdir -p "$S/lbin"
cat > "$S/lbin/gh" <<'LFAKEGH'
#!/usr/bin/env bash
# Fake `gh pr merge` for the merge-train self-test. It writes the squash commit GitHub
# WOULD write onto the local bare remote: same tree, same parent, same author — so the
# train's post-landing assertions and its receipt run for real, with no network.
#   FAKE_GH_MODE=wrongtree  the landed bytes are NOT the tested bytes (the exit-4 path)
#   FAKE_GH_MODE=foreign    the squash is authored by somebody else (the exit-6 path)
#   FAKE_GH_TREE=<tree>     push THAT tree (a batch: the tree of the whole candidate)
set -o pipefail
pr=""; mail=""; head=""
while [ $# -gt 0 ]; do
  case "$1" in
    --author-email)      mail=$2; shift 2 ;;
    --match-head-commit) head=$2; shift 2 ;;
    -R)                  shift 2 ;;
    --squash)            shift ;;
    pr|merge)            shift ;;
    *)                   [ -z "$pr" ] && pr=$1; shift ;;
  esac
done
[ -n "$head" ] && [ -n "$mail" ] || { echo "fake gh: unexpected argv" >&2; exit 1; }
tree=$(git rev-parse --verify "$head^{tree}") || exit 1
case "${FAKE_GH_MODE:-ok}" in
  wrongtree) tree=$(git rev-parse --verify "$FAKE_GH_BASE^{tree}") || exit 1 ;;
  foreign)   mail="somebody@example.com" ;;
esac
[ -n "${FAKE_GH_TREE:-}" ] && tree=$FAKE_GH_TREE
parent=$(git --git-dir="$FAKE_GH_REMOTE" rev-parse refs/heads/main) || exit 1
c=$(GIT_AUTHOR_NAME='Wan Zulkarnain' GIT_AUTHOR_EMAIL="$mail" \
    GIT_COMMITTER_NAME='GitHub' GIT_COMMITTER_EMAIL="$mail" \
    git commit-tree "$tree" -p "$parent" -m "squash #$pr") || exit 1
git push -q "$FAKE_GH_REMOTE" "$c:refs/heads/main" || exit 1
printf 'fake gh: merged #%s as %s\n' "$pr" "$c"
LFAKEGH
chmod +x "$S/lbin/gh"

LSTUB=$S/gate-stub-summary.sh
cat > "$LSTUB" <<'LSTUBEOF'
#!/usr/bin/env bash
# A gate stub that reports a preflight-shaped summary, so the train's scope parser runs
# on the same shape a real run produces.
#   LAND_STUB_SHAPE=scoped (default)  a diff-mapped run: some gates pass, the rest skip
#   LAND_STUB_SHAPE=reused            the whole run was a RECORD READ (the #1732 shape)
case "${LAND_STUB_SHAPE:-scoped}" in
  reused)
    printf 'preflight summary: 0 passed, 0 failed, 0 skipped in 0s\n'
    printf '  REUSE: build vet test (REMEMBERED from a recorded pass on tree 1234abc, NOT re-run this time)\n'
    printf '  SKIP: a11y e2e-core (not evidence)\n'
    ;;
  *)
    printf 'preflight summary: 3 passed, 0 failed, 0 skipped in 1s\n'
    printf '  PASS: build vet gofmt\n'
    printf '  SKIP: test web a11y e2e-core npmaudit (not evidence)\n'
    ;;
esac
exit 0
LSTUBEOF
chmod +x "$LSTUB"

# The train, with the fake gh FIRST on PATH so the merge goes to the local bare remote.
land_run() {   # $1 = gate stub, rest = train argv
  local stub=$1; shift
  PATH="$S/lbin:$PATH" FAKE_GH_REMOTE="$LAND_REMOTE" FAKE_GH_BASE="$BASE" \
    TRAIN_GATE_CMD="bash $stub" LAND_WAIT=30 run "$@"
}

# Read field `$2` out of receipt line `$1` (1-based), through a real JSON parser.
receipt_field() {   # $1 = line number, $2 = field
  python3 - "$RECEIPT" "$1" "$2" <<'PY'
import json, sys
lines = [l for l in open(sys.argv[1], encoding="utf-8") if l.strip()]
rec = json.loads(lines[int(sys.argv[2]) - 1])
v = rec.get(sys.argv[3])
print("" if v is None else v)
PY
}
receipt_lines() { [ -f "$RECEIPT" ] && awk 'NF{n++} END{print n+0}' "$RECEIPT" || printf '0\n'; }

# The tree a squash of these refs onto BASE produces — for the batch case, where the
# fake gh cannot know which prefix it is being asked to land.
cand_tree() {
  local w=$S/candtree
  git worktree remove --force "$w" >/dev/null 2>&1
  git worktree add -q --detach "$w" "$BASE" || return 1
  local r
  for r in "$@"; do
    git -C "$w" merge --squash "$r" >/dev/null 2>&1 && git -C "$w" commit -q -m "squash $r" >/dev/null
  done
  git -C "$w" rev-parse HEAD^{tree}
  git worktree remove --force "$w" >/dev/null 2>&1
}

# --- S1: the success path -----------------------------------------------------
OUT=$(land_run "$LSTUB" --base "$BASE" --land 9001:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
GTREE=$(sed -n 's/^merge-train\.sh: GREEN candidate tree \([0-9a-f]*\).*/\1/p' <<<"$OUT")
LANDED_MAIN=$(git --git-dir="$LAND_REMOTE" rev-parse refs/heads/main)
[ "$RC" -eq 0 ] && ok "a landed PR exits 0" || bad "expected exit 0, got $RC"
grep -q '^merge-train\.sh: LANDED tree .* == tested candidate tree' <<<"$OUT" \
  && ok "the landed tree is asserted equal to the tested candidate tree" || bad "no LANDED assertion"
[ "$(receipt_lines)" = 1 ] && ok "exactly ONE receipt line was appended" \
  || bad "expected 1 receipt line, got $(receipt_lines)"
[ "$(receipt_field 1 pr)" = 9001 ] && ok "the line names the PR (pr=9001)" || bad "pr is [$(receipt_field 1 pr)]"
[ "$(receipt_field 1 ref)" = train-good1 ] && ok "the line names the ref it landed" || bad "ref is [$(receipt_field 1 ref)]"
[ "$(receipt_field 1 head)" = "$(git rev-parse train-good1)" ] \
  && ok "head is the sha handed to --match-head-commit" || bad "head is [$(receipt_field 1 head)]"
[ "$(receipt_field 1 base_sha)" = "$BASE" ] && ok "base_sha is the base the batch was gated on" || bad "base_sha is [$(receipt_field 1 base_sha)]"
[ -n "$GTREE" ] && [ "$(receipt_field 1 tested_tree)" = "$GTREE" ] \
  && ok "tested_tree is the tree the gate actually green'd ($GTREE)" || bad "tested_tree is [$(receipt_field 1 tested_tree)], GREEN said [$GTREE]"
[ "$(receipt_field 1 landed_tree)" = "$(receipt_field 1 tested_tree)" ] \
  && ok "landed_tree == tested_tree (the receipt cannot claim two trees)" || bad "landed_tree != tested_tree"
[ "$(receipt_field 1 merge_sha)" = "$LANDED_MAIN" ] \
  && ok "merge_sha is the squash commit this PR became on main" || bad "merge_sha is [$(receipt_field 1 merge_sha)], main is $LANDED_MAIN"
SC1=$(receipt_field 1 scope)
case "$SC1" in
  "scoped: diff-mapped vs $BASE"*) ok "scope names the gate's own scope: [$SC1]" ;;
  *) bad "scope does not name a diff-mapped run: [$SC1]" ;;
esac
grep -q 'never ran test web a11y e2e-core npmaudit' <<<"$SC1" \
  && ok "scope names the gates that NEVER ran (a scoped pass is never read as full)" \
  || bad "scope hides which gates did not run: [$SC1]"
GS1=$(receipt_field 1 gate_seconds)
case "$GS1" in ''|*[!0-9]*) bad "gate_seconds is not a number: [$GS1]" ;; *) ok "gate_seconds is a number ($GS1)" ;; esac
case "$(receipt_field 1 at)" in *Z) ok "at is a UTC timestamp" ;; *) bad "at is not UTC: [$(receipt_field 1 at)]" ;; esac

# --- S2: the gate reported a RECORD READ, and the receipt says so -------------
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
OUT=$(LAND_STUB_SHAPE=reused land_run "$LSTUB" --base "$BASE" --land 9002:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 0 ] && ok "a landing on a REUSED record exits 0" || bad "expected exit 0, got $RC"
[ "$(receipt_lines)" = 2 ] && ok "a second PR appends a second line (append-only, not rewritten)" \
  || bad "expected 2 receipt lines, got $(receipt_lines)"
SC2=$(receipt_field 2 scope)
case "$SC2" in
  reused:*) ok "scope says the gates were REUSED from a record, not run: [$SC2]" ;;
  *) bad "a record-read run is not labelled reused: [$SC2]" ;;
esac
grep -q '1234abc' <<<"$SC2" && ok "the reused scope names the tree the record came from" \
  || bad "the reused scope does not name its source tree: [$SC2]"

# --- S3: the tree mismatch path writes NOTHING --------------------------------
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
OUT=$(FAKE_GH_MODE=wrongtree land_run "$LSTUB" --base "$BASE" --land 9003:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 4 ] && ok "landed tree != tested tree exits 4" || bad "expected exit 4, got $RC"
grep -q 'MISMATCH: landed tree' <<<"$OUT" && ok "says MISMATCH" || bad "no MISMATCH line"
[ "$(receipt_lines)" = 2 ] \
  && ok "NO receipt line for a landing whose bytes the gate did not vouch for" \
  || bad "the mismatch path wrote a receipt ($(receipt_lines) lines) — evidence for a landing that never happened"

# --- S4: the author mismatch path writes NOTHING -------------------------------
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
OUT=$(FAKE_GH_MODE=foreign land_run "$LSTUB" --base "$BASE" --land 9004:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 6 ] && ok "a landed commit under a foreign identity exits 6" || bad "expected exit 6, got $RC"
[ "$(receipt_lines)" = 2 ] \
  && ok "NO receipt line when the author assert failed (the tree alone is not a landing)" \
  || bad "the author-mismatch path wrote a receipt ($(receipt_lines) lines)"

# --- S5: a batch ejected on red writes NOTHING ---------------------------------
# Case Q2 runs the REAL preflight with a stubbed `go test` on the base tree, so the
# sandbox record store still carries a RED record for $BASE_TREE here; the train would
# then (correctly) refuse to attribute anything to the batch and exit 5. Drop it so this
# case measures the RED-BATCH path, and S5b keeps it for the base-red path.
rm -f "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json"
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
OUT=$(land_run "$REDSTUB" --base "$BASE" --land 9005:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 1 ] && ok "a red batch exits 1 (ejected before any landing)" || bad "expected exit 1, got $RC"
[ "$(receipt_lines)" = 2 ] && ok "NO receipt line for a batch ejected on red" \
  || bad "a red batch wrote a receipt ($(receipt_lines) lines)"

# --- S5b: a batch ejected on a RED BASE writes NOTHING -------------------------
python3 - "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json" <<'PY'
import json, sys
json.dump({"format": 1, "tree": sys.argv[1].split("/")[-1][:-5], "passed": 0, "failed": 1,
           "skipped": 0, "gates": {"gofmt": {"status": "fail"}}}, open(sys.argv[1], "w"))
PY
OUT=$(land_run "$REDSTUB" --base "$BASE" --land 9008:train-good1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 5 ] && ok "a batch on a RED BASE exits 5" || bad "expected exit 5, got $RC"
grep -q 'BASE-RED' <<<"$OUT" && ok "names the red base it refused to attribute to" || bad "no BASE-RED line"
[ "$(receipt_lines)" = 2 ] && ok "NO receipt line for a batch ejected on a red base" \
  || bad "a base-red batch wrote a receipt ($(receipt_lines) lines)"
rm -f "$PREFLIGHT_RESULT_DIR/$BASE_TREE.json"

# --- S5c: a CONFLICTING batch writes NOTHING (exit 3) --------------------------
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
OUT=$(land_run "$LSTUB" --base "$BASE" --land 9009:train-clash 9010:train-clash2); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 3 ] && ok "a conflicting batch exits 3" || bad "expected exit 3, got $RC"
[ "$(receipt_lines)" = 2 ] && ok "NO receipt line for a batch ejected on conflict" \
  || bad "a conflicting batch wrote a receipt ($(receipt_lines) lines)"

# --- S6: a LATER run on the same tree cannot erase the receipt ----------------
# This is the defect itself: the per-tree record is rewritten by the next run on the
# same bytes. The receipt is a separate append-only file, so the landing's evidence must
# survive a re-gate unchanged, byte for byte.
BEFORE=$(sha256sum "$RECEIPT" | awk '{print $1}')
OUT=$(TRAIN_GATE_CMD="bash $LSTUB" run --base "$BASE" train-good1); RC=$?
REGATED=$(sed -n 's/^merge-train\.sh: GREEN candidate tree \([0-9a-f]*\).*/\1/p' <<<"$OUT")
AFTER=$(sha256sum "$RECEIPT" | awk '{print $1}')
[ "$RC" -eq 0 ] && ok "the same batch re-gates green (the tree is unchanged)" || bad "re-gate exited $RC"
[ "$REGATED" = "$GTREE" ] && ok "the re-gate is on the SAME tree ($GTREE) as the landing" \
  || bad "the re-gate tree ($REGATED) is not the landed tree ($GTREE) — the case measures nothing"
[ "$BEFORE" = "$AFTER" ] && ok "the landing receipt is byte-identical after a later run on the same tree" \
  || bad "a later run on the same tree changed the receipt — a landing's evidence must not be rewritable"
[ "$(receipt_lines)" = 2 ] && ok "both earlier lines are still there (append-only)" \
  || bad "$(receipt_lines) line(s) survived the re-gate, expected 2"

# --- S7: a BATCH appends one line PER landed PR -------------------------------
git --git-dir="$LAND_REMOTE" update-ref refs/heads/main "$BASE"
BATCH_TREE=$(cand_tree train-good1 train-good2)
if [ -z "$BATCH_TREE" ]; then
  bad "cannot compute the batch candidate tree — the batch case would be vacuous"
else
  OUT=$(FAKE_GH_TREE="$BATCH_TREE" land_run "$LSTUB" --base "$BASE" --land 9006:train-good1 9007:train-good2); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "a two-PR batch lands (exit 0)" || bad "expected exit 0, got $RC"
  [ "$(receipt_lines)" = 4 ] && ok "a batch of two PRs appends TWO lines (one per landed PR)" \
    || bad "expected 4 lines in total, got $(receipt_lines)"
  [ "$(receipt_field 3 pr)" = 9006 ] && [ "$(receipt_field 4 pr)" = 9007 ] \
    && ok "the two lines name both PRs, in landing order" \
    || bad "the batch lines name [$(receipt_field 3 pr), $(receipt_field 4 pr)]"
  [ "$(receipt_field 3 ref)" = train-good1 ] && [ "$(receipt_field 4 ref)" = train-good2 ] \
    && ok "each line names its own ref" || bad "the batch lines do not name their refs"
  [ "$(receipt_field 3 tested_tree)" = "$BATCH_TREE" ] && [ "$(receipt_field 4 landed_tree)" = "$BATCH_TREE" ] \
    && ok "both lines carry the batch's tree as tested AND landed" \
    || bad "batch trees: tested [$(receipt_field 3 tested_tree)] landed [$(receipt_field 4 landed_tree)] want $BATCH_TREE"
  M3=$(receipt_field 3 merge_sha); M4=$(receipt_field 4 merge_sha)
  [ -n "$M3" ] && [ -n "$M4" ] && [ "$M3" != "$M4" ] \
    && ok "each PR carries its OWN squash sha ($(printf '%.8s' "$M3")…, $(printf '%.8s' "$M4")…)" \
    || bad "the two PRs share a merge_sha [$M3] [$M4] — the field would identify neither"
fi

printf '\nmerge-train selftest: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || { _selftest_keep=1; exit 1; }
