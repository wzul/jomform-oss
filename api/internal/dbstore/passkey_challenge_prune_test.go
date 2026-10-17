package dbstore

// Card t_77dc59f5 — an abandoned passkey ceremony left its challenge row on
// disk for good, and this is the guard for the sweep that removes it.
//
// THE DEFECT. `passkey_challenges` had exactly one writer (PutChallenge, on
// /register/start and /login/start) and exactly one deleter (TakeChallenge, on
// /finish). A ceremony the user walks away from — the passkey prompt dismissed,
// the tab closed, the phone never answered — is therefore never deleted, and
// nothing else in the repo ever touched the table: no cron, no deploy script, no
// store method. Measured on prod: 82 rows, ZERO of them live, the newest expiry
// already 3h20m old — i.e. not a burst of in-flight ceremonies, but a year of
// dead ones (every row also carried an encrypted server-side session that had
// outlived its own 5-minute contract, and an entry in
// idx_passkey_challenges_exp that the planner still has to step over).
//
// WHAT IS PINNED HERE, and why each arm is needed. The consume path was the only
// path any test covered, so the guard has to fail for every way a sweep can be
// wrong — not just for "no sweep at all" — and the rows are planted so that each
// wrong sweep is caught by the arm that names it:
//
//   - An expired, UNCONSUMED row is gone after the next ceremony. The abandoned
//     login challenge is stored through PutChallenge — the real write path — so
//     removing the sweep, or wiring it to something the handlers never call,
//     reds this arm.
//   - An expired row of the OTHER kind is gone too. The register challenge is
//     planted directly, so a sweep filtered on its caller's kind (the login
//     ceremony's) is caught rather than leaving half the table behind forever.
//   - LIVE rows survive it. A "sweep" written as `DELETE FROM passkey_challenges`
//     with no predicate would satisfy the arms above and destroy every ceremony
//     in flight; that row is planted directly, BEFORE the sweep runs, so the arm
//     that catches it is this one and not an earlier premise check.
//   - The premise is asserted, not assumed: each abandoned row really is past
//     its expiry, and TakeChallenge really does refuse it — which is what makes
//     deleting it safe rather than a race against a ceremony that still works.
//   - The janitor path is driven too (PruneExpiredChallenges called directly, as
//     the API's hourly sweep does): it must report what it removed, must remove
//     the expired row, and must not be a second, wider delete than the one on
//     the write path.
//
// MEASURED BITE (value-only mutations, file sha256 asserted before and after,
// restored byte-exact): with the `PruneExpiredChallenges` call removed from
// PutChallenge both abandoned challenges survive and this guard FAILs on the
// arms above; with the predicate weakened to a plain `DELETE FROM
// passkey_challenges` the in-flight row goes with them and the live-row arm is
// what FAILs; with the predicate made timid (`<= now() - interval '10 minutes'`)
// the expired rows survive and both the write-path and janitor arms FAIL.
// Skipped without JOMFORM_TEST_DATABASE_URL.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// challengeKey mints a per-PASS key: `-test.count` re-runs the binary in one
// process against the same scratch schema, so a fixed key would make pass 2
// read pass 1's rows.
func challengeKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// storedChallenge reports whether a row exists for key, and when it expires.
func storedChallenge(t *testing.T, pool *pgxpool.Pool, key string) (bool, time.Time) {
	t.Helper()
	var exp time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT expires_at FROM passkey_challenges WHERE key=$1`, key).Scan(&exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, time.Time{}
	}
	if err != nil {
		t.Fatalf("read challenge %s: %v", key, err)
	}
	return true, exp
}

// plantChallenge writes a challenge row the way a PREVIOUS ceremony left it:
// expired when live is false, with the store's own 5-minute TTL when it is true.
// Direct SQL on purpose: a row that has to survive into the sweep phase must not
// depend on the sweep being correct, or a broken sweep deletes it early and the
// arm that names the breakage never reports.
func plantChallenge(t *testing.T, pool *pgxpool.Pool, key, kind string, live bool) {
	t.Helper()
	offset := "- interval '1 second'"
	if live {
		offset = "+ interval '5 minutes'"
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO passkey_challenges (key, value, kind, expires_at)
		 VALUES ($1, '{"challenge":"planted"}', $2, now() `+offset+`)`, key, kind); err != nil {
		t.Fatalf("plant challenge %s/%s: %v", key, kind, err)
	}
}

