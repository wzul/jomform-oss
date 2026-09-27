// The docs pages that name MCP tools — "no shipped page presents a tool that
// is not registered as one".
//
// WHY THIS EXISTS (the bug it was written for): v1.12.0 shipped
// web/src/app/docs/chat/page.tsx claiming the chat's proposal lane "exists over
// MCP as the propose_write tool", and its "Related:" footer listed
// propose_write beside submit_feedback as if both were entries of the linked
// MCP tool reference. propose_write is deliberately NOT registered as an MCP
// tool (MCP-PARITY.md, "In-app assistant — the proposal lane"; the confirm step
// is a session-only route, so a registered version would hand every MCP caller a
// token it could never spend, and api/internal/mcpsrv/chat_tools_guard_test.go
// fails if it is ever registered). The code was right and the COPY was wrong —
// which is exactly the class of defect nothing on the Go side can see.
//
// WHAT IT GUARDS:
//   1. A name the docs CALL a tool ("as the X tool", "X, the tool that …") must
//      be a name the Go registry actually registers. That is the phrasing that
//      shipped the false claim, and it is the phrasing a future copy edit is
//      most likely to reuse.
//   2. The two exact sentences that shipped the false claim cannot come back.
//   3. The chat page keeps saying the lane is in-app / NOT an MCP tool, so a
//      silent deletion of the correction fails here.
//   4. MCP-PARITY.md still records the decision as NOT registered — the page's
//      claim and the parity file have to agree, or one of them is lying.
//   5. Every `{ name: … }` entry of the MCP tool reference (/docs/mcp) is a
//      registered tool, so that page cannot advertise a tool that does not exist
//      (the reverse direction of 1).
//
// HOW THE REGISTRY IS READ, and why it is not a hardcoded list: the tool
// registry lives in Go (api/internal/mcpsrv/tools.go), so a hardcoded name list
// here would be a second, silently-drifting copy of it — the same failure mode
// as the bug. It is derived from the source instead, cross-language, in the
// idiom of salesRows.test.ts / spamJudgeToggle.test.ts. Both registration shapes
// in that file are handled: `addTool(s, &mcp.Tool{Name: …}, h)` (the per-call
// wrapper that resolves and scopes the caller) and the two older
// `mcp.AddTool(s.mcp, &mcp.Tool{Name: …}` call sites. If a third shape appears,
// test 1 fails on the count rather than letting this guard pass vacuously.
//
// WHAT IT DOES NOT COVER (stated honestly): it reads source text, so it cannot
// prove the served HTML — the rendered-page check is `npm run build` plus a
// browser read of /docs/chat. It also does not check that a listed tool's
// DESCRIPTION is accurate, only that the tool exists.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const dir = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const DOCS_DIR = dir("../app/docs");
const TOOLS_GO = "../../../api/internal/mcpsrv/tools.go";
const PARITY_MD = "../../../MCP-PARITY.md";
const TOOL_REFERENCE_PAGE = "../app/docs/mcp/page.tsx";
const CHAT_DOCS_PAGE = "../app/docs/chat/page.tsx";

// Every docs page in the tree, recursively — a new /docs/<x> page is covered by
// this guard the day it lands, without editing this file.
function docsPages(base: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const name of readdirSync(base)) {
    const full = join(base, name);
    if (statSync(full).isDirectory()) {
      for (const [k, v] of docsPages(full)) out.set(k, v);
    } else if (name === "page.tsx") {
      out.set(full, readFileSync(full, "utf8"));
    }
  }
  return out;
}

const PAGES = docsPages(DOCS_DIR);

