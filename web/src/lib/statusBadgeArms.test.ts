// Repo-wide guard: a legal SALE STATUS must never paint an UNCOLOURED pill.
//
// THE DEFECT (card t_df0857c6, measured live on prod v1.21.30 — see that card's
// QA report, not this comment, for the run)
//
//   The dashboard's Recent-sales badge helper fell back to the EMPTY STRING:
//
//     web/src/app/dashboard-client.tsx   const badge = (s) => ({ … })[s] || "";
//
//   so any status its map did not name rendered a pill carrying the wrapper's
//   classes only (`whitespace-nowrap rounded-full border px-2.5 py-0.5
//   text-[11px] font-medium `) — no background, no text colour. Measured with a
//   real browser against the real prod stylesheet, on the status-badge cell of
//   the dashboard table:
//
//     refunded   -> computed color rgb(23, 24, 28) on #ffffff, no colour token
//     authorized -> same
//     paid       -> bg-blue-50 text-blue-700 (#eff6ff)     [named map arm]
//     cancelled  -> bg-slate-100 + text-[var(--jf-muted-on-tint)]
//
//   `refunded` and `authorized` are BOTH legal sales statuses — the DB's own
//   `sales_status_check` (api/internal/db/migrations/000002_skip_capture.up.sql)
//   accepts seven of them — and both reach that table. `/sales` paints the same
//   `refunded` sale orange (web/src/app/sales/page.tsx), and its own fallback
//   takes the on-tint token, so the dashboard was the ONE surface where a real
//   status painted a colourless pill, and it disagreed with `/sales` about what
//   `refunded` looks like.
//
// WHY THIS IS A SOURCE GUARD AND NOT ONLY A BROWSER SPEC
//
//   `web/e2e/a11y-dashboard-status-badge.spec.ts` MEASURES the painted pill for
//   `refunded` / `authorized` on a built page, which is the claim that a person
//   can see. A browser spec cannot be exhaustive, though: it renders the statuses
//   a fixture names. This file answers the question for EVERY status the database
//   accepts, now and after a migration adds one, because the status set is READ
//   FROM THE MIGRATIONS rather than typed here.
//
//   The other guards are structurally blind to this defect, and that is not a
//   coincidence: `mutedText.test.ts` and `tintedSurface.test.ts` judge a class
//   list that RENDERS, and an empty class list renders no token for either rule to
//   judge — a pill with no colour at all is not a contrast violation, it is the
//   absence of one. So the guard here is about COVERAGE: a status with no arm.
//
// WHAT IT READS, AND WHY IT PARSES THE HELPER ITSELF
//
//   The shared reader in `./jsxSource.ts` resolves a local helper call to the SET
//   of class lists that helper can return (`resolveClassExpr`), which is what the
//   contrast guards need — but a set has lost the KEY: it cannot say what
//   `authorized` renders. This guard needs the map, so it reads the two helpers
//   with the TypeScript parser directly: the object literal's property names and
//   their string values, plus the `|| …` arm after the lookup. The shape it
//   accepts is exactly the one every status badge in this app is written in
//   (`({ … })[s] || "fallback"`); anything else fails the non-vacuity assertion
//   instead of being skipped, because a helper this file cannot read is not one it
//   has cleared.
//
// THE ASSERTION (both surfaces, every status)
//
//   For each status in the DB's `sales_status_check`, the arm that status renders
//   must declare a text COLOUR. A missing key falls through to the fallback arm,
//   which the same assertion covers — so this file fails on the pre-fix shape for
//   two independent reasons (`authorized`/`refunded` have no key, and the fallback
//   is ""), and on a map entry that is itself colourless.
//
//   `text-[11px]` is a FONT SIZE, not a colour, and the regex below knows the
//   difference — which is the sharp edge of this defect: a class list can look
//   "styled" and paint nothing.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { codeOnly } from "./jsxSource.ts";

const SRC = fileURLToPath(new URL("../", import.meta.url)); // web/src/
const MIGRATIONS = fileURLToPath(new URL("../../../api/internal/db/migrations/", import.meta.url));

// ─── the DB's own status set ────────────────────────────────────────────────

/**
 * The statuses `sales_status_check` accepts, read from the LATEST migration that
 * declares one.
 *
 * Ascending filename order, last match wins: migration 000001 creates the table
 * with a five-status CHECK and 000002 replaces the constraint with the seven-status
 * one that is in force (`ALTER TABLE sales … ADD CONSTRAINT sales_status_check`).
 * Reading it instead of pinning it is what makes a new status land here without
 * anyone remembering to update this file — and a status added to the DB but not to
 * the badge maps is exactly the defect this guard is for.
 */
