// Workspace: multi-tenant storefront layer. Each user may own many workspaces;
// forms, products, sales and settings hang off a workspace.
package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

	// DeletedAt is set only by the trash reads (ListTrashed / TrashedByID). The
	// live reads never populate it: a nil value is how a caller tells a live
	// workspace from a trashed one without a second query. Same convention as
	// Form.DeletedAt.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ── The ONE workspace-name rule ────────────────────────────────────────────────
//
// WorkspaceNameMaxChars is the cap on a workspace's display name. It is counted
// in CHARACTERS (utf8.RuneCountInString), which is the unit the dashboard's name
// field counts: `maxLength={120}` on an <input> limits UTF-16 code units, and
// for every character outside a surrogate pair (i.e. everything a merchant
// types, including accented Latin, CJK, Tamil and Arabic) one character is one
// code unit. Counting BYTES here — as this check used to — made the server
// stricter than the field the user was typing into: 120 accented characters or
// 45 CJK characters passed the field and were refused by the API.
const WorkspaceNameMaxChars = 120

// ErrWorkspaceNameLength is the refusal for a name over the cap. A SENTINEL, not
// a fresh error, because a fresh value reaches no arm of the transports' error
// switches: it fell through to the default and the merchant got
// `500 {"error":"internal error"}` for a name that is simply too long. It is
// returned as a stated 400 by REST and as a readable tool error by MCP.
var ErrWorkspaceNameLength = errors.New("workspace name maximum 120 characters")

// ValidateWorkspaceName trims a caller-supplied workspace name and reports
// whether it is within the cap. It returns the NORMALISED (trimmed) name so a
// caller writes exactly what was validated.
//
// It is the ONE name rule: the two create paths (REST and MCP) and the rename
// path all call it, so create and rename cannot disagree about what a name is —
// the same shape as ValidateWorkspaceSlug (workspace_slug.go). An empty name is
// NOT refused here: on rename an omitted/empty name means "keep the current
// one", and on create the transports state the "name dan slug wajib"
// requirement themselves.
func ValidateWorkspaceName(name string) (string, error) {
	n := strings.TrimSpace(name)
	if utf8.RuneCountInString(n) > WorkspaceNameMaxChars {
		return "", ErrWorkspaceNameLength
	}
	return n, nil
}

// ErrWorkspaceSlugTaken is the ONE refusal for "another LIVE workspace already
// answers on that slug". Both sides of the merge declared it separately for two
// sightings of that one condition; they are kept as ONE sentinel because they are
// one rule, and because the transports' switches key on the identity:
//
//   - rename/create: the requested slug is already in use — either LIVE (another
//     workspace serves it) or RESERVED (it is a former slug of another
//     workspace, held in workspace_slug_aliases so the addresses already in the
//     wild keep working).
//   - restore: the slug was re-claimed while this workspace sat in the bin. The
//     unique index covers LIVE rows only, so binning releases the name and
//     another workspace may take it; clearing deleted_at would then violate the
//     index. Nothing is renamed behind the owner's back.
//
// Both are a 409 on the wire, and neither ever overwrites the other side.
//
// The two are distinct on the MESSAGE because the remedy differs: a rename wants
// a different slug, a restore wants the holder renamed first. They are separated
// by the CALLING TRANSPORT (the restore transport knows it called Restore, and
// the MCP restore tool knows it too) rather than by a second sentinel — a
// second exported error would have to be added to every switch that answers this
// one, and the two are the same condition seen from two verbs.
var ErrWorkspaceSlugTaken = errors.New("slug already in use")

// WorkspaceStore wraps workspaces.
type WorkspaceStore struct{ pool *pgxpool.Pool }

func NewWorkspaceStore(pool *pgxpool.Pool) *WorkspaceStore { return &WorkspaceStore{pool: pool} }

// workspaceSelect builds every non-trash workspace read: trashed workspaces are
// invisible everywhere except the explicit trash query. deleted_at is part of
// the column list because every workspace read scans into a Workspace, and the
// migration/scan-drift guard pins a select's column count to that struct's
// field count — so Workspace.DeletedAt has to be selectable here. It is always
// NULL on these reads (the WHERE filters to live rows), which is exactly how a
// caller tells a live workspace from a trashed one.
//
// Same shape as formSelect, on purpose: the two trashable objects in this repo
// must not have two different ways of hiding a trashed row (issue: workspace trash).
const workspaceSelect = `SELECT id, owner_user_id, name, slug, deleted_at FROM workspaces WHERE deleted_at IS NULL`

// trashedWorkspaceSelect builds the two trash reads (ListTrashed / TrashedByID).
//
// It is a SEPARATE constant from workspaceSelect, and its filter is the OPPOSITE
// one, on purpose: appending `deleted_at IS NOT NULL` to workspaceSelect would
// make that query self-contradictory and every trash read would silently return
// nothing — the exact regression class the form trash documented (issue #98).
// The columns are workspaceSelect's plus nothing else (both carry deleted_at);
// the trash read is the only one that populates it.
const trashedWorkspaceSelect = `SELECT id, owner_user_id, name, slug, deleted_at
	FROM workspaces WHERE deleted_at IS NOT NULL`

// scanWorkspace scans one workspace row. It returns a NIL workspace alongside a
// non-nil error, never a zero-valued struct.
//
// WHY NIL, and how the two halves of the story fit. Returning the ALLOCATED
// struct alongside the error made create_workspace's uniqueness pre-check
// (mcpsrv/workspaces.go) — which tests `existing != nil` and discards the error
// — read a MISS as a hit, so it refused every slug and no workspace could be
// created over MCP at all, while the same create worked over REST (which has no
// pre-check). That is why it went unnoticed.
//
// The two conventions were both reasonable and silently disagreed: this function
// said "a missing row is a value plus an error", the callers said "a missing row
// is nil". The callers are not wrong — every call site in the repo tests the
// pointer or the error — so the store answers nil, the one shape that makes
// `existing != nil` mean what it reads as.
//
// A miss is ALSO the store's not-found sentinel (pgx.ErrNoRows, via
// isNoSuchRow), so a caller that checks the error rather than the pointer still
// gets one — the convention every sibling store follows (see notfound.go).
//
// It is a one-function fix because every call site shares it: the same
// `s, err := ByID(...)` then `if err != nil || s == nil` pairing appears in
// rest_workspace_members.go, rest_workspaces.go, profile.go, the member tools and
// the delete path, and all of them treat the two the same way — only BySlug's
// ignored error made the difference visible.
func scanWorkspace(row rowScanner) (*Workspace, error) {
	s := &Workspace{}
	if err := row.Scan(&s.ID, &s.Owner, &s.Name, &s.Slug, &s.DeletedAt); err != nil {
		if isNoSuchRow(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return s, nil
}

// scanTrashedWorkspace scans a trashedWorkspaceSelect row into the same struct.
// It is a SEPARATE function from scanWorkspace because the drift guard pins each
// select constant to the scan function that reads it — and a trash read that
// shared the live scan would be one column away from silently reading the wrong
// value into DeletedAt.
func scanTrashedWorkspace(row rowScanner) (*Workspace, error) {
	s := &Workspace{}
	if err := row.Scan(&s.ID, &s.Owner, &s.Name, &s.Slug, &s.DeletedAt); err != nil {
		if isNoSuchRow(err) {
			return nil, pgx.ErrNoRows
		}
		return nil, err
	}
	return s, nil
}

// ByOwner lists workspaces owned by a user.
func (s *WorkspaceStore) ByOwner(ctx context.Context, userID string) ([]*Workspace, error) {
	rows, err := s.pool.Query(ctx, workspaceSelect+" AND owner_user_id=$1 ORDER BY created_at", userID)
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
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" AND slug=$1", slug))
}

