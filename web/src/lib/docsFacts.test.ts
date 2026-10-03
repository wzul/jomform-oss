// The type surface of src/lib/docsFacts.ts.
//
// WHY A TEST FILE AND NOT JUST THE MODULE (card t_5cfeb788)
//
// `next build` type-checks the files the app imports. `docsFacts.ts` is imported by
// `docsIndex.test.ts` and by `web/scripts/docsFacts.mjs` — neither is part of the app
// graph — so without this file its types would only be checked by whatever happens to
// reach it. This is the same reason the repo already keeps `docsIndex.test.ts`'s
// neighbours explicit: a guard whose own surface is unchecked can rot silently.
//
// It also pins the two properties the build-time reader depends on and that a
// plausible refactor could quietly break: that the tables are non-empty (an emptied
// table would make every fact comparison vacuous) and that the floor and the list
// agree (the floor is what notices a list being shortened to suit itself).

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  DESTINATION_FACTS,
  PER_ENTRY_FLOOR,
  parseFactTargets,
  checkDestinationFacts,
} from "./docsFacts.ts";

test("the fact tables are loaded and non-empty", () => {
  const entries = Object.keys(DESTINATION_FACTS);
  assert.ok(
    entries.length >= 25,
    `DESTINATION_FACTS holds ${entries.length} entries — it must cover every /docs entry that ` +
      `has a destination guide, and an emptied table would make every comparison vacuous`,
  );
  for (const [title, facts] of Object.entries(DESTINATION_FACTS)) {
    assert.ok(facts.length > 0, `${JSON.stringify(title)} has an empty fact list`);
  }
});

test("every floored entry is in the fact table, and the floor matches the list", () => {
  for (const [title, floor] of Object.entries(PER_ENTRY_FLOOR)) {
    const facts = DESTINATION_FACTS[title];
    assert.ok(facts, `PER_ENTRY_FLOOR names ${JSON.stringify(title)}, which has no fact list`);
    assert.equal(
      facts.length,
      floor,
      `${JSON.stringify(title)}: the list holds ${facts.length} facts and the floor says ${floor}. ` +
        `A fact dropped from the list is a guard getting weaker in the same commit; if the copy ` +
        `genuinely moved, lower the floor in the same commit and say why.`,
    );
  }
});

test("parseFactTargets reads the GROUPS table it is pointed at", () => {
  const src = [
    "const GROUPS: G[] = [",
    "  {",
    '    title: "Sales",',
    '    body: "…",',
    '    href: "/docs/sales#sales",',
    "  },",
    "  {",
    '    title: "Products",',
    '    body: "…",',
    "  },",
    "];",
  ].join("\n");
  const targets = parseFactTargets(src);
  assert.deepEqual(targets, [
    { title: "Sales", href: "/docs/sales#sales" },
    { title: "Products" },
  ]);
});

test("checkDestinationFacts reports a fact the reader text does not carry", () => {
  const targets = [{ title: "Templates", href: "/docs/customize#templates" }];
  const missing = checkDestinationFacts({
    targets,
    hrefToKey: () => "customize",
    readerTextOf: () => "nothing a reader would need",
  });
  assert.ok(
    missing.length === DESTINATION_FACTS.Templates.length,
    `expected every Templates fact to be reported missing, got ${missing.length}`,
  );
  assert.ok(
    missing.every((m) => m.startsWith("Templates → /docs/customize#templates:")),
    `the report must name the entry and the href it went to, so a red is attributable: ${missing[0]}`,
  );
});

test("checkDestinationFacts passes when the reader text carries them", () => {
  const facts = DESTINATION_FACTS.Templates;
  const missing = checkDestinationFacts({
    targets: [{ title: "Templates", href: "/docs/customize#templates" }],
    hrefToKey: () => "customize",
    readerTextOf: () => facts.join(" and "),
  });
  assert.deepEqual(missing, [], "a page carrying every fact must not be reported");
});

test("checkDestinationFacts distinguishes 'no page' from 'no text'", () => {
  const targets = [{ title: "Templates", href: "/docs/customize#templates" }];
  const noPage = checkDestinationFacts({
    targets,
    hrefToKey: () => undefined,
    readerTextOf: () => "",
  });
  assert.deepEqual(noPage, ["Templates → /docs/customize#templates (no page)"]);
  const noText = checkDestinationFacts({
    targets,
    hrefToKey: () => "customize",
    readerTextOf: () => undefined,
  });
  assert.deepEqual(noText, ["Templates → /docs/customize#templates (no text to read)"]);
});
