package httpapi

// The og:image in-process cache key must carry EVERY input the card render
// depends on. It did not: it was keyed on the form ID alone
//
//	var ogImageCache sync.Map // formID -> []byte
//	if v, ok := ogImageCache.Load(formID); ok { return v.([]byte), nil }
//
// while renderOGImage draws the request HOST (the subtitle's domain), the
// workspace name, the form TITLE, the THEME colour (the gradient tint) and the
// form's LOGO — five arguments the key ignored. So the first render for a form
// id was served for every later request for that form, whatever the request
// said:
//
//   - the same form on a second host (its workspace subdomain and a verified
//     custom domain are two resolvable hosts for one form id) got the FIRST
//     host's card, so the preview named an address that had not been requested;
//   - a title / theme / logo change was never reflected — the entry has no TTL
//     and no invalidation, so it survived for the life of the process, and the
//     CDN then held the same bytes for another 24h under the 24h header.
//
// Measured on tree a596504e (2026-10-13): two cachedOGImage calls for one form
// id with different hosts/workspaces/themes returned the FIRST render's bytes
// (52305 bytes, bytes.Equal == true). Re-measured on this card's base tree with
// the guard below, through the real mux: two hosts served byte-identical cards
// (58686 bytes), and after a title+theme PATCH the same bytes came back.
//
// The assertions below are the missing guard, and they are deliberately two
// sided: distinct render inputs must MISS the cache (a wrong key is the
// defect), and identical inputs must HIT it (deleting the cache is not a fix —
// the endpoint exists to keep social crawlers off the renderer, and the render
// does an outbound logo GET). A cache hit is observed the only way it can be
// observed from outside: the caller gets back the SAME slice, so the two
// results share a backing array and no render happened in between. Bytes alone
// cannot tell a hit from a re-render, which is why the pointer is asserted. The
// bound on the cache (ogImageCacheMax) is asserted separately, with tiny
// payloads, because filling it with real renders would cost seconds.
//
// Runs against a real Postgres (JOMFORM_TEST_DATABASE_URL) — the HTTP-level
// cases resolve a form through the real stores, because a fake store would let
// the guard pass while the served card followed the wrong host.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestOGImageCacheIsBounded pins that the process cache cannot grow without
// limit. The full key means every edit of a form (title / theme / logo) is a new
// key and the superseded entry is never read again, so an unbounded map would
// hold every revision ever rendered — ~50KB per card, for the life of the
// process. Tiny payloads are stored directly so this costs nothing.
func TestOGImageCacheIsBounded(t *testing.T) {
	keys := make([]ogImageCacheKey, 0, ogImageCacheMax+2)
	for i := 0; i < ogImageCacheMax+2; i++ {
		key := ogImageCacheKey{formID: "it-ogbound-" + randHex(6)}
		keys = append(keys, key)
		ogImageCachePut(key, []byte{byte(i)})
	}

	ogImageCacheMu.Lock()
	size := len(ogImageCache)
	ogImageCacheMu.Unlock()
	if size > ogImageCacheMax {
		t.Errorf("the og:image cache holds %d entries after %d distinct renders, want at most %d: "+
			"the full key makes every form EDIT a new entry, so nothing bounds the process memory here",
			size, len(keys), ogImageCacheMax)
	}
	// Eviction is FIFO, and it must never look like a wrong answer: the two oldest
	// keys are the ones dropped (whatever the cache held before this test), the
	// newest is still readable.
	if _, hit := ogImageCacheGet(keys[0]); hit {
		t.Errorf("the OLDEST entry survived while the cap was exceeded — eviction is not FIFO, or " +
			"nothing evicts at all")
	}
	// A re-put of a live key refreshes the entry rather than adding one.
	ogImageCachePut(keys[len(keys)-1], []byte{0xff})
	if got, hit := ogImageCacheGet(keys[len(keys)-1]); !hit || got[0] != 0xff {
		t.Errorf("the newest entry is not readable after the cap was enforced")
	}
}

// ogTestLogoPNG encodes a solid-colour PNG, so two logo URLs can be made to
// render visibly different cards without a network dependency.
func ogTestLogoPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// ogArgs is one render's inputs, named so a failure can say WHICH input the key
// dropped instead of printing six anonymous strings.
type ogArgs struct {
	formID, title, workspace, domain, theme, logo string
}

