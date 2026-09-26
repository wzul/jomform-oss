# Automated Postgres + media backups to Google Drive

The production database is dumped nightly, gzipped, uploaded to Google Drive and
verified by re-reading the uploaded object. A restore drill is a first-class mode
because **a backup that has never been restored is not a backup**.

This directory is the source of truth for the script that runs on the host. The
script is copied to `/usr/local/bin/jomform-backup.sh` and driven by a root cron
entry; it does **not** run out of the checkout.

## What is backed up

| Artifact | Path on the host | Uploaded to |
|---|---|---|
| Postgres dump (custom format, `pg_dump -Fc`, gzipped) | `/var/backups/jomform/jomform-daily-<UTC date>.dump.gz` | `gdrive:jomform-backups/daily/` |
| Checksum of the dump | same path + `.sha256` | `gdrive:jomform-backups/daily/` |
| MinIO media volume (`/data`, tar+gzip) | `/var/backups/jomform/jomform-daily-<UTC date>-media.tar.gz` | `gdrive:jomform-backups/daily/` |

Retention: **30 daily** and **12 monthly**. The monthly copy is promoted on the
1st of each month by a server-side `rclone copyto` (no re-upload) into
`gdrive:jomform-backups/monthly/`. Local staging keeps the most recent 3 days of
dumps; Drive is the durable copy.

The monthly pair is `jomform-monthly-<YYYY-MM>.dump.gz` plus a
`.dump.gz.sha256` sidecar whose **content names the monthly object**:

```
<digest>  jomform-monthly-<YYYY-MM>.dump.gz
```

The sidecar is written at promotion time from the daily sidecar's digest, not
copied from the daily object. Copying the file verbatim leaves a sidecar that
names the **daily** path — `sha256sum -c` then fails on a perfectly good monthly
dump, and the path it names stops existing as soon as local retention prunes the
daily file. The digest is identical either way (a server-side copy does not
change the bytes); only the name differs, and the name is a bare basename so it
resolves beside the object wherever the pair is downloaded to.

Verify a monthly copy by downloading the pair and checking it:

```bash
rclone copy gdrive:jomform-backups/monthly/jomform-monthly-<YYYY-MM>.dump.gz* /tmp/
cd /tmp && sha256sum -c jomform-monthly-<YYYY-MM>.dump.gz.sha256
```

### Is the media volume covered?

**Yes, deliberately.** The MinIO volume holds customer-uploaded media. A
database-only restore would leave `media` rows pointing at objects that no longer
exist, so the nightly run archives `/data` as well. It is small — a few hundred
KB today — so the cost of including it is negligible next to the value of a
restore that actually renders the customer's pages. If a future media volume
grows past ~1 GB, revisit this decision and record it here rather than silently
dropping it from the archive.

## The rclone remote

The remote and its OAuth token live **outside this repository**, at
`/root/.config/rclone/rclone.conf` with mode `600`. This repository may become
public and `gitleaks` scans the full git history, so the token must never be
committed, copied into a worktree, or pasted into a PR.

Configure it with the **`drive.file` scope, never the default full `drive`
scope**:

```bash
rclone config create gdrive drive scope drive.file
```

`drive.file` grants access **only to files this application itself creates**. It
cannot read, modify or delete anything else in the account. Without it, anyone
who obtains root on the production host gains write access to the entire Drive
rather than to one backup folder. The trade-off, stated honestly: rclone can no
longer see files it did not create, so seeding the destination by hand, or
restoring from a dump somebody uploaded manually, is not possible — for an
automated backup destination that is the correct trade.

The OAuth handshake needs a human: it opens a browser and asks for the account's
consent. It cannot be completed by an agent. When the token expires the job
raises an alert and the fix is one command:

```bash
rclone config reconnect gdrive:
```

Verify the scope took effect rather than assuming it:

```bash
grep '^scope' /root/.config/rclone/rclone.conf      # must print: scope = drive.file
rclone lsd gdrive:                                  # lists only what this job created
```

