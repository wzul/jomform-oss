// GENERATED FILE — do not edit. Run `npm run docs:nav` (web/scripts/docsNav.mjs).
//
// The one registry the /docs chrome reads: breadcrumb, prev/next, related guides and
// the search index all come from here. The authored source is NAV_GROUPS in
// src/app/docs/layout.tsx (which docsIndex.test.ts reads as source text) plus the
// page files themselves; docsNav.test.ts fails when this file drifts from them.

export type DocsNavLayer = "Learn" | "Product docs" | "Reference";

export type DocsNavKind = "product" | "guide";

export type DocsNavEntry = {
  /** The page's route, e.g. `/docs/shipping`. */
  path: string;
  /** Its sidebar label (the guide's own title for a guide page). */
  label: string;
  layer: DocsNavLayer;
  section: string;
  /** Position in the reading order — the sidebar order, with the guides after their index. */
  order: number;
  kind: DocsNavKind;
  /** Last edit date (YYYY-MM-DD) of the page bytes `digest` names, or the value the
   *  committed file carried when git was absent. A landing commit never changes it. */
  updated: string;
  /** sha256 (first 16 hex chars) of the page file these fields were generated from: the
   *  freshness check pins `updated` to it, so a squash landing cannot re-date a page. */
  digest: string;
  /** Related screenshot guides, for a product page. */
  guides: string[];
};

