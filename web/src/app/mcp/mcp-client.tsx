"use client";

// issue #120: MCP setup instructions page — publicly served at /mcp.
// Shows the correct server URL (https://mcp.jomform.com/mcp) with a copy
// button, per-client config examples, the OAuth 2.1 flow, example commands
// and troubleshooting. Reachable pre-login (no session guard).
import { useEffect, useState } from "react";
import Link from "next/link";
import { useI18n, LANG_SHORT, LANGS_SHOWN } from "../../components/i18n";
import { endonymLangProps } from "../../lib/endonymLang";

const SERVER_URL = "https://mcp.jomform.com/mcp";

// Claude / Claude Desktop config example — the transport type for a remote
// (HTTP + OAuth 2.1) MCP server is `"type": "http"`.
const CLAUDE_CONFIG = `{
  "mcpServers": {
    "jomform": {
      "type": "http",
      "url": "${SERVER_URL}"
    }
  }
}`;

function CopyBtn({ value, label, copiedLabel }: { value: string; label: string; copiedLabel: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      onClick={() => {
        navigator.clipboard.writeText(value).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
      className="rounded-md bg-[var(--jf-primary)] px-3 py-1.5 text-xs font-medium text-white hover:opacity-90"
    >
      {done ? copiedLabel : label}
    </button>
  );
}

function CopyBlock({ code, copyLabel, copiedLabel }: { code: string; copyLabel: string; copiedLabel: string }) {
  const [done, setDone] = useState(false);
  return (
    <div className="relative mt-2">
      <button
        onClick={() => {
          navigator.clipboard.writeText(code).then(() => {
            setDone(true);
            setTimeout(() => setDone(false), 1500);
          });
        }}
        className="absolute right-2 top-2 rounded-md bg-white/10 px-2 py-1 text-[11px] font-medium text-white hover:bg-white/20"
      >
        {done ? copiedLabel : copyLabel}
      </button>
      <pre className="overflow-x-auto rounded-lg bg-slate-900 p-4 text-[11px] leading-relaxed text-slate-100">
        {code}
      </pre>
    </div>
  );
}

export default function McpPage() {
  const { t, lang, setLang } = useI18n();

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  const steps = [
    { tt: t("mcp.step1t"), b: t("mcp.step1b") },
    { tt: t("mcp.step2t"), b: t("mcp.step2b") },
    { tt: t("mcp.step3t"), b: t("mcp.step3b") },
    { tt: t("mcp.step4t"), b: t("mcp.step4b") },
  ];

  return (
    <main className="min-h-screen bg-white">
      {/* top bar */}
      <nav className="flex items-center justify-between border-b border-slate-100 px-5 py-3">
        <Link href="/" className="text-sm font-medium text-slate-600 hover:text-slate-900">
          {t("mcp.backHome")}
        </Link>
        <div className="flex overflow-hidden rounded-md border border-slate-200 text-xs">
          {LANGS_SHOWN.map((l) => (
            <button
              key={l}
              onClick={() => setLang(l)}
              // The endonym is written in its own language (中文 → zh, தமிழ் → ta,
              // عربي → ar); without this the page's voice read it as English on
              // /mcp (WCAG 3.1.2).
              {...endonymLangProps(l)}
              className={`px-2 py-1 font-medium transition-colors ${
                lang === l ? "bg-[var(--jf-primary)] text-white" : "bg-white text-slate-500"
              }`}
            >
              {LANG_SHORT[l]}
            </button>
          ))}
        </div>
      </nav>

      <div className="mx-auto max-w-3xl px-6 py-12">
        <p className="text-3xl">🤖</p>
        <h1 className="mt-2 text-3xl font-bold tracking-tight text-slate-900">{t("mcp.title")}</h1>
        <p className="mt-3 text-sm leading-relaxed text-slate-600">{t("mcp.subtitle")}</p>

        {/* Server URL */}
        <section className="jf-card mt-8 p-6">
          <p className="text-sm font-medium text-slate-700">{t("mcp.urlLabel")}</p>
          <div className="mt-3 flex items-center gap-3">
            <code className="min-w-0 flex-1 break-all rounded-lg bg-slate-900 px-4 py-3 font-mono text-[13px] text-emerald-300">
              {SERVER_URL}
            </code>
            <CopyBtn value={SERVER_URL} label={t("mcp.copy")} copiedLabel={t("mcp.copied")} />
          </div>
        </section>

        {/* What is a connected agent */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("mcp.whatTitle")}</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">{t("mcp.whatBody")}</p>
        </section>

        {/* Steps */}
        <section className="mt-8">
          <h2 className="text-lg font-semibold">{t("mcp.stepsTitle")}</h2>
          <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-4">
            {steps.map((s, i) => (
              <div key={s.tt} className="rounded-xl border border-slate-200 p-4">
                <span className="flex h-7 w-7 items-center justify-center rounded-full bg-[var(--jf-primary)] text-xs font-bold text-white">
                  {i + 1}
                </span>
                <p className="mt-3 text-sm font-semibold text-slate-900">{s.tt}</p>
                <p className="mt-1 text-xs leading-relaxed text-slate-500">{s.b}</p>
              </div>
            ))}
          </div>
        </section>

        {/* Per-client examples */}
        <section className="mt-8">
          <h2 className="text-lg font-semibold">{t("mcp.clientsTitle")}</h2>

          <div className="jf-card mt-4 p-6">
            <h3 className="text-sm font-semibold text-slate-900">{t("mcp.claudeTitle")}</h3>
            <p className="mt-1 text-xs text-slate-500">{t("mcp.claudeIntro")}</p>
            <CopyBlock
              code={CLAUDE_CONFIG}
              copyLabel={t("mcp.copy")}
              copiedLabel={t("mcp.copied")}
            />
          </div>

          <div className="jf-card mt-4 p-6">
            <h3 className="text-sm font-semibold text-slate-900">{t("mcp.genericTitle")}</h3>
            <p className="mt-1 text-xs text-slate-500">{t("mcp.genericIntro")}</p>
            <p className="mt-3 text-sm">
              <span className="text-slate-500">{t("mcp.urlLabel")} </span>
              <code className="break-all rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[12px] text-slate-800">
                {SERVER_URL}
              </code>
            </p>
          </div>
        </section>

        {/* Scope choice: which grant the agent gets is asked for by the client
            at authorize time, and the API enforces it on every write tool.
            Documented here because a merchant choosing a connection needs to
            know what "read-only" actually guarantees. */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("mcp.scopesTitle")}</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">{t("mcp.scopesBody")}</p>
          <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="rounded-xl border border-slate-200 p-4">
              <p className="text-sm font-semibold text-slate-900">
                <code className="rounded bg-slate-100 px-1 font-mono text-[12px]">read</code>{" "}
                {t("mcp.scopeRead")}
              </p>
              <p className="mt-1 text-xs leading-relaxed text-slate-500">{t("mcp.scopeReadBody")}</p>
            </div>
            <div className="rounded-xl border border-slate-200 p-4">
              <p className="text-sm font-semibold text-slate-900">
                <code className="rounded bg-slate-100 px-1 font-mono text-[12px]">readwrite</code>{" "}
                {t("mcp.scopeReadWrite")}
              </p>
              <p className="mt-1 text-xs leading-relaxed text-slate-500">
                {t("mcp.scopeReadWriteBody")}
              </p>
            </div>
          </div>
          <p className="mt-3 text-xs leading-relaxed text-slate-500">{t("mcp.scopeReadOnlyNote")}</p>
        </section>

        {/* Example commands */}
        <section className="jf-card mt-6 p-6">
          <h2 className="text-lg font-semibold">{t("mcp.examplesTitle")}</h2>
          <p className="mt-2 text-sm leading-relaxed text-slate-600">{t("mcp.examplesBody")}</p>
        </section>

        {/* Troubleshooting */}
        <section className="jf-card mt-6 border-amber-200 bg-amber-50/50 p-6">
          <h2 className="text-lg font-semibold text-amber-900">{t("mcp.troubleTitle")}</h2>
          <ul className="mt-3 list-disc space-y-2 pl-5 text-sm leading-relaxed text-amber-900">
            <li>{t("mcp.troubleBare")}</li>
            <li>{t("mcp.troubleConnect")}</li>
          </ul>
        </section>
      </div>
    </main>
  );
}
