# Preflight gate results — one record per tree

`scripts/preflight.sh` writes a **result record** for every tree it gates.
`scripts/preflight_reuse.sh` reads it. Together they make a gate verdict a
property of the **tree** rather than of the **moment the gate happened to run**.

## Why

Measured on the agent host: 509 completed full preflight runs over 6.4 days
(97.1 host-hours of gate time) across ~384 distinct trees.

| | runs | host-hours |
|---|---|---|
| total full runs | 509 | 97.1 |
| on a tree that had **already** been gated | 206 (39.4%) | 38.3 |
| — the prior run was **green** | 58 | 17.0 |
| — the prior run was **red** | 148 | 21.3 |

The worst tree was gated **ten** times. And 28 of the 125 repeatedly-gated trees
(22%) came back with **both** verdicts on identical bytes — `R R R R R R R G R`,
`G R G`, `G R R`.

That is the correctness problem, not the waste. Each attempt sat behind the
`selftest`-era host-wide `flock` and learned nothing from the attempt ahead of it,
so a verdict that should describe the bytes described instead how loaded the host
was at that minute. A gate whose verdict depends on when it ran is not a gate, and
it is not what a merger needs when it decides to squash.

## The record

`${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/<tree-sha>.json`

```json
{
  "format": 1,
  "tool": "scripts/gate_result.sh",
  "tree": "<tree sha of the WORKING TREE>",
  "repo": "/root/wzul-git/jomform/.worktrees/t_dbd93601",
  "commit": "3fba5ff",
  "branch": "jomform/t_dbd93601-...",
  "key": "<sha256 of the INVALIDATORS in key_parts>",
  "key_parts": "go=go1.27.0;webdeps=<hash>;gatescript=<hash>;resultlib=<hash>;testdb=<host/db>;extra=<hash>;node=v26.7.0",
  "gates": {
    "build": { "status": "pass", "seconds": 12, "at": "2026-09-27T07:33:05Z", "reused": false },
    "a11y":  { "status": "fail", "seconds": 41, "at": "2026-09-27T07:35:02Z", "reused": false }
  },
  "passed": 13, "failed": 1, "skipped": 0,
  "selected": "build,vet,...",
  "runs": 3,
  "seconds": 1338,
  "verdict": "red",
  "log": "/root/.hermes/profiles/qa/cache/scratch/preflight.log",
  "finished_at": "2026-09-27T07:35:02Z"
}
```

### What each part is for

* **`tree`** — the sha of the **working tree**, computed with a private index
  (`read-tree HEAD` + `add -A` + `write-tree`). It honours `.gitignore`, includes
  uncommitted edits, and equals `HEAD^{tree}` for a clean checkout. **Not** the
  commit sha: a merge-tree result is a different object from a branch head, and a
  record may only be reused for the bytes it actually gated.
