# deploy/host/ — the prod host's own jobs

Scripts in this directory run **on the production host** (203.0.113.10), not in
an image and not in a container. They are installed to `/usr/local/bin` with a
root crontab line, and they exist to stop the host from accumulating state that
nothing else owns. Deploying a new image does not install, update or remove them —
the installer below does, and it is idempotent.

| File | What it is |
|---|---|
| `jomform-container-prune.sh` | the guard: reclaims **stopped** containers on the prod host |
| `install-host-container-prune.sh` | the installer: places it, arms the cron line, verifies the sha256 |
| `jomform-container-prune_test.sh` | the decoy suite — the per-merge gate `prunehost` |
| `jomform-container-prune_bite.sh` | the sabotage proof (on demand, not a gate) |
| `jomform-container-prune_leak_bite.sh` | the SIGKILL leak proof for the LIVE event window (on demand, not a gate) |

## Why the guard exists

Between 2026-09-19 and 2026-10-12 production accumulated **152 anonymous one-shot
`docker run` containers** — 149 `exited` + 3 `created` — on top of the six live
ones. They are deploy/QA inspection probes run over ssh: auto-named
(`adoring_beaver`), command like `sh -c 'find / -maxdepth …'`, never invoked with
`--rm`, so every one of them stayed in `docker ps -a` for ever. Card `t_2646fc00`
swept the pile by hand (158 → 6) and installed this guard so it cannot grow back.

**Why the Hermes `kanban-container-gc` plugin is not the answer.** That plugin
runs inside Kanban worker processes and talks to the **local** docker CLI
(`shutil.which("docker")`, no `DOCKER_HOST`, no ssh), and its matcher only covers
card-stack name shapes (`^(jf-|mrg|mg|qa)…-(pg|vk|minio)N$`). It covers the agent
host (vmi3187266); prod is a different host with a different leak class —
anonymous probes that belong to no card. The right mechanism on prod is
host-local and age-based, which is what this guard is.

## What it reclaims, and what it never touches

Reclaims: containers whose state is `exited`, `created` or `dead` **and** older
than the age floor **and** not spared by a keep rule.

Never touches:

* a `running`, `paused` or `restarting` container — the candidate list is
  re-read from `docker ps` immediately before the removal, so a container that
  started in the meantime is dropped from the batch;
* any container carrying a `com.docker.compose.project` label — the six live
  jomform/traefik containers are all compose-labelled;
