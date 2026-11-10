"use client";

// Activity — the workspace-scoped audit trail (card t_eb09b25a).
//
// WHY THIS PAGE EXISTS. Every action the product records already landed in the
// `events` table (issue #92) and was readable through `GET /v1/rest/events`,
// `.../events/count` and `.../events/breakdown` plus the MCP tool `list_events`
// — but NO shipped page called the LIST endpoint at all (`grep -rn
// 'event_name=' web/src` returned zero hits). The one UI that ever showed these
// rows was the Settings → MCP "Request history" table, deleted by PR #311 while
// issue #195 stayed CLOSED. So the merchant who deleted a workspace and its
// payment account could not see the `workspace.deleted` row anywhere in the
// product: from a user's point of view every event was write-only.
//
// WHAT THIS IS NOT. It is not the realtime feed. `/sales` subscribes to
// `GET /v1/rest/events/stream` (SSE) and that stream carries ONLY the handful of
// event names the sales list reacts to (sale.paid, sale.failed, sale.refunded,
// subscription.charged, response.submitted). The audit trail is every recorded
// event, so it has to be the paginated, filterable LIST — reading it from the
// stream would silently show a merchant a fraction of their own history.
//
// SCOPE. Workspace-scoped: every fetch carries `?workspace=<slug>` through the
// shared helper (lib/workspace.workspaceQuery), and the server resolves that
// slug against the caller's own workspaces (resolveAccountScope). A slug that is
// not the caller's is refused 403 "workspace not found" rather than falling
// back to their first account — so this page can never read another tenant's
// trail. No new endpoint: the read path is the one verified working on prod.

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { PageLoading } from "../../components/PageLoading";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { workspaceQuery } from "../../lib/workspace";
import { fetchJson, runLoad } from "../../lib/wsfetch";
import { translateApiErr } from "../../lib/apiErr";
import { formatDateTime } from "../../lib/formatDate";
import { eventLabelKey } from "../../lib/activityEvents";

// One events row as `GET /v1/rest/events` returns it (api/internal/httpapi/rest_events.go).
type ActivityEvent = {
  id: number;
  event_name: string;
  properties?: Record<string, unknown>;
  user_id?: string;
  ip?: string;
  user_agent?: string;
  created_at: string;
};

type ActivityListing = { items: ActivityEvent[]; page: number; limit: number; total: number };

const PER_PAGE = 20;

// The event-name CLASSES the filter offers. The server matches a trailing "*"
// as a prefix (dbstore.eventWhere), which is the only reason a family filter is
// one request instead of one per name. EVERY family the API records belongs
// here: a family missing from this list is a row a merchant can read in the
// unfiltered list but cannot filter to (card t_07d8f4a4 — chat.*, admin.* and
// feedback.* were recorded and unselectable). activityPage.test.ts reads the
// recorder call sites under api/internal and fails in BOTH directions when this
// list and the recorded vocabulary disagree, so the list cannot silently fall
// behind again. Kept as raw event-name vocabulary rather than translated
// labels, the same choice /deliveries makes for its five delivery statuses —
// these are the tokens the API answers in and the ones an API-key or MCP user
// types.
const EVENT_CLASSES = [
  "form.*",
  "product.*",
  "sale.*",
  "settings.*",
  "workspace.*",
  "template.*",
  "media.*",
  "upload.*",
  "profile.*",
  "auth.*",
  "mcp.*",
  "chat.*",
  "admin.*",
  "feedback.*",
] as const;

/**
 * A date input's `yyyy-mm-dd` turned into the RFC3339 instant the API parses.
 *
 * The API reads `from`/`to` with `time.Parse(time.RFC3339, …)` and IGNORES a
 * value it cannot parse (rest_events.go:eventFilterFromRequest). A bare
 * `2026-09-25` therefore fails to parse and the filter is dropped in silence:
 * the page would look like it applied a range and the server would answer the
 * unfiltered list. `end` gives the last instant of the chosen day so a `to` of
 * today includes today.
 */
