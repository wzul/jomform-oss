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

import { signIn } from "./auth";

// The dashboard paths are gated on the SERVER (web/src/middleware.ts): a request
// without the `jf_session` cookie is redirected to /login BEFORE the page
// renders. This spec visits one, so it declares the session it means — before
// the gate existed an anonymous visit rendered the account shell, so this spec
// could (and did) rely on the bug. See e2e/auth.ts.
test.beforeEach(async ({ page, baseURL }) => {
  await signIn(page, baseURL!);
});
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

// ── the case the fix is actually about: text truncated AT the cap ──
//
// MARKED above is ~110 characters, so it never reaches feedbackNotifyErrorMax and
// no multi-byte character in it sits anywhere near byte 500: it proves the
// renderer does not mangle multi-byte text ANYWHERE, but nothing about the one
// shape the rune-aware cap exists for. Text that is longer than the cap and
// multi-byte exactly where the cap falls is what used to make Postgres refuse the
// whole UPDATE (SQLSTATE 22021) — silently, so the console showed a healthy thread.
//
// This constant is the STORED value, not the input, and that distinction is the
// point. dbstore.RecordNotifyFailure caps at 500 CHARACTERS (`[]rune(errText)`),
// so for CAP_ERROR_INPUT the store keeps 499 "a"s plus the ONE "中" that fits at
// index 499 and drops the other two, together with the entire tail. Measured on
// the shipped store: input runes=534 bytes=540 → stored runes=500 bytes=502, the
// last 12 characters being "aaaaaaaaaaa中". Feeding the spec the untruncated
// input would assert about a string the API never serves, and would stay green
// against a store that had lost the rune boundary. The Go guard
// (api/internal/dbstore/feedback_notify_marker_test.go,
// TestRecordNotifyFailureKeepsARuneBoundaryOnAMultiByteError) keeps that stored
// shape true; this file makes the RENDERED half match it.
//
// Built with "a".repeat rather than written out, so the byte offset of the cut is
// a declared property of the fixture instead of something a reader has to count.
const CAP = 500;
// The INPUT the store was given: one "a" short of the cap, then three multi-byte
// runes and a tail. The cap falls ON the first of those three runes, which is the
// whole point of the fixture.
const CAP_INPUT_TAIL = "tail-after-the-cap";
const CAP_ERROR_INPUT = "a".repeat(CAP - 1) + "中中中" + CAP_INPUT_TAIL;
// What RecordNotifyFailure STORES for CAP_ERROR_INPUT: the first 500 CHARACTERS,
// so 499 "a"s and exactly one 中 — the two remaining 中 and the entire tail are
// discarded. Asserted against the input at the top of the test below, so neither
// constant can be edited into a pair the store would never produce.
const CAP_ERROR_STORED = "a".repeat(CAP - 1) + "中";
// What a byte-slice cap (errText[:500]) produces for the input above: the same
// text up to byte 500, then a HALF-WRITTEN 中 (one 0xe4, one 0xbf). Deliberately
// invalid UTF-8 — this is the shape that reached the console as a mangled glyph
// when the truncation was the bug. It is asserted ABSENT so that a mangled render
// cannot satisfy the assertions below by accident.
const CAP_ERROR_TAIL_BYTES_MANGLED = "aaaaaaaaa\ufffd";

