// THE CONFIRMATION CARD'S OWN WORDS FOLLOW THE DASHBOARD LANGUAGE (card t_30ffa28a).
//
// THE DEFECT, as the QA lane measured it on v1.12.0 in a real browser (English
// dashboard, fresh storage, `lang=en`):
//
//     NEEDS YOUR CONFIRMATION   Kemas kini produk
//     Medan produk yang disenaraikan di bawah akan ditukar. Baki medan kekal seperti sedia ada.
//     Produk   QA Renamed Product …
//     Nama     QA Renamed Product t_567b594c
//     Cadangan menukar nama produk kepada QA Renamed Product …
//     update_product
//     [Confirm] [Cancel]   Nothing has changed yet.
//
// The badge, the buttons and the hint were English (they come from the five-locale
// dictionary); the title, the summary, every field label and the composed value
// were Malay literals built by the SERVER (api/internal/chat/proposal.go). The API
// does not receive the caller's language on a JSON call (issue #6177), so those
// sentences were one language on every dashboard — on the one surface where getting
// the text right IS the safety mechanism.
//
// WHAT THIS SPEC PINS, and why it is a browser test. The halves of this card live in
// two languages and two repos: the server decides the WORDS (as dictionary keys) and
// the page decides the LANGUAGE. A source scan cannot show what a merchant actually
// reads for a given `jf_lang`; only the rendered DOM can. So the real page is driven
// in a real browser with the API stubbed at the network boundary — no Go server, no
// session, no credential (the pattern e2e/a11y-chat-card.spec.ts uses) — and the
// card's text is READ OUT OF THE DOM and compared against the dictionary:
//
//   * in each of the five languages the card renders the dictionary's own string;
//   * the composed value ("{n} times") is interpolated, not shown as a placeholder;
//   * the merchant's own data and the assistant's note are shown VERBATIM (they are
//     not the server's words, and translating them would be a lie);
//   * on the English dashboard no Malay sentence from the defect survives.
//
// A guard whose subject never rendered proves nothing, so the card's presence and
// its two field rows are asserted before anything is read.
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
import { DICT, LANGS, type Lang } from "../src/components/i18nDict";
import { sendChatPrompt } from "./chatComposer";

const TOKEN = "tok-card-lang";
const WS = "ws-b";

/** The card's own markup: the sky-tinted proposal box. */
const CARD = '[class*="border-sky-200"]';

/** The keys the server sends, and the English text the defect got wrong. */
const TITLE_KEY = "chat.proposal.title.update_product";
const SUMMARY_KEY = "chat.proposal.summary.update_product";
const LABEL_NAME = "chat.proposal.label.name";
const LABEL_CHARGES = "chat.proposal.label.subscriptionCharges";
const VALUE_CHARGES = "chat.proposal.value.chargeCount";

/** The merchant's own data, and the assistant's own wording — both shown as sent. */
const PRODUCT_NAME = "Tee 2026 (XL)";
const NOTE = "Cadangan menukar nama produk kepada Tee 2026 (XL).";

const PROPOSAL = {
  token: TOKEN,
  args_hash: "hash-card-lang",
  tool: "update_product",
  title_key: TITLE_KEY,
  summary_key: SUMMARY_KEY,
  fields: [
    { label_key: LABEL_NAME, value: PRODUCT_NAME },
    { label_key: LABEL_CHARGES, value_key: VALUE_CHARGES, value_params: { n: "12" } },
  ],
  note: NOTE,
  expires_at: "2027-01-01T00:00:00Z",
  reversible: true,
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
 * Every call the page makes, with the card on the reply.
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
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [PROPOSAL] })),
  );
}

/**
 * A FRESH browser state in one language: cleared storage, one workspace selected.
 *
 * The host keeps localStorage between runs, and a `jf_lang` left by an earlier run
 * renders a locale this test did not ask for — the same trap the QA lane hit when a
 * page rendered Arabic because a previous card had left `jf_lang=ar` behind.
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

/** The card's rows as the browser shows them: label → value, in DOM order. */
async function cardRows(page: Page): Promise<Array<[string, string]>> {
  return page.locator(CARD).evaluate((card) =>
    [...card.querySelectorAll("dt")].map((dt, i) => [
      (dt.textContent || "").trim(),
      ((card.querySelectorAll("dd")[i]?.textContent) || "").trim(),
    ]),
  );
}

