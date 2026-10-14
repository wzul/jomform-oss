// A REFUSED TOOL CALL'S WHY FOLLOWS THE DASHBOARD LANGUAGE (card t_3839995c).
//
// THE DEFECT, as QA measured it on v1.12.0 in a real browser (English dashboard,
// fresh storage, `lang=en`):
//
//     That action is not allowed here.
//     update_product — alat "update_product" tidak dibenarkan dalam chat ini
//
// The heading came from the five-locale dictionary; the sentence after the em dash
// was the SERVER's own Malay — `steps[].detail`, built in api/internal/chat/chat.go
// (the allowlist refusal, "RALAT: " plus an internal error, the proposal lane's
// message). The API does not receive the caller's language on a JSON call (issue
// #6177), so that sentence reached every locale in one language, on a page whose
// every other failure sentence (chat.fail / chat.confirmFail /
// chat.proposalArgsInvalid) goes through the dictionary.
//
// WHAT THIS SPEC PINS, and why it is a browser test. The halves live in two
// languages and two repos: the server decides the KEY (a machine code) and the page
// decides the LANGUAGE. A source scan cannot show what a merchant actually reads for
// a given `jf_lang`; only the rendered DOM can. So the real page is driven in a real
// browser with the API stubbed at the network boundary — no Go server, no session,
// no credential (the pattern e2e/chat-card-language.spec.ts and
// e2e/a11y-chat-card.spec.ts use) — and the refusal block's text is READ OUT OF THE
// DOM and compared against the dictionary:
//
//   * in each of the five languages the block renders the dictionary's own strings
//     for BOTH steps (a bare key and a key with parameters);
//   * the parameters are interpolated, not shown as a placeholder;
//   * the tool name stays the identifier it is, and a step that was NOT refused is
//     not dragged into the block;
//   * on the English dashboard no Malay sentence from the defect survives.
//
// A guard whose subject never rendered proves nothing, so the block and its two
// step lines are asserted BEFORE anything is read out of it.
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page renders.
// This spec visits one, so it declares the session it means — see e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
import { DICT, LANGS, type Lang } from "../src/components/i18nDict";
import { sendChatPrompt } from "./chatComposer";

const WS = "ws-b";

/** The refusal block's own markup: the amber-tinted box. */
const BLOCK = 'div[class*="bg-amber-50"]';

/** The keys the server sends — the same literals as api/internal/chat/chat.go. */
const KEY_WRITE_TOOL = "chat.refused.writeTool";
const KEY_PROPOSAL_LIMIT = "chat.refused.proposalLimit";

/**
 * The Malay the defect shipped. These are the SERVER's old literals (not the
 * dictionary's Malay, which is asserted separately): neither has any business in an
 * English transcript any more.
 */
const SHIPPED_MALAY = [
  'alat "update_product" tidak dibenarkan dalam chat ini',
  "tidak boleh dilaksanakan oleh chat",
  "had cadangan untuk satu mesej dicapai",
];

/** What the model attempted, as the reply reports it. */
const REPLY = {
  reply: "There is one thing I could not do for you — ask me again for the change.",
  steps: [
    { tool: "update_product", status: "refused", detail_key: KEY_WRITE_TOOL },
    {
      tool: "propose_write",
      status: "refused",
      detail_key: KEY_PROPOSAL_LIMIT,
      detail_params: { max: "3" },
    },
    // NOT refused: the block must not pick this one up, and its absence is what
    // makes the filter (and not just the copy) part of the assertion.
    { tool: "list_forms", status: "called" },
  ],
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

/**
 * Every call the page makes, with the refused steps on the reply.
 *
 * `lang` is what the profile answers: the dashboard language is per-PROFILE, and
 * the page takes it from there (localStorage is only the instant first paint).
 */
async function stubAPI(page: Page, lang: string) {
  const json = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }))
      : r.fulfill(json(REPLY)),
  );
}

/**
 * A FRESH browser state in one language: cleared storage, one workspace selected.
 *
 * The host keeps localStorage between runs, and a `jf_lang` left by an earlier run
 * renders a locale this test did not ask for — the trap the QA lane hit when a page
 * rendered Arabic because a previous card had left `jf_lang=ar` behind.
 */
