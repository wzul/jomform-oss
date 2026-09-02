// Shared helpers for JomForm k6 load tests.
//
// Env overrides (all optional):
//   BASE_URL   target base URL (default http://localhost:8080)
//   VUS        virtual users (default 50)
//   DURATION   steady-state duration (default 30s)
//   RAMP       ramp-up duration (default 10s)
//   FORM_SLUG  public form slug (default "demo")
//   SPACE      space subdomain for public form (default "demo")
//   API_KEY    Bearer API key for authed dashboard endpoints
//   THRESH_P95 p95 latency threshold in ms (default 500)
//   THRESH_ERR error-rate threshold as fraction (default 0.01)
import http from "k6/http";
import { check } from "k6";

export const BASE_URL = (__ENV.BASE_URL || "http://localhost:8080").replace(/\/$/, "");
export const VUS = Number(__ENV.VUS || 50);
export const DURATION = __ENV.DURATION || "30s";
export const RAMP = __ENV.RAMP || "10s";
export const FORM_SLUG = __ENV.FORM_SLUG || "demo";
export const SPACE = __ENV.SPACE || "demo";
export const API_KEY = __ENV.API_KEY || "";
export const THRESH_P95 = Number(__ENV.THRESH_P95 || 500);
export const THRESH_ERR = Number(__ENV.THRESH_ERR || 0.01);

// authHeaders returns the Authorization header for authed endpoints.
export function authHeaders() {
  const h = { "Content-Type": "application/json" };
  if (API_KEY) h["Authorization"] = "Bearer " + API_KEY;
  return h;
}

// publicFormURL builds the buyer-facing form URL on the space subdomain.
// Public forms are only served on a space subdomain or custom domain
// (issue #77), so the host must carry the space prefix.
export function publicFormURL() {
  const u = new URL(BASE_URL);
  const host = u.hostname.startsWith(SPACE + ".") ? u.hostname : SPACE + "." + u.hostname;
  u.hostname = host;
  u.pathname = "/" + FORM_SLUG;
  return u.toString();
}

// publicSubmitURL builds the buyer submit endpoint for the space subdomain.
export function publicSubmitURL() {
  const u = new URL(BASE_URL);
  const host = u.hostname.startsWith(SPACE + ".") ? u.hostname : SPACE + "." + u.hostname;
  u.hostname = host;
  u.pathname = "/" + FORM_SLUG + "/order";
  return u.toString();
}

// randomEmail produces a unique buyer email per iteration so the submit path
// exercises a fresh sale insert each time.
export function randomEmail() {
  return "buyer+" + Date.now() + "-" + Math.floor(Math.random() * 1e6) + "@example.com";
}

// checkStatus is a shared response assertion helper.
export function checkStatus(res, expected, tag) {
  return check(res, {
    [tag + " status " + expected]: (r) => r.status === expected,
  });
}
