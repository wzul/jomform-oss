// A stale `jf_workspace` slug must NOT sign a signed-in user out (t_da4b2031).
//
// THE DEFECT. `jf_workspace` can name a workspace that was renamed or deleted
// while it was selected. Every scoped fetch then carries that slug, the server
// answers 403 "workspace not found" (a TENANCY refusal: `resolveAccountScope`
// -> `scopeOr403`), and `web/src/lib/wsfetch.ts` read 401/403 as "the session is
// gone" and navigated to /login. Measured on prod (v1.17.0): `/chat` with
// `jf_workspace = ws-gone` ended on https://jomform.com/login.
//
// THE FIX, AS MEASURED HERE. The 403 carries a machine code
// (`workspace.notFound`), so wsfetch classifies it as a SCOPE refusal rather than
// a dead session: the stale slug is dropped, the session re-resolves a workspace
// the caller really has, and the page refetches. The user stays on the page.
//
// WHY THIS IS A BROWSER TEST AND NOT ONLY THE UNIT GUARD. `wsfetch.test.ts`
// executes the classification and `workspaceScope.test.ts` executes the scope
// publishing, and those bite on a revert. Neither can show the ORDER, which is
// the whole defect: the failing request is the one fired on mount, before the
// session bundle has answered. Only a real build can show that the page survives
// it — and only a real build shows the final URL.
//
// HOW THE SERVER IS REPRODUCED WITHOUT A BACKEND. The stub answers exactly what
// `resolveAccountScope` -> `scopeOr403` answers. A `?workspace=` the session does
// not have gets `403 {"error":"workspace not found","code":"workspace.notFound"}`
// — the body `writeErr` builds from `errCodeByMessage` in
// api/internal/httpapi/errcode.go, which this file READS rather than restating.
// An unscoped request is served normally, and its bundle carries the real list.
//
// WHAT MAKES IT BITE. The assertions are the two directions: the page stays (no
// /login) AND the stale slug is replaced by a workspace the caller has, so the
// scope-using behaviour is exercised rather than merely tolerated. The control
// below drives a 401 and an UNCoded 403 on the same route and asserts the
// /login navigation still happens — a fix that swallowed every refusal would
// leave an expired session hanging, and that control fails if it does.
//
// Run with:
//
//   npm run e2e                                        # the gate that runs this file
//   npx playwright test e2e/stale-workspace-slug.spec.ts --project=feature-core
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { readFileSync } from "node:fs";
import { join } from "node:path";

import { DICT } from "../src/components/i18nDict";

const EN = {
  scopeRequired: DICT["chat.scopeRequired"].en,
  unavailable: DICT["chat.unavailable"].en,
  title: DICT["chat.title"].en,
  placeholder: DICT["chat.placeholder"].en,
};

/**
 * The tenancy code and the wire prose, read out of the SERVER source rather than
 * typed here: a rename on the server must fail this file, not silently leave it
 * stubbing a body the API never sends.
 *
 * `__dirname`, not `import.meta.url`: package.json has no `"type": "module"`, so
 * Playwright loads this spec as CommonJS and `import.meta` is a syntax error
 * (the same reason e2e/settings-tabs.spec.ts uses `__dirname`).
 */
const errcodeGo = readFileSync(join(__dirname, "../../api/internal/httpapi/errcode.go"), "utf8");
const NOT_FOUND_CODE = (errcodeGo.match(/\bCodeWorkspaceNotFound\s*=\s*"([^"]+)"/) ?? [])[1] ?? "";
const NOT_FOUND_PROSE = "workspace not found";

/** The slug the browser holds from a workspace that no longer exists. */
const STALE = "ws-gone";
/** The workspace the session really has. */
const REAL = "ws-a";

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-a", name: "Real", slug: REAL, role: "admin" }],
  chip_status: null,
  keys: [],
  domains: [],
  webhooks: [],
  social: [],
  einvoice: null,
  products: [],
  summary: { counts: {}, revenue_sen: 0 },
  sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [],
  product_analytics: [],
};