function dayBound(date: string, end: boolean): string {
  if (!date) return "";
  return `${date}T${end ? "23:59:59" : "00:00:00"}Z`;
}

export default function ActivityPage() {
  const { t } = useI18n();
  const { user, anon, workspace } = useSession();
  const router = useRouter();

  const [rows, setRows] = useState<ActivityEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");

  // filters
  const [eventClass, setEventClass] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");

  // pagination
  const [page, setPage] = useState(1);

  // Issue #503: 401/403 → /login, the same redirect Shell.signOut() uses.
  const goToLogin = useCallback(() => {
    router.replace("/login");
    window.location.href = "/login";
  }, [router]);

  const load = useCallback(async () => {
    setChecking(true);
    setErr("");
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("limit", String(PER_PAGE));
    if (eventClass) params.set("event_name", eventClass);
    const from = dayBound(dateFrom, false);
    const to = dayBound(dateTo, true);
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const sp = workspaceQuery();
    const sep = sp ? "&" : "";
    // Issue #503: a non-ok response must not be reported as a generic load
    // error (an expired session used to look like a broken page). 401/403 →
    // /login through the shared helper; every other failure renders a message.
    const r = await runLoad(
      () => fetchJson<ActivityListing>(`/api/rest/events?${params.toString()}${sep}${sp.replace("?", "")}`),
      { onUnauth: goToLogin, message: t("act.loadErr"), errorKey: (b: unknown) => translateApiErr(b, t) },
    );
    if (!r.ok) {
      setRows([]);
      setTotal(0);
      setErr(r.error);
      setChecking(false);
      return;
    }
    setRows(Array.isArray(r.data.items) ? r.data.items : []);
    setTotal(typeof r.data.total === "number" ? r.data.total : 0);
    setChecking(false);
  }, [page, eventClass, dateFrom, dateTo, t, goToLogin]);

  // Issue #170: re-fetch when the active workspace changes — the trail is
  // per-workspace and a stale list would be another workspace's history.
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, workspace]);

  // Reset to page 1 when the filters change (page 4 of the old filter is not
  // page 4 of the new one).
  useEffect(() => {
    setPage(1);
  }, [eventClass, dateFrom, dateTo]);

  // Auth bounce via router (client transition), not a full document reload.
  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  const totalPages = Math.max(1, Math.ceil(total / PER_PAGE));
  const safePage = Math.min(page, totalPages);
  const countLabel = useMemo(() => t("act.countItems", { n: total }), [t, total]);

  // The page paints inside the shell from the first frame: a bare <main> here
  // would unmount the sidebar for the length of the fetch (lib/pageShellLoading).
  if (checking && rows.length === 0 && !err) return <PageLoading active="activity" />;

  return (
    <Shell active="activity">
      <div className="w-full flex-1 px-6 py-8">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold tracking-tight">{t("act.title")}</h1>
            <p className="max-w-2xl text-sm text-[var(--jf-muted)]">{t("act.subtitle")}</p>
          </div>
          {/* Inline "?" doc link (AGENTS.md DOCUMENTATION PARITY rule 4). */}
          <a
            href="/docs/activity"
            target="_blank"
            rel="noopener noreferrer"
            title={t("act.docs")}
            className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-slate-200 text-jf-2xs font-bold text-slate-600 hover:bg-[var(--jf-primary)] hover:text-white"
          >
            ?
          </a>
        </div>

        {/* Filters */}
        <div className="mt-6 flex flex-wrap items-end gap-3">
          <label className="text-xs text-[var(--jf-muted)]">
            {t("act.filterEvent")}
            <select
              value={eventClass}
              onChange={(e) => setEventClass(e.target.value)}
              className="jf-input mt-0.5 block w-48"
            >
              <option value="">{t("act.allEvents")}</option>
              {EVENT_CLASSES.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </label>
          {/* FROM AND TO ARE ONE FIELD GROUP (card t_804040d3). As three
              independent flex items they wrapped badly on a phone: measured at
              390px, the event-type select and "From" shared the first row and
              "To" sat alone on the second, so the range read as a ragged pair
              rather than a range. The pair is now a single flex item that takes
              its own row below `sm`, where the two boxes split that row evenly;
              above `sm` they return to their fixed widths and sit inline. */}
          <div className="flex w-full items-end gap-2 sm:w-auto">
            <label className="min-w-0 flex-1 text-xs text-[var(--jf-muted)] sm:flex-none">
              {t("act.filterFrom")}
              <input
                type="date"
                value={dateFrom}
                onChange={(e) => setDateFrom(e.target.value)}
                className="jf-input mt-0.5 block w-full sm:w-40"
              />
            </label>
            <label className="min-w-0 flex-1 text-xs text-[var(--jf-muted)] sm:flex-none">
              {t("act.filterTo")}
              <input
                type="date"
                value={dateTo}
                onChange={(e) => setDateTo(e.target.value)}
                className="jf-input mt-0.5 block w-full sm:w-40"
              />
            </label>
          </div>
        </div>

        {/* Table */}
        <div className="jf-card mt-4 overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[var(--jf-border)] text-left text-jf-3xs uppercase tracking-wide text-[var(--jf-muted)]">
                <th className="whitespace-nowrap px-4 py-3 font-medium">{t("act.colEvent")}</th>
                <th className="hidden px-2 py-3 font-medium md:table-cell">{t("act.colProperties")}</th>
                <th className="px-2 py-3 font-medium">{t("act.colTime")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e) => {
                // THE CELL IS A SENTENCE, NOT A TOKEN (card t_804040d3). The raw
                // event name is what the REST/MCP surface answers in — it is not
                // something a merchant should have to read in their own table —
                // so the label comes from the dictionary and the machine token
                // stays on the cell as its `title`, which is where an operator
                // looks for it. A name the map does not know falls back to the
                // token itself rather than to a blank cell (lib/activityEvents.ts).
                const labelKey = eventLabelKey(e.event_name);
                return (
                  <tr key={e.id} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-4 py-3 text-xs font-semibold" title={e.event_name}>
                      {labelKey ? t(labelKey) : e.event_name}
                    </td>
                    <td className="hidden max-w-lg break-all px-2 py-3 font-mono text-jf-2xs text-[var(--jf-muted)] md:table-cell">
                      {e.properties && Object.keys(e.properties).length > 0 ? JSON.stringify(e.properties) : "—"}
                    </td>
                    <td className="whitespace-nowrap px-2 py-3 text-jf-2xs text-[var(--jf-muted)]">
                      {formatDateTime(e.created_at, user?.timezone)}
                    </td>
                  </tr>
                );
              })}
              {!checking && rows.length === 0 && (
                <tr>
                  <td colSpan={3} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                    {err || t("act.empty")}
                    {/* Issue #503: a failed load gets an action, not a dead end. */}
                    {err && (
                      <div className="mt-2">
                        <button
                          type="button"
                          onClick={() => void load()}
                          className="text-xs text-[var(--jf-muted)] underline decoration-dotted underline-offset-2 hover:text-[var(--jf-primary)]"
                        >
                          {t("common.retry")}
                        </button>
                      </div>
                    )}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
          {checking && rows.length === 0 && (
            <div className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">{t("act.loading")}</div>
          )}
        </div>

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="mt-4 flex flex-wrap items-center justify-between gap-2 text-xs text-[var(--jf-muted)]">
            <span>
              {t("act.pageOf", { page: safePage, total: totalPages })} · {countLabel}
            </span>
            <div className="flex items-center gap-1">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={safePage <= 1}
                className="jf-btn-ghost"
              >
                {t("common.prev")}
              </button>
              <button
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                disabled={safePage >= totalPages}
                className="jf-btn-ghost"
              >
                {t("common.next")}
              </button>
            </div>
          </div>
        )}

        {/* Retention is a property of the data itself, so it is stated on the
            page that reads it rather than left in a docs page nobody opens:
            auth.* and settings.* are kept longer than everything else. */}
        <p className="mt-4 max-w-2xl text-xs text-[var(--jf-muted)]">{t("act.retention")}</p>
      </div>
    </Shell>
  );
}
