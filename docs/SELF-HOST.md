# Self-hosting JomForm

JomForm is MIT-licensed and runs on your own machine. This page is the whole
installation: one command, one env file, no edits to the code.

If you would rather not run a server, the hosted service at the project's own
domain is the same software.

---

## 1. What you need

| | |
|---|---|
| **Docker** | Docker Engine 24+ with the **Compose v2 plugin** (`docker compose`, not `docker-compose`) |
| **RAM** | 2 GB minimum (the Next.js build is the peak); 4 GB is comfortable |
| **Disk** | 10 GB to start. Media, backups and the database grow from there |
| **A hostname** | e.g. `forms.example.com`, with an **A/AAAA record** pointing at the server. Passkeys and session cookies are bound to this host, so an IP address is not a substitute |
| **Ports** | 80 and 443 reachable from the internet — Caddy obtains the TLS certificate through them |

No separate Postgres, Valkey, S3 or Node install: the stack ships all of it.

## 2. Install

```bash
git clone https://github.com/wzul/jomform.git
cd jomform
bash deploy/selfhost/bootstrap.sh --base-url https://forms.example.com
```

That single command:

1. copies `deploy/.env.example` to `deploy/.env` (mode `0600`),
2. generates every secret with `openssl rand -hex 32` — the Postgres password,
   the S3 secret key and `ENCRYPTION_KEY`,
3. derives the front door from `--base-url`: the Caddy site, the published
   ports, and `APP_ENV` (`https` → `prod`, `http` → `dev`),
4. **refuses to start** if any secret or `APP_BASE_URL` is still a placeholder,
5. builds the API and web images, starts the stack, and waits for health.

The first build compiles Go and Next.js; several minutes is normal. When it
returns, the URL it prints is live.

### Trying it on your laptop first

```bash
bash deploy/selfhost/bootstrap.sh --base-url http://localhost:8080
```

`http://` means no TLS, so the bootstrap sets `APP_ENV=dev` (a `Secure` cookie is
never sent back over plain HTTP — with `prod` the login would appear to work and
then be forgotten). Passkeys do not work over plain HTTP except on `localhost`,
which is why the trial form is `localhost`.

### What the bootstrap will not do for you

- **It will not invent your hostname.** `--base-url` is the origin users load.
  It is also the passkey relying party (the RP ID is its host), so a wrong value
  is a deployment whose passkeys can never work.
- **It will not rewrite an existing `deploy/.env`.** A second run validates the
  file and starts the stack; it never rotates a secret. Rotating
  `ENCRYPTION_KEY` makes every stored payment credential unreadable.

## 3. The first account

Signup is email-verified, and the sender is **AWS SES** (the SESv2 API — not a
plain SMTP relay), so a fresh install has no way to mail the confirmation link.
Until you configure SES the API writes that link to its own log:

```bash
docker compose -f deploy/docker-compose.yml logs api \
  | grep -o 'http[^ ]*/v1/auth/verify?token=[^ "]*'
```

Open it (valid 24 hours), sign in, create a workspace, then create and publish a
form. Your published form is served by this install at
`https://your-host/f/<slug>` — open it in a private window and submit it, which
is a buyer's path end to end.

> **Known limitation.** The builder's "Copy link" button still shows the hosted
> product's tenant-hostname shape — `<workspace>.<product-domain>/<slug>`, which
> a self-hosted install does not serve. On a self-hosted install the working
> address is the one above (`{APP_BASE_URL}/f/<slug>`), served by this stack's
> front door. The printed link is tracked separately; use the `/f/` form until
> it is configurable.
>
> Serving from your own origin is switched on by `JOMFORM_SELF_HOSTED=1`, which
> the bootstrap writes into `deploy/.env` and this compose passes to the API — it
> is the install's declaration that it has ONE hostname, so it is off by default
> and the hosted product never sets it. Set it to `0` if you put several tenants
> behind one shared origin.

When you have SES (`SES_SMTP_HOST`, `SES_SMTP_USER`, `SES_SMTP_PASS`,
`SES_FROM` in `deploy/.env`), set `JOMFORM_LOG_VERIFY_LINK=0` so links stop
appearing in your logs and arrive by email instead. Password reset and magic-link
login need SES too.

