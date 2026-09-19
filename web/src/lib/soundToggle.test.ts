// The /sales notification-sound switch is a PER-WORKSPACE setting, not a
// per-session one.
//
// THE BUG (as shipped, and as the comment on the line claimed otherwise):
//   const [soundEnabled, setSoundEnabled] = useState(false);   // page.tsx
// The comment above it read "`soundEnabled` is the per-workspace audio-beep
// toggle", but nothing on the page ever read or wrote the stored value: the
// whole documented feature (REST `sound_enabled`, the `set_realtime_sound` MCP
// tool, the Settings → Storefront switch) was unreachable from `/sales`. For the
// surface it was built for — an unattended kitchen/counter screen — the setting
// was useless: enable the beep, reload, and the screen went silent with no
// indication. Either the comment or the behaviour was wrong; the documented
// behaviour (per-workspace) is the intended one, so the behaviour was fixed.
//
// WHAT THIS GUARD PINS (each is a way the fix can silently regress)
//   1. The page READS the stored value and seeds the switch from it. Reverting
//      the load leaves the switch at `false` on every load — the original bug.
//   2. The page WRITES the value back when the switch is used, through the ONE
//      existing settings path (`PUT /v1/workspaces/{id}/settings`, reached from
//      the dashboard as `/api/workspaces/{id}/settings`). A `localStorage`
//      shortcut is the failure this must catch: it would look correct in one
//      browser and be wrong on the second device in the same workspace, which is
//      the whole point of a per-workspace setting.
//   3. The wire field is `sound_enabled`, and it is spelled ONCE — pinned against
//      the Go struct tag, the REST body field and the MCP tool input, read
//      cross-language like spamJudgeToggle.test.ts / salesRows.test.ts do. A
//      rename on either side would silently save nothing.
//   4. A failed write REVERTS the switch, so it never displays a value the
//      server does not hold. This is the "honest failure" half of the card and
//      is the second way the switch can lie.
//   5. The workspace id is resolved from the session's workspace list, because
//      the settings endpoint is keyed by workspace ID while every list fetch on
//      the page is scoped by SLUG. Without the id the write cannot address the
//      workspace at all — a green-looking toggle that saves nothing.
//   6. The Live pill stays STATUS ONLY (it follows the SSE stream, not the
//      switch), and the sound value is not held in localStorage — the shortcut
//      that would pass a single-browser reload check.
//   7. AGENTS.md documentation parity: the field carries an inline "?" link, and
//      the page it points at exists and documents the behaviour (per workspace,
//      survives a reload, reverts on a failed save).
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the rendered switch, the click, or the round trip. This repo has
// no JSX/DOM test runner (see src/lib/i18n.test.ts); the executed proof of the
// load/write/revert behaviour lives in `src/lib/workspaceSound.test.ts`, which
// runs the page's own module-level helpers, and the real round trip is exercised
// against the API by the Go-side settings tests plus a live browser check.
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

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const sales = read("../app/sales/page.tsx");
const workspaceLib = read("./workspace.ts");
// The Go side of the wire contract, read cross-language on purpose: the field
// name is one value living in two languages.
const goWorkspace = read("../../../api/internal/dbstore/workspace.go");
const goRest = read("../../../api/internal/httpapi/rest_workspaces.go");
const goMcp = read("../../../api/internal/mcpsrv/workspaces.go");
const goRestRoutes = read("../../../api/internal/httpapi/rest.go");

const DOCS_PAGE = "../app/docs/page.tsx";
const FIELD = "sound_enabled";
const SETTINGS_PATH = "/api/workspaces/${soundWorkspaceId}/settings";

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    sales.includes("function SalesPageInner"),
    "app/sales/page.tsx is not the sales page source — the guard's anchor moved",
  );
  assert.ok(
    goWorkspace.includes("type WorkspaceSettings struct"),
    "could not find `type WorkspaceSettings struct` in api/internal/dbstore/workspace.go",
  );
  assert.ok(
    workspaceLib.includes("export function activeWorkspaceId"),
    "lib/workspace.ts no longer resolves the active workspace id",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE)),
    `the docs page this guard checks (${DOCS_PAGE}) does not exist`,
  );
});

