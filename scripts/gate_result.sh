#!/usr/bin/env bash
#
# scripts/gate_result.sh — the per-tree gate RESULT RECORD (shared library).
#
# WHY THIS EXISTS
#   scripts/preflight.sh gates a TREE, but until now nothing remembered the
#   verdict. The same tree was gated up to ten times on this host (509 completed
#   full runs over 6.4 days, 97.1 host-hours), because each attempt sat behind the
#   shared gate queue and learned nothing from the one ahead of it: 206 of those runs
#   (38.3 host-hours, 39.4%) re-gated a tree that had already been gated. Worse,
#   28 of the 125 repeatedly-gated trees (22%) came back with BOTH verdicts on
#   identical bytes, so the verdict read as a property of the MOMENT instead of
#   the TREE — which is not a gate, and is not what a merger needs when it decides
#   to squash.
#
#   This library makes the result a property of the tree. The runner writes one
#   record per tree; anything that would otherwise queue reads it FIRST
#   (scripts/preflight_reuse.sh), and a lane whose tree is already green never
#   joins the gate queue at all.
#
# WHAT THE RECORD NAMES (all four are load-bearing; none is decoration)
#   * the TREE sha, not the commit sha. `2f961d7` (a merge tree) and `4bb8eae`
#     (the PR head) are different objects, and a record may only ever be reused
#     for the bytes it actually gated. The tree sha of the WORKING TREE is
#     computed with a private index (`read-tree HEAD` + `add -A` + `write-tree`),
#     so it honours .gitignore, includes uncommitted edits, and equals
#     `HEAD^{tree}` for a clean checkout.
#   * the ENVIRONMENT FINGERPRINT. A tree sha is not enough: the same bytes can be
#     gated by a different toolchain, against a different installed web
#     dependency set, or by a different gate script. Those are hashed into `key`,
#     and a record whose key no longer matches is REFUSED (never silently reused).
#     The test-database host rides along too — a record made against another
#     database is not evidence about this one. Credentials are never stored, and
#     that is enforced by SHAPE rather than promised. A value carrying a
#     `scheme://` is stripped at its authority's LAST `@` — the anchor Go's net/url
#     uses — so a password containing `@` or `/` goes with it; where that authority
#     carries NO userinfo and IS the address the driver resolves, the whole value is
#     kept, because dropping through the `@` there would alias two SERVERS; and the
#     loopback spellings (`localhost`, `127.0.0.1`, `[::1]`) are canonicalised to
#     one token, so the key is a fact about the database and not about how the host
#     was written. A value with NO scheme at all — PostgreSQL's keyword/value DSN,
#     a form the API's own driver ACCEPTS — is not stripped, it is REFUSED: its
#     password never reaches the record at all. See gr_testdb_component for each
#     shape and what it costs.
#   * `finished_at`, so a cached pass cannot outlive its usefulness. Records older
#     than PREFLIGHT_RESULT_TTL (default 86400s) are treated as absent.
#   * the GATE LIST it gated, with a per-gate status, so a consumer can reuse the
#     passes and re-run only what failed.
#
# THE TWO SAFETY RULES (do not "simplify" them away)
#   1. A record is ADVISORY FOR SKIPPING ONLY. Reuse may never turn a red into a
#      green: a reused gate must have been observed `pass` on THIS tree under
#      THIS key, and the aggregate `verdict` is `green` only when EVERY gate in
#      the current inventory holds a `pass`. One `fail` anywhere makes it `red`;
#      anything unobserved makes it `partial`. A merger still needs a verdict for
#      the exact bytes it merges — this is not a substitute for gating them.
#   2. EVERY CARRIED-OVER GATE IS LABELLED `reused: true` with the timestamp of
#      the run that observed it, so a human reading a verdict can see whether a
#      gate RAN or was REMEMBERED.
#
#      ONE DELIBERATE EXCEPTION, and it narrows rule 1 rather than waiving it. The
#      `monitor` gate reads a FIXED SET OF FILES (deploy/monitoring/*) and never the
#      product tree; `selftest` reads THE WHOLE CHECKOUT (its checks derive gate claims
#      from api/ web/ scripts/ deploy/, read web/e2e, and sweep every tracked file —
#      `--gate-deps` reports `selftest: *`). Both are cached by preflight.sh's
#      `hcached`, keyed by a sha256 of exactly what they read: the declared globs for
#      `monitor`, and `git ls-files -s` over the tracked tree for `selftest` (the
#      `:tree:` input, refused outright when the tree is dirty or is not a checkout). A
#      hit does NOT set `reused`: in this format that word means
#      "carried over between flushes", and the run did invoke the gate and read a pass
#      from it. A hit is visible as `seconds: 0`, an `at` that is the OBSERVING run's
#      time, and the HIT line naming the harness record behind it. Change one of those
#      bytes and the entry stops matching and the gate re-runs (proved by `selftest`
#      check 49). Do not extend this to a gate that reads the tree without `:tree:`.
#
# FORMAT — one pretty-printed JSON file per tree:
#   ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/<tree-sha>.json
#
#   {
#     "format": 1,
#     "tree": "<tree sha>",
#     "key": "<sha256 of key_parts>",
#     "key_parts": "go=...;node=...;webdeps=...;gatescript=...;resultlib=...;testdb=...",
#     "gates": { "build": { "status": "pass", "seconds": 12, "at": "...", "reused": false }, ... },
#     "passed": 14, "failed": 0, "skipped": 0,   <- the tally of the LAST run
#     "selected": "build,vet,...",               <- what that run actually ran
#     "runs": 3,                                 <- gate RUNS this record stands on
#                                                   (not flushes, not reuses)
#     "seconds": 1338, "verdict": "green",
#     "complete": true,                          <- did the run that wrote it finish?
#     "log": "<log path, optional>", "finished_at": "<iso>"
#   }
#
#   READERS ARE jq-FREE ON PURPOSE. The file is written by THIS library in a fixed
#   shape, so every read below is an anchored `sed` expression. Anchoring is the
#   point: a line that does not match yields the empty string, and an empty string
#   is never read as a pass.
#
# WRITE SEMANTICS — MERGE, never blind overwrite. A lane that re-runs only the
#   failed gates must not erase the passes it is standing on, and a `skip` records
#   nothing (a gate that did not run is not an observation). Concretely, per gate:
#     * pass/fail from this run  -> replaces that gate's entry (reused: false);
#     * skip, or not reached     -> the previous entry is carried over (reused:
#                                   true) and keeps the `at` that observed it;
#     * never observed anywhere   -> absent (and the verdict cannot be `green`).
#
#   AND A CARRY IS ONLY LEGAL UNDER THE KEY THAT OBSERVED IT (card t_d49a99f1).
#   The entries in the existing file were observed under the key THAT FILE names.
#   When this run's key differs — a re-installed web/node_modules, a new Go
#   toolchain, an edited gate script, a different test database, a new
#   PREFLIGHT_KEY_EXTRA — those entries are not evidence about this environment,
#   and carrying them would write them UNDER THE NEW KEY. Measured on this host
#   while gating the merge of PR #1477: a complete record from another lane (23/23
#   pass under key 25042aea) was followed by a run under key f2be5fdd; after five
#   gates that run's record read `verdict: green, passed: 5` under f2be5fdd, with
#   eighteen gates still carrying `at` from the other lane's run and never run
#   under f2be5fdd at all. `gr_record_usable` validates the FILE's key, which by
#   then was the new one — so a consumer would have called those bytes gated. The
#   carry-over loop therefore reads the existing record's `key` first and carries
#   NOTHING when it differs: the gates become `skip` (unobserved) and the verdict
#   can only be `green` once every gate really has an observation under this key.
#
#   A CARRIED ENTRY IS NEVER RELABELLED AS OBSERVED (same card, one flush later).
#   Because the file is rewritten after EVERY gate, a run re-reads its own output
#   on the next flush. The pre-fix loop wrote carried values into GR_STATUS — the
#   map that means "this run observed it" — so from the second flush onward those
#   gates took the OBSERVED branch, which sets `reused: false` while keeping the
#   older `at`. Measured on the run above: at 18:07:53Z the record read `passed=5`
#   with eighteen entries carrying `at` from 17:20-17:48Z and `reused: false` —
#   the one label that could have told a reader those gates did not run was gone.
#   Carried values now live in their own maps (GR_CARRY_*), so GR_STATUS keeps its
#   only meaning: a gate THIS RUN observed. `reused` in the file is exactly "this
#   entry was remembered, not re-run", every time the file is written.
#
#   A RUN THAT DID NOT FINISH IS NOT A VERDICT (same card). `finished_at` says when
#   the file was last written, which is after every gate, so a killed run leaves a
#   record that looks exactly like a finished one. `complete` separates them: the
#   runner flushes `complete: false` after every gate and a final `complete: true`
#   only when it reaches the end of its inventory (green OR red — a red run that
#   finished is still a finished observation, which is what the documented
#   `--only <PREFLIGHT_RERUN> --reuse-record` lane reads). `gr_record_usable`
#   refuses anything that does not say `complete: true`, so a mid-run or killed
#   record can never be handed to a lane as a verdict. `verdict` is written as
#   `in_progress` while incomplete, for the human reading the raw file.
#
#   `runs` COUNTS RUNS, NOT WRITES (card t_68fa3c5c). gr_flush is re-entered after
#   EVERY gate — that is how the file stays current — so a per-write increment made
#   one COMPLETE 23-gate run read `runs: 24`, and a following two-gate run on the
#   same tree take it to 27. The field answers "how many flushes" while its name
#   promises "how many gate runs this record stands on", which is exactly what a
#   merger asks of a reused verdict. It is now incremented ONCE PER RUNNING PROCESS
#   (GR_RUN_COUNTED, the same shape as GR_COMPLETE), and it RESTARTS AT 1 when the
#   record's key changes: a key change carries no gate entry, so the file's verdict
#   then stands on THIS run alone and counting the earlier runs would claim support
#   the record does not have. Renaming the field to `flushes` was the alternative
#   and was rejected: it reports the machinery instead of the evidence, and every
#   consumer would still have to derive "how many runs" from something else.
#
# The record directory is host state, deliberately outside the repo (a record is
# evidence about a machine, not a versioned artefact) and outside /tmp (which is
# wiped between sessions, so a record there would be useless the next morning).

