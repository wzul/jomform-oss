// Card t_9efc5ed7 — Settings → CHIP: the approved card redesign, and the
// Test/Live mode control that was removed with it.
//
// THE TWO DEFECTS THIS PINS, both measured on the shipped page:
//
//   1. THE MODE CONTROL ASKED A QUESTION NOBODY READ. `chip_mode` never chose the
//      API endpoint (ChipAPIBaseUrl does), never blocked anything and never
//      changed a payment — the key itself decides what CHIP does. Card t_8f38dc34
//      had already stopped INFERRING the label from the key prefix (CHIP keys are
//      opaque bearer tokens with no published prefix); it made the merchant CHOOSE
//      it instead, and nothing consumed the answer. The control, its state, its
//      save field and its copy are gone; `accounts.chip_mode` survives as
//      write-only legacy (api/internal/dbstore/chipmode.go, pinned by that
//      package's tests and the REST/MCP integration tests).
//
//   2. THE BRAND-ID PLACEHOLDER LOOKED TRUNCATED. The field shipped
//      `e.g. 5f2b…-uuid`, where "…" was a LITERAL CHARACTER in the string, not CSS
//      overflow — so the field read as clipped before the merchant typed anything
//      (Wan: "brand id tu ada truncate .... uuid"). Every placeholder in the card
//      now names the ACTION and carries no ellipsis of either kind.
//
// WHY A SOURCE GUARD HERE, AND A BROWSER ARM TOO
//   The repo has no JSX/DOM unit runner (see `src/lib/i18n.test.ts`), so what can
//   be pinned cheaply is the SHAPE the page must keep: no radiogroup, no mode
//   field in the save body, ONE status pill, no ellipsis in any placeholder, and a
//   dictionary with no orphan key left behind. The RENDERED proof — one pill in
//   the live DOM, checkboxes with an announced state, axe green — is
//   `e2e/a11y-chip-settings-card.spec.ts`, which drives the real client component.
//
// Run with: npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { codeOnly } from "./jsxSource.ts";
import { DICT, LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
// Comments stripped, so the sentence explaining WHY the mode control went cannot
// satisfy or trip a check about the control itself (every guard in this repo reads
// code this way).
const settings = codeOnly(read("../app/settings/page.tsx"));
const dictSrc = read("../components/i18nDict.ts");

/** The CHIP panel only: from its branch to the next panel's. */
const chipPanel = (() => {
  const from = settings.indexOf('{activeTab === "chip" && (');
  assert.ok(from >= 0, "settings/page.tsx no longer opens the CHIP panel with `{activeTab === \"chip\" && (`");
  const to = settings.indexOf('{activeTab === "domains" && (', from);
  assert.ok(to > from, "settings/page.tsx no longer has a panel after the CHIP one — this slice would swallow the file");
  return settings.slice(from, to);
})();

const ELLIPSES = ["…", "..."];

test("the CHIP card renders NO Test/Live mode control", () => {
  // The radiogroup, its two radios and the testids the old e2e spec drove.
  assert.equal(
    (chipPanel.match(/role="radiogroup"/g) ?? []).length,
    0,
    "the CHIP card renders a radiogroup again — the Test/Live mode control is back, and nothing reads it",
  );
  assert.equal(
    (chipPanel.match(/type="radio"/g) ?? []).length,
    0,
    "the CHIP card renders a radio input again (the removed mode control)",
  );
  assert.ok(!chipPanel.includes("chip-mode-"), "the CHIP card still carries a `chip-mode-` testid");
  // …and nothing left over that only existed to feed it.
  assert.ok(!settings.includes("chipMode"), "settings/page.tsx still references `chipMode` state");
  assert.ok(
    !settings.includes("CHIP_MODE_OPTIONS") && !settings.includes("chipModeFromStored"),
    "settings/page.tsx still imports the deleted lib/chipMode helpers",
  );
  // The save body must not carry the field any more. `mode` survived in the
  // response handler (`typeof d.mode`) only to keep older payloads typed, so the
  // assertion is about the identifier the control used, not the word.
  assert.ok(
    !/mode:\s*chipMode/.test(settings),
    "the CHIP save still sends `mode: chipMode` — the merchant is being asked for an answer nobody reads",
  );
});

test("no placeholder in the CHIP card carries an ellipsis (the old `5f2b…-uuid`)", () => {
  // EVERY placeholder in the panel, literal or braced.
  const placeholders = [...chipPanel.matchAll(/placeholder=(?:"([^"]*)"|\{([^}]*)\})/g)].map((m) =>
    (m[1] ?? m[2] ?? "").trim(),
  );
  assert.ok(placeholders.length >= 2, `the CHIP card renders ${placeholders.length} placeholder(s) — expected the Brand ID and Secret Key fields`);
  for (const ph of placeholders) {
    for (const ell of ELLIPSES) {
      assert.ok(
        !ph.includes(ell),
        `a CHIP placeholder contains ${JSON.stringify(ell)} (${JSON.stringify(ph)}) — it reads as a truncated field. ` +
          `The failing case this replaced was the literal "e.g. 5f2b…-uuid".`,
      );
    }
  }
  // …and the two dictionary values the fields actually show name the ACTION.
  assert.equal(DICT["set.brandIdPh"].en, "Paste the Brand ID from the CHIP portal");
  assert.equal(DICT["set.secretKeyPh"].en, "Paste the Secret Key from the CHIP portal");
  for (const lang of LANGS) {
    for (const k of ["set.brandIdPh", "set.secretKeyPh"] as const) {
      const v = DICT[k][lang];
      for (const ell of ELLIPSES) {
        assert.ok(!v.includes(ell), `${k}.${lang} contains ${JSON.stringify(ell)}: ${JSON.stringify(v)}`);
      }
    }
  }
});

test("the connection state is ONE pill, and the two duplicated Verified chips are gone", () => {
  // Exactly one element carries the status testid. The card used to render the
  // same "✓ API credentials verified" chip twice — once on Brand ID, once on
  // Secret Key — plus a third state word in the header.
  assert.equal(
    (chipPanel.match(/chip-status-badge/g) ?? []).length,
    1,
    "the CHIP panel renders more than one status pill (the duplicated Verified chips are back)",
  );
  assert.ok(
    !chipPanel.includes("set.apiVerified"),
    "the per-field '✓ API credentials verified' chip is back — the header pill says it once",
  );
  // The pill's three states all exist in every locale, so no arm of it can render
  // a raw key.
  for (const k of ["set.connected", "set.notConnected", "set.notVerified"] as const) {
    assert.ok(k in DICT, `${k} is missing from the dictionary — the pill would render its key literally`);
    for (const lang of LANGS) {
      assert.ok((DICT[k][lang] ?? "").trim() !== "", `${k}.${lang} is empty`);
    }
  }
  assert.ok(
    chipPanel.includes('data-testid="chip-status-badge"'),
    "the CHIP panel no longer renders the status pill this guard is about",
  );
});

test("no dictionary key exists only to ask for the removed mode", () => {
  // The dead keys, in BOTH directions: the object (what `t()` reads) and the
  // source (a planted declaration would be a key with no call site, which is the
  // orphan drift `activityPage.test.ts` exists to prevent).
  const DEAD = [
    "set.mode",
    "set.modeHint",
    "set.live",
    "set.test",
    "set.activeMode",
    "set.apiVerified",
    "set.notSet",
    "set.secretMasked",
    "set.secretMaskedPlaceholder",
  ];
  for (const k of DEAD) {
    assert.ok(!(k in DICT), `${k} is back in the dictionary with no call site — it only ever asked for the removed mode`);
    assert.ok(!dictSrc.includes(`"${k}"`), `${k} is declared in i18nDict.ts again`);
  }
  // And the page uses NOTHING that only the removed control rendered.
  for (const k of DEAD) {
    assert.ok(!chipPanel.includes(`"${k}"`), `the CHIP card still renders ${k}`);
  }
});

test("the card keeps the controls the redesign put there", () => {
  // Not a style check — each one is a promise the approved design makes, and a
  // refactor that drops one would otherwise pass every other arm here.
  const required: Array<[string, string]> = [
    ['data-testid="chip-brand-id"', "the Brand ID field"],
    ['data-testid="chip-secret-key"', "the Secret Key paste field (the Replace flow)"],
    ['data-testid="chip-secret-masked"', "the masked saved-key line"],
    ['data-testid="chip-secret-replace"', "the Replace action"],
    ['data-testid="chip-advanced"', "the Advanced disclosure holding the fallback email"],
    ['data-testid="chip-pm-toggle-all"', "the payment-method Select all/Clear all toggle"],
    ['data-testid="chip-test-connection"', "the Test connection action"],
    ['id="chip-creds-form"', "the form the sticky bar submits"],
  ];
  for (const [needle, what] of required) {
    assert.ok(chipPanel.includes(needle), `the CHIP card no longer renders ${what} (${needle})`);
  }
  // The fallback email lives INSIDE a <details>, not as a primary row.
  const details = chipPanel.indexOf('data-testid="chip-advanced"');
  const email = chipPanel.indexOf('data-testid="chip-email-fallback"');
  assert.ok(details >= 0 && email > details, "the fallback email is no longer inside the Advanced disclosure");
  // The payment methods announce themselves as checkboxes, in a two-column grid.
  assert.match(chipPanel, /role="checkbox"/, "the payment-method boxes are not announced as checkboxes");
  assert.match(chipPanel, /aria-checked=\{checked\}/, "the payment-method boxes carry no aria-checked state");
  assert.match(chipPanel, /grid-cols-2/, "the payment-method grid is no longer two columns");
});
