// The chat composer on a phone: it must not send an empty box, and its
// placeholder must be READ, not clipped (card t_804040d3).
//
// THE DEFECT THE SWEEP SAW. /chat at 390x844 showed the composer's placeholder
// cut off mid-word — "Ask a question about your…" — because the English string
// was wider than the box a single-row textarea has to draw it in.
//
// MEASURED, both sides, with the composer's own computed font (14px Inter) and
// the canvas measureText the arm below uses — a `next build` of THIS tree at
// 390x844, one fresh browser context per locale, the room read off the live
// `#chat-input`. "old" is the dictionary string before this card, "new" is what
// src/components/i18nDict.ts ships now:
//
//     en  old "Ask a question about your data…"      217.59px in 210px  CLIPPED
//         new "Ask about your data…"                 144.81px in 210px
//     ms  old "Tanya soalan tentang data anda…"      222.51px in 199px  CLIPPED
//         new "Tanya tentang data anda…"             175.27px in 199px
//     ta  old "உங்கள் தரவு பற்றி கேள்வி கேளுங்கள்…"    235.85px in 198px  CLIPPED
//         new "உங்கள் தரவு பற்றி கேளுங்கள்…"           187.91px in 198px
//     zh  "就您的数据提问…"                            110.10px in 216px  fits, unchanged
//     ar  "اطرح سؤالاً عن بياناتك…"                   141.80px in 207px  fits, unchanged
//
// The three long strings are shortened in the dictionary (Tamil drops the
// redundant கேள்வி; the ellipsis stays). The room differs per locale because the
// Send button's own label sets how much is left over — 210px in English, 198px
// in Tamil. zh and ar already fitted and are untouched, so this file measures
// EVERY locale instead of trusting that note: a future longer translation fails
// here rather than in a screenshot nobody takes.
//
// AND THE SEND BUTTON. The card reports Send enabled with an empty textarea.
// Both guards for that already exist on main (`disabled={disabled || !input.trim()}`
// on the button, `if (!text || sending) return;` in sendText, both since the
// feature landed in #651), so this file does not "fix" them — it PINS them, from
// the outside, in a real browser: disabled on load, still disabled for
// whitespace-only text, enabled for real text, disabled again when cleared, and
// an Enter keypress in the empty box must not reach the server at all. That last
// one is the arm the source cannot prove: `onKeyDown` calls `sendText` directly,
// so the only thing standing between an empty Enter and a POST is the guard
// inside sendText.
//
// HOW THE STATE IS REACHED. The page is a dashboard path, so the server's gate
// needs a session (e2e/auth.ts) and every API call is stubbed at the browser
// level — no credential is fabricated and no Go server or database is involved.
// The locale comes from `jf_lang` in localStorage, which is how the real app
// stores it, so the text asserted is the text a user in that language gets.
//
// Run with:
//
//   npx playwright test e2e/a11y-chat-composer.spec.ts --project=a11y
//
// The `a11y` project selects every `a11y-*.spec.ts` by FILENAME PREFIX, so this
// file is in that gate by its name alone (and the `feature-core` project, which
// excludes that prefix, never runs it).
import { test, expect, type Page } from "@playwright/test";

import { signIn } from "./auth";
import { DICT, LANGS } from "../src/components/i18nDict";

const PHONE = { width: 390, height: 844 };

const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: "ws-b", role: "admin" }],
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
 * A cleared browser state in one locale. The browser keeps localStorage between
 * runs; a `jf_lang` left behind by an earlier card once made a page render in a
 * locale its own spec never asserted, so every arm here starts from a cleared
 * one and then states the language it means.
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

/** Stub every API call the chat page makes, and COUNT the message POSTs. */
async function stubAPI(page: Page): Promise<{ posts: string[] }> {
  const posts: string[] = [];
  const json = (body: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  // Catch-all first, specific routes after it: Playwright resolves matching
  // routes in reverse registration order, so the later (specific) ones win.
  await page.route(/(.*)\/api\/.*$/, (route) => route.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat\/sessions(\?.*)?$/, (r) => r.fulfill(json({ sessions: [] })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) => {
    if (r.request().method() === "GET") {
      return r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }));
    }
    posts.push(r.request().postData() ?? "");
    return r.fulfill(json({ reply: "An answer.", proposals: [] }));
  });
  return { posts };
}

