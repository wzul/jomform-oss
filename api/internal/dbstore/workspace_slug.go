// Workspace slug rules — the definition the LEGACY PROVISION reuses.
//
// WHAT IS TRUE TODAY (measured on main, not assumed). This file's rule exists so
// the auto-provisioning path cannot mint a slug the API would itself reject:
// ValidateWorkspaceSlug has exactly ONE production caller,
// ProvisionLegacyWorkspaceForOwner (workspace.go), in this package. It is
// enforced there against the derived slug before the workspace is inserted.
//
// WHAT IS NOT TRUE — and used to be claimed here. A previous version of this
// comment said "the rule existed as three separate copies ... both writers now
// delegate". Only one of the other two did. `grep -n 'ValidateWorkspaceSlug' api/
// | grep -v _test` returns the definition, this comment, and that one call site:
// the REST create handler (httpapi/rest_workspaces.go) and
// mcpsrv.validateWorkspaceSlug still each carry their own inline copy of the
// length / charset / "-jomform" rules. The copies agree today, so nothing is
// broken — but a doc comment asserting a de-duplication that has not happened is
// the "an invariant nothing verifies" shape this chain keeps having to fix.
//
// The remaining copies are known and tracked, not forgotten: PR 691
// (feat(workspace): rename a workspace's name and public slug) makes both
// writers delegate here, and the reserved-subdomain rule already lives in ONE
// place (internal/urlguard) and is consumed by both. Until that lands, correct
// this comment rather than the code: rewriting the two handlers here would
// duplicate an in-flight, reviewed change and guarantee a conflict.
package dbstore

import (
	"errors"
	"regexp"
	"strings"
)

var workspaceSlugUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// ValidateWorkspaceSlug enforces the workspace slug contract:
//
//	3-40 characters, a-z 0-9 and dash only, and must not end in "jomform"
//	(the wildcard subdomain suffix).
func ValidateWorkspaceSlug(slug string) error {
	if len(slug) < 3 || len(slug) > 40 {
		return errors.New("slug 3-40 aksara")
	}
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return errors.New("slug: a-z, 0-9, dash sahaja")
		}
	}
	if strings.HasSuffix(slug, "jomform") {
		return errors.New("slug tidak sah")
	}
	return nil
}

// SlugFromEmailLocalPart derives a workspace slug from an email address for the
// LEGACY auto-provisioning path (Wan's rule, 2026-09-20: "guna 5 aksara pertama
// dari email address").
//
// It takes the first 5 characters of the RAW local part and THEN sanitises, which
// is not the same as taking 5 characters of the already-sanitised local part:
// "a.b.c.d.e@x.test" yields "a-b-c" here (the dot is expanded to a dash, so the
// five raw characters survive as five), while sanitise-first would yield
// "a-b-c-d-e" from a nine-character window. Stated explicitly because the two
// answers differ and the rule is only unambiguous once chosen.
//
// The result is padded from the user id when sanitising leaves fewer than the
// 3-character minimum (a local part like "-.-" sanitises to ""), so this can
// never return a slug that ValidateWorkspaceSlug would reject on length.
func SlugFromEmailLocalPart(email, userID string) string {
	local := strings.ToLower(strings.TrimSpace(email))
	if at := strings.Index(local, "@"); at >= 0 {
		local = local[:at]
	}
	raw := local
	if len(raw) > 5 {
		raw = raw[:5]
	}
	base := strings.ReplaceAll(raw, ".", "-")
	base = workspaceSlugUnsafe.ReplaceAllString(base, "")
	base = strings.Trim(base, "-")
	for len(base) < 3 {
		pad := userID
		if len(pad) > 8 {
			pad = pad[:8]
		}
		if pad == "" {
			break
		}
		// Hex from a uuid is already slug-safe (a-f, 0-9).
		base += pad
		base = workspaceSlugUnsafe.ReplaceAllString(base, "")
		break
	}
	if len(base) > 40 {
		base = base[:40]
	}
	return strings.Trim(base, "-")
}
