# Standing up JomForm on a fresh host

This is the app-layer runbook. It exists because "we could deploy to a new server"
was true only in the sense that someone had done it by hand once: measured
2026-10-12, the repo described a host that did not match the one running
production, in ways that would have failed SILENTLY.

## What was actually wrong (measured, 2026-10-12)

| file | repo said | the host runs | what a fresh host would have got |
|---|---|---|---|
| prod compose | did not exist in the repo | `/opt/jomform/compose.yml`, 130 lines | nothing to deploy from |
| `deploy/docker-compose.yml` | the DEV shape | — | `POSTGRES_PASSWORD` defaulting to `jomform_dev`, and `pgdata` mounted at `/var/lib/postgresql/data` while the pinned `postgres:18` image keeps its data dir under `/var/lib/postgresql` — i.e. an **empty database** or a **known password** |
| `deploy/vps/traefik-jomform.yml` | 55 lines, Traefik **v2** syntax (`Host(...)`, `certResolver: le`, `entryPoints: [websecure]`) | 179 lines, **v3** (`HostRegexp`, `letsencrypt`, block sequences) | routing that does not match production |
| `deploy/.env.example` | the DEV shape: a different key set, and `SMTP_*` names | the production contract: its own key set, and the API reads `SES_*` | a deployment with **no working email** and several empty credentials |

The repo now holds the production files **byte-for-byte**, and
`deploy/vps/drift-check.sh` is how that claim is checked rather than believed.

## The pieces

- `deploy/vps/compose.prod.yml` — the production stack, as installed. Its object
  store is **SeaweedFS**, not MinIO: the `minio/minio` and `minio/mc` images were
  withdrawn from Docker Hub, so a compose that names them cannot be pulled at all
  (`docker manifest inspect` answers `denied`). The API creates its own bucket at
  startup, so there is no bucket-init container.
- `deploy/vps/migrate-media.sh` — the one-way copy of the media bucket from an
  existing MinIO store onto SeaweedFS, with a count+bytes+per-object verification.
  See § "Moving production off MinIO". A fresh host needs none of it.
- `deploy/vps/traefik-jomform.yml`, `traefik-docker-compose.yml` — the edge, as installed.
- `deploy/vps/.env.prod.example` — every key the compose reads, marked as
  ACCOUNT secrets (carry over) vs HOST secrets (may be minted). The count is
  deliberately NOT written down here: `deploy/host/bootstrap-host_test.sh` (the
  `host` gate) derives the key set from `compose.prod.yml` and fails if the
  example and the compose disagree in either direction, so a key added to one and
  not the other is red and a number in this sentence could only rot.
- `deploy/vps/drift-check.sh` — repo vs host, per file. Exit 1 on any drift.
- `deploy/host/bootstrap-host.sh` — installs the files, the external network, the
  stack and the two cron lines. **Dry-run by default**; `--apply` needs root.
- `deploy/host/jomform-container-prune.sh` — the host-side guard that reclaims
  STOPPED containers (152 anonymous probe containers had accumulated on prod by
  2026-10-12). Its installer, its decoy suite (`prunehost` gate) and its bite proof
  are in the same directory: **`deploy/host/README.md`** is its doc home. The
  bootstrap installs its CRON LINE only — the script itself is installed by
  `deploy/host/install-host-container-prune.sh`, which is step 2 of the procedure
  below.
- Both scripts have a self-test (`*_test.sh`), run by the `host` gate in the preflight.

## Procedure

```bash
# 0. on the CURRENT host, if you are replacing it: nothing — the nightly backup is
#    the move mechanism (see § Data). Confirm it is green first:
tail -1 /var/lib/jomform-backup/last-run.json

# 1. on the new host: docker + the compose plugin, DNS not yet pointed at it
#    (issue the certificate after the stack answers, so the challenge succeeds)

# 2. the env file: start from the example, fill the ACCOUNT values from the
#    password manager (or copy the current host's /opt/jomform/.env over ssh)
cp deploy/vps/.env.prod.example ~/jomform.env
chmod 600 ~/jomform.env
#    the example ships with EMPTY secrets on purpose: an unfilled copy fails at
#    startup (ENCRYPTION_KEY / POSTGRES_PASSWORD) rather than
#    running with a placeholder that looks like a credential.

# 3. see what it would do, then do it
deploy/host/bootstrap-host.sh --env-file ~/jomform.env
sudo deploy/host/bootstrap-host.sh --env-file ~/jomform.env --apply

# 4. install the backup script + its rclone remote on the new host, or it has
#    NO backups (deploy/backup/README.md § Install on the host)
#
# 5. install the container-prune guard (deploy/host/README.md § Installing it).
#    Step 3 installed its cron line, so without this one the 03:40 job fails
#    silently and stopped containers accumulate exactly as they did before:
deploy/host/install-host-container-prune.sh --host root@<new-host>

# 6. point DNS at the new host, then check the edge is what the repo says
deploy/vps/drift-check.sh --host root@<new-host>

# 7. move the data (see below), verify, then flip the DNS record.
```

