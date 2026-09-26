// Workspace-scoped public form URL resolution for buyer-facing surfaces
// (receipt email, receipt PDF). Issue #83: a purchased form's canonical URL
// is {workspace}.jomform.com/{slug} (or a verified custom domain), never the apex
// thank-you page. Mirrors formURL() in httpapi/rest_handlers.go and
// formPublicURL() in mcpsrv/forms.go (issue #77).
package payments

import (
	"context"
	"fmt"
	"strings"
)

// formPublicURL resolves the canonical workspace-scoped URL for a form owned by
// the given account. Priority: verified custom domain, then workspace subdomain.
// Returns "" when neither a custom domain nor a workspace slug is resolvable
// (caller falls back to the apex thank-you page).
func (s *Service) formPublicURL(ctx context.Context, accountID, formID string) string {
	if s.Forms == nil || accountID == "" || formID == "" {
		return ""
	}
	f, err := s.Forms.GetByID(ctx, accountID, formID)
	if err != nil || f == nil || f.Slug == "" {
		return ""
	}
	base := s.workspaceBaseURL(ctx, accountID)
	if base == "" {
		return ""
	}
	return base + "/" + f.Slug
}

// workspaceBaseURL resolves the scheme+host a buyer-facing page for the given
// account lives under: a verified custom domain first, else the subdomain host
// the public serving path resolves ({host}.jomform.com). Returns "" only when
// neither is resolvable — in particular an account with NO workspace resolves to
// nothing here, so a receipt's "view your order" link would fall back to the apex
// thank-you page instead of the form. A signup deliberately starts in exactly
// that state (#718: the user is asked to create a workspace first), so this is a
// real case, not a defensive branch. See WorkspaceStore.PublicHostSlugByAccount.
func (s *Service) workspaceBaseURL(ctx context.Context, accountID string) string {
	if s.Accounts != nil {
		if acc, aerr := s.Accounts.ByID(ctx, accountID); aerr == nil && acc.CustomDomain != "" && acc.CustomDomainVerified {
			return "https://" + strings.TrimRight(acc.CustomDomain, "/")
		}
	}
	if s.Workspaces != nil {
		if slug, serr := s.Workspaces.PublicHostSlugByAccount(ctx, accountID); serr == nil && slug != "" {
			return fmt.Sprintf("https://%s.jomform.com", slug)
		}
	}
	return ""
}
