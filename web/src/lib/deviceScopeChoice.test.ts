// The access-level choice on the RFC 8628 consent card (card t_cf45b84c).
//
// THE DEFECT THIS GUARD EXISTS FOR. The consent card rendered the client's
// requested scope as TEXT and offered only Approve / Deny. So the grant was
// whatever the harness had asked for: an agent that requested `read` was stuck
// read-only and had to be re-granted the moment it needed to write, and the only
// way to change it afterwards was the Connected-apps list — whose scope change
// ROTATES the credential and forces the harness to re-authenticate.
// Wan: "default scope tu jgn read only. bagi semua supaya user tak perlu grant
// banyak kali."
//
// WHAT IT PINS
//   1. The card opens on WHAT THE CLIENT ASKED FOR: requested `readwrite` →
//      `readwrite` preselected, requested `read` → `read`. Never silently
//      read-only, which is the whole point of the card.
//   2. BOTH directions are offered and both are sendable: read → readwrite and
//      readwrite → read, one click each, no second trip through /profile.
//   3. The radios come from what the SERVER can issue (`choices`), in the order
//      the card shows them, and a response that omits `choices` (an API older
//      than this card, i.e. a rolling deploy) still renders BOTH rather than
//      collapsing to read — a fallback of `read` would reinstate the defect.
//   4. The approve body carries the chosen scope, the deny body does not, and a
//      bogus selection is DROPPED (so the API applies its documented default)
//      rather than sent — the server's contract is "absent = the request".
//   5. The page WIRES to this module: it imports it, seeds the selection from
//      `defaultDeviceScope(d.scopes)` on every checked code, renders one radio
//      per `deviceScopeOptions(...)` entry with `checked` bound to the selection,
//      and posts through `deviceDecisionBody`. A perfect helper the page ignores
//      is exactly the shape of the bug being fixed — the same lesson
//      lib/profileTab.test.ts records.
//
// BITE PROOF (measured, not asserted here). Forcing `defaultDeviceScope` to
// return "read" unconditionally turns test 1 red and nothing else; see the
// handoff on the card for the run.
//
// WHAT IT DOES NOT COVER (stated honestly). This repo has no DOM/JSX unit
// runner (node --test cannot load .tsx), so this file does not RENDER the card.
// That the two radios paint, with the requested one checked, was read out of the
// live built app in a browser with a cleared profile and is recorded on the
// card's handoff.
//
// Run with:
//
//   npm run test:unit

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  defaultDeviceScope,
  deviceDecisionBody,
  deviceScopeOptions,
} from "./deviceScopeChoice.ts";

const HERE = fileURLToPath(new URL(".", import.meta.url));
const PAGE = readFileSync(`${HERE}../app/profile/page.tsx`, "utf8");

// --- 1. the default is the client's request, in both directions -------------

test("the card opens on the scope the CLIENT requested — never silently read-only", () => {
  // readwrite requested → readwrite preselected. THIS is the line Wan asked for:
  // a harness that wants to write must not land on read and need a re-grant.
  assert.equal(defaultDeviceScope(["readwrite"]), "readwrite");
  // A read request opens on read — and can still be widened with one click
  // (test 2), so the cautious default costs the user nothing later.
  assert.equal(defaultDeviceScope(["read"]), "read");
  // A request that names both resolves to the wider one: the client has said it
  // intends to write, and honouring the narrower value would be the silent
  // downgrade this card removes.
  assert.equal(defaultDeviceScope(["read", "readwrite"]), "readwrite");
  // Order in the array must not matter.
  assert.equal(defaultDeviceScope(["readwrite", "read"]), "readwrite");
});

// --- 2. both directions are offered ----------------------------------------

test("both access levels are rendered, read first", () => {
  const opts = deviceScopeOptions(["read", "readwrite"]);
  assert.deepEqual(
    opts.map((o) => o.value),
    ["read", "readwrite"],
    "the card must offer both levels, narrower first",
  );
  // Each radio states its CONSEQUENCE, in its own dictionary keys: the labels are
  // what the person reads, and the copy must not be shared between the two (that
  // would make the consequence unreadable).
  assert.equal(opts[0].labelKey, "prof.deviceScopeRead");
  assert.equal(opts[0].helpKey, "prof.deviceScopeReadHelp");
  assert.equal(opts[1].labelKey, "prof.deviceScopeReadWrite");
  assert.equal(opts[1].helpKey, "prof.deviceScopeReadWriteHelp");
});

test("a cautious user can approve a readwrite request as read, and vice versa", () => {
  // readwrite asked, read chosen → read goes on the wire.
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "approve", "read"), {
    user_code: "AAAA-BBBB",
    decision: "approve",
    scope: "read",
  });
  // read asked, readwrite chosen → readwrite goes on the wire (the widening the
  // card exists to make one click).
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "approve", "readwrite"), {
    user_code: "AAAA-BBBB",
    decision: "approve",
    scope: "readwrite",
  });
});

// --- 3. the radios follow the server's list --------------------------------

