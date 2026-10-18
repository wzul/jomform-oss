package httpapi

// THE DEFECT THESE GUARD. `DELETE /v1/rest/media/{id}/purge` removed the row and
// the object, the origin then answered 404 — and Cloudflare's edge kept serving
// its own copy, so a purged object stayed downloadable by anyone holding the
// URL. Measured on prod 2026-10-18: origin 404 (cache-busted), edge 200 with
// `cf-cache-status: HIT`, `age: 35`, `cache-control: public, max-age=86400`.
//
// Measuring only the origin is exactly how the gap stayed invisible: the origin
// is honest the moment the bytes go. So these tests measure BOTH ends:
//
//	TestPurgedObjectLeavesTheEdgeWhenThePurgeAsksItTo
//	    — the purge request reaches the CDN with every object URL (base, the three
//	      sized variants and the PDF preview), AFTER the bytes are gone, and the
//	      handler reports what the CDN said.
//	TestMediaPurgeNamesTheWindowWhenThePurgeIsNotConfigured
//	    — with no token the answer says so and names the freshness window that
//	      then bounds the staleness, instead of implying the edge was cleared.
//	TestMediaFreshnessBoundsHowLongTheEdgeCanOutliveAPurge
//	    — a CDN that honours the origin's Cache-Control: the purged object is
//	      STILL served from the edge inside the window (that is the residual this
//	      card bounds), and it is gone from the edge one second after it, with no
//	      purge at all.
//	TestAssetRouteSharesTheMediaFreshnessPolicy
//	    — /assets (merchant code assets) can be purged too, so it declares the
//	      same window.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/wzul/jomform/api/internal/cdnpurge"
)

// ── a fake Cloudflare purge endpoint ─────────────────────────────────────────

// edgePurgeRecorder is a fake CDN API: it records the purge request and lets the
// test inspect the store's state at the moment the CDN is asked to drop the
// object.
type edgePurgeRecorder struct {
	mu        sync.Mutex
	calls     int
	method    string
	path      string
	auth      string
	prefixes  []string
	bodyErr   string
	onRequest func()
	status    int
	payload   string
}

func (p *edgePurgeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prefixes []string `json:"prefixes"`
		Files    []string `json:"files"`
	}
	decodeErr := json.NewDecoder(r.Body).Decode(&body)

	p.mu.Lock()
	p.calls++
	p.method = r.Method
	p.path = r.URL.Path
	p.auth = r.Header.Get("Authorization")
	p.prefixes = append([]string(nil), body.Prefixes...)
	switch {
	case decodeErr != nil:
		p.bodyErr = decodeErr.Error()
	case len(body.Files) > 0:
		p.bodyErr = "request used files[] (exact URLs), which leaves the query variants cached"
	}
	status, payload := p.status, p.payload
	p.mu.Unlock()

	if p.onRequest != nil {
		p.onRequest()
	}
	if status == 0 {
		status = http.StatusOK
	}
	if payload == "" {
		payload = `{"success":true,"errors":[],"messages":[],"result":{"id":"purge-ok"}}`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(payload))
}

func (p *edgePurgeRecorder) snapshot() (calls int, method, path, auth, bodyErr string, prefixes []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls, p.method, p.path, p.auth, p.bodyErr, append([]string(nil), p.prefixes...)
}

// pointThePurgeAt makes cdnpurge.FromEnv reach the fake CDN for jomform.com.
func pointThePurgeAt(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv(cdnpurge.EnvToken, "test-token")
	t.Setenv(cdnpurge.EnvZoneID, "zone-123")
	t.Setenv(cdnpurge.EnvAPIBase, srv.URL)
	t.Setenv(cdnpurge.EnvHosts, "jomform.com")
}

// ── the purge asks the edge ──────────────────────────────────────────────────

