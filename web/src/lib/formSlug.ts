// A form's public URL path segment — the dashboard half of the rule the API
// enforces (card t_f76f81eb).
//
// WHY THIS MODULE EXISTS. The Form Settings slug field normalised the value and
// sent it, and the API stored whatever arrived. Measured on prod v1.25.0:
//
//	POST  /v1/rest/forms      {"slug":"bad slug!!/x"} -> 201, url ".../bad slug!!/x"
//	PATCH /v1/rest/forms/{id} {"slug":"login"}        -> 200
//
// "login", "privacy" and "api" are all WELL FORMED, so a charset-only field
// offered them happily and the merchant discovered the refusal from a 400 after
// the save — or, before this fix, from a buyer who got a 404. The rule lives in
// Go (dbstore.ValidateFormSlug + urlguard.ReservedFormSlug); this module mirrors
// it so the field can refuse before the request is sent.
//
// The mirror cannot be allowed to drift: web/src/lib/formSlug.test.ts reads the
// Go sources and fails when the reserved list, the length cap or the charset
// disagree in either direction.
//
// This repo has no JSX test runner, so the decision lives in a plain module and
// the page calls it (the same shape lib/publishResult.ts and
// lib/workspaceSlug.ts use).

/** Mirrors `dbstore.formSlugMaxLen` in the Go API. 1-40 is the documented window. */
export const FORM_SLUG_MAX = 40;

/** Mirrors `dbstore.ValidFormSlug`'s charset: a-z, 0-9 and hyphen only. */
export const FORM_SLUG_CHARSET = /^[a-z0-9-]+$/;

/** Mirrors `urlguard.ReservedSubdomainCode`'s sibling for form slugs. */
export const FORM_SLUG_RESERVED_CODE = "form.slugReserved";

/** The shape refusal's existing code (unchanged by this card). */
export const FORM_SLUG_INVALID_CODE = "form.slugInvalid";

/**
 * Mirrors `urlguard.reservedSubdomainPrefix`. A label JomForm owns under this
 * prefix is refused whether or not the exact name is on the list below, so a
 * surface added later is covered without a second edit.
 */
export const FORM_SLUG_RESERVED_PREFIX = "jomform-";

/**
 * Mirrors the union of `urlguard.reservedSubdomains` (the labels a WORKSPACE may
 * not claim — reused, not re-invented) and `urlguard.reservedFormSlugPaths` (the
 * single-segment GET routes this API serves on the same host a form is served
 * on: a literal route wins over /{slug}, so such a form could never be reached).
 *
 * Sorted, lower-case, no dots. formSlug.test.ts compares this array against the
 * two Go maps and fails on ANY difference in either direction.
 */
export const FORM_SLUG_RESERVED: readonly string[] = [
  "account",
  "accounts",
  "admin",
  "api",
  "app",
  "assets",
  "billing",
  "blog",
  "cdn",
  "checkout",
  "custom",
  "docs",
  "healthz",
  "help",
  "login",
  "mail",
  "mcp",
  "metrics",
  "pay",
  "payments",
  "privacy",
  "readyz",
  "refund",
  "s3",
  "security",
  "signup",
  "status",
  "support",
  "templates",
  "terms",
  "version",
  "www",
];

/**
 * Normalises what the merchant typed into the value that will be sent: lower
 * case, every run of characters a slug cannot contain collapsed to a hyphen, and
 * hyphens trimmed from the ends. This mirrors the API's normalisation
 * (`dbstore.ValidateFormSlug` trims and lower-cases) so the value the field
 * shows is the value the API stores.
 *
 * It deliberately does NOT try to make a reserved name acceptable — that refusal
 * has no normalised form, which is why formSlugProblem reports it separately.
 */
export function normalizeFormSlug(raw: string): string {
  return raw
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, FORM_SLUG_MAX);
}

/** True when the (already lower-cased) value is a name JomForm owns or serves. */
export function isReservedFormSlug(slug: string): boolean {
  const s = slug.trim().toLowerCase();
  return s.startsWith(FORM_SLUG_RESERVED_PREFIX) || FORM_SLUG_RESERVED.includes(s);
}

export type FormSlugProblem = "length" | "chars" | "reserved" | null;

/**
 * formSlugProblem reports why the API would refuse this slug, or null when it
 * would accept it. The page uses it to refuse BEFORE sending, so the field can
 * never offer a value the server refuses.
 *
 * The order of the legs is not observable: every reserved label is itself well
 * formed and inside the cap (formSlug.test.ts asserts that), so no value is
 * simultaneously "malformed" and "a reserved label" — Go and this function
 * therefore always reach the same verdict, and answer the same code.
 */
export function formSlugProblem(slug: string): FormSlugProblem {
  const s = slug.trim().toLowerCase();
  if (s.length === 0 || s.length > FORM_SLUG_MAX) return "length";
  if (!FORM_SLUG_CHARSET.test(s)) return "chars";
  if (isReservedFormSlug(s)) return "reserved";
  return null;
}

/**
 * formSlugBlocked is the SAVE decision: the problem that must stop the request,
 * or null when the value may be sent.
 *
 * The one exception is a slug the form ALREADY has. A row stored before the rule
 * existed keeps its slug, and re-saving that form's settings must not be blocked
 * by a name nobody is trying to claim now — the store refuses NEW claims, not
 * existing rows. Every other case is a plain refusal.
 */
export function formSlugBlocked(nextSlug: string, currentSlug: string): FormSlugProblem {
  const problem = formSlugProblem(nextSlug);
  if (problem && nextSlug.trim().toLowerCase() !== currentSlug.trim().toLowerCase()) return problem;
  return null;
}
