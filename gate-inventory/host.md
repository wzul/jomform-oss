# `host` — suite gate detail

Command (what the table row runs): `deploy/host/bootstrap-host_test.sh`

Edit THIS file for what this gate catches, its spec counts and its history. The
row in [`GATE-INVENTORY.md`](../GATE-INVENTORY.md) is an index entry: it changes
only when the gate is added, removed, renamed or its command changes — that split
is why two gates' edits never meet in one diff (see "Why the detail lives in one
file per gate" there).

## What it catches

the fresh-host bootstrap, the repo/host drift check and the media migration, each against its own harness (no real host: a stubbed `docker`/`id`/`rclone` on PATH, `--root` at a temp dir, fixtures instead of ssh). The bootstrap's dry-run is the DEFAULT and writes NOTHING (the install root is asserted still empty); it refuses an env file that is world-writable-readable or missing a key the compose reads — and the key list is DERIVED from `deploy/vps/compose.prod.yml`, proven by planting a key in a COPY of the compose and expecting it named, so the contract cannot rot into a list someone wrote once; it refuses to clobber a differing live file without `--force` (and when forced, plans the BACKUP before the install, in that order); it refuses `--apply` as a non-root user, a host with no docker, and a missing `--env-file`; and it states that it did NOT move the data. The compose contract is also asserted against the REAL binary (`docker compose config` with a generated env: exit 0, no warning, no value interpolating to null), and the prod compose must name no withdrawn image (`minio/minio`, `minio/mc`, `quay.io/minio`) with the check bite-proved on a planted one. The drift check compares the repo's `deploy/vps/` files byte-for-byte with the host's `/opt/jomform/compose.yml` and both `/opt/traefik/` files, printing DRIFT with the diff and exit 1, and treating a missing or EMPTY live file as a FAILURE rather than a pass. `deploy/vps/migrate-media_test.sh` proves the MinIO→SeaweedFS copy is only ever reported VERIFIED when both sides agree on object count AND bytes and rclone's per-object check finds nothing missing (a lost object, a shorter byte total, a failed source read, a destination that is not this bucket, and a store that never gets healthy are each RED), and that the credentials never reach a command line and the 0600 config is removed on every exit
