import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";
import { API_BASE, endpointCount } from "../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "REST API reference — JomForm Docs",
  description:
    "The JomForm REST API: create a key, make your first call, then the endpoint reference, webhooks and the error catalogue. Server-to-server only — the API sends no CORS headers.",
  path: "/docs/api",
});

// Card t_0356ed4c: /docs/api was 5,658 words and 27,442px tall on a phone, because
// one page carried the quickstart, the auth and scope rules, the whole generated
// endpoint reference, six worked examples and the error catalogue. It is now a HUB
// of four task pages, and every one of them is one hop away:
//
//   /docs/api/authentication   key, first call, scopes, CORS, a throwaway workspace
//   /docs/api/endpoints        every operation (generated) + the worked examples
//   /docs/api/webhooks         events, signatures, deliveries and retries
//   /docs/api/errors           status codes, rate limits, versioning
//
// NOTHING WAS DELETED: each section of the old page moved to its task page with its
// id intact (quickstart, auth, scopes, errors, limits, cors, endpoints, versioning,
// testing), so an existing anchor still resolves — on the page that now owns it.
// The two generated halves (endpointsByTag, apiExamples) moved to endpoints/page.tsx
// and still come from lib/openapi.ts, so the reference cannot list a route the API
// does not serve.

const total = endpointCount();

const SECTIONS: Section[] = [
  {
    id: "start",
    title: "Start here",
    icon: "🚀",
    body: [
      "The JomForm **REST API** drives forms, products, sales, subscriptions and webhooks from your own server.",
      `Every call goes to \`${API_BASE}\` with a **bearer API key**. A missing or revoked key gets \`401\` before any workspace is resolved.`,
      "Create the key under **Settings → API keys**: **readonly** can only read, **readwrite** can write.",
      "The API is **server-to-server only**: it sends no CORS headers, so a browser cannot call it directly.",
      {
        list: [
          "**First call** → [authentication](/docs/api/authentication): create a key, export it, make a request.",
          "**Look up an operation** → [endpoints by resource](/docs/api/endpoints): every operation, generated from the contract.",
          "**React to events** → [webhooks](/docs/api/webhooks): events, signatures, deliveries and retries.",
          "**Something failed** → [errors and limits](/docs/api/errors): status codes, throttled routes, versioning.",
        ],
      },
      {
        note: `The endpoint tables render **${total} operations** from the published OpenAPI 3.1 contract. A hand-typed row is the defect the generated reference exists to prevent.`,
      },
    ],
  },
];

export default function ApiDocsPage() {
  return (
    <DocsPage
      title="REST API"
      lead="Create a key, make your first call, then look up the operation you need. Server-to-server only."
      sections={SECTIONS}
      jumpTo
    />
  );
}