* any **image** (`docker rm` cannot remove one) and any **named volume**
  (`docker rm -v` drops only the removed container's own anonymous volumes);
* anything younger than the age floor.

### The gates, in the order the guard applies them

1. **state** — `exited`/`created`/`dead` only;
2. **name** — `KEEP_NAMES` (the six live containers) plus every name in
   `/etc/jomform/keep-containers`, one per line, `#` comments allowed;
3. **age** — `CreatedAt` older than `JF_PRUNE_MIN_AGE_H` hours;
4. **compose** — no `com.docker.compose.project` label;
5. **re-check** — not in `docker ps` at removal time.

### Knobs

| Variable | Default | Meaning |
|---|---|---|
| `JF_PRUNE_MIN_AGE_H` | `168` (7 days) | age floor in hours; `0` disables the floor (a hand-run only) |
| `JF_PRUNE_DRY_RUN` | `0` | `1` lists what would go and removes nothing |
| `JF_PRUNE_KEEP_FILE` | `/etc/jomform/keep-containers` | the escape hatch (optional; absent is fine) |

### It fails closed

An unreachable docker daemon, a failed `docker ps`, an **empty container list**
(the stack is always up, so empty means something is wrong), or a private file for
`docker rm`'s output that cannot be created (see below) is logged and exits
**non-zero having removed nothing**. It never guesses. Success is verified by
counting containers before and after, never by `docker rm`'s exit status.

Exit codes: `0` = it ran (removed N ≥ 0, including "nothing to do"); `1` =
fail-closed or bad usage.

### Its `docker rm` output is a private file, never a fixed path

`docker rm`'s output goes to a file the guard creates for itself — `mktemp`, random
name, `O_EXCL`, mode 0600, under `$TMPDIR` — and deletes again before it exits.
Every line of a failing `docker rm` is logged as it is read, so deleting the file
costs no diagnostic.

It used to go to **`/tmp/jomform-container-prune.rm.out`**: a fixed name in a
world-writable directory (`/tmp` is mode 1777), opened by **root** *before*
`docker rm` ran, on every pass that reached the removal step. Any local user who
created that name first — a symlink to a file root can write — made root truncate
and replace the target, and two overlapping passes (a hand-run at 03:40 while cron
fires) shared one diagnostic file. Reproduced against the shipped bytes by card
`t_3d34df24` on a private `dockerd`: the victim's sha256 went
`89f2522a…` → `f5120dc7…`. The suite now plants exactly that symlink before its
real floor-0 pass and requires the target to come out byte-identical, and it fails
the pass if the guard cannot create its private file (nothing removed, exit 1).

## Installing it

```bash
# on the prod host
deploy/host/install-host-container-prune.sh

# from anywhere, over ssh/scp
deploy/host/install-host-container-prune.sh --host root@203.0.113.10

# print the plan and the exact cron line, write nothing
deploy/host/install-host-container-prune.sh --dry-run
```

It installs `/usr/local/bin/jomform-container-prune.sh` (0755 root:root), creates
the log, adds the cron line **only if that exact line is absent**, then verifies
the installed copy's sha256 against this repository's copy and that the crontab
holds the line exactly once. Two runs leave exactly one line and touch nothing
else in the crontab — `api/internal/biteharness` and the suite's install arm both
pin that.

The installed cron line is:

```
40 3 * * * /usr/local/bin/jomform-container-prune.sh >> /var/log/jomform-container-prune.log 2>&1
```

`--prefix` and `--log` exist so an install can be sandboxed (the suite uses them
with a fake `crontab`, `ssh` and `scp` on `PATH`); the default values are the
installed ones above, and `--dry-run` prints the production line verbatim.

## Running it by hand

```bash
# 1. what would it take, right now, without the age floor?
JF_PRUNE_MIN_AGE_H=0 JF_PRUNE_DRY_RUN=1 /usr/local/bin/jomform-container-prune.sh

# 2. what does the SCHEDULED run see (7-day floor)?
JF_PRUNE_DRY_RUN=1 /usr/local/bin/jomform-container-prune.sh

# 3. the real thing, and the log it leaves
/usr/local/bin/jomform-container-prune.sh; tail -20 /var/log/jomform-container-prune.log
```

Every line is timestamped and prefixed `jomform-container-prune:`. A normal pass
prints `inspected=N candidates=M (age floor 168h; skipped young=… keep=… compose=…)`
and then either `nothing to remove` or `removed=N containers (before -> after)`.

**Rollback** is two commands: remove the cron line (`crontab -l | grep -v
jomform-container-prune | crontab -`) and delete `/usr/local/bin/jomform-container-prune.sh`.
It removes no image and no volume, so nothing it did needs undoing.

## Explicitly OUT of scope: volumes

The guard does **not** reclaim volumes, and that is deliberate: `docker volume rm`
can destroy real data, and a volume that no container references is not proof that
its contents are disposable. Production still holds **5 orphaned anonymous
volumes** (4 × 50 MB created 2026-09-19, 1 × 16 KB created 2026-09-01, measured
2026-10-12 by card `t_2646fc00`), unreferenced by any container, left alone. They
need a per-volume human-eyed check, not a cron job. Until that happens, do not
assume this guard covers them.

## Testing

### The decoy suite — the `prunehost` gate

```bash
bash deploy/host/jomform-container-prune_test.sh      # ~60-90s, 135 checks
```

It runs the guard against a **private Docker daemon of its own** — a second
`dockerd` on a scratch data-root and unix socket under `$TMPDIR`, started and
torn down by the suite — and plants decoy containers in it:

| Decoy | Exists for |
|---|---|
| `jf-prune-decoy-young` | the AGE gate: stopped, unlabelled, minutes old → not a candidate at 168h, a candidate at floor 0 |
| `jf-prune-decoy-plain` | the guard BITING: the sole candidate at floor 0, and it is removed |
| `jf-prune-decoy-compose` | the COMPOSE gate: stopped, labelled `com.docker.compose.project=jomform` → `keep (compose project=…)` |
| `jf-prune-decoy-keepfile` | the KEEP gate: stopped, named in the keep file → `keep (name)` |
| `jf-prune-decoy-running` | the STATE gate: never listed, and it survives a real floor-0 run |
| `jf-prune-decoy-nomkt` | the refusal arm: a stopped container created **after** the real run, which a floor-0 pass that cannot create its private rm-output file must NOT remove |
| `jf-prune-decoy-leak` | the teardown check BITING: running, created **after** the real run and left running on purpose, so the liveness predicate below must report it — and must report it gone once its container is |

plus the arms that are not about a decoy: the fail-closed one (an empty container
list must exit non-zero), the **private rm-output file** pair (a `mktemp` that
always fails must make the pass exit non-zero having removed NOTHING, and the
symlink planted at the guard's old fixed `/tmp` path must leave its target
byte-identical after the real pass — with the file the guard DID open measured
through a delegating `mktemp` wrapper: a private path under the run's own `TMPDIR`,
regular, 0600, root-owned, and deleted before the guard exits), the installer arms,
and — at the end — the assertions that the suite tore its own
sandbox down (scratch tree gone, no private `dockerd`, no decoy process, no
mount), plus the two arms below that keep the decoy-process one honest.

**"No decoy process was left running" is keyed on THIS RUN'S CONTAINERS, never on a
host-wide `pgrep -f "sleep N"`** (card `t_d088f152`). The decoy lifetime is a number
drawn from 120..239, and a host-wide `pgrep -f` for it also matches every other
lane's `sleep`, cron poll loop and certbot on this shared machine: measured
2026-10-31, `FAIL no decoy process was left running (want 'gone', got 'alive')` —
1 of 104 checks — on the merge tree for PR #1502, with the **identical bytes**
`104/104` green on a re-run. A check that reads the whole host cannot tell "my
decoy leaked" from "somebody else is sleeping", so on a busy host it was a coin
flip that also stopped the runner (costing the gates after it). The predicate now
reads `/proc/<pid>/cgroup` and matches **this run's container ids**
(`…/docker-<id>.scope`): measured on the agent host, that marker survives the leak
the suite is about — a `kill -9` of the private `dockerd` leaves the decoy's
`sleep` running and still inside that cgroup — and it cannot see any process the
suite did not create. It also removes the teardown RACE: the suite waits (bounded)
for the process it recorded instead of sampling once and calling a healthy
teardown a leak. Two arms keep it honest on **every** run: a decoy left running on
purpose (`jf-prune-decoy-leak`) must be reported and **named**, and an unrelated
`sleep` for the *same* number of seconds must leave the check green — with the
pre-fix `pgrep -f` kept as the control that reds on it.

**The teardown wait covers ALL of this run's decoys, and it NAMES what survived**
(card `t_d243986f`). The processes a private `dockerd` owns exit asynchronously: a
`kill` of the daemon can return while a container's `sleep` is still inside its cgroup
and still running. Measured 2026-11-04 with a peer lane churning containers on the host
daemon: this arm and the next one each sampled one live decoy a few seconds after
`priv_stop` returned — `RESULT: 2 of 131 checks FAILED` — on bytes that were green on a
re-run, because 5s was not always enough on a loaded host. The wait is now bounded at
40s over every decoy container this run recorded, and the failure message prints the
surviving pids and their command lines (`want '0', got '1'` was the undiagnosable red),
while the arm above still proves the instrument names a real leak.

**The host daemon is shared, so the host check is ATTRIBUTED, not counted.** The
suite reads the host's container and image id sets before the run and compares
them at the end — but a bare "nothing that existed is gone" comparison cannot
survive a machine where every lane and every compose service also uses that
daemon. Measured on card `t_df1719c1`'s full gate run: a peer re-created a
container inside the suite's own window and the gate reported

```
FAIL no host container that existed before this run is gone (want '0', got '1')
```

— a bare count naming nothing, on a tree with nothing wrong, and the runner stops
at the first failure so the rest of that run's inventory is lost. So each loss is
now **attributed** from the host daemon's own `docker events` window plus this
run's own removal log:

| Verdict | Meaning |
|---|---|
| `OURS` | this run's audited removal path recorded the id — it only ever accepts a container this run created, so this means the suite really did touch the host, and it FAILS |
| `FOREIGN` | a destroy/delete event this run did not issue — **named** with the container's name and the second it happened, and **not** a failure of this tree |
| `FOREIGN-NOEVENT` | the loss is not this run's — its only host-removal path refuses an id it did not create, which a self-check re-measures on every run — but the daemon no longer holds a destroy for it. RECORDED and PASSES with that reason and the window's own counts, instead of redding a correct tree (card `t_d243986f`) |

**That window has TWO SOURCES, because `docker events --since` is not a history.** The
daemon keeps its event record in a bounded in-memory ring of **256 events** (measured on
the agent host: `--since` 300s, 600s, 1800s and 7200s every one answers exactly 256
lines), so a query is "whatever of the last 256 is still there", not "everything since
then". A quiet host emits ~2.5 events/s from health checks alone, and a **concurrent
lane**'s docker work (any build, any e2e/a11y/media gate) pushes the probe's destroy out
of the ring before the end-of-run replay asks for it: the merge train for PR #1535
printed `2 of 112 checks FAILED` — `… its removal CANNOT be attributed` and `the probe
container the PEER removed mid-run is attributed FOREIGN and NAMED (want '1', got '0')`
— on a correct tree, and since a red record cannot be reused each retry re-ran the whole
inventory (~48 min). So the suite now opens a **LIVE `docker events` subscriber** before
the before-snapshot (every event is on disk as it is published, so nothing can be
evicted from under it), keeps the end-of-run `--since/--until` **replay as the
fallback** for a subscriber that died or a daemon that restarted, and merges the two
into the log the verdicts read. On every run it asserts that the LIVE half captured the
peer probe's destroy, that the half is not empty, and that the subscriber is gone once
its window closed; and it drives the eviction shape (destroy held only by the LIVE
source, replay empty) and the no-event shape through the verdict function itself —
including the control that the SAME empty window still FAILS for an id this run's own
audited path removed.

The suite reaches the host daemon through exactly one raw `docker` call site
behind a read-only door (which refuses every destructive verb) plus three
target-checked write helpers, and a self-check fails the run if a second raw call
site appears — that is what makes "this run issued no other removal" a property
of the file rather than a hope. It is exercised on **every** run: the suite plants
two uniquely-named probe containers on the host daemon (created, never started,
from an empty-rootfs image imported here — no pull, no network, no host image) and
a **peer process** (`bash -c` calling the docker CLI directly, the shape of the
redeploy that caused the red) removes one of them mid-run. That one must come back
`FOREIGN` and named, with the control probe still present, or the suite goes red.
Both probes and the probe image are removed in the teardown and asserted gone, so
the host is left exactly as it was found. The teardown is bound to EXIT/INT/TERM/HUP
and removes by **name** as well as by id — a signal can land between `docker create`
returning and the id being recorded, and the trap-installation arm is what caught
exactly that leak when it happened.

A run that is **killed** (`SIGKILL` is the gate runner's final signal on a timeout)
cannot be trapped at all, so the next run **sweeps before it takes its snapshot**: a
`jf-prune-{peer,control}probe-<pid>` or `jf-gate-concurrency-peer-<pid>-<n>-<n>`
container whose embedded owner pid is not a live process, and which is more than two
minutes old, is debris from a run that is gone, and is removed before it can be read as
"existed before this run" by every run after it; one whose owner **is** alive — or one
young enough that a concurrent run may still have planted it — is never touched. Without
that, a single interrupted run leaves probes that red the gate on *every* tree until
someone cleans the daemon by hand — measured 2026-11-04: 12 such probes made a
`deploy/*.sh`-only delta and pristine `origin/main` red the same 5 checks, and
`docker rm -f` of exactly those 12 made the same bytes green.
The sweep is exercised on every run with two baits named after a pid that cannot be
allocated (both must go) and a control probe owned by this run's own pid (must be
spared and still present), and it asserts that nothing outside the two probe shapes was
removed.

This is why the suite is safe to run on a busy host: the guard under test cannot
see the host's containers at all, the one destructive pass it runs happens in
a daemon that holds nothing but the five decoys — **and that daemon runs in a
network namespace of the suite's own** (`unshare -n`), because `--bridge=none`
does not merely skip creating a bridge: it makes moby **delete** the default
bridge of whichever network namespace the daemon is started in
(`daemon/daemon_unix.go`, `removeDefaultBridgeInterface()`, the `--bridge=none`
case). In the host's namespace that is the host's `docker0`, and every container
on the default bridge is then unreachable from the host — the shared test valkey
among them, which is how card `t_a10c01dd` (the root cause of `t_eb901784`) redded
other lanes' gates on trees that were correct. The suite therefore plants a
`docker0` **decoy** in its own namespace and requires the daemon to delete that
one — so the survival of the caller's bridge is attributable to the namespace and
not to a docker that stopped doing it — and it reads the caller's `docker0`
through a descriptor inherited across the re-exec, requiring the **same ifindex
and MAC** before and after. The namespace arm is checked before anything
destructive exists and fails closed: a run that is in the caller's namespace
refuses to start a daemon at all.

Requirements: **root** (a private `dockerd` cannot run unprivileged), the
`dockerd` binary, `busybox` (a 1 MB throwaway image, imported — nothing is pulled
and no host image is borrowed), `unshare`/`nsenter` (util-linux), and a reachable
host docker daemon. It fails loudly without them; it never skips.

### The bite proof (on demand; each case is a whole suite run)

```bash
bash deploy/host/jomform-container-prune_bite.sh      # 14 sabotages, one behaviour each
```

It copies the guard, the installer and the suite into a throwaway directory,
removes ONE gate or the installer's idempotency check in the copy, and requires
the suite to go RED **on the assertion that names that gate** — baseline green
first, every mutation verified to have landed and to still parse, and the three
committed files' sha256 re-checked at the end. A red for an unrelated reason is
not credited, and a green is printed as `DID NOT BITE`. Two of them are
aimed at the suite's own HOST-DAEMON attribution rather than at the guard: the
destroy-event lookup disabled, so the peer's removal becomes unattributable and
the suite must red NAMING the container, and this run's own removal ledger
disabled, so the classifier can never say `OURS` and its own self-test reds. The
last one is the LIVE window's lock discipline: the closure of the inherited
descriptors is removed, so the subscriber keeps this run's lock and the arm
asserting it holds no such descriptor must red.

### The SIGKILL leak proof (on demand, ~1 minute)

```bash
bash deploy/host/jomform-container-prune_leak_bite.sh
```

The suite's teardown closes the LIVE `docker events` subscriber, but `SIGKILL` —
the signal a gate run actually dies of — never reaches a trap. Measured
2026-11-05: a subscriber outlived its run by 40 minutes and held both
`/var/lock/jomform-merge.lock` and a preflight slot through descriptors it had
inherited, because `flock` is open-file-description scoped. This harness takes
the merge lock at fd 200 (the merge lane's own shape), starts the suite, waits for
its LIVE window, `SIGKILL`s it, and requires within a bounded time that no
`docker … events` process from that run survives **and** that the lock is free
once the harness drops its own descriptor. It runs the same shape against a
control copy carrying the pre-fix subscriber and requires it to LEAK — a green on
the fixed suite alone would be a tautology. It cleans up every probe, image and
scratch tree it planted; the merge lock is a precondition (it refuses while
another lane is merging).

It is deliberately **not** a per-merge gate (each case costs a whole suite run),
which is also why its row in `BITE-HARNESSES.md` says so; the register is checked
by `api/internal/biteharness`, so a `*bite*` file with no row fails the `api test`
gate.

## The installed copy on production

Measured 2026-10-12 (card `t_2646fc00`), re-measured 2026-10-12 by card
`t_3d34df24`:

```
/usr/local/bin/jomform-container-prune.sh
sha256 e0b52efd587778bdd6f99660a7e655e2446a3e2ca7ede5f44f976f97fa5135df
root crontab: 40 3 * * * /usr/local/bin/jomform-container-prune.sh >> /var/log/jomform-container-prune.log 2>&1
/var/log/jomform-container-prune.log  (0644, empty until the first pass)
```

**That is the PRE-FIX copy, and production still runs it.** Card `t_3d34df24`
replaced the guard's fixed `/tmp/jomform-container-prune.rm.out` redirect with a
private per-run `mktemp` file, so the repository copy is now

```
sha256 8634f8da790cf6b23b8049e0e7dae22b60e44eca115a869d8e3bba74ca591524
```

and the host keeps the old bytes until the installer runs again:

```bash
deploy/host/install-host-container-prune.sh --host root@203.0.113.10
```

Its verification step prints the new sha and refuses if the installed copy does not
match the repository's; the cron line is left alone because that exact line is
already present. Until that command runs, the fix is in the repo and not on the
host — say so, do not assume it.

The sha is a **measurement, not a contract** — a future fix to the guard changes it,
and the installer then reports the difference rather than silently replacing it
with something else:

```bash
ssh root@203.0.113.10 sha256sum /usr/local/bin/jomform-container-prune.sh
sha256sum deploy/host/jomform-container-prune.sh
```

Related: `deploy/HOST-SETUP.md` (the fresh-host runbook — its bootstrap installs
this guard's **cron line** and nothing else, so a rebuilt host needs step 2 of that
procedure, which is this directory's installer), `deploy/DEPLOY.md` ("Cleanup is
part of the probe, not a follow-up"), `deploy/backup/README.md` (the other
host-side job, and the pattern this directory follows), `BITE-HARNESSES.md` (the
bite-harness register).
