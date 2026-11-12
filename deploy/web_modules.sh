#!/usr/bin/env bash
#
# web_modules — give a worktree that has none a `web/node_modules`, from the
# closest EXACT copy this host already has.
#
# WHY THIS EXISTS. `web/node_modules` is gitignored, so every worktree created
# from a ref (a merge-train candidate, a release tree pinned to a tag) starts
# without one. Two separate things then break:
#
#   1. THE GATES THAT NEED IT REFUSE TO RUN. `scripts/preflight.sh` will not
#      pretend `npm run build` built anything without `.bin/next` — it fails with
#      "web/node_modules/.bin/next is missing — 'npm run build' would exit 0 with
#      'sh: 1: next: not found' and build NOTHING". That is correct: a gate that
#      silently skips is worse than one that fails. But it means a tool that
#      creates a worktree MUST seed it before it gates.
#   2. THE PER-TREE GATE RECORD CANNOT BE REUSED. The environment fingerprint in
#      scripts/gate_result.sh carries `webdeps` = sha256 of
#      `web/node_modules/.package-lock.json`. An unseeded tree records
#      `webdeps=none`, which can never equal the `webdeps=<sha>` a merge train
#      recorded for the SAME tree — so the release pays the whole inventory again
#      for bytes that were already gated.
#
# Measured on this host, v1.27.0: the merger's FULL GREEN record for tree
# 929631f existed and was minutes old, with
# `webdeps=c62b724d50eeb120f07babb7777d1d3ed89d2211f35499af908a59aee6fc1405`. A
# release tree at that tag read `webdeps=none` and was refused by
# `scripts/preflight_reuse.sh --show` with "the environment changed since that
# record". A donor install with exactly that webdeps existed on the host
# (/root/jf-deploy-t_0afb1e60), which is why the operator's hand-workaround
# (copy node_modules until the sha matches, then ship) worked.
#
# WHAT THIS DOES, in order, and why that order:
#   * PASS 1 — a candidate whose `.package-lock.json` is EXACTLY the webdeps the
#     per-tree record names. This is the only copy that lets the ship REUSE the
#     record, so it wins even if it is not the newest one on the host.
#   * PASS 2 — a candidate installed from the SAME web/package-lock.json as this
#     tree. It cannot guarantee reuse (the record names a different dependency
#     set), but the gates can run and the ship's own preflight says, loudly,
#     that it is gating in full. Never guessed silently: the caller is told when
#     the copy does not match the record.
#   * PASS 3 — `npm ci` in the tree itself, the slow (~81 s) but always available
#     answer. The result is cached so the next release does not pay it again.
#
# WHY `cp -a` AND NOT HARDLINKS. Hardlinking would share node_modules/.cache
# between two trees that may be building at the same time — shared mutable state
# in a gate input, which produces FALSE gate results (the same reason
# deploy/merge-train.sh copies rather than links).
#
# WHY A SYMLINKED DONOR IS REFUSED (card t_eaf5edd4). `cp -a` reproduces a
# symlink AS a symlink, so a donor whose `web/node_modules` was a LINK (many
# long-lived worktrees on this host point it at
# /root/wzul-git/jomform/web/node_modules) handed the candidate a
# `web/node_modules` that was a link to a directory. Nothing reported a problem:
# `cp -a` exits 0, and `[ -x .bin/next ]` FOLLOWS the link and also says the
# tree is installed — the failure lands minutes later inside a guard that walks
# the tree:
#
#   model_default_guard_test.go: walk <candidate> for model ids:
#     read <candidate>/web/node_modules: is a directory
#
# (`filepath.Walk` lstats each entry, so a symlink is never a directory: the
# skipDirs arm for `node_modules` never applies, and the ReadFile hits EISDIR.)
# Measured on the train run of 2026-11-12 14:31 for PR #1733 — the same batch
# went GREEN the moment TRAIN_NODE_MODULES was pointed at a REAL donor, so the
# seeding path was the cause and the tree was innocent.
#
# `cp -aL` would HIDE the same trap instead of fixing it: it silently copies a
# tree this host does not own for this tree (~1 GB per candidate) and the next
# link in the chain reproduces it again. So the rule is one line, applied at
# every door: A SYMLINK IS NOT A DONOR.
#   * wm_usable is the only test a candidate source may use, so a new source
#     cannot reintroduce the trap by remembering `[ -x .bin/next ]` instead;
#   * wm_copy_from refuses a link a second time at the one place that writes;
#   * a tree whose OWN `web/node_modules` is a link is not "already installed"
#     (wm_have), it is re-seeded — so a lane cannot gate through one either.
#
# CALLERS
#   deploy/merge-train.sh   seeds its candidate, caching within the run
#   deploy/cut-release.sh   seeds the tag-pinned release tree
# and the CLI below exists so the seeding can be driven and tested on its own:
#
#   deploy/web_modules.sh seed <worktree> [want-webdeps]
#   deploy/web_modules.sh webdeps <worktree>          # sha256 of .package-lock.json
#   deploy/web_modules.sh record-webdeps <tree-sha>   # the webdeps= a record names
#   deploy/web_modules.sh candidates <worktree>       # the donors it would consider
#
# ENVIRONMENT (all optional; nothing here is required for a default run)
#   WM_REPO            the repo whose worktrees may donate a node_modules
#   WM_TEMPLATE        an explicit node_modules directory to copy from
#   WM_CACHE_DIR       a plain node_modules directory to reuse AND to fill after
#                      `npm ci` (merge-train's $WORK/web_modules: one copy per run)
#   WM_LOCK_CACHE      a KEYED host cache, `<root>/<sha256(web/package-lock.json)>/`
#                      holding `node_modules/` and a `webdeps` marker file
#   RELEASE_NODE_MODULES  an explicit donor, first in line (operator override)
#   WM_NPM             the npm to run (default: npm; a test seam)
#   WM_NPM_LOG         redirect `npm ci` output here (merge-train's $WORK/npm.log)
#   WM_LAUNCH          array: the launcher for `npm ci` (a CPU quota scope)
#   WM_PROG            name the notes are prefixed with
#   PREFLIGHT_RESULT_DIR  the gate record store (default /var/lib/jomform/gate)
#
# Exits: 0 seeded · 1 cannot seed (the caller must say so and NOT gate).
# `wm_seed` also sets WM_MATCHED (1 when the seeded set is the one the record
# named), WM_SEED_WEBDEPS and WM_SEED_SOURCE.
set -o pipefail

