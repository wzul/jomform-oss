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
//   * a count of ONE renders the SINGULAR entry — English "1 time", not "1 times"
//     (t_212be8da: the count values had no singular form, and English is the default
//     dashboard language);
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

// The SINGULAR entries of the count values (t_212be8da). The server picks between
// them by n (api/internal/chat/proposal.go, countedValue), so a card whose count is
// one arrives with these keys and no parameters at all.
const VALUE_CHARGES_ONE = "chat.proposal.value.chargeCountOne";
const VALUE_VARIATION_ONE = "chat.proposal.value.variationSetOne";
const VALUE_FIELD_ONE = "chat.proposal.value.fieldCountOne";
const VALUE_ITEM_ONE = "chat.proposal.value.itemCountOne";
const LABEL_PRODUCT = "chat.proposal.label.product";
const LABEL_VARIATIONS = "chat.proposal.label.variations";
const LABEL_CONFIG = "chat.proposal.label.config";
const LABEL_NOTIFICATIONS = "chat.proposal.label.notifications";

/** The merchant's own data, and the assistant's own wording — both shown as sent. */
const PRODUCT_NAME = "Tee 2026 (XL)";
const PRODUCT_ID = "11111111-2222-3333-4444-555555555555";
const NOTE = "Cadangan menukar nama produk kepada Tee 2026 (XL).";

/** One card as the page receives it, typed structurally so a fixture with a count
 *  of ONE (which carries NO value_params — the singular entry states the one) is the
 *  same type as the plural one. */
type ProposalFixture = {
  token: string;
  args_hash: string;
  tool: string;
  title_key: string;
  summary_key: string;
  fields: Array<{
    label_key: string;
    label_raw?: string;
    value?: string;
    value_key?: string;
    value_params?: Record<string, string>;
  }>;
  note?: string;
  expires_at: string;
  reversible: boolean;
};

