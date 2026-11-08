// Unroutable-recipient guard (card t_641d62d5).
//
// A recipient whose domain can never accept delivery must not be handed to the
// mail provider at all. Two families qualify, and both are deterministic —
// neither needs DNS:
//
//   - RFC 2606 / RFC 6761 special-use names: `example.com`, `example.net`,
//     `example.org`, and anything under the reserved TLDs `.test`, `.invalid`,
//     `.localhost` and `.example`. These names are reserved precisely so they
//     cannot be routed; `example.com` also publishes a null MX (RFC 7505,
//     `example.com. MX 0 .`), which is an explicit "this domain accepts no
//     mail". A real send to one of them is a guaranteed hard bounce.
//   - Known disposable/temp-mail domains that QA keeps aiming at. These do have
//     MX records, so a DNS check would happily pass them; they appear here
//     because they are throwaway mailboxes whose addresses were already
//     suppressed by SES after the first bounce, and the provider keeps being
//     asked to mail more of them.
//
// WHY A STATIC LIST AND NOT AN MX LOOKUP: the reserved names cannot be
// delegated, so a lookup can only ever confirm what the rule already knows —
// while adding a DNS dependency that fails open on a resolver timeout exactly
// when the caller is a reputation guard that should fail closed. The list is the
// guarantee; an MX probe would be a weaker, slower version of it. (The MX
// pre-check for registration is a different guard with a different job — see
// docs/SES-REPUTATION.md §3.)
//
// A hard bounce is charged against the sending identity's reputation, and one
// identity carries every receipt, notification and login link on the platform,
// so a handful of avoidable bounces is a platform-wide risk, not a local one.
package email

import "strings"

// Reasons returned by UnroutableDomain.
//
// They are a small closed set because they end up as a metric label
// (`jomform_ses_unroutable_total{reason=…}`) and in a structured log line; a
// free-text reason would make both unbounded.
const (
	// UnroutableReserved is an RFC 2606 / RFC 6761 special-use name.
	UnroutableReserved = "reserved"
	// UnroutableDisposable is a known disposable / QA recipient provider.
	UnroutableDisposable = "disposable"
)

// reservedDomains are the RFC 2606 second-level names that are never delegated.
// The rule is suffix-based, so `mail.example.com` is refused too: nothing under
// a reserved name can be routed to a real mailbox.
var reservedDomains = []string{
	"example.com",
	"example.net",
	"example.org",
}

// reservedTLDs are the RFC 6761 special-use TLDs whose every subdomain is
// unroutable.
var reservedTLDs = []string{
	"test",
	"invalid",
	"localhost",
	"example",
}

// disposableDomains are throwaway/temp-mail providers seen on this deployment's
// SES suppression list after their addresses hard-bounced. They resolve to real
// MX hosts, so only this list can refuse them.
var disposableDomains = []string{
	"uberip.com",
	"1secmail.net",
	"simulator.amazonses.com",
}

// UnroutableDomain reports whether addr's domain can never receive mail, and
// why. The second return is false for every other address (including a
// malformed one with no domain — format validation is a different guard's job),
// so a caller can treat true as "do not send, and do not treat it as an error".
//
// Matching is case-insensitive, ignores a trailing root dot, and applies to the
// domain and all of its subdomains.
func UnroutableDomain(addr string) (reason string, unroutable bool) {
	domain := strings.TrimSuffix(DomainOf(addr), ".")
	if domain == "" {
		return "", false
	}
	if matchesDomainOrSubdomain(domain, reservedDomains) || matchesDomainOrSubdomain(domain, reservedTLDs) {
		return UnroutableReserved, true
	}
	if matchesDomainOrSubdomain(domain, disposableDomains) {
		return UnroutableDisposable, true
	}
	return "", false
}

// matchesDomainOrSubdomain reports whether domain is one of the names in list or
// a subdomain of one. The `"." + name` suffix rather than a bare suffix keeps
// `notexample.com` from matching `example.com`.
func matchesDomainOrSubdomain(domain string, list []string) bool {
	for _, name := range list {
		if domain == name || strings.HasSuffix(domain, "."+name) {
			return true
		}
	}
	return false
}
