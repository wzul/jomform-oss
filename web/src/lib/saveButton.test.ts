// Guard for the shared `SaveButton` — the button that confirms on itself.
//
// THE DEFECT THIS PINS (measured on staging 2026-10-06)
//   The Storefront tab's "Save footer" button held a "Saved ✓" state for 2s
//   (PR #997), so the confirmation landed where the merchant pressed. That fix
//   was written INLINE, on ONE button. Every other save button on the same page
//   kept the old behaviour, so the same product answered the same press two
//   different ways:
//
//     "Save footer"       -> the button turns "Saved ✓" (green) AND a toast
//     "Save landing page" -> only a toast; the button never changed
//
//   Measured: pressing "Save landing page" showed the toast "Landing page saved ✓"
//   while the button's own label and colour stayed exactly as before. A merchant
//   watching the control they just clicked saw nothing happen.
//
//   The fix is the extraction, not a second inline copy: `components/SaveButton.tsx`
//   carries the state machine, and BOTH buttons render it, so the two cannot drift
//   again — which is precisely how this was found.
//
// WHAT IS PINNED, AND WHY EACH ONE MATTERS
//   1. Both save buttons on the page are wired through the component. One
//      unwired button is the whole defect.
//   2. A FAILURE MUST NOT CONFIRM. `onSave` returning `false`, or throwing,
//      leaves the button idle — otherwise a failed write displays "Saved ✓".
//      This is the same class as the tone defect on these pages, and it is the
//      half a copy of the footer's original inline version did not have.
//   3. The confirmation label is a SHORTER dictionary key, not the full
//      sentence: a longer string widens the button mid-press, which reads as a
//      layout jump exactly when the user is looking at it. And it must be a DICT
//      key — `t()` falls back to the key itself, so a literal ships English to
//      every locale silently.
//   4. The timer is cleared BEFORE re-arming and on unmount: a stale timer
//      flipping a NEWER label back to idle is the bug this prevents.
//   5. The state class APPENDS to `jf-btn-primary` rather than forking it.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   * It reads source, so it cannot observe the rendered state. That was
//     measured in a real browser on staging: the landing button reached
//     `Saved ✓` / `rgb(4,120,87)` at ~204ms and reverted at ~2204ms, while the
//     footer (moved to the same component) reached it at ~256ms and reverted at
//     ~2254ms — so the move did not regress the button that already worked.
//   * It does not re-test the tone work on /sales, /spam and /form-settings:
//     that is a separate, pre-existing contract owned by
//     `src/lib/actionFailureTone.test.ts`.
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const SRC = fileURLToPath(new URL("../", import.meta.url));
const settings = readFileSync(`${SRC}app/settings/page.tsx`, "utf8");
const saveBtn = readFileSync(`${SRC}components/SaveButton.tsx`, "utf8");

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(existsSync(`${SRC}components/SaveButton.tsx`),
    "components/SaveButton.tsx moved — this guard is checking nothing");
  assert.ok(saveBtn.length > 500, "components/SaveButton.tsx is suspiciously small — did the read break?");
  assert.ok(settings.includes("async function saveLandingPage"),
    "settings/page.tsx no longer defines saveLandingPage — the guard's anchor moved");
  assert.ok(settings.includes("async function saveStorefrontSettings"),
    "settings/page.tsx no longer defines saveStorefrontSettings — the guard's anchor moved");
});

test("BOTH save buttons are wired through the shared component", () => {
  // The landing button is the one that was reported. If it goes back to a plain
  // <button>, the symptom returns exactly: a toast appears and the button does
  // not change.
  assert.ok(settings.includes("onSave={saveLandingPage}"),
    "the landing-page Save button is no longer wired through SaveButton — it would go back to " +
      "showing only a toast while the button itself never changes (the reported symptom)");
  assert.ok(settings.includes("onSave={saveStorefrontSettings}"),
    "the footer Save button is no longer wired through SaveButton");
  assert.equal(settings.match(/<SaveButton/g)?.length, 2,
    "settings/page.tsx renders a different number of SaveButton components than the two save " +
      "buttons it has — one of them was hand-rolled again, which is how the two drifted apart");
});

