# Production monitoring

Two things watch JomForm production: a **one-file uptime + disk watchdog** that
runs on this host and alerts into Telegram, and the API's own Prometheus
`/metrics` endpoint. There is deliberately no monitoring stack — no Prometheus
server, no Grafana, no agent. The goal is not dashboards; it is not being blind.

## What the watchdog checks

`deploy/monitoring/jomform-watch.sh` runs every 5 minutes and checks five things.
Each one can fail on its own and each is named in the alert.

| Check | What it proves | Why it is here |
|---|---|---|
| `web` | `GET https://jomform.com/` returns 200 **and** contains `/_next/static/` | Cloudflare → Traefik → web container — the path a real visitor takes |
| `health` | `GET https://jomform.com/healthz` returns 200 and contains `service` | Cloudflare → Traefik → api container |
| `ready` | the api container's own `/readyz` reports `database: ok` **and** `valkey: ok`, read over ssh | `/healthz` answers 200 while the database is down; without this, login and payments could be broken and the watchdog silent |
| `disk` | `df` on the prod host stays under the threshold | prod had no disk alarm at all |
| `board` | the agent host's Kanban `task_runs` ends `crashed`/`gave_up`/`spawn_failed` **fewer than `JF_WATCH_KANBAN_THRESHOLD` times** in a trailing `JF_WATCH_KANBAN_WINDOW_SECONDS` window | see below — the host fault that produced 110 crashed runs and paged nobody |

Three of those choices are load-bearing and worth stating, because the obvious
shorter version of this script does not catch them:

- **The web check uses the page, not `/healthz`.** A dead web container leaves
  `/healthz` answering 200 (it is routed straight to the API), so an `/healthz`-only
  watcher would report healthy while every visitor got an error page.
- **`/readyz` is checked over ssh.** It is not exposed through Traefik (an
  unauthenticated dependency readout does not belong on the public edge), and
  `docker compose exec -T` **reads stdin** — without `< /dev/null` the compose
  client swallows the ssh channel and the probe hangs.
- **`board` reads the DB, not the cards.** A host fault that makes the dispatcher
  spawn a worker on an interpreter that cannot import its own module kills every
  card on the board independently: each one is requeued, retried, and eventually
  blocked, so the board looks like N separately broken cards and nothing about
  the *host* is being measured. No monitor read `task_runs` at all, so 110 runs
  could end `crashed` (9 of them in the last 3 h) without a page.

The `board` check is **off** where there is no board (the prod host) and that is
not silent: the daily digest reports `board: NOT MEASURED here`, because a check
that quietly does nothing is the same blindness in a new place. Point it at a
board with `JF_WATCH_KANBAN_DB=/path/to/kanban.db`; `auto` (the default) probes
the two usual Hermes board paths. Set but unreadable — a deleted or misnamed
board — is a **failure**, like an unreadable `df`.

A board that reads fine but holds one row whose `metadata` is **not** JSON is not
that case, and treating it as one is a trap: the `infrastructure` count reads
`metadata` through `json_valid(metadata)` first, because SQLite **raises**
`malformed JSON` on a non-JSON argument instead of returning `NULL`. One such row
used to abort the whole five-scalar query, which was both directions wrong at
once — the operator was paged `unreadable` for a board that reads perfectly, and
the check stopped **measuring**, so a genuine burst in the same window paged
nobody. `import_err=` is fetched by that same SELECT and went with it.

A marker check is part of the HTTP checks on purpose: a proxy or hosting
provider's error/maintenance page answers **200**, so status code alone cannot
tell "our app" from "something else wearing our hostname".

## Delivery and noise

Alerts go straight to the Telegram Bot API from the script. The alert path must
not depend on the agent stack it is watching.

- **Edge-triggered.** One message when a check goes down, one when it recovers.
  A repeat every `JF_WATCH_REALERT_SECONDS` (default 30 min) while it stays down.
  Without this, an outage that lasts the night is 100 messages, which trains
  everyone to mute the channel.
