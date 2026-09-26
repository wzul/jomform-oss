// Guard for the merchant-facing half of the carded defect: a slug save the
// server REJECTED must say so, and say why, IN THE MERCHANT'S LANGUAGE.
//
// THE BUG, as the merchant lived it. Form Settings → General has an editable
// slug field. Its save handler PATCHed `{slug, title, theme}` to
// /v1/rest/forms/{id}; the endpoint's request struct had no Slug member, so the
// value was dropped, the response was 200 {status:"updated"}, and the page
// showed "Saved ✓". The form kept its old URL and the new one 404'd. The failure
// was invisible, which is worse than a loud rejection.
//
// THE SECOND HALF, and what this file is really about. The first cut of the fix
// surfaced the rejection by printing the server's `error` prose — which is a
// MALAY literal ("slug sudah digunakan oleh borang lain dalam workspace ini"),
// because the API never receives the caller's language on a JSON call. An
// English dashboard therefore read Malay exactly where the fix was supposed to
// make the reason visible. That is the defect tracked as issue #6177.
//
// HOW IT IS FIXED NOW. The API answers both refusals through #6177's SHARED
// machine-error-code path — plain `writeErr` plus the message→code table in
// api/internal/httpapi/errcode.go; this PR carries no error-code mechanism of
// its own — and the console renders the reason with #6177's shared
// lib/apiErr.ts (API_ERROR_KEYS → the dashboard dictionary). ONE mechanism.
//
// WHAT THIS GUARD PINS
//   1. The two codes the rename can answer are mapped by the SHARED table
//      (API_ERROR_KEYS), and the codes themselves are read out of the Go source
//      rather than restated — renaming one on the server fails HERE.
//   2. Each code renders a localized sentence in all five locales, and that
//      sentence is NEVER the server's prose.
//   3. An unknown/absent code falls back to the calling page's own localized
//      message, never to the prose — what makes the mechanism safe against a
//      code the server adds later.
//   4. The form-settings save path routes a rejection through the shared apiErr
//      and never prints the saved confirmation (pre-fix, a dropped rename
//      answered 200 and the page said "Saved ✓").
//   5. No second, local error-code mechanism is left anywhere.
//   6. The slug field's inline "?" doc link still reaches a real docs section.
//
// Run with the repo's Node test runner:  npm run test:unit
import assert from "node:assert/strict";
import test from "node:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

import { API_ERROR_KEYS, apiErr } from "./apiErr.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const here = path.dirname(fileURLToPath(import.meta.url));
const webSrc = path.join(here, "..");
const formSettings = path.join(webSrc, "app", "form-settings", "page.tsx");
const apiDir = path.join(here, "..", "..", "..", "api", "internal", "httpapi");

// The server's wire prose for the two refusals. It must NEVER reach the banner:
// it is a Malay literal, and the API does not know the dashboard's language
// (issue #6177 — an English dashboard reading Malay prose is the defect).
const SERVER_PROSE = [
  "slug sudah digunakan oleh borang lain dalam workspace ini",
  "form slug must be 1-40 characters of a-z, 0-9 or -",
];

/** Every .ts/.tsx under web/src (excluding node_modules and .next). */
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name === ".next") continue;
    const p = `${dir}/${name}`;
    if (statSync(p).isDirectory()) walk(p, out);
    else if (name.endsWith(".tsx") || name.endsWith(".ts")) out.push(p);
  }
  return out;
}

const webSources = walk(webSrc);

/** The two codes a form-slug RENAME can answer with, read out of the Go source. */
function renameCodes(): Record<string, string> {
  const src = readFileSync(path.join(apiDir, "errcode.go"), "utf8");
  const out: Record<string, string> = {};
  for (const m of src.matchAll(/Code(FormSlugInUse|FormSlugInvalid)\s*=\s*"([^"]+)"/g)) {
    out[m[1]] = m[2];
  }
  return out;
}

/** A fake `t` that resolves dictionary keys the way the real renderer does. */
function tIn(lang: string) {
  return (key: string): string => {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    assert.ok(entry, `missing dictionary key ${key}`);
    const v = entry[lang];
    return typeof v === "string" ? v : (entry.en as string);
  };
}