## The env contract — the part that breaks a migration

`deploy/vps/.env.prod.example` is the authority; the rules that matter:

- **ACCOUNT secrets must be inherited, never regenerated.**
  `ENCRYPTION_KEY` is the sharp one: it encrypts data at rest (per-account CHIP
  keys, stored credentials). A new value does not rotate them — it makes them
  **unreadable**, and the rows look like corruption. Same reasoning for the
  provider credentials (`SES_*`, `JF_CF_OAUTH_*`, `JF_GOOGLE_*`, `OLLAMA_API_KEY`):
  they are issued elsewhere, so a new host reuses the same client.
- **HOST secrets may be minted** (`POSTGRES_PASSWORD`, and
  `S3_ACCESS_KEY` / `S3_SECRET_KEY` if the media is restored into a
  freshly-initialised store), because the database and the object store are
  rebuilt from the backup, not copied.
- **`SESSION_SECRET` is not part of the contract any more.** It was a startup
  requirement that nothing read: sessions are opaque 32-byte random tokens in
  Valkey, not signed cookies, so there was nothing for a signing secret to sign.
  It was removed from the compose and the config together — an env file inherited
  from an older host may keep the line, but nothing reads it and the compose no
  longer passes it.

`bootstrap-host.sh` DERIVES the required key list from `compose.prod.yml` and
refuses an env file that is missing one — a key added to the compose without
being added to the example is caught there rather than by a deployment with an
empty credential. `deploy/host/bootstrap-host_test.sh` (the `host` gate) asserts
the stronger property on the checked-in file: the example's key set EQUALS the
compose's **in both directions**, so a stale key left in the example is red as
well, and the gate also proves the documented `cp` → `chmod 600` → bootstrap path
works on a copy of it.

## Data

The data move is deliberately **not** automated: it needs the R2 credential and a
decision about the cutover window. It is the documented restore drill, and the
drill is a first-class mode of the backup script:

```bash
/usr/local/bin/jomform-backup.sh --restore /var/backups/jomform/jomform-daily-<date>.dump.gz
```

`deploy/backup/README.md` § Restore drill covers the media archive and the
"a backup that has never been restored is not a backup" reasoning. The nightly
backup writes `last-run.json`; a fresh host that has not been through this step
has an **empty database** even though every container is healthy.

## Moving production off MinIO

**A fresh host does not need this section.** It is for the existing production
host, which has a `jomform_miniodata` volume full of customer media.

MinIO's community images were withdrawn (see `compose.prod.yml`), and MinIO and
SeaweedFS do **not** share an on-disk format: pointing the new service at the old
volume serves an empty store. The objects have to be copied through the S3 API,
which means the old server must still be running when the copy happens.

Order matters:

```bash
# 0. a fresh backup, and check it is green — the media archive is the fallback
/usr/local/bin/jomform-backup.sh && tail -1 /var/lib/jomform-backup/last-run.json

# 1. add the new credentials to the env file (the compose fails closed without
#    them; S3_ACCESS_KEY is an identity — `jomform` — and S3_SECRET_KEY a secret):
#      S3_ACCESS_KEY=jomform
#      S3_SECRET_KEY=$(openssl rand -hex 32)

# 2. copy the media INTO the live stack, and let it prove the copy
#    (PLAN first, then --apply). This starts only the new `s3` service, as an
#    extra container in the live project; the api keeps serving the old store.
deploy/vps/migrate-media.sh --env-file /opt/jomform/.env
deploy/vps/migrate-media.sh --env-file /opt/jomform/.env --apply

# 3. only after it prints VERIFIED: install the new compose and recreate the api
#    against the store that run just filled
deploy/host/bootstrap-host.sh --env-file /opt/jomform/.env --force --apply

# 4. and stop the old store (this is the step that removes the minio container)
docker compose -p jomform -f /opt/jomform/compose.yml --env-file /opt/jomform/.env \
  up -d --remove-orphans
```

Do **not** install the new compose before step 2. `bootstrap-host.sh --apply` ends
in `compose up -d`, which recreates the api against the empty `s3data` volume — every
already-uploaded image 404s until the copy finishes, and an upload in that window
lands only on the new side, which makes the copy's own count comparison red forever.

`migrate-media.sh` pins the compose project (`--project`, default `jomform`) because
`docker compose` otherwise derives the project from the compose file's **directory**:
running the repo's `deploy/vps/compose.prod.yml` from a checkout would create a second
project (`vps`) with its own network, and the copy could not reach the store the api
will use. `--network` defaults to `<project>_internal`, so the two agree.

The script starts the new `s3` service itself, creates the bucket if it is
missing, copies with `--checksum`, and then refuses to call the run a success
unless **object count AND total bytes agree on both sides** and rclone's
per-object check finds every source object on the destination. It never stops or
deletes anything: the old container keeps serving until step 4's
`--remove-orphans`, and the `jomform_miniodata` volume is never touched — keep it
until the media has been exercised in the browser.

