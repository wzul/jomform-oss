package dbstore

import (
	"context"
	"testing"
)

// TestMediaStoreTotalBytes (issue #384, reinterpreted by the trash card): the
// account's storage total is the sum of EVERY media row's size, live AND
// trashed.
//
// THE RULE CHANGED HERE, and this test is where the change is pinned. Before the
// trash card, deleting a media row released its bytes, because deleting it also
// destroyed the object. Now a delete only moves the row to the bin and the object
// stays in the bucket, so the bytes are still the merchant's storage and still
// count against the 1GB cap. A bin that did not count would be a free tier on the
// one number that is a billing boundary.
//
// PRE-FIX this read 400 (the trashed row was excluded) — the assertion below is
// the bite, not a restatement of the implementation.
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set.
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

	// Soft-delete one row (200 bytes). The ROW moves to the bin; the OBJECT does
	// not, and the bytes therefore stay counted.
	delKey, delSize, ok, err := ms.SoftDelete(ctx, acc, ids[1])
	if err != nil || !ok {
		t.Fatalf("soft delete: ok=%v err=%v", ok, err)
	}
	if delSize != 200 {
		t.Fatalf("soft delete returned size %d, want 200 (purge must release the right bytes)", delSize)
	}
	if delKey == "" {
		t.Fatal("soft delete returned an empty key")
	}
	total, err = ms.TotalBytes(ctx, acc)
	if err != nil {
		t.Fatalf("total bytes after soft delete: %v", err)
	}
	if total != 600 {
		t.Fatalf("total after SOFT DELETE = %d, want 600 — the bin still occupies the bucket, "+
			"so its bytes must still count against the 1GB cap (they are released by Purge, not here)", total)
	}

	// Purging the trashed row is what gives the bytes back.
	if _, _, ok, err := ms.Purge(ctx, acc, ids[1]); err != nil || !ok {
		t.Fatalf("purge: ok=%v err=%v", ok, err)
	}
	total, err = ms.TotalBytes(ctx, acc)
	if err != nil {
		t.Fatalf("total bytes after purge: %v", err)
	}
	if total != 400 {
		t.Fatalf("total after PURGE = %d, want 400 — the bytes leave the bucket on purge, so the "+
			"counter must come down there", total)
	}
}

// TestMediaPurgeReleasesQuotaCounterNotJustTotalBytes pins the OTHER half: the
// counter the uplink enforces (accounts.media_used_bytes) must follow the same
// rule as the sum, or a merchant could trash 1GB and then be refused an upload
// they can no longer see the cause of (or worse, keep uploading against bytes
// that are gone).
//
// Skipped unless JOMFORM_TEST_DATABASE_URL is set.
func TestMediaPurgeReleasesQuotaCounterNotJustTotalBytes(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	ms := NewMediaStore(p)
	acc := uniqueAccount(t, ctx, p)

	m := &Media{
		AccountID: acc, Key: "u/" + acc + "/" + randSuffix() + ".png",
		URL: "https://media.test/u/" + acc + "/x.png", Mime: "image/png",
		Size: 4096, Source: MediaSourceMerchant,
	}
	if err := ms.Create(ctx, m); err != nil {
		t.Fatalf("create media: %v", err)
	}
	// The counter is seeded to the row's bytes, which is the invariant the
	// upload path maintains (TotalBytes == media_used_bytes).
	if _, err := p.Exec(ctx, `UPDATE accounts SET media_used_bytes = $2 WHERE id = $1`, acc, 4096); err != nil {
		t.Fatalf("seed quota: %v", err)
	}

	if _, _, ok, err := ms.SoftDelete(ctx, acc, m.ID); err != nil || !ok {
		t.Fatalf("soft delete: ok=%v err=%v", ok, err)
	}
	if used, err := ms.UsedBytes(ctx, acc); err != nil {
		t.Fatalf("used bytes: %v", err)
	} else if used != 4096 {
		t.Fatalf("counter after soft delete = %d, want 4096 (unchanged: the bytes are still stored)", used)
	}

	if _, _, ok, err := ms.Purge(ctx, acc, m.ID); err != nil || !ok {
		t.Fatalf("purge: ok=%v err=%v", ok, err)
	}
	if used, err := ms.UsedBytes(ctx, acc); err != nil {
		t.Fatalf("used bytes: %v", err)
	} else if used != 0 {
		t.Fatalf("counter after purge = %d, want 0 — the purge is the release point, and the "+
			"handler's releaseMediaQuota is the call that does it", used)
	}
}
