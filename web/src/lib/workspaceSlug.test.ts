// t_586ff0c3 — the dashboard's half of the reserved-subdomain refusal.
//
// WHY THIS FILE EXISTS
//
// The Go half (api/internal/urlguard/subdomains.go + the two validator tests)
// proves a reserved name cannot be CLAIMED. It cannot prove the merchant is told
// WHY. Two separate defects live on this side, and both are invisible to a Go
// test:
//
//   1. The API's refusal sentence is English, and the dashboard used to map every
//      400 to t("workspace.slugInvalid") — "Slug must be 3-40 chars, a-z, 0-9,
//      dash." A reserved name is perfectly well-formed, so that message would be
//      both untranslated AND wrong, sending the merchant off to retype the same
//      sensitive name. This is #694's class, and #694's rule is that the
//      dashboard renders its own sentence from a stable token.
//   2. A cross-language token: the Go constant and the TS constant are two
//      spellings of one contract. If they drift, the refusal silently falls back
//      to the malformed-slug message and nothing fails.
//
// Run with:
//   npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { DICT } from "../components/i18nDict.ts";
import {
  RESERVED_SUBDOMAIN_CODE,
  RESERVED_SUBDOMAIN_KEY,
  isReservedSlugRefusal,
} from "./workspaceSlug.ts";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// The Go rule this mirrors — read from source so the two cannot drift apart
// silently (a mirror that disagrees is a second, wrong rule).
const goSubdomains = read("../../../api/internal/urlguard/subdomains.go");
const goRestWorkspaces = read("../../../api/internal/httpapi/rest_workspaces.go");
const goMcpWorkspaces = read("../../../api/internal/mcpsrv/workspaces.go");
const switcher = read("../components/WorkspaceSwitcher.tsx");
const picker = read("../app/workspace-picker/page.tsx");
// lib/workspace.ts is where BOTH create surfaces meet (the topbar switcher and
// the first-run ask both call createWorkspace), so it is the ONE place the
// reserved token can be read without one surface drifting from the other.
const workspaceLib = read("./workspace.ts");
const i18nDict = read("../components/i18nDict.ts");
const docsPage = read("../app/docs/customize/page.tsx");
const docsLayout = read("../app/docs/layout.tsx");

test("the guard is looking at real files (it cannot pass vacuously)", () => {
  assert.ok(
    goSubdomains.includes("func ReservedSubdomain("),
    "urlguard/subdomains.go is not the reserved-name rule source — the guard's anchor moved",
  );
  assert.ok(
    switcher.includes("createWorkspace"),
    "WorkspaceSwitcher.tsx is not the workspace-create surface — the guard's anchor moved",
  );
});

test("WEB/GO CONTRACT: RESERVED_SUBDOMAIN_CODE equals the Go constant", () => {
  // A rename on either side would silently drop the dashboard back to the
  // malformed-slug message for a reserved name, which is exactly the #694 class
  // of defect (the reason is on the wire and the UI cannot read it).
  const m = goSubdomains.match(/ReservedSubdomainCode\s*=\s*"([a-z_]+)"/);
  assert.ok(m, "urlguard.ReservedSubdomainCode not found — the Go rule moved or was renamed");
  assert.equal(RESERVED_SUBDOMAIN_CODE, m[1]);
});

test("both Go surfaces actually send that code", () => {
  // The constant is useless if the handler never writes it: the dashboard would
  // see no token and fall back to the generic message.
  assert.match(
    goRestWorkspaces,
    /"code":\s*urlguard\.ReservedSubdomainCode/,
    "the REST refusal no longer carries a `code` — the dashboard would print the " +
      "API's English prose instead of the localised sentence",
  );
  assert.match(
    goRestWorkspaces,
    /urlguard\.ReservedSubdomainMessage\(slug\)/,
    "the REST refusal no longer carries a WHY message for curl/API-key callers",
  );
  // Both surfaces must consult the ONE list (a hand-rolled copy is a second rule).
  for (const [name, src] of [
    ["httpapi/rest_workspaces.go", goRestWorkspaces],
    ["mcpsrv/workspaces.go", goMcpWorkspaces],
  ] as const) {
    assert.ok(
      src.includes("urlguard.ReservedSubdomain("),
      `${name} does not call urlguard.ReservedSubdomain — the rule has drifted into ` +
        "a second (or missing) implementation there",
    );
  }
});

