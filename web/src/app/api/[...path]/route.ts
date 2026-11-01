import { NextRequest, NextResponse } from "next/server";

import { forwardClientAddress } from "../../../lib/clientAddress";
import { relayResponseHeaders } from "../../../lib/proxyResponseHeaders";

// Dashboard proxies /api/* to the Go API /v1/* so session cookies stay
// same-origin. /api/auth/login -> {API}/v1/auth/login etc.
const API = process.env.JOMFORM_API_URL || "http://localhost:8082";

function targetOf(req: NextRequest) {
  const url = new URL(req.url);
  const sub = url.pathname.replace(/^\/api/, "");
  return { path: API + "/v1" + sub + url.search };
}

async function proxy(req: NextRequest) {
  const { path } = targetOf(req);
  const headers = new Headers();
  const auth = req.headers.get("authorization");
  if (auth) headers.set("Authorization", auth);
  const cookie = req.headers.get("cookie");
  if (cookie) headers.set("Cookie", cookie);

  // Forward the BROWSER's host. This proxy replaces Host with the API's own
  // address (JOMFORM_API_URL, e.g. http://api:8080), and the Go API derives the
  // passkey (WebAuthn) relying party from the request host — reading Host here
  // refused the product's own endpoints (/api/auth/passkeys/{login,register}/start
  // answered 403 for a browser on jomform.com, since the API saw `api:8080`).
  // The API validates this value against the app's own domain either way, so it
  // can only ever select among the hosts this deployment already serves.
  // `host` is the name the client connected to (the browser sets it and Traefik
  // forwards it unchanged); the forwarded variant is only a fallback for a chain
  // that has already rewritten it.
  const browserHost =
    req.headers.get("host") ||
    req.headers.get("x-forwarded-host")?.split(",")[0]?.trim() ||
    new URL(req.url).host;
  if (browserHost) headers.set("X-Forwarded-Host", browserHost);

  // Relay the CLIENT ADDRESS the connection arrived with (card t_ae8791f9). Every
  // dashboard request reaches the API from this container, so without these the
  // API's client address — the key of its per-IP rate limit and of the login
  // lockout — is the container: one bucket for every browser. See
  // web/src/lib/clientAddress.ts for the measurement, and for why relaying these
  // is safe (the API's rule in middleware/clientip.go is what decides which value
  // may be believed, and it is the only reader of them).
  forwardClientAddress(req.headers, headers);

  const init: RequestInit = { method: req.method, headers };
  if (req.method !== "GET" && req.method !== "HEAD") {
    init.body = await req.arrayBuffer();
    headers.set("Content-Type", req.headers.get("content-type") || "application/json");
  }

  // redirect: "manual" so the proxy does NOT follow 3xx responses (e.g. the
  // 302 to Google from /v1/auth/oauth/google). We forward the status + Location
  // header to the browser so it performs the redirect itself (issue #273).
  const r = await fetch(path, { ...init, redirect: "manual" });
  // Relay the response headers the browser needs, from ONE declared list
  // (lib/proxyResponseHeaders.ts): the body's Content-Type, the redirect's
  // Location, every Set-Cookie, the API's Retry-After — the header the login
  // lockout (429 + `Retry-After: 900`, auth.go) and the per-IP throttle
  // (middleware/ratelimit.go) state their backoff in, which this hop used to
  // drop, so a locked-out dashboard could not tell when its window clears — and
  // the API's Cache-Control, which the merchant's printable stand sets to
  // `private, no-store` and this hop used to drop the same way.
  // Built, not the upstream set passed through: see the module for why.
  const respHeaders = new Headers();
  relayResponseHeaders(r.headers, respHeaders);

  return new NextResponse(r.body, { status: r.status, headers: respHeaders });
}

export const dynamic = "force-dynamic";

export const GET = proxy;
export const POST = proxy;
export const PUT = proxy;
export const PATCH = proxy;
export const DELETE = proxy;