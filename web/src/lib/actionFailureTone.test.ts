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
//      (`data-tone`) so the rendered result is observable;
//   5. WHERE the tone CHOICE is a decision (`showMsgOk` on one branch,
//      `showMsgFail` on the other), the condition deciding it is the API
//      response — never a statically constant expression, and on /sales'
//      PARTIAL bulk-capture the failure branch is the `d.failed` one.
//
// WHY RULE 5 EXISTS (card t_0863b3ca, follow-up to the QA card t_98be596e)
//   Rules 1–4 pin the MECHANISM: the tone state, the pair, the fill, the ARIA.
//   None of them reads WHICH branch of a `d.failed ? … : …` carries the failure,
//   so the judgement PR #1012 made — a mixed bulk-capture run is a FAILURE, only
//   an all-captured run is a success — could be neutralised by VALUE and every
//   gate stayed green. Measured on the merged tree e53bf391 (2026-10-07):
//
//     web/src/app/sales/page.tsx:647
//     -      if (d.failed) showMsgFail(summary);
//     +      if (false) showMsgFail(summary);
//
//   A mixed outcome reported SUCCESS again and this file still printed
//   `tests 6 / pass 6 / fail 0`, because the `showMsgFail` CALL was still there
//   for rule 2 to find — only the condition had been hollowed out. Deleting a
//   call is not the only way to remove a tone; making its guard unreachable is
//   the same defect with every string this file grepped for still in place.
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
import ts from "typescript";
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
// The raw source, for the one reader below that needs a real AST: a tone
// CHOICE is a control-flow fact (`which branch runs`), which only a parser can
// answer — a regex over `codeOnly` cannot tell a condition from a string that
// looks like one.
const RAW_OF = new Map(FILES.map((rel) => [rel, readFileSync(`${SRC}${rel}`, "utf8")]));

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

/**
 * A site where the code CHOOSES which tone to report. Two shapes, both read off
 * the AST because the answer is a control-flow fact:
 *
 *   * a branch — `if (cond) showMsgFail(x); else showMsgOk(y);` (36 of these);
 *   * a SELECTED setter — `(cond ? showMsgOk : showMsgFail)(t(…))`, which is how
 *     /settings writes its credential and tracking saves.
 *
 * A site counts only when BOTH tones appear at it. That is what makes it a
 * decision rather than one of the many bare guards (`if (!r.ok) showMsgFail(…)`)
 * whose branch is the failure by construction and which therefore cannot be
 * hollowed out by flipping a condition.
 */
type ToneChoice = {
  line: number;
  /** the condition expression itself — what the constant check judges */
  condNode: ts.Node;
  cond: string;
  /** the setters reachable on each side of the condition (branch shape), or the
   *  setter each arm of the ternary IS. */
  then: Set<"showMsgOk" | "showMsgFail">;
  else: Set<"showMsgOk" | "showMsgFail">;
};

/** The tone setters CALLED anywhere inside a subtree (the branch shape). */
function settersCalledIn(node: ts.Node | undefined): Set<"showMsgOk" | "showMsgFail"> {
  const out = new Set<"showMsgOk" | "showMsgFail">();
  if (!node) return out;
  const visit = (n: ts.Node): void => {
    if (ts.isCallExpression(n)) {
      const e = n.expression;
      if (ts.isIdentifier(e) && (e.text === "showMsgOk" || e.text === "showMsgFail")) out.add(e.text);
    }
    ts.forEachChild(n, visit);
  };
  visit(node);
  return out;
}

/** The tone setter an expression IS, not one it calls (the ternary's branches). */
function setterNamed(node: ts.Node | undefined): "showMsgOk" | "showMsgFail" | null {
  if (!node) return null;
  if (ts.isIdentifier(node)) {
    return node.text === "showMsgOk" || node.text === "showMsgFail" ? node.text : null;
  }
  if (ts.isParenthesizedExpression(node)) return setterNamed(node.expression);
  return null;
}

/**
 * Is this condition STATICALLY constant? That is the mutation the card is about:
 * `if (false)` / `if (0)` / `if (!1)` / `if ("")` is not a decision at all — one
 * branch is dead code that no build error and no gate can see.
 *
 * `true`/`false` are keywords, every other primitive is the generic
 * `LiteralToken` family. Deliberately NOT attempted: a whole-expression constant
 * folder (`1 > 2`, `SOME_ENV && false`). Flagging those needs a semantic
 * evaluation this file cannot do, and a check that only ever sees the literal
 * form is honest about it in the failure message rather than guessing.
 */
function isConstantCondition(node: ts.Node): boolean {
  if (!ts.isExpression(node)) return false;
  if (node.kind === ts.SyntaxKind.TrueKeyword || node.kind === ts.SyntaxKind.FalseKeyword) return true;
  if (ts.isNumericLiteral(node) || ts.isBigIntLiteral(node)) return true;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return true;
  // `!0` / `!1` — the minifier's spelling of `true` / `false`.
  if (ts.isPrefixUnaryExpression(node) && node.operator === ts.SyntaxKind.ExclamationToken) {
    return ts.isNumericLiteral(node.operand);
  }
  return false;
}

