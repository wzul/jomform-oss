// Language placement — the dashboard language is PER-PROFILE, so it must be
// correct in a SECOND BROWSER, and the two language settings must stay separate.
//
// THE CARD'S HEADLINE CRITERION, AND WHY NOTHING CAUGHT IT
//   "/profile has a working language picker and the choice survives a reload AND
//    is correct in a different browser — that is what 'per-profile' means, and it
//    is the assertion that catches a localStorage shortcut."
//
//   The feature shipped with that criterion covered only on the SERVER side:
//   lang_placement_integration_test.go proves users.lang round-trips and the REST
//   route persists it. Nothing pinned the CLIENT half — that the dashboard
//   actually reads the choice back from the PROFILE and writes it there. So
//   deleting the two `fetch` calls in components/i18n.tsx (leaving the
//   localStorage mirror, which keeps working in one browser) left every guard in
//   the repo GREEN while the feature silently degraded to the per-browser
//   setting it was built to replace. That is the exact regression this file
//   exists to make impossible.
//
// WHAT IS PINNED HERE
//   1. The choice is WRITTEN to the profile (POST /api/auth/profile/lang) — not
//      only mirrored to localStorage.
//   2. The choice is READ BACK from the profile (GET /api/auth/profile → lang)
//      and wins over the browser value. This is the "different browser" half: a
//      localStorage-only implementation cannot satisfy it.
//   3. The localStorage key is KEPT as the migration source and first paint
//      (the card forbids removing it before existing values migrate), and it is
//      promoted UP to the profile so nobody silently resets to English.
//   4. There is exactly ONE source of truth: the /profile picker writes through
//      the shared i18n context and keeps no page-local copy, so the picker and
//      the rest of the dashboard cannot disagree.
//   5. The dashboard language and the buyer-facing form language are NOT
//      collapsed into one field (the card's explicit scope limit): the public
//      renderers still resolve form → workspace → en and never read users.lang.
//   6. The workspace default is read-modify-written on save, so an unrelated
//      "save my footer" cannot silently reset a merchant's buyer language.
//   7. The wire names agree CROSS-LANGUAGE, read from the Go source on purpose:
//      the route, the JSON field and the MCP tool input are one contract written
//      in two languages, and a rename on either side saves nothing.
//   8. Documentation parity: the inline "?" link on the field points at a docs
//      page and anchor that exist and are reachable from the docs nav.
//
//   9. A pick made while the profile read is in flight WINS. The read is issued
//      at mount and answered from the row as it was when it was SERVED, so
//      applying it after an explicit choice silently reverted that choice in the
//      picker, the document and `localStorage` — while the server kept the new
//      language (card t_02338e21). The client half is pinned here; the EXECUTED
//      proof, which holds the read open in a real browser, is
//      web/e2e/profile-lang-picker-race.spec.ts.
//
// WHAT IT DOES NOT COVER (stated honestly)
//   This repo has no JSX/DOM test runner (see src/lib/i18n.test.ts), so this file
//   asserts the client's wiring from source and the server's contract from the Go
//   source — it does not execute React, click the picker, or run a second
//   browser. The EXECUTED proof of the per-profile behaviour is the Go-side
//   round trip (lang_placement_integration_test.go, DB-backed) plus the REST
//   route test; this file is the guard that keeps the client wired to it.
//
// Run with:
//
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { LANGS } from "../components/i18nDict.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const i18n = read("../components/i18n.tsx");
const shell = read("../components/Shell.tsx");
const profilePage = read("../app/profile/page.tsx");
const settingsPage = read("../app/settings/page.tsx");

