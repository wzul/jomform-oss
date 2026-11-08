// THE STREAMED ANSWER, IN A REAL BROWSER (card t_148d7100).
//
// THE DEFECT THIS SPEC PINS. The answer used to arrive as one late POST: measured
// on staging, the transcript jumped 45 → 361 characters in a SINGLE frame after
// 2–8 seconds of a static "Looking that up…". A unit test can prove the frame
// parser and it can prove the reducer; neither can prove that the PAGE re-renders
// as fragments arrive. That is a claim about React, the fetch reader and the DOM
// together, so it is measured here, in the browser, the way the card asked:
// sample the live region's `innerText.length` every 100ms and require the number
// to take at least three distinct increasing values during ONE answer.
//
// HOW THE STREAM IS REAL. `route.fulfill` hands the browser a complete body, so a
// test built on it would render the whole answer in one paint and prove nothing
// about streaming. Instead the POST's URL is redirected (route.continue) to a
// local HTTP server in this file that writes each SSE frame with a delay between
// them — chunked, flushed, over a real socket, which is exactly what the API's
// handler does.
//
// The second arm is STOP: the card asks for the request to be stopped in under a
// second with the partial text kept. Both halves are measured — the UI stops
// showing progress, AND the server sees the client go away (otherwise "Stop" is a
// cosmetic button that lets the model keep working and the bill keep running).
import { test, expect, type Page } from "@playwright/test";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

