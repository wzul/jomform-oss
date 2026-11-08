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
//   * the tool is still NAMED, but in the dashboard's words: the label from
//     `chat.tool.<name>`, never the snake_case identifier the server sends (a
//     refusal block is dashboard UI; card t_ec87d6c7), and a step that was NOT
//     refused is not dragged into the block;
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
 * The tool LABELS the block renders — `chat.tool.<name>` out of the dictionary,
 * NOT the identifiers on the wire (card t_ec87d6c7). A refusal block is dashboard
 * UI: it names the tool in the dashboard's language, and a snake_case identifier
 * is not a word in any of the five locales.
 */
const TOOL_UPDATE = "chat.tool.update_product";
const TOOL_PROPOSE = "chat.tool.propose_write";
const TOOL_GET_FORM = "chat.tool.get_form";
const TOOL_LIST_FORMS = "chat.tool.list_forms";

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
 * `reply` defaults to this file's mixed refusal block and is overridden by the
 * silent-proposal arm below, which needs a different wire entirely.
 */
async function stubAPI(page: Page, lang: string, reply: unknown = REPLY) {
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
      : r.fulfill(json(reply)),
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

      // The tools are still NAMED — in the dashboard's words, not as the raw
      // identifiers the server sent. The identifier is what this block must NOT
      // show: `toolLabel` resolves `chat.tool.<name>` so the name follows the
      // dashboard language (Wan's rule for chat UI, card t_ec87d6c7).
      const lines = block.locator("span.block");
      await expect(
        lines.nth(0),
        `the refused ${JSON.stringify(REPLY.steps[0].tool)} must be named in ${lang}`,
      ).toContainText(DICT[TOOL_UPDATE][lang]);
      await expect(
        lines.nth(1),
        `the refused ${JSON.stringify(REPLY.steps[1].tool)} must be named in ${lang}`,
      ).toContainText(DICT[TOOL_PROPOSE][lang]);
      expect(
        text.includes("update_product") || text.includes("propose_write"),
        `the block prints a raw tool identifier in ${lang}: ${text}`,
      ).toBe(false);

      // A sentence that was not interpolated, or a key that was not resolved,
      // reaches the user as itself: both are the failure this file exists for.
      expect(text.includes("{"), `the block still shows a placeholder in ${lang}: ${text}`).toBe(false);
      expect(text.includes("chat.refused."), `the block shows a raw KEY in ${lang}: ${text}`).toBe(false);

      // The step that was NOT refused is not part of this block: the page filters
      // on status, and a block that lists every step would tell the user the
      // assistant was refused for reading their forms.
      expect(
        text.includes(DICT[TOOL_LIST_FORMS][lang]),
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
    // by a block that failed to render. The tool is named in WORDS: "Product
    // update", not `update_product` (t_ec87d6c7).
    await expect(block).toContainText(DICT["chat.refused"].en);
    await expect(block).toContainText(DICT[KEY_WRITE_TOOL].en);
    await expect(block.locator("span.block").nth(0)).toContainText(DICT[TOOL_UPDATE].en);
    expect(
      text.includes("update_product"),
      `the English block prints the raw tool identifier: ${text}`,
    ).toBe(false);
  });
});

// ── A TOOL-LEVEL REFUSAL IS NOT A PERMISSION REFUSAL (t_66c1f8b1) ──────────────
//
// THE DEFECT, measured live on prod v1.21.250, English dashboard, real model turn.
// The merchant asked to update a form whose id did not exist; the read tool
// `get_form` was ALLOWED and DECLINED, and the wire carried:
//
//     steps: get_form refused (chat.refused.tool), list_forms called
//
// The rendered block was headed "That action is not allowed here." over "The tool
// refused the call, so nothing changed." — telling the merchant the assistant may
// not read their forms when the id was simply wrong. That is the false permission
// claim t_7f1694cb removed for the proposal lane, one refusal class over.
//
// WHAT THIS ARM PINS. The block's heading comes from the STEPS, so only a real
// browser can show which sentence a merchant reads for a given wire. Two wires:
// a block whose only refusal is the declined read (the reported shape, all five
// languages), and the mixed shape QA also measured (a declined read beside a refused
// proposal) — neither contains a forbidden call, so neither may claim one.
const DECLINED_READ_REPLY = {
  reply: "I could not find a form with that id — here are the forms you have.",
  steps: [
    { tool: "get_form", status: "refused", detail_key: "chat.refused.tool" },
    // NOT refused: the block must still filter on status.
    { tool: "list_forms", status: "called" },
  ],
};

