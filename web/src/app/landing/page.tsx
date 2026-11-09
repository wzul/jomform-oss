"use client";

// Marketing landing page — the logged-out home page — product-led layout.
//
// WHAT CHANGED AND WHY (card t_09f60795, Wan's decision: variant A + C's
// showcase warmth)
//   The old page was 786 lines of `mx-auto max-w-3xl … text-center`: a centred
//   text block for a hero, then seven use cases as full-width centred sections
//   repeating the same shape. It promised "beautiful order forms" and showed
//   none, the headline broke with "forms." hanging on its own line, and "Log in"
//   competed with the primary CTA. Measured page height: 7,261px.
//
//   Now: a two-column hero whose right column IS the product (`HeroOrderForm`,
//   a real order-form card on a dark stage), a three-step lane under it, the
//   seven use cases as cards in a three-column grid — each keeping every
//   "How to set it up" step and its guide link — the MCP band as a white card
//   with a short terminal transcript, then "Free. Not a trial." and the footer.
//
// THE MARKUP THIS FILE MUST KEEP, and which test pins each piece
//   `src/lib/landingHeroPan.test.ts` reads this file's source and anchors on
//   the FIRST section element: the hero. So the badge → h1 → subtitle → CTA row
//   order, the single h1 with its unconditional `[overflow-wrap:anywhere]`,
//   the coloured span holding `land.hero2`, and the CTA row's em-banded
//   `flex-wrap` are load-bearing, not stylistic. Same for the main element's
//   width band.
//   `src/lib/homeServerContent.test.ts` requires exactly ONE h1 in the file —
//   which is also why this comment does not spell one out in angle brackets:
//   the guard counts raw occurrences in the source, comments included.
//   `src/lib/landingRaisedText.test.ts` pins the nav's `flex-wrap`, the Log in
//   anchor's `whitespace-nowrap` and the footer row's `flex-wrap` + `gap-y-1`.
//   `src/lib/landingUseCases.test.ts` counts the seven `land.setup` headings and
//   the seven ordered lists (class `mt-3 list-decimal …`) IN THIS FILE, and
//   requires each showcase's guide link to sit between its last step and the
//   next section close — which is why the setup lists live here rather than
//   inside `ShowcaseCard`.
//
// Every colour is a `--jf-*` token and every space a Tailwind step (no hex, no
// `[Npx]` — design-system rule, card t_f2e231d3 will absorb these primitives).
// `--jf-primary-strong` is used for small accent text because `--jf-primary`
// measures 4.42:1 on `--jf-bg` (under AA for normal text); it is only used for
// large text and on white cards, where it measures 4.70:1 and up.
import { useEffect } from "react";

import { useI18n } from "../../components/i18n";
import OperatorNotice from "../../components/OperatorNotice";
import AgentBand from "../../components/landing/AgentBand";
import HeroOrderForm from "../../components/landing/HeroOrderForm";
import ShowcaseCard from "../../components/landing/ShowcaseCard";
import StepsBand from "../../components/landing/StepsBand";

/**
 * The five capability lines. They are the proof under "Free. Not a trial."
 * rather than a separate feature grid: each one is a shape that ships today,
 * and every one of them was already on this page before the redesign — the
 * redesign moved them, it did not invent a claim.
 */
const FEATURES = [
  { icon: "✏️", key: "land.f1" as const },
  { icon: "💳", key: "land.f2" as const },
  { icon: "🤖", key: "land.f3" as const },
  { icon: "📊", key: "land.f4" as const },
  { icon: "🤝", key: "land.f5" as const },
];

