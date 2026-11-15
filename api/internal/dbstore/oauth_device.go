// RFC 8628 device authorisation (card t_9a6cc85f) and the "Connected apps"
// token inventory.
//
// The device grant exists so a headless AI harness can onboard WITHOUT a
// browser: the client asks for a device_code, the AUTHORIZATION SERVER mails a
// user_code to the account's registered address, and the harness polls
// /oauth/token until a person approved it. That makes the email the consent
// screen, so everything the consent covers (client, scope, requester) is
// recorded on the row the mail is built from.
//
// Both secrets are stored as SHA-256 hashes, exactly like the authorization
// codes and tokens beside them: a dump of this table must not yield a session.
package dbstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wzul/jomform/api/internal/oauthmeta"
	"github.com/wzul/jomform/api/internal/statemachine"
)

// DeviceAuthorisation errors. The token endpoint maps them onto the RFC 8628
// §3.5 error codes, so they are deliberately named after the states they
// describe rather than after the HTTP surface.
var (
	// ErrDeviceCodeInvalid: unknown, already consumed, or wrong principal.
	ErrDeviceCodeInvalid = errors.New("device code is invalid or already used")
	// ErrDevicePending: not approved (or denied) yet.
	ErrDevicePending = errors.New("authorization_pending")
	// ErrDeviceDenied: the person said no.
	ErrDeviceDenied = errors.New("access_denied")
	// ErrDeviceExpired: past expires_at.
	ErrDeviceExpired = errors.New("expired_token")
	// ErrDeviceSlowDown: polled faster than interval_s; the interval has been
	// penalised by 5s for the rest of the authorisation.
	ErrDeviceSlowDown = errors.New("slow_down")
	// ErrDeviceScopeUnsupported: the approval named a scope this deployment
	// cannot issue. Distinct from ErrDeviceCodeInvalid because the code is fine
	// and the person may simply pick again — answering "this code is not valid"
	// would tell them to start over for a one-click mistake.
	ErrDeviceScopeUnsupported = errors.New("the chosen scope is not one this server can issue")
	// ErrOAuthTokenMissing: no live token row with that id for this user.
	ErrOAuthTokenMissing = errors.New("oauth token not found")
)

// OAuthDeviceCode models one in-flight device authorisation (hashes stored).
type OAuthDeviceCode struct {
	DeviceHash   string
	UserCodeHash string
	ClientID     string
	UserID       string
	AccountID    string
	Scope        []string
	IP           string
	UserAgent    string
	ExpiresAt    time.Time
	IntervalS    int
	LastPolledAt *time.Time
	ApprovedAt   *time.Time
	DeniedAt     *time.Time
	ConsumedAt   *time.Time
	CreatedAt    time.Time
}

// Facts renders the row's state-bearing columns for the ONE derivation
// (statemachine.DeviceStateOf). Every reader goes through this — the token
// endpoint, the consent card and the MCP surface — so the screen and the token
// endpoint cannot disagree about what state a code is in (card t_c6ca84a7).
//
// last_polled_at is not a fact: it records that a client asked, never that
// anything was decided.
func (d *OAuthDeviceCode) Facts() statemachine.DeviceFacts {
	return statemachine.DeviceFacts{
		ExpiresAt:  d.ExpiresAt,
		ApprovedAt: d.ApprovedAt,
		DeniedAt:   d.DeniedAt,
		ConsumedAt: d.ConsumedAt,
	}
}

// StateAt derives the code's state at `now`. Reads take an explicit clock so a
// test can place the row on either side of its deadline without sleeping.
func (d *OAuthDeviceCode) StateAt(now time.Time) statemachine.DeviceState {
	return statemachine.DeviceStateOf(d.Facts(), now)
}

// State is StateAt(time.Now()), for readers that run on a wall clock.
func (d *OAuthDeviceCode) State() statemachine.DeviceState {
	return d.StateAt(time.Now().UTC())
}

// Status is the agent-readable answer to "is this code still usable": state,
// usable, remaining seconds and the events the table still allows.
func (d *OAuthDeviceCode) Status(now time.Time) statemachine.DeviceStatus {
	return statemachine.DeviceStatusOf(d.Facts(), now)
}

