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

const TABS: { id: ProfileTab; label: string; short: string }[] = [
  { id: "identity", label: "Identity", short: "Identity" },
  { id: "security", label: "Security", short: "Keselamatan" },
  { id: "passkeys", label: "Passkeys", short: "Passkey" },
];

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
    if (r.ok) { setMsg("Saved ✓"); load(); return true; }
    const d = await r.json().catch(() => ({}));
    setErr(d.error || "Failed");
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
      if (fin.ok) { setMsg("Passkey berdaftar ✓"); load(); }
      else setErr((await fin.json().catch(() => ({}))).error || "Gagal daftar passkey");
    } catch {
      setErr("Passkey dibatalkan atau tidak disokong pelayar.");
    }
  }

  return (
    <Shell active="profile">
      <main className="mx-auto max-w-2xl flex-1 px-6 py-8">
        <h1 className="text-2xl font-bold">Profile</h1>

        {/* Tab bar */}
        <div className="mt-4 flex gap-1 border-b border-slate-200">
          {TABS.map((t) => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              aria-selected={tab === t.id}
              role="tab"
              className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium sm:px-4 ${
                tab === t.id
                  ? "border-slate-900 text-slate-900"
                  : "border-transparent text-slate-500 hover:text-slate-800"
              }`}
            >
              <span className="sm:hidden">{t.short}</span>
              <span className="hidden sm:inline">{t.label}</span>
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
              <h2 className="text-lg font-semibold">Identity</h2>
              <label className="mt-3 block text-xs text-slate-500">Display name</label>
              <input value={name} onChange={(e) => setName(e.target.value)} className="jf-input mt-1" placeholder="Your name" />
              <button
                onClick={() => post("/api/auth/profile/name", { name })}
                className="jf-btn-primary mt-3"
              >Save name</button>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">Email address</h2>
              <p className="mt-1 text-xs text-slate-400">
                {"Current: "}
                {p ? (
                  <strong>{p.email}</strong>
                ) : (
                  <span aria-hidden="true" className="inline-block h-3 w-44 animate-pulse rounded bg-slate-200 align-middle" />
                )}
              </p>
              <label className="mt-3 block text-sm">New email</label>
              <input value={email} onChange={(e) => setEmail(e.target.value)} className="jf-input mt-1" placeholder="new@email.com" />
              <label className="mt-3 block text-sm">Current password (to confirm)</label>
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1" />
              <button
                onClick={async () => {
                  if (await post("/api/auth/profile/email", { email, password: pwCurrent })) {
                    setPwCurrent("");
                  }
                }}
                className="jf-btn-primary mt-3"
              >Change email</button>
            </section>
          </>
        )}

        {/* Security tab */}
        {tab === "security" && (
          <>
            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">Password</h2>
              <label className="mt-3 block text-sm">Current password</label>
              <input type="password" value={pwCurrent} onChange={(e) => setPwCurrent(e.target.value)} className="jf-input mt-1" />
              <label className="mt-2 block text-sm">New password (min. 10 chars)</label>
              <input type="password" value={pwNew} onChange={(e) => setPwNew(e.target.value)} className="jf-input mt-1" />
              <label className="mt-2 block text-sm">Confirm new password</label>
              <input type="password" value={pwConfirm} onChange={(e) => setPwConfirm(e.target.value)} className="jf-input mt-1" />
              <button
                onClick={async () => {
                  if (pwNew !== pwConfirm) { setErr("Passwords don't match"); return; }
                  await post("/api/auth/change-password", { password: pwCurrent, new_password: pwNew });
                  setPwCurrent(""); setPwNew(""); setPwConfirm("");
                }}
                className="jf-btn-primary mt-3"
              >Change password</button>
            </section>

            <section className="jf-card mt-6 p-6">
              <h2 className="text-lg font-semibold">Two-factor authentication (TOTP)</h2>
              <p className="mt-1 text-sm text-slate-500">
                Status: <strong>{p?.totp_enabled ? "Enabled ✓" : "Not enabled"}</strong>
              </p>
              <button
                onClick={() => post("/api/auth/2fa/setup", {})}
                className="jf-btn-ghost mt-3"
              >
                {p?.totp_enabled ? "Re-setup 2FA" : "Set up 2FA"}
              </button>
              {p?.totp_enabled && (
                <button
                  onClick={async () => {
                    const code = window.prompt("Enter a current 2FA code to disable:");
                    if (code) await post("/api/auth/2fa/disable", { totp_code: code });
                  }}
                  className="ml-2 px-4 py-2 text-sm text-red-500 hover:underline"
                >Disable 2FA</button>
              )}
            </section>
          </>
        )}

        {/* Passkeys tab */}
        {tab === "passkeys" && (
          <section className="jf-card mt-6 p-6">
            <h2 className="text-lg font-semibold">Passkeys</h2>
            <p className="mt-1 text-sm text-slate-500">
              Log masuk dengan cap jari / wajah / PIN peranti — tanpa kata laluan.
            </p>
            <button onClick={registerPasskey} className="jf-btn-primary mt-3">
              Daftar passkey
            </button>
            {passkeys.length > 0 && (
              <ul className="mt-3 space-y-1.5">
                {passkeys.map((k) => (
                  <li key={k.id} className="flex items-center justify-between rounded-md border border-slate-100 px-3 py-2 text-xs">
                    <span className="font-mono text-slate-500">{k.id.slice(0, 20)}…</span>
                    <button
                      onClick={async () => {
                        if (!window.confirm("Buang passkey ini?")) return;
                        await fetch(`/api/auth/passkeys/${k.id}`, { method: "DELETE" });
                        load();
                      }}
                      className="text-red-500 hover:underline"
                    >Buang</button>
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