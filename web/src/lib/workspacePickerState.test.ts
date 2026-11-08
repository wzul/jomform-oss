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
  const r = await createAgainst(409, { error: "slug already in use", code: "workspace.slugTaken" });
  assert.equal(r.ok, false, "a 409 was reported as a success");

  // The refusal the page keeps on screen, and the mode it keeps it in.
  assert.equal(pickerMode(EMPTY_ACCOUNT), "ask", "a zero-workspace account is not asked to pick anything");
  assert.equal(pickerCopyKeys("ask", 0).title, "workspace.askPageTitle");

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
  const r = await createAgainst(409, { error: "slug already in use", code: "workspace.slugTaken" });
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
  assert.deepEqual(pickerCopyKeys(pickerMode(EMPTY_ACCOUNT), 0), {
    title: "workspace.askPageTitle",
    subtitle: "workspace.askPageSubtitle",
  });
  // …and only a multi-row list gets the "pick one" heading, which is the
  // sentence that was printed for an account with no workspaces at all.
  assert.deepEqual(pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: 2 }), 2), {
    title: "workspace.pickerTitle",
    subtitle: "workspace.pickerSubtitle",
  });
  // A load failure keeps the picker heading (its body is the error panel), but
  // claims nothing about a count it has not read.
  const failed = pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, loadError: "boom" }), 0);
  assert.equal(failed.title, "workspace.pickerTitle");
  assert.equal(failed.subtitle, "workspace.pickerNeutralSubtitle");
});

test("one workspace never renders the 'multiple workspaces' sentence", () => {
  // THE DEFECT: a user with exactly one workspace and no default lands here,
  // and the list state printed "You have multiple workspaces. Pick which one to
  // enter." above a single row. The subtitle is now counted.
  const single = pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: 1 }), 1);
  assert.equal(single.title, "workspace.pickerTitle");
  assert.equal(single.subtitle, "workspace.pickerSingleSubtitle");

  const tByLang = (lang: string) => tIn(lang);
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const t = tByLang(lang);
    const singleSentence = t("workspace.pickerSingleSubtitle");
    const multipleSentence = t("workspace.pickerSubtitle");
    assert.ok(singleSentence.trim() !== "", `workspace.pickerSingleSubtitle.${lang} is empty`);
    assert.notEqual(
      singleSentence,
      multipleSentence,
      `lang=${lang}: the one-workspace line is still the multiple-workspaces line`,
    );
    // And the sentence must not claim plurality in any locale — the whole point
    // is that one row is below it. English is the only locale this can assert
    // structurally; the inequality above covers the other four.
    if (lang === "en") {
      assert.ok(
        !/multiple|which one/i.test(singleSentence),
        `the one-workspace subtitle still claims a choice: ${singleSentence}`,
      );
    }
  }

  // Two rows and up keep the wording the card says must stay.
  for (const n of [2, 3, 7]) {
    assert.equal(
      pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: n }), n).subtitle,
      "workspace.pickerSubtitle",
      `${n} workspaces did not get the multiple-workspaces line`,
    );
  }
  // The count is what selects the sentence, not the mode: the same list mode
  // answers differently for 1 and 2.
  assert.notEqual(
    pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: 1 }), 1).subtitle,
    pickerCopyKeys(pickerMode({ ...EMPTY_ACCOUNT, workspaceCount: 2 }), 2).subtitle,
  );
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
  // The count must reach the copy: without it the single-workspace list falls
  // back to "You have multiple workspaces…" (the reported bug). The build also
  // rejects a missing argument — this is the cheaper, always-run half of that.
  assert.ok(src.includes("const copy = pickerCopyKeys(mode, workspaces.length)"),
    "the heading is no longer derived from the rendered mode AND the row count it heads");
});

// ---------------------------------------------------------------------------
// Card t_57954ce5: a REFUSED set-default must not take the workspace list away.
//
// THE DEFECT (measured on a `next build` of this tree, API answered 400 to
// `PUT /api/auth/profile/default-workspace`): clicking "Make this the default
// workspace" on a non-default row replaced the WHOLE list with the page's
// load-error panel — `main li` went 2 -> 0, every `Default` badge vanished, and
// the only button left on the page was Retry. The rows had been read
// successfully a moment earlier and were still valid; the load-error panel says
// the opposite ("the list could not be read").
//
// CAUSE: `setDefault()` wrote its refusal into `setLoadError`, which is the one
// input `pickerMode` uses to choose the loadError surface. Same class as
// t_6c0badbe, one write path further along.
//
// WHAT THIS GUARD PINS, AND WHY IT BITES
//   1. The refusal has its OWN state, and the write's failure branch writes
//      only to it — never to `loadError`. Reverting `setSetDefaultError(r.error)`
//      to `setLoadError(r.error)` (the pre-fix line) fails here.
//   2. The refusal is rendered INSIDE the list surface (a `role="alert"`
//      paragraph above the rows), not in the loadError branch and not outside
//      the card. Moving it to the error panel fails here.
//   3. The mode decision still cannot see it: `pickerMode`'s input is
//      `{checking, loadError, workspaceCount}` only, and the pre-fix shape
//      (the refusal written into `loadError`) is asserted to produce
//      "loadError" through the same function — the test demonstrates the
//      difference instead of restating a constant.
//   4. The sentence is still the localized `workspace.setDefaultFail`, in all
//      five locales, and still different from the load-failure line.
//
// WHAT IT DOES NOT COVER (stated honestly): the Node runner cannot load `.tsx`,
// so this does not click the real button. The click is driven in a browser
// against a stubbed API in the card handoff.
// ---------------------------------------------------------------------------

