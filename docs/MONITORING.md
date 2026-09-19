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

Mutations that were run against it to prove it *bites* (each turned the suite
red): dropping the body-marker check → 3 failures; alerting on every run instead
of edge-triggered → 5 failures; treating `exit 2` as `exit 0` → 2 failures;
skipping `/readyz` on failure → 5 failures.

## Prometheus `/metrics`

The API serves Prometheus metrics at `/metrics` (request counts by route and
status, among others). It is **not** exposed through Traefik — scrape it from
inside the docker network or over an ssh tunnel. Wiring it to a Prometheus server
is future work; the watchdog above is what makes an outage visible today.