// The Go half of the same contract, read cross-language on purpose.
const goAuth = read("../../../api/internal/httpapi/auth.go");
const goProfileRoutes = read("../../../api/internal/httpapi/public.go");
const goFormLang = read("../../../api/internal/httpapi/formLang.go");
const goRenderBlocks = read("../../../api/internal/httpapi/render_blocks.go");
const goRestWorkspaces = read("../../../api/internal/httpapi/rest_workspaces.go");
const goUserStore = read("../../../api/internal/dbstore/user.go");
const goTools = read("../../../api/internal/mcpsrv/tools.go");
// THE REGISTRY IS THE PACKAGE, NOT ONE FILE (card t_b893be19).
//
// The MCP tool literals do not all live in tools.go: #732 moved `disable_2fa`'s
// `&mcp.Tool{}` into parity_issue396.go, and `mcpToolInventory.test.ts` widened its own
// reader to the package for exactly that reason. Asserting against tools.go alone made
// this check FALSE-RED the moment a registration moved to a sibling file, and FALSE-GREEN
// when a registration was removed from tools.go while a `//` comment still spelled the
// name there. Reading the package answers the question the message actually asks.
const MCP_DIR = fileURLToPath(new URL("../../../api/internal/mcpsrv/", import.meta.url));
const goMcpsrv = readdirSync(MCP_DIR)
  .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
  .sort()
  .map((f) => readFileSync(MCP_DIR + f, "utf8"))
  .join("\n");
const goMcpWorkspaces = read("../../../api/internal/mcpsrv/workspaces.go");
const goParityTest = read("../../../api/internal/mcpsrv/mcp_parity_test.go");
const goParityDoc = read("../../../MCP-PARITY.md");
const goLangMigration = read("../../../api/internal/db/migrations/000066_user_lang.up.sql");

const DOCS_PAGE = "../app/docs/customize/page.tsx";
const DOCS_LAYOUT = "../app/docs/layout.tsx";

// The one path the dashboard language travels on, spelled once.
const PROFILE_LANG_PATH = "/api/auth/profile/lang";
const PROFILE_PATH = "/api/auth/profile";
const STORAGE_KEY = "jf_lang";

// baselineBlock slices the `mcpParityBaselineTools` declaration out of the Go
// parity guard, so an assertion can be scoped to that list rather than the whole
// file (the file also mentions the tool in its comments and other lists).
function baselineBlock(src: string): string {
  const start = src.indexOf("var mcpParityBaselineTools = []string{");
  assert.ok(start !== -1, "mcpParityBaselineTools is gone from the parity guard");
  const end = src.indexOf("\n}", start);
  assert.ok(end !== -1, "could not find the end of mcpParityBaselineTools");
  return src.slice(start, end);
}

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    i18n.includes("export function I18nProvider"),
    "components/i18n.tsx is not the i18n provider source — the guard's anchor moved",
  );
  assert.ok(
    goAuth.includes("func (a *AuthHandlers) RegisterRoutes"),
    "could not find RegisterRoutes in api/internal/httpapi/auth.go",
  );
  assert.ok(
    goUserStore.includes("func (a *UserStore) UpdateLang") ||
      goUserStore.includes("func (s *UserStore) UpdateLang"),
    "could not find UserStore.UpdateLang in api/internal/dbstore/user.go",
  );
  assert.ok(
    existsSync(path(DOCS_PAGE)),
    `the docs page this guard checks (${DOCS_PAGE}) does not exist`,
  );
});

