package cdnpurge

// Tests for the edge-purge client.
//
// THE DEFECT THESE GUARD. A purged media object stayed downloadable from
// Cloudflare's edge for up to a day: the origin answered 404 the moment the
// bytes went, and the edge kept serving its own copy (`cf-cache-status: HIT`,
// `age: 35`) because nothing asked it to drop the entry. So these tests do not
// assert "a client exists" — they assert the request that actually invalidates
// the object:
//
//	1. the URL list is the OBJECT's own paths, sent as prefixes (so a query
//	   variant of the same object is covered by the same prefix),
//	2. a URL outside the configured zone is dropped instead of failing the whole
//	   request,
//	3. a refusal is REPORTED (Cloudflare answers 200 with success:false for
//	   several rejections), never swallowed, and
//	4. unconfigured is named as unconfigured, not as a purge that happened.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// purgeRecorder is a fake Cloudflare API: it records the purge requests it
// receives and answers with a canned response.
type purgeRecorder struct {
	mu      sync.Mutex
	calls   int
	method  string
	path    string
	auth    string
	ct      string
	body    map[string]any
	status  int
	payload string
}

func (p *purgeRecorder) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.calls++
		p.method = r.Method
		p.path = r.URL.Path
		p.auth = r.Header.Get("Authorization")
		p.ct = r.Header.Get("Content-Type")
		p.body = nil
		_ = json.NewDecoder(r.Body).Decode(&p.body)
		status, payload := p.status, p.payload
		p.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		if payload == "" {
			payload = `{"success":true,"errors":[],"messages":[],"result":{"id":"abc"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}
}

func (p *purgeRecorder) snapshot() (calls int, method, path, auth, ct string, body map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.method, p.path, p.auth, p.ct, p.body
}

// fakeCF starts the fake API and returns it plus a client pointed at it.
func fakeCF(t *testing.T, rec *purgeRecorder) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(rec.handler(t))
	c := &Client{
		Token:   "test-token",
		ZoneID:  "zone-123",
		Hosts:   []string{"jomform.com"},
		APIBase: srv.URL,
		HTTP:    srv.Client(),
	}
	return c, srv.Close
}

// prefixesOf pulls the `prefixes` array out of a recorded request body.
func prefixesOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	if body == nil {
		t.Fatal("the purge request carried no JSON body")
	}
	raw, ok := body["prefixes"]
	if !ok {
		t.Fatalf("the purge body carries no prefixes (body %v) — an exact-URL purge leaves the "+
			"query-string variants of the same object cached and still downloadable", body)
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("prefixes is %T, want an array", raw)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			t.Fatalf("prefix entry is %T, want string", it)
		}
		out = append(out, s)
	}
	return out
}

func TestPurgeObjectURLsSendsTheObjectPathsAsPrefixes(t *testing.T) {
	rec := &purgeRecorder{}
	c, done := fakeCF(t, rec)
	defer done()

	urls := []string{
		"https://jomform.com/media/u/acc/0b3e.png",
		"https://jomform.com/media/u/acc/0b3e-thumbnail.png",
		"https://jomform.com/media/u/acc/0b3e-medium.png",
		"https://jomform.com/media/u/acc/0b3e-full.png",
		"https://jomform.com/media/u/acc/0b3e-preview.jpg",
		// A query the page/embed appended: the same object, and — because a CDN
		// keys on the full URL — a separate entry that must collapse onto the same
		// prefix rather than be sent as a second, weaker one.
		"https://jomform.com/media/u/acc/0b3e.png?v=1792342130",
	}
	purged, err := c.PurgeObjectURLs(context.Background(), urls)
	if err != nil {
		t.Fatalf("PurgeObjectURLs: %v", err)
	}

	calls, method, path, auth, ct, body := rec.snapshot()
	if calls != 1 {
		t.Fatalf("the CDN received %d purge requests, want exactly 1", calls)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if want := "/zones/zone-123/purge_cache"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if auth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the bearer token", auth)
	}
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	got := prefixesOf(t, body)
	// The query variant must collapse onto the same prefix rather than send a
	// second, weaker entry: the prefix is what matches every variant.
	want := urls[0:5]
	if len(got) != len(want) {
		t.Fatalf("sent %d prefixes (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("prefix[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// And the returned list is what we asked about, not the input with queries.
	for i := range purged {
		if strings.Contains(purged[i], "?") {
			t.Errorf("purged[%d] = %q still carries the query string — a prefix purge of a "+
				"query'd URL does not match the bare URL", i, purged[i])
		}
	}
}

func TestPurgeObjectURLsDropsURLsOutsideTheZone(t *testing.T) {
	rec := &purgeRecorder{}
	c, done := fakeCF(t, rec)
	defer done()

	purged, err := c.PurgeObjectURLs(context.Background(), []string{
		"https://jomform.com/media/u/acc/a.png",
		"https://another-zone.example/media/u/acc/a.png",
	})
	if err != nil {
		t.Fatalf("PurgeObjectURLs: %v", err)
	}
	_, _, _, _, _, body := rec.snapshot()
	got := prefixesOf(t, body)
	if len(got) != 1 || got[0] != "https://jomform.com/media/u/acc/a.png" {
		t.Fatalf("prefixes = %v, want only the in-zone URL — an out-of-zone URL makes Cloudflare "+
			"reject the WHOLE request, which would silently cancel the good purge too", got)
	}
	if len(purged) != 1 {
		t.Errorf("purged = %v, want just the in-zone URL", purged)
	}
}

func TestPurgeObjectURLsWithNoZoneURLAsksTheCDNNothing(t *testing.T) {
	rec := &purgeRecorder{}
	c, done := fakeCF(t, rec)
	defer done()

	purged, err := c.PurgeObjectURLs(context.Background(), []string{
		"https://another-zone.example/media/u/acc/a.png",
	})
	if err != nil {
		t.Fatalf("PurgeObjectURLs: %v", err)
	}
	if calls, _, _, _, _, _ := rec.snapshot(); calls != 0 {
		t.Errorf("the CDN received %d requests for an out-of-zone URL, want 0", calls)
	}
	if len(purged) != 0 {
		t.Errorf("purged = %v, want empty", purged)
	}
}

func TestPurgeObjectURLsReportsASuccessFalseRefusal(t *testing.T) {
	// Cloudflare answers 200 with success:false for several rejections (1134 is
	// rate limited), so the status code alone is not evidence of a purge.
	rec := &purgeRecorder{
		status:  http.StatusOK,
		payload: `{"success":false,"errors":[{"code":1134,"message":"Unable to purge, rate limit reached."}],"messages":[],"result":null}`,
	}
	c, done := fakeCF(t, rec)
	defer done()

	if _, err := c.PurgeObjectURLs(context.Background(), []string{"https://jomform.com/media/u/a/x.png"}); err == nil {
		t.Fatal("a success:false response was reported as a successful purge — an operator would " +
			"believe the edge was cleared while it still serves the deleted object")
	} else if !strings.Contains(err.Error(), "1134") || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %q, want it to name Cloudflare's code and message", err)
	}
}

func TestPurgeObjectURLsReportsAnHTTPFailure(t *testing.T) {
	rec := &purgeRecorder{status: http.StatusForbidden, payload: `{"success":false,"errors":[{"code":9109,"message":"Unauthorized"}]}`}
	c, done := fakeCF(t, rec)
	defer done()

	if _, err := c.PurgeObjectURLs(context.Background(), []string{"https://jomform.com/media/u/a/x.png"}); err == nil {
		t.Fatal("a 403 from the CDN was reported as a successful purge")
	}
}

func TestPurgeObjectURLsCapsTheRequestAtTheDocumentedLimit(t *testing.T) {
	rec := &purgeRecorder{}
	c, done := fakeCF(t, rec)
	defer done()

	urls := make([]string, 0, 150)
	for i := 0; i < 150; i++ {
		urls = append(urls, "https://jomform.com/media/u/a/obj"+string(rune('a'+i%26))+string(rune('a'+i/26))+".png")
	}
	if _, err := c.PurgeObjectURLs(context.Background(), urls); err != nil {
		t.Fatalf("PurgeObjectURLs: %v", err)
	}
	_, _, _, _, _, body := rec.snapshot()
	if got := len(prefixesOf(t, body)); got != maxPrefixesPerRequest {
		t.Errorf("sent %d prefixes, want the documented ceiling of %d", got, maxPrefixesPerRequest)
	}
}

func TestPurgeObjectURLsIsNotConfiguredWithoutBothCredentials(t *testing.T) {
	// A token without a zone (or the reverse) cannot purge: reporting it as
	// configured would name a purge that cannot happen.
	for name, env := range map[string][2]string{
		"no token":  {"", "zone-123"},
		"no zone":   {"test-token", ""},
		"neither":   {"", ""},
		"blank ids": {"   ", "  "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(EnvToken, env[0])
			t.Setenv(EnvZoneID, env[1])
			t.Setenv(EnvAPIBase, "http://127.0.0.1:1") // must never be dialled
			res := PurgeObjectURLs(context.Background(), []string{"https://jomform.com/media/u/a/x.png"})
			if res.Status != StatusNotConfigured {
				t.Fatalf("status = %q, want %q", res.Status, StatusNotConfigured)
			}
			if res.Err != nil {
				t.Errorf("Err = %v, want nil — unconfigured is a supported state, not a failure", res.Err)
			}
			if f := res.Fields(); f["edge"] != StatusNotConfigured {
				t.Errorf("Fields()[edge] = %v, want %q", f["edge"], StatusNotConfigured)
			}
		})
	}
}

func TestPurgeObjectURLsReportsTheZoneStatusAndURLs(t *testing.T) {
	rec := &purgeRecorder{}
	c, done := fakeCF(t, rec)
	defer done()
	t.Setenv(EnvToken, "test-token")
	t.Setenv(EnvZoneID, "zone-123")
	t.Setenv(EnvAPIBase, c.APIBase)
	t.Setenv(EnvHosts, "jomform.com")

	res := PurgeObjectURLs(context.Background(), []string{"https://jomform.com/media/u/a/x.png"})
	if res.Status != StatusPurged {
		t.Fatalf("status = %q, want %q (err %v)", res.Status, StatusPurged, res.Err)
	}
	f := res.Fields()
	if f["edge"] != StatusPurged {
		t.Errorf("Fields()[edge] = %v, want %q", f["edge"], StatusPurged)
	}
	urls, ok := f["edge_urls"].([]string)
	if !ok || len(urls) != 1 || urls[0] != "https://jomform.com/media/u/a/x.png" {
		t.Errorf("Fields()[edge_urls] = %v, want the purged URL", f["edge_urls"])
	}
	if _, has := f["edge_error"]; has {
		t.Errorf("a successful purge carries edge_error: %v", f["edge_error"])
	}
}

func TestPurgeObjectURLsNamesNoURLsAndOutOfZone(t *testing.T) {
	t.Setenv(EnvToken, "t")
	t.Setenv(EnvZoneID, "z")
	t.Setenv(EnvHosts, "jomform.com")
	t.Setenv(EnvAPIBase, "http://127.0.0.1:1") // must never be dialled

	if res := PurgeObjectURLs(context.Background(), nil); res.Status != StatusNoURLs {
		t.Errorf("no URLs: status = %q, want %q", res.Status, StatusNoURLs)
	}
	if res := PurgeObjectURLs(context.Background(), []string{"https://other.example/media/u/a/x.png"}); res.Status != StatusOutOfZone {
		t.Errorf("out-of-zone: status = %q, want %q", res.Status, StatusOutOfZone)
	}
}

func TestFromEnvDerivesTheZoneHostFromThePublicBase(t *testing.T) {
	cases := []struct {
		name   string
		public string
		app    string
		want   string
	}{
		{"the media base wins", "https://JomForm.com/media", "https://other.example", "jomform.com"},
		{"the app base is the fallback", "", "https://jomform.com", "jomform.com"},
		{"a port is dropped", "https://jomform.com:443/media", "", "jomform.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvToken, "t")
			t.Setenv(EnvZoneID, "z")
			t.Setenv(EnvHosts, "")
			t.Setenv("S3_PUBLIC_BASE", tc.public)
			t.Setenv("APP_BASE_URL", tc.app)
			c := FromEnv()
			if c == nil {
				t.Fatal("FromEnv returned nil for a configured pair")
			}
			if len(c.Hosts) != 1 || c.Hosts[0] != tc.want {
				t.Fatalf("Hosts = %v, want [%q]", c.Hosts, tc.want)
			}
		})
	}
}

func TestFromEnvHonoursAnExplicitHostList(t *testing.T) {
	t.Setenv(EnvToken, "t")
	t.Setenv(EnvZoneID, "z")
	t.Setenv(EnvHosts, "jomform.com, staging.jomform.com")
	c := FromEnv()
	if c == nil {
		t.Fatal("FromEnv returned nil for a configured pair")
	}
	want := []string{"jomform.com", "staging.jomform.com"}
	if len(c.Hosts) != len(want) || c.Hosts[0] != want[0] || c.Hosts[1] != want[1] {
		t.Fatalf("Hosts = %v, want %v", c.Hosts, want)
	}
}
