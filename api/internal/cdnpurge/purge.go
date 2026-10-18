// Package cdnpurge invalidates the CDN edge's copies of objects the API has
// deleted.
//
// THE DEFECT THIS EXISTS FOR. `/media/{key}` answers with a public, day-long
// freshness lifetime, so Cloudflare holds the bytes and answers from its own
// edge without touching the origin. Purging a media row removed the row and the
// object, and the origin then answered 404 — but the EDGE kept handing out the
// bytes to anyone holding the URL, measured live on 2026-10-18 as
// `cf-cache-status: HIT, age: 35` on a URL whose origin copy was already gone.
// Measuring only the origin is exactly how that gap stayed invisible: the
// origin is honest immediately, so the check passes while the object is still
// downloadable.
//
// TWO PARTS TO THE ANSWER, and both are here or beside this package:
//
//  1. the media routes now answer with a SHORT freshness lifetime
//     (httpapi.mediaCacheControl), so a wrong answer can only survive for that
//     window even when nothing is purged — the self-healing floor; and
//  2. a purge asks the CDN to drop the object's entries in the SAME request, so
//     the window is zero when this client is configured.
//
// PURGE BY PREFIX, NOT BY EXACT URL. A CDN keys on the full URL including the
// query string, so `…/x.jpg?t=1` and `…/x.jpg` are DIFFERENT entries: an
// exact-URL purge of one leaves the other cached and still downloadable, and the
// queries a page, a mail client, an embed or a tracking link appends are not
// enumerable. A prefix is the object's own URL path — Cloudflare matches every
// URL that starts with it, so the bare URL and every query variant of it go
// together. Object keys end in the file extension (`u/{acct}/{uuid}.jpg`, with
// variants named `{uuid}-thumbnail.jpg`), so a prefix can never reach a sibling
// object.
// See https://developers.cloudflare.com/cache/how-to/cache-keys/ (the default
// cache key is scheme + host + URI **with query string**, so the variants are
// separate entries), https://developers.cloudflare.com/api/resources/cache/methods/purge/
// (`prefixes`) and .../cache/how-to/purge-cache/purge_by_prefix/.
//
// CONFIGURATION. The client is built from the environment on each purge:
//
//	JF_CF_PURGE_TOKEN    API token with the Cache Purge permission
//	JF_CF_PURGE_ZONE_ID  the zone the public media URLs belong to
//	JF_CF_PURGE_HOSTS    optional comma-separated hosts that live in that zone;
//	                     defaults to the host of APP_BASE_URL
//	JF_CF_PURGE_API_BASE optional override of the API root (tests)
//
// Unconfigured is a supported state, not an error: the response names
// `edge: not_configured` and the short freshness lifetime is the only bound on
// staleness. A rejected purge is likewise reported, never swallowed — the bytes
// are already gone by then, so failing the request would not bring them back,
// but a silent failure would leave an operator believing the edge was cleared.
package cdnpurge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment variable names, one place, so the docs and the compose can be
// compared against the code rather than remembered.
const (
	EnvToken   = "JF_CF_PURGE_TOKEN"
	EnvZoneID  = "JF_CF_PURGE_ZONE_ID"
	EnvHosts   = "JF_CF_PURGE_HOSTS"
	EnvAPIBase = "JF_CF_PURGE_API_BASE"
)

// DefaultAPIBase is Cloudflare's API root. The purge endpoint is
// POST {base}/zones/{zone_id}/purge_cache.
const DefaultAPIBase = "https://api.cloudflare.com/client/v4"

// maxPrefixesPerRequest is Cloudflare's documented ceiling of 100 operations
// per purge request ("Max operations per request"), shared by every purge
// method. A media purge sends at most base + 3 sized variants + 1 PDF preview =
// 5, so the cap is a guard against a future caller looping over a library, not
// a limit this card can reach.
const maxPrefixesPerRequest = 100

