# Auth & Payment Pen-Test Checklist

Manual security review checklist for the JomForm auth and payment surface.
Run after any major auth/payment change and before any public release
(see issue #74). Each item: **test** → **expected** → **result** → **fix/decision**.

Automated gates (govulncheck, npm audit, gitleaks) run in CI — see
`.github/workflows/ci.yml`. This checklist covers what automation cannot.

---

## 1. Brute-force / rate-limit bypass

| # | Test | Expected | Result | Fix/Decision |
|---|------|----------|--------|--------------|
| 1.1 | Hammer the login endpoint from one IP past the burst. | 429 + `Retry-After`; no more attempts accepted. | | |
| 1.2 | Rotate source IPs (proxy pool) to bypass the per-IP limiter. | Rate limit is per-IP only — **known limitation**; consider account-level or global throttling for login. | | |
| 1.3 | **Header spoofing of `CF-Connecting-IP` / `X-Real-IP` / `X-Forwarded-For` via a direct request (not through Cloudflare).** | `middleware/realip.go` only trusts these headers when the immediate peer is a Cloudflare CIDR (`trusted()`). A direct request's spoofed header must be **ignored** — the socket peer is used. Verify `ClientIP()` (ratelimit.go) cannot be tricked by a direct request. | | |
| 1.4 | Send `CF-Connecting-IP` with a non-IP value / malformed IP. | `net.ParseIP` rejects it; falls back to socket peer. | | |
| 1.5 | Registration endpoint abuse (open signup). | `RegistrationLimit` (burst 5, 1/hour refill) throttles per IP. | | |
| 1.6 | Email 2FA code endpoint (`/v1/auth/2fa/email-code`). | Tight per-IP limit (3/hour) — verify it cannot be bypassed by IP rotation. | | |
| 1.7 | Magic-link request endpoint (`/v1/auth/magic-link/request`). | Per-IP limit (1/hour, burst 2). | | |

## 2. 2FA bypass paths

| # | Test | Expected | Result | Fix/Decision |
|---|------|----------|--------|--------------|
| 2.1 | **Magic link + 2FA interaction (issue #73):** request a magic link for a user with 2FA enabled. | Magic-link verify must still require the TOTP/email-OTP step — a magic link must NOT bypass 2FA. | | |
| 2.2 | **Email OTP (issue #72):** brute-force the 6-digit email code. | Per-IP limit (3/hour) + code single-use + short expiry. Verify no timing/entropy weakness. | | |
| 2.3 | **Trust cookie (issue #63):** steal/replay the `jf_trust` cookie. | Cookie is single-purpose (waives TOTP only), bound to user ID, 30-day expiry, `HttpOnly`/`Secure`/`SameSite`. Password is still required — trust cookie alone must NOT authenticate. | | |
| 2.4 | TOTP code reuse / replay. | TOTP codes single-use; clock-skew window bounded. | | |
| 2.5 | Disable-2FA flow without re-auth. | Disabling 2FA must require the current password / a fresh 2FA challenge. On a magic-link session for a 2FA user, a fresh TOTP code is also required (issue #458). | | |
| 2.6 | Abandoned TOTP setup (secret set, `totp_enabled=false`). | Must NOT be held at a 2FA challenge (auth.go guards on `TOTPEnabled`). | | |
| 2.7 | **Magic-link session step-up (issue #458):** sign in via magic link as a 2FA user, then call `change-password` / `2fa/disable` / `passkeys/register/start` without a `totp_code`. | Each returns 401 with `next_step=totp_stepup_required`; with a fresh TOTP code the action proceeds. Non-2FA users and normal (non-magic) sessions are unchanged. | | |

## 3. Payment

| # | Test | Expected | Result | Fix/Decision |
|---|------|----------|--------|--------------|
| 3.1 | **Price tampering:** submit a client-supplied price/amount. | Server must derive price from the product record (sen), never trust the client. Verify `toCHIPProducts` uses `it.PriceSen` from DB. | | |
| 3.2 | **Quantity abuse:** submit negative / zero / huge quantity. | Rejected or clamped; total = price × qty computed server-side. | | |
| 3.3 | **Webhook replay / idempotency:** replay a CHIP `sale.paid` event. | Idempotent on `(chip_purchase_id, event_type)` — a replayed event must not double-activate or double-credit. | | |
| 3.4 | **`skip_capture` abuse:** call capture/release on a non-authorized sale. | `CaptureSale`/`ReleaseSale` reject when `!sale.SkipCapture` or `ChipPurchaseID == ""`. | | |
| 3.5 | Webhook signature verification (inbound CHIP callbacks). | HMAC/ed25519 signature verified before processing; replay window bounded. | | |
| 3.6 | Subscription recurring charge double-charge. | `run_due_charges` idempotent; no duplicate charge per period. | | |

## 4. SSRF

| # | Test | Expected | Result | Fix/Decision |
|---|------|----------|--------|--------------|
| 4.1 | **Webhook URL (issue #45):** create a webhook pointing at `http://169.254.169.254/`, `http://127.0.0.1/`, `http://localhost/`, internal IPs, or a redirect chain to them. | `createWebhook` only checks `strings.HasPrefix(url, "http")` — **no private-IP/SSRF guard on the outbound dispatcher** (`dispatch2.go`). **KNOWN GAP:** add a private/loopback/link-local IP blocklist (and DNS-rebind protection) before the repo goes public. | | |
| 4.2 | **Image URL fetch:** any server-side fetch of a user-supplied image URL. | Image URLs (`LogoURL`, block `Src`) are rendered **client-side** (`<img src>`), not fetched by the server — no SSRF surface. Re-verify if a server-side image proxy is ever added. | | |
| 4.3 | OIDC `UserURL`/`TokenURL` (social.go). | These are fixed provider URLs from config, not user-supplied — confirm no user input reaches them. | | |

## 5. Dependency hygiene

- [ ] `go mod tidy` clean (no diff) — verified.
- [ ] `web/package-lock.json` committed.
- [ ] No scan step uses `|| true` or `continue-on-error` in CI.

---

## How to run

```bash
# Go vuln scan (fails on known vulns).
# GOTOOLCHAIN pin (issue #368): govulncheck@latest declares go 1.26, so the go
# command would build the scanner with go1.26.8 and fail on this go1.27 module.
cd api && GOTOOLCHAIN=$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Web vuln scan (fails on high/critical)
cd web && npm audit --audit-level=high

# Secrets scan (full history)
gitleaks detect --source . --log-opts="--all"
```

## Sign-off

| Date | Reviewer | Result | Notes |
|------|----------|--------|-------|
|      |          |        |       |