test("the guard sees the real app (it cannot pass vacuously)", () => {
  assert.ok(webSources.length > 40, `expected to walk the dashboard, got ${webSources.length} files`);
  assert.ok(
    webSources.some((p) => p.endsWith("lib/apiErr.ts")),
    "lib/apiErr.ts is not in the walked set — the shared mechanism is missing",
  );
  assert.ok(Object.keys(renameCodes()).length === 2, `expected both rename codes, got ${JSON.stringify(renameCodes())}`);
});

test("the rename's codes are the SHARED ones, mapped in API_ERROR_KEYS", () => {
  const codes = renameCodes();
  assert.equal(codes.FormSlugInUse, "form.slugInUse", "the server's rename-collision code moved");
  assert.equal(codes.FormSlugInvalid, "form.slugInvalid", "the server's invalid-slug code moved");
  // Mapped by #6177's shared table, not by a second, local mapping.
  for (const [name, code] of Object.entries(codes)) {
    assert.ok(
      code in API_ERROR_KEYS,
      `${name} = ${code} has no entry in API_ERROR_KEYS (web/src/lib/apiErr.ts), so the ` +
        `dashboard cannot translate it and the merchant sees the page's generic sentence`,
    );
  }
  // The rename-collision code must NOT be the trash-restore code: the two
  // refusals are different actions with different remedies, and one sentence
  // cannot instruct both (see the comment on CodeFormSlugInUse in errcode.go).
  assert.notEqual(
    codes.FormSlugInUse,
    "form.slugTaken",
    "the rename collision and the trash-restore collision collapsed onto one code, so one of " +
      "the two refusals now shows the other's instruction",
  );
  assert.ok("form.slugTaken" in API_ERROR_KEYS, "sanity: the trash-restore code lost its mapping");
});

test("a 409 rename collision renders the localized sentence, never the Malay prose", () => {
  const body = { code: renameCodes().FormSlugInUse, error: SERVER_PROSE[0] };
  for (const lang of LANGS) {
    const rendered = apiErr(body, "FALLBACK", tIn(lang));
    assert.notEqual(rendered, "FALLBACK", `lang=${lang} did not translate a KNOWN code`);
    assert.ok(
      !rendered.includes(SERVER_PROSE[0]),
      `lang=${lang} rendered the server's Malay prose: ${JSON.stringify(rendered)} — the ` +
        `dashboard must translate the machine code (issue #6177)`,
    );
  }
  // The English sentence specifically, so a dictionary entry that quietly became
  // Malay for `en` is caught rather than merely "not the prose".
  assert.match(apiErr(body, "FALLBACK", tIn("en")), /different slug/i);
  // And it really differs per language — one English string for all five is the
  // same bug wearing a dictionary.
  assert.equal(
    new Set(LANGS.map((l) => tIn(l)("apiErr.formSlugInUse"))).size,
    LANGS.length,
    "the collision sentence does not differ per language",
  );
});

test("a 400 invalid slug renders the localized rule, never the server's prose", () => {
  const body = { code: renameCodes().FormSlugInvalid, error: SERVER_PROSE[1] };
  for (const lang of LANGS) {
    const rendered = apiErr(body, "FALLBACK", tIn(lang));
    assert.notEqual(rendered, "FALLBACK", `lang=${lang} did not translate a KNOWN code`);
    assert.ok(!rendered.includes(SERVER_PROSE[1]), `lang=${lang} printed the server's prose`);
  }
  assert.match(apiErr(body, "FALLBACK", tIn("en")), /1-40 characters/i);
});

test("an unknown or absent code falls back to the page's sentence, never the prose", () => {
  // A code the server adds later, or a body from an older build: the page's own
  // localized message must win. Falling back to `error` is the defect.
  for (const body of [
    { code: "form.someFutureCode", error: SERVER_PROSE[0] },
    { error: SERVER_PROSE[0] }, // codeless: an older endpoint, a proxy page
    null,
    undefined,
  ]) {
    const rendered = apiErr(body, "Localized fallback.", () => "T");
    assert.equal(rendered, "Localized fallback.");
  }
});

