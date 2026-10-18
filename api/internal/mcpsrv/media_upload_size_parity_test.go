package mcpsrv

// t_aaff87ac: upload_media and list_media describe the SAME media row, so their
// `size` must be the same number.
//
// Measured on prod v1.21.227: upload_media kind=pdf answered "size": 27617 (the
// bytes sent) while GET /v1/rest/media and list_media answered 32210 for the row
// that same call created — 27617 + the 4593-byte Ghostscript preview it
// generated and counted against the quota. The upload path wrote the row with
// the stored total (base + preview / image variants) and then answered with the
// base byte count, because mediaOutFrom took `size` from its argument and never
// read it back off the row it had just looked up. mediaOutFromRow (used by
// list_media) returns m.Size, hence the divergence.
//
// The card's acceptance shape is exactly what this test does: upload one PDF and
// one PNG through upload_media, then read the SAME ids back through list_media
// and require the size to agree. The byte counts are read from the fake object
// store, so "the stored total" is measured rather than assumed — with gs
// installed the PDF really does gain a preview object, and every raster image
// gains thumbnail/medium/full variants.

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/objstore"
	"github.com/wzul/jomform/api/internal/testdb"
)

// fakeS3 is a path-style, in-memory stand-in for the media bucket. The upload
// pipeline needs a store that accepts PUT and answers HEAD truthfully (the list
// side derives variant/preview URLs by asking the store), and nothing more.
type fakeS3 struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path style: /<bucket>/<key...>
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	key := parts[1]
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.mu.Lock()
		f.objs[key] = body
		f.mu.Unlock()
		w.Header().Set("ETag", fmt.Sprintf("%q", fmt.Sprintf("%d", len(body))))
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		f.mu.Lock()
		body, ok := f.objs[key]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		f.mu.Lock()
		body, ok := f.objs[key]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) size(key string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objs[key]
	return len(body), ok
}

// newFakeStore starts the fake bucket and returns an ObjectStore pointed at it.
// The endpoint host is spelled "localhost" on purpose: objstore.NewObjectStore
// switches the S3 client to path-style addressing for a localhost/minio
// endpoint, and the AWS SDK would otherwise address the bucket as a subdomain.
func newFakeStore(t *testing.T) (*objstore.ObjectStore, *fakeS3) {
	t.Helper()
	f := &fakeS3{objs: map[string][]byte{}}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	_, port, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("httptest url %q: %v", ts.URL, err)
	}
	store, err := objstore.NewObjectStore(
		"http://localhost:"+port, "test-ak", "test-sk",
		"jomform-media", "us-east-1", "https://media.test",
	)
	if err != nil {
		t.Fatalf("NewObjectStore: %v", err)
	}
	return store, f
}

// pdfFixtureBytes is a structurally valid single-page PDF carrying a `{` byte,
// i.e. the ordinary shape of a Chrome "Print to PDF" (the fixture pattern of
// t_adc169da). Ghostscript renders it, so the upload really does generate and
// count a preview.
func pdfFixtureBytes() []byte {
	stream := "BT /F1 12 Tf 20 50 Td ({INVOICE} JF-1001) Tj ET\n"
	objects := []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 100]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>",
		fmt.Sprintf("<</Length %d>>stream\n%s\nendstream", len(stream), stream),
		"<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, body := range objects {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj%s endobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// pngFixtureBytes is a large-enough opaqued PNG that the optimiser keeps it as a
// JPEG and the thumbnail/medium/full variants are generated — the exact shape
// whose bytes the row (and list_media) add to the reported size.
func pngFixtureBytes(t *testing.T) []byte {
	t.Helper()
	const w, h = 900, 700
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(1))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