test("isReservedSlugRefusal matches the token, never the prose", () => {
  assert.equal(isReservedSlugRefusal({ code: RESERVED_SUBDOMAIN_CODE }), true);
  // The exact body the REST handler writes.
  assert.equal(
    isReservedSlugRefusal({
      error: '"billing" is reserved for JomForm: …',
      code: RESERVED_SUBDOMAIN_CODE,
    }),
    true,
  );
  // Every other refusal shape must NOT be read as reserved, or the merchant gets
  // the wrong explanation for a taken slug or a malformed one.
  for (const body of [
    null,
    undefined,
    {},
    { error: "slug sudah digunakan" },
    { code: "no_products" },
    { code: "reserved_subdomain_extra" },
    { code: RESERVED_SUBDOMAIN_CODE.toUpperCase() },
    "reserved_subdomain",
    42,
  ]) {
    assert.equal(
      isReservedSlugRefusal(body),
      false,
      `${JSON.stringify(body)} must not be classified as a reserved-name refusal`,
    );
  }
});

test("the create path renders the localised sentence, not the API's prose", () => {
  // The call site, asserted from source: the module's decision has to be used.
  //
  // #718 landed the shared create helper (lib/workspace.createWorkspace) first,
  // and BOTH surfaces — the topbar switcher and the first-run ask — now POST
  // through it. That is where the reserved-name branch belongs, and asserting it
  // there is STRONGER than asserting it in the switcher alone: one assertion now
  // covers both surfaces, which is the property #718's refactor exists to keep.
  assert.match(
    workspaceLib,
    /isReservedSlugRefusal\(body\)/,
    "lib/workspace.createWorkspace never consults isReservedSlugRefusal — a reserved " +
      "name would show 'Slug must be 3-40 chars…', which is both wrong and untranslated",
  );
  assert.match(
    workspaceLib,
    /errorKey: "workspace\.slugReserved"/,
    "the reserved-name branch does not return the dictionary key",
  );
  // The key is spelled once: the string the branch returns is the exported
  // constant, so the two cannot drift into a key the dictionary does not have.
  assert.match(
    workspaceLib,
    new RegExp(`"${RESERVED_SUBDOMAIN_KEY}"`),
    `createWorkspace returns a key other than "${RESERVED_SUBDOMAIN_KEY}"`,
  );
  // The body must be read before the branch (a branch on an unread body would be
  // dead code that still passes the assertions above).
  assert.match(
    workspaceLib,
    /const body = [\s\S]{0,80}?await res\.json\(\)/,
    "the create path does not read the response body, so it cannot see the code",
  );
  // And the API's `error` prose must never be rendered. This is #694's rule for
  // this surface: the only text the merchant sees comes from the dictionary.
  for (const [name, src] of [
    ["lib/workspace.ts", workspaceLib],
    ["WorkspaceSwitcher.tsx", switcher],
  ] as const) {
    assert.doesNotMatch(
      src,
      /body\.error|body\?\.error/,
      `${name} renders the API's error prose — that string is English and is ` +
        "meant for curl/MCP callers (#694)",
    );
  }
  // Both surfaces must render the key through the dictionary, or the reserved
  // sentence would exist and never reach the merchant on one of the two paths.
  for (const [name, src] of [
    ["WorkspaceSwitcher.tsx", switcher],
    ["workspace-picker/page.tsx", picker],
  ] as const) {
    assert.match(
      src,
      /t\(r\.errorKey\)/,
      `${name} does not render the key createWorkspace returned, so a reserved name ` +
        "would show nothing (or a raw key) on that path",
    );
  }
});

// ── The EXECUTED mapping: a reserved 400 is not "slug invalid" ──────────────
//
// The source assertions above prove the branch EXISTS; this proves it is the one
// that runs. Both refusals are 400s, so a status-only mapping answers the wrong
// key for a reserved name — and that is the reported defect, not a style nit.

