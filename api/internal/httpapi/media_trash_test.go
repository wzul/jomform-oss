package httpapi

// Media trash (the card's acceptance #1 and #2).
//
// THE DEFECT THIS GUARDS. On main, DELETE /v1/rest/media/{id} marked the row
// deleted AND called Store.DeleteObject in the same handler, so the "soft delete"
// destroyed the bytes. The row said "in the trash" while the object was already
// gone, and a restore could only ever have produced a row pointing at nothing.
//
// So the two assertions that matter are not "a list route exists":
//
//	1. after a soft delete the OBJECT IS STILL IN THE STORE (main fails this —
//	   the fake store's map is empty), and
//	2. the restore brings it back and the same URL still works.
//
// And the quota rule the card asks to be ASSERTED, not documented: the bytes stay
// counted while the row is in the bin and are released by the purge.
//
// Integration against a real Postgres; skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// mediaTrashFixture seeds a user + account, wires the handlers over the test
// pool with a fake object store, and returns the handlers, the account id, the
// media store and the API key.
func mediaTrashFixture(t *testing.T) (*RESTHandlers, *fakeObjectStore, string, *dbstore.MediaStore, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	store := newFakeObjectStore()
	h.Store = store
	ms := dbstore.NewMediaStore(pool)
	h.Media = ms

	return h, store, accountID, ms, bundleAPIKey(t, ctx, pool, accountID)
}

// seedMediaRow inserts a media row for the account and puts its object in the
// fake store, so "the bytes exist" is a real state and not an assumption.
func seedMediaRow(t *testing.T, ctx context.Context, ms *dbstore.MediaStore, store *fakeObjectStore, accountID string, size int64) *dbstore.Media {
	t.Helper()
	m := &dbstore.Media{
		AccountID: accountID,
		Key:       "u/" + accountID + "/" + randSuffix() + ".png",
		URL:       "https://media.test/u/" + accountID + "/x.png",
		Mime:      "image/png",
		Size:      size,
		Source:    dbstore.MediaSourceMerchant,
	}
	if err := ms.Create(ctx, m); err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := store.PutObject(ctx, m.Key, "image/png", []byte("bytes")); err != nil {
		t.Fatalf("put object: %v", err)
	}
	return m
}

