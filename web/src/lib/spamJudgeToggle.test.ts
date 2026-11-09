// The dashboard switch for the per-workspace AI spam judge.
//
// THE BUG: a merchant had every real registration on a form flagged as spam by
// the background AI judge. Flags are not cosmetic — the hourly sweep runs
// `DELETE FROM submissions WHERE is_spam = true AND created_at < $1`, so an
// unreviewed flag is permanent data loss after 30 days. There was no switch to
// turn the judge off, and no way for a dashboard user to find out that turning
// it off was even possible.
//
// This repo has no JSX/DOM test runner (no vitest/jest — see src/lib/i18n.test.ts
// and src/lib/homeServerContent.test.ts), so like those guards this one is a
// source scan. That is the right granularity for what can break here, because
// every failure mode below is a DISAGREEMENT BETWEEN FILES, which is exactly
// what a source scan can see and a component test would not:
//
//   1. The wire field name. The page sends `spam_judge_enabled` and reads
//      `d.spam_judge_enabled`; the Go struct tags it `json:"spam_judge_enabled"`.
//      The Go value is read from source and compared, so a rename on either side
//      fails here instead of silently saving nothing (the switch would appear to
//      work and change nothing — the worst shape for this feature, because the
//      merchant believes spam detection is off while it keeps flagging entries).
//   2. The OFF direction. The toggle must send `false` when the switch is off.
//      An inverted wire value would turn the judge ON exactly when the merchant
//      asked for it off — and every other assertion here would still pass.
//   3. The READ direction. The response must be interpreted with "absent means
//      ON", because the server treats a missing/unset value as enabled. A
//      `!!d.x` read would render the switch as OFF while the server is still
//      judging every entry.
//   4. The documentation link (AGENTS.md DOCUMENTATION PARITY, step 4): the
//      field must carry an inline "?" link, and the page it points at must
//      exist and must actually document this toggle (including that the Spam
//      page stays empty).
//   5. The i18n keys, in all five locales.
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered switch or that clicking it fires the request. The
// pixels and the click are checked by the API-side integration tests, which
// assert the real behaviour (no judge job enqueued with the toggle off).
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT, LANGS } from "../components/i18nDict.ts";
import { usesDocsProse } from "./docsProseReader.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const settings = read("../app/settings/page.tsx");
// The Go side of the wire contract, read cross-language on purpose (the same
// idiom as salesRows.test.ts / homeServerContent.test.ts): the field name is one
// value living in two languages.
const goWorkspace = read("../../../api/internal/dbstore/workspace.go");
const goRest = read("../../../api/internal/httpapi/rest_workspaces.go");
const goMcp = read("../../../api/internal/mcpsrv/workspaces.go");

const DOCS_PAGE = "../app/docs/spam-detection/page.tsx";
const FIELD = "spam_judge_enabled";

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    settings.includes("saveStorefrontSettings"),
    "app/settings/page.tsx is not the settings page source — the guard's anchor moved",
  );
  assert.ok(
    goWorkspace.includes("type WorkspaceSettings struct"),
    "could not find `type WorkspaceSettings struct` in api/internal/dbstore/workspace.go",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE)),
    `the docs page this guard checks (${DOCS_PAGE}) does not exist`,
  );
});

test("the field name is the same string in the dashboard, the Go struct and both Go writers", () => {
  // The JSON tag the API actually emits/accepts.
  assert.match(
    goWorkspace,
    new RegExp(`json:"${FIELD}"`),
    `api/internal/dbstore/workspace.go no longer tags the field json:"${FIELD}" — ` +
      `the dashboard would save a key the server ignores`,
  );
  // The dashboard must both send and read that exact key, and must not keep a
  // second, drifted spelling anywhere. The assertions are anchored on word
  // boundaries rather than a plain `includes`: BITE-PROVED NECESSARY — renaming
  // the key to `spam_judge_enabled_probe` still satisfied a substring check (the
  // correct name is its prefix), so the guard would have stayed green while the
  // dashboard sent a key the server ignores.
  assert.match(
    settings,
    new RegExp(`\\b${FIELD}:\\s*sfSpamJudgeEnabled\\b`),
    `app/settings/page.tsx does not send { ${FIELD}: sfSpamJudgeEnabled } — the switch would ` +
      `save nothing`,
  );
  assert.match(
    settings,
    new RegExp(`\\bd\\.${FIELD}\\b`),
    `app/settings/page.tsx does not READ d.${FIELD} back from the server, so the switch would ` +
      `always render its initial value`,
  );
  // No drifted spelling may exist anywhere in the file: a second key that
  // merely starts with the real one (or ends with it) is the failure mode above.
  const drifted = settings.match(/spam_judge_enabled\w+/g) ?? [];
  assert.deepEqual(
    drifted.filter((m) => !m.startsWith(`${FIELD}_docs`)),
    [],
    `app/settings/page.tsx mentions a drifted field name (${drifted.join(", ")}) — only the exact ` +
      `\`${FIELD}\` key is read or sent`,
  );
  // The REST writer and the MCP writer each need the field, or one of the two
  // surfaces silently drops the merchant's choice.
  for (const [name, src] of [
    ["api/internal/httpapi/rest_workspaces.go", goRest],
    ["api/internal/mcpsrv/workspaces.go", goMcp],
  ] as const) {
    assert.match(
      src,
      new RegExp(`\\b${FIELD}\\b`),
      `${name} does not mention ${FIELD} — one write surface would silently ignore the toggle`,
    );
  }
});