With `drive.file`, `rclone lsd gdrive:` listing only the `jomform-backups`
folder is the expected and correct output. Other folders in the account are
invisible to this remote by design — that is the scope working, not a fault.

## Install on the host

```bash
install -m 0755 deploy/backup/jomform-backup.sh /usr/local/bin/jomform-backup.sh
install -d -m 0700 /var/backups/jomform /var/lib/jomform-backup
```

Then add the cron entry (this is the only scheduled job for this purpose):

```cron
# JomForm nightly backup -> Google Drive. Logs to /var/log/jomform-backup.log.
17 2 * * * /usr/local/bin/jomform-backup.sh >> /var/log/jomform-backup.log 2>&1
```

Nightly at 02:17 UTC, off the top of the hour so it is not competing with other
scheduled work on the host.

The job appends to `/var/log/jomform-backup.log` on its own, so the log must
exist and be rotatable. Create it and install the rotation config:

```bash
install -m 0640 -o root -g adm /dev/null /var/log/jomform-backup.log
cat >/etc/logrotate.d/jomform-backup <<'EOF'
/var/log/jomform-backup.log {
    su root adm
    daily
    rotate 14
    compress
    missingok
    notifempty
    copytruncate
}
EOF
```

The `su root adm` line is required, not cosmetic: `/var/log` is group-writable,
and without it `logrotate` refuses the file with *"parent directory has insecure
permissions"* and skips it silently — the log would grow unbounded.

## Running it by hand

```bash
/usr/local/bin/jomform-backup.sh               # full run: dump, media, upload, verify, retention
/usr/local/bin/jomform-backup.sh --no-media    # skip the media archive
/usr/local/bin/jomform-backup.sh --local-only  # dump + verify locally, do not upload
```

### A hand-run must not reach production — the guard refuses the shape, it does not contain the run

Read this distinction before the rule, because the rule reads like more than it is:

**The fail-closed guard is a refusal rule, not containment.** It answers exactly one
question — *is this the cron job, or a hand-run?* — and refuses a hand-run that left out a leg
it would otherwise have reached production through. It never moves a path for you. Every
file a run writes goes to the knobs **that run names**, and every knob it does not name keeps
the value installed on the host. A hand-run is contained by the environment block under
"A hand-run therefore looks like this" — by naming every path and credential knob — and by
nothing else. `--local-only` is not containment either: it skips the upload, and the dump,
the lock and the status file still land wherever the run's knobs point.

The script is installed **outside** the checkout and reads its credentials from
`/opt/jomform/.env` (real SES keys, real `SUPER_ADMIN_EMAILS`) and its
destination from `JFBACKUP_REMOTE_ROOT` (the production Drive folder). An ad-hoc
run that sets some knobs and forgets others therefore silently reaches production
credentials and production storage. That is not hypothetical: on 2026-09-19 two
hand-runs set `JFBACKUP_STAGE_DIR`/`_STATE_DIR`/`_LOG_FILE`, forgot
`JFBACKUP_ENV_FILE` and `JFBACKUP_REMOTE_ROOT`, mailed the real super-admin
inbox over SES and wrote into the production backup folder.

So the script now **fails closed on a partial override**:

- If any of `JFBACKUP_STAGE_DIR`, `JFBACKUP_STATE_DIR`, `JFBACKUP_LOG_FILE`,
  `JFBACKUP_LOCK_FILE`, `JFBACKUP_PATH_PREFIX`, `RCLONE_CONFIG`, `JFBACKUP_REMOTE`
  or `JFBACKUP_REMOTE_ROOT` moves off its installed value, the run is a hand-run,
  not the cron job.
