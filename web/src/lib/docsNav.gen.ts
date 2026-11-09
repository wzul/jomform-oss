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
    "updated": "2026-11-08",
    "digest": "9b92e3928c46a900",
    "guides": []
  },
  {
    "path": "/docs/builder",
    "label": "The form builder",
    "layer": "Learn",
    "section": "Start here",
    "order": 1,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "974cdaa49b669a46",
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
    "updated": "2026-11-08",
    "digest": "1424055ef9b0f1a6",
    "guides": []
  },
  {
    "path": "/docs/guides/affiliate-programme",
    "label": "Affiliate programme — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 3,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "7aa62a1cf3888260",
    "guides": []
  },
  {
    "path": "/docs/guides/club-membership",
    "label": "Club membership — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 4,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "9f68da628611a050",
    "guides": []
  },
  {
    "path": "/docs/guides/design-with-mcp",
    "label": "Design with an AI agent (MCP) — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 5,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "f3c89a628a15f950",
    "guides": []
  },
  {
    "path": "/docs/guides/donations",
    "label": "Collect donations — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 6,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "d6c2b66f00ed1f66",
    "guides": []
  },
  {
    "path": "/docs/guides/payment-fees",
    "label": "Payment fees — merchant setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 7,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "a7d9af645d4adedf",
    "guides": []
  },
  {
    "path": "/docs/guides/qr-table-ordering",
    "label": "QR table ordering — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 8,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "3e8eacd22383c567",
    "guides": []
  },
  {
    "path": "/docs/guides/tuition-class",
    "label": "Tuition / class registration — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 9,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "8dee7f241ff4df6c",
    "guides": []
  },
  {
    "path": "/docs/guides/wedding-rsvp",
    "label": "Wedding page + RSVP form — setup guide",
    "layer": "Learn",
    "section": "Screenshot guides",
    "order": 10,
    "kind": "guide",
    "updated": "2026-11-08",
    "digest": "9974356311fd76bd",
    "guides": []
  },
  {
    "path": "/docs/workspaces",
    "label": "Workspaces & deleting one",
    "layer": "Product docs",
    "section": "Workspaces",
    "order": 11,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "7347ca5010962cbf",
    "guides": []
  },
  {
    "path": "/docs/activity",
    "label": "Activity log",
    "layer": "Product docs",
    "section": "Workspaces",
    "order": 12,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "b61e051b05701eec",
    "guides": []
  },
  {
    "path": "/docs/sales",
    "label": "Sales, responses & analytics",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 13,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "7ca408fc529d53c9",
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
    "order": 14,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "67bf0fa1bf298b86",
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
    "order": 15,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "66c54c8ca427e754",
    "guides": []
  },
  {
    "path": "/docs/payment-fees",
    "label": "Processing fees paid by the buyer",
    "layer": "Product docs",
    "section": "Sales & payments",
    "order": 16,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "61004d352f1c91af",
    "guides": [
      "/docs/guides/payment-fees"
    ]
  },
  {
    "path": "/docs/customize",
    "label": "Design → AI agent → MCP",
    "layer": "Product docs",
    "section": "Forms & fields",
    "order": 17,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "e3db90fdbcb01365",
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
    "order": 18,
    "kind": "product",
    "updated": "2026-11-07",
    "digest": "4d0492d224cfc318",
    "guides": []
  },
  {
    "path": "/docs/variations",
    "label": "Variation products",
    "layer": "Product docs",
    "section": "Forms & fields",
    "order": 19,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "b021f5f6f04547b4",
    "guides": []
  },
  {
    "path": "/docs/embed",
    "label": "Embed on your site",
    "layer": "Product docs",
    "section": "Storefront & pages",
    "order": 20,
    "kind": "product",
    "updated": "2026-11-06",
    "digest": "326feb83cdcd5797",
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
    "order": 21,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "225c0552bc0a1512",
    "guides": []
  },
  {
    "path": "/docs/placeholders",
    "label": "Placeholders reference",
    "layer": "Product docs",
    "section": "Email",
    "order": 22,
    "kind": "product",
    "updated": "2026-11-07",
    "digest": "f51361fdf3990127",
    "guides": []
  },
  {
    "path": "/docs/ses-reputation",
    "label": "Email suppression list",
    "layer": "Product docs",
    "section": "Email",
    "order": 23,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "f2ca13830c964f1f",
    "guides": []
  },
  {
    "path": "/docs/spam-detection",
    "label": "AI spam detection",
    "layer": "Product docs",
    "section": "Security",
    "order": 24,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "373fc4581feeac5b",
    "guides": []
  },
  {
    "path": "/docs/security",
    "label": "Trusted devices (2FA)",
    "layer": "Product docs",
    "section": "Security",
    "order": 25,
    "kind": "product",
    "updated": "2026-11-05",
    "digest": "3c07c71dae8ecbf4",
    "guides": []
  },
  {
    "path": "/docs/chat",
    "label": "In-app assistant",
    "layer": "Product docs",
    "section": "In-app & support",
    "order": 26,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "c84287c9cd10acf4",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/feedback",
    "label": "In-app feedback",
    "layer": "Product docs",
    "section": "In-app & support",
    "order": 27,
    "kind": "product",
    "updated": "2026-11-08",
    "digest": "b01ae870b85c8e8b",
    "guides": []
  },
  {
    "path": "/docs/api",
    "label": "REST API reference",
    "layer": "Reference",
    "section": "Reference",
    "order": 28,
    "kind": "product",
    "updated": "2026-11-07",
    "digest": "be3155ff5354fc08",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  },
  {
    "path": "/docs/mcp",
    "label": "MCP tool reference",
    "layer": "Reference",
    "section": "Reference",
    "order": 29,
    "kind": "product",
    "updated": "2026-11-09",
    "digest": "38c4c8d420147644",
    "guides": [
      "/docs/guides/design-with-mcp"
    ]
  }
];
