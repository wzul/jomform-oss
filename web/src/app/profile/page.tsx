"use client";

// Profile page — user identity & security (name, email, password, 2FA, passkeys),
// organised into Identity / Security / Passkeys tabs.
import { useCallback, useEffect, useState } from "react";
import QRCode from "qrcode";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";
import { useSession } from "../../components/session";
import { formatDate } from "../../lib/formatDate";

type Profile = {
  id: string;
  email: string;
  name: string;
  avatar_url: string; // issue #133
  phone: string; // issue #181 optional phone number (empty = not set)
  timezone: string; // issue #191 preferred IANA timezone (empty = Auto → browser default)
  totp_enabled: boolean;
  created_at: string;
};

// Issue #301: connected accounts (social login connectors) are user-level.
type SocialRow = { provider: string; email?: string; linked_at?: string };

// Issue #307: connected MCP agents (OAuth clients) are user-level too — a
// user binds one agent and it can operate across all their spaces.
type MCPAgentRow = {
  client_id: string;
  name: string;
  created_at?: string;
  last_used_at?: string;
};

type ProfileTab = "identity" | "security" | "passkeys" | "connected" | "mcp";
const TAB_IDS: ProfileTab[] = ["identity", "security", "passkeys", "connected", "mcp"];

// Issue #191: common IANA timezones offered in the Profile → Identity tab.
// The user can also pick Auto (empty) to follow the browser/PC default.
const COMMON_TIMEZONES = [
  "UTC",
  "Asia/Kuala_Lumpur",
  "Asia/Singapore",
  "Asia/Jakarta",
  "Asia/Manila",
  "Asia/Bangkok",
  "Asia/Ho_Chi_Minh",
  "Asia/Shanghai",
  "Asia/Hong_Kong",
  "Asia/Taipei",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Asia/Kolkata",
  "Asia/Dubai",
  "Asia/Riyadh",
  "Australia/Sydney",
  "Australia/Perth",
  "Pacific/Auckland",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Moscow",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Africa/Lagos",
  "Africa/Nairobi",
];

function loadStoredTab(): ProfileTab {
  if (typeof window === "undefined") return "identity";
  // Issue #310: when arriving via a social-connect callback (?linked=) or an
  // explicit ?tab=connected, open the Social accounts tab so the user sees
  // their freshly-linked account.
  const params = new URLSearchParams(window.location.search);
  if (params.get("linked") !== null) return "connected";
  if (params.get("tab") === "connected") return "connected";
  try {
    const v = window.localStorage.getItem("jf_profile_tab");
    if (v === "identity" || v === "security" || v === "passkeys" || v === "connected" || v === "mcp") return v;
  } catch {
    // storage unavailable — fall through
  }
  return "identity";
}