test("the dashboard reads an absent value as ON (the server's default), never as OFF", () => {
  // The server's contract: the column defaults TRUE and the store falls back to
  // true. A `!!d.x` read renders an OFF switch for a workspace that is actually
  // still judging every entry — a merchant would believe spam detection is off.
  assert.match(
    settings,
    new RegExp(`setSfSpamJudgeEnabled\\(d\\.${FIELD} !== false\\)`),
    `app/settings/page.tsx must read the toggle as \`d.${FIELD} !== false\` (absent means ON, ` +
      `matching the server default) — a \`!!d.${FIELD}\` read shows OFF while the judge is running`,
  );
  // And the local state must start ON, so a workspace whose fetch has not landed
  // does not render "off" for a moment and mislead the merchant.
  assert.match(
    settings,
    /const \[sfSpamJudgeEnabled, setSfSpamJudgeEnabled\] = useState\(true\)/,
    "sfSpamJudgeEnabled must initialise to true (the server default), not false",
  );
});

test("the docs page exists, is linked from the field, and documents the real effects", () => {
  const docs = read(DOCS_PAGE);

  // The inline "?" link next to the field (AGENTS.md documentation-parity step 4).
  assert.ok(
    settings.includes('href="/docs/spam-detection#per-workspace-toggle"'),
    'the toggle has no inline "?" doc link to /docs/spam-detection#per-workspace-toggle ' +
      "(AGENTS.md DOCUMENTATION PARITY step 4)",
  );
  // The anchor the link targets must actually exist on the page AND must reach
  // the DOM as an HTML id. The page renders through src/app/docs/docsProse since
  // the prose rewrite: its section is DATA (`id: "per-workspace-toggle"`) and the
  // shared renderer turns that into `<section id={s.id}>`. So both halves are
  // asserted — the declaration on the page, and the render site in the renderer.
  // Checking only the literal `id="per-workspace-toggle"` would now fail on a
  // page that is actually correct, and checking only the data would pass on a
  // renderer that never emits the id.
  assert.ok(
    docs.includes('id: "per-workspace-toggle"'),
    `the docs link points at #per-workspace-toggle but ${DOCS_PAGE} declares no section with that id`,
  );
  assert.ok(
    usesDocsProse(docs),
    `${DOCS_PAGE} no longer renders through src/app/docs/docsProse — the section id would ` +
      `have no element to reach the DOM through`,
  );
  assert.match(
    read("../app/docs/docsProse.tsx"),
    /<section\b[^>]*\bid=\{s\.id\}/,
    `the docs link points at #per-workspace-toggle but the prose renderer does not put a ` +
      `section's id into the DOM, so the anchor would not scroll`,
  );

  // The page must state the consequences a merchant needs, not just that a
  // switch exists: an un-reviewed flag is deleted, so turning the judge off
  // keeps /spam empty and nothing gets flagged in the first place.
  assert.match(
    docs,
    /never set|not.{0,20}flagged|nothing is flagged/i,
    "the docs page does not say that with the judge off nothing is flagged",
  );
  assert.match(
    docs,
    /30 days|deleted/i,
    "the docs page does not explain the 30-day auto-delete of flagged entries — the reason the " +
      "switch matters",
  );
  assert.ok(
    docs.includes(FIELD),
    `the docs page does not name the ${FIELD} field, so an agent reading the docs cannot find it`,
  );

  // Reachable from the docs nav, or the page is documented but unfindable.
  const nav = read("../app/docs/layout.tsx");
  assert.ok(
    nav.includes('"/docs/spam-detection"'),
    "the docs sidebar does not link /docs/spam-detection — the page would be unreachable",
  );
});

test("the toggle's i18n keys exist in all five locales", () => {
  for (const key of ["set.sfSpamTitle", "set.sfSpamHelp", "set.sfSpamDocs"] as const) {
    const entry = DICT[key] as Record<string, unknown> | undefined;
    assert.ok(entry, `the dictionary has no ${key} — the toggle would render a raw key`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — a user on that locale would silently see English`,
      );
    }
  }
  // Both keys must actually be used by the switch, or they are dead dictionary
  // entries and the UI is hard-coded English.
  for (const key of ["set.sfSpamTitle", "set.sfSpamHelp"] as const) {
    assert.ok(
      settings.includes(`t("${key}")`),
      `app/settings/page.tsx does not render t("${key}")`,
    );
  }
});
