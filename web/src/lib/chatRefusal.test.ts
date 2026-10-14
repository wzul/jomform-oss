// A refused tool call's WHY, pinned from the WEB side (card t_3839995c).
//
// THE DEFECT. The confirmation card's own wording became dictionary keys in
// t_30ffa28a; the REFUSAL block on the same page was left behind. A refused step
// carried `detail` — a Malay sentence the RUNNER wrote in
// api/internal/chat/chat.go — and the page printed it verbatim under the block's
// one localized word. On an English dashboard the transcript read:
//
//   assistant tried — update_product — alat "update_product" tidak dibenarkan dalam chat ini
//
// The API never receives the caller's language on a JSON call (issue #6177), so a
// sentence built there reaches EVERY locale in one language — on the page whose
// other failure sentences (chat.fail / chat.confirmFail / chat.proposalArgsInvalid)
// all go through the dictionary.
//
// WHAT THIS FILE PINS, and why each half is needed:
//
//   1. THE TWO HALVES MOVE TOGETHER. Every "chat.refused.*" key the Go runner can
//      emit must exist in the dictionary with all five locales. A key added on the
//      server without its translations renders the raw KEY to the user, at HTTP
//      200, with nothing erroring — the same silent shape as the #499 `#ZgotmplZ`
//      rewrite. This is the check the commit that shipped the defect lacked.
//   2. THE DECISIONS THE PAGE MAKES ARE EXECUTED, not merely scanned: a key is
//      resolved in the dashboard's language, its parameters are interpolated, and a
//      step with no key renders as EMPTY rather than the string "undefined" or a
//      raw key. This repo has no React/DOM runner (see src/lib/i18n.test.ts), so
//      the decision lives in src/lib/chatRefusal.ts where plain node can run it.
//   3. NOTHING PUTS A SERVER SENTENCE BACK IN THE BLOCK. The page must not print a
//      step's prose field (`s.detail`, the shipped shape) and the Go Step must not
//      carry one — both scans are proved against the PRE-FIX shape first, so
//      neither can pass by looking at nothing.
//
// The Go half of the rule (every refusal cause reports a key, and no step field is
// a sentence) is api/internal/chat/refusal_i18n_test.go. Two halves of one block
// drifting apart is the defect, so they are pinned from both ends.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { refusalText, type RefusedStep } from "./chatRefusal.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const page = read("../app/chat/page.tsx");
const goChat = read("../../../api/internal/chat/chat.go");

/**
 * `t()` as the page gets it, over the real dictionary, so a missing key shows up
 * here exactly as the user would see it (the key itself).
 */
const tIn =
  (lang: string) =>
  (k: string, params?: Record<string, string | number>): string => {
    const entry = DICT[k as keyof typeof DICT] as Record<string, string> | undefined;
    let s = entry ? entry[lang] || entry.en : k;
    if (params) for (const [name, v] of Object.entries(params)) s = s.replaceAll(`{${name}}`, String(v));
    return s as string;
  };

/** Every `"chat.refused.*"` literal the Go runner can put on a step. */
function refusalKeysInGo(src: string): string[] {
  return [...new Set([...src.matchAll(/"(chat\.refused\.[A-Za-z0-9_.]+)"/g)].map((m) => m[1]))];
}

test("the guard is looking at the real files (it cannot pass vacuously)", () => {
  assert.ok(
    goChat.includes("func (r *Runner) Run(") && goChat.includes("keyRefusedWriteTool"),
    "api/internal/chat/chat.go is not the file this guard means to judge",
  );
  assert.ok(
    page.includes("refusalText(") && page.includes('t("chat.refused")'),
    "app/chat/page.tsx is not the chat page, or it no longer renders a refusal through " +
      "lib/chatRefusal — this guard would then judge nothing",
  );
  const keys = refusalKeysInGo(goChat);
  assert.ok(
    keys.length >= 6,
    `extracted only ${keys.length} chat.refused.* key(s) from the Go source — the extractor has ` +
      `drifted from the key literals, so the completeness check below would be judging nothing`,
  );
});

test("every refusal key the SERVER can emit exists in the dictionary, in all five locales", () => {
  for (const key of refusalKeysInGo(goChat)) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(
      entry,
      `${key} is emitted by the API but is NOT in the dictionary — the refusal would show the raw ` +
        `key "${key}" to the user in every locale, at HTTP 200, with nothing erroring (t_3839995c)`,
    );
    for (const lang of LANGS) {
      assert.ok(
        String(entry![lang]).trim().length > 0,
        `${key}.${lang} is empty — every locale has to carry the refusal's wording, or the block ` +
          `is blank in that language`,
      );
    }
  }
});

