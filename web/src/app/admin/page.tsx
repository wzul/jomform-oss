"use client";

// Issue #387: super-admin platform-wide usage dashboard (/admin).
// The platform owner (super-admin role, #385) can visually monitor usage
// across ALL workspaces once JomForm is public. It is gated on the super-admin
// role: non-admins are redirected to the landing page. Read-only — no
// destructive actions exist on this page. Data comes from the super-admin
// GET /v1/admin/overview endpoint (audited server-side).

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { Markdown } from "../../components/Markdown";
import { useI18n, type Key } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate, formatDateTime } from "../../lib/formatDate";
import { growthLegend, growthValue, type GrowthCounts } from "../../lib/adminGrowth";
import {
  hasNotifyFailure,
  notifyFailedWhoKey,
  replyResult,
  retryResult,
} from "../../lib/feedbackNotify";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { apiErr, translateApiErr } from "../../lib/apiErr";

type Totals = {
  workspaces: number;
  forms: number;
  products: number;
  sales: number;
  users: number;
  media_bytes: number;
};

// The growth counts (workspaces/forms/products/sales/users) come from the API's
// dbstore.GrowthCounts via growth_24h / growth_7d. The shape and the field order
// are declared once in ../../lib/adminGrowth so the value line and the legend
// under it cannot drift.
type Growth = GrowthCounts;

type EmailSummary = {
  delivered: number;
  bounce: number;
  complaint: number;
  open: number;
  click: number;
};

type SpamQueue = { submissions: number; sales: number };

type RecentSignup = { id: string; email: string; name?: string; created_at: string };
type ActiveWorkspace = { id: string; name: string; slug: string; owner_email: string; last_activity: string };
type StorageRow = { workspace_id: string; workspace_name: string; slug: string; used_bytes: number };

type DeadLetter = {
  dedup_key: string;
  account_id?: string;
  event_type: string;
  attempts: number;
  last_error: string;
  created_at: string;
  dead_since: string;
};

type DeadLetters = { items: DeadLetter[]; total: number };

// Issue #601: the suppression list — addresses JomForm refuses to mail.
type Suppression = {
  email: string;
  reason: string;
  source: string;
  note?: string;
  created_by?: string;
  created_by_email?: string;
  created_at: string;
};

type Suppressions = { items: Suppression[]; page: number; limit: number; total: number };

// In-app feedback: threads filed by users. The triage section lists every
// thread, opens one, replies, and changes the status. A thread belongs to the
// person who filed it (it is system-owned, not account data), so this list is
// the only place an operator can see the queue.
type FeedbackThread = {
  id: string;
  subject: string;
  status: string;
  user_id: string;
  user_email: string;
  created_at: string;
  last_activity_at: string;
  message_count: number;
  // The notification-failure marker (absent on a healthy thread): when the last
  // email for this thread could not be handed to the delivery queue. Without
  // showing it the console looks healthy while the filer has been told nothing.
  notify_failed_at?: string;
  notify_failed_kind?: string;
  notify_failed_error?: string;
};

type FeedbackMessage = {
  id: string;
  author_kind: string;
  author_email?: string;
  body: string;
  created_at: string;
};

type FeedbackList = {
  items: FeedbackThread[];
  page: number;
  limit: number;
  total: number;
  counts: Record<string, number>;
};

// The four statuses the API accepts (dbstore.FeedbackStatuses). One list, so
// the filter chips and the status dropdown cannot drift from the server's set.
const FB_STATUSES = ["open", "in_progress", "resolved", "closed"] as const;
const FB_PER_PAGE = 20;

const SUPP_PER_PAGE = 20;
// The three canonical reason codes (suppressed_emails_reason_chk, migration
// 000061). Kept as one list so the add form's dropdown and the row badge cannot
// drift from what the API accepts.
const SUPP_REASONS = ["hard_bounce", "complaint", "manual"] as const;

// A pragmatic address check for the client side only — the API validates with
// a real RFC 5322 parse and is the authority. This exists so an obvious typo is
// caught before a round trip, not to replace the server's answer.
const looksLikeEmail = (s: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);

/**
 * Reads the API's `{error, code}` failure body off a failed response, falling
 * back to `fallback` when the body is absent or unparseable.
 *
 * Issue #6177: the server's `error` string is Malay prose, so it must NEVER be
 * rendered. The machine `code` is translated instead; an unknown code falls back
 * to `fallback`, which each caller passes translated.
 */
async function serverError(r: Response, fallback: string, t: (k: Key) => string): Promise<string> {
  try {
    const body = await r.json();
    return apiErr(body, fallback, t);
  } catch {
    // Non-JSON body (a proxy error page, an empty 500): use the fallback.
  }
  return fallback;
}

type Overview = {
  totals: Totals;
  growth_24h: Growth;
  growth_7d: Growth;
  email: EmailSummary;
  spam: SpamQueue;
  recent_signups: RecentSignup[];
  active_workspaces: ActiveWorkspace[];
  storage: StorageRow[];
  quota_bytes: number;
};

const fmtBytes = (b: number) => {
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(2)} GB`;
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(1)} MB`;
  if (b >= 1 << 10) return `${(b / (1 << 10)).toFixed(0)} KB`;
  return `${b} B`;
};

// Suppression reason badge colours (mirror the deliveries page palette):
// a hard bounce and a complaint are damage to the sending identity, a manual
// entry is a decision an operator made.
const suppReasonBadge = (reason: string) =>
  ({
    hard_bounce: "bg-red-50 text-red-700 border-red-200",
    complaint: "bg-orange-50 text-orange-700 border-orange-200",
    manual: "bg-slate-100 text-slate-600 border-slate-200",
  })[reason] || "bg-slate-100 text-slate-500 border-slate-200";

// The feedback status badge colours. One function so the list rows, the thread
// header and the user-facing page's legend cannot drift apart.
function fbStatusBadge(status: string) {
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

function StatCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="rounded-xl border border-[var(--jf-border)] bg-white p-4">
      {/* Type sizes: the label/sub lines are body text and must not go below
          14px (text-sm) on any viewport — the mobile audit found 37 text
          leaves at 12px on this page. The text-2xl value stays as it is. */}
      <p className="text-sm font-medium uppercase tracking-wide text-[var(--jf-muted)]">{label}</p>
      {/* `[overflow-wrap:anywhere]` is the last-resort guard: a 6-digit count in
          text-2xl is ~100px wide and an unbreakable run of digits would paint
          past the card border in a narrow grid cell (measured 1.2px past a 116px
          card at 1024px). Allowing the break makes the card's min-content one
          character, so the value wraps instead of escaping the card. */}
      <p className="mt-1 text-2xl font-semibold text-[var(--jf-text)] [overflow-wrap:anywhere]">{value}</p>
      {sub ? (
        <p className="mt-0.5 break-words text-sm text-[var(--jf-muted)]">{sub}</p>
      ) : null}
    </div>
  );
}

