// The confirmation card's wording, resolved in the DASHBOARD'S language.
//
// THE DEFECT (card t_30ffa28a). The server's confirmation card carried its own
// human wording: a title, a summary and a field label per row, plus sentences it
// COMPOSED out of a value ("3 fields", "yes", "no limit (infinite)"). All of them
// were Malay literals, because the API does not receive the caller's language on a
// JSON call (issue #6177 — the same reason web/src/lib/chatConfirm.ts reads a
// machine CODE instead of the wire's prose). The card's chrome was localized by the
// dictionary, so on an English dashboard one card read:
//
//     NEEDS YOUR CONFIRMATION   Kemas kini produk
//     Medan produk yang disenaraikan di bawah akan ditukar. …
//     Produk   QA Renamed Product …
//     Nama     QA Renamed Product …
//     [Confirm] [Cancel]   Nothing has changed yet.
//
// half English, half Malay — on the one surface in the product where the wording IS
// the safety mechanism, because a merchant who cannot read the description of the
// change is being asked to approve it.
//
// THE RULE. The API now sends KEYS plus the merchant's own DATA:
//
//     title_key    chat.proposal.title.<tool>
//     summary_key  chat.proposal.summary.<tool>
//     label_key    chat.proposal.label.<what>   (label_raw only for an argument or
//                  theme key the dictionary does not cover yet — an identifier, not
//                  a sentence)
//     value_key    chat.proposal.value.<what>   with value_params interpolated
//     value        shown verbatim: an id, a name, a number, an amount
//
// These four functions are the ONLY way a card becomes text on the page, and they
// do not have a branch that returns the server's own sentence — because the server
// no longer sends one. That is what makes the class of defect unrepresentable
// rather than merely fixed: there is nothing left on the wire for a call site to
// print by accident.
//
// WHY IT IS A LIB AND NOT AN INLINE EXPRESSION IN THE PAGE. This repo has no
// React/DOM unit runner (see src/lib/i18n.test.ts), so a decision taken inside the
// component can only be pinned by scanning its source — which cannot show that a
// key wins over a raw label, or that absent data renders as nothing rather than the
// string "undefined". Kept here, the decisions are EXECUTED by
// src/lib/chatProposal.test.ts, and the same file pins the other half cross-language:
// every "chat.proposal.*" key the Go server can emit must exist in the dictionary
// in all five locales.
//
// The one string on the card that deliberately has NO key is `note`: that is the
// ASSISTANT's own wording, in the language of the conversation it is having (the
// same language as its reply in the transcript above the card), not the server's
// wording — and the prompt answers in the language the user wrote in. See the
// ProposalCard doc comment in api/internal/chat/proposal.go.

import type { Key } from "../components/i18n";

/**
 * The `t()` this module needs: any dictionary key, with the optional parameters a
 * composed sentence interpolates. Structural (rather than the context's own type)
 * so the Node test runner can drive it with a plain function.
 */
export type Translate = (key: Key, params?: Record<string, string | number>) => string;

/** One row of the card, exactly as the API sends it (api/internal/chat/proposal.go). */
export type ProposalField = {
  /** The dictionary key naming this row. Empty only when `label_raw` is set. */
  label_key: string;
  /** An argument (or theme) key shown verbatim, when the dictionary has no entry. */
  label_raw?: string;
  /** The merchant's own data — shown as-is. */
  value?: string;
  /** A sentence the server composed; rendered through `t()`. */
  value_key?: string;
  /** The numbers and data interpolated into `value_key`'s sentence. */
  value_params?: Record<string, string>;
};

/** The card as the page receives it. */
export type ProposalCardData = {
  token: string;
  args_hash: string;
  tool: string;
  title_key: string;
  summary_key: string;
  fields?: ProposalField[];
  /** The assistant's own wording, in the conversation's language — see the header. */
  note?: string;
  expires_at: string;
  reversible: boolean;
};

/**
 * The label for one field row: the dictionary entry, or the raw argument key the
 * server could not map (a theme key added after the dictionary was written — shown
 * under its own name rather than dropped, because a card that hides a field lets a
 * user confirm something they were not shown).
 */
export function proposalLabel(f: ProposalField, t: Translate): string {
  if (f.label_key) return t(f.label_key as Key);
  if (f.label_raw) return f.label_raw;
  return "";
}

/**
 * The value for one field row. A key means the server composed a sentence around a
 * number or a flag ("3 fields", "yes"), so it is rendered through `t()` with the
 * card's own parameters; otherwise the value is the merchant's data and is shown
 * verbatim.
 */
export function proposalValue(f: ProposalField, t: Translate): string {
  if (f.value_key) return t(f.value_key as Key, f.value_params);
  return f.value ?? "";
}

/** The card's title, in the dashboard's language. */
export function proposalTitle(p: ProposalCardData, t: Translate): string {
  return p.title_key ? t(p.title_key as Key) : "";
}

/** The card's one-sentence summary, in the dashboard's language. */
export function proposalSummary(p: ProposalCardData, t: Translate): string {
  return p.summary_key ? t(p.summary_key as Key) : "";
}
