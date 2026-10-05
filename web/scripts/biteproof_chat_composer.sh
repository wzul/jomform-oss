#!/usr/bin/env bash
# Bite proof for the /chat composer-hydration race in the /chat e2e specs
# (card t_b9eea741).
#
# THE DEFECT THE SHARED PROLOGUE REMOVES. `e2e/a11y-chat-card.spec.ts:302` red the
# a11y gate in 1 of 4 full runs on IDENTICAL BYTES, at openCard's Send assertion:
#
#     Error: the Send button must be ENABLED before it is clicked
#     expect(locator).toBeEnabled() failed
#     Expected: enabled   Received: disabled   Timeout: 20000ms
#       - 42 x locator resolved to <button disabled type="submit" ...>Send</button>
#
# while the composer assertion immediately ABOVE it passed, in the same run. The
# page renders `available` as null (its useState default), so the SERVER-RENDERED
# composer is ALREADY ENABLED while Send is DISABLED (the box is empty):
#
#     $ curl -s http://127.0.0.1:PORT/chat | grep -o '<button[^>]*>Send</button>'
#     <button type="submit" disabled="" class="jf-btn-primary disabled:opacity-50">Send</button>
#
# so the old prologue — visible -> enabled -> fill -> enabled -> click — ALL runs
# against markup React has not taken over. The `fill` is applied to a textarea no
# listener owns; hydration then RE-IMPOSES the controlled value (""), the input
# state stays empty, and Send is disabled for the whole 20s on a page that is in
# fact correct. That is why PR 950's fix (t_4221975d) did not close it: asserting
# ENABLED before interacting is satisfied by the pre-hydration markup.
#
# WHAT THIS HARNESS PROVES. It widens the pre-hydration window (stalls the first
# /_next/static JS chunk), then runs the REMOVED pre-fix bytes and the SHIPPED
# shared prologue against the SAME stalled page:
#
#   baseline   the two shipped /chat specs are GREEN — not a tautology
#   case 1     the pre-fix prologue, re-created byte-for-byte from what this card
#              deleted, must go RED at the NAMED Send assertion (the card's own
#              failure, on demand instead of 1-in-4)
#   case 2     e2e/chatComposer.ts's `sendChatPrompt` on the same stalled page
#              must go GREEN — the other half of the same measurement
#
# The defect is TEST-side, so there is no page mutation to make: the change under
# proof IS the spec-side prologue. Case 1 is therefore the deleted bytes, and the
# pair of cases is the measurement that says the deletion was load-bearing. The
# harness mutates nothing in the tree: the fixture spec is created and removed, and
# the two shipped specs are sha-checked unchanged at the end.
#
# Run:  bash web/scripts/biteproof_chat_composer.sh
# Env:  A11Y_PORT (default 3397) — playwright.config.ts's webServer serves there.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

SPEC_A=e2e/a11y-chat-card.spec.ts
SPEC_B=e2e/chat-scope-400.spec.ts
SELF=e2e/zzbiteproof-chat-composer.spec.ts
PLAYWRIGHT=./node_modules/.bin/playwright
export A11Y_PORT="${A11Y_PORT:-3397}"

cleanup() { rm -f "$SELF"; }
trap cleanup EXIT

sha_a_before="$(sha256sum "$SPEC_A")"
sha_b_before="$(sha256sum "$SPEC_B")"
fail=0

rebuild() {
  echo "   (npm run build — the specs measure the BUILT page)"
  if ! npm run build >/tmp/biteproof-b9eea741-build.log 2>&1; then
    echo "   BUILD FAILED — see /tmp/biteproof-b9eea741-build.log"
    return 1
  fi
}