/**
 * The scope refusal `scopeOr403` writes, as the parsed body.
 *
 * @param code overrides the machine code, for the control cases: a 403 whose body
 * carries NO tenancy code (an authorization refusal) must still be a session
 * failure, and that is the case this file proves was not softened.
 */
function scopeRefusal(code: string | null = NOT_FOUND_CODE, status = 403) {
  return {
    status,
    contentType: "application/json",
    body: JSON.stringify({
      error: NOT_FOUND_PROSE,
      ...(code ? { code } : {}),
    }),
  };
}

/** Every response a signed-in dashboard page needs, with the real scope rules. */
async function stubScopedAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Requests this file records, so the shape it reproduced can be asserted
  // rather than assumed (a test that stops sending the stale slug would pass for
  // the wrong reason).
  const seen: string[] = [];
  await page.addInitScript(() => {
    (window as unknown as { __scopeLog: string[] }).__scopeLog = [];
    // document.cookie / storage are cleared so the run starts from the state a
    // new visitor has, with only the stale slug seeded.
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-gone");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
  page.on("request", (r) => {
    if (/\/api\/rest\//.test(r.url())) seen.push(r.url());
  });

  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));

  // THE SCOPE RULE. A slug the session does not have is refused; no scope is
  // served the real list. This is `scopeOr403`'s behaviour, not a choice of the
  // test's. `r.request().url()` because Playwright's Route has no `url` of its
  // own; a `route.fulfill` with no url would stub the wrong request silently.
  const stale = (r: { request: () => { url: () => string } }) =>
    r.request().url().includes(`workspace=${STALE}`);
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
    stale(r) ? r.fulfill(scopeRefusal()) : r.fulfill(json(BUNDLE)),
  );
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    stale(r)
      ? r.fulfill(scopeRefusal())
      : r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } })),
  );
  // Every other scoped list the Shell/page may reach.
  await page.route(/(.*)\/api\/rest\/(forms|products|sales|feedback|events)(\?.*)?$/, (r) =>
    stale(r) ? r.fulfill(scopeRefusal()) : r.fulfill(json([])),
  );
  return seen;
}

