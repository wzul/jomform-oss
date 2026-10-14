// The confirmation card's wording, pinned from the WEB side (card t_30ffa28a).
//
// THE DEFECT. On an English dashboard the confirmation card read half English and
// half Malay: the badge, the buttons and the "nothing has changed yet" hint came
// from the five-locale dictionary, while the title, the summary, every field label
// and every sentence the SERVER composed ("3 medan", "ya", "tiada had (infinite)")
// were Malay literals built in api/internal/chat/proposal.go. The API never
// receives the caller's language on a JSON call (issue #6177), so a sentence built
// there reached EVERY locale in one language — on the one surface where the wording
// IS the safety mechanism.
//
// WHAT THIS FILE PINS, and why each half is needed:
//
//   1. THE TWO HALVES MOVE TOGETHER. Every "chat.proposal.*" key the Go server can
//      emit must exist in the dictionary with all five locales. A key added on the
//      server without its translations renders the raw KEY to the user
//      (`chat.proposal.label.price` on the card) in every locale, at HTTP 200 with
//      nothing erroring — the same silent shape as the #499 `#ZgotmplZ` rewrite.
//      This is the check the commit that shipped the defect claimed to have.
//   2. THE DECISIONS THE PAGE MAKES ARE EXECUTED, not merely scanned: a label key
//      wins over a raw fallback, a composed value is interpolated through `t()`,
//      data is shown verbatim, and an absent value renders as EMPTY rather than the
//      string "undefined". This repo has no React/DOM runner (see
//      src/lib/i18n.test.ts), so the same decisions are taken in
//      src/lib/chatProposal.ts where a plain-node test can run them.
//   3. NOTHING PUTS A SERVER SENTENCE BACK ON THE CARD. The page must not print the
//      card's prose fields from the wire (`{p.title}`, `{f.label}`, …) — the shape
//      that shipped — and the Go renderer must not assign a literal sentence to a
//      label/title/value field. Both scans are proved against the PRE-FIX shape
//      first, so neither can pass by looking at nothing.
//
// The card's `note` is deliberately NOT covered here: it is the ASSISTANT's own
// wording in the language of the conversation (the reply above the card is in the
// same language and is not expected to follow the dashboard), so it has no key to
// look up. See lib/chatProposal.ts.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import {
  proposalLabel,
  proposalSummary,
  proposalTitle,
  proposalValue,
  type ProposalCardData,
  type ProposalField,
} from "./chatProposal.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

const page = read("../app/chat/page.tsx");
const goProposal = read("../../../api/internal/chat/proposal.go");
const goChat = read("../../../api/internal/chat/chat.go");

/**
 * `t()` as the page gets it, over the real dictionary, so a missing key shows up
 * here exactly as the user would see it (the key itself).
 */
const tIn =
  (lang: string) =>
  (k: string, params?: Record<string, string | number>): string => {
    const entry = DICT[k as keyof typeof DICT] as Record<string, string> | undefined;
    let s = entry ? (entry[lang] || entry.en) : k;
    if (params) for (const [name, v] of Object.entries(params)) s = s.replaceAll(`{${name}}`, String(v));
    return s as string;
  };

/** Every `"chat.proposal.*"` literal the Go server can put on a card. */
function proposalKeysInGo(src: string): string[] {
  return [...new Set([...src.matchAll(/"(chat\.proposal\.[A-Za-z0-9_.]+)"/g)].map((m) => m[1]))];
}

test("the guard is looking at the real files (it cannot pass vacuously)", () => {
  assert.ok(
    goProposal.includes("func Describe(") && goProposal.includes("proposalDocs"),
    "api/internal/chat/proposal.go is not the file this guard means to judge",
  );
  assert.ok(
    page.includes("chat-input") && page.includes("proposalLabel("),
    "app/chat/page.tsx is not the chat page, or it no longer renders a card label through " +
      "lib/chatProposal — this guard would then judge nothing",
  );
  const keys = proposalKeysInGo(goProposal);
  assert.ok(
    keys.length >= 40,
    `extracted only ${keys.length} chat.proposal.* key(s) from the Go source — the extractor has ` +
      `drifted from the key literals, so the completeness check below would be judging nothing`,
  );
});