test("the choice is WRITTEN to the profile, not only mirrored to this browser", () => {
  // The write: a POST to the profile-language path carrying the new code.
  assert.match(
    i18n,
    new RegExp(`fetch\\("${PROFILE_LANG_PATH}",\\s*\\{`),
    "components/i18n.tsx never POSTs to the profile — the choice would live only in " +
      "this browser, which is the per-browser behaviour this feature replaced",
  );
  assert.match(
    i18n,
    /method: "POST"/,
    "the language write is not a POST (the route is POST /v1/auth/profile/lang)",
  );
  assert.match(
    i18n,
    /body:\s*JSON\.stringify\(\{\s*lang:\s*l\s*\}\)/,
    "the language write does not send `{ lang: <code> }` — the server would store nothing",
  );
  // The setter must reach that write, not just the localStorage mirror.
  assert.match(
    i18n,
    /const setLang = useCallback\([\s\S]{0,600}?persistLang\(l\)/,
    "setLang() no longer persists to the profile — flipping the picker would feel " +
      "correct in this browser and be lost in the next one",
  );
});

test("the choice is READ BACK from the profile, so a second browser gets it", () => {
  // The read: the profile endpoint, and the value must come from `d.lang`.
  assert.match(
    i18n,
    new RegExp(`fetch\\("${PROFILE_PATH}"\\)`),
    "components/i18n.tsx never reads the profile — a signed-in user on another " +
      "browser would start on their browser's value instead of their own setting",
  );
  assert.match(
    i18n,
    /isLang\(d\.lang\)[\s\S]{0,160}?setLangState\(d\.lang\)/,
    "the profile's `lang` does not set the UI language — the read is decorative",
  );
  // The profile must WIN. Ordering is the whole assertion: the browser value is
  // applied first (instant paint) and the profile read overrides it afterwards.
  const browserFirst = i18n.indexOf(`localStorage.getItem(STORAGE_KEY)`);
  const profileRead = i18n.indexOf(`fetch("${PROFILE_PATH}")`);
  assert.ok(
    browserFirst !== -1 && profileRead !== -1 && browserFirst < profileRead,
    "the profile read no longer overrides the browser value — the per-browser " +
      "value would win and the setting would not follow the user",
  );
});

test("localStorage stays as the migration source, and is promoted up to the profile", () => {
  // The card forbids dropping the key before existing values are migrated.
  assert.match(
    i18n,
    new RegExp(`localStorage\\.getItem\\(STORAGE_KEY\\)`),
    `the legacy ${STORAGE_KEY} value is no longer read — every existing user who ` +
      "never set a profile language would silently reset to English",
  );
  assert.match(
    i18n,
    /isLang\(local\)[\s\S]{0,80}?persistLang\(local\)/,
    "the browser value is no longer promoted to the profile on first run — the " +
      "migration path for existing users is gone",
  );
  assert.ok(
    i18n.includes(`const STORAGE_KEY = "${STORAGE_KEY}"`),
    `the legacy storage key is no longer named ${STORAGE_KEY}`,
  );
});

test("there is one source of truth: the picker writes through the shared context", () => {
  // The picker must go through the context setter (which persists), and must not
  // keep a page-local language copy that could drift from it.
  assert.match(
    profilePage,
    /setLang\(e\.target\.value as Lang\)/,
    "the /profile picker does not call the shared i18n setLang — it would not persist",
  );
  assert.doesNotMatch(
    profilePage,
    /useState<Lang>\(/,
    "the /profile page keeps its own language state — the picker and the dashboard " +
      "chrome could then disagree (two sources of truth)",
  );
  assert.doesNotMatch(
    profilePage,
    new RegExp(`fetch\\("${PROFILE_LANG_PATH}"`),
    "the /profile page writes the language itself instead of through the shared " +
      "context — a second write path that the rest of the app does not observe",
  );
  // And the chrome must no longer carry a language control at all (it moved).
  assert.doesNotMatch(
    shell,
    /<LangToggle \/>|<LangSelect \/>/,
    "a language control is back in the app chrome; the dashboard language is " +
      "per-profile and is set on /profile",
  );
});

test("the dashboard language and the buyer-facing form language are NOT collapsed", () => {
  // The buyer chain is form.config.lang ?? workspace default ?? en. It must never
  // consult the signed-in user's profile language: a buyer has no profile here,
  // and merging the two is the scope error the card calls out explicitly. The
  // per-profile accessors are the precise tell — the buyer path must not reach
  // for them at all.
  for (const [name, src] of [
    ["public.go", goProfileRoutes],
    ["formLang.go", goFormLang],
    ["render_blocks.go", goRenderBlocks],
  ] as const) {
    assert.doesNotMatch(
      src,
      /ValidProfileLang|UpdateLang|users\.lang/,
      `${name} reaches for the PER-PROFILE language on the BUYER path — the ` +
        "dashboard language and the form language must stay separate settings",
    );
  }
  // The buyer resolution still starts from the workspace default.
  assert.match(
    goFormLang,
    /func \(h \*PublicHandlers\) resolveWorkspaceLang\(/,
    "the workspace-language resolver is gone from formLang.go",
  );
  assert.match(
    goFormLang,
    /WorkspaceLang\(\)/,
    "the buyer renderer no longer inherits the workspace default at all",
  );
  // And no buyer page may hardcode a language again — that literal WAS the bug.
  assert.doesNotMatch(
    goProfileRoutes,
    /<html lang="(ms|en)"/,
    'a public.go renderer hardcodes <html lang="ms|en"> again — a workspace set ' +
      "to another language would silently serve the wrong chrome",
  );
});

test("an unrelated settings save cannot reset the workspace's buyer language", () => {
  // SaveSettings writes default_lang unconditionally, so the REST saver must seed
  // it from the CURRENT row and only override it when a code was supplied. Seeding
  // from the request would store "" and WorkspaceLang() coerces that to "en".
  assert.match(
    goRestWorkspaces,
    /DefaultLang:\s*cur\.DefaultLang/,
    "rest_workspaces.go no longer preserves the current default_lang — an unrelated " +
      "save (footer, currency) would silently reset the merchant's form language",
  );
  // The UI must send a real code, and must seed its select from the stored value.
  assert.match(
    settingsPage,
    /default_lang:\s*sfDefaultLang/,
    "Settings no longer sends default_lang — the workspace language would never save",
  );
  assert.match(
    settingsPage,
    /setSfDefaultLang\(d\.default_lang \|\| "en"\)/,
    "Settings no longer seeds the picker from the stored default_lang — it would " +
      "show English for a workspace set to another language",
  );
});

test("the wire names agree across the Go and TypeScript sides", () => {
  // The route: one string, registered on the Go side.
  assert.ok(
    goAuth.includes(`"POST /v1/auth/profile/lang"`),
    "POST /v1/auth/profile/lang is not registered — the client write would 404",
  );
  // The JSON field the profile read returns and the client reads.
  assert.match(
    goAuth,
    /Lang\s+string\s+`json:"lang/,
    'the profile response no longer carries `json:"lang"` — the client would read undefined',
  );
  // The column, its CHECK, and the column-aligned select (the silent-401 class).
  assert.match(
    goLangMigration,
    /ADD COLUMN IF NOT EXISTS lang TEXT/,
    "migration 000066 no longer adds users.lang",
  );
  assert.match(
    goUserStore,
    /COALESCE\(lang,''\)/,
    "UserStore's SELECT lost the COALESCE(lang,'') column — a column-aligned scan " +
      "drift that fails as a wrong value or a scan error on a signed-in request",
  );
  // The five codes must be the same five on both sides.
  for (const code of LANGS) {
    assert.ok(
      goUserStore.includes(`"${code}"`),
      `the Go validator (ValidProfileLang) no longer accepts "${code}", which the ` +
        "dashboard offers — the picker would save a rejected code",
    );
  }
  // MCP parity: the tool is registered AND named in the checklist, and it must NOT
  // also sit in the "not in the checklist" baseline (the double-listing drift).
  // The message names the CLAIM the check can actually make: a REGISTRATION is a
  // `&mcp.Tool{... Name: "…"}` literal, so the name must appear as a registration in the
  // package — not merely as a string somewhere in one file.
  assert.match(
    goMcpsrv,
    /Name:\s*"update_profile_language"/,
    "the update_profile_language MCP tool is no longer REGISTERED — no `&mcp.Tool{}` in " +
      "api/internal/mcpsrv names it (a bare string is not a registration: the old check " +
      "passed on a `//` comment that spelled the name in tools.go)",
  );
  assert.match(
    goParityDoc,
    /`update_profile_language`/,
    "MCP-PARITY.md no longer names update_profile_language — registered tools must " +
      "be in the checklist (the parity guard parses that file)",
  );
  assert.doesNotMatch(
    baselineBlock(goParityTest),
    /"update_profile_language"/,
    "update_profile_language is back in the baseline of tools absent from the " +
      "checklist while the checklist names it — the parity guard's hygiene test " +
      "fails on exactly that pair",
  );
  assert.match(
    goMcpWorkspaces,
    /func \(s \*Server\) updateProfileLanguage\(/,
    "the MCP handler for update_profile_language is gone",
  );
});

test("the field's inline \"?\" link points at a docs page and anchor that exist", () => {
  const anchor = "/docs/customize#language";
  assert.ok(
    profilePage.includes(`href="${anchor}"`),
    `the /profile language field lost its inline "?" doc link (${anchor}) — ` +
      "AGENTS.md documentation parity requires one next to the field",
  );
  const docs = read(DOCS_PAGE);
  assert.match(
    docs,
    /id="language"/,
    `the docs page ${DOCS_PAGE} no longer has the #language anchor the link targets`,
  );
  const layout = read(DOCS_LAYOUT);
  assert.ok(
    layout.includes(anchor),
    `the docs nav (${DOCS_LAYOUT}) no longer links to ${anchor} — the page would be ` +
      "reachable only by typing the URL",
  );
});

test("a choice made while the profile read is in flight is not reverted by it", () => {
  // Card t_02338e21. `fetch("/api/auth/profile")` is issued at mount, and its
  // answer is the row as it was when the GET was SERVED — i.e. it predates any
  // POST issued afterwards. Applying it after an explicit choice therefore
  // reverted that choice in the picker, the document and the mirror while the
  // server kept the new language: silent, because only a reload showed the
  // truth. Measured on prod v1.22.27: 4/4 picks inside the window.
  const profileRead = i18n.indexOf(`fetch("${PROFILE_PATH}")`);
  assert.ok(profileRead >= 0, `components/i18n.tsx no longer reads ${PROFILE_PATH}`);

  // The marker, and the fact that it is a REF: state used as an effect
  // dependency would re-issue the request (one stale read replacing another),
  // and `useState` would re-render on every pick for no reason.
  assert.match(
    i18n,
    /const chosenRef = useRef\(false\)/,
    "the 'the user has chosen' marker is gone (or is not a ref) — the mount read " +
      "can overwrite an explicit choice again",
  );

  // It is set by the USER path, and set BEFORE the state write, so a response
  // landing between the two lines cannot win.
  const setter = i18n.indexOf("const setLang = useCallback(");
  assert.ok(setter >= 0, "setLang is gone from components/i18n.tsx");
  const marked = i18n.indexOf("chosenRef.current = true;", setter);
  const stateWrite = i18n.indexOf("setLangState(l);", setter);
  assert.ok(
    marked > setter && stateWrite > setter && marked < stateWrite,
    "setLang() no longer marks the choice before writing the state — a profile " +
      "answer arriving mid-pick could still overwrite it",
  );

  // The mount effect DISCARDS its answer once that marker is set — before BOTH
  // writers, so the revert cannot survive in `localStorage` either (that mirror
  // is what makes it look permanent to the user).
  const guard = i18n.indexOf("if (chosenRef.current) return;", profileRead);
  const applied = i18n.indexOf("setLangState(d.lang)", profileRead);
  const mirrored = i18n.indexOf("localStorage.setItem(STORAGE_KEY, d.lang)", profileRead);
  assert.ok(guard > profileRead, "the mount read applies its answer with no guard against a newer choice");
  assert.ok(
    guard < applied && guard < mirrored,
    "the guard sits AFTER a writer — the late profile answer still reverts the UI",
  );
});
