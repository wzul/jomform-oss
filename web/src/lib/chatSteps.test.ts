// A tool name becomes a sentence, in the visitor's language (card t_148d7100).
//
// WHAT THIS FILE OWNS:
//
//   1. COMPLETENESS AGAINST THE SERVER'S OWN LIST. The chat's read tools are
//      declared in Go (api/internal/mcpsrv/chat_tools.go, `chatReadTools`). A tool
//      added there appears on the wire as a `step` event, and a page with no entry
//      for it would say only "Working on it…" — for ever, for a tool that is
//      perfectly well known. So the allowlist is READ OUT OF THE GO SOURCE and
//      every name must resolve to a noun key.
//   2. THE KEY EXISTS IN ALL FIVE LOCALES. A missing translation renders the raw
//      key at HTTP 200 with nothing erroring — the silent shape this repo has
//      shipped before (issue #6177's family).
//   3. THE SENTENCE, not just the lookup. The English rendering is asserted, so a
//      mapping that resolves to the wrong noun (sales for forms) fails.
//   4. THE PAST TENSE for the finished trail: a line that still says "Reading your
//      forms…" long after it read them reads as work that never finished.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { FILING_TOOL, NOUN_BY_TOOL, stepLabel } from "./chatSteps.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const goChatTools = read("../../../api/internal/mcpsrv/chat_tools.go");

/** The names in the Go `chatReadTools` slice — the server's own allowlist. */
function readToolsFromGo(): string[] {
  const m = goChatTools.match(/var chatReadTools = \[\]string\{([\s\S]*?)\n\}/);
  assert.ok(m, "could not find `var chatReadTools = []string{…}` in api/internal/mcpsrv/chat_tools.go");
  return [...m[1].matchAll(/"([a-z0-9_]+)"/g)].map((x) => x[1]);
}

/** A translator for one locale, using the real dictionary and interpolation. */
const translator =
  (lang: (typeof LANGS)[number]) =>
  (key: string, params?: Record<string, string | number>): string => {
    const entry = DICT[key as keyof typeof DICT];
    assert.ok(entry, `the dictionary has no entry for ${key}`);
    let s = (entry as Record<string, string>)[lang];
    assert.ok(s, `${key} has no ${lang} translation`);
    for (const [k, v] of Object.entries(params ?? {})) s = s.replaceAll(`{${k}}`, String(v));
    return s;
  };

test("the Go read allowlist is really being read (this cannot pass vacuously)", () => {
  const tools = readToolsFromGo();
  assert.ok(
    tools.length >= 10,
    `only ${tools.length} read tools were parsed out of chat_tools.go — the anchor moved and this ` +
      `completeness check would pass for an empty list`,
  );
  assert.ok(tools.includes("list_forms"), "list_forms is not in the parsed allowlist");
});

test("EVERY read tool the server can emit has a noun", () => {
  const missing = readToolsFromGo().filter((t) => !NOUN_BY_TOOL[t]);
  assert.deepEqual(
    missing,
    [],
    `these tools appear as a live step but have no noun in lib/chatSteps.ts, so the user would see ` +
      `"Working on it…" while the assistant reads them: ${missing.join(", ")}`,
  );
});

test("every noun key exists in all five locales", () => {
  const keys = new Set([...Object.values(NOUN_BY_TOOL), "chat.step.reading", "chat.step.read", "chat.step.filing", "chat.step.filed", "chat.step.working", "chat.step.worked"]);
  for (const key of keys) {
    for (const lang of LANGS) {
      assert.ok(
        translator(lang)(key),
        `${key} is missing its ${lang} translation — the page would render the raw key`,
      );
    }
  }
});

test("the sentence names the resource, in the reader's language", () => {
  const en = translator("en");
  assert.equal(stepLabel("list_forms", en), "Reading your forms…");
  assert.equal(stepLabel("list_sales", en), "Reading your sales…");
  assert.equal(stepLabel("list_form_submissions", en), "Reading your form responses…");
  assert.equal(stepLabel(FILING_TOOL, en), "Filing that for you…");
  assert.equal(
    stepLabel("not_a_real_tool", en),
    "Working on it…",
    "an unknown tool must still say something — a blank status line reads as a broken page",
  );
});

test("the finished trail is past tense, and still names the resource", () => {
  const en = translator("en");
  assert.equal(stepLabel("list_forms", en, true), "Read your forms");
  assert.notEqual(
    stepLabel("list_forms", en, true),
    stepLabel("list_forms", en),
    "a finished step that reads like an unfinished one is the same lie as a settled card that keeps " +
      "asking for confirmation",
  );
});

test("the Malay sentence interpolates the Malay noun, not the English one", () => {
  assert.equal(stepLabel("list_forms", translator("ms")), "Membaca borang anda…");
});
