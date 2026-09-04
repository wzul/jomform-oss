import type { Metadata } from "next";
import { PRIVACY_SECTIONS, PRIVACY_POLICY } from "../privacy-content";

export const metadata: Metadata = {
  title: "Privacy Policy — JomForm",
  description: "How JomForm collects, uses, and protects your data.",
  alternates: { canonical: "https://jomform.com/legal/privacy" },
  openGraph: {
    title: "Privacy Policy — JomForm",
    description: "How JomForm collects, uses, and protects your data.",
    url: "https://jomform.com/legal/privacy",
    siteName: "JomForm",
    type: "website",
  },
};

export default function PrivacyPage() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <h1 className="text-3xl font-bold">Privacy Policy</h1>
      <p className="mt-2 text-sm text-slate-600">Last updated: {PRIVACY_POLICY.lastUpdated}</p>
      <div className="mt-8 space-y-8">
        {PRIVACY_SECTIONS.map((s) => (
          <section key={s.title}>
            <h2 className="text-lg font-semibold">{s.title}</h2>
            <div className="mt-2 space-y-2 text-sm leading-relaxed text-slate-600">
              {s.body.map((p, i) => (
                <p key={i}>{p}</p>
              ))}
            </div>
          </section>
        ))}
      </div>
    </main>
  );
}