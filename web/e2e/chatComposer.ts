// THE COMPOSER PROLOGUE FOR THE /chat SPECS — wait for the page to be LIVE, not
// merely painted.
//
// THE DEFECT (card t_b9eea741). `e2e/a11y-chat-card.spec.ts:302` red the gate in
// 1 of 4 full runs on IDENTICAL BYTES, at `openCard`'s Send assertion — while the
// composer assertion immediately above it PASSED, in the same run, and its
// sibling tests through the same helper passed:
//
//     Error: the Send button must be ENABLED before it is clicked
//     expect(locator).toBeEnabled() failed
//     Locator:  getByRole('button', { name: 'Send' })
//     Expected: enabled
//     Received: disabled
//     Timeout:  20000ms
//       - 42 x locator resolved to <button disabled ...>Send</button>
//         at openCard (web/e2e/a11y-chat-card.spec.ts:189:78)
//
// Neither assertion is about a wrong page. The page renders `available` as
// `null` (its useState default, `src/app/chat/page.tsx:144`) and only the
// availability probe's answer moves it, and with `available === null` the
// composer is ENABLED in the server-rendered HTML while Send stays DISABLED
// (`disabled={disabled || !input.trim()}`, `page.tsx:718` — the box is empty):
//
//     $ curl -s http://127.0.0.1:PORT/chat | grep -o '<textarea[^>]*chat-input[^>]*>'
//     <textarea id="chat-input" rows="2" maxLength="2000" ... class="jf-input ...">
//     $ curl -s http://127.0.0.1:PORT/chat | grep -o '<button[^>]*>Send</button>'
//     <button type="submit" disabled="" class="jf-btn-primary disabled:opacity-50">Send</button>
//
// So `toBeVisible` + `toBeEnabled` + `fill` + `toBeEnabled` ALL pass or proceed
// against markup that React has not yet taken over. The `fill` then writes the
// value into a textarea nothing is listening to; hydration lands and RE-IMPOSES
// the controlled value (""), so `input.trim()` stays empty, `sending` stays
// false, and the Send button is disabled — for good, because nothing is
// re-rendering. The composer's own `disabled` stays false, which is exactly what
// the gate log shows (Input: ENABLED, Send: DISABLED).
//
// MEASURED, on the production build of these bytes, with the spec's own stubs
// and the hydration chunk delayed to widen the window (both the raw page and the
// reproduction are on card t_b9eea741):
//
//     composer assertion PASSED; hydrated at that moment = false
//     right after fill   { inputDisabled: false, value: "rename my product", sendDisabled: true }
//     after hydration    { inputDisabled: false, value: "",                   sendDisabled: true }
//     Send isEnabled (20s wait) = false                      <-- the gate's red, on demand
//
// That also explains the run-to-run shape: on an idle host the chunk is cached
// and hydration wins the race (green), and under load the two assertions at :182
// and :189 can both be resolved against SSR markup (red) — same bytes, different
// answer. It is why PR 950's idiom (card t_4221975d) did NOT close it: that fix
// asserted ENABLED before interacting, and on the pre-hydration markup enabled is
// the value you get.
//
// WHY THIS FILE EXISTS RATHER THAN A LOCAL FIX. `e2e/chat-scope-400.spec.ts`
// carries the identical assert-then-fill-then-assert-then-click prologue with the
// identical comment (PR 950 wrote it), so it is racing the same way at the same
// two lines; `e2e/a11y-chat-card.spec.ts` carries a third copy. A race removed at
// one copy is not removed, and a fourth copy is the next flake — so the prologue
// is shared, like `src/lib/workspace.ts` shares the scope rule and
// `src/lib/sendGuard` shares the double-submit rule. The repo has no shared e2e
// module yet; this is the first, and it is deliberately one function.
//
// WHAT IT DOES, AND WHY EACH STEP IS NEEDED
//
//   1. SUBSCRIBE BEFORE NAVIGATING. The availability probe is issued by a
//      `useEffect`, so it cannot exist before React has hydrated and attached the
//      effect — its RESPONSE is the one unambiguous "the page is live" signal
//      available to the test. A `page.waitForResponse` created after `goto` can
//      miss it entirely, which is the same class of race being fixed.
//   2. ASSERT THE PROBE ANSWERED, with a message naming the PROBE. If it never
//      answers, this is not "the card's copy is wrong" and must not be reported
//      as such.
//   3. ASSERT ENABLED, as PR 950 did — a deployment that genuinely answers
//      `available:false` must still red, and this is the assertion that says so.
//   4. FILL, THEN REQUIRE Send ENABLED INSIDE `expect.toPass`. `fill` dispatches
//      a real input event, so on a hydrated page the controlled value and the
//      button follow; the retry exists because hydration can still be in flight
//      for a beat after the fetch resolves, and a retried fill is a wait rather
//      than a red. This is the "make a late re-probe a wait and not a failure"
//      shape the card asked for, applied to the actual race.
//
// It deliberately does NOT raise a timeout: in the gate's red the button was
// still disabled at the END of the full 20s, so elapsed time was never the
// defect — the fill had been discarded and would never be re-applied.
import { expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";

/** The composer's own id — the page's markup, not a class that can drift. */
const COMPOSER = "#chat-input";

/** The availability probe the page's first effect issues. */
const PROBE = /\/api\/rest\/chat(?:\?|$)/;

/** How long the probe may take before the page is declared not-live. */
const PROBE_TIMEOUT = 20_000;

/**
 * Navigate to /chat and send one prompt through the composer, returning once the
 * click has been issued.
 *
 * Every /chat spec that drives the composer must go through this: reaching the
 * page and interacting with it BEFORE hydration is what red the gate at
 * `a11y-chat-card.spec.ts:189` on 1 of 4 identical-byte runs (card t_b9eea741).
 */
export async function sendChatPrompt(
  page: Page,
  baseURL: string | undefined,
  text: string,
): Promise<void> {
  // (1) Registered BEFORE the navigation: the probe is issued by an effect, so
  //     the response can only arrive after hydration — which is the point.
  const probed = page
    .waitForResponse((r) => r.request().method() === "GET" && PROBE.test(r.url()), {
      timeout: PROBE_TIMEOUT,
    })
    .then(() => true)
    .catch(() => false);

  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });

  const box = page.locator(COMPOSER);
  await expect(box, "the composer must be offered when the assistant is available").toBeVisible({
    timeout: 15_000,
  });

  // (2) The page is LIVE, not merely painted. Without this the next two
  //     assertions can both be satisfied by server-rendered markup that React
  //     replaces — which is how this file's defect reproduces.
  expect(
    await probed,
    "the availability probe (GET /api/rest/chat) never answered within " +
      `${PROBE_TIMEOUT}ms. The composer and Send start ENABLED/DISABLED in the server-rendered ` +
      "HTML, so a page whose probe never answers still LOOKS interactable — but it has not " +
      "hydrated, any value typed into it is discarded, and this failure is about the PAGE, not " +
      "about the card's copy.",
  ).toBe(true);

  // (3) A deployment that really answers `available:false` must still red here.
  await expect(
    box,
    "the composer must be ENABLED when the assistant is available — it is only re-disabled by an " +
      "availability answer of `available:false`, which is a real refusal and not this file's race",
  ).toBeEnabled({ timeout: 20_000 });

  const send = page.getByRole("button", { name: DICT["chat.send"].en });

  // (4) Typing through the app, then requiring the button to follow. `fill`
  //     dispatches an input event, so on a hydrated page React sees the change;
  //     the retry covers the beat between the probe's response and hydration
  //     finishing, and a value discarded by a late hydration is re-typed rather
  //     than left to fail. The assertion is inside the retried block on purpose:
  //     a retry that cannot fail is not a guard.
  await expect(async () => {
    await box.fill(text);
    await expect(
      send,
      "the Send button must be ENABLED once the composer holds text — it shares the composer's " +
        "disabled flag, so a disabled Send here means the typed value never reached the input's " +
        "React state (a `fill` applied before hydration is discarded by it)",
    ).toBeEnabled({ timeout: 5_000 });
  }).toPass({ timeout: 20_000 });

  await send.click();
}
