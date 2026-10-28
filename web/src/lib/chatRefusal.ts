// A REFUSED tool call's sentence, resolved in the DASHBOARD'S language.
//
// THE DEFECT (card t_3839995c). The step a refusal is shown in carried its own
// human wording: `detail`, a sentence the SERVER wrote in Malay — the allowlist
// refusal ("alat \"update_product\" tidak dibenarkan dalam chat ini"), "RALAT: "
// plus an internal error, the proposal lane's message. The block's only localized
// part was its heading, so on an ENGLISH dashboard the transcript read:
//
//     assistant tried — update_product — alat "update_product" tidak dibenarkan dalam chat ini
//
// half English, half Malay, on a page whose every other failure sentence
// (chat.fail / chat.confirmFail / chat.proposalArgsInvalid) goes through the
// five-locale dictionary. The API cannot fix that by translating instead: it never
// receives the caller's language on a JSON call (issue #6177 — the same reason
// lib/chatConfirm.ts branches on a machine CODE rather than on the wire's prose).
//
// THE RULE. The server sends a machine code plus the values its sentence
// interpolates, exactly as a confirmation card sends title_key/value_key now:
//
//     detail_key      chat.refused.*   (why the call was refused)
//     detail_params   the values that sentence substitutes, e.g. {max: "3"}
//
// and `detail` — the prose field — no longer exists on the wire at all. That is
// what makes the class of defect unrepresentable rather than merely fixed: there is
// nothing left for a call site to print by accident. The wording the MODEL needs is
// not lost, because it never travelled here — it is the tool result, which is
// fenced into the prompt and never reaches a browser (api/internal/chat/prompt.go).
//
// WHY IT IS A LIB AND NOT AN INLINE EXPRESSION IN THE PAGE. This repo has no
// React/DOM unit runner (see src/lib/i18n.test.ts), so a decision taken inside the
// component can only be pinned by scanning its source — which cannot show that a
// key beats a missing entry, or that a refusal with no key renders as NOTHING
// rather than as the string "undefined". Kept here, the decision is EXECUTED by
// src/lib/chatRefusal.test.ts, and the same file pins the other half
// cross-language: every "chat.refused.*" key the Go runner can emit must exist in
// the dictionary in all five locales.

import type { Key } from "../components/i18n";
import type { Translate } from "./chatProposal";

/**
 * A step as the page receives it (api/internal/chat/chat.go's Step).
 *
 * `detail_key` is the dictionary key naming this step's detail, whichever status
 * carries it: for a REFUSED step, why it was refused; for a PROPOSED step, the
 * card that was prepared. There is deliberately no `detail` field — see the header.
 */
export type RefusedStep = {
  tool: string;
  status: string;
  detail_key?: string;
  detail_params?: Record<string, string>;
};

/**
 * The sentence for one step, in the dashboard's language.
 *
 * An absent (or empty) key renders as NOTHING rather than as a placeholder: the
 * page prints the tool name beside this, so a step with no key still says what the
 * assistant tried, and a fabricated sentence would be worse than a short line. A
 * key with no dictionary entry renders as the key itself — the dictionary half is
 * pinned by src/lib/chatRefusal.test.ts from the Go source.
 */
export function refusalText(s: RefusedStep, t: Translate): string {
  if (!s.detail_key) return "";
  return t(s.detail_key as Key, s.detail_params);
}

/**
 * The keys whose refusal means the assistant may not dispatch this call AT ALL:
 * a write tool (the only route to a write is a card the human confirms) and any
 * other name this chat excludes (an invented tool, `list_api_keys`). These are the
 * only refusals where "That action is not allowed here." is a true sentence.
 */
const FORBIDDEN_KEYS = ["chat.refused.notAllowed", "chat.refused.writeTool"];

/**
 * The keys whose refusal means "the assistant could not put THIS change
 * together" rather than "the assistant may not do that". Both are the proposal
 * lane: one is its argument gate, the other its per-answer budget.
 */
const PROPOSAL_LANE_KEYS = ["chat.refused.proposal", "chat.refused.proposalLimit"];

/**
 * The keys whose refusal means the call WAS allowed and did not go through: the
 * tool ran and declined (`chat.refused.tool` — a read it will not serve, an id
 * that does not exist), or the call failed before it reached the tool
 * (`chat.refused.error` — a transport failure, unparseable arguments). Neither is
 * a permission claim; both leave the assistant free to try again.
 */
const STEP_LANE_KEYS = ["chat.refused.tool", "chat.refused.error"];

/**
 * The heading for a refusal BLOCK, in the dashboard's language.
 *
 * THE DEFECT (t_7f1694cb). The block's heading was hard-coded to `chat.refused` —
 * "That action is not allowed here." — for every refusal. That is the right claim
 * for a forbidden tool, and the WRONG claim for a proposal whose ARGUMENTS were
 * refused: creating a form IS proposable, so a merchant whose natural
 * "create a form called …" request was rejected read a sentence telling them the
 * feature was off-limits, over an empty reply. The heading must not contradict the
 * rest of the page.
 *
 * THE RULE. The proposal heading when EVERY refused step is the proposal lane:
 * nothing in the block is a forbidden action, so nothing in it is "not allowed".
 * The generic heading when the block CONTAINS a forbidden call: that really is
 * something the assistant may not do, and understating it would be the same defect
 * in the other direction. An unkeyed step is not a known lane (it is an unknown), so
 * it keeps the generic heading.
 *
 * THE SECOND SHAPE OF THE SAME CLASS (t_66c1f8b1, measured live on v1.21.250). The
 * else branch above caught EVERY non-proposal key, so a block whose only refusal was
 * a TOOL-LEVEL one — `get_form` on an id that does not exist, i.e. a read the chat is
 * allowed to make, which ran and declined — was headed "That action is not allowed
 * here." over "The tool refused the call". The merchant was told the assistant may not
 * read their forms when the id was simply wrong. `chat.refused.error` (the call failed
 * before it reached the tool) is the same claim: allowed, did not go through. Both get
 * a heading that reports the attempt failed and claims nothing about permission.
 *
 * A block that mixes the two non-forbidden lanes (the live shape from that card's
 * parent: a declined `get_form` next to a refused `propose_write`) takes the same
 * heading: the block-wide claim must not be narrowed to the change when the block
 * also holds a step that is not a change.
 */
export function refusalHeading(steps: RefusedStep[], t: Translate): string {
  const keys = steps.map((s) => s.detail_key ?? "");
  const proposalLane = (k: string) => PROPOSAL_LANE_KEYS.includes(k);
  const stepLane = (k: string) => STEP_LANE_KEYS.includes(k);
  if (steps.length > 0 && keys.every(proposalLane)) {
    return t("chat.refusedHeading.proposal" as Key);
  }
  // Something in this block IS not allowed — say so, whatever else it holds.
  if (keys.some((k) => FORBIDDEN_KEYS.includes(k))) return t("chat.refused" as Key);
  const allAllowed = steps.length > 0 && keys.every((k) => proposalLane(k) || stepLane(k));
  return allAllowed ? t("chat.refusedHeading.step" as Key) : t("chat.refused" as Key);
}