async function freshState(page: Page, lang: string) {
  await page.addInitScript((l) => {
    try {
      localStorage.clear();
      sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b");
      localStorage.setItem("jf_lang", l);
    } catch {
      /* storage unavailable */
    }
  }, lang);
}

/** Open the transcript and return the refusal block, after proving it rendered. */
async function openRefusal(page: Page, baseURL: string | undefined, lang: Lang) {
  // The Send button's accessible name is localized too, so the shared prologue is
  // told which language this page is in (see e2e/chatComposer.ts).
  await sendChatPrompt(page, baseURL, "rename my product", DICT["chat.send"][lang]);
  const block = page.locator(BLOCK);
  await expect(
    block,
    "the refusal block must render — otherwise this test proves nothing",
  ).toHaveCount(1, { timeout: 20_000 });
  await expect(
    block.locator("span.block"),
    "the refused steps must each render their own line",
  ).toHaveCount(2);
  return block;
}

test.describe("a refused tool call's why follows the dashboard language", () => {
  test("the block renders the DICTIONARY's strings in each of the five languages", async ({
    page,
    baseURL,
  }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang);
      await freshState(page, lang);
      const block = await openRefusal(page, baseURL, lang);

      const text = (await block.innerText()).trim();
      console.log(`refusal block in ${lang}: ${JSON.stringify(text)}`);

      // The heading, and BOTH sentences — one keyed plainly, one interpolating a
      // number into the middleware's own sentence.
      await expect(block).toContainText(DICT["chat.refused"][lang]);
      await expect(block).toContainText(DICT[KEY_WRITE_TOOL][lang]);
      await expect(block).toContainText(DICT[KEY_PROPOSAL_LIMIT][lang].replace("{max}", "3"));

      // The tool names stay the identifiers they are — never translated, never
      // hidden by the sentence.
      await expect(block).toContainText("update_product");
      await expect(block).toContainText("propose_write");

      // A sentence that was not interpolated, or a key that was not resolved,
      // reaches the user as itself: both are the failure this file exists for.
      expect(text.includes("{"), `the block still shows a placeholder in ${lang}: ${text}`).toBe(false);
      expect(text.includes("chat.refused."), `the block shows a raw KEY in ${lang}: ${text}`).toBe(false);

      // The step that was NOT refused is not part of this block: the page filters
      // on status, and a block that lists every step would tell the user the
      // assistant was refused for reading their forms.
      expect(
        text.includes("list_forms"),
        `the refusal block includes a step that was CALLED, not refused: ${text}`,
      ).toBe(false);
    }
  });

  test("the English block carries none of the Malay the defect shipped", async ({ page, baseURL }) => {
    await stubAPI(page, "en");
    await freshState(page, "en");
    const block = await openRefusal(page, baseURL, "en");

    const text = (await block.innerText()).trim();
    console.log(`the English refusal block reads:\n${text}`);

    for (const malay of SHIPPED_MALAY) {
      expect(
        text.includes(malay),
        `the English block shows the server's own Malay wording ${JSON.stringify(malay)} — that is ` +
          `the defect (a merchant reading a dashboard they chose in English, being told why their ` +
          `change was refused in a language they may not read). Block text:\n${text}`,
      ).toBe(false);
    }
    // And the dictionary's Malay must not leak into the English page either — the
    // server used to answer in one language for every locale, and a page that
    // resolved the ENGLISH sentence from a Malay entry would be the same defect
    // wearing a key.
    for (const key of [KEY_WRITE_TOOL, KEY_PROPOSAL_LIMIT] as const) {
      expect(
        text.includes(DICT[key].ms),
        `the English block shows ${key}'s MALAY string — the locale resolution is inverted`,
      ).toBe(false);
    }

    // Everything else is still there, so the assertions above cannot be satisfied
    // by a block that failed to render.
    await expect(block).toContainText(DICT["chat.refused"].en);
    await expect(block).toContainText(DICT[KEY_WRITE_TOOL].en);
    await expect(block).toContainText("update_product");
  });
});
