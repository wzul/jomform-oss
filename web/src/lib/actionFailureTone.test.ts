// Repo-wide guard: a page's ACTION FEEDBACK must not render a FAILURE and a
// SUCCESS in the same chrome (card t_c88c854d).
//
// THE DEFECT THIS PINS (found while verifying t_2574be79, PR #997 rework)
//   /settings had just been taught the rule "the tone travels with the text"
//   (card t_ad2af554: the `showMsgOk`/`showMsgFail` pair, a red failure banner,
//   no auto-dismiss for a failure). Three other pages held ONE message state
//   carrying BOTH outcomes and painted it with ONE class list — indigo 12px
//   text, no fill, no icon:
//
//     web/src/app/sales/page.tsx:849          {msg && <p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>}
//     web/src/app/spam/page.tsx:175           same chrome
//     web/src/app/form-settings/page.tsx:1257 same chrome
//
//   so a successful refund and a "Refund gagal <ref>." (failed refund) were the
//   same line on screen, and the only difference was the sentence. A merchant
//   scanning for a failure saw nothing that read as one. Measured on the built
//   pre-fix tree (Chromium, production build, API stubbed at the browser level,
//   a real 500 on the action's endpoint, fresh localStorage + cookies cleared,
//   lang=en): /sales refund 500 and /sales refund 200 both read
//   `{tone:null, bg:#ffffff, ink:rgb(94,106,210), role:null}`.
//
// WHICH PAGES THE RULE APPLIES TO — and why the answer is DERIVED, then PINNED
//   The rule is about a page whose `msg` state carries a FAILURE as well as a
//   success. A page whose every failure goes somewhere else (a separate
//   `setErr(...)` → red banner) has a `msg` holding successes only, and one
//   chrome is CORRECT for it — `/profile`, `LegalTab`, `MembersTab` and
//   `/templates` are all that shape. Hard-coding either side would rot: a page
//   that later routes a failure into `msg` (the defect) would stay outside a
//   hand-kept exclusion, and a page that stops doing so would stay inside. So
//   the membership is COMPUTED from each page's own writers and then asserted
//   against a PINNED list — a page entering or leaving the set is a deliberate
//   edit to this file, with a reason, not a silent drift.
//
// WHAT IS ASSERTED PER PAGE IN THE SET
//   1. a `msgOk` tone state starting TRUE, and a `showMsgOk`/`showMsgFail` pair
//      that sets the tone and the text in ONE call — never inferred from the
//      string, never left stale from a previous save;
//   2. every writer of a non-empty message goes through that pair (the pair's
//      own bodies are the only `setMsg(<non-empty>)` left, and they set the tone
//      on the line above);
//   3. the element that renders `{msg}` branches its class list on the tone,
//      with a `bg-red-50` failure fill and an emerald success branch;
//   4. a failure is a live `role="alert"`, and the element exposes its tone
//      (`data-tone`) so the rendered result is observable.
//
// WHY A SOURCE SCAN AND NOT THE BROWSER SPEC ALONE
//   `web/e2e/action-failure-tone.spec.ts` measures the RENDERED result on the
//   three pages (fill, ink, ARIA role, plus a success control per page). What it
//   cannot do is reach a writer nobody drove — /form-settings alone has 14. This
//   file is the repo-wide half: it walks EVERY `.tsx` under `web/src`, so a page
//   added later fails HERE, next to the reason, without anyone remembering to
//   drive it.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { codeOnly } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/

/** Every `.tsx` under web/src, as a path relative to web/src. */
function tsxFiles(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (name.endsWith(".tsx")) out.push(p.slice(SRC.length));
    }
  };
  walk(SRC);
  return out.sort();
}

const FILES = tsxFiles();
const CODE_OF = new Map(FILES.map((rel) => [rel, codeOnly(readFileSync(`${SRC}${rel}`, "utf8"))]));

