// The access-level choice on the RFC 8628 consent card — the pure half
// (card t_cf45b84c).
//
// THE DEFECT THIS RESOLVES. The consent card showed the client's requested
// scope as TEXT and offered only Approve / Deny, so the scope was whatever the
// harness had asked for in POST /oauth/device_authorization:
//
//   * an agent that asked for `read` stayed read-only and had to be re-granted
//     later, when it first needed to write;
//   * the only way to change it afterwards was the Connected-apps list, whose
//     scope change ROTATES the credential and forces the harness to
//     re-authenticate.
//
// Wan (2026-11-12): "default scope tu jgn read only. bagi semua supaya user tak
// perlu grant banyak kali." So the card now offers both scopes as a real radio
// group, preselected to WHAT THE CLIENT ASKED FOR (never silently read-only),
// and both directions are allowed in one click: read → readwrite so the agent
// never needs re-granting, readwrite → read for a cautious user.
//
// WHY THIS IS A MODULE AND NOT INLINE IN THE PAGE. src/app/profile/page.tsx is
// a .tsx file the node unit runner cannot load (the same reason lib/profileTab.ts
// exists), so the rule lives here where a test can drive it, and the page MUST
// call it — the test asserts the call site as well, because a perfect helper the
// page ignores is exactly the shape of the bug being fixed.
//
// WHAT THIS FILE DOES NOT DECIDE. Which scopes the SERVER can issue: that is
// `choices` on the consent response (api/internal/httpapi/oauth_device.go,
// derived from oauthmeta.ScopesSupported()). This module only filters the
// labels it knows against what the server said, and falls back to both when an
// older API sends no `choices` at all.

/** The two access levels a person can grant. Values are the OAuth scope names
 *  the API accepts verbatim — they are never rendered, only sent. */
export type DeviceScope = "read" | "readwrite";

/** One radio on the card: the machine value, and the two dictionary keys whose
 *  copy states the consequence. Visible text carries NO jargon ("scope",
 *  "grant") — the card says what the agent may DO. */
export type DeviceScopeOption = {
  value: DeviceScope;
  labelKey: "prof.deviceScopeRead" | "prof.deviceScopeReadWrite";
  helpKey: "prof.deviceScopeReadHelp" | "prof.deviceScopeReadWriteHelp";
};

const ALL_OPTIONS: readonly DeviceScopeOption[] = [
  {
    value: "read",
    labelKey: "prof.deviceScopeRead",
    helpKey: "prof.deviceScopeReadHelp",
  },
  {
    value: "readwrite",
    labelKey: "prof.deviceScopeReadWrite",
    helpKey: "prof.deviceScopeReadWriteHelp",
  },
];

const KNOWN: readonly string[] = ALL_OPTIONS.map((o) => o.value);

function isDeviceScope(v: unknown): v is DeviceScope {
  return typeof v === "string" && (KNOWN as readonly string[]).includes(v);
}

/**
 * The radios to render, in card order, given the scopes the SERVER says it can
 * issue (`choices` on the consent response).
 *
 * `read` is always first: it is the narrower grant, and a person scanning the
 * card should meet the cautious option before the wider one.
 *
 * A response with no `choices` (an API older than this card, i.e. a rolling
 * deploy) and a response that names none of the values this module knows both
 * fall back to BOTH options. Falling back to `read` alone would reinstate the
 * silent read-only default this card exists to remove, and rendering nothing
 * would leave the card with no way to approve at all.
 */
export function deviceScopeOptions(
  supported: readonly string[] | null | undefined,
): DeviceScopeOption[] {
  const usable = Array.isArray(supported)
    ? supported.filter((s): s is DeviceScope => isDeviceScope(s))
    : [];
  if (usable.length === 0) return ALL_OPTIONS.map((o) => ({ ...o }));
  return ALL_OPTIONS.filter((o) => usable.includes(o.value)).map((o) => ({ ...o }));
}

/**
 * Which radio the card preselects, given the scope the CLIENT requested
 * (`scopes` on the consent response).
 *
 * THE REQUESTED SCOPE IS THE DEFAULT, and the point of the card is that the
 * person can change it. A harness that asked for `readwrite` opens on
 * `readwrite`; a harness that asked for `read` opens on `read` and may be
 * widened with the same single click. Never silently read-only: that is Wan's
 * instruction, and it is what makes "one click" enough for the harness.
 *
 * `read` is returned only when the request genuinely says read. A request that
 * names readwrite — even alongside read, which is what a `read readwrite`
 * scope parameter resolves to — opens on the wider level, because the client
 * has stated it intends to write.
 */
export function defaultDeviceScope(
  requested: readonly string[] | null | undefined,
): DeviceScope {
  if (Array.isArray(requested) && requested.some((s) => s === "readwrite")) {
    return "readwrite";
  }
  return "read";
}

/**
 * The JSON body for the approve call.
 *
 * `scope` is sent only on approve, and only for a KNOWN value: an unknown
 * selection (there is no such state in the UI, but this is the function that
 * would carry it to the wire) is dropped rather than sent, so the API applies
 * its own documented default — the client's requested scope — instead of
 * refusing the approval over a value the person never chose.
 */
export function deviceDecisionBody(
  userCode: string,
  decision: "approve" | "deny",
  scope: DeviceScope | string | null | undefined,
): { user_code: string; decision: "approve" | "deny"; scope?: DeviceScope } {
  if (decision === "deny") return { user_code: userCode, decision };
  return isDeviceScope(scope)
    ? { user_code: userCode, decision, scope }
    : { user_code: userCode, decision };
}
