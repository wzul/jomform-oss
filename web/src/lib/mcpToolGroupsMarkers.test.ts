// Regression guard for "`/docs/mcp/tools` prints `mode` as literal backticks".
//
// THE BUG (card t_5b4ed0a9, measured on the served build, fresh browser session):
//   the set_chip_settings row read
//     "… Admin only. `mode` is accepted for compatibility with older clients
//      and IGNORED: no payment behaviour depends on it."
//   — with the two backticks visible to the reader. The text node sits in
//   DIV>DIV>DIV>SECTION>DIV>DIV>TABLE>TBODY>TR>TD, NOT inside a <pre>/<code>, and a
//   sweep of all 40 /docs routes in the same session found it as the only marker
//   left in prose.
//
// WHY THE RENDERER'S OWN GUARDS CANNOT SEE IT, and why the fix is the DATA:
//   web/src/app/docs/mcp/tools/page.tsx prints the row straight into a cell —
//   <td>{t.desc}</td> — and the group prose into <p>{g.note}</p>. These strings are
//   DATA, not page prose: they never go through docsProse's Inline parser, so a
//   marker here cannot become a chip, it can only reach the reader verbatim.
//   leakedMarkers() scores this string 0: `mode` is a balanced pair, parseInline
//   turns it into a `code` token, and a code token is excluded by definition. The
//   string is not "leaked markdown", it is a raw cell — a different mechanism, which
//   is why the renderer-side scan could not catch it.
//
// THE RULE: every TOOL_GROUPS desc and note is PLAIN TEXT, because it is printed as
// plain text. The registry in api/internal/mcpsrv is the same text byte for byte
// (api/internal/mcpsrv/docs_mcp_page_description_test.go compares them over
// tools/list), so a marker removed here must be removed there too — that Go guard
// is what keeps the two surfaces one string.
//
// WHAT THIS CHECKS: no backtick, no `**` bold run, no `[[lang|…]]` span and no
// `[text](href)` link in any desc/note. It scans for the CHARACTERS rather than
// reusing parseInline, because a reader sees an unbalanced marker just as plainly
// as a balanced one, and parseInline keeps an unbalanced marker as text — the one
// shape this guard must also fail on.
import { test } from "node:test";
import assert from "node:assert/strict";

import { TOOL_GROUPS } from "./mcpToolGroups.ts";

/** Inline markers the docs renderer would turn into a chip — printed raw here. */
const MARKERS: { name: string; re: RegExp; sample: string }[] = [
  { name: "backtick", re: /`/, sample: "uses `mode` for compatibility" },
  { name: "bold", re: /\*\*/, sample: "this is **bold** text" },
  { name: "language span", re: /\[\[/, sample: "[[en|English]]" },
  { name: "link", re: /\]\(/, sample: "see [the docs](/docs/mcp)" },
];

type Row = { where: string; text: string };

/** Every string the /docs/mcp/tools page prints without a parser: desc + note. */
function printableStrings(): Row[] {
  const out: Row[] = [];
  for (const g of TOOL_GROUPS) {
    if (typeof g.note === "string") out.push({ where: `note of "${g.group}"`, text: g.note });
    for (const t of g.tools) out.push({ where: `desc of ${t.name}`, text: t.desc });
  }
  return out;
}

test("no TOOL_GROUPS desc or note carries an inline marker printed raw", () => {
  const rows = printableStrings();

  // Vacuity/anchors: the rows really are being read, and the row that carried the
  // bug is one of them. A changed import that yielded nothing cannot pass.
  const names = TOOL_GROUPS.flatMap((g) => g.tools.map((t) => t.name));
  assert.ok(rows.length >= 150, `expected >=150 printable strings, got ${rows.length}`);
  assert.ok(names.includes("set_chip_settings"), "anchor row set_chip_settings is missing");
  assert.ok(names.includes("list_products"), "anchor row list_products is missing");

  const offenders: string[] = [];
  for (const { where, text } of rows) {
    for (const { name, re } of MARKERS) {
      if (re.test(text)) {
        const at = text.search(re);
        offenders.push(`${where} — ${name} at offset ${at}: …${text.slice(Math.max(0, at - 40), at + 40)}…`);
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "these strings are printed raw on /docs/mcp/tools, so a marker reaches the reader verbatim:\n  " +
      offenders.join("\n  "),
  );
});

test("the marker detector actually bites", () => {
  // A guard whose regexes stopped matching would report a clean registry forever.
  for (const { name, re, sample } of MARKERS) {
    assert.ok(re.test(sample), `the ${name} pattern does not match its own sample`);
  }
  assert.ok(
    MARKERS.every(({ re }) => !re.test("Set the credentials. Empty secret = keep the existing key. Admin only.")),
    "a marker pattern fires on plain registry prose",
  );
});
