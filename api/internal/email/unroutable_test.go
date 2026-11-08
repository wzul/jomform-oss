package email

// Unroutable-recipient guard (card t_641d62d5).
//
// The guard is a static predicate on purpose, so its tests are plain table
// tests: no DNS, no Valkey, no mailer. What they pin is the exact SET of names
// that can never receive mail, because a false negative is a hard bounce
// charged against the sending identity and a false positive silently drops a
// real login link.

import "testing"

func TestUnroutableDomainRefusesNamesThatCannotReceiveMail(t *testing.T) {
	cases := []struct {
		addr   string
		reason string
		why    string
	}{
		// The address that produced the bounce this card exists for.
		{"nonexistent-t_c161c7d2@example.com", UnroutableReserved, "the exact address a QA run mailed on production"},
		{"test-bounce@example.com", UnroutableReserved, "the address already on the SES suppression list"},
		{"someone@EXAMPLE.COM", UnroutableReserved, "the reserved name is case-insensitive"},
		{"someone@Example.Com.", UnroutableReserved, "a trailing root dot is the same name"},
		{"someone@mail.example.com", UnroutableReserved, "nothing under a reserved name can be routed"},
		{"someone@example.net", UnroutableReserved, "RFC 2606 reserved"},
		{"someone@example.org", UnroutableReserved, "RFC 2606 reserved"},
		{"someone@foo.test", UnroutableReserved, "RFC 6761 reserved TLD"},
		{"someone@test", UnroutableReserved, "the reserved TLD itself"},
		{"someone@foo.invalid", UnroutableReserved, "RFC 6761 reserved TLD"},
		{"someone@foo.localhost", UnroutableReserved, "RFC 6761 reserved TLD"},
		{"someone@foo.example", UnroutableReserved, "RFC 6761 reserved TLD"},
		{"someone@uberip.com", UnroutableDisposable, "temp-mail provider on the suppression list"},
		{"someone@in.uberip.com", UnroutableDisposable, "a subdomain of a disposable provider"},
		{"someone@1secmail.net", UnroutableDisposable, "disposable provider on the suppression list"},
		{"someone@simulator.amazonses.com", UnroutableDisposable, "SES's own bounce simulator"},
	}
	for _, c := range cases {
		reason, refused := UnroutableDomain(c.addr)
		if !refused {
			t.Errorf("UnroutableDomain(%q) = not refused; want refused (%s)", c.addr, c.why)
			continue
		}
		if reason != c.reason {
			t.Errorf("UnroutableDomain(%q) reason = %q, want %q", c.addr, reason, c.reason)
		}
	}
}

func TestUnroutableDomainAllowsOrdinaryRecipients(t *testing.T) {
	// Every case here is an address this guard must NOT touch. The near-misses
	// matter most: a bare suffix match would refuse mytest.com / localhost.com
	// and silently drop a real user's login link.
	for _, addr := range []string{
		"wan@jomform.com",
		"Buyer@JomForm.COM",
		"someone@mytest.com",
		"someone@testmail.com",
		"someone@localhost.com",
		"someone@notexample.com",
		"someone@example.co",
		"someone@tests",
		"someone@jomform.com.",
	} {
		if reason, refused := UnroutableDomain(addr); refused {
			t.Errorf("UnroutableDomain(%q) = refused (%s); an ordinary recipient must be allowed", addr, reason)
		}
	}
}

func TestUnroutableDomainIgnoresAddressesWithNoDomain(t *testing.T) {
	// Format validation belongs to a different guard; this one must not claim
	// an address it cannot judge, or it would turn a 400 into a silent no-op.
	for _, addr := range []string{"", "no-at-sign", "trailing@", "  "} {
		if reason, refused := UnroutableDomain(addr); refused {
			t.Errorf("UnroutableDomain(%q) = refused (%s); an address with no domain is not this guard's call",
				addr, reason)
		}
	}
}
