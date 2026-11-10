// THE CONVERSATION HISTORY, IN A REAL BROWSER (card t_6b36e0d7).
//
// THE DEFECT THIS PINS. The transcript lived in localStorage and the sign-out
// sweep deleted it, so logging out destroyed every conversation the person had
// had. It is now a server resource, listed in a sidebar the user can re-open,
// rename and delete from.
//
// WHY A BROWSER TEST WHEN THE STORE HAS ITS OWN TESTS. The Go tests prove the
// rows are scoped and stored; the unit tests prove the wire payload is parsed.
// Neither proves the page actually RENDERS the list, re-opens the most recent
// conversation on load, switches when a row is clicked, or creates a second
// conversation without clearing the first. Those are claims about React and the
// DOM together, so each arm drives the page and reads a live element.
//
// The API is stubbed at the network boundary (no Go server, no model, no key) —
// but the STUB IS STATEFUL for the whole test, so "New conversation" really adds
// a row and the old one is really still listed afterwards: a route that returned
// a canned list on every call would render a green page that proves nothing.
import { test, expect, type Page } from "@playwright/test";

import { DICT } from "../src/components/i18nDict";
import { signIn } from "./auth";

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

const EN = {
  history: DICT["chat.history"].en,
  historyEmpty: DICT["chat.historyEmpty"].en,
  newConversation: DICT["chat.newConversation"].en,
  rename: DICT["chat.rename"].en,
  delete: DICT["chat.delete"].en,
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

type Row = { id: string; title: string; updated_at: string; message_count: number; archived: boolean };
type Msg = { id: string; role: "user" | "assistant"; content: string; created_at: string; steps?: unknown[] };

/**
 * A stateful stub of the chat-history API. The store is per-test, so a create or
 * a delete really changes what the next GET returns — that is what makes the
 * "New conversation leaves the old one in the list" arm mean something.
 *
 * The seed is COPIED, not aliased. `TWO_ROWS` is one module-level literal shared
 * by every arm, and the rename arm mutates the row the PATCH finds — with a
 * shallow copy that write landed on the shared seed, so a later arm in the same
 * worker read "Live forms check" where it asserts the original label. Measured
 * on a 10m feature-core run (350 tests, `fullyParallel: true`): the rename arm and
 * the sign-out arm landed in one worker and the sign-out arm failed on a row the
 * rename had renamed. A per-test store must not be able to write through to the
 * seed, so both the rows and their message arrays are copied.
 */
async function stubHistory(page: Page, seed: { rows: Row[]; messages: Record<string, Msg[]> }) {
  const rows = seed.rows.map((r) => ({ ...r }));
  const messages: Record<string, Msg[]> = Object.fromEntries(
    Object.entries(seed.messages).map(([id, turns]) => [id, turns.map((m) => ({ ...m }))]),
  );
  const calls = { post: 0, patch: 0, delete: 0, patched: [] as Array<{ id: string; body: unknown }> };
  let next = 1;

  const json = (body: unknown, status = 200) => ({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));

  // Availability + the exchange itself.
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) => {
    if (r.request().method() === "GET") {
      return r.fulfill(json({ available: true, limits: LIMITS, read_only: true }));
    }
    return r.fulfill(json({ reply: "Two of them are live.", steps: [] }));
  });

  // One conversation (GET) / rename (PATCH) / delete (DELETE).
  await page.route(/(.*)\/api\/rest\/chat\/sessions\/[^/?]+(\?.*)?$/, (r) => {
    const id = new URL(r.request().url()).pathname.split("/").pop()!;
    const method = r.request().method();
    if (method === "PATCH") {
      const body = JSON.parse(r.request().postData() || "{}") as { title?: string };
      calls.patch += 1;
      calls.patched.push({ id, body });
      const row = rows.find((x) => x.id === id);
      if (row && typeof body.title === "string") row.title = body.title;
      return r.fulfill(json({ session: row }));
    }
    if (method === "DELETE") {
      calls.delete += 1;
      const at = rows.findIndex((x) => x.id === id);
      if (at >= 0) rows.splice(at, 1);
      delete messages[id];
      return r.fulfill(json({ status: "deleted" }));
    }
    const row = rows.find((x) => x.id === id);
    if (!row) return r.fulfill(json({ error: "conversation not found" }, 404));
    return r.fulfill(json({ session: row, messages: messages[id] ?? [] }));
  });

  // The collection: list (GET) and create (POST).
  await page.route(/(.*)\/api\/rest\/chat\/sessions(\?.*)?$/, (r) => {
    if (r.request().method() === "POST") {
      calls.post += 1;
      const row: Row = {
        id: `new-${next++}`,
        title: "",
        updated_at: new Date().toISOString(),
        message_count: 0,
        archived: false,
      };
      rows.unshift(row);
      messages[row.id] = [];
      return r.fulfill(json({ session: row }, 201));
    }
    return r.fulfill(json({ sessions: rows }));
  });

  return { rows, messages, calls };
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

