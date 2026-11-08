package httpapi

// Issue #379 regression test: the workspace/custom-domain asset route
// (handleAsset) and the /media code-asset path (handleMedia) must reject a
// key that belongs to a DIFFERENT account than the requesting workspace/domain.
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
	"strings"
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

// ObjectExists is the HEAD the media restore path asks before promising a URL.
// Without it here the fake would not satisfy ObjectStoreIface at all, and the
// typed-nil guard (typed_nil_objectstore_test.go) would stop compiling — which
// is how the interface addition stays honest.
func (f *fakeObjectStore) ObjectExists(_ context.Context, key string) (bool, error) {
	_, ok := f.objects[key]
	return ok, nil
}

// assetTenancyFixture seeds a user with TWO workspaces, each owning its own
// account (issue #158), and returns the handlers plus both workspace slugs and
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

	sp := dbstore.NewWorkspaceStore(pool)
	workspaceA, err := sp.Create(ctx, userID, "Asset A", "ast-a-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace A: %v", err)
	}
	workspaceB, err := sp.Create(ctx, userID, "Asset B", "ast-b-"+userID[:8])
	if err != nil {
		t.Fatalf("create workspace B: %v", err)
	}
	accA := accountIDForWorkspace(t, ctx, pool, workspaceA.ID)
	accB := accountIDForWorkspace(t, ctx, pool, workspaceB.ID)
	if accA == "" || accB == "" || accA == accB {
		t.Fatalf("workspaces must each own a distinct account (A=%q B=%q)", accA, accB)
	}

	store := newFakeObjectStore()
	// Account A's code asset (CSS) + image (PNG).
	_ = store.PutObject(ctx, "u/"+accA+"/aaaa.css", "text/css; charset=utf-8", []byte("body{}"))
	_ = store.PutObject(ctx, "u/"+accA+"/aaaa.png", "image/png", []byte("png"))

	accounts := dbstore.NewAccountStore(pool)
	ph := &PublicHandlers{
		Accounts:   accounts,
		Domains:    dbstore.NewDomainStore(pool),
		Workspaces: sp,
		Store:      store,
	}
	rh := &RESTHandlers{
		Accounts:   accounts,
		Domains:    dbstore.NewDomainStore(pool),
		Workspaces: sp,
		Store:      store,
	}
	return ph, rh, workspaceA.Slug, workspaceB.Slug, accA, accB
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
// on account B's workspace subdomain must 404 (issue #379).
func TestHandleAssetRejectsCrossTenantKey(t *testing.T) {
	ph, _, slugA, slugB, accA, _ := assetTenancyFixture(t)

	// Same-tenant: account A's asset on account A's workspace subdomain → 200.
	rec := publicAssetServe(t, ph, slugA+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("same-tenant asset on %s = %d, want 200 (body %s)", slugA+".jomform.com", rec.Code, rec.Body.String())
	}

	// Cross-tenant: account A's asset on account B's workspace subdomain → 404.
	rec = publicAssetServe(t, ph, slugB+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant asset on %s = %d, want 404 (body %s)", slugB+".jomform.com", rec.Code, rec.Body.String())
	}
}

// TestHandleAssetRejectsUnknownWorkspace: an asset on a workspace subdomain that does
// not exist must 404 (the requesting tenant cannot be resolved).
func TestHandleAssetRejectsUnknownWorkspace(t *testing.T) {
	ph, _, _, _, accA, _ := assetTenancyFixture(t)
	rec := publicAssetServe(t, ph, "no-such-workspace.jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("asset on unknown workspace = %d, want 404", rec.Code)
	}
}

// TestHandleAssetUnconfiguredStoreAnswers503Not500 drives GET /assets/{key} all
// the way to the store fetch with a store that reports itself unconfigured, and
// pins the sentinel→503 branch in handleAsset (public.go).
//
// WHY THIS EXISTS
//
//	The typed-nil test (TestHandleAssetTypedNilStoreAnswers503NotPanic) returns
//	at the storeUnavailable guard BEFORE GetObject is ever called, so it cannot
//	reach the errors.Is(err, objstore.ErrNotConfigured) branch. The handleMedia
//	twin is covered at BOTH layers — the {"sentinel", notConfiguredStore{}} case
//	in TestHandleMediaUnconfiguredMessageIsTheGuard drives its branch red when it
//	is removed. handleAsset had no equivalent: the branch was a verified mutation
//	survivor (status value flipped to 500 left the whole package GREEN).
//
//	The tenant-scope check must pass first or the request 404s before the fetch,
//	so this reuses the #379 fixture: a real workspace subdomain resolving to the
//	key's own account.
func TestHandleAssetUnconfiguredStoreAnswers503Not500(t *testing.T) {
	ph, _, slugA, _, accA, _ := assetTenancyFixture(t)

	h := &PublicHandlers{
		Accounts:   ph.Accounts,
		Domains:    ph.Domains,
		Workspaces: ph.Workspaces,
		Store:      notConfiguredStore{},
	}

	rec := publicAssetServe(t, h, slugA+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /assets/<same-tenant key> with an unconfigured store = %d (%s), want 503 %q",
			rec.Code, strings.TrimSpace(rec.Body.String()), "Media storage is not configured")
	}
	if body := rec.Body.String(); !strings.Contains(body, "Media storage is not configured") {
		t.Fatalf("503 body = %q, want the guard's own message %q", body, "Media storage is not configured")
	}
}

// TestHandleMediaCodeAssetRejectsCrossTenant: a code asset from account A
// requested on account B's workspace subdomain via /media must 404 (issue #379).
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

	// Images are intentionally public: account A's image on account B's workspace
	// subdomain via /media → 200.
	rec = restMediaServe(t, rh, slugB+".jomform.com", "u/"+accA+"/aaaa.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-tenant image on /media = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
