// WebAuthn binary-field encoding for the browser → server contract.
//
// THE DEFECT THIS EXISTS FOR (card t_df9233f6)
//   Both passkey call sites encoded the ceremony's binary fields with `btoa`,
//   which emits STANDARD base64 (`+`, `/`, `=`). go-webauthn decodes every one
//   of those fields with `base64.RawURLEncoding`
//   (`protocol/base64.go` → `base64.RawURLEncoding.DecodeString`), so a payload
//   whose encoding happened to carry a `+` or a `/` failed to decode, surfaced
//   as `ErrBadRequest("Parse error for Registration")`, and the handler mapped
//   it to the generic `400 "response tidak sah"`. Registration failed ~8/8 in a
//   measured run (the attestation object is large enough that it nearly always
//   carries one of the two characters) and the user's only symptom was
//   "Failed to register passkey" — nothing was stored and nothing said why.
//
//   The alphabet is therefore part of the wire contract, not a style choice:
//   base64url, padding stripped. `b64url` below is the single encoder both
//   pages use, and `src/lib/webauthn.test.ts` pins it against a payload that
//   DOES contain bytes which produce `+` and `/` in standard base64, so a
//   revert to `btoa` cannot pass unnoticed again.
//
// `fromB64url` is the inverse, used for the fields the server sends us (the
// challenge and `user.id`), which are already base64url on the wire.

/** Encode bytes as base64url: standard alphabet with `+`→`-`, `/`→`_`, no padding. */
export function b64url(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** Decode base64url (padded or not) back to the bytes. */
export function fromB64url(value: string): Uint8Array {
  const b64 = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = b64 + "=".repeat((4 - (b64.length % 4)) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}
