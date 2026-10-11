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
//   - TestPasskeyCeremonySurvivesTheProxyHop (card t_ce549b48) is the arm that
//     was MISSING, and its absence is why v1.21.52 took passkey sign-in off prod:
//     every test above drives the handler with the APP host as r.Host, while the
//     product's own dashboard proxy replaces Host with the API's internal address
//     (`api:8080`), and that request was answered 403. The arm now drives the real
//     mux in the proxy's shape — with and without the forwarded host — and keeps
//     the refusal arms that prove the fix widened who may ask, not what is handed
//     out.
//
//   - TestPasskeyProxyForwardsTheBrowserHost pins the web half of the same
//     contract: the proxy must actually send the browser's host, or the API has
//     nothing true to read.
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
			// ---- card t_ce549b48: the deployment's OWN proxy hop ----
			//
			// This is the host the API sees for the product's own path. The
			// dashboard's /api/* proxy dials JOMFORM_API_URL and REPLACES Host
			// with it, so the request that runs a real browser ceremony arrives
			// as `api:8080`. v1.21.52 refused exactly that —
			// `POST https://jomform.com/api/auth/passkeys/login/start` answered
			// 403 `request.forbidden` while the direct /v1/ URL kept working —
			// because `api` is not a subdomain of jomform.com.
			//
			// It is the deployment's own hop, so it is accepted AS the app host:
			// the ceremony is jomform.com's, and jomform.com is the only origin
			// listed. The internal host must never become an origin itself —
			// "https://api:8080" is an origin no browser can ever present.
			name: "the deployments own proxy hop", appBaseURL: "https://jomform.com", requestHost: "api:8080",
			wantRPID: "jomform.com", wantOrigins: []string{"https://jomform.com"},
		},
		{
			// The same container under its compose/swarm name: this repo's
			// Traefik config targets http://jomform-api-1:8080.
			name: "the proxy hop under its compose name", appBaseURL: "https://jomform.com", requestHost: "jomform-api-1:8080",
			wantRPID: "jomform.com", wantOrigins: []string{"https://jomform.com"},
		},
		{
			// The hop is a bare label whatever it is called, and the case does
			// not depend on how the deployment spells it.
			name: "the proxy hop with mixed-case spelling", appBaseURL: "https://jomform.com", requestHost: "API:8080",
			wantRPID: "jomform.com", wantOrigins: []string{"https://jomform.com"},
		},
		{
			// A local dev deployment: the proxy targets http://api:8080 (the
			// compose service name) while APP_BASE_URL names localhost, so the
			// hop's host is not the app's host either — and the app's origin is
			// what is listed.
			name: "the proxy hop in local dev", appBaseURL: "http://localhost:8080", requestHost: "api:8080",
			wantRPID: "localhost", wantOrigins: []string{"http://localhost:8080"},
		},
		{
			// The hop's host is compared WITHOUT its port, so a proxy that dials
			// the app's own name on another port (localhost:8082 is
			// route.ts's own default) is the app-host arm rather than the
			// internal-hop arm, and its port-preserving origin is added as it
			// always was. Measured, not assumed: this is what the port rule in
			// passkeyHostname/passkeyOriginFor already produced.
			name: "the proxy hop on the app host, another port", appBaseURL: "http://localhost:8080", requestHost: "localhost:8082",
			wantRPID: "localhost",
			wantOrigins: []string{
				"http://localhost:8080", "http://localhost:8082",
			},
		},
		{
			// A deployment whose proxy targets an address instead of a name.
			name: "the proxy hop by private address", appBaseURL: "https://jomform.com", requestHost: "172.17.0.5:8080",
			wantRPID: "jomform.com", wantOrigins: []string{"https://jomform.com"},
		},
		{
			// …and the staging deployment, whose hop is the same internal name
			// and whose ceremony must stay bound to ITS domain.
			name: "staging proxy hop", appBaseURL: "https://" + stagingHost, requestHost: "api:8080",
			wantRPID: stagingHost, wantOrigins: []string{"https://" + stagingHost},
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

// TestPasskeyCeremonySurvivesTheProxyHop is the arm #1128's tests did not have —
// and the one the product actually sends.
//
// #1128 drove the handler with the app host as r.Host (`httptest`), so the shape
// the browser produces through the dashboard's own proxy was never exercised, and
// it answered 403: web/src/app/api/[...path]/route.ts builds a FRESH header set
// for its hop (Authorization, Cookie, Content-Type only) and fetches
// JOMFORM_API_URL, so the API receives `Host: api:8080` — the deployment's own
// internal name, not the app's domain. Measured on prod with v1.21.52:
//
//	POST https://jomform.com/api/auth/passkeys/login/start   -> 403 (Host: api:8080)
//	POST https://api.jomform.com/v1/auth/passkeys/login/start -> 200 (Host: api.jomform.com)
//
// i.e. passkey sign-in and registration were dead for every browser, while the
// one URL no browser uses answered correctly.
//
// EVERY ARM BELOW IS A REQUEST SHAPE THAT MUST KEEP WORKING, or the one shape
// that must still be refused:
//
//  1. Host: api:8080 + X-Forwarded-Host: jomform.com — the fixed proxy's shape.
//  2. Host: api:8080, no forwarded host — an api image newer than the web image
//     (a rolling deploy, or an api-only bump). The hop is recognised for what it
//     is, so passkey does not depend on the two images shipping together.
//  3. Host: api:8080 + X-Forwarded-Host: www.jomform.com — the same hop for a
//     browser on a host this deployment serves; the ceremony stays the app's.
//  4. Host: api.jomform.com — the direct API route (Traefik, jf-api-f), which is
//     what kept answering 200 and hid the regression. It must keep working.
//  5. Host: api:8080 + X-Forwarded-Host: attacker.example — the forwarded host is
//     VALIDATED, not trusted: a caller cannot smuggle a foreign origin into the
//     ceremony by header.
//  6. Host: attacker.example — a genuinely foreign host is still refused with no
//     options and no key.
//
// 5 and 6 are the arms that keep the guard real: the fix widens WHO may ask (the
// deployment's own proxy) without widening WHAT can be handed out (never a host
// outside the app's domain).
func TestPasskeyCeremonySurvivesTheProxyHop(t *testing.T) {
	if os.Getenv("JOMFORM_TEST_DATABASE_URL") == "" {
		t.Skip("JOMFORM_TEST_DATABASE_URL not set")
	}
	pool := bundleTestPool(t)
	h := passkeyHandlers(t, pool)
	h.Passkeys = dbstore.NewPasskeyStore(pool)

	// proxyHopHost is what the API sees for a browser on passkeyOriginHost: the
	// host JOMFORM_API_URL resolves to in the deployment (card t_ce549b48).
	const proxyHopHost = "api:8080"

	start := func(t *testing.T, host, forwarded string) *httptest.ResponseRecorder {
		t.Helper()
		mux := http.NewServeMux()
		h.RegisterRoutes(mux)
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/v1/auth/passkeys/login/start", nil)
		if forwarded != "" {
			req.Header.Set("X-Forwarded-Host", forwarded)
		}
		req.Header.Set("X-Real-IP", uniqueTestIP())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// wantsCeremony asserts a 200 whose body really carries a ceremony bound to
	// wantRPID — a 200 without options would pass a status-only check.
	wantsCeremony := func(t *testing.T, host, forwarded, wantRPID string) {
		t.Helper()
		rec := start(t, host, forwarded)
		if rec.Code != http.StatusOK {
			t.Fatalf("login/start (Host %s, X-Forwarded-Host %q) = %d (body %s), want 200 — this is the "+
				"request shape the product sends: a browser goes to /api/auth/passkeys/login/start, and the "+
				"dashboard proxy forwards it to the API with its own Host. A 403 here is passkey sign-in and "+
				"registration dead for every user (v1.21.52 on prod).",
				host, forwarded, rec.Code, rec.Body.String())
		}
		var body struct {
			Key     string `json:"key"`
			Options struct {
				PublicKey struct {
					RPID      string `json:"rpId"`
					Challenge string `json:"challenge"`
				} `json:"publicKey"`
			} `json:"options"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode login/start body (Host %s, X-Forwarded-Host %q): %v (%s)", host, forwarded, err, rec.Body.String())
		}
		if body.Key == "" || body.Options.PublicKey.Challenge == "" {
			t.Fatalf("login/start (Host %s, X-Forwarded-Host %q) answered 200 without a usable ceremony: %s",
				host, forwarded, rec.Body.String())
		}
		if body.Options.PublicKey.RPID != wantRPID {
			t.Errorf("login/start (Host %s, X-Forwarded-Host %q) returned rpId %q, want %q — the ceremony "+
				"reaching the browser must be the DEPLOYMENT's, whichever host the internal hop used",
				host, forwarded, body.Options.PublicKey.RPID, wantRPID)
		}
		// The internal host must never reach the wire as an RP ID: "api" is not a
		// domain any page can be on.
		if body.Options.PublicKey.RPID == "api" {
			t.Errorf("the ceremony is bound to the proxy hop's own name (%q) — no browser origin can be "+
				"on it, so every ceremony would fail", body.Options.PublicKey.RPID)
		}
	}

	// wantsRefusal asserts 403 AND that nothing ceremony-shaped came back.
	wantsRefusal := func(t *testing.T, host, forwarded string) {
		t.Helper()
		rec := start(t, host, forwarded)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("login/start (Host %s, X-Forwarded-Host %q) = %d (body %s), want 403 — a passkey "+
				"ceremony must not be offered at an origin the deployment does not own",
				host, forwarded, rec.Code, rec.Body.String())
		}
		var refusal struct {
			Code      string          `json:"code"`
			Options   json.RawMessage `json:"options"`
			Key       string          `json:"key"`
			Challenge json.RawMessage `json:"challenge"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &refusal); err != nil {
			t.Fatalf("decode refusal body: %v (%s)", err, rec.Body.String())
		}
		if refusal.Options != nil || refusal.Key != "" || refusal.Challenge != nil {
			t.Errorf("the refusal for (Host %s, X-Forwarded-Host %q) still carries ceremony material "+
				"(options=%s key=%q challenge=%s)", host, forwarded, refusal.Options, refusal.Key, refusal.Challenge)
		}
	}

	t.Run("proxy hop forwarding the browser host", func(t *testing.T) {
		wantsCeremony(t, proxyHopHost, passkeyOriginHost, passkeyOriginHost)
	})
	t.Run("proxy hop with no forwarded host (api-only deploy)", func(t *testing.T) {
		wantsCeremony(t, proxyHopHost, "", passkeyOriginHost)
	})
	t.Run("proxy hop for a browser on www", func(t *testing.T) {
		wantsCeremony(t, proxyHopHost, "www."+passkeyOriginHost, passkeyOriginHost)
	})
	t.Run("the direct API route", func(t *testing.T) {
		wantsCeremony(t, "api."+passkeyOriginHost, "", passkeyOriginHost)
	})
	t.Run("forwarded host cannot smuggle a foreign origin", func(t *testing.T) {
		wantsRefusal(t, proxyHopHost, "attacker.example")
	})
	t.Run("a foreign host is still refused", func(t *testing.T) {
		wantsRefusal(t, "attacker.example", "")
	})
}

// The test below is the OTHER half of this card: the proxy that dropped the
// browser's host must carry it, or the ceremony loses the origin it really runs
// on (a browser on www.jomform.com would be handed an origin allow-list without
// https://www.jomform.com). node --test cannot load route.ts (Next imports), so
// the file is read as TEXT — the cross-language seam this package already works
// under, and the shape web/src/lib/passkeyProxyHost.test.ts mirrors in web/.
//
// A source-level pin is the only thing that can catch this half: the Go suite
// cannot import the proxy, and no unit test of the API can see a header the proxy
// never sends. The bug that made this card necessary lived in exactly this gap.
func TestPasskeyProxyForwardsTheBrowserHost(t *testing.T) {
	route := webSource(t, "src/app/api/[...path]/route.ts")

	if !strings.Contains(route, `headers.set("X-Forwarded-Host", browserHost)`) {
		t.Errorf("web/src/app/api/[...path]/route.ts no longer sends the browser's host to the API as " +
			"X-Forwarded-Host — the proxy replaces Host with JOMFORM_API_URL, so without this header the API " +
			"cannot know the browser's origin (this is the shape that made v1.21.52 answer 403 to the " +
			"product's own /api/auth/passkeys/login/start)")
	}

	// The value must come from the connection's own Host first: `host` is set by
	// the client and rewritten by nobody until the proxy, while a forwarded value
	// is caller-supplied and only worth falling back to.
	hostIdx := strings.Index(route, `req.headers.get("host")`)
	fwdIdx := strings.Index(route, `req.headers.get("x-forwarded-host")`)
	if hostIdx < 0 {
		t.Errorf("web/src/app/api/[...path]/route.ts no longer reads req.headers.get(\"host\") — the browser's " +
			"host is the value the API must be told")
	} else if fwdIdx >= 0 && fwdIdx < hostIdx {
		t.Errorf("web/src/app/api/[...path]/route.ts prefers a caller-supplied X-Forwarded-Host over the " +
			"connection's own Host header")
	}

	// And the path that carries passkeys through it must be the one that proxies at
	// all: if the catch-all route is deleted the assertion above becomes vacuous.
	if !strings.Contains(route, "/v1") {
		t.Errorf("web/src/app/api/[...path]/route.ts no longer targets the API's /v1 namespace — this guard " +
			"is reading a file that no longer proxies anything")
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
