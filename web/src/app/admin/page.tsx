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
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate, formatDateTime } from "../../lib/formatDate";
import { growthLegend, growthValue, type GrowthCounts } from "../../lib/adminGrowth";
import { fetchJson, runLoad } from "../../lib/wsfetch";

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

function StatCard({ label, value, sub }: { label: string; value: string | number; sub?: string }) {
  return (
    <div className="rounded-xl border border-[var(--jf-border)] bg-white p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-[var(--jf-muted)]">{label}</p>
      <p className="mt-1 text-2xl font-semibold text-[var(--jf-text)]">{value}</p>
      {sub ? (
        <p className="mt-0.5 break-words text-xs text-[var(--jf-muted)]">{sub}</p>
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
      { onUnauth: goToLogin, message: t("admin.deadLettersLoadFail") },
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
      { onUnauth: goToLogin, message: t("admin.loadFail") },
    );
    setErr(r.ok ? "" : r.error);
    if (r.ok) setData(r.data);
  }, [t, goToLogin]);

  useEffect(() => {
    if (isSuperAdmin) {
      load();
      loadDead();
    }
  }, [isSuperAdmin, load, loadDead]);

  if (anon || !isSuperAdmin) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-[var(--jf-muted)]">{t("admin.forbidden")}</p>
      </div>
    );
  }

  return (
    <Shell active="admin">
      <div className="mx-auto max-w-6xl px-4 py-8">
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
              className="ml-2 underline decoration-dotted underline-offset-2"
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
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
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
              <p className="mb-3 text-xs text-[var(--jf-muted)]">{t("admin.deadLettersHint")}</p>
              {deadErr && (
                <p className="mb-2 text-xs text-red-600">{deadErr}</p>
              )}
              {!dead || dead.items.length === 0 ? (
                <p className="text-sm text-[var(--jf-muted)]">{t("admin.deadLettersNone")}</p>
              ) : (
                <div className="overflow-x-auto rounded-xl border border-[var(--jf-border)]">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-xs uppercase tracking-wide text-[var(--jf-muted)]">
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
                            <div className="font-mono text-[11px] text-[var(--jf-muted)]">{d.dedup_key}</div>
                          </td>
                          <td className="px-4 py-2 font-mono text-[11px]">
                            {d.account_id ? d.account_id.slice(0, 8) : <span className="italic text-[var(--jf-muted)]">{t("admin.dlUnattributed")}</span>}
                          </td>
                          <td className="px-4 py-2">{d.attempts}</td>
                          <td className="px-4 py-2 text-xs">{d.last_error || "—"}</td>
                          <td className="px-4 py-2 text-xs">{d.dead_since?.slice(0, 19).replace("T", " ")}</td>
                          <td className="px-4 py-2">
                            <button
                              onClick={() => requeue(d.dedup_key)}
                              disabled={requeueing === d.dedup_key}
                              className="rounded-md border border-[var(--jf-border)] px-2 py-1 text-xs font-medium hover:bg-slate-50 disabled:opacity-50"
                            >
                              {requeueing === d.dedup_key ? t("admin.dlRequeueing") : t("admin.dlRequeue")}
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
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
                  <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-xs uppercase tracking-wide text-[var(--jf-muted)]">
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
                            <span className="ml-2 text-xs text-[var(--jf-muted)]">/{s.slug}</span>
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
                    <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-xs uppercase tracking-wide text-[var(--jf-muted)]">
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
                              {u.name ? <span className="ml-2 text-xs text-[var(--jf-muted)]">{u.email}</span> : null}
                            </td>
                            <td className="px-4 py-2 text-[var(--jf-muted)]">{formatDate(u.created_at)}</td>
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
                    <thead className="border-b border-[var(--jf-border)] bg-slate-50 text-xs uppercase tracking-wide text-[var(--jf-muted)]">
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
                              <span className="ml-2 text-xs text-[var(--jf-muted)]">/{s.slug}</span>
                            </td>
                            <td className="px-4 py-2 text-[var(--jf-muted)]">{s.owner_email}</td>
                            <td className="px-4 py-2 text-[var(--jf-muted)]">{formatDateTime(s.last_activity)}</td>
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
