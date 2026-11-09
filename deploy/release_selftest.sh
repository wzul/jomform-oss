#!/usr/bin/env bash
# release_selftest — bite proof for the release rules.
#
# Proves, offline and without docker, that:
#   A. an UNTAGGED tree cannot be deployed        (require-release-tag exit 4)
#   B. a tagged tree resolves to its tag          (exit 0, tag on stdout)
#   C. VERSION_OVERRIDE may only restate the tag  (mismatch -> exit 5)
#   D. the next-version arithmetic is right       (minor vs patch, from the last tag)
#   E. a release plans, and a dry run tags NOTHING
#   F. a release with no commits since the tag refuses (exit 3)
#   G. an existing tag is never re-cut              (exit 4)
#   H. the mirror sync is a no-op until --yes
#   I. the mirror carries the released tree under a commit named "Release <tag>"
#   J. a tag that was never pushed cannot be mirrored (exit 2)
#   J2. a key-shaped literal in the released tree is refused BEFORE the push (exit 6)
#   Q. the tag-pinned release tree is SEEDED before it ships, and the dependency set
#      is chosen by the per-tree record (reuse) — with `--ship-only` recovering a
#      genuine ship failure
#   Q6. an unshippable cut is REFUSED before it tags (exit 7) and never reaches the mirror
#   Q7. a ship that returned 0 but prod did not receive exits 5, and the mirror is not reached
#   Q8. control: a verified ship does reach the mirror
#   R. build-and-ship performs the server half and PROVES it (or refuses before building);
#      a build-only run still works and never claims a ship
#
# Everything runs in throwaway repos: no network, no real remote, no docker, no ship.
set -o pipefail

# THE CALLER'S VERSION_OVERRIDE IS DROPPED. Every case below that needs an override sets its own
# (`VERSION_OVERRIDE=v1.0.0 bash "$RRT"`, `ship VERSION_OVERRIDE=v1.0.0`), so an inherited value
# does not describe these fixtures — it rewrites the cases that assert an UNTAGGED tree, a
# CONTRADICTING override, or the positional form. That is not theoretical: deploy/build-and-ship.sh
# exports VERSION_OVERRIDE for require-release-tag and then runs this suite, and deploy/cut-release.sh
# ships with exactly that override, so the release path could not ship. Measured on this tree,
# same bytes: `VERSION_OVERRIDE=v9.9.9 bash deploy/release_selftest.sh` -> 72 passed / 7 failed,
# `env -u VERSION_OVERRIDE bash deploy/release_selftest.sh` -> 79 passed / 0 failed.
# build-and-ship.sh drops it at the call site too (`env -u`); this line makes the suite hermetic
# however it is invoked, including by hand. Section P re-executes this file with an override set
# and requires it to be gone, so deleting this line turns the suite RED.
unset VERSION_OVERRIDE

# Hermeticity probe for section P only: report the environment this process was handed, after the
# drop above, and exit before any fixture is built.
if [ "${RELEASE_SELFTEST_PROBE:-0}" = 1 ]; then
  if [ -z "${VERSION_OVERRIDE:-}" ]; then echo "clean"; exit 0; fi
  echo "inherited VERSION_OVERRIDE=$VERSION_OVERRIDE"
  exit 1
fi

PROG=${0##*/}
HERE=$(cd "$(dirname "$0")" && pwd)
S=${SELF_TEST_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/release-selftest.XXXXXX")}
pass=0; fail=0
ok()  { printf '  ok   %s\n' "$*"; pass=$((pass+1)); }
bad() { printf '  FAIL %s\n' "$*"; fail=$((fail+1)); }

RRT="$HERE/require-release-tag.sh"
CUT="$HERE/cut-release.sh"
SYNC="$HERE/sync-oss.sh"
# deploy/web_modules.sh is the SEEDER the release path needs (see section Q): without it
# cut-release cannot give its tag-pinned worktree a web/node_modules, so a missing one is
# a broken release path, not a skipped case. deploy/release_scope.sh is the RULE that says
# whether a diff owes an image at all (section S); without it both release tools refuse to
# run, so a missing one is likewise a broken release path.
for f in "$RRT" "$CUT" "$SYNC" "$HERE/web_modules.sh" "$HERE/release_scope.sh"; do
  [ -f "$f" ] || { echo "$PROG: missing $f" >&2; exit 1; }
done

ORIGIN=$S/origin.git
WORK=$S/work
git init -q --bare "$ORIGIN"
git init -q "$WORK"
cd "$WORK" || exit 1
git config user.name "Release Selftest"
git config user.email "selftest@users.noreply.github.com"
git remote add origin "$ORIGIN"
printf 'hello\n' > README.md
git add -A && git commit -q -m "initial"
git branch -M main
git push -q -u origin main
# Drop tags HERE AND ON ORIGIN: cut-release fetches --tags, so a tag deleted only locally
# comes back on the next run and makes `git describe` ambiguous.
DROP_TAGS() {
  git tag --list | while read -r t; do
    git tag -d "$t" >/dev/null
    git push -q origin --delete "$t" >/dev/null 2>&1 || true
  done
}

echo "== A. an untagged tree cannot be deployed"
OUT=$(bash "$RRT" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 4 ] && ok "exit 4 for an untagged HEAD" || bad "expected exit 4, got $RC"
grep -q 'carries no tag' <<<"$OUT" && ok "says the tree carries no tag" || bad "refusal not explained"
grep -q 'cut-release.sh' <<<"$OUT" && ok "names the way to cut a release" || bad "no remedy named"
[ -z "$(bash "$RRT" 2>/dev/null)" ] && ok "prints nothing on stdout when refusing" || bad "printed a version while refusing"
# The bite: the line this replaced (`git describe --tags --always`) answers happily here, so the
# old code shipped an untagged tree stamped with a SHA. That is the defect, reproduced.
OLD=$(git describe --tags --always HEAD 2>/dev/null)
[ -n "$OLD" ] && ok "the old fallback would have shipped '$OLD' from an untagged HEAD" \
  || bad "the old formula is empty too — this test proves nothing about the fallback"

echo "== B. a tagged tree resolves to its tag"
git tag -a v1.0.0 -m "Release v1.0.0"
git push -q origin v1.0.0
OUT=$(bash "$RRT" 2>/dev/null); RC=$?
[ $RC -eq 0 ] && ok "exit 0 on a tagged HEAD" || bad "expected exit 0, got $RC"
[ "$OUT" = "v1.0.0" ] && ok "printed exactly v1.0.0" || bad "printed '$OUT'"

echo "== C. VERSION_OVERRIDE may only restate the tag"
OUT=$(VERSION_OVERRIDE=v9.9.9 bash "$RRT" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 5 ] && ok "exit 5 when the override contradicts the tag" || bad "expected exit 5, got $RC"
grep -q 'does not match the tag' <<<"$OUT" && ok "says the override does not match" || bad "mismatch not explained"
OUT=$(VERSION_OVERRIDE=v1.0.0 bash "$RRT" 2>/dev/null); RC=$?
[ $RC -eq 0 ] && [ "$OUT" = "v1.0.0" ] && ok "a matching override is accepted" || bad "matching override rejected ($RC)"

echo "== D. next-version arithmetic"
chk() {  # $1=last tag  $2=kind  $3=want
  local got
  DROP_TAGS
  git tag -a "$1" -m "t"
  got=$(bash "$CUT" --print-next "--$2" 2>/dev/null)
  [ "$got" = "$3" ] && ok "$1 --$2 -> $3" || bad "$1 --$2 -> got '$got', want '$3'"
}
chk v1.23.12 minor v1.24.0
chk v1.23.12 patch v1.23.13
chk v1.24.0  minor v1.25.0
chk v1.24.9  patch v1.24.10

echo "== E. a release plans, and a dry run tags nothing"
DROP_TAGS
git tag -a v1.0.0 -m "Release v1.0.0"
git push -q origin v1.0.0
printf 'feature\n' >> README.md
mkdir -p api && printf 'package api\n' > api/feature.go
git add -A && git commit -q -m "feat(api): something worth releasing"
git push -q origin main
BEFORE=$(git tag --list | wc -l | tr -d ' ')
OUT=$(bash "$CUT" --dry-run 2>&1); RC=$?
AFTER=$(git tag --list | wc -l | tr -d ' ')
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "exit 0 on a dry run" || bad "expected exit 0, got $RC"
grep -q 'DRY RUN' <<<"$OUT" && ok "says it is a dry run" || bad "no dry-run banner"
grep -q 'next release: v1.1.0' <<<"$OUT" && ok "plans v1.1.0 (minor bump from v1.0.0)" || bad "wrong next version"
grep -q 'build-and-ship.sh' <<<"$OUT" && ok "plans the ship" || bad "ship step missing from the plan"
grep -q 'sync-oss.sh' <<<"$OUT" && ok "plans the mirror" || bad "mirror step missing from the plan"
[ "$BEFORE" = "$AFTER" ] && ok "created no tag" || bad "a dry run created a tag ($BEFORE -> $AFTER)"

echo "== F. nothing to release"
git tag -a v1.1.0 -m "Release v1.1.0"
git push -q origin v1.1.0
OUT=$(bash "$CUT" --dry-run 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 3 ] && ok "exit 3 with no commits since the last tag" || bad "expected exit 3, got $RC"
grep -q 'nothing to release' <<<"$OUT" && ok "says nothing to release" || bad "reason not stated"

echo "== G. an existing tag is never re-cut"
printf 'more\n' >> README.md
git commit -q -am "fix: another change"
git push -q origin main
# The colliding tag must exist as a REF but not be reachable from main, otherwise it becomes
# the "last release" and the count collapses to zero — which is a different refusal.
git checkout -q -b sidebranch
printf 'side\n' > SIDE.md && git add -A && git commit -q -m "side commit"
git tag -a v1.2.0 -m "Release v1.2.0"      # the version the next minor bump would pick
git checkout -q main && git branch -q -D sidebranch
OUT=$(bash "$CUT" --dry-run 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 4 ] && ok "exit 4 when the next version is already tagged" || bad "expected exit 4, got $RC"
grep -q 'already exists' <<<"$OUT" && ok "says the tag already exists" || bad "collision not explained"
git tag -d v1.2.0 >/dev/null

echo "== H–J. the public mirror is a release artifact"
MIRROR=$S/oss.git
SEED=$S/seed
mkdir -p "$SEED" && ( cd "$SEED" && git init -q && git config user.email s@x && git config user.name s \
  && printf 'old\n' > OLD.md && git add -A && git commit -q -m "seed" && git branch -M main )
git clone -q --bare "$SEED" "$MIRROR"

BEFORE_SHA=$(git -C "$MIRROR" rev-parse main)
OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.1.0 --dry-run 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "mirror dry run exits 0" || bad "expected exit 0, got $RC"
grep -q 'DRY RUN' <<<"$OUT" && ok "mirror dry run says nothing was pushed" || bad "no dry-run banner"
[ "$(git -C "$MIRROR" rev-parse main)" = "$BEFORE_SHA" ] && ok "the mirror did not move" \
  || bad "a dry run pushed to the mirror"
grep -qi 'deletion' <<<"$OUT" && ok "reports the deletions it would make" || bad "deletions not reported"

OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.1.0 --yes 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "mirror --yes exits 0" || bad "expected exit 0, got $RC"
MSG=$(git -C "$MIRROR" log -1 --format=%s main 2>/dev/null || true)
[ "$MSG" = "Release v1.1.0" ] && ok "mirror commit is 'Release v1.1.0'" || bad "mirror commit is '$MSG'"
git -C "$MIRROR" cat-file -e "main:README.md" 2>/dev/null \
  && ok "the released file is what landed in the mirror" || bad "released file missing from the mirror"
git -C "$MIRROR" cat-file -e "main:OLD.md" 2>/dev/null \
  && bad "a file dropped upstream survived in the mirror" || ok "a file dropped upstream is deleted from the mirror"

OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.1.0 --yes 2>&1); RC=$?
[ $RC -eq 7 ] && ok "a second sync is a no-op (exit 7)" || bad "expected exit 7, got $RC"

printf 'local only\n' >> README.md
git commit -q -am "local only change"
git tag -a v1.3.0 -m "Release v1.3.0"       # never pushed
OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.3.0 --yes 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 2 ] && ok "exit 2 for an unpushed tag" || bad "expected exit 2, got $RC"
grep -q 'not pushed to origin' <<<"$OUT" && ok "says the tag is not pushed" || bad "unpushed tag not explained"

