"use client";

// In-app feedback: every signed-in user can reach this page from the sidebar
// (the nav link is NOT admin-gated), file a report, and come back to read the
// reply in the same thread.
//
// The backend store is system-owned: a thread belongs to the PERSON who filed
// it and carries no account/workspace id, so this page never sends a
// ?workspace= scope. Its routes are the /v1/rest/feedback ones, which resolve
// the filer from the session — the client cannot ask for someone else's thread
// because there is no parameter with which to ask.
//
// A super-admin additionally has the triage view on /admin's Feedback tab; this
// page stays "my threads" for everyone, admins included, so an admin can still
// file and follow their own report.

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { Markdown } from "../../components/Markdown";
import { useI18n, type Key } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDateTime } from "../../lib/formatDate";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { translateApiErr } from "../../lib/apiErr";

type Thread = {
  id: string;
  subject: string;
  status: string;
  created_at: string;
  last_activity_at: string;
  message_count: number;
};

type Message = {
  id: string;
  author_kind: string;
  author_email?: string;
  body: string;
  created_at: string;
};

const MIN_BODY = 10;
const PAGE_SIZE = 20;

// The four statuses the API accepts (dbstore.FeedbackStatuses). Kept as one list
// so the badge classes and the filter chips cannot drift from the server's set.
const STATUSES = ["open", "in_progress", "resolved", "closed"] as const;

// statusKey maps a server status onto its dictionary key. A status the API adds
// later renders as its raw value rather than crashing the page.
function statusKey(status: string): Key {
  const k = `fb.status.${status}` as Key;
  return k;
}

function statusBadge(status: string) {
  switch (status) {
    case "open":
      return "bg-amber-100 text-amber-700";
    case "in_progress":
      return "bg-blue-100 text-blue-700";
    case "resolved":
      return "bg-emerald-100 text-emerald-700";
    case "closed":
      return "bg-slate-200 text-slate-600";
    default:
      return "bg-slate-100 text-slate-600";
  }
}

