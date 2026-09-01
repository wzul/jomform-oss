"use client";

// Profile page — user identity & security (name, email, password, 2FA, passkeys),
// organised into Identity / Security / Passkeys tabs.
import { useCallback, useEffect, useState } from "react";
import { Shell } from "../../components/Shell";
import { useI18n } from "../../components/i18n";

type Profile = {
  id: string;
  email: string;
  name: string;
  totp_enabled: boolean;
  created_at: string;
};

type ProfileTab = "identity" | "security" | "passkeys";

const TAB_IDS: ProfileTab[] = ["identity", "security", "passkeys"];

function loadStoredTab(): ProfileTab {
  if (typeof window === "undefined") return "identity";
  try {
    const v = window.localStorage.getItem("jf_profile_tab");
    if (v === "identity" || v === "security" || v === "passkeys") return v;
  } catch {
    // storage unavailable — fall through
  }
  return "identity";
}

export default function ProfilePage() {
  const { t } = useI18n();
  const [tab, setTab] = useState<ProfileTab>(loadStoredTab);
  const [p, setP] = useState<Profile | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [pwCurrent, setPwCurrent] = useState("");
  const [pwNew, setPwNew] = useState("");
  const [pwConfirm, setPwConfirm] = useState("");
  const [msg, setMsg] = useState("");
  const [passkeys, setPasskeys] = useState<{ id: string }[]>([]);
  const [err, setErr] = useState("");

  // Persist active tab.
  useEffect(() => {
    try {
      window.localStorage.setItem("jf_profile_tab", tab);
    } catch {
      // storage unavailable — ignore
    }
  }, [tab]);

  const load = useCallback(() => {
    fetch("/api/auth/profile")
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (d) {
          setP(d);
          setName(d.name || "");
          setEmail(d.email || "");
        }
      });
    fetch("/api/auth/passkeys")
      .then((r) => (r.ok ? r.json() : []))
      .then((k) => setPasskeys(Array.isArray(k) ? k : []))
      .catch(() => setPasskeys([]));
  }, []);

  useEffect(() => { load(); }, [load]);

  async function post(path: string, body: unknown) {
    setMsg(""); setErr("");
    const r = await fetch(path, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (r.ok) { setMsg(t("prof.saved")); load(); return true; }
    const d = await r.json().catch(() => ({}));
    setErr(d.error || t("prof.failed"));
    return false;
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

  return (
    <Shell active="profile">
      <main className="mx-auto max-w-2xl flex-1 px-6 py-8">
        <h1 className="text-2xl font-bold">{t("prof.title")}</h1>

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
              <label className="mt-3 block text-xs text-slate-500">{t("prof.displayName")}</label>
              <input value={name} onChange={(e) => setName(e.target.value)} className="jf-input mt-1" placeholder={t("prof.yourName")} />
              <button
                onClick={() => post("/api/auth/profile/name", { name })}
                className="jf-btn-primary mt-3"
              >{t("prof.saveName")}</button>
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
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="jf-input mt-1" placeholder="new@email.com" />
              <label className="mt-3 block text-sm">{t("prof.currentPwConfirm")}</label>
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1" />
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
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1" />
              <label className="mt-2 block text-sm">{t("prof.newPw")}</label>
              <input type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} className="jf-input mt-1" />
              <label className="mt-2 block text-sm">{t("prof.confirmPw")}</label>
              <input type="password" value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)} className="jf-input mt-1" />
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
                onClick={() => post("/api/auth/2fa/setup", {})}
                className="jf-btn-ghost mt-3"
              >
                {p?.totp_enabled ? t("prof.resetup2fa") : t("prof.setup2fa")}
              </button>
              {p?.totp_enabled && (
                <button
                  onClick={async () => {
                    const code = window.prompt(t("prof.enter2faCode"));
                    if (code) await post("/api/auth/2fa/disable", { totp_code: code });
                  }}
                  className="ml-2 px-4 py-2 text-sm text-red-500 hover:underline"
                >{t("prof.disable2fa")}</button>
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
      </main>
    </Shell>
  );
}