// MediaMaxAgeSeconds is the freshness lifetime a /media or /assets response
// declares (httpapi's mediaCacheControl). It is stated here, beside the purge,
// because the two numbers are one decision: a purge makes the edge window zero,
// and this is the window that remains when no purge is configured or when the
// CDN refuses one. A test pins the served header to this value so the two
// cannot drift.
const MediaMaxAgeSeconds = 300

// purgeTimeout bounds the outbound call. The bytes are already deleted by the
// time a purge is attempted, so a slow CDN API must not hold the merchant's
// request open: a timeout is reported as a failed purge, which the response
// names, and the short freshness lifetime bounds the damage.
const purgeTimeout = 10 * time.Second

// Result statuses. They are the values the caller publishes as `edge`, so they
// are stated once here rather than spelled at two call sites (REST and MCP).
const (
	// StatusPurged — the CDN accepted the purge request for every URL given.
	// Cloudflare's own 200 means ACCEPTED, not "confirmed removed": the proof is
	// a following request answering `CF-Cache-Status: MISS` (or the origin's
	// 404), which is what the probe in scripts/media-purge-probe.sh measures.
	StatusPurged = "purged"
	// StatusNotConfigured — no token/zone in the environment. The purge did not
	// happen; the short freshness lifetime is the only bound on staleness.
	StatusNotConfigured = "not_configured"
	// StatusOutOfZone — URLs were given but none belong to the configured zone,
	// so nothing was asked of the CDN. Purging a URL from another zone is the
	// operator's configuration mistake and Cloudflare would reject it.
	StatusOutOfZone = "out_of_zone"
	// StatusNoURLs — there was nothing to purge: no object store, or a key that
	// produced no URL. Distinct from out_of_zone, which means an operator's zone
	// configuration does not cover the URLs we do have.
	StatusNoURLs = "no_urls"
	// StatusFailed — the CDN refused the request or could not be reached.
	StatusFailed = "failed"
)

// Client is one zone's cache-purge capability.
type Client struct {
	Token    string
	ZoneID   string
	Hosts    []string // hosts that live in ZoneID (lower case, no port)
	APIBase  string
	HTTP     *http.Client
	Endpoint string // set by tests to observe the call; defaults to the CDN API
}

// FromEnv builds a client from the environment, or nil when it is not
// configured. A half-configured client (token without zone, or the reverse) is
// NOT usable: purging needs both, and reporting "configured" for a pair that
// cannot work would name a purge that never happened.
func FromEnv() *Client {
	token := strings.TrimSpace(os.Getenv(EnvToken))
	zone := strings.TrimSpace(os.Getenv(EnvZoneID))
	if token == "" || zone == "" {
		return nil
	}
	hosts := splitHosts(os.Getenv(EnvHosts))
	if len(hosts) == 0 {
		if h := normalizeHost(appBaseURL()); h != "" {
			hosts = []string{h}
		}
	}
	base := strings.TrimSpace(os.Getenv(EnvAPIBase))
	if base == "" {
		base = DefaultAPIBase
	}
	return &Client{
		Token:   token,
		ZoneID:  zone,
		Hosts:   hosts,
		APIBase: strings.TrimSuffix(base, "/"),
		HTTP:    &http.Client{Timeout: purgeTimeout},
	}
}

// appBaseURL mirrors httpapi's public base default so this package needs no
// dependency on it: the CDN zone a deployment purges is the zone its public
// media URLs live in, and those are built from APP_BASE_URL / S3_PUBLIC_BASE.
func appBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("S3_PUBLIC_BASE")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("APP_BASE_URL")); v != "" {
		return v
	}
	return ""
}