func (a ogArgs) cached(t *testing.T) []byte {
	t.Helper()
	png, err := cachedOGImage(a.formID, a.title, a.workspace, a.domain, a.theme, a.logo)
	if err != nil {
		t.Fatalf("cachedOGImage(%+v): %v", a, err)
	}
	if len(png) == 0 {
		t.Fatalf("cachedOGImage(%+v) returned no bytes", a)
	}
	return png
}

func (a ogArgs) fresh(t *testing.T) []byte {
	t.Helper()
	png, err := renderOGImage(a.title, a.workspace, a.domain, a.theme, a.logo)
	if err != nil {
		t.Fatalf("renderOGImage(%+v): %v", a, err)
	}
	return png
}

// ogServed fetches /og/{slug}.png on host through the real public mux.
func ogServed(t *testing.T, h *PublicHandlers, host, slug string) []byte {
	t.Helper()
	rec := publicServe(t, h, host, "/og/"+slug+".png")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /og/%s.png on %s = %d, want 200 (body %s)", slug, host, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("GET /og/%s.png Content-Type = %q, want image/png", slug, ct)
	}
	return rec.Body.Bytes()
}

// ogStoredCard renders the card the STORED state of f must produce when it is
// requested on host, using the handler's own subtitle inputs. renderOGImage is
// deterministic for fixed inputs (the same reasoning qr_cache_slug_rename_test.go
// uses for qrcode.Encode), so byte equality against this is a statement about
// the card's CONTENT — title, subtitle/host, theme tint, logo — not about
// renderer noise.
func ogStoredCard(t *testing.T, h *PublicHandlers, ctx context.Context, f *dbstore.Form, host string) []byte {
	t.Helper()
	themePrimary := ""
	if f.Theme != nil {
		if v, ok := f.Theme["primary"].(string); ok {
			themePrimary = v
		}
	}
	png, err := renderOGImage(f.Title, h.workspaceNameFor(ctx, f), host, themePrimary, f.LogoURL)
	if err != nil {
		t.Fatalf("renderOGImage (expected card for %s): %v", host, err)
	}
	return png
}

// TestCachedOGImageKeyCarriesEveryRenderInput is the card's own repro, widened
// to every input the render reads: same form id, one input varied at a time.
func TestCachedOGImageKeyCarriesEveryRenderInput(t *testing.T) {
	logos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "red.png":
			w.Write(ogTestLogoPNG(t, color.RGBA{0xff, 0x00, 0x00, 0xff}))
		case "blue.png":
			w.Write(ogTestLogoPNG(t, color.RGBA{0x00, 0x00, 0xff, 0xff}))
		default:
			http.Error(w, "no logo", http.StatusNotFound)
		}
	}))
	defer logos.Close()

	formID := "it-ogkey-" + randHex(8)
	base := ogArgs{
		formID:    formID,
		title:     "Menu A",
		workspace: "Workspace One",
		domain:    "alpha.example.test",
		theme:     "#5e6ad2",
		logo:      logos.URL + "/red.png",
	}

	first := base.cached(t)
	if !bytes.Equal(first, base.fresh(t)) {
		t.Fatalf("the first cachedOGImage call did not return this render's bytes")
	}

	// Identical inputs MUST hit: the caller gets the same slice back, so no
	// render ran and the cache is still a cache.
	again := base.cached(t)
	if &again[0] != &first[0] {
		t.Errorf("two cachedOGImage calls with IDENTICAL inputs returned different slices "+
			"(%d bytes and %d bytes): the entry was re-rendered instead of served from the cache — "+
			"the endpoint exists to keep a crawler storm off the renderer and off the "+
			"outbound logo GET", len(first), len(again))
	}

	variants := []struct {
		what string
		args ogArgs
	}{
		{"the TITLE", ogArgs{formID, "Menu B", base.workspace, base.domain, base.theme, base.logo}},
		{"the WORKSPACE name", ogArgs{formID, base.title, "Workspace Two", base.domain, base.theme, base.logo}},
		{"the request HOST (the subtitle domain)", ogArgs{formID, base.title, base.workspace, "beta.example.test", base.theme, base.logo}},
		{"the THEME colour", ogArgs{formID, base.title, base.workspace, base.domain, "#ff0000", base.logo}},
		{"the LOGO url", ogArgs{formID, base.title, base.workspace, base.domain, base.theme, logos.URL + "/blue.png"}},
	}
	for _, v := range variants {
		// Precondition: the varied input really changes the CARD. Without this
		// the case below would pass vacuously (a key that ignores an input the
		// render also ignores cannot be caught by comparing bytes).
		if bytes.Equal(v.args.fresh(t), base.fresh(t)) {
			t.Fatalf("precondition failed: changing %s does not change the rendered card, so this "+
				"case cannot see the cache key", v.what)
		}
		got := v.args.cached(t)
		if bytes.Equal(got, first) {
			t.Errorf("changed %s and the cache returned the cached card byte-for-byte (%d bytes).\n"+
				"The entry is keyed on the form id alone, so the first render for a form is served "+
				"for every later request for it: the card names a host / title / colour / logo that "+
				"was not asked for, and (with no TTL or invalidation) it stays that way for the life "+
				"of the process. Key the entry on everything the render depends on.",
				v.what, len(got))
			continue
		}
		if want := v.args.fresh(t); !bytes.Equal(got, want) {
			t.Errorf("changed %s and the served card is neither the cached one nor a render of the "+
				"new inputs — the key and the render disagree", v.what)
		}
	}

	// The variants must not have displaced the base entry: each distinct input
	// set is its OWN entry, not a shared slot.
	if got := base.cached(t); &got[0] != &first[0] {
		t.Errorf("after rendering other input sets for the same form id, the base inputs no longer " +
			"hit their own entry — the entries share a slot instead of being keyed per input set")
	}
}

