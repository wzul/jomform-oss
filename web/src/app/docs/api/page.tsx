import type { Metadata } from "next";
import { docsMetadata } from "../../../lib/docsMetadata";
import { DocsPage, type Section } from "../docsProse";
import {
  API_BASE,
  OPENAPI_PUBLIC_URL,
  accessLevels,
  apiExamples,
  endpointCount,
  endpointsByTag,
  rateLimitedEndpoints,
  scopeOnlyWriteRoutes,
  writeGatedReads,
} from "../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "REST API reference — JomForm Docs",
  description:
    "The JomForm REST API: create a key, make your first call, and the full endpoint reference. Server-to-server only — the API sends no CORS headers and does not answer browser preflight requests.",
  path: "/docs/api",
});

// The REST API reference, rewritten for the /docs prose renderer.
//
// PROSE IS SHORT AND THE DATA IS THE CONTRACT. The operation count, the endpoint
// rows, the access legend, the throttled list, the write-gated reads and the six
// worked examples all come from lib/openapi.ts — the shaping layer over the
// generated contract — so this page cannot list an operation the API does not
// serve, and cannot silently miss one it does. The api suite pins the contract
// to the real route table (openapi_parity_test.go) and executes every example
// (openapi_examples_test.go).
//
// THE SECTION IDS ARE A CONTRACT with /docs and with the dashboard's inline "?"
// links, so the nine anchors are unchanged: quickstart, auth, scopes, errors,
// limits, cors, endpoints, versioning, testing.

const total = endpointCount();
const limited = rateLimitedEndpoints();

// The one-line definition of each access level, applied to every row carrying it.
const ACCESS_LABELS: Record<string, string> = {
  read: "Any authenticated key (readonly or readwrite)",
  write: "readwrite key; manage/admin role inside a ?workspace= scope",
  admin: "readwrite key; admin role in the scoped workspace",
  session: "Browser session only, and an API key gets 401",
};

// The legend is a DEFINITION OF THE LEVEL, so the caveat has to be rendered from
// the contract too: GET /v1/rest/settings/webhook-key is gated by the key scope
// alone, and the "write" label promises a ?workspace= role as well.
const ACCESS_LEGEND: string[] = accessLevels().map((level) => {
  const label = `**${level}**: ${ACCESS_LABELS[level] ?? ""}.`;
  if (level !== "write" || scopeOnlyWriteRoutes().length === 0) return label;
  const routes = scopeOnlyWriteRoutes()
    .map((e) => `${e.method} ${e.path}`)
    .join(", ");
  return `${label} **Not every write route:** ${routes} applies the key scope only and reads no \`?workspace=\` role.`;
});

// The reads a readonly key is refused, straight from the contract.
const WRITE_GATED_READS: string[] = writeGatedReads().map(
  (e) => `**${e.method}** \`${e.path}\` — ${e.summary}`,
);

// Only the routes that are throttled, with the policy the contract declares.
const THROTTLED_ROUTES: string[] = limited.map(
  (e) => `**${e.method}** \`${e.path}\` — ${e.rateLimit} (${e.summary})`,
);