# Verdict rule from biteproof_forms_bulk_failure.sh, and for the same reason
# (card t_dd211ae1): the check is scoped to the reporter's own Error block, never
# the whole output, because Playwright ECHOES the spec's source as a `>` context
# line — so every string in an assertion message is printed whatever the test died
# of, and an unscoped search would accept a timeout as the named assertion.
run_case() {
  local name="$1" want="$2" expect="$3" args="$4"
  echo "== $name"
  local out
  out="$($PLAYWRIGHT test $args --reporter=list 2>&1)"
  local error_block
  error_block="$(grep -E '^ +Error: |^ +expect\(|^ +Received|^ +Expected|^ +Locator:|^ +Timeout:|^ +Call log:|^ +at ' <<<"$out")"
  if [ "$want" = pass ]; then
    if grep -qE "^ +[0-9]+ passed" <<<"$out" && ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
      grep -E "^ +[0-9]+ passed" <<<"$out" | sort -u | head -1
      echo "   OK (green: every case in this spec passed)"
    else
      echo "   NOT GREEN — a red on a tree with no defect, so no red below could be attributed to the removed bytes:"
      grep -E "passed|failed|^ *Error: " <<<"$out" | sort -u | head -8
      fail=1
    fi
    return
  fi
  if ! grep -qE "^ +[0-9]+ failed" <<<"$out"; then
    echo "   NOT DETECTED — the pre-fix prologue stayed green, so the harness reproduced nothing"
    grep -E "passed|failed" <<<"$out" | tail -3
    fail=1
  elif ! grep -qE "$expect" <<<"$error_block"; then
    echo "   DETECTED, but NOT at a named assertion. Wanted one of: $expect"
    grep -E "^ +Error: " <<<"$out" | sort -u | head -5
    fail=1
  else
    grep -E "^ +[0-9]+ failed" <<<"$out" | sort -u | head -1
    grep -E "$expect" <<<"$error_block" | head -1 | cut -c1-160
    echo "   OK (red at the named assertion)"
  fi
}

echo
echo "== baseline + case 1: build once, then the shipped specs and the pre-fix prologue"
rebuild || exit 2
run_case "0. the two shipped /chat specs pass (green control)" pass "" "$SPEC_A $SPEC_B"

# CASE 1 + CASE 2 share one fixture file whose TWO tests are selected by name, so
# there is a single build and a single server. Both run with the first JS chunk
# stalled by 4s, which is what turns the 1-in-4 race into a deterministic one.
cat > "$SELF" <<'SPEC'
// FIXTURE — created and removed by web/scripts/biteproof_chat_composer.sh.
// Case 1 is the prologue card t_b9eea741 DELETED (verbatim, from
// a11y-chat-card.spec.ts:176-192 / chat-scope-400.spec.ts:236-264). Case 2 is the
// shared prologue that replaced it.
import { test, expect, type Page } from "@playwright/test";
import { DICT } from "../src/components/i18nDict";
import { sendChatPrompt } from "./chatComposer";

const EN = { send: DICT["chat.send"].en };
const PROPOSAL = {
  token: "tok-card-1", args_hash: "hash-card-1", tool: "update_product",
  title: "Rename the product", summary: "Tee 2026 → Tee 2026 (XL)",
  fields: [{ label: "Name", value: "Tee 2026 (XL)" }, { label: "Price", value: "RM25.00" }],
  expires_at: "2027-01-01T00:00:00Z", reversible: true,
};
const BUNDLE = {
  me: { id: "u1", email: "merchant@example.com", name: "Merchant", lang: "", is_super_admin: false },
  workspaces: [{ id: "ws-id-b", name: "Second", slug: "ws-b", role: "admin" }],
  chip_status: null, keys: [], domains: [], webhooks: [], social: [], einvoice: null, products: [],
  summary: { counts: {}, revenue_sen: 0 }, sales: { items: [], page: 1, limit: 20, total: 0 },
  form_analytics: [], product_analytics: [],
};
async function stub(page: Page) {
  const json = (b: unknown) => ({ status: 200, contentType: "application/json", body: JSON.stringify(b) });
  await page.route(/(.*)\/api\/.*$/, (r) => r.fulfill(json({})));
  await page.route(/(.*)\/api\/rest\/dashboard-bundle(\?.*)?$/, (r) => r.fulfill(json(BUNDLE)));
  await page.route(/(.*)\/api\/auth\/me$/, (r) => r.fulfill(json(BUNDLE.me)));
  await page.route(/(.*)\/api\/auth\/profile$/, (r) => r.fulfill(json({ lang: "" })));
  await page.route(/(.*)\/api\/rest\/chat(\?.*)?$/, (r) =>
    r.request().method() === "GET"
      ? r.fulfill(json({ available: true, limits: { messages_per_hour: 60, max_message_chars: 2000 } }))
      : r.fulfill(json({ reply: "Here is the change I would make.", proposals: [PROPOSAL] })));
  await page.route(/(.*)\/api\/rest\/chat\/(confirm|cancel)(\?.*)?$/, (r) =>
    r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ error: "x" }) }));
}
async function stallHydration(page: Page, ms: number) {
  let left = ms;
  await page.route(/\/_next\/static\/.*\.js(\?.*)?$/, async (route) => {
    const d = left; left = 0;
    if (d > 0) await new Promise((r) => setTimeout(r, d));
    await route.continue();
  });
}
async function fresh(page: Page) {
  await page.addInitScript(() => {
    try { localStorage.clear(); sessionStorage.clear();
      localStorage.setItem("jf_workspace", "ws-b"); localStorage.setItem("jf_lang", "en");
    } catch { /* storage unavailable */ }
  });
}