test("createWorkspace maps a token-carrying 400 to the reserved key, not slugInvalid", async () => {
  const { createWorkspace } = await import("./workspace.ts");
  const originalFetch = globalThis.fetch;
  const stub =
    (status: number, body: unknown) =>
    (async () =>
      ({
        ok: status >= 200 && status < 300,
        status,
        json: async () => body,
      }) as unknown as Response) as unknown as typeof fetch;
  try {
    // The exact body httpapi/rest_workspaces.go writes for a reserved name.
    globalThis.fetch = stub(400, {
      error: '"billing" is reserved for JomForm: …',
      code: RESERVED_SUBDOMAIN_CODE,
    });
    assert.deepEqual(
      await createWorkspace({ name: "Billing", slug: "billing" }),
      { ok: false, errorKey: RESERVED_SUBDOMAIN_KEY },
      "a reserved name is answered with the malformed-slug message — the merchant is " +
        "told their typing was bad and retries the same sensitive name",
    );

    // A plain malformed slug must still get the malformed message: over-matching
    // the token would make the length rule unreachable for every 400.
    globalThis.fetch = stub(400, { error: "slug 3-40 aksara" });
    assert.deepEqual(await createWorkspace({ name: "x", slug: "ab" }), {
      ok: false,
      errorKey: "workspace.slugInvalid",
    });

    // A 400 whose body cannot be read (or is not JSON) must not throw out of the
    // submit handler — it is the malformed-slug answer, as before.
    globalThis.fetch = (async () =>
      ({
        ok: false,
        status: 400,
        json: async () => {
          throw new SyntaxError("Unexpected token");
        },
      }) as unknown as Response) as unknown as typeof fetch;
    assert.deepEqual(await createWorkspace({ name: "x", slug: "ab" }), {
      ok: false,
      errorKey: "workspace.slugInvalid",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("every locale defines the reserved-name sentence", () => {
  const entry = DICT[RESERVED_SUBDOMAIN_KEY as keyof typeof DICT] as
    | Record<string, string>
    | undefined;
  assert.ok(entry, `i18nDict.ts has no entry for "${RESERVED_SUBDOMAIN_KEY}"`);
  for (const lang of ["en", "ms", "zh", "ta", "ar"]) {
    assert.ok(
      entry[lang] && entry[lang].trim() !== "",
      `"${RESERVED_SUBDOMAIN_KEY}" is missing ${lang}`,
    );
  }
  // The sentence must say WHO reserved it, or a merchant reads it as a generic
  // rejection and retries the same name.
  assert.match(entry.en, /reserved/i);
  assert.match(entry.en, /JomForm/);
  // And it must reach the merchant through the renderer, not just exist in the
  // dictionary (the key-as-text failure the i18n guard cannot see).
  assert.match(i18nDict, new RegExp(`"${RESERVED_SUBDOMAIN_KEY}":`));
});

test("the docs page documents the reserved-name rule, and the nav reaches it", () => {
  // AGENTS.md documentation parity: the refusal is a shipped behaviour, so it is
  // documented and the docs nav can reach the section.
  const anchor = "/docs/customize#workspace-address";
  assert.match(
    docsPage,
    /id="workspace-address"/,
    "the customize docs page has no #workspace-address section for the rule",
  );
  assert.match(
    docsPage,
    /reserved/i,
    "the docs page does not explain that some workspace names are reserved",
  );
  assert.ok(
    docsLayout.includes(anchor),
    `the docs nav (${docsLayout}) does not link to ${anchor} — the section would be ` +
      "reachable only by typing the URL",
  );
});

test("the reserved set is documented as JomForm-owned, not inferred from data", () => {
  // The card forbids seeding the list from prod account slugs. Pin the doc
  // comment that says so, so a later edit does not quietly reintroduce it.
  assert.match(
    goSubdomains,
    /account slugs/i,
    "subdomains.go no longer states that the list is NOT seeded from accounts.slug",
  );
  // The apex is not a claimable label, so it must not be an entry (a `jomform.com`
  // key could never match a slug and would read as protection it does not give).
  assert.doesNotMatch(
    goSubdomains,
    /^\s*"jomform\.com":/m,
    'subdomains.go lists the apex "jomform.com" as a reserved label — a slug can ' +
      "never equal it, so the entry is inert",
  );
});
