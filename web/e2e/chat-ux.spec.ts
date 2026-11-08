// THE CHAT PAGE'S CONTROLS (card t_148d7100), measured in a real browser with the
// API stubbed at the network boundary — no Go server, no model, no key.
//
// The defects this file pins, one arm each:
//
//   1. THE BUTTON WAS THE STATUS. While sending, the Send button renamed itself
//      "Looking that up…" — so the only control on the page doubled as the state
//      display, and the same sentence appeared again in the transcript. Send must
//      keep its name while an answer is in flight (the e2e prologue looks it up by
//      name), and the state must live in its own `role="status"` line.
//   2. THE EMPTY STATE HAD NOTHING TO PRESS. It becomes chips that SEND their own
//      question through the same path as the composer.
//   3. THE ANSWER COULD NOT BE COPIED. `navigator.clipboard` was never used.
//   4. THE COMPOSER WAS A FIXED TWO-LINE WINDOW (`rows={2}` + `resize-none`). It
//      must grow with its content and stop at eight lines.
//   5. THE WHOLE LOG WAS A LIVE REGION. `aria-live="polite"` on the transcript made
//      a screen reader re-read the conversation on every new message; only the
//      LATEST assistant message may be live, and it is `aria-busy` while streaming.
//
// The copy arm is a real clipboard read, not a spy on the call: a button that
// calls `writeText` with the wrong string (the rendered Markdown, the whole
// transcript) is a defect a spy cannot see.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { CHAT_STARTERS } from "../src/lib/chatStarters";
import { sendChatPrompt } from "./chatComposer";
import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — otherwise
// the whole file asserts against the login page and every arm fails for a reason
// that has nothing to do with the chat (measured: all 5 arms red with the page
// snapshot showing "Sign in or sign up"). See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const EN = {
  send: DICT["chat.send"].en,
  stop: DICT["chat.stop"].en,
  copy: DICT["chat.copy"].en,
  copied: DICT["chat.copied"].en,
  thinking: DICT["chat.thinking"].en,
  title: DICT["chat.title"].en,
};

const WS = "ws-b";
const LIMITS = {
  messages_per_hour: 60,
  burst: 10,
  max_message_chars: 2000,
  max_history_turns: 16,
  max_tool_steps: 6,
  max_proposals_per_message: 2,
  proposal_ttl_seconds: 900,
  confirms_per_hour: 30,
};
const ANSWER = "You have 3 forms live.";

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

/** The POST bodies the page sent, in order. */
type Posted = { message: string; history: Array<{ role: string; content: string }> };

/**
 * Stub every dashboard call. `delayMs` holds the answer back so the in-flight
 * state can be inspected — the state that used to exist only as the button's
 * label.
 */
async function stubAPI(page: Page, opts: { delayMs?: number; steps?: unknown[] } = {}) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  const posted: Posted[] = [];
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, async (r) => {
    if (r.request().method() === "GET") {
      return r.fulfill(json({ available: true, limits: LIMITS, read_only: true }));
    }
    posted.push(JSON.parse(r.request().postData() || "{}") as Posted);
    if (opts.delayMs) await new Promise((res) => setTimeout(res, opts.delayMs));
    return r.fulfill(json({ reply: ANSWER, steps: opts.steps ?? [] }));
  });
  return posted;
}

/** A FRESH browser state: no storage, English, one workspace selected. */
async function freshState(page: Page) {
  await page.addInitScript(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", "en");
    } catch {
      /* storage unavailable */
    }
  });
}

/** Wait for the page to be LIVE (hydrated), then return the composer. */
async function open(page: Page, baseURL: string | undefined) {
  const probed = page
    .waitForResponse((r) => r.request().method() === "GET" && /\/api\/rest\/chat(\?|$)/.test(r.url()), {
      timeout: 20_000,
    })
    .then(() => true)
    .catch(() => false);
  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
  expect(await probed, "the availability probe never answered — the page did not hydrate").toBe(true);
  const box = page.locator("#chat-input");
  await expect(box).toBeEnabled({ timeout: 20_000 });
  return box;
}

const LIVE = '[aria-live="polite"]';

test("the empty state's chips send their own question", async ({ page, baseURL }) => {
  const posted = await stubAPI(page);
  await freshState(page);
  await open(page, baseURL);

  // The chips are in the empty state, before anything has been sent.
  const chipText = DICT[CHAT_STARTERS[0]].en as string;
  const chip = page.getByRole("button", { name: chipText });
  await expect(chip, "the empty state must offer a question to click, not only prose").toBeVisible();

  await chip.click();
  await expect
    .poll(() => posted.length, { timeout: 10_000, message: "clicking a chip must SEND it" })
    .toBe(1);
  expect(
    posted[0].message,
    "the chip must send its own text — a chip that fills the composer but does not send is the " +
      "one-sentence empty state with an extra click",
  ).toBe(chipText);
});

