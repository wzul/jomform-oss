// Passkeys (WebAuthn): credential storage + WebAuthn wiring.
package dbstore

import (
	"context"
	"errors"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PasskeyUser adapts a user for go-webauthn.
type PasskeyUser struct {
	ID    string
	Email string
	Creds []webauthn.Credential
}

func (u *PasskeyUser) WebAuthnID() []byte          { return []byte(u.ID) }
func (u *PasskeyUser) WebAuthnName() string        { return u.Email }
func (u *PasskeyUser) WebAuthnDisplayName() string { return u.Email }
func (u *PasskeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Creds
}
func (u *PasskeyUser) WebAuthnIcon() string { return "" }

// errNoChallenge marks an expired/unknown challenge.
var errNoChallenge = errors.New("challenge tidak sah")

// PasskeyStore wraps passkeys + challenge storage.
type PasskeyStore struct {
	pool *pgxpool.Pool
}

func NewPasskeyStore(pool *pgxpool.Pool) *PasskeyStore {
	return &PasskeyStore{pool: pool}
}

// Save inserts or updates a credential.
func (s *PasskeyStore) Save(ctx context.Context, userID string, c webauthn.Credential) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkeys (id, user_id, public_key, attestation, transport, flags, sign_count, aaguid)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (id) DO UPDATE SET public_key=EXCLUDED.public_key, sign_count=EXCLUDED.sign_count`,
		c.ID, userID, c.PublicKey, c.AttestationType,
		joinTransport(c.Transport), 0, c.Authenticator.SignCount, string(c.Authenticator.AAGUID))
	return err
}

func joinTransport(t []protocol.AuthenticatorTransport) string {
	out := ""
	for i, v := range t {
		if i > 0 {
			out += ","
		}
		out += string(v)
	}
	return out
}

// ByUser lists credentials for a user.
func (s *PasskeyStore) ByUser(ctx context.Context, userID string) ([]webauthn.Credential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, public_key, attestation, transport, flags, sign_count, aaguid FROM passkeys WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []webauthn.Credential
	for rows.Next() {
		var id, pk []byte
		var att, tr string
		var flags int
		var sc uint32
		var aaguid []byte
		if err := rows.Scan(&id, &pk, &att, &tr, &flags, &sc, &aaguid); err != nil {
			return nil, err
		}
		out = append(out, webauthn.Credential{
			ID:              id,
			PublicKey:       pk,
			AttestationType: att,
			Transport:       []protocol.AuthenticatorTransport{},
			Authenticator:   webauthn.Authenticator{SignCount: sc, AAGUID: []byte(aaguid)},
		})
		_ = tr
	}
	return out, rows.Err()
}

// UserFor loads the WebAuthn user (with credentials) by id.
func (s *PasskeyStore) UserFor(ctx context.Context, userID, email string) (*PasskeyUser, error) {
	creds, err := s.ByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &PasskeyUser{ID: userID, Email: email, Creds: creds}, nil
}

// PutChallenge stores a one-shot challenge (5 min TTL).
func (s *PasskeyStore) PutChallenge(ctx context.Context, key, value, kind string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkey_challenges (key, value, kind, expires_at) VALUES ($1,$2,$3, now()+interval '5 minutes')
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, kind=EXCLUDED.kind, expires_at=now()+interval '5 minutes'`,
		key, value, kind)
	return err
}

// TakeChallenge reads+deletes a challenge (single use).
func (s *PasskeyStore) TakeChallenge(ctx context.Context, key, kind string) (string, error) {
	var value string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM passkey_challenges WHERE key=$1 AND kind=$2 AND expires_at > now() RETURNING value`,
		key, kind).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", errNoChallenge
	}
	return value, err
}

// Delete removes a credential.
func (s *PasskeyStore) Delete(ctx context.Context, userID string, credID []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM passkeys WHERE id=$1 AND user_id=$2`, credID, userID)
	return err
}