// ByID fetches one workspace.
func (s *WorkspaceStore) ByID(ctx context.Context, id string) (*Workspace, error) {
	return scanWorkspace(s.pool.QueryRow(ctx, workspaceSelect+" AND id=$1", id))
}

// Create makes a new workspace (slug unique) AND a fresh, empty account linked
// to it (issue #158). Each workspace owns its own account so a new workspace starts
// with a clean slate: no CHIP creds, no products, no sales, no domains, no
// webhooks. CHIP credentials are never shared across workspaces. The account
// slug is derived from the workspace slug (unique-verified by the caller) plus a
// short id suffix to avoid colliding with the workspace's own slug.
//
// A slug that another workspace has MOVED OFF (i.e. one held in
// workspace_slug_aliases, so the addresses already in the wild still resolve) is
// refused with ErrWorkspaceSlugTaken. The check is part of the INSERT statement
// rather than a separate read, so a create can never race a rename into two
// workspaces sharing one host. The UNIQUE index on workspaces.slug remains the
// backstop for a slug that is live (SQLSTATE 23505), which callers already map.
func (s *WorkspaceStore) Create(ctx context.Context, ownerUserID, name, slug string) (*Workspace, error) {
	sp := &Workspace{Owner: ownerUserID, Name: name, Slug: slug}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO workspaces (owner_user_id, name, slug)
		 SELECT $1,$2,$3
		  WHERE NOT EXISTS (SELECT 1 FROM workspace_slug_aliases WHERE slug = $3)
		 RETURNING id, owner_user_id, name, slug`,
		ownerUserID, name, slug).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug)
	if isNoSuchRow(err) {
		// The WHERE NOT EXISTS filtered the row out: the slug is reserved by a
		// workspace that used to answer on it.
		return sp, ErrWorkspaceSlugTaken
	}
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
//
//	A signup creates an account and NO workspace (auth.go; Wan's decision
//	2026-09-20), and a user with zero workspaces is asked to create one before
//	they can reach any create surface. If that first workspace were created the
//	ordinary way, Workspaces.Create would insert a SECOND account linked to it
//	(issue #158) and the signup account would be left behind with
//	workspace_id NULL — reachable by nothing, while the dashboard reads the
//	account resolved from the scoped workspace. So the create path reuses this
//	account instead, and the user ends with exactly one.
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
var errAccountNotLinkable = errors.New("this account cannot be linked to a workspace")

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
	  AND NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.owner_user_id = $1 AND w.deleted_at IS NULL)
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
//   - workspace_settings is materialised lazily by its accessor (Settings
//     INSERT ... ON CONFLICT DO NOTHING); workspace_email_settings is not
//     materialised at all — its accessor (EmailSettings) answers the built-in
//     defaults in memory and writes nothing, so the row appears with the first
//     save. workspace_legal_pages / workspace_invites are per-workspace content
//     that simply does not exist yet. None need a row at provisioning.
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
var errWorkspaceSlugExhausted = errors.New("a workspace slug could not be generated")

// ProvisionLegacyWorkspaceForOwner is the entry point the LOGIN path uses: it
// resolves the owner's email itself and applies Wan's derivation rule (first 5
// characters of the email, sanitised), so no call site can forget the rule or
// invent a slug the API would reject.
//
// The slug is validated before it is used, and the ONE refusal this path
// RECOVERS from is the reserved-name one (card t_ada797d1): the derived name is
// our own guess from the user's email, never something he chose, and a name
// JomForm reserves is policy about OUR names, not a mistake of his. So instead
// of failing, the ladder skips the policy-refused name and the next
// deterministic candidate is used — the same recovery a TAKEN slug already gets
// (legacySlugCandidates does the skipping). Failing here would leave a
// form-owning user without a workspace on every sign-in, forever: the login
// path only logs the error, and the ask-first create path makes an account of
// its own (issue #158) rather than adopting his, so his forms would stay
// invisible with no in-product recovery.
//
// Any OTHER refusal means the derivation is broken, not that policy applies: it
// is returned as an error rather than provisioning a workspace whose own
// subdomain would 400 — a slug the wildcard host cannot serve is worse than no
// workspace, because the user would then have a workspace whose form URLs are
// dead.
//
// Either way, a workspace on a RESERVED host is never minted: the reserved leg
// of the rule (ValidateWorkspaceSlug) is what refuses it, and this path cannot
// insert a name the rule refuses.
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
	if _, err := ValidateWorkspaceSlug(slug); err != nil {
		if !errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
			return nil, fmt.Errorf("invalid legacy slug (%q): %w", slug, err)
		}
		// Policy refusal about the name we derived, which the user never chose
		// and must not be shown: fall through and let the ladder (below) pick
		// the next deterministic name the rule accepts. See the doc comment.
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
//
// A name the slug RULE refuses on its reserved leg is not yielded at all (card
// t_ada797d1). The wanted slug is derived from the user's email, so a local part
// like "docs" or "www" derives a name JomForm reserves; the derivation is our
// guess, not the user's choice, so the ladder steps over it and the next
// candidate is used — exactly as it already steps over a name that is taken.
// Without this the ladder's first entry would be a reserved host, and every
// caller of this ladder is a path that INSERTs workspaces.slug.
//
// The test is the rule itself (ValidateWorkspaceSlug's sentinel), not a second
// copy of the reserved list: the list lives in urlguard and is consulted in one
// place, so a name added there is skipped here the moment it is added.
func legacySlugCandidates(wantSlug, ownerUserID string) []string {
	raw := []string{wantSlug}
	for i := 2; i <= 9; i++ {
		raw = append(raw, fmt.Sprintf("%s-%d", wantSlug, i))
	}
	suffix := ownerUserID
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix != "" {
		raw = append(raw, wantSlug+"-"+suffix)
	}
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		if _, err := ValidateWorkspaceSlug(c); errors.Is(err, ErrWorkspaceSlugReservedSubdomain) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ── Rename (name and/or slug) ───────────────────────────────────────────────────

// WorkspaceRenameInput is a partial update of a workspace's identity. An empty
// Name or Slug means KEEP the current value (the same read-modify-write contract
// the per-workspace settings writer uses), so a caller that only renames the
// slug can never blank the name by omission.
type WorkspaceRenameInput struct {
	WorkspaceID string
	Name        string
	Slug        string
	// CanChangeSlug is the caller's authority to MOVE the tenant's public
	// identity — the owner, and nobody else. A member with manage/admin role may
	// rename the workspace (the label their colleagues see) but not its address:
	// the slug is the host printed on QR stands and shared links, so the person
	// who carries the consequences makes that call. Enforced HERE rather than
	// only in the transports, so a future third caller cannot bypass it by
	// forgetting its own copy.
	CanChangeSlug bool
}

// WorkspaceRenameResult reports what a rename actually changed, so the caller can
// tell the user (and re-point jf_workspace in the browser) without re-reading.
type WorkspaceRenameResult struct {
	Workspace   *Workspace
	OldName     string
	OldSlug     string
	SlugChanged bool
	NameChanged bool
}

// ErrWorkspaceSlugNotOwned means a slug change was requested by a caller without
// the authority to move the tenant's public address.
var ErrWorkspaceSlugNotOwned = errors.New("only the workspace owner can change the slug")

// ErrWorkspaceNoChange means the request asked for the values the workspace
// already has.
var ErrWorkspaceNoChange = errors.New("no changes")

// Rename updates a workspace's name and/or slug in ONE transaction, remembering
// the old slug so every URL already handed out keeps working.
//
// WHY THE ALIAS IS WRITTEN IN THE SAME TRANSACTION. A rename that moved the
// workspace but left no alias would turn every printed QR stand, embed snippet
// and affiliate link naming the old host into a 404 the merchant never sees.
// Either both halves land or neither does.
//
// WHAT IS REFUSED, and why each refusal is a refusal rather than a silent
// overwrite:
//
//	ErrWorkspaceSlugTaken   — the requested slug is LIVE on another workspace
//	                          (its host would answer for two tenants), or it is
//	                          RESERVED as a former slug of another workspace
//	                          (taking it would hijack that workspace's printed
//	                          links). Both leave BOTH sides untouched.
//	ErrWorkspaceSlugNotOwned — a non-owner asked to move the public address.
//	ErrWorkspaceNoChange     — name and slug are both the current values.
//
// A rename BACK onto a slug this workspace used before is allowed and is not a
// collision with itself: the alias row for the new slug is removed, because a
// live slug must not also be an alias.
func (s *WorkspaceStore) Rename(ctx context.Context, in WorkspaceRenameInput) (*WorkspaceRenameResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row: two concurrent renames of one workspace would otherwise both
	// pass the availability check and the loser would fail late with a raw
	// unique-constraint error instead of a 409.
	cur := &Workspace{}
	if err := tx.QueryRow(ctx,
		`SELECT id, owner_user_id, name, slug FROM workspaces WHERE id=$1 FOR UPDATE`,
		in.WorkspaceID).Scan(&cur.ID, &cur.Owner, &cur.Name, &cur.Slug); isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	} else if err != nil {
		return nil, err
	}

	newName := strings.TrimSpace(in.Name)
	if newName == "" {
		newName = cur.Name
	} else {
		// ONLY a name the caller actually supplied is judged by the cap. The
		// check used to run on the FALLBACK value too, so a workspace whose
		// stored name is over the cap could not be renamed AT ALL — including a
		// slug-only PATCH, which is the primary use of this route: the request
		// carries no name, the fallback put the stored (too long) one in
		// newName, and the length test refused the whole call. That is how an
		// unrelated stored value turned a slug move into a 500.
		var nerr error
		newName, nerr = ValidateWorkspaceName(in.Name)
		if nerr != nil {
			return nil, nerr
		}
	}

	wantSlug := strings.TrimSpace(in.Slug)
	slug, verr := cur.Slug, error(nil)
	if wantSlug != "" {
		slug, verr = ValidateWorkspaceSlug(wantSlug)
		if verr != nil {
			return nil, verr
		}
	}

	slugChanged := slug != cur.Slug
	nameChanged := newName != cur.Name
	if !slugChanged && !nameChanged {
		return nil, ErrWorkspaceNoChange
	}
	if slugChanged && !in.CanChangeSlug {
		return nil, ErrWorkspaceSlugNotOwned
	}

	if slugChanged {
		// LIVE collision: another workspace already answers on that host.
		var otherID string
		if err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug=$1`, slug).Scan(&otherID); err == nil && otherID != cur.ID {
			return nil, ErrWorkspaceSlugTaken
		} else if err != nil && !isNoSuchRow(err) {
			return nil, err
		}
		// RESERVED collision: another workspace used to answer there, and its
		// printed links still do. Taking it would hijack them.
		var aliasOwner string
		err := tx.QueryRow(ctx, `SELECT workspace_id FROM workspace_slug_aliases WHERE slug=$1`, slug).Scan(&aliasOwner)
		switch {
		case err == nil && aliasOwner != cur.ID:
			return nil, ErrWorkspaceSlugTaken
		case err == nil:
			// Our OWN former slug, coming back into service: drop the alias so
			// the address is live exactly once.
			if _, derr := tx.Exec(ctx, `DELETE FROM workspace_slug_aliases WHERE slug=$1`, slug); derr != nil {
				return nil, derr
			}
		case isNoSuchRow(err):
		default:
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workspaces SET name=$2, slug=$3 WHERE id=$1`,
		cur.ID, newName, slug); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrWorkspaceSlugTaken
		}
		return nil, err
	}

	if slugChanged {
		// The old host is remembered, so the URLs already in the wild keep
		// resolving to this workspace (see CurrentSlugForHost).
		if _, err := tx.Exec(ctx,
			`INSERT INTO workspace_slug_aliases (slug, workspace_id) VALUES ($1,$2)
			 ON CONFLICT (slug) DO UPDATE SET workspace_id = EXCLUDED.workspace_id`,
			cur.Slug, cur.ID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &WorkspaceRenameResult{
		Workspace:   &Workspace{ID: cur.ID, Owner: cur.Owner, Name: newName, Slug: slug},
		OldName:     cur.Name,
		OldSlug:     cur.Slug,
		SlugChanged: slugChanged,
		NameChanged: nameChanged,
	}, nil
}

// CurrentSlugForHost resolves the slug of the workspace that should answer on a
// given host label, following a rename's alias: the LIVE workspace is found
// first, and only when nothing live matches is the workspace_slug_aliases table
// consulted. Returns "" when neither matches.
//
// This is the ONE host resolution for a former address, and it deliberately
// looks in that order: a slug that is live again (renamed back, or re-created
// after its workspace went away) is served directly and its stale alias row —
// which the rename path removes — can never shadow a live tenant.
//
// It answers with the workspace's CURRENT slug, not a workspace id, because the
// caller's job is to move a request from a former host to the current one
// (a permanent, method-preserving redirect — HTTP 308 with Cache-Control:
// no-store, see PublicHandlers.AliasHostRedirect) rather than to serve the same
// tenant on two hosts.
func (s *WorkspaceStore) CurrentSlugForHost(ctx context.Context, slug string) (string, error) {
	var cur *string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(
		  (SELECT sp.slug FROM workspaces sp WHERE sp.slug = $1),
		  (SELECT sp.slug FROM workspace_slug_aliases a
		     JOIN workspaces sp ON sp.id = a.workspace_id
		    WHERE a.slug = $1)
		)`, slug).Scan(&cur)
	if err != nil {
		return "", err
	}
	if cur == nil {
		return "", nil
	}
	return *cur, nil
}

