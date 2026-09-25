// Render guard: a notification that failed is SHOWN on the thread, carrying the
// stored error text, and a healthy thread shows no banner.
// THE FIX THIS GUARDS (card t_f29eab32). The notification-failure marker is
// written by dbstore.RecordNotifyFailure, which caps the recorded error at 500
// characters. That cap used to be a BYTE slice (errText[:500]). Error text is
// arbitrary UTF-8 — an accented hostname, a curly quote, an em-dash, a non-ASCII
// server name — so the cut could land inside a multi-byte character and Postgres
// then refused the whole UPDATE ("invalid byte sequence for encoding UTF8",
// SQLSTATE 22021). The consequence is not a shortened message: NO marker is
// written, so the console shows a HEALTHY thread while the filer was told nothing
// — the silent-failure shape the marker exists to prevent. The Go guard
// (api/internal/dbstore/feedback_notify_marker_test.go) proves the row lands;
// this file proves what an operator SEES, which is the half no unit test can show.
//
// WHY A BROWSER TEST. `npm run test:unit` (node --test over src/lib/*.test.ts) has
// no DOM: it cannot prove the banner renders, that it carries the stored text, or
// that the text survives being displayed. The admin feedback surface is the only
// place this marker reaches a human, and it sits behind a super-admin session, so
// the API is stubbed at the browser level (page.route) — the technique the other
// specs in this directory use. No Go server, no session, no database: the spec
// renders the REAL built page and reads the REAL DOM.
//
// It joins the axe gate (`npm run a11y`, part of scripts/preflight.sh) because
// that is the one gate in this repo that serves the built app in a real browser.
// Nothing here asserts on a CSS class — every rule is a property of the rendered
// output, so a re-arrangement that re-introduced the defect would still fail it.
import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

// The thread the tests open. notify_failed_* present = the marker migration 000070
// writes when a notification could not be handed to the queue.
const MARKED = {
  id: "11111111-1111-1111-1111-111111111111",
  subject: "Pembayaran tersekat",
  status: "open",
  user_id: "u-2",
  user_email: "filer@example.test",
  created_at: "2026-09-20T02:00:00Z",
  last_activity_at: "2026-09-20T03:00:00Z",
  message_count: 2,
  // Multi-byte characters (an accented hostname and a non-ASCII server name) so
  // a mangling anywhere on the store → JSON → render path is visible here.
  notify_failed_at: "2026-09-20T03:00:00Z",
  notify_failed_kind: "filer",
  notify_failed_error:
    "enqueue feedback_notify: dial tcp 127.0.0.1:1: connect: connection refused — " +
    "hoszt: példa.example és 中継サーバー",
};

const HEALTHY = {
  id: "22222222-2222-2222-2222-222222222222",
  subject: "Sihat",
  status: "open",
  user_id: "u-3",
  user_email: "lain@example.test",
  created_at: "2026-09-20T02:00:00Z",
  last_activity_at: "2026-09-20T03:00:00Z",
  message_count: 1,
};

// The dashboard bundle: identity + the super-admin flag that gates the Feedback
// section (admin/page.tsx reads bundle.me.is_super_admin).
function bundle() {
  return {
    me: { id: "u-1", email: "ops@example.test", name: "Ops", is_super_admin: true },
    workspaces: [],
  };
}

