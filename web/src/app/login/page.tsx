"use client";

import { useState } from "react";

type Mode = "login" | "register";

export default function Login() {
  const [mode, setMode] = useState<Mode>("login");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [done, setDone] = useState(false);

  async function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const fd = new FormData(e.currentTarget);
    const path = mode === "login" ? "/api/auth/login" : "/api/auth/register";
    const body: Record<string, string> = {
      email: String(fd.get("email") || ""),
      password: String(fd.get("password") || ""),
    };
    if (needTotp) body.totp_code = String(fd.get("totp_code") || "");

    const r = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    setBusy(false);

    if (r.ok) {
      setDone(true);
      window.location.href = "/";
      return;
    }
    const d = await r.json().catch(() => ({}));
    if (d.next_step === "totp_required") {
      setNeedTotp(true);
      setError("Masukkan kod 6-angka dari aplikasi pengesah anda.");
      return;
    }
    setError(d.error || "Gagal. Cuba semula.");
  }

  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#08090a] px-6">
      {/* ambient glow */}
      <div
        aria-hidden
        className="pointer-events-none absolute left-1/2 top-[-260px] h-[420px] w-[720px] -translate-x-1/2 rounded-full opacity-40 blur-3xl"
        style={{
          background:
            "radial-gradient(closest-side, rgba(94,106,210,0.35), rgba(113,112,255,0.12), transparent)",
        }}
      />

      <div className="relative w-full max-w-sm">
        {/* logo */}
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-11 w-11 items-center justify-center rounded-xl bg-[#5e6ad2] text-lg font-bold text-white shadow-[0_0_24px_rgba(94,106,210,0.45)]">
            JF
          </div>
          <h1 className="text-xl font-semibold text-[#f7f8f8]" style={{ letterSpacing: "-0.02em" }}>
            {mode === "login" ? "Selamat kembali" : "Cipta akaun JomForm"}
          </h1>
          <p className="mt-1.5 text-sm text-[#8a8f98]">
            {mode === "login"
              ? "Log masuk untuk urus borang dan jualan anda."
              : "Percuma selamanya — borang pertama siap dalam 5 minit."}
          </p>
        </div>

        <div className="rounded-xl border border-white/[0.08] bg-[#0f1011] p-6 shadow-[0_0_0_1px_rgba(0,0,0,0.2),0_8px_24px_rgba(0,0,0,0.4)]">
          {/* tabs */}
          <div className="mb-5 grid grid-cols-2 rounded-lg border border-white/[0.06] bg-black/30 p-0.5">
            {(["login", "register"] as const).map((m) => (
              <button
                key={m}
                type="button"
                onClick={() => {
                  setMode(m);
                  setError("");
                  setNeedTotp(false);
                }}
                className={`rounded-md py-1.5 text-[13px] font-medium transition-colors ${
                  mode === m
                    ? "bg-[#5e6ad2]/20 text-[#c7c9ff] shadow-[inset_0_0_0_1px_rgba(113,112,255,0.35)]"
                    : "text-[#8a8f98] hover:text-[#d0d6e0]"
                }`}
              >
                {m === "login" ? "Log Masuk" : "Daftar"}
              </button>
            ))}
          </div>

          {error && (
            <p className="mb-4 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-[13px] text-red-300">
              {error}
            </p>
          )}

          <form onSubmit={submit} className="space-y-3">
            <div>
              <label className="mb-1 block text-xs font-medium text-[#8a8f98]">Emel</label>
              <input
                name="email"
                type="email"
                required
                autoComplete="email"
                placeholder="anda@kedai.my"
                className="w-full rounded-md border border-white/[0.08] bg-white/[0.02] px-3 py-2 text-sm text-[#f7f8f8] placeholder-[#62666d] outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-white/[0.04]"
              />
            </div>
            <div>
              <label className="mb-1 block text-xs font-medium text-[#8a8f98]">
                {mode === "register" ? "Kata laluan (min. 10 aksara)" : "Kata laluan"}
              </label>
              <input
                name="password"
                type="password"
                required
                minLength={mode === "register" ? 10 : 1}
                placeholder="••••••••••"
                className="w-full rounded-md border border-white/[0.08] bg-white/[0.02] px-3 py-2 text-sm text-[#f7f8f8] placeholder-[#62666d] outline-none transition-colors focus:border-[#7170ff]/60 focus:bg-white/[0.04]"
              />
            </div>
            {needTotp && (
              <div>
                <label className="mb-1 block text-xs font-medium text-[#8a8f98]">Kod 2FA</label>
                <input
                  name="totp_code"
                  inputMode="numeric"
                  pattern="[0-9]*"
                  maxLength={6}
                  required
                  placeholder="000000"
                  className="w-full rounded-md border border-white/[0.08] bg-white/[0.02] px-3 py-2 font-mono text-sm tracking-[0.4em] text-[#f7f8f8] outline-none focus:border-[#7170ff]/60"
                />
              </div>
            )}
            <button
              disabled={busy}
              className="mt-1 w-full rounded-md bg-[#5e6ad2] py-2 text-sm font-medium text-white transition-colors hover:bg-[#828fff] disabled:opacity-60"
            >
              {busy ? "…" : mode === "login" ? "Log Masuk" : "Cipta Akaun Percuma"}
            </button>
          </form>
        </div>

        <p className="mt-6 text-center text-xs text-[#62666d]">
          Dengan meneruskan, anda bersetuju dengan Terma & Polisi Privasi JomForm.
        </p>
      </div>
    </main>
  );
}