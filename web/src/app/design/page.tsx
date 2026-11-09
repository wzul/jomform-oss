import type { Metadata } from "next";

import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  CodeInput,
  Container,
  EmptyState,
  Field,
  Input,
  Section,
  Select,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableEmpty,
  TableHead,
  TableHeaderCell,
  TableRow,
  Tabs,
  Textarea,
} from "../../components/ui";
import { DARK_CONTRASTS, LIGHT_CONTRASTS, type ContrastPair } from "../../lib/designTokenRatios";
import { RADIUS_SCALE, SHADOW_SCALE, SPACE_SCALE, TYPE_SCALE } from "../../lib/designTokenScale";

// /design — the design system's own page (card t_f2e231d3).
//
// WHAT IT IS FOR. Two readers: a human deciding whether a control looks right, and
// an AGENT that needs to know what exists before it invents a fourth kind of card.
// For the second reader the page is a manifest: every primitive in the layer, every
// state it can be in, every colour token with its measured contrast, and the four
// scales. `src/lib/designTokens.test.ts` fails when a primitive exists in
// `components/ui/` but is not shown here, so the manifest cannot fall behind the
// layer.
//
// NOT INDEXED: this is an internal reference, not a product page. `robots` says so
// and the sitemap never listed it.
//
// WHY SOME STATES ARE "SHOWN" RATHER THAN HOVERED. A server-rendered page cannot
// hold a pointer over a control, and a picture of a hover state is a picture of
// yesterday's token. The specimens labelled *hover*/*focus* paint the SAME token
// class the component applies in that state, next to the live control — so the
// comparison is available at a glance and the live control still answers a real
// pointer and a real Tab key. The measured evidence for those pairings is the
// contrast table below, not this page's own appearance.

export const metadata: Metadata = {
  title: "Design system — JomForm",
  description: "The tokens, primitives and states every JomForm surface is built from.",
  robots: { index: false },
};

const NAV = [
  ["#type", "Type scale"],
  ["#space", "Space"],
  ["#radius", "Radius"],
  ["#elevation", "Elevation"],
  ["#colour", "Colour & contrast"],
  ["#button", "Button"],
  ["#card", "Card"],
  ["#badge", "Badge"],
  ["#fields", "Input, Textarea, Select, Checkbox, Field"],
  ["#alert", "Alert"],
  ["#table", "Table & EmptyState"],
  ["#tabs", "Tabs"],
  ["#skeleton", "Skeleton"],
] as const;

function SectionHeading({ id, title, children }: { id: string; title: string; children?: React.ReactNode }) {
  return (
    <div className="mb-jf-4 border-b border-[var(--jf-border)] pb-jf-2">
      <h2 id={id} className="text-jf-read-xl font-semibold text-[var(--jf-text)]">
        {title}
      </h2>
      {children ? <p className="mt-jf-1 text-jf-read-sm text-[var(--jf-muted)]">{children}</p> : null}
    </div>
  );
}

function SubHeading({ id, title }: { id: string; title: string }) {
  return (
    <h3 id={id} className="mb-jf-2 text-jf-read-md font-semibold text-[var(--jf-text)]">
      {title}
    </h3>
  );
}

/** A state specimen: the same token classes the control applies in that state. */
function Specimen({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col items-start gap-jf-2">
      {children}
      <p className="text-jf-read-xs text-[var(--jf-muted)]">{label}</p>
    </div>
  );
}