export default function FeedbackPage() {
  const { t } = useI18n();
  const { anon, user } = useSession();
  const router = useRouter();

  const [threads, setThreads] = useState<Thread[]>([]);
  const [total, setTotal] = useState(0);
  const [listErr, setListErr] = useState("");
  const [loading, setLoading] = useState(true);

  const [openId, setOpenId] = useState("");
  const [messages, setMessages] = useState<Message[]>([]);
  const [thread, setThread] = useState<Thread | null>(null);
  const [threadErr, setThreadErr] = useState("");

  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [sending, setSending] = useState(false);
  const [formMsg, setFormMsg] = useState("");
  const [formErr, setFormErr] = useState("");

  const [reply, setReply] = useState("");
  const [replying, setReplying] = useState(false);

  // ?thread=<id> opens that thread on mount. It is what the reply notification
  // email links to: the email says an answer arrived, and the reader should land
  // ON that answer rather than in a list to hunt through. A missing or unusable
  // id simply leaves the list — the parameter is a convenience, never an access
  // grant (the API still scopes every read to the session's own user).
  useEffect(() => {
    if (typeof window === "undefined") return;
    const id = new URLSearchParams(window.location.search).get("thread");
    if (id) void openThread(id);
    // Deliberately once, on mount: re-running on openThread's identity would
    // reopen the thread every time the list reloads and discard a draft reply.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Issue #503: an expired session gets an action, not a dead page.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const loadList = useCallback(async () => {
    setLoading(true);
    setListErr("");
    const r = await runLoad(
      () => fetchJson<{ items?: Thread[]; total?: number }>(
        `/api/rest/feedback?page=1&limit=${PAGE_SIZE}`),
      { onUnauth: goToLogin, message: t("fb.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setThreads([]);
      setTotal(0);
      setListErr(r.error);
      setLoading(false);
      return;
    }
    setThreads(Array.isArray(r.data.items) ? r.data.items : []);
    setTotal(typeof r.data.total === "number" ? r.data.total : 0);
    setLoading(false);
  }, [t, goToLogin]);

  const openThread = useCallback(async (id: string) => {
    setOpenId(id);
    setThreadErr("");
    setMessages([]);
    setThread(null);
    const r = await runLoad(
      () => fetchJson<{ thread: Thread; messages: Message[] }>(
        `/api/rest/feedback/${encodeURIComponent(id)}`),
      { onUnauth: goToLogin, message: t("fb.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setThreadErr(r.error);
      return;
    }
    setThread(r.data.thread);
    setMessages(Array.isArray(r.data.messages) ? r.data.messages : []);
  }, [t, goToLogin]);

  useEffect(() => {
    void loadList();
  }, [loadList]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setFormMsg("");
    setFormErr("");
    if (body.trim().length < MIN_BODY) {
      setFormErr(t("fb.tooShort"));
      return;
    }
    setSending(true);
    try {
      const r = await fetch("/api/rest/feedback", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ subject: subject.trim(), body: body.trim() }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (r.status === 429) {
        setFormErr(t("fb.rateLimited"));
        return;
      }
      if (!r.ok) {
        setFormErr(t("fb.sendFail"));
        return;
      }
      setSubject("");
      setBody("");
      setFormMsg(t("fb.sent"));
      await loadList();
    } catch {
      setFormErr(t("fb.sendFail"));
    } finally {
      setSending(false);
    }
  }

  async function sendReply(e: React.FormEvent) {
    e.preventDefault();
    if (!openId || reply.trim().length < MIN_BODY) return;
    setReplying(true);
    setThreadErr("");
    try {
      const r = await fetch(`/api/rest/feedback/${encodeURIComponent(openId)}/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: reply.trim() }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        setThreadErr(t("fb.sendFail"));
        return;
      }
      setReply("");
      await openThread(openId);
      await loadList();
    } catch {
      setThreadErr(t("fb.sendFail"));
    } finally {
      setReplying(false);
    }
  }

  return (
    <Shell active="feedback">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("fb.title")}</h1>
            <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("fb.subtitle")}</p>
          </div>
          {/* Inline "?" doc link (DOCUMENTATION PARITY). */}
          <a
            href="/docs/feedback"
            target="_blank"
            rel="noopener noreferrer"
            title={t("fb.docs")}
            className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            ?
          </a>
        </div>

        {user?.is_super_admin && (
          <p className="mt-3 rounded-md bg-indigo-50 px-3 py-2 text-xs text-[var(--jf-primary-on-tint)]">
            {t("fb.adminHint")}{" "}
            <a href="/admin?tab=feedback" className="underline decoration-dotted underline-offset-2">
              /admin
            </a>
          </p>
        )}

        <div className="mt-6 grid gap-6 lg:grid-cols-2">
          {/* ── file a new thread ── */}
          <section className="jf-card p-6">
            <h2 className="text-base font-semibold">{t("fb.new")}</h2>
            <form onSubmit={submit} className="mt-4 space-y-3">
              <div>
                <label htmlFor="fb-subject" className="text-xs font-medium text-[var(--jf-muted)]">
                  {t("fb.subject")}
                </label>
                <input
                  id="fb-subject"
                  value={subject}
                  onChange={(e) => setSubject(e.target.value)}
                  maxLength={200}
                  placeholder={t("fb.subjectPh")}
                  className="jf-input mt-1"
                />
              </div>
              <div>
                <label htmlFor="fb-body" className="text-xs font-medium text-[var(--jf-muted)]">
                  {t("fb.body")}
                </label>
                <textarea
                  id="fb-body"
                  value={body}
                  onChange={(e) => setBody(e.target.value)}
                  rows={5}
                  maxLength={5000}
                  placeholder={t("fb.bodyPh")}
                  className="jf-input mt-1"
                />
                <p className="mt-1 text-[11px] text-[var(--jf-muted)]">{t("fb.navHint")}</p>
              </div>
              {formErr && (
                <p role="alert" className="text-xs text-red-700">{formErr}</p>
              )}
              {formMsg && <p className="text-xs text-[var(--jf-primary)]">{formMsg}</p>}
              <button
                type="submit"
                disabled={sending}
                className="jf-btn-primary disabled:opacity-50"
              >
                {sending ? t("fb.sending") : t("fb.submit")}
              </button>
            </form>
          </section>

          {/* ── the user's own threads ── */}
          <section className="jf-card p-6">
            <h2 className="text-base font-semibold">
              {t("fb.mine")} {total > 0 && <span className="text-[var(--jf-muted)]">({total})</span>}
            </h2>

            {listErr && (
              <p role="alert" className="mt-3 text-xs text-red-700">
                {listErr}
                <button
                  type="button"
                  onClick={() => void loadList()}
                  className="ml-2 underline decoration-dotted underline-offset-2"
                >
                  {t("common.retry")}
                </button>
              </p>
            )}

            {loading ? (
              <p className="mt-3 text-sm text-[var(--jf-muted)]">…</p>
            ) : threads.length === 0 ? (
              <p className="mt-3 text-sm text-[var(--jf-muted)]">{t("fb.empty")}</p>
            ) : (
              <ul className="mt-3 divide-y divide-[var(--jf-border)]">
                {threads.map((th) => (
                  <li key={th.id}>
                    <button
                      type="button"
                      onClick={() => void openThread(th.id)}
                      className={`flex w-full flex-wrap items-center justify-between gap-2 px-1 py-3 text-left hover:bg-slate-50 ${
                        openId === th.id ? "bg-slate-50" : ""
                      }`}
                    >
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">
                          {th.subject || t("fb.noSubject")}
                        </span>
                        <span className="mt-0.5 block text-[11px] text-[var(--jf-muted)]">
                          {formatDateTime(th.last_activity_at)} · {t("fb.messages", { n: th.message_count })}
                        </span>
                      </span>
                      <span className={`shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium ${statusBadge(th.status)}`}>
                        {t(statusKey(th.status))}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}

            {threadErr && (
              <p role="alert" className="mt-3 text-xs text-red-700">{threadErr}</p>
            )}

            {/* ── the selected thread + its two-way conversation ── */}
            {thread && (
              <div className="mt-5 border-t border-[var(--jf-border)] pt-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="text-sm font-semibold">
                    {thread.subject || t("fb.noSubject")}
                  </h3>
                  <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${statusBadge(thread.status)}`}>
                    {t(statusKey(thread.status))}
                  </span>
                </div>

                <ul className="mt-3 space-y-3">
                  {messages.map((m) => {
                    const isAdmin = m.author_kind === "admin";
                    return (
                      <li
                        key={m.id}
                        className={`rounded-lg border px-3 py-2 ${
                          isAdmin
                            ? "border-indigo-100 bg-indigo-50/50"
                            : "border-[var(--jf-border)] bg-white"
                        }`}
                      >
                        <div className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-[var(--jf-muted)]">
                          <span className="font-medium text-[var(--jf-text)]">
                            {isAdmin ? t("fb.whoTeam") : t("fb.whoUser")}
                          </span>
                          <span>{formatDateTime(m.created_at)}</span>
                        </div>
                        <div className="mt-1 text-sm">
                          {/* The assistant files thread bodies with Markdown in
                              them, and an admin replies from the same surface —
                              one renderer with /chat so the two cannot drift. */}
                          <Markdown text={m.body} variant="plain" />
                        </div>
                      </li>
                    );
                  })}
                </ul>

                {thread.status !== "closed" && (
                  <form onSubmit={sendReply} className="mt-4">
                    <label htmlFor="fb-reply" className="text-xs font-medium text-[var(--jf-muted)]">
                      {t("fb.reply")}
                    </label>
                    <textarea
                      id="fb-reply"
                      value={reply}
                      onChange={(e) => setReply(e.target.value)}
                      rows={3}
                      maxLength={5000}
                      placeholder={t("fb.replyPh")}
                      className="jf-input mt-1"
                    />
                    <button
                      type="submit"
                      disabled={replying || reply.trim().length < MIN_BODY}
                      className="jf-btn-primary mt-2 disabled:opacity-50"
                    >
                      {t("fb.replySend")}
                    </button>
                  </form>
                )}
              </div>
            )}

            {!thread && !threadErr && !loading && threads.length > 0 && (
              <p className="mt-3 text-xs text-[var(--jf-muted)]">{t("fb.pickOne")}</p>
            )}
          </section>
        </div>

        {/* A status legend, so the badge colours are explained rather than
            guessed at — the user has no other place to learn them. */}
        <div className="mt-6 flex flex-wrap gap-2">
          {STATUSES.map((s) => (
            <span
              key={s}
              className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${statusBadge(s)}`}
            >
              {t(statusKey(s))}
            </span>
          ))}
        </div>
      </div>
    </Shell>
  );
}
