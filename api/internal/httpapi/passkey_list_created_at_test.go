package httpapi

// t_96c7c227 — GET /v1/auth/passkeys declared a `created_at` in every row and
// never filled it.
//
// THE DEFECT, as measured on prod (v1.21.136, 2026-10-14): a genuine
// registration through the live endpoints followed by
// `GET https://api.jomform.com/v1/auth/passkeys` with a real session cookie
// answered `[{"id":"oKhz0kXvqKqiLulrCRZcjgW20VKlpM6QzYsfjWp1yJk","created_at":""}]`.
// The handler's row type has always carried the field, but nothing ever wrote
// it: the store read `id, public_key, attestation, transport, flags,
// sign_count, aaguid` and no timestamp, and the loop built each row with the id
// alone. The value was never missing from the database — `passkeys.created_at`
// is `TIMESTAMPTZ NOT NULL DEFAULT now()`, so every credential has one — only
// from the response.
//
// WHAT IS ASSERTED HERE, and why each arm is needed:
//
//   - The response goes through the ROUTER (`h.RegisterRoutes` + a real session
//     cookie from the product's own password login), so the guard covers the
//     registered route, the `authed` wrapper, the handler and the store. A
//     store-level test cannot see this class of defect at all: the timestamp
//     was readable and the field was declared, and the row the customer read
//     was still empty.
//
//   - The rows are decoded as raw messages rather than into a typed struct,
//     because "the key is absent", "the value is null" and "the value is
//     filled" all unmarshal into the same zero string. The endpoint's contract
//     is the JSON, so the JSON is what is read.
//
//   - `created_at` must be non-empty, must parse as RFC 3339, and must equal
//     the `created_at` COLUMN of that credential's row (read straight out of
//     Postgres, to the second the response is formatted at). Against the
//     pre-fix handler this arm fails on the empty string; a handler that
//     invented a timestamp instead of reading the row's fails the equality arm.
//
//   - The fixture row is BACKDATED seven days before the request (t_ef0080c6).
//     It used to be inserted with the column's DEFAULT now(), so a handler that
//     formatted time.Now() at read time landed in the SAME SECOND as the row
//     and passed this arm. Measured: replacing `CreatedAt: c.CreatedAt…` with
//     `CreatedAt: time.Now()…` in passkeyList left this test GREEN, and only a
//     +48h control reddened it — exactly the regression the release exists to
//     prevent, invisible to its own guard. A row a week older than the clock
//     cannot be confused with a clock re-read at request time, so the arm now
//     fails for a re-now() handler AND for a zero time (0001-01-01T00:00:00Z,
//     the "column never selected" shape), and passes only for the row's own
//     value. The backdate is asserted below, so the fixture cannot quietly
//     drift back into a `DEFAULT now()` row and re-vacuum the arm.
//
//   - The returned id SET must equal the credentials of THIS user. The list is
//     the one endpoint that hands a caller their own rows, and the query behind
//     it is new, so the arm pins that its `user_id` filter is really there: a
//     list that forgot it would answer with another tenant's passkeys while
//     every assertion above stayed green (the fixture user's own row would
//     still be in the result, with a correct timestamp).
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/wzul/jomform/api/internal/dbstore"
)