function salesStatusCheck(): { statuses: string[]; file: string } {
  const files = readdirSync(MIGRATIONS)
    .filter((f) => f.endsWith(".up.sql"))
    .sort();
  let out: { statuses: string[]; file: string } = { statuses: [], file: "" };
  for (const f of files) {
    const sql = readFileSync(`${MIGRATIONS}${f}`, "utf8");
    const re = /ALTER TABLE sales[\s\S]{0,400}?CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)\s*\)/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(sql))) {
      const statuses = [...m[1].matchAll(/'([^']+)'/g)].map((q) => q[1]);
      if (statuses.length > 0) out = { statuses, file: f };
    }
  }
  return out;
}

// ─── does this class list declare a text COLOUR? ────────────────────────────

/**
 * A class that sets the text COLOUR — never a font size.
 *
 * `text-[var(--jf-muted-on-tint)]` and `text-amber-700` are colours;
 * `text-[11px]`, `text-xs`, `text-sm` and `text-center` are not, and treating one
 * of them as a colour is how a pill that paints nothing would read as styled.
 * (The same distinction `mutedText.test.ts` draws for its own token table.)
 */
const COLOUR_CLS_RE =
  /^(?:[a-z-]+:)*text-(?:\[(?![^\]]*(?:px|rem|em|%|vh|vw))[^\]]+\]|white|black|current|transparent|inherit|[a-z]+-(?:50|100|200|300|400|500|600|700|800|900|950))$/;

function declaresTextColour(classes: string): boolean {
  return classes
    .split(/\s+/)
    .filter((c) => c !== "")
    .some((c) => COLOUR_CLS_RE.test(c));
}

// ─── reading a badge helper's arms out of its own source ────────────────────

type BadgeShape = { arms: Map<string, string>; fallback: string };

/** `node` without its `(…)` wrappers, as far as a class map needs. */
function unwrap(node: ts.Expression): ts.Expression {
  let n = node;
  while (ts.isParenthesizedExpression(n)) n = n.expression;
  return n;
}

/**
 * The object literal a class-map helper looks up: `({ … })[s]` (or a bare `{ … }`).
 *
 * Returns null when the initializer is not that shape, which is what makes a
 * helper this file cannot read a FAILURE in `armProblems` rather than a skip.
 */
function mapLiteral(node: ts.Expression): ts.ObjectLiteralExpression | null {
  const n = unwrap(node);
  if (ts.isObjectLiteralExpression(n)) return n;
  if (ts.isElementAccessExpression(n)) {
    const obj = unwrap(n.expression);
    if (ts.isObjectLiteralExpression(obj)) return obj;
  }
  return null;
}

/**
 * The status→class arms of a local helper named `name`, plus its `|| …` fallback.
 *
 * The shape: `const badge = (s: string) => ({ pending: "…", … })[s] || "…";` — an
 * arrow function whose body is the lookup and the fallback joined by `||`.
 * `code` must already have been through `codeOnly`, so a commented-out arm cannot
 * satisfy the guard.
 */