echo "== J2. a key-shaped literal is refused BEFORE the push (push protection is remote)"
# GitHub push protection refuses a push whose content matches a provider secret pattern, even
# when the value is a synthetic fixture, and it has no path allowlist — so a `_test.go` fixture
# that gitleaks allowlists is still refused. That refusal lands at step 4 of the release, AFTER
# the deploy has shipped, and leaves the mirror stale (t_1a11c700). sync-oss must refuse it
# locally instead, naming the file. The fixture is ASSEMBLED here for the same reason the real
# one is assembled in mask_test.go: a contiguous `sk_live_` + 48 characters in THIS file would
# trip the very detector the case is about.
KEYBODY=$(printf '%s' 0123456789abcdef 0123456789abcdef 0123456789abcdef)
printf 'api_key = "%s%s"\n' 'sk_live_' "$KEYBODY" > key_fixture.txt
git add -A && git commit -q -m "test: a key-shaped fixture"
git tag -a v1.7.0 -m "Release v1.7.0" && git push -q origin main v1.7.0
MIRROR_BEFORE=$(git -C "$MIRROR" rev-parse main)
OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.7.0 --yes 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 6 ] && ok "a key-shaped literal exits 6" || bad "expected exit 6, got $RC"
# The guard's own line is `    <relpath>`; the rsync change list prints `<marker> <relpath>`,
# so this anchors on the guard and cannot be satisfied by the diff by accident.
grep -qE '^ +key_fixture\.txt$' <<<"$OUT" && ok "the refusal names the offending file" \
  || bad "offending file not named by the guard"
grep -q 'push protection' <<<"$OUT" && ok "the refusal names push protection" || bad "reason not explained"
[ "$(git -C "$MIRROR" rev-parse main)" = "$MIRROR_BEFORE" ] && ok "nothing reached the mirror" \
  || bad "a refused tree was pushed anyway"
