// Guard for the dashboard half of card t_f76f81eb: the Form Settings slug field
// must not offer a value the API refuses.
//
// THE DEFECT. The field normalised what was typed (lower-case, invalid runs to
// "-") and sent it; the API had no form-slug validator on the create path and
// only a SHAPE check on rename. Measured on prod v1.25.0:
//
//	POST  /v1/rest/forms      {"slug":"bad slug!!/x"} -> 201 (a public URL with raw
//	                                                    spaces and an extra segment)
//	PATCH /v1/rest/forms/{id} {"slug":"login"}        -> 200 (a label JomForm owns)
//
// WHAT THIS FILE PINS
//   1. The rule itself on the values that matter, including that a reserved name
//      is refused while ordinary merchant slugs are not (no blanket denial).
//   2. The mirror cannot drift: the reserved list and the length cap are READ OUT
//      of the Go sources and compared with the TypeScript constants, in both
//      directions — a label added on one side alone fails here.
//   3. Every reserved label is itself a well-formed slug inside the cap, which is
//      why the order of the legs is not observable between the two languages.
//   4. The Form Settings page really applies it: the input normalises through the
//      shared helper, the save REFUSES before sending, and the refusal is rendered
//      from the dashboard dictionary (never the server's English prose).
//
// Run with the repo's Node test runner:  npm run test:unit
import assert from "node:assert/strict";
import test from "node:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

import {
  FORM_SLUG_CHARSET,
  FORM_SLUG_MAX,
  FORM_SLUG_RESERVED,
  FORM_SLUG_RESERVED_CODE,
  FORM_SLUG_RESERVED_PREFIX,
  formSlugBlocked,
  formSlugProblem,
  isReservedFormSlug,
  normalizeFormSlug,
} from "./formSlug.ts";
import { API_ERROR_KEYS } from "./apiErr.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const webSrc = path.join(here, "..");
const repoRoot = path.join(webSrc, "..", "..");
const urlguardDir = path.join(repoRoot, "api", "internal", "urlguard");
const dbstoreDir = path.join(repoRoot, "api", "internal", "dbstore");
const formSettings = path.join(webSrc, "app", "form-settings", "page.tsx");

/** Keys of a `var <name> = map[string]string{ … }` literal in a Go source file. */
function goMapKeys(src: string, varName: string): string[] {
  const start = src.indexOf(`var ${varName} = map[string]string{`);
  assert.ok(start >= 0, `${varName} is not declared as a map[string]string literal any more`);
  const end = src.indexOf("\n}", start);
  assert.ok(end > start, `${varName}'s literal is not terminated as expected`);
  const body = src.slice(start, end);
  const keys: string[] = [];
  for (const m of body.matchAll(/^\t"([a-z0-9-]+)":/gm)) keys.push(m[1]);
  return keys.sort();
}

/** The set of labels the Go rule refuses, read out of the two source files. */
function goReservedLabels(): string[] {
  const subdomains = readFileSync(path.join(urlguardDir, "subdomains.go"), "utf8");
  const formSlug = readFileSync(path.join(urlguardDir, "form_slug.go"), "utf8");
  const union = new Set([...goMapKeys(subdomains, "reservedSubdomains"), ...goMapKeys(formSlug, "reservedFormSlugPaths")]);
  return [...union].sort();
}

/** The length cap the Go validator enforces. */
function goMaxLen(): number {
  const src = readFileSync(path.join(dbstoreDir, "form.go"), "utf8");
  const m = /const formSlugMaxLen = (\d+)/.exec(src);
  assert.ok(m, "dbstore/form.go no longer declares `const formSlugMaxLen = <n>`");
  return Number(m[1]);
}

/** The reserved prefix the Go rule applies on top of the label list. */
function goReservedPrefix(): string {
  const src = readFileSync(path.join(urlguardDir, "subdomains.go"), "utf8");
  const m = /const reservedSubdomainPrefix = "([a-z0-9-]+)"/.exec(src);
  assert.ok(m, "urlguard/subdomains.go no longer declares reservedSubdomainPrefix");
  return m[1];
}

/** Difference report between the Go labels and the TypeScript mirror. */
function listDrift(goLabels: string[], webLabels: readonly string[]): string[] {
  const go = new Set(goLabels);
  const web = new Set(webLabels);
  return [
    ...goLabels.filter((l) => !web.has(l)).map((l) => `missing from web/src/lib/formSlug.ts: ${l}`),
    ...webLabels.filter((l) => !go.has(l)).map((l) => `not refused by the Go rule: ${l}`),
  ];
}

