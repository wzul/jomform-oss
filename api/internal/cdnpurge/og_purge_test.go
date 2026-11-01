package cdnpurge

// The second caller of this package: a form's generated social card.
//
// THE DEFECT THESE GUARD. /og/{slug}.png is served with a public freshness
// lifetime, so a crawler that fetched it while the form was live leaves a copy at
// the edge. The origin then stops serving the card when the form leaves a live
// status (draft -> 404, private -> the password gate) while the edge keeps
// answering 200 with the form's title, workspace and host — measured on prod
// 2026-10-26 on a DRAFT form: `cf-cache-status: HIT`, `age: 1243`.
//
// Three things had to be true for the purge to work at all on a tenant host, and
// each is pinned here:
//
//	1. the URL the purge names is the one the card is cached under, as a PREFIX
//	   (so `?v=2` variants go with it);
//	2. a wildcard host entry matches the per-workspace subdomains, without
//	   silently widening to a lookalike host and WITHOUT matching the apex the
//	   operator did not list;
//	3. more invalidated cards than one request may carry are CHUNKED, not
//	   truncated — a bulk action holds up to 500 forms on two hosts each, and a
//	   silent truncation would report `edge: purged` while leaving entries behind.

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
)

// TestCardURLIsTheServedPathAsAPrefix: the URL the purge asks for must be the URL
// the card is served at — same scheme, same host, same path — because the CDN's
// cache key includes all three.
func TestCardURLIsTheServedPathAsAPrefix(t *testing.T) {
	if got, want := OGImagePath("request-a-quote"), "/og/request-a-quote.png"; got != want {
		t.Fatalf("OGImagePath = %q, want %q", got, want)
	}
	if got, want := OGImageURL("abc.jomform.com", "request-a-quote"),
		"https://abc.jomform.com/og/request-a-quote.png"; got != want {
		t.Fatalf("OGImageURL = %q, want %q", got, want)
	}
	// A host is normalised the way the zone matcher normalises it, so a configured
	// entry with a scheme or a port still produces the real URL.
	if got, want := OGImageURL("https://ABC.jomform.com:443/", "x"),
		"https://abc.jomform.com/og/x.png"; got != want {
		t.Fatalf("OGImageURL(normalised host) = %q, want %q", got, want)
	}
	// No host or no slug is not a relative URL to purge: it is nothing to do.
	if got := OGImageURL("", "x"); got != "" {
		t.Errorf("OGImageURL(\"\", x) = %q, want \"\"", got)
	}
	if got := OGImageURL("abc.jomform.com", " "); got != "" {
		t.Errorf("OGImageURL(host, blank slug) = %q, want \"\"", got)
	}
}

