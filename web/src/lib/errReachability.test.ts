// Repo-wide guard: a page's LOAD-ONLY error banner cannot be the only home for
// an ACTION failure (card t_bfe1cda5, follow-up to t_c88c854d).
//
// THE DEFECT THIS PINS (measured in a real browser, not inferred)
//   `/sales` keeps ONE `err` state for its LOAD error, and both of its render
//   sites sit inside the empty-table branch:
//
//     web/src/app/sales/page.tsx:1136   {!checking && rows.length === 0 && ( <tr>
//     web/src/app/sales/page.tsx:1138     {err || (responsesView ? … : t("sales.empty"))}
//     web/src/app/sales/page.tsx:1138     {err && ( <div>…retry…</div> )}
//
//   That is the right home for a load error — a failed load is what leaves the
//   table empty. It is the WRONG home for anything else, because the branch only
//   renders while there are NO rows. The per-workspace sound switch lives in the
//   page HEADER, and its save failure was written to `err`:
//
//     web/src/app/sales/page.tsx:448   setErr(t("sales.soundSaveFail"));   // no workspace
//     web/src/app/sales/page.tsx:468   setErr(message);                    // PUT refused
//
//   With any row on the table — the normal state — the toggle flipped back and
//   the merchant was told NOTHING. Read on the production build with one row and
//   a forced 500 on the settings PUT:
//
//     /sales sound PUT 500 -> aria-checked before=false after=false
//     /sales sound PUT 500 -> elements carrying the failure sentence: {"hit":[],"switches":1}
//
//   `aria-checked` returning to `false` proves the handler ran and took the
//   failure path; `hit: []` proves the sentence was rendered in ZERO elements.
//
// WHY A SOURCE SCAN AND NOT ONLY THE BROWSER SPEC
//   `web/e2e/action-failure-tone.spec.ts` drives the switch and measures the
//   RENDERED result, which is the acceptance proof for this card. It cannot see
//   a writer nobody drives — and the page's own writers are not the only way
//   this comes back: a NEW page whose `err` sites are all inside a
//   `rows.length === 0` branch, with an action failure routed to `err`, is the
//   same silent failure with a different file name. This is the repo-wide half.
//
// THE RULE
//   1. A page whose EVERY `err` render site is LOAD-GATED (its enclosing JSX
//      guard mentions `rows.length === 0` / `checking` / `loading`) renders that
//      banner only while the table is empty or still loading.
//   2. Such a page may therefore not write an ACTION failure into `err`: every
//      non-empty `setErr(...)` must live inside the function that drives the
//      load flag (`loadSales` and friends), i.e. the one place whose failure
//      CAUSES the empty table.
//   3. A page with at least one UNGATED `err` banner has an always-reachable
//      home for any writer, so one chrome is correct for it — the rule does not
//      apply, which is how it stays quiet on the other fifteen pages.
//
// The set of pages in (1) is COMPUTED from the JSX and then PINNED, for the same
// reason `actionFailureTone.test.ts` pins its set: a page entering this shape is
// a deliberate look ("your whole err channel is now load-scoped — is every
// writer a load failure?"), not a silent drift.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import { tsxFiles } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/
const FILES = tsxFiles(SRC);
const SOURCE_OF = new Map(FILES.map((rel) => [rel, readFileSync(`${SRC}${rel}`, "utf8")]));

/**
 * A JSX guard that makes its branch unreachable while the list HAS rows: the
 * empty-table test (`rows.length === 0`) or the load-state flag (`checking` /
 * `loading`).
 *
 * Decided on the AST, not on the source text. A text test is wrong here in both
 * directions, and both were measured on this tree: a `\bloading\b` match fires on a
 * SIBLING `{t("fs.loading")}` inside the same fragment (which silently exempted
 * form-settings from the rule), and a match on the string `!checking` calls a
 * load-only banner reachable — `!checking` renders precisely WHEN the load has
 * finished, so it is the opposite of a gate. See `isLoadGate` and `conjunctsOf`.
 */
function isLoadGate(n: ts.Node): boolean {
  if (ts.isParenthesizedExpression(n)) return isLoadGate(n.expression);
  // `!checking` renders whenever the load is DONE — not a gate.
  if (ts.isPrefixUnaryExpression(n)) return false;
  if (ts.isIdentifier(n)) return n.text === "checking" || n.text === "loading";
  if (ts.isBinaryExpression(n)) {
    const k = n.operatorToken.kind;
    const eq = k === ts.SyntaxKind.EqualsEqualsEqualsToken || k === ts.SyntaxKind.EqualsEqualsToken;
    if (!eq) return false;
    const lengthOf = (x: ts.Node) =>
      ts.isPropertyAccessExpression(x) && ts.isIdentifier(x.name) && x.name.text === "length";
    const zero = (x: ts.Node) => ts.isNumericLiteral(x) && x.text === "0";
    return (lengthOf(n.left) && zero(n.right)) || (zero(n.left) && lengthOf(n.right));
  }
  return false;
}