test("the radios follow what the server says it can issue", () => {
  // A deployment that issued only one scope must not offer the other: the API
  // would refuse it, and a radio that cannot be honoured is a lie on the card.
  assert.deepEqual(
    deviceScopeOptions(["read"]).map((o) => o.value),
    ["read"],
  );
  assert.deepEqual(
    deviceScopeOptions(["readwrite"]).map((o) => o.value),
    ["readwrite"],
  );
  // Unknown values are ignored rather than rendered as a bare machine name.
  assert.deepEqual(
    deviceScopeOptions(["read", "admin"]).map((o) => o.value),
    ["read"],
  );
});

test("a response with no `choices` still renders BOTH levels", () => {
  // Rolling deploy: the new dashboard can be served by an API that predates this
  // card and sends no `choices`. The card must still offer both — falling back
  // to read alone would silently reinstate read-only, and rendering nothing
  // would leave no way to approve.
  assert.deepEqual(
    deviceScopeOptions(undefined).map((o) => o.value),
    ["read", "readwrite"],
  );
  assert.deepEqual(
    deviceScopeOptions(null).map((o) => o.value),
    ["read", "readwrite"],
  );
  assert.deepEqual(
    deviceScopeOptions([]).map((o) => o.value),
    ["read", "readwrite"],
  );
  assert.deepEqual(
    deviceScopeOptions(["nonsense"]).map((o) => o.value),
    ["read", "readwrite"],
  );
});

// --- 4. what reaches the wire ----------------------------------------------

test("deny sends no scope, and an unknown selection is dropped (not sent)", () => {
  // Deny grants nothing, so it must carry nothing to grant.
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "deny", "readwrite"), {
    user_code: "AAAA-BBBB",
    decision: "deny",
  });
  // The server's contract is "absent scope = the client's request". Sending junk
  // would have it refuse an approval over a value the person never chose; the
  // documented default is the better answer, and the API re-validates anyway.
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "approve", undefined), {
    user_code: "AAAA-BBBB",
    decision: "approve",
  });
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "approve", ""), {
    user_code: "AAAA-BBBB",
    decision: "approve",
  });
  assert.deepEqual(deviceDecisionBody("AAAA-BBBB", "approve", "READWRITE"), {
    user_code: "AAAA-BBBB",
    decision: "approve",
  });
});

// --- 5. the page actually uses it ------------------------------------------

test("the page wires to this module instead of re-implementing it", () => {
  assert.match(
    PAGE,
    /import\s*\{[^}]*defaultDeviceScope[^}]*\}\s*from\s*"\.\.\/\.\.\/lib\/deviceScopeChoice"/,
    "src/app/profile/page.tsx must import defaultDeviceScope from lib/deviceScopeChoice",
  );

  // The selection is seeded from the REQUESTED scope on every checked code.
  // Hardcoding "read" here — the defect — leaves this regex unmatched.
  assert.match(
    PAGE,
    /setDeviceScope\(\s*defaultDeviceScope\(\s*d\.scopes\s*\)\s*\)/,
    "checkDevice must seed the radio from defaultDeviceScope(d.scopes)",
  );

  // One radio per option the server allows, checked from the selection.
  assert.match(
    PAGE,
    /deviceScopeOptions\(\s*deviceConsent\.choices\s*\)\.map\(/,
    "the card must render its radios from deviceScopeOptions(deviceConsent.choices)",
  );
  assert.match(
    PAGE,
    /type="radio"/,
    "the access-level choice must be a real radio group, not prose",
  );
  assert.match(
    PAGE,
    /checked=\{\s*deviceScope === opt\.value\s*\}/,
    "each radio's checked state must be bound to the selection",
  );
  assert.match(
    PAGE,
    /onChange=\{\s*\(\)\s*=>\s*setDeviceScope\(opt\.value\)\s*\}/,
    "picking a radio must update the selection",
  );

  // The label/help mapping the module owns must be the one the page RENDERS: the
  // page writes the four keys out literally (the i18n call-site scan needs a
  // static argument), so this is the assertion that keeps the two halves from
  // drifting into "the module says Read only, the card says something else".
  for (const key of [
    "prof.deviceScopeRead",
    "prof.deviceScopeReadHelp",
    "prof.deviceScopeReadWrite",
    "prof.deviceScopeReadWriteHelp",
  ]) {
    assert.ok(
      PAGE.includes(`t("${key}")`),
      `profile/page.tsx must render ${key} from the dictionary`,
    );
  }

  // The approve call carries the choice through the shared body builder.
  assert.match(
    PAGE,
    /body:\s*JSON\.stringify\(\s*deviceDecisionBody\(\s*deviceCode,\s*decision,\s*deviceScope\s*\)\s*\)/,
    "decideDevice must post deviceDecisionBody(deviceCode, decision, deviceScope)",
  );

  // The old shape — the request printed as text, with no choice at all — must be
  // gone from the card. `scope_prose` stays in the response type (the API's own
  // contract keeps it, and the consent EMAIL still renders it), but the card must
  // no longer RENDER it as the access level: that text is what made the choice
  // look fixed.
  assert.equal(
    PAGE.includes("deviceConsent.scope_prose"),
    false,
    "the consent card must not render scope_prose as the access level any more",
  );
});