test("Send keeps its name while an answer is in flight, and the status is its own line", async ({
  page,
  baseURL,
}) => {
  await stubAPI(page, { delayMs: 1500 });
  await freshState(page);
  const box = await open(page, baseURL);

  await box.fill("which of my forms are live?");
  await page.getByRole("button", { name: EN.send }).click();

  // THE DEFECT: the submit control used to become "Looking that up…", so the only
  // way to know the page was working was to watch the button's LABEL change — and
  // the same words appeared a second time inside the transcript.
  const send = page.getByRole("button", { name: EN.send });
  await expect(send, "the Send button must keep its name while the answer is in flight").toBeVisible();
  await expect(page.getByRole("button", { name: EN.stop }), "Stop must be its own control").toBeVisible();
  await expect(
    page.getByRole("status"),
    "the state must live in a status line of its own (announced politely to a screen reader)",
  ).toHaveText(EN.thinking);

  // It settles back to send-mode with the answer in the transcript.
  await expect(page.locator(LIVE).last()).toContainText(ANSWER, { timeout: 15_000 });
  await expect(send).toBeVisible();
  await expect(page.getByRole("button", { name: EN.stop })).toHaveCount(0);
});

test("the finished answer can be copied, and the button says so", async ({ page, baseURL, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await stubAPI(page);
  await freshState(page);
  await sendChatPrompt(page, baseURL, "which of my forms are live?");
  await expect(page.locator(LIVE).last()).toContainText(ANSWER, { timeout: 15_000 });

  const copy = page.getByRole("button", { name: EN.copy }).first();
  await expect(copy, "an assistant message must offer a copy control").toBeVisible();
  await copy.click();
  await expect(page.getByRole("button", { name: EN.copied }).first()).toBeVisible();

  const clip = await page.evaluate(() => navigator.clipboard.readText());
  expect(clip, "the clipboard must hold the answer's own words — not the transcript, not the Markdown source").toBe(
    ANSWER,
  );
});

test("the composer grows with its content and stops at eight lines", async ({ page, baseURL }) => {
  await stubAPI(page);
  await freshState(page);
  const box = await open(page, baseURL);

  const height = () => box.evaluate((el: HTMLTextAreaElement) => el.clientHeight);
  const oneLine = await height();

  await box.fill("line one\nline two\nline three\nline four");
  const fourLines = await height();
  expect(fourLines, "the textarea must grow with its content — it was a fixed two-line window").toBeGreaterThan(
    oneLine,
  );

  // A long paste must not swallow the page: it stops at the documented maximum and
  // scrolls inside the box instead.
  await box.fill(Array.from({ length: 30 }, (_, i) => `line ${i + 1}`).join("\n"));
  const many = await box.evaluate((el: HTMLTextAreaElement) => ({
    client: el.clientHeight,
    scroll: el.scrollHeight,
  }));
  const lineHeight = await box.evaluate((el: HTMLTextAreaElement) => parseFloat(getComputedStyle(el).lineHeight) || 20);
  expect(
    many.client,
    `a 30-line paste produced a ${many.client}px box — it must stop at eight lines (~${8 * lineHeight}px)`,
  ).toBeLessThanOrEqual(8 * lineHeight + 40);
  expect(many.scroll, "past the maximum the box scrolls internally").toBeGreaterThan(many.client);
});

test("only the latest assistant message is a live region, and the log is not", async ({ page, baseURL }) => {
  await stubAPI(page);
  await freshState(page);
  await open(page, baseURL);

  // SCOPED TO THE TRANSCRIPT, not the document. Next.js mounts its own
  // `#__next-route-announcer__` live region (`aria-live="assertive"`) as soon as
  // the router hydrates — measured here, it is present on an EMPTY chat and it
  // has nothing to do with this page. A page-wide `[aria-live]` count would
  // therefore be 1 before the first message and 2 after the answer, i.e. it would
  // red for a reason that is not the card's and would hide a real regression in
  // the transcript behind a fixed offset.
  const log = page.getByRole("group", { name: EN.title });

  // BEFORE any answer: nothing inside the transcript is live. The old page
  // announced the whole log from its first render.
  expect(
    await log.locator("[aria-live]").count(),
    "an empty transcript has nothing to announce",
  ).toBe(0);

  await sendChatPrompt(page, baseURL, "which of my forms are live?");
  await expect(log.locator(LIVE).last()).toContainText(ANSWER, { timeout: 15_000 });

  const regions = await log.locator("[aria-live]").count();
  expect(
    regions,
    "exactly ONE live region: the latest answer. The whole log was `aria-live` before this card, " +
      "so every new message re-read the entire conversation to a screen-reader user",
  ).toBe(1);
  expect(
    await log.locator('[role="log"]').count(),
    'role="log" implies aria-live="polite" (WAI-ARIA 1.2 §5.2.9), so it cannot stay on the transcript',
  ).toBe(0);
  // And the one region is the ANSWER, not a wrapper around the whole transcript.
  await expect(log.locator(LIVE)).toContainText(ANSWER);
});
