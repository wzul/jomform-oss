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
