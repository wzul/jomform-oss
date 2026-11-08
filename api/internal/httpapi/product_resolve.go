package httpapi

// Issue #461: product display + price data must be resolved LIVE from the DB
// products table (keyed by product_id), not from the form's saved config
// snapshot.
//
// The builder writes a snapshot of each referenced product into the form config
// at SAVE time (`products: [{id,name,price_sen,images}]`). That snapshot drifts
// as soon as the merchant edits the product afterwards — an image uploaded
// later never rendered, and a renamed/re-priced product kept showing (and
// charging, via lookupProduct) the old values. The products table is the source
// of truth: resolve name/price/images from it at render + submit time and keep
// the snapshot as a fallback only for a product row that is archived or gone,
// so a removed product never blanks the form or breaks checkout.
//
// Pricing blocks (option/shipping) are NOT product references: they
// legitimately carry their own price_sen in the config and stay
// snapshot-based (see lookupProduct).

import (
	"context"
	"encoding/json"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// productSnapshot is one product as recorded in the form config at save time.
type productSnapshot struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceSen    int64    `json:"price_sen"`
	Images      []string `json:"images"`
}

// productResolver overlays the live products table over the form's saved
// snapshot. A product that still exists (and is active) resolves its CURRENT
// name/price/images; a product that is archived or missing falls back to the
// snapshot.
type productResolver struct {
	live     map[string]*dbstore.Product
	snapshot map[string]productSnapshot
}

// newProductResolver builds a resolver from the form's snapshot products and
// the form account's live active products. Either side may be empty/nil.
func newProductResolver(snapshots []productSnapshot, live []*dbstore.Product) productResolver {
	r := productResolver{
		live:     make(map[string]*dbstore.Product, len(live)),
		snapshot: make(map[string]productSnapshot, len(snapshots)),
	}
	for _, p := range snapshots {
		if p.ID != "" {
			r.snapshot[p.ID] = p
		}
	}
	for _, p := range live {
		if p != nil && p.ID != "" {
			r.live[p.ID] = p
		}
	}
	return r
}

// resolve returns the current display data for one product id: the live product
// row when the product still exists (active), otherwise the form snapshot.
// ok is false when the id is neither live nor snapshotted (unknown reference).
//
// The returned image is the product's FIRST image, or "" when the live product
// has no images — a cleared image must not fall back to a stale snapshot one.
func (r productResolver) resolve(productID string) (name string, priceSen int64, image string, ok bool) {
	if p, isLive := r.live[productID]; isLive {
		return p.Name, p.PriceSen, firstImage(p.Images), true
	}
	if s, isSnap := r.snapshot[productID]; isSnap {
		return s.Name, s.PriceSen, firstImage(s.Images), true
	}
	return "", 0, "", false
}

// snapshotName returns the product name recorded in the form snapshot at save
// time ("" when the product was never snapshotted). Used to tell an
// auto-populated product label apart from a merchant's explicit per-form
// override.
func (r productResolver) snapshotName(productID string) string {
	return r.snapshot[productID].Name
}

// liveProduct returns the LIVE product row for an id, or nil when the id is not
// a live row of the form's account (a pricing block, an archived product, or a
// snapshot-only reference).
//
// Card t_06851d88: the product block renderer needs the product's TYPE and
// buyer-chosen-amount limits, which the snapshot does not carry. A snapshot-only
// reference (archived product) therefore renders as a fixed-price product — it
// cannot be re-priced by a buyer whose product is gone.
func (r productResolver) liveProduct(productID string) *dbstore.Product {
	return r.live[productID]
}

// firstImage returns the first non-empty image URL, or "".
func firstImage(images []string) string {
	if len(images) > 0 && images[0] != "" {
		return images[0]
	}
	return ""
}

// configProductSnapshots extracts the `products` snapshot array from a form
// config blob. A malformed/missing payload yields nil.
func configProductSnapshots(cfg any) []productSnapshot {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out struct {
		Products []productSnapshot `json:"products"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out.Products
}

// liveProductsForForm loads the form account's active products for live
// resolution. Nil-safe: without a product store (bare-mode tests) it returns
// nil, so callers degrade to the snapshot-only (pre-#461) behavior. A lookup
// error also degrades to the snapshot rather than failing the render/submit.
func (h *PublicHandlers) liveProductsForForm(ctx context.Context, f *dbstore.Form) []*dbstore.Product {
	if h.Products == nil || f == nil || f.AccountID == "" {
		return nil
	}
	ps, err := h.Products.ListActiveByAccount(ctx, f.AccountID)
	if err != nil {
		return nil
	}
	return ps
}

// productMapByID indexes products by id for per-item resolution.
func productMapByID(ps []*dbstore.Product) map[string]*dbstore.Product {
	out := make(map[string]*dbstore.Product, len(ps))
	for _, p := range ps {
		if p != nil && p.ID != "" {
			out[p.ID] = p
		}
	}
	return out
}
