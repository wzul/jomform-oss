// Workspace slug rules — the ONE definition every write path reuses.
//
// WHAT IS TRUE TODAY (measured, not assumed). This file's rule exists so no
// write path can mint a slug the API would itself reject. It has THREE
// production callers, all in this repo:
//
//	ProvisionLegacyWorkspaceForOwner (dbstore/workspace.go) — the derived slug
//	REST create/rename (httpapi/rest_workspaces.go)
//	MCP create/rename (mcpsrv/workspaces.go, via validateWorkspaceSlug)
//
// The last two DELEGATE here as of PR 691 (feat(workspace): rename a
// workspace's name and public slug). Before that each carried its own inline
// copy of the length / charset / "-jomform" rules; the copies agreed, but a
// shared rule with three definitions drifts silently, which is the repo's
// standing rule against hand-rolling a second copy (see urlguard).
//
// The reserved-subdomain leg is part of THIS function rather than a separate
// check in each transport: the list lives in internal/urlguard and the rule is
// enforced here, so REST and MCP cannot drift into two different answers about
// a name JomForm owns. It runs FIRST, before the length and charset legs — "s3"
// is a reserved label AND shorter than the 3-character minimum, and if the
// shape rule won the caller would be told their input was malformed and would
// retry a variation of the sensitive name. Policy refusal beats shape refusal.
package dbstore

import (
	"errors"
	"regexp"
	"strings"

	"github.com/wzul/jomform/api/internal/urlguard"
)

var workspaceSlugUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

// Rejections from ValidateWorkspaceSlug. They carry the user-facing wording, so
// every transport returns the same message for the same bad input instead of
// restating it (REST returns the text as the 400 body, MCP as the tool error,
// the dashboard shows it inline).
//
// A caller that needs to tell one refusal from another (the MCP rename tool
// switches on them) matches these by errors.Is rather than by comparing text.
var (
	ErrWorkspaceSlugLength   = errors.New("slug 3-40 characters")
	ErrWorkspaceSlugChars    = errors.New("slug: a-z, 0-9, dash only")
	ErrWorkspaceSlugReserved = errors.New("invalid slug")
)

// ErrWorkspaceSlugReservedSubdomain is the reserved-LIST refusal: the label the
// caller asked for is a host JomForm owns or must protect (see
// urlguard.ReservedSubdomain — infrastructure, product and support/billing
// looking names).
//
// WHY IT IS A SEPARATE SENTINEL from ErrWorkspaceSlugReserved, and not a reuse
// of it. The two legs are different rules with different wire answers, and the
// transport switches match by errors.Is, so one sentinel cannot serve both:
//
//	ErrWorkspaceSlugReserved — the "*jomform" TAIL rule. The caller's own input
//	                           is shaped badly, so the answer is the generic
//	                           slug wording ("invalid slug") and the generic
//	                           slug code.
//	ErrWorkspaceSlugReservedSubdomain — a POLICY refusal about a name that is
//	                           perfectly well formed. Its answer names WHY (the
//	                           label is JomForm's) and carries the stable
//	                           `reserved_subdomain` token the dashboard matches
//	                           on (#694) instead of the generic slug code.
//
// Before this existed the reserved leg returned a FRESH errors.New, which
// satisfied NO sentinel: every transport switch fell through, so REST answered
// 500 {"code":"internal"} and logged an ERROR/stack trace for plain user input,
// while MCP answered `db: ...` (t_758bb125). The list was enforced — no workspace
// could be renamed onto a reserved label — but the refusal could not be reported.
var ErrWorkspaceSlugReservedSubdomain = errors.New("slug is reserved for JomForm")

// reservedSlugRefusal is the reserved-label refusal. It answers the class
// sentinel to errors.Is (so a transport classifies it without reading prose)
// while its message is the SHARED urlguard wording, which names the label and
// therefore cannot be the sentinel's own fixed text.
type reservedSlugRefusal struct{ label string }

func (e *reservedSlugRefusal) Error() string { return urlguard.ReservedSubdomainMessage(e.label) }

func (e *reservedSlugRefusal) Unwrap() error { return ErrWorkspaceSlugReservedSubdomain }

// ValidateWorkspaceSlug normalises a caller-supplied slug and reports whether it
// is usable as a workspace host. It returns the NORMALISED slug (trimmed and
// lower-cased) so a caller writes exactly what was validated rather than
// re-deriving it.
//
// The rule:
//   - trimmed, then lower-cased, so "  MyShop  " is accepted as "myshop";
//   - a reserved label (see urlguard.ReservedSubdomain) is refused — it is a
//     host JomForm owns or must protect;
//   - 3-40 characters;
//   - only a-z, 0-9 and '-';
//   - must not END in "jomform" (that would produce a host that collides with
//     the product's own subdomains — e.g. "mcp.jomform" — or an
//     attacker-shaped "jomform" host).
func ValidateWorkspaceSlug(slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if urlguard.ReservedSubdomain(slug) {
		// The refusal CLASS is a sentinel the transports match on; its MESSAGE is
		// the shared urlguard wording, which names the label. Returning the bare
		// sentinel here would answer "slug is reserved for JomForm" with no label,
		// and returning a fresh errors.New (what this used to do) satisfies no
		// sentinel at all — the shape that made REST answer 500 and MCP `db: ...`.
		return "", &reservedSlugRefusal{label: slug}
	}
	if len(slug) < 3 || len(slug) > 40 {
		return "", ErrWorkspaceSlugLength
	}
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return "", ErrWorkspaceSlugChars
		}
	}
	if strings.HasSuffix(slug, "jomform") {
		return "", ErrWorkspaceSlugReserved
	}
	return slug, nil
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