import { DICT } from "../src/components/i18nDict";
import { sendChatPrompt } from "./chatComposer";
import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const EN = {
  send: DICT["chat.send"].en,
  stop: DICT["chat.stop"].en,
  stopped: DICT["chat.stopped"].en,
  copy: DICT["chat.copy"].en,
  jumpLatest: DICT["chat.jumpLatest"].en,
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

type Frame = { at: number; data: unknown };

/** What the stub server writes for the NEXT POST, and when. Replaced per test. */
let plan: { frames: Frame[]; endAt: number } = { frames: [], endAt: 0 };
/** What the stub saw: how many POSTs, and whether the last one was cut off. */
const seen = { posts: 0, aborted: false, authHeader: "" };

let server: Server;
let stubOrigin = "";

test.beforeAll(async () => {
  server = createServer((req, res) => {
    const cors: Record<string, string> = {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Methods": "POST,GET,OPTIONS",
      "Access-Control-Allow-Headers": "*",
    };
    if (req.method === "OPTIONS") {
      res.writeHead(204, cors);
      res.end();
      return;
    }
    seen.posts += 1;
    seen.aborted = false;
    // A client that walks away mid-answer closes the response before it is
    // finished. This is the server-side half of "Stop really stops": the API's
    // handler relies on exactly this signal to stop the runner.
    res.on("close", () => {
      if (!res.writableFinished) seen.aborted = true;
    });
    res.writeHead(200, {
      ...cors,
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache",
      Connection: "keep-alive",
    });
    for (const f of plan.frames) {
      setTimeout(() => {
        if (!res.writableEnded) res.write(`data: ${JSON.stringify(f.data)}\n\n`);
      }, f.at);
    }
    setTimeout(() => {
      if (!res.writableEnded) res.end();
    }, plan.endAt);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  stubOrigin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((r) => server.close(() => r()));
});

/** Every dashboard call stubbed; the chat POST is redirected to the streaming stub. */
async function stubAPI(page: Page) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  // Catch-all FIRST; Playwright resolves the most recently registered route first.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  // A well-formed conversation row. The page creates one on the FIRST send and
  // treats a body it cannot read as a storage failure ("A new conversation could
  // not be started."), which is a state a real deployment does not show — the
  // catch-all stub used to answer `{}` here, so this spec drove the page with an
  // alert rendered over the transcript. Stubbed so the arm measures the scroll
  // contract alone.
  const session = (n: number) => ({
    id: "s-e2e-1",
    title: "",
    updated_at: "2026-01-01T00:00:00Z",
    message_count: n,
    archived: false,
  });
  await page.route(/(.*)\/api\/rest\/chat\/sessions(\?.*)?$/, (r) =>
    r.request().method() === "POST"
      ? r.fulfill(json({ session: session(0) }))
      : r.fulfill(json({ sessions: [session(2)] })),
  );
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, async (r) => {
    if (r.request().method() === "GET") {
      return r.fulfill(json({ available: true, limits: LIMITS, read_only: true }));
    }
    // THE REAL STREAM: the browser's own request, sent to a server that writes
    // frame by frame. `continue` does not buffer the body, so the page sees each
    // frame as it is flushed — which is the thing being measured.
    return r.continue({ url: `${stubOrigin}/v1/rest/chat?stream=1` });
  });
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

/**
 * The last assistant message — the ONLY live region on the page (the whole log
 * carried `aria-live` before this card, which made a screen reader re-read the
 * conversation).
 */
const LIVE = '[aria-live="polite"]';

test("the answer grows at least three times while it is being written", async ({ page, baseURL }) => {
  await stubAPI(page);
  await freshState(page);

  // Frames ~300ms apart, the whole answer over ~1.2s: long enough that a
  // one-shot render cannot produce three distinct sizes, short enough to keep the
  // spec quick.
  plan = {
    frames: [
      { at: 150, data: { type: "step", tool: "list_forms" } },
      { at: 350, data: { type: "delta", text: "You have " } },
      { at: 650, data: { type: "delta", text: "3 forms " } },
      { at: 950, data: { type: "delta", text: "live." } },
      { at: 1200, data: { type: "done", result: { reply: "You have 3 forms live.", steps: [] } } },
    ],
    endAt: 1300,
  };

  await sendChatPrompt(page, baseURL, "which of my forms are live?");

  // Sample the live region every 100ms while the answer arrives. `innerText`
  // (not textContent) is what a user sees.
  const samples = await page.evaluate(async () => {
    const out: number[] = [];
    for (let i = 0; i < 40; i++) {
      const nodes = document.querySelectorAll<HTMLElement>('[aria-live="polite"]');
      const last = nodes[nodes.length - 1];
      out.push(last ? last.innerText.trim().length : -1);
      await new Promise((r) => setTimeout(r, 100));
    }
    return out;
  });

  const growing = samples.filter((n, i) => i > 0 && n > samples[i - 1]);
  expect(
    growing.length,
    `the answer grew ${growing.length} time(s) across one response — samples: ${JSON.stringify(samples)}. ` +
      `The card requires at least 3: a single jump from short to long is the ORIGINAL defect ` +
      `(45 → 361 characters in one frame), and a page that renders one chunk proves the parser ` +
      `ran but not that the reader can read while the model writes.`,
  ).toBeGreaterThanOrEqual(3);

  // And it still ENDS with the answer, cursor gone.
  const live = page.locator(LIVE).last();
  await expect(live).toContainText("You have 3 forms live.");
  await expect(live).not.toContainText("▍");
});

test("Stop cuts the request off in under a second and keeps the partial answer", async ({ page, baseURL }) => {
  await stubAPI(page);
  await freshState(page);

  // A long answer: a fragment every 300ms for ten seconds. Without a working Stop
  // this test would run for ten seconds and keep the model busy for all of them.
  const frames: Frame[] = [];
  for (let i = 0; i < 30; i++) {
    frames.push({ at: 200 + i * 300, data: { type: "delta", text: `part${i} ` } });
  }
  plan = { frames, endAt: 12_000 };

  await sendChatPrompt(page, baseURL, "tell me something long");

  // Wait until text is actually on screen, so "partial text kept" has a subject.
  await expect(page.locator(LIVE).last()).not.toHaveText("", { timeout: 10_000 });

  const stop = page.getByRole("button", { name: EN.stop });
  await expect(stop, "a Stop control must be offered while an answer is in flight").toBeVisible();

  const started = Date.now();
  await stop.click();

  // The marker is the visible proof that the answer is incomplete.
  await expect(
    page.getByText(EN.stopped).first(),
    "after Stop the transcript must say the answer is incomplete — a partial answer that looks " +
      "finished is worse than no answer",
  ).toBeVisible({ timeout: 5_000 });
  const elapsed = Date.now() - started;
  expect(elapsed, `Stop took ${elapsed}ms to settle; the card requires under 1000ms`).toBeLessThan(1000);

  // The controls went back to send-mode, and the partial text is still there.
  await expect(page.getByRole("button", { name: EN.stop })).toHaveCount(0);
  const kept = (await page.locator(LIVE).last().innerText()).replace(EN.stopped, "").trim();
  expect(kept.length, "the partial answer was deleted by Stop — it is the only thing the user waited for").toBeGreaterThan(0);

  // THE REQUEST REALLY STOPPED, not just the spinner: the server saw the client
  // close the response before it finished writing.
  await expect
    .poll(() => seen.aborted, {
      timeout: 3_000,
      message:
        "the stub server never saw the client disconnect, so the browser kept the connection open " +
        "and the API would have kept the model running (and billing) behind a stopped answer",
    })
    .toBe(true);
});

test("a server that answers one JSON body still renders the answer (the fallback)", async ({ page, baseURL }) => {
  // A buffering proxy, an older deployment, or a runner with no streaming
  // capability: the page must render it exactly as before rather than show an
  // empty transcript.
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: LIMITS }))
      : r.fulfill(
          json({
            reply: "You have 3 forms live.",
            steps: [{ tool: "list_forms", status: "called" }],
          }),
        ),
  );
  await freshState(page);

  await sendChatPrompt(page, baseURL, "which of my forms are live?");

  await expect(page.locator(LIVE).last()).toContainText("You have 3 forms live.");
  // The step that RAN is in the transcript (before this card, only refused steps
  // were rendered at all).
  await expect(page.getByText(DICT["chat.step.read"].en.replace("{thing}", DICT["chat.noun.forms"].en))).toBeVisible();
  // And a copy control exists on the finished answer.
  await expect(page.getByRole("button", { name: EN.copy }).first()).toBeVisible();
});

