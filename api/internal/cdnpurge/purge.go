// Package cdnpurge invalidates the CDN edge's copies of objects the API has
// deleted — media objects, and the generated social card a form stops serving.
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
// THE SECOND CALLER: /og/{slug}.png. A form's generated social card is served
// with a public freshness lifetime too, so a crawler that fetched it while the
// form was live leaves a copy at the edge. The card names the form's TITLE, its
// workspace name and its host, and the form's own page stops serving it the
// moment the form leaves a live status (draft -> 404, private -> the password
// gate). The origin is honest immediately, the edge is not: measured on prod
// 2026-10-26, 9m after the release that added the status gate, a DRAFT form's
// card still answered `cf-cache-status: HIT, age: 1243` with the pre-fix bytes
// while the same URL with a cache-buster answered 404 — the entry survives until
// it expires or is evicted, which for the public arm was a day. So every write
// path that takes a form OUT of a publicly-served status asks for its card's
// prefix in the same request (httpapi setFormStatus/deleteForm/bulkForms/
// purgeForm, mcpsrv publish_form/unpublish_form/delete_form/bulk_forms), and the
// public arm's lifetime is bounded (OGImageMaxAgeSeconds) so an unconfigured
// purge cannot leave the mistake visible for a day.
//
// CONFIGURATION. The client is built from the environment on each purge:
//
//	JF_CF_PURGE_TOKEN    API token with the Cache Purge permission
//	JF_CF_PURGE_ZONE_ID  the zone the public media URLs belong to
//	JF_CF_PURGE_HOSTS    optional comma-separated hosts that live in that zone;
//	                     defaults to the host of APP_BASE_URL. An entry may be a
//	                     wildcard ("*.jomform.com") to cover the per-workspace
//	                     subdomains a form's card is published on — listing each
//	                     tenant host is not possible, and an exact-match rule
//	                     silently dropped every card purge as out of zone
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
// 5, so it can never reach the cap; a bulk form action can (a card is purged on
// two hosts, up to 500 forms), which is why the package-level PurgeObjectURLs
// splits anything larger into several requests instead of truncating.
const maxPrefixesPerRequest = 100

// MediaMaxAgeSeconds is the freshness lifetime a /media or /assets response
// declares (httpapi's mediaCacheControl). It is stated here, beside the purge,
// because the two numbers are one decision: a purge makes the edge window zero,
// and this is the window that remains when no purge is configured or when the
// CDN refuses one. A test pins the served header to this value so the two
// cannot drift.
const MediaMaxAgeSeconds = 300

// OGImageMaxAgeSeconds is the freshness lifetime the PUBLIC arm of
// /og/{slug}.png declares (httpapi's ogImageCacheControl), stated here for the
// same reason as MediaMaxAgeSeconds: it is what bounds how long the edge can
// keep describing a form after it stops being live when no purge is configured.
//
// It used to be 86400. That is the number that made a pre-fix card outlive the
// release that fixed it (measured on prod: the draft form's card answered from
// the edge with `age: 1243` nine minutes after the new origin code started, and
// would have been served for up to a day). Five minutes is the same bound the
// media routes chose: long enough that a crawler or a share costs one origin
// render per window — the render is cached in-process by input set, so a
// re-render is a map lookup — and short enough that a mistaken or stale card
// cannot sit in a shared cache for a day. The private arm declares
// `private, no-store` and is not affected.
const OGImageMaxAgeSeconds = 300

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