const PROPOSAL: ProposalFixture = {
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

/**
 * The tool name as the DASHBOARD renders it, per language (card t_ec87d6c7).
 *
 * `ProposalFixture.tool` is what the SERVER sends on the wire; it is not what the
 * merchant reads. A proposal card is dashboard UI and may not print a snake_case
 * identifier, so `page.tsx` resolves `chat.tool.<name>` through `toolLabel` and
 * renders the dictionary's label in the dashboard's language. These are the same
 * `DICT` entries the card resolves, so the guard follows all five locales.
 */
const TOOL_LABEL: Record<string, Record<Lang, string>> = {
  update_product: DICT["chat.tool.update_product"],
  upsert_form: DICT["chat.tool.upsert_form"],
};

/**
 * The same tool with every count at ONE: what the server sends for a product whose
 * variation set has a single row and whose subscription is charged once. The counts
 * arrive as the SINGULAR key with no value_params — which is why the card cannot say
 * "1 rows" / "1 times" (t_212be8da).
 */
const PRODUCT_ONE_PROPOSAL: ProposalFixture = {
  token: TOKEN,
  args_hash: "hash-card-one-product",
  tool: "update_product",
  title_key: TITLE_KEY,
  summary_key: SUMMARY_KEY,
  fields: [
    { label_key: LABEL_PRODUCT, value: PRODUCT_ID },
    { label_key: LABEL_VARIATIONS, value_key: VALUE_VARIATION_ONE },
    { label_key: LABEL_CHARGES, value_key: VALUE_CHARGES_ONE },
  ],
  note: NOTE,
  expires_at: "2027-01-01T00:00:00Z",
  reversible: true,
};

/** A form creation card counting ONE config field and ONE notification: the two
 *  counts describeFormCreation renders as fieldCount / itemCount. */
const FORM_ONE_PROPOSAL: ProposalFixture = {
  token: TOKEN,
  args_hash: "hash-card-one-form",
  tool: "upsert_form",
  title_key: "chat.proposal.title.upsert_form",
  summary_key: "chat.proposal.summary.upsert_form",
  fields: [
    { label_key: "chat.proposal.label.action", value_key: "chat.proposal.value.createForm" },
    { label_key: LABEL_CONFIG, value_key: VALUE_FIELD_ONE },
    { label_key: LABEL_NOTIFICATIONS, value_key: VALUE_ITEM_ONE },
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
async function stubAPI(page: Page, lang: string, proposal: ProposalFixture = PROPOSAL) {
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
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [proposal] })),
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
async function openCard(page: Page, baseURL: string | undefined, lang: Lang, proposal: ProposalFixture = PROPOSAL) {
  // The Send button's accessible name is localized too, so the shared prologue is
  // told which language this page is in (see e2e/chatComposer.ts).
  await sendChatPrompt(page, baseURL, "rename my product", DICT["chat.send"][lang]);
  const card = page.locator(CARD).first();
  await expect(card, "the proposal card must render — otherwise this test proves nothing").toHaveCount(
    1,
    { timeout: 20_000 },
  );
  const wantRows = proposal.fields.length;
  await expect(card.locator("dt"), `the card's ${wantRows} field rows must render`).toHaveCount(wantRows);
  const label = TOOL_LABEL[proposal.tool]?.[lang];
  expect(
    label,
    `the fixture's tool ${proposal.tool} has no chat.tool.* label — the card would fall back to ` +
      `a humanised English name in ${lang}, and this guard would be pinning the wrong string`,
  ).toBeTruthy();
  // The tool stays NAMED on the card — in the dashboard's words. Located by its
  // NODE, not by bare text: in `ms` this label equals the card's title
  // ("Kemas kini produk"), so a text query would match two elements.
  await expect(
    card.locator("p.font-mono"),
    `the card must name the tool as ${JSON.stringify(label)} in ${lang}`,
  ).toHaveText(label);
  expect(
    (await card.innerText()).includes(proposal.tool),
    `the card prints the raw tool identifier ${proposal.tool} on an ${lang} dashboard — a card ` +
      `is dashboard UI and may not print snake_case (t_ec87d6c7)`,
  ).toBe(false);
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

  // ── A COUNT OF ONE (t_212be8da). ────────────────────────────────────────────
  //
  // The count values had ONE form per key, so English — the dashboard's DEFAULT
  // language — read "1 fields" / "1 rows" / "1 times" on the card the merchant is
  // asked to confirm. The server now sends the SINGULAR key when the count is one
  // (api/internal/chat/proposal.go, countedValue). What the merchant actually READS
  // for that key is decided on this side of the wire, which is why it is read out of
  // the rendered DOM here rather than asserted against the wire.
  //
  // Two cards, because the four counts come from two tools: a product (one variation,
  // one charge) and a form creation (one config field, one notification entry).
  test("a product card whose counts are ONE reads the singular form in each language", async ({
    page,
    baseURL,
  }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang, PRODUCT_ONE_PROPOSAL);
      await freshState(page, lang);
      await openCard(page, baseURL, lang, PRODUCT_ONE_PROPOSAL);
      const rows = await cardRows(page);
      expect(
        rows,
        `the one-count product card in ${lang} — the SINGULAR entries must be rendered, and the ` +
          `count entries (which interpolate {n}) are what the card must NOT use at n=1`,
      ).toEqual([
        [DICT[LABEL_PRODUCT][lang], PRODUCT_ID],
        [DICT[LABEL_VARIATIONS][lang], DICT[VALUE_VARIATION_ONE][lang]],
        [DICT[LABEL_CHARGES][lang], DICT[VALUE_CHARGES_ONE][lang]],
      ]);
      console.log(`one-count product card in ${lang}: ${JSON.stringify(rows)}`);

      if (lang === "en") {
        // The English sentences, in the dashboard's DEFAULT language — the one the
        // defect was measured in ("1 rows — …" / "1 times" were the pre-fix copy).
        expect(rows[1][1], "the English card still counts rows in the plural").toMatch(/^1 row, /);
        expect(rows[2][1], "the English card still counts charges in the plural").toBe("1 time");
      }
    }
  });

  test("a form creation card whose counts are ONE reads the singular form in each language", async ({
    page,
    baseURL,
  }) => {
    for (const lang of LANGS) {
      await stubAPI(page, lang, FORM_ONE_PROPOSAL);
      await freshState(page, lang);
      await openCard(page, baseURL, lang, FORM_ONE_PROPOSAL);
      const rows = await cardRows(page);
      expect(
        rows,
        `the one-count form card in ${lang} — "Configuration" / "Notifications" must read the ` +
          `SINGULAR entries at n=1`,
      ).toEqual([
        [DICT["chat.proposal.label.action"][lang], DICT["chat.proposal.value.createForm"][lang]],
        [DICT[LABEL_CONFIG][lang], DICT[VALUE_FIELD_ONE][lang]],
        [DICT[LABEL_NOTIFICATIONS][lang], DICT[VALUE_ITEM_ONE][lang]],
      ]);
      console.log(`one-count form card in ${lang}: ${JSON.stringify(rows)}`);

      if (lang === "en") {
        // The English copy, stated as the sentence the merchant reads — and the
        // pre-fix plural, stated as the one that must NOT be on the card. This is
        // the default dashboard language, the one the defect was measured in.
        expect(rows[1][1], "the English card still counts fields in the plural").toBe("1 field");
        expect(rows[2][1], "the English card still counts items in the plural").toBe("1 item");
      }
    }
  });
});