// TestPasskeyAbandonedChallengeIsSweptByTheNextCeremony is the guard described
// at the top of this file.
func TestPasskeyAbandonedChallengeIsSweptByTheNextCeremony(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	store := NewPasskeyStore(pool)

	// A login ceremony the user dismissed: started through the real write path,
	// then left to expire unconsumed.
	abandonedLogin := challengeKey("prune-abandoned-login")
	if err := store.PutChallenge(ctx, abandonedLogin, `{"challenge":"abandoned"}`, "login"); err != nil {
		t.Fatalf("PutChallenge(%s): %v", abandonedLogin, err)
	}
	// The other kind, and a ceremony that is still in flight: planted directly,
	// with a live expiry, so both are on disk when the sweep runs.
	abandonedRegister := challengeKey("prune-abandoned-register")
	plantChallenge(t, pool, abandonedRegister, "register", false)
	live := challengeKey("prune-live-register")
	plantChallenge(t, pool, live, "register", true)

	// The abandonment, made real. Backdating is the only way to reach that state
	// inside a test, and it is exactly prod's state: expired, and still on disk.
	if _, err := pool.Exec(ctx,
		`UPDATE passkey_challenges SET expires_at = now() - interval '1 second' WHERE key=$1`,
		abandonedLogin); err != nil {
		t.Fatalf("backdate the abandoned login challenge: %v", err)
	}

	for _, c := range []struct{ key, kind string }{
		{abandonedLogin, "login"},
		{abandonedRegister, "register"},
	} {
		ok, exp := storedChallenge(t, pool, c.key)
		if !ok {
			t.Fatalf("premise broken: the abandoned %s challenge is not in the table at all, so this test would pass on a "+
				"table with nothing to sweep", c.kind)
		}
		if exp.After(time.Now()) {
			t.Fatalf("premise broken: the abandoned %s challenge is still LIVE (expires %s) — the row under test is not the "+
				"one the defect leaves behind", c.kind, exp)
		}
		// The safety property the sweep rests on, asserted rather than assumed:
		// an expired row cannot be consumed, so no /finish can be racing it.
		if _, err := store.TakeChallenge(ctx, c.key, c.kind); !errors.Is(err, errNoChallenge) {
			t.Fatalf("premise broken: an EXPIRED %s challenge was consumable (err=%v) — the sweep would be deleting rows a "+
				"ceremony can still use", c.kind, err)
		}
		if ok, _ := storedChallenge(t, pool, c.key); !ok {
			t.Fatalf("premise broken: TakeChallenge removed the expired %s row, so the sweep has nothing left to prove", c.kind)
		}
	}

	// The next ceremony — the write path every /register/start and /login/start
	// goes through, and the one that has to carry the sweep.
	next := challengeKey("prune-next-ceremony")
	if err := store.PutChallenge(ctx, next, `{"challenge":"next"}`, "register"); err != nil {
		t.Fatalf("PutChallenge(%s): %v", next, err)
	}

	for _, c := range []struct{ key, kind string }{
		{abandonedLogin, "login"},
		{abandonedRegister, "register"},
	} {
		if ok, exp := storedChallenge(t, pool, c.key); ok {
			t.Errorf("the abandoned %s challenge is STILL in passkey_challenges (expired at %s) after the next ceremony ran — "+
				"an abandoned ceremony keeps its row forever, which is the defect: prod held 82 rows like this, 0 of them live",
				c.kind, exp)
		}
	}
	for _, l := range []struct{ key, why string }{
		{live, "a ceremony that was already in flight can no longer be finished"},
		{next, "the ceremony cannot be finished by the row that started it"},
	} {
		if ok, exp := storedChallenge(t, pool, l.key); !ok {
			t.Errorf("the sweep deleted the LIVE challenge %s — a sweep without the `expires_at <= now()` predicate (or one that "+
				"runs after the INSERT) satisfies the arm above and breaks every ceremony: %s", l.key, l.why)
		} else if !exp.After(time.Now()) {
			t.Errorf("the live challenge %s is not live (expires %s)", l.key, exp)
		}
	}

	// The janitor path: the API's hourly retention sweep calls this directly, and
	// it is the half that drains a deployment which has served no ceremony at all
	// — the row left by the LAST abandoned ceremony before traffic stopped.
	quiet := challengeKey("prune-abandoned-quiet")
	plantChallenge(t, pool, quiet, "login", false)

	n, err := store.PruneExpiredChallenges(ctx)
	if err != nil {
		t.Fatalf("PruneExpiredChallenges: %v", err)
	}
	if n < 1 {
		t.Errorf("PruneExpiredChallenges reported %d row(s) removed, want at least the one expired row it was handed — the "+
			"janitor's count is what the operator reads to tell \"nothing to sweep\" from \"the sweep is broken\"", n)
	}
	if ok, exp := storedChallenge(t, pool, quiet); ok {
		t.Errorf("the janitor's sweep left an expired challenge behind (%s, expired at %s)", quiet, exp)
	}
	if ok, _ := storedChallenge(t, pool, live); !ok {
		t.Errorf("the janitor's sweep took the LIVE challenge %s with it — calling the sweep directly must not be a wider delete "+
			"than the write path's", live)
	}
}
