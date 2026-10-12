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

An unreachable docker daemon, a failed `docker ps`, or an **empty container
list** (the stack is always up, so empty means something is wrong) is logged and
exits **non-zero having removed nothing**. It never guesses. Success is verified
by counting containers before and after, never by `docker rm`'s exit status.

Exit codes: `0` = it ran (removed N ≥ 0, including "nothing to do"); `1` =
fail-closed or bad usage.

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
bash deploy/host/jomform-container-prune_test.sh      # ~25s, 65 checks
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

plus the fail-closed arm (an empty container list must exit non-zero), the
installer arms, and — at the end — four assertions that the suite tore its own
sandbox down (scratch tree gone, no private `dockerd`, no decoy process, no
mount). The host daemon is read **only** to prove nothing outside the sandbox was
removed: the host's container and image id sets are snapshotted first and
compared last (`before ⊆ after`), and no decoy is ever visible on it.

This is why the suite is safe to run on a busy host: the guard under test cannot
see the host's containers at all, and the one destructive pass it runs happens in
a daemon that holds nothing but the five decoys.

Requirements: **root** (a private `dockerd` cannot run unprivileged), the
`dockerd` binary, `busybox` (a 1 MB throwaway image, imported — nothing is pulled
and no host image is borrowed), and a reachable host docker daemon. It fails
loudly without them; it never skips.

### The bite proof (on demand, ~3 minutes)

```bash
bash deploy/host/jomform-container-prune_bite.sh      # 6 sabotages, one per gate
```

It copies the guard, the installer and the suite into a throwaway directory,
removes ONE gate or the installer's idempotency check in the copy, and requires
the suite to go RED **on the assertion that names that gate** — baseline green
first, every mutation verified to have landed and to still parse, and the three
committed files' sha256 re-checked at the end. A red for an unrelated reason is
not credited, and a green is printed as `DID NOT BITE`.

It is deliberately **not** a per-merge gate (each case costs a whole suite run),
which is also why its row in `BITE-HARNESSES.md` says so; the register is checked
by `api/internal/biteharness`, so a `*bite*` file with no row fails the `api test`
gate.

## The installed copy on production

Measured 2026-10-12 (card `t_2646fc00`):

```
/usr/local/bin/jomform-container-prune.sh
sha256 e0b52efd587778bdd6f99660a7e655e2446a3e2ca7ede5f44f976f97fa5135df
root crontab: 40 3 * * * /usr/local/bin/jomform-container-prune.sh >> /var/log/jomform-container-prune.log 2>&1
/var/log/jomform-container-prune.log  (0644, empty until the first pass)
```

The repository copy is byte-identical to it, and the installer verifies the
sha256 on every install. That sha is a **measurement, not a contract** — a future
fix to the guard changes it, and the installer then reports the difference rather
than silently replacing it with something else:

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
