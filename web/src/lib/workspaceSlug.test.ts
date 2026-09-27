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
import { apiErrorKey } from "./apiErr.ts";
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
// The ONE rule both transports delegate to — where the reserved leg now lives.
const goDbstoreSlugRule = read("../../../api/internal/dbstore/workspace_slug.go");
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

test("both Go surfaces reach that code through the ONE rule", () => {
  // The constant is useless if the handler never writes it: the dashboard would
  // see no token and fall back to the generic message.
  assert.match(
    goRestWorkspaces,
    /"code":\s*urlguard\.ReservedSubdomainCode/,
    "the REST refusal no longer carries a `code` — the dashboard would print the " +
      "API's English prose instead of the localised sentence",
  );
  // The WHY message for curl/API-key callers now comes from the RULE rather than
  // from a direct urlguard call in the handler: the reserved leg moved inside
  // dbstore.ValidateWorkspaceSlug (see "both Go surfaces reach that code through
  // the ONE rule" above), so the handler forwards what the rule returned. The
  // property this assertions guards is unchanged — a machine-readable refusal for
  // non-browser callers, not a bare 400 with no reason.
  assert.match(
    goRestWorkspaces,
    /"error":\s*verr\.Error\(\)/,
    "the REST reserved-name refusal no longer carries a WHY message for curl/API-key callers",
  );
  // WHERE THE LIST IS CONSULTED, and why this moved.
  //
  // t_586ff0c3 enforced the reserved list in EACH transport, and this assertion
  // used to pin that: a `urlguard.ReservedSubdomain(` call in rest_workspaces.go
  // and in mcpsrv/workspaces.go. That shape cannot survive the rename path PR 691
  // adds. The rename path validates through dbstore.ValidateWorkspaceSlug, so a
  // list consulted only in the two CREATE handlers would let an owner rename a
  // workspace TO a reserved label — the list is not reachable from that call site
  // at all, and duplicating it there is the second-implementation defect the
  // repo's urlguard rule exists to prevent.
  //
  // So the leg now lives INSIDE the ONE rule, which is what makes it apply to
  // every caller (create on both transports, rename on both, and the legacy
  // provisioning path). This assertion checks the stronger property: the rule
  // consults the list, and neither transport hand-rolls it.
  assert.match(
    goDbstoreSlugRule,
    /urlguard\.ReservedSubdomain\(/,
    "dbstore.ValidateWorkspaceSlug does not consult urlguard.ReservedSubdomain — a reserved " +
      "label would be accepted by every write path that delegates to it, including rename",
  );
  assert.match(
    goDbstoreSlugRule,
    /urlguard\.ReservedSubdomainMessage\(/,
    "the rule refuses a reserved label without the shared wording",
  );
  for (const [name, src] of [
    ["httpapi/rest_workspaces.go", goRestWorkspaces],
    ["mcpsrv/workspaces.go", goMcpWorkspaces],
  ] as const) {
    assert.ok(
      src.includes("ValidateWorkspaceSlug("),
      `${name} no longer delegates to the shared slug rule — either a slug can be written without ` +
        "being validated, or the rule has drifted back into a second copy there",
    );
    assert.ok(
      !src.includes("urlguard.ReservedSubdomain("),
      `${name} consults the reserved list directly again — the list must be reached through ` +
        "ValidateWorkspaceSlug so there is exactly one place to keep the two in step",
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

test("the rename card renders that same sentence, from the same token", () => {
  // t_b7f81f5e. The reserved refusal reaches the dashboard through a SECOND
  // surface: the Settings → Workspace rename card (WorkspaceTab.tsx doRename),
  // which routes failures through translateApiErr (lib/apiErr.ts). Before this
  // the token had NO entry there, so a rename onto a reserved label showed the
  // card's generic "couldn't rename" sentence — or, before the server fix, a 500
  // the card could say nothing about at all.
  //
  // Asserted as BEHAVIOUR, not as a table row: the mapping has to actually
  // resolve for the token, and to the same sentence the create path uses.
  assert.equal(
    apiErrorKey({ code: RESERVED_SUBDOMAIN_CODE }),
    RESERVED_SUBDOMAIN_KEY,
    `translateApiErr cannot resolve "${RESERVED_SUBDOMAIN_CODE}" — the rename card falls back to its ` +
      "generic sentence for a refusal it could explain exactly",
  );
  // A code this build does not know must still fall back — the mapping must not
  // have become a wildcard.
  assert.equal(apiErrorKey({ code: "reserved_subdomain_extra" }), "");
  assert.equal(apiErrorKey({ code: RESERVED_SUBDOMAIN_CODE.toUpperCase() }), "");
  // And the card really is wired to it: the failure branch must route through
  // translateApiErr rather than printing the server's prose (#6177).
  const tab = read("../components/WorkspaceTab.tsx");
  assert.match(
    tab,
    /setRenameErr\(translateApiErr\(d, t\)/,
    "WorkspaceTab.doRename no longer routes a refused rename through translateApiErr — the reserved " +
      "refusal would be reported as the card's generic sentence, or as the API's English prose",
  );
  assert.doesNotMatch(
    tab,
    /setRenameErr\(d\?\.error|setRenameErr\(d\.error/,
    "WorkspaceTab.doRename renders the API's error prose again (issue #6177)",
  );
});

test("both Go transports carry the token for a RENAME, not only for a create", () => {
  // The dashboard half above is worthless if the server never sends the token on
  // the rename path — which is exactly what the qa card measured: rename answered
  // 500 {"code":"internal"} while create answered reserved_subdomain on the same
  // tree. So the token must appear beside the RENAME handler's refusal too.
  //
  // The shape checked is the shared writer (writeWorkspaceSlugReserved), called
  // from both arms: asserted from source because the alternative — restating the
  // JSON body here — would pass over a handler that stopped calling it.
  assert.match(
    goRestWorkspaces,
    /func writeWorkspaceSlugReserved\(/,
    "httpapi no longer has the ONE reserved-refusal writer — create and rename can drift apart again",
  );
  assert.match(
    goRestWorkspaces,
    /case errors\.Is\(err, dbstore\.ErrWorkspaceSlugReservedSubdomain\):/,
    "the REST RENAME switch has no arm for the reserved-list sentinel, so a rename onto a reserved " +
      "label falls through to respondErr and answers 500 {\"code\":\"internal\"} (the defect, t_758bb125)",
  );
  assert.match(
    goRestWorkspaces,
    /func writeWorkspaceSlugReserved\(w http\.ResponseWriter, verr error\) \{[\s\S]*?"code":\s*urlguard\.ReservedSubdomainCode/,
    "the shared reserved-refusal writer no longer carries the reserved_subdomain token — the dashboard " +
      "would fall back to its generic 400 sentence for a refusal it can explain exactly (#694)",
  );
  // MCP: the same refusal as DATA carrying the code, never the `db: ` wrap.
  // Two assertions rather than one windowed regex: the arm must exist, and the
  // code must be written. A single `[\s\S]{0,N}` window is brittle — it depends
  // on how long the comment above the arm happens to be, so it would start
  // failing for a reason unrelated to the property it guards.
  assert.match(
    goMcpWorkspaces,
    /case errors\.Is\(err, dbstore\.ErrWorkspaceSlugReservedSubdomain\):/,
    "the MCP rename has no arm for the reserved-list sentinel, so the refusal matches no case and " +
      "falls to `fmt.Errorf(\"db: %w\", err)` — an agent reads `db: …` for input it can simply change " +
      "(the defect, t_758bb125)",
  );
  assert.match(
    goMcpWorkspaces,
    /"code":\s*urlguard\.ReservedSubdomainCode/,
    "the MCP reserved refusal does not carry the machine code, so a caller cannot decide its sentence " +
      "from the code (#694) and must parse prose",
  );
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
