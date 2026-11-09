// The measured contrast of every colour token this app ships (card t_f2e231d3).
//
// WHY THIS IS DATA AND NOT A COMMENT TABLE
//   The token layer's rationale comments in `app/globals.css` quote ratios, and a
//   quoted ratio rots the moment a value moves: the number is prose, the colour is
//   code, and nothing compares them. Here the pairing (foreground, background,
//   floor) and the MEASURED ratio are one record, `/design` renders every record,
//   and `designTokens.test.ts` re-derives each ratio from `globals.css` and fails
//   when the two disagree — so "the gallery says 4.70:1" is a claim the unit suite
//   re-measures on every run instead of a number someone once typed.
//
// WHAT `min` MEANS
//   4.5 — WCAG 2.2 AA for normal-size text;
//   3.0 — WCAG 1.4.11, a graphical object or a UI component's boundary;
//   1.0 — not a legibility pairing at all (a tint, a divider, a gridline). It is
//         listed so the gallery can show EVERY colour token with its measurement
//         rather than only the ones that carry text, and so a token cannot be
//         added without one.
//
// THE COVERAGE RULE IS ENFORCED, NOT REMEMBERED: the unit test requires every
// `--jf-*` token that holds a colour (in the light block AND in the dark block)
// to appear in at least one pair below, so a new colour token cannot land without
// a measurement.
//
// A translucent backdrop is composited over its theme's `--jf-surface` before the
// ratio is taken: `--jf-primary-tint` is 8-digit (an alpha over whatever is
// behind it), and a ratio against a value that does not exist — the raw alpha —
// would be meaningless.

export type ContrastPair = {
  /** Foreground token, spelled `--jf-…` exactly as `globals.css` declares it. */
  fg: string;
  /** Background token. An 8-digit value is composited over `--jf-surface`. */
  bg: string;
  /** The WCAG floor this pairing is held to. */
  min: number;
  /** What the pairing IS, in the product. */
  role: string;
  /** Measured, rounded to 2 decimal places. Re-derived by the unit test. */
  ratio: number;
};

