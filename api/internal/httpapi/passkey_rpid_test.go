package httpapi

// Card t_bca19fe1 — the passkey relying party (RP) identity must come from the
// DEPLOYMENT, not from a literal.
//
// THE DEFECT. webAuthnInstance answered `RPID: "jomform.com"` for every request
// host, so a ceremony could only ever complete on a *.jomform.com origin — and
// the browser, not the server, was what refused it: WebAuthn binds a ceremony to
// the page's origin (the RP ID must be equal to the origin's effective domain or
// a registrable domain suffix of it, WebAuthn Level 2 §5.1.3 step 8), which is a
// SecurityError raised before an authenticator is reached. Measured live on
// https://jomform-staging.example.com: `POST /v1/auth/passkeys/login/start`
// answered `"rpId":"jomform.com"` for a page at a different origin and
// `navigator.credentials.get()` threw "The relying party ID is not a registrable
// domain suffix of, nor equal to the current domain".
//
// WHAT IS PINNED HERE, and why each half is needed:
//
//   - TestPasskeyRPIdentityFollowsTheConfiguredAppBaseURL is the table over the
//     derivation itself, including the two shapes a naive suffix test gets wrong
//     ("eviljomform.com" and "jomform.com.evil.example") and the staging shape
//     the card measured.
//
//   - TestPasskeyCeremonyIsRefusedOffTheDeploymentsDomain drives the REAL
//     /v1/auth/passkeys/login/start endpoint, because a green unit test on the
//     derivation proves nothing about whether the handler USES it. Both
//     directions bite against the pre-fix code: the staging arm asserted an RP
//     ID the deployment owns (pre-fix: "jomform.com" — the defect verbatim) and
//     the foreign-host arm demanded a refusal (pre-fix: 200, with options that
//     silently implied the deployment's RP ID was available to a host it does not
//     own; the origin allow-list was `"https://" + r.Host`).
//
// Skipped without JOMFORM_TEST_DATABASE_URL (the challenge store is Postgres).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// stagingHost is the origin this card measured the defect at, and the value its
// staging deployment sets APP_BASE_URL to (dokploy compose for the staging
// stack). A deployment on a host that is not jomform.com is the whole point.
const stagingHost = "jomform-staging.example.com"

