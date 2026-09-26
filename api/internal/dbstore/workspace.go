// Workspace: multi-tenant storefront layer. Each user may own many workspaces;
// forms, products, sales and settings hang off a workspace.
package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Workspace models the workspaces table.
type Workspace struct {
	ID    string
	Name  string
	Slug  string
	Owner string
}

// WorkspaceStore wraps workspaces.
type WorkspaceStore struct{ pool *pgxpool.Pool }

func NewWorkspaceStore(pool *pgxpool.Pool) *WorkspaceStore { return &WorkspaceStore{pool: pool} }

const workspaceSelect = `SELECT id, owner_user_id, name, slug FROM workspaces`

// scanWorkspace scans one workspace row.
//
// A MISSING row is reported as (nil, pgx.ErrNoRows) — nil so a caller's pointer
// test reads a miss as a miss, and the store's not-found sentinel so a caller
// that checks the error still gets one (the convention every sibling store
// follows; see notfound.go).
//
// Returning the ALLOCATED struct alongside the error instead of nil made
// create_workspace's uniqueness pre-check (mcpsrv/workspaces.go) — which tests
// `existing != nil` and discards the error — read a miss as a hit, so it refused
// every slug and no workspace could be created over MCP at all, while the same
// create worked over REST (which has no pre-check). That is why it went
// unnoticed.
func scanWorkspace(row rowScanner) (*Workspace, error) {
	s := &Workspace{}
	if err := row.Scan(&s.ID, &s.Owner, &s.Name, &s.Slug); err != nil {
		if isNoSuchRow(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return s, nil
}

// ByOwner lists workspaces owned by a user.
func (s *WorkspaceStore) ByOwner(ctx context.Context, userID string) ([]*Workspace, error) {
	rows, err := s.pool.Query(ctx, workspaceSelect+" WHERE owner_user_id=$1 ORDER BY created_at", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		sp, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// BySlug resolves a workspace by its URL slug.
func (s *WorkspaceStore) BySlug(ctx context.Context, slug string) (*Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" WHERE slug=$1", slug))
}

// ByID fetches one workspace.
func (s *WorkspaceStore) ByID(ctx context.Context, id string) (*Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" WHERE id=$1", id))
}

// Create makes a new workspace (slug unique) AND a fresh, empty account linked
// to it (issue #158). Each workspace owns its own account so a new workspace starts
// with a clean slate: no CHIP creds, no products, no sales, no domains, no
// webhooks. CHIP credentials are never shared across workspaces. The account
// slug is derived from the workspace slug (unique-verified by the caller) plus a
// short id suffix to avoid colliding with the workspace's own slug.
func (s *WorkspaceStore) Create(ctx context.Context, ownerUserID, name, slug string) (*Workspace, error) {
	sp := &Workspace{Owner: ownerUserID, Name: name, Slug: slug}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
	if err != nil {
		return sp, err
	}
	// Fresh account owned by the workspace. Empty CHIP creds, no products/sales.
	// The account slug is unique-verified by the caller (workspace slug) plus a
	// short id suffix; a collision is extremely unlikely and the DB enforces
	// uniqueness as a backstop.
	accSlug := slug + "-" + sp.ID[:8]
	_, aerr := s.pool.Exec(ctx,
		`INSERT INTO accounts (owner_user_id, name, slug, workspace_id)
		 VALUES ($1,$2,$3,$4)`,
		ownerUserID, name, accSlug, sp.ID)
	if aerr != nil {
		return sp, aerr
	}
	return sp, nil
}

// SoleEmptyUnscopedAccount reports the caller's ONE account when (a) the caller
// owns exactly one account, (b) that account is not linked to any workspace, and
// (c) it carries no CONTENT (no forms, products or sales).
//
// WHY THIS EXISTS (ask-first onboarding)
//   A signup creates an account and NO workspace (auth.go; Wan's decision
//   2026-09-20), and a user with zero workspaces is asked to create one before
//   they can reach any create surface. If that first workspace were created the
//   ordinary way, Workspaces.Create would insert a SECOND account linked to it
//   (issue #158) and the signup account would be left behind with
//   workspace_id NULL — reachable by nothing, while the dashboard reads the
//   account resolved from the scoped workspace. So the create path reuses this
//   account instead, and the user ends with exactly one.
//
// The CONTENT condition is the boundary of this method and not an optimisation:
// an account that already holds forms/products/sales is a legacy workspace-less
// account, and moving it (and the public host slug its published forms resolve
// through — PublicHostSlugByAccount) is a DATA MIGRATION decision that is
// enumerated and measured per user, not a side effect of pressing "create
// workspace". Such an account is reported as "" here so the caller keeps the
// fresh-account behaviour. Relaxing that guard is the legacy card's call.
//
// api_keys is deliberately NOT part of that boundary, although it was until this
// fix. A key is a CREDENTIAL, not content: it carries no public host slug and no
// published URL, so there is nothing for a link to move, while CreateForAccount
// already syncs api_keys.workspace_id with the account. Excluding key holders
// made this method unreachable on the one transport that always has a key — an
// /mcp caller authenticates through the key table (mcpsrv/http.go), so every MCP
// create_workspace call was refused the reuse and fell through to
// Workspaces.Create, leaving a fresh user owning TWO accounts with the agent's
// own credential stranded on the unreachable signup one (the dashboard could
// neither list nor revoke it). It also broke the REST path for any key-holding
// user. The key travels with its account instead; see
// TestSoleEmptyUnscopedAccountKeepsTheCallersKey.
func (s *WorkspaceStore) SoleEmptyUnscopedAccount(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT a.id
		FROM accounts a
		WHERE a.owner_user_id = $1
		  AND a.workspace_id IS NULL
		  AND (SELECT count(*) FROM accounts b WHERE b.owner_user_id = $1) = 1
		  AND NOT EXISTS (SELECT 1 FROM forms f WHERE f.account_id = a.id)
		  AND NOT EXISTS (SELECT 1 FROM products p WHERE p.account_id = a.id)
		  AND NOT EXISTS (SELECT 1 FROM sales s WHERE s.account_id = a.id)
		LIMIT 1`, userID).Scan(&id)
	if err != nil {
		// No such account (including "the caller owns several") is NOT an error
		// here: it means "use the ordinary fresh-account path".
		return "", nil
	}
	return id, nil
}

// CreateForAccount makes a new workspace (slug unique) and links an EXISTING
// account to it instead of inserting a fresh one — the ask-first onboarding path
// (see SoleEmptyUnscopedAccount). The account must already belong to
// ownerUserID; the UPDATE is owner-checked so a caller can never link somebody
// else's account into their new workspace.
//
// It also syncs the workspace_id carried by the account's dependent rows. This
// is required, not optional: trg_forms_sync_workspace_id fires only on INSERT /
// UPDATE OF account_id on forms (migration 000045), so linking the ACCOUNT does
// not move the rows that hang off it. The precedent is migration 000012, which
// synced forms/products/sales/api_keys explicitly. Under the content guard above
// there is normally nothing to move, but the sync belongs here so the method is
// correct for any caller rather than correct only because of its current one.
func (s *WorkspaceStore) CreateForAccount(ctx context.Context, ownerUserID, name, slug, accountID string) (*Workspace, error) {
	sp := &Workspace{Owner: ownerUserID, Name: name, Slug: slug}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sp, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug); err != nil {
		return sp, err
	}
	// Owner-checked link: the account must belong to the workspace's owner and
	// must not already be in another workspace.
	ct, err := tx.Exec(ctx,
		`UPDATE accounts SET workspace_id=$1, updated_at=now()
		 WHERE id=$2 AND owner_user_id=$3 AND workspace_id IS NULL`,
		sp.ID, accountID, ownerUserID)
	if err != nil {
		return sp, err
	}
	if ct.RowsAffected() != 1 {
		return sp, errAccountNotLinkable
	}
	for _, stmt := range []string{
		`UPDATE forms SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE products SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE sales SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE api_keys SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
	} {
		if _, err := tx.Exec(ctx, stmt, sp.ID, accountID); err != nil {
			return sp, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return sp, err
	}
	return sp, nil
}