WM_PROG=${WM_PROG:-web-modules}
wm_note() { printf '%s: %s\n' "$WM_PROG" "$*"; }
wm_warn() { printf '%s: %s\n' "$WM_PROG" "$*" >&2; }

# wm_sha256_file <path> — the file's sha256, or the literal token `none` when it
# does not exist. `none` is load-bearing: it is what an unseeded tree records as
# `webdeps`, and no real sha256 can collide with it.
wm_sha256_file() {
  [ -r "$1" ] || { printf 'none'; return 0; }
  sha256sum "$1" 2>/dev/null | awk '{print $1}'
}

# wm_dir_webdeps <node_modules-dir> — the webdeps component of a candidate.
wm_dir_webdeps() { wm_sha256_file "${1%/}/.package-lock.json"; }

# wm_webdeps <worktree> — the webdeps component of a worktree.
wm_webdeps() { wm_dir_webdeps "${1:-$PWD}/web/node_modules"; }

# wm_lock_sha <worktree> — sha256 of the tree's web/package-lock.json, or empty
# when it has none. THE IDENTITY OF THE DEPENDENCY SET: two installs of the same
# lockfile are the same dependency set, whatever their .package-lock.json says.
wm_lock_sha() {
  local f="${1:-$PWD}/web/package-lock.json"
  [ -r "$f" ] || { printf ''; return 0; }
  wm_sha256_file "$f"
}

# wm_record_webdeps <tree-sha> — the webdeps a per-tree gate record names, or
# empty when there is no record, no key_parts, or the record says `none`.
# READ-ONLY: this never decides whether a record is reusable (that is
# scripts/preflight_reuse.sh, inside the ship). It only answers "which installed
# dependency set would this record accept", so the seed can prefer those bytes.
wm_record_webdeps() {
  local tree=${1:-} f v
  case "$tree" in ''|none) return 0 ;; esac
  f="${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/$tree.json"
  [ -r "$f" ] || return 0
  v="$(sed -nE 's/^  "key_parts": ".*webdeps=([0-9a-f]{64}).*$/\1/p' "$f" | head -1)"
  case "$v" in ''|none) return 0 ;; esac
  printf '%s' "$v"
}