func TestPasskeyRPIdentityFollowsTheConfiguredAppBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		appBaseURL  string
		requestHost string
		wantRPID    string
		wantOrigins []string
		wantRefused bool
	}{
		{
			// Production: APP_BASE_URL is the app's own domain, so the RP ID is
			// unchanged from what prod always needed — the fix must not move it.
			name: "prod app host", appBaseURL: "https://jomform.com", requestHost: "jomform.com",
			wantRPID: "jomform.com", wantOrigins: []string{"https://jomform.com"},
		},
		{
			// A product subdomain (issue #140) still runs the ceremony: the
			// session cookie is shared with it, so the dashboard — and the
			// ceremony — really can be on mcp.jomform.com.
			name: "prod product subdomain", appBaseURL: "https://jomform.com", requestHost: "mcp.jomform.com",
			wantRPID: "jomform.com",
			wantOrigins: []string{
				"https://jomform.com", "https://mcp.jomform.com",
			},
		},
		{
			// The port is part of an origin, and RPOrigins matches exactly.
			name: "subdomain with a port", appBaseURL: "https://jomform.com", requestHost: "kedai.jomform.com:8443",
			wantRPID: "jomform.com",
			wantOrigins: []string{
				"https://jomform.com", "https://kedai.jomform.com:8443",
			},
		},
		{
			// THE CARD. Pre-fix this returned "jomform.com", which the browser
			// rejected for the staging origin outright.
			name: "staging deployment", appBaseURL: "https://" + stagingHost, requestHost: stagingHost,
			wantRPID: stagingHost, wantOrigins: []string{"https://" + stagingHost},
		},
		{
			// The RP ID is the app host, not the registrable domain: the same
			// WebAuthn rule would also have permitted "example.com", which
			// would scope the credentials to every unrelated service on that host.
			name: "staging subdomain", appBaseURL: "https://" + stagingHost, requestHost: "shop." + stagingHost,
			wantRPID: stagingHost,
			wantOrigins: []string{
				"https://" + stagingHost, "https://shop." + stagingHost,
			},
		},
		{
			// Dev over plain HTTP: the spec allows an http origin whose host is
			// localhost, and the configured scheme is what the origin carries
			// (the pre-fix code hard-coded https, which no dev browser accepts).
			name: "dev localhost", appBaseURL: "http://localhost:8080", requestHost: "localhost:8080",
			wantRPID: "localhost", wantOrigins: []string{"http://localhost:8080"},
		},
		{
			// Host names are case-insensitive and a browser serialises the
			// origin lower-cased; a Host header is caller-controlled spelling.
			name: "mixed-case host header", appBaseURL: "https://jomform.com", requestHost: "MCP.JomForm.COM",
			wantRPID: "jomform.com",
			wantOrigins: []string{
				"https://jomform.com", "https://mcp.jomform.com",
			},
		},
		{
			// A base URL spelled with a leading "www." is the same site. An RP ID
			// may not be a subdomain of the origin, so keeping "www." would
			// refuse every ceremony at the bare host — where this app is served
			// too (Traefik routes both jomform.com and www.jomform.com).
			name: "www base url, bare host", appBaseURL: "https://www.jomform.com", requestHost: "jomform.com",
			wantRPID: "jomform.com",
			wantOrigins: []string{
				"https://www.jomform.com", "https://jomform.com",
			},
		},
		{
			name: "www base url, www host", appBaseURL: "https://www.jomform.com", requestHost: "www.jomform.com",
			wantRPID: "jomform.com", wantOrigins: []string{"https://www.jomform.com"},
		},
		{
			// The www rule must not trim a host that merely CONTAINS "www.".
			name: "www inside a label", appBaseURL: "https://wwwshop.example.com", requestHost: "wwwshop.example.com",
			wantRPID: "wwwshop.example.com", wantOrigins: []string{"https://wwwshop.example.com"},
		},
		{
			// A suffix test without a label boundary would accept this.
			name: "look-alike domain", appBaseURL: "https://jomform.com", requestHost: "eviljomform.com",
			wantRefused: true,
		},
		{
			// …and a prefix/contains test would accept this.
			name: "app domain as a subdomain of an attacker", appBaseURL: "https://jomform.com",
			requestHost: "jomform.com.evil.example", wantRefused: true,
		},
		{
			name: "unrelated host", appBaseURL: "https://jomform.com", requestHost: "attacker.example",
			wantRefused: true,
		},
		{
			// A staging deployment is refused for a jomform.com host too: the
			// boundary is the CONFIGURED domain, not the prod one.
			name: "prod host at a staging deployment", appBaseURL: "https://" + stagingHost,
			requestHost: "jomform.com", wantRefused: true,
		},
		{
			// No APP_BASE_URL (or one that is not a URL) leaves no RP ID to
			// derive. That is a configuration error, not a caller refusal, and
			// it is reported separately from the two refusal cases above.
			name: "no app base url", appBaseURL: "", requestHost: "jomform.com", wantRefused: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := passkeyRPIdentity(c.appBaseURL, c.requestHost)
			if c.wantRefused {
				if err == nil {
					t.Fatalf("passkeyRPIdentity(%q, %q) = {rpID:%q origins:%v}, want a refusal — a host the "+
						"deployment does not own must not be handed a ceremony", c.appBaseURL, c.requestHost,
						got.rpID, got.origins)
				}
				// The two failures must stay distinguishable: the handler answers
				// the origin one with 403 and the rest with 500.
				if c.appBaseURL == "" && err == errPasskeyOriginOutsideApp {
					t.Errorf("an unusable APP_BASE_URL was reported as a foreign origin — the handler would "+
						"answer 403 for this server's own misconfiguration (err=%v)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("passkeyRPIdentity(%q, %q) refused: %v", c.appBaseURL, c.requestHost, err)
			}
			if got.rpID != c.wantRPID {
				t.Errorf("rpID = %q, want %q — the RP ID must be the configured app domain: a literal (or the "+
					"wrong domain) cannot be a registrable domain suffix of the page's origin, and the BROWSER "+
					"refuses the ceremony with a SecurityError before an authenticator is reached",
					got.rpID, c.wantRPID)
			}
			if !reflect.DeepEqual(got.origins, c.wantOrigins) {
				t.Errorf("origins = %v, want %v (order matters here only for readability: the configured "+
					"origin first, then the request's own)", got.origins, c.wantOrigins)
			}
		})
	}
}