// TestMediaTrashKeepsTheObjectAndTheQuotaBite is the card's acceptance #1, #2
// and #4 in one flow, because they are one decision:
//
//	soft delete → row in the bin, OBJECT STILL THERE, bytes still counted
//	restore     → row back, same URL, bytes still counted (no double release)
//	purge       → row gone, OBJECT GONE, bytes released
func TestMediaTrashKeepsTheObjectAndTheQuotaBite(t *testing.T) {
	ctx := context.Background()
	h, store, accountID, ms, key := mediaTrashFixture(t)

	m := seedMediaRow(t, ctx, ms, store, accountID, 4096)
	// The counter starts at the row's bytes, which is the invariant the upload
	// path maintains.
	if _, err := bundleTestPool(t).Exec(ctx,
		`UPDATE accounts SET media_used_bytes=$2 WHERE id=$1`, accountID, 4096); err != nil {
		t.Fatalf("seed quota: %v", err)
	}

	// ── soft delete: the row moves to the bin, the bytes do NOT move.
	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/media/"+m.ID, "")
	if code != http.StatusOK {
		t.Fatalf("DELETE media = %d, want 200 (body %s)", code, raw)
	}
	// THIS IS THE BITE. On main the handler called Store.DeleteObject here, so
	// this assertion fails: the fake store's map no longer holds the key.
	if present, err := store.ObjectExists(ctx, m.Key); err != nil {
		t.Fatalf("ObjectExists: %v", err)
	} else if !present {
		t.Fatal("the object was REMOVED from the store by a soft delete — that is the defect this " +
			"card exists to fix: a \"trash\" whose bytes are already destroyed cannot be restored")
	}
	// And the bytes still count against the 1GB cap: the bin holds real storage.
	if used, err := ms.UsedBytes(ctx, accountID); err != nil {
		t.Fatalf("used bytes: %v", err)
	} else if used != 4096 {
		t.Fatalf("quota after soft delete = %d, want 4096 — a bin that does not count would be "+
			"free storage on a billing boundary", used)
	}
	// The live list must not serve it any more.
	if code, _ := tplReq(t, h, key, http.MethodGet, "/v1/rest/media", ""); code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media = %d, want 200", code)
	}

	// ── the bin lists it, and says the object is still there.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/media/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media/trash = %d, want 200 (body %s)", code, raw)
	}
	items := trashItems(t, raw)
	row := findTrashItem(items, m.ID)
	if row == nil {
		t.Fatalf("the trashed media item is not listed in /v1/rest/media/trash (body %s)", raw)
	}
	if row["object_present"] != true {
		t.Errorf("object_present = %v, want true — the bytes really are still in the store", row["object_present"])
	}
	if row["deleted_at"] == "" || row["deleted_at"] == nil {
		t.Error("the bin row carries no deleted_at, so the UI cannot say when it was trashed")
	}

	// ── restore: same URL, still no quota movement.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/media/"+m.ID+"/restore", "")
	if code != http.StatusOK {
		t.Fatalf("POST restore = %d, want 200 (body %s)", code, raw)
	}
	if present, err := store.ObjectExists(ctx, m.Key); err != nil || !present {
		t.Fatalf("object missing after restore: present=%v err=%v", present, err)
	}
	if used, err := ms.UsedBytes(ctx, accountID); err != nil {
		t.Fatalf("used bytes after restore: %v", err)
	} else if used != 4096 {
		t.Fatalf("quota after restore = %d, want 4096 — trash→restore is quota-neutral by construction", used)
	}
	// The row is live again and reachable through the ordinary read.
	if code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/media", ""); code != http.StatusOK {
		t.Fatalf("GET /v1/rest/media after restore = %d (body %s)", code, raw)
	}
	// And gone from the bin.
	code, raw = tplReq(t, h, key, http.MethodGet, "/v1/rest/media/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET trash after restore = %d", code)
	}
	if findTrashItem(trashItems(t, raw), m.ID) != nil {
		t.Error("a restored media item is still listed in the bin")
	}

	// ── purge: the bytes really go, and this is where the quota comes down.
	if _, _, _, err := ms.SoftDelete(ctx, accountID, m.ID); err != nil {
		t.Fatalf("soft delete for purge: %v", err)
	}
	code, raw = tplReq(t, h, key, http.MethodDelete, "/v1/rest/media/"+m.ID+"/purge", "")
	if code != http.StatusOK {
		t.Fatalf("DELETE purge = %d, want 200 (body %s)", code, raw)
	}
	if present, err := store.ObjectExists(ctx, m.Key); err != nil {
		t.Fatalf("ObjectExists after purge: %v", err)
	} else if present {
		t.Fatal("the object survived a purge — the bytes would linger in the bucket forever")
	}
	if used, err := ms.UsedBytes(ctx, accountID); err != nil {
		t.Fatalf("used bytes after purge: %v", err)
	} else if used != 0 {
		t.Fatalf("quota after purge = %d, want 0 — the purge is the release point", used)
	}
}

// TestMediaPurgeDoesNotDoubleReleaseQuota is the guard for the other direction of
// the release move: the store releases the counter in the same transaction as the
// row delete, and the handler must NOT release it again. A second release would
// subtract the bytes from the account's OTHER media, and because the decrement is
// clamped at zero it would look successful — a silent quota/bucket divergence.
func TestMediaPurgeDoesNotDoubleReleaseQuota(t *testing.T) {
	ctx := context.Background()
	h, store, accountID, ms, key := mediaTrashFixture(t)

	doomed := seedMediaRow(t, ctx, ms, store, accountID, 4096)
	keeper := seedMediaRow(t, ctx, ms, store, accountID, 8192) // a DIFFERENT object
	if _, err := bundleTestPool(t).Exec(ctx,
		`UPDATE accounts SET media_used_bytes=$2 WHERE id=$1`, accountID, 4096+8192); err != nil {
		t.Fatalf("seed quota: %v", err)
	}

	if _, _, _, err := ms.SoftDelete(ctx, accountID, doomed.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/media/"+doomed.ID+"/purge", ""); code != http.StatusOK {
		t.Fatalf("purge = %d (body %s)", code, raw)
	}

	if used, err := ms.UsedBytes(ctx, accountID); err != nil {
		t.Fatalf("used bytes: %v", err)
	} else if used != 8192 {
		t.Fatalf("quota after purging one of two objects = %d, want 8192 (the surviving object's "+
			"bytes) — anything less means the purge released the same bytes twice", used)
	}
	// The surviving object is untouched, which is the same fact from the bucket's side.
	if present, err := store.ObjectExists(ctx, keeper.Key); err != nil || !present {
		t.Fatalf("the unrelated object was affected: present=%v err=%v", present, err)
	}
}

