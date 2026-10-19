// t_cb4e3d99 — what the /media purge toast is ALLOWED to claim.
//
// THE DEFECT. `DELETE /api/rest/media/{id}/purge` answers with the edge outcome:
//
//   {"status":"purged","media_id":…,"key":…,"edge":"not_configured","edge_max_age_s":300}
//
// (`edge` is one of purged | not_configured | no_urls | out_of_zone | failed;
// `edge_max_age_s` is present exactly when the CDN copy was NOT cleared — see
// api/internal/httpapi/media.go and api/internal/cdnpurge). The page discarded
// that body and printed one constant, "File deleted for good.", so a merchant
// whose CDN credential is unset — i.e. every deployment today — was told the
// file is gone everywhere while the edge could keep serving its copy for up to
// 300 s (and a browser that already fetched it, longer still).
//
// The response and the `media.purged` audit event name the outcome; only the
// surface a merchant actually reads did not. This module is the decision, kept
// out of the component so `node --test` can execute it: given the parsed
// response, which dictionary key (and which parameters) may the toast use.
//
// THE RULE IT ENCODES: the plain "deleted for good" line is only reachable when
// the API said the edge was actually purged. Every other value — including a
// body we could not read at all — falls back to a line that admits a residual
// copy, because silence here is a false claim about a URL the merchant may
// believe they took down.

// The dictionary keys this decision can return. Typed as a union of literals so
// a rename in i18nDict.ts is a compile error at the call site, not a raw key
// rendered to the user at runtime.
export type PurgeNoticeKey = "media.purged" | "media.purgedEdgeStale" | "media.purgedEdgeUnnamed";

export type PurgeNotice = {
  key: PurgeNoticeKey;
  params?: { seconds: number };
};

// The purge response fields this decision reads. Everything is `unknown` on
// purpose: the body is caller-visible JSON and a proxy, an older API build or a
// truncated response must degrade to the honest fallback rather than throw.
type PurgeBody = {
  edge?: unknown;
  edge_max_age_s?: unknown;
};

// Numeric interpretation of `edge_max_age_s`. JSON gives a number, but a
// string is accepted too so a response serialised by another layer still
// produces the real window instead of the vague fallback.
function maxAgeSeconds(raw: unknown): number | null {
  let n = NaN;
  if (typeof raw === "number") n = raw;
  else if (typeof raw === "string" && raw.trim() !== "") n = Number(raw);
  if (!Number.isFinite(n) || n < 0) return null;
  return Math.round(n);
}

// purgeNotice maps a parsed purge response to the toast it may show.
//
//   edge === "purged"                    -> media.purged          (plain line)
//   edge_max_age_s > 0 and edge ≠ purged -> media.purgedEdgeStale (names seconds)
//   edge_max_age_s === 0                 -> media.purged          (no window)
//   anything else / unreadable body      -> media.purgedEdgeUnnamed
//
// A zero window is the only non-`purged` case that earns the plain line: there
// is no residual copy to warn about, so warning would be noise. It is not the
// same as "unknown", which must warn.
export function purgeNotice(body: unknown): PurgeNotice {
  const b: PurgeBody = body !== null && typeof body === "object" ? (body as PurgeBody) : {};
  const edge = typeof b.edge === "string" ? b.edge : "";
  if (edge === "purged") return { key: "media.purged" };
  const seconds = maxAgeSeconds(b.edge_max_age_s);
  if (seconds === 0) return { key: "media.purged" };
  if (seconds !== null) return { key: "media.purgedEdgeStale", params: { seconds } };
  return { key: "media.purgedEdgeUnnamed" };
}
