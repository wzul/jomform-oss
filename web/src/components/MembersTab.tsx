"use client";

// Issue #163: Workspace Members management tab. Admin-only (owner or admin
// member). Invite by email + role, list members with role badges, change
// role, remove member, and show/cancel pending invites. Backed by the
// endpoints from #161 (POST/GET/DELETE /v1/workspaces/{id}/invites and
// GET/PATCH/DELETE /v1/workspaces/{id}/members).
import { useCallback, useEffect, useState } from "react";
import { useI18n } from "./i18n";

type Member = { user_id: string; email: string; name: string; role: string };
type Invite = { id: string; email: string; role: string; created_at?: string };

const ROLES = ["read_only", "manage", "admin"] as const;

function roleLabel(t: (k: any) => string, role: string): string {
  if (role === "owner") return t("role.owner");
  return t(`role.${role}` as any) || role;
}

export function MembersTab({ workspaceId }: { workspaceId: string }) {
  const { t } = useI18n();
  const [members, setMembers] = useState<Member[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<string>("read_only");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    if (!workspaceId) return;
    fetch(`/api/workspaces/${workspaceId}/members`)
      .then((r) => (r.ok ? r.json() : []))
      .then((d) => setMembers(Array.isArray(d) ? d : []))
      .catch(() => {});
    fetch(`/api/workspaces/${workspaceId}/invites`)
      .then((r) => (r.ok ? r.json() : []))
      .then((d) => setInvites(Array.isArray(d) ? d : []))
      .catch(() => {});
  }, [workspaceId]);

  useEffect(() => {
    load();
  }, [load]);

  async function invite(e: React.FormEvent) {
    e.preventDefault();
    if (busy || !workspaceId) return;
    setBusy(true);
    setMsg("");
    setErr("");
    try {
      const r = await fetch(`/api/workspaces/${workspaceId}/invites`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: email.trim(), role }),
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) {
        setErr(t("mem.inviteFail", { error: d.error || `HTTP ${r.status}` }));
        return;
      }
      setEmail("");
      setMsg(d.status === "added" ? t("mem.added") : t("mem.invited"));
      load();
    } catch {
      setErr(t("mem.inviteFail", { error: "network" }));
    } finally {
      setBusy(false);
    }
  }

  async function changeRole(userId: string, newRole: string) {
    if (!workspaceId) return;
    setErr("");
    const r = await fetch(`/api/workspaces/${workspaceId}/members/${userId}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ role: newRole }),
    });
    if (r.ok) {
      setMsg(t("mem.roleUpdated"));
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("mem.roleUpdateFail", { error: d.error || `HTTP ${r.status}` }));
    }
  }

  async function removeMember(userId: string, name: string) {
    if (!workspaceId) return;
    if (!window.confirm(t("mem.removeConfirm", { name: name || "this member" }))) return;
    setErr("");
    const r = await fetch(`/api/workspaces/${workspaceId}/members/${userId}`, { method: "DELETE" });
    if (r.ok) {
      setMsg(t("mem.removed"));
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("mem.removeFail", { error: d.error || `HTTP ${r.status}` }));
    }
  }

  async function cancelInvite(inviteId: string) {
    if (!workspaceId) return;
    setErr("");
    const r = await fetch(`/api/workspaces/${workspaceId}/invites/${inviteId}`, { method: "DELETE" });
    if (r.ok) {
      setMsg(t("mem.inviteCancelled"));
      load();
    } else {
      const d = await r.json().catch(() => ({}));
      setErr(t("mem.inviteFail", { error: d.error || `HTTP ${r.status}` }));
    }
  }

  return (
    <section className="jf-card mt-6 p-6">
      <h2 className="text-lg font-semibold">{t("mem.title")}</h2>
      <p className="mt-1 max-w-xl text-sm text-slate-400">{t("mem.adminOnly")}</p>

      {msg && <p className="mt-3 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-600">{msg}</p>}
      {err && <p className="mt-3 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-600">{err}</p>}

      {/* Invite form */}
      <form onSubmit={invite} className="mt-4">
        <h3 className="text-sm font-semibold text-slate-700">{t("mem.inviteTitle")}</h3>
        <div className="mt-2 flex flex-wrap items-end gap-2">
          <div className="min-w-56 flex-1">
            <label className="text-xs text-slate-500">{t("mem.inviteEmail")}</label>
            <input
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              type="email"
              required
              placeholder="teammate@example.com"
              className="jf-input mt-1 w-full"
            />
          </div>
          <div>
            <label className="text-xs text-slate-500">{t("mem.inviteRole")}</label>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value)}
              className="jf-input mt-1 w-40"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {roleLabel(t, r)}
                </option>
              ))}
            </select>
          </div>
          <button disabled={busy} className="jf-btn-primary">
            {busy ? "…" : t("mem.invite")}
          </button>
        </div>
        <p className="mt-1 text-[11px] text-slate-400">{t(`role.hint.${role}` as any)}</p>
      </form>

      {/* Members list */}
      <h3 className="mt-6 text-sm font-semibold text-slate-700">{t("mem.membersList")}</h3>
      {members.length === 0 ? (
        <p className="mt-2 text-sm text-slate-400">{t("mem.noMembers")}</p>
      ) : (
        <ul className="mt-2 space-y-2">
          {members.map((m) => (
            <li
              key={m.user_id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[var(--jf-border)] px-4 py-2.5"
            >
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">
                  {m.name || m.email}
                  {m.name && <span className="ml-1 text-slate-400">({m.email})</span>}
                </p>
                <span
                  className={`mt-0.5 inline-block rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                    m.role === "admin" || m.role === "owner"
                      ? "border border-indigo-200 bg-indigo-50 text-[var(--jf-primary)]"
                      : m.role === "manage"
                        ? "border border-sky-200 bg-sky-50 text-sky-700"
                        : "border border-slate-200 bg-slate-50 text-slate-500"
                  }`}
                >
                  {roleLabel(t, m.role)}
                </span>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <select
                  value={m.role === "owner" ? "admin" : m.role}
                  disabled={m.role === "owner"}
                  onChange={(e) => changeRole(m.user_id, e.target.value)}
                  title={t("mem.changeRole")}
                  className="rounded border border-[var(--jf-border)] bg-white px-2 py-1 text-xs"
                >
                  {ROLES.map((r) => (
                    <option key={r} value={r}>
                      {roleLabel(t, r)}
                    </option>
                  ))}
                </select>
                <button
                  disabled={m.role === "owner"}
                  onClick={() => removeMember(m.user_id, m.name || m.email)}
                  className="text-xs text-slate-500 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  {t("mem.remove")}
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {/* Pending invites */}
      <h3 className="mt-6 text-sm font-semibold text-slate-700">{t("mem.pendingTitle")}</h3>
      {invites.length === 0 ? (
        <p className="mt-2 text-sm text-slate-400">{t("mem.noPending")}</p>
      ) : (
        <ul className="mt-2 space-y-2">
          {invites.map((inv) => (
            <li
              key={inv.id}
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-2.5"
            >
              <div className="min-w-0">
                <p className="truncate text-sm">{inv.email}</p>
                <span className="mt-0.5 inline-block rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-semibold text-amber-700">
                  {roleLabel(t, inv.role)}
                </span>
              </div>
              <button
                onClick={() => cancelInvite(inv.id)}
                className="text-xs text-slate-500 hover:text-red-500"
              >
                {t("mem.cancelInvite")}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