test("the guard sees the real app and the real Go rule (it cannot pass vacuously)", () => {
  assert.ok(FORM_SLUG_RESERVED.length >= 20, `FORM_SLUG_RESERVED shrank to ${FORM_SLUG_RESERVED.length} labels`);
  const go = goReservedLabels();
  assert.ok(go.length >= 20, `parsed only ${go.length} Go labels — the parser is not reading the maps`);
  assert.ok(go.includes("login"), `the parsed Go list has no "login": ${go.join(",")}`);
  assert.ok(go.includes("privacy"), `the parsed Go list has no "privacy" (the path leg): ${go.join(",")}`);
  assert.ok(goMaxLen() >= 1, "the parsed length cap is not a real number");
  assert.match(readFileSync(formSettings, "utf8"), /formSlugProblem/, "the Form Settings page no longer applies the rule");
});

test("the reserved list is exactly the Go list — drift in EITHER direction fails", () => {
  const drift = listDrift(goReservedLabels(), FORM_SLUG_RESERVED);
  assert.deepEqual(
    drift,
    [],
    `the dashboard's reserved-slug list and the API's disagree:\n  ${drift.join("\n  ")}\n` +
      "A label the API refuses but the field offers is a 400 after the save (the defect); a label the " +
      "field refuses but the API accepts is a slug the merchant can never use from the dashboard.",
  );
  // BITE PROOF, in memory only: the predicate really reports a planted difference.
  const planted = listDrift([...goReservedLabels(), "totally-new-surface"], FORM_SLUG_RESERVED);
  assert.equal(planted.length, 1, "the drift predicate did not report a planted Go-only label");
  assert.match(planted[0], /totally-new-surface/);
});

test("the length cap and the reserved prefix match the Go rule", () => {
  assert.equal(FORM_SLUG_MAX, goMaxLen(), "FORM_SLUG_MAX drifted from dbstore.formSlugMaxLen");
  assert.equal(FORM_SLUG_RESERVED_PREFIX, goReservedPrefix(), "the jomform- prefix drifted");
  assert.equal(FORM_SLUG_RESERVED_CODE, "form.slugReserved", "the reserved code token moved");
  // The prefix is applied, not just declared: a future surface under it is refused.
  assert.equal(isReservedFormSlug("jomform-future-thing"), true);
});

test("the rule refuses what the API refuses, and accepts what merchants use", () => {
  const cases: Array<[string, ReturnType<typeof formSlugProblem>]> = [
    ["login", "reserved"],
    ["Login", "reserved"],
    ["privacy", "reserved"],
    ["api", "reserved"],
    ["jomform-anything", "reserved"],
    ["", "length"],
    ["a".repeat(FORM_SLUG_MAX + 1), "length"],
    ["bad slug!!/x", "chars"],
    ["has_underscore", "chars"],
    ["kedai-kopi", null],
    ["order", null],
    ["menu", null],
    ["a".repeat(FORM_SLUG_MAX), null],
  ];
  for (const [slug, want] of cases) {
    assert.equal(formSlugProblem(slug), want, `formSlugProblem(${JSON.stringify(slug)})`);
  }

  // Every refused label is itself well formed and inside the cap. That is what
  // makes the leg order unobservable between the two languages (see formSlug.ts).
  for (const label of FORM_SLUG_RESERVED) {
    assert.match(label, FORM_SLUG_CHARSET, `reserved label ${label} is not charset-valid`);
    assert.ok(label.length <= FORM_SLUG_MAX, `reserved label ${label} is longer than the cap`);
  }

  // The normaliser is what makes the field unable to OFFER a bad shape: it
  // rewrites as the merchant types, which is the same contract as before.
  assert.equal(normalizeFormSlug("Kedai Kopi!!"), "kedai-kopi");
  assert.equal(normalizeFormSlug("  --Menu  "), "menu");
  assert.equal(normalizeFormSlug("a".repeat(60)), "a".repeat(FORM_SLUG_MAX));
  assert.equal(formSlugProblem(normalizeFormSlug("bad slug!!/x")), null);
});

