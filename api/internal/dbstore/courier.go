// Courier provider connections (phase 2 of the shipping feature).
//
// This is the STORE side of internal/shipping's CourierStore: which provider each
// workspace has connected, the pickup point a courier is quoted from, the sealed
// OAuth grant of an OAuth provider, and the single-use state of a connect
// attempt. The quoting rules live in internal/shipping — this file only reads and
// writes rows.
//
// THE GRANT IS SEALED. An OAuth access/refresh token is as good as a password on
// the merchant's EasyParcel account, so it goes into `token_sealed` through the
// SAME internal/cryptoutil AES-GCM key the deployment already uses for CHIP
// secret keys, and it is only ever opened inside Load. Nothing in this file
// returns a token to a caller that renders responses, and the row type that the
// drift guard aligns (shippingCourierRow) has a TokenSealed field precisely so a
// migration cannot add a column and forget the scan — it is never put on the wire
// (see httpapi's courier status shapes).
//
// TWO PROVIDERS, TWO AUTH SHAPES. A Lalamove connection stores only configuration
// (its HMAC keys live in a runtime env file and are never copied into the
// database). An EasyParcel connection additionally holds the grant from the
// merchant's own consent, which is why `token_sealed` may be empty.
//
// SCOPING follows products/forms/shipping zones: every read is keyed on the
// account the request resolved to.
package dbstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wzul/jomform/api/internal/cryptoutil"
	"github.com/wzul/jomform/api/internal/shipping"
)

// ErrCourierNoKey: the deployment has no encryption key, so a grant cannot be
// sealed. Refusing is deliberate — the alternative is a plaintext token in the
// database, which is exactly what this design exists to avoid.
var ErrCourierNoKey = errors.New("the encryption key is not configured; a courier connection cannot be stored")

// CourierStore wraps pgx for courier connections + grants.
type CourierStore struct {
	pool *pgxpool.Pool
	// key is the AES-GCM key grants are sealed with (the deployment's existing
	// SecretsKey). A nil/empty key means no grant can be stored.
	key []byte
}

// NewCourierStore builds a CourierStore. key may be nil in a bare-mode test
// deployment; then only a provider that needs no grant (Lalamove) can be stored.
func NewCourierStore(pool *pgxpool.Pool, key []byte) *CourierStore {
	return &CourierStore{pool: pool, key: key}
}

// courierAccountSelect is the column list every courier-connection read shares.
// It is registered in migration_scan_drift_test.go, so a migration that adds a
// column to shipping_courier_accounts cannot be half-applied to one call site.
const courierAccountSelect = `SELECT id, account_id, provider, status, environment,
		account_label, origin_postcode, origin_state, origin_country,
		origin_lat, origin_lng, service_type, token_sealed,
		token_expires_at, refresh_expires_at, last_error,
		connected_at, disconnected_at
		FROM shipping_courier_accounts`

// shippingCourierRow is one shipping_courier_accounts row, flat: one exported
// field per column in courierAccountSelect, which is what the drift guard aligns.
type shippingCourierRow struct {
	ID               string
	AccountID        string
	Provider         string
	Status           string
	Environment      string
	AccountLabel     string
	OriginPostcode   string
	OriginState      string
	OriginCountry    string
	OriginLat        string
	OriginLng        string
	ServiceType      string
	TokenSealed      string
	TokenExpiresAt   *time.Time
	RefreshExpiresAt *time.Time
	LastError        string
	ConnectedAt      *time.Time
	DisconnectedAt   *time.Time
}

// scanShippingCourierRow is the full-row scan the drift guard aligns against the
// constant above.
func scanShippingCourierRow(row pgx.Row) (*shippingCourierRow, error) {
	r := &shippingCourierRow{}
	if err := row.Scan(&r.ID, &r.AccountID, &r.Provider, &r.Status, &r.Environment,
		&r.AccountLabel, &r.OriginPostcode, &r.OriginState, &r.OriginCountry,
		&r.OriginLat, &r.OriginLng, &r.ServiceType, &r.TokenSealed,
		&r.TokenExpiresAt, &r.RefreshExpiresAt, &r.LastError,
		&r.ConnectedAt, &r.DisconnectedAt); err != nil {
		return nil, err
	}
	return r, nil
}