// TestMediaRestoreRefusesWhenTheObjectIsGone covers the case the card calls out
// ("Handle the case where the object was removed by something else in the
// meantime"): the row and the object have independent lives, so a restore must
// ASK rather than assume. Removing the bytes behind the row while it sits in the
// bin is exactly the situation main created on every delete.
func TestMediaRestoreRefusesWhenTheObjectIsGone(t *testing.T) {
	ctx := context.Background()
	h, store, accountID, ms, key := mediaTrashFixture(t)

	m := seedMediaRow(t, ctx, ms, store, accountID, 1024)
	if code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/media/"+m.ID, ""); code != http.StatusOK {
		t.Fatalf("DELETE media = %d (body %s)", code, raw)
	}
	// Something else removes the bytes while the row is in the bin (an operator,
	// a bucket lifecycle rule, a failed deploy). The URL is now dead.
	if err := store.DeleteObject(ctx, m.Key); err != nil {
		t.Fatalf("delete object behind the bin row: %v", err)
	}

	// The bin must say so, not claim the object is present.
	code, raw := tplReq(t, h, key, http.MethodGet, "/v1/rest/media/trash", "")
	if code != http.StatusOK {
		t.Fatalf("GET trash = %d", code)
	}
	if row := findTrashItem(trashItems(t, raw), m.ID); row != nil && row["object_present"] != false {
		t.Errorf("object_present = %v, want false — the bytes are gone and the bin must not claim otherwise",
			row["object_present"])
	}

	// And the restore refuses with 409 instead of handing back a broken URL.
	code, raw = tplReq(t, h, key, http.MethodPost, "/v1/rest/media/"+m.ID+"/restore", "")
	if code != http.StatusConflict {
		t.Fatalf("restore with the object gone = %d, want 409 (body %s) — restoring would put a "+
			"URL that 404s back in the library", code, raw)
	}
	// Nothing changed: the row is still in the bin, not silently live.
	total, err := ms.TotalBytes(ctx, accountID)
	if err != nil {
		t.Fatalf("total bytes: %v", err)
	}
	if total != 1024 {
		t.Errorf("total bytes after a refused restore = %d, want 1024 (the row must still be there)", total)
	}
}

// TestMediaPurgeRefusesALiveRow: the bin is the ONLY way into a permanent delete,
// so a mistyped call cannot destroy bytes that are still being served.
func TestMediaPurgeRefusesALiveRow(t *testing.T) {
	ctx := context.Background()
	h, store, accountID, ms, key := mediaTrashFixture(t)

	m := seedMediaRow(t, ctx, ms, store, accountID, 512)
	code, raw := tplReq(t, h, key, http.MethodDelete, "/v1/rest/media/"+m.ID+"/purge", "")
	if code != http.StatusNotFound {
		t.Fatalf("purge of a LIVE row = %d, want 404 (body %s)", code, raw)
	}
	if present, err := store.ObjectExists(ctx, m.Key); err != nil || !present {
		t.Fatalf("a refused purge destroyed the object: present=%v err=%v", present, err)
	}
	if _, err := ms.ByIDAccount(ctx, accountID, m.ID); err != nil {
		t.Fatalf("the live row is no longer readable: %v", err)
	}
}

// trashItems pulls the rows out of a bin listing body. The trash family answers
// a BARE ARRAY (forms, products and media alike), which is what this asserts —
// a media bin that answered an envelope would have broken the shared shape.
func trashItems(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("bin listing is not a bare array of rows: %v (body %s)", err, raw)
	}
	return items
}

// findTrashItem returns the row with the given id, or nil.
func findTrashItem(items []map[string]any, id string) map[string]any {
	for _, it := range items {
		if it["id"] == id {
			return it
		}
	}
	return nil
}