# The functions below are SOURCED by the gate runner and by the lane-side
# consumer, so nothing here may run at source time beyond these declarations.
declare -A GR_STATUS=()
declare -A GR_SECONDS=()
declare -A GR_AT=()
declare -A GR_REUSED=()
# What this run did NOT observe but the record already holds under THIS key. Kept
# OUT of GR_STATUS/GR_SECONDS/GR_AT on purpose: those three mean "this run observed
# this gate", and the writer is re-entered after every gate, so mixing the two in
# one map relabelled carried entries `reused: false` one flush later (see A CARRIED
# ENTRY IS NEVER RELABELLED AS OBSERVED in the header).
declare -A GR_CARRY_ST=()
declare -A GR_CARRY_SECONDS=()
declare -A GR_CARRY_AT=()
GR_TREE=""
GR_TREE_SHORT=""
GR_REPO=""
GR_COMMIT=""
GR_BRANCH=""
GR_PASSED=0
GR_FAILED=0
GR_SKIPPED=0
GR_LOG=""
GR_PID=""
GR_FORMAT=1
# 1 only when the runner has reached the END of its inventory; every intermediate
# flush writes `complete: false` (see A RUN THAT DID NOT FINISH IS NOT A VERDICT).
GR_COMPLETE=0
# 1 once THIS PROCESS has counted itself into `runs` — i.e. once its first flush has
# landed. The writer is re-entered after every gate, so without this one 23-gate run
# would report 24 runs (see `runs` COUNTS RUNS, NOT WRITES).
GR_RUN_COUNTED=0
# Announce a key-change carry refusal once per process, not once per flush.
GR_KEYNOTE=""

gr_dir() { printf '%s\n' "${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}"; }
gr_ttl() { printf '%s\n' "${PREFLIGHT_RESULT_TTL:-86400}"; }
gr_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

gr_json_escape() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }

# gr_tree_sha [repo] — the sha of the WORKING TREE at <repo> (default: $PWD).
# A private index is used so this can run next to a live gate in another worktree
# without touching that worktree's index. Output is empty and the status is
# non-zero when it cannot be computed — the caller must refuse rather than guess.
gr_tree_sha() {
  local repo="${1:-$PWD}" idx out rc
  idx="$(mktemp "${TMPDIR:-/tmp}/gate-result-index.XXXXXX")" || return 1
  rm -f "$idx"
  out="$(cd "$repo" 2>/dev/null && GIT_INDEX_FILE="$idx" git read-tree HEAD 2>/dev/null \
        && GIT_INDEX_FILE="$idx" git add -A -- . >/dev/null 2>&1 \
        && GIT_INDEX_FILE="$idx" git write-tree 2>/dev/null)"
  rc=$?
  rm -f "$idx"
  [ "$rc" -eq 0 ] || return 1
  [ -n "$out" ] || return 1
  printf '%s\n' "$out"
}

# gr_short_sha <sha> — 7 characters, the way the lanes name a tree.
gr_short_sha() { printf '%.7s\n' "$1"; }

# gr_head_tree [repo] — the sha of HEAD's TREE COMMIT at <repo> (default: $PWD),
# or empty when there is none (no commits yet, not a repo). Compare it with
# gr_tree_sha: equal means the gated bytes ARE HEAD's bytes; different means the
# run gated content no commit names (a merge-lane worktree, an uncommitted edit).
# Output is empty on failure — the caller must record "unknown", never guess.
gr_head_tree() {
  local repo="${1:-$PWD}" out
  out="$(cd "$repo" 2>/dev/null && git rev-parse --verify 'HEAD^{tree}' 2>/dev/null)" || return 1
  [ -n "$out" ] || return 1
  printf '%s\n' "$out"
}

gr_sha256_file() {
  [ -r "$1" ] || { printf 'none'; return 0; }
  sha256sum "$1" 2>/dev/null | awk '{print $1}'
}