// THE PRE-FIX SHAPE — the bytes this card removed from both /chat specs.
test("PRE-FIX shape on a hydration-stalled page", async ({ page, baseURL }) => {
  await stub(page);
  await fresh(page);
  await stallHydration(page, 4000);
  await page.goto(`${baseURL}/chat`, { waitUntil: "domcontentloaded" });
  const box = page.locator("#chat-input");
  await expect(box, "the composer must be offered when the assistant is available").toBeVisible({ timeout: 15_000 });
  await expect(
    box,
    "the composer must be ENABLED when the assistant is available — it starts disabled until the " +
      "availability probe lands, so interacting before that waits out the whole test timeout",
  ).toBeEnabled({ timeout: 20_000 });
  await box.fill("rename my product");
  const send = page.getByRole("button", { name: EN.send });
  await expect(
    send,
    "the Send button must be ENABLED before it is clicked — it shares the composer's disabled flag",
  ).toBeEnabled({ timeout: 20_000 });
  await send.click();
});

// THE SHIPPED SHAPE on the same stalled page.
test("SHARED shape on the same hydration-stalled page", async ({ page, baseURL }) => {
  await stub(page);
  await fresh(page);
  await stallHydration(page, 4000);
  await sendChatPrompt(page, baseURL, "rename my product");
  await expect(
    page.locator(".border-sky-200").filter({ hasText: PROPOSAL.title }),
    "the proposal card must render — the prompt really went through",
  ).toHaveCount(1, { timeout: 20_000 });
});
SPEC

run_case "1. the PRE-FIX prologue reds at the named Send assertion" red \
  'the Send button must be ENABLED before it is clicked' \
  "$SELF --grep PRE-FIX"

run_case "2. the SHARED prologue is green on the same stalled page" pass "" \
  "$SELF --grep SHARED"

echo
cleanup
sha_a_after="$(sha256sum "$SPEC_A")"
sha_b_after="$(sha256sum "$SPEC_B")"
if [ "$sha_a_before" != "$sha_a_after" ] || [ "$sha_b_before" != "$sha_b_after" ]; then
  echo "RESTORE FAILED — a shipped spec is not byte-identical:"
  echo "  $SPEC_A before: $sha_a_before"
  echo "  $SPEC_A after : $sha_a_after"
  echo "  $SPEC_B before: $sha_b_before"
  echo "  $SPEC_B after : $sha_b_after"
  exit 2
fi
if [ -e "$SELF" ]; then
  echo "CLEANUP FAILED — the fixture spec is still on disk: $SELF"
  exit 2
fi
echo "clean: no repo file mutated; the fixture spec is removed"
echo "   $sha_a_after"
echo "   $sha_b_after"
if [ $fail -eq 0 ]; then echo "ALL CASES OK"; else echo "SOME CASES FAILED"; fi
exit $fail