# The control: the same tree with the key-shaped value removed mirrors cleanly, so the case
# above fails on the literal and on nothing else.
printf 'api_key = built at run time\n' > key_fixture.txt
git commit -q -am "test: drop the key-shaped value"
git tag -a v1.8.0 -m "Release v1.8.0" && git push -q origin main v1.8.0
OUT=$(OSS_URL="file://$MIRROR" bash "$SYNC" --tag v1.8.0 --yes 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "the same tree without the literal mirrors cleanly" || bad "expected exit 0, got $RC"


echo "== K. when is a release DUE (the schedule that replaces per-merge deploys)"
DUE="$HERE/release-due.sh"
[ -f "$DUE" ] || { bad "deploy/release-due.sh is missing"; echo; echo "release selftest: $pass passed, $fail failed"; exit 1; }
git checkout -q main 2>/dev/null || true
git push -q origin main 2>/dev/null || true
DROP_TAGS
git tag -a v1.4.0 -m "Release v1.4.0" && git push -q origin v1.4.0

OUT=$(bash "$DUE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "no commits since the tag -> not due" || bad "expected exit 1, got $RC"
grep -q 'nothing on' <<<"$OUT" && ok "says there is nothing to release" || bad "empty window not explained"

mkdir -p api && printf 'one\n' > api/one.go && git add -A && git commit -q -m "fix(api): one commit" && git push -q origin main
OUT=$(RELEASE_MIN_COMMITS=5 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 1 ] && ok "1 commit, threshold 5 -> not due" || bad "expected exit 1, got $RC"
grep -q 'not due yet' <<<"$OUT" && ok "says the window is smaller/younger than policy" || bad "not-due reason missing"

OUT=$(RELEASE_MIN_COMMITS=1 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "1 commit, threshold 1 -> DUE" || bad "expected exit 0, got $RC"
grep -q 'RELEASE_MIN_COMMITS=1' <<<"$OUT" && ok "names the threshold that triggered it" || bad "trigger not named"

OUT=$(RELEASE_MIN_COMMITS=0 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
[ $RC -eq 1 ] && ok "both thresholds 0 -> schedule disabled (on demand only)" || bad "expected exit 1, got $RC"

OUT=$(RELEASE_MIN_COMMITS=0 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" --force 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "--force is due regardless of the schedule" || bad "expected exit 0, got $RC"
grep -qi 'forced' <<<"$OUT" && ok "says it was forced" || bad "forced reason missing"
# NOTE: --file-card is deliberately NOT exercised here — it files a card on the live board.
# Its dedupe is verified by reading the code path, not by poking the real kanban.


# K2 — THE DEFECT THIS CARD EXISTS FOR. A window of deploy/-only commits is "due" on the
# arithmetic (6 commits >= 5) and was released four times in eight (v1.25.0, v1.25.1,
# v1.27.2, v1.27.4) — each taking GitHub's "Latest" badge with no image behind it. The
# schedule now asks whether the diff owes anything at all.
drop_tags_keep_main() { DROP_TAGS; git push -q origin main 2>/dev/null || true; }
drop_tags_keep_main
git tag -a v1.9.0 -m "Release v1.9.0" && git push -q origin v1.9.0
mkdir -p deploy
for i in 1 2 3 4 5 6; do
  printf 'tooling %s\n' "$i" >> deploy/release-tooling.txt
  git add -A && git commit -q -m "chore(deploy): tooling change $i"
done
git push -q origin main
OUT=$(RELEASE_MIN_COMMITS=1 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 1 ] && ok "a deploy/-only window is NOT due, however many commits it holds" \
  || bad "expected exit 1, got $RC (the schedule would cut a version for deploy/ again)"
grep -q 'owe no image' <<<"$OUT" && ok "says the window owes no image" || bad "the no-image reason is not stated"
grep -qi 'scope: deploy/' <<<"$OUT" && ok "names the paths that made it plumbing" || bad "the scope is not named"
grep -q 'ride along with the next real release' <<<"$OUT" && ok "says the commits are kept for the next real release" \
  || bad "the commits' fate is not stated"

# The control: --force is an explicit decision (Wan said "release now") and still files.
OUT=$(RELEASE_MIN_COMMITS=0 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" --force 2>&1); RC=$?
[ "$RC" -eq 0 ] && ok "control: --force is still DUE on a tooling-only window" || bad "expected exit 0, got $RC"

# No regression: one api/ commit in the same window makes it a release again.
printf 'package api\n' > api/real.go && git add -A && git commit -q -m "fix(api): a real change (#1234)"
git push -q origin main
OUT=$(RELEASE_MIN_COMMITS=1 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 0 ] && ok "an api/ commit makes the window due again (no regression)" || bad "expected exit 0, got $RC"
grep -q 'this window owes an image' <<<"$OUT" && ok "says the window owes an image" || bad "the image debt is not reported"
grep -qi 'scope:.*api/' <<<"$OUT" && ok "names api/ as the reason" || bad "the scope is not named"

# And a web/ commit alone is enough too: the frontend is a shipped artefact.
drop_tags_keep_main
git tag -a v1.9.1 -m "Release v1.9.1" && git push -q origin v1.9.1
mkdir -p web && printf 'export const y = 1;\n' > web/another.tsx && git add -A && git commit -q -m "fix(web): a frontend change"
git push -q origin main
OUT=$(RELEASE_MIN_COMMITS=1 RELEASE_MAX_AGE_HOURS=0 bash "$DUE" 2>&1); RC=$?
[ "$RC" -eq 0 ] && ok "a web/-only window is DUE" || bad "expected exit 0, got $RC"
grep -qi 'scope:.*web/' <<<"$OUT" && ok "names web/ as the reason" || bad "the scope is not named"


echo "== L. the repair path re-cuts the SAME version (--tag), never a new one"
DROP_TAGS
git push -q origin main 2>/dev/null || true
git tag -a v1.5.0 -m "Release v1.5.0" && git push -q origin v1.5.0
printf 'repair\n' > api/repair.go && git add -A && git commit -q -m "fix(api): needs re-cutting" && git push -q origin main
OUT=$(bash "$CUT" --dry-run --tag v1.5.0 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 4 ] && ok "a tag that still exists is never silently moved (exit 4)" || bad "expected exit 4, got $RC"
git push -q origin --delete v1.5.0 && git tag -d v1.5.0 >/dev/null
OUT=$(bash "$CUT" --dry-run --tag v1.5.0 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "after deleting the ref, --dry-run --tag plans the SAME version" || bad "expected exit 0, got $RC"
grep -q 'next release: v1.5.0' <<<"$OUT" && ok "re-cuts v1.5.0, not v1.6.0" || bad "wrong version planned"
OUT=$(bash "$CUT" --dry-run --tag 1.5 2>&1); RC=$?
[ $RC -eq 2 ] && ok "refuses a malformed --tag (exit 2)" || bad "expected exit 2, got $RC"


echo "== M. a release that touches web/ owes a SECOND image"
DROP_TAGS
git push -q origin main 2>/dev/null || true
git tag -a v1.6.0 -m "Release v1.6.0" && git push -q origin v1.6.0
mkdir -p web && printf 'x\n' > web/thing.tsx && git add -A && git commit -q -m "feat(web): change the frontend" && git push -q origin main
OUT=$(bash "$CUT" --dry-run 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ $RC -eq 0 ] && ok "dry run exits 0 with a web/ change" || bad "expected exit 0, got $RC"
grep -q 'IMAGE=jomform-web' <<<"$OUT" && ok "the plan names the web image when web/ changed" \
  || bad "a web/ release would ship only the API image"
OUT=$(bash "$CUT" --dry-run --api-only 2>&1); RC=$?
grep -q 'IMAGE=jomform-web' <<<"$OUT" && bad "--api-only still planned the web image" \
  || ok "--api-only skips the web image on purpose"

echo "== N. --file-card files exactly ONE card, sees every OPEN status, and never files blind"
FB="$S/fake"; FL="$S/fake.log"; mkdir -p "$S/bin" "$FB"
# A stand-in for `hermes kanban`: it records the argv it was handed and answers from
# FAKE_STATE/<status> files, so the filing path is driven end to end WITHOUT a card ever
# touching the real board.
cat > "$S/bin/hermes" <<'FAKE_HERMES'
#!/usr/bin/env bash
printf 'ARGV %s\n' "$*" >> "${FAKE_LOG:-/dev/null}"
board=""; action=""; status=""
while [ $# -gt 0 ]; do
  case "$1" in
    --board)  board="${2:-}"; shift 2 ;;
    --status) status="${2:-}"; shift 2 ;;
    kanban|--json) shift ;;
    *) if [ -z "$action" ]; then action="$1"; fi; shift ;;
  esac
done
case "$action" in
  list)
    if [ "${FAKE_LIST_FAIL:-0}" = 1 ]; then
      printf 'kanban: delegate_task child contexts cannot mutate Kanban tasks or boards\n' >&2
      exit 1
    fi
    [ -n "$status" ] && cat "${FAKE_STATE:-/dev/null}/$status" 2>/dev/null
    exit 0 ;;
  create)
    if [ "${FAKE_CREATE_FAIL:-0}" = 1 ]; then
      printf 'kanban: delegate_task child contexts cannot mutate Kanban tasks or boards\n' >&2
      exit 2
    fi
    printf 'Created t_fa3e0001  (ready, assignee=jf-deployer)\n'
    exit 0 ;;
  edit) printf 'Edited %s\n' "${board:-?}"; exit 0 ;;
esac
exit 0
FAKE_HERMES
chmod +x "$S/bin/hermes"
reset_fake() { rm -rf "$FB"; mkdir -p "$FB"; : > "$FL"; }
fake_release_card() {  # $1 = id suffix, $2 = status
  printf '  t_%s  %s  jf-deployer  release: cut the next release\n' "$1" "$2" >> "$FB/$2"
}
# Not fenced: the lane marker is removed for every case that is supposed to succeed.
due_open() {
  ( unset HERMES_DELEGATED_CHILD_CONTEXT
    PATH="$S/bin:$PATH" FAKE_LOG="$FL" FAKE_STATE="$FB" \
      env "$@" bash "$DUE" --force --file-card 2>&1 )
}

# N1 — a card in ANY open status blocks a second one. The bite for the pre-fix loop
# (`for st in ready running todo`) is 'blocked' and 'review': there it filed a duplicate.
for st in triage todo scheduled ready running blocked review; do
  reset_fake; fake_release_card "x${st}" "$st"
  OUT=$(due_open); RC=$?
  if [ "$RC" -eq 3 ] && ! grep -q 'ARGV .* create ' "$FL"; then
    ok "an open release card in '$st' blocks a second filing (exit 3, no create)"
  else
    bad "status '$st': expected exit 3 and no create, got rc=$RC"; printf '%s\n' "$OUT" | sed 's/^/    /'
  fi
done
# N2 — a DONE card is not open, so the window really does get filed.
reset_fake; printf '  t_done0000  done  jf-deployer  release: cut the next release\n' > "$FB/done"
OUT=$(due_open); RC=$?
[ "$RC" -eq 0 ] && ok "a DONE release card does not block (exit 0)" || bad "expected exit 0 with only a done card, got $RC"
grep -q 'ARGV kanban --board jomform create release: cut the next release' "$FL" \
  && ok "create carries the release title" || bad "create did not carry the title"
grep -q -- '--assignee jf-deployer --project jomform --workspace worktree --priority 130' "$FL" \
  && ok "create names the deploy lane, the project, a worktree and priority 130" || bad "create argv is wrong"
grep -q 'ARGV kanban --board jomform edit t_fa3e0001' "$FL" \
  && ok "the body is written to the id the CLI answered with" || bad "the card body was not written back"
grep -q 'filed t_fa3e0001' <<<"$OUT" && ok "reports the id it filed" || bad "filed id not reported"

# N3 — the FENCED lane shell: a dedupe it cannot read must never file blind...
reset_fake
OUT=$(PATH="$S/bin:$PATH" HERMES_DELEGATED_CHILD_CONTEXT="$S/lane" FAKE_LIST_FAIL=1 \
      FAKE_LOG="$FL" FAKE_STATE="$FB" bash "$DUE" --force --file-card 2>&1); RC=$?
[ "$RC" -eq 4 ] && ok "a board it cannot read is exit 4, never a silent 0/2" || bad "expected exit 4, got $RC"
grep -q 'nothing was filed' <<<"$OUT" && ok "says nothing was filed" || bad "outcome not stated"
grep -q 'kanban_create tool' <<<"$OUT" && ok "names what to do next (the kanban_create tool)" || bad "no remedy named"
grep -q 'does not match\|HERMES_DELEGATED_CHILD_CONTEXT=' <<<"$OUT" && ok "names the fence it hit" || bad "the fence is not named"
! grep -q 'ARGV .* create ' "$FL" && ok "NO create was attempted: it did not file what it could not dedupe" \
  || bad "it filed a card without being able to read the board"

# N4 — a create the board refuses: the reason is shown, not swallowed (this was the silent 2).
reset_fake
OUT=$(due_open FAKE_CREATE_FAIL=1); RC=$?
[ "$RC" -eq 2 ] && ok "a refused create is exit 2" || bad "expected exit 2, got $RC"
grep -q 'cannot mutate Kanban tasks or boards' <<<"$OUT" && ok "the board's own refusal is printed" \
  || bad "the refusal was swallowed again"

# N5 — --print-card is the lane's way out: the text, and no board access at all.
reset_fake
OUT=$( unset HERMES_DELEGATED_CHILD_CONTEXT; PATH="$S/bin:$PATH" FAKE_LOG="$FL" FAKE_STATE="$FB" \
       bash "$DUE" --force --print-card 2>&1 ); RC=$?
[ "$RC" -eq 0 ] && ok "--print-card exits 0 when a release is due" || bad "expected exit 0, got $RC"
grep -q '^TITLE: release: cut the next release' <<<"$OUT" && ok "prints the card title" || bad "no title printed"
grep -q 'cut-release.sh --yes' <<<"$OUT" && ok "prints the body a lane can file" || bad "no body printed"
[ ! -s "$FL" ] && ok "--print-card never touches the board" || bad "--print-card called the board"

# N6 — THE CARD ITSELF. A deploy/-only window must not even CREATE the card that leads to a
# version cut: filing it and refusing it later would cost a deploy-lane run to discover.
reset_fake
git push -q origin main 2>/dev/null || true
git tag -a v1.7.5 -m "Release v1.7.5" && git push -q origin v1.7.5
printf 'tooling only\n' >> deploy/release-tooling.txt
git add -A && git commit -q -m "chore(deploy): tooling only (#1700)"
git push -q origin main
OUT=$( unset HERMES_DELEGATED_CHILD_CONTEXT; PATH="$S/bin:$PATH" FAKE_LOG="$FL" FAKE_STATE="$FB" \
       bash "$DUE" --file-card 2>&1 ); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 1 ] && ok "a deploy/-only window files NO card (exit 1)" || bad "expected exit 1, got $RC"
! grep -q 'ARGV .* create ' "$FL" && ok "…create was never called" \
  || bad "it filed a release card for a tooling-only window"
grep -q 'owe no image' <<<"$OUT" && ok "…and says why nothing was filed" || bad "no reason printed"
# The control: --force is the explicit "release now", and it does file.
reset_fake
OUT=$(due_open); RC=$?
[ "$RC" -eq 0 ] && grep -q 'ARGV .* create ' "$FL" && ok "control: --force files the card anyway" \
  || bad "control: --force did not file (rc=$RC)"