/** Open the card and return it, after proving it really rendered. */
async function openCard(page: Page, baseURL: string | undefined, lang: Lang) {
  // The Send button's accessible name is localized too, so the shared prologue is
  // told which language this page is in (see e2e/chatComposer.ts).
  await sendChatPrompt(page, baseURL, "rename my product", DICT["chat.send"][lang]);
  const card = page.locator(CARD).first();
  await expect(card, "the proposal card must render — otherwise this test proves nothing").toHaveCount(
    1,
    { timeout: 20_000 },
  );
  await expect(card.locator("dt"), "the card's two field rows must render").toHaveCount(2);
  await expect(card.getByText(PROPOSAL.tool)).toBeVisible();
  return card;
}

test.describe("the confirmation card's own words follow the dashboard language", () => {
  test("the card renders the DICTIONARY's string in each of the five languages", async ({
    page,
    baseURL,
  }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang);
      await freshState(page, lang);
      const card = await openCard(page, baseURL, lang);

      const title = DICT[TITLE_KEY][lang];
      const summary = DICT[SUMMARY_KEY][lang];
      const rows = await cardRows(page);

      // The card's heading and its summary: the dictionary's own string, in the
      // language the profile asked for.
      await expect(card).toContainText(title);
      await expect(card).toContainText(summary);
      expect(
        rows,
        `the card's field rows in ${lang} — a label that is not the dictionary's string means the ` +
          `server's own wording reached the page again`,
      ).toEqual([
        [DICT[LABEL_NAME][lang], PRODUCT_NAME],
        [DICT[LABEL_CHARGES][lang], DICT[VALUE_CHARGES][lang].replace("{n}", "12")],
      ]);

      console.log(`card in ${lang}: ${JSON.stringify(title)} / ${JSON.stringify(rows)}`);
    }
  });

  test("the English card carries none of the Malay the defect shipped", async ({ page, baseURL }) => {
    await stubAPI(page, "en");
    await freshState(page, "en");
    const card = await openCard(page, baseURL, "en");

    const text = (await card.innerText()).trim();
    console.log(`the English card reads:\n${text}`);

    // The two sentences the QA lane photographed on an English dashboard. They are
    // the server's old literals; neither has any business on this card any more.
    for (const malay of [
      DICT["chat.proposal.title.update_product"].ms, // "Kemas kini produk"
      DICT["chat.proposal.summary.update_product"].ms,
      DICT["chat.proposal.label.name"].ms, // "Nama"
      DICT["chat.proposal.label.subscriptionCharges"].ms,
      DICT["chat.proposal.value.chargeCount"].ms.replace("{n}", "12"), // "12 kali"
    ]) {
      expect(
        text.includes(malay),
        `the English card shows the Malay server wording ${JSON.stringify(malay)} — that is the defect ` +
          `(a merchant who does not read Malay being asked to confirm a change described in it). ` +
          `Card text:\n${text}`,
      ).toBe(false);
    }

    // Everything ELSE on the card is still there, so the assertions above cannot be
    // satisfied by a card that failed to render: the localized chrome, the tool
    // name, the buttons, the hint — and the two strings that are deliberately NOT
    // translated.
    await expect(card).toContainText(DICT["chat.proposalBadge"].en);
    await expect(card.getByRole("button", { name: DICT["chat.confirm"].en })).toBeVisible();
    await expect(card.getByRole("button", { name: DICT["chat.cancel"].en })).toBeVisible();
    await expect(card).toContainText(DICT["chat.confirmHint"].en);
    // The merchant's own data, verbatim.
    await expect(card).toContainText(PRODUCT_NAME);
    // The assistant's note, verbatim: it is the model's own wording in the
    // CONVERSATION's language (here Malay, as the QA run's was), not the server's,
    // and it stays exactly as written rather than being looked up in a dictionary.
    await expect(card).toContainText(NOTE);
  });
});
