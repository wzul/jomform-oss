// t_b3991178 — naming the ONE media row behind a stored url.
//
// A dashboard page stores a media URL, not a media id: /form-settings keeps
// `logo_url` and nothing else. To apply the shared code-asset rule
// (lib/mediaThumb.ts) it needs that url's MIME, and until this card the only
// client-side read it had was the picker's own library page —
// `/api/rest/media?limit=100`, newest first. So a logo that is NOT on that page
// was "unknown": a row older than the newest 100 rows, or a row whose
// advertised host changed after it was picked (the stored url then equals no
// row's advertised url at all). Unknown MIME is rendered as an <img>, which for
// a code asset on the tenant host (CORP: same-origin) is a broken tile with
// HTTP 200 and nothing in the console — the exact silent failure.
//
// `GET /api/rest/media?url=<url>` moves the question to the server, which
// resolves the url to the object key and reads the row by key. The page window
// and the advertised host are then irrelevant: the answer is "this account's
// row for this object, if there is one".
//
// `null` here means "not a library row" (a pasted https://cdn.example link, or
// a read that failed) and it is NOT an error — the caller keeps its own
// fallback, which is the right answer for an external image. What the caller
// must not do is infer the MIME from the url's extension: that would be another
// copy of the rule that already drifted once (see lib/mediaThumb.ts).

import type { SelectedMedia } from "./mediaThumb";

/** The query for the single-row read. Encoded, so a url carrying `&` or `?`
 *  cannot splice a second query parameter into the request. */
export function mediaLookupPath(url: string): string {
  return `/api/rest/media?url=${encodeURIComponent(url)}`;
}

/**
 * Read the row (mime, preview) out of a `?url=` response.
 *
 * The endpoint answers with the LIST envelope — `items` holds 0 or 1 row — so a
 * caller that already parses the library list needs no second schema. An empty
 * `items` is the ordinary "not a library row" case, not a failure.
 */
export function mediaRowFromLookup(payload: unknown): SelectedMedia | null {
  if (!payload || typeof payload !== "object") return null;
  const items = (payload as { items?: unknown }).items;
  if (!Array.isArray(items) || items.length === 0) return null;
  const row = items[0];
  if (!row || typeof row !== "object") return null;
  const mime = (row as { mime?: unknown }).mime;
  if (typeof mime !== "string" || !mime) return null;
  const preview = (row as { preview?: unknown }).preview;
  return typeof preview === "string" && preview ? { mime, preview } : { mime };
}