* **`key` / `key_parts`** — everything that makes a verdict describe an
  *environment*, not just a tree: the Go toolchain, the installed web dependency
  set (`web/node_modules/.package-lock.json`), `scripts/preflight.sh` and
  `scripts/gate_result.sh` themselves, the test-database host/database, and
  `PREFLIGHT_KEY_EXTRA` for anything else. Credentials are stripped, and **which
  shape a value has decides how**. A value carrying a **`scheme://`** is stripped
  at its authority's last `@` — the anchor Go's `net/url` uses — so a password
  containing `@` or `/` (what a base64 or URL-encoded password routinely carries)
  does not survive into the record. The query string is dropped before that, and
  the scheme with it, so `?options=…` never reaches the file either. A value whose
  **authority carries no userinfo** and whose database name does
  (`…/hostA:5432/db@x`) is kept whole: the text before the first `/` is the ADDRESS
  the driver resolves (measured with the pinned pgx v5.10.0), so that `@` is not a
  credential delimiter, and stripping it would collapse `hostA:5432/db@x`,
  `hostB:5432/db@x` and `hostA:5432/other@x` onto ONE component and ONE key — a
  record written against hostA accepted as evidence about hostB. A bracketed IPv6
  authority (`[::1]:5432`) is an address too and is kept for the same reason. Where
  the authority is neither an address nor clean userinfo — `…://u1:pw@host:port/db`,
  a shape the driver itself REFUSES — the value is stripped at its last `@`: that
  direction can only refuse a record, and it never leaves a credential on disk. A
  value with **no `scheme://` at all** — PostgreSQL's keyword/value DSN
  (`host=… password=… user=…`), which the API's own driver accepts — is **not**
  stripped and is **refused**: the component becomes the literal token `refused`
  and the value never reaches the fingerprint. That is deliberate: there is no
  scheme to strip and no authority to anchor on, and every `key=value` pair is
  indistinguishable from any other (`password=` is not syntactically special — the
  driver takes an arbitrary key as a runtime parameter), so a strip could not be
  shown to have removed the userinfo. Refusing is the conservative direction: two
  different databases under an un-strippable DSN can only make records look
  *alike*, never make a record about one database read as evidence about another.
  So the component this produces is always `<host>[:port][/database]`; what the
  strip *removes* is the credential, and what it KEEPS is the host, port and
  database, because a record made against another database is not evidence about
  this one.

  **The host is a fact about the SERVER, not about its spelling:** `localhost`,
  `127.0.0.1` and `[::1]` are canonicalised to the single token `loopback`, so one
  database yields one key however the caller wrote it. Before this rule the URL's
  host string was hashed verbatim, and a gate that ran green on tree `f98b5fcf`
  under the test-env file (`` `${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}` ``,
  which exports `127.0.0.1`) was refused by the very next reader that spelled it
  `localhost` — same server, same database, caller's environment unchanged —
  because the two spellings hashed to different keys. The refusal says only "the
  environment changed since that record", so the lane re-queued behind the
  host-wide slot pool (waiter measured at 50-86 minutes) to re-run fifteen gates that
  had already passed on those exact bytes. The canonicalisation is an ANCHORED
  match on `<host>[:port][/database]` only — which is exactly the form the strip
  above guarantees, so `[::1]:5432/db@x` is canonicalised in its host slot while
  its port and database stay nameable. Every other host is hashed verbatim, and a
  second server (another port) or a second database (another name) still produces a
  different key, so a record can never be evidence about another database. The
  **Node version is recorded in `key_parts` but is NOT hashed into `key`**: it is a
  property of the shell on this host, not of the tree, and pinning it made reuse
  dead between the two shapes that actually run the gates (an interactive shell
  with nvm, and a worker lane that resolves `/root/.hermes/tools/node-*/bin`),
  which refused records on byte-identical trees. A run under a different Node still
  accepts the record but says so out loud (`OBSERVED under node …; this run
  resolves node …`), and the installed web dependency set — which *is* hashed — is
  what carries the toolchain signal. **The canonicalisation changes the canonical
  form, so a record written before it carries `testdb=127.0.0.1:…` and is refused
  once** (the key material moved; that is rule 3 below, not a fault). A record
  written after it carries `testdb=loopback:…` and is reusable under either
  spelling.
* **`gates`** — per-gate verdict with the seconds it took and a `reused` flag.
  `reused: true` means a **later** run carried this entry over rather than
  re-running the gate; the `at` timestamp still names the run that **observed** it.
* **`finished_at`** — records older than `PREFLIGHT_RESULT_TTL` (default 86400s)
  are treated as absent, so a cached pass cannot outlive its usefulness.
* **`verdict`** — `green` only when **every** gate in the inventory holds an
  observed `pass`; **`red`** if any gate holds a `fail`; otherwise `partial`.

## Using it