test("every card key the SERVER can emit exists in the dictionary, in all five locales", () => {
  const keys = proposalKeysInGo(goProposal);
  // The key the server can emit for the step's detail is the same title key, read
  // from chat.go's own field so the two cannot drift apart silently.
  assert.ok(
    goChat.includes("DetailKey") && goChat.includes("card.TitleKey"),
    "the proposed step no longer carries the card's title KEY into steps[].detail_key — it used to " +
      "carry the Malay title string, which is the defect this card fixes",
  );
  for (const key of keys) {
    const entry = DICT[key as keyof typeof DICT] as Record<string, string> | undefined;
    assert.ok(
      entry,
      `${key} is emitted by the API but is NOT in the dictionary — the card would show the raw key ` +
        `"${key}" to the user in every locale, at HTTP 200, with nothing erroring (t_30ffa28a)`,
    );
    for (const lang of LANGS) {
      assert.ok(
        String(entry![lang]).trim().length > 0,
        `${key}.${lang} is empty — every locale has to carry the card's wording, or the card is ` +
          `blank in that language`,
      );
    }
  }
});

test("the page renders the card's own words from KEYS, never from a sentence on the wire", () => {
  // ── The pre-fix shape is what the scan must be able to see. ──
  const PROSE_IN_JSX = [/\{p\.title\}/, /\{p\.summary\}/, /\{f\.label\}/, /\{f\.value\}/];
  for (const re of PROSE_IN_JSX) {
    assert.ok(
      re.test("… {p.title} … {p.summary} … {f.label} … {f.value} …"),
      `the scan ${re} cannot match the shipped shape — it would pass on the pre-fix page`,
    );
    assert.ok(
      !re.test(page),
      `the chat page prints a card field straight off the wire (${re}) — that is the shape that put ` +
        `Malay prose on an English dashboard: the API does not know this visitor's language, so the ` +
        `card's wording is carried as dictionary KEYS (t_30ffa28a)`,
    );
  }
  // ── And the mechanism that replaced it is really used. ──
  for (const call of ["proposalTitle(p, t)", "proposalSummary(p, t)", "proposalLabel(f, t)", "proposalValue(f, t)"]) {
    assert.ok(
      page.includes(call),
      `app/chat/page.tsx no longer renders through ${call} — the card's wording must come from the ` +
        `dictionary, and a call site that bypasses this is how the defect comes back`,
    );
  }
  // The one string that is deliberately NOT a key: the assistant's own wording.
  assert.match(
    page,
    /\{p\.note\b/,
    "the card no longer renders the assistant's note — that string is the model's own wording in the " +
      "conversation's language (not the server's, and not the dashboard's), so losing it removes the " +
      "assistant's explanation rather than translating it",
  );
});

test("the Go renderer cannot assign a literal sentence to a card field", () => {
  // ── The scan, and its proof against the pre-fix shape. ──
  const LITERAL_FIELD = /(?:Label|LabelKey|LabelRaw|Value|ValueKey|TitleKey|SummaryKey|title|summary)\s*:\s*"([^"]*)"/g;
  const preFix = `fields = append(fields, ProposalField{Label: "Produk", Value: named(label, "product", id)})`;
  const sample = [...preFix.matchAll(LITERAL_FIELD)].map((m) => m[1]);
  assert.ok(
    sample.includes("Produk"),
    "the scan cannot see a literal label on the pre-fix renderer — it would pass on the code this " +
      "card exists to fix",
  );

  const literals = [...goProposal.matchAll(LITERAL_FIELD)].map((m) => m[1]);
  for (const lit of literals) {
    assert.ok(
      lit === "" || lit.startsWith("chat.proposal."),
      `api/internal/chat/proposal.go assigns the literal ${JSON.stringify(lit)} to a card field — a ` +
        `sentence here is written in ONE language by a server that never learns the caller's, which ` +
        `is exactly the defect. A card field carries a chat.proposal.* KEY, or ` +
        `label_raw (an argument key), or the merchant's own data`,
    );
  }
  assert.ok(
    proposalKeysInGo(goProposal).length >= 40,
    "the key table in proposal.go is missing or has shrunk — the card's wording would have nowhere to " +
      "come from",
  );
});

