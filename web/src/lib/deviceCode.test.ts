// The device-flow code a person retypes must resolve to the code that was
// mailed (card t_4f3ab16e).
//
// THE DEFECT this file is part of: /profile?tab=mcp had no code field at all
// unless the URL carried `?user_code=`, so a person whose harness printed a code
// had nowhere to type it. The field is now always there — which makes the SHAPE
// of what is typed matter for the first time: a field that only accepts the
// canonical `KX9M-X5BS` turns `kx9mx5bs` into "not valid" for a code the user is
// reading off their own screen.
//
// The rule itself lives on the server (api/internal/httpapi/oauth_device.go,
// normalizeUserCode), which is what hashes the code and looks the row up. This
// file pins the CLIENT half — the same five shapes, in the same order, as the Go
// test that resolves them through the real HTTP handler — so the two copies
// cannot silently disagree.
import assert from "node:assert/strict";
import { test } from "node:test";

import { DEVICE_CODE_LEN, normalizeDeviceCode } from "./deviceCode.ts";

const CANONICAL = "KX9M-X5BS";

test("the shapes a person actually types all resolve to the mailed code", () => {
  // Every entry is the SAME code: a harness printing it, a phone keyboard
  // lower-casing it, a copy-paste carrying the mail's spaces, a person typing it
  // without the dash, and the dash replaced by a space.
  const shapes = [
    "KX9M-X5BS", // as mailed / as the CLI prints it
    "kx9mx5bs", // keyboard lower-case, no dash
    " kx9mx5bs ", // surrounding spaces from a copy-paste
    "KX9M X5BS", // a space where the dash was
    "  KX9M-X5BS\t", // a trailing tab
    "kx9m-x5bs", // lower-case WITH the dash
  ];
  for (const shape of shapes) {
    assert.equal(
      normalizeDeviceCode(shape),
      CANONICAL,
      `${JSON.stringify(shape)} must resolve to ${CANONICAL} — the server hashes the canonical form, so a shape it never sees as canonical is a code that "is not valid" for a person reading it off their screen`,
    );
  }
});

test("a string that cannot be a code is rejected without a round trip", () => {
  // Not a shape problem: these are too short / too long / empty, which the
  // server also refuses (it needs DEVICE_CODE_LEN characters). Rejecting them
  // locally is the same answer, one round trip earlier.
  for (const bad of ["", "   ", "KX9M", "KX9M-X5B", "KX9M-X5BSX", "KX9MX5BSX"]) {
    assert.equal(normalizeDeviceCode(bad), "", `${JSON.stringify(bad)} is not a code`);
  }
});

test("the length the guard uses is the one the server uses", () => {
  // Pinned as a number so a widened/loosened rule on either side is a visible
  // edit here rather than a silently different acceptance criterion.
  assert.equal(DEVICE_CODE_LEN, 8);
  assert.equal(normalizeDeviceCode("X".repeat(DEVICE_CODE_LEN)), "XXXX-XXXX");
});