```bash
# Should I gate this tree at all? (no lock taken, cheap, read-only)
./scripts/preflight_reuse.sh
#   exit 0 -> a record exists; use the printed PREFLIGHT_SKIP / PREFLIGHT_RERUN
#   exit 1 -> no usable record; gate normally

# The same question about an EXPLICIT tree — the tree you just pushed, or the bytes
# a bite mutation is about, i.e. bytes that are no longer in your working tree.
# `--tree <sha>` and `--tree=<sha>` both reach the DECISION (not just --show).
./scripts/preflight_reuse.sh --tree <tree-sha>

./scripts/preflight_reuse.sh --show            # the raw record (evidence)
./scripts/preflight_reuse.sh --report 24       # the MEASURED saving, last 24h

# Full green already on record: nothing to run.
./scripts/preflight.sh --reuse-record

# A prior run was red: re-run only what it could not vouch for.
./scripts/preflight.sh --only <PREFLIGHT_RERUN> --reuse-record
```

As a library, so a lane can label its own evidence:

```bash
. scripts/preflight_reuse.sh
preflight_reuse_decision && echo "$PREFLIGHT_EVIDENCE"
# PREFLIGHT_DECISION=full|partial|none, PREFLIGHT_SKIP, PREFLIGHT_RERUN,
# PREFLIGHT_REMEMBERED_SECONDS
```

Pass `PREFLIGHT_ONLY=<csv>` before calling `preflight_reuse_decision` when the
run will use `preflight.sh --only <csv>`, so the consumer and the runner answer
the same question. The runner does this itself on its own reuse path, so
`--only vet --reuse-record` asks about `vet` and reports (and ledgers) only `vet`.

`--log <path>` records where the run's output is being captured, so the next lane
reads the evidence and not just the verdict.

## The safety rules (do not "simplify" these away)

1. **Reuse may never turn a red into a green.** A gate is only ever skipped on an
   observed `pass` for *this* tree under *this* fingerprint. A `fail` anywhere in
   the record refuses the reuse outright. The aggregate rule: a run is green only
   when every gate it would otherwise execute holds a recorded pass.
2. **A reused gate is labelled, and never counted as `passed`.** The summary
   prints a separate `REUSE:` line (`REMEMBERED from a recorded pass on tree …,
   NOT re-run this time`), and `passed` counts only gates this process executed.
   A human reading a verdict can always tell whether a gate ran or was remembered.
   The corollary matters as much as the rule: a reused gate is *evidence*, so it
   must never also appear on a line that means "no evidence" — `SKIP: … (not
   evidence)` and `STILL SKIPPED AND NOT REUSED (no recorded pass)` name only the
   gates this run has nothing to say about. A gate on both the `REUSE:` and a
   no-evidence line is a contradiction a reader cannot resolve, so a per-gate
   reuse run subtracts the reused set from both of those lines.
3. **The environment change invalidates.** Bump a toolchain, edit
   `scripts/preflight.sh`, or reinstall `web/node_modules` and the record is
   refused — `scripts/preflight.sh --reuse-record` fails loudly rather than
   reporting a green it did not earn.
4. **The record is advisory, and a host-state problem is never a red gate.**
   If the record cannot be written the run continues with a warning; a merger
   still needs a verdict for the exact bytes it merges.
5. **A whole-inventory reuse is decided BEFORE the lock, and only there.** Reuse
   exists so a lane about to re-gate a green tree does not queue: a full-green
   record is re-read outside `flock` and the run exits immediately. Everything
   else (partial, red, absent) takes the lock and behaves exactly as before. The
   in-lock block is the per-gate path only. There is exactly one place a
   whole-inventory reuse is granted, so there is no second place to disagree with
   the first.
6. **A reuse answers the question it was asked.** A run with `--only <subset>`
   reuses (and ledgers) only the gates in that subset; the runner passes its own
   selection to the consumer, so a one-gate run can never report — or claim the
   saving for — the whole inventory. The TREE half of the same rule: `--tree <sha>`
   reaches the DECISION, and the printed `PREFLIGHT_TREE` and evidence line name
   that tree. A lane asking about bytes that are no longer in its working tree —
   the tree it just pushed, or the tree a bite mutation is about — must not be
   answered about the working tree instead (that silently wrong answer cost a full
   gate run for bytes already vouched for; card `t_912df3a4`).