export default function Landing() {
  const { t, lang } = useI18n();

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  return (
    /* `max-[17em]:[overflow-wrap:anywhere]` (card t_1aed910e): below 17em of
       viewport the page is at raised text and a word can be wider than the
       column it sits in. Measured on the built page with the phone's Tamil font
       at 320px / a 24px root: the QR card's `land.qrtitle` heading held one
       unbreakable Tamil word 341px wide in a 152px column and panned the page
       10.5px. `anywhere` is the only value whose break opportunities count in
       the min-content size. The band keeps the 100% render untouched: 320px is
       20em at a 16px root, outside the band. */
    <main className="min-h-screen bg-[var(--jf-bg)] max-[17em]:[overflow-wrap:anywhere]">
      {/* nav
          `flex-wrap` + `gap-y-2` (card t_68456ead): three items whose widths
          are their own text; at a raised root font they no longer fit on one
          row and, without wrapping, Log in was pushed 26.7px past a 320px
          viewport. */}
      <nav className="flex flex-wrap items-center justify-between gap-y-2 border-b border-[var(--jf-border)] bg-[var(--jf-surface)] px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-jf-md font-semibold text-[var(--jf-text)]">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <div className="flex items-center gap-4">
          <a
            href="/builder"
            className="hidden text-sm text-[var(--jf-muted)] hover:text-[var(--jf-text)] sm:block"
          >
            {t("land.buildForm")}
          </a>
          <LangButtons />
        </div>
        {/* `whitespace-nowrap` (card t_68456ead): at a raised root font the
            label used to break to two lines inside the button; the row wraps
            instead of the words. */}
        <a
          href="/login"
          className="whitespace-nowrap rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          {t("land.login")}
        </a>
      </nav>

      {/* hero — the product IS the first screen (variant A) */}
      <section className="mx-auto max-w-6xl px-6 py-12 lg:py-20">
        <div className="grid grid-cols-1 items-center gap-10 lg:grid-cols-2 lg:gap-14">
          <div>
            <p className="inline-block rounded-full border border-[var(--jf-border)] bg-[var(--jf-surface)] px-3 py-1 text-xs font-medium text-[var(--jf-primary-strong)]">
              {t("land.badge")}
            </p>
            {/* The h1 carries an UNCONDITIONAL break guard for BOTH lines (card
                t_1aed910e): at 320px / a 24px root the ta hero lays out as a
                370.6px run (hero1) and a 372.6px run (hero2) in a 248px column
                — document scrollWidth 409 vs clientWidth 320. `anywhere` is
                inert where nothing overflows, so it costs the 100% render
                nothing. Never scope it by direction or width band: ta is LTR
                and pans above the old band. */}
            <h1 className="mt-4 text-4xl font-extrabold leading-[1.05] tracking-tight text-[var(--jf-text)] [overflow-wrap:anywhere] sm:text-5xl">
              {t("land.hero1")}{" "}
              <span className="text-[var(--jf-primary)]">{t("land.hero2")}</span>
            </h1>
            {/* Same guard on the subtitle, same measurement: in ta at a 24px
                root one run of `land.sub` is 385px wide and its right edge
                lands at 421 (pan 61 at 360px, 31 at 390px). */}
            <p className="mt-5 max-w-xl text-lg leading-relaxed text-[var(--jf-text-secondary)] [overflow-wrap:anywhere]">{t("land.sub")}</p>
            {/* `max-[15.5em]:flex-wrap` (cards t_51990bdc, t_1aed910e): the two
                labels plus padding and gap are 371px = 15.46em at a 24px root
                with the phone's Tamil font, so below 15.5em the pair cannot
                fit and an unwrapped row pushes the second button past the
                viewport edge. At a 16px root 320px is 20em — outside the band —
                so the 100%-text row stays on one line. */}
            <div className="mt-8 flex items-center justify-center gap-3 max-[15.5em]:flex-wrap sm:justify-start">
              <a href="/login" className="rounded-xl bg-[var(--jf-primary)] px-6 py-3 text-sm font-semibold text-white hover:opacity-90">
                {t("land.cta1")}
              </a>
              <a href="#how" className="rounded-xl border border-[var(--jf-border)] bg-[var(--jf-surface)] px-6 py-3 text-sm font-medium text-[var(--jf-text-secondary)] hover:bg-[var(--jf-bg)]">
                {t("land.cta2")}
              </a>
            </div>
            <ul className="mt-7 flex flex-wrap gap-x-5 gap-y-2 text-xs text-[var(--jf-muted)]">
              {[t("land.trust1"), t("land.trust2"), t("land.trust3")].map((label) => (
                <li key={label} className="flex items-center gap-1.5">
                  <span aria-hidden="true" className="text-[var(--jf-primary-strong)]">
                    ✓
                  </span>
                  {label}
                </li>
              ))}
            </ul>
          </div>

          {/* The right column is a real order form, not a picture of one. It is
              aria-hidden at the source: it is an illustration, and its controls
              are out of the tab order. */}
          <HeroOrderForm />
        </div>
      </section>

      {/* the three-step lane */}
      <StepsBand />

      {/* use-case showcase — seven cards in a three-column grid */}
      <section className="mx-auto max-w-6xl px-6 py-16">
        <div className="max-w-2xl">
          <p className="text-xs font-semibold uppercase tracking-widest text-[var(--jf-primary-strong)]">
            {t("land.usecasetitle")}
          </p>
          <h2 className="mt-2 text-2xl font-bold tracking-tight text-[var(--jf-text)] sm:text-3xl">
            {t("land.showcases")}
          </h2>
          <p className="mt-2 text-sm leading-relaxed text-[var(--jf-text-secondary)]">
            {t("land.showcasessub")}
          </p>
        </div>

        <div className="mt-8 grid grid-cols-1 items-start gap-5 md:grid-cols-2 lg:grid-cols-3">
          {/* use case showcase — wedding / RSVP (issue #381) */}
          <ShowcaseCard
            icon="💍"
            title={t("land.weddingtitle")}
            body={t("land.weddingbody")}
            bullets={[
              t("land.wedding1"),
              t("land.wedding2"),
              t("land.wedding3"),
              t("land.wedding4"),
            ]}
            cta={{ href: "/builder", label: t("land.weddingcta") }}
            secondary={{ href: "/mcp", label: t("land.weddingmcp") }}
          >
            {/* How to set it up — the real product paths, not marketing. Every
                step names a control the shipped product has, and no step claims
                a capability the code lacks. Every showcase has a screenshot
                guide now, so the list ends on a link to it under /docs/guides
                (src/lib/landingUseCases.test.ts checks each link lands on a
                page file and that no card still promises one). */}
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

          {/* use case showcase — QR ordering / restaurant (issue #441) */}
          <ShowcaseCard
            icon="🍽️"
            title={t("land.qrtitle")}
            body={t("land.qrbody")}
            bullets={[t("land.qr1"), t("land.qr2"), t("land.qr3"), t("land.qr4")]}
            cta={{ href: "/builder", label: t("land.qrcta") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

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
          <ShowcaseCard
            icon="🎓"
            title={t("land.tuitiontitle")}
            body={t("land.tuitionbody")}
            bullets={[
              t("land.tuition1"),
              t("land.tuition2"),
              t("land.tuition3"),
              t("land.tuition4"),
            ]}
            cta={{ href: "/builder", label: t("land.tuitioncta") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

          {/* use case showcase — Canva→Claude→MCP customization (issue #441) */}
          <ShowcaseCard
            icon="🎨"
            title={t("land.canvatitle")}
            body={t("land.canvabody")}
            bullets={[t("land.canva1"), t("land.canva2"), t("land.canva3"), t("land.canva4")]}
            cta={{ href: "/mcp", label: t("land.canvacta") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

          {/* use case showcase — affiliate marketing (issue #450) */}
          <ShowcaseCard
            icon="🤝"
            title={t("land.afftitle")}
            body={t("land.affbody")}
            bullets={[t("land.aff1"), t("land.aff2"), t("land.aff3"), t("land.aff4")]}
            cta={{ href: "/settings", label: t("land.affcta") }}
            secondary={{ href: "/mcp", label: t("land.affmcp") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

          {/* use case showcase — club / society membership with recurring fees
              (card t_72d8fbac). Every bullet is a behaviour that exists today: the
              month|year billing period and the hourly due-charge sweep
              (api/internal/payments/recurring.go), the card token saved at
              checkout, the /subscriptions dashboard, and the subscription MCP
              tools. No receipt bullet — a subscription payment sends no buyer
              receipt (api/internal/payments/purchases.go skips it for
              SaleTypeSubscription). */}
          <ShowcaseCard
            icon="🎟️"
            title={t("land.membershiptitle")}
            body={t("land.membershipbody")}
            bullets={[
              t("land.membership1"),
              t("land.membership2"),
              t("land.membership3"),
              t("land.membership4"),
            ]}
            cta={{ href: "/builder", label: t("land.membershipcta") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>

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
          <ShowcaseCard
            icon="🤲"
            title={t("land.donationtitle")}
            body={t("land.donationbody")}
            bullets={[
              t("land.donation1"),
              t("land.donation2"),
              t("land.donation3"),
              t("land.donation4"),
            ]}
            cta={{ href: "/builder", label: t("land.donationcta") }}
            secondary={{ href: "/mcp", label: t("land.donationmcp") }}
          >
            <div className="mt-4 rounded-xl border border-[var(--jf-border)] bg-[var(--jf-bg)] p-4">
              <p className="text-jf-2xs font-semibold uppercase tracking-widest text-[var(--jf-muted)]">
                {t("land.setup")}
              </p>
              <ol className="mt-3 list-decimal space-y-1 pl-4 text-xs leading-relaxed text-[var(--jf-text-secondary)]">
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
          </ShowcaseCard>
        </div>
      </section>

      {/* MCP band — a white card with a short terminal transcript */}
      <AgentBand>
        <a href="/mcp" className="mt-5 inline-block rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-medium text-white hover:opacity-90">
          {t("land.mcpguide")}
        </a>
      </AgentBand>

      {/* pricing — "Free. Not a trial." */}
      <section className="mx-auto max-w-6xl px-6 pb-16">
        <div className="grid gap-8 rounded-3xl border border-[var(--jf-border)] bg-[var(--jf-surface)] p-6 sm:p-10 lg:grid-cols-2 lg:items-start">
          <div>
            <h2 className="text-3xl font-extrabold tracking-tight text-[var(--jf-text)] sm:text-4xl">
              {t("land.pricetitle")}
            </h2>
            <p className="mt-3 max-w-md text-sm leading-relaxed text-[var(--jf-text-secondary)]">
              {t("land.pricebody")}
            </p>
          </div>
          <ul className="space-y-3">
            {FEATURES.map((f) => (
              <li key={f.key} className="flex gap-3">
                <span aria-hidden="true" className="text-base">
                  {f.icon}
                </span>
                <span className="text-sm leading-relaxed text-[var(--jf-text-secondary)]">
                  {t(f.key)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      </section>

      {/* footer */}
      <footer className="border-t border-[var(--jf-border)] bg-[var(--jf-surface)] px-6 py-8 text-center text-xs">
        {/* `flex-wrap` + `gap-y-1` (card t_68456ead): the five labels are
            min-content and the gaps scale with the root font, so a `nowrap` row
            overflowed BOTH edges of `justify-center` at a raised root font — and
            at 100% text it squeezed "Privacy Policy" into a 49.7px box. */}
        <div className="flex flex-wrap justify-center gap-x-6 gap-y-1 pb-4 text-xs text-[var(--jf-muted)]">
          <a href="/legal/privacy" className="hover:underline">
            Privacy Policy
          </a>
          <a href="/legal/terms" className="hover:underline">
            Terms of Service
          </a>
          <a href="/docs" className="hover:underline">
            Docs
          </a>
          {/* t_bac29e18: the REST API is a product surface, so it is reachable
              from the public footer as well as the docs nav. */}
          <a href="/docs/api" className="hover:underline">
            API
          </a>
          <a href="/brand" className="hover:underline">
            Brand
          </a>
        </div>
        <p className="text-[var(--jf-muted)]">{t("land.footer")}</p>
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
    <div className="flex overflow-hidden rounded-md border border-[var(--jf-border)] text-xs">
      {(["en", "ms"] as const).map((l) => (
        <button
          key={l}
          onClick={() => setLang(l)}
          className={`px-2 py-1 font-medium transition-colors ${
            lang === l
              ? "bg-[var(--jf-primary)] text-white"
              : "bg-[var(--jf-surface)] text-[var(--jf-muted)]"
          }`}
        >
          {l === "en" ? "EN" : "BM"}
        </button>
      ))}
    </div>
  );
}
