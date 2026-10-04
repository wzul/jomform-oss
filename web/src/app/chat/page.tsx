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
//   5. EVERY call carries the active workspace (the `?workspace=` query the
//      shared workspaceQuery() helper builds). The chat is one of the dashboard
//      surfaces that resolve the account from the SELECTED workspace, and it is
//      the only one that can WRITE through a confirmation — so an unscoped call is
//      not a read-scope wobble here: the server falls back to the user's FIRST
//      owned account, the assistant names that account's product on the card, and
//      confirming the card edits it. A merchant with two workspaces would be shown
//      a card for the account they are not looking at.
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
import { fetchJson, runLoad, classifyResponseFailure, classifyStatus } from "../../lib/wsfetch";
import { translateApiErr } from "../../lib/apiErr";
import { confirmDecisionFor } from "../../lib/chatConfirm";
import { dropCurrentWorkspaceScope, workspaceQuery } from "../../lib/workspace";

type Turn = { role: "user" | "assistant"; content: string };

type Step = { tool: string; status: string; detail?: string };

type Filed = { thread_id: string; subject: string; excerpt: string };

type Limits = {
  messages_per_hour: number;
  burst: number;
  max_message_chars: number;
  max_history_turns: number;
  max_tool_steps: number;
  max_proposals_per_message: number;
  proposal_ttl_seconds: number;
  confirms_per_hour: number;
  confirm_burst: number;
};

type ProposalField = { label: string; value: string };

// A confirmation card (phase 2). It carries the opaque token, which is the ONLY
// thing that can authorise the write — and the args_hash, which binds the
// execution to the arguments shown here. Neither is ever sent to the model.
type Proposal = {
  token: string;
  args_hash: string;
  tool: string;
  title: string;
  summary: string;
  fields: ProposalField[];
  note?: string;
  expires_at: string;
  reversible: boolean;
};

type ChatReply = {
  reply: string;
  steps?: Step[];
  filed?: Filed;
  proposals?: Proposal[];
  limits?: Limits;
};

// A card's lifecycle in the UI. "pending" is the only state where the buttons
// exist: a confirmed or cancelled card is spent server-side, so offering the
// buttons again would produce a 409 the user cannot act on.
//
// "scopeRequired" is NOT a failure of the change, and it is deliberately not
// folded into "failed". The server refuses an unscoped call BEFORE it reads the
// token (rest_scope.go's ambiguous-scope path), so nothing was attempted and the
// token is still live. A card that said "The change could not be applied. Please
// try again." would be instructing the user to repeat an action that cannot work
// without a workspace — and it said that underneath an alert that had already
// told them to choose one (t_790b1016).
type ProposalState =
  | "pending"
  | "confirming"
  | "cancelling"
  | "done"
  | "cancelled"
  | "stale"
  | "unexecutable"
  | "scopeRequired"
  | "failed";

type Msg = {
  role: "user" | "assistant";
  content: string;
  // files / steps / proposals are only present on an assistant turn that
  // produced them.
  filed?: Filed;
  refused?: Step[];
  proposals?: Proposal[];
  // states maps a proposal token to what happened to it.
  states?: Record<string, ProposalState>;
};

// Mirrors the server's cap (chatMaxHistory in rest_chat.go). The server drops
// older turns; sending more than it keeps would mean the user's own view of the
// conversation and the model's differ for no benefit.
const MAX_HISTORY = 32;

