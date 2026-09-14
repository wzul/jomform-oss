package dbstore

import (
	"context"
	"testing"
)

// TestMediaStoreTotalBytes (issue #384): TotalBytes sums the account's
// non-deleted media sizes, and soft-deleting a row releases its bytes back
// into the quota. Skipped unless JOMFORM_TEST_DATABASE_URL is set.
func TestMediaStoreTotalBytes(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)

	acc := uniqueAccount(t, ctx, p)
	other := uniqueAccount(t, ctx, p)

	// Seed three media rows for the account (sizes 100, 200, 300).
	ids := make([]string, 0, 3)
	for _, size := range []int64{100, 200, 300} {
		m := &Media{
			AccountID: acc, Key: "u/" + acc + "/" + randSuffix() + ".png",
			URL: "https://media.test/u/" + acc + "/x.png", Mime: "image/png",
			Size: size, Source: MediaSourceMerchant,
		}
		if err := ms.Create(ctx, m); err != nil {
			t.Fatalf("create media: %v", err)
		}
		ids = append(ids, m.ID)
	}
	// A row for a different account must not leak into acc's total.
	om := &Media{
		AccountID: other, Key: "u/" + other + "/" + randSuffix() + ".png",
		URL: "https://media.test/u/" + other + "/x.png", Mime: "image/png",
		Size: 999999, Source: MediaSourceMerchant,
	}
	if err := ms.Create(ctx, om); err != nil {
		t.Fatalf("create other media: %v", err)
	}

	total, err := ms.TotalBytes(ctx, acc)
	if err != nil {
		t.Fatalf("total bytes: %v", err)
	}
	if total != 600 {
		t.Fatalf("total = %d, want 600", total)
	}

	// Soft-delete one row (200 bytes) → quota released.
	delKey, delSize, ok, err := ms.SoftDelete(ctx, acc, ids[1])
	if err != nil || !ok {
		t.Fatalf("soft delete: ok=%v err=%v", ok, err)
	}
	if delSize != 200 {
		t.Fatalf("soft delete returned size %d, want 200 (issue #545: delete must release the right bytes)", delSize)
	}
	if delKey == "" {
		t.Fatal("soft delete returned an empty key")
	}
	total, err = ms.TotalBytes(ctx, acc)
	if err != nil {
		t.Fatalf("total bytes after delete: %v", err)
	}
	if total != 400 {
		t.Fatalf("total after delete = %d, want 400", total)
	}
}