// TestPurgedObjectLeavesTheEdgeWhenThePurgeAsksItTo drives the real REST purge
// route against a real Postgres fixture and a fake CDN, and asserts:
//
//  1. every URL the purge destroys is sent to the CDN (base + thumbnail + medium
//     + full + preview) — a purge that sends only the base URL leaves the three
//     variants, which is what the media grid actually renders;
//  2. they are sent as PREFIXES, so a query variant of the same object (a page,
//     an embed or a tracking link can append one) is covered by the same prefix;
//  3. they are sent AFTER the bytes are gone, so a request in between can only
//     miss the origin and never re-fill the edge with the deleted object;
//  4. the response NAMES the purge outcome, so an operator can tell "the edge was
//     asked" from "no CDN is configured".
func TestPurgedObjectLeavesTheEdgeWhenThePurgeAsksItTo(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping media edge-purge test")
	}
	ctx := context.Background()
	h, store, accountID, ms, apiKey := mediaTrashFixture(t)

	rec := &edgePurgeRecorder{}
	cf := httptest.NewServer(rec)
	defer cf.Close()
	pointThePurgeAt(t, cf)

	m := seedMediaRow(t, ctx, ms, store, accountID, 4096)
	// The variants the upload pipeline would have written for a raster image,
	// plus the PDF preview a PDF row would carry — all of them are objects the
	// purge deletes, so all of them are URLs the edge may hold.
	for _, k := range mediaObjectKeys(m.Key) {
		if err := store.PutObject(ctx, k, "image/png", []byte("variant")); err != nil {
			t.Fatalf("put variant %s: %v", k, err)
		}
	}

	// THE ORDERING CLAIM, measured: at the moment the CDN is asked to drop the
	// object, its bytes must already be gone.
	var presentAtPurge []string
	rec.onRequest = func() {
		for _, k := range mediaObjectKeys(m.Key) {
			if ok, _ := store.ObjectExists(ctx, k); ok {
				presentAtPurge = append(presentAtPurge, k)
			}
		}
	}

	if _, _, _, err := ms.SoftDelete(ctx, accountID, m.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	code, raw := tplReq(t, h, apiKey, http.MethodDelete, "/v1/rest/media/"+m.ID+"/purge", "")
	if code != http.StatusOK {
		t.Fatalf("purge = %d, want 200 (body %s)", code, raw)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("purge response is not JSON: %v (body %s)", err, raw)
	}
	if out["status"] != "purged" {
		t.Fatalf("status = %v, want purged (body %s)", out["status"], raw)
	}

	calls, method, path, auth, bodyErr, prefixes := rec.snapshot()
	if calls != 1 {
		t.Fatalf("the CDN received %d purge requests, want exactly 1 — a purge that does not ask the "+
			"edge leaves the object downloadable from it (response %s)", calls, raw)
	}
	if method != http.MethodPost || path != "/zones/zone-123/purge_cache" {
		t.Errorf("purge call = %s %s, want POST /zones/zone-123/purge_cache", method, path)
	}
	if auth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want the bearer token", auth)
	}
	if bodyErr != "" {
		t.Errorf("purge request body: %s", bodyErr)
	}

	wantPrefixes := mediaObjectURLs(store, m.Key)
	if len(wantPrefixes) != 5 {
		t.Fatalf("the purge is expected to invalidate 5 object URLs (base + 3 variants + preview), "+
			"the handler built %d: %v", len(wantPrefixes), wantPrefixes)
	}
	gotSet := map[string]bool{}
	for _, p := range prefixes {
		gotSet[p] = true
	}
	for _, want := range wantPrefixes {
		if !gotSet[want] {
			t.Errorf("the CDN was not asked to drop %q (it was sent %v) — that URL is still served "+
				"from the edge after the purge", want, prefixes)
		}
		if !json.Valid([]byte(`"` + want + `"`)) {
			t.Errorf("url %q is not a plain URL", want)
		}
	}
	if len(prefixes) != len(wantPrefixes) {
		t.Errorf("sent %d prefixes, want %d (%v)", len(prefixes), len(wantPrefixes), prefixes)
	}
	for _, p := range prefixes {
		if p == "" || p[0] != 'h' {
			t.Errorf("prefix %q is not an absolute URL — Cloudflare rejects the whole request", p)
		}
	}

	if len(presentAtPurge) > 0 {
		t.Errorf("the CDN was asked to drop %v while those objects were STILL in the bucket — a "+
			"request in that window would re-fill the edge with the object we are deleting", presentAtPurge)
	}

	// The handler names the outcome, and the URLs it asked about.
	if out["edge"] != cdnpurge.StatusPurged {
		t.Errorf("response edge = %v, want %q (body %s)", out["edge"], cdnpurge.StatusPurged, raw)
	}
	urls, _ := out["edge_urls"].([]any)
	if len(urls) != len(wantPrefixes) {
		t.Errorf("response edge_urls = %v, want the %d URLs that were purged", out["edge_urls"], len(wantPrefixes))
	}
	if _, has := out["edge_max_age_s"]; has {
		t.Errorf("a purged edge response carries edge_max_age_s (%v) — that field is the residual "+
			"window for a purge that did NOT happen", out["edge_max_age_s"])
	}
}