# gr_testdb_component — host/port/database of the test database URL, with the
# credentials stripped. A record made against another database is not evidence
# about this one, and the password must never reach a file on disk.
#
# WHICH SHAPES ARE STRIPPED, AND WHICH ARE REFUSED. Stated by shape rather than
# claimed "total", because there is an input class no strip can cover:
#
#   A. `scheme://…` — the URL form. STRIPPED. The userinfo is removed from the
#      authority's LAST `@` (see THE ANCHOR below), so `host:port/database`
#      survives and the credential does not. This is what the test-env file
#      exports (`postgres://…`) and the only form any gate on this host uses.
#
#   B. No `scheme://` — REFUSED. PostgreSQL's keyword/value DSN
#      (`host=… password=… user=…`) is NOT a URL: there is no scheme to strip and
#      no authority to anchor on, and every `key=value` pair is indistinguishable
#      from any other, so a strip cannot be shown to have removed the userinfo.
#      A `password=` is not even syntactically special — the driver takes an
#      arbitrary `key=value` as a runtime parameter, so a whitelist of credential
#      keys could not be shown complete. Rather than guess, the value never
#      reaches the fingerprint: the component is the literal token `refused`, and
#      every such run shares one key. That is the SAFE direction — two different
#      databases under an un-strippable DSN can only make records look ALIKE (the
#      conservative direction), never make a record about one database read as
#      evidence about another.
#      Measured before the scheme guard existed (the defect): the value was
#      returned VERBATIM, so `password=s3cret-token` was written into the
#      world-readable record at ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/<tree>.json
#      AND hashed into the key, so rotating that password silently refused a valid
#      green record. The API's own driver accepts the form (measured with this
#      repo's pinned pgx v5.10.0), so a lane can gate with it.
#
# `refused` cannot collide with a real component: a bare host/port/database is
# not a DSN either (pgx rejects `localhost:5432/db` and `db` as "invalid
# keyword/value"), so no caller reaches this function with one. It is a bare word
# with no `:` or `;`, so it cannot disturb the `key_parts` line it is written
# into (the readers are anchored seds).
#
# THE STEPS BELOW APPLY TO SHAPE A — a value that carries a `scheme://`. A value
# with no scheme is refused above, before any of them runs.
#
# THE HOST IS CANONICALISED, because the key material must be a fact about the
# DATABASE and not about how it was SPELLED. Measured on this host: the gate ran
# green on tree f98b5fcf and wrote `testdb=127.0.0.1:5432/jomform_test`, and the
# very next reader — the SAME server, the SAME database, the caller's environment
# unchanged — was refused with "the environment changed since that record" because
# it spelled the host `localhost` (recorded key 3c1cf0fc vs current b34aadac).
# The rejection is SILENT about the cause and costs the whole gate: the lane
# re-queues behind the host-wide slot pool, whose waiter has been measured at 50-86
# minutes, to re-run fifteen gates that had already passed on those exact bytes.
# Both spellings reach this process in normal use — the file a caller is told to
# source (${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}) exports 127.0.0.1
# while an `export JOMFORM_TEST_DATABASE_URL=...localhost...` lane spells it the
# other way — so which key a record carries depends on which shape invoked which
# script.
#
# `localhost`, `127.0.0.1` and `[::1]` are rewritten to the single token
# `loopback`, and ONLY when the component is exactly `<host>[:port][/database]`
# (an anchored match). Everything else is hashed verbatim, which is the safe
# direction: an unknown host cannot be judged from one string, and a canonical
# form nobody measured could merge two servers that really are different.
#
# `loopback` rather than one of the aliases, deliberately: it is not a name for
# the server, it is the STATEMENT that this key does not distinguish these
# spellings. A record whose key_parts still carries `testdb=127.0.0.1:...` was
# written before this rule and is refused exactly once, which is the documented
# behaviour of a changed key material (docs/PREFLIGHT-REUSE.md, rule 3).
#
# DELIBERATELY NOT RESOLVING THE HOST. The obvious alternative — resolve the name
# and key the server's address — is broken on the host this runs on, measured:
# `getent hosts localhost` answers `::1` while `getent hosts 127.0.0.1` answers
# `127.0.0.1`, so resolving would have produced two keys for the one server again,
# just different ones. It would also make the key depend on the caller's
# NSS/DNS/hosts state, which no lane can see from its record, and it costs a
# resolver round trip inside a library that otherwise reads only local files.
# Dropping the host entirely (keeping port+database) was rejected too: the
# property the rest of this comment claims — a record against another database is
# not evidence about this one — is worth keeping, and port+database cannot see a
# second server reached over a tunnel or a forwarded port.
#
# THE ANCHOR IS THE AUTHORITY, NOT THE FIRST `@`. The pre-fix expression
# (`s#^[^@/]*@##`) was ALL-OR-NOTHING: it consumed userinfo only when an `@` was
# preceded by neither an `@` nor a `/`, so a password containing either character
# was not stripped AT ALL and the credential walked straight into the record's key
# material (measured: `postgres://u1:***@127.0.0.1:5432/db` ->
# `2@127.0.0.1:5432/db`, and the `/` shape survived whole). Records are written to
# ${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/<tree>.json in a world-readable
# dir, and a leaked password is also KEY MATERIAL: rotating it would move the key
# and silently refuse a valid green record — the same silent "the environment
# changed since that record" failure, for a password change instead of a hostname
# spelling. (A simple password rotated cleanly, which is what hid this: only the
# `@`/`/` class leaks.)
#
# THE RULE, in three steps, because there are three genuinely different shapes —
# and all three are STEPS OF SHAPE A, reached only by a value that carries a
# `scheme://`. A value with no scheme is refused before them (see WHICH SHAPES
# ARE STRIPPED above), so no shape below has to reason about one:
#   1. An `@` INSIDE the authority (the substring before the first `/`) is the
#      userinfo delimiter — the LAST one, the same anchor Go's net/url uses
#      (`parseAuthority` takes `strings.LastIndex(authority, "@")`). Everything
#      through it is dropped, and the path is preserved, so a DATABASE name
#      containing an `@` (`.../jo@m_test`) is not eaten.
#   2. NO `@` anywhere: the component is whatever the driver's own reading gives,
#      `host[:port][/database]`. Nothing in it is a credential, so nothing is
#      stripped.
#   3. An `@` AFTER the authority's first `/`. Two readings disagree here, so the
#      anchor is decided by WHICH READ THE DRIVER TAKES, not by assuming either:
#
#        * the authority is NOT `host[:port]` — it reads as userinfo, or as a
#          `host:` followed by something that is not a port. There is no address to
#          trust, and the value is still a string a caller can export, so everything
#          through the LAST `@` in the string is dropped. Measured with this repo's
#          pinned pgx v5.10.0, that shape is also one the driver REFUSES ("invalid
#          port ... after host"), so no lane can gate with it: the strip here can
#          only refuse a record, never reuse another server's verdict.
#        * the authority IS `host[:port]` — the text the driver resolves as the
#          ADDRESS (measured with pgx v5.10.0, this repo's pinned version: the
#          authority is host and port, the remaining tail is the DATABASE name).
#          The whole value is kept, because dropping through the `@` here aliases
#          two SERVERS. Measured on the parent of this change: three DSNs naming
#          hostA/db, hostB/db and hostA/other all collapsed to the single component
#          `x` with ONE key, and a record written against hostA was accepted by
#          gr_reusable as evidence about hostB — end to end, the record's
#          `testdb=x` read back under hostB, REUSED. A component that refuses a
#          record costs a re-gate; a component that aliases two servers reuses a
#          verdict about the wrong database, silently. That is why the direction
#          chosen here is the one that keeps the server.
#
#    Both directions are stated: a component produced from a rejected authority is
#    a bare tail, which cannot equal the component of a real DSN, so it refuses a
#    record rather than aliasing another server, and the two rejected shapes
#    measured (`host:abc`, a `/` inside the authority) are refused by the driver
#    too, so no lane can write a record under them.
#
# WHICH SHAPE IS WHICH IS DECIDED BY THE AUTHORITY, NOT BY LOOKING FOR AN `@` — see
# gr_is_hostport, which recognises `host[:port]` positively. Every choice above has
# a stated direction: no component that cannot be shown to describe the server the
# driver reaches is produced, and where that costs a refusal it is a re-gate rather
# than a wrong reuse.
#
# A `:` in the authority with NO `@` anywhere also carries its "password" into the
# component, and it is NOT carved out, because it is not reachable: pgx and Go's
# net/url both REFUSE that string (measured with pgx v5.10.0 — its error names the
# authority as an address with "too many colons in address", and net/url's parser
# rejects the same string), so no lane can gate with it, and treating every such
# authority as a credential would destroy the host and port for the DSNs that DO
# work. (An earlier note in this comment claimed the API's driver LOADS that shape;
# measured, neither pgx nor net/url does. Net: no credential-bearing string reaches
# the component except through a shape that has already had its userinfo removed.)
#
# The QUERY is cut BEFORE the anchor is located. A query cannot be part of the
# authority, and a query may legitimately carry `@` (`?options=a@b`), so cutting
# it first is what stops a query `@` from being read as a credential delimiter.
# Credentials in the query are dropped with it.
#
# gr_is_hostport <text> — 0 when <text> is exactly `host[:port]`: no scheme, no
# path, no userinfo, non-empty host, and — when a `:` is present — a port of digits
# only. A BRACKETED IPv6 authority (`[::1]:5432`) is one too: it is an address the
# driver resolves (measured with pgx v5.10.0: `[::1]:5432` -> host=::1 port=5432),
# and the comment above names `[::1]` as a spelling this host's callers use, so
# reading it as userinfo would have kept the collision for exactly that spelling.
#
# Recognised POSITIVELY rather than by excluding an `@`, because the decision
# gr_testdb_component has to make is which reading the driver takes, and only an
# address proves which. What the positive test buys, stated as a direction: an
# authority it REJECTS can produce a component that is a bare tail (`y`), and a
# bare tail cannot equal a component the strip produces from a DSN with real
# userinfo — `postgres://u:p@y` yields `y`, but `postgres://y@z` yields `z`, and a
# component carrying a port or a path always keeps its `:` or `/` — so a rejected
# authority refuses a record rather than aliasing another server.
#
# THE EXAMPLES BELOW WERE CORRECTED, and the correction is measured rather than
# read (card t_672654f5, the secondary item; pgx v5.10.0 on this host):
#
#   * `host:abc` — REFUSED by the driver ONLY when the `:abc` is a PORT
#     (`postgres://host:abc` -> "invalid port \":abc\" after host"). The shape
#     this comment originally cited, `postgres://host:abc@x`, is ACCEPTED: the
#     driver reads `host:abc` as USERINFO and resolves to host=x, db="". So the
#     original sentence named a refusal that does not happen for that DSN.
#   * `host:5432/a/b` — likewise ACCEPTED. pgx splits the authority at the first
#     `/`, so `postgres://host:5432/a/b@x` resolves to host=host, db="a/b@x"
#     (verified). It does not reach gr_is_hostport as `host:5432/a/b` at all; the
#     authority the function is handed is `host:5432`, which it ACCEPTS, and the
#     value travels the "driver's own answer" branch.
#   * the shapes that DO reach the reject branch with a driver-accepted DSN are
#     the ones the positive test rejects while pgx parses them: `host:` (an empty
#     port — pgx accepts, resolving host=host, db="x@y"; gr_is_hostport rejects
#     it, giving the component `y`) and `:5432` (no host — pgx accepts, resolving
#     to the unix socket /var/run/postgresql; gr_is_hostport rejects it).
#
# The direction the original comment claimed still holds, and the corrected
# examples are what demonstrate it: those two inputs really do produce a bare
# tail, and a bare tail is what cannot alias a userinfo-bearing server.
gr_is_hostport() {
  case "$1" in
    "")                return 1 ;;
    \[*)               [[ "$1" =~ ^\[[^]]+\](:[0-9]+)?$ ]] || return 1 ;;
    *:*)               [[ "$1" =~ ^[^:/[:space:]]+:[0-9]+$ ]] || return 1 ;;
    *[!A-Za-z0-9._-]*) return 1 ;;
  esac
  return 0
}
gr_testdb_component() {
  local url="${JOMFORM_TEST_DATABASE_URL:-}" comp auth tail
  if [ -z "$url" ]; then printf 'unset'; return 0; fi
  # Shape B: no scheme. Nothing here can be stripped with confidence, so the
  # value is refused rather than risked — see WHICH SHAPES ARE STRIPPED above.
  case "$url" in
    *://*) : ;;
    *)     printf 'refused'; return 0 ;;
  esac
  comp="${url#*://}"                       # the scheme, when there is one
  comp="$(printf '%s' "$comp" | sed -E 's#\?.*$##; s#/$##')"
  auth="${comp%%/*}"                       # the authority: up to the first '/'
  tail="${comp#"$auth"}"                   # whatever follows it (path, or empty)
  case "$auth" in
    *@*) comp="${auth##*@}$tail" ;;        # step 1 — the LAST @ inside the authority
    *)   case "$comp" in
           *@*)                            # step 3 — an @ at or after the path
             # The authority decides which reading the driver takes: where it is a
             # real `host[:port]` the tail is a DATABASE name and the whole value
             # is the driver's own answer; otherwise the `@` is not shown to be a
             # credential delimiter and the strip is kept. See THE RULE above.
             gr_is_hostport "$auth" || comp="${comp##*@}" ;;
         esac ;;
  esac
  # Canonicalise EACH host in a comma-separated authority, then DEDUPE (card
  # t_54342924).
  #
  # A DSN list is how a URL names an HA pair, and "two aliases of one server"
  # written as a list — `127.0.0.1,127.0.0.1:5432/db` — escaped the anchored
  # loopback rule entirely (`,` is not in the host pattern), so ONE server still
  # produced TWO keys. That is the same class as t_7add0cfa: a record refused with
  # "the environment changed since that record", costing the whole gate and the
  # host-wide slot pool wait. The component's whole job is to answer "is this the same
  # test database?", and it answered wrongly the moment an alias escaped.
  #
  # Two steps, because mapping each host is not enough on its own:
  #   1. normalise each element, filling the DEFAULT PORT for a loopback host that
  #      omits it (`127.0.0.1` and `127.0.0.1:5432` are one server — that is what
  #      5432 as a default MEANS);
  #   2. DROP DUPLICATES, so a list naming the same server twice is the same
  #      component as naming it once.
  # Order is preserved, and distinct hosts are untouched — a genuine pair
  # (`host1:5432,host2:5432`) must stay two elements.
  #
  # Measured before this change:
  #   postgres://u:***@127.0.0.1,127.0.0.1:5432/jomform_test -> 127.0.0.1,127.0.0.1:5432/jomform_test
  #   postgres://u:***@localhost:5432/jomform_test           -> loopback:5432/jomform_test
  # After: both give `loopback:5432/jomform_test`.
  local hostpart rest out="" seg
  hostpart="${comp%%/*}"
  if [ "$hostpart" = "$comp" ]; then rest=""; else rest="/${comp#*/}"; fi
  local IFS=','
  for seg in $hostpart; do
    seg="$(printf '%s' "$seg" | sed -E 's#^(localhost|127\.0\.0\.1|\[::1\]):?([0-9]*)$#loopback:\2#')"
    # A loopback host with no port took the default above -> 5432.
    case "$seg" in loopback:) seg="loopback:5432" ;; esac
    # Dedupe, preserving order.
    case ",$out," in
      *",$seg,"*) : ;;
      *) out="${out:+$out,}$seg" ;;
    esac
  done
  comp="$out$rest"
  printf '%s' "$comp"
}