- The two **destination** knobs matter for the mirror-image reason the local ones
  do. The installed job never sets `JFBACKUP_REMOTE`/`JFBACKUP_REMOTE_ROOT`, so a
  run that sets either is a hand-run, and a run that moves **only** the
  destination has contained only its drive half: the credential file its alert
  email is sent through is chosen by `JFBACKUP_ENV_FILE`, which that run never
  names and which therefore still resolves to `/opt/jomform/.env`. That shape —
  "let me try it into a scratch folder first" — is why the destination knobs are
  triggers rather than something the guard ignores.
- `JFBACKUP_ENV_FILE` is deliberately **not** a trigger: it names the file to
  *read* credentials from, not where a half of the run goes, so a run that points
  it at a scratch file has already contained its credential half. The guard exists
  to refuse a run that reaches production by *omission*, which is the shape that
  failed on 2026-09-19.
- On such a run, `JFBACKUP_ENV_FILE`, `JFBACKUP_REMOTE` and
  `JFBACKUP_REMOTE_ROOT` must be **explicitly set** (any value — a scratch name
  is fine). The test is "was it provided", not "does it differ from the default".
- If one is missing the run prints one line naming it and exits non-zero
  **before any work**: no dump, no archive, no upload, no lock file, and
  deliberately **no alert email** — the alert is half of what leaked. It is not
  "reached nothing" though: `fail()` writes the FAILED status into the run's own
  `STATE_DIR` before the exit, so a refusal against the installed script still
  touches production state. That is the `JFBACKUP_STATE_DIR` point below.
- For a deliberate real run, set `JFBACKUP_ALLOW_UNSAFE=1`; the run logs a WARN
  naming the credential file and the remote it is about to reach, and records it
  in syslog. It is not the default, and **only the value `1` opens it**: the
  hatch is keyed on the value, not on the variable being set, so
  `JFBACKUP_ALLOW_UNSAFE=0` — the value a caller writes to mean *no* — is **not**
  a request for a deliberate real run and leaves the refusal in place. `false`,
  an empty value (`export JFBACKUP_ALLOW_UNSAFE=`) and any other string behave the
  same way. A run that sets the variable to a non-empty value other than `1`
  logs a WARN saying why the hatch stayed shut; an empty value is the one shape
  refused without that WARN, because the note is itself guarded on the variable
  being non-empty — for `export JFBACKUP_ALLOW_UNSAFE=` the refusal is the signal
  rather than a log line saying so.

A hand-run therefore looks like this:

```bash
JFBACKUP_STAGE_DIR=/tmp/jfbtest/stage \
JFBACKUP_STATE_DIR=/tmp/jfbtest/state \
JFBACKUP_LOG_FILE=/tmp/jfbtest/run.log \
JFBACKUP_LOCK_FILE=/tmp/jfbtest/lock \
JFBACKUP_ENV_FILE=/tmp/jfbtest/env \
JFBACKUP_REMOTE=googldrive \
JFBACKUP_REMOTE_ROOT=jfbtest-scratch \
  /usr/local/bin/jomform-backup.sh --local-only
```

Every line in that block is load-bearing, and it is not the guard that makes it so.

- **`JFBACKUP_STATE_DIR` in particular must be named, or the run rewrites the job's own health
  record.** The status file is `$STATE_DIR/last-run.json` (`last-failure.json` on failure, plus
  `last-success.epoch` on success), and *a refused run writes it too*: the refusal path is
  `fail()` then `write_status()`, which runs before the exit. So both directions reach the state
  dir: a hand-run that omits only this knob overwrites the nightly job's `OK` record with its
  own, and a refusal overwrites it with a `FAILED` record, which is the exact reading an
  operator uses to answer "is the nightly backup healthy?". Two of these runs have already
  happened on production and each needed hand-remediation. If you are only poking at the
  script, move `JFBACKUP_STATE_DIR` (and `JFBACKUP_STAGE_DIR`) to a scratch directory even if
  you expect the run to be refused.
- `JFBACKUP_STAGE_DIR` is the mirror image for the artifacts: the dump, its `.sha256` sidecar
  and the media archive are written under it, and a run that leaves it at its installed value
  stages real dumps into the production staging directory.
