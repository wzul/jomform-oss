# Building and shipping JomForm images

One guarded entry point for building the api (or web) image and shipping it to
the VPS. It exists because during the v1.0.0 deploy two agents built images
from the same working tree at the same time, and prod briefly ran a pre-release
binary labelled as the release.

This file is about IMAGES. For standing the stack up on a NEW host — the compose
and Traefik the host must run, the env contract (which secrets are inherited and
which may be minted), the repo/host drift check, and the data move — see
[`HOST-SETUP.md`](HOST-SETUP.md).

## What the script guarantees

| Guarantee | Mechanism |
|---|---|
| Never builds from a dirty tree | exits non-zero if `git status --porcelain` is non-empty |
| Two builds can't interleave | `flock -n` on `/tmp/jomform-build.lock` — second invocation fails fast instead of racing |
| Provenance | stamps `VERSION` (git describe) + `COMMIT` (rev-parse) as build args and OCI labels; `docker image inspect` ties any image to an exact commit |
| **The security + build gate ran** | runs `scripts/preflight.sh` before any image is built, and does not proceed unless **every** gate it lists passes (`./scripts/preflight.sh --list` is the authoritative inventory; the script's own label and `--skip-tests` warning are derived from it, so they cannot drift). A failing gate exits non-zero **before an image exists**, so a broken or vulnerable build cannot ship |
| Works with the scp pattern | `docker save | gzip` tarball, optional `scp` when `SSH_HOST` is set — no registry needed |

There is **no GitHub Actions CI** for this repo (see "Why there is no CI"
below), so the preflight is not a convenience — it is the only gate. `--skip-tests`
bypasses the whole preflight and prints a loud warning; it is for a genuine
emergency only, and the resulting image carries no gate evidence.

### Per-tree prerequisites (the gate refuses without them)

The preflight is run inside whichever tree you are in, and a fresh worktree has
no dependencies. Three seeds, once per tree:

```bash
cd web && npm ci                                        # web build / a11y / unit suite
source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}  # JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL
docker info                                             # the `webimage` gate (see below)
```

The `webimage` gate is the only gate that builds the **shipping web artifact**,
so its prerequisite is treated exactly like the two above: an unreachable docker
daemon is a **refusal**, not a skip — `scripts/preflight.sh` exits non-zero with
the daemon named, because a preflight that turns this gate off is a green run
that proves nothing about the image. On a host with no daemon you have to say so
out loud, which prints the NOT-RUN warning block:

```bash
./scripts/preflight.sh --skip webimage
```

The `web` gate's dependency refusal is worth knowing about, because the failure
it prevents is **silent**. `npm run test:unit` is `node --test … src/lib/*.test.ts`,
and `node --test` needs no dependencies to start — so in a tree whose
`web/node_modules` is missing, empty, or half-installed (an interrupted `npm ci`)
the command still runs, the test files that `import "react"` die at module load,
their remaining tests never register, and it prints a *smaller, plausible-looking*
summary instead of failing. Measured at one head: `254 tests / 253 pass / 1 fail`
against `260 / 260 / 0` with a real install — one false failure and six silently
absent tests in the same run. That count is not evidence about the code, and it
was read as evidence on this host. "Half-installed" covers both depths the
interruption can stop at: whole package directories absent, and — one step later
— every declared directory present with only its `package.json` inside, which
measured `326 / 324 / 2` against `350 / 350 / 0`.

`web/scripts/web-deps-check.mjs` asserts every package declared in
`web/package.json` is present under `web/node_modules/` **and** that each package
that declares a runtime entry point has the file it names (`exports["."]` when
present, else `main`) on disk — the second half is what refuses the deeper
interruption, where every directory exists and no payload does. `npm run test:unit`
runs it before `node --test`, and the `web`/`a11y` prerequisites in
`scripts/preflight.sh` run the same checker — so an uninstalled tree is refused
by both the suite and the gate, with the `npm ci` command in the message, rather
than reporting a number. `scripts/preflight_test.sh` pins that refusal (and the
empty-`node_modules`-with-`.bin/next`-present shape) so it cannot regress.

The api binary logs `version=... commit=...` at startup
(`api/internal/version`), so after deploy `docker compose logs api` proves
exactly which commit is running.

## Why there is no CI (and where the gates went)

`.github/workflows/` was removed on 2026-09-19. Every Actions job failed with
**0 steps** on an account-wide billing failure ("The job was not started because
recent account payments have failed or your spending limit needs to be
increased"), so a red check on a PR said nothing about the code — it was noise,
not a gate. The workflows will be rebuilt when the repo is made public.

Deleting them would have silently dropped security gates that existed
nowhere else, so each was ported to `scripts/preflight.sh` — which has since
grown gates the CI never had. `scripts/preflight.sh --list` is the authority on
the current set; do not read the list below as the full count (it is the old-CI
mapping only):

| Old CI gate | Now |
|---|---|
| `go vet ./...` | `scripts/preflight.sh` gate `vet` |
| `go test ./... -race` | gate `test` (`-race` added to the local trio too) |
| `govulncheck ./...` | gate `govuln` |
| `npm run build` | gate `web` |
| `npm run a11y` (axe-core) | gate `a11y` |
| `npm audit --audit-level=high` | gate `npmaudit` |
| `gitleaks detect` (full history) | gate `gitleaks` |
| — (never a CI gate; it did not exist) | gate `webimage` — a real `docker build -f web/Dockerfile` of the **shipping web artifact**, run cache-only |
| `load-gate.yml` (k6, **on demand only**) | run manually — see `loadtest/BASELINE.md` |

The `webimage` gate has no CI ancestor because nothing in CI ever built the image
either: the preflight ran `npm run build` on the **host** and the image build
happened later inside `deploy/build-and-ship.sh`, after the preflight had already
passed. A tree could therefore be green through every gate while
`docker build -f web/Dockerfile .` died — which has happened twice (both recorded
in `web/Dockerfile`'s header: `MCP-PARITY.md` missing from the build context, and
a `web/` context). `web/src/lib/buildContext.test.ts` guards the COPY **set** for
literal reads and is not enough: it cannot see a `.dockerignore` rule that
filters a COPY source out of the context, a runtime-stage `COPY --from=build`
pointing at a path the build stage never produced, or any host-versus-image
difference that is not a literal read. The gate runs the build with
`--output type=cacheonly`, so BuildKit executes every `RUN` and resolves every
`COPY` against the real context (including `.dockerignore`) while exporting no
image — measured 196s cold and ~3s warm on the agent host, against ~216 MB of
image per run if it were a normal build. Its bite proof is on demand
(`scripts/biteproof_web_image_gate.sh`, ~2.5 min) for the same reason the backup
suite's is.

The k6 load gate was never a per-PR gate (it was `workflow_dispatch`), so it did
not move into the preflight; it is documented as a manual on-demand run.

## Usage

```bash
./deploy/build-and-ship.sh            # builds jomform-api from api/Dockerfile
./deploy/build-and-ship.sh v1.0.1     # explicit version stamp instead of git describe
```

Environment overrides:

| Variable | Default | Purpose |
|---|---|---|
| `IMAGE` | `jomform-api` | image name (and tarball name) |
| `CONTEXT` | `api` | build context directory — **`api` for the api image, `.` (the repo root) for the web image** |
| `DOCKERFILE` | `api/Dockerfile` | Dockerfile path |
| `OUT_DIR` | `/tmp` | tarball output (kept **outside** the repo on purpose — anything inside the repo would trip the dirty-tree guard) |
| `SSH_HOST` | unset | set to scp the tarball (e.g. the prod VPS IP) |
| `SSH_USER` / `SSH_PORT` / `SSH_PATH` | `root` / `22` / `/tmp` | scp destination details |
| `LOCK_FILE` | `/tmp/jomform-build.lock` | single-writer lock path |

Example — build the web image instead:

```bash
IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile ./deploy/build-and-ship.sh
```

### Why the web image is built from the repo root

`web/package.json` runs the unit tests before `next build`
(`"build": "npm run test:unit && next build"`), and
`web/src/lib/adminGrowth.test.ts` reads `api/internal/dbstore/admin_overview.go`
— the Go struct behind the `/admin` growth JSON — to pin the growth-card legend
order to the API's field order instead of restating it in the test. That read
lives at `../../../api/...` **relative to `web/src/lib/`**, so the build context
has to contain `api/` and `web/` as siblings: the context is the repo root
(`CONTEXT=.`), the Dockerfile copies into `/repo/web` + `/repo/api` so the
relative path resolves the same way inside the container as on a developer's
machine, and the root `.dockerignore` keeps the uploaded context small.
Building the web image from `web/` resolves the read to
`/api/internal/dbstore/admin_overview.go`, which does not exist — an unhandled
ENOENT that fails `npm run build`, so the image build exits 1.
The api image is unaffected: `CONTEXT=api` + `api/Dockerfile` (with its own
`api/.dockerignore`) still build.

The `webimage` gate runs exactly this command (`docker build --file
web/Dockerfile --output type=cacheonly .`), so a context or `.dockerignore`
mistake is caught **before** step 2 rather than by the ship script after the
preflight has already reported green.

### Troubleshooting: `stat ... /cmd/jormform-api: directory not found`

Seen from a bind-mounted `go build` and from `stat`/`open` by literal path, while
`ls api/cmd` and globs happily list the entry — which makes it read as a filesystem
fault with `stat()` denying a name `readdir` just printed. It is not: the path is
spelled wrong.

The directory is **`api/cmd/jomform-api`** — one `m` after `jo`. The failing
lookups spell it **`jormform-api`** — two `m`s, 12 characters. That string has never
existed in this repo or anywhere in its git history, so every literal `stat`,
`open`, and `go build ./cmd/jormform-api` on it returns `ENOENT`, while `ls`/globs
keep printing the real entry `jomform-api` because those show the *listing* and the
failing command uses the *typo*. The two differ by one inserted letter in the
middle, which is exactly what makes it look like a phantom flake.

Five-second check:

```bash
ls api/cmd                              # the real entry is jomform-api, never jormform-api
stat -c '%F' api/cmd/jormform-api       # ENOENT  <-- misspelled
stat -c '%F' api/cmd/jomform-api        # directory
cd api && go build -o /tmp/api.bin ./cmd/jormform-api
```

`./cmd/jomform-api` (one `m`) is the only correct spelling. `Makefile`
(`go build -o bin/jomform-api ./cmd/jomform-api`) and `api/Dockerfile` already use
it, so neither the image build nor the gate is affected by the typo.

A second `ENOENT` that is **not** a typo and is also not tree damage: **`api/cmd/openapi-gen`**.
It is genuinely absent from a `v1.14.x` tree — the OpenAPI contract landed afterwards, so
the directory first appears in `v1.16.0` and `stat api/cmd/openapi-gen` on a `v1.14.x` tree
returns `ENOENT` correctly. Confirm which tree you are actually on before reading its absence
as a filesystem fault — and read the tree from the ref you mean, since a local `main` can be
dozens of commits behind `origin/main`:

```bash
git ls-tree -d origin/main api/cmd/    # not `main` — the local ref may be stale
git rev-list --count main..origin/main    # how far apart are they
```

## Deploy order (the whole sequence, every time)

1. **Tag + release first.** Merge all PRs, then
   `git tag vX.Y.Z && git push origin vX.Y.Z && gh release create vX.Y.Z`.
   Never build before the tag exists.
2. `./deploy/build-and-ship.sh` on a clean tree (the script refuses otherwise).
   It runs `scripts/preflight.sh` itself — do not pre-check by hand and do not
   reach for `--skip-tests`. **Before it takes a slot in the host gate pool it
   reads the per-tree result record** (`scripts/preflight_reuse.sh`): a tree
   already recorded green on these bytes gets `--reuse-record` (the runner exits
   before the pool and takes no slot), a partially-recorded tree re-runs only the
   gates the record could not vouch for (`--only <PREFLIGHT_RERUN> --reuse-record`),
   and anything else runs the full gate. The script prints which gates were
   REMEMBERED rather than run, and a remembered gate is never counted as passed —
   see `docs/PREFLIGHT-REUSE.md`. The gate is:
   ```bash
   source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}   # JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL
   ./scripts/preflight.sh --list   # the gate inventory (authoritative)
   ./scripts/preflight.sh          # every gate. Measured 2026-10-03 on the agent host, one clean
                                   # 16-gate run, idle: 753s (12m33s), of which `test` is 279s
                                   # and `e2e` 123s. THAT IS ONE SAMPLE, NOT A BUDGET: wall clock
                                   # tracks host load, and contended runs of the same work have
                                   # been measured at 19-23 min. Plan an incident window on the
                                   # slower number. (`webimage` adds 196s cold, ~3s warm.)
                                   # Runs again inside build-and-ship.sh, or is REUSED there when
                                   # a full-green per-tree record already covers the bytes.
   ```
   Run it standalone first if you want to see a failure before the lock is held.
3. Ship the tarball (`SSH_HOST=... ./deploy/build-and-ship.sh` or scp manually).
4. On the server: `docker load -i /tmp/jomform-api-vX.Y.Z.tar.gz`.
5. On the server: `docker tag jomform-api:vX.Y.Z jomform-api:latest`.
   Compose runs `:latest` — if you skip this, `restart`/`up` keeps the old
   image and you deploy nothing.
6. On the server: `docker compose up -d --force-recreate api`
   (plain `restart` does **not** pick up the newly loaded image).
7. Verify: `docker compose logs api | head` must show
   `version=vX.Y.Z commit=<sha>` matching the tag; then curl the health route.

Steps 4–7 are printed by the script at the end as a reminder; they are always
run on the server manually.

**Probing the running version must not disturb the service.** To read the release
a container is running, use
`docker exec jomform-api-1 /usr/local/bin/jomform-api --version`: it prints
`version=<v> commit=<sha>` and exits 0. `--help` prints the usage, and any other
argument is a usage error (exit 2). All of those are answered **before startup**,
so a probe opens no Postgres or valkey connection, runs no migration, starts no
background worker and binds no port. This matters because the port bind is the
last thing startup does: a binary that ignored its argv would serve that probe by
booting a second full instance inside the live container (migrations and the
email worker included) and only fail on the already-bound port — or never, if it
were given a free one. `curl`ing the public health route is still the check for
whether the service actually serves; the flag only tells you what binary it is.

### A release that owes no image (the delta cannot move the shipped artefact)

Steps 1–7 assume the release owes an image. Some releases do not, and then the
sequence is **step 1 only** — but only when that is *proved*, never asserted. The
screen is: every changed file is either excluded from the build context that
produces the image, or read by no build input.

**api image** (context `api/`, `api/Dockerfile`). The load-bearing rule is Go's
own: `go build ./cmd/jomform-api` never compiles a `_test.go` file, so a delta
that is entirely `api/**/*_test.go` cannot move the binary. Measured 2026-10-17
(`GOTOOLCHAIN=go1.27.2`, `CGO_ENABLED=0`, `-trimpath -buildvcs=false -s -w`):
adding a `*_test.go` to a package left the binary's sha256 **unchanged**
(`26f5900e…` → `26f5900e…`), while adding one non-test `.go` to that same package
**moved** it (`ffe1fd18…`) — so the check bites.

What is **not** the load-bearing reason, although it looks like one: `api/.dockerignore`
lists **both** `*_test.go` and `**/*_test.go`. A pattern with no `/` is root-anchored — a
context holding `root_test.go` and `internal/httpapi/nested_test.go` with `.dockerignore` =
`*_test.go` **alone** keeps the nested file and drops only the root one — but
`**/*_test.go` matches at **any** depth, so as the file stands no `*_test.go` reaches the
api build context (measured 2026-10-27 on Docker 29.7.1: 0 of 3 in that controlled context;
this repo's own `api/` tree, 564 `*_test.go` on disk → 0 in the context). The reason a
test-only delta is still safe is Go's rule above — `go build` never compiles a `_test.go`
— which is why the proof is byte identity of the binary, not an inference from the
exclude list.

**web image** (context the repo root, `web/Dockerfile`). The runtime stage copies
only `/repo/web/.next/standalone`, `.next/static` and `public`, so a file that no
build input *reads* cannot reach it; `web/src/lib/buildContext.test.ts` is the list
of reads that must be present in the context. An `api/` test-file delta owes no web
image when grepping the changed basenames under `web/` finds no reader.

**Prove it; do not assert it.** The check is byte identity of the api binary. Export
each revision with `git archive` (no `.git`, so the build cannot pick up VCS state
that differs between the two trees), build both with the Dockerfile's recipe and —
the part that is easy to get wrong — the stamps the **running** container reports:

```bash
OLD=17e6d526   # the commit prod runs   (docker exec jomform-api-1 /usr/local/bin/jomform-api --version)
NEW=a6b046db   # the commit you tagged
V=v1.21.136    # the version prod runs  (same command)
for rev in $OLD $NEW; do
  mkdir -p /tmp/bi-$rev
  git archive --format=tar "$rev" | tar -x -C /tmp/bi-$rev
  ( cd /tmp/bi-$rev/api && GOTOOLCHAIN=go1.27.2 CGO_ENABLED=0 go build \
      -trimpath -buildvcs=false \
      -ldflags "-s -w -X github.com/wzul/jomform/api/internal/version.Version=$V -X github.com/wzul/jomform/api/internal/version.Commit=$OLD" \
      -o /tmp/api-$rev ./cmd/jomform-api )
  sha256sum /tmp/api-$rev
done
ssh root@$HOST 'docker exec jomform-api-1 sha256sum /usr/local/bin/jomform-api'
```

All three digests must be equal: both builds and the binary the container is
running. Two controls keep that green from being a tautology — build the **new**
revision with its **own** stamps (the tag you did not ship) and the digest must
move, and change one string in production source and it must move too.

The entry point is **`jomform-api`**, one `m` after `jo`: a lookup of
`jormform-api` returns `ENOENT` for a path `ls` happily lists, which reads as a
filesystem fault mid-proof — see the troubleshooting section above.

Worked example, v1.21.136 → v1.21.137 (delta: 7 `M` files, all
`api/internal/httpapi/*_test.go`, 0 non-test files). Both revisions, built through
the Dockerfile with prod's stamps, produce the **same image and the same binary**:

| built from | stamps | api image id | binary sha256 | size |
|---|---|---|---|---|
| `17e6d526` (what prod runs) | `v1.21.136` / `17e6d526` | `6a6f16bd7a1b…` | `7d955f5d912c894670c381701f3d81ac8ae5befd80a3a961a7b5bf5a836cb45a` | 57 184 416 B |
| `a6b046db` (the new tag) | `v1.21.136` / `17e6d526` | `6a6f16bd7a1b…` | `7d955f5d912c894670c381701f3d81ac8ae5befd80a3a961a7b5bf5a836cb45a` | 57 184 416 B |
| `a6b046db` — **control** | `v1.21.137` / `a6b046db` | `670b1a45d357…` | `546856803b8b5ba33c94f3f9fb9e485405eb4bbd143a102becc51c340a36bef9` | 57 184 416 B |

… and that shared digest is the binary in the running `jomform-api-1`.

**A release that ships no image leaves the label behind — deliberately.** At the time of
this example (October 2026) prod kept reporting the previous release (`--version` and
`/version` both said `v1.21.136 | 17e6d526`) while the newest tag was `v1.21.137`, because
the binary running there *is* the v1.21.136 artefact, byte for byte. Nothing was deployed,
so nothing may claim a new version. Do not read that lag as a failed deploy — check the
tag and the byte-identity proof, not the label. The label catches up at the next release
that does owe an image.

## Verifying a deploy: probe a form you own, never a merchant's live form

A deploy acceptance that drives the real public route has to POST a real
submission somewhere. On 2026-09-18/19 four different deploy cards did that
against **a real merchant's live form** (`wan.jomform.com/f/event-registration`),
which left six probe rows inside that merchant's own submission list — listed on
his `/spam` page, where the deploy lane has no business writing.

**Rule: a probe that submits a form must submit to a form the deploy lane owns.**

### The probe surface

Use a form in the account **`abc-abefb94b`** (owned by `wan@jomform.com`). It is
deliberately *not* the merchant-facing `wan` account, so nothing a probe writes
is ever visible to a customer. Its `test` form is published and reachable today.

Verified against prod 2026-09-20, this is the whole state — do not assume more:

| Fact | Measured |
|---|---|
| The `abc-abefb94b` account exists and is published-visible | 2 forms, both `published` (`cuba`, `test`) |
| Its `test` form is usable as an order probe | 4 blocks |
| Its `test` form can serve a *response-mode* probe as it stands | **no** — `config.mode` is unset and its blocks carry **no `id` at all** |
| ANY published response-mode form on the whole deployment with a digit-leading block id | exactly one — the merchant's `event-registration` (`4nwz0vg`). **There is no non-merchant one yet.** |

So for issue #635's shape (`4nwz0vg`, a base36 id leading with a digit — the id
that panicked `renderNotification`) a probe form has to be *created* with a
chosen id, not found. The config is caller-supplied and unvalidated for block
ids: `FormStore.Create`/`Update` take `config map[string]any`
(`api/internal/dbstore/form.go`), and the `upsert_form` MCP tool exposes the same
free-form `Config map[string]any` — so the id can be fixed deliberately:

```jsonc
// probe form config: one text block whose id is chosen, not random
{"mode": "response", "blocks": [{"id": "4nwz0vg", "type": "text", "value": ""}]}
```

`api/internal/httpapi/response_form_digit_block_id_635_test.go` builds exactly
this shape in the test DB, so the same fixture is known to produce a submittable
form — the probe target is reproducible, it just does not exist in prod yet.

Creating that form is a one-time prod write for whoever owns the probe lane — it
is **not** part of a deploy, and it must not be created inside a merchant's
workspace. Do not mint a throwaway form per deploy: `uid()` in
`web/src/app/builder/page.tsx` is `Math.random().toString(36).slice(2, 9)`, so a
random id only leads with a digit ~28% of the time — the shape is a property you
either choose deliberately or get lucky with. Never borrow a merchant's form
because it already has the id you need.

### Cleanup is part of the probe, not a follow-up

A probe write is removed in the same run that made it, per row, by **exact id**
— never a blanket `DELETE`, and never a widened `WHERE`.

Temp API keys have a supported helper instead of a hand-rolled `INSERT`:
`deploy/tmp-apikey.sh`, self-tested by `deploy/tmp-apikey_test.sh` (the `tmpkey`
preflight gate). Hand-rolled versions left live credentials on the prod host
three separate ways — a captured id glued to psql's `INSERT 0 1` command tag, a
key minted in a shape the API can never accept, and a cleanup written as a `LIKE`
sweep that would take a sibling card's live key.

Probe **containers** are the same story one layer down. An inspection probe run
as a bare `docker run …` over ssh is never `--rm`'d, so it stays in
`docker ps -a` for ever: **152 anonymous one-shot containers** accumulated on
prod between 2026-09-19 and 2026-10-12 (149 `exited` + 3 `created`), none of them
attributable to a card, and they were swept by hand (card `t_2646fc00`). Run a
probe with `--rm`, and know that `deploy/host/` now holds the host-side guard that
reclaims what is left behind — stopped containers older than 7 days, and never a
running one, a `com.docker.compose.project`-labelled one, an image or a named
volume. See `deploy/host/README.md`, including the volume reclaim it deliberately
does **not** do.

## Prerequisite

The api image Dockerfile (`api/Dockerfile`) must be present, and the script
exits with a clear error pointing at it when it is not. It declares `ARG
VERSION`, `ARG COMMIT` and `ARG BUILD_DATE` and wires all three into the `go
build -ldflags "-X ...version.Version=$VERSION -X ...version.Commit=$COMMIT -X
...version.Date=$BUILD_DATE"` line, so `/version` (and `--version`, and the
startup banner) reports the same stamps this script logs and writes into the OCI
labels. The ARG defaults mirror `api/internal/version/version.go`, so a bare
`docker build` reports `dev`/`none`/`unknown` rather than an empty string;
`api/internal/version/dockerfile_wiring_test.go` fails if that wiring is lost.
## A purged media object must stop being served — at the origin AND at the edge

`DELETE /v1/rest/media/{id}/purge` removes the row and the object. The **origin**
then answers 404 immediately, and for a while that was the whole check — which is
why the gap went unseen: the CDN in front keeps its own copy and never asks the
origin, so the same URL kept being downloadable from the edge. Measured on prod
2026-10-18, after a successful purge: origin 404 (with a cache-buster) and edge
`200` with `cache-control: public, max-age=86400`, `cf-cache-status: HIT`,
`age: 35`.

Two things close it, and the deployment is expected to have both:

1. **A bounded freshness lifetime (always on).** `/media` and `/assets` answer
   `Cache-Control: public, max-age=300` (`httpapi.mediaCacheControl`). Nothing has
   to be purged for a deleted object to stop being served; the worst case is that
   window. Rendering is unaffected because media objects are IMMUTABLE — a new
   upload is a new key, so the bytes behind a URL never change and a re-fetch can
   only return the same object (or a 404 once it has been purged).
2. **An edge purge in the same request (configure it).** When
   `JF_CF_PURGE_TOKEN` + `JF_CF_PURGE_ZONE_ID` are set (production:
   `deploy/vps/.env.prod.example`; dev: `deploy/.env.example`), the purge asks
   Cloudflare to drop the object's URLs —
   sent as **prefixes**, because a CDN keys on the full URL and an exact-URL purge
   would leave every query variant (`?v=…`, `?t=…`, whatever a page or an embed
   appends) cached — and the response says what happened:

   ```json
   {"status":"purged","media_id":"…","key":"u/…/x.png",
    "edge":"purged","edge_urls":["https://jomform.com/media/u/…/x.png", "…-thumbnail.png", "…"]}
   ```

   `edge` is one of `purged`, `not_configured`, `out_of_zone`, `failed`. A purge
   that did NOT happen still returns 200 (the bytes are already gone and no error
   brings them back) but names itself, and adds `edge_max_age_s` — the window that
   then bounds the staleness. Cloudflare's own `200` means the purge was
   **accepted**; the proof is a following request for the URL answering
   `CF-Cache-Status: MISS` (or 404).

The token needs the **Zone → Cache Purge** permission on the zone the public
media URLs live in (`S3_PUBLIC_BASE`, i.e. `https://jomform.com/media`). Purge is
by URL prefix, which is unaffected by custom cache keys; if the zone's cache key
includes a header (`Accept-Language`, `CF-IPCountry`, …) the purge covers the
default key only.

### The form's share card and QR PNG use the same purge

`/og/{slug}.png` — the generated 1200×630 preview a chat app or a social site
shows when a form's link is pasted — is public and cacheable too, and it draws the
form's **title, workspace name and host**. When a form stops being publicly served
(set back to draft, switched to private, or moved to the trash) the same request
drops the cached card, and the reply names the outcome:

```json
{"status":"draft","edge":"purged",
 "edge_urls":["https://<workspace>.jomform.com/og/<slug>.png",
              "https://<workspace>.jomform.com/qr/<slug>.png"]}
```

`/qr/{slug}.png` — the QR image a merchant prints on a table stand — is the second
artefact in that same request. It carries no merchant text, but it encodes the
form's public URL, so a 200 there confirms the form exists at that slug: the same
disclosure the status gate exists to stop, and the same 24-hour edge entry the
card had before this. One purge names both prefixes; a `/qr/{slug}.png?table=N`
variant is a separate CDN entry under the default key and the prefix covers it.

Two deployment facts matter here:

- Both artefacts are served on a **per-workspace host**, so `JF_CF_PURGE_HOSTS`
  needs a wildcard for the tenant subdomains (`*.jomform.com`) plus any verified
  custom domains. An exact host list makes every purge report `edge: out_of_zone`
  and clear nothing — the form's status still changes, which is why the reply has
  to be read rather than assumed.
- The public arms declare `Cache-Control: public, max-age=300` (not a day), so a
  deployment that cannot purge still stops serving a stale or unpublished card or
  QR within five minutes. That window, not the origin's status gate, is what bounds
  the mistake — measured on prod 2026-10-26, where a draft form's card kept
  answering from the edge with `age: 1243` after the gate had shipped, and again on
  2026-10-31, where a draft form's QR PNG answered `cf-cache-status: HIT,
  age: 1566` while every cache-busted fetch got the origin's 404.

`edge` is `purged`, `not_configured`, `out_of_zone` or `failed`; when it is not
`purged` the reply also carries `edge_max_age_s`. The status change itself never
fails because a purge did — the write has already happened, and the window is what
bounds it.

### Measuring it — probe both ends, never just the origin

`scripts/media-purge-probe.sh` is the check. It probes the origin with a
cache-buster and the edge bare **and** with a query appended (a separate CDN
entry), optionally purges through the app's own route, and decides:

```bash
scripts/media-purge-probe.sh \
  --origin https://halo.example.com \
  --edge   https://jomform.com \
  --path   /media/u/<account>/<uuid>.png \
  --purge  <trashed-media-id> --api https://halo.example.com --token "$JOMFORM_API_TOKEN"
```

- **PASS** — origin 404 and the edge is 404 (bare and with a query), **or** every
  served edge copy is bounded by the lifetime the origin itself declares, within
  `--window` (default 300s) — it then prints how many seconds are left;
- **FAIL** — the origin still serves the object after the purge, or an edge copy
  cannot expire inside the window: the origin declares a lifetime longer than it,
  the entry's `age` already exceeds the lifetime the origin declares now (it was
  filled under a longer TTL), or `--await-expiry` measured it still serving past
  the window.

With no `--purge` the run is a **read-only probe**: the object is supposed to
still be served, so `200` at the origin is the healthy answer and only the edge
copy is judged.

**The edge's own `cache-control` is not its lifetime.** A CDN may REWRITE the
client-facing header — Cloudflare's zone setting *Browser Cache TTL* does — so a
browser can be handed `max-age=14400` while the shared edge entry still expires
on the origin's 300s (measured on this stack 2026-10-18: the entry went
`cf-cache-status: EXPIRED` at t+302s under a `max-age=14400` header). The probe
therefore judges a served copy against the **origin's** declared lifetime, and an
edge value above it is reported as a rewrite, not as staleness. That inference
cannot tell a rewrite from a real edge-cache-TTL override on a *fresh* entry, so
`--await-expiry` measures instead: it re-probes the copy until it stops serving
(404, `cf-cache-status: EXPIRED`/`REVALIDATED`, or an `age` that resets on a
refill) or `--window` + 5s elapses. It costs up to `--window` seconds per served
copy and the observation wins over any header.

Exit codes: `0` pass, `1` fail, `2` the probe could not decide — a transport
error, a usage error, or a served copy whose lifetime cannot be read anywhere
(the origin already 404s, so it declares none; re-probe an object the origin
still serves, or pass `--await-expiry`).
A check that stops at the origin reports a clean purge while the object is still
downloadable — that is the mistake this script is built to prevent.

## Reconciling the media store against the `media` table

Stored bytes and the `media` table can disagree, and when they do
`accounts.media_used_bytes` matches neither. Two measured causes: every MCP image
upload before v1.21.231 stored the base + thumbnail/medium/full objects and
reserved quota but wrote **no row**, so the app could never list, trash or purge
them; and rows written before #545 record only the base object's size.

`api/cmd/media-reconcile` (package `api/internal/mediareconcile`) reconciles the
three views. Report mode is the default and read-only — the pool opens with
`default_transaction_read_only=on` **and** an `UPDATE` probe must be rejected with
SQLSTATE 25006 before anything is listed, so it does not rely on the flag alone.
It groups every object under `u/` by account + base uuid, attaches the `media` row
in **both** states (a trashed row still owns its bytes), and scans the surfaces
that can name a uuid: `users.avatar_url`, `forms.config` / `theme` / `logo_url`
(which covers template forms — `is_template = true`, there is no `templates`
table), `products.images`, `product_variations.images`,
`workspace_settings.landing_page_json` / `menu_json`,
`workspace_legal_pages.content`.

Classification is evidence-first: a row wins (`rowed`); a reference wins over any
ruling (`keep`); then an explicit `deletable` ruling from a decisions file; and
`adopt` — create the row, add no bytes — is the default. There is **no default
delete**, and a `deletable` ruling on a referenced group is refused and reported
as refused.

Nothing is deleted through the app: a row-less object has no id to `DELETE`, and a
purge subtracts the ROW's size, which for a row-less object was never charged
that way, leaving the counter below the truth. Deletion is per object key in the
store, after a second reference check, and then `accounts.media_used_bytes` is
recomputed as `min(SUM(size) of every object under u/<account>/ listed AFTER the
deletes, 1 GiB cap)` — never `old - size` per object, never `SUM(media.size)`.

The binary is static, so it runs inside the api container, which already carries
`DATABASE_URL` and `S3_*`:

```bash
cd api && CGO_ENABLED=0 go build -trimpath -o /tmp/media-reconcile ./cmd/media-reconcile
scp /tmp/media-reconcile host:/tmp/
ssh host 'docker cp /tmp/media-reconcile <api-container>:/tmp/'
ssh host 'docker exec <api-container> /tmp/media-reconcile -json /tmp/report.json'
```

**First run, 2026-10-20** (read-only report, then one approved cleanup): the
bucket held 44 objects / 3,199,411 B against 2 media rows. One account had 12
row-less groups; 9 were referenced by `products.images` and one of those is the
account's live avatar, so "no row" does not mean "unreferenced". The 3
unreferenced groups (a pre-fix MCP probe, 4 objects, and two byte-identical copies
of the avatar — same ETag and size, the referenced copy kept) were ruled
deletable, re-checked, and deleted by key: 6 objects / 61,849 B. Both counters
were then derived from a fresh store listing: the affected account went
3,074,848 → 3,131,665 (an INCREASE — the increment-maintained counter had been
below the store's truth), the other stayed 5,897. Every remaining row-less group
is referenced and was left alone. Details and the decisions format:
`api/cmd/media-reconcile/README.md`.