# gr_key_material [repo] — the components that INVALIDATE a record, in a fixed
# order, as one line. These are the facts a verdict genuinely depends on: the Go
# toolchain, the INSTALLED web dependency set, the two gate scripts themselves,
# the test database, and PREFLIGHT_KEY_EXTRA for anything else.
#
# WHAT IS DELIBERATELY NOT HERE: THE NODE VERSION. It is still recorded — see
# gr_key_parts and gr_node_advisory — but it does not refuse a record, because on
# this host it is a fact about the SHELL and not about the work. Measured: the
# same tree, the same database and the same webdeps hash yield `node=v24.19.0`
# from an INTERACTIVE shell (nvm is appended by /root/.bashrc) and
# `node=v26.7.0` in a Hermes worker lane (PATH carries
# /root/.hermes/tools/node-26.7.0-linux-x64/bin and no nvm). Keyed on node, a
# record written by one shape is REFUSED by the other with "the environment
# changed since that record" on identical bytes — i.e. reuse was dead for the
# the exact shape (a worker lane) that runs the gates, which is the whole point of
# the feature. Pinning node so every lane resolves one is not available from the
# repo: `preflight.sh` deliberately does NOT source the test-environment file (see
# the pre-lock block), and no host file ships with a PR. So node is demoted from a
# refusal to a LABELLED ADVISORY: the record names the node it was observed under,
# and a run under a different one says so out loud, but a matching
# tree+webdeps+gatescript is still a valid observation of those bytes.
#
# `webdeps` (the sha of web/node_modules/.package-lock.json) is what carries the
# installed-toolchain signal, and unlike `node --version` it is a property of the
# TREE's install rather than of the caller's PATH.
gr_key_material() {
  local repo="${1:-$PWD}" go_v webdeps gatescript resultlib
  go_v="$(cd "$repo/api" 2>/dev/null && go env GOVERSION 2>/dev/null)"
  [ -n "$go_v" ] || go_v="unknown"
  webdeps="$(gr_sha256_file "$repo/web/node_modules/.package-lock.json")"
  gatescript="$(gr_sha256_file "$repo/scripts/preflight.sh")"
  resultlib="$(gr_sha256_file "$repo/scripts/gate_result.sh")"
  printf 'go=%s;webdeps=%s;gatescript=%s;resultlib=%s;testdb=%s;extra=%s' \
    "$go_v" "$webdeps" "$gatescript" "$resultlib" "$(gr_testdb_component)" \
    "$(printf '%s' "${PREFLIGHT_KEY_EXTRA:-none}" | sha256sum | awk '{print $1}')"
}

