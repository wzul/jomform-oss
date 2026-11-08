package mediareconcile

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The reconcile tool must address a CUSTOM endpoint PATH-style, for the same
// reason the API's own object store does (objstore.endpoint_addressing_test.go):
// a custom endpoint is by definition not AWS S3 — it is a self-hosted or
// third-party S3-compatible server named on a compose network or an internal
// hostname — and virtual-host addressing (`<bucket>.<host>`) needs a wildcard DNS
// record such a server never publishes.
//
// WHY THIS TEST EXISTS. This constructor kept the old sniff (`localhost`,
// `127.0.0.1`, `minio`, `:9000`) after objstore was already fixed, so it worked
// against exactly one stack shape: a service literally named `minio` on port
// 9000. When production moved to `S3_ENDPOINT=http://s3:8333` (SeaweedFS), the
// tool built `jomform-media.s3:8333`, which does not resolve on the compose
// network, so `cmd/media-reconcile` could not read the store — precisely when it
// is run (auditing the bucket after the media migration).
//
// The first case is the one that discriminates: `http://s3:8333` contains none of
// the sniffed strings, so with the old rule this assertion is red. The rest keep
// the shapes that used to work working.
func TestNewS3BucketPathStyleForEveryCustomEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{"compose service after the SeaweedFS switch (the shape that broke)", "http://s3:8333", true},
		{"minio service on its old port (the only shape the sniff handled)", "http://minio:9000", true},
		{"internal hostname on a custom port", "http://storage.internal:9000", true},
		{"internal hostname, default port", "https://s3.internal", true},
		{"third-party S3-compatible", "https://s3.us-east-1.example.com", true},
		{"real AWS (no endpoint set)", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewS3Bucket(tc.endpoint, "key", "secret", "jomform-media", "us-east-1")
			if err != nil {
				t.Fatalf("NewS3Bucket(%q): %v", tc.endpoint, err)
			}
			if got := b.client.Options().UsePathStyle; got != tc.want {
				t.Fatalf("UsePathStyle for endpoint %q = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

// And the style must be visible ON THE WIRE, not only in an option field: the
// request must carry the endpoint's own Host and the bucket as a PATH segment
// (/<bucket>?list-type=2), which is what a server with no wildcard DNS can
// answer.
//
// NOTE ON SCOPE: a loopback stub endpoint contains `127.0.0.1`, which the old
// sniff also matched, so this test alone does NOT discriminate the rule — the
// table test above is what goes red when the sniff comes back. What this one
// covers is the wiring after the decision: that List actually issues a
// path-style request against the endpoint it was given.
func TestS3BucketListIsPathStyleOnTheWire(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Host+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
			`<ListBucketResult><Name>jomform-media</Name><KeyCount>0</KeyCount>`+
			`<MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	defer srv.Close()

	b, err := NewS3Bucket(srv.URL, "key", "secret", "jomform-media", "us-east-1")
	if err != nil {
		t.Fatalf("NewS3Bucket: %v", err)
	}
	if _, err := b.List(context.Background(), "u/"); err != nil {
		t.Fatalf("List: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no request reached the endpoint")
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, req := range seen {
		if strings.HasPrefix(req, "jomform-media.") {
			t.Fatalf("virtual-host addressing leaked onto the wire: %q", req)
		}
		if !strings.HasPrefix(req, host+" ") {
			t.Fatalf("request %q did not go to the endpoint host %q", req, host)
		}
		if !strings.HasPrefix(strings.TrimPrefix(req, host+" "), "/jomform-media") {
			t.Fatalf("request %q is not path-style (/<bucket>…)", req)
		}
	}
}