// TestQRImageURLIsTheServedPathAsAPrefix pins the QR PNG's URL the same way the
// card's is pinned, and pins ArtefactURLs as "the card AND the QR": the purge
// learns the URL space from this package only, so a path that drifts from the
// handler (httpapi /qr/{slug}.png) would ask the CDN to drop an entry nobody holds
// while the real one keeps serving the form's existence.
func TestQRImageURLIsTheServedPathAsAPrefix(t *testing.T) {
	if got, want := QRImagePath("tuition-class-registration"), "/qr/tuition-class-registration.png"; got != want {
		t.Fatalf("QRImagePath = %q, want %q", got, want)
	}
	if got, want := QRImageURL("wan.jomform.com", "tuition-class-registration"),
		"https://wan.jomform.com/qr/tuition-class-registration.png"; got != want {
		t.Fatalf("QRImageURL = %q, want %q", got, want)
	}
	if got := QRImageURL("", "x"); got != "" {
		t.Errorf("QRImageURL(\"\", x) = %q, want \"\"", got)
	}
	if got := QRImageURL("abc.jomform.com", " "); got != "" {
		t.Errorf("QRImageURL(host, blank slug) = %q, want \"\"", got)
	}

	// The per-table codes are query variants of the same path: a prefix matches
	// them all, so the purge never has to enumerate table numbers.
	if got, want := QRImagePath("menu"), "/qr/menu.png"; got != want {
		t.Errorf("QRImagePath(menu) = %q, want %q (no query string: the prefix covers ?table=N)", got, want)
	}

	got := ArtefactURLs("wan.jomform.com", "menu")
	want := []string{
		"https://wan.jomform.com/og/menu.png",
		"https://wan.jomform.com/qr/menu.png",
	}
	if len(got) != len(want) {
		t.Fatalf("ArtefactURLs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ArtefactURLs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := ArtefactURLs("", "menu"); len(got) != 0 {
		t.Errorf("ArtefactURLs(\"\", menu) = %v, want nothing (an unresolvable host is not a purge)", got)
	}
	if got := ArtefactURLs("wan.jomform.com", ""); len(got) != 0 {
		t.Errorf("ArtefactURLs(host, \"\") = %v, want nothing", got)
	}
}

// TestWildcardPurgeHostCoversTenantCards: a card lives on a per-workspace host, so
// the purge zone cannot list them one by one. An exact host list dropped every card
// purge as out of zone (silently — the form's status still changed), which is the
// shape a card fix must not ship with.
func TestWildcardPurgeHostCoversTenantCards(t *testing.T) {
	rec := &purgeRecorder{}
	c, closeFn := fakeCF(t, rec)
	defer closeFn()
	c.Hosts = splitHosts("*.jomform.com, shop.example.com")

	cardURLs := []string{
		OGImageURL("thezoolfamilyshop.jomform.com", "tpl-request-a-quote-bad8"),
		// A lookalike that is NOT a subdomain of the listed base, and the apex the
		// operator did not list: neither may be purged on a wildcard's behalf.
		OGImageURL("eviljomform.com", "other"),
		OGImageURL("jomform.com", "apex"),
	}
	purged, err := c.PurgeObjectURLs(context.Background(), cardURLs)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	want := "https://thezoolfamilyshop.jomform.com/og/tpl-request-a-quote-bad8.png"
	if len(purged) != 1 || purged[0] != want {
		t.Fatalf("purged prefixes = %v, want exactly [%q]: the wildcard must cover the tenant "+
			"host and must not widen to a lookalike or to the apex", purged, want)
	}

	// A deeper subdomain is still under the same zone.
	if p, err := c.PurgeObjectURLs(context.Background(),
		[]string{OGImageURL("a.b.jomform.com", "deep")}); err != nil || len(p) != 1 {
		t.Fatalf("deep subdomain purged = %v (err %v), want one prefix", p, err)
	}

	// An EXACT entry still works and still matches only itself: the wildcard is an
	// addition, not a replacement for the media purge's host list.
	c.Hosts = splitHosts("shop.example.com")
	if p, err := c.PurgeObjectURLs(context.Background(),
		[]string{OGImageURL("shop.example.com", "exact"), OGImageURL("x.shop.example.com", "sub")}); err != nil ||
		len(p) != 1 || p[0] != "https://shop.example.com/og/exact.png" {
		t.Fatalf("exact host purged = %v (err %v), want exactly the listed host", p, err)
	}
}

// TestPurgeChunksPastThePerRequestCap: one request may carry 100 operations. A bulk
// draft over 60 forms on two hosts is 120 prefixes, and reporting `edge: purged`
// after sending the first 100 would be a lie about 20 cached cards.
func TestPurgeChunksPastThePerRequestCap(t *testing.T) {
	rec := &purgeRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	defer srv.Close()
	t.Setenv(EnvToken, "test-token")
	t.Setenv(EnvZoneID, "zone-123")
	t.Setenv(EnvAPIBase, srv.URL)
	t.Setenv(EnvHosts, "*.jomform.com")

	const n = 250
	urls := make([]string, 0, n)
	for i := 0; i < n; i++ {
		urls = append(urls, OGImageURL("ws.jomform.com", slugForIndex(i)))
	}
	res := PurgeObjectURLs(context.Background(), urls)
	if res.Status != StatusPurged {
		t.Fatalf("result = %+v, want purged", res)
	}
	if len(res.URLs) != n {
		t.Fatalf("%d prefixes were purged, want all %d — a truncated request reports a purge "+
			"for entries it never asked about", len(res.URLs), n)
	}
	calls, _, _, _, _, body := rec.snapshot()
	wantCalls := (n + maxPrefixesPerRequest - 1) / maxPrefixesPerRequest
	if calls != wantCalls {
		t.Fatalf("%d purge requests for %d prefixes, want %d (100 per request)", calls, n, wantCalls)
	}
	if got := len(prefixesOf(t, body)); got > maxPrefixesPerRequest {
		t.Fatalf("a single request carried %d prefixes, over the documented cap of %d",
			got, maxPrefixesPerRequest)
	}
}

// TestArtefactFieldsNamesTheWindowWhenNoPurgeHappened: `edge: not_configured`
// alone leaves an operator guessing how long the old artefacts keep being served,
// and the number IS the answer. A successful purge must not claim a residual
// window, and a zero Result (nothing was invalidated) must claim nothing at all.
//
// The window is the one BOTH artefacts declare (the card's and the QR PNG's are
// the same constant), because one purge drops both and the reply carries one
// number.
func TestArtefactFieldsNamesTheWindowWhenNoPurgeHappened(t *testing.T) {
	if QRImageMaxAgeSeconds != OGImageMaxAgeSeconds {
		t.Fatalf("QRImageMaxAgeSeconds = %d, OGImageMaxAgeSeconds = %d — one purge, one reported window",
			QRImageMaxAgeSeconds, OGImageMaxAgeSeconds)
	}
	purged := Result{Status: StatusPurged, URLs: []string{"https://a/og/x.png"}}.ArtefactFields()
	if purged["edge"] != StatusPurged {
		t.Errorf("purged edge = %v, want %q", purged["edge"], StatusPurged)
	}
	if _, ok := purged["edge_max_age_s"]; ok {
		t.Errorf("a successful purge reported a residual window: %v", purged)
	}
	for _, status := range []string{StatusNotConfigured, StatusOutOfZone, StatusFailed} {
		f := Result{Status: status}.ArtefactFields()
		got, ok := f["edge_max_age_s"].(int)
		if !ok || got != OGImageMaxAgeSeconds {
			t.Errorf("%s: edge_max_age_s = %v, want %d", status, f["edge_max_age_s"], OGImageMaxAgeSeconds)
		}
	}
	if f := (Result{}).ArtefactFields(); f != nil {
		t.Errorf("a zero Result rendered %v, want nil — nothing was invalidated, so the reply "+
			"must not claim an edge action", f)
	}
}

// slugForIndex makes distinct, valid-looking slugs.
func slugForIndex(i int) string { return fmt.Sprintf("card-%03d", i) }
