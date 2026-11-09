// The scale tables the /design gallery renders (card t_f2e231d3).
//
// WHY THIS IS DATA. The gallery claims "this is the type scale / the space grid /
// the radius ladder", and a table typed into JSX would be a claim nobody checks:
// the CSS can move and the page keeps describing the old step. Here the numbers
// are data, `designTokenScale.test.ts` re-reads them from `globals.css` and fails
// when they disagree, and the page renders whatever the data says — so the table
// is a MEASUREMENT of the stylesheet, not a description of what it once was.
//
// The `utility` string is the Tailwind class a caller writes; the test also
// requires it to appear in the gallery, so a step cannot be documented without
// being shown.
//
// EVERY utility in this file is a LITERAL string. That is not a style choice:
// Tailwind v4 finds classes by scanning source text, so a class built at runtime
// (`w-jf-${n}`) is never emitted and the element renders with no width at all —
// invisibly, with no error. Add a step as a new literal row.

export type TypeStep = {
  /** The `:root` token that holds the value. */
  token: string;
  /** The companion token that holds the paired line-height. */
  lineHeightToken: string;
  px: number;
  lineHeightPx: number;
  /** The utility the `@theme` alias generates. */
  utility: string;
};

export const TYPE_SCALE: readonly TypeStep[] = [
  { token: "--jf-text-xs", lineHeightToken: "--jf-text-xs--lh", px: 12, lineHeightPx: 16, utility: "text-jf-read-xs" },
  { token: "--jf-text-sm", lineHeightToken: "--jf-text-sm--lh", px: 14, lineHeightPx: 20, utility: "text-jf-read-sm" },
  { token: "--jf-text-md", lineHeightToken: "--jf-text-md--lh", px: 16, lineHeightPx: 24, utility: "text-jf-read-md" },
  { token: "--jf-text-lg", lineHeightToken: "--jf-text-lg--lh", px: 18, lineHeightPx: 28, utility: "text-jf-read-lg" },
  { token: "--jf-text-xl", lineHeightToken: "--jf-text-xl--lh", px: 20, lineHeightPx: 28, utility: "text-jf-read-xl" },
  { token: "--jf-text-2xl", lineHeightToken: "--jf-text-2xl--lh", px: 24, lineHeightPx: 32, utility: "text-jf-read-2xl" },
  { token: "--jf-text-3xl", lineHeightToken: "--jf-text-3xl--lh", px: 30, lineHeightPx: 36, utility: "text-jf-read-3xl" },
];

export type SpaceStep = { token: string; px: number; utility: string };

/** A 4px grid: step N is N x 4px. The utilities are LITERALS on purpose — see
 *  the note at the top of this file; a computed `w-jf-${n}` is a class Tailwind's
 *  extractor never sees, so it would be generated for no step at all. */
export const SPACE_SCALE: readonly SpaceStep[] = [
  { token: "--jf-space-1", px: 4, utility: "w-jf-1" },
  { token: "--jf-space-2", px: 8, utility: "w-jf-2" },
  { token: "--jf-space-3", px: 12, utility: "w-jf-3" },
  { token: "--jf-space-4", px: 16, utility: "w-jf-4" },
  { token: "--jf-space-5", px: 20, utility: "w-jf-5" },
  { token: "--jf-space-6", px: 24, utility: "w-jf-6" },
  { token: "--jf-space-7", px: 28, utility: "w-jf-7" },
  { token: "--jf-space-8", px: 32, utility: "w-jf-8" },
  { token: "--jf-space-9", px: 36, utility: "w-jf-9" },
  { token: "--jf-space-10", px: 40, utility: "w-jf-10" },
  { token: "--jf-space-11", px: 44, utility: "w-jf-11" },
  { token: "--jf-space-12", px: 48, utility: "w-jf-12" },
];

export type RadiusStep = { token: string; px: number; utility: string };

export const RADIUS_SCALE: readonly RadiusStep[] = [
  { token: "--jf-radius-sm", px: 4, utility: "rounded-jf-sm" },
  { token: "--jf-radius-md", px: 8, utility: "rounded-jf-md" },
  { token: "--jf-radius-lg", px: 12, utility: "rounded-jf-lg" },
  { token: "--jf-radius-xl", px: 16, utility: "rounded-jf-xl" },
  { token: "--jf-radius-full", px: 9999, utility: "rounded-jf-full" },
];

export type ShadowStep = { token: string; utility: string };

export const SHADOW_SCALE: readonly ShadowStep[] = [
  { token: "--jf-shadow-1", utility: "shadow-jf-1" },
  { token: "--jf-shadow-2", utility: "shadow-jf-2" },
  { token: "--jf-shadow-3", utility: "shadow-jf-3" },
];