const DECLINED_READ_AND_PROPOSAL_REPLY = {
  reply: "I could not read that form, and I could not prepare the change either.",
  steps: [
    { tool: "get_form", status: "refused", detail_key: "chat.refused.tool" },
    { tool: "propose_write", status: "refused", detail_key: "chat.refused.proposal" },
  ],
};

test.describe("a tool-level refusal is not headed as a forbidden action", () => {
  test("the declined-read block says the step failed, never 'not allowed'", async ({ page, baseURL }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang, DECLINED_READ_REPLY);
      await freshState(page, lang);
      await sendChatPrompt(page, baseURL, "rename my product", DICT["chat.send"][lang]);

      const block = page.locator(BLOCK);
      await expect(
        block,
        "the refusal block must render — otherwise this test proves nothing",
      ).toHaveCount(1, { timeout: 20_000 });
      await expect(block.locator("span.block"), "the declined step must render its own line").toHaveCount(1);

      const text = (await block.innerText()).trim();
      console.log(`declined-tool block in ${lang}: ${JSON.stringify(text)}`);

      // The heading is the tool lane's, and NOT the permission claim.
      await expect(block).toContainText(DICT["chat.refusedHeading.step"][lang]);
      expect(
        text.includes(DICT["chat.refused"][lang]),
        `a declined READ is headed "not allowed" in ${lang}: ${text} — get_form IS allowed in ` +
          `chat; the id was wrong (t_66c1f8b1)`,
      ).toBe(false);
      // The step's own why still renders, so the assertion above cannot be satisfied
      // by a block that failed to render.
      await expect(block).toContainText(DICT["chat.refused.tool"][lang]);
      await expect(block.locator("span.block").nth(0)).toContainText(DICT[TOOL_GET_FORM][lang]);
      expect(text.includes("chat.refused"), `the block shows a raw KEY in ${lang}: ${text}`).toBe(false);
      expect(
        text.includes(DICT[TOOL_LIST_FORMS][lang]),
        `the refusal block includes a step that was CALLED, not refused: ${text}`,
      ).toBe(false);
    }
  });

  test("a mixed block of allowed refusals claims neither permission nor a prepared change", async ({
    page,
    baseURL,
  }) => {
    // The shape QA measured on a Malay turn: the same declined read next to a refused
    // proposal. Nothing in it was forbidden, and it is not a change-only block either.
    await stubAPI(page, "en", DECLINED_READ_AND_PROPOSAL_REPLY);
    await freshState(page, "en");
    await sendChatPrompt(page, baseURL, "rename my product", DICT["chat.send"].en);

    const block = page.locator(BLOCK);
    await expect(block, "the refusal block must render").toHaveCount(1, { timeout: 20_000 });
    await expect(block.locator("span.block")).toHaveCount(2);

    const text = (await block.innerText()).trim();
    console.log(`mixed allowed-refusal block: ${JSON.stringify(text)}`);

    await expect(block).toContainText(DICT["chat.refusedHeading.step"].en);
    for (const wrong of [DICT["chat.refused"].en, DICT["chat.refusedHeading.proposal"].en]) {
      expect(
        text.includes(wrong),
        `the mixed block is headed ${JSON.stringify(wrong)}: ${text} — no step in it was ` +
          `forbidden, and not every step is a change`,
      ).toBe(false);
    }
    // Both reasons still render, so the block really is the mixed shape.
    await expect(block).toContainText(DICT["chat.refused.tool"].en);
    await expect(block).toContainText(DICT["chat.refused.proposal"].en);
  });
});

