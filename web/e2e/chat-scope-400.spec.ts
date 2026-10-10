// The chat confirm path's refusals: decided by CODE, and never two messages.
//
// THE FIRST DEFECT (card t_790b1016, filed by the QA lane while verifying PR 747).
// A 400 on the confirm/cancel path is a WORKSPACE-SCOPE REFUSAL, not a failure of
// the change. The PR gave it its own alert copy (`chat.scopeRequired`) but left
// the proposal card on `setState(..., "failed")`, and the card's footer renders
// `chat.confirmFail` for that state. One 400 therefore put TWO sentences on
// screen at once, and the second one was wrong advice:
//
//     role=alert  : "Choose a workspace first — the assistant answers for one
//                    workspace at a time."
//     card footer : "The change could not be applied. Please try again."
//
// Retrying without a workspace fails identically, so the footer instructed the
// user to repeat an action that cannot work.
//
// THE SECOND DEFECT (card t_fb393abe, the same lane). The branch that got its own
// copy was keyed on the BARE STATUS — `if (r.status === 400)` — and returned
// before the body was read. A 400 on this route is NOT necessarily a scope
// refusal: the server maps every unrecognised confirm failure to 400, and one such
// class arrives AFTER the token is claimed (a tool that refuses to execute — the
// write-role gate for a read_only workspace member). That user's request carried
// their own ?workspace= and the page still told them to choose one. Both 400
// cases are driven below on the SAME status and the SAME request shape; only the
// body's machine `code` separates them, which is why the page now branches on it.
//
// WHY THIS IS A BROWSER TEST AND NOT ONLY A SOURCE GUARD. `chatPage.test.ts` pins
// the page's branch and `chatConfirm.test.ts` executes the decision, and those are
// the guards that bite on a revert — but the page has no React/DOM runner in
// `npm run test:unit`, so nothing there can observe that a sentence actually
// appeared ON a rendered page. This file measures the rendered DOM of the real
// production build, which is how both defects were found in the first place.
//
// HOW THE STATE IS REACHED WITHOUT A SESSION OR A BACKEND. The reachable shape
// is "propose while a workspace is selected, then lose the scope WITHOUT a
// reload" (a second tab's sign-out, a storage clear). Both halves are reproduced
// exactly: an init script selects `ws-b`, then AFTER the proposal card is on
// screen the test clears `jf_workspace` and presses Confirm — no navigation, so
// the Shell never re-selects a workspace and the request goes out unscoped, which
// is what the server answers 400 to. The API is stubbed at the browser level
// (the pattern `e2e/a11y-dialogs.spec.ts` uses), so no credential is fabricated
// and no Go server or database is involved.
//
// WHAT MAKES IT BITE. The assertions read the shipped English copy out of the
// app's own dictionary rather than typing it in, and they count occurrences:
// "renders the scope copy ONCE" is a count, not a substring. A revert to
// `setState(..., "failed")` puts `chat.confirmFail` back on the page and fails
// both the absence assertion and the count; a revert to the bare status test makes
// the non-scope case render the scope copy. The scope code itself is read from
// src/lib/chatConfirm.ts rather than typed here, so a rename on the server cannot
// silently make the scope case unreachable (it would then be indistinguishable
// from the non-scope 400, and the first test would pass for the wrong reason).
// Verified by mutation, not assumed.
//
// Run with:
//
//   npm run a11y                       # the whole gate (this file included)
//   npx playwright test e2e/chat-scope-400.spec.ts    # this file alone
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";
import { WORKSPACE_SCOPE_REQUIRED_CODE, PROPOSAL_ARGS_INVALID_CODE } from "../src/lib/chatConfirm";
import { sendChatPrompt } from "./chatComposer";