// Stub the reads the page needs. Written once so every test renders the same
// way, and so anything NOT stubbed (a reply POST, the retry POST) shows up as a
// real network attempt rather than a silent pass.
//
// /api/admin/overview is stubbed too because the Feedback section renders only
// once the overview has loaded (admin/page.tsx: `!data ? Loading…`). Without it
// the page shows "We couldn't load the overview" and no feedback list exists to
// inspect — which would make every assertion below fail for the wrong reason.
async function stubAdmin(p: Page) {
  const list = [MARKED, HEALTHY];
  await p.route("**/api/rest/dashboard-bundle*", (r) =>
    r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(bundle()) }),
  );
  await p.route("**/api/admin/overview", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        totals: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0, media_bytes: 0 },
        growth_24h: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        growth_7d: { workspaces: 0, forms: 0, products: 0, sales: 0, users: 0 },
        email: { delivered: 0, bounce: 0, complaint: 0, open: 0, click: 0 },
        spam: { submissions: 0, sales: 0 },
        recent_signups: [],
        active_workspaces: [],
        storage: [],
        quota_bytes: 0,
      }),
    }),
  );
  await p.route("**/api/admin/dead-letters*", (r) =>
    r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [], total: 0 }) }),
  );
  await p.route("**/api/admin/suppressions*", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ items: [], page: 1, limit: 20, total: 0 }),
    }),
  );
  await p.route("**/api/admin/feedback?*", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: list,
        page: 1,
        limit: 20,
        total: list.length,
        counts: { open: list.length, in_progress: 0, resolved: 0, closed: 0 },
      }),
    }),
  );
  // The thread detail read returns the thread the URL names, so opening the
  // healthy row cannot silently serve the marked one (which would make the
  // "no banner on a healthy thread" assertion pass for the wrong reason).
  await p.route("**/api/admin/feedback/*", (r) => {
    const url = new URL(r.request().url());
    const id = url.pathname.split("/").pop();
    const th = list.find((t) => t.id === id) ?? MARKED;
    const marked = "notify_failed_at" in th;
    return r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        thread: th,
        messages: marked
          ? [
              { id: "m-1", author_kind: "user", body: "pembayaran saya tersekat", created_at: "2026-09-20T02:00:00Z" },
              { id: "m-2", author_kind: "admin", body: "kami sedang menyemak", created_at: "2026-09-20T03:00:00Z" },
            ]
          : [
              { id: "m-3", author_kind: "user", body: "semuanya berjalan lancar", created_at: "2026-09-20T02:00:00Z" },
            ],
      }),
    });
  });
}