// TestPasskeyCeremonyIsRefusedOffTheDeploymentsDomain drives the REAL
// login/start endpoint with the two host shapes that matter: the deployment's own
// domain (must mint a ceremony whose RP ID belongs to it) and a host it does not
// own (must be refused, with no options in the body at all).
//
// The foreign arm is the guard the card asks for. Pre-fix it answered 200 with
// `"rpId":"jomform.com"` and an origin allow-list built from the caller's own
// Host header, so nothing but the browser's SecurityError stood between a foreign
// origin and the deployment's RP ID.
func TestPasskeyCeremonyIsRefusedOffTheDeploymentsDomain(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set")
	}
	pool := bundleTestPool(t)
	h := passkeyHandlers(t, pool)
	// The staging deployment's configuration, verbatim: this is the shape that
	// could not run a ceremony at all before the fix.
	h.BaseURL = "https://" + stagingHost
	h.Passkeys = dbstore.NewPasskeyStore(pool)

	start := func(t *testing.T, host string) *httptest.ResponseRecorder {
		t.Helper()
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/v1/auth/passkeys/login/start", nil)
		req.Header.Set("X-Real-IP", uniqueTestIP())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// 1. The deployment's own host: the ceremony is minted, and the RP ID in the
	//    options is the deployment's domain — the value the browser will hash
	//    into rpIdHash and the one this card's defect got wrong.
	rec := start(t, stagingHost)
	if rec.Code != http.StatusOK {
		t.Fatalf("login/start at the deployment's own host = %d (body %s), want 200", rec.Code, rec.Body.String())
	}
	var ok struct {
		Key     string `json:"key"`
		Options struct {
			PublicKey struct {
				RPID      string `json:"rpId"`
				Challenge string `json:"challenge"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ok); err != nil {
		t.Fatalf("decode login/start body: %v (%s)", err, rec.Body.String())
	}
	if ok.Key == "" || ok.Options.PublicKey.Challenge == "" {
		t.Fatalf("login/start answered 200 without a usable ceremony: %s", rec.Body.String())
	}
	if ok.Options.PublicKey.RPID != stagingHost {
		t.Errorf("login/start at %s returned rpId %q, want %q — the page's origin is https://%s, and a browser "+
			"refuses any RP ID that is not equal to (or a registrable domain suffix of) that origin's effective "+
			"domain, with a SecurityError that reaches the user as \"Passkey cancelled or not supported by this "+
			"browser\". This is the card's defect reproduced at the seam.",
			stagingHost, ok.Options.PublicKey.RPID, stagingHost, stagingHost)
	}

	// 2. A subdomain of the deployment is NOT a foreign origin: subdomain
	//    ceremonies must keep working (the session cookie is shared with them).
	sub := "shop." + stagingHost
	rec = start(t, sub)
	if rec.Code != http.StatusOK {
		t.Fatalf("login/start at a subdomain of the deployment (%s) = %d (body %s), want 200", sub, rec.Code, rec.Body.String())
	}
	var subBody struct {
		Options struct {
			PublicKey struct {
				RPID string `json:"rpId"`
			} `json:"publicKey"`
		} `json:"options"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &subBody); err != nil {
		t.Fatalf("decode subdomain login/start body: %v", err)
	}
	if subBody.Options.PublicKey.RPID != stagingHost {
		t.Errorf("the ceremony at %s is bound to RP ID %q, want %q — a subdomain shares the app's RP ID",
			sub, subBody.Options.PublicKey.RPID, stagingHost)
	}

	// 3. A host this deployment does not own: refused, and refused WITHOUT
	//    handing out a challenge or options.
	rec = start(t, "attacker.example")
	if rec.Code != http.StatusForbidden {
		t.Errorf("login/start at a foreign host = %d (body %s), want 403 — a passkey ceremony must not be "+
			"offered at an origin the deployment does not own", rec.Code, rec.Body.String())
	}
	var refusal struct {
		Error     string          `json:"error"`
		Code      string          `json:"code"`
		Options   json.RawMessage `json:"options"`
		Key       string          `json:"key"`
		Challenge json.RawMessage `json:"challenge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
		t.Fatalf("decode refusal body: %v (%s)", err, rec.Body.String())
	}
	if refusal.Options != nil || refusal.Key != "" || refusal.Challenge != nil {
		t.Errorf("the refusal body still carries ceremony material (options=%s key=%q challenge=%s) — a refused "+
			"origin must get nothing it could try to complete", refusal.Options, refusal.Key, refusal.Challenge)
	}
	if refusal.Code == "" {
		t.Errorf("the refusal carries no machine code: %s (issue #6177)", rec.Body.String())
	}
}

// TestPasskeyHandlersRefuseWhenNoRPIDCanBeDerived keeps the other half of the
// boundary: a deployment that cannot name its own domain (APP_BASE_URL unset or
// unusable) must not fall back to trusting the caller's Host header — the exact
// weakness the literal replaced. It answers the server-side 500 rather than
// handing out options bound to whatever domain arrived.
func TestPasskeyHandlersRefuseWhenNoRPIDCanBeDerived(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set")
	}
	pool := bundleTestPool(t)
	h := passkeyHandlers(t, pool)
	h.BaseURL = ""
	h.Passkeys = dbstore.NewPasskeyStore(pool)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "http://"+stagingHost+"/v1/auth/passkeys/login/start", nil)
	req.Header.Set("X-Real-IP", uniqueTestIP())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("login/start with no APP_BASE_URL = %d (body %s), want 500 — there is no RP ID to derive, and "+
			"the request's own Host must not become it", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "webauthn config error") {
		t.Errorf("the 500 body does not name the configuration problem: %s", rec.Body.String())
	}
}