test("the page READS the stored value on mount and seeds the switch from it", () => {
  // The read, through the one settings endpoint, for the resolved workspace.
  assert.ok(
    sales.includes(`fetch(\`${SETTINGS_PATH}\`)`),
    "app/sales/page.tsx does not GET the workspace settings — the switch would " +
      "always start at its default instead of the stored value (the original bug)",
  );
  // The value it seeds the switch with must come from that payload through the
  // shared reader, not a `!!` read of an arbitrary shape.
  assert.match(
    sales,
    /setSoundEnabled\(soundEnabledFromSettings\(d\)\)/,
    "app/sales/page.tsx does not seed soundEnabled from the loaded settings " +
      "(soundEnabledFromSettings) — the stored value would be ignored",
  );
  // And the switch must not be usable before that read has settled: a switch
  // that renders the default for a workspace whose stored value is TRUE is
  // showing the merchant the opposite of the stored setting.
  assert.match(
    sales,
    /disabled=\{!soundLoaded\}/,
    "the sound switch is not gated on the settings read finishing (soundLoaded) — " +
      "it would render its default before the stored value is known",
  );
});

test("the page WRITES the value through the existing settings path, not localStorage", () => {
  // The write: a PUT to the same settings endpoint, with the single shared body.
  assert.ok(
    sales.includes(`fetch(\`${SETTINGS_PATH}\`, {`),
    "app/sales/page.tsx does not PUT the workspace settings on a toggle change — " +
      "the switch would flip and save nothing",
  );
  assert.match(
    sales,
    /method: "PUT"/,
    "the sound write is not a PUT — saveWorkspaceSettings is a PUT on the settings path",
  );
  assert.match(
    sales,
    /body: JSON\.stringify\(soundToggleBody\(next\)\)/,
    "app/sales/page.tsx does not send soundToggleBody(next) — the wire body must be " +
      "the one shared single-field payload, not page state",
  );
  // The value written must be the NEW one the switch shows, and the switch must
  // be flipped optimistically before the request (the card's "flip immediately").
  const toggle = sales.slice(sales.indexOf("async function toggleSound"));
  assert.ok(toggle.length > 0, "app/sales/page.tsx no longer defines toggleSound");
  assert.ok(
    toggle.indexOf("setSoundEnabled(next)") < toggle.indexOf(`fetch(\`${SETTINGS_PATH}\`, {`),
    "toggleSound does not set the switch optimistically before the write",
  );
  // THE localStorage SHORTCUT: a per-browser value would pass a single-browser
  // reload check and be wrong on the kitchen tablet. The page must STORE nothing
  // (comments may mention the word; only an actual storage call counts).
  assert.ok(
    !/localStorage\s*\.\s*(get|set|remove)Item/.test(sales),
    "app/sales/page.tsx stores the sound setting in localStorage — that is a " +
      "per-browser value, and the documented setting is per WORKSPACE",
  );
  // And no second endpoint may be invented for it (the card's explicit limit).
  assert.ok(
    !/events\/sound|realtime\/sound|sound_enabled\//.test(sales),
    "app/sales/page.tsx invents a second sound endpoint instead of using the " +
      `${FIELD} field on the workspace settings path`,
  );
  // REACHABILITY. A source scan for the fetch text alone is not proof that the
  // page ever CALLS it: an early `return`, or the call moved behind a dead
  // branch, leaves the string in the file and the switch saving nothing — the
  // original bug with a misleading diff. So the write must be on the straight
  // path between the optimistic flip and the request, and it must be awaited and
  // have its status checked.
  const body = sales.slice(sales.indexOf("async function toggleSound"));
  const seqAt = body.indexOf("++soundWriteSeq.current");
  const fetchAt = body.indexOf("`/api/workspaces/${soundWorkspaceId}/settings`, {");
  assert.ok(
    seqAt >= 0 && fetchAt > seqAt,
    "the sound write is not present after the write-sequencing line inside toggleSound",
  );
  assert.ok(
    !/\breturn\b/.test(body.slice(seqAt, fetchAt)),
    "the settings write is unreachable: a `return` sits between the optimistic " +
      "flip and the request, so the switch would flip and save nothing",
  );
  assert.ok(
    body.includes("const r = await fetch("),
    "the sound write is not awaited/have its result captured",
  );
});