/** Wait for hydration (the availability probe), then the history list. */
async function open(page: Page, baseURL: string | undefined) {
  const probed = page
    .waitForResponse((r) => r.request().method() === "GET" && /\/api\/rest\/chat(\?|$)/.test(r.url()), {
      timeout: 20_000,
    })
    .then(() => true)
    .catch(() => false);
  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
  expect(await probed, "the availability probe never answered — the page did not hydrate").toBe(true);
  await expect(page.locator("#chat-input")).toBeEnabled({ timeout: 20_000 });
}

/** The sidebar, scoped so a transcript sentence cannot satisfy a row assertion. */
const sidebar = (page: Page) => page.locator(`aside[aria-label="${EN.history}"]`);
/** One row's clickable label button (the row, not its rename/delete controls). */
const rowButton = (page: Page, label: string) => sidebar(page).getByRole("button", { name: label, exact: true });

const TWO_ROWS: Row[] = [
  { id: "s1", title: "which of my forms are live?", updated_at: "2026-11-07T10:00:00Z", message_count: 2, archived: false },
  { id: "s2", title: "how many sales this week?", updated_at: "2026-11-06T10:00:00Z", message_count: 2, archived: false },
];

const MESSAGES: Record<string, Msg[]> = {
  s1: [
    { id: "m1", role: "user", content: "which of my forms are live?", created_at: "2026-11-07T10:00:00Z" },
    { id: "m2", role: "assistant", content: "Two of them are live: produk-pertama and booking.", created_at: "2026-11-07T10:00:01Z" },
  ],
  s2: [
    { id: "m3", role: "user", content: "how many sales this week?", created_at: "2026-11-06T10:00:00Z" },
    { id: "m4", role: "assistant", content: "Eleven sales came in this week.", created_at: "2026-11-06T10:00:01Z" },
  ],
};

test("the saved conversations are listed and the most recent one is open", async ({ page, baseURL }) => {
  await freshState(page);
  await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);

  // Both rows are in the sidebar, in the server's order.
  await expect(sidebar(page)).toBeVisible();
  const labels = await sidebar(page)
    .locator("li > div > button:first-child")
    .allInnerTexts();
  expect(labels.map((s) => s.trim())).toEqual([
    "which of my forms are live?",
    "how many sales this week?",
  ]);

  // The most recently used conversation is OPEN, with its messages — this is the
  // "log in again and your conversation is there" requirement, measured.
  await expect(page.getByText("Two of them are live: produk-pertama and booking.")).toBeVisible();
  await expect(page.getByText("Eleven sales came in this week.")).toHaveCount(0);
  await expect(rowButton(page, "which of my forms are live?")).toHaveAttribute("aria-current", "true");
});

