package dbstore

import "strings"

// ProtectedAccountEmails are addresses that NO purge may ever remove, whatever
// marker they carry (card t_6ae7f586).
//
// WHY THIS EXISTS. The designated production test login — the ONE account the
// lanes reuse instead of registering new ones — is `wan@jomform.com`, and it is
// verified=false on production. That makes it a candidate of the hourly
// unverified sweep TODAY: the sweep's only predicate is `verified = false AND
// created_at < cutoff`, so the account that must survive every purge is exactly
// the shape the purge is built to delete.
//
// So the protection is a property of the ACCOUNT, checked by every purge path
// (the unverified sweep's candidate list, its per-user transaction, and the
// test-account purge), and never a configuration a caller can pass in.
// `TestUnverifiedPurgeNeverRemovesAProtectedAccount` and
// `TestTestPurgeNeverRemovesAProtectedAccount` each prove the refusal AND prove
// it bites, by running the same sweep with this list neutralised.
var ProtectedAccountEmails = []string{"wan@jomform.com"}

// IsProtectedAccountEmail reports whether email is one of the designated
// protected accounts, using the package default list. Callers outside this
// package (the signup handler, which must refuse to MARK a protected address as
// a disposable test account) use this; the store uses its own list so a test can
// prove the guard is load-bearing.
func IsProtectedAccountEmail(email string) bool {
	return emailInList(email, ProtectedAccountEmails)
}

// emailInList is the one comparison rule: case-insensitive, trimmed. The users
// table is CITEXT, so a case mismatch would otherwise protect the wrong row.
func emailInList(email string, list []string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return false
	}
	for _, p := range list {
		if e == strings.ToLower(strings.TrimSpace(p)) {
			return true
		}
	}
	return false
}

// protectedList returns the addresses this store treats as protected.
//
// protectedOverride is nil for every store built by NewUserStore, i.e. in
// production and in ordinary tests. It exists ONLY so a test can construct a
// store with the safeguard neutralised and require the sweep to delete the
// protected row — the bite proof the card asks for ("neutralise the safeguard
// and it must fail"). A nil override is "use the default", never "protect
// nothing", so an empty list cannot be reached by accident.
func (s *UserStore) protectedList() []string {
	if s.protectedOverride != nil {
		return *s.protectedOverride
	}
	return ProtectedAccountEmails
}

// isProtectedEmail reports whether this store must never purge email.
func (s *UserStore) isProtectedEmail(email string) bool {
	return emailInList(email, s.protectedList())
}

// protectedLower returns the protected list lowercased, for the SQL predicate
// `lower(email::text) <> ALL($n::text[])`.
func (s *UserStore) protectedLower() []string {
	list := s.protectedList()
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, strings.ToLower(strings.TrimSpace(e)))
	}
	return out
}