// inZone reports whether a URL host belongs to the configured zone, so a purge
// never sends Cloudflare a URL from another zone (it rejects the whole request,
// which would silently cancel the purge of the valid URLs too).
//
// An entry may be a wildcard — "*.jomform.com" — which matches every subdomain
// under it (any depth) and NOT the base host itself. That is required by the
// card purge: a form's `/og/{slug}.png` is served on a per-workspace host
// (`<slug>.jomform.com`), one host per workspace, so an exact-match list could
// never name them; the alternative — a wildcard-less rule that treats a listed
// host as covering its subdomains — would silently start sending hosts the
// operator never listed, and a subdomain delegated to its own zone would then
// fail the whole request. A wildcard is opt-in, so nothing changes for a
// deployment that lists exact hosts (the media purge's shape).
func (c *Client) inZone(host string) bool {
	for _, h := range c.Hosts {
		if h == host {
			return true
		}
		if base, ok := strings.CutPrefix(h, "*."); ok && base != "" &&
			strings.HasSuffix(host, "."+base) {
			return true
		}
	}
	return false
}

// OGImagePath is the path of a form's generated social card on its public host.
// The handler serves it (httpapi handleOGImage); this is the one place the
// purge learns it, so the URL a card is cached under and the URL a purge asks
// for cannot drift.
func OGImagePath(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "/og/" + url.PathEscape(slug) + ".png"
}

// OGImageURL is the absolute public URL of a form's generated social card on
// one of the hosts it is served from. Empty inputs yield "" so a caller can
// skip an unresolvable host instead of purging a relative path.
//
// The scheme is always https: a card is only ever reachable through the public
// (TLS) host, and the CDN's cache key includes the scheme, so a prefix built
// with any other scheme would match nothing.
func OGImageURL(host, slug string) string {
	host = normalizeHost(host)
	path := OGImagePath(slug)
	if host == "" || path == "" {
		return ""
	}
	return "https://" + host + path
}

// PurgeObjectURLs asks the CDN to drop every cached entry for the given object
// URLs (as prefixes) and returns the URLs it asked about.
//
// A URL whose host is not in the configured zone is dropped rather than sent:
// Cloudflare rejects the whole request for an out-of-zone URL, so sending one
// bad URL would silently cancel the purge of the good ones too.
func (c *Client) PurgeObjectURLs(ctx context.Context, urls []string) ([]string, error) {
	prefixes := c.zonePrefixes(urls)
	if len(prefixes) > maxPrefixesPerRequest {
		prefixes = prefixes[:maxPrefixesPerRequest]
	}
	return c.sendPrefixes(ctx, prefixes)
}

// sendPrefixes is one purge request for the prefixes given. The caller is
// responsible for the per-request ceiling (see PurgeObjectURLs, which chunks).
func (c *Client) sendPrefixes(ctx context.Context, prefixes []string) ([]string, error) {
	if len(prefixes) == 0 {
		return nil, nil
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
	prefixes := c.zonePrefixes(urls)
	if len(prefixes) == 0 {
		return Result{Status: StatusOutOfZone}
	}
	// CHUNKED, not truncated. A bulk status change can invalidate more cards than
	// one request may carry (a form is purged on two hosts, and a bulk action holds
	// up to 500 forms), and a silent truncation would leave the rest cached while
	// reporting `edge: purged`. The first refusal stops the loop and names itself:
	// retrying the remaining batches against an API that just rejected one would
	// only multiply the errors.
	var purged []string
	for start := 0; start < len(prefixes); start += maxPrefixesPerRequest {
		end := start + maxPrefixesPerRequest
		if end > len(prefixes) {
			end = len(prefixes)
		}
		sent, err := c.sendPrefixes(ctx, prefixes[start:end])
		purged = append(purged, sent...)
		if err != nil {
			return Result{Status: StatusFailed, URLs: purged, Err: err}
		}
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

// OGFields is Fields() for a social-card purge, plus the freshness window that
// bounds staleness when the purge did NOT happen. A caller that purged a card
// must report BOTH: `edge: not_configured` alone leaves an operator guessing
// how long the old card keeps being served, and the number is the answer.
func (r Result) OGFields() map[string]any {
	if r.Status == "" {
		// No purge was requested: publishing `edge: ""` would name an outcome
		// that was never attempted.
		return nil
	}
	f := r.Fields()
	if r.Status != StatusPurged {
		f["edge_max_age_s"] = OGImageMaxAgeSeconds
	}
	return f
}