test("the page renders a refusal's WHY from the key, never from a sentence on the wire", () => {
  // ── The pre-fix shape is what the scan must be able to see. ──
  const PRE_FIX_BLOCK = '… {s.tool}{s.detail ? ` — ${s.detail}` : ""} …';
  const RAW_DETAIL = /\bs\.detail\b/;
  assert.ok(
    RAW_DETAIL.test(PRE_FIX_BLOCK),
    "the scan cannot match the shipped shape — it would pass on the pre-fix page",
  );
  assert.ok(
    !RAW_DETAIL.test(page),
    "the chat page prints a step's raw detail off the wire — that is the shape that put Malay prose " +
      "on an English dashboard: the API does not know this visitor's language, so a refusal's why is " +
      "carried as a dictionary KEY (t_3839995c)",
  );
  // ── And the mechanism that replaced it is really used. ──
  assert.ok(
    page.includes("const why = refusalText(s, t)") && page.includes("{why ? ` — ${why}` : \"\"}"),
    "app/chat/page.tsx no longer renders the refusal's why through refusalText — a call site that " +
      "bypasses lib/chatRefusal is how the defect comes back",
  );
  // ── The Step TYPE must not offer a prose field for the next call site to print. ──
  const PROSE_FIELD = /\bdetail\??:\s*string/;
  assert.ok(
    PROSE_FIELD.test("type Step = { tool: string; status: string; detail?: string };"),
    "the scan cannot match the pre-fix Step type — it would pass on the shipped interface",
  );
  assert.ok(
    !PROSE_FIELD.test(page),
    "app/chat/page.tsx's Step still declares a prose `detail` field. The absence of that field is " +
      "what makes the defect unrepresentable rather than merely fixed: there is nothing left for a " +
      "call site to print by accident (t_3839995c)",
  );
});

test("the Go Step cannot carry a sentence either", () => {
  // The field named `detail` is gone; its keyed replacements are not a match for
  // this rule (`DetailKey string` / `DetailParams map[…]` both fail `\bDetail\b`).
  const PROSE_FIELD = /\bDetail\b\s+string/;
  assert.ok(
    PROSE_FIELD.test('Detail string `json:"detail,omitempty"`'),
    "the scan cannot match the pre-fix Step field — it would pass on the shipped struct",
  );
  assert.ok(
    !PROSE_FIELD.test(goChat),
    "api/internal/chat/chat.go's Step still carries a prose `Detail` string. The server does not " +
      "know the caller's language (issue #6177), so that is one language on every dashboard " +
      "(t_3839995c) — the step carries a chat.refused.* KEY instead",
  );
});

test("a refusal's sentence is the DICTIONARY's, in the dashboard's language", () => {
  const step: RefusedStep = { tool: "update_product", status: "refused", detail_key: "chat.refused.writeTool" };
  for (const lang of LANGS) {
    const rendered = refusalText(step, tIn(lang));
    assert.equal(
      rendered,
      DICT["chat.refused.writeTool"][lang],
      `the refusal did not render the dictionary's ${lang} string`,
    );
    assert.ok(
      !rendered.includes("chat.refused."),
      `the refusal rendered the raw key in ${lang}: ${rendered}`,
    );
  }
  // The tool name stays the IDENTIFIER it is: the page prints it beside the
  // sentence, so it is never translated and never folded into the sentence.
  assert.equal(step.tool, "update_product");
});

test("a sentence's parameters are interpolated, and a keyless step renders as NOTHING", () => {
  const budget: RefusedStep = {
    tool: "propose_write",
    status: "refused",
    detail_key: "chat.refused.proposalLimit",
    detail_params: { max: "3" },
  };
  for (const lang of LANGS) {
    const rendered = refusalText(budget, tIn(lang));
    assert.ok(rendered.includes("3"), `the budget refusal lost its number in ${lang}: ${rendered}`);
    assert.ok(
      !rendered.includes("{"),
      `the budget sentence was not interpolated in ${lang}: ${rendered} — the user would read a ` +
        `placeholder`,
    );
  }
  // An absent key is a step with no why: empty, never "undefined" and never a raw
  // key. The page prints the tool name beside it, so the line still says what the
  // assistant tried.
  assert.equal(refusalText({ tool: "x", status: "refused" }, tIn("en")), "");
  assert.equal(refusalText({ tool: "x", status: "refused", detail_key: "" }, tIn("en")), "");
});