test("a reader who scrolled up is not pulled down, and Jump to latest brings them back", async ({
  page,
  baseURL,
}) => {
  await stubAPI(page);
  await freshState(page);

  // A LONG answer, so the transcript really overflows its box and "scrolled up"
  // is a place the reader can be. Short answers leave the log under its own
  // height, where `isAtBottom` is true by definition and this arm would pass for
  // the wrong reason.
  //
  // The fragments are 350ms apart (last one at ~8.3s) so the answer is still
  // arriving well after the transcript has overflowed and the reader has
  // scrolled up: the "did not move" assertion below needs fragments arriving
  // WHILE the reader is up, and a tail that ends as soon as the box fills would
  // make that claim true by vacuum on a loaded host.
  const sentence =
    "This answer is deliberately long so that the transcript has somewhere to scroll, which is the " +
    "only condition under which staying scrolled up can be observed at all. ";
  const frames: Frame[] = [];
  for (let i = 0; i < 24; i++) {
    frames.push({ at: 250 + i * 350, data: { type: "delta", text: sentence } });
  }
  frames.push({
    at: 250 + 24 * 350,
    data: { type: "done", result: { reply: sentence.repeat(24).trim(), steps: [] } },
  });
  plan = { frames, endAt: 250 + 25 * 350 };

  await sendChatPrompt(page, baseURL, "tell me something long");

  const log = page.getByRole("group", { name: EN.title });
  await expect(log).toBeVisible({ timeout: 15_000 });

  // Wait until the answer has filled the box — otherwise "the view did not move"
  // is trivially true.
  await expect
    .poll(
      () =>
        log.evaluate((el) => el.scrollHeight - el.clientHeight),
      { timeout: 15_000, message: "the transcript never overflowed, so this arm has nothing to scroll" },
    )
    .toBeGreaterThan(100);

  // The reader scrolls UP, mid-answer. Assigning scrollTop fires the same scroll
  // event a wheel or a drag would, which is how the page learns they left the
  // bottom (lib/chatScroll.ts).
  //
  // THE RETRY IS THE POINT (this arm was red inside the full parallel suite and
  // green standalone). The browser dispatches that scroll event on a later
  // frame, while the page's follow effect runs on every fragment — so a single
  // assignment can be overwritten by a fragment that lands in that one-frame gap,
  // and a lost assignment never comes back because nothing else moves the
  // reader. Re-assigning until the page has registered it is a WAIT for the
  // property under test, not a weaker assertion: the control must still appear,
  // and every position assertion below is unchanged.
  const jump = page.getByRole("button", { name: EN.jumpLatest });
  await expect(async () => {
    await log.evaluate((el) => {
      el.scrollTop = 0;
    });
    await expect(
      jump,
      "after the reader scrolled up, the way back must be offered — staying put must not be a dead end",
    ).toBeVisible({ timeout: 1_500 });
  }).toPass({ timeout: 20_000 });

  const before = await log.evaluate((el) => ({
    top: el.scrollTop,
    len: (el as HTMLElement).innerText.length,
  }));
  expect(before.top, "the reader was not actually at the top of the transcript").toBeLessThan(2);

  // Let the rest of the answer arrive while the reader stays up. THE DEFECT: the
  // old page called scrollIntoView on every change, so this is exactly where it
  // yanked the reader away from the answer they had scrolled up to read.
  //
  // The window is measured in CONTENT, not in clock time: wait for at least one
  // more fragment to reach the transcript, and watch the reader's position the
  // whole time. A fixed sleep would let a slow host finish the answer before the
  // arm started looking, and "the transcript moved 0px" would then be true of a
  // page that never got the chance to move it.
  let pulledTo = 0;
  await expect
    .poll(
      async () => {
        const now = await log.evaluate((el) => ({
          top: el.scrollTop,
          len: (el as HTMLElement).innerText.length,
        }));
        if (now.top > before.top + 1) pulledTo = now.top;
        return now.len > before.len ? 1 : 0;
      },
      {
        timeout: 20_000,
        message:
          "no further fragment reached the transcript while the reader was scrolled up, so this arm " +
          "has nothing to measure — the answer stopped arriving before the scroll-up",
      },
    )
    .toBe(1);

  const after = await log.evaluate((el) => el.scrollTop);
  expect(
    pulledTo,
    `the page scrolled a reader who was reading something else to ${pulledTo}px while fragments kept ` +
      "arriving — the follow effect must not move a scrolled-up reader",
  ).toBe(0);
  expect(
    Math.abs(after - before.top),
    `the transcript moved ${Math.abs(after - before.top)}px while the reader was scrolled up ` +
      `(scrollTop ${before.top} -> ${after}) — the page must not follow the answer for a reader who ` +
      "is reading something else",
  ).toBeLessThanOrEqual(1);

  // And the way back really works: pinned again, and at the end of the answer.
  await jump.click();
  await expect
    .poll(() => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight), { timeout: 5_000 })
    .toBeLessThanOrEqual(48);
  await expect(jump, "once back at the bottom the control has nothing left to offer").toHaveCount(0);
});
