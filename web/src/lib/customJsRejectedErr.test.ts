// Card t_2717ba30 — /form-settings showed only "Failed" when the server refused a
// blocked theme.custom_js.
//
// THE DEFECT, measured on prod v1.21.296. Pasting `eval('2+2');` into the General
// tab's "Custom JS (power users)" field and pressing Save answered
//
//   PATCH /api/rest/forms/<id>?workspace=<ws>
//     400 {"code":"request.badRequest","error":"custom_js ditolak: active code execution marker: eval("}
//
// and the page rendered p[data-testid="action-msg"] with text "Failed". The refusal
// was correct (nothing was written), but the message named neither the control nor
// the reason: the API's sentence is assembled around the scan's reason, so it had
// no specific code and fell through to the HTTP class code this dictionary
// deliberately does not translate.
//
// WHAT THIS PINS
//   1. `form.customJsRejected` is MAPPED — so the page's `apiErr(d, t("prof.failed"), t)`
//      resolves to a real sentence instead of its generic fallback.
//   2. That sentence names the Custom JS FIELD (and the pattern families the scan
//      blocks), in every locale, and is not just the fallback echoed back.
//   3. The generic fallback still holds where it must: a class code
//      (`request.badRequest`) and a body with no code at all both keep the page's
//      own "Failed" copy — the new mapping must not swallow other 400s, and the
//      server's Malay prose must never be printed (#6177).
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";

import { API_ERROR_KEYS, apiErr } from "./apiErr.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const CODE = "form.customJsRejected";
const KEY = "apiErr.formCustomJsRejected";

/** The dictionary lookup, per locale — the same `t` the page passes in. */
const tIn = (lang: string) => (k: string) => {
  const entry = (DICT as Record<string, Record<string, unknown>>)[k];
  assert.ok(entry, `missing dictionary key ${k}`);
  const v = entry[lang];
  return typeof v === "string" ? v : (entry.en as string);
};

// The real body the API answers for a blocked value. `error` is the Malay prose
// that must never reach the screen.
const REFUSAL = {
  code: CODE,
  error: "custom_js ditolak: active code execution marker: eval(",
};

test("the custom_js refusal code is mapped to its own sentence", () => {
  assert.equal(
    API_ERROR_KEYS[CODE],
    KEY,
    `${CODE} has no dictionary mapping — the page would render its generic "Failed" ` +
      `for a refusal it can name exactly`,
  );
  const entry = (DICT as Record<string, Record<string, unknown>>)[KEY];
  assert.ok(entry, `dictionary key ${KEY} is missing`);
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.equal(typeof v, "string", `${KEY}.${lang} is missing`);
    assert.ok((v as string).trim().length > 0, `${KEY}.${lang} is empty`);
  }
});

test("the sentence names the Custom JS field and the reason, in every locale", () => {
  const generic = "Failed";
  for (const lang of LANGS) {
    const rendered = apiErr(REFUSAL, generic, tIn(lang));
    assert.notEqual(
      rendered,
      generic,
      `lang=${lang}: a known code rendered the page's generic fallback`,
    );
    assert.ok(
      !rendered.includes("custom_js ditolak"),
      `lang=${lang}: the API's Malay prose reached the screen: ${JSON.stringify(rendered)}`,
    );
    // The field name is the part a generic "Failed" cannot say — the merchant has
    // to know WHICH control the server refused. It is a product term, so it is
    // spelled the same in every locale (a partial localisation may qualify it).
    assert.ok(
      rendered.includes("Custom JS"),
      `lang=${lang}: the sentence does not name the Custom JS field: ${JSON.stringify(rendered)}`,
    );
    // And the reason class: one of the three families the scan blocks.
    assert.ok(
      /code execution|执行|pelaksanaan|تنفيذ/i.test(rendered),
      `lang=${lang}: the sentence does not name the blocked pattern class: ${JSON.stringify(rendered)}`,
    );
  }
});

test("other 400s from the same PATCH keep the page's generic fallback", () => {
  // A class code is deliberately untranslated (apiErr.ts's module doc): the page's
  // own sentence is better than a vague generic one.
  const classCode = { code: "request.badRequest", error: "product_image_size mesti small|medium|large|hidden" };
  for (const lang of LANGS) {
    assert.equal(
      apiErr(classCode, "Failed", tIn(lang)),
      "Failed",
      `lang=${lang}: a class code was translated — the new sentence must fire for the ` +
        `custom_js refusal only, or a layout/colour mistake would be reported as rejected JS`,
    );
  }

  // No code at all (a proxy error page, an older endpoint): fallback, never prose.
  assert.equal(
    apiErr({ error: "custom_js ditolak: active code execution marker: eval(" }, "Failed", tIn("en")),
    "Failed",
    "a body with no code rendered something other than the page's own sentence",
  );
});