// errAccountNotLinkable is returned when an account cannot be linked to a new
// workspace (not owned by the caller, or already linked elsewhere).
var errAccountNotLinkable = errors.New("akaun tidak boleh dikaitkan dengan workspace")

// ── Legacy onboarding: an owner who already has content but no workspace ──────
//
// WHY THIS EXISTS
//   Signup has always created an account and NO workspace, so every user who
//   joined before the ask-first flow (2026-09-20) owns forms that hang off an
//   account with accounts.workspace_id IS NULL. Those users cannot reach their
//   own forms in the dashboard: every scoped read resolves its account from the
//   selected workspace, and there is none to select (measured on prod: 4 users,
//   18 forms).
//
//   Wan's rule (2026-09-20, final revision): *has forms, no workspace* → CREATE
//   the workspace for him and attach the account + its forms, with the name and
//   slug derived from the first 5 characters of the email. Nothing is deleted.
//   *No forms, no workspace* → he is ASKED to create one (that is the ask-first
//   path in web/src/app/workspace-picker, not this method).
//
// This is the deliberate RELAXATION of the content guard that
// SoleEmptyUnscopedAccount enforces for the ask-first path. That guard refuses a
// content-carrying account precisely because moving one is a data decision
// rather than a side effect of pressing "create workspace" — this method is that
// data decision, so it is a separate, explicitly bounded entry point instead of
// a widened condition on the interactive one.

// LegacyProvisionedEmails are the system identities that must NEVER be
// auto-provisioned, however much content their account carries.
//
// templates@system.jomform owns the jomform-templates account, which owns 14
// published is_template rows and no workspace ON PURPOSE: migration 000059
// moved those rows there specifically so they STOP matching the workspace-scoped
// published queries (ListPublishedByWorkspace / ListPublishedByAccountSlug).
// They back FormStore.ListTemplates (form.go, no account filter) → TemplatePicker,
// the landing gallery and GET /api/templates. Giving that account a workspace
// would re-expose all 14 templates as a public storefront and empty the picker's
// gallery — the exact opposite of 000059's intent.
var LegacyProvisionedEmails = []string{"templates@system.jomform"}

// legacyUnscopedAccountSQL resolves the account a legacy provision would act on:
// the caller's own, still workspace-less, carrying at least one form, whose owner
// is a real user who is not one of the excluded system identities. A caller who
// already owns ANY workspace resolves to nothing, which is what makes the whole
// operation idempotent (and what stops a workspace the user created himself from
// being duplicated or having forms stolen into it).
const legacyUnscopedAccountSQL = `
	SELECT a.id
	FROM accounts a
	JOIN users u ON u.id = a.owner_user_id
	WHERE a.owner_user_id = $1
	  AND a.workspace_id IS NULL
	  AND NOT (u.email = ANY($2::text[]))
	  AND EXISTS (SELECT 1 FROM forms f WHERE f.account_id = a.id)
	  AND NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.owner_user_id = $1)
	ORDER BY a.created_at
	LIMIT 1`

// LegacyUnscopedAccount reports the account a legacy provision would act on, or
// "" when the rule does not apply to this owner. Exposed so a caller (the login
// path, an operator runbook) can tell "nothing to do" from "provisioned".
//
// ONLY pgx.ErrNoRows means "not applicable". Every other error is returned, and
// that distinction is load-bearing: this method's whole contract is "provision X
// for this user", and an implementation that reported a broken query as "nothing
// to do" would silently stop provisioning while every caller looked healthy — the
// same silent-failure class (an operation that does nothing and reports success)
// that this card exists to remove. Found by a bite proof: when the SQL was
// broken, the callers reported "not applicable" instead of an error.
func (s *WorkspaceStore) LegacyUnscopedAccount(ctx context.Context, ownerUserID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, legacyUnscopedAccountSQL,
		ownerUserID, LegacyProvisionedEmails).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// ProvisionLegacyWorkspace applies Wan's *has forms, no workspace* rule: it
