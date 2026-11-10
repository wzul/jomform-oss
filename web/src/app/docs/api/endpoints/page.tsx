import type { Metadata } from "next";
import { docsMetadata } from "../../../../lib/docsMetadata";
import { DocsPage, type Section } from "../../docsProse";
import {
  accessLevels,
  apiExamples,
  endpointCount,
  endpointsByTag,
  scopeOnlyWriteRoutes,
} from "../../../../lib/openapi.ts";

export const metadata: Metadata = docsMetadata({
  title: "API endpoints by resource — JomForm Docs",
  description:
    "Every JomForm REST operation, grouped by resource, generated from the OpenAPI 3.1 contract — plus six worked examples the api suite executes.",
  path: "/docs/api/endpoints",
});

// Card t_0356ed4c: the reference half of the old /docs/api page. The rows are
// GENERATED from lib/openapi.ts (which shapes the generated contract), so this
// page cannot list an operation the API does not serve, and cannot silently miss
// one it does. The api suite pins the contract to the real route table
// (openapi_parity_test.go) and executes every example (openapi_examples_test.go).
//
// WHY THIS PAGE IS STILL LONG: it is the reference. A reader arrives with one
// operation in mind, and the section heading + table row is the answer — the page
// is tall because the API is big, not because the prose rambles. The jump strip and
// the per-tag tables are what make it scannable on a phone.

const total = endpointCount();

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
    id: "examples",
    title: "Worked examples",
    icon: "🧪",
    body: [
      "Each example is **executed against a live server**, so the status and the body fragment below are the ones the api suite asserts.",
      ...EXAMPLE_BLOCKS,
    ],
  },
];

export default function ApiEndpointsPage() {
  return (
    <DocsPage
      title="Endpoints by resource"
      lead="Every operation, grouped by resource, with the access gate each one enforces."
      sections={SECTIONS}
      jumpTo
    />
  );
}