- `JFBACKUP_LOCK_FILE` is listed although it is not in the guard's must-be-provided set: it is
  a knob whose move is not *required* for containment, but a run that leaves it installed takes
  the nightly job's own lock (`flock`) and can make a concurrent cron run exit early.

The same rule binds the committed test suite, which is the point, and it is the reason the
suite is written the way it is: every case in `jomform-backup_test.sh` goes through a helper
that names **all** of the containment knobs — the same discipline the block above asks of a
human — so no case can accidentally reach the installed paths. The helper's own comment says
so at its definition, and a final case asserts that the helper (and the recipe above) still
name every one of those knobs: a helper that quietly stopped naming one would leave the suite
writing into the very paths it exists to stay out of.

What the guard *does* enforce is narrower, and it is stated above: a run that moved any of
those knobs must **explicitly provide** `JFBACKUP_ENV_FILE`, `JFBACKUP_REMOTE` and
`JFBACKUP_REMOTE_ROOT`, so that a run cannot reach the production credentials or the production
Drive folder **by omission**. That is a test on what the caller provided, not a check that the
run is contained, and it is deliberately not a licence to run against the installed script
without the block above.

## Restore drill — run this, do not just trust the dump

Restores a dump into a throwaway `postgres:18-alpine` container, prints the row
counts it actually contains, and removes the container:

```bash
/usr/local/bin/jomform-backup.sh --restore /var/backups/jomform/jomform-daily-<date>.dump.gz
```

Compare the printed counts against production:

```bash
docker compose -f /opt/jomform/compose.yml exec -T postgres \
  psql -U jomform -d jomform -Atc \
  "select 'users',count(*) from users union all select 'accounts',count(*) from accounts
   union all select 'forms',count(*) from forms union all select 'sales',count(*) from sales
   union all select 'submissions',count(*) from submissions" < /dev/null
```

To restore for real, `pg_restore --no-owner` into the target database, then
extract the media archive back into the MinIO `/data` volume:

```bash
tar -C /var/lib/docker/volumes/jomform_miniodata/_data -xzf jomform-daily-<date>-media.tar.gz
```

## How failure is made visible

A silent backup failure is the defect this exists to close, so a failing run is
loud in four independent places:

1. **Exit code** — non-zero on any failure (so a cron `MAILTO` also fires).
2. **Log** — `/var/log/jomform-backup.log`, with an `ERROR` line naming the step.
3. **syslog** — `logger -t jomform-backup` so the alert also lands in journald.
4. **Email** — an alert to `SUPER_ADMIN_EMAILS`, sent directly over the SESv2 API
   with the credentials in `/opt/jomform/.env`. It is deliberately *not* sent
   through the application's own queue: if the database is why the backup failed,
   the application's mail path is down with it.

**"Exited 0" is not "uploaded".** After every upload the remote object is
re-read and its size and hash compared against the local file. An upload that
returns success but leaves nothing in Drive fails the run.

The last run's outcome is also written to `/var/lib/jomform-backup/last-run.json`
(`/var/lib/jomform-backup/last-failure.json` on failure) so the state can be
checked without reading the log. Check the freshness of the last success with:

```bash
cat /var/lib/jomform-backup/last-run.json
```

A `--restore` drill deliberately does **not** touch those files: it records itself
in `/var/lib/jomform-backup/last-restore-drill.json` instead. If a drill wrote
`last-run.json`, an operator checking "is the nightly backup healthy?" would read
the drill's `OK` and never learn that the backups had stopped succeeding.

On the first failed run after a previously successful one, the alert email is
sent; a failing run does not retry in a tight loop because the lock file
(`/var/lock/jomform-backup.lock`) makes a concurrent run exit immediately.

## Tested failure paths

Every one of these is exercised by `jomform-backup_test.sh` (209 assertions,
stubbed `rclone`/`docker`, no network and no database required) and every one of
them is additionally proven to *bite* by sabotaging the script and requiring the
suite to go red:

```bash
bash deploy/backup/jomform-backup_test.sh
```

A test that only ever passes proves nothing, so every sabotage named below is
actually applied — by `deploy/backup/jomform-backup_bite.sh`, which mutates one
thing at a time in a throwaway copy of this directory and requires the suite to
go red on an assertion that sabotage owns. A mutation that does not land, or one
that leaves the script unparseable, is reported as an **abort** rather than
counted as caught, so the harness cannot inflate its own coverage:

```bash
bash deploy/backup/jomform-backup_bite.sh
```

It applies **twenty-nine** sabotages in four groups. Thirteen target the script's
failure handling: removed hash verification, removed post-upload verification,
the `FAILED` line downgraded to `WARN`, removed empty-archive guard, unescaped
status JSON, ignored restore failure, upload failure returning exit 0, a backend
exposing no hash degrading to a size-only check, the `rclone is not installed`
check removed, disabled remote check, a removed cron `PATH` pin (the bullet for
it is in the list above), a restore drill writing the backup job's status, and
the monthly sidecar write failure reported as a healthy run.
Nine more mutate the fail-closed seam: the seam never called, the
`JFBACKUP_ENV_FILE` leg removed (the credential half that actually mailed the
real inbox), the `JFBACKUP_REMOTE_ROOT` leg removed, the refusal moved below the
mkdir+lock (so only the ordering assertions can catch it), the refusal also
sending the alert email, the escape hatch opening on a value that is not `1` (so
`=0` behaves like `=1`), the escape hatch defaulting to ON, the location knobs no
longer being watched, and the destination knobs no longer being watched.
Two mutate the **documented cron entry** rather than the script: the entry
altered to set a seam knob (so the documented nightly job is itself refusable),
and the entry deleted outright (leaving the assertion nothing to read). Both are
needed because that assertion once grepped README **line 8**, a prose sentence
naming the same script path, so it read prose instead of the cron entry and
reported `ok` whatever the entry set.
The last five mutate **containment** rather than the seam, which is the
distinction the hand-run section is built on: the README's recipe dropping a knob,
the suite's own helper dropping one, the false "property of the script" sentence
being restored, and the script ignoring a named `STATE_DIR` or `STAGE_DIR`.

**That harness runs inside a private mount namespace** (`unshare -m`, with a
tmpfs over each installed-default path), and **refuses to run without one**. Two
of the containment mutations make the mutated script resolve a knob to the value
installed on the host, so an unsandboxed run of them stages real dumps and writes
fabricated status files into the paths the production job owns. That failure is
not hypothetical: the containment group leaked into the agent host's
`/var/backups/jomform` and `/var/lib/jomform-backup` the first time it was run,
and had to be hand-remediated. The wrapper is what makes this harness safe to
keep, and removing it would reintroduce that leak.

The monthly sidecar path has **two** exceptions to that, not one. The verbatim
daily sidecar copy is sabotaged **inside the suite** — `jomform-backup_test.sh`
does it itself, to a copy of the script in a temp dir, so the sabotages above do
not include it. The *write failure* half is mutated by this harness, but only
since that mutation was anchored on the monthly block itself: it used to anchor on
the bare `    uploads_ok=0`, which occurs three times in `jomform-backup.sh` (the
two daily loops and the monthly gate), so `replace(..., 1)` landed on the
**daily** site and left this path unmutated while the README claimed the harness
covered it. The case does bite either way — mutate the monthly gate alone and the
suite goes red on that case's own `exit=1` / `FAILED` / `last-failure.json`
assertions — so the gap was in what the harness applied, never in the assertion.