What the copy does **not** carry: object **versions**. `minio-init` used to run
`mc version enable`, and the API only ever reads the current version of an
object, so the current objects are the complete set the application uses. The
bucket is also not re-provisioned with a public-read policy when it already
exists (the API's `EnsureBucket` is a no-op in that case) — nothing depends on
anonymous bucket reads: media is served by the API's own `/media/` route, which
reads the object server-side.

### Stored media URLs — the one startup migration that cannot follow the switch

Two different things carry the internal endpoint: the **objects** (copied by
`migrate-media.sh`) and the **stored URLs** in `users.avatar_url`,
`forms.logo_url` and `products.images`. The URLs were normalised once by the
API's startup migration (`dbstore.MigrateMediaURLs`, issue #145), which rewrites
`{S3_ENDPOINT}/{S3_BUCKET}/…` to the browser-reachable base
(`S3_PUBLIC_BASE`, here `https://jomform.com/media/`).

That migration derives the prefix it replaces from the **current**
`S3_ENDPOINT`. After step 3 installs `S3_ENDPOINT=http://s3:8333` it can no
longer rewrite a row that still says `http://minio:9000/…` — nothing in the new
process knows that host name any more. So the check has to run **before** step 3,
not after it.

Checked on the production database on 2026-11-04, before this move: **0** rows in
all three places, and stored media already reads `https://jomform.com/media/…`
(new uploads are written with that public base, never with the internal
endpoint). On this host the switch therefore leaves nothing behind. Run the same
counts on the host you are moving (replace `minio:9000` with your CURRENT
`S3_ENDPOINT`'s host if it differs); all three must be `0`:

```sql
select count(*) from users    where avatar_url like '%minio:9000%';
select count(*) from forms    where logo_url   like '%minio:9000%';
select count(*) from products where images::text like '%minio:9000%';
```

If one is not `0`, rewrite it explicitly — the statements below were replayed
against a scratch schema seeded with a legacy row, a public row, an empty/NULL
row, a Google avatar and a non-array `products.images`: they touch only the
legacy rows, preserve array order, and leave the non-array row exactly as it was
found (the same `jsonb_typeof` rule the startup migration follows). Adjust the
public base if `S3_PUBLIC_BASE` is set to something other than
`https://jomform.com/media`:

```sql
update users    set avatar_url = replace(avatar_url, 'http://minio:9000/jomform-media/', 'https://jomform.com/media/') where avatar_url like '%minio:9000%';
update forms    set logo_url   = replace(logo_url,   'http://minio:9000/jomform-media/', 'https://jomform.com/media/') where logo_url like '%minio:9000%';
update products set images = (
        select jsonb_agg(replace(elem, 'http://minio:9000/jomform-media/', 'https://jomform.com/media/'))
        from jsonb_array_elements_text(products.images) as t(elem)
      )
 where images::text like '%minio:9000%'
   and jsonb_typeof(images) = 'array';
```

After step 3, `cmd/media-reconcile` audits the store with the new `S3_*` values
(`-endpoint` defaults to `$S3_ENDPOINT`). It addresses any custom endpoint
path-style, so it follows the store to `http://s3:8333` instead of asking for
`jomform-media.s3:8333`; run it before step 4 if you want the bucket counted
independently of `migrate-media.sh`'s own verification.

Rollback after step 4 means the old compose plus the old images (still on the
box until pruned) on a store that was written to since the copy; the reliable
fallback is the backup archive from step 0.

## Drift, and what to do about it

```bash
deploy/vps/drift-check.sh --host root@203.0.113.10     # production
```

- **OK** — the repo's files are what the host runs.
- **DRIFT** — the script prints the diff. Fix it by installing the repo's file on
  the host (the repo is the source of truth now), or — if the host change was
  deliberate — by updating the repo file **in a PR**, so both agree again.
  Editing the host file is how the drift happened in the first place; a host-only
  edit means the next person to read the repo is reading fiction.
- The three pairs it checks are `/opt/jomform/compose.yml`,
  `/opt/traefik/dynamic/jomform.yml` and `/opt/traefik/docker-compose.yml`.
  A file it cannot read is a **failure**, never a pass: a green drift check over
  a host it could not read would be worse than no check.

## Still not covered (stated, not implied)

- **DNS / Cloudflare** records and the WAF rule noted in `docs/PROXY-DECISION.md`
  are manual.
- **Certificate issuance** is Traefik's (ACME, `certResolver: le`); the store
  lives at `/opt/traefik/acme` on the host and re-issues, so it is not migrated.
- **The VPS itself** (provider, firewall, SSH keys) is not provisioned from code.
  That is the layer Terraform/Ansible would cover; with one host, a scripted
  bootstrap plus this runbook is the cheaper trade — see the note in the PR that
  introduced this file.