echo "== O. build-and-ship cannot be told a version that contradicts the tag"
BS="$S/ship"; mkdir -p "$BS/deploy" "$BS/api"
cp "$HERE/build-and-ship.sh" "$HERE/require-release-tag.sh" "$BS/deploy/"
: > "$BS/api/Dockerfile"
# A WITNESS in place of the release selftest, for case O2 below: build-and-ship runs
# deploy/release_selftest.sh before the gate, and the real suite sets its own VERSION_OVERRIDE
# per case — so a value inherited from the caller is the defect O2 pins. This witness fails when
# it sees one and passes otherwise. Inert for every case above, which pass SKIP_RELEASE_SELFTEST=1.
cat > "$BS/deploy/release_selftest.sh" <<'WITNESS'
#!/usr/bin/env bash
# Witness, not a test suite: a value inherited from the caller is exactly what must not arrive.
if [ -n "${VERSION_OVERRIDE:-}" ]; then
  echo "witness: VERSION_OVERRIDE=$VERSION_OVERRIDE was inherited by the release selftest" >&2
  exit 1
fi
echo "  ok   the release selftest started with no VERSION_OVERRIDE of its own"
echo "release selftest: 1 passed, 0 failed"
WITNESS
chmod +x "$BS/deploy/release_selftest.sh"
( cd "$BS" && git init -q && git config user.email s@x && git config user.name s \
    && git add -A && git commit -q -m "ship sandbox" && git tag -a v9.9.9 -m "Release v9.9.9" ) >/dev/null
# DOCKERFILE=nope stops the run right after the version step, so these cases never reach the
# gate or docker; SKIP_RELEASE_SELFTEST keeps it from recursing into this file.
ship() { ( cd "$BS" && env "$@" LOCK_FILE="$S/ship.lock" OUT_DIR="$S/out" DOCKERFILE=nope \
             SKIP_RELEASE_SELFTEST=1 bash deploy/build-and-ship.sh 2>&1 ); }
OUT=$(ship VERSION_OVERRIDE=v1.0.0); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 5 ] && ok "an inherited VERSION_OVERRIDE that contradicts the tag exits 5" \
  || bad "expected exit 5, got $RC"
grep -q 'VERSION_OVERRIDE=v1.0.0 does not match the tag' <<<"$OUT" && ok "names the contradiction" \
  || bad "the contradiction is not explained"
OUT=$(ship); RC=$?
grep -q 'stamps: version=v9.9.9' <<<"$OUT" && ok "control: the same tree stamps v9.9.9 from its tag" \
  || bad "the sandbox never reached the stamps line"; printf '%s\n' "$OUT" | sed 's/^/    /'
# The POSITIONAL form is the same override, and an un-exported one is exactly the bug.
OUT=$( ( cd "$BS" && env LOCK_FILE="$S/ship.lock" OUT_DIR="$S/out" DOCKERFILE=nope \
           SKIP_RELEASE_SELFTEST=1 bash deploy/build-and-ship.sh v1.0.0 2>&1 ) ); RC=$?
[ "$RC" -eq 5 ] && ok "a POSITIONAL version that contradicts the tag exits 5 too" \
  || bad "expected exit 5 from the positional form, got $RC"
# ...and the refusal keeps require-release-tag's own codes rather than a generic 1.
BU="$S/ship-untagged"; cp -a "$BS" "$BU"; git -C "$BU" tag -d v9.9.9 >/dev/null
OUT=$( ( cd "$BU" && env LOCK_FILE="$S/ship2.lock" OUT_DIR="$S/out" DOCKERFILE=nope \
           SKIP_RELEASE_SELFTEST=1 bash deploy/build-and-ship.sh 2>&1 ) ); RC=$?
[ "$RC" -eq 4 ] && ok "an untagged tree still exits 4 (no tag), distinct from 5" \
  || bad "expected exit 4 for an untagged tree, got $RC"

echo "== O2. the CUT-RELEASE shape: VERSION_OVERRIDE=<tag> must not reach the release selftest"
# deploy/cut-release.sh ships a detached, TAGGED tree with
#   VERSION_OVERRIDE="$NEXT" ./deploy/build-and-ship.sh
# That override is build-and-ship's OWN export (require-release-tag.sh needs it), and it used to
# be inherited by the release selftest build-and-ship runs — which sets its own override per case,
# so the inherited value reddened 7 of its cases and refused EVERY release cut. This case pins the
# CALL SITE's environment, not the rules: the witness above fails when a caller's VERSION_OVERRIDE
# reaches it, so a green here means the child was handed a clean environment.
OUT=$( ( cd "$BS" && env VERSION_OVERRIDE=v9.9.9 LOCK_FILE="$S/ship3.lock" OUT_DIR="$S/out" \
           DOCKERFILE=nope bash deploy/build-and-ship.sh 2>&1 ) ); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
grep -q 'stamps: version=v9.9.9' <<<"$OUT" \
  && ok "the cut-release shape stamps the tag it was given" \
  || bad "the cut-release shape never reached the stamps line (rc=$RC)"
grep -q 'release rules selftest: passed' <<<"$OUT" \
  && ok "…and hands the release selftest a CLEAN environment" \
  || bad "the ship stopped at the release selftest (rc=$RC)"
! grep -q 'the release rules selftest FAILED' <<<"$OUT" \
  && ok "…so an inherited override can no longer refuse the ship" \
  || bad "the release selftest refused the ship: the caller's override leaked into it"

echo "== P. the suite itself does not inherit the caller's VERSION_OVERRIDE"
# The same class from the other side: this file re-executes ITSELF with an override in the
# environment. Deleting the `unset VERSION_OVERRIDE` at the top turns this RED.
OUT=$(VERSION_OVERRIDE=v9.9.9 RELEASE_SELFTEST_PROBE=1 bash "$HERE/release_selftest.sh" 2>&1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 0 ] && [ "$OUT" = "clean" ] \
  && ok "a caller's VERSION_OVERRIDE is dropped before any case runs" \
  || bad "the suite inherited its caller's VERSION_OVERRIDE (rc=$RC)"

echo "== Q. the tag-pinned release tree is SEEDED before it ships"
# THE DEFECT (measured on v1.27.0): cut-release ships from a DETACHED worktree pinned to
# the tag. That worktree has no web/node_modules (gitignored), so for a web-touching
# release the pre-build gate fail-stops with "web/node_modules/.bin/next is missing", and
# the per-tree record's `webdeps` component reads `none` — which can never match a record
# the merge train wrote for the SAME tree, so the reuse is dead as well.
#
# Proven WITHOUT docker, npm or a network: deploy/build-and-ship.sh is replaced by a
# WITNESS that refuses to "ship" a tree that is not seeded with the dependency set the
# per-tree record names, and refuses to run anywhere but the temp release worktree.
# Deleting the `wm_seed` call from cut-release turns every case below RED.
QREPO=$S/qrepo
QENV=$S/qenv.sh
printf 'export JOMFORM_TEST_DATABASE_URL=postgres://u:p@127.0.0.1:5432/fixture\n' > "$QENV"
mkdir -p "$S/bin"
cat > "$S/bin/gh" <<'GH'
#!/usr/bin/env bash
echo "gh-stub $*"
GH
# The ssh stub answers the ONE question deploy/ship_proof.sh asks — "which version is the
# running <service> container?" — without a network, and records that it was asked. Cases
# that must NOT reach the server (a refused cut, a failed ship) assert it was never called.
# STUB_LIVE_VERSION is the version this fixture world says prod is running: v1.1.0 unless a
# case says otherwise, because a case where the answer is the OLD version is how
# "the ship returned 0 but prod never got it" is reproduced.
cat > "$S/bin/ssh" <<'SSH'
#!/usr/bin/env bash
last=""
for a in "$@"; do last=$a; done
printf 'ssh-arg: %s\n' "$last" >> "${SSH_STUB_LOG:-/dev/null}"
printf '%s\n' "${STUB_LIVE_VERSION-v1.1.0}"
if [ "${SSH_STUB_FAIL:-0}" != 0 ]; then exit "$SSH_STUB_FAIL"; fi
exit 0
SSH
chmod +x "$S/bin/gh" "$S/bin/ssh"

qfixture() {   # $1 = sandbox dir -> sets QD, QTREE, QDEPS, QGATE
  QD=$1
  QGATE="$1.gate"
  rm -rf "$QD" "$QD.git-origin" "$QD.donor2" "$QGATE"
  rm -f "$QD.ssh.log"
  mkdir -p "$QD/deploy" "$QD/web" "$QGATE"
  cp "$HERE/cut-release.sh" "$HERE/web_modules.sh" "$HERE/ship_proof.sh" "$HERE/release_scope.sh" "$QD/deploy/"
  # The mirror is a STUB here: the release path must be unable to reach it on an unshipped
  # tree, and "SYNC STUB RAN" in the output is how a case proves it did or did not.
  cat > "$QD/deploy/sync-oss.sh" <<'SYNCSTUB'
#!/usr/bin/env bash
printf 'SYNC STUB RAN: %s\n' "$*"
exit 0
SYNCSTUB
  chmod +x "$QD/deploy/sync-oss.sh"
  printf 'node_modules/\n' > "$QD/.gitignore"
  printf '{"name":"web","lockfileVersion":3}\n' > "$QD/web/package-lock.json"
  # The WITNESS is the ship. Its three assertions ARE the acceptance: it ran in the
  # tag-pinned temp worktree, that worktree was seeded, and the seeded dependency set is
  # the one the record names — and seeding left the tree CLEAN, so the real builder's
  # dirty-tree guard would still pass.
  cat > "$QD/deploy/build-and-ship.sh" <<'WITNESS'
#!/usr/bin/env bash
# REQUIRE_SHIP is how cut-release says "this run is a deploy". A cut that hands the ship a
# build-only run is the v1.27.3 defect: the child builds, exits 0, and the caller reads that
# as "prod has it". Deleting `REQUIRE_SHIP=1` from either call site turns section Q RED here.
if [ "${REQUIRE_SHIP:-0}" != 1 ]; then
  echo "WITNESS FAIL: cut-release did not pass REQUIRE_SHIP=1 — this is a build-only run and it must never be read as a ship" >&2
  exit 1
fi
printf 'SHIP image=%s version=%s cwd=%s\n' "${IMAGE:-jomform-api}" "${VERSION_OVERRIDE:-none}" "$PWD"
case "$PWD" in
  */release-tree.*/tree) ;;
  *) echo "WITNESS FAIL: the ship did not run in a tag-pinned temp worktree ($PWD)" >&2; exit 1 ;;
