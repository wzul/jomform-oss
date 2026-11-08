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
// The one string on the card that deliberately has NO key is `note`: it is the
// ASSISTANT's own wording, so it does not exist until the model writes it and
// there is nothing for this module to look up. It is NOT free to be any language,
// though (card t_ec87d6c7): the note is printed on the card, so it follows the
// card's language — the dashboard's — and the server tells the model which
// language that is in the system prompt (see DashboardLangFrom in
// api/internal/chat/lang.go). The page therefore renders it verbatim; making the
// page guess at a language would be a second, worse answer to a question the
// server already answers.

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

/**
 * The human name of a TOOL, in the dashboard's language (card t_ec87d6c7).
 *
 * WHY THIS EXISTS. The card printed the tool it would run, and the refusal block
 * printed the tool that was refused, both as the RAW server name:
 *
 *     upsert_form
 *     propose_write — The assistant could not prepare that change …
 *
 * `upsert_form` and `propose_write` are internal identifiers, not language. Wan's
 * rule for the card is that it must read as the merchant's own UI, and an
 * identifier in snake_case is not a word in ANY locale — it is the one string on
 * the card that cannot be translated because it was never meant to be read.
 *
 * The mapping is `chat.tool.<name>` in the dictionary (five locales, next to the
 * card's other keys). A name the dictionary does not cover yet is HUMANISED
 * rather than passed through — `some_new_tool` becomes "Some new tool" — so a
 * tool added after this file was written cannot put an identifier on the screen.
 * It is English words in that case, which is the dashboard's default language and
 * still language, not a token; the guard in chatProposal.test.ts fails the suite
 * before it can ship.
 */
export function toolLabel(tool: string, t: Translate): string {
  const name = typeof tool === "string" ? tool.trim() : "";
  if (!name) return "";
  const key = `chat.tool.${name}`;
  const translated = t(key as Key);
  // t() answers with the KEY ITSELF when the dictionary has no entry; that is the
  // one answer this must never return.
  if (translated && translated !== key) return translated;
  return humanToolName(name);
}

/**
 * The fallback for a tool the dictionary has not named yet: separators become
 * spaces and the first letter is capitalised, so the result can never contain an
 * underscore (or a dash) or start with a lowercase identifier.
 */
export function humanToolName(name: string): string {
  const words = name.replace(/[_-]+/g, " ").trim().toLowerCase();
  if (!words) return "";
  return words.charAt(0).toUpperCase() + words.slice(1);
}