export default function AdminPage() {
  const { t } = useI18n();
  const router = useRouter();
  const { user, anon, bundle } = useSession();
  const [data, setData] = useState<Overview | null>(null);
  const [err, setErr] = useState("");

  const isSuperAdmin = !!user?.is_super_admin;

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  // Issue #48: anonymous visit bounces to the landing page.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  // Non-super-admins are redirected away (the endpoint also 403s server-side).
  useEffect(() => {
    if (!anon && bundle !== undefined && !isSuperAdmin) {
      router.replace("/");
    }
  }, [anon, bundle, isSuperAdmin, router]);

  // Issue #579: dead letters — inbound callbacks parked because they could not
  // be processed. Loaded alongside the overview; a failure here must not blank
  // the rest of the page.
  const [dead, setDead] = useState<DeadLetters | null>(null);
  const [deadErr, setDeadErr] = useState("");
  const [requeueing, setRequeueing] = useState("");

  const loadDead = useCallback(async () => {
    const r = await runLoad(
      () => fetchJson<DeadLetters>("/api/admin/dead-letters"),
      { onUnauth: goToLogin, message: t("admin.deadLettersLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    setDeadErr(r.ok ? "" : r.error);
    if (r.ok) setDead(r.data);
  }, [goToLogin, t]);

  const requeue = useCallback(async (dedupKey: string) => {
    setRequeueing(dedupKey);
    setDeadErr("");
    try {
      await fetchJson("/api/admin/dead-letters/requeue", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ dedup_key: dedupKey }),
      });
      await loadDead();
    } catch (e) {
      setDeadErr(e instanceof Error ? e.message : String(e));
    } finally {
      setRequeueing("");
    }
  }, [loadDead]);

  const load = useCallback(async () => {
    // Issue #503: explicit non-ok handling. 401 (expired session) and 403 both
    // bounce to /login — a non-super-admin never reaches this page anyway (the
    // redirect above fires first), so a 403 here means a stale session.
    const r = await runLoad(
      () => fetchJson<Overview>("/api/admin/overview"),
      { onUnauth: goToLogin, message: t("admin.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    setErr(r.ok ? "" : r.error);
    if (r.ok) setData(r.data);
  }, [t, goToLogin]);

  // Issue #601: suppression list state. Kept separate from `load`/`loadDead`
  // so a failure in one section never blanks the others.
  const [supp, setSupp] = useState<Suppressions | null>(null);
  const [suppErr, setSuppErr] = useState("");
  const [suppPages, setSuppPages] = useState(1);
  const [suppSearch, setSuppSearch] = useState("");
  // The debounced value actually sent to the server; typing must not fire a
  // request per keystroke.
  const [suppQuery, setSuppQuery] = useState("");
  const [suppBusy, setSuppBusy] = useState("");
  const [addEmail, setAddEmail] = useState("");
  const [addReason, setAddReason] = useState<string>(SUPP_REASONS[0]);
  const [addNote, setAddNote] = useState("");
  const [addMsg, setAddMsg] = useState("");
  const [addErr, setAddErr] = useState("");
  const [adding, setAdding] = useState(false);

  const loadSupp = useCallback(async (page: number, q: string) => {
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(SUPP_PER_PAGE));
    if (q) params.set("q", q);
    const r = await runLoad(
      () => fetchJson<Suppressions>(`/api/admin/suppressions?${params.toString()}`),
      { onUnauth: goToLogin, message: t("admin.suppLoadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    setSuppErr(r.ok ? "" : r.error);
    if (r.ok) setSupp(r.data);
  }, [goToLogin, t]);

  // Debounce the search box; a change resets to page 1 (a filtered result set
  // has its own pagination, so staying on page 4 would show an empty page).
  useEffect(() => {
    const id = window.setTimeout(() => {
      setSuppQuery(suppSearch);
      setSuppPages(1);
    }, 300);
    return () => window.clearTimeout(id);
  }, [suppSearch]);

  const removeSupp = useCallback(async (addr: string) => {
    if (!window.confirm(t("admin.suppRemoveConfirm", { email: addr }))) return;
    setSuppBusy(addr);
    setSuppErr("");
    try {
      const r = await fetch(`/api/admin/suppressions/${encodeURIComponent(addr)}`, { method: "DELETE" });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        // The server knows why better than the page does: a 404 is "not on the
        // list" and an infrastructure failure says so. Its machine code is
        // translated; only the generic line is used when there is none.
        setSuppErr(await serverError(r, t("admin.suppRemoveFail"), t));
        return;
      }
      // Re-read the SAME page the operator was on, so the row disappears in
      // place instead of jumping them back to page 1. If that was the last row
      // on the last page, step back one page so they do not land on an empty
      // page — changing the page fires the loader effect, so only reload
      // explicitly when the page is unchanged (otherwise the row is fetched
      // twice).
      const remaining = supp?.items.filter((s) => s.email !== addr).length ?? 0;
      const target = remaining === 0 && suppPages > 1 ? suppPages - 1 : suppPages;
      if (target !== suppPages) {
        setSuppPages(target);
      } else {
        await loadSupp(target, suppQuery);
      }
    } catch {
      setSuppErr(t("admin.suppRemoveFail"));
    } finally {
      setSuppBusy("");
    }
  }, [goToLogin, loadSupp, supp, suppPages, suppQuery, t]);

  const addSupp = useCallback(async () => {
    setAddErr("");
    setAddMsg("");
    // Lowercase + trim exactly as the API does, so what the operator sees in
    // the confirmation is what was stored.
    const addr = addEmail.trim().toLowerCase();
    if (!looksLikeEmail(addr)) {
      setAddErr(t("admin.suppBadEmail"));
      return;
    }
    setAdding(true);
    try {
      const r = await fetch("/api/admin/suppressions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: addr, reason: addReason, note: addNote.trim() }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        // A rejected write carries a machine code (an invalid email, a bad
        // reason code); translating it is what makes the form fixable — the
        // server's own prose is Malay and is never rendered.
        setAddErr(await serverError(r, t("admin.suppAddFail"), t));
        return;
      }
      const res = (await r.json()) as { email: string; created: boolean };
      setAddMsg(res.created
        ? t("admin.suppAdded", { email: res.email })
        : t("admin.suppAddedExisting", { email: res.email }));
      setAddEmail("");
      setAddNote("");
      if (suppPages !== 1) {
        // The new row is newest-first, so page 1 is where it shows; changing
        // the page fires the loader effect.
        setSuppPages(1);
      } else {
        await loadSupp(1, suppQuery);
      }
    } catch {
      setAddErr(t("admin.suppAddFail"));
    } finally {
      setAdding(false);
    }
  }, [addEmail, addNote, addReason, goToLogin, loadSupp, suppPages, suppQuery, t]);

  // In-app feedback triage state. Separate from the other sections for the same
  // reason the suppression list is: a failure here must not blank the overview.
  const [fbList, setFbList] = useState<FeedbackList | null>(null);
  const [fbErr, setFbErr] = useState("");
  const [fbFilter, setFbFilter] = useState<string>("");
  const [fbPage, setFbPage] = useState(1);
  const [fbOpenId, setFbOpenId] = useState("");
  const [fbThread, setFbThread] = useState<FeedbackThread | null>(null);
  const [fbMessages, setFbMessages] = useState<FeedbackMessage[]>([]);
  const [fbReply, setFbReply] = useState("");
  const [fbBusy, setFbBusy] = useState("");
  const [fbMsg, setFbMsg] = useState("");
  const [fbActionErr, setFbActionErr] = useState("");

  const loadFeedback = useCallback(async (page: number, status: string) => {
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(FB_PER_PAGE));
    if (status) params.set("status", status);
    setFbErr("");
    const r = await runLoad(
      () => fetchJson<FeedbackList>(`/api/admin/feedback?${params.toString()}`),
      { onUnauth: goToLogin, message: t("afb.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setFbErr(r.error);
      return;
    }
    setFbList(r.data);
  }, [t, goToLogin]);

  // Reads a thread and returns it, so a caller can act on what the thread LOOKS
  // like now rather than on what the server said a moment ago.
  const loadFeedbackThread = useCallback(async (id: string): Promise<FeedbackThread | null> => {
    setFbOpenId(id);
    setFbMsg("");
    setFbActionErr("");
    setFbMessages([]);
    setFbThread(null);
    const r = await runLoad(
      () => fetchJson<{ thread: FeedbackThread; messages: FeedbackMessage[] }>(
        `/api/admin/feedback/${encodeURIComponent(id)}`),
      { onUnauth: goToLogin, message: t("afb.loadFail"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setFbActionErr(r.error);
      return null;
    }
    setFbThread(r.data.thread);
    setFbMessages(Array.isArray(r.data.messages) ? r.data.messages : []);
    return r.data.thread;
  }, [t, goToLogin]);

  const openFeedback = useCallback(async (id: string) => {
    await loadFeedbackThread(id);
  }, [loadFeedbackThread]);

  const sendFeedbackReply = useCallback(async () => {
    if (!fbOpenId || fbReply.trim().length < 10) return;
    setFbBusy("reply");
    setFbActionErr("");
    setFbMsg("");
    try {
      const r = await fetch(`/api/admin/feedback/${encodeURIComponent(fbOpenId)}/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ body: fbReply.trim() }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        setFbActionErr(t("afb.replyFail"));
        return;
      }
      // The reply is stored either way; the response's `notification` field says
      // whether the filer was actually told. The mapping (and the reason a
      // missing field must not be read as success) lives in ../../lib/feedbackNotify
      // so `npm run test:unit` can execute it.
      let notification = "";
      try {
        const payload = (await r.json()) as { notification?: string };
        notification = payload.notification ?? "";
      } catch {
        notification = "";
      }
      setFbReply("");
      const result = replyResult(notification);
      // The reload comes FIRST, then the message. `openFeedback` resets this
      // section's message state (it is what makes switching threads safe), so
      // setting the outcome before the await leaves nothing on screen — the
      // browser proof caught exactly that: the reply reached the server and the
      // console showed the admin no outcome at all.
      await openFeedback(fbOpenId);
      await loadFeedback(fbPage, fbFilter);
      if (result.kind === "error") {
        setFbActionErr(t(result.messageKey as Key));
      } else {
        setFbMsg(t(result.messageKey as Key));
      }
    } catch {
      setFbActionErr(t("afb.replyFail"));
    } finally {
      setFbBusy("");
    }
  }, [fbOpenId, fbReply, fbPage, fbFilter, goToLogin, loadFeedback, openFeedback, t]);

  // Re-send the notification that last failed for the open thread. This is the
  // action half of the marker: seeing "the filer was not told" is only useful if
  // the operator can do something about it without leaving the thread.
  const retryFeedbackNotify = useCallback(async () => {
    if (!fbOpenId) return;
    setFbBusy("notify");
    setFbActionErr("");
    setFbMsg("");
    try {
      const r = await fetch(`/api/admin/feedback/${encodeURIComponent(fbOpenId)}/notify`, {
        method: "POST",
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        setFbActionErr(t("afb.notifyRetryFail"));
        return;
      }
      let notification = "";
      try {
        const payload = (await r.json()) as { notification?: string };
        notification = payload.notification ?? "";
      } catch {
        notification = "";
      }
      const result = retryResult(notification);
      // Same ordering rule as the reply path: the reload resets the message
      // state, so the outcome must be set after it.
      //
      // AND THE RELOAD IS READ, not just awaited. A retry reports what the
      // ATTEMPT did; the marker is what the thread still shows. When those
      // disagree — the retry queued a mail yet the failure is still recorded on
      // the thread (a differently-kinded marker, a send that did not clear it) —
      // the console used to print "Notification sent again." directly above a
      // banner saying the filer was NOT told, with nothing to say which one was
      // stale. The confirmation is therefore keyed on the thread that comes back:
      // a marker still set means the success message must not be shown.
      const reloaded = await loadFeedbackThread(fbOpenId);
      await loadFeedback(fbPage, fbFilter);
      if (result.kind === "error") {
        setFbActionErr(t(result.messageKey as Key));
      } else if (reloaded && hasNotifyFailure(reloaded)) {
        // A delivery that did not clear the marker: say what is still true.
        setFbMsg(t("afb.notifyRetryStillMarked"));
      } else {
        setFbMsg(t(result.messageKey as Key));
      }
    } catch {
      setFbActionErr(t("afb.notifyRetryFail"));
    } finally {
      setFbBusy("");
    }
  }, [fbOpenId, fbPage, fbFilter, goToLogin, loadFeedback, loadFeedbackThread, t]);

  const setFeedbackStatus = useCallback(async (status: string) => {
    if (!fbOpenId) return;
    setFbBusy("status");
    setFbActionErr("");
    setFbMsg("");
    try {
      const r = await fetch(`/api/admin/feedback/${encodeURIComponent(fbOpenId)}/status`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      if (r.status === 401 || r.status === 403) {
        goToLogin();
        return;
      }
      if (!r.ok) {
        setFbActionErr(t("afb.statusFail"));
        return;
      }
      setFbMsg(t("afb.statusSet"));
      await openFeedback(fbOpenId);
      await loadFeedback(fbPage, fbFilter);
    } catch {
      setFbActionErr(t("afb.statusFail"));
    } finally {
      setFbBusy("");
    }
  }, [fbOpenId, fbPage, fbFilter, goToLogin, loadFeedback, openFeedback, t]);

  useEffect(() => {
    if (isSuperAdmin) {
      load();
      loadDead();
    }
  }, [isSuperAdmin, load, loadDead]);

  // Issue #601: the suppression list is a server-paginated, server-searched
  // section, so it reloads on its own whenever the debounced query or the page
  // changes (and once on entry).
  useEffect(() => {
    if (isSuperAdmin) void loadSupp(suppPages, suppQuery);
  }, [isSuperAdmin, loadSupp, suppPages, suppQuery]);

  // In-app feedback: same pattern as the suppression list — a server-side
  // filter + pager, reloading when either changes (and once on entry).
  useEffect(() => {
    if (isSuperAdmin) void loadFeedback(fbPage, fbFilter);
  }, [isSuperAdmin, loadFeedback, fbPage, fbFilter]);

  if (anon || !isSuperAdmin) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-[var(--jf-muted)]">{t("admin.forbidden")}</p>
      </div>
    );
  }

  return (
    <Shell active="admin">
      {/* The page root is a flex item of #main-content, which Shell renders as a
          COLUMN flex container (Shell.tsx). `mx-auto` (margin-left/right: auto)
          cancels `align-self: stretch`, so without a definite width this box
          sizes to MAX-CONTENT — its min-content is the dead-letters table, so
          the whole document became ~1047px wide on a 360px phone and every
          section was pushed off-screen. `w-full` gives the flex item a definite
          width; `mx-auto` + `max-w-6xl` still centre it above 1152px, so desktop
          is unchanged. Every other Shell page already uses `w-full` — this was
          the only one that did not (adminResponsive.test.ts guards the rule). */}
      <div className="mx-auto w-full max-w-6xl px-4 py-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold text-[var(--jf-text)]">{t("admin.title")}</h1>
            <p className="mt-1 text-sm text-[var(--jf-muted)]">
              {t("admin.subtitle")} · <span className="font-medium">{t("admin.readonly")}</span>
            </p>
          </div>
        </div>

        {err ? (
          <div role="alert" className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
            {err}
            {/* Issue #503: a failed load gets an action, not a dead end. */}
            <button
              type="button"
              onClick={() => void load()}
              className="ml-2 inline-flex min-h-11 items-center px-2 underline decoration-dotted underline-offset-2"
            >
              {t("common.retry")}
            </button>
          </div>
        ) : !data ? (
          <p className="text-sm text-[var(--jf-muted)]">Loading…</p>
        ) : (
          <div className="space-y-8">
            {/* Platform totals */}
            <section>
              <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                {t("admin.totals")}
              </h2>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
                <StatCard label={t("admin.workspaces")} value={data.totals.workspaces} />
                <StatCard label={t("admin.forms")} value={data.totals.forms} />
                <StatCard label={t("admin.products")} value={data.totals.products} />
                <StatCard label={t("admin.sales")} value={data.totals.sales} />
                <StatCard label={t("admin.users")} value={data.totals.users} />
                <StatCard label={t("admin.storageUsed")} value={fmtBytes(data.totals.media_bytes)} />
              </div>
              {/* Issue #387 / stat-card report: the five counts are
                  workspaces/forms/products/sales/users and used to render as a
                  bare `0 / 0 / 0 / 0 / 0`. The legend line names each number in
                  the same order, in the active locale. */}
              <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
                <StatCard
                  label={t("admin.growth24h")}
                  value={growthValue(data.growth_24h)}
                  sub={growthLegend(t)}
                />
                <StatCard
                  label={t("admin.growth7d")}
                  value={growthValue(data.growth_7d)}
                  sub={growthLegend(t)}
                />
              </div>
            </section>

            {/* Email + spam */}
            <section className="grid grid-cols-1 gap-6 lg:grid-cols-2">
              <div>
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("admin.email")}
                </h2>
                {/* Mobile audit: `sm:grid-cols-5` put five cards in one row from
                    640px up, but this grid sits inside the `lg:grid-cols-2`
                    section, so the cards were only 90px wide at 1280px and the
                    `text-2xl` values painted past the card border (measured at
                    1280px on the unfixed page: a 6-digit value 26.8px past, a
                    label 7.7px past — pre-existing, and present at widths where
                    the page had no horizontal overflow at all). Five columns can
                    never fit this half-width section. Two up on a phone and
                    three up once the section is really wide (xl) keeps every
                    value inside its card: the narrowest cell is 164px at 1280px
                    and 138px at 320px, against a ~100px value. */}
                <div className="grid grid-cols-2 gap-3 xl:grid-cols-3">
                  <StatCard label={t("admin.emailDelivered")} value={data.email.delivered} />
                  <StatCard label={t("admin.emailBounce")} value={data.email.bounce} />
                  <StatCard label={t("admin.emailComplaint")} value={data.email.complaint} />
                  <StatCard label={t("admin.emailOpen")} value={data.email.open} />
                  <StatCard label={t("admin.emailClick")} value={data.email.click} />
                </div>
              </div>
              <div>
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("admin.spam")}
                </h2>
                <div className="grid grid-cols-2 gap-3">
                  <StatCard label={t("admin.spamSubmissions")} value={data.spam.submissions} />
                  <StatCard label={t("admin.spamSales")} value={data.spam.sales} />
                </div>
              </div>
            </section>

            {/* Dead letters (issue #579) */}
            <section>
              <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                {t("admin.deadLetters")}
              </h2>
              <p className="mb-3 text-sm text-[var(--jf-muted)]">{t("admin.deadLettersHint")}</p>
              {deadErr && (
                <p className="mb-2 text-sm text-red-600">{deadErr}</p>
              )}
              {!dead || dead.items.length === 0 ? (
                <p className="text-sm text-[var(--jf-muted)]">{t("admin.deadLettersNone")}</p>
              ) : (
                <>
                  {/* The six-column table's min-content is ~823px even with the
                      two long cells breakable (it was ~1013px before), and this
                      page box is only `min(1152px, 100vw-32px)` wide — so below
                      an xl viewport the table cannot fit and its Requeue column
                      (the one action on this section, and the reason an operator
                      opens this page) sat outside the visible area: measured
                      687px to the right at 360px and still 24px past the edge at
                      1024px. Below xl each dead letter is a stacked card with a
                      full-width 44px Requeue button in view; the table appears
                      from xl up, where 823px fits inside 1056px. */}
                  <ul className="space-y-3 xl:hidden">
                    {dead.items.map((d) => (
                      <li
                        key={d.dedup_key}
                        className="rounded-xl border border-[var(--jf-border)] bg-white p-3"
                      >
                        <div className="flex flex-wrap items-baseline justify-between gap-2">
                          <span className="font-medium">{d.event_type || "—"}</span>
                          <span className="text-xs text-[var(--jf-muted)]">
                            {d.dead_since?.slice(0, 19).replace("T", " ")}
                          </span>
                        </div>
                        <p className="mt-1 break-all font-mono text-xs text-[var(--jf-muted)]">
                          {d.dedup_key}
                        </p>
                        <dl className="mt-2 space-y-1 text-sm">
                          <div className="flex gap-2">
                            <dt className="shrink-0 text-[var(--jf-muted)]">{t("admin.dlWorkspace")}</dt>
                            <dd className="min-w-0 break-all font-mono text-xs">
                              {d.account_id ? (
                                d.account_id.slice(0, 8)
                              ) : (
                                <span className="italic text-[var(--jf-muted)]">{t("admin.dlUnattributed")}</span>
                              )}
                            </dd>
                          </div>
                          <div className="flex gap-2">
                            <dt className="shrink-0 text-[var(--jf-muted)]">{t("admin.dlAttempts")}</dt>
                            <dd>{d.attempts}</dd>
                          </div>
                          <div className="flex gap-2">
                            <dt className="shrink-0 text-[var(--jf-muted)]">{t("admin.dlReason")}</dt>
                            <dd className="min-w-0 [overflow-wrap:anywhere]">{d.last_error || "—"}</dd>
                          </div>
                        </dl>
                        <button
                          onClick={() => requeue(d.dedup_key)}
                          disabled={requeueing === d.dedup_key}
                          className="mt-3 min-h-11 w-full rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-50"
                        >
                          {requeueing === d.dedup_key ? t("admin.dlRequeueing") : t("admin.dlRequeue")}
                        </button>
                      </li>
                    ))}
                  </ul>
                  <div className="hidden overflow-x-auto rounded-xl border border-[var(--jf-border)] xl:block">
                    <table className="w-full text-left text-sm">
                      <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-sm uppercase tracking-wide text-[var(--jf-muted)]">
                        <tr>
                          <th className="px-4 py-2 font-medium">{t("admin.dlEvent")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.dlWorkspace")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.dlAttempts")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.dlReason")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.dlParked")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.dlAction")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {dead.items.map((d) => (
                          <tr key={d.dedup_key} className="border-b border-[var(--jf-border)] last:border-0">
                            <td className="px-4 py-2">
                              <div className="font-medium">{d.event_type || "—"}</div>
                              {/* break-all: a dedup_key is an opaque token with no
                                  spaces, so it set the table's min-content floor
                                  (1013px). Breaking it is what lets the wrapper
                                  scroll instead of the page. */}
                              <div className="break-all font-mono text-xs text-[var(--jf-muted)]">{d.dedup_key}</div>
                            </td>
                            <td className="px-4 py-2 font-mono text-xs">
                              {d.account_id ? d.account_id.slice(0, 8) : <span className="italic text-[var(--jf-muted)]">{t("admin.dlUnattributed")}</span>}
                            </td>
                            <td className="px-4 py-2">{d.attempts}</td>
                            <td className="px-4 py-2 [overflow-wrap:anywhere]">{d.last_error || "—"}</td>
                            <td className="px-4 py-2 whitespace-nowrap text-xs">{d.dead_since?.slice(0, 19).replace("T", " ")}</td>
                            <td className="px-4 py-2">
                              <button
                                onClick={() => requeue(d.dedup_key)}
                                disabled={requeueing === d.dedup_key}
                                className="min-h-11 whitespace-nowrap rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-50"
                              >
                                {requeueing === d.dedup_key ? t("admin.dlRequeueing") : t("admin.dlRequeue")}
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </>
              )}
            </section>

            {/* Suppression list (issue #601) */}
            {/*
              The addresses JomForm refuses to mail. Kept on this page rather
              than a new route (same precedent as dead letters, #579): it is
              super-admin only, needs no new nav entry, and the mobile guard
              already covers /admin.

              Mobile discipline: an email address is a long unbreakable token —
              the same thing that blew this page to 1047px wide on a phone
              (#592). Every cell carrying an address breaks (break-all), and the
              section uses the card-list-below-xl + table-from-xl shape of the
              dead-letters section, because an email plus six other columns
              cannot fit the page box below xl.
            */}
            <section>
              <h2 className="mb-3 flex flex-wrap items-center gap-2 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                {t("admin.supp")}
                {/* AGENTS.md DOCUMENTATION PARITY rule 4: an inline "?" next to
                    the feature, deep-linking the docs page that explains it. */}
                <a
                  href="/docs/ses-reputation#suppression"
                  target="_blank"
                  rel="noopener noreferrer"
                  title={t("admin.suppDocs")}
                  aria-label={t("admin.suppDocs")}
                  className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                >
                  ?
                </a>
              </h2>
              <p className="mb-3 text-sm text-[var(--jf-muted)]">{t("admin.suppHint")}</p>

              {/* Manual add. Its own error/success line, so a rejected email
                  does not read as a failed list load. */}
              <div className="mb-4 rounded-xl border border-[var(--jf-border)] bg-white p-3">
                <p className="mb-2 text-sm font-medium text-[var(--jf-text)]">{t("admin.suppAddTitle")}</p>
                <div className="flex flex-wrap items-end gap-2">
                  <label className="flex min-w-0 flex-1 basis-56 flex-col gap-1 text-sm">
                    <span className="text-[var(--jf-muted)]">{t("admin.suppAddEmail")}</span>
                    <input
                      value={addEmail}
                      onChange={(e) => setAddEmail(e.target.value)}
                      type="email"
                      inputMode="email"
                      autoComplete="off"
                      placeholder="name@example.com"
                      aria-label={t("admin.suppAddEmail")}
                      className="jf-input w-full"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-sm">
                    <span className="text-[var(--jf-muted)]">{t("admin.suppAddReason")}</span>
                    <select
                      value={addReason}
                      onChange={(e) => setAddReason(e.target.value)}
                      aria-label={t("admin.suppAddReason")}
                      className="jf-input"
                    >
                      {SUPP_REASONS.map((r) => (
                        <option key={r} value={r}>
                          {r}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="flex min-w-0 flex-1 basis-48 flex-col gap-1 text-sm">
                    <span className="text-[var(--jf-muted)]">{t("admin.suppAddNote")}</span>
                    <input
                      value={addNote}
                      onChange={(e) => setAddNote(e.target.value)}
                      autoComplete="off"
                      aria-label={t("admin.suppAddNote")}
                      className="jf-input w-full"
                    />
                  </label>
                  <button
                    type="button"
                    onClick={() => void addSupp()}
                    disabled={adding}
                    className="min-h-11 whitespace-nowrap rounded-md border border-[var(--jf-border)] px-4 text-sm font-medium hover:bg-slate-50 disabled:opacity-50"
                  >
                    {adding ? t("admin.suppAdding") : t("admin.suppAddSubmit")}
                  </button>
                </div>
                {addErr && (
                  <p role="alert" className="mt-2 break-words text-sm text-red-600">
                    {addErr}
                  </p>
                )}
                {addMsg && <p className="mt-2 break-all text-sm text-emerald-700">{addMsg}</p>}
              </div>

              {/* Search (debounced, resets to page 1). */}
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <input
                  value={suppSearch}
                  onChange={(e) => setSuppSearch(e.target.value)}
                  type="search"
                  autoComplete="off"
                  placeholder={t("admin.suppSearchPh")}
                  aria-label={t("admin.suppSearchPh")}
                  className="jf-input min-h-11 w-full sm:w-72"
                />
                {supp && (
                  <span className="text-sm text-[var(--jf-muted)]">
                    {t("admin.suppCount", { n: supp.total })}
                  </span>
                )}
              </div>

              {suppErr && (
                <div role="alert" className="mb-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
                  {suppErr}
                  <button
                    type="button"
                    onClick={() => void loadSupp(suppPages, suppQuery)}
                    className="ml-2 inline-flex min-h-11 items-center px-2 underline decoration-dotted underline-offset-2"
                  >
                    {t("common.retry")}
                  </button>
                </div>
              )}

              {!supp ? (
                <p className="text-sm text-[var(--jf-muted)]">Loading…</p>
              ) : supp.items.length === 0 ? (
                <p className="text-sm text-[var(--jf-muted)]">
                  {suppQuery ? t("admin.suppNoMatch") : t("admin.suppNone")}
                </p>
              ) : (
                <>
                  {/* Below xl: one card per address, with the Remove action in
                      view. */}
                  <ul className="space-y-3 xl:hidden">
                    {supp.items.map((s) => (
                      <li
                        key={s.email}
                        className="rounded-xl border border-[var(--jf-border)] bg-white p-3"
                      >
                        {/* break-all: an address is unbreakable and sets the
                            card's min-content otherwise. */}
                        <div className="break-all font-medium text-[var(--jf-text)]">{s.email}</div>
                        <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
                          <span
                            className={`rounded-full border px-2 py-0.5 font-medium ${suppReasonBadge(s.reason)}`}
                          >
                            {s.reason}
                          </span>
                          <span className="text-[var(--jf-muted)]">{s.source}</span>
                          <span className="text-[var(--jf-muted)]">
                            {s.created_at?.slice(0, 19).replace("T", " ")}
                          </span>
                        </div>
                        {s.note ? (
                          <p className="mt-1 [overflow-wrap:anywhere] text-xs text-[var(--jf-muted)]">
                            {s.note}
                          </p>
                        ) : null}
                        <p className="mt-1 break-all text-xs text-[var(--jf-muted)]">
                          {t("admin.suppColBy")}: {s.created_by_email || "—"}
                        </p>
                        <button
                          type="button"
                          onClick={() => void removeSupp(s.email)}
                          disabled={suppBusy === s.email}
                          className="mt-3 min-h-11 w-full rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-50"
                        >
                          {suppBusy === s.email ? t("admin.suppRemoving") : t("admin.suppRemove")}
                        </button>
                      </li>
                    ))}
                  </ul>
                  {/* From xl up: the table. */}
                  <div className="hidden overflow-x-auto rounded-xl border border-[var(--jf-border)] xl:block">
                    <table className="w-full text-left text-sm">
                      <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-sm uppercase tracking-wide text-[var(--jf-muted)]">
                        <tr>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColEmail")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColReason")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColSource")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColNote")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColAdded")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColBy")}</th>
                          <th className="px-4 py-2 font-medium">{t("admin.suppColAction")}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {supp.items.map((s) => (
                          <tr key={s.email} className="border-b border-[var(--jf-border)] last:border-0">
                            <td className="px-4 py-2 break-all font-medium">{s.email}</td>
                            <td className="px-4 py-2">
                              <span
                                className={`whitespace-nowrap rounded-full border px-2 py-0.5 text-xs font-medium ${suppReasonBadge(s.reason)}`}
                              >
                                {s.reason}
                              </span>
                            </td>
                            <td className="px-4 py-2 whitespace-nowrap text-[var(--jf-muted)]">{s.source}</td>
                            <td className="px-4 py-2 [overflow-wrap:anywhere] text-[var(--jf-muted)]">{s.note || "—"}</td>
                            <td className="px-4 py-2 whitespace-nowrap text-xs text-[var(--jf-muted)]">
                              {s.created_at?.slice(0, 19).replace("T", " ")}
                            </td>
                            <td className="px-4 py-2 break-all text-[var(--jf-muted)]">{s.created_by_email || "—"}</td>
                            <td className="px-4 py-2">
                              <button
                                type="button"
                                onClick={() => void removeSupp(s.email)}
                                disabled={suppBusy === s.email}
                                className="min-h-11 whitespace-nowrap rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-50"
                              >
                                {suppBusy === s.email ? t("admin.suppRemoving") : t("admin.suppRemove")}
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>

                  {/* Pager. Server-side, so the buttons only move the page
                      number; the loader effect refetches. */}
                  {suppPages > 1 && (
                    <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-[var(--jf-muted)]">
                      <span>{t("admin.suppPageOf", { page: supp.page, total: suppPages })}</span>
                      <div className="flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => setSuppPages((p) => Math.max(1, p - 1))}
                          disabled={supp.page <= 1}
                          className="min-h-11 rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-40"
                        >
                          {t("common.prev")}
                        </button>
                        <button
                          type="button"
                          onClick={() => setSuppPages((p) => Math.min(suppPages, p + 1))}
                          disabled={supp.page >= suppPages}
                          className="min-h-11 rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-40"
                        >
                          {t("common.next")}
                        </button>
                      </div>
                    </div>
                  )}
                </>
              )}
            </section>

            {/* In-app feedback triage (system-owned threads filed by users). */}
            <section>
              <h2 className="mb-3 flex flex-wrap items-center gap-2 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                {t("afb.title")}
                {/* AGENTS.md DOCUMENTATION PARITY rule 4: an inline "?" next to
                    the feature, deep-linking the docs page that explains it. */}
                <a
                  href="/docs/feedback"
                  target="_blank"
                  rel="noopener noreferrer"
                  title={t("fb.docs")}
                  aria-label={t("fb.docs")}
                  className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-slate-200 text-xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
                >
                  ?
                </a>
              </h2>
              <p className="mb-3 text-sm text-[var(--jf-muted)]">{t("afb.subtitle")}</p>

              {/* Status filter chips (server-side filter). */}
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  onClick={() => {
                    setFbFilter("");
                    setFbPage(1);
                  }}
                  className={`min-h-11 rounded-md px-3 text-sm font-medium ${
                    fbFilter === ""
                      ? "bg-[var(--jf-primary)] text-white"
                      : "border border-[var(--jf-border)] hover:bg-slate-50"
                  }`}
                >
                  {t("afb.filterAll")}
                  {fbList && ` (${fbList.total})`}
                </button>
                {FB_STATUSES.map((s) => (
                  <button
                    key={s}
                    type="button"
                    onClick={() => {
                      setFbFilter(s);
                      setFbPage(1);
                    }}
                    className={`min-h-11 rounded-md px-3 text-sm font-medium ${
                      fbFilter === s
                        ? "bg-[var(--jf-primary)] text-white"
                        : "border border-[var(--jf-border)] hover:bg-slate-50"
                    }`}
                  >
                    {t(`fb.status.${s}` as Key)}
                    {fbList && ` (${fbList.counts?.[s] ?? 0})`}
                  </button>
                ))}
              </div>

              {fbErr && (
                <div role="alert" className="mb-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">
                  {fbErr}
                  <button
                    type="button"
                    onClick={() => void loadFeedback(fbPage, fbFilter)}
                    className="ml-2 inline-flex min-h-11 items-center px-2 underline decoration-dotted underline-offset-2"
                  >
                    {t("common.retry")}
                  </button>
                </div>
              )}

              {!fbList ? (
                <p className="text-sm text-[var(--jf-muted)]">Loading…</p>
              ) : fbList.items.length === 0 ? (
                <p className="text-sm text-[var(--jf-muted)]">{t("afb.empty")}</p>
              ) : (
                <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
                  {/* Thread list. Below xl each row is a stacked card. */}
                  <ul className="space-y-2">
                    {fbList.items.map((th) => (
                      <li key={th.id}>
                        <button
                          type="button"
                          onClick={() => void openFeedback(th.id)}
                          className={`flex w-full flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left ${
                            fbOpenId === th.id
                              ? "border-[var(--jf-primary)] bg-indigo-50/25"
                              : "border-[var(--jf-border)] bg-white hover:bg-slate-50"
                          }`}
                        >
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-sm font-medium">
                              {th.subject || t("fb.noSubject")}
                            </span>
                            <span className="mt-0.5 block break-all text-xs text-[var(--jf-muted)]">
                              {th.user_email} · {formatDateTime(th.last_activity_at)} ·{" "}
                              {t("fb.messages", { n: th.message_count })}
                            </span>
                          </span>
                          <span className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${fbStatusBadge(th.status)}`}>
                            {t(`fb.status.${th.status}` as Key)}
                          </span>
                          {/* A queued thread whose notification is broken is
                              marked in the LIST too: an operator scanning the
                              queue must not have to open every thread to find
                              the one where nobody was told. */}
                          {hasNotifyFailure(th) && (
                            <span
                              className="shrink-0 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-900"
                              title={t("afb.notifyFailedTitle")}
                            >
                              {t("afb.notifyFailedBadge")}
                            </span>
                          )}
                        </button>
                      </li>
                    ))}
                  </ul>

                  {/* The selected thread: messages, reply box, status control. */}
                  <div className="rounded-xl border border-[var(--jf-border)] bg-white p-4">
                    {!fbThread ? (
                      <p className="text-sm text-[var(--jf-muted)]">{t("fb.pickOne")}</p>
                    ) : (
                      <>
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
                            {fbThread.subject || t("fb.noSubject")}
                          </h3>
                          <select
                            value={fbThread.status}
                            onChange={(e) => void setFeedbackStatus(e.target.value)}
                            disabled={fbBusy === "status"}
                            aria-label={t("afb.columnStatus")}
                            className="jf-input min-h-11 w-auto text-sm"
                          >
                            {FB_STATUSES.map((s) => (
                              <option key={s} value={s}>
                                {t(`fb.status.${s}` as Key)}
                              </option>
                            ))}
                          </select>
                        </div>
                        <p className="mt-1 break-all text-xs text-[var(--jf-muted)]">
                          {t("afb.columnFiler")}: {fbThread.user_email}
                        </p>

                        {/* The notification-failure marker. It is a FACT about
                            the thread (the last email could not be handed to
                            the queue), so it sits with the thread rather than
                            in a transient toast — and it carries the retry, so
                            seeing it is enough to act on it. */}
                        {hasNotifyFailure(fbThread) && (
                          <div
                            role="alert"
                            className="mt-3 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900"
                          >
                            <p className="font-medium">{t("afb.notifyFailedTitle")}</p>
                            <p className="mt-1">
                              {t("afb.notifyFailedWho", {
                                who: t(notifyFailedWhoKey(fbThread.notify_failed_kind) as Key),
                              })}
                            </p>
                            <p className="mt-1 text-xs">
                              {formatDateTime(fbThread.notify_failed_at ?? "")}
                              {fbThread.notify_failed_error
                                ? ` · ${fbThread.notify_failed_error}`
                                : ""}
                            </p>
                            <button
                              type="button"
                              onClick={() => void retryFeedbackNotify()}
                              disabled={fbBusy === "notify"}
                              className="mt-2 min-h-11 rounded-md border border-amber-400 bg-white px-3 text-sm font-medium text-amber-900 hover:bg-amber-100 disabled:opacity-50"
                            >
                              {fbBusy === "notify" ? t("afb.notifyRetrying") : t("afb.notifyRetry")}
                            </button>
                          </div>
                        )}

                        <ul className="mt-3 space-y-3">
                          {fbMessages.map((m) => {
                            const isAdmin = m.author_kind === "admin";
                            return (
                              <li
                                key={m.id}
                                className={`rounded-lg border px-3 py-2 ${
                                  isAdmin
                                    ? "border-indigo-100 bg-indigo-50/50"
                                    : "border-[var(--jf-border)] bg-slate-50"
                                }`}
                              >
                                <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
                                  <span className="font-medium text-[var(--jf-text)]">
                                    {isAdmin ? t("fb.whoTeam") : fbThread.user_email}
                                  </span>
                                  <span>{formatDateTime(m.created_at)}</span>
                                </div>
                                <div className="mt-1 text-sm">
                                  {/* Same shared renderer as /chat and /feedback
                                      (the card requires ONE, not two copies). */}
                                  <Markdown text={m.body} variant="plain" />
                                </div>
                              </li>
                            );
                          })}
                        </ul>

                        {fbMsg && <p className="mt-3 text-sm text-emerald-700">{fbMsg}</p>}
                        {fbActionErr && (
                          <p role="alert" className="mt-3 break-words text-sm text-red-600">{fbActionErr}</p>
                        )}

                        <div className="mt-4">
                          <label htmlFor="afb-reply" className="text-xs font-medium text-[var(--jf-muted)]">
                            {t("afb.replyTitle")}
                          </label>
                          <textarea
                            id="afb-reply"
                            value={fbReply}
                            onChange={(e) => setFbReply(e.target.value)}
                            rows={3}
                            maxLength={5000}
                            placeholder={t("afb.replyPh")}
                            className="jf-input mt-1"
                          />
                          <button
                            type="button"
                            onClick={() => void sendFeedbackReply()}
                            disabled={fbBusy === "reply" || fbReply.trim().length < 10}
                            className="mt-2 min-h-11 rounded-md bg-[var(--jf-primary)] px-4 text-sm font-medium text-white disabled:opacity-50"
                          >
                            {t("afb.replySend")}
                          </button>
                        </div>
                      </>
                    )}
                  </div>
                </div>
              )}

              {fbList && fbList.total > FB_PER_PAGE && (
                <div className="mt-3 flex items-center gap-3">
                  <button
                    type="button"
                    onClick={() => setFbPage((p) => Math.max(1, p - 1))}
                    disabled={fbPage <= 1}
                    className="min-h-11 rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-40"
                  >
                    {t("common.prev")}
                  </button>
                  <span className="text-sm text-[var(--jf-muted)]">
                    {t("admin.suppPageOf", {
                      page: fbPage,
                      total: Math.max(1, Math.ceil(fbList.total / FB_PER_PAGE)),
                    })}
                  </span>
                  <button
                    type="button"
                    onClick={() => setFbPage((p) => p + 1)}
                    disabled={fbPage >= Math.ceil(fbList.total / FB_PER_PAGE)}
                    className="min-h-11 rounded-md border border-[var(--jf-border)] px-3 text-sm font-medium hover:bg-slate-50 disabled:opacity-40"
                  >
                    {t("common.next")}
                  </button>
                </div>
              )}
            </section>

            {/* Storage per workspace */}
            <section>
              <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                {t("admin.storage")}
              </h2>
              <div className="overflow-x-auto rounded-xl border border-[var(--jf-border)]">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-sm uppercase tracking-wide text-[var(--jf-muted)]">
                    <tr>
                      <th className="px-4 py-2 font-medium">{t("admin.workspaces")}</th>
                      <th className="px-4 py-2 font-medium">{t("admin.storageUsed")}</th>
                      <th className="px-4 py-2 font-medium">{t("admin.quota")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.storage.length === 0 ? (
                      <tr>
                        <td colSpan={3} className="px-4 py-3 text-[var(--jf-muted)]">
                          —
                        </td>
                      </tr>
                    ) : (
                      data.storage.map((s) => (
                        <tr key={s.workspace_id} className="border-b border-[var(--jf-border)] last:border-0">
                          <td className="px-4 py-2">
                            <span className="font-medium">{s.workspace_name}</span>
                            <span className="ml-2 break-all text-xs text-[var(--jf-muted)]">/{s.slug}</span>
                          </td>
                          <td className="px-4 py-2">{fmtBytes(s.used_bytes)}</td>
                          <td className="px-4 py-2">{fmtBytes(data.quota_bytes)}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </section>

            {/* Recent signups + active workspaces */}
            <section className="grid grid-cols-1 gap-6 lg:grid-cols-2">
              <div>
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("admin.recentSignups")}
                </h2>
                <div className="overflow-x-auto rounded-xl border border-[var(--jf-border)]">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-sm uppercase tracking-wide text-[var(--jf-muted)]">
                      <tr>
                        <th className="px-4 py-2 font-medium">{t("admin.users")}</th>
                        <th className="px-4 py-2 font-medium">{t("admin.lastActivity")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.recent_signups.length === 0 ? (
                        <tr>
                          <td colSpan={2} className="px-4 py-3 text-[var(--jf-muted)]">
                            —
                          </td>
                        </tr>
                      ) : (
                        data.recent_signups.map((u) => (
                          <tr key={u.id} className="border-b border-[var(--jf-border)] last:border-0">
                            <td className="px-4 py-2">
                              <span className="font-medium">{u.name || u.email}</span>
                              {u.name ? <span className="ml-2 break-all text-xs text-[var(--jf-muted)]">{u.email}</span> : null}
                            </td>
                            <td className="px-4 py-2 whitespace-nowrap text-[var(--jf-muted)]">{formatDate(u.created_at)}</td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
              <div>
                <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
                  {t("admin.activeWorkspaces")}
                </h2>
                <div className="overflow-x-auto rounded-xl border border-[var(--jf-border)]">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-sm uppercase tracking-wide text-[var(--jf-muted)]">
                      <tr>
                        <th className="px-4 py-2 font-medium">{t("admin.workspaces")}</th>
                        <th className="px-4 py-2 font-medium">{t("admin.owner")}</th>
                        <th className="px-4 py-2 font-medium">{t("admin.lastActivity")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.active_workspaces.length === 0 ? (
                        <tr>
                          <td colSpan={3} className="px-4 py-3 text-[var(--jf-muted)]">
                            —
                          </td>
                        </tr>
                      ) : (
                        data.active_workspaces.map((s) => (
                          <tr key={s.id} className="border-b border-[var(--jf-border)] last:border-0">
                            <td className="px-4 py-2">
                              <span className="font-medium">{s.name}</span>
                              <span className="ml-2 break-all text-xs text-[var(--jf-muted)]">/{s.slug}</span>
                            </td>
                            <td className="px-4 py-2 break-all text-[var(--jf-muted)]">{s.owner_email}</td>
                            <td className="px-4 py-2 whitespace-nowrap text-[var(--jf-muted)]">{formatDateTime(s.last_activity)}</td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </section>
          </div>
        )}
      </div>
    </Shell>
  );
}