test("clicking an older conversation continues it", async ({ page, baseURL }) => {
  await freshState(page);
  await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);

  await rowButton(page, "how many sales this week?").click();
  await expect(page.getByText("Eleven sales came in this week.")).toBeVisible();
  await expect(page.getByText("Two of them are live: produk-pertama and booking.")).toHaveCount(0);
  await expect(rowButton(page, "how many sales this week?")).toHaveAttribute("aria-current", "true");
});

test("New conversation creates a second one and leaves the first in the list", async ({ page, baseURL }) => {
  await freshState(page);
  const stub = await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);

  await page.getByRole("button", { name: EN.newConversation, exact: true }).click();

  // The server was asked for a new conversation, the transcript is empty, and
  // BOTH older rows are still listed.
  await expect.poll(() => stub.calls.post).toBe(1);
  await expect(page.getByText("Two of them are live: produk-pertama and booking.")).toHaveCount(0);
  await expect(rowButton(page, "which of my forms are live?")).toBeVisible();
  await expect(rowButton(page, "how many sales this week?")).toBeVisible();
  // The new, unnamed row is labelled rather than blank.
  await expect(rowButton(page, EN.newConversation)).toBeVisible();
});

test("a conversation can be renamed in place", async ({ page, baseURL }) => {
  await freshState(page);
  const stub = await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);

  const row = sidebar(page).locator("li").first();
  await row.getByRole("button", { name: `${EN.rename}: which of my forms are live?` }).click();
  const field = row.getByRole("textbox", { name: EN.rename });
  await field.fill("Live forms check");
  await field.press("Enter");

  await expect.poll(() => stub.calls.patch).toBe(1);
  expect(stub.calls.patched[0]).toEqual({ id: "s1", body: { title: "Live forms check" } });
  await expect(rowButton(page, "Live forms check")).toBeVisible();
});

test("deleting a conversation asks first, then removes it and opens the next one", async ({ page, baseURL }) => {
  await freshState(page);
  const stub = await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);

  page.on("dialog", (d) => void d.accept());
  await sidebar(page)
    .locator("li")
    .first()
    .getByRole("button", { name: `${EN.delete}: which of my forms are live?` })
    .click();

  await expect.poll(() => stub.calls.delete).toBe(1);
  await expect(rowButton(page, "which of my forms are live?")).toHaveCount(0);
  // The next conversation opens, and its messages are the ones on screen.
  await expect(page.getByText("Eleven sales came in this week.")).toBeVisible();
});

test("an empty history says so instead of rendering a blank sidebar", async ({ page, baseURL }) => {
  await freshState(page);
  await stubHistory(page, { rows: [], messages: {} });
  await open(page, baseURL);
  await expect(sidebar(page).getByText(EN.historyEmpty)).toBeVisible();
});

test("signing out and signing back in keeps the conversation", async ({ page, baseURL }) => {
  // THE REPORTED DEFECT, at the client level: signing out used to sweep the
  // browser store, so the next login started from nothing. The conversation is
  // on the server now; the only client behaviour that matters is that the
  // sign-out path deletes NOTHING of it and the next mount reads it back.
  await freshState(page);
  await stubHistory(page, { rows: TWO_ROWS, messages: MESSAGES });
  await open(page, baseURL);
  await expect(page.getByText("Two of them are live: produk-pertama and booking.")).toBeVisible();

  // Sign out through the shell's own control, the way a user does.
  await page.locator(`button:has-text("${DICT["nav.signout"].en}"):visible`).first().click();
  await page.waitForURL(/\/login/, { timeout: 20_000 });

  // Sign back in (the API stub never invalidated the store — that is the point:
  // the server keeps the conversation, and the client must not clear it).
  await signIn(page, baseURL);
  await open(page, baseURL);

  await expect(rowButton(page, "which of my forms are live?")).toBeVisible();
  await expect(page.getByText("Two of them are live: produk-pertama and booking.")).toBeVisible();
});
