package httpapi

// Issue #379 regression test: the space/custom-domain asset route
// (handleAsset) and the /media code-asset path (handleMedia) must reject a
// key that belongs to a DIFFERENT account than the requesting space/domain.
// A merchant must never be able to serve another account's uploaded code
// asset on their own domain, even if they know the target's object key.
//
// Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL); skipped
// when unset. Uses a fake object store so no live S3/MinIO is required.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/wzul/jomform/api/internal/dbstore"
)

// fakeObjectStore is an in-memory ObjectStoreIface for the tenant-ownership
// regression tests. It returns the stored bytes + content-type for any key.
type fakeObjectStore struct {
	objects map[string]fakeObject
}

type fakeObject struct {
	body []byte
	ct   string
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string]fakeObject{}}
}

func (f *fakeObjectStore) GetObject(_ context.Context, key string) ([]byte, string, error) {
	o, ok := f.objects[key]
	if !ok {
		return nil, "", &types.NoSuchKey{}
	}
	return o.body, o.ct, nil
}
func (f *fakeObjectStore) PutObject(_ context.Context, key, ct string, body []byte) error {
	f.objects[key] = fakeObject{body: body, ct: ct}
	return nil
}
func (f *fakeObjectStore) PublicURL(key string) string { return "https://jomform.com/media/" + key }
func (f *fakeObjectStore) DeleteObject(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

// assetTenancyFixture seeds a user with TWO spaces, each owning its own
// account (issue #158), and returns the handlers plus both space slugs and
// both account ids. The fake store is preloaded with a code asset (CSS) and
// an image (PNG) under account A's key.
func assetTenancyFixture(t *testing.T) (*PublicHandlers, *RESTHandlers, string, string, string, string) {
	t.Helper()
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping asset-tenancy test")
	}
	ctx := context.Background()
	pool := bundleTestPool(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ('ast-' || md5(random()::text) || '@example.test', 'it')
		 RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	sp := dbstore.NewSpaceStore(pool)
	spaceA, err := sp.Create(ctx, userID, "Asset A", "ast-a-"+userID[:8])
	if err != nil {
		t.Fatalf("create space A: %v", err)
	}
	spaceB, err := sp.Create(ctx, userID, "Asset B", "ast-b-"+userID[:8])
	if err != nil {
		t.Fatalf("create space B: %v", err)
	}
	accA := accountIDForSpace(t, ctx, pool, spaceA.ID)
	accB := accountIDForSpace(t, ctx, pool, spaceB.ID)
	if accA == "" || accB == "" || accA == accB {
		t.Fatalf("spaces must each own a distinct account (A=%q B=%q)", accA, accB)
	}

	store := newFakeObjectStore()
	// Account A's code asset (CSS) + image (PNG).
	_ = store.PutObject(ctx, "u/"+accA+"/aaaa.css", "text/css; charset=utf-8", []byte("body{}"))
	_ = store.PutObject(ctx, "u/"+accA+"/aaaa.png", "image/png", []byte("png"))

	accounts := dbstore.NewAccountStore(pool)
	ph := &PublicHandlers{
		Accounts: accounts,
		Domains:  dbstore.NewDomainStore(pool),
		Spaces:   sp,
		Store:    store,
	}
	rh := &RESTHandlers{
		Accounts: accounts,
		Domains:  dbstore.NewDomainStore(pool),
		Spaces:   sp,
		Store:    store,
	}
	return ph, rh, spaceA.Slug, spaceB.Slug, accA, accB
}

// publicAssetServe routes GET /assets/{key} through the public mux on the
// given host.
func publicAssetServe(t *testing.T, h *PublicHandlers, host, key string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/assets/"+key, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// restMediaServe routes GET /media/{key} through the REST mux on the given
// host.
func restMediaServe(t *testing.T, h *RESTHandlers, host, key string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/media/"+key, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHandleAssetRejectsCrossTenantKey: an asset key from account A requested
// on account B's space subdomain must 404 (issue #379).
func TestHandleAssetRejectsCrossTenantKey(t *testing.T) {
	ph, _, slugA, slugB, accA, _ := assetTenancyFixture(t)

	// Same-tenant: account A's asset on account A's space subdomain → 200.
	rec := publicAssetServe(t, ph, slugA+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("same-tenant asset on %s = %d, want 200 (body %s)", slugA+".jomform.com", rec.Code, rec.Body.String())
	}

	// Cross-tenant: account A's asset on account B's space subdomain → 404.
	rec = publicAssetServe(t, ph, slugB+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant asset on %s = %d, want 404 (body %s)", slugB+".jomform.com", rec.Code, rec.Body.String())
	}
}

// TestHandleAssetRejectsUnknownSpace: an asset on a space subdomain that does
// not exist must 404 (the requesting tenant cannot be resolved).
func TestHandleAssetRejectsUnknownSpace(t *testing.T) {
	ph, _, _, _, accA, _ := assetTenancyFixture(t)
	rec := publicAssetServe(t, ph, "no-such-space.jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("asset on unknown space = %d, want 404", rec.Code)
	}
}

// TestHandleMediaCodeAssetRejectsCrossTenant: a code asset from account A
// requested on account B's space subdomain via /media must 404 (issue #379).
// Images remain public and are served cross-tenant.
func TestHandleMediaCodeAssetRejectsCrossTenant(t *testing.T) {
	_, rh, slugA, slugB, accA, _ := assetTenancyFixture(t)

	// Same-tenant code asset on /media → 200.
	rec := restMediaServe(t, rh, slugA+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("same-tenant code asset on /media = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// Cross-tenant code asset on /media → 404.
	rec = restMediaServe(t, rh, slugB+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant code asset on /media = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}

	// Images are intentionally public: account A's image on account B's space
	// subdomain via /media → 200.
	rec = restMediaServe(t, rh, slugB+".jomform.com", "u/"+accA+"/aaaa.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-tenant image on /media = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
