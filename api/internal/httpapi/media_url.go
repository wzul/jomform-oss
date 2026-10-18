package httpapi

// The URL the media library HANDS a merchant is not always the URL the object
// store advertises. This file is that decision, in one place, for every REST
// response that carries a media url — the paginated list, the bin, a restore,
// and every upload endpoint (image / asset / avatar / pdf).

import (
	"context"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// merchantMediaURL is the url this account should be given for a stored object:
// `canonical` is what the store advertises (objstore.PublicURL / the row's
// stored url), and the rule itself lives in objstore.MerchantURL so the REST and
// MCP lanes cannot drift:
//
//   - images keep the canonical media-host URL (public, embeddable, unchanged);
//   - a CODE ASSET is advertised on the account's PUBLIC host, because
//     /media serves one only when the request Host resolves to the key's own
//     account — the bare media host resolves to nobody, so the canonical URL
//     used to 404 the asset it had just advertised (issue #379, measured on
//     v1.21.224).
//
// The IsCodeAsset() return below is NOT a second copy of the rule: it skips
// resolving the account's public host (a workspace lookup) for the only case
// that would waste it, since an image's url never changes. Every code asset
// still goes through objstore.MerchantURL.
//
// When the account has no public host (no workspace, no verified custom
// domain) the canonical URL is returned: it is what the merchant got before,
// and inventing a host that cannot serve the bytes would be worse.
func (h *RESTHandlers) merchantMediaURL(ctx context.Context, acc *dbstore.Account, canonical, mime string) string {
	if !objstore.IsCodeAsset(mime) {
		return canonical
	}
	return objstore.MerchantURL(canonical, mime, h.publicHostFor(ctx, acc))
}