test("a label key wins, a raw key is shown as-is, and nothing renders 'undefined'", () => {
  const t = tIn("en");
  assert.equal(proposalLabel({ label_key: "chat.proposal.label.price" }, t), "Price");
  // A theme/argument key the dictionary does not cover: shown under its own name.
  assert.equal(proposalLabel({ label_key: "", label_raw: "brand_new_theme_key" }, t), "brand_new_theme_key");
  // Neither: empty, never the string "undefined" or a raw key.
  assert.equal(proposalLabel({ label_key: "" }, t), "");
});

test("a composed value is interpolated in the dashboard's language; data is verbatim", () => {
  for (const lang of LANGS) {
    const t = tIn(lang);
    const charges: ProposalField = {
      label_key: "chat.proposal.label.subscriptionCharges",
      value_key: "chat.proposal.value.chargeCount",
      value_params: { n: "12" },
    };
    const rendered = proposalValue(charges, t);
    assert.ok(
      rendered.includes("12"),
      `the composed charge count lost its number in ${lang}: ${rendered}`,
    );
    assert.ok(
      !rendered.includes("{"),
      `the composed sentence was not interpolated in ${lang}: ${rendered} — the card would show a ` +
        `placeholder to the user`,
    );
  }
  // The number alone is not the sentence: English says "{n} times", not "12".
  assert.equal(proposalValue({ label_key: "x", value_key: "chat.proposal.value.chargeCount", value_params: { n: "12" } }, tIn("en")), "12 times");
  // DATA stays data — translating a merchant's own value would be a lie about what
  // gets written.
  assert.equal(proposalValue({ label_key: "x", value: "QA Renamed Product" }, tIn("ar")), "QA Renamed Product");
  // And an absent value is empty, not "undefined".
  assert.equal(proposalValue({ label_key: "x" }, tIn("en")), "");
});

