"use client";

// In-app assistant (phase 1) — a chat about the caller's OWN data that can file
// feedback and can change nothing else.
//
// THE UI'S JOB IS TO MAKE THE BOUNDARY VISIBLE, not just to send messages:
//
//   1. It asks GET /v1/rest/chat before showing anything. That flag means "this
//      deployment can actually answer": the server reports available:false when
//      there is no assistant wired OR when its model has no credential
//      configured, and the page says the assistant is unavailable instead of
//      offering a box that only ever errors (dev and bare mode are real, and a
//      box that always fails is worse than no box).
//
//      A key that is CONFIGURED but REJECTED is the same answer, discovered one
//      step later: the status flag is a presence check (only the provider can
//      say a key still works), so the POST reports 503 and the branch below
//      treats it exactly like available:false — the box closes rather than
//      every message dying.
//   2. It shows a read-only badge and one line stating what the assistant can
//      and cannot do, with an inline "?" link into the docs page. That is not
//      decoration: "it cannot delete your form" is the design, and a user who
//      asks it to delete a form must be able to read why it will not.
//   3. A FILED FEEDBACK is announced in the transcript with a link to
//      /feedback. Filing must be visible at the moment it happens — the thread
//      also appears on the Feedback page, and a user discovering it later there
//      would be the wrong order of events.
//   4. A REFUSED tool call is shown, not hidden. The server returns the steps it
//      attempted; rendering them is how the user learns the assistant tried
//      something and was not allowed, rather than experiencing it as a broken
//      answer.
//
// The history it sends is the transcript the user can see, capped to the last
// MAX_HISTORY turns. The server re-caps and drops any client-forged system/tool
// turn, so the wire shape here is a convenience, not a trust boundary.

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { Markdown } from "../../components/Markdown";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { fetchJson, runLoad } from "../../lib/wsfetch";

type Turn = { role: "user" | "assistant"; content: string };

type Step = { tool: string; status: string; detail?: string };

type Filed = { thread_id: string; subject: string; excerpt: string };

type Limits = {
  messages_per_hour: number;
  burst: number;
  max_message_chars: number;
  max_history_turns: number;
  max_tool_steps: number;
};

type ChatReply = {
  reply: string;
  steps?: Step[];
  filed?: Filed;
  limits?: Limits;
};

type Msg = {
  role: "user" | "assistant";
  content: string;
  // files / steps are only present on an assistant turn that produced them.
  filed?: Filed;
  refused?: Step[];
};

// Mirrors the server's cap (chatMaxHistory in rest_chat.go). The server drops
// older turns; sending more than it keeps would mean the user's own view of the
// conversation and the model's differ for no benefit.
const MAX_HISTORY = 32;

