// t_0fe14ba5 — when a media item may be rendered through an <img>.
//
// The url the library advertises for a CODE asset is now on the account's
// PUBLIC host (objstore.MerchantURL), because /media serves one only to a
// request whose Host resolves to the key's own account (issue #379). Code
// assets are also served with `Cross-Origin-Resource-Policy: same-origin`
// (issue #361), so an <img> on the DASHBOARD (jomform.com / 127.0.0.1) pointing
// at <workspace>.jomform.com/media/x.svg is blocked by the browser and renders a
// broken-image icon.
//
// SVG is the trap. `mime.startsWith("image/")` is true for image/svg+xml, so the
// grid used to inline an SVG — and it only ever rendered because the media host
// and the dashboard happened to share an origin. SVG is a code asset here (the
// same allowlist as CSS/JS/HTML/WOFF2), so it gets the same treatment: an
// extension badge, not an <img>.
//
// A PDF's `preview` is still an <img>: that object is a JPEG rendered at upload
// time and stored on the media host, not the document itself.
//
// Keep this in step with objstore.IsCodeAsset on the API side — a mime the API
// treats as a code asset is tenant-scoped there.

// isCodeAssetMime reports whether a stored content-type is a merchant code
// asset (CSS/JS/HTML/SVG/WOFF2), mirroring objstore.IsCodeAsset.
export function isCodeAssetMime(mime: string): boolean {
  const m = mime.split(";")[0].trim().toLowerCase();
  if (m.startsWith("text/")) return true;
  return m === "application/javascript" || m === "image/svg+xml" || m === "font/woff2";
}

// canInlineMediaPreview decides between a real <img> thumbnail and the
// extension badge: an image that is NOT a code asset, or any generated preview
// (a PDF's first page is a JPEG on the media host).
export function canInlineMediaPreview(mime: string, hasPreview: boolean): boolean {
  if (hasPreview) return true;
  if (!mime.startsWith("image/")) return false;
  return !isCodeAssetMime(mime);
}

// The tile label for an item that canInlineMediaPreview() refuses to inline:
// a code file's extension badge (e.g. "SVG", "CSS"), "PDF" for a document, and
// the first four characters of anything else.
//
// This lives here, next to the decision it labels, because it used to be a
// private copy inside app/media/page.tsx — and the picker's copy of the whole
// rule is exactly what drifted (t_04ea7f88): the grid learned to badge an SVG
// and the logo picker kept an <img> on a tenant-host URL. One rule, one label.
export function extBadge(mime: string): string {
  if (mime === "application/pdf") return "PDF";
  if (mime === "text/css; charset=utf-8") return "CSS";
  if (mime === "text/javascript; charset=utf-8" || mime === "application/javascript; charset=utf-8") return "JS";
  if (mime === "text/html; charset=utf-8") return "HTML";
  if (mime === "image/svg+xml") return "SVG";
  if (mime === "font/woff2") return "WOFF2";
  return mime.slice(0, 4).toUpperCase();
}