esac
if [ -n "$(git status --porcelain)" ]; then
  echo "WITNESS FAIL: the seeded tree is DIRTY — the real builder refuses a dirty tree:" >&2
  git status --porcelain >&2
  exit 1
fi
[ -x web/node_modules/.bin/next ] || {
  echo "WITNESS FAIL: web/node_modules/.bin/next is missing in $PWD — the release tree was shipped UNSEEDED" >&2
  exit 1
}
got=$(sha256sum web/node_modules/.package-lock.json | awk '{print $1}')
echo "WITNESS webdeps=$got"
[ "$got" = "${EXPECT_WEBDEPS:-}" ] || {
  echo "WITNESS FAIL: webdeps $got != expected ${EXPECT_WEBDEPS:-<unset>}" >&2
  exit 1
}
exit 0
WITNESS
  chmod +x "$QD/deploy/build-and-ship.sh"
  # The DONOR: an install that already exists on the host (another worktree, in real life).
  mkdir -p "$QD/web/node_modules/.bin"
  printf '#!/bin/sh\nexit 0\n' > "$QD/web/node_modules/.bin/next"
  chmod +x "$QD/web/node_modules/.bin/next"
  printf '{"name":"next","packages":{"node_modules/next":{"version":"16.0.0"}}}\n' \
    > "$QD/web/node_modules/.package-lock.json"
  QDEPS=$(sha256sum "$QD/web/node_modules/.package-lock.json" | awk '{print $1}')
  (
    cd "$QD" || exit 1
    git init -q .
    git config user.name "Release Selftest"
    git config user.email "selftest@users.noreply.github.com"
    printf 'hello\n' > README.md
    printf 'export const x = 1;\n' > web/page.tsx
    git add -A && git commit -q -m "initial"
    git branch -M main
    git init -q --bare "$QD.git-origin"
    git remote add origin "$QD.git-origin"
    git tag -a v1.0.0 -m "Release v1.0.0"
    git push -q -u origin main v1.0.0
    printf 'export const x = 2;\n' > web/page.tsx
    git commit -q -am "feat(web): a change worth releasing"
    git push -q origin main
  ) || { bad "could not build the release fixture $QD"; return 1; }
  QTREE=$(git -C "$QD" rev-parse 'HEAD^{tree}')
  return 0
}

# qadd_donor <marker> — a SECOND worktree of the same repo holding an install of the
# SAME web/package-lock.json but a DIFFERENT .package-lock.json (a different install of
# the same dependency set). This is what makes "the record's bytes won, not merely a
# compatible install" testable: the seeder must pick THIS one when the record names it,
# even though the main worktree comes first in `git worktree list`.
qadd_donor() {   # $1 = payload marker -> sets QRDEPS
  local d="$QD.donor2"
  git -C "$QD" worktree add -q --detach "$d" HEAD || return 1
  mkdir -p "$d/web/node_modules/.bin"
  printf '#!/bin/sh\nexit 0\n' > "$d/web/node_modules/.bin/next"
  chmod +x "$d/web/node_modules/.bin/next"
  printf '{"name":"next","packages":{"node_modules/next":{"version":"16.0.0"},"marker":"%s"}}\n' "$1" \
    > "$d/web/node_modules/.package-lock.json"
  QRDEPS=$(sha256sum "$d/web/node_modules/.package-lock.json" | awk '{print $1}')
  return 0
}

qrecord() {   # $1 = webdeps the per-tree record names (the reuse key)
  cat > "$QGATE/$QTREE.json" <<REC
{
  "format": 1,
  "tree": "$QTREE",
  "key": "fixture",
  "key_parts": "go=go1.0.0;webdeps=$1;gatescript=fixture;resultlib=fixture;testdb=loopback:5432/fixture;extra=fixture",
  "gates": {},
  "passed": 0,
  "failed": 0,
  "runs": 1,
  "verdict": "green",
  "complete": true,
  "finished_at": "2026-01-01T00:00:00Z"
}
REC
}

qcut() {      # $1 = webdeps the witness must see; rest = extra env assignments for the child
  local want=$1; shift
  ( cd "$QD" && env PATH="$S/bin:$PATH" PREFLIGHT_RESULT_DIR="$QGATE" TEST_ENV_FILE="$QENV" \
      SSH_HOST=stub-deploy-host SSH_STUB_LOG="$QD.ssh.log" \
      EXPECT_WEBDEPS="$want" "$@" \
      bash deploy/cut-release.sh --yes --no-oss 2>&1 )
}
qshiponly() {  # $1 = webdeps the witness must see
  ( cd "$QD" && env PATH="$S/bin:$PATH" PREFLIGHT_RESULT_DIR="$QGATE" TEST_ENV_FILE="$QENV" \
      SSH_HOST=stub-deploy-host SSH_STUB_LOG="$QD.ssh.log" \
      EXPECT_WEBDEPS="$1" bash deploy/cut-release.sh --yes --ship-only --no-oss 2>&1 )
}
# qcutmirror — the same cut with the MIRROR ENABLED (it is a stub that prints SYNC STUB RAN).
# Used only by the cases that assert the mirror cannot be reached.
qcutmirror() {  # <flags for cut-release> <extra env assignments...>
  local flags=$1; shift
  # shellcheck disable=SC2086  # flags is a literal we control ("--no-ship", "", ...)
  ( cd "$QD" && env PATH="$S/bin:$PATH" PREFLIGHT_RESULT_DIR="$QGATE" TEST_ENV_FILE="$QENV" \
      SSH_HOST=stub-deploy-host SSH_STUB_LOG="$QD.ssh.log" \
      EXPECT_WEBDEPS="$QDEPS" "$@" \
      bash deploy/cut-release.sh --yes $flags 2>&1 )
}