const LOADED_TWO: PickerFacts = { checking: false, loadError: "", workspaceCount: 2 };

/**
 * The page source WITHOUT comments. The code carries comments that NAME the
 * defect (`// NEVER setLoadError here`, `card t_57954ce5`), so a guard that
 * greps the raw file would match its own explanation instead of the code.
 */
function code(path: string): string {
  return readFileSync(path, "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");
}

test("a refused set-default does not change the picker's surface", () => {
  // The state after a successful read of two workspaces: the rows are on screen.
  assert.equal(pickerMode(LOADED_TWO), "list");

  // BITE: the pre-fix shape — the same account, with the write's refusal written
  // into the load-error flag. That is what replaced the list with the panel.
  assert.equal(
    pickerMode({ ...LOADED_TWO, loadError: "Failed to set default workspace." }),
    "loadError",
    "feeding a set-default refusal into the load-error flag no longer changes the surface — " +
      "then this guard no longer describes the defect and must be re-derived",
  );
});

test("the set-default refusal is its own state, and the page keeps the list when it fires", () => {
  const src = code(PICKER_PAGE);

  // 1. A third state, separate from loadError and createError.
  assert.ok(
    src.includes('const [setDefaultError, setSetDefaultError] = useState("")'),
    "the set-default refusal has no state of its own — it is sharing the load-error flag again " +
      "(the t_57954ce5 defect: a 4xx on the write replaces the list with the error panel)",
  );

  // 2. The WRITE's failure branch writes that state, and never loadError.
  const fn = src.slice(src.indexOf("async function setDefault"));
  assert.ok(fn.length > 0, "setDefault() is gone — this guard no longer describes the page");
  const untilSuccess = fn.slice(0, fn.indexOf("setDefaultId(workspaceId)"));
  assert.ok(
    untilSuccess.includes("setSetDefaultError(r.error)"),
    "setDefault() no longer renders its refusal in place — the merchant gets nothing on screen " +
      "when the write is refused",
  );
  assert.ok(
    !untilSuccess.includes("setLoadError"),
    "setDefault() writes a WRITE refusal into loadError — that is the t_57954ce5 defect " +
      "(pickerMode then swaps the whole workspace list for the load-error panel + Retry)",
  );
  // The 401 -> login path and the localized sentence are unchanged.
  assert.ok(untilSuccess.includes("onUnauth: goToLogin"), "the 401 -> /login path was dropped from the write");
  assert.ok(
    untilSuccess.includes('t("workspace.setDefaultFail")'),
    "the write no longer falls back to the workspace.setDefaultFail sentence",
  );

  // 3. It is rendered in the LIST branch: after the ask/create branch, as a
  //    role="alert" paragraph that precedes the rows.
  const loadErrorIdx = src.indexOf('mode === "loadError"');
  const askIdx = src.indexOf('mode === "ask"');
  assert.ok(loadErrorIdx > 0 && askIdx > loadErrorIdx, "the render branches were restructured — re-derive this guard");
  assert.ok(
    !src.slice(loadErrorIdx, askIdx).includes("setDefaultError"),
    "the set-default refusal is rendered in the LOAD-ERROR branch — it would print 'the list could " +
      "not be read' for a list that was read fine",
  );
  const alertIdx = src.indexOf("{setDefaultError && (");
  assert.ok(
    alertIdx > askIdx,
    "the set-default refusal is not rendered inside the list surface (the final render branch)",
  );
  assert.ok(
    src.slice(alertIdx, alertIdx + 400).includes('role="alert"'),
    "the set-default refusal is not announced to assistive tech (no role=\"alert\")",
  );
  assert.ok(
    src.indexOf("{workspaces.map((s) => {") > alertIdx,
    "the refusal is rendered after the rows — it must sit beside them, above the list",
  );

  // 4. And the mode input still excludes it.
  assert.ok(
    !/pickerMode\(\{[^}]*setDefaultError/.test(src),
    "the set-default refusal is an input to pickerMode again — a failed WRITE can change the page mode",
  );
});

test("the set-default refusal is the localized sentence, not the load-failure one", () => {
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const t = tIn(lang);
    const sentence = t("workspace.setDefaultFail");
    assert.ok(sentence.trim() !== "", `workspace.setDefaultFail.${lang} is empty`);
    assert.notEqual(
      sentence,
      t("workspace.loadFail"),
      `lang=${lang}: the refused set-default prints the LIST-load failure line`,
    );
    // It must not claim the list could not be read — that is the sentence the
    // pre-fix page printed, and it is false.
    if (lang === "en") {
      assert.ok(
        !/load|could not be read/i.test(sentence),
        `the refusal sentence claims a read failure: ${sentence}`,
      );
    }
  }
  // The page renders exactly this key for the write's refusal (asserted from the
  // source above); the dictionary must therefore carry it in every locale the
  // app ships.
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    const entry = (DICT as Record<string, Record<string, unknown>>)["workspace.setDefaultFail"];
    assert.equal(typeof entry[lang], "string", `workspace.setDefaultFail has no ${lang} translation`);
  }
});