// creates the workspace (slug from wantSlug, with a deterministic suffix when
// that slug is taken), links the owner's legacy account to it, moves the rows
// hanging off that account, and records the owner as the workspace's admin
// member (the 000025 convention: every workspace has an admin member).
//
// Returns (nil, nil) when the rule does not apply — the caller has no workspace-less
// content-carrying account, or already owns a workspace (re-running changes
// nothing), or is an excluded system identity.
//
// TRAPS this method exists to get right:
//   - trg_forms_sync_workspace_id (000045) fires only on INSERT / UPDATE OF
//     account_id on forms, so linking the ACCOUNT does not move its rows. Every
//     carrying table is synced explicitly here, as migration 000012 did.
//   - affiliates.workspace_id is NOT NULL (000047), so no affiliate row can be
//     sitting on an unscoped account — there is nothing to move for it.
//   - workspace_settings / workspace_email_settings are materialised lazily by
//     their own accessors (Settings / EmailSettings INSERT ... ON CONFLICT DO
//     NOTHING), and workspace_legal_pages / workspace_invites are per-workspace
//     content that simply does not exist yet. None need a row at provisioning.
func (s *WorkspaceStore) ProvisionLegacyWorkspace(ctx context.Context, ownerUserID, name, wantSlug string) (*Workspace, error) {
	sp := &Workspace{}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var accountID string
	if err := tx.QueryRow(ctx, legacyUnscopedAccountSQL, ownerUserID, LegacyProvisionedEmails).
		Scan(&accountID); err != nil {
		// Only a missing row means "not applicable" (the rule does not apply, or a
		// previous run already provisioned) — that is a no-op, not a failure. A
		// real error must propagate: reporting a broken query as "nothing to do"
		// would stop provisioning while every caller looked healthy. See
		// LegacyUnscopedAccount.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	// Slug allocation with a deterministic, terminating ladder. A slug collision
	// must never surface to a user who never chose a slug, so each attempt that
	// loses the unique race is retried on the next candidate. Each attempt runs
	// under its own SAVEPOINT: inside a transaction a failed statement aborts the
	// whole tx, so without the savepoint the second candidate could never be tried.
	candidates := legacySlugCandidates(wantSlug, ownerUserID)
	inserted := false
	for _, candidate := range candidates {
		attempt, berr := tx.Begin(ctx)
		if berr != nil {
			return nil, berr
		}
		err := attempt.QueryRow(ctx,
			`INSERT INTO workspaces (owner_user_id, name, slug) VALUES ($1,$2,$3)
			 RETURNING id, owner_user_id, name, slug`,
			ownerUserID, name, candidate).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
		if err == nil {
			if cerr := attempt.Commit(ctx); cerr != nil {
				return nil, cerr
			}
			inserted = true
			break
		}
		_ = attempt.Rollback(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			return nil, err
		}
	}
	if !inserted {
		return nil, errWorkspaceSlugExhausted
	}

	// Owner-checked link: the account must belong to the workspace's owner and
	// must still be unlinked (it was read under this tx, so a concurrent link
	// loses the race here rather than being overwritten).
	ct, err := tx.Exec(ctx,
		`UPDATE accounts SET workspace_id=$1, updated_at=now()
		 WHERE id=$2 AND owner_user_id=$3 AND workspace_id IS NULL`,
		sp.ID, accountID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() != 1 {
		return nil, errAccountNotLinkable
	}

	// Explicit sync of everything the account carries. forms is the load-bearing
	// one; the rest are synced so this method is correct for any account rather
	// than only for the shape prod happens to have today (all three are 0 rows
	// for the affected accounts, and the caller asserts that before/after).
	for _, stmt := range []string{
		`UPDATE forms SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE products SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE sales SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
		`UPDATE api_keys SET workspace_id=$1 WHERE account_id=$2 AND workspace_id IS NULL`,
	} {
		if _, err := tx.Exec(ctx, stmt, sp.ID, accountID); err != nil {
			return nil, err
		}
	}

	// The owner becomes an admin member, matching 000025_space_members' backfill
	// ("every existing space owner becomes an admin member"). WorkspaceStore.access
	// already treats an owner as admin, so this is for surface parity with every
	// other workspace rather than for authorisation.
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'admin')
		 ON CONFLICT (workspace_id, user_id) DO NOTHING`, sp.ID, ownerUserID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return sp, nil
}

// errWorkspaceSlugExhausted is returned when every deterministic slug candidate
// was already taken. Bounded rather than unbounded so a caller always terminates.
var errWorkspaceSlugExhausted = errors.New("slug workspace tidak dapat dijana")

// ProvisionLegacyWorkspaceForOwner is the entry point the LOGIN path uses: it
// resolves the owner's email itself and applies Wan's derivation rule (first 5
// characters of the email, sanitised), so no call site can forget the rule or
// invent a slug the API would reject.
//
// The slug is validated before it is used. If derivation somehow produced
// something ValidateWorkspaceSlug refuses this returns an error rather than
// provisioning a workspace whose own subdomain would 400 — a slug that the
// wildcard host cannot serve is worse than no workspace, because the user would
// then have a workspace whose form URLs are dead.
func (s *WorkspaceStore) ProvisionLegacyWorkspaceForOwner(ctx context.Context, ownerUserID string) (*Workspace, error) {
	accountID, err := s.LegacyUnscopedAccount(ctx, ownerUserID)
	if err != nil || accountID == "" {
		return nil, err
	}
	var email string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(email,'') FROM users WHERE id=$1`, ownerUserID).
		Scan(&email); err != nil {
		return nil, err
	}
	slug := SlugFromEmailLocalPart(email, ownerUserID)
	if err := ValidateWorkspaceSlug(slug); err != nil {
		return nil, fmt.Errorf("slug legacy tidak sah (%q): %w", slug, err)
	}
	name := email
	if at := strings.Index(name, "@"); at > 0 {
		name = name[:at]
	}
	if strings.TrimSpace(name) == "" {
		name = slug
	}
	return s.ProvisionLegacyWorkspace(ctx, ownerUserID, name, slug)
}

// legacySlugCandidates is the deterministic, bounded slug ladder: the wanted slug
// first, then "…-2" through "…-9", then a suffix derived from the owner's user id.
// Deterministic because the same starting slug yields the same sequence, so two
// users whose emails reduce to the same 5 characters get stable, distinct slugs
// (measured on prod: owner00@example.com and owner00+test@example.com
// both reduce to "owner"), and terminating because the sequence is finite and its
// last entry carries 8 characters of a uuid.
func legacySlugCandidates(wantSlug, ownerUserID string) []string {
	out := []string{wantSlug}
	for i := 2; i <= 9; i++ {
		out = append(out, fmt.Sprintf("%s-%d", wantSlug, i))
	}
	suffix := ownerUserID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix != "" {
		out = append(out, wantSlug+"-"+suffix)
	}
	return out
}

// WorkspaceAccess carries the account set + the caller's role for a workspace the
// user can access (owner or member). Role is one of RoleReadOnly/RoleManage/
// RoleAdmin (issue #162). Accounts holds every account linked to the workspace.
type WorkspaceAccess struct {
	WorkspaceID string
	Name        string
	Slug        string
	Role        string
	Accounts    []string
}

// AccessBySlug resolves the workspace's account ids + the caller's role when the
// user is the owner or a member of the workspace. ok=false when the workspace is
// unknown or the user has no access (issue #162 — members, not just owners).
func (s *WorkspaceStore) AccessBySlug(ctx context.Context, slug, userID string) (WorkspaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.slug = $1`, slug, userID)
}

// AccessByID resolves the workspace's account ids + the caller's role when the
// user is the owner or a member of the workspace.
func (s *WorkspaceStore) AccessByID(ctx context.Context, workspaceID, userID string) (WorkspaceAccess, bool, error) {
	return s.access(ctx, `WHERE sp.id = $1`, workspaceID, userID)
}

// access is the shared owner-or-member workspace resolution. The owner is always
// treated as an admin (the migration backfills owners as admin members, and
// new workspaces add the owner as a member at creation), so a workspace created
// before the backfill still resolves correctly.
func (s *WorkspaceStore) access(ctx context.Context, where string, key, userID string) (WorkspaceAccess, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, a.id, COALESCE(m.role, '')
		FROM workspaces sp
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $2
		`+where, key, userID)
	if err != nil {
		return WorkspaceAccess{}, false, err
	}
	defer rows.Close()
	var out WorkspaceAccess
	found := false
	for rows.Next() {
		var owner, accID, role string
		if err := rows.Scan(&out.WorkspaceID, &out.Name, &out.Slug, &owner, &accID, &role); err != nil {
			return WorkspaceAccess{}, false, err
		}
		found = true
		if owner == userID {
			role = RoleAdmin
		}
		if role == "" {
			// Workspace exists but the user is neither owner nor a member.
			return WorkspaceAccess{}, false, nil
		}
		out.Role = role
		if accID != "" {
			out.Accounts = append(out.Accounts, accID)
		}
	}
	if err := rows.Err(); err != nil {
		return WorkspaceAccess{}, false, err
	}
	return out, found, nil
}

// WorkspaceBlocker is one category of content that prevents a workspace from
// being deleted, with the count that blocked it. The delete path returns the
// non-zero categories so the caller can list exactly what must be cleared
// instead of a bare "not empty".
type WorkspaceBlocker struct {
	// Kind is the machine-readable category: "forms", "products", "sales",
	// "domains", "webhooks", "api_keys" or "subscriptions".
	Kind string `json:"kind"`
	// Count is how many rows of that kind the workspace still owns.
	Count int64 `json:"count"`
	// Label is the human-readable category name shown in the refusal.
	Label string `json:"label"`
}

// WorkspaceDeleteError is returned by Delete when the workspace cannot be
// removed. Two shapes, both actionable:
//
//	reason "last_workspace" — the owner would be left with zero workspaces. A
//	   user must always have at least one, so this is the guard Wan asked for: it
//	   protects the workspace they started with without hard-coding which one
//	   that is.
//	reason "not_empty"     — the workspace still owns content. Blockers lists
//	   each category and its count, so the caller can clear them (trash + purge a
//	   form, archive a product, …) instead of guessing.
type WorkspaceDeleteError struct {
	Reason   string
	Blockers []WorkspaceBlocker
}

func (e *WorkspaceDeleteError) Error() string {
	if e.Reason == "last_workspace" {
		return "sekurang-kurangnya satu workspace diperlukan"
	}
	return "workspace tidak kosong"
}

// ErrWorkspaceNotEmpty reports whether err is the "still owns content" refusal,
// and returns the blocking categories.
func ErrWorkspaceNotEmpty(err error) ([]WorkspaceBlocker, bool) {
	var de *WorkspaceDeleteError
	if errors.As(err, &de) && de.Reason == "not_empty" {
		return de.Blockers, true
	}
	return nil, false
}

// IsLastWorkspaceError reports whether err is the "you would be left with no
// workspace" refusal.
func IsLastWorkspaceError(err error) bool {
	var de *WorkspaceDeleteError
	return errors.As(err, &de) && de.Reason == "last_workspace"
}

// emptyCheckSQL is the definition of "an empty workspace", in ONE place so the
// refusal message and the delete path can never disagree about it. A workspace
// is empty when ALL of these are zero:
//
//	forms         — INCLUDING trashed ones. A form in the trash is still a row
//	                in `forms`; it must be purged (trash → delete permanently)
//	                before the workspace can go. This is why the refusal message
//	                names the trash path.
//	products      — LIVE products only (active). The card asks that products
//	                block, and they do — but ONLY the ones the merchant is still
//	                selling. There is deliberately no product DELETE in the
//	                store (archiving is "dulang tanpa padam data"), so counting
//	                archived products too would make this guard UNSATISFIABLE and
//	                lock the merchant into a workspace they can never empty. An
//	                archived product is already withdrawn from sale; it is
//	                removed with the workspace below, exactly like the account.
//	                (A subscription is the one thing that could keep an archived
//	                product alive — and every subscription, of any status,
//	                blocks above, so that interaction is refused first.)
//	sales         — the trading history. Never destroyed by a workspace delete.
//	subscriptions — EVERY status, not just active. The FK is
//	                `subscriptions.account_id → accounts ON DELETE RESTRICT`
//	                (and `product_id → products RESTRICT`), so a CANCELLED
//	                subscription still blocks the account and product deletes.
//	                Counting only active/past_due would let the guard pass and
//	                then fail the DELETE with SQLSTATE 23503 — a raw 500 on a
//	                path the merchant was told was clear. Billing history joins
//	                sales as something this call never destroys.
//	domains, webhooks, api_keys — merchant-facing configuration. Domains and
//	                webhooks CASCADE at the DB level and live API keys CASCADE
//	                too, so these are a POLICY choice (the card asks for them):
//	                the merchant is told to clear configuration rather than have
//	                it vanish silently. Revoked keys are not counted — they are
//	                already dead — but they are still deleted below, because a
//	                revoked row keeps its workspace_id and that FK RESTRICTs.
//
// The account holding CHIP credentials is removed WITH the workspace, never
// shared, so it is not a blocker — it is the thing that must go.
//
// WHY THIS IS ONE ROW OF FILTERED COUNTS, NOT A UNION OF PER-CATEGORY ROWS.
// The repo has a single definition of "the merged record read" (sales +
// submissions), and a guard (httpapi TestMergedReadGoesThroughTheOneBuilder)
// refuses an explicit union of the two tables outside dbstore/records.go — a
// union in a second file is how that record set grew a second definition (the
// guard matches the SQL keyword, comments included, so this note names it only
// in prose). This query is not a record read; it is a per-category blocker
// census. Returning ONE row of seven counters keeps the shape impossible to
// mistake for the merged read, counts every category in a single scan of
// `forms`, and leaves each category's filter exactly as it was.
const emptyCheckSQL = `
	SELECT
	  COUNT(*) FILTER (WHERE TRUE) AS forms,
	  (SELECT COUNT(*) FROM products WHERE account_id = ANY($1) AND active) AS products,
	  (SELECT COUNT(*) FROM sales WHERE account_id = ANY($1)) AS sales,
	  (SELECT COUNT(*) FROM subscriptions WHERE account_id = ANY($1)) AS subscriptions,
	  (SELECT COUNT(*) FROM account_domains WHERE account_id = ANY($1)) AS domains,
	  (SELECT COUNT(*) FROM outbound_webhooks WHERE account_id = ANY($1) AND active) AS webhooks,
	  (SELECT COUNT(*) FROM api_keys
	    WHERE account_id = ANY($1) AND revoked_at IS NULL
	      AND id <> COALESCE($2::uuid, '00000000-0000-0000-0000-000000000000'::uuid)) AS api_keys
	FROM forms WHERE account_id = ANY($1)`

// emptyCheckArgs builds the parameter list for emptyCheckSQL: the account ids,
// and the API key (if any) that is EXEMPT from the live-key blocker.
//
// WHY THE EXEMPTION EXISTS. The live-key blocker stops a workspace being
// destroyed while credentials still point at it. But the delete revokes every
// key on the account anyway, so no live key can survive it — and without the
// exemption the check is self-defeating: whoever deletes the workspace is
// authenticated by a key, that key is live, so every key-authenticated caller
// (i.e. every MCP caller) would be refused. That is a lock, not a guard. Exactly
// ONE key is ignored, and only when it is the very key making the call.
func emptyCheckArgs(accIDs []string, exemptKeyID string) []any {
	return []any{accIDs, nullableKeyID(exemptKeyID)}
}

// nullableKeyID turns an empty key id into a nil, so the COALESCE in
// emptyCheckSQL handles "no key on the call" (a session caller) without a
// second statement.
func nullableKeyID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// blockerLabels names each blocker for the refusal message.
var blockerLabels = map[string]string{
	"forms":         "borang (termasuk yang dalam tong sampah)",
	"products":      "produk",
	"sales":         "jualan",
	"subscriptions": "langganan (sejarah bil)",
	"domains":       "domain tersuai",
	"webhooks":      "webhook",
	"api_keys":      "kunci API",
}

// CountEmpty reports what still blocks the workspace from being deleted — the
// non-zero categories only, ordered as in emptyCheckSQL. An empty slice means
// the workspace may be deleted.
//
// This is a READ, deliberately separate from Delete: the handler calls it first
// so a refusal can list every blocking category with its count, instead of
// discovering the first obstacle and stopping.
func (s *WorkspaceStore) CountEmpty(ctx context.Context, workspaceID, exemptKeyID string) (int64, []WorkspaceBlocker, error) {
	accs, err := s.accountIDs(ctx, workspaceID)
	if err != nil {
		return 0, nil, err
	}
	var forms, products, sales, subs, domains, webhooks, keys int64
	err = s.pool.QueryRow(ctx, emptyCheckSQL, emptyCheckArgs(accs, exemptKeyID)...).
		Scan(&forms, &products, &sales, &subs, &domains, &webhooks, &keys)
	if err != nil {
		return 0, nil, err
	}
	return int64(len(accs)), blockersFrom(forms, products, sales, subs, domains, webhooks, keys), nil
}

// blockersFrom turns the seven counters into the non-zero blocker list, in the
// order emptyCheckSQL returns them. ONE place, so the delete path and the read
// path can never disagree about which categories blocked or how they are named.
func blockersFrom(forms, products, sales, subs, domains, webhooks, keys int64) []WorkspaceBlocker {
	var out []WorkspaceBlocker
	for _, c := range []struct {
		kind  string
		count int64
	}{
		{"forms", forms}, {"products", products}, {"sales", sales},
		{"subscriptions", subs}, {"domains", domains},
		{"webhooks", webhooks}, {"api_keys", keys},
	} {
		if c.count > 0 {
			out = append(out, WorkspaceBlocker{Kind: c.kind, Count: c.count, Label: blockerLabels[c.kind]})
		}
	}
	return out
}

// accountIDs returns every account id linked to a workspace.
func (s *WorkspaceStore) accountIDs(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Delete removes a workspace and everything that belongs to it, refusing when
// either guard says no.
//
// exemptKeyID is the id of the API key authenticating the call, or "" for a
// session caller. See emptyCheckArgs for why exactly one live key may be
// ignored by the emptiness guard.
//
// WHY THIS IS NOT ONE `DELETE FROM workspaces`. The foreign keys on the live
// schema make that statement FAIL (SQLSTATE 23503) on any workspace holding an
// account — accounts.workspace_id, api_keys.workspace_id, forms.workspace_id,
// products.workspace_id and sales.workspace_id are all NO ACTION (RESTRICT),
// while only the seven child tables (workspace_settings, workspace_members,
// workspace_invites, workspace_legal_pages, workspace_email_settings,
// affiliates) cascade. Every workspace MUST own an account (migration 000024),
// so the naive delete works on literally nothing.
//
// The order below is what makes the cascade reach the workspace: the RESTRICT
// rows are removed deepest-first (sales → their subscription/charge children
// cascade; then forms, products, api_keys; then the accounts), and only then the
// workspaces row, whose own children fall away by CASCADE.
//
// The accounts go WITH the workspace: an account holds CHIP credentials, and
// the repo invariant is that CHIP is never shared between workspaces, so an
// account orphaned by a workspace delete would be a credential nobody owns.
//
// Guards (both checked inside the transaction, so nothing is deleted when
// either refuses):
//
//	last_workspace — the owner would be left with zero workspaces. The rule is
//	   "a user always has at least one", expressed as a COUNT rather than by
//	   pinning the workspace they started with, so it protects whichever
//	   workspace is last without a "is this the original one" flag.
//	not_empty      — CountEmpty reports blockers; the workspace is untouched.
//
// Returns the number of rows removed per table for the audit trail.
func (s *WorkspaceStore) Delete(ctx context.Context, workspaceID, ownerUserID, exemptKeyID string) (map[string]int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the workspace row: two concurrent deletes of the same workspace
	// would otherwise both pass the guards and the second would fail late with a
	// raw FK error.
	var owner string
	if err := tx.QueryRow(ctx,
		`SELECT owner_user_id FROM workspaces WHERE id=$1 FOR UPDATE`,
		workspaceID).Scan(&owner); isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	} else if err != nil {
		return nil, err
	}

	// Guard 1 — at least one workspace must remain. Counted on the owner's
	// membership (owned OR member), which is exactly what the switcher lists, so
	// a user who is a member of someone else's workspace still has one to land
	// on and may delete their own last one.
	var remaining int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces sp
		 WHERE sp.id <> $1
		   AND (sp.owner_user_id = $2
		        OR EXISTS (SELECT 1 FROM workspace_members m
		                    WHERE m.workspace_id = sp.id AND m.user_id = $2))`,
		workspaceID, ownerUserID).Scan(&remaining); err != nil {
		return nil, err
	}
	if remaining == 0 {
		return nil, &WorkspaceDeleteError{Reason: "last_workspace"}
	}

	// Guard 2 — the workspace must be empty, using the ONE definition.
	var accIDs []string
	{
		rows, qerr := tx.Query(ctx, `SELECT id FROM accounts WHERE workspace_id=$1`, workspaceID)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			accIDs = append(accIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(accIDs) > 0 {
		var forms, products, sales, subs, domains, webhooks, keys int64
		qerr := tx.QueryRow(ctx, emptyCheckSQL, emptyCheckArgs(accIDs, exemptKeyID)...).
			Scan(&forms, &products, &sales, &subs, &domains, &webhooks, &keys)
		if qerr != nil {
			return nil, qerr
		}
		if blockers := blockersFrom(forms, products, sales, subs, domains, webhooks, keys); len(blockers) > 0 {
			return nil, &WorkspaceDeleteError{Reason: "not_empty", Blockers: blockers}
		}
	}

	deleted := map[string]int64{}
	// Step 1: trading history first — sales/subscriptions RESTRICT their
	// account, and subscription_charges CASCADE off their subscription. Only
	// counts of zero can reach here (guard 2), but the statement is written as
	// a delete so a race that inserts a sale between the check and here cannot
	// leave the account behind.
	for _, step := range []struct {
		table string
		sql   string
	}{
		{"sales", `DELETE FROM sales WHERE account_id = ANY($1)`},
		{"forms", `DELETE FROM forms WHERE account_id = ANY($1)`},
		{"products", `DELETE FROM products WHERE account_id = ANY($1)`},
		{"api_keys", `DELETE FROM api_keys WHERE account_id = ANY($1)`},
	} {
		tag, derr := tx.Exec(ctx, step.sql, accIDs)
		if derr != nil {
			return nil, derr
		}
		deleted[step.table] = tag.RowsAffected()
	}
	// Step 2: the accounts (they hold the CHIP credentials; never orphaned).
	if len(accIDs) > 0 {
		tag, derr := tx.Exec(ctx, `DELETE FROM accounts WHERE workspace_id=$1`, workspaceID)
		if derr != nil {
			return nil, derr
		}
		deleted["accounts"] = tag.RowsAffected()
	}
	// Step 3: the workspace itself. workspace_settings / workspace_members /
	// workspace_invites / workspace_legal_pages / workspace_email_settings /
	// affiliates go with it by CASCADE. users.default_workspace_id is SET NULL
	// by its FK, so no dangling default is left behind.
	tag, derr := tx.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, workspaceID)
	if derr != nil {
		return nil, derr
	}
	deleted["workspaces"] = tag.RowsAffected()
	if deleted["workspaces"] == 0 {
		return nil, ErrWorkspaceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return deleted, nil
}

// ErrWorkspaceNotFound means the workspace id named no row.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// ByMember lists workspaces the user owns OR is a member of, with the caller's
// role in each (issue #162). Used by the workspace switcher so members see the
// workspaces they belong to, not just the ones they own.
func (s *WorkspaceStore) ByMember(ctx context.Context, userID string) ([]WorkspaceAccess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(m.role, ''), a.id
		FROM workspaces sp
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $1
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		WHERE sp.owner_user_id = $1 OR m.user_id = $1
		ORDER BY sp.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceAccess
	seen := map[string]int{}
	for rows.Next() {
		var a WorkspaceAccess
		var owner, role, accID string
		if err := rows.Scan(&a.WorkspaceID, &a.Name, &a.Slug, &owner, &role, &accID); err != nil {
			return nil, err
		}
		if owner == userID {
			role = RoleAdmin
		}
		a.Role = role
		if i, ok := seen[a.WorkspaceID]; ok {
			if accID != "" {
				out[i].Accounts = append(out[i].Accounts, accID)
			}
			continue
		}
		seen[a.WorkspaceID] = len(out)
		if accID != "" {
			a.Accounts = append(a.Accounts, accID)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LandingPage is the customizable workspace landing-page design (issue #360). The
// storefront root ({workspace}.jomform.com and custom domains) renders a styled
// landing page from this config when Enabled. When no config is set (or
// Enabled=false) handleWorkspaceRoot falls back to the auto-generated bare listing.
type LandingPage struct {
	Enabled bool `json:"enabled"`
	// Hero is the main heading / hero title.
	Hero string `json:"hero"`
	// Subtitle is the hero subheading.
	Subtitle string `json:"subtitle"`
	// Body is the landing-page body text (plain paragraphs).
	Body string `json:"body"`
	// ImageURL is an optional banner/hero image.
	ImageURL string `json:"image_url"`
	// PrimaryColor is the theme accent color (hex, e.g. "#7c3aed").
	PrimaryColor string `json:"primary_color"`
	// BackgroundColor is the page background (hex). Empty = default.
	BackgroundColor string `json:"background_color"`
	// LogoURL is an optional brand logo image.
	LogoURL string `json:"logo_url"`
	// CTAs are buttons linking to published forms (label + form slug).
	CTAs []LandingCTA `json:"ctas"`
	// Forms are published forms embedded inline on the landing page (issue
	// #362). Each embed renders the full JomForm form (fields + validation +
	// submission) same-origin via an iframe to /{slug}, so RSVP-style forms
	// submit straight into the form's submissions/sales exactly as a direct
	// visit would. Stored in the landing_page_json blob.
	Forms []LandingEmbed `json:"forms"`
	// Issue #438 shipped a per-workspace restaurant menu here (LandingPage.Menu,
	// rendered at the workspace ROOT). Issue #497 moved the menu onto the FORM
	// (forms.config.menu) because the workspace-level shape hijacked the whole
	// storefront, sold every account product, and allowed only one menu per
	// workspace. The field is retained ONLY so a stray/legacy
	// landing_page_json.menu is parsed and safely ignored — nothing reads it.
	//
	// Deprecated: use MenuFromConfig(form.Config) / WithMenu instead.
	LegacyMenu *MenuPage `json:"menu,omitempty"`
}

// MenuPage, MenuCategory, IsBestSeller, BestSellerProductIDs and the
// config helpers now live in form_menu.go — issue #497 moved the restaurant
// menu onto the FORM. See MenuFromConfig / WithMenu.

// LandingCTA is a landing-page call-to-action button linking to a public form.
type LandingCTA struct {
	Label string `json:"label"`
	// FormSlug is the published form's slug within the workspace; rendered as a
	// relative link that resolves on the workspace subdomain / custom domain.
	FormSlug string `json:"form_slug"`
}

// LandingEmbed is a published form rendered inline on the landing page (issue
// #362). FormSlug must resolve to a published form in the same workspace; the
// server only renders embed blocks whose slug matches a published form, so a
// draft/private/unknown slug is never shown.
type LandingEmbed struct {
	// Label is an optional heading shown above the embedded form.
	Label string `json:"label"`
	// FormSlug is the published form to embed (e.g. "rsvp").
	FormSlug string `json:"form_slug"`
}

// WorkspaceSettings is the storefront configuration for one workspace.
type WorkspaceSettings struct {
	WorkspaceID string `json:"workspace_id"`
	FooterText  string `json:"footer_text"`
	CompanyReg  string `json:"company_reg"`
	PrivacyURL  string `json:"privacy_url"`
	TermsURL    string `json:"terms_url"`
	// Issue #183: optional public-form footer fields.
	RefundURL      string `json:"refund_url"`
	ContactPhone   string `json:"contact_phone"`
	ContactAddress string `json:"contact_address"`
	BusinessName   string `json:"business_name"`
	MenuJSON       string `json:"menu_json"`
	DefaultLang    string `json:"default_lang"`
	// Currency is the workspace's settlement/display currency (issue #185).
	// Default MYR. The workspace's account(s) inherit it for CHIP purchases; new
	// amounts are stored in this currency's minor units. Historical sales keep
	// their original currency (never auto-converted).
	Currency string `json:"currency"`
	// Indexing is the per-workspace SEO toggle (issue #276). When true (default)
	// the workspace's public storefront + forms are eligible for search-engine /
	// AI indexing on the jomform.com apex and on custom domains. When false
	// the workspace opts out of indexing entirely. Public forms on jomform.com
	// subdomains are always noindex regardless of this flag (privacy).
	Indexing bool `json:"indexing"`
	// LandingPage is the customizable landing-page config (issue #360).
	// Stored as landing_page_json on workspace_settings. When nil/empty the
	// storefront root falls back to the auto-generated bare listing.
	LandingPage *LandingPage `json:"landing_page,omitempty"`
	// SoundEnabled is the per-workspace realtime notification sound toggle
	// (issue #447). When true, the merchant's dashboard plays an audio beep
	// when a new sale/response arrives over the SSE stream. Default false
	// (muted) so no merchant is surprised by sound.
	SoundEnabled bool `json:"sound_enabled"`
	// SpamJudgeEnabled is the per-workspace AI spam-judge toggle.
	// When true (the default) the background judge (issue #245) is enqueued for
	// every public submission/sale. When false NO judge job is enqueued at all
	// (the gate is on the enqueue, not in the worker, so no AI work is spent),
	// and is_spam is therefore never set — the workspace's /spam queue stays
	// empty. True by default so enabling the feature changes no existing
	// workspace's behaviour.
	SpamJudgeEnabled bool `json:"spam_judge_enabled"`
}

// WorkspaceLang returns the storefront default language code, or a fallback when
// unset. Callers must use this so a not-yet-migrated or legacy row resolves
// to 'en' (maintainer decision 2026-09-01), never "".
func (st *WorkspaceSettings) WorkspaceLang() string {
	switch st.DefaultLang {
	case "en", "ms", "zh", "ta", "ar":
		return st.DefaultLang
	default:
		return "en"
	}
}

// WorkspaceCurrency returns the workspace's currency code, or MYR when unset/unknown.
// Callers must use this so a not-yet-migrated or legacy row resolves to MYR
// (issue #185), never "".
func (st *WorkspaceSettings) WorkspaceCurrency() string {
	if IsSupportedCurrency(st.Currency) {
		return st.Currency
	}
	return "MYR"
}

// Settings reads (creating default row when missing) the storefront config.
// The fallback literal mirrors the migration defaults so a workspace whose row
// does not exist yet behaves exactly like a migrated one: indexing on and the
// AI spam judge on (issue #276 / this change).
func (s *WorkspaceStore) Settings(ctx context.Context, workspaceID string) (*WorkspaceSettings, error) {
	var lpJSON string
	st := &WorkspaceSettings{WorkspaceID: workspaceID, MenuJSON: "[]", DefaultLang: "en", Currency: "MYR", Indexing: true, SpamJudgeEnabled: true}
	err := s.pool.QueryRow(ctx,
		`SELECT footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json::text, default_lang, currency, indexing, COALESCE(landing_page_json::text, '{}'), sound_enabled, spam_judge_enabled
		   FROM workspace_settings WHERE workspace_id=$1`,
		workspaceID).Scan(&st.FooterText, &st.CompanyReg, &st.PrivacyURL, &st.TermsURL, &st.RefundURL, &st.ContactPhone, &st.ContactAddress, &st.BusinessName, &st.MenuJSON, &st.DefaultLang, &st.Currency, &st.Indexing, &lpJSON, &st.SoundEnabled, &st.SpamJudgeEnabled)
	if err != nil {
		// default row
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO workspace_settings (workspace_id) VALUES ($1) ON CONFLICT DO NOTHING`, workspaceID)
		if ierr != nil {
			return nil, ierr
		}
		lpJSON = "{}"
	}
	// Parse the landing-page config; a missing/bad payload is treated as "no
	// landing page" so the storefront keeps falling back to the bare listing.
	if lpJSON != "" && lpJSON != "{}" {
		var lp LandingPage
		if jerr := json.Unmarshal([]byte(lpJSON), &lp); jerr == nil {
			st.LandingPage = &lp
		}
	}
	return st, nil
}

// SaveSettings upserts the storefront config.
func (s *WorkspaceStore) SaveSettings(ctx context.Context, st *WorkspaceSettings) error {
	var lpJSON string
	if st.LandingPage != nil {
		b, err := json.Marshal(st.LandingPage)
		if err != nil {
			return err
		}
		lpJSON = string(b)
	} else {
		lpJSON = "{}"
	}
	if lpJSON == "" || lpJSON == "null" {
		lpJSON = "{}"
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspace_settings (workspace_id, footer_text, company_reg, privacy_url, terms_url, refund_url, contact_phone, contact_address, business_name, menu_json, default_lang, currency, indexing, landing_page_json, sound_enabled, spam_judge_enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb,$15,$16)
		 ON CONFLICT (workspace_id) DO UPDATE SET
		   footer_text=EXCLUDED.footer_text, company_reg=EXCLUDED.company_reg,
		   privacy_url=EXCLUDED.privacy_url, terms_url=EXCLUDED.terms_url,
		   refund_url=EXCLUDED.refund_url, contact_phone=EXCLUDED.contact_phone,
		   contact_address=EXCLUDED.contact_address, business_name=EXCLUDED.business_name,
		   menu_json=EXCLUDED.menu_json,
		   default_lang=EXCLUDED.default_lang, currency=EXCLUDED.currency,
		   indexing=EXCLUDED.indexing,
		   landing_page_json=EXCLUDED.landing_page_json,
		   sound_enabled=EXCLUDED.sound_enabled,
		   spam_judge_enabled=EXCLUDED.spam_judge_enabled`,
		st.WorkspaceID, st.FooterText, st.CompanyReg, st.PrivacyURL, st.TermsURL, st.RefundURL, st.ContactPhone, st.ContactAddress, st.BusinessName, st.MenuJSON, st.WorkspaceLang(), st.WorkspaceCurrency(), st.Indexing, lpJSON, st.SoundEnabled, st.SpamJudgeEnabled)
	return err
}

// WorkspaceCurrencyByAccount resolves the currency of the workspace that owns the
// given account ("" when the account has no linked workspace). Used by the
// payments path to pick the CHIP purchase currency from the workspace (issue
// #185) instead of the account-level default.
func (s *WorkspaceStore) WorkspaceCurrencyByAccount(ctx context.Context, accountID string) (string, error) {
	var cur string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.currency, 'MYR')
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&cur)
	if err != nil {
		return "", err
	}
	return cur, nil
}

// IndexingByAccount resolves whether the workspace owning the given account has
// indexing enabled (issue #276). Defaults to true when the account has no
// linked workspace (legacy) or the setting is unset. Used by the public form
// renderer to decide whether a custom-domain form may be indexed.
func (s *WorkspaceStore) IndexingByAccount(ctx context.Context, accountID string) (bool, error) {
	var idx bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.indexing, TRUE)
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&idx)
	if err != nil {
		return true, err
	}
	return idx, nil
}

// SpamJudgeEnabledByAccount resolves whether the workspace owning the given
// account has the AI spam judge enabled. Defaults to true when the
// account has no linked workspace (legacy) or the setting is unset — the same
// "on unless explicitly turned off" contract the migration default gives a
// migrated row. Used by the public submit path to decide whether to enqueue a
// spam-judge job AT ALL (the gate is on the enqueue, so a disabled judge costs
// no AI work).
//
// The bool returned alongside the error is the FAIL DIRECTION, and it is true
// on error: the judge exists to remove work, so a database hiccup must not
// silently stop spam protection for every workspace, and running the judge is
// the pre-existing behaviour — an error therefore cannot change behaviour. The
// caller logs the error; it must not fail the submission on it.
func (s *WorkspaceStore) SpamJudgeEnabledByAccount(ctx context.Context, accountID string) (bool, error) {
	var on bool
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(ss.spam_judge_enabled, TRUE)
		   FROM accounts a
		   LEFT JOIN workspace_settings ss ON ss.workspace_id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&on)
	if err != nil {
		return true, err
	}
	return on, nil
}

// IndexedWorkspaceURLs returns the custom-domain URLs of workspaces that have indexing
// enabled (issue #276). Only verified custom domains count; the jomform.com
// apex is handled by the web app. Used to build sitemap.xml.
func (s *WorkspaceStore) IndexedWorkspaceURLs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ad.domain
		   FROM workspace_settings ss
		   JOIN accounts a ON a.workspace_id = ss.workspace_id
		   JOIN account_domains ad ON ad.account_id = a.id
		  WHERE ss.indexing = TRUE AND ad.verified = TRUE
		  ORDER BY ad.domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IDBySlug resolves a workspace id from its slug ("" when unknown).
func (s *WorkspaceStore) IDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug=$1`, slug).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// WorkspaceIDByAccount resolves the workspace id a form's account belongs to ("" when
// the account has no linked workspace). Rendering falls back to 'en' in that case.
func (s *WorkspaceStore) WorkspaceIDByAccount(ctx context.Context, accountID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT a.workspace_id FROM accounts a WHERE a.id=$1`, accountID).Scan(&id)
	if err != nil {
		return "", err
	}
	return id, nil
}