test.describe("admin feedback: the notification-failure marker", () => {
  test("a marked thread shows the stored error text and the retry control", async ({ page }) => {
    // FRESH STATE. localStorage and cookies persist between runs on this host,
    // and a leftover jf_lang would render the page in a language this test never
    // asked for (that has already produced a false reading on this board). Clear
    // both, then assert the locale the page actually came up in.
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await page.context().clearCookies();

    await stubAdmin(page);
    await page.goto("/admin", { waitUntil: "domcontentloaded" });

    const storedLang = await page.evaluate(() => window.localStorage.getItem("jf_lang"));
    expect(storedLang, "this run must start with NO stored locale (fresh state)").toBeFalsy();
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    // THE BADGE IS ON EXACTLY THE MARKED THREAD.
    const markedRow = page.getByRole("button", { name: /Pembayaran tersekat/ });
    const healthyRow = page.getByRole("button", { name: /Sihat/ });
    await expect(markedRow).toBeVisible();
    await expect(healthyRow).toBeVisible();
    await expect(healthyRow.getByText("Email failed")).toHaveCount(0);
    await expect(markedRow.getByText("Email failed")).toBeVisible();

    // Open the marked thread: the banner is a role=alert (announced, not just
    // coloured) and it names who was not told.
    await markedRow.click();
    const banner = page.getByRole("alert").filter({ hasText: "Notification not delivered" });
    await expect(banner).toBeVisible();
    await expect(banner).toContainText("The filer was not told");

    // THE REAL ERROR TEXT REACHES THE SCREEN, multi-byte characters intact.
    await expect(banner).toContainText("connection refused");
    await expect(banner).toContainText("példa.example");
    await expect(banner).toContainText("中継サーバー");

    // …and the banner carries the way to act on it.
    await expect(banner.getByRole("button", { name: "Send the notification again" })).toBeVisible();
  });

  test("the marker badge is absent from a thread with no failure", async ({ page }) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await page.context().clearCookies();

    await stubAdmin(page);
    await page.goto("/admin", { waitUntil: "domcontentloaded" });

    // The healthy thread carries no badge…
    const healthyRow = page.getByRole("button", { name: /Sihat/ });
    await expect(healthyRow).toBeVisible();
    await expect(healthyRow.getByText("Email failed")).toHaveCount(0);
    // …and no failure banner, which only exists once a marked thread is open.
    // The banner is scoped to the thread panel, so its absence here is the real
    // assertion (a page-wide text search would also match the marked row's badge
    // tooltip and prove nothing either way).
    await healthyRow.click();
    await expect(page.getByRole("alert").filter({ hasText: "Notification not delivered" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Send the notification again" })).toHaveCount(0);
  });

  // ── defect 3: the two messages cannot contradict each other ──
  //
  // Driven as a BROWSER, against the server state defect 1 can really produce: the
  // retry answers "queued" (a job DID leave the process) while the thread comes back
  // still carrying a marker. Before the fix the console printed "Notification sent
  // again." directly above a banner reading "Notification not delivered — the filer
  // was not told", with nothing to say which one was stale. The unit guard pins the
  // ordering in the source; this proves what an operator actually reads.
  test("a re-sent notification that left the marker set does not claim it was delivered", async ({
    page,
  }) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await page.context().clearCookies();

    await stubAdmin(page);
    // The retry succeeds at the queue but the thread keeps its marker: the server
    // state defect 1 used to create (a differently-kinded marker survives a
    // delivery). The detail read must keep returning the marker AFTER the retry,
    // which is the whole point — so the stub does not mutate.
    await page.route("**/api/admin/feedback/*/notify", (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ notification: "queued", status: "retried" }),
      }),
    );

    await page.goto("/admin", { waitUntil: "domcontentloaded" });
    const markedRow = page.getByRole("button", { name: /Pembayaran tersekat/ });
    await markedRow.click();

    const panel = page.getByRole("alert").filter({ hasText: "Notification not delivered" });
    await expect(panel).toBeVisible();
    await panel.getByRole("button", { name: "Send the notification again" }).click();

    // The confirmation is shown…
    await expect(page.getByText(/Re-sent — but the thread still records a failure/)).toBeVisible();
    // …and it does NOT claim the notification was delivered.
    await expect(page.getByText("Notification sent again.")).toHaveCount(0);
    // …and the banner that says the opposite is still there, as it must be: the
    // marker is still set server-side. The two are now consistent by construction.
    await expect(page.getByRole("alert").filter({ hasText: "Notification not delivered" })).toBeVisible();
  });

  // ── axe on the alert surface itself ──
  //
  // The banner is a role="alert" region added for exactly the operators most
  // likely to need it, and until now no automated scan had ever looked at this
  // page: web/e2e/a11y.spec.ts covers the seven PUBLIC routes only, and the admin
  // feedback surface is behind a super-admin session it cannot provision. This
  // spec already renders that surface against a stubbed API, so the scan costs
  // one test and closes the gap.
  test("the failure banner and its retry control pass axe", async ({ page }) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await page.context().clearCookies();

    await stubAdmin(page);
    await page.goto("/admin", { waitUntil: "domcontentloaded" });

    // Render the surface the scan is about: the marked thread, with the banner and
    // the retry in the DOM. A scan of the list alone would miss both.
    const markedRow = page.getByRole("button", { name: /Pembayaran tersekat/ });
    await expect(markedRow).toBeVisible();
    await markedRow.click();
    const banner = page.getByRole("alert").filter({ hasText: "Notification not delivered" });
    await expect(banner).toBeVisible();
    await expect(banner.getByRole("button", { name: "Send the notification again" })).toBeVisible();

    const results = await new AxeBuilder({ page }).analyze();
    const blockers = results.violations.filter(
      (v) => v.impact === "critical" || v.impact === "serious",
    );
    if (blockers.length > 0) {
      console.log(
        `\n[admin feedback banner] ${blockers.length} critical/serious violation(s):\n` +
          blockers
            .map(
              (v) =>
                `  - ${v.id} (${v.impact}): ${v.help}\n` +
                v.nodes
                  .slice(0, 3)
                  .map((n) => `      ${n.target.join(" ")} — ${n.failureSummary?.split("\n")[0] ?? ""}`)
                  .join("\n"),
            )
            .join("\n"),
      );
    }
    expect(blockers, "the notification-failure banner must have no critical/serious axe violations").toEqual([]);
  });
});
