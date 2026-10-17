// Passkeys (WebAuthn): credential storage + WebAuthn wiring.
package dbstore

import (
	"bytes"
	"context"
	"errors"
	"time"

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

// ── credential flags ────────────────────────────────────────────────────────
//
// WHY THE `flags` COLUMN IS LOAD-BEARING. go-webauthn compares the STORED
// credential's BackupEligible bit against the one in each assertion's
// authenticator data and rejects the login outright when the two disagree
// (webauthn/login.go, validateLogin: "Backup Eligible flag inconsistency
// detected during login validation"). This store used to write a hard-coded 0
// for every credential AND drop the column on the way back in, so the
// comparison was always "what the authenticator says" vs "false" — and every
// credential that reports BE=1, i.e. every SYNCED passkey (iCloud Keychain /
// Touch ID, Google Password Manager, Windows Hello), was rejected at login with
// a 401 while registration answered 200. That is the defect of card t_0a2d413c:
// the hand-built test assertion carried no BE bit, which is the one shape the
// hard-coded 0 agreed with.
//
// The octet round trip is deliberately explicit rather than a boolean per flag:
// [webauthn.CredentialFlags.ProtocolValue] is the raw authenticator-data octet
// the credential was created with, and [webauthn.NewCredentialFlags] rebuilds
// the booleans from it, so nothing here has to track which bits WebAuthn adds
// next.
//
// unrecordedCredentialFlags is the sentinel for "this column holds no recorded
// value" — the value the pre-fix Save wrote for everything. A row written by
// the current code can never carry it: go-webauthn's registration verification
// requires the UP flag (a ceremony without user presence is rejected) and AT to
// be set whenever attested credential data is parsed, so a stored octet is at
// least 0x41. A 0 therefore identifies a row from before flags were persisted,
// which is what [PasskeyUser.RestoreUnrecordedFlags] keys on.
const unrecordedCredentialFlags = 0

// credentialFlagsColumn encodes the credential's flags for the `flags` INT
// column. Only the low octet is ever written: the column type is INT, but the
// value comes from [protocol.AuthenticatorFlags], which the WebAuthn spec
// defines as a single byte.
func credentialFlagsColumn(f webauthn.CredentialFlags) int {
	return int(f.ProtocolValue())
}

// columnCredentialFlags is the inverse of credentialFlagsColumn.
func columnCredentialFlags(v int) webauthn.CredentialFlags {
	return webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(byte(v)))
}

// Save inserts or updates a credential.
//
// The AAGUID is written as the 16 raw bytes it is. It used to be written with
// string(c.Authenticator.AAGUID) — the raw bytes reinterpreted as text — which
// Postgres refuses (SQLSTATE 22021, `invalid byte sequence for encoding "UTF8"`)
// for every REAL authenticator: Chrome's AAGUID starts with 0xad, Google
// Password Manager's with 0x9d, Windows Hello's with 0x98, and even the
// all-zero AAGUID the spec defines for none-attestation is rejected. Only an
// all-ASCII AAGUID — i.e. a test virtual authenticator — could be stored, which
// is why this stayed hidden. Passing the bytes is required by the column type
// too: aaguid is BYTEA as of 000075, and a Go string sent as a text-format
// parameter fails the same way.
//
// The credential's flags are written too, and re-written on conflict: they are
// the only record of the authenticator's BackupEligible bit, which the login
// ceremony compares against every assertion (see the flags section above).
func (s *PasskeyStore) Save(ctx context.Context, userID string, c webauthn.Credential) error {
	// A credential whose authenticator attested no AAGUID — a hand-built one, or
	// the spec's all-zero value reduced to a nil slice — stores the empty byte
	// string, never NULL: the column is NOT NULL DEFAULT '\x', and the
	// string-typed write this replaced stored "" for the same case.
	aaguid := c.Authenticator.AAGUID
	if aaguid == nil {
		aaguid = []byte{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkeys (id, user_id, public_key, attestation, transport, flags, sign_count, aaguid)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (id) DO UPDATE SET public_key=EXCLUDED.public_key, sign_count=EXCLUDED.sign_count, flags=EXCLUDED.flags`,
		c.ID, userID, c.PublicKey, c.AttestationType,
		joinTransport(c.Transport), credentialFlagsColumn(c.Flags), c.Authenticator.SignCount, aaguid)
	return err
}

// RecordAssertion writes back the credential state a SUCCESSFUL assertion
// advanced: the signature counter and the flags.
//
// go-webauthn's storage contract requires both on every successful login (see
// the [Storage] section of the webauthn package docs): BackupState can change
// between ceremonies and UserVerified is a latch that only ever advances, while
// the counter is what a clone warning is derived from. It is also what makes a
// repaired legacy row stay repaired — once the adopted octet is stored, the
// BackupEligible consistency check applies to that credential from the next
// ceremony on.
//
// [Storage]: https://pkg.go.dev/github.com/go-webauthn/webauthn/webauthn#hdr-Storage
func (s *PasskeyStore) RecordAssertion(ctx context.Context, userID string, c webauthn.Credential) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE passkeys SET sign_count=$3, flags=$4 WHERE id=$1 AND user_id=$2`,
		c.ID, userID, c.Authenticator.SignCount, credentialFlagsColumn(c.Flags))
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
			// The flags MUST come back: the login ceremony compares the stored
			// BackupEligible bit against every assertion's (see the flags
			// section above). Dropping them here is what made a registered
			// synced passkey unusable while the hand-built test, whose
			// assertion carried no BE bit, stayed green.
			Flags:         columnCredentialFlags(flags),
			Authenticator: webauthn.Authenticator{SignCount: sc, AAGUID: []byte(aaguid)},
		})
		_ = tr
	}
	return out, rows.Err()
}