export const LIGHT_CONTRASTS: readonly ContrastPair[] = [
  { fg: "--jf-text", bg: "--jf-surface", ratio: 17.74, min: 4.5, role: "body text on a card" },
  { fg: "--jf-text", bg: "--jf-bg", ratio: 16.67, min: 4.5, role: "body text on the page" },
  { fg: "--jf-text", bg: "--jf-surface-2", ratio: 15.85, min: 4.5, role: "body text on the second surface" },
  { fg: "--jf-text-secondary", bg: "--jf-surface", ratio: 10.31, min: 4.5, role: "the step between body and muted text" },
  { fg: "--jf-text-tertiary", bg: "--jf-surface", ratio: 5.77, min: 4.5, role: "the third neutral text step" },
  { fg: "--jf-text-tertiary", bg: "--jf-surface-2", ratio: 5.15, min: 4.5, role: "a disabled control's label on the neutral fill" },
  { fg: "--jf-muted", bg: "--jf-surface", ratio: 4.83, min: 4.5, role: "secondary copy on a card" },
  { fg: "--jf-muted", bg: "--jf-bg", ratio: 4.54, min: 4.5, role: "secondary copy on the page" },
  { fg: "--jf-muted", bg: "--jf-bg-soft", ratio: 4.55, min: 4.5, role: "secondary copy on the soft page tint" },
  { fg: "--jf-on-color", bg: "--jf-primary", ratio: 4.70, min: 4.5, role: "the primary CTA's label, at rest" },
  { fg: "--jf-on-color", bg: "--jf-primary-hover", ratio: 4.51, min: 4.5, role: "the primary CTA's label, hovered" },
  { fg: "--jf-on-color", bg: "--jf-primary-active", ratio: 7.22, min: 4.5, role: "the primary CTA's label, pressed" },
  { fg: "--jf-on-color", bg: "--jf-primary-strong", ratio: 6.29, min: 4.5, role: "a label on the darker indigo step" },
  { fg: "--jf-on-color", bg: "--jf-danger-fill", ratio: 6.52, min: 4.5, role: "the danger CTA's label on its fill" },
  { fg: "--jf-primary-strong", bg: "--jf-bg", ratio: 5.91, min: 4.5, role: "an inline link / the selected tab's label, on the page" },
  { fg: "--jf-primary-on-tint", bg: "--jf-primary-tint", ratio: 6.68, min: 4.5, role: "the selected row's text on its tint" },
  { fg: "--jf-muted-on-tint", bg: "--jf-primary-tint", ratio: 5.06, min: 4.5, role: "muted text on the selected tint" },
  { fg: "--jf-danger", bg: "--jf-surface", ratio: 3.76, min: 3.0, role: "the danger MARKER (icon, dot) — never a label" },
  { fg: "--jf-danger-text", bg: "--jf-surface", ratio: 6.52, min: 4.5, role: "danger text on a card" },
  { fg: "--jf-danger-text", bg: "--jf-danger-bg", ratio: 5.96, min: 4.5, role: "danger text on its own tint" },
  { fg: "--jf-warning-text", bg: "--jf-surface", ratio: 5.02, min: 4.5, role: "warning text on a card" },
  { fg: "--jf-warning-text", bg: "--jf-warning-bg", ratio: 4.84, min: 4.5, role: "warning text on its callout" },
  { fg: "--jf-warning-text", bg: "--jf-warning-bg-hover", ratio: 4.51, min: 4.5, role: "warning text on the callout's hover" },
  { fg: "--jf-success", bg: "--jf-surface", ratio: 2.54, min: 1.0, role: "the success MARKER — white on it is 2.54:1, never a fill for text" },
  { fg: "--jf-success-bg", bg: "--jf-surface", ratio: 1.05, min: 1.0, role: "the success tint behind a message" },
  { fg: "--jf-warning", bg: "--jf-surface", ratio: 3.19, min: 3.0, role: "the warning MARKER" },
  { fg: "--jf-info", bg: "--jf-surface", ratio: 6.70, min: 4.5, role: "the informational accent, as text or an icon" },
  { fg: "--jf-text", bg: "--jf-code-bg", ratio: 16.26, min: 4.5, role: "inline code on its own backdrop" },
  { fg: "--jf-border", bg: "--jf-surface", ratio: 1.23, min: 1.0, role: "a divider, not an edge — decoration" },
  { fg: "--jf-border-strong", bg: "--jf-surface", ratio: 1.61, min: 1.0, role: "a control's edge (a stronger step than the divider)" },
  { fg: "--jf-accent", bg: "--jf-surface", ratio: 3.84, min: 3.0, role: "the interactive accent as a border or ring" },
  { fg: "--jf-focus-ring", bg: "--jf-surface", ratio: 4.70, min: 3.0, role: "the keyboard focus outline" },
  { fg: "--jf-switch-off", bg: "--jf-surface", ratio: 4.76, min: 3.0, role: "the switch's OFF track (its white knob is 4.76:1 on it)" },
  { fg: "--jf-chart-axis", bg: "--jf-surface", ratio: 2.54, min: 1.0, role: "a chart's axis — lighter than any body text on purpose" },
  { fg: "--jf-chart-grid", bg: "--jf-surface", ratio: 1.16, min: 1.0, role: "a chart's gridline" },
  // The INK BAND (card t_09f60795): a fixed dark slab on the landing hero, so its
  // ink is held to the band's own two fills rather than to the page.
  { fg: "--jf-ink-text", bg: "--jf-ink", ratio: 17.48, min: 4.5, role: "the ink band's primary text" },
  { fg: "--jf-ink-text", bg: "--jf-ink-raised", ratio: 15.72, min: 4.5, role: "the ink band's primary text on its raised slab" },
  { fg: "--jf-ink-muted", bg: "--jf-ink", ratio: 8.75, min: 4.5, role: "secondary copy on the ink band" },
  { fg: "--jf-ink-muted", bg: "--jf-ink-raised", ratio: 7.87, min: 4.5, role: "secondary copy on the ink band's raised slab" },
  { fg: "--jf-ink-accent", bg: "--jf-ink", ratio: 8.14, min: 4.5, role: "the terminal's \"you\" cue and the hero's kicker line" },
  { fg: "--jf-ink-ok", bg: "--jf-ink", ratio: 11.98, min: 4.5, role: "the agent's reply line in the terminal" },
  { fg: "--jf-ink-border", bg: "--jf-ink", ratio: 1.37, min: 1.0, role: "the band's decorative hairline (never a text or control edge)" },
];