func splitHosts(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if h := normalizeHost(part); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// normalizeHost lower-cases a host or URL and drops the port, so
// `JomForm.com` and `https://jomform.com:443` name the same zone entry.
func normalizeHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			raw = u.Host
		}
	}
	if h, _, ok := strings.Cut(raw, "/"); ok {
		raw = h
	}
	if i := strings.LastIndex(raw, ":"); i > 0 && !strings.Contains(raw[i:], "]") {
		raw = raw[:i]
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func (c *Client) inZone(host string) bool {
	for _, h := range c.Hosts {
		if h == host {
			return true
		}
	}
	return false
}

// PurgeObjectURLs asks the CDN to drop every cached entry for the given object
// URLs (as prefixes) and returns the URLs it asked about.
//
// A URL whose host is not in the configured zone is dropped rather than sent:
// Cloudflare rejects the whole request for an out-of-zone URL, so sending one
// bad URL would silently cancel the purge of the good ones too.
func (c *Client) PurgeObjectURLs(ctx context.Context, urls []string) ([]string, error) {
	prefixes := c.zonePrefixes(urls)
	if len(prefixes) == 0 {
		return nil, nil
	}
	if len(prefixes) > maxPrefixesPerRequest {
		prefixes = prefixes[:maxPrefixesPerRequest]
	}
	body, err := json.Marshal(map[string]any{"prefixes": prefixes})
	if err != nil {
		return nil, fmt.Errorf("encode purge request: %w", err)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = c.APIBase + "/zones/" + url.PathEscape(c.ZoneID) + "/purge_cache"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build purge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: purgeTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("purge request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("purge rejected: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// A 200 carrying success:false is a refusal too — Cloudflare answers 200
	// with an errors[] list for several rejections (e.g. 1134 rate limited), so
	// trusting the status code alone would report a purge that did not happen.
	var out struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("purge response not JSON: %w", err)
	}
	if !out.Success {
		var msgs []string
		for _, e := range out.Errors {
			msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		if len(msgs) == 0 {
			msgs = []string{"success=false with no error detail"}
		}
		return nil, fmt.Errorf("purge rejected: %s", strings.Join(msgs, "; "))
	}
	return prefixes, nil
}

// zonePrefixes turns object URLs into deduplicated, in-zone prefixes.
func (c *Client) zonePrefixes(urls []string) []string {
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		if !c.inZone(normalizeHost(u.Host)) {
			continue
		}
		// Drop the query and fragment: a prefix must be the object's own path so
		// it matches the bare URL AND every query variant of it (whatever a page,
		// an embed or a tracking link appends).
		prefix := u.Scheme + "://" + u.Host + u.Path
		if _, dup := seen[prefix]; dup {
			continue
		}
		seen[prefix] = struct{}{}
		out = append(out, prefix)
	}
	return out
}

// Result is what a caller publishes: the status, the URLs asked about, and the
// error when there was one.
type Result struct {
	Status string
	URLs   []string
	Err    error
}

// PurgeObjectURLs is the whole decision a caller needs: build the client from
// the environment, purge, and describe the outcome in the values both the REST
// handler and the MCP tool publish.
//
// It never returns a fatal error to its caller: the object and the row are
// already gone by the time this runs, so a CDN failure cannot be undone by
// failing the request. It must be REPORTED, which is what StatusFailed and
// Err are for.
func PurgeObjectURLs(ctx context.Context, urls []string) Result {
	c := FromEnv()
	if c == nil {
		return Result{Status: StatusNotConfigured}
	}
	if len(urls) == 0 {
		return Result{Status: StatusNoURLs}
	}
	purged, err := c.PurgeObjectURLs(ctx, urls)
	if err != nil {
		return Result{Status: StatusFailed, URLs: purged, Err: err}
	}
	if len(purged) == 0 {
		return Result{Status: StatusOutOfZone}
	}
	return Result{Status: StatusPurged, URLs: purged}
}

// Fields renders the result as response fields, so the REST handler and the MCP
// tool cannot drift in what they publish.
func (r Result) Fields() map[string]any {
	f := map[string]any{"edge": r.Status}
	if len(r.URLs) > 0 {
		f["edge_urls"] = r.URLs
	}
	if r.Err != nil {
		f["edge_error"] = r.Err.Error()
	}
	return f
}
