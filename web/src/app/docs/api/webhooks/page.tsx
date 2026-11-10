import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";
import { endpointsByTag } from "../../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "API webhooks — JomForm Docs",
  description:
    "JomForm webhooks: the events, the HMAC and Ed25519 signatures to verify, the delivery log and the retry routes — with the webhook endpoints from the contract.",
  path: "/docs/api/webhooks",
});

// Card t_0356ed4c: /docs/api had the webhook routes buried in a 19-tag table and
// nothing about what a receiver has to DO with them. This page is the task page:
// events, the two signature headers, the delivery log and the retry route, then
// the contract's own Webhooks rows (generated, never typed).
//
// The header names are the ones api/internal/httpapi/dispatch2.go sets
// (X-JomForm-Event, X-JomForm-Signature: sha256=<hex>, X-JomForm-Signature-Ed25519).
// The Ed25519 public key comes from the get_webhook_signing_key tool / the
// settings webhook-key route; that is why the page names it.

const WEBHOOK_BLOCKS: Section["body"] = endpointsByTag()
  .filter((group) => group.tag === "Webhooks")
  .flatMap((group) => [
    { heading: `${group.tag}` },
    {
      table: {
        label: `Endpoints: ${group.tag}`,
        head: ["Method", "Path", "Access", "What it does"],
        rows: group.endpoints.map((e) => [
          `${e.method}`,
          `\`${e.path}\``,
          `${e.access}`,
          `${e.summary}${e.description ? ` ${e.description}` : ""}`,
        ]),
      },
    },
  ]);

const SECTIONS: Section[] = [
  {
    id: "webhooks",
    title: "Webhooks",
    icon: "🪝",
    body: [
      "A webhook posts a **JSON body** to your URL when an event happens. The body is the sale, response or subscription the API returns.",
      {
        list: [
          "`sale.paid` fires when a payment is confirmed.",
          "`sale.failed` fires when a payment fails.",
          "`subscription.charged` fires on a recurring charge.",
          "`*` subscribes to every event above.",
        ],
      },
      { heading: "Verify the signature" },
      "Every request carries **X-JomForm-Event** and an HMAC-SHA256 `X-JomForm-Signature` header over the raw body.",
      "An account with an **Ed25519 key** also gets `X-JomForm-Signature-Ed25519`, verified with the public key from `get_webhook_signing_key`.",
      "Deliveries are logged with their **status code**, attempt number and error. `GET /v1/rest/webhooks/{id}/deliveries` lists them.",
      "Re-send a failed delivery with its retry route; it is queued for the next sweep. A scheduled retry has its own route.",
      ...WEBHOOK_BLOCKS,
    ],
  },
];

export default function ApiWebhooksPage() {
  return (
    <DocsPage
      title="Webhooks"
      lead="The events JomForm sends, how to verify a delivery, and how to re-send one that failed."
      sections={SECTIONS}
      jumpTo
    />
  );
}
