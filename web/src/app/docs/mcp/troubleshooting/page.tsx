import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "MCP troubleshooting — JomForm Docs",
  description:
    "Why an MCP call to JomForm fails: a 401 with no discovery, an invalid scope, a read grant that cannot write, a client that rejects tools/list, or a tool that needs a user token.",
  path: "/docs/mcp/troubleshooting",
});

// Card t_0356ed4c: the fourth page of the /docs/mcp split. Nothing here is new
// behaviour — every symptom below is one the old single page already stated (the
// 401 challenge, the resource mismatch, invalid_scope, the read grant that cannot
// upgrade, the object outputSchema, feedback needing a user token). It is the same
// facts arranged as "what you see → what it means", which is how a reader uses
// them under pressure. The prose-convention and style gates still apply.

const SECTIONS: Section[] = [
  {
    id: "troubleshooting",
    title: "When a call fails",
    icon: "🩺",
    body: [
      "**Start with the status code.** Each row below is a condition the server refuses, and what to change.",
      {
        table: {
          label: "MCP failures and their cause",
          head: ["You see", "What it means"],
          rows: [
            ["`401` with `WWW-Authenticate`", "No token yet. Let the client follow the discovery flow on [OAuth & tokens](/docs/mcp/oauth)."],
            ["`401` after a sign-in", "The token was issued for another resource. The `resource` must be `https://mcp.jomform.com/mcp`."],
            ["`invalid_scope`", "The client asked for a scope JomForm does not advertise. Ask for `read` or `readwrite`."],
            ["A write refused with a scope error", "The grant is `read`. A read grant cannot upgrade itself, so connect again and ask for `readwrite`."],
            ["A write refused with a role error", "The key is fine; your **role** in the `?workspace=` scope is read-only."],
            ["`create_api_key` refused", "A read or readonly credential cannot mint a wider one. Use a readwrite credential."],
            ["The client rejects `tools/list`", "Every tool declares an **object** outputSchema. Update the client; the server will not bend to a strict array check."],
            ["`submit_feedback` refused", "Feedback is user data, so it needs a **user token** from an OAuth sign-in. An API key is rejected."],
          ],
        },
      },
      {
        tip: "Read a sale's lifecycle with `describe_lifecycle` before a state change. An event the table marks `enabled=false` will be refused.",
      },
    ],
  },
];

export default function McpTroubleshootingPage() {
  return (
    <DocsPage
      title="Troubleshooting"
      lead="The eight refusals an MCP client meets in practice, and the one thing to change for each."
      sections={SECTIONS}
    />
  );
}
