// Card t_7fb80479 — the field-type list is ONE list. This guard reads every
// copy of it in the tree and fails when they disagree.
//
// WHY THIS EXISTS. The `text` block had two meanings in production because the
// copies of this list drifted: the palette offered it in the Standard group
// with placeholder / max length / default value / pattern, both public
// renderers painted `<input type="text">` (t_99c3a08b, t_23289ce3), the builder
// canvas drew a control — and yet the API's ValidTypes, its collectableTypes,
// the two docs lists and this app's own `fieldTypes` all still excluded it. So
// an agent could not register the palette's only short-answer field, a required
// text answer was never validated server-side, and the builder could not offer
// the block a required marker at all. Five hand-maintained copies, no guard.
//
// WHAT IT PINS (all as SETS — order is presentation):
//   * web/src/app/builder/page.tsx's `fieldTypes`     (the builder's panel gates)
//   * api/internal/formfields/formfields.go ValidTypes (what an agent may register)
//   * api/internal/formfields/formfields.go collectableTypes (the derived contract)
//   * the /docs/customize field list                  (what a merchant is told)
//   * the register_form_fields description, in BOTH
//     api/internal/mcpsrv/tools.go and /docs/mcp      (what an agent is told)
//
// A type added on one side and forgotten on the other reds this. Deleting a
// line from any of the five is the bite: try removing `text` from `fieldTypes`
// or from ValidTypes and this file names the copy that lost it.
//
// The prose blocks (`section`, `html`) must stay OUT of every copy: they are
// display blocks, not field types, and a prose block has no answer to collect.
//
// Run with the repo's Node test runner:  npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "..", "..", "..");
const read = (rel: string) => readFileSync(join(ROOT, rel), "utf8");

const BUILDER_PAGE = "web/src/app/builder/page.tsx";
const FORMFIELDS_GO = "api/internal/formfields/formfields.go";
const DOCS_CUSTOMIZE = "web/src/app/docs/customize/page.tsx";
const DOCS_MCP = "web/src/app/docs/mcp/page.tsx";
const TOOLS_GO = "api/internal/mcpsrv/tools.go";

/** The types of a `string[]` literal, from `const name... = [ ... ];`. */
function arrayLiteral(src: string, name: string, where: string): Set<string> {
  const m = src.match(new RegExp(`const ${name}[^=]*=\\s*\\[([\\s\\S]*?)\\]`));
  assert.ok(m, `${name} not found in ${where} — the list moved; update this guard`);
  return new Set([...m[1].matchAll(/"([a-z-]+)"/g)].map((x) => x[1]));
}

/** The keys of a `map[string]bool` literal set to true. */
function goStringSet(src: string, name: string): Set<string> {
  const m = src.match(new RegExp(`var ${name} = map\\[string\\]bool\\{([\\s\\S]*?)\\n\\}`));
  assert.ok(m, `${name} not found in ${FORMFIELDS_GO} — the list moved; update this guard`);
  return new Set([...m[1].matchAll(/"([a-z-]+)":\s*true/g)].map((x) => x[1]));
}

/** The `(a|b|c)` type list spelled in the register_form_fields description. */
function registerFormFieldsList(src: string, where: string): Set<string> {
  const m = src.match(/type must be a JomForm input type \(([^)]*)\)/);
  assert.ok(m, `the register_form_fields type list not found in ${where} — reworded? update this guard`);
  return new Set(m[1].split("|").map((s) => s.trim()).filter(Boolean));
}

/** The `(a, b, c)` type list spelled in /docs/customize's prose. */
function customizeList(src: string, where: string): Set<string> {
  const m = src.match(/Add fields \(([^)]*)\)/);
  assert.ok(m, `the /docs/customize field list not found in ${where} — reworded? update this guard`);
  return new Set(m[1].split(/[,\s]+/).map((s) => s.trim()).filter(Boolean));
}

const fieldTypes = arrayLiteral(read(BUILDER_PAGE), "fieldTypes", BUILDER_PAGE);
const formfieldsGo = read(FORMFIELDS_GO);
const validTypes = goStringSet(formfieldsGo, "ValidTypes");
const collectableTypes = goStringSet(formfieldsGo, "collectableTypes");
const customize = customizeList(read(DOCS_CUSTOMIZE), DOCS_CUSTOMIZE);
const docsMcp = registerFormFieldsList(read(DOCS_MCP), DOCS_MCP);
const toolsGo = registerFormFieldsList(read(TOOLS_GO), TOOLS_GO);

const COPIES: Record<string, Set<string>> = {
  "web/src/app/builder/page.tsx fieldTypes": fieldTypes,
  "formfields.go ValidTypes": validTypes,
  "formfields.go collectableTypes": collectableTypes,
  "the /docs/customize field list": customize,
  "the register_form_fields description (/docs/mcp)": docsMcp,
  "the register_form_fields description (api/internal/mcpsrv/tools.go)": toolsGo,
};

test("every copy of the field-type list carries the same set", () => {
  const canon = [...fieldTypes].sort();
  assert.ok(canon.length >= 15, `parsed only ${canon.length} field types from ${BUILDER_PAGE} — the extraction is broken`);
  for (const [where, set] of Object.entries(COPIES)) {
    const got = [...set].sort();
    const missing = canon.filter((t) => !set.has(t));
    const extra = got.filter((t) => !fieldTypes.has(t));
    assert.deepEqual(
      got,
      canon,
      `${where} disagrees with the other copies.\n  missing there: ${missing.join(", ") || "-"}\n  only there:    ${extra.join(", ") || "-"}`,
    );
  }
});

test("`text` is a field type in every copy — and the prose blocks are in none", () => {
  for (const [where, set] of Object.entries(COPIES)) {
    assert.ok(
      set.has("text"),
      `${where} lost \`text\`: the palette offers it as a short-answer field and both renderers paint <input type="text">, so every list that names the input types must carry it`,
    );
    for (const prose of ["section", "html", "divider", "image"]) {
      assert.ok(
        !set.has(prose),
        `${where} lists ${prose} as a field type — a display block has no answer to collect`,
      );
    }
  }
});