test("the write is addressed to the ACTIVE workspace (id, not the slug)", () => {
  // The settings route is /v1/workspaces/{id}/settings: a slug there would 404
  // (or worse, address a different workspace). The id comes from the session's
  // workspace list, resolved by the shared helper.
  assert.match(
    sales,
    /const soundWorkspaceId = activeWorkspaceId\(workspaces\)/,
    "app/sales/page.tsx does not resolve the active workspace ID for the sound " +
      "settings calls — the settings route is keyed by workspace id",
  );
  assert.match(
    sales,
    /const \{ anon, role, workspace, user, workspaces \} = useSession\(\)/,
    "app/sales/page.tsx no longer reads the workspace list from the session — " +
      "the id cannot be resolved",
  );
  // A missing id must not leave the switch lying about a value it never saved.
  assert.match(
    sales,
    /if \(!soundWorkspaceId\) \{[\s\S]{0,200}?setSoundEnabled\(previous\)/,
    "toggleSound does not revert the switch when there is no workspace to write to",
  );
});

test("a FAILED write reverts the switch and surfaces the failure", () => {
  const toggle = sales.slice(sales.indexOf("async function toggleSound"));
  assert.ok(
    toggle.includes("setSoundEnabled(previous)"),
    "toggleSound does not restore the previous value when the write fails — the " +
      "switch would keep showing a value the server does not hold",
  );
  // The reverted state must be reported, not silent.
  assert.match(
    toggle,
    /setErr\(t\("sales\.soundSaveFail"\)\)|setErr\(message\)/,
    "toggleSound reverts silently — a failed save must be visible to the merchant",
  );
  // Both failure shapes: a rejected HTTP response and a network failure.
  assert.match(toggle, /if \(r\.ok\) return;/, "toggleSound ignores a non-ok response");
  assert.match(toggle, /\} catch \{/, "toggleSound has no network-failure path");
  // A slow failed write must not clobber a newer click's state.
  assert.match(
    toggle,
    /soundWriteSeq\.current/,
    "toggleSound has no write sequencing — a stale failure could revert a newer click",
  );
});

test("the Live pill stays status only and does not follow the switch", () => {
  // The card's scope limit: the pill reflects the SSE connection. Binding it to
  // the sound setting (or to any persisted value) is a different control.
  assert.match(
    sales,
    /const \[live, setLive\] = useState\(false\)/,
    "`live` is no longer connection-scoped state — the Live pill must stay status only",
  );
  assert.ok(
    sales.includes("es.onopen = () => setLive(true)") && sales.includes("es.onerror = () => setLive(false)"),
    "the Live pill no longer follows the SSE stream",
  );
  assert.ok(
    !/setLive\((?!true\)|false\))/.test(sales),
    "the Live pill is driven by something other than the stream connection",
  );
});