test("a FAILURE must not confirm on the button", () => {
  // Both signals: the caller's explicit `false`, and a thrown error.
  assert.match(saveBtn, /if \(\(await onSave\(\)\) === false\)\s*confirmed = false;/,
    "components/SaveButton.tsx no longer honours a `false` return from onSave — a FAILED save " +
      "would display the success confirmation");
  assert.match(saveBtn, /\}\s*catch\s*\{[\s\S]{0,400}?confirmed = false;/,
    "components/SaveButton.tsx no longer treats a rejected onSave as a failure — a thrown error " +
      "would display the success confirmation");
  assert.match(saveBtn, /if \(!confirmed\) return;/,
    "components/SaveButton.tsx sets the saved state regardless of the outcome");
  // And the page must actually SIGNAL failure for both handlers, or the
  // component's contract is decorative.
  for (const fn of ["saveStorefrontSettings", "saveLandingPage"]) {
    assert.ok(
      settings.includes(`async function ${fn}(): Promise<boolean>`),
      `${fn} no longer returns a success boolean — SaveButton cannot tell a failed write from a ` +
        "good one and would confirm both",
    );
  }
});

test("the confirmation label is the SHORT dictionary key", () => {
  // On the button: the short form, so the control does not widen mid-press.
  assert.match(settings, /savedLabel=\{t\("set\.lpSavedShort"\)\}/,
    "the landing Save button no longer uses the short saved label — the full " +
      '"Landing page saved ✓" widens the button as it switches');
  assert.match(settings, /savedLabel=\{t\("set\.sfSavedShort"\)\}/,
    "the footer Save button no longer uses the short saved label");
  // And the key must exist in the dictionary, or t() renders the raw key.
  const dict = readFileSync(`${SRC}components/i18nDict.ts`, "utf8");
  for (const key of ["set.lpSavedShort", "set.sfSavedShort"]) {
    assert.ok(dict.includes(`"${key}"`),
      `i18nDict.ts has no "${key}" entry — t() falls back to the key itself, so the button would ` +
        "show the literal string to every locale");
  }
  // Five locales, like every other entry in this dictionary.
  const entry = dict.slice(dict.indexOf('"set.lpSavedShort"'));
  const body = entry.slice(0, entry.indexOf("},"));
  for (const loc of ["en:", "ms:", "zh:", "ta:", "ar:"]) {
    assert.ok(body.includes(loc), `set.lpSavedShort is missing its \`${loc}\` locale`);
  }
});

