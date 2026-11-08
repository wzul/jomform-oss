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
for f in "$RRT" "$CUT" "$SYNC"; do
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
git commit -q -am "feat: something worth releasing"
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

printf 'one\n' >> README.md && git commit -q -am "fix: one commit" && git push -q origin main
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


echo "== L. the repair path re-cuts the SAME version (--tag), never a new one"
DROP_TAGS
git push -q origin main 2>/dev/null || true
git tag -a v1.5.0 -m "Release v1.5.0" && git push -q origin v1.5.0
printf 'repair\n' >> README.md && git commit -q -am "fix: needs re-cutting" && git push -q origin main
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

printf '\nrelease selftest: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ] || exit 1