// ogSetBusinessName gives the fixture's workspace a SHORT business name, so the
// card's subtitle renders as one wrapped line — "<name> · <host>" — and the
// requested host is on the card at all (see ogShortenHosts).
func ogSetBusinessName(t *testing.T, fx renameFixture, name string) {
	t.Helper()
	if _, err := fx.pool.Exec(fx.ctx,
		`INSERT INTO workspace_settings (workspace_id, business_name)
		 SELECT workspace_id, $2 FROM accounts WHERE id=$1
		 ON CONFLICT DO NOTHING`, fx.accountID, name); err != nil {
		t.Fatalf("create workspace_settings: %v", err)
	}
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE workspace_settings SET business_name=$2
		 WHERE workspace_id = (SELECT workspace_id FROM accounts WHERE id=$1)`,
		fx.accountID, name); err != nil {
		t.Fatalf("set business_name: %v", err)
	}
}

// ogShortenHosts rewrites the fixture's two hostnames to SHORT ones in place and
// returns them.
//
// The assertions below need the requested HOST to reach the rendered card at all.
// renderOGImage wraps the subtitle ("<workspace> · <host>") to one line and keeps
// only that line (subLines[:1]), so a pair that does not fit ~900px loses the
// domain to the dropped second line. newRenameFixture's slugs are 35+ random hex
// characters, which is long enough for the domain to always be the dropped line:
// MEASURED with the fixture's own slug, two FRESH renders (cache bypassed) of the
// same form on two different hosts are byte-identical — 56017 bytes each — so the
// host never reaches the card and the comparison below would pass whatever the
// cache key did. A real workspace slug and a real custom domain are short, so the
// fixture's own freshly-seeded rows are shortened here; nothing else reads those
// two columns.
//
// (The truncation itself is a separate, pre-existing rendering behaviour: with a
// short workspace name the two hosts differ, as this test asserts, and with a
// name long enough to wrap the card simply never names the address.)
func ogShortenHosts(t *testing.T, fx renameFixture) (string, string) {
	t.Helper()
	wsSlug := "og" + randHex(3)
	domain := "og-" + randHex(3) + ".example.test"
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE workspaces SET slug=$1 WHERE slug=$2`, wsSlug, fx.workspaceSlug); err != nil {
		t.Fatalf("shorten the workspace slug: %v", err)
	}
	if _, err := fx.pool.Exec(fx.ctx,
		`UPDATE account_domains SET domain=$1 WHERE account_id=$2 AND domain=$3`,
		domain, fx.accountID, fx.customDomain); err != nil {
		t.Fatalf("shorten the custom domain: %v", err)
	}
	return wsSlug + ".jomform.com", domain
}

