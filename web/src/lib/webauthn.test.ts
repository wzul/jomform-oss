// Card t_df9233f6 — the browser→server base64 alphabet contract.
//
// THE DEFECT
//   Both passkey pages encoded the ceremony's binary fields with `btoa`, which
//   emits STANDARD base64 (`+`, `/`, `=`). go-webauthn decodes all of them with
//   `base64.RawURLEncoding` (`protocol/base64.go`), so a body whose encoding
//   happened to carry a `+` or a `/` failed to parse, the handler answered
//   `400 {"error":"response tidak sah"}`, and the user saw only "Failed to
//   register passkey". Measured through the real UI on prod by the parent card:
//   8/8 registrations rejected; the SAME ceremony re-encoded as base64url
//   returned `200 {"status":"registered"}`. The alphabet was the only variable.
//
// WHY THIS GUARD IS HERE AND NOT ONLY IN GO
//   The Go suite hand-builds its payloads with `base64.RawURLEncoding`
//   (auth_passkey_legacy_workspace_test.go:150-160) and never consults the
//   client's encoder, which is exactly why a green gate coexisted with a
//   100%-failing endpoint. The seam has two ends, so it is pinned at both:
//   api/internal/httpapi/passkey_seam_test.go asserts the SERVER accepts the
//   shipped alphabet and reads the client source cross-language; this file
//   asserts the ENCODER itself, on real bytes.
//
// WHAT THIS DOES NOT COVER (stated honestly)
//   * It does not execute React, so it cannot see that the pages call the
//     helper on the right fields — that half is the cross-language source read
//     in the Go guard and the browser run recorded on the card.
//   * It does not prove the server accepts the output; the Go seam test does,
//     with a cryptographically genuine attestation in both alphabets.
//
// Run with:  npm run test:unit
//   (node --test --disable-warning=MODULE_TYPELESS_PACKAGE_JSON src/lib/*.test.ts)
import { test } from "node:test";
import assert from "node:assert/strict";

import { b64url, fromB64url } from "./webauthn.ts";

// 0xfb 0xff 0xfe 0xfd -> "+//+/Q==" in STANDARD base64: it carries BOTH
// characters the two alphabets disagree on, plus padding. Verified by running
// btoa on these bytes (not hand-computed — a first attempt at these constants
// was wrong, which is exactly the trap this guard exists to catch). Picking
// bytes whose standard encoding contains neither would make every assertion
// below pass against a `btoa` implementation too.
const PLUS_SLASH_BYTES = Uint8Array.from([0xfb, 0xff, 0xfe, 0xfd]);
const STANDARD_B64 = "+//+/Q==";
const URL_B64 = "-__-_Q";

test("b64url emits the URL alphabet for bytes btoa spells with + and /", () => {
  // Ground truth first: this fixture really does exercise the difference.
  const asBtoa = btoa(String.fromCharCode(...PLUS_SLASH_BYTES));
  assert.equal(asBtoa, STANDARD_B64, "fixture drifted: btoa no longer spells these bytes with + and /");

  const got = b64url(PLUS_SLASH_BYTES);
  assert.equal(got, URL_B64, "b64url must map + -> - and / -> _ and strip padding");
  assert.ok(!got.includes("+"), "b64url output contains a + — RawURLEncoding cannot decode it");
  assert.ok(!got.includes("/"), "b64url output contains a / — RawURLEncoding cannot decode it");
  assert.ok(!got.includes("="), "b64url output is padded — the server's decode is Raw (no padding)");
});

test("b64url never emits +, / or = for any byte value", () => {
  // Every byte 0..255 in one payload, so no single value can hide an offender.
  const all = Uint8Array.from({ length: 256 }, (_, i) => i);
  const got = b64url(all);
  assert.ok(!/[+/=]/.test(got), `b64url(${all.length} bytes) emitted ${JSON.stringify(got.match(/[+/=]/g))}`);
  // And it is the alphabet the server documents: decode it back as RawURL.
  assert.equal(fromB64url(got).length, 256);
});

test("b64url agrees with the standard alphabet's bytes, not just its shape", () => {
  // The two encodings must decode to the SAME bytes — that is what makes
  // accepting either on the server lossless.
  assert.deepEqual(Array.from(fromB64url(b64url(PLUS_SLASH_BYTES))), Array.from(PLUS_SLASH_BYTES));
  assert.deepEqual(Array.from(fromB64url(STANDARD_B64)), Array.from(PLUS_SLASH_BYTES));
  assert.deepEqual(Array.from(fromB64url(URL_B64)), Array.from(PLUS_SLASH_BYTES));
});

test("fromB64url round-trips unpadded server values", () => {
  // The server sends the challenge as RawURL (no padding), e.g. a 32-byte
  // challenge — 43 characters with no trailing '='.
  const challenge = Uint8Array.from({ length: 32 }, (_, i) => (i * 7) % 256);
  const wire = b64url(challenge);
  assert.equal(wire.length, 43, "a 32-byte challenge has no padding to strip");
  assert.deepEqual(Array.from(fromB64url(wire)), Array.from(challenge));
  // Padding is tolerated on the way in, for a client that sends it.
  assert.deepEqual(Array.from(fromB64url(wire + "=")), Array.from(challenge));
});