# wm_is_symlink <path> — THE ONE PLACE the seeder decides a path is a link. A
# test/bite-proof can neuter exactly this function and the whole rule goes with
# it, which is what makes the guard provable instead of merely present.
wm_is_symlink() { [ -L "${1%/}" ]; }

# wm_link_note <path> [own] — say ONCE per run that a node_modules is a SYMLINK and
# what that means for the tree: when it is a DONOR (default) it is skipped, with
# its target named; when it is the tree's OWN (`own`) it is re-seeded. Silence
# here is how the EISDIR above reached a lane: the copy succeeded, the tree looked
# installed, and a guard three minutes later reported "is a directory".
WM_LINK_SAID=${WM_LINK_SAID:-}
wm_link_note() {
  local p=${1%/} role=${2:-donor} t
  case " $WM_LINK_SAID " in *" $p "*) return 0 ;; esac
  WM_LINK_SAID="$WM_LINK_SAID $p"
  t="$(readlink "$p" 2>/dev/null)"
  if [ "$role" = own ]; then
    wm_note "re-seeding ${p%/web/node_modules}: its web/node_modules is a SYMLINK${t:+ -> $t} — a link is not an install this host owns, and the gate that walks this tree cannot read one"
    return 0
  fi
  wm_warn "not using donor $p: web/node_modules is a SYMLINK${t:+ -> $t}. cp -a would reproduce the LINK instead of the tree, and a walk of the seeded tree then dies with 'read .../web/node_modules: is a directory'. A symlink is not an install this host owns — skipped."
}

# wm_usable <node_modules-dir> — is this directory a donor worth copying? It must
# carry `.bin/next` AND be a REAL directory. EVERY candidate source goes through
# this: not `[ -x "$p/.bin/next" ]`, which FOLLOWS a symlink and therefore says
# yes to exactly the donor that poisons the candidate.
wm_usable() {
  local p=${1%/}
  [ -x "$p/.bin/next" ] || return 1
  wm_is_symlink "$p" || return 0
  wm_link_note "$p"
  return 1
}

# wm_have <worktree> — is this tree already installed, as a REAL directory? A tree
# whose web/node_modules is a SYMLINK is NOT installed as far as the gates are
# concerned: the walk above cannot read it, so "have" must not short-circuit past
# the re-seed (that is how one poisoned donor stayed in the tree for the rest of a
# run). wm_seed replaces the link; `rm -rf`/`rm -f` on a link remove only the link.
wm_have() {
  local nm="${1:-$PWD}/web/node_modules"
  [ -x "$nm/.bin/next" ] || return 1
  [ ! -L "$nm" ]
}

# wm_candidates <target-worktree> — fill WM_CANDS with `nmdir<TAB>worktree<TAB>lock`
# for every copy worth considering, in priority order. The lock is computed
# lazily (by the caller) for worktree entries: hashing every lockfile on a host
# with hundreds of worktrees costs seconds, and pass 1 usually hits early.
wm_candidates() {
  local target=${1:-} p
  WM_CANDS=()
  if [ -n "${RELEASE_NODE_MODULES:-}" ] && wm_usable "${RELEASE_NODE_MODULES}"; then
    WM_CANDS+=("${RELEASE_NODE_MODULES%/}"$'\t\t')
  fi
  if [ -n "${WM_TEMPLATE:-}" ] && wm_usable "${WM_TEMPLATE}"; then
    WM_CANDS+=("${WM_TEMPLATE%/}"$'\t\t')
  fi
  if [ -n "${WM_CACHE_DIR:-}" ] && wm_usable "${WM_CACHE_DIR}"; then
    WM_CANDS+=("${WM_CACHE_DIR%/}"$'\t\t')
  fi
  if [ -n "${WM_LOCK_CACHE:-}" ] && [ -d "${WM_LOCK_CACHE%/}" ]; then
    for p in "${WM_LOCK_CACHE%/}"/*/node_modules; do
      wm_usable "$p" || continue
      WM_CANDS+=("$p"$'\t\t'"$(basename "$(dirname "$p")")")
    done
  fi
  if [ -n "${WM_REPO:-}" ]; then
    local wt
    # Process substitution, not a pipe: the array must survive the loop.
    while IFS= read -r wt; do
      [ -n "$wt" ] || continue
      [ "$wt" = "$target" ] && continue
      # wm_usable, never `[ -x .bin/next ]`: a worktree that SYMLINKS its
      # web/node_modules is the exact donor this card measured (two of them are
      # registered on this host), and `-x` follows the link and says yes.
      wm_usable "$wt/web/node_modules" || continue
      WM_CANDS+=("$wt/web/node_modules"$'\t'"$wt"$'\t')
    done < <(git -C "$WM_REPO" worktree list --porcelain 2>/dev/null | sed -n 's/^worktree //p')
  fi
  return 0
}

# wm_cand_lock <entry-fields...> — the lock sha a candidate was installed from,
# computed only when the candidate is actually about to be used.
#
# A candidate that IS a worktree hands its lock over for free. A candidate that
# is only a node_modules path (RELEASE_NODE_MODULES, WM_CACHE_DIR, WM_TEMPLATE)
# still usually LIVES in a worktree — `<wt>/web/node_modules` — so the lock is
# derived from the directory that contains it rather than assumed absent. Without
# that derivation an explicit donor could only ever win PASS 1 (the exact webdeps
# match), and an operator pointing RELEASE_NODE_MODULES at a perfectly good
# install would get an `npm ci` instead.
wm_cand_lock() {   # $1=nm dir, $2=worktree (may be empty), $3=lock (may be empty)
  local nm=${1:-} wt=${2:-} lock=${3:-} outer
  if [ -n "$lock" ]; then printf '%s' "$lock"; return 0; fi
  if [ -z "$wt" ]; then
    wt="${nm%/}"
    case "$wt" in
      */web/node_modules) wt="${wt%/web/node_modules}" ;;
      *) wt="" ;;
    esac
  fi
  if [ -n "$wt" ]; then wm_lock_sha "$wt"; return 0; fi
  printf ''
}

