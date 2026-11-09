"use client";

// The three-step lane under the hero: Build → Buyer pays → It keeps itself.
//
// It replaces the old "how it flows" block, which was three centred paragraphs
// with no visual relationship to the product. Here the numbers are the only
// decoration, and they are DIGITS (not the word "step"), so the band carries no
// English chrome in the other four locales — see the i18n rule in AGENTS.md.
import { useI18n } from "../i18n";

export default function StepsBand() {
  const { t } = useI18n();

  // Built from LITERAL keys rather than `t(step.title)` inside the map:
  // `src/lib/i18nKeys.test.ts` keeps a ceiling on call sites whose key the
  // static scanner cannot name, and a `t(variable)` would spend it. Same
  // strings, resolved one level earlier.
  const steps = [
    { title: t("land.path1t"), body: t("land.path1b") },
    { title: t("land.path2t"), body: t("land.path2b") },
    { title: t("land.path3t"), body: t("land.path3b") },
  ];

  return (
    // `id="how"` is the anchor the hero's secondary CTA points at.
    <div id="how" className="border-y border-[var(--jf-border)] bg-[var(--jf-surface)]">
      <ol className="mx-auto grid max-w-6xl grid-cols-1 gap-px bg-[var(--jf-border)] sm:grid-cols-3">
        {steps.map((step, i) => (
          <li key={step.title} className="bg-[var(--jf-surface)] px-6 py-7">
            <span className="font-mono text-jf-xs font-semibold tracking-widest text-[var(--jf-primary-strong)]">
              {String(i + 1).padStart(2, "0")}
            </span>
            <p className="mt-2 text-base font-semibold text-[var(--jf-text)]">{step.title}</p>
            <p className="mt-1 text-sm leading-relaxed text-[var(--jf-text-secondary)]">
              {step.body}
            </p>
          </li>
        ))}
      </ol>
    </div>
  );
}