/**
 * Is this JSX expression, or the LEFT side of its `&&`/`||`/`?:`, the `err`
 * state? `{err}`, `{err && …}`, `{err || …}` and `{err ? … : …}` are all render
 * sites for it; an expression that merely MENTIONS `err` elsewhere (`{!err && …}`,
 * `{err ? t("a") : t("b")}` inside another element) is not.
 */
function isErrSite(e: ts.Node): boolean {
  if (ts.isIdentifier(e)) return e.text === "err";
  if (ts.isBinaryExpression(e)) return isErrSite(e.left);
  if (ts.isParenthesizedExpression(e)) return isErrSite(e.expression);
  if (ts.isConditionalExpression(e)) return isErrSite(e.condition);
  return false;
}

/**
 * The predicate nodes that must ALL hold for the element to render, read
 * STRUCTURALLY off the ancestor chain (see `isLoadGate` for why text will not
 * do). `a && b && c` contributes three; a ternary contributes its condition; an
 * `a || b` contributes `a` (the site's own truthiness — the branch renders when
 * EITHER side does, so only the left operand is a predicate the site needs).
 */
function conjunctsOf(node: ts.Node): ts.Node[] {
  const out: ts.Node[] = [];
  let cur: ts.Node | undefined = node.parent;
  while (cur) {
    if (ts.isConditionalExpression(cur)) out.push(...flattenAnd(cur.condition));
    else if (ts.isBinaryExpression(cur)) {
      const k = cur.operatorToken.kind;
      if (k === ts.SyntaxKind.AmpersandAmpersandToken) out.push(...flattenAnd(cur));
      else if (k === ts.SyntaxKind.BarBarToken) out.push(cur.left);
    } else if (ts.isIfStatement(cur)) out.push(...flattenAnd(cur.expression));
    cur = cur.parent;
  }
  return out;
}

/** `a && b && c` → the three operand nodes. */
function flattenAnd(e: ts.Node): ts.Node[] {
  if (ts.isParenthesizedExpression(e)) return flattenAnd(e.expression);
  if (ts.isBinaryExpression(e) && e.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken)
    return [...flattenAnd(e.left), ...flattenAnd(e.right)];
  return [e];
}

/**
 * Does the nearest enclosing function drive a load state? That is the function
 * whose failure is the one the empty-table banner exists to report: it sets
 * `checking`/`loading` (or clears them) around the fetch whose refusal leaves
 * the list empty.
 */