# wm_copy_from <candidate nm dir> <worktree>
wm_copy_from() {
  local src=${1:?} wt=${2:?}
  src="${src%/}"
  # BELT AND BRACES: every candidate already passed wm_usable. This refuses a link
  # a SECOND time, at the one place that writes, so a donor source added later
  # cannot reintroduce the defect by forgetting the check.
  if wm_is_symlink "$src"; then
    wm_link_note "$src"
    wm_warn "refusing to seed $wt from $src: copying it would put a LINK at $wt/web/node_modules, and the gate that walks that tree then fails with 'is a directory'"
    return 1
  fi
  wm_note "seeding web/node_modules from $src (cp -a of a REAL directory; webdeps $(wm_dir_webdeps "$src" | cut -c1-12))"
  mkdir -p "$wt/web" || return 1
  # Remove a PARTIAL install first: `.bin/next` missing does not mean the
  # directory is absent (an interrupted `npm ci` leaves one behind), and copying
  # onto it would land the payload one level too deep (`node_modules/node_modules`).
  # A SYMLINK is removed as a link (`rm` never descends into one), which is how a
  # tree poisoned by an older seeder gets repaired instead of skipped.
  rm -rf "$wt/web/node_modules" 2>/dev/null
  cp -a "$src" "$wt/web/node_modules" || { wm_warn "copy from $src failed"; return 1; }
  wm_have "$wt"
}

