// Package assetgate — background scan of uploaded merchant code assets (issue #364).
//
// #361 added the ability for a merchant to upload CSS/JS/HTML (and SVG/fonts)
// to customise their landing/space page, stored and served tenant-scoped.
// That introduced a fundamentally different risk from #255's sanitised,
// non-executing custom HTML: served+executable JS/CSS could be used to host
// malicious scripts. This package implements the security gate (#364):
//
//   - A cheap deterministic heuristic scan (DeterministicScan) runs at upload
//     time so obviously malicious or suspicious files are never served.
//   - Files that pass the deterministic scan are enqueued as an "asset_scan"
//     background job (same durable Valkey queue as email #199 / spam judge
//     #245) and classified by an LLM (malicious / benign / needs review).
//   - The scan result is persisted on the media row's `scan_status` column
//     (added by migration 000040). `ok` is the ONLY status that allows a code
//     asset to be served; every other status (pending / needs_review /
//     quarantined) keeps the route in 404 so a not-yet-scanned or flagged
//     asset is never publicly executable.
//
// Images (customer + merchant form attachments and image blocks) are NOT code
// assets and are unaffected by the gate.

package assetgate

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/wzul/jomform/api/internal/dbstore"
	"github.com/wzul/jomform/api/internal/emailqueue"
)

// Media scan statuses stored on media.scan_status (migration 000040).
const (
	// StatusPending means "not scanned yet" — the default. Not served.
	StatusPending = "pending"
	// StatusScanning is set while an asset_scan background job is queued/running.
	StatusScanning = "scanning"
	// StatusOK means the asset passed deterministic scan + AI review. Served.
	StatusOK = "ok"
	// StatusNeedsReview means a human must review before serving. Not served.
	StatusNeedsReview = "needs_review"
	// StatusQuarantined means the asset was flagged malicious; never served
	// until a human explicitly clears it.
	StatusQuarantined = "quarantined"
)

// JobType is the emailqueue.Job.Type used for asset-scan jobs. It rides the
// same durable Valkey-backed background queue as email (#199) and spam judge
// (#245) so scans survive a restart.
const JobType = "asset_scan"

// ScanJob carries everything the background worker needs to classify one
// uploaded code asset. The bytes are re-fetched from object storage by key in
// the worker (kept out of the queue body so Valkey entries stay small).
type ScanJob struct {
	MediaID   string `json:"media_id"`
	Key       string `json:"key"`
	AccountID string `json:"account_id"`
	Mime      string `json:"mime"`
}

// Enqueue pushes an asset-scan job onto the background queue. Best-effort: a
// failure is logged and the asset stays in 'pending' (fail-closed — never
// served) rather than being served unscanned.
func Enqueue(ctx context.Context, q *emailqueue.Queue, job ScanJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return q.Enqueue(ctx, emailqueue.Job{
		Type:      JobType,
		AccountID: job.AccountID,
		Body:      string(payload),
	})
}

// ---------------------------------------------------------------------------
// Deterministic heuristic scanner (issue #364, layer 1 — cheap, always runs).
//
// The scanner is deliberately conservative to avoid false positives on a
// legitimate custom landing page, while still catching obvious malware. It
// returns StatusOK (nothing wrong), StatusNeedsReview (suspicious — a human
// should look before serving), or StatusQuarantined (active code-execution /
// obfuscation markers that a normal landing page should never contain).
// ---------------------------------------------------------------------------

// Verdict is the result of a deterministic scan.
type Verdict struct {
	// Status is one of StatusOK / StatusNeedsReview / StatusQuarantined.
	Status string
	// Reasons lists the markers that fired (for the review queue / logging).
	Reasons []string
}

