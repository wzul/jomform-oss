// Workspace slug rules — ONE definition, shared by every writer.
//
// Before this file the rule existed as three separate copies (the REST create
// handler's inline loop, mcpsrv.validateWorkspaceSlug, and slugify's sanitising
// steps), so a new writer had to re-implement it and could disagree with the DB
// or with the other surface. The auto-provisioning path added here needed the
// same rule a fourth time, which is where "one more copy" stops being
// acceptable: both writers now delegate, and the legacy provision reuses the
// same validator so it cannot mint a slug the API would itself reject.
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
