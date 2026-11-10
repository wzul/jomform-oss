import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";
import { OPENAPI_PUBLIC_URL, endpointCount, rateLimitedEndpoints } from "../../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "API errors & limits — JomForm Docs",
  description:
    "JomForm API status codes, the exact routes that are rate limited, the 429 contract, and what /v1 stability means for an integration.",
  path: "/docs/api/errors",
});

// Card t_0356ed4c: the failure half of the old /docs/api page — status codes, the
// throttled routes, and versioning. The ids (errors, limits, versioning) moved with
// the sections, so a link to one of them still resolves on this page.

const total = endpointCount();
const limited = rateLimitedEndpoints();

// Only the routes that are throttled, with the policy the contract declares.
const THROTTLED_ROUTES: string[] = limited.map(
  (e) => `**${e.method}** \`${e.path}\` — ${e.rateLimit} (${e.summary})`,
);

const SECTIONS: Section[] = [
  {
    id: "errors",
    title: "Errors",
    icon: "⚠️",
    body: [
      "Failures use the **HTTP status** for the class and a JSON body that says what went wrong.",
      { code: '{ "error": "authentication required" }' },
      {
        list: [
          "**400**: the request is invalid. The message is written for the caller and may change wording without a version bump.",
          "**401**: missing, unknown or revoked credential.",
          "**403**: not allowed. A readonly key on a write, or no role in the `?workspace=` you asked for. The body says why.",
          "**404**: no such object in your workspace.",
          "**409**: the request conflicts with the object's state. Publishing a payment form with no products returns 409.",
          "**429**: rate limited (see below).",
          "**500**: an internal error. The detail is logged server-side and never returned.",
          "**503**: the feature needs something this deployment lacks. Retrying will not help.",
        ],
      },
      'A publish **409** carries `requires_ack: true`, because that form could never take money. Send `{"acknowledge":true}` to publish it anyway.',
      { note: "The body names the reason, and contains the word `scope` when a scope is the cause." },
    ],
  },
  {
    id: "limits",
    title: "Rate limits",
    icon: "🚦",
    body: [
      "**Most of this API has no rate limit.** Do not design a client around a uniform quota that does not exist.",
      `Exactly ${limited.length} of the ${total} operations are throttled:`,
      { list: THROTTLED_ROUTES },
      "Uploads and the AI routes are limited per client IP. The in-app assistant is limited per user and is session-only.",
      "A throttled answer is **429** and carries `Retry-After`, the seconds that client must wait for that route.",
      "Every other route, including the writes, is unmetered. Apply your own backoff and treat a **5xx** as retryable.",
      "Traffic reaches the API through **Cloudflare**, where edge protections live. No per-key quota is promised.",
      {
        warn: "Nothing meters a client that retries in a loop. Cap the retries and back off; the server will not protect you.",
      },
    ],
  },
  {
    id: "versioning",
    title: "Versioning & changelog",
    icon: "🏷️",
    body: [
      "Everything lives under `/v1`, and **/v1 is stable**: paths, request fields and response field names do not change.",
      "Additive changes may land at any time: a new endpoint, a new optional field, a new event type. Tolerate unknown fields.",
      "A change that breaks an integration means a new prefix, `/v2`, with the old one kept running.",
      "Error message **wording** is not covered. Match the status code, and the stable `code` field where one is returned.",
      "The stable fields include `requires_ack`, so match that rather than the sentence around it.",
      `Watch the published [openapi.json](${OPENAPI_PUBLIC_URL}), regenerated with every route change, and the release notes for each tag.`,
      "`GET /version` answers over HTTP. On a host you operate, `jomform-api --version` prints `version=<version> commit=<commit>` and exits.",
      {
        note: "`--version` and `--help` answer before startup: no database, cache, migration, worker or port. Safe on a live instance, including in its own container.",
      },
    ],
  },
];

export default function ApiErrorsPage() {
  return (
    <DocsPage
      title="Errors, limits & versions"
      lead="What a failure status means, which routes are actually throttled, and what /v1 promises."
      sections={SECTIONS}
    />
  );
}
