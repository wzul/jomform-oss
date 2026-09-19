// The site name Google Search shows for jomform.com.
//
// Google's site-name system chooses the name automatically and cannot be set
// from a settings screen. It documents `WebSite` structured data on the home
// page as the strongest signal it accepts, and says it also considers
// `og:site_name`, `<title>`, heading elements and other text on the home page.
// Without the structured data, Search fell back to the bare domain, so the
// result line above the title read `jomform.com` instead of `JomForm`.
//
// This states the PREFERENCE. It does not force the outcome — Search may still
// pick another source, and a recrawl plus processing time is needed before any
// change is visible. `alternateName` exists for exactly that reason: it is the
// fallback Search considers when it is not confident about the preferred name.
//
// Keep `SITE_NAME` in step with the home page's `og:site_name` and `<title>`:
// Google asks for the site name to be used consistently across the home page,
// and siteName.test.ts fails if those sources drift apart.

export const SITE_URL = "https://jomform.com/";
export const SITE_NAME = "JomForm";

// Google's documented backup form for the domain-as-site-name case is the bare
// domain in all lower case — upper case is not detected as a preference. It is
// also the string Search was already showing, so this is a fallback, not a
// second brand claim.
export const SITE_ALTERNATE_NAME = "jomform.com";

// The `WebSite` node for the home page. One per domain: site names are not
// supported per subdirectory, and the markup belongs on the home page only.
export const WEBSITE_JSON_LD = {
  "@context": "https://schema.org",
  "@type": "WebSite",
  name: SITE_NAME,
  alternateName: SITE_ALTERNATE_NAME,
  url: SITE_URL,
} as const;

// Serialised for the inline `<script type="application/ld+json">`. `JSON.stringify`
// is safe here only because `<` is escaped: an unescaped `<` inside a string
// value would let the browser close the tag early.
export function websiteJsonLdScript(): string {
  return JSON.stringify(WEBSITE_JSON_LD).replace(/</g, "\\u003c");
}
