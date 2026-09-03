# Proxy choice: Traefik vs Caddy (kept: Traefik)

Decision date: 2026-09-01 (after VPS migration to plain-server + Traefik routing).

## Context

JomForm now runs on a dedicated VPS (docker compose: Postgres, Valkey, MinIO, api, web)
fronted by Traefik v3 with a file provider. Question raised: why not Caddy, since the Go API
doesn't need a reverse proxy at all?

## Decision: keep Traefik

Reasons:

1. **Dynamic per-domain routing is the product.** Merchants get
   `<space>.jomform.com` subdomains and can claim custom domains with automatic
   Let's Encrypt certificates. Traefik's file provider lets us append/replace a
   small YAML snippet per domain with zero reload of the Go processes:
   `deploy/vps/traefik-jomform.yml` is the live pattern (v3 `HostRegexp`
   syntax — plain regex, no Go named-group placeholders).
2. **Wildcard TLS** for `*.jomform.com` is already issued and working.
3. **No code coupling.** The API serves plain HTTP inside the network. Swapping
   the proxy for Caddy or Go `autocert` later touches only `deploy/vps/`, never
   application code.
4. **Proven config in production** (routing matrix below). Migration cost to
   Caddy would buy no user-visible benefit right now.

## When we would revisit

- If certificate issuance per custom domain becomes painful via the file
  provider (e.g. dozens of new domains per day) → consider the Docker provider
  with labels (requires fixing the Docker API version error seen in logs:
  "client version 1.24 is too old…", i.e. pin `DOCKER_API_VERSION`) or a
  custom ACME service inside the API.
- If we ever re-introduce Dokploy, its bundled Traefik resumes that role
  naturally. (Not planned — plain-server + Traefik file provider is the
  current deployment.)

## Gotchas recorded (for future edits)

- Traefik v3 `HostRegexp` does NOT accept Go named-group syntax like
  `{sub:[a-z0-9-]+}`; use plain regex `[a-z0-9-]+.jomform.com`.
- `ssh`-based python heredocs into `/opt/traefik/dynamic/*.yml` are fragile —
  edit via local python + scp, then `docker compose restart`.
- File provider is independent of the Docker provider; the Docker provider
  currently errors on API version and can be ignored.
## Cloudflare WAF note (CF login button)

Observed 2026-09-01: `GET /api/cf/start` WITH a `jf_session` cookie returns
Cloudflare "Just a moment..." managed challenge (403) from the edge — while the
same request without the cookie passes through to the origin (401 anon) and
origin-direct returns 302 to the consent URL. Bot Fight Mode is OFF. No
`cf-mitigated` header. The zone likely has a custom WAF rule / managed ruleset
sensitivity targeting cookie-bearing requests on this path (or a Cloudflare
login-protection special rule). Action for Wan: Cloudflare dashboard →
Security → WAF → check custom rules + Security Events for
`http.request.uri.path eq "/api/cf/start"` and add a Skip rule for that path
(matching the authed cookie), or lower sensitivity for `/api/*`.
