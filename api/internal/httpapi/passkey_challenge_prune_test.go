package httpapi

// Card t_77dc59f5 — the sweep has to be on the path the ENDPOINT takes.
//
// The store guard (dbstore/passkey_challenge_prune_test.go) proves the sweep
// itself works when PutChallenge is called. It cannot prove that the endpoint's
// passkey start really goes through PutChallenge — a call site that stored the
// challenge some other way would leave that guard green and prod still growing.
// So this drives the REAL `POST /v1/auth/passkeys/login/start` twice, exactly as
// the passkey page does: the first ceremony's row is left to expire unconsumed
// (the user dismissed the prompt), and the second must take it off the table.
//
// The live half is asserted in the same breath: the second ceremony's own row
// must be stored and live, or a "sweep" that deleted everything would pass.
//
// Skipped without JOMFORM_TEST_DATABASE_URL / JOMFORM_TEST_VALKEY_URL.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPasskeyAbandonedChallengeIsSweptByTheNextLoginStart(t *testing.T) {
	ctx := context.Background()
	pool := bundleTestPool(t)
	h := passkeyHandlers(t, pool)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// startLogin drives /login/start through the real mux and returns the
	// challenge key the server issued. Each call comes from its own address
	// (uniqueTestIP), so the endpoint's rate limiter sees two separate clients —
	// which is what two separate ceremonies are.
	startLogin := func() string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost,
			"http://"+passkeyOriginHost+"/v1/auth/passkeys/login/start", nil)
		setTestClientAddr(req, uniqueTestIP())
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("login/start answered %d (%s)", rec.Code, rec.Body.String())
		}
		var st struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Key == "" {
			t.Fatalf("login/start body has no key: %v (%s)", err, rec.Body.String())
		}
		return st.Key
	}

	challengeRows := func(key string) (rows int, live int) {
		t.Helper()
		if err := pool.QueryRow(ctx,
			`SELECT count(*), count(*) FILTER (WHERE expires_at > now())
			   FROM passkey_challenges WHERE key=$1`, key).Scan(&rows, &live); err != nil {
			t.Fatalf("read the stored challenge %s: %v", key, err)
		}
		return rows, live
	}

	abandoned := startLogin()
	if rows, live := challengeRows(abandoned); rows != 1 || live != 1 {
		t.Fatalf("premise broken: login/start stored %d row(s) for %s, %d of them live, want 1 and 1", rows, abandoned, live)
	}

	// The abandonment: the 5-minute TTL runs out with no /finish. Backdating is
	// the only way to reach that state inside a test — and it is prod's state,
	// measured: 82 rows, none of them live.
	if _, err := pool.Exec(ctx,
		`UPDATE passkey_challenges SET expires_at = now() - interval '1 second' WHERE key=$1`,
		abandoned); err != nil {
		t.Fatalf("backdate the abandoned challenge: %v", err)
	}
	if rows, live := challengeRows(abandoned); rows != 1 || live != 0 {
		t.Fatalf("premise broken: the abandoned challenge %s reads %d row(s), %d live, want 1 and 0", abandoned, rows, live)
	}

	second := startLogin()

	if rows, live := challengeRows(abandoned); rows != 0 {
		t.Errorf("POST /v1/auth/passkeys/login/start left the previous, EXPIRED challenge (%s) on disk (%d row(s), %d live) — "+
			"the endpoint has to carry the sweep, not just the store, or prod keeps every abandoned ceremony's row forever",
			abandoned, rows, live)
	}
	if rows, live := challengeRows(second); rows != 1 || live != 1 {
		t.Errorf("the second ceremony's own challenge (%s) reads %d row(s), %d live, want 1 and 1 — the sweep must not take "+
			"back the challenge PutChallenge just minted", second, rows, live)
	}
}
