"use client";

// Marketing landing page — i18n (EN default, BM toggle in-shell).
import { useEffect } from "react";
import { useI18n } from "../../components/i18n";
import OperatorNotice from "../../components/OperatorNotice";

const FEATURES = [
  { icon: "✏️", key: "land.f1" as const },
  { icon: "💳", key: "land.f2" as const },
  { icon: "🤖", key: "land.f3" as const },
  { icon: "📊", key: "land.f4" as const },
  { icon: "🤝", key: "land.f5" as const },
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
    /* `max-[17em]:[overflow-wrap:anywhere]` (card t_1aed910e): below 17em of
       viewport the page is at raised text (320px at a 24px root is 13.33em) and
       a word can be wider than the column it sits in. Measured on the built
       page with the phone's Tamil font (Noto Sans Tamil, AOSP's ta fallback)
       applied to the whole page at 320px / a 24px root: the QR-ordering card's
       `land.qrtitle` heading holds one unbreakable Tamil word 341px wide in a
       152px column, so the card's shrink-to-fit `div.text-center` column
       measured 341px (left -10.5, right 330.5) and the page panned 10.5px on its
       own, even with the hero lines fixed (the hero alone had made it 409).
       `overflow-wrap: anywhere` (not
       `break-word`) is what fixes it: `anywhere` is the only value whose break
       opportunities count in the min-content size, so the flex column stops
       being floored at the widest word and the word breaks inside itself.
       The band keeps the 100% render untouched: the same 320px viewport is
       20em at a 16px root, i.e. outside the band, so nothing re-breaks there
       (measured: 16px root, ta, Noto on the whole page — pan 0, and every hero
       box identical before and after this change). */
    <main className="min-h-screen bg-white max-[17em]:[overflow-wrap:anywhere]">
      {/* nav
          `flex-wrap` + `gap-y-2` (card t_68456ead): a nav row is three items —
          the wordmark, the language switch and Log in — and every one of them
          grows with the browser's default font size while the 320px viewport
          does not. At a 24px root the three needed ~317px of a 260px content
          box, so the row could not shrink (each item's min-content is its own
          text) and the Log in button was pushed 26.7px past the right edge,
          clipped on screen and dragging the whole page into a 64px sideways
          pan. Wrapping moves it to a second row instead of off the page; at
          100% text size the three items still fit on one row, so nothing
          changes there (measured: pan 0 at 16px, both before and after). */}
      <nav className="flex flex-wrap items-center justify-between gap-y-2 border-b border-slate-100 px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-jf-md font-semibold">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <div className="flex items-center gap-4">
          <a href="/builder" className="hidden text-sm text-slate-500 hover:text-slate-800 sm:block">
            {t("land.buildForm")}
          </a>
          <LangButtons />
        </div>
        {/* `whitespace-nowrap` (card t_68456ead): at a raised root font the
            label used to break to two lines inside the button ("Log" / "in"),
            because the button was the item the flex row squeezed last. The
            label is one unit; the row wraps instead of the words. */}
        <a href="/login" className="whitespace-nowrap rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90">
          {t("land.login")}
        </a>
      </nav>

      {/* hero */}
      <section className="mx-auto max-w-3xl px-6 pb-16 pt-20 text-center">
        <p className="mb-3 inline-block rounded-full border border-indigo-100 bg-indigo-50 px-3 py-1 text-xs font-medium text-[var(--jf-primary-on-tint)]">
          {t("land.badge")}
        </p>
        {/* `[overflow-wrap:anywhere]` on the h1 (card t_1aed910e): at a 24px
            root a Tamil hero line is wider than the hero column, and the
            document with it. Measured on the built page in ta with the phone's
            Tamil font (Noto Sans Tamil, Android's ta fallback — the host has no
            Noto, so the previous "ta pan 0" measurements had measured FreeSans)
            at 320px / a 24px root: `land.hero1` lays out as a 370.6px run with
            its right edge at 406.6 and `land.hero2` as a 372.6px run with its
            right edge at 408.6, in a 248px column — documentElement.scrollWidth
            409 vs clientWidth 320, window.scrollX 0 -> 89. At 360px it pans 61
            and at 390px 31 (the same 408.6 right edge).
            The guard is on the h1 — BOTH lines — and it is unconditional, not
            `rtl:`/banded. The earlier fix scoped it to RTL because that is the
            locale the previous card had measured; ta is LTR, so it kept
            `overflow-wrap: normal` and overflowed. `anywhere` only breaks a
            word that does not fit its line box, which is exactly the condition
            that pans, so it is inert where nothing overflows: at a 16px root no
            hero run exceeds its column in any of the five locales (measured:
            ta h1 min-content 249.7px in a 272px column, pan 0 in en/ms/zh/ta/ar
            at 320px, and the CTA geometry below is unchanged). */}
        <h1 className="text-4xl font-bold tracking-tight text-slate-900 [overflow-wrap:anywhere] sm:text-5xl">
          {t("land.hero1")}{" "}
          <span className="text-[var(--jf-primary)]">{t("land.hero2")}</span>
        </h1>
        {/* Same guard, same reason, on the subtitle (card t_1aed910e): in ta at
            a 24px root one run of `land.sub` is 385px wide at the 27px subtitle
            size, and an unbreakable run is laid out from the paragraph's start
            edge, so its right edge lands at 36 + 385 = 421 — the document
            measured scrollWidth 421 at BOTH 360 and 390 (pan 61 and 31), above
            the 352.8px (14.7em) band the previous fix used, which is why that
            band could not hold. Unconditional, like the h1: it breaks only a run
            that cannot fit, and at a 16px root that run is 260px inside a 272px
            column with the page measuring pan 0 in all five locales — so no line
            moves at 100% text. */}
        <p className="mx-auto mt-5 max-w-xl text-lg text-slate-500 [overflow-wrap:anywhere]">{t("land.sub")}</p>
        {/* hero CTAs — `max-[15.5em]:flex-wrap` (cards t_51990bdc, t_1aed910e):
            the two labels plus padding and the 0.75em gap are a fixed number of
            em wide, while the row sits in a 100vw - 3em content box, so below a
            threshold viewport width the pair cannot fit and `flex` without
            `flex-wrap` pushes the second button past the viewport edge.
            The threshold is measured on the WIDEST font the page meets, not on
            the host's: with the phone's Tamil font (Noto Sans Tamil) at a 24px
            root the ta pair is 201 + 152 + 18 gap = 371px = 15.46em, and at
            360px it measured left -5.5 / right 365.5 — a 5.5px pan. The pair
            pans exactly when it is wider than the viewport (it is centred, so
            it overflows both sides equally), so the band has to sit just above
            15.46em: 15.5em. At 390px the same pair fits (right edge 367.5 in
            390) and stays on one row, as measured. At a 16px root the same
            320px viewport is 20em, far outside the band, so the 100%-text
            one-row layout is untouched (measured: en 126+134, ms 112+148,
            ar 109+124). */}
        <div className="mt-8 flex items-center justify-center gap-3 max-[15.5em]:flex-wrap">
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
          <p className="mt-4 break-all rounded-xl bg-slate-900 p-4 font-mono text-jf-xs text-emerald-300">
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
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.weddingstep1")}</li>
                  <li>{t("land.weddingstep2")}</li>
                  <li>{t("land.weddingstep3")}</li>
                  <li>{t("land.weddingstep4")}</li>
                  <li>{t("land.weddingstep5")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/wedding-rsvp"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guidewedding")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — QR ordering / restaurant (issue #441) */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-emerald-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-emerald-100 text-4xl">
              🍽️
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.qrtitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.qrbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.qr1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.qr2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.qr3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.qr4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/builder"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.qrcta")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.qrstep1")}</li>
                  <li>{t("land.qrstep2")}</li>
                  <li>{t("land.qrstep3")}</li>
                  <li>{t("land.qrstep4")}</li>
                  <li>{t("land.qrstep5")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/qr-table-ordering"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guideqr")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — recurring class / tuition fees (card t_345b0c87).
          Every bullet is a behaviour that exists today, and this is the mapping:
            * "registration form: student, subject / level, session date & time"
              — two sources, because no shipped template carries a date or time
              block: `tpl-tuition` (Education) ships the registration form —
              student name, parent email/phone, subject/level and the
              registration-fee product — in its `blocks` JSON
              (api/internal/db/migrations/000027_template_library.up.sql:55-57).
              The session date & time is added in the builder from the palette's
              own `date`/`time` blocks (web/src/app/builder/page.tsx:145-146,
              PALETTE_GROUPS), painted by
              api/internal/httpapi/render_blocks.go `dateInputType` (:443).
            * "one-off registration fee, receipt emailed on payment" — a
              `one_time` product pays on submit and the buyer receipt is sent for
              it: sendReceiptAsync runs for every sale that is NOT a subscription
              (api/internal/payments/purchases.go:229).
            * "monthly class fee auto-charged to the saved card" — a product with
              type=subscription + billing_period month (api/internal/dbstore/product.go:25-26)
              saves the card at checkout (ForceRecurring for SaleTypeSubscription,
              purchases.go:113) and the hourly due-charge sweep charges that token,
              with the 3-attempt/24h retry ladder and cancel at day 7
              (api/internal/payments/recurring.go).
            * "subscriptions managed from the dashboard or MCP" — the
              /subscriptions page and the list_subscriptions / cancel_subscription /
              retry_subscription / update_subscription_card MCP tools
              (api/internal/mcpsrv/tools.go:122-134).
          Deliberately NOT written, because the code has no such feature: class
          slots with remaining seats, a real-time calendar, attendance, or tiered
          membership. The receipt is claimed for the one-off fee only — a recurring
          charge sends no buyer receipt (purchases.go, `cur.SaleType !=
          dbstore.SaleTypeSubscription`). The seat cap in the body is the existing
          entry limit (forms.entry_limit, api/internal/httpapi/restrictions.go:41). */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-sky-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-sky-100 text-4xl">
              🎓
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.tuitiontitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.tuitionbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.tuition1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.tuition2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.tuition3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.tuition4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/builder"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.tuitioncta")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.tuitionstep1")}</li>
                  <li>{t("land.tuitionstep2")}</li>
                  <li>{t("land.tuitionstep3")}</li>
                  <li>{t("land.tuitionstep4")}</li>
                  <li>{t("land.tuitionstep5")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/tuition-class"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guidetuition")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — Canva→Claude→MCP customization (issue #441) */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-violet-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-violet-100 text-4xl">
              🎨
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.canvatitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.canvabody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.canva1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.canva2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.canva3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.canva4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/mcp"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.canvacta")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.canvastep1")}</li>
                  <li>{t("land.canvastep2")}</li>
                  <li>{t("land.canvastep3")}</li>
                  <li>{t("land.canvastep4")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/design-with-mcp"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guidecanva")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — affiliate marketing (issue #450) */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-amber-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-amber-100 text-4xl">
              🤝
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.afftitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.affbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.aff1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.aff2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.aff3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.aff4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/settings"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.affcta")}
                </a>
                <a
                  href="/mcp"
                  className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-50"
                >
                  {t("land.affmcp")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.affstep1")}</li>
                  <li>{t("land.affstep2")}</li>
                  <li>{t("land.affstep3")}</li>
                  <li>{t("land.affstep4")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/affiliate-programme"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guideaff")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — club / society membership with recurring fees
          (card t_72d8fbac). Every bullet is a behaviour that exists today: the
          month|year billing period and the hourly due-charge sweep
          (api/internal/payments/recurring.go), the card token saved at
          checkout, the /subscriptions dashboard, and the subscription MCP
          tools. No receipt bullet — a subscription payment sends no buyer
          receipt (api/internal/payments/purchases.go skips it for
          SaleTypeSubscription). */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-teal-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-teal-100 text-4xl">
              🎟️
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.membershiptitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.membershipbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.membership1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.membership2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.membership3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.membership4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/builder"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.membershipcta")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.membershipstep1")}</li>
                  <li>{t("land.membershipstep2")}</li>
                  <li>{t("land.membershipstep3")}</li>
                  <li>{t("land.membershipstep4")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/club-membership"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guidemembership")}
                  </a>
                </p>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* use case showcase — collect donations (card t_41666fb1). The copy
          describes what SHIPS TODAY, led by the pay-what-you-want product
          (custom_amount, migration 000085_custom_amount_products): the donor
          types the amount on the form and the SERVER validates it against that
          product's min_price_sen (floored at 100 sen = RM1.00), max_price_sen
          and suggested_price_sen before it prices the sale
          (api/internal/httpapi/custom_amount.go). Fixed-denomination products
          — one product per RM10 / RM50 / RM100 — remain a supported
          ALTERNATIVE, which is what the third step offers.
          Donationstep2 starts from tpl-donation (migration 000027), the
          payment-mode form with its product block, name, email and message
          fields; a paid donation is a sale row, so /sales lists it with the
          donor's name and email. Deliberately absent, and negated by the guard
          in web/src/lib/landingUseCases.test.ts: a donor wall and a progress
          bar — no code renders either. If products.type moves again, that
          guard reads the migration and fails with the copy to re-read. */}
      <section className="mx-auto max-w-4xl border-t border-slate-100 px-6 py-16">
        <div className="mt-6 rounded-2xl border border-slate-200 bg-gradient-to-br from-pink-50 via-white to-white p-8">
          <div className="flex flex-col items-center gap-6 sm:flex-row sm:items-start">
            <div className="flex h-20 w-20 shrink-0 items-center justify-center rounded-2xl bg-pink-100 text-4xl">
              🤲
            </div>
            <div className="text-center sm:text-left">
              <h2 className="text-xl font-bold text-slate-900 sm:text-2xl">
                {t("land.donationtitle")}
              </h2>
              <p className="mt-3 max-w-xl text-sm leading-relaxed text-slate-600">
                {t("land.donationbody")}
              </p>
              <ul className="mt-5 grid grid-cols-1 gap-2 text-sm text-slate-600 sm:grid-cols-2">
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.donation1")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.donation2")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.donation3")}
                </li>
                <li className="flex items-center gap-2">
                  <span className="text-[var(--jf-primary)]">✓</span> {t("land.donation4")}
                </li>
              </ul>
              <div className="mt-6 flex flex-wrap items-center justify-center gap-3 sm:justify-start">
                <a
                  href="/builder"
                  className="rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
                >
                  {t("land.donationcta")}
                </a>
                <a
                  href="/mcp"
                  className="rounded-lg border border-slate-200 px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-50"
                >
                  {t("land.donationmcp")}
                </a>
              </div>
              {/* How to set it up — the real product paths, not marketing.
                  Every step names a control the shipped product has, and no
                  step claims a capability the code lacks. Every showcase has a
                  screenshot guide now, so the list ends on a link to it under
                  /docs/guides (src/lib/landingUseCases.test.ts checks each link
                  lands on a page file and that no card still promises one). */}
              <div className="mt-6 rounded-xl border border-slate-100 bg-slate-50 p-5 text-left">
                <p className="text-xs font-semibold uppercase tracking-widest text-slate-500">
                  {t("land.setup")}
                </p>
                <ol className="mt-3 list-decimal space-y-1.5 pl-5 text-sm leading-relaxed text-slate-600">
                  <li>{t("land.donationstep1")}</li>
                  <li>{t("land.donationstep2")}</li>
                  <li>{t("land.donationstep3")}</li>
                  <li>{t("land.donationstep4")}</li>
                  <li>{t("land.donationstep5")}</li>
                </ol>
                <p className="mt-3 text-xs">
                  <a
                    href="/docs/guides/donations"
                    className="font-medium text-[var(--jf-primary-strong)] underline decoration-dotted underline-offset-2 hover:opacity-90"
                  >
                    {t("land.guidedonation")}
                  </a>
                </p>
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
        {/* `flex-wrap` + `gap-y-1` (card t_68456ead): the five links are
            min-content ("Brand" cannot break at all) plus four gaps that scale
            with the root font; at a 24px root they need 394px inside a 248px
            footer, and a `nowrap` row SHRINKS its items, so the row overflowed
            on both sides of `justify-center` and dragged the page 64px sideways
            with it. At 100% text the same squeeze was already visible: the row
            is 272px wide and the five intact labels plus four 24px gaps need
            354px, so `nowrap` compressed "Privacy Policy" into a 49.7px box and
            "Terms of Service" into 60.5px, breaking each label onto two lines
            inside its own link. Wrapping gives every link its natural width on
            one line and puts the overflow on a second row, which is the honest
            shape at 320px: pan stays 0, the footer grows 4px (201 -> 205) for
            the row gap, and no label is broken. */}
        <div className="flex flex-wrap justify-center gap-x-6 gap-y-1 pb-4 text-xs text-slate-500">
          <a href="/legal/privacy" className="hover:underline">Privacy Policy</a>
          <a href="/legal/terms" className="hover:underline">Terms of Service</a>
          <a href="/docs" className="hover:underline">Docs</a>
          {/* t_bac29e18: the REST API is a product surface, so it is reachable
              from the public footer as well as the docs nav. */}
          <a href="/docs/api" className="hover:underline">API</a>
          <a href="/brand" className="hover:underline">Brand</a>
        </div>
        <p>
          {t("land.footer")}
        </p>
        {/* Who runs JomForm. The landing page is also the logged-out home page,
            so this is the home footer too. One shared component — see
            src/lib/operator.ts for the single source of the identifiers. */}
        <OperatorNotice className="mt-2" />
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