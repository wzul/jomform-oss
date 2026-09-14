package httpapi

// Issue #549: the confirmation `redirect_url` check must go through the shared
// urlguard rule, so it accepts exactly what every other URL field accepts.
//
// This drives the REAL REST handler (PUT /v1/rest/forms/{id}/confirmations), not a
// helper, so it guards the actual write path. It FAILS against the pre-fix code,
// which used a hand-rolled `strings.HasPrefix(v, "https://")` and therefore
// rejected "HTTPS://…" and whitespace-padded URLs.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// redirectPut sends a confirmations payload through the real authed handler.
func redirectPut(t *testing.T, h *RESTHandlers, rawKey, formID string, confs []map[string]any) (int, string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"confirmations": confs})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/rest/forms/"+formID+"/confirmations", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestIssue549RedirectAcceptsWhatUrlguardAccepts is the core guard.
func TestIssue549RedirectAcceptsWhatUrlguardAccepts(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	form := &dbstore.Form{AccountID: accountID, Title: "I549", Slug: "i549-" + randSuffix(), Status: "draft"}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	// Values every URL field in the product accepts (urlguard trims + lowercases).
	mustAccept := []string{
		"https://example.com/thanks",
		"HTTPS://example.com/thanks",
		"Https://Example.com/Thanks",
		"  https://example.com/thanks  ",
	}
	for _, u := range mustAccept {
		code, body := redirectPut(t, h, raw, form.ID, []map[string]any{{
			"name": "Redirect", "type": "redirect", "is_default": true, "redirect_url": u,
		}})
		if code != http.StatusOK {
			t.Errorf("issue #549: redirect_url %q rejected with %d (%s) — the shared rule "+
				"accepts it, so every other URL field does too; the redirect check must agree",
				u, code, body)
		}
	}
}

// TestIssue549RedirectRejectsUnsafeSchemes proves the fix did not loosen the rule.
func TestIssue549RedirectRejectsUnsafeSchemes(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	form := &dbstore.Form{AccountID: accountID, Title: "I549b", Slug: "i549b-" + randSuffix(), Status: "draft"}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	mustReject := []string{
		"http://example.com",      // plain http must not be allowed for a redirect
		"//evil.example.com",      // protocol-relative
		"javascript:alert(1)",     // scheme injection
		"data:text/html,<b>x</b>", // data URI
		"ftp://example.com",       // wrong scheme
		"HTTPSX://example.com",    // not the https scheme
		"",                        // a redirect REQUIRES a target
		"   ",                     // whitespace-only is still empty
	}
	for _, u := range mustReject {
		code, body := redirectPut(t, h, raw, form.ID, []map[string]any{{
			"name": "Redirect", "type": "redirect", "is_default": true, "redirect_url": u,
		}})
		if code == http.StatusOK {
			t.Errorf("issue #549: redirect_url %q was ACCEPTED, want rejection — a redirect "+
				"must be a non-empty https URL (body %s)", u, body)
		}
	}
}

// TestIssue549RedirectMessageIsSharedWording pins the shared rejection message, so a
// merchant sees the same explanation as on every other URL field.
func TestIssue549RedirectMessageIsSharedWording(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	form := &dbstore.Form{AccountID: accountID, Title: "I549c", Slug: "i549c-" + randSuffix(), Status: "draft"}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}

	_, body := redirectPut(t, h, raw, form.ID, []map[string]any{{
		"name": "Redirect", "type": "redirect", "is_default": true, "redirect_url": "javascript:alert(1)",
	}})
	for _, want := range []string{"redirect_url", "https://"} {
		if !bytes.Contains([]byte(body), []byte(want)) {
			t.Errorf("issue #549: rejection body %q should contain %q (the shared urlguard wording)", body, want)
		}
	}
	// The old ad-hoc wording must be gone.
	if bytes.Contains([]byte(body), []byte("URL pengesahan mesti https://")) {
		t.Error("issue #549: the old hand-rolled message is still returned for redirect_url")
	}
}

// TestIssue549MessageTypeNeedsNoRedirect keeps the message-type path working.
func TestIssue549MessageTypeNeedsNoRedirect(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	accountID, _ := bundleFixture(t, ctx, pool)
	h := newBundleHandlers(pool)
	raw := bundleAPIKey(t, ctx, pool, accountID)

	form := &dbstore.Form{AccountID: accountID, Title: "I549d", Slug: "i549d-" + randSuffix(), Status: "draft"}
	if err := dbstore.NewFormStore(pool).Create(ctx, form); err != nil {
		t.Fatalf("create form: %v", err)
	}
	code, body := redirectPut(t, h, raw, form.ID, []map[string]any{{
		"name": "Thanks", "type": "message", "is_default": true, "message": "Terima kasih!",
	}})
	if code != http.StatusOK {
		t.Errorf("a message confirmation needs no redirect_url, got %d (%s)", code, body)
	}
}