// AliasesForWorkspace lists the former slugs a workspace still answers on, oldest
// first. Used by the rename answer and the tests; the redirect path does not need
// it.
func (s *WorkspaceStore) AliasesForWorkspace(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT slug FROM workspace_slug_aliases WHERE workspace_id=$1 ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
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
	return s.access(ctx, `AND sp.slug = $1`, slug, userID)
}

// AccessByID resolves the workspace's account ids + the caller's role when the
// user is the owner or a member of the workspace.
func (s *WorkspaceStore) AccessByID(ctx context.Context, workspaceID, userID string) (WorkspaceAccess, bool, error) {
	return s.access(ctx, `AND sp.id = $1`, workspaceID, userID)
}

// access is the shared owner-or-member workspace resolution. The owner is always
// treated as an admin (the migration backfills owners as admin members, and
// new workspaces add the owner as a member at creation), so a workspace created
// before the backfill still resolves correctly.
//
// The account id is COALESCEd to an empty string because an account-less
// workspace is a real state (000024_space_accounts only backfilled spaces that
// lacked one, and the delete tests create workspaces with no accounts row
// directly). The LEFT JOIN
// emits NULL for it, and a strict scan into *string fails the WHOLE call — every
// row, not just that one — which reads downstream as "workspace not found"
// (403) instead of "workspace with no accounts yet".
func (s *WorkspaceStore) access(ctx context.Context, where string, key, userID string) (WorkspaceAccess, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(a.id::text, ''), COALESCE(m.role, '')
		FROM workspaces sp
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $2
		WHERE sp.deleted_at IS NULL `+where, key, userID)
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

// WorkspaceBlocker is one category of content a workspace holds, with its count.
//
// IT NO LONGER BLOCKS ANYTHING. It was the refusal shape for the old "not_empty"
// guard on delete; that guard is GONE (Wan's decision: "Buang guard — workspace
// penuh boleh masuk bin, restore bila perlu (data kekal 100%)"), because a
// workspace that keeps every one of its rows has nothing left to protect against.
// The census survives because it is still the honest answer to "what is in this
// workspace?", which the Dashboard's Danger zone shows before a delete and the
// trash view shows after one.
type WorkspaceBlocker struct {
	// Kind is the machine-readable category: "forms", "products", "sales",
	// "domains", "webhooks", "api_keys" or "subscriptions".
	Kind string `json:"kind"`
	// Count is how many rows of that kind the workspace still owns.
	Count int64 `json:"count"`
	// Label is the human-readable category name shown in the census.
	Label string `json:"label"`
}

// WorkspaceDeleteError is returned by Delete when the workspace cannot be
// trashed. ONE shape:
//
//	reason "last_workspace" — the owner would be left with no LIVE workspace. A
//	   user must always have at least one, so this is the guard Wan asked for: it
//	   protects whichever workspace is last without hard-coding which one that is.
//
// The old "not_empty" reason is deliberately gone with its guard: since the
// delete only MARKS the workspace (and Purge is the only thing that destroys
// rows), refusing to bin a workspace because it holds forms would be refusing to
// do the one thing this path now does safely.
type WorkspaceDeleteError struct {
	Reason string
}

func (e *WorkspaceDeleteError) Error() string {
	if e.Reason == "last_workspace" {
		return "at least one workspace is required"
	}
	return "the workspace cannot be deleted"
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
	"forms":         "forms (including those in the Trash)",
	"products":      "products",
	"sales":         "sales",
	"subscriptions": "subscriptions (billing history)",
	"domains":       "custom domains",
	"webhooks":      "webhooks",
	"api_keys":      "API keys",
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

// Trash MARKS a workspace deleted. Nothing is destroyed: every row under it
// (its accounts, forms, products, sales, api_keys, members, settings) stays on
// disk, untouched, and Restore brings the workspace back whole.
//
// THIS USED TO BE A HARD DELETE. The old Delete ran real DELETEs deepest-first
// over the workspace's whole world and the workspace row itself. That ordering
// analysis was correct and is REUSED verbatim by Purge below — the FKs are NO
// ACTION (verified on the live schema: accounts/api_keys/forms/products/sales
// .workspace_id are all NO ACTION, while workspace_settings/workspace_members/
// workspace_invites/workspace_legal_pages/workspace_email_settings/affiliates
// cascade), and every workspace MUST own an account (migration 000024), so a
// bare `DELETE FROM workspaces` works on literally nothing. It is not discarded;
// it MOVES.
//
// WHY THE `not_empty` GUARD IS GONE (Wan's decision, verbatim: "Buang guard —
// workspace penuh boleh masuk bin, restore bila perlu (data kekal 100%)").
// Refusing to bin a workspace because it still held forms made the delete nearly
// useless — emptying a workspace first was, in practice, impossible, because a
// trashed form still counted. With every row KEPT by this method, there is
// nothing left for such a guard to protect against: the operation it was
// guarding is now reversible, so it protects nothing and costs the feature.
//
// WHY THE `last_workspace` GUARD STAYS. A user must always retain at least one
// LIVE workspace (a user with none cannot be represented, and the switcher has
// nowhere to land them). That rule was never part of what was asked to change
// and is enforced here as before — now with `deleted_at IS NULL` on the census,
// so binning your workspace does not count as still having one.
//
// The mark is refused when the row is already trashed (ErrWorkspaceNotFound,
// exactly as an unknown id is): trashing twice is not two operations, and a
// second call must not quietly move deleted_at forward.
func (s *WorkspaceStore) Trash(ctx context.Context, workspaceID, ownerUserID string) (*Workspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row: two concurrent trashes would otherwise both pass the guard
	// and the second would overwrite the first's deleted_at. FOR UPDATE on the
	// already-trashed row is excluded by the WHERE, so this also serialises
	// against a concurrent Restore.
	sp := &Workspace{}
	err = tx.QueryRow(ctx,
		`SELECT id, owner_user_id, name, slug, deleted_at FROM workspaces
		  WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`,
		workspaceID).Scan(&sp.ID, &sp.Owner, &sp.Name, &sp.Slug, &sp.DeletedAt)
	if isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	}
	if err != nil {
		return nil, err
	}

	// Guard — at least one LIVE workspace must remain. Counted on the owner's
	// membership (owned OR member), which is exactly what the switcher lists, so
	// a user who is a member of someone else's workspace still has one to land
	// on and may bin their own last one.
	//
	// `deleted_at IS NULL` on BOTH legs is the whole point: a workspace already
	// in the bin is not somewhere to land, and a member row pointing at a
	// trashed workspace must not be counted as one. Without it, binning your
	// only workspace would be allowed as soon as you were a member of it — the
	// `last_workspace` rule would silently stop protecting the user.
	var remaining int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspaces sp
		 WHERE sp.id <> $1
		   AND sp.deleted_at IS NULL
		   AND (sp.owner_user_id = $2
		        OR EXISTS (SELECT 1 FROM workspace_members m
		                    WHERE m.workspace_id = sp.id AND m.user_id = $2))`,
		workspaceID, ownerUserID).Scan(&remaining); err != nil {
		return nil, err
	}
	if remaining == 0 {
		return nil, &WorkspaceDeleteError{Reason: "last_workspace"}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE workspaces SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`,
		workspaceID); err != nil {
		return nil, err
	}

	// Clear a login default pointing at the binned workspace.
	//
	// Under the old HARD delete this happened by itself: users.default_workspace_id
	// REFERENCES workspaces(id) ON DELETE SET NULL, so removing the row nulled the
	// default. A MARK fires no FK, so without this line the default survives and
	// the next login lands the user on a workspace that is in the bin — the exact
	// "dangling default" the SET NULL was there to prevent, moved from a visible
	// bug to a silent one (login succeeds, dashboard is empty, nothing explains
	// why). Restore does not re-point it: the user picks their default again
	// rather than having a stale choice resurrected behind their back.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET default_workspace_id=NULL WHERE default_workspace_id=$1`,
		workspaceID); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// A fresh read, so the caller sees the committed mark rather than a
	// hand-built struct.
	return s.TrashedByID(ctx, workspaceID)
}

