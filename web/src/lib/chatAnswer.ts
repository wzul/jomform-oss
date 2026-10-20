// THE ASSISTANT'S ANSWER WHEN THE MODEL WROTE NOTHING (card t_7f1694cb).
//
// THE DEFECT, measured on prod v1.21.149. A merchant typed a plain request —
// "Create a form called My New Form with one text field for the customer's name."
// — and the transcript's whole content was an amber refusal block. The wire was:
//
//   steps: propose_write:refused, propose_write:refused
//   reply: ""
//
// `reply: ""` is not a short answer, it is NO answer: the page rendered an empty
// assistant bubble, so the merchant was told their change could not be prepared
// and nothing else. The model had been given the refusal (correctly — it is the
// one that can correct the call) and chose to say nothing, which it is free to do.
//
// THE RULE. The server now names a dictionary sentence instead of passing the
// silence through (api/internal/chat/chat.go, `Result.ReplyKey`), because the API
// does not know this visitor's language (issue #6177) and a sentence built there
// would reach every locale in one language — the same rule the refusal block
// follows (t_3839995c).
//
// WHY THIS IS A LIB. This repo has no React/DOM unit runner, so a decision taken
// inside app/chat/page.tsx could only be pinned by scanning its source — which
// cannot show that a real answer BEATS the fallback, that a missing key renders as
// nothing rather than as the string "undefined", or that the key exists in all five
// locales. Here it is executed by src/lib/chatAnswer.test.ts, which also reads
// `chat.reply.*` out of the Go source so the two halves cannot drift: a key the
// server can emit with no dictionary entry would render as the raw key, at HTTP
// 200, with nothing erroring.

import type { Key } from "../components/i18n";
import type { Translate } from "./chatProposal";

/**
 * The text of the assistant's bubble.
 *
 * The model's own words win whenever it wrote any: the fallback is a floor, never
 * a replacement, and a canned sentence in place of a real answer would be a worse
 * product than the bug. Whitespace-only prose counts as silence (the runner makes
 * the same call), so a model that emitted "\n\n" cannot leave an empty bubble.
 * No prose and no key is empty string — the page renders nothing rather than
 * "undefined", which is the shape the defect would take if this module guessed.
 */
export function answerText(
  reply: string | undefined,
  replyKey: string | undefined,
  t: Translate,
): string {
  const prose = reply ?? "";
  if (prose.trim()) return prose;
  if (replyKey) return t(replyKey as Key);
  return "";
}