export const DARK_CONTRASTS: readonly ContrastPair[] = [
  { fg: "--jf-text", bg: "--jf-surface", ratio: 14.59, min: 4.5, role: "body text on a card" },
  { fg: "--jf-text", bg: "--jf-bg", ratio: 15.83, min: 4.5, role: "body text on the page" },
  { fg: "--jf-text", bg: "--jf-surface-2", ratio: 13.18, min: 4.5, role: "body text on the second surface" },
  { fg: "--jf-text-secondary", bg: "--jf-surface", ratio: 10.69, min: 4.5, role: "the step between body and muted text" },
  { fg: "--jf-text-tertiary", bg: "--jf-surface", ratio: 7.81, min: 4.5, role: "the third neutral text step" },
  { fg: "--jf-text-tertiary", bg: "--jf-surface-2", ratio: 7.06, min: 4.5, role: "a disabled control's label on the neutral fill" },
  { fg: "--jf-muted", bg: "--jf-surface", ratio: 6.76, min: 4.5, role: "secondary copy on a card" },
  { fg: "--jf-muted", bg: "--jf-bg", ratio: 7.34, min: 4.5, role: "secondary copy on the page" },
  { fg: "--jf-muted", bg: "--jf-bg-soft", ratio: 7.10, min: 4.5, role: "secondary copy on the soft page tint" },
  { fg: "--jf-on-color", bg: "--jf-primary", ratio: 4.70, min: 4.5, role: "the primary CTA's label, at rest" },
  { fg: "--jf-on-color", bg: "--jf-primary-hover", ratio: 4.51, min: 4.5, role: "the primary CTA's label, hovered" },
  { fg: "--jf-on-color", bg: "--jf-primary-active", ratio: 7.22, min: 4.5, role: "the primary CTA's label, pressed" },
  { fg: "--jf-on-color", bg: "--jf-danger-fill", ratio: 6.47, min: 4.5, role: "the danger CTA's label on its fill, in dark" },
  { fg: "--jf-primary-on-tint", bg: "--jf-primary-tint", ratio: 9.43, min: 4.5, role: "the selected row's text on its tint" },
  { fg: "--jf-muted-on-tint", bg: "--jf-primary-tint", ratio: 7.22, min: 4.5, role: "muted text on the selected tint" },
  { fg: "--jf-danger", bg: "--jf-surface", ratio: 6.29, min: 3.0, role: "the danger MARKER (icon, dot) — never a label" },
  { fg: "--jf-danger-text", bg: "--jf-surface", ratio: 9.17, min: 4.5, role: "danger text on a card" },
  { fg: "--jf-danger-text", bg: "--jf-danger-bg", ratio: 9.08, min: 4.5, role: "danger text on its own tint" },
  { fg: "--jf-warning-text", bg: "--jf-surface", ratio: 12.07, min: 4.5, role: "warning text on a card" },
  { fg: "--jf-warning-text", bg: "--jf-warning-bg", ratio: 11.00, min: 4.5, role: "warning text on its callout" },
  { fg: "--jf-warning-text", bg: "--jf-warning-bg-hover", ratio: 9.30, min: 4.5, role: "warning text on the callout's hover" },
  { fg: "--jf-success", bg: "--jf-surface", ratio: 9.06, min: 1.0, role: "the success MARKER — white on it is 2.54:1, never a fill for text" },
  { fg: "--jf-success-bg", bg: "--jf-surface", ratio: 1.14, min: 1.0, role: "the success tint behind a message" },
  { fg: "--jf-warning", bg: "--jf-surface", ratio: 10.43, min: 3.0, role: "the warning MARKER" },
  { fg: "--jf-info", bg: "--jf-surface", ratio: 9.66, min: 4.5, role: "the informational accent, as text or an icon" },
  { fg: "--jf-text", bg: "--jf-code-bg", ratio: 13.83, min: 4.5, role: "inline code on its own backdrop" },
  { fg: "--jf-border", bg: "--jf-surface", ratio: 1.30, min: 1.0, role: "a divider, not an edge — decoration" },
  { fg: "--jf-border-strong", bg: "--jf-surface", ratio: 1.78, min: 1.0, role: "a control's edge (a stronger step than the divider)" },
  { fg: "--jf-accent", bg: "--jf-surface", ratio: 6.40, min: 3.0, role: "the interactive accent as a border or ring" },
  { fg: "--jf-focus-ring", bg: "--jf-surface", ratio: 6.40, min: 3.0, role: "the keyboard focus outline" },
  { fg: "--jf-switch-off", bg: "--jf-surface", ratio: 4.62, min: 3.0, role: "the switch's OFF track (its white knob is 4.76:1 on it)" },
  { fg: "--jf-chart-axis", bg: "--jf-surface", ratio: 5.63, min: 1.0, role: "a chart's axis — lighter than any body text on purpose" },
  { fg: "--jf-chart-grid", bg: "--jf-surface", ratio: 1.23, min: 1.0, role: "a chart's gridline" },
  { fg: "--jf-primary-strong", bg: "--jf-surface", ratio: 8.73, min: 4.5, role: "the indigo text step on a dark card" },
];
