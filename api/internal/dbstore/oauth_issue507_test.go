package dbstore

// Regression test for issue #507: the MCP OAuth INSERTs wrote the optional
// account_id through `NULLIF($n,'')` with no type information, so Postgres
// resolved the untyped parameter as TEXT and rejected the assignment to the
// uuid column:
//
//	ERROR: column "account_id" is of type uuid but expression is of type text
//
// The failure could not be caught by the handler tests because they inject an
// in-memory fake store — the real SQL was never executed. This test runs the
// REAL statements against a real Postgres (JOMFORM_TEST_DATABASE_URL; skipped
// when unset), for every site the issue lists, in both the set-account and the
// empty-account (#307) variant, and asserts the stored value (id vs NULL). A
// missing/incorrect cast therefore fails the build here, at the same place the
// prod 500 happened.
//
// Sites covered:
//   - StoreAuthCode  → oauth_auth_codes.account_id (~line 101)
//   - CreateToken    → oauth_tokens.account_id     (~line 134)
//   - RefreshGrant   → oauth_tokens rotation insert inside the tx (~line 195)

import (
	"context"
	"testing"
	"time"
)

func TestOAuthAccountIDInsertCastsUUID(t *testing.T) {
	ctx := context.Background()
	p := testPool(t)
	store := NewOAuthStore(p)

	userID := testOwnerUser(t, ctx, p)
	accountID := uniqueAccount(t, ctx, p)

	// Two variants of the same write: a real account uuid, and the empty
	// string that issue #307 made legitimate (user-level connection → NULL).
	variants := []struct {
		name      string
		accountID string
		wantNull  bool
	}{
		{"account set", accountID, false},
		{"empty account (#307)", "", true},
	}

	for _, v := range variants {
		t.Run("StoreAuthCode/"+v.name, func(t *testing.T) {
			codeHash := "it-507-code-" + randSuffix() + randSuffix()
			exp := time.Now().UTC().Add(10 * time.Minute)
			err := store.StoreAuthCode(ctx, codeHash, &OAuthAuthCode{
				ClientID: "jfc_it507", UserID: userID, AccountID: v.accountID,
				RedirectURI: "https://client.example.test/cb", CodeChallenge: "chal",
				Scope: []string{"readwrite"}, ExpiresAt: exp,
			})
			if err != nil {
				t.Fatalf("StoreAuthCode(account=%q): %v", v.accountID, err)
			}

			// The stored value must be exactly what was asked for: the uuid
			// when set, SQL NULL when empty.
			var got *string
			if err := p.QueryRow(ctx,
				`SELECT account_id::text FROM oauth_auth_codes WHERE code_hash=$1`, codeHash,
			).Scan(&got); err != nil {
				t.Fatalf("read back auth code: %v", err)
			}
			if v.wantNull {
				if got != nil {
					t.Fatalf("account_id = %q, want NULL", *got)
				}
			} else if got == nil || *got != v.accountID {
				t.Fatalf("account_id = %v, want %q", got, v.accountID)
			}

			// The code must be redeemable and carry the same account back to
			// the token exchange step (COALESCE(account_id::text,'')).
			ac, err := store.ConsumeAuthCode(ctx, codeHash)
			if err != nil {
				t.Fatalf("ConsumeAuthCode: %v", err)
			}
			if ac.AccountID != v.accountID {
				t.Fatalf("consumed account_id = %q, want %q", ac.AccountID, v.accountID)
			}
			if ac.UserID != userID {
				t.Fatalf("consumed user_id = %q, want %q", ac.UserID, userID)
			}
		})

		t.Run("CreateToken/"+v.name, func(t *testing.T) {
			accessHash := "it-507-tok-" + randSuffix() + randSuffix()
			refreshHash := "it-507-rf-" + randSuffix() + randSuffix()
			refreshExp := time.Now().UTC().Add(30 * 24 * time.Hour)
			err := store.CreateToken(ctx, &OAuthToken{
				TokenHash: accessHash, RefreshHash: refreshHash,
				ClientID: "jfc_it507", UserID: userID, AccountID: v.accountID,
				Scope:           []string{"readwrite"},
				AccessExpiresAt: time.Now().UTC().Add(time.Hour), RefreshExpiresAt: &refreshExp,
			})
			if err != nil {
				t.Fatalf("CreateToken(account=%q): %v", v.accountID, err)
			}

			var got *string
			if err := p.QueryRow(ctx,
				`SELECT account_id::text FROM oauth_tokens WHERE token_hash=$1`, accessHash,
			).Scan(&got); err != nil {
				t.Fatalf("read back token: %v", err)
			}
			if v.wantNull {
				if got != nil {
					t.Fatalf("account_id = %q, want NULL", *got)
				}
			} else if got == nil || *got != v.accountID {
				t.Fatalf("account_id = %v, want %q", got, v.accountID)
			}

			// End-to-end: the freshly stored access token must resolve (i.e.
			// the row is usable to authenticate MCP calls) and carry the
			// account through.
			at, err := store.AccessTokenByHash(ctx, accessHash)
			if err != nil {
				t.Fatalf("AccessTokenByHash: %v", err)
			}
			if at.AccountID != v.accountID {
				t.Fatalf("resolved account_id = %q, want %q", at.AccountID, v.accountID)
			}

			// Refresh rotation (site 3) re-inserts into oauth_tokens with the
			// account read back from the old row — same cast, same defect.
			rotAccess := "it-507-rot-" + randSuffix() + randSuffix()
			rotRefresh := "it-507-rotrf-" + randSuffix() + randSuffix()
			now := time.Now().UTC()
			nt, err := store.RefreshGrant(ctx, refreshHash, rotAccess, rotRefresh,
				now.Add(time.Hour), now.Add(30*24*time.Hour))
			if err != nil {
				t.Fatalf("RefreshGrant(account=%q): %v", v.accountID, err)
			}
			if nt.AccountID != v.accountID {
				t.Fatalf("rotated account_id = %q, want %q", nt.AccountID, v.accountID)
			}

			var rotGot *string
			if err := p.QueryRow(ctx,
				`SELECT account_id::text FROM oauth_tokens WHERE token_hash=$1`, rotAccess,
			).Scan(&rotGot); err != nil {
				t.Fatalf("read back rotated token: %v", err)
			}
			if v.wantNull {
				if rotGot != nil {
					t.Fatalf("rotated account_id = %q, want NULL", *rotGot)
				}
			} else if rotGot == nil || *rotGot != v.accountID {
				t.Fatalf("rotated account_id = %v, want %q", rotGot, v.accountID)
			}

			// The rotated access token must be usable too.
			if _, err := store.AccessTokenByHash(ctx, rotAccess); err != nil {
				t.Fatalf("AccessTokenByHash(rotated): %v", err)
			}
		})
	}
}