test("a failed READ is reported, not shown as the default value", () => {
  // The read must distinguish "the stored value is off" from "the value could
  // not be read". Collapsing the two shows OFF for a workspace whose beep is ON
  // — the same lie as the original bug, just at load time instead of on save.
  const load = sales.slice(
    sales.indexOf("read the workspace's STORED sound setting"),
    sales.indexOf("async function toggleSound"),
  );
  assert.ok(load.length > 0, "could not locate the sound read effect in app/sales/page.tsx");
  assert.match(
    load,
    /if \(!d\) \{[\s\S]{0,400}?setSoundReadFailed\(true\)/,
    "a settings read that produced no payload is treated as 'off' — the switch " +
      "would silently render a value the workspace may not hold",
  );
  assert.match(
    load,
    /\.catch\(\(\) => \{[\s\S]{0,400}?setSoundReadFailed\(true\)/,
    "a failed/offline settings read is silent — the switch would render the default",
  );
  // And the failure must be VISIBLE, from the five-locale dictionary.
  assert.match(
    sales,
    /\{soundReadFailed && \([\s\S]{0,200}?t\("sales\.soundReadFail"\)/,
    "the failed read is not surfaced in the UI",
  );
});

test("the field name is the same string in the dashboard, the Go struct and both Go writers", () => {
  // The JSON tag the API emits/accepts.
  assert.match(
    goWorkspace,
    new RegExp(`json:"${FIELD}"`),
    `api/internal/dbstore/workspace.go no longer tags the field json:"${FIELD}" — ` +
      `the dashboard would save a key the server ignores`,
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
  // And the REST route the dashboard's PUT resolves to must still be the
  // settings route (the proxy maps /api/* to /v1/*).
  assert.match(
    goRestRoutes,
    /PUT \/v1\/workspaces\/\{id\}\/settings/,
    "the workspace settings PUT route is gone — the dashboard's write would 404",
  );
  // The shared helper writes exactly that one field. Two writers with one field
  // each is how a drifted second spelling would appear.
  assert.match(
    workspaceLib,
    new RegExp(`return \\{ ${FIELD}: next \\}`),
    `lib/workspace.ts soundToggleBody no longer returns exactly { ${FIELD}: next } — ` +
      `a body carrying other settings could clobber them`,
  );
  const drifted = sales.match(/sound_enabled\w+/g) ?? [];
  assert.deepEqual(
    drifted.filter((m) => !m.startsWith(`${FIELD}_docs`)),
    [],
    `app/sales/page.tsx mentions a drifted field name (${drifted.join(", ")}) — only the ` +
      `exact \`${FIELD}\` key is read or sent`,
  );
});

test("the docs page exists, is linked from the switch, and documents the real behaviour", () => {
  const docs = read(DOCS_PAGE);

  // The inline "?" link next to the switch (AGENTS.md documentation-parity step 4).
  assert.ok(
    sales.includes('href="/docs#realtime-notifications"'),
    'the sound switch has no inline "?" doc link to /docs#realtime-notifications ' +
      "(AGENTS.md DOCUMENTATION PARITY step 4)",
  );
  // The anchor must exist AND must reach the DOM as an id: this page declares its
  // sections as data and renders `id={f.slug}`, so both halves are asserted.
  assert.ok(
    docs.includes('slug: "realtime-notifications"'),
    `the docs link points at #realtime-notifications but ${DOCS_PAGE} declares no section with that slug`,
  );
  assert.ok(
    docs.includes("id={f.slug}"),
    `the docs link points at #realtime-notifications but ${DOCS_PAGE} does not render the ` +
      `section ids into the DOM, so the anchor would not scroll`,
  );
  // The consequences a merchant needs: it is per workspace (not per browser) and
  // it survives a reload — the two facts the bug contradicted.
  assert.match(
    docs,
    /per-workspace setting[\s\S]{0,400}?reload the page/i,
    "the docs page does not say the sound setting is per workspace and survives a reload",
  );
  assert.match(
    docs,
    /goes back to its previous position|reverts? /i,
    "the docs page does not say a rejected save reverts the switch",
  );
  assert.ok(
    docs.includes(FIELD),
    `the docs page does not name the ${FIELD} field, so an agent reading the docs cannot find it`,
  );
});

test("the switch's i18n keys exist in all five locales", () => {
  for (const key of [
    "sales.soundLabel",
    "sales.soundLoading",
    "sales.soundSaveFail",
    "sales.soundReadFail",
    "sales.soundDocs",
  ] as const) {
    const entry = DICT[key] as Record<string, unknown> | undefined;
    assert.ok(entry, `the dictionary has no ${key} — the switch would render a raw key`);
    for (const lang of LANGS) {
      const v = entry[lang];
      assert.ok(
        typeof v === "string" && v.trim() !== "",
        `${key}.${lang} is missing — a user on that locale would silently see English`,
      );
    }
  }
  // Both new keys must actually be used by the page, or they are dead dictionary
  // entries and the UI is hard-coded English.
  for (const key of [
    "sales.soundLoading",
    "sales.soundSaveFail",
    "sales.soundReadFail",
    "sales.soundDocs",
  ] as const) {
    assert.ok(
      sales.includes(`t("${key}")`),
      `app/sales/page.tsx does not render t("${key}")`,
    );
  }
});