- **Daily digest** (`JF_WATCH_MODE=digest`, 09:17) is the watchdog's own
  heartbeat: checks run, failing runs, alerts sent, prod disk, and the board
  check's own state (`board: …`, or `board: NOT MEASURED here` where the host has
  no board). If the digest stops arriving, the *watcher* is dead — which is
  otherwise indistinguishable from "everything is fine".
- **Exit codes:** `0` healthy, `1` something failed (alert attempted), `2`
  misconfigured (no notifier), `3` an alert could not be delivered. A watcher
  that cannot alert **refuses to run** instead of reporting `ok`.

## Install

On the host that will run the watchdog (it needs `curl`, `ssh`, `jq` is not
required):

```bash
deploy/monitoring/install-jomform-watch.sh \
  --telegram-token <bot-token> --telegram-chat <chat-id>
```

The installer will not arm the cron entry until it has **proven delivery** — it
fires a deliberate failure through the same notifier the real alerts use and
aborts if Telegram did not accept it. An installed-but-mute watchdog is the
exact failure this work exists to remove, so that proof is a precondition, not a
suggestion.

It installs:

| Path | Contents |
|---|---|
| `/usr/local/bin/jomform-watch.sh` | the watchdog |
| `/etc/jomform-watch.env` | config, `0600` root (see `jomform-watch.env.example`) |
| `/etc/cron.d/jomform-watch` | check every 5 min, digest daily 09:17 |
| `/var/log/jomform-watch.log` | one line per pass, self-rotating in place |

Run `--dry-run` to see the plan without writing anything.

## Proving it bites

This is the only test that matters, and it is specified in the card that added
this: an alerting path never observed firing is not proven.

```bash
# on the prod host
docker stop jomform-api-1     # expect an alert within ~5 minutes
docker start jomform-api-1    # expect a RECOVERED message
```

Expected messages, in order:

```
⚠️ JomForm prod alert
JomForm watchdog on <host> (check #N)
DOWN health: health code=000 err=… [https://jomform.com/healthz]
```

```
✅ JomForm prod recovered
RECOVERED health: health code=200 bytes=36
```

`docker stop jomform-web-1` exercises the `web` check; `docker stop` on the
postgres container exercises `ready` (the `health` check stays green, which is
the point — it demonstrates why the dependency probe exists).

## The offline self-test

`deploy/monitoring/jomform-watch_test.sh` runs in the preflight gate (`monitor`)
and needs no network and no prod host: a stub HTTP server plus a fake notifier.
It asserts the properties that make a watchdog worth having —