// deviceErrFor is the ONE place a state-machine answer becomes a store error.
// The error values are the token endpoint's RFC 8628 vocabulary and are
// unchanged by card t_c6ca84a7 — only their source moved, from four hand-written
// switches to the table in statemachine.
func deviceErrFor(reason statemachine.DeviceReason) error {
	switch reason {
	case statemachine.DeviceReasonNone:
		return nil
	case statemachine.DeviceReasonAuthorizationPending:
		return ErrDevicePending
	case statemachine.DeviceReasonAccessDenied:
		return ErrDeviceDenied
	case statemachine.DeviceReasonExpiredToken:
		return ErrDeviceExpired
	default:
		return ErrDeviceCodeInvalid
	}
}

// CreateDeviceCode persists a new device authorisation. The row is created
// BEFORE any mail is sent (see httpapi/oauth_device.go): the mail carries the
// user_code of a row that already exists, so a failed send leaves an
// authorisation nobody can approve rather than a code that resolves to nothing.
//
// It also prunes rows whose 10-minute life ended more than a day ago. That is a
// best-effort DELETE on an indexed time column — this table is written once per
// onboarding attempt and must not grow forever.
func (s *OAuthStore) CreateDeviceCode(ctx context.Context, d *OAuthDeviceCode) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM oauth_device_codes WHERE expires_at < now() - interval '1 day'`); err != nil {
		// Pruning is housekeeping; never fail an authorisation on it.
		_ = err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_device_codes
		 (device_hash, user_code_hash, client_id, user_id, account_id, scope, ip, user_agent,
		  expires_at, interval_s)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10)`,
		d.DeviceHash, d.UserCodeHash, d.ClientID, d.UserID, d.AccountID, d.Scope,
		d.IP, d.UserAgent, d.ExpiresAt, d.IntervalS)
	return err
}

// CountDeviceCodesByIP returns how many authorisations this address started
// since `since` — the per-IP half of the hourly rate limit that keeps the
// email path from becoming a mail bomb.
func (s *OAuthStore) CountDeviceCodesByIP(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_device_codes WHERE ip = $1 AND created_at >= $2`,
		ip, since).Scan(&n)
	return n, err
}

// CountDeviceCodesByUser returns how many authorisations this account started
// since `since` — the per-account half of the hourly rate limit.
func (s *OAuthStore) CountDeviceCodesByUser(ctx context.Context, userID string, since time.Time) (int, error) {
	if userID == "" {
		return 0, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_device_codes WHERE user_id = $1 AND created_at >= $2`,
		userID, since).Scan(&n)
	return n, err
}

// deviceSelectColumnList is the ONE column list every device-code read scans.
// Keeping it in a constant next to the struct means a migration that adds a
// column has exactly one place to update (the same rule the *Select constants
// follow for the account/form/product tables).
const deviceSelectColumnList = `device_hash, user_code_hash, client_id,
	COALESCE(user_id::text,''), COALESCE(account_id::text,''), scope, ip, user_agent,
	expires_at, interval_s, last_polled_at, approved_at, denied_at, consumed_at, created_at`

