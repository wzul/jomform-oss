import type { Metadata } from "next";
import { TERMS_SECTIONS, TERMS_LAST_UPDATED } from "../terms-content";
import OperatorNotice from "../../../components/OperatorNotice";

export const metadata: Metadata = {
  title: "Terms of Service — JomForm",
  description: "Terms governing the use of JomForm.",
  alternates: { canonical: "https://jomform.com/legal/terms" },
  openGraph: {
    title: "Terms of Service — JomForm",
    description: "Terms governing the use of JomForm.",
    url: "https://jomform.com/legal/terms",
    siteName: "JomForm",
    type: "website",
    // Issue #411: brand og:image so sharing shows a preview.
    images: [{ url: "https://jomform.com/og-default.png", width: 1200, height: 630, alt: "JomForm" }],
  },
  twitter: {
    card: "summary_large_image",
    title: "Terms of Service — JomForm",
    description: "Terms governing the use of JomForm.",
    images: ["https://jomform.com/og-default.png"],
  },
};

export default function TermsPage() {
  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <h1 className="text-3xl font-bold">Terms of Service</h1>
      <p className="mt-2 text-sm text-slate-600">Last updated: {TERMS_LAST_UPDATED}</p>
      <div className="mt-8 space-y-8">
        {TERMS_SECTIONS.map((s) => (
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
      {/* Who the contract is with. Same shared component as the footer and
          /legal/privacy — the operator identity has exactly one definition. */}
      <footer className="mt-12 border-t border-slate-100 pt-6">
        <OperatorNotice className="text-sm leading-relaxed text-slate-600" />
      </footer>
    </main>
  );
}