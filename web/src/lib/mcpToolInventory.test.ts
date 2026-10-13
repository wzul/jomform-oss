// /docs/mcp claims to list the whole tool registry — this guard makes that true
// in BOTH directions (card t_cde2e73a, issue #395's reference page).
//
// THE BUG. The page rendered "164 tools across 31 categories" and told the
// reader it was "kept in sync with the tool registry", while
// api/internal/mcpsrv/tools.go registered 168. Ten registered tools had no row:
// submit_feedback (the one tool /docs/chat points at), the four other in-app
// feedback tools, the four affiliate tools (#444) and set_domain_custom_cert
// (named in prose inside set_domain_tls_mode's description but never listed).
// A reader deciding what an agent may call got a list that was missing a
// fifth of the feedback surface and all of the affiliate surface.
//
// The count was wrong in a second, quieter way: it was
//   TOOL_GROUPS.reduce((n, g) => n + g.tools.length, 0)
// over a list that carried FIVE tools twice (the dashboard-mirror rows for
// dead-letter events and suppressions) and one row whose `name` was an em dash
// — prose about magic-link sessions sitting in a group called "Keselamatan
// sesi", pretending to be a tool. So "164 tools" was not 164 tools either.
//
// WHY THIS IS THE GUARD THAT MATTERS, and why it is NOT just a number fix:
// web/src/lib/docsToolClaims.test.ts already fails when a listed name is not
// registered (the reverse direction). Nothing failed the FORWARD direction —
// a registered tool the page forgot — which is why ten of them could go
// missing with a green suite. Editing the number 164 by hand (or adding rows
// without a guard) reproduces exactly the hand-maintained-copy defect that
// shipped the propose_write claim in the first place.
//
// WHAT IT CHECKS:
//   1. Vacuity/anchors: the page's group array and the Go registry are really
//      being read, and the registry extractor sees EVERY `&mcp.Tool{` in
//      tools.go — so a third registration shape fails here instead of quietly
//      shrinking the registered set (which would make direction 2 pass by
//      having nothing to require).
//   2. FORWARD: every registered tool has a row on the page. A new Go tool
//      with no docs row fails the build.
//   3. REVERSE: every row on the page is a registered tool (no phantom).
//   4. The count is REAL: no row may repeat a name, and every row's name must
//      be a snake_case tool identifier — the em-dash placeholder and any
//      future "prose in a tool row" fail here.
//   5. The number on the page is DERIVED, not typed: the render must count the
//      data, and no literal "<n> tools" may appear in the page source. A
//      hand-typed count reddens the suite the moment the registry moves.
//   6. No claim on the page names a single FILE as the registry. The page told
//      a reader — and a search engine, via its <meta name="description"> — that
//      the registry was api/internal/mcpsrv/tools.go, and #732 falsified that
//      by moving disable_2fa's literal into parity_issue396.go. Direction 1-3
//      check the ROW LIST; this checks the CLAIM those rows sit under.
//
// HOW THE REGISTRY IS READ: from the Go source, cross-language, in the idiom of
// salesRows.test.ts / spamJudgeToggle.test.ts. A hardcoded name list here would
// be a third hand-maintained copy of the registry — the same failure mode as
// the bug.
//
// WHAT IT DOES NOT COVER (stated honestly): it is a source scan. It proves the
// two SETS agree and that the count is derived; it does NOT prove the served
// HTML (that is `npm run build` plus a browser read of /docs/mcp), it does NOT
// check that a row's DESCRIPTION is accurate or matches the Go description, and
// it does not check the ORDER or the group a tool is filed under.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const PAGE = read("../app/docs/mcp/page.tsx");

// The registry is the mcpsrv PACKAGE, not one file. It was tools.go for years,
// so reading tools.go alone looked right — until a refactor moved a tool literal
// out of it (disable_2fa's constructor into parity_issue396.go) and this guard
// silently extracted 168 names against a 169-tool registry: the FORWARD
// direction then required one tool too few, while the REVERSE direction accused
// a row that IS registered, reddening a correct page. Pin the whole non-test
// package, the way api/internal/mcpsrv/mcp_parity_test.go discovers its source.
// The vacuity anchor below runs over this same widened scope, so a registration
// shape that lives anywhere in the package still fails loudly here instead of
// quietly shrinking REGISTERED.
const MCP_DIR = fileURLToPath(new URL("../../../api/internal/mcpsrv/", import.meta.url));
const MCP_BY_FILE = readdirSync(MCP_DIR)
  .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
  .sort()
  .map((f) => [f, readFileSync(MCP_DIR + f, "utf8")] as const);