export const DOCS_NAV: DocsNavEntry[] = [
  {
    "path": "/docs",
    "label": "Overview & features",
    "layer": "Learn",
    "section": "Start here",
    "order": 0,
    "kind": "product",
    "updated": "2026-11-10",
    "digest": "2c0a8641c2bba330",
    "guides": []
  },
  {
    "path": "/docs/builder",
    "label": "The form builder",
    "layer": "Learn",
    "section": "Start here",
    "order": 1,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "6b4bd528e5cd8754",
    "guides": [
      "/docs/guides/qr-table-ordering",
      "/docs/guides/tuition-class",
      "/docs/guides/wedding-rsvp"
    ]
  },
  {
    "path": "/docs/guides",
    "label": "Screenshot guides",
    "layer": "Learn",
    "section": "Start here",
    "order": 2,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "887f4b47bf2e87c5",
    "guides": []
  },
  {
    "path": "/docs/guides/affiliate-programme",
    "label": "Affiliate programme: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 3,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "ac62bcd392ac0dae",
    "guides": []
  },
  {
    "path": "/docs/guides/club-membership",
    "label": "Club membership: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 4,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "aa7aa691e5ab6950",
    "guides": []
  },
  {
    "path": "/docs/guides/design-with-mcp",
    "label": "Design with an AI agent (MCP): setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 5,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "0e46420fba514d29",
    "guides": []
  },
  {
    "path": "/docs/guides/donations",
    "label": "Collect donations: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 6,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "6c58e21cc8ca1d36",
    "guides": []
  },
  {
    "path": "/docs/guides/payment-fees",
    "label": "Payment fees: merchant setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 7,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "6b4673907a9150cd",
    "guides": []
  },
  {
    "path": "/docs/guides/qr-table-ordering",
    "label": "Table ordering with a QR menu: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 8,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "5e65d1c54b056b15",
    "guides": []
  },
  {
    "path": "/docs/guides/tuition-class",
    "label": "Tuition and class registration: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 9,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "df0ae2ec6e81f925",
    "guides": []
  },
  {
    "path": "/docs/guides/wedding-rsvp",
    "label": "Wedding page and RSVP form: setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 10,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "ba3ebdd5b4278784",
    "guides": []
  },
  {
    "path": "/docs/workspaces",
    "label": "Workspaces & deleting one",
    "layer": "Product docs",
    "section": "Workspaces",
    "order": 11,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "026faa26f73e3a11",
    "guides": []
  },
  {
    "path": "/docs/activity",
    "label": "Activity log",
    "layer": "Product docs",
    "section": "Workspaces",
    "order": 12,
    "kind": "product",
    "updated": "2026-11-10",
    "digest": "83cf7254c194a95c",
    "guides": []
  },
  {
    "path": "/docs/dormancy",
    "label": "Dormant workspaces & retention",
    "layer": "Product docs",
    "section": "Workspaces",
    "order": 13,
    "kind": "product",
    "updated": "2026-11-10",
    "digest": "090792e94c4c7a03",
    "guides": []
  },
  {
    "path": "/docs/sales",
    "label": "Sales, responses & analytics",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 14,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "5b981f115469b397",
    "guides": [
      "/docs/guides/qr-table-ordering",
      "/docs/guides/donations",
      "/docs/guides/wedding-rsvp",
      "/docs/guides/tuition-class"
    ]
  },
  {
    "path": "/docs/subscriptions",
    "label": "Subscriptions & recurring billing",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 15,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "a5857f7ef6083505",
    "guides": [
      "/docs/guides/club-membership",
      "/docs/guides/tuition-class"
    ]
  },
  {
    "path": "/docs/shipping",
    "label": "Shipping by zone",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 16,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "9011bda593a49569",
    "guides": []
  },
  {
    "path": "/docs/payment-fees",
    "label": "Processing fees paid by the buyer",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 17,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "79c75ea15fef92cf",
    "guides": [
      "/docs/guides/payment-fees"
    ]
  },
  {
    "path": "/docs/customize",
    "label": "Design → AI agent → MCP",
    "layer": "Product docs",
    "section": "Forms & fields",
    "order": 18,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "fb461f0d5a420214",
    "guides": [
      "/docs/guides/affiliate-programme",
      "/docs/guides/qr-table-ordering",
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/custom-forms",
    "label": "Custom HTML forms",
    "layer": "Product docs",
    "section": "Forms & fields",
    "order": 19,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "24234ac28a50073b",
    "guides": []
  },
  {
    "path": "/docs/variations",
    "label": "Variation products",
    "layer": "Product docs",
    "section": "Forms & fields",
    "order": 20,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "49ab090a6a06b921",
    "guides": []
  },
  {
    "path": "/docs/embed",
    "label": "Embed on your site",
    "layer": "Product docs",
    "section": "Storefront & pages",
    "order": 21,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "89b270f43ef52784",
    "guides": [
      "/docs/guides/donations",
      "/docs/guides/wedding-rsvp"
    ]
  },
  {
    "path": "/docs/email",
    "label": "Email & notifications",
    "layer": "Product docs",
    "section": "Email",
    "order": 22,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "7af30ece25f95cc2",
    "guides": []
  },
  {
    "path": "/docs/placeholders",
    "label": "Placeholders reference",
    "layer": "Product docs",
    "section": "Email",
    "order": 23,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "85783b5f2f1eda48",
    "guides": []
  },
  {
    "path": "/docs/ses-reputation",
    "label": "Email suppression list",
    "layer": "Product docs",
    "section": "Email",
    "order": 24,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "dedd2c52c9bb122c",
    "guides": []
  },
  {
    "path": "/docs/spam-detection",
    "label": "AI spam detection",
    "layer": "Product docs",
    "section": "Security",
    "order": 25,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "6b8fdf5736bff2a9",
    "guides": []
  },
  {
    "path": "/docs/security",
    "label": "Trusted devices (2FA)",
    "layer": "Product docs",
    "section": "Security",
    "order": 26,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "bb0a2682e272fb83",
    "guides": []
  },
  {
    "path": "/docs/chat",
    "label": "In-app assistant",
    "layer": "Product docs",
    "section": "In-app & support",
    "order": 27,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "3327c3a9e4670dbf",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/feedback",
    "label": "In-app feedback",
    "layer": "Product docs",
    "section": "In-app & support",
    "order": 28,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "c09d6ce9d8127f22",
    "guides": []
  },
  {
    "path": "/docs/api",
    "label": "REST API reference",
    "layer": "Reference",
    "section": "Reference",
    "order": 29,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "4befcda5b0d24c14",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/api/authentication",
    "label": "API authentication & scopes",
    "layer": "Reference",
    "section": "Reference",
    "order": 30,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "b288e90e79e9feef",
    "guides": []
  },
  {
    "path": "/docs/api/endpoints",
    "label": "API endpoints by resource",
    "layer": "Reference",
    "section": "Reference",
    "order": 31,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "7063aad11ea7fc56",
    "guides": []
  },
  {
    "path": "/docs/api/webhooks",
    "label": "API webhooks",
    "layer": "Reference",
    "section": "Reference",
    "order": 32,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "6092faded8cab2a3",
    "guides": []
  },
  {
    "path": "/docs/api/errors",
    "label": "API errors & limits",
    "layer": "Reference",
    "section": "Reference",
    "order": 33,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "e1a8ce411b1e5e39",
    "guides": []
  },
  {
    "path": "/docs/mcp",
    "label": "MCP tool reference",
    "layer": "Reference",
    "section": "Reference",
    "order": 34,
    "kind": "product",
    "updated": "2026-11-10",
    "digest": "60b08d759d53e7b0",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/mcp/connect",
    "label": "Connect an MCP client",
    "layer": "Reference",
    "section": "Reference",
    "order": 35,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "9659eb47f6c835ea",
    "guides": []
  },
  {
    "path": "/docs/mcp/tools",
    "label": "MCP tool list",
    "layer": "Reference",
    "section": "Reference",
    "order": 36,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "44b49396b8009d22",
    "guides": []
  },
  {
    "path": "/docs/mcp/oauth",
    "label": "MCP OAuth & tokens",
    "layer": "Reference",
    "section": "Reference",
    "order": 37,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "ef07db2f11bfa02b",
    "guides": []
  },
  {
    "path": "/docs/mcp/troubleshooting",
    "label": "MCP troubleshooting",
    "layer": "Reference",
    "section": "Reference",
    "order": 38,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "8a4466527459c245",
    "guides": []
  }
];
