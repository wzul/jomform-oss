// Space-scoped public form URL resolution for buyer-facing surfaces
// (receipt email, receipt PDF). Issue #83: a purchased form's canonical URL
// is {space}.jomform.com/{slug} (or a verified custom domain), never the apex
// thank-you page. Mirrors formURL() in httpapi/rest_handlers.go and
// formPublicURL() in mcpsrv/forms.go (issue #77).
package payments

import (
	"context"
	"fmt"
	"strings"
)

// formPublicURL resolves the canonical space-scoped URL for a form owned by
// the given account. Priority: verified custom domain, then space subdomain.
// Returns "" when neither a custom domain nor a space slug is resolvable
// (caller falls back to the apex thank-you page).
func (s *Service) formPublicURL(ctx context.Context, accountID, formID string) string {
	if s.Forms == nil || accountID == "" || formID == "" {
		return ""
	}
	f, err := s.Forms.GetByID(ctx, accountID, formID)
	if err != nil || f == nil || f.Slug == "" {
		return ""
	}
	base := s.spaceBaseURL(ctx, accountID)
	if base == "" {
		return ""
	}
	return base + "/" + f.Slug
}

// spaceBaseURL resolves the scheme+host a buyer-facing page for the given
// account lives under: a verified custom domain first, else the space
// subdomain ({space}.jomform.com). Returns "" when neither is resolvable.
func (s *Service) spaceBaseURL(ctx context.Context, accountID string) string {
	if s.Accounts != nil {
		if acc, aerr := s.Accounts.ByID(ctx, accountID); aerr == nil && acc.CustomDomain != "" && acc.CustomDomainVerified {
			return "https://" + strings.TrimRight(acc.CustomDomain, "/")
		}
	}
	if s.Spaces != nil {
		if slug, serr := s.Spaces.SpaceSlugByAccount(ctx, accountID); serr == nil && slug != "" {
			return fmt.Sprintf("https://%s.jomform.com", slug)
		}
	}
	return ""
}