const AT_CAP = {
  id: "33333333-3333-3333-3333-333333333333",
  subject: "Had hadapan",
  status: "open",
  user_id: "u-4",
  user_email: "cap@example.test",
  created_at: "2026-09-20T02:00:00Z",
  last_activity_at: "2026-09-20T03:00:00Z",
  message_count: 2,
  notify_failed_at: "2026-09-20T03:00:00Z",
  notify_failed_kind: "filer",
  notify_failed_error: CAP_ERROR_STORED,
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
  const list = [MARKED, HEALTHY, AT_CAP];
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
  // An id that is in no list is a 404 rather than a fallback: serving MARKED for
  // an unknown id would let the at-the-cap test below assert against MARKED's
  // short error text and pass while rendering nothing it meant to render.
  await p.route("**/api/admin/feedback/*", (r) => {
    const url = new URL(r.request().url());
    const id = url.pathname.split("/").pop();
    const th = list.find((t) => t.id === id);
    if (!th) {
      return r.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "no such thread in this fixture" }),
      });
    }
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
    await signIn(page);

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

  // ── the cap itself: text truncated AT 500 characters by a multi-byte rune ──
  //
  // This is the shape the rune-aware cap exists for and the shape nothing above
  // renders: an error text longer than the cap, multi-byte exactly where the cut
  // falls. It is asserted against the STORE's value (AT_CAP), not an invented
  // string, because the property under test is that the renderer shows what the
  // store wrote — the rune that survived at the boundary included — with no
  // further mangling, and because a value the store cannot produce would keep
  // this test green against a store that had lost the rune boundary.
  test("error text the store capped at 500 characters renders to the cap, rune intact", async ({
    page,
  }) => {
    // FRESH STATE, same reasoning as the first test: a leftover jf_lang would
    // render this page in a language this test never asked for.
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await signIn(page);

    await stubAdmin(page);
    await page.goto("/admin", { waitUntil: "domcontentloaded" });
    await expect(page.locator("html")).toHaveAttribute("lang", "en");

    const capRow = page.getByRole("button", { name: /Had hadapan/ });
    await expect(capRow).toBeVisible();
    await capRow.click();

    const banner = page.getByRole("alert").filter({ hasText: "Notification not delivered" });
    await expect(banner).toBeVisible();

    // The fixture is the STORE's value, and this asserts it is: the input is what
    // the caller handed RecordNotifyFailure, the stored value is its first 500
    // CHARACTERS. Without this, either constant could drift into a pair the store
    // never produces and the assertions below would be about a fiction. Measured
    // on the shipped store: input runes=534 bytes=540 → stored runes=500 bytes=502
    // (the Go guard keeps the stored runes at the cap;
    // TestRecordNotifyFailureKeepsARuneBoundaryOnAMultiByteError).
    expect([...CAP_ERROR_INPUT].length).toBeGreaterThan(CAP);
    expect([...CAP_ERROR_INPUT].slice(0, CAP).join("")).toBe(CAP_ERROR_STORED);

    // The text is the STORED value, whole: 499 "a"s and the 中 that fits at the
    // boundary. `toContainText` normalises whitespace only — it does not repair
    // mojibake — so a banner that rendered this string differently fails here.
    await expect(banner).toContainText(CAP_ERROR_STORED);

    // Read the one paragraph that carries the error text (the banner's heading,
    // who-line and retry label are separate nodes), so the assertions below are
    // about the error text itself and cannot be satisfied by other parts of the
    // banner. Asserting it is exactly one node also proves the value was not
    // split across lines on the way out.
    const errLine = banner.locator("p").filter({ hasText: CAP_ERROR_STORED });
    await expect(errLine, "exactly one paragraph renders the stored error text").toHaveCount(1);
    const errText = await errLine.innerText();

    // It ENDS exactly with the stored value: the characters past the cap were
    // trimmed away, so nothing follows the boundary rune. A renderer that
    // appended anything, that re-expanded the trimmed tail, or that cut the text
    // somewhere else would not end here.
    expect(errText.endsWith(CAP_ERROR_STORED)).toBe(true);
    // …and it rendered the whole value rather than a fragment of it.
    expect(errText.length).toBeGreaterThanOrEqual(CAP);

    // A byte-slice cap ends 中 one byte short; a renderer that inherited that
    // shape (or that mangled the boundary on the way to the DOM) would produce
    // this instead. Requiring it to be ABSENT is what stops a mangled render from
    // satisfying the assertions above by accident.
    expect(errText).not.toContain(CAP_ERROR_TAIL_BYTES_MANGLED);

    // Every character at and after the boundary is intact UTF-8: no U+FFFD in
    // what an operator reads.
    expect(errText).not.toContain("\ufffd");

    // Still actionable, which is the other half of the marker's contract.
    await expect(banner.getByRole("button", { name: "Send the notification again" })).toBeVisible();
  });

  test("the marker badge is absent from a thread with no failure", async ({ page }) => {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await signIn(page);

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
    await signIn(page);

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
    await signIn(page);

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

  // ── the OUTCOME's tone, not just its sentence (card t_01385c19) ─────────────
  //
  // THE DEFECT THESE ARMS PIN
  //   The Feedback section has TWO outcome lines: `fbMsg` (plain emerald text —
  //   the success line) and `fbActionErr` (a red `role="alert"`). Both writers
  //   branched on `result.kind === "error"` FIRST and sent everything else — the
  //   `unconfirmed` half of `replyResult`/`retryResult` — to the success line, and
  //   the retry's own "still records a failure" caveat went there too. So an
  //   operator read a statement that the mail did NOT go out in the same chrome
  //   the console uses for "Notification sent again."
  //
  //   The spec above proves the marker's banner; these arms prove the TONE of the
  //   outcome line, which is what the card is about, and the UNIT guard
  //   (src/lib/adminFeedbackTone.test.ts) stops the writer coming back. Neither
  //   half is sufficient: the scan cannot read a computed ink or an ARIA role,
  //   and this file cannot see the writer nobody drove. The DELIVERED control is
  //   not decoration — it is what proves the probe can tell the two apart, so a
  //   red failure reading cannot be a "this line is always red" artefact.

  /** The two outcome inks as the browser resolves them. */
  const EMERALD_700 = "#007a55";
  const RED_600 = "#e7000b";

  type AfbReading = { present: boolean; text: string; ink: string; tone: string; role: string };

  /**
   * The outcome line's rendered shape, read out of the live DOM. `getComputedStyle`
   * returns whatever colour space Tailwind emitted (a `lab(...)` for the v4 ramps),
   * so the ink is round-tripped through a canvas exactly as
   * action-failure-tone.spec.ts does.
   */
  async function afbReading(page: Page): Promise<AfbReading> {
    return page.evaluate(`(() => {
      const el = document.querySelector('[data-testid="afb-msg"]');
      if (!el) return { present: false, text: "", ink: "", tone: "", role: "" };
      const cv = document.createElement("canvas");
      cv.width = cv.height = 1;
      const ctx = cv.getContext("2d", { willReadFrequently: true });
      const hex = (css) => {
        ctx.clearRect(0, 0, 1, 1);
        ctx.fillStyle = css;
        ctx.fillRect(0, 0, 1, 1);
        const d = ctx.getImageData(0, 0, 1, 1).data;
        return "#" + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("");
      };
      return {
        present: true,
        text: (el.textContent || "").trim(),
        ink: hex(getComputedStyle(el).color),
        tone: el.getAttribute("data-tone") || "",
        role: el.getAttribute("role") || "",
      };
    })()`) as Promise<AfbReading>;
  }

  /** Wait for the outcome line, then read it. There is exactly ONE at a time. */
  async function settledAfbReading(page: Page): Promise<AfbReading> {
    await page.waitForSelector('[data-testid="afb-msg"]', { timeout: 30_000 });
    await expect(page.locator('[data-testid="afb-msg"]')).toHaveCount(1);
    const r = await afbReading(page);
    expect(r.text, "the outcome line rendered empty — there is nothing to judge").not.toBe("");
    return r;
  }

  /** Open /admin against a fresh state and open the thread named by `subject`. */
  async function openThread(page: Page, subject: RegExp) {
    await page.goto("/login", { waitUntil: "domcontentloaded" });
    await page.evaluate(() => window.localStorage.clear());
    await signIn(page);
    await stubAdmin(page);
    await page.goto("/admin", { waitUntil: "domcontentloaded" });
    // The ENGLISH default, stated explicitly: a leftover jf_lang has already
    // produced a false reading on this board.
    await expect(page.locator("html")).toHaveAttribute("lang", "en");
    const row = page.getByRole("button", { name: subject });
    await expect(row).toBeVisible();
    await row.click();
  }

  test("a reply whose delivery was NOT confirmed is red and live, NOT the success line", async ({ page }) => {
    // The reply stored, the `notification` field absent (an older server, or a
    // proxy that stripped it). `replyResult` returns `unconfirmed` — the sentence
    // says the status was not confirmed — and that is where the pre-fix code sent
    // it to emerald.
    await openThread(page, /Sihat/);
    await page.route("**/api/admin/feedback/*/messages", (r) =>
      r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({}) }),
    );
    await page.locator("#afb-reply").fill("We are looking into this for you now.");
    await page.getByRole("button", { name: "Send reply" }).click();

    const r = await settledAfbReading(page);
    console.log(`/admin reply unconfirmed -> ${JSON.stringify(r)}`);
    expect(r.text, "the unconfirmed reply's own sentence").toContain("was not confirmed");
    expect(
      r.tone,
      `the unconfirmed reply is on the ${r.tone || "untoned"} line (ink ${r.ink}). It says the delivery ` +
        `was NOT confirmed, so it must not wear the success chrome the console uses for a delivery`,
    ).toBe("failure");
    expect(r.role, "a statement that the mail did not go out must be a live alert region").toBe("alert");
    expect(r.ink, "the failure line's measured ink").toBe(RED_600);
  });

  test("a retry that sent NOTHING is red and live, NOT the success line", async ({ page }) => {
    // `skipped` is the admin lane's no-op exit: no recipient configured, so no job
    // was enqueued and no mail will arrive. "Notification sent again." would be a
    // claim no delivery backs — the exact false-positive the marker exists for.
    await openThread(page, /Pembayaran tersekat/);
    await page.route("**/api/admin/feedback/*/notify", (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ notification: "skipped", status: "retried" }),
      }),
    );
    await page
      .getByRole("alert")
      .filter({ hasText: "Notification not delivered" })
      .getByRole("button", { name: "Send the notification again" })
      .click();

    const r = await settledAfbReading(page);
    console.log(`/admin retry skipped -> ${JSON.stringify(r)}`);
    expect(r.text, "the retry that sent nothing says so").toContain("Nothing was sent");
    expect(
      r.tone,
      `a retry that enqueued nothing is on the ${r.tone || "untoned"} line (ink ${r.ink}) — it must not ` +
        `read as "Notification sent again."`,
    ).toBe("failure");
    expect(r.role, "a retry that sent nothing must be a live alert region").toBe("alert");
    expect(r.ink, "the failure line's measured ink").toBe(RED_600);
  });

  test("the retry's marker-still-set caveat is red and live, NOT the success line", async ({ page }) => {
    // The delivery left the process but the thread still records a failure. Its
    // sentence ("... the thread still records a failure, so it was not delivered to
    // everyone") used to be printed on the success line, directly above a banner
    // saying the filer was not told.
    await openThread(page, /Pembayaran tersekat/);
    await page.route("**/api/admin/feedback/*/notify", (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ notification: "queued", status: "retried" }),
      }),
    );
    await page
      .getByRole("alert")
      .filter({ hasText: "Notification not delivered" })
      .getByRole("button", { name: "Send the notification again" })
      .click();

    const r = await settledAfbReading(page);
    console.log(`/admin retry marker-still-set -> ${JSON.stringify(r)}`);
    expect(r.text, "the caveat's own sentence").toContain("still records a failure");
    expect(
      r.tone,
      `the "still records a failure" caveat is on the ${r.tone || "untoned"} line (ink ${r.ink})`,
    ).toBe("failure");
    expect(r.role, "the caveat must be a live alert region, not a quiet confirmation").toBe("alert");
    expect(r.ink, "the failure line's measured ink").toBe(RED_600);
    // …and it does not claim the notification was delivered.
    await expect(page.getByText("Notification sent again.")).toHaveCount(0);
  });

  // ── the STATUS change's own confirmation (card t_fb9942b7) ──────────────────
  //
  // THE DEFECT THIS PINS
  //   Changing the open thread's status through the page's own `<select>` fired
  //   the POST, the thread reloaded, and the operator got NO outcome line at all
  //   (neither red nor emerald). `setFeedbackStatus` wrote `afb.statusSet` BEFORE
  //   awaiting the reload, and `loadFeedbackThread` clears fbMsg as its first
  //   synchronous act — React batched the write and the reset into one render, so
  //   the DOM only ever saw the clear. Measured on prod v1.20.94: a MutationObserver
  //   on `document.body` recorded the `afb-msg` node appearing ZERO times across
  //   the whole status action while the network log showed `POST …/status -> 200`.
  //   The reply and retry paths were reordered to write after the reload; the
  //   status path was never touched.
  //
  //   The unit guard (src/lib/feedbackNotify.test.ts) pins the source ordering;
  //   this arm proves what an operator actually reads, which no source scan can.
  test("a status change shows the emerald confirmation AFTER the reload", async ({ page }) => {
    await openThread(page, /Pembayaran tersekat/);
    // The status POST returns 200 and the thread reload follows. The stub answers
    // only the POST; the detail read (`**/api/admin/feedback/*`) still serves
    // MARKED through stubAdmin, so the reload really happens and really resets the
    // message state — which is the whole mechanism under test.
    await page.route("**/api/admin/feedback/*/status", (r) =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ status: "resolved" }),
      }),
    );

    // Guard the premise: the confirmation node does not exist before the action,
    // so a reading below cannot be a leftover from opening the thread.
    await expect(page.locator('[data-testid="afb-msg"]')).toHaveCount(0);

    await page.getByLabel("Status").selectOption("resolved");

    const r = await settledAfbReading(page);
    console.log(`/admin status change -> ${JSON.stringify(r)}`);
    expect(r.text).toBe("Status updated.");
    expect(
      r.tone,
      `the status confirmation is on the ${r.tone || "untoned"} line (ink ${r.ink}). A 200 on ` +
        `POST …/status must render the emerald outcome line, not nothing — the pre-fix handler wrote ` +
        `it before the reload that clears it (card t_fb9942b7)`,
    ).toBe("success");
    // A confirmation is not a live alert region.
    expect(r.role, "a success is not a live alert region").toBe("");
    expect(r.ink, "the success line's measured ink").toBe(EMERALD_700);
  });

  test("CONTROL: a DELIVERED retry that cleared the marker uses the success line", async ({ page }) => {
    // The retry's genuine-success arm, exactly the sibling of the "still marked"
    // arm above: the job was queued AND the thread comes back with no marker, so
    // "Notification sent again." is true and must keep the app's emerald
    // confirmation chrome. This is what makes the three failure arms non-vacuous:
    // a red reading above cannot be a "this line is always red" artefact.
    await openThread(page, /Pembayaran tersekat/);
    // After the retry the detail read answers a thread with the marker CLEARED —
    // the server state a successful re-send produces (and the reason the success
    // copy is gated on the reloaded thread at all).
    let cleared = false;
    await page.route("**/api/admin/feedback/*", (r) => {
      const url = new URL(r.request().url());
      const id = url.pathname.split("/").pop();
      if (id !== MARKED.id && id !== HEALTHY.id) {
        return r.fallback();
      }
      const th = cleared ? HEALTHY : MARKED;
      return r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ thread: th, messages: [] }),
      });
    });
    await page.route("**/api/admin/feedback/*/notify", (r) => {
      cleared = true;
      return r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ notification: "queued", status: "retried" }),
      });
    });
    await page
      .getByRole("alert")
      .filter({ hasText: "Notification not delivered" })
      .getByRole("button", { name: "Send the notification again" })
      .click();

    const r = await settledAfbReading(page);
    console.log(`/admin retry delivered + cleared -> ${JSON.stringify(r)}`);
    expect(r.text, "a delivered retry's confirmation").toContain("Notification sent again.");
    expect(r.tone, "a DELIVERED retry is a success and must keep the success line").toBe("success");
    expect(r.role, "a success is not a live alert region").toBe("");
    expect(r.ink, "the success line's measured ink").toBe(EMERALD_700);
    // …and the failure banner is gone, because the marker really was cleared.
    await expect(page.getByRole("alert").filter({ hasText: "Notification not delivered" })).toHaveCount(0);
  });
});