const TOOLS_GO = MCP_BY_FILE.map(([, src]) => src).join("\n");

// Per-file `&mcp.Tool{` literal counts. Test 6 needs these: "this FILE is the
// registry" is a claim about one file and is only true when that file holds
// every literal, which a count over the whole joined package cannot see.
const LITERALS_BY_FILE = new Map(
  MCP_BY_FILE.map(([f, src]) => [f, (src.match(/&mcp\.Tool\{/g) ?? []).length]),
);

// The whole TOOL_GROUPS array literal — from the declaration to the line that
// closes it at column 0. Reading only the array keeps the render code (and the
// prose sections below it) out of the row scan.
const START = PAGE.indexOf("const TOOL_GROUPS");
const END = PAGE.indexOf("\n];", START);
const REGISTRY_BLOCK = START >= 0 && END > START ? PAGE.slice(START, END) : "";

// A comment is not a row. The array carries `//` explanations (and this card's
// own fix left one quoting the em-dash row it removed), so comment-ONLY lines are
// dropped before the row scan — otherwise a comment that NAMES a tool would
// satisfy the forward direction for it, and a comment quoting the placeholder
// would fail the no-non-tool check. Only whole-line comments are stripped: a
// description containing `https://` must keep its text, so no in-line `//`
// stripping (which would truncate at the URL scheme). Stated limit: a row placed
// on a shared line with a trailing comment would still be read as code.
const CODE = REGISTRY_BLOCK.split("\n")
  .filter((line) => !line.trimStart().startsWith("//"))
  .join("\n");

// Every tool row. Anchored on the object-literal shape the page uses, and
// deliberately permissive about the captured name so a NON-identifier name (the
// em-dash placeholder) is captured and then REJECTED by test 4 rather than being
// silently skipped by a narrow regex.
const ROWS = [...CODE.matchAll(/\{\s*name:\s*"([^"]*)"/g)].map((m) => m[1]);

// The registered tool set, derived from the Go registry source.
const REGISTERED = new Set(
  [...TOOLS_GO.matchAll(/&mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"/g)].map((m) => m[1]),
);

test("the guard is looking at the real page and the real registry (it cannot pass vacuously)", () => {
  assert.ok(
    REGISTRY_BLOCK.length > 1000,
    `parsed only ${REGISTRY_BLOCK.length} chars of TOOL_GROUPS from /docs/mcp — the array extractor ` +
      "has drifted from that page's shape",
  );
  assert.ok(
    ROWS.length >= 150,
    `found only ${ROWS.length} tool row(s) in the TOOL_GROUPS array — the row extractor has drifted`,
  );
  // Anchors chosen so this test stays about the PARSE: both have been on the
  // page since long before this guard, so a failure here means the extractor
  // broke, not that a specific tool was (or was not) listed. Whether a given
  // tool is listed is what the two direction tests below are for.
  assert.ok(
    ROWS.includes("list_products") && ROWS.includes("get_account"),
    "the row extractor did not find list_products/get_account — the parse is wrong",
  );
  assert.ok(
    REGISTERED.size >= 150,
    `parsed only ${REGISTERED.size} registered tool(s) from the non-test mcpsrv package — the ` +
      "registry extractor has drifted from the registration shapes in that package",
  );
  for (const known of ["list_products", "submit_feedback", "set_form_theme", "update_product"]) {
    assert.ok(REGISTERED.has(known), `the registry extractor missed ${known}, a tool registered in Go`);
  }
  // Every `&mcp.Tool{` in the package must yield a name. If a third registration
  // shape appears, this fails HERE — with the registry set quietly incomplete,
  // direction 2 below would require less and less and could pass by having
  // nothing left to check. Over the WIDENED scope this is what makes a tool
  // literal moved into a new file or a constructor count as registered.
  const literals = (TOOLS_GO.match(/&mcp\.Tool\{/g) ?? []).length;
  assert.equal(
    literals,
    REGISTERED.size,
    `${literals} \`&mcp.Tool{\` literal(s) in the non-test mcpsrv package but only ` +
      `${REGISTERED.size} name(s) extracted — a registration shape the extractor does not ` +
      "understand, so the registered set is incomplete",
  );
});

test("every tool the registry registers has a row on /docs/mcp", () => {
  const missing = [...REGISTERED].filter((n) => !ROWS.includes(n)).sort();
  assert.deepEqual(
    missing,
    [],
    `/docs/mcp omits ${missing.length} registered tool(s) that api/internal/mcpsrv/ ` +
      `registers:\n  ${missing.join("\n  ")}\n` +
      "The page says it lists the whole registry, so a registered tool with no row makes that " +
      "claim false. Add the row (with its description) or, if the tool should not be listed, do " +
      "not claim the page is the registry.",
  );
});

test("every row on /docs/mcp is a tool the registry actually registers", () => {
  const phantom = [...new Set(ROWS)].filter((n) => !REGISTERED.has(n)).sort();
  assert.deepEqual(
    phantom,
    [],
    `/docs/mcp advertises tool(s) that api/internal/mcpsrv/ does not register: ` +
      `${phantom.join(", ")} — the reference page must only list tools that exist`,
  );
});

test("the rendered count is a real count: no repeated row, and no non-tool row", () => {
  const seen = new Map<string, number>();
  for (const n of ROWS) seen.set(n, (seen.get(n) ?? 0) + 1);
  const repeated = [...seen.entries()].filter(([, c]) => c > 1).map(([n]) => n).sort();
  assert.deepEqual(
    repeated,
    [],
    `/docs/mcp lists ${repeated.length} tool(s) more than once (${repeated.join(", ")}) — the ` +
      "rendered total counts each row, so a repeated row inflates it and the page reports more " +
      "tools than the registry has",
  );
  // A row's name must be a tool identifier. The page shipped `{ name: "—" }` — a
  // prose paragraph about magic-link sessions in a tool card, counted as a tool.
  const notATool = ROWS.filter((n) => !/^[a-z][a-z0-9_]*$/.test(n));
  assert.deepEqual(
    notATool,
    [],
    `/docs/mcp has a row whose name is not a tool identifier: ${JSON.stringify(notATool)} — ` +
      "that row renders as a tool card and is counted in the total. Prose belongs in a group " +
      "`note`, not in a `{ name: … }` row.",
  );
});

test("the tool count on the page is derived from the data, never typed by hand", () => {
  // The claim this card exists for was a hand-maintained number. Pin the shape
  // that makes the number follow the registry: the render counts the array.
  assert.match(
    PAGE,
    /TOOL_GROUPS\.reduce\(\(n, g\) => n \+ g\.tools\.length, 0\)/,
    "the /docs/mcp render no longer derives its tool total from TOOL_GROUPS — a count that is not " +
      "derived from the list it describes is the hand-maintained copy this guard exists to stop",
  );
  // ... and the category total must skip note-only groups, or "N categories"
  // counts a heading that has no tools under it.
  assert.match(
    PAGE,
    /TOOL_GROUPS\.filter\(\(g\) => g\.tools\.length > 0\)\.length/,
    "the /docs/mcp render counts every group as a category, including a group that lists no tools " +
      "(the Keselamatan sesi note-only group) — the rendered category total would be wrong",
  );
  // No typed total may exist anywhere on the page, including in the metadata
  // description (which shipped "164" -style prose is exactly how the drift starts).
  const typed = [...PAGE.matchAll(/\b\d+\s+tools\b/gi)].map((m) => m[0]);
  assert.deepEqual(
    typed,
    [],
    `/docs/mcp has a hand-typed tool count in its source: ${JSON.stringify(typed)}. The page must ` +
      "count TOOL_GROUPS, not carry a number an editor has to remember to update.",
  );
});

// ── The CLAIM, not just the row list ─────────────────────────────────────────
//
// The page's own body was corrected to name the api/internal/mcpsrv PACKAGE as
// the registry; its `<meta name="description">` and its header comment still
// named a single FILE, and neither this suite nor docsToolClaims.test.ts looked
// at them. #732 falsified the file claim by moving disable_2fa's `&mcp.Tool{}`
// literal into parity_issue396.go, so tools.go is no longer the registry (176 of
// the 177 literals) and the page contradicted itself — the body right, the head
// wrong, for as long as nobody edited the head.
//
// Naming a file as the registry is a claim that the registry IS that file, and
// such a claim is true only while that file holds EVERY `&mcp.Tool{` literal in
// the package. One literal elsewhere makes the file a subset of the registry and
// the claim false — which is exactly what happened, and the reason this is
// checked per FILE rather than against the joined package source the tests above
// use.
//
// The claim is read in BOTH spellings, because the sentence is the same one
// either way (card t_69bd144f):
//   - a PATH, `api/internal/mcpsrv/<file>.go`. Judged whatever <file> is: the
//     path itself says the claim is about THIS package, so a name that is not a
//     source file here (a ghost, a `_test.go`) is still a claim and is judged by
//     its literal count — 0 of the N — exactly as the original guard did.
//   - a BARE name, `<file>.go` with no directory in front of it. This is the
//     spelling the guard shipped blind to: "the tool registry in tools.go" was
//     measured GREEN here while the whole web unit suite stayed 510/510, on a
//     page whose file name is just as false as the path form. A bare name is
//     judged ONLY when it really is a source file of the package
//     (LITERALS_BY_FILE), so a `.go` token belonging to some other tree — prose,
//     a test file, a file that does not exist — is not read as a claim about
//     this registry and cannot red a correct page. STATED LIMIT: a bare name
//     that is not a file here is therefore invisible; the width this test has is
//     over the package's own files, and the path form keeps the wider reach.
//
// Dropping the file name entirely ("the tool registry") is a legitimate state —
// the card that filed this allows it — so there is deliberately no "must name a
// path" anchor here, and the positive controls are the bite proof's own cases:
// web/scripts/biteproof_mcp_tool_inventory.sh puts the file name back — as a
// path AND as a bare name — and requires THIS test to go RED on each, and drives
// the two states that must stay GREEN (no file name, and a bare `.go` token that
// is not a file of this package) so the widened extraction cannot red prose.
test("no claim on /docs/mcp names a single FILE as the registry", () => {
  // The anchor first: a metadata description is the sentence a search engine and
  // an agent scraping the page are told what the registry is. If the block is
  // deleted, this check would pass by absence rather than by agreement.
  assert.match(
    PAGE,
    /\n\s*description:\s*"/,
    "/docs/mcp has no metadata description — the surface a search engine and an agent scraping " +
      "the page read is the one this test checks, so its absence must not silence the check",
  );
  // Both spellings of the claim (see the note above): the PATH form, judged
  // whatever it names, and the BARE form, judged only when the name is a source
  // file of this package. The bare pass is what closes the blindness this card
  // was filed for — it is the same claim and the same per-file count either way.
  const claimed = [
    ...new Set([
      ...[...PAGE.matchAll(/api\/internal\/mcpsrv\/([A-Za-z0-9_]+\.go)/g)].map((m) => m[1]),
      ...[...PAGE.matchAll(/\b([A-Za-z0-9_]+\.go)\b/g)]
        .map((m) => m[1])
        .filter((f) => LITERALS_BY_FILE.has(f)),
    ]),
  ];
  const wholePackage = [...LITERALS_BY_FILE.values()].reduce((n, c) => n + c, 0);
  assert.ok(
    wholePackage > 100,
    `counted only ${wholePackage} \`&mcp.Tool{\` literal(s) across the non-test mcpsrv package — ` +
      "the per-file counts cannot judge a file-name claim, so this test would pass vacuously",
  );
  const subsets = claimed
    .map((f) => [f, LITERALS_BY_FILE.get(f) ?? 0] as const)
    .filter(([, n]) => n !== wholePackage)
    .map(
      ([f, n]) =>
        `${f} holds ${n} of the ${wholePackage} \`&mcp.Tool{\` literal(s) that ` +
        "api/internal/mcpsrv registers",
    );
  assert.deepEqual(
    subsets,
    [],
    `/docs/mcp tells the reader its registry is one file, and that file does not hold the whole ` +
      `registry:\n  ${subsets.join("\n  ")}\n` +
      "The registry is the api/internal/mcpsrv PACKAGE — this guard and docsToolClaims.test.ts " +
      "both read it that way, and the page body already says so. Name the package, or drop the " +
      'file name and say "the tool registry". A file that holds a subset is not the registry, ' +
      "and a search engine quoting the page is told it is.",
  );
});