function ContrastTable({ rows, caption }: { rows: readonly ContrastPair[]; caption: string }) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <caption className="pb-jf-2 text-left text-jf-read-sm text-[var(--jf-muted)]">{caption}</caption>
        <TableHead>
          <TableRow>
            <TableHeaderCell>Pair</TableHeaderCell>
            <TableHeaderCell>Role</TableHeaderCell>
            <TableHeaderCell className="text-right">Ratio</TableHeaderCell>
            <TableHeaderCell className="text-right">Floor</TableHeaderCell>
            <TableHeaderCell>Result</TableHeaderCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => {
            const ok = row.ratio >= row.min;
            // The swatch shows SAMPLE TEXT only where the pairing is about text AND
            // there is headroom over the floor. Two reasons, both measured: a pair
            // with a 3.0 floor is a graphic/boundary pairing (a "Aa" in it fails the
            // text floor, and axe names it — 3 nodes on the first run of this page),
            // and axe rounds its own ratio to 2dp and read 4.41 where the same pair
            // computes 4.42 here, so a sample sitting at 4.51 could fail the gate on
            // a rounding difference. Below the margin the swatch is a graphic mark.
            const showsText = row.min >= 4.5 && row.ratio >= 4.6;
            return (
              <TableRow key={`${row.fg}-${row.bg}`}>
                <TableCell>
                  <span className="flex items-center gap-jf-2">
                    <span
                      aria-hidden="true"
                      className="inline-flex h-jf-8 w-jf-12 items-center justify-center rounded-jf-sm border border-[var(--jf-border)] text-jf-read-sm"
                      style={{ color: `var(${row.fg})`, backgroundColor: `var(${row.bg})` }}
                    >
                      {showsText ? (
                        "Aa"
                      ) : (
                        <span
                          className="block h-jf-3 w-jf-6 rounded-jf-sm"
                          style={{ backgroundColor: `var(${row.fg})` }}
                        />
                      )}
                    </span>
                    <span className="font-mono text-jf-read-xs text-[var(--jf-text-secondary)]">
                      {row.fg} / {row.bg}
                    </span>
                  </span>
                </TableCell>
                <TableCell className="text-[var(--jf-text-secondary)]">{row.role}</TableCell>
                <TableCell className="text-right font-mono">{row.ratio.toFixed(2)}</TableCell>
                <TableCell className="text-right font-mono text-[var(--jf-muted)]">{row.min.toFixed(1)}</TableCell>
                <TableCell>
                  <Badge tone={ok ? "success" : "danger"}>{ok ? "pass" : "fail"}</Badge>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

export default function DesignSystemPage() {
  return (
    <main className="min-h-screen bg-[var(--jf-bg)] text-[var(--jf-text)]">
      <Container>
        <Section spacing="lg" aria-labelledby="design-title">
          <p className="text-jf-read-xs font-semibold uppercase tracking-wide text-[var(--jf-muted)]">
            Internal reference · not indexed
          </p>
          <h1 id="design-title" className="mt-jf-2 text-jf-read-3xl font-semibold">
            JomForm design system
          </h1>
          <p className="mt-jf-2 max-w-3xl text-jf-read-md text-[var(--jf-text-secondary)]">
            Every value on this page comes from the <code>--jf-*</code> tokens in{" "}
            <code>app/globals.css</code>. The primitives below live in{" "}
            <code>components/ui/</code>; a page that needs a control, a card or a field builds it from one of them
            rather than styling it again. Contrast ratios are recomputed from the stylesheet by{" "}
            <code>src/lib/designTokens.test.ts</code> on every test run, so a number here cannot describe a colour
            the app no longer paints.
          </p>
          <nav aria-label="Sections" className="mt-jf-4">
            <ul className="flex flex-wrap gap-x-jf-4 gap-y-jf-2 text-jf-read-sm">
              {NAV.map(([href, label]) => (
                <li key={href}>
                  <a className="text-[var(--jf-primary-strong)] underline-offset-2 hover:underline" href={href}>
                    {label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
        </Section>

        {/* ── type scale ─────────────────────────────────────────────────── */}
        <Section aria-labelledby="type">
          <SectionHeading id="type" title="Type scale">
            Seven steps, each with its paired line-height. Set ONE utility and the line box follows.
          </SectionHeading>
          <div className="overflow-x-auto">
            <Table>
              <TableHead>
                <TableRow>
                  <TableHeaderCell>Token</TableHeaderCell>
                  <TableHeaderCell>Size / line-height</TableHeaderCell>
                  <TableHeaderCell>Utility</TableHeaderCell>
                  <TableHeaderCell>Sample</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {TYPE_SCALE.map((step) => (
                  <TableRow key={step.token}>
                    <TableCell className="font-mono text-jf-read-xs">{step.token}</TableCell>
                    <TableCell className="font-mono text-jf-read-xs">
                      {step.px}px / {step.lineHeightPx}px
                    </TableCell>
                    <TableCell className="font-mono text-jf-read-xs">{step.utility}</TableCell>
                    <TableCell>
                      <span className={`${step.utility} block text-[var(--jf-text)]`}>
                        The quick brown fox jumps over the lazy dog
                      </span>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </Section>

        {/* ── space ──────────────────────────────────────────────────────── */}
        <Section aria-labelledby="space">
          <SectionHeading id="space" title="Space">
            A 4px grid: step N is N x 4px, used for padding, gap and margin.
          </SectionHeading>
          <div className="flex flex-col gap-jf-2">
            {SPACE_SCALE.map((step) => (
              <div key={step.token} className="flex flex-col gap-jf-1">
                <span className="font-mono text-jf-read-xs text-[var(--jf-text-secondary)]">
                  {step.token} · {step.px}px
                </span>
                <span
                  aria-hidden="true"
                  className={`${step.utility} block h-jf-2 rounded-jf-sm bg-[var(--jf-primary)]`}
                />
              </div>
            ))}
          </div>
        </Section>

        {/* ── radius ─────────────────────────────────────────────────────── */}
        <Section aria-labelledby="radius">
          <SectionHeading id="radius" title="Radius">
            Five steps. Controls are <code>md</code>; cards are <code>lg</code>; pills are <code>full</code>.
          </SectionHeading>
          <div className="flex flex-wrap gap-jf-4">
            {RADIUS_SCALE.map((step) => (
              <Specimen key={step.token} label={`${step.token} (${step.px === 9999 ? "pill" : `${step.px}px`})`}>
                <span
                  aria-hidden="true"
                  className={`${step.utility} block h-jf-12 w-jf-12 border border-[var(--jf-border-strong)] bg-[var(--jf-surface-2)]`}
                />
              </Specimen>
            ))}
          </div>
        </Section>

        {/* ── elevation ──────────────────────────────────────────────────── */}
        <Section aria-labelledby="elevation">
          <SectionHeading id="elevation" title="Elevation">
            Three shadow steps: a resting card, a raised panel, a dialog.
          </SectionHeading>
          <div className="flex flex-wrap gap-jf-6">
            {SHADOW_SCALE.map((step) => (
              <Specimen key={step.token} label={`${step.token} · ${step.utility}`}>
                <span
                  aria-hidden="true"
                  className={`${step.utility} block h-jf-12 w-jf-12 rounded-jf-lg border border-[var(--jf-border)] bg-[var(--jf-surface)]`}
                />
              </Specimen>
            ))}
          </div>
        </Section>

        {/* ── colour ─────────────────────────────────────────────────────── */}
        <Section aria-labelledby="colour">
          <SectionHeading id="colour" title="Colour & contrast">
            Every colour token, with the pairing it exists for and the ratio that pairing measures. The floor is
            WCAG 2.2 AA (4.5:1 for text, 3:1 for a graphical object); a row at 1.0 is a tint, a divider or a
            gridline and carries no text.
          </SectionHeading>

          <SubHeading id="colour-light" title="Light theme (`:root`)" />
          <ContrastTable rows={LIGHT_CONTRASTS} caption="Light palette — measured against --jf-surface." />

          <div className="mt-jf-10">
            <SubHeading id="colour-dark" title="Dark theme (`[data-theme=&quot;dark&quot;]`)" />
            <p className="mb-jf-3 max-w-3xl text-jf-read-sm text-[var(--jf-muted)]">
              The dark palette is OPT-IN, not a <code>prefers-color-scheme</code> override: nothing in the product
              sets the attribute today, so no visitor&apos;s page changes. The island below is a real{" "}
              <code>data-theme=&quot;dark&quot;</code> subtree — every primitive inside it is reading the same
              token names, which is the property a future dark theme depends on.
            </p>
            <ContrastTable rows={DARK_CONTRASTS} caption="Dark palette — measured against the dark --jf-surface." />

            <div
              data-theme="dark"
              className="mt-jf-6 rounded-jf-lg border border-[var(--jf-border)] bg-[var(--jf-bg)] p-jf-6 text-[var(--jf-text)]"
            >
              <div className="flex flex-wrap items-center gap-jf-4">
                <Button>Primary</Button>
                <Button variant="secondary">Secondary</Button>
                <Button variant="ghost">Ghost</Button>
                <Button variant="danger">Danger</Button>
                <Badge tone="success">paid</Badge>
                <Badge tone="warning">pending</Badge>
              </div>
              <Card className="mt-jf-4 p-jf-4">
                <p className="text-jf-read-sm text-[var(--jf-text)]">
                  A card inside the dark island: <span className="text-[var(--jf-muted)]">muted copy</span>,{" "}
                  <span className="text-[var(--jf-primary-strong)]">the indigo text step</span>, and{" "}
                  <code className="rounded-jf-sm bg-[var(--jf-code-bg)] px-jf-1">inline code</code>.
                </p>
                <div className="mt-jf-3">
                  <Alert tone="info">Informational message on a dark surface.</Alert>
                </div>
              </Card>
            </div>
          </div>
        </Section>

        {/* ── Button ─────────────────────────────────────────────────────── */}
        <Section aria-labelledby="button">
          <SectionHeading id="button" title="Button">
            Four variants, three sizes, and the six states a control can be in. An anchor that navigates is{" "}
            <code>as=&quot;a&quot;</code>, never a button with an <code>onClick</code>.
          </SectionHeading>

          <SubHeading id="button-variants" title="Variants" />
          <div className="flex flex-wrap items-center gap-jf-3">
            <Button>Primary</Button>
            <Button variant="secondary">Secondary</Button>
            <Button variant="ghost">Ghost</Button>
            <Button variant="danger">Danger</Button>
            <Button as="a" href="#button">
              As a link
            </Button>
          </div>

          <div className="mt-jf-6">
            <SubHeading id="button-sizes" title="Sizes" />
            <div className="flex flex-wrap items-center gap-jf-3">
              <Button size="sm">Small</Button>
              <Button size="md">Medium</Button>
              <Button size="lg">Large</Button>
              <Button variant="secondary" size="sm">
                Small
              </Button>
              <Button variant="secondary" size="md">
                Medium
              </Button>
              <Button variant="secondary" size="lg">
                Large
              </Button>
            </div>
          </div>

          <div className="mt-jf-6">
            <SubHeading id="button-states" title="States" />
            <div className="flex flex-wrap items-start gap-jf-6">
              <Specimen label="default">
                <Button>Save changes</Button>
              </Specimen>
              <Specimen label="hover (same token the control uses)">
                <span className="inline-flex min-h-jf-10 items-center justify-center rounded-jf-md bg-[var(--jf-primary-hover)] px-jf-4 py-jf-2 text-jf-read-sm font-medium text-[var(--jf-on-color)]">
                  Save changes
                </span>
              </Specimen>
              <Specimen label="focus-visible (same token as the global ring)">
                <span className="inline-flex min-h-jf-10 items-center justify-center rounded-jf-md bg-[var(--jf-primary)] px-jf-4 py-jf-2 text-jf-read-sm font-medium text-[var(--jf-on-color)] outline-2 outline-offset-2 outline-[var(--jf-focus-ring)]">
                  Save changes
                </span>
              </Specimen>
              <Specimen label="pressed">
                <span className="inline-flex min-h-jf-10 items-center justify-center rounded-jf-md bg-[var(--jf-primary-active)] px-jf-4 py-jf-2 text-jf-read-sm font-medium text-[var(--jf-on-color)]">
                  Save changes
                </span>
              </Specimen>
              <Specimen label="disabled">
                <Button disabled>Save changes</Button>
              </Specimen>
              <Specimen label="loading (aria-busy, not pressable)">
                <Button loading>Save changes</Button>
              </Specimen>
            </div>
          </div>
        </Section>

        {/* ── Card ───────────────────────────────────────────────────────── */}
        <Section aria-labelledby="card">
          <SectionHeading id="card" title="Card">
            The one surface. <code>flat</code> for a card inside a card, <code>raised</code> at rest,{" "}
            <code>floating</code> for a dialog; <code>interactive</code> when the whole card is the target.
          </SectionHeading>
          <div className="grid gap-jf-4 sm:grid-cols-3">
            <Card elevation="flat" className="p-jf-4">
              <p className="text-jf-read-sm font-medium">flat</p>
              <p className="text-jf-read-xs text-[var(--jf-muted)]">A nested card.</p>
            </Card>
            <Card elevation="raised" className="p-jf-4">
              <p className="text-jf-read-sm font-medium">raised</p>
              <p className="text-jf-read-xs text-[var(--jf-muted)]">The resting card.</p>
            </Card>
            <Card elevation="floating" className="p-jf-4">
              <p className="text-jf-read-sm font-medium">floating</p>
              <p className="text-jf-read-xs text-[var(--jf-muted)]">A dialog or a menu.</p>
            </Card>
            <Card interactive className="p-jf-4">
              <p className="text-jf-read-sm font-medium">interactive</p>
              <p className="text-jf-read-xs text-[var(--jf-muted)]">Hover and focus-within affordance.</p>
            </Card>
            <Card className="p-jf-4">
              <p className="text-jf-read-sm font-medium">composed</p>
              <p className="text-jf-read-xs text-[var(--jf-muted)]">
                A card holding a status pill: <Badge tone="neutral">Draft</Badge>
              </p>
            </Card>
          </div>
        </Section>

        {/* ── Badge ──────────────────────────────────────────────────────── */}
        <Section aria-labelledby="badge">
          <SectionHeading id="badge" title="Badge">
            A status pill. Each tone is a text-on-tint pair; the danger tone uses the danger TEXT token, because the
            danger marker colour cannot carry a label.
          </SectionHeading>
          <div className="flex flex-wrap items-center gap-jf-2">
            <Badge>neutral</Badge>
            <Badge tone="info">info</Badge>
            <Badge tone="success">success</Badge>
            <Badge tone="warning">warning</Badge>
            <Badge tone="danger">danger</Badge>
          </div>
        </Section>

        {/* ── fields ─────────────────────────────────────────────────────── */}
        <Section aria-labelledby="fields">
          <SectionHeading id="fields" title="Input, Textarea, Select, Checkbox, Field">
            <code>Field</code> owns the wiring: it generates the ids and sets{" "}
            <code>aria-describedby</code>, and an error also sets <code>aria-invalid</code> — the attribute paints
            the red edge, so a control cannot look wrong without being announced as wrong.
          </SectionHeading>
          <div className="grid gap-jf-6 md:grid-cols-2">
            <Field id="design-name" label="Name" hint="Shown on the form and in the receipt.">
              <Input placeholder="Aisyah Rahman" />
            </Field>
            <Field id="design-email" label="Email" required errorText="Enter a valid email address.">
              <Input type="email" defaultValue="not-an-email" />
            </Field>
            <Field id="design-plan" label="Plan" hint="You can change this later.">
              <Select defaultValue="starter">
                <option value="starter">Starter</option>
                <option value="growth">Growth</option>
              </Select>
            </Field>
            <Field id="design-disabled" label="Workspace" hint="Locked while the workspace is being renamed.">
              <Input defaultValue="kedai-aisyah" disabled />
            </Field>
            <Field id="design-notes" label="Notes" hint="Markdown is not supported." className="md:col-span-2">
              <Textarea placeholder="Delivery instructions" />
            </Field>
            <Field id="design-code" label="Verification code" hint="Six digits from your authenticator app." className="md:col-span-2">
              <CodeInput defaultValue="123456" aria-label="Verification code" />
            </Field>
            <div className="flex flex-col gap-jf-2">
              <Specimen label="Checkbox — default / checked / disabled">
                <span className="flex items-center gap-jf-4">
                  <Checkbox aria-label="Default" />
                  <Checkbox aria-label="Checked" defaultChecked />
                  <Checkbox aria-label="Disabled" disabled />
                  <Checkbox aria-label="Checked and disabled" defaultChecked disabled />
                </span>
              </Specimen>
            </div>
          </div>
        </Section>

        {/* ── Alert ──────────────────────────────────────────────────────── */}
        <Section aria-labelledby="alert">
          <SectionHeading id="alert" title="Alert">
            Four tones. Info and success announce as <code>status</code>; warning and danger announce as{" "}
            <code>alert</code>.
          </SectionHeading>
          <div className="flex flex-col gap-jf-3">
            <Alert tone="info" title="Heads up">
              The CHIP sandbox is in use, so payments will not settle.
            </Alert>
            <Alert tone="success" title="Saved">
              Your storefront footer is live.
            </Alert>
            <Alert tone="warning" title="Check the address">
              Two orders have a postcode that does not match the state.
            </Alert>
            <Alert tone="danger" title="Payment refused">
              The card was declined by the issuer. Nothing was charged.
            </Alert>
          </div>
        </Section>

        {/* ── Table ──────────────────────────────────────────────────────── */}
        <Section aria-labelledby="table">
          <SectionHeading id="table" title="Table & EmptyState">
            A native table with the surface decisions made once, and an empty state that is a ROW in the same table
            — so an empty result cannot render as a header over nothing.
          </SectionHeading>
          <div className="overflow-x-auto">
            <Table>
              <TableHead>
                <TableRow>
                  <TableHeaderCell>Reference</TableHeaderCell>
                  <TableHeaderCell>Customer</TableHeaderCell>
                  <TableHeaderCell className="text-right">Total</TableHeaderCell>
                  <TableHeaderCell>Status</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                <TableRow>
                  <TableCell className="font-mono text-jf-read-xs">JF-1042</TableCell>
                  <TableCell>Aisyah Rahman</TableCell>
                  <TableCell className="text-right font-mono">RM 128.00</TableCell>
                  <TableCell>
                    <Badge tone="success">paid</Badge>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell className="font-mono text-jf-read-xs">JF-1043</TableCell>
                  <TableCell>Lim Wei Jie</TableCell>
                  <TableCell className="text-right font-mono">RM 64.50</TableCell>
                  <TableCell>
                    <Badge tone="warning">pending</Badge>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>

          <div className="mt-jf-6 overflow-x-auto">
            <Table>
              <caption className="pb-jf-2 text-left text-jf-read-sm text-[var(--jf-muted)]">
                The same table with no rows.
              </caption>
              <TableHead>
                <TableRow>
                  <TableHeaderCell>Reference</TableHeaderCell>
                  <TableHeaderCell>Customer</TableHeaderCell>
                  <TableHeaderCell className="text-right">Total</TableHeaderCell>
                  <TableHeaderCell>Status</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                <TableEmpty columns={4}>
                  <EmptyState
                    title="No sales yet"
                    description="A sale appears here the moment a customer completes a form."
                    action={<Button size="sm">Open the builder</Button>}
                  />
                </TableEmpty>
              </TableBody>
            </Table>
          </div>
        </Section>

        {/* ── Tabs ───────────────────────────────────────────────────────── */}
        <Section aria-labelledby="tabs">
          <SectionHeading id="tabs" title="Tabs">
            <code>role=&quot;tablist&quot;</code> with a roving tabindex: one tab is in the Tab order, the arrow
            keys move inside the strip, Home/End jump to the ends.
          </SectionHeading>
          <Tabs
            label="Design system example tabs"
            items={[
              {
                id: "overview",
                label: "Overview",
                panel: <p className="text-jf-read-sm">The first panel. It is mounted; the others are not.</p>,
              },
              {
                id: "settings",
                label: "Settings",
                panel: <p className="text-jf-read-sm">The second panel.</p>,
              },
              {
                id: "billing",
                label: "Billing",
                panel: <p className="text-jf-read-sm">The third panel.</p>,
              },
              {
                id: "locked",
                label: "Locked",
                disabled: true,
                panel: <p className="text-jf-read-sm">Never reached: the tab is disabled.</p>,
              },
            ]}
          />
        </Section>

        {/* ── Skeleton ───────────────────────────────────────────────────── */}
        <Section aria-labelledby="skeleton">
          <SectionHeading id="skeleton" title="Skeleton">
            A finite pulse (three cycles), switched off outright under reduced motion. Hidden from assistive
            technology: the page&apos;s own live region announces the load.
          </SectionHeading>
          <div className="flex flex-col gap-jf-3">
            <Skeleton shape="line" />
            <Skeleton shape="line" className="w-2/3" />
            <div className="flex items-center gap-jf-3">
              <Skeleton shape="circle" />
              <Skeleton shape="block" className="w-1/2" />
            </div>
          </div>
        </Section>

        <Section spacing="sm">
          <p className="text-jf-read-xs text-[var(--jf-muted)]">
            Tokens: <code>app/globals.css</code> · primitives: <code>components/ui/</code> · contrast data:{" "}
            <code>lib/designTokenRatios.ts</code> · the gate: <code>lib/designTokens.test.ts</code>
          </p>
        </Section>
      </Container>
    </main>
  );
}