export default function ChatPage() {
  const { t } = useI18n();
  const { anon, workspace } = useSession();
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
        () =>
          fetchJson<{ available?: boolean; limits?: Limits }>(
            `/api/rest/chat${workspaceQuery()}`,
          ),
        { onUnauth: goToLogin, message: t("chat.unavailable"), errorKey: (b: unknown) => translateApiErr(b, t) },
      );
      if (!r.ok) {
        // A "scope" failure is the TENANCY refusal for a stale `?workspace=`
        // (t_da4b2031): runLoad has already dropped the slug, so the right move is
        // to say what happened and let the re-probe below run once the session
        // publishes the reconciled workspace — NOT to close the box, and above all
        // NOT to navigate away. It is reported with the same copy the missing-scope
        // 400 uses, because the user's action is the same.
        if (r.kind === "scope") {
          setErr(t("chat.scopeRequired"));
          return;
        }
        // A 400 here is the WORKSPACE-SCOPE refusal (rest_scope.go's
        // `scopeAmbiguous`: an unscoped call from a session that can reach more
        // than one workspace), and it is NOT the same answer as
        // available:false. The assistant is available — this request simply
        // carried no scope for the server to resolve an account from — so
        // reporting "The assistant is not available right now." tells the user
        // something false, and it closes the box for a condition the user fixes
        // by choosing a workspace. It is the same 400 the message path already
        // separates out in send(); the probe consumed it as a generic failure
        // and was the one call site left saying the wrong thing (t_997bb646).
        //
        // WHY THERE IS NO INLINE FETCH AND NO NEW runLoad OPTION: the outcome
        // runLoad already returns carries the `status` (LoadOutcome), so the
        // branch needs nothing new — and reading it here leaves runLoad's
        // 401/403 → onUnauth and the API-code translation untouched for every
        // other page. The GET route has exactly one 400 (the scope gate); a
        // genuinely unavailable deployment answers 200 with available:false
        // and a rejected credential answers 503 on the message path, so this
        // branch cannot swallow either.
        if (r.status === 400) {
          setErr(t("chat.scopeRequired"));
          return;
        }
        setAvailable(false);
        return;
      }
      // The probe succeeded, so any scope message from THIS probe is resolved:
      // clear it, or the user keeps reading "choose a workspace first" over a page
      // that is working (the stale-slug path above). It runs only on mount and on
      // a workspace change, so it cannot wipe an unrelated message mid-read.
      setErr("");
      setAvailable(r.data.available === true);
      if (r.data.limits) setLimits(r.data.limits);
    })();
    // `workspace` is a dependency (t_da4b2031): on a stale slug the first probe is
    // refused, the slug is dropped, the session then publishes a workspace the
    // caller really has, and THIS is what makes the page probe again for it
    // instead of sitting on the refusal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [goToLogin, workspace]);

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
      const r = await fetch(`/api/rest/chat${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: text, history }),
      });
      // ONE decision for the failure statuses, shared with runLoad: a 401 (or an
      // uncoded 403) is a dead session; a 403 that NAMES the tenancy code
      // (workspace.notFound) is a STALE SCOPE, and treating it as a dead session
      // is exactly the defect this card fixes — the merchant was bounced to
      // /login for a slug in their own localStorage (t_da4b2031). The scope case
      // re-reads the reconciled slug instead of navigating away, and says what to
      // do, so no data is lost and no session is thrown away.
      if (!r.ok) {
        const failure = await classifyResponseFailure(r);
        if (failure === "scope") {
          setErr(t("chat.scopeRequired"));
          return;
        }
        if (failure === "unauth") {
          goToLogin();
          return;
        }
      }
      if (r.status === 429) {
        setErr(t("chat.rateLimited"));
        return;
      }
      // 400 = the request itself was refused (a missing/unusable workspace scope),
      // NOT a dead session: the 401/403 rule above deliberately does not reach
      // here. Showing the generic failure would hide the one thing the user can
      // act on (choose a workspace), so the server's scope refusal gets its own
      // copy.
      if (r.status === 400) {
        setErr(t("chat.scopeRequired"));
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
      const proposals = d.proposals ?? [];
      const states: Record<string, ProposalState> = {};
      for (const p of proposals) states[p.token] = "pending";
      setMsgs((prev) => [
        ...prev,
        {
          role: "assistant",
          content: d.reply ?? "",
          filed: d.filed,
          refused: refused.length ? refused : undefined,
          proposals: proposals.length ? proposals : undefined,
          states: proposals.length ? states : undefined,
        },
      ]);
    } catch {
      setErr(t("chat.fail"));
    } finally {
      setSending(false);
    }
  }

  // ── The confirmation lane ──────────────────────────────────────────────────
  //
  // The ONLY place this page can cause a write. Everything the model controls is
  // already fixed by the time we get here: the token is opaque and single use, the
  // args_hash binds the execution to the arguments rendered above, and the server
  // re-checks the token, the hash, the expiry and the caller. So this function's
  // whole job is to send those two values and report honestly what came back —
  // it cannot "confirm the wrong thing" because it does not decide what runs.
  //
  // The model never sees the token: it went straight from the server's response
  // into this component's state.
  async function decide(msgIndex: number, p: Proposal, action: "confirm" | "cancel") {
    setErr("");
    setState(msgIndex, p.token, action === "confirm" ? "confirming" : "cancelling");
    // The two routes are written LITERALLY rather than built from `action`: the
    // route-parity guard resolves frontend paths against the backend mux by
    // reading this source, and a computed path is a path nothing can check. A
    // typo here would then be a 404 at the one moment the user has committed to a
    // decision.
    const url = action === "confirm" ? "/api/rest/chat/confirm" : "/api/rest/chat/cancel";
    try {
      // The decision carries the active workspace for the same reason the message
      // does: the server resolves the WRITE account from it, and a confirmation is
      // the one chat call that changes data. Without it a two-workspace session
      // falls back to the user's FIRST account — the card names the account they
      // selected, but the write would land on the other one.
      const r = await fetch(`${url}${workspaceQuery()}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(
          action === "confirm"
            ? { token: p.token, args_hash: p.args_hash }
            : { token: p.token },
        ),
      });
      // 401 → a dead session. A 403 is decided by the BODY's machine code, not by
      // the status: an uncoded 403 is an authorization failure (→ /login), while
      // one naming the tenancy code (workspace.notFound) is a stale `?workspace=`
      // slug and must NOT sign the merchant out (t_da4b2031). So the body is read
      // FIRST and handed to the shared decision below; only an "unauth" outcome
      // navigates, and the two reads cannot disagree because the same (status,
      // body) pair drives both. The old `r.status === 401 || r.status === 403`
      // line sent the scope refusal straight to /login.
      const body = await r.json().catch(() => null);
      const failure = classifyStatus(r.status, body);
      if (failure === "scope") {
        // The stale slug must stop being sent (t_da4b2031), exactly as runLoad
        // does for a page load: publishWorkspaceScope("") drops it and notifies,
        // so the next request is scoped to whatever workspace the session then
        // publishes rather than repeating a refusal the server already answered.
        dropCurrentWorkspaceScope();
      }
      if (failure === "unauth") {
        goToLogin();
        return;
      }
      // EVERY other answer goes through ONE decision that branches on the API's
      // machine CODE, not on the bare status (lib/chatConfirm.ts). The 400 is the
      // case that matters: it is NOT necessarily a workspace-scope refusal — the
      // server maps every unrecognised confirm failure to 400, including a tool it
      // refused AFTER claiming the token — so a status-only branch told a member
      // whose write was refused by their ROLE to "choose a workspace first", which
      // they cannot act on (t_fb393abe). The body was read above; a body that
      // cannot be read yields no code, which falls to the failure branch and never
      // the scope copy.
      const outcome = confirmDecisionFor(r.status, body);
      if (outcome === "scopeRequired") {
        // The card gets its OWN state, not "failed": the server refuses this
        // BEFORE it reads the token (requireResolvedWorkspaceScope runs ahead of
        // the token checks in handleChatConfirm), so nothing was attempted, the
        // token is still live, and the card must not wear a "Did not go through"
        // pill over a "try again" footer — retrying unchanged cannot work
        // (t_790b1016). The scope copy is carried ONCE, by the alert below:
        // rendering it in the card as well would duplicate the sentence, which is
        // the pre-fix shape.
        setState(msgIndex, p.token, "scopeRequired");
        setErr(t("chat.scopeRequired"));
        return;
      }
      if (outcome === "rateLimited") {
        setState(msgIndex, p.token, "failed");
        setErr(t("chat.confirmRateLimited"));
        return;
      }
      if (outcome === "stale") {
        // Spent or expired — the card cannot be answered any more. The user needs
        // the assistant to prepare it again, which is what the copy says.
        setState(msgIndex, p.token, "stale");
        setErr(action === "confirm" ? t("chat.proposalStale") : t("chat.cancelStale"));
        return;
      }
      if (outcome === "unexecutable") {
        // A card whose arguments the tool would never accept (t_623676d1). The
        // server refuses it BEFORE the claim, so the token is UNSPENT — the card
        // must not wear "expired", which is false of it, and must not tell the
        // user to try again on an action that can never work. Its own state and
        // its own sentence say what is actually true: nothing was attempted, and
        // a freshly prepared card is the fix.
        //
        // Cancel is the exception: cancelling such a card is a real, successful
        // action (the token IS spent, the proposal IS settled), so it is reported
        // as a done/cancelled card rather than as this refusal.
        if (action === "cancel") {
          setState(msgIndex, p.token, "cancelled");
          return;
        }
        setState(msgIndex, p.token, "unexecutable");
        setErr(t("chat.proposalUnexecutable"));
        return;
      }
      if (outcome === "failed") {
        // A genuine failure of the change — including a 400 that names a refusal
        // which is not the scope. The card says so, and here that is accurate: for
        // the post-claim tool refusal the token really is spent and the change
        // really did not happen. The sentence is the PAGE's own, never the server's
        // prose (issue #6177): the wire `error` is Malay and the API does not know
        // this visitor's language.
        setState(msgIndex, p.token, "failed");
        setErr(action === "confirm" ? t("chat.confirmFail") : t("chat.cancelFail"));
        return;
      }
      setState(msgIndex, p.token, action === "confirm" ? "done" : "cancelled");
    } catch {
      setState(msgIndex, p.token, "failed");
      setErr(action === "confirm" ? t("chat.confirmFail") : t("chat.cancelFail"));
    }
  }

  function setState(msgIndex: number, token: string, state: ProposalState) {
    setMsgs((prev) =>
      prev.map((m, i) =>
        i === msgIndex ? { ...m, states: { ...(m.states ?? {}), [token]: state } } : m,
      ),
    );
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

            {/* A proposal is a DECISION, so it is rendered as a card with the
                specific arguments, not as assistant prose. The badge says plainly
                that nothing has happened yet — the model cannot write, and a user
                who believed the change was already made would not read the card
                at all. */}
            {msgs.map((m, i) =>
              m.proposals
                ? m.proposals.map((p) => {
                    const state = m.states?.[p.token] ?? "pending";
                    const open = state === "pending";
                    return (
                      <div
                        key={`p${i}-${p.token}`}
                        className="rounded-lg border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-900"
                      >
                        <div className="flex flex-wrap items-center gap-2">
                          {/* The badge must not keep saying "needs your confirmation"
                              once the user has settled it — a done card headed
                              "NEEDS CONFIRMATION" reads as an unfinished task and
                              invites a second click the server would refuse. */}
                          <span
                            className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-white ${
                              open ? "bg-sky-600" : state === "failed" ? "bg-red-500" : "bg-slate-500"
                            }`}
                          >
                            {open
                              ? t("chat.proposalBadge")
                              : state === "done"
                                ? t("chat.proposalBadgeDone")
                                : state === "cancelled"
                                  ? t("chat.proposalBadgeCancelled")
                                  : state === "stale"
                                    ? t("chat.proposalBadgeStale")
                                    : state === "unexecutable"
                                      ? t("chat.proposalBadgeUnexecutable")
                                      : state === "scopeRequired"
                                        ? t("chat.proposalBadgeScopeRequired")
                                        : t("chat.proposalBadgeFailed")}
                          </span>
                          <span className="font-semibold">{p.title}</span>
                        </div>
                        {p.summary && <p className="mt-1 opacity-80">{p.summary}</p>}
                        <dl className="mt-2 space-y-0.5">
                          {p.fields?.map((f) => (
                            <div key={f.label} className="flex gap-2">
                              <dt className="min-w-[7rem] shrink-0 opacity-70">{f.label}</dt>
                              <dd className="break-words font-medium">{f.value}</dd>
                            </div>
                          ))}
                        </dl>
                        {p.note && (
                          <p className="mt-2 border-l-2 border-sky-300 pl-2 italic opacity-80">
                            {p.note}
                          </p>
                        )}
                        <p className="mt-2 font-mono text-[10px] opacity-60">{p.tool}</p>

                        {open && (
                          <div className="mt-2 flex flex-wrap items-center gap-2">
                            <button
                              type="button"
                              onClick={() => void decide(i, p, "confirm")}
                              className="jf-btn-primary disabled:opacity-50"
                            >
                              {t("chat.confirm")}
                            </button>
                            <button
                              type="button"
                              onClick={() => void decide(i, p, "cancel")}
                              className="jf-btn-ghost disabled:opacity-50"
                            >
                              {t("chat.cancel")}
                            </button>
                            <span className="opacity-70">{t("chat.confirmHint")}</span>
                          </div>
                        )}
                        {state === "confirming" && <p className="mt-2 font-medium">{t("chat.confirming")}</p>}
                        {state === "cancelling" && <p className="mt-2 font-medium">{t("chat.cancelling")}</p>}
                        {state === "done" && (
                          <p className="mt-2 font-medium text-emerald-700">{t("chat.confirmed")}</p>
                        )}
                        {state === "cancelled" && (
                          <p className="mt-2 font-medium opacity-80">{t("chat.cancelled")}</p>
                        )}
                        {state === "stale" && (
                          <p className="mt-2 font-medium text-amber-700">{t("chat.proposalStale")}</p>
                        )}
                        {/* NO footer for "unexecutable" either, and for the same
                            reason as "scopeRequired": the sentence is carried once,
                            by the page's single role="alert" (t_623676d1). The card's
                            job in this state is to stop claiming a failure — the
                            token is unspent, nothing was attempted — so it wears its
                            own badge and offers no "try again" footer for an action
                            that can never succeed as shown. */}
                        {/* NO footer for "scopeRequired". The scope refusal is
                            carried by the page's single role="alert" (setErr above)
                            — rendering it here as well would put the same sentence
                            on screen twice, which is the duplication this card's
                            own history calls out ("duplicated, but not
                            contradictory"). The card's job in this state is to stop
                            claiming a failure: the badge says what it needs, the
                            buttons are gone (nothing was attempted and the refusal
                            is not retryable unchanged), and NOTHING here tells the
                            user to try the same action again. */}
                        {state === "failed" && (
                          <p className="mt-2 font-medium text-red-600">{t("chat.confirmFail")}</p>
                        )}
                      </div>
                    );
                  })
                : null,
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
