import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";
import { API_BASE, writeGatedReads } from "../../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "API authentication & scopes — JomForm Docs",
  description:
    "Create a JomForm API key, make your first call, and understand key scopes, workspace roles, the missing CORS headers and how to test without a sandbox.",
  path: "/docs/api/authentication",
});

// Card t_0356ed4c: the auth half of the old /docs/api page, moved here unchanged.
// The section ids (quickstart, auth, scopes, cors, testing) are the ones the old
// page published and docsApi.test.ts pins as a contract, so an anchor written
// against the single page still resolves on the page that now owns it.

// The reads a readonly key is refused, straight from the contract.
const WRITE_GATED_READS: string[] = writeGatedReads().map(
  (e) => `**${e.method}** \`${e.path}\` — ${e.summary}`,
);

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
          "Make your first call. Every [worked example](/docs/api/endpoints#examples) is executed against a live server.",
        ],
      },
      {
        code: 'export JOMFORM_API_KEY="jfk_your_key_here"',
        caption: "A placeholder. This page never prints a real key.",
      },
      "One run covers the flow: authenticate, create a form, publish it, read it back, then read the records it made.",
    ],
  },
  {
    id: "auth",
    title: "Authentication",
    icon: "🔑",
    body: [
      `Send the key as a **bearer token** to \`${API_BASE}\` on every call.`,
      { code: "Authorization: Bearer ***" },
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

export default function ApiAuthenticationPage() {
  return (
    <DocsPage
      title="Authentication & scopes"
      lead="One bearer key, two scopes, and a workspace role on top. Plus the CORS answer nobody reads until it bites."
      sections={SECTIONS}
    />
  );
}
