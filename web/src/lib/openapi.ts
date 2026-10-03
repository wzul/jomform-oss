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

/**
 * The publicly reachable URL of the OpenAPI contract — the one the /docs/api
 * page links to from two places.
 *
 * It is a URL on jomform.com, not a github.com blob, because the repository is
 * PRIVATE: a link to `api/openapi/openapi.json` on github.com answers 404 for an
 * anonymous reader, and the page's stated audience is partly third-party
 * developers with no repository access. The bytes at this URL are the same
 * document — the web app serves `web/public/openapi.json`, which the generator
 * writes from the same OpenAPISpecJSON() call as the repository copy, and
 * TestGeneratedArtifactsAreInSync fails if either copy is stale or if they
 * differ.
 *
 * Kept here (not typed into the page) so the page has ONE definition of where
 * the contract lives, and so docsApi.test.ts can assert the link resolves to a
 * host a reader can actually reach.
 */
export const OPENAPI_PUBLIC_URL = "https://jomform.com/openapi.json";

/** One operation, as the reference table renders it. */
export type Endpoint = {
  method: string;
  path: string;
  summary: string;
  tag: string;
  /** read | write | admin | session — the gate the API's code enforces. */
  access: string;
  /**
   * True when Access=write applies the KEY SCOPE ONLY — the handler calls
   * requireWriteScope and never consults a ?workspace= role (card t_4abd052a).
   * The legend in /docs/api is a definition of the LEVEL, rendered once and
   * applied to every write-labelled route, so without this the legend promised
   * "manage/admin role inside a ?workspace= scope" for the one route that does
   * not apply that axis. Published by the contract, not typed into the page.
   */
  scopeOnly: boolean;
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
  "x-jomform-scope-only"?: boolean;
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
        scopeOnly: op["x-jomform-scope-only"] === true,
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

/**
 * The routes whose documented access is `write` but whose gate is the KEY SCOPE
 * ALONE — the routes the legend's "manage/admin role inside a ?workspace= scope"
 * does NOT describe (card t_4abd052a).
 *
 * Derived from the contract, so the caveat cannot outrun the gate: the registry
 * marks a route `ScopeOnly`, that reaches the published operation as
 * `x-jomform-scope-only`, and this reads it. A route that gains or loses the role
 * axis changes this set by changing that one field.
 */
export function scopeOnlyWriteRoutes(): Endpoint[] {
  return specEndpoints().filter((e) => e.access === "write" && e.scopeOnly);
}

/** The worked examples, as the page renders them. */
export const apiExamples = API_EXAMPLES.examples;
