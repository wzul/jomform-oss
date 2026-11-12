package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// deviceGrant is the RFC 8628 §3.2 response. The field names are the RFC's.
type deviceGrant struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenGrant is the RFC 6749 §5.1 response the device grant ends in.
type tokenGrant struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// oauthErrorBody is the error shape /oauth/token answers with. RFC 8628 §3.5
// names the codes; error_description is the server's own words.
type oauthErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// pollResult is one poll's outcome: either a token, or the RFC error code plus
// the HTTP status (the status decides whether an unknown failure is worth
// retrying).
type pollResult struct {
	token   *tokenGrant
	errCode string
	errDesc string
	status  int
}

const (
	// slowDownStep is added to the poll interval on `slow_down` and kept for the
	// rest of the authorisation (RFC 8628 §3.5).
	slowDownStep = 5 * time.Second
	// maxTransientPolls bounds retries of a 5xx/409/429 answer, so a broken
	// server cannot hold the terminal until the code expires.
	maxTransientPolls = 4
	// deviceGrantType is the urn the token endpoint dispatches on.
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
)

// normalizeServer validates and canonicalises --server.
//
// A token travels over this connection, so https is required. The one exception
// is a loopback address, which is how a self-hosted or test deployment is
// reached and which cannot be observed off the machine.
func normalizeServer(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("%w: --server is required", errUsage)
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%w: --server %q is not a URL", errUsage, raw)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return "", fmt.Errorf("%w: --server must be https (http is accepted only on a loopback address)", errUsage)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: --server must not carry a query or fragment", errUsage)
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+strings.TrimRight(u.Path, "/"), "/"), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolveMCPURL picks the endpoint written into the harness config: the flag
// when given, the hosted endpoint for the hosted server, and otherwise the same
// deployment's /mcp.
func resolveMCPURL(raw, server string) (string, error) {
	if strings.TrimSpace(raw) != "" {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%w: --mcp-url %q is not a URL", errUsage, raw)
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
			return "", fmt.Errorf("%w: --mcp-url must be https (http is accepted only on a loopback address)", errUsage)
		}
		return strings.TrimRight(raw, "/"), nil
	}
	su, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	if su.Hostname() == "jomform.com" || su.Hostname() == "www.jomform.com" {
		return defaultMCPURL, nil
	}
	return server + "/mcp", nil
}

// endpoint joins a path onto the server base without doubling slashes.
func endpoint(server, path string) string {
	return strings.TrimRight(server, "/") + path
}

// ensureClient returns the client_id this machine registered earlier, or
// registers one now with the same dynamic client registration endpoint the
// browser flow uses. Nothing about the registration is secret: the server
// issues a public client (token_endpoint_auth_method=none).
func (c *cli) ensureClient(ctx context.Context, server, clientName string) (string, error) {
	if saved, err := c.loadClient(); err == nil && saved.Server == server && saved.ClientID != "" {
		return saved.ClientID, nil
	}
	body, err := json.Marshal(map[string]any{
		"client_name":   clientName,
		"redirect_uris": []string{"http://127.0.0.1:8765/callback"},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(server, "/oauth/register"), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", server, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("could not read the registration response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("client registration failed (HTTP %d); %s may not accept dynamic registration",
			resp.StatusCode, server)
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ClientID == "" {
		return "", fmt.Errorf("client registration returned no client_id")
	}
	// A failed save is not fatal: the id is still usable for this run.
	_ = c.saveClient(storedClient{Server: server, ClientID: out.ClientID, ClientName: clientName})
	return out.ClientID, nil
}

// startDevice asks the server for a code pair.
func (c *cli) startDevice(ctx context.Context, server, clientID string, o loginOptions) (*deviceGrant, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", o.scope)
	if strings.TrimSpace(o.email) != "" {
		form.Set("login_hint", strings.TrimSpace(o.email))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(server, "/oauth/device_authorization"), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", server, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("could not read the device authorization response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, describeDeviceStartFailure(resp.StatusCode, raw)
	}
	var g deviceGrant
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("the server sent a device authorization this client could not read")
	}
	if g.DeviceCode == "" || g.UserCode == "" || g.VerificationURIComplete == "" {
		return nil, fmt.Errorf("the server sent an incomplete device authorization")
	}
	return &g, nil
}

// describeDeviceStartFailure turns a refused start into something a person can
// act on. The body is decoded for its error code only; it is never echoed, so a
// misbehaving server cannot push arbitrary text (or a token) onto the terminal.
func describeDeviceStartFailure(status int, raw []byte) error {
	var e oauthErrorBody
	_ = json.Unmarshal(raw, &e)
	switch e.Error {
	case "invalid_client":
		return fmt.Errorf("the server does not know this client (HTTP %d); the registration may have been deleted", status)
	case "invalid_scope":
		return fmt.Errorf("the server refused the requested scope: %s", e.ErrorDescription)
	case "temporarily_unavailable":
		return fmt.Errorf("the server is not taking device authorisations right now (HTTP %d); try again later", status)
	}
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("too many device authorisations from this address; try again later")
	}
	return fmt.Errorf("the server refused the device authorization (HTTP %d)", status)
}