// TestMediaPurgeNamesTheWindowWhenThePurgeIsNotConfigured is the other half of
// the contract: without a CDN credential nothing is purged, and the response has
// to SAY so — with the freshness window that is now the only bound — rather than
// returning a plain "purged" that reads as "the edge was cleared".
func TestMediaPurgeNamesTheWindowWhenThePurgeIsNotConfigured(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set; skipping media edge-purge test")
	}
	ctx := context.Background()
	h, store, accountID, ms, apiKey := mediaTrashFixture(t)

	// Unconfigured: no token, no zone.
	t.Setenv(cdnpurge.EnvToken, "")
	t.Setenv(cdnpurge.EnvZoneID, "")
	// If anything dials this, it ignored the configuration and must fail loudly.
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a purge request was sent with no CDN credentials configured: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer cf.Close()
	t.Setenv(cdnpurge.EnvAPIBase, cf.URL)
	t.Setenv(cdnpurge.EnvHosts, "jomform.com")

	m := seedMediaRow(t, ctx, ms, store, accountID, 1024)
	if _, _, _, err := ms.SoftDelete(ctx, accountID, m.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	code, raw := tplReq(t, h, apiKey, http.MethodDelete, "/v1/rest/media/"+m.ID+"/purge", "")
	if code != http.StatusOK {
		t.Fatalf("purge = %d, want 200 (body %s)", code, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("purge response is not JSON: %v (body %s)", err, raw)
	}
	if out["edge"] != cdnpurge.StatusNotConfigured {
		t.Errorf("response edge = %v, want %q — a purge with no CDN credential must not read as "+
			"\"the edge was cleared\" (body %s)", out["edge"], cdnpurge.StatusNotConfigured, raw)
	}
	if got, _ := out["edge_max_age_s"].(float64); int(got) != cdnpurge.MediaMaxAgeSeconds {
		t.Errorf("response edge_max_age_s = %v, want %d — the response must name how long the edge "+
			"may still serve the purged object (body %s)", out["edge_max_age_s"], cdnpurge.MediaMaxAgeSeconds, raw)
	}
	// The purge itself still succeeded: the row and the bytes are gone.
	if present, err := store.ObjectExists(ctx, m.Key); err != nil || present {
		t.Fatalf("the object survived the purge: present=%v err=%v", present, err)
	}
}

// ── the freshness window, measured through a CDN that honours it ─────────────

// cacheStore is a concurrency-safe in-memory object store: the origin below is
// served by a real HTTP server, so the test's writes race the handler's reads.
type cacheStore struct {
	mu      sync.Mutex
	objects map[string]cacheObject
}

type cacheObject struct {
	body []byte
	ct   string
}

func newCacheStore() *cacheStore { return &cacheStore{objects: map[string]cacheObject{}} }

func (c *cacheStore) GetObject(_ context.Context, key string) ([]byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	o, ok := c.objects[key]
	if !ok {
		return nil, "", &types.NoSuchKey{}
	}
	return o.body, o.ct, nil
}

func (c *cacheStore) PutObject(_ context.Context, key, ct string, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[key] = cacheObject{body: body, ct: ct}
	return nil
}

func (c *cacheStore) PublicURL(key string) string { return "https://jomform.com/media/" + key }

func (c *cacheStore) DeleteObject(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.objects, key)
	return nil
}

func (c *cacheStore) ObjectExists(_ context.Context, key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.objects[key]
	return ok, nil
}

