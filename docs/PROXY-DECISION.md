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
   syntax — plain regex, no Go named-group placeholders) and is the live FILE
   rather than a description of it: measured 2026-10-12 it held 55 lines in
   Traefik v2 syntax while the host loaded 179 lines of v3, so installing "the
   live pattern" on a new host would have produced different routing. It is now a
   byte-for-byte copy of what the edge loads, and `deploy/vps/drift-check.sh`
   compares the two (see `deploy/HOST-SETUP.md`).
2. **Wildcard ROUTING** for `*.jomform.com`: one `jf-wildcard@file` router sends every
   `<space>.jomform.com` host to the api with no per-domain config. That is not the same
   as wildcard *TLS* — we have no wildcard certificate and never did; see the TLS gotcha
   below (corrected 2026-10-23, the old "wildcard TLS is already issued and working"
   claim was false).
3. **No code coupling.** The API serves plain HTTP inside the network. Swapping
   the proxy for Caddy or Go `autocert` later touches only `deploy/vps/`, never
   application code.
4. **Proven config in production** (routing matrix below). Migration cost to
   Caddy would buy no user-visible benefit right now.

## When we would revisit

- If certificate issuance per custom domain becomes painful via the file
  provider (e.g. dozens of new domains per day) → consider the Docker provider
  with labels, but only after an image bump to traefik **>= v3.6.1**, the first
  release whose Docker client auto-negotiates the API version. (traefik v3.3
  pins a client that speaks API 1.24 and Docker Engine 29 requires >= 1.44;
  pinning `DOCKER_API_VERSION` does not help, because the client's SDK refuses
  the handshake before that variable is consulted.) Or a custom ACME service
  inside the API.
- If we ever re-introduce Dokploy, its bundled Traefik resumes that role
  naturally. (Not planned — plain-server + Traefik file provider is the
  current deployment.)

## Gotchas recorded (for future edits)

- Traefik v3 `HostRegexp` does NOT accept Go named-group syntax like
  `{sub:[a-z0-9-]+}`; use plain regex `[a-z0-9-]+.jomform.com`.
- **The wildcard regex is UNANCHORED, so it also matches the reserved hosts.**
  `[a-z0-9-]+.jomform.com` matches `www.jomform.com` (the unescaped `.` takes the
  literal `w`), and the tenant wildcard's priority (20) beats the static www
  routers (priority 10). So `www` must be excluded with
  `&& !Host(`www.jomform.com`)` on **every** wildcard entry point (`jf-wildcard`
  :443 and `jf-wildcard-http` :80). This is what PR 740 fixed for `www`.
- **Whether a static host NEEDS that exclusion is a PRIORITY comparison, not a
  rule about the wildcard** (card t_3f44c437). Traefik serves the matching router
  with the highest priority — an explicit `priority:`, otherwise the rule's
  length — so `www` (static 10) is outranked by the wildcard (20) and needs the
  exclusion, while `docs` (55/60) and `mcp` (60) outrank it and correctly have
  none. Do not "fix" the file by excluding every statically routed host: it is a
  byte-for-byte copy of what the host loads (`deploy/vps/drift-check.sh` fails on
  any difference). `subdomains_test.go` compares the priorities per entry point
  (routers on different entry points never compete) and logs the labels where no
  exclusion is needed.
- **A host this edge routes that is NOT under jomform.com is DECLARED**, with its
  reason, in `urlguard.foreignRoutedHosts` (`api/internal/urlguard/subdomains.go`)
  — today `halo.wanzul-hosting.com`, a merchant custom domain served by
  `jf-custom-cert-halo` with its own certificate. The Traefik guard reds on an
  undeclared foreign host and refuses an entry under jomform.com (those are
  classified by label through `reservedSubdomains`).
