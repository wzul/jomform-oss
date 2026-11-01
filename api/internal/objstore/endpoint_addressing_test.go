package objstore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The object store must address a CUSTOM endpoint PATH-style, because a custom
// endpoint is by definition not AWS S3: it is a self-hosted or third-party
// S3-compatible server named on a compose network or an internal hostname, and
// virtual-host addressing (`<bucket>.<host>`) needs a wildcard DNS record that
// such a server never has.
//
// WHY THIS TEST EXISTS. The old rule sniffed the endpoint string for
// `localhost`, `minio` or `:9000` and enabled path-style only then, so the S3
// path worked for exactly one stack shape — a service literally named `minio`
// on port 9000. Measured on a real one-command install (card t_28a06494): with
// the object store named anything else (here `seaweedfs:8333`) the client asked
// the resolver for `jomform-media.<host>`, got NXDOMAIN during the startup
// bucket provisioning, and — because the API treats a failed EnsureBucket as
// "no media store" — silently disabled image uploads on a stack that reported
// itself healthy. Nothing logged a user-facing error; the upload endpoint just
// answered 503.
func TestCustomEndpointUsesPathStyle(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     bool
	}{
		{"compose service (the shape that was broken)", "http://seaweedfs:8333", true},
		{"internal hostname on a custom port", "http://storage.internal:9000", true},
		{"internal hostname, default port", "https://s3.internal", true},
		{"third-party S3-compatible", "https://s3.us-east-1.example.com", true},
		{"real AWS (no endpoint set)", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := NewObjectStore(tc.endpoint, "key", "secret", "jomform-media", "us-east-1", "https://forms.example.com/media")
			if err != nil {
				t.Fatalf("NewObjectStore: %v", err)
			}
			if got := st.client.Options().UsePathStyle; got != tc.want {
				t.Fatalf("UsePathStyle for endpoint %q = %v, want %v", tc.endpoint, got, tc.want)
			}
		})
	}
}

// And the style must be visible ON THE WIRE, not just in an option field: with a
// custom endpoint the request path must be /<bucket>/<key> and the Host must be
// the endpoint's own host — that is what a server with no wildcard DNS can
// answer.
func TestCustomEndpointRequestIsPathStyleOnTheWire(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Host+" "+r.URL.Path)
		mu.Unlock()
		// HeadBucket for a bucket that does not exist yet: 404 + the S3 error
		// body the SDK turns into types.NotFound, so EnsureBucket proceeds to
		// CreateBucket.
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>no such bucket</Message></Error>`))
	}))
	defer srv.Close()

	st, err := NewObjectStore(srv.URL, "key", "secret", "jomform-media", "us-east-1", "https://forms.example.com/media")
	if err != nil {
		t.Fatalf("NewObjectStore: %v", err)
	}
	// The result does not matter — the REQUEST it makes does.
	_ = st.EnsureBucket(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(paths) == 0 {
		t.Fatal("no request reached the endpoint")
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, p := range paths {
		if strings.HasPrefix(p, "jomform-media.") {
			t.Fatalf("virtual-host addressing leaked onto the wire: %q", p)
		}
		if !strings.HasPrefix(p, host+" ") {
			t.Fatalf("request %q did not go to the endpoint host %q", p, host)
		}
		if !strings.HasPrefix(strings.TrimPrefix(p, host+" "), "/jomform-media") {
			t.Fatalf("request %q is not path-style (/<bucket>/…)", p)
		}
	}
}
