# Site name in Google Search — what this change does and does not do

The search result line above a title showed the bare domain `jomform.com` instead of
`JomForm`. This note records what was changed, what was verified, and the part that is
not ours to decide.

## What was changed

`web/src/app/page.tsx` (the home page's server component) now renders one
`WebSite` structured-data block into the server HTML:

```json
{
  "@context": "https://schema.org",
  "@type": "WebSite",
  "name": "JomForm",
  "alternateName": "jomform.com",
  "url": "https://jomform.com/"
}
```

The values live in `web/src/lib/siteName.ts` so `og:site_name` and the structured data
cannot drift apart, and the home page is the only page in the app that carries the block.

Verified locally against a production build (`next build` then `next start`), by fetching
the rendered HTML and parsing the block — not by reading the source:

```
GET http://127.0.0.1:39311/           -> 1 ld+json block
GET /legal/privacy, /docs, /mcp, /brand -> 0

{"@context":"https://schema.org","@type":"WebSite","name":"JomForm",
 "alternateName":"jomform.com","url":"https://jomform.com/"}
```

## What Google does with it

Google's site-name system is **fully automated**. There is no form, account setting or
support request that forces the name it prints — from Google's documentation:

> While we can't manually change automatically selected site names, you can indicate
> alternatives for our automated system to consider.

So this change states the preference with the strongest signal Google accepts. It does not
guarantee the outcome, and after deploying it still needs a Google recrawl plus processing
time before any change is visible. `alternateName` is the fallback Google considers when it
is not confident about the preferred name — it is written in lower case because Google's
docs say a domain written `Example.com` is not detected as a site-name preference.

## The verification detail worth knowing

Site names are **not** shown by the Rich Results Test — Google's own documentation says so
and points at the Schema Markup Validator instead. A clean Rich Results Test run would not
have proven anything here.

## The other half of the report — the paid row

The reported screenshot shows two different rows, and only one of them is this work:

- The **Sponsored** unit at the top of the screenshot sits above a *"Hide sponsored
  result"* button. Its title and description are ad copy, and its name line is the
  advertiser's business name.
- The rows below the button are **organic** results. Those use the site name line
  described above.

Business names in ads come from Advertiser verification (Admin → Policy → Account in
Google Ads), where Google requires the name to match the verified domain name or legal
name — `JomForm` matches neither, so the account is currently showing `jomform.com`.
Changing that is an account action, not a code change in this repository.
