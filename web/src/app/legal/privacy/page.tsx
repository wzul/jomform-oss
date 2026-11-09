import type { Metadata } from "next";
import { PRIVACY_SECTIONS, PRIVACY_POLICY } from "../privacy-content";
import OperatorNotice from "../../../components/OperatorNotice";
import { Container, Section } from "../../../components/ui";
import { OG_IMAGE, OG_IMAGE_URL } from "../../../lib/ogImage";

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
    // Issue #411: brand og:image so sharing shows a preview. Content-versioned
    // URL (og-default-<hash>.png) so a client that cached an older card is
    // forced to the new file.
    images: [OG_IMAGE],
  },
  twitter: {
    card: "summary_large_image",
    title: "Privacy Policy — JomForm",
    description: "How JomForm collects, uses, and protects your data.",
    images: [OG_IMAGE_URL],
  },
};

export default function PrivacyPage() {
  // Card t_8f20418d: the page's own `max-w-3xl px-6 py-12` was a private copy of
  // the reading column and the vertical rhythm — `Container width="prose"` is
  // that measure and `Section spacing="lg"` is that 48px block. Nothing else
  // here changed: the prose, the section list and the operator notice are the
  // same nodes.
  return (
    <main>
      <Container width="prose">
        <Section spacing="lg">
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
          {/* The data controller. The privacy policy does not name the operator
              anywhere else, and this is the one place it is stated — rendered from
              the shared component so it cannot drift from the footer or /legal/terms. */}
          <footer className="mt-12 border-t border-slate-100 pt-6">
            <OperatorNotice className="text-sm leading-relaxed text-slate-600" />
          </footer>
        </Section>
      </Container>
    </main>
  );
}
