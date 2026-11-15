// The files that may still carry a design-system deviation (cards t_f2e231d3,
// t_46ae7ad7).
//
// TWO LISTS, ONE RULE: an entry is a DEBT, it carries the COUNT it was seeded
// with, and it may only SHRINK. `designTokens.test.ts` enforces all three halves:
//
//   1. a file NOT on the list must carry ZERO findings — a new deviation fails
//      the build, it is not parked here;
//   2. a file ON the list must carry AT MOST its seeded count — the number can
//      go down, never up, so a file cannot get worse while it is allowlisted;
//   3. a file ON the list must still carry AT LEAST ONE finding — the moment a
//      file is clean its entry FAILS until it is deleted, so an entry cannot
//      outlive the drift it was written for.
//
// The total is printed by the unit suite on every run (`design-tokens: <label>:
// N finding(s) in M file(s), K allowlisted`), so the shrink is visible in the
// gate's output instead of being inferred from a diff.
//
// ── DESIGN_TOKEN_ALLOWLIST: raw colour literals and arbitrary colour utilities
//
// THIS LIST IS EMPTY ON PURPOSE, AND THAT IS THE ASSERTION. When the design
// system card was written the tree held 41 raw hex literals across 12 `.tsx`
// files; by the time the primitive layer landed, `scripts/design-guard.py` (the
// `design` preflight gate) and card t_e2f3a553 had already moved every one of
// them onto a token. What is left in the tree is not styling: the brand page's
// swatch palette, a usage sentence that names a hex, and three colour-picker
// defaults that are persisted with a form. Those carry the documented per-line
// directive both scanners honour (`design-tokens-exempt: <reason>`), so they do
// not belong here — a justified data literal is not a debt.
//
// Card t_46ae7ad7 re-measured `origin/main` before adding the second list rather
// than trusting the card body's figure (680 literals in 59 files): the tree
// carries 0 in shipped code. An empty list is the shrink END state, so the seed
// for this rule is `{}` and any entry added to get a build green fails.
//
// ── ARBITRARY_PX_ALLOWLIST: `[<n>px]` where a scale step already names <n>
//
// MEASURED seed (card t_46ae7ad7, on `origin/main`): 7 `min-h-[44px]` occurrences
// across 4 files. 44px is `--jf-space-11`, so the token form is `min-h-jf-11`
// (Tailwind v4 resolves it from the `--spacing-jf-11` entry in the `@theme`
// block) — the substitution is exact, which is why this is a debt to pay rather
// than a value to justify.
//
// WHY THIS RULE DOES NOT BAN px. An arbitrary length is often the only honest
// form: `w-[720px]` and `h-[420px]` on the login page's decorative blur, the
// `top-[-260px]` that places it, `w-[220px]` on a photo drop-zone. No scale step
// names those numbers, so they are outside this rule by construction — it fires
// only when a step EXISTS, which is the case where the literal is a bypass of a
// decision the scale already made and the token form renders the same pixels.
//
// The paths are relative to `web/src` with forward slashes, e.g.
// "app/foo/page.tsx" — the same shape `designTokens.test.ts` walks.
//
// Card t_a1af018f (the migration card) empties the px list file by file; this
// file is where that progress is recorded, one deletion at a time.

/** A per-file debt: the file (relative to `web/src`) and the count it was seeded with. */
export type CountAllowlist = Readonly<Record<string, number>>;

/** Raw colour literals. Must stay empty — see the note above. */
export const DESIGN_TOKEN_ALLOWLIST: CountAllowlist = {};

/**
 * `[<n>px]` where a scale step already names `<n>`. Seeded at the measured count
 * on `origin/main` and shrink-only.
 */
export const ARBITRARY_PX_ALLOWLIST: CountAllowlist = {
  "app/docs/docsBreadcrumb.tsx": 1,
  "app/docs/layout.tsx": 1,
  "app/docs/page.tsx": 4,
  "app/forms/page.tsx": 1,
};
