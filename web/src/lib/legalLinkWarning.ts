// Card t_e8b4f7da — the sentence a legal-URL field shows when the PUBLIC FOOTER
// will not publish that link.
//
// THE DEFECT THIS EXISTS FOR
//   Saving a legal URL the footer drops was confirmed as success: PUT 200,
//   "Footer saved ✓", the value stored verbatim — and no footer link on the
//   public form, because the footer refuses to emit a link the public route
//   would 404 (`https://abc.jomform.com/privacy/`, or the canonical path with no
//   saved page text). The merchant was never told, so they could ship a form
//   with no privacy/terms/refund link and believe otherwise.
//
// WHY THIS MODULE IS THE ONLY PLACE THE DECISION LIVES
//   The HOST/PATH/CONTENT rule is the footer's, and it stays in the API
//   (`rewriteHostedLegalURL` → `classifyHostedLegalURL` → `servableHostedLegalLink`
//   in api/internal/httpapi/public.go). The API answers every settings GET/PUT
//   with `legal_link_status`, one verdict per field. This module only maps that
//   verdict to dictionary keys — it must never test the URL itself, because a
//   second copy of the rule is exactly how the dashboard and the footer end up
//   disagreeing.
//
// `shown: false` with NO `reason` is a field the merchant left empty: an absent
// link is not a warning.

export type LegalLinkStatus = {
  shown: boolean;
  /** "" / absent when shown; else why the footer dropped the link. */
  reason?: "hosted_path_not_served" | "hosted_page_has_no_content";
  page_type?: string;
  /** The exact own-host URL the public route serves for that page. */
  served_url?: string;
  /** False when the hosted page has no saved text — "write the page". */
  has_content?: boolean;
};

export type LegalLinkField = "privacy_url" | "terms_url" | "refund_url";
export type LegalLinkStatusMap = Partial<Record<LegalLinkField, LegalLinkStatus>>;

export type LegalLinkWarningKey = "set.sfLegalLinkNotServed" | "set.sfLegalLinkNoContent";

/** One line to render: a dictionary key plus its interpolation values. */
export type LegalLinkWarning = { key: LegalLinkWarningKey; vars: { served: string } };

/**
 * The warning lines for one field, in reading order. Empty when there is
 * nothing to warn about.
 *
 * TWO lines can apply at once — a non-canonical path AND a page with no content.
 * They are separate facts: correcting the URL alone still shows nothing while the
 * page is empty, and a merchant told only "use this URL instead" would correct it
 * and conclude the feature is broken.
 */
export function legalLinkWarningLines(st?: LegalLinkStatus): LegalLinkWarning[] {
  if (!st || st.shown || !st.reason) return [];
  const vars = { served: st.served_url || "" };
  const lines: LegalLinkWarning[] = [];
  if (st.reason === "hosted_path_not_served") {
    lines.push({ key: "set.sfLegalLinkNotServed", vars });
  }
  if (!st.has_content) {
    lines.push({ key: "set.sfLegalLinkNoContent", vars });
  }
  return lines;
}