// Delete is the historical name for Trash, kept as a thin alias so the REST and
// MCP call sites (and their tests) name the dashboard action they perform. It is
// NOT a hard delete any more — Purge is.
func (s *WorkspaceStore) Delete(ctx context.Context, workspaceID, ownerUserID, _ string) (map[string]int64, error) {
	sp, err := s.Trash(ctx, workspaceID, ownerUserID)
	if err != nil {
		return nil, err
	}
	// The response shape the audit event and the dashboard already read. The old
	// DELETE reported per-table row counts because it removed rows; this reports
	// what the mark covers, so an operator reading an old log line still learns
	// which tables the workspace holds.
	counts, err := s.ContentCounts(ctx, workspaceID, "")
	if err != nil {
		return nil, err
	}
	out := map[string]int64{"workspaces": 1}
	for k, v := range counts {
		out[k] = v
	}
	_ = sp
	return out, nil
}

// Restore clears deleted_at on a trashed workspace, bringing it and everything
// under it back exactly as it was — no row was ever touched by the trash, so
// there is nothing to rebuild.
//
// A restore whose slug was re-claimed while the workspace sat in the bin is
// REFUSED with ErrWorkspaceSlugTaken rather than failing as an opaque 500: the
// unique index covers LIVE rows only, so trashing releases the name and a new
// workspace may take it (issue: a trashed workspace must not hold a name
// forever). Clearing deleted_at then violates the index (SQLSTATE 23505).
// Nothing is renamed behind the user's back — the same rule forms already
// follows (FormStore.Restore / ErrFormSlugTaken), and the caller is told which
// obstacle to clear.
func (s *WorkspaceStore) Restore(ctx context.Context, workspaceID, ownerUserID string) error {
	// Ownership is checked in the WHERE: only the owner may bring a workspace
	// back, matching who may bin it.
	tag, err := s.pool.Exec(ctx,
		`UPDATE workspaces SET deleted_at=NULL
		  WHERE id=$1 AND owner_user_id=$2 AND deleted_at IS NOT NULL`,
		workspaceID, ownerUserID)
	if isUniqueViolation(err) {
		return ErrWorkspaceSlugTaken
	}
	// A caller-supplied id that cannot be a uuid at all raises SQLSTATE 22P02
	// BEFORE any row is considered (see notfound.go), so it reaches here as an
	// Exec error rather than as RowsAffected()==0. It is the same miss the branch
	// below reports: an untrusted id that names nothing. Without this the route
	// answered 500 {"code":"internal"} for `POST /v1/workspaces/not-a-uuid/restore`
	// while the SAME id on the purge route answered 404 — two answers to one
	// situation, and the 500 is the one that counts a caller's bad id as a
	// server fault (measured in this worktree before the fix; card t_a71ee62b).
	if isNoSuchRow(err) {
		return ErrWorkspaceNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWorkspaceNotFound
	}
	return nil
}

