import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";

export const metadata: Metadata = docsMetadata({
  title: "Connect an MCP client — JomForm Docs",
  description:
    "Connect Claude, Cursor or another MCP client to JomForm: the remote server URL, the OAuth sign-in, and the JSON entry Cursor uses.",
  path: "/docs/mcp/connect",
});

// Card t_0356ed4c split /docs/mcp (7,685 words on a phone) by TASK. This page is
// the one a reader lands on with "how do I connect this thing" — the prose that
// used to open the old page, plus the two client walk-throughs that were only in
// the MCP tool reference's OAuth section.
//
// The client steps are the vendors' own documented flows, checked against
// support.claude.com (custom connectors, remote MCP) and cursor.com/docs/mcp —
// both read 2026-11-10. A vendor that renames a menu item is a copy change here,
// not a protocol change.

const SECTIONS: Section[] = [
  {
    id: "connect",
    title: "Connect an agent",
    icon: "🔌",
    body: [
      "JomForm speaks the **Model Context Protocol**. Point any MCP client at `https://mcp.jomform.com/mcp` and it discovers the tools on its own.",
      {
        note: "The connector authenticates with **OAuth 2.1** using an authorization code and PKCE. There are no API keys to copy or rotate. Scopes are on [OAuth & tokens](/docs/mcp/oauth).",
      },
      { heading: "Claude" },
      {
        steps: [
          "Open **Settings → Connectors** and choose **Add custom connector**.",
          "Paste `https://mcp.jomform.com/mcp` as the remote MCP server URL.",
          "Claude reads the **OAuth metadata** itself, so the detected client settings are fine.",
          "Choose **Sign in now** and approve the JomForm consent screen. The tools then appear in a conversation.",
        ],
      },
      { heading: "Cursor" },
      {
        steps: [
          "Open **Settings → Tools & MCP → New MCP Server**.",
          "Add `https://mcp.jomform.com/mcp` as a remote URL, or edit `~/.cursor/mcp.json`.",
          "Sign in when Cursor opens the browser; the client stores the token for later calls.",
        ],
      },
      {
        code: '{\n  "mcpServers": {\n    "jomform": { "url": "https://mcp.jomform.com/mcp" }\n  }\n}',
        caption: "Cursor's mcp.json entry. The OAuth sign-in opens on the first call.",
      },
    ],
  },
  {
    id: "no-browser",
    title: "No browser at all",
    icon: "🖥️",
    body: [
      "A headless harness can skip the browser. JomForm supports the **device authorization grant**, RFC 8628.",
      "The harness asks for a code, JomForm emails it to your account, and you approve it from that email.",
      "See [OAuth & tokens](/docs/mcp/oauth) for the endpoints, the polling rules and the scope ladder.",
    ],
  },
  {
    id: "cli",
    title: "No browser? Use the CLI",
    icon: "⌨️",
    body: [
      "Some MCP clients only implement the browser flow. On a machine with no display they cannot finish. The **JomForm CLI** runs the device grant in your terminal instead.",
      {
        steps: [
          "Build the CLI once: `cd api && go build -o jomform ./cmd/jomform`.",
          "Run `./jomform mcp login --headless`. Add `--out` for a client other than Cursor, and `--email` to mail the code to a specific address.",
          "Open the printed link on any device — a phone is fine — sign in and press **Approve**.",
          "The command finishes by itself, writes the token and prints the config path.",
        ],
      },
      {
        code: "$ ./jomform mcp login --headless\nhttps://jomform.com/profile?tab=mcp&user_code=BCDF-GHJK\nBCDF-GHJK\nopen this link and press Approve — you have 10 minutes\n/home/you/.cursor/mcp.json",
        caption: "The link already carries the code; the code is repeated on its own line.",
      },
      "The entry it writes is the one you would add by hand, with the token in the header:",
      {
        code: '{\n  "mcpServers": {\n    "jomform": {\n      "url": "https://mcp.jomform.com/mcp",\n      "headers": { "Authorization": "Bearer <token>" }\n    }\n  }\n}',
        caption: "Only the jomform entry is written.",
      },
      {
        note: "Most clients read their config file only at startup. Restart the client after the command finishes, then call a tool to check the connection.",
      },
    ],
  },
  {
    id: "cli-files",
    title: "Where the CLI writes",
    icon: "📄",
    body: [
      "The default is Cursor's file. Claude Desktop keeps its own, so pass `--out` with the path for your client:",
      {
        list: [
          "`~/.cursor/mcp.json` — the default.",
          "`~/Library/Application Support/Claude/claude_desktop_config.json` — Claude Desktop on macOS.",
          "`~/.config/Claude/claude_desktop_config.json` — Claude Desktop on Linux.",
        ],
      },
      "Any other path needs `--force`, so a typo cannot rewrite an unrelated JSON file.",
      "The entry is **merged**: every other server in the file keeps its bytes, and the file is written 0600.",
    ],
  },
  {
    id: "cli-status",
    title: "Check the grant, and sign out",
    icon: "🔎",
    body: [
      {
        list: [
          "`./jomform mcp status` prints the expiry, the scopes and the account, and exits non-zero when there is no token or it expired.",
          "`./jomform mcp logout` revokes the refresh token on the server, removes the mcp.json entry and deletes the local copy.",
        ],
      },
      {
        warn: "The entry carries the **access token**, issued for about an hour. When `status` says it expired, run the login command again.",
      },
    ],
  },
];

export default function McpConnectPage() {
  return (
    <DocsPage
      title="Connect an MCP client"
      lead="One remote URL and a browser sign-in. No API key, no local server to run."
      sections={SECTIONS}
    />
  );
}