// connectionFromRow maps a row onto the resolver's Connection type. The sealed
// grant NEVER leaves this function: the caller learns only whether one exists.
func connectionFromRow(r *shippingCourierRow) shipping.Connection {
	c := shipping.Connection{
		Provider:      r.Provider,
		Status:        r.Status,
		Environment:   r.Environment,
		AccountLabel:  r.AccountLabel,
		Origin:        shipping.Address{State: r.OriginState, Postcode: r.OriginPostcode},
		OriginCountry: r.OriginCountry,
		OriginLat:     r.OriginLat,
		OriginLng:     r.OriginLng,
		ServiceType:   r.ServiceType,
		HasGrant:      strings.TrimSpace(r.TokenSealed) != "",
		LastError:     r.LastError,
	}
	if r.TokenExpiresAt != nil {
		c.GrantExpiresAt = *r.TokenExpiresAt
	}
	if r.RefreshExpiresAt != nil {
		c.RefreshExpiresAt = *r.RefreshExpiresAt
	}
	if r.ConnectedAt != nil {
		c.ConnectedAt = *r.ConnectedAt
	}
	if r.DisconnectedAt != nil {
		c.DisconnectedAt = *r.DisconnectedAt
	}
	return c
}

// Connection returns one provider's connection for a workspace.
func (s *CourierStore) Connection(ctx context.Context, accountID, provider string) (shipping.Connection, bool, error) {
	if s == nil || s.pool == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(provider) == "" {
		return shipping.Connection{}, false, nil
	}
	row, err := scanShippingCourierRow(s.pool.QueryRow(ctx,
		courierAccountSelect+` WHERE account_id=$1 AND provider=$2`,
		strings.TrimSpace(accountID), strings.TrimSpace(provider)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shipping.Connection{}, false, nil
		}
		return shipping.Connection{}, false, err
	}
	return connectionFromRow(row), true, nil
}

