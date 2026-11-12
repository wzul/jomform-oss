package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Renewing the grant.
//
// The server issues an access token for an hour (OAUTH_ACCESS_TOKEN_TTL_SECONDS,
// default 3600) and a refresh token for 30 days. A harness entry that carries
// the access token in a static `headers` block therefore goes dark an hour after
// login, because a harness has no way to renew it. The entry this CLI writes is a
// launcher instead (`jomform mcp proxy`), and the proxy renews the pair here,
// writing the rotated refresh token back before it is used again.
//
// Rotation makes this a shared-state problem: the server revokes the old refresh
// token the moment it issues the new one (single-use, dbstore.RefreshGrant). Two
// processes that both renew from the same stored token would race, and the loser
// would be left holding a dead refresh token. Hence the lock file plus a re-read
// inside it: whoever wins writes the pair, and the loser picks that pair up
// rather than spending the token it read a moment earlier.

const (
	// refreshSkew renews an access token this long before the server stops
	// accepting it, so a request that starts just under the wire still lands.
	refreshSkew = 60 * time.Second
	// lockWait bounds how long one process waits for another that is already
	// renewing the same token file.
	lockWait = 10 * time.Second
	// lockStale is when a lock file is presumed abandoned by a process that died
	// mid-renewal, and may be removed.
	lockStale = 30 * time.Second
	// lockPoll is the retry interval while waiting for the lock.
	lockPoll = 25 * time.Millisecond
)

// errGrantDead marks a renewal the server refused for good: the refresh token is
// expired, already rotated, or revoked. Only a fresh device grant fixes that, so
// the message has to say so.
var errGrantDead = errors.New("the stored grant can no longer be renewed")

// tokenDue reports whether an access token is inside its renewal window.
//
// The window is a minute for an ordinary one-hour token. A deployment (or a test)
// that issues very short tokens would otherwise renew on every single request, so
// for a lifetime under two minutes the window is half the lifetime instead.
func tokenDue(tok storedToken, now time.Time) bool {
	if tok.AccessToken == "" {
		return true
	}
	window := refreshSkew
	if lifetime := tok.ExpiresAt.Sub(tok.CreatedAt); lifetime > 0 && lifetime < 2*refreshSkew {
		window = lifetime / 2
	}
	return !now.Add(window).Before(tok.ExpiresAt)
}

// accessToken returns a token the server will accept: the stored one when it is
// still good, a renewed one otherwise.
//
// `rejected` is the token the server has just refused (or "" when none has). A
// token that differs from it was renewed by another process in the meantime and
// is preferred over spending the refresh token again.
func (c *cli) accessToken(ctx context.Context, rejected string) (storedToken, error) {
	tok, err := c.loadToken()
	if err != nil {
		return tok, err
	}
	if tok.AccessToken != rejected && !tokenDue(tok, c.now()) {
		return tok, nil
	}

	unlock, err := c.lockToken()
	if err != nil {
		return tok, err
	}
	defer unlock()

	// Re-read inside the lock: a sibling process may have rotated the pair while
	// this one waited, and its copy is the only one the server still honours.
	if fresh, ferr := c.loadToken(); ferr == nil {
		tok = fresh
		if tok.AccessToken != rejected && !tokenDue(tok, c.now()) {
			return tok, nil
		}
	}
	return c.refreshGrant(ctx, tok)
}

// refreshGrant trades the stored refresh token for a fresh pair and persists it.
// The old refresh token dies at the server when this succeeds, so the new one is
// written to disk before it is returned to the caller.
func (c *cli) refreshGrant(ctx context.Context, tok storedToken) (storedToken, error) {
	if tok.RefreshToken == "" {
		return tok, fmt.Errorf("%w: no refresh token is stored — run `jomform mcp login --headless` again", errGrantDead)
	}
	if tok.Server == "" {
		return tok, fmt.Errorf("%w: the stored token does not name the server that issued it — run `jomform mcp login --headless` again", errGrantDead)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", tok.RefreshToken)
	if tok.ClientID != "" {
		form.Set("client_id", tok.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(tok.Server, "/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return tok, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return tok, fmt.Errorf("could not reach %s to renew the token: %w", tok.Server, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return tok, fmt.Errorf("could not read the token renewal response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e oauthErrorBody
		_ = json.Unmarshal(raw, &e)
		switch e.Error {
		case "invalid_grant", "invalid_client", "invalid_request", "unsupported_grant_type":
			return tok, fmt.Errorf("%w: the server answered %s — run `jomform mcp login --headless` again", errGrantDead, errCodeWords(e))
		}
		// 5xx, 409 and 429 are the network or a load balancer, not a verdict:
		// the next request tries again rather than sending the user to re-login.
		return tok, fmt.Errorf("the server could not renew the token right now (HTTP %d)", resp.StatusCode)
	}
	var g tokenGrant
	if err := json.Unmarshal(raw, &g); err != nil || g.AccessToken == "" {
		return tok, fmt.Errorf("the server sent a renewal this client could not read; the stored token was left alone")
	}
	// RFC 6749 §6: a server MAY keep the same refresh token, so an omitted one is
	// not a reason to lose the grant.
	if g.RefreshToken == "" {
		g.RefreshToken = tok.RefreshToken
	}
	if g.Scope == "" {
		g.Scope = tok.Scope
	}
	if g.TokenType == "" {
		g.TokenType = "Bearer"
	}

	now := c.now()
	renewed := tok
	renewed.AccessToken = g.AccessToken
	renewed.RefreshToken = g.RefreshToken
	renewed.Scope = g.Scope
	renewed.TokenType = g.TokenType
	renewed.CreatedAt = now
	// An absent expires_in means the lifetime is unknown; treat the token as due
	// again on the next request rather than claim a lifetime the server did not.
	if g.ExpiresIn > 0 {
		renewed.ExpiresAt = now.Add(time.Duration(g.ExpiresIn) * time.Second)
	} else {
		renewed.ExpiresAt = now
	}
	if err := c.saveToken(renewed); err != nil {
		// The token works for this request, but the rotated refresh token only
		// exists in memory. Say so loudly: the next run has to log in again.
		c.warnf("the renewed token could not be stored: %v", err)
	}
	return renewed, nil
}

// errCodeWords renders an RFC 6749 error for a person: the code, plus the
// server's own words when it sent any (bounded, so a broken server cannot flood
// a log line).
func errCodeWords(e oauthErrorBody) string {
	if d := strings.TrimSpace(e.ErrorDescription); d != "" {
		if len(d) > 160 {
			d = d[:160] + "…"
		}
		return e.Error + " (" + d + ")"
	}
	return e.Error
}

// lockToken takes the renewal lock for this config directory and returns the
// function that releases it.
func (c *cli) lockToken() (func(), error) {
	path := filepath.Join(c.configDir(), "token.lock")
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("could not take %s: %w", path, err)
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStale {
			// A process that died mid-renewal must not wedge every later call.
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another jomform process kept %s locked for %s", path, lockWait)
		}
		time.Sleep(lockPoll)
	}
}