# gr_key_parts [repo] — the components AND the informational ones, in a fixed
# order, as one human-readable line. Every component is a fact about the MACHINE
# the verdict came from or about the CODE that produced it.
#
# `node` is EVIDENCE, not key material: it is written into the record so a reader
# can see the toolchain a verdict was observed under, and a mismatch is announced
# (gr_node_advisory), but it does not by itself refuse the record — see
# gr_key_material for the measurement that made that the only workable policy.
gr_key_parts() {
  local repo="${1:-$PWD}" node_v
  node_v="$(gr_node_version)"
  printf '%s;node=%s' "$(gr_key_material "$repo")" "$node_v"
}

# gr_key [repo] — the fingerprint the record is keyed on. Hashes the KEY
# MATERIAL only, so the recorded node is visible without being load-bearing.
gr_key() { gr_key_material "${1:-$PWD}" | sha256sum | awk '{print $1}'; }

# gr_node_version — the node this shell resolves, or "unknown".
gr_node_version() {
  local v
  v="$(node --version 2>/dev/null)"
  printf '%s\n' "${v:-unknown}"
}

# gr_node_advisory <file> — print a labelled note when the record was observed
# under a DIFFERENT node than this shell resolves. Returns 1 when it differs, 0
# otherwise, so a caller can act on it; the caller must NOT refuse the record on
# it (that is the whole point — see gr_key_material).
gr_node_advisory() {
  local file="$1" rec_node cur_node
  [ -r "$file" ] || return 0
  rec_node="$(printf '%s' "$(gr_read_field "$file" key_parts)" \
    | sed -nE 's/.*(^|;)node=([^;]*).*/\2/p' | sed -n 1p)"
  [ -n "$rec_node" ] || return 0
  cur_node="$(gr_node_version)"
  [ "$rec_node" = "$cur_node" ] && return 0
  echo "preflight: note — the record was OBSERVED under node $rec_node; this run resolves node $cur_node. node is advisory and does NOT invalidate the record (it is a property of the shell on this host, not of the tree), but the web-side gates were observed under a different node." >&2
  return 1
}

gr_file() { printf '%s/%s.json\n' "$(gr_dir)" "$1"; }

# ---- readers (anchored; an unmatched line yields nothing, never a pass) --------
gr_read_field() { # <file> <field>
  [ -r "$1" ] || return 0
  sed -nE "s/^  \"$2\": \"(.*)\",?$/\1/p" "$1" | sed -n 1p
}
gr_read_int() { # <file> <field>
  [ -r "$1" ] || return 0
  sed -nE "s/^  \"$2\": ([0-9]+),?$/\1/p" "$1" | sed -n 1p
}
# gr_read_bool <file> <field> — true|false|null for an UNQUOTED JSON literal, empty
# when the field is absent (a record written before the field existed). Kept
# separate from gr_read_field because that reader is anchored on a QUOTED value:
# reading `"gated_is_head": false` with it returns nothing, which would read as
# "the field says no such thing" when the field says exactly that.
gr_read_bool() { # <file> <field>
  [ -r "$1" ] || return 0
  sed -nE "s/^  \"$2\": (true|false|null),?$/\1/p" "$1" | sed -n 1p
}
gr_gate_status() { # <file> <gate>
  [ -r "$1" ] || return 0
  sed -nE "s/^    \"$2\": \{ \"status\": \"([a-z]+)\".*$/\1/p" "$1" | sed -n 1p
}
gr_gate_entry_field() { # <file> <gate> <status|seconds|at|reused>
  local f="$2" k="$3"
  [ -r "$1" ] || return 0
  case "$k" in
    status)  gr_gate_status "$1" "$f" ;;
    seconds) sed -nE "s/^    \"$f\": \{ \"status\": \"[a-z]+\", \"seconds\": ([0-9]+),.*$/\1/p" "$1" | sed -n 1p ;;
    at)      sed -nE "s/^    \"$f\": \{ \"status\": \"[a-z]+\", \"seconds\": [0-9]+, \"at\": \"([^\"]*)\",.*$/\1/p" "$1" | sed -n 1p ;;
    reused)  sed -nE "s/^    \"$f\": \{ \"status\": \"[a-z]+\", \"seconds\": [0-9]+, \"at\": \"[^\"]*\", \"reused\": (true|false) \},?$/\1/p" "$1" | sed -n 1p ;;
  esac
}

# gr_verdict <file> <gate...> — green | red | partial | "" (no record).
#   * any gate observed `fail`          -> red   (a red can never be read green)
#   * every gate observed `pass`        -> green
#   * anything else (skip/absent)       -> partial
gr_verdict() {
  local file="$1"; shift
  [ -r "$file" ] || return 0
  [ "$(gr_read_int "$file" format)" = "$GR_FORMAT" ] || { printf 'unrecognised\n'; return 0; }
  local g st n=0 missing=0
  for g in "$@"; do
    n=$((n+1))
    st="$(gr_gate_status "$file" "$g")"
    case "$st" in
      fail) printf 'red\n'; return 0 ;;
      pass) ;;
      *)    missing=$((missing+1)) ;;
    esac
  done
  if [ "$n" -gt 0 ] && [ "$missing" -eq 0 ]; then printf 'green\n'; return 0; fi
  printf 'partial\n'
}