7. **A gate the caller skipped is not a reuse.** Before the reuse decision, the
   vouched set is compared against what this run was actually going to execute.
   A record may vouch for `monitor` while the run is `--only vet`; naming
   `monitor` as saved would claim a saving this run never made and would put a
   reused gate on the `SKIP: … (not evidence)` line at the same time — a
   contradiction for a reader. Such gates are named on the `not in this run's
   selection` line and are **not** ledgered and **not** folded into `SKIP`.
   The refusal for a voucher-less record is decided **before** this filter, so a
   record that vouches for nothing is still refused loudly rather than quietly
   reported as "nothing to reuse".
8. **One reuse event, one ledger line.** The consumer (`preflight_reuse.sh`) and
   the runner both see the same reuse; the ledger is an append-only log of
   *events*, so the second writer must not double the measured saving. A write
   is suppressed when an identical event (same tree, mode, remembered seconds and
   gate set) is already in the ledger inside
   `PREFLIGHT_LEDGER_DEDUPE_WINDOW` (default 120s). The identity deliberately
   excludes the `by`/`pid` fields, which describe *who* wrote the line.

## Layout

| Path | Role |
|---|---|
| `scripts/gate_result.sh` | the record format: readers, writer, reuse rules, ledger |
| `scripts/preflight_reuse.sh` | the consumer a lane runs **before** queueing |
| `scripts/preflight.sh` | writes the record per gate, and `--reuse-record` |
| `${PREFLIGHT_RESULT_DIR:-/var/lib/jomform/gate}/reuse-ledger.jsonl` | one line per reuse, with the host-seconds it avoided |

## The measured saving

The ledger is an append-only file of reuse **events**, so the saving is counted
from real files rather than estimated:

```bash
./scripts/preflight_reuse.sh --report 24
#   reuse events in window : 12  (full-green 5, partial 7)
#   host-seconds NOT spent : 7421s  (2.06 host-hours)
```

## Overridable

| Variable | Default | Meaning |
|---|---|---|
| `PREFLIGHT_RESULT_DIR` | `/var/lib/jomform/gate` | where records and the ledger live |
| `PREFLIGHT_RESULT_TTL` | `86400` | seconds a record stays usable |
| `PREFLIGHT_KEY_EXTRA` | *(unset)* | fold anything else into the fingerprint |
| `PREFLIGHT_ONLY` | *(unset)* | tell the consumer which gates the run will select |
| `PREFLIGHT_LOG` | *(unset)* | log path recorded as evidence |
| `PREFLIGHT_LEDGER_DEDUPE_WINDOW` | `120` | seconds inside which an identical reuse event is not ledgered twice (0 disables) |

## What this does **not** do

* It does not change the gate list, the `a11y` split, or the reuse rules.
* **The gate pool is separate from this file** (card `t_79c0964a`). The gate runs
  used to be serialised behind ONE host-wide `flock`, on the premise that two
  concurrent runs share a scratch schema and a scratch valkey index. Both are
  per-PROCESS now (`test_<pkg>_p<pid>`, `jftest:p<pid>:` on a pid-derived DB), so
  the premise was stale and the lock only serialised lanes; it is now a capped
  POOL of `PREFLIGHT_SLOT_COUNT` slots (default 2), so two lanes gate in parallel
  while the host stays bounded. A whole-inventory reuse still takes NO slot, which
  is why the pre-lock check here is now the pre-POOL check.
* It does not allow a **partial** run to be read as full coverage: a run with
  skipped gates still prints the loud warning and still says it is not full
  coverage.
* It does not re-stamp a record on reuse. A record keeps the `finished_at` of the
  run that **observed** it, so the age shown is the age of the observation.