// TestUploadMediaSizeMatchesListMedia pins the card's acceptance: for one
// uploaded item, upload_media's `size` is what list_media reports for that id.
func TestUploadMediaSizeMatchesListMedia(t *testing.T) {
	if strings.TrimSpace(os.Getenv("JOMFORM_TEST_DATABASE_URL")) == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping upload_media/list_media size parity test")
	}
	ctx := context.Background()
	pool := testdb.Open(t)

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ('umsz-' || md5(random()::text) || '@example.test', 'it') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug)
		 SELECT $1, 'umsz-acc-' || md5(random()::text), 'umsz-' || md5(random()::text)
		 RETURNING id`, userID).Scan(&accountID); err != nil {
		t.Fatalf("create account: %v", err)
	}

	store, fake := newFakeStore(t)
	media := dbstore.NewMediaStore(pool)
	srv := &Server{deps: &Deps{Media: media, Store: store}}
	callCtx := WithAuthIdentity(ctx, &AuthIdentity{
		AccountID: accountID,
		Scopes:    []string{dbstore.ScopeReadWrite},
	})

	listedByID := func(t *testing.T, id string) mediaOut {
		t.Helper()
		_, out, err := srv.listMedia(callCtx, nil, listMediaIn{Limit: 100})
		if err != nil {
			t.Fatalf("list_media: %v", err)
		}
		items, ok := out["items"].([]mediaOut)
		if !ok {
			t.Fatalf("list_media items have unexpected type %T", out["items"])
		}
		for _, it := range items {
			if it.ID == id {
				return it
			}
		}
		t.Fatalf("list_media does not contain media id %q (got %d items)", id, len(items))
		return mediaOut{}
	}

	t.Run("pdf", func(t *testing.T) {
		pdf := pdfFixtureBytes()
		_, up, err := srv.uploadMedia(callCtx, nil, uploadMediaIn{
			Kind: "pdf", Data: base64.StdEncoding.EncodeToString(pdf),
		})
		if err != nil {
			t.Fatalf("upload_media kind=pdf: %v", err)
		}
		if up.ID == "" {
			t.Fatalf("upload_media kind=pdf returned no media id; the row it stores must be listed by list_media")
		}
		item := listedByID(t, up.ID)
		if up.Size != item.Size {
			t.Fatalf("upload_media reported size=%d for media %s, list_media reports size=%d — the two tools describe the same row",
				up.Size, up.ID, item.Size)
		}
		// The preview is what makes the stored total exceed the uploaded bytes.
		// With gs installed it MUST have been generated and counted; without it
		// the equality above is the whole invariant.
		if gs, _ := exec.LookPath("gs"); gs != "" {
			previewKey := objstore.BuildPDFPreviewKey(up.Key)
			previewBytes, ok := fake.size(previewKey)
			if !ok {
				t.Fatalf("no preview object at %q although gs is installed; fixture no longer exercises the divergence", previewKey)
			}
			if want := int64(len(pdf) + previewBytes); up.Size != want {
				t.Fatalf("upload_media size=%d, want the stored total %d (pdf %d + preview %d) that list_media reports",
					up.Size, want, len(pdf), previewBytes)
			}
		}
	})

	t.Run("image", func(t *testing.T) {
		raw := pngFixtureBytes(t)
		_, up, err := srv.uploadMedia(callCtx, nil, uploadMediaIn{
			Kind: "image", Data: base64.StdEncoding.EncodeToString(raw),
		})
		if err != nil {
			t.Fatalf("upload_media kind=image: %v", err)
		}
		if up.ID == "" {
			t.Fatalf("upload_media kind=image returned no media id; an MCP image upload must be recorded in the library like the dashboard's is")
		}
		item := listedByID(t, up.ID)
		if up.Size != item.Size {
			t.Fatalf("upload_media reported size=%d for image media %s, list_media reports size=%d",
				up.Size, up.ID, item.Size)
		}
		// The stored total includes the generated size variants: prove the row
		// really is bigger than the base object, or the assertion proves nothing.
		baseKey := up.Key
		var variants int64
		for _, size := range objstore.ImageSizes {
			if n, ok := fake.size(objstore.BuildVariantKey(baseKey, size)); ok {
				variants += int64(n)
			}
		}
		if variants == 0 {
			t.Fatalf("no size variants were stored for %q; the fixture no longer exercises the divergence", baseKey)
		}
		baseBytes, _ := fake.size(baseKey)
		if want := int64(baseBytes) + variants; item.Size != want {
			t.Fatalf("list_media size=%d, want the stored total %d (base %d + variants %d)",
				item.Size, want, baseBytes, variants)
		}
		if up.Size == int64(baseBytes) && variants > 0 {
			t.Fatalf("upload_media still reports the base object (%d bytes) while the row counts %d variant bytes", baseBytes, variants)
		}
	})

	// No row may be left behind unlisted either way: the two ids above are the
	// same identities list_media serves, so a second read must be stable.
	if _, out, err := srv.listMedia(callCtx, nil, listMediaIn{Limit: 100}); err != nil {
		t.Fatalf("list_media (re-read): %v", err)
	} else if items, _ := out["items"].([]mediaOut); len(items) < 2 {
		t.Fatalf("list_media returned %d items, want both uploaded rows", len(items))
	}
}