# gr_record_usable <tree> — 0 when the record for <tree> describes THIS tree,
# under the CURRENT environment fingerprint, inside the TTL, in a format this
# reader understands. It says nothing about pass/fail: that is gr_verdict's job.
# Split out so the partial branch does not have to pattern-match a message.
#
# gr_key_diff_hint <record-file> — name the components that DIFFER between a record's
# key and the current one, and say what to do about it (card t_dbaccdfa).
#
# WHY THIS EXISTS. The refusal above was correct but unactionable. Measured on this
# host: `bash scripts/preflight_reuse.sh` with no environment file sourced prints
#
#   no reusable record for tree f600c45 — gating: the environment changed ...
#   testdb=unset;extra=140b...
#
# while the SAME tree, same bytes, with `/root/.config/jomform/test-env.sh` sourced
# reads REUSED. Nothing is broken: `preflight.sh` deliberately does NOT source the
# env file (so the runner and this reader compute the same key from the caller's own
# environment — see its pre-lock comment), the fingerprint includes the test database,
# and with `JOMFORM_TEST_DATABASE_URL` unset the integration tests really are a
# different observation. Measured: `go test ./internal/testdb/` RUNs 28 cases with the
# DSN unset (14 SKIP, "JOMFORM_TEST_DATABASE_URL not set; skipping integration test")
# and 52 with it set. So the refusal must stay.
#
# What was missing is the operator's NEXT MOVE. `testdb=unset` against a record that
# carries `testdb=loopback:5432/jomform_test` IS the difference, and reading it out of
# two 70-character fingerprint strings is a scavenger hunt at the end of a gate the
# caller may have waited an hour to not-run. The hint names the differing components
# and, when the current one is `unset`, prints the exact command that makes the record
# reusable. It never reuses anything: a changed fingerprint is still refused, only the
# refusal is now actionable.
gr_key_diff_hint() {
  local file="$1"
  local rec cur rd cd
  rec="$(gr_read_field "$file" key_parts)"
  cur="$(gr_key_parts "${PREFLIGHT_REUSE_REPO:-$PWD}")"
  [ -n "$rec" ] || return 0
  declare -A _seen=()
  local name
  # key_parts is `name=value` joined by `;`, so split on BOTH `;` and whitespace —
  # splitting on whitespace alone puts the whole string in one "component" (measured).
  local IFS='; '
  local pair
  for pair in $rec; do [ -n "$pair" ] && _seen["${pair%%=*}"]="${pair#*=}"; done
  for pair in $cur; do
    [ -n "$pair" ] || continue
    name="${pair%%=*}"; cd="${pair#*=}"
    rd="${_seen[$name]-<absent in the record>}"
    if [ "$rd" != "$cd" ]; then
      printf '  component %s DIFFERS: recorded [%s] vs now [%s]\n' "$name" "$rd" "$cd" >&2
      if [ "$cd" = "unset" ]; then
        printf '    -> the record was written with a real test database; this shell has none.\n' >&2
        printf '       Re-run after: source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}\n' >&2
      fi
    fi
    unset "_seen[$name]"
  done
  for name in "${!_seen[@]}"; do
    printf '  component %s is in the record but NOT in this shell: recorded [%s]\n' "$name" "${_seen[$name]}" >&2
  done
}
gr_record_usable() {
  local tree="$1"
  local file; file="$(gr_file "$tree")"
  [ -r "$file" ] || { echo "no record for tree $tree" >&2; return 3; }
  [ "$(gr_read_int "$file" format)" = "$GR_FORMAT" ] \
    || { echo "record $file has format [$(gr_read_int "$file" format)] — not this reader's format" >&2; return 3; }
  local rec_tree; rec_tree="$(gr_read_field "$file" tree)"
  [ "$rec_tree" = "$tree" ] \
    || { echo "record $file names tree [$rec_tree], not [$tree] — refusing" >&2; return 3; }
  local rec_key cur_key; rec_key="$(gr_read_field "$file" key)"; cur_key="$(gr_key "${PREFLIGHT_REUSE_REPO:-$PWD}")"
  [ -n "$rec_key" ] && [ "$rec_key" = "$cur_key" ] \
    || { echo "the environment changed since that record (recorded key [${rec_key:-none}] != current [$cur_key]: $(gr_key_parts "${PREFLIGHT_REUSE_REPO:-$PWD}"))" >&2; gr_key_diff_hint "$file"; return 3; }
  local fin fin_s now_s ttl
  fin="$(gr_read_field "$file" finished_at)"; ttl="$(gr_ttl)"
  fin_s="$(date -d "$fin" +%s 2>/dev/null)"
  now_s="$(date +%s)"
  if [ -z "$fin_s" ]; then
    echo "record $file has an unreadable finished_at [$fin] — refusing" >&2; return 3
  fi
  if [ "$((now_s - fin_s))" -ge "$ttl" ]; then
    echo "record $file finished ${fin} — older than the ${ttl}s horizon (PREFLIGHT_RESULT_TTL)" >&2; return 3
  fi
  # A RECORD FROM A RUN THAT DID NOT FINISH IS NOT A VERDICT (card t_d49a99f1).
  # The file is flushed after EVERY gate, so `finished_at` and a green `verdict`
  # describe the file's last write, not a completed run: a run killed mid-flight
  # (or one still going, read by the lane that is waiting on it) leaves a record
  # that is indistinguishable from a finished one by key + TTL alone. `complete`
  # is the runner's own statement that it reached the end of its inventory, and
  # only that statement may be reused. A record written before this field existed
  # carries no `complete` and is refused here — the safe direction, because every
  # such record was written by a different `scripts/gate_result.sh` and its key
  # cannot match this one anyway (the lib is key material).
  local rec_complete; rec_complete="$(gr_read_bool "$file" complete)"
  if [ "$rec_complete" != "true" ]; then
    echo "record $file is INCOMPLETE (complete=[${rec_complete:-absent}]) — the run that wrote it did not finish, so this file is not a verdict about tree $tree; gate it" >&2
    return 3
  fi
  # NOTE: the node ADVISORY is NOT printed from here. This function is called inside
  # command substitutions by both readers (`x="$(gr_record_usable ...)"`), where its
  # stderr is captured and — on the success path — thrown away, so a note written
  # here would never reach a human. The accept sites call gr_node_advisory directly
  # instead; see gr_node_advisory for why a differing node must not refuse.
  return 0
}

# gr_reusable <tree> <gate...> — 0 when the record for <tree> is a full green
# verdict for exactly these gates under the CURRENT fingerprint and inside the
# TTL. Every refusal reason is printed to stderr, so a lane reports WHY it gated.
#
# The node advisory is printed HERE, on the accept path, because this function is
# called directly (not inside a command substitution) by both readers — so its
# stderr reaches the lane. See gr_node_advisory: a differing node is a note, never
# a refusal, which is why it is printed and then IGNORED.
gr_reusable() {
  local tree="$1"; shift
  local file; file="$(gr_file "$tree")"
  gr_record_usable "$tree" || return $?
  local v; v="$(gr_verdict "$file" "$@")"
  case "$v" in
    green) gr_node_advisory "$file" || : ; return 0 ;;
    red)     echo "record $file is RED for this tree — a record can never turn a red verdict green; gate it" >&2; return 3 ;;
    partial) echo "record $file is PARTIAL (only some gates were observed) — gate it" >&2; return 3 ;;
    *)       echo "record $file has no verdict this reader understands" >&2; return 3 ;;
  esac
}