## 4. TLS

The stack ships **Caddy** as its front door and publishes only ports 80 and 443.
With an `https://` base URL Caddy obtains and renews the certificate itself; there
is nothing to install and no certificate to paste. Keep the DNS record in place —
renewal uses it.

If you already terminate TLS elsewhere (a load balancer, an existing nginx,
Cloudflare Tunnel), point it at the `caddy` service's host port and keep the same
path split it performs — see `deploy/selfhost/Caddyfile`. An nginx equivalent is
three `location` blocks: `/f/`, `/t/`, `/v1/`, `/media/`, `/embed/`, `/oauth/`,
`/.well-known/oauth-` and `/healthz` → the API (`api:8080`); everything else,
including `/api/`, → the web app (`web:3000`). The `/api/` case matters: that is
the dashboard's own proxy to the API, and it is what keeps the session cookie
first-party.

## 5. Backups

Two things must be backed up **together**; one without the other is not a
restore.

1. **The database** — `docker compose -f deploy/docker-compose.yml exec -T postgres pg_dump -U jomform jomform | gzip > jomform-$(date +%F).sql.gz`
2. **`deploy/.env`** — it holds `ENCRYPTION_KEY`, which decrypts the merchant
   payment credentials stored in the database. A dump restored without it comes
   back with credentials nobody can read.

Uploaded media lives in the `jomform_s3data` volume; copy it with
`docker run --rm -v jomform_s3data:/data -v "$PWD":/out alpine tar czf /out/media-$(date +%F).tar.gz -C /data .`
or, more simply, keep volume snapshots alongside the dumps. Test a
restore before you need one.

## 6. Operating it

```bash
docker compose -f deploy/docker-compose.yml ps          # what is running, and healthy?
docker compose -f deploy/docker-compose.yml logs -f api # API log (verification links land here)
docker compose -f deploy/docker-compose.yml down        # stop (volumes survive)
git pull && bash deploy/selfhost/bootstrap.sh           # upgrade in place
```

The stack is:

| Service | Role |
|---|---|
| `caddy` | the only published service: TLS + the path split between web and api |
| `web` | the dashboard and form builder (Next.js standalone) |
| `api` | REST, MCP, public form pages, media |
| `postgres` | data |
| `valkey` | sessions, rate limits, background queue |
| `s3` | uploaded media, an S3-compatible SeaweedFS server |

Postgres, Valkey and the S3 server are **not** published to the host — nothing but the
front door is reachable from outside, which is why Valkey needs no password.

## 7. Troubleshooting

**`bootstrap: REFUSED — placeholder secret(s) in …/deploy/.env`**
The env file existed already, so it was not generated over. Replace each named
key with `openssl rand -hex 32`, or delete `deploy/.env` and re-run to have them
generated. Never keep a key that appears in this repository.

**`docker compose config` fails with `… is required — run deploy/selfhost/bootstrap.sh`**
A secret the compose needs is unset. Run the bootstrap (it fills it in) rather
than `docker compose up` by hand.

**The stack is healthy but the page does not load.**
Check the front door: `docker compose logs caddy`. A certificate needs the DNS
record to point here and port 80 to be reachable.

**Login succeeds and then I am logged out.**
The origin in `APP_BASE_URL` is not the origin you are loading, or you are on
plain HTTP with `APP_ENV=prod`. Re-run the bootstrap with the real `--base-url`.

**Passkeys refuse to register.**
`APP_BASE_URL` must be the host in your address bar: the passkey RP ID is its
host, and a browser refuses an RP ID that is not the page's own origin.

**`docker compose` fails with `Bind for 0.0.0.0:443 failed: port is already allocated`.**
Another web server owns the port. Set `HTTP_PORT` / `HTTPS_PORT` in
`deploy/.env` before the first run (the bootstrap keeps values you set there and
only overrides them when `--base-url` names a port itself), or put your own proxy
in front and let it own 80/443 — see § 4.

## 8. Licence

MIT. See `LICENSE`. The hosted service is a separate offering; the licence covers
this code, not that instance.