// One heading and one table per tag, so the reference stays scannable. The rows
// are generated: a hand-typed row is the defect this page exists to prevent.
const ENDPOINT_BLOCKS: Section["body"] = endpointsByTag().flatMap((group) => [
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

// The six worked examples. Each is executed against a live server by the api
// suite, so the status and the body fragment below are the ones it asserts.
const EXAMPLE_BLOCKS: Section["body"] = apiExamples.flatMap((e) => [
  { heading: `${e.title}` },
  {
    code: `${e.curl}`,
    caption: `${e.method} ${e.path}. Expect HTTP ${e.expectStatus}, with ${e.expectContains} in the body.`,
  },
  `${e.why}`,
  `${e.note}`,
]);

const SECTIONS: Section[] = [
  {
    id: "quickstart",
    title: "Start in 5 minutes",
    icon: "🚀",
    body: [
      "Create a key in **Settings → API keys**. Choose **readwrite** to change anything, **readonly** to read.",
      {
        steps: [
          "The raw key is shown **once**, so copy it when it appears. You can also mint one with `POST /v1/rest/keys`.",
          "Export it in your shell. The API never takes a key in the query string.",
          "Make your first call. Every example below is executed against a live server.",
        ],
      },
      {
        code: 'export JOMFORM_API_KEY="jfk_your_key_here"',
        caption: "A placeholder. This page never prints a real key.",
      },
      "One run covers the flow: authenticate, create a form, publish it, read it back, read the records it made.",
      ...EXAMPLE_BLOCKS,
    ],
  },
  {
    id: "auth",
    title: "Authentication",
    icon: "🔑",
    body: [
      `Send the key as a **bearer token** to \`${API_BASE}\` on every call.`,
      { code: "Authorization: Bearer $JOMFORM_API_KEY" },
      "A key belongs to a **workspace**, not a person. Everything it reads and writes is that workspace's data.",
      "The dashboard also accepts a browser session cookie on these routes. A script should use a key.",
      "**The same key drives the MCP server.** One credential covers `/v1/rest/*` and MCP, so see the [MCP tool reference](/docs/mcp).",
      "A missing or revoked key gets **401** before any workspace is resolved, so a broken client fails loudly.",
    ],
  },
  {
    id: "scopes",
    title: "Scopes",
    icon: "🎫",
    body: [
      "A key carries a **scope**, independent of who created it. It decides what the key may do at all.",
      {
        list: [
          "**readonly** is read only. Every write is refused with **403**.",
          `It is also refused on the reads whose handler writes anyway: ${writeGatedReads().length} routes, listed next.`,
          "**readwrite** reads and writes.",
        ],
      },
      { list: WRITE_GATED_READS },
      "Inside a `?workspace=<slug>` scope there is a second axis: your **role in that workspace**. A write needs both the key and the role.",
      "The payment-credential routes need **admin**. Scope belongs to the credential; role belongs to the person.",
      { note: "The endpoint reference states, per row, which gate applies." },
    ],
  },
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
    id: "cors",
    title: "No CORS, server-to-server only",
    icon: "🚫",
    body: [
      "This API is built for **server-to-server** calls. It sends **no CORS headers at all**.",
      "There is no `Access-Control-Allow-Origin` and no `Access-Control-Allow-Methods`. A preflight `OPTIONS` is not answered with them.",
      "A `fetch()` from another origin is blocked by the browser, and the request may not even be sent.",
      "That is **deliberate**. Call the API from your own backend, where the key stays out of the browser.",
      "An API key shipped in client-side JavaScript is a key you have published.",
      { tip: "If your front end needs data, have it call your server, and let your server call JomForm." },
    ],
  },
  {
    id: "endpoints",
    title: "Endpoint reference",
    icon: "📚",
    body: [
      "**Every route** on this page is real: the rows are generated, never typed.",
      `All ${total} operations, from the **OpenAPI 3.1** contract. The **Access** column is the gate the code enforces.`,
      { list: ACCESS_LEGEND },
      ...ENDPOINT_BLOCKS,
      {
        note: "Each table carries the route, its access gate and its summary. The published contract carries every request and response schema.",
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
  {
    id: "testing",
    title: "Building and testing against the API",
    icon: "🧪",
    body: [
      "There is **no separate sandbox or test key**. A key acts on your real workspace.",
      "An integration test against production data is a test that may email a customer.",
      {
        steps: [
          "Create a throwaway workspace. The workspace switcher does it in a click, or `POST /v1/workspaces`.",
          "Point its CHIP credentials at CHIP's own sandbox if you exercise payments.",
          "Create a key scoped to that workspace.",
          "Delete the workspace when you are done.",
        ],
      },
      "A throwaway workspace you can delete is what makes testing without a sandbox safe.",
      { tip: "Use `readonly` for anything that only reads. It cannot damage real data even if your harness has a bug." },
    ],
  },
];

export default function ApiDocsPage() {
  return (
    <DocsPage
      title="REST API"
      lead={`Create a key, make your first call, then look up all ${total} operations. Server-to-server only.`}
      sections={SECTIONS}
    />
  );
}
