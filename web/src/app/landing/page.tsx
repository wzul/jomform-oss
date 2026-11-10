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
//   seven use cases as cards in a three-column grid — each with its title, body,
//   tick bullets and ONE plain line pointing at its screenshot guide — the MCP band
//   as a white card with a short terminal transcript, then "Free. Not a trial."
//   and the footer.
//
// NO "HOW TO SET IT UP" LISTS ON THIS PAGE (card t_b4e5881d, reversing t_41666fb1)
//   Each showcase used to carry a nested setup box: a heading, a four/five-item
//   ordered list and a guide link. Wan, on a phone screenshot of the RSVP card:
//   "benda yang terlalu technical tak sesuai ada kat landing page." The steps are
//   still written down — in docs/guides/<slug>/README.md, rendered at
//   /docs/guides/<slug> — so the card keeps a single guide line and the landing
//   says what the product does rather than how to configure it.
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
//   `src/lib/landingUseCases.test.ts` requires, per showcase, its title/body/
//   bullets/cta rendered once, ONE guide line inside the card's own slice of this
//   file, and ZERO setup lists — and it checks each linked guide still carries
//   numbered steps, so removing the steps here cannot mean losing them from the
//   product.
//
// Every colour is a `--jf-*` token and every space a Tailwind step (no hex, no
// `[Npx]` — design-system rule, card t_f2e231d3 will absorb these primitives).
// `--jf-primary-strong` is used for small accent text because `--jf-primary`
// measures 4.42:1 on `--jf-bg` (under AA for normal text); it is only used for
// large text and on white cards, where it measures 4.70:1 and up.
import { useEffect } from "react";