Three of those twelve were **not** caught when this harness was first written
against the suite, and the suite grew the assertions they need rather than the
claim being trimmed: the `FAILED` line's *level* (the message text survives a
downgrade to `WARN`, so the suite now asserts `ERROR`), an ignored restore
failure (every `--restore` case drove a stub that succeeded, so there is now a
case whose `pg_restore` fails), and the cron `PATH` pin (running under `env -i`
does not prove the script pins its own `PATH`, because bash supplies a usable
default when the environment has none — the new case hands the job a `PATH` that
resolves nothing).

- **`rclone copy` fails** — the provider's own error text is carried into the log
  and the status detail (not just "upload failed"), the run exits non-zero, and
  `last-failure.json` is written.
- **`rclone copy` exits 0 but nothing lands on the remote** — the post-upload
  re-read finds no object and the run FAILS. This is the trap the whole script
  exists to close: exit 0 is not proof of an upload.
- **The object lands but its bytes differ at the same size** — the MD5 comparison
  fails the run, so a truncated or corrupted upload cannot pass on size alone.
- **The backend exposes no hash for the object** — the run fails rather than
  silently degrading to a size-only check.
- **Remote unavailable / token expired** — the dump is still written locally, the
  run exits non-zero, logs the rclone error, writes `last-failure.json` and sends
  the alert. The alert body says explicitly that a local dump on the same host as
  the database is not an offsite backup.
- **`pg_dump` produced an empty archive** — the dump is inspected with
  `pg_restore --list` and a run that yields no `TABLE DATA` entries fails rather
  than uploading a plausible-looking file.
- **`rclone` is not installed at all** — the local dump still happens and the run
  still fails loudly. The premise is expressed by removing `rclone` from the
  path the script pins itself: a `JFBACKUP_PATH_PREFIX` cannot do it, because it
  only *prepends* to `_SYS_PATH` and `/usr/bin` stays on the path — and a prefix
  entry named `rclone` does not shadow the real one either, since bash skips any
  PATH candidate that is not an executable regular file (a directory, a
  non-executable file and a broken symlink were each measured to resolve through
  on a host that has `/usr/bin/rclone`). The case therefore mirrors the six
  pinned directories (symlinks, minus `rclone`) into the case dir and pins a
  copy of the script's `_SYS_PATH` at it. The case asserts the premise holds —
  `rclone` off that path, `python3`/`docker`/`gzip` still on it — and reports a
  **failure**, not a skip, when the fixture cannot express it.
- **The restore fails** — `--restore` exits non-zero and records the failure
  instead of reporting OK.
- **The JSON status file must stay parseable** — an rclone error containing double
  quotes is escaped, and the test asserts the file actually parses.
- **A restore drill must not overwrite the backup job's status** — a drill that
  wrote `last-run.json` would hide a failing nightly backup behind a green drill.
- **The job must not depend on cron's `PATH`** — cron hands a job a minimal
  environment; the script pins the system path itself, and the case runs it with
  a `PATH` that resolves nothing and requires the dump to happen anyway. This is
  the one case that cannot be written by running under `env -i` alone: bash
  supplies a usable default `PATH` when the environment carries none, so a script
  that merely inherited it would pass.
- **The monthly sidecar must name the monthly object** — the promotion writes the
  sidecar for the monthly name instead of copying the daily file, and the case
  downloads the pair into an empty directory and runs a real `sha256sum -c`. The
  daily files are deleted first, because local retention prunes them: without
  that step an absolute staging path in the sidecar still resolves on the host,
  and the check would pass on the very sidecar this case exists to reject.
  The 1st-of-month branch is driven through the `JFBACKUP_DAY_OF_MONTH` seam
  (`DAY_OF_MONTH` is otherwise computed from `date -u +%d`, so the branch would
  never execute in the suite at all); a separate case asserts that with the
  override unset the script still uses the real date, so the seam is not a way
  to promote every night.
- **The monthly sidecar write fails** — the run sets `uploads_ok=0`, logs the
  failure and writes `last-failure.json`, rather than reporting a healthy run
  with an unverifiable monthly copy.
