// The history the chat page PUTS ON THE WIRE is capped by the limit the server
// REPORTS — not by a literal, and not by the edge's reject threshold.
//
// THE DEFECT (card t_4c29963a, measured on production, api v1.16.24/4238550).
// Three numbers described the same thing and agreed with none of the others: the
// page sent `MAX_HISTORY = 32` turns, the edge rejects a request at
// `chatMaxHistory = 40` (rest_chat.go), and the runner KEEPS 16 (`chat.MaxTurns`,
// chat.go). A signed-in 19-turn history whose first turn carried a unique
// passphrase came back "Tiada. Sepanjang perbualan ini (giliran 4 hingga 19) tiada
// kata laluan disebut" — the model citing exactly the window it could see, while
// the user could still scroll turns 1-3. The page was already TOLD the right
// number: GET /v1/rest/chat answers `"max_history_turns":16`, and the page
// declared the field and never read it.
//
// WHY THIS IS A BROWSER TEST AND NOT ONLY THE SOURCE GUARD. `chatHistoryCap.test.ts`
// pins the page's source and the Go constants against each other, and it bites on a
// revert — but what a user experiences is the BODY of a POST, and only a real build
// puts one on a wire. This file drives the production build, reads the request
// bodies the page actually sent, and asserts the cap the page applied.
//
// WHAT MAKES IT BITE. The stub reports a cap that is NEITHER literal in the page
// (`max_history_turns: 4`): the fixed page sends 4, the pre-fix page sends 10 for
// the same transcript (it sliced to its own 32), and no page can pass this by
// happening to agree with a constant. The last body's `history` length is asserted
// EXACTLY, and every body's is bounded above — so a page that sends more than the
// reported cap anywhere in the run fails, including one that caps only the last.
//
// Run with:
//
//   npm run e2e                                  # the whole gate (this file included)
//   npx playwright test e2e/chat-history-cap.spec.ts
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { sendChatPrompt } from "./chatComposer";
import { DICT } from "../src/components/i18nDict";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts), so a spec
// that visits one declares the session it means. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const EN = {
  send: DICT["chat.send"].en,
  title: DICT["chat.title"].en,
};

/**
 * The cap the stub reports. Deliberately a number that is not a literal anywhere in
 * the page (32 was, 16 is the fallback), so "the page sent the reported cap" and
 * "the page sent one of its own numbers" cannot be the same observation.
 */
const REPORTED_CAP = 4;

/** How many turns this spec sends. The transcript outgrows the cap on turn 3. */
const TURNS = 6;

/** Every POST body the page sent, in order. */
type ChatPost = { message: string; history: Array<{ role: string; content: string }> };

/** The dashboard bundle the Shell resolves the session from (see chat-probe-400). */
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-a", name: "First", slug: "ws-a", role: "admin" }],
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
 * Serve the dashboard and the chat API, recording every message POST's body.
 *
 * Catch-all FIRST, specific routes after it: Playwright resolves matching routes in
 * reverse registration order, so the later (specific) ones win.
 */
async function stubChat(page: Page): Promise<ChatPost[]> {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const limits = {
    messages_per_hour: 60,
    burst: 10,
    max_message_chars: 2000,
    max_history_turns: REPORTED_CAP,
    max_tool_steps: 4,
    max_proposals_per_message: 3,
    proposal_ttl_seconds: 300,
    confirms_per_hour: 30,
    confirm_burst: 5,
  };
  const posts: ChatPost[] = [];
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) => {
    if (r.request().method() === "GET") {
      // The availability probe AND the reported cap — the number this spec is about.
      return r.fulfill(json({ available: true, limits, read_only: true }));
    }
    posts.push(JSON.parse(r.request().postData() ?? "{}") as ChatPost);
    return r.fulfill(json({ reply: "ok", steps: [], limits }));
  });
  return posts;
}

/** A FRESH browser state: no localStorage at all, English. */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

test("the page sends at most the cap the server reports, whatever that cap is", async ({ page, baseURL }) => {
  const posts = await stubChat(page);
  await freshState(page);

  // Turn 1 goes through the shared prologue, which waits for the page to be LIVE
  // (hydration) rather than merely painted — see e2e/chatComposer.ts. Every turn
  // after it is typed straight into the composer, because by then it has answered.
  await sendChatPrompt(page, baseURL, "turn 1");
  await expect.poll(() => posts.length, { timeout: 15_000 }).toBe(1);

  const box = page.locator("#chat-input");
  const send = page.getByRole("button", { name: EN.send });
  for (let i = 2; i <= TURNS; i++) {
    await box.fill(`turn ${i}`);
    await expect(send, "Send must be enabled once the composer holds text").toBeEnabled();
    await send.click();
    await expect
      .poll(() => posts.length, {
        timeout: 15_000,
        message:
          `the page must have sent message ${i}. If no POST is recorded the composer stopped ` +
          `accepting text after the previous answer, which is a different defect than this one`,
      })
      .toBe(i);
  }

  // The page rendered the whole conversation — otherwise the transcript never
  // outgrew the cap and the assertion below would be measuring nothing.
  await expect(page.getByRole("heading", { name: EN.title })).toBeVisible();
  expect(posts.at(-1)!.message, "the last POST must be this loop's last turn").toBe(`turn ${TURNS}`);

  // THE FIX. The last send carries the reported cap — 4 turns of a 10-message
  // transcript — because the page slices by `limits.max_history_turns`.
  // PRE-FIX (MAX_HISTORY = 32) this body carries all 10, and so does a page that
  // mirrors any other number.
  expect(
    posts.at(-1)!.history.length,
    `the history the page SENT was ${posts.at(-1)!.history.length} turns long, but the server ` +
      `reported max_history_turns: ${REPORTED_CAP}. The page must send the cap the server ` +
      `enforces (chat.MaxTurns in api/internal/chat/chat.go): a longer history is silently ` +
      `dropped by the runner, which is how the assistant came to answer about a window the ` +
      `user could not see it had lost`,
  ).toBe(REPORTED_CAP);
  for (const [i, p] of posts.entries()) {
    expect(
      p.history.length,
      `POST ${i + 1} sent ${p.history.length} turns — more than the reported cap of ` +
        `${REPORTED_CAP}. Sent lengths: ${JSON.stringify(posts.map((x) => x.history.length))}`,
    ).toBeLessThanOrEqual(REPORTED_CAP);
  }
  // The EXACT shape, so the cap is shown being REACHED and HELD rather than asserted
  // once: turn i sends min(2(i-1), cap) turns, because every earlier exchange added
  // one user turn and one answer. PRE-FIX this array is [0, 2, 4, 6, 8, 10] — the
  // page sliced to its own 32 and the whole transcript went out.
  expect(
    posts.map((p) => p.history.length),
    `the history length of each POST, in order. The transcript grows by two messages a turn, so ` +
      `turn ${REPORTED_CAP / 2 + 1} onwards must carry exactly the reported cap of ${REPORTED_CAP} ` +
      `and never more`,
  ).toEqual([0, 2, 4, 4, 4, 4]);
});