// PasskeyListRow is one row of a passkey LIST: the credential id plus the
// timestamp the row was created with.
//
// The timestamp is a second query rather than a field on
// [webauthn.Credential], which has no place for it, and [PasskeyStore.ByUser]
// does not read the column. That gap is card t_96c7c227: `GET
// /v1/auth/passkeys` declared a `created_at` in every row and could never fill
// it, so the public response carried `"created_at": ""` for every credential —
// while the column itself is `TIMESTAMPTZ NOT NULL DEFAULT now()` and always
// has the value. The LIST endpoints read it through here.
type PasskeyListRow struct {
	ID        []byte
	CreatedAt time.Time
}

// ListCreatedAt returns the user's credentials with the `created_at` the row
// holds, for the LIST surfaces that report when a passkey was registered
// (REST `GET /v1/auth/passkeys`, MCP `list_passkeys`).
//
// A zero CreatedAt here means the column was not selected — the defect this
// method replaced — not a row without a timestamp: the column is NOT NULL with
// a default, so every stored credential has one.
func (s *PasskeyStore) ListCreatedAt(ctx context.Context, userID string) ([]PasskeyListRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, created_at FROM passkeys WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PasskeyListRow
	for rows.Next() {
		var r PasskeyListRow
		if err := rows.Scan(&r.ID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RestoreUnrecordedFlags fills in the flags of a credential whose row predates
// flag persistence, adopting what the assertion reports, and reports whether it
// did so.
//
// It exists because such a row cannot simply be rejected. Before the fix EVERY
// row held flags = 0, so every already-registered synced passkey would keep
// failing the BackupEligible consistency check with no way back except deleting
// the passkey and registering it again — the same dead end, one layer out.
//
// The sentinel is what keeps the repair from weakening the check rather than
// bypassing it: [unrecordedCredentialFlags] is not a value a recorded
// credential can hold, so for such a row there is no stored state to disagree
// with — the comparison was being made against a value that never came from the
// authenticator. A row that carries a recorded octet is untouched here, and
// go-webauthn still rejects a changed BackupEligible flag for it. The handler
// persists the adopted octet after the login ([PasskeyStore.RecordAssertion]),
// so a credential is repaired at most once and the strict check applies from
// the next ceremony on.
func (u *PasskeyUser) RestoreUnrecordedFlags(credID []byte, flags protocol.AuthenticatorFlags) bool {
	for i := range u.Creds {
		if !bytes.Equal(u.Creds[i].ID, credID) {
			continue
		}
		if credentialFlagsColumn(u.Creds[i].Flags) != unrecordedCredentialFlags {
			return false
		}
		u.Creds[i].Flags = webauthn.NewCredentialFlags(flags)
		return true
	}
	return false
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
//
// CARD t_77dc59f5. The expired rows are swept ON THE WAY IN, and that placement
// is the point: this INSERT was the table's only writer and TakeChallenge its
// only deleter, so a ceremony the user ABANDONED — the passkey prompt dismissed,
// the tab closed, the phone never answered — left its row on disk forever. This
// is not a theoretical leak, it is a measurement: `passkey_challenges` on prod
// held 82 rows with ZERO of them live, the newest expiry 3h20m old, every one of
// them a stored server-side ceremony that has outlived its own 5-minute
// contract, plus the index entry the planner still has to step over.
//
// What makes the sweep safe to run here is the consume predicate: TakeChallenge
// only ever removes a row with `expires_at > now()`, so a row this deletes is by
// definition one no /finish can use — there is no ceremony to race. A LIVE row
// is left alone (the DELETE carries the expiry predicate, it is not a wipe), and
// the new row is inserted AFTER the delete, never before it, so the sweep cannot
// take back the challenge it just minted.
//
// A deployment that stops serving ceremonies would still keep its last row, so
// the API's hourly retention sweep calls PruneExpiredChallenges too — see
// cmd/jomform-api. The two together are the fix: rows that accumulate while the
// product is in use, and the rows left by the last ceremony before it went quiet.
func (s *PasskeyStore) PutChallenge(ctx context.Context, key, value, kind string) error {
	if _, err := s.PruneExpiredChallenges(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO passkey_challenges (key, value, kind, expires_at) VALUES ($1,$2,$3, now()+interval '5 minutes')
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, kind=EXCLUDED.kind, expires_at=now()+interval '5 minutes'`,
		key, value, kind)
	return err
}

// PruneExpiredChallenges deletes every challenge whose TTL has passed and
// reports how many rows went. See PutChallenge (card t_77dc59f5) for why the
// table needed it and why an expired row is safe to remove.
//
// It walks `idx_passkey_challenges_exp`, so with nothing expired it is an index
// probe that locks no row at all. The error is returned rather than swallowed:
// a prune failure nobody can see is the shape that let 82 dead rows accumulate
// unnoticed, and the next ceremony (or the next hourly tick) retries it.
func (s *PasskeyStore) PruneExpiredChallenges(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM passkey_challenges WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
