// /docs/mcp/connect promises that a platform-admin account cannot use the device
// grant (card t_c94bb976).
//
// THE PROMISE, AND WHY IT IS WORTH A GUARD. The page tells an operator signed in
// as the platform admin to stop before running the CLI:
//
//   "**Platform-admin accounts cannot use the device grant.** No code arrives,
//    and **Approve** answers `403` ("not valid or has expired"). Use the browser
//    flow in **Connect an agent** above instead."
//
// Every clause of that is enforced somewhere else, and until this file none of it
// was pinned on the page side:
//
//   no code arrives     api/internal/httpapi/oauth_device.go (deviceAuthorization)
//                       — pinned by TestDeviceAuthorizationNeverMailsSuperAdmin
//   Approve → 403       same file (postDeviceConsent) — pinned by
//                       TestDeviceApproveRefusedForPlatformAdmin
//                       (oauth_device_admin_refusal_t_c94bb976_test.go)
//   the quoted banner   `request.forbidden` is NOT in lib/apiErr.ts's
//                       API_ERROR_KEYS, so /profile falls back to
//                       prof.deviceNotFound — the words quoted in the warning
//
// Delete the callout, soften it to a `note`, or let the dashboard's fallback copy
// drift and this page keeps publishing a warning a reader either cannot see or
// cannot trust. `grep -rn "Platform-admin accounts cannot use the device grant"
// --include=*_test.*` returned nothing before this card: the whole sentence was
// unguarded, and no e2e/a11y run covers /docs prose.
//
// HOW IT READS THE PAGE (and what that does not cover). /docs/mcp/connect is a
// `SECTIONS` literal rendered by src/app/docs/docsProse. Reading the file as TEXT
// would let the sentence satisfy the guard from a comment — the hole
// docsIndex.test.ts closed for /docs. lib/docsProseReader parses the literal, so
// what is asserted here is a string the renderer is handed, in the block kind the
// renderer branches on. It still does not execute React: the RENDERED callout is
// checked in a browser on the built page (recorded in this card's handoff), which
// is the part a unit guard here cannot claim.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { calloutsOf, sectionsOf } from "./docsProseReader.ts";
import { API_ERROR_KEYS } from "./apiErr.ts";
import { DICT } from "../components/i18nDict.ts";

const PAGE = "../app/docs/mcp/connect/page.tsx";
const source = readFileSync(fileURLToPath(new URL(PAGE, import.meta.url)), "utf8");

// The sentence, in the page's own words. An edit here is an edit to a published
// promise: if the copy genuinely moved, change WARNING in the SAME commit and say
// why in the message — that is the guard doing its job, not a reason to loosen it
// to a substring search.
const WARNING =
  '**Platform-admin accounts cannot use the device grant.** No code arrives, and **Approve** answers `403` ("not valid or has expired"). Use the browser flow in **Connect an agent** above instead.';

// The command the warning stands in front of.
const LOGIN_COMMAND = "$ jomform mcp login --headless";

const sections = sectionsOf(source, PAGE);

function section(id: string) {
  const found = sections.find((s) => s.id === id);
  if (!found) {
    assert.fail(
      `/docs/mcp/connect has no section with id ${JSON.stringify(id)} — it has ` +
        `[${sections.map((s) => s.id).join(", ")}]. The page was restructured, so this guard is looking at nothing.`,
    );
  }
  return found;
}

test("the page still warns platform admins that the device grant is refused for them", () => {
  const warns = calloutsOf(source, PAGE).filter((c) => c.section === "cli" && c.kind === "warn");
  assert.equal(
    warns.length,
    1,
    `the "cli" section declares ${warns.length} warn block(s), want exactly 1 — the platform-admin warning is either gone ` +
      `or demoted to a tip/note, and a reader who skims for the amber callout no longer finds it`,
  );
  assert.equal(
    warns[0].text,
    WARNING,
    "the platform-admin warning changed. It is a published promise (the API refuses those accounts at the mail step AND at " +
      "consent), so if the copy really moved, change WARNING in this file in the same commit.",
  );
  for (const claim of ["Platform-admin accounts cannot use the device grant", "No code arrives", "`403`"]) {
    assert.ok(
      WARNING.includes(claim),
      `the warning no longer states ${JSON.stringify(claim)} — it is one of the three things a reader acts on`,
    );
  }
});

test("the warning sits where the reader meets it: after the steps, above the command", () => {
  const cli = section("cli");
  const warnAt = cli.strings.indexOf(WARNING);
  assert.ok(
    warnAt >= 0,
    "the warning is not among the strings the renderer receives for the \"cli\" section — it is in a comment, or in a " +
      "block the renderer drops",
  );
  const loginAt = cli.strings.findIndex((s) => s.includes(LOGIN_COMMAND));
  assert.ok(loginAt >= 0, `the "cli" section no longer carries ${JSON.stringify(LOGIN_COMMAND)}`);
  assert.ok(
    warnAt < loginAt,
    "the warning sits BELOW the login command it is warning about, so a reader runs the command before reading the refusal",
  );
  const lastStepAt = cli.strings.findIndex((s) => s.startsWith("The command finishes by itself"));
  assert.ok(lastStepAt >= 0, "the last numbered step of the CLI walkthrough is gone");
  assert.ok(warnAt > lastStepAt, "the warning interrupts the numbered walkthrough instead of following it");

  // "…in **Connect an agent** above instead" is a claim about page POSITION.
  const order = sections.map((s) => s.id);
  const connectAt = order.indexOf("connect");
  assert.ok(connectAt >= 0, `the "connect" section is gone: [${order.join(", ")}]`);
  assert.ok(
    connectAt < order.indexOf("cli"),
    `the browser-flow section no longer precedes the CLI section, so "Connect an agent above" points the wrong way: ` +
      `[${order.join(", ")}]`,
  );
});

test("the banner it quotes is the one the dashboard really shows for that 403", () => {
  const quoted = /"([^"]+)"/.exec(WARNING)?.[1];
  if (!quoted) {
    assert.fail(
      "the warning no longer quotes the sentence a person sees on the consent card — without it a reader who hits the 403 " +
        "cannot tell the bare refusal from an expired code",
    );
  }
  assert.ok(
    !("request.forbidden" in API_ERROR_KEYS),
    "request.forbidden now has its own dashboard message (lib/apiErr.ts API_ERROR_KEYS), so /profile no longer falls back to " +
      "prof.deviceNotFound and the words quoted on /docs/mcp/connect are no longer what a person sees. Re-derive the " +
      "warning's parenthetical from the mapped key in the same commit.",
  );
  const fallback = DICT["prof.deviceNotFound"].en;
  assert.ok(
    fallback.includes(quoted),
    `the page quotes ${JSON.stringify(quoted)}, which prof.deviceNotFound's English copy does not carry ` +
      `(${JSON.stringify(fallback)}). The parenthetical must stay a real fragment of the banner the dashboard renders, ` +
      `or the page tells a reader to expect words they will never see.`,
  );
});