# Q1 — a record for the tagged tree names a dependency set that exists on the host in a
# DIFFERENT worktree than the one `git worktree list` names first: the release tree must
# be seeded with THOSE bytes (not merely with a compatible install), because only those
# make the record reusable.
if qfixture "$QREPO"; then
  qadd_donor record
  qrecord "$QRDEPS"
  OUT=$(qcut "$QRDEPS"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "a web-touching release reaches RELEASED with no hand-seeded worktree" \
    || bad "expected exit 0, got $RC"
  grep -q 'RELEASED v1.1.0' <<<"$OUT" && ok "released the bumped version" || bad "never reached RELEASED"
  grep -q 'web deps match the gate record' <<<"$OUT" \
    && ok "names the record it can reuse" || bad "the reuse the seed earns is not reported"
  grep -q 'REUSES that record instead of taking a gate slot' <<<"$OUT" \
    && ok "says the ship will reuse the record rather than gate again" || bad "reuse not announced"
  [ "$(grep -c '^WITNESS webdeps=' <<<"$OUT")" -eq 2 ] \
    && ok "both images shipped from a SEEDED release tree (api + web)" \
    || bad "expected 2 seeded ship witnesses, got $(grep -c '^WITNESS webdeps=' <<<"$OUT")"
  grep -q "^WITNESS webdeps=$QRDEPS" <<<"$OUT" \
    && ok "the seeded set IS the record's webdeps — the record's install beat the first-listed worktree" \
    || bad "the seeded dependency set is not the one the record names (a merely compatible install won)"
  grep -q 'test environment:' <<<"$OUT" \
    && ok "the test environment is loaded before gating (the fingerprint depends on it)" \
    || bad "the test env file was not loaded"
  # THE POST-SHIP ASSERTION RAN. "the ship returned 0" is not the acceptance criterion any
  # more: cut-release asks the server and says so, once per image.
  [ "$(grep -c 'verified: prod runs v1.1.0' <<<"$OUT")" -eq 1 ] \
    && ok "the API ship is confirmed against prod, not against the child's exit code" \
    || bad "cut-release did not verify the API image on prod"
  grep -q "verified: prod's web container runs v1.1.0" <<<"$OUT" \
    && ok "the web ship is confirmed against prod too" || bad "the web image was not verified on prod"
  # ...and the question was actually ASKED OF BOTH SERVICES. The say() above is printed
  # unconditionally, so without this the API half could be deleted while the web half still
  # caught a bad prod (and every case above stayed green).
  grep -q "ps -q 'api'" "$QD.ssh.log" 2>/dev/null \
    && ok "the API proof asked prod which container is running" || bad "prod was never asked about api"
  grep -q "ps -q 'web'" "$QD.ssh.log" 2>/dev/null \
    && ok "the web proof asked prod which container is running" || bad "prod was never asked about web"
else
  bad "section Q fixture could not be built"
fi

# Q2 — the record names a dependency set this host does not have, but an install of the
# SAME web/package-lock.json does: the gates can run, and the ship must say it gates in
# FULL instead of pretending the record vouches for those bytes.
if qfixture "$QREPO"; then
  qrecord "$(printf '1%.0s' $(seq 64))"
  OUT=$(qcut "$QDEPS"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "an unmatched record does not block the release" || bad "expected exit 0, got $RC"
  grep -q 'gates in FULL (a record cannot vouch for a different dependency set)' <<<"$OUT" \
    && ok "says the unmatched dependency set means a FULL gate" || bad "the loss of reuse is not reported"
  grep -q "^WITNESS webdeps=$QDEPS" <<<"$OUT" \
    && ok "shipped the same-lockfile install rather than an arbitrary one" \
    || bad "the ship did not get the same-lockfile dependency set"
else
  bad "section Q fixture could not be built"
fi

# Q3 — no donor anywhere and npm cannot run: the ship must NOT start, must name the seed as
# the reason, and must LEAVE THE TAG (the tag is the record of the decision).
#
# "NO DONOR" HAS TO BE ENFORCED, NOT ASSUMED. deploy/web_modules.sh deliberately borrows an
# install from this host (PASS 1: the exact webdeps a record names; PASS 2: the same
# lockfile), so a real machine that has ever built web/ hands the sandbox a donor and the
# seeder SUCCEEDS — the run then dies later on the witness check with a different banner.
# That is what made this case fail on this host while passing on a clean one. Every donor
# source is therefore emptied for this case only: WM_REPO is a directory that is not a git
# repository (so `worktree list` yields nothing), and the explicit override paths are blank.
NODONOR=$(mktemp -d)
if qfixture "$QREPO"; then
  rm -rf "$QD/web/node_modules"
  OUT=$(qcut x WM_NPM=/bin/false WM_REPO="$NODONOR" WM_TEMPLATE= WM_CACHE_DIR= \
        WM_LOCK_CACHE= RELEASE_NODE_MODULES=); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 5 ] && ok "an unseedable release tree exits 5 instead of shipping" \
    || bad "expected exit 5, got $RC"
  grep -q 'SHIP NOT STARTED' <<<"$OUT" && ok "says the ship did not start" || bad "no refusal banner"
  grep -q 'tag and release REMAIN' <<<"$OUT" && ok "says the tag remains the record" || bad "the tag's status is not stated"
  ! grep -q '^SHIP image=' <<<"$OUT" && ok "NOTHING was built or shipped" || bad "the ship ran on an unseeded tree"
  git -C "$QD" rev-parse --verify --quiet refs/tags/v1.1.0 >/dev/null \
    && ok "the tag survived the failed ship" || bad "the tag was removed by a failed ship"
else
  bad "section Q fixture could not be built"
fi

# Q4 — a GENUINE ship failure keeps the tag, and --ship-only is a real recovery: it
# recreates the release tree from the tag and SEEDS it again.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$(qcut deadbeef); RC=$?
  [ "$RC" -eq 5 ] && ok "a failed ship exits 5" || bad "expected exit 5 from a failing ship, got $RC"
  grep -q 'the tag and release REMAIN' <<<"$OUT" && ok "the failed ship keeps the tag" || bad "tag status not stated"
  OUT=$(qshiponly "$QDEPS"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "--ship-only RECOVERS the failed ship" || bad "expected exit 0 from --ship-only, got $RC"
  grep -q 'retrying for existing tag v1.1.0' <<<"$OUT" && ok "the retry ships the EXISTING tag" || bad "the retry did not name the tag"
  [ "$(grep -c '^WITNESS webdeps=' <<<"$OUT")" -eq 2 ] \
    && ok "--ship-only seeds the worktree it recreates (api + web)" \
    || bad "--ship-only shipped an unseeded tree"
else
  bad "section Q fixture could not be built"
fi

# Q5 — no record at all (a release of a tree nothing has gated yet): the tree is still
# seeded from the host's same-lockfile install, and the ship says it gates in full.
if qfixture "$QREPO"; then
  OUT=$(qcut "$QDEPS"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "a never-gated tree still ships (seeded, then gated in full)" \
    || bad "expected exit 0, got $RC"
  grep -q 'no gate record for tree' <<<"$OUT" && ok "says there is no record to reuse" || bad "the missing record is not reported"
  grep -q "^WITNESS webdeps=$QDEPS" <<<"$OUT" && ok "seeded from the same-lockfile install" || bad "not seeded"
else
  bad "section Q fixture could not be built"
fi

# Q6 — THE v1.27.3 DEFECT, REPRODUCED. Nothing in this environment sets SSH_HOST, so a cut
# that must ship cannot reach prod. It must REFUSE (exit 7) BEFORE tagging, build nothing,
# and never reach the mirror — instead of exiting 0, printing "shipped"/"mirrored"/"RELEASED",
# and letting the public repo advertise a release prod never received.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$( cd "$QD" && env -u SSH_HOST -u REMOTE_BUILD_HOST \
           PATH="$S/bin:$PATH" PREFLIGHT_RESULT_DIR="$QGATE" TEST_ENV_FILE="$QENV" \
           EXPECT_WEBDEPS="$QDEPS" bash deploy/cut-release.sh --yes 2>&1 ); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 7 ] && ok "an unshippable cut exits 7" || bad "expected exit 7, got $RC"
  grep -q 'REFUSING to cut v1.1.0' <<<"$OUT" && ok "names the refusal and the version" \
    || bad "the refusal is not explained"
  grep -q 'neither SSH_HOST' <<<"$OUT" && ok "says what is missing" || bad "the missing setting is not named"
  grep -q 'NOTHING was tagged' <<<"$OUT" && ok "says nothing was tagged" || bad "tag state not stated"
  git -C "$QD" rev-parse --verify --quiet refs/tags/v1.1.0 >/dev/null \
    && bad "an unshippable release was tagged anyway" || ok "no tag was created"
  ! grep -q '^SHIP image=' <<<"$OUT" && ok "nothing was built or shipped" || bad "the ship ran without a server"
  ! grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "the mirror was never reached" || bad "an unshipped tree was mirrored"
  ! grep -q 'RELEASED v1.1.0' <<<"$OUT" && ok "never claims RELEASED" || bad "claims RELEASED without shipping"
else
  bad "section Q fixture could not be built"
fi

# Q7 — THE CHILD SAYS OK, PROD SAYS NO. This is the shape that actually happened: the ship
# returned 0 (there it was a skipped scp; here it is a recreate that left the old image), and
# the release claimed success. cut-release must ask prod itself and stop before the mirror.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$(qcutmirror "" STUB_LIVE_VERSION=v1.0.0); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 5 ] && ok "a ship that prod did not receive exits 5" || bad "expected exit 5, got $RC"
  grep -q 'SHIP NOT PROVEN for v1.1.0' <<<"$OUT" && ok "says the ship is not proven" \
    || bad "the unproven ship is not reported"
  grep -q "labelled version=v1.0.0" <<<"$OUT" && ok "names what prod is actually running" \
    || bad "does not say what prod runs"
  ! grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "the mirror was never reached" || bad "mirrored a tree prod did not receive"
  ! grep -q 'RELEASED' <<<"$OUT" && ok "never claims RELEASED" || bad "claims RELEASED without an unproven ship"
  git -C "$QD" rev-parse --verify --quiet refs/tags/v1.1.0 >/dev/null \
    && ok "the tag survives the unproven ship (it is the record of the decision)" \
    || bad "the tag was removed by an unproven ship"
else
  bad "section Q fixture could not be built"
fi

# Q8 — the control for Q6/Q7: the SAME fixture, the SAME flags, prod actually running the new
# tag -> the release completes and the mirror IS reached. Without this the two refusals above
# could be satisfied by a script that never mirrors anything.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$(qcutmirror ""); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "a verified ship reaches RELEASED with the mirror enabled" \
    || bad "expected exit 0, got $RC"
  grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "a SHIPPED tree is mirrored" || bad "the mirror was not reached on a good ship"
  grep -q 'RELEASED v1.1.0' <<<"$OUT" && ok "reports RELEASED" || bad "never reached RELEASED"
else
  bad "section Q fixture could not be built"
fi


# ============================================================================
# R. build-and-ship: the ship path, and the proof that makes it a ship
#
# The v1.27.3 defect had two halves: cut-release never required a ship, and
# build-and-ship printed "SSH_HOST not set — skipping scp" as an INFO line and exited 0, so
# even a caller that looked at the exit code was told the build succeeded and nothing more.
# Section Q covers the caller (cut-release refuses, and cannot reach the mirror). This section
# covers the callee, on the SHIPPED bytes with docker/ssh/scp stubbed:
#
#   R1  --ship with no reachable server -> exit 7 BEFORE the gate and BEFORE any build.
#   R2  --ship with a server -> it performs load + tag :latest + recreate ITSELF and exits 0,
#       saying the running container carries the tag.
#   R3  the recreate leaves the OLD image running -> exit 7 and it names what prod runs.
#       This is the assertion biting: without it, R2's "success" is just a command that ran.
#   R4  control: WITHOUT --ship the same tree still builds and exits 0, and never tags or
#       recreates anything — a build-only run must stay possible and must never look like a
#       ship (this is what DEPLOY.md's documented usage relies on).
# ============================================================================
echo "== R. build-and-ship: a ship is performed and PROVEN (or refused)"
RWS="$S/shipws"
RBIN="$RWS/bin"
rm -rf "$RWS" "$S/shipws-state"
mkdir -p "$RWS/deploy" "$RWS/api" "$RWS/compose" "$RBIN" "$S/shipws-state"
cp "$HERE/build-and-ship.sh" "$HERE/require-release-tag.sh" "$HERE/ship_proof.sh" "$RWS/deploy/"
: > "$RWS/api/Dockerfile"
cat > "$RBIN/docker" <<'DOCKER'
#!/usr/bin/env bash
set -uo pipefail
log=${DOCKER_STUB_LOG:-/dev/null}
state=${DOCKER_STUB_STATE:?}
printf 'docker %s\n' "$*" >>"$log"
cmd=${1:-}
[ $# -gt 0 ] && shift
case "$cmd" in
  build)
    ver=""; tag=""
    while [ $# -gt 0 ]; do
      case "${1:-}" in
        --build-arg) case "${2:-}" in VERSION=*) ver=${2#VERSION=} ;; esac; shift 2 ;;
        --tag) tag=${2:-}; shift 2 ;;
        *) shift ;;
      esac
    done
    mkdir -p "$state/images" "$state/labels"
    printf 'id-%s\n' "$ver" > "$state/images/$tag"
    printf '%s\n' "$ver" > "$state/labels/id-$ver"
    ;;
  image)   # image inspect --format FMT <ref>
    ref=${!#}
    [ -f "$state/images/$ref" ] || { printf 'no such image: %s\n' "$ref" >&2; exit 1; }
    cat "$state/images/$ref"
    ;;
  save)    # save <ref> -> bytes on stdout; gzip accepts an empty stream
    ;;
  load)    # the tag lives inside the tarball, which this stub cannot read: recorded only
    ;;
  tag)
    src=${1:-}; dst=${2:-}
    [ -f "$state/images/$src" ] || { printf 'no such image: %s\n' "$src" >&2; exit 1; }
    cp "$state/images/$src" "$state/images/$dst"
    ;;
  compose)
    sub=${1:-}
    [ $# -gt 0 ] && shift
    svc=${!#}
    case "$sub" in
      ps)
        [ -f "$state/running/$svc" ] && printf 'ctr-%s\n' "$svc"
        ;;
      up)
        # STUB_STALE=1 is "compose said OK and nothing changed" — the failure mode a bare
        # exit code cannot see (issue #576 shipped a web container exactly that way).
        if [ "${STUB_STALE:-0}" != 1 ]; then
          mkdir -p "$state/running"
          cp "$state/images/${STUB_LATEST_REF:-jomform-api:latest}" "$state/running/$svc" 2>/dev/null \
            || printf 'unknown\n' > "$state/running/$svc"
        fi
        ;;
    esac
    ;;
  inspect)  # inspect --format FMT ctr-<svc>
    ref=${!#}
    id=$(cat "$state/running/${ref#ctr-}" 2>/dev/null || printf 'missing')
    cat "$state/labels/$id" 2>/dev/null || printf '\n'
    ;;
esac
exit 0
DOCKER
# ssh runs the remote command HERE, so "the server" is the same docker stub. That is what
# makes the load/tag/recreate/proof sequence observable without a network or a container.
cat > "$RBIN/ssh" <<'SSH'
#!/usr/bin/env bash
last=""
for a in "$@"; do last=$a; done
exec bash -c "$last"
SSH
cat > "$RBIN/scp" <<'SCP'
#!/usr/bin/env bash
printf 'scp %s\n' "$*" >> "${DOCKER_STUB_LOG:-/dev/null}"
exit 0
SCP
chmod +x "$RBIN/docker" "$RBIN/ssh" "$RBIN/scp"
(
  cd "$RWS" || exit 1
  git init -q .
  git config user.name "Release Selftest"
  git config user.email "selftest@users.noreply.github.com"
  git add -A
  git commit -q -m "ship sandbox"
  git tag -a v1.0.0 -m "Release v1.0.0"
) >/dev/null 2>&1

rship() {   # <logfile> <args for build-and-ship> <extra env assignments...>
  local log=$1; shift
  local sargs=$1; shift
  : > "$log"
  # shellcheck disable=SC2086  # sargs is a literal we control ("--ship" or "")
  ( cd "$RWS" && env PATH="$RBIN:$PATH" \
      DOCKER_STUB_LOG="$log" DOCKER_STUB_STATE="$S/shipws-state" \
      LOCK_FILE="$S/shipws.lock" OUT_DIR="$S/shipws-out" \
      REMOTE_COMPOSE_DIR="$RWS/compose" \
      SKIP_TESTS=1 SKIP_RELEASE_SELFTEST=1 \
      "$@" bash deploy/build-and-ship.sh $sargs 2>&1 )
}

# R1 — no server, but a ship was asked for: refused before anything is built.
RLOG="$S/r1.docker.log"
OUT=$(rship "$RLOG" "--ship"); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 7 ] && ok "R1 --ship with no SSH_HOST/REMOTE_BUILD_HOST exits 7" \
  || bad "R1 expected exit 7, got $RC"