// Purge destroys a TRASHED workspace and everything that belongs to it, for
// good. This is where the hard delete lives.
//
// The ORDER IS THE OLD Delete's ORDER, unchanged, because that ordering is what
// makes the cascade reach the workspace at all:
//
//  1. trading history first — sales (their subscription/charge children
//     cascade), then forms, products, api_keys. These are the rows whose
//     workspace_id/account_id FKs are NO ACTION, so the workspace row cannot go
//     while they are present.
//  2. the accounts the WORKSPACE CREATED — a purge destroys what the workspace
//     brought into existence and UNLINKS what predates it; see THE ACCOUNT RULE
//     below, which is the deliberate boundary of this method. They hold the CHIP
//     credentials, and the repo invariant is that CHIP is never shared between
//     workspaces, so an account born with a workspace that was left behind
//     would be a credential nobody owns.
//  3. the workspace row itself. workspace_settings / workspace_members /
//     workspace_invites / workspace_legal_pages / workspace_email_settings /
//     affiliates go with it by CASCADE; users.default_workspace_id is SET NULL.
//
// THE ACCOUNT RULE (card t_9fe4c36a) — a purge deletes the accounts the
// workspace CREATED and UNLINKS the ones that predate it. It never destroys a
// row that existed before the workspace.
//
// WHY THIS NEEDED DECIDING RATHER THAN "fixing". Step 2 used to run
// `DELETE FROM accounts WHERE workspace_id=$1`, which conflates two different
// rows behind one column:
//
//   - the account Workspaces.Create inserted FOR this workspace — born with it,
//     empty CHIP creds, and unreachable if left behind, which is why a purge
//     must take it; and
//   - the account that EXISTED BEFORE the workspace. The ask-first create does
//     not insert one: SoleEmptyUnscopedAccount + CreateForAccount LINK the
//     signup account the user already owns (issue #158), and
//     ProvisionLegacyWorkspace reuses the same link for a legacy content-carrying
//     account. That account carries the merchant's own identity — its slug, its
//     CHIP brand id and encrypted secret key — and the purge destroyed it
//     because the caller typed the WORKSPACE's slug, not because the account was
//     the workspace's.
//
// MEASURED, not hypothetical (t_eff6e23a, prod, 2026-10-01): a pre-existing
// unlinked account (created 2026-09-01, workspace_id NULL, read live before the
// test) was adopted by a first-workspace create, and the purge that followed
// answered {"purged":{"accounts":1,...}} and destroyed it. It had to be restored
// by hand from its read-live shape: the purge is the hard delete, so nothing in
// the product can bring the row back.
//
// THE DISCRIMINATOR is accounts.created_at against workspaces.created_at, and it
// is exact for both write paths rather than a guess about them: Create inserts
// the workspace first and the account second, so an account the workspace made
// is never older than it, while every linking path UPDATEs an account that was
// inserted earlier (at signup, or before the legacy provisioning). The rule is
// also stable under itself — an account this rule unlinks can be adopted by a
// LATER workspace and is still older than that one, so it is preserved again.
// The failure direction is chosen deliberately: an account is preserved only
// when it is STRICTLY older than the workspace, so a shape the ordering cannot
// distinguish behaves exactly as it did before this change (deleted), and the
// destructive branch is never the surprising one.
//
// WHAT HAPPENS TO AN UNLINKED ACCOUNT, stated rather than left implicit. It
// returns to `workspace_id = NULL` — the exact state it was in before the
// adoption, which is the state SoleEmptyUnscopedAccount looks for, so the next
// first-workspace create reuses it and no row is stranded. It keeps what it
// holds (its slug, its CHIP linkage) because the purge names the WORKSPACE and
// that identity is the merchant's, not the workspace's. It keeps NOTHING of the
// workspace's: forms, products, sales and api_keys were deleted in step 1 by
// account_id, so a later reuse cannot hand the next workspace the destroyed
// one's content. NAMED CONSEQUENCE: a merchant who purges a workspace and
// immediately creates another gets the same account back, CHIP linkage included
// — visible and changeable in their CHIP settings, whereas the destroyed row was
// not recoverable at all. Clearing the credentials here was rejected: it would
// be a second destructive act (against material the user typed) that the
// caller's confirmation — the workspace's slug — never named.
//
// The two kinds are reported separately (`accounts` deleted, `accounts_unlinked`
// preserved) so the caller is told which happened instead of inferring it from a
// total. The behaviour is pinned by
// TestWorkspacePurgeDoesNotDestroyAnAdoptedLegacyAccount (dbstore) and
// TestRESTWorkspacePurgeUnlinksAnAdoptedLegacyAccount (httpapi).
//
// TWO GUARDS, because a purge cannot be undone:
//
//  1. Only a TRASHED workspace may be purged (`deleted_at IS NOT NULL`), so the
//     only way in is trash → purge and a live workspace cannot be destroyed by
//     a single mistyped call. A live/unknown id is ErrWorkspaceNotFound here,
//     exactly as an unknown id is.
//  2. SALES REFUSE IT, with ErrWorkspaceHasSales. A workspace with trading
//     history is not a scratch workspace: the purge deletes those sales rows
//     outright, and sales are the merchant's record of money received. The
//     merchant is told to settle them first. Counted inside the transaction so
//     a sale landing between the check and the delete cannot slip through.
//
// WHAT IS HUMAN-ONLY, PRECISELY (card t_c295f96e). This block used to open
// "HUMAN-ONLY, ENFORCED ELSEWHERE", which reads as "only a person can purge".
// That is not what the repo enforces, and saying it beside a method an owner's
// API key can reach through MCP is the same over-strong claim the REST route
// carried. The enforced rule is narrower and worth naming exactly:
//
//   - NOT PROPOSABLE TO THE IN-APP CHAT. mcpsrv (chat_tools.go /
//     chat_tools_guard_test.go) pins that no registered tool whose name carries a
//     purge/permanent verb is ever proposable, so the chat cannot offer this.
//   - NOT ON THE DASHBOARD. No web surface calls DELETE /v1/workspaces/{id}/purge
//     (the Trash view on Settings → Workspace is restore-only), so this step is
//     reached from the REST API or from MCP — not by clicking.
//   - REACHABLE BY MCP, deliberately. The registered tool is purge_workspace, and
//     a write-scoped key on the owner's account reaches it — the same class
//     purge_form / purge_product / purge_media have always been in. An MCP client
//     acting for the owner is not the in-app chat, which is the axis the rule
//     actually turns on.
//
// The two conditions a caller must still clear are unchanged: trashed-only, and
// never a workspace that has sales. This method is the store half of that pair.
func (s *WorkspaceStore) Purge(ctx context.Context, workspaceID, ownerUserID string) (map[string]int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row so a concurrent Restore cannot flip it live between the
	// trashed check and the delete.
	var owner string
	if err := tx.QueryRow(ctx,
		`SELECT owner_user_id FROM workspaces
		  WHERE id=$1 AND deleted_at IS NOT NULL FOR UPDATE`,
		workspaceID).Scan(&owner); isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	} else if err != nil {
		return nil, err
	}
	// Owner-only, the same rule as the trash itself.
	if ownerUserID == "" || owner != ownerUserID {
		return nil, &WorkspaceDeleteError{Reason: "not_owner"}
	}

	accIDs := []string{}
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

	// Guard — never destroy trading history. Sales RESTRICT their account, so
	// without this the purge of a sold workspace would either fail late with a
	// raw FK error or (worse, if the FK were ever relaxed) silently discard the
	// merchant's record of money received.
	var sales int64
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM sales WHERE account_id = ANY($1)`, accIDs).Scan(&sales); err != nil {
		return nil, err
	}
	if sales > 0 {
		return nil, ErrWorkspaceHasSales
	}

	purged := map[string]int64{}
	// Step 1: the RESTRICT rows, deepest-first. The subscription children
	// (subscription_charges) cascade off their subscription.
	for _, step := range []struct {
		table string
		sql   string
	}{
		{"sales", `DELETE FROM sales WHERE account_id = ANY($1)`},
		// SUBSCRIPTIONS BEFORE PRODUCTS (card t_3f895712). BOTH ends of a
		// subscription RESTRICT: `subscriptions.account_id → accounts` and
		// `subscriptions.product_id → products`, so an account whose purge reached
		// the products step while a subscription row still existed failed with a raw
		// FK error — and a sold workspace, which is refused above anyway, was not the
		// only way to hit it. Deleting the subscription here removes both ends at
		// once. `subscription_charges` cascades off the parent, so its history goes
		// with it; that is the same ordering the accounts/workspace deletions below
		// already assume.
		{"subscriptions", `DELETE FROM subscriptions WHERE account_id = ANY($1)`},
		{"forms", `DELETE FROM forms WHERE account_id = ANY($1)`},
		{"products", `DELETE FROM products WHERE account_id = ANY($1)`},
		{"api_keys", `DELETE FROM api_keys WHERE account_id = ANY($1)`},
	} {
		tag, derr := tx.Exec(ctx, step.sql, accIDs)
		if derr != nil {
			return nil, derr
		}
		purged[step.table] = tag.RowsAffected()
	}
	// Step 2: the accounts, on THE ACCOUNT RULE above — the ones this workspace
	// CREATED are deleted; the ones that PREDATE it (adopted by the ask-first
	// create or the legacy provisioning) are UNLINKED, never destroyed.
	//
	// BOTH statements must run before step 3: accounts.workspace_id is a plain
	// nullable FK with NO ACTION, so a preserved account still pointing at the
	// workspace would make the workspace's own delete fail with a raw 23503 —
	// silently turning this rule into "the purge refuses", which is not it.
	if len(accIDs) > 0 {
		tag, derr := tx.Exec(ctx, `
			DELETE FROM accounts
			 WHERE workspace_id = $1
			   AND created_at >= (SELECT created_at FROM workspaces WHERE id = $1)`,
			workspaceID)
		if derr != nil {
			return nil, derr
		}
		purged["accounts"] = tag.RowsAffected()

		unlinked, derr := tx.Exec(ctx, `
			UPDATE accounts SET workspace_id = NULL, updated_at = now()
			 WHERE workspace_id = $1
			   AND created_at < (SELECT created_at FROM workspaces WHERE id = $1)`,
			workspaceID)
		if derr != nil {
			return nil, derr
		}
		purged["accounts_unlinked"] = unlinked.RowsAffected()
	}
	// Step 3: the workspace itself. Its CASCADE children fall away with it.
	tag, derr := tx.Exec(ctx,
		`DELETE FROM workspaces WHERE id=$1 AND deleted_at IS NOT NULL`, workspaceID)
	if derr != nil {
		return nil, derr
	}
	purged["workspaces"] = tag.RowsAffected()
	if purged["workspaces"] == 0 {
		return nil, ErrWorkspaceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return purged, nil
}

// ErrWorkspaceNotFound means the workspace id named no row (or named a workspace
// in the state the call does not act on — a live row for a purge, a trashed row
// for a trash).
var ErrWorkspaceNotFound = errors.New("workspace not found")

// A restore whose slug was re-claimed by another LIVE workspace while this one
// sat in the bin is refused with ErrWorkspaceSlugTaken (declared once, at the top
// of this file, beside the rename/create use of the same rule).

// ErrWorkspaceHasSales refuses a purge of a workspace that has EVER received a
// sale. Sales are the merchant's record of money received; a purge destroys
// them, so a workspace holding any is not purgable.
var ErrWorkspaceHasSales = errors.New("this workspace has sales")

// AccountIsLive reports whether accountID belongs to a workspace that is NOT in
// the bin.
//
// This is the predicate audit attribution must use, and it is deliberately not
// "does the row exist". Under the trash every account row survives its
// workspace's move to the bin, so an existence check would happily file an
// event against a workspace the merchant has just put away — the audit trail for
// a delete would be stored inside the thing that was deleted, which is exactly
// the failure mode the hard-delete era produced by a different route (the FK
// refused the write and it was swallowed).
//
// An account with no workspace at all (workspace_id IS NULL) counts as live: it
// is not parked on a binned workspace, and it is the caller's own.
//
// The EXISTS subquery is deliberate, not a stylistic choice: a repo guard
// (web/src/lib/formPublicURL.test.ts, "layer 2 is intact") pins the account-slug
// fallback's join shape OUT of this file, because that join is how the removed
// fallback resolved a host. This predicate asks whether a workspace is live —
// the opposite question — so it must not be written in that shape, and the
// banned text must not appear here even in a comment (the guard greps the whole
// file). Do not "simplify" this back.
func (s *WorkspaceStore) AccountIsLive(ctx context.Context, accountID string) (bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM accounts a
			WHERE a.id = $1
			  AND (a.workspace_id IS NULL OR EXISTS (
			        SELECT 1 FROM workspaces sp
			         WHERE sp.id = a.workspace_id AND sp.deleted_at IS NULL))
		)`, accountID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var live bool
	if err := rows.Scan(&live); err != nil {
		return false, err
	}
	return live, rows.Err()
}

