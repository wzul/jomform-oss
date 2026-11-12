// The device-flow consent card's CODE LOOKUP: one settled answer, sequenced, so a
// superseded response cannot paint anything (card t_3368be9e, reworked onto main
// by card t_762ada65).
//
// THE DEFECT. The card on /profile?tab=mcp rendered from two independent pieces of
// state — the consent a lookup resolved, and an error — and the lookup had no
// ordering. Two lookups can be in flight at once: the page fires one for the
// `?user_code=` the email link carried (often the EXPIRED code from an earlier
// attempt) and another for the fresh code a person types. Whichever settled LAST
// painted, so a slow 404 for the dead code landed after the valid consent and put
// a red "This code is not valid or has expired" banner directly above a fully
// rendered consent card for an unexpired code, with Approve/Deny on it. Wan's
// screenshot of /profile?tab=mcp is that frame: two mutually exclusive claims on
// screen at the same time.
//
// WHAT MAKES IT IMPOSSIBLE HERE, rather than merely unlikely:
//
//   1. ONE STATE VALUE. The consent, the problem and the code the problem is
//      about live in `DeviceState`, so a caller cannot set one half without also
//      deciding the other — the divergence the bug needed is not expressible.
//   2. A SEQUENCE. Every lookup takes an id from `beginLookup`; `settleLookup`
//      returns `null` for any id that is no longer current, and `null` is the only
//      thing a stale request can get back. It cannot touch the state at all, let
//      alone half of it. Editing or clearing the field also claims an id, so a
//      request for the code the person has moved past is stranded even before its
//      response arrives.
//   3. THE PROBLEM IS A KEY, NOT PROSE. `DeviceState.problem` is `"not_found"`; the
//      sentence is resolved at render. A prose string captured when the request
//      settled survives a language switch and reads as a stale claim in the
//      previous locale.
//
// NORMALISATION IS NOT DEFINED HERE. The rule ("strip the dash and the spaces,
// upper-case, require 8, re-insert the dash") already exists once in
// src/lib/deviceCode.ts — the client half of api/internal/httpapi/oauth_device.go
// normalizeUserCode, pinned on both sides by deviceCode.test.ts and
// oauth_device_typecode_t_4f3ab16e_test.go. The page imports that one; this module
// deliberately defines no `normalize*` at all, because a second copy is exactly
// the drift those two pins exist to prevent.
//
// Run with:
//
//   npm run test:unit

/** The consent card's whole server payload (deviceConsentOut). */
export type DeviceConsent = {
  user_code: string;
  client_id: string;
  client: string;
  /** What the CLIENT asked for in POST /oauth/device_authorization. */
  scopes: string[];
  /**
   * The access levels this deployment can issue (oauthmeta.ScopesSupported()),
   * one radio each. Absent from an API older than card t_cf45b84c, in which case
   * the card falls back to both — see lib/deviceScopeChoice.ts.
   */
  choices?: string[];
  scope_prose: string;
  expires_at: string;
  state: "pending" | "approved" | "denied" | "expired" | "used";
};

/** The one failure a code lookup can report. A key, not a sentence: see the
 *  header. `""` means "no problem". */
export type DeviceProblem = "" | "not_found";

/**
 * The consent panel's state, as ONE value.
 *
 * The invariants that matter, and that the tests below assert directly:
 *
 *   * `problem !== ""` ⇒ `consent === null` (an error and a card are the two
 *     answers to the same question, so only one of them can be true);
 *   * `consent !== null` ⇒ `problem === ""`;
 *   * `checking` is true only while a lookup this state is waiting on is current.
 */
export type DeviceState = {
  consent: DeviceConsent | null;
  // Named `problem` rather than `error` because src/lib/apiErr.test.ts forbids
  // reading a `.error` outside apiErr.ts (t_10453dd9: the API's prose may travel
  // exactly one hop). This field is a machine key, not the API's prose, but that
  // guard is a shape match on `.error` — the honest move is to keep the class
  // unreadable here rather than widen the guard's exemption list.
  problem: DeviceProblem;
  /** The canonical code (`XXXX-XXXX`) the answer is about; `""` before any
   *  answer. A problem may never outlive the code it referred to, so the render
   *  compares this with the field. */
  code: string;
  /** A lookup is in flight. */
  checking: boolean;
};

/** No card, no problem, nothing in flight. Also the answer to "the input was
 *  cleared or edited". */
export const DEVICE_IDLE: DeviceState = { consent: null, problem: "", code: "", checking: false };

/**
 * The three-way label the panel renders from: the shape main's `deviceLookup`
 * state used to hold on its own. Kept as a DERIVED value so the render can keep
 * asking `deviceLookup === "checking"` / `"invalid"` while there is only ever one
 * piece of state to keep consistent.
 */
export type DeviceLookup = "idle" | "checking" | "invalid";

export function deviceLookupOf(state: DeviceState): DeviceLookup {
  if (state.checking) return "checking";
  return state.problem === "not_found" ? "invalid" : "idle";
}

/** The state a lookup starts from: everything the previous answer claimed is
 *  gone, because it was about a different code. */
export function checkingDevice(): DeviceState {
  return { ...DEVICE_IDLE, checking: true };
}

/** Dictionary key for a DeviceProblem, or null when there is nothing to say. */
export function deviceErrorKey(problem: DeviceProblem): "prof.deviceNotFound" | null {
  return problem === "not_found" ? "prof.deviceNotFound" : null;
}

/**
 * A monotonic request id. Held in a ref by the page, so the counter survives
 * re-renders and a slow response can be recognised as stale.
 */
export type LookupSequence = { current: number };

export const newLookupSequence = (): LookupSequence => ({ current: 0 });

/** Claim the next id. Anything settled under an older id is superseded. */
export function beginLookup(seq: LookupSequence): number {
  seq.current += 1;
  return seq.current;
}

export function isCurrentLookup(seq: LookupSequence, id: number): boolean {
  return seq.current === id;
}

/** What a single lookup can come back with. */
export type LookupOutcome = { consent: DeviceConsent } | { failed: true };

/**
 * Turn a settled request into the consent panel's next state — or into `null`
 * when a newer lookup has already been started.
 *
 * `null` is the whole guard: a superseded request has no way to express a
 * partial answer (clear the error but keep the card, or the reverse), which is
 * exactly the half-and-half state the bug produced.
 */
export function settleLookup(
  seq: LookupSequence,
  id: number,
  code: string,
  outcome: LookupOutcome,
): DeviceState | null {
  if (!isCurrentLookup(seq, id)) return null;
  if ("consent" in outcome) {
    return { consent: outcome.consent, problem: "", code, checking: false };
  }
  return { consent: null, problem: "not_found", code, checking: false };
}

/**
 * The state the panel renders when the field is cleared or edited.
 *
 * It is `DEVICE_IDLE` rather than "clear the problem, keep the card" on purpose:
 * once the input no longer holds the code the card was about, neither half of the
 * old answer is still true.
 */
export function clearedDevice(): DeviceState {
  return DEVICE_IDLE;
}
