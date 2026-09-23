# Production monitoring

Two things watch JomForm production: a **one-file uptime + disk watchdog** that
runs on this host and alerts into Telegram, and the API's own Prometheus
`/metrics` endpoint. There is deliberately no monitoring stack — no Prometheus
server, no Grafana, no agent. The goal is not dashboards; it is not being blind.

## What the watchdog checks

`deploy/monitoring/jomform-watch.sh` runs every 5 minutes and checks four things.
Each one can fail on its own and each is named in the alert.

| Check | What it proves | Why it is here |
|---|---|---|
| `web` | `GET https://jomform.com/` returns 200 **and** contains `/_next/static/` | Cloudflare → Traefik → web container — the path a real visitor takes |
| `health` | `GET https://jomform.com/healthz` returns 200 and contains `service` | Cloudflare → Traefik → api container |
| `ready` | the api container's own `/readyz` reports `database: ok` **and** `valkey: ok`, read over ssh | `/healthz` answers 200 while the database is down; without this, login and payments could be broken and the watchdog silent |
| `disk` | `df` on the prod host stays under the threshold | prod had no disk alarm at all |

Two of those choices are load-bearing and worth stating, because the obvious
shorter version of this script does not catch them:

- **The web check uses the page, not `/healthz`.** A dead web container leaves
  `/healthz` answering 200 (it is routed straight to the API), so an `/healthz`-only
  watcher would report healthy while every visitor got an error page.
- **`/readyz` is checked over ssh.** It is not exposed through Traefik (an
  unauthenticated dependency readout does not belong on the public edge), and
  `docker compose exec -T` **reads stdin** — without `< /dev/null` the compose
  client swallows the ssh channel and the probe hangs.

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
  heartbeat: checks run, failing runs, alerts sent, prod disk. If the digest
  stops arriving, the *watcher* is dead — which is otherwise indistinguishable
  from "everything is fine".
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
- **no notifier is exit 2, never `ok`**, and an undeliverable alert is exit 3.

The board's worktree pruner is the other host-only safety script in this
directory's family; its behaviour and self-test are documented under
[Kanban worktree pruner](#kanban-worktree-pruner--what-it-may-and-may-not-delete).

Mutations that were run against it to prove it *bites* (each turned the suite
red): dropping the body-marker check → 3 failures; alerting on every run instead
of edge-triggered → 5 failures; treating `exit 2` as `exit 0` → 2 failures;
skipping `/readyz` on failure → 5 failures.

## Kanban worktree pruner — what it may and may not delete

`deploy/worktrees/prune-kanban-worktrees.sh` is the host-side reclaimer for the
agent board's worktrees. Each card worktree carries its own `web/node_modules`
(442M) + `.next` (146M), so unfinished reclamation is ~9G/day, and nothing else
reclaims them (`hermes kanban gc` prunes events and logs, not worktrees). It runs
from the Hermes cron job `prune-kanban-worktrees` (daily `0 4 * * *`, a
`no_agent` job, so it is a script run and not an agent turn).

Before removing anything it requires **all** of: not the main checkout; a
terminal card status; a completion older than `--min-age-hours` (24h); no live
process referencing the worktree or the card; a clean tree; and — the one this
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

Exit codes: `0` nothing to do, `3` work exists only locally (i.e. the run kept
something on purpose — a cron wrapper can alert on it), `2` bad arguments.

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
  still running, an unreadable card, and a worktree with a live worker process are
  each kept, each with its own reason in the log;
- the stray-worktree loop (outside `.worktrees`, no card reference) gets the same
  correction;
- `--dry-run` removes nothing.

Prove it bites with `JF_PRUNE_SCRIPT=<a copy with the old test restored>`. Doing
that here turned exactly the six mirror assertions red, including the one that
says the worktree survives.

## Prometheus `/metrics`

The API serves Prometheus metrics at `/metrics` (request counts by route and
status, among others). It is **not** exposed through Traefik — scrape it from
inside the docker network or over an ssh tunnel. Wiring it to a Prometheus server
is future work; the watchdog above is what makes an outage visible today.