/** Every tone CHOICE in one page, in source order. */
function toneChoices(rel: string): ToneChoice[] {
  return toneChoicesFrom(RAW_OF.get(rel) ?? "");
}

/** The same reader, against a source string (so the bite proofs can drive it). */
function toneChoicesFrom(src: string): ToneChoice[] {
  const sf = ts.createSourceFile("guard.tsx", src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: ToneChoice[] = [];
  const lineAt = (n: ts.Node) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
  const text = (n: ts.Node) => n.getText(sf).replace(/\s+/g, " ");
  const visit = (n: ts.Node): void => {
    if (ts.isIfStatement(n)) {
      const then = settersCalledIn(n.thenStatement);
      const else_ = settersCalledIn(n.elseStatement);
      if (then.size > 0 && else_.size > 0) {
        out.push({ line: lineAt(n), condNode: n.expression, cond: text(n.expression), then, else: else_ });
      }
    }
    if (ts.isConditionalExpression(n)) {
      const t = setterNamed(n.whenTrue);
      const f = setterNamed(n.whenFalse);
      if (t && f && t !== f) {
        out.push({
          line: lineAt(n),
          condNode: n.condition,
          cond: text(n.condition),
          then: new Set([t]),
          else: new Set([f]),
        });
      }
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  return out;
}

const CHOICES_OF = new Map(FILES.map((rel) => [rel, toneChoices(rel)]));
const ALL_CHOICES = FILES.flatMap((rel) => CHOICES_OF.get(rel)!.map((c) => ({ rel, ...c })));


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

// ─────────────────────────────────────────────────────────────────────────────
// Rule 5 — a tone CHOICE must be a real decision (card t_0863b3ca)
// ─────────────────────────────────────────────────────────────────────────────

/**
 * The /sales PARTIAL bulk-capture decision, as PR #1012 left it. The page is
 * partial BY DESIGN: `d.failed > 0` is a FAILURE (some authorizations are still
 * uncaptured and the merchant has to read each status), only `d.failed === 0` is
 * a success. This is the writer whose guard this card is about, and it is pinned
 * here rather than derived, for the same reason the other lists are: a change to
 * it is a DELIBERATE edit to this file with a reason, not a silent drift.
 */
const SALES_TONE_CHOICES: { line: number; cond: string; fail: "then" | "else"; ok: "then" | "else" }[] = [
  { line: 647, cond: "d.failed", fail: "then", ok: "else" },
];

test("the tone-choice walk found the app, and it reads both shapes (it cannot pass vacuously)", () => {
  // A file whose only tone writers are ONE tone (a bare `if (!r.ok) showMsgFail`)
  // is not a decision and must not be reported as one — that is the shape most of
  // the tree has, and reporting it would make this rule fire everywhere and be
  // switched off.
  assert.ok(
    ALL_CHOICES.length >= 15,
    `the walk found only ${ALL_CHOICES.length} tone CHOICES under web/src — expected 15+ (measured 19 ` +
      `on the merged tree: 16 branches plus /settings' 3 selected setters). Found: ` +
      `${JSON.stringify(ALL_CHOICES.map((c) => `${c.rel}:${c.line}`))}`,
  );
  for (const rel of PINNED_TONED) {
    assert.ok(
      CHOICES_OF.get(rel)!.length > 0,
      `src/${rel} has no tone CHOICE any more — every failure is now unconditional, or the ` +
        `reader stopped seeing this page (re-anchor it rather than trusting the pass)`,
    );
  }
  // BOTH shapes, read from sources this test writes, so neither can rot away
  // unnoticed: the branch form and the selected-setter form.
  const branch = toneChoicesFrom('if (d.failed) showMsgFail(s); else showMsgOk(s);');
  assert.equal(branch.length, 1, "the branch shape was not read as a tone choice");
  assert.equal(branch[0].cond, "d.failed", "the branch shape's condition was mis-read");
  assert.ok(branch[0].then.has("showMsgFail"), "the failure branch was not read as the `then` side");
  assert.ok(branch[0].else.has("showMsgOk"), "the success branch was not read as the `else` side");
  const sel = toneChoicesFrom('(r.ok ? showMsgOk : showMsgFail)(t("x"));');
  assert.equal(sel.length, 1, "the selected-setter shape was not read as a tone choice");
  assert.equal(sel[0].cond, "r.ok", "the selected-setter condition was mis-read");
  assert.ok(sel[0].then.has("showMsgOk"), "the selected-setter's success arm was not read");
  assert.ok(sel[0].else.has("showMsgFail"), "the selected-setter's failure arm was not read");
  // ...and a ONE-TONE guard is not a choice.
  assert.equal(
    toneChoicesFrom('if (!r.ok) showMsgFail(s);').length,
    0,
    "a bare `if (!r.ok) showMsgFail(…)` was read as a tone CHOICE — this rule would fire on the " +
      "whole tree and have to be switched off",
  );
});

test("no tone choice is decided by a CONSTANT condition (t_0863b3ca)", () => {
  const offenders: string[] = [];
  for (const rel of FILES) {
    for (const c of CHOICES_OF.get(rel)!) {
      // The condition node itself: a constant makes one whole branch unreachable,
      // so the code no longer CHOOSES a tone at all.
      if (isConstantCondition(c.condNode)) {
        offenders.push(
          `src/${rel}:${c.line} — the tone of a ${c.then.has("showMsgFail") ? "failure" : "success"}/` +
            `${c.else.has("showMsgOk") ? "success" : "failure"} pair is decided by the CONSTANT ` +
            `condition \`${c.cond}\`. One branch is dead code, so the other outcome can never be ` +
            `reported — and every string this guard greps for is still present. This is the shape ` +
            `measured on the merged tree: \`if (d.failed) showMsgFail(summary);\` → ` +
            `\`if (false) showMsgFail(summary);\`, which left this file 6/6 green. Decide the tone on ` +
            `the RESPONSE that carries the outcome, not on a literal.`,
        );
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `A tone CHOICE decided by a constant is not a decision (card t_0863b3ca):\n  ${offenders.join("\n  ")}`,
  );
});

test("/sales' mixed bulk-capture reports the FAILURE tone on the `d.failed` branch (t_0863b3ca)", () => {
  const choices = CHOICES_OF.get("app/sales/page.tsx")!;
  const offenders: string[] = [];
  for (const pinned of SALES_TONE_CHOICES) {
    // Matched on the CONDITION, not the line: a line number is a fact about a
    // revision, and this page is edited constantly (it moved by 200 lines
    // between the pre-fix and merged trees this card measured).
    const hits = choices.filter((c) => c.cond === pinned.cond);
    if (hits.length === 0) {
      offenders.push(
        `app/sales/page.tsx — no tone choice on \`${pinned.cond}\` any more. PR #1012's decision was ` +
          `"a mixed bulk-capture run (d.failed > 0) is a FAILURE; only an all-captured run is a ` +
          `success". If that decision was dropped, the mixed summary now wears the SUCCESS banner and ` +
          `the merchant is told a partial capture worked. If the DECISION MOVED (a different variable, ` +
          `an extracted helper), move this row in the same edit.`,
      );
      continue;
    }
    const hit = hits[0];
    if (!hit.then.has("showMsgFail") || !hit.else.has("showMsgOk")) {
      offenders.push(
        `app/sales/page.tsx:${hit.line} — \`${pinned.cond}\` chooses the WRONG tone: ` +
          `then=[${[...hit.then].join(",")}] else=[${[...hit.else].join(",")}]. A mixed run must report ` +
          `the FAILURE tone (showMsgFail on the \`d.failed\` branch); only an all-captured run is a ` +
          `success.`,
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `The /sales bulk-capture tone decision is unguarded (card t_0863b3ca):\n  ${offenders.join("\n  ")}`,
  );
});

test("rule 5 bites on the exact mutation the card measured (in-file bite proof)", () => {
  // THE mutation, verbatim from the card: the failure call is still THERE, only
  // its condition is hollowed out. A guard that greps for `showMsgFail(` sees
  // nothing wrong — which is why this one reads the condition.
  const mutated = 'if (false) showMsgFail(summary);\nelse showMsgOk(summary);';
  const read = toneChoicesFrom(mutated);
  assert.equal(read.length, 1, "the mutated writer was not read at all — the guard cannot bite on it");
  assert.ok(
    isConstantCondition(read[0].condNode),
    "the constant condition `false` was NOT flagged — this is the exact mutation that shipped green",
  );
  // ...and the SHIPPED code is not flagged, or the guard would red on main.
  const shipped = 'if (d.failed) showMsgFail(summary);\nelse showMsgOk(summary);';
  const shippedRead = toneChoicesFrom(shipped);
  assert.equal(shippedRead.length, 1, "the shipped writer was not read as a tone choice");
  assert.ok(
    !isConstantCondition(shippedRead[0].condNode),
    "the shipped `d.failed` condition was flagged as constant — the guard reds on its own fix",
  );
  // The rest of the constant spellings, so a rewrite of the mutation cannot slip
  // through: `!1` is what a minifier emits, `0`/`""` are what a careless edit
  // leaves behind.
  for (const c of ["false", "true", "0", "1", '""', "!0", "!1"]) {
    const rows = toneChoicesFrom(`if (${c}) showMsgFail(s); else showMsgOk(s);`);
    assert.ok(
      isConstantCondition(rows[0].condNode),
      `\`if (${c})\` was not read as a CONSTANT condition`,
    );
  }
  for (const c of ["d.failed", "r.ok", "!r.ok", 'outcome.kind !== "needs_ack"']) {
    const rows = toneChoicesFrom(`if (${c}) showMsgFail(s); else showMsgOk(s);`);
    assert.ok(
      !isConstantCondition(rows[0].condNode),
      `the real condition \`${c}\` was read as constant — the rule would fire on the whole tree`,
    );
  }
});
