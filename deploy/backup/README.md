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

Every one of these is exercised by `jomform-backup_test.sh` (49 assertions,
stubbed `rclone`/`docker`, no network and no database required) and ten of them
are additionally proven to *bite* by sabotaging the script and requiring the
suite to go red:

```bash
bash deploy/backup/jomform-backup_test.sh
```

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
  still fails loudly.
- **The restore fails** — `--restore` exits non-zero and records the failure
  instead of reporting OK.
- **The JSON status file must stay parseable** — an rclone error containing double
  quotes is escaped, and the test asserts the file actually parses.
- **A restore drill must not overwrite the backup job's status** — a drill that
  wrote `last-run.json` would hide a failing nightly backup behind a green drill.

A test that only ever passes proves nothing, so the suite is run against a
deliberately sabotaged script for each of: removed hash verification, removed
post-upload verification, the `FAILED` line downgraded to `WARN`, removed
empty-archive guard, unescaped status JSON, ignored restore failure, upload
failure returning exit 0, disabled remote check, a removed cron `PATH` pin, and
a restore drill writing the backup job's status.

### Running the suite needs nothing but bash

It stubs `docker` and `rclone` on the `PATH` it hands the script, so it runs on a
laptop, in a worktree, or on the production host without touching Drive.