# ---- writer -------------------------------------------------------------------
# gr_flush <seconds> — write (merge) the record for the tree named by the GR_*
# globals above. Returns non-zero and prints nothing useful when it cannot write;
# the CALLER must announce that loudly and must NOT fail the gate run over it: the
# record is advisory, and a host-state problem must not turn a green tree red.
gr_flush() {
  local elapsed="${1:-0}" dir file tmp tree_key now verdict
  dir="$(gr_dir)"
  [ -n "$GR_TREE" ] || { echo "gate-result: no tree sha — record NOT written" >&2; return 1; }
  [ -n "$GR_PID" ] || GR_PID="$PPID"
  mkdir -p "$dir" 2>/dev/null || { echo "gate-result: cannot create $dir" >&2; return 1; }
  file="$(gr_file "$GR_TREE")"
  tree_key="$(gr_key "${GR_REPO:-$PWD}")"
  now="$(gr_now)"

  # ---- PROVENANCE: never let `commit` label bytes it does not name -------------
  # `tree` is the reuse key and it is the WORKING TREE (see gr_tree_sha), so a run
  # can gate content that no commit names — an uncommitted edit, or a merge-lane
  # worktree, which merges into the index/worktree while HEAD stays at the PRE-MERGE
  # base tip. In that second shape `git rev-parse --short HEAD` names the base, i.e.
  # the commit whose bytes were NOT gated, and a reader who trusts the label cites
  # the wrong artefact (measured on this host: record tree 08a5120f — the v1.21.197
  # merge — carried commit 087f1d3e, which is v1.21.196's merge, tree 064fb586).
  #
  # So the record now says it out loud, three ways that a grep, a human and a
  # machine can each use:
  #   * `commit` keeps naming HEAD but carries `+uncommitted` when the gated bytes
  #     are not HEAD's, so the label cannot be read as the gated artefact;
  #   * `head_tree` is `HEAD^{tree}` at the moment of the write — with `tree` beside
  #     it, `tree == head_tree` is decidable from the record alone;
  #   * `gated_is_head` is that comparison as a boolean, or `null` when HEAD's tree
  #     could not be computed at all. `null` is NOT `false`: a repo with no commits
  #     is "unknown", and claiming "not HEAD" there would be a different invention.
  #
  # None of this is key material and none of it changes the key: the reuse decision
  # stays derived from `tree` + environment, exactly as before (preflight_reuse.sh
  # reads the record unchanged).
  local head_tree gated_is_head commit_label
  if head_tree="$(gr_head_tree "${GR_REPO:-$PWD}")"; then :; else head_tree=""; fi
  if [ -z "$head_tree" ]; then
    gated_is_head=null
  elif [ "$head_tree" = "$GR_TREE" ]; then
    gated_is_head=true
  else
    gated_is_head=false
  fi
  commit_label="${GR_COMMIT:-}"
  if [ "$gated_is_head" = false ]; then
    commit_label="${commit_label:-unknown}+uncommitted"
  fi

  # Carry over a previous entry for a gate this run did not observe. A `skip` is
  # not an observation, so it must never erase a pass the lane is standing on.
  #
  # ONLY AN OBSERVATION IS CARRIED, AND ONLY AN OBSERVATION IS `reused: true`.
  # Carrying a previous `skip` forward as reused would label a gate "remembered
  # from an earlier run" when no run ever observed it — the record would then
  # claim a memory it does not have, which is exactly the kind of reading this
  # card exists to remove. A previous skip stays a skip, with `reused: false`.
  #
  # AND ONLY UNDER THE KEY THAT OBSERVED IT (card t_d49a99f1). The existing file's
  # entries were observed under the key THAT FILE names. When this run's key
  # differs — a re-installed `web/node_modules`, a bumped toolchain, an edited gate
  # script, another test database — carrying them would write another environment's
  # passes under THIS key, which is precisely what `gr_record_usable` promises a
  # reader never happens. So the previous key is read first, and a mismatch carries
  # NOTHING: those gates stay `skip` (unobserved) and the verdict cannot be `green`
  # until every gate really has an observation under this key.
  #
  # The comparison is against `$tree_key` — the key this write is ABOUT — not
  # against `gr_key` re-computed per gate, so the one value the file's `key` field
  # will carry is the same value the carry decision used.
  local prev_key="" prev_ok=0
  if [ -r "$file" ]; then
    prev_key="$(gr_read_field "$file" key)"
    [ -n "$prev_key" ] && [ "$prev_key" = "$tree_key" ] && prev_ok=1
  fi
  if [ -r "$file" ] && [ "$prev_ok" = 0 ] && [ -z "$GR_KEYNOTE" ]; then
    GR_KEYNOTE=1
    echo "gate-result: note — the record for tree $(gr_short_sha "$GR_TREE") was written under a DIFFERENT environment (key $(printf '%.12s' "${prev_key:-none}") vs current $(printf '%.12s' "$tree_key")), so its per-gate entries are NOT carried: every gate must be observed under the current key before the record can read green." >&2
  fi
  local g st sec at
  for g in "${GR_GATE_NAMES[@]}"; do
    st="${GR_STATUS[$g]:-}"
    if [ "$st" = "pass" ] || [ "$st" = "fail" ]; then
      GR_REUSED[$g]=false
      GR_CARRY_ST[$g]=""; GR_CARRY_SECONDS[$g]=""; GR_CARRY_AT[$g]=""
      [ -n "${GR_AT[$g]:-}" ] || GR_AT[$g]="$now"
      [ -n "${GR_SECONDS[$g]:-}" ] || GR_SECONDS[$g]=0
      continue
    fi
    if [ "$prev_ok" = 1 ]; then
      st="$(gr_gate_status "$file" "$g")"
      sec="$(gr_gate_entry_field "$file" "$g" seconds)"
      at="$(gr_gate_entry_field "$file" "$g" at)"
    else
      sec=""; at=""; st=""
    fi
    if [ "$st" = "pass" ] || [ "$st" = "fail" ]; then
      GR_CARRY_ST[$g]="$st"; GR_CARRY_SECONDS[$g]="${sec:-0}"; GR_CARRY_AT[$g]="${at:-$now}"; GR_REUSED[$g]=true
    else
      GR_CARRY_ST[$g]=""; GR_CARRY_SECONDS[$g]=""; GR_CARRY_AT[$g]=""
      GR_REUSED[$g]=false
    fi
  done

  # A record from a run that has not finished is NOT a verdict: `verdict` says so
  # in words a human reads, and `complete: false` is what the reader refuses.
  local complete_json=false
  if [ "${GR_COMPLETE:-0}" = 1 ]; then
    complete_json=true
    verdict="$(gr_verdict_of_current)" || verdict="partial"
  else
    verdict="in_progress"
  fi

  # `runs` / log carry over when the record already exists. `runs` counts RUNS, not
  # writes (see `runs` COUNTS RUNS, NOT WRITES): the increment happens on the FIRST
  # flush of each process and every later flush of that process keeps the number it
  # wrote, so a 23-gate run reports 1 run and a second run on the same tree reports 2.
  # Under a DIFFERENT key the count restarts at 1, for the same reason the gate
  # entries are not carried: the file's verdict then stands on this run alone.
  local runs=1 log="${GR_LOG:-}"
  if [ -r "$file" ]; then
    if [ "${prev_ok:-0}" = 1 ]; then
      runs="$(gr_read_int "$file" runs)"
      case "$runs" in ''|*[!0-9]*) runs=0 ;; esac
      [ "${GR_RUN_COUNTED:-0}" = 1 ] || runs=$((runs + 1))
      [ "$runs" -ge 1 ] || runs=1
    fi
    [ -n "$log" ] || log="$(gr_read_field "$file" log)"
  fi

  tmp="$file.tmp.$$"
  {
    printf '{\n'
    printf '  "format": %s,\n' "$GR_FORMAT"
    printf '  "tool": "scripts/gate_result.sh",\n'
    printf '  "tree": "%s",\n' "$GR_TREE"
    printf '  "repo": "%s",\n' "$(gr_json_escape "${GR_REPO:-}")"
    printf '  "commit": "%s",\n' "$(gr_json_escape "$commit_label")"
    printf '  "head_tree": "%s",\n' "$head_tree"
    printf '  "gated_is_head": %s,\n' "$gated_is_head"
    printf '  "branch": "%s",\n' "$(gr_json_escape "${GR_BRANCH:-}")"
    printf '  "key": "%s",\n' "$tree_key"
    printf '  "key_parts": "%s",\n' "$(gr_json_escape "$(gr_key_parts "${GR_REPO:-$PWD}")")"
    printf '  "gates": {\n'
    local n=${#GR_GATE_NAMES[@]} i=0
    for g in "${GR_GATE_NAMES[@]}"; do
      i=$((i+1))
      # The effective values: this run's observation if it has one, else the entry
      # carried under the SAME key, else `skip`. `reused` is the carried flag, so a
      # remembered entry can never be printed as one this run observed.
      gr_effective "$g" "$now"
      printf '    "%s": { "status": "%s", "seconds": %s, "at": "%s", "reused": %s }%s\n' \
        "$g" "$EFF_STATUS" "$EFF_SECONDS" "$EFF_AT" "$EFF_REUSED" \
        "$([ "$i" -lt "$n" ] && printf ',' || printf '')"
    done
    printf '  },\n'
    printf '  "passed": %s,\n' "$GR_PASSED"
    printf '  "failed": %s,\n' "$GR_FAILED"
    printf '  "skipped": %s,\n' "$GR_SKIPPED"
    printf '  "selected": "%s",\n' "$(gr_json_escape "${GR_SELECTED:-}")"
    printf '  "runs": %s,\n' "$runs"
    printf '  "seconds": %s,\n' "$elapsed"
    printf '  "verdict": "%s",\n' "$verdict"
    printf '  "complete": %s,\n' "$complete_json"
    printf '  "log": "%s",\n' "$(gr_json_escape "$log")"
    printf '  "finished_at": "%s"\n' "$now"
    printf '}\n'
  } > "$tmp" 2>/dev/null || { rm -f "$tmp"; echo "gate-result: cannot write $tmp" >&2; return 1; }
  mv -f "$tmp" "$file" 2>/dev/null || { rm -f "$tmp"; echo "gate-result: cannot replace $file" >&2; return 1; }
  # This process is now counted in `runs` (see the runs block above): later flushes
  # of the SAME run must not add another. Set only after the write landed, so a
  # failed flush does not consume the increment.
  GR_RUN_COUNTED=1
  return 0
}

