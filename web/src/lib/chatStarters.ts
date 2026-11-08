// The empty-state starter questions (card t_148d7100).
//
// THE DEFECT. The empty chat was one sentence of prose ("Ask something like
// \"which of my forms are live?\"…") with nothing to click. It TELLS a user what
// they could type instead of letting them try it, which is the difference between
// a demo and a product — and it was the whole of the empty state.
//
// WHY THE CHIPS ARE KEYS, NOT SENTENCES. A chip SENDS its own text as the
// message, so it has to exist in the visitor's language — the model reads the
// question, and every locale's dictionary entry is what gets sent. A literal here
// would send English to a Malay dashboard.
//
// THEY ARE ALSO A CONTRACT ABOUT WHAT THE ASSISTANT CAN ANSWER. Each one names a
// read the chat's allowlist really serves (forms, sales, submissions, and the
// assistant's own scope), so a chip cannot promise a question the server refuses.
// src/lib/chatStarters.test.ts pins that against the Go allowlist.
export const CHAT_STARTERS = [
  "chat.starter.forms",
  "chat.starter.sales",
  "chat.starter.submissions",
  "chat.starter.help",
] as const;
