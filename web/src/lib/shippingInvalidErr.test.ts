// Card t_417fbacb — /form-settings' Shipping tab showed only "Failed to save
// shipping rates" for a refusal that names a field.
//
// THE DEFECT, measured on prod v1.21.345. Deleting the ONLY zone set and pressing
// "Save shipping rates" answered
//
//   PUT /api/rest/shipping?workspace=<ws>  {"sets":[]}
//     400 {"code":"request.badRequest","error":"sekurang-kurangnya satu set zon diperlukan"}
//
// and the merchant read the page's own fallback sentence with no reason at all;
// the set was back after a reload. Two halves had to move together:
//
//   1. the API must ACCEPT the empty payload (pinned by
//      api/internal/httpapi/rest_shipping_test.go and
//      api/internal/dbstore/shipping_test.go), and
//   2. a save it still REFUSES — a set with no name, a set with no zone, a zone
//      with no rate — must reach the merchant as a sentence about the zones,
//      which needs a machine code the server can name. ValidateShippingSets wraps
//      every shape refusal in dbstore.ShippingInvalidLeadIn, the REST edge keys
//      `shipping.invalid` on that lead-in, and this file pins the dashboard half.
//
// WHAT THIS PINS
//   1. `shipping.invalid` is MAPPED, so the page's `apiErr(d, t("fs.shipSaveFail"), t)`
//      resolves to a real sentence instead of its generic fallback.
//   2. That sentence exists in EVERY locale and is not the fallback echoed back;
//      it names the rules a save has to satisfy (a set name, a zone, a rate).
//   3. The page's save call site still passes the response BODY through apiErr
//      with the shipping fallback — a mapping nothing calls is dead, and this is
//      the one line that decides whether the merchant sees the sentence.
//   4. The generic fallback still holds where it must: a class code
//      (`request.badRequest`) and a body with no code both keep the page's own
//      copy, and the server's Malay prose is never printed (#6177).
//   5. `shipping.setInUse` keeps its own sentence — the new row must not shadow
//      the other shipping refusal.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { API_ERROR_KEYS, apiErr } from "./apiErr.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const CODE = "shipping.invalid";
const KEY = "apiErr.shipInvalid";
const FALLBACK = "Failed to save shipping rates";

/** The dictionary lookup, per locale — the same `t` the page passes in. */
const tIn = (lang: string) => (k: string) => {
  const entry = (DICT as Record<string, Record<string, unknown>>)[k];
  assert.ok(entry, `missing dictionary key ${k}`);
  const v = entry[lang];
  return typeof v === "string" ? v : (entry.en as string);
};

/** The page source, so the call site can be pinned (a mapping is dead otherwise). */
const PAGE = readFileSync(
  fileURLToPath(new URL("../app/form-settings/page.tsx", import.meta.url)),
  "utf8",
);

// The real body the API answers for a configuration it cannot resolve: the prose
// names the zone, and must never reach the screen.
const REFUSAL = {
  code: CODE,
  error: "kadar penghantaran tidak sah — zon \"West Malaysia\" belum ada kadar",
};

test("the shipping shape-refusal code is mapped to its own sentence", () => {
  assert.equal(
    API_ERROR_KEYS[CODE],
    KEY,
    `${CODE} has no dictionary mapping — the page would render its generic ` +
      `"${FALLBACK}" for a refusal whose reason it can explain`,
  );
  const entry = (DICT as Record<string, Record<string, unknown>>)[KEY];
  assert.ok(entry, `dictionary key ${KEY} is missing`);
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.equal(typeof v, "string", `${KEY}.${lang} is missing`);
    assert.ok((v as string).trim().length > 0, `${KEY}.${lang} is empty`);
  }
});

test("the sentence names what a shipping save requires, in every locale", () => {
  for (const lang of LANGS) {
    const rendered = apiErr(REFUSAL, FALLBACK, tIn(lang));
    assert.notEqual(rendered, FALLBACK, `lang=${lang}: a known code rendered the page's fallback`);
    assert.ok(
      !rendered.includes("kadar penghantaran tidak sah"),
      `lang=${lang}: the API's Malay prose reached the screen: ${JSON.stringify(rendered)}`,
    );
    // The three rules a save has to satisfy. One product term per locale keeps
    // the assertion honest about WHICH sentence rendered (a copy of the fallback,
    // or another shipping sentence, would fail here).
    const term: Record<string, RegExp> = {
      en: /zone set/i,
      ms: /set zon/i,
      zh: /区域组/,
      ta: /மண்டல தொகுப்ப/,
      ar: /مجموعة مناطق/,
    };
    assert.ok(
      term[lang].test(rendered),
      `lang=${lang}: the sentence does not name the zone set that has to be fixed: ` +
        `${JSON.stringify(rendered)}`,
    );
    // …and the rate, which is the part a server refusal most often names.
    const rate: Record<string, RegExp> = {
      en: /rate/i,
      ms: /kadar/i,
      zh: /费率/,
      ta: /கட்டணம்/,
      ar: /سعر/,
    };
    assert.ok(
      rate[lang].test(rendered),
      `lang=${lang}: the sentence does not name the rate: ${JSON.stringify(rendered)}`,
    );
  }
});

test("the Shipping tab's save passes the body through apiErr with its own fallback", () => {
  // The wiring half. `showMsgFail(apiErr(d, t("fs.shipSaveFail"), t))` is the
  // exact statement that decides whether the new sentence is reachable; a save
  // that printed its own literal instead would leave this mapping dead.
  assert.ok(
    PAGE.includes('showMsgFail(apiErr(d, t("fs.shipSaveFail"), t))'),
    "form-settings' saveShipping no longer routes the response body through apiErr — " +
      "the shipping.invalid sentence cannot reach the merchant",
  );
  // The catch branch (no response at all) has nothing to translate and must keep
  // the page's own sentence.
  assert.ok(
    PAGE.includes('showMsgFail(t("fs.shipSaveFail"))'),
    "the network-failure branch lost its own sentence",
  );
});

test("other refusals from the same save keep the page's generic fallback", () => {
  // A class code is deliberately untranslated (apiErr.ts's module doc): the
  // page's own sentence is better than a vague generic one, and this new mapping
  // must not swallow it.
  const classCode = { code: "request.badRequest", error: "butiran tidak lengkap" };
  for (const lang of LANGS) {
    assert.equal(
      apiErr(classCode, FALLBACK, tIn(lang)),
      FALLBACK,
      `lang=${lang}: a class code was translated — the new sentence must fire for the ` +
        `shipping shape refusal only`,
    );
  }
  assert.equal(
    apiErr({ error: "kadar penghantaran tidak sah — zon \"West\" belum ada kadar" }, FALLBACK, tIn("en")),
    FALLBACK,
    "a body with no code rendered something other than the page's own sentence",
  );
});

test("the other shipping refusal keeps its own sentence", () => {
  // The new row must not shadow `shipping.setInUse`: its remedy (re-point the
  // form, or keep the set) is different from "fix the zone".
  const inUse = apiErr({ code: "shipping.setInUse" }, FALLBACK, tIn("en"));
  assert.notEqual(inUse, FALLBACK, "shipping.setInUse lost its translation");
  assert.ok(
    !/zone set needs a name/i.test(inUse),
    `shipping.setInUse rendered the shape-refusal sentence: ${JSON.stringify(inUse)}`,
  );
});