// ── A REFUSED PROPOSAL THAT ENDS WITH NO MODEL PROSE (t_7f1694cb) ─────────────
//
// THE DEFECT, measured live on prod v1.21.149. This is the wire the merchant's
// transcript actually carried for "Create a form called My New Form with one text
// field for the customer's name.":
//
//     steps: propose_write:refused (chat.refused.proposal) x2
//     reply: ""
//
// and the page rendered ONE amber block headed "That action is not allowed here."
// with nothing else — no answer at all. Two separate wrongs, both visible only in a
// rendered DOM:
//
//   1. the heading claimed the ACTION was forbidden, when creating a form is exactly
//      what the chat exists to propose and only the ARGUMENTS were refused; and
//   2. `reply: ""` left the assistant's bubble empty, so the merchant was never
//      told what to send instead.
//
// The server now names a dictionary sentence (`reply_key`) instead of passing the
// silence through (api/internal/chat/chat.go, answerOrKey), and the page picks the
// heading from the steps (src/lib/chatRefusal.ts, refusalHeading). Both halves are
// decided in a language neither the API nor the Go test knows, so only the DOM can
// show what a merchant reads — which is why this arm exists rather than another
// source scan.
const SILENT_PROPOSAL_REPLY = {
  reply: "",
  reply_key: "chat.reply.noAnswer",
  steps: [
    { tool: "propose_write", status: "refused", detail_key: "chat.refused.proposal" },
    { tool: "propose_write", status: "refused", detail_key: "chat.refused.proposal" },
  ],
};

test.describe("a refused proposal with no model prose still answers the merchant", () => {
  test("the block is not headed 'not allowed', and the bubble carries an answer", async ({
    page,
    baseURL,
  }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang, SILENT_PROPOSAL_REPLY);
      await freshState(page, lang);
      await sendChatPrompt(page, baseURL, "Create a form called My New Form", DICT["chat.send"][lang]);

      const block = page.locator(BLOCK);
      await expect(
        block,
        "the refusal block must render — otherwise this test proves nothing",
      ).toHaveCount(1, { timeout: 20_000 });
      await expect(block.locator("span.block"), "the refused steps must each have a line").toHaveCount(2);

      const blockText = (await block.innerText()).trim();
      // The transcript is a `role="group"` named by the page's own title in the
      // CURRENT language (`t("chat.title")`) — not a `role="log"`, which
      // t_148d7100 removed because it implies aria-live and re-announced the
      // whole conversation. Same read, same meaning: the rendered transcript's
      // text, which is what "nothing raw reached the screen" is measured against.
      const transcript = (
        await page.getByRole("group", { name: DICT["chat.title"][lang] }).innerText()
      ).trim();
      console.log(`silent-proposal turn in ${lang}: block=${JSON.stringify(blockText)}`);

      // (1) THE HEADING says the change could not be prepared — in this dashboard's
      // language, and NOT the "not allowed" claim the defect shipped.
      await expect(block).toContainText(DICT["chat.refusedHeading.proposal"][lang]);
      expect(
        blockText.includes(DICT["chat.refused"][lang]),
        `the block still claims the action is not allowed in ${lang}: ${blockText} — creating a ` +
          `form IS proposable; only the arguments were refused (t_7f1694cb)`,
      ).toBe(false);

      // (2) THE ANSWER is the dictionary's sentence, in this language, and it lands
      // in the assistant's own BUBBLE rather than inside the refusal block (a
      // sentence folded into the block would read as part of the refusal).
      const answer = page.locator('div[class*="bg-slate-50"]');
      await expect(
        answer,
        "the assistant's bubble must render — the defect's transcript had the refusal block and " +
          "nothing else",
      ).toHaveCount(1);
      await expect(answer).toContainText(DICT["chat.reply.noAnswer"][lang]);
      console.log(`silent-proposal answer in ${lang}: ${JSON.stringify((await answer.innerText()).trim())}`);
      expect(
        blockText.includes(DICT["chat.reply.noAnswer"][lang]),
        `the answer is rendered inside the refusal block in ${lang}, not as the assistant's own ` +
          `answer: ${blockText}`,
      ).toBe(false);

      // (3) NOTHING RAW. A key that did not resolve reaches the merchant as itself.
      for (const raw of ["chat.reply.", "chat.refused.", "chat.refusedHeading."]) {
        expect(
          transcript.includes(raw),
          `the transcript shows the raw KEY ${raw} in ${lang}:\n${transcript}`,
        ).toBe(false);
      }
    }
  });
});
