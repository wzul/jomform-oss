"use client";

// Marketing landing page — i18n (EN default, BM toggle in-shell).
import { useEffect } from "react";
import { useI18n } from "../../components/i18n";

const FEATURES = [
  { icon: "✏️", key: "land.f1" as const },
  { icon: "💳", key: "land.f2" as const },
  { icon: "🤖", key: "land.f3" as const },
  { icon: "📊", key: "land.f4" as const },
];

const STEPS = [
  { t: "land.step1t" as const, b: "land.step1b" as const },
  { t: "land.step2t" as const, b: "land.step2b" as const },
  { t: "land.step3t" as const, b: "land.step3b" as const },
];

export default function Landing() {
  const { t, lang } = useI18n();

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  return (
    <main className="min-h-screen bg-white">
      {/* nav */}
      <nav className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-[15px] font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <div className="flex items-center gap-4">
          <a href="/builder" className="hidden text-sm text-slate-500 hover:text-slate-800 sm:block">
            {t("land.buildForm")}
          </a>
          <LangButtons />
        </div>
        <a href="/login" className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90">
          {t("land.login")}
        </a>
      </nav>

      {/* hero */}
      <section className="mx-auto max-w-3xl px-6 pb-16 pt-20 text-center">
        <p className="mb-3 inline-block rounded-full border border-indigo-100 bg-indigo-50 px-3 py-1 text-xs font-medium text-[#4338ca]">
          {t("land.badge")}
        </p>
        <h1 className="text-4xl font-bold tracking-tight text-slate-900 sm:text-5xl">
          {t("land.hero1")}{" "}
          <span className="text-[var(--jf-primary)]">{t("land.hero2")}</span>
        </h1>
        <p className="mx-auto mt-5 max-w-xl text-lg text-slate-500">{t("land.sub")}</p>
        <div className="mt-8 flex items-center justify-center gap-3">
          <a href="/login" className="rounded-xl bg-[var(--jf-primary)] px-6 py-3 text-sm font-semibold text-white hover:opacity-90">
            {t("land.cta1")}
          </a>
          <a href="#how" className="rounded-xl border border-slate-200 px-6 py-3 text-sm font-medium text-slate-600 hover:bg-slate-50">
            {t("land.cta2")}
          </a>
        </div>
      </section>

      {/* how it flows */}
      <section id="how" className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-3">
          {STEPS.map((s) => (
            <div key={s.t}>
              <p className="text-sm font-semibold text-[var(--jf-primary)]">{t(s.t)}</p>
              <p className="mt-2 text-sm text-slate-500">{t(s.b)}</p>
            </div>
          ))}
        </div>
      </section>

      {/* MCP selling point — the headline differentiator */}
      <section className="mx-auto max-w-4xl px-6 py-10">
        <div className="rounded-2xl border border-indigo-100 bg-gradient-to-br from-indigo-50 via-white to-white p-8 text-center">
          <p className="text-3xl">🤖</p>
          <h2 className="mt-2 text-xl font-bold text-slate-900 sm:text-2xl">
            {t("land.mcptitle")}
          </h2>
          <p className="mx-auto mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
            {t("land.mcpbody")}
          </p>
          <p className="mt-4 break-all rounded-xl bg-slate-900 p-4 font-mono text-[12px] text-emerald-300">
            {t("land.mcpsnippet")}
          </p>
          <a
            href="/mcp"
            className="mt-5 inline-block rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
          >
            {t("land.mcpguide")}
          </a>
        </div>
      </section>

      {/* use case showcase — wedding / RSVP (issue #381) */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <p className="text-center text-xs font-semibold uppercase tracking-widest text-[var(--jf-primary)]">
          {t("land.usecasetitle")}
        </p>
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-rose-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-rose-100 text-4xl">
              💍
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.weddingtitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.weddingbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.wedding1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.wedding2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.wedding3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.wedding4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/builder"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.weddingcta")}
                </a>
                <a
                  href="/mcp"
                  className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-50"
                >
                  {t("land.weddingmcp")}
                </a>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* features */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="grid grid-cols-1 gap-8 sm:grid-cols-2">
          {FEATURES.map((f) => (
            <div key={f.key} className="flex gap-3">
              <span className="text-2xl">{f.icon}</span>
              <p className="text-sm leading-relaxed text-slate-600">{t(f.key)}</p>
            </div>
          ))}
        </div>
      </section>

      {/* footer */}
      <footer className="border-t border-slate-100 px-6 py-8 text-center text-xs text-slate-500">
        <div className="flex justify-center gap-6 pb-4 text-xs text-slate-500">
          <a href="/legal/privacy" className="hover:underline">Privacy Policy</a>
          <a href="/legal/terms" className="hover:underline">Terms of Service</a>
          <a href="/docs" className="hover:underline">Docs</a>
          <a href="/brand" className="hover:underline">Brand</a>
        </div>
        <p>
          {t("land.footer")}
        </p>
      </footer>
    </main>
  );
}

function LangButtons() {
  const { lang, setLang } = useI18n();
  return (
    <div className="flex overflow-hidden rounded-md border border-slate-200 text-xs">
      {(["en", "ms"] as const).map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          className={`px-2 py-1 font-medium transition-colors ${
            lang === l ? "bg-[var(--jf-primary)] text-white" : "bg-white text-slate-500"
          }`}
        >
          {l === "en" ? "EN" : "BM"}
        </button>
      ))}
    </div>
  );
}