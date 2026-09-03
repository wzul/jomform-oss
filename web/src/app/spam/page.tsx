"use client";

// Issue #245: AI spam review queue.
// The merchant sees spam-flagged submissions and sales (flagged by the
// background gemma4 judge) and can un-spam a false positive. A spam-flagged
// sale that receives a successful payment is automatically un-spammed by the
// payment callback. Spam entries auto-delete after 30 days.

import { Suspense, useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";
import { spaceQuery } from "../../lib/space";

type SpamSubmission = {
  id: string;
  form_id: string;
  data: Record<string, unknown>;
  spam_reason?: string;
  created_at: string;
};

type SpamSale = {
  ref_code: string;
  status: string;
  total_sen: number;
  buyer: string;
  buyer_email?: string;
  form_id?: string;
  spam_reason?: string;
  created_at: string;
};

const PAGE_SIZE = 20;

const money = (sen: number) =>
  `RM${(sen / 100).toLocaleString("en-MY", { minimumFractionDigits: 2 })}`;

function SpamPageInner() {
  const { t } = useI18n();
  const { anon, role, user, space } = useSession();
  const router = useRouter();
  const canWrite = role !== "read_only";

  const [tab, setTab] = useState<"submissions" | "sales">("submissions");
  const [subs, setSubs] = useState<SpamSubmission[]>([]);
  const [sales, setSales] = useState<SpamSale[]>([]);
  const [checking, setChecking] = useState(true);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState("");

  const load = useCallback(async () => {
    setChecking(true);
    setErr("");
    const sp = spaceQuery();
    const sep = sp ? "&" : "";
    try {
      const [sr, salr] = await Promise.all([
        fetch(`/api/rest/spam/submissions?page=1&limit=${PAGE_SIZE}${sep}${sp.replace("?", "")}`),
        fetch(`/api/rest/spam/sales?page=1&limit=${PAGE_SIZE}${sep}${sp.replace("?", "")}`),
      ]);
      if (!sr.ok || !salr.ok) {
        setErr(t("bld.loadSalesErr"));
        return;
      }
      const sd = await sr.json();
      const sald = await salr.json();
      setSubs(Array.isArray(sd.items) ? sd.items : []);
      setSales(Array.isArray(sald.items) ? sald.items : []);
    } catch {
      setErr(t("bld.loadSalesErr"));
    } finally {
      setChecking(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load, space]);

  useEffect(() => {
    if (anon) router.replace("/landing");
  }, [anon, router]);

  async function unspamSubmission(id: string) {
    setBusy(id);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/spam/submissions/${encodeURIComponent(id)}/unspam${spaceQuery()}`, {
        method: "POST",
      });
      if (r.ok) {
        setMsg(t("spam.unspammed"));
        setSubs((prev) => prev.filter((s) => s.id !== id));
      } else {
        setMsg(t("spam.unspamFail"));
      }
    } finally {
      setBusy(null);
    }
  }

  async function unspamSale(ref: string) {
    setBusy(ref);
    setMsg("");
    try {
      const r = await fetch(`/api/rest/spam/sales/${encodeURIComponent(ref)}/unspam${spaceQuery()}`, {
        method: "POST",
      });
      if (r.ok) {
        setMsg(t("spam.unspammed"));
        setSales((prev) => prev.filter((s) => s.ref_code !== ref));
      } else {
        setMsg(t("spam.unspamFail"));
      }
    } finally {
      setBusy(null);
    }
  }

  const reason = (r?: string) => r || "—";

  return (
    <Shell active="spam">
      <div className="w-full flex-1 px-6 py-8">
        <div>
          <h1 className="max-w-xl text-xl font-semibold tracking-tight">{t("spam.title")}</h1>
          <p className="max-w-xl text-sm text-[var(--jf-muted)]">{t("spam.subtitle")}</p>
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-2">
          <button
            onClick={() => setTab("submissions")}
            className={`rounded-md px-3 py-1.5 text-xs font-medium ${
              tab === "submissions"
                ? "bg-[var(--jf-primary)] text-white"
                : "border border-[var(--jf-border)] text-slate-600 hover:bg-slate-50"
            }`}
          >
            {t("spam.submissions")} ({subs.length})
          </button>
          <button
            onClick={() => setTab("sales")}
            className={`rounded-md px-3 py-1.5 text-xs font-medium ${
              tab === "sales"
                ? "bg-[var(--jf-primary)] text-white"
                : "border border-[var(--jf-border)] text-slate-600 hover:bg-slate-50"
            }`}
          >
            {t("spam.sales")} ({sales.length})
          </button>
        </div>

        {msg && <p className="mt-3 text-xs text-[var(--jf-primary)]">{msg}</p>}
        {err && <p className="mt-3 text-xs text-red-500">{err}</p>}

        <div className="jf-card mt-4 overflow-x-auto">
          {tab === "submissions" ? (
            <table className="w-full min-w-[640px] text-sm">
              <thead>
                <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-4 py-3 font-medium">ID</th>
                  <th className="px-2 py-3 font-medium">{t("spam.reason")}</th>
                  <th className="px-2 py-3 font-medium">{t("prod.date")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {subs.map((s) => (
                  <tr key={s.id} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-4 py-3 font-mono text-[11px]">{s.id.slice(0, 8)}</td>
                    <td className="max-w-64 truncate px-2 py-3 text-xs text-[var(--jf-muted)]">{reason(s.spam_reason)}</td>
                    <td className="whitespace-nowrap px-2 py-3 text-[11px] text-[var(--jf-muted)]">
                      {formatDate(s.created_at, user?.timezone)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      {canWrite && (
                        <button
                          onClick={() => unspamSubmission(s.id)}
                          disabled={busy !== null}
                          className="jf-btn-ghost px-2.5 py-1 text-xs disabled:opacity-40"
                        >
                          {busy === s.id ? "…" : t("spam.unspam")}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
                {!checking && subs.length === 0 && (
                  <tr>
                    <td colSpan={4} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                      {t("spam.empty")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          ) : (
            <table className="w-full min-w-[640px] text-sm">
              <thead>
                <tr className="border-b border-[var(--jf-border)] text-left text-[10px] uppercase tracking-wide text-[var(--jf-muted)]">
                  <th className="px-4 py-3 font-medium">{t("dash.ref")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.buyer")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.amount")}</th>
                  <th className="px-2 py-3 font-medium">{t("dash.status")}</th>
                  <th className="px-2 py-3 font-medium">{t("spam.reason")}</th>
                  <th className="px-2 py-3 font-medium">{t("prod.date")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("sales.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {sales.map((s) => (
                  <tr key={s.ref_code} className="border-b border-[var(--jf-border)] last:border-0 hover:bg-slate-50/60">
                    <td className="whitespace-nowrap px-4 py-3 font-mono text-[11px]">{s.ref_code}</td>
                    <td className="max-w-32 truncate px-2 py-3 text-xs">
                      {s.buyer || "—"}
                      {s.buyer_email && (
                        <span className="block truncate text-[10px] text-[var(--jf-muted)]">{s.buyer_email}</span>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-2 py-3 text-xs font-semibold">{money(s.total_sen)}</td>
                    <td className="whitespace-nowrap px-2 py-3 text-xs">{s.status}</td>
                    <td className="max-w-48 truncate px-2 py-3 text-xs text-[var(--jf-muted)]">{reason(s.spam_reason)}</td>
                    <td className="whitespace-nowrap px-2 py-3 text-[11px] text-[var(--jf-muted)]">
                      {formatDate(s.created_at, user?.timezone)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right">
                      {canWrite && (
                        <button
                          onClick={() => unspamSale(s.ref_code)}
                          disabled={busy !== null}
                          className="jf-btn-ghost px-2.5 py-1 text-xs disabled:opacity-40"
                        >
                          {busy === s.ref_code ? "…" : t("spam.unspam")}
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
                {!checking && sales.length === 0 && (
                  <tr>
                    <td colSpan={7} className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">
                      {t("spam.empty")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          )}
          {checking && (
            <div className="px-4 py-10 text-center text-sm text-[var(--jf-muted)]">{t("spam.loading")}</div>
          )}
        </div>

        <p className="mt-3 text-xs text-[var(--jf-muted)]">{t("spam.autoUnspamNote")}</p>
      </div>
    </Shell>
  );
}

export default function SpamPage() {
  return (
    <Suspense fallback={<Shell active="spam"><div className="px-6 py-8 text-sm text-[var(--jf-muted)]">…</div></Shell>}>
      <SpamPageInner />
    </Suspense>
  );
}
