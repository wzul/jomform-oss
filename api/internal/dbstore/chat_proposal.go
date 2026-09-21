// Chat write proposals (phase 2 of the in-app assistant): the stored, single-use
// binding between a write the model PROPOSED and the confirmation a HUMAN gives.
//
// WHY THIS TABLE EXISTS (the property it holds, not just what it stores).
//
// In phase 2 the model may describe a write it wants to perform. The danger is
// not that the model can write — it cannot, see internal/chat — it is that the
// thing confirmed is not the thing that was proposed. So the proposal is
// persisted as a whole:
//
//	tool + canonical arguments + the caller who may confirm it + an expiry.
//
// The confirm step executes the STORED arguments, and the wire carries an
// args_hash the UI must echo, so:
//
//   - changing the arguments between propose and confirm is refused (the hash
//     the client sends must equal the stored hash — see ClaimArgsHash);
//   - using a token twice is refused, because Claim is a single UPDATE ...
//     WHERE consumed_at IS NULL RETURNING: two concurrent confirms cannot both
//     win a row (no read-then-write window);
//   - an expired token is refused;
//   - another caller's token is refused exactly like a token that does not
//     exist, because every read here is scoped by user_id (the same
//     no-existence-oracle rule the feedback store follows).
//
// The token itself is stored ONLY as sha256. The raw value exists in the propose
// response and in the user's browser; nothing that can read this table (a
// backup, a replica, a psql session) can replay a proposal with it.
//
// A CONSUMED PROPOSAL IS SPENT EVEN IF EXECUTION FAILS. The alternative — a
// failed execution releases the token — is a double-write hazard the moment a
// network error arrives after the write actually landed (the classic ambiguous
// failure). The user's recovery is to ask again, which produces a fresh
// proposal and a fresh card.
package dbstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The proposal store's sentinel errors. The REST layer maps all three to a
// refusal that does not leak which case it was to a caller who is not the
// owner: Claim scopes by user_id, so a stranger's token classifies as
// ErrChatProposalNotFound.
var (
	// ErrChatProposalNotFound means no unspent proposal with this (token, user)
	// exists — including the case where the token belongs to somebody else.
	ErrChatProposalNotFound = errors.New("cadangan tidak dijumpai")
	// ErrChatProposalExpired means the proposal existed but its window closed.
	// A proposal is decided while the user is reading the card, not later.
	ErrChatProposalExpired = errors.New("cadangan telah luput")
	// ErrChatProposalConsumed means the token was already confirmed or
	// cancelled: a spent token is never spent twice.
	ErrChatProposalConsumed = errors.New("cadangan sudah digunakan atau dibatalkan")
)

// chatProposalSelect is the column list every read in this file scans, kept as
// one constant so a column added by a migration has one place to be added. The
// scan sites are ChatWriteProposalStore.Create (RETURNING) and .Claim — both
// below. (chat_write_proposals has no `*Select` constant audited by
// migration_scan_drift_test.go; this file keeps its own aligned by hand.)
const chatProposalSelect = `id, token_hash, user_id, account_id, tool, args, args_hash,
	title, summary, note, created_at, expires_at, consumed_at`