// TestPasskeyListCarriesTheStoredCreatedAt is the regression guard for the
// card: a stored passkey's row must come back from GET /v1/auth/passkeys with
// the timestamp the row holds.
//
// Neutralise the fix — drop the `CreatedAt` assignment in passkeyList, or stop
// selecting the column in PasskeyStore.ListCreatedAt — and this test goes red
// on the empty value, while every pre-existing passkey test stays green,
// because none of them reads the list endpoint at all.
func TestPasskeyListCarriesTheStoredCreatedAt(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)

	ah := authFixtureWithWorkspaces(t)
	user, _, _ := legacyLoginFixture(t, ah, 0)

	// The row the product's own registration writes: a client-side discoverable
	// credential with the flags a synced passkey reports and the non-ASCII
	// AAGUID a real authenticator attests (see nonASCIIProdAAGUID), so the
	// fixture is not a shape no registration produces.
	authn, cose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, user, webauthn.Credential{
		ID:            authn.id,
		PublicKey:     cose,
		Flags:         webauthn.NewCredentialFlags(realPasskeyRegistrationFlags),
		Authenticator: webauthn.Authenticator{AAGUID: nonASCIIProdAAGUID},
	}); err != nil {
		t.Fatalf("store the fixture passkey: %v", err)
	}
	t.Cleanup(func() {
		// The test database is shared and long-lived; leave only the fixture
		// user behind, as the sibling passkey tests do.
		_, _ = pool.Exec(context.Background(), `DELETE FROM passkeys WHERE id=$1`, authn.id)
	})

	// A SECOND tenant, with a credential of its own. Without a stranger's row
	// in the table the leak arm below could only pass vacuously — there would be
	// nothing for a list that lost its user_id filter to leak.
	_, otherUser := bundleFixture(t, ctx, pool)
	otherAuthn, otherCose := newPasskeyTestAuthenticator(t)
	if err := dbstore.NewPasskeyStore(pool).Save(ctx, otherUser, webauthn.Credential{
		ID:            otherAuthn.id,
		PublicKey:     otherCose,
		Flags:         webauthn.NewCredentialFlags(realPasskeyRegistrationFlags),
		Authenticator: webauthn.Authenticator{AAGUID: nonASCIIProdAAGUID},
	}); err != nil {
		t.Fatalf("store another user's passkey: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM passkeys WHERE id=$1`, otherAuthn.id)
	})

	// ── the timestamp under test ────────────────────────────────────────────
	// Backdate the row seven days. The column defaults to now(), so a fixture
	// inserted a moment ago sits in the same second as a handler that formatted
	// the clock at read time — the arm below then passes for exactly the
	// regression it exists to catch (t_ef0080c6: measured green with a re-now()
	// handler, red only with a +48h control).
	if _, err := pool.Exec(ctx, `UPDATE passkeys SET created_at = now() - interval '7 days' WHERE id=$1`, authn.id); err != nil {
		t.Fatalf("backdate the fixture row: %v", err)
	}

	// The row's own timestamp, read from the column the response must report.
	var storedCreatedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM passkeys WHERE id=$1`, authn.id).
		Scan(&storedCreatedAt); err != nil {
		t.Fatalf("read the fixture row's created_at: %v", err)
	}
	// Guard the guard: if the backdate ever stops taking effect the equality arm
	// silently returns to "same second as the clock" and proves nothing. Fail
	// loudly here instead, where the cause is obvious.
	if age := time.Since(storedCreatedAt); age < 6*24*time.Hour {
		t.Fatalf("the fixture row's created_at is %s (%.1fh old), not the backdated value — without a row the "+
			"clock cannot reach, the equality arm below cannot tell the row's timestamp from a handler that "+
			"re-read time.Now()", storedCreatedAt.UTC().Format(time.RFC3339), age.Hours())
	}

	// A genuine session for the fixture user, then the list through the router.
	session := passkeyPasswordSession(t, ah, pool, user)
	h := passkeyHandlers(t, pool)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "http://"+passkeyOriginHost+"/v1/auth/passkeys", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	setTestClientAddr(req, uniqueTestIP())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/auth/passkeys answered %d (%s)", rec.Code, rec.Body.String())
	}

	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode the list body: %v (%s)", err, rec.Body.String())
	}
	if len(rows) == 0 {
		t.Fatalf("GET /v1/auth/passkeys listed no rows for a user with a stored passkey (%s)", rec.Body.String())
	}

	seen, mine := map[string]bool{}, map[string]json.RawMessage(nil)
	for _, r := range rows {
		var id string
		if raw, ok := r["id"]; ok {
			_ = json.Unmarshal(raw, &id)
		}
		seen[id] = true
		if id == base64URL(authn.id) {
			mine = r
		}
	}
	if mine == nil {
		t.Fatalf("the stored credential %s is not in the list (%s)",
			base64URL(authn.id), rec.Body.String())
	}

	// Every id must belong to THIS user. The fixture user owns exactly the one
	// credential above, so a stranger's row can only come from the query losing
	// its user_id filter.
	want := map[string]bool{}
	dbRows, err := pool.Query(ctx, `SELECT id FROM passkeys WHERE user_id=$1`, user)
	if err != nil {
		t.Fatalf("read this user's credentials: %v", err)
	}
	for dbRows.Next() {
		var id []byte
		if err := dbRows.Scan(&id); err != nil {
			dbRows.Close()
			t.Fatalf("scan this user's credential id: %v", err)
		}
		want[base64URL(id)] = true
	}
	dbRows.Close()
	if err := dbRows.Err(); err != nil {
		t.Fatalf("iterate this user's credentials: %v", err)
	}
	for id := range want {
		if !seen[id] {
			t.Fatalf("this user's credential %s is missing from the list (%s)", id, rec.Body.String())
		}
	}
	for id := range seen {
		if !want[id] {
			t.Fatalf("the list contains %s, which is NOT one of this user's credentials — the response leaked "+
				"another user's passkey (%s)", id, rec.Body.String())
		}
	}

	// ── the field itself ────────────────────────────────────────────────────
	raw, ok := mine["created_at"]
	if !ok {
		t.Fatalf("the row carries no `created_at` key at all (%s) — the field is part of the response's "+
			"contract and must be on every row", rec.Body.String())
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("`created_at` is not a JSON string: %s", raw)
	}
	if got == "" {
		t.Fatalf("`created_at` is empty for a stored passkey (%s), but the row's column holds %s — the "+
			"endpoint advertises a timestamp it never reads, which is the defect of card t_96c7c227",
			rec.Body.String(), storedCreatedAt.UTC().Format(time.RFC3339))
	}
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("`created_at` is %q, which is not RFC 3339: %v", got, err)
	}
	if !parsed.Equal(storedCreatedAt.UTC().Truncate(time.Second)) {
		t.Fatalf("`created_at` is %q, but the credential's row holds %s — the list must report the row's own "+
			"timestamp. The row is backdated seven days, so a value near the current clock means the handler "+
			"formatted time.Now() instead of the column, and an all-zero value means it formatted nothing",
			got, storedCreatedAt.UTC().Format(time.RFC3339))
	}
}