test("a count of ONE has its own entry, and English reads \"1 field\", not \"1 fields\" (t_212be8da)", () => {
  // ── THE BITE, restated as the pre-fix shape. The COUNT entry interpolated with
  // n=1 IS what an English merchant read on the card they were asked to confirm:
  // "Configuration — 1 fields". If this line ever stops reading that way, the
  // assertions below are no longer looking at the entry the server sends at n=1.
  assert.equal(
    proposalValue(
      { label_key: "chat.proposal.label.config", value_key: "chat.proposal.value.fieldCount", value_params: { n: "1" } },
      tIn("en"),
    ),
    "1 fields",
    "the count entry no longer reads \"1 fields\" at n=1 — the pre-fix shape this card " +
      "exists to fix is not what the test below is comparing against",
  );

  // ── The four count rows, as the server sends them at n=1 (one config field, one
  // notification entry, one variation, one charge). The English copy is pinned
  // here, in the dashboard's DEFAULT language, because that is where it read wrong.
  const oneRows: [ProposalField, string][] = [
    [{ label_key: "chat.proposal.label.config", value_key: "chat.proposal.value.fieldCountOne" }, "1 field"],
    [{ label_key: "chat.proposal.label.theme", value_key: "chat.proposal.value.fieldCountOne" }, "1 field"],
    [{ label_key: "chat.proposal.label.notifications", value_key: "chat.proposal.value.itemCountOne" }, "1 item"],
    [{ label_key: "chat.proposal.label.subscriptionCharges", value_key: "chat.proposal.value.chargeCountOne" }, "1 time"],
    [
      { label_key: "chat.proposal.label.variations", value_key: "chat.proposal.value.variationSetOne" },
      "1 row — the full list, replacing the existing one",
    ],
  ];
  for (const [row, wantEn] of oneRows) {
    assert.ok(
      goProposal.includes(`"${row.value_key}"`),
      `${row.value_key} is not a key api/internal/chat/proposal.go can emit — the server picks the ` +
        `singular entry by n (countedValue), so a singular entry nothing sends is a card that still ` +
        `reads "1 fields"`,
    );
    assert.equal(
      proposalValue(row, tIn("en")),
      wantEn,
      `the singular row ${row.value_key} does not read ${JSON.stringify(wantEn)} in English`,
    );
    // The singular entry takes no parameters from the server, so a {n} left in it
    // would reach the card as a literal placeholder in every locale.
    for (const lang of LANGS) {
      const rendered = proposalValue(row, tIn(lang));
      assert.ok(
        rendered.includes("1") && !rendered.includes("{"),
        `the singular row ${row.value_key} in ${lang} renders ${JSON.stringify(rendered)} — it must ` +
          `state the one itself and leave no placeholder behind (the server sends no value_params)`,
      );
    }
  }

  // ── The plural side is untouched: those entries interpolate the number.
  assert.equal(
    proposalValue(
      { label_key: "chat.proposal.label.config", value_key: "chat.proposal.value.fieldCount", value_params: { n: "3" } },
      tIn("en"),
    ),
    "3 fields",
  );
  assert.equal(
    proposalValue(
      { label_key: "chat.proposal.label.subscriptionCharges", value_key: "chat.proposal.value.chargeCount", value_params: { n: "0" } },
      tIn("en"),
    ),
    "0 times",
    "zero is the plural form in English (\"0 times\"), and the server sends the count entry for it",
  );

  // ── THE CLASS, not just these four rows: any value entry that interpolates a
  // number needs a singular sibling, in all five locales, without a placeholder of
  // its own. Discovered from the dictionary, so a count added later is caught here
  // rather than on the card. The list pair is excluded by its own names —
  // listOne/listMany already carry the singular case (describeList).
  const dict = DICT as unknown as Record<string, Record<string, string>>;
  const listPair = new Set(["chat.proposal.value.listOne", "chat.proposal.value.listMany"]);
  const counts = Object.keys(DICT).filter(
    (k) => k.startsWith("chat.proposal.value.") && !listPair.has(k) && LANGS.some((l) => dict[k]?.[l]?.includes("{n}")),
  );
  assert.ok(
    counts.length >= 4,
    `found ${counts.length} interpolated count entries (${counts.join(", ")}) — the scan has drifted from the ` +
      `dictionary and the singular check below would judge almost nothing`,
  );
  for (const k of counts) {
    const one = dict[`${k}One`];
    assert.ok(one, `${k} interpolates a number but has no ${k}One entry — at n=1 that card reads ` +
      `"1 fields" in English, the dashboard's default language`);
    for (const lang of LANGS) {
      const v = String(one[lang]).trim();
      assert.ok(v.length > 0, `${k}One.${lang} is empty — the card would be blank in that language`);
      assert.ok(
        !v.includes("{"),
        `${k}One.${lang} = ${JSON.stringify(v)} carries a placeholder, and the server sends NO ` +
          `value_params with the singular entry (countedValue) — it would print on the card`,
      );
    }
  }
});

test("the card's heading renders in the dictionary's language, and the assistant's note does not", () => {
  const card: ProposalCardData = {
    token: "t",
    args_hash: "h",
    tool: "update_product",
    title_key: "chat.proposal.title.update_product",
    summary_key: "chat.proposal.summary.update_product",
    fields: [{ label_key: "chat.proposal.label.name", value: "x" }],
    note: "Cadangan menukar nama produk.",
    expires_at: "2026-01-01T00:00:00Z",
    reversible: true,
  };
  assert.equal(proposalTitle(card, tIn("en")), "Update product");
  assert.equal(proposalTitle(card, tIn("ms")), "Kemas kini produk");
  assert.notEqual(proposalTitle(card, tIn("en")), proposalTitle(card, tIn("ar")));
  assert.notEqual(proposalSummary(card, tIn("en")), proposalSummary(card, tIn("ms")));
  // The whole card, rendered: no Malay sentence is left on it in English, which is
  // the defect restated as one assertion.
  const rendered = [
    proposalTitle(card, tIn("en")),
    proposalSummary(card, tIn("en")),
    ...(card.fields ?? []).map((f) => proposalLabel(f, tIn("en"))),
  ].join(" | ");
  assert.doesNotMatch(
    rendered,
    /(Kemas kini|Medan|Produk\b|Nama\b)/,
    `the English card still carries Malay server wording: ${rendered}`,
  );
});
