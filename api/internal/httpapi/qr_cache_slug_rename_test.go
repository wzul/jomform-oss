package httpapi

// A public response may only be cached when its bytes are a function of its
// cache key. /qr/{slug}.png declared `public, max-age=86400` on BOTH resolution
// arms, but the bytes it serves encode the form's CURRENT slug (formPublicURL
// reads f.Slug): through the rename fallback (findPublicForm) the requested path
// and the encoded URL name DIFFERENT slugs, so a CDN entry created under the old
// slug's key keeps serving a URL the form has already left, for the whole TTL and
// across every later rename.
//
// Measured on a workspace behind Cloudflare: GET /qr/{slug}.png (cf-cache-status
// HIT, age ~12min, from an entry written before the rename) decoded to a slug the
// form had moved off two renames earlier and which now 301s — while the same
// request with a cache-buster decoded to the correct, live URL, and the printable
// stand (uncached) was correct too.
//
// The remedy implemented (the card's first option) keeps the long cache on the
// live-slug arm only — there the bytes ARE a function of the path — and answers
// the fallback arm with Cache-Control: no-store. Cloudflare does not store a
// response carrying no-store (it "prevents caching in either Origin Cache Control
// mode", see developers.cloudflare.com/cache/concepts/default-cache-behavior), so
// no key can hold bytes that follow a slug other than its own. The redirect
// alternative (301 /qr/{old}.png -> /qr/{live}.png) would have needed the same
// care the other way round: .png is one of Cloudflare's default cached
// extensions and a 301 without a cache directive gets a 120m edge TTL, so the
// old->new mapping itself would have gone sticky.
//
// The guard the card found MISSING is the one below: no test fetched
// /qr/{old-slug}.png through the rename fallback and asserted the ENCODED URL, so
// nothing could see a stale representation. qrcode.Encode is deterministic for a
// given target/level/size — the same technique qr_stand_issue499_test.go uses —
// so byte equality against a fresh encode of the wanted target is an assertion
// about the encoded URL, not about renderer noise.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// qrPublicURL is the canonical public URL a form serving slug has on host, built
// exactly as formPublicURL builds it. The scheme comes from the package-global
// isDev (set at wiring time and pinned by oauth_test.go) rather than being
// hardcoded, so this test does not pass alone and fail in the full suite.
func qrPublicURL(host, slug, query string) string {
	scheme := "https"
	if isDev {
		scheme = "http"
	}
	u := scheme + "://" + host + "/" + slug
	if query != "" {
		u += "?" + query
	}
	return u
}

// assertQRPngEncodes asserts the served PNG is byte-identical to a fresh
// qrcode.Encode of want, i.e. the image necessarily scans to want.
func assertQRPngEncodes(t *testing.T, png []byte, want string) {
	t.Helper()
	exp, err := qrcode.Encode(want, qrcode.Medium, 512)
	if err != nil {
		t.Fatalf("qrcode.Encode(%q): %v", want, err)
	}
	if !bytes.Equal(png, exp) {
		t.Errorf("the served QR does not encode %q", want)
	}
}

// qrGet fetches a /qr/{slug}.png path on host and requires a 200 image/png.
func qrGet(t *testing.T, h *PublicHandlers, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := publicServe(t, h, host, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s on %s = %d, want 200 (body %s)", path, host, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("GET %s Content-Type = %q, want image/png", path, ct)
	}
	return rec
}

// TestQRPngCacheFollowsTheFormsLiveSlug is the core guard. After a rename the
// form's OLD slug resolves through the rename fallback; the PNG that arm serves
// encodes the form's LIVE url, so it must not be served from a cache keyed on the
// old slug — and it really must encode the live url, not the requested one.
func TestQRPngCacheFollowsTheFormsLiveSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-qrcache-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-qrcache-new-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	const query = "table=7"
	// Both resolution branches of findPublicForm: the workspace subdomain and a
	// verified custom domain. They are separate lookups and the custom-domain one
	// has had its own bugs (see the rename suite), so both are driven.
	for _, host := range []string{fx.host, fx.customDomain} {
		t.Run(host, func(t *testing.T) {
			liveTarget := qrPublicURL(host, newSlug, query)

			// 1. The form's OWN slug: the bytes are a function of the cache key,
			//    so the 24h public cache stays — a per-table QR is fetched on
			//    every table stand render and must not hit the origin each time.
			live := qrGet(t, fx.pub, host, "/qr/"+newSlug+".png?"+query)
			assertQRPngEncodes(t, live.Body.Bytes(), liveTarget)
			if cc := live.Header().Get("Cache-Control"); !strings.Contains(cc, "public") || !strings.Contains(cc, "max-age=86400") {
				t.Errorf("GET /qr/%s.png Cache-Control = %q, want the 24h public cache kept for the live slug "+
					"(its bytes ARE a function of its own key)", newSlug, cc)
			}

			// 2. The rename fallback arm — the carded defect. Its bytes encode the
			//    live url, so a cache entry under the OLD slug's key would keep
			//    serving a URL the form has already left.
			old := qrGet(t, fx.pub, host, "/qr/"+oldSlug+".png?"+query)
			assertQRPngEncodes(t, old.Body.Bytes(), liveTarget)
			if stale, err := qrcode.Encode(qrPublicURL(host, oldSlug, query), qrcode.Medium, 512); err != nil {
				t.Fatalf("qrcode.Encode: %v", err)
			} else if bytes.Equal(old.Body.Bytes(), stale) {
				t.Errorf("GET /qr/%s.png encodes %q — the slug the form no longer serves (it 301s, and once "+
					"the redirect expires it 404s). The QR must encode the form's live url %q",
					oldSlug, qrPublicURL(host, oldSlug, query), liveTarget)
			}
			cc := old.Header().Get("Cache-Control")
			if !strings.Contains(cc, "no-store") {
				t.Errorf("GET /qr/%s.png Cache-Control = %q, want no-store.\n"+
					"This response's bytes follow the form's CURRENT slug, not the requested path, so caching "+
					"them under this key serves a stale QR for the whole TTL after every later rename "+
					"(measured on prod behind Cloudflare, where such a cache entry decoded to a slug two "+
					"renames stale). Cache the live-slug arm, never the fallback arm.", oldSlug, cc)
			}
			if strings.Contains(cc, "max-age") || strings.Contains(cc, "public") {
				t.Errorf("GET /qr/%s.png Cache-Control = %q still carries a cache lifetime — a stored copy "+
					"outlives the next rename and then encodes a URL this form has left", oldSlug, cc)
			}
		})
	}
}