# wm_install <worktree> — npm ci in the tree, then fill the caches.
wm_install() {
  local wt=${1:?} log="${WM_NPM_LOG:-}" npm="${WM_NPM:-npm}" launch=()
  # WM_LAUNCH is OPTIONAL (the header says so), and an UNSET array under a caller's
  # `set -u` is an unbound-variable ERROR, not "no launcher" — measured while proving
  # this card's fix live: `WM_LAUNCH: unbound variable` aborted the seed instead of
  # reporting the install's own failure. `${arr[@]+…}` is the expansion the rest of
  # this file already uses for the same reason.
  if [ "${WM_LAUNCH[@]+set}" = set ] && [ "${#WM_LAUNCH[@]}" -gt 0 ]; then launch=("${WM_LAUNCH[@]}"); fi
  command -v "$npm" >/dev/null 2>&1 || { wm_warn "cannot seed: $npm is not in PATH"; return 1; }
  [ -r "$wt/web/package-lock.json" ] || { wm_warn "cannot seed: $wt/web/package-lock.json is missing"; return 1; }
  # A SYMLINK left by an older seeder (or by hand) must go FIRST: `npm ci` would
  # otherwise install THROUGH the link and mutate another tree's install, and the
  # tree would still be gated through a link. `rm -f` on a link removes the link.
  if wm_is_symlink "$wt/web/node_modules"; then
    wm_link_note "$wt/web/node_modules" own
    rm -f "$wt/web/node_modules" || { wm_warn "cannot seed: $wt/web/node_modules is a symlink that could not be removed"; return 1; }
  fi
  wm_note "seeding web/node_modules (npm ci, ~81s)"
  local rc=0
  if [ -n "$log" ]; then
    ( cd "$wt/web" && ${launch[@]+"${launch[@]}"} "$npm" ci --no-audit --no-fund ) >"$log" 2>&1 || rc=$?
    [ "$rc" -eq 0 ] || tail -3 "$log" | sed 's/^/    | /'
  else
    ( cd "$wt/web" && ${launch[@]+"${launch[@]}"} "$npm" ci --no-audit --no-fund ) || rc=$?
  fi
  [ "$rc" -eq 0 ] || { wm_warn "npm ci failed in $wt/web"; return 1; }
  wm_have "$wt" || { wm_warn "npm ci reported success but $wt/web/node_modules/.bin/next is missing"; return 1; }
  # Fill the caches so the NEXT tree does not pay this again. A cache that
  # cannot be written is a note, never a failure: it costs a future 81 s, and it
  # must not turn a seedable tree into an unseedable one.
  local lock webdeps
  lock="$(wm_lock_sha "$wt")"
  webdeps="$(wm_webdeps "$wt")"
  if [ -n "${WM_CACHE_DIR:-}" ]; then
    rm -rf "${WM_CACHE_DIR%/}" 2>/dev/null
    cp -a "$wt/web/node_modules" "${WM_CACHE_DIR%/}" 2>/dev/null \
      || wm_warn "could not fill the run cache ${WM_CACHE_DIR%/} (not fatal)"
  fi
  if [ -n "${WM_LOCK_CACHE:-}" ] && [ -n "$lock" ]; then
    mkdir -p "${WM_LOCK_CACHE%/}/$lock" 2>/dev/null \
      && cp -a "$wt/web/node_modules" "${WM_LOCK_CACHE%/}/$lock/node_modules" 2>/dev/null \
      && printf '%s\n' "$webdeps" > "${WM_LOCK_CACHE%/}/$lock/webdeps" 2>/dev/null \
      || wm_warn "could not fill the host cache ${WM_LOCK_CACHE%/}/$lock (not fatal)"
  fi
  return 0
}

