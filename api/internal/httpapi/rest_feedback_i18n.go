// Feedback notification copy: the thread deep links, and the one normalisation
// the feedback surface needs on top of the shared transactional dictionary.
//
// WHY THIS FILE EXISTS
//
// Both feedback emails used to hardcode their Malay subject and body at the call
// site, so an English-speaking filer received "Balasan baharu pada maklum balas
// anda" — and the shared branded shell declared <html lang="ms"> over it. The
// reader's language is users.lang (migration 000066; values en|ms|zh|ta|ar, NULL
// until the user picks one), which the dashboard already honours.
//
// The COPY is not here. Both emails render through the ONE transactional
// dictionary (internal/emailtext): emailtext.Text for the subject and the body,
// emailtext.Phrase for the standalone pieces. That dictionary is what makes
// "the email follows the reader's language" a property of the system rather than
// of whichever file was edited last, and a second per-surface copy is how two
// dictionaries end up disagreeing. What stays here is what the dictionary cannot
// own — which THREAD a notification links to, and the language normaliser the
// feedback surface shares with the rest of the repo.
package httpapi

import (
	"net/url"
	"strings"

	"github.com/wzul/jomform/api/internal/emailtext"
)

// feedbackLang normalises a users.lang value to one of the five locales this
// repo ships, defaulting to English. Anything else — unset (the common case), or
// a value a future column change introduces — is English rather than Malay.
//
// It delegates to emailtext.NormalizeLang rather than repeating the switch: the
// dictionary already implements this rule (including a regional tag such as
// "ms-MY" resolving through its primary subtag), and a second copy is exactly
// how the two drift.
func feedbackLang(lang string) string { return emailtext.NormalizeLang(lang) }

// feedbackThreadLink is the filer's deep link to ONE thread.
//
// It is `/feedback?thread=<id>` and not `/feedback/<id>` on purpose: the console
// serves a single client-rendered feedback page that selects a thread in component
// state (web/src/app/feedback/page.tsx — it lists threads and opens one with
// openThread(id)), and there is no per-thread route to link to. A `/feedback/<id>`
// URL would 404 through the Next router, which is worse than the queue link it
// replaces. The page reads this parameter on mount and opens that thread, so the
// filer lands on the answer rather than hunting for it in a list.
//
// A thread id that the page cannot open (deleted, or another user's) degrades to
// the plain list — the link is a deep link, never an authorization.
func feedbackThreadLink(baseURL, id string) string {
	return feedbackDeepLink(baseURL, "/feedback", id)
}

// feedbackAdminLink is the operator's deep link to ONE thread on the triage
// surface, with the same reasoning as feedbackThreadLink: /admin is one page with
// the triage section inline (no per-thread route and no tab routing), so the
// thread is selected by parameter and web/src/app/admin/page.tsx opens it.
func feedbackAdminLink(baseURL, id string) string {
	return feedbackDeepLink(baseURL, "/admin", id)
}

// feedbackDeepLink joins a console path with the thread selector. The id is
// percent-escaped: it is a uuid today, but the link is built from a stored value
// and must stay a valid query string whatever it holds.
func feedbackDeepLink(baseURL, path, id string) string {
	base := strings.TrimRight(baseURL, "/")
	if id == "" {
		// No id (a bare thread struct in a test, or a row read without one):
		// the page itself, never a dangling `?thread=`.
		return base + path
	}
	return base + path + "?thread=" + url.QueryEscape(id)
}