// DeterministicScan inspects the raw bytes of an allowlisted code asset and
// returns a fail-closed judgement. `mime` picks file-type-specific rules
// (CSS is treated differently from JS/HTML because a CSS file legitimately
// references external fonts/images via url()/@import).
func DeterministicScan(data []byte, mime string) Verdict {
	lower := strings.ToLower(string(data))
	var v Verdict

	isCSS := strings.Contains(mime, "text/css")
	isHTML := strings.Contains(mime, "text/html")

	addM := func(reason string) {
		v.Reasons = append(v.Reasons, reason)
		v.Status = StatusQuarantined
	}
	addS := func(reason string) {
		if v.Status != StatusQuarantined {
			v.Status = StatusNeedsReview
		}
		v.Reasons = append(v.Reasons, reason)
	}

	// --- Hard block: active code execution the browser would run. ---------
	for _, m := range []string{
		"eval(", "eval (", "new function(", "new function (", "function(",
		"document.write(", "document.writeln(", "constructor.constructor",
		"constructor[\"constructor\"]", "constructor['constructor']",
	} {
		if strings.Contains(lower, m) {
			addM("active code execution marker: " + m)
		}
	}
	// CSS-as-code and javascript: URI vectors.
	for _, m := range []string{"expression(", "-moz-binding", "behavior:url", "url(javascript:"} {
		if isCSS && strings.Contains(lower, m) {
			addM("executable CSS marker: " + m)
		}
	}
	if strings.Contains(lower, "javascript:") {
		addM("javascript: URI present")
	}

	// --- Suspicious: flag for a human (or AI) review. ---------------------
	for _, m := range []string{
		".atob(", ".btoa(", "atob(", "btoa(", // base64 encode/decode = obfuscation
		"sendbeacon(", "navigator.sendbeacon(", "xmlhttprequest", // tracking beacons
		"//<![cdata[", // embedded script in SVG/HTML
		"srcdoc=",     // iframe srcdoc can smuggle HTML/JS
	} {
		if strings.Contains(lower, m) && !(isCSS) {
			addS("obfuscation/beacon marker: " + m)
		}
	}
	// data: URIs that can carry executable content.
	for _, m := range []string{"data:text/html", "data:text/javascript", "data:text/js", "data:application/xhtml"} {
		if strings.Contains(lower, m) && !isCSS {
			addS("executable data: URI: " + m)
		}
	}
	// Long contiguous base64 — obfuscation heuristic. Minified JS can have
	// long strings, so a high threshold and "needs review", never hard block.
	if longestBase64Run(lower) > 256 {
		addS("suspicious long base64 blob (obfuscation)")
	}
	// External URL in JS/HTML (the script-load / beacon vector). CSS external
	// fonts/images are legitimate and ignored. `data:` is handled above.
	if !isCSS && hasExternalURL(lower) {
		addS("reference to external (non-jomform) URL from JS/HTML")
	}
	// HTML script/iframe pointing at an external host.
	if isHTML {
		for _, m := range []string{"<script src=\"http", "<script src='http", "<script src=\"//", "<iframe src=\"http", "<iframe src='http", "<object data=\"http", "<embed src=\"http"} {
			if strings.Contains(lower, m) {
				addS("external script/embed in HTML: " + m)
				break
			}
		}
	}
	// Phishing language cluster (only when it's a landing/collect page).
	for _, m := range []string{
		"verify your account", "confirm your identity", "account suspended",
		"credit card details", "password reset", "login to continue",
		"your payment is required", "reactivate your account",
	} {
		if strings.Contains(lower, m) {
			addS("phishing-marking text: " + m)
		}
	}
	// Prototype pollution vector.
	if strings.Contains(lower, "__proto__") {
		addS("prototype-pollution marker: __proto__")
	}

	if v.Status == "" {
		v.Status = StatusOK
	}
	return v
}

// hostIsJomform reports whether a host is jomform.com or a *.jomform.com
// subdomain (the app's own origin). Everything else is "external".
func hostIsJomform(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "jomform.com" || h == "www.jomform.com" {
		return true
	}
	return strings.HasSuffix(h, ".jomform.com")
}

// hasExternalURL returns true when s contains any http(s) URL whose host is
// NOT the app's own jomform.com origin. Used to catch cross-origin
// script-load / data-exfiltration in JS/HTML assets.
func hasExternalURL(s string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], "http://")
		j := strings.Index(s[idx:], "https://")
		start := -1
		if i >= 0 && (j < 0 || i < j) {
			start = i
		} else if j >= 0 {
			start = j
		}
		if start < 0 {
			return false
		}
		start += idx
		// Find the end of the URL (up to a quote, space, close paren/bracket, or end).
		end := start + len("https://")
		for end < len(s) {
			c := s[end]
			if c <= ' ' || c == '"' || c == '\'' || c == ')' || c == ']' || c == '}' || c == '<' || c == '>' || c == ';' {
				break
			}
			end++
		}
		raw := s[start:end]
		if u, err := url.Parse("http://" + strings.TrimPrefix(strings.TrimPrefix(raw, "http://"), "https://")); err == nil && u.Host != "" {
			if !hostIsJomform(u.Host) {
				return true
			}
		}
		idx = end
		if idx >= len(s) {
			return false
		}
	}
}

var base64Re = regexp.MustCompile(`[A-Za-z0-9+/=]{16,}`)

