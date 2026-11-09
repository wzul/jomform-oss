// The files that may still carry a raw colour literal (card t_f2e231d3).
//
// THIS LIST IS EMPTY ON PURPOSE, AND THAT IS THE ASSERTION. When the design-system
// card was written the tree held 41 raw hex literals across 12 `.tsx` files; by the
// time the primitive layer landed, `scripts/design-guard.py` (the `design` preflight
// gate) and card t_e2f3a553 had already moved every one of them onto a token, and
// the only literals left are nine values that are DATA, not styling — the brand
// page's swatch palette, a usage sentence that names a hex, and three colour-picker
// defaults that are persisted with a form. Those carry the documented per-line
// directive the gate already honours (`design-tokens-exempt: <reason>`), so they do
// not belong here: an allowlist entry is a debt, and a justified data literal is not.
//
// WHY KEEP AN EMPTY LIST AT ALL. So the rule has one place to look, and so
// `designTokens.test.ts` can assert the end state directly: an entry that is added
// to get a build green fails, because the test also requires every entry to still
// contain a hex literal (entries may only shrink) AND requires this list to be
// empty. A new deviation is fixed or justified, never parked.
//
// The paths are relative to `web/src` with forward slashes, e.g.
// "app/foo/page.tsx" — the same shape `designTokens.test.ts` walks.

export const DESIGN_TOKEN_ALLOWLIST: readonly string[] = [];
