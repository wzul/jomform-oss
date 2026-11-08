// The empty state's starter chips (card t_148d7100).
//
// THE DEFECT. The empty chat was one sentence describing what a user COULD type
// ("Ask something like \"which of my forms are live?\"…") with nothing to press.
//
// WHAT THIS FILE OWNS: the chips are a promise. Each one is a question the
// assistant can really answer — that is a claim about the SERVER's read allowlist,
// which lives in Go — and each is SENT verbatim, so it must exist in all five
// locales. A chip added without a translation sends the raw key to the model; a
// chip promising data the allowlist does not serve sends the user to a refusal.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { CHAT_STARTERS } from "./chatStarters.ts";

const goChatTools = readFileSync(
  fileURLToPath(new URL("../../../api/internal/mcpsrv/chat_tools.go", import.meta.url)),
  "utf8",
);

test("three or four chips, as the card asks", () => {
  assert.ok(
    CHAT_STARTERS.length >= 3 && CHAT_STARTERS.length <= 4,
    `${CHAT_STARTERS.length} chips: an empty state needs a few choices, not a menu`,
  );
});

test("every chip exists in all five locales", () => {
  for (const key of CHAT_STARTERS) {
    const entry = DICT[key as keyof typeof DICT];
    assert.ok(entry, `${key} is not in the dictionary — the chip would render as a raw key`);
    for (const lang of LANGS) {
      const s = (entry as Record<string, string>)[lang];
      // NOT a character count: "你能做什么？" is seven characters and perfectly
      // usable, so a length threshold that feels safe in English would fail a
      // correct Chinese chip (and any short Malay or Tamil one).
      assert.ok(s && s.trim() !== "" && s !== key, `${key} has no usable ${lang} sentence`);
    }
  }
});

test("a chip is a QUESTION, not a label", () => {
  for (const key of CHAT_STARTERS) {
    const en = (DICT[key as keyof typeof DICT] as Record<string, string>).en;
    assert.match(en, /\?$/, `${key} (${en}) does not read as a question — the chip is sent as the message`);
  }
});

test("the chips are distinct, so the transcript cannot get two identical questions", () => {
  const seen = CHAT_STARTERS.map((k) => (DICT[k as keyof typeof DICT] as Record<string, string>).en);
  assert.equal(new Set(seen).size, seen.length);
});

test("each chip's question is one the assistant's read allowlist answers", () => {
  // The three data questions name a resource the chat may read. `list_forms`,
  // `list_sales` and `list_form_submissions` are the reads behind them; the
  // fourth chip asks what the assistant can do, which needs no tool at all.
  const allow = [...goChatTools.match(/var chatReadTools = \[\]string\{([\s\S]*?)\n\}/)![1].matchAll(/"([a-z0-9_]+)"/g)].map(
    (m) => m[1],
  );
  for (const tool of ["list_forms", "list_sales", "list_form_submissions"]) {
    assert.ok(
      allow.includes(tool),
      `${tool} is no longer in the chat's read allowlist, so the chip that asks about it would be refused`,
    );
  }
});
