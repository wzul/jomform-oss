// THE ANSWER WHEN THE MODEL WROTE NOTHING — pinned from the WEB side (t_7f1694cb).
//
// THE DEFECT. On prod v1.21.149 a merchant asked, in plain English, for a form and
// got `reply: ""` with two refused propose_write steps. The page rendered an EMPTY
// assistant bubble, so the only content of the turn was the refusal block: the
// merchant was told their change could not be prepared and nothing else, with no
// hint of what to do next.
//
// The server half (api/internal/chat/chat.go, `Result.ReplyKey` + `answerOrKey`)
// names a dictionary sentence instead of passing the silence through, because the
// API does not know the visitor's language (issue #6177). This file pins the half
// that decides what the visitor actually sees, and the two halves together:
//
//   1. THE TWO HALVES MOVE TOGETHER. Every `chat.reply.*` key the Go runner can
//      emit must exist in the dictionary in all five locales — a missing entry
//      renders the raw KEY at HTTP 200 with nothing erroring, the same silent shape
//      as #499's `#ZgotmplZ` and as the refusal block's own missing translation.
//   2. THE DECISION IS EXECUTED, not scanned: a real answer beats the fallback, a
//      whitespace-only answer does not, and no answer with no key renders as
//      nothing rather than as the string "undefined".
//   3. THE PAGE USES THIS MODULE, and no longer passes the raw wire field through.
//      The pre-fix line (`content: d.reply ?? ""`) is asserted against first, so
//      the scan cannot pass by looking for something that was never there.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { answerText } from "./chatAnswer.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const page = read("../app/chat/page.tsx");
const goChat = read("../../../api/internal/chat/chat.go");

/** `t()` as the page gets it, over the real dictionary. */
const tIn =
  (lang: string) =>
  (k: string, params?: Record<string, string | number>): string => {
    const entry = DICT[k as keyof typeof DICT] as Record<string, string> | undefined;
    let s = entry ? entry[lang] || entry.en : k;
    if (params) for (const [name, v] of Object.entries(params)) s = s.replaceAll(`{${name}}`, String(v));
    return s as string;
  };

/** Every `"chat.reply.*"` literal the Go runner can put on the wire. */
function answerKeysInGo(src: string): string[] {
  return [...new Set([...src.matchAll(/"(chat\.reply\.[A-Za-z0-9_.]+)"/g)].map((m) => m[1]))];
}

test("the guard is looking at the real files (it cannot pass vacuously)", () => {
  assert.ok(
    goChat.includes("keyReplyNoAnswer") && goChat.includes("func (res *Result) answerOrKey("),
    "api/internal/chat/chat.go is not the file this guard means to judge — it no longer names an " +
      "answer key for a silent model",
  );
  // The pre-fix shape the page must NOT have any more. Asserted first, so the
  // negative check below cannot pass against a scanner that matches nothing.
  assert.ok(
    /content:\s*d\.reply \?\? ""/.test('          content: d.reply ?? "",'),
    "the scan cannot match the shipped page line — it would pass on the pre-fix page",
  );
  assert.ok(
    !/content:\s*d\.reply \?\? ""/.test(page),
    "app/chat/page.tsx passes the wire's reply straight through again — a model that wrote " +
      'nothing then leaves an EMPTY assistant bubble, which is the reported defect (t_7f1694cb)',
  );
  assert.ok(
    page.includes("answerText(d.reply, d.reply_key, t)") && page.includes("reply_key?: string"),
    "app/chat/page.tsx does not resolve the answer through lib/chatAnswer — the server's " +
      "reply_key would be inert",
  );
});

test("every answer key the SERVER can emit exists in the dictionary, in all five locales", () => {
  const keys = answerKeysInGo(goChat);
  assert.ok(
    keys.length >= 1,
    "extracted no chat.reply.* key from the Go source — the extractor has drifted from the " +
      "literals, so the completeness check below would be judging nothing",
  );
  for (const key of keys) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(
      entry,
      `${key} is emitted by the API but is NOT in the dictionary — the user would read the raw key ` +
        `"${key}" instead of an answer, at HTTP 200, with nothing erroring (t_7f1694cb)`,
    );
    for (const lang of LANGS) {
      assert.ok(
        String(entry![lang]).trim().length > 0,
        `${key}.${lang} is empty — the answer would be blank in that language`,
      );
    }
  }
});

test("a real answer beats the fallback, and the fallback beats silence", () => {
  const prose = "Saya tidak dapat menyediakan borang itu. Beritahu saya namanya.";
  for (const lang of LANGS) {
    const t = tIn(lang);
    // 1. The model's own words win, exactly as they arrived (no trimming: rewriting
    //    a model's Markdown answer is not this module's job).
    assert.equal(answerText(prose, "chat.reply.noAnswer", t), prose);
    assert.equal(answerText("  " + prose + "\n", undefined, t), "  " + prose + "\n");
    // 2. Silence falls back to the dictionary's sentence, in THIS language.
    const rendered = answerText("", "chat.reply.noAnswer", t);
    assert.equal(rendered, DICT["chat.reply.noAnswer"][lang]);
    assert.ok(!rendered.includes("chat.reply."), `the answer rendered the raw key in ${lang}`);
    // 3. Whitespace is silence — the runner makes the same call, so a model that
    //    emitted "\n\n" cannot leave an empty bubble either.
    assert.equal(answerText("\n\n  ", "chat.reply.noAnswer", t), DICT["chat.reply.noAnswer"][lang]);
    // 4. No prose AND no key is nothing. Never "undefined", never a sentence this
    //    module invented.
    assert.equal(answerText("", undefined, t), "");
    assert.equal(answerText(undefined, undefined, t), "");
    assert.equal(answerText("   ", "", t), "");
  }
  // 5. The sentence says the two things a refused proposal needs: nothing was
  //    changed, and what to send next.
  const en = DICT["chat.reply.noAnswer"].en as string;
  assert.ok(en.includes("nothing was changed"), `the fallback does not say nothing changed: ${en}`);
  assert.ok(en.includes("try again"), `the fallback gives the user no next step: ${en}`);
});