- **The reserved serving list is ONE thing** (card t_d49dae9f):
  `urlguard.ServingReservedSubdomains` in `api/internal/urlguard/subdomains.go`.
  `accountSubdomain` (httpapi/public.go) consumes `urlguard.IsServingReserved`.
  Note what that list is FOR: which Host headers the API must not resolve to a
  workspace. The Traefik guard's `!Host(...)` check is drawn from the wider
  `reservedSubdomains` (unclaimable names) instead, because the harm an exclusion
  can do — taking a live host off a router — is only possible for a label a
  merchant could actually claim; the serving list is a strict sub-set of it
  (asserted in the same file), so no exclusion legal before becomes illegal.
  Deleting a wildcard `!Host(...)` clause, or excluding a claimable label, turns
  `go test ./internal/urlguard/` RED (wired into the `test` gate).
- `ssh`-based python heredocs into `/opt/traefik/dynamic/*.yml` are fragile —
  edit via local python + scp, then `docker compose restart`.
- **The Docker provider is REMOVED** (2026-10-20): `deploy/vps/traefik-docker-compose.yml`
  no longer passes the two docker-provider flags nor mounts the Docker socket, because
  the provider never worked here — the Go Docker SDK pinned inside `traefik:v3.3` speaks
  API 1.24 while Docker Engine 29.1.3 requires >= 1.44, so the daemon refused the
  handshake and the provider retried forever (~2 ERR lines/s, ~23 MB/day of log) while
  discovering nothing. Routing is file-provider-only (all 18 routers are `@file`) and no
  container carries a `traefik.*` label, so removing it lost nothing. Bringing label
  discovery back is a deliberate re-enable: traefik **>= v3.6.1** (the first release
  whose Docker client auto-negotiates the API version — v3.5.x and older still send
  1.24) PLUS both flags and the socket mount. The absence of those lines is not an
  accident, and `deploy/vps/drift-check.sh` compares this file byte-for-byte with the
  host's `/opt/traefik/docker-compose.yml`, so do not "tidy" it.
- **There is NO wildcard certificate for `*.jomform.com` at the origin** (measured
  2026-10-23; the doc claimed one was "already issued and working" until then).
  `/opt/traefik/acme/acme.json` holds 5 certs — `jomform.com` (+ SAN
  `www.jomform.com`), `mcp.jomform.com`, `docs.jomform.com`, `halo.wanzul-hosting.com`,
  `jftest.wanzul-hosting.com` — none of them a wildcard. The `jf-wildcard` router does
  ask for `domains: ['*.jomform.com', jomform.com]` from the `le` resolver, but that
  resolver is HTTP-01, which cannot issue a wildcard, so the request is unsatisfiable
  and Traefik falls back to its DEFAULT SELF-SIGNED cert for those hosts (SNI straight
  to `127.0.0.1:443`: `api.jomform.com` and `test-wan.jomform.com` present
  `CN = TRAEFIK DEFAULT CERT`; only `jomform.com` and `docs.jomform.com` present a real
  Let's Encrypt leaf). Visitors still get a valid padlock because Cloudflare's edge
  certificate is what the browser sees, and the edge reaches the origin over HTTP/2
  accepting that self-signed cert — i.e. the zone is **Full, not Full (strict)**.
  Switching to Full (strict) would 526 every `<space>.jomform.com` host until the
  origin has a real cert, which needs a DNS-01 challenge (HTTP-01 cannot do wildcards).
  Custom domains (`halo.`, `jftest.`) do get their own LE certs.
- `deploy/vps/traefik-jomform.yml` is a hand-maintained copy that is NOT
  installed by any deploy step (`deploy/build-and-ship.sh` only ships the image
  tarball), and prod's live `/opt/traefik/dynamic/jomform.yml` carries routers
  this file does not. Treat routing changes as prod edits, not repo edits.
- `deploy/traefik-wildcard.yml` is a Docker-provider snippet (`PathPrefix` +
  `HostRegexp`, no `!Host`) not shipped by anything; its unanchored regex would
  also shadow `www` **if it were ever activated**. It is intentionally left
  unchanged (Wan, 2026-10-04) — do not regenerate it from the file provider.
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
