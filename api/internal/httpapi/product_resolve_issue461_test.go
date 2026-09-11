package httpapi

// Issue #461: product display data (name/price_sen/images) and the charged
// price must resolve LIVE from the products table, keyed by product_id, with
// the form config snapshot as a fallback only for an archived/missing product.
// Pure unit test — no DB.

import (
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

func TestProductResolverPrefersLiveProduct(t *testing.T) {
	// The form snapshot is stale: no images, old name, old price.
	snapshots := []productSnapshot{
		{ID: "p1", Name: "Nasi Lemak", PriceSen: 1800},
		{ID: "p2", Name: "Teh Tarik", PriceSen: 400, Images: []string{"https://cdn.example/teh-old.jpg"}},
	}
	// The DB has the current values: an image uploaded AFTER the form save, a
	// renamed + re-priced product.
	live := []*dbstore.Product{
		{ID: "p1", Name: "Nasi Lemak Ayam Basmathi", PriceSen: 2100,
			Images: []string{"https://cdn.example/nasi.jpg", "https://cdn.example/nasi-2.jpg"}},
		{ID: "p2", Name: "Teh Tarik Kaw", PriceSen: 500},
	}
	r := newProductResolver(snapshots, live)

	name, price, image, ok := r.resolve("p1")
	if !ok {
		t.Fatal("p1 should resolve")
	}
	if name != "Nasi Lemak Ayam Basmathi" {
		t.Errorf("name = %q, want the live name", name)
	}
	if price != 2100 {
		t.Errorf("price = %d, want the live 2100", price)
	}
	if image != "https://cdn.example/nasi.jpg" {
		t.Errorf("image = %q, want the live first image", image)
	}

	// A live product with no images must NOT fall back to a stale snapshot
	// image — clearing the product's image clears the rendered thumbnail.
	name2, price2, image2, ok2 := r.resolve("p2")
	if !ok2 || name2 != "Teh Tarik Kaw" || price2 != 500 {
		t.Errorf("p2 = (%q,%d,%v), want live name/price", name2, price2, ok2)
	}
	if image2 != "" {
		t.Errorf("p2 image = %q, want \"\" (live product has no images)", image2)
	}
}

func TestProductResolverFallsBackToSnapshotWhenArchivedOrMissing(t *testing.T) {
	snapshots := []productSnapshot{
		{ID: "archived", Name: "Retired Item", PriceSen: 900, Images: []string{"https://cdn.example/old.jpg"}},
		{ID: "live", Name: "Live Item", PriceSen: 1000},
	}
	// The archived product is absent from the ACTIVE live list (archived/deleted
	// rows are not returned), so the snapshot keeps the form from blanking.
	live := []*dbstore.Product{{ID: "live", Name: "Live Item v2", PriceSen: 1100}}

	r := newProductResolver(snapshots, live)

	name, price, image, ok := r.resolve("archived")
	if !ok {
		t.Fatal("archived product should still resolve from the snapshot")
	}
	if name != "Retired Item" || price != 900 || image != "https://cdn.example/old.jpg" {
		t.Errorf("archived fallback = (%q,%d,%q), want the snapshot values", name, price, image)
	}

	// A product neither live nor snapshotted is unknown (submit path rejects it).
	if _, _, _, ok := r.resolve("ghost"); ok {
		t.Error("unknown product id must not resolve")
	}
}

func TestProductResolverNilLiveProductsDegradesToSnapshot(t *testing.T) {
	// Bare-mode / DB-unavailable: no live products loaded. The snapshot must
	// still drive rendering (pre-#461 behavior), never blank the form.
	r := newProductResolver([]productSnapshot{{ID: "p1", Name: "Snap", PriceSen: 1234}}, nil)
	name, price, _, ok := r.resolve("p1")
	if !ok || name != "Snap" || price != 1234 {
		t.Errorf("snapshot-only resolve = (%q,%d,%v), want (Snap,1234,true)", name, price, ok)
	}
}

func TestConfigProductSnapshotsParsesBuilderConfig(t *testing.T) {
	cfg := map[string]any{
		"blocks": []any{map[string]any{"type": "product", "product_id": "p1"}},
		"products": []any{
			map[string]any{"id": "p1", "name": "Tee", "price_sen": 1000, "images": []any{"https://cdn.example/tee.jpg"}},
			map[string]any{"id": "p2", "name": "Cap", "price_sen": 500},
		},
	}
	snaps := configProductSnapshots(cfg)
	if len(snaps) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snaps))
	}
	if snaps[0].ID != "p1" || snaps[0].Name != "Tee" || snaps[0].PriceSen != 1000 || len(snaps[0].Images) != 1 {
		t.Errorf("snapshot[0] = %+v", snaps[0])
	}
	if snaps[1].Images != nil {
		t.Errorf("snapshot[1].Images = %v, want nil (no images key)", snaps[1].Images)
	}
	// A config without products (response forms) yields nil, not a panic.
	if got := configProductSnapshots(map[string]any{"blocks": []any{}}); got != nil {
		t.Errorf("no-products config = %v, want nil", got)
	}
}
