# Automated Postgres + media backups to Cloudflare R2

The production database is dumped nightly, gzipped, uploaded to Cloudflare R2
(over rclone's `s3` backend) and verified by re-reading the uploaded object. A
restore drill is a first-class mode because **a backup that has never been
restored is not a backup**.

This directory is the source of truth for the script that runs on the host. The
script is copied to `/usr/local/bin/jomform-backup.sh` and driven by a root cron
entry; it does **not** run out of the checkout.

## What is backed up

| Artifact | Path on the host | Uploaded to |
|---|---|---|
| Postgres dump (custom format, `pg_dump -Fc`, gzipped) | `/var/backups/jomform/jomform-daily-<UTC date>.dump.gz` | `r2jomform:jomform-backup/daily/` |
| Checksum of the dump | same path + `.sha256` | `r2jomform:jomform-backup/daily/` |
| MinIO media volume (`/data`, tar+gzip) | `/var/backups/jomform/jomform-daily-<UTC date>-media.tar.gz` | `r2jomform:jomform-backup/daily/` |

Retention: **7 daily**, **4 weekly** and **6 monthly** backup **sets**. One set is
three files — the dump, its `.sha256` sidecar and the `-media.tar.gz` archive — and
`prune_remote` counts **files**, so the file budgets are **21 / 12 / 18**.

The weekly set is promoted every **Sunday (UTC)**; the monthly set on the **1st**
of each month. Both are promoted by a server-side `rclone copyto` (no re-upload)
into `r2jomform:jomform-backup/weekly/` and `r2jomform:jomform-backup/monthly/`,
and both promote **all three** artifacts — the dump, its sidecar and the media
archive — so a weekly or monthly restore can render customer media, not only
rebuild the rows that point at it. The weekly name is the **ISO year-week**
(`jomform-weekly-<YYYY>-W<WW>`, `date -u +%G-W%V`) rather than a calendar week, so
it sorts correctly across a year boundary — `prune_remote` keeps the newest names
by `sort -r`, and a name like `2026-W01` must not sort below `2025-W52`. Local
staging keeps the most recent 3 days of dumps; R2 is the durable copy.

R2 has **no object versioning and no S3 Object Lock** — Cloudflare's "S3 API
compatibility" page lists `GetBucketVersioning`, `PutBucketVersioning` and the
Object Lock headers as **not implemented**. An overwrite therefore destroys the
previous object outright, so **this retention window IS the version history**, and
R2's free tier (10 GB/month) makes the extra dailies cost nothing rather than
being trimmed to save space.

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

The promotion is verified before it is reported: the promoted dump is re-read and
its digest compared with the one this sidecar names, the promoted media archive is
re-read and its size and hash compared with the local staged archive, and the
sidecar is written only once those comparisons have passed. A monthly dump that is
not on the remote, or is not the bytes that were promoted, therefore fails the
run — and gets no sidecar, so nothing makes the pair look verified.

Verify a monthly copy by downloading the pair and checking it:

```bash
rclone copy r2jomform:jomform-backup/monthly/jomform-monthly-<YYYY-MM>.dump.gz* /tmp/
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

The remote and its S3 credentials live **outside this repository**, at
`/root/.config/rclone/rclone.conf` with mode `600`. This repository may become
public and `gitleaks` scans the full git history, so the credentials must never be
committed, copied into a worktree, or pasted into a PR.

The remote is named **`r2jomform`** and uses rclone's **`s3` backend with
`provider = Cloudflare`**. For an S3 remote `REMOTE_ROOT` is the **bucket**,
`jomform-backup`, so the durable prefix is `r2jomform:jomform-backup` and the
`daily/` `weekly/` `monthly/` layout under it is unchanged from the Drive era. The
name is load-bearing rather than cosmetic: `r2jomform` is the script's installed
default (`REMOTE_DEFAULT`), so the cron job resolves `r2jomform:` unless
`JFBACKUP_REMOTE` overrides it. A remote created under any other name is not the
one the job reaches, and every nightly run then fails with *"didn't find section in
config file"*.

The concrete R2 facts, so the config is reproducible:

- **Endpoint**: `https://7d89f41b310291c98f383d10c698e29f.r2.cloudflarestorage.com`
- **Region**: `auto` (R2 only has `auto`; an empty value and the `us-east-1` alias
  both resolve to it)
- **Bucket**: `jomform-backup`, location APAC, and **PRIVATE** — no public access
- **Credentials**: one R2 **Account API token scoped to the single bucket**
  `jomform-backup`, with only **Object Read & Write**

The least-privilege reasoning here is the point, and it is not the Drive page's old
`drive.file`-scope argument: the token grants access to **one bucket and nothing
else in the account**, so root on the production host yields one backup bucket —
not the whole R2 account. (A bucket-scoped S3 token does have a cost on the read
path: it cannot `ListBuckets`. See the write caveat below.)

The config section, for reference (the secret itself never lives in this repo):

```ini
[r2jomform]
type = s3
provider = Cloudflare
access_key_id = <R2 token Access Key ID>
secret_access_key = <R2 token Secret Access Key>
endpoint = https://7d89f41b310291c98f383d10c698e29f.r2.cloudflarestorage.com
region = auto
```

### `--s3-no-check-bucket` is REQUIRED on every write

Because the token is scoped to **one** bucket, `ListBuckets` is **denied (403)**.
rclone then cannot confirm the bucket exists, concludes it is missing, and calls
`CreateBucket` — which the token is **not** allowed to do — so **every upload fails
closed**, 10 runs out of 10, on rclone 1.75.2 and 1.60.1 alike:

```
Failed to copy: failed to prepare upload: operation error S3: CreateBucket,
https response error StatusCode: 403 ... api error AccessDenied
```

This is a property of the token's least privilege, not of any rclone version, and
it is **not** fixed by retries. The script therefore passes `--s3-no-check-bucket`
on **every rclone write**: `upload_file`'s `copy`, and the tier promotion's
server-side `copyto` (weekly and monthly). Read operations (`lsjson`, `lsf`,
`hashsum`, `cat`, `deletefile`) do not need it. The equivalent remote-config option
is `no_check_bucket = true`, but the flag is kept in the script **on purpose** — the
rclone config lives outside the repo and must not be load-bearing, or a re-created
config would silently break every backup. The suite models this failure, so
dropping the flag turns it red.

There is **no OAuth token to reconnect** for an S3 remote — `rclone config
reconnect` is a Drive remedy and does not exist here. If uploads fail with an auth
error, issue a **new R2 API token** in the Cloudflare dashboard (Object Read & Write
on the `jomform-backup` bucket) and write its Access Key ID / Secret Access Key into
`/root/.config/rclone/rclone.conf`. The failure alert names exactly this remedy.

Verify the remote is reachable and scoped as expected rather than assuming it:

```bash
rclone lsjson r2jomform:jomform-backup/daily/            # lists what the job uploaded
grep -E '^\[r2jomform\]|^provider|^endpoint|^region' /root/.config/rclone/rclone.conf
```

A one-bucket token listing only the job's own artifacts is the expected and correct
output; other buckets in the account are invisible to it by design — that is the
scoping working, not a fault.

## Install on the host

```bash
install -m 0755 deploy/backup/jomform-backup.sh /usr/local/bin/jomform-backup.sh
install -d -m 0700 /var/backups/jomform /var/lib/jomform-backup
```

Then add the cron entry (this is the only scheduled job for this purpose):

```cron
# JomForm nightly backup -> Cloudflare R2. Logs to /var/log/jomform-backup.log.
17 2 * * * /usr/local/bin/jomform-backup.sh >> /var/log/jomform-backup.log 2>&1
```

Nightly at 02:17 **host local time** (the agent host is Europe/Berlin — CEST/UTC+2 —
so this is 00:17 UTC in summer and 01:17 UTC in winter; the timer is a systemd unit on
that host and reads its local clock, not UTC). Off the top of the hour so it is not competing with other
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
destination from `JFBACKUP_REMOTE_ROOT` (the production R2 backup). An ad-hoc
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
JFBACKUP_REMOTE=r2jomform \
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
R2 backup **by omission**. That is a test on what the caller provided, not a check that the
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
returns success but leaves nothing on the remote fails the run. Each tier
promotion (weekly **and** monthly) is a write of its own and is held to the same
rule: the promoted dump is re-read and its digest compared with the digest the tier
sidecar names, and the promoted media archive is re-read and compared with the
local staged archive, **before** that sidecar is written — so a `copyto` that
stored nothing, or stored other bytes under the tier name, fails the run instead of
shipping a set that looks complete.

Every rclone **write** carries `--s3-no-check-bucket`. The R2 token is scoped to
one bucket, so it cannot `ListBuckets`; without the flag rclone calls `CreateBucket`
(403, `AccessDenied`) and **every write fails** — the backup would fail every
night. The stub models that real failure, so dropping the flag turns the suite red.

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

Every one of these is exercised by `jomform-backup_test.sh` (293 assertions,
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

It applies **thirty-nine** sabotages in six groups. Thirteen target the script's
failure handling: removed hash verification, removed post-upload verification,
the `FAILED` line downgraded to `WARN`, removed empty-archive guard, unescaped
status JSON, ignored restore failure, upload failure returning exit 0, a backend
exposing no hash degrading to a size-only check, the `rclone is not installed`
check removed, disabled remote check, a removed cron `PATH` pin (the bullet for
it is in the list above), a restore drill writing the backup job's status, and
the monthly promotion failure reported as a healthy run.
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
Five mutate **containment** rather than the seam, which is the
distinction the hand-run section is built on: the README's recipe dropping a knob,
the suite's own helper dropping one, the false "property of the script" sentence
being restored, and the script ignoring a named `STATE_DIR` or `STAGE_DIR`.
Ten are added by the R2 port and the weekly tier, and they are the ones that pin
the behaviour a future edit would most plausibly lose: **every write missing
`--s3-no-check-bucket`** (the CreateBucket/AccessDenied failure — the most
valuable one here), the Drive OAuth `reconnect` hint restored in place of the R2
credential remedy, the **weekly gate** disabled and separately forced always-on,
the **media archive no longer promoted** into a tier, the daily / weekly / monthly
**retention budgets** each reverted to the old value, the **weekly prune call
removed**, and the `JFBACKUP_KEEP_*` **overrides ignored** (the budget
hard-coded).

**That harness runs inside a private mount namespace** (`unshare -m`, with a
tmpfs over each installed-default path), and **refuses to run without one**. Two
of the containment mutations make the mutated script resolve a knob to the value
installed on the host, so an unsandboxed run of them stages real dumps and writes
fabricated status files into the paths the production job owns. That failure is
not hypothetical: the containment group leaked into the agent host's
`/var/backups/jomform` and `/var/lib/jomform-backup` the first time it was run,
and had to be hand-remediated. The wrapper is what makes this harness safe to
keep, and removing it would reintroduce that leak.

The promotion path has **five** sabotages that live **inside the suite** rather
than in this harness, and they are named here so "the sabotages above" is never
read as covering them: the verbatim daily sidecar copy, the removal of the
sidecar's post-upload re-read, the removal of the promoted dump's re-read
(run twice, once per shape — a copy that stored nothing and a copy
that stored a truncated object; `sabotage_promoted_dump_verification`), the
removal of the promotion's own failure mark (`tier_ok=0`) in its copy-failure
branch (the mutation leaves the run failing — the promoted-dump re-read catches
the absent object too — so the assertions it turns red are the ones on the
**verdict**: the `FAILED` record and the status detail must still name the copy
that broke, not the re-read that noticed afterwards), and the **R2
`--s3-no-check-bucket` removal** (a copy of the script with the flag stripped must
go red against the stub's modelled CreateBucket/AccessDenied). Each runs
against a copy of the script inside a temp dir, so the tracked file is never
written to. The promotion-failure half IS also mutated by this harness (the
"monthly promotion failure returning a healthy run" mutation), and that mutation
is anchored on the monthly gate's own `FAIL_DETAIL` + `uploads_ok=0` block — the
only occurrence carrying "monthly promotion failed", since the weekly gate is a
separate block now — so a future edit that moves or rewrites the monthly gate
makes it ABORT rather than quietly mutate the weekly path instead. It used to
anchor on the bare `    uploads_ok=0`, which occurs three times in
`jomform-backup.sh`, so `replace(..., 1)` landed on a daily site and left the path
unmutated while the README claimed the harness covered it.

Three of those thirteen failure-handling sabotages were **not** caught when this
harness was first written against the suite, and the suite grew the assertions
they need rather than the claim being trimmed: the `FAILED` line's *level* (the
message text survives a downgrade to `WARN`, so the suite now asserts `ERROR`),
an ignored restore failure (every `--restore` case drove a stub that succeeded, so
there is now a case whose `pg_restore` fails), and the cron `PATH` pin (running
under `env -i` does not prove the script pins its own `PATH`, because bash
supplies a usable default when the environment has none — the new case hands the
job a `PATH` that resolves nothing).

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
- **A CONFIGURED remote must not be read as unconfigured** — `rclone listremotes`
  prints one line per remote (one `write()` per line with a real rclone), and the
  membership test reads that list from a **here-string**, never through a pipe: a
  `grep -qx` that exits at its first match can leave a pipe writer with whole
  lines still queued, and under `set -o pipefail` the writer's SIGPIPE makes 141
  the pipeline's status even though `grep` matched — a remote that IS configured
  is refused as "not configured", the nightly backup fails closed and a false
  "remote unavailable" alarm is mailed to the super-admins. The case drives a stub
  whose remote list carries three names with the needle **first**, and the stub
  pauses between writes so the early-exit reader really has closed the pipe before
  the next write: that is what makes the case fail on **every** run against a pipe
  matcher instead of most of them (196/200 refusals with no pause, 200/200 with
  one, measured here). It asserts the run exits 0, uploads and verifies, and raises
  no alarm — and a second case asserts the same three-name list still REFUSES a
  remote that is genuinely absent, so a matcher loosened to accept anything cannot
  satisfy the pair. The bite harness reverts only the matcher body to the pipe form
  and requires the accepted direction to go red.
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
- **The monthly sidecar write exits 0 and stores NOTHING** — the sidecar's own
  upload is re-read instead of being trusted on the command's exit status: an
  `rclone rcat` that consumes the stream, exits 0 and leaves no object fails the
  run. The case asserts the run's `exit code` is `exit=1`, that the log names the
  verification (`monthly sidecar verification failed`) and the missing object
  (`remote object missing or unreadable after upload`), that the log marks
  `FAILED` with `last-failure.json` written — and, the point of the case, that
  **no** sidecar object is left on the remote, so nothing makes the pair look
  verified. Driven by the stub's `STUB_SILENT_NAME` naming the monthly `.sha256`,
  which drops exactly that one object's stream while every other object lands. A
  further case removes the re-read from a copy of the script (an in-suite
  sabotage, above) and requires that run to report SUCCESS again, so the
  assertion bites rather than decorating.
- **The monthly sidecar lands but its stored CONTENT differs** — size and hash
  alone are not the acceptance: the stub leaves the stored sidecar's size and hash
  right for the modelled backend, so the size/hash re-read passes, and makes the
  re-read return different bytes — only the content comparison catches it. The
  case asserts the run's `exit code` is `exit=1`, that the log names the content
  mismatch (`remote content mismatch`) and marks `FAILED`, rather than reporting a
  healthy month with an unusable checksum. Driven by the stub's
  `STUB_CAT_CORRUPT_NAME` naming the monthly `.sha256` (not the dump); the same
  in-suite sabotage removes this comparison together with the missing-object one.
- **The promoted monthly dump is not what was promoted** — the promotion's own
  write is re-read instead of being trusted on `copyto`'s exit status. A `copyto`
  that exits 0 having stored nothing fails the run (`remote object missing or
  unreadable after promotion`), and so does one that stored different bytes under
  the monthly name (`remote digest mismatch`); neither leaves a sidecar, so a
  dump that failed verification is never dressed as a verified pair. The cases are
  driven by the stub's `STUB_COPYTO_SILENT_NAME` and `STUB_COPYTO_CORRUPT_NAME`,
  which drop or truncate exactly one named destination's copy while the daily
  uploads land normally. Two further cases put the pre-fix behaviour back (see the
  in-suite sabotages above) and require the run to report a healthy month again —
  one of them then runs this README's own `sha256sum -c` on the truncated pair and
  requires it to **fail**, which is the reader's view of the defect.
- **The promotion's own `copyto` fails** — the second failure mode of the
  promotion's write, and the one nothing exercised until now: the copy command
  reports an error of its own (the provider's text on stderr, non-zero exit) while
  the daily trio has already uploaded. The run exits non-zero with
  `"status": "FAILED"` and a `last-failure.json`, the log carries
  `monthly dump copy failed` **and** the provider's own message, both the `FAILED`
  record and the status detail still name the copy that broke, and nothing on the
  remote claims a monthly copy exists — neither the dump nor a sidecar, because the
  sidecar is written only once the promoted dump has been read back, so a failed
  copy can never be dressed as a verified pair. The daily objects are asserted to
  be on the remote, so the case cannot pass on a run that failed before the
  promotion. Driven by the stub's `STUB_COPYTO_FAIL_NAME` (it fails exactly one
  named destination's copy; a case dir is never left behind), and an in-suite
  sabotage named above removes the branch's failure mark and requires the suite
  to go red on the verdict assertions.
- **The weekly tier** — promoted every **Sunday (UTC)** (`date -u +%u` = 7),
  driven in the suite by the `JFBACKUP_DAY_OF_WEEK` seam the same way the monthly
  gate is driven by `JFBACKUP_DAY_OF_MONTH`. A case asserts it promotes on Sunday
  and produces the dump + sidecar + media under `jomform-weekly-<YYYY>-W<WW>` (the
  ISO year-week, so it sorts across a year boundary), a case asserts it does
  **not** promote on another day, and a case with the override unset asserts the
  real date is respected (so the seam is not a way to promote every night).
- **A tier set must carry the media archive** — the monthly (and now weekly)
  promotion promotes the `-media.tar.gz` too and re-reads it, so a tier restore can
  render customer media rather than only rebuild the rows that point at it. The
  case asserts the archive is on the remote and byte-identical to the daily one;
  removing the media promotion turns it red.
- **Retention keeps whole sets** — the daily/weekly/monthly tiers keep the newest
  **21 / 12 / 18** FILES, i.e. **7 / 4 / 6** three-file sets. The case seeds whole
  sets older than the run's own daily set and asserts the kept counts, the newest
  set surviving and the oldest pruned; the `JFBACKUP_KEEP_DAILY` / `_WEEKLY` /
  `_MONTHLY` overrides are exercised too.
- **Every rclone write carries `--s3-no-check-bucket`** — the bucket-scoped R2
  token cannot `ListBuckets`, so without the flag rclone calls `CreateBucket` (403,
  `AccessDenied`) and every write fails. The stub models that real failure, and a
  copy of the script with the flag stripped must go red; separately, a run that
  spans both write shapes (the daily `copy` and the promotion's `copyto`) asserts
  that **every** write invocation carried the flag. This is the assertion that
  stops a future edit silently re-introducing a backup that fails every night.
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
a restore drill writing the backup job's status, the tier sidecar copied
verbatim from the daily object, and an rclone write with `--s3-no-check-bucket`
stripped (the last two are done inside the suite itself, on a copy of the script
in a temp dir).

The fail-closed test seam above has its own bite proof, which mutates the seam
one part at a time in a throwaway copy of this directory and requires the suite
to go red each time (a mutation that does not land, or one that leaves the script
unparseable, is reported as an abort rather than counted):

```bash
bash deploy/backup/jomform-backup_bite.sh
```

It covers, in addition to the seam: the R2/weekly/retention groups named above —
every write missing `--s3-no-check-bucket`, the Drive `reconnect` hint restored in
place of the R2 remedy, the weekly gate disabled and separately forced always-on,
the media archive no longer promoted into a tier, each retention budget reverted to
its old value, the weekly prune call removed, the `JFBACKUP_KEEP_*` overrides
ignored, and the remote-check matcher reverted to `rclone listremotes | grep -qx`
(which refuses a remote that IS configured, see the multi-remote cases above).

### Running the suite needs nothing but bash

It stubs `docker` and `rclone` on the `PATH` it hands the script, so it runs on a
laptop, in a worktree, or on the production host without touching R2.

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

The **bite proof** (`jomform-backup_bite.sh`, ~45 minutes — 40 mutations, each a
full suite run) is deliberately *not* a per-run gate: 45 minutes on every merge is
how a gate gets skipped. Run it on demand whenever the seam or the failure paths
change:

```bash
bash deploy/backup/jomform-backup_bite.sh
```