export default function ProfilePage() {
  const { t } = useI18n();
  const { user: sessionUser, reload: reloadSession } = useSession();
  const [tab, setTab] = useState<ProfileTab>(loadStoredTab);
  const [p, setP] = useState<Profile | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  // Issue #181: optional phone number state.
  const [phone, setPhone] = useState("");
  // Issue #191: preferred timezone state ("" = Auto → browser default).
  const [timezone, setTimezone] = useState("");
  // Issue #133: profile picture state — current avatar URL, upload progress.
  const [avatarUrl, setAvatarUrl] = useState("");
  const [avatarPreview, setAvatarPreview] = useState("");
  const [uploadingAvatar, setUploadingAvatar] = useState(false);
  const [avatarFailed, setAvatarFailed] = useState(false);
  const [pwCurrent, setPwCurrent] = useState("");
  const [pwNew, setPwNew] = useState("");
  const [pwConfirm, setPwConfirm] = useState("");
  const [msg, setMsg] = useState("");
  const [passkeys, setPasskeys] = useState<{ id: string }[]>([]);
  const [err, setErr] = useState("");
  // 2FA setup modal state.
  const [show2FA, setShow2FA] = useState(false);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [secretText, setSecretText] = useState("");
  const [code, setCode] = useState("");
  const [enabling, setEnabling] = useState(false);
  // Issue #63: trusted-device state for the Profile → Security line.
  const [trustedUntil, setTrustedUntil] = useState("");
  // Issue #301: connected accounts (user-level, independent of the active
  // space) — loaded via GET /api/auth/social (currentUserID-scoped).
  const [social, setSocial] = useState<SocialRow[]>([]);
  // Issue #307: connected MCP agents (user-level OAuth clients) — loaded via
  // GET /api/auth/mcp-agents.
  const [agents, setAgents] = useState<MCPAgentRow[]>([]);

  // Persist active tab.
  useEffect(() => {
    try {
      window.localStorage.setItem("jf_profile_tab", tab);
    } catch {
      // storage unavailable — ignore
    }
  }, [tab]);

  const load = useCallback(() => {
    // Issue #29: identity fields hydrate from the shared session context
    // (dashboard-bundle, fetched once per session) instead of a per-mount
    // /api/auth/profile round-trip. Saves call reloadSession() below, which
    // re-fetches the bundle so name/email/2FA state stay fresh. Passkeys
    // still load here, in parallel.
    if (sessionUser) {
      setP((prev) => ({
        id: sessionUser.id ?? prev?.id ?? "",
        email: sessionUser.email ?? prev?.email ?? "",
        name: sessionUser.name ?? prev?.name ?? "",
        avatar_url: sessionUser.avatar_url ?? prev?.avatar_url ?? "",
        phone: sessionUser.phone ?? prev?.phone ?? "",
        timezone: sessionUser.timezone ?? prev?.timezone ?? "",
        totp_enabled: sessionUser.totp_enabled ?? prev?.totp_enabled ?? false,
        created_at: prev?.created_at ?? "",
      }));
      setName((cur) => cur || sessionUser.name || "");
      setEmail((cur) => cur || sessionUser.email || "");
      setAvatarUrl((cur) => cur || sessionUser.avatar_url || "");
      setPhone((cur) => cur || sessionUser.phone || "");
      setTimezone((cur) => cur || sessionUser.timezone || "");
    }
    fetch("/api/auth/passkeys")
      .then((r) => (r.ok ? r.json() : []))
      .then((k) => setPasskeys(Array.isArray(k) ? k : []))
      .catch(() => setPasskeys([]));
    // Issue #301: connected accounts load straight from the user-level
    // /api/auth/social endpoint (no ?space=), so they're identical across all
    // of the user's spaces.
    fetch("/api/auth/social")
      .then((r) => (r.ok ? r.json() : []))
      .then((s) => setSocial(Array.isArray(s) ? s : []))
      .catch(() => setSocial([]));
    // Issue #307: connected MCP agents load straight from the user-level
    // /api/auth/mcp-agents endpoint (no ?space=), so they're identical across
    // all of the user's spaces.
    fetch("/api/auth/mcp-agents")
      .then((r) => (r.ok ? r.json() : []))
      .then((s) => setAgents(Array.isArray(s) ? s : []))
      .catch(() => setAgents([]));
  }, [sessionUser]);

  useEffect(() => { load(); }, [load]);

  // Issue #63: fetch trusted-device status for the Profile → Security line.
  const loadTrust = useCallback(() => {
    fetch("/api/auth/trust-status")
      .then((r) => (r.ok ? r.json() : { trusted: false }))
      .then((d) => setTrustedUntil(d.trusted ? d.until || "" : ""))
      .catch(() => setTrustedUntil(""));
  }, []);

  useEffect(() => { loadTrust(); }, [loadTrust]);

  async function post(path: string, body: unknown) {
    setMsg(""); setErr("");
    const r = await fetch(path, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (r.ok) { setMsg(t("prof.saved")); load(); reloadSession(); return true; }
    const d = await r.json().catch(() => ({}));
    setErr(d.error || t("prof.failed"));
    return false;
  }

  // Issue #133: upload a profile picture → POST /api/rest/uploads/avatar
  // (server downscales to ~256px) → save the returned URL.
  async function uploadAvatar(file: File) {
    setMsg(""); setErr("");
    if (!["image/png", "image/jpeg", "image/webp", "image/gif"].includes(file.type)) {
      setErr(t("prof.avatarTypeError"));
      return;
    }
    if (file.size > 20 * 1024 * 1024) {
      setErr(t("prof.avatarError"));
      return;
    }
    setUploadingAvatar(true);
    try {
      const fd = new FormData();
      fd.append("file", file);
      const r = await fetch("/api/rest/uploads/avatar", { method: "POST", body: fd });
      const d = await r.json().catch(() => ({}));
      if (!r.ok || !d.url) { setErr(t("prof.avatarError")); return; }
      await saveAvatarUrl(d.url);
    } finally {
      setUploadingAvatar(false);
    }
  }

  // Persist the avatar URL (or clear it when url is "") server-side.
  async function saveAvatarUrl(url: string): Promise<boolean> {
    const r = await fetch("/api/rest/profile/avatar", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ avatar_url: url }),
    });
    if (!r.ok) {
      const d = await r.json().catch(() => ({}));
      setErr(d.error || t("prof.avatarError"));
      return false;
    }
    setAvatarUrl(url);
    setAvatarPreview("");
    setAvatarFailed(false);
    setMsg(t("prof.avatarSaved"));
    load();
    reloadSession();
    return true;
  }

  // Issue #133: remove the profile picture (falls back to the initial letter).
  async function removeAvatar() {
    if (!window.confirm(t("prof.removeAvatar") + "?")) return;
    setMsg(""); setErr("");
    await saveAvatarUrl("");
  }

  async function registerPasskey() {
    setMsg(""); setErr("");
    try {
      const st = await fetch("/api/auth/passkeys/register/start", { method: "POST" }).then((r) => r.json());
      const opts = st.options.publicKey || st.options;
      // decode base64url fields
      opts.challenge = Uint8Array.from(atob(opts.challenge.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
      if (opts.user?.id) opts.user.id = Uint8Array.from(atob(opts.user.id.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
      const cred = await navigator.credentials.create({ publicKey: opts });
      const c = cred as PublicKeyCredential;
      const resp = c.response as AuthenticatorAttestationResponse;
      const body = {
        key: st.key,
        id: btoa(String.fromCharCode(...new Uint8Array(c.rawId))),
        rawId: btoa(String.fromCharCode(...new Uint8Array(c.rawId))),
        type: c.type,
        response: {
          attestationObject: btoa(String.fromCharCode(...new Uint8Array(resp.attestationObject))),
          clientDataJSON: btoa(String.fromCharCode(...new Uint8Array(resp.clientDataJSON))),
        },
      };
      const fin = await fetch("/api/auth/passkeys/register/finish", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      if (fin.ok) { setMsg(t("prof.passkeyRegistered")); load(); }
      else setErr((await fin.json().catch(() => ({}))).error || t("prof.passkeyRegFailed"));
    } catch {
      setErr(t("prof.passkeyUnsupported"));
    }
  }

  // Issue #39: 2FA setup must render the QR/secret client-side and hold the
  // user at a code-confirmation step before the account is actually enabled.
  async function start2FASetup() {
    setMsg(""); setErr(""); setShow2FA(false);
    try {
      const r = await fetch("/api/auth/2fa/setup", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: "{}",
      });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) { setErr(d.error || t("prof.twoFaSetupError")); return; }
      setSecretText(d.secret || "");
      const url = d.otpauth_url || "";
      if (url) {
        const dataUrl = await QRCode.toDataURL(url, { width: 220, margin: 1 });
        setQrDataUrl(dataUrl);
      }
      setCode("");
      setShow2FA(true);
    } catch {
      setErr(t("prof.twoFaSetupError"));
    }
  }

  async function enable2FA() {
    setMsg(""); setErr(""); setEnabling(true);
    try {
      const r = await fetch("/api/auth/2fa/enable", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ code }),
      });
      if (r.ok) {
        setShow2FA(false);
        setMsg(t("prof.twoFaEnabled"));
        setCode(""); setSecretText(""); setQrDataUrl("");
        // Issue #69: flip totp_enabled OPTIMISTICALLY so the "Aktif ✓" status
        // line and the success banner agree this frame — zero fetch dependency.
        // reloadSession() then reconciles from the single source of truth (the
        // re-fetched dashboard-bundle) once, and session.tsx keeps showing the
        // previous user (never "…") until that rotation resolves.
        setP((prev) => (prev ? { ...prev, totp_enabled: true } : prev));
        load();
        reloadSession();
      } else {
        const d = await r.json().catch(() => ({}));
        setErr(d.error || t("prof.failed"));
      }
    } catch {
      setErr(t("prof.failed"));
    } finally {
      setEnabling(false);
    }
  }

  return (
    <Shell active="profile">
      <main className="w-full flex-1 px-6 py-8">
        <h1 className="max-w-xl text-2xl font-bold">{t("prof.title")}</h1>

        {/* Tab bar */}
        <div className="mt-4 flex gap-1 border-b border-slate-200">
          {TAB_IDS.map((id) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              aria-selected={tab === id}
              role="tab"
              className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                tab === id
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-800"
              }`}
            >
              <span className="sm:hidden">{t(`prof.${id}` as const)}</span>
              <span className="hidden sm:inline">{t(`prof.${id}` as const)}</span>
            </button>
          ))}
        </div>

        {/* Banners — shown above tab content regardless of active tab */}
        {msg && <p className="mt-4 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
        {err && <p className="mt-4 rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700">{err}</p>}

        {/* Identity tab */}
        {tab === "identity" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.identity")}</h2>
              {/* Issue #133: profile picture upload + preview, fallback to initial. */}
              <div className="mt-4 flex items-center gap-4">
                <div className="relative">
                  {avatarPreview || (avatarUrl && !avatarFailed) ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={avatarPreview || avatarUrl}
                      alt={name || email || ""}
                      onError={() => setAvatarFailed(true)}
                      className="h-20 w-20 rounded-full object-cover ring-1 ring-slate-200"
                    />
                  ) : (
                    <div className="flex h-20 w-20 items-center justify-center rounded-full bg-indigo-100 text-2xl font-semibold text-[var(--jf-primary)] ring-1 ring-slate-200">
                      {((name || email || "?")[0] || "?").toUpperCase()}
                    </div>
                  )}
                </div>
                <div className="flex flex-col gap-2">
                  <label className="jf-btn-ghost cursor-pointer px-4 py-2 text-sm">
                    {uploadingAvatar ? t("prof.uploadingAvatar") : t("prof.uploadAvatar")}
                    <input
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/gif"
                      className="sr-only"
                      disabled={uploadingAvatar}
                      onChange={(e) => {
                        const f = e.target.files?.[0];
                        if (!f) return;
                        setAvatarPreview(URL.createObjectURL(f));
                        uploadAvatar(f);
                      }}
                    />
                  </label>
                  {(avatarUrl || avatarPreview) && (
                    <button onClick={removeAvatar} className="self-start px-2 text-sm text-red-500 hover:underline">
                      {t("prof.removeAvatar")}
                    </button>
                  )}
                </div>
              </div>
              <div className="mt-4 border-t border-slate-100 pt-4">
              <label className="mt-3 block text-xs text-slate-500">{t("prof.displayName")}</label>
              <input value={name} onChange={(e) => setName(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder={t("prof.yourName")} />
              <button
                onClick={() => post("/api/auth/profile/name", { name })}
                className="jf-btn-primary mt-3"
              >{t("prof.saveName")}</button>
              </div>
              {/* Issue #181: optional phone number — blank = not set. */}
              <div className="mt-4 border-t border-slate-100 pt-4">
                <label className="mt-3 block text-xs text-slate-500">{t("prof.phone")}</label>
                <input
                  value={phone}
                  onChange={(e) => setPhone(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                  placeholder={t("prof.phonePlaceholder")}
                  inputMode="tel"
                />
                <button
                  onClick={() => post("/api/auth/profile/phone", { phone })}
                  className="jf-btn-primary mt-3"
                >{t("prof.savePhone")}</button>
              </div>
              {/* Issue #191: preferred timezone — Auto (browser/PC) or a hard-coded IANA timezone. */}
              <div className="mt-4 border-t border-slate-100 pt-4">
                <label className="mt-3 block text-xs text-slate-500">{t("prof.timezone")}</label>
                <select
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                  className="jf-input mt-1 max-w-xl"
                >
                  <option value="">{t("prof.timezoneAuto")}</option>
                  {COMMON_TIMEZONES.map((tz) => (
                    <option key={tz} value={tz}>{tz}</option>
                  ))}
                </select>
                <button
                  onClick={() => post("/api/auth/profile/timezone", { timezone })}
                  className="jf-btn-primary mt-3"
                >{t("prof.saveTimezone")}</button>
              </div>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.emailAddress")}</h2>
              <p className="mt-1 text-xs text-slate-400">
                {t("prof.current")}{" "}
                {p ? (
                  <strong>{p.email}</strong>
                ) : (
                  <span aria-hidden="true" className="inline-block h-3 w-44 animate-pulse rounded bg-slate-200 align-middle" />
                )}
              </p>
              <label className="mt-3 block text-sm">{t("prof.newEmail")}</label>
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="jf-input mt-1 max-w-xl" placeholder="new@email.com" />
              <label className="mt-3 block text-sm">{t("prof.currentPwConfirm")}</label>
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <button
                onClick={async () => {
                  if (await post("/api/auth/profile/email", { email, password: pwCurrent })) {
                    setPwCurrent("");
                  }
                }}
                className="jf-btn-primary mt-3"
              >{t("prof.changeEmail")}</button>
            </section>
          </>
        )}

        {/* Security tab */}
        {tab === "security" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.password")}</h2>
              <label className="mt-3 block text-sm">{t("prof.currentPw")}</label>
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <label className="mt-2 block text-sm">{t("prof.newPw")}</label>
              <input type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <label className="mt-2 block text-sm">{t("prof.confirmPw")}</label>
              <input type="password" value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)} className="jf-input mt-1 max-w-xl" />
              <button
                onClick={async () => {
                  if (pwNew !== pwConfirm) { setErr(t("prof.pwMismatch")); return; }
                  await post("/api/auth/change-password", { password: pwCurrent, new_password: pwNew });
                  setPwCurrent(""); setPwNew(""); setPwConfirm("");
                }}
                className="jf-btn-primary mt-3"
              >{t("prof.changePw")}</button>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">{t("prof.twoFa")}</h2>
              <p className="mt-1 text-sm text-slate-500">
                {t("prof.status")} <strong>{p?.totp_enabled ? t("prof.enabled") : t("prof.notEnabled")}</strong>
              </p>
              <button
                onClick={start2FASetup}
                className="jf-btn-ghost mt-3"
              >
                {p?.totp_enabled ? t("prof.resetup2fa") : t("prof.setup2fa")}
              </button>
              {p?.totp_enabled && (
                <button
                  onClick={async () => {
                    // Backend handle2FADisable verifies the account password
                    // (not a TOTP code) before disabling.
                    const password = window.prompt(t("prof.enter2faCode"));
                    if (password) {
                      const ok = await post("/api/auth/2fa/disable", { password });
                      // #69: flip optimistically so the status line ("Belum
                      // aktif") agrees with reality immediately; post() already
                      // reloadSession()'d (keeps prev user until it resolves).
                      if (ok) {
                        setP((prev) => (prev ? { ...prev, totp_enabled: false } : prev));
                      }
                      setTrustedUntil("");
                    }
                  }}
                  className="ml-2 px-4 py-2 text-sm text-red-500 hover:underline"
                >{t("prof.disable2fa")}</button>
              )}
              {/* Issue #63: trust this device status + forget control. */}
              {p?.totp_enabled && trustedUntil && (
                <div className="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm">
                  <span className="text-emerald-700">
                    {t("prof.trusted", { date: formatDate(trustedUntil, sessionUser?.timezone) })}
                  </span>
                  <button
                    onClick={async () => {
                      const r = await fetch("/api/auth/trust/forget", { method: "POST" });
                      if (r.ok) setTrustedUntil("");
                    }}
                    className="text-[13px] text-red-500 hover:underline"
                  >{t("prof.forgetDevice")}</button>
                </div>
              )}
            </section>
          </>
        )}

        {/* Passkeys tab */}
        {tab === "passkeys" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("prof.passkeys")}</h2>
            <p className="mt-1 text-sm text-slate-500">
              {t("prof.passkeyDesc")}
            </p>
            <button onClick={registerPasskey} className="jf-btn-primary mt-3">
              {t("prof.registerPasskey")}
            </button>
            {passkeys.length > 0 && (
              <ul className="mt-3 space-y-1.5">
                {passkeys.map((k) => (
                  <li key={k.id} className="flex items-center justify-between rounded-md border border-slate-100 px-3 py-2 text-xs">
                    <span className="font-mono text-slate-500">{k.id.slice(0, 20)}…</span>
                    <button
                      onClick={async () => {
                        if (!window.confirm(t("prof.removePasskeyConfirm"))) return;
                        await fetch(`/api/auth/passkeys/${k.id}`, { method: "DELETE" });
                        load();
                      }}
                      className="text-red-500 hover:underline"
                    >{t("prof.remove")}</button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        )}

        {/* Connected accounts tab (issue #301): user-level social login
            connectors, moved here from space-scoped Settings. Fetched from
            the user-level /api/auth/social endpoint — not tied to the active
            space. The space-level Cloudflare auto-CNAME connect stays in
            Settings → Domains. */}
        {tab === "connected" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("prof.connectedTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("prof.connectedHelp")}
            </p>
            <ul className="mt-4 space-y-2">
              {social.map((l) => (
                <li key={l.provider} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                  <div>
                    <p className="text-sm capitalize">{l.provider} — {l.email}</p>
                    <p className="text-[11px] text-slate-400">{t("prof.linked")} {l.linked_at ? formatDate(l.linked_at, sessionUser?.timezone) : ""}</p>
                  </div>
                  <button onClick={async () => { await fetch(`/api/auth/social/${l.provider}`, { method: "DELETE" }); load(); }}
                    className="text-xs text-slate-500 hover:text-red-500">{t("prof.unlink")}</button>
                </li>
              ))}
              {social.length === 0 && (
                <li className="text-sm text-slate-400">{t("prof.noLinked")}</li>
              )}
            </ul>
            <a href="/api/auth/oauth/google" className="jf-btn-ghost mt-3 inline-block">
              {t("prof.linkGoogle")}
            </a>
          </section>
        )}

        {/* Connected agents tab (issue #307): user-level MCP OAuth clients,
            moved here from space-scoped Settings. A connected agent can
            operate across every space the user owns, up to the user's role in
            each — so the connection is user-level, not per-space. */}
        {tab === "mcp" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">{t("prof.mcpTitle")}</h2>
            <p className="mt-1 max-w-xl text-sm text-slate-400">
              {t("set.serverUrl")}{" "}
              <code className="rounded bg-slate-100 px-1">https://mcp.jomform.com/mcp</code> —{" "}
              {t("set.mcpHelp")}{" "}
              <code className="rounded bg-slate-100 px-1">/.well-known/oauth-authorization-server</code>{" "}
              {t("set.mcpNoKeys")}
            </p>
            <ul className="mt-4 space-y-2">
              {agents.map((a) => (
                <li key={a.client_id} className="flex items-center justify-between rounded-lg border border-[var(--jf-border)] px-4 py-2.5">
                  <div>
                    <p className="text-sm">{a.name || a.client_id}</p>
                    <p className="text-[11px] text-slate-400">{t("set.clientId")} {a.client_id}</p>
                  </div>
                  <button
                    onClick={async () => {
                      if (!(window.confirm(t("prof.mcpDisconnectConfirm") + "?"))) return;
                      await fetch(`/api/auth/mcp-agents/${encodeURIComponent(a.client_id)}`, { method: "DELETE" });
                      load();
                    }}
                    className="text-xs text-slate-500 hover:text-red-500"
                  >
                    {t("set.revoke")}
                  </button>
                </li>
              ))}
              {agents.length === 0 && (
                <li className="rounded-lg border border-dashed border-[var(--jf-border)] px-4 py-3 text-sm text-slate-400">
                  {t("set.noAgents")}
                </li>
              )}
            </ul>
            <div className="mt-6 border-t border-[var(--jf-border)] pt-5">
              <h3 className="text-sm font-semibold">{t("prof.mcpScopeTitle")}</h3>
              <p className="mt-1 text-xs text-slate-400">
                {t("prof.mcpScopeHelp")}
              </p>
            </div>
          </section>
        )}

        {/* Issue #39: 2FA setup modal — QR rendered client-side from otpauth_url
            (no external image service), raw secret shown as text, then a
            code-confirmation step before the account is actually enabled. */}
        {show2FA && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
            role="dialog"
            aria-modal="true"
            aria-label={t("prof.twoFaSetupTitle")}
            onClick={() => setShow2FA(false)}
          >
            <div
              className="jf-card w-full max-w-md p-6"
              onClick={(e) => e.stopPropagation()}
            >
              <h2 className="text-lg font-semibold">{t("prof.twoFaSetupTitle")}</h2>
              <ol className="mt-3 space-y-2 text-sm text-slate-600">
                <li>{t("prof.twoFaScan")}</li>
                <li>
                  {t("prof.twoFaManual")}
                  <div className="mt-1 rounded-md border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs tracking-widest select-all">
                    {secretText}
                  </div>
                </li>
              </ol>
              {qrDataUrl ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={qrDataUrl}
                  alt="QR code"
                  width={220}
                  height={220}
                  className="mx-auto mt-3 rounded border border-slate-200"
                />
              ) : (
                <div className="mx-auto mt-3 flex h-[220px] w-[220px] items-center justify-center rounded border border-slate-200 bg-slate-50 text-xs text-slate-400">…</div>
              )}
              <label className="mt-4 block text-sm">{t("prof.twoFaCodeLabel")}</label>
              <input
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                inputMode="numeric"
                pattern="[0-9]*"
                autoComplete="one-time-code"
                placeholder={t("prof.twoFaCodePlaceholder")}
                className="jf-input mt-1 font-mono tracking-[0.4em]"
              />
              <div className="mt-4 flex justify-end gap-2">
                <button onClick={() => setShow2FA(false)} className="jf-btn-ghost px-4 py-2 text-sm">
                  {t("prof.twoFaClose")}
                </button>
                <button
                  onClick={enable2FA}
                  disabled={!code || code.length !== 6 || enabling}
                  className={`${(!code || code.length !== 6) ? "opacity-50" : ""} jf-btn-primary`}
                >
                  {enabling ? "…" : t("prof.twoFaEnabling")}
                </button>
              </div>
            </div>
          </div>
        )}
      </main>
    </Shell>
  );
}