// TrashedByID reads one workspace from the bin. It uses trashedWorkspaceSelect,
// so it matches rows the live readers deliberately hide.
//
// A miss is this STORE's not-found sentinel, ErrWorkspaceNotFound — the same
// convention the other trash reads in this package follow (FormStore.GetTrashed
// and ProductStore.GetTrashed both map isNoSuchRow to their own sentinel), and
// the shape its own siblings here answer with (Restore via RowsAffected==0,
// Trash/Rename/Purge via isNoSuchRow).
//
// WHY IT MUST NOT BE THE RAW DRIVER ERROR (card t_a71ee62b, live on v1.19.1).
// scanTrashedWorkspace maps a missing row and SQLSTATE 22P02 to pgx.ErrNoRows,
// which is NOT ErrWorkspaceNotFound. rest_workspaces.go's purgeWorkspace tests
// only errors.Is(err, ErrWorkspaceNotFound), so a raw miss fell through to
// respondErr's default branch: EVERY not-found purge answered
// 500 {"code":"internal"} with error_class=internal and a stack trace — an
// unknown uuid, a non-uuid, and a REAL live workspace id alike — while the
// sibling restore/bin routes on the same session answered 404. A caller's bad
// id must not be counted as a server fault, and the sentence beside that
// handler ("a LIVE workspace is a 404 here") has to be true.
//
// Both miss shapes are covered by mapping at the error level, not by parsing the
// id first: Postgres keeps owning what "a valid uuid" means (braces, hyphens,
// prefixes and all), exactly as notfound.go's isNoSuchRow documents.
func (s *WorkspaceStore) TrashedByID(ctx context.Context, id string) (*Workspace, error) {
	sp, err := scanTrashedWorkspace(s.pool.QueryRow(ctx, trashedWorkspaceSelect+" AND id=$1", id))
	if isNoSuchRow(err) {
		return nil, ErrWorkspaceNotFound
	}
	return sp, err
}