function badgeShape(code: string, name: string): BadgeShape | null {
  const sf = ts.createSourceFile("guard.tsx", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  let out: BadgeShape | null = null;
  const visit = (node: ts.Node): void => {
    if (out) return;
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.name.text === name &&
      node.initializer
    ) {
      let body: ts.Expression = unwrap(node.initializer);
      if (ts.isArrowFunction(body) && body.body && !ts.isBlock(body.body)) body = unwrap(body.body);
      const alt =
        ts.isBinaryExpression(body) && body.operatorToken.kind === ts.SyntaxKind.BarBarToken ? body : null;
      const obj = mapLiteral(alt ? alt.left : body);
      const fb = alt && ts.isStringLiteral(unwrap(alt.right)) ? (unwrap(alt.right) as ts.StringLiteral).text : null;
      if (obj && fb !== null) {
        const arms = new Map<string, string>();
        for (const p of obj.properties) {
          if (!ts.isPropertyAssignment(p)) continue;
          const key = ts.isIdentifier(p.name)
            ? p.name.text
            : ts.isStringLiteral(p.name)
              ? p.name.text
              : null;
          const value = unwrap(p.initializer);
          if (key !== null && ts.isStringLiteral(value)) arms.set(key, value.text);
        }
        if (arms.size > 0) out = { arms, fallback: fb };
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

// ─── the two surfaces that paint a sale status ──────────────────────────────

const SURFACES: { rel: string; helper: string; what: string }[] = [
  {
    rel: "app/dashboard-client.tsx",
    helper: "badge",
    what: "the dashboard's Recent-sales table (desktop rows AND the stacked mobile cards)",
  },
  {
    rel: "app/sales/page.tsx",
    helper: "saleBadge",
    what: "the /sales table and its detail drawer",
  },
];

/**
 * Everything wrong with ONE surface's arms, for the statuses the DB accepts.
 *
 * Factored out of the test so the fixture arm below runs the SAME code the real
 * tree does: a bite proof that calls a different predicate than the guard would
 * prove nothing about the guard.
 */
function armProblems(rel: string, code: string, helper: string, statuses: string[]): string[] {
  const shape = badgeShape(code, helper);
  if (shape === null) {
    return [
      `${rel} — the \`${helper}\` helper was not read at all: it is not the ` +
        `\`({ … })[s] || "fallback"\` shape this guard reads, so this file has NOT cleared its ` +
        `status arms. Re-anchor the reader rather than deleting the assertion.`,
    ];
  }
  const out: string[] = [];
  for (const [key, cls] of shape.arms) {
    if (!declaresTextColour(cls)) {
      out.push(`${rel} — the \`${key}\` arm declares no text COLOUR: ${JSON.stringify(cls)}`);
    }
  }
  if (!declaresTextColour(shape.fallback)) {
    out.push(
      `${rel} — the FALLBACK arm declares no text COLOUR: ${JSON.stringify(shape.fallback)}. ` +
        `Every status the map does not name renders through it, so the pill paints the wrapper's ` +
        `classes only (measured on prod: rgb(23, 24, 28) on #ffffff)`,
    );
  }
  for (const s of statuses) {
    const cls = shape.arms.get(s) ?? shape.fallback;
    if (!declaresTextColour(cls)) {
      out.push(
        `${rel} — a \`${s}\` sale paints a pill with no text colour (arm ${JSON.stringify(cls)}); ` +
          `\`sales_status_check\` accepts \`${s}\`, so this is a real status a merchant can see`,
      );
    }
  }
  return out;
}

test("every legal sale status renders a COLOURED pill on every status surface", () => {
  const check = salesStatusCheck();
  assert.ok(
    existsSync(MIGRATIONS) && check.statuses.length >= 5,
    `the sales status set could not be read from api/internal/db/migrations ` +
      `(found ${check.statuses.length} statuses${check.file ? ` in ${check.file}` : ""}) — the DB is ` +
      `the input this guard exists to be exhaustive about, so an unreadable list is a failure, never a skip`,
  );

  const problems: string[] = [];
  for (const { rel, helper, what } of SURFACES) {
    assert.ok(existsSync(`${SRC}${rel}`), `${rel} does not exist — the surface moved`);
    const code = codeOnly(readFileSync(`${SRC}${rel}`, "utf8"));
    const shape = badgeShape(code, helper);
    // NON-VACUITY, per surface: a helper that resolves to a handful of arms is the
    // shape under test; one arm (or none) means the reader is looking at something
    // else and every assertion below would be about a map that is not there.
    assert.ok(
      shape !== null && shape.arms.size >= 5,
      `${rel}'s \`${helper}\` resolved to ${shape ? shape.arms.size : "no"} arms — ${what} has lost ` +
        `its status map, so this guard is not judging the surface it names`,
    );
    problems.push(...armProblems(rel, code, helper, check.statuses));
  }

  assert.deepEqual(
    problems,
    [],
    "A sale status must render a pill with a background and a text colour, on EVERY surface that " +
      "paints one (card t_df0857c6: the dashboard's `|| \"\"` fallback rendered rgb(23,24,28) on " +
      "#ffffff for `refunded` and `authorized`). Statuses the DB accepts (" +
      check.file +
      "): " +
      check.statuses.join(", ") +
      ".\n  " +
      problems.join("\n  "),
  );
});

test("the two status maps agree where they overlap", () => {
  // `/sales`'s own comment says it carries "the same status badge colors as the
  // dashboard Recent sales table", and the defect this guard exists for was
  // visible as a DISAGREEMENT: the same `refunded` sale was orange on /sales and
  // colourless on the dashboard. A status both surfaces name must therefore
  // render the same arm; a status only one names is a decision, not a defect, so
  // it is not flagged here — `saleBadge`'s extra arms are deliberate.
  const problems: string[] = [];
  const shapes = new Map<string, BadgeShape>();
  for (const { rel, helper } of SURFACES) {
    const shape = badgeShape(codeOnly(readFileSync(`${SRC}${rel}`, "utf8")), helper);
    assert.ok(shape !== null, `${rel}'s \`${helper}\` could not be read`);
    shapes.set(rel, shape);
  }
  const [a, b] = SURFACES;
  const sa = shapes.get(a.rel)!;
  const sb = shapes.get(b.rel)!;

  for (const [key, cls] of sa.arms) {
    const other = sb.arms.get(key);
    if (other !== undefined && other !== cls) {
      problems.push(
        `\`${key}\` is ${JSON.stringify(cls)} on ${a.rel} and ${JSON.stringify(other)} on ${b.rel} — ` +
          `one sale must paint the same pill on both surfaces`,
      );
    }
  }
  // The status this card was filed for, named explicitly: a map that stops
  // covering it is the original defect, and the loop above cannot see a key that
  // vanished from one side.
  for (const rel of [a.rel, b.rel]) {
    const shape = shapes.get(rel)!;
    assert.ok(
      shape.arms.has("refunded"),
      `${rel} no longer names \`refunded\` — a refunded sale reaches this table ` +
        `(\`sales_status_check\` accepts it) and is exactly the status that painted an uncoloured pill`,
    );
  }

  assert.deepEqual(problems, [], problems.join("\n  "));
});

test("armProblems BITES: the pre-fix helper is reported, the fixed one is not", () => {
  // The in-file bite proof. Without it, a guard whose reader silently returns no
  // arms — or whose colour predicate matches font sizes — would be GREEN on the
  // tree that rendered the defect, and nothing here would say so.
  //
  // The fixture is the dashboard helper's PRE-FIX text (card t_df0857c6's diff),
  // reduced to the arms that matter: no `refunded` key and an empty fallback.
  const PRE_FIX = [
    "const badge = (s: string) =>",
    "  ({",
    '    pending: "bg-amber-50 text-amber-700 border-amber-200",',
    '    paid: "bg-blue-50 text-blue-700 border-blue-200",',
    '    cancelled: "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200",',
    '  })[s] || "";',
    "",
  ].join("\n");
  const pre = armProblems("fixture.tsx", codeOnly(PRE_FIX), "badge", ["paid", "authorized", "refunded"]);
  for (const status of ["authorized", "refunded"]) {
    assert.ok(
      pre.some((p) => p.includes(`\`${status}\``)),
      `the pre-fix fixture's \`${status}\` arm was NOT reported, so this guard cannot see the defect ` +
        `it exists for: ${JSON.stringify(pre)}`,
    );
  }
  assert.ok(
    pre.some((p) => p.includes("FALLBACK arm declares no text COLOUR")),
    `the pre-fix fixture's empty fallback was NOT reported: ${JSON.stringify(pre)}`,
  );

  // The same helper with the fix applied must be clean — otherwise "everything is
  // reported" would be the way this test passes.
  const FIXED = PRE_FIX.replace(
    '  })[s] || "";',
    '    refunded: "bg-orange-50 text-orange-700 border-orange-200",\n' +
      '  })[s] || "bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200";',
  );
  assert.deepEqual(
    armProblems("fixture.tsx", codeOnly(FIXED), "badge", ["paid", "authorized", "refunded"]),
    [],
    "the FIXED fixture was reported — the predicate is a false positive and the real assertion above " +
      "would be red on a correct tree",
  );

  // The sharp edge, asserted on its own: a font-size utility is not a colour, and
  // the wrapper the dashboard renders is exactly what made an uncoloured pill look
  // styled.
  const WRAPPER = "whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[11px] font-medium ";
  assert.equal(
    declaresTextColour(WRAPPER),
    false,
    "`text-[11px]` was read as a text colour — a pill that paints nothing would pass this guard",
  );
  assert.equal(declaresTextColour("bg-orange-50 text-orange-700 border-orange-200"), true);
  assert.equal(declaresTextColour("bg-slate-100 text-[var(--jf-muted-on-tint)] border-slate-200"), true);
});