test("the refusal renders from the dictionary, never the server's English prose", () => {
  // The two refusals must stay two sentences: their remedies differ.
  const pageSrc = readFileSync(formSettings, "utf8");
  assert.ok(
    pageSrc.includes('t("apiErr.formSlugReserved")') && pageSrc.includes('t("apiErr.formSlugInvalid")'),
    "the Form Settings page no longer renders BOTH slug refusals from the dictionary — a refused save then " +
      "shows either the server's English prose or one blanket sentence",
  );

  // The code is mapped by the shared table, and every locale has the sentence.
  assert.ok("form.slugReserved" in API_ERROR_KEYS, "form.slugReserved is not mapped in lib/apiErr.ts");
  const key = API_ERROR_KEYS["form.slugReserved"];
  assert.equal(key, "apiErr.formSlugReserved");
  const entry = (DICT as Record<string, Record<string, unknown>>)[key];
  assert.ok(entry, `i18nDict.ts has no entry for ${key}`);
  for (const lang of LANGS) {
    const v = entry[lang];
    assert.ok(
      typeof v === "string" && v.trim() !== "",
      `${key}.${lang} is empty — that locale silently shows English`,
    );
    assert.ok(!String(v).includes("JomForm uses that name for its own addresses"), `${key}.${lang} prints the API's prose`);
  }
  // The reserved sentence must be distinguishable from the shape sentence: the
  // remedies differ ("change the shape" vs "pick another name").
  assert.notEqual(
    (DICT as Record<string, Record<string, unknown>>)["apiErr.formSlugReserved"].en,
    (DICT as Record<string, Record<string, unknown>>)["apiErr.formSlugInvalid"].en,
  );
});

test("the save decision blocks a refused slug, but never an existing row's own", () => {
  // A merchant typing a reserved name over a normal one: refused.
  assert.equal(formSlugBlocked("login", "western-menu"), "reserved");
  assert.equal(formSlugBlocked("privacy", "western-menu"), "reserved");
  assert.equal(formSlugBlocked("", "western-menu"), "length");
  // Unchanged and legitimate: allowed.
  assert.equal(formSlugBlocked("western-menu", "western-menu"), null);
  // A row stored BEFORE the rule keeps its slug — re-saving its settings is not
  // a new claim, and blocking it would make the page unusable for that form.
  assert.equal(formSlugBlocked("login", "login"), null);
  assert.equal(formSlugBlocked("Login", "login"), null);
  // Claiming it again after moving away is still a refusal.
  assert.equal(formSlugBlocked("login", "somewhere-else"), "reserved");
});

test("the Form Settings save REFUSES a refused slug before sending it", () => {
  const src = readFileSync(formSettings, "utf8");
  const start = src.indexOf("async function saveGeneral()");
  assert.ok(start >= 0, "app/form-settings/page.tsx no longer defines saveGeneral — the slug save path is gone");
  const end = src.indexOf("\n  async function ", start + 10);
  const body = src.slice(start, end > start ? end : start + 8000);

  // The gate runs the SHARED decision on the value about to be sent, and its
  // refusal returns before the request. The whole three-line shape is pinned:
  // a guard that is inverted, commented out, or moved after the PATCH must fail
  // here, because the decision alone (tested above) would still look right.
  assert.match(
    body,
    /const nextSlug = normalizeFormSlug\(slug\);\s*\n\s*const blocked = formSlugBlocked\(nextSlug, form\?\.slug \|\| ""\);\s*\n\s*if \(blocked\) \{\s*\n\s*showMsgFail\(slugProblemText\(blocked\)\);\s*\n\s*return;\s*\n\s*\}/,
    "saveGeneral no longer refuses a refused slug before sending it (expected: normalise -> formSlugBlocked -> " +
      "showMsgFail + return, all before the PATCH)",
  );

  // And it really is before the PATCH.
  const gateAt = body.indexOf("formSlugBlocked(nextSlug");
  const patchAt = body.indexOf("method: \"PATCH\"");
  assert.ok(gateAt >= 0, "saveGeneral no longer applies the shared rule before saving");
  assert.ok(patchAt > gateAt, "the slug rule is applied AFTER the PATCH — the field still offers the value");

  // The payload carries the NORMALISED value the rule judged, not a second
  // inline rewrite that could disagree with it.
  assert.ok(
    !/slug:\s*slug\.toLowerCase\(\)\.replace/.test(body),
    "saveGeneral still normalises the slug inline — that is the second copy of the rule this card removes",
  );
  assert.match(body, /slug:\s*nextSlug/, "the PATCH payload does not send the value the rule judged");

  // The field itself flags the state, rendered from the dictionary.
  assert.match(src, /onChange=\{\(e\) => setSlug\(normalizeFormSlug\(e\.target\.value\)\)\}/, "the slug input no longer normalises through the shared helper");
  assert.match(src, /id="fs-slug-error"/, "the field has no inline refusal element");
  assert.match(src, /\{slugProblemText\(slugProblem\)\}/, "the inline refusal does not render from the dictionary");
  assert.match(src, /aria-invalid=\{slugProblem === "reserved"/, "the field does not expose the refusal to assistive tech");
});