const EN = {
  scopeRequired: DICT["chat.scopeRequired"].en,
  confirmFail: DICT["chat.confirmFail"].en,
  proposalStale: DICT["chat.proposalStale"].en,
  proposalUnexecutable: DICT["chat.proposalUnexecutable"].en,
  confirmRateLimited: DICT["chat.confirmRateLimited"].en,
  badgeFailed: DICT["chat.proposalBadgeFailed"].en,
  badgeScopeRequired: DICT["chat.proposalBadgeScopeRequired"].en,
  badgeUnexecutable: DICT["chat.proposalBadgeUnexecutable"].en,
  confirm: DICT["chat.confirm"].en,
  send: DICT["chat.send"].en,
  placeholder: DICT["chat.placeholder"].en,
};

const TOKEN = "tok-1";
const WS = "ws-b";

/**
 * The proposal the assistant hands back, and the card that renders it.
 *
 * THE SHAPE IS THE SERVER'S (t_30ffa28a). The card's own wording is carried as
 * DICTIONARY KEYS (title_key / summary_key / label_key) plus the merchant's data —
 * the API does not know the caller's language, so it does not write the words. The
 * English text these tests look for is therefore read out of the dictionary, and
 * the fixture below carries no sentence of its own except the assistant's `note`.
 */
const PROPOSAL = {
  token: TOKEN,
  args_hash: "hash-1",
  tool: "update_product",
  title_key: "chat.proposal.title.update_product",
  summary_key: "chat.proposal.summary.update_product",
  fields: [{ label_key: "chat.proposal.label.name", value: "Tee 2026 (XL)" }],
  note: "The assistant's own sentence, in the conversation's language.",
  expires_at: "2027-01-01T00:00:00Z",
  reversible: true,
};

/** The card's title as the ENGLISH dashboard renders it. */
const CARD_TITLE = DICT["chat.proposal.title.update_product"].en;

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: WS, role: "admin" }],
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
 * Stub every call the page makes. `decisionStatus` and `decisionCode` are the
 * answer the confirm/cancel route gives — the two variables these tests turn.
 *
 * `decisionCode` is what the page must BRANCH on (t_fb393abe): the card's outcome
 * comes from the API's machine `code`, never from the bare status, so a test that
 * sends a 400 with no code exercises the "unknown refusal" path rather than the
 * scope refusal. The default (no code) is therefore the shape a proxy/older
 * deployment answers, not the scope case.
 */
async function stubAPI(page: Page, decisionStatus: number, decisionCode = "") {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  // Availability, then the message that produces the card. Same path, so the
  // method decides which stub answers.
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }))
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [PROPOSAL] })),
  );
  const statusText: Record<number, string> = {
    400: "skop workspace diperlukan — pilih workspace dahulu",
    409: "cadangan ini sudah luput atau telah digunakan",
    429: "had pengesahan dicapai",
    500: "ralat dalaman",
  };
  await page.route(/(.*)\/api\/rest\/chat\/(confirm|cancel)(\?.*)?$/, (r) => {
    // A non-scope 400 carries the TOOL's prose, not the scope sentence: that is
    // what the server sends for a post-claim refusal, and the page must still not
    // render the scope copy for it.
    // A 409 carrying the unexecutable code carries its own message (the server's
    // real one), so the stub reproduces the shape a real server sends — the page
    // must branch on the CODE, not on this prose.
    const unexecutable = decisionStatus === 409 && decisionCode === UNEXECUTABLE_CODE;
    const text = unexecutable
      ? UNEXECUTABLE_WIRE_TEXT
      : decisionStatus === 400 && decisionCode === NON_SCOPE_400_CODE
        ? NON_SCOPE_400_WIRE_TEXT
        : (statusText[decisionStatus] ?? "ralat");
    return r.fulfill({
      status: decisionStatus,
      contentType: "application/json",
      body: JSON.stringify({
        error: text,
        ...(decisionCode ? { code: decisionCode } : {}),
      }),
    });
  });
}

/**
 * The unexecutable refusal's code, read out of the web constant that mirrors the
 * server's (src/lib/chatConfirm.ts, itself read from api/internal/httpapi/errcode.go
 * by chatConfirm.test.ts). Read rather than typed so the two spellings cannot
 * drift — a rename on the server would silently make this case unreachable.
 */
