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

- `deploy/vps/compose.prod.yml` — the production stack, as installed.
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
- **HOST secrets may be minted** (`POSTGRES_PASSWORD`, and `MINIO_ROOT_PASSWORD`
  if the media is restored into a freshly-initialised bucket), because the
  database and the object store are rebuilt from the backup, not copied.
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
