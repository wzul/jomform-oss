// The form-settings "Print stand" link must reach the MERCHANT'S OWN stand, not
// the public one.
//
// THE DEFECT THIS PINS (card t_fde19a1d)
//   The public route /qr/{slug}/stand served a printable page carrying the form's
//   TITLE at 200 while the form's own page answered 404 for a draft — an
//   unpublished form's identity, published. That route now applies the page's
//   status rule (draft -> 404, private -> that form's unlock cookie only).
//
//   Gating it alone would have taken the print artefact away from the merchant,
//   who is usually printing a stand BEFORE the form goes live. So the settings
//   page opens the authed route instead:
//
//     GET /api/rest/forms/{id}/qr-stand   (proxied to /v1/rest/forms/{id}/qr-stand)
//
//   which serves the same document (built by the same Go template) for a form the
//   caller owns, at any status.
//
// WHY THE SCOPE MATTERS AS MUCH AS THE PATH (measured in a browser, not inferred)
//   The first version of this link was a bare path. With a session and no
//   ?workspace=<slug>, the API resolves the caller to their FIRST account
//   (rest.go resolve, issue #158) — so a merchant with more than one workspace
//   opened a stand link for a form in another workspace and got the API's own 404
//   body in the tab. Measured against a two-account session: bare path -> 404
//   `{"code":"request.notFound","error":"tidak dijumpai"}`, workspaceUrl(path) ->
//   the stand, title and Table badge rendered.
//
// WHAT THIS TEST CAN AND CANNOT SEE
//   It reads SOURCE, so it cannot prove the link renders or that the route
//   answers — the browser run above did that, and the Go tests own the route's
//   behaviour. What source can pin is the shape that regressed and would silently
//   come back: the public path, the missing scope, and a link that is not opened
//   in its own tab.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const PAGE = readFileSync(join(HERE, "..", "app", "form-settings", "page.tsx"), "utf8");

/** The Print stand <a …> block: from its own opening tag to its closing tag. */
function printStandAnchor(): string {
  const at = PAGE.indexOf("/qr-stand");
  assert.ok(at > 0, "the Print stand link is gone from form-settings — this guard no longer guards anything");
  const start = PAGE.lastIndexOf("<a", at);
  const end = PAGE.indexOf("</a>", at);
  assert.ok(start > 0 && end > at, "could not isolate the Print stand anchor in form-settings");
  return PAGE.slice(start, end);
}

test("the Print stand link opens the authed REST route, never the public /qr/{slug}/stand", () => {
  const anchor = printStandAnchor();
  assert.match(
    anchor,
    /href=\{workspaceUrl\(/,
    "the Print stand href is not built through workspaceUrl(): without ?workspace=<slug> the API " +
      "resolves a session to the caller's FIRST account (rest.go resolve), so a merchant with more " +
      "than one workspace gets the API's 404 body in the tab instead of the stand",
  );
  assert.ok(
    anchor.includes("/api/rest/forms/${formId}/qr-stand"),
    "the Print stand link must target /api/rest/forms/{id}/qr-stand — the authed route that serves " +
      "the stand at any status for the form's own account",
  );
  assert.ok(
    !anchor.includes("/stand`") && !anchor.includes("/qr/${"),
    "the Print stand link points at the PUBLIC /qr/{slug}/stand route again: that route applies the " +
      "form page's status rule, so a draft's stand answers 404 there and the merchant cannot print it",
  );
  assert.ok(
    anchor.includes('target="_blank"'),
    "the stand is a printable document, not a dashboard view: it must open in its own tab",
  );
});

test("the table number is carried into the stand's QR", () => {
  const anchor = printStandAnchor();
  assert.ok(
    anchor.includes("?table=") && anchor.includes("encodeURIComponent(qrTable.trim())"),
    "the stand link must carry the table number the merchant typed, percent-encoded, so the printed " +
      "QR opens the form with ?table=N",
  );
});
