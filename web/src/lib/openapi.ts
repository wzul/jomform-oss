// The /docs/api page's data layer.
//
// The endpoint reference on that page is rendered from OPENAPI_SPEC — the
// generated copy of the contract in api/internal/httpapi/openapi_routes.go —
// rather than written out by hand. That is the whole point: a hand-written
// endpoint list is a document that is wrong the moment a route changes, and
// nothing in this repo would notice (the same failure that produced the t_d768342e
// docs card). Here the page and the API's own route table are pinned to one
// registry by the api test suite.
//
// This module only SHAPES that data for rendering; it does not invent anything.
// docsApi.test.ts asserts the shaping is faithful and that the page actually uses
// it.

import { OPENAPI_SPEC } from "./openapiSpec.gen.ts";
import { API_EXAMPLES } from "./apiExamples.gen.ts";

export const API_BASE = "https://api.jomform.com";

/** One operation, as the reference table renders it. */
export type Endpoint = {
  method: string;
  path: string;
  summary: string;
  tag: string;
  /** read | write | admin | session — the gate the API's code enforces. */
  access: string;
  /** "" when the route has NO rate limit, which is the majority case. */
  rateLimit: string;
  description: string;
};

type SpecOperation = {
  operationId?: string;
  summary?: string;
  description?: string;
  tags?: string[];
  "x-jomform-access"?: string;
  "x-jomform-rate-limit"?: { scope?: string; policy?: string };
};

const METHOD_ORDER = ["GET", "POST", "PUT", "PATCH", "DELETE"];

/** Every operation in the contract, flat, ordered by path then method. */
export function specEndpoints(): Endpoint[] {
  const paths = (OPENAPI_SPEC as { paths?: Record<string, Record<string, SpecOperation>> }).paths ?? {};
  const out: Endpoint[] = [];
  for (const path of Object.keys(paths).sort()) {
    const item = paths[path];
    const methods = Object.keys(item).sort(
      (a, b) => METHOD_ORDER.indexOf(a.toUpperCase()) - METHOD_ORDER.indexOf(b.toUpperCase()),
    );
    for (const method of methods) {
      const op = item[method];
      out.push({
        method: method.toUpperCase(),
        path,
        summary: op.summary ?? "",
        tag: op.tags?.[0] ?? "Other",
        access: op["x-jomform-access"] ?? "read",
        rateLimit: op["x-jomform-rate-limit"]?.policy ?? "",
        description: op.description ?? "",
      });
    }
  }
  return out;
}

/** The endpoints grouped by tag, in the tag order the contract declares. */
export function endpointsByTag(): { tag: string; endpoints: Endpoint[] }[] {
  const declared = ((OPENAPI_SPEC as { tags?: { name: string }[] }).tags ?? []).map((t) => t.name);
  const all = specEndpoints();
  const groups = new Map<string, Endpoint[]>();
  for (const e of all) {
    const list = groups.get(e.tag) ?? [];
    list.push(e);
    groups.set(e.tag, list);
  }
  // Declared tags first (the document's own order), then any tag the document
  // forgot to declare — silently dropping it would hide endpoints from the page.
  const order = [...declared.filter((t) => groups.has(t)), ...[...groups.keys()].filter((t) => !declared.includes(t))];
  return order.map((tag) => ({ tag, endpoints: groups.get(tag) ?? [] }));
}

/** Only the routes that ARE rate limited — the honest, short list. */
export function rateLimitedEndpoints(): Endpoint[] {
  return specEndpoints().filter((e) => e.rateLimit !== "");
}

/**
 * The READ routes whose documented access is `write` — the reads a readonly key
 * is refused (403) because the handler applies a write gate anyway. The page's
 * Scopes section used to enumerate these by hand, and the hand-written list was
 * incomplete: it offered the form email-settings and notification-rule routes as
 * the whole set while GET /v1/rest/settings/webhook-key was also gated as a
 * write. Deriving the list from the contract removes the drift, the same way the
 * operation count does.
 */
export function writeGatedReads(): Endpoint[] {
  return specEndpoints().filter((e) => e.method === "GET" && e.access === "write");
}

/** Total operation count, so the page can state the true size of the API. */
export function endpointCount(): number {
  return specEndpoints().length;
}

/** The access levels present in the contract, so the page documents what exists. */
export function accessLevels(): string[] {
  return [...new Set(specEndpoints().map((e) => e.access))].sort();
}

/** The worked examples, as the page renders them. */
export const apiExamples = API_EXAMPLES.examples;