function inLoadFunction(node: ts.Node, sf: ts.SourceFile): boolean {
  let cur: ts.Node | undefined = node;
  while (cur) {
    if (ts.isFunctionLike(cur)) return /set(?:Checking|Loading)\(/.test(cur.getText(sf));
    cur = cur.parent;
  }
  return false;
}

type Page = {
  /** every render site of `err` on the page */
  sites: number;
  /** render sites of `err` that are NOT inside a load gate */
  ungatedSites: number[];
  /** `setErr(<non-empty>)` writers that are NOT inside the load function */
  strayWriters: { line: number; arg: string }[];
};

/** Walk one source and report the two facts the rule turns on. */
function analyse(rel: string, src: string): Page {
  const sf = ts.createSourceFile("guard.tsx", src, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const ungatedSites: number[] = [];
  const strayWriters: { line: number; arg: string }[] = [];
  let sites = 0;
  const lineAt = (n: ts.Node) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
  const visit = (node: ts.Node): void => {
    if (ts.isJsxExpression(node) && node.expression && isErrSite(node.expression)) {
      sites += 1;
      if (!conjunctsOf(node.expression).some(isLoadGate)) ungatedSites.push(lineAt(node));
    }
    if (ts.isCallExpression(node)) {
      const e = node.expression;
      const name = ts.isIdentifier(e) ? e.text : ts.isPropertyAccessExpression(e) ? e.name.text : "";
      if (name === "setErr" && node.arguments.length) {
        const arg = node.arguments[0].getText().replace(/\s+/g, " ");
        // `setErr("")` clears the text and is not a failure — the load function
        // itself opens with it.
        if (arg !== '""' && !inLoadFunction(node, sf)) strayWriters.push({ line: lineAt(node), arg });
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return { sites, ungatedSites, strayWriters };
}

const ANALYSED: Map<string, Page> = new Map(
  FILES.map((rel) => [rel, analyse(rel, SOURCE_OF.get(rel)!)] as const).filter(([, p]) => p.sites > 0),
);

/** The pages that render `err` ONLY through a load gate — computed, see header. */
const LOAD_GATED_ONLY = [...ANALYSED].filter(([, p]) => p.ungatedSites.length === 0).map(([rel]) => rel).sort();

/**
 * PINNED. Three list pages whose `err` is the LOAD error and nothing else, each
 * with exactly one writer, inside its own loader. A page APPEARING here has just
 * made its entire `err` channel load-scoped: check that every writer really is a
 * load failure and that each action failure has a banner that renders with rows
 * on the table. A page LEAVING has gained an always-reachable `err` banner.
 */
const PINNED_LOAD_GATED_ONLY = ["app/activity/page.tsx", "app/deliveries/page.tsx", "app/sales/page.tsx"];

test("the walk found the app, and the reader reads the shapes it keys on (it cannot pass vacuously)", () => {
  assert.ok(
    FILES.length >= 40,
    `the walk found only ${FILES.length} .tsx files under web/src — it is not reading the app`,
  );
  assert.ok(
    FILES.includes("app/sales/page.tsx") && FILES.includes("app/deliveries/page.tsx"),
    "the walk is not reaching the list pages this rule is about",
  );
  // The reader must classify the four render shapes, or a site can hide from it:
  // `{err}` / `{err && …}` / `{err || …}` are sites, `{!err && …}` is not.
  assert.ok(isErrSite(parseExpr("err")), "a bare `{err}` render site was not recognised");
  assert.ok(isErrSite(parseExpr('err && t("x")')), "a `{err && …}` render site was not recognised");
  assert.ok(isErrSite(parseExpr('err || t("x")')), "a `{err || …}` render site was not recognised");
  assert.ok(!isErrSite(parseExpr('!err && t("x")')), "`{!err && …}` was read as a site for the error text");
  assert.ok(!isErrSite(parseExpr('t("x")')), "a plain expression was read as a site for the error text");
  // ...and the load gate must recognise the two shapes the tree uses, and NOT a
  // plain conditional whose branch merely looks like one. These are the exact
  // readings that made the first two versions of this guard wrong: a SIBLING
  // `t("fs.loading")` (not a gate) and `!checking` (the OPPOSITE of a gate).
  assert.ok(isLoadGate(parseExpr("rows.length === 0")), "the empty-table gate was not recognised");
  assert.ok(isLoadGate(parseExpr("checking")), "the loading flag was not recognised");
  assert.ok(!isLoadGate(parseExpr("confirmOpen")), "a dialog flag was read as a load gate");
  assert.ok(
    !isLoadGate(parseExpr("!checking")),
    "`!checking` was read as a gate — it renders precisely when the load has FINISHED, so a " +
      "load-only banner would be called reachable and the rule would never fire",
  );
  assert.ok(
    !isLoadGate(parseExpr('t("fs.loading")')),
    'a dictionary key that merely contains the word "loading" was read as a gate',
  );
  // The conjuncts of the REAL /sales guard, read structurally: `rows.length === 0`
  // is a gate; the sibling `!checking` is not.
  const sfSales = ts.createSourceFile(
    "x.tsx",
    SOURCE_OF.get("app/sales/page.tsx")!,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const conj: ts.Node[] = [];
  const collect = (n: ts.Node) => {
    if (ts.isJsxExpression(n) && n.expression && isErrSite(n.expression)) conj.push(...conjunctsOf(n.expression));
    ts.forEachChild(n, collect);
  };
  collect(sfSales);
  assert.ok(
    conj.some(isLoadGate),
    `no gate is read on app/sales/page.tsx's \`err\` sites any more (read: ` +
      `${JSON.stringify(conj.map((n) => n.getText()))}) — re-anchor the guard`,
  );
  assert.ok(
    conj.some((n) => n.getText() === "rows.length === 0"),
    `the empty-table guard this card was filed on is no longer a conjunct in app/sales/page.tsx ` +
      `(read: ${JSON.stringify(conj.map((n) => n.getText()))})`,
  );
});

/** Parse a standalone expression, for the reader's own non-vacuity checks. */
function parseExpr(src: string): ts.Node {
  const sf = ts.createSourceFile("e.tsx", `const x = ${src};`, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let found: ts.Node | undefined;
  const visit = (n: ts.Node) => {
    if (ts.isVariableStatement(n)) {
      found = (n.declarationList.declarations[0] as ts.VariableDeclaration).initializer;
    }
    ts.forEachChild(n, visit);
  };
  visit(sf);
  assert.ok(found, `could not parse the probe expression: ${src}`);
  return found!;
}

test("the pages whose `err` renders only inside a load gate are exactly the pinned three", () => {
  assert.deepEqual(
    LOAD_GATED_ONLY,
    PINNED_LOAD_GATED_ONLY,
    `The pages whose error banner renders ONLY while the list is empty/loading changed.\n` +
      `  computed: ${JSON.stringify(LOAD_GATED_ONLY)}\n  pinned:   ${JSON.stringify(PINNED_LOAD_GATED_ONLY)}\n` +
      `A page APPEARING here has moved its whole \`err\` channel behind a table/load guard — every ` +
      `writer must be a load failure, and any action failure needs a banner that renders WITH rows ` +
      `(card t_bfe1cda5). A page LEAVING has gained an always-reachable \`err\` banner: re-anchor ` +
      `this list rather than trusting the old one.`,
  );
});

test("a load-only error banner never receives an action failure (t_bfe1cda5)", () => {
  const offenders: string[] = [];
  for (const rel of PINNED_LOAD_GATED_ONLY) {
    const page: Page | undefined = ANALYSED.get(rel);
    assert.ok(page, `src/${rel} no longer renders an \`err\` state — re-anchor this guard`);
    assert.ok(
      page.ungatedSites.length === 0,
      `src/${rel} now renders \`err\` outside the load gate (line ${page.ungatedSites[0]}) — ` +
        `if that is deliberate, move it off PINNED_LOAD_GATED_ONLY`,
    );
    for (const w of page.strayWriters) {
      offenders.push(
        `src/${rel} — line ${w.line} writes an ACTION failure into \`err\` (\`setErr(${w.arg})\`) while ` +
          `every render site of \`err\` sits inside a load gate (\`rows.length === 0\` / loading). ` +
          `With a row on the table that banner does not render, so this failure is INVISIBLE — measured ` +
          `on /sales: the switch reverted and the sentence was in zero elements. Route it through the ` +
          `page's message banner (\`showMsgOk\`/\`showMsgFail\`) or render \`err\` where the control is.`,
      );
    }
  }
  assert.deepEqual(
    offenders,
    [],
    `An action failure must not be routed to a banner that only renders while the list is empty ` +
      `(card t_bfe1cda5):\n  ${offenders.join("\n  ")}`,
  );
});

test("the guard would fail on the code it replaced (in-file bite proof)", () => {
  // The EXACT pre-fix /sales shape: one `err` state, both render sites inside the
  // empty-table branch, and the sound save writing a failure to it from outside
  // the loader. `analyse` must see a stray writer — otherwise a scan that matched
  // nothing would read as a clean file, which is how this defect shipped.
  const preFix = `
    const [err, setErr] = useState("");
    const [rows, setRows] = useState([]);
    const [checking, setChecking] = useState(true);
    const loadSales = useCallback(async () => {
      setChecking(true);
      setErr("");
      setErr(r.error);
      setChecking(false);
    }, []);
    async function toggleSound(next: boolean) {
      setSoundEnabled(next);
      setErr(t("sales.soundSaveFail"));
    }
    return (
      <table><tbody>
        {!checking && rows.length === 0 && (
          <tr><td>{err || t("sales.empty")}{err && <button>retry</button>}</td></tr>
        )}
      </tbody></table>
    );`;
  const page = analyse("app/probe/page.tsx", preFix);
  assert.equal(
    page.ungatedSites.length,
    0,
    "the pre-fix render sites were read as reachable with rows on the table — the rule cannot see the defect",
  );
  assert.deepEqual(
    page.strayWriters.map((w) => w.line > 0),
    [true],
    "the pre-fix ACTION writer was not read as stray — the guard would pass on the code it replaces",
  );

  // ...and the FIX must be read as clean: the same page with the failure routed
  // through the tone pair has no stray writer.
  const fixed = preFix.replace(
    'setErr(t("sales.soundSaveFail"));',
    'showMsgFail(t("sales.soundSaveFail"));',
  );
  assert.deepEqual(
    analyse("app/probe/page.tsx", fixed).strayWriters,
    [],
    "the fixed writer was still read as a stray ACTION failure — the guard reds on its own remedy",
  );
});