// RoleByAccount returns the role a user holds in the workspace that owns the given
// account ("" when the account has no workspace or the user is not a member).
// The account owner is always treated as an admin (issue #162).
func (s *WorkspaceStore) RoleByAccount(ctx context.Context, accountID, userID string) (string, error) {
	var owner, role string
	err := s.pool.QueryRow(ctx, `
		SELECT a.owner_user_id, COALESCE(m.role, '')
		FROM accounts a
		LEFT JOIN workspace_members m ON m.workspace_id = a.workspace_id AND m.user_id = $2
		WHERE a.id = $1`, accountID, userID).Scan(&owner, &role)
	if err != nil {
		return "", err
	}
	if owner == userID {
		return RoleAdmin, nil
	}
	return role, nil
}

// PublicHostSlugByAccount resolves the subdomain a form's account is publicly
// reachable under, for building {slug}.jomform.com/{form-slug} URLs.
//
// It resolves the way the PUBLIC REQUEST PATH resolves, which is the invariant
// that matters: form serving (FormStore.ByWorkspaceSlug, reached from
// accountSubdomain + handleWorkspaceRootForm) accepts the WORKSPACE slug, and
// nothing else. A host that names no workspace resolves nothing, and the
// caller 404s — the same answer accountSubdomain already gives for the apex and
// for www/app/api/mail/s3.
//
// This used to fall back to the account slug (accounts.slug) when no workspace
// was linked, which papered over the real defect: AccountStore.Create set no
// workspace_id, so every signup-created account had a NULL workspace, was
// reachable only at its synthetic account slug, and the URL builder answered
// "". The fix is not a second host — it is that a workspace is served on its
// workspace subdomain and nothing else, so there is exactly one host per
// workspace. The workspace comes from the user's ask-first create
// (SoleEmptyUnscopedAccount + CreateForAccount), NOT from signup: a signup
// creates an account and no workspace (#718). It returns "" only while that
// account still has no linked workspace — which is the shape a fresh signup is
// deliberately in, and which the workspace-picker prompt resolves.
//
// NOT for legal-page hosts ({slug}.jomform.com/privacy|terms|refund): those are
// resolved by workspaces.IDBySlug alone (handlePublicLegalPage), so a legal URL
// must keep using WorkspaceSlugByAccount — see rewriteHostedLegalURL, which
// leaves a stored legal URL untouched when no workspace slug resolves rather
// than rewriting it to a host that would 404.
func (s *WorkspaceStore) PublicHostSlugByAccount(ctx context.Context, accountID string) (string, error) {
	return s.WorkspaceSlugByAccount(ctx, accountID)
}

// WorkspaceSlugByAccount resolves the workspace slug a form's account belongs to
// ("", and pgx.ErrNoRows, when the account has no linked workspace). Used to
// build the canonical workspace-scoped public URL {workspace}.jomform.com/{slug}
// (issue #77) and, by the legal-page rewriter, the {workspace}.jomform.com host
// that handlePublicLegalPage resolves through workspaces.IDBySlug.
//
// There is deliberately no account-slug fallback: a workspace is served on its
// workspace subdomain and nothing else (see PublicHostSlugByAccount).
func (s *WorkspaceStore) WorkspaceSlugByAccount(ctx context.Context, accountID string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx,
		`SELECT sp.slug FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id
		  WHERE a.id=$1`, accountID).Scan(&slug)
	if err != nil {
		return "", err
	}
	return slug, nil
}