// TestOGImageFollowsTheRequestedHost is the headline defect end to end: one
// form, two hosts that both really resolve it (its workspace subdomain and a
// verified custom domain). The card is rendered with the request Host as the
// subtitle, so each host must get ITS OWN card.
func TestOGImageFollowsTheRequestedHost(t *testing.T) {
	fx := newRenameFixture(t, "it-oghost-"+randHex(4))
	ogSetBusinessName(t, fx, "Kedai Ali")
	workspaceHost, customHost := ogShortenHosts(t, fx)
	f, err := dbstore.NewFormStore(fx.pool).GetByID(fx.ctx, fx.accountID, fx.formID)
	if err != nil {
		t.Fatalf("read the form back: %v", err)
	}

	// The workspace subdomain is requested FIRST, so it is its bytes the
	// mis-keyed cache would hand to the custom domain second.
	hosts := []string{workspaceHost, customHost}
	served := map[string][]byte{}
	for _, host := range hosts {
		body := ogServed(t, fx.pub, host, fx.slug)
		want := ogStoredCard(t, fx.pub, fx.ctx, f, host)
		if !bytes.Equal(body, want) {
			t.Errorf("GET /og/%s.png on %s served a card that is not the render for THIS host.\n"+
				"The card's subtitle names its domain and the cache is keyed on the form id alone, so "+
				"the first host served for a form is served for every host afterwards — a social "+
				"preview advertising an address that was not requested. Key on the host too.",
				fx.slug, host)
		}
		served[host] = body
	}
	if bytes.Equal(served[hosts[0]], served[hosts[1]]) {
		t.Errorf("GET /og/%s.png returned byte-identical cards on %s and %s (%d bytes): one host is "+
			"being served the other host's card", fx.slug, hosts[0], hosts[1], len(served[hosts[0]]))
	}
}

// TestOGImageFollowsATitleAndThemeChange is the other half: the card follows
// the form's STORED state, so an edit is reflected on the next fetch from the
// same host (the process cache had no TTL and no invalidation).
func TestOGImageFollowsATitleAndThemeChange(t *testing.T) {
	fx := newRenameFixture(t, "it-ogedit-"+randHex(4))
	ogSetBusinessName(t, fx, "Kedai Ali")
	host, _ := ogShortenHosts(t, fx)
	fs := dbstore.NewFormStore(fx.pool)

	before := ogServed(t, fx.pub, host, fx.slug)
	if f, err := fs.GetByID(fx.ctx, fx.accountID, fx.formID); err != nil {
		t.Fatalf("read the form back: %v", err)
	} else if want := ogStoredCard(t, fx.pub, fx.ctx, f, host); !bytes.Equal(before, want) {
		t.Fatalf("the card served before the edit is not a render of the stored form")
	}

	// The merchant's own save path (form-settings POSTs title + theme).
	if code, raw := fx.patch(t, `{"slug":"`+fx.slug+`","title":"Eastern menu","theme":{"primary":"#ff0000"}}`); code != http.StatusOK {
		t.Fatalf("PATCH title+theme = %d, want 200 (body %s)", code, raw)
	}

	f, err := fs.GetByID(fx.ctx, fx.accountID, fx.formID)
	if err != nil {
		t.Fatalf("read the form back after the edit: %v", err)
	}
	if f.Title != "Eastern menu" {
		t.Fatalf("stored title = %q after the edit, want %q", f.Title, "Eastern menu")
	}

	after := ogServed(t, fx.pub, host, fx.slug)
	want := ogStoredCard(t, fx.pub, fx.ctx, f, host)
	if !bytes.Equal(after, want) {
		t.Errorf("after the title/theme edit GET /og/%s.png on %s still served the PRE-edit card "+
			"(%d bytes, unchanged=%v).\nThe in-process entry is keyed on the form id alone and never "+
			"expires, so a rename, a re-brand or a new logo is not reflected until the process "+
			"restarts — and the 24h Cache-Control on top of it hands the same stale card to the CDN.",
			fx.slug, host, len(after), bytes.Equal(after, before))
	}
}
