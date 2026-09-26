// The feedback deep link — the web half of "the email opens the thread".
//
// WHAT CAN BREAK HERE, and why a source scan is the right guard:
//
//   1. THE LINK THE EMAIL SENDS GOES NOWHERE. The Go side builds
//      `/feedback?thread=<id>` and `/admin?thread=<id>`
//      (api/internal/httpapi/rest_feedback_i18n.go). A query parameter is only a
//      deep link if the PAGE reads it: without the mount effect below, the URL is
//      accepted, the list renders, and the reader is exactly where the old bare
//      `/feedback` link put them — a silent regression that no test on either side
//      would notice on its own, because neither side is wrong by itself.
//   2. The console has no per-thread ROUTE (this is why the parameter exists —
//      there is no /feedback/[id] page). If a route is added later, the parameter
//      must keep working or the email links must be re-pointed; this test pins
//      the contract that the parameter is read.
//   3. Both surfaces must do it: the filer's page and the admin triage page. The
//      two emails link to different pages and a fix applied to one is half a fix.
//
// WHAT IT DOES NOT COVER (stated honestly): it cannot execute React, so it does
// not observe the thread actually opening, the messages rendering, or the
// behaviour for an id the user may not read. That is driven in a real browser
// for this card. The permission claim is separate and pinned by the Go tests
// (feedback_isolation_issue615_test.go) — the parameter grants nothing.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

const read = (rel: string) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");
const path = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

const filerPage = read("../app/feedback/page.tsx");
const adminPage = read("../app/admin/page.tsx");
const goI18n = read("../../../api/internal/httpapi/rest_feedback_i18n.go");

test("the filer's page opens ?thread=<id> from the reply email", () => {
  assert.ok(
    /URLSearchParams\([^)]*location\.search[^)]*\)[\s\S]{0,80}?\.get\("thread"\)/.test(filerPage),
    "web/src/app/feedback/page.tsx must read the ?thread= parameter the reply email sends — " +
      "without it the link opens the queue and the reader hunts for the answer",
  );
  assert.ok(
    /openThread\(id\)/.test(filerPage),
    "reading ?thread= must actually OPEN that thread (call openThread), not just parse the URL",
  );
});

test("the admin page opens ?thread=<id> from the filing notification", () => {
  assert.ok(
    /URLSearchParams\([^)]*location\.search[^)]*\)[\s\S]{0,80}?\.get\("thread"\)/.test(adminPage),
    "web/src/app/admin/page.tsx must read the ?thread= parameter the operator email sends",
  );
  assert.ok(
    /openFeedback\(id\)/.test(adminPage),
    "reading ?thread= on /admin must actually OPEN that thread in the triage section",
  );
});

test("there is no per-thread route, so the parameter is the contract", () => {
  // The Go helper explains WHY the link is a parameter: the console serves one
  // client-rendered page per surface and selects a thread in state. If a
  // /feedback/[id] route is ever added, this test still passes (the parameter
  // keeps working) but the doc comment must be updated — so the claim is pinned
  // here as a comment, and the absence of the route is asserted below.
  assert.ok(
    !existsSync(path("../app/feedback/[id]")),
    "a /feedback/[id] route now exists: the email link (/feedback?thread=) and this " +
      "explanation (api/internal/httpapi/rest_feedback_i18n.go) must be revisited together",
  );
});

test("the Go link builders agree with the pages (cross-language pin)", () => {
  // The paths the Go side builds must be the paths the pages are served at.
  assert.ok(
    /feedbackDeepLink\(baseURL, "\/feedback", id\)/.test(goI18n),
    "feedbackThreadLink must deep-link to /feedback",
  );
  assert.ok(
    /feedbackDeepLink\(baseURL, "\/admin", id\)/.test(goI18n),
    "feedbackAdminLink must deep-link to /admin",
  );
  assert.ok(
    goI18n.includes('"?thread="'),
    "the Go link must carry the ?thread= parameter these pages read — the two sides " +
      "are useless apart and neither side fails on its own",
  );
});