/** The composer's parts, named by what a screen reader is given. */
const composer = (page: Page, lang: keyof typeof DICT["chat.send"]) => ({
  box: page.locator("#chat-input"),
  send: page.getByRole("button", { name: DICT["chat.send"][lang] }),
});

test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});

test.describe("chat composer @ 390px", () => {
  test("Send is DISABLED with an empty box — and no keystroke can send one", async ({ page, baseURL }) => {
    await freshState(page, "en");
    const { posts } = await stubAPI(page);
    await page.setViewportSize(PHONE);
    await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

    const { box, send } = composer(page, "en");
    await expect(box).toBeVisible({ timeout: 20_000 });
    await expect(send).toBeVisible();

    // ON LOAD: empty box, disabled button. `toBeDisabled` reads the native
    // `disabled` property, which is what assistive tech announces — a CSS-only
    // "look" of disabled would fail here.
    await expect(box).toHaveValue("");
    await expect(send, "Send is enabled with an empty composer — the card's first defect").toBeDisabled();
    await expect(send).toHaveJSProperty("disabled", true);

    // WHITESPACE IS STILL EMPTY. `!input.trim()` is what makes this true; a bare
    // `!input` would enable the button for a single space.
    await box.fill("   ");
    await expect(send, "Send is enabled for a box holding only spaces").toBeDisabled();

    // AND IT COMES BACK: the guard is live state, not just the first render.
    await box.fill("How many orders yesterday?");
    await expect(send, "Send stays disabled with a real question in the box").toBeEnabled();
    await box.fill("");
    await expect(send, "Send is still enabled after the box was cleared").toBeDisabled();

    // THE ENTER KEY IS THE OTHER DOOR. The textarea's onKeyDown calls sendText()
    // directly, so the empty-box guard inside sendText is the only thing between
    // an idle Enter press and a POST.
    await box.click();
    await page.keyboard.press("Enter");
    await page.waitForTimeout(500); // give a POST time to appear if it were sent
    expect(
      posts,
      `an Enter in the empty composer reached the server: ${JSON.stringify(posts)}. The box must not send an empty ` +
        "question — that was the card's defect.",
    ).toEqual([]);
    // And nothing was added to the transcript as a user turn.
    await expect(page.locator("text=How many orders yesterday?")).toHaveCount(0);
  });

  for (const lang of LANGS) {
    test(`the ${lang} placeholder fits the 390px box instead of being clipped`, async ({ page, baseURL }) => {
      await freshState(page, lang);
      await stubAPI(page);
      await page.setViewportSize(PHONE);
      await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

      const { box } = composer(page, lang as keyof typeof DICT["chat.send"]);
      await expect(box).toBeVisible({ timeout: 20_000 });

      const want = DICT["chat.placeholder"][lang];
      await expect(box, `the ${lang} composer does not carry its dictionary placeholder`).toHaveAttribute(
        "placeholder",
        want,
      );

      // MEASURE IT, in the composer's own font, against the width it really has
      // to draw in. `scrollWidth` cannot answer this — a placeholder is not
      // content — so the string is measured with the same font/padding the
      // browser will use.
      const m = await box.evaluate((el, text) => {
        const cs = getComputedStyle(el);
        const canvas = document.createElement("canvas");
        const ctx = canvas.getContext("2d")!;
        // A placeholder is painted with the element's font, possibly with its own
        // colour; the WIDTH is the font's.
        ctx.font = `${cs.fontStyle} ${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
        const pad = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight);
        const border = parseFloat(cs.borderLeftWidth) + parseFloat(cs.borderRightWidth);
        return {
          textWidth: ctx.measureText(text).width,
          room: el.clientWidth - pad - border,
          fontSize: cs.fontSize,
          scrollsX: el.scrollWidth - el.clientWidth,
        };
      }, want);

      expect(
        m.textWidth,
        `the ${lang} placeholder "${want}" measures ${m.textWidth.toFixed(2)}px but the composer only has ` +
          `${m.room.toFixed(2)}px to draw it in (font ${m.fontSize}) — it is clipped mid-word on a 390px phone. ` +
          "Shorten the string in src/components/i18nDict.ts.",
      ).toBeLessThanOrEqual(m.room);
      // Sanity: the box really is the phone-width composer, not a desktop one.
      expect(m.room, `the composer measured ${m.room.toFixed(0)}px of content width at 390px — wrong surface`).toBeLessThan(
        320,
      );
    });
  }
});