// edgeSim is the smallest CDN that has the behaviour this card is about: it
// stores a 200 with the freshness lifetime the ORIGIN declared, serves it from
// its own copy while that lifetime holds, and never asks the origin in that
// time. The clock is injectable so the window can be crossed without sleeping.
type edgeSim struct {
	originURL string

	mu      sync.Mutex
	now     time.Time
	entries map[string]*edgeSimEntry
	origin  int // requests that reached the origin
}

type edgeSimEntry struct {
	status   int
	body     []byte
	header   http.Header
	storedAt time.Time
	ttl      time.Duration
}

func newEdgeSim(originURL string) *edgeSim {
	return &edgeSim{
		originURL: originURL,
		now:       time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		entries:   map[string]*edgeSimEntry{},
	}
}

func (e *edgeSim) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

func (e *edgeSim) originHits() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.origin
}

func (e *edgeSim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.URL.RequestURI()
	e.mu.Lock()
	if ent, ok := e.entries[key]; ok && e.now.Sub(ent.storedAt) < ent.ttl {
		e.mu.Unlock()
		for k, v := range ent.header {
			w.Header()[k] = v
		}
		w.Header().Set("X-Edge", "HIT")
		w.Header().Set("Age", strconv.Itoa(int(e.now.Sub(ent.storedAt).Seconds())))
		w.WriteHeader(ent.status)
		_, _ = w.Write(ent.body)
		return
	}
	e.mu.Unlock()

	resp, err := http.Get(e.originURL + key)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	e.mu.Lock()
	e.origin++
	ttl := time.Duration(cacheMaxAgeSeconds(resp.Header.Get("Cache-Control"))) * time.Second
	if resp.StatusCode == http.StatusOK && ttl > 0 {
		hdr := http.Header{}
		for k, v := range resp.Header {
			if k == "Date" || k == "Content-Length" {
				continue
			}
			hdr[k] = v
		}
		e.entries[key] = &edgeSimEntry{
			status: resp.StatusCode, body: body, header: hdr, storedAt: e.now, ttl: ttl,
		}
	}
	e.mu.Unlock()

	for k, v := range resp.Header {
		if k == "Date" || k == "Content-Length" {
			continue
		}
		w.Header()[k] = v
	}
	w.Header().Set("X-Edge", "MISS")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

// TestMediaFreshnessBoundsHowLongTheEdgeCanOutliveAPurge measures BOTH ends
// through a CDN that behaves like the real one, and pins the two facts this card
// is about:
//
//  1. inside the declared window the edge still serves an object whose bytes are
//     gone from the origin — that is the residual, and it is why the window has
//     to be minutes rather than a day; and
//  2. one second past the window the edge answers 404 with no purge at all — the
//     self-healing floor when no CDN credential is configured.
func TestMediaFreshnessBoundsHowLongTheEdgeCanOutliveAPurge(t *testing.T) {
	store := newCacheStore()
	key := "u/00000000-0000-0000-0000-000000000001/edge-purge.png"
	if err := store.PutObject(context.Background(), key, "image/png", []byte("\x89PNG bytes")); err != nil {
		t.Fatalf("put object: %v", err)
	}

	h := &RESTHandlers{Store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /media/{key...}", h.handleMedia)
	origin := httptest.NewServer(mux)
	defer origin.Close()

	sim := newEdgeSim(origin.URL)
	edge := httptest.NewServer(sim)
	defer edge.Close()

	url := edge.URL + "/media/" + key
	get := func() (*http.Response, string) {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET via edge: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp, string(body)
	}

	// First request: a MISS, and the edge stores the origin's declared lifetime.
	resp, _ := get()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edge GET = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Edge"); got != "MISS" {
		t.Fatalf("first edge GET = %s, want MISS", got)
	}
	window := cacheMaxAgeSeconds(resp.Header.Get("Cache-Control"))
	if window != cdnpurge.MediaMaxAgeSeconds {
		t.Fatalf("the media response declares max-age=%d, want %d — the edge window and the purge "+
			"contract must be one number", window, cdnpurge.MediaMaxAgeSeconds)
	}

	// Second request: the edge answers from its own copy and never touches the
	// origin — the behaviour that keeps a deleted object alive.
	resp, _ = get()
	if got := resp.Header.Get("X-Edge"); got != "HIT" {
		t.Fatalf("second edge GET = %s, want HIT (the edge must be caching at all for this defect "+
			"to be real)", got)
	}
	if hits := sim.originHits(); hits != 1 {
		t.Fatalf("the edge hit the origin %d times for two requests, want 1 (a cached answer)", hits)
	}

	// THE PURGE: the bytes go, exactly as purgeMedia removes them.
	if err := store.DeleteObject(context.Background(), key); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	// The origin is honest immediately, which is what a probe that measures only
	// the origin reports.
	originResp, err := http.Get(origin.URL + "/media/" + key + "?cb=1")
	if err != nil {
		t.Fatalf("GET origin: %v", err)
	}
	_ = originResp.Body.Close()
	if originResp.StatusCode != http.StatusNotFound {
		t.Fatalf("origin GET after the purge = %d, want 404", originResp.StatusCode)
	}

	// And the edge still hands the purged object out — the defect, reproduced
	// locally. What this card changes is how long it may: it is the declared
	// window, not a day.
	resp, _ = get()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the edge stopped serving the purged object immediately (%d) — with no purge API "+
			"call the residual window is expected to hold until it expires", resp.StatusCode)
	}
	if window > 3600 {
		t.Fatalf("the edge is allowed to serve a purged object for %ds", window)
	}

	// One second past the window the edge revalidates against the origin and the
	// purged object is gone — with no CDN credential in play at all.
	sim.advance(time.Duration(window)*time.Second + time.Second)
	resp, body := get()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("edge GET one second past the %ds window = %d (%s), want 404 — the freshness "+
			"lifetime is the self-healing bound and it did not hold", window, resp.StatusCode, body)
	}
	if hits := sim.originHits(); hits != 2 {
		t.Errorf("the expired entry was answered without asking the origin (%d hits, want 2) — a "+
			"stale answer outliving its own max-age", hits)
	}
}