func scanDeviceCode(row pgx.Row) (*OAuthDeviceCode, error) {
	d := &OAuthDeviceCode{}
	err := row.Scan(&d.DeviceHash, &d.UserCodeHash, &d.ClientID, &d.UserID, &d.AccountID,
		&d.Scope, &d.IP, &d.UserAgent, &d.ExpiresAt, &d.IntervalS,
		&d.LastPolledAt, &d.ApprovedAt, &d.DeniedAt, &d.ConsumedAt, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDeviceCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// DeviceCodeByUserCode resolves the authorisation a person was mailed, for the
// dashboard's approve/deny card. It is a plain read: whether the row is still
// approvable is decided by ApproveDeviceCode/DenyDeviceCode, which lock it.
func (s *OAuthStore) DeviceCodeByUserCode(ctx context.Context, userCodeHash string) (*OAuthDeviceCode, error) {
	return scanDeviceCode(s.pool.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes WHERE user_code_hash = $1`,
		userCodeHash))
}

// ApproveDeviceCode records consent for a device authorisation after the
// person confirmed it on jomform.com (the email's link lands there).
//
// The person must be the one the mail was sent to: when the row already names a
// user, only that user may approve it. An unbound row (no login_hint, or a
// login_hint that matched nobody) is bound to the approving session here —
// that is the RFC 8628 "type the code at the verification URI" path.
//
// `scope` is the HUMAN'S CHOICE, not a narrowing of the client's request. The
// consent card offers both scopes in either direction (card t_cf45b84c): the
// request is only the default the card opens on, so an approval may widen as
// well as narrow. What it may NOT do is name something this deployment cannot
// issue, and that is re-checked here because this is the last write before a
// token exists — the caller validating it first is not a reason for the store to
// trust it.
//
// WHETHER APPROVAL IS LEGAL IS THE TABLE'S ANSWER (card t_c6ca84a7), not a
// switch here: approving twice, approving a denied row and approving an expired
// one are three cells of statemachine.DeviceTransition, and the same table is
// what the consent card reads when it decides whether to draw the buttons.
func (s *OAuthStore) ApproveDeviceCode(ctx context.Context, userCodeHash, userID, accountID string, scope []string) (*OAuthDeviceCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDeviceCode(tx.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes
		 WHERE user_code_hash = $1 FOR UPDATE`, userCodeHash))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if step := statemachine.DeviceApproveStep(d.Facts(), now); !step.Allowed {
		return nil, deviceErrFor(step.Answer)
	}
	if d.UserID != "" && d.UserID != userID {
		// Someone else's authorisation: indistinguishable from unknown.
		return nil, ErrDeviceCodeInvalid
	}
	if !issuableScope(scope) {
		return nil, ErrDeviceScopeUnsupported
	}
	if _, err := tx.Exec(ctx,
		`UPDATE oauth_device_codes
		 SET approved_at = now(), user_id = $2, account_id = NULLIF($3,'')::uuid, scope = $4
		 WHERE user_code_hash = $1`,
		userCodeHash, userID, accountID, scope); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	d.UserID, d.AccountID, d.Scope = userID, accountID, scope
	d.ApprovedAt = &now
	return d, nil
}

// DenyDeviceCode records a refusal. The token endpoint then answers
// access_denied (RFC 8628 §3.5) instead of polling forever.
//
// The row is locked and the decision is the table's, exactly like
// ApproveDeviceCode — the two are now mutually exclusive by the same mechanism
// instead of by one SQL predicate here and a Go switch there (card t_c6ca84a7).
func (s *OAuthStore) DenyDeviceCode(ctx context.Context, userCodeHash, userID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDeviceCode(tx.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes
		 WHERE user_code_hash = $1 FOR UPDATE`, userCodeHash))
	if err != nil {
		return err
	}
	if step := statemachine.DeviceTransition(d.StateAt(time.Now().UTC()), statemachine.DeviceEventDeny); !step.Allowed {
		// FROZEN ANSWER (card t_c6ca84a7): a denial that cannot be recorded
		// has always been ErrDeviceCodeInvalid, whatever the reason — the
		// table decides whether the row may be denied, not what this one
		// error value says. The dashboard maps it to 404 "not valid" and the
		// reason a person sees is the state on the card, which is derived
		// once and is never this error.
		return ErrDeviceCodeInvalid
	}
	if d.UserID != "" && d.UserID != userID {
		return ErrDeviceCodeInvalid
	}
	tag, err := tx.Exec(ctx,
		`UPDATE oauth_device_codes SET denied_at = now()
		 WHERE user_code_hash = $1 AND denied_at IS NULL`, userCodeHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		// Belt on top of the lock: never report a denial that did not land.
		return ErrDeviceCodeInvalid
	}
	return tx.Commit(ctx)
}

// PollDeviceCode is the token endpoint's side of RFC 8628 §3.4/§3.5. It runs
// under a row lock so the states cannot race each other, and it enforces every
// rule the RFC gives the server, in the RFC's order:
//
//	unknown/consumed → ErrDeviceCodeInvalid
//	denied           → ErrDeviceDenied   (access_denied)
//	expired          → ErrDeviceExpired  (expired_token)
//	polled too fast  → ErrDeviceSlowDown (slow_down, +5s penalty, persisted)
//	not approved yet → ErrDevicePending  (authorization_pending)
//	approved         → CONSUMED HERE, row returned for token minting
//
// Consumption is part of the same locked transaction, so two polls of one
// approved code cannot both mint a token: the second finds consumed_at set.
//
// WHICH OF THOSE HAPPENS IS NOT DECIDED HERE ANY MORE (card t_c6ca84a7). The
// row's state comes from statemachine.DeviceStateOf, the event from
// DeviceTokenEvent, and the answer from the table — the same three calls the
// consent card makes, so the two cannot disagree about whether a code is
// approved or expired.
//
// THE ROW COMES BACK EVEN WHEN THE ANSWER IS A REFUSAL that is about the code
// itself (pending, slow_down, denied, expired, already spent). The token
// endpoint uses it to report the state and the seconds left instead of a bare
// 400. Only an UNKNOWN code and a WRONG client_id return nil — the two cases
// where there is no code to describe and saying anything would turn the endpoint
// into an oracle. (A spent code is returned: the caller has already proved it
// holds it. Its refusal is still ErrDeviceCodeInvalid, unchanged.)
func (s *OAuthStore) PollDeviceCode(ctx context.Context, deviceHash, clientID string) (*OAuthDeviceCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDeviceCode(tx.QueryRow(ctx,
		`SELECT `+deviceSelectColumnList+` FROM oauth_device_codes
		 WHERE device_hash = $1 FOR UPDATE`, deviceHash))
	if err != nil {
		return nil, err
	}
	// RFC 8628 §3.4: the token request repeats client_id. A mismatch must not
	// burn the code, so it is checked before anything is written.
	if clientID != "" && d.ClientID != clientID {
		return nil, ErrDeviceCodeInvalid
	}
	now := time.Now().UTC()
	state := d.StateAt(now)
	step := statemachine.DeviceTransition(state, statemachine.DeviceTokenEvent(state))
	if !step.Allowed {
		return d, deviceErrFor(step.Answer)
	}
	if d.LastPolledAt != nil {
		interval := time.Duration(d.IntervalS) * time.Second
		if now.Sub(*d.LastPolledAt) < interval {
			// RFC 8628 §3.5: answer slow_down AND add 5 seconds to the
			// interval for this and all subsequent polls.
			if _, err := tx.Exec(ctx,
				`UPDATE oauth_device_codes SET interval_s = interval_s + 5, last_polled_at = now()
				 WHERE device_hash = $1`, deviceHash); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			d.IntervalS += 5
			d.LastPolledAt = &now
			return d, ErrDeviceSlowDown
		}
	}
	if step.Answer == statemachine.DeviceReasonAuthorizationPending {
		// Pending: the poll is legal, it records the ask and nothing else.
		if _, err := tx.Exec(ctx,
			`UPDATE oauth_device_codes SET last_polled_at = now() WHERE device_hash = $1`,
			deviceHash); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		d.LastPolledAt = &now
		return d, ErrDevicePending
	}
	// Approved: consume exactly once, inside the same locked transaction.
	tag, err := tx.Exec(ctx,
		`UPDATE oauth_device_codes SET consumed_at = now()
		 WHERE device_hash = $1 AND consumed_at IS NULL`, deviceHash)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrDeviceCodeInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	d.ConsumedAt = &now
	d.LastPolledAt = &now
	return d, nil
}

// issuableScope reports whether `scope` is a set this deployment can issue: one
// or more advertised values and nothing else. It replaces the old
// "must be a subset of the client's request" rule, because at the consent card
// the PERSON decides the scope and the request is only the default the card
// opens on (card t_cf45b84c) — widening at approval is the point, so the request
// is no longer a ceiling. The vocabulary check stays here: an approval that
// names a scope this server cannot honour must fail before anything is written.
func issuableScope(scope []string) bool {
	if len(scope) == 0 {
		return false
	}
	for _, s := range scope {
		ok := false
		for _, supported := range oauthmeta.ScopesSupported() {
			if s == supported {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// ─── Connected apps ──────────────────────────────────────────────────────────

// ConnectedApp, ConnectedAppsByUser, RotateTokenScope and the token-pair state
// wiring live in oauth_connected_apps.go (card t_df243a6e): this file is the
// device flow, and a change to one aggregate must not be a merge conflict in the
// other.

// RevokeTokenByIDForUser revokes ONE token pair the user owns. Access and
// refresh live on the same row, so revoking it kills both in a single UPDATE —
// which is what the Connected-apps "Revoke" button promises.
//
// The user_id predicate is the authorization: an id belonging to somebody else
// matches nothing and reads back as ErrOAuthTokenMissing. `revoked_at IS NULL` is
// the statement's own idempotency guard, not a re-derivation of the pair's state:
// the answer is a frozen 404 either way, so there is nothing for the machine to
// say (card t_df243a6e).
func (s *OAuthStore) RevokeTokenByIDForUser(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE oauth_tokens SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOAuthTokenMissing
	}
	return nil
}

// RotateTokenScope lives in oauth_connected_apps.go — it is the dashboard's
// connected-app write, and card t_df243a6e moved it next to the reader whose
// answer gates it.

// ScopeString joins a scope set the way RFC 6749 §3.3 spells it, for audit
// payloads. Local to this package because every other caller already has the
// slice it wants to print.
func ScopeString(scope []string) string { return strings.Join(scope, " ") }
