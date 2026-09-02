import { TERMS_SECTIONS, TERMS_LAST_UPDATED } from "../terms-content";

export const metadata = {
  title: "Terms of Service — JomForm",
  description: "Terms governing the use of JomForm.",
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
    </main>
  );
}