// Connections returns every connection of a workspace, provider-ordered.
func (s *CourierStore) Connections(ctx context.Context, accountID string) ([]shipping.Connection, error) {
	if s == nil || s.pool == nil || strings.TrimSpace(accountID) == "" {
		return []shipping.Connection{}, nil
	}
	rows, err := s.pool.Query(ctx,
		courierAccountSelect+` WHERE account_id=$1 ORDER BY provider`, strings.TrimSpace(accountID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []shipping.Connection{}
	for rows.Next() {
		row, serr := scanShippingCourierRow(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, connectionFromRow(row))
	}
	return out, rows.Err()
}

// SaveConnection upserts a workspace's connection. It deliberately does NOT touch
// token_sealed: connecting writes configuration, and only the token half writes
// the grant (Save), so a routine origin edit can never clobber a live grant.
func (s *CourierStore) SaveConnection(ctx context.Context, accountID string, c shipping.Connection) error {
	if s == nil || s.pool == nil {
		return errors.New("courier: store is not available")
	}
	accountID = strings.TrimSpace(accountID)
	provider := strings.TrimSpace(c.Provider)
	if accountID == "" || provider == "" {
		return errors.New("courier: account_id and provider are required")
	}
	status := strings.TrimSpace(c.Status)
	if status == "" {
		status = shipping.StatusDisconnected
	}
	switch status {
	case shipping.StatusConnected, shipping.StatusDisconnected, shipping.StatusError:
	default:
		return fmt.Errorf("courier: invalid status: %s", status)
	}
	// A connect stamps connected_at (and clears a previous disconnect); any other
	// status keeps whatever was there — an origin edit on a disconnected
	// connection must not resurrect it as connected.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO shipping_courier_accounts
			(account_id, provider, status, environment, account_label,
			 origin_postcode, origin_state, origin_country, origin_lat, origin_lng,
			 service_type, connected_at, disconnected_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,
			CASE WHEN $3='connected' THEN now() ELSE NULL END,
			CASE WHEN $3='connected' THEN NULL ELSE now() END)
		ON CONFLICT (account_id, provider) DO UPDATE SET
			status=$3, environment=$4, account_label=$5,
			origin_postcode=$6, origin_state=$7, origin_country=$8,
			origin_lat=$9, origin_lng=$10, service_type=$11,
			connected_at=CASE WHEN $3='connected' THEN now()
				ELSE shipping_courier_accounts.connected_at END,
			disconnected_at=CASE WHEN $3='connected' THEN NULL ELSE now() END,
			-- A connect clears the previous error class; any other status keeps
			-- whatever the caller recorded separately (MarkError). The column is
			-- qualified: inside ON CONFLICT DO UPDATE a bare name is ambiguous
			-- between the existing row and the EXCLUDED pseudo-row (SQLSTATE 42702).
			last_error=CASE WHEN $3='connected' THEN '' ELSE shipping_courier_accounts.last_error END,
			updated_at=now()`,
		accountID, provider, status, strings.TrimSpace(c.Environment), strings.TrimSpace(c.AccountLabel),
		strings.TrimSpace(c.Origin.Postcode), strings.TrimSpace(c.Origin.State),
		firstNonEmpty(strings.TrimSpace(c.OriginCountry), "MY"),
		strings.TrimSpace(c.OriginLat), strings.TrimSpace(c.OriginLng),
		strings.ToUpper(strings.TrimSpace(c.ServiceType)))
	return err
}

// Disconnect marks a connection disconnected and DROPS its grant, so a
// disconnected provider can never be quoted with a token the merchant revoked.
// The row is kept: the pickup point is the merchant's configuration and
// reconnecting must not make them retype it.
func (s *CourierStore) Disconnect(ctx context.Context, accountID, provider string) error {
	if s == nil || s.pool == nil {
		return errors.New("courier: store is not available")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE shipping_courier_accounts SET
			status=$3, token_sealed='', token_expires_at=NULL, refresh_expires_at=NULL,
			last_error='', disconnected_at=now(), updated_at=now()
		WHERE account_id=$1 AND provider=$2`,
		strings.TrimSpace(accountID), strings.TrimSpace(provider), shipping.StatusDisconnected)
	return err
}

// IssueState creates the single-use OAuth state of one connect attempt.
func (s *CourierStore) IssueState(ctx context.Context, accountID, provider, redirectURI, createdBy string) (string, error) {
	if s == nil || s.pool == nil {
		return "", errors.New("courier: store is not available")
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b[:])
	_, err := s.pool.Exec(ctx, `
		INSERT INTO shipping_courier_states (state, account_id, provider, redirect_uri, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid)`,
		state, strings.TrimSpace(accountID), strings.TrimSpace(provider),
		strings.TrimSpace(redirectURI), strings.TrimSpace(createdBy))
	if err != nil {
		return "", err
	}
	return state, nil
}

// ConsumeState claims a state exactly once and within ttl. A state that is
// unknown, already used or expired reads as found=false — the callback then
// refuses, which is what stops a replayed consent link from re-binding a grant.
func (s *CourierStore) ConsumeState(ctx context.Context, state string, ttl time.Duration) (shipping.StateRef, bool, error) {
	if s == nil || s.pool == nil || strings.TrimSpace(state) == "" {
		return shipping.StateRef{}, false, nil
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	var ref shipping.StateRef
	err := s.pool.QueryRow(ctx, `
		UPDATE shipping_courier_states SET used_at=now()
		WHERE state=$1 AND used_at IS NULL AND created_at > now() - $2::interval
		RETURNING account_id::text, provider, redirect_uri, COALESCE(created_by::text,'')`,
		strings.TrimSpace(state), fmt.Sprintf("%d seconds", int(ttl.Seconds()))).
		Scan(&ref.AccountID, &ref.Provider, &ref.RedirectURI, &ref.CreatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shipping.StateRef{}, false, nil
		}
		return shipping.StateRef{}, false, err
	}
	return ref, true, nil
}

// courierGrant is the sealed blob's shape.
type courierGrant struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	TokenType        string    `json:"token_type"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// Save seals a grant and writes it onto the connection. It creates the
// connection row if the callback arrives before one exists (a merchant who
// authorized before the dashboard saved a pickup point), so a grant is never
// dropped on the floor.
func (s *CourierStore) Save(ctx context.Context, accountID, provider string, t shipping.TokenSet) error {
	if s == nil || s.pool == nil {
		return errors.New("courier: store is not available")
	}
	if len(s.key) == 0 {
		return ErrCourierNoKey
	}
	blob, err := json.Marshal(courierGrant{
		AccessToken:      t.AccessToken,
		RefreshToken:     t.RefreshToken,
		TokenType:        t.TokenType,
		ExpiresAt:        t.ExpiresAt,
		RefreshExpiresAt: t.RefreshExpiresAt,
	})
	if err != nil {
		return err
	}
	sealed, err := cryptoutil.Encrypt(s.key, blob)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO shipping_courier_accounts
			(account_id, provider, status, environment, token_sealed,
			 token_expires_at, refresh_expires_at, last_error, connected_at)
		VALUES ($1,$2,$3,$4,$5, NULLIF($6,'')::timestamptz, NULLIF($7,'')::timestamptz, '', now())
		ON CONFLICT (account_id, provider) DO UPDATE SET
			status=$3, environment=$4, token_sealed=$5,
			token_expires_at=NULLIF($6,'')::timestamptz,
			refresh_expires_at=NULLIF($7,'')::timestamptz,
			last_error='', connected_at=COALESCE(shipping_courier_accounts.connected_at, now()),
			disconnected_at=NULL, updated_at=now()`,
		strings.TrimSpace(accountID), strings.TrimSpace(provider), shipping.StatusConnected,
		shipping.EnvUnknown, sealed, tsOrEmpty(t.ExpiresAt), tsOrEmpty(t.RefreshExpiresAt))
	return err
}

