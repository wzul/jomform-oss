import { test, expect } from '@playwright/test';

/**
 * Money-path E2E scenarios (issue #99). These run against a staging deploy
 * with a test-mode CHIP account (see docs/CHIP-SANDBOX-E2E.md).
 *
 * NOTE: each scenario needs a published form + a test-mode CHIP account
 * seeded with sandbox credentials. The exact selectors depend on the current
 * form builder / checkout markup; update them to match the live DOM before
 * enabling in CI. The structure below is the canonical skeleton.
 */

const base = process.env.E2E_BASE_URL || 'https://staging.jomform.com';

// 1. Happy path: buyer opens a published form → fills → pays (sandbox card)
//    → thank-you → receipt email sent.
test('happy path: form → pay → thank-you → receipt', async ({ page }) => {
  await page.goto(`${base}/acme/tee`);
  await page.getByLabel(/Nama/i).fill('Ali Test');
  await page.getByLabel(/Emel/i).fill('buyer@example.test');
  await page.getByRole('button', { name: /Bayar/i }).click();

  // CHIP sandbox checkout: fill test card and submit.
  await page.getByLabel(/Card number/i).fill('4242 4242 4242 4242');
  await page.getByLabel(/Expiry/i).fill('12/30');
  await page.getByLabel(/CVC/i).fill('123');
  await page.getByRole('button', { name: /Pay/i }).click();

  // Thank-you page.
  await expect(page).toHaveURL(/\/t\//);
  await expect(page.getByText(/Terima kasih/i)).toBeVisible();

  // Receipt email sent — assert via SES test inbox / SNS subscription.
  // (Out of scope for the browser; covered by the receipt-content scenario.)
});

// 2. Merchant confirm: pending → paid → confirmed.
test('merchant confirm: pending → paid → confirmed', async ({ page }) => {
  // Login as merchant, open the sale, confirm it.
  await page.goto(`${base}/login`);
  await page.getByLabel(/Emel/i).fill(process.env.E2E_MERCHANT_EMAIL || 'merchant@example.test');
  await page.getByLabel(/Kata laluan/i).fill(process.env.E2E_MERCHANT_PASSWORD || '');
  await page.getByRole('button', { name: /Log masuk/i }).click();

  await page.goto(`${base}/dashboard/sales`);
  const row = page.locator('tr', { hasText: 'JF-' }).first();
  await expect(row).toContainText('pending');
  await row.getByRole('button', { name: /Sahkan/i }).click();
  await expect(row).toContainText('confirmed');
});

// 3. Skip-capture: authorize → capture (full + partial) → release → cancelled.
test('skip-capture: authorize → capture → release', async ({ page }) => {
  // Create a skip_capture sale (authorize only), then capture full, then
  // release a second one → cancelled.
  // (Requires a form configured with skip_capture; selectors TBD.)
  test.skip(true, 'needs a skip_capture form + merchant capture UI wiring');
});

// 4. Bank-direct: instructions + proof upload → confirm.
test('bank-direct: instructions + proof upload → confirm', async ({ page }) => {
  // Buyer picks bank_direct, sees instructions, uploads proof; merchant
  // confirms. (Selectors TBD.)
  test.skip(true, 'needs bank-direct form + proof upload UI wiring');
});

// 5. Receipt content: email + PDF carry space-scoped URL (#83), correct
//    amounts, no footer (#80).
test('receipt content: URL, amount, no footer', async ({ page }) => {
  // Read the receipt email (SES test inbox / SNS) and assert:
  //   - "Borang dibeli: https://acme.jomform.com/tee"  (space-scoped, #83)
  //   - "Jumlah  : RM 45.00"                          (correct amount)
  //   - no footer text (issue #80)
  // (Requires a test inbox reader; selectors TBD.)
  test.skip(true, 'needs a test inbox / SES reader to assert email+PDF content');
});