grep -q 'nothing can reach a server' <<<"$OUT" && ok "R1 says the server is unreachable" \
  || bad "R1 the refusal is not explained"
! grep -q 'docker build' "$RLOG" && ok "R1 refused BEFORE building an image" \
  || bad "R1 it gated/built first and only then noticed it could not ship"

# R2 — a reachable server: the server half runs here, and the proof passes.
RLOG="$S/r2.docker.log"
OUT=$(rship "$RLOG" "--ship" SSH_HOST=stub-deploy-host); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 0 ] && ok "R2 a shipped run exits 0" || bad "R2 expected exit 0, got $RC"
grep -q 'scp -P 22' "$RLOG" && ok "R2 the tarball is scp'd" || bad "R2 nothing was scp'd"
grep -q 'docker load -i' "$RLOG" && ok "R2 the image is loaded on the server" || bad "R2 not loaded"
grep -q 'docker tag jomform-api:v1.0.0 jomform-api:latest' "$RLOG" \
  && ok "R2 :latest is re-pointed at the release tag" || bad "R2 :latest was not tagged"
grep -q 'docker compose up -d --force-recreate api' "$RLOG" \
  && ok "R2 the service is recreated" || bad "R2 the service was not recreated"
grep -q 'SHIPPED jomform-api:v1.0.0' <<<"$OUT" && ok "R2 reports the ship only after the proof" \
  || bad "R2 does not report a verified ship"
! grep -q 'run manually' <<<"$OUT" && ok "R2 leaves no hand-run steps behind" \
  || bad "R2 still tells the operator to finish by hand"

# R3 — the recreate returns 0 and the OLD image is still what runs.
# This is the assertion, not the command: without it R2's success means "the commands ran".
RLOG="$S/r3.docker.log"
rm -rf "$S/shipws-state"; mkdir -p "$S/shipws-state/running" "$S/shipws-state/labels"
printf 'id-v0.9.0\n' > "$S/shipws-state/running/api"
printf 'v0.9.0\n' > "$S/shipws-state/labels/id-v0.9.0"
OUT=$(rship "$RLOG" "--ship" SSH_HOST=stub-deploy-host STUB_STALE=1); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 7 ] && ok "R3 a recreate that left the old image exits 7" \
  || bad "R3 expected exit 7, got $RC"
grep -q 'SHIP NOT PROVEN' <<<"$OUT" && ok "R3 says the ship is not proven" \
  || bad "R3 the unproven ship is not reported"
grep -q 'labelled version=v0.9.0' <<<"$OUT" && ok "R3 names the version that is actually running" \
  || bad "R3 does not say what the server is running"

# R4 — the control: a build-only run still works, and never touches the running stack.
RLOG="$S/r4.docker.log"
rm -rf "$S/shipws-state"; mkdir -p "$S/shipws-state"
OUT=$(rship "$RLOG" ""); RC=$?
printf '%s\n' "$OUT" | sed 's/^/    /'
[ "$RC" -eq 0 ] && ok "R4 a build-only run still exits 0" || bad "R4 expected exit 0, got $RC"
grep -q 'docker save' "$RLOG" && ok "R4 the tarball is produced" || bad "R4 no tarball"
grep -q 'Remaining steps ON THE SERVER' <<<"$OUT" && ok "R4 prints the server steps" \
  || bad "R4 the manual steps are gone"
! grep -Eq 'docker (load|tag|compose)' "$RLOG" \
  && ok "R4 a build-only run never loads, tags or recreates anything" \
  || bad "R4 a build-only run touched the running stack"
! grep -q 'SHIPPED ' <<<"$OUT" && ok "R4 never claims a ship" || bad "R4 a build-only run claimed a ship"

# Q9 — --no-ship with the mirror is a contradiction, and it is refused rather than "released".
# The header has always said "nothing public points at a tree that did not ship"; this is the
# flag combination that would have broken it without any environment being wrong.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$(qcutmirror "--no-ship"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 2 ] && ok "Q9 --no-ship with the mirror is refused (exit 2)" || bad "Q9 expected exit 2, got $RC"
  grep -q 'nothing public points at a tree that did not ship' <<<"$OUT" \
    && ok "Q9 names the invariant it would break" || bad "Q9 the refusal is not explained"
  git -C "$QD" rev-parse --verify --quiet refs/tags/v1.1.0 >/dev/null \
    && bad "Q9 it tagged before noticing the contradiction" || ok "Q9 refused before tagging"
  ! grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "Q9 the mirror was never reached" || bad "Q9 it mirrored anyway"
fi

# Q10 — the control for Q9: tag+publish ONLY is still a legitimate step, and it says so.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  OUT=$(qcutmirror "--no-ship --no-oss"); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "Q10 --no-ship --no-oss still tags and publishes" || bad "Q10 expected exit 0, got $RC"
  grep -q 'CUT v1.1.0' <<<"$OUT" && ok "Q10 reports it as CUT, not RELEASED" || bad "Q10 overstates the run"
  grep -q 'NOTHING was shipped' <<<"$OUT" && ok "Q10 says nothing was shipped" || bad "Q10 does not say so"
  ! grep -q '^SHIP image=' <<<"$OUT" && ok "Q10 no image was built" || bad "Q10 it shipped something"
  git -C "$QD" rev-parse --verify --quiet refs/tags/v1.1.0 >/dev/null \
    && ok "Q10 the tag exists (the decision is recorded)" || bad "Q10 the tag is missing"
fi

# Q11 — --oss-only (the documented mirror retry) is not a licence to mirror an unverified
# tree: it proves the tag is live on prod first, and refuses without that.
if qfixture "$QREPO"; then
  qrecord "$QDEPS"
  ( cd "$QD" && git tag -a v1.1.0 -m "Release v1.1.0" && git push -q origin v1.1.0 ) >/dev/null 2>&1
  OUT=$( cd "$QD" && env -u SSH_HOST -u REMOTE_BUILD_HOST PATH="$S/bin:$PATH" \
           bash deploy/cut-release.sh --yes --oss-only 2>&1 ); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 7 ] && ok "Q11 --oss-only without a way to verify prod exits 7" || bad "Q11 expected exit 7, got $RC"
  ! grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "Q11 it did not mirror unverified" || bad "Q11 it mirrored unverified"
  OUT=$( cd "$QD" && env -u REMOTE_BUILD_HOST PATH="$S/bin:$PATH" SSH_HOST=stub-deploy-host \
           STUB_LIVE_VERSION=v1.0.0 bash deploy/cut-release.sh --yes --oss-only 2>&1 ); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 7 ] && ok "Q11 --oss-only refuses when prod runs an older tag" || bad "Q11 expected exit 7, got $RC"
  ! grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "Q11 still did not mirror" || bad "Q11 mirrored an unshipped tag"
  OUT=$( cd "$QD" && env -u REMOTE_BUILD_HOST PATH="$S/bin:$PATH" SSH_HOST=stub-deploy-host \
           STUB_LIVE_VERSION=v1.1.0 bash deploy/cut-release.sh --yes --oss-only 2>&1 ); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "Q11 --oss-only mirrors once the tag is verified on prod" || bad "Q11 expected exit 0, got $RC"
  grep -q 'SYNC STUB RAN' <<<"$OUT" && ok "Q11 the mirror ran on the verified tag" || bad "Q11 the mirror never ran"