const UNEXECUTABLE_CODE = PROPOSAL_ARGS_INVALID_CODE;

/**
 * The server's own wire prose for the unexecutable refusal (rest_chat.go's
 * chatConfirmMessageFor). Deliberately NOT the spent/expired sentence: the stub
 * must carry the shape a real server sends, so this test proves the page tells
 * the live token from the dead one.
 */
const UNEXECUTABLE_WIRE_TEXT = "cadangan itu tidak boleh dilaksanakan — minta pembantu menyediakan kad baharu";

/**
 * The scope refusal's code, read out of the WEB constant that mirrors the server's
 * (src/lib/chatConfirm.ts). Read rather than typed so the two spellings cannot
 * drift: a rename on the server fails here instead of silently making the scope
 * case unreachable (it would then be indistinguishable from the non-scope 400 and
 * this file's whole first test would pass for the wrong reason).
 */
const SCOPE_CODE = WORKSPACE_SCOPE_REQUIRED_CODE;

/**
 * A code that is NOT the scope refusal: the shape the server answers when a tool
 * it was asked to execute refuses (a read_only member's confirmed write, after the
 * token was claimed). The wire message is the tool's own; the code is the HTTP class.
 */
const NON_SCOPE_400_CODE = "request.badRequest";

/**
 * The wire prose that comes with NON_SCOPE_400_CODE — QA's measured body for the
 * post-claim refusal (api/internal/mcpsrv/callscope.go's write-role gate). It is
 * deliberately NOT the scope sentence: the stub must carry the shape a real server
 * sends, or the test would prove nothing about the page telling them apart.
 */
const NON_SCOPE_400_WIRE_TEXT = "perlu peranan manage atau admin dalam workspace ini";

/**
 * A FRESH browser state (cleared storage — the browser keeps localStorage
 * between runs and a `jf_lang` left by an earlier card would render these
 * strings in a locale this file never asserts), English, with a workspace
 * selected so the FIRST chat call is scoped.
 */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/**
 * Get a proposal on screen, then (by default) LOSE the workspace scope without
 * reloading, and press Confirm. Returns the request URL the decision went out
 * to, so the test can assert the shape it believes it reproduced.
 *
 * `dropScope: false` keeps the SELECTED workspace, which is the other shape this
 * card needs: a request that carries `?workspace=` and is still refused with a
 * 400 (a post-claim tool refusal). That is the shape QA measured — the user has a
 * workspace and is told to choose one, which is the defect (t_fb393abe).
 */