# wm_seed <worktree> [want-webdeps] — make sure the tree has a usable
# web/node_modules. Returns 0 when seeded (or when the tree has no web/ at all),
# 1 when it could not be. Sets WM_MATCHED / WM_SEED_WEBDEPS / WM_SEED_SOURCE.
wm_seed() {
  local wt=${1:?wm_seed needs a worktree} want=${2:-}
  WM_MATCHED=0; WM_SEED_WEBDEPS=""; WM_SEED_SOURCE=""

  if [ ! -d "$wt/web" ]; then
    wm_note "$wt has no web/ — nothing to seed"
    return 0
  fi
  # A tree whose OWN web/node_modules is a link is not installed (wm_have says so
  # below); name it here so the re-seed is explained rather than silent.
  if wm_is_symlink "$wt/web/node_modules"; then
    wm_link_note "$wt/web/node_modules" own
  fi
  if wm_have "$wt"; then
    WM_SEED_WEBDEPS="$(wm_webdeps "$wt")"
    WM_SEED_SOURCE="$wt/web/node_modules (already installed)"
    if [ -z "$want" ]; then WM_MATCHED=1
    elif [ "$WM_SEED_WEBDEPS" = "$want" ]; then WM_MATCHED=1; fi
    return 0
  fi

  local want_lock; want_lock="$(wm_lock_sha "$wt")"
  wm_candidates "$wt"
  local entry nm dw lock
  if [ "${#WM_CANDS[@]}" -eq 0 ]; then
    wm_note "no donor web/node_modules on this host — installing"
  fi

  # PASS 1 — the exact dependency set the per-tree record names. This is the
  # copy that makes the ship reuse the record instead of re-gating the tree.
  if [ -n "$want" ]; then
    for entry in ${WM_CANDS[@]+"${WM_CANDS[@]}"}; do
      IFS=$'\t' read -r nm dw lock <<<"$entry"
      [ "$(wm_dir_webdeps "$nm")" = "$want" ] || continue
      wm_note "found the dependency set the gate record names (webdeps ${want:0:12}) at $nm"
      if wm_copy_from "$nm" "$wt"; then
        WM_SEED_WEBDEPS="$(wm_webdeps "$wt")"; WM_SEED_SOURCE="$nm"
        [ "$WM_SEED_WEBDEPS" = "$want" ] && WM_MATCHED=1
        return 0
      fi
    done
  fi

  # PASS 2 — the same web/package-lock.json. Runs the gates; does NOT promise
  # reuse (the record's webdeps is a different install), and the caller says so.
  for entry in ${WM_CANDS[@]+"${WM_CANDS[@]}"}; do
    IFS=$'\t' read -r nm dw lock <<<"$entry"
    [ -n "$want_lock" ] || continue
    [ "$(wm_cand_lock "$nm" "$dw" "$lock")" = "$want_lock" ] || continue
    if [ -n "$want" ]; then
      wm_note "no copy matches the record's webdeps ($want); using $nm (installed from the same web/package-lock.json) — the gates can run, but the record cannot vouch for this dependency set"
    fi
    if wm_copy_from "$nm" "$wt"; then
      WM_SEED_WEBDEPS="$(wm_webdeps "$wt")"; WM_SEED_SOURCE="$nm"
      return 0
    fi
  done
  # An explicit donor / run cache with no known lockfile may be used when nobody
  # asked for a specific dependency set (merge-train's run cache, an operator's
  # RELEASE_NODE_MODULES). A WORKTREE may not: using one installed from a different
  # web/package-lock.json would gate a tree against dependency versions it never
  # declared — a false result, not a fast one. Worktrees qualify only through PASS
  # 2's lock comparison.
  if [ -z "$want" ]; then
    for entry in ${WM_CANDS[@]+"${WM_CANDS[@]}"}; do
      IFS=$'\t' read -r nm dw lock <<<"$entry"
      [ -z "$dw" ] && [ -z "$lock" ] || continue
      if wm_copy_from "$nm" "$wt"; then
        WM_SEED_WEBDEPS="$(wm_webdeps "$wt")"; WM_SEED_SOURCE="$nm"
        return 0
      fi
    done
  fi

  # PASS 3 — install.
  wm_install "$wt" || return 1
  WM_SEED_WEBDEPS="$(wm_webdeps "$wt")"; WM_SEED_SOURCE="$wt/web/node_modules (npm ci)"
  if [ -n "$want" ] && [ "$WM_SEED_WEBDEPS" = "$want" ]; then WM_MATCHED=1; fi
  return 0
}

# ------------------------------------------------------------------ CLI --------
# Executed, never sourced, when argv names a subcommand. A sourcing caller gets
# the functions above and nothing else.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  wm_usage() { sed -n '2,/^set -o pipefail/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//'; }
  case "${1:-}" in
    -h|--help) wm_usage; exit 0 ;;
    seed)
      wt=${2:?seed needs a worktree}; want=${3:-}
      WM_REPO=${WM_REPO:-$(git -C "$wt" rev-parse --show-toplevel 2>/dev/null)}
      if [ -z "$want" ]; then
        # No explicit want: use the record for the tree HEAD names, when there is
        # one. That is what makes a CLI run behave like a release ship.
        want="$(wm_record_webdeps "$(git -C "$wt" rev-parse 'HEAD^{tree}' 2>/dev/null)")"
      fi
      if wm_seed "$wt" "$want"; then
        printf 'seeded webdeps=%s source=%s matched_record=%s\n' \
          "${WM_SEED_WEBDEPS:-none}" "${WM_SEED_SOURCE:-none}" "$WM_MATCHED"
        exit 0
      fi
      exit 1 ;;
    webdeps)
      wm_webdeps "${2:?webdeps needs a worktree}"; printf '\n'; exit 0 ;;
    record-webdeps)
      v="$(wm_record_webdeps "${2:?record-webdeps needs a tree sha}")"
      printf '%s\n' "${v:-none}"; exit 0 ;;
    candidates)
      WM_REPO=${WM_REPO:-$(git -C "${2:?candidates needs a worktree}" rev-parse --show-toplevel 2>/dev/null)}
      wm_candidates "${2}"; printf '%s\n' ${WM_CANDS[@]+"${WM_CANDS[@]}"}; exit 0 ;;
    ""|*) wm_usage >&2; exit 2 ;;
  esac
fi
