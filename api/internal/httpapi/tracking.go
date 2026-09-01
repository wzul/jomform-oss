// Tracking scripts (Google Analytics, Facebook Pixel, any JS) — global and
// per form, injected into the public form <head>. Sanitised: only <script/src>
// and <meta> tags allowed (no event handlers, no document.cookie writes).
package httpapi

import (
	"regexp"
	"strings"
)

var (
	scriptSrcRe  = regexp.MustCompile(`(?is)^<script\s+src=["']https://[a-z0-9.-]+[^"']*["']\s*(defer|async)?\s*></script>$`)
	metaRe       = regexp.MustCompile(`(?is)^<meta\s+[^>]*>$`)
	commentRe    = regexp.MustCompile("(?s)^<!--.*-->$")
	disallowedRe = regexp.MustCompile(`(?i)(onload|onerror|onclick|document\s*\.\s*cookie|eval\s*\(|javascript:)`)
)

// sanitizeTracking allows only whitelisted benign markup for <head>.
func sanitizeTracking(js string) string {
	out := []string{}
	for _, seg := range strings.Split(strings.TrimSpace(js), "\n") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if commentRe.MatchString(seg) || scriptSrcRe.MatchString(seg) || metaRe.MatchString(seg) {
			if !disallowedRe.MatchString(seg) {
				out = append(out, seg)
			}
		}
	}
	return strings.Join(out, "\n")
}

// headInjection composes final head HTML: global first, then per-form.
func headInjection(global, perForm string) string {
	global = sanitizeTracking(global)
	perForm = sanitizeTracking(perForm)
	if global == "" && perForm == "" {
		return ""
	}
	return global + "\n" + perForm
}