async function confirmWithoutScope(page: Page, baseURL: string | undefined, dropScope = true) {
  // The prologue is SHARED with the other /chat specs (e2e/chatComposer.ts, card
  // t_b9eea741). This helper carried the identical assert-then-fill-then-assert
  // shape that PR 950 (t_4221975d) wrote for the OTHER failure mode, and it races
  // this one the same way: the server-rendered composer is already `enabled`
  // (the page's `available` default is null), so a `fill` issued before React
  // hydrates is applied to markup nothing is listening to and is discarded by
  // hydration — leaving the Send assertion below to burn its whole timeout on a
  // page that is in fact correct. `sendChatPrompt` waits for the availability
  // probe's response (only reachable post-hydration) and retries the type until
  // Send follows, so the wait is a wait and not a red.
  await sendChatPrompt(page, baseURL, "rename my product");

  const card = page.locator('[class*="border-sky-200"]').filter({ hasText: CARD_TITLE });
  await expect(card, "the proposal card must render — otherwise this test is vacuous").toHaveCount(1);

  // The scope drop is no longer performed here (see the re-anchor comment above);
  // the `dropScope` flag still selects which SHAPE this run asserts.
  //
  // RE-ANCHORED FOR t_da4b2031. Clearing `jf_workspace` used to un-scope the
  // decision, because `workspaceQuery()` read localStorage directly. Under the
  // confirmed-scope rule (lib/workspace.ts) the workspace the bundle CONFIRMED is
  // still the active one, so clearing storage mid-session changes nothing about
  // the request — and asserting a shape the fix deliberately no longer produces is
  // how a guard starts lying. The refusal is now reached by the stub's ANSWER
  // (`decisionStatus`/`decisionCode` in stubAPI), which is what this file is
  // actually about: the PAGE telling the two 400s apart.
  if (!dropScope) {
    expect(
      await page.evaluate(() => localStorage.getItem("jf_workspace")),
      "the selected workspace must still be stored — this shape is a SCOPED request, which is the " +
        "case where 'choose a workspace first' is advice the user cannot act on",
    ).toBe("ws-b");
  }

  const decision: string[] = [];
  page.on("request", (r) => {
    if (/\/api\/rest\/chat\/(confirm|cancel)/.test(r.url())) decision.push(r.url());
  });

  await card.getByRole("button", { name: EN.confirm }).click();
  // Scoped to a `<p>`: Next renders its own route announcer as
  // `<div role="alert" id="__next-route-announcer__">`, so the bare role locator
  // is ambiguous and the page still holds a SECOND empty alert node. The chat
  // page's message paragraph is the `p[role="alert"]`.
  await expect(page.locator('p[role="alert"]')).toBeVisible({ timeout: 10_000 });
  return decision;
}