test("the timer is cleared before re-arming and on unmount", () => {
  assert.match(saveBtn, /if \(timer\.current !== null\) \{\s*window\.clearTimeout\(timer\.current\);/,
    "components/SaveButton.tsx does not clear a pending timer before arming a new one — saving " +
      "twice would let the FIRST save's timer revert the second save's label early");
  assert.match(saveBtn, /return \(\) => \{\s*if \(timer\.current !== null\) window\.clearTimeout\(timer\.current\);/,
    "components/SaveButton.tsx's unmount cleanup is gone — a save completing after navigation " +
      "would set state on a dead component");
});

test("the saved state APPENDS to the base class rather than forking it", () => {
  assert.match(saveBtn, /className = "jf-btn-primary"/,
    "components/SaveButton.tsx no longer defaults to the base .jf-btn-primary class");
  assert.match(saveBtn, /savedClassName = "jf-btn-primary jf-btn-saved"/,
    "components/SaveButton.tsx no longer appends jf-btn-saved to jf-btn-primary — a fork loses " +
      "the base rule");
  // The page must not override either default.
  assert.doesNotMatch(settings, /<SaveButton[\s\S]{0,300}?className=/,
    "settings/page.tsx overrides SaveButton's className — the shared base/state pair would no " +
      "longer apply to that button");
});

// ─── the THROWN/network failure must reach the page (card t_9fad3d6a) ─────────
//
// THE DEFECT THIS PINS (observed in a real browser, not inferred)
//   `saveLandingPage`'s `await fetch(...)` sat OUTSIDE any `try`. On a rejected
//   fetch — lost connectivity, or the request aborted — the function threw,
//   `SaveButton`'s own `catch` turned that into `confirmed = false`, so the
//   button correctly stayed idle … and NOTHING wrote the page's error state.
//   Measured with the PUT aborted at the browser level:
//
//     button after: "Save landing page" | jf-btn-primary     <- correctly idle
//     banners after: []                                      <- no banner
//     toast after:   null
//     page text changed? NO — the page says NOTHING new
//
//   Same run, same server: landing HTTP 500 → red banner "Failed to save" ✓;
//   footer network failure → red banner "Failed to save" ✓. So a merchant who
//   lost connectivity could not tell "saved" from "not saved" — the exact class
//   `components/SaveButton.tsx` and `saveStorefrontSettings` already answer, and
//   the landing handler simply had no `catch`.
//
// WHY A SOURCE SCAN AND NOT ONLY THE BROWSER SPEC
//   The rendered half is measured by `web/e2e/settings-landing-network-failure.spec.ts`
//   in the `e2e` gate (the button stays idle AND the failure sentence is on
//   screen). A source scan cannot render — but it CAN stop the `try` coming back
//   out of the handler, which is the whole fix and is invisible to any assertion
//   about the happy path. Both halves are needed: the spec proves the behaviour
//   on today's bytes, this proves the next revert cannot be silent.
//
// WHICH ELEMENT, AND WHY THIS ONE
//   The failure lands in `lpUrlErr` — the red banner at the landing FIELDS
//   (`bg-red-50 … text-red-700`), immediately above the button that was pressed,
//   with the same sentence the handler's own HTTP-refusal branch already uses.
//   `setErr` was the alternative (it is what `saveStorefrontSettings` uses), but
//   on this page `err` renders in the banner at the very TOP of a 2600-line
//   page — several screens from the button — which is the distance the
//   SaveButton/Toast work exists to remove. Either is a real fix; this one puts
//   the message where the merchant is looking, and it is asserted as such below.

/** The `{ … }` block that starts at `open` (index of the `{`), braces balanced. */
function blockAfter(src: string, open: number): string | null {
  if (open < 0 || src[open] !== "{") return null;
  let depth = 0;
  for (let i = open; i < src.length; i += 1) {
    if (src[i] === "{") depth += 1;
    else if (src[i] === "}") {
      depth -= 1;
      if (depth === 0) return src.slice(open, i + 1);
    }
  }
  return null;
}

/**
 * The body of `saveLandingPage`, from its declaration to its closing brace. The
 * slice ends at the first `\n  }` (two-space indent) — the function's own brace;
 * every block INSIDE it closes at four spaces or deeper. At EOF (a fixture with
 * no trailing newline, or a handler that is the last thing in the file) the rest
 * of the source IS the body — returning `null` there would make every assertion
 * below pass vacuously, which this guard's own bite proof caught.
 */
function landingHandlerBody(settingsSrc: string): string | null {
  const at = settingsSrc.indexOf("async function saveLandingPage");
  if (at < 0) return null;
  const rest = settingsSrc.slice(at);
  const end = rest.indexOf("\n  }\n");
  return end < 0 ? rest : rest.slice(0, end + 4);
}

/**
 * Every reason a `saveLandingPage` source fails the THROWN/network contract.
 * A list rather than one `assert`, so the real file and the pre-fix fixture are
 * judged by the SAME reader — the bite proof below is then not a second,
 * hand-written copy of the rule.
 */
function landingThrownFailureGap(settingsSrc: string): string[] {
  const problems: string[] = [];
  const body = landingHandlerBody(settingsSrc);
  if (body === null) return ["no `async function saveLandingPage` handler to judge"];

  const tryAt = body.indexOf("try {");
  if (tryAt < 0) {
    return [
      "saveLandingPage has no `try` — a THROWN/network failure (the fetch rejects) propagates " +
        "out of the handler, SaveButton's own catch keeps the button idle (correct) and NOTHING " +
        "writes the page's error state, so the merchant is told nothing at all (measured: " +
        "banners [], toast null, no new page text)",
    ];
  }
  const tryBlock = blockAfter(body, body.indexOf("{", tryAt));
  if (!tryBlock) {
    problems.push("saveLandingPage's `try` has no balanced block — re-anchor this guard");
    return problems;
  }
  // The landing PUT itself must be INSIDE that try, or the catch cannot see a rejection from it.
  if (!tryBlock.includes("await fetch(`/api/workspaces/${lpWorkspaceId}/settings`")) {
    problems.push(
      "the landing PUT (`await fetch(`/api/workspaces/${lpWorkspaceId}/settings`)`) is OUTSIDE " +
        "the try — a rejected/aborted fetch would still leave the page silent",
    );
  }
  const catchAt = body.indexOf("catch", tryAt + tryBlock.length);
  if (catchAt < 0) {
    problems.push("saveLandingPage's try has no catch clause — the thrown failure is swallowed");
    return problems;
  }
  const catchBlock = blockAfter(body, body.indexOf("{", catchAt));
  if (!catchBlock) {
    problems.push("saveLandingPage's catch has no balanced block — re-anchor this guard");
    return problems;
  }
  if (!/setLpUrlErr\(t\("set\.failedSave"\)\)/.test(catchBlock)) {
    problems.push(
      "saveLandingPage's catch does not write the failure sentence (`setLpUrlErr(t(\"set.failedSave\"))`) " +
        "— the rejected request would still put NOTHING on screen",
    );
  }
  // The failure must NOT confirm on the button (SaveButton's contract).
  if (!/return false;/.test(body)) {
    problems.push("saveLandingPage no longer returns `false` on the failure path — the button would confirm");
  }
  return problems;
}

test("a THROWN/network failure on the landing save must reach the page (card t_9fad3d6a)", () => {
  // Anchor: the handler, the state it writes, and the element that renders it.
  assert.ok(
    settings.includes('const [lpUrlErr, setLpUrlErr] = useState("")'),
    "settings/page.tsx no longer declares `lpUrlErr` — the landing failure banner this guard " +
      "keys on is gone; re-anchor rather than trusting a green run",
  );
  assert.match(
    settings,
    /\{lpUrlErr && \(\s*<p className="max-w-xl rounded-md bg-red-50 px-2 py-1\.5 text-xs font-medium text-red-700">/,
    "the `lpUrlErr` element is no longer the red banner at the landing fields — the failure would " +
      "have nowhere visible to land",
  );
  // The sentence must be a dictionary key with every locale, or `t()` renders the raw key.
  const dict = readFileSync(`${SRC}components/i18nDict.ts`, "utf8");
  const entry = dict.slice(dict.indexOf('"set.failedSave"'));
  const dictBody = entry.slice(0, entry.indexOf("},"));
  for (const loc of ["en:", "ms:", "zh:", "ta:", "ar:"]) {
    assert.ok(dictBody.includes(loc), `set.failedSave is missing its \`${loc}\` locale`);
  }

  assert.deepEqual(
    landingThrownFailureGap(settings),
    [],
    "A THROWN/network failure on the landing-page save must put a failure message on screen " +
      "(card t_9fad3d6a). The button already stays idle — which is correct — so this message is " +
      "the ONLY claim the merchant gets.",
  );
});

test("the thrown-failure guard would fail on the handler it replaced (in-file bite proof)", () => {
  // The EXACT pre-fix handler, planted hermetically: the fetch at top level, no
  // try. The same reader above must reject it — without this, a scan that
  // matched nothing would read as a clean file, which is how the defect shipped.
  const preFix = [
    "  async function saveLandingPage(): Promise<boolean> {",
    "    if (!lpWorkspaceId) return false;",
    "    setMsg(\"\");",
    "    setErr(\"\");",
    "    setLpUrlErr(\"\");",
    "    const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {",
    "      method: \"PUT\",",
    "    });",
    "    if (r.ok) {",
    "      showMsgOk(t(\"set.lpSaved\"));",
    "      return true;",
    "    } else {",
    "      const d = await r.json().catch(() => ({}));",
    "      setLpUrlErr(apiErr(d, t(\"set.failedSave\"), t));",
    "    }",
    "    return false;",
    "  }",
  ].join("\n");
  assert.ok(
    landingThrownFailureGap(preFix).length > 0,
    "the PRE-FIX handler (no try) was read as satisfying the THROWN/network contract — the guard " +
      "is not looking at the real shape and would pass on the code it replaces",
  );
  // ...and a bare try that does NOT cover the fetch is still rejected: the catch
  // has to be able to SEE the rejection.
  const tryOutsideFetch = preFix.replace(
    "    const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {",
    "    try { setMsg(\"\"); } catch { setErr(\"\"); }\n" +
      "    const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {",
  );
  assert.ok(
    landingThrownFailureGap(tryOutsideFetch).length > 0,
    "a try that does not enclose the landing fetch was read as covering it — the guard is " +
      "matching the presence of `try` rather than the property it is about",
  );
  // ...and the FIX must be read as clean, so the guard does not red on its own remedy.
  const fixed = preFix.replace(
    "    const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {",
    "    try {\n      const r = await fetch(`/api/workspaces/${lpWorkspaceId}/settings`, {",
  ).replace(
    "    return false;\n  }",
    "    } catch {\n      setLpUrlErr(t(\"set.failedSave\"));\n    }\n    return false;\n  }",
  );
  assert.deepEqual(
    landingThrownFailureGap(fixed),
    [],
    "the fixed handler was still read as having a gap — the guard reds on its own remedy",
  );
});