// Load opens the sealed grant. A missing row, an empty blob or an unsealable one
// reads as found=false: a grant we cannot read is a grant we do not have, and the
// caller then reports the connection as expired rather than using garbage.
func (s *CourierStore) Load(ctx context.Context, accountID, provider string) (shipping.TokenSet, bool, error) {
	if s == nil || s.pool == nil || len(s.key) == 0 {
		return shipping.TokenSet{}, false, nil
	}
	row, err := scanShippingCourierRow(s.pool.QueryRow(ctx,
		courierAccountSelect+` WHERE account_id=$1 AND provider=$2`,
		strings.TrimSpace(accountID), strings.TrimSpace(provider)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shipping.TokenSet{}, false, nil
		}
		return shipping.TokenSet{}, false, err
	}
	if strings.TrimSpace(row.TokenSealed) == "" {
		return shipping.TokenSet{}, false, nil
	}
	plain, err := cryptoutil.Decrypt(s.key, row.TokenSealed)
	if err != nil {
		return shipping.TokenSet{}, false, nil
	}
	var g courierGrant
	if err := json.Unmarshal(plain, &g); err != nil {
		return shipping.TokenSet{}, false, nil
	}
	return shipping.TokenSet{
		AccessToken:      g.AccessToken,
		RefreshToken:     g.RefreshToken,
		TokenType:        g.TokenType,
		ExpiresAt:        g.ExpiresAt,
		RefreshExpiresAt: g.RefreshExpiresAt,
	}, true, nil
}

// MarkError records a bounded error CLASS on the connection (never a payload).
func (s *CourierStore) MarkError(ctx context.Context, accountID, provider, class string) error {
	if s == nil || s.pool == nil {
		return errors.New("courier: store is not available")
	}
	class = boundedClass(class)
	_, err := s.pool.Exec(ctx, `
		UPDATE shipping_courier_accounts SET status=$3, last_error=$4, updated_at=now()
		WHERE account_id=$1 AND provider=$2`,
		strings.TrimSpace(accountID), strings.TrimSpace(provider), shipping.StatusError, class)
	return err
}

// boundedClass keeps an error class short and alphanumeric-plus-underscore, so a
// caller that accidentally passes a vendor payload cannot store it.
func boundedClass(class string) string {
	class = strings.ToLower(strings.TrimSpace(class))
	var b strings.Builder
	for _, r := range class {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	return b.String()
}

// tsOrEmpty renders a time as an RFC3339 string, or "" for the zero time (so a
// NULL is written rather than year 1).
func tsOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// firstNonEmpty returns the first non-blank value.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
