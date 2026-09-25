// Feedback notification state, as the /admin Feedback triage section reads it.
//
// WHY THIS IS A MODULE AND NOT INLINE IN page.tsx. A feedback notification is
// best-effort: the reply is stored and the email is handed to the delivery queue.
// The API now answers WHICH of those two happened (`notification`:
// queued|sent|failed|none) and marks a failure on the thread
// (notify_failed_at / notify_failed_kind / notify_failed_error). Two rules follow
// from that, and both are easy to get wrong in a JSX branch where nothing can
// execute them:
//
//   1. "sent" must NOT be shown for an outcome that is not a delivery. The
//      original defect was exactly this — the API answered {"status":"sent"}
//      unconditionally and the console reported the filer was notified. The
//      failure moved to a field, so the console has to read the field.
//   2. The marker must be visible on the thread AND actionable (a retry), not
//      merely a transient toast: an operator who scrolls away must still find it.
//
// This module owns the mapping so it can be run by `npm run test:unit`
// (src/lib/*.test.ts), while page.tsx owns only the rendering.
//
// WHAT IT DOES NOT COVER: it cannot execute React, so it does not observe the
// rendered banner or the click. The Go-side tests
// (api/internal/httpapi/feedback_notify_failure_test.go) drive the real routes,
// and the rendered result is checked in a browser.

// The outcome values the API publishes. Kept in step with the Go constants
// feedbackNotifyQueued/Sent/Failed/None/Skipped in
// api/internal/httpapi/rest_feedback.go; a rename on one side would silently make
// the console treat every reply as "unconfirmed", which is why the contract test
// pins both spellings.
export const NOTIFY_QUEUED = "queued";
export const NOTIFY_SENT = "sent";
export const NOTIFY_FAILED = "failed";
export const NOTIFY_NONE = "none";
// NOTIFY_SKIPPED means nothing was ATTEMPTED: the admin lane has a deliberate
// no-op exit when no super-admin recipient is configured, so no job was enqueued
// and no mail will arrive. It is NOT a delivery, and it is NOT a failure either —
// it is a configuration state, and the console must say so rather than imply a
// mail went out.
export const NOTIFY_SKIPPED = "skipped";

// The two marker kinds (feedback_threads_notify_failed_kind_chk).
export const NOTIFY_KIND_FILER = "filer";
export const NOTIFY_KIND_ADMIN = "admin";

export type NotifyOutcome = "queued" | "sent" | "failed" | "none" | "skipped" | "";

// delivered reports whether an outcome means the mail actually left the
// building. Anything else — including an unknown/absent value from an older or
// newer server — is NOT a delivery and must not be presented as one.
export const delivered = (outcome: string): boolean =>
  outcome === NOTIFY_QUEUED || outcome === NOTIFY_SENT;

// replyResult maps the `notification` field on an admin-reply response onto what
// the console should say. Three cases, and the third is the honest one:
//
//	ok:       a delivery happened → the normal success message
//	error:    the enqueue/send failed → say so, and point at the banner
//	unconfirmed: the field is missing or unrecognised → say the reply was stored
//	          WITHOUT claiming the filer was told, because that claim is the
//	          thing that was wrong in the first place
export type ReplyResultKind = "ok" | "error" | "unconfirmed";

export const replyResult = (
  outcome: string,
): { kind: ReplyResultKind; messageKey: string } => {
  if (outcome === NOTIFY_FAILED) {
    return { kind: "error", messageKey: "afb.repliedNotifyFailed" };
  }
  if (delivered(outcome)) {
    return { kind: "ok", messageKey: "afb.replied" };
  }
  return { kind: "unconfirmed", messageKey: "afb.repliedUnconfirmed" };
};

// retryResult maps the `notification` field on a retry response. ONLY a delivery
// (queued|sent) and the "nothing to retry" case (none) are successes:
//
//	queued|sent  the mail left the process → "Notification sent again."
//	none         there was no failure to retry → nothing changed, nothing wrong
//	failed       it went out and STILL failed → say so; the banner stays
//	skipped      nothing was ATTEMPTED (no configured recipient) → say that, not
//	             a delivery: no job was enqueued, so the success copy would be a
//	             claim no mail backs
//	unknown/""   an older or newer server, or a stripped field → must NOT claim a
//	             delivery. Same rule the reply path applies, and the reason is the
//	             same: "the filer was told" is the claim this module exists to
//	             stop inventing.
export const retryResult = (
  outcome: string,
): { kind: "ok" | "error" | "unconfirmed"; messageKey: string } => {
  if (outcome === NOTIFY_FAILED) {
    return { kind: "error", messageKey: "afb.notifyRetryFail" };
  }
  if (outcome === NOTIFY_SKIPPED) {
    return { kind: "unconfirmed", messageKey: "afb.notifyRetrySkipped" };
  }
  if (delivered(outcome) || outcome === NOTIFY_NONE) {
    return { kind: "ok", messageKey: "afb.notifyRetryDone" };
  }
  return { kind: "unconfirmed", messageKey: "afb.notifyRetryUnconfirmed" };
};

// notifyFailedWhoKey names the party that was not told, for the banner. An
// unknown kind falls back to the platform-team wording rather than rendering an
// empty label or a raw enum value at an operator.
export const notifyFailedWhoKey = (kind: string | undefined): string =>
  kind === NOTIFY_KIND_FILER ? "afb.notifyFailedFiler" : "afb.notifyFailedAdmin";

// hasNotifyFailure is the single predicate both markers (list badge and thread
// banner) key on, so they cannot disagree about what "broken" means.
export const hasNotifyFailure = (thread: {
  notify_failed_at?: string;
}): boolean => !!thread.notify_failed_at;

// notificationFailed is the narrower predicate for the thread where no
// notification was delivered at all (used to decide the banner's emphasis).
export const notificationFailed = (thread: {
  notify_failed_at?: string;
  notify_failed_kind?: string;
}): boolean => !!thread.notify_failed_at && !!thread.notify_failed_kind;
