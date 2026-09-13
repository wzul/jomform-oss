package httpapi

// Issue #539: fetchUserInfo must send the request it built (the previous
// http.Get(req.URL) dropped the Authorization header), enforce the provider's
// email_verified claim, and — for a provider with a dedicated emails endpoint
// (GitHub /user/emails) — treat that endpoint as authoritative instead of
// silently decoding a missing claim as false-yet-still-verified.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIssue539UserInfoSendsBearerToken proves the userinfo call is authenticated.
func TestIssue539UserInfoSendsBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub": "u1", "email": "a@example.test", "email_verified": true, "name": "A",
		})
	}))
	defer srv.Close()

	p := oidcProvider{UserURL: srv.URL}
	if _, err := fetchUserInfo(p, "tok-abc"); err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Fatalf("issue #539: userinfo request carried Authorization=%q, want \"Bearer tok-abc\" — "+
			"http.Get(req.URL) dropped the header, making the call unauthenticated", gotAuth)
	}
}

// TestIssue539UserInfoHonoursUnverifiedClaim proves a provider asserting
// email_verified=false is not treated as verified.
func TestIssue539UserInfoHonoursUnverifiedClaim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub": "u2", "email": "b@example.test", "email_verified": false, "name": "B",
		})
	}))
	defer srv.Close()

	p := oidcProvider{UserURL: srv.URL}
	up, err := fetchUserInfo(p, "tok")
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if up.EmailVerified {
		t.Fatal("issue #539: a provider asserting email_verified=false must not yield a verified profile")
	}
}

// TestIssue539GithubEmailsEndpointIsAuthoritative covers the GitHub shape: /user
// returns no email_verified and often a null email, so /user/emails decides.
func TestIssue539GithubEmailsEndpointIsAuthoritative(t *testing.T) {
	mux := http.NewServeMux()
	// GitHub /user: address hidden, no email_verified field at all.
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login": "octocat", "name": "Octo", "avatar_url": "https://a/x.png",
		})
	})
	// GitHub /user/emails: primary + verified.
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "secondary@example.test", "primary": false, "verified": true},
			{"email": "Primary@Example.test", "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := oidcProvider{UserURL: srv.URL + "/user", EmailsURL: srv.URL + "/user/emails"}
	up, err := fetchUserInfo(p, "tok")
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if !up.EmailVerified {
		t.Fatal("issue #539: a verified primary email from /user/emails must yield verified=true")
	}
	if !strings.EqualFold(up.Email, "primary@example.test") {
		t.Fatalf("email = %q, want the primary address (lowercased)", up.Email)
	}
	if up.UID != "octocat" {
		t.Fatalf("uid = %q, want the login fallback", up.UID)
	}
}

// TestIssue539GithubUnverifiedPrimaryIsNotTrusted proves the fail-closed branch
// and that the authoritative endpoint WINS over the profile-level claim.
//
// The distinguishing case: the profile claims email_verified=true while the
// dedicated emails endpoint reports the primary address as UNVERIFIED. The
// authoritative source must win, so the result must be verified=false. (A
// profile that merely omits the claim cannot distinguish the code paths, so this
// test deliberately has the profile assert the claim.)
func TestIssue539GithubUnverifiedPrimaryIsNotTrusted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login": "octo2", "email": "x@example.test", "email_verified": true,
		})
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "unverified@example.test", "primary": true, "verified": false},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := oidcProvider{UserURL: srv.URL + "/user", EmailsURL: srv.URL + "/user/emails"}
	up, err := fetchUserInfo(p, "tok")
	if err != nil {
		t.Fatalf("fetchUserInfo: %v", err)
	}
	if up.EmailVerified {
		t.Fatal("issue #539: the profile claimed email_verified=true but the authoritative " +
			"emails endpoint reports the primary address as unverified — the endpoint must " +
			"win, so this must not be treated as verified")
	}
}

// TestIssue539NoEmailIsRejected proves a provider that yields no usable address
// fails closed rather than creating an account without one.
func TestIssue539NoEmailIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": "u3", "name": "No Email"})
	}))
	defer srv.Close()

	if _, err := fetchUserInfo(oidcProvider{UserURL: srv.URL}, "tok"); err == nil {
		t.Fatal("issue #539: a userinfo response with no email must be rejected")
	}
}
