// Language placement — integration coverage for the PER-PROFILE dashboard
// language (users.lang).
//
// WHY THIS EXISTS SEPARATELY FROM THE RENDERER TESTS
//   public.go / formLang.go tests pin what a BUYER sees. This file pins the
//   MERCHANT's own setting, which is the other half of the change: the dashboard
//   language used to live only in the browser's localStorage, so it was lost on
//   every new machine and never followed the user.
//
// WHAT IS PINNED
//   1. users.lang survives a real DB write+read through AccountStore.ByID
//      (issue: the repo's silent-401 class — a SELECT list and its Scan
//      arguments drifting out of column alignment — would show up here as a
//      wrong/zero value or a scan error, not as a compile failure).
//   2. An unsupported code is refused, and clearing ("" = Auto) works.
//   3. The /v1/auth/profile/lang route persists and the value comes back on the
//      profile read, so the web client can hydrate from it.
//
// The route half runs through the real handler+mux; it needs Postgres and is
// skipped without it (the fixture skips itself).

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUserLangRoundTripsThroughTheStore is the column-alignment guard for the
// new users.lang column: it writes through the store and reads back through the
// store's own select, so a SELECT/Scan mismatch fails here rather than on a
// signed-in production request.
func TestUserLangRoundTripsThroughTheStore(t *testing.T) {
	users := authUsers(t) // skips without a test DB
	ctx := t.Context()
	accountID, userID := bundleFixture(t, ctx, bundleTestPool(t))

	if accountID == "" || userID == "" {
		t.Fatal("fixture did not return an account/user id")
	}

	// Start from "not set" so the assertion cannot pass on a stale row.
	if err := users.UpdateLang(ctx, userID, ""); err != nil {
		t.Fatalf("clear lang: %v", err)
	}
	u, err := users.ByID(ctx, userID)
	if err != nil {
		t.Fatalf("ByID after clear: %v", err)
	}
	if u.Lang != "" {
		t.Fatalf("cleared lang read back as %q, want empty", u.Lang)
	}

	for _, code := range []string{"en", "ms", "zh", "ta", "ar"} {
		if err := users.UpdateLang(ctx, userID, code); err != nil {
			t.Fatalf("UpdateLang(%q): %v", code, err)
		}
		u, err := users.ByID(ctx, userID)
		if err != nil {
			t.Fatalf("ByID after UpdateLang(%q): %v", code, err)
		}
		if u.Lang != code {
			t.Errorf("lang round-trip: wrote %q, read %q", code, u.Lang)
		}
	}

	// An unsupported code must not reach the column (its CHECK would reject it);
	// the store clears instead, so the dashboard falls back rather than erroring.
	if err := users.UpdateLang(ctx, userID, "fr"); err != nil {
		t.Fatalf("UpdateLang(fr) should clear, not error: %v", err)
	}
	u, err = users.ByID(ctx, userID)
	if err != nil {
		t.Fatalf("ByID after UpdateLang(fr): %v", err)
	}
	if u.Lang != "" {
		t.Errorf("unsupported code stored as %q, want empty", u.Lang)
	}
}

// TestProfileLangRoutePersistsAndReadsBack drives the REST route the dashboard
// uses: POST /v1/auth/profile/lang then GET the profile. This is the path that
// makes the choice follow the user into another browser.
func TestProfileLangRoutePersistsAndReadsBack(t *testing.T) {
	h, users, _ := authTestFixture(t) // skips without DB + Valkey
	ctx := t.Context()
	accountID, userID := bundleFixture(t, ctx, bundleTestPool(t))
	if accountID == "" || userID == "" {
		t.Fatal("fixture did not return an account/user id")
	}

	// Authenticate a real session for the fixture user.
	tok := sessionForUser(t, h, userID)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := post("/v1/auth/profile/lang", `{"lang":"ta"}`); rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/auth/profile/lang status = %d (body %s)", rec.Code, rec.Body.String())
	}

	// The value must be readable back through the profile endpoint the web
	// client hydrates from.
	rec := get("/v1/auth/profile")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/auth/profile status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Lang string `json:"lang"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("profile JSON: %v (body %s)", err, rec.Body.String())
	}
	if body.Lang != "ta" {
		t.Errorf("profile lang = %q, want \"ta\"", body.Lang)
	}

	// An unsupported code is a 400, not a silent store.
	if rec := post("/v1/auth/profile/lang", `{"lang":"fr"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST unsupported lang status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	// Clearing is how a user returns to "Auto".
	if rec := post("/v1/auth/profile/lang", `{"lang":""}`); rec.Code != http.StatusOK {
		t.Fatalf("POST clear lang status = %d (body %s)", rec.Code, rec.Body.String())
	}
	u, err := users.ByID(ctx, userID)
	if err != nil {
		t.Fatalf("ByID after clear: %v", err)
	}
	if u.Lang != "" {
		t.Errorf("cleared lang = %q, want empty", u.Lang)
	}
}
