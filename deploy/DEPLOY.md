# Building and shipping JomForm images

One guarded entry point for building the api (or web) image and shipping it to
the VPS. It exists because during the v1.0.0 deploy two agents built images
from the same working tree at the same time, and prod briefly ran a pre-release
binary labelled as the release.

## What the script guarantees

| Guarantee | Mechanism |
|---|---|
| Never builds from a dirty tree | exits non-zero if `git status --porcelain` is non-empty |
| Two builds can't interleave | `flock -n` on `/tmp/jomform-build.lock` — second invocation fails fast instead of racing |
| Provenance | stamps `VERSION` (git describe) + `COMMIT` (rev-parse) as build args and OCI labels; `docker image inspect` ties any image to an exact commit |
| **The security + build gate ran** | runs `scripts/preflight.sh` before any image is built — api build/vet/test `-race`/govulncheck, web build/axe-core a11y/npm audit, and gitleaks over the full git history. A failing gate exits non-zero **before an image exists**, so a broken or vulnerable build cannot ship |
| Works with the scp pattern | `docker save | gzip` tarball, optional `scp` when `SSH_HOST` is set — no registry needed |

There is **no GitHub Actions CI** for this repo (see "Why there is no CI"
below), so the preflight is not a convenience — it is the only gate. `--skip-tests`
bypasses the whole preflight and prints a loud warning; it is for a genuine
emergency only, and the resulting image carries no gate evidence.

The api binary logs `version=... commit=...` at startup
(`api/internal/version`), so after deploy `docker compose logs api` proves
exactly which commit is running.

## Why there is no CI (and where the gates went)

`.github/workflows/` was removed on 2026-09-19. Every Actions job failed with
**0 steps** on an account-wide billing failure ("The job was not started because
recent account payments have failed or your spending limit needs to be
increased"), so a red check on a PR said nothing about the code — it was noise,
not a gate. The workflows will be rebuilt when the repo is made public.

Deleting them would have silently dropped four security gates that existed
nowhere else, so each was ported to `scripts/preflight.sh`:

| Old CI gate | Now |
|---|---|
| `go vet ./...` | `scripts/preflight.sh` gate `vet` |
| `go test ./... -race` | gate `test` (`-race` added to the local trio too) |
| `govulncheck ./...` | gate `govuln` |
| `npm run build` | gate `web` |
| `npm run a11y` (axe-core) | gate `a11y` |
| `npm audit --audit-level=high` | gate `npmaudit` |
| `gitleaks detect` (full history) | gate `gitleaks` |
| `load-gate.yml` (k6, **on demand only**) | run manually — see `loadtest/BASELINE.md` |

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

## Deploy order (the whole sequence, every time)

1. **Tag + release first.** Merge all PRs, then
   `git tag vX.Y.Z && git push origin vX.Y.Z && gh release create vX.Y.Z`.
   Never build before the tag exists.
2. `./deploy/build-and-ship.sh` on a clean tree (the script refuses otherwise).
   It runs `scripts/preflight.sh` itself — do not pre-check by hand and do not
   reach for `--skip-tests`. The gate is:
   ```bash
   source ${JOMFORM_ENV:-$HOME/.config/jomform/test-env.sh}   # JOMFORM_TEST_DATABASE_URL + JOMFORM_TEST_VALKEY_URL
   ./scripts/preflight.sh    # 8 gates, ~6 min; runs again inside build-and-ship.sh
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

## Prerequisite

The api image Dockerfile (`api/Dockerfile`) is being added by the Dockerfile
issue. Until it lands, the script exits with a clear error pointing at it.
When it lands it should declare `ARG VERSION`, `ARG COMMIT`, `ARG BUILD_DATE`
and wire them into the `go build -ldflags "-X ...version.Version=$VERSION -X
...version.Commit=$COMMIT"` line; the OCI labels set by this script already
carry provenance regardless.