- an alert actually fires, names the failing check, and carries the evidence;
- a repeat failure does **not** re-alert, and a recovery does;
- 200-with-the-wrong-body is a failure, not a pass;
- a transient failure that succeeds on retry raises nothing;
- a degraded or unreachable `/readyz` alerts;
- an over-threshold disk alerts, and an unreadable `df` fails loud;
- a **board crash burst pages** (the 110-crashed-runs incident), naming how many
  distinct cards and lanes it hit and how many carried the spawn-import
  signature; two crashes do not; card-level outcomes (`timed_out`, `reclaimed`,
  `blocked`, review states) never do; a configured-but-unreadable board fails
  loud; a board holding one **non-JSON `metadata` value** is *not* unreadable —
  it still measures and still pages, asserted in both directions (the bad row
  alongside a sub-threshold count stays quiet, an over-threshold burst that
  includes it still alerts with every scalar intact); a host with no board stays
  quiet *and says so* in the digest; and reading
  the board **never takes a write lock and never modifies the `.db`** — its bytes
  (sha256) and its mtime are identical before and after a pass, asserted in
  **both** journal modes. On a **WAL** board (what the agent host's boards are)
  the read-only open additionally *materialises* the `-wal`/`-shm` pair when
  nothing else holds them; that is normal SQLite WAL reader behaviour, not a
  write, and the test pins it that way rather than claiming it never happens.
  (`immutable=1` is the only URI mode that skips locking/change detection, and it
  is deliberately **not** used: it would blind the check to the dispatcher's
  writes while it runs.)
- **no notifier is exit 2, never `ok`**, and an undeliverable alert is exit 3.

The board's worktree pruner is the other host-only safety script in this
directory's family; its behaviour and self-test are documented under
[Kanban worktree pruner](#kanban-worktree-pruner--what-it-may-and-may-not-delete).

Mutations that were run against it to prove it *bites* (each turned the suite
red): dropping the body-marker check → 3 failures; alerting on every run instead
of edge-triggered → 5 failures; treating `exit 2` as `exit 0` → 2 failures;
skipping `/readyz` on failure → 5 failures; running the whole suite against the
**pre-`board` watchdog** (before the check existed) → 25 failures, all of them
the new board assertions, which is the measurement that says the board cases
test the fix rather than restating it.

Two more, added with the WAL correction to case 22 (measured in a throwaway
copy, control green at 103/0 first): restoring the **pre-fix case 22** — the old
"leaves no `-wal`/`-shm` behind" claim, run against a **WAL** board — → 1 failure,
`watching the board left a -wal/-shm behind`, which is precisely the assertion
that was a tautology on a rollback-mode fixture; and making the check **write**
to the board (dropping the `-readonly` guard and adding a real `PRAGMA`) → 3
failures, every one of them a new fingerprint/sidecar leg — the `.db`
sha256+mtime is now pinned on both journal modes.

The `json_valid(metadata)` guard was proven the same way, on the **shipped**
bytes (the mutated copy is byte-identical to `git show
v1.19.12:deploy/monitoring/jomform-watch.sh`, sha256 `a601b71c…`): control
`115 passed / 0 failed`, then the guard line deleted → **10 failures, 105
passed**, every one of them the new non-JSON case, each reading
`board unreadable db=… err=Error: stepping, malformed JSON` — i.e. the exact
false page and the exact lost measurement the check exists to prevent; guard
restored → back to `115 passed / 0 failed`.

## Kanban worktree pruner — what it may and may not delete

`deploy/worktrees/prune-kanban-worktrees.sh` is the host-side reclaimer for the
agent board's worktrees. Each card worktree carries its own `web/node_modules`
(442M) + `.next` (146M), so unfinished reclamation is ~9G/day, and nothing else
reclaims them (`hermes kanban gc` prunes events and logs, not worktrees). It runs
from the Hermes cron job `prune-kanban-worktrees` (daily `0 4 * * *`, a
`no_agent` job, so it is a script run and not an agent turn).

Before removing anything it requires **all** of: not the main checkout; a
terminal card status; a completion older than `--min-age-hours` (24h); no live
process using the worktree or the card — neither one whose **argv** names it nor
one whose **working directory** is inside it; a clean tree; and — the one this
section is about — **durable** work.

**"Durable" means reachable from a remote-tracking ref of `origin`**
(`refs/remotes/origin/*`), or merged into `origin/main`. It deliberately does
**not** mean "some ref under `refs/remotes/` holds it". Work held only by a
hand-made `refs/remotes/pr/<N>` mirror is reported `UNSAFE` and **kept**.

Why the distinction matters: a `refs/remotes/pr/<N>` mirror
(`git fetch origin pull/N/head:refs/remotes/pr/N`, the documented GitHub idiom)
is in **no refspec**, so `git fetch --prune` never removes it — pruning is a
function of the remote's refspec — and it keeps pointing at whatever a PR's head
was when it was made, i.e. the pre-rebase / pre-force-push snapshot. A durability
test written as `git branch -r --contains <sha>` lists those mirrors too, so a
detached worktree holding the last copy of a commit read as "pushed to a remote"
and was removed. That is not hypothetical: it is how the defect this section
records was reproduced, and it is why the test names `refs/remotes/origin`
explicitly and prints the non-remote-tracking refs it refused to trust
(`[ignoring non-remote-tracking ref(…)]` in the log).

**"No live process" is two reads, and the second one is why a build survives.**
A process can hold a worktree two ways: its argv can name the path (a worker's
`work kanban task <id>`, an editor opened on a file in there, a shell that has not
exec'd its last command yet), or its **working directory** can be inside it — the
shape a real build has. `cd <wt> && npm run build` makes the shell exec the last
command of the `&&` list, so the path leaves argv entirely: the process is
`npm run build`, and npm, node and everything they start inherit cwd `<wt>`. An
argv-only guard cannot see that, which is how a stray was removed with a build
running inside it (`rm -rf` of 442M `node_modules` + 146M `.next`). Both reads are
kept because neither subsumes the other: a build that has not exec'd yet is visible
only in argv, an exec'd one only in cwd. The cwd read is the expensive one (~0.1s
for the whole process table, against ~30ms for the argv read), so it is made **last
in each loop — immediately before the removal or the `rm -rf` it guards** — where it
can still change the outcome and where it is the freshest answer for that decision.

A cwd read that **fails** is neither a keep nor a removal, and the process state
separates the two cases. A **zombie** (`State: Z`) is dead and holds nothing, so it
is skipped — measured on the agent host, every one of the 15 pids whose cwd read
failed was a zombie, so reading a failed read as "keep" would keep everything and
exit `1` on every run. Any other state is a live process we could not inspect: the
worktree is left on disk **and the run exits `1`**, with a `FAIL` naming the pid. A
silent keep there would hide a pruner that has stopped reclaiming; removing on a
read that was never made is the one irreversible mistake this script exists to avoid.

**A refusal is not a failure.** Keeping work is this script's normal, deliberate
outcome: an unpushed local branch, a hand-made mirror, a live worker, a card
younger than `--min-age-hours`, a dirty tree. Every one of those is named in the
log, and the run still exits `0`, because treating them as errors makes the job
status an alarm that can never clear. Measured on the agent host before this
change: **11 of 20** logged runs exited 3, the **last 7 of 7** did, and **528 of
567** refusals were a local branch whose commit still exists in a ref — i.e. the
code was the steady state, not a signal. The codes now mean:

| Code | Meaning | Alert? |
|---|---|---|
| `0` | the run did its job: every removal it attempted succeeded, and whatever it kept, it kept on purpose | no |
| `1` | the run could not do its job — a `git worktree remove` FAILED, a card could not be read (so its worktree can never be judged), or `git fetch` failed and the durability verdict came from stale refs | yes |
| `2` | bad arguments | — |
| `3` | **no progress**: nothing was reclaimed while work whose only copy may be a kept directory was kept — the pruner-has-started-keeping-everything alarm | yes |

Exit `3` is deliberately narrow. A busy board that keeps a few at-risk
directories while reclaiming the reclaimable rest is exit `0`; the alarm is the
shape where the pruner stops reclaiming *anything*. `--no-fetch` is a deliberate
caller choice (the log says so) and is never exit `1`; a failed fetch is. A
`--dry-run` reclaims nothing *by design*, so it is never judged on progress.

### Operating it

```bash
deploy/worktrees/prune-kanban-worktrees.sh --dry-run    # what it would remove; changes nothing
deploy/worktrees/prune-kanban-worktrees.sh --min-age-hours 72
tail -5 /root/agents/prune-kanban-worktrees.log
```

`--no-fetch` skips `git fetch --prune origin`, which the durability test depends
on; using it means judging on whatever the refs happen to be, and the log says so.

Install or update it on a host with the installer — **do not edit a host copy in
place**:

```bash
deploy/worktrees/install-prune-kanban-worktrees.sh --dry-run
deploy/worktrees/install-prune-kanban-worktrees.sh
```

The cron job resolves its `script:` name under that Hermes home's `scripts/`
directory, so the copy that *runs* is not necessarily the copy you would edit by
path (the agent host has two copies, in `/root/.hermes/scripts` and
`/root/agents`). The installer validates the source with its own self-test,
installs, then **re-runs the self-test from the installed location**, and reports
whether the cron job that names that filename exists.

### The offline self-test

`deploy/worktrees/prune-kanban-worktrees_test.sh` runs in the preflight gate
(`prunewt`) and needs no board, no network and no real worktree: a sandbox repo
with its own bare `origin`, plus a fake `hermes kanban show` answering from a
state file. It asserts, as outcomes read off real runs —

- a real `git fetch --prune origin` leaves a `refs/remotes/pr/N` mirror in place
  (measured, not quoted);
- the **old** predicate (`git branch -r --contains`) calls mirror-only work
  `pushed-branch` while the script's predicate does not — the control that keeps
  the fix honest;
- a mirror-only worktree **survives**, with the offending ref named in the log;
- genuinely pushed work and work merged into `origin/main` are still **removed**,
  so the fix cannot pass by refusing everything;
- work held only by a local branch, a dirty worktree, a too-recent card, a card
  still running, an unreadable card, a worktree holding a commit on no ref at all,
  and a worktree with a live worker process are each kept, each with its own reason
  in the log;
- the **exit-code contract** is read off real runs: a keep-on-purpose run with a
  reclaim exits `0`; the same at-risk set with nothing reclaimed exits `3`; a
  removal git refuses (a locked worktree), an unreadable card, and a failed fetch
  each exit `1`; `--no-fetch` stays `0`; a `--dry-run` is never judged on progress;
- **every branch of that contract**, not just the three above, has its own run behind
  it (card `t_7360634f` measured four of them green against one-armed mutants): the
  STRAY loop's refused removal (a locked stray) exits `1` with `failures=1`, the CARD
  loop's and the STRAY loop's unresolvable HEAD each exit `1` and log a `FAIL` — never
  a silent `KEEP` — and an unknown argument exits `2` having run nothing at all;
- the stray-worktree loop (outside `.worktrees`, no card reference) gets the same
  correction;
- a directory that is the **working directory** of a live process survives — the
  shape a real `cd <wt> && npm run build` has, where the shell execs the build so the
  path is in the kernel's cwd and in no argv — and the same directory is removed once
  that process is gone, so the guard is not a blanket refusal; a live pid whose cwd
  cannot be read is exit `1` (never a silent keep, never a removal), a **zombie**'s
  unreadable cwd is skipped, and the build-artefact reap (`rm -rf web/node_modules`)
  carries the same guard;
- a worktree whose HEAD cannot be resolved at all — the shape a lane worktree takes
  when its **branch is deleted**, i.e. a valid gitdir whose `HEAD` points at a ref
  that no longer exists — is the same "cannot judge this worktree" condition as an
  unreadable card: the run exits `1`, logs a `FAIL`, and never reports it as a keep.
  Measured before the fix: the read was a plain `git rev-parse HEAD`, which prints
  the literal string `HEAD` for that shape before failing, so the guard never fired
  and the verdict was taken by resolving `HEAD` **in the repo's context** — the same
  worktree reported `only on LOCAL branch …` when the repo's HEAD was an unpushed
  branch, and `has 1 uncommitted change(s)` when it was on main (an unborn HEAD makes
  `git status --porcelain` call every index entry an addition). Both kept it forever,
  named a reason about the repo, and exited `0`;
- `--dry-run` removes nothing.

Prove it bites with `JF_PRUNE_SCRIPT=<a copy with the old test restored>`. Doing
that here turned exactly the six mirror assertions red, including the one that
says the worktree survives.

## Prometheus `/metrics`

The API serves Prometheus metrics at `/metrics` (request counts by route and
status, among others). It is **not** exposed through Traefik — scrape it from
inside the docker network or over an ssh tunnel. Wiring it to a Prometheus server
is future work; the watchdog above is what makes an outage visible today.