// longestBase64Run returns the length of the longest contiguous run of
// base64-ish characters, used as an obfuscation heuristic.
func longestBase64Run(s string) int {
	found := base64Re.FindAllString(s, -1)
	best := 0
	for _, f := range found {
		if len(f) > best {
			best = len(f)
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Background AI review (issue #364, layer 2).
// ---------------------------------------------------------------------------

// ObjectFetcher fetches an object's bytes by key from the media bucket.
// *httpapi.ObjectStore satisfies it structurally (no import cycle).
type ObjectFetcher interface {
	GetBytes(ctx context.Context, key string) ([]byte, string, error)
}

var _ ObjectFetcher = (*storeHasGetBytes)(nil)

// storeHasGetBytes is an unexported marker so the interface is satisfied
// structurally and documents the method contract.
type storeHasGetBytes struct{}

func (storeHasGetBytes) GetBytes(ctx context.Context, key string) ([]byte, string, error) {
	return nil, "", nil
}

// AssetClassifier classes a code snippet as MALICIOUS / BENIGN / NEEDS REVIEW.
// *llm.Client satisfies it structurally.
type AssetClassifier interface {
	ClassifyAsset(ctx context.Context, code string) (string, error)
}

// Handler implements emailqueue.Handler for asset_scan jobs. It is wired into
// the background worker's dispatcher (same pattern as spamjudge.Handler), so
// the same Valkey queue + worker serves email, spam-judge and asset-scan jobs.
type Handler struct {
	Media *dbstore.MediaStore
	Store ObjectFetcher
	LLM   AssetClassifier
}

// Send classifies one asset-scan job: re-checks deterministic heuristics
// (defence in depth), then asks the LLM for a verdict and persists it to the
// media row. A returned error triggers the worker's retry-with-backoff ladder
// so a transient LLM outage doesn't bake in a wrong status.
func (h *Handler) Send(ctx context.Context, job emailqueue.Job) error {
	var sj ScanJob
	if err := json.Unmarshal([]byte(job.Body), &sj); err != nil {
		slog.Warn("assetgate_bad_job", "err", err)
		return nil
	}
	if sj.MediaID == "" || sj.Key == "" {
		slog.Warn("assetgate_bad_job_fields", "account", sj.AccountID)
		return nil
	}
	if h.Media == nil {
		slog.Warn("assetgate_no_store", "account", sj.AccountID, "media", sj.MediaID)
		return nil
	}
	slog.Info("assetgate_scan_start", "account", sj.AccountID, "media", sj.MediaID, "key", sj.Key)

	// Re-fetch the bytes and re-run deterministic heuristics defensively, so
	// a file that somehow reached the queue without being flagged upstream is
	// still caught before the (fallible) AI step.
	final := StatusOK
	if h.Store != nil {
		body, mime, err := h.Store.GetBytes(ctx, sj.Key)
		if err != nil {
			slog.Warn("assetgate_fetch_failed", "media", sj.MediaID, "key", sj.Key, "err", err)
			return err // retry — object may be still propagating
		}
		det := DeterministicScan(body, mime)
		if det.Status == StatusQuarantined {
			final = StatusQuarantined
			if err := h.Media.SetScanStatus(ctx, sj.AccountID, sj.MediaID, final, strings.Join(det.Reasons, "; ")); err != nil {
				slog.Warn("assetgate_store_failed", "media", sj.MediaID, "err", err)
				return err
			}
			slog.Warn("assetgate_quarantined_deterministic", "media", sj.MediaID, "key", sj.Key)
			return nil
		}
	}
	if h.LLM == nil {
		// No AI available — leave the asset blocked (pending/needs_review).
		// Never mark ok without a scan.
		slog.Warn("assetgate_no_llm", "media", sj.MediaID, "account", sj.AccountID)
		return nil
	}
	body, _, err := h.Store.GetBytes(ctx, sj.Key)
	if err != nil {
		return err
	}
	verdict, err := h.LLM.ClassifyAsset(ctx, string(body))
	if err != nil {
		slog.Warn("assetgate_ai_failed", "media", sj.MediaID, "err", err)
		return err
	}
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "malicious":
		final = StatusQuarantined
	case "needs review", "needs_review", "review":
		final = StatusNeedsReview
	default:
		final = StatusOK
	}
	reason := "AI judge verdict: " + verdict
	if err := h.Media.SetScanStatus(ctx, sj.AccountID, sj.MediaID, final, reason); err != nil {
		slog.Warn("assetgate_store_failed", "media", sj.MediaID, "err", err)
		return err
	}
	slog.Info("assetgate_scan_done", "media", sj.MediaID, "key", sj.Key, "status", final)
	return nil
}