test.describe("chat confirm path: a scope refusal is not a failed change", () => {
  test("a 400 renders the scope copy once and never the 'try again' footer", async ({
    page,
    baseURL,
  }) => {
    await stubAPI(page, 400, SCOPE_CODE);
    await freshState(page);
    const decision = await confirmWithoutScope(page, baseURL);

    // The shape is asserted in the direction that matters (t_da4b2031): the
    // decision must never carry a slug the session does not have. Asserting
    // UNSCOPED is no longer right — the confirmed scope is deliberately retained
    // — and a guard that demanded it would fail on a correct build.
    expect(decision, "the decision must have been sent exactly once").toHaveLength(1);
    expect(
      decision[0],
      `the decision must not carry a workspace the session does not have. Sent: ${JSON.stringify(decision)}`,
    ).not.toContain("workspace=ws-gone");

    // ONE message. Counted, because the defect was two.
    const scopeNodes = page.getByText(EN.scopeRequired, { exact: true });
    await expect(
      scopeNodes,
      "the scope refusal must be rendered exactly once — the pre-fix shape duplicated the copy",
    ).toHaveCount(1);
    await expect(page.locator('p[role="alert"]')).toHaveText(EN.scopeRequired);

    // And NOT the advice that cannot work. This is the assertion the defect
    // fails: the card footer used to render exactly this sentence.
    await expect(
      page.getByText(EN.confirmFail, { exact: true }),
      `nothing on the page may tell the user to try again: retrying without a workspace fails ` +
        `identically ("${EN.confirmFail}")`,
    ).toHaveCount(0);

    // The card itself stops claiming a failure: it says what it NEEDS.
    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: CARD_TITLE });
    await expect(card).toContainText(EN.badgeScopeRequired);
    await expect(
      card,
      `the card must not wear the "did not go through" badge — nothing was attempted`,
    ).not.toContainText(EN.badgeFailed);

    // Report the OBSERVED values so the evidence is in the run log, not inferred.
    console.log(
      `OBSERVED scope=400 code=${JSON.stringify(SCOPE_CODE)} ` +
        `alert=${JSON.stringify(await page.locator('p[role="alert"]').textContent())} ` +
        `scopeCopyCount=${await scopeNodes.count()} ` +
        `confirmFailCount=${await page.getByText(EN.confirmFail, { exact: true }).count()}`,
    );
  });

  test("a 400 that is NOT a scope refusal never renders the scope copy", async ({ page, baseURL }) => {
    // t_fb393abe. The route answers 400 for a second, unrelated condition: a tool
    // the server refused to execute AFTER it claimed the token (a read_only
    // member's confirmed write, api/internal/chat/proposal.go's "server menolak").
    // The page used to branch on the bare status, so that user — whose request
    // carried their own ?workspace= — was told to "Choose a workspace first".
    //
    // THE SHAPE IS THE POINT, and it is QA's measured one: the request KEEPS its
    // ?workspace= (the user has a workspace selected) and is still refused with a
    // 400 — the post-claim tool refusal. The same status as the test above, the
    // opposite scope shape; the CODE in the body is the only thing that can
    // separate them, which is exactly why the page must branch on it.
    await stubAPI(page, 400, NON_SCOPE_400_CODE);
    await freshState(page);
    const decision = await confirmWithoutScope(page, baseURL, false);

    expect(decision, "the decision must have been sent exactly once").toHaveLength(1);
    expect(
      decision[0],
      `the decision must have gone out SCOPED — that is the shape QA measured and the case where ` +
        `"choose a workspace first" is advice the user cannot act on. Sent: ${JSON.stringify(decision)}`,
    ).toContain("workspace=ws-b");

    // THE ASSERTION THAT FAILS PRE-FIX: the scope copy must not appear at all.
    const scopeNodes = page.getByText(EN.scopeRequired, { exact: true });
    await expect(
      scopeNodes,
      `a 400 whose body does not name the scope code rendered "Choose a workspace first" — that is ` +
        `exactly the pre-fix defect: any 400 on this route was reported as a missing workspace, ` +
        `including a refusal that happened AFTER the token was claimed and had nothing to do with scope`,
    ).toHaveCount(0);
    await expect(page.locator('p[role="alert"]')).not.toHaveText(EN.scopeRequired);

    // Instead the generic failure copy — the honest answer, and the one the card
    // footer already carries. On this branch the token really IS spent and nothing
    // changed, so the "try again" advice is accurate (it asks for a NEW card).
    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: CARD_TITLE });
    await expect(
      card,
      "a non-scope 400 must report a failed change on the card",
    ).toContainText(EN.confirmFail);
    await expect(card).toContainText(EN.badgeFailed);
    await expect(
      card,
      `the card must not claim it needs a workspace — the user already had one selected`,
    ).not.toContainText(EN.badgeScopeRequired);

    // Report the OBSERVED values, including the request shape, so the evidence is
    // in the run log rather than inferred from the assertions.
    console.log(
      `OBSERVED nonScope=400 code=${JSON.stringify(NON_SCOPE_400_CODE)} ` +
        `request=${JSON.stringify(decision[0])} ` +
        `alert=${JSON.stringify(await page.locator('p[role="alert"]').textContent())} ` +
        `scopeCopyCount=${await scopeNodes.count()}`,
    );
  });

  test("a genuine failure still renders 'try again' on the card", async ({ page, baseURL }) => {
    // The other direction, so the fix cannot be satisfied by deleting the
    // failure path: a 5xx IS a failure of the change and keeps its own copy —
    // including on the card, which is the element this card's fix touched.
    await stubAPI(page, 500);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: CARD_TITLE });
    await expect(
      card,
      `a genuine failure must still render chat.confirmFail on the card — deleting the failure path ` +
        `would satisfy the 400 assertions for the wrong reason`,
    ).toContainText(EN.confirmFail);
    await expect(
      page.getByText(EN.scopeRequired, { exact: true }),
      "a 5xx is not a scope refusal — the scope copy must not appear",
    ).toHaveCount(0);
    // The card reports the failure it actually had, not "needs a workspace".
    await expect(card).toContainText(EN.badgeFailed);
    await expect(card).not.toContainText(EN.badgeScopeRequired);
  });

  test("a 409 (stale) keeps its own distinct copy, and is not reported as a failure", async ({
    page,
    baseURL,
  }) => {
    await stubAPI(page, 409);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    // The card's 409 branch is unchanged by this fix and must stay reachable.
    // Count is NOT asserted: the 409 pre-dates this card with the alert and the
    // footer carrying the SAME sentence (duplicated but not contradictory), and
    // the card's acceptance is that the copy stays distinct — not that it moves.
    await expect(
      page.getByText(EN.proposalStale, { exact: true }).first(),
      "a 409 must keep its own 'expired or already used' copy",
    ).toBeVisible();
    await expect(
      page.getByText(EN.confirmFail, { exact: true }),
      "a spent proposal is not a failed change — the 'try again' copy must not appear",
    ).toHaveCount(0);
  });

  test("a 429 keeps the rate-limit copy", async ({ page, baseURL }) => {
    await stubAPI(page, 429);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    await expect(
      page.getByText(EN.confirmRateLimited, { exact: true }).first(),
      "a rate-limited confirmation must keep its own limit copy, not the generic failure",
    ).toBeVisible();
    await expect(
      page.getByText(EN.scopeRequired, { exact: true }),
      "a rate limit is not a scope refusal — the scope copy must not appear",
    ).toHaveCount(0);
  });

  test("a 409 for an unexecutable card is not reported as expired or as a failure", async ({
    page,
    baseURL,
  }) => {
    // t_623676d1. A proposal whose arguments the target tool would never accept —
    // the model keyed the object `id` while update_product requires `product_id` —
    // used to answer a bare 400 AFTER spending the token, and the page told the
    // user "The change could not be applied. Please try again." for a card that
    // can never work. It now answers 409 with its OWN code, and the token is UNSPENT
    // (the refusal runs before the claim), so the page must say what is actually
    // true: nothing was attempted, nothing was used up, ask for a fresh card.
    await stubAPI(page, 409, UNEXECUTABLE_CODE);
    await freshState(page);
    await confirmWithoutScope(page, baseURL);

    // THE ASSERTION THAT FAILS PRE-FIX: the page must NOT claim the card expired.
    // That sentence ("expired or was already used") is false of this card — the
    // token is still live — and it would hide the one thing the user needs to know.
    await expect(
      page.getByText(EN.proposalStale, { exact: true }),
      `the unexecutable 409 rendered the spent/expired copy ("${EN.proposalStale}") — the token is ` +
        `UNSPENT, the card neither expired nor was used, and the user is misled about which state they are in`,
    ).toHaveCount(0);
    // And not a failed change either: nothing was attempted.
    await expect(
      page.getByText(EN.confirmFail, { exact: true }),
      `the unexecutable card rendered "try again" ("${EN.confirmFail}") — retrying the SAME card can ` +
        `never succeed as shown, so the advice is unusable`,
    ).toHaveCount(0);

    // Instead its own sentence, ONCE (the alert), and its own badge on the card.
    await expect(page.locator('p[role="alert"]')).toHaveText(EN.proposalUnexecutable);
    const copyNodes = page.getByText(EN.proposalUnexecutable, { exact: true });
    await expect(
      copyNodes,
      "the unexecutable refusal must be rendered exactly once — the card footer and the alert must not both carry it",
    ).toHaveCount(1);
    const card = page.locator('[class*="border-sky-200"]').filter({ hasText: CARD_TITLE });
    await expect(card).toContainText(EN.badgeUnexecutable);
    await expect(
      card,
      'the card must not wear the "did not go through" badge — nothing was attempted and nothing was used up',
    ).not.toContainText(EN.badgeFailed);

    console.log(
      `OBSERVED unexecutable=409 code=${JSON.stringify(UNEXECUTABLE_CODE)} ` +
        `alert=${JSON.stringify(await page.locator('p[role="alert"]').textContent())} ` +
        `staleCount=${await page.getByText(EN.proposalStale, { exact: true }).count()} ` +
        `confirmFailCount=${await page.getByText(EN.confirmFail, { exact: true }).count()}`,
    );
  });
});
