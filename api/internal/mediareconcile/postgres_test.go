package mediareconcile

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/testdb"
)

// seedRefs creates one record on every reference surface, all naming uuid, plus
// an account/workspace chain so the foreign keys are satisfied.
func seedRefs(t *testing.T, pool *pgxpool.Pool, uuid string) (accountID string) {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, avatar_url)
		 VALUES ('mrec-' || md5(random()::text) || '@example.test', 'x',
		         'https://jomform.com/media/u/acct/`+uuid+`.jpg')
		 RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 VALUES ($1, 'Reconcile Store', 'mrec-' || md5(random()::text)) RETURNING id::text`,
		userID).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 VALUES ($1, 'Reconcile', 'mrec-' || md5(random()::text)) RETURNING id::text`,
		userID).Scan(&workspaceID); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_settings (workspace_id, landing_page_json, menu_json)
		 VALUES ($1, '{"hero":{"banner_image":"https://x/`+uuid+`.jpg"}}',
		            '[{"name":"Item","image":"https://x/`+uuid+`-thumbnail.jpg"}]')`,
		workspaceID); err != nil {
		t.Fatalf("workspace settings: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_legal_pages (workspace_id, page_type, content)
		 VALUES ($1, 'privacy', 'see https://x/`+uuid+`.jpg')`, workspaceID); err != nil {
		t.Fatalf("legal page: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO forms (account_id, slug, title, config, theme, logo_url)
		 VALUES ($1, 'mrec-' || md5(random()::text), 'Reconcile form',
		         '{"blocks":[{"type":"image","src":"https://x/`+uuid+`-full.jpg"}]}',
		         '{"og_image":"https://x/`+uuid+`-medium.jpg"}',
		         'https://x/`+uuid+`-thumbnail.jpg')`, accountID); err != nil {
		t.Fatalf("form: %v", err)
	}
	var productID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO products (account_id, name, price_sen, images)
		 VALUES ($1, 'Reconcile product', 100, '["https://x/`+uuid+`.jpg"]') RETURNING id::text`,
		accountID).Scan(&productID); err != nil {
		t.Fatalf("product: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO product_variations (product_id, name, price_sen, images)
		 VALUES ($1, 'Default', 100, '["https://x/`+uuid+`-full.jpg"]')`, productID); err != nil {
		t.Fatalf("variation: %v", err)
	}
	return accountID
}

// TestReferencesCoversEverySurface is the safety property the whole tool rests
// on: a row-less group is only "unreferenced" if EVERY place the product can
// store a media URL was actually searched. The test seeds one hit per surface and
// requires one hit per surface back, so a surface dropped from the list (or a
// query pointing at the wrong column) fails here rather than authorising a
// delete in production.
func TestReferencesCoversEverySurface(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	const uuid = "05ab4de5-24b2-4712-8443-42e384e58584"
	seedRefs(t, pool, uuid)

	store := NewPGStoreFromPool(pool)
	refs, err := store.References(ctx, []string{uuid, "197cf316-5bc9-4f8e-9e98-a6b285593651"})
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := map[string]int{}
	for _, h := range refs[uuid] {
		got[h.Surface]++
		if h.RecordID == "" {
			t.Fatalf("surface %s returned an empty record id", h.Surface)
		}
	}
	for _, surface := range Surfaces() {
		if got[surface] == 0 {
			t.Errorf("surface %s did not report a reference that exists in the fixture", surface)
		}
	}
	if len(refs["197cf316-5bc9-4f8e-9e98-a6b285593651"]) != 0 {
		t.Errorf("unreferenced uuid reported hits: %+v", refs["197cf316-5bc9-4f8e-9e98-a6b285593651"])
	}
}

func TestCreateMediaRowAndMediaRowsIncludeTrashed(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	const uuid = "7911b2bf-049e-4496-8747-6477ea897b32"
	accountID := seedRefs(t, pool, uuid)
	store := NewPGStoreFromPool(pool)

	key := "u/" + accountID + "/" + uuid + ".jpg"
	created, err := store.CreateMediaRow(ctx, Row{
		AccountID: accountID,
		Key:       key,
		URL:       "https://jomform.com/media/" + key,
		Mime:      MimeForExt(key),
		Size:      12123,
	})
	if err != nil {
		t.Fatalf("CreateMediaRow: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("created row missing id/created_at: %+v", created)
	}
	if created.Mime != "image/jpeg" {
		t.Fatalf("mime = %q", created.Mime)
	}

	// A trashed row is still a row and must be reported by MediaRows, because the
	// reconciler must never classify it as "no row".
	if _, err := pool.Exec(ctx, `UPDATE media SET deleted_at = now() WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("trash row: %v", err)
	}
	rows, err := store.MediaRows(ctx)
	if err != nil {
		t.Fatalf("MediaRows: %v", err)
	}
	var found *Row
	for i := range rows {
		if rows[i].ID == created.ID {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("created row not returned by MediaRows (%d rows)", len(rows))
	}
	if found.DeletedAt == nil {
		t.Fatal("trashed row came back with a nil deleted_at")
	}
	if found.Size != 12123 {
		t.Fatalf("size = %d", found.Size)
	}
}

func TestSetMediaUsedBytesReturnsPreviousValue(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	accountID := seedRefs(t, pool, "e4914af1-f27d-4a6f-8a0e-683fd6eec0b7")
	store := NewPGStoreFromPool(pool)

	before, err := store.SetMediaUsedBytes(ctx, accountID, 5000)
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	if before != 0 {
		t.Fatalf("before = %d, want 0", before)
	}
	before, err = store.SetMediaUsedBytes(ctx, accountID, 1234)
	if err != nil {
		t.Fatalf("second set: %v", err)
	}
	if before != 5000 {
		t.Fatalf("before = %d, want 5000", before)
	}
	accts, err := store.Accounts(ctx)
	if err != nil {
		t.Fatalf("Accounts: %v", err)
	}
	for _, a := range accts {
		if a.ID == accountID && a.Counter != 1234 {
			t.Fatalf("counter = %d, want 1234", a.Counter)
		}
	}
}

// TestAssertReadOnlyProvesServerSideEnforcement drives the probe in both
// directions: a read-only store must pass it, and a writable store must FAIL it
// (otherwise the probe proves nothing).
func TestAssertReadOnlyProvesServerSideEnforcement(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	var searchPath string
	if err := pool.QueryRow(ctx, `SHOW search_path`).Scan(&searchPath); err != nil {
		t.Fatalf("show search_path: %v", err)
	}
	dsn := os.Getenv(testdb.DSNEnv)

	ro, err := NewPGStoreWithOptions(ctx, Options{DSN: dsn, SearchPath: searchPath, ReadOnly: true})
	if err != nil {
		t.Fatalf("open read-only store: %v", err)
	}
	defer ro.Close()
	if err := ro.AssertReadOnly(ctx); err != nil {
		t.Fatalf("read-only store failed the probe: %v", err)
	}

	writable := NewPGStoreFromPool(pool)
	err = writable.AssertReadOnly(ctx)
	if err == nil {
		t.Fatal("writable store passed the read-only probe: the probe cannot detect an unsafe session")
	}
	if !strings.Contains(err.Error(), "accepted a write") {
		t.Fatalf("writable probe error = %v, want the accepted-a-write message", err)
	}
}