// TestQRPngCacheAfterTwoRenamesFollowsTheCurrentSlug replays the MEASURED
// sequence: a QR PNG is fetched while the form still serves slug A, the form then
// moves twice, and the entry written under A's key is read afterwards. Every
// non-live slug must re-render the current URL and refuse to be stored.
func TestQRPngCacheAfterTwoRenamesFollowsTheCurrentSlug(t *testing.T) {
	fx := newRenameFixture(t, "it-qr2-a-"+randHex(4))
	a := fx.slug
	b := "it-qr2-b-" + randHex(4)
	c := "it-qr2-c-" + randHex(4)

	for _, next := range []string{b, c} {
		if code, raw := fx.patch(t, `{"slug":"`+next+`","title":"Western menu"}`); code != http.StatusOK {
			t.Fatalf("rename to %s = %d, want 200 (body %s)", next, code, raw)
		}
	}

	const query = "table=7"
	liveTarget := qrPublicURL(fx.host, c, query)

	// Both slugs behind the form now resolve through the fallback (the redirect
	// row for A still names the form, whatever its slug is today), and both must
	// serve the CURRENT url uncached. A cached-as-public response here is exactly
	// the stale artefact the card measured.
	for _, slug := range []string{a, b} {
		rec := qrGet(t, fx.pub, fx.host, "/qr/"+slug+".png?"+query)
		assertQRPngEncodes(t, rec.Body.Bytes(), liveTarget)
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("GET /qr/%s.png (two renames stale) Cache-Control = %q, want no-store", slug, cc)
		}
	}

	// The live slug itself keeps the long cache, so the fix does not turn the
	// endpoint into an uncacheable one.
	rec := qrGet(t, fx.pub, fx.host, "/qr/"+c+".png?"+query)
	assertQRPngEncodes(t, rec.Body.Bytes(), liveTarget)
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=86400") {
		t.Errorf("GET /qr/%s.png (the live slug) Cache-Control = %q, want the 24h public cache", c, cc)
	}

	// A second table is a separate cache key with the same rule: the fallback arm
	// stays uncached and still carries that table number onto the live url (the
	// query is what the stand exists to convey).
	rec = qrGet(t, fx.pub, fx.host, "/qr/"+a+".png?table=9")
	assertQRPngEncodes(t, rec.Body.Bytes(), qrPublicURL(fx.host, c, "table=9"))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("GET /qr/%s.png?table=9 Cache-Control = %q, want no-store", a, cc)
	}
}

// TestQRPngCacheOnAClaimedSlugIsTheClaimantsOwnKey pins that the rule is "the
// requested slug IS a live slug", not "this form's slug": when a later form
// claims the freed slug (Wan's supersede rule — the claimant wins), that path is
// a live slug again, so its own QR is cacheable and encodes its OWN url.
func TestQRPngCacheOnAClaimedSlugIsTheClaimantsOwnKey(t *testing.T) {
	fx := newRenameFixture(t, "it-qrclaim-old-"+randHex(4))
	oldSlug := fx.slug
	newSlug := "it-qrclaim-new-" + randHex(4)

	if code, raw := fx.patch(t, `{"slug":"`+newSlug+`","title":"Western menu"}`); code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (body %s)", code, raw)
	}

	claimant := &dbstore.Form{
		AccountID: fx.accountID, Slug: oldSlug, Title: "Claimant menu",
		Status: dbstore.FormStatusPublished,
		Config: map[string]any{"blocks": []any{map[string]any{"type": "text", "value": "Claimant body"}}},
		Theme:  map[string]any{"primary": "#000000"},
	}
	if err := dbstore.NewFormStore(fx.pool).Create(fx.ctx, claimant); err != nil {
		t.Fatalf("the freed old slug must be claimable by a later form, got: %v", err)
	}

	const query = "table=7"
	rec := qrGet(t, fx.pub, fx.host, "/qr/"+oldSlug+".png?"+query)
	// Live-first resolution: the claimant is the form this URL belongs to now, so
	// the code encodes the requested slug itself — its own key — and is cacheable.
	assertQRPngEncodes(t, rec.Body.Bytes(), qrPublicURL(fx.host, oldSlug, query))
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=86400") {
		t.Errorf("GET /qr/%s.png after a later form claimed it Cache-Control = %q, want the public cache: "+
			"the requested slug IS a live slug again, so the bytes are a function of this key", oldSlug, cc)
	}
}
