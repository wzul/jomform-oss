import type { Metadata } from "next";
import McpPage from "./mcp-client";
import { OG_IMAGE, OG_IMAGE_URL } from "../../lib/ogImage";

// Issue #276: per-page meta for the MCP setup/marketing page.
export const metadata: Metadata = {
  title: "Connect JomForm to your AI agent (MCP) — JomForm",
  description:
    "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Build forms, manage sales and payments from your agent.",
  alternates: { canonical: "https://jomform.com/mcp" },
  openGraph: {
    title: "Connect JomForm to your AI agent (MCP) — JomForm",
    description:
      "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Build forms, manage sales and payments from your agent.",
    url: "https://jomform.com/mcp",
    siteName: "JomForm",
    type: "website",
    // Issue #411: brand og:image so sharing shows a preview. Content-versioned
    // URL so a client that cached an older card is forced to the new file.
    images: [OG_IMAGE],
  },
  twitter: {
    card: "summary_large_image",
    title: "Connect JomForm to your AI agent (MCP) — JomForm",
    description:
      "Connect JomForm to Claude, Cursor and other AI agents via the Model Context Protocol. Build forms, manage sales and payments from your agent.",
    images: [OG_IMAGE_URL],
  },
};

export default function Page() {
  return <McpPage />;
}