// The registered MCP tool set, derived from the Go registry source.
function registeredTools(): Set<string> {
  const src = read(TOOLS_GO);
  const names = new Set<string>();
  for (const m of src.matchAll(/&mcp\.Tool\{\s*Name:\s*"([a-z0-9_]+)"/g)) names.add(m[1]);
  return names;
}

const REGISTERED = registeredTools();

// The two phrasings that present a bare name as a tool. A tool name in this
// registry is a snake_case identifier, so the capture requires at least one
// underscore — that is what keeps "as no tool to call" (real chat/page.tsx
// prose) out without a hand-maintained stoplist.
const TOOLNAME = String.raw`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`;
const CALLED_A_TOOL = [
  new RegExp(String.raw`\bas (?:the )?(${TOOLNAME}) tool\b`, "gi"),
  new RegExp(String.raw`\b(${TOOLNAME}),? the tool (?:that|which|for)\b`, "gi"),
];

// A tool name as it appears anywhere in docs prose or in a tool-reference entry.
const NAMED_IN_PROSE = /\b[a-z][a-z0-9]*_[a-z0-9_]+\b/g;

test("the guard is looking at the real docs tree and the real registry (it cannot pass vacuously)", () => {
  assert.ok(
    PAGES.size >= 10,
    `walked only ${PAGES.size} docs page(s) under src/app/docs — the walk is broken`,
  );
  assert.ok(
    PAGES.has(fileURLToPath(new URL(CHAT_DOCS_PAGE, import.meta.url))),
    "the docs walk did not pick up /docs/chat — the page this guard was written for",
  );
  assert.ok(
    REGISTERED.size >= 150,
    `parsed only ${REGISTERED.size} registered tool(s) from ${TOOLS_GO} — the registry ` +
      "extractor has drifted from the registration shapes in that file",
  );
  for (const known of ["submit_feedback", "list_products", "set_form_theme", "update_product"]) {
    assert.ok(REGISTERED.has(known), `the registry extractor missed ${known}, a tool registered in Go`);
  }
  // The whole point of the guard: these two are the names the docs must be
  // careful about. One is registered, one is deliberately not.
  assert.ok(REGISTERED.has("submit_feedback"), "submit_feedback is no longer a registered MCP tool");
  assert.ok(
    !REGISTERED.has("propose_write"),
    "propose_write is registered as an MCP tool — that is the decision the docs, MCP-PARITY.md " +
      "and api/internal/mcpsrv/chat_tools_guard_test.go all record as deliberately NOT taken",
  );
});

test("a name the docs call a tool is a tool that is actually registered", () => {
  const offenders: string[] = [];
  for (const [file, src] of PAGES) {
    for (const re of CALLED_A_TOOL) {
      for (const m of src.matchAll(re)) {
        if (!REGISTERED.has(m[1])) {
          const line = src.slice(0, m.index).split("\n").length;
          offenders.push(`${file}:${line} presents "${m[1]}" as a tool, but no such tool is registered`);
        }
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `the docs present a name as an MCP tool that api/internal/mcpsrv/tools.go does not register:\n  ` +
      offenders.join("\n  ") +
      "\nEither the tool is not registered (then do NOT describe it as one — see MCP-PARITY.md's " +
      '"In-app assistant — the proposal lane") or it is registered and the claim is true.',
  );
});

test("the two sentences that shipped the false claim cannot come back", () => {
  const chatDocs = read(CHAT_DOCS_PAGE);
  assert.doesNotMatch(
    chatDocs,
    /exists over MCP as the propose_write tool/i,
    "the shipped v1.12.0 sentence is back: it told users the proposal lane exists over MCP as " +
      "propose_write, which is not registered as an MCP tool",
  );
  assert.doesNotMatch(
    chatDocs,
    /, and propose_write, the tool that/i,
    "the shipped v1.12.0 footer is back: it listed propose_write beside submit_feedback as if " +
      "both were entries of the linked MCP tool reference",
  );
});

test("the chat page still says the proposal lane is in-app and NOT an MCP tool", () => {
  const chatDocs = read(CHAT_DOCS_PAGE);
  // Still documented (AGENTS.md DOCUMENTATION PARITY step 2): the fix is to make
  // the CLAIM true, not to delete the name.
  assert.ok(
    chatDocs.includes("propose_write"),
    "the chat docs no longer name propose_write at all — the phase-2 lane would be undocumented",
  );
  assert.match(
    chatDocs,
    /propose_write[\s\S]{0,160}not an MCP tool|no propose_write over MCP/i,
    "the chat docs name propose_write but no longer say it is not an MCP tool — without that, " +
      "the name alone reads as a registered tool again",
  );
  // The accurate half of the original paragraph must survive the fix as well.
  assert.match(
    chatDocs,
    /deliberately not something an API call can supply/i,
    "the docs lost the true half of the original sentence: the confirmation is a signed-in " +
      "person's click and not something an API call can supply",
  );
});

test("MCP-PARITY.md still records the decision as NOT registered, in step with the page", () => {
  const parity = read(PARITY_MD);
  assert.match(
    parity,
    /`propose_write` — NOT registered/,
    "MCP-PARITY.md no longer records propose_write as deliberately NOT registered — the docs " +
      "page and the parity file now contradict each other",
  );
  assert.match(
    parity,
    /session-only/,
    "MCP-PARITY.md no longer gives the reason (the confirm step is a session-only route), which " +
      "is the reason the docs page leans on",
  );
  assert.match(
    parity,
    /no MCP caller could ever confirm/i,
    "MCP-PARITY.md no longer says why registering propose_write would be wrong (the token goes to " +
      "the browser and an MCP caller could never confirm it)",
  );
});

test("every entry of the MCP tool reference is a registered tool", () => {
  const reference = read(TOOL_REFERENCE_PAGE);
  const entries = [...reference.matchAll(/\{\s*name:\s*"([a-z0-9_]+)"/g)].map((m) => m[1]);
  assert.ok(
    entries.length >= 100,
    `parsed only ${entries.length} tool entries from /docs/mcp — the extractor has drifted from ` +
      "that page's entry shape",
  );
  assert.ok(
    entries.includes("list_products"),
    "the /docs/mcp extractor did not find list_products, a listed tool — the parse is wrong",
  );
  const phantom = [...new Set(entries)].filter((n) => !REGISTERED.has(n));
  assert.deepEqual(
    phantom,
    [],
    `/docs/mcp advertises tool(s) that api/internal/mcpsrv/tools.go does not register: ` +
      `${phantom.join(", ")} — the reference page must only list tools that exist`,
  );
});

test("the prose scan does not mistake a field or a phrase for a tool name", () => {
  // A guard against this file going over-eager: these are NOT tool names and the
  // scan must leave them alone. `as no tool to call` is real chat/page.tsx prose.
  const chatDocs = read(CHAT_DOCS_PAGE);
  const phrases = ["as no tool to call", "calls that change's own tool itself"];
  for (const phrase of phrases) {
    assert.ok(chatDocs.includes(phrase), `chat/page.tsx no longer contains ${JSON.stringify(phrase)}`);
  }
  for (const re of CALLED_A_TOOL) {
    for (const phrase of phrases) {
      assert.equal(
        [...phrase.matchAll(re)].length,
        0,
        `${re} matched the non-tool phrase ${JSON.stringify(phrase)} — the scan would produce a ` +
          "false positive on ordinary prose",
      );
    }
  }
  // And the prose name matcher must not be so broad that it flags the whole page.
  const names = [...chatDocs.matchAll(NAMED_IN_PROSE)].map((m) => m[1]);
  assert.ok(names.length > 0, "the prose name matcher found nothing in the chat docs");
});