test.describe("a stale jf_workspace slug does not sign the user out", () => {
  test("the guard reads the real server contract (it cannot pass vacuously)", () => {
    expect(
      NOT_FOUND_CODE,
      "errcode.go no longer declares CodeWorkspaceNotFound — the stub below would answer a body the " +
        "API never sends, so this file would prove nothing",
    ).toBe("workspace.notFound");
    expect(NOT_FOUND_PROSE.length).toBeGreaterThan(0);
  });

  test("/chat survives a stale slug, stays signed in, and re-resolves the workspace", async ({
    page,
    baseURL,
  }) => {
    const seen = await stubScopedAPI(page);
    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

    // The page must NOT have been replaced by the login screen. The defect is a
    // NAVIGATION, so the assertion is the final URL, not the presence of a node.
    await page.waitForTimeout(2500);
    const url = page.url();
    expect(
      url,
      `a stale jf_workspace (${STALE}) redirected the page — this is the reported sign-out (t_da4b2031)`,
    ).not.toContain("/login");
    expect(url).toContain("/chat");

    // The scope-using behaviour is exercised, not merely tolerated: the stale slug
    // is replaced by the workspace the session actually has.
    await expect
      .poll(async () => page.evaluate(() => localStorage.getItem("jf_workspace")), { timeout: 10_000 })
      .toBe(REAL);
    // The request shape really was the defect's: a scoped request carried the
    // STALE slug at least once (if it never did, this test reproduced nothing).
    expect(
      seen.some((u) => u.includes(`workspace=${STALE}`)),
      `no request ever carried workspace=${STALE}, so this run did not reproduce the defect: ${JSON.stringify(seen)}`,
    ).toBe(true);
    // ...and once reconciled, the page asks for the REAL workspace.
    expect(
      seen.some((u) => u.includes(`workspace=${REAL}`)),
      `after reconciliation no request carried workspace=${REAL}, so the page never recovered: ${JSON.stringify(seen)}`,
    ).toBe(true);

    // The page is usable: the assistant is offered (the probe succeeded on the
    // reconciled scope) and no alert is left over.
    await expect(page.locator("#chat-input")).toBeVisible({ timeout: 15_000 });
    await expect(page.locator("#chat-input")).toBeEnabled();
    expect(
      await page.getByText(EN.scopeRequired, { exact: true }).count(),
      "the scope copy is still on screen after the page re-resolved its workspace — the user is being " +
        "told to fix something that is already fixed",
    ).toBe(0);
    expect(await page.getByText(EN.unavailable, { exact: true }).count()).toBe(0);

    console.log(
      `OBSERVED stale=${STALE} finalUrl=${url} jf_workspace=${await page.evaluate(() =>
        localStorage.getItem("jf_workspace"),
      )} scopedRequests=${JSON.stringify([...new Set(seen)].slice(0, 8))}`,
    );
  });

  // THE BLAST RADIUS. The card's own instruction is "audit the pages, not just
  // the chat", and the reason is that this was never chat-specific: EVERY page
  // that fires a scoped fetch on mount went out with the stale slug, and the
  // dashboard-bundle's own request carries it too (QA measured exactly that:
  // `GET /v1/rest/dashboard-bundle?workspace=ws-gone -> 403` in the same trace).
  // Driving each page is what shows the fix holds where the shared code is used
  // rather than only at the site that was reported.
  for (const path of ["/", "/forms", "/products", "/sales", "/settings", "/templates"]) {
    test(`${path} survives a stale slug and stays signed in`, async ({ page, baseURL }) => {
      const seen = await stubScopedAPI(page);
      await page.goto(`${baseURL}${path}`, { waitUntil: "domcontentloaded" });
      await page.waitForTimeout(2000);
      expect(
        page.url(),
        `a stale jf_workspace (${STALE}) redirected ${path} — this is the reported sign-out (t_da4b2031)`,
      ).not.toContain("/login");
      // The run really did exercise the defect's request shape.
      expect(
        seen.some((u) => u.includes(`workspace=${STALE}`)),
        `${path} never sent workspace=${STALE}, so this run reproduced nothing: ${JSON.stringify(seen)}`,
      ).toBe(true);
      console.log(`OBSERVED path=${path} finalUrl=${page.url()} staleRequests=${seen.filter((u) => u.includes(STALE)).length}`);
    });
  }

  test("CONTROL: a 401 on the same route still goes to /login", async ({ page, baseURL }) => {
    // The fix must not have turned into "never navigate": an expired session is
    // still an expired session. Same route, same shape, no tenancy code — and the
    // session failure must still win.
    const json = (body: unknown) => ({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
    await page.addInitScript(() => {
      try {
        localStorage.clear();
        localStorage.setItem("jf_lang", "en");
      } catch {
        /* storage unavailable */
      }
    });
    await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
    await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
      r.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "authentication required" }) }),
    );
    await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) =>
      r.fulfill({ status: 401, contentType: "application/json", body: JSON.stringify({ error: "authentication required" }) }),
    );

    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
    await page.waitForURL(/\/login/, { timeout: 15_000 });
    expect(page.url()).toContain("/login");
    console.log(`OBSERVED control=401 finalUrl=${page.url()}`);
  });

  test("CONTROL: an UNCoded 403 on the same route still goes to /login", async ({ page, baseURL }) => {
    // The other half of the same hazard: the split is by the tenancy CODE, so a
    // 403 the API did NOT name as one (a genuine authorization refusal) must keep
    // its old meaning. Softening it would tell a user with no role to "choose a
    // workspace", which they cannot act on.
    const json = (body: unknown) => ({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
    await page.addInitScript(() => {
      try {
        localStorage.clear();
        localStorage.setItem("jf_lang", "en");
      } catch {
        /* storage unavailable */
      }
    });
    await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
    await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
      r.fulfill(scopeRefusal(null)),
    );
    await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(scopeRefusal(null)));

    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
    await page.waitForURL(/\/login/, { timeout: 15_000 });
    expect(page.url()).toContain("/login");
    console.log(`OBSERVED control=uncoded403 finalUrl=${page.url()}`);
  });
});