// ChatWriteProposal is one stored proposal: a write tool plus the exact
// arguments the user was shown, bound to the person who may confirm it.
type ChatWriteProposal struct {
	ID        string
	TokenHash string
	UserID    string
	AccountID string
	Tool      string
	// Args is the canonical (sorted-key) argument object. It is what EXECUTES:
	// the confirm path never takes arguments from the request.
	Args map[string]any
	// ArgsHash is sha256(hex) of the canonical arguments. The UI echoes it on
	// confirm, which is how "the arguments did not change" is checked.
	ArgsHash string
	// Title / Summary are the SERVER's rendering of the effect, derived from
	// (tool, args) only — never from model prose — so the card cannot describe
	// an effect the arguments do not have.
	Title   string
	Summary string
	// Note is the model's own explanation, shown to the user as the assistant's
	// wording. It is untrusted display text and is never parsed.
	Note       string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// IsSpent reports whether the token has already been used (confirmed or
// cancelled). Callers do not need it for correctness — Claim enforces it — but
// the audit view reads it.
func (p *ChatWriteProposal) IsSpent() bool { return p != nil && p.ConsumedAt != nil }

// ChatWriteProposalStore wraps pgx for chat_write_proposals.
type ChatWriteProposalStore struct{ pool *pgxpool.Pool }

// NewChatWriteProposalStore builds a ChatWriteProposalStore.
func NewChatWriteProposalStore(pool *pgxpool.Pool) *ChatWriteProposalStore {
	return &ChatWriteProposalStore{pool: pool}
}

// CanonicalArgs renders an argument object as canonical JSON: object keys sorted
// (encoding/json sorts map keys), no insignificant whitespace. It is the form
// that is both hashed and has the hash compared against, so two spellings of the
// same object cannot produce two different hashes.
func CanonicalArgs(args map[string]any) ([]byte, error) {
	if args == nil {
		args = map[string]any{}
	}
	return json.Marshal(args)
}

// HashArgs returns the hex sha256 of the canonical form of args. It is exported
// because the REST layer compares it against what the client echoed, and a test
// asserts the two implementations agree.
func HashArgs(args map[string]any) (string, error) {
	b, err := CanonicalArgs(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// HashProposalToken hashes a raw opaque token for storage/lookup. Only the hash
// is ever persisted (see the file header).
func HashProposalToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Create stores a proposal and fills in its ID and CreatedAt. ArgsHash is
// recomputed from Args here (not taken from the caller) so a stored row can
// never carry a hash that disagrees with its own arguments — the exact
// inconsistency the confirm path would then be trusting.
func (s *ChatWriteProposalStore) Create(ctx context.Context, p *ChatWriteProposal) error {
	if p == nil {
		return errors.New("cadangan kosong")
	}
	if p.TokenHash == "" || p.UserID == "" || p.AccountID == "" || p.Tool == "" {
		return errors.New("cadangan tidak lengkap (token, pengguna, akaun dan alat diperlukan)")
	}
	canon, err := CanonicalArgs(p.Args)
	if err != nil {
		return err
	}
	h := sha256.Sum256(canon)
	p.ArgsHash = hex.EncodeToString(h[:])
	p.CreatedAt = time.Now().UTC()
	if p.ExpiresAt.IsZero() {
		return errors.New("cadangan tanpa masa luput — tetapan luput wajib")
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO chat_write_proposals
		   (token_hash, user_id, account_id, tool, args, args_hash, title, summary, note, expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 RETURNING id, created_at, expires_at`,
		p.TokenHash, p.UserID, p.AccountID, p.Tool, string(canon), p.ArgsHash,
		p.Title, p.Summary, p.Note, p.ExpiresAt).
		Scan(&p.ID, &p.CreatedAt, &p.ExpiresAt)
}

// Claim atomically spends one proposal for a caller: the single-use step.
//
// It is ONE statement, so there is no window between "is it spent?" and "mark it
// spent" — the row is both checked and consumed by the same UPDATE, and a second
// caller (or a second click) finds consumed_at already set and is refused.
//
// The classification on failure is deliberately user-scoped: a token belonging
// to another caller is looked up with the SAME user_id predicate, so it answers
// ErrChatProposalNotFound — identical to a token that was never issued. That is
// what makes a token unguessable by observation rather than merely long.
//
// Expiry is compared against the DATABASE clock (now()), not the process clock,
// so replicas with skewed clocks cannot extend a token's life.
func (s *ChatWriteProposalStore) Claim(ctx context.Context, userID, tokenHash string) (*ChatWriteProposal, error) {
	if userID == "" || tokenHash == "" {
		return nil, ErrChatProposalNotFound
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE chat_write_proposals
		    SET consumed_at = now()
		  WHERE token_hash = $1 AND user_id = $2
		    AND consumed_at IS NULL AND expires_at > now()
		  RETURNING `+chatProposalSelect, tokenHash, userID)
	p, err := scanChatProposal(row)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, ErrChatProposalNotFound) {
		return nil, err
	}
	// No row was claimed: say WHY, to the owner only.
	return nil, s.classifyMiss(ctx, userID, tokenHash)
}

// classifyMiss explains a failed Claim without widening what a caller may learn:
// the lookup keeps the user_id predicate, so a token that is not this caller's is
// still "not found".
func (s *ChatWriteProposalStore) classifyMiss(ctx context.Context, userID, tokenHash string) error {
	var consumed *time.Time
	var expires time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT consumed_at, expires_at FROM chat_write_proposals
		  WHERE token_hash = $1 AND user_id = $2`, tokenHash, userID).
		Scan(&consumed, &expires)
	if err != nil {
		return ErrChatProposalNotFound
	}
	if consumed != nil {
		return ErrChatProposalConsumed
	}
	if !expires.After(time.Now().UTC()) {
		return ErrChatProposalExpired
	}
	// The row exists, is unspent and is unexpired, yet the claim above failed.
	// That is a race with another confirm: the honest answer is "already used".
	return ErrChatProposalConsumed
}

// ByID reads one proposal by id, scoped by user id (the audit view). A proposal
// that is not this caller's is ErrChatProposalNotFound.
func (s *ChatWriteProposalStore) ByID(ctx context.Context, userID, id string) (*ChatWriteProposal, error) {
	if userID == "" || id == "" {
		return nil, ErrChatProposalNotFound
	}
	return scanChatProposal(s.pool.QueryRow(ctx,
		`SELECT `+chatProposalSelect+` FROM chat_write_proposals
		  WHERE id = $1 AND user_id = $2`, id, userID))
}

// ByToken reads one proposal by token HASH, scoped by user id. It does NOT spend
// the token: the confirm path reads first so a tampered request, an expired token
// or a mismatched argument hash is refused WITHOUT burning the user's proposal,
// and only then calls Claim.
//
// The user_id predicate is what makes another person's token indistinguishable
// from a token that was never issued — the caller cannot use this read as an
// existence oracle for somebody else's proposal.
func (s *ChatWriteProposalStore) ByToken(ctx context.Context, userID, tokenHash string) (*ChatWriteProposal, error) {
	if userID == "" || tokenHash == "" {
		return nil, ErrChatProposalNotFound
	}
	return scanChatProposal(s.pool.QueryRow(ctx,
		`SELECT `+chatProposalSelect+` FROM chat_write_proposals
		  WHERE token_hash = $1 AND user_id = $2`, tokenHash, userID))
}

// DeleteSpentBefore removes spent rows older than `before` (the retention sweep).
// It never touches an unspent proposal, however old: a pending card that a user
// has not answered is not litter, it is a decision in flight — the token simply
// expires and is then collectable by a later sweep.
func (s *ChatWriteProposalStore) DeleteSpentBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM chat_write_proposals
		  WHERE consumed_at IS NOT NULL AND consumed_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// scanChatProposal scans one row of chatProposalSelect, mapping "no row" to the
// store's not-found sentinel. Args is re-canonicalised after the JSONB round
// trip so the value the confirm executes is byte-identical to the one that was
// hashed at create time.
func scanChatProposal(row interface {
	Scan(dest ...any) error
}) (*ChatWriteProposal, error) {
	var p ChatWriteProposal
	var rawArgs []byte
	if err := row.Scan(&p.ID, &p.TokenHash, &p.UserID, &p.AccountID, &p.Tool, &rawArgs,
		&p.ArgsHash, &p.Title, &p.Summary, &p.Note, &p.CreatedAt, &p.ExpiresAt, &p.ConsumedAt); err != nil {
		if isNoSuchRow(err) {
			return nil, ErrChatProposalNotFound
		}
		return nil, err
	}
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &p.Args); err != nil {
			return nil, err
		}
	}
	if p.Args == nil {
		p.Args = map[string]any{}
	}
	return &p, nil
}

// ArgsHashMatches reports whether the hash the client echoed on confirm is the
// hash of the stored arguments. It is the "the arguments did not change between
// propose and confirm" check, done on the SERVER's copy: a client that submits a
// different argument set hashes differently and is refused before anything is
// spent.
func (p *ChatWriteProposal) ArgsHashMatches(echoed string) bool {
	if p == nil || echoed == "" {
		return false
	}
	want, err := HashArgs(p.Args)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(echoed), want) && want == p.ArgsHash
}