import { LANG_LABEL, LANG_SHORT, useI18n } from "../../components/i18n";
import { endonymLangProps } from "../../lib/endonymLang";
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
          `flex-wrap` + `gap-y-2` (card t_68456ead): the brand and the link
          cluster are each as wide as their own text; at a raised root font they
          no longer fit on one row and, without wrapping, the last item was
          pushed 26.7px past a 320px viewport.

          HEADER HIERARCHY — DO NOT INVERT (card t_53bcd451, measured from the
          shipped 390px and 1440px screenshots):
            A public landing page is read by a FIRST-TIME visitor, so the
            strongest control in this header must be the one that starts them
            building: "Build a form" is the filled primary and points at
            /builder (for a visitor with no session cookie the SSR gate turns
            that into /login?next=…builder, so the intent survives sign-in).
            "Log in" is the returning user's shortcut — it stays a plain text
            link and must never carry the fill. The old header had it the other
            way round: a filled purple Log in beside a plain Build a form, so a
            new visitor's eye landed on an account form.
            `src/lib/landingHeaderHierarchy.test.ts` fails the unit suite if the
            fill moves back onto /login, if /builder loses it, if the Docs link
            leaves the header, if the language buttons drop under 44px, or if
            the trust bullets go back to 12px muted text. */}
      <nav className="flex flex-wrap items-center justify-between gap-y-2 border-b border-[var(--jf-border)] bg-[var(--jf-surface)] px-5 py-3">
        <div className="flex items-center gap-2">
          <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--jf-primary)] text-sm font-bold text-white">
            JF
          </div>
          <span className="text-jf-md font-semibold text-[var(--jf-text)]">
            Jom<span className="text-[var(--jf-primary)]">Form</span>
          </span>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-1">
          {/* /docs is a full product surface, so it is reachable from the
              header and not only from the footer. */}
          <a
            href="/docs"
            className="whitespace-nowrap text-sm text-[var(--jf-text-secondary)] hover:text-[var(--jf-text)]"
          >
            Docs
          </a>
          {/* `whitespace-nowrap` (card t_68456ead): at a raised root font the
              label used to break to two lines inside its link; the row wraps
              instead of the words. Text link, NOT the filled button — see the
              hierarchy note above. */}
          <a
            href="/login"
            className="whitespace-nowrap text-sm text-[var(--jf-muted)] hover:text-[var(--jf-text)]"
          >
            {t("land.login")}
          </a>
          <a
            href="/builder"
            className="whitespace-nowrap rounded-lg bg-[var(--jf-primary)] px-4 py-2 text-sm font-semibold text-white hover:opacity-90"
          >
            {t("land.buildForm")}
          </a>
          <LangButtons />
        </div>
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
            {/* Trust bullets (card t_53bcd451). They were `text-xs` in
                --jf-muted: the reassurance was there but visually discarded.
                Now `text-sm` in --jf-text-secondary, which measures 9.69:1 on
                this hero surface (--jf-bg #f7f8f8) — comfortably past the 4.5:1
                AA floor for normal text, where --jf-muted measured 4.54:1 at
                12px. Colour is a token, so the a11y gate's axe scan measures
                the same pair on every run. */}
            <ul className="mt-7 flex flex-wrap gap-x-5 gap-y-2 text-sm text-[var(--jf-text-secondary)]">
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
            guide={{ href: "/docs/guides/wedding-rsvp", label: t("land.guidewedding") }}
          />

          {/* use case showcase — QR ordering / restaurant (issue #441) */}
          <ShowcaseCard
            icon="🍽️"
            title={t("land.qrtitle")}
            body={t("land.qrbody")}
            bullets={[t("land.qr1"), t("land.qr2"), t("land.qr3"), t("land.qr4")]}
            cta={{ href: "/builder", label: t("land.qrcta") }}
            guide={{ href: "/docs/guides/qr-table-ordering", label: t("land.guideqr") }}
          />

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
            guide={{ href: "/docs/guides/tuition-class", label: t("land.guidetuition") }}
          />

          {/* use case showcase — Canva→Claude→MCP customization (issue #441) */}
          <ShowcaseCard
            icon="🎨"
            title={t("land.canvatitle")}
            body={t("land.canvabody")}
            bullets={[t("land.canva1"), t("land.canva2"), t("land.canva3"), t("land.canva4")]}
            cta={{ href: "/mcp", label: t("land.canvacta") }}
            guide={{ href: "/docs/guides/design-with-mcp", label: t("land.guidecanva") }}
          />

          {/* use case showcase — affiliate marketing (issue #450) */}
          <ShowcaseCard
            icon="🤝"
            title={t("land.afftitle")}
            body={t("land.affbody")}
            bullets={[t("land.aff1"), t("land.aff2"), t("land.aff3"), t("land.aff4")]}
            cta={{ href: "/settings", label: t("land.affcta") }}
            secondary={{ href: "/mcp", label: t("land.affmcp") }}
            guide={{ href: "/docs/guides/affiliate-programme", label: t("land.guideaff") }}
          />

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
            guide={{ href: "/docs/guides/club-membership", label: t("land.guidemembership") }}
          />

          {/* use case showcase — collect donations (card t_41666fb1). The copy
              describes what SHIPS TODAY, led by the pay-what-you-want product
              (custom_amount, migration 000085_custom_amount_products): the donor
              types the amount on the form and the SERVER validates it against that
              product's min_price_sen (floored at 100 sen = RM1.00), max_price_sen
              and suggested_price_sen before it prices the sale
              (api/internal/httpapi/custom_amount.go). Fixed-denomination products
              — one product per RM10 / RM50 / RM100 — remain a supported
              ALTERNATIVE, which is what docs/guides/donations/README.md walks through
              (card t_b4e5881d took the steps off this page; the guide keeps them).
              That guide also starts from tpl-donation (migration 000027), the
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
            guide={{ href: "/docs/guides/donations", label: t("land.guidedonation") }}
          />
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

/**
 * The landing's language control.
 *
 * Card t_53bcd451: it measured roughly 20px tall — under the 24px AA pointer
 * minimum and far under the 44px touch target — and each button's only name was
 * the two-letter label ("EN" / "BM"), which names neither the language nor the
 * target of the press.
 *
 * The HIT AREA is 44px (`min-h-11 min-w-11`) and the VISUAL pill inside it keeps
 * the original size: the padding around the label is what grows, so the header
 * keeps its density while every pointer, thumb included, gets a real target.
 * `aria-pressed` carries the segmented control's state, and the accessible name
 * puts the language's own name in (`LANG_LABEL`) while keeping the visible short
 * label first, so it contains the text it shows (WCAG 2.5.3) as well as naming
 * the language it switches to.
 */
function LangButtons() {
  const { lang, setLang } = useI18n();
  return (
    <div className="flex shrink-0 items-center overflow-hidden rounded-md border border-[var(--jf-border)]">
      {(["en", "ms"] as const).map((l) => (
        <button
          key={l}
          type="button"
          /* The endonym appears TWICE in this element — in the accessible name
             below and in the visible label — and `endonymLang.test.ts` inspects
             the element that holds each occurrence, requiring the declaration
             BEFORE it. It is empty today (EN/BM are Latin abbreviations, the
             same script as the page) and it is what keeps a non-Latin locale
             from shipping undeclared (WCAG 3.1.2). */
          {...endonymLangProps(l)}
          onClick={() => setLang(l)}
          aria-pressed={lang === l}
          aria-label={`${LANG_SHORT[l]} — ${LANG_LABEL[l]}`}
          title={LANG_LABEL[l]}
          className="flex min-h-11 min-w-11 items-center justify-center px-1 text-xs"
        >
          <span
            {...endonymLangProps(l)}
            className={`rounded px-2 py-0.5 font-medium transition-colors ${
              lang === l
                ? "bg-[var(--jf-primary)] text-white"
                : "text-[var(--jf-muted)]"
            }`}
          >
            {LANG_SHORT[l]}
          </span>
        </button>
      ))}
    </div>
  );
}