- **A partial override refuses before doing any work** — with `JFBACKUP_STAGE_DIR`
  (or any other location knob) set and `JFBACKUP_ENV_FILE`, `JFBACKUP_REMOTE` or
  `JFBACKUP_REMOTE_ROOT` missing, the run exits non-zero naming the missing
  variable and takes **no** dump, **no** media archive, **no** lock file and
  **sends no alert email** — asserted on the ordering (the stage directory is
  byte-for-byte unchanged), not just on the exit code. Note what the ordering
  assertion covers and what it does not: the case moves `JFBACKUP_STATE_DIR` too,
  so the refusal's own `write_status()` lands in the case's scratch state dir and
  the stage dir is provably untouched. A refusal that left `JFBACKUP_STATE_DIR` at
  its installed value would still write the FAILED record into the production
  state dir — refusal ordering protects the dump, not the status file. The same
  run with those knobs set proceeds normally, and `JFBACKUP_ALLOW_UNSAFE=1` is
  the deliberate escape hatch that logs a WARN naming the credentials and remote
  it reached.
- **A destination-only override is a hand-run too** — a run that moves **only**
  `JFBACKUP_REMOTE`/`JFBACKUP_REMOTE_ROOT`, with no location knob moved at all, is
  refused for the same reason and with the same ordering: it is the shape an
  operator gets by editing the installed cron line to try a scratch folder, and
  its credential half is still `/opt/jomform/.env`. The case is built against a
  copy of the script whose installed defaults point into the case directory,
  because every knob the other helpers set is itself a move.

A test that only ever passes proves nothing, so the suite is run against a
deliberately sabotaged script for each of: removed hash verification, removed
post-upload verification, the `FAILED` line downgraded to `WARN`, removed
empty-archive guard, unescaped status JSON, ignored restore failure, upload
failure returning exit 0, disabled remote check, a removed cron `PATH` pin,
a restore drill writing the backup job's status, and the monthly sidecar copied
verbatim from the daily object (the last one is done inside the suite itself, on
a copy of the script in a temp dir).

The fail-closed test seam above has its own bite proof, which mutates the seam
one part at a time in a throwaway copy of this directory and requires the suite
to go red each time (a mutation that does not land, or one that leaves the script
unparseable, is reported as an abort rather than counted):

```bash
bash deploy/backup/jomform-backup_bite.sh
```

It covers: the seam never called, the `JFBACKUP_ENV_FILE` leg removed (the
credential half that actually mailed the real inbox), the
`JFBACKUP_REMOTE_ROOT` leg removed, the refusal moved below the mkdir+lock (so
only the ordering assertions can catch it), the refusal also sending the alert
email, the escape hatch defaulting to ON, the location knobs no longer being
watched, and the destination knobs no longer being watched (the shape a
destination-only run produces).

### Running the suite needs nothing but bash

It stubs `docker` and `rclone` on the `PATH` it hands the script, so it runs on a
laptop, in a worktree, or on the production host without touching Drive.

### This suite is a preflight gate (`backup`)

`scripts/preflight.sh` runs it on every full gate, as the `backup` gate, alongside
the other two offline self-tests (`monitor`, `tmpkey`):

```bash
./scripts/preflight.sh --only backup
```

Until that gate existed **nothing ran this suite** — `--list` did not name it and
the runner had no call site for it, so an edit to `jomform-backup.sh` could break
the whole suite while the gate still printed green. The nightly backup is the one
job whose failure is silent data loss, so it is exactly the wrong one to leave
ungated. `scripts/preflight_test.sh` check 2 now fails if the gate is advertised but
has no `run backup` call site (verified by de-wiring it: `listed but never run:
[backup]`).

The **bite proof** (`jomform-backup_bite.sh`, ~8 minutes) is deliberately *not* a
per-run gate — 8 minutes on every merge is how a gate gets skipped. Run it on demand
whenever the seam or the failure paths change:

```bash
bash deploy/backup/jomform-backup_bite.sh
```