// pollToken polls /oauth/token until the person approves, denies or the code
// expires.
//
// RFC 8628 §3.4: wait `interval` before every poll. §3.5: authorization_pending
// means keep waiting, slow_down means the interval grows by five seconds and the
// larger value stays for the rest of the grant, access_denied and expired_token
// are terminal. A 5xx/409/429 is the network or a load balancer, not a verdict,
// so it is retried a bounded number of times instead of being reported as a
// rejection.
func (c *cli) pollToken(ctx context.Context, server, clientID, deviceCode string, interval time.Duration, deadline time.Time) (*tokenGrant, error) {
	transient := 0
	for {
		if !c.now().Before(deadline) {
			return nil, fmt.Errorf("the code expired before it was approved — run `jomform mcp login --headless` again")
		}
		c.sleep(interval)
		if !c.now().Before(deadline) {
			return nil, fmt.Errorf("the code expired before it was approved — run `jomform mcp login --headless` again")
		}

		res, err := c.tokenPoll(ctx, server, clientID, deviceCode)
		if err != nil {
			return nil, err
		}
		if res.token != nil {
			return res.token, nil
		}
		switch res.errCode {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += slowDownStep
			continue
		case "access_denied":
			return nil, fmt.Errorf("the request was denied on jomform — nothing was stored")
		case "expired_token":
			return nil, fmt.Errorf("the code expired before it was approved — run `jomform mcp login --headless` again")
		case "invalid_grant":
			return nil, fmt.Errorf("the server rejected the device code — run `jomform mcp login --headless` again")
		}
		switch {
		case res.status == http.StatusTooManyRequests:
			// The token endpoint's throttle is transient; back off like slow_down.
			interval += slowDownStep
		case res.status >= 500 || res.status == http.StatusConflict:
			// Retried below.
		default:
			if res.errCode != "" {
				return nil, fmt.Errorf("the server refused the token request: %s", res.errDescOrCode())
			}
			return nil, fmt.Errorf("the server answered the token request with HTTP %d", res.status)
		}
		transient++
		if transient > maxTransientPolls {
			return nil, fmt.Errorf("the server kept failing the token request (last HTTP %d); try again later", res.status)
		}
	}
}

// errDescOrCode names a refusal without trusting the body's length or content.
func (r *pollResult) errDescOrCode() string {
	if strings.TrimSpace(r.errDesc) != "" {
		return r.errCode + ": " + strings.TrimSpace(r.errDesc)
	}
	return r.errCode
}

// tokenPoll performs one POST /oauth/token.
func (c *cli) tokenPoll(ctx context.Context, server, clientID, deviceCode string) (*pollResult, error) {
	form := url.Values{}
	form.Set("grant_type", deviceGrantType)
	form.Set("device_code", deviceCode)
	form.Set("client_id", clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(server, "/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lost contact with the server while waiting for approval: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("could not read the token response: %w", err)
	}
	// A 5xx is decided by its status: a proxy's HTML error page is not a JSON
	// document and must not be reported as a protocol violation.
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusTooManyRequests {
		return &pollResult{status: resp.StatusCode}, nil
	}
	if resp.StatusCode == http.StatusOK {
		var tok tokenGrant
		if err := json.Unmarshal(raw, &tok); err != nil || tok.AccessToken == "" {
			return nil, fmt.Errorf("the server sent a token response this client could not read")
		}
		if tok.TokenType == "" {
			tok.TokenType = "Bearer"
		}
		return &pollResult{token: &tok, status: resp.StatusCode}, nil
	}
	var e oauthErrorBody
	if err := json.Unmarshal(raw, &e); err != nil || e.Error == "" {
		return nil, fmt.Errorf("the server sent an unreadable answer to the token request (HTTP %d)", resp.StatusCode)
	}
	return &pollResult{errCode: e.Error, errDesc: e.ErrorDescription, status: resp.StatusCode}, nil
}

// revoke posts the stored refresh token to /oauth/revoke (RFC 7009). The server
// answers 200 even for an unknown token, so any 200 means the credential is dead.
func (c *cli) revoke(ctx context.Context, tok storedToken) error {
	form := url.Values{}
	refresh := tok.RefreshToken != ""
	if refresh {
		form.Set("token", tok.RefreshToken)
		form.Set("token_type_hint", "refresh_token")
	} else {
		form.Set("token", tok.AccessToken)
		form.Set("token_type_hint", "access_token")
	}
	if tok.ClientID != "" {
		form.Set("client_id", tok.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(tok.Server, "/oauth/revoke"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", tok.Server, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
	}
	return nil
}
