package httpapi

// Issue #384: per-space 1GB storage quota. An upload that would push the
// account's stored media over the cap is rejected with 413 and a clear
// message. Integration test: needs real Postgres (JOMFORM_TEST_DATABASE_URL);
// skipped when unset. Uses a fake object store so no live S3/MinIO is needed.

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
)

// uploadQuotaFixture seeds a user + account and returns handlers wired with a
// fake object store + a real MediaStore, plus the account id and an API key.
func uploadQuotaFixture(t *testing.T) (*RESTHandlers, string, string) {
	t.Helper()
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)

	h := newBundleHandlers(pool)
	h.Store = newFakeObjectStore()
	h.Media = dbstore.NewMediaStore(pool)

	raw := bundleAPIKey(t, ctx, pool, accountID)
	return h, accountID, raw
}

// quotaUpload issues a multipart POST /v1/rest/uploads/image with the given
// bytes and returns the status code + body.
func quotaUpload(t *testing.T, h *RESTHandlers, rawKey string, body []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "x.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/rest/uploads/image", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+rawKey)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestUploadRejectsOverQuota (issue #384): seed the account's media near the
// 1GB cap, then an upload that would push it over must be rejected with 413
// and the quota message. A small upload that stays under the cap succeeds.
func TestUploadRejectsOverQuota(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw := uploadQuotaFixture(t)

	// Seed media so the account is just under the 1GB cap (1GB - 1 byte), so
	// even a tiny stored artifact pushes it over.
	ms := dbstore.NewMediaStore(bundleTestPool(t))
	seed := &dbstore.Media{
		AccountID: accountID, Key: "u/" + accountID + "/seed.png",
		URL: "https://media.test/u/" + accountID + "/seed.png", Mime: "image/png",
		Size: objstore.MaxSpaceBytes - 1, Source: dbstore.MediaSourceMerchant,
	}
	if err := ms.Create(ctx, seed); err != nil {
		t.Fatalf("seed media: %v", err)
	}

	// A 2MB PNG upload would push the space over 1GB → 413 + quota message.
	code, body := quotaUpload(t, h, raw, tinyPNG(t))
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-quota upload = %d, want 413 (body %s)", code, body)
	}
	if !bytes.Contains([]byte(body), []byte("1GB")) {
		t.Fatalf("over-quota body %q must mention the 1GB limit", body)
	}
}

// TestUploadUnderQuotaSucceeds (issue #384): with plenty of quota remaining a
// normal upload succeeds (200), proving the quota gate does not block
// legitimate uploads.
func TestUploadUnderQuotaSucceeds(t *testing.T) {
	ctx := context.Background()
	h, accountID, raw := uploadQuotaFixture(t)

	// Seed a small amount of media (1MB) so the account is well under 1GB.
	ms := dbstore.NewMediaStore(bundleTestPool(t))
	seed := &dbstore.Media{
		AccountID: accountID, Key: "u/" + accountID + "/seed.png",
		URL: "https://media.test/u/" + accountID + "/seed.png", Mime: "image/png",
		Size: 1 << 20, Source: dbstore.MediaSourceMerchant,
	}
	if err := ms.Create(ctx, seed); err != nil {
		t.Fatalf("seed media: %v", err)
	}

	code, body := quotaUpload(t, h, raw, tinyPNG(t))
	if code != http.StatusOK {
		t.Fatalf("under-quota upload = %d, want 200 (body %s)", code, body)
	}
}