// ListTrashed lists the caller's TRASHED workspaces, newest-binned first.
//
// It is deliberately scoped by OWNER (not "owned or member"): only the owner may
// restore or purge, so listing a member's trashed workspace would offer a row
// they cannot act on. It must NOT reuse workspaceSelect, which filters
// `deleted_at IS NULL`; appending `deleted_at IS NOT NULL` to that would make the
// query self-contradictory and the trash list would ALWAYS be empty — the issue
// #98 regression class, which is why the two constants are separate.
func (s *WorkspaceStore) ListTrashed(ctx context.Context, ownerUserID string) ([]*Workspace, error) {
	rows, err := s.pool.Query(ctx,
		trashedWorkspaceSelect+" AND owner_user_id=$1 ORDER BY deleted_at DESC", ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workspace
	for rows.Next() {
		sp, serr := scanTrashedWorkspace(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ContentCounts reports what a workspace holds, by category — the census the
// dashboard shows beside a trash/delete action, so the effect of the decision is
// visible before it is made. It uses the SAME one definition of the census the
// delete path used (emptyCheckSQL), which is why that query survived the guard's
// removal.
func (s *WorkspaceStore) ContentCounts(ctx context.Context, workspaceID, exemptKeyID string) (map[string]int64, error) {
	accs, err := s.accountIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{"accounts": int64(len(accs))}
	if len(accs) == 0 {
		return out, nil
	}
	var forms, products, sales, subs, domains, webhooks, keys int64
	if err := s.pool.QueryRow(ctx, emptyCheckSQL, emptyCheckArgs(accs, exemptKeyID)...).
		Scan(&forms, &products, &sales, &subs, &domains, &webhooks, &keys); err != nil {
		return nil, err
	}
	for _, b := range blockersFrom(forms, products, sales, subs, domains, webhooks, keys) {
		out[b.Kind] = b.Count
	}
	return out, nil
}

// ByMember lists workspaces the user owns OR is a member of, with the caller's
// role in each (issue #162). Used by the workspace switcher so members see the
// workspaces they belong to, not just the ones they own.
//
// A TRASHED workspace is excluded: the switcher is the list of places the user
// can go, and a binned workspace is not one of them — sending them there would
// show an empty dashboard with no way to explain why. The trash has its own
// read (ListTrashed), which is what the dashboard's trash view uses.
//
// An ACCOUNT-LESS workspace is INCLUDED, with an empty Accounts slice. It is a
// real state (see WorkspaceStore.access) and it must not be allowed to take the
// caller's whole listing down: the LEFT JOIN emits NULL for it, and a strict
// scan into *string used to fail the entire query with
// `cannot scan NULL into *string`, so ONE account-less workspace made ByMember
// return (nil, err) — which resolveSessionAccount reads as "no workspaces" (401
// for a signed-in user), and isLastWorkspace reads as "last workspace" (every
// delete refused).
func (s *WorkspaceStore) ByMember(ctx context.Context, userID string) ([]WorkspaceAccess, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.name, sp.slug, sp.owner_user_id, COALESCE(m.role, ''), COALESCE(a.id::text, '')
		FROM workspaces sp
		LEFT JOIN workspace_members m ON m.workspace_id = sp.id AND m.user_id = $1
		LEFT JOIN accounts a ON a.workspace_id = sp.id
		WHERE sp.deleted_at IS NULL
		  AND (sp.owner_user_id = $1 OR m.user_id = $1)
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

// IDBySlug resolves a LIVE workspace id from its slug ("" when unknown).
//
// `deleted_at IS NULL` is load-bearing, not defensive: the slug index covers live
// rows only, so a trashed workspace and a live one can hold the same slug at the
// same time. Without the filter this query could resolve the TRASHED id, and
// every caller (the public storefront root, the legal-page host, order routing)
// would then serve a binned workspace's data.
func (s *WorkspaceStore) IDBySlug(ctx context.Context, slug string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM workspaces WHERE slug=$1 AND deleted_at IS NULL`, slug).Scan(&id)
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

// HasLiveWorkspace reports whether the account is linked to a workspace that has
// not been binned.
//
// WHY IT EXISTS (card t_6c0badbe). `accounts.workspace_id` is nullable: a signup
// creates an account and NO workspace (issue #718 — the user is asked to create
// one first), and `forms_sync_workspace_id` (migration 000045) copies that NULL
// onto every form the account creates. A form with `workspace_id IS NULL` can
// still be PUBLISHED, and then it is unreachable forever: the public serving
// path resolves a form by workspace host (and, on a self-hosted install, by the
// workspace behind the slug — ownOriginForm), so nothing ever serves it. The
// merchant sees a form marked published that answers 404 at every address.
//
// `deleted_at IS NULL` is part of the question, not an extra filter: the public
// host resolver excludes binned workspaces, so a form in a binned workspace is
// exactly as unreachable as a form with none.
//
// WorkspaceIDByAccount is NOT usable here: it scans a nullable column into a
// string, so the very case this asks about is indistinguishable from a database
// error.
func (s *WorkspaceStore) HasLiveWorkspace(ctx context.Context, accountID string) (bool, error) {
	if accountID == "" {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM accounts a
		     JOIN workspaces w ON w.id = a.workspace_id AND w.deleted_at IS NULL
		    WHERE a.id = $1)`, accountID).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
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
//
// The join excludes a TRASHED workspace: its slug may have been re-claimed by a
// live workspace, and building a public URL from the binned row would point at a
// host that no longer serves this account. A trashed workspace resolves to "",
// which callers already handle (they fall back rather than emitting a dead link).
func (s *WorkspaceStore) WorkspaceSlugByAccount(ctx context.Context, accountID string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx,
		`SELECT sp.slug FROM accounts a JOIN workspaces sp ON sp.id = a.workspace_id
		  WHERE a.id=$1 AND sp.deleted_at IS NULL`, accountID).Scan(&slug)
	if err != nil {
		return "", err
	}
	return slug, nil
}
