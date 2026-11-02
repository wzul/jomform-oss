// Guard for card t_6c0badbe: a 409 from workspace CREATE must not switch the
// workspace picker out of its create form.
//
// THE DEFECT, as measured on a clean self-host install. A brand-new account
// (zero workspaces) landed on /workspace-picker and submitted the ask-first
// create form with a slug another account already held. The API answered 409
// and the page showed "That slug is already taken." — and nothing else: the
// heading became "Choose a workspace / You have multiple workspaces. Pick which
// one to enter." (a sentence that was false; the account had 0 rows in
// `workspaces`), the list was empty, and the name/slug fields were gone. Only a
// reload recovered.
//
// CAUSE. One `error` state served both the LOAD failure and the CREATE failure,
// and the render branched on it before it branched on the list. The title
// branched on the same flag. So a create refusal was indistinguishable from
// "the list could not be read", and it took the form away.
//
// WHAT THIS GUARD PINS, AND WHY IT BITES
//   1. The mode decision ignores create failures STRUCTURALLY: `PickerFacts`
//      has no create-error field, and pickerMode(0 workspaces, no load error)
//      is "ask". The pre-fix shape — one `error` flag fed into the decision —
//      is asserted to produce "loadError" through the same function, so the
//      test demonstrates the difference rather than restating a constant.
//   2. The create refusal itself is real and translated: `createWorkspace`
//      executed against a stubbed 409 yields the slug-taken key, and that key
//      renders as the localized slug-taken sentence (all five locales), not the
//      generic create-failure line. This is the message the merchant keeps
//      seeing WHILE the form stays on screen.
//   3. Source guard on the page: `askFirstCreate` writes its failure to
//      `setCreateError`, never to `setLoadError`, and the page decides its mode
//      through pickerMode (not a bare `error ?`/`workspaces.length === 0 ?`
//      chain). Reverting to the single-error shape fails here even if someone
//      re-adds a compatible-looking mode function.
//   4. The heading comes from the same mode as the body (pickerCopyKeys), so
//      "You have multiple workspaces" can never be printed for a zero-workspace
//      account again.
//
// WHAT IT DOES NOT COVER (stated honestly): the Node test runner cannot load
// `.tsx`, so this does not click the real form. The click is reported in the
// card handoff, driven in a browser against a real API.
//
// Run with: npm run test:unit
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { pickerCopyKeys, pickerMode, type PickerFacts } from "./workspacePickerState.ts";
import { createWorkspace } from "./workspace.ts";
import { DICT } from "../components/i18nDict.ts";

const PICKER_PAGE = fileURLToPath(new URL("../app/workspace-picker/page.tsx", import.meta.url));

function tIn(lang: string) {
  return (k: string) => {
    const entry = (DICT as Record<string, Record<string, unknown>>)[k];
    assert.ok(entry, `missing dictionary key ${k}`);
    const v = entry[lang];
    return typeof v === "string" ? v : (entry.en as string);
  };
}

/** A fake fetch answering one status with one body (the API's 409 envelope). */
function stubFetch(status: number, body: unknown) {
  const fn = (async () => ({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  })) as unknown as typeof fetch;
  return fn;
}

async function createAgainst(status: number, body: unknown) {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = stubFetch(status, body);
    return await createWorkspace({ name: "Clean Run", slug: "cleanrun" });
  } finally {
    globalThis.fetch = original;
  }
}

// The state the page is in when the reported 409 arrives: the list loaded
// successfully and answered ZERO workspaces (measured: account `cleanrun` had 0
// rows in `workspaces`), and no load error exists.
const EMPTY_ACCOUNT: PickerFacts = { checking: false, loadError: "", workspaceCount: 0 };

test("a 409 on create leaves the picker in its create-form mode", async () => {
  const r = await createAgainst(409, { error: "slug sudah digunakan", code: "workspace.slugTaken" });
  assert.equal(r.ok, false, "a 409 was reported as a success");

  // The refusal the page keeps on screen, and the mode it keeps it in.
  assert.equal(pickerMode(EMPTY_ACCOUNT), "ask", "a zero-workspace account is not asked to pick anything");
  assert.equal(pickerCopyKeys("ask").title, "workspace.askPageTitle");

  // BITE: the pre-fix shape. If the create failure is fed into the SAME flag
  // the mode decision reads (which is exactly what the page did), the very same
  // account flips to the list-error surface — no list, no fields. This is the
  // reported bug, expressed through the function under test.
  const preFixShape: PickerFacts = { ...EMPTY_ACCOUNT, loadError: "That slug is already taken." };
  assert.equal(
    pickerMode(preFixShape),
    "loadError",
    "feeding a create failure into the load-error flag no longer changes the surface — " +
      "then this guard no longer describes the defect and must be re-derived",
  );
});

test("the create refusal is the translated slug-taken sentence, not the generic one", async () => {
  const r = await createAgainst(409, { error: "slug sudah digunakan", code: "workspace.slugTaken" });
  assert.equal(r.ok, false);
  const key = (r as { errorKey: string }).errorKey;

  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const t = tIn(lang);
    assert.equal(t(key), t("workspace.slugTaken"), `lang=${lang}`);
    assert.notEqual(t(key), t("workspace.createFail"), `lang=${lang} degraded to the generic line`);
    // The sentence must not be the picker's "multiple workspaces" copy — that
    // is the false claim the page made about the account's state.
    assert.notEqual(t(key), t("workspace.pickerSubtitle"), `lang=${lang}`);
  }
});

test("the heading is selected from the same mode as the body it heads", () => {
  // A zero-workspace account gets the first-run heading…
  assert.deepEqual(pickerCopyKeys(pickerMode(EMPTY_ACCOUNT)), {
    title: "workspace.askPageTitle",
    subtitle: "workspace.askPageSubtitle",
  });
  // …and only a non-empty list gets the "pick one" heading, which is the
  // sentence that was printed for an account with no workspaces at all.
  assert.deepEqual(pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: 2 })), {
    title: "workspace.pickerTitle",
    subtitle: "workspace.pickerSubtitle",
  });
  // A load failure keeps the picker heading (its body is the error panel).
  assert.equal(pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, loadError: "boom" })).title, "workspace.pickerTitle");
});

test("the page writes a create failure to createError and decides its mode with pickerMode", () => {
  const src = readFileSync(PICKER_PAGE, "utf8");

  const ask = src.slice(src.indexOf("async function askFirstCreate"));
  assert.ok(ask.includes("setCreateError(t(r.errorKey))"), "askFirstCreate no longer renders the create refusal in place");
  // The failure branch must not touch the load-error state: that is what
  // switched the page's mode.
  const failureBranch = ask.slice(0, ask.indexOf("enterWorkspace(r.slug)"));
  assert.ok(
    !failureBranch.includes("setLoadError"),
    "askFirstCreate writes a CREATE refusal into loadError — that is the t_6c0badbe defect " +
      "(the create form and its fields disappear and the picker heading is printed for a " +
      "zero-workspace account)",
  );

  assert.ok(src.includes("pickerMode({ checking, loadError, workspaceCount: workspaces.length })"),
    "the page no longer decides its mode through lib/workspacePickerState.ts");
  assert.ok(!/checking\s*\?\s*\(/.test(src), "the page branched on the raw `checking` flag again");
  assert.ok(!/error\s*\?\s*\(/.test(src), "the page branched on a raw `error` flag again");
  assert.ok(src.includes("const copy = pickerCopyKeys(mode)"),
    "the heading is no longer derived from the rendered mode");
});