/** A page/component that renders a `msg` state at all. */
const MSG_RENDERERS = FILES.filter((rel) => /const \[msg, setMsg\] = useState\(/.test(CODE_OF.get(rel)!));

/** The bodies of a page's `showMsgOk`/`showMsgFail` pair, blanked out. */
function withoutTonePair(code: string): string {
  return code.replace(
    /const showMsg(?:Ok|Fail) = useCallback\(\(text: string\) => \{[\s\S]*?\}, \[\]\);/g,
    (m) => m.replace(/[^\n]/g, " "),
  );
}

/**
 * Every `name(...)` call in the file, with its argument read by BALANCING
 * PARENTHESES — not by stopping at the first `)`. `apiErr(d, t("prof.failed"), t)`
 * and `r.ok ? t("fs.saved") : t("prof.failed")` both contain inner calls, and a
 * naive `[^)]*` hands back a truncated argument that can HIDE the failure
 * sentence — which is exactly the classification this file turns on.
 */
function callsOf(code: string, name: string): { line: number; call: string; arg: string }[] {
  const out: { line: number; call: string; arg: string }[] = [];
  for (const m of code.matchAll(new RegExp(`(?<![\\w$.])${name}\\(`, "g"))) {
    let i = m.index + m[0].length;
    let depth = 1;
    let quote = "";
    while (i < code.length && depth > 0) {
      const c = code[i];
      if (quote) {
        if (c === "\\") i += 1;
        else if (c === quote) quote = "";
      } else if (c === '"' || c === "'" || c === "`") quote = c;
      else if (c === "(") depth += 1;
      else if (c === ")") depth -= 1;
      i += 1;
    }
    const arg = code.slice(m.index + m[0].length, i - 1).trim();
    out.push({
      line: code.slice(0, m.index).split("\n").length,
      call: `${name}(${arg.replace(/\s+/g, " ")})`,
      arg,
    });
  }
  return out;
}

/**
 * Every writer of a NON-EMPTY message through the bare setter, i.e. one that
 * bypasses the tone pair. `setMsg("")` resets the TEXT and deliberately leaves
 * the tone alone (the next writer sets both), so it is out of scope; and the
 * pair's own bodies are blanked by `withoutTonePair`, because they set the tone
 * on the line above (pinned separately).
 */
function untonedWriters(code: string): { line: number; call: string; arg: string }[] {
  return callsOf(withoutTonePair(code), "setMsg").filter((w) => w.arg !== '""');
}

/**
 * The failure sentences this page puts into its message state — the property
 * that puts it IN the tonal set. Two shapes, one per era of the code:
 *
 *   * `showMsgFail(...)` — the FIXED shape: the call IS a failure by
 *     construction, whatever sentence it carries;
 *   * a bare `setMsg(<failure>)` — the PRE-FIX shape the card is about,
 *     recognised from the argument: `apiErr(...)` / `publishErrorText(...)` (the
 *     localized failure paths) or a `t("…Fail")` / `t("prof.failed")` key. A
 *     confirmation such as `t("fs.saved")` does not match, which is what keeps
 *     `/profile` and the two tabs out of the set.
 */
function failureWriters(code: string): { line: number; call: string; arg: string }[] {
  const found = callsOf(code, "showMsgFail");
  // `/settings` selects the setter instead of calling it —
  // `(d.verified === true ? showMsgOk : showMsgFail)(t(...))` — so a page that
  // merely REFERENCES the failure setter also has a failure path by
  // construction: nobody defines one for nothing.
  if (found.length === 0 && /\bshowMsgFail\b/.test(code)) {
    const line = code.slice(0, code.search(/\bshowMsgFail\b/)).split("\n").length;
    found.push({ line, call: "showMsgFail (selected, not called directly)", arg: "" });
  }
  for (const w of untonedWriters(code)) {
    if (/\bapiErr\(|\bpublishErrorText\(/.test(w.arg)) found.push(w);
    else if (/t\("[^"]*(Fail|fail|failed)/.test(w.arg)) found.push(w);
    else if (/t\("prof\.failed"\)/.test(w.arg)) found.push(w);
  }
  return found;
}

/**
 * The pages whose `msg` carries a failure — and therefore owe the tone. PINNED:
 * `sales`, `spam` and `form-settings` are this card's three sites; `settings` is
 * where the rule came from (`showMsgOk`/`showMsgFail`, t_ad2af554). Any OTHER
 * page appearing here is a new un-toned failure banner and the fix is the tone
 * pair, not this list; a page leaving is a deliberate narrowing that belongs in a
 * diff with a reason.
 */
const PINNED_TONED = ["app/form-settings/page.tsx", "app/sales/page.tsx", "app/settings/page.tsx", "app/spam/page.tsx"];

/**
 * The pages whose failures are routed to a SEPARATE red banner, so one `msg`
 * chrome is correct for them. PINNED from the other side, and for the same
 * reason: if one of them starts putting a failure in `msg`, it moves to
 * PINNED_TONED above and gets the pair.
 */
const PINNED_SEPARATE_ERR = [
  "app/profile/page.tsx",
  "components/LegalTab.tsx",
  "components/MembersTab.tsx",
  "app/templates/page.tsx",
];

const computedToned = MSG_RENDERERS.filter((rel) => failureWriters(CODE_OF.get(rel)!).length > 0);

/**
 * The pages this card actually re-chromed. `/settings` is in PINNED_TONED (it is
 * where the tone rule came from) but its failure INDICATION is owned by another
 * pair of guards already on main — `src/lib/toastTone.test.ts` and
 * `web/e2e/settings-toast-failure.spec.ts` — which is why the ARIA/`data-tone`
 * obligations below are asserted only for the three pages this card changed.
 */
const OWNS_BANNER_ARIA = ["app/form-settings/page.tsx", "app/sales/page.tsx", "app/spam/page.tsx"];

test("the walk found the app and the pages this rule is about (it cannot pass vacuously)", () => {
  assert.ok(FILES.length >= 40, `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`);
  assert.ok(
    MSG_RENDERERS.length >= 6,
    `only ${MSG_RENDERERS.length} files render a \`msg\` state — expected the toned four plus the ` +
      `separate-\`err\` four; a shrinking population reads as a clean pass (found: ${MSG_RENDERERS.join(", ")})`,
  );
  // The reader the whole scan keys on, against a shape it must not truncate: a
  // naive first-`)` scan hands back `r.ok ? t("fs.saved"` here and loses the
  // failure sentence entirely.
  const nested = 'setMsg(r.ok ? t("fs.saved") : t("prof.failed"));';
  assert.equal(untonedWriters(nested).length, 1, "a non-empty setMsg was not read as a writer");
  assert.equal(
    untonedWriters(nested)[0].arg,
    'r.ok ? t("fs.saved") : t("prof.failed")',
    "the writer's argument was truncated at an inner call's `)`, so a failure sentence can hide from this scan",
  );
  assert.equal(untonedWriters('setMsg("");').length, 0, "an empty setMsg was read as a writer");
  // ...and the tone pair's own body must be recognised as the FIX, not a writer:
  // `withoutTonePair` blanks both setters, so a page using them has none left.
  const pairBody =
    'const showMsgFail = useCallback((text: string) => {\n    setMsgOk(false);\n    setMsg(text);\n  }, []);';
  assert.equal(
    untonedWriters(withoutTonePair(pairBody)).length,
    0,
    "the tone pair's own body was read as an untoned writer — every toned page would fail on its own fix",
  );
});

test("the set of pages whose `msg` carries a failure is exactly the pinned one", () => {
  assert.deepEqual(
    computedToned,
    PINNED_TONED,
    `The pages whose \`msg\` state carries a FAILURE (and therefore must carry its tone) changed.\n` +
      `  computed: ${JSON.stringify(computedToned)}\n  pinned:   ${JSON.stringify(PINNED_TONED)}\n` +
      `A page that APPEARED here has started writing a failure into its message banner: give it the ` +
      `tone pair (\`msgOk\` + \`showMsgOk\`/\`showMsgFail\`) and a red, live failure banner — that is ` +
      `card t_c88c854d. A page that LEFT is a deliberate narrowing: if its failures now go to a ` +
      `separate \`setErr\` banner, move it to PINNED_SEPARATE_ERR with the reason.`,
  );
});

test("a page outside the set keeps `msg` for successes only (why one chrome is right for it)", () => {
  const offenders: string[] = [];
  for (const rel of PINNED_SEPARATE_ERR) {
    assert.ok(
      MSG_RENDERERS.includes(rel),
      `src/${rel} no longer renders a \`msg\` state — re-anchor this guard rather than trusting it`,
    );
    const code = CODE_OF.get(rel)!;
    const w = failureWriters(code)[0];
    if (w) {
      offenders.push(
        `src/${rel} — line ${w.line} writes a FAILURE into \`msg\` (${w.call}) while being pinned as a ` +
          `page that keeps \`msg\` for successes. Its failure now shares the success chrome: move it to ` +
          `PINNED_TONED and give it the tone pair (card t_c88c854d)`,
      );
    }
    if (!/setErr\(/.test(code)) {
      offenders.push(`src/${rel} never calls setErr — the separate failure banner this page is pinned for is gone`);
    }
  }
  assert.deepEqual(offenders, [], offenders.join("\n"));
});

test("every toned page carries the tone pair, and every writer goes through it", () => {
  const offenders: string[] = [];
  for (const rel of PINNED_TONED) {
    const name = `src/${rel}`;
    const code = CODE_OF.get(rel)!;
    // 1. the tone state, starting TRUE (so a message set without a tone reads as
    //    a confirmation, which is what every pre-existing success path is).
    if (!/const \[msgOk, setMsgOk\] = useState\(true\)/.test(code)) {
      offenders.push(`${name} — no \`msgOk\` tone state starting true`);
    }
    // 2. the pair: the tone and the text set in ONE call. Matched as the body
    //    shape, so a setter that sets only one of them is caught.
    if (!/const showMsgOk = useCallback\(\(text: string\) => \{\s*setMsgOk\(true\);\s*setMsg\(text\);\s*\}, \[\]\);/.test(code)) {
      offenders.push(`${name} — no \`showMsgOk\` setting the tone and the text together`);
    }
    if (!/const showMsgFail = useCallback\(\(text: string\) => \{\s*setMsgOk\(false\);\s*setMsg\(text\);\s*\}, \[\]\);/.test(code)) {
      offenders.push(`${name} — no \`showMsgFail\` setting the tone and the text together`);
    }
    // 3. NO writer may bypass the pair. This is the invariant the card is about:
    //    a bare `setMsg(<non-empty>)` sets text without a tone, so a failure can
    //    inherit whatever tone the previous save left. The pair's own bodies are
    //    excluded (they set the tone on the line above, which rule 2 pins).
    for (const w of untonedWriters(withoutTonePair(code))) {
      offenders.push(
        `${name} — line ${w.line} writes the message text without its tone: \`${w.call}\`. ` +
          `Every non-empty writer must go through showMsgOk/showMsgFail`,
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `A page whose message state carries BOTH outcomes must carry the TONE with it (the rule card ` +
      `t_ad2af554 established on /settings). Measured pre-fix: /sales refund 500 and refund 200 both ` +
      `{bg:#ffffff, ink:rgb(94,106,210), tone:null} (card t_c88c854d).\n  ${offenders.join("\n  ")}`,
  );
});

test("the element rendering {msg} branches its chrome on the tone, and a failure is red and live", () => {
  const offenders: string[] = [];
  for (const rel of PINNED_TONED) {
    const name = `src/${rel}`;
    const code = CODE_OF.get(rel)!;
    const at = code.indexOf("{msg}");
    if (at < 0) {
      offenders.push(`${name} — no \`{msg}\` render site — re-anchor this guard`);
      continue;
    }
    // The element's OWN opening tag, so a sibling's attributes cannot satisfy a
    // check aimed at it.
    const open = code.lastIndexOf("<", at);
    const tag = code.slice(open, code.indexOf(">", at) + 1);
    const cls = tag.match(/className=\{`([\s\S]*?)`\}/);
    if (!cls) {
      offenders.push(`${name} — the {msg} element has no template-literal className (tag: ${tag})`);
      continue;
    }
    // Both branches present, chosen by the tone: the whole defect was ONE class
    // list for both outcomes.
    if (!/\?[\s\S]*:/.test(cls[1]) || !/msgOk/.test(cls[1])) {
      offenders.push(`${name} — the {msg} element's className does not branch on \`msgOk\``);
    }
    // A failure must be RED-FILLED. (The failure INK is measured for AA by the
    // browser spec and by the sibling colour guards; this pins the fill, which is
    // the visible half of "you cannot tell them apart".)
    if (!/bg-red-50/.test(cls[1])) {
      offenders.push(`${name} — the {msg} element has no failure fill (bg-red-50) in its tone branch`);
    }
    if (!/emerald/.test(cls[1])) {
      offenders.push(`${name} — the {msg} element has no success branch (an emerald fill/ink)`);
    }
    // A failure must be ANNOUNCED, not only seen — and the tone must be
    // OBSERVABLE, which is what lets the browser spec in
    // e2e/action-failure-tone.spec.ts judge the rendered result at all. Asserted
    // for the pages this card owns (see OWNS_BANNER_ARIA).
    if (!OWNS_BANNER_ARIA.includes(rel)) continue;
    if (!/role=\{msgOk \? undefined : "alert"\}/.test(tag)) {
      offenders.push(
        `${name} — a failure banner is not a live region: expected ` +
          `\`role={msgOk ? undefined : "alert"}\` on the {msg} element (tag: ${tag})`,
      );
    }
    if (!/data-tone=\{msgOk \? "success" : "failure"\}/.test(tag)) {
      offenders.push(`${name} — the {msg} element does not expose \`data-tone={msgOk ? "success" : "failure"}\``);
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `A page that paints a failure and a success through one message state must branch its chrome on ` +
      `the tone (card t_c88c854d). The PRE-FIX shape was a single class list — ` +
      `\`className="mt-3 text-xs text-[var(--jf-primary)]"\` — on all three pages, which is what made ` +
      `a failed refund and a successful one identical:\n  ${offenders.join("\n  ")}`,
  );
});

test("the guard would fail on the code it replaced (in-file bite proof)", () => {
  // The exact pre-fix shapes, planted hermetically: each rule above must reject
  // its own mutation. Without this, a scan that matched nothing would read as a
  // clean file.
  const preFixBanner = '<p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>';
  assert.doesNotMatch(preFixBanner, /bg-red-50/, "the pre-fix banner was read as having a failure fill");
  assert.doesNotMatch(preFixBanner, /role=\{/, "the pre-fix banner was read as a live region");
  assert.doesNotMatch(preFixBanner, /data-tone=/, "the pre-fix banner was read as exposing a tone");
  assert.doesNotMatch(preFixBanner, /className=\{`/, "the pre-fix banner was read as branching its className");

  // The pre-fix writer: the text and the tone could not travel together because
  // there was only one setter. It must be READ as a failure writer, which is how
  // the page is put in the toned set at all.
  const preFixWriter = 'setMsg(r.ok ? t("fs.saved") : t("prof.failed"));';
  assert.ok(failureWriters(preFixWriter).length > 0, "the pre-fix writer (a failure reaching `msg`) was not classified");
  assert.doesNotMatch(preFixWriter, /showMsgOk|showMsgFail/, "the pre-fix writer was read as choosing a tone");

  // A page that keeps `msg` for successes only: a confirmation writer is NOT a
  // failure writer, which is what keeps /profile and the two tabs out of the set.
  assert.equal(
    failureWriters('setMsg(t("mem.added"));').length,
    0,
    "a success-only writer was classified as carrying a failure — the set would swallow every page",
  );
  // ...while the localized failure paths are.
  assert.ok(failureWriters('setMsg(apiErr(d, t("x"), t));').length > 0, "apiErr(...) was not read as a failure");
  assert.ok(
    failureWriters('setMsg(publishErrorText(o, t("prof.failed"), t));').length > 0,
    "publishErrorText(...) was not read as a failure",
  );
  assert.ok(failureWriters('setMsg(t("fs.notifFail"));').length > 0, "a `…Fail` dictionary key was not read as a failure");
  // The FIXED shape — a `showMsgFail(...)` call — is a failure by construction,
  // and is what keeps the toned pages in the set now that non-empty bare writers
  // are gone from them.
  assert.ok(failureWriters('showMsgFail(t("fs.notifFail"));').length > 0, "a showMsgFail(...) call was not read as a failure");
  // ...including the SELECTED form /settings uses, where the setter is chosen
  // rather than called: `(d.verified === true ? showMsgOk : showMsgFail)(t(...))`.
  assert.ok(
    failureWriters('(d.verified === true ? showMsgOk : showMsgFail)(t("set.savedCredsUnverified"));').length > 0,
    "a page that SELECTS showMsgFail rather than calling it was not read as carrying a failure",
  );

  // The fixed writer must NOT be seen as a bare writer, or every toned page
  // would fail on its own fix.
  assert.equal(
    untonedWriters('showMsgFail(apiErr(d, t("prof.failed"), t));').length,
    0,
    "showMsgFail(...) was read as a bare setMsg — the rule cannot tell them apart",
  );
});