export default function ChatPage() {
  const { t } = useI18n();
  const { anon } = useSession();
  const router = useRouter();

  const [available, setAvailable] = useState<boolean | null>(null);
  const [limits, setLimits] = useState<Limits | null>(null);
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [err, setErr] = useState("");

  const endRef = useRef<HTMLDivElement>(null);

  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Availability first: with no LLM the box must not be offered at all.
  useEffect(() => {
    void (async () => {
      const r = await runLoad(
        () => fetchJson<{ available?: boolean; limits?: Limits }>("/api/rest/chat"),
        { onUnauth: goToLogin, message: t("chat.unavailable") },
      );
      if (!r.ok) {
        setAvailable(false);
        return;
      }
      setAvailable(r.data.available === true);
      if (r.data.limits) setLimits(r.data.limits);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goToLogin]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ block: "end" });
  }, [msgs.length, sending]);

  async function send(e: React.FormEvent) {
    e.preventDefault();
    const text = input.trim();
    if (!text || sending) return;
    setErr("");

    const cap = limits?.max_message_chars ?? 0;
    if (cap > 0 && text.length > cap) {
      setErr(t("chat.tooLong"));
      return;
    }

    // The transcript the user can see, minus the filing/refusal cards (which are
    // UI affordances, not conversation turns).
    const history: Turn[] = msgs
      .slice(-MAX_HISTORY)
      .map((m) => ({ role: m.role, content: m.content }));

    setMsgs((prev) => [...prev, { role: "user", content: text }]);
    setInput("");
    setSending(true);
    try {
      const r = await fetch("/api/rest/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: text, history }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (r.status === 429) {
        setErr(t("chat.rateLimited"));
        return;
      }
      if (r.status === 503) {
        setAvailable(false);
        setErr(t("chat.unavailable"));
        return;
      }
      if (!r.ok) {
        setErr(t("chat.fail"));
        return;
      }
      const d = (await r.json()) as ChatReply;
      if (d.limits) setLimits(d.limits);
      const refused = (d.steps ?? []).filter((s) => s.status === "refused");
      setMsgs((prev) => [
        ...prev,
        {
          role: "assistant",
          content: d.reply ?? "",
          filed: d.filed,
          refused: refused.length ? refused : undefined,
        },
      ]);
    } catch {
      setErr(t("chat.fail"));
    } finally {
      setSending(false);
    }
  }

  const disabled = sending || available === false;

  return (
    <Shell active="chat">
      <div className="flex w-full flex-1 flex-col px-6 py-8">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight">
              {t("chat.title")}
              <span className="rounded-full bg-slate-200 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-600">
                {t("chat.readOnlyBadge")}
              </span>
            </h1>
            <p className="mt-1 max-w-2xl text-sm text-[var(--jf-muted)]">{t("chat.subtitle")}</p>
          </div>
          {/* Inline "?" doc link (DOCUMENTATION PARITY). */}
          <a
            href="/docs/chat"
            target="_blank"
            rel="noopener noreferrer"
            title={t("chat.docs")}
            className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-[11px] font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            ?
          </a>
        </div>

        <div className="mt-6 flex min-h-0 flex-1 flex-col rounded-lg border border-[var(--jf-border)] bg-white">
          <div
            className="min-h-[18rem] flex-1 space-y-3 overflow-y-auto p-4"
            role="log"
            aria-live="polite"
          >
            {available === false && (
              <p className="text-sm text-[var(--jf-muted)]">{t("chat.unavailable")}</p>
            )}
            {available !== false && msgs.length === 0 && (
              <p className="text-sm text-[var(--jf-muted)]">{t("chat.empty")}</p>
            )}
            {msgs.map((m, i) => (
              <div key={i} className={m.role === "user" ? "flex justify-end" : "flex justify-start"}>
                <div
                  className={`max-w-[46rem] min-w-0 rounded-lg px-3 py-2 text-sm ${
                    m.role === "user"
                      ? "bg-[var(--jf-primary)] text-white"
                      : "bg-slate-50 text-[var(--jf-text)]"
                  }`}
                >
                  {/* The answer is rendered as ELEMENTS, not raw text: the model
                      writes Markdown, and showing its markers was the reported
                      defect. One shared renderer with /feedback. */}
                  <Markdown text={m.content} variant={m.role === "user" ? "user" : "assistant"} />
                </div>
              </div>
            ))}

            {/* A filing is announced in the transcript, with a way to go read it. */}
            {msgs.map((m, i) =>
              m.filed ? (
                <div
                  key={`f${i}`}
                  className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
                >
                  <span className="font-medium">{t("chat.filed")}</span>
                  {m.filed.subject && (
                    <span className="block truncate opacity-80">{m.filed.subject}</span>
                  )}
                  <a
                    href="/feedback"
                    className="mt-1 inline-block underline decoration-dotted underline-offset-2"
                  >
                    {t("chat.viewThread")}
                  </a>
                </div>
              ) : null,
            )}

            {/* A refusal is shown: the user should learn the assistant TRIED. */}
            {msgs.map((m, i) =>
              m.refused ? (
                <div key={`r${i}`} className="rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800">
                  <span className="font-medium">{t("chat.refused")}</span>
                  {m.refused.map((s) => (
                    <span key={s.tool} className="mt-1 block opacity-80">
                      {s.tool}
                      {s.detail ? ` — ${s.detail}` : ""}
                    </span>
                  ))}
                </div>
              ) : null,
            )}

            {sending && <p className="text-xs text-[var(--jf-muted)]">{t("chat.thinking")}</p>}
            <div ref={endRef} />
          </div>

          {err && (
            <p role="alert" className="px-4 text-xs text-red-500">
              {err}
            </p>
          )}

          <form onSubmit={send} className="flex items-end gap-2 border-t border-[var(--jf-border)] p-3">
            <label htmlFor="chat-input" className="sr-only">
              {t("chat.placeholder")}
            </label>
            <textarea
              id="chat-input"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                // Enter sends; Shift+Enter is a newline. A chat box that cannot
                // take a newline cannot take a pasted question either.
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void send(e);
                }
              }}
              rows={2}
              disabled={disabled}
              maxLength={limits?.max_message_chars || 2000}
              placeholder={t("chat.placeholder")}
              className="jf-input flex-1 resize-none disabled:opacity-50"
            />
            <button type="submit" disabled={disabled || !input.trim()} className="jf-btn-primary disabled:opacity-50">
              {sending ? t("chat.thinking") : t("chat.send")}
            </button>
          </form>
        </div>

        {limits && (
          <p className="mt-2 text-[11px] text-[var(--jf-muted)]">
            {t("chat.limitNote", { n: limits.messages_per_hour })}
          </p>
        )}
      </div>
    </Shell>
  );
}