test("saveGeneral routes a rejected slug save through the SHARED apiErr, with no local helper", () => {
  const src = readFileSync(formSettings, "utf8");
  const start = src.indexOf("async function saveGeneral()");
  assert.ok(
    start >= 0,
    "app/form-settings/page.tsx no longer defines saveGeneral — the slug save path is gone",
  );
  const end = src.indexOf("\n  async function ", start + 10);
  const body = src.slice(start, end > start ? end : start + 6000);

  assert.ok(
    body.includes("if (!r.ok)"),
    "saveGeneral has no failure branch — a rejected slug save would fall through to the saved confirmation",
  );
  assert.ok(
    /apiErr\(d,\s*t\("prof\.failed"\)/.test(body),
    "saveGeneral no longer routes a failed save through the shared lib/apiErr — the rejection is " +
      "silent again (or a generic \"Failed\" that does not name the slug), which is the defect",
  );
  assert.ok(
    !body.includes("saveFailureMessage"),
    "saveGeneral still calls the PR's own local saveFailureMessage — there must be exactly ONE " +
      "way the console turns an API error code into a sentence (issue #6177's lib/apiErr)",
  );
  // The saved confirmation is reachable only on the success path: the rejection
  // branch must return before it.
  const rejectAt = body.indexOf("if (!r.ok)");
  const savedAt = body.indexOf('t("fs.saved")');
  assert.ok(savedAt < 0 || savedAt > rejectAt, "the saved confirmation precedes the rejection branch");
});

test("the PR carries no second, local error-code mechanism anywhere", () => {
  // The card's acceptance criterion, asserted rather than trusted: the shared
  // #6177 path is the only one. A re-introduced local helper, a local
  // code→key map, or a hand-rolled Go writeCodedErr fails here.
  //
  // Tests are excluded from this walk: this file necessarily NAMES the retired
  // helpers to forbid them, and a guard that flags its own assertion text proves
  // nothing.
  const nonTestSources = webSources.filter((p) => !p.endsWith(".test.ts"));
  assert.ok(nonTestSources.length > 30, "the walk is not covering the dashboard");
  for (const [needle, why] of [
    ["formSlugSave", "the PR's local slug-error decoder (superseded by lib/apiErr)"],
    ["SLUG_ERROR_KEYS", "the PR's local code→key map (superseded by API_ERROR_KEYS)"],
  ] as const) {
    const offenders = nonTestSources.filter((p) => readFileSync(p, "utf8").includes(needle));
    assert.deepEqual(
      offenders.map((p) => p.slice(webSrc.length)),
      [],
      `${needle} is back in web/src — ${why}`,
    );
  }
  for (const name of ["server.go", "errors.go"]) {
    const src = readFileSync(path.join(apiDir, name), "utf8");
    assert.ok(
      !/func\s+writeCodedErr\s*\(/.test(src),
      `api/internal/httpapi/${name} DEFINES writeCodedErr again — the slug refusals must go ` +
        `through the shared writeErr/errorBody path (errcode.go), so there is exactly ONE way ` +
        `the API reports a machine error code`,
    );
  }
  // No hand-rolled code constant outside the shared table either.
  for (const name of ["server.go", "errors.go", "rest_handlers.go"]) {
    const src = readFileSync(path.join(apiDir, name), "utf8");
    assert.ok(
      !/CodeFormSlug\w+\s*=\s*"/.test(src),
      `api/internal/httpapi/${name} declares its own CodeFormSlug* constant — error codes live ` +
        `in the shared table in errcode.go and nowhere else`,
    );
  }
});

test("the slug field carries an inline \"?\" doc link to a docs section that exists", () => {
  const src = readFileSync(formSettings, "utf8");
  assert.ok(
    src.includes('href="/docs#form-url-slug"'),
    "the slug field has no inline \"?\" doc link to /docs#form-url-slug " +
      "(AGENTS.md docs-parity rule 4) — the rename changes a URL the merchant " +
      "already published, so the rule must be reachable where the decision is made",
  );
  assert.match(
    src,
    /aria-label=\{t\("fs\.slugDocTitle"\)\}/,
    "the doc link is not labelled, so it is invisible to a screen reader",
  );
  const docs = readFileSync(path.join(webSrc, "app", "docs", "page.tsx"), "utf8");
  assert.ok(
    docs.includes('slug: "form-url-slug"'),
    'web/src/app/docs/page.tsx has no section with slug "form-url-slug", so the ' +
      'field\'s "?" link dead-ends (a hash link to nowhere)',
  );
});

test("the slug field's own strings exist in every language the dict declares", () => {
  for (const key of ["fs.slug", "fs.slugHint", "fs.slugDocTitle"]) {
    const entry = (DICT as Record<string, Record<string, unknown>>)[key];
    assert.ok(entry, `i18nDict.ts has no entry for ${key}`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is empty — that locale silently shows English`,
      );
    }
  }
});