# gr_effective <gate> <now> — set EFF_STATUS / EFF_SECONDS / EFF_AT / EFF_REUSED
# for <gate>, from the in-memory maps. ONE reading, used by BOTH the writer and
# gr_verdict_of_current, so the file's `verdict` can never be computed from a
# different set of statuses than the file prints:
#   * observed by THIS run (GR_STATUS)          -> its own values, reused: false;
#   * carried under the SAME key (GR_CARRY_*)   -> the observed run's values, reused: true;
#   * neither                                   -> `skip` as of this write.
gr_effective() {
  local g="$1" now="${2:-}"
  case "${GR_STATUS[$g]:-}" in
    pass|fail)
      EFF_STATUS="${GR_STATUS[$g]}"; EFF_SECONDS="${GR_SECONDS[$g]:-0}"
      EFF_AT="${GR_AT[$g]:-$now}"; EFF_REUSED=false; return 0 ;;
  esac
  case "${GR_CARRY_ST[$g]:-}" in
    pass|fail)
      EFF_STATUS="${GR_CARRY_ST[$g]}"; EFF_SECONDS="${GR_CARRY_SECONDS[$g]:-0}"
      EFF_AT="${GR_CARRY_AT[$g]:-$now}"; EFF_REUSED=true; return 0 ;;
  esac
  EFF_STATUS="skip"; EFF_SECONDS=0; EFF_AT="$now"; EFF_REUSED=false
  return 0
}

# gr_verdict_of_current — the verdict of the in-memory maps for the current gate
# inventory. Same rule as gr_verdict: a fail is red, an unobserved gate is partial.
# It reads through gr_effective, so "observed this run" and "carried under this
# key" are the only two things that can hold a `pass` — a carried entry from
# ANOTHER key is never in the maps at all (see the carry-over loop in gr_flush).
gr_verdict_of_current() {
  local g missing=0 n=0 now
  now="$(gr_now)"
  for g in "${GR_GATE_NAMES[@]}"; do
    n=$((n+1))
    gr_effective "$g" "$now"
    case "$EFF_STATUS" in
      fail) printf 'red\n'; return 0 ;;
      pass) ;;
      *)    missing=$((missing+1)) ;;
    esac
  done
  if [ "$n" -gt 0 ] && [ "$missing" -eq 0 ]; then printf 'green\n'; return 0; fi
  printf 'partial\n'
}

# ---- ledger -------------------------------------------------------------------
# gr_ledger — append-only event log of REUSE (not of gating). Acceptance for the
# card is "count runs that reused a record over 24h and the host-seconds they did
# not spend", so the claim has to be a measurement off real files rather than an
# estimate: ONE JSON LINE PER REUSE EVENT, summed by `preflight_reuse.sh --report`.
#
# ONE EVENT, ONE LINE — AND THAT IS ENFORCED HERE, NOT HOPED FOR. The documented
# lane path is consumer-then-runner (`docs/PREFLIGHT-REUSE.md`: run
# preflight_reuse.sh, then preflight.sh --reuse-record), and BOTH ends make the
# same decision from the same record and would otherwise both write. Measured,
# isolated: consumer-only 1 line / 588s, runner-only 1 line / 588s, but
# consumer-then-runner 2 lines / 1176s for 588s of real saving — `--report` is
# the instrument acceptance is read from, so the number a merger reads was double.
# The host ledger showed the same shape on every lane in its window (tree 53854bd
# at 1405s written by pids 391575/395208/1985442/2071621).
#
# THE FIX IS A DEDUPE AT THE WRITE, because the two writers are SEPARATE PROCESSES
# (a shell runs the consumer, it exits, then the runner starts): no exported
# marker can survive from one to the other, and a marker file would be more host
# state to keep honest. A line is SKIPPED when the ledger already carries the same
# event — same tree, same mode, same gates, same seconds — written within
# PREFLIGHT_LEDGER_DEDUPE_WINDOW (default 120s). The documented two-step path runs
# back-to-back (seconds apart), so it collapses to one line; a genuinely separate
# lane reusing the same tree more than the window later is still counted, which is
# correct — that really was a second saving.
#
# The trade, stated rather than implied: two DIFFERENT lanes that reuse the very
# same tree/mode/gates/seconds inside the window are counted once instead of
# twice. That under-counts rather than inflating, it is bounded by the window, and
# it is the direction a saving figure should err in.
gr_ledger() { # <tree> <mode full|partial> <remembered_seconds> <gates-csv>
  local dir; dir="$(gr_dir)"
  mkdir -p "$dir" 2>/dev/null || return 1
  local ledger="$dir/reuse-ledger.jsonl"
  local suffix="\"tree\":\"$1\",\"mode\":\"$2\",\"remembered_seconds\":${3:-0},\"gates\":\"$4\""
  local window="${PREFLIGHT_LEDGER_DEDUPE_WINDOW:-120}"
  if [ -r "$ledger" ] && [ "$window" -gt 0 ] 2>/dev/null; then
    local line ts now
    now="$(date +%s)"
    while IFS= read -r line; do
      [ -n "$line" ] || continue
      # Strip the leading timestamp, then require the event's IDENTITY as a PREFIX:
      # the line continues with `,"by":...,"pid":...`, which describe WHO wrote it
      # and must not make two writes of the same event look different.
      case "${line#*\",}" in
        "$suffix",*) ;;
        *) continue ;;
      esac
      ts="$(printf '%s' "$line" | sed -nE 's/^\{"at":"([^"]*)".*/\1/p' | sed -n 1p)"
      ts="$(date -d "$ts" +%s 2>/dev/null)"
      [ -n "$ts" ] || continue
      [ "$((now - ts))" -lt "$window" ] || continue
      # Same event, inside the window: the consumer already ledgered this reuse.
      local age=$((now - ts))
      echo "preflight: note — reuse already ledgered ${age}s ago for this tree (${1:0:7}) by pid $(printf '%s' "$line" | sed -nE 's/.*"pid":([0-9]+).*/\1/p'); not counting it twice." >&2
      return 0
    done < "$ledger"
  fi
  printf '{"at":"%s","tree":"%s","mode":"%s","remembered_seconds":%s,"gates":"%s","by":"%s","pid":%s}\n' \
    "$(gr_now)" "$1" "$2" "${3:-0}" "$4" "$(gr_json_escape "${GR_REPO:-}")" "${GR_PID:-$$}" \
    >> "$ledger" 2>/dev/null || return 1
  return 0
}

# gr_gates_with_pass <file> <gate...> — the gates the record holds an observed
# `pass` for, space-separated. Only `pass` qualifies: a `fail` or a `skip` is not
# a voucher and must be re-run.
gr_gates_with_pass() {
  local file="$1"; shift
  local out="" g
  for g in "$@"; do
    [ "$(gr_gate_status "$file" "$g")" = "pass" ] || continue
    out="$out$g "
  done
  printf '%s' "${out% }"
}

# gr_gates_reused_seconds <file> <gate...> — the seconds that would NOT be spent
# if every `pass` in the record is remembered: the sum of the recorded per-gate
# seconds for the gates named. The gates named are the ones the caller will NOT
# re-run.
gr_gates_reused_seconds() {
  local file="$1"; shift
  local g total=0 sec
  for g in "$@"; do
    [ "$(gr_gate_status "$file" "$g")" = "pass" ] || continue
    sec="$(gr_gate_entry_field "$file" "$g" seconds)"; sec="${sec:-0}"
    total=$((total + sec))
  done
  printf '%s\n' "$total"
}
