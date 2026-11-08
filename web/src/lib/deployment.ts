// What address does THIS deployment publish for a workspace (card t_48a93084)?
//
// The dashboard used to know one answer and hardcode it: "<slug>.jomform.com" —
// in the picker's hint, in the workspace switcher, in the rename form, in the
// settings copy and in the legal-page link. That is the HOSTED product's shape.
// A self-hosted install has no such host: ONE origin serves every workspace,
// and its forms live at `<APP_BASE_URL>/f/<form-slug>` (see the API's
// internal/installurl, and PublicHandlers.ownOriginForm on the Go side).
//
// The dashboard cannot see the API's runtime config, so the API now publishes
// it at GET /v1/meta (proxied as /api/meta) and every surface below asks THIS
// module what to print.
//
// THE FLAG DECIDES, NEVER THE HOST. `origin` is a true statement on BOTH
// deployments — the hosted product's APP_BASE_URL is its own apex — so a rule
// like "the origin does not end in jomform.com, therefore self-hosted" takes
// the wrong branch on the product itself. That is exactly how issue #77 was
// broken (card t_4ddab217): the apex began serving tenant forms because a
// handler inferred the deployment from host equality. Every function here reads
// `selfHosted` and only `selfHosted`.
//
// When the deployment is unknown (the fetch has not answered yet) the callers
// pass null and print NOTHING rather than the hosted address. A flash of
// "…jomform.com" on an install that has no such host is the defect, not a
// cosmetic detail.

/** The deployment facts GET /v1/meta publishes. */
export type Deployment = {
  /** JOMFORM_SELF_HOSTED on the API. The ONLY branch input. */
  selfHosted: boolean;
  /** The install's own origin ("https://forms.example.com"), "" when unset. */
  origin: string;
};

/** The hosted product's tenant domain. */
export const HOSTED_DOMAIN = "jomform.com";

/** Where a custom domain points on the HOSTED product. */
export const HOSTED_CNAME_TARGET = "custom.jomform.com";

/**
 * normalizeOrigin trims an origin and refuses anything that is not an absolute
 * http(s) URL with a host. `localhost:18099` parses as a URL whose scheme is
 * "localhost", and printing it produces a link that resolves relative to the
 * current page — the dead-link class the footer rules already refuse
 * (card t_457c4d87). Such a value yields "".
 */
export function normalizeOrigin(raw: unknown): string {
  if (typeof raw !== "string") return "";
  const base = raw.trim().replace(/\/+$/, "");
  if (!/^https?:\/\/[^/?#]/i.test(base)) return "";
  return base;
}

/**
 * parseDeployment reads a /v1/meta response. Anything unexpected is the HOSTED
 * deployment — the product's own behaviour, and the safe reading of a malformed
 * body — because a wrong self-hosted branch would print an origin that may not
 * be this install's.
 */
export function parseDeployment(raw: unknown): Deployment {
  if (!raw || typeof raw !== "object") return { selfHosted: false, origin: "" };
  const r = raw as Record<string, unknown>;
  return {
    selfHosted: r.self_hosted === true,
    origin: normalizeOrigin(r.origin),
  };
}

/**
 * workspaceURL is the address a workspace is reached at, or "" when it cannot be
 * printed honestly:
 *
 *   hosted     https://<slug>.jomform.com
 *   self-host  <origin>/            (the apex serves the workspace)
 */
export function workspaceURL(slug: string, d: Deployment | null): string {
  if (!d) return "";
  if (!d.selfHosted) return slug ? `https://${slug}.${HOSTED_DOMAIN}` : "";
  return d.origin ? `${d.origin}/` : "";
}

/**
 * workspaceAddressLabel is what a sentence names: the hosted host without a
 * scheme ("acme.jomform.com", exactly as the copy read before this change) or
 * the install's own origin including its scheme, because on a self-hosted
 * install the origin IS the address and "localhost:18099" alone reads like a
 * bare hostname.
 */
export function workspaceAddressLabel(slug: string, d: Deployment | null): string {
  if (!d) return "";
  if (!d.selfHosted) return slug ? `${slug}.${HOSTED_DOMAIN}` : "";
  return d.origin;
}

/**
 * addressLabel renders a URL the API already returned (workspace rows carry
 * `url`) as a display string: no scheme, no trailing slash.
 */
export function addressLabel(url: string | undefined | null): string {
  if (!url) return "";
  return url.replace(/^https?:\/\//i, "").replace(/\/+$/, "");
}

/**
 * legalPageURL is the public address of one hosted legal page, or "" when this
 * deployment does not serve one.
 *
 * The route is host-scoped (api/internal/httpapi/rest_legal.go: it resolves the
 * workspace from the request's subdomain), so a self-hosted install — which has
 * no subdomains — answers 404 for /privacy, /terms and /refund. Hiding the link
 * is the honest answer; printing a URL that 404s is the dead-link defect
 * card t_806c3ea2 removed from the footer.
 */
export function legalPageURL(slug: string, page: string, d: Deployment | null): string {
  if (!d || d.selfHosted) return "";
  const base = workspaceURL(slug, d);
  return base && page ? `${base}/${page}` : "";
}

/**
 * cnameTarget is where a merchant points a custom domain: the hosted product's
 * CNAME target, or this install's own host on a self-hosted deployment. The
 * verifier (POST /v1/rest/domains/{id}/verify) probes whether the domain SERVES
 * JomForm, so any host that routes to this install works — the copy must name
 * the host that actually does.
 *
 * The unknown (null) branch answers the HOSTED target rather than "". That is
 * the one place the "print nothing while unknown" rule bends, and deliberately:
 * this is a DNS record value a merchant types, and an empty target renders as a
 * bare "CNAME your.domain →" with nothing to type — unusable on the deployment
 * that needs it most (the product). The ADDRESS printers (workspaceURL,
 * workspaceAddressLabel, legalPageURL) still render nothing until the answer
 * arrives, because there a missing line is harmless and a wrong host is the
 * defect itself.
 */
export function cnameTarget(d: Deployment | null): string {
  if (!d) return HOSTED_CNAME_TARGET;
  if (!d.selfHosted) return HOSTED_CNAME_TARGET;
  return d.origin ? addressLabel(d.origin) : "";
}