fi

echo "== S. a diff that ships no image is REFUSED (this is how v1.27.4 took \"Latest\")"
# The whole defect, end to end: a window of deploy/-only commits, the same shape as v1.25.1,
# v1.27.2 and v1.27.4. The cutter must refuse BEFORE the tag exists — a tag is the record of
# the decision, and this is the decision not to make one. --tag-only is the sanctioned way to
# record a tooling milestone: it publishes no GitHub release, so "Latest" keeps pointing at
# the version prod runs.
SREPO=$S/srepo; SORIGIN=$S/srepo.git; SGH=$S/srepo.gh.log
rm -rf "$SREPO" "$SORIGIN"; mkdir -p "$SREPO/deploy" "$SREPO/api"
cp "$HERE/cut-release.sh" "$HERE/release_scope.sh" "$HERE/ship_proof.sh" "$SREPO/deploy/"
cat > "$S/bin/gh" <<'GH'
#!/usr/bin/env bash
printf 'gh %s\n' "$*" >> "${GH_LOG:-/dev/null}"
echo "gh-stub $*"
GH
chmod +x "$S/bin/gh"
scut() {   # <cut-release args...>; no server, so only the pre-tag decisions are reachable
  ( cd "$SREPO" && env -u SSH_HOST -u REMOTE_BUILD_HOST PATH="$S/bin:$PATH" GH_LOG="$SGH" \
      bash deploy/cut-release.sh "$@" 2>&1 )
}
if (
  cd "$SREPO" || exit 1
  git init -q .
  git config user.name "Release Selftest"
  git config user.email "selftest@users.noreply.github.com"
  printf 'FROM scratch\n' > api/Dockerfile
  printf '#!/bin/sh\ntrue\n' > deploy/tool.sh
  git add -A && git commit -q -m "initial"
  git branch -M main
  git init -q --bare "$SORIGIN"
  git remote add origin "$SORIGIN"
  git tag -a v2.0.0 -m "Release v2.0.0"
  git push -q -u origin main v2.0.0
  printf 'tool 1\n' >> deploy/tool.sh && git commit -q -am "chore(deploy): tooling one (#1600)"
  printf 'tool 2\n' >> deploy/tool.sh && git commit -q -am "chore(deploy): tooling two (#1601)"
  printf 'tool 3\n' >> deploy/tool.sh && git commit -q -am "ci: workflow tidy (#1602)"
  git push -q origin main
) >/dev/null 2>&1; then
  : > "$SGH"

  # S1 — the refusal. Exit 3 (nothing to release), the reason in plain words, and no tag:
  # a tag left behind here IS the defect, because `require-release-tag.sh` would then accept
  # those bytes for a deploy and GitHub would advertise them as "Latest".
  OUT=$(scut --yes --no-oss); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 3 ] && ok "a deploy/-only diff is refused with exit 3" || bad "S1 expected exit 3, got $RC"
  grep -q 'nothing to release: the diff ships no image' <<<"$OUT" \
    && ok "S1 says exactly what is missing (no image)" || bad "S1 the refusal is not explained"
  grep -qi 'touch only deploy/' <<<"$OUT" && ok "S1 names deploy/ as the whole delta" || bad "S1 the scope is not named"
  grep -q -- '--tag-only' <<<"$OUT" && ok "S1 offers the honest alternative (--tag-only)" \
    || bad "S1 no remedy named"
  git -C "$SREPO" rev-parse --verify --quiet refs/tags/v2.1.0 >/dev/null \
    && bad "S1 an image-less diff was TAGGED anyway" || ok "S1 nothing was tagged"
  [ ! -s "$SGH" ] && ok "S1 no GitHub release was published" || bad "S1 it published a release"

  # S2 — and --dry-run does not paint a plan either: the refusal is the answer.
  OUT=$(scut --dry-run); RC=$?
  [ "$RC" -eq 3 ] && ok "S2 --dry-run refuses too (exit 3)" || bad "S2 expected exit 3, got $RC"
  ! grep -q 'DRY RUN' <<<"$OUT" && ok "S2 it plans nothing, because there is nothing to plan" \
    || bad "S2 a plan was printed for an unreleasable diff"

  # S3 — --tag-only records the decision without stealing "Latest".
  OUT=$(scut --yes --tag-only); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "S3 --tag-only records the tag (exit 0)" || bad "S3 expected exit 0, got $RC"
  git -C "$SREPO" rev-parse --verify --quiet refs/tags/v2.1.0 >/dev/null \
    && ok "S3 the tag exists" || bad "S3 no tag"
  git -C "$SORIGIN" rev-parse --verify --quiet refs/tags/v2.1.0 >/dev/null \
    && ok "S3 the tag was pushed" || bad "S3 the tag was not pushed"
  grep -q 'no GitHub release (--tag-only)' <<<"$OUT" && ok "S3 it publishes no GitHub release" \
    || bad "S3 it published a release"
  [ ! -s "$SGH" ] && ok "S3 gh was never asked to publish (Latest keeps its meaning)" \
    || bad "S3 gh released it anyway"
  grep -q 'tagged and pushed ONLY' <<<"$OUT" && ok "S3 the final line does not overstate the run" \
    || bad "S3 the final line overstates it"
  ! grep -q 'shipping the tagged tree' <<<"$OUT" && ok "S3 nothing was shipped or mirrored" \
    || bad "S3 it shipped an image-less tree"
  # Remove the record again: S4/S5 measure the SAME deploy-only window, and a tag on it would
  # turn the next runs into the "tag already exists" refusal instead of the case under test.
  ( cd "$SREPO" && git tag -d v2.1.0 >/dev/null 2>&1; git push -q origin --delete v2.1.0 >/dev/null 2>&1 )

  # S4 — --force is the deliberate product decision: it gets PAST the scope gate (and then
  # stops at the next precondition, because this fixture has no server — that is exit 7, a
  # different refusal, which is the proof that --force was accepted).
  OUT=$(scut --yes --force --no-oss); RC=$?
  [ "$RC" -eq 7 ] && ok "S4 --force bypasses the scope gate (stops later, at exit 7)" \
    || bad "S4 expected exit 7 from the ship precondition, got $RC"
  ! grep -q 'nothing to release' <<<"$OUT" && ok "S4 the scope refusal did not fire" \
    || bad "S4 --force was ignored"

  # S5 — NO REGRESSION: one api/ commit and the same window releases, and the notes are a
  # grouped changelog rather than a dump of the diff.
  printf 'package api\n' > "$SREPO/api/thing.go"
  git -C "$SREPO" add -A && git -C "$SREPO" commit -q -m "fix(chat): keep the stopped answer (#1610)"
  printf '\nvar Y = 1\n' >> "$SREPO/api/thing.go"
  git -C "$SREPO" add -A && git -C "$SREPO" commit -q -m "feat(payments): show the fee breakdown (#1611)"
  printf 'notes\n' >> "$SREPO/README.md"
  git -C "$SREPO" add -A && git -C "$SREPO" commit -q -m "chore(deploy): plumbing (#1612)"
  git -C "$SREPO" push -q origin main
  OUT=$(scut --dry-run); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "S5 an api/ commit still plans a release (no regression)" || bad "S5 expected exit 0, got $RC"
  grep -q 'this diff owes an image' <<<"$OUT" && ok "S5 the plan names the image debt" || bad "S5 scope not reported"
  grep -q '^    | # v2.1.0 — ' <<<"$OUT" && ok "S5 the notes are TITLED and DATED" || bad "S5 no dated title"
  grep -q '^    | ## Chat' <<<"$OUT" && ok "S5 the notes group by area (Chat)" || bad "S5 no area headings"
  grep -q '^    | ## Payments' <<<"$OUT" && ok "S5 …and by the second area (Payments)" || bad "S5 one area only"
  grep -q '^    | - keep the stopped answer (#1610)' <<<"$OUT" && ok "S5 each line is human, with its PR" \
    || bad "S5 the bullet does not carry the PR number"
  grep -q '^    | - show the fee breakdown (#1611)' <<<"$OUT" && ok "S5 …and the second one" \
    || bad "S5 second bullet missing"
  grep -q '^    | ## Internal' <<<"$OUT" && ok "S5 plumbing is rolled into Internal" || bad "S5 no Internal rollup"
  grep -q '4 plumbing commit(s)' <<<"$OUT" && ok "S5 the Internal line counts, it does not list" \
    || bad "S5 the Internal line is not a rollup"
  ! grep -qE '^    \| (deploy|README)' <<<"$OUT" && ok "S5 no path dump in the notes" \
    || bad "S5 the notes still look like a build log"
  grep -q 'commits · .* PRs · .* product areas' <<<"$OUT" && ok "S5 aggregate stats are present" \
    || bad "S5 no stats line"

  # S6 — user-visible docs OWE the WEB image: web/scripts/guides.mjs materialises
  # docs/guides into web/src/lib/guides.gen.ts + web/public, so a guide change is a product
  # change. This is the case that makes "docs" a release trigger rather than plumbing.
  mkdir -p "$SREPO/docs/guides/demo"
  printf '# Demo\n\nA guide a user reads.\n' > "$SREPO/docs/guides/demo/README.md"
  git -C "$SREPO" add -A && git -C "$SREPO" commit -q -m "docs(guides): a new guide (#1620)"
  git -C "$SREPO" push -q origin main
  OUT=$(scut --dry-run); RC=$?
  printf '%s\n' "$OUT" | sed 's/^/    /'
  [ "$RC" -eq 0 ] && ok "S6 a docs/guides change is releasable (exit 0)" || bad "S6 expected exit 0, got $RC"
  grep -q 'IMAGE=jomform-web' <<<"$OUT" && ok "S6 …and it owes the WEB image (the guides are baked into it)" \
    || bad "S6 a guide change would ship no web image"
  grep -q '^    | ## Documentation' <<<"$OUT" && ok "S6 the notes file it under Documentation" \
    || bad "S6 the guide is not grouped as Documentation"
else
  bad "section S fixture could not be built"
fi

printf '\nrelease selftest: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