// TestAssetRouteSharesTheMediaFreshnessPolicy: /assets serves merchant code
// assets, which a purge destroys in exactly the same way, so it declares the same
// bounded lifetime. A page that pins one route's header and not the other's
// leaves half the purge surface on a day-long cache.
func TestAssetRouteSharesTheMediaFreshnessPolicy(t *testing.T) {
	ph, _, slugA, _, accA, _ := assetTenancyFixture(t)
	rec := publicAssetServe(t, ph, slugA+".jomform.com", "u/"+accA+"/aaaa.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/%s = %d, want 200 (body %s)", "aaaa.css", rec.Code, rec.Body.String())
	}
	cc := rec.Header().Get("Cache-Control")
	if got := cacheMaxAgeSeconds(cc); got != cdnpurge.MediaMaxAgeSeconds {
		t.Fatalf("the asset route declares Cache-Control %q (max-age %d), want %d — a purged code "+
			"asset would stay downloadable from the edge for the difference", cc, got, cdnpurge.MediaMaxAgeSeconds)
	}
}

// TestMediaCacheControlIsBoundedNotADay pins the served header itself, so the
// number in the code, the number in the response and the number the CDN is told
// cannot drift apart.
func TestMediaCacheControlIsBoundedNotADay(t *testing.T) {
	if got := cacheMaxAgeSeconds(mediaCacheControl); got != cdnpurge.MediaMaxAgeSeconds {
		t.Fatalf("mediaCacheControl = %q (max-age %d) but cdnpurge.MediaMaxAgeSeconds = %d",
			mediaCacheControl, got, cdnpurge.MediaMaxAgeSeconds)
	}
	if mediaCacheMaxAgeSeconds != cdnpurge.MediaMaxAgeSeconds {
		t.Fatalf("mediaCacheMaxAgeSeconds = %d, want %d", mediaCacheMaxAgeSeconds, cdnpurge.MediaMaxAgeSeconds)
	}
	if cacheMaxAgeSeconds(mediaCacheControl) >= 86400 {
		t.Fatalf("mediaCacheControl = %q — a day-long edge lifetime is the defect this card fixes",
			mediaCacheControl)
	